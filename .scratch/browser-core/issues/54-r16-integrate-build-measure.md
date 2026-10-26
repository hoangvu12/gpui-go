# 54 - R16: integrate, build rev 16 and measure

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 11 (Claude Code); worker chat 95babf81; R16 integration branch native/r16
Blocked by: 50, 51, 52, 53
Triage: ready-for-agent

**What to build:** The R16 branch is integrated, rebuilt as native rev 16 and measured, with pins and evidence updated. Needs the user's approval before the rebuild.

**Blocked by:** 50 (R16: group targets, opacity groups and rounded content mask), 51 (R16: transformed composite and property nodes), 52 (R16: masks, clip-path, blend modes, drop-shadow and gradients), 53 (R16: retained frame, damage redraw, dirty-rect present and caching)

**Status:** ready-for-agent

- [x] The user approved the rebuild (recorded in the ticket).
- [x] All native spec packages pass; unchanged bit-exact fixtures stay unchanged.
- [x] Perf measured against the gpui-perf baseline and recorded.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-27, orchestrator 11: verified and merged `native/r16` into main (50-53, 43, 42 and this rebuild; no conflicts). The embedded DLL is native revision 16, SHA-256 `8ec910d4…`, identical to the build output in the shared target. On the merged tree with the embedded artifact (no bundle, no swap): `go vet ./...` and `go build ./...` clean, `displaycodemod -check` and `positioncodemod -check` 0; `go test -p 1 -count=1 ./...` 97 packages ok, the only failure portfixture TestFx0004 (known font count). gpui-go-base needed one port for ticket 42's API change (`gpui.MeasureFunc` returns `gpui.MeasuredSize`; text_inline_flow.go's closure, committed there with the merge); then vet, both codemod checks, `basecoverage -check` (current) and `go test -p 1 -count=1 ./...` all 10 packages ok. Measurements accepted as recorded (frame times unchanged on group-free scenes under machine load; property-only tick 1.58 ms vs 4.93 ms full frame; tess cache 98.3%; hwnd Present1 partial, DComp full by the readback fallback).

- 2026-10-26, orchestrator 11: the rebuild is approved by the user's decision of 2026-10-25 ("feel free to do native tickets after every go only is done, but make sure to make it fast somehow without making my pc to freeze"). Conditions: one cargo build at a time, `-j 4`, below-normal priority. R16 runs on the integration branch `native/r16` (50 → 51 → 52 → 53, then 42/43), with interim DLLs tested through `GPUI_GO_NATIVE_BUNDLE` and not committed; this ticket refreshes the embedded artifact.

## Answer

Landed on `native/r16` (two commits: the rebuild + the pins/evidence,
and the test updates the branch needed). **Native revision 16** set at

- every pin site: `reference/native/src/lib.rs`
  `GPUI_GO_NATIVE_REVISION`, `internal/native/abi.go`
  `nativeRevision`, the measure tool's constant,
  `internal/consumerspec/evidence.go` (`EvidenceNativeRevision` +
  the SHA/bytes pins), the glyphspec/scenespec identity gates, and
  the ABI docs (`SCENE_ABI.md`, `RENDERER_ABI.md`, the design doc's
  appendix-A header; every remaining `151` in the tree is history
  text or a line number).

**The DLL** (built clean, one cargo process, `-j 4` below-normal,
  `CARGO_TARGET_DIR=gpui-r16-target`, static CRT confirmed by the
  measure tool's PE import scan, capability mask `0xFFF`):

- SHA-256 `8ec910d4f24df66f937b312a02345458fd218888eae15a29a906745fc225dd11`,
  19,464,192 bytes, gzip-9 7,688,896, rustc 1.97.0
  (2d8144b78 2026-07-07), built 2026-10-26T18:07:38Z.
- Services: scene **9**, renderer **6**, text **3** (the vendored
  Parley adapter), layout **2** (the trait-driven Taffy compute).
- The measure tool refreshed the embedded pair
  `internal/native/artifacts/{gpui_go_native.dll,manifest.json}` and
  `reference/out/native-bootstrap.json` (this ticket is the one
  place they are committed; the before pair was rev 15.1,
  `ef337ad3…` 19,107,328 bytes).

**The design §6 measurements** (the new env-gated harness
`internal/rendererspec/r54_measure_test.go`, raw JSON in
`evidence/browser-core/ticket54-compositor.json` +
`evidence/perf/ticket54-{before,after}.json`; recorded in
`evidence/browser-core/ticket54-native-rebuild.json` and a new
"Recorded measurements (rev 16)" section in `reference/README.md`):

| Measurement | Value |
|---|---|
| FrameDivs / FrameText, rev 15.1 → rev 16 (best of 5) | 17.09 → 16.92 ms / 130.80 → 132.72 ms (no change on the group-free path by construction; ~2× machine load from other agents on both runs; deterministic counters equal on the div scenes — 5,503 allocs, ~3,060 native calls) |
| Property-only frame (transform tick, no rebuild/layout/paint) | min 1.58 ms vs the full changed frame min 4.93 ms and the same scene's full redraw min 2.17 ms — ≪ met |
| Damage: hover change | 1 input rect, 0–1‰ of the window, min 3.16 ms (rebuild-dominated — ticket 60's fast path owns the rest) |
| Damage: scroll | full-column (no blit) 1 rect / 283‰ min 4.29 ms; exposed-strip (blit) 1 rect / 9‰ min 3.75 ms |
| Pool high-water | requirements isolated targets 3; retained 2 targets / 1,048,576 bytes; 0 overflow |
| State binds per frame | 35 (full redraw and property tick alike; the r53 gate pins the tracker's savings) |
| Tess cache hit rate | 98.3 % (1,888 hits / 32 misses / 0 evictions over 30 re-rendered frames) |
| Presents | DComp: 0 partial / 60 full (the pinned readback fallback); hwnd: 60 partial / 0 full (Present1 accepted) |

**The suites** (all with the embedded artifact — no
`GPUI_GO_NATIVE_BUNDLE`, no manifest swap):

- gpui-go: `go vet ./...` ok, `go build ./...` ok,
  `displaycodemod -check` ok, `positioncodemod -check` ok,
  `go test -p 1 -count=1 ./...`: **95 packages ok** — all 7
  internal/native loader tests pass. Remaining failures: portfixture
  fx-0004 (the KNOWN font count 94 vs 95), gpui
  TestWin32TranslateKeyDownCarriesCodeAndLocation (the KNOWN key-map
  drift, fails alone), focusspec TestRealWindowKeyboardTranslation
  (environmental focus contention — passed in the first run and
  fails identically on main's rev-15.1 tree under the same machine
  state).
- Two real branch gaps found and fixed here: pathspec's two tests
  still pinned rev 15's `MAX_FILTER_GROUP_DEPTH` (ticket 50 removed
  it — every matched group isolates, nesting-based; the tests now
  expect 3 targets at depth 3 and 4 at depth 4) and
  `gpui/grid_tracks_test.go` needed the display codemod's explicit
  `.Flex()`.
- gpui-go-base (tested through a temporary `GOWORK` outside both
  repos; NOTHING edited or committed there):
  `conformance/cmd/basecoverage -check` ok (ledger current, 0 gaps);
  `go vet`/`go build`/`go test -p 1 -count=1 ./...`: 6 packages ok
  (basecoverage, dataurl, rope, segment, strftime, sumtree) and 4
  build failures (the root package + base-gallery/base-showcase/
  zterm examples), ALL one core API change to port: ticket 42
  changed `gpui.MeasureFunc` from `func(req MeasureRequest) Size` to
  `func(req MeasureRequest) MeasuredSize` (Size + optional
  FirstBaseline) — `text_inline_flow.go:288` passes the old closure
  to `gpui.RequestElementMeasuredLayout`; the fix is to return
  `gpui.MeasuredSize{Size: l.size}` / `{Size: layout.size}` at the
  closure's two returns (FirstBaseline can stay nil — the rev-15
  bottom-edge fallback). Reported, not committed, per the ticket's
  instruction.

**Open native leftovers recorded as follow-ups** (none implemented —
none were trivial):

- 52: the pinned underline/sprite/path pipelines' rounded-mask gap
  (they clip rect+fade only; `shaders/mod.rs` divergence 4 records
  it — vendoring those pipelines is the fix).
- 52: the atlas mask stretches the whole polychrome texture over
  the group's bounds (no tile-id lookup; 55 can allocate dedicated
  mask textures or extend the atlas).
- 52: the gradient table is not dithered (the 1024-texel ramp's
  quantization is below the two-stop path's banding; the R15
  dither is untouched).
- 52: a coverage mask on a TRANSFORM group samples the window-frame
  rasterization (a local-frame mask would need per-frame
  rasterization in the group's bucket).
- 51/52: a window-frame group's bucket is inflated by its region's
  distance from the origin (the frame-origin invariant's price; a
  group at (0,0) is truly bounds-sized).
- 42: the strut-baseline paint drift (the measured baseline is
  Chrome's strut math; the paint layer's underline/strut geometry
  can still drift from it).
- 58: the Go damage computation and paint flashing (pass rects into
  `Surface.DrawSceneDamage`; capture-backed scene surfaces update
  outside the scene's knowledge — their bounds belong in 58's
  rule).
- 60: the frame-loop fast path
  (`UpdateEffectMatrix/UpdateEffectOpacity` → old/new bounds dilated
  ~2px → `DrawSceneDamage`; the measurements above are its ceiling).
- 55: the Go styling for the R16 surface (mask-image, clip-path,
  mix-blend-mode/isolation, the >2-stop gradient classes,
  `filter: drop-shadow()`, transform groups) and the
  smoothed-content-mask pixel gate once it authors quad masks.
