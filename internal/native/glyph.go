package native

// Glyph raster service access (ticket10, reserved slot 5).
//
// This file is the Go mirror of the native glyph service documented in
// reference/native/GLYPH_ABI.md and implemented in
// reference/native/src/glyph.rs: the pinned Windows DirectWrite
// rasterizer ported from crates/gpui_windows/src/font_rasterizer.rs
// (254b5dbd47cbb5acbcc5bbdcbb322a339276c88a), running inside the shared
// Parley/Fontique text stack the text service (slot 4) owns. The service
// table lives in reserved slot 5 of the bootstrap ABI table and is
// advertised with capability bit 5 ("glyph-raster-dwrite").
//
// The service exposes the pinned raster surface through the public
// TextSystem::rasterize_glyph call the window paint path makes:
// AlphaMask (one coverage byte), BgraSubpixelMask (ClearType LCD
// coverage in BGRA) and BgraColor (straight-alpha color pixels) with
// the typed unsupported-color routing (bitmap/COLRv1/legacy-variable
// glyphs fall back to the retained Swash rasterizer; other raster
// failures are errors). Font ids are the text service's canonical ids.
//
// Every f32 crosses the ABI as IEEE-754 bits; decoded value types
// resolve them. Records are plain fixed-width Go structs whose layout
// is asserted against the native self-check fields at service-fetch
// time (validateGlyphTable). No Go pointer is retained across calls;
// the pixel dump buffer is borrowed for one call only.

import (
	"errors"
	"fmt"
	"math"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Status codes and errors
// ---------------------------------------------------------------------------

// Native glyph service status codes (mirror glyph_status in
// reference/native/src/glyph.rs). 0 is success; negative values are
// caller/argument errors; 100+ are internal failures.
const (
	glyphStatusOK      int32 = 0
	glyphErrBadHandle  int32 = -2
	glyphErrNullArg    int32 = -3
	glyphErrBadValue   int32 = -4
	glyphErrRaster     int32 = -5
	glyphErrCapacity   int32 = -7
	glyphErrGlyphPanic int32 = 101
)

// glyphServiceVersion mirrors GPUI_GO_GLYPH_SERVICE_VERSION.
const glyphServiceVersion uint32 = 1

func glyphStatusName(code int32) string {
	switch code {
	case glyphStatusOK:
		return "ok"
	case glyphErrBadHandle:
		return "bad handle (font id not canonical/registered)"
	case glyphErrNullArg:
		return "null argument"
	case glyphErrBadValue:
		return "bad value (mode/effect enumerant, non-finite float, bound or record size)"
	case glyphErrRaster:
		return "rasterization failed (font missing, DirectWrite/Swash failure, oversized buffer)"
	case glyphErrCapacity:
		return "pixel dump capacity too small"
	case glyphErrGlyphPanic:
		return "native panic contained"
	default:
		return "unknown glyph status"
	}
}

// Glyph service sentinel errors, distinguishable with errors.Is.
var (
	// ErrGlyphBadHandle: the font id is not canonical or not registered
	// with the text service.
	ErrGlyphBadHandle = errors.New("native: glyph font id is invalid")
	// ErrGlyphNullArg: a required pointer argument was null.
	ErrGlyphNullArg = errors.New("native: glyph argument was null")
	// ErrGlyphBadValue: a request failed validation (mode/effect
	// enumerant, non-finite float, bound, record size).
	ErrGlyphBadValue = errors.New("native: glyph value rejected")
	// ErrGlyphRaster: the pinned rasterization returned an error.
	ErrGlyphRaster = errors.New("native: glyph rasterization failed")
	// ErrGlyphCapacity: the pixel dump capacity was too small; retry with
	// the reported need.
	ErrGlyphCapacity = errors.New("native: glyph pixel dump capacity too small")
	// ErrGlyphPanic: a native panic was contained by catch_unwind.
	ErrGlyphPanic = errors.New("native: glyph panic contained")
)

// GlyphStatusError reports a non-zero glyph service status code.
type GlyphStatusError struct {
	Code   int32
	Name   string
	Detail string
}

func (e *GlyphStatusError) Error() string {
	detail := e.Detail
	if detail != "" {
		detail = ": " + detail
	}
	return ErrBadStatus.Error() + ": glyph status " + fmt.Sprint(e.Code) + " (" + e.Name + ")" + detail
}

func (e *GlyphStatusError) Unwrap() error {
	switch e.Code {
	case glyphStatusOK:
		return nil
	case glyphErrBadHandle:
		return ErrGlyphBadHandle
	case glyphErrNullArg:
		return ErrGlyphNullArg
	case glyphErrBadValue:
		return ErrGlyphBadValue
	case glyphErrRaster:
		return ErrGlyphRaster
	case glyphErrCapacity:
		return ErrGlyphCapacity
	case glyphErrGlyphPanic:
		return ErrGlyphPanic
	default:
		return ErrBadStatus
	}
}

func glyphStatusError(code int32, detail string) *GlyphStatusError {
	return &GlyphStatusError{Code: code, Name: glyphStatusName(code), Detail: detail}
}

// ---------------------------------------------------------------------------
// Enumerants and record mirrors
// ---------------------------------------------------------------------------

// GlyphRenderMode selects the raster output kind (native 0/1/2
// enumerant; the pinned GlyphRenderMode).
type GlyphRenderMode uint32

const (
	// GlyphModeGrayscale requests a one-channel coverage mask.
	GlyphModeGrayscale GlyphRenderMode = 0
	// GlyphModeSubpixel requests a three-channel LCD coverage mask.
	GlyphModeSubpixel GlyphRenderMode = 1
	// GlyphModeColor requests color pixels (COLR artwork or the tinted
	// monochrome currentColor path).
	GlyphModeColor GlyphRenderMode = 2
)

// RasterColorEffectKind tags the prepared color effect (native 0/1/2;
// the pinned RasterColorEffect). Dilation is a CoreGraphics-only effect
// this backend never produces.
type RasterColorEffectKind uint32

const (
	// RasterEffectIndependent: pixels do not depend on the scene color.
	RasterEffectIndependent RasterColorEffectKind = 0
	// RasterEffectPreblend: a quantized scene color baked into the
	// raster.
	RasterEffectPreblend RasterColorEffectKind = 1
)

// RasterFormat tags the rasterized byte layout (native 0/1/2; the
// pinned RasterizedGlyphFormat).
type RasterFormat uint32

const (
	// RasterFormatAlphaMask: one byte of coverage per pixel.
	RasterFormatAlphaMask RasterFormat = 0
	// RasterFormatBgraSubpixelMask: four bytes per pixel in blue, green,
	// red, unused order.
	RasterFormatBgraSubpixelMask RasterFormat = 1
	// RasterFormatBgraColor: four bytes per pixel in blue, green, red,
	// straight-alpha order.
	RasterFormatBgraColor RasterFormat = 2
)

// RenderingMode is the platform's recommended text rendering mode
// (native 0/1/2; the pinned TextRenderingMode).
type RenderingMode uint32

const (
	// RenderingModePlatformDefault defers to the platform.
	RenderingModePlatformDefault RenderingMode = 0
	// RenderingModeSubpixel is ClearType-style rendering.
	RenderingModeSubpixel RenderingMode = 1
	// RenderingModeGrayscale is plain antialiased rendering.
	RenderingModeGrayscale RenderingMode = 2
)

// RasterBackend is the rasterizer backend tag (native 0/1).
type RasterBackend uint32

const (
	// RasterBackendDirectWrite: DirectWrite with the Swash per-glyph
	// fallback.
	RasterBackendDirectWrite RasterBackend = 0
	// RasterBackendSwash: Swash only (DirectWrite failed to construct).
	RasterBackendSwash RasterBackend = 1
)

// RasterStyleRecord mirrors GpuiGoRasterStyleRecord (36 bytes, alignment
// 4): mode @0, effect tag @4, preblend color bytes @8..24, dilation @24
// (must be 0), reserved @28, record_size @32.
type RasterStyleRecord struct {
	Mode       uint32
	EffectTag  uint32
	ColorR     uint32
	ColorG     uint32
	ColorB     uint32
	ColorA     uint32
	Dilation   uint32
	Reserved   uint32
	RecordSize uint32
}

// NewRasterStyleRecord returns the zeroed record with its self-check.
func NewRasterStyleRecord() RasterStyleRecord {
	return RasterStyleRecord{RecordSize: uint32(unsafe.Sizeof(RasterStyleRecord{}))}
}

// RasterRecord mirrors GpuiGoRasterRecord (36 bytes, alignment 4):
// format @0, bounds x/y/w/h @4..20, buffer w/h @20..28, pixel_count
// @28, record_size @32.
type RasterRecord struct {
	Format     uint32
	BoundsX    int32
	BoundsY    int32
	BoundsW    int32
	BoundsH    int32
	Width      int32
	Height     int32
	PixelCount uint32
	RecordSize uint32
}

// RasterBackendRecord mirrors GpuiGoRasterBackendRecord (24 bytes,
// alignment 4).
type RasterBackendRecord struct {
	Backend         uint32
	VariableFactory uint32
	GammaBits       uint32
	ContrastBits    uint32
	SystemSubpixel  uint32
	RecordSize      uint32
}

// ---------------------------------------------------------------------------
// Service table mirror and validation
// ---------------------------------------------------------------------------

// glyphTable mirrors the native GpuiGoGlyphTable: 6 function pointers
// then 9 self-check scalars; 120 bytes (116 bytes of fields rounded to
// the 8-byte alignment), alignment 8.
type glyphTable struct {
	prepareStyle     uintptr
	glyphForChar     uintptr
	rasterize        uintptr
	recommendedMode  uintptr
	backendInfo      uintptr
	panicProbe       uintptr
	serviceVersion   uint32
	sizeOfTable      uint32
	alignOfTable     uint32
	sizeOfStyleRec   uint32
	sizeOfRasterRec  uint32
	sizeOfBackendRec uint32
	subpixelX        uint32
	subpixelY        uint32
	maxRasterBytes   uint32
}

func glyphTableFromSlot(slot uintptr) *glyphTable {
	return (*glyphTable)(*(*unsafe.Pointer)(unsafe.Pointer(&slot)))
}

func validateGlyphTable(t *glyphTable, path string) error {
	abi := func(field, expected, actual string) error {
		return &ABIError{Path: path, Field: field, Expected: expected, Actual: actual}
	}
	if t.serviceVersion != glyphServiceVersion {
		return abi("glyph service_version", fmt.Sprint(glyphServiceVersion), fmt.Sprint(t.serviceVersion))
	}
	if t.sizeOfTable != uint32(unsafe.Sizeof(glyphTable{})) {
		return abi("glyph size_of_table", fmt.Sprint(unsafe.Sizeof(glyphTable{})), fmt.Sprint(t.sizeOfTable))
	}
	if t.alignOfTable != uint32(unsafe.Alignof(glyphTable{})) {
		return abi("glyph align_of_table", fmt.Sprint(unsafe.Alignof(glyphTable{})), fmt.Sprint(t.alignOfTable))
	}
	if t.sizeOfStyleRec != uint32(unsafe.Sizeof(RasterStyleRecord{})) {
		return abi("glyph size_of_style_record", fmt.Sprint(unsafe.Sizeof(RasterStyleRecord{})), fmt.Sprint(t.sizeOfStyleRec))
	}
	if t.sizeOfRasterRec != uint32(unsafe.Sizeof(RasterRecord{})) {
		return abi("glyph size_of_raster_record", fmt.Sprint(unsafe.Sizeof(RasterRecord{})), fmt.Sprint(t.sizeOfRasterRec))
	}
	if t.sizeOfBackendRec != uint32(unsafe.Sizeof(RasterBackendRecord{})) {
		return abi("glyph size_of_backend_record", fmt.Sprint(unsafe.Sizeof(RasterBackendRecord{})), fmt.Sprint(t.sizeOfBackendRec))
	}
	slots := [...]uintptr{
		t.prepareStyle, t.glyphForChar, t.rasterize, t.recommendedMode,
		t.backendInfo, t.panicProbe,
	}
	names := [...]string{
		"glyph_prepare_style", "glyph_for_char", "glyph_rasterize",
		"glyph_recommended_mode", "glyph_backend_info", "glyph_panic_probe",
	}
	for i, slot := range slots {
		if slot == 0 {
			return &CapabilityError{Path: path, Detail: "glyph table slot " + names[i] + " is null"}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Service and typed calls
// ---------------------------------------------------------------------------

// GlyphService is the typed accessor for the native glyph raster
// service of a loaded Library. It holds a Go-owned copy of the validated
// service table. It is safe for concurrent use: the native service
// serializes every entry behind the text service's state mutex.
type GlyphService struct {
	lib   *Library
	table glyphTable
}

// Glyph returns the glyph raster service, validating its table first.
func (l *Library) Glyph() (*GlyphService, error) {
	if l.released.Load() {
		return nil, ErrClosed
	}
	if l.identity.Capabilities&capGlyphRaster == 0 {
		return nil, &CapabilityError{
			Path:     l.identity.Path,
			Required: capGlyphRaster,
			Actual:   l.identity.Capabilities,
			Detail:   "glyph-raster-dwrite capability bit missing",
		}
	}
	slot := l.table.reserved[5]
	if slot == 0 {
		return nil, &CapabilityError{Path: l.identity.Path, Detail: "glyph service table (reserved slot 5) is null"}
	}
	table := *glyphTableFromSlot(slot) // copy out of DLL memory
	if err := validateGlyphTable(&table, l.identity.Path); err != nil {
		return nil, err
	}
	return &GlyphService{lib: l, table: table}, nil
}

// MaxRasterBytes is the native raster byte-count bound.
func (s *GlyphService) MaxRasterBytes() int { return int(s.table.maxRasterBytes) }

// SubpixelVariants returns the (x, y) subpixel variant counts (4, 1).
func (s *GlyphService) SubpixelVariants() (x, y int) {
	return int(s.table.subpixelX), int(s.table.subpixelY)
}

// PrepareRasterStyle normalizes a scene color and render mode into the
// cache-relevant raster settings (the pinned
// PlatformTextSystem::prepare_raster_style: color mode quantizes the
// scene color into a Preblend effect; other modes stay independent).
func (s *GlyphService) PrepareRasterStyle(colorR, colorG, colorB, colorA float32, mode GlyphRenderMode) (RasterStyleRecord, error) {
	out := RasterStyleRecord{}
	if err := checkFinite32(colorR, colorG, colorB, colorA); err != nil {
		return out, glyphStatusError(glyphErrBadValue, err.Error())
	}
	if mode > GlyphModeColor {
		return out, glyphStatusError(glyphErrBadValue, "mode")
	}
	code, err := callGlyphPrepareStyle(
		s.table.prepareStyle,
		math.Float32bits(colorR),
		math.Float32bits(colorG),
		math.Float32bits(colorB),
		math.Float32bits(colorA),
		uint32(mode),
		&out,
	)
	if err != nil {
		return RasterStyleRecord{}, err
	}
	if code != glyphStatusOK {
		return RasterStyleRecord{}, glyphStatusError(code, "prepare_style")
	}
	return out, nil
}

// GlyphForChar maps a character to its nominal glyph id in the resolved
// face. The second result is false when the pinned lookup returns None
// (the missing-glyph outcome).
func (s *GlyphService) GlyphForChar(fontID FontID, ch rune) (uint32, bool, error) {
	if uint32(ch) > 0x10FFFF || (uint32(ch) >= 0xD800 && uint32(ch) <= 0xDFFF) {
		return 0, false, glyphStatusError(glyphErrBadValue, "char code")
	}
	var glyphID uint32
	code, err := callGlyphForChar(s.table.glyphForChar, uint64(fontID), uint32(ch), &glyphID)
	if err != nil {
		return 0, false, err
	}
	if code != glyphStatusOK {
		return 0, false, glyphStatusError(code, "glyph_for_char")
	}
	return glyphID, glyphID != math.MaxUint32, nil
}

// RasterizeGlyph rasters one glyph with the pinned parameters and
// returns the metadata record plus the pixel bytes (the bulk-dump
// capacity protocol retries internally, so the caller always receives
// the full pixel buffer on success).
func (s *GlyphService) RasterizeGlyph(params RasterParams) (*RasterRecord, []byte, error) {
	if params.FontSize < 0 || !isFiniteFloat32(params.FontSize) {
		return nil, nil, glyphStatusError(glyphErrBadValue, "font size")
	}
	if params.Scale <= 0 || !isFiniteFloat32(params.Scale) {
		return nil, nil, glyphStatusError(glyphErrBadValue, "scale")
	}
	if params.SubpixelX >= uint32(s.table.subpixelX) || params.SubpixelY >= uint32(s.table.subpixelY) {
		return nil, nil, glyphStatusError(glyphErrBadValue, "subpixel variant")
	}
	style := params.Style
	if style.RecordSize != uint32(unsafe.Sizeof(RasterStyleRecord{})) {
		return nil, nil, glyphStatusError(glyphErrBadValue, "style record size")
	}

	out := RasterRecord{}
	var pixels []byte
	needed := uint32(0)
	// First call with a null buffer: learns the byte count (the metadata
	// record is written either way); second call with the exact buffer.
	code, err := callGlyphRasterize(
		s.table.rasterize,
		uint64(params.FontID),
		params.GlyphID,
		math.Float32bits(params.FontSize),
		params.SubpixelX,
		params.SubpixelY,
		math.Float32bits(params.Scale),
		&style,
		&out,
		nil,
		0,
		&needed,
	)
	if err != nil {
		return nil, nil, err
	}
	if code == glyphErrCapacity {
		if needed > s.table.maxRasterBytes {
			return nil, nil, glyphStatusError(glyphErrRaster, "raster exceeds the byte bound")
		}
		if needed == 0 {
			// A zero-byte raster: the record already carries it.
			rec := out
			return &rec, nil, nil
		}
		pixels = make([]byte, needed)
		code, err = callGlyphRasterize(
			s.table.rasterize,
			uint64(params.FontID),
			params.GlyphID,
			math.Float32bits(params.FontSize),
			params.SubpixelX,
			params.SubpixelY,
			math.Float32bits(params.Scale),
			&style,
			&out,
			pixels,
			needed,
			&needed,
		)
		if err != nil {
			return nil, nil, err
		}
	}
	if code != glyphStatusOK {
		return nil, nil, glyphStatusError(code, "rasterize")
	}
	rec := out
	return &rec, pixels, nil
}

// RecommendedRenderingMode returns the platform's recommended mode for
// ordinary text (never PlatformDefault per the pin's contract).
func (s *GlyphService) RecommendedRenderingMode() (RenderingMode, error) {
	var mode uint32
	code, err := callGlyphRecommendedMode(s.table.recommendedMode, &mode)
	if err != nil {
		return 0, err
	}
	if code != glyphStatusOK {
		return 0, glyphStatusError(code, "recommended_mode")
	}
	return RenderingMode(mode), nil
}

// BackendInfo returns the rasterizer's construction facts (backend,
// variable-axes support, gamma, enhanced contrast, the OS subpixel
// setting).
func (s *GlyphService) BackendInfo() (RasterBackendRecord, error) {
	out := RasterBackendRecord{}
	code, err := callGlyphBackendInfo(s.table.backendInfo, &out)
	if err != nil {
		return RasterBackendRecord{}, err
	}
	if code != glyphStatusOK {
		return RasterBackendRecord{}, glyphStatusError(code, "backend_info")
	}
	return out, nil
}

// PanicProbe exercises the native panic containment boundary (test-only).
func (s *GlyphService) PanicProbe() error {
	code, err := callGlyphPanicProbe(s.table.panicProbe)
	if err != nil {
		return err
	}
	if code == glyphErrGlyphPanic {
		return nil
	}
	return glyphStatusError(code, "panic_probe")
}

// RasterParams are the pinned RenderGlyphParams: the font id, glyph id,
// font size, subpixel variant, scale factor and the prepared raster
// style.
type RasterParams struct {
	FontID    FontID
	GlyphID   uint32
	FontSize  float32
	SubpixelX uint32
	SubpixelY uint32
	Scale     float32
	Style     RasterStyleRecord
}

// isFiniteFloat32 reports whether the f32 is finite (no NaN/inf bits).
func isFiniteFloat32(v float32) bool {
	return !math.IsInf(float64(v), 0) && !math.IsNaN(float64(v))
}

// checkFinite32 reports the first non-finite argument.
func checkFinite32(values ...float32) error {
	for i, v := range values {
		if !isFiniteFloat32(v) {
			return fmt.Errorf("argument %d is not finite", i)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Raw call shims: see glyph_windows.go (syscall.SyscallN) and
// glyph_other.go (non-Windows stubs).
// ---------------------------------------------------------------------------
