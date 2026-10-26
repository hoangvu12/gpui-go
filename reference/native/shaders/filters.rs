#[wgsl_rs::wgsl]
pub mod surface {
    use super::super::common::*;
    use wgsl_rs::std::*;

    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct SurfaceUniforms {
        pub bounds: Bounds,
        pub content_mask: ContentMask,
        pub color_format: SurfaceColorFormat,
        pub opacity: f32,
        pub padding0: u32,
        pub padding1: u32,
        pub padding2: u32,
        pub padding3: u32,
        pub padding4: u32,
        pub padding5: u32,
    }
    uniform!(group(1), binding(0), SURFACE_LOCALS: SurfaceUniforms);
    texture!(group(1), binding(1), SURFACE_TEXTURE: Texture2D<f32>);
    texture!(group(1), binding(2), SURFACE_CHROMA_TEXTURE: Texture2D<f32>);
    sampler!(group(1), binding(3), SURFACE_SAMPLER: Sampler);

    pub const YCBCR_TO_LINEAR_RGB: Mat4x4f = mat4x4f(
        vec4f(1.0000, 1.0000, 1.0000, 0.0),
        vec4f(0.0000, -0.3441, 1.7720, 0.0),
        vec4f(1.4020, -0.7141, 0.0000, 0.0),
        vec4f(-0.7010, 0.5291, -0.8860, 1.0),
    );

    pub fn sample_yuv_surface(texture_position: Vec2f) -> Vec4f {
        let luma = texture_sample_level(SURFACE_TEXTURE, SURFACE_SAMPLER, texture_position, 0.0).x;
        let chroma = texture_sample_level(
            SURFACE_CHROMA_TEXTURE,
            SURFACE_SAMPLER,
            texture_position,
            0.0,
        )
        .xy();
        YCBCR_TO_LINEAR_RGB * vec4f(luma, chroma.x, chroma.y, 1.0)
    }

    #[derive(Wgsl)]
    pub struct SurfaceVarying {
        #[builtin(position)]
        pub position: Vec4f,
        #[location(0)]
        pub texture_position: Vec2f,
        #[location(3)]
        pub clip_distances: Vec4f,
    }

    #[vertex]
    pub fn vertex_surface(#[builtin(vertex_index)] vertex_id: u32) -> SurfaceVarying {
        let vertex = rectangle_vertex(vertex_id, get!(SURFACE_LOCALS).bounds);
        SurfaceVarying {
            position: vertex.clip_position,
            texture_position: vertex.unit_position,
            clip_distances: clip_distances(
                vertex.viewport_position,
                get!(SURFACE_LOCALS).content_mask.bounds,
            ),
        }
    }

    #[fragment]
    pub fn fragment_surface(input: SurfaceVarying) -> Vec4f {
        if is_clipped(input.clip_distances) {
            return transparent();
        }
        let locals = get!(SURFACE_LOCALS);
        let fade = ContentMask::alpha(locals.content_mask, input.position.xy());
        if locals.color_format == SurfaceColorFormat::Yuv {
            return sample_yuv_surface(input.texture_position) * locals.opacity * fade;
        }
        texture_sample_level(
            SURFACE_TEXTURE,
            SURFACE_SAMPLER,
            input.texture_position,
            0.0,
        ) * locals.opacity
            * fade
    }
}

#[wgsl_rs::wgsl]
pub mod blur {
    use super::super::common::*;
    use super::super::corner_smoothing::*;
    use wgsl_rs::std::*;

    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct BlurUniforms {
        pub bounds: Bounds,
        pub content_mask: Bounds,
        pub corner_radii: Corners,
        pub direction: Vec2f,
        pub standard_deviation: f32,
        pub opacity: f32,
        pub sample_count: u32,
        pub sample_step: f32,
        pub composite_clip: BlurCompositeClip,
        pub downsample_mode: DownsampleMode,
        pub source_size: Vec2f,
        pub target_size: Vec2f,
        pub corner_smoothing: f32,
        pub padding0: u32,
        pub padding1: u32,
        pub padding2: u32,
    }
    uniform!(group(1), binding(0), BLUR_LOCALS: BlurUniforms);
    texture!(group(1), binding(1), BLUR_TEXTURE: Texture2D<f32>);
    sampler!(group(1), binding(2), BLUR_SAMPLER: Sampler);

    // rev-15 addition (browser-core ticket 39), reshaped in rev 15.1
    // (browser-core ticket 77): the filter chain's colour matrices in
    // its own constant buffer so the pinned `BlurUniforms` layout is
    // untouched. Each entry is one colour function's row-major 4x5
    // matrix: the first four columns of row_i are `row_i`, the fifth
    // column is `offset`: out_i = row_i · (r, g, b, a) + offset_i.
    //
    // rev 15.1 carries one entry PER colour function (applied in chain
    // order, each followed by the [0,1] clamp) instead of rev 15's one
    // composed matrix: Chrome does NOT compose the chain into one map —
    // each function's output is rasterized 8-bit before the next
    // applies, which clamps every channel to [0,1] (measured against
    // the filters-chain record: hue-rotate(90deg) sepia(1) on pure red
    // reads (70, 62, 48) = sepia·clamp(hue·red) — the composed matrix
    // would give (42, 38, 29), and the fixture's whole-image mean
    // channel diff against Chrome was 15.4667). The entries' number is
    // `count`; unused entries are ignored (the ABI record zero-pads
    // them). `sample_offset` is the composite's region-relative
    // sampling offset (0,0) — the pinned formula — for content filters.
    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct ColorMatrixEntry {
        pub row0: Vec4f,
        pub row1: Vec4f,
        pub row2: Vec4f,
        pub row3: Vec4f,
        pub offset: Vec4f,
    }

    // rev 16 (browser-core ticket 51) appends the transformed-group
    // composite's 2x3 matrix after the rev-15.1 prefix, as two
    // row-major `vec4`s (`transform_row0` = (m00, m01, tx, 0),
    // `transform_row1` = (m10, m11, ty, 0); the vec4s keep the uniform
    // layout 16-aligned - a nested matrix struct would violate the
    // uniform address-space layout constraints). `target_origin` and
    // `padding2` are layout words every composite leaves at zero: the
    // pooled group buckets all render at their frame's origin (the
    // fragment-position invariant), so no origin translation is
    // needed. The unit matrix for every pass but the transformed
    // composite. The rev-15.1 prefix stays byte-identical.
    //
    // rev 16 (browser-core ticket 52) appends the mask, blend and
    // drop-shadow fields AFTER the matrix rows (the prefix stays
    // byte-identical): `mask_kind`/`mask_reference`/`mask_mode` drive
    // the composite's dst_in mask sample (0 none, 1 gradient table, 2
    // atlas texture, 3 coverage, 4 invalid - the composite draws
    // nothing, the design §3.5's "an invalid mask hides the
    // element"), `blend_mode` is the W3C compositing tag (0 src-over
    // .. 16 plus-lighter; only the BLEND composite reads it),
    // `tint_color` is the drop-shadow pass's straight sRGBA tint,
    // `backdrop_size` the blend composite's backdrop-copy size, and
    // `shadow_mode`/`padding3` close the struct at 752 bytes.
    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct ColorMatrixUniforms {
        pub entries: [ColorMatrixEntry; 8],
        pub sample_offset: Vec2f,
        pub count: u32,
        pub padding0: u32,
        pub target_origin: Vec2f,
        pub padding2: Vec2f,
        pub transform_row0: Vec4f,
        pub transform_row1: Vec4f,
        pub mask_kind: u32,
        pub mask_reference: u32,
        pub mask_mode: u32,
        pub blend_mode: u32,
        pub tint_color: Vec4f,
        pub backdrop_size: Vec2f,
        pub shadow_mode: u32,
        pub padding3: u32,
    }
    uniform!(group(1), binding(3), COLOR_MATRIX_LOCALS: ColorMatrixUniforms);

    // rev 16 (browser-core ticket 52): the composite's mask source
    // (t10) and the blend composite's backdrop copy (t11). The MASK
    // texture carries whichever source the b5 mask kind names: the
    // atlas texture for image masks, the coverage target for clip-path
    // masks (the GRADIENT mask samples the shared ramp directly - the
    // common module's GRADIENTS/GRADIENT_RAMP bindings). Both stay
    // unbound on every pass that does not sample them (the same
    // shape as the rev-15 extension cbuffers).
    texture!(group(1), binding(8), MASK_TEXTURE: Texture2D<f32>);
    texture!(group(1), binding(9), BACKDROP_TEXTURE: Texture2D<f32>);

    // rev-15 addition (browser-core ticket 39): the backdrop mirror
    // crop. CSS crops the backdrop-filter input to the element's border
    // box (filter-effects-2: the backdrop image is the backdrop
    // "clipped to the border box"; Chromium additionally mirrors the
    // edge pixels, viz skia_renderer.cc:3254 @ 0d4dcd4f using
    // SkTileMode::kMirror). The pinned path blurs the whole current
    // target, so content just outside the element bleeds in. The crop
    // entry points copy the element's region of the backdrop into a
    // dedicated texture (the blur passes then read it with mirror
    // addressing, so the kernel's reads beyond the edge fold back onto
    // the element's own content), and the composite samples it with the
    // region-relative coordinate `sample_offset` carries.
    #[repr(C)]
    #[derive(Clone, Copy, Wgsl)]
    pub struct BackdropCropUniforms {
        /// The region's origin in the backdrop texture (device pixels).
        pub origin: Vec2f,
        /// The backdrop texture's size (device pixels).
        pub source_size: Vec2f,
        /// The cropped region's size (device pixels, the element's
        /// pixel-covered bounds).
        pub region_size: Vec2f,
        /// The mirrored margin around the region (device pixels); the
        /// crop texture is `region + 2*margin` so the blur kernel's
        /// reads past the region fold back onto the element's content.
        pub margin: Vec2f,
    }
    uniform!(group(1), binding(4), BACKDROP_CROP_LOCALS: BackdropCropUniforms);

    #[vertex]
    pub fn vertex_backdrop_crop(
        #[builtin(vertex_index)] vertex_id: u32,
    ) -> BlurVarying {
        let vertex = fullscreen_vertex(vertex_id);
        BlurVarying {
            position: vertex.clip_position,
            texture_coordinates: vertex.texture_coordinates,
            clip_distances: unclipped_distances(),
        }
    }

    /// The SkTileMode::kMirror fold: coordinates beyond `[0, extent)`
    /// reflect back into it (Chromium's backdrop edge handling,
    /// components/viz/service/display/skia_renderer.cc:3254).
    pub fn mirror_fold(value: f32, extent: f32) -> f32 {
        let folded = abs(signed_modulo(value, 2.0 * extent));
        select(folded, 2.0 * extent - folded, folded > extent)
    }

    #[fragment]
    pub fn fragment_backdrop_crop(input: BlurVarying) -> Vec4f {
        // The crop texture spans `[−margin, region + margin)` around the
        // region; a destination pixel's local coordinate is folded back
        // into `[0, region)` with mirroring, then offset by the region's
        // origin in the backdrop texture.
        let locals = get!(BACKDROP_CROP_LOCALS);
        let local = input.position.xy() - locals.margin;
        let folded = vec2f(
            mirror_fold(local.x, locals.region_size.x),
            mirror_fold(local.y, locals.region_size.y),
        );
        let coordinates = (folded + locals.origin) / locals.source_size;
        texture_sample_level(BLUR_TEXTURE, BLUR_SAMPLER, coordinates, 0.0)
    }

    #[derive(Clone, Copy, Wgsl)]
    pub struct BlurCompositeVertexData {
        pub position: Vec4f,
        pub texture_coordinates: Vec2f,
        pub clip_distances: Vec4f,
    }

    pub fn prepare_blur_composite_vertex(vertex_id: u32) -> BlurCompositeVertexData {
        let vertex = rectangle_vertex(vertex_id, get!(BLUR_LOCALS).bounds);
        BlurCompositeVertexData {
            position: vertex.clip_position,
            texture_coordinates: vertex.unit_position,
            clip_distances: clip_distances(
                vertex.viewport_position,
                get!(BLUR_LOCALS).content_mask,
            ),
        }
    }

    pub fn blur_composite_color(position: Vec2f, coverage: f32) -> Vec4f {
        // rev-15: the backdrop path samples the crop texture, so the
        // coordinate is region-relative (the content path's offset is
        // (0, 0) and reproduces the pinned formula exactly).
        let uv = (position - get!(COLOR_MATRIX_LOCALS).sample_offset)
            / get!(BLUR_LOCALS).target_size;
        let mut blurred = texture_sample_level(BLUR_TEXTURE, BLUR_SAMPLER, uv, 0.0);
        // rev-15 addition (browser-core ticket 39), reshaped in rev 15.1
        // (browser-core ticket 77): apply the chain's colour-matrix
        // filters, one entry per function in chain order, each followed
        // by the CSS [0,1] clamp (Chrome rasterizes every function's
        // output 8-bit before the next applies — the filters-chain
        // fixture measured a composed matrix diverging by a 15.47 mean
        // channel diff). The blurred texture holds premultiplied
        // content (every scene fragment emits premultiplied colors), so
        // the color is un-premultiplied, mapped through the entries,
        // and re-premultiplied.
        let matrix = get!(COLOR_MATRIX_LOCALS);
        if matrix.count > 0u32 {
            let alpha = blurred.w;
            let mut mapped = select(
                transparent(),
                blurred / vec4f(alpha, alpha, alpha, 1.0),
                alpha > 0.0,
            );
            let mut entry = 0u32;
            while entry < matrix.count {
                let m = matrix.entries[entry as usize];
                mapped = vec4f(
                    dot(m.row0, mapped) + m.offset.x,
                    dot(m.row1, mapped) + m.offset.y,
                    dot(m.row2, mapped) + m.offset.z,
                    dot(m.row3, mapped) + m.offset.w,
                );
                // The per-function clamp (a negative channel after
                // hue-rotate, an over-1 channel after sepia or
                // saturate — the values the next function must NOT
                // see).
                mapped = saturate(mapped);
                entry += 1u32;
            }
            blurred = mapped * vec4f(
                mapped.w,
                mapped.w,
                mapped.w,
                1.0,
            );
        }
        // rev 16 (browser-core ticket 52): the mask composite - the
        // sampled group colour multiplied by the mask's alpha (dst_in
        // on the premultiplied source; a kind-0 mask leaves the factor
        // at 1, so every non-masked composite is bit-identical).
        let factor = coverage * get!(BLUR_LOCALS).opacity * composite_mask_alpha(position);
        vec4f(
            blurred.x * factor,
            blurred.y * factor,
            blurred.z * factor,
            blurred.w * factor,
        )
    }

    #[derive(Wgsl)]
    pub struct BlurVarying {
        #[builtin(position)]
        pub position: Vec4f,
        #[location(0)]
        pub texture_coordinates: Vec2f,
        #[location(3)]
        pub clip_distances: Vec4f,
    }

    #[vertex]
    pub fn vertex_blur_fullscreen(#[builtin(vertex_index)] vertex_id: u32) -> BlurVarying {
        let vertex = fullscreen_vertex(vertex_id);
        BlurVarying {
            position: vertex.clip_position,
            texture_coordinates: vertex.texture_coordinates,
            clip_distances: unclipped_distances(),
        }
    }

    #[fragment]
    pub fn fragment_blur_downsample(input: BlurVarying) -> Vec4f {
        if get!(BLUR_LOCALS).downsample_mode == DownsampleMode::HalfResolution {
            let destination = floor(input.position.xy());
            let source_coordinates = min(
                (destination + 0.5) / get!(BLUR_LOCALS).target_size,
                (get!(BLUR_LOCALS).source_size - 0.5) / get!(BLUR_LOCALS).source_size,
            );
            return texture_sample_level(BLUR_TEXTURE, BLUR_SAMPLER, source_coordinates, 0.0);
        }
        texture_sample_level(BLUR_TEXTURE, BLUR_SAMPLER, input.texture_coordinates, 0.0)
    }

    pub fn sample_blur_texture(texture_coordinates: Vec2f) -> Vec4f {
        texture_sample_level(BLUR_TEXTURE, BLUR_SAMPLER, texture_coordinates, 0.0)
    }

    pub fn gaussian_blur(texture_coordinates: Vec2f) -> Vec4f {
        let uniforms = get!(BLUR_LOCALS);
        let center_weight = gaussian(0.0, uniforms.standard_deviation);
        let mut weighted_color = sample_blur_texture(texture_coordinates) * center_weight;
        let mut weight_sum = center_weight;
        let mut sample_index = 1u32;

        while sample_index <= uniforms.sample_count {
            let distance = sample_index as f32 * uniforms.sample_step;
            let weight = gaussian(distance, uniforms.standard_deviation);
            let coordinate_offset = uniforms.direction * distance;
            let symmetric_pair = sample_blur_texture(texture_coordinates - coordinate_offset)
                + sample_blur_texture(texture_coordinates + coordinate_offset);
            weighted_color = weighted_color + symmetric_pair * weight;
            weight_sum += 2.0 * weight;
            sample_index += 1u32;
        }
        weighted_color / max(weight_sum, MIN_NORMALIZED_WEIGHT)
    }

    #[fragment]
    pub fn fragment_blur(input: BlurVarying) -> Vec4f {
        gaussian_blur(input.texture_coordinates)
    }

    #[vertex]
    pub fn vertex_blur_composite(#[builtin(vertex_index)] vertex_id: u32) -> BlurVarying {
        let vertex = prepare_blur_composite_vertex(vertex_id);
        BlurVarying {
            position: vertex.position,
            texture_coordinates: vertex.texture_coordinates,
            clip_distances: vertex.clip_distances,
        }
    }

    // ------------------------------------------------------------------
    // rev 16 (browser-core ticket 52): the composite's MASK sample and
    // the W3C blend functions.
    // ------------------------------------------------------------------

    // The composite's MASK sample's alpha at the given sampling
    // position (the LOCAL position for a transform composite - CSS
    // masks inside the element's own space). The b5 `mask_kind`/
    // `mask_reference`/`mask_mode` fields drive it: a gradient mask
    // evaluates the referenced table entry's parameter at the position
    // and samples the shared ramp row (the tile mode wraps it); an
    // atlas mask samples the bound atlas texture stretched over the
    // composite's bounds (the mask rect, the group bounds); a coverage
    // mask samples the bound A8 coverage target at the position. Mode 0
    // reads the sample's alpha (dst_in), mode 1 its REC-601 luminance
    // (Chromium's luminance-to-alpha mask filter). Kind 4 (an
    // unresolvable source) yields 0 - the design §3.5's "an invalid
    // mask hides the element".
    pub fn composite_mask_alpha(position: Vec2f) -> f32 {
        let locals = get!(COLOR_MATRIX_LOCALS);
        if locals.mask_kind == 0u32 {
            return 1.0;
        }
        if locals.mask_kind == 4u32 {
            return 0.0;
        }
        let sampled: Vec4f;
        if locals.mask_kind == 1u32 {
            // The gradient mask: the referenced table entry's parameter
            // wrapped under its tile mode, sampled from the shared ramp
            // row (the ramp's dimensions queried at runtime).
            let gradient = get!(GRADIENTS)[locals.mask_reference as usize];
            let t = gradient_table_parameter(
                gradient.kind,
                gradient.geometry0,
                gradient.geometry1,
                position,
            );
            let wrapped = select(
                select(t, fract(t), gradient.tile == 1u32),
                1.0 - abs(signed_modulo(t, 2.0) - 1.0),
                gradient.tile == 2u32,
            );
            let rows = f32(texture_dimensions(GRADIENT_RAMP).y);
            sampled = texture_sample_level(
                GRADIENT_RAMP,
                GRADIENT_SAMPLER,
                vec2f(
                    gradient_ramp_coordinate(wrapped),
                    (f32(gradient.stop_row) + 0.5) / rows,
                ),
                0.0,
            );
        } else if locals.mask_kind == 2u32 {
            // The atlas mask: the texture stretched over the composite's
            // bounds (the mask rect).
            let bounds = get!(BLUR_LOCALS).bounds;
            let unit = (position - bounds.origin) / bounds.size;
            sampled = texture_sample_level(MASK_TEXTURE, BLUR_SAMPLER, unit, 0.0);
        } else {
            // The coverage mask: the A8 coverage target at the position
            // (its own dimensions queried at runtime).
            let dims = texture_dimensions(MASK_TEXTURE);
            let size = vec2f(f32(dims.x), f32(dims.y));
            sampled = texture_sample_level(
                MASK_TEXTURE,
                BLUR_SAMPLER,
                position / size,
                0.0,
            );
        }
        if locals.mask_mode == 1u32 {
            return color_brightness(sampled.xyz());
        }
        sampled.w
    }

    // The W3C compositing-1 §10.1 separable blend primitives (the
    // per-channel B(Cb, Cs) functions; no recursion - WGSL forbids it,
    // so overlay and hard-light inline their multiply/screen arms).
    pub fn blend_multiply(cb: f32, cs: f32) -> f32 {
        cb * cs
    }

    pub fn blend_screen(cb: f32, cs: f32) -> f32 {
        cb + cs - cb * cs
    }

    pub fn blend_hard_light(cb: f32, cs: f32) -> f32 {
        if cs <= 0.5 {
            return blend_multiply(cb, 2.0 * cs);
        }
        blend_screen(cb, 2.0 * cs - 1.0)
    }

    pub fn blend_soft_light(cb: f32, cs: f32) -> f32 {
        if cs <= 0.5 {
            return cb - (1.0 - 2.0 * cs) * cb * (1.0 - cb);
        }
        let mut d = sqrt(cb);
        if cb <= 0.25 {
            d = ((16.0 * cb - 12.0) * cb + 4.0) * cb;
        }
        cb + (2.0 * cs - 1.0) * (d - cb)
    }

    // One separable blend mode's per-channel B(Cb, Cs) (compositing-1
    // §10.1; the mode tags follow the design's A.1 list).
    pub fn blend_separable_channel(mode: u32, cb: f32, cs: f32) -> f32 {
        if mode == 1u32 {
            return blend_multiply(cb, cs);
        }
        if mode == 2u32 {
            return blend_screen(cb, cs);
        }
        if mode == 3u32 {
            return blend_hard_light(cs, cb);
        }
        if mode == 4u32 {
            return min(cb, cs);
        }
        if mode == 5u32 {
            return max(cb, cs);
        }
        if mode == 6u32 {
            if cb == 0.0 {
                return 0.0;
            }
            if cs == 1.0 {
                return 1.0;
            }
            return min(1.0, cb / (1.0 - cs));
        }
        if mode == 7u32 {
            if cb == 1.0 {
                return 1.0;
            }
            if cs == 0.0 {
                return 0.0;
            }
            return 1.0 - min(1.0, (1.0 - cb) / cs);
        }
        if mode == 8u32 {
            return blend_hard_light(cb, cs);
        }
        if mode == 9u32 {
            return blend_soft_light(cb, cs);
        }
        if mode == 10u32 {
            return abs(cb - cs);
        }
        if mode == 11u32 {
            return cb + cs - 2.0 * cb * cs;
        }
        cs
    }

    // The W3C compositing-1 §10.2 non-separable helpers on straight
    // colors (Lum, ClipColor, SetLum, Sat, SetSat).
    pub fn blend_lum(c: Vec3f) -> f32 {
        dot(c, REC_601_LUMA_WEIGHTS)
    }

    pub fn blend_clip_color(c: Vec3f) -> Vec3f {
        let l = blend_lum(c);
        let n = min(min(c.x, c.y), c.z);
        let x = max(max(c.x, c.y), c.z);
        let mut out = c;
        if n < 0.0 && l > n {
            out = vec3f(l, l, l) + (c - vec3f(l, l, l)) * l / (l - n);
        }
        if x > 1.0 && x > l {
            out = vec3f(l, l, l) + (c - vec3f(l, l, l)) * (1.0 - l) / (x - l);
        }
        out
    }

    pub fn blend_set_lum(c: Vec3f, l: f32) -> Vec3f {
        let d = l - blend_lum(c);
        blend_clip_color(c + vec3f(d, d, d))
    }

    pub fn blend_sat(c: Vec3f) -> f32 {
        max(max(c.x, c.y), c.z) - min(min(c.x, c.y), c.z)
    }

    // SetSat: (C - Cmin) * s / (Cmax - Cmin) - the min channel lands at
    // 0, the max at s and the mid on the interpolated scale (the
    // equal-channel case flattens to 0).
    pub fn blend_set_sat(c: Vec3f, s: f32) -> Vec3f {
        let cmin = min(min(c.x, c.y), c.z);
        let cmax = max(max(c.x, c.y), c.z);
        if cmax > cmin {
            return (c - vec3f(cmin, cmin, cmin)) * (s / (cmax - cmin));
        }
        vec3f(0.0, 0.0, 0.0)
    }

    // One non-separable mode's B(Cb, Cs) (compositing-1 §10.2: hue,
    // saturation, color, luminosity).
    pub fn blend_nonseparable(mode: u32, cb: Vec3f, cs: Vec3f) -> Vec3f {
        if mode == 12u32 {
            return blend_set_lum(blend_set_sat(cs, blend_sat(cb)), blend_lum(cb));
        }
        if mode == 13u32 {
            return blend_set_lum(blend_set_sat(cb, blend_sat(cs)), blend_lum(cb));
        }
        if mode == 14u32 {
            return blend_set_lum(cs, blend_lum(cb));
        }
        blend_set_lum(cb, blend_lum(cs))
    }

    // The full blend + source-over composite on PREMULTIPLIED inputs
    // (compositing-1 §10's closed form): with straight colors
    // Cb = P_b/αb and Cs = P_s/αs,
    //   αo·Co = (1−αb)·P_s + αs·αb·B(Cb, Cs) + (1−αs)·P_b
    //   αo    = αs + (1−αs)·αb
    // Plus-lighter (mode 16) is the premultiplied clamp(P_s + P_b)
    // (Skia kPlus, "clamp(s+d)").
    pub fn blend_premultiplied(mode: u32, backdrop: Vec4f, source: Vec4f) -> Vec4f {
        if mode == 16u32 {
            return clamp(source + backdrop, vec4f(0.0, 0.0, 0.0, 0.0), vec4f(1.0, 1.0, 1.0, 1.0));
        }
        let alpha_backdrop = backdrop.w;
        let alpha_source = source.w;
        let cb = select(
            vec3f(0.0, 0.0, 0.0),
            backdrop.xyz() / vec3f(alpha_backdrop, alpha_backdrop, alpha_backdrop),
            alpha_backdrop > 0.0,
        );
        let cs = select(
            vec3f(0.0, 0.0, 0.0),
            source.xyz() / vec3f(alpha_source, alpha_source, alpha_source),
            alpha_source > 0.0,
        );
        let mut mixed = vec3f(
            blend_separable_channel(mode, cb.x, cs.x),
            blend_separable_channel(mode, cb.y, cs.y),
            blend_separable_channel(mode, cb.z, cs.z),
        );
        if mode >= 12u32 && mode <= 15u32 {
            mixed = blend_nonseparable(mode, cb, cs);
        }
        let out_alpha = alpha_source + (1.0 - alpha_source) * alpha_backdrop;
        let out_color = (1.0 - alpha_backdrop) * source.xyz()
            + vec3f(alpha_source, alpha_source, alpha_source) * alpha_backdrop * mixed
            + (1.0 - alpha_source) * backdrop.xyz();
        vec4f(out_color.x, out_color.y, out_color.z, out_alpha)
    }

    #[fragment]
    pub fn fragment_blur_composite(input: BlurVarying) -> Vec4f {
        if is_clipped(input.clip_distances) {
            return transparent();
        }
        let coverage = select(
            1.0,
            antialiased_coverage(rounded_rectangle_signed_distance(
                input.position.xy(),
                get!(BLUR_LOCALS).bounds,
                get!(BLUR_LOCALS).corner_radii,
            )),
            get!(BLUR_LOCALS).composite_clip == BlurCompositeClip::RoundedBounds,
        );
        blur_composite_color(input.position.xy(), coverage)
    }

    #[derive(Wgsl)]
    pub struct SmoothedBlurVarying {
        #[builtin(position)]
        pub position: Vec4f,
        #[location(0)]
        pub texture_coordinates: Vec2f,
        #[location(1)]
        #[interpolate(flat)]
        pub horizontal_corner_reaches: Vec4f,
        #[location(2)]
        #[interpolate(flat)]
        pub vertical_corner_reaches: Vec4f,
        #[location(3)]
        pub clip_distances: Vec4f,
        #[location(4)]
        #[interpolate(flat)]
        pub smoothing_factors: Vec4f,
        #[location(5)]
        #[interpolate(flat)]
        pub superellipse_power: f32,
    }

    // The transformed-group composite (rev 16, browser-core ticket 51):
    // one textured quad under the effect group's 2x3 matrix. The
    // subtree rendered into its bucket in LOCAL coordinates (the
    // children's own records; the bucket renders the local frame's [0,
    // size]); the vertex maps the bucket rect's corners through the
    // matrix and carries the LOCAL position as a varying - the group's
    // rectangular content mask and its rounded clip (the rrect over
    // the content mask with the record's corner radii) live in the
    // local frame, because CSS clips inside the element's own space
    // and the transform applies after. The fragment samples the bucket
    // at the interpolated LOCAL position over the bucket's size
    // (linear filtering; the affine interpolation of the quad's
    // corners).
    #[derive(Clone, Copy, Wgsl)]
    pub struct TransformedCompositeVarying {
        #[builtin(position)]
        pub position: Vec4f,
        #[location(0)]
        pub local_position: Vec2f,
        #[location(1)]
        pub clip_distances: Vec4f,
        // rev 16, ticket 52 (closing 51's handed-off gap): the smoothed
        // corner machinery - the prepared reaches and factors ride flat
        // varyings exactly like the smoothed axis-aligned composite's.
        #[location(4)]
        #[interpolate(flat)]
        pub horizontal_corner_reaches: Vec4f,
        #[location(5)]
        #[interpolate(flat)]
        pub vertical_corner_reaches: Vec4f,
        #[location(6)]
        #[interpolate(flat)]
        pub smoothing_factors: Vec4f,
        #[location(7)]
        #[interpolate(flat)]
        pub superellipse_power: f32,
    }

    #[vertex]
    pub fn vertex_transformed_blur_composite(
        #[builtin(vertex_index)] vertex_id: u32,
    ) -> TransformedCompositeVarying {
        let locals = get!(BLUR_LOCALS);
        let extensions = get!(COLOR_MATRIX_LOCALS);
        let unit_position = rectangle_corner(vertex_id);
        // The local position: within the group's content region (the
        // quad), in the bucket's coordinates (the bucket holds the
        // frame's [0, size]).
        let local_position = Bounds::position(locals.bounds, unit_position);
        // The row-major 2x3: device = row0.xy * p + row0.z,
        // row1.xy * p + row1.z (the ABI record's transform layout,
        // transform-origin resolved by the caller).
        let device_position = vec2f(
            extensions.transform_row0.x * local_position.x
                + extensions.transform_row0.y * local_position.y
                + extensions.transform_row0.z,
            extensions.transform_row1.x * local_position.x
                + extensions.transform_row1.y * local_position.y
                + extensions.transform_row1.z,
        );
        // rev 16, ticket 52 (closing 51's handed-off gap): the smoothed
        // corner preparation over the CONTENT MASK's size (the rect the
        // clip coverage evaluates; a zero smoothing returns the plain
        // radii and zero factors).
        let prepared = prepare_corners(
            locals.content_mask.size,
            locals.corner_radii,
            locals.corner_smoothing,
            true,
        );
        TransformedCompositeVarying {
            position: viewport_to_clip_position(device_position),
            local_position,
            clip_distances: clip_distances(local_position, locals.content_mask),
            horizontal_corner_reaches: prepared.horizontal_reaches,
            vertical_corner_reaches: prepared.vertical_reaches,
            smoothing_factors: prepared.smoothing_factors,
            superellipse_power: prepared.superellipse_power,
        }
    }

    #[fragment]
    pub fn fragment_transformed_blur_composite(input: TransformedCompositeVarying) -> Vec4f {
        if is_clipped(input.clip_distances) {
            return transparent();
        }
        let locals = get!(BLUR_LOCALS);
        // rev 16, ticket 52: the rounded clip's coverage - the smoothed
        // corner SDF when the record carries smoothing (the prepared
        // varyings), the plain rrect otherwise; evaluated at the LOCAL
        // position (CSS clips inside the element, then the transform
        // applies). A zero smoothing keeps the plain path bit-exact.
        let signed_distance = select(
            rounded_rectangle_signed_distance(
                input.local_position,
                locals.content_mask,
                locals.corner_radii,
            ),
            prepared_corner_signed_distance(
                input.local_position,
                locals.content_mask,
                locals.corner_radii,
                locals.corner_smoothing,
                PreparedCorners {
                    horizontal_reaches: input.horizontal_corner_reaches,
                    vertical_reaches: input.vertical_corner_reaches,
                    smoothing_factors: input.smoothing_factors,
                    superellipse_power: input.superellipse_power,
                },
            ),
            locals.corner_smoothing > 0.0,
        );
        let coverage = select(
            1.0,
            antialiased_coverage(signed_distance),
            locals.composite_clip == BlurCompositeClip::RoundedBounds,
        );
        // The bucket-relative sampling: the local position over the
        // bucket's size (linear filtering).
        let sampled = texture_sample_level(
            BLUR_TEXTURE,
            BLUR_SAMPLER,
            input.local_position / get!(BLUR_LOCALS).target_size,
            0.0,
        );
        // rev 16 (browser-core ticket 52): the mask composite multiplies
        // the sampled group colour at the LOCAL position (CSS masks
        // inside the element's own space, then the transform applies).
        let factor = coverage * get!(BLUR_LOCALS).opacity * composite_mask_alpha(input.local_position);
        vec4f(
            sampled.x * factor,
            sampled.y * factor,
            sampled.z * factor,
            sampled.w * factor,
        )
    }

    // The blend composite (rev 16, browser-core ticket 52): the group
    // composites with a W3C blend mode instead of source-over. D3D11
    // has no programmable fixed-function blending, so the pass samples
    // a BACKDROP COPY (the b11-bound texture of the parent target's
    // content, copied before this draw) and applies the blend formula
    // in the shader, writing with NO fixed blending - outside the
    // group's coverage the formula degenerates to exactly the
    // backdrop, so the untouched pixels round-trip bit-exactly. The
    // vertex is the transformed composite's shape (the matrix maps the
    // quad's corners; the identity for an axis-aligned group - the
    // axis-aligned and transformed blend groups share one pipeline).
    #[derive(Clone, Copy, Wgsl)]
    pub struct BlendCompositeVarying {
        #[builtin(position)]
        pub position: Vec4f,
        #[location(0)]
        pub local_position: Vec2f,
        #[location(1)]
        pub clip_distances: Vec4f,
    }

    #[vertex]
    pub fn vertex_blend_composite(
        #[builtin(vertex_index)] vertex_id: u32,
    ) -> BlendCompositeVarying {
        let locals = get!(BLUR_LOCALS);
        let extensions = get!(COLOR_MATRIX_LOCALS);
        let unit_position = rectangle_corner(vertex_id);
        let local_position = Bounds::position(locals.bounds, unit_position);
        let device_position = vec2f(
            extensions.transform_row0.x * local_position.x
                + extensions.transform_row0.y * local_position.y
                + extensions.transform_row0.z,
            extensions.transform_row1.x * local_position.x
                + extensions.transform_row1.y * local_position.y
                + extensions.transform_row1.z,
        );
        BlendCompositeVarying {
            position: viewport_to_clip_position(device_position),
            local_position,
            clip_distances: clip_distances(local_position, locals.content_mask),
        }
    }

    #[fragment]
    pub fn fragment_blend_composite(input: BlendCompositeVarying) -> Vec4f {
        let locals = get!(BLUR_LOCALS);
        let extensions = get!(COLOR_MATRIX_LOCALS);
        // The backdrop copy at the device position (the parent frame).
        let backdrop = texture_sample_level(
            BACKDROP_TEXTURE,
            BLUR_SAMPLER,
            input.position.xy() / extensions.backdrop_size,
            0.0,
        );
        // Where the group does not cover (the content-mask rect's
        // outside, the rrect's outside, the mask's zero, the opacity's
        // zero) the formula degenerates to the backdrop - the untouched
        // pixels round-trip EXACTLY (this pass writes with no fixed
        // blending, so it must reproduce the parent everywhere).
        if is_clipped(input.clip_distances) {
            return backdrop;
        }
        let coverage = select(
            1.0,
            antialiased_coverage(rounded_rectangle_signed_distance(
                input.local_position,
                locals.content_mask,
                locals.corner_radii,
            )),
            locals.composite_clip == BlurCompositeClip::RoundedBounds,
        );
        // The source: the group's bucket at the local position, its
        // opacity and mask applied (premultiplied).
        let source = texture_sample_level(
            BLUR_TEXTURE,
            BLUR_SAMPLER,
            input.local_position / locals.target_size,
            0.0,
        );
        let factor = coverage * locals.opacity * composite_mask_alpha(input.local_position);
        let masked = source * vec4f(factor, factor, factor, factor);
        blend_premultiplied(extensions.blend_mode, backdrop, masked)
    }

    // The drop-shadow composite (rev 16, browser-core ticket 52): the
    // blurred silhouette of the group (the sampled source's ALPHA -
    // the gaussian averages each channel, so the blurred alpha IS the
    // blurred silhouette), tinted with the op's color and drawn
    // PREMULTIPLIED beneath the content quad. The quad is the group's
    // dilated bounds translated by the op's offset; `sample_offset`
    // carries the offset (the translation composite's rule), so the
    // silhouette lands at its offset position.
    #[fragment]
    pub fn fragment_drop_shadow_composite(input: BlurVarying) -> Vec4f {
        if is_clipped(input.clip_distances) {
            return transparent();
        }
        let sampled = texture_sample_level(
            BLUR_TEXTURE,
            BLUR_SAMPLER,
            (input.position.xy() - get!(COLOR_MATRIX_LOCALS).sample_offset)
                / get!(BLUR_LOCALS).target_size,
            0.0,
        );
        let extensions = get!(COLOR_MATRIX_LOCALS);
        let silhouette = sampled.w * get!(BLUR_LOCALS).opacity;
        let tint = extensions.tint_color;
        let alpha = tint.w * silhouette;
        vec4f(tint.x * alpha, tint.y * alpha, tint.z * alpha, alpha)
    }

    #[vertex]
    pub fn vertex_smoothed_blur_composite(
        #[builtin(vertex_index)] vertex_id: u32,
    ) -> SmoothedBlurVarying {
        let vertex = prepare_blur_composite_vertex(vertex_id);
        let prepared = prepare_corners(
            get!(BLUR_LOCALS).bounds.size,
            get!(BLUR_LOCALS).corner_radii,
            get!(BLUR_LOCALS).corner_smoothing,
            true,
        );
        SmoothedBlurVarying {
            position: vertex.position,
            texture_coordinates: vertex.texture_coordinates,
            horizontal_corner_reaches: prepared.horizontal_reaches,
            vertical_corner_reaches: prepared.vertical_reaches,
            clip_distances: vertex.clip_distances,
            smoothing_factors: prepared.smoothing_factors,
            superellipse_power: prepared.superellipse_power,
        }
    }

    #[fragment]
    pub fn fragment_smoothed_blur_composite(input: SmoothedBlurVarying) -> Vec4f {
        if is_clipped(input.clip_distances) {
            return transparent();
        }
        let coverage = select(
            1.0,
            antialiased_coverage(prepared_corner_signed_distance(
                input.position.xy(),
                get!(BLUR_LOCALS).bounds,
                get!(BLUR_LOCALS).corner_radii,
                get!(BLUR_LOCALS).corner_smoothing,
                PreparedCorners {
                    horizontal_reaches: input.horizontal_corner_reaches,
                    vertical_reaches: input.vertical_corner_reaches,
                    smoothing_factors: input.smoothing_factors,
                    superellipse_power: input.superellipse_power,
                },
            )),
            get!(BLUR_LOCALS).composite_clip == BlurCompositeClip::RoundedBounds,
        );
        blur_composite_color(input.position.xy(), coverage)
    }
}
