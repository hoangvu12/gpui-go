# What gpui-go is missing for building apps, compared with zgui and gpui-fast

Researched 2026-10-23 from each project's actual source: zgui at `ade8f27` and gpui-fast at `124f79b0`. The detailed tables, with file:line on both sides, are in [zgui.md](zgui.md) and [gpui-fast.md](gpui-fast.md). The orchestrator spot-checked claims against gpui-go's source: the layout reset each frame (`gpui/render.go:108`), solid-only backgrounds (`gpui/class.go:1119`), no `CachedView` users outside `gpui/view_cache.go`, no Base menu, and `Window.CapturePointer` (`gpui/interact.go:551`). These are proposals until the user accepts them.

## Combined ranking

Performance (how well apps run):
1. **Damage-limited rendering.** gpui-go clears and redraws the whole window every frame. zgui keeps the last frame, repaints only the changed rectangles, and scrolls by copying pixels. gpui-fast doesn't have this either. Needs native work; the size is XL.
2. **Automatic view reuse and splicing.** gpui-fast reuses every view whose inputs didn't change, and rebuilds only the nested view that did. gpui-go's `CachedView` is opt-in and unused. Go; XL.
3. **Retained layout and text measurement.** gpui-go resets the layout engine every frame, while gpui-fast and zgui keep it. Needs native layout ABI changes plus Go; L.
4. **Composition-only updates.** In zgui, transform and opacity changes reuse cached layer textures with no relayout or repaint. In gpui-go they rebuild the view, its layout and the scene. Go plus native; XL.
5. **Scroll layers and list-row retention** (gpui-fast tiles; zgui scroll-by-copy). Native plus Go; L.
6. **Smaller renderer items.** gpui-go repeats GPU state binds and re-tessellates paths every frame. Native; S-M.

Features (what apps can express):
1. **Transforms on any element: scale, rotate, skew.** Native.
2. **Gradients, masks, edge fades, clip insets, and rounded-corner clipping of children.** Native.
3. **Layout (FLIP) and shared-element animation, and enter/exit for keyed lists.** Go. In zgui this lives in its Luau layer.
4. **A menu component in Base:** dropdowns, context menus, submenus, checked items, keyboard navigation. Go.
5. **Drag callbacks:** drag-end, drag-leave, and drag position per target. Go, small.
6. **Window kinds and native prompts:** popup, floating and dialog windows, plus message boxes. Go.
7. **Embedded native views such as WebView2.** Both zgui and gpui-fast have them. Go plus native.
8. **Spellcheck.** zgui uses Windows `ISpellChecker`. Go.
9. **Theming:** per-subtree theme scopes, plus the system accent colour, high contrast and text size. Go.
10. **Accessibility under caching.** A reused `CachedView` drops its accessibility nodes. Go.

## Where gpui-go is already ahead
- Base's widget set: an editor with LSP, tables, trees, docking, pickers and plots.
- Motion in Go, which zgui's Rust core lacks.
- Element blur, filters and corner smoothing (gpui-fast lacks these).
- A waitable swapchain at max latency 1.
- Dragging files out to the OS.
- Pinch gestures.

## Corrections to earlier notes
- gpui-fast now schedules Windows frames on demand (#84), so gpui-go's idle loop is no longer an edge over it.
- gpui-fast's list layers are now on.
- Pointer capture exists in gpui-go. The zterm parity report listed it as missing.
