package base

import (
	"slices"
	"testing"
)

// Ported from U:history.rs:134-285 (10/10).

func wantInts(t *testing.T, what string, got, want []int) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s = %v, want %v", what, got, want)
	}
}

func wantSome[T comparable](t *testing.T, what string, got T, ok bool, want T) {
	t.Helper()
	if !ok || got != want {
		t.Fatalf("%s = (%v, %v), want Some(%v)", what, got, ok, want)
	}
}

func wantNone[T any](t *testing.T, what string, got T, ok bool) {
	t.Helper()
	if ok {
		t.Fatalf("%s = Some(%v), want None", what, got)
	}
}

func reversed(s []int) []int {
	out := slices.Clone(s)
	slices.Reverse(out)
	return out
}

func TestHistoryNavigationMovesBetweenEntriesWithoutBackingPastTheRoot(t *testing.T) {
	h := NewHistory[int]().MaxEntries(3)
	h.Push(1)
	h.Push(2)
	h.Push(3)

	v, ok := h.Current()
	wantSome(t, "current", v, ok, 3)
	v, ok = h.Back()
	wantSome(t, "back", v, ok, 2)
	v, ok = h.Back()
	wantSome(t, "back", v, ok, 1)
	v, ok = h.Back()
	wantNone(t, "back", v, ok)
	v, ok = h.Forward()
	wantSome(t, "forward", v, ok, 2)
	wantInts(t, "entries", h.Entries(), []int{1, 2})
	wantInts(t, "entries rev", reversed(h.Entries()), []int{2, 1})
	wantInts(t, "forward entries", h.ForwardEntries(), []int{3})
	if !h.CanBack() || !h.CanForward() {
		t.Fatal("expected can_back and can_forward")
	}
}

func TestHistoryPushingAfterBackTruncatesTheForwardBranch(t *testing.T) {
	h := NewHistory[int]()
	h.Push(1)
	h.Push(2)
	h.Push(3)
	v, ok := h.Back()
	wantSome(t, "back", v, ok, 2)

	h.Push(4)

	wantInts(t, "entries", h.Entries(), []int{1, 2, 4})
	if h.CanForward() {
		t.Fatal("can_forward after push")
	}
	v, ok = h.Forward()
	wantNone(t, "forward", v, ok)
}

func TestHistoryRepeatedEntriesPreserveEveryNavigationStep(t *testing.T) {
	h := NewHistory[int]()
	h.Push(1)
	h.Push(2)
	h.Push(1)

	wantInts(t, "entries", h.Entries(), []int{1, 2, 1})
	v, ok := h.Back()
	wantSome(t, "back", v, ok, 2)
	v, ok = h.Back()
	wantSome(t, "back", v, ok, 1)
}

func TestHistoryMaxEntriesEvictsTheOldestEntry(t *testing.T) {
	h := NewHistory[int]().MaxEntries(2)
	h.Push(1)
	h.Push(2)
	h.Push(3)

	wantInts(t, "entries", h.Entries(), []int{2, 3})
	v, ok := h.Back()
	wantSome(t, "back", v, ok, 2)
	v, ok = h.Back()
	wantNone(t, "back", v, ok)
}

func TestHistoryLoweringMaxEntriesTruncatesPopulatedEntriesAndCapsForwardRestores(t *testing.T) {
	h := NewHistory[int]()
	h.Push(1)
	h.Push(2)
	h.Push(3)
	v, ok := h.Back()
	wantSome(t, "back", v, ok, 2)

	h = h.MaxEntries(1)

	wantInts(t, "entries", h.Entries(), []int{2})
	v, ok = h.Forward()
	wantSome(t, "forward", v, ok, 3)
	wantInts(t, "entries", h.Entries(), []int{3})
	v, ok = h.Back()
	wantNone(t, "back", v, ok)
}

func TestHistoryZeroMaxEntriesRetainsNothing(t *testing.T) {
	h := NewHistory[int]().MaxEntries(0)
	h.Push(1)

	v, ok := h.Current()
	wantNone(t, "current", v, ok)
	if n := len(h.Entries()); n != 0 {
		t.Fatalf("entries len = %d", n)
	}
	if h.CanBack() || h.CanForward() {
		t.Fatal("expected no navigation")
	}
}

func TestHistoryReplaceCurrentUpdatesInPlaceAndPushesWhenEmpty(t *testing.T) {
	h := NewHistory[int]()
	h.ReplaceCurrent(1)
	h.Push(2)
	h.ReplaceCurrent(3)

	wantInts(t, "entries", h.Entries(), []int{1, 3})
}

func TestHistoryRemoveCurrentPreservesForwardEntries(t *testing.T) {
	h := NewHistory[int]()
	h.Push(1)
	h.Push(2)
	h.Push(3)
	v, ok := h.Back()
	wantSome(t, "back", v, ok, 2)

	v, ok = h.RemoveCurrent()
	wantSome(t, "remove_current", v, ok, 2)

	v, ok = h.Current()
	wantSome(t, "current", v, ok, 1)
	wantInts(t, "forward entries", h.ForwardEntries(), []int{3})
	v, ok = h.Forward()
	wantSome(t, "forward", v, ok, 3)
}

func TestHistoryRetainFiltersBackAndForwardEntriesWithoutReordering(t *testing.T) {
	h := NewHistory[int]()
	for entry := 1; entry <= 8; entry++ {
		h.Push(entry)
	}
	h.Back()
	h.Back()
	h.Back()
	h.Back()

	h.Retain(func(entry int) bool { return entry%2 == 0 })

	wantInts(t, "entries", h.Entries(), []int{2, 4})
	wantInts(t, "forward entries", h.ForwardEntries(), []int{6, 8})
	v, ok := h.Forward()
	wantSome(t, "forward", v, ok, 6)
	v, ok = h.Forward()
	wantSome(t, "forward", v, ok, 8)
}

func TestHistoryClearRemovesBackAndForwardEntries(t *testing.T) {
	h := NewHistory[int]()
	h.Push(1)
	h.Push(2)
	h.Back()

	h.Clear()

	v, ok := h.Current()
	wantNone(t, "current", v, ok)
	if len(h.Entries()) != 0 || len(h.ForwardEntries()) != 0 {
		t.Fatal("entries remain after clear")
	}
	if h.CanBack() || h.CanForward() {
		t.Fatal("expected no navigation")
	}
}
