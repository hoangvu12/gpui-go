# Close the gaps a consumer app (mordbard) hit in gpui-go

Labels: wayfinder:map
Status: open

## Destination

A consumer Windows app built only on gpui-go's public API (mordbard, a Go desktop app ported 1:1 from an HTML design) can show element-tree text in a presented, redrawing window, use embedded fonts, enforce a minimum window size, and hold a strong self-entity handle in listeners.

## Notes

- Found by mordbard's readiness spike (2026-10-19). Evidence and run logs: `../mordbard/.scratch/mordbard-v1/research/02-gpui-go-readiness.md` and `../mordbard/spikes/gpui-readiness/` (on mordbard's `ticket/09-gpui-readiness` branch until merged).
- Filed with the user's approval on 2026-10-19. Implementation by Roboco `pi` / `iroha/dashscope/glm-5.3` side chats, one ticket each, reviewed by the mordbard orchestrator before merging into `base-port`.
- Tickets 01 and 02 block mordbard's UI (no text otherwise). 03 blocks the 1:1 type spec; 04 and 05 have workarounds.

## Tickets

- [Present text drawn by the element tree through a consumer-owned present loop](issues/01-present-element-tree-text.md)
- [Stop Text elements leaking native shaping handles on every frame](issues/02-text-shaping-handle-leak.md)
- [Register fonts from memory (embedded font bytes)](issues/03-register-fonts-from-memory.md)
- [Enforce a window minimum size (WM_GETMINMAXINFO)](issues/04-window-minimum-size.md)
- [Give views a strong self-handle (Context.Entity() borrow expires before listeners run)](issues/05-strong-self-entity-handle.md)

## Decisions so far

- [Window minimum size](issues/04-window-minimum-size.md) — `WindowOptions.MinSize` (logical, zero = unconstrained) enforced via WM_GETMINMAXINFO's ptMinTrackSize (scaled + frame offset, re-derived per DPI) plus the SetBounds/initial-placement clamp; zero falls to DefWindowProcW exactly as before.
- [Give views a strong self-handle](issues/05-strong-self-entity-handle.md) — `Context.StrongSelf()` is the window-owned strong self-handle for listener capture (the conforming alternative to a retaining `Context.Entity()`); one lease per (window, view), released at window close.
- [Present element-tree text through the consumer present loop](issues/01-present-element-tree-text.md): `Window.GlyphAtlas()` exposes the window's text atlas to `Surface.DrawScene`/`RenderScene`/`RenderToImage`, and a nil/foreign atlas on a sprite scene answers the typed `ErrSceneSpriteAtlas` (pool/texture-index precondition) instead of a native bad-handle; hello example, README and the `internal/presentspec` real-window gate updated.

## Not yet specified

## Out of scope
