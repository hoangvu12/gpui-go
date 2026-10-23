# 08 - Text rasterization and italic probes against Chrome

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 6 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** A measured comparison of gpui-go text against Chrome: dark, light and grey text, grayscale and ClearType, plus synthetic italic slant. The result decides whether gamma/contrast shader work (R17) is needed.

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] A probe report with captures and numbers is committed under research.
- [x] The report states a clear yes/no on R17 text shader work and on italic slant, with the evidence.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

## Answer

Report: [research/browser-parity/08-raster-probes/README.md](../../../research/browser-parity/08-raster-probes/README.md) (captures checked in beside it; rerun with `go test ./research/browser-parity/08-raster-probes/ -v`). Regression fixtures: `internal/parity/fixtures/textpaint` (`textpaint-raster`, `textpaint-raster-gray`, `textpaint-italic`), registered in `TestFixtures` with tolerances from the measured numbers. Harness extensions: `Fixture.ChromeFlags` (per-fixture recorder flags, e.g. `--disable-lcd-text`) and `parity.MeasureInk` (text-ink statistics), both documented in `docs/browser-parity-harness.md`.

**R17 (gamma/contrast shader work): NO — skip.** Like-with-like (Chrome LCD vs gpui ClearType, both engines' default on this ClearType-on PC): mean ink (total text darkness) matches within **0.952–1.005** across dark/light/grey × 12/16/24/32 px; grey text is ≤0.5% off. Coverage histograms agree per bucket to ~0.003 of the cell; whole-image hot-pixel fractions ≤0.0076. The effective edge-coverage curves differ ≤~10% (dark) and up to ~20% only on the faintest light-on-dark AA pixels — light-on-dark mean ink is the widest gap at ~4–5% lighter. The two mechanisms (Chrome: Skia sRGB preblend, contrast 1.0 — empirically pinned by a raw-0.5 edge landing at 0.463/0.466; gpui: raw DirectWrite coverage + the pin's Terminal gamma table at DWrite gamma 1.8 + contrast 0.5) land on nearly the same curves on this PC, and converge further on tuner-configured PCs where Chrome also reads DirectWrite. What the A/B did surface is AA-mode selection, not gamma: Chrome drops LCD above 48 device px (SK_MAX_SIZE_FOR_LCDTEXT — measured: no fringes at 32 px logical/64 device, fringes at 24/48) and Chrome falls back to grayscale on non-opaque layers where gpui always ClearTypes; both belong to report 04 §2(b)/Q21 and a future native rebuild, not R17.

**Synthetic italic slant: YES — needs a fix.** Chrome shears a no-italic family by exactly **−0.2500 (−14.03°)** (Skia `setSkewX(-1/4)`, measured to four decimals); gpui-go's DirectWrite oblique simulation shears by **−0.3399 (−18.77°)** — 34% steeper (16.3 px vs 12.0 px of lean over the same stems; controls: upright 0° on both, a real italic face −0.1685/−0.1684). Root cause: `reference/native/src/glyph.rs:1301-1309` maps Fontique's italic synthesis (14°, which would match Chrome) onto `DWRITE_FONT_SIMULATIONS_OBLIQUE` and discards the angle. Fix shape: apply the shear as a transform (as Skia does) instead of the simulation — native S, one rebuild, a divergence toward CSS per D0, with `textpaint-italic` as the gate (re-record when it lands).

Nothing in `gpui/` or the native code was changed; this ticket measures.
