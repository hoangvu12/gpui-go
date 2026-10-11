import sys

# ---- capture.rs: the draw-side frame resolution ----
p = 'reference/native/src/capture.rs'
s = open(p, encoding='utf-8').read()
old = '''unsafe fn frame_release_body(frame: u64) -> i32 {'''
new = '''/// Ticket28: resolves a published frame's texture for the renderer's
/// surface draw path (an owned COM clone — the view cache keeps the
/// texture alive independently of the frame handle). Called under the
/// renderer state lock (the allowed renderer->capture nesting).
pub(crate) fn frame_texture_for_draw(
    frame_handle: u64,
) -> Result<(ID3D11Texture2D, u32, u32), i32> {
    let (slot_index, slot_generation, _seq) = frame_handle_parts(frame_handle);
    let state = lock_global();
    if slot_index >= state.slots.len() {
        return Err(capture_status::ERR_BAD_HANDLE);
    }
    let slot = &state.slots[slot_index];
    if !slot.occupied || slot.slot_generation != slot_generation {
        return Err(capture_status::ERR_STALE_HANDLE);
    }
    let Some(stream) = slot.stream.as_ref() else {
        return Err(capture_status::ERR_STALE_HANDLE);
    };
    let Some(frame) = stream.published.get(&frame_handle) else {
        return Err(capture_status::ERR_BAD_HANDLE);
    };
    Ok((frame.texture.clone(), frame.width, frame.height))
}

unsafe fn frame_release_body(frame: u64) -> i32 {'''
assert s.count(old) == 1
s = s.replace(old, new)
open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("capture.rs frame lookup added")

# ---- atlas.rs: AtlasKey::Svg ----
p = 'reference/native/src/atlas.rs'
s = open(p, encoding='utf-8').read()

def rep(old, new, count=1):
    global s
    assert s.count(old) == count, f"atlas needle count {s.count(old)} != {count}: {old[:70]!r}"
    s = s.replace(old, new)

rep('''/// Atlas key kind tags (the pinned `AtlasKey` variants,
/// crates/gpui/src/platform.rs:1815). Svg keys remain a later ticket
/// and are rejected as bad values here.
pub mod atlas_key_kind {
    /// A glyph key: `(RenderGlyphParams, RasterizedGlyphFormat)`.
    pub const GLYPH: u32 = 0;
    /// An image key: `RenderImageParams { image_id, frame_index }`
    /// (ticket17; the pinned `AtlasKey::Image`, texture kind
    /// polychrome). The record reuses the glyph slots: `image_id`
    /// occupies the `font_id` slot, `frame_index` the
    /// `font_size_bits` slot, and every other field must be zero.
    pub const IMAGE: u32 = 1;
}''',
'''/// Atlas key kind tags (the pinned `AtlasKey` variants,
/// crates/gpui/src/platform.rs:1815).
pub mod atlas_key_kind {
    /// A glyph key: `(RenderGlyphParams, RasterizedGlyphFormat)`.
    pub const GLYPH: u32 = 0;
    /// An image key: `RenderImageParams { image_id, frame_index }`
    /// (ticket17; the pinned `AtlasKey::Image`, texture kind
    /// polychrome). The record reuses the glyph slots: `image_id`
    /// occupies the `font_id` slot, `frame_index` the
    /// `font_size_bits` slot, and every other field must be zero.
    pub const IMAGE: u32 = 1;
    /// An SVG mask key (ticket19's follow-up, landed with the wave-4
    /// rebuild): the pinned `AtlasKey::Svg(RenderSvgParams)` — the
    /// identity is the SVG path+size hash in the `font_id` slot (the
    /// Go side's canonical FNV-1a identity, bit 62 set for structural
    /// disjointness) with the raster width and height in the
    /// `glyph_id`/`subpixel_x` slots; the pinned key is the string
    /// path itself, and the hashed identity preserves the observable
    /// hit/miss/removal semantics with a documented hash-collision
    /// bound. Texture kind: monochrome (the pinned
    /// `AtlasKey::Svg(_)` mapping, platform.rs 1840).
    pub const SVG: u32 = 2;
}''')

rep('''enum AtlasKey {
    /// A glyph raster's cache identity.
    Glyph {
        font_id: u64,
        glyph_id: u32,
        font_size_bits: u32,
        subpixel_x: u32,
        subpixel_y: u32,
        scale_bits: u32,
        style_mode: u32,
        style_effect: u32,
        style_color: [u32; 4],
        format: u32,
    },
    /// A decoded image frame's cache identity (ticket17; the pinned
    /// `AtlasKey::Image`).
    Image {
        image_id: u64,
        frame_index: u32,
    },
}''',
'''enum AtlasKey {
    /// A glyph raster's cache identity.
    Glyph {
        font_id: u64,
        glyph_id: u32,
        font_size_bits: u32,
        subpixel_x: u32,
        subpixel_y: u32,
        scale_bits: u32,
        style_mode: u32,
        style_effect: u32,
        style_color: [u32; 4],
        format: u32,
    },
    /// A decoded image frame's cache identity (ticket17; the pinned
    /// `AtlasKey::Image`).
    Image {
        image_id: u64,
        frame_index: u32,
    },
    /// An SVG mask's cache identity (ticket19's follow-up; the pinned
    /// `AtlasKey::Svg(RenderSvgParams)` — see
    /// [`atlas_key_kind::SVG`] for the identity derivation).
    Svg {
        svg_id: u64,
        width: u32,
        height: u32,
    },
}''')

rep('''            // The pinned `AtlasKey::Image` mapping: polychrome.
            AtlasKey::Image { .. } => atlas_texture_kind::POLYCHROME,''',
'''            // The pinned `AtlasKey::Image` mapping: polychrome.
            AtlasKey::Image { .. } => atlas_texture_kind::POLYCHROME,
            // The pinned `AtlasKey::Svg` mapping (platform.rs 1840):
            // monochrome.
            AtlasKey::Svg { .. } => atlas_texture_kind::MONOCHROME,''')

rep('''    if record.kind != atlas_key_kind::GLYPH {
        // Svg keys remain a later ticket; not silently accepted.
        return Err(atlas_status::ERR_BAD_VALUE);
    }''',
'''    if record.kind == atlas_key_kind::SVG {
        // The SVG mask key: svg identity hash in the font_id slot,
        // raster width/height in the glyph_id/subpixel_x slots; every
        // other field must be zero.
        if record.reserved != [0, 0]
            || record.font_size_bits != 0
            || record.subpixel_y != 0
            || record.scale_bits != 0
            || record.style.record_size != 0
            || record.style.dilation != 0
            || record.style.reserved != 0
            || record.style.mode != 0
            || record.style.color_effect_tag != 0
            || record.style.color_r != 0
            || record.style.color_g != 0
            || record.style.color_b != 0
            || record.style.color_a != 0
            || record.format != 0
        {
            return Err(atlas_status::ERR_BAD_VALUE);
        }
        if record.font_id == 0 || record.glyph_id == 0 || record.subpixel_x == 0 {
            return Err(atlas_status::ERR_BAD_VALUE);
        }
        return Ok(AtlasKey::Svg {
            svg_id: record.font_id,
            width: record.glyph_id,
            height: record.subpixel_x,
        });
    }
    if record.kind != atlas_key_kind::GLYPH {
        // Unknown kind tags are refused, never silently accepted.
        return Err(atlas_status::ERR_BAD_VALUE);
    }''')

open(p, 'w', encoding='utf-8', newline='\n').write(s)
print("atlas.rs done")
