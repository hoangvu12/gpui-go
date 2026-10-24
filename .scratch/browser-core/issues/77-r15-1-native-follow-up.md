# 77 - R15.1 native follow-up rebuild

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 8 (Claude Code)
Blocked by: 39
Triage: ready-for-agent

**What to build:** A small native rebuild (rev 15.1, approved by the user on 2026-10-24: "ok try that") that closes the gaps found after rev 15:
1. **The extension-uniform upload.** `reference/native/src/renderer/draw.rs:2327` and `:2330` call `update_buffer(ctx, &buf, &[*uniforms])`, and `match &extras` makes `uniforms` a `&&T`, so only 8 pointer bytes reach the cbuffer. Colour filters are GPU no-ops (41), and the backdrop crop is most likely broken by the same bug (04).
2. **The synthetic italic shear.** Chrome shears a family without an italic face by −0.25 (Skia `setSkewX(-1/4)`, `platform/fonts/win/font_platform_data_win.cc:37-38`); gpui-go's DirectWrite oblique simulation shears by −0.34 (08, `research/browser-parity/08-raster-probes/README.md`).
3. **The rev-15 text-run fields and the measure baseline** (word-break, overflow-wrap, text-wrap, locale, variations, width, optical size, justify; the first baseline) cross the ABI but don't drive shaping or alignment (39's Answer, TEXT_ABI.md, LAYOUT_ABI.md). 42 and 43 need them.

**Blocked by:** 39 (R15 native rebuild)

- [x] The three filters fixtures pass Tier C without `SkipPixels`, and their wrong-view tests discriminate.
- [x] `shadow-backdrop` is re-registered in `internal/parity/fixtures/shadow/shadow.go` and passes Tier C.
- [x] The italic probe measures Chrome's −0.25 shear; `textpaint-italic` gets closer to Chrome.
- [x] The text-run fields and the baseline drive behaviour, or the ticket records exactly what blocks each one and the smallest plan to unblock it. (text-wrap: nowrap drives; the rest are recorded per field in TEXT_ABI.md/LAYOUT_ABI.md — see the Answer.)
- [x] Revision and artifact pins, measurements and evidence are updated in every place rev 15 touched; bit-exact fixtures change only with a recorded reason.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, worker (core/77-r15-1): built, measured, verified. `go test -p 1 -count=1` passes on the required set (native, fontspec, glyphspec, scenespec, textspec, layoutspec, consumerspec, parity/..., the 08 probes, gpui, base, richtextspec, zterm); the only failure anywhere in the run is the known pre-existing portfixture `TestFx0004` (font count 94 vs 95, out of the required set), and `gpui TestWin32TranslateKeyDownCarriesCodeAndLocation` needed one re-run (it reads the live Shift key; it passed). `go vet ./...` clean, `gofmt -l` clean on the touched files, `go build ./...` clean. `cargo test -p gpui-go-native` still fails to compile the lib test target on the same 5 pre-existing a11y/capture errors; nothing new (this rebuild's record changes updated the scene test constructors). The revision choice (151 = the u32 encoding of 15.1) is explained in the Answer before its use.

## Answer

Done as **native revision 151 — rev 15.1** (the revision field is a u32
equality-compared by the ABI table, the manifest and the Go constants, so a
literal 15.1 is unrepresentable; taking 16 would consume the number the R16
plan reserves — ticket 54 says "rebuilt as native rev 16" — so the point
release encodes as 151, documented at every pin site: `lib.rs`, `abi.go`,
`tools/measure`, `consumerspec/evidence.go`). SHA-256
`ef337ad3051a382b7c9d98e5c10c9926f72df6e7d3c0d4d2a1012ec7cb322f4b`,
19,107,328 bytes, static CRT, mask `0xFFF`. Evidence:
[`evidence/browser-core/ticket77-native-rebuild.json`](../../../evidence/browser-core/ticket77-native-rebuild.json).

### 1. The extension-uniform upload (done — and the deeper divergence it revealed)

`dx_blur_pass`'s two arms now upload `&[**uniforms]`
(`reference/native/src/renderer/draw.rs`): `match &extras` on
`BlurPassExtras::Composite(&T)` binds `uniforms: &&T`, so rev 15's
`&[*uniforms]` was a one-element slice of REFERENCES — `update_buffer`
copied 8 pointer bytes into the 112-byte cbuffer and every colour
function was a GPU no-op (ticket 41's diagnosis). Every other
`update_buffer` call in draw.rs passes value types and was audited
unaffected. The backdrop-crop cbuffer (draw.rs, the same arms) had the
identical bug — the mirror fold read zeros and sampled a constant point
(ticket 04's "un-blurred neighbor average").

With the cbuffer actually written, the filters fixtures exposed a
**second, deeper divergence**: Chrome does NOT compose a chain into one
matrix — each function's output is rasterized 8-bit before the next
applies, **clamping every channel to [0,1]**. Measured against the
filters-chain Chrome record (hue-rotate(90deg) sepia(1)): pure red reads
(70,62,48) = sepia·clamp(hue·red) while the composed matrix gives
(42,38,29); all five stripes' 15 channel values match the
clamp-after-each model exactly, and the composed payload measured a
**15.4667 mean channel diff / 0.2 hot** on the hue-sepia cell (the
arithmetic prediction: (23.8+17.6+35.7)/5). So this rebuild also
replaced the composed payload with the per-function one:

- the scene filter record grew 188 → **780 bytes** (`MAX_SCENE_FILTERS`
  4 → 8, `color_matrix` 20 → 160 words — one 20-word slot per colour
  function; scene service v5; `scene.rs` + `SCENE_ABI.md` + the Go
  mirrors),
- the vendored composite shader (`shaders/filters.rs`) carries 8
  `ColorMatrixEntry` slots + a count and applies them in chain order
  **with `saturate` between** (the Rust mirror in draw.rs: 112 → 656
  bytes; the uniform-array + while-loop shape compiles clean through
  the pinned wgsl-rs → Naga 24 → HLSL sm 5.0 → D3DCompile chain),
- the Go `FilterChain`/`FilterBoundary`/`BackdropFilter` carry the
  per-function matrices (`gpui/filter_colors.go`, `gpui/scene.go`,
  `gpui/scene_kernel.go`); chains longer than the record's capacity
  compose their excess into the last slot (the documented
  approximation, `shaders/mod.rs` divergence 3).

**Fixture numbers (before → after, Tier C mean/hot):**

| fixture | rev 15 | rev 15.1 |
|---|---|---|
| filters-colors | skipped (blocked) | whole **0.0000 / 0.00000**, every swatch ROI **0.0000 / 0.00000** — pixel-exact |
| filters-chain | skipped (blocked); with the uniform fix alone the composed payload measured whole 0.3510, hue-sepia ROI **15.4667 / 0.20000** | whole **0.0000 / 0.00000**, both ROIs **0.0000 / 0.00000** — pixel-exact |
| filters-image | skipped (blocked) | whole 0.0493 / 0.00096; ROIs img-control 2.1740 / 0.04094, img-grayscale 1.2747 / 0.02042, img-hue-rotate 1.8084 / 0.04083 (the recorded image-resampling class; the colour map itself is exact) |
| shadow-backdrop | unregistered (broken: the zeroed crop uniform) | whole **0.3562 / 0.00000**, both ROIs 0.3562 / 0.00000 — no pixel over the 32-per-channel hot bar |

The three `SkipPixels` flags are gone, the three filters discriminators
are unskipped and fail Tier C as they should (Tier A green), and
shadow-backdrop is re-registered with its half-σ discriminator as a
passing gate (`shadow_test.go`
`TestBackdropSigmaIsHalfRadiusFailsOnlyThePixelComparison`). **Still
open (recorded, not fixed): the partial-region backdrop crop's composite
geometry** — a backdrop smaller than the window still renders its
blurred input zoomed (dx_blur_and_composite passes the crop total as the
composite's target size while the blurred content occupies the top-left
of the window-sized half-res ping); the fixture's full-viewport panel
sidesteps it (the crop region IS the window, margin 0), so it passes —
the clean fix is a dedicated crop-sized half-res ping/pong pair, left
for the next native change with the analysis in the evidence JSON.

### 2. The synthetic italic shear (done)

`reference/native/src/glyph.rs` no longer maps Fontique's italic
synthesis onto `DWRITE_FONT_SIMULATIONS_OBLIQUE`; the raster transform
carries the exact **−0.25** x-shear (`synthetic_italic_shear` /
`raster_transform`, driven by the synthesis FLAG only — the constant is
Chrome's `setSkewX(-SK_Scalar1/4)`
`platform/fonts/win/font_platform_data_win.cc:37-38`, not Fontique's 14°
(tan 14° = 0.2493)). The shear pivots on the glyph-run baseline, rides
all three raster paths (mask, monochrome color, COLRv0), and leaves
shaping, advances, the glyph-cache keys (the FontId carries the
synthesis) and the subpixel baseline origin untouched — Chrome's faux
italic is raster-only. The bold simulation is untouched (rev 15's
measured-good rule).

- The 08 italic probe: gpui tahoma-italic shear **−0.3399 → −0.2500**
  (Chrome −0.2500), dx over the stems 16.3 → 12.0 px; the controls
  unchanged (upright 0.0000, the real-italic Segoe face −0.1685 vs
  Chrome's −0.1684). `italic_test.go` now GATES the shear (the
  synthetic row must measure −0.25 on both sides).
- `textpaint-italic` Tier C (a recorded divergence **toward Chrome**):
  whole 0.3460 → **0.2165**, hot 0.00494 → **0.00328**, worst ROI
  (tahoma-italic) 1.7671 → **0.6882**; the tolerance re-recorded at 2×
  (0.45 / 1.4 / 0.02) and the wrong-view M-stem discriminator still
  fails (whole 1.573 / ROI 5.918). The 08 README's §6 and §4.4 carry
  the before/after tables; the captures re-measured.
- No bit-exact oracle fixture changed (fx-0005 has no italic case; the
  fx-0004 italic run is a real Segoe UI face — and geometry is
  advance-driven anyway).

### 3. The text-run fields and the measure baseline (one wired, the rest recorded)

- **text-wrap: nowrap — WIRED** (`text.rs` `nowrap_requested`): CSS
  text-wrap is a wrap policy and the pinned request can express "do not
  soft-wrap" (`wrap_width: None` → the adapter's `break_all_lines(None)`,
  the pinned hard-break-only path). `text_shape`/`text_layout` suppress
  the wrap when any run carries the tag; the layout summary reports the
  ACTIVE constraint; hard breaks and the clamp survive. Gate:
  `internal/textspec/rev15_1_nowrap_test.go` (the control wraps to 3
  rows, nowrap stays 1 with no active wrap, relayout stays 1,
  balance/pretty/stable still wrap, hard breaks + clamp survive).
- **balance/pretty/stable** — Go-side bisection by the rev-15 record's
  design; the tag crosses the ABI for that layer. Nothing to wire.
- **word-break / overflow-wrap — BLOCKED**: Parley 0.11.1 has the
  range-level styles and the builder hook (`set_line_break_override`,
  which the pinned adapter already pins Chromium's table through), but
  both live behind `gpui_ce_parley`'s private `parley` state and the
  pinned `TextRun` has no slot. Smallest plan: the vendored
  `parley_paragraph_layout` copy + a side-channel request entry (the
  pinned gpui types stay untouched); 6–10 h. Not done.
- **locale — BLOCKED** (no slot; Parley has no language style — an
  upstream change first). **font width / variations / optical size —
  BLOCKED** (the pinned resolve hardcodes `FontWidth::NORMAL` at
  `catalog.rs`; the store's variation interning is `pub(crate)`); the
  plan is a vendored adapter with an extended resolve entry, 6–10 h.
  **justify — BLOCKED** (the pinned `TextAlign` is Left/Center/Right
  only; Parley's `Alignment::Justify` is unreachable); 4–6 h.
- **The measure baseline — BLOCKED** as rev 15 recorded (Taffy 0.13's
  leaf compute hardcodes `first_baselines: Point::NONE`; LAYOUT_ABI.md
  keeps the note; revisit with the R17 retained-layout ABI).

The full per-field record with file:line, the smallest plan and
estimates is in `reference/native/TEXT_ABI.md` (rev 15.1 section).

### Verification

- `go vet ./...` clean; `gofmt -l` clean on every touched file;
  `go build ./...` clean.
- `go test -p 1 -count=1` on the required set
  (native/fontspec/glyphspec/scenespec/textspec/layoutspec/consumerspec/
  parity/…/08-probes/gpui/base/richtextspec/zterm): **all pass**.
- `cargo test -p gpui-go-native` still fails to compile the lib test
  target with the same **5 pre-existing** a11y/capture errors; this
  rebuild's record changes updated their test constructors.
- No bit-exact fixture changed; the only pixel fixture that changed is
  the browser-parity `textpaint-italic` (a recorded divergence toward
  Chrome, above).

### Divergences (recorded)

All toward Chrome: the per-function colour-matrix clamp semantics
(`shaders/mod.rs` divergence 3, `SCENE_ABI.md`); the exact −0.25
synthetic-italic shear (`GLYPH_ABI.md`'s new section); the
overflow-capacity and blur-interleaving approximations of the chain
payload (documented at the record). The vendored shader tree stays the
one structural divergence (the frozen `ce-source` is unmodified).

### Not done / open

- The partial-region backdrop crop's composite geometry (the zoom bug,
  above — shadow-backdrop passes because its panel is full-viewport).
- The 48-device-px LCD cap and the grayscale window mode (08's two
  mode-selection findings).
- word-break, overflow-wrap, locale, width, variations, optical size,
  justify (tickets 42/43) and the baseline alignment — behind the
  pinned adapter/Taffy seams, recorded per field.
