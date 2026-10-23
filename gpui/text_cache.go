package gpui

// This file is the port of the reference line-layout cache
// (crates/gpui/src/text_system/line_layout.rs::LineLayoutCache, lines
// 925-1127 at 254b5dbd…): the two-frame shaped-line cache that keeps a
// window's shaped lines alive across frames while frames keep
// requesting them, and drops the ones no frame requested. The consumer
// gap (ticket 02): without it, every frame's text work re-shaped its
// text into a fresh native shaping handle (the port's per-frame
// shapedCell and inline measurements are frame-scoped by design, like
// the reference's own per-frame TextLayout caches), and a redrawing
// window with text exhausted the native live-handle bound (256,
// MaxShapingHandles / ErrTextShapingLimit) within seconds — the
// mordbard readiness spike measured 17 forced redraws of a 7-Text
// window failing every further frame.
//
// Reference semantics, ported (deviations recorded inline):
//
//   - One cache per window text system: the reference WindowTextSystem
//     owns a LineLayoutCache (text_system.rs:293, constructed per
//     window at WindowTextSystem::new), and its shape_text resolves
//     through line_layout_cache.layout_wrapped_line. The port's
//     realWindowTextSystem (div.go) holds this cache the same way; both
//     of its shaped paths (the text element's measure seam and the
//     inline paragraph's per-segment shaping) route through it.
//   - The reference keeps TWO frame maps — `lines` (the
//     shape_line/layout_line seam) and `wrapped_lines` (shape_text) —
//     because its platform exposes two seam methods; the port's seam
//     has one shaped path (TextLayoutRequest, optionally wrapped and
//     clamped), so one map carries the wrapped_lines role.
//   - Frame rotation (layout_wrapped_line / finish_frame): a lookup
//     consults the current frame, then the previous frame (a hit moves
//     the entry into the current frame), and only a full miss shapes
//     through the platform. finish_frame — called by the window draw's
//     tail (window.rs:3230 self.text_system().finish_frame()) — swaps
//     the two frame caches and clears the new current one, which holds
//     the old previous frame's leftovers: exactly the entries the
//     finishing frame did not request. The reference drops them (the
//     Arc's last reference dies then, or later if a caller still holds
//     the layout); the port DISPOSES their native shaping handles at
//     that point, because a Go *WrappedLine has no refcount to release
//     them implicitly. A cached line therefore survives at most one
//     idle frame, exactly like the reference's cache entries.
//   - The reference's used_lines/used_wrapped_lines bookkeeping exists
//     for reuse_layouts / truncate_layouts — the deferred-draw
//     paint-range reuse of reuse_prepaint/reuse_paint (window.rs
//     3719-3820). The port's deferred draws re-request their elements'
//     layouts each frame, so those entry points are not ported, and
//     eviction does not need the used list: an entry leaves the
//     previous frame by being reused (moved into the current frame) or
//     dies in the finish_frame swap.
//   - sync_font_generation: the reference clears both frames when the
//     platform's font generation moves (fonts were registered). The
//     native ABI exposes the generation per layout (the shaping
//     summary's FontGeneration, the registration-batch counter), not
//     as a standalone query, so the port discovers a move lazily: a
//     fresh shape under a different generation clears the cache before
//     inserting itself. The invalidation trigger is identical (a font
//     catalog generation bump); only the discovery point differs,
//     recorded here. The used bookkeeping on the reference's clear()
//     is likewise covered by the swap.
//   - The live-handle bound (Base ticket 40): the reference's cache has
//     no resource bound — its entries are Arc'd Rust layouts. The
//     port's entries each hold a native shaping handle, and the native
//     service bounds live handles at 256 PROCESS-WIDE (frozen at rev
//     11). The text element measures each text under its unwrapped and
//     wrapped keys, so ~128 texts in a frame, or a page switch whose
//     current plus previous frame together hold 256 distinct lines,
//     reached the bound; the failed frame did not rotate, so every later
//     frame failed too ("measure callback failed (latched)"). Every
//     line this cache inserts is therefore enrolled with the text
//     system's parking order (TextSystem.markEvictable): a shape that
//     hits the bound releases the least recently used cache-owned
//     handle and retries, and a parked line re-shapes from its stored
//     request on its next query. Nothing about the cache's semantics
//     changes — keys, hits, the rotation and disposal are the
//     reference's; only the handle under an entry may be re-created.
//   - Locking: the reference guards previous_frame with a Mutex and
//     current_frame with an RwLock. The port's draws run on the
//     foreground host thread, but the public Window.ShapeText facts
//     path can shape outside a frame, so one Mutex guards the whole
//     cache (the native service serializes its own state the same
//     way).

import (
	"encoding/binary"
	"math"
	"sync"
)

// shapedLineCache is the reference LineLayoutCache (see the file
// header): the two-frame shaped-line cache of one window text system.
// The zero value is ready to use.
type shapedLineCache struct {
	// mu guards both frame maps and the generation bookkeeping (the
	// reference's Mutex + RwLock pair, collapsed: the port has no
	// concurrent layout readers to favor).
	mu sync.Mutex
	// previous holds the entries the last completed frame requested
	// (the reference previous_frame: Mutex<FrameCache>).
	previous map[shapedLineKey]*WrappedLine
	// current holds this frame's entries so far (the reference
	// current_frame: RwLock<FrameCache>).
	current map[shapedLineKey]*WrappedLine
	// fontGeneration is the font catalog generation the cached lines
	// were shaped under (sync_font_generation's remembered value);
	// hasFontGeneration is false before the first shape.
	fontGeneration    uint32
	hasFontGeneration bool
}

// shapedLineKey is the reference CacheKey (text, font_size, runs,
// wrap_width, max_lines — line_layout.rs:1128-1135) in a Go-comparable
// shape: the run list's descriptor fields are length-prefix encoded
// into one string, so deep-equal run lists produce equal keys (the
// reference's derived PartialEq/Hash over CacheKeyRef compares the same
// fields). Floats key by IEEE-754 bits: equal f32 values are equal
// keys, and the shaper's finiteness validation runs before any entry is
// inserted.
type shapedLineKey struct {
	text     string
	fontSize uint32 // f32 bits
	runs     string // the canonical run-list encoding
	wrap     uint32 // f32 bits when hasWrap
	hasWrap  bool
	clamp    uint32
	hasClamp bool
}

// shapedLineKeyOf builds the cache key of one shaping request (the
// CacheKey construction of layout_wrapped_line).
func shapedLineKeyOf(request TextLayoutRequest) shapedLineKey {
	key := shapedLineKey{
		text:     request.Text,
		fontSize: math.Float32bits(request.FontSize),
		runs:     encodeShapeRuns(request.Runs),
	}
	if request.WrapWidth != nil {
		key.wrap = math.Float32bits(*request.WrapWidth)
		key.hasWrap = true
	}
	if request.LineClamp != nil {
		key.clamp = *request.LineClamp
		key.hasClamp = true
	}
	return key
}

// encodeShapeRuns encodes one style-run list into a canonical,
// collision-free string (the reference's SmallVec<TextRun> compared
// field by field through CacheKeyRef): every string is length-prefixed
// and every number fixed-width little-endian, so distinct run lists
// cannot collide and equal run lists encode identically whatever their
// slice provenance.
func encodeShapeRuns(runs []TextRun) string {
	size := 4
	for _, run := range runs {
		size += 4 + 4 + len(run.Font.Family) + 4 + 4
		for _, feature := range run.Font.Features {
			size += 4 + len(feature.Tag) + 4
		}
		for _, family := range run.Font.Fallbacks {
			size += 4 + len(family)
		}
		size += 1 + 4
	}
	buf := make([]byte, 0, size)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(runs)))
	for _, run := range runs {
		buf = binary.LittleEndian.AppendUint32(buf, uint32(run.Len))
		buf = appendStringKey(buf, run.Font.Family)
		buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(run.Font.Weight))
		buf = binary.LittleEndian.AppendUint32(buf, uint32(run.Font.Style))
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(run.Font.Features)))
		for _, feature := range run.Font.Features {
			buf = appendStringKey(buf, feature.Tag)
			buf = binary.LittleEndian.AppendUint32(buf, feature.Value)
		}
		buf = binary.LittleEndian.AppendUint32(buf, uint32(len(run.Font.Fallbacks)))
		for _, family := range run.Font.Fallbacks {
			buf = appendStringKey(buf, family)
		}
		if run.LetterSpacing != nil {
			buf = append(buf, 1)
			buf = binary.LittleEndian.AppendUint32(buf, math.Float32bits(*run.LetterSpacing))
		} else {
			buf = append(buf, 0)
		}
	}
	return string(buf)
}

// appendStringKey appends one length-prefixed string to the key buffer.
func appendStringKey(buf []byte, s string) []byte {
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(s)))
	return append(buf, s...)
}

// layoutWrappedLine is the reference
// LineLayoutCache::layout_wrapped_line: answer the shaped line for the
// request, reusing the current frame's entry, then the previous
// frame's, and only shaping through the text system on a full miss. The
// caller must not dispose the returned line: the cache owns its native
// handle for its remaining lifetime (the reference hands out an Arc
// clone; the port hands out the shared handle, whose only disposer is
// the cache).
func (c *shapedLineCache) layoutWrappedLine(system *TextSystem, request TextLayoutRequest) (*WrappedLine, error) {
	key := shapedLineKeyOf(request)
	c.mu.Lock()
	defer c.mu.Unlock()
	if line, ok := c.current[key]; ok {
		return line, nil
	}
	if line, ok := c.previous[key]; ok {
		// The previous-frame hit moves the entry into the current frame
		// (the reference removes it from previous_frame and inserts it
		// into current_frame, marking it used).
		delete(c.previous, key)
		if c.current == nil {
			c.current = make(map[shapedLineKey]*WrappedLine)
		}
		c.current[key] = line
		return line, nil
	}
	line, err := system.ShapeText(request)
	if err != nil {
		return nil, err
	}
	// sync_font_generation: a layout shaped under a different font
	// catalog generation invalidates every cached layout. The reference
	// reads the platform's generation before the lookup; the port reads
	// it off the fresh layout (the only place the native ABI exposes
	// it), so a stale generation is discovered one shape late — the
	// same clear, the same correctness, recorded above.
	if generation := line.summary.FontGeneration; !c.hasFontGeneration || generation != c.fontGeneration {
		c.clearLocked()
		c.fontGeneration = generation
		c.hasFontGeneration = true
	}
	if c.current == nil {
		c.current = make(map[shapedLineKey]*WrappedLine)
	}
	c.current[key] = line
	// The cache owns the line: under the process-wide live-handle bound
	// its native handle may be parked and re-shaped on demand (Base
	// ticket 40, text.go shapeHandle).
	system.markEvictable(line)
	return line, nil
}

// finishFrame is the reference LineLayoutCache::finish_frame, called by
// the window draw's completion (window.rs:3230): swap the two frame
// caches, then clear the new current one — the old previous frame's
// leftovers, the entries the finishing frame did not request. The
// reference drops them; the port also disposes their native shaping
// handles (no Arc releases them implicitly). An abandoned frame build
// does not finish: like the reference, its entries stay put and the
// next completed frame's rotation handles them, so nothing leaks either
// way.
func (c *shapedLineCache) finishFrame() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.previous, c.current = c.current, c.previous
	for _, line := range c.current {
		_ = line.Dispose()
	}
	c.current = nil
}

// clearLocked releases every cached line (LineLayoutCache::clear): a
// font-generation move, or the cache's retirement with its window. The
// caller holds the mutex.
func (c *shapedLineCache) clearLocked() {
	for _, line := range c.current {
		_ = line.Dispose()
	}
	for _, line := range c.previous {
		_ = line.Dispose()
	}
	c.current = nil
	c.previous = nil
}

// dispose retires the whole cache with its window's element runtime
// (the reference window's WindowTextSystem — and with it the cache and
// its Arc-held lines — drops with the window; the port's dropDrawState
// releases the native handles explicitly next to the atlas).
func (c *shapedLineCache) dispose() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.clearLocked()
	c.fontGeneration = 0
	c.hasFontGeneration = false
}

// entryCount reports the number of cached entries across both frames
// (test-support observation of the cache's size).
func (c *shapedLineCache) entryCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.current) + len(c.previous)
}
