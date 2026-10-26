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
    }
    uniform!(group(1), binding(3), COLOR_MATRIX_LOCALS: ColorMatrixUniforms);

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
        let factor = coverage * get!(BLUR_LOCALS).opacity;
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
        TransformedCompositeVarying {
            position: viewport_to_clip_position(device_position),
            local_position,
            clip_distances: clip_distances(local_position, locals.content_mask),
        }
    }

    #[fragment]
    pub fn fragment_transformed_blur_composite(input: TransformedCompositeVarying) -> Vec4f {
        if is_clipped(input.clip_distances) {
            return transparent();
        }
        let coverage = select(
            1.0,
            antialiased_coverage(rounded_rectangle_signed_distance(
                input.local_position,
                get!(BLUR_LOCALS).content_mask,
                get!(BLUR_LOCALS).corner_radii,
            )),
            get!(BLUR_LOCALS).composite_clip == BlurCompositeClip::RoundedBounds,
        );
        // The bucket-relative sampling: the local position over the
        // bucket's size (linear filtering).
        let sampled = texture_sample_level(
            BLUR_TEXTURE,
            BLUR_SAMPLER,
            input.local_position / get!(BLUR_LOCALS).target_size,
            0.0,
        );
        let factor = coverage * get!(BLUR_LOCALS).opacity;
        vec4f(
            sampled.x * factor,
            sampled.y * factor,
            sampled.z * factor,
            sampled.w * factor,
        )
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
