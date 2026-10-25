package base

// The code-editor paths of U:input/base/element.rs that the input element
// joins: the editor scrollbar (EditorScrollbar, :117-292), the gutter
// bounds (:307-323), decoration composition (:95-115), range decorations,
// search matches and the hover highlight (:746-861), inline-completion
// ghost lines (:1136-1221), fold icons (:1223-1406), the line numbers of
// prepaint (:2753-2808) and highlight_lines (:1989-2139).
//
// The ghost-line paint reads the inline completion item's insert text
// through InputExtras.inlineCompletionText (editor_lsp_completions.go
// keeps the item).

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
)

const (
	// foldIconWidth is FOLD_ICON_WIDTH (U:input/base/element.rs:52).
	foldIconWidth float32 = 14
	// maxHighlightLineLength is MAX_HIGHLIGHT_LINE_LENGTH
	// (U:input/base/element.rs:54).
	maxHighlightLineLength = 10_000
)

// foldChevronRightSVG / foldChevronDownSVG are U:input/base/element.rs:66-67.
var (
	foldChevronRightSVG = []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 18 6-6-6-6"/></svg>`)
	foldChevronDownSVG  = []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m6 9 6 6 6-6"/></svg>`)
)

// composeDecorationCollections applies the collections in reverse
// creation order so the first one is composed last and keeps its
// precedence (U:input/base/element.rs:95-115).
func composeDecorationCollections(styles []gpui.HighlightRange, collections [][]TextDecoration, visible gpui.TextRange) []gpui.HighlightRange {
	for i := len(collections) - 1; i >= 0; i-- {
		decorations := make([]gpui.HighlightRange, 0, len(collections[i]))
		for _, d := range collections[i] {
			decorations = append(decorations, gpui.HighlightRange{Range: gpui.TextRange{Start: d.Range.Start, End: d.Range.End}, Style: d.Style})
		}
		styles = composeDecorations(styles, decorations, visible)
	}
	if len(styles) == 0 {
		return nil
	}
	return styles
}

// editorScrollbarLayout is EditorScrollbarLayout: the scrollbar's
// viewport (the input past the gutter, padded) and content size
// (U:input/base/element.rs:117-121, 164-194).
type editorScrollbarLayout struct {
	bounds     gpui.Bounds
	scrollSize gpui.Size
}

func newEditorScrollbarLayout(inputBounds gpui.Bounds, lineNumberWidth float32, scrollSize gpui.Size, paddings gpui.Edges) editorScrollbarLayout {
	left := float32(0)
	if lineNumberWidth != 0 {
		left = paddings.Left + lineNumberWidth - lineNumberRightMargin
	}
	return editorScrollbarLayout{
		bounds: gpui.Bounds{
			Origin: gpui.Point{X: inputBounds.Origin.X + left, Y: inputBounds.Origin.Y - paddings.Top},
			Size: gpui.Size{
				Width:  inputBounds.Size.Width - left + paddings.Right,
				Height: inputBounds.Size.Height + paddings.Top + paddings.Bottom,
			},
		},
		scrollSize: gpui.Size{Width: scrollSize.Width - left + paddings.Right + inputRightMargin, Height: scrollSize.Height},
	}
}

// editorScrollbarSnapshot is EditorScrollbarSnapshot: what the last text
// prepaint left for the scrollbar (U:input/base/element.rs:136-162).
type editorScrollbarSnapshot struct {
	layout             editorScrollbarLayout
	cursorScrollOffset gpui.Point
	softWrap           bool
}

// editorGutterBounds is the whole fixed gutter column, padding included
// (U:input/base/element.rs:307-323).
func editorGutterBounds(inputBounds gpui.Bounds, lineNumberWidth, ghostLinesHeight float32, paddings gpui.Edges) gpui.Bounds {
	return gpui.Bounds{
		Origin: gpui.Point{X: inputBounds.Origin.X - paddings.Left, Y: inputBounds.Origin.Y - paddings.Top},
		Size: gpui.Size{
			Width:  lineNumberWidth + paddings.Left,
			Height: inputBounds.Size.Height + ghostLinesHeight + paddings.Top + paddings.Bottom,
		},
	}
}

// editorScrollbar is EditorScrollbar: an absolute overlay that lays the
// Scrollbar out over the snapshot the text element left
// (U:input/base/element.rs:196-292).
type editorScrollbar[M InputModeKind] struct {
	state gpui.Entity[InputBaseState[M]]
}

func newEditorScrollbar[M InputModeKind](state gpui.Entity[InputBaseState[M]]) gpui.AnyElement {
	return gpui.CustomElement[struct{}, *gpui.AnyElement](&editorScrollbar[M]{state: state})
}

// IntoElement implements gpui.IntoElement.
func (e *editorScrollbar[M]) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, *gpui.AnyElement](e)
}

// ID implements gpui.Element (U:input/base/element.rs:218-220).
func (e *editorScrollbar[M]) ID() (gpui.ElementID, bool) {
	return gpui.ElementIDOf("editor-scrollbar"), true
}

// SourceLocation implements gpui.Element.
func (e *editorScrollbar[M]) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout covers the input absolutely (U:input/base/element.rs:226-239).
func (e *editorScrollbar[M]) RequestLayout(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, _ *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Position = gpui.PositionAbsolute
	style.Size.Width = gpui.DefiniteLengthOf(gpui.Fraction(1))
	style.Size.Height = gpui.DefiniteLengthOf(gpui.Fraction(1))
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic("base: EditorScrollbar request_layout: " + err.Error())
	}
	return id, struct{}{}
}

// Prepaint syncs the cursor scroll offset and lays the scrollbar out at
// the snapshot's viewport (U:input/base/element.rs:241-276).
func (e *editorScrollbar[M]) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *struct{}, w *gpui.Window, app *gpui.App) *gpui.AnyElement {
	var snapshot *editorScrollbarSnapshot
	var handle gpui.ScrollHandle
	e.state.Read(app, func(s *InputBaseState[M], _ *gpui.App) struct{} {
		snapshot = s.editorScrollbarSnapshot
		handle = s.scrollHandle
		return struct{}{}
	})
	if snapshot == nil {
		return nil
	}
	if handle.Offset() != snapshot.cursorScrollOffset {
		handle.SetOffset(snapshot.cursorScrollOffset)
	}
	var bar *Scrollbar
	if !snapshot.softWrap {
		bar = NewScrollbar(handle)
	} else {
		bar = ScrollbarVertical(handle)
	}
	element := bar.ViewportBounds(snapshot.layout.bounds).ScrollSize(snapshot.layout.scrollSize).IntoElement()
	size := snapshot.layout.bounds.Size
	element.PrepaintAsRoot(snapshot.layout.bounds.Origin, gpui.AvailableSize{
		Width:  gpui.DefiniteAvailableSpace(size.Width),
		Height: gpui.DefiniteAvailableSpace(size.Height),
	}, w, app)
	return &element
}

// Paint is U:input/base/element.rs:278-291.
func (e *editorScrollbar[M]) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *struct{}, prepaint **gpui.AnyElement, w *gpui.Window, app *gpui.App) {
	if *prepaint != nil {
		(*prepaint).Paint(w, app)
	}
}

// layoutRangeDecorations builds the fills and frames of the geometric
// decorations over the visible, non-folded spans
// (U:input/base/element.rs:746-815).
func layoutRangeDecorations[M InputModeKind](state *InputBaseState[M], layout *lastLayout, bounds gpui.Bounds, scaleFactor float32, contentMask gpui.Bounds) (fills, frames []coloredPath) {
	// Query separate buffer spans across folds instead of the hidden text
	// between the first and last visible offsets.
	var spans []InputSelection
	for i, offset := range layout.visibleLineByteOffsets {
		if i >= len(layout.lines) {
			break
		}
		end := min(offset+layout.lines[i].len()+1, layout.visibleRangeOffset.End)
		if n := len(spans); n > 0 && spans[n-1].End == offset {
			spans[n-1].End = end
		} else if offset < end {
			spans = append(spans, NewInputSelection(offset, end))
		}
	}
	for _, d := range state.extras.rangeDecorations(spans) {
		switch d.Style() {
		case RangeDecorationFill:
			color, ok := d.Color()
			if !ok {
				color = state.editorStyle.Foreground.Opacity(0.12)
			}
			if path, ok := layoutMatchRange(d.rng.Start, d.rng.End, layout, bounds); ok {
				fills = append(fills, coloredPath{path, color})
			}
		case RangeDecorationFrame:
			color, ok := d.Color()
			if !ok {
				color = state.editorStyle.Foreground
			}
			corners := layoutRangeCorners(d.rng.Start, d.rng.End, layout)
			if len(corners) == 0 {
				continue
			}
			ox, oy := bounds.Origin.X+layout.lineNumberWidth, bounds.Origin.Y
			points := frameOutlinePoints(padFrameCorners(corners, 1))
			for i := range points {
				points[i] = gpui.Point{X: ox + points[i].X, Y: oy + points[i].Y}
			}
			strokeWidth, snapped := snapFrameOutline(points, 1, scaleFactor)
			snapped = clampFrameToContentMask(snapped, strokeWidth, contentMask)
			if len(snapped) == 0 {
				continue
			}
			b := gpui.StrokePathBuilder(strokeWidth).MoveTo(snapped[0].X, snapped[0].Y)
			for _, p := range snapped[1:] {
				b.LineTo(p.X, p.Y)
			}
			b.Close()
			if path, err := b.Build(); err == nil {
				frames = append(frames, coloredPath{path, color})
			}
		}
	}
	return fills, frames
}

// searchMatchPath is one laid-out match and whether it is the current one.
type searchMatchPath struct {
	path   *gpui.Path
	active bool
}

// layoutSearchMatches lays out the matches reaching into the visible
// range while a search is active (U:input/base/element.rs:817-847).
func layoutSearchMatches[M InputModeKind](state *InputBaseState[M], layout *lastLayout, bounds gpui.Bounds) []searchMatchPath {
	if !state.searchSession.IsActive() {
		return nil
	}
	ranges := state.searchSession.Matcher.MatchedRanges()
	current := state.searchSession.Matcher.CurrentMatchIndex()
	visible := layout.visibleRangeOffset
	first := sort.Search(len(ranges), func(i int) bool { return ranges[i].End > visible.Start })
	var paths []searchMatchPath
	for i := first; i < len(ranges) && ranges[i].Start < visible.End; i++ {
		if path, ok := layoutMatchRange(ranges[i].Start, ranges[i].End, layout, bounds); ok {
			paths = append(paths, searchMatchPath{path: path, active: current == i})
		}
	}
	return paths
}

// layoutHoverHighlight is U:input/base/element.rs:849-861.
func layoutHoverHighlight[M InputModeKind](state *InputBaseState[M], layout *lastLayout, bounds gpui.Bounds) *gpui.Path {
	rng, ok := state.extras.hoverSymbolRange()
	if !ok {
		return nil
	}
	if path, ok := layoutMatchRange(rng.Start, rng.End, layout, bounds); ok {
		return path
	}
	return nil
}

// layoutInlineCompletion shapes the inline completion's first line (after
// the cursor) and its later lines (ghost lines shifting content down)
// (U:input/base/element.rs:1136-1221).
func layoutInlineCompletion[M InputModeKind](state *InputBaseState[M], visibleRange InputSelection, fontSize float32, w *gpui.Window) (*gpui.ShapedLine, []*gpui.ShapedLine) {
	if !state.focusHandle.IsFocused(w) {
		return nil, nil
	}
	text, ok := state.extras.inlineCompletionText()
	if !ok {
		return nil, nil
	}
	cursorRow := int(state.CursorPosition().Line)
	if cursorRow < visibleRange.Start || cursorRow >= visibleRange.End {
		return nil, nil
	}
	color := state.editorStyle.MutedForeground.Opacity(0.5)
	font := gpui.WindowTextStyle(w).FontDescriptorOf()
	lines := strings.Split(text, "\n")
	// Hard tabs draw as blank columns (input_tab_map.go): the first line
	// continues the cursor's column, the ghost lines start a line.
	tabs := state.displayTabs(w)
	var first *gpui.ShapedLine
	if lines[0] != "" {
		lineStart := state.ext().LineStartOffset(cursorRow)
		col := tabs.columnAfter(state.text.Slice(lineStart, max(state.Cursor(), lineStart)).String(), 0)
		first = tabs.shapeLine(w, lines[0], col, fontSize, []gpui.TextRun{{Len: len(lines[0]), Font: font, Color: color}})
	}
	ghost := make([]*gpui.ShapedLine, 0, len(lines)-1)
	for _, line := range lines[1:] {
		// A space gives an empty line its height.
		if line == "" {
			line = " "
		}
		ghost = append(ghost, tabs.shapeLine(w, line, 0, fontSize, []gpui.TextRun{{Len: len(line), Font: font, Color: color}}))
	}
	return first, ghost
}

// foldIcon is one prepainted fold icon.
type foldIcon struct {
	displayRow int
	folded     bool
	element    gpui.AnyElement
}

// foldIconLayout is FoldIconLayout: the gutter hitbox and the icons
// (U:input/base/element.rs:1228-1255).
type foldIconLayout struct {
	lineNumberHitbox gpui.Hitbox
	icons            []foldIcon
}

// layoutFoldIcons inserts the gutter hitbox and prepaints a fold icon for
// every visible fold candidate, fixed in the gutter during horizontal
// scroll (U:input/base/element.rs:1223-1369).
func (e *textElement[M]) layoutFoldIcons(originX float32, bounds gpui.Bounds, layout *lastLayout, w *gpui.Window, app *gpui.App) foldIconLayout {
	icons := foldIconLayout{lineNumberHitbox: w.InsertHitbox(gpui.Bounds{
		Origin: gpui.Point{X: originX, Y: bounds.Origin.Y + layout.visibleTop},
		Size:   gpui.Size{Width: layout.lineNumberWidth, Height: bounds.Size.Height},
	}, gpui.HitboxNormal)}

	type foldInfo struct {
		bufferLine int
		folded     bool
		offsetY    float32
	}
	state := e.read(app)
	if !state.mode.isFolding() {
		return icons
	}
	var infos []foldInfo
	offsetY := layout.visibleTop
	for i, bufferLine := range layout.visibleBufferLines {
		if i >= len(layout.lines) {
			break
		}
		if state.displayMap.isFoldCandidate(bufferLine) {
			infos = append(infos, foldInfo{bufferLine, state.displayMap.isFoldedAt(bufferLine), offsetY})
		}
		offsetY += float32(len(layout.lines[i].wrappedLines)) * layout.lineHeight
	}

	lineHeight := layout.lineHeight
	lineNumberWidth := layout.lineNumberWidth - lineNumberRightMargin - foldIconHitboxWidth
	relX, relY := (foldIconHitboxWidth-foldIconWidth)/2, (lineHeight-foldIconWidth)/2
	renderer := state.editorStyle.FoldIconRenderer
	for ix, info := range infos {
		origin := gpui.Point{X: originX + relX + lineNumberWidth, Y: bounds.Origin.Y + relY + info.offsetY}
		var child gpui.AnyElement
		if renderer != nil {
			child = renderer(ix, info.folded)
		} else {
			path, svg := "input-fold-chevron-down", foldChevronDownSVG
			if info.folded {
				path, svg = "input-fold-chevron-right", foldChevronRightSVG
			}
			child = gpui.Canvas(func(gpui.Bounds, *gpui.Window, *gpui.App) struct{} { return struct{}{} },
				func(b gpui.Bounds, _ struct{}, w *gpui.Window, app *gpui.App) {
					_ = gpui.PaintSvg(w, app, b, path, svg, gpui.UnitTransformation(), gpui.WindowTextStyle(w).Color)
				}).Flex().Size(gpui.Px(foldIconWidth)).IntoElement()
		}
		stateEntity := e.state
		bufferLine := info.bufferLine
		icon := gpui.Div().Flex().ID(gpui.ElementIDOf(fmt.Sprintf("fold-%d", ix))).
			Size(gpui.Px(foldIconWidth)).
			Child(child).
			OnMouseDown(gpui.MouseButtonLeft, func(_ *gpui.MouseDownEvent, _ *gpui.Window, app *gpui.App) {
				app.StopPropagation()
				stateEntity.Update(app, func(s *InputBaseState[M], cx *gpui.Context[InputBaseState[M]]) {
					s.displayMap.toggleFold(bufferLine)
					cx.Notify()
				})
			}).IntoElement()
		icon.PrepaintAsRoot(origin, gpui.AvailableSize{
			Width:  gpui.DefiniteAvailableSpace(foldIconHitboxWidth),
			Height: gpui.DefiniteAvailableSpace(lineHeight),
		}, w, app)
		icons.icons = append(icons.icons, foldIcon{displayRow: info.bufferLine, folded: info.folded, element: icon})
	}
	return icons
}

// paintFoldIcons paints the icons shown on gutter hover, on the current
// line, or folded; the gutter repaints when the pointer crosses it
// (U:input/base/element.rs:1371-1406).
func (e *textElement[M]) paintFoldIcons(layout *foldIconLayout, currentRow int, hasCurrentRow bool, w *gpui.Window, app *gpui.App) {
	isHovered := layout.lineNumberHitbox.IsHovered(w)
	if len(layout.icons) > 0 {
		hitbox := layout.lineNumberHitbox
		entityID := e.state.EntityID()
		w.OnMouseEvent(func(event any, phase gpui.DispatchPhase, w *gpui.Window, app *gpui.App) {
			if _, ok := event.(*gpui.MouseMoveEvent); ok && phase == gpui.DispatchBubble && hitbox.IsHovered(w) != isHovered {
				app.Notify(entityID)
			}
		})
	}
	for _, icon := range layout.icons {
		isCurrentLine := hasCurrentRow && currentRow == icon.displayRow
		if !isHovered && !isCurrentLine && !icon.folded {
			continue
		}
		icon.element.Paint(w, app)
	}
}

// lineNumberLayout is one buffer line's number and its visual row count
// (the SmallVec of one shaped number and empty continuation lines).
type lineNumberLayout struct {
	number *gpui.ShapedLine
	rows   int
}

// layoutLineNumbers shapes the visible line numbers, the current one in
// the foreground (U:input/base/element.rs:2753-2808).
func layoutLineNumberLines[M InputModeKind](state *InputBaseState[M], layout *lastLayout, digits int, textSize float32, font gpui.FontDescriptor, currentRow int, hasCurrentRow bool, w *gpui.Window) []lineNumberLayout {
	if !state.mode.showLineNumber() {
		return nil
	}
	out := make([]lineNumberLayout, 0, len(layout.visibleBufferLines))
	for i, bufferLine := range layout.visibleBufferLines {
		if i >= len(layout.lines) {
			break
		}
		color := state.editorStyle.MutedForeground
		if hasCurrentRow && currentRow == bufferLine {
			color = state.editorStyle.Foreground
		}
		text := fmt.Sprintf("%*d", digits, displayedLineNumber(bufferLine+1))
		shaped := mustShapeLine(w, text, textSize, []gpui.TextRun{{Len: len(text), Font: font, Color: color}})
		out = append(out, lineNumberLayout{number: shaped, rows: max(len(layout.lines[i].wrappedLines), 1)})
	}
	return out
}
