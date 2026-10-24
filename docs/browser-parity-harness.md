# The browser-parity harness

Ticket 01 (`.scratch/browser-core/issues/01-browser-parity-harness.md`) built the spec's
**seam 1**: every behaviour check in the browser-core work is a fixture — a small HTML/CSS
page, the equivalent gpui-go view, and a scripted input sequence — recorded once in
headless Chrome, then replayed against gpui-go in a test. This is the guide for writing
new fixtures. The implementation lives in `internal/parity`:

| File | Owns |
|---|---|
| `fixture.go` | the `Fixture` type, input steps, the `EventLog` recorder, the registry |
| `replay.go` | the gpui side: test window, real text system, simulated input, observables |
| `capture.go` | the offscreen pixel capture (hidden host window + render-to-image) |
| `compare.go` | the three comparisons and their readable diffs, `MeasurePixels` |
| `recorder.go`, `ws.go`, `chrome.go` | the headless-Chrome recorder (DevTools protocol) |
| `record.go` | the checked-in record format (`record.json`, `chrome.png`, `page.html`) |
| `fixtures/<name>/` | one directory per fixture; the tracer (`fixtures/tracer`) is the template |
| `testdata/<name>/` | the recorder's output, checked in |

## The three comparisons

- **Tier A — boxes, exact.** The recorded `getBoundingClientRect` values (CSS pixels) are
  compared against the replayed debug bounds (logical pixels) with no tolerance, per
  compared element id. Values compare at the replay's float32 precision: use geometry
  that is exact in float32 (integers, halves), or the comparison will rightly fail.
- **Tier B — the event transcript, exact.** The page's `[data-id]` elements listen for the
  fixture's `EventKinds`; the view's handlers append to an `EventLog`. Kinds, targets,
  order (including bubble order: target first, then ancestors) and the final
  `defaultPrevented` state must match exactly. The vocabulary is `mousedown`, `mouseup`,
  `click`, `contextmenu` (browser-core ticket 17), the pointer boundary events `pointerenter`/`pointerleave` (browser-core
  ticket 10) and the pointer events `pointerdown`, `pointermove`, `pointerup`,
  `gotpointercapture` and `lostpointercapture` (browser-core ticket 11); the boundary kinds arrive
  in Chromium's order — leave events innermost
  (child) first, enter events outermost (parent) first, all before the move that caused
  them — and a `Move` step to a point outside the viewport is the DOM-visible half of
  "the pointer left the window" (it fires the leave on the whole hovered chain); the
  WM_MOUSELEAVE half is covered by gpui's own window tests. While an element holds
  the pointer capture, the pointer kinds follow Chromium's capture rules
  (pointer_event_manager.cc): `gotpointercapture` fires when the pending capture
  processes at the next pointer event (before that event, bubbling from the captured
  element), the moves target the captured chain alone, and the release fires
  `lostpointercapture` after the ups with the boundary events settling after it
  (`fixtures/capture` is the fixture).
  The key kinds are `keydown` and `keyup` (browser-core ticket 09): key entries also carry the
  event's key (`e.key`, DOM key names — `Tab`, ` `, `ArrowDown`), the physical code and
  location (`e.code`/`e.location`, browser-core ticket 13 — `BracketRight`, `Numpad1`
  at 3), the focused element's id at listener time (`document.activeElement`, body
  normalized to `""`), which is the order probe for the default-action fixtures — a
  listener that still sees the old focus ran before the default action changed it — and
  the `isComposing` flag (browser-core ticket 18), which proves keydowns during an IME
  composition reach the listeners flagged, not dropped. The
  replay's key step is one full press — key down then key up, like the recorder's CDP
  `keyDown`+`keyUp` pair — and both halves' default-prevented states are recorded.
  The composition kinds are `compositionstart`, `compositionupdate` and
  `compositionend` (browser-core ticket 18): entries carry the event's `data` (the
  selected text a start replaces, the new composition string of an update, the
  committed text of an end, `""` for a cancel). They live on the input-handler seam,
  not the div listeners: the page's editable element receives the DOM events, and the
  view's handler implements `gpui.IMECompositionHandler` and logs through
  `log.AppendComposition` (`fixtures/imecompose` is the fixture).
  The editing kinds are `copy`, `cut`, `paste`, `beforeinput` and `select` (browser-core
  ticket 26): entries carry the event's `data` (the paste's clipboard text) and, for
  `beforeinput`, the `inputType` ("deleteByCut", "insertFromPaste"). They live on the
  input-handler seam too: the page's editable element receives the DOM events, and the
  view's handler implements `gpui.EditActionHandler` and logs through
  `log.AppendEditEvent`/`log.AppendBeforeInput` (`fixtures/editkeys` is the fixture
  set). One recording caveat: headless Chrome consumes Ctrl+C/X/V in the browser
  process before the renderer (the Edit-menu accelerators — no keydown reaches the
  page), so the editing fixtures record through the Windows alternates the renderer
  does deliver (Ctrl+Insert, Shift+Delete, Shift+Insert — the same editing commands);
  the letter bindings are pinned by gpui's unit tests.
  The drag-and-drop kinds are `dragstart`, `drag`, `dragenter`, `dragover`,
  `dragleave`, `drop` and `dragend` (browser-core ticket 16): dragenter, dragover,
  dragleave, drop and dragend entries carry the event's final `dropEffect`
  ("none", "copy", "move", "link" — the negotiated operation; a listener's
  assignment lands in the entry), and every drag entry carries its event's final
  default-prevented state (`defaultPrevented: true` is the target's acceptance —
  the recorder reads the page event's `dataTransfer` after the dispatch, so the
  port's drag events carry their final state and the replay settles the entries
  at the end through `log.FinalizeDragEntries`, since the drag dispatches run
  inside the platform events' dispatches and the per-step marking cannot see
  them). A drag entry's effect on a change move reads the shared-DataTransfer
  rule: a dragenter that fired with a dragleave reads "none" (the leave resets
  the transfer). Headless Chrome records an internal drag with plain CDP mouse
  events — the browser's drag controller simulates the drag loop for CDP input
  (`input_handler.cc` HandleMouseEvent) — and `parity.CancelDrag()` is the
  Escape step: the recorder drives `Input.cancelDragging` (the OS drag loop's
  DRAGDROP_S_CANCEL twin) and the replay dispatches one Escape keydown that the
  drag machinery consumes, so neither half records key entries
  (`fixtures/dnd` is the fixture set: the drag across three targets, the refused
  drop, and the Escape cancel).
- **Tier C — pixels, approximately.** The replay's final frame is rendered offscreen at
  device resolution and compared with the recorded Chrome screenshot under the fixture's
  provisional `PixelTolerance` (mean channel diff and hot-pixel fraction, image-wide and
  per compared box, so one wrong element cannot hide in the average; no masks — missing
  content is a failure). Every Tier C number is **provisional**: research 19's calibration
  (reference repeat noise, injected defects) has not run. `TestPixelTolerancesKeepHeadroom`
  pins each tolerance at ≥2× the measured correct-view statistics, so rasterization drift
  fails there first, with a recalibration instruction, instead of failing fixtures
  mysteriously.

## Writing a fixture

1. Create `internal/parity/fixtures/<yourfixture>/<yourfixture>.go` (package
   `<yourfixture>`), modeled on `fixtures/tracer/tracer.go`:

   ```go
   var Fixture = parity.Fixture{
       Name:       "my-fixture",            // the testdata directory name
       Window:     parity.Size{Width: 640, Height: 400},
       HTML:       page,                     // the whole HTML page, doctype on
       Steps:      []parity.Step{parity.Click(400, 230), parity.Key("enter")},
       BoxIDs:     []string{"box", "root"}, // the ids whose boxes are compared
       EventKinds: []string{"click", "keydown", "keyup"},
       View:       View,                     // func(*parity.EventLog) gpui.AnyElement
       Tolerance:  parity.PixelTolerance{...},
       SkipPixels: "",                      // non-empty: skip Tier C, see below
   }
   func init() { parity.Register(Fixture) }
   ```

   Fixtures that need the test window (focus handles, window flags, key bindings) set
   `ViewWithWindow func(log *parity.EventLog, w *gpui.Window, app *gpui.App)
   gpui.AnyElement` instead of `View` (exactly one of the two; the keyorder fixtures
   are the examples). Mutable per-fixture state — the focus handles — lives in the
   closure *outside* the returned function, keyed on the window: the view function
   re-runs every frame (new handles every frame would break the focus identity), and
   `Replay` can run the same fixture twice in one process (TestFixtures and the
   tolerance headroom check) with a fresh window whose handles must be fresh too.
   A fixture whose Tier C comparison cannot pass yet because of a known
   renderer/native defect sets `SkipPixels` to a reason naming the blocker
   (ticket and file:line) and the fix that clears it: Tier A (boxes) and
   Tier B (events) still compare, the Chrome record and the view stay checked
   in ready, and removing the flag is part of the blocking fix's acceptance.
   `internal/parity/fixtures/filters` is the example (native rev 15's
   colour-matrix cbuffer bug).

2. **The two sides must agree by construction.** Shared numbers as constants; the page's
   CSS uses the same values the view sets (the tracer pins `line-height: 1.618` because
   the pin's default line height is 1.618 until D1 lands — anything the two defaults
   disagree on must be pinned explicitly on one side). Fonts must resolve to the same
   files: the view uses the real text system, so `"Segoe UI"` in CSS and the default
   system UI font in gpui agree on this PC. **Images** go in as an embedded asset: check
   the PNG in beside the fixture, `//go:embed` it, and inject it into the HTML as a
   `data:` URI so the recorded `page.html` is self-contained (the object-fit fixture
   `internal/parity/fixtures/boxpaint` is the template; the gpui side builds the same
   image with `gpui.NewImage` from the embedded bytes). **Flex defaults:** the pin's
   default display is flex where the browser's is block — pin `display: flex` on the
   page's containers (and `flex-shrink: 0` where a child must overflow) so both sides
   place children identically.
   system UI font in gpui agree on this PC. The keyorder fixtures show the key-event
   rules: focusable elements need a `tabindex` on the page and
   `TrackFocus(handle).TabStop(true)` handles in the view (created once, outside the
   view closure, and reported with `log.TrackFocus(handle, id)` so key entries can carry
   the focus field); an element whose click the port does not yet deliver (an ancestor
   of a keyboard activation) narrows its listeners with `data-events="keydown,keyup"`
   on the page and matching handlers in the view; and the pages that do not want the
   browser's ring on their focusable elements pin `outline: none` on the page AND carry
   the pin's twin `.OutlineNone()` on the view's focusable boxes — the port draws
   Chrome's default focus ring on focused, focus-visible elements now (ticket 48), so
   a page without the pin must leave the ring in the view too.

3. **Element ids are the mapping.** Every compared or instrumented HTML element carries
   `data-id="<id>"`; the gpui elements set `.ID("<id>")` (interactive state, click
   handlers need it) and `.DebugSelector("<id>")` (the bounds key) with the same string.
   `BoxIDs` lists the compared ones; `EventKinds` lists the event kinds the page binds and
   the view logs (`log.MouseDown("box")`, `log.Click("box")`,
   `log.PointerEnter("box")`, `log.PointerLeave("box")`, the pointer kinds of
   ticket 11 — `log.PointerDown("box")`, `log.PointerMove("box")`,
   `log.PointerUp("box")`, `log.GotPointerCapture("box")`,
   `log.LostPointerCapture("box")` — and `log.KeyDown("box")`, `log.KeyUp("box")`,
   `log.KeyDownPreventDefault("box")`; one helper per kind). The key helpers record the
   event's key, physical code and location and the focus field, and the preventing one
   also calls `Window.PreventDefault`. The pointer boundary kinds need the element ids:
   the retained hover state that diffs enter/leave across frames keys on them. An
   element may narrow its bound kinds with a `data-events="keydown,keyup"` attribute (a
   comma-separated subset of `EventKinds` — the instrumentation honours it on the page
   side, and the view simply registers only the matching handlers).

4. **The input script is one vocabulary.** Steps are logical (CSS-pixel) positions:
   `parity.Move/Down/Up/Click/Key/Wait`, plus the IME composition steps of
   browser-core ticket 18: `parity.ImeSet(text, selStart, selEnd)` drives one
   composition update (the recorder's CDP `Input.imeSetComposition` — the page's real
   IME adapter — and the replay's simulated `WM_IME_COMPOSITION`, the composition
   string with the cursor at `selEnd`), and `parity.ImeCommit(text)` commits
   (`Input.insertText` / the simulated result string). A non-empty `ImeSet` over no
   session starts one, over an active session updates it, and an empty `ImeSet`
   cancels it; a commit over an active session ends it. The keyed steps never type
   text on either side (the recorder sends no `text` param and the replay dispatches
   only the platform events), so a key press mid-composition is pure transcript.
   Press and release steps carry a CLICK COUNT (browser-core ticket 19):
   `Step.Clicks` (0 = 1) becomes the CDP `clickCount` on the recorder side and the
   event's `ClickCount` on the replay side — the count a rapid press sequence
   reports, which the renderer's double/triple-click arms read (MouseEventManager
   hands SelectionController the event's click count; it does not count the presses
   itself). `parity.MultiClick(x, y, count)` is the sugar — the same
   move/press/release triple carrying the count — so a double click is a `Click`
   followed by `MultiClick(x, y, 2)`, a triple click a third `MultiClick(x, y, 3)`,
   and a double-click-then-drag spells the second press's halves out with
   `DownWithClicks`/`UpWithClicks` (`fixtures/selection` is the fixture set). The recorder dispatches them to Chrome as trusted
   input (full hit testing) and the replay simulates them through the visual test context
   — the exact same coordinates, the exact same expansion (`Click` = move + down + up on
   both sides). `Key("<keystroke>")` is one full press on both sides: the recorder sends
   a CDP `keyDown`+`keyUp` pair (with the typed-case `key`, so `"j"` reports `e.key`
   `"j"`) and the replay dispatches a `KeyDownEvent` then a `KeyUpEvent` through the
   platform dispatch — bindings, key listeners and the core default actions
   (browser-core ticket 09) all run, and a default action between the halves is visible
   in the keyup entries' focus field. `parity.KeyDown("<keystroke>")` and
   `parity.KeyUp("<keystroke>")` (browser-core ticket 12) are the halves alone: the
   recorder sends one CDP event and the replay dispatches one platform event, so a
   fixture can hold a key down at the record's settle — the Space-held `:active`
   fixture (`fixtures/activation`) is the use — or put something between the halves of
   a press.
   `Wait(ms)` advances time deterministically for mid-transition and mid-animation
   sampling (browser-core ticket 44): the recorder commits the pending style change,
   then pauses every animation the page runs and advances each one's `currentTime` by
   exactly the step's milliseconds (the Web Animations seek — a virtual-time budget
   turned out NOT to drive the CSS transition timeline in `--headless=new`: transitions
   created by a style change sit unstarted at `currentTime` 0 until a frame runs, and
   no frames are produced during a budget), **then sleeps the same milliseconds of
   real time** — the animations are already paused and seeked, so the sleep moves
   nothing the page shows, but wall time passing is what ends the browser's
   timer-driven input transactions (the wheel's 500ms scroll-latching transaction,
   browser-core ticket 15) — and the replay advances the test
   application's virtual clock (`TestApp.AdvanceClock`) and delivers one animation
   frame, so the last drawn frame samples the transitions at exactly `now + ms`. A
   later wait seeks the then-current animations, so a transition a later step started
   begins at 0 again — mid-flight interruptions (the reversing-shortening rule) read
   the paused progress. The `fixtures/transition` fixtures are the pattern.

   `parity.Wheel(x, y, deltaX, deltaY)` is the mouse-wheel step (browser-core ticket
   15): the deltas are logical (CSS) pixels in the platform's `ScrollWheelEvent`
   convention — **negative deltaY scrolls down** (WebKit's "positive deltaY means
   scroll up", `ui/events/blink/web_input_event_builders_win.cc:339`) — and the
   recorder dispatches CDP `Input.dispatchMouseEvent` type `mouseWheel` with them
   negated (CDP's params carry the DOM's signs; the DevTools input handler negates
   them back into the WebInputEvent, `input_handler.cc:472-473`), which scrolls the
   page exactly `|delta|` CSS pixels (measured at the 2.0 device scale). The replay
   dispatches one `ScrollWheelEvent` with the pixel delta through the platform
   dispatch, then a zero-delta phase-`Ended` `ScrollWheelEvent` that ends the port's
   wheel gesture — the pairing mirrors the DevTools handler, which stamps **every**
   synthetic CDP wheel `kPhaseBegan` (`input_handler.cc:490`), making each CDP wheel a
   complete gesture of its own (`mouse_wheel_phase_handler.cc`'s
   `DispatchPendingWheelEndEvent` ends the previous transaction). A run of `Wheel`
   steps without a `Wait` between them is therefore a run of separate gestures on
   both sides, one latch each; a `Wait(600)` separates them for the story too. The
   wheel's latching rules themselves — the innermost scroller that can consume
   claims the gesture, the 500ms latching transaction, overscroll-behavior
   containment — are what `fixtures/wheel` records.

   A key step produces the keystroke the platform reports (browser-core ticket 13): the
   replay parses the keystroke and normalizes it with
   `gpui.Keystroke.WithSimulatedCode()` — the physical code from the US layout table
   (or an explicit one), the location the code implies, and the shifted-key fold with
   Shift kept (`shift-]` carries `Key "}"`, `Code "BracketRight"`) — and the recorder
   sends the same set to Chrome (see the recorder details below).
   `parity.KeyCode("1", "Numpad1")` spells a physical key the keystroke syntax
   cannot: the key and modifiers parse as usual, both sides carry the code, and the
   location follows it (`Numpad1` at 3, the numpad).

   `parity.Script(js, run)` is a page-script step (browser-core ticket 46): the
   recorder evaluates `js` (a JavaScript expression, run synchronously on the
   settled page after the preceding steps) and the replay calls `run` with the
   fixture's event log, the test window and its application, inside an update —
   a scripted state change neither the input vocabulary nor the clock can express
   (rows inserted above the scrolled viewport, the scroll-anchoring fixture's
   trigger). The two halves must make the SAME change: the page script mutates
   the DOM, the Go function mutates the view's state (keyed per run like the
   view's other state) and refreshes the window, and the record's two-frame
   settle captures whatever both sides did — Chrome's own follow-ups (a layout
   pass, the scroll anchoring adjustment) land in the settle. Run must be
   idempotent across the replays of one fixture (`TestFixtures` and the tolerance
   check run the steps twice): key the state on the event log or the window so a
   fresh run starts from the initial state, or make the step reset-then-mutate.
   `fixtures/anchoring` is the pattern.

   The key kinds cover the activation and focus-visible rules (browser-core
   ticket 12, `fixtures/activation`): an Enter full press records the activation click
   between the keydown and keyup entries (the DOM keypress — the recorder types `"\r"`
   so the keypress exists), a Space `KeyDown`-only step records the held `:active`
   state at the settle, and the focus-visible fixtures pair a click step with key
   steps to prove the focus-origin rule.

5. **The view function re-runs every frame** (like any `Render`): build the tree fresh on
   each call, keep mutable state outside (the `EventLog` is passed in; anything else goes
   in the fixture's closure or an entity).

6. Import the fixture package in `internal/parity/replay_test.go`'s import block (blank
   import) so `TestFixtures` picks it up, and in `cmd/parityrecord/main.go`.

   A fixture with no instrumented events (a pure painting fixture) leaves `EventKinds`
   and `Steps` empty: the recorder's instrumentation binds nothing and both transcripts
   are empty, which the Tier B comparison accepts. The `BoxIDs` are then the whole
   fixture; `internal/parity/fixtures/boxpaint` records that way.

   A fixture that needs the browser in a different state (a different text AA mode, a
   color profile) sets `ChromeFlags`: the recorder appends them to its own flags, so
   one page can be recorded under several browser configurations as several fixtures —
   the `textpaint` rasterization fixtures are the pattern (`textpaint-raster` with the
   default flags, `textpaint-raster-gray` with `--disable-lcd-text`). The flags are
   record-time only; the replay never reads them, but the record they produced is what
   the replay is compared against, so copy them when cloning a fixture. A smooth-
   scrolling fixture (browser-core ticket 45, spec D13) sets `SmoothScrolling: true`
   instead: the recorder adds `--enable-smooth-scrolling` (which wins over the
   harness's own disable flag) and the replay opts the test application's smooth
   scrolling in — unlike `ChromeFlags`, this one IS read at replay time, because the
   record was made under it (`fixtures/smoothscroll` is the fixture set; see the
   smooth-scrolling note under "Recorder details worth knowing" for why its records
   pin end states and the mid-flight curve lives in the port-side tests).

   A fixture that needs the page rendered under a **media feature** sets `EmulatedMedia`
   (CDP `Emulation.setEmulatedMedia`, applied before the page loads): the
   forced-colours fixture (`internal/parity/fixtures/forcedcolors`) records with
   `forced-colors: active`, so Chrome's forced-colors mode maps the page's colours
   onto the system palette during style resolution and the record carries the mapped
   result. Like `ChromeFlags`, the features are record-time only and part of the
   fixture's identity.

   A fixture whose device pixel ratio is **not the default 2.0** sets `Scale`
   (browser-core ticket 07): the recorder forces Chrome to it
   (`Emulation.setDeviceMetricsOverride`) and the replay opens the test window at it
   (`gpui.NewTestWindowAtScale`), so one coordinate space still serves the fixture -
   boxes compare in logical pixels and pixels at the fixture's device resolution,
   exactly like the default. This is how the border fixtures pin the device-pixel
   rounding rules at fractional scales: `fixtures/scrollrange` records `border-175`
   at 1.75 and `border-hairline` at 1.0. Pick geometry whose device edges are
   integral at the fixture's scale (multiples of 4 work at 1.75) or the port's
   device-edge bounds snapping lands on different pixels than Chrome's LayoutUnit
   layout and Tier A fails for reasons unrelated to the behaviour under test. The
   window's device extent (logical x Scale) must still fit the capture budget.

   A fixture whose replay needs **page-computed values** sets `ComputedScript` (a
   JavaScript expression evaluated on the settled page whose value must be a JSON
   object of strings) and reads them from `Record.Computed`. The forced-colours
   fixture is the pattern: Chrome's emulated forced-colours palette is not this PC's
   Windows palette, so the page carries hidden `[data-color-probe]` elements each
   authoring one CSS system-colour keyword (a system colour passes through forced
   colors unforced), the script reads their computed colours into the record, and the
   replay injects exactly those colours — deterministic, theme-independent.

   A fixture whose **view resolves styles against app-level state** sets `Configure
   func(app *gpui.App) error`: `Replay` runs it on the application right after
   creating it, before the window and the view, so the first frame already resolves
   under it. The forced-colours fixture pins the pass on with the recorded palette
   (`gpui.App.SetForcedColors`); the fixture value is copied wholesale when a test
   swaps the View, so the configuration flows into the discrimination replays too —
   a wrong-view test that needs it off swaps in a no-op.

7. Record, from the module root:

   ```sh
   go run ./cmd/parityrecord -fixture my-fixture
   ```

   The files land in `internal/parity/testdata/my-fixture/` (`record.json` with the
   Chromium version, boxes, event log; `chrome.png`; `page.html` for reading). Check them
   in. Re-record when the page, the steps, the ids or Chrome change; the record's
   `recordedAt`/`chrome` fields are the provenance.

8. Run the replay (the whole harness):

   ```sh
   go test ./internal/parity/... -run 'TestFixtures'
   ```

   Tune the `Tolerance` with the measured numbers: temporarily add a
   `parity.MeasurePixels` call (or run `TestPixelTolerancesKeepHeadroom -v`) to see the
   correct view's mean/hot statistics, and set each gate at ~2× those. Add a
   deliberately-wrong view that must fail the gate (the tracer's
   `TestWrongPaddingFailsOnlyThePixelComparison` is the pattern) and keep it as a test —
   a tolerance that a real defect passes is worthless.

   For a **text-heavy** fixture, calibrate from the ink statistics, not the diff alone:
   `parity.MeasureInk(img, rect, fgHex, bgHex)` (textmeasure.go) reports the mean ink
   (total coverage), the ink/full/partial pixel fractions, the partial pixels' edge
   darkness, the 16-bucket coverage histogram, and the channel spread that tells
   grayscale edges (0) from LCD fringes — on either the recorded screenshot or the
   replay capture (`parity.ROIOfDevice` turns a recorded box into the device rectangle
   at the replay's scale). Two text rasters can differ per pixel (ClearType fringe
   phase) while carrying the same ink, so the ink numbers are what a text tolerance
   means. The `textpaint` fixtures are the pattern; the probes under
   `research/browser-parity/08-raster-probes` are the worked example.

### Discrimination tests

Keep the deliberately-wrong views as tests next to the fixture
(`fixtures/tracer/tracer_test.go` is the template): one per tier, each asserting the
target comparison fails **with a readable diff** and — where the wrong view renders
identically — that the other comparisons still pass. That is what makes a failure mean
the thing it says.

## How to run things

```sh
# Record every registered fixture (Chrome must be installed; GPUI_PARITY_CHROME
# overrides the path):
go run ./cmd/parityrecord

# The whole harness: boxes, transcript and pixels for every fixture:
go test ./internal/parity/...

# Only the tracer's end-to-end replay:
go test ./internal/parity -run 'TestFixtures/tracer-box-click' -v
```

The recorder never shows a window (`--headless`, a throwaway profile, its DevTools port
picked free; the browser tree is killed when the recording ends) and never touches the
user's browser profile. The replay is headless and offscreen only: the gpui view runs in
the hostless test window (deterministic virtual clock, simulated input), and the pixel
capture renders that window's scene through a hidden, never-shown window placed off
screen. `GPUI_REAL_INPUT_TESTS` is never set by anything here.

## Design notes

- **Device pixel ratio.** The test window's scale factor defaults to 2.0, so Chrome
  records at `--force-device-scale-factor` 2 and the replay asserts the window's scale
  (`parity.DeviceScaleFactor`). A fixture may override the ratio with `Scale`
  (browser-core ticket 07): both sides then run at the fixture's ratio instead, and
  the replay asserts that. Boxes compare in logical pixels; pixels compare at device
  resolution; both sides' numbers are the same because the ratios are.
- **Window size budget.** The pixel capture renders through a real (hidden) window
  surface, whose client area cannot exceed the primary monitor: a fixture's device extent
  (logical × its scale) must fit the screen minus the caption (on a 1080p display at
  100%: up to ~1920×1060). The tracer's 640×400 → 1280×800 keeps a wide margin.
- **Chrome version.** Every record carries the Chromium version it was captured with
  (`record.json` → `chrome`). Fixtures are machine-and-version-stable: re-record when
  Chrome updates meaningfully.
- **Tier C defaultPrevented.** The recorder reads each DOM event's final
  `defaultPrevented`; the replay marks the entries a dispatched event produced with the
  dispatch result's `DefaultPrevented` (`Window.PreventDefault`). Both sides report the
  post-dispatch state.
- **TSX twin fixtures (seam 3).** `fixtures/tsxtracer` is the tracer's page written as a
  `.tsx` view run through the script host (`gpui/script`, ticket 62): it reuses the
  tracer's page, steps, ids, kinds and tolerance, and is **not** registered — it has no
  record of its own, because it is compared against the tracer's existing record
  (`tsxtracer_test.go` runs the three comparisons). This is the shape for script-side
  fixtures: the same Chrome page, one more implementation of the view. A wrong `.tsx`
  view must fail the comparison (the package carries that discrimination test too).

## Known divergences the harness works around today

These are gpui-go (pin) behaviours that differ from the browser; the fixtures avoid them
until the owning spec tickets land, and each should get a fixture the day it is fixed:

- **No default focus ring** — FIXED by ticket 48 (the outline ticket, spec story 42):
  a focused, focus-visible element now draws the default ring — one 2px `#101010` stroke
  centred on the border box with 3px-rounded corners, the look the recorded stable Chrome
  155 draws (`fixtures/outline` is the fixture set). Two notes from the landing: the pages
  that pin `outline: none` on focusable elements (keyorder, activation, tabnav, contextmenu,
  imecompose, keycode) now carry the pin's port twin — `.OutlineNone()` on the view's
  focusable boxes, so their records stay valid unchanged; and Chromium main has since moved
  to a two-tone ring (`#E59700` under white on light, outline_painter.cc's PaintFocusRing),
  recorded as the port's divergence toward the record — re-record when the recorder's Chrome
  updates past the refresh.
- **Default line height is 1.618** (CSS `normal` comes with D1): the tracer's page pins
  `line-height: 1.618`.
- Text rasterization differs in mechanism (Skia's sRGB preblend vs DirectWrite
  params + the pin's shader), but the ticket-08 A/B
  (research/browser-parity/08-raster-probes) measured the ink matching within ~5%, so
  what Tier C's tolerance absorbs is fringe-phase and mode noise, not a rasterization
  gap; the measured mode divergences (no grayscale window mode; ClearType kept above
  Chrome's 48-device-px LCD cap) are tracked by report 04 §2(b) and Q21. Text-heavy
  fixtures calibrate from the ink statistics (`parity.MeasureInk`).

Divergences fixed by browser-core tickets get their fixture when they land: ticket 10
(hover) removed two pin behaviours — hover clearing on key presses (D12) and hover
sticking at the window edge — and added `OnPointerEnter`/`OnPointerLeave` plus the
`pointerenter`/`pointerleave` transcript kinds (`fixtures/hover` is the fixture);
ticket 13 (key code and location, with Shift kept) made the port's key events carry
the DOM's key/code/location pair, so the keyorder fixtures were re-recorded with the
code field and the keycode fixtures landed (`fixtures/keycode`); and ticket 12 (click,
activation and focus-visible, D12) closed four of this list's divergences at once, with
`fixtures/activation` as the fixtures:

- **`OnClick` fires before the `OnMouseUp` listeners** during the up dispatch (the DOM
  delivers `mouseup` before `click`). FIXED by ticket 12: the click now fires after
  the whole up dispatch (and after `lostpointercapture` under a capture), at the
  common ancestor of the press and release targets —
  `activation-click-ancestor` instruments `mousedown`, `mouseup` and `click`
  together, so the transcript pins the browser's order. The tracer still instruments
  `mousedown` and `click` only (a fixture's shape is its own choice).
- **A mouse down on a focusable element reports `defaultPrevented`** — the pin's focus
  transfer always called `Window.PreventDefault` after focusing (interact.go, the
  div.rs:3279 rule that a focusable parent must not steal focus), where Chrome leaves
  the mousedown unprevented. FIXED by ticket 12: the innermost-wins signal between
  focusable elements is a window-internal flag now, and the focus is recorded as a
  mouse-origin focus — `activation-enter` and the `focus-visible-*` fixtures instrument
  `mousedown`, and both sides report it unprevented. The keyorder fixtures still bind
  no `mousedown` listeners (their pages predate the fix; their records stay valid).
- **A keyboard activation click does not bubble** to ancestors (the pin called only the
  focused element's own click listeners; Chrome dispatches a full bubbling click).
  FIXED by ticket 12: the activation click now walks the focused element's whole
  ancestor path — `activation-enter` and `activation-space-release` instrument root's
  click and both clicks bubble. The keyorder-activate fixture's root still binds
  `data-events="keydown,keyup"` (its record was made that way; the shape stays).
- **Enter activates on keyup, not keypress** (the pin; Chrome clicks at keypress, so
  an Enter without CDP `text` produces no keypress and no click at all). FIXED by
  ticket 12: the recorder types `"\r"` on the Enter keyDown so the keypress exists,
  and the port's Enter default action fires the click between the keydown and keyup
  — `activation-enter` is the fixture. Space keeps its own shape (`:active` while
  held, click at the keyup), pinned by `activation-space-held` and
  `activation-space-release`.

The click-under-capture order (`pointerup`, `lostpointercapture`, `click`, boundary
settling) that the capture fixture avoided is also ticket 12's now: the port delivers
it, and the capture ticket's own tests pin it
(`TestPointerCaptureRetargetsEventsToTheCapturedChain`).

## Recorder details worth knowing

- **The record settles before it is read**: after the scripted steps, the
  recorder waits two animation frames before reading the transcript, the boxes
  and the screenshot, so the state a record captures is the settled post-input
  state — the last input's rendering work (a scroll's compositor update, an
  anchored element's recalculation, an intersection observer's
  `position-visibility` delivery) has landed, exactly like the replay, whose
  boxes come from a fully parked final frame (ticket 25 found the un-settled
  read capturing a scroll one key behind).
- **Smooth scrolling is off in the recorder's Chrome by default, and a
  fixture can turn it on** (`SmoothScrolling: true`, browser-core ticket
  45): the harness's own `--disable-smooth-scrolling` keeps every
  pre-45 fixture's scrolls instant on both sides (Chrome's flag pair
  ORs, web_contents_impl.cc:4042-4046, so the fixture's
  `--enable-smooth-scrolling` wins over it when set), and the replay's
  TestApp mirrors the split — the deterministic test application runs
  with the port's smooth scrolling off (the recorder's Chrome shape;
  `TestApp.WithSmoothScrolling` opts in) while a real application's
  default is on (spec D13). The smooth fixtures (`fixtures/smoothscroll`)
  record with the flag on and replay with the port's smooth scrolling
  on. One hard limit, worked around: Chrome's smooth scroll animations
  run on the compositor as impl-only cc animations — they are not
  document.getAnimations() Web Animations, so the Wait step cannot seek
  them — and CDP-dispatched wheel events are `kScrollByPrecisePixel`
  (input_handler.cc:484), which never animates, so no timed mid-animation
  scroll offset can be recorded from Chrome. The smooth records
  therefore pin the END states (the convergence and the step sizes),
  the keyboard path being the recordable non-precise source, and the
  mid-flight curve is pinned on the port side: the fixture package's
  own mid-animation replay (the offset at exactly half the 150ms
  keyboard animation is the ease-in-out midpoint, 20px of the 40px step)
  and the gpui unit tests that fit Chromium's curve formulas
  (`gpui/scroll_smooth_test.go`).
- **Keyboard scrolls land synchronously** with the flag off (measured at the harness's
  2.0 device scale: 40 CSS px per arrow — Chromium's kPixelsPerLineStep — the page step
  max(0.875xlength, length-40), Home/End to the axis ends): the keyorder scroll
  fixtures pin those numbers against the port's keyboard-scroll default action.
- **Key events are synthesized with the whole DOM identity** (ticket 13,
  `cdpKeyEvents` in internal/parity/recorder.go): `Input.dispatchKeyEvent` builds the
  DOM event from what it is given, so the recorder must send every piece — verified
  against `content/browser/devtools/protocol/input_handler.cc` and the recorded
  fixtures (Chrome 155). The `key` param becomes `e.key` verbatim (the recorder sends
  the platform-folded DOM key, so `shift-]` reports `"}"`); the `code` param becomes
  `e.code` (`CodeStringToDomCode`); the `location` param (1 left, 2 right) and the
  `isKeypad` flag become `e.location` through the modifier bits (`GetEventModifiers`
  maps them to `kIsLeft`/`kIsRight`/`kIsKeyPad`); the `modifiers` bits are Alt=1,
  Ctrl=2, Meta=4, Shift=8 (translated onto the WebInputEvent flags); and
  `nativeVirtualKeyCode` must be set, or the event is marked skip-if-unhandled. The
  US virtual key codes come from the recorder's own `cdpVKOfCode` table (the one piece
  the keystroke spelling cannot carry).
- **A shifted letter still reports the pin's lowercase key** on both sides: the
  platform reports `Key "a"` for Shift+A (the pin's shape; the DOM key would be
  `"A"`), and `WithSimulatedCode` folds only the shiftable OEM and digit keys to
  match the platform — so a key fixture must not press Shift with a letter and expect
  the DOM `key` name (the `keycode` fixtures use shifted punctuation instead; the
  shift-folding of letters would be a separate ticket).
- **Enter carries `text: "\r"` on its CDP keyDown** (ticket 12,
  `cdpKeyEvents`): `Input.dispatchKeyEvent` only generates the keypress — the event
  the DOM's Enter activation rides (`html_element.cc:4318-4324`, between keydown and
  keyup) — when the keyDown's text is non-empty
  (`keyboard_event_manager.cc:384-386`), and a real Enter press types `"\r"`. Without
  it, a recorded Enter produces no keypress and no activation at all; with it, the
  activation-enter record carries the click between the keydown and keyup entries.
ticket 11 (pointer capture) added the pointer-event and capture transcript kinds
(`fixtures/capture`), the capture retargeting behind the existing
`Window.CapturePointer` API (pending capture, got/lost callbacks, a clean cancel on
WM_CAPTURECHANGED) and the element-local position
(`MouseDownEvent`/`MouseUpEvent`/`MouseMoveEvent.LocalPosition`). The capture fixture
still avoids instrumenting `click` (its record predates ticket 12); the
click-after-lostpointercapture order it avoided is now the port's behaviour, pinned by
ticket 12's fixtures and the capture ticket's own transcript tests.

Ticket 12 (click, activation and focus-visible, D12) added the common-ancestor click
(`gpui/mouseevent.go` `dispatchMouseClick` — the press and release hit-test chains'
deepest shared element, the captured chain under a pointer capture, fired after the
up's listeners and the capture release), the keyboard activation defaults
(`gpui/key_default_action.go` — Enter clicks at the keydown, Space holds `:active`
through `Active` styles and clicks at the keyup, both bubbling through the focused
element's path), the mouse-origin focus recording (`gpui/focus.go`
`focusWithOrigin`) with the unmodified-key `:focus-visible` rule
(`gpui/interact.go` `focusVisibleMatches`), and the key half-press steps
(`parity.KeyDown`/`parity.KeyUp`). `fixtures/activation` is the fixture set.

Ticket 33 (position: static, the opt-in) added `PositionStatic` and the
containing-block scope walk (`gpui/containingblock.go`): an absolute child of a
static parent defers to the nearest positioned ancestor and Taffy then places it
against that ancestor's padding box, a static chain defers up to the window root
(the port's initial-containing-block stand-in), and the pin's direct-parent rule
survives untouched for everything that never says `Static()` — the control case
in `fixtures/position` pins that, and the fixture's wrong view (every static
parent left relative) is the pre-ticket pin, so its Tier A failure is the
regression gate. Custom container elements join the rule through
`gpui.RequestContainerLayout`; `RequestElementLayout` keeps the plain attach.

Ticket 32 (text min-content, spec D2) made every measured text path wrap at
every break opportunity under a min-content available width — the Text element's
measured leaf (`gpui/div.go`), the inline paragraph of a Block div
(`gpui/inline.go`) and StyledText (`gpui/styled_text.go`) — so text in a flex
row shrinks and wraps like Chromium (min-content is the widest unbreakable
segment, `InlineNode::ComputeMinMaxSizes`' zero-available-size line breaking),
and it keyed each text measure cache by sizing mode: a min-content entry answers
min-content queries only, and an unconstrained (max-content) query reuses only
an unconstrained entry — the pin's rule let a definite-width shape answer a
max-content query with its wrapped size, which the flex base-size pass then took
as the item's max-content. `fixtures/mincontent` is the fixture set (all three
measured paths, the flex-shrink case, the long unbreakable word); its wrong view
(`WhitespaceNowwrap` everywhere) is the pre-ticket pin. No existing fixture
changed: they all pin fixed sizes or `flex-shrink: 0` on children that must
overflow, so no text in them ever shrank.

Ticket 14 (core Tab navigation, focus fixup and Inert, D4) gave the Tab default
the HTML sequential order (`gpui/focus.go` `TabStopMap` — positive tab indexes
ascending, then the zero bucket in tree order; a negative index is never a stop,
and a focused excluded element's search runs in tree order, Chromium
`focus_controller.cc:1347-1443`), the pass-out boundary (Tab past the last stop
blurs — Chrome hands focus to the browser UI at the document scope's end, which
headless records as the body — and the sequential focus navigation starting
point keeps the removed element's position, `gpui/focus.go`
`fixupFocusedElement`/`focusSearchStart`), the end-of-frame focus fixup
(removed, hidden or inert focused elements blur, `gpui/helpers_frame.go`
`finishFrame`), the mouse-focus default with no focusable press target
(`gpui/mouseevent.go` `defaultMouseFocus` — the flat-tree ancestor walk of
`HandleMouseFocus`, so an occluding popup does not eat its host's focus, and a
press on empty space blurs), and the `Inert()` style (`gpui/div.go` — the
subtree's registrations are suppressed, its hitboxes are skipped by the hit test
as pointer-events: none, and its accessibility nodes are hidden; the a11y half
is pinned by `gpui/div_a11y_test.go` `TestA11yDivInertHidesTheSubtree` since the
harness cannot see the tree). Two pin behaviours closed with it, both recorded
as divergences toward Chrome: key events with focus on the body now reach the
root element's listeners (`gpui/key_dispatch.go` `focusDispatchPath`'s
`bodyNodeID` — the pin dropped them on the tree root alone), and
`Window.FocusNext`/`FocusPrev` no longer wrap (the pass-out blur above; Base's
questionnaire tests pass because the fixup only answers removal, never a
state-only handle). `fixtures/tabnav` is the fixture set, with one recorded
quirk: a second forward pass-out from the same last element lands
nondeterministically in headless Chrome, so `tabnav-order` steps each boundary
exactly once.

Ticket 17 (the contextmenu event, story 60) added `DivElement.OnContextMenu`
(`gpui/contextmenu.go`): the right-button release dispatches a bubbling
contextmenu after the whole up tail — the listeners, the capture release, the
click/auxclick, the boundary settling — regardless of the click having been
swallowed, targeted at the element under the release; the unmodified Menu key
fires it on key UP and Shift+F10 on key DOWN (Windows' arms,
`web_frame_widget_impl.cc:1112-1141`), after the key listeners and only when
the event was not canceled, targeted at the focused element's chain with the
menu position at the focused element's centre. The keyboard registration
lives on the dispatch node (like the keyboard click) and the mouse one on the
frame's path-keyed registrations (like the click listeners); the harness's
`contextmenu` kind and `log.ContextMenu`/`log.ContextMenuPreventDefault`
helpers record it. One recorder fix landed with it: a key step without text
now sends the CDP `rawKeyDown` type (the shape a real Windows key press
takes) instead of `keyDown` — Chrome's Shift+F10 arm requires kRawKeyDown
(`web_frame_widget_impl.cc:1112`), so the untextured `keyDown` type never
triggered it; the transcripts of the existing key fixtures are unchanged by
the switch (verified by re-recording `tabnav-order`). The synthesized
follow-up shares the triggering dispatch's default-prevented flag — the
mouse-up's or the key-up's — so the prevented record binds only the
`contextmenu` kind (the coarseness the activation click already documents).
`fixtures/contextmenu` is the fixture set (mouse, menu-key, shift-f10 and the
prevented pair whose Tab-after-menu pins "suppresses nothing else").

Ticket 21 (the top layer and light dismiss) added the `toggle` transcript
kind (the DOM popover toggle event, `log.Toggle`, with the entry's
`state` field carrying `newState`) and the popover fixtures
(`fixtures/popover`): nested popovers whose child opens from an invoker
inside the parent, with steps clicking inside the child, the parent, the
invoker and the empty page — the recorded transcripts pin Chromium's
click-based light dismiss (the press/release pair rule, the invoker link
and the hide-everything-above walk; `mouse_event_manager.cc:262-267`,
`html_element.cc:3141-3227`). The pages reset the popover UA stylesheet
(Chrome centers `[popover]` with `inset: 0; margin: auto;
width: fit-content`, html.css:1966-1977) and record the popover
`::backdrop`'s own rule: it is `pointer-events: none` (html.css:
2012-2015), so a click on the scrim region falls through to the page and
light-dismisses the whole layer — the port's plain backdrop div matches
it without a hitbox.

Ticket 26 (edit actions and the HTML clipboard) added the core's editing commands
(`gpui/edit_action.go` — Copy/Cut/Paste/SelectAll as the actions `edit::Copy` &c., the
Windows-Chrome key table of `editing_behavior.cc:168-170,174-178` mapped in
`editCommandOfKeystroke`, and the default-action wiring that runs them as the FIRST arm
of `runKeyDefaultActions`, Chromium's editor-commands-before-everything order
`keyboard_event_manager.cc:442-445`): the commands dispatch to the focused input
handler's optional `gpui.EditActionHandler` seam, firing the DOM's copy/cut/paste/
beforeinput/select events in the recorded order and performing the mutations through
the base/IME interfaces — cut writes then fires beforeinput(deleteByCut) then deletes;
paste reads, fires paste (carrying the item), fires beforeinput(insertFromPaste) then
inserts; the clipboard events fire regardless of the selection's state (the record's
collapsed-caret shape) with the write and mutation gating on it. The clipboard layer
gained the CF_HTML exchange (`gpui/cfhtml_windows.go`, Chromium's HtmlToCFHtml/
CFHtmlToHTML with the header offsets, published as the registered "HTML Format"),
the `ClipboardString.HTML` companion, and the clipboard-change observer
(`App.OnClipboardChange` — AddClipboardFormatListener on the host's platform window,
WM_CLIPBOARDUPDATE). The busy clipboard now retries OpenClipboard 5 times with a 5 ms
backoff (Chromium's ScopedClipboard::Acquire, `clipboard_win.cc:102-130`) before the
typed Denied error names the attempts — a recorded divergence from the pin's single
immediate attempt. `fixtures/editkeys` is the fixture set: `editkeys` (a disabled copy,
select, copy, cut through the Windows alternates) and `editkeys-paste` (the paste
command's event pair); the Ctrl+C/X/V letter bindings cannot be recorded (headless
Chrome consumes them pre-renderer), so gpui's `TestEditCommandKeyMapping` pins them,
and the paste's real clipboard half is pinned by internal/clipspec's
`TestClipboardPasteEditActionRoundTrip` (the ticket's fallback for headless-clipboard
unreliability). Base keeps its own input bindings (they win over the core defaults
through the binding position); the core actions add the alternate keys to Base inputs
as browser-parity wins (Ctrl+Insert copies like Chrome's, where the pin's keymap had
no binding).

Ticket 38 (white-space and tracking) made the element layer collapse white
space like CSS before shaping, keeping an offset map (`gpui/white_space.go`):
normal, nowrap and pre-line fold runs of spaces, tabs, newlines and carriage
returns to one space (removed at a paragraph's start or end, and across inline
element boundaries), pre-line preserves the newlines as breaks, and pre/
pre-wrap keep everything — tabs included, which advance to the browser's
8-space stops (`gpui/text_tabs.go`, keyed by `TextLayoutRequest.TabSizeSpaces`
through the shape cache). The Text leaf, the inline paragraph and StyledText
all collapse; StyledText's `PositionForIndex`/`IndexForPosition` map through
the byte map, so carets and selection keep answering in source offsets; a
paragraph that collapses away entirely measures no line box, like Chromium's
self-collapsing empty line. Base's TextView opts out with pre-wrap (its text
is the pin's verbatim rich text — `base/text_inline.go`), so no Base behaviour
changed. `fixtures/whitespace` is the fixture pair (`whitespace-collapse`,
`whitespace-pre`); the wrong view (everything pre) is the pre-ticket pin.
No existing fixture changed.

Ticket 48 (the outline and the default focus ring, story 42) added the CSS
outline to the Style (`gpui/style.go` `OutlineStyle`: width, kind, colour,
offset — a frame painted ABOVE the content as the element's last decoration,
the outline phase after the foreground, `paint_phase.h:46-47`) and the UA
focus rule's materialization (`gpui/interact.go` `computeStyleInternal`:
outline-style: auto — the CSS initial value — becomes the ring kind on a
focused, focus-visible element and none everywhere else, html.css:1701-1703).
The ring is one 2px stroke centred on the border box with the element radii
plus the offset floored at 3 (outline_painter.cc's FocusRingStrokeWidth 3,
FocusRingOuterStrokeWidth 2, GetFocusRingCornerRadii), in the outline colour
or the default #101010 the recorded Chrome 155 resolves for
-webkit-focus-ring-color (web_view_impl.cc:3836's renderer-preference
override); Chromium main has since refreshed it to a two-tone ring
(#E59700 under white on light) — a recorded divergence toward the record, to
re-record when the recorder's Chrome updates. `outline-none` opts out per
element, a click focus draws nothing (12's focus origin), and the forced
pass maps the ring colour to Highlight (forced_colors.css:18-19).
`fixtures/outline` is the fixture set (offsets positive/zero/negative,
widths, colours, a rounded border, the dashed pattern, the Tab-focused ring
vs the click-focused no-ring vs the outline-none opt-out); the fixture views
whose pages pin `outline: none` gained the pin's port twin (`.OutlineNone()`)
with no record change, and dashed, dotted, double and the rest beyond solid
stay unportable through the scene's two border styles (a recorded
limitation).

Ticket 15 (wheel latching, overscroll-behavior and the keyboard scroll target) added
the `Wheel` step above and `fixtures/wheel`: one wheel gesture latches to the
innermost scroller that can consume it (the whole delta goes to one scroller — the
pre-ticket port scrolled nested scrollers together, the ticket's spec risk), the
latched gesture does not chain when its scroller hits the end, a new gesture after
the 500ms latching transaction chains to the outer scroller,
`overscroll-behavior: contain` stops the chaining on both the wheel and the keyboard
walks, and the keyboard arrows scroll the focused chain — or, with nothing focused,
the mouse-press node's chain (`LogicalScroll`'s fallback order,
`core/input/scroll_manager.cc:216-232`). One collateral: ticket 07's
`scrollrange` discrimination test's wrong view gained the end-padding drop, because
the press-node keyboard scroll now scrolls the scroller even when the fixture's
click misses the focusable row (the wrong view's old premise), and the top-padding
basis cancels at the end scroll — the end padding does not.
as browser-parity wins (Ctrl+Insert copies like Chrome's, where the pin's keymap had
no binding).

Ticket 22 (close watchers and the Escape default action) added the close-watcher
stack (`gpui/closewatcher.go` — the DOM CloseWatcher: `NewCloseWatcher`,
`RequestClose`/`Close`/`Destroy`, `OnCancel` with a preventable
`CloseWatcherEvent`, `OnClose`; the Escape default action signals the NEWEST
watcher group newest-first, stopping at a prevented cancel,
`close_watcher.cc:195-215`, routed from the keydown default chain after Tab
`keyboard_event_manager.cc:454-464` → `DefaultEscapeEventHandler` :618-630), the
user-activation grouping (every key down or mouse press notifies the stack at
the head of the input dispatch; a watcher created after an interaction gets its
own group, a programmatic one joins the newest, so nested popovers close
newest-first on repeated Escape), and the TopLayer registration (the show steps
create each entry's watcher and the hide steps destroy it,
`html_element.cc:2146-2157`/`:2834-2836`; manual popovers create none — the kind
correction runs at the entry's first registration, since `Show` precedes the
element's render). `fixtures/closewatcher` is the fixture set
(`closewatcher-escape`, `closewatcher-hint`, `closewatcher-prevented`), and it
found two harness facts worth knowing: the recorder's CDP **key commands do not
pump the renderer's task loop while its mouse commands do** — the popover
toggle events are queued DOM tasks, so a key step that closes a popover leaves
its toggle queued in Chrome (a later transition coalesces it through the toggle
task tracker, and it fires only at the record's settle) where the port's
end-of-dispatch drain delivers it right after the causing dispatch; the
fixtures insert a listener-less pointer **Move as a task-loop pump** between
the clicks and the Escapes so both sides deliver the toggles at the same
positions. And the port's **occluding popover hitbox cuts the mouse propagation
chain at the popover** (`Occlude()` ends the hit-test walk, so an ancestor's
`OnMouseDown` never fires for clicks inside an overlay), where the DOM's body
still sees the bubbled mouse events — the closewatcher pages narrow the root to
`data-events="keydown"` (the popover fixtures' pages simply bind no root mouse
listeners); the recorded divergence stays with ticket 12's follow-ups.
and the anchor's hitbox record for the mouse-focus default's ancestor
walk). The discrimination view keeps Tier A passing.

Ticket 35 (position: fixed) added `PositionFixed` and `Fixed()` (the
Tailwind `fixed` class left the unsupported list), the containing-block
rule (`gpui/containingblock.go`'s fixed split: every container defers a
fixed child into the ROOT scope — the window root is the port's
initial-containing-block stand-in, since nothing but a transform, filter
or containment makes an ancestor a fixed container,
`layout_object.cc:2009-2067`, and the port has no transforms), and the
paint rule (`gpui/fixed.go`: the fixed div's whole draw defers at
`fixedDrawPriority` under a zeroed element offset, so it paints above
the whole in-flow scene — Appendix E step 8 — is not scrolled or
clipped by any ancestor scroller, and keeps its hitbox on top;
explicitly deferred surfaces and the top layer still paint above it).
`fixtures/fixed` is the fixture (`fixed-window`): the scroller scrolled
120 by three keyboard line steps with the fixed box straddling the
scroller's right edge (the clip escape), the relative+static escape
chain, and the flow column whose fixed box overlaps a later in-flow
sibling. The wrong view (the boxes as plain absolute — the pre-ticket
pin, the old class.go advice) fails Tier A on exactly the three moved
boxes.

Ticket 19 (core text selection) moved the window-scoped selection model
from Base into the core (`gpui/text_selection.go`, the whole
U:text_selection.rs port, plus its auto-scroll, touch-selection and
gesture-global dependencies; Base re-exports the surface as aliases and
installs its upstream word/line boundaries over the new
`gpui.SetTextSelectionBoundaries` seam, so Base's text views are unchanged
on top of the core model). The core's own boundaries are the browser's
(`gpui/text_boundary.go`): the UAX #29 Latin-subset word segments for the
double click, the paragraph-including-its-break line for the triple
click, and Windows' double-click trailing-whitespace extension; a drag
after a multi-click extends by the same granularity (the stored
multi-click range is the anchor word,
`windowSelectionState.updateGranularityEndpoints`). The copy command
falls to the window selection when nothing editable is focused
(`gpui/text_selection_copy.go`, the `EditActionHandler` the model stands
in for — Chromium's editor commands act on the frame's selection, not
only on editable elements).
`fixtures/selection` is the fixture set (`selection-drag`,
`selection-word`, `selection-line`, `selection-word-drag`): the pages
carry a hidden `background: Highlight` probe and the records carry
`window.getSelection()` (anchor/focus offsets, the text) through the
fixture's `ComputedScript`; the replay reads the model's projection back
through the participant's `ProjectedRanges`/`ProjectedText` (a final
Script step's Go half captures it) and the fixture's own test compares
the source ranges — Tier B-like, beside the harness's three tiers. The
replay's selection wash is pinned to the record's own screenshot-sampled
colour (`Configure` + `App.SetSelectionColors`): headless Chrome's wash
is the embedder default's blend (kDefaultActiveSelectionBgColor
0xFF1967D2, renderer_preferences.h:26), not the OS palette the computed
`Highlight` system colour resolves, so the record's pixels are the only
deterministic source — the comparison then measures the selection's
geometry, not the theme. Two recorded divergences the fixtures inherit:
the selected text keeps its own colour over the wash where Chrome paints
HighlightText (ticket 28's recorded divergence, the tolerance floor),
and the triple-click's break paints to the last inked glyph (the break
takes no ink) — a selection crossing a hard break paints the row tail to
the run's bounds where Chrome uses the line box's width.

Ticket 16 (the full drag-and-drop sequence, stories 55/56) added the DOM drag
events around the pin's drag machinery (`gpui/dragevent.go`): a per-window drag
session drives `dragstart` (a prevented one cancels the start), `drag`,
`dragenter`/`dragleave` (on the hit chain, enter before leave),
`dragover` (with the effectAllowed-derived default dropEffect, settable by the
listener and masked against the source's `AnyDrag.EffectAllowed`), `drop` (an
accepting target only) and `dragend` (the final effect), in Chromium's order —
the drag fires before every enter/leave/over, the enter+over pair fires at the
drag's start, and the move after a target change fires dragover alone; a
release over a non-accepting target is the cancel sequence (drag, dragleave,
dragend-none, no drop). Escape is consumed by the drag machinery before any
listener, binding or default action (the OLE drag loop owns the keyboard), and
window deactivation cancels the same way. The OLE IDropTarget's
DragEnter/DragOver/Drop effects now follow the session's acceptance — COPY only
over a target that takes the drop (a drop listener or a cancelled dragover),
NONE over elements that will not accept the files (the pin's unconditional COPY
was the recorded divergence). The events are the DOM-style builders
`OnDragStart`, `OnDragEvent` (the DOM drag; `OnDrag` is the pin's source
builder), `OnDragEnter`, `OnDragOverEvent` (the pin's `OnDragOver` is the
drag-over style), `OnDragLeave`, `OnDropEvent` (the pin's `OnDrop` is the
value delivery) and `OnDragEnd`. `fixtures/dnd` is the fixture set
(`dnd-across`, `dnd-drop-refused`, `dnd-escape`).
