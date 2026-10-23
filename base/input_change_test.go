package base

import "testing"

// U:input/base/change.rs has no tests; this Go-only check pins shifted's
// saturation at 0 (U:input/base/change.rs:32-43).
func TestChangeShifted(t *testing.T) {
	c := newChange(NewInputSelection(2, 4), "ab", NewInputSelection(2, 3), "x")
	got := c.shifted(-3)
	if got.oldRange != NewInputSelection(0, 1) || got.newRange != NewInputSelection(0, 0) ||
		got.oldText != "ab" || got.newText != "x" {
		t.Fatalf("shifted(-3) = %+v", got)
	}
	if c.shifted(5).newRange != NewInputSelection(7, 8) {
		t.Fatal("shifted(5)")
	}
}
