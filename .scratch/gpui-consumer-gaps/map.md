# Close the gaps a consumer app (mordbard) hit in gpui-go

Labels: wayfinder:map
Status: open

## Destination

A consumer Windows app built only on gpui-go's public API (mordbard, a Go desktop app ported 1:1 from an HTML design) can show element-tree text in a presented, redrawing window, use embedded fonts, enforce a minimum window size, and hold a strong self-entity handle in listeners.

## Notes

- Found by mordbard's readiness spike (2026-10-19). Evidence and run logs: `../mordbard/.scratch/mordbard-v1/research/02-gpui-go-readiness.md` and `../mordbard/spikes/gpui-readiness/` (on mordbard's `ticket/09-gpui-readiness` branch until merged).
- Filed with the user's approval on 2026-10-19. Implementation by Roboco `pi` / `iroha/dashscope/glm-5.3` side chats, one ticket each, reviewed by the mordbard orchestrator before merging into `base-port`.
- Tickets 01 and 02 block mordbard's UI (no text otherwise). 03 blocks the 1:1 type spec; 04 and 05 have workarounds.
- Second batch (2026-10-20), from mordbard's working app (tickets 10 and 12 of `mordbard-v1`, drafts under `../mordbard/.scratch/mordbard-v1/issues/draft-gpui-*.md`): 06–08 and 09 are rendering/lifecycle gaps the working app hit; 10 is this repo's own test hygiene (the %TEMP% leak it caused on this machine). Root causes verified in this worktree before filing: 07's panic is a cross-goroutine `App.Update` racing the foreground scheduler drive; 08's missing preview text is a doubled element offset in `ParentRelativeLayoutBounds`; 09's invisible zero-blur inset is the band landing under the element border (the rasterizer handles blur 0 — the draft's suspicion was wrong); 06 is the pinned fontique typographic-family-only registration with no report of the landed name.

## Tickets

- [Present text drawn by the element tree through a consumer-owned present loop](issues/01-present-element-tree-text.md)
- [Stop Text elements leaking native shaping handles on every frame](issues/02-text-shaping-handle-leak.md)
- [Register fonts from memory (embedded font bytes)](issues/03-register-fonts-from-memory.md)
- [Enforce a window minimum size (WM_GETMINMAXINFO)](issues/04-window-minimum-size.md)
- [Give views a strong self-handle (Context.Entity() borrow expires before listeners run)](issues/05-strong-self-entity-handle.md)
- [AddFont reports only a generation; the registered family name(s) are invisible](issues/06-addfont-family-names.md)
- [App.Update from a non-foreground goroutine races the scheduler drive and panics in the window trampoline](issues/07-update-marshal-foreground.md)
- [Text inside drag preview views paints at twice the drag offset (outside the preview card)](issues/08-drag-preview-text-offset.md)
- [Zero-blur inset shadows hide under the element border (CSS casts them from the padding edge)](issues/09-inset-shadow-padding-edge.md)
- [The native test suite leaks an 18 MB gpui-go-native-test-* folder per cache root in %TEMP%](issues/10-native-test-temp-leak.md)

## Decisions so far

- [Ticket 06 resolved: AddFont reports the families the bytes landed under (the pinned fontique precedence, typographic family when it differs)](issues/06-addfont-family-names.md) — `FontRegistration.Families`, one entry per face in file order, deduplicated, never invented for rejected bytes; dual-family registration deliberately not done (the pin registers one family per face).

- [Ticket 09 resolved: inset shadows cast from the padding edge like CSS (recorded deviation from the pin)](issues/09-inset-shadow-padding-edge.md) — the hard `inset 0 1px 0` band paints just inside the border where the reference frames draw it; borderless records and the drop-shadow/direct-seam paths keep the pinned geometry.

- [Ticket 08 resolved: the parent-relative layout path adds the element offset exactly once](issues/08-drag-preview-text-offset.md) — drag preview (and any prepaint-under-offset) text paints inside the card; verified deterministically, on a real window through the consumer present path, and red on the unfixed line.

- [Window minimum size](issues/04-window-minimum-size.md) — `WindowOptions.MinSize` (logical, zero = unconstrained) enforced via WM_GETMINMAXINFO's ptMinTrackSize (scaled + frame offset, re-derived per DPI) plus the SetBounds/initial-placement clamp; zero falls to DefWindowProcW exactly as before.
- [Give views a strong self-handle](issues/05-strong-self-entity-handle.md) — `Context.StrongSelf()` is the window-owned strong self-handle for listener capture (the conforming alternative to a retaining `Context.Entity()`); one lease per (window, view), released at window close.
- [Present element-tree text through the consumer present loop](issues/01-present-element-tree-text.md): `Window.GlyphAtlas()` exposes the window's text atlas to `Surface.DrawScene`/`RenderScene`/`RenderToImage`, and a nil/foreign atlas on a sprite scene answers the typed `ErrSceneSpriteAtlas` (pool/texture-index precondition) instead of a native bad-handle; hello example, README and the `internal/presentspec` real-window gate updated.
- [Register fonts from memory](issues/03-register-fonts-from-memory.md) — resolved: `TextSystem.AddFont` surfaces the pinned `add_fonts` path through a rev-12 native rebuild (text service v2), registered families resolve by name for shaping and rasterization, and `FontIdentity.FamilyFound` makes the silent fallback diagnosable.
- [Ticket 02 resolved: a per-window line-layout cache (the reference `LineLayoutCache` port) makes shaped lines reusable across frames and disposes them at rotation/teardown](issues/02-text-shaping-handle-leak.md)

## Not yet specified

## Out of scope
