package base

// Selection extension, pointer input and scrolling of the input engine
// (U:input/base/state.rs:1316-1444, 2273-2610, 2975-3220, 3385-3436).
//
// Not ported here: the native right-click menu (handle_right_click_menu,
// U:input/base/state.rs:2095-2124, needs NativeMenu from native.rs), the
// touch edit-menu reopen/keep/scroll steps (touch.rs). A right press still
// moves the caret like upstream (U:input/base/state.rs:2335-2344).

import (
	"github.com/hoangvu12/gpui-go/base/sumtree"
	"github.com/hoangvu12/gpui-go/gpui"
)

// selectLeft is U:input/base/state.rs:1316-1319.
func (s *InputBaseState[M]) selectLeft(_ *SelectLeft, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.undoManager.breakTransactionCoalescing()
	s.selectAllCursorsTo(func(sel cursorSelection) int { return s.previousBoundary(sel.cursorOffset()) }, cx)
}

// selectRight is U:input/base/state.rs:1321-1323.
func (s *InputBaseState[M]) selectRight(_ *SelectRight, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.selectAllCursorsTo(func(sel cursorSelection) int { return s.nextBoundary(sel.cursorOffset()) }, cx)
}

// selectUp is U:input/base/state.rs:1325-1341.
func (s *InputBaseState[M]) selectUp(_ *SelectUp, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	if s.IsSingleLine() {
		return
	}
	s.selectAllCursorsToWithAffinity(func(sel cursorSelection) (int, bool) {
		return s.verticalSelectionTarget(sel.cursorOffset(), sel.columnAnchor, s.lineEndAffinityFor(sel), -1)
	}, true, cx)
}

// selectDown is U:input/base/state.rs:1343-1359.
func (s *InputBaseState[M]) selectDown(_ *SelectDown, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	if s.IsSingleLine() {
		return
	}
	s.selectAllCursorsToWithAffinity(func(sel cursorSelection) (int, bool) {
		return s.verticalSelectionTarget(sel.cursorOffset(), sel.columnAnchor, s.lineEndAffinityFor(sel), 1)
	}, true, cx)
}

// onActionSelectAll is U:input/base/state.rs:1361-1368.
func (s *InputBaseState[M]) onActionSelectAll(_ *SelectAll, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.SelectAll(w, cx)
}

// selectToStart is U:input/base/state.rs:1370-1378.
func (s *InputBaseState[M]) selectToStart(_ *SelectToStart, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.undoManager.breakTransactionCoalescing()
	s.selectAllCursorsTo(func(cursorSelection) int { return 0 }, cx)
}

// selectToEnd is U:input/base/state.rs:1380-1389.
func (s *InputBaseState[M]) selectToEnd(_ *SelectToEnd, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.undoManager.breakTransactionCoalescing()
	end := s.text.Len()
	s.selectAllCursorsTo(func(cursorSelection) int { return end }, cx)
}

// selectToStartOfLine is U:input/base/state.rs:1391-1402.
func (s *InputBaseState[M]) selectToStartOfLine(_ *SelectToStartOfLine, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.undoManager.breakTransactionCoalescing()
	s.selectAllCursorsTo(func(sel cursorSelection) int {
		return s.startOfLineAt(sel.cursorOffset(), s.lineEndAffinityFor(sel))
	}, cx)
}

// selectToEndOfLine is U:input/base/state.rs:1404-1421.
func (s *InputBaseState[M]) selectToEndOfLine(_ *SelectToEndOfLine, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.undoManager.breakTransactionCoalescing()
	s.selectAllCursorsToWithAffinity(func(sel cursorSelection) (int, bool) {
		return s.endOfLineAt(sel.cursorOffset(), s.lineEndAffinityFor(sel)), true
	}, false, cx)
}

// selectToPreviousWord is U:input/base/state.rs:1423-1434.
func (s *InputBaseState[M]) selectToPreviousWord(_ *SelectToPreviousWordStart, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.undoManager.breakTransactionCoalescing()
	s.selectAllCursorsTo(func(sel cursorSelection) int { return s.previousStartOfWordAt(sel.cursorOffset()) }, cx)
}

// selectToNextWord is U:input/base/state.rs:1436-1444.
func (s *InputBaseState[M]) selectToNextWord(_ *SelectToNextWordEnd, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.undoManager.breakTransactionCoalescing()
	s.selectAllCursorsTo(func(sel cursorSelection) int { return s.nextEndOfWordAt(sel.cursorOffset()) }, cx)
}

// onMouseDown places the caret, selects a word or line, or starts a
// drag (U:input/base/state.rs:2273-2371).
func (s *InputBaseState[M]) onMouseDown(event *gpui.MouseDownEvent, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	// A token that consumed the press must not also place the caret.
	if w.DefaultPrevented() {
		return
	}
	s.undoManager.breakTransactionCoalescing()
	// The input has its own text selection; suppress the window-level one.
	GlobalStateInit(cx.App())
	GlobalStateSuppressTextSelection(cx.App())

	s.hooks.clearInlineCompletion(s, cx)
	s.dismissTouchSelection(cx)

	// An empty marked range means Esc aborted the IME typing.
	if s.imeMarkedRange != nil && s.imeMarkedRange.len() == 0 {
		s.imeMarkedRange = nil
	}

	s.selecting = true
	offset, lineEndAffinity, columnsPastLineEnd := s.resolveMousePosition(event.Position)

	if s.hooks.onClick(s, event, offset, w, cx) {
		return
	}

	// Triple click selects the line.
	if event.Button == gpui.MouseButtonLeft && event.ClickCount >= 3 {
		s.selectLine(offset, w, cx)
		return
	}
	// Double click selects the word.
	if event.Button == gpui.MouseButtonLeft && event.ClickCount == 2 {
		s.selectWord(offset, w, cx)
		return
	}

	if event.Button == gpui.MouseButtonRight {
		if s.enableContextMenu && !s.activeSelection().contains(offset) {
			s.moveTo(offset, nil, cx)
		}
		return
	}

	// Multi-cursor placement, multi-line only.
	if s.IsMultiLine() && event.Button == gpui.MouseButtonLeft {
		mods := event.Modifiers
		if mods.Alt && (mods.Shift || (isLinux() && mods.Control)) {
			// Alt+Shift starts a block (Linux also accepts Ctrl+Alt).
			start := columnarPoint{offset: offset, columnsPastLineEnd: columnsPastLineEnd}
			s.columnSelectStart = &start
			s.selecting = true
			s.moveToWithAffinity(offset, nil, lineEndAffinity, cx)
			return
		} else if mods.Alt {
			s.addCursorAt(offset, cx)
			// The press anchors a block if the user keeps dragging.
			start := columnarPoint{offset: offset, columnsPastLineEnd: columnsPastLineEnd}
			s.columnSelectStart = &start
			return
		}
	}

	if event.Modifiers.Shift {
		s.selectToWithAffinity(offset, lineEndAffinity, cx)
	} else {
		s.moveToWithAffinity(offset, nil, lineEndAffinity, cx)
	}
}

// onMouseUp ends the drag (U:input/base/state.rs:2373-2391).
func (s *InputBaseState[M]) onMouseUp(_ *gpui.MouseUpEvent, _ *gpui.Window, _ *gpui.Context[InputBaseState[M]]) {
	if s.activeSelection().isEmpty() {
		s.activeSelectionMut().reversed = false
	}
	s.selecting = false
	s.selectedWordRange = nil
	s.columnSelectStart = nil
	s.autoScroll.Stop()
}

// onMouseMove clears hover state outside the input and forwards the
// offset under the pointer to the mode; a code editor shows the
// diagnostic under the pointer (U:input/base/state.rs:2393-2438).
func (s *InputBaseState[M]) onMouseMove(event *gpui.MouseMoveEvent, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	if s.lastBounds == nil || !s.lastBounds.Contains(event.Position) {
		s.hooks.clearHoverState(s, cx)
		return
	}
	offset, _ := s.indexForMousePosition(event.Position)
	s.hooks.onMouseMove(s, offset, event, w, cx)

	if s.IsCodeEditor() {
		// Keep the current popover while the pointer stays on the same
		// diagnostic, so moving over a squiggle neither repaints nor
		// rebuilds the popover.
		var diagnostic DiagnosticEntry
		found := false
		if set := s.mode.diagnosticSet(); set != nil {
			diagnostic, found = set.ForOffset(offset)
		}
		switch {
		case found:
			if s.diagnosticPopover == nil || !s.diagnosticPopover.Equal(diagnostic) {
				s.diagnosticPopover = &diagnostic
				cx.Notify()
			}
		case s.diagnosticPopover != nil:
			s.diagnosticPopover = nil
			cx.Notify()
		}
	}
}

// onScrollWheel scrolls, stopping propagation only when the offset
// changed, and hides the diagnostic popover (U:input/base/state.rs:
// 2440-2467).
func (s *InputBaseState[M]) onScrollWheel(event *gpui.ScrollWheelEvent, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	lineHeight := w.LineHeight()
	if s.lastLayout != nil {
		lineHeight = s.lastLayout.lineHeight
	}
	delta := event.Delta.PixelDelta(lineHeight)

	old := s.scrollHandle.Offset()
	target := gpui.Point{X: old.X + delta.X, Y: old.Y + delta.Y}
	s.updateScrollOffset(&target, cx)

	if s.scrollHandle.Offset() != old {
		cx.StopPropagation()
	}

	if s.diagnosticPopover != nil {
		s.diagnosticPopover = nil
		cx.Notify()
	}
}

// updateScrollOffset clamps an offset into the scrollable range and
// notifies only on change (U:input/base/state.rs:2469-2497).
func (s *InputBaseState[M]) updateScrollOffset(offset *gpui.Point, cx *gpui.Context[InputBaseState[M]]) {
	o := s.scrollHandle.Offset()
	if offset != nil {
		o = *offset
	}
	// Besides left alignment, a cursor width is reserved on the right.
	safeXOffset := float32(0)
	if s.textAlign != gpui.TextAlignLeft {
		safeXOffset = -cursorWidth
	}
	safeYStart := min(-s.scrollSize.Height+s.inputBounds.Size.Height, 0)
	safeXStart := min(-s.scrollSize.Width+s.inputBounds.Size.Width+safeXOffset, safeXOffset)

	if s.IsSingleLine() {
		o.Y = 0
	} else {
		o.Y = clampF(o.Y, safeYStart, 0)
	}
	o.X = clampF(o.X, safeXStart, 0)
	if s.scrollHandle.Offset() != o {
		s.scrollHandle.SetOffset(o)
		cx.Notify()
	}
}

// clampF is f32::clamp (min <= max is the caller's contract).
func clampF(v, lo, hi float32) float32 { return min(max(v, lo), hi) }

// scrollTo reveals offset; a direction keeps the edges on the same side
// (U:input/base/state.rs:2499-2514).
func (s *InputBaseState[M]) scrollTo(offset int, direction *moveDirection, cx *gpui.Context[InputBaseState[M]]) {
	padding := scrollPaddingMinimal
	if direction != nil {
		padding = scrollPaddingSurroundingLines
	}
	s.scrollToWithPadding(offset, direction, padding, cx)
}

// scrollToWithPadding reveals offset with an independent direction
// restriction and padding (U:input/base/state.rs:2516-2610).
func (s *InputBaseState[M]) scrollToWithPadding(offset int, direction *moveDirection, padding scrollPadding, cx *gpui.Context[InputBaseState[M]]) {
	layout := s.lastLayout
	if layout == nil || s.lastBounds == nil {
		return
	}
	bounds := *s.lastBounds

	scroll := s.scrollHandle.Offset()
	was := scroll
	lineHeight := layout.lineHeight

	point := s.ext().OffsetToPoint(offset)
	row := point.Row

	// Resolve the wrapped row even outside the last layout.
	displayRow, _ := s.displayMap.bufferPosToDisplayPos(row, point.Column)
	rowOffsetY := lineHeight * float32(displayRow)

	// Right alignment uses no margin: the caret is clamped inside the
	// bounds in layout_cursors.
	var safetyMargin float32
	switch layout.textAlign {
	case gpui.TextAlignLeft:
		safetyMargin = inputRightMargin
	case gpui.TextAlignCenter:
		safetyMargin = cursorWidth
	}
	for vi, bufferLine := range layout.visibleBufferLines {
		if bufferLine != row {
			continue
		}
		line := layout.lines[vi]
		local := max(offset-layout.visibleLineByteOffsets[vi], 0)
		if pos, ok := line.positionForIndex(local, layout, false); ok {
			boundsWidth := bounds.Size.Width - layout.lineNumberWidth
			colX := pos.X
			if colX-safetyMargin < -scroll.X {
				scroll.X = -colX + safetyMargin
			} else if colX+safetyMargin > -scroll.X+boundsWidth {
				scroll.X = -(colX - boundsWidth + safetyMargin)
			}
		}
		break
	}

	// The same edge clearance as layout_cursors.
	edgeHeight := lineHeight
	if padding == scrollPaddingSurroundingLines && s.IsCodeEditor() {
		edgeHeight = cursorSurroundingPadding(s.mode.isAutoGrow(), s.cursorSurroundingLines,
			viewportVisibleLines(bounds.Size.Height, lineHeight), lineHeight)
	}
	if rowOffsetY-edgeHeight+lineHeight < -scroll.Y {
		scroll.Y = -rowOffsetY + edgeHeight - lineHeight
	} else if rowOffsetY+edgeHeight > -scroll.Y+bounds.Size.Height {
		scroll.Y = -(rowOffsetY - bounds.Size.Height + edgeHeight)
	}

	// Avoid an unnecessary scroll in the move direction.
	if direction != nil {
		if *direction == moveDirectionUp {
			scroll.Y = max(scroll.Y, was.Y)
		} else {
			scroll.Y = min(scroll.Y, was.Y)
		}
	}

	// Clamp the deferred target into update_scroll_offset's safe range.
	safeYMin := min(-s.scrollSize.Height+s.inputBounds.Size.Height, 0)
	scroll.X = min(scroll.X, 0)
	scroll.Y = clampF(scroll.Y, safeYMin, 0)
	s.deferredScrollOffset = &scroll
	cx.Notify()
}

// indexForMousePosition resolves a window position to an offset and its
// caret affinity (U:input/base/state.rs:2975-2985).
func (s *InputBaseState[M]) indexForMousePosition(position gpui.Point) (int, bool) {
	offset, affinity, _ := s.resolveMousePosition(position)
	return offset, affinity
}

// resolveMousePosition resolves a window position to an offset, its caret
// affinity and the columns it sits past the row's end
// (U:input/base/state.rs:2987-3085).
func (s *InputBaseState[M]) resolveMousePosition(position gpui.Point) (int, bool, int) {
	if s.text.Len() == 0 {
		return 0, false, 0
	}
	if s.lastBounds == nil || s.lastLayout == nil {
		return 0, false, 0
	}
	bounds, layout := *s.lastBounds, s.lastLayout
	lineHeight := layout.lineHeight

	// Relative to the text bounds, which include the padding and the
	// scroll offset.
	inner := gpui.Point{X: position.X - bounds.Origin.X - layout.lineNumberWidth, Y: position.Y - bounds.Origin.Y}

	yOffset := layout.visibleTop
	var lastLinePos *gpui.Point
	for vi, line := range layout.lines {
		if vi >= len(layout.visibleBufferLines) {
			break
		}
		lineStart := layout.visibleLineByteOffsets[vi]
		pos := gpui.Point{X: inner.X, Y: inner.Y - yOffset}

		// A single line never wraps: no boundary to disambiguate.
		if s.IsSingleLine() {
			local := line.closestIndexForX(pos.X, layout)
			return s.resolveIndex(lineStart + local), false, 0
		}

		if local, affinity, ok := line.closestIndexForPosition(pos, layout); ok {
			return s.resolveIndex(lineStart + local), affinity, line.columnsPastLineEnd(pos, layout)
		} else if pos.Y < 0 {
			// Above this line: its start.
			return s.resolveIndex(lineStart), false, 0
		}

		yOffset += line.size(lineHeight).Height
		p := pos
		lastLinePos = &p
	}

	// Clamp to the last laid-out row rather than the document end, which
	// would select text drag autoscroll has not reached.
	if n := len(layout.lines); n > 0 && lastLinePos != nil && len(layout.visibleLineByteOffsets) > 0 {
		line := layout.lines[n-1]
		lineStart := layout.visibleLineByteOffsets[len(layout.visibleLineByteOffsets)-1]
		lastRowTop := max(line.size(lineHeight).Height-lineHeight, 0)
		pos := gpui.Point{X: lastLinePos.X, Y: lastRowTop}
		return s.resolveIndex(lineStart + line.len()), false, line.columnsPastLineEnd(pos, layout)
	}
	return s.text.Len(), false, 0
}

// resolveIndex maps a display byte index back to a text offset, undoing
// the mask expansion (U:input/base/state.rs:3087-3095).
func (s *InputBaseState[M]) resolveIndex(index int) int {
	if s.masked {
		return s.ext().CharIndexToOffset(index / inputMaskCharLen)
	}
	return min(index, s.text.Len())
}

// extendSelection moves sel's moving end to offset, flipping reversed when
// the ends cross; a sticky word range stays covered
// (U:input/base/state.rs:3097-3130).
func extendSelection(sel *cursorSelection, offset int, wordRange *cursorSelection) {
	if sel.reversed {
		sel.start = offset
	} else {
		sel.end = offset
	}
	if sel.end < sel.start {
		sel.reversed = !sel.reversed
		sel.start, sel.end = sel.end, sel.start
	}
	if wordRange != nil {
		if sel.start > wordRange.start {
			sel.start = wordRange.start
		}
		if sel.end < wordRange.end {
			sel.end = wordRange.end
		}
	}
}

// selectTo extends the active selection to offset, clearing the affinity
// (U:input/base/state.rs:3132-3135).
func (s *InputBaseState[M]) selectTo(offset int, cx *gpui.Context[InputBaseState[M]]) {
	s.selectToWithAffinity(offset, false, cx)
}

// selectToWithAffinity extends the active selection carrying the caret's
// line-end affinity (U:input/base/state.rs:3137-3162).
func (s *InputBaseState[M]) selectToWithAffinity(offset int, lineEndAffinity bool, cx *gpui.Context[InputBaseState[M]]) {
	s.hooks.clearInlineCompletion(s, cx)

	s.cursorLineEndAffinity = lineEndAffinity
	offset = s.cursorBoundary(offset, sumtree.Left)
	extendSelection(s.activeSelectionMut(), offset, s.selectedWordRange)
	rng := s.normalizeTokenRange(s.SelectedRange())
	s.setSelection(rng.Start, rng.End)

	if s.activeSelection().isEmpty() {
		s.updatePreferredColumn()
	}
	cx.Notify()
}

// selectAllCursorsTo extends every selection to f's offset and merges
// overlaps (U:input/base/state.rs:3164-3172).
func (s *InputBaseState[M]) selectAllCursorsTo(f func(sel cursorSelection) int, cx *gpui.Context[InputBaseState[M]]) {
	s.selectAllCursorsToWithAffinity(func(sel cursorSelection) (int, bool) { return f(sel), false }, false, cx)
}

// selectAllCursorsToWithAffinity extends selections with caret affinity,
// preserving the column anchors through vertical moves
// (U:input/base/state.rs:3174-3219).
func (s *InputBaseState[M]) selectAllCursorsToWithAffinity(f func(sel cursorSelection) (int, bool), preserveColumn bool, cx *gpui.Context[InputBaseState[M]]) {
	s.pauseBlinkCursor(cx)
	s.undoManager.breakTransactionCoalescing()
	s.hooks.clearInlineCompletion(s, cx)

	activeAffinity := false
	activeID := s.activeSelection().id
	next := make([]cursorSelection, 0, s.selections.len())
	for sel := range s.selections.iter() {
		offset, affinity := f(sel)
		if sel.id == activeID {
			activeAffinity = affinity
		}
		offset = s.cursorBoundary(offset, sumtree.Left)
		newSel := sel
		extendSelection(&newSel, offset, nil)
		rng := s.normalizeTokenRange(NewInputSelection(newSel.start, newSel.end))
		newSel.start, newSel.end = rng.Start, rng.End
		if !preserveColumn {
			newSel.columnAnchor = s.preferredColumnForWithAffinity(newSel.cursorOffset(), affinity)
		} else if !newSel.columnAnchor.ok {
			newSel.columnAnchor = s.preferredColumnForWithAffinity(sel.cursorOffset(), s.lineEndAffinityFor(sel))
		}
		next = append(next, newSel)
	}
	s.cursorLineEndAffinity = activeAffinity
	s.selections.replaceAll(next)
	s.selections.mergeOverlapping()

	s.scrollTo(s.Cursor(), nil, cx)
	cx.Notify()
}

// onDragMove extends the selection while the left button drags, with
// vertical autoscroll in a multi-line input
// (U:input/base/state.rs:3385-3436).
func (s *InputBaseState[M]) onDragMove(event *gpui.MouseMoveEvent, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	if s.text.Len() == 0 || s.lastLayout == nil || !s.focusHandle.IsFocused(w) || !s.selecting {
		return
	}

	s.autoScroll.LastDragPosition = gpui.Some(event.Position)
	s.dragSelectTo(event.Position, cx)

	if !s.IsSingleLine() {
		delta := AutoScrollComputeDelta(event.Position.Y, s.inputBounds)
		// The input's ScrollHandle is negative-y-is-down.
		var scrollDelta gpui.Optional[float32]
		if d, ok := delta.Get(); ok {
			scrollDelta = gpui.Some(-d)
		}
		s.autoScroll.Set(scrollDelta, cx, func(delta float32, state *InputBaseState[M], cx *gpui.Context[InputBaseState[M]]) {
			current := state.scrollHandle.Offset()
			target := gpui.Point{X: current.X, Y: current.Y + delta}
			state.updateScrollOffset(&target, cx)
			if pos, ok := state.autoScroll.LastDragPosition.Get(); ok {
				state.dragSelectTo(pos, cx)
			}
		})
	}
}

// dragSelectTo is the shared body of on_drag_move and its autoscroll
// tick: extend the columnar block or the selection to position
// (U:input/base/state.rs:3408-3415, 3424-3433).
func (s *InputBaseState[M]) dragSelectTo(position gpui.Point, cx *gpui.Context[InputBaseState[M]]) {
	offset, affinity, columnsPastLineEnd := s.resolveMousePosition(position)
	if s.columnSelectStart != nil {
		s.buildColumnarSelection(*s.columnSelectStart, columnarPoint{offset: offset, columnsPastLineEnd: columnsPastLineEnd}, cx)
	} else {
		s.selectToWithAffinity(offset, affinity, cx)
	}
}
