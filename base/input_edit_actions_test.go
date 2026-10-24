package base

import (
	"runtime"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Go-own tests of browser-core 73: Base's inputs and editors take the
// editing commands — Copy, Cut, Paste, SelectAll — from the core's
// default-action arm (browser-core 26, Chrome's Windows key table) on
// the input element handler's gpui.EditActionHandler surface
// (input_state_edit.go), not from their own key bindings (inputInit's
// !mac letter rows are deleted). The TestApp has no clipboard, so the
// copy and paste halves observe what survives hostless: the write's
// absence (a masked value stays out of the clipboard, a paste inserts
// nothing) and the command's mutations (select-all's selection, cut's
// deletion); the clipboard exchange itself is pinned by clipspec and
// gpui's TestEditActions* suite.

func editKeystroke(letters string) string {
	if runtime.GOOS == "darwin" {
		return "cmd-" + letters
	}
	return "ctrl-" + letters
}

// The editing keys run on the focused input through the core's
// default-action arm: ctrl-a selects all, ctrl-x cuts the selection,
// and no action dispatches (the keystroke is no longer a binding). The
// Windows alternates the upstream list never bound also run: ctrl-insert
// (copy) leaves the state alone, shift-insert (paste) inserts nothing
// hostless (editing_behavior.cc:168-170).
func TestInputEditCommandsRunThroughTheCoreDefaultAction(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin keeps the input's cmd bindings (the core's table is !mac)")
	}
	v := buildInput(t, plainInput)
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("alpha beta", w, cx)
		s.Focus(w, cx)
	})
	v.cx.RunUntilParked()
	dispatched := 0
	countDispatch := func(action gpui.AnyAction) {
		v.cx.Update(func(_ *gpui.Window, app *gpui.App) {
			app.OnAction(action, func(_ any, phase gpui.DispatchPhase, _ *gpui.App) {
				if phase == gpui.DispatchBubble {
					dispatched++
				}
			})
		})
	}
	countDispatch(CopyAction)
	countDispatch(CutAction)
	countDispatch(PasteAction)
	countDispatch(SelectAllAction)

	// Select-all: the whole value is selected.
	v.cx.SimulateKeystrokes(editKeystroke("a"))
	v.read(func(s *InputState) { wantSelected(t, s, 0, 10) })

	// Cut: the selection is deleted (the write is hostless).
	v.cx.SimulateKeystrokes(editKeystroke("x"))
	v.cx.RunUntilParked()
	v.read(func(s *InputState) { wantValue(t, s, "") })

	// The copy alternate and the paste alternate run without a
	// clipboard: no mutation, no dispatch.
	v.cx.SimulateKeystrokes(editKeystroke("c"))
	v.cx.RunUntilParked()
	v.cx.SimulateKeystrokes("ctrl-insert")
	v.cx.RunUntilParked()
	v.cx.SimulateKeystrokes("shift-insert")
	v.cx.RunUntilParked()
	v.read(func(s *InputState) { wantValue(t, s, "") })
	if dispatched != 0 {
		t.Fatalf("the editing keys dispatched the action %d times; want the core commands (no action dispatch)", dispatched)
	}
}

// A masked input's value stays out of the clipboard: cut through the
// core's command neither deletes nor writes (the seam runs the upstream
// gated body, which the prevented return also shields from the core's
// own single-selection write).
func TestInputEditCommandsKeepAMaskedValueOutOfTheClipboard(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin keeps the input's cmd bindings (the core's table is !mac)")
	}
	v := buildInput(t, plainInput)
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.SetValue("hunter2", w, cx)
		s.SetMasked(true, w, cx)
		s.Focus(w, cx)
	})
	v.cx.RunUntilParked()

	v.cx.SimulateKeystrokes(editKeystroke("a"))
	v.cx.SimulateKeystrokes(editKeystroke("c"))
	v.cx.SimulateKeystrokes(editKeystroke("x"))
	v.cx.RunUntilParked()
	v.read(func(s *InputState) {
		wantValue(t, s, "hunter2")
		wantSelected(t, s, 0, 7)
	})
}

// Tab in a single-line input moves focus out (the plain textarea's
// rule): the tab binding's action finds no single-line handler, the
// key falls to the core's Tab default action, and with no next stop
// focus passes out of the input (browser-core 14's pass-out — the
// editor does not consume a text-insertion Tab at key-down,
// editor_key_bindings.cc:62-68).
func TestInputTabMovesFocusOutOfASingleLineInput(t *testing.T) {
	v := buildInput(t, plainInput)
	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputBaseState[InputMode]]) {
		s.Focus(w, cx)
	})
	v.cx.RunUntilParked()
	v.cx.SimulateKeystrokes("tab")
	v.cx.RunUntilParked()
	focused := false
	v.cx.Update(func(w *gpui.Window, _ *gpui.App) { focused = v.readFocused(w) })
	if focused {
		t.Fatal("tab left focus inside the single-line input; want the core's focus movement")
	}
}

func (v *inputView[M]) readFocused(w *gpui.Window) bool {
	focused := false
	v.input.Read(v.cx.App(), func(s *InputBaseState[M], _ *gpui.App) struct{} {
		focused = s.focusHandle.IsFocused(w)
		return struct{}{}
	})
	return focused
}

// Tab in the multi-line editor inserts an indent (the textarea in
// tab-capture mode, the rule a code editor follows): the editor's
// IndentInline handler consumes the key, so the core's Tab default
// action never runs and focus stays inside.
func TestEditorTabInsertsAnIndentAndKeepsFocus(t *testing.T) {
	v := buildEditor(t)
	setupCursors(v, "a|b")
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) { s.Focus(w, cx) })
	v.cx.RunUntilParked()
	v.cx.SimulateKeystrokes("tab")
	v.cx.RunUntilParked()
	var tabIndent string
	v.read(func(s *EditorState) {
		tabIndent = s.mode.tabSize().String()
	})
	v.read(func(s *EditorState) { wantValue(t, s, "a"+tabIndent+"b\n") })
	focused := false
	v.cx.Update(func(w *gpui.Window, _ *gpui.App) { focused = v.readFocused(w) })
	if !focused {
		t.Fatal("tab moved focus out of the editor; want the indent to consume the key")
	}
}
