package base

import (
	"reflect"
	"strings"
	"testing"
)

// The 8 tests of U:input/base/undo_manager.rs:463-599.

// hasUndos and undoCount are the cfg(test) helpers of
// U:input/base/undo_manager.rs:365-373.
func (m *undoManager) hasUndos() bool { return len(m.undoTransactions) > 0 }
func (m *undoManager) undoCount() int { return len(m.undoTransactions) }

// U:input/base/undo_manager.rs:467-470.
func inpTypingChange(offset int, text string) change {
	end := offset + len(text)
	return newChange(NewInputSelection(offset, offset), "", NewInputSelection(offset, end), text)
}

func inpMustUndo(t *testing.T, m *undoManager) replay {
	t.Helper()
	r, ok := m.undo()
	if !ok {
		t.Fatal("undo() = None")
	}
	return r
}

func inpNoUndo(t *testing.T, m *undoManager) {
	t.Helper()
	if _, ok := m.undo(); ok {
		t.Fatal("undo() should be None")
	}
}

// U:input/base/undo_manager.rs:472-481.
func TestUndoManagerAdjacentTypingTransactionsCoalesce(t *testing.T) {
	m := newUndoManager()
	m.recordTransaction(inpTypingChange(0, "a"), editIntentTyping)
	m.recordTransaction(inpTypingChange(1, "b"), editIntentTyping)
	if got := inpMustUndo(t, m).changes; !reflect.DeepEqual(got, []change{inpTypingChange(0, "ab")}) {
		t.Fatalf("got %+v", got)
	}
	inpNoUndo(t, m)
}

// U:input/base/undo_manager.rs:483-498.
func TestUndoManagerExplicitTransactionCollectsMultipleChanges(t *testing.T) {
	m := newUndoManager()
	m.beginTransaction()
	m.recordTransaction(inpTypingChange(0, "a"), editIntentTyping)
	m.recordTransaction(inpTypingChange(1, "b"), editIntentTyping)
	m.commitTransaction()
	got := inpMustUndo(t, m).changes
	if len(got) != 2 || got[0].newText != "b" || got[1].newText != "a" {
		t.Fatalf("got %+v", got)
	}
	inpNoUndo(t, m)
}

// U:input/base/undo_manager.rs:500-515.
func TestUndoManagerNestedTransactionsCommitAsOneEntry(t *testing.T) {
	m := newUndoManager()
	m.beginTransaction()
	m.recordTransaction(inpTypingChange(0, "a"), editIntentTyping)
	m.beginTransaction()
	m.recordTransaction(inpTypingChange(1, "b"), editIntentTyping)
	m.commitTransaction()
	if m.hasUndos() {
		t.Fatal("inner bracket closed the transaction")
	}
	m.recordTransaction(inpTypingChange(2, "c"), editIntentTyping)
	m.commitTransaction()
	inpEqInt(t, len(inpMustUndo(t, m).changes), 3)
	inpNoUndo(t, m)
}

// U:input/base/undo_manager.rs:517-535.
func TestUndoManagerBatchesOfTheSameShapeAndIntentCoalesce(t *testing.T) {
	m := newUndoManager()
	m.beginTransactionWith(editIntentTyping)
	m.recordTransaction(inpTypingChange(5, "a"), editIntentTyping)
	m.recordTransaction(inpTypingChange(0, "a"), editIntentTyping)
	m.commitTransaction()
	m.beginTransactionWith(editIntentTyping)
	m.recordTransaction(inpTypingChange(7, "b"), editIntentTyping)
	m.recordTransaction(inpTypingChange(1, "b"), editIntentTyping)
	m.commitTransaction()
	inpEqInt(t, len(inpMustUndo(t, m).changes), 4)
	inpNoUndo(t, m)
}

// U:input/base/undo_manager.rs:537-550.
func TestUndoManagerABatchDoesNotCoalesceIntoADifferentlyShapedOne(t *testing.T) {
	m := newUndoManager()
	m.beginTransactionWith(editIntentTyping)
	m.recordTransaction(inpTypingChange(5, "a"), editIntentTyping)
	m.recordTransaction(inpTypingChange(0, "a"), editIntentTyping)
	m.commitTransaction()
	m.recordTransaction(inpTypingChange(1, "b"), editIntentTyping)
	inpEqInt(t, len(inpMustUndo(t, m).changes), 1)
	inpEqInt(t, len(inpMustUndo(t, m).changes), 2)
}

// U:input/base/undo_manager.rs:552-563.
func TestUndoManagerAnAtomicBatchNeverCoalesces(t *testing.T) {
	m := newUndoManager()
	m.beginTransaction()
	m.recordTransaction(inpTypingChange(0, "a"), editIntentTyping)
	m.commitTransaction()
	m.recordTransaction(inpTypingChange(1, "b"), editIntentTyping)
	inpEqInt(t, len(inpMustUndo(t, m).changes), 1)
	inpEqInt(t, len(inpMustUndo(t, m).changes), 1)
}

// U:input/base/undo_manager.rs:565-577.
func TestUndoManagerLimitsTheNumberOfRetainedTransactions(t *testing.T) {
	m := newUndoManager()
	for offset := range 1100 {
		m.recordTransaction(inpTypingChange(offset, "a"), editIntentAtomic)
	}
	inpEqInt(t, m.undoCount(), maxUndoTransactions)
	for range maxUndoTransactions {
		inpMustUndo(t, m)
	}
	inpNoUndo(t, m)
}

// U:input/base/undo_manager.rs:579-598.
func TestUndoManagerSplitsACoalescedTransactionBeforeItsChangeListGrowsTooLarge(t *testing.T) {
	m := newUndoManager()
	for offset := range 1100 {
		m.recordTransaction(inpTypingChange(offset, "a"), editIntentTyping)
	}
	if got := inpMustUndo(t, m).changes; !reflect.DeepEqual(got, []change{inpTypingChange(maxChangesPerTransaction, strings.Repeat("a", 100))}) {
		t.Fatalf("first undo %d changes", len(got))
	}
	if got := inpMustUndo(t, m).changes; !reflect.DeepEqual(got, []change{inpTypingChange(0, strings.Repeat("a", maxChangesPerTransaction))}) {
		t.Fatalf("second undo %d changes", len(got))
	}
	inpNoUndo(t, m)
}
