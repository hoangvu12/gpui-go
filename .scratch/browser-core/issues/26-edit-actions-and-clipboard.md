# 26 - Edit actions and the HTML clipboard

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 09
Triage: ready-for-agent

**What to build:** Copy, cut, paste and select-all are core actions with standard keys; the clipboard reads and writes the HTML format; apps can observe clipboard changes.

**Blocked by:** 09 (Default actions run after key listeners)

**Status:** ready-for-agent

- [x] Clipboard round trip of HTML format (clipspec, rerun under contention).
- [x] Key-to-action fixtures match Chrome's editing commands for an input.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

- **API** — the core's editing actions `gpui.Copy/Cut/Paste/SelectAll` with the descriptors `edit::Copy`/`edit::Cut`/`edit::Paste`/`edit::SelectAll` (`gpui/edit_action.go`); they dispatch to the focused input handler's optional `gpui.EditActionHandler` seam (Copy/Cut/Paste/BeforeInput/Select/SelectAll, embedding `InputHandler`, the ticket-18 `IMECompositionHandler` pattern), performing the mutations through the base/IME interfaces. The clipboard model gained the HTML companion `ClipboardString.HTML` ("" = none, `WithHTML`) — `gpui/clipboard.go`.
- **Key bindings** — the Windows-Chrome rows of Chromium's editing key table with `InterpretKeyEvent`'s exact-modifier match: Ctrl+C/X/V/A, Shift+Delete (Cut), Ctrl+Insert (Copy), Shift+Insert (Paste) (`editCommandOfKeystroke`, gpui/edit_action.go; `editing_behavior.cc:168-170,174-178`). The commands run as the FIRST arm of the keydown's default actions (`defaultEditActions` in `runKeyDefaultActions`, gpui/key_default_action.go), gated on the focused input handler accepting text input, skipped while an IME composition is active (IsComposing — the kVKeyProcessKey rule), consuming the key (DefaultHandled) once a command ran (keyboard_event_manager.cc:442-445).
- **The DOM event order** (pinned by the records): select fires between the keydown and keyup of the Ctrl+A press; cut fires cut → beforeinput(deleteByCut) → delete; paste fires paste (carrying the read item) → beforeinput(insertFromPaste) → insert; the copy/cut events fire regardless of the selection's state (the record's collapsed-caret shape) while the write and the mutation gate on it (clipboard_commands.cc:321-431, 466-540; the port's flows in `runEditCommand`).
- **CF_HTML** — `gpui/cfhtml_windows.go` ports Chromium's `HtmlToCFHtml`/`CFHtmlToHTML` (the "Version:0.9" header with the four 10-digit offsets, the `<!--StartFragment-->` markers, the SourceURL line, the OpenOffice header-offset fallback, the whitespace trim) and the exchange publishes/parses the registered "HTML Format" (`clipboard_format_type_win.cc:137`) beside CF_UNICODETEXT in `writeClipboardString`/`readClipboardStringEntry` (gpui/clipboard_windows.go). The input's copy writes plain text only, matching Chrome's `EnclosingTextControl` path.
- **Busy clipboard** — `openClipboardGuard` now retries `OpenClipboard` 5× with a 5 ms backoff before the typed `*ClipboardReadError{Denied}` whose message names the attempts (Chromium's `ScopedClipboard::Acquire`, clipboard_win.cc:102-130). A recorded divergence from the pin's single immediate attempt (browser wins per D0).
- **Change observer** — `App.OnClipboardChange` → `Host.OnClipboardChange` registers `AddClipboardFormatListener` on the host's platform window; `WM_CLIPBOARDUPDATE` runs the callback synchronously on the host thread (gpui/clipboard_observer_windows.go, the case in `platformWndProc`); the listener is removed at host shutdown. Chromium's ClipboardMonitor shape (remoting/host/clipboard_win.cc:158-171, 312-318).
- **Files** — gpui/edit_action.go (the actions, the key table, the flows), gpui/cfhtml_windows.go (the codec), gpui/clipboard_windows.go (the CF_HTML write/read and the retry), gpui/clipboard_observer_windows.go, gpui/clipboard.go (the HTML field), gpui/key_default_action.go (the wiring), gpui/win32host_windows.go (WM_CLIPBOARDUPDATE), internal/parity/fixtures/editkeys/ + testdata (the fixtures), internal/clipspec/html_test.go.
- **Tests** — gpui: `TestEditCommandKeyMapping`, `TestEditActionsFireEventsAndMutate`, `TestEditActionsWindowsAlternateKeys`, `TestEditActionsEmptySelectionStillFiresEvents`, `TestEditActionsSkipDuringComposition`, `TestEditActionsRequireAcceptingHandler`, `TestEditActionsNoHandler`, `TestEditActionsPreventedKeydown`, `TestEditActionsPreventDefaultEvent`, `TestEditActionsSelectAllNeedsEventSurface`, `TestCFHTMLWriterShape/ReaderRoundTrip/ReaderOpenOfficeFallback/ReaderForeignPayloads`. clipspec (real clipboard, previous contents saved and restored): `TestClipboardHTMLFormatRoundTrip`, `TestClipboardHTMLFormatRoundTripUnderContention`, `TestClipboardBusyRetryClearError`, `TestClipboardPasteEditActionRoundTrip` (the paste pin — the real window, the real item, the insertion), `TestClipboardChangeObserver`. parity: `editkeys` + `editkeys-paste` records and the one deliberately-wrong view `TestEditKeysPlainHandlerLosesEditEvents`.
- **Key citations** — `editing_behavior.cc:168-178` (the Windows key rows), `editor_key_bindings.cc:43-70,128-135` (the command dispatch), `keyboard_event_manager.cc:442-445` (editor commands first), `clipboard_commands.cc:321-431,466-540` (the command bodies and the event order), `editor_command.cc:876-886` + `text_control_element.cc:949-957` (SelectAll and the select event), `clipboard_util_win.cc:1258-1435` (CF_HTML), `clipboard_format_type_win.cc:137` ("HTML Format"), `clipboard_win.cc:102-130` (the busy retry), remoting/host `clipboard_win.cc:158-171,312-318` (the change listener).
- **Limitations** — the letter bindings (Ctrl+C/X/V) cannot be recorded in headless Chrome (the browser process consumes them pre-renderer as the Edit-menu accelerators — no keydown reaches the page), so the fixtures record through the Windows alternates (the same commands) and gpui's unit tests pin the letter bindings; paste's clipboard data is nondeterministic at replay, so the parity record carries none (Chrome's beforeinput data is null) and the real insertion is pinned by clipspec; the select-all needs the handler's `EditActionHandler` surface (both real handlers keep the pin's no-op `SetSelectedTextRange`); Ctrl+Shift+V (PasteAndMatchStyle) and Undo/Redo are other tickets; Base keeps its own input bindings (they win at the binding position, so Base's Shift+Delete stays the pin's Delete — a recorded Base divergence; the core defaults add Ctrl+Insert/Shift+Insert copy/paste to Base inputs, a browser-parity win).
