# 01 - Browser-parity harness (tracer)

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 6 (Claude Code); worker chat 4f488bc4
Blocked by: none
Triage: ready-for-agent

**What to build:** A maintainer can write one fixture: a small HTML/CSS page, the equivalent gpui-go view and a scripted input sequence. A recorder captures the page in headless Chrome (element boxes, the event log for the input sequence, a screenshot) as checked-in fixture files. A replay test runs the gpui-go view in the headless test window with the real text system and simulated input, and compares boxes exactly, event logs exactly and pixels approximately. This is the main test seam of the spec.

**Blocked by:** None - can start immediately

**Status:** ready-for-agent

- [x] The recorder runs headless Chrome (no visible window) and writes boxes, event log and screenshot fixtures, recording the Chrome version.
- [x] Replay compares boxes as Tier A (exact), event logs as a Tier B transcript (exact) and pixels as Tier C, with a per-fixture tolerance documented as provisional (research 19).
- [x] One tracer fixture passes end to end: a padded, bordered box with text and a click handler.
- [x] A deliberately wrong gpui-go view makes each of the three comparisons fail with a readable diff.
- [x] Writing a new fixture is documented in a short guide.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, orchestrator 6: verified on core/01-harness before merge. Re-ran vet, `go test ./internal/parity/... -v` (TestFixtures/tracer-box-click and the three deliberate-failure tests pass; each fails only its own tier) and `go test ./gpui/...` (ok). A fresh `go run ./cmd/parityrecord -fixture tracer-box-click` reproduced the record and a byte-identical screenshot (only recordedAt changed). Confirmed the capture window is created hidden and off-screen. The `internal/portfixture` font-count failure (94 vs 95 families) and the gofmt issue in internal/fontspec/weight_synthesis_test.go are pre-existing on main.

- 2026-10-24: claimed by the harness worker (branch core/01-harness).

## Answer

Built on branch `core/01-harness`. Everything lives in `internal/parity` (harness) with one fixture package (`internal/parity/fixtures/tracer`), one command (`cmd/parityrecord`), checked-in records in `internal/parity/testdata/<fixture>/`, and the guide at `docs/browser-parity-harness.md`.

### What was built

- **The fixture model** (`fixture.go`): a `Fixture` is the HTML page, the logical window size, the gpui-go view (`func(*EventLog) gpui.AnyElement`, re-run every frame like any Render), the scripted input sequence (`parity.Move/Down/Up/Click/Key` at CSS-pixel positions — one vocabulary dispatched to both sides), the compared element ids, the event-kind vocabulary, and the provisional Tier C tolerance. Element ids are the mapping: `data-id` on the HTML side, `.ID(id).DebugSelector(id)` on the gpui side (IDs for interactivity/element state, debug selectors for the bounds key — zterm's pattern).
- **The recorder** (`recorder.go`, `ws.go`, `chrome.go`, `cmd/parityrecord`): finds the locally installed Chrome (PATH / standard locations / `GPUI_PARITY_CHROME` override; never `chrome.exe --version`, which opens the user's browser), launches it `--headless=new` with a throwaway profile and a picked-free DevTools port, pins the viewport via `Emulation.setDeviceMetricsOverride` at the harness's device pixel ratio (2, the gpui test window's fixed scale factor), loads the page from a temp file, binds the fixture's event kinds to every `[data-id]` element, dispatches the input steps as trusted input (`Input.dispatchMouseEvent`/`dispatchKeyEvent`, full browser hit testing), then collects `getBoundingClientRect` boxes, the event transcript (with each event's final `defaultPrevented`) and `Page.captureScreenshot`. A minimal hand-rolled RFC 6455 client talks the DevTools websocket (golang.org/x/net/websocket always sends an Origin header, which Chrome rejects). The output — `record.json` (name, Chrome version, recorded-at, viewport, boxes, events), `chrome.png`, and a `page.html` copy — is written to `internal/parity/testdata/<name>/` and checked in. The Chrome process tree is killed (`taskkill /T /F`) at the end.
- **The replay** (`replay.go`, `capture.go`): `parity.Replay(t, fixture)` opens the hostless test window (`NewTestWindow`) at the fixture's logical size on the REAL text system (`UseRealTextSystem`), runs the input steps through the simulated-input context (`VisualTestContext` — the same dispatch the zterm drag tests use, whose flush draws after every event so hit testing sees the last frame), then collects the three observables: element boxes from the last frame's debug bounds, the transcript from the `EventLog` the view's handlers appended to (each dispatched event's entries marked with the dispatch result's `DefaultPrevented`), and the final frame's pixels. The pixel capture is the offscreen-capture prior art adapted: the test window's finished scene plus its glyph atlas are rendered through a HIDDEN, never-shown host window's surface (`Renderer.RenderToImage` without presenting) resized so its client extent is exactly the scene's device-pixel extent, and verified against the surface's actual extent. One capture host (app + host + renderer) per replay; the deterministic virtual clock is available to fixtures for future motion tests.
- **The comparisons** (`compare.go`): Tier A compares every recorded box against the replayed debug bounds exactly (float32 precision, no tolerance) and reports per-element diffs; Tier B compares the transcript exactly (kind:target(:defaultPrevented) per line, first-divergence diffs); Tier C compares both opaque RGBA images at device resolution — mean per-channel diff and hot-pixel fraction (pixels whose mean channel diff exceeds 32), image-wide AND within every compared box (a whole-image average cannot hide one badly wrong element; no masks) — under the fixture's `PixelTolerance`, reporting measured statistics against the gates. A size mismatch is a hard failure. `MeasurePixels` reports the ungated statistics (the tolerance-setting tool), and `TestPixelTolerancesKeepHeadroom` pins every tolerance at ≥2× the measured correct-view values so rasterization drift fails loudly there with a recalibration instruction instead of failing fixtures mysteriously.
- **The tracer fixture** (`fixtures/tracer`): a 640×400 page (white root, centered 200×80 box, 8px padding, 1px #334155 border, #e2e8f0 background, "Hello!" in the system UI font at 16px/1.618) with mousedown/click handlers on the box AND the root (the bubble order is part of the transcript), one scripted click inside the box clear of the text, and deliberately-wrong view constructors (`WrongBoxSize`, `WithoutClick`, `WrongPadding`).

### How to run

```sh
# Record (from the module root; Chrome must be installed):
go run ./cmd/parityrecord                 # every registered fixture
go run ./cmd/parityrecord -fixture tracer-box-click

# Replay (all three tiers for every registered fixture):
go test ./internal/parity/...

# The discrimination tests (wrong views must fail, readably):
go test ./internal/parity/fixtures/tracer -v
```

Verified on this PC (Go 1.27.1, Chrome 155.0.8059.85): `TestFixtures/tracer-box-click` passes — boxes exact (box (220,160) 200×80, root (0,0) 640×400), transcript exact (`mousedown:box, mousedown:root, click:box, click:root`), pixels within tolerance (measured whole-image mean 0.014, box ROI 0.231 vs the 0.1/0.5 gates). The three wrong views fail exactly their tiers with readable diffs (`box: recorded (220, 160) 200x80, replayed (210, 155) 220x90`; `event 3: recorded click:box, replayed (missing)`; `box: mean channel diff 0.817 > 0.500`), and each leaves the comparisons it does not target passing. `go vet` and `go test` pass on `./internal/parity/...`, `./cmd/parityrecord`, `./gpui/...` and the full `-p 1 ./...` run.

### Limits and open questions

- **Tier C tolerances are provisional** (research 19's calibration — reference-repeat noise, injected-defect detection — has not run). They were set from the measured correct-vs-correct statistics with ~2× headroom and verified against the three wrong views; `TestPixelTolerancesKeepHeadroom` guards the headroom. Recalibrate when Chrome or the native artifact changes rasterization: measure, set gates at ~2× the correct values, keep a wrong-view test that fails them.
- **Known divergence worked around**: the pin fires `OnClick` BEFORE the `OnMouseUp` listeners during the up dispatch, where the browser delivers `mouseup` first. The tracer instruments mousedown and click (whose delivery order, including bubble order, matches); the mouseup-ordering fixture belongs to the input-bundle ticket (spec D12). Recorded in the guide's known-divergences section.
- **Capture window budget**: the pixel capture renders through a real (hidden) window surface, so a fixture's device extent (logical × 2) must fit the primary monitor minus the caption (~1060px tall at 1080p/100%). The tracer uses 640×400 → 1280×800 with wide margin. Fixtures needing bigger captures need a windowless/explicit-extent surface path (a native-client render like headlessspec Part A) — future harness work if a ticket needs it.
- **Event vocabulary**: mousedown, mouseup, click today (one `EventLog` helper each, validated at record and replay time). Keyboard events dispatch on both sides (`StepKey`, with a virtual-key table for the common keys) but no key event kinds are instrumentable yet; extend the helpers and the vocabulary together when a ticket needs them.
- **The `defaultPrevented` semantics**: Chrome's record is the DOM event's final state; the replay marks each dispatched event's entries from the dispatch result (`Window.PreventDefault`). They agree at the whole-dispatch level; per-listener observation order is D5/D12 territory.
- **Single-machine fixtures**: the records are only as portable as this PC's fonts (Segoe UI) and Chrome 155; the record carries the Chrome version, and the guide says when to re-record. The recorder never touches the user's browser profile (fresh `--user-data-dir`), never sets `GPUI_REAL_INPUT_TESTS`, and nothing here ever shows a window.
