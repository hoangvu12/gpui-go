package gpui

// The selection gesture's application-global state, split out of Base's
// GlobalState (U:global_state.rs) when the selection model moved into the
// core (browser-core ticket 19): the mouse-down suppression flag the
// model reads, the document-order counter its participants draw from, and
// the touch-press timestamp the double-tap arm consults. Base's
// GlobalState* wrappers delegate here, so a Base control that suppresses
// selection for its own press still suppresses the core model — one flag,
// two spellings.
//
// The gesture state (browser-core 19, Chromium's selection_controller.cc):
// a control that owns a mouse press calls SuppressTextSelection so the
// window's drag selection does not also start (SelectionController's
// selectstart gating, selection_controller.cc:665-671 — the suppression is
// reset at the next press's capture phase, paintTextSelection's arm); the
// counter restarts each selection frame and hands each registered
// participant its paint order (U:global_state.rs:100-109).

import "time"

// selectionGestureState is the global's payload (the selection-related
// fields of U:global_state.rs:18-30).
type selectionGestureState struct {
	suppressTextSelection  bool
	selectionDocumentOrder uint64
	lastTouch              time.Time
	hasLastTouch           bool
}

// selectionGestureGlobals returns (creating when absent) the app's
// selection gesture global.
func selectionGestureGlobals(app *App) *selectionGestureState {
	if s, ok := TryGlobal[*selectionGestureState](app); ok {
		return s
	}
	s := &selectionGestureState{selectionDocumentOrder: 1}
	app.SetGlobal(s)
	return s
}

// SuppressTextSelection suppresses window-level text selection for the
// current mouse down, so a control owning a press or drag does not also
// start selection (U:global_state.rs:68-74; Base's
// GlobalStateSuppressTextSelection delegates here).
func SuppressTextSelection(app *App) {
	selectionGestureGlobals(app).suppressTextSelection = true
}

// ResetTextSelectionSuppression clears the current mouse-down suppression
// (U:global_state.rs:76-80).
func ResetTextSelectionSuppression(app *App) {
	selectionGestureGlobals(app).suppressTextSelection = false
}

// IsTextSelectionSuppressed reports whether the current mouse down
// suppresses selection — the model's internal read, exported for Base's
// GlobalState delegation (browser-core 19).
func IsTextSelectionSuppressed(app *App) bool {
	s, ok := TryGlobal[*selectionGestureState](app)
	return ok && s.suppressTextSelection
}

// NoteTouchPress records that a finger went down
// (U:global_state.rs:44-49; Base's GlobalStateNoteTouch delegates here).
func NoteTouchPress(app *App) {
	s := selectionGestureGlobals(app)
	s.lastTouch, s.hasLastTouch = time.Now(), true
}

// IsTouchPress reports whether the press being handled came from a
// finger: a touch went down less than a second ago, within the
// double-tap window (U:global_state.rs:51-58).
func IsTouchPress(app *App) bool {
	s, ok := TryGlobal[*selectionGestureState](app)
	return ok && s.hasLastTouch && time.Since(s.lastTouch) < time.Second
}

// beginSelectionFrame restarts the selection document order
// (U:global_state.rs:100-103): the layer's prepaint call.
func beginSelectionFrame(app *App) {
	selectionGestureGlobals(app).selectionDocumentOrder = 1
}

// nextSelectionDocumentOrder hands out the next document order
// (U:global_state.rs:105-109; wrapping add). Exposed for the participants
// that register in paint order (Base's TextView adapter).
func nextSelectionDocumentOrder(app *App) uint64 {
	s := selectionGestureGlobals(app)
	order := s.selectionDocumentOrder
	s.selectionDocumentOrder++
	return order
}

// NextSelectionDocumentOrder is the exported spelling of the paint-order
// counter (browser-core 19): a participant that registers per frame calls
// it from its paint and passes the value to
// TextSelectionRegistration.WithDocumentOrder.
func NextSelectionDocumentOrder(app *App) uint64 { return nextSelectionDocumentOrder(app) }
