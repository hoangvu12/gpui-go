package base

// Editing, history, clipboard and the IME handler of the input engine
// (U:input/base/state.rs:1549-2271, 2612-2871, 3586-4346), plus the
// stand-ins for modules later tickets port (see input_state.go).
//
// Code-editor-only branches whose data joins with the editor tickets are
// not ported here: the language-config smart indent of
// indent_of_next_line_at (U:input/base/state.rs:1588-1625), automatic
// closing (auto_close_target / skip_over_target / the pair deletion of
// backspace, U:input/base/state.rs:1685-1795, 1808-1839, 3937-3956,
// 3986-4003), the bracket split of enter (U:input/base/state.rs:
// 1966-2021) and fold-candidate maintenance (U:input/base/state.rs:
// 3810-3853). LayoutMode::is_auto_close / is_smart_indent are false
// outside a code editor, so a plain input and a textarea never reach
// them.

import (
	"slices"
	"sort"
	"strings"
	"unicode"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/base/sumtree"
	"github.com/hoangvu12/gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// Stand-ins for later tickets (touch selection, search)
// ---------------------------------------------------------------------------

// dismissTouchSelection drops the touch handles and edit menu; with no
// touch selection it is a no-op (U:input/base/touch.rs:86-92).
func (s *InputBaseState[M]) dismissTouchSelection(*inputCx[M]) {}

// updateSearch keeps the search matches in step with an edit; an input
// with no active search session skips it (U:input/editor/search.rs:
// 272-277).
func (s *InputBaseState[M]) updateSearch(*gpui.App) {}

// onActionSearch leaves the shortcut to the ancestors of an input that is
// not searchable (U:input/editor/search.rs:286-292). Opening the search
// panel joins with the search port.
func (s *InputBaseState[M]) onActionSearch(_ *Search, _ *gpui.Window, cx *inputCx[M]) {
	if !s.searchable {
		cx.Propagate()
	}
}

// onActionReplace is U:input/editor/search.rs:294-305 (see onActionSearch).
func (s *InputBaseState[M]) onActionReplace(_ *Replace, _ *gpui.Window, cx *inputCx[M]) {
	if !s.searchable {
		cx.Propagate()
	}
}

// ---------------------------------------------------------------------------
// selection.rs: select_word / select_line
// ---------------------------------------------------------------------------

// selectWord selects the word at offset on double-click; a masked value
// has no word boundaries, so it takes all of it
// (U:input/base/selection.rs:22-43).
func (s *InputBaseState[M]) selectWord(offset int, _ *gpui.Window, cx *inputCx[M]) {
	start, end := 0, s.text.Len()
	if !s.masked {
		var ok bool
		start, end, ok = textSelectorWordRange(&s.text, offset)
		if !ok {
			return
		}
	}
	s.undoManager.breakTransactionCoalescing()
	s.selections.removeAllButActive()
	s.setSelection(start, end)
	word := s.activeSelection()
	s.selectedWordRange = &word
	cx.Notify()
}

// selectLine selects the line at offset on triple-click
// (U:input/base/selection.rs:45-55).
func (s *InputBaseState[M]) selectLine(offset int, _ *gpui.Window, cx *inputCx[M]) {
	start, end := textSelectorLineRange(&s.text, offset)
	s.undoManager.breakTransactionCoalescing()
	s.selections.removeAllButActive()
	s.setSelection(start, end)
	s.selectedWordRange = nil
	cx.Notify()
}

// ---------------------------------------------------------------------------
// Editing actions
// ---------------------------------------------------------------------------

// indentOfNextLine is U:input/base/state.rs:1546-1551.
func (s *InputBaseState[M]) indentOfNextLine() string { return s.indentOfNextLineAt(s.Cursor()) }

// indentOfNextLineAt is the indent of the line after offset: the deeper
// of the current and next lines' leading whitespace
// (U:input/base/state.rs:1553-1628; the language-config smart indent
// joins with the editor ticket).
func (s *InputBaseState[M]) indentOfNextLineAt(offset int) string {
	if s.IsSingleLine() {
		return ""
	}
	affinity := s.lineEndAffinityAt(offset)
	leading := func(from int) string {
		var b strings.Builder
		it := s.text.CharsAt(from)
		for c, ok := it.Next(); ok; c, ok = it.Next() {
			if !unicode.IsSpace(c) || c == '\n' || c == '\r' {
				break
			}
			b.WriteRune(c)
		}
		return b.String()
	}
	currentIndent := leading(s.startOfLineAt(offset, affinity))
	nextIndent := leading(s.endOfLineAt(offset, affinity))
	if len(nextIndent) > len(currentIndent) {
		return nextIndent
	}
	return currentIndent
}

// deleteSelections deletes every selection as one batch; collapsed
// cursors expand through collapsedTarget first
// (U:input/base/state.rs:1630-1683).
func (s *InputBaseState[M]) deleteSelections(silent bool, collapsedIntent editIntent, collapsedTarget func(offset int) InputSelection, w *gpui.Window, cx *inputCx[M]) {
	if !s.IsEditable() {
		return
	}
	cursors := slices.Collect(s.selections.iter())
	intent := collapsedIntent
	for _, sel := range cursors {
		if !sel.isEmpty() {
			intent = editIntentAtomic
			break
		}
	}
	next := make([]cursorSelection, 0, len(cursors))
	for _, sel := range cursors {
		rng := NewInputSelection(sel.start, sel.end)
		if sel.isEmpty() {
			rng = collapsedTarget(sel.cursorOffset())
		}
		sel.start, sel.end = min(rng.Start, rng.End), max(rng.Start, rng.End)
		next = append(next, sel)
	}
	// Capture the user's selections before expanding or merging deletion
	// ranges.
	s.undoManager.beginTransactionWith(intent)
	s.undoManager.recordSelections(cloneSelections(cursors), cloneSelections(cursors))
	s.selections.replaceAll(next)
	s.undoManager.setPendingIntent(intent)

	wasSilent := s.silentReplaceText
	s.silentReplaceText = silent
	s.ReplaceTextInRange(nil, "", w, cx)
	s.silentReplaceText = wasSilent
	s.undoManager.recordSelections(cursors, slices.Collect(s.selections.iter()))
	s.undoManager.commitTransaction()
	s.pauseBlinkCursor(cx)
}

// backspace deletes backwards; a lone cursor at the start propagates the
// keystroke (U:input/base/state.rs:1797-1848).
func (s *InputBaseState[M]) backspace(_ *Backspace, w *gpui.Window, cx *inputCx[M]) {
	if s.selections.isSingle() && s.activeSelection().isEmpty() && s.Cursor() == 0 {
		cx.Propagate()
		return
	}
	s.deleteSelections(false, editIntentBackspace, func(offset int) InputSelection {
		return NewInputSelection(s.previousBoundary(offset), offset)
	}, w, cx)
}

// delete deletes forwards (U:input/base/state.rs:1850-1858).
func (s *InputBaseState[M]) delete(_ *Delete, w *gpui.Window, cx *inputCx[M]) {
	s.deleteSelections(false, editIntentDeleteForward, func(offset int) InputSelection {
		return NewInputSelection(offset, s.nextBoundary(offset))
	}, w, cx)
}

// deleteToBeginningOfLine is U:input/base/state.rs:1860-1879.
func (s *InputBaseState[M]) deleteToBeginningOfLine(_ *DeleteToBeginningOfLine, w *gpui.Window, cx *inputCx[M]) {
	s.deleteSelections(true, editIntentAtomic, func(offset int) InputSelection {
		start := s.startOfLineAt(offset, s.lineEndAffinityAt(offset))
		if start == offset {
			start = max(start-1, 0)
		}
		return NewInputSelection(start, offset)
	}, w, cx)
}

// deleteToEndOfLine is U:input/base/state.rs:1881-1900.
func (s *InputBaseState[M]) deleteToEndOfLine(_ *DeleteToEndOfLine, w *gpui.Window, cx *inputCx[M]) {
	s.deleteSelections(true, editIntentAtomic, func(offset int) InputSelection {
		end := s.endOfLineAt(offset, s.lineEndAffinityAt(offset))
		if end == offset {
			end = min(end+1, s.text.Len())
		}
		return NewInputSelection(offset, end)
	}, w, cx)
}

// deletePreviousWord is U:input/base/state.rs:1902-1915.
func (s *InputBaseState[M]) deletePreviousWord(_ *DeleteToPreviousWordStart, w *gpui.Window, cx *inputCx[M]) {
	s.deleteSelections(true, editIntentAtomic, func(offset int) InputSelection {
		return NewInputSelection(s.previousStartOfWordAt(offset), offset)
	}, w, cx)
}

// deleteNextWord is U:input/base/state.rs:1917-1930.
func (s *InputBaseState[M]) deleteNextWord(_ *DeleteToNextWordEnd, w *gpui.Window, cx *inputCx[M]) {
	s.deleteSelections(true, editIntentAtomic, func(offset int) InputSelection {
		return NewInputSelection(offset, s.nextEndOfWordAt(offset))
	}, w, cx)
}

// enter inserts a newline in a multi-line input; a single line (or submit
// on Enter) propagates and only emits PressEnter
// (U:input/base/state.rs:1932-2047).
func (s *InputBaseState[M]) enter(action *Enter, w *gpui.Window, cx *inputCx[M]) {
	if s.hooks.handleContextMenuAction(s, EnterAction.Box(*action), w, cx) {
		return
	}
	// The user chose not to accept the inline completion.
	if s.hooks.hasInlineCompletion(s) {
		s.hooks.clearInlineCompletion(s, cx)
	}

	insertNewline := s.IsMultiLine() && (!s.submitOnEnter || action.Shift)
	if insertNewline {
		if !s.selections.isSingle() {
			// A newline (with per-line indent) at every cursor.
			s.selections.mergeOverlapping()
			sels := slices.Collect(s.selections.iter())
			edits := make([]inputTextEdit, 0, len(sels))
			for _, sel := range sels {
				indent := ""
				if s.IsCodeEditor() {
					indent = s.indentOfNextLineAt(sel.cursorOffset())
				}
				edits = append(edits, inputTextEdit{rng: NewInputSelection(sel.start, sel.end), text: "\n" + indent})
			}
			sort.SliceStable(edits, func(i, j int) bool { return edits[i].rng.Start < edits[j].rng.Start })
			s.replaceTextInRanges(edits, w, cx)
			s.pauseBlinkCursor(cx)
		} else {
			indent := ""
			if s.IsCodeEditor() {
				indent = s.indentOfNextLine()
			}
			s.replaceTextInRangeSilent(nil, "\n"+indent, w, cx)
			s.pauseBlinkCursor(cx)
		}
	} else {
		// Single line or submit-on-enter: just emit the event.
		s.undoManager.breakTransactionCoalescing()
		cx.Propagate()
	}

	InputBaseStateEvents[M]().Emit(cx, InputEvent{Kind: InputEventPressEnter, Secondary: action.Secondary, Shift: action.Shift})
}

// Clean clears the text (U:input/base/state.rs:2049-2053).
func (s *InputBaseState[M]) Clean(w *gpui.Window, cx *inputCx[M]) {
	s.replaceText("", w, cx)
	s.setSelection(0, 0)
	s.scrollTo(0, nil, cx)
}

// escape dismisses, collapses extra cursors, cancels IME, cleans on
// escape, or propagates (U:input/base/state.rs:2055-2093).
func (s *InputBaseState[M]) escape(action *Escape, w *gpui.Window, cx *inputCx[M]) {
	if s.hooks.handleContextMenuAction(s, EscapeAction.Box(*action), w, cx) {
		return
	}
	// Escape also dismisses a request whose popup has not arrived yet.
	s.hooks.hideContextMenu(s, cx)

	if !s.selections.isSingle() {
		s.undoManager.breakTransactionCoalescing()
		s.selections.removeAllButActive()
		cx.Notify()
		return
	}

	if s.hooks.hasInlineCompletion(s) {
		s.hooks.clearInlineCompletion(s, cx)
		return
	}
	s.hooks.clearInlineCompletion(s, cx)

	// No touch selection exists until the touch port (touch.rs); the
	// dismiss step of U:input/base/state.rs:2078-2082 never fires.

	if s.imeMarkedRange != nil {
		s.UnmarkText(w, cx)
	}
	if s.cleanOnEscape {
		s.Clean(w, cx)
		return
	}
	cx.Propagate()
}

// addCursorAbove is U:input/base/state.rs:2126-2133.
func (s *InputBaseState[M]) addCursorAbove(_ *AddCursorAbove, _ *gpui.Window, cx *inputCx[M]) {
	s.addCursorVertical(-1, cx)
}

// addCursorBelow is U:input/base/state.rs:2135-2142.
func (s *InputBaseState[M]) addCursorBelow(_ *AddCursorBelow, _ *gpui.Window, cx *inputCx[M]) {
	s.addCursorVertical(1, cx)
}

// addCursorVertical adds a cursor one display row above or below each
// cursor, keeping the column; duplicates and stuck cursors are skipped
// (U:input/base/state.rs:2144-2190).
func (s *InputBaseState[M]) addCursorVertical(moveLines int, cx *inputCx[M]) {
	if !s.IsMultiLine() {
		return
	}
	s.pauseBlinkCursor(cx)
	s.undoManager.breakTransactionCoalescing()

	type source struct {
		offset   int
		anchor   cursorColumnAnchor
		affinity bool
	}
	var sources []source
	offsets := map[int]bool{}
	for sel := range s.selections.iter() {
		sources = append(sources, source{sel.cursorOffset(), sel.columnAnchor, s.lineEndAffinityFor(sel)})
		offsets[sel.cursorOffset()] = true
	}

	newest, hasNewest := 0, false
	for _, src := range sources {
		anchor := src.anchor
		if !anchor.ok {
			anchor = s.preferredColumnFor(src.offset)
		}
		target, _ := s.verticalTarget(src.offset, anchor, src.affinity, moveLines)
		if target == src.offset || offsets[target] {
			continue
		}
		offsets[target] = true
		cursor := newCursorSelection(s.selections.generateID(), target, target)
		cursor.columnAnchor = anchor
		s.selections.add(cursor)
		newest, hasNewest = target, true
	}
	if hasNewest {
		s.scrollTo(newest, nil, cx)
	}
	cx.Notify()
}

// addCursorAt adds a collapsed cursor at offset, rejected inside an
// existing selection or on an existing cursor
// (U:input/base/state.rs:2192-2216).
func (s *InputBaseState[M]) addCursorAt(offset int, cx *inputCx[M]) {
	if !s.IsMultiLine() {
		return
	}
	for sel := range s.selections.iter() {
		if sel.contains(offset) || (sel.isCollapsed() && sel.cursorOffset() == offset) {
			return
		}
	}
	s.undoManager.breakTransactionCoalescing()
	sel := newCursorSelection(s.selections.generateID(), offset, offset)
	sel.columnAnchor = s.preferredColumnFor(offset)
	s.selections.add(sel)
	cx.Notify()
}

// buildColumnarSelection builds a block selection, one selection per
// display row over the same column span
// (U:input/base/state.rs:2218-2256).
func (s *InputBaseState[M]) buildColumnarSelection(start, end columnarPoint, cx *inputCx[M]) {
	if !s.IsMultiLine() {
		return
	}
	s.undoManager.breakTransactionCoalescing()

	startRow, startCol := s.columnarRowColumn(start)
	endRow, endCol := s.columnarRowColumn(end)
	startRow, endRow = min(startRow, endRow), max(startRow, endRow)
	startCol, endCol = min(startCol, endCol), max(startCol, endCol)

	next := make([]cursorSelection, 0, endRow-startRow+1)
	for row := startRow; row <= endRow; row++ {
		selStart := s.displayMap.displayRowColumnToOffset(row, startCol)
		selEnd := s.displayMap.displayRowColumnToOffset(row, endCol)
		id := s.selections.generateID()
		selStart = s.ext().ClipOffset(selStart, sumtree.Left)
		selEnd = s.ext().ClipOffset(selEnd, sumtree.Left)
		next = append(next, newCursorSelection(id, selStart, selEnd))
	}
	s.selections.replaceAll(next)
	cx.Notify()
}

// columnarRowColumn resolves a columnar point to its display row and the
// column it reaches past the row's end (U:input/base/state.rs:2258-2271).
func (s *InputBaseState[M]) columnarRowColumn(p columnarPoint) (int, int) {
	displayPoint := s.displayMap.offsetToWrapDisplayPoint(p.offset)
	row, ok := s.displayMap.wrapRowToDisplayRow(displayPoint.row)
	if !ok {
		row = s.displayMap.nearestVisibleDisplayRow(displayPoint.row)
	}
	return row, displayPoint.column + p.columnsPastLineEnd
}

// showCharacterPalette is U:input/base/state.rs:2612-2619.
func (s *InputBaseState[M]) showCharacterPalette(_ *ShowCharacterPalette, w *gpui.Window, _ *inputCx[M]) {
	_ = w.ShowCharacterPalette()
}

// ---------------------------------------------------------------------------
// Clipboard
// ---------------------------------------------------------------------------

// selectedTexts is the text of every non-empty selection, in document
// order (U:input/base/state.rs:2621-2634).
func (s *InputBaseState[M]) selectedTexts() []string {
	var sels []cursorSelection
	for sel := range s.selections.iter() {
		if !sel.isEmpty() {
			sels = append(sels, sel)
		}
	}
	sort.SliceStable(sels, func(i, j int) bool { return sels[i].start < sels[j].start })
	texts := make([]string, 0, len(sels))
	for _, sel := range sels {
		texts = append(texts, s.text.Slice(sel.start, sel.end).String())
	}
	return texts
}

// copy copies the selections, one per line (U:input/base/state.rs:
// 2636-2644). The platform write's error is dropped like the pinned
// write_to_clipboard, which logs it.
func (s *InputBaseState[M]) copy(_ *Copy, _ *gpui.Window, cx *inputCx[M]) {
	if !s.IsCopyable() {
		return
	}
	_ = cx.App().WriteClipboard(gpui.NewClipboardStringItem(strings.Join(s.selectedTexts(), "\n")))
}

// cut copies then deletes the selections as one atomic edit
// (U:input/base/state.rs:2646-2657).
func (s *InputBaseState[M]) cut(_ *Cut, w *gpui.Window, cx *inputCx[M]) {
	if !s.IsCopyable() {
		return
	}
	_ = cx.App().WriteClipboard(gpui.NewClipboardStringItem(strings.Join(s.selectedTexts(), "\n")))
	s.undoManager.setPendingIntent(editIntentAtomic)
	s.replaceTextInRangeSilent(nil, "", w, cx)
}

// paste inserts the clipboard text (U:input/base/state.rs:2659-2666). A
// read failure is the pinned None.
func (s *InputBaseState[M]) paste(_ *Paste, w *gpui.Window, cx *inputCx[M]) {
	if !s.IsEditable() {
		return
	}
	item, err := cx.App().ReadClipboard()
	if err != nil || item == nil {
		return
	}
	s.insertClipboard(*item, w, cx)
}

// insertClipboard inserts clipboard text as one atomic edit: newlines are
// dropped on a single line, and a multi-line input with matching counts
// takes one line per selection; a clipboard without text is left alone
// (U:input/base/state.rs:2668-2712).
func (s *InputBaseState[M]) insertClipboard(item gpui.ClipboardItem, w *gpui.Window, cx *inputCx[M]) {
	newText, ok := item.Text()
	if !ok || newText == "" {
		return
	}
	// A paste is one atomic edit, never part of a typing run.
	s.undoManager.setPendingIntent(editIntentAtomic)

	if !s.IsMultiLine() {
		newText = strings.ReplaceAll(newText, "\n", "")
		s.replaceTextInRangeSilent(nil, newText, w, cx)
		s.scrollTo(s.Cursor(), nil, cx)
		return
	}

	if !s.selections.isSingle() {
		s.selections.mergeOverlapping()
	}
	lines := strings.Split(newText, "\n")
	count := s.selections.len()
	if count > 1 && len(lines) == count {
		sels := slices.Collect(s.selections.iter())
		sort.SliceStable(sels, func(i, j int) bool { return sels[i].start < sels[j].start })
		edits := make([]inputTextEdit, 0, count)
		for i, sel := range sels {
			edits = append(edits, inputTextEdit{rng: NewInputSelection(sel.start, sel.end), text: lines[i]})
		}
		s.replaceTextInRanges(edits, w, cx)
	} else {
		s.replaceTextInRangeSilent(nil, newText, w, cx)
	}
	s.scrollTo(s.Cursor(), nil, cx)
}

// ---------------------------------------------------------------------------
// History
// ---------------------------------------------------------------------------

// inputTextEdit is one (Range<usize>, String) edit of a batch.
type inputTextEdit struct {
	rng  InputSelection
	text string
}

// typingIntent is the intent of an unlabelled batch: insertions at
// collapsed cursors are typing (U:input/base/state.rs:2714-2725).
func typingIntent(edits []inputTextEdit, newText string) editIntent {
	if newText == "" || strings.ContainsAny(newText, "\n\r") {
		return editIntentAtomic
	}
	for _, e := range edits {
		if !e.rng.IsEmpty() {
			return editIntentAtomic
		}
	}
	return editIntentTyping
}

// collapseForIntent is where a cursor stood before an edit with intent:
// deletes collapse back to the side they came from
// (U:input/base/state.rs:2727-2743).
func collapseForIntent(intent editIntent, sel cursorSelection, rng InputSelection) cursorSelection {
	switch intent {
	case editIntentBackspace:
		sel.placeAt(rng.End, cursorColumnAnchor{})
	case editIntentDeleteForward:
		sel.placeAt(rng.Start, cursorColumnAnchor{})
	}
	return sel
}

// pushHistory records one change; reports whether it was recorded
// (U:input/base/state.rs:2745-2792). text is the pre-edit text.
func (s *InputBaseState[M]) pushHistory(text rope.Rope, rng InputSelection, newText string, requestedIntent *editIntent, selectionBefore cursorSelection, selectionAfter *cursorSelection) bool {
	s.documentRevision++
	delta := s.editTokens(rng, len(newText))
	if s.undoManager.isIgnoring() {
		return false
	}

	ext := RopeExtOf(&text)
	rng = NewInputSelection(ext.ClipOffset(rng.Start, sumtree.Left), ext.ClipOffset(rng.End, sumtree.Right))
	oldText := text.Slice(rng.Start, rng.End).String()
	newRange := NewInputSelection(rng.Start, rng.Start+len(newText))

	var intent editIntent
	switch {
	case requestedIntent != nil:
		intent = *requestedIntent
	case rng.IsEmpty() && oldText == "" && newText != "" && !strings.ContainsAny(newText, "\n\r"):
		intent = editIntentTyping
	default:
		intent = editIntentAtomic
	}

	selectionBefore = collapseForIntent(intent, selectionBefore, rng)
	after := newCursorSelection(0, newRange.End, newRange.End)
	if selectionAfter != nil {
		after = *selectionAfter
	}

	openTransaction := s.undoManager.hasOpenTransaction()
	c := newChange(rng, oldText, newRange, newText)
	// change.tokenDelta is `any` (input_change.go): store only a non-nil
	// delta so the undo manager's nil checks see None.
	if delta != nil {
		c.tokenDelta = delta
	}
	recorded := s.undoManager.recordTransaction(c, intent)
	// A batch records its own cursor sets; this covers a change that is a
	// transaction on its own.
	if recorded && !openTransaction {
		s.undoManager.recordSelections([]cursorSelection{selectionBefore}, []cursorSelection{after})
	}
	return recorded
}

// undo is U:input/base/state.rs:2794-2796.
func (s *InputBaseState[M]) undo(_ *Undo, w *gpui.Window, cx *inputCx[M]) {
	s.replayHistory(true, w, cx)
}

// redo is U:input/base/state.rs:2798-2800.
func (s *InputBaseState[M]) redo(_ *Redo, w *gpui.Window, cx *inputCx[M]) {
	s.replayHistory(false, w, cx)
}

// replayHistory replays one transaction backwards or forwards
// (U:input/base/state.rs:2802-2850). The auto-closed pairs restore joins
// with the editor auto-close port.
func (s *InputBaseState[M]) replayHistory(undo bool, w *gpui.Window, cx *inputCx[M]) {
	if !s.IsEditable() {
		return
	}
	var r replay
	var ok bool
	if undo {
		r, ok = s.undoManager.undo()
	} else {
		r, ok = s.undoManager.redo()
	}
	if !ok {
		return
	}
	tokenAware := s.inlineTokens != nil
	for _, c := range r.changes {
		if d, _ := c.tokenDelta.(*tokenDelta); d != nil {
			tokenAware = true
		}
	}
	emitEvents := s.emitEvents
	if tokenAware {
		s.emitEvents = false
	}
	s.replayingHistory = tokenAware
	s.undoManager.setIgnoring(true)
	for _, c := range r.changes {
		rng, text := c.oldRange, c.newText
		if undo {
			rng, text = c.newRange, c.oldText
		}
		rngUTF16 := s.rangeToUTF16(rng)
		s.replaceTextInRangeSilent(&rngUTF16, text, w, cx)
		d, _ := c.tokenDelta.(*tokenDelta)
		s.replayTokens(rng, len(text), d, undo)
	}
	s.restoreSelections(r.selections)
	if tokenAware {
		s.imeMarkedRange = nil
	}
	s.undoManager.setIgnoring(false)
	s.replayingHistory = false
	s.emitEvents = emitEvents
	if tokenAware && emitEvents {
		InputBaseStateEvents[M]().Emit(cx, InputEvent{Kind: InputEventChange})
	}
	cx.Notify()
}

// restoreSelections restores a transaction's cursors clamped to the text;
// nil leaves the replay's selections (U:input/base/state.rs:2852-2871).
func (s *InputBaseState[M]) restoreSelections(sels []cursorSelection) {
	if sels == nil {
		return
	}
	n := s.text.Len()
	restored := make([]cursorSelection, 0, len(sels))
	for _, sel := range sels {
		sel.start, sel.end = min(sel.start, n), min(sel.end, n)
		restored = append(restored, sel)
	}
	s.selections.replaceAll(restored)
	s.selections.mergeOverlapping()
}

// ---------------------------------------------------------------------------
// Replacement
// ---------------------------------------------------------------------------

// replaceTextInRangeSilent replaces without UI reactions such as
// completion (U:input/base/state.rs:3583-3596).
func (s *InputBaseState[M]) replaceTextInRangeSilent(rngUTF16 *gpui.UTF16Range, newText string, w *gpui.Window, cx *inputCx[M]) {
	s.silentReplaceText = true
	s.ReplaceTextInRange(rngUTF16, newText, w, cx)
	s.silentReplaceText = false
}

// replaceTextInRanges applies a batch of edits (byte ranges in the
// pre-edit document) as one history transaction
// (U:input/base/state.rs:3598-3808). The token-span merge, highlighter,
// fold and diagnostic steps join with their tickets.
func (s *InputBaseState[M]) replaceTextInRanges(edits []inputTextEdit, w *gpui.Window, cx *inputCx[M]) {
	if !s.IsEditable() || len(edits) == 0 {
		return
	}
	// Every edit invalidates provider responses for the previous document.
	s.hooks.hideContextMenu(s, cx)
	s.hooks.clearInlineCompletion(s, cx)

	// Descending by start, so applying front-first edits the highest
	// offsets first.
	sorted := make([]inputTextEdit, 0, len(edits))
	for _, e := range edits {
		sorted = append(sorted, inputTextEdit{rng: s.normalizeTokenRange(e.rng), text: e.text})
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].rng.Start > sorted[j].rng.Start })
	// Ranges widened over one token merge (U:input/base/state.rs:3624-3638).
	if len(s.tokenSpans()) > 0 {
		merged := make([]inputTextEdit, 0, len(sorted))
		for _, e := range sorted {
			if n := len(merged); n > 0 && (e.rng.End > merged[n-1].rng.Start || e.rng == merged[n-1].rng) {
				last := &merged[n-1]
				last.rng = NewInputSelection(min(e.rng.Start, last.rng.Start), max(e.rng.End, last.rng.End))
				continue
			}
			merged = append(merged, e)
		}
		sorted = merged
	}

	var requestedIntent *editIntent
	if intent, ok := s.undoManager.takePendingIntent(); ok {
		requestedIntent = &intent
	}
	selectionBefore := s.activeSelection()
	originalSelections := slices.Collect(s.selections.iter())
	collapseIntent := editIntentAtomic
	if requestedIntent != nil {
		collapseIntent = *requestedIntent
	}
	selectionsBefore := make([]cursorSelection, 0, len(originalSelections))
	for _, sel := range originalSelections {
		selectionsBefore = append(selectionsBefore, collapseForIntent(collapseIntent, sel, NewInputSelection(sel.start, sel.end)))
	}
	group := len(sorted) > 1
	if group {
		s.undoManager.beginTransaction()
	}

	recorded := false
	var highlighterEdits []inputEditWithText
	for _, e := range sorted {
		oldText := s.text
		s.ext().Replace(e.rng.Start, e.rng.End, e.text)

		s.hooks.adjustAnnotations(s, e.rng, len(e.text))
		if s.pushHistory(oldText, e.rng, e.text, requestedIntent, selectionBefore, nil) {
			recorded = true
		}
		// Incremental, single-range updates run per edit
		// (U:input/base/state.rs:3693-3697).
		s.displayMap.adjustFoldsForEdit(oldText, e.rng, e.text)
		s.displayMap.onTextChanged(s.text, e.rng, e.text, w)

		if group {
			if s.IsCodeEditor() {
				edit := replacementInputEdit(oldText, s.text, e.rng, e.text)
				highlighterEdits = append(highlighterEdits, inputEditWithText{edit: edit, text: s.text})
			}
		} else if s.IsCodeEditor() {
			s.hooks.driveHighlighter(s, replacementInputEdit(oldText, s.text, e.rng, e.text), s.text, s.mode.isFolding(), w, cx)
		}
	}
	if group {
		if len(highlighterEdits) > 0 {
			s.hooks.driveHighlighterBatch(s, highlighterEdits, s.mode.isFolding(), w, cx)
		}
		s.undoManager.commitTransaction()
	}

	s.hooks.refreshLanguageFeatures(s, w, cx)
	s.updateSearch(cx.App())

	// One collapsed cursor per edit, at the end of its inserted text.
	ascending := slices.Clone(sorted)
	sort.SliceStable(ascending, func(i, j int) bool { return ascending[i].rng.Start < ascending[j].rng.Start })
	textLen := s.text.Len()
	delta := 0
	type editResult struct {
		rng    InputSelection
		offset int
	}
	results := make([]editResult, 0, len(ascending))
	for _, e := range ascending {
		offset := min(e.rng.Start+delta+len(e.text), textLen)
		results = append(results, editResult{rng: e.rng, offset: offset})
		delta += len(e.text) - (e.rng.End - e.rng.Start)
	}

	used := make([]bool, len(results))
	next := make([]cursorSelection, 0, len(ascending))
	for _, sel := range originalSelections {
		index, found := sort.Find(len(results), func(i int) int {
			r := results[i].rng
			switch {
			case sel.start != r.Start:
				return compareInts(sel.start, r.Start)
			default:
				return compareInts(sel.end, r.End)
			}
		})
		if found && !used[index] {
			used[index] = true
			sel.placeAt(results[index].offset, cursorColumnAnchor{})
			next = append(next, sel)
		}
	}
	for index, r := range results {
		if !used[index] {
			next = append(next, newCursorSelection(s.selections.generateID(), r.offset, r.offset))
		}
	}
	s.selections.replaceAll(next)
	s.selections.mergeOverlapping()

	selectionsAfter := slices.Collect(s.selections.iter())
	if recorded {
		s.undoManager.recordSelections(selectionsBefore, selectionsAfter)
	}

	s.imeMarkedRange = nil
	s.updatePreferredColumn()
	s.dismissTouchSelection(cx)
	if s.IsMultiLine() {
		s.mode.updateAutoGrow(s.displayMap.wrapRowCount())
	}
	if s.emitEvents {
		InputBaseStateEvents[M]().Emit(cx, InputEvent{Kind: InputEventChange})
	}
	cx.Notify()
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// EntityInputHandler (U:input/base/state.rs:3856-4346)
// ---------------------------------------------------------------------------

// TextForRange returns the text of a UTF-16 range and the range actually
// used (U:input/base/state.rs:3857-3867).
func (s *InputBaseState[M]) TextForRange(rngUTF16 gpui.UTF16Range, _ *gpui.Window, _ *inputCx[M]) (string, *gpui.UTF16Range, bool) {
	rng := s.rangeFromUTF16(rngUTF16)
	adjusted := s.rangeToUTF16(rng)
	return s.text.Slice(rng.Start, rng.End).String(), &adjusted, true
}

// SelectedTextRange is the active selection in UTF-16 units
// (U:input/base/state.rs:3869-3879).
func (s *InputBaseState[M]) SelectedTextRange(_ bool, _ *gpui.Window, _ *inputCx[M]) (gpui.UTF16Selection, bool) {
	return gpui.UTF16Selection{
		Range:    s.rangeToUTF16(s.SelectedRange()),
		Reversed: s.activeSelection().reversed,
	}, true
}

// MarkedTextRange is the IME composition range in UTF-16 units
// (U:input/base/state.rs:3881-3888).
func (s *InputBaseState[M]) MarkedTextRange(_ *gpui.Window, _ *inputCx[M]) (gpui.UTF16Range, bool) {
	if s.imeMarkedRange == nil {
		return gpui.UTF16Range{}, false
	}
	return s.rangeToUTF16(NewInputSelection(s.imeMarkedRange.start, s.imeMarkedRange.end)), true
}

// UnmarkText ends the composition and its transaction
// (U:input/base/state.rs:3890-3893).
func (s *InputBaseState[M]) UnmarkText(_ *gpui.Window, _ *inputCx[M]) {
	s.imeMarkedRange = nil
	s.undoManager.commitTransaction()
}

// ReplaceTextInRange replaces a UTF-16 range (nil: the IME marked range,
// else the selection) with newText; invalid text is rejected
// (U:input/base/state.rs:3895-4135).
func (s *InputBaseState[M]) ReplaceTextInRange(rngUTF16 *gpui.UTF16Range, newText string, w *gpui.Window, cx *inputCx[M]) {
	var requestedIntent *editIntent
	if intent, ok := s.undoManager.takePendingIntent(); ok {
		requestedIntent = &intent
	}
	if !s.IsEditable() {
		return
	}
	selectionBefore := s.activeSelection()
	// Committing a composition ends the transaction it opened.
	endsComposition := s.imeMarkedRange != nil

	s.pauseBlinkCursor(cx)

	// Normalization keeps the UTF-16 length but may change the UTF-8 byte
	// length, so every byte offset below uses the normalized text.
	if !s.replayingHistory {
		newText = s.normalizeInput(newText)
	}

	rng := s.resolveReplacementRange(rngUTF16)
	rng = s.normalizeTokenRange(rng)

	if s.IsMultiLine() {
		multiCursor := rngUTF16 == nil && s.imeMarkedRange == nil && !s.selections.isSingle()
		if multiCursor {
			s.selections.mergeOverlapping()
			var edits []inputTextEdit
			for sel := range s.selections.iter() {
				edits = append(edits, inputTextEdit{rng: NewInputSelection(sel.start, sel.end), text: newText})
			}
			sort.SliceStable(edits, func(i, j int) bool { return edits[i].rng.Start < edits[j].rng.Start })
			// One keystroke across several cursors is one batch with the
			// keystroke's intent, so a run of them coalesces.
			intent := typingIntent(edits, newText)
			if requestedIntent != nil {
				intent = *requestedIntent
			}
			s.undoManager.beginTransactionWith(intent)
			s.undoManager.setPendingIntent(intent)
			s.replaceTextInRanges(edits, w, cx)
			s.undoManager.commitTransaction()
		} else {
			if rngUTF16 != nil {
				s.selections.removeAllButActive()
			}
			if requestedIntent != nil {
				s.undoManager.setPendingIntent(*requestedIntent)
			}
			s.replaceTextInRanges([]inputTextEdit{{rng: rng, text: newText}}, w, cx)
		}
		if endsComposition {
			s.undoManager.commitTransaction()
		}
		if !s.silentReplaceText {
			s.hooks.onTextTyped(s, rng, newText, w, cx)
		}
		return
	}

	// Single-line path.
	oldText := s.text
	s.ext().Replace(rng.Start, rng.End, newText)

	newOffset := min(rng.Start+len(newText), s.text.Len())

	// The mask changed the text, e.g. regrouping separators.
	maskChanged := false

	if s.IsSingleLine() {
		pendingText := s.text.String()
		// Only reject the edit if the old text was valid, so a
		// pre-existing invalid text (a non-conforming default_value) does
		// not trap the user.
		if !s.replayingHistory && !s.validatedTokenEdit &&
			!s.isValidInput(pendingText, cx) && s.isValidInput(oldText.String(), cx) {
			s.text = oldText
			return
		}

		if !s.replayingHistory && !s.maskPattern.IsNone() {
			maskText := s.maskPattern.Mask(pendingText)
			maskChanged = maskText != pendingText
			s.text = rope.FromStr(maskText)
			newTextLen := max(len(newText)+len(maskText)-len(pendingText), 0)
			newOffset = min(rng.Start+newTextLen, len(maskText))
		}
	}

	if maskChanged {
		// Masking rewrote the whole document.
		s.hooks.resetAnnotations(s)
	} else {
		s.hooks.adjustAnnotations(s, rng, len(newText))
	}
	if maskChanged {
		// A segment-based entry no longer matches the masked document;
		// record a whole-document change so undo/redo restores it exactly.
		atomic := editIntentAtomic
		after := newCursorSelection(0, newOffset, newOffset)
		s.pushHistory(oldText, NewInputSelection(0, oldText.Len()), s.text.String(), &atomic, selectionBefore, &after)
	} else {
		s.pushHistory(oldText, rng, newText, requestedIntent, selectionBefore, nil)
	}
	// Adjust folds before the wrap map: drop overlapping folds, shift the
	// others (U:input/base/state.rs:4090-4093).
	s.displayMap.adjustFoldsForEdit(oldText, rng, newText)
	s.displayMap.onTextChanged(s.text, rng, newText, w)

	if s.IsCodeEditor() {
		s.hooks.driveHighlighter(s, replacementInputEdit(oldText, s.text, rng, newText), s.text, s.mode.isFolding(), w, cx)
	}
	s.hooks.refreshLanguageFeatures(s, w, cx)
	s.setCursorTo(newOffset)
	s.imeMarkedRange = nil
	// A commit ends the IME composition (macOS delivers insertText:
	// without unmarkText); close the transaction here.
	if endsComposition {
		s.undoManager.recordSelections([]cursorSelection{selectionBefore}, []cursorSelection{s.activeSelection()})
		s.undoManager.commitTransaction()
	}
	s.updatePreferredColumn()
	s.updateSearch(cx.App())
	s.dismissTouchSelection(cx)
	if s.IsMultiLine() {
		s.mode.updateAutoGrow(s.displayMap.wrapRowCount())
	}
	if !s.silentReplaceText {
		s.hooks.onTextTyped(s, rng, newText, w, cx)
	}
	if s.emitEvents {
		InputBaseStateEvents[M]().Emit(cx, InputEvent{Kind: InputEventChange})
	}
	cx.Notify()
}

// resolveReplacementRange is the byte range an IME replacement acts on:
// the given UTF-16 range, else the marked range, else the selection
// (U:input/base/state.rs:3927-3934, 4173-4180).
func (s *InputBaseState[M]) resolveReplacementRange(rngUTF16 *gpui.UTF16Range) InputSelection {
	switch {
	case rngUTF16 != nil:
		return s.rangeFromUTF16(*rngUTF16)
	case s.imeMarkedRange != nil:
		return s.rangeFromUTF16(s.rangeToUTF16(NewInputSelection(s.imeMarkedRange.start, s.imeMarkedRange.end)))
	}
	return s.SelectedRange()
}

// ReplaceAndMarkTextInRange inserts the IME's temporary composition text
// (U:input/base/state.rs:4137-4266).
func (s *InputBaseState[M]) ReplaceAndMarkTextInRange(rngUTF16 *gpui.UTF16Range, newText string, newSelectedRangeUTF16 *gpui.UTF16Range, w *gpui.Window, cx *inputCx[M]) {
	var requestedIntent *editIntent
	if intent, ok := s.undoManager.takePendingIntent(); ok {
		requestedIntent = &intent
	}
	if !s.IsEditable() {
		return
	}
	selectionBefore := s.activeSelection()

	startsComposition := s.imeMarkedRange == nil
	if startsComposition {
		// Even a canceled preedit separates the typing gestures on either
		// side.
		s.undoManager.breakTransactionCoalescing()
		s.undoManager.beginTransaction()
	}

	// Never leave stale secondary cursors behind.
	s.selections.removeAllButActive()

	s.hooks.resetLanguageFeatures(s)

	if !s.replayingHistory {
		newText = s.normalizeInput(newText)
	}

	rng := s.normalizeTokenRange(s.resolveReplacementRange(rngUTF16))

	oldText := s.text
	s.ext().Replace(rng.Start, rng.End, newText)

	if s.IsSingleLine() {
		pendingText := s.text.String()
		if !s.replayingHistory && !s.validatedTokenEdit &&
			!s.isValidInput(pendingText, cx) && s.isValidInput(oldText.String(), cx) {
			s.text = oldText
			if startsComposition {
				s.undoManager.commitTransaction()
			}
			return
		}
	}

	s.hooks.adjustAnnotations(s, rng, len(newText))
	// U:input/base/state.rs:4208-4211.
	s.displayMap.adjustFoldsForEdit(oldText, rng, newText)
	s.displayMap.onTextChanged(s.text, rng, newText, w)
	if s.IsCodeEditor() {
		s.hooks.driveHighlighter(s, replacementInputEdit(oldText, s.text, rng, newText), s.text, s.mode.isFolding(), w, cx)
	}
	s.hooks.refreshLanguageFeatures(s, w, cx)
	if newText == "" {
		// Canceling the IME input cancels the selection.
		s.setCursorTo(rng.Start)
		s.imeMarkedRange = nil
	} else {
		marked := newCursorSelection(0, rng.Start, rng.Start+len(newText))
		s.imeMarkedRange = &marked
		newStart, newEnd := rng.Start+len(newText), rng.Start+len(newText)
		if newSelectedRangeUTF16 != nil {
			inserted := rope.FromStr(newText)
			ext := RopeExtOf(&inserted)
			newStart = rng.Start + ext.OffsetUTF16ToOffset(newSelectedRangeUTF16.Start)
			newEnd = rng.Start + ext.OffsetUTF16ToOffset(newSelectedRangeUTF16.End)
		}
		s.setSelection(newStart, newEnd)
	}
	if s.IsMultiLine() {
		s.mode.updateAutoGrow(s.displayMap.wrapRowCount())
	}
	active := s.activeSelection()
	if s.pushHistory(oldText, rng, newText, requestedIntent, selectionBefore, &active) {
		s.undoManager.recordSelections([]cursorSelection{selectionBefore}, []cursorSelection{s.activeSelection()})
	}
	if newText == "" {
		s.undoManager.commitTransaction()
	}
	cx.Notify()
}

// BoundsForRange positions the IME candidates
// (U:input/base/state.rs:4268-4326).
func (s *InputBaseState[M]) BoundsForRange(rngUTF16 gpui.UTF16Range, bounds gpui.Bounds, _ *gpui.Window, _ *inputCx[M]) (gpui.Bounds, bool) {
	layout := s.lastLayout
	if layout == nil {
		return gpui.Bounds{}, false
	}
	lineHeight := layout.lineHeight
	rng := s.rangeFromUTF16(rngUTF16)

	var startOrigin, endOrigin *gpui.Point
	yOffset := layout.visibleTop
	for vi, line := range layout.lines {
		if startOrigin != nil && endOrigin != nil {
			break
		}
		indexOffset := layout.visibleLineByteOffsets[vi]
		if startOrigin == nil {
			if p, ok := line.positionForIndex(max(rng.Start-indexOffset, 0), layout, false); ok {
				startOrigin = &gpui.Point{X: p.X, Y: p.Y + yOffset}
			}
		}
		if endOrigin == nil {
			if p, ok := line.positionForIndex(max(rng.End-indexOffset, 0), layout, false); ok {
				endOrigin = &gpui.Point{X: p.X, Y: p.Y + yOffset}
			}
		}
		yOffset += line.size(lineHeight).Height
	}
	var start, end gpui.Point
	if startOrigin != nil {
		start = *startOrigin
	}
	if endOrigin != nil {
		end = *endOrigin
	}
	// Keep both ends on the same line.
	end.Y = start.Y

	ox := bounds.Origin.X + layout.lineNumberWidth
	oy := bounds.Origin.Y
	// + line_height shows the IME panel under the cursor line.
	return boundsFromCorners(
		gpui.Point{X: ox + start.X, Y: oy + start.Y},
		gpui.Point{X: ox + end.X, Y: oy + end.Y + lineHeight},
	), true
}

// CharacterIndexForPoint is the UTF-16 offset under a window point
// (U:input/base/state.rs:4328-4345).
func (s *InputBaseState[M]) CharacterIndexForPoint(p gpui.Point, _ *gpui.Window, _ *inputCx[M]) (int, bool) {
	layout := s.lastLayout
	if layout == nil || s.lastBounds == nil {
		return 0, false
	}
	// Bounds::localize: None outside the bounds.
	if !s.lastBounds.Contains(p) {
		return 0, false
	}
	linePoint := gpui.Point{X: p.X - s.lastBounds.Origin.X, Y: p.Y - s.lastBounds.Origin.Y}
	for vi, line := range layout.lines {
		offset := layout.visibleLineByteOffsets[vi]
		if index, ok := line.indexForPosition(linePoint, layout); ok {
			return s.offsetToUTF16(offset + index), true
		}
	}
	return 0, false
}

// inputElementHandler is the pinned ElementInputHandler over an input
// state entity (R:input.rs:152-305): every call updates the entity, and
// bounds_for_range passes the element bounds captured at paint. It
// implements gpui.IMEInputHandler; the members the trait gives default
// bodies (R:input.rs:110-131) keep them: set_selected_text_range does
// nothing, text_length_utf16 is None, accepts_text_input is true, and
// dispatch_input replaces the selection (the platform handler's default).
type inputElementHandler[M InputModeKind] struct {
	view          gpui.Entity[InputBaseState[M]]
	elementBounds gpui.Bounds
}

func (h *inputElementHandler[M]) update(w *gpui.Window, app *gpui.App, f func(*InputBaseState[M], *gpui.Window, *inputCx[M])) {
	_ = h.view.UpdateIn(app, w, f)
}

// AcceptsTextInput implements gpui.InputHandler.
func (h *inputElementHandler[M]) AcceptsTextInput(*gpui.Window, *gpui.App) bool { return true }

// ReplaceTextInRange implements gpui.InputHandler.
func (h *inputElementHandler[M]) ReplaceTextInRange(rng *gpui.UTF16Range, text string, w *gpui.Window, app *gpui.App) {
	h.update(w, app, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) { s.ReplaceTextInRange(rng, text, w, cx) })
}

// DispatchInput implements gpui.InputHandler.
func (h *inputElementHandler[M]) DispatchInput(input string, w *gpui.Window, app *gpui.App) {
	h.ReplaceTextInRange(nil, input, w, app)
}

// SelectedTextRange implements gpui.IMEInputHandler.
func (h *inputElementHandler[M]) SelectedTextRange(ignoreDisabledInput bool, w *gpui.Window, app *gpui.App) (sel gpui.UTF16Selection, ok bool) {
	h.update(w, app, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) {
		sel, ok = s.SelectedTextRange(ignoreDisabledInput, w, cx)
	})
	return sel, ok
}

// MarkedTextRange implements gpui.IMEInputHandler.
func (h *inputElementHandler[M]) MarkedTextRange(w *gpui.Window, app *gpui.App) (rng gpui.UTF16Range, ok bool) {
	h.update(w, app, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) { rng, ok = s.MarkedTextRange(w, cx) })
	return rng, ok
}

// TextForRange implements gpui.IMEInputHandler.
func (h *inputElementHandler[M]) TextForRange(rng gpui.UTF16Range, w *gpui.Window, app *gpui.App) (text string, adjusted *gpui.UTF16Range, ok bool) {
	h.update(w, app, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) {
		text, adjusted, ok = s.TextForRange(rng, w, cx)
	})
	return text, adjusted, ok
}

// ReplaceAndMarkTextInRange implements gpui.IMEInputHandler.
func (h *inputElementHandler[M]) ReplaceAndMarkTextInRange(rng *gpui.UTF16Range, text string, newSelectedRange *gpui.UTF16Range, w *gpui.Window, app *gpui.App) {
	h.update(w, app, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) {
		s.ReplaceAndMarkTextInRange(rng, text, newSelectedRange, w, cx)
	})
}

// UnmarkText implements gpui.IMEInputHandler.
func (h *inputElementHandler[M]) UnmarkText(w *gpui.Window, app *gpui.App) {
	h.update(w, app, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) { s.UnmarkText(w, cx) })
}

// BoundsForRange implements gpui.IMEInputHandler.
func (h *inputElementHandler[M]) BoundsForRange(rng gpui.UTF16Range, w *gpui.Window, app *gpui.App) (b gpui.Bounds, ok bool) {
	h.update(w, app, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) {
		b, ok = s.BoundsForRange(rng, h.elementBounds, w, cx)
	})
	return b, ok
}

// CharacterIndexForPoint implements gpui.IMEInputHandler.
func (h *inputElementHandler[M]) CharacterIndexForPoint(p gpui.Point, w *gpui.Window, app *gpui.App) (index int, ok bool) {
	h.update(w, app, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) {
		index, ok = s.CharacterIndexForPoint(p, w, cx)
	})
	return index, ok
}

// SetSelectedTextRange implements gpui.IMEInputHandler (the trait
// default does nothing, R:input.rs:110-117).
func (h *inputElementHandler[M]) SetSelectedTextRange(gpui.UTF16Range, *gpui.Window, *gpui.App) {}

// TextLengthUTF16 implements gpui.IMEInputHandler (the trait default is
// None, R:input.rs:119-126).
func (h *inputElementHandler[M]) TextLengthUTF16(*gpui.Window, *gpui.App) (int, bool) {
	return 0, false
}
