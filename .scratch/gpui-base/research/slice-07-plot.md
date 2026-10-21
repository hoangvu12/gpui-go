# Slice 07: Base `plot/**` inventory

Upstream: `upstream-base/ce/crates/base/src/plot` @ c0820693. Paths below are relative to `plot/` unless prefixed. The core Rust pin is `gpui-go/reference/ce-source/crates/gpui/src` (shown as `R:`). The Go core is `gpui-go/gpui` (shown as `G:`). This was read-only research. Nothing was built.
It agrees with the earlier `E2-plot.md` and re-verifies each claim with grep. The two notes that are new here are the `shape_line` arity drift and the missing `TextRun.Color`.

## Summary table

Line counts come from `wc -l`. The tests column is `#[test]` + `#[gpui::test]`. The classes are:

- PURE-GO: everything it needs is already in G.
- CORE+: needs a Go-side core addition (listed in the next section).
- NATIVE: has a native gap.

| file | lines | tests | class | missing core APIs | 3rd-party |
|---|---|---|---|---|---|
| mod.rs (`Plot` trait, `PlotMotion`, `Curve`, `origin_point`, `polygon`) | 263 | 0 | PURE-GO | none (`polygon` = `StrokePathBuilder`+`AddPolygon`+`Build`, mod.rs:247-263). Needs the Base `motion` slice (`Spring`, `Transition`, mod.rs:24) | – |
| element.rs (`PlotElement<P>`: layout, hitbox, hover cursor cell, tooltip overlay) | 200 | 0 | CORE+ | `App.Notify(EntityID)` (element.rs:189) | – |
| appear.rs (`PlotAppear`, `PlotAppearScope`, `track_appear`) | 591 | 3+8 | CORE+ | `TryGlobal[T]` (appear.rs:237). Tests need `simulate_next_frame` (appear.rs:399,423,499) | – (std `panic::Location`, `thread_local!` at :86) |
| axis.rs (`PlotAxis`, `AxisText`, `AxisLabelSide/Placement`, `axis_gutter`, `AXIS_GAP`) | 307 | 2 | CORE+ | `PaintPath(w,…)` (axis.rs:214). Labels via label.rs | – |
| grid.rs (`Grid` with `dash_array`) | 204 | 4 | CORE+ | `PaintQuad(w,…)` (grid.rs:83). Dashes are split into quads in Go (grid.rs:112-150), so no path dash is needed | – |
| hover.rs (`TooltipState`, `PlotHover`, `glide`, `hover_progress`, `pointer_spring`, `is_hover_entering`) | 261 | 1 | CORE+ | `UseKeyedState` (hover.rs:154, plus 2 more sites). Needs Base `motion`/`Theme` | – |
| label.rs (`Text`, `PlotLabel`, `measure_text_width`, `truncate_text_to_width`, `TEXT_*`) | 182 | 0 | CORE+ | `ShapeLine` with `Width` and `Paint` (label.rs:23-42, :161-179) | – |
| path_cache.rs (`PathCache`, `PathCaches`, `ShapeKey`) | 197 | 2 | CORE+ | `UseKeyedState` (path_cache.rs:82) | std `DefaultHasher` |
| scale.rs + scale/sealed.rs (`PlotValue`, `Scale<T>`) | 35+7 | 0 | PURE-GO | – | num-traits (`Num`, `ToPrimitive`), optional rust_decimal |
| scale/band.rs (`ScaleBand`) | 266 | 7 | PURE-GO | – | num-traits `Zero` (band.rs:5,105) |
| scale/linear.rs (`ScaleLinear`) | 90 | 4 | PURE-GO | – | itertools `minmax_by` (linear.rs:3,22) |
| scale/ordinal.rs (`ScaleOrdinal<D,R>`) | 104 | 3 | PURE-GO | – | – |
| scale/point.rs (`ScalePoint`, `tick_at`, `nearest_index`) | 217 | 11 | PURE-GO | – | – |
| shape.rs (re-exports) | 20 | 0 | PURE-GO | – | – |
| shape/arc.rs (`Arc`, `ArcData`, `centroid`, `contains`, `paint[_cached]`) | 326 | 4 | CORE+ (NATIVE for gradient fills) | `PaintPath` (arc.rs:238,251). Uses `FillPathBuilder`, `MoveTo`, `LineTo`, `ArcTo`, `Build` (arc.rs:149ff) | – |
| shape/area.rs (`Area<T>`, fill + stroke line) | 230 | 0 | CORE+ (NATIVE for gradient fills) | `PaintPath` (area.rs:212-227). Uses `CubicBezierTo` and `Close` | – |
| shape/bar.rs (`Bar<T>`, `BarAlignment`) | 291 | 0 | CORE+ (NATIVE for gradient fills) | `PaintQuad` (bar.rs:235). `Corners` and `fill` are present | – |
| shape/line.rs (`Line<T>`, dots) | 290 | 1 | CORE+ | `PaintPath` (line.rs:239,253) and `PaintQuad` (dots, line.rs:245,256) | – |
| shape/pie.rs (`Pie<T>`, `arcs`) | 134 | 2 | PURE-GO | – | – |
| shape/radial_line.rs (`RadialLine<T>`) | 305 | 2 | CORE+ | `PaintPath` (radial_line.rs:238,241) and `PaintQuad` (:244). Uses `AddPolygon` and `Close` | – |
| shape/sankey.rs (`Sankey` layout, `SankeyGraph`/`Link`/`NodeLayout`/`Error`, `sankey_link_path`) | 1327 | 12 | PURE-GO | none. `sankey_link_path` only builds a Path (sankey.rs:905-934: `FillPathBuilder`, `CubicBezierTo`, `Close`) | – |
| shape/stack.rs (`Stack<T>`, `StackSeries`, `StackPoint`) | 193 | 1 | PURE-GO | – | – |
| **Total** | **6040** | **67** (59 `#[test]` + 8 `gpui::test`) | 11 PURE-GO / 12 CORE+ | | |

Notes:

- The arc, line and radial_line tests call `PathBuilder::build`. In Go, `Build` runs the native `path_script` tessellator (G:path.go:378), so those tests need the DLL loaded.
- Tests per group: scales 25, sankey 12, stack + pie + arc 7, grid + axis + path_cache 8, shapes 3, hover + appear 12.

## Core API usage: already present in G

- **PathBuilder: every method plot uses is present.**
  - Builders: `stroke(w)` maps to `StrokePathBuilder(width)` (G:path.go:217), and `fill()` maps to `FillPathBuilder()` (:212).
  - Segments: `move_to` maps to `MoveTo` (:244), `line_to` to `LineTo` (:252), `cubic_bezier_to` to `CubicBezierTo` (:271), and `arc_to` to `ArcTo(rx,ry,rot,large,sweep,x,y)` (:283).
  - Others: `add_polygon` maps to `AddPolygon` (:308), `close` to `Close` (:322), and `build` to `Build` (:378).
  - Unused but present: `CurveTo` (:261), `DashArray` (:232), `WithStyle` (:224).
  - Plot never uses path dashes or custom stroke options. Grid dashes are quads.
  - The adapter difference is that Go takes `x, y float32` where Rust takes a `Point`.
- **Path fields:** `Path.Vertices` and `Path.Bounds` are exported and mutable (G:path.go:160-178). `PathCache::translated` (path_cache.rs:31-38) needs this.
- **Element layout and hitboxes:**
  - `Element` trait: G:element.go:287.
  - `request_layout`: `RequestElementLayout` (G:element.go:1530).
  - `with_element_state`: `WithElementState` (G:element.go:1379).
  - `insert_hitbox`: G:mouseevent.go:477. `Hitbox.IsHovered`: :249.
  - `on_mouse_event`: G:mouseevent.go:459 (type-switch on `*MouseMoveEvent`).
  - `mouse_position`: `TrackedMousePosition` (G:mouseevent.go:531). Do not use `Window.MousePosition`, which calls the OS.
- **Frames and views:**
  - `request_animation_frame`: `RequestAnimationFrame(w)` (G:imagecache.go:958).
  - `current_view`: `CurrentView(w)` (G:view.go:325).
  - `prepaint_as_root`: G:element.go:672.
- **Text style and paint helpers:**
  - `text_style`: `WindowTextStyle(w)` (G:element.go:1154).
  - `PaintQuad`/`fill`/`Corners`/`BorderStyle` value types: G:paint.go.
  - `TextAlign`: G:div.go:54.
- **Tests:** `SetReduceMotion` is at G:app.go:287. `AdvanceClock` and `RunUntilParked` are in G:testapp.go.

## Core additions needed (Go-side only, no native code)

1. **`PaintPath(w *Window, p *Path, bg Background) error`**
   - Rust: `Window::paint_path` (R:window.rs:4715).
   - Go today: only `(*Scene).PaintPath(path, bg, *PaintContext)` (G:path.go:455) exists. The frame scene and paint context are reachable only through the unexported `currentFrame(w)` (G:element.go:1090).
   - `PaintSvg` already follows this pattern internally (G:svgelement.go:383). The new wrapper can be modelled on it.
   - Used at 11 sites: axis, arc, area, line and radial_line.
2. **`PaintQuad(w *Window, pq PaintQuad) error`**
   - Rust: `Window::paint_quad` (R:window.rs:4634, :4639).
   - Go today: only `(*Scene).PaintQuad` (G:scene.go:521). It is called internally at G:div.go:1670.
   - Used by grid, bar, and the line/radial dots.
3. **`ShapeLine(w, text, fontSize, runs) (*ShapedLine, error)`, with `Width()` and `Paint(w, app, origin, lineHeight, align, color)`**
   - Rust: `WindowTextSystem::shape_line` (R:text_system.rs:325, 3 params), `ShapedLine::width` (R:text_system/line.rs:29), and `ShapedLine::paint` (R:text_system/line.rs:100).
   - Go today: `(*Window).ShapeText` returns metrics only (G:testwindow.go:548). Painting goes through the frame-private `(*Scene).PaintShapedText` (G:draw_text.go:734, used at G:div.go:363).
   - Arity drift: Base calls `shape_line(text, size, &[run], None)` with 4 args (label.rs:39-41). The core pin takes 3. Drop the trailing `None`, which is a force-width argument.
   - Colour: Go `TextRun` has no `Color` field (G:text.go:453-461), while Rust's has `color` (R:text_system.rs:489-503). Pass the colour to `Paint` instead.
4. **`UseKeyedState[S](w, app, key ElementID, init) Entity[S]`**
   - Rust: R:window.rs:4093.
   - Go today: absent from G.
   - Used by path_cache.rs:82 and hover.rs:154 (plus 2 more sites). Other Base slices need it too.
5. **`(*App).Notify(EntityID)`**
   - Rust: `App::notify(entity_id)` (R:app.rs:2920), called as `cx.notify(view)` at element.rs:189.
   - Go today: only `(*Context[S]).Notify` (G:context.go:65) and the unexported `applyNotify` (G:app.go:521).
6. **`TryGlobal[T](app) (T, bool)`**
   - Rust: R:app.rs:2120.
   - Go today: `Global[T]` panics when the global is absent (G:app.go:575).
   - Used at appear.rs:237.
7. **Test helper (verify first): `simulate_next_frame`.**
   - It is not in G. `DrawWindowFrame(w)` (G:render.go:58) is the likely equivalent.
   - Only appear.rs's 8 `gpui::test`s need it.

Plot also depends on the Base `motion` slice (`Spring`, `Transition`, `Easing`, `Presence`, `spring()`, `transition()`) and on Base `Theme`/`PlotTheme`. These are not core work. Element, hover and appear must wait for them.

## Native gaps

There is one gap: **gradient and pattern `Background`s.**

- The plot fill parameters take `impl Into<Background>`, and the docs invite `gpui::linear_gradient` (bar.rs:34-39, :141-146; R:color.rs:375).
- Go `Background` is solid-only (G:paint.go:168-174). `Scene.PaintPath` keeps only `.Solid` (G:path.go:464).
- The frozen DLL rejects any colour with `tag != 0`: `check_color` (reference/native/src/scene.rs:2506-2510). Tag 0 is documented as "the only supported value… gradient and pattern tags are reserved" (scene.rs:164-167).
- Plot itself never builds a gradient, so solid charts and all 67 tests are unaffected.
- Mitigation: accept `Background`, paint solid, and document the gap.

Everything else is covered natively. Tessellation (lyon fill and stroke, cubic curves, arcs, dash) goes through `path_script` (reference/native/SCENE_ABI.md:153, :237). The per-path vertex cap is `MAX_SCENE_PATH_VERTICES` (SCENE_ABI.md:253, 1,048,576), which is ample. Every `Build` is an FFI call, so port `PathCache` faithfully to amortise the cost.

## Third-party usage

| crate | exact usage | Go replacement |
|---|---|---|
| num-traits | `PlotValue: Num + ToPrimitive` (scale.rs:12,19) and `Zero::is_zero` (band.rs:5,105) | Generic constraint `~float32 \| ~float64` plus a `toF32` helper. `len == 0` replaces `is_zero` |
| itertools | `minmax_by(partial_cmp)` over the domain (scale/linear.rs:3,22) | 5-line min/max loop |
| rust_decimal (optional feature `decimal`) | `impl PlotValue/Sealed for Decimal` (scale.rs:24, sealed.rs:7) | Skip in v1 |
| std `DefaultHasher` | `ShapeKey` hashing (path_cache.rs:1) | `hash/maphash` or `hash/fnv` over the f32 bits |
| std `panic::Location`, `thread_local!` | appear scope (appear.rs:12, :86, :153) | `runtime.Caller`. Keep scope state per window, not as a package global |

No Go third-party module is needed.

## Suggested ticket split

1. **T1 plot-core-gaps (S, `gpui/`)**
   - Scope: core additions 1-6 above, plus a test for each, plus a decision on item 7.
   - Shared with other Base slices: Notify and TryGlobal are Base-wide, so de-duplicate them.
2. **T2 plot-pure (M/L, `base/plot`, no core dependency)**
   - Scope: scale/* and PlotValue (25 tests), stack and pie (3 tests), sankey layout and `sankey_link_path` (12 tests), and arc geometry (`centroid`/`contains`, 4 tests).
   - Build-calling tests need the DLL.
   - Sankey is the largest piece (~930 non-test lines). It needs exact float parity.
3. **T3 plot-render + interaction (M/L, needs T1 and Base motion/theme)**
   - Scope: mod, path_cache, grid, label, axis, the line/area/radial_line/bar/arc paint paths, element, hover, and appear plus `PlotAppearScope` (23 tests in total).
   - Keys: key the `Appeared` map by `GlobalElementID.String()`, because `GlobalElementID` holds a slice and so cannot be a Go map key (G:element.go:103,126).
