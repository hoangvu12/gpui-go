package gpui

// Ticket18's tests (browser-core, .scratch/browser-core/issues/
// 18-ime-composition-events.md): the IME composition events on the
// input-handler seam and the IsComposing flag on key events.
//
//   - The composition callbacks fire in Chromium's order around the
//     text mutations (core/editing/ime/input_method_controller.cc):
//     compositionstart + compositionupdate before the first mark
//     mutation (SetComposition's start arm :1020-1022 +
//     InsertTextDuringCompositionWithEvents :391), compositionupdate
//     before every later mutation, and compositionend after the
//     commit's or cancel's mutation (ReplaceComposition :728 — "No DOM
//     update after 'compositionend'" — and CancelComposition :932).
//     The event order was verified against headless Chrome through
//     CDP Input.imeSetComposition / Input.insertText (the
//     imecompose parity fixture's record).
//   - The start's data is the selection the composition replaces
//     (DispatchCompositionStartEvent :935-948 passes
//     GetFrame().SelectedText()); the update's data is the new
//     composition string; the end's data is the committed text, or
//     the empty string for a cancel.
//   - KeyDownEvents and KeyUpEvents carry the DOM's isComposing
//     (core/events/keyboard_event.cc:100, :122 — HasCurrentComposition
//     at event construction), stamped at dispatch time, and keydowns
//     during an active composition are NOT dropped: they reach the
//     element key listeners flagged (the spec's "keydowns dropped
//     during IME" risk, proven here).

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// The composition-event recording handler (the fake seam, ime_test.go)
// ---------------------------------------------------------------------------

// compositionLog interleaves the composition callbacks and the
// handler's text mutations in one transcript, so the ORDER of events
// around the mutations is the thing the tests read.
type compositionLog struct {
	entries []string
}

func (l *compositionLog) add(entry string) { l.entries = append(l.entries, entry) }

// compositionEditor is an Editor with the ticket's optional
// IMECompositionHandler surface: the callbacks append to the shared
// transcript, the Editor members mutate the model (the app pattern —
// a handler that wants the events wraps the editor).
type compositionEditor struct {
	*Editor
	log *compositionLog
}

func newCompositionEditor(text string, caret int) *compositionEditor {
	return &compositionEditor{
		Editor: NewEditor(text, caret, EditorOptions{
			Shape: func(string) (*WrappedLine, error) { return nil, errNoEditorLayout },
		}),
		log: &compositionLog{},
	}
}

func (e *compositionEditor) CompositionStart(data string, w *Window, app *App) {
	e.log.add("compositionstart:" + data)
}

func (e *compositionEditor) CompositionUpdate(data string, w *Window, app *App) {
	e.log.add("compositionupdate:" + data)
}

func (e *compositionEditor) CompositionEnd(data string, w *Window, app *App) {
	e.log.add("compositionend:" + data)
}

func (e *compositionEditor) ReplaceTextInRange(rng *UTF16Range, text string, w *Window, app *App) {
	e.log.add("replace:" + text)
	e.Editor.ReplaceTextInRange(rng, text, w, app)
}

func (e *compositionEditor) ReplaceAndMarkTextInRange(rng *UTF16Range, text string, newSelected *UTF16Range, w *Window, app *App) {
	e.log.add("mark:" + text)
	e.Editor.ReplaceAndMarkTextInRange(rng, text, newSelected, w, app)
}

// wantTranscript asserts the exact interleaved transcript.
func wantIMETranscript(t *testing.T, got []string, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("transcript =\n\t%s\nwant\n\t%s", strings.Join(got, "\n\t"), strings.Join(want, "\n\t"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("transcript[%d] = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

// ---------------------------------------------------------------------------
// The composition-event order (input_method_controller.cc)
// ---------------------------------------------------------------------------

// TestIMECompositionEventOrder drives a whole session through the
// composition driver (the fake seam, ime_test.go's pattern): start
// (two updates), commit, then a second session that is cancelled by
// an empty composition string. Chromium's order — compositionstart,
// compositionupdate BEFORE each mutation, compositionend AFTER the
// commit/cancel mutation — is the transcript.
func TestIMECompositionEventOrder(t *testing.T) {
	e := newCompositionEditor("", 0)
	seam := &fakeIMESeam{handler: e}
	driver := &imeCompositionDriver{seam: seam, src: &fakeIMECompositionSource{comp: []uint16{'n', 'i'}, compOK: true}}

	// First update: the session starts (compositionstart, then
	// compositionupdate, then the mark mutation).
	if _, handled := driver.handleComposition(uintptr(GCSCompStr)); !handled {
		t.Fatal("the first composition update must be handled")
	}
	wantIMETranscript(t, e.log.entries, []string{
		"compositionstart:",
		"compositionupdate:ni",
		"mark:ni",
	})
	e.log.entries = nil

	// Update to "nihao": one compositionupdate before the mark.
	driver.src = &fakeIMECompositionSource{comp: []uint16{'n', 'i', 'h', 'a', 'o'}, compOK: true}
	if _, handled := driver.handleComposition(uintptr(GCSCompStr)); !handled {
		t.Fatal("the second composition update must be handled")
	}
	wantIMETranscript(t, e.log.entries, []string{
		"compositionupdate:nihao",
		"mark:nihao",
	})
	e.log.entries = nil

	// Commit: compositionupdate with the committed text before the
	// replacement, compositionend after it.
	driver.src = &fakeIMECompositionSource{result: []uint16{'n', 'i', 'h', 'a', 'o'}, resultOK: true}
	if _, handled := driver.handleComposition(uintptr(GCSResultStr)); !handled {
		t.Fatal("the commit must be handled")
	}
	wantIMETranscript(t, e.log.entries, []string{
		"compositionupdate:nihao",
		"replace:nihao",
		"compositionend:nihao",
	})
	if text := e.Text(); text != "nihao" {
		t.Fatalf("text after the commit = %q, want nihao", text)
	}
	if _, ok := e.MarkedTextRange(nil, nil); ok {
		t.Fatal("the commit must end the composition (no marked range)")
	}
	e.log.entries = nil

	// A second session: start + update, then the empty composition
	// string cancels it (compositionupdate("") before the mutation,
	// compositionend("") after it).
	driver.src = &fakeIMECompositionSource{comp: []uint16{'x'}, compOK: true}
	if _, handled := driver.handleComposition(uintptr(GCSCompStr)); !handled {
		t.Fatal("the second session's first update must be handled")
	}
	wantIMETranscript(t, e.log.entries, []string{
		"compositionstart:",
		"compositionupdate:x",
		"mark:x",
	})
	e.log.entries = nil
	driver.src = &fakeIMECompositionSource{compOK: true}
	if _, handled := driver.handleComposition(uintptr(GCSCompStr)); !handled {
		t.Fatal("the empty composition string must be handled")
	}
	wantIMETranscript(t, e.log.entries, []string{
		"compositionupdate:",
		"mark:",
		"compositionend:",
	})
	if text := e.Text(); text != "nihao" {
		t.Fatalf("text after the cancel = %q, want nihao (the composed x is removed)", text)
	}
}

// TestIMECompositionStartDataIsSelectedText pins the start's data
// (DispatchCompositionStartEvent passes GetFrame().SelectedText()):
// the text of the handler's selection at composition start, "" for an
// empty selection.
func TestIMECompositionStartDataIsSelectedText(t *testing.T) {
	e := newCompositionEditor("hello world", 5)
	// Select "wo" (UTF-16 6..8): compositionstart's data is "wo".
	e.setSelection(EditorSelectionFromRange(6, 8))
	seam := &fakeIMESeam{handler: e}
	driver := &imeCompositionDriver{seam: seam, src: &fakeIMECompositionSource{comp: []uint16{'n'}, compOK: true}}
	if _, handled := driver.handleComposition(uintptr(GCSCompStr)); !handled {
		t.Fatal("the composition must be handled")
	}
	wantIMETranscript(t, e.log.entries, []string{
		"compositionstart:wo",
		"compositionupdate:n",
		"mark:n",
	})
}

// TestIMECompositionCommitWithoutSessionHasNoEvents: a result string
// with no active composition replaces the text without composition
// events (Chromium's CommitText without a composition is InsertText,
// input_method_controller.cc:630-635 — the probe's
// insertText-outside-composition case: beforeinput/input only).
func TestIMECompositionCommitWithoutSessionHasNoEvents(t *testing.T) {
	e := newCompositionEditor("abc", 3)
	seam := &fakeIMESeam{handler: e}
	driver := &imeCompositionDriver{seam: seam, src: &fakeIMECompositionSource{result: []uint16{'x'}, resultOK: true}}
	if _, handled := driver.handleComposition(uintptr(GCSResultStr)); !handled {
		t.Fatal("the result string must be handled")
	}
	wantIMETranscript(t, e.log.entries, []string{"replace:x"})
	if text := e.Text(); text != "abcx" {
		t.Fatalf("text = %q, want abcx (the commit inserts at the caret, Chromium's InsertText arm)", text)
	}
}

// TestIMECompositionZeroLParamCancels: the Japanese zero-lParam
// message (GPUI-CE events.rs: the composition is replaced with empty
// text) cancels an active session — compositionupdate("") before the
// mutation, compositionend("") after it — and fires nothing when no
// session is active.
func TestIMECompositionZeroLParamCancels(t *testing.T) {
	e := newCompositionEditor("", 0)
	e.Editor.ReplaceAndMarkTextInRange(nil, "ni", nil, nil, nil)
	seam := &fakeIMESeam{handler: e}
	driver := &imeCompositionDriver{seam: seam, src: &fakeIMECompositionSource{}}
	if _, handled := driver.handleComposition(0); !handled {
		t.Fatal("the zero-lParam message must be handled")
	}
	wantIMETranscript(t, e.log.entries, []string{
		"compositionupdate:",
		"replace:",
		"compositionend:",
	})
	if text := e.Text(); text != "" {
		t.Fatalf("text after the zero-lParam cancel = %q, want empty", text)
	}

	// Without an active session: the replacement still runs (the
	// reference's behavior), but no composition events.
	e.log.entries = nil
	if _, handled := driver.handleComposition(0); !handled {
		t.Fatal("the zero-lParam message without a session must be handled")
	}
	wantIMETranscript(t, e.log.entries, []string{"replace:"})
}

// TestIMECompositionHandlerWithoutCallbacks: a handler that does not
// implement the optional IMECompositionHandler surface still receives
// the text mutations (the base InputHandler contract) — the events are
// skipped, nothing panics.
func TestIMECompositionHandlerWithoutCallbacks(t *testing.T) {
	e := newRecordingEditor("", 0)
	seam := &fakeIMESeam{handler: e}
	driver := &imeCompositionDriver{seam: seam, src: &fakeIMECompositionSource{comp: []uint16{'n'}, compOK: true}}
	if _, handled := driver.handleComposition(uintptr(GCSCompStr)); !handled {
		t.Fatal("the composition must be handled")
	}
	wantCalls := []string{"mark:nil=n:sel=nil"}
	if len(e.calls) != len(wantCalls) || e.calls[0] != wantCalls[0] {
		t.Fatalf("calls = %v, want %v", e.calls, wantCalls)
	}
}

// ---------------------------------------------------------------------------
// IsComposing on key events (keyboard_event.cc:122)
// ---------------------------------------------------------------------------

// imeWindowTest is one test's window: a focused element with key
// listeners over an input handler that holds the composition state.
// The handler registers through the paint-time seam (Window.HandleInput
// for the field's focus handle, the focused element's registration the
// frame completion installs as the window's active input handler) —
// the production pattern, so the handler survives every redraw.
type imeWindowTest struct {
	*testing.T
	ta     *TestApp
	app    *App
	window *Window
	editor *compositionEditor
	focus  FocusHandle

	keyLog []string
}

func newIMEWindowTest(t *testing.T) *imeWindowTest {
	ta := NewTestApp()
	f := &imeWindowTest{
		T:     t,
		ta:    ta,
		app:   ta.App(),
		focus: ta.App().FocusHandle().TabStop(true),
	}
	f.editor = newCompositionEditor("", 0)
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			w.HandleInput(f.focus, f.editor)
			return Div().ID("root").SizeFull().
				OnKeyDown(func(e *KeyDownEvent, w *Window, app *App) {
					f.keyLog = append(f.keyLog, "keydown:root isComposing="+boolString(e.IsComposing))
				}).
				OnKeyUp(func(e *KeyUpEvent, w *Window, app *App) {
					f.keyLog = append(f.keyLog, "keyup:root isComposing="+boolString(e.IsComposing))
				}).
				Child(Div().ID("field").Size(Px(200), Px(40)).TrackFocus(f.focus).
					OnKeyDown(func(e *KeyDownEvent, w *Window, app *App) {
						f.keyLog = append(f.keyLog, "keydown:field isComposing="+boolString(e.IsComposing))
					}).
					OnKeyUp(func(e *KeyUpEvent, w *Window, app *App) {
						f.keyLog = append(f.keyLog, "keyup:field isComposing="+boolString(e.IsComposing))
					})).
				IntoElement()
		}
	})
	f.window = cx.Window()
	f.ta.RunUntilParked()
	return f
}

func boolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// press dispatches one full key press through the platform dispatch.
func (f *imeWindowTest) press(source string) {
	f.Helper()
	stroke := MustParseKeystroke(source)
	f.window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, f.app)
	f.window.DispatchKeyEvent(&KeyUpEvent{Keystroke: stroke}, f.app)
}

// startComposition drives one simulated composition update (the
// SimulateIMEComposition seam).
func (f *imeWindowTest) startComposition(text string) {
	f.Helper()
	attrs := make([]byte, len([]rune(text)))
	f.window.SimulateIMEComposition(IMECompositionMessage{
		Flags:       GCSCompStr | GCSCursorPos,
		Composition: text,
		Cursor:      len([]rune(text)),
		Attrs:       attrs,
	}, f.ta)
}

// commitComposition drives one simulated commit.
func (f *imeWindowTest) commitComposition(text string) {
	f.Helper()
	f.window.SimulateIMEComposition(IMECompositionMessage{
		Flags:  GCSResultStr,
		Result: text,
	}, f.ta)
}

// focus focuses the field.
func (f *imeWindowTest) focusField() {
	f.Helper()
	f.app.Update(func(app *App) { f.window.Focus(f.focus) })
	f.ta.RunUntilParked()
}

// TestKeyEventsDuringCompositionAreFlaggedNotDropped is the ticket's
// first acceptance item (the spec's "keydowns dropped during IME"
// risk): with an active composition, keydowns and keyups still reach
// the element key listeners — carrying IsComposing=true — and after
// the commit ends the session, keys arrive flagged false.
func TestKeyEventsDuringCompositionAreFlaggedNotDropped(t *testing.T) {
	f := newIMEWindowTest(t)
	f.focusField()
	if handler := f.window.ActiveInputHandler(); handler == nil {
		t.Fatal("the focused field's HandleInput registration must install the window's active input handler")
	}

	f.press("enter")
	wantIMETranscript(t, f.keyLog, []string{
		"keydown:field isComposing=false",
		"keydown:root isComposing=false",
		"keyup:field isComposing=false",
		"keyup:root isComposing=false",
	})
	f.keyLog = nil

	// The composition session starts: "ni" is marked.
	f.startComposition("ni")
	if _, ok := f.editor.MarkedTextRange(nil, nil); !ok {
		t.Fatal("the simulated composition did not mark the editor's text")
	}

	// Keys during the composition are NOT dropped: both listeners see
	// them, flagged isComposing=true.
	f.press("enter")
	wantIMETranscript(t, f.keyLog, []string{
		"keydown:field isComposing=true",
		"keydown:root isComposing=true",
		"keyup:field isComposing=true",
		"keyup:root isComposing=true",
	})
	f.keyLog = nil

	// The commit ends the session (compositionend fires, the marked
	// range clears) and the next keys are flagged false.
	f.commitComposition("nihao")
	if text := f.editor.Text(); text != "nihao" {
		t.Fatalf("text after the commit = %q, want nihao", text)
	}
	if _, ok := f.editor.MarkedTextRange(nil, nil); ok {
		t.Fatal("the commit must end the composition")
	}
	wantIMETranscript(t, f.editor.log.entries, []string{
		"compositionstart:",
		"compositionupdate:ni",
		"mark:ni",
		"compositionupdate:nihao",
		"replace:nihao",
		"compositionend:nihao",
	})

	f.press("a")
	wantIMETranscript(t, f.keyLog, []string{
		"keydown:field isComposing=false",
		"keydown:root isComposing=false",
		"keyup:field isComposing=false",
		"keyup:root isComposing=false",
	})
}

// TestKeyEventsWithPlainInputHandlerAreNeverComposing: a window whose
// active handler does not implement the IME extension has no
// composition state, so keys are never flagged.
func TestKeyEventsWithPlainInputHandlerAreNeverComposing(t *testing.T) {
	ta := NewTestApp()
	focus := ta.App().FocusHandle().TabStop(true)
	var keyLog []string
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			w.HandleInput(focus, &plainIMEWindowHandler{})
			return Div().ID("root").SizeFull().
				OnKeyDown(func(e *KeyDownEvent, w *Window, app *App) {
					keyLog = append(keyLog, "keydown isComposing="+boolString(e.IsComposing))
				}).
				OnKeyUp(func(e *KeyUpEvent, w *Window, app *App) {
					keyLog = append(keyLog, "keyup isComposing="+boolString(e.IsComposing))
				}).
				Child(Div().ID("field").Size(Px(200), Px(40)).TrackFocus(focus)).
				IntoElement()
		}
	})
	window := cx.Window()
	ta.RunUntilParked()
	ta.App().Update(func(app *App) { window.Focus(focus) })
	ta.RunUntilParked()
	stroke := MustParseKeystroke("space")
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: stroke}, ta.App())
	window.DispatchKeyEvent(&KeyUpEvent{Keystroke: stroke}, ta.App())
	for _, entry := range keyLog {
		if strings.HasSuffix(entry, "isComposing=true") {
			t.Fatalf("a plain InputHandler has no composition state: %v", keyLog)
		}
	}
	if len(keyLog) != 2 {
		t.Fatalf("key log = %v, want two entries", keyLog)
	}
}

// TestSimulateIMECompositionWithoutHandlerFallsThrough: with no input
// handler, the simulated composition message is not handled (the
// reference's with_input_handler None: DefWindowProc).
func TestSimulateIMECompositionWithoutHandlerFallsThrough(t *testing.T) {
	f := newIMEWindowTest(t)
	if f.window.SimulateIMEComposition(IMECompositionMessage{
		Flags:       GCSCompStr | GCSCursorPos,
		Composition: "ni",
		Cursor:      2,
		Attrs:       []byte{ATTRInput, ATTRInput},
	}, f.ta) {
		t.Fatal("a window with no input handler must not handle the composition")
	}
}

// TestSimulateIMECompositionRequiresIMEHandler: a plain InputHandler
// (no IME extension) is invisible to the composition seam.
func TestSimulateIMECompositionRequiresIMEHandler(t *testing.T) {
	f := newIMEWindowTest(t)
	// Install the plain handler directly (the platform seam SetInputHandler:
	// the paint registration is absent for this one test), inside an
	// update like the host thread would.
	f.app.Update(func(app *App) { f.window.SetInputHandler(&plainIMEWindowHandler{}) })
	if f.window.SimulateIMEComposition(IMECompositionMessage{
		Flags:       GCSCompStr | GCSCursorPos,
		Composition: "ni",
		Cursor:      2,
		Attrs:       []byte{ATTRInput, ATTRInput},
	}, f.ta) {
		t.Fatal("a plain InputHandler must not handle the composition")
	}
}

// plainIMEWindowHandler implements only the keyboard-path
// InputHandler (imespec's plainOnlyHandler shape).
type plainIMEWindowHandler struct{}

func (h *plainIMEWindowHandler) AcceptsTextInput(w *Window, app *App) bool { return true }
func (h *plainIMEWindowHandler) ReplaceTextInRange(rng *UTF16Range, text string, w *Window, app *App) {
}
func (h *plainIMEWindowHandler) DispatchInput(input string, w *Window, app *App) {}
