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
  `defaultPrevented` state must match exactly.
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
       EventKinds: []string{"mousedown", "click"},
       View:       View,                     // func(*parity.EventLog) gpui.AnyElement
       Tolerance:  parity.PixelTolerance{...},
   }
   func init() { parity.Register(Fixture) }
   ```

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

3. **Element ids are the mapping.** Every compared or instrumented HTML element carries
   `data-id="<id>"`; the gpui elements set `.ID("<id>")` (interactive state, click
   handlers need it) and `.DebugSelector("<id>")` (the bounds key) with the same string.
   `BoxIDs` lists the compared ones; `EventKinds` lists the event kinds the page binds and
   the view logs (`log.MouseDown("box")`, `log.Click("box")` — one helper per kind).

4. **The input script is one vocabulary.** Steps are logical (CSS-pixel) positions:
   `parity.Move/Down/Up/Click/Key`. The recorder dispatches them to Chrome as trusted
   input (full hit testing) and the replay simulates them through the visual test context
   — the exact same coordinates, the exact same expansion (`Click` = move + down + up on
   both sides).

5. **The view function re-runs every frame** (like any `Render`): build the tree fresh on
   each call, keep mutable state outside (the `EventLog` is passed in; anything else goes
   in the fixture's closure or an entity).

6. Import the fixture package in `internal/parity/replay_test.go`'s import block (blank
   import) so `TestFixtures` picks it up, and in `cmd/parityrecord/main.go`.

   A fixture with no instrumented events (a pure painting fixture) leaves `EventKinds`
   and `Steps` empty: the recorder's instrumentation binds nothing and both transcripts
   are empty, which the Tier B comparison accepts. The `BoxIDs` are then the whole
   fixture; `internal/parity/fixtures/boxpaint` records that way.

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

## Known divergences the harness works around today

These are gpui-go (pin) behaviours that differ from the browser; the fixtures avoid them
until the owning spec tickets land, and each should get a fixture the day it is fixed:

- **`OnClick` fires before the `OnMouseUp` listeners** during the up dispatch (the DOM
  delivers `mouseup` before `click`). The tracer instruments `mousedown` and `click`
  only. Owned by the input bundle (spec D12).
- **Default line height is 1.618** (CSS `normal` comes with D1): the tracer's page pins
  `line-height: 1.618`.
- Text rasterization itself differs (Skia gamma/contrast vs the native stack), which is
  what Tier C's tolerance absorbs; research 19's A/B decides whether that gap needs work.
