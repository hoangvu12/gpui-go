package base

// Port of U:touch_selection.rs: touch selection geometry shared by the
// window text selection. Moved from base into the core with the selection
// model (browser-core ticket 19, gpui/touch_selection.go); this file
// re-exports the surface Base's callers use, and keeps the small
// unexported geometry helpers (caretLineBox) as wrappers over the core's.

import "github.com/hoangvu12/gpui-go/gpui"

type (
	// SelectionEdge is one end of a selection, as a touch handle grabs it.
	SelectionEdge = gpui.SelectionEdge
	// TouchSelectionSnapshot is where a touch selection's ends are laid
	// out this frame.
	TouchSelectionSnapshot = gpui.TouchSelectionSnapshot
)

// The selection ends.
const (
	SelectionEdgeStart = gpui.SelectionEdgeStart
	SelectionEdgeEnd   = gpui.SelectionEdgeEnd
)

// The touch handle's geometry constants (the model owns the handle
// painting; the constants stay addressable for the tests that measure
// them).
const (
	TouchHandleHitSize  = gpui.TouchHandleHitSize
	TouchHandleKnobSize = gpui.TouchHandleKnobSize
	TouchHandleBarWidth = gpui.TouchHandleBarWidth
	TouchHandleExtent   = gpui.TouchHandleExtent
)

// TouchHandleHitBounds is the touch target of one handle end.
func TouchHandleHitBounds(edge SelectionEdge, caret gpui.Bounds) gpui.Bounds {
	return gpui.TouchHandleHitBounds(edge, caret)
}

// TouchHandlePaint paints the handle for edge on the caret line box.
func TouchHandlePaint(edge SelectionEdge, caret gpui.Bounds, color gpui.Hsla, w *gpui.Window) {
	gpui.TouchHandlePaint(edge, caret, color, w)
}

// caretLineBox is the caret line box at position (the core's
// gpui.CaretLineBox, kept as the unexported spelling Base's inline
// renderer and selection adapter call).
func caretLineBox(position gpui.Point, lineHeight float32) gpui.Bounds {
	return gpui.CaretLineBox(position, lineHeight)
}
