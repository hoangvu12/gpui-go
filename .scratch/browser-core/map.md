# Browser core: implementing the browser-parity spec

Labels: wayfinder:map

## Destination

Implement [the spec](../browser-parity/spec.md):
- a browser-like gpui-go core (the browser wins over the GPUI-CE pin, D0)
- TSX UI scripting ([ADR 0003](../../docs/adr/0003-tsx-ui-scripting.md))
- the R15 and R16 native rebuilds
- the performance track
- Base moved to a local `gpui-go-base` repository

The user approved the 76-ticket breakdown on 2026-10-23 ("looks good").

## Notes

- Tickets are in `issues/`, numbered in dependency order. Each lists `Blocked by`.
- The frontier is every open, unassigned ticket whose blockers are all resolved. At the start these have no blockers: 01 (the harness), 02, 03, 30, 31, 39 (R15, approved), 47, 49 (the R16 design, for human review) and 68 (the display codemod). Most tickets wait on 01.
- The main test seam is the browser-parity harness (01): HTML fixtures recorded once in headless Chrome, replayed against the gpui-go view in a headless window. The native spec packages, the TSX host through the same harness, and Base against the public API are the other seams.
- R16 (50-53) works on one integration branch. Green is promised only at 54, which needs the user's approval for the rebuild.
- Rules:
  - At most 6 workers (raised from 3 by the user on 2026-10-24), each in its own worktree.
  - Verify worker claims.
  - No push, tag or GitHub repo creation without the user.
  - Headless and offscreen by default.
  - Never set `GPUI_REAL_INPUT_TESTS=1` for a full run while the user is on the PC.
  - zterm and the gallery are never pushed.

## Decisions so far

- [01 - Browser-parity harness](issues/01-browser-parity-harness.md): `internal/parity` + `cmd/parityrecord` + guide `docs/browser-parity-harness.md`. Fixtures run at device pixel ratio 2, mapped by `data-id` and `.ID().DebugSelector()`. Records come from headless Chrome over a raw DevTools websocket. Replay uses the hostless test window and real text system, with pixels rendered through a hidden off-screen window. Tier C tolerances are provisional (at least 2x the measured correct values). Known gap: the pin fires click before mouseup (left to the D12 input ticket). Fixture size is limited to the screen height (~1060 device px).

- [49 - R16 compositor design](issues/49-r16-design.md): approved 2026-10-24 ([docs/design/r16-compositor.md](../../docs/design/r16-compositor.md)). One effect-group mechanism and a target pool where overflow warns. Property-only frames are automatic, damage is capped at 16 regions, gradients take up to 64 stops, and DComp partial present happens only after a readback test. 50-53 now also wait on 39. Rev 16 (54) still needs approval.
- [39 - R15 native rebuild](issues/39-r15-native-rebuild.md): native rev 15, DLL `8b944eae…` (static CRT).
  - Working: Chrome's Windows bold rule (≥600 over a face <600); gradient backgrounds with CSS line length and premultiplied sRGB; colour-matrix filters; pixelated sprites with a point sampler; drop shadows clipped out of the border box; backdrop crop with mirrored margins; flow-root; grid track lists; justify-items/self.
  - Patched shaders are compiled from a vendored copy (`native/shaders/` + `native/build.rs`); ce-source is untouched. R16 should build on this.
  - GAP: the text-run fields and the measure baseline only pass through the ABI, so 42 and 43 need a follow-up native change.
- [05 - Box painting fixes](issues/05-box-painting-fixes.md): the overflow clip always insets by the border widths. Overflowing radii scale by one common factor (`ConstrainRadiiForQuadSize` replaces `ClampRadiiForQuadSize`). Images fit and clip in the content box, and `object-fit: none` is centred. The pinned shader can't draw a radius past half a side; R16 fixes that.
- [31 - TypeAhead helper](issues/31-typeahead-helper.md): `gpui.TypeAhead`, a port of Blink's type_ahead.cc. It has a 1 s timeout, first-letter cycling, matching from the selection with wrap-around, and case/accent folding (precomposed Latin only, not full ICU). The clock is injectable.
- [47 - Preferred frame interval](issues/47-preferred-frame-interval.md): `WindowOptions.PreferredFrameInterval` / `Window.SetPreferredFrameInterval`. Like Chromium, only full and half rate exist: half rate when interval > 2×vsync − 0.5 ms. The pacer grants a half-rate window every other slot, using a measured median vsync.
- [02 - Wheel page-scroll setting and capture release](issues/02-wheel-page-scroll-and-capture-release.md): WHEEL_PAGESCROLL becomes a `ScrollDelta` Pages variant, resolved at the scroller with Chromium's page step; mouse capture is released only when no button is held.
- [03 - Missing cursors and frames for hidden windows](issues/03-cursors-and-hidden-window-frames.md): grab, grabbing, copy and context-menu are runtime-built 32x32 monochrome cursors (Windows has no system ones); minimized or DWM-cloaked windows keep their frame demand pending until visible. Ordinary occlusion is not detected.

## Not yet specified

- R17+ items (text gamma preblend if probe 08 shows a gap, skip-ink, COLRv1 emoji, per-codepoint fallback), a retained layout interface, scroll layers or scroll-by-copy, View Transitions, the TSF text store and pen/touch pointer events. These are ticketed after measuring.

## Out of scope

- The spec's Out of Scope section applies.
