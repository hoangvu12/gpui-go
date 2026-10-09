package gpui

// This file is the port's glyph DRAW path of ticket10: the pinned
// window paint path (crates/gpui/src/window.rs at
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a) from
// paint_glyph/paint_emoji down to the sprite primitive insertion:
//
//	paint_glyph(origin, font_id, glyph_id, font_size, color)
//	  → scale the origin by the scale factor
//	  → quantize_glyph_origin → (integer origin, subpixel variant)
//	  → requested_mode = subpixel if the platform recommends it, else
//	    grayscale
//	  → prepare_raster_style(hsla→rgba color, mode)
//	  → paint_glyph_from_atlas:
//	      rasterize_glyph(params)
//	      if bounds are zero: return (empty glyphs never insert)
//	      atlas insert (a cached key returns its tile with no upload)
//	      sprite bounds = integer origin + raster bounds origin
//	      insert MonochromeSprite (AlphaMask) / SubpixelSprite
//	      (BgraSubpixelMask) / PolychromeSprite (BgraColor)
//
// Citations (window.rs at the pin): quantize_glyph_origin lines 82-93,
// quantize_color_glyph_origin lines 95-99, paint_glyph lines 4806-4839,
// paint_glyph_from_atlas lines 4841-4939, paint_emoji lines 4957-4988,
// the shaped-line glyph loop in text_system/line.rs
// paint_text_fragment lines 271-319.
//
// Deviations from the pin (bounded, each recorded):
//
// * The pin's Window owns the text system, sprite atlas and window
//   state; the port's Scene paint methods take them as parameters (the
//   port's Scene is the ticket08 scene kernel facade, and the window
//   host/renderer integration of later tickets will own them).
// * The pin's paint_glyph_from_atlas consults TextSystem::raster_metadata
//   (pub(crate) in gpui, not ABI-exposable) to skip rasterization when
//   the metadata cache already knows the glyph. The port rasterizes and
//   lets the atlas-level map dedup prevent re-upload and duplicate
//   tiles: the observable atlas behavior (one tile per key, one upload)
//   is identical, the raster work is repeated. The native
//   TextSystem::rasterize_glyph keeps the pin's metadata-consistency
//   check either way.
// * The pin's should_use_subpixel_rendering consults the platform
//   window's background transparency, is_subpixel_rendering_supported
//   and the per-window TextRenderingMode override. The port's host
//   window does not expose background appearance yet; the mode falls
//   back to the platform recommendation (recommended_rendering_mode),
//   and the caller can pass an explicit override through
//   PaintTextOptions.

import (
	"fmt"
	"math"

	"gpui-go/internal/native"
)

// subpixelVariantsX/Y mirror the pinned SUBPIXEL_VARIANTS_X/Y (4, 1).
const (
	subpixelVariantsX = 4
	subpixelVariantsY = 1
)

// ---------------------------------------------------------------------------
// Sprite primitives (the pinned MonochromeSprite/SubpixelSprite/
// PolychromeSprite, crates/gpui/src/scene.rs:1069-1121)
// ---------------------------------------------------------------------------

// TransformationMatrix is the pinned 2D affine transformation (unit by
// default): rotation/scale 2x2 plus translation.
type TransformationMatrix struct {
	// RotationScale is [[a, b], [c, d]].
	RotationScale [2][2]float32
	// Translation is [tx, ty].
	Translation [2]float32
}

// UnitTransformation is the pinned TransformationMatrix::unit.
func UnitTransformation() TransformationMatrix {
	return TransformationMatrix{
		RotationScale: [2][2]float32{{1, 0}, {0, 1}},
	}
}

// MonochromeSprite is an R8 atlas tile drawn as a textured quad with a
// mask color (the pinned MonochromeSprite). Geometry is device pixels;
// Order is kernel-assigned.
type MonochromeSprite struct {
	// Order is the draw order (kernel-assigned on insert).
	Order uint32
	// Padding (payload; 0 from the glyph paint path).
	Padding uint32
	// Bounds is the sprite's on-screen placement, device pixels.
	Bounds Bounds
	// ContentMask is the clip bounds, device pixels.
	ContentMask Bounds
	// Color is the mask color (the coverage tint).
	Color Hsla
	// Tile is the atlas tile this sprite samples.
	Tile AtlasTile
	// Transformation is the vertex transformation (unit from the glyph
	// paint path).
	Transformation TransformationMatrix
}

// SubpixelSprite is a BGRA LCD-coverage atlas tile blended with a mask
// color (the pinned SubpixelSprite; same field set as MonochromeSprite).
type SubpixelSprite struct {
	// Order is the draw order (kernel-assigned on insert).
	Order uint32
	// Padding (payload).
	Padding uint32
	// Bounds is the sprite's on-screen placement, device pixels.
	Bounds Bounds
	// ContentMask is the clip bounds, device pixels.
	ContentMask Bounds
	// Color is the LCD blend color.
	Color Hsla
	// Tile is the atlas tile this sprite samples.
	Tile AtlasTile
	// Transformation is the vertex transformation (unit from the glyph
	// paint path).
	Transformation TransformationMatrix
}

// PolychromeSprite is a BGRA color atlas tile drawn with its own opacity
// and corner smoothing (the pinned PolychromeSprite: no color, no
// transformation).
type PolychromeSprite struct {
	// Order is the draw order (kernel-assigned on insert).
	Order uint32
	// Grayscale selects grayscale sampling (the pinned ShaderBool).
	Grayscale bool
	// Opacity is the sprite opacity.
	Opacity float32
	// CornerSmoothing in [0, 1].
	CornerSmoothing float32
	// Bounds is the sprite's on-screen placement, device pixels.
	Bounds Bounds
	// ContentMask is the clip bounds, device pixels.
	ContentMask Bounds
	// CornerRadii, device pixels.
	CornerRadii Corners
	// Tile is the atlas tile this sprite samples.
	Tile AtlasTile
}

// spriteTileKindValid checks the pinned texture-kind/pool pairing: a
// monochrome sprite's tile must live in the monochrome pool, a subpixel
// sprite's in the subpixel pool, a polychrome sprite's in the polychrome
// pool (the renderer contract's "never reinterpret one format as
// another").
func spriteTileKindValid(tile AtlasTile, kind AtlasTextureKind) bool {
	return AtlasTextureKind(tile.TextureKind) == kind
}

// InsertMonochromeSprite inserts a monochrome sprite record (the pinned
// Scene::insert_primitive(MonochromeSprite)). Order must be zero; the
// kernel assigns it. A sprite whose clipped bounds are empty is
// dropped.
func (s *Scene) InsertMonochromeSprite(sprite MonochromeSprite) error {
	if s.kernel == nil {
		return ErrSceneClosed
	}
	return s.kernel.insertMonochromeSprite(sprite)
}

// InsertSubpixelSprite inserts a subpixel sprite record.
func (s *Scene) InsertSubpixelSprite(sprite SubpixelSprite) error {
	if s.kernel == nil {
		return ErrSceneClosed
	}
	return s.kernel.insertSubpixelSprite(sprite)
}

// InsertPolychromeSprite inserts a polychrome sprite record.
func (s *Scene) InsertPolychromeSprite(sprite PolychromeSprite) error {
	if s.kernel == nil {
		return ErrSceneClosed
	}
	return s.kernel.insertPolychromeSprite(sprite)
}

// MonochromeSprites dumps the finished scene's monochrome sprite records.
func (s *Scene) MonochromeSprites() ([]MonochromeSprite, error) {
	return s.kernel.monochromeSprites()
}

// SubpixelSprites dumps the finished scene's subpixel sprite records.
func (s *Scene) SubpixelSprites() ([]SubpixelSprite, error) {
	return s.kernel.subpixelSprites()
}

// PolychromeSprites dumps the finished scene's polychrome sprite records.
func (s *Scene) PolychromeSprites() ([]PolychromeSprite, error) {
	return s.kernel.polychromeSprites()
}

// insertMonochromeSprite validates and inserts one monochrome sprite
// (the tile must come from the monochrome pool).
func (k *SceneKernel) insertMonochromeSprite(sprite MonochromeSprite) error {
	if err := k.checkLive("InsertMonochromeSprite"); err != nil {
		return err
	}
	if sprite.Order != 0 {
		return fmt.Errorf("gpui: scene insert monochrome sprite: record order must be zero, got %d (the kernel assigns the draw order)", sprite.Order)
	}
	if !spriteTileKindValid(sprite.Tile, AtlasTextureMonochrome) {
		return fmt.Errorf("gpui: scene insert monochrome sprite: tile kind %d does not match the monochrome pool (never reinterpret one format as another)", sprite.Tile.TextureKind)
	}
	rec := maskSpriteRecord(sprite.Order, sprite.Padding, sprite.Bounds, sprite.ContentMask, sprite.Color, sprite.Tile, sprite.Transformation)
	if err := k.scene.InsertMonochromeSprite(&rec); err != nil {
		return fmt.Errorf("gpui: scene insert monochrome sprite: %w", err)
	}
	return nil
}

// insertSubpixelSprite validates and inserts one subpixel sprite (the
// tile must come from the subpixel pool).
func (k *SceneKernel) insertSubpixelSprite(sprite SubpixelSprite) error {
	if err := k.checkLive("InsertSubpixelSprite"); err != nil {
		return err
	}
	if sprite.Order != 0 {
		return fmt.Errorf("gpui: scene insert subpixel sprite: record order must be zero, got %d (the kernel assigns the draw order)", sprite.Order)
	}
	if !spriteTileKindValid(sprite.Tile, AtlasTextureSubpixel) {
		return fmt.Errorf("gpui: scene insert subpixel sprite: tile kind %d does not match the subpixel pool (never reinterpret one format as another)", sprite.Tile.TextureKind)
	}
	rec := maskSpriteRecord(sprite.Order, sprite.Padding, sprite.Bounds, sprite.ContentMask, sprite.Color, sprite.Tile, sprite.Transformation)
	if err := k.scene.InsertSubpixelSprite(&rec); err != nil {
		return fmt.Errorf("gpui: scene insert subpixel sprite: %w", err)
	}
	return nil
}

// maskSpriteRecord packs the shared monochrome/subpixel field set into
// the native record.
func maskSpriteRecord(order, padding uint32, bounds, mask Bounds, color Hsla, tile AtlasTile, transformation TransformationMatrix) native.SceneSpriteRecord {
	rec := native.NewSceneSpriteRecord()
	rec.Padding = padding
	rec.Bounds = sceneBoundsRecord(bounds)
	rec.ContentMask = sceneBoundsRecord(mask)
	rec.Color = sceneColorRecord(color)
	rec.Tile = tile.sceneTile()
	rec.Transformation = [6]uint32{
		math.Float32bits(transformation.RotationScale[0][0]),
		math.Float32bits(transformation.RotationScale[0][1]),
		math.Float32bits(transformation.RotationScale[1][0]),
		math.Float32bits(transformation.RotationScale[1][1]),
		math.Float32bits(transformation.Translation[0]),
		math.Float32bits(transformation.Translation[1]),
	}
	return rec
}

// insertPolychromeSprite validates and inserts one polychrome sprite.
func (k *SceneKernel) insertPolychromeSprite(sprite PolychromeSprite) error {
	if err := k.checkLive("InsertPolychromeSprite"); err != nil {
		return err
	}
	if sprite.Order != 0 {
		return fmt.Errorf("gpui: scene insert polychrome sprite: record order must be zero, got %d (the kernel assigns the draw order)", sprite.Order)
	}
	if !spriteTileKindValid(sprite.Tile, AtlasTexturePolychrome) {
		return fmt.Errorf("gpui: scene insert polychrome sprite: tile kind %d does not match the polychrome pool (never reinterpret one format as another)", sprite.Tile.TextureKind)
	}
	rec := native.NewScenePolychromeSpriteRecord()
	grayscale := uint32(0)
	if sprite.Grayscale {
		grayscale = 1
	}
	rec.Grayscale = grayscale
	rec.Opacity = math.Float32bits(sprite.Opacity)
	rec.CornerSmoothing = math.Float32bits(sprite.CornerSmoothing)
	rec.Bounds = sceneBoundsRecord(sprite.Bounds)
	rec.ContentMask = sceneBoundsRecord(sprite.ContentMask)
	rec.CornerRadii = [4]uint32{
		math.Float32bits(sprite.CornerRadii.TopLeft),
		math.Float32bits(sprite.CornerRadii.TopRight),
		math.Float32bits(sprite.CornerRadii.BottomRight),
		math.Float32bits(sprite.CornerRadii.BottomLeft),
	}
	rec.Tile = sprite.Tile.sceneTile()
	if err := k.scene.InsertPolychromeSprite(&rec); err != nil {
		return fmt.Errorf("gpui: scene insert polychrome sprite: %w", err)
	}
	return nil
}

// monochromeSprites dumps the finished scene's monochrome sprites.
func (k *SceneKernel) monochromeSprites() ([]MonochromeSprite, error) {
	records, err := k.scene.DumpMonochromeSprites()
	if err != nil {
		return nil, fmt.Errorf("gpui: scene dump monochrome sprites: %w", err)
	}
	out := make([]MonochromeSprite, 0, len(records))
	for _, rec := range records {
		out = append(out, MonochromeSprite{
			Order:          rec.Order,
			Padding:        rec.Padding,
			Bounds:         sceneBoundsValue(rec.Bounds),
			ContentMask:    sceneBoundsValue(rec.ContentMask),
			Color:          sceneColorValue(rec.Color),
			Tile:           atlasTileOf(rec.Tile),
			Transformation: transformationOf(rec.Transformation),
		})
	}
	return out, nil
}

// subpixelSprites dumps the finished scene's subpixel sprites.
func (k *SceneKernel) subpixelSprites() ([]SubpixelSprite, error) {
	records, err := k.scene.DumpSubpixelSprites()
	if err != nil {
		return nil, fmt.Errorf("gpui: scene dump subpixel sprites: %w", err)
	}
	out := make([]SubpixelSprite, 0, len(records))
	for _, rec := range records {
		out = append(out, SubpixelSprite{
			Order:          rec.Order,
			Padding:        rec.Padding,
			Bounds:         sceneBoundsValue(rec.Bounds),
			ContentMask:    sceneBoundsValue(rec.ContentMask),
			Color:          sceneColorValue(rec.Color),
			Tile:           atlasTileOf(rec.Tile),
			Transformation: transformationOf(rec.Transformation),
		})
	}
	return out, nil
}

// polychromeSprites dumps the finished scene's polychrome sprites.
func (k *SceneKernel) polychromeSprites() ([]PolychromeSprite, error) {
	records, err := k.scene.DumpPolychromeSprites()
	if err != nil {
		return nil, fmt.Errorf("gpui: scene dump polychrome sprites: %w", err)
	}
	out := make([]PolychromeSprite, 0, len(records))
	for _, rec := range records {
		out = append(out, PolychromeSprite{
			Order:           rec.Order,
			Grayscale:       rec.Grayscale != 0,
			Opacity:         math.Float32frombits(rec.Opacity),
			CornerSmoothing: math.Float32frombits(rec.CornerSmoothing),
			Bounds:          sceneBoundsValue(rec.Bounds),
			ContentMask:     sceneBoundsValue(rec.ContentMask),
			CornerRadii: Corners{
				TopLeft:     math.Float32frombits(rec.CornerRadii[0]),
				TopRight:    math.Float32frombits(rec.CornerRadii[1]),
				BottomRight: math.Float32frombits(rec.CornerRadii[2]),
				BottomLeft:  math.Float32frombits(rec.CornerRadii[3]),
			},
			Tile: atlasTileOf(rec.Tile),
		})
	}
	return out, nil
}

// atlasTileOf converts a native scene tile record back into the value.
func atlasTileOf(rec native.SceneTileRecord) AtlasTile {
	return AtlasTile{
		TextureIndex: rec.TextureIndex,
		TextureKind:  AtlasTextureKind(rec.TextureKind),
		TileID:       rec.TileID,
		Padding:      rec.Padding,
		BoundsX:      rec.BoundsX,
		BoundsY:      rec.BoundsY,
		BoundsW:      rec.BoundsW,
		BoundsH:      rec.BoundsH,
	}
}

// SpriteSceneCommand is one compiled render-plan command as it crosses
// the native ABI — the pinned RenderCommand/PrimitiveBatch surface,
// including the sprite batches' atlas texture index (the
// pre-ticket10 SceneCommand value type omits it because no batch
// carried one then; this mirror exposes the full record without
// changing that type).
type SpriteSceneCommand struct {
	// Kind is the command tag: 0 batch, 1 begin filter, 2 end filter.
	Kind uint32
	// PrimitiveKind is the batch primitive tag (the pinned
	// PrimitiveKind discriminants: 0 shadows, 1 quads, 3 underlines,
	// 4 monochrome sprites, 5 subpixel sprites, 6 polychrome sprites,
	// 7 surfaces, 8 backdrop filters, 9 filter boundary).
	PrimitiveKind uint32
	// RangeStart/RangeEnd is the [start, end) primitive-array range of a
	// batch command.
	RangeStart, RangeEnd uint32
	// Smoothed marks corner-smoothed batches (quads, shadows and
	// polychrome sprites).
	Smoothed bool
	// TextureIndex is the batch's atlas texture (sprite batches; the
	// pinned PrimitiveBatch's texture_id index).
	TextureIndex uint32
	// BoundaryIndex/ClosingBoundaryIndex are the filter-boundary indices
	// of filter commands.
	BoundaryIndex, ClosingBoundaryIndex uint32
	// FilterTarget is 0 inline / 1 isolated (with the target pool index
	// in TargetIndex).
	FilterTarget, TargetIndex uint32
}

// SpriteCommands returns the finished scene's compiled plan commands
// with the sprite batches' atlas texture indices (the native command
// record surface; SceneCommands returns the older value view).
func (s *Scene) SpriteCommands() ([]SpriteSceneCommand, error) {
	if s.kernel == nil {
		return nil, ErrSceneClosed
	}
	records, err := s.kernel.nativeScene().PlanCommands()
	if err != nil {
		return nil, fmt.Errorf("gpui: scene plan: %w", err)
	}
	out := make([]SpriteSceneCommand, 0, len(records))
	for _, rec := range records {
		out = append(out, SpriteSceneCommand{
			Kind:                 rec.CommandKind,
			PrimitiveKind:        rec.PrimitiveKind,
			RangeStart:           rec.RangeStart,
			RangeEnd:             rec.RangeEnd,
			Smoothed:             rec.Smoothed != 0,
			TextureIndex:         rec.TextureIndex,
			BoundaryIndex:        rec.BoundaryIndex,
			ClosingBoundaryIndex: rec.ClosingBoundaryIndex,
			FilterTarget:         rec.FilterTarget,
			TargetIndex:          rec.TargetIndex,
		})
	}
	return out, nil
}

// SpriteSceneMeta is the scene totals observable including the three
// sprite class counts (the native meta record surface; SceneMeta omits
// them because no sprite class existed when that type landed).
type SpriteSceneMeta struct {
	// OpCount is the paint-operation count.
	OpCount uint32
	// Monochrome/Subpixel/PolychromeSpriteCount are the per-class sprite
	// counts.
	MonochromeSpriteCount uint32
	SubpixelSpriteCount   uint32
	PolychromeSpriteCount uint32
	// IsFinished is true after Finish.
	IsFinished bool
}

// SpriteMeta returns the scene totals with the sprite counts.
func (s *Scene) SpriteMeta() (SpriteSceneMeta, error) {
	if s.kernel == nil {
		return SpriteSceneMeta{}, ErrSceneClosed
	}
	rec, err := s.kernel.nativeScene().Meta()
	if err != nil {
		return SpriteSceneMeta{}, fmt.Errorf("gpui: scene meta: %w", err)
	}
	return SpriteSceneMeta{
		OpCount:               rec.OpCount,
		MonochromeSpriteCount: rec.MonochromeSpriteCount,
		SubpixelSpriteCount:   rec.SubpixelSpriteCount,
		PolychromeSpriteCount: rec.PolychromeSpriteCount,
		IsFinished:            rec.IsFinished != 0,
	}, nil
}

// transformationOf converts the packed transformation bits back into
// the value.
func transformationOf(bits [6]uint32) TransformationMatrix {
	return TransformationMatrix{
		RotationScale: [2][2]float32{
			{math.Float32frombits(bits[0]), math.Float32frombits(bits[1])},
			{math.Float32frombits(bits[2]), math.Float32frombits(bits[3])},
		},
		Translation: [2]float32{math.Float32frombits(bits[4]), math.Float32frombits(bits[5])},
	}
}

// quantizeGlyphOrigin is the pinned quantize_glyph_origin
// (window.rs:82-93): the origin is quantized to the subpixel grid, split
// into the integer origin and the subpixel variant of each axis.
func quantizeGlyphOrigin(x, y float32) (ix, iy float32, vx, vy uint8) {
	axis := func(value float32, variants int) (float32, uint8) {
		quantized := roundHalfTowardZero(value*float32(variants)) / float32(variants)
		integer := float32(math.Floor(float64(quantized)))
		variant := int(math.Round(float64((quantized - integer) * float32(variants))))
		if variant > variants-1 {
			variant = variants - 1
		}
		if variant < 0 {
			variant = 0
		}
		return integer, uint8(variant)
	}
	ix, vx = axis(x, subpixelVariantsX)
	iy, vy = axis(y, subpixelVariantsY)
	return ix, iy, vx, vy
}

// quantizeColorGlyphOrigin is the pinned quantize_color_glyph_origin
// (window.rs:95-99): color glyphs align to whole pixels with no subpixel
// variant.
func quantizeColorGlyphOrigin(x, y float32) (float32, float32) {
	return roundHalfTowardZero(x), roundHalfTowardZero(y)
}

// hslaToRgba is the pinned TextSystem::prepare_raster_style color path
// (crates/gpui/src/text_system.rs:266-279): the Hsla scene color is
// converted to Rgba through palette's FromColorUnclamped<Hsl> for Rgb
// (palette 0.7.7, crates/../../registry/src/…/palette-0.7.7/src/rgb/
// rgb.rs::from_color_unclamped). This is the bit-exact f32 port of that
// conversion: c = (1 - |2l-1|)·s, h = hue_degrees/60, zone table for
// (r,g,b) candidates, m = l - c/2, alpha carried through.
func hslaToRgba(color Hsla) Rgba {
	// The port's Hsla stores the hue as the [0,1] fraction of the circle;
	// palette's Hsl stores positive degrees.
	degrees := color.H * 360
	c := (1 - abs32(2*color.L-1)) * color.S
	h := degrees / 60
	hModTwo := h - float32(math.Floor(float64(h*0.5)))*2
	x := c * (1 - abs32(hModTwo-1))
	m := color.L - c*0.5

	var r, g, b float32
	// The zones: [0,1) r=c g=x; [1,2) r=x g=c; [2,3) g=c b=x; [3,4) g=x
	// b=c; [4,5) r=x b=c; [5,6) r=c b=x.
	switch {
	case h >= 0 && h < 1:
		r, g, b = c, x, 0
	case h >= 1 && h < 2:
		r, g, b = x, c, 0
	case h >= 2 && h < 3:
		r, g, b = 0, c, x
	case h >= 3 && h < 4:
		r, g, b = 0, x, c
	case h >= 4 && h < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return Rgba{R: r + m, G: g + m, B: b + m, A: color.A}
}

func abs32(v float32) float32 {
	return float32(math.Abs(float64(v)))
}

// whiteRgba is the pinned white() scene color of the emoji path.
func whiteRgba() Rgba {
	return Rgba{R: 1, G: 1, B: 1, A: 1}
}

// PaintTextOptions parameterizes the text paint path's mode selection
// (the pinned should_use_subpixel_rendering inputs the port's host
// window does not expose yet).
type PaintTextOptions struct {
	// RenderingMode overrides the platform recommendation when not
	// PlatformDefault (the pinned per-window text_rendering_mode).
	RenderingMode TextRenderingMode
}

// shouldUseSubpixelRendering resolves the requested mode for ordinary
// text (the bounded port of the pinned should_use_subpixel_rendering;
// see the module docs for the recorded deviation).
func shouldUseSubpixelRendering(ts *TextSystem, opts PaintTextOptions) (bool, error) {
	mode := opts.RenderingMode
	if mode == TextRenderingPlatformDefault {
		recommended, err := ts.RecommendedRenderingMode()
		if err != nil {
			return false, err
		}
		mode = recommended
	}
	return mode == TextRenderingSubpixel, nil
}

// PaintGlyph paints one monochrome glyph into the scene at the current
// z-index (the pinned Window::paint_glyph, window.rs:4806-4839): the
// y component of origin is the glyph's baseline.
func (s *Scene) PaintGlyph(
	origin Point,
	fontID FontID,
	glyphID uint32,
	fontSize float32,
	color Hsla,
	ts *TextSystem,
	atlas *Atlas,
	ctx *PaintContext,
	opts PaintTextOptions,
) error {
	elementOpacity := ctx.ElementOpacity
	glyphOrigin := Point{
		X: origin.X * ctx.ScaleFactor,
		Y: origin.Y * ctx.ScaleFactor,
	}
	integerX, integerY, subpixelX, subpixelY := quantizeGlyphOrigin(glyphOrigin.X, glyphOrigin.Y)
	integerOrigin := Point{X: integerX, Y: integerY}

	subpixel, err := shouldUseSubpixelRendering(ts, opts)
	if err != nil {
		return fmt.Errorf("gpui: PaintGlyph: %w", err)
	}
	requestedMode := GlyphRenderGrayscale
	if subpixel {
		requestedMode = GlyphRenderSubpixel
	}

	rasterStyle, err := ts.PrepareRasterStyle(hslaToRgba(color), requestedMode)
	if err != nil {
		return fmt.Errorf("gpui: PaintGlyph: %w", err)
	}

	params := RasterGlyphParams{
		FontID:      fontID,
		GlyphID:     glyphID,
		FontSize:    fontSize,
		SubpixelX:   subpixelX,
		SubpixelY:   subpixelY,
		ScaleFactor: ctx.ScaleFactor,
		RasterStyle: rasterStyle,
	}
	return s.paintGlyphFromAtlas(integerOrigin, params, color, elementOpacity, ts, atlas, ctx)
}

// PaintColorGlyph paints one color glyph (emoji/COLR artwork) into the
// scene (the pinned Window::paint_emoji, window.rs:4957-4988): color
// glyphs align to whole pixels and rasterize in Color mode with the
// white scene color.
func (s *Scene) PaintColorGlyph(
	origin Point,
	fontID FontID,
	glyphID uint32,
	fontSize float32,
	ts *TextSystem,
	atlas *Atlas,
	ctx *PaintContext,
) error {
	elementOpacity := ctx.ElementOpacity
	glyphOrigin := Point{
		X: origin.X * ctx.ScaleFactor,
		Y: origin.Y * ctx.ScaleFactor,
	}
	integerX, integerY := quantizeColorGlyphOrigin(glyphOrigin.X, glyphOrigin.Y)
	integerOrigin := Point{X: integerX, Y: integerY}

	rasterStyle, err := ts.PrepareRasterStyle(whiteRgba(), GlyphRenderColor)
	if err != nil {
		return fmt.Errorf("gpui: PaintColorGlyph: %w", err)
	}
	params := RasterGlyphParams{
		FontID:      fontID,
		GlyphID:     glyphID,
		FontSize:    fontSize,
		SubpixelX:   0,
		SubpixelY:   0,
		ScaleFactor: ctx.ScaleFactor,
		RasterStyle: rasterStyle,
	}
	return s.paintGlyphFromAtlas(integerOrigin, params, Hsla{A: 1}, elementOpacity, ts, atlas, ctx)
}

// paintGlyphFromAtlas is the pinned paint_glyph_from_atlas
// (window.rs:4841-4939): rasterize, skip empty bounds, insert into the
// atlas and insert the format's sprite primitive with the bounds
// integer origin + raster bounds origin.
func (s *Scene) paintGlyphFromAtlas(
	integerOrigin Point,
	params RasterGlyphParams,
	maskColor Hsla,
	opacity float32,
	ts *TextSystem,
	atlas *Atlas,
	ctx *PaintContext,
) error {
	rasterized, err := ts.RasterizeGlyph(params)
	if err != nil {
		return fmt.Errorf("gpui: paintGlyphFromAtlas: %w", err)
	}
	if rasterized.BoundsW == 0 || rasterized.BoundsH == 0 {
		// Empty rasters (spaces, empty outlines) never insert.
		return nil
	}

	tile, err := atlas.InsertGlyph(rasterized, params)
	if err != nil {
		return fmt.Errorf("gpui: paintGlyphFromAtlas: %w", err)
	}

	bounds := Bounds{
		Origin: Point{
			X: integerOrigin.X + float32(rasterized.BoundsX),
			Y: integerOrigin.Y + float32(rasterized.BoundsY),
		},
		Size: Size{
			Width:  float32(tile.BoundsW),
			Height: float32(tile.BoundsH),
		},
	}
	mask := snappedContentMask(ctx.Mask, ctx.ScaleFactor)

	switch rasterized.Format {
	case RasterFormatAlphaMask:
		return s.InsertMonochromeSprite(MonochromeSprite{
			Bounds:         bounds,
			ContentMask:    mask,
			Color:          OpacityHsla(maskColor, opacity),
			Tile:           tile,
			Transformation: UnitTransformation(),
		})
	case RasterFormatBgraSubpixelMask:
		return s.InsertSubpixelSprite(SubpixelSprite{
			Bounds:         bounds,
			ContentMask:    mask,
			Color:          OpacityHsla(maskColor, opacity),
			Tile:           tile,
			Transformation: UnitTransformation(),
		})
	case RasterFormatBgraColor:
		return s.InsertPolychromeSprite(PolychromeSprite{
			Opacity:     opacity,
			Bounds:      bounds,
			ContentMask: mask,
			Tile:        tile,
		})
	default:
		return fmt.Errorf("gpui: paintGlyphFromAtlas: unknown raster format %d", rasterized.Format)
	}
}

// PaintShapedText paints a shaped line's glyphs into the scene (the
// pinned paint_text_fragment glyph loop, line.rs:271-319): per glyph,
// cull against the content mask using the font's bounding box, then
// paint through PaintGlyph (is_emoji glyphs take the color path,
// window.rs paint_emoji).
func (s *Scene) PaintShapedText(
	line *WrappedLine,
	origin Point,
	lineHeight float32,
	color Hsla,
	ts *TextSystem,
	atlas *Atlas,
	ctx *PaintContext,
	opts PaintTextOptions,
) error {
	if line == nil {
		return fmt.Errorf("gpui: PaintShapedText: nil line")
	}
	fragments, err := line.Fragments()
	if err != nil {
		return fmt.Errorf("gpui: PaintShapedText: %w", err)
	}
	glyphs, err := line.Glyphs()
	if err != nil {
		return fmt.Errorf("gpui: PaintShapedText: %w", err)
	}
	summary, err := line.Summary()
	if err != nil {
		return fmt.Errorf("gpui: PaintShapedText: %w", err)
	}

	// The pinned row placement: baseline = (line_height - ascent -
	// descent)/2 + ascent from the row top.
	paddingTop := (lineHeight - summary.Ascent - summary.Descent) / 2
	baseline := origin.Y + paddingTop + summary.Ascent

	for _, fragment := range fragments {
		// The pin's cull box: the font's bounding box at the fragment's
		// font size, placed at the glyph's line origin.
		metrics, err := ts.FontMetrics(fragment.FontID)
		if err != nil {
			return fmt.Errorf("gpui: PaintShapedText: %w", err)
		}
		maxGlyphSize := Size{
			Width:  metrics.BoundingBoxWidth * fragment.FontSize / float32(metrics.UnitsPerEm),
			Height: (metrics.BoundingBoxHeight) * fragment.FontSize / float32(metrics.UnitsPerEm),
		}
		for i := 0; i < fragment.GlyphCount; i++ {
			glyph := glyphs[fragment.GlyphStart+i]
			cullOrigin := Point{
				X: origin.X + glyph.X,
				Y: origin.Y,
			}
			if !intersectsBounds(
				Bounds{Origin: cullOrigin, Size: maxGlyphSize},
				ctx.Mask,
			) {
				continue
			}

			glyphOrigin := Point{
				X: origin.X + glyph.X,
				Y: baseline + glyph.Y,
			}
			if glyph.IsEmoji {
				if err := s.PaintColorGlyph(glyphOrigin, fragment.FontID, glyph.ID, fragment.FontSize, ts, atlas, ctx); err != nil {
					return err
				}
			} else {
				if err := s.PaintGlyph(glyphOrigin, fragment.FontID, glyph.ID, fragment.FontSize, color, ts, atlas, ctx, opts); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// PaintShapedTextLine paints one visual line of a shaped document at
// an explicit origin and baseline (the inline piece paint path — the
// pinned paint_visual_line: the line's fragments paint at the line
// origin with the row's baseline; the ordinary PaintShapedText derives
// its own single baseline from the line height and is the text
// element's bounded path).
func (s *Scene) PaintShapedTextLine(
	line *WrappedLine,
	lineIndex int,
	origin Point,
	baselineY float32,
	color Hsla,
	ts *TextSystem,
	atlas *Atlas,
	ctx *PaintContext,
) error {
	if line == nil {
		return fmt.Errorf("gpui: PaintShapedTextLine: nil line")
	}
	summary, err := line.Summary()
	if err != nil {
		return fmt.Errorf("gpui: PaintShapedTextLine: %w", err)
	}
	if lineIndex < 0 || lineIndex >= summary.LineCount {
		return fmt.Errorf("gpui: PaintShapedTextLine: line index %d out of range (%d lines)", lineIndex, summary.LineCount)
	}
	record, err := line.Line(lineIndex, 1)
	if err != nil {
		return fmt.Errorf("gpui: PaintShapedTextLine: %w", err)
	}
	fragments, err := line.Fragments()
	if err != nil {
		return fmt.Errorf("gpui: PaintShapedTextLine: %w", err)
	}
	glyphs, err := line.Glyphs()
	if err != nil {
		return fmt.Errorf("gpui: PaintShapedTextLine: %w", err)
	}
	if record.FragmentEnd < record.FragmentStart || record.FragmentEnd > len(fragments) {
		return fmt.Errorf("gpui: PaintShapedTextLine: fragment range %d..%d out of range", record.FragmentStart, record.FragmentEnd)
	}
	for _, fragment := range fragments[record.FragmentStart:record.FragmentEnd] {
		metrics, err := ts.FontMetrics(fragment.FontID)
		if err != nil {
			return fmt.Errorf("gpui: PaintShapedTextLine: %w", err)
		}
		maxGlyphSize := Size{
			Width:  metrics.BoundingBoxWidth * fragment.FontSize / float32(metrics.UnitsPerEm),
			Height: metrics.BoundingBoxHeight * fragment.FontSize / float32(metrics.UnitsPerEm),
		}
		for i := 0; i < fragment.GlyphCount; i++ {
			glyph := glyphs[fragment.GlyphStart+i]
			cullOrigin := Point{
				X: origin.X + glyph.X,
				Y: origin.Y,
			}
			if !intersectsBounds(
				Bounds{Origin: cullOrigin, Size: maxGlyphSize},
				ctx.Mask,
			) {
				continue
			}
			glyphOrigin := Point{
				X: origin.X + glyph.X,
				Y: baselineY + glyph.Y,
			}
			if glyph.IsEmoji {
				if err := s.PaintColorGlyph(glyphOrigin, fragment.FontID, glyph.ID, fragment.FontSize, ts, atlas, ctx); err != nil {
					return err
				}
			} else {
				if err := s.PaintGlyph(glyphOrigin, fragment.FontID, glyph.ID, fragment.FontSize, color, ts, atlas, ctx, PaintTextOptions{}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// intersectsBounds reports whether two logical-pixel bounds intersect
// (the pinned Bounds::intersects).
func intersectsBounds(a, b Bounds) bool {
	return a.Origin.X < b.Origin.X+b.Size.Width &&
		a.Origin.X+a.Size.Width > b.Origin.X &&
		a.Origin.Y < b.Origin.Y+b.Size.Height &&
		a.Origin.Y+a.Size.Height > b.Origin.Y
}
