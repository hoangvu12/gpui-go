package gpui

// This file is the port's atlas half of ticket10: the typed seam over
// the native texture atlas service (reserved slot 6, capability
// "atlas-d3d11-v1"; see internal/native/atlas.go and
// reference/native/ATLAS_ABI.md). The native service is the pinned
// D3D11 sprite atlas ported from
// crates/gpui_windows/src/directx_atlas.rs at
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a (etagere 0.2.15 bucketed
// packing; monochrome R8_UNORM / polychrome and subpixel B8G8R8A8_UNORM
// pools; upload validation before allocation; tile removal with texture
// free-listing; generation-invalidated tiles on device loss), bound to
// the renderer service's shared device exactly where the pin passes the
// renderer's device into DirectXAtlas::new.
//
// The renderer contract's ownership rule is preserved: "Go obtains
// raster bytes before calling the atlas insertion seam" — the atlas
// never calls back into Go builders; InsertGlyph receives the finished
// raster. A cached key returns the existing tile with no upload (the
// pin's get_or_insert_with map hit).

import (
	"errors"
	"fmt"
	"math"
	"sync"

	"gpui-go/internal/native"
)

// AtlasTextureKind selects the atlas pool (the pinned
// AtlasTextureKind).
type AtlasTextureKind uint32

const (
	// AtlasTextureMonochrome is the R8 coverage pool.
	AtlasTextureMonochrome AtlasTextureKind = 0
	// AtlasTexturePolychrome is the BGRA color pool.
	AtlasTexturePolychrome AtlasTextureKind = 1
	// AtlasTextureSubpixel is the BGRA LCD-coverage pool.
	AtlasTextureSubpixel AtlasTextureKind = 2
)

// AtlasTile is a placed atlas tile (the pinned AtlasTile minus the
// generation bookkeeping the ABI adds): the texture pool index, the
// tile id within that texture and the tile bounds inside the texture.
type AtlasTile struct {
	// TextureIndex is the tile's texture within its kind pool.
	TextureIndex uint32
	// TextureKind is the pool the tile lives in.
	TextureKind AtlasTextureKind
	// TileID is the tile's id within its texture (the serialized etagere
	// allocation id; the pinned TileId).
	TileID uint32
	// Padding around the tile content (0 for glyph tiles).
	Padding uint32
	// BoundsX/BoundsY/BoundsW/BoundsH is the tile rectangle inside the
	// texture, in texels.
	BoundsX, BoundsY, BoundsW, BoundsH int32
	// Generation is the atlas generation that produced this tile; tiles
	// from an earlier generation are invalid after device loss.
	Generation uint32
}

// sceneTile converts the tile into the sprite record's tile payload.
func (t AtlasTile) sceneTile() native.SceneTileRecord {
	return native.SceneTileRecord{
		TextureIndex: t.TextureIndex,
		TextureKind:  uint32(t.TextureKind),
		TileID:       t.TileID,
		Padding:      t.Padding,
		BoundsX:      t.BoundsX,
		BoundsY:      t.BoundsY,
		BoundsW:      t.BoundsW,
		BoundsH:      t.BoundsH,
	}
}

var (
	// ErrAtlasService reports a failure to load/validate the native atlas
	// service.
	ErrAtlasService = errors.New("gpui: native atlas service unavailable")
	// ErrAtlasTile is a placeholder alias of the stale-tile error (kept
	// for future draw-side consumers).
	ErrAtlasTile = errors.New("gpui: atlas tile unavailable")
)

// Atlas is a native texture atlas instance. Its methods are safe for
// concurrent use (the native service serializes every entry), but the
// type is not the per-frame cache owner: scene/submission retirement
// and cache release are the application's (and later tickets')
// responsibility, per the renderer contract ("cache release and
// submission completion are separate prerequisites for tile reuse").
type Atlas struct {
	svc    *native.AtlasService
	handle native.AtlasHandle

	closeOnce sync.Once
	closeErr  error
}

var (
	atlasServiceOnce sync.Once
	atlasServiceVal  *native.AtlasService
	atlasServiceErr  error
)

// atlasService loads the process-global native atlas service handle.
func atlasService() (*native.AtlasService, error) {
	atlasServiceOnce.Do(func() {
		lib, err := native.Load(native.Options{})
		if err != nil {
			atlasServiceErr = fmt.Errorf("gpui: %w: loading the native artifact: %w", ErrAtlasService, err)
			return
		}
		svc, err := lib.Atlas()
		if err != nil {
			atlasServiceErr = fmt.Errorf("gpui: %w: %w", ErrAtlasService, err)
			return
		}
		atlasServiceVal = svc
	})
	return atlasServiceVal, atlasServiceErr
}

// NewAtlas creates an atlas bound to the renderer service's shared
// D3D11 device (the pinned construction site: the renderer's device and
// immediate context).
func NewAtlas() (*Atlas, error) {
	svc, err := atlasService()
	if err != nil {
		return nil, err
	}
	handle, err := svc.Create()
	if err != nil {
		return nil, fmt.Errorf("gpui: NewAtlas: %w", err)
	}
	return &Atlas{svc: svc, handle: handle}, nil
}

// Dispose releases the atlas: every texture (COM refcounts) and tile
// record. Tile values previously returned stay comparable but their
// queries miss afterwards.
func (a *Atlas) Dispose() error {
	a.closeOnce.Do(func() {
		a.closeErr = a.svc.Dispose(a.handle)
	})
	return a.closeErr
}

// MaxSlots returns the native live-atlas slot bound (8).
func (a *Atlas) MaxSlots() int { return a.svc.MaxAtlasHandles() }

// DefaultAtlasSize is the pinned default texture edge (1024).
func (a *Atlas) DefaultAtlasSize() int { return a.svc.DefaultAtlasSize() }

// MaxAtlasSize is the D3D11 maximum texture edge (16384).
func (a *Atlas) MaxAtlasSize() int { return a.svc.MaxAtlasSize() }

// atlasKey builds the native key record from the glyph params and the
// raster format (the pinned AtlasKey::Glyph { params, format }).
func atlasKey(params RasterGlyphParams, format RasterFormat) native.AtlasKeyRecord {
	key := native.NewAtlasKeyRecord()
	key.FontID = uint64(params.FontID)
	key.GlyphID = params.GlyphID
	key.FontSizeBits = math.Float32bits(params.FontSize)
	key.SubpixelX = uint32(params.SubpixelX)
	key.SubpixelY = uint32(params.SubpixelY)
	key.ScaleBits = math.Float32bits(params.ScaleFactor)
	key.Style = native.NewRasterStyleRecord()
	key.Style.Mode = uint32(params.RasterStyle.Mode)
	key.Style.EffectTag = uint32(params.RasterStyle.Effect)
	key.Style.ColorR = uint32(params.RasterStyle.PreblendColor.R)
	key.Style.ColorG = uint32(params.RasterStyle.PreblendColor.G)
	key.Style.ColorB = uint32(params.RasterStyle.PreblendColor.B)
	key.Style.ColorA = uint32(params.RasterStyle.PreblendColor.A)
	key.Format = uint32(format)
	return key
}

// InsertGlyph uploads a finished raster and returns its tile. A cached
// key returns the existing tile with no upload and no new allocation
// (the pin's map hit). Empty rasters (zero bounds) are never inserted —
// the paint path returns before calling this, exactly as the pin's
// build closure does.
func (a *Atlas) InsertGlyph(raster *RasterizedGlyph, params RasterGlyphParams) (AtlasTile, error) {
	if raster == nil {
		return AtlasTile{}, fmt.Errorf("gpui: InsertGlyph: nil raster")
	}
	key := atlasKey(params, raster.Format)
	tile, err := a.svc.Insert(a.handle, key, raster.Width, raster.Height, raster.Pixels)
	if err != nil {
		return AtlasTile{}, fmt.Errorf("gpui: InsertGlyph: %w", err)
	}
	return AtlasTile{
		TextureIndex: tile.TextureIndex,
		TextureKind:  AtlasTextureKind(tile.TextureKind),
		TileID:       tile.TileID,
		Padding:      tile.Padding,
		BoundsX:      tile.BoundsX,
		BoundsY:      tile.BoundsY,
		BoundsW:      tile.BoundsW,
		BoundsH:      tile.BoundsH,
		Generation:   tile.Generation,
	}, nil
}

// RemoveGlyph removes the key's tile, deallocating its atlas space (the
// pin's PlatformAtlas::remove). Cache release and GPU completion are
// separate prerequisites; callers release only after both retire.
func (a *Atlas) RemoveGlyph(params RasterGlyphParams, format RasterFormat) error {
	key := atlasKey(params, format)
	if err := a.svc.Remove(a.handle, key); err != nil {
		return fmt.Errorf("gpui: RemoveGlyph: %w", err)
	}
	return nil
}

// GlyphTile returns the live tile of a key (false when the key has no
// live tile — the pin exposes contains under test support; the port
// uses the typed query).
func (a *Atlas) GlyphTile(params RasterGlyphParams, format RasterFormat) (AtlasTile, bool, error) {
	key := atlasKey(params, format)
	tile, err := a.svc.Query(a.handle, key)
	if err != nil {
		if isNativeError(err, native.ErrAtlasNotFound) {
			return AtlasTile{}, false, nil
		}
		return AtlasTile{}, false, fmt.Errorf("gpui: GlyphTile: %w", err)
	}
	return AtlasTile{
		TextureIndex: tile.TextureIndex,
		TextureKind:  AtlasTextureKind(tile.TextureKind),
		TileID:       tile.TileID,
		Padding:      tile.Padding,
		BoundsX:      tile.BoundsX,
		BoundsY:      tile.BoundsY,
		BoundsW:      tile.BoundsW,
		BoundsH:      tile.BoundsH,
		Generation:   tile.Generation,
	}, true, nil
}

// TextureCount returns the live texture count of one kind pool.
func (a *Atlas) TextureCount(kind AtlasTextureKind) (int, error) {
	count, err := a.svc.TextureCount(a.handle, native.AtlasTextureKind(kind))
	if err != nil {
		return 0, fmt.Errorf("gpui: TextureCount: %w", err)
	}
	return count, nil
}

// TileCount returns the number of live tile keys.
func (a *Atlas) TileCount() (int, error) {
	count, err := a.svc.TileCount(a.handle)
	if err != nil {
		return 0, fmt.Errorf("gpui: TileCount: %w", err)
	}
	return count, nil
}

// Generation returns the atlas generation (bumped on device loss).
func (a *Atlas) Generation() (uint32, error) {
	generation, err := a.svc.Generation(a.handle)
	if err != nil {
		return 0, fmt.Errorf("gpui: Generation: %w", err)
	}
	return generation, nil
}

// NotifyDeviceLost clears every pool and tile and bumps the generation
// (the pin's handle_device_lost: the renderer contract's "device
// generation changes invalidate all physical tiles, even if a logical
// font/image resource remains alive").
func (a *Atlas) NotifyDeviceLost() error {
	if err := a.svc.NotifyDeviceLost(a.handle); err != nil {
		return fmt.Errorf("gpui: NotifyDeviceLost: %w", err)
	}
	return nil
}
