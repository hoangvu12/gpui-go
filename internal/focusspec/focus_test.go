//go:build windows

// Package focusspec holds ticket13's focus/keyboard/action dispatch
// tests: deterministic dispatch transcripts through the real element
// pipeline (test windows drawn with the real layout/scene services)
// plus real-host input evidence through the Win32 keyboard translation
// (focus_real_test.go).
//
// Environment expectation: the headless tests draw through the native
// layout services; the real tests create real Win32 windows in the
// user's interactive session and FAIL (never skip) when window creation
// fails, like internal/winhostspec.
package focusspec

import (
	"testing"

	"gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// The dispatch corpus: a two-context element tree with a focused view,
// key listeners and a typed action
// ---------------------------------------------------------------------------

// increment is the corpus action.
type increment struct{}

// incrementAction is its canonical descriptor.
var incrementAction = gpui.DefineUnitAction[increment]("focusspec::Increment")

// shadow is the corpus's shadowed action (bound in the root context).
type shadow struct{}

// shadowAction is its canonical descriptor.
var shadowAction = gpui.DefineUnitAction[shadow]("focusspec::Shadow")

// keysView is the corpus view state: the focus handle and the shared
// transcript recorder.
type keysView struct {
	focus      gpui.FocusHandle
	transcript *[]string
}

// Render implements Render[keysView]: a Root context div with a key
// listener wrapping a focused Keys context div with a key listener and
// the typed action listener.
func (v *keysView) Render(w *gpui.Window, cx *gpui.Context[keysView]) gpui.AnyElement {
	transcript := v.transcript
	return gpui.Div().
		KeyContext("Root").
		OnKeyDown(func(e *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
			*transcript = append(*transcript, "root-keydown:"+e.Keystroke.Key)
		}).
		OnAction(shadowAction, func(a *shadow, w *gpui.Window, app *gpui.App) {
			*transcript = append(*transcript, "shadow")
		}).
		Child(gpui.Div().
			KeyContext("Keys").
			TrackFocus(v.focus).
			OnKeyDown(func(e *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
				*transcript = append(*transcript, "keys-keydown:"+e.Keystroke.Key)
			}).
			OnAction(incrementAction, func(a *increment, w *gpui.Window, app *gpui.App) {
				*transcript = append(*transcript, "increment")
			})).
		IntoElement()
}

// keyCorpus is one drawn test window with its recorder.
type keyCorpus struct {
	ta         *gpui.TestApp
	app        *gpui.App
	window     *gpui.Window
	view       gpui.Entity[keysView]
	focus      gpui.FocusHandle
	transcript *[]string
}

// newKeyCorpus builds one corpus: an app, a test window, a focused view
// recording into the transcript, and one drawn frame.
func newKeyCorpus(t *testing.T, bindings ...gpui.KeyBinding) *keyCorpus {
	t.Helper()
	ta := gpui.NewTestApp()
	app := ta.App()
	if len(bindings) > 0 {
		app.BindKeys(bindings...)
	}
	window := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 200})

	focus := gpui.NewFocusHandle(window)
	var transcript []string

	view := gpui.NewEntity(app, window.Scope(), func(v *keysView, cx *gpui.Context[keysView]) {
		v.focus = focus
		v.transcript = &transcript
	})
	window.SetRootView(gpui.ViewOf(view))

	// Focus before the draw so the dispatch tree registers the focus
	// identity and the frame's tab stops.
	window.Focus(focus)

	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return &keyCorpus{ta: ta, app: app, window: window, view: view, focus: focus, transcript: &transcript}
}

// dispatch sends one key-down event and returns the propagation state.
func (k *keyCorpus) dispatch(keystroke string) bool {
	return k.window.DispatchKeyEvent(&gpui.KeyDownEvent{
		Keystroke: gpui.MustParseKeystroke(keystroke),
	}, k.app)
}

func (k *keyCorpus) drawAgain(t *testing.T) {
	t.Helper()
	if _, err := gpui.DrawWindowFrame(k.window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
}

func (k *keyCorpus) recorded() []string {
	return append([]string{}, (*k.transcript)...)
}

// requireTranscript asserts the exact ordered transcript.
func requireTranscript(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("transcript = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("transcript[%d] = %q, want %q (full: %v vs %v)", i, got[i], want[i], got, want)
		}
	}
}

// TestFocusHandleLifecycle checks the focus runtime: focus, focused,
// blur, disable, the idempotent logical release, the focus-lost
// notification on releasing the focused handle, the weak upgrade
// failure and listener cancellation.
func TestFocusHandleLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("focus lifecycle draws through the native layout services")
	}
	k := newKeyCorpus(t)
	w := k.window

	if got := w.Focused(); !got.Ok || got.Handle.ID() != k.focus.ID() {
		t.Fatalf("focused = %+v, want the corpus handle %d", got, k.focus.ID())
	}
	if !k.focus.IsFocused(w) {
		t.Fatal("IsFocused = false after focus")
	}
	if w.FocusGeneration() == 0 {
		t.Error("focus generation did not advance on focus")
	}
	// Contains: the focused handle contains itself through the rendered
	// dispatch tree.
	if !k.focus.Contains(k.focus, w) {
		t.Fatal("Contains(self) = false")
	}

	// Blur clears focus.
	w.Blur()
	if w.Focused().Ok {
		t.Fatal("focused after blur")
	}
	if k.focus.IsFocused(w) {
		t.Fatal("IsFocused after blur")
	}

	// Re-focus for the release path, with a focus-lost listener.
	w.Focus(k.focus)
	var focusLost int
	sub := w.OnFocusLost(func(w *gpui.Window) { focusLost++ })

	// Logical release is idempotent; the focused release fires the
	// focus-lost listeners exactly once and clears focus.
	k.focus.Release()
	k.focus.Release()
	if focusLost != 1 {
		t.Fatalf("focus-lost notifications = %d, want 1", focusLost)
	}
	if w.Focused().Ok {
		t.Fatal("focused after the handle release")
	}
	// A released handle cannot be focused again.
	w.Focus(k.focus)
	if w.Focused().Ok {
		t.Fatal("released handle refocused")
	}

	// The weak handle's upgrade fails at logical zero, irreversibly.
	weak := k.focus.Downgrade()
	if _, ok := weak.Upgrade(); ok {
		t.Fatal("weak upgrade of a released handle succeeded")
	}

	// Listener cancellation: the subscription no longer fires.
	sub.Cancel()
	fresh := gpui.NewFocusHandle(w)
	w.Focus(fresh)
	fresh.Release()
	if focusLost != 1 {
		t.Fatalf("focus-lost notifications after Cancel = %d, want 1", focusLost)
	}

	// DisableFocus blurs and refuses refocus.
	w.DisableFocus()
	if w.Focused().Ok {
		t.Fatal("focused after DisableFocus")
	}
	other := gpui.NewFocusHandle(w)
	w.Focus(other)
	if w.Focused().Ok {
		t.Fatal("focus accepted while disabled")
	}
}

// TestKeyDispatchActionThroughFocusTree checks the observable core: a
// focused counter reacts to a bound keystroke through the keymap, the
// action dispatch bubbles with stop-by-default, and the context
// precedence shadows the shallower binding.
func TestKeyDispatchActionThroughFocusTree(t *testing.T) {
	if testing.Short() {
		t.Skip("dispatch draws through the native layout services")
	}
	k := newKeyCorpus(t,
		gpui.MustKeyBinding("ctrl-i", incrementAction.Box(increment{}), "Keys"),
		// The root-context binding for the same keystroke loses to the
		// deeper Keys binding (depth precedence).
		gpui.MustKeyBinding("ctrl-i", shadowAction.Box(shadow{}), "Root"),
	)

	if propagate := k.dispatch("ctrl-i"); propagate {
		t.Fatal("handled keystroke propagated (want consumed)")
	}
	requireTranscript(t, k.recorded(), "increment")

	// An unbound keystroke falls through to the key listeners in
	// capture/bubble order with no action.
	if propagate := k.dispatch("x"); !propagate {
		t.Fatal("unbound keystroke was consumed")
	}
	requireTranscript(t, k.recorded(), "increment", "keys-keydown:x", "root-keydown:x")
}

// TestKeyDispatchCaptureBubbleOrder checks the listener phase order:
// capture root → capture child, then bubble child → bubble root, and
// that stopping propagation in a capture listener skips the bubble
// phase.
func TestKeyDispatchCaptureBubbleOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("dispatch draws through the native layout services")
	}
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 200})
	focus := gpui.NewFocusHandle(window)
	var transcript []string

	window.SetRootView(gpui.ViewOf(gpui.NewEntity(app, window.Scope(), func(v *phaseView, cx *gpui.Context[phaseView]) {
		v.focus = focus
		v.transcript = &transcript
	})))
	window.Focus(focus)
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}

	window.DispatchKeyEvent(&gpui.KeyDownEvent{
		Keystroke: gpui.MustParseKeystroke("k"),
	}, app)
	requireTranscript(t, transcript,
		"capture:root", "capture:child",
		"bubble:child", "bubble:root")

	// Stopping propagation during a capture listener skips the rest.
	transcript = nil
	window.DispatchKeyEvent(&gpui.KeyDownEvent{
		Keystroke: gpui.MustParseKeystroke("s"),
	}, app)
	requireTranscript(t, transcript, "capture:root", "capture:child", "stop")
}

// phaseView renders the capture/bubble phase corpus: the child stops
// propagation on "s".
type phaseView struct {
	focus      gpui.FocusHandle
	transcript *[]string
}

func (v *phaseView) Render(w *gpui.Window, cx *gpui.Context[phaseView]) gpui.AnyElement {
	transcript := v.transcript
	return gpui.Div().
		KeyContext("Root").
		CaptureKeyDown(func(e *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
			*transcript = append(*transcript, "capture:root")
		}).
		OnKeyDown(func(e *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
			*transcript = append(*transcript, "bubble:root")
		}).
		Child(gpui.Div().
			KeyContext("Keys").
			TrackFocus(v.focus).
			CaptureKeyDown(func(e *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
				*transcript = append(*transcript, "capture:child")
				if e.Keystroke.Key == "s" {
					*transcript = append(*transcript, "stop")
					app.StopPropagation()
				}
			}).
			OnKeyDown(func(e *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
				*transcript = append(*transcript, "bubble:child")
			})).
		IntoElement()
}

// TestChordDispatchAndTimeoutReplay checks multi-stroke bindings: the
// first keystroke pends (with the pending-input observers), the second
// completes the action, and an abandoned chord replays through the key
// listeners and the input handler after the timeout flush.
func TestChordDispatchAndTimeoutReplay(t *testing.T) {
	if testing.Short() {
		t.Skip("chords draw through the native layout services")
	}
	k := newKeyCorpus(t,
		gpui.MustKeyBinding("g g", incrementAction.Box(increment{}), "Keys"),
	)
	input := &recordingInput{accepts: true}
	k.window.SetInputHandler(input)

	var pendingChanges int
	k.window.OnPendingInputChange(func(w *gpui.Window, app *gpui.App) { pendingChanges++ })

	// First keystroke of the chord pends; nothing dispatches and the
	// keystroke is consumed (the reference sets propagate_event=false
	// when input pends, so the prefix cannot also insert text).
	if propagate := k.dispatch("g"); propagate {
		t.Fatal("chord prefix was not consumed")
	}
	requireTranscript(t, k.recorded())
	if !k.window.HasPendingKeystrokes() {
		t.Fatal("no pending keystrokes after the chord prefix")
	}
	if got := k.window.PendingInputKeystrokes(); len(got) != 1 || got[0].Key != "g" {
		t.Fatalf("pending keystrokes = %v", got)
	}
	if pendingChanges == 0 {
		t.Fatal("pending-input observers were not notified")
	}

	// The second keystroke completes the action.
	if propagate := k.dispatch("g"); propagate {
		t.Fatal("completed chord propagated")
	}
	requireTranscript(t, k.recorded(), "increment")
	if k.window.HasPendingKeystrokes() {
		t.Fatal("pending keystrokes survive chord completion")
	}

	// An abandoned chord: prefix pends, then the timeout flush replays
	// it through the key listeners and the input handler.
	pendingChanges = 0
	k.dispatch("g->g")
	requireTranscript(t, k.recorded(), "increment")
	if !k.window.HasPendingKeystrokes() {
		t.Fatal("abandoned chord prefix did not pend")
	}
	k.ta.AdvanceClock(1100)
	k.ta.RunUntilParked()

	requireTranscript(t, k.recorded(), "increment", "keys-keydown:g", "root-keydown:g")
	if input.dispatched != "g" {
		t.Fatalf("replayed input = %q, want g", input.dispatched)
	}
	if k.window.HasPendingKeystrokes() {
		t.Fatal("pending keystrokes survive the timeout flush")
	}
	if pendingChanges == 0 {
		t.Fatal("pending-input observers were not notified by the flush")
	}
}

// recordingInput records the text-input deliveries.
type recordingInput struct {
	accepts    bool
	dispatched string
	replaced   []string
}

func (r *recordingInput) AcceptsTextInput(w *gpui.Window, app *gpui.App) bool {
	return r.accepts
}

func (r *recordingInput) ReplaceTextInRange(rng *gpui.UTF16Range, text string, w *gpui.Window, app *gpui.App) {
	r.replaced = append(r.replaced, text)
}

func (r *recordingInput) DispatchInput(input string, w *gpui.Window, app *gpui.App) {
	r.dispatched = input
}

// TestKeymapJSONLoading checks the keymap JSON surface: entries with
// contexts resolve through the app's BuildAction registry, and a null
// binding disables.
func TestKeymapJSONLoading(t *testing.T) {
	if testing.Short() {
		t.Skip("keymap JSON draws through the native layout services")
	}
	k := newKeyCorpus(t)
	k.app.RegisterActions(incrementAction)

	err := k.app.LoadKeymapJSON([]byte(`[
		{"context": "Keys", "bindings": [["ctrl-j", "focusspec::Increment"]]},
		{"context": "Keys", "bindings": [["ctrl-k", null]]}
	]`))
	if err != nil {
		t.Fatalf("LoadKeymapJSON: %v", err)
	}

	// The JSON binding dispatches through the focus tree (a new frame
	// binds the keymap).
	k.drawAgain(t)
	if propagate := k.dispatch("ctrl-j"); propagate {
		t.Fatal("JSON binding was not consumed")
	}
	requireTranscript(t, k.recorded(), "increment")

	// A null action disables the keystroke.
	k.app.BindKeys(gpui.MustKeyBinding("ctrl-l", incrementAction.Box(increment{}), "Keys"))
	k.drawAgain(t)
	k.app.LoadKeymapJSON([]byte(`[
		{"context": "Keys", "bindings": [["ctrl-l", null]]}
	]`))
	k.drawAgain(t)
	if propagate := k.dispatch("ctrl-l"); !propagate {
		t.Fatal("disabled keystroke was consumed")
	}
	// The disabled keystroke reaches no action; it falls through to the
	// key listeners.
	requireTranscript(t, k.recorded(), "increment", "keys-keydown:l", "root-keydown:l")
}

// TestKeystrokeObserversAndInterceptors checks the public keystroke
// observers: observers see the keystroke and the dispatched action;
// interceptors can consume the event before dispatch.
func TestKeystrokeObserversAndInterceptors(t *testing.T) {
	if testing.Short() {
		t.Skip("observers draw through the native layout services")
	}
	k := newKeyCorpus(t, gpui.MustKeyBinding("ctrl-i", incrementAction.Box(increment{}), "Keys"))

	var observed []string
	k.app.OnKeystroke(func(event *gpui.KeystrokeEvent, w *gpui.Window, app *gpui.App) {
		name := ""
		if event.Action.Name() != "" {
			name = event.Action.Name()
		}
		observed = append(observed, event.Keystroke.Key+":"+name)
	})

	k.dispatch("ctrl-i")
	requireTranscript(t, k.recorded(), "increment")
	if len(observed) != 1 || observed[0] != "i:focusspec::Increment" {
		t.Fatalf("observed = %v, want [i:focusspec::Increment]", observed)
	}

	// The interceptor consumes before dispatch: no action, no key
	// listeners and no further observer event (the finish path returns
	// before the observers when propagation is stopped).
	k.app.OnKeystrokeInterceptor(func(event *gpui.KeystrokeEvent, w *gpui.Window, app *gpui.App) {
		if event.Keystroke.Key == "i" {
			app.StopPropagation()
		}
	})
	k.dispatch("ctrl-i")
	requireTranscript(t, k.recorded(), "increment") // no second dispatch
	if len(observed) != 1 {
		t.Fatalf("observed = %v, want exactly the first dispatch's event", observed)
	}
}

// TestFocusTransferTwoWindows checks two-window focus isolation: each
// window owns its dispatch tree and focus state; a key dispatched to
// one window never reaches the other, and focus changes in one do not
// disturb the other.
func TestFocusTransferTwoWindows(t *testing.T) {
	if testing.Short() {
		t.Skip("two windows draw through the native layout services")
	}
	k1 := newKeyCorpus(t, gpui.MustKeyBinding("ctrl-i", incrementAction.Box(increment{}), "Keys"))
	k2 := newKeyCorpus(t, gpui.MustKeyBinding("ctrl-i", incrementAction.Box(increment{}), "Keys"))

	// Both windows hold their own focus.
	if !k1.focus.IsFocused(k1.window) || !k2.focus.IsFocused(k2.window) {
		t.Fatal("windows do not hold their own focus")
	}

	// A key in window two dispatches only there.
	if propagate := k2.dispatch("ctrl-i"); propagate {
		t.Fatal("window two's keystroke was not consumed")
	}
	requireTranscript(t, k2.recorded(), "increment")
	requireTranscript(t, k1.recorded())

	// Focus blur in window one leaves window two focused.
	k1.window.Blur()
	if !k2.focus.IsFocused(k2.window) {
		t.Fatal("window two lost focus when window one blurred")
	}
	if propagate := k2.dispatch("x"); !propagate {
		t.Fatal("window two's unbound key was consumed")
	}
	requireTranscript(t, k2.recorded(), "increment", "keys-keydown:x", "root-keydown:x")
}

// TestFocusReleaseDuringDispatch checks dispatch survives a listener
// releasing the focused handle mid-dispatch: the dispatch completes,
// focus reports none, and the next dispatch routes to the root path.
func TestFocusReleaseDuringDispatch(t *testing.T) {
	if testing.Short() {
		t.Skip("dispatch draws through the native layout services")
	}
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 200})
	focus := gpui.NewFocusHandle(window)
	var transcript []string

	window.SetRootView(gpui.ViewOf(gpui.NewEntity(app, window.Scope(), func(v *releaseView, cx *gpui.Context[releaseView]) {
		v.focus = focus
		v.transcript = &transcript
	})))
	window.Focus(focus)
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}

	window.DispatchKeyEvent(&gpui.KeyDownEvent{
		Keystroke: gpui.MustParseKeystroke("r"),
	}, app)
	requireTranscript(t, transcript, "release", "root-keydown:r")
	if window.Focused().Ok {
		t.Fatal("focus survived the in-dispatch release")
	}

	// The next dispatch routes to the window root node only (no element
	// holds focus): no element listeners observe it, exactly like the
	// reference's root-only dispatch path.
	window.DispatchKeyEvent(&gpui.KeyDownEvent{
		Keystroke: gpui.MustParseKeystroke("t"),
	}, app)
	requireTranscript(t, transcript, "release", "root-keydown:r")
}

// releaseView releases its own focus handle during its key listener.
type releaseView struct {
	focus      gpui.FocusHandle
	transcript *[]string
}

func (v *releaseView) Render(w *gpui.Window, cx *gpui.Context[releaseView]) gpui.AnyElement {
	transcript := v.transcript
	focus := v.focus
	return gpui.Div().
		KeyContext("Root").
		OnKeyDown(func(e *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
			*transcript = append(*transcript, "root-keydown:"+e.Keystroke.Key)
		}).
		Child(gpui.Div().
			KeyContext("Keys").
			TrackFocus(focus).
			OnKeyDown(func(e *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
				*transcript = append(*transcript, "release")
				focus.Release()
			})).
		IntoElement()
}

// TestTabTraversalOrder checks focus_next/focus_prev: document order,
// tab indices, group nesting, non-tab-stop skipping and wraparound.
func TestTabTraversalOrder(t *testing.T) {
	if testing.Short() {
		t.Skip("tab traversal draws through the native layout services")
	}
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 200})

	a := gpui.NewFocusHandle(window).TabStop(true)
	b := gpui.NewFocusHandle(window).TabStop(true)
	// c is focusable but not a tab stop: traversal skips it.
	c := gpui.NewFocusHandle(window).TabStop(false)
	// d sorts before a through a negative tab index.
	d := gpui.NewFocusHandle(window).TabStop(true).TabIndex(-1)
	// e and f live inside their own group, after everything else.
	e := gpui.NewFocusHandle(window).TabStop(true)
	f := gpui.NewFocusHandle(window).TabStop(true)

	window.SetRootView(gpui.ViewOf(gpui.NewEntity(app, window.Scope(), func(v *tabView, cx *gpui.Context[tabView]) {
		v.a, v.b, v.c, v.d, v.e, v.f = a, b, c, d, e, f
	})))
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}

	// From nothing: the first tab stop in order is d (index -1 sorts
	// first), then a, b (c skipped), then e, f.
	window.FocusNext()
	if got := window.Focused(); !got.Ok || got.Handle.ID() != d.ID() {
		t.Fatalf("first focus-next = %+v, want d", got)
	}
	window.FocusNext()
	if got := window.Focused(); !got.Ok || got.Handle.ID() != a.ID() {
		t.Fatalf("second focus-next = %+v, want a", got)
	}
	// From a: b (c is skipped).
	window.FocusNext()
	if got := window.Focused(); !got.Ok || got.Handle.ID() != b.ID() {
		t.Fatalf("third focus-next = %+v, want b (c skipped)", got)
	}
	// The full cycle continues b -> e (c skipped) -> f -> wrap to d.
	window.FocusNext()
	if got := window.Focused(); !got.Ok || got.Handle.ID() != e.ID() {
		t.Fatalf("fourth focus-next = %+v, want e", got)
	}
	window.FocusNext()
	if got := window.Focused(); !got.Ok || got.Handle.ID() != f.ID() {
		t.Fatalf("fifth focus-next = %+v, want f", got)
	}
	window.FocusNext()
	if got := window.Focused(); !got.Ok || got.Handle.ID() != d.ID() {
		t.Fatalf("wrapped focus-next = %+v, want d", got)
	}
	// Backwards from d wraps to the last stop (f).
	window.FocusPrev()
	if got := window.Focused(); !got.Ok || got.Handle.ID() != f.ID() {
		t.Fatalf("wrapped focus-prev = %+v, want f", got)
	}
	// e is f's predecessor.
	window.FocusPrev()
	if got := window.Focused(); !got.Ok || got.Handle.ID() != e.ID() {
		t.Fatalf("focus-prev from f = %+v, want e", got)
	}
	// Focusing the non-tab-stop c and traversing lands on the next
	// stop after it.
	window.Focus(c)
	window.FocusNext()
	if got := window.Focused(); !got.Ok || got.Handle.ID() != e.ID() {
		t.Fatalf("focus-next from the non-stop c = %+v, want e", got)
	}
}

func focusedID(w *gpui.Window) *uint64 {
	got := w.Focused()
	if !got.Ok {
		return nil
	}
	id := got.Handle.ID()
	return &id
}

// tabView renders the tab corpus: d, a, b, c in order, then a tab group
// containing e and f.
type tabView struct {
	a, b, c, d, e, f gpui.FocusHandle
}

func (v *tabView) Render(w *gpui.Window, cx *gpui.Context[tabView]) gpui.AnyElement {
	return gpui.Div().
		KeyContext("Root").
		Child(gpui.Div().KeyContext("Row").TrackFocus(v.d)).
		Child(gpui.Div().KeyContext("Row").TrackFocus(v.a)).
		Child(gpui.Div().KeyContext("Row").TrackFocus(v.b)).
		Child(gpui.Div().KeyContext("Row").TrackFocus(v.c)).
		Child(gpui.Div().KeyContext("Group").TabGroup().
			Child(gpui.Div().TrackFocus(v.e)).
			Child(gpui.Div().TrackFocus(v.f))).
		IntoElement()
}
