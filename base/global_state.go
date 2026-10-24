package base

// Application-wide state shared by Base behaviors (U:global_state.rs).
//
// Deviation: the text_view_state_stack (U:global_state.rs:23, 96-98) holds
// TextView entities and joins with the TextView ticket (28).
//
// browser-core ticket 72: the deferred-popover liveness list is deleted
// — the top-layer entry's own registration (gpui/toplayer.go) is the
// popup's liveness, and the entry's frame-end reconciliation closes what
// stopped rendering, which is what the list's tokens approximated
// (U:global_state.rs:11-16, 121-149).

import "github.com/hoangvu12/gpui-go/gpui"

// GlobalState is the application-wide state shared by Base behaviors
// (U:global_state.rs:18-30). The global is stored as *GlobalState. The
// selection-gesture fields moved into the core with the selection model
// (browser-core 19: the suppression flag, the selection document order
// and the touch-press timestamp live in gpui's selection gesture global,
// gpui/text_selection_globals.go — the functions below delegate there so
// one flag serves the core model and Base's controls).
type GlobalState struct {
	appMenus []gpui.Menu
}

// newGlobalState returns the initial state (U:global_state.rs:33-42;
// the selection-gesture counters moved to the core's own global).
func newGlobalState() *GlobalState {
	return &GlobalState{}
}

// GlobalStateNoteTouch records that a finger went down
// (U:global_state.rs:44-49; the timestamp lives in the core's selection
// gesture global, browser-core 19).
func GlobalStateNoteTouch(app *gpui.App) {
	GlobalStateInit(app)
	gpui.NoteTouchPress(app)
}

// GlobalStateIsTouchPress reports whether the press being handled came
// from a finger: a touch went down less than a second ago, within the
// double-tap window (U:global_state.rs:51-58).
func GlobalStateIsTouchPress(app *gpui.App) bool {
	return gpui.IsTouchPress(app)
}

// GlobalStateInit ensures the Base global exists (U:global_state.rs:60-66).
func GlobalStateInit(app *gpui.App) {
	if !gpui.HasGlobal[*GlobalState](app) {
		app.SetGlobal(newGlobalState())
	}
}

// GlobalStateSuppressTextSelection suppresses window-level text selection
// for the current mouse down, so a control owning a press or drag does
// not also start application text selection (U:global_state.rs:68-74;
// the flag is the core model's own, browser-core 19).
func GlobalStateSuppressTextSelection(app *gpui.App) {
	GlobalStateInit(app)
	gpui.SuppressTextSelection(app)
}

// GlobalStateResetTextSelectionSuppression clears the current mouse-down
// suppression (U:global_state.rs:76-80).
func GlobalStateResetTextSelectionSuppression(app *gpui.App) {
	gpui.ResetTextSelectionSuppression(app)
}

// GlobalStateIsTextSelectionSuppressed reports whether the current mouse
// down suppresses text selection (U:global_state.rs:82-86).
func GlobalStateIsTextSelectionSuppressed(app *gpui.App) bool {
	return gpui.IsTextSelectionSuppressed(app)
}

// GlobalStateGlobal returns the global; it panics when GlobalStateInit
// has not run (U:global_state.rs:88-90).
func GlobalStateGlobal(app *gpui.App) *GlobalState { return gpui.Global[*GlobalState](app) }

// GlobalStateGlobalMut returns the global for mutation, queuing a global
// notification like App::global_mut (U:global_state.rs:92-94,
// R:app.rs:2128-2135). It panics when GlobalStateInit has not run.
func GlobalStateGlobalMut(app *gpui.App) *GlobalState {
	s := gpui.Global[*GlobalState](app)
	app.NotifyGlobal[*GlobalState]()
	return s
}

// The selection document order moved into the core's selection gesture
// global with the selection model (browser-core 19: the layer's
// BeginSelectionFrame and the participants' paint-order counter live in
// gpui/text_selection_globals.go).

// AppMenus returns the application menus (U:global_state.rs:111-114).
func (s *GlobalState) AppMenus() []gpui.Menu { return s.appMenus }

// SetAppMenus replaces the application menus (U:global_state.rs:116-119).
func (s *GlobalState) SetAppMenus(menus []gpui.Menu) { s.appMenus = menus }
