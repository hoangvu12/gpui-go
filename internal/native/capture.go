// Capture service access (ticket28, reserved slot 10).
//
// The Windows Graphics Capture producer on the renderer service's
// actual D3D11 device (the IDXGIDevice -> CreateDirect3D11Device-
// FromDXGIDevice -> free-threaded frame pool chain), with monitor
// source enumeration (Win32), bounded frame-arrival notifications
// drained by the foreground poll (the checkout/copy under the
// renderer's device lock — the immutable destination textures), the
// content-size watch (pool Recreate), and the stop/destroy lifecycle.
// No Go pointer is retained across calls.

package native

import (
	"errors"
	"fmt"
	"unsafe"
)

// captureServiceVersion mirrors GPUI_GO_CAPTURE_SERVICE_VERSION.
const captureServiceVersion uint32 = 1

// Capture status codes (mirror capture_status in capture.rs).
const (
	captureStatusOK          int32 = 0
	captureStatusStaleHandle int32 = -1
	captureStatusBadHandle   int32 = -2
	captureStatusNullArg     int32 = -3
	captureStatusBadValue    int32 = -4
	captureStatusUnsupported int32 = -5
	captureStatusStopped     int32 = -6
	captureStatusDevice      int32 = -7
	captureStatusCopy        int32 = -8
	captureStatusRecordSize  int32 = -9
	captureStatusCapacity    int32 = -10
	captureStatusPanic       int32 = 100
)

// CaptureSourceRecord mirrors GpuiGoCaptureSourceRecord (40 bytes,
// alignment 8): one enumerated monitor source.
type CaptureSourceRecord struct {
	// HMonitor is the monitor handle (create the stream from it).
	HMonitor uint64
	// Width/Height are the monitor resolution in device pixels.
	Width  int32
	Height int32
	// IsMain is 1 for the primary monitor.
	IsMain uint32
	// ID is the 1-based source id.
	ID uint32
	// LabelOffset/LabelLen slice the label out of the string buffer.
	LabelOffset uint32
	LabelLen    uint32
	// RecordSize self-check.
	RecordSize uint32
}

// CaptureFrameRecord mirrors GpuiGoCaptureFrameRecord (40 bytes,
// alignment 8): one published (copied) frame.
type CaptureFrameRecord struct {
	// Handle releases the frame with FrameRelease.
	Handle uint64
	// Width/Height are the destination texture's (pool buffer) size.
	Width  uint32
	Height uint32
	// ContentWidth/ContentHeight are the capture item's content size
	// (the caller's crop; changes trigger the pool Recreate).
	ContentWidth  int32
	ContentHeight int32
	// Timestamp is the frame's SystemRelativeTime (100ns units,
	// clamped at 0).
	Timestamp uint64
	// RecordSize self-check.
	RecordSize uint32
}

// CaptureStreamStats mirrors GpuiGoCaptureStreamStats (36 bytes,
// alignment 4).
type CaptureStreamStats struct {
	// Pending frames right now (the bounded queue).
	Pending uint32
	// Published frames not yet released.
	Published uint32
	// Dropped frames (the drop-oldest overflow counter).
	Dropped uint32
	// SizeChanges counts pool Recreate calls.
	SizeChanges uint32
	// Stopped is 1 when the session is stopped.
	Stopped uint32
	// PoolWidth/PoolHeight are the current frame-pool dimensions.
	PoolWidth  int32
	PoolHeight int32
	// LastErrorHR is the last failing HRESULT (0 = none).
	LastErrorHR int32
	// RecordSize self-check.
	RecordSize uint32
}

// captureNativeTable mirrors GpuiGoCaptureTable: 9 function pointers
// then 9 self-check/capacity scalars; size 112, alignment 8.
type captureNativeTable struct {
	supported        uintptr
	sourcesEnumerate uintptr
	streamCreate     uintptr
	streamPoll       uintptr
	streamStop       uintptr
	streamDestroy    uintptr
	streamStats      uintptr
	frameRelease     uintptr
	panicProbe       uintptr

	serviceVersion     uint32
	sizeOfTable        uint32
	alignOfTable       uint32
	sizeOfSourceRecord uint32
	sizeOfFrameRecord  uint32
	sizeOfStreamStats  uint32
	maxStreams         uint32
	maxPendingFrames   uint32
	bufferCount        uint32
}

// captureTableFromSlot reads the service table out of DLL memory (the
// bootstrap table's reserved slot 10).
func captureTableFromSlot(slot uintptr) *captureNativeTable {
	return (*captureNativeTable)(*(*unsafe.Pointer)(unsafe.Pointer(&slot)))
}

// validateCaptureTable asserts the native self-check fields against
// this mirror's compile-time layout.
func validateCaptureTable(table *captureNativeTable, path string) error {
	if table.serviceVersion != captureServiceVersion {
		return fmt.Errorf("native: capture service version = %d, want %d (%s)", table.serviceVersion, captureServiceVersion, path)
	}
	if table.sizeOfTable != uint32(unsafe.Sizeof(captureNativeTable{})) {
		return fmt.Errorf("native: capture table size = %d, want %d (%s)", table.sizeOfTable, unsafe.Sizeof(captureNativeTable{}), path)
	}
	if table.alignOfTable != uint32(unsafe.Alignof(captureNativeTable{})) {
		return fmt.Errorf("native: capture table alignment = %d, want %d (%s)", table.alignOfTable, unsafe.Alignof(captureNativeTable{}), path)
	}
	if table.sizeOfSourceRecord != uint32(unsafe.Sizeof(CaptureSourceRecord{})) {
		return fmt.Errorf("native: capture source record size = %d, want %d", table.sizeOfSourceRecord, unsafe.Sizeof(CaptureSourceRecord{}))
	}
	if table.sizeOfFrameRecord != uint32(unsafe.Sizeof(CaptureFrameRecord{})) {
		return fmt.Errorf("native: capture frame record size = %d, want %d", table.sizeOfFrameRecord, unsafe.Sizeof(CaptureFrameRecord{}))
	}
	if table.sizeOfStreamStats != uint32(unsafe.Sizeof(CaptureStreamStats{})) {
		return fmt.Errorf("native: capture stream stats size = %d, want %d", table.sizeOfStreamStats, unsafe.Sizeof(CaptureStreamStats{}))
	}
	return nil
}

// CaptureService is the capture producer service client (reserved
// slot 10).
type CaptureService struct {
	lib   *Library
	table captureNativeTable
}

// Capture fetches the capture service (capability bit 11, reserved
// slot 10).
func (l *Library) Capture() (*CaptureService, error) {
	if l.released.Load() {
		return nil, ErrClosed
	}
	if l.identity.Capabilities&capCaptureWGC == 0 {
		return nil, &CapabilityError{
			Path:     l.identity.Path,
			Required: capCaptureWGC,
			Actual:   l.identity.Capabilities,
			Detail:   "capture-wgc capability bit missing",
		}
	}
	slot := l.table.reserved[10]
	if slot == 0 {
		return nil, &CapabilityError{Path: l.identity.Path, Detail: "capture service table (reserved slot 10) is null"}
	}
	table := *captureTableFromSlot(slot) // copy out of DLL memory
	if err := validateCaptureTable(&table, l.identity.Path); err != nil {
		return nil, err
	}
	return &CaptureService{lib: l, table: table}, nil
}

// CaptureStreamHandle is a validated opaque native stream handle.
type CaptureStreamHandle uint64

// CaptureFrameHandle is a validated opaque published-frame handle.
type CaptureFrameHandle uint64

// The capture service errors.
var (
	// ErrCaptureStaleHandle reports a handle that encoded a past
	// generation.
	ErrCaptureStaleHandle = errors.New("native: capture handle is stale (destroyed or slot reused)")
	// ErrCaptureBadHandle reports a malformed or never-issued handle.
	ErrCaptureBadHandle = errors.New("native: capture handle is malformed")
	// ErrCaptureBadValue reports an invalid argument.
	ErrCaptureBadValue = errors.New("native: capture argument rejected")
	// ErrCaptureUnsupported reports Windows Graphics Capture being
	// unavailable (the blocked-environment record, not a passed
	// capture behavior).
	ErrCaptureUnsupported = errors.New("native: Windows Graphics Capture is unavailable")
	// ErrCaptureStopped reports an operation on a stopped stream.
	ErrCaptureStopped = errors.New("native: capture stream is stopped")
	// ErrCaptureDevice reports the capture-device creation failure.
	ErrCaptureDevice = errors.New("native: capture device creation failed")
	// ErrCaptureCopy reports the frame-copy failure (the HRESULT is in
	// the stream stats).
	ErrCaptureCopy = errors.New("native: capture frame copy failed")
	// ErrCaptureCapacity reports a too-small poll buffer (retry).
	ErrCaptureCapacity = errors.New("native: capture poll buffer too small")
	// ErrCapturePanic reports a contained native panic.
	ErrCapturePanic = errors.New("native: capture service panicked")
)

// captureError maps a native status code to an error.
func captureError(code int32) error {
	switch code {
	case captureStatusStaleHandle:
		return ErrCaptureStaleHandle
	case captureStatusBadHandle:
		return ErrCaptureBadHandle
	case captureStatusBadValue:
		return ErrCaptureBadValue
	case captureStatusUnsupported:
		return ErrCaptureUnsupported
	case captureStatusStopped:
		return ErrCaptureStopped
	case captureStatusDevice:
		return ErrCaptureDevice
	case captureStatusCopy:
		return ErrCaptureCopy
	case captureStatusCapacity:
		return ErrCaptureCapacity
	case captureStatusPanic:
		return ErrCapturePanic
	default:
		return fmt.Errorf("native: capture service status %d", code)
	}
}

// CaptureSource is one enumerated monitor source with its decoded
// label.
type CaptureSource struct {
	// HMonitor is the monitor handle.
	HMonitor uint64
	// Width/Height are the resolution in device pixels.
	Width  int32
	Height int32
	// IsMain is true for the primary monitor.
	IsMain bool
	// ID is the 1-based source id.
	ID uint32
	// Label is the friendly device name.
	Label string
}

// Supported reports whether Windows Graphics Capture is available on
// this system (the item interop resolving a monitor; a false result is
// the blocked-environment record).
func (s *CaptureService) Supported() (bool, error) {
	if s == nil {
		return false, fmt.Errorf("native: no capture service")
	}
	supported, code, err := callCaptureSupported(s.table.supported)
	if err != nil {
		return false, err
	}
	switch code {
	case captureStatusOK:
		return supported != 0, nil
	case captureStatusUnsupported:
		return false, nil
	default:
		return false, captureError(code)
	}
}

// Sources enumerates the capturable monitors (the pin's screen_sources
// metadata: resolution, label, is_main, 1-based id). The capacity
// protocol: a too-small record or string buffer returns
// ErrCaptureCapacity with the needs (retry).
func (s *CaptureService) Sources(records []CaptureSourceRecord, stringsBuf []byte) ([]CaptureSource, uint32, uint32, error) {
	if s == nil {
		return nil, 0, 0, fmt.Errorf("native: no capture service")
	}
	count, stringsNeeded, code, err := callCaptureSourcesEnumerate(
		s.table.sourcesEnumerate,
		records,
		stringsBuf,
	)
	if err != nil {
		return nil, count, stringsNeeded, err
	}
	if code != captureStatusOK {
		return nil, count, stringsNeeded, captureError(code)
	}
	sources := make([]CaptureSource, 0, count)
	for i := uint32(0); i < count && int(i) < len(records); i++ {
		record := records[i]
		if record.RecordSize != uint32(unsafe.Sizeof(CaptureSourceRecord{})) {
			return nil, count, stringsNeeded, fmt.Errorf("native: capture source record size %d does not match the mirror", record.RecordSize)
		}
		source := CaptureSource{
			HMonitor: record.HMonitor,
			Width:    record.Width,
			Height:   record.Height,
			IsMain:   record.IsMain != 0,
			ID:       record.ID,
		}
		if record.LabelLen > 0 {
			start := int(record.LabelOffset)
			end := start + int(record.LabelLen)
			if end > len(stringsBuf) {
				return nil, count, stringsNeeded, fmt.Errorf("native: capture label offset out of bounds")
			}
			source.Label = string(stringsBuf[start:end])
		}
		sources = append(sources, source)
	}
	return sources, count, stringsNeeded, nil
}

// CreateStream creates the WinRT producer for the monitor ON THE
// RENDERER'S DEVICE (the ticket's same-device criterion) and starts
// the capture session. The initial item size is returned for the
// caller's layout.
func (s *CaptureService) CreateStream(hmonitor uint64) (CaptureStreamHandle, int32, int32, error) {
	if s == nil {
		return 0, 0, 0, fmt.Errorf("native: no capture service")
	}
	handle, width, height, code, err := callCaptureStreamCreate(s.table.streamCreate, hmonitor)
	if err != nil {
		return 0, 0, 0, err
	}
	if code != captureStatusOK {
		return 0, 0, 0, captureError(code)
	}
	return handle, width, height, nil
}

// Poll is the foreground checkout/copy: each pending frame is copied
// (under the renderer's device lock) into an immutable destination
// texture and published as a frame record. The capacity protocol: a
// nil/too-small buffer returns ErrCaptureCapacity with the need
// (nothing was drained). A stopped stream with no pending frames
// returns ErrCaptureStopped.
func (s *CaptureService) Poll(stream CaptureStreamHandle, frames []CaptureFrameRecord) ([]CaptureFrameRecord, uint32, error) {
	if s == nil {
		return nil, 0, fmt.Errorf("native: no capture service")
	}
	count, code, err := callCaptureStreamPoll(s.table.streamPoll, stream, frames)
	if err != nil {
		return nil, 0, err
	}
	if code != captureStatusOK {
		return nil, count, captureError(code)
	}
	published := make([]CaptureFrameRecord, 0, count)
	for i := uint32(0); i < count && int(i) < len(frames); i++ {
		if frames[i].RecordSize != uint32(unsafe.Sizeof(CaptureFrameRecord{})) {
			return nil, count, fmt.Errorf("native: capture frame record size %d does not match the mirror", frames[i].RecordSize)
		}
		published = append(published, frames[i])
	}
	return published, count, nil
}

// StopStream stops the capture session (the pin's CaptureStream drop
// -> capture.stop()); late frame arrivals are refused and already
// pending frames remain drainable.
func (s *CaptureService) StopStream(stream CaptureStreamHandle) error {
	if s == nil {
		return fmt.Errorf("native: no capture service")
	}
	code, err := callCaptureStreamStop(s.table.streamStop, stream)
	if err != nil {
		return err
	}
	if code != captureStatusOK {
		return captureError(code)
	}
	return nil
}

// DestroyStream drops the session, frame pool and item (apartment-
// affine release on the native MTA thread) and releases every
// published frame texture.
func (s *CaptureService) DestroyStream(stream CaptureStreamHandle) error {
	if s == nil {
		return fmt.Errorf("native: no capture service")
	}
	code, err := callCaptureStreamDestroy(s.table.streamDestroy, stream)
	if err != nil {
		return err
	}
	if code != captureStatusOK {
		return captureError(code)
	}
	return nil
}

// StreamStats returns the observability record (the bounded queue,
// drop counter, size changes, the last failing HRESULT).
func (s *CaptureService) StreamStats(stream CaptureStreamHandle) (CaptureStreamStats, error) {
	if s == nil {
		return CaptureStreamStats{}, fmt.Errorf("native: no capture service")
	}
	var out CaptureStreamStats
	code, err := callCaptureStreamStats(s.table.streamStats, stream, &out)
	if err != nil {
		return CaptureStreamStats{}, err
	}
	if code != captureStatusOK {
		return CaptureStreamStats{}, captureError(code)
	}
	if out.RecordSize != uint32(unsafe.Sizeof(CaptureStreamStats{})) {
		return CaptureStreamStats{}, fmt.Errorf("native: capture stats record size %d does not match the mirror", out.RecordSize)
	}
	return out, nil
}

// FrameRelease releases a published frame's destination texture (the
// renderer's surface-view cache holds its own reference while a scene
// still draws it).
func (s *CaptureService) FrameRelease(frame CaptureFrameHandle) error {
	if s == nil {
		return fmt.Errorf("native: no capture service")
	}
	code, err := callCaptureFrameRelease(s.table.frameRelease, frame)
	if err != nil {
		return err
	}
	if code != captureStatusOK {
		return captureError(code)
	}
	return nil
}

// SurfaceSourceTag values for the scene's surface record (mirror
// surface_source_kind in scene.rs).
const (
	// SurfaceSourceUnsupported is the pinned stand-in (the renderer's
	// draw path bails on it explicitly).
	SurfaceSourceUnsupported uint32 = 0
	// SurfaceSourceCaptureFrame is a capture frame handle (the pinned
	// WindowsCapture import; the renderer resolves it through the
	// capture service).
	SurfaceSourceCaptureFrame uint32 = 1
)
