package base

// Port of U:input/base/undo_manager.rs. Every item is pub(crate) or private
// upstream, so all stay unexported.
//
// Optional snapshots: Option<Vec<CursorSelection>> is a nil slice for None
// (a non-nil, possibly empty slice is Some). Option<AutoClosedPairs> is a
// nil `any` for None; AutoClosedPairs belongs to the editor auto-close port
// (U:input/editor/auto_close.rs:3-7), which should narrow these fields. The
// snapshot is returned as stored rather than cloned, so it must be treated
// as immutable.

import "strings"

const (
	maxUndoTransactions      = 1000 // U:input/base/undo_manager.rs:6
	maxChangesPerTransaction = 1000 // U:input/base/undo_manager.rs:7
)

// editIntent classifies an edit for coalescing
// (U:input/base/undo_manager.rs:9-15).
type editIntent uint8

const (
	editIntentTyping editIntent = iota
	editIntentBackspace
	editIntentDeleteForward
	editIntentAtomic
)

// undoTransaction is one undo entry (U:input/base/undo_manager.rs:17-35).
type undoTransaction struct {
	intent  editIntent
	changes []change
	// lastBatchLen is how many changes the most recent batch contributed;
	// only a following batch of the same length can coalesce.
	lastBatchLen int
	// recordedChanges counts recorded changes (a single-cursor typing run
	// merges into one change), deciding when the transaction is full.
	recordedChanges       int
	selectionsBefore      []cursorSelection
	selectionsAfter       []cursorSelection
	autoClosedPairsBefore any
	autoClosedPairsAfter  any
}

// pendingTransaction collects changes between beginTransaction and the
// matching commitTransaction (U:input/base/undo_manager.rs:37-47).
type pendingTransaction struct {
	intent                editIntent
	changes               []change
	selectionsBefore      []cursorSelection
	selectionsAfter       []cursorSelection
	autoClosedPairsBefore any
	autoClosedPairsAfter  any
}

// replay is one transaction handed back to be replayed, with the cursors
// to restore afterwards (U:input/base/undo_manager.rs:49-55).
type replay struct {
	changes         []change
	selections      []cursorSelection
	autoClosedPairs any
}

// undoManager coordinates undo and redo as explicit editing transactions;
// compatible adjacent transactions coalesce until a boundary, and
// begin/commit brackets nest (U:input/base/undo_manager.rs:57-74).
type undoManager struct {
	undoTransactions   []*undoTransaction
	redoTransactions   []*undoTransaction
	ignoring           bool
	transactionDepth   int
	pending            *pendingTransaction
	pendingIntent      *editIntent
	coalescingBoundary bool
}

// newUndoManager is UndoManager::new (U:input/base/undo_manager.rs:77-87).
func newUndoManager() *undoManager { return &undoManager{} }

// takePendingIntent takes the intent requested for the next recorded change
// (U:input/base/undo_manager.rs:89-93).
func (m *undoManager) takePendingIntent() (editIntent, bool) {
	if m.pendingIntent == nil {
		return 0, false
	}
	intent := *m.pendingIntent
	m.pendingIntent = nil
	return intent, true
}

// setPendingIntent requests the intent for the next recorded change
// (U:input/base/undo_manager.rs:95-98).
func (m *undoManager) setPendingIntent(intent editIntent) { m.pendingIntent = &intent }

// recordTransaction records one change; a no-op change only breaks
// coalescing. Reports whether the change was recorded
// (U:input/base/undo_manager.rs:100-117).
func (m *undoManager) recordTransaction(c change, intent editIntent) bool {
	if m.ignoring {
		return false
	}
	if c.tokenDelta == nil && c.oldRange == c.newRange && c.oldText == c.newText {
		m.breakTransactionCoalescing()
		return false
	}
	if m.pending != nil {
		m.pending.changes = append(m.pending.changes, c)
	} else {
		m.pushBatch([]change{c}, intent)
	}
	return true
}

// beginTransaction opens an atomic transaction
// (U:input/base/undo_manager.rs:119-122).
func (m *undoManager) beginTransaction() { m.beginTransactionWith(editIntentAtomic) }

// beginTransactionWith opens a transaction committing with intent; only the
// outermost bracket decides the intent (U:input/base/undo_manager.rs:124-142).
func (m *undoManager) beginTransactionWith(intent editIntent) {
	m.transactionDepth++
	if m.transactionDepth == 1 {
		m.pending = &pendingTransaction{intent: intent}
	}
}

// commitTransaction closes one bracket; the outermost pushes the collected
// batch unless it is empty or a no-op (U:input/base/undo_manager.rs:144-179).
func (m *undoManager) commitTransaction() {
	if m.transactionDepth == 0 {
		return
	}
	m.transactionDepth--
	if m.transactionDepth > 0 {
		return
	}
	pending := m.pending
	m.pending = nil
	if pending == nil {
		return
	}
	if len(pending.changes) == 0 || isNoopBatch(pending.changes) {
		return
	}
	m.pushBatch(pending.changes, pending.intent)
	if pending.selectionsBefore != nil {
		m.recordSelectionsBefore(pending.selectionsBefore)
	}
	if pending.selectionsAfter != nil {
		m.recordSelectionsAfter(pending.selectionsAfter)
	}
	if n := len(m.undoTransactions); n > 0 {
		transaction := m.undoTransactions[n-1]
		if pending.autoClosedPairsBefore != nil && transaction.autoClosedPairsBefore == nil {
			transaction.autoClosedPairsBefore = pending.autoClosedPairsBefore
		}
		if pending.autoClosedPairsAfter != nil {
			transaction.autoClosedPairsAfter = pending.autoClosedPairsAfter
		}
	}
}

// pushBatch pushes one logical edit onto the undo stack, coalescing it into
// the previous transaction when compatible (U:input/base/undo_manager.rs:181-234).
func (m *undoManager) pushBatch(changes []change, intent editIntent) {
	if len(changes) == 0 {
		return
	}
	m.redoTransactions = m.redoTransactions[:0]
	var previous *undoTransaction
	if n := len(m.undoTransactions); n > 0 {
		previous = m.undoTransactions[n-1]
	}
	canCoalesce := !m.coalescingBoundary && intent != editIntentAtomic && previous != nil &&
		previous.intent == intent &&
		previous.lastBatchLen == len(changes) &&
		previous.recordedChanges+len(changes) <= maxChangesPerTransaction &&
		isAdjacentBatch(intent, previous.trailingBatch(), changes)

	if canCoalesce {
		previous.lastBatchLen = len(changes)
		previous.recordedChanges += len(changes)
		// Adjacent single-cursor keystrokes stay one contiguous insertion.
		if intent == editIntentTyping && len(changes) == 1 && len(previous.changes) > 0 {
			last := &previous.changes[len(previous.changes)-1]
			last.newText += changes[0].newText
			last.newRange.End = changes[0].newRange.End
			return
		}
		previous.changes = append(previous.changes, changes...)
		return
	}

	if len(m.undoTransactions) >= maxUndoTransactions {
		m.undoTransactions = m.undoTransactions[1:]
	}
	m.undoTransactions = append(m.undoTransactions, &undoTransaction{
		intent:          intent,
		lastBatchLen:    len(changes),
		recordedChanges: len(changes),
		changes:         changes,
	})
	m.coalescingBoundary = intent == editIntentAtomic
}

// recordSelections records the cursors around the open transaction, or the
// most recent one; the first before snapshot wins
// (U:input/base/undo_manager.rs:236-251).
func (m *undoManager) recordSelections(before, after []cursorSelection) {
	if m.ignoring {
		return
	}
	m.recordSelectionsBefore(before)
	m.recordSelectionsAfter(after)
}

// recordAutoClosedPairs is U:input/base/undo_manager.rs:253-267.
func (m *undoManager) recordAutoClosedPairs(before, after any) {
	if m.ignoring {
		return
	}
	if m.pending != nil {
		if m.pending.autoClosedPairsBefore == nil {
			m.pending.autoClosedPairsBefore = before
		}
	} else if t := m.lastUndo(); t != nil && t.autoClosedPairsBefore == nil {
		t.autoClosedPairsBefore = before
	}
	m.recordAutoClosedPairsAfter(after)
}

// recordAutoClosedPairsAfter is U:input/base/undo_manager.rs:269-278.
func (m *undoManager) recordAutoClosedPairsAfter(after any) {
	if m.ignoring {
		return
	}
	if m.pending != nil {
		m.pending.autoClosedPairsAfter = after
	} else if t := m.lastUndo(); t != nil {
		t.autoClosedPairsAfter = after
	}
}

// recordSelectionsBefore is U:input/base/undo_manager.rs:280-286.
func (m *undoManager) recordSelectionsBefore(before []cursorSelection) {
	if m.pending != nil {
		if m.pending.selectionsBefore == nil {
			m.pending.selectionsBefore = before
		}
	} else if t := m.lastUndo(); t != nil && t.selectionsBefore == nil {
		t.selectionsBefore = before
	}
}

// recordSelectionsAfter is U:input/base/undo_manager.rs:288-294.
func (m *undoManager) recordSelectionsAfter(after []cursorSelection) {
	if m.pending != nil {
		m.pending.selectionsAfter = after
	} else if t := m.lastUndo(); t != nil {
		t.selectionsAfter = after
	}
}

// lastUndo is `undo_transactions.last_mut()`.
func (m *undoManager) lastUndo() *undoTransaction {
	if n := len(m.undoTransactions); n > 0 {
		return m.undoTransactions[n-1]
	}
	return nil
}

// hasOpenTransaction reports an open begin bracket
// (U:input/base/undo_manager.rs:296-299).
func (m *undoManager) hasOpenTransaction() bool { return m.transactionDepth > 0 }

// breakTransactionCoalescing commits any open brackets (only when the
// caller owns none) and stops the next edit coalescing
// (U:input/base/undo_manager.rs:301-308).
func (m *undoManager) breakTransactionCoalescing() {
	if m.transactionDepth == 0 {
		m.commitAllTransactions()
	}
	m.coalescingBoundary = true
}

// commitAllTransactions closes every open bracket
// (U:input/base/undo_manager.rs:310-317).
func (m *undoManager) commitAllTransactions() {
	if m.transactionDepth == 0 {
		return
	}
	m.transactionDepth = 1
	m.commitTransaction()
}

func (m *undoManager) isIgnoring() bool { return m.ignoring } // U:input/base/undo_manager.rs:319-321

// setIgnoring is U:input/base/undo_manager.rs:323-328.
func (m *undoManager) setIgnoring(ignoring bool) {
	m.ignoring = ignoring
	if ignoring {
		m.commitAllTransactions()
	}
}

// clear is U:input/base/undo_manager.rs:330-337.
func (m *undoManager) clear() {
	m.undoTransactions = nil
	m.redoTransactions = nil
	m.transactionDepth = 0
	m.pending = nil
	m.pendingIntent = nil
	m.coalescingBoundary = false
}

// undo pops the latest transaction, returning its changes in reverse
// application order and the cursors from before it
// (U:input/base/undo_manager.rs:339-350).
func (m *undoManager) undo() (replay, bool) {
	m.commitAllTransactions()
	n := len(m.undoTransactions)
	if n == 0 {
		return replay{}, false
	}
	transaction := m.undoTransactions[n-1]
	m.undoTransactions = m.undoTransactions[:n-1]
	changes := make([]change, len(transaction.changes))
	for i, c := range transaction.changes {
		changes[len(changes)-1-i] = c
	}
	r := replay{
		changes:         changes,
		selections:      cloneSelections(transaction.selectionsBefore),
		autoClosedPairs: transaction.autoClosedPairsBefore,
	}
	m.redoTransactions = append(m.redoTransactions, transaction)
	m.coalescingBoundary = true
	return r, true
}

// redo pops the latest undone transaction, returning its changes and the
// cursors from after it (U:input/base/undo_manager.rs:352-363).
func (m *undoManager) redo() (replay, bool) {
	m.commitAllTransactions()
	n := len(m.redoTransactions)
	if n == 0 {
		return replay{}, false
	}
	transaction := m.redoTransactions[n-1]
	m.redoTransactions = m.redoTransactions[:n-1]
	r := replay{
		changes:         append([]change(nil), transaction.changes...),
		selections:      cloneSelections(transaction.selectionsAfter),
		autoClosedPairs: transaction.autoClosedPairsAfter,
	}
	m.undoTransactions = append(m.undoTransactions, transaction)
	m.coalescingBoundary = true
	return r, true
}

// cloneSelections clones an optional snapshot, keeping nil (None) nil.
func cloneSelections(s []cursorSelection) []cursorSelection {
	if s == nil {
		return nil
	}
	return append([]cursorSelection{}, s...)
}

// trailingBatch is the changes of the most recent batch
// (U:input/base/undo_manager.rs:376-382).
func (t *undoTransaction) trailingBatch() []change {
	return t.changes[len(t.changes)-t.lastBatchLen:]
}

// isNoopBatch reports a chain of changes, each rewriting the region the
// previous produced, that leaves the document as it was found
// (U:input/base/undo_manager.rs:384-410).
func isNoopBatch(changes []change) bool {
	for _, c := range changes {
		if c.tokenDelta != nil {
			return false
		}
	}
	if len(changes) == 0 {
		return true
	}
	first, last := changes[0], changes[0]
	for _, c := range changes[1:] {
		if last.newRange.Start != c.oldRange.Start || last.newRange.End != c.oldRange.End {
			return false
		}
		last = c
	}
	return first.oldRange.Start == last.newRange.Start &&
		first.oldRange.End == last.newRange.End &&
		first.oldText == last.newText
}

// isAdjacentBatch reports every change of current continuing the change at
// the same position in previous, after shifting each previous change by
// the ones that follow it (U:input/base/undo_manager.rs:412-435).
func isAdjacentBatch(intent editIntent, previous, current []change) bool {
	if len(previous) != len(current) {
		return false
	}
	shifts := make([]int, len(previous))
	shift := 0
	for i := len(previous) - 1; i >= 0; i-- {
		shifts[i] = shift
		shift += len(previous[i].newText) - len(previous[i].oldText)
	}
	for i := range previous {
		if !isAdjacent(intent, previous[i].shifted(shifts[i]), current[i]) {
			return false
		}
	}
	return true
}

// isAdjacent is U:input/base/undo_manager.rs:437-461.
func isAdjacent(intent editIntent, previous, current change) bool {
	if previous.tokenDelta != nil || current.tokenDelta != nil {
		return false
	}
	switch intent {
	case editIntentTyping:
		return previous.oldRange.IsEmpty() &&
			current.oldRange.IsEmpty() &&
			!strings.ContainsAny(previous.newText, "\n\r") &&
			!strings.ContainsAny(current.newText, "\n\r") &&
			previous.newRange.End == current.oldRange.Start
	case editIntentBackspace:
		return previous.newText == "" &&
			current.newText == "" &&
			current.oldRange.End == previous.oldRange.Start
	case editIntentDeleteForward:
		return previous.newText == "" &&
			current.newText == "" &&
			current.oldRange.Start == previous.oldRange.Start
	}
	return false
}
