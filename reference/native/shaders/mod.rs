//! Vendored, patched copies of the pinned shader modules
//! (`crates/gpui_render/src/shaders` at the `254b5dbd…` pin).
//!
//! The pinned CE source is the frozen oracle and is never modified; the
//! rev-15 rebuild (browser-core ticket 39) needs three shader-level
//! behaviours the pinned shader blobs do not express, so this tree is a
//! copy of the pinned modules with the recorded divergences below,
//! compiled by this crate's `build.rs` through the same
//! wgsl-rs → Naga 24 → HLSL sm_5.0 → D3DCompile (Fxc) pipeline the
//! pinned crate uses. Every unpatched function is a byte-for-byte copy
//! of the pin; the unpatched pipelines (paths, sprites, surfaces,
//! emoji, the blur/downsample passes) keep consuming the pinned
//! `gpui_ce_render::artifacts::NATIVE_SHADERS` blobs.
//!
//! # Recorded divergences from the pin
//!
//! 1. **Drop shadows clip out of the border box** (`shadows.rs`,
//!    `shadow_coverage`/`smoothed_shadow_coverage`): the pinned shader
//!    multiplies the element-shape coverage in only for *inset* shadows,
//!    so a drop shadow also fills under the element and shows through
//!    translucent backgrounds. CSS (css-backgrounds-3 §box-shadow: an
//!    outer shadow is "clipped by the border box") and Chromium
//!    (`ClipToBorderEdge`, third_party/blink/renderer/core/paint/
//!    box_painter_base.cc:165-188, Chromium 0d4dcd4f) clip outer
//!    shadows out of the (rounded) border box; the patched shader
//!    multiplies the non-inset coverage by
//!    `1 - antialiased_coverage(element rounded-rect SDF)`.
//! 2. **CSS linear-gradient geometry and premultiplied sRGB mixing**
//!    (`common.rs`, `linear_gradient_ratio`/`linear_gradient_color`):
//!    the pinned ratio normalizes by the box's dominant axis, so
//!    diagonal gradients do not reach the corners, and the pinned mix is
//!    a straight (non-premultiplied) blend, so a gradient fading to
//!    `transparent` goes grey. CSS (css-images-3 §linear-gradients: the
//!    gradient line length is `|W·sin θ| + |H·cos θ|`; css-images-4
//!    §coloring-gradient-line: premultiplied interpolation) and Chromium
//!    (`EndPointsFromAngle`, third_party/blink/renderer/core/css/
//!    css_gradient_value.cc:1284-1350; premultiplied
//!    `platform/graphics/gradient.cc:287`, Chromium 0d4dcd4f) do both;
//!    the patched shader computes the CSS gradient-line ratio and mixes
//!    premultiplied sRGB stops (the ABI record's default color space,
//!    delivered through the pinned `Background::color_space(Srgb)`
//!    builder; the Oklab path is unchanged from the pin).
//! 3. **Colour-matrix filters** (`filters.rs`): the pinned `ScaledFilter`
//!    is blur-only, so no colour filter (grayscale, sepia, brightness,
//!    contrast, invert, saturate, hue-rotate, opacity) can be expressed.
//!    CSS (filter-effects-1 §FilterProperty) applies each colour
//!    function as a 4×5 matrix over non-premultiplied RGBA; the patched
//!    composite pass carries the chain's matrices — one 4×5 entry per
//!    colour function — in `BlurUniforms`'s extension cbuffer and
//!    applies them in chain order, each followed by the [0,1] clamp.
//!    **rev 15.1 (browser-core ticket 77): the per-function entries and
//!    clamps replaced rev 15's single composed matrix.** Chrome does not
//!    compose a chain into one linear map: each function's output is
//!    rasterized 8-bit before the next applies, clamping every channel
//!    to [0,1] (measured against the filters-chain record: hue-rotate
//!    (90deg) sepia(1) on pure red reads (70,62,48) = sepia·clamp
//!    (hue·red) in Chrome, while the composed matrix gives (42,38,29);
//!    the fixture's mean channel diff was 15.4667). A chain longer than
//!    the record's capacity composes its excess into the last entry —
//!    the ABI record documents that cap — and a blur interleaved
//!    between colour functions still composes with the other blurs
//!    (gaussians commute; the clamp between a blur and a matrix does
//!    not, which stays the recorded approximation). The application is
//!    after any blur, on the unpremultiplied sampled color, then
//!    re-premultiplied.

//! # rev 16 (browser-core ticket 50) divergences
//!
//! 4. **The extended content mask and the ABI-record instance layout**
//!    (`common.rs`, `quads.rs`, `shadows.rs`): the mask grows from
//!    bounds-only (16 bytes) to the pin's full shader shape - bounds,
//!    per-edge `fade_out` ramp, corner radii, smoothing and one layout
//!    pad word (56 bytes; the pad exists because the mask's bounds
//!    member forces 8-byte alignment in the HLSL structured-buffer
//!    layout, and a struct's size rounds up to its alignment; the fade
//!    word order is LEFT, TOP, RIGHT, BOTTOM) - and the
//!    `ContentMask::alpha` clip multiplies in the rounded-rectangle
//!    SDF when the mask carries radii (the INNERMOST rounded clip,
//!    inline; ancestor rounded clips become effect groups). The quad
//!    and shadow instance structs mirror the ABI records field for
//!    field (`GpuiGoSceneQuadRecord` 280 bytes - the PINNED field
//!    order, which is the alignment-safe one, plus a trailing layout
//!    pad; `GpuiGoSceneShadowRecord` 216, same rule), so the draw path
//!    uploads the scene's records directly as the instance arrays -
//!    zero per-frame conversion. A zero-radii, zero-fade mask
//!    evaluates exactly as the pre-R16 bounds-only mask (group-free
//!    scenes render pixel-identically). The pinned
//!    underline/sprite/path pipelines keep consuming the pinned DXBC
//!    blobs (bounds + fade only, no radii) - the recorded gap handed
//!    to ticket 52's mask work.

#![allow(dead_code, unused_assignments, unused_imports)]

pub mod common;
pub mod interface;

mod corner_smoothing;
mod filters;
mod quads;
mod shadows;

pub use filters::{blur, surface};
pub use quads::quad;
pub use shadows::shadow;
