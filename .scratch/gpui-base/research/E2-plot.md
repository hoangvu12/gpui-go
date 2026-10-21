# E2 — Base `plot/**` + `PlotTheme` port research

Upstream: `upstream-base/ce/crates/base/src/plot` @ c0820693 (paths below are relative to `crates/base/src`). Go core: `gpui-go/gpui` (pin 254b5dbd). This was read-only research. Nothing was built or executed. Status: COMPLETE. These claims were checked against the source: tessellation is native, Background is solid-only and the native side rejects `tag != 0`, and `UseKeyedState`, `TryGlobal`, `App.Notify(EntityID)` and `SimulateNextFrame` are absent from gpui/.

## 1. Inventory

LOC is total / non-test (the non-test count is the lines before `#[cfg(test)]`). "pub" counts `pub` lines.

| Module | LOC | non-test | pub | tests | What the tests exercise |
|---|---|---|---|---|---|
| plot/mod.rs | 263 | 262 | 25 | 0 | — (`Plot` trait, `PlotMotion`, `Curve`, `origin_point`, `polygon`) |
| plot/element.rs | 200 | 199 | 2 | 0 | — (`PlotElement<P>`: layout, hitbox, tooltip cursor cell, overlay) |
| plot/appear.rs | 591 | 258 | 7 | 11 (8 `gpui::test`) | appear runs once over the theme duration; reduced motion skips it; a new generation replays; `PlotAppearScope` keeps a finished appear across a remount, replays an unfinished one, forgets when unmounted; stagger math. Uses `advance_clock`, `simulate_next_frame`, `set_reduce_motion`, `Theme` global |
| plot/hover.rs | 261 | 225 | 15 | 1 | `PlotHover` readers (progress, entering, state) |
| plot/axis.rs | 307 | 253 | 25 | 2 | builder order does not move labels; `axis_gutter` fits default labels (text measuring) |
| plot/grid.rs | 204 | 150 | 7 | 4 | dash splitting: solid = 1 segment, alternating dashes clip at the end, odd array repeats like SVG, 1px line box |
| plot/label.rs | 182 | 181 | 19 | 0 | — (`Text`, `PlotLabel`, `measure_text_width`, `truncate_text_to_width`) |
| plot/path_cache.rs | 197 | 133 | 12 | 2 | builds once per key and translates vertices/bounds to each origin; `ShapeKey` hashing |
| plot/scale.rs + sealed.rs | 42 | 40 | 7 | 0 | — (`PlotValue` sealed f32/f64[/Decimal], `Scale<T>`) |
| scale/band.rs | 266 | 161 | 8 | 7 | band width/step/count/dedup/zero/range start, max band width |
| scale/linear.rs | 90 | 46 | 2 | 4 | linear map, unordered domain, f32, empty |
| scale/ordinal.rs | 104 | 66 | 6 | 3 | ordinal map, unknown, colors |
| scale/point.rs | 217 | 90 | 3 | 11 | tick, range, empty/single, `nearest_index` (offset, empty range), reversed range |
| shape.rs | 20 | 19 | 8 | 0 | re-exports |
| shape/arc.rs | 326 | 255 | 16 | 4 | defaults, builder, centroid, `contains` hit test |
| shape/area.rs | 230 | 229 | 11 | 0 | — |
| shape/bar.rs | 291 | 290 | 16 | 0 | — |
| shape/line.rs | 290 | 260 | 16 | 1 | `path()` returns Some, dot count (calls `PathBuilder::build`, so it needs the native tessellator) |
| shape/pie.rs | 134 | 98 | 7 | 2 | angles, zero values |
| shape/radial_line.rs | 305 | 248 | 16 | 2 | points; fill/stroke path presence and dot count (needs native build) |
| shape/sankey.rs | 1327 | 934 | 47 | 12 | builder, chain layout, large topology, alignment, link offsets, imbalanced widths, sqrt scale, value conservation, vertical centering, stagger, circular link error, degenerate |
| shape/stack.rs | 193 | 137 | 14 | 1 | basic stack y0/y1 |
| **Total plot** | **6040** | **~4,330** | | **~68** | |
| theme.rs `PlotTheme` (theme.rs:96-119, field theme.rs:22) | ~25 | | 4 | 0 | — (`new`, `with_motion`, `motion`; default is motionless) |

## 2. gpui core API usage → Go status

| Rust API (where it is used) | Status | Go evidence / what to add |
|---|---|---|
| `PathBuilder::stroke/fill`, `move_to`, `line_to`, `cubic_bezier_to`, `arc_to`, `add_polygon`, `close`, `build` (mod.rs polygon; axis.rs:209; arc/area/line/radial_line/sankey) | PRESENT | gpui/path.go:212 `FillPathBuilder`, :217 `StrokePathBuilder`, :244 `MoveTo`, :252 `LineTo`, :271 `CubicBezierTo`, :283 `ArcTo`, :308 `AddPolygon`, :322 `Close`, :378 `Build`. The Go API takes x,y floats rather than a Point (an adapter detail). |
| `dash_array` | PRESENT (not needed) | path.go:232 `DashArray`. Plot's grid dashes are Go-side quad segments (grid.rs:108-150), not path dashes. |
| Tessellation (lyon) | PRESENT, **native-side** | path.go:16-23 and :410-414: `Build` → `sceneService().TessellatePath` → the native `path_script` entry (reference/native/SCENE_ABI.md:153, :237). The DLL ships the pinned lyon code, so plot needs no new native code for this. Hazard: every `Build` is an FFI call. `PathCache` (path_cache.rs) amortises it, so port it faithfully. Shape unit tests that call `.path()` need the DLL loaded. |
| `Path.vertices[].xy_position`, `Path.bounds` mutation (path_cache.rs:31-38 `translated`) | PRESENT | path.go:160-178: `Path.Vertices []PathVertex{X,Y,S,T}`, `Path.Bounds` are exported and mutable. |
| `window.paint_path(path, impl Into<Background>)` (11 sites) | **MISSING-GO** (wrapper) | Only `(*Scene).PaintPath(path, bg, ctx)` exists (path.go:455). An external package's element cannot reach the frame's scene or `PaintContext`, because `currentFrame(w)` is unexported (PaintSvg uses it internally at svgelement.go:390). Add `PaintPath(w *Window, p *Path, bg Background) error`. |
| `window.paint_quad(fill(..))` / `quad(..)` (grid.rs:83, bar.rs:235, line/radial dots) | PaintQuad struct and `Fill` PRESENT; window paint **MISSING-GO** | paint.go:234 `PaintQuad`, :256 `Fill`, :198 `Corners`, :215 `BorderStyle`. Painting from outside goes through `frame.scene.PaintQuad` (div.go:1670), which is unexported. Add `PaintQuad(w *Window, pq PaintQuad) error`. There is no `Quad(...)` helper, but a struct literal is enough. |
| `Background` with `linear_gradient` (bar.rs:145 docs; every `fill: impl Into<Background>` in area/bar/arc) | **MISSING-NATIVE** (gradient only) | Go `Background` is solid-only (paint.go:168-174: "gradients and patterns are later work"). `PaintPath` keeps only `color.Solid` (path.go:464). The native ABI rejects any non-solid colour: `check_color` returns ERR_BAD_VALUE when `tag != 0` (reference/native/src/scene.rs:2506-2510). `GpuiGoSceneColor` reserves the gradient tags (scene.rs:164-167, SCENE_ABI.md:90). Solid fills work. Gradient area and bar fills cannot be expressed. |
| Text: `window.text_system().shape_line(text, size, &[TextRun], None)`, `ShapedLine::width`, `ShapedLine::paint(origin, size, align, None, ..)` (label.rs:23-42, :170-179) | Measuring PRESENT-ish; painting **MISSING-GO** | Measuring: `(*Window).ShapeText(text, TextStyle, wrap)` (testwindow.go:548) and `WindowTextStyle(w)` (element.go:1154). There is no `ShapeLine` with TextRun color or font, and no public single-line paint from an element. `(*Scene).PaintShapedText` (draw_text.go:734) needs Scene, TextSystem, Atlas and PaintContext, all frame-private. Add `ShapeLine(w, text, fontSize, runs) (*ShapedLine, error)` with `Width()`, plus `(*ShapedLine).Paint(w, app, origin, lineHeight, align)`. `TextAlign` is PRESENT at div.go:54. `FontWeight` is a plain float32 (text.go:91). |
| `Element` impl, `window.request_layout(Style{size: full})` (element.rs:47-58, appear.rs:174) | PRESENT | element.go:272 `Element[L,P]`; element.go:1530 `RequestElementLayout`. |
| `window.insert_hitbox`, `Hitbox::is_hovered` (element.rs:89, :167) | PRESENT | mouseevent.go:477 `InsertHitbox`, :249 `IsHovered`. |
| `window.on_mouse_event::<MouseMoveEvent>` (element.rs:182) | PRESENT | mouseevent.go:459 `OnMouseEvent(func(event any, phase, w, app))`. Type-switch on `*MouseMoveEvent`. |
| `window.mouse_position()` (element.rs:93, :168) | PRESENT | mouseevent.go:531 `TrackedMousePosition`. Do not use `Window.MousePosition` (window.go:296), which queries the OS and returns an error. |
| `window.with_element_state` (element.rs:22, appear.rs:165) | PRESENT | element.go:1379 `WithElementState[S,R]`. |
| `window.use_keyed_state(key, cx, init) -> Entity<T>` (path_cache.rs:52, hover.rs:154, :210, :222) | **MISSING-GO** | Not found in gpui/. Add `UseKeyedState[S](w, app, key ElementID, init) Entity[S]`, an element-state-backed entity (Base-wide need). |
| `window.current_view()` + `cx.notify(view)` (element.rs:166, :189) | `CurrentView` PRESENT; notify by id **MISSING-GO** | view.go:325 `CurrentView(w) (EntityID, bool)`. The only notify paths are `(*Context[S]).Notify` (context.go:65) and the unexported `applyNotify` (app.go:521). Add `(*App).Notify(EntityID)`. |
| `window.request_animation_frame()` / `window.refresh()` (element.rs:177, appear tests) | PRESENT | imagecache.go:958 `RequestAnimationFrame(w)`, which wraps the unexported `requestRefresh`. |
| `AnyElement::prepaint_as_root`, `prepaint`, `paint` (element.rs:110, appear.rs) | PRESENT | element.go:672 `PrepaintAsRoot`, :637 `Prepaint`, :645 `Paint`. |
| `cx.try_global::<Theme>()` / `Theme::global` (appear.rs:229, hover.rs:108, :156) | `Global[T]` PRESENT; try **MISSING-GO** | app.go:575 `Global[T]` panics when absent. Add `TryGlobal[T](a) (T, bool)`. |
| `ElementId::Integer`, `GlobalElementId` as a HashMap key (appear.rs:245, Appeared map) | PRESENT (with a workaround) | element.go:61 `ElementID`, :103 `GlobalElementID`. `GlobalElementID` holds `Path []ElementID`, so it is NOT comparable and cannot be a Go map key. Key the `Appeared` map by `GlobalElementID.String()` (element.go:126, exported) instead. |
| `content_mask` / element opacity in paint | PRESENT (implicit) | The wrapper must read `currentFrame(w).paint`, as PaintSvg does. |
| Tests: `TestAppContext.advance_clock`, `run_until_parked`, `simulate_next_frame`, `set_reduce_motion`, `open_window` | Mostly PRESENT | testapp.go:25 `NewTestApp`, :50 `RunUntilParked`, :54 `AdvanceClock`; app.go:287 `SetReduceMotion`. I found no `SimulateNextFrame`; `DrawWindowFrame(w)` (render.go:58) is the likely equivalent. Check this in the ticket. |
| Base `crate::motion` (`Spring`, `Transition`, `Easing`, `Presence`, `spring()`, `transition()`, `TransitionId`) (mod.rs:24, appear.rs:21, hover.rs:10) | Cross-slice dependency | Not core gpui. It comes from the Base motion slice and must land before element, hover and appear. |

## 3. Third-party dependencies → pure Go

| Dep | Use | Go route |
|---|---|---|
| `num_traits` (Num, ToPrimitive, Zero) | `PlotValue`, band.rs | A generic constraint `~float32 \| ~float64` plus a `toF32` helper. |
| `itertools::minmax_by` | scale/linear.rs:22 | A hand-written loop (5 lines). |
| `rust_decimal` (optional feature `decimal`) | `PlotValue for Decimal` | Skip for v1. A later version could accept an interface value (`ToF32()`), with no dependency. |
| `std DefaultHasher` | `ShapeKey` | `hash/maphash` or `hash/fnv` over f32 bits. Only equality within one process matters. |
| `std::panic::Location` | appear scope source location | `runtime.Caller` if needed (it returns None upstream). |

No dependency needs a third-party Go module.

## 4. Sizes and hazards

| Group | Go size | Hazards |
|---|---|---|
| scales (band, linear, ordinal, point, sealed) | S/M (~450 + tests) | Generics over `PlotValue`. Rust `Option<f32>` returns become `(float32, bool)`. Floating-point bit-equality in tests. |
| stack + pie | S (~250) | Rust generic `T` data with closures (`Box<dyn Fn>`) becomes Go `func` fields. |
| sankey | L (~1,100 + ~400 tests) | The largest pure algorithm (relaxation iterations, circular-link detection, alignment). It needs exact float parity with the 12 tests. `sankey_link_path` uses `PathBuilder`. |
| mod + path_cache + grid + label + axis | M (~800) | Adding a translated path must copy the slice (Rust `clone`). Labels need the new ShapeLine wrapper. `truncate_text_to_width` binary-searches over char boundaries, so use rune boundaries. |
| shapes: line, area, radial_line, arc, bar | M (~1,100) | Curve::Natural builds cubic control points. ArcTo flag semantics. Gradient fills must fall back to solid until native gradients exist. |
| element + hover + appear (+ PlotAppearScope) | M (~700) | Depends on Base motion. The thread-local `SCOPES` stack becomes per-window Go state, not a package global, because of goroutine safety. The tooltip cursor `Rc<Cell>` lives in element state. Mouse listener closures capture a hitbox copy. |
| PlotTheme | S (~40) | Goes in the theme slice and needs `PlotMotion`. |

Total is about 4.5–5k Go lines plus about 2k test lines. That is XL overall; every ticket below is S, M or L.

## 5. Proposed ticket slices (bounded)

1. **P0 core-gaps (S, gpui/)**: exported `PaintPath(w,…)`, `PaintQuad(w,…)`, `ShapeLine` + `ShapedLine.Width/Paint(w,…)`, `UseKeyedState[S]`, `(*App).Notify(EntityID)`, `TryGlobal[T]`. This is shared with other Base slices; de-dupe it with them.
2. **P1 scales (S/M)**: band, linear, ordinal and point, with 25 tests.
3. **P2 stack + pie + arc geometry (S)**: arc `contains`/centroid math, with 7 tests. The arc path uses Build.
4. **P3 sankey (L)**: layout and `sankey_link_path`, with 12 tests.
5. **P4 path infra + static layers (M)**: mod (`Plot` interface, `Curve`, `polygon`, `origin_point`), path_cache, grid, label, axis, with 8 tests. Needs P0.
6. **P5 shapes (M)**: line, area, radial_line, bar, arc paint and `paint_cached`, with 3 tests. Needs P0 and P4.
7. **P6 interaction (M)**: `PlotElement`, hover, appear and `PlotAppearScope`, plus `PlotMotion`/`PlotTheme`, with 9 tests. Needs P0, P4 and the Base motion slice.

## 6. MISSING-GO (deduplicated)

1. `PaintPath(w *Window, path *Path, bg Background) error`: a window-level paint wrapper over the frame scene and `PaintContext` (path.go:455 is Scene-only, and `currentFrame` is unexported).
2. `PaintQuad(w *Window, pq PaintQuad) error`: the same pattern (scene.go:521 is Scene-only).
3. `ShapeLine(w, text, fontSize, []TextRun)` → `*ShapedLine` with `Width()` and `Paint(w, app, origin, lineHeight, TextAlign)`. Today there is only `Window.ShapeText` (facts only) and the frame-private `Scene.PaintShapedText`.
4. `UseKeyedState[S](w, app, key, init) Entity[S]`, the `window.use_keyed_state` equivalent.
5. `(*App).Notify(EntityID)`, so a non-Context listener can notify `CurrentView(w)`.
6. `TryGlobal[T](app) (T, bool)`.
7. (Verify) a `SimulateNextFrame`-style test helper, or confirm that `DrawWindowFrame` covers it.
8. (Cross-slice, not core) Base `motion`: `Spring`, `Transition`, `Easing`, `Presence`, `spring()`, `transition()`.

## 7. MISSING-NATIVE (frozen DLL)

1. **Gradient and pattern `Background` for paths and quads.** The native scene validates every colour with `check_color` and rejects `tag != 0` (`reference/native/src/scene.rs:2506-2510`). `GpuiGoSceneColor` documents tag 0 as "the only supported value… gradient and pattern tags are reserved" (`scene.rs:164-167`; `SCENE_ABI.md:90`). The native side converts back through `gpui::Background::from(hsla)` only (`scene.rs:3746-3753`). Plot impact: `linear_gradient` fills on area, bar and arc (bar.rs:145, area.rs:212/224) cannot render. **Mitigation:** accept `Background`, paint `Solid`, and document the gap. It is not a blocker for any test or for solid-colour charts.
2. **No other native gaps.** Path tessellation (lyon, fill and stroke, arcs, dashes) is already native and exposed through `path_script` (path.go:16-23, :410-414; SCENE_ABI.md:153, :237-254). The vertex limit is `MAX_SCENE_PATH_VERTICES` = 1,048,576 per path (SCENE_ABI.md:253), which is ample for plots.
