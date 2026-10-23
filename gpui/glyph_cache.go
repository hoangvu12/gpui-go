package gpui

// The glyph paint path's caches (gpui-perf ticket 02,
// .scratch/gpui-perf/issues/02-glyph-raster-cache.md): the pinned
// paint_glyph_from_atlas (window.rs:4841-4939) asks
// TextSystem::raster_bounds first, which answers from the
// raster_metadata cache keyed by RenderGlyphParams
// (text_system.rs raster_bounds), and rasterizes only inside the atlas's
// get_or_insert_with build closure, which runs on an atlas miss
// (directx_atlas.rs get_or_insert_with). A glyph already in the atlas
// therefore costs a map lookup and a sprite insert, never a raster.
//
// The port mirrors that with:
//
//   - rasterCaches.metadata: the raster_metadata cache (bounds, buffer
//     size and format by the render-glyph key), filled by the first
//     rasterization of a key;
//   - Atlas.glyphTiles: the Go mirror of the native atlas's key → tile
//     map for glyph keys, filled by InsertGlyph and GlyphTile and kept in
//     step with RemoveGlyph, NotifyDeviceLost and Dispose (the only ways a
//     glyph tile leaves the native atlas: it never evicts on its own);
//   - memoized PrepareRasterStyle, RecommendedRenderingMode and
//     FontMetrics answers: the native answers are pure functions of the
//     rasterizer's construction state (glyph.rs prepare_style,
//     recommended_mode) and of the font (the pin's TextSystem keeps a
//     font_metrics map too);
//   - pooled pixel buffers for the rasterizations that do happen: the
//     raster is copied into the atlas by the insert, so its buffer is
//     reused, and offering it up front makes the raster one native call
//     instead of the two-call capacity protocol.

import (
	"math"
	"sync"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// glyphCacheKey is the render-glyph key by bit pattern (the native atlas
// keys font size and scale by their f32 bits, so the Go mirrors do too).
type glyphCacheKey struct {
	fontID       FontID
	glyphID      uint32
	fontSizeBits uint32
	subpixelX    uint8
	subpixelY    uint8
	scaleBits    uint32
	style        PreparedRasterStyle
}

func glyphCacheKeyOf(params RasterGlyphParams) glyphCacheKey {
	return glyphCacheKey{
		fontID:       params.FontID,
		glyphID:      params.GlyphID,
		fontSizeBits: math.Float32bits(params.FontSize),
		subpixelX:    params.SubpixelX,
		subpixelY:    params.SubpixelY,
		scaleBits:    math.Float32bits(params.ScaleFactor),
		style:        params.RasterStyle,
	}
}

// glyphTileKey is the atlas key of a glyph: the render-glyph key and the
// raster format (the pinned AtlasKey::Glyph).
type glyphTileKey struct {
	glyph  glyphCacheKey
	format RasterFormat
}

// rasterMetadata is the cached raster facts of one render-glyph key:
// everything paint needs except the pixels.
type rasterMetadata struct {
	boundsX, boundsY int32
	boundsW, boundsH int32
	width, height    int32
	format           RasterFormat
}

func rasterMetadataOf(r *RasterizedGlyph) rasterMetadata {
	return rasterMetadata{
		boundsX: r.BoundsX, boundsY: r.BoundsY,
		boundsW: r.BoundsW, boundsH: r.BoundsH,
		width: r.Width, height: r.Height,
		format: r.Format,
	}
}

// rasterStyleKey is one PrepareRasterStyle request.
type rasterStyleKey struct {
	color Rgba
	mode  GlyphRenderMode
}

// maxRasterStyleMemo bounds the prepared-style memo (one entry per
// distinct text color and mode); it is cleared when full.
const maxRasterStyleMemo = 4096

// rasterCaches are a TextSystem's paint-path caches. The text system is
// process-global and safe for concurrent use, so they are guarded.
type rasterCaches struct {
	mu             sync.Mutex
	metadata       map[glyphCacheKey]rasterMetadata
	styles         map[rasterStyleKey]PreparedRasterStyle
	fontMetrics    map[FontID]FontMetrics
	recommended    TextRenderingMode
	hasRecommended bool
}

func (c *rasterCaches) lookupMetadata(key glyphCacheKey) (rasterMetadata, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	meta, ok := c.metadata[key]
	return meta, ok
}

func (c *rasterCaches) storeMetadata(key glyphCacheKey, meta rasterMetadata) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.metadata == nil {
		c.metadata = make(map[glyphCacheKey]rasterMetadata)
	}
	c.metadata[key] = meta
}

func (c *rasterCaches) lookupStyle(key rasterStyleKey) (PreparedRasterStyle, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	style, ok := c.styles[key]
	return style, ok
}

func (c *rasterCaches) storeStyle(key rasterStyleKey, style PreparedRasterStyle) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.styles == nil || len(c.styles) >= maxRasterStyleMemo {
		c.styles = make(map[rasterStyleKey]PreparedRasterStyle)
	}
	c.styles[key] = style
}

func (c *rasterCaches) lookupFontMetrics(id FontID) (FontMetrics, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, ok := c.fontMetrics[id]
	return m, ok
}

func (c *rasterCaches) storeFontMetrics(id FontID, m FontMetrics) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fontMetrics == nil {
		c.fontMetrics = make(map[FontID]FontMetrics)
	}
	c.fontMetrics[id] = m
}

func (c *rasterCaches) lookupRecommended() (TextRenderingMode, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.recommended, c.hasRecommended
}

func (c *rasterCaches) storeRecommended(mode TextRenderingMode) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.recommended, c.hasRecommended = mode, true
}

// rasterBufferSize is the pooled pixel buffer's capacity: a 64x64 BGRA
// raster, which holds ordinary text glyphs at 2x; larger rasters fall
// back to the exact-buffer retry.
const rasterBufferSize = 64 * 64 * 4

var rasterBufferPool = sync.Pool{New: func() any {
	buf := make([]byte, rasterBufferSize)
	return &buf
}}

// rasterizeForPaint rasterizes one glyph for the paint path into a pooled
// buffer and records its metadata. The returned raster's pixels may
// alias the pooled buffer: the caller hands it to release once the
// raster has been inserted into the atlas (which copies it).
func (ts *TextSystem) rasterizeForPaint(params RasterGlyphParams) (*RasterizedGlyph, *[]byte, error) {
	buf := rasterBufferPool.Get().(*[]byte)
	raster, err := ts.rasterizeGlyphInto(params, *buf)
	if err != nil {
		rasterBufferPool.Put(buf)
		return nil, nil, err
	}
	ts.raster.storeMetadata(glyphCacheKeyOf(params), rasterMetadataOf(raster))
	return raster, buf, nil
}

func releaseRasterBuffer(buf *[]byte) {
	if buf != nil {
		rasterBufferPool.Put(buf)
	}
}

// rasterBoundsForPaint is the pinned TextSystem::raster_bounds for the
// paint path: the cached metadata of the key, or a rasterization that
// fills the cache. A fresh raster is returned with its pooled buffer so
// an atlas miss can insert it without rasterizing again.
func (ts *TextSystem) rasterBoundsForPaint(params RasterGlyphParams) (rasterMetadata, *RasterizedGlyph, *[]byte, error) {
	if !glyphPaintCachesOff {
		if meta, ok := ts.raster.lookupMetadata(glyphCacheKeyOf(params)); ok {
			return meta, nil, nil, nil
		}
	}
	raster, buf, err := ts.rasterizeForPaint(params)
	if err != nil {
		return rasterMetadata{}, nil, nil, err
	}
	return rasterMetadataOf(raster), raster, buf, nil
}

// glyphPaintCachesOff makes the paint path ignore the metadata and tile
// caches: every glyph rasterizes and goes through the native atlas
// insert, the pre-ticket-02 path. The differential test draws its
// reference frames with it.
var glyphPaintCachesOff bool

// cachedGlyphTile answers a glyph key from the atlas's Go-side tile map.
func (a *Atlas) cachedGlyphTile(params RasterGlyphParams, format RasterFormat) (AtlasTile, bool) {
	if glyphPaintCachesOff {
		return AtlasTile{}, false
	}
	a.tilesMu.Lock()
	defer a.tilesMu.Unlock()
	tile, ok := a.glyphTiles[glyphTileKey{glyph: glyphCacheKeyOf(params), format: format}]
	return tile, ok
}

func (a *Atlas) storeGlyphTile(params RasterGlyphParams, format RasterFormat, tile AtlasTile) {
	a.tilesMu.Lock()
	defer a.tilesMu.Unlock()
	if a.glyphTiles == nil {
		a.glyphTiles = make(map[glyphTileKey]AtlasTile)
	}
	a.glyphTiles[glyphTileKey{glyph: glyphCacheKeyOf(params), format: format}] = tile
}

func (a *Atlas) forgetGlyphTile(params RasterGlyphParams, format RasterFormat) {
	a.tilesMu.Lock()
	defer a.tilesMu.Unlock()
	delete(a.glyphTiles, glyphTileKey{glyph: glyphCacheKeyOf(params), format: format})
}

func (a *Atlas) forgetGlyphTiles() {
	a.tilesMu.Lock()
	defer a.tilesMu.Unlock()
	a.glyphTiles = nil
}

// nativeRasterStyle is the native record of a prepared raster style.
func nativeRasterStyle(style PreparedRasterStyle) native.RasterStyleRecord {
	record := native.NewRasterStyleRecord()
	record.Mode = uint32(style.Mode)
	record.EffectTag = uint32(style.Effect)
	record.ColorR = uint32(style.PreblendColor.R)
	record.ColorG = uint32(style.PreblendColor.G)
	record.ColorB = uint32(style.PreblendColor.B)
	record.ColorA = uint32(style.PreblendColor.A)
	return record
}
