# 70 - Codemod applied in Base, zterm and the gallery

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 10 (Claude Code)
Blocked by: 68
Triage: ready-for-agent

**What to build:** All elements in Base, zterm and the gallery that relied on the default display state Flex explicitly.

**Blocked by:** 68 (Codemod: explicit Flex where the default was relied on)

**Status:** ready-for-agent

- [x] Base tests and zterm tests pass; offscreen captures of zterm and the gallery are identical.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-25, orchestrator 10: verified and merged (ef745a5 + 93e34d8, with 69 and 70 together). `displaycodemod -check ./...` on the merged tree: 0 pending. The diff audit shows only `.Flex()` insertions, plus the TSX pin comment in gpui/script/lower.go and the class_test hover-want fix. `go vet ./...` is clean. The broad batch set plus inlinespec/listspec/textframespec: all ok except 78's four (fixed on its branch), spellspec TestSpellcheckCheckNeverBlocksTheUIThread (load; ok on re-run) and gpui/script TestFrameOverheadIsUnder1msAtTheProbeSize (the known load flake). The worker's capture comparison: zterm 15/19 and the gallery 66/69 byte-identical; every difference also occurs between two runs of the same code (spinner/reveal animation phase, one AA rounding step).

## Answer

- **Applied to Base, zterm and the gallery**: base 75 files / 231 insertions — the pin-default sites 68 named are exactly what got pinned (`base/button.go` NewButton's base, `base/tabs.go` NewTab, `base/root.go`'s root, `base.HFlex`/`VFlex`'s inner div, …); examples 25 files / 184 insertions: zterm 11 / 43, base-gallery 11 / 114, base-showcase 3 / 27. Parity fixture views are pinned like everything else (38 files / 182 insertions — counted in 69): **71 revisits which fixture views should mirror their pages' plain block divs** instead of the Flex they now state explicitly.
- **Generated files**: none touched, none regenerated (tsxbundle's committed output embeds JS, no element calls) — see 69 for the tool's new generated-file skip.
- **Offscreen captures, before vs after the codemod** (BEFORE captured on the clean tree, then `-w`, then AFTER; compared with a small Go program in %TEMP%, byte-for-byte first, then exact RGBA pixel-for-pixel):
	- zterm (`ZTERM_CAPTURE_DIR=... go test ./examples/zterm -run TestCapture -count=1`, 19 states): **15/19 byte-identical**; tab-drag, tab-menu, tab-slot and tabs differ by 14–36 pixels in one ~10×13 region (~(45–57, 245–258)) — the continuously animated agent-status spinner glyphs (RequestAnimationFrame; braille frames). The same four files differ by the same magnitude between **any two runs of identical code** (pre-pre, post-post, and pre-archive vs pre-worktree all show it), so this is pre-existing capture nondeterminism, not a codemod change.
	- gallery (`GALLERY_CAPTURE_DIR=... go test ./examples/base-gallery -run TestCapturePages -count=1`, 69 pages): **66/69 byte-identical**; motion and motion-open (reveal-animation pages) differ by thousands of pixels between any two runs of the same code; plot-radial differs by 2 pixels (one antialiasing rounding step, 0xd5↔0xd6, at (437–438, 235)) that also occurs between two pristine pre-codemod runs, and a pre-codemod run is byte-identical to a post-codemod run — rasterizer nondeterminism, not the codemod.
	- Net: every differing capture is reproducible on unchanged code; no pixel difference is attributable to the codemod.
- **Tests**: `go test ./base ./examples/zterm ./examples/base-gallery ./examples/base-showcase ./examples/tsx-hello` all ok; the whole-repo run (`go test -p 1 -count=1 ./...`) is in 69's answer (99 ok; only the known 78/portfixture failures).
- **TSX flag for 71**: see 69 — the lowering `gpui.Div()` is pinned Flex now; 71 decides whether TSX `<div>` follows the block default.
