package base

// Port of U:input/base/movement.rs: caret movement over the last layout.
// Vertical moves go through the display map (input_layout.go's
// inputDisplayMap stand-in until the textarea ticket).

import (
	"github.com/hoangvu12/gpui-go/base/sumtree"
	"github.com/hoangvu12/gpui-go/gpui"
)

// moveDirection is MoveDirection (U:input/base/movement.rs:11-15); nil
// *moveDirection is None.
type moveDirection uint8

const (
	moveDirectionUp moveDirection = iota
	moveDirectionDown
)

func dirPtr(d moveDirection) *moveDirection { return &d }

// preferredColumnFor is the column anchor for offset
// (U:input/base/movement.rs:18-21).
func (s *InputBaseState[M]) preferredColumnFor(offset int) cursorColumnAnchor {
	return s.preferredColumnForWithAffinity(offset, false)
}

// preferredColumnForWithAffinity resolves an offset on a soft wrap
// boundary to the row the caret is drawn on
// (U:input/base/movement.rs:23-35).
func (s *InputBaseState[M]) preferredColumnForWithAffinity(offset int, lineEndAffinity bool) cursorColumnAnchor {
	layout := s.lastLayout
	if layout == nil {
		return cursorColumnAnchor{}
	}
	point := s.ext().OffsetToPoint(offset)
	line, ok := layout.line(point.Row)
	if !ok {
		return cursorColumnAnchor{}
	}
	pos, ok := line.positionForIndex(point.Column, layout, lineEndAffinity)
	if !ok {
		return cursorColumnAnchor{}
	}
	return cursorColumnAnchor{x: pos.X, column: point.Column, ok: true}
}

// lineEndAffinityFor is the affinity of sel: only the active cursor
// carries one (U:input/base/movement.rs:37-41).
func (s *InputBaseState[M]) lineEndAffinityFor(sel cursorSelection) bool {
	return sel.id == s.activeSelection().id && s.cursorLineEndAffinity
}

// lineEndAffinityAt is the affinity for a cursor known only by its offset
// (U:input/base/movement.rs:43-47).
func (s *InputBaseState[M]) lineEndAffinityAt(offset int) bool {
	return offset == s.Cursor() && s.cursorLineEndAffinity
}

// updatePreferredColumn refreshes the active column anchor after a move
// (U:input/base/movement.rs:49-55).
func (s *InputBaseState[M]) updatePreferredColumn() {
	anchor := s.preferredColumnForWithAffinity(s.Cursor(), s.cursorLineEndAffinity)
	s.activeSelectionMut().columnAnchor = anchor
}

// moveTo moves the cursor to offset (U:input/base/movement.rs:57-69).
func (s *InputBaseState[M]) moveTo(offset int, direction *moveDirection, cx *inputCx[M]) {
	s.moveToWithAffinity(offset, direction, false, cx)
}

// moveToWithAffinity moves the cursor, carrying the caret's line-end
// affinity (U:input/base/movement.rs:71-96).
func (s *InputBaseState[M]) moveToWithAffinity(offset int, direction *moveDirection, lineEndAffinity bool, cx *inputCx[M]) {
	s.undoManager.breakTransactionCoalescing()
	s.selections.removeAllButActive()
	offset = s.cursorBoundary(offset, sumtree.Left)
	s.cursorLineEndAffinity = lineEndAffinity
	s.setCursorTo(offset)
	s.scrollTo(offset, direction, cx)
	s.pauseBlinkCursor(cx)
	s.updatePreferredColumn()
	s.hooks.hideContextMenu(s, cx)
	s.hooks.clearInlineCompletion(s, cx)
	cx.Notify()
}

// verticalTarget is the target offset and affinity of a vertical move by
// moveLines, honoring the column anchor (U:input/base/movement.rs:98-175).
func (s *InputBaseState[M]) verticalTarget(offset int, columnAnchor cursorColumnAnchor, lineEndAffinity bool, moveLines int) (int, bool) {
	layout := s.lastLayout
	if layout == nil {
		return offset, lineEndAffinity
	}

	// Start from the row the caret is drawn on.
	displayPoint := s.displayMap.offsetToWrapDisplayPointWithAffinity(offset, lineEndAffinity)

	currentDisplayRow, ok := s.displayMap.wrapRowToDisplayRow(displayPoint.row)
	if !ok {
		currentDisplayRow = s.displayMap.nearestVisibleDisplayRow(displayPoint.row)
	}
	maxDisplayRow := max(s.displayMap.displayRowCount()-1, 0)
	targetDisplayRow := min(max(currentDisplayRow+moveLines, 0), maxDisplayRow)
	targetWrapRow, ok := s.displayMap.displayRowToWrapRow(targetDisplayRow)
	if !ok {
		targetWrapRow = displayPoint.row
	}

	displayPoint.row = targetWrapRow
	displayPoint.column = 0
	newOffset := s.displayMap.wrapDisplayPointToOffset(displayPoint)

	if !columnAnchor.ok {
		columnAnchor = s.preferredColumnForWithAffinity(offset, lineEndAffinity)
	}
	newAffinity := false
	if columnAnchor.ok {
		next := s.displayMap.offsetToWrapDisplayPoint(newOffset)
		next.column = 0
		nextPoint := s.displayMap.wrapDisplayPointToPoint(next)
		lineStartOffset := s.ext().LineStartOffset(nextPoint.Row)

		if line, ok := layout.line(nextPoint.Row); ok {
			pos := gpui.Point{X: columnAnchor.x, Y: float32(next.localRow) * layout.lineHeight}
			if x, affinity, ok := line.closestIndexForPosition(pos, layout); ok {
				newOffset = lineStartOffset + x
				// Landing on a wrap boundary keeps the caret on that row.
				newAffinity = affinity
			}
		} else {
			// Not in the visible range: use the column directly.
			maxLineLen := s.ext().SliceLine(nextPoint.Row).Len()
			newOffset = lineStartOffset + min(columnAnchor.column, maxLineLen)
		}
	}
	return newOffset, newAffinity
}

// verticalSelectionTarget extends to the document edge when there is no
// further visual row (U:input/base/movement.rs:177-199).
func (s *InputBaseState[M]) verticalSelectionTarget(offset int, columnAnchor cursorColumnAnchor, lineEndAffinity bool, moveLines int) (int, bool) {
	target, targetAffinity := s.verticalTarget(offset, columnAnchor, lineEndAffinity, moveLines)
	if s.lastLayout != nil {
		row := func(offset int, affinity bool) int {
			return s.displayMap.offsetToWrapDisplayPointWithAffinity(offset, affinity).row
		}
		if row(offset, lineEndAffinity) == row(target, targetAffinity) {
			if moveLines < 0 {
				return 0, false
			}
			return s.text.Len(), false
		}
	}
	return target, targetAffinity
}

// cursorMove is the (new offset, column anchor, line-end affinity) a move
// maps each selection to.
type cursorMove struct {
	offset   int
	anchor   cursorColumnAnchor
	affinity bool
}

// moveAllCursors moves every cursor through f, collapsing each, merges
// overlaps and runs the post-move sequence
// (U:input/base/movement.rs:201-237).
func (s *InputBaseState[M]) moveAllCursors(f func(sel cursorSelection) cursorMove, direction *moveDirection, _ *gpui.Window, cx *inputCx[M]) {
	s.undoManager.breakTransactionCoalescing()
	activeAffinity := false
	activeID := s.activeSelection().id
	next := make([]cursorSelection, 0, s.selections.len())
	for sel := range s.selections.iter() {
		m := f(sel)
		if sel.id == activeID {
			activeAffinity = m.affinity
		}
		sel.placeAt(s.cursorBoundary(m.offset, sumtree.Left), m.anchor)
		next = append(next, sel)
	}
	s.selections.replaceAll(next)
	s.selections.mergeOverlapping()

	s.cursorLineEndAffinity = activeAffinity
	s.scrollTo(s.Cursor(), direction, cx)
	s.pauseBlinkCursor(cx)
	s.hooks.hideContextMenu(s, cx)
	s.hooks.clearInlineCompletion(s, cx)
	cx.Notify()
}

// moveVertical moves every cursor by moveLines rows
// (U:input/base/movement.rs:239-291).
func (s *InputBaseState[M]) moveVertical(moveLines int, collapse bool, w *gpui.Window, cx *inputCx[M]) {
	if s.IsSingleLine() {
		return
	}
	s.pauseBlinkCursor(cx)

	direction := moveDirectionDown
	if moveLines < 0 {
		direction = moveDirectionUp
	}
	s.moveAllCursors(func(sel cursorSelection) cursorMove {
		var effective int
		var anchor cursorColumnAnchor
		var affinity bool
		switch {
		case sel.isEmpty() || !collapse:
			effective, anchor, affinity = sel.cursorOffset(), sel.columnAnchor, s.lineEndAffinityFor(sel)
		case moveLines < 0:
			effective = s.previousBoundary(max(sel.start-1, 0))
			anchor, affinity = s.preferredColumnFor(effective), false
		default:
			effective = s.nextBoundary(max(sel.end-1, 0))
			affinity = s.lineEndAffinityAt(effective)
			anchor = s.preferredColumnForWithAffinity(effective, affinity)
		}
		if !anchor.ok {
			anchor = s.preferredColumnForWithAffinity(effective, affinity)
		}
		offset, affinity := s.verticalTarget(effective, anchor, affinity, moveLines)
		return cursorMove{offset: offset, anchor: anchor, affinity: affinity}
	}, &direction, w, cx)
}

// left moves left; a lone cursor at the start propagates the keystroke to
// an ancestor (U:input/base/movement.rs:293-317).
func (s *InputBaseState[M]) left(_ *MoveLeft, w *gpui.Window, cx *inputCx[M]) {
	if s.selections.isSingle() && s.activeSelection().isEmpty() && s.Cursor() == 0 {
		cx.Propagate()
		return
	}
	s.moveAllCursors(func(sel cursorSelection) cursorMove {
		offset := sel.start
		if sel.isEmpty() {
			offset = s.previousBoundary(sel.cursorOffset())
		}
		return cursorMove{offset: offset, anchor: s.preferredColumnFor(offset)}
	}, nil, w, cx)
}

// right mirrors left (U:input/base/movement.rs:319-343).
func (s *InputBaseState[M]) right(_ *MoveRight, w *gpui.Window, cx *inputCx[M]) {
	if s.selections.isSingle() && s.activeSelection().isEmpty() && s.Cursor() == s.text.Len() {
		cx.Propagate()
		return
	}
	s.moveAllCursors(func(sel cursorSelection) cursorMove {
		offset := sel.end
		if sel.isEmpty() {
			offset = s.nextBoundary(sel.cursorOffset())
		}
		return cursorMove{offset: offset, anchor: s.preferredColumnFor(offset)}
	}, nil, w, cx)
}

// up is U:input/base/movement.rs:345-351.
func (s *InputBaseState[M]) up(action *MoveUp, w *gpui.Window, cx *inputCx[M]) {
	if s.hooks.handleContextMenuAction(s, MoveUpAction.Box(*action), w, cx) {
		return
	}
	s.moveVertical(-1, true, w, cx)
}

// down is U:input/base/movement.rs:353-359.
func (s *InputBaseState[M]) down(action *MoveDown, w *gpui.Window, cx *inputCx[M]) {
	if s.hooks.handleContextMenuAction(s, MoveDownAction.Box(*action), w, cx) {
		return
	}
	s.moveVertical(1, true, w, cx)
}

// pageUp is U:input/base/movement.rs:361-372.
func (s *InputBaseState[M]) pageUp(_ *MovePageUp, w *gpui.Window, cx *inputCx[M]) {
	if s.IsSingleLine() || s.lastLayout == nil {
		return
	}
	displayLines := int(s.inputBounds.Size.Height / s.lastLayout.lineHeight)
	s.moveVertical(-displayLines, false, w, cx)
}

// pageDown is U:input/base/movement.rs:374-390.
func (s *InputBaseState[M]) pageDown(_ *MovePageDown, w *gpui.Window, cx *inputCx[M]) {
	if s.IsSingleLine() || s.lastLayout == nil {
		return
	}
	displayLines := int(s.inputBounds.Size.Height / s.lastLayout.lineHeight)
	s.moveVertical(displayLines, false, w, cx)
}

// home is U:input/base/movement.rs:392-402.
func (s *InputBaseState[M]) home(_ *MoveHome, w *gpui.Window, cx *inputCx[M]) {
	s.moveAllCursors(func(sel cursorSelection) cursorMove {
		offset := s.startOfLineAt(sel.cursorOffset(), s.lineEndAffinityFor(sel))
		return cursorMove{offset: offset, anchor: s.preferredColumnFor(offset)}
	}, dirPtr(moveDirectionUp), w, cx)
}

// end keeps the caret at the end of the visual row it is on
// (U:input/base/movement.rs:404-415).
func (s *InputBaseState[M]) end(_ *MoveEnd, w *gpui.Window, cx *inputCx[M]) {
	s.moveAllCursors(func(sel cursorSelection) cursorMove {
		offset := s.endOfLineAt(sel.cursorOffset(), s.lineEndAffinityFor(sel))
		return cursorMove{offset: offset, anchor: s.preferredColumnFor(offset), affinity: true}
	}, dirPtr(moveDirectionDown), w, cx)
}

// moveToStart is U:input/base/movement.rs:417-424.
func (s *InputBaseState[M]) moveToStart(_ *MoveToStart, _ *gpui.Window, cx *inputCx[M]) {
	s.moveTo(0, nil, cx)
}

// moveToEnd is U:input/base/movement.rs:426-428.
func (s *InputBaseState[M]) moveToEnd(_ *MoveToEnd, _ *gpui.Window, cx *inputCx[M]) {
	s.moveTo(s.text.Len(), nil, cx)
}

// moveToPreviousWord is U:input/base/movement.rs:430-445.
func (s *InputBaseState[M]) moveToPreviousWord(_ *MoveToPreviousWord, w *gpui.Window, cx *inputCx[M]) {
	s.moveAllCursors(func(sel cursorSelection) cursorMove {
		offset := s.previousStartOfWordAt(sel.cursorOffset())
		return cursorMove{offset: offset, anchor: s.preferredColumnFor(offset)}
	}, nil, w, cx)
}

// moveToNextWord is U:input/base/movement.rs:447-462.
func (s *InputBaseState[M]) moveToNextWord(_ *MoveToNextWord, w *gpui.Window, cx *inputCx[M]) {
	s.moveAllCursors(func(sel cursorSelection) cursorMove {
		offset := s.nextEndOfWordAt(sel.cursorOffset())
		return cursorMove{offset: offset, anchor: s.preferredColumnFor(offset)}
	}, nil, w, cx)
}
