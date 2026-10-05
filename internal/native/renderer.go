// Renderer service access (ticket07).
//
// This file is the Go mirror of the native renderer service implemented in
// reference/native/src/renderer.rs (D3D11 clear-frame present with
// D3D11_QUERY_EVENT retirement, following the pinned gpui-CE Windows
// renderer's device/swap-chain creation semantics). The service table
// lives in reserved slot 2 of the bootstrap ABI table and is advertised
// by capability bit 2 ("renderer-d3d11").
//
// The ABI's retirement protocol is the renderer contract's completion
// protocol: surface_present is acceptance (a tracked submission token
// plus, at most, a present error that still leaves the token valid);
// submission_poll is the only completion evidence (S_OK = completed,
// S_FALSE = pending); submission_retire is the exactly-once terminal
// record, allowed only after a completed poll or a confirmed device-loss
// abort. Present or Flush alone is never completion.
//
// Records are plain fixed-width Go structs whose layout is asserted
// against the native self-check fields at service-fetch time
// (validateRendererTable). No Go pointer ever crosses the boundary: the
// only caller-provided pointer arguments are value records and
// device-info string buffers, borrowed for the duration of one call.

package native

import (
	"errors"
	"fmt"
	"math"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Status codes and errors
// ---------------------------------------------------------------------------

// Native renderer service status codes (mirror renderer_status in
// reference/native/src/renderer.rs). 0 is success; negative values are
// caller/argument errors — with one documented exception: -9 is a tracked
// partial-submit outcome, not a caller mistake — and 100+ are internal
// failures.
const (
	rendererStatusOK            int32 = 0
	rendererErrStale            int32 = -1
	rendererErrBadHandle        int32 = -2
	rendererErrNullArg          int32 = -3
	rendererErrBadValue         int32 = -4
	rendererErrDevice           int32 = -5
	rendererErrBusy             int32 = -6
	rendererErrPending          int32 = -7
	rendererErrQuarantine       int32 = -8
	rendererErrPresent          int32 = -9
	rendererErrCapacity         int32 = -10
	rendererErrUnsupportedScene int32 = -11
	rendererErrScene            int32 = -12
	rendererErrPanic            int32 = 101
)

// rendererServiceVersion mirrors GPUI_GO_RENDERER_SERVICE_VERSION.
// Version 2 (ticket16) added the scene drawing entries and the draw
// scalars.
const rendererServiceVersion uint32 = 2

func rendererStatusName(code int32) string {
	switch code {
	case rendererStatusOK:
		return "ok"
	case rendererErrStale:
		return "stale handle"
	case rendererErrBadHandle:
		return "bad handle"
	case rendererErrNullArg:
		return "null argument"
	case rendererErrBadValue:
		return "bad value (invalid mode/extent/record)"
	case rendererErrDevice:
		return "device failure (no compatible D3D11 adapter or device error)"
	case rendererErrBusy:
		return "pending-submission cap exceeded (bounded in-flight backpressure)"
	case rendererErrPending:
		return "un-retired submissions remain"
	case rendererErrQuarantine:
		return "quarantined (failed without confirmed device loss)"
	case rendererErrPresent:
		return "present failed (submission still tracked — partial submit)"
	case rendererErrCapacity:
		return "capacity (string buffer or readback too small)"
	case rendererErrUnsupportedScene:
		return "unsupported surface source (the pinned import error)"
	case rendererErrScene:
		return "scene handle rejected (stale, unfinished or failed)"
	case rendererErrPanic:
		return "native panic contained"
	default:
		return "unknown renderer status"
	}
}

// Renderer service sentinel errors, distinguishable with errors.Is.
var (
	// ErrRendererStaleHandle: the handle encoded a past generation
	// (surface destroyed, submission retired, slot reused).
	ErrRendererStaleHandle = errors.New("native: renderer handle is stale")
	// ErrRendererBadHandle: the handle never existed or is malformed.
	ErrRendererBadHandle = errors.New("native: renderer handle is invalid")
	// ErrRendererNullArg: a required pointer argument was null.
	ErrRendererNullArg = errors.New("native: renderer argument was null")
	// ErrRendererBadValue: an invalid mode, extent or record value.
	ErrRendererBadValue = errors.New("native: renderer value rejected")
	// ErrRendererDevice: the shared D3D11 device could not be created or
	// failed (the pin's adapter enumeration found nothing — no WARP
	// fallback).
	ErrRendererDevice = errors.New("native: renderer device failure")
	// ErrRendererBusy: the per-surface pending-submission cap was
	// exceeded; backpressure applied before any GPU work was accepted.
	ErrRendererBusy = errors.New("native: renderer pending-submission cap exceeded")
	// ErrRendererPending: the operation was refused because un-retired
	// submissions remain (retire of a pending submission, destroy with
	// outstanding submissions).
	ErrRendererPending = errors.New("native: renderer submissions outstanding")
	// ErrRendererQuarantine: retirement was refused because the submission
	// failed without confirmed device loss; its record stays tracked.
	ErrRendererQuarantine = errors.New("native: renderer submission quarantined")
	// ErrRendererPresent: the Present call failed; the submission token is
	// still valid and tracked and must be polled and retired.
	ErrRendererPresent = errors.New("native: renderer present failed")
	// ErrRendererCapacity: the device-info string buffer or the
	// surface-read pixel buffer was too small.
	ErrRendererCapacity = errors.New("native: renderer buffer too small")
	// ErrRendererUnsupportedSource: the scene's surface source is the
	// pinned SurfaceSource::Unsupported stand-in; the pinned draw path
	// bails on it (ticket16) — pixel output for imported surface sources
	// awaits the capture-producer slice.
	ErrRendererUnsupportedSource = errors.New("native: renderer cannot import this surface source")
	// ErrRendererScene: the draw entry's scene handle did not resolve to
	// a finished, healthy scene (stale, malformed, unfinished or failed).
	ErrRendererScene = errors.New("native: renderer scene handle rejected")
	// ErrRendererPanic: a native panic was contained by catch_unwind.
	ErrRendererPanic = errors.New("native: renderer panic contained")
)

// RendererStatusError reports a non-zero renderer service status code.
type RendererStatusError struct {
	Code   int32
	Name   string
	Detail string
}

func (e *RendererStatusError) Error() string {
	detail := e.Detail
	if detail != "" {
		detail = ": " + detail
	}
	return ErrBadStatus.Error() + ": renderer status " + fmt.Sprint(e.Code) + " (" + e.Name + ")" + detail
}

func (e *RendererStatusError) Unwrap() error {
	switch e.Code {
	case rendererErrStale:
		return ErrRendererStaleHandle
	case rendererErrBadHandle:
		return ErrRendererBadHandle
	case rendererErrNullArg:
		return ErrRendererNullArg
	case rendererErrBadValue:
		return ErrRendererBadValue
	case rendererErrDevice:
		return ErrRendererDevice
	case rendererErrBusy:
		return ErrRendererBusy
	case rendererErrPending:
		return ErrRendererPending
	case rendererErrQuarantine:
		return ErrRendererQuarantine
	case rendererErrPresent:
		return ErrRendererPresent
	case rendererErrCapacity:
		return ErrRendererCapacity
	case rendererErrUnsupportedScene:
		return ErrRendererUnsupportedSource
	case rendererErrScene:
		return ErrRendererScene
	case rendererErrPanic:
		return ErrRendererPanic
	default:
		return ErrBadStatus
	}
}

func rendererStatusError(code int32, detail string) *RendererStatusError {
	return &RendererStatusError{Code: code, Name: rendererStatusName(code), Detail: detail}
}

// ---------------------------------------------------------------------------
// Surface modes, submission states, terminal records
// ---------------------------------------------------------------------------

// SurfaceMode selects the swap-chain composition mode (native surface_mode).
type SurfaceMode uint32

const (
	// SurfaceModeDCompPremultiplied: DirectComposition swap chain with
	// premultiplied alpha (the pin's default path).
	SurfaceModeDCompPremultiplied SurfaceMode = 0
	// SurfaceModeHwndAlphaIgnore: plain CreateSwapChainForHwnd swap chain
	// with alpha ignored (the pin's GPUI_DISABLE_DIRECT_COMPOSITION path).
	SurfaceModeHwndAlphaIgnore SurfaceMode = 1
)

// BackgroundAppearance selects the pinned draw path's clear color (the
// pin's WindowBackgroundAppearance reduced to the opaque/transparent
// distinction its render method makes; ticket16).
type BackgroundAppearance uint32

const (
	// BackgroundOpaque clears with [1,1,1,1] (the pin's opaque
	// appearance).
	BackgroundOpaque BackgroundAppearance = 0
	// BackgroundTransparent clears with [0,0,0,0] (the pin's
	// alpha-blended appearance).
	BackgroundTransparent BackgroundAppearance = 1
)

// SubmissionState values (native submission_state).
const (
	// SubmissionPending: GetData returned S_FALSE; the GPU has not
	// finished and the record stays tracked.
	SubmissionPending uint32 = 0
	// SubmissionCompleted: GetData returned S_OK; retirement is allowed.
	SubmissionCompleted uint32 = 1
	// SubmissionFailed: the query errored (including device removal);
	// retirement only on confirmed device loss.
	SubmissionFailed uint32 = 2
)

// RetireTerminal values (native retire_terminal).
const (
	// RetireCompleted: the submission retired after an observed completed
	// poll.
	RetireCompleted uint32 = 0
	// RetireAbortedLoss: the submission retired as an abort after a
	// retire-time GetDeviceRemovedReason confirmed device loss.
	RetireAbortedLoss uint32 = 1
)

// Pinned swap-chain facts the native service reports (mirror of
// RENDER_TARGET_FORMAT_VALUE / RENDERER_BUFFER_COUNT).
const (
	// RenderTargetFormatValue is DXGI_FORMAT_B8G8R8A8_UNORM (87).
	RenderTargetFormatValue uint32 = 87
	// RenderBufferCount is the pin's triple-buffered flip-sequential
	// buffer count.
	RenderBufferCount uint32 = 3
)

// ---------------------------------------------------------------------------
// Record mirrors
// ---------------------------------------------------------------------------

// SurfaceInfo mirrors GpuiGoSurfaceInfoRecord (36 bytes, alignment 4):
// mode, format, buffer count, device-pixel extent, the device's feature
// level, the resize generation and the recorded scale bits.
type SurfaceInfo struct {
	Mode             uint32
	Format           uint32
	BufferCount      uint32
	Width            uint32
	Height           uint32
	FeatureLevel     uint32
	ResizeGeneration uint32
	ScaleBits        uint32
	RecordSize       uint32
}

// SubmissionStateRecord mirrors GpuiGoSubmissionStateRecord (20 bytes,
// alignment 4): the state, the confirmed device-removed reason, the raw
// GetData HRESULT of the last poll and the Present HRESULT recorded at
// acceptance.
type SubmissionStateRecord struct {
	State               uint32
	DeviceRemovedReason int32
	DataHR              int32
	PresentHR           int32
	RecordSize          uint32
}

// RetireRecord mirrors GpuiGoRetireRecord (20 bytes, alignment 4): the
// terminal outcome plus the confirmed removal reason of an abort.
type RetireRecord struct {
	Terminal            uint32
	DeviceRemovedReason int32
	Reserved0           uint32
	Reserved1           uint32
	RecordSize          uint32
}

// DeviceInfoRecord mirrors GpuiGoDeviceInfoRecord (64 bytes, alignment 8).
// The three identity strings are UTF-8, packed contiguously in the order
// adapter description, driver name, driver version, addressed by the
// offset/length pairs.
type DeviceInfoRecord struct {
	VendorID             uint32
	DeviceID             uint32
	FeatureLevel         uint32
	AdapterFlags         uint32
	Reserved             uint32
	DedicatedVideoMemory uint64
	MultithreadProtected uint32
	AdapterDescOffset    uint32
	AdapterDescLen       uint32
	DriverNameOffset     uint32
	DriverNameLen        uint32
	DriverVersionOffset  uint32
	DriverVersionLen     uint32
	RecordSize           uint32
}

// DeviceInfo is the decoded environment identity of the shared device.
type DeviceInfo struct {
	// VendorID/DeviceID are the DXGI adapter ids (e.g. 0x10DE NVIDIA).
	VendorID uint32
	DeviceID uint32
	// FeatureLevel is the raw D3D_FEATURE_LEVEL value (0xb100 = 11.1).
	FeatureLevel uint32
	// Software reports the DXGI_ADAPTER_FLAG_SOFTWARE bit.
	Software bool
	// DedicatedVideoMemory is the adapter's dedicated VRAM in bytes.
	DedicatedVideoMemory uint64
	// MultithreadProtected reports the immediate context's protection
	// state (set at device creation per the renderer contract).
	MultithreadProtected bool
	// AdapterDescription is the DXGI adapter description string.
	AdapterDescription string
	// DriverName is the pinned vendor mapping (e.g. "NVIDIA Corporation").
	DriverName string
	// DriverVersion is the pinned driver-version probe result.
	DriverVersion string
}

// ---------------------------------------------------------------------------
// Service table mirror and validation
// ---------------------------------------------------------------------------

// rendererTable mirrors the native GpuiGoRendererTable: 8 function
// pointers then 8 self-check scalars; 96 bytes, alignment 8.
type rendererTable struct {
	surfaceCreate      uintptr
	surfaceResize      uintptr
	surfaceInfo        uintptr
	surfacePresent     uintptr
	surfaceDestroy     uintptr
	submissionPoll     uintptr
	submissionRetire   uintptr
	deviceInfo         uintptr
	surfaceDrawScene   uintptr
	surfaceRenderScene uintptr
	surfaceReadPixels  uintptr
	serviceVersion     uint32
	sizeOfTable        uint32
	alignOfTable       uint32
	sizeOfSurfaceInfo  uint32
	sizeOfSubmission   uint32
	sizeOfRetire       uint32
	sizeOfDeviceInfo   uint32
	maxPending         uint32
	maxInstanceBuffer  uint32
	pathMultisample    uint32
}

// rendererTableFromSlot reinterprets a reserved-slot word (the address of
// the native static service table) as a table pointer; the pointee is a
// Rust static copied out immediately (same pattern as layoutTableFromSlot).
func rendererTableFromSlot(slot uintptr) *rendererTable {
	return (*rendererTable)(*(*unsafe.Pointer)(unsafe.Pointer(&slot)))
}

// validateRendererTable checks the renderer service table copy against
// the Go mirrors before any function slot is trusted.
func validateRendererTable(t *rendererTable, path string) error {
	abi := func(field, expected, actual string) error {
		return &ABIError{Path: path, Field: field, Expected: expected, Actual: actual}
	}
	if t.serviceVersion != rendererServiceVersion {
		return abi("renderer service_version", fmt.Sprint(rendererServiceVersion), fmt.Sprint(t.serviceVersion))
	}
	if t.sizeOfTable != uint32(unsafe.Sizeof(rendererTable{})) {
		return abi("renderer size_of_table", fmt.Sprint(unsafe.Sizeof(rendererTable{})), fmt.Sprint(t.sizeOfTable))
	}
	if t.alignOfTable != uint32(unsafe.Alignof(rendererTable{})) {
		return abi("renderer align_of_table", fmt.Sprint(unsafe.Alignof(rendererTable{})), fmt.Sprint(t.alignOfTable))
	}
	if t.sizeOfSurfaceInfo != uint32(unsafe.Sizeof(SurfaceInfo{})) {
		return abi("renderer size_of_surface_info", fmt.Sprint(unsafe.Sizeof(SurfaceInfo{})), fmt.Sprint(t.sizeOfSurfaceInfo))
	}
	if t.sizeOfSubmission != uint32(unsafe.Sizeof(SubmissionStateRecord{})) {
		return abi("renderer size_of_submission_state", fmt.Sprint(unsafe.Sizeof(SubmissionStateRecord{})), fmt.Sprint(t.sizeOfSubmission))
	}
	if t.sizeOfRetire != uint32(unsafe.Sizeof(RetireRecord{})) {
		return abi("renderer size_of_retire_record", fmt.Sprint(unsafe.Sizeof(RetireRecord{})), fmt.Sprint(t.sizeOfRetire))
	}
	if t.sizeOfDeviceInfo != uint32(unsafe.Sizeof(DeviceInfoRecord{})) {
		return abi("renderer size_of_device_info", fmt.Sprint(unsafe.Sizeof(DeviceInfoRecord{})), fmt.Sprint(t.sizeOfDeviceInfo))
	}
	if t.maxPending == 0 {
		return &CapabilityError{Path: path, Detail: "renderer max_pending_submissions is zero"}
	}
	if t.maxInstanceBuffer == 0 {
		return &CapabilityError{Path: path, Detail: "renderer max_instance_buffer_bytes is zero"}
	}
	if t.pathMultisample == 0 {
		return &CapabilityError{Path: path, Detail: "renderer path_multisample_count is zero"}
	}
	slots := [...]uintptr{
		t.surfaceCreate, t.surfaceResize, t.surfaceInfo, t.surfacePresent,
		t.surfaceDestroy, t.submissionPoll, t.submissionRetire, t.deviceInfo,
		t.surfaceDrawScene, t.surfaceRenderScene, t.surfaceReadPixels,
	}
	names := [...]string{
		"surface_create", "surface_resize", "surface_info", "surface_present",
		"surface_destroy", "submission_poll", "submission_retire", "device_info",
		"surface_draw_scene", "surface_render_scene", "surface_read_pixels",
	}
	for i, slot := range slots {
		if slot == 0 {
			return &CapabilityError{Path: path, Detail: "renderer table slot " + names[i] + " is null"}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Renderer service, surfaces and submissions
// ---------------------------------------------------------------------------

// RendererService is the typed accessor for the native renderer service of
// a loaded Library. It holds a Go-owned copy of the validated service
// table. It is safe for concurrent use; the native service serializes all
// immediate-context work behind its own device mutex.
type RendererService struct {
	lib   *Library
	table rendererTable
}

// Renderer returns the renderer service of the loaded artifact. The
// capability bit ("renderer-d3d11") must be present and the service table
// in reserved slot 2 must validate (service version and record sizes)
// before any call is made. An artifact without the renderer capability
// fails with ErrMissingCapability here, not at Load.
func (l *Library) Renderer() (*RendererService, error) {
	if l.released.Load() {
		return nil, ErrClosed
	}
	if l.identity.Capabilities&capRendererD3D11 == 0 {
		return nil, &CapabilityError{
			Path:     l.identity.Path,
			Required: capRendererD3D11,
			Actual:   l.identity.Capabilities,
			Detail:   "renderer-d3d11 capability bit missing",
		}
	}
	slot := l.table.reserved[2]
	if slot == 0 {
		return nil, &CapabilityError{Path: l.identity.Path, Detail: "renderer service table (reserved slot 2) is null"}
	}
	table := *rendererTableFromSlot(slot) // copy out of DLL memory
	if err := validateRendererTable(&table, l.identity.Path); err != nil {
		return nil, err
	}
	return &RendererService{lib: l, table: table}, nil
}

// MaxPendingSubmissions is the native per-surface cap on tracked
// (un-retired) submissions — the bounded in-flight bound the Go layer
// mirrors for backpressure.
func (s *RendererService) MaxPendingSubmissions() int {
	return int(s.table.maxPending)
}

// SurfaceHandle is a validated opaque native surface handle. It becomes
// stale after Destroy (or slot reuse).
type SurfaceHandle uint64

// SubmissionHandle is a validated opaque native submission handle. It
// becomes stale after Retire (exactly-once terminal record).
type SubmissionHandle uint64

// RGBA is a clear color, one f32 per channel, crossing the ABI as bits.
type RGBA struct {
	R float32
	G float32
	B float32
	A float32
}

// bits packs the clear color into the ABI's 4 x u32 f32-bit record.
func (c RGBA) bits() [4]uint32 {
	return [4]uint32{
		math.Float32bits(c.R), math.Float32bits(c.G), math.Float32bits(c.B), math.Float32bits(c.A),
	}
}

// CreateSurface attaches a swap chain to the window identified by hwnd in
// the requested mode, at the given device-pixel extent (clamped to >= 1
// natively, as the pin does). scale is the window's device-pixels-per-
// logical-pixel factor, recorded with the surface. The shared D3D11
// device is created on first use (per-process single device).
func (s *RendererService) CreateSurface(hwnd uintptr, mode SurfaceMode, width, height uint32, scale float32) (SurfaceHandle, error) {
	if s.lib.released.Load() {
		return 0, ErrClosed
	}
	if mode != SurfaceModeDCompPremultiplied && mode != SurfaceModeHwndAlphaIgnore {
		return 0, rendererStatusError(rendererErrBadValue, "renderer_surface_create: unknown mode")
	}
	var handle uint64
	code, err := callRendererSurfaceCreate(
		s.table.surfaceCreate, uint64(hwnd), uint32(mode), width, height,
		math.Float32bits(scale), &handle)
	if err != nil {
		return 0, err
	}
	if code != rendererStatusOK {
		return 0, rendererStatusError(code, "renderer_surface_create")
	}
	return SurfaceHandle(handle), nil
}

// Resize resizes the surface's swap chain to the new device-pixel extent
// (the pinned resize path: unbind, drop views, ResizeBuffers(3, ...),
// recreate, rebind; unchanged extents are a no-op).
func (s *RendererService) Resize(surface SurfaceHandle, width, height uint32) error {
	if s.lib.released.Load() {
		return ErrClosed
	}
	code, err := callRendererSurfaceResize(s.table.surfaceResize, uint64(surface), width, height)
	if err != nil {
		return err
	}
	if code != rendererStatusOK {
		return rendererStatusError(code, "renderer_surface_resize")
	}
	return nil
}

// Info returns the surface's facts (mode, format, buffer count, extent,
// feature level, resize generation, scale bits).
func (s *RendererService) Info(surface SurfaceHandle) (SurfaceInfo, error) {
	if s.lib.released.Load() {
		return SurfaceInfo{}, ErrClosed
	}
	var info SurfaceInfo
	code, err := callRendererSurfaceInfo(s.table.surfaceInfo, uint64(surface), &info)
	if err != nil {
		return SurfaceInfo{}, err
	}
	if code != rendererStatusOK {
		return SurfaceInfo{}, rendererStatusError(code, "renderer_surface_info")
	}
	if info.RecordSize != uint32(unsafe.Sizeof(SurfaceInfo{})) {
		return SurfaceInfo{}, &ABIError{
			Field:    "surface info record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(SurfaceInfo{})),
			Actual:   fmt.Sprint(info.RecordSize),
		}
	}
	return info, nil
}

// PresentClear submits one clear+present frame and returns the tracked
// submission token. Acceptance is not completion: the returned submission
// must be polled until SubmissionCompleted (or a confirmed-loss abort)
// before Retire. A present failure returns ErrRendererPresent *and* the
// valid token (the partial-submit rule): the submission is still tracked
// and must be retired.
func (s *RendererService) PresentClear(surface SurfaceHandle, color RGBA) (SubmissionHandle, error) {
	if s.lib.released.Load() {
		return 0, ErrClosed
	}
	bits := color.bits()
	var handle uint64
	code, err := callRendererSurfacePresent(s.table.surfacePresent, uint64(surface), &bits, &handle)
	if err != nil {
		return 0, err
	}
	if code == rendererErrPresent {
		// Partial submit: the token is valid and tracked; return both.
		return SubmissionHandle(handle), rendererStatusError(code, "renderer_surface_present")
	}
	if code != rendererStatusOK {
		return 0, rendererStatusError(code, "renderer_surface_present")
	}
	return SubmissionHandle(handle), nil
}

// DrawScene renders the finished scene into the surface's back buffer,
// presents it, and returns the tracked submission token (the pin's
// DirectXRenderer::draw = render + present, with the event-query
// retirement; ticket16). The scene handle must reference a finished
// scene of the scene service (ERR_RENDERER_SCENE otherwise); sprite
// scenes need the atlas handle that owns their textures. Bounded
// in-flight backpressure applies before any GPU work is accepted. A
// present failure still returns the tracked submission token together
// with the error (the partial-submit rule).
func (s *RendererService) DrawScene(surface SurfaceHandle, scene SceneHandle, appearance BackgroundAppearance, atlas AtlasHandle) (SubmissionHandle, error) {
	if s.lib.released.Load() {
		return 0, ErrClosed
	}
	var handle uint64
	code, err := callRendererSurfaceDrawScene(s.table.surfaceDrawScene, uint64(surface), uint64(scene), uint32(appearance), uint64(atlas), &handle)
	if err != nil {
		return 0, err
	}
	if code == rendererErrPresent {
		return SubmissionHandle(handle), rendererStatusError(code, "renderer_surface_draw_scene")
	}
	if code != rendererStatusOK {
		return 0, rendererStatusError(code, "renderer_surface_draw_scene")
	}
	return SubmissionHandle(handle), nil
}

// RenderScene renders the finished scene without presenting (the pin's
// render body, the readback test path; ticket16). No submission token
// is issued — pair with ReadPixels, then DrawScene to present.
func (s *RendererService) RenderScene(surface SurfaceHandle, scene SceneHandle, appearance BackgroundAppearance, atlas AtlasHandle) error {
	if s.lib.released.Load() {
		return ErrClosed
	}
	code, err := callRendererSurfaceRenderScene(s.table.surfaceRenderScene, uint64(surface), uint64(scene), uint32(appearance), uint64(atlas))
	if err != nil {
		return err
	}
	if code != rendererStatusOK {
		return rendererStatusError(code, "renderer_surface_render_scene")
	}
	return nil
}

// ReadPixels reads the surface's current back buffer back through a
// staging texture (the pin's render_to_image tail; ticket16), returning
// BGRA->RGBA-swapped RGBA bytes (width*height*4). Call it after
// RenderScene (before presenting) for a deterministic read.
func (s *RendererService) ReadPixels(surface SurfaceHandle, buf []byte) ([]byte, error) {
	if s.lib.released.Load() {
		return nil, ErrClosed
	}
	var needed uint32
	// First call with a zero-length buffer reports the required size.
	if _, err := callRendererSurfaceReadPixels(s.table.surfaceReadPixels, uint64(surface), nil, &needed); err != nil {
		return nil, err
	}
	if needed == 0 {
		return nil, rendererStatusError(rendererErrBadValue, "renderer_surface_read_pixels: zero-sized back buffer")
	}
	if len(buf) < int(needed) {
		buf = make([]byte, needed)
	}
	code, err := callRendererSurfaceReadPixels(s.table.surfaceReadPixels, uint64(surface), buf, &needed)
	if err != nil {
		return nil, err
	}
	if code != rendererStatusOK {
		return nil, rendererStatusError(code, "renderer_surface_read_pixels")
	}
	return buf[:needed], nil
}

// DestroySurface frees the surface and its swap chain. It is refused with
// ErrRendererPending while un-retired submissions remain — drain them
// first (poll to completion, retire). After a successful destroy the
// handle is stale.
func (s *RendererService) DestroySurface(surface SurfaceHandle) error {
	if s.lib.released.Load() {
		return ErrClosed
	}
	code, err := callRendererSurfaceDestroy(s.table.surfaceDestroy, uint64(surface))
	if err != nil {
		return err
	}
	if code != rendererStatusOK {
		return rendererStatusError(code, "renderer_surface_destroy")
	}
	return nil
}

// Poll performs one bounded, nonblocking poll of a submission and returns
// its current state record. Pending means S_FALSE (still tracked);
// Completed means S_OK and is the only completion evidence; Failed means
// a query error or confirmed device removal (see DeviceRemovedReason).
// Repeated polls of a terminal state are idempotent.
func (s *RendererService) Poll(submission SubmissionHandle) (SubmissionStateRecord, error) {
	if s.lib.released.Load() {
		return SubmissionStateRecord{}, ErrClosed
	}
	var record SubmissionStateRecord
	code, err := callRendererSubmissionPoll(s.table.submissionPoll, uint64(submission), &record)
	if err != nil {
		return SubmissionStateRecord{}, err
	}
	if code != rendererStatusOK {
		return SubmissionStateRecord{}, rendererStatusError(code, "renderer_submission_poll")
	}
	if record.RecordSize != uint32(unsafe.Sizeof(SubmissionStateRecord{})) {
		return SubmissionStateRecord{}, &ABIError{
			Field:    "submission state record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(SubmissionStateRecord{})),
			Actual:   fmt.Sprint(record.RecordSize),
		}
	}
	return record, nil
}

// Retire produces the exactly-once terminal record for a submission. It
// is refused with ErrRendererPending while the submission is still
// pending, with ErrRendererQuarantine when it failed without confirmed
// device loss, and with ErrRendererStaleHandle after it was retired (the
// second-retire typed error).
func (s *RendererService) Retire(submission SubmissionHandle) (RetireRecord, error) {
	if s.lib.released.Load() {
		return RetireRecord{}, ErrClosed
	}
	var record RetireRecord
	code, err := callRendererSubmissionRetire(s.table.submissionRetire, uint64(submission), &record)
	if err != nil {
		return RetireRecord{}, err
	}
	if code != rendererStatusOK {
		return RetireRecord{}, rendererStatusError(code, "renderer_submission_retire")
	}
	if record.RecordSize != uint32(unsafe.Sizeof(RetireRecord{})) {
		return RetireRecord{}, &ABIError{
			Field:    "retire record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(RetireRecord{})),
			Actual:   fmt.Sprint(record.RecordSize),
		}
	}
	return record, nil
}

// DeviceInfo returns the environment identity of the shared device
// (adapter description, feature level, driver identity). The strings are
// fetched through the native capacity protocol: the empty-buffer probe
// reports the needed byte count, the retry packs the three strings. The
// shared device is created on first use if needed.
func (s *RendererService) DeviceInfo() (*DeviceInfo, error) {
	if s.lib.released.Load() {
		return nil, ErrClosed
	}
	var record DeviceInfoRecord
	var needed uint32
	// Capacity probe: an empty-buffer call reports the packed byte count.
	code, err := callRendererDeviceInfo(s.table.deviceInfo, &record, nil, &needed)
	if err != nil {
		return nil, err
	}
	var buf []byte
	if code == rendererErrCapacity {
		buf = make([]byte, needed)
		code, err = callRendererDeviceInfo(s.table.deviceInfo, &record, buf, &needed)
		if err != nil {
			return nil, err
		}
	}
	if code != rendererStatusOK {
		return nil, rendererStatusError(code, "renderer_device_info")
	}
	if record.RecordSize != uint32(unsafe.Sizeof(DeviceInfoRecord{})) {
		return nil, &ABIError{
			Field:    "device info record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(DeviceInfoRecord{})),
			Actual:   fmt.Sprint(record.RecordSize),
		}
	}
	return decodeDeviceInfo(record, buf), nil
}

// decodeDeviceInfo converts the packed record + string buffer into the
// value form, validating the offsets/lengths against the buffer.
func decodeDeviceInfo(record DeviceInfoRecord, buf []byte) *DeviceInfo {
	slice := func(off, length uint32) string {
		if uint64(off)+uint64(length) > uint64(len(buf)) {
			return ""
		}
		return string(buf[off : off+length])
	}
	return &DeviceInfo{
		VendorID:             record.VendorID,
		DeviceID:             record.DeviceID,
		FeatureLevel:         record.FeatureLevel,
		Software:             record.AdapterFlags&1 != 0,
		DedicatedVideoMemory: record.DedicatedVideoMemory,
		MultithreadProtected: record.MultithreadProtected != 0,
		AdapterDescription:   slice(record.AdapterDescOffset, record.AdapterDescLen),
		DriverName:           slice(record.DriverNameOffset, record.DriverNameLen),
		DriverVersion:        slice(record.DriverVersionOffset, record.DriverVersionLen),
	}
}
