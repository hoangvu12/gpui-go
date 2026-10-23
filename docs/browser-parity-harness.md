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
  `click` and the pointer boundary events `pointerenter`/`pointerleave` (browser-core
  ticket 10): the boundary kinds arrive in Chromium's order — leave events innermost
  (child) first, enter events outermost (parent) first, all before the move that caused
  them — and a `Move` step to a point outside the viewport is the DOM-visible half of
  "the pointer left the window" (it fires the leave on the whole hovered chain); the
  WM_MOUSELEAVE half is covered by gpui's own window tests.
  The key kinds are `keydown` and `keyup` (browser-core ticket 09): key entries also carry the
  event's key (`e.key`, DOM key names — `Tab`, ` `, `ArrowDown`), the physical code and
  location (`e.code`/`e.location`, browser-core ticket 13 — `BracketRight`, `Numpad1`
  at 3), and the focused element's id at listener time (`document.activeElement`, body
  normalized to `""`), which is the order probe for the default-action fixtures — a
  listener that still sees the old focus ran before the default action changed it. The
  replay's key step is one full press — key down then key up, like the recorder's CDP
  `keyDown`+`keyUp` pair — and both halves' default-prevented states are recorded.
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
   on the page and matching handlers in the view; and the pages pin `outline: none` on
   focusable elements because the port has no default focus ring yet (ticket 48).

3. **Element ids are the mapping.** Every compared or instrumented HTML element carries
   `data-id="<id>"`; the gpui elements set `.ID("<id>")` (interactive state, click
   handlers need it) and `.DebugSelector("<id>")` (the bounds key) with the same string.
   `BoxIDs` lists the compared ones; `EventKinds` lists the event kinds the page binds and
   the view logs (`log.MouseDown("box")`, `log.Click("box")`,
   `log.PointerEnter("box")`, `log.PointerLeave("box")` — one helper per kind). The
   pointer boundary kinds need the element ids: the retained hover state that diffs
   enter/leave across frames keys on them.
   the view logs (`log.MouseDown("box")`, `log.Click("box")`, `log.KeyDown("box")`,
   `log.KeyUp("box")`, `log.KeyDownPreventDefault("box")` — one helper per kind; the key
   helpers record the event's key, physical code and location and the focus field, and
   the preventing one also calls
   `Window.PreventDefault`). An element may narrow its bound kinds with a
   `data-events="keydown,keyup"` attribute (a comma-separated subset of `EventKinds` —
   the instrumentation honours it on the page side, and the view simply registers only
   the matching handlers).

4. **The input script is one vocabulary.** Steps are logical (CSS-pixel) positions:
   `parity.Move/Down/Up/Click/Key`. The recorder dispatches them to Chrome as trusted
   input (full hit testing) and the replay simulates them through the visual test context
   — the exact same coordinates, the exact same expansion (`Click` = move + down + up on
   both sides). `Key("<keystroke>")` is one full press on both sides: the recorder sends
   a CDP `keyDown`+`keyUp` pair (with the typed-case `key`, so `"j"` reports `e.key`
   `"j"`) and the replay dispatches a `KeyDownEvent` then a `KeyUpEvent` through the
   platform dispatch — bindings, key listeners and the core default actions
   (browser-core ticket 09) all run, and a default action between the halves is visible
   in the keyup entries' focus field.

   A key step produces the keystroke the platform reports (browser-core ticket 13): the
   replay parses the keystroke and normalizes it with
   `gpui.Keystroke.WithSimulatedCode()` — the physical code from the US layout table
   (or an explicit one), the location the code implies, and the shifted-key fold with
   Shift kept (`shift-]` carries `Key "}"`, `Code "BracketRight"`) — and the recorder
   sends the same set to Chrome (see the recorder details below).
   `parity.KeyCode("1", "Numpad1")` spells a physical key the keystroke syntax
   cannot: the key and modifiers parse as usual, both sides carry the code, and the
   location follows it (`Numpad1` at 3, the numpad).

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
   the replay is compared against, so copy them when cloning a fixture.

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

- **Device pixel ratio.** The test window's scale factor is fixed at 2.0, so Chrome
  records at `--force-device-scale-factor` 2 and the replay asserts the window's scale
  (`parity.DeviceScaleFactor`). Boxes compare in logical pixels; pixels compare at device
  resolution; both sides' numbers are the same because the ratios are.
- **Window size budget.** The pixel capture renders through a real (hidden) window
  surface, whose client area cannot exceed the primary monitor: a fixture's device extent
  (logical × 2) must fit the screen minus the caption (on a 1080p display at 100%: up to
  ~1920×1060). The tracer's 640×400 → 1280×800 keeps a wide margin.
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

- **`OnClick` fires before the `OnMouseUp` listeners** during the up dispatch (the DOM
  delivers `mouseup` before `click`). The tracer instruments `mousedown` and `click`
  only. Owned by the input bundle (spec D12).
- **A mouse down on a focusable element reports `defaultPrevented`** — the pin's focus
  transfer always calls `Window.PreventDefault` after focusing (interact.go, the
  div.rs:3279 rule that a focusable parent must not steal focus), where Chrome leaves
  the mousedown unprevented. The keyorder fixtures bind no `mousedown` listeners; owned
  by the input bundle (spec D12).
- **A keyboard activation click does not bubble** to ancestors (the pin calls only the
  focused element's own click listeners; Chrome dispatches a full bubbling click). The
  activation fixture's root binds `data-events="keydown,keyup"` so its click is not
  instrumented; owned by the click/activation ticket (spec D12).
- **Enter activates on keyup, not keypress** (the pin; Chrome clicks at keypress, so
  the recorder's Enter — which sends no CDP `text` — produces no keypress and no click
  at all). The activation fixture uses Space, whose keyup click both sides deliver;
  the Enter timing is the activation ticket's (spec D12).
- **No default focus ring** — Chrome's `:focus-visible` `outline: auto` has no port
  equivalent yet, so the keyorder pages pin `outline: none` on focusable elements (the
  outline ticket, spec story 42, owns the port's ring).
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
code field and the keycode fixtures landed (`fixtures/keycode`).

## Recorder details worth knowing

- **Smooth scrolling is disabled in the recorder's Chrome**
  (`--disable-smooth-scrolling`): with it on, keyboard scrolls land as
  compositor-driven animated offsets at nondeterministic times
  (PaintLayerScrollableArea::ScrollAnimatorEnabled reads the setting), which a
  recorded fixture cannot compare against the port's instant scrolls. The
  smooth-scrolling ticket (spec D13) re-records its fixtures when the port scrolls
  smoothly.
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
