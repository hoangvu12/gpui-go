package native

// Texture atlas service access (ticket10, reserved slot 6).
//
// This file is the Go mirror of the native atlas service documented in
// reference/native/ATLAS_ABI.md and implemented in
// reference/native/src/atlas.rs: the pinned D3D11 sprite atlas ported
// from crates/gpui_windows/src/directx_atlas.rs
// (254b5dbd47cbb5acbcc5bbdcbb322a339276c88a) — etagere 0.2.15 bucketed
// packing, the three monochrome (R8_UNORM) / polychrome and subpixel
// (B8G8R8A8_UNORM) texture pools, 1024x1024 default textures clamped to
// the D3D11 16384 limit, upload validation before allocation, tile
// removal with texture free-listing, and generation-invalidated tiles
// on device loss. The atlas is bound to the renderer service's shared
// device (reserved slot 2) exactly where the pin passes the renderer's
// device into DirectXAtlas::new.
//
// The service table lives in reserved slot 6 of the bootstrap ABI table
// and is advertised with capability bit 6 ("glyph-atlas-d3d11"). Records
// are plain fixed-width Go structs whose layout is asserted against the
// native self-check fields at service-fetch time
// (validateAtlasTable). No Go pointer is retained across calls: the
// upload byte buffer is borrowed for one call only.

import (
	"errors"
	"fmt"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Status codes and errors
// ---------------------------------------------------------------------------

// Native atlas service status codes (mirror atlas_status in
// reference/native/src/atlas.rs). 0 is success; negative values are
// caller/argument errors; 100+ are internal failures.
const (
	atlasStatusOK       int32 = 0
	atlasErrStaleHandle int32 = -1
	atlasErrBadHandle   int32 = -2
	atlasErrNullArg     int32 = -3
	atlasErrBadValue    int32 = -4
	atlasErrDevice      int32 = -5
	atlasErrNotFound    int32 = -8
	atlasErrAtlasPanic  int32 = 101
)

// atlasServiceVersion mirrors GPUI_GO_ATLAS_SERVICE_VERSION.
const atlasServiceVersion uint32 = 1

func atlasStatusName(code int32) string {
	switch code {
	case atlasStatusOK:
		return "ok"
	case atlasErrStaleHandle:
		return "stale handle (atlas disposed or slot reused)"
	case atlasErrBadHandle:
		return "bad handle (malformed, or the slot limit)"
	case atlasErrNullArg:
		return "null argument"
	case atlasErrBadValue:
		return "bad value (key record, dimensions, byte count, reserved fields)"
	case atlasErrDevice:
		return "device failure (texture or view creation failed)"
	case atlasErrNotFound:
		return "no live tile for the key"
	case atlasErrAtlasPanic:
		return "native panic contained"
	default:
		return "unknown atlas status"
	}
}

// Atlas service sentinel errors, distinguishable with errors.Is.
var (
	// ErrAtlasStaleHandle: the atlas handle encoded a past generation.
	ErrAtlasStaleHandle = errors.New("native: atlas handle is stale")
	// ErrAtlasBadHandle: the handle never existed or is malformed.
	ErrAtlasBadHandle = errors.New("native: atlas handle is invalid")
	// ErrAtlasNullArg: a required pointer argument was null.
	ErrAtlasNullArg = errors.New("native: atlas argument was null")
	// ErrAtlasBadValue: a request failed validation.
	ErrAtlasBadValue = errors.New("native: atlas value rejected")
	// ErrAtlasDevice: texture creation failed on the shared device.
	ErrAtlasDevice = errors.New("native: atlas device failure")
	// ErrAtlasNotFound: the queried key has no live tile.
	ErrAtlasNotFound = errors.New("native: atlas key not found")
	// ErrAtlasPanic: a native panic was contained by catch_unwind.
	ErrAtlasPanic = errors.New("native: atlas panic contained")
)

// AtlasStatusError reports a non-zero atlas service status code.
type AtlasStatusError struct {
	Code   int32
	Name   string
	Detail string
}

func (e *AtlasStatusError) Error() string {
	detail := e.Detail
	if detail != "" {
		detail = ": " + detail
	}
	return ErrBadStatus.Error() + ": atlas status " + fmt.Sprint(e.Code) + " (" + e.Name + ")" + detail
}

func (e *AtlasStatusError) Unwrap() error {
	switch e.Code {
	case atlasStatusOK:
		return nil
	case atlasErrStaleHandle:
		return ErrAtlasStaleHandle
	case atlasErrBadHandle:
		return ErrAtlasBadHandle
	case atlasErrNullArg:
		return ErrAtlasNullArg
	case atlasErrBadValue:
		return ErrAtlasBadValue
	case atlasErrDevice:
		return ErrAtlasDevice
	case atlasErrNotFound:
		return ErrAtlasNotFound
	case atlasErrAtlasPanic:
		return ErrAtlasPanic
	default:
		return ErrBadStatus
	}
}

func atlasStatusError(code int32, detail string) *AtlasStatusError {
	return &AtlasStatusError{Code: code, Name: atlasStatusName(code), Detail: detail}
}

// ---------------------------------------------------------------------------
// Enumerants and record mirrors
// ---------------------------------------------------------------------------

// AtlasTextureKind selects the texture pool (native 0/1/2 enumerant;
// the pinned AtlasTextureKind).
type AtlasTextureKind uint32

const (
	// AtlasKindMonochrome is the R8_UNORM coverage pool.
	AtlasKindMonochrome AtlasTextureKind = 0
	// AtlasKindPolychrome is the BGRA color pool.
	AtlasKindPolychrome AtlasTextureKind = 1
	// AtlasKindSubpixel is the BGRA LCD-coverage pool.
	AtlasKindSubpixel AtlasTextureKind = 2
)

// AtlasKeyKind tags the key variant (native 0 = glyph; svg/image are
// later tickets and rejected).
type AtlasKeyKind uint32

const (
	// AtlasKeyGlyph is the glyph key: raster params plus format.
	AtlasKeyGlyph AtlasKeyKind = 0
)

// AtlasKeyRecord mirrors GpuiGoAtlasKeyRecord (88 bytes, alignment 8):
// kind @0, font_id @8, glyph_id @16, font_size_bits @20, subpixel_x
// @24, subpixel_y @28, scale_bits @32, style @36 (the 36-byte style
// record), format @72, reserved @76, record_size @84.
type AtlasKeyRecord struct {
	Kind         uint32
	_            uint32
	FontID       uint64
	GlyphID      uint32
	FontSizeBits uint32
	SubpixelX    uint32
	SubpixelY    uint32
	ScaleBits    uint32
	Style        RasterStyleRecord
	Format       uint32
	Reserved     [2]uint32
	RecordSize   uint32
}

// NewAtlasKeyRecord returns the zeroed record with its self-check.
func NewAtlasKeyRecord() AtlasKeyRecord {
	return AtlasKeyRecord{RecordSize: uint32(unsafe.Sizeof(AtlasKeyRecord{}))}
}

// AtlasTileRecord mirrors GpuiGoAtlasTileRecord (40 bytes, alignment
// 4): texture_index @0, texture_kind @4, tile_id @8, padding @12,
// bounds x/y/w/h @16..32, generation @32, record_size @36.
type AtlasTileRecord struct {
	TextureIndex uint32
	TextureKind  uint32
	TileID       uint32
	Padding      uint32
	BoundsX      int32
	BoundsY      int32
	BoundsW      int32
	BoundsH      int32
	Generation   uint32
	RecordSize   uint32
}

// ---------------------------------------------------------------------------
// Service table mirror and validation
// ---------------------------------------------------------------------------

// atlasTable mirrors the native GpuiGoAtlasTable: 10 function pointers
// then 9 self-check scalars; 120 bytes (116 bytes of fields rounded to
// the 8-byte alignment), alignment 8.
type atlasTable struct {
	create         uintptr
	dispose        uintptr
	insert         uintptr
	remove         uintptr
	query          uintptr
	textureCount   uintptr
	tileCount      uintptr
	generation     uintptr
	notifyLost     uintptr
	panicProbe     uintptr
	serviceVersion uint32
	sizeOfTable    uint32
	alignOfTable   uint32
	sizeOfKeyRec   uint32
	alignOfKeyRec  uint32
	sizeOfTileRec  uint32
	defaultAtlas   uint32
	maxAtlas       uint32
	maxAtlasSlots  uint32
}

func atlasTableFromSlot(slot uintptr) *atlasTable {
	return (*atlasTable)(*(*unsafe.Pointer)(unsafe.Pointer(&slot)))
}

func validateAtlasTable(t *atlasTable, path string) error {
	abi := func(field, expected, actual string) error {
		return &ABIError{Path: path, Field: field, Expected: expected, Actual: actual}
	}
	if t.serviceVersion != atlasServiceVersion {
		return abi("atlas service_version", fmt.Sprint(atlasServiceVersion), fmt.Sprint(t.serviceVersion))
	}
	if t.sizeOfTable != uint32(unsafe.Sizeof(atlasTable{})) {
		return abi("atlas size_of_table", fmt.Sprint(unsafe.Sizeof(atlasTable{})), fmt.Sprint(t.sizeOfTable))
	}
	if t.alignOfTable != uint32(unsafe.Alignof(atlasTable{})) {
		return abi("atlas align_of_table", fmt.Sprint(unsafe.Alignof(atlasTable{})), fmt.Sprint(t.alignOfTable))
	}
	if t.sizeOfKeyRec != uint32(unsafe.Sizeof(AtlasKeyRecord{})) {
		return abi("atlas size_of_key_record", fmt.Sprint(unsafe.Sizeof(AtlasKeyRecord{})), fmt.Sprint(t.sizeOfKeyRec))
	}
	if t.alignOfKeyRec != uint32(unsafe.Alignof(AtlasKeyRecord{})) {
		return abi("atlas align_of_key_record", fmt.Sprint(unsafe.Alignof(AtlasKeyRecord{})), fmt.Sprint(t.alignOfKeyRec))
	}
	if t.sizeOfTileRec != uint32(unsafe.Sizeof(AtlasTileRecord{})) {
		return abi("atlas size_of_tile_record", fmt.Sprint(unsafe.Sizeof(AtlasTileRecord{})), fmt.Sprint(t.sizeOfTileRec))
	}
	slots := [...]uintptr{
		t.create, t.dispose, t.insert, t.remove, t.query,
		t.textureCount, t.tileCount, t.generation, t.notifyLost, t.panicProbe,
	}
	names := [...]string{
		"atlas_create", "atlas_dispose", "atlas_insert", "atlas_remove",
		"atlas_query", "atlas_texture_count", "atlas_tile_count",
		"atlas_generation", "atlas_notify_device_lost", "atlas_panic_probe",
	}
	for i, slot := range slots {
		if slot == 0 {
			return &CapabilityError{Path: path, Detail: "atlas table slot " + names[i] + " is null"}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Service and typed calls
// ---------------------------------------------------------------------------

// AtlasService is the typed accessor for the native atlas service of a
// loaded Library. It holds a Go-owned copy of the validated service
// table. It is safe for concurrent use: the native service serializes
// every entry behind its own state mutex.
type AtlasService struct {
	lib   *Library
	table atlasTable
}

// Atlas returns the texture atlas service, validating its table first.
func (l *Library) Atlas() (*AtlasService, error) {
	if l.released.Load() {
		return nil, ErrClosed
	}
	if l.identity.Capabilities&capAtlasD3D11 == 0 {
		return nil, &CapabilityError{
			Path:     l.identity.Path,
			Required: capAtlasD3D11,
			Actual:   l.identity.Capabilities,
			Detail:   "glyph-atlas-d3d11 capability bit missing",
		}
	}
	slot := l.table.reserved[6]
	if slot == 0 {
		return nil, &CapabilityError{Path: l.identity.Path, Detail: "atlas service table (reserved slot 6) is null"}
	}
	table := *atlasTableFromSlot(slot) // copy out of DLL memory
	if err := validateAtlasTable(&table, l.identity.Path); err != nil {
		return nil, err
	}
	return &AtlasService{lib: l, table: table}, nil
}

// AtlasHandle is a validated opaque native atlas handle. It becomes
// stale after Dispose (or native slot reuse).
type AtlasHandle uint64

// DefaultAtlasSize is the pinned default texture edge (1024).
func (s *AtlasService) DefaultAtlasSize() int { return int(s.table.defaultAtlas) }

// MaxAtlasSize is the D3D11 maximum texture edge (16384).
func (s *AtlasService) MaxAtlasSize() int { return int(s.table.maxAtlas) }

// MaxAtlasHandles is the native live-atlas bound.
func (s *AtlasService) MaxAtlasHandles() int { return int(s.table.maxAtlasSlots) }

// Create creates a new atlas bound to the renderer service's shared
// device.
func (s *AtlasService) Create() (AtlasHandle, error) {
	var handle uint64
	code, err := callAtlasCreate(s.table.create, &handle)
	if err != nil {
		return 0, err
	}
	if code != atlasStatusOK {
		return 0, atlasStatusError(code, "create")
	}
	return AtlasHandle(handle), nil
}

// Dispose releases the atlas: every texture (COM refcounts) and tile
// record. The handle is stale afterwards.
func (s *AtlasService) Dispose(handle AtlasHandle) error {
	code, err := callAtlasDispose(s.table.dispose, uint64(handle))
	if err != nil {
		return err
	}
	if code != atlasStatusOK {
		return atlasStatusError(code, "dispose")
	}
	return nil
}

// Insert validates and uploads one bitmap, returning its tile. A cached
// key returns the existing tile with no upload (the pinned
// get_or_insert_with map hit).
func (s *AtlasService) Insert(handle AtlasHandle, key AtlasKeyRecord, width, height int32, bytes []byte) (AtlasTileRecord, error) {
	tile := AtlasTileRecord{}
	if key.RecordSize != uint32(unsafe.Sizeof(AtlasKeyRecord{})) {
		return tile, atlasStatusError(atlasErrBadValue, "key record size")
	}
	if key.Kind != uint32(AtlasKeyGlyph) {
		return tile, atlasStatusError(atlasErrBadValue, "key kind")
	}
	if key.Reserved != [2]uint32{} {
		return tile, atlasStatusError(atlasErrBadValue, "reserved")
	}
	if width > int32(s.table.maxAtlas) || height > int32(s.table.maxAtlas) {
		return tile, atlasStatusError(atlasErrBadValue, "dimensions over the texture limit")
	}
	if len(bytes) > 0 && (width <= 0 || height <= 0) {
		return tile, atlasStatusError(atlasErrBadValue, "dimensions")
	}
	code, err := callAtlasInsert(
		s.table.insert,
		uint64(handle),
		&key,
		width,
		height,
		bytes,
		uint32(len(bytes)),
		&tile,
	)
	if err != nil {
		return tile, err
	}
	if code != atlasStatusOK {
		return tile, atlasStatusError(code, "insert")
	}
	return tile, nil
}

// Remove removes the key's tile, deallocating its atlas space.
func (s *AtlasService) Remove(handle AtlasHandle, key AtlasKeyRecord) error {
	if key.RecordSize != uint32(unsafe.Sizeof(AtlasKeyRecord{})) {
		return atlasStatusError(atlasErrBadValue, "key record size")
	}
	code, err := callAtlasRemove(s.table.remove, uint64(handle), &key)
	if err != nil {
		return err
	}
	if code != atlasStatusOK {
		return atlasStatusError(code, "remove")
	}
	return nil
}

// Query returns the live tile of a key, or ErrAtlasNotFound.
func (s *AtlasService) Query(handle AtlasHandle, key AtlasKeyRecord) (AtlasTileRecord, error) {
	tile := AtlasTileRecord{}
	if key.RecordSize != uint32(unsafe.Sizeof(AtlasKeyRecord{})) {
		return tile, atlasStatusError(atlasErrBadValue, "key record size")
	}
	code, err := callAtlasQuery(s.table.query, uint64(handle), &key, &tile)
	if err != nil {
		return tile, err
	}
	if code != atlasStatusOK {
		return tile, atlasStatusError(code, "query")
	}
	return tile, nil
}

// TextureCount returns the live texture count of one kind pool.
func (s *AtlasService) TextureCount(handle AtlasHandle, kind AtlasTextureKind) (int, error) {
	var count uint32
	code, err := callAtlasTextureCount(s.table.textureCount, uint64(handle), uint32(kind), &count)
	if err != nil {
		return 0, err
	}
	if code != atlasStatusOK {
		return 0, atlasStatusError(code, "texture_count")
	}
	return int(count), nil
}

// TileCount returns the number of live tile keys.
func (s *AtlasService) TileCount(handle AtlasHandle) (int, error) {
	var count uint32
	code, err := callAtlasTileCount(s.table.tileCount, uint64(handle), &count)
	if err != nil {
		return 0, err
	}
	if code != atlasStatusOK {
		return 0, atlasStatusError(code, "tile_count")
	}
	return int(count), nil
}

// Generation returns the atlas generation (bumped on device loss).
func (s *AtlasService) Generation(handle AtlasHandle) (uint32, error) {
	var generation uint32
	code, err := callAtlasGeneration(s.table.generation, uint64(handle), &generation)
	if err != nil {
		return 0, err
	}
	if code != atlasStatusOK {
		return 0, atlasStatusError(code, "generation")
	}
	return generation, nil
}

// NotifyDeviceLost clears every pool and tile and bumps the generation
// (the pinned handle_device_lost).
func (s *AtlasService) NotifyDeviceLost(handle AtlasHandle) error {
	code, err := callAtlasNotifyDeviceLost(s.table.notifyLost, uint64(handle))
	if err != nil {
		return err
	}
	if code != atlasStatusOK {
		return atlasStatusError(code, "notify_device_lost")
	}
	return nil
}

// PanicProbe exercises the native panic containment boundary (test-only).
func (s *AtlasService) PanicProbe() error {
	code, err := callAtlasPanicProbe(s.table.panicProbe)
	if err != nil {
		return err
	}
	if code == atlasErrAtlasPanic {
		return nil
	}
	return atlasStatusError(code, "panic_probe")
}
