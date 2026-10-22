package base

// Port of U:input/base/cursor.rs: the public Selection plus the editor's
// private multi-cursor model (CursorSelection, Selections).
//
// Naming (doc.go): `input::Selection` (U:input/mod.rs:83) takes the Input
// prefix as InputSelection, since Selection is a plausible crate-root name.
// CursorSelection and Selections are pub(super) upstream and stay
// unexported. Rust's Range/RangeBounds conversions (U:input/base/cursor.rs:
// 35-53, 112-132) have no Go counterpart; the Start/End fields are the range.

import (
	"iter"
	"slices"
)

// InputSelection is a selection in the text as start and end byte offsets
// (U:input/base/cursor.rs:3-8).
type InputSelection struct {
	Start int
	End   int
}

// NewInputSelection is Selection::new (U:input/base/cursor.rs:11-13).
func NewInputSelection(start, end int) InputSelection {
	return InputSelection{Start: start, End: end}
}

// Len is end - start, saturating at 0 (U:input/base/cursor.rs:15-17).
func (s InputSelection) Len() int { return max(s.End-s.Start, 0) }

// IsEmpty reports start == end (U:input/base/cursor.rs:19-21).
func (s InputSelection) IsEmpty() bool { return s.Start == s.End }

// Clear sets start and end to 0 (U:input/base/cursor.rs:23-27).
func (s *InputSelection) Clear() { s.Start, s.End = 0, 0 }

// Contains reports start <= offset < end (U:input/base/cursor.rs:29-32).
func (s InputSelection) Contains(offset int) bool { return offset >= s.Start && offset < s.End }

// cursorColumnAnchor is CursorSelection's Option<(Pixels, usize)>
// (U:input/base/cursor.rs:65): the remembered x (Pixels, float32 in the Go
// core) and column for vertical motion; ok is Some.
type cursorColumnAnchor struct {
	x      float32
	column int
	ok     bool
}

// cursorSelection is one cursor with its selection
// (U:input/base/cursor.rs:59-66). The cursor sits at end, or at start when
// reversed.
type cursorSelection struct {
	id           cursorID
	start        int
	end          int
	reversed     bool
	columnAnchor cursorColumnAnchor
}

// newCursorSelection is CursorSelection::new (U:input/base/cursor.rs:69-77).
func newCursorSelection(id cursorID, start, end int) cursorSelection {
	return cursorSelection{id: id, start: start, end: end}
}

func (s cursorSelection) len() int              { return max(s.end-s.start, 0) }         // :79-81
func (s cursorSelection) isEmpty() bool         { return s.start == s.end }              // :83-85
func (s *cursorSelection) clear()               { s.start, s.end = 0, 0 }                // :87-90
func (s cursorSelection) contains(off int) bool { return off >= s.start && off < s.end } // :92-94
func (s cursorSelection) isCollapsed() bool     { return s.isEmpty() }                   // :107-109

// cursorOffset is where the cursor sits (U:input/base/cursor.rs:96-98).
func (s cursorSelection) cursorOffset() int {
	if s.reversed {
		return s.start
	}
	return s.end
}

// placeAt collapses the selection to offset (U:input/base/cursor.rs:100-105).
func (s *cursorSelection) placeAt(offset int, columnAnchor cursorColumnAnchor) {
	s.start = offset
	s.end = offset
	s.reversed = false
	s.columnAnchor = columnAnchor
}

// selections is the cursor set; it always holds at least one selection and
// the active one is at index 0 (U:input/base/cursor.rs:134-137).
type selections struct {
	selections []cursorSelection
	nextID     int
}

// newSelections is Selections::new (U:input/base/cursor.rs:140-145).
func newSelections() *selections {
	return &selections{selections: []cursorSelection{newCursorSelection(newCursorID(0), 0, 0)}, nextID: 1}
}

// defaultSelections is `Default for Selections` (U:input/base/cursor.rs:244-248).
func defaultSelections() *selections { return newSelections() }

// active returns the active selection (U:input/base/cursor.rs:147-152).
func (s *selections) active() cursorSelection { return s.selections[0] }

// activeMut returns the active selection for mutation
// (U:input/base/cursor.rs:154-159).
func (s *selections) activeMut() *cursorSelection { return &s.selections[0] }

// iter yields the selections (U:input/base/cursor.rs:161-163).
func (s *selections) iter() iter.Seq[cursorSelection] { return slices.Values(s.selections) }

// len is the number of selections, always >= 1 (U:input/base/cursor.rs:165-168).
func (s *selections) len() int { return len(s.selections) }

// isSingle reports exactly one selection (U:input/base/cursor.rs:170-173).
func (s *selections) isSingle() bool { return len(s.selections) == 1 }

// generateID returns a new unique cursor id (U:input/base/cursor.rs:175-180).
func (s *selections) generateID() cursorID {
	id := newCursorID(s.nextID)
	s.nextID++
	return id
}

// add appends a selection (U:input/base/cursor.rs:182-185).
func (s *selections) add(selection cursorSelection) { s.selections = append(s.selections, selection) }

// replaceAll replaces every selection, ignoring an empty list to keep the
// at-least-one invariant (U:input/base/cursor.rs:187-193).
func (s *selections) replaceAll(selections []cursorSelection) {
	if len(selections) > 0 {
		s.selections = selections
	}
}

// removeAllButActive keeps only index 0 (U:input/base/cursor.rs:195-198).
func (s *selections) removeAllButActive() { s.selections = s.selections[:1] }

// mergeOverlapping sorts by start and folds overlapping or adjacent
// selections together, keeping the active id (and its direction) on the
// merged result at index 0 (U:input/base/cursor.rs:200-241).
func (s *selections) mergeOverlapping() {
	if len(s.selections) <= 1 {
		return
	}
	activeID := s.active().id
	slices.SortStableFunc(s.selections, func(a, b cursorSelection) int { return a.start - b.start })

	merged := make([]cursorSelection, 0, len(s.selections))
	for _, selection := range s.selections {
		if n := len(merged); n > 0 {
			last := &merged[n-1]
			if selection.start <= last.end {
				didMerge := selection.start != last.start || selection.end != last.end
				last.end = max(last.end, selection.end)
				if selection.id == activeID {
					last.id = activeID
					last.reversed = selection.reversed
				}
				if didMerge {
					last.columnAnchor = cursorColumnAnchor{}
				}
				continue
			}
		}
		merged = append(merged, selection)
	}
	if pos := slices.IndexFunc(merged, func(c cursorSelection) bool { return c.id == activeID }); pos >= 0 {
		merged[0], merged[pos] = merged[pos], merged[0]
	}
	s.selections = merged
}
