package gpui

import (
	"fmt"
	"time"
)

// TestApp is a deterministic headless application: a virtual clock, the
// controlled foreground/background dispatcher (equivalent to the
// reference TestDispatcher), and the application core. It is the seam the
// conformance port fixture drives.
//
// Observable operations:
//
//   - Update runs one application update with effect flushing; the
//     flush draws every dirty test window (the reference test-support
//     flush, app.rs 1800-1812).
//   - RunUntilParked drains runnable tasks until the scheduler would
//     park (pending timers do not fire and do not keep it running).
//   - AdvanceClock(ms) moves the virtual clock forward, firing ready
//     timers and running their foreground continuations synchronously
//     inside the call.
//   - Tick drives one queued task to its next park point.
type TestApp struct {
	app   *App
	sched *scheduler
}

// serveFrames is the test frame loop's catch-all (Base tickets 03/33):
// every effect flush already draws the dirty test windows (app.rs
// 1800-1812, frameloop.go drawDirtyTestWindowsInFlush); after the
// scheduler parks, the dirty test windows that remain (a window dirtied
// outside an update, or re-dirtied by its own frame without queuing an
// effect) are drawn until they settle, re-running the scheduler between
// frames. Next-frame callbacks are not delivered here (an animation loop
// would never park): Window.SimulateNextFrame and
// Window.SimulateFrameRequest deliver them (window.rs 2643-2650,
// platform/test/window.rs 196-206).
func (ta *TestApp) serveFrames() {
	drawDirtyTestWindows(ta.app, ta.sched.run)
}

// NewTestApp creates a deterministic test application.
func NewTestApp() *TestApp {
	sched := newScheduler()
	app := newApp(sched)
	return &TestApp{app: app, sched: sched}
}

// App returns the application core.
func (ta *TestApp) App() *App { return ta.app }

// RootScope returns the application's root ownership scope, the natural
// owner for fixture-level entities.
func (ta *TestApp) RootScope() *Scope { return ta.app.RootScope() }

// Background returns the background executor.
func (ta *TestApp) Background() *BackgroundExecutor { return ta.app.Background() }

// Foreground returns the foreground executor.
func (ta *TestApp) Foreground() *ForegroundExecutor { return ta.app.Foreground() }

// Update runs f as one application update on the dispatcher goroutine;
// queued effects flush when it returns.
func (ta *TestApp) Update(f func(*App)) { ta.app.Update(f) }

// RunUntilParked runs tasks until the scheduler would park. Timers that
// have not yet fired keep their tasks parked. It then draws the dirty
// test windows until they settle.
func (ta *TestApp) RunUntilParked() {
	ta.sched.run()
	ta.serveFrames()
}

// AdvanceClock moves the virtual clock forward by ms, running ready
// timers and their foreground continuations synchronously. It then draws
// the dirty test windows until they settle.
func (ta *TestApp) AdvanceClock(ms int64) {
	ta.sched.advanceClock(ms)
	ta.serveFrames()
}

// Tick drives at most one queued task to its next park point, reporting
// whether work remained.
func (ta *TestApp) Tick() bool {
	ta.sched.guardNotUpdating()
	t := ta.sched.takeOne()
	if t == nil {
		return false
	}
	ta.sched.drive(t)
	return true
}

// NowMs reports the virtual clock in milliseconds.
func (ta *TestApp) NowMs() int64 { return ta.sched.nowMs() }

// Now reports the virtual clock since the test started.
func (ta *TestApp) Now() time.Duration {
	return time.Duration(ta.sched.nowMs()) * time.Millisecond
}

// WithAssets assigns the application's asset registry (the reference
// Application::with_assets, app.rs lines 218-225), the test-side
// injection of the app's asset source.
func (ta *TestApp) WithAssets(assets *AssetRegistry) *TestApp {
	ta.app.SetAssets(assets)
	return ta
}

// WithHTTPClient injects the application's HTTP client (the reference
// Application::with_http_client, app.rs lines 234-239). Without it the
// app keeps the NullHttpClient default — zero network attempts.
func (ta *TestApp) WithHTTPClient(client HttpClient) *TestApp {
	ta.app.SetHTTPClient(client)
	return ta
}

// WithSystemPreferences pins the system-preference record this hostless
// application reports through App.SystemPreferences (browser-core ticket
// 74): a test drives the record an attached host would have read from
// Windows — the OS half of the reduced-motion gate (App.MotionReduced)
// and the preference-driven styling. A later call replaces the pin, so
// a test can flip a preference mid-flight; SystemColors still answers
// the default palette (a host is needed to pin colours).
func (ta *TestApp) WithSystemPreferences(prefs SystemPreferences) *TestApp {
	ta.app.testSystemPreferences = &prefs
	return ta
}

// ---------------------------------------------------------------------------
// Base ticket 12: the TestAppContext window and input surface
// (R:app/test_context.rs, R:app/test_app.rs; R: = reference/ce-source/
// crates/gpui/src at 254b5dbd)
// ---------------------------------------------------------------------------

// access implements AppContext: a TestApp is an application context like
// the reference TestAppContext (R:app/test_context.rs impl AppContext for
// TestAppContext), so entities can be created and read through it.
func (ta *TestApp) access() appAccess { return appAccess{} }

// appRef recovers the application behind the context.
func (ta *TestApp) appRef() *App { return ta.app }

// openTestWindowView opens a test window of the given size whose root
// view is built inside the window's context, drawn once before the
// open returns (R:app.rs:1339-1359 open_window: cx.new(build_root_view),
// then window.draw).
func openTestWindowView[S any, PS interface {
	*S
	Render[S]
}](ta *TestApp, size Size, build func(*S, *Window, *Context[S])) (Entity[S], *Window) {
	window := NewTestWindow(ta.app, size)
	var root Entity[S]
	ta.app.Update(func(app *App) {
		root = NewEntity(app, window.Scope(), func(state *S, cx *Context[S]) {
			if build != nil {
				build(state, window, cx)
			}
		})
		window.SetRootView(ViewOf[S, PS](root))
		if _, err := DrawWindowFrame(window); err != nil {
			panic(fmt.Sprintf("gpui: open test window: initial draw: %v", err))
		}
	})
	return root, window
}

// AddWindowView opens a test window at the test display's maximized
// bounds (1920x1080) whose root view of state S is built by build, and
// returns the root view and a VisualTestContext for the window, after
// running until parked (R:app/test_context.rs:313-336 add_window_view).
// Ported Base tests shadow their context with the returned one:
//
//	view, cx := ta.AddWindowView(func(v *MyView, w *Window, cx *Context[MyView]) { ... })
func (ta *TestApp) AddWindowView[S any, PS interface {
	*S
	Render[S]
}](build func(*S, *Window, *Context[S])) (Entity[S], *VisualTestContext) {
	root, window := openTestWindowView[S, PS](ta, testDisplaySize, build)
	cx := NewVisualTestContext(window, ta)
	cx.RunUntilParked()
	return root, cx
}

// AddEmptyWindow opens a test window with no content at the test
// display's maximized bounds and returns its VisualTestContext after
// running until parked (R:app/test_context.rs:295-308 add_empty_window:
// the root renders Empty).
func (ta *TestApp) AddEmptyWindow() *VisualTestContext {
	_, window := openTestWindowView[emptyTestView](ta, testDisplaySize, nil)
	cx := NewVisualTestContext(window, ta)
	cx.RunUntilParked()
	return cx
}

// emptyTestView is add_empty_window's root (`cx.new(|_| Empty)`).
type emptyTestView struct{}

// Render implements Render: the Empty element.
func (*emptyTestView) Render(w *Window, cx *Context[emptyTestView]) AnyElement { return Empty() }

// OpenWindow opens a test window at the test display's maximized bounds
// with the root view built by build, running until parked
// (R:app/test_app.rs:157-176 TestApp::open_window; update runs until
// parked, R:app/test_app.rs:115-122).
func (ta *TestApp) OpenWindow[S any, PS interface {
	*S
	Render[S]
}](build func(*S, *Window, *Context[S])) *TestAppWindow[S] {
	return ta.OpenWindowWithOptions[S, PS](WindowOptions{}, build)
}

// OpenWindowWithOptions opens a test window with the given options
// (R:app/test_app.rs:179-195 open_window_with_options). A test window
// reads only the requested size (opts.Bounds.Size); a zero size opens at
// the test display's maximized bounds like OpenWindow.
func (ta *TestApp) OpenWindowWithOptions[S any, PS interface {
	*S
	Render[S]
}](opts WindowOptions, build func(*S, *Window, *Context[S])) *TestAppWindow[S] {
	size := opts.Bounds.Size
	if size.Width <= 0 || size.Height <= 0 {
		size = testDisplaySize
	}
	root, window := openTestWindowView[S, PS](ta, size, build)
	ta.RunUntilParked()
	return &TestAppWindow[S]{ta: ta, window: window, root: root}
}

// DispatchAction dispatches the action to the focused node of the
// window, then runs until parked (R:app/test_context.rs:503-514
// dispatch_action).
func (ta *TestApp) DispatchAction(w *Window, action BoxedAction) {
	requireOpenTestWindow(ta, w)
	ta.app.Update(func(app *App) {
		w.DispatchBoxed(action, app)
	})
	ta.RunUntilParked()
}

// SimulateKeystrokes types a space-separated list of keystrokes into the
// window, then runs until parked (R:app/test_context.rs:516-530
// simulate_keystrokes): ta.SimulateKeystrokes(w, "ctrl-a backspace").
// Every component parses with the pinned keystroke syntax
// (ParseKeystroke; an invalid component panics like the pinned unwrap)
// and dispatches through Window.DispatchKeystroke, each in its own
// window update.
func (ta *TestApp) SimulateKeystrokes(w *Window, keystrokes string) {
	for _, keystroke := range parseSimulatedKeystrokes(keystrokes) {
		ta.DispatchKeystroke(w, keystroke)
	}
	ta.RunUntilParked()
}

// SimulateInput types text into the window, then runs until parked
// (R:app/test_context.rs:532-542 simulate_input): the text splits like
// Rust's str::split(""), so every char dispatches as one keystroke
// between an empty leading and trailing keystroke (which type nothing).
func (ta *TestApp) SimulateInput(w *Window, input string) {
	for _, component := range splitOnEmpty(input) {
		ta.DispatchKeystroke(w, MustParseKeystroke(component))
	}
	ta.RunUntilParked()
}

// DispatchKeystroke dispatches one keystroke inside the window's update
// (R:app/test_context.rs:544-550 dispatch_keystroke). It does not run
// until parked.
func (ta *TestApp) DispatchKeystroke(w *Window, keystroke Keystroke) {
	requireOpenTestWindow(ta, w)
	ta.app.Update(func(app *App) {
		w.dispatchKeystroke(keystroke, app)
	})
}

// SimulateWindowResize resizes the test window to size (R:app/
// test_context.rs:423-426 simulate_window_resize → R:platform/test/
// window.rs:157-168 simulate_resize → the resize callback, R:window.rs:
// 1847-1854, which runs Window::bounds_changed, R:window.rs:2694-2705:
// the viewport takes the content size, the mouse position takes the
// platform's (the test window reports the origin, R:platform/test/
// window.rs:262-264) and the window refreshes). The refresh's draw runs
// in the update's flush. It does not run until parked (the pin neither).
func (ta *TestApp) SimulateWindowResize(w *Window, size Size) {
	requireOpenTestWindow(ta, w)
	ds, ok := windowDrawStates[w]
	if !ok || !ds.testWindow {
		panic("gpui: SimulateWindowResize requires a test window")
	}
	ta.app.Update(func(app *App) {
		ds.viewport = size
		inputState(w).mousePosition = Point{}
		w.Refresh()
	})
}
