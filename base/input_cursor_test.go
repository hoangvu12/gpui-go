package base

import (
	"slices"
	"testing"
)

// The 8 tests of U:input/base/cursor.rs:250-360.

// U:input/base/cursor.rs:256-264 selection_keeps_its_public_range_api.
// Deviation: Rust's `Range<usize>::from(selection)` has no Go counterpart;
// the Start/End fields stand in for the range.
func TestInputSelectionKeepsItsPublicRangeAPI(t *testing.T) {
	selection := NewInputSelection(2, 5)
	if selection != (InputSelection{Start: 2, End: 5}) {
		t.Fatalf("got %+v", selection)
	}
	if selection.Start != 2 || selection.End != 5 || selection.Len() != 3 {
		t.Fatalf("range %+v", selection)
	}
}

// U:input/base/cursor.rs:266-275 test_line_column_from_to.
func TestInputPositionNew(t *testing.T) {
	if NewInputPosition(1, 2) != (InputPosition{Line: 1, Character: 2}) {
		t.Fatal("Position::new")
	}
}

// U:input/base/cursor.rs:277-283 test_cursor_offset_reversed.
func TestCursorSelectionCursorOffsetReversed(t *testing.T) {
	sel := newCursorSelection(newCursorID(0), 5, 10)
	inpEqInt(t, sel.cursorOffset(), 10)
	sel.reversed = true
	inpEqInt(t, sel.cursorOffset(), 5)
}

// U:input/base/cursor.rs:285-295 test_place_at.
func TestCursorSelectionPlaceAt(t *testing.T) {
	sel := newCursorSelection(newCursorID(0), 5, 10)
	sel.reversed = true
	sel.placeAt(7, cursorColumnAnchor{x: 12, column: 3, ok: true})
	if sel.start != 7 || sel.end != 7 || !sel.isCollapsed() || sel.reversed {
		t.Fatalf("got %+v", sel)
	}
	if sel.columnAnchor != (cursorColumnAnchor{x: 12, column: 3, ok: true}) {
		t.Fatalf("anchor %+v", sel.columnAnchor)
	}
}

// U:input/base/cursor.rs:297-305 test_selections_never_empty.
func TestSelectionsNeverEmpty(t *testing.T) {
	s := newSelections()
	inpEqInt(t, s.len(), 1)
	if s.active().id != newCursorID(0) {
		t.Fatal("active id")
	}
	inpEqInt(t, defaultSelections().len(), 1)
}

// U:input/base/cursor.rs:307-312 test_selections_active_mut.
func TestSelectionsActiveMut(t *testing.T) {
	s := newSelections()
	s.activeMut().placeAt(4, cursorColumnAnchor{})
	inpEqInt(t, s.active().cursorOffset(), 4)
}

// U:input/base/cursor.rs:314-343 test_selections_merge_overlapping.
func TestSelectionsMergeOverlapping(t *testing.T) {
	s := newSelections()
	id1, id2, id3 := s.generateID(), s.generateID(), s.generateID()
	s.replaceAll([]cursorSelection{
		newCursorSelection(id1, 0, 10),
		newCursorSelection(id2, 5, 15),
		newCursorSelection(id3, 20, 30),
	})
	s.mergeOverlapping()

	inpEqInt(t, s.len(), 2)
	if a := s.active(); a.id != id1 || a.start != 0 || a.end != 15 {
		t.Fatalf("active %+v", a)
	}
	var ranges [][2]int
	for sel := range s.iter() {
		ranges = append(ranges, [2]int{sel.start, sel.end})
	}
	if !slices.Contains(ranges, [2]int{0, 15}) || !slices.Contains(ranges, [2]int{20, 30}) {
		t.Fatalf("ranges %v", ranges)
	}
}

// U:input/base/cursor.rs:345-359 merging_preserves_the_active_selection_direction.
func TestSelectionsMergingPreservesTheActiveSelectionDirection(t *testing.T) {
	s := newSelections()
	activeID, otherID := s.generateID(), s.generateID()
	active := newCursorSelection(activeID, 5, 15)
	active.reversed = true
	s.replaceAll([]cursorSelection{active, newCursorSelection(otherID, 0, 10)})
	s.mergeOverlapping()
	if s.active().id != activeID || !s.active().reversed {
		t.Fatalf("active %+v", s.active())
	}
}
