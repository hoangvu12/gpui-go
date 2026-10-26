//! The vendored Parley adapter (browser-core tickets 43 and 39's
//! vendoring pattern, the same decision `native/shaders/` recorded for
//! the shader tree in rev 15).
//!
//! `gpui_ce_parley`'s `ParleyTextSystem` (the pinned text backend) is
//! fully encapsulated: its `parley: Mutex<ParleyState>` (the one
//! `FontContext` + `LayoutContext`), its `FontStore` (the canonical
//! `FontId` interner) and its `FontCatalog` are all `pub(crate)` in the
//! pinned crate, so a second crate cannot push the browser-core text
//! fields (word-break, overflow-wrap, text alignment) into Parley's
//! ranged style builder through the public surface — TEXT_ABI.md
//! recorded exactly this seam as the blocker for tickets 42/43.
//!
//! This module vendors the pinned adapter's implementation —
//! `text_system.rs` (the `ParleyTextSystem`, the paragraph layout, the
//! `PlatformTextLayout` implementations), `store.rs` (the `FontStore`
//! interning), `catalog.rs` (the `FontCatalog` resolve) and
//! `paragraphs.rs` (the paragraph splitting) — as byte-copies of the
//! pinned checkout at `254b5dbd…` with the import paths rewritten for
//! this module tree. The service's `TextStack` (src/text.rs) constructs
//! THIS system instead of the pinned one, so the process keeps exactly
//! ONE `FontContext`/`FontStore` — the vendored one — and the FontIds
//! the text service resolves and reports are the same FontIds the glyph
//! service rasterizes (both services share the stack in
//! `crate::text::with_stack`); there is no second stack anywhere, which
//! is the FontId-provenance guarantee the ticket's approach asked for.
//! `text::tests::vendored_font_ids_match_the_pinned_adapter` additionally
//! proves the vendored store interns like the pinned store (a fresh
//! pinned system and a fresh vendored system resolve the same descriptor
//! sequence to the same FontIds).
//!
//! The pinned crate stays a dependency: its PUBLIC types
//! (`SystemFonts`, `GlyphRasterizer`, `RasterFace`, `ColorGlyphKind`,
//! `FontVariation`, `FontSynthesis`, `SwashGlyphRasterizer`) are
//! imported by the vendored copies — `crate::glyph`'s
//! `WindowsGlyphRasterizer` still implements the PINNED
//! `GlyphRasterizer` trait, so the DirectWrite rasterizer plugs into the
//! vendored system unchanged.
//!
//! # Recorded divergences from the pin (rev 16, ticket 43)
//!
//! Everything not listed here is byte-identical to the pinned checkout
//! (the copies carry the pinned names and line structure; the pinned
//! crate's own test modules are dropped — they test the pinned crate,
//! and this crate's tests cover the vendored behavior):
//!
//! 1. **The import rewrite** — `crate::{CatalogState, FaceFamily,
//!    FaceRequest, FontCatalog, FontStore}` → the vendored module's own
//!    types; the pinned crate's public types stay `gpui_ce_parley::…`.
//! 2. **The orphan-rule rewrites in `store.rs`** — the pinned crate's
//!    `impl From<Synthesis> for FontSynthesis` cannot be vendored
//!    (`FontSynthesis` is the pinned crate's type, `Synthesis`
//!    fontique's), so `LoadedFont::raster_face` spells the same
//!    field mapping out. `store.rs`'s public types and its
//!    `SwashGlyphRasterizer` are not vendored (see the file headers).
//! 3. **`catalog.rs`'s test-only constructors** (`FontCatalog::new`,
//!    `CatalogState::register`, `register_fonts`) are dropped; only the
//!    shared-collection construction the service uses remains.
//! 4. **The rev-16 side-channel (ticket 43)** — `ParleyLayoutParams`
//!    carries a `browser: BrowserLayoutParams` with the run-level
//!    word-break / overflow-wrap tags (pushed as Parley
//!    `StyleProperty::WordBreak`/`OverflowWrap` range styles, on top of
//!    the existing `CHROMIUM_LINE_BREAK_OVERRIDE` the pinned adapter
//!    always pins) and the document-level text-align tag (start / end /
//!    justify driven through Parley's `Alignment::Start`/`End`/
//!    `Justify`; `Justify` stretches the whitespace clusters exactly as
//!    Parley 0.11's alignment pass does). The pinned gpui types
//!    (`TextLayoutRequest`, `TextRun`) stay untouched — the fields reach
//!    the vendored layout through
//!    `ParleyTextSystem::layout_text_with_browser`, and the trait's
//!    `layout_text` keeps its pinned, field-free behavior.
//! 5. **`layout_text_with_browser` also reports `is_rtl`** (the
//!    document's base direction, `paragraphs[0]`'s — the pinned
//!    multi-paragraph rule) so the Go paint layer can resolve
//!    text-align start/end against the direction.
//! 6. **A document carrying a `WordBreak::BreakAll` run drops the
//!    `CHROMIUM_LINE_BREAK_OVERRIDE`** — parley's override hook is
//!    un-scoped and applied after the segmenter, and its ASCII table
//!    sets every printable-ASCII pair to no-break, so break-all could
//!    never break ASCII text while the override is pinned. Chrome's own
//!    break-all path consults its fast pair table first and then
//!    overrides it with the break-all line-break class table, which
//!    ALLOWS AL×AL breaks (`ShouldBreakAfterBreakAll`,
//!    third_party/blink/renderer/platform/text/text_break_iterator.cc:
//!    125-149 and :328-336 at 0d4dcd4f) — the segmenter's own BreakAll
//!    rules stand instead, which is that class-table outcome. Recorded
//!    cost: a mixed normal+break-all document loses the override's
//!    punctuation rules for its normal runs.

mod catalog;
mod paragraphs;
mod store;
mod text_system;

pub use text_system::ParleyTextSystem;

pub(crate) use text_system::{BrowserLayoutParams, BrowserRunFields, BrowserTextAlign};
