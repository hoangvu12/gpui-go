package base

import "time"

// UndoHistory is a history of grouped undo transactions
// (U:undo_history.rs:3-13). web_time::Instant becomes the monotonic
// time.Time.
type UndoHistory[T any] struct {
	undos            [][]T
	redos            [][]T
	lastChangedAt    time.Time
	hasLastChangedAt bool
	maxUndos         int
	groupInterval    time.Duration
	hasGroupInterval bool
	grouping         bool
	ignoring         bool
}

// NewUndoHistory returns an empty history keeping up to 1000 undo
// transactions (U:undo_history.rs:16-26; also Default, 142-146).
func NewUndoHistory[T any]() *UndoHistory[T] {
	return &UndoHistory[T]{maxUndos: 1000}
}

// MaxUndos sets the maximum number of undo transactions to keep (default
// 1000); lowering it immediately removes the oldest transactions
// (U:undo_history.rs:28-35).
func (h *UndoHistory[T]) MaxUndos(maxUndos int) *UndoHistory[T] {
	h.maxUndos = maxUndos
	h.enforceMaxUndos()
	return h
}

// GroupInterval sets the interval in which consecutive changes are
// grouped (U:undo_history.rs:37-44). A successful undo or redo ends timed
// grouping; explicit grouping is independent.
func (h *UndoHistory[T]) GroupInterval(groupInterval time.Duration) *UndoHistory[T] {
	h.groupInterval, h.hasGroupInterval = groupInterval, true
	return h
}

// StartGrouping starts explicitly grouping pushed changes into one
// transaction (U:undo_history.rs:46-49).
func (h *UndoHistory[T]) StartGrouping() { h.grouping = true }

// EndGrouping ends explicit grouping (U:undo_history.rs:51-54).
func (h *UndoHistory[T]) EndGrouping() { h.grouping = false }

// IsIgnoring reports whether recording is ignored (U:undo_history.rs:56-59).
func (h *UndoHistory[T]) IsIgnoring() bool { return h.ignoring }

// SetIgnoring sets whether recording is ignored (U:undo_history.rs:61-64).
func (h *UndoHistory[T]) SetIgnoring(ignoring bool) { h.ignoring = ignoring }

// Push pushes a change into the current transaction or starts a new one
// (U:undo_history.rs:66-87).
func (h *UndoHistory[T]) Push(item T) {
	if h.ignoring || h.maxUndos == 0 {
		return
	}
	groupWithPrevious := h.grouping ||
		(h.hasLastChangedAt && h.hasGroupInterval && time.Since(h.lastChangedAt) <= h.groupInterval)
	if groupWithPrevious && len(h.undos) > 0 {
		last := len(h.undos) - 1
		h.undos[last] = append(h.undos[last], item)
	} else {
		h.undos = append(h.undos, []T{item})
		h.enforceMaxUndos()
	}
	h.lastChangedAt, h.hasLastChangedAt = time.Now(), true
	h.redos = h.redos[:0]
}

// Undo undoes the latest transaction, returning its changes newest first
// (U:undo_history.rs:89-99).
func (h *UndoHistory[T]) Undo() ([]T, bool) {
	if len(h.undos) == 0 {
		return nil, false
	}
	transaction := h.undos[len(h.undos)-1]
	h.undos = h.undos[:len(h.undos)-1]
	changes := make([]T, 0, len(transaction))
	for i := len(transaction) - 1; i >= 0; i-- {
		changes = append(changes, transaction[i])
	}
	h.redos = append(h.redos, transaction)
	h.hasLastChangedAt = false
	return changes, true
}

// Redo redoes the latest undone transaction, returning its changes oldest
// first (U:undo_history.rs:101-115).
func (h *UndoHistory[T]) Redo() ([]T, bool) {
	if h.maxUndos == 0 || len(h.redos) == 0 {
		return nil, false
	}
	transaction := h.redos[len(h.redos)-1]
	h.redos = h.redos[:len(h.redos)-1]
	changes := append([]T(nil), transaction...)
	h.undos = append(h.undos, transaction)
	h.enforceMaxUndos()
	h.hasLastChangedAt = false
	return changes, true
}

// CanUndo reports whether an undo transaction is available
// (U:undo_history.rs:117-120).
func (h *UndoHistory[T]) CanUndo() bool { return len(h.undos) > 0 }

// CanRedo reports whether a redo transaction is available
// (U:undo_history.rs:122-125).
func (h *UndoHistory[T]) CanRedo() bool { return len(h.redos) > 0 }

// Clear clears both directions (U:undo_history.rs:127-132).
func (h *UndoHistory[T]) Clear() {
	h.undos = h.undos[:0]
	h.redos = h.redos[:0]
	h.hasLastChangedAt = false
}

// enforceMaxUndos drains the oldest excess transactions
// (U:undo_history.rs:134-139).
func (h *UndoHistory[T]) enforceMaxUndos() {
	if excess := len(h.undos) - h.maxUndos; excess > 0 {
		h.undos = append(h.undos[:0], h.undos[excess:]...)
	}
}
