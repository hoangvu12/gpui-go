package gpui

// This file is Base ticket 12's window-bound test contexts:
//
//   - VisualTestContext, the port of R:app/test_context.rs:754-1005 (the
//     test-equivalent of a Window plus App, dereferencing to its
//     TestAppContext — the port embeds *TestApp);
//   - TestAppWindow, the port of R:app/test_app.rs:320-494 (the window
//     handle of the simpler TestApp API).
//
// R: = reference/ce-source/crates/gpui/src at
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a. Every simulated event runs
// through testsim.go's simulateEvent: the window's real dispatch inside
// one update (whose flush draws the dirty test windows, R:app.rs:
// 1800-1812), then the scheduler park. Hit testing therefore uses the
// last completed frame exactly like the pin ("Make sure you've called
// draw first!", R:app/test_context.rs:945-946; the opening draw and the
// flush draws keep it current).

// ---------------------------------------------------------------------------
// VisualTestContext (R:app/test_context.rs:754-1005)
// ---------------------------------------------------------------------------

// VisualTestContext is a test context bound to one window (the reference
// VisualTestContext): the TestApp methods remain available through the
// embedded *TestApp (the pin's Deref to TestAppContext), and the
// window-bound methods shadow their TestApp spellings.
type VisualTestContext struct {
	// TestApp is the original test application context (the pin's pub
	// cx: TestAppContext).
	*TestApp
	window *Window
}

// NewVisualTestContext binds a test context to a window of ta
// (R:app/test_context.rs:776-784 VisualTestContext::from_window).
func NewVisualTestContext(window *Window, ta *TestApp) *VisualTestContext {
	if ta == nil {
		panic("gpui: NewVisualTestContext requires a test application")
	}
	requireOpenTestWindow(ta, window)
	return &VisualTestContext{TestApp: ta, window: window}
}

// access implements AppContext (R:app/test_context.rs:1007 impl
// AppContext for VisualTestContext).
func (cx *VisualTestContext) access() appAccess { return appAccess{} }

// appRef recovers the application behind the context.
func (cx *VisualTestContext) appRef() *App { return cx.TestApp.app }

// Window returns the bound window (the pin's window handle).
func (cx *VisualTestContext) Window() *Window { return cx.window }

// Update runs f with the window and the application inside the window's
// update (R:app/test_context.rs:769-774 update → update_window): effects
// flush when f returns, drawing the dirty test windows. It panics when
// the window was closed (the pin's unwrap).
func (cx *VisualTestContext) Update(f func(w *Window, app *App)) {
	requireOpenTestWindow(cx.TestApp, cx.window)
	cx.TestApp.app.Update(func(app *App) { f(cx.window, app) })
}

// DispatchAction dispatches the action to the focused node, then runs
// until parked (R:app/test_context.rs:791-797).
func (cx *VisualTestContext) DispatchAction(action BoxedAction) {
	cx.TestApp.DispatchAction(cx.window, action)
}

// SimulateKeystrokes types a space-separated keystroke sequence, e.g.
// cx.SimulateKeystrokes("ctrl-a backspace"), then runs until parked
// (R:app/test_context.rs:814-818 → 516-530).
func (cx *VisualTestContext) SimulateKeystrokes(keystrokes string) {
	cx.TestApp.SimulateKeystrokes(cx.window, keystrokes)
}

// SimulateInput types text, e.g. cx.SimulateInput("hello"), then runs
// until parked (R:app/test_context.rs:820-824 → 532-542).
func (cx *VisualTestContext) SimulateInput(input string) {
	cx.TestApp.SimulateInput(cx.window, input)
}

// SimulateMouseMove simulates a mouse move to position with the pressed
// button (nil: none) and modifiers (R:app/test_context.rs:826-838).
func (cx *VisualTestContext) SimulateMouseMove(position Point, button *MouseButton, modifiers Modifiers) {
	var pressed *MouseButton
	if button != nil {
		b := *button
		pressed = &b
	}
	cx.SimulateEvent(&MouseMoveEvent{Position: position, Modifiers: modifiers, PressedButton: pressed})
}

// SimulateMouseDown simulates a single-click press of button at position
// (R:app/test_context.rs:840-854: click_count 1, first_mouse false).
func (cx *VisualTestContext) SimulateMouseDown(position Point, button MouseButton, modifiers Modifiers) {
	cx.SimulateEvent(&MouseDownEvent{Position: position, Modifiers: modifiers, Button: button, ClickCount: 1, FirstMouse: false})
}

// SimulateMouseUp simulates a single-click release of button at position
// (R:app/test_context.rs:856-869: click_count 1).
func (cx *VisualTestContext) SimulateMouseUp(position Point, button MouseButton, modifiers Modifiers) {
	cx.SimulateEvent(&MouseUpEvent{Position: position, Modifiers: modifiers, Button: button, ClickCount: 1})
}

// SimulateClick simulates a primary (left) button click at position: a
// press then a release, each its own simulated event
// (R:app/test_context.rs:871-886).
func (cx *VisualTestContext) SimulateClick(position Point, modifiers Modifiers) {
	cx.SimulateEvent(&MouseDownEvent{Position: position, Modifiers: modifiers, Button: MouseButtonLeft, ClickCount: 1, FirstMouse: false})
	cx.SimulateEvent(&MouseUpEvent{Position: position, Modifiers: modifiers, Button: MouseButtonLeft, ClickCount: 1})
}

// SimulateModifiersChange simulates a modifiers change with capslock off
// (R:app/test_context.rs:888-894).
func (cx *VisualTestContext) SimulateModifiersChange(modifiers Modifiers) {
	cx.SimulateEvent(&ModifiersChangedEvent{Modifiers: modifiers, Capslock: Capslock{On: false}})
}

// SimulateCapslockChange simulates a capslock change with no modifiers
// (R:app/test_context.rs:896-902).
func (cx *VisualTestContext) SimulateCapslockChange(on bool) {
	cx.SimulateEvent(&ModifiersChangedEvent{Modifiers: NoModifiers(), Capslock: Capslock{On: on}})
}

// SimulateResize simulates the user resizing the window
// (R:app/test_context.rs:904-907 → TestApp.SimulateWindowResize).
func (cx *VisualTestContext) SimulateResize(size Size) {
	cx.TestApp.SimulateWindowResize(cx.window, size)
}

// DebugBounds returns the last completed frame's bounds of the element
// with the given debug selector (R:app/test_context.rs:909-912
// debug_bounds: rendered_frame.debug_bounds.get(selector)).
func (cx *VisualTestContext) DebugBounds(selector string) (Bounds, bool) {
	return WindowDebugBound(cx.window, selector)
}

// SimulateEvent simulates a platform input event — a mouse, scroll,
// pinch, file-drop or keyboard event, by value or pointer — dispatched
// through the window, then runs until parked
// (R:app/test_context.rs:945-951 simulate_event). It returns the
// dispatch result.
func (cx *VisualTestContext) SimulateEvent(event any) DispatchEventResult {
	return simulateEvent(cx.TestApp, cx.window, event)
}

// A11ySnapshot returns the window's accessibility tree as of a fresh
// frame: the window's accessibility runtime is activated when it is not
// yet (ActivateWindowA11y, whose refresh draws on the park), the
// scheduler runs until parked and the frame's build is published
// (PublishWindowA11y). Per-element facts (role, label, value, bounds,
// focus binding, answered actions) are the published nodes; the node of
// an element is A11yNodeIDOf its global element id path.
func (cx *VisualTestContext) A11ySnapshot() (*A11ySnapshot, error) {
	if st := windowA11yStateOf(cx.window); st == nil || !st.active {
		ActivateWindowA11y(cx.window)
	} else {
		cx.window.Refresh()
	}
	cx.RunUntilParked()
	return PublishWindowA11y(cx.window)
}

// A11yNode returns the published accessibility node of the element with
// the given global element id path (A11ySnapshot, then A11yNodeIDOf).
func (cx *VisualTestContext) A11yNode(path ...ElementID) (A11ySnapshotNode, bool) {
	snapshot, err := cx.A11ySnapshot()
	if err != nil {
		return A11ySnapshotNode{}, false
	}
	id, ok := A11yNodeIDOf(&GlobalElementID{Path: path})
	if !ok {
		return A11ySnapshotNode{}, false
	}
	return snapshot.Node(id)
}

// ---------------------------------------------------------------------------
// TestAppWindow (R:app/test_app.rs:320-494)
// ---------------------------------------------------------------------------

// TestAppWindow is a test window with a root view of state S and the
// simulation helpers of the pin's TestAppWindow: every helper runs
// until parked after it dispatches.
type TestAppWindow[S any] struct {
	ta     *TestApp
	window *Window
	root   Entity[S]
}

// Window returns the window (R:app/test_app.rs:329-332 handle).
func (tw *TestAppWindow[S]) Window() *Window { return tw.window }

// Root returns the root view entity (R:app/test_app.rs:334-342).
func (tw *TestAppWindow[S]) Root() Entity[S] {
	requireOpenTestWindow(tw.ta, tw.window)
	return tw.root
}

// Update updates the root view with the window and its context, then
// runs until parked (R:app/test_app.rs:344-357).
func (tw *TestAppWindow[S]) Update(f func(state *S, w *Window, cx *Context[S])) {
	requireOpenTestWindow(tw.ta, tw.window)
	tw.ta.app.Update(func(app *App) {
		if err := tw.root.UpdateIn(app, tw.window, f); err != nil {
			panic(err)
		}
	})
	tw.ta.RunUntilParked()
}

// Read reads the root view (R:app/test_app.rs:359-372).
func (tw *TestAppWindow[S]) Read[R any](f func(state *S, app *App) R) R {
	requireOpenTestWindow(tw.ta, tw.window)
	return tw.root.Read(tw.ta.app, f)
}

// SimulateKeystroke parses and dispatches one keystroke, then runs until
// parked (R:app/test_app.rs:384-396).
func (tw *TestAppWindow[S]) SimulateKeystroke(keystroke string) {
	tw.ta.DispatchKeystroke(tw.window, MustParseKeystroke(keystroke))
	tw.ta.RunUntilParked()
}

// SimulateKeystrokes types a space-separated keystroke sequence, running
// until parked after each keystroke (R:app/test_app.rs:398-403).
func (tw *TestAppWindow[S]) SimulateKeystrokes(keystrokes string) {
	for _, keystroke := range splitOnSpace(keystrokes) {
		tw.SimulateKeystroke(keystroke)
	}
}

// SimulateInput types text one char at a time (R:app/test_app.rs:
// 405-410: input.chars(), no empty components).
func (tw *TestAppWindow[S]) SimulateInput(input string) {
	for _, ch := range input {
		tw.SimulateKeystroke(string(ch))
	}
}

// SimulateMouseMove simulates a mouse move with no pressed button and no
// modifiers (R:app/test_app.rs:412-419).
func (tw *TestAppWindow[S]) SimulateMouseMove(position Point) {
	tw.SimulateEvent(&MouseMoveEvent{Position: position, Modifiers: Modifiers{}, PressedButton: nil})
}

// SimulateMouseDown simulates a single-click press (R:app/test_app.rs:
// 421-430).
func (tw *TestAppWindow[S]) SimulateMouseDown(position Point, button MouseButton) {
	tw.SimulateEvent(&MouseDownEvent{Position: position, Button: button, Modifiers: Modifiers{}, ClickCount: 1, FirstMouse: false})
}

// SimulateMouseUp simulates a single-click release (R:app/test_app.rs:
// 432-440).
func (tw *TestAppWindow[S]) SimulateMouseUp(position Point, button MouseButton) {
	tw.SimulateEvent(&MouseUpEvent{Position: position, Button: button, Modifiers: Modifiers{}, ClickCount: 1})
}

// SimulateClick simulates a press then a release of button at position
// (R:app/test_app.rs:442-446).
func (tw *TestAppWindow[S]) SimulateClick(position Point, button MouseButton) {
	tw.SimulateMouseDown(position, button)
	tw.SimulateMouseUp(position, button)
}

// SimulateScroll simulates a pixel-precise wheel scroll in the Moved
// phase (R:app/test_app.rs:448-456).
func (tw *TestAppWindow[S]) SimulateScroll(position Point, delta Point) {
	tw.SimulateEvent(&ScrollWheelEvent{
		Position:   position,
		Delta:      ScrollDeltaPixels(delta.X, delta.Y),
		Modifiers:  Modifiers{},
		TouchPhase: TouchPhaseMoved,
	})
}

// SimulateEvent dispatches a platform input event, then runs until
// parked (R:app/test_app.rs:458-470).
func (tw *TestAppWindow[S]) SimulateEvent(event any) DispatchEventResult {
	return simulateEvent(tw.ta, tw.window, event)
}

// SimulateResize resizes the window, then runs until parked
// (R:app/test_app.rs:472-483).
func (tw *TestAppWindow[S]) SimulateResize(size Size) {
	tw.ta.SimulateWindowResize(tw.window, size)
	tw.ta.RunUntilParked()
}

// Draw forces a redraw of the window (R:app/test_app.rs:485-493:
// window.draw(cx).clear(cx) inside update_window).
func (tw *TestAppWindow[S]) Draw() {
	requireOpenTestWindow(tw.ta, tw.window)
	tw.ta.app.Update(func(app *App) {
		if _, err := DrawWindowFrame(tw.window); err != nil {
			panic(err)
		}
	})
}
