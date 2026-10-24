package gpui

// Port of U:touch_selection.rs: touch selection geometry shared by window
// text selection (text_selection.go) and, upstream, the input engine.
// Moved from base into the core by browser-core ticket 19 (the text
// selection model owns the touch gesture); Base re-exports it.
//
// A touch selection is one made by a long press. It differs from a pointer
// selection in what the user needs afterwards: a grab handle at each end to
// adjust it, since a finger cannot hover an I-beam, and an edit menu next
// to it, since there is no right click. The core owns the gesture, the
// handle drag and the menu lifecycle; this file carries what a presentation
// layer needs to draw them.

// SelectionEdge is one end of a selection, as a touch handle grabs it
// (U:touch_selection.rs:14-21).
type SelectionEdge uint8

const (
	// SelectionEdgeStart is the end before the first selected character.
	SelectionEdgeStart SelectionEdge = iota
	// SelectionEdgeEnd is the end after the last selected character.
	SelectionEdgeEnd
)

// Opposite is the end that stays put while this one is dragged
// (U:touch_selection.rs:23-31).
func (e SelectionEdge) Opposite() SelectionEdge {
	if e == SelectionEdgeStart {
		return SelectionEdgeEnd
	}
	return SelectionEdgeStart
}

// TouchSelectionSnapshot is where a touch selection's ends are laid out
// this frame, and what the gesture is doing to them
// (U:touch_selection.rs:33-51). Each end is the caret line box at that end
// in window coordinates: zero width, one line tall. Built only by the
// model; a presentation layer reads it to draw the handles and place the
// edit menu.
type TouchSelectionSnapshot struct {
	start, end Bounds
	// startVisible and endVisible report whether each end is inside its
	// owner's viewport. An end scrolled out of view keeps its geometry,
	// for the drag that holds the other end, but gets no handle.
	startVisible, endVisible bool
	menuOpen                 bool
	dragging                 Optional[SelectionEdge]
}

// newTouchSelectionSnapshot is TouchSelectionSnapshot::new
// (U:touch_selection.rs:54-63).
func newTouchSelectionSnapshot(start, end Bounds) TouchSelectionSnapshot {
	return TouchSelectionSnapshot{start: start, end: end, startVisible: true, endVisible: true}
}

// withEdgeVisible is U:touch_selection.rs:65-71.
func (s TouchSelectionSnapshot) withEdgeVisible(edge SelectionEdge, visible bool) TouchSelectionSnapshot {
	if edge == SelectionEdgeStart {
		s.startVisible = visible
	} else {
		s.endVisible = visible
	}
	return s
}

// withMenuOpen is U:touch_selection.rs:73-76.
func (s TouchSelectionSnapshot) withMenuOpen(open bool) TouchSelectionSnapshot {
	s.menuOpen = open
	return s
}

// withDragging is U:touch_selection.rs:78-81.
func (s TouchSelectionSnapshot) withDragging(dragging Optional[SelectionEdge]) TouchSelectionSnapshot {
	s.dragging = dragging
	return s
}

// Start is the caret line box before the first selected character
// (U:touch_selection.rs:83-86).
func (s TouchSelectionSnapshot) Start() Bounds { return s.start }

// End is the caret line box after the last selected character
// (U:touch_selection.rs:88-91).
func (s TouchSelectionSnapshot) End() Bounds { return s.end }

// Edge is the caret line box at the given end (U:touch_selection.rs:93-99).
func (s TouchSelectionSnapshot) Edge(edge SelectionEdge) Bounds {
	if edge == SelectionEdgeStart {
		return s.start
	}
	return s.end
}

// IsEdgeVisible reports whether the given end is in view: a handle is drawn
// only for an end that is; the menu anchors to the ends that are
// (U:touch_selection.rs:101-108).
func (s TouchSelectionSnapshot) IsEdgeVisible(edge SelectionEdge) bool {
	if edge == SelectionEdgeStart {
		return s.startVisible
	}
	return s.endVisible
}

// IsEmpty reports a bare caret, which gets a menu but no handles
// (U:touch_selection.rs:110-113).
func (s TouchSelectionSnapshot) IsEmpty() bool { return s.start == s.end }

// Bounds is the smallest box holding the ends in view, for anchoring the
// edit menu; false when the whole selection is scrolled away
// (U:touch_selection.rs:115-124).
func (s TouchSelectionSnapshot) Bounds() (Bounds, bool) {
	switch {
	case s.startVisible && s.endVisible:
		return boundsUnion(s.start, s.end), true
	case s.startVisible:
		return s.start, true
	case s.endVisible:
		return s.end, true
	}
	return Bounds{}, false
}

// IsMenuOpen reports whether the edit menu is open. It closes while a
// handle is dragged and reopens when the drag ends
// (U:touch_selection.rs:126-130).
func (s TouchSelectionSnapshot) IsMenuOpen() bool { return s.menuOpen }

// Dragging is the end currently being dragged by its handle
// (U:touch_selection.rs:132-135).
func (s TouchSelectionSnapshot) Dragging() Optional[SelectionEdge] { return s.dragging }

// The grab handle at one end of a touch selection: a bar down the caret
// line with a knob at its outer end, the start's above and the end's below
// (U:touch_selection.rs:138-186, TouchHandle). The model paints it inside
// the text that owns the selection, with the selection's own color.
const (
	// TouchHandleHitSize is how wide the finger may miss the knob and still
	// take it (TouchHandle::HIT_SIZE).
	TouchHandleHitSize float32 = 44
	// TouchHandleKnobSize is the knob's diameter (TouchHandle::KNOB_SIZE).
	TouchHandleKnobSize float32 = 10
	// TouchHandleBarWidth is the bar down the caret line
	// (TouchHandle::BAR_WIDTH).
	TouchHandleBarWidth float32 = 2
	// TouchHandleExtent is the room the knob takes beyond the line, which
	// an edit menu keeps clear (TouchHandle::EXTENT).
	TouchHandleExtent float32 = 12
)

// TouchHandleHitBounds is the touch target, centered on the caret and
// reaching out past the knob (TouchHandle::hit_bounds,
// U:touch_selection.rs:157-167).
func TouchHandleHitBounds(edge SelectionEdge, caret Bounds) Bounds {
	top := caret.Top()
	if edge == SelectionEdgeStart {
		top = caret.Top() - TouchHandleExtent
	}
	return Bounds{
		Origin: Point{X: caret.Left() - TouchHandleHitSize/2, Y: top},
		Size:   Size{Width: TouchHandleHitSize, Height: caret.Size.Height + TouchHandleExtent},
	}
}

// TouchHandlePaint paints the handle for edge on the caret line box caret
// (TouchHandle::paint, U:touch_selection.rs:169-185).
func TouchHandlePaint(edge SelectionEdge, caret Bounds, color Hsla, w *Window) {
	bar := Bounds{
		Origin: Point{X: caret.Left() - TouchHandleBarWidth/2, Y: caret.Top()},
		Size:   Size{Width: TouchHandleBarWidth, Height: caret.Size.Height},
	}
	knobTop := caret.Bottom()
	if edge == SelectionEdgeStart {
		knobTop = caret.Top() - TouchHandleKnobSize
	}
	knob := Bounds{
		Origin: Point{X: caret.Left() - TouchHandleKnobSize/2, Y: knobTop},
		Size:   Size{Width: TouchHandleKnobSize, Height: TouchHandleKnobSize},
	}
	w.PaintQuad(Fill(bar, SolidBackground(color)))
	quad := Fill(knob, SolidBackground(color))
	r := TouchHandleKnobSize / 2
	quad.CornerRadii = Corners{TopLeft: r, TopRight: r, BottomRight: r, BottomLeft: r}
	w.PaintQuad(quad)
}

// CaretLineBox is the caret line box at position, for reporting a
// selection end (U:touch_selection.rs:188-191) — exported for Base's
// inline renderer when the model moved into the core (browser-core 19).
func CaretLineBox(position Point, lineHeight float32) Bounds {
	return caretLineBox(position, lineHeight)
}

// caretLineBox is the caret line box at position, for reporting a selection
// end (U:touch_selection.rs:188-191).
func caretLineBox(position Point, lineHeight float32) Bounds {
	return Bounds{Origin: position, Size: Size{Height: lineHeight}}
}

// caretInView reports whether a caret line box shows inside viewport: its
// line overlaps the viewport vertically and its x lies within it. A
// zero-width box never intersects anything, so this is not an intersection
// test (U:touch_selection.rs:193-201).
func caretInView(caret, viewport Bounds) bool {
	return caret.Bottom() > viewport.Top() &&
		caret.Top() < viewport.Bottom() &&
		caret.Left() >= viewport.Left() &&
		caret.Left() <= viewport.Right()
}

// edgeDrag maps a finger to the text position a handle drag selects
// (U:touch_selection.rs:203-214). The knob a finger holds sits above or
// below the line, so the finger is never over the text it moves; the
// finger-to-caret offset captured when the drag begins is kept for the
// rest of it.
type edgeDrag struct {
	edge   SelectionEdge
	offset Point
}

// newEdgeDrag is EdgeDrag::begin (U:touch_selection.rs:217-222).
func newEdgeDrag(edge SelectionEdge, caret Bounds, finger Point) edgeDrag {
	return edgeDrag{edge: edge, offset: Point{X: caret.Center().X - finger.X, Y: caret.Center().Y - finger.Y}}
}

// setEdge records that the finger dragged its end past the other one and
// now holds that one (U:touch_selection.rs:228-231).
func (d *edgeDrag) setEdge(edge SelectionEdge) { d.edge = edge }

// textPosition is the text position the finger points at
// (U:touch_selection.rs:233-236).
func (d edgeDrag) textPosition(finger Point) Point {
	return Point{X: finger.X + d.offset.X, Y: finger.Y + d.offset.Y}
}
