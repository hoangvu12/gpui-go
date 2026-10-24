package gpui

// The window selection's share of the edit actions (browser-core ticket
// 19): the copy command with no focused editable falls to the window's
// text selection, exactly like Chromium's editor commands, which act on
// the frame's selection rather than only on editable elements —
// ExecuteCopy reads frame->Selection() whether or anything is focused
// (third_party/blink/renderer/core/editing/commands/
// clipboard_commands.cc:321-371: the copy event fires and the write gates
// on the selection; the port's static-selection copy fires no event
// surface, a documented limitation — the DOM copy event targets the body,
// and the port's EditActionHandler seam is per focused input).
//
// browser-core 73: Base no longer keeps its own CopyAction binding on
// its Root (U:root.rs:264-273 deleted) — this default owns the copy
// keystroke in a Base application too, so the copy is untrimmed where
// the Root's handler trimmed it (Chrome's rule: the selection is copied
// exactly). The Root's OnAction handler stays for programmatic dispatch
// of the aliased edit::Copy.

// windowSelectionEditHandler is the EditActionHandler the copy command
// dispatches to when the focused element accepts no text input and the
// window runs a selection layer: the frame-selection stand-in. Only Copy
// acts (static text is read-only: cut and paste stay editable-only
// commands, and select-all is the focused handler's own selection).
type windowSelectionEditHandler struct {
	w *Window
}

// AcceptsTextInput implements InputHandler: static text accepts none.
func (windowSelectionEditHandler) AcceptsTextInput(_ *Window, _ *App) bool { return false }

// ReplaceTextInRange implements InputHandler: static text is read-only.
func (windowSelectionEditHandler) ReplaceTextInRange(_ *UTF16Range, _ string, _ *Window, _ *App) {
}

// DispatchInput implements InputHandler: static text takes no input.
func (windowSelectionEditHandler) DispatchInput(_ string, _ *Window, _ *App) {}

// Copy fires the copy default (the clipboard write of the window's
// selected text, in document order — TextSelectionSelectedText), which is
// the port's whole static-selection copy: the DOM event the command fires
// on the body has no port surface (see the file comment).
func (h windowSelectionEditHandler) Copy(_ *Window, app *App) bool {
	if text := TextSelectionSelectedText(h.w, app); text != "" {
		if err := app.WriteClipboard(NewClipboardStringItem(text)); err != nil {
			editFault(app, "copy write failed", err)
		}
	}
	return false
}

// Cut implements EditActionHandler: a no-op (static text is not cuttable).
func (windowSelectionEditHandler) Cut(_ *Window, _ *App) bool { return false }

// Paste implements EditActionHandler: a no-op.
func (windowSelectionEditHandler) Paste(_ *ClipboardItem, _ *Window, _ *App) bool { return false }

// BeforeInput implements EditActionHandler: a no-op.
func (windowSelectionEditHandler) BeforeInput(_, _ string, _ *Window, _ *App) bool { return false }

// Select implements EditActionHandler: a no-op.
func (windowSelectionEditHandler) Select(_ *Window, _ *App) {}

// SelectAll implements EditActionHandler: a no-op (the window selection
// never receives the select-all command — the fallback wires Copy only).
func (windowSelectionEditHandler) SelectAll(_ *Window, _ *App) {}

// windowSelectionEditHandlerFor is the focused-handler fallback for the
// edit commands (browser-core 19): nil when the window runs no selection
// layer (nothing is selectable, the command does not run).
func windowSelectionEditHandlerFor(w *Window, app *App) InputHandler {
	if _, ok := textSelectionStateExisting(w, app); ok {
		return windowSelectionEditHandler{w: w}
	}
	return nil
}
