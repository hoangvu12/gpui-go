# 01 · Visual primitives: how the browser behaves, and how gpui-go differs

Ticket: `.scratch/browser-parity/issues/01-visual-primitives.md`. Researched 2026-10-23. Read only, nothing built.

Sources and pins:
- **Chromium** `0d4dcd4f` (sparse checkout, `%TEMP%\research-src\chromium`). Paths below are relative to `third_party/blink/renderer/` unless they start with `cc/`, `components/` or `ui/`.
- **Skia** `e71ecd07` (`%TEMP%\research-src\skia`).
- **gpui-go**, this worktree (`research/browser-parity` @ `8f5ad4d`): `gpui/` and `reference/native/src/`.
- **GPUI-CE shaders.** The native DLL draws with `gpui_render` from `reference/ce-source`, pinned at `254b5dbd`. That checkout isn't in this worktree, so I read the cargo git cache instead: `~/.cargo/git/checkouts/gpui-ce-6758b1db058c6497/beb5a6d`, which is 7 days newer than the pin. Shader claims marked **[V\*]** come from that cache; the pinned shader could differ in detail. Paths are relative to `crates/gpui_render/src/`.
- **Specs:** W3C/WHATWG drafts, linked by section. Where Chromium quotes the spec in a source comment I note it; I did not fetch the spec text separately.

Markers: **[V]** means I read it in source. **[I]** means it's inferred and not traced end to end. Sizes: S ≤ 2 days, M ≤ 1 week, L ≤ 3 weeks, XL more than that.

---

## Ranked gaps

| # | Gap | Who notices | Work | Size |
|---|-----|-------------|------|------|
| 1 | Clips are axis-aligned rectangles only, so rounded corners don't clip children | Every card or avatar with an image or coloured child | native + Go | M–L |
| 2 | Opacity is folded into each primitive, not applied to the group | Any translucent element whose children overlap | native + Go | M |
| 3 | Elements can't be transformed (translate, scale, rotate, skew, transform-origin) | Every animation, and hit-testing too | native + Go | L–XL |
| 4 | Blur sizes use the wrong scale: box-shadow blur is 2× CSS, `filter: blur` is ½ CSS | Every ported shadow and blur looks wrong | Go | S |
| 5 | Box-shadow shape: drop shadows show through translucent boxes, and spread doesn't grow the radii | Glass or translucent cards; `0 0 0 4px` focus halos on rounded boxes | Go + native | S |
| 6 | Gradients: the Go side and the ABI are solid-only, though the shared shader already does 2-stop linear | Every gradient | Go + native ABI, then native | S, then L |
| 7 | Filter and backdrop-filter semantics: blur only, edge handling, depth limit, what the filter covers | Glass UIs, disabled-looking (grayscale) states | native + Go | M |
| 8 | No mask-image or edge fades (`OverflowFade` is stored but never painted) | Fading scroll edges, masked icons | native, after #2 | M |
| 9 | No clip-path | Shaped avatars, reveal animations | native, after #1 | M |
| 10 | No mix-blend-mode or isolation | Design-tool-like UIs | native, after #2 | M–L |
| 11 | Outline and focus ring: no offset, wrong paint order, no `auto` style | Keyboard focus visuals | Go | S |
| 12 | Images: `object-fit: none` isn't centred, no object-position, fits the border box, no image-rendering | Image grids, pixel art, icons | Go + native | S |
| 13 | Corner radii are clamped per corner, not scaled together as CSS does | Asymmetric radii on small boxes | Go | S |

#1, #2 and #3 are the foundation. Masks (#8), clip-path (#9) and blend modes (#10) all reuse the group target from #2 and the coverage mask from #1, so build those first. #4 and #6 (first step) are a few lines each and would make ported CSS look right immediately.

---

## 1. Border-radius clipping of descendants

**Spec.** [css-overflow-3 §corner-clipping](https://drafts.csswg.org/css-overflow-3/#corner-clipping): when overflow is not `visible`, content is clipped to the padding box, with its corners shaped by the border radii (reduced by the border widths). If one axis is `clip` and the other is `visible`, the clip isn't rounded. Chromium quotes this rule at `core/paint/paint_property_tree_builder.cc:3109-3112` [V].

**Chromium.**
- `UpdateInnerBorderRadiusClip`, `paint_property_tree_builder.cc:3257-3299`, builds a separate clip node from `ContouredBorderGeometry::PixelSnappedContouredInnerBorder`: the padding box with inner radii [V]. The rectangular overflow clip comes from `UpdateOverflowClip`, `:3357`, via `OverflowClipRect` [V].
- In cc, a rounded clip becomes a "fast rounded corner" effect, or a render surface with `RenderSurfaceReason::kRoundedCorner` (`cc/trees/property_tree_builder.cc:371`, `platform/graphics/compositing/property_tree_manager.cc:1142-1163`) [V].
- Overflow values are computed first: `visible` paired with a scrolling axis becomes `auto` (`core/css/resolver/style_adjuster.cc:654-705`) [V].

**gpui-go.**
- The content mask is a plain rectangle: `content_mask: GpuiGoSceneBounds` in every scene record (`reference/native/src/scene.rs:210-211`) [V]. The shaders clip with four rectangle distances (`shaders/common.rs:389-402` `clip_distances`/`is_clipped`) [V\*].
- `Style.OverflowMask` returns a `*Bounds` with no radii (`gpui/style.go:1181-1205`) [V].
- It insets by the border only when the border colour is non-transparent (`gpui/style.go:1189`) [V]. CSS always clips to the padding edge, so a box with a transparent 4px border clips 4px too far out.
- It keeps the pinned "x visible, y clipped" mixed mask (`style.go:1196-1200`) [V]. CSS would compute `visible` to `auto` here.
- The border paints after the children (`gpui/div.go:1960-1974`) [V]. CSS paints it before them (CSS2 Appendix E). This partly hides the missing rounded clip along straight edges, but children still poke out of the corners.
- Image and polychrome sprites carry their own corner radii (`scene.rs:575-594`) [V]. So an `img` rounds itself, but a div's children don't.

**Gap and fix.**
- Native: add corner radii and smoothing to the content mask. One option is an SDF test in every fragment shader; the other is a coverage or stencil mask texture, which nested rounded clips need anyway. Then intersect masks in Go (`WithContentMask`).
- The single-rrect-in-shader version is M. Nested rounded clips (a rounded scroller inside a rounded card) need a mask stack or texture, which brings it to L.
- Go (S): always inset by the border widths; compute the overflow values the CSS way.

## 2. Opacity and isolation groups

**Spec.** [css-color-4 §transparency](https://drafts.csswg.org/css-color-4/#transparency): opacity applies to the element and its descendants as one offscreen group, composited once. [compositing-1 §isolation](https://drafts.fxtf.org/compositing-1/#isolation) says `isolation: isolate` forces a group.

**Chromium.**
- The effect node gets `state.opacity = style.Opacity()` (`paint_property_tree_builder.cc:2265`) [V]. Isolation is a stacking-context trigger (`core/style/computed_style.h:1827`) [V].
- cc gives the effect its own render surface when needed (`RenderSurfaceReason::kOpacity`, `cc/trees/effect_node.cc:52`) [V]. With a single drawing child it skips the surface and multiplies alpha directly [I], which gives the same result.

**gpui-go.**
- `WithElementOpacity` multiplies a scalar into the paint context (`gpui/element.go:1355-1364`, `gpui/scene.go:594-596`) [V]. Every quad, shadow and underline folds it into its own colour (`gpui/paint.go:426-431, 553-560, 616`) [V].
- So at 50% opacity a child's background shows its parent's background *through* it, and overlapping text and borders double up. This is the classic per-primitive-alpha artifact.

**Gap and fix.**
- The native filter-group machinery already renders a subtree to an isolated target and composites it with an opacity (`scene.rs:1770-1790`, `renderer/draw.rs:1528-1590`) [V]. An "opacity group" is that path with an identity filter.
- Two limits apply. Only two nested isolated targets are allowed (`MAX_FILTER_GROUP_DEPTH = 2`, `scene.rs:150`), and they are full-window textures [V].
- Fix: add an opacity-only group with a plain composite (no blur), skipped when the subtree is one primitive. Remove or raise the depth cap with a target pool. Native M plus Go S.
- This group is the base for #8 and #10.

## 3. Transforms (2D and 3D, transform-origin)

**Spec.**
- [css-transforms-1 §transform-rendering](https://drafts.csswg.org/css-transforms-1/#transform-rendering): transforms affect painting and hit-testing but not layout, and they create a stacking context and a containing block.
- [§transform-origin](https://drafts.csswg.org/css-transforms-1/#transform-origin-property): the default is `50% 50%` of the reference box (the border box).
- [css-transforms-2](https://drafts.csswg.org/css-transforms-2/): adds `translate`, `rotate` and `scale` properties, perspective, `preserve-3d` and `backface-visibility`.

**Chromium.**
- `ComputedStyle::ApplyTransform` (`core/style/computed_style.cc:1401-1456`) [V] composes, in order: translate(origin) · `translate` · `rotate` · `scale` · offset-path · `transform` list · translate(−origin).
- The initial origin is `TransformOrigin(50%, 50%, 0)` (`core/css/css_properties.json5:6747`) [V].
- Transform nodes are built in `UpdateTransform` and the individual-property updates (`paint_property_tree_builder.cc:1531-1753`), and perspective in `UpdatePerspective` (`:3474`) [V].

**gpui-go.**
- Elements have no transform. The Tailwind parser rejects `translate|rotate|scale|skew|origin|transform` with "GPUI elements have no CSS transforms" (`gpui/class.go:1105`) [V].
- Only two things can be transformed: monochrome sprites, which carry a 2×3 matrix (`scene.rs:536-539`) and are used by `SvgTransformation` (`gpui/svgelement.go:110-142`) [V]; and path tessellation (`gpui/path.go:338-356`) [V].
- Quads and shadows are SDFs in screen space, aligned to the axes [V\*, `shaders/shadows.rs`, `shaders/quads.rs`].

**Gap and fix.**
- Option (a): a per-primitive affine transform in every shader, plus transformed content masks. That's XL, and every SDF gets harder.
- Option (b): render the transformed subtree to a group target (as in #2), then composite it as a transformed quad. That's L, and it is how cc composites transformed layers anyway [I].
- Option (b) is recommended. It handles 2D transforms and simple perspective; `preserve-3d` is out of scope.
- Go side, either way: a transform stack in the paint context, transform-origin resolution, and **inverse-mapped hit testing**. Hitboxes are rectangles today [I].
- Translate-only transforms can skip the group (they're the existing element offset).

## 4. Blur sizes (shadows and filters) use the wrong scale

This was the most surprising result: the same CSS number gives different pixels in gpui-go, and in opposite directions.

**Spec.**
- box-shadow blur: [css-backgrounds-3 §shadow-blur](https://drafts.csswg.org/css-backgrounds-3/#shadow-blur). The blur is a Gaussian with σ equal to half the blur radius. Chromium quotes this at `core/style/shadow_data.h:75-80`: "Per spec, sigma is exactly half the blur radius" [V].
- `filter: blur(L)`: [filter-effects-1 §funcdef-filter-blur](https://drafts.fxtf.org/filter-effects-1/#funcdef-filter-blur). L *is* the standard deviation.

**Chromium.**
- Shadows use `BlurAsSigma() = radius * 0.5` (`shadow_data.h:76-88`) [V].
- Filters build `FEGaussianBlur(std_deviation)` from the raw length (`core/paint/filter_effect_builder.cc:284-291`) [V], and the compositor path calls `AppendBlurFilter(pixel_radius)` (`:473-477`) [V].

**gpui-go / GPUI-CE.**
- The shadow shader uses `gaussian(sample_position, shadow.blur_radius)`, so σ equals the blur radius (`shaders/shadows.rs:51-67`; `gaussian` at `shaders/common.rs:490-493`) [V\*]. Go passes `BlurRadius * scale` through unchanged (`gpui/paint.go:556, 581`) [V]. **Result: shadows are 2× as blurry as CSS.**
- Filter and backdrop blur use `RADIUS_TO_STANDARD_DEVIATION = 0.5` (`blur.rs:17`, `BlurKernel::for_radius` at `blur.rs:52-65`) [V\*]. **Result: `filter: blur()` and `backdrop-filter: blur()` are ½ as blurry as CSS.**
- The Tailwind scale is copied 1:1 from Tailwind (`blur-md` = 12px, `gpui/class.go:1062-1063`) [V]. So `backdrop-blur-md` gives σ=6 in gpui-go and σ=12 in Chrome.

**Fix (Go, S).** Divide the shadow blur by 2 and multiply the filter radius by 2 at the Go → record boundary, documented as a deliberate divergence from the pin. Alternatively, fix it natively in `gpui_render`, but that crate is the frozen oracle.

**Related blur-quality gaps.**
- gpui-go always downsamples 2× before blurring, even for σ=0.5 (`renderer/draw.rs:2153-2158`, `blur.rs:11-16`) [V]. Skia blurs at full resolution up to σ=4 and only downsamples above that (`src/core/SkBlurEngine.h:194` `kMaxLinearSigma = 4`) [V]. Small blurs in gpui-go are softer and blockier than Chrome's.
- gpui-go caps the kernel at 32 taps per side with a stride (`blur.rs:19, 58-63`) [V\*]. Very large σ may band [I].

## 5. Box-shadow geometry

**Spec.** [css-backgrounds-3 §box-shadow](https://drafts.csswg.org/css-backgrounds-3/#box-shadow) and [§shadow-shape](https://drafts.csswg.org/css-backgrounds-3/#shadow-shape):
- An outer shadow is drawn only *outside* the border box ("clipped by the border box"). An inset shadow is drawn only inside the padding box.
- Spread grows the corner radii too, with the spread-ratio correction for small radii.

**Chromium.**
- `ClipToBorderEdge` clips out the (rounded) border box before drawing outer shadows (`core/paint/box_painter_base.cc:165-188`, called at `:321`) [V].
- Spread uses `ApplySpreadToShadowShape → OutsetWithCornerCorrection + ConstrainRadii` (`box_painter_base.cc:90-95`; `platform/geometry/float_rounded_rect.cc:178-219, 289`) [V].
- Shadows paint in reverse list order (`box_painter_base.cc:295`) [V].

**gpui-go.**
- For drop shadows, `shadow_coverage` multiplies by the element mask only when `inset` is set (`shaders/shadows.rs:76-95`) [V\*]. A drop shadow therefore fills *under* the element too, so it shows through any translucent background (glass cards, `bg-white/50`). The record already carries `element_bounds` and radii (`scene.rs:274-277`), so a clip-out is a one-line shader change (native S).
- `dropShadowFor` dilates the bounds by the spread but keeps `CornerRadii: elementRadii` (`gpui/paint.go:553-565`) [V]. So the classic `0 0 0 4px` halo on an `r=8` box has 8px corners, where CSS gives 12px (and a circle on a 0-radius box stays square in both). Go S.
- The inset hole correctly shrinks the radii by the spread (`paint.go:572-579`) [V].
- `BoxShadow` is iterated in list order (`gpui/div.go:1885`) [V]. CSS paints the *first* shadow on top. Go S.
- **Already matching:** inset shadows are cast from the padding edge, a recorded deliberate deviation from the pin toward CSS (`gpui/div.go:1916-1949`) [V].

## 6. Gradients (linear, radial, conic, repeating)

**Spec.**
- [css-images-3 §linear-gradients](https://drafts.csswg.org/css-images-3/#linear-gradients): the gradient line length is `|W·sin θ| + |H·cos θ|`, so the corners get exactly the end colours. The section also covers [color-stop fixup](https://drafts.csswg.org/css-images-3/#color-stop-fixup).
- Interpolation is premultiplied ([css-images-4 §coloring-gradient-line](https://drafts.csswg.org/css-images-4/#coloring-gradient-line)), by default in sRGB for legacy colours ([css-color-4 §interpolation](https://drafts.csswg.org/css-color-4/#interpolation)).
- Radial, conic and repeating gradients are in css-images-3/4.

**Chromium.**
- `EndPointsFromAngle` computes the endpoints from the corners (`core/css/css_gradient_value.cc:1284-1350`) [V].
- Legacy colours interpolate in `kSRGBLegacy`, which maps to Skia `kSRGB` (`css_gradient_value.cc:290, 560`; `platform/graphics/gradient.cc:247`) [V], premultiplied (`gradient.cc:287`) [V].
- Linear, radial and conic gradients are separate Skia shaders (`gradient.cc:375-490`) [V].

**gpui-go.**
- `Background{Solid Hsla}` only (`gpui/paint.go:171-177`) [V]. The ABI's colour `tag` reserves gradient and pattern tags (`scene.rs:167-184`) [V]. The class parser rejects `bg-gradient|bg-linear|bg-radial|bg-conic|from|via|to` (`gpui/class.go:1119`) [V].
- **Surprise:** the pinned shader's `Background` already has `LinearGradient` (2 stops) and `PatternSlash` (`shaders/common.rs:198-205, 605-671`), as does upstream `gpui::linear_gradient` (`crates/gpui/src/color.rs:368-386`) [V\*]. gpui-go's ABI drops this.

**Gap and fix.**
- Step 1 (S): carry `tag`, angle and 2 stops through the quad record and the Go `Background`; support `bg-linear-*`/`from`/`to` with 2 stops.
- Even step 1 differs from CSS in three ways:
  - The default colour space is Oklab (`color.rs:384`). CSS is sRGB.
  - The mix is not premultiplied (`common.rs:660-671`), so fading to `transparent` goes grey.
  - The ratio is normalised by the box's dominant axis (`common.rs:622-645`), not by the CSS line length, so diagonal gradients don't reach the corners [I, from reading the formula].
  - Default to sRGB and premultiply in the shader: native S.
- Step 2 (native L): n-stop gradients via a small stop texture or array, plus radial, conic and repeating, with stop fixup in Go.

## 7. filter and backdrop-filter

**Spec.**
- [filter-effects-1](https://drafts.fxtf.org/filter-effects-1/#FilterProperty): `filter` applies to the whole rendered element, including its box-shadow, border and outline. The ten functions include colour matrices and `drop-shadow`.
- [filter-effects-2 §backdrop-filter](https://drafts.fxtf.org/filter-effects-2/#BackdropFilterProperty): the input is the backdrop up to the [Backdrop Root](https://drafts.fxtf.org/filter-effects-2/#backdrop-root), clipped to the border box with its radii. Edge pixels are mirrored (the spec allows this; Chromium does it).

**Chromium.**
- The function switch is at `filter_effect_builder.cc:156-340` [V].
- Backdrop bounds are the pixel-snapped rounded border box (`core/paint/paint_layer.cc:2092-2105`) [V]. The backdrop chain also gets `style.Filter()` appended (`paint_layer.cc:2630-2631`) [V].
- Viz crops the backdrop input with `SkTileMode::kMirror` (`components/viz/service/display/skia_renderer.cc:3254`) [V].

**gpui-go.**
- `Filter{BlurRadius}` is blur only (`gpui/style.go:1035-1041`). The class parser rejects the colour filters and `mix-blend` (`class.go:1121`) [V].
- Drop shadows, the outer ring and the backdrop filter paint *outside* the filter group (`gpui/div.go:1885-1900` vs `:1977-1985`), so `filter: blur()` doesn't blur the element's own shadow [V].
- Backdrop blur samples the whole current target, with no crop or mirror at the element edge (`renderer/draw.rs:2135-2260`, composite clipped to the rounded bounds) [V]. Content just outside the box bleeds in.
- There's no backdrop root: the backdrop is "whatever is in the current target" [I].
- A third nested filter group silently draws *without* its filter ("Inline filter groups … draw nothing", `draw.rs:1587-1588`; `scene.rs:1778-1785`) [V].
- A filter chain nests one group per filter (`div.go:1990-2000`) [V], so `blur(a) blur(b)` on an element already inside a filter hits the depth cap fast.
- **Already matching:** backdrop composites clip to the rounded rect (`draw.rs:1511-1512`), and content blur bleeds by 3σ (`draw.rs:2237-2243`) [V].

**Fix.**
- Native S–M: colour-matrix filters (grayscale, sepia, saturate, hue-rotate, invert, brightness, contrast, opacity) as one 4×5 matrix pass in the composite shader.
- Native S: a mirror-clamped crop for the backdrop.
- Go S: move shadows and the ring inside the filter group.
- With #2's target pool, drop the depth cap.
- `drop-shadow()` reuses the blur and an offset (native M).

## 8. mask-image, mask modes, edge fades

**Spec.** [css-masking-1 §the-mask-image](https://drafts.fxtf.org/css-masking-1/#the-mask-image) and [§the-mask-mode](https://drafts.fxtf.org/css-masking-1/#the-mask-mode). Mask layers are images or gradients, in alpha or luminance mode, composited with `mask-composite`. An invalid image masks everything out.

**Chromium.**
- A mask is an effect node with `SkBlendMode::kDstIn` over the element's group (`paint_property_tree_builder.cc:2378`) [V].
- Masked content is clipped to `CSSMaskPainter::MaskBoundingBox` (`:2230-2245`; `core/paint/css_mask_painter.cc:44`) [V].
- Luminance mode wraps the layer in a luminance-to-alpha layer (`core/paint/svg_mask_painter.cc:72-118`) [V].

**gpui-go.**
- Nothing. `Style.OverflowFade` is stored and refined but "not applied at paint" (`gpui/style.go:865-868`, `:1176-1180`) [V].

**Fix (native M, after #2).** Render the group, then composite with a mask source. That source is a gradient (from #6), giving edge fades, or an atlas image, in alpha or luminance mode. Fades are the 80% case, and a 4-edge linear alpha ramp in the composite shader handles them without general masks (S).

## 9. clip-path

**Spec.** [css-masking-1 §the-clip-path](https://drafts.fxtf.org/css-masking-1/#the-clip-path): basic shapes and `path()` relative to a reference box. The clip also clips **hit-testing**.

**Chromium.**
- `ClipPathClipper::PathBasedClipInternal` (`core/paint/clip_path_clipper.cc:552`) handles the fast path; otherwise it falls back to a mask-based clip (`paint_property_tree_builder.cc:2874`) [V].
- Hit-testing uses `ClipPathClipper::HitTest` (`clip_path_clipper.cc:495-515`; `paint_layer.cc:2107-2118`) [V].

**gpui-go.** None.

**Fix.** Native M once #1 has a coverage-mask texture: tessellate the path with the existing `PathBuilder`/lyon (`gpui/path.go`, `scene.rs:614-689`) into the mask. Go S for the hit-test (point in path).

## 10. mix-blend-mode and isolation

**Spec.** [compositing-1 §mix-blend-mode](https://drafts.fxtf.org/compositing-1/#mix-blend-mode): the element blends with its backdrop inside the nearest isolated group. `isolation: isolate` creates a group.

**Chromium.**
- The effect node gets `state.blend_mode = ToSkBlendMode(style.GetBlendMode())` (`paint_property_tree_builder.cc:2290`) [V].
- cc forces a render surface for it (`RenderSurfaceReason::kBlendMode`, `cc/trees/property_tree_builder.cc:380`; `cc/trees/effect_node.cc:48`) [V].

**gpui-go.** None. Every pass uses premultiplied source-over [I]. The class parser rejects `mix-blend` (`class.go:1121`) [V].

**Fix (native M–L, after #2).** D3D11 has no programmable blending. Copy the backdrop (as the backdrop filter already does) and blend in the composite shader. The separable modes are a small shader. `isolation` falls out of #2 for free.

## 11. Outline and focus rings

**Spec.**
- [css-ui-4 §outline-props](https://drafts.csswg.org/css-ui-4/#outline-props): outlines don't affect layout, follow the border-radius, and are painted *on top* of the element's content (CSS2 Appendix E, final outline phase).
- [§outline-offset](https://drafts.csswg.org/css-ui-4/#outline-offset): offsets may be negative, clamped by the box size.
- `outline-style: auto` is the platform focus ring.
- [selectors-4 §focus-visible](https://drafts.csswg.org/selectors-4/#the-focus-visible-pseudo).

**Chromium.**
- The focus ring is 3px, drawn as two strokes in a 2:1 ratio and scaled with zoom (`core/paint/outline_painter.cc:40-57`) [V]. It is inset by 1px when the border is wide enough (`:59-75`) [V].
- Negative offset is clamped (`:77-83`) [V]. Corners follow the radii (`:163-290`), and `PaintFocusRing` draws the result (`:878-910`) [V].

**gpui-go.**
- `Ring` is a dilated border-only quad whose radii grow by the width (`gpui/style.go:1080-1099`) [V]. That matches a solid `outline` with offset 0, corners included. `InsetRing` is a zero-blur inset shadow (`style.go:1103-1108`) [V].
- It paints *before* the backdrop and background (`div.go:1890-1891`) [V]. CSS outlines paint after all descendants.
- `ring-offset` and `outline` aren't supported (`class.go:1116-1117`) [V].
- **Already matching:** a keyboard-modality `:focus-visible` (`gpui/interact.go:457-476`, `class.go:151, 447`) [V].

**Fix (Go S).** Add an `Outline{Width, Offset, Color, Style}` painted after the continuation, plus a default focus-ring style. Ring-offset is just a bigger dilation.

## 12. Images: object-fit, object-position, image-rendering

**Spec.** [css-images-3 §the-object-fit](https://drafts.csswg.org/css-images-3/#the-object-fit), [§the-object-position](https://drafts.csswg.org/css-images-3/#the-object-position) and [§the-image-rendering](https://drafts.csswg.org/css-images-3/#the-image-rendering):
- Fitting happens in the **content box**.
- object-position defaults to `50% 50%`, so `none` is centred too.
- `pixelated` and `crisp-edges` mean nearest-neighbour scaling.

**Chromium.**
- `LayoutReplaced::ComputeReplacedContentRect` (`core/layout/layout_replaced.cc:428-470`) [V]: `none` takes the intrinsic size, and every mode is then placed by `ObjectPosition()`.
- `GetInterpolationQuality` maps `pixelated`/`crisp-edges` to `kInterpolationNone` (`core/style/computed_style.cc:1326-1337`) [V].

**gpui-go.**
- `ObjectFit.GetBounds` (`gpui/capture.go:609-660`) [V] centres contain, cover and scale-down (matches CSS), but **`none` is placed at the origin** (`:658-659`).
- There is no object-position.
- The fit is computed in the border box (`gpui/img.go:570` passes `bounds`) [V], not the content box.
- **Already matching:** cover is cropped correctly through a sub-tile (`gpui/paintapi.go:237-272`) [V], and images round their own corners [V].
- The sampler is fixed at `D3D11_FILTER_MIN_MAG_MIP_LINEAR` (`renderer/draw.rs:265-266`) [V], so there's no nearest-neighbour mode. Atlas tiles have no mip chain [I], so large downscales will alias where Chrome uses mipmaps.

**Fix.**
- Go S: centre `none`, add `ObjectPosition`, and fit in the content box.
- Native S: a point sampler and a per-sprite flag.
- Native M (optional): mipmaps or pre-scaled frames for images drawn far below their native size.

## 13. Overlapping corner radii

**Spec.** [css-backgrounds-3 §corner-overlap](https://drafts.csswg.org/css-backgrounds-3/#corner-overlap): if adjacent radii add up to more than the side, scale **all** radii by one factor `f = min(side / sum)`.

**Chromium.** `FloatRoundedRect::ConstrainRadii` (`platform/geometry/float_rounded_rect.cc:289-301`) [V].

**gpui-go.** `ClampRadiiForQuadSize` clamps each radius to half the shortest side (`gpui/style.go:1019-1029`) [V]. Pills look the same. Asymmetric radii differ: on a 40px-tall box with `rounded-tl-[30px]` and the other corners at 0, CSS keeps 30 and gpui-go gives 20. Elliptical (x/y) radii aren't representable at all (`Corners` holds one float per corner).

**Fix.** Go S for the factor. Elliptical radii would be native M and are low value.

---

## Where gpui-go already matches or goes further

- Inset shadows are cast from the padding edge, as CSS does (`gpui/div.go:1916-1949`) [V].
- Backdrop blur clips to the rounded border box, like `PaintLayer::BackdropFilterBounds` [V]. Content blur bleeds past the box [V].
- The ring follows the corner radii (radius + width), like Chromium's outline corners [V].
- object-fit contain, cover and scale-down are centred and cropped correctly [V].
- `:focus-visible` uses input modality [V].
- **Ahead of the browser:** corner smoothing (squircles) on quads, shadows, filters and images (`scene.rs:227-228` and elsewhere). CSS has only the experimental `corner-shape`.
- Border dashes with configurable dash and gap (`scene.rs:221-226`), which CSS can't express.

## Summary of where the work lands

- **Native renderer:** the rounded or coverage content mask (#1), a group target pool and opacity groups (#2), transformed group composite (#3), the shadow clip-out (#5), gradient ABI then n-stop and radial/conic (#6), colour-matrix filters and backdrop crop (#7), mask composite (#8), path coverage (#9), blend composite (#10), the point sampler (#12).
- **Go only:** blur scale (#4), spread radii and shadow order (#5), the overflow and border-inset rules (#1), filter coverage of shadows (#7), outline and offset (#11), object-position and `none` centring (#12), the radius factor (#13).
- **Shared building blocks:** one offscreen group target (from #2) and one coverage-mask texture (from #1). Together they unlock #3 and #8–#10.
