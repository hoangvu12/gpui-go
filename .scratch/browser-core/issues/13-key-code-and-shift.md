# 13 - Key code and location, with Shift kept

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 6 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** Key events carry the physical code and location, Shift is kept on the keystroke, and keymaps match either form, so Ctrl+Shift+] fires as written.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Event-log fixtures of key/code for shifted punctuation match Chrome on a US layout.
- [x] A binding written as ctrl-shift-] and one written as ctrl-} both fire for the same keystroke.
- [x] Existing keymap tests pass.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

### API

- `gpui.Keystroke` gains the physical key's identity alongside the key character (`gpui/keystroke.go:196-202`):
  - `Code string` — the DOM `KeyboardEvent.code` ("KeyA", "Digit1", "BracketRight"), layout-independent, `""` for parsed/synthesized keystrokes;
  - `Location KeyLocation` — the DOM `KeyboardEvent.location`.
- `gpui.KeyLocation` (`gpui/keystroke.go:126-166`): `KeyLocationStandard/Left/Right/Numpad` (DOM_KEY_LOCATION_0-3) with `String()`.
- `gpui.Keystroke.WithSimulatedCode()` (`gpui/keystroke.go:487`): simulated input takes the platform's key spelling — Code from the US key table (or a preset one), the Location the code implies, and the shifted-key fold with Shift kept (`shift-]` → `Key "}", Shift, Code "BracketRight"`); letters never fold.
- Matching is unchanged for keystrokes without a code (parsed, all existing tests): `Keystroke.ShouldMatch` (`gpui/keystroke.go:247-281`) adds the two either-form rules, active only when the typed keystroke carries a code (every platform-produced one does):
  - key form — a binding written with the shifted character and no shift (`ctrl-}`) matches the typed key with Shift folded away (only when the key is the shifted form of the physical key, so `shift-a` still requires a shift binding);
  - code form — a binding written with the unshifted physical key and shift (`ctrl-shift-]`) matches the typed modifiers with the code's unshifted US character.

### What changed

- `gpui/keyboard_win32.go:304-325` — `win32TranslateKeyDown` derives the DOM code from the message's scan code — lParam bits 16-23 plus the extended bit 24 (never from the virtual key) — and fills `Keystroke.Code`/`Location`.
- `gpui/keyboard_win32.go:328-348` — `keystrokeKeyOf` no longer clears Shift in the shifted-key conversion (the report-03 `gpui/keyboard_win32.go:309-312` divergence, zterm #25); the file header records the deliberate divergence toward the browser (D0).
- `gpui/keycode_win32.go` (new, Windows) — the scan-code half: `win32ScanCodeOfLParam` (`:36-44`), `domCodeOfScanCode` with Chromium's four shared-key scan-code remaps (`:49-64`), and the 155-entry `scanCodeToDomCode` table (`:66`, generated from `dom_code_data.inc`'s win column and checked against the original).
- `gpui/keycode.go` (new, platform-neutral) — the DOM code tables: the US character pairs of the writing-system codes (unshifted for matching, shifted only for the foldable OEM/digit set — letters never fold), the key→code lookup for `WithSimulatedCode`, and the location table (`domKeyLocationOfCode`, `:186`) ported from `DomCodeToLocation`.

### Harness extension (ticket 09's keydown/keyup vocabulary)

- `internal/parity/fixture.go:219-223` — `EventRecord` gains `Code` (omitempty) and `Location` (omitempty, the DOM 0-3 value); key entries on both sides carry them.
- `internal/parity/fixture.go:293-321, 343-354` — the `KeyDown`/`KeyUp`/`KeyDownPreventDefault` helpers log `e.Keystroke.Code`/`Location` through the widened `appendKey`.
- `internal/parity/fixture.go:85-115` — `Step.Code` + `parity.KeyCode("1", "Numpad1")`: a step can spell a physical key the keystroke syntax cannot (numpad), with both sides carrying the code and the location following it.
- `internal/parity/recorder.go:42-80, 85` — the page instrumentation records `e.code`/`e.location`; `eventsScript` maps them.
- `internal/parity/recorder.go:481-566` — the recorder's key dispatch checked and fixed: `Input.dispatchKeyEvent` needs the whole DOM identity, or Chrome reports the wrong `e.code`/`e.location`. The keystroke is normalized with `gpui.Keystroke.WithSimulatedCode()` first, then the CDP events carry the DOM key (`key` param, e.g. `"}"` for Shift+]), the physical code (`code` param), the unshifted US virtual key (`windowsVirtualKeyCode`/`nativeVirtualKeyCode`, from the new `cdpVKOfCode` table `:568` — the old single-char fallback produced a wrong code (`"Key]"`) and a wrong VK for punctuation), the CDP modifier bits (Alt=1, Ctrl=2, Meta=4, Shift=8), and the location via the `location` (1/2) and `isKeypad` params. Verified against `content/browser/devtools/protocol/input_handler.cc` (fetched from source.chromium.org) and the recorded fixtures (Chrome 155) — the numpad step records `e.location` 3.
- `internal/parity/replay.go:209-227` — the replay's `StepKey` normalizes the parsed keystroke with `WithSimulatedCode()` (plus a preset `Step.Code`), so the gpui listeners see the same key/code/location pair the platform reports.
- `docs/browser-parity-harness.md` — the key-entry vocabulary, the `KeyCode` step and a recorder-details bullet on the CDP key event identity.
- Registered in `internal/parity/replay_test.go` and `cmd/parityrecord/main.go`.

### Fixtures

- New package `internal/parity/fixtures/keycode` (registered; testdata `keycode-*`):
  - `keycode-shifted` — Shift+]/Shift+1/Shift+; (key "}", "!", ":", codes BracketRight/Digit1/Semicolon), a plain `a` (KeyA) and a numpad 1 (`KeyCode("1", "Numpad1")`, location 3): the Tier B transcript's key/code/location match Chrome on a US layout.
  - `keycode-binding-code` / `keycode-binding-char` — the same Ctrl+Shift+] keystroke under the browser dispatch order with `ctrl-shift-]` and `ctrl-}` bound respectively; `keycode_test.go` replays both and asserts each action fired exactly once (`TestBindingFormsBothFire`), plus the negative control (`ctrl-]` fires nothing, `TestBindingCodeFormNeedsTheEitherFormMatch`).
- The keyorder fixtures (ticket 09) were re-recorded so their key entries carry the code field (Chrome 155.0.8059.85, same pages/steps).
- Go unit tests: `gpui/keycode_test.go` (either-form matching incl. keymap-level `BindingsForInput` and chord prefixes; `WithSimulatedCode`; letters don't fold) and `gpui/keycode_win32_test.go` (synthetic WM_KEYDOWN wParam/lParam → code+location incl. extended/numpad/left/right and the shared-key remaps; the full translation under a stated-and-guarded US layout assumption — the scan-code table itself is layout-independent, so no ToUnicode injection was needed for it).

### Chromium citations (0d4dcd4f, %TEMP%\research-src\chromium; `content/...input_handler.cc` fetched from source.chromium.org)

- Scan code from lParam: `ui/events/win/events_win_utils.cc:481-495` (`GetScanCodeFromLParam`: bits 16-23 + bit 24 → `0xE000` prefix) and `:369-372` (`CodeFromMSG` → `CodeForWindowsScanCode`).
- Scan code → code: `ui/events/keycodes/keyboard_code_conversion_win.cc:31-49` (the four remaps) + `ui/events/keycodes/dom/keycode_converter.cc:24-31` (the IS_WIN macro picks the win column), `:178-186` (`NativeKeycodeToDomCode`); the table is `ui/events/keycodes/dom/dom_code_data.inc` (win column; `:171` Digit1, `:190-192` brackets, `:202` Semicolon, `:225` PrintScreen, `:228` Pause, `:243-263` numpad, `:336/:340` Lang1/Lang2).
- Location from the code: `ui/events/keycodes/dom/keycode_converter.cc:351-400` (`DomCodeToLocation`: numpad + left/right modifiers) → modifier bits (`ui/events/blink/blink_event_util.cc:763-776`, `ui/events/blink/web_input_event.cc:82-97`) → `e.location` (`third_party/blink/renderer/core/events/keyboard_event.cc:62-70`; `e.code` at `:117`).
- CDP key identity: `third_party/blink/public/devtools_protocol/domains/Input.pdl:97-140` (the `code`/`key`/`windowsVirtualKeyCode`/`isKeypad`/`location` params) and `content/browser/devtools/protocol/input_handler.cc` (`GetEventModifiers`: 1→Alt, 2→Ctrl, 4→Meta, 8→Shift, `location` 1/2 → kIsLeft/kIsRight, `isKeypad` → kIsKeyPad; `key`→dom_key, `code`→dom_code).

### Limitations

- Shift plus a letter keeps the pin's lowercase `Key` ("a"), not the DOM key ("A"): the pin's shape, kept deliberately (report 03 §4 "Matches" the GPUI shape); key fixtures use shifted punctuation, and `docs/browser-parity-harness.md` records the rule.
- `e.key` for CDP-dispatched events is the `key` param verbatim, so the recorder sends the platform-folded spelling — the fixtures compare the port's behavior against that spelling, not a real user's `e.key` for keys the port does not fold (same limitation, one source).
- The either-form matching's code form resolves the binding key through the code's US character; bindings written with non-US characters still match through the exact and key-form rules (layout-dependent), and the WindowsKeyboardMapper path already maps bindings through the live layout.
- The layout-dependent translation tests (shifted-key conversion, full translation) assume and guard the US layout (`requireUSLayout` skips otherwise); the scan-code table and matching tests are layout-independent.
- Ticket 13's Status stays `claimed` for the orchestrator's resolve; the acceptance checkboxes above are all met and verified by the commands in the commit message.
