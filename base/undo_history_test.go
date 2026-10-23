package base

import (
	"testing"
	"time"
)

// Ported from U:undo_history.rs:148-329 (14/14).

func wantUndo(t *testing.T, what string, got []int, ok bool, want ...int) {
	t.Helper()
	if want == nil {
		if ok {
			t.Fatalf("%s = Some(%v), want None", what, got)
		}
		return
	}
	if !ok {
		t.Fatalf("%s = None, want Some(%v)", what, want)
	}
	wantInts(t, what, got, want)
}

func TestUndoHistoryExplicitGroupingUndoesNewestFirstAndRedoesOldestFirst(t *testing.T) {
	h := NewUndoHistory[int]()
	h.StartGrouping()
	h.Push(1)
	h.Push(2)
	h.Push(3)
	h.EndGrouping()

	got, ok := h.Undo()
	wantUndo(t, "undo", got, ok, 3, 2, 1)
	got, ok = h.Redo()
	wantUndo(t, "redo", got, ok, 1, 2, 3)
}

func TestUndoHistoryUngroupedPushesFormSeparateTransactions(t *testing.T) {
	h := NewUndoHistory[int]()
	h.Push(1)
	h.Push(2)

	got, ok := h.Undo()
	wantUndo(t, "undo", got, ok, 2)
	got, ok = h.Undo()
	wantUndo(t, "undo", got, ok, 1)
}

func TestUndoHistoryGroupIntervalCombinesImmediatePushes(t *testing.T) {
	h := NewUndoHistory[int]().GroupInterval(60 * time.Second)
	h.Push(1)
	h.Push(2)

	got, ok := h.Undo()
	wantUndo(t, "undo", got, ok, 2, 1)
}

func TestUndoHistoryUndoBreaksTimedGroupingAcrossTheBranchBoundary(t *testing.T) {
	h := NewUndoHistory[int]()
	h.Push(1)
	h.Push(2)
	h = h.GroupInterval(60 * time.Second)
	got, ok := h.Undo()
	wantUndo(t, "undo", got, ok, 2)

	h.Push(3)

	got, ok = h.Undo()
	wantUndo(t, "undo", got, ok, 3)
	got, ok = h.Undo()
	wantUndo(t, "undo", got, ok, 1)
}

func TestUndoHistoryRedoBreaksTimedGroupingAcrossTheBranchBoundary(t *testing.T) {
	h := NewUndoHistory[int]()
	h.Push(1)
	h.Push(2)
	got, ok := h.Undo()
	wantUndo(t, "undo", got, ok, 2)
	got, ok = h.Redo()
	wantUndo(t, "redo", got, ok, 2)
	h = h.GroupInterval(60 * time.Second)

	h.Push(3)

	got, ok = h.Undo()
	wantUndo(t, "undo", got, ok, 3)
	got, ok = h.Undo()
	wantUndo(t, "undo", got, ok, 2)
	got, ok = h.Undo()
	wantUndo(t, "undo", got, ok, 1)
}

func TestUndoHistoryExplicitGroupingStillAppendsAfterUndo(t *testing.T) {
	h := NewUndoHistory[int]()
	h.Push(1)
	h.Push(2)
	got, ok := h.Undo()
	wantUndo(t, "undo", got, ok, 2)

	h.StartGrouping()
	h.Push(3)
	h.EndGrouping()

	got, ok = h.Undo()
	wantUndo(t, "undo", got, ok, 3, 1)
}

func TestUndoHistoryANewPushClearsRedo(t *testing.T) {
	h := NewUndoHistory[int]()
	h.Push(1)
	h.Push(2)
	got, ok := h.Undo()
	wantUndo(t, "undo", got, ok, 2)

	h.Push(3)

	if h.CanRedo() {
		t.Fatal("can_redo after a new push")
	}
	got, ok = h.Redo()
	wantUndo(t, "redo", got, ok)
}

func TestUndoHistoryIgnoringDropsPushes(t *testing.T) {
	h := NewUndoHistory[int]()
	h.SetIgnoring(true)
	h.Push(1)

	if !h.IsIgnoring() {
		t.Fatal("is_ignoring")
	}
	if h.CanUndo() {
		t.Fatal("can_undo while ignoring")
	}
	got, ok := h.Undo()
	wantUndo(t, "undo", got, ok)
}

func TestUndoHistoryClearClearsBothDirections(t *testing.T) {
	h := NewUndoHistory[int]()
	h.Push(1)
	h.Undo()

	h.Clear()

	if h.CanUndo() || h.CanRedo() {
		t.Fatal("history remains after clear")
	}
}

func TestUndoHistoryMaxUndosEvictsTheOldestTransaction(t *testing.T) {
	h := NewUndoHistory[int]().MaxUndos(2)
	h.Push(1)
	h.Push(2)
	h.Push(3)

	got, ok := h.Undo()
	wantUndo(t, "undo", got, ok, 3)
	got, ok = h.Undo()
	wantUndo(t, "undo", got, ok, 2)
	got, ok = h.Undo()
	wantUndo(t, "undo", got, ok)
}

func TestUndoHistoryLoweringMaxUndosEvictsOldestPopulatedTransactionsImmediately(t *testing.T) {
	h := NewUndoHistory[int]()
	h.Push(1)
	h.Push(2)
	h.Push(3)

	h = h.MaxUndos(2)

	got, ok := h.Undo()
	wantUndo(t, "undo", got, ok, 3)
	got, ok = h.Undo()
	wantUndo(t, "undo", got, ok, 2)
	got, ok = h.Undo()
	wantUndo(t, "undo", got, ok)
}

func TestUndoHistoryRedoAfterLoweringMaxUndosPreservesTheCap(t *testing.T) {
	h := NewUndoHistory[int]()
	h.Push(1)
	h.Push(2)
	h.Push(3)
	got, ok := h.Undo()
	wantUndo(t, "undo", got, ok, 3)

	h = h.MaxUndos(1)
	got, ok = h.Redo()
	wantUndo(t, "redo", got, ok, 3)

	got, ok = h.Undo()
	wantUndo(t, "undo", got, ok, 3)
	got, ok = h.Undo()
	wantUndo(t, "undo", got, ok)
}

func TestUndoHistoryRedoAtZeroMaxUndosKeepsTheTransactionAvailable(t *testing.T) {
	h := NewUndoHistory[int]()
	h.Push(1)
	got, ok := h.Undo()
	wantUndo(t, "undo", got, ok, 1)

	h = h.MaxUndos(0)

	got, ok = h.Redo()
	wantUndo(t, "redo", got, ok)
	if !h.CanRedo() {
		t.Fatal("the transaction must stay available")
	}
	h = h.MaxUndos(1)
	got, ok = h.Redo()
	wantUndo(t, "redo", got, ok, 1)
}

func TestUndoHistoryZeroMaxUndosRetainsNoTransactions(t *testing.T) {
	h := NewUndoHistory[int]().MaxUndos(0)
	h.Push(1)

	if h.CanUndo() {
		t.Fatal("can_undo")
	}
	got, ok := h.Undo()
	wantUndo(t, "undo", got, ok)
}
