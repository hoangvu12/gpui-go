package base

// Port of U:text/inline_object.rs (ticket 28): the measured geometry of an
// inline plugin object and the InlineObject element, an atomic selection
// wrapper that leaves the child's styling and interaction to GPUI.
//
// Go adaptations (recorded deviations):
//   - gpui's accessibility Node carries Role, Value and Hidden only (no
//     label, no read-only flag; gpui/element.go Node), so WriteA11yInfo
//     writes the accessible name as the Value.
//   - The window drag selection (selection_points) and the adapter's
//     register_inline are ticket 29's hooks (text_state.go).

import (
	"fmt"
	"math"
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
)

// inlineMetrics is an object's size and baseline
// (U:text/inline_object.rs:19-38).
type inlineMetrics struct {
	size     gpui.Size
	baseline float32
}

// isValid is U:text/inline_object.rs:25-37.
func (m inlineMetrics) isValid() bool {
	finite := func(v float32) bool { return !math.IsInf(float64(v), 0) && !math.IsNaN(float64(v)) }
	return finite(m.size.Width) && finite(m.size.Height) && finite(m.baseline) &&
		m.size.Width > 0 && m.size.Height > 0 && m.baseline >= 0 && m.baseline <= m.size.Height
}

// inlineObjectContent is the frame's element, consumed once
// (Rc<RefCell<Option<AnyElement>>>).
type inlineObjectContent struct {
	element *gpui.AnyElement
}

// measuredInlineObject is an object's geometry, cloned during wrapping
// (U:text/inline_object.rs:40-48).
type measuredInlineObject struct {
	metrics   inlineMetrics
	content   *inlineObjectContent
	text      string
	fontSize  float32
	textStyle gpui.TextStyle
}

// measureInlineObject measures an object; it must run before a measured
// layout callback (U:text/inline_object.rs:51-105). The text fallback is
// the object's text shaped in style; a valid presentation replaces it.
func measureInlineObject(text string, presentation *InlineElement, style gpui.TextStyle, w *gpui.Window, app *gpui.App) measuredInlineObject {
	rem := gpui.RemSize(w)
	fontSize := style.FontSizePixels(rem)
	text = strings.NewReplacer("\r", " ", "\n", " ").Replace(text)
	var width, ascent, descent float32
	if line, err := w.TextSystem().ShapeLine(text, fontSize, []gpui.TextRun{style.ToRun(len(text))}, nil); err == nil {
		width, ascent, descent = line.Width(), line.Ascent(), positiveDescent(line.Descent())
	}
	height := max(gpui.ResolveLineHeight(w, style), ascent+descent)
	fallback := inlineMetrics{
		size:     gpui.Size{Width: max(width, 1), Height: height},
		baseline: (height-ascent-descent)/2 + ascent,
	}
	result := measuredInlineObject{metrics: fallback, text: text, fontSize: fontSize, textStyle: style}
	if presentation != nil {
		// The wrapper carries inherited marks into both layout and painting.
		wrapper := gpui.Div().Flex().Child(presentation.element)
		*wrapper.TextStyleMut() = textStyleSubtractDefault(style)
		element := wrapper.IntoElement()
		measured := element.LayoutAsRoot(gpui.AvailableSize{Width: gpui.MaxContentAvailableSpace(), Height: gpui.MaxContentAvailableSpace()}, w, app)
		baseline := max(measured.Height-(fallback.size.Height-fallback.baseline), 0)
		if presentation.baseline != nil {
			baseline = *presentation.baseline
		}
		metrics := inlineMetrics{size: measured, baseline: baseline}
		if metrics.isValid() {
			result.metrics = metrics
			result.content = &inlineObjectContent{element: &element}
		}
	}
	return result
}

// fitText fits the plain-text fallback into width; custom elements keep
// their real size (U:text/inline_object.rs:107-122).
func (o measuredInlineObject) fitText(width *float32) measuredInlineObject {
	if o.content == nil {
		scale := float32(1)
		if width != nil {
			scale = min(max(*width, 0)/o.metrics.size.Width, 1)
		}
		o.metrics.size = gpui.Size{Width: o.metrics.size.Width * scale, Height: o.metrics.size.Height * scale}
		o.metrics.baseline *= scale
		o.fontSize *= scale
	}
	return o
}

// element is the frame's content element and whether it was measured
// (U:text/inline_object.rs:124-148).
func (o measuredInlineObject) element() (gpui.AnyElement, bool) {
	if o.content != nil {
		el := o.content.element
		if el == nil {
			panic("base: inline element painted once per frame")
		}
		o.content.element = nil
		return *el, true
	}
	text := gpui.NewStyledText(o.text).WithRuns([]gpui.TextRun{o.textStyle.ToRun(len(o.text))})
	return gpui.Div().Flex().
		W(gpui.Px(o.metrics.size.Width)).
		H(gpui.Px(o.metrics.size.Height)).
		TextSize(gpui.Px(o.fontSize)).
		LineHeight(gpui.Px(o.metrics.size.Height)).
		// CSS pre's pair: the object's label renders verbatim without
		// wrapping (browser-core 38: nowrap would collapse it).
		WhitespacePre().
		OverflowHidden().
		Child(text).
		IntoElement(), false
}

// inlineObject is the atomic selection wrapper
// (U:text/inline_object.rs:151-164).
type inlineObject struct {
	id                 gpui.ElementID
	text               string
	accessibilityLabel string
	object             measuredInlineObject
	selected           *sharedFlag
	selectionBounds    gpui.Bounds
	lineBounds         gpui.Bounds
	content            gpui.AnyElement
	contentMeasured    bool
	link               *LinkMark
	linkClickHandler   LinkClickHandlerFn
}

// newInlineObject is InlineObject::new (U:text/inline_object.rs:177-201).
func newInlineObject(id gpui.ElementID, text, accessibilityLabel string, object measuredInlineObject, selected *sharedFlag, selectionBounds, lineBounds gpui.Bounds) *inlineObject {
	content, measured := object.element()
	return &inlineObject{
		id: id, text: text, accessibilityLabel: accessibilityLabel, object: object, selected: selected,
		selectionBounds: selectionBounds, lineBounds: lineBounds, content: content, contentMeasured: measured,
	}
}

// withLink is InlineObject::link (U:text/inline_object.rs:167-175).
func (o *inlineObject) withLink(link *LinkMark, handler LinkClickHandlerFn) *inlineObject {
	o.link, o.linkClickHandler = link, handler
	return o
}

// IntoElement implements gpui.IntoElement.
func (o *inlineObject) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, gpui.Hitbox](o)
}

// ID implements gpui.Element (U:text/inline_object.rs:213-215).
func (o *inlineObject) ID() (gpui.ElementID, bool) { return o.id, true }

// SourceLocation implements gpui.Element.
func (o *inlineObject) SourceLocation() *gpui.SourceLocation { return nil }

// A11yRole is GenericContainer (U:text/inline_object.rs:220-222).
func (o *inlineObject) A11yRole() (gpui.Role, bool) { return gpui.RoleGenericContainer, true }

// WriteA11yInfo writes the role and the accessible name
// (U:text/inline_object.rs:224-228; the name goes in Value, see the file
// comment).
func (o *inlineObject) WriteA11yInfo(node *gpui.Node) {
	node.Role = gpui.RoleGenericContainer
	node.Value = o.accessibilityLabel
}

// RequestLayout requests the measured size (U:text/inline_object.rs:230-249).
func (o *inlineObject) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, _ *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size.Width = gpui.PxLength(o.object.metrics.size.Width)
	style.Size.Height = gpui.PxLength(o.object.metrics.size.Height)
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("base: InlineObject request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint lays the content out at the object's bounds and reports its line
// span (U:text/inline_object.rs:251-287).
func (o *inlineObject) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, _ *struct{}, w *gpui.Window, app *gpui.App) gpui.Hitbox {
	hitbox := w.InsertHitbox(bounds, gpui.HitboxNormal)
	if o.contentMeasured {
		o.content.PrepaintAt(bounds.Origin, w, app)
	} else {
		o.content.PrepaintAsRoot(bounds.Origin, gpui.AvailableSize{
			Width:  gpui.DefiniteAvailableSpace(bounds.Size.Width),
			Height: gpui.DefiniteAvailableSpace(bounds.Size.Height),
		}, w, app)
	}
	textViewReportLineSpan(app, textLineSpan{top: bounds.Top(), bottom: bounds.Bottom(), lineHeight: bounds.Size.Height})
	return hitbox
}

// Paint paints the selection, registers the link and multi-click
// listeners and paints the content (U:text/inline_object.rs:289-407).
func (o *inlineObject) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, bounds gpui.Bounds, _ *struct{}, hitbox *gpui.Hitbox, w *gpui.Window, app *gpui.App) {
	view, hasView := textViewStateCurrent(app)
	selectable, selected := false, false
	var selectionColor gpui.Hsla
	if hasView {
		view.Read(app, func(s *TextViewState, app *gpui.App) struct{} {
			selectable = s.isSelectable()
			selectionColor = s.textViewStyle.Selection()
			switch {
			case selectable && s.preserveInlineSelection && !s.isAllSelected():
				selected = o.selected.get()
			case !selectable:
			case s.isAllSelected():
				selected = true
			default:
				if m := s.multiClickSelection(); m != nil {
					if m.lineBounds != nil {
						selected = m.lineBounds.Contains(bounds.Center())
					} else {
						selected = bounds.Contains(m.pos)
					}
				}
				if !selected {
					if start, end, ok := s.selectionPoints(app); ok {
						selected = pointInTextSelection(o.selectionBounds.Origin, o.selectionBounds.Size.Width, start, end, o.selectionBounds.Size.Height)
					}
				}
			}
			return struct{}{}
		})
	}
	o.selected.set(selected)
	if selected {
		w.PaintQuad(gpui.Fill(bounds, gpui.SolidBackground(selectionColor)))
	}
	if o.link != nil {
		w.SetCursorStyle(gpui.CursorPointingHand, hitbox)
		box := *hitbox
		url := o.link.URL
		handler := o.linkClickHandler
		w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
			ev, ok := event.(*gpui.MouseUpEvent)
			if !ok || phase != gpui.DispatchBubble || !box.IsHovered(w) {
				return
			}
			if hasView && view.Read(app, func(s *TextViewState, app *gpui.App) bool { return s.hasSelection(app) }) {
				return
			}
			textSelectionEndHook(w, app)
			app.StopPropagation()
			click := gpui.ClickEvent{Kind: gpui.ClickEventMouse, Mouse: gpui.MouseClickEvent{
				Down: gpui.MouseDownEvent{Button: ev.Button, Position: ev.Position, Modifiers: ev.Modifiers, ClickCount: ev.ClickCount},
				Up:   *ev,
			}}
			handleLinkClick(handler, url, click, w, app)
		})
	}
	if selectable {
		if o.link == nil {
			w.SetCursorStyle(gpui.CursorIBeam, hitbox)
		}
		visible := textBoundsIntersect(bounds, gpui.ContentMask(w))
		if visible.Size.Width > 0 && visible.Size.Height > 0 && textSelectionRegisterInline != nil {
			textSelectionRegisterInline(view, []gpui.Bounds{visible}, "", nil, bounds, app)
		}
		box := *hitbox
		selectedState := o.selected
		text := o.text
		viewID := currentView(w)
		lineBounds := o.lineBounds
		w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
			ev, ok := event.(*gpui.MouseDownEvent)
			if !ok || phase != gpui.DispatchBubble || !box.IsHovered(w) || ev.Button != gpui.MouseButtonLeft ||
				ev.ClickCount < 2 || ev.ClickCount > 3 {
				return
			}
			GlobalStateSuppressTextSelection(app)
			selectedState.set(true)
			view.Update(app, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
				if ev.ClickCount == 3 {
					s.setMultiClickLine(lineBounds, cx.App())
				} else {
					s.setMultiClickSelection(ev.Position, textViewMultiClickWord, text, cx.App())
				}
			})
			app.Notify(viewID)
		})
	}
	o.content.Paint(w, app)
}
