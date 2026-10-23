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
//!    composite pass carries one composed matrix in `BlurUniforms` (the
//!    ABI's filter record composes consecutive matrix filters in chain
//!    order — matrix multiplication is associative over the chain) and
//!    applies it to the unpremultiplied sampled color before
//!    re-premultiplying, after any blur.

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
