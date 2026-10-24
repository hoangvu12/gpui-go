package base

// Application-wide state shared by Base behaviors (U:global_state.rs).
//
// Deviation: the text_view_state_stack (U:global_state.rs:23, 96-98) holds
// TextView entities and joins with the TextView ticket (28).

import (
	"weak"

	"github.com/hoangvu12/gpui-go/gpui"
)

// deferredToken is the liveness cell behind a DeferredPopover (the Rust
// Rc<()>). It is padded past the tiny-allocator size so a dropped token
// is collected on its own.
type deferredToken struct {
	released bool
	_        [16]byte
}

// DeferredPopover holds the deferred interaction context open for as long
// as it is alive (U:global_state.rs:11-16). It is handed out by
// GlobalStateRegisterDeferredPopover.
//
// Go adaptation: Rust closes the context when the token drops. Go has no
// drop, so Release closes it deterministically; a token that becomes
// unreachable without Release still closes once the garbage collector
// reclaims it (the registry only holds weak pointers), which keeps the
// upstream guarantee that popup state collected without ever closing
// cannot leave the context open for the rest of the session.
type DeferredPopover struct {
	token *deferredToken
}

// Release closes this registration (Rust: drop the token). Releasing
// twice is a no-op.
func (p *DeferredPopover) Release() {
	if p != nil && p.token != nil {
		p.token.released = true
		p.token = nil
	}
}

// GlobalState is the application-wide state shared by Base behaviors
// (U:global_state.rs:18-30). The global is stored as *GlobalState. The
// selection-gesture fields moved into the core with the selection model
// (browser-core 19: the suppression flag, the selection document order
// and the touch-press timestamp live in gpui's selection gesture global,
// gpui/text_selection_globals.go — the functions below delegate there so
// one flag serves the core model and Base's controls).
type GlobalState struct {
	appMenus         []gpui.Menu
	deferredPopovers []weak.Pointer[deferredToken]
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

// GlobalStateIsInDeferredContext reports whether any deferred popup
// currently owns an open interaction context (U:global_state.rs:121-128).
func GlobalStateIsInDeferredContext(app *gpui.App) bool {
	for _, popover := range GlobalStateGlobal(app).deferredPopovers {
		if deferredAlive(popover) {
			return true
		}
	}
	return false
}

// GlobalStateRegisterDeferredPopover registers an open deferred popup,
// which stays registered for as long as the returned token is held
// (U:global_state.rs:130-149). A token rather than an identifier, because
// popup state is routinely dropped without ever being closed; a
// registration that outlived its popup would leave the application
// believing a popup is open forever.
func GlobalStateRegisterDeferredPopover(app *gpui.App) *DeferredPopover {
	token := &deferredToken{}
	s := GlobalStateGlobalMut(app)
	live := s.deferredPopovers[:0]
	for _, popover := range s.deferredPopovers {
		if deferredAlive(popover) {
			live = append(live, popover)
		}
	}
	s.deferredPopovers = append(live, weak.Make(token))
	return &DeferredPopover{token: token}
}

// deferredAlive is the Weak::strong_count() > 0 check.
func deferredAlive(p weak.Pointer[deferredToken]) bool {
	token := p.Value()
	return token != nil && !token.released
}
