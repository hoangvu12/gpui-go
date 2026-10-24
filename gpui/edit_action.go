package gpui

// This file is browser-core ticket 26 (edit actions and the HTML
// clipboard, .scratch/browser-core/issues/26-edit-actions-and-html-
// clipboard.md): the core's editing actions — copy, cut, paste and
// select-all — as canonical actions, their Windows-Chrome key
// bindings, and the default-action wiring that runs them on the
// focused input handler, in Chromium's order and with Chromium's DOM
// event sequence.
//
// The Chromium reference (0d4dcd4f, %TEMP%\research-src\chromium):
//
//   - The key bindings are the Windows rows of the editing key table,
//     third_party/blink/renderer/core/editing/editing_behavior.cc:
//     168-170 — {VKEY_INSERT, kCtrlKey, "Copy"}, {VKEY_INSERT,
//     kShiftKey, "Paste"}, {VKEY_DELETE, kShiftKey, "Cut"} — and the
//     !Mac letter rows, editing_behavior.cc:174-178 — {'C', kCtrlKey,
//     "Copy"}, {'V', kCtrlKey, "Paste"}, {'X', kCtrlKey, "Cut"},
//     {'A', kCtrlKey, "SelectAll"}. EditingBehavior::InterpretKeyEvent
//     masks the modifiers to Shift|Alt|Ctrl|Meta and matches the pair
//     EXACTLY (editing_behavior.cc:288-302: the map key is
//     `modifiers << 16 | virtual_key`), so Ctrl+Shift+C, plain C and
//     Ctrl+Alt+C bind nothing.
//   - The dispatch: Editor::HandleKeyboardEvent (core/editing/
//     editor_key_bindings.cc:128-135) runs the command via
//     HandleEditingKeyboardEvent (:43-70: InterpretKeyEvent →
//     CreateCommand → command.Execute for non-text-insertion commands
//     at kRawKeyDown), and it is the FIRST arm of
//     KeyboardEventManager::DefaultKeyboardEventHandler
//     (core/input/keyboard_event_manager.cc:442-445) — the editor
//     commands run before the IME-composition skip, Tab, Escape and
//     the navigation keys, and an event the command consumed is
//     DefaultHandled (editor_key_bindings.cc:131-134).
//   - The command bodies (core/editing/commands/clipboard_commands.cc
//     and editor_command.cc:876-886): ExecuteCopy (:321-371) fires the
//     copy event and writes the clipboard when a selection exists;
//     ExecuteCut (:389-431) fires the cut event, writes, fires
//     beforeinput (kDeleteByCut, :417-420) and deletes; Paste
//     (:466-540) reads the clipboard, fires the paste event, fires
//     beforeinput (kInsertFromPaste, :508-514) and inserts; the events
//     fire regardless of the selection's state (verified by the
//     ticket's headless-Chrome records: a collapsed caret still fires
//     copy and cut; only the write and the mutation gate on the
//     selection); ExecuteSelectAll (editor_command.cc:876-886) moves
//     the selection and TextControlElement::ScheduleSelectEvent
//     (core/html/forms/text_control_element.cc:949-957) fires the
//     select event, which the records show landing between the
//     keydown and the keyup (synchronously with the command).
//   - The input case writes plain text only: EnclosingTextControl
//     (clipboard_commands.cc:352-358) — WritePlainText, no CF_HTML.
//
// The port's flows run from the default-action position
// (runKeyDefaultActions, gpui/key_default_action.go — the listeners
// ran, a prevented key suppresses everything, and an app binding that
// handled the key returned before it), gated on the focused input
// handler accepting text input (the CanEdit arm), skipped while an IME
// composition is active (the key events carry IsComposing; Chromium's
// composing keys are kVKeyProcessKey, which matches no table row), and
// they consume the key (DefaultHandled) once the command ran.

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// The edit actions (canonical descriptors)
// ---------------------------------------------------------------------------

// Copy is the copy editing action's payload (the "Copy" editor
// command of editing_behavior.cc's table). The action exists for apps
// to bind and observe; the standard keys run the edit-command flow
// directly from the default-action position.
type Copy struct{}

// CopyAction is the canonical copy action descriptor.
var CopyAction = DefineUnitAction[Copy]("edit::Copy")

// Cut is the cut editing action's payload (the "Cut" command).
type Cut struct{}

// CutAction is the canonical cut action descriptor.
var CutAction = DefineUnitAction[Cut]("edit::Cut")

// Paste is the paste editing action's payload (the "Paste" command).
type Paste struct{}

// PasteAction is the canonical paste action descriptor.
var PasteAction = DefineUnitAction[Paste]("edit::Paste")

// SelectAll is the select-all editing action's payload (the
// "SelectAll" command).
type SelectAll struct{}

// SelectAllAction is the canonical select-all action descriptor.
var SelectAllAction = DefineUnitAction[SelectAll]("edit::SelectAll")

// ---------------------------------------------------------------------------
// The edit-event seam (the DOM events of the editing commands)
// ---------------------------------------------------------------------------

// EditActionHandler is the optional edit-action extension of
// InputHandler (browser-core ticket 26): the copy/cut/paste/
// beforeinput/select events the DOM fires on the focused editable
// element, delivered in Chromium's order around the mutations the
// core's edit actions perform (see the file header's citations), plus
// the SelectAll command's selection move. A handler that does not
// implement this surface still receives the text mutations through
// the base interface (the copy/cut writes and the paste insert run
// without the events), exactly like the composition split of ticket
// 18 (gpui.IMECompositionHandler).
//
// The boolean returns report the DOM event's defaultPrevented: true
// stops the command's default processing (the clipboard write, the
// deletion, the insertion) while the key stays consumed.
type EditActionHandler interface {
	InputHandler

	// Copy fires the DOM copy event (ClipboardEvent "copy", fired by
	// ExecuteCopy before the clipboard write). true = prevented: the
	// default write is skipped.
	Copy(w *Window, app *App) bool
	// Cut fires the DOM cut event (ExecuteCut's first step). true =
	// prevented: the write and the deletion are skipped.
	Cut(w *Window, app *App) bool
	// Paste fires the DOM paste event carrying the clipboard item that
	// was read for it (ClipboardEvent "paste"; the DataTransfer the
	// command builds — a nil item is an unreadable clipboard). true =
	// prevented: the beforeinput and the insertion are skipped.
	Paste(item *ClipboardItem, w *Window, app *App) bool
	// BeforeInput fires the DOM beforeinput event (InputEvent; the
	// command's mutation preview — "deleteByCut" for the cut
	// command, "insertFromPaste" for a paste). true = prevented: the
	// mutation is skipped.
	BeforeInput(inputType, data string, w *Window, app *App) bool
	// Select fires the DOM select event (TextControlElement's
	// selection-change notification).
	Select(w *Window, app *App)
	// SelectAll selects the handler's whole text (the SelectAll
	// editing command's selection move; Select fires after it).
	SelectAll(w *Window, app *App)
}

// ---------------------------------------------------------------------------
// The key table (EditingBehavior::InterpretKeyEvent's Windows rows)
// ---------------------------------------------------------------------------

// editCommandKind is one editing command of the key table.
type editCommandKind uint8

const (
	editCommandNone editCommandKind = iota
	editCommandCopy
	editCommandCut
	editCommandPaste
	editCommandSelectAll
)

// String renders the command (the execCommand name the table spells).
func (c editCommandKind) String() string {
	switch c {
	case editCommandCopy:
		return "Copy"
	case editCommandCut:
		return "Cut"
	case editCommandPaste:
		return "Paste"
	case editCommandSelectAll:
		return "SelectAll"
	}
	return ""
}

// editCommandOfKeystroke maps one keystroke onto its editing command,
// the ported Windows rows of kKeyboardCodeKeyDownEntries
// (editing_behavior.cc:168-170 and 174-178), with
// InterpretKeyEvent's exact-match rule (editing_behavior.cc:288-302):
// the modifiers mask to Shift/Alt/Ctrl/Platform (the WebInputEvent
// four; the port's Function modifier is not a DOM modifier) and must
// equal the row's modifiers exactly, and the key is the row's virtual
// key (letters stay lowercase — the platform reports the unshifted
// character — and the immutable names are the keystroke grammar's).
//
// The rows the port does not carry: PasteAndMatchStyle (Ctrl+Shift+V,
// the match-style paste), Undo/Redo and the movement commands — other
// tickets own them; a keystroke that maps to none reports
// editCommandNone.
func editCommandOfKeystroke(k Keystroke) editCommandKind {
	m := k.Modifiers
	masked := Modifiers{Control: m.Control, Alt: m.Alt, Shift: m.Shift, Platform: m.Platform}
	ctrlOnly := masked == Modifiers{Control: true}
	shiftOnly := masked == Modifiers{Shift: true}
	switch {
	case ctrlOnly:
		switch strings.ToLower(k.Key) {
		case "c":
			return editCommandCopy
		case "x":
			return editCommandCut
		case "v":
			return editCommandPaste
		case "a":
			return editCommandSelectAll
		case "insert":
			// {VKEY_INSERT, kCtrlKey, "Copy"} — the Windows alternate of
			// the copy binding (editing_behavior.cc:168).
			return editCommandCopy
		}
	case shiftOnly:
		switch k.Key {
		case "delete":
			// {VKEY_DELETE, kShiftKey, "Cut"} (editing_behavior.cc:170).
			return editCommandCut
		case "insert":
			// {VKEY_INSERT, kShiftKey, "Paste"} (editing_behavior.cc:169).
			return editCommandPaste
		}
	}
	return editCommandNone
}

// ---------------------------------------------------------------------------
// The default-action wiring (Editor::HandleKeyboardEvent)
// ---------------------------------------------------------------------------

// defaultEditActions is the editor-command arm of the keydown's
// default actions (browser-core ticket 26; Chromium's
// KeyboardEventManager::DefaultKeyboardEventHandler runs
// frame_->GetEditor().HandleKeyboardEvent FIRST, keyboard_event_
// manager.cc:442-445): the Windows-Chrome editing keys map to their
// commands, which run on the focused input handler when it accepts
// text input (the CanEdit arm of the command's enabled checks — a
// focused non-editable element gets no editor commands). A key event
// flagged IsComposing (an IME composition in progress) runs nothing:
// Chromium's composing key is kVKeyProcessKey, which matches no row of
// the table. It reports whether the command ran (DefaultHandled — the
// key is consumed).
func (w *Window) defaultEditActions(event any, app *App) bool {
	keyDown, ok := event.(*KeyDownEvent)
	if !ok {
		return false
	}
	if keyDown.IsComposing {
		// The IME composition owns the keyboard (browser-core ticket
		// 18): no editor commands while composing.
		return false
	}
	fs := focusState(w)
	handler := fs.inputHandler
	if handler == nil || !handler.AcceptsTextInput(w, app) {
		return false
	}
	command := editCommandOfKeystroke(keyDown.Keystroke)
	if command == editCommandNone {
		return false
	}
	return w.runEditCommand(command, handler, app)
}

// runEditCommand executes one editing command on the focused input
// handler (the EditorCommand bodies of clipboard_commands.cc and
// editor_command.cc — see the file header's citations). The selection
// queries and text mutations go through the IME extension of the
// handler when it implements it (both real handlers do; a base-only
// handler has no selection, so only the paste insertion can run); the
// DOM events go through the optional EditActionHandler surface, which
// can prevent each default step. It reports whether the command ran
// (always true once a command was mapped — DefaultHandled).
func (w *Window) runEditCommand(command editCommandKind, handler InputHandler, app *App) bool {
	ime, hasIME := handler.(IMEInputHandler)
	events, hasEvents := handler.(EditActionHandler)

	// selectionText is the handler's selected text (the copy/cut
	// write source; CanCopy's non-empty selection gate).
	selectionText := func() (string, UTF16Range, bool) {
		if !hasIME {
			return "", UTF16Range{}, false
		}
		sel, ok := ime.SelectedTextRange(true, w, app)
		if !ok || sel.Range.Start >= sel.Range.End {
			return "", UTF16Range{}, false
		}
		text, _, ok := ime.TextForRange(sel.Range, w, app)
		if !ok || text == "" {
			return "", UTF16Range{}, false
		}
		return text, sel.Range, true
	}

	switch command {
	case editCommandCopy:
		// ExecuteCopy (clipboard_commands.cc:321-371): fire the copy
		// event, then write the selection when one exists (the event
		// fires regardless — the record's collapsed-caret copy).
		if hasEvents && events.Copy(w, app) {
			return true
		}
		if text, _, ok := selectionText(); ok {
			// The input case writes plain text (EnclosingTextControl,
			// clipboard_commands.cc:352-358: WritePlainText, no
			// CF_HTML).
			if err := app.WriteClipboard(NewClipboardStringItem(text)); err != nil {
				// The reference's write_to_clipboard logs failures; the
				// port records a host fault (the command still consumed
				// the key).
				editFault(app, "copy write failed", err)
			}
		}
		return true

	case editCommandCut:
		// ExecuteCut (clipboard_commands.cc:389-431): fire the cut
		// event, write the selection, fire beforeinput
		// (kDeleteByCut) and delete.
		if hasEvents && events.Cut(w, app) {
			return true
		}
		text, rng, ok := selectionText()
		if ok {
			if err := app.WriteClipboard(NewClipboardStringItem(text)); err != nil {
				editFault(app, "cut write failed", err)
			}
		}
		if ok {
			// The mutation preview fires after the write and only when
			// the deletion will happen (the record's collapsed-caret
			// cut fires the cut event and no beforeinput).
			if hasEvents && events.BeforeInput("deleteByCut", "", w, app) {
				return true
			}
			// DeleteSelectionWithSmartDelete: replace the selection
			// with nothing through the base interface.
			handler.ReplaceTextInRange(&rng, "", w, app)
		}
		return true

	case editCommandPaste:
		// Paste (clipboard_commands.cc:466-540): read the clipboard,
		// fire the paste event carrying it, fire beforeinput
		// (kInsertFromPaste) and insert the text. The read's failure
		// degrades to an empty item (the command's DataTransfer over
		// an unavailable clipboard), and the beforeinput fires before
		// the fragment check — the port fires it with the text it has.
		item, err := app.ReadClipboard()
		if err != nil {
			editFault(app, "paste read failed", err)
			item = nil
		}
		if hasEvents && events.Paste(item, w, app) {
			return true
		}
		text := ""
		if item != nil {
			if t, ok := item.Text(); ok {
				text = t
			}
		}
		if hasEvents && events.BeforeInput("insertFromPaste", text, w, app) {
			return true
		}
		if text != "" {
			handler.ReplaceTextInRange(nil, text, w, app)
		}
		return true

	case editCommandSelectAll:
		// ExecuteSelectAll (editor_command.cc:876-886) +
		// ScheduleSelectEvent: the selection move is the handler's own
		// (the port's SelectAll member — Base's InputBaseState carries
		// the same name), the select event follows it synchronously
		// (the record's select between the keydown and the keyup).
		if !hasEvents {
			// No edit-action surface: the core has no selection seam
			// to move (the IME trait's set_selected_text_range is the
			// pin's no-op contract in both real handlers), so the
			// command cannot run.
			return false
		}
		events.SelectAll(w, app)
		events.Select(w, app)
		return true
	}
	return false
}

// editFault records one edit command's clipboard-exchange failure
// (the reference's write_to_clipboard logs; the port records a host
// fault when a host is attached — a hostless application has nowhere
// to record, so its failures are silent exactly like the reference's
// log-only handling).
func editFault(app *App, context string, err error) {
	if h := app.Host(); h != nil {
		h.recordFault(fmt.Sprintf("edit action %s: %v", context, err))
	}
}
