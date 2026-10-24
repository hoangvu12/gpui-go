package base

// Port of U:selectable_text.rs: plain text that participates in the
// window-scoped TextSelection (text_selection.go).
//
// Go adaptation: a run without an explicit handle keeps its participant in
// a keyed entity under its element id (Window.UseKeyedState), which the
// window releases when the run stops drawing — the retained
// with_element_state handle of U:selectable_text.rs:157-166.

import "github.com/hoangvu12/gpui-go/gpui"

// SelectableText is plain text that participates in the window-scoped
// TextSelection (U:selectable_text.rs:11-27). Use NewSelectableText for an
// independent run, or SelectableTextWithHandle when several elements form
// one selectable document. The application renders one TextSelectionLayer
// above its content and calls Init during startup.
type SelectableText struct {
	id             gpui.ElementID
	handle         *TextSelectionHandle
	text           string
	styledText     *gpui.StyledText
	documentOrder  uint64
	textStyle      *gpui.TextStyleRefinement
	selectionColor *gpui.Hsla
}

// NewSelectableText creates a run that owns its own selection
// (U:selectable_text.rs:30-33).
func NewSelectableText[I gpui.IntoElementID](id I, text string) *SelectableText {
	return buildSelectableText(gpui.ElementIDOf(id), nil, text)
}

// SelectableTextWithHandle creates a run that joins the document handle
// belongs to (U:selectable_text.rs:35-42).
func SelectableTextWithHandle[I gpui.IntoElementID](id I, handle TextSelectionHandle, text string) *SelectableText {
	return buildSelectableText(gpui.ElementIDOf(id), &handle, text)
}

// buildSelectableText is SelectableText::build (U:selectable_text.rs:44-54).
func buildSelectableText(id gpui.ElementID, handle *TextSelectionHandle, text string) *SelectableText {
	return &SelectableText{id: id, handle: handle, text: text, styledText: gpui.NewStyledText(text)}
}

// DocumentOrder places this run in reading order among the others sharing
// its handle (U:selectable_text.rs:56-60).
func (t *SelectableText) DocumentOrder(order uint64) *SelectableText {
	t.documentOrder = order
	return t
}

// TextStyle sets the text style the run is laid out and painted with
// (U:selectable_text.rs:62-66).
func (t *SelectableText) TextStyle(style gpui.TextStyleRefinement) *SelectableText {
	t.textStyle = &style
	return t
}

// SelectionColor overrides the selection background, which defaults to the
// system's selection wash (browser-core 28, system_selection.go — the
// ::selection UA default) (U:selectable_text.rs:68-73, diverged toward
// the browser).
func (t *SelectableText) SelectionColor(color gpui.Hsla) *SelectableText {
	t.selectionColor = &color
	return t
}

// paintSelectableTextSelection paints the selection background of r
// (U:selectable_text.rs:75-97).
func paintSelectableTextSelection(layout *gpui.TextLayout, r gpui.TextRange, color gpui.Hsla, w *gpui.Window) {
	start, ok := layout.PositionForIndex(r.Start)
	if !ok {
		return
	}
	end, ok := layout.PositionForIndex(r.End)
	if !ok {
		return
	}
	for _, bounds := range selectionQuadBounds(start, end, layout.Bounds(), layout.LineHeight()) {
		w.PaintQuad(gpui.Fill(bounds, gpui.SolidBackground(color)))
	}
}

// selectionQuadBounds is the quads a selection from start to end covers:
// the rest of the first row, the full width of the rows between, and the
// last row up to end (U:selectable_text.rs:100-128).
func selectionQuadBounds(start, end gpui.Point, bounds gpui.Bounds, lineHeight float32) []gpui.Bounds {
	if start.Y == end.Y {
		return []gpui.Bounds{textBoundsFromCorners(start, gpui.Point{X: end.X, Y: end.Y + lineHeight})}
	}
	quads := []gpui.Bounds{textBoundsFromCorners(start, gpui.Point{X: bounds.Right(), Y: start.Y + lineHeight})}
	if end.Y > start.Y+lineHeight {
		quads = append(quads, textBoundsFromCorners(gpui.Point{X: bounds.Left(), Y: start.Y + lineHeight}, gpui.Point{X: bounds.Right(), Y: end.Y}))
	}
	return append(quads, textBoundsFromCorners(gpui.Point{X: bounds.Left(), Y: end.Y}, gpui.Point{X: end.X, Y: end.Y + lineHeight}))
}

// IntoElement implements gpui.IntoElement.
func (t *SelectableText) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[TextSelectionHandle, gpui.Hitbox](t)
}

// ID implements gpui.Element (U:selectable_text.rs:142-144).
func (t *SelectableText) ID() (gpui.ElementID, bool) { return t.id, true }

// SourceLocation implements gpui.Element.
func (t *SelectableText) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout resolves the participant and lays the text out
// (U:selectable_text.rs:150-177).
func (t *SelectableText) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, TextSelectionHandle) {
	var handle TextSelectionHandle
	if t.handle != nil {
		handle = *t.handle
	} else {
		text := t.text
		entity := w.UseKeyedState(gpui.NameElementID("selection"), app, func(_ *gpui.Window, cx *gpui.Context[selectableTextState]) selectableTextState {
			return selectableTextState{fallbackCopyText: text}
		})
		handle = TextSelectionHandle{entity: entity}
	}
	var id gpui.LayoutID
	if t.textStyle != nil {
		id = gpui.WithTextStyle(w, t.textStyle, func(w *gpui.Window) gpui.LayoutID {
			id, _ := t.styledText.RequestLayout(global, inspector, w, app)
			return id
		})
	} else {
		id, _ = t.styledText.RequestLayout(global, inspector, w, app)
	}
	return id, handle
}

// Prepaint lays the text out and registers the run for the frame
// (U:selectable_text.rs:179-197).
func (t *SelectableText) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, handle *TextSelectionHandle, w *gpui.Window, app *gpui.App) gpui.Hitbox {
	t.styledText.Prepaint(global, inspector, bounds, &struct{}{}, w, app)
	hitbox := w.InsertHitbox(bounds, gpui.HitboxNormal)
	registration := NewTextSelectionRegistration(hitbox, bounds).
		WithDocumentOrder(t.documentOrder).
		WithTextBounds([]gpui.Bounds{bounds}).
		WithRenderedElement(*handle, w, app)
	handle.Register(registration, w, app)
	return hitbox
}

// Paint projects the selection onto the run, paints its background and
// the text (U:selectable_text.rs:199-236).
func (t *SelectableText) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, handle *TextSelectionHandle, _ *gpui.Hitbox, w *gpui.Window, app *gpui.App) {
	layout := t.styledText.Layout()
	selectedBefore := TextSelectionSelectedText(w, app)
	projection := handle.UpdateRuns([]TextSelectionRun{
		NewTextSelectionRun(t.text, layout, bounds).WithDocumentOrder(t.documentOrder),
	}, app)
	if selectedBefore != TextSelectionSelectedText(w, app) {
		w.Refresh()
	}
	// The selection wash comes from the system by default (browser-core
	// 28, system_selection.go) — the ::selection UA default — with
	// SelectionColor as the app's explicit ::selection styling.
	color := systemSelectionColors(app).Background
	if t.selectionColor != nil {
		color = *t.selectionColor
	}
	for _, r := range projection.Ranges() {
		if r, ok := r.Get(); ok {
			paintSelectableTextSelection(layout, r, color, w)
		}
	}
	t.styledText.Paint(global, inspector, bounds, &struct{}{}, &struct{}{}, w, app)
}
