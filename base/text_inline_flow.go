package base

// Port of U:text/inline_flow.rs (ticket 28): InlineFlow, the paragraph
// layout that wraps text, inline images and plugin objects into lines and
// paints each line fragment as its own Inline, InlineObject or image.
//
// Wrapping is re-expressed over the legacy facade ticket 10 restored in
// gpui (gpui/line_wrapper.go LineWrapper + LineFragment, and
// gpui/shaped_line.go WindowTextSystem.ShapeLine / ShapedLine.XForIndex),
// because the pin has no LineWrapper (R:text_system.rs). Word and grapheme
// bounds come from base/segment (unicode-segmentation's
// split_word_bounds / graphemes).
//
// Go adaptations (recorded deviations):
//   - cap_height (U:text/inline_flow.rs:1122-1128): the pin exposes no
//     font metrics through the window text system, so a code span's
//     background is centered on a cap height of 0.7 em (the common Latin
//     cap height; Base's own test font uses 700/1000).
//   - Images: gpui's Img has no click listeners (it is not an
//     InteractiveElement in the port), so a linked image is wrapped in an
//     identified div carrying the cursor and the click/aux-click handlers.
//   - The ShapedLine handed out by the facade lives for the frame that
//     shaped it (gpui/shaped_line.go), so the layout keeps widths and
//     metrics, never a ShapedLine.

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/hoangvu12/gpui-go/base/segment"
	"github.com/hoangvu12/gpui-go/gpui"
)

// inlineFlowImageLen is the byte length an image or object stands for
// (U:text/inline_flow.rs:26).
const inlineFlowImageLen = 1

// inlineCodePadding is U:text/inline_flow.rs:27 INLINE_CODE_PADDING.
const inlineCodePadding float32 = 2

// inlineCapHeightEm is the cap height re-expression (see the file comment).
const inlineCapHeightEm float32 = 0.7

// inlineRenderer renders an inline object (U:text/inline_flow.rs:35-37).
type inlineRenderer func(context *InlineRenderContext, w *gpui.Window, app *gpui.App) (InlineElement, bool)

// inlineFlowItemKind selects the InlineFlowItem variant.
type inlineFlowItemKind int

const (
	inlineFlowObject inlineFlowItemKind = iota
	inlineFlowText
	inlineFlowImage
)

// inlineFlowItem is U:text/inline_flow.rs:39-67.
type inlineFlowItem struct {
	kind inlineFlowItemKind
	// Object
	id                 int
	renderer           inlineRenderer
	accessibilityLabel string
	selected           *sharedFlag
	style              gpui.HighlightStyle
	// Object and Image
	link *LinkMark
	// Object and Text
	text string
	// Text
	state       *inlineState
	links       []textLinkRange
	highlights  []inlineHighlightRange
	backgrounds []textColorRange
	// reveal is the start of a pending reveal in this item (ticket 30).
	reveal *revealAt
	// Image
	source gpui.ImageSource
	title  string
	width  *gpui.DefiniteLength
	height *gpui.DefiniteLength
}

// measureLen is MeasureItem::len (U:text/inline_flow.rs:727-733).
func (i *inlineFlowItem) measureLen() int {
	if i.kind == inlineFlowText {
		return len(i.text)
	}
	return inlineFlowImageLen
}

// sameLayoutInput reports whether other lays out the same; an object is
// rendered anew every frame, so a flow with one is never reused
// (U:text/inline_flow.rs:735-761).
func (i *inlineFlowItem) sameLayoutInput(o *inlineFlowItem) bool {
	switch {
	case i.kind == inlineFlowText && o.kind == inlineFlowText:
		return i.text == o.text && textLinksEqual(i.links, o.links) && inlineHighlightsEqual(i.highlights, o.highlights)
	case i.kind == inlineFlowImage && o.kind == inlineFlowImage:
		return optEqual(i.width, o.width) && optEqual(i.height, o.height)
	}
	return false
}

// inlineFlow is the InlineFlow element (U:text/inline_flow.rs:29-33).
type inlineFlow struct {
	id               gpui.ElementID
	items            []inlineFlowItem
	linkClickHandler LinkClickHandlerFn
}

// newInlineFlow is InlineFlow::new (U:text/inline_flow.rs:244-255).
func newInlineFlow(id gpui.ElementID, items []inlineFlowItem, handler LinkClickHandlerFn) *inlineFlow {
	return &inlineFlow{id: id, items: items, linkClickHandler: handler}
}

// inlineFlowTypography is resolved while the parent's text-style and rem
// stacks are active (U:text/inline_flow.rs:161-168).
type inlineFlowTypography struct {
	textStyle  gpui.TextStyle
	remSize    float32
	lineHeight float32
}

func (t inlineFlowTypography) equal(o inlineFlowTypography) bool {
	return t.remSize == o.remSize && t.lineHeight == o.lineHeight && textStyleEqual(t.textStyle, o.textStyle)
}

// flowLayoutKey is everything but the wrap width a flow's layout depends on
// (U:text/inline_flow.rs:141-159).
type flowLayoutKey struct {
	items      []inlineFlowItem
	imageSizes []*gpui.Size
	typography inlineFlowTypography
}

func (k *flowLayoutKey) matches(o *flowLayoutKey) bool {
	if len(k.items) != len(o.items) || !k.typography.equal(o.typography) || !reflect.DeepEqual(k.imageSizes, o.imageSizes) {
		return false
	}
	for i := range k.items {
		if !k.items[i].sameLayoutInput(&o.items[i]) {
			return false
		}
	}
	return true
}

// inlineFlowFrameState is what a flow keeps from frame to frame as element
// state under its id (U:text/inline_flow.rs:79-139).
type inlineFlowFrameState struct {
	fragmentStates []*inlineState
	key            *flowLayoutKey
	layouts        []*inlineFlowLayout
}

// layoutsPerFlow is U:text/inline_flow.rs:99-100.
const layoutsPerFlow = 3

func (s *inlineFlowFrameState) fragmentState(ix int) *inlineState {
	for len(s.fragmentStates) <= ix {
		s.fragmentStates = append(s.fragmentStates, &inlineState{})
	}
	return s.fragmentStates[ix]
}

func (s *inlineFlowFrameState) setKey(key *flowLayoutKey) {
	if s.key == nil || !s.key.matches(key) {
		s.layouts = nil
	}
	s.key = key
}

func (s *inlineFlowFrameState) layoutAt(wrapWidth *float32) *inlineFlowLayout {
	for _, l := range s.layouts {
		if optEqual(l.wrapWidth, wrapWidth) {
			return l
		}
	}
	return nil
}

func (s *inlineFlowFrameState) pushLayout(l *inlineFlowLayout) {
	if len(s.layouts) >= layoutsPerFlow {
		s.layouts = s.layouts[1:]
	}
	s.layouts = append(s.layouts, l)
}

// inlineFlowLayout is U:text/inline_flow.rs:170-175.
type inlineFlowLayout struct {
	fragments []positionedFragment
	size      gpui.Size
	wrapWidth *float32
}

// positionedFragmentKind selects the PositionedFragment variant.
type positionedFragmentKind int

const (
	positionedObject positionedFragmentKind = iota
	positionedText
	positionedImage
)

// positionedFragment is U:text/inline_flow.rs:177-202.
type positionedFragment struct {
	kind            positionedFragmentKind
	itemIx          int
	origin          gpui.Point
	size            gpui.Size
	object          *measuredInlineObject
	selectionBounds gpui.Bounds
	sourceRange     gpui.TextRange
	fontSize        float32
	text            string
	links           []textLinkRange
	highlights      []inlineHighlightRange
	codeBackground  *textCodeBackground
}

// textCodeBackground is the rounded box behind a code span.
type textCodeBackground struct {
	bounds gpui.Bounds
	color  gpui.Hsla
}

// inlineFlowLayoutState is the RequestLayoutState
// (U:text/inline_flow.rs:69-77).
type inlineFlowLayoutState struct {
	typography inlineFlowTypography
	frameState *inlineFlowFrameState
	current    **inlineFlowLayout
}

// inlineFlowPrepaint is one prepainted element with its code background.
type inlineFlowPrepaint struct {
	element    gpui.AnyElement
	background *textCodeBackground
	// suppressed marks the detached-root fragments (objects and images,
	// browser-core 20): their paints run under selection suppression
	// like their prepaints.
	suppressed bool
}

// IntoElement implements gpui.IntoElement.
func (f *inlineFlow) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[inlineFlowLayoutState, []inlineFlowPrepaint](f)
}

// ID implements gpui.Element (U:text/inline_flow.rs:314-316).
func (f *inlineFlow) ID() (gpui.ElementID, bool) { return f.id, true }

// SourceLocation implements gpui.Element.
func (f *inlineFlow) SourceLocation() *gpui.SourceLocation { return nil }

// inlineFlowFrameStateBox holds the frame state as element state.
type inlineFlowFrameStateBox struct{ s *inlineFlowFrameState }

// RequestLayout captures the typography, measures images and objects, and
// requests a measured layout that wraps the flow at the width it is given,
// reusing a layout of the same key and width (U:text/inline_flow.rs:322-419).
func (f *inlineFlow) RequestLayout(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, inlineFlowLayoutState) {
	frameState := gpui.WithElementState(w, global, func(slot *inlineFlowFrameStateBox, _ *gpui.Window) (*inlineFlowFrameState, inlineFlowFrameStateBox) {
		if slot == nil || slot.s == nil {
			fresh := inlineFlowFrameStateBox{s: &inlineFlowFrameState{}}
			return fresh.s, fresh
		}
		return slot.s, *slot
	})
	typography := inlineFlowTypography{textStyle: gpui.WindowTextStyle(w), remSize: gpui.RemSize(w), lineHeight: w.LineHeight()}
	imageSizes := make([]*gpui.Size, len(f.items))
	for ix := range f.items {
		item := &f.items[ix]
		if item.kind == inlineFlowImage {
			size := measureImageSize(ix, item.source, item.width, item.height, typography.lineHeight, typography.remSize, w, app)
			imageSizes[ix] = &size
		}
	}
	objects := prepareInlineObjects(f.items, typography.textStyle, w, app)
	frameState.setKey(&flowLayoutKey{items: f.items, imageSizes: imageSizes, typography: typography})
	var current *inlineFlowLayout
	state := inlineFlowLayoutState{typography: typography, frameState: frameState, current: &current}
	currentSlot := state.current

	id, err := gpui.RequestElementMeasuredLayout(w, gpui.DefaultStyle(), func(req gpui.MeasureRequest) gpui.Size {
		var wrapWidth *float32
		if typography.textStyle.WhiteSpace.WrapsAtWidth() {
			if req.KnownWidthPresent {
				width := req.KnownWidth
				wrapWidth = &width
			} else if req.AvailWidth.Kind == gpui.AvailDefinite {
				width := req.AvailWidth.Definite
				wrapWidth = &width
			}
		}
		if l := frameState.layoutAt(wrapWidth); l != nil {
			*currentSlot = l
			return l.size
		}
		key := frameState.key
		refinement := textStyleSubtractDefault(typography.textStyle)
		layout := gpui.WithRemSize(w, typography.remSize, func(w *gpui.Window) *inlineFlowLayout {
			return gpui.WithTextStyle(w, &refinement, func(w *gpui.Window) *inlineFlowLayout {
				return layoutMeasuredFlow(key.items, key.imageSizes, objects, typography.textStyle, wrapWidth, w)
			})
		})
		*currentSlot = layout
		frameState.pushLayout(layout)
		return layout.size
	})
	if err != nil {
		panic(fmt.Sprintf("base: InlineFlow request_layout: %v", err))
	}
	return id, state
}

// Prepaint lays every fragment out as its own root at its origin
// (U:text/inline_flow.rs:421-654).
func (f *inlineFlow) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, state *inlineFlowLayoutState, w *gpui.Window, app *gpui.App) []inlineFlowPrepaint {
	layout := *state.current
	if layout == nil {
		return nil
	}
	textStyle := state.typography.textStyle
	elements := make([]inlineFlowPrepaint, 0, len(layout.fragments))
	revealIx := f.revealFragment(layout.fragments)
	textFragmentCount := 0
	for fragmentIx := range layout.fragments {
		fragment := &layout.fragments[fragmentIx]
		switch fragment.kind {
		case positionedObject:
			item := &f.items[fragment.itemIx]
			if item.kind != inlineFlowObject {
				continue
			}
			objectSize := fragment.object.metrics.size
			sel := fragment.selectionBounds
			el := newInlineObject(
				gpui.NamedIntegerElementID("inline-object", uint64(item.id)),
				item.text, item.accessibilityLabel, *fragment.object, item.selected,
				gpui.Bounds{Origin: textPointAdd(bounds.Origin, sel.Origin), Size: sel.Size},
				gpui.Bounds{Origin: gpui.Point{X: bounds.Left(), Y: bounds.Top() + sel.Top()}, Size: gpui.Size{Width: bounds.Size.Width, Height: sel.Size.Height}},
			).withLink(item.link, f.linkClickHandler).IntoElement()
			// Browser-core 20: an object's content prepaints as its own
			// root, outside the window's plain-text document — its inner
			// text must not register a second, competing participant (the
			// paragraph's own registration owns the line).
			gpui.WithUserSelectSuppressedVoid(w, func(w *gpui.Window) {
				el.PrepaintAsRoot(textPointAdd(bounds.Origin, fragment.origin), gpui.AvailableSize{
					Width:  gpui.DefiniteAvailableSpace(objectSize.Width),
					Height: gpui.DefiniteAvailableSpace(objectSize.Height),
				}, w, app)
			})
			elements = append(elements, inlineFlowPrepaint{element: el, suppressed: true})
		case positionedText:
			item := &f.items[fragment.itemIx]
			if item.kind != inlineFlowText {
				continue
			}
			fragState := state.frameState.fragmentState(textFragmentCount)
			textFragmentCount++
			fragState.setText(fragment.text)
			isCode := false
			for _, h := range fragment.highlights {
				if h.h.fontSizeScale != nil {
					isCode = true
				}
			}
			padding := float32(0)
			if isCode {
				padding = inlineCodePadding
			}
			var background *textCodeBackground
			if cb := fragment.codeBackground; cb != nil {
				background = &textCodeBackground{bounds: gpui.Bounds{Origin: textPointAdd(bounds.Origin, cb.bounds.Origin), Size: cb.bounds.Size}, color: cb.color}
			}
			// The fragment's typography: the flow's style at the fragment's
			// size, on one line; fragments are already wrapped.
			fragmentStyle := textStyle
			fragmentStyle.FontSize = gpui.Px(fragment.fontSize)
			fragmentStyle.LineHeight = gpui.DefinitePx(fragment.size.Height)
			// CSS pre's pair — preserved text, no soft wrap — the verbatim
			// no-wrap of a pre-wrapped fragment (browser-core 38: nowrap
			// would collapse the fragment's spaces).
			fragmentStyle.WhiteSpace = gpui.WhiteSpacePre
			origin := textPointAdd(textPointAdd(bounds.Origin, fragment.origin), gpui.Point{X: padding})
			sel := fragment.selectionBounds
			el := newInlineText(fragState, fragment.links, fragment.highlights, f.linkClickHandler).
				withSelectionSource(item.state, fragment.sourceRange).
				withRangeBackgrounds(sliceColorRanges(item.backgrounds, fragment.sourceRange.Start, fragment.sourceRange.End)).
				withReveal(f.fragmentReveal(item, fragment, fragmentIx, revealIx)).
				withTextStyle(fragmentStyle).
				withSelectionBounds(gpui.Bounds{Origin: gpui.Point{X: bounds.Left(), Y: bounds.Top() + sel.Top()}, Size: gpui.Size{Width: bounds.Size.Width, Height: sel.Size.Height}}).
				withPaintOrigin(origin).
				IntoElement()
			refinement := textStyleSubtractDefault(fragmentStyle)
			gpui.WithRemSize(w, state.typography.remSize, func(w *gpui.Window) struct{} {
				gpui.WithTextStyleVoid(w, &refinement, func(w *gpui.Window) {
					el.PrepaintAsRoot(origin, gpui.AvailableSize{
						Width:  gpui.DefiniteAvailableSpace(fragment.size.Width - padding*2),
						Height: gpui.DefiniteAvailableSpace(fragment.size.Height),
					}, w, app)
				})
				return struct{}{}
			})
			elements = append(elements, inlineFlowPrepaint{element: el, background: background})
		case positionedImage:
			item := &f.items[fragment.itemIx]
			if item.kind != inlineFlowImage {
				continue
			}
			el := inlineFlowImageElement(len(elements), item.source, item.link, fragment.size, f.linkClickHandler)
			// Like the object fragments (browser-core 20): detached roots.
			gpui.WithUserSelectSuppressedVoid(w, func(w *gpui.Window) {
				el.PrepaintAsRoot(textPointAdd(bounds.Origin, fragment.origin), gpui.AvailableSize{
					Width:  gpui.DefiniteAvailableSpace(fragment.size.Width),
					Height: gpui.DefiniteAvailableSpace(fragment.size.Height),
				}, w, app)
			})
			elements = append(elements, inlineFlowPrepaint{element: el, suppressed: true})
		}
	}
	if textFragmentCount < len(state.frameState.fragmentStates) {
		state.frameState.fragmentStates = state.frameState.fragmentStates[:textFragmentCount]
	}
	return elements
}

// Paint clears stale fragment selections, then paints each code background
// under its fragment (U:text/inline_flow.rs:656-685).
func (f *inlineFlow) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *inlineFlowLayoutState, prepaint *[]inlineFlowPrepaint, w *gpui.Window, app *gpui.App) {
	preserve := false
	if view, ok := textViewStateCurrent(app); ok {
		preserve = view.Read(app, func(s *TextViewState, _ *gpui.App) bool { return s.preserveInlineSelection })
	}
	if !preserve {
		for i := range f.items {
			if f.items[i].kind == inlineFlowText {
				f.items[i].state.setSelection(nil)
			}
		}
	}
	radius := ThemeGlobal(app).Tokens.Radius.Sm
	for _, p := range *prepaint {
		if bg := p.background; bg != nil {
			q := gpui.Fill(bg.bounds, gpui.SolidBackground(bg.color))
			q.CornerRadii = gpui.Corners{TopLeft: radius, TopRight: radius, BottomRight: radius, BottomLeft: radius}
			w.PaintQuad(q)
		}
		// The object and image fragments paint under selection
		// suppression, like their prepaint (browser-core 20).
		if p.suppressed {
			gpui.WithUserSelectSuppressedVoid(w, func(w *gpui.Window) {
				p.element.Paint(w, app)
			})
		} else {
			p.element.Paint(w, app)
		}
	}
}

// inlineFlowImageElement is InlineFlow::image_element
// (U:text/inline_flow.rs:257-299; the click handlers ride a wrapper, see
// the file comment).
func inlineFlowImageElement(ix int, source gpui.ImageSource, link *LinkMark, size gpui.Size, handler LinkClickHandlerFn) gpui.AnyElement {
	image := gpui.Img(source).Flex().ID(gpui.IntegerElementID(uint64(ix))).ObjectFit(gpui.ObjectFitContain).
		MaxW(gpui.Fraction(1)).W(gpui.Px(size.Width)).H(gpui.Px(size.Height))
	if link == nil {
		return image.IntoElement()
	}
	return textLinkedImage(gpui.NamedIntegerElementID("image-link", uint64(ix)), image, link.URL, handler)
}

// textLinkedImage wraps a linked image in an identified div carrying the
// pointer cursor and the click / aux-click link handlers
// (U:text/inline_flow.rs:271-297, U:text/node.rs:2215-2242).
func textLinkedImage(id gpui.ElementID, image *gpui.ImgElement, url string, handler LinkClickHandlerFn) gpui.AnyElement {
	open := func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
		textSelectionEndHook(w, app)
		app.StopPropagation()
		handleLinkClick(handler, url, *event, w, app)
	}
	return gpui.Div().Flex().ID(id).CursorPointer().OnClick(open).OnAuxClick(open).Child(image).IntoElement()
}

// inlineFlowIntrinsicWidth is the intrinsic width for table sizing, using
// the same objects and wrapping inputs as painting
// (U:text/inline_flow.rs:764-797).
func inlineFlowIntrinsicWidth(items []inlineFlowItem, w *gpui.Window, app *gpui.App) float32 {
	lineHeight := w.LineHeight()
	rem := gpui.RemSize(w)
	images := make([]*gpui.Size, len(items))
	for ix := range items {
		if items[ix].kind == inlineFlowImage {
			s := measureImageSize(ix, items[ix].source, items[ix].width, items[ix].height, lineHeight, rem, w, app)
			images[ix] = &s
		}
	}
	return layoutFlow(items, images, gpui.WindowTextStyle(w), nil, w, app).size.Width
}

// prepareInlineObjects renders and measures every object
// (U:text/inline_flow.rs:799-836).
func prepareInlineObjects(items []inlineFlowItem, textStyle gpui.TextStyle, w *gpui.Window, app *gpui.App) []*measuredInlineObject {
	out := make([]*measuredInlineObject, len(items))
	for i := range items {
		item := &items[i]
		if item.kind != inlineFlowObject {
			continue
		}
		style := textStyle.Highlight(item.style)
		rem := gpui.RemSize(w)
		fontSize := style.FontSizePixels(rem)
		// Browser-parity D1: the style's CSS `normal` line height (the
		// default) resolves through the window's text system.
		context := newInlineRenderContext(style, fontSize, gpui.ResolveLineHeight(w, style), rem)
		// The id inline objects paint under, so a stateful child sees the
		// same id path in request_layout as in prepaint.
		measured := gpui.WithID(w, gpui.NamedIntegerElementID("inline-object", uint64(item.id)), func(w *gpui.Window) measuredInlineObject {
			refinement := textStyleSubtractDefault(style)
			var presentation *InlineElement
			gpui.WithTextStyleVoid(w, &refinement, func(w *gpui.Window) {
				if item.renderer != nil {
					if el, ok := item.renderer(context, w, app); ok {
						presentation = &el
					}
				}
			})
			return measureInlineObject(item.text, presentation, style, w, app)
		})
		out[i] = &measured
	}
	return out
}

// layoutFlow prepares the objects and lays the flow out
// (U:text/inline_flow.rs:838-856).
func layoutFlow(items []inlineFlowItem, imageSizes []*gpui.Size, textStyle gpui.TextStyle, wrapWidth *float32, w *gpui.Window, app *gpui.App) *inlineFlowLayout {
	objects := prepareInlineObjects(items, textStyle, w, app)
	return layoutMeasuredFlow(items, imageSizes, objects, textStyle, wrapWidth, w)
}

// flowLayouts counts the flows laid out (U:text/inline_flow.rs:858-862,
// cfg(test) FLOW_LAYOUTS).
var flowLayouts int

// lineFragmentLayout is U:text/inline_flow.rs:223-242.
type lineFragmentLayout struct {
	itemIx         int
	kind           positionedFragmentKind
	size           gpui.Size
	sourceRange    gpui.TextRange
	baseline       float32
	object         *measuredInlineObject
	fontSize       float32
	text           string
	links          []textLinkRange
	highlights     []inlineHighlightRange
	codeBackground *textCodeBackground
}

// layoutMeasuredFlow wraps the items into lines and positions every
// fragment on its line's shared baseline (U:text/inline_flow.rs:864-1106).
func layoutMeasuredFlow(items []inlineFlowItem, imageSizes []*gpui.Size, prepared []*measuredInlineObject, textStyle gpui.TextStyle, wrapWidth *float32, w *gpui.Window) *inlineFlowLayout {
	flowLayouts++
	rem := gpui.RemSize(w)
	lineHeight := plainLineHeight(textStyle, rem, w)
	totalLen := 0
	for i := range items {
		totalLen += items[i].measureLen()
	}
	if totalLen == 0 {
		return &inlineFlowLayout{wrapWidth: wrapWidth}
	}
	objects := make([]*measuredInlineObject, len(prepared))
	for i, o := range prepared {
		if o != nil {
			fitted := o.fitText(wrapWidth)
			objects[i] = &fitted
		}
	}
	lines := inlineFlowLineRanges(items, imageSizes, objects, textStyle, wrapWidth, w)
	fontSize := textStyle.FontSizePixels(rem)
	var fragments []positionedFragment
	maxWidth, y := float32(0), float32(0)

	for _, lineRange := range lines {
		var lineFragments []lineFragmentLayout
		lineWidth := float32(0)
		// The body line's shaped metrics, the same the text paint path uses.
		bodyAscent, bodyDescent := shapedMetrics(" ", fontSize, textRuns(1, textStyle, nil), w)
		bodyBaseline := centeredBaseline(bodyAscent, bodyDescent, lineHeight)
		bodyOverflowAbove := max(bodyAscent-bodyBaseline, 0)
		bodyOverflowBelow := max(bodyDescent-(lineHeight-bodyBaseline), 0)
		lineAscent, lineDescent := bodyBaseline, lineHeight-bodyBaseline
		itemStart := 0

		for itemIx := range items {
			item := &items[itemIx]
			itemEnd := itemStart + item.measureLen()
			if itemEnd <= lineRange.Start {
				itemStart = itemEnd
				continue
			}
			if itemStart >= lineRange.End {
				break
			}
			switch item.kind {
			case inlineFlowText:
				localStart := max(lineRange.Start, itemStart) - itemStart
				localEnd := min(lineRange.End, itemEnd) - itemStart
				for _, segment := range textSizeRanges(len(item.text), item.highlights) {
					start, end := max(localStart, segment.rng.Start), min(localEnd, segment.rng.End)
					if start >= end {
						continue
					}
					subtext := item.text[start:end]
					highlights := sliceHighlightRanges(item.highlights, start, end)
					links := sliceLinkRanges(item.links, start, end)
					runs := textRuns(len(subtext), textStyle, highlights)
					segmentFontSize := fontSize * segment.scale
					shapedWidth, ascent, descent := shapedLineMetrics(subtext, segmentFontSize, runs, w)
					isCode := highlightsHaveScale(highlights)
					padding := float32(0)
					if isCode {
						padding = inlineCodePadding * 2
					}
					width := shapedWidth + padding
					glyphSize := gpui.Size{Width: width, Height: ascent + descent}
					baseline := ascent
					var codeBackground *textCodeBackground
					if isCode {
						codeBackground = inlineCodeBackground(descent, segmentFontSize, highlights, glyphSize, baseline)
					}
					lineAscent = max(lineAscent, baseline-bodyOverflowAbove)
					lineDescent = max(lineDescent, glyphSize.Height-baseline-bodyOverflowBelow)
					lineWidth += width
					lineFragments = append(lineFragments, lineFragmentLayout{
						itemIx: itemIx, kind: positionedText, size: glyphSize,
						sourceRange: gpui.TextRange{Start: start, End: end}, baseline: baseline,
						fontSize: segmentFontSize, text: subtext, links: links, highlights: highlights,
						codeBackground: codeBackground,
					})
				}
			case inlineFlowObject:
				object := *objects[itemIx]
				m := object.metrics
				lineAscent = max(lineAscent, m.baseline)
				lineDescent = max(lineDescent, m.size.Height-m.baseline)
				lineWidth += m.size.Width
				lineFragments = append(lineFragments, lineFragmentLayout{
					itemIx: itemIx, kind: positionedObject, object: &object, size: m.size,
					sourceRange: gpui.TextRange{Start: 0, End: inlineFlowImageLen}, baseline: m.baseline,
				})
			case inlineFlowImage:
				if lineRange.Start <= itemStart && itemEnd <= lineRange.End {
					if imageSizes[itemIx] == nil {
						panic("base: image size should be measured before layout")
					}
					size := *imageSizes[itemIx]
					lineWidth += size.Width
					baseline := min(max(size.Height/2+bodyBaseline-lineHeight/2, 0), size.Height)
					lineAscent = max(lineAscent, baseline)
					lineDescent = max(lineDescent, size.Height-baseline)
					lineFragments = append(lineFragments, lineFragmentLayout{
						itemIx: itemIx, kind: positionedImage, size: size,
						sourceRange: gpui.TextRange{Start: 0, End: inlineFlowImageLen}, baseline: baseline,
					})
				}
			}
			itemStart = itemEnd
		}

		x := float32(0)
		for _, fragment := range lineFragments {
			// Equal leading on both sides of a text run's glyph box, as much
			// as the line allows; negative where glyphs overflow the line.
			leading := float32(0)
			if fragment.kind == positionedText {
				leading = max(min(lineAscent-fragment.baseline, lineDescent-(fragment.size.Height-fragment.baseline)), -fragment.size.Height/2)
			}
			fragment.size.Height += leading * 2
			fragment.baseline += leading
			origin := gpui.Point{X: x, Y: y + lineAscent - fragment.baseline}
			selectionBounds := gpui.Bounds{Origin: gpui.Point{X: x, Y: y}, Size: gpui.Size{Width: fragment.size.Width, Height: lineAscent + lineDescent}}
			positioned := positionedFragment{
				kind: fragment.kind, itemIx: fragment.itemIx, origin: origin, size: fragment.size,
				object: fragment.object, selectionBounds: selectionBounds, sourceRange: fragment.sourceRange,
				fontSize: fragment.fontSize, text: fragment.text, links: fragment.links, highlights: fragment.highlights,
			}
			if cb := fragment.codeBackground; cb != nil {
				positioned.codeBackground = &textCodeBackground{
					bounds: gpui.Bounds{Origin: textPointAdd(textPointAdd(origin, gpui.Point{Y: leading}), cb.bounds.Origin), Size: cb.bounds.Size},
					color:  cb.color,
				}
			}
			x += fragment.size.Width
			fragments = append(fragments, positioned)
		}
		maxWidth = max(maxWidth, lineWidth)
		y += lineAscent + lineDescent
	}
	return &inlineFlowLayout{fragments: fragments, size: gpui.Size{Width: maxWidth, Height: y}, wrapWidth: wrapWidth}
}

func highlightsHaveScale(highlights []inlineHighlightRange) bool {
	for _, h := range highlights {
		if h.h.fontSizeScale != nil {
			return true
		}
	}
	return false
}

// inlineCodeBackground is the rounded box behind a code span, centered on
// the cap-height body of the text; it takes the background color out of
// the highlights so the text does not paint it again at the full line
// height (U:text/inline_flow.rs:1108-1145; cap height per the file
// comment).
func inlineCodeBackground(descent, fontSize float32, highlights []inlineHighlightRange, fragmentSize gpui.Size, baseline float32) *textCodeBackground {
	capHeight := fontSize * inlineCapHeightEm
	verticalPadding := fontSize*0.125 + descent/2
	var color *gpui.Hsla
	for i := range highlights {
		if color == nil && highlights[i].h.style.BackgroundColor != nil {
			c := *highlights[i].h.style.BackgroundColor
			color = &c
		}
		highlights[i].h.style.BackgroundColor = nil
	}
	if color == nil {
		return nil
	}
	return &textCodeBackground{
		bounds: gpui.Bounds{
			Origin: gpui.Point{X: 0, Y: baseline - capHeight - verticalPadding},
			Size:   gpui.Size{Width: fragmentSize.Width, Height: capHeight + verticalPadding*2},
		},
		color: *color,
	}
}

// inlineFlowLineRanges splits the flow at hard line breaks and wraps each
// hard line, re-wrapping tighter where the shaped spans overflow the width
// (U:text/inline_flow.rs:1147-1266).
func inlineFlowLineRanges(items []inlineFlowItem, imageSizes []*gpui.Size, objects []*measuredInlineObject, textStyle gpui.TextStyle, wrapWidth *float32, w *gpui.Window) []gpui.TextRange {
	totalLen := 0
	for i := range items {
		totalLen += items[i].measureLen()
	}
	var hardLines []gpui.TextRange
	lineStart, itemStart := 0, 0
	for i := range items {
		if items[i].kind == inlineFlowText {
			text := items[i].text
			for off := 0; ; {
				nl := strings.IndexByte(text[off:], '\n')
				if nl < 0 {
					break
				}
				newline := itemStart + off + nl
				hardLines = append(hardLines, gpui.TextRange{Start: lineStart, End: newline})
				lineStart = newline + 1
				off += nl + 1
			}
		}
		itemStart += items[i].measureLen()
	}
	hardLines = append(hardLines, gpui.TextRange{Start: lineStart, End: totalLen})
	if wrapWidth == nil {
		return hardLines
	}
	rem := gpui.RemSize(w)
	fontSize := textStyle.FontSizePixels(rem)
	wrapper, err := w.TextSystem().LineWrapper(textStyle.FontDescriptorOf(), fontSize)
	if err != nil {
		return hardLines
	}
	var ranges []gpui.TextRange
	for _, hardLine := range hardLines {
		itemStart := 0
		var wrapFragments []gpui.LineFragment
		for ix := range items {
			item := &items[ix]
			itemEnd := itemStart + item.measureLen()
			if itemEnd > hardLine.Start && itemStart < hardLine.End {
				switch item.kind {
				case inlineFlowText:
					start, end := max(hardLine.Start, itemStart)-itemStart, min(hardLine.End, itemEnd)-itemStart
					if start < end {
						wrapFragments = pushTextWrapFragments(wrapFragments, item.text, item.highlights, gpui.TextRange{Start: start, End: end}, textStyle, *wrapWidth, w)
					}
				case inlineFlowObject:
					wrapFragments = append(wrapFragments, gpui.LineFragmentElement(objects[ix].metrics.size.Width, inlineFlowImageLen))
				case inlineFlowImage:
					if hardLine.Start <= itemStart && itemEnd <= hardLine.End {
						if imageSizes[ix] == nil {
							panic("base: image size should be measured before wrapping")
						}
						wrapFragments = append(wrapFragments, gpui.LineFragmentElement(imageSizes[ix].Width, inlineFlowImageLen))
					}
				}
			}
			itemStart = itemEnd
		}

		// The wrapper sums per-character widths, which can be narrower than
		// the shaped run; check each line against the shaped spans and
		// re-wrap it tighter when it overflows.
		spans := shapedSpans(items, imageSizes, objects, textStyle, hardLine, w)
		start := hardLine.Start
		for start < hardLine.End {
			rest := fragmentsFrom(wrapFragments, start-hardLine.Start)
			budget := *wrapWidth
			end := nextWrap(wrapper, rest, budget, start, hardLine.End)
			overflow := spansWidth(spans, gpui.TextRange{Start: start, End: end}) - *wrapWidth
			step := overflow
			for overflow > 0 {
				budget -= max(step, 0.5)
				if budget <= 0 {
					break
				}
				tighter := nextWrap(wrapper, rest, budget, start, hardLine.End)
				if tighter < end {
					end = tighter
					overflow = spansWidth(spans, gpui.TextRange{Start: start, End: end}) - *wrapWidth
					step = overflow
				} else {
					step *= 2
				}
			}
			ranges = append(ranges, gpui.TextRange{Start: start, End: end})
			start = end
		}
		if hardLine.Start >= hardLine.End {
			ranges = append(ranges, hardLine)
		}
	}
	return ranges
}

// nextWrap is where the first line of fragments wraps at wrapWidth; end
// when it all fits (U:text/inline_flow.rs:1268-1281).
func nextWrap(wrapper *gpui.LineWrapper, fragments []gpui.LineFragment, wrapWidth float32, start, end int) int {
	boundaries := wrapper.WrapLine(fragments, wrapWidth)
	if len(boundaries) == 0 {
		return end
	}
	return min(start+boundaries[0].Ix, end)
}

// fragmentsFrom drops the first skip bytes of fragments
// (U:text/inline_flow.rs:1283-1311).
func fragmentsFrom(fragments []gpui.LineFragment, skip int) []gpui.LineFragment {
	offset := 0
	out := make([]gpui.LineFragment, 0, len(fragments))
	for _, f := range fragments {
		n := f.LenUTF8
		if f.Kind == gpui.LineFragmentKindText {
			n = len(f.Text)
		}
		if offset+n <= skip {
			offset += n
			continue
		}
		if f.Kind == gpui.LineFragmentKindText {
			out = append(out, gpui.LineFragmentText(f.Text[max(skip-offset, 0):]))
		} else {
			out = append(out, gpui.LineFragmentElement(f.Width, f.LenUTF8))
		}
		offset += n
	}
	return out
}

// shapedSpan is one run of a hard line as the renderer lays it out
// (U:text/inline_flow.rs:1313-1325): text with its caret table, or a fixed
// width. Ranges are hard-line offsets.
type shapedSpan struct {
	rng     gpui.TextRange
	fixed   bool
	width   float32
	line    *gpui.ShapedLine
	padding float32
}

// shapedSpans is U:text/inline_flow.rs:1327-1393.
func shapedSpans(items []inlineFlowItem, imageSizes []*gpui.Size, objects []*measuredInlineObject, textStyle gpui.TextStyle, hardLine gpui.TextRange, w *gpui.Window) []shapedSpan {
	fontSize := textStyle.FontSizePixels(gpui.RemSize(w))
	var spans []shapedSpan
	itemStart := 0
	for ix := range items {
		item := &items[ix]
		itemEnd := itemStart + item.measureLen()
		if itemEnd > hardLine.Start && itemStart < hardLine.End {
			switch item.kind {
			case inlineFlowText:
				localStart, localEnd := max(hardLine.Start, itemStart)-itemStart, min(hardLine.End, itemEnd)-itemStart
				for _, segment := range textSizeRanges(len(item.text), item.highlights) {
					start, end := max(localStart, segment.rng.Start), min(localEnd, segment.rng.End)
					if start >= end {
						continue
					}
					highlights := sliceHighlightRanges(item.highlights, start, end)
					runs := textRuns(end-start, textStyle, highlights)
					line, err := w.TextSystem().ShapeLine(item.text[start:end], fontSize*segment.scale, runs, nil)
					if err != nil {
						continue
					}
					padding := float32(0)
					if highlightsHaveScale(highlights) {
						padding = inlineCodePadding * 2
					}
					spans = append(spans, shapedSpan{rng: gpui.TextRange{Start: itemStart + start, End: itemStart + end}, line: line, padding: padding})
				}
			case inlineFlowObject:
				spans = append(spans, shapedSpan{rng: gpui.TextRange{Start: itemStart, End: itemEnd}, fixed: true, width: objects[ix].metrics.size.Width})
			case inlineFlowImage:
				if imageSizes[ix] != nil {
					spans = append(spans, shapedSpan{rng: gpui.TextRange{Start: itemStart, End: itemEnd}, fixed: true, width: imageSizes[ix].Width})
				}
			}
		}
		itemStart = itemEnd
	}
	return spans
}

// spansWidth is the painted width of r (U:text/inline_flow.rs:1395-1427).
func spansWidth(spans []shapedSpan, r gpui.TextRange) float32 {
	total := float32(0)
	for _, s := range spans {
		if s.fixed {
			if r.Start <= s.rng.Start && s.rng.End <= r.End {
				total += s.width
			}
			continue
		}
		start, end := max(r.Start, s.rng.Start), min(r.End, s.rng.End)
		if start >= end {
			continue
		}
		total += s.line.XForIndex(end-s.rng.Start) - s.line.XForIndex(start-s.rng.Start) + s.padding
	}
	return total
}

// pushTextWrapFragments appends the wrap fragments for r of text: a span in
// another font family enters the wrapper as measured elements, by word and
// then by grapheme when a word is wider than the line
// (U:text/inline_flow.rs:1429-1502).
func pushTextWrapFragments(fragments []gpui.LineFragment, text string, highlights []inlineHighlightRange, r gpui.TextRange, textStyle gpui.TextStyle, wrapWidth float32, w *gpui.Window) []gpui.LineFragment {
	fontSize := textStyle.FontSizePixels(gpui.RemSize(w))
	cursor := r.Start
	for _, hr := range highlights {
		if hr.h.fontFamily == nil {
			continue
		}
		start, end := max(hr.rng.Start, cursor), min(hr.rng.End, r.End)
		if start >= end {
			continue
		}
		if cursor < start {
			fragments = append(fragments, gpui.LineFragmentText(text[cursor:start]))
		}
		span := text[start:end]
		highlight := hr.h
		scale := float32(1)
		if highlight.fontSizeScale != nil {
			scale = *highlight.fontSizeScale
		}
		measure := func(s string) float32 {
			runs := textRuns(len(s), textStyle, []inlineHighlightRange{{gpui.TextRange{Start: 0, End: len(s)}, highlight}})
			width, _, _ := shapedLineMetrics(s, fontSize*scale, runs, w)
			return width
		}
		padding := float32(0)
		if highlight.fontSizeScale != nil {
			padding = inlineCodePadding * 2
		}
		if width := measure(span) + padding; width <= wrapWidth {
			fragments = append(fragments, gpui.LineFragmentElement(width, len(span)))
		} else {
			for word := range segment.SplitWordBounds(span) {
				if width := measure(word) + padding; width <= wrapWidth {
					fragments = append(fragments, gpui.LineFragmentElement(width, len(word)))
					continue
				}
				for g := range segment.Graphemes(word) {
					fragments = append(fragments, gpui.LineFragmentElement(measure(g)+padding, len(g)))
				}
			}
		}
		cursor = end
	}
	if cursor < r.End {
		fragments = append(fragments, gpui.LineFragmentText(text[cursor:r.End]))
	}
	return fragments
}

// measureImageSize is U:text/inline_flow.rs:1504-1521.
func measureImageSize(ix int, source gpui.ImageSource, width, height *gpui.DefiniteLength, lineHeight, rem float32, w *gpui.Window, app *gpui.App) gpui.Size {
	var intrinsic *gpui.Size
	if width == nil || height == nil {
		intrinsic = intrinsicImageSize(ix, source, width, height, w, app)
	}
	return imageSize(width, height, intrinsic, lineHeight, rem)
}

// intrinsicImageSize lays an img out at its min size
// (U:text/inline_flow.rs:1523-1545).
func intrinsicImageSize(ix int, source gpui.ImageSource, width, height *gpui.DefiniteLength, w *gpui.Window, app *gpui.App) *gpui.Size {
	image := gpui.Img(source).Flex().ID(gpui.IntegerElementID(uint64(ix))).ObjectFit(gpui.ObjectFitContain).MaxW(gpui.Fraction(1))
	if width != nil {
		image = image.W(*width)
	}
	if height != nil {
		image = image.H(*height)
	}
	el := image.IntoElement()
	measured := el.LayoutAsRoot(gpui.AvailableSize{Width: gpui.MinContentAvailableSpace(), Height: gpui.MinContentAvailableSpace()}, w, app)
	if measured.Width <= 0 || measured.Height <= 0 {
		return nil
	}
	return &measured
}

// definiteLengthToPixels is DefiniteLength::to_pixels(base, rem).
func definiteLengthToPixels(l gpui.DefiniteLength, base, rem float32) float32 {
	switch l.Kind {
	case gpui.DefiniteLengthPx:
		return l.Value
	case gpui.DefiniteLengthRem:
		return l.Value * rem
	}
	return l.Value * base
}

// imageSize resolves an image's size against the line height
// (U:text/inline_flow.rs:1547-1582).
func imageSize(width, height *gpui.DefiniteLength, intrinsic *gpui.Size, lineHeight, rem float32) gpui.Size {
	valid := intrinsic != nil && intrinsic.Width > 0 && intrinsic.Height > 0
	switch {
	case width != nil && height != nil:
		return gpui.Size{Width: definiteLengthToPixels(*width, lineHeight, rem), Height: definiteLengthToPixels(*height, lineHeight, rem)}
	case width != nil:
		wpx := definiteLengthToPixels(*width, lineHeight, rem)
		h := lineHeight
		if valid {
			h = wpx * (intrinsic.Height / intrinsic.Width)
		}
		return gpui.Size{Width: wpx, Height: h}
	case height != nil:
		hpx := definiteLengthToPixels(*height, lineHeight, rem)
		wpx := hpx
		if valid {
			wpx = hpx * (intrinsic.Width / intrinsic.Height)
		}
		return gpui.Size{Width: wpx, Height: hpx}
	}
	return inlineImageSizeForLine(intrinsic, lineHeight)
}

// inlineImageSizeForLine is U:text/inline_flow.rs:1584-1597.
func inlineImageSizeForLine(intrinsic *gpui.Size, lineHeight float32) gpui.Size {
	height := lineHeight * 0.75
	aspect := float32(1)
	if intrinsic != nil && intrinsic.Width > 0 && intrinsic.Height > 0 {
		aspect = intrinsic.Width / intrinsic.Height
	}
	return gpui.Size{Width: max(height*aspect, 1), Height: max(height, 1)}
}

// shapedLineMetrics shapes text and returns its width, ascent and descent
// (the shape_line call of U:text/inline_flow.rs:1599-1606).
func shapedLineMetrics(text string, fontSize float32, runs []gpui.TextRun, w *gpui.Window) (float32, float32, float32) {
	line, err := w.TextSystem().ShapeLine(text, fontSize, runs, nil)
	if err != nil {
		panic(fmt.Sprintf("base: InlineFlow shape_line(%q): %v", text, err))
	}
	return line.Width(), line.Ascent(), positiveDescent(line.Descent())
}

// positiveDescent is the shaped line's descent as the positive distance
// below the baseline that ShapedLine::descent is (U:text/inline_flow.rs:
// 1621-1622). The port's deterministic test text system reports the
// signed font metric (gpui/testwindow.go testShapedText.descent), so the
// magnitude is taken.
func positiveDescent(descent float32) float32 {
	if descent < 0 {
		return -descent
	}
	return descent
}

func shapedMetrics(text string, fontSize float32, runs []gpui.TextRun, w *gpui.Window) (float32, float32) {
	_, ascent, descent := shapedLineMetrics(text, fontSize, runs, w)
	return ascent, descent
}

// plainLineHeight is the line height of a plain line in textStyle, snapped
// like the text layout's (U:text/inline_flow.rs:1608-1617). Browser-parity
// D1: the style's CSS `normal` line height (the default) resolves
// through the window's text system (gpui.ResolveLineHeight); a definite
// length keeps the snapped LineHeightPixels derivation.
func plainLineHeight(textStyle gpui.TextStyle, rem float32, w *gpui.Window) float32 {
	if textStyle.LineHeight.Kind == gpui.DefiniteLengthNormal {
		return gpui.ResolveLineHeight(w, textStyle)
	}
	fontSize := textStyle.FontSizePixels(rem)
	return gpui.PixelSnap(w, textStyle.LineHeightPixels(fontSize, rem))
}

// centeredBaseline is where text painting puts the baseline in a box
// lineHeight tall (U:text/inline_flow.rs:1619-1625).
func centeredBaseline(ascent, descent, lineHeight float32) float32 {
	return (lineHeight-ascent-descent)/2 + ascent
}

// sliceHighlightRanges, sliceLinkRanges, sliceColorRanges and
// sliceFadeRanges are slice_ranges (U:text/inline_flow.rs:1627-1642) for
// each payload: the ranges overlapping [start, end), rebased to start.
func sliceHighlightRanges(ranges []inlineHighlightRange, start, end int) []inlineHighlightRange {
	var out []inlineHighlightRange
	for _, r := range ranges {
		if s, e := max(r.rng.Start, start), min(r.rng.End, end); s < e {
			out = append(out, inlineHighlightRange{gpui.TextRange{Start: s - start, End: e - start}, r.h})
		}
	}
	return out
}

func sliceLinkRanges(ranges []textLinkRange, start, end int) []textLinkRange {
	var out []textLinkRange
	for _, r := range ranges {
		if s, e := max(r.rng.Start, start), min(r.rng.End, end); s < e {
			out = append(out, textLinkRange{gpui.TextRange{Start: s - start, End: e - start}, r.link})
		}
	}
	return out
}

func sliceColorRanges(ranges []textColorRange, start, end int) []textColorRange {
	var out []textColorRange
	for _, r := range ranges {
		if s, e := max(r.rng.Start, start), min(r.rng.End, end); s < e {
			out = append(out, textColorRange{gpui.TextRange{Start: s - start, End: e - start}, r.color})
		}
	}
	return out
}

func sliceFadeRanges(ranges []textFadeRange, start, end int) []textFadeRange {
	var out []textFadeRange
	for _, r := range ranges {
		if s, e := max(r.rng.Start, start), min(r.rng.End, end); s < e {
			out = append(out, textFadeRange{gpui.TextRange{Start: s - start, End: e - start}, r.fadeOut})
		}
	}
	return out
}

// textLeafElementID is the element id of a text leaf
// (U:text/stream_fade.rs:108-118 From<TextLeafKey> for ElementId; the
// opaque bytes become a named integer id).
func textLeafElementID(key *textLeafKey) gpui.ElementID {
	if key == nil {
		return gpui.NameElementID("p")
	}
	return gpui.NamedIntegerElementID("leaf-"+strconv.Itoa(key.blockStart), uint64(key.ordinal))
}
