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

- [ ] The three filters fixtures pass Tier C without `SkipPixels`, and their wrong-view tests discriminate.
- [ ] `shadow-backdrop` is re-registered in `internal/parity/fixtures/shadow/shadow.go` and passes Tier C.
- [ ] The italic probe measures Chrome's −0.25 shear; `textpaint-italic` gets closer to Chrome.
- [ ] The text-run fields and the baseline drive behaviour, or the ticket records exactly what blocks each one and the smallest plan to unblock it.
- [ ] Revision and artifact pins, measurements and evidence are updated in every place rev 15 touched; bit-exact fixtures change only with a recorded reason.

## Comments

## Answer
