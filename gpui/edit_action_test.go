package gpui

import (
	"strings"
	"testing"
)

// Ticket 26's edit-action gates: the key table's mapping, the
// event-and-mutation flows on a focused input handler, the composition
// and accept-input gates, and the prevented-keydown suppression. The
// real-clipboard halves (the copy/cut write, the paste read) are pinned
// by internal/clipspec against the real system clipboard.

// fakeEditHandler is the test input handler: a plain text model over
// one string and a selection, implementing the base, IME and
// edit-action surfaces, recording the DOM events the core's edit
// actions fire.
type fakeEditHandler struct {
	text        string
	sel         UTF16Range
	events      []string
	prevent     func(event string) bool
	acceptsText bool
	// marked simulates an active IME composition (MarkedTextRange's
	// answer drives the key events' isComposing stamp).
	marked bool
}

func (h *fakeEditHandler) AcceptsTextInput(*Window, *App) bool { return h.acceptsText }

func (h *fakeEditHandler) DispatchInput(input string, w *Window, app *App) {
	h.ReplaceTextInRange(nil, input, w, app)
}

func (h *fakeEditHandler) ReplaceTextInRange(rng *UTF16Range, text string, w *Window, app *App) {
	start, end := 0, UTF8ToUTF16Offset(h.text, len(h.text))
	if rng != nil {
		start, end = rng.Start, rng.End
	}
	start = min(start, end)
	rng8 := UTF16Range{
		Start: UTF16ToUTF8Offset(h.text, start),
		End:   UTF16ToUTF8Offset(h.text, end),
	}
	h.text = h.text[:rng8.Start] + text + h.text[rng8.End:]
	// The selection collapses to the edit's end.
	caret := start + UTF8ToUTF16Offset(text, len(text))
	h.sel = UTF16Range{Start: caret, End: caret}
}

func (h *fakeEditHandler) SelectedTextRange(bool, *Window, *App) (UTF16Selection, bool) {
	return UTF16Selection{Range: h.sel}, true
}

func (h *fakeEditHandler) MarkedTextRange(*Window, *App) (UTF16Range, bool) {
	if !h.marked {
		return UTF16Range{}, false
	}
	return h.sel, true
}

func (h *fakeEditHandler) TextForRange(rng UTF16Range, w *Window, app *App) (string, *UTF16Range, bool) {
	start, end := min(rng.Start, rng.End), max(rng.Start, rng.End)
	n := UTF8ToUTF16Offset(h.text, len(h.text))
	start, end = min(start, n), min(end, n)
	adjusted := UTF16Range{Start: start, End: end}
	rng8 := UTF16Range{
		Start: UTF16ToUTF8Offset(h.text, start),
		End:   UTF16ToUTF8Offset(h.text, end),
	}
	return h.text[rng8.Start:rng8.End], &adjusted, true
}

func (h *fakeEditHandler) ReplaceAndMarkTextInRange(rng *UTF16Range, text string, newSelectedRange *UTF16Range, w *Window, app *App) {
	h.ReplaceTextInRange(rng, text, w, app)
}

func (h *fakeEditHandler) UnmarkText(*Window, *App) {}

func (h *fakeEditHandler) BoundsForRange(UTF16Range, *Window, *App) (Bounds, bool) {
	return Bounds{}, false
}

func (h *fakeEditHandler) CharacterIndexForPoint(Point, *Window, *App) (int, bool) {
	return 0, false
}

func (h *fakeEditHandler) SetSelectedTextRange(UTF16Range, *Window, *App) {}

func (h *fakeEditHandler) TextLengthUTF16(*Window, *App) (int, bool) {
	return UTF8ToUTF16Offset(h.text, len(h.text)), true
}

func (h *fakeEditHandler) Copy(w *Window, app *App) bool {
	h.events = append(h.events, "copy")
	return h.prevent != nil && h.prevent("copy")
}

func (h *fakeEditHandler) Cut(w *Window, app *App) bool {
	h.events = append(h.events, "cut")
	return h.prevent != nil && h.prevent("cut")
}

func (h *fakeEditHandler) Paste(item *ClipboardItem, w *Window, app *App) bool {
	text := ""
	if item != nil {
		if t, ok := item.Text(); ok {
			text = t
		}
	}
	h.events = append(h.events, "paste:"+text)
	return h.prevent != nil && h.prevent("paste")
}

func (h *fakeEditHandler) BeforeInput(inputType, data string, w *Window, app *App) bool {
	h.events = append(h.events, "beforeinput:"+inputType+":"+data)
	return h.prevent != nil && h.prevent("beforeinput")
}

func (h *fakeEditHandler) Select(w *Window, app *App) {
	h.events = append(h.events, "select")
}

func (h *fakeEditHandler) SelectAll(w *Window, app *App) {
	h.events = append(h.events, "selectall")
	h.sel = UTF16Range{Start: 0, End: UTF8ToUTF16Offset(h.text, len(h.text))}
}

// newEditTestWindow opens a test window whose view registers the
// handler through the paint-time seam (Window.HandleInput for the
// focused handle — the focused element's registration the frame
// completion installs, the same seam the parity fixture's view uses).
func newEditTestWindow(t *testing.T, handler InputHandler) (*TestApp, *Window) {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	focus := app.FocusHandle().TabStop(true)
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			w.HandleInput(focus, handler)
			return Div().Flex().ID("root").SizeFull().TrackFocus(focus).IntoElement()
		}
	})
	window := cx.Window()
	app.Update(func(*App) { window.Focus(focus) })
	cx.RunUntilParked()
	return ta, window
}

// pressKey dispatches one full key press through the window.
func pressKey(t *testing.T, window *Window, app *App, keystroke string) {
	t.Helper()
	key := MustParseKeystroke(keystroke)
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: key}, app)
	window.DispatchKeyEvent(&KeyUpEvent{Keystroke: key}, app)
}

// TestEditCommandKeyMapping pins the Windows rows of Chromium's
// editing key table (editing_behavior.cc:168-170 and 174-178) and
// InterpretKeyEvent's exact-modifier match (:288-302): the seven
// bindings map, and near-misses (an extra modifier, an unmodified
// letter, the wrong key) map nothing.
func TestEditCommandKeyMapping(t *testing.T) {
	cases := []struct {
		keystroke string
		want      editCommandKind
	}{
		{"ctrl-c", editCommandCopy},
		{"ctrl-x", editCommandCut},
		{"ctrl-v", editCommandPaste},
		{"ctrl-a", editCommandSelectAll},
		{"shift-delete", editCommandCut},
		{"ctrl-insert", editCommandCopy},
		{"shift-insert", editCommandPaste},
		// Exact-modifier misses.
		{"ctrl-shift-c", editCommandNone},
		{"ctrl-alt-c", editCommandNone},
		{"c", editCommandNone},
		{"shift-c", editCommandNone},
		{"ctrl-shift-insert", editCommandNone},
		{"ctrl-delete", editCommandNone},
		{"shift-a", editCommandNone},
		{"ctrl-b", editCommandNone},
		{"delete", editCommandNone},
		{"insert", editCommandNone},
	}
	for _, tc := range cases {
		key, err := ParseKeystroke(tc.keystroke)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.keystroke, err)
		}
		if got := editCommandOfKeystroke(key); got != tc.want {
			t.Errorf("editCommandOfKeystroke(%q) = %s, want %s", tc.keystroke, got, tc.want)
		}
	}
}

// TestEditActionsFireEventsAndMutate drives the copy, cut, paste and
// select-all commands through real key dispatches on a focused input
// handler, pinning Chromium's DOM event order (clipboard_commands.cc:
// copy → write; cut → write → beforeinput(deleteByCut) → delete; paste
// → read → beforeinput(insertFromPaste) → insert; select-all →
// select). The clipboard exchange itself fails on the hostless test
// application (ErrNoHost), so the copy and cut writes are skipped and
// the paste reads an empty item — the flows' events and mutations are
// what this gate pins; internal/clipspec pins the real exchanges.
func TestEditActionsFireEventsAndMutate(t *testing.T) {
	handler := &fakeEditHandler{text: "hello world", acceptsText: true}
	ta, window := newEditTestWindow(t, handler)
	app := ta.App()

	// Select all: the command moves the selection and fires select.
	pressKey(t, window, app, "ctrl-a")
	wantEditStrings(t, "select-all", handler.events, []string{"selectall", "select"})
	if handler.sel != (UTF16Range{Start: 0, End: 11}) {
		t.Fatalf("selection after ctrl-a = %+v, want 0..11", handler.sel)
	}

	// Copy: the copy event fires (the write is skipped hostless).
	handler.events = nil
	pressKey(t, window, app, "ctrl-c")
	wantEditStrings(t, "copy", handler.events, []string{"copy"})

	// Cut: the cut event, the beforeinput preview, the deletion.
	handler.events = nil
	pressKey(t, window, app, "ctrl-x")
	wantEditStrings(t, "cut", handler.events, []string{"cut", "beforeinput:deleteByCut:"})
	if handler.text != "" {
		t.Fatalf("text after cut = %q, want empty", handler.text)
	}

	// Paste: the paste event (empty item — the hostless read fails),
	// the beforeinput preview, no insertion.
	handler.events = nil
	pressKey(t, window, app, "ctrl-v")
	wantEditStrings(t, "paste", handler.events, []string{"paste:", "beforeinput:insertFromPaste:"})
	if handler.text != "" {
		t.Fatalf("text after hostless paste = %q, want empty", handler.text)
	}
}

// TestEditActionsWindowsAlternateKeys pins the alternate bindings the
// parity fixture records (Ctrl+Insert is copy, Shift+Delete is cut,
// Shift+Insert is paste — editing_behavior.cc:168-170).
func TestEditActionsWindowsAlternateKeys(t *testing.T) {
	handler := &fakeEditHandler{text: "hi", sel: UTF16Range{Start: 0, End: 2}, acceptsText: true}
	ta, window := newEditTestWindow(t, handler)
	app := ta.App()

	handler.events = nil
	pressKey(t, window, app, "ctrl-insert")
	wantEditStrings(t, "alt copy", handler.events, []string{"copy"})

	handler.events = nil
	pressKey(t, window, app, "shift-delete")
	wantEditStrings(t, "alt cut", handler.events, []string{"cut", "beforeinput:deleteByCut:"})
	if handler.text != "" {
		t.Fatalf("text after shift-delete = %q, want empty", handler.text)
	}

	handler.events = nil
	pressKey(t, window, app, "shift-insert")
	wantEditStrings(t, "alt paste", handler.events, []string{"paste:", "beforeinput:insertFromPaste:"})
}

// TestEditActionsEmptySelectionStillFiresEvents pins the record's
// collapsed-caret shape: a copy or cut with no selection fires the
// clipboard event but does nothing else (no beforeinput, no mutation —
// CanDeleteRange's gate, clipboard_commands.cc:417-420's position).
func TestEditActionsEmptySelectionStillFiresEvents(t *testing.T) {
	handler := &fakeEditHandler{text: "hello", acceptsText: true}
	ta, window := newEditTestWindow(t, handler)
	app := ta.App()

	pressKey(t, window, app, "ctrl-c")
	wantEditStrings(t, "collapsed copy", handler.events, []string{"copy"})

	handler.events = nil
	pressKey(t, window, app, "ctrl-x")
	wantEditStrings(t, "collapsed cut", handler.events, []string{"cut"})
	if handler.text != "hello" {
		t.Fatalf("text after a collapsed cut = %q, want hello", handler.text)
	}
}

// TestEditActionsSkipDuringComposition pins the IME rule (the ticket
// 18 collaboration): a key event flagged isComposing runs no edit
// command — Chromium's composing keys are kVKeyProcessKey, which
// matches no row of the editing key table.
func TestEditActionsSkipDuringComposition(t *testing.T) {
	handler := &fakeEditHandler{text: "hello", acceptsText: true, marked: true}
	ta, window := newEditTestWindow(t, handler)
	app := ta.App()

	// The dispatch stamps the key events' isComposing from the handler's
	// marked range (keyboard_event.cc:122 HasCurrentComposition), so a
	// ctrl-c mid-composition arrives flagged and runs no editor command.
	key := MustParseKeystroke("ctrl-c")
	window.DispatchKeyEvent(&KeyDownEvent{Keystroke: key}, app)
	window.DispatchKeyEvent(&KeyUpEvent{Keystroke: key}, app)
	wantEditStrings(t, "composing", handler.events, nil)
}

// TestEditActionsRequireAcceptingHandler pins the CanEdit gate: a
// handler that does not accept text input gets no editor commands.
func TestEditActionsRequireAcceptingHandler(t *testing.T) {
	handler := &fakeEditHandler{text: "hello", sel: UTF16Range{Start: 0, End: 5}, acceptsText: false}
	ta, window := newEditTestWindow(t, handler)
	app := ta.App()

	pressKey(t, window, app, "ctrl-a")
	pressKey(t, window, app, "ctrl-c")
	wantEditStrings(t, "rejecting handler", handler.events, nil)
}

// TestEditActionsNoHandler pins the no-input-handler case: a window
// without a focused text handler runs no editor commands (a focused
// non-editable element gets none — Chromium's CanEdit).
func TestEditActionsNoHandler(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	focus := app.FocusHandle().TabStop(true)
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			return Div().Flex().ID("root").SizeFull().TrackFocus(focus).IntoElement()
		}
	})
	window := cx.Window()
	app.Update(func(*App) { window.Focus(focus) })
	cx.RunUntilParked()

	// Nothing to observe except that the key is not consumed: the
	// dispatch propagates.
	key := MustParseKeystroke("ctrl-c")
	if propagate := window.DispatchKeyEvent(&KeyDownEvent{Keystroke: key}, app); !propagate {
		t.Fatal("ctrl-c without an input handler was consumed; want propagation (no editor command)")
	}
}

// TestEditActionsPreventedKeydown pins the default-action position's
// suppression: a listener that prevents the keydown (the DOM's
// listener-then-default order) keeps the edit commands from running.
func TestEditActionsPreventedKeydown(t *testing.T) {
	handler := &fakeEditHandler{text: "hello", acceptsText: true}
	ta := NewTestApp()
	app := ta.App()
	focus := app.FocusHandle().TabStop(true)
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			w.HandleInput(focus, handler)
			return Div().Flex().ID("root").SizeFull().TrackFocus(focus).
				OnKeyDown(func(_ *KeyDownEvent, w *Window, app *App) { w.PreventDefault() }).
				IntoElement()
		}
	})
	window := cx.Window()
	app.Update(func(*App) { window.Focus(focus) })
	cx.RunUntilParked()

	pressKey(t, window, app, "ctrl-a")
	wantEditStrings(t, "prevented", handler.events, nil)
}

// TestEditActionsPreventDefaultEvent pins the DOM events' own prevent
// semantics: a prevented copy event skips the write (observable as no
// fault here — the write fails hostless anyway, so the gate is the
// returned bool), a prevented beforeinput skips the mutation.
func TestEditActionsPreventDefaultEvent(t *testing.T) {
	handler := &fakeEditHandler{text: "hello", sel: UTF16Range{Start: 0, End: 5}, acceptsText: true}
	handler.prevent = func(event string) bool { return event == "beforeinput" }
	ta, window := newEditTestWindow(t, handler)
	app := ta.App()

	pressKey(t, window, app, "ctrl-x")
	wantEditStrings(t, "prevented beforeinput", handler.events, []string{"cut", "beforeinput:deleteByCut:"})
	if handler.text != "hello" {
		t.Fatalf("text after a prevented beforeinput = %q, want hello", handler.text)
	}
}

// TestEditActionsSelectAllNeedsEventSurface pins the bounded surface:
// a handler without the EditActionHandler extension gets no select-all
// (the core's only selection seam — the IME trait's
// set_selected_text_range — is the pin's no-op in both real handlers),
// while the copy/cut/paste mutations still run.
func TestEditActionsSelectAllNeedsEventSurface(t *testing.T) {
	handler := &plainEditHandler{text: "hello", sel: UTF16Range{Start: 0, End: 5}, acceptsText: true}
	ta, window := newEditTestWindow(t, handler)
	app := ta.App()

	pressKey(t, window, app, "ctrl-a")
	if handler.text != "hello" || handler.sel != (UTF16Range{Start: 0, End: 5}) {
		t.Fatalf("select-all without the event surface moved state: %q %+v", handler.text, handler.sel)
	}
	// The clipboard commands still run their mutations through the base
	// interface: the cut deletes the selection.
	pressKey(t, window, app, "ctrl-x")
	if handler.text != "" {
		t.Fatalf("text after cut without the event surface = %q, want empty", handler.text)
	}
}

// plainEditHandler is fakeEditHandler without the EditActionHandler
// surface (the base InputHandler + IMEInputHandler contract).
type plainEditHandler struct {
	text        string
	sel         UTF16Range
	acceptsText bool
}

func (h *plainEditHandler) AcceptsTextInput(*Window, *App) bool { return h.acceptsText }

func (h *plainEditHandler) DispatchInput(input string, w *Window, app *App) {
	h.ReplaceTextInRange(nil, input, w, app)
}

func (h *plainEditHandler) ReplaceTextInRange(rng *UTF16Range, text string, w *Window, app *App) {
	start, end := 0, UTF8ToUTF16Offset(h.text, len(h.text))
	if rng != nil {
		start, end = rng.Start, rng.End
	}
	start = min(start, end)
	rng8 := UTF16Range{
		Start: UTF16ToUTF8Offset(h.text, start),
		End:   UTF16ToUTF8Offset(h.text, end),
	}
	h.text = h.text[:rng8.Start] + text + h.text[rng8.End:]
	caret := start + UTF8ToUTF16Offset(text, len(text))
	h.sel = UTF16Range{Start: caret, End: caret}
}

func (h *plainEditHandler) SelectedTextRange(bool, *Window, *App) (UTF16Selection, bool) {
	return UTF16Selection{Range: h.sel}, true
}

func (h *plainEditHandler) MarkedTextRange(*Window, *App) (UTF16Range, bool) {
	return UTF16Range{}, false
}

func (h *plainEditHandler) TextForRange(rng UTF16Range, w *Window, app *App) (string, *UTF16Range, bool) {
	start, end := min(rng.Start, rng.End), max(rng.Start, rng.End)
	n := UTF8ToUTF16Offset(h.text, len(h.text))
	start, end = min(start, n), min(end, n)
	adjusted := UTF16Range{Start: start, End: end}
	rng8 := UTF16Range{
		Start: UTF16ToUTF8Offset(h.text, start),
		End:   UTF16ToUTF8Offset(h.text, end),
	}
	return h.text[rng8.Start:rng8.End], &adjusted, true
}

func (h *plainEditHandler) ReplaceAndMarkTextInRange(rng *UTF16Range, text string, newSelectedRange *UTF16Range, w *Window, app *App) {
	h.ReplaceTextInRange(rng, text, w, app)
}

func (h *plainEditHandler) UnmarkText(*Window, *App) {}

func (h *plainEditHandler) BoundsForRange(UTF16Range, *Window, *App) (Bounds, bool) {
	return Bounds{}, false
}

func (h *plainEditHandler) CharacterIndexForPoint(Point, *Window, *App) (int, bool) {
	return 0, false
}

func (h *plainEditHandler) SetSelectedTextRange(UTF16Range, *Window, *App) {}

func (h *plainEditHandler) TextLengthUTF16(*Window, *App) (int, bool) {
	return UTF8ToUTF16Offset(h.text, len(h.text)), true
}

// wantEditStrings asserts the edit-action transcript equality.
func wantEditStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%s events = %v, want %v", what, got, want)
	}
}
