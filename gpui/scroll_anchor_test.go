package gpui

import "testing"

// TestScrollAnchoringKeepsVisibleContentOnInsertionAbove is the
// scroll-anchoring slice's red/green (browser-core ticket 46): a
// scroller scrolled to the end of a 300px column in a 100px port, then
// a 50px row inserted above the viewport — the offset follows the
// insertion (the visible rows' bounds stay put) instead of leaving the
// content shifted up, exactly as Chrome's scroll anchoring adjusts it
// (ScrollAnchor::Adjust, core/layout/scroll_anchor.cc:713-760). The
// browser fixture (internal/parity/fixtures/anchoring) pins the same
// numbers against Chrome's record; this test is the library-level gate.
func TestScrollAnchoringKeepsVisibleContentOnInsertionAbove(t *testing.T) {
	handle := NewScrollHandle()
	inserted := false
	f := newScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		scroller := Div().
			ID("scroller").
			W(Px(100)).
			H(Px(100)).
			TrackScroll(handle).
			OverflowYScroll().
			FlexCol()
		if inserted {
			scroller = scroller.Child(Div().ID("new").W(Px(100)).H(Px(50)).FlexShrink0().DebugSelector("new"))
		}
		for i := 0; i < 6; i++ {
			scroller = scroller.Child(Div().ID(rowID(i)).W(Px(100)).H(Px(50)).FlexShrink0().DebugSelector(rowID(i)))
		}
		return scroller.IntoElement()
	})
	if handle.MaxOffset() != (Point{Y: 200}) {
		t.Fatalf("max offset = %v, want (0,200)", handle.MaxOffset())
	}
	// Scroll to the end: the port shows content [200, 300) — the anchor
	// is row 4 (the first intersecting row, its top at 200).
	handle.SetOffset(Point{Y: -200})
	f.draw()
	if handle.Offset() != (Point{Y: -200}) {
		t.Fatalf("offset after the scroll = %v, want (0,-200)", handle.Offset())
	}
	anchorBefore, ok := WindowDebugBound(f.window, rowID(4))
	if !ok || anchorBefore.Origin.Y != 0 {
		t.Fatalf("anchor row bounds before = %+v ok=%v, want y=0 (the port's top edge)", anchorBefore, ok)
	}

	// Insert a 50px row above the whole viewport: the anchor row's
	// content position moves to 250, the anchoring adjusts the offset to
	// -250, and the anchor row's bounds stay at y=0.
	inserted = true
	f.update(func(v *scrollView) {})
	if handle.MaxOffset() != (Point{Y: 250}) {
		t.Fatalf("max offset after the insertion = %v, want (0,250)", handle.MaxOffset())
	}
	if handle.Offset() != (Point{Y: -250}) {
		t.Fatalf("offset after the insertion = %v, want (0,-250) (the anchoring followed the insertion; a port without it leaves -200)", handle.Offset())
	}
	anchorAfter, ok := WindowDebugBound(f.window, rowID(4))
	if !ok || anchorAfter.Origin.Y != 0 {
		t.Fatalf("anchor row bounds after = %+v ok=%v, want y=0 (the visible content stayed in place)", anchorAfter, ok)
	}
	// The inserted row sits above the viewport: its content position is
	// 0, so at offset -250 it prepaints at y=-250.
	newRow, ok := WindowDebugBound(f.window, "new")
	if !ok || newRow.Origin.Y != -250 {
		t.Fatalf("inserted row bounds = %+v ok=%v, want y=-250", newRow, ok)
	}
}

// TestScrollAnchoringDoesNotFireFromRestOrFollowUserScrolls pins the
// two suppression rules (browser-core 46): no anchoring at a zero
// offset (Chromium's NotifyBeforeLayout clears the anchor at rest —
// content inserted while at the top does not move the offset), and a
// user scroll between frames adjusts nothing (the anchor's
// content-space position is scroll-independent, so the delta is zero
// and the anchoring never fights the scroll).
func TestScrollAnchoringDoesNotFireFromRestOrFollowUserScrolls(t *testing.T) {
	handle := NewScrollHandle()
	inserted := false
	f := newScrollFixture(t, Size{Width: 400, Height: 300}, func(v *scrollView, w *Window, cx *Context[scrollView]) AnyElement {
		scroller := Div().
			ID("scroller").
			W(Px(100)).
			H(Px(100)).
			TrackScroll(handle).
			OverflowYScroll().
			FlexCol()
		if inserted {
			scroller = scroller.Child(Div().ID("new").W(Px(100)).H(Px(50)).FlexShrink0().DebugSelector("new"))
		}
		for i := 0; i < 6; i++ {
			scroller = scroller.Child(Div().ID(rowID(i)).W(Px(100)).H(Px(50)).FlexShrink0().DebugSelector(rowID(i)))
		}
		return scroller.IntoElement()
	})
	// At rest (offset 0): inserting above does not move the offset.
	inserted = true
	f.update(func(v *scrollView) {})
	if handle.Offset() != (Point{}) {
		t.Fatalf("offset after inserting at rest = %v, want (0,0) (no anchoring from the top)", handle.Offset())
	}
	// A user scroll with unchanged content: the offset takes the user's
	// value verbatim.
	handle.SetOffset(Point{Y: -90})
	f.draw()
	if handle.Offset() != (Point{Y: -90}) {
		t.Fatalf("offset after a user scroll = %v, want (0,-90)", handle.Offset())
	}
	handle.SetOffset(Point{Y: -120})
	f.draw()
	if handle.Offset() != (Point{Y: -120}) {
		t.Fatalf("offset after a second user scroll = %v, want (0,-120) (the anchoring never fights the user)", handle.Offset())
	}
}

// rowID names one anchoring test row.
func rowID(i int) string {
	return "row-" + string(rune('0'+i))
}
