// SVG service access (ticket19, reserved slot 8).
//
// The pinned resvg/usvg 0.48.1 stack behind the CE SvgRenderer public
// paths (reference/ce-source/crates/gpui/src/svg_renderer.rs): parse
// with the system+bundled font resolution and the emoji fallback, the
// three sizing modes (Size aspect-preserving, ExactSize, ScaleFactor
// with the smooth 2x factor), the 8192-pixel clamp, the
// premultiplied-RGBA→BGRA output, and the alpha-mask entry — plus the
// owned font-asset surface (the pinned bundled-font paths, pushed in
// by the Go side, with the missing set queryable: the NeedsFontAssets
// seam of the runtime ownership contract). The table mirror carries
// self-check fields validated at fetch (validateSvgTable). No Go
// pointer is retained across calls: the encoded bytes are borrowed for
// the parse, and parsed trees live behind generation-stamped handles
// until Dispose.

package native

import (
	"errors"
	"fmt"
	"unsafe"
)

// svgServiceVersion mirrors GPUI_GO_SVG_SERVICE_VERSION.
const svgServiceVersion uint32 = 1

// SVG render-mode tags (mirror svg_mode in svg.rs).
const (
	// SvgModeSize renders at a width in device pixels, aspect
	// preserved (SvgSize::Size).
	SvgModeSize uint32 = 1
	// SvgModeExactSize renders at exact device dimensions
	// (SvgSize::ExactSize).
	SvgModeExactSize uint32 = 2
	// SvgModeScaleFactor renders at a logical scaling factor; the
	// native side applies the smooth 2x factor and reports it back
	// (SvgSize::ScaleFactor).
	SvgModeScaleFactor uint32 = 3
)

// SVG service status codes (mirror svg_status in svg.rs).
const (
	svgStatusOK          int32 = 0
	svgStatusStaleHandle int32 = -1
	svgStatusBadHandle   int32 = -2
	svgStatusNullArg     int32 = -3
	svgStatusBadValue    int32 = -4
	svgStatusParse       int32 = -5
	svgStatusCapacity    int32 = -6
	svgStatusHandleLimit int32 = -7
	svgStatusPanic       int32 = 101
)

// SvgRenderInfo mirrors GpuiGoSvgRenderInfo: the rendered pixel
// dimensions and the applied scale factor; size 12, alignment 4.
type SvgRenderInfo struct {
	Width       uint32
	Height      uint32
	ScaleFactor float32
}

// svgNativeTable mirrors GpuiGoSvgTable: 8 function pointers then 11
// self-check/capacity/mode-tag scalars; size 112, alignment 8.
type svgNativeTable struct {
	fontAssetCount  uintptr
	fontAssetPath   uintptr
	hasFontAsset    uintptr
	addFont         uintptr
	parse           uintptr
	dispose         uintptr
	render          uintptr
	renderAlphaMask uintptr

	serviceVersion    uint32
	sizeOfTable       uint32
	alignOfTable      uint32
	sizeOfRenderInfo  uint32
	maxSvgHandles     uint32
	maxSvgBytes       uint32
	smoothScaleFactor float32
	fontAssetPaths    uint32
	modeSize          uint32
	modeExactSize     uint32
	modeScaleFactor   uint32
}

// svgTableFromSlot reads the service table out of DLL memory (the
// bootstrap table's reserved slot 8).
func svgTableFromSlot(slot uintptr) *svgNativeTable {
	return (*svgNativeTable)(*(*unsafe.Pointer)(unsafe.Pointer(&slot)))
}

// validateSvgTable asserts the native self-check fields against this
// mirror's compile-time layout (a mismatch fails the fetch instead of
// silently misreading a field).
func validateSvgTable(table *svgNativeTable, path string) error {
	if table.serviceVersion != svgServiceVersion {
		return fmt.Errorf("native: svg service version = %d, want %d (%s)", table.serviceVersion, svgServiceVersion, path)
	}
	if table.sizeOfTable != uint32(unsafe.Sizeof(svgNativeTable{})) {
		return fmt.Errorf("native: svg table size = %d, want %d (%s)", table.sizeOfTable, unsafe.Sizeof(svgNativeTable{}), path)
	}
	if table.alignOfTable != uint32(unsafe.Alignof(svgNativeTable{})) {
		return fmt.Errorf("native: svg table alignment = %d, want %d (%s)", table.alignOfTable, unsafe.Alignof(svgNativeTable{}), path)
	}
	if table.sizeOfRenderInfo != uint32(unsafe.Sizeof(SvgRenderInfo{})) {
		return fmt.Errorf("native: svg render-info size = %d, want %d", table.sizeOfRenderInfo, unsafe.Sizeof(SvgRenderInfo{}))
	}
	if table.modeSize != SvgModeSize || table.modeExactSize != SvgModeExactSize || table.modeScaleFactor != SvgModeScaleFactor {
		return fmt.Errorf("native: svg mode tags (%d,%d,%d) do not match the mirror", table.modeSize, table.modeExactSize, table.modeScaleFactor)
	}
	if table.fontAssetPaths != 2 {
		return fmt.Errorf("native: svg font-asset path count = %d, want 2 (the pinned bundled list)", table.fontAssetPaths)
	}
	return nil
}

// SvgService is the SVG service client (reserved slot 8).
type SvgService struct {
	lib   *Library
	table svgNativeTable
}

// Svg fetches the SVG service (capability bit 9, reserved slot 8).
func (l *Library) Svg() (*SvgService, error) {
	if l.released.Load() {
		return nil, ErrClosed
	}
	if l.identity.Capabilities&capSvgResvg == 0 {
		return nil, &CapabilityError{
			Path:     l.identity.Path,
			Required: capSvgResvg,
			Actual:   l.identity.Capabilities,
			Detail:   "svg-resvg-0-48 capability bit missing",
		}
	}
	slot := l.table.reserved[8]
	if slot == 0 {
		return nil, &CapabilityError{Path: l.identity.Path, Detail: "svg service table (reserved slot 8) is null"}
	}
	table := *svgTableFromSlot(slot) // copy out of DLL memory
	if err := validateSvgTable(&table, l.identity.Path); err != nil {
		return nil, err
	}
	return &SvgService{lib: l, table: table}, nil
}

// SvgHandle is a validated opaque native parsed-tree handle. It
// becomes stale after Dispose (or native slot reuse).
type SvgHandle uint64

// The SVG service errors.
var (
	// ErrSvgStaleHandle reports a handle that encoded a past
	// generation (disposed or slot reused since).
	ErrSvgStaleHandle = errors.New("native: svg handle is stale (disposed or slot reused)")
	// ErrSvgBadHandle reports a malformed or never-issued handle.
	ErrSvgBadHandle = errors.New("native: svg handle is malformed")
	// ErrSvgBadValue reports an invalid length, capacity, mode tag or
	// font-asset argument (including a duplicate font-asset path).
	ErrSvgBadValue = errors.New("native: svg argument rejected")
	// ErrSvgParse reports the pinned usvg parse/rasterize failure.
	ErrSvgParse = errors.New("native: svg parse or rasterize failed")
	// ErrSvgHandleLimit reports the live-handle capacity exhaustion.
	ErrSvgHandleLimit = errors.New("native: svg handle limit reached")
	// ErrSvgPanic reports a contained native panic.
	ErrSvgPanic = errors.New("native: svg service panicked")
)

// svgError maps a native status code to an error.
func svgError(code int32) error {
	switch code {
	case svgStatusStaleHandle:
		return ErrSvgStaleHandle
	case svgStatusBadHandle:
		return ErrSvgBadHandle
	case svgStatusBadValue:
		return ErrSvgBadValue
	case svgStatusParse:
		return ErrSvgParse
	case svgStatusHandleLimit:
		return ErrSvgHandleLimit
	case svgStatusPanic:
		return ErrSvgPanic
	default:
		return fmt.Errorf("native: svg service status %d", code)
	}
}

// SvgFontAssetPath returns the pinned bundled-font asset path at
// index (the renderer's resolver consults exactly these paths; the
// count is SvgFontAssetCount). The string is a copy of the native
// static.
func (s *SvgService) SvgFontAssetPath(index uint32) (string, error) {
	if s == nil {
		return "", fmt.Errorf("native: no svg service")
	}
	path, err := callSvgFontAssetPath(s.table.fontAssetPath, index)
	if err != nil {
		return "", err
	}
	return path, nil
}

// SvgFontAssetCount returns the pinned bundled-font path count (2:
// IBM Plex Sans Regular and Lilex Regular — svg_renderer.rs's
// load_bundled_fonts list).
func (s *SvgService) SvgFontAssetCount() (uint32, error) {
	if s == nil {
		return 0, fmt.Errorf("native: no svg service")
	}
	count, err := callSvgFontAssetCount(s.table.fontAssetCount)
	if err != nil {
		return 0, err
	}
	return uint32(count), nil
}

// HasFontAsset reports whether the pinned font-asset path was pushed
// (the missing set the Go side orchestrates loading for).
func (s *SvgService) HasFontAsset(path string) (bool, error) {
	if s == nil {
		return false, fmt.Errorf("native: no svg service")
	}
	has, err := callSvgHasFontAsset(s.table.hasFontAsset, path)
	if err != nil {
		return false, err
	}
	return has, nil
}

// AddFont pushes a loaded font-asset's bytes under its pinned path
// into the native registry. A duplicate path is ErrSvgBadValue (the
// registry's DuplicateAssetPath rule); the Go side owns the
// exactly-once loading orchestration.
func (s *SvgService) AddFont(path string, bytes []byte) error {
	if s == nil {
		return fmt.Errorf("native: no svg service")
	}
	code, err := callSvgAddFont(s.table.addFont, path, bytes)
	if err != nil {
		return err
	}
	if code != svgStatusOK {
		return svgError(code)
	}
	return nil
}

// Parse parses SVG bytes into a retained tree handle (fonts resolve
// against the system database plus every pushed asset; a parse after
// new fonts sees them — the port's font-snapshot lifetime).
func (s *SvgService) Parse(bytes []byte) (SvgHandle, error) {
	if s == nil {
		return 0, fmt.Errorf("native: no svg service")
	}
	handle, code, err := callSvgParse(s.table.parse, bytes)
	if err != nil {
		return 0, err
	}
	if code != svgStatusOK {
		return 0, svgError(code)
	}
	return handle, nil
}

// Dispose releases a parsed-tree handle.
func (s *SvgService) Dispose(handle SvgHandle) error {
	if s == nil {
		return fmt.Errorf("native: no svg service")
	}
	code, err := callSvgDispose(s.table.dispose, handle)
	if err != nil {
		return err
	}
	if code != svgStatusOK {
		return svgError(code)
	}
	return nil
}

// Render rasterizes a parsed tree at the requested size (mode per
// SvgModeSize/SvgModeExactSize/SvgModeScaleFactor) into a BGRA8
// buffer, returning the pixels with the rendered dimensions and the
// applied scale factor. The capacity protocol applies: a nil buffer
// first queries the required size (ErrSvgCapacity carries the needed
// count), then the caller re-renders with that capacity.
func (s *SvgService) Render(handle SvgHandle, mode uint32, width, height uint32, scale float32) (pixels []byte, info SvgRenderInfo, err error) {
	if s == nil {
		return nil, info, fmt.Errorf("native: no svg service")
	}
	return renderSvg(s.table.render, handle, mode, width, height, scale)
}

// RenderAlphaMask rasterizes SVG bytes at a width (aspect preserved)
// and returns the alpha channel (one byte per pixel, the
// pub(crate) render_alpha_mask adaptation) with the rendered
// dimensions.
func (s *SvgService) RenderAlphaMask(bytes []byte, width, height uint32) (mask []byte, info SvgRenderInfo, err error) {
	if s == nil {
		return nil, info, fmt.Errorf("native: no svg service")
	}
	return renderSvgAlphaMask(s.table.renderAlphaMask, bytes, width, height)
}

// SmoothScaleFactor returns the pinned smooth-scale factor the
// ScaleFactor mode applies (2.0).
func (s *SvgService) SmoothScaleFactor() float32 {
	if s == nil {
		return 0
	}
	return s.table.smoothScaleFactor
}
