package gpui

import "unicode/utf8"

// Port of U:selectable_text.rs: plain text that participates in the
// window-scoped TextSelection (text_selection.go). Moved from base into
// the core with the model (browser-core ticket 19): the core's static
// selectable text — Base re-exports it as an alias.
//
// Go adaptation: a run without an explicit handle keeps its participant in
// a keyed entity under its element id (Window.UseKeyedState), which the
// window releases when the run stops drawing — the retained
// with_element_state handle of U:selectable_text.rs:157-166.

// SelectableText is plain text that participates in the window-scoped
// TextSelection (U:selectable_text.rs:11-27). Use NewSelectableText for an
// independent run, or SelectableTextWithHandle when several elements form
// one selectable document. The application renders one TextSelectionLayer
// as the root's first child so the window's selection gestures install.
type SelectableText struct {
	id             ElementID
	handle         *TextSelectionHandle
	text           string
	styledText     *StyledText
	documentOrder  uint64
	textStyle      *TextStyleRefinement
	selectionColor *Hsla
}

// NewSelectableText creates a run that owns its own selection
// (U:selectable_text.rs:30-33).
func NewSelectableText[I IntoElementID](id I, text string) *SelectableText {
	return buildSelectableText(ElementIDOf(id), nil, text)
}

// SelectableTextWithHandle creates a run that joins the document handle
// belongs to (U:selectable_text.rs:35-42).
func SelectableTextWithHandle[I IntoElementID](id I, handle TextSelectionHandle, text string) *SelectableText {
	return buildSelectableText(ElementIDOf(id), &handle, text)
}

// buildSelectableText is SelectableText::build (U:selectable_text.rs:44-54).
func buildSelectableText(id ElementID, handle *TextSelectionHandle, text string) *SelectableText {
	return &SelectableText{id: id, handle: handle, text: text, styledText: NewStyledText(text)}
}

// DocumentOrder places this run in reading order among the others sharing
// its handle (U:selectable_text.rs:56-60).
func (t *SelectableText) DocumentOrder(order uint64) *SelectableText {
	t.documentOrder = order
	return t
}

// TextStyle sets the text style the run is laid out and painted with
// (U:selectable_text.rs:62-66).
func (t *SelectableText) TextStyle(style TextStyleRefinement) *SelectableText {
	t.textStyle = &style
	return t
}

// SelectionColor overrides the selection background, which defaults to the
// system's selection wash (browser-core 28, App.SelectionColors — the
// ::selection UA default) (U:selectable_text.rs:68-73, diverged toward
// the browser).
func (t *SelectableText) SelectionColor(color Hsla) *SelectableText {
	t.selectionColor = &color
	return t
}

// paintSelectableTextSelection paints the selection background of r
// (U:selectable_text.rs:75-97). A selection whose end sits at a line
// break paints only to the last inked glyph — the break takes no ink —
// exactly like Chromium's LayoutSelection fragments (the selected
// InlineBoxes' extent; a trailing \n is a zero-advance character, so the
// fragment ends at the preceding glyph, not at the row's wrap edge).
func paintSelectableTextSelection(layout *TextLayout, r TextRange, color Hsla, w *Window) {
	text := layout.Text()
	// The painted end walks back over trailing line breaks: the break
	// characters carry no ink.
	end := r.End
	for end > r.Start {
		last, size := utf8.DecodeLastRuneInString(text[:end])
		if last != '\n' && last != '\r' {
			break
		}
		end -= size
	}
	start, ok := layout.PositionForIndex(r.Start)
	if !ok {
		return
	}
	endPos, ok := layout.PositionForIndex(end)
	if !ok {
		return
	}
	for _, bounds := range selectionQuadBounds(start, endPos, layout.Bounds(), layout.LineHeight()) {
		w.PaintQuad(Fill(bounds, SolidBackground(color)))
	}
}

// selectionQuadBounds is the quads a selection from start to end covers:
// the rest of the first row, the full width of the rows between, and the
// last row up to end (U:selectable_text.rs:100-128).
func selectionQuadBounds(start, end Point, bounds Bounds, lineHeight float32) []Bounds {
	if start.Y == end.Y {
		return []Bounds{boundsFromCorners(start, Point{X: end.X, Y: end.Y + lineHeight})}
	}
	quads := []Bounds{boundsFromCorners(start, Point{X: bounds.Right(), Y: start.Y + lineHeight})}
	if end.Y > start.Y+lineHeight {
		quads = append(quads, boundsFromCorners(Point{X: bounds.Left(), Y: start.Y + lineHeight}, Point{X: bounds.Right(), Y: end.Y}))
	}
	return append(quads, boundsFromCorners(Point{X: bounds.Left(), Y: end.Y}, Point{X: end.X, Y: end.Y + lineHeight}))
}

// IntoElement implements IntoElement.
func (t *SelectableText) IntoElement() AnyElement {
	return CustomElement[TextSelectionHandle, Hitbox](t)
}

// ID implements Element (U:selectable_text.rs:142-144).
func (t *SelectableText) ID() (ElementID, bool) { return t.id, true }

// SourceLocation implements Element.
func (t *SelectableText) SourceLocation() *SourceLocation { return nil }

// RequestLayout resolves the participant and lays the text out
// (U:selectable_text.rs:150-177).
func (t *SelectableText) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, TextSelectionHandle) {
	var handle TextSelectionHandle
	if t.handle != nil {
		handle = *t.handle
	} else {
		text := t.text
		entity := w.UseKeyedState(NameElementID("selection"), app, func(_ *Window, cx *Context[selectableTextState]) selectableTextState {
			return selectableTextState{fallbackCopyText: text}
		})
		handle = TextSelectionHandle{entity: entity}
	}
	var id LayoutID
	if t.textStyle != nil {
		id = WithTextStyle(w, t.textStyle, func(w *Window) LayoutID {
			id, _ := t.styledText.RequestLayout(global, inspector, w, app)
			return id
		})
	} else {
		// Without an explicit refinement the run renders verbatim
		// (browser-core 38, white_space.go): the pin's selectable
		// text is user-provided text — pre-wrap — where the core's
		// default normal would collapse its newlines and space runs.
		// An explicit refinement still wins (a caller can opt into the
		// collapse with WhiteSpaceNormal).
		id = WithTextStyle(w, &TextStyleRefinement{WhiteSpace: Some(WhiteSpacePreWrap)}, func(w *Window) LayoutID {
			id, _ := t.styledText.RequestLayout(global, inspector, w, app)
			return id
		})
	}
	return id, handle
}

// Prepaint lays the text out and registers the run for the frame
// (U:selectable_text.rs:179-197). Browser-core 20: user-select: none
// on the ambient text style (an ancestor's opt-out) suppresses the
// registration — the run still paints, but it never joins the window
// selection, like Chrome's none text. Browser-core 59: the
// registration carries the run itself, like the automatic path's
// (user_select.go), so the model's copy projection resolves on demand
// from the registration when the reporting element's frame is reused
// (view_reuse.go) and its paint's UpdateRuns cache refresh is skipped
// — the automatic path's on-demand rule (text_selection.go copyItem).
func (t *SelectableText) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, handle *TextSelectionHandle, w *Window, app *App) Hitbox {
	t.styledText.Prepaint(global, inspector, bounds, &struct{}{}, w, app)
	if !userSelectAllowsSelection(w) {
		return Hitbox{}
	}
	// An explicit participant brings the window's selection state into
	// existence the same way the automatic one does (browser-core 20:
	// the install resolves the state lazily, so the first participant
	// creates it).
	ensureUserSelectState(w, app)
	hitbox := w.InsertHitbox(bounds, HitboxNormal)
	registration := NewTextSelectionRegistration(hitbox, bounds).
		WithDocumentOrder(t.documentOrder).
		WithTextBounds([]Bounds{bounds}).
		WithRuns([]TextSelectionRun{
			NewTextSelectionRun(t.text, t.styledText.Layout(), bounds).WithDocumentOrder(t.documentOrder),
		}).
		WithRenderedElement(*handle, w, app)
	handle.Register(registration, w, app)
	return hitbox
}

// Paint projects the selection onto the run, paints its background and
// the text (U:selectable_text.rs:199-236).
func (t *SelectableText) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, handle *TextSelectionHandle, _ *Hitbox, w *Window, app *App) {
	layout := t.styledText.Layout()
	selectedBefore := TextSelectionSelectedText(w, app)
	projection := handle.UpdateRuns([]TextSelectionRun{
		NewTextSelectionRun(t.text, layout, bounds).WithDocumentOrder(t.documentOrder),
	}, app)
	if selectedBefore != TextSelectionSelectedText(w, app) {
		w.Refresh()
	}
	// The selection wash comes from the system by default (browser-core
	// 28, App.SelectionColors) — the ::selection UA default — with
	// SelectionColor as the app's explicit ::selection styling.
	color := app.SelectionColorsForPaint().Background
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
