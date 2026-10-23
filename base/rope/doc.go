// Package rope is a pure-Go recreation of the subset of ropey 2.0.0-beta.1
// (crate features metric_chars, metric_lines_lf, metric_utf16) that GPUI Base
// uses as the text buffer of every input.
//
// Upstream pins: Base declares `ropey = "=2.0.0-beta.1"` with those three
// features (upstream-base/ce/Cargo.toml:89-93) and re-exports `Rope` from
// input/mod.rs:120. The call sites that define the needed surface are
// input/base/rope_ext.rs:277-448 (RopeExt), input/base/selection.rs:86-87,
// input/base/state.rs:1027,1702-1750 and editor/display_map/text_wrapper.rs.
// Behaviour follows the ropey sources (src/rope.rs, src/slice.rs,
// src/shared_impl.rs, src/iter.rs, src/lib.rs); comments cite them as
// ropey/<file>:<line>.
//
// Semantics kept from ropey 2:
//
//   - All positions are byte offsets. Char, UTF-16 and LF-line metrics are
//     tracked per node, so conversions are O(log N).
//   - Lines are LF-only (LineType::LF): CR is ordinary text, a CRLF pair ends
//     its line at the LF, and the text always has len_lines = LF count + 1.
//   - Rope values are persistent: nodes are never mutated after creation, so
//     copying a Rope (Rust `clone`) is O(1) and edits path-copy. A Rope and
//     its slices stay valid after the Rope they came from is edited.
//   - Methods that ropey documents as panicking panic with the same causes;
//     the Try*/Get* forms return ErrNonCharBoundary, ErrOutOfBounds or
//     ErrInvalidRange instead.
//
// Deviations (Go language or scope):
//
//   - Rust's &str is always valid UTF-8; a Go string may not be. FromStr,
//     Insert and SliceFromStr replace invalid UTF-8 with U+FFFD
//     (strings.ToValidUTF8) before storing it.
//   - Only LineType LF exists, matching the features Base enables.
//   - Ranges are passed as (start, end) byte offsets instead of RangeBounds.
//   - Not ported (unused by Base): RopeBuilder, ChunkCursor, from_reader,
//     write_to, Ord/Hash, extra::*, insert_char, trailing_line_break_idx.
//     ropey 2.0.0-beta.1 has no Rope::append; the slice-05 inventory listing
//     it is mistaken.
package rope
