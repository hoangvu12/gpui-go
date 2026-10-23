package gpui

// This file is the port's capture model half of ticket28: the
// screen-capture source/stream/frame model over App and the native
// capture service (internal/native, reserved slot 10 of the rev-10
// artifact), plus the Surface element port
// (crates/gpui/src/elements/surface.rs).
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/platform/screen_capture/windows.rs 1-44
//     (screen_sources: Monitor::enumerate with resolution, label,
//     is-main and a 1-based id, resolved on a background thread through
//     a oneshot receiver) and 75-161 (capture_monitor: the free-threaded
//     windows-capture producer over the monitor with the BGRA8 color
//     format; CaptureHandler::on_frame_arrived 116-133 extracts the raw
//     texture, the content size and the timestamp and hands them to the
//     frame callback; CaptureStream::Drop 153-161 stops the session).
//   - crates/gpui/src/platform.rs 485-494 (SourceMetadata: id, label,
//     is_main, resolution), 497-506 (ScreenCaptureSource: metadata +
//     stream), 511-515 (ScreenCaptureStream: metadata),
//     517 (ScreenCaptureFrame) and 237-239
//     (Platform::is_screen_capture_supported).
//   - crates/gpui/src/platform/windows_screen_capture.rs
//     (WindowsScreenCaptureFrame: the D3D11 texture, the device-pixel
//     size, the 100ns display time).
//   - crates/gpui/src/elements/surface.rs 74-141 (the Surface element:
//     the object-fit bounds of the source's content size inside the
//     element bounds, then paint_surface) and crates/gpui/src/style.rs
//     25-141 (ObjectFit::get_bounds).
//
// Bounded deviations, recorded honestly:
//
//   - FROZEN ARTIFACT GATE (rev-10): the scene service's
//     scene_insert_surface export pre-validates surface records with
//     a zero-reserved-words rule that predates ticket28
//     (check_surface in reference/native/src/scene.rs), so the tag-1
//     record carrying the frame handle — the record the state-level
//     insert validation and the renderer's capture-surface draw path
//     both accept — is currently REJECTED with the scene bad-value
//     error. The Go half builds the documented payload; the native
//     one-line fix (dropping the stale reserved check from
//     check_surface) is recorded for the next approved DLL rebuild,
//     after which the full capture→draw loop works unchanged from
//     here. The frozen answer is pinned by the capturespec tests.
//
//   - The pin's frame callback (the on_frame_arrived handoff into app
//     code through a boxed Send closure) cannot cross this port's ABI
//     safely; the native service replaces it with the bounded pending
//     queue drained by the foreground poll (the checkout/copy under the
//     renderer's device lock). The Go model exposes that poll directly:
//     Poll drains, publishes immutable destination frames and returns
//     their records; the app drives the cadence (the pin's callback
//     cadence is the producer's, the port's is the poller's).
//   - The pin's source discovery resolves on a background thread
//     through a oneshot; the native enumeration is synchronous on the
//     caller's thread, so App.ScreenCaptureSources is synchronous too
//     (with the service's capacity retry protocol).
//   - The pin's frame owns an Arc of the capture texture, so a scene
//     keeps any frame it references alive. The port's scene ABI
//     references a frame only by its native handle, which stays
//     resolvable only while the capture service still holds it
//     published; the stream therefore keeps exactly ONE published frame
//     (the current frame) and releases every superseded frame at the
//     poll that replaces it. A scene must paint the stream's current
//     frame; painting a frame the stream advanced past fails the draw
//     with the renderer's typed bad-handle error (the explicit record,
//     never a silent blank).
//   - The scene surface record packs the 64-bit frame handle across
//     its two reserved words (the rev-11 fix-up): every stream's
//     frames are paintable. (The rev-10 frozen ABI carried only the
//     low 32 bits, which restricted paintable frames to slot-0
//     streams; that limitation is gone.)
//   - The Surface element's style refinement carries no opacity in
//     this port yet (Style has no opacity field), so the element paints
//     with the ambient element opacity of the paint context instead of
//     the pin's with_element_opacity(style.opacity) scope; the
//     paired-opacity mechanism itself is the scene's and is unchanged.
//   - is_main comes from the native MONITORINFOF_PRIMARY flag (the
//     equivalent observable of the pin's primary-monitor comparison)
//     and the label is the MONITORINFOEXW device name.

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// ---------------------------------------------------------------------------
// The application's native capture service
// ---------------------------------------------------------------------------

// ErrCaptureService reports a failure to resolve the native capture
// service (the artifact missing the capture-wgc capability, or a failed
// load).
var ErrCaptureService = errors.New("gpui: native capture service unavailable")

// The native capture client's typed errors, aliased so callers can
// errors.Is them without importing internal/native (svg.go's
// ErrSvgParse pattern).
var (
	// ErrCaptureUnsupported reports Windows Graphics Capture being
	// unavailable — the blocked-environment record, not a passed capture
	// behavior (a false Supported() with a nil error).
	ErrCaptureUnsupported = native.ErrCaptureUnsupported
	// ErrCaptureStopped reports a poll of a stopped stream with nothing
	// left to drain.
	ErrCaptureStopped = native.ErrCaptureStopped
	// ErrCaptureStaleHandle reports a stream or frame handle that
	// encoded a past generation (destroyed or slot reused).
	ErrCaptureStaleHandle = native.ErrCaptureStaleHandle
	// ErrCaptureBadHandle reports a malformed or never-issued handle.
	ErrCaptureBadHandle = native.ErrCaptureBadHandle
	// ErrCaptureBadValue reports a rejected capture argument (for
	// example a zero monitor handle).
	ErrCaptureBadValue = native.ErrCaptureBadValue
	// ErrCaptureDevice reports the capture-device creation failure on
	// the renderer's D3D11 device.
	ErrCaptureDevice = native.ErrCaptureDevice
	// ErrCaptureCopy reports a frame-copy failure (the HRESULT is in the
	// stream stats).
	ErrCaptureCopy = native.ErrCaptureCopy
	// ErrCaptureCapacity reports a too-small poll or enumeration buffer
	// (retry with the reported need).
	ErrCaptureCapacity = native.ErrCaptureCapacity
)

// CaptureService returns the application's native capture service
// (App::screen-capture wiring through the process native library):
// lazily resolved once; the first failure sticks, exactly like the
// a11y and SVG accessors. Call it on the foreground thread.
func (a *App) CaptureService() (*native.CaptureService, error) {
	if a == nil {
		return nil, fmt.Errorf("gpui: CaptureService requires an application")
	}
	if s := a.captureService; s != nil {
		return s, nil
	}
	if err := a.captureErr; err != nil {
		return nil, err
	}
	lib, err := native.Load(native.Options{})
	if err != nil {
		a.captureErr = fmt.Errorf("%w: loading the native artifact: %w", ErrCaptureService, err)
		return nil, a.captureErr
	}
	svc, err := lib.Capture()
	if err != nil {
		a.captureErr = fmt.Errorf("%w: %v", ErrCaptureService, err)
		return nil, a.captureErr
	}
	a.captureService = svc
	return svc, nil
}

// ScreenCaptureSupported reports whether Windows Graphics Capture is
// available on this system (Platform::is_screen_capture_supported; the
// native probe resolves a capture item for a monitor). A false result
// with a nil error is the honest blocked-environment record: capture
// consent/availability is missing on this machine, which the caller
// must surface, not treat as passed capture behavior.
func (a *App) ScreenCaptureSupported() (bool, error) {
	svc, err := a.CaptureService()
	if err != nil {
		return false, err
	}
	return svc.Supported()
}

// ---------------------------------------------------------------------------
// Sources (the pin's screen_sources / SourceMetadata)
// ---------------------------------------------------------------------------

// ScreenCaptureSource is one capturable monitor (the pinned
// SourceMetadata plus the monitor handle the stream is created from).
type ScreenCaptureSource struct {
	// ID is the 1-based source identity (SourceMetadata.id).
	ID uint64
	// HMonitor is the monitor handle (the native create entry's
	// argument; the pin's Monitor).
	HMonitor uint64
	// Label is the friendly device name (SourceMetadata.label; the
	// native MONITORINFOEXW device name, for example "\\.\DISPLAY1").
	Label string
	// IsMain is true for the primary monitor (SourceMetadata.is_main).
	IsMain bool
	// Width and Height are the monitor resolution in device pixels
	// (SourceMetadata.resolution).
	Width  int32
	Height int32
}

// ScreenCaptureSources enumerates the capturable monitors (the pinned
// screen_sources): resolution, label, is-main and the 1-based id for
// every monitor. The native capacity protocol is honored internally:
// a too-small record or string buffer is retried with the reported
// needs (bounded attempts), so the caller never sees the capacity
// error on a healthy system. Call it on the foreground thread.
func (a *App) ScreenCaptureSources() ([]ScreenCaptureSource, error) {
	svc, err := a.CaptureService()
	if err != nil {
		return nil, err
	}
	records := make([]native.CaptureSourceRecord, 8)
	stringsBuf := make([]byte, 256)
	const attempts = 4
	for attempt := 0; ; attempt++ {
		sources, count, stringsNeeded, err := svc.Sources(records, stringsBuf)
		if err == nil {
			out := make([]ScreenCaptureSource, len(sources))
			for i, source := range sources {
				out[i] = ScreenCaptureSource{
					ID:       uint64(source.ID),
					HMonitor: source.HMonitor,
					Label:    source.Label,
					IsMain:   source.IsMain,
					Width:    source.Width,
					Height:   source.Height,
				}
			}
			return out, nil
		}
		if !errors.Is(err, native.ErrCaptureCapacity) {
			return nil, fmt.Errorf("gpui: ScreenCaptureSources: %w", err)
		}
		if attempt+1 >= attempts || count > 4096 || stringsNeeded > 1<<20 {
			return nil, fmt.Errorf("gpui: ScreenCaptureSources: %w", native.ErrCaptureCapacity)
		}
		if uint32(len(records)) < count {
			records = make([]native.CaptureSourceRecord, count)
		}
		if uint32(len(stringsBuf)) < stringsNeeded {
			stringsBuf = make([]byte, stringsNeeded)
		}
	}
}

// ---------------------------------------------------------------------------
// The stream (the pin's ScreenCaptureStream with the poll model)
// ---------------------------------------------------------------------------

// ScreenCaptureFrame is one published capture frame (the pinned
// ScreenCaptureFrame/WindowsScreenCaptureFrame): the immutable
// destination texture's identity plus the crop the caller applies.
type ScreenCaptureFrame struct {
	// Handle is the native frame identity. It stays resolvable while
	// the stream keeps this frame published (its current frame); the
	// renderer resolves it through the capture service when a scene
	// that references it draws.
	Handle uint64
	// Width and Height are the destination (pool buffer) texture's size
	// in device pixels.
	Width  uint32
	Height uint32
	// ContentWidth and ContentHeight are the capture item's content
	// size in device pixels — the caller's crop. A change from the pool
	// size triggers the native pool Recreate; the element's object fit
	// fits THIS size inside the element bounds.
	ContentWidth  int32
	ContentHeight int32
	// Timestamp is the frame's SystemRelativeTime in 100-nanosecond
	// units (the pinned display_time).
	Timestamp uint64
}

// CaptureStreamStats is the stream's observability record (the native
// stream stats plus the Go model's release counter).
type CaptureStreamStats struct {
	// Pending is the bounded pending-queue depth right now.
	Pending uint32
	// Published is the number of published (copied, not yet released)
	// frames. The stream keeps exactly one (the current frame).
	Published uint32
	// Dropped counts frames the bounded pending queue discarded
	// (drop-oldest overflow while the app was not polling).
	Dropped uint32
	// SizeChanges counts native pool Recreate calls (content-size
	// changes).
	SizeChanges uint32
	// Stopped is true once the session stopped.
	Stopped bool
	// PoolWidth and PoolHeight are the current frame-pool dimensions
	// (the content size the pool tracks).
	PoolWidth  int32
	PoolHeight int32
	// LastErrorHR is the last failing HRESULT (0 = none).
	LastErrorHR int32
	// FramesReleased is the Go stream's released-frame counter: every
	// frame the stream released because a newer frame superseded it
	// (the bounded published set's observable).
	FramesReleased uint64
}

// capturePollInterval is the poll cadence of PollForFrame's bounded
// wait loop (the app-driven equivalent of the pin's callback cadence;
// the queue is bounded and drop-oldest, so a slow poll loses frames by
// design, it never blocks the producer).
const capturePollInterval = 30 * time.Millisecond

// ScreenCaptureStream is a capture session on one monitor (the pinned
// ScreenCaptureStream): the native WinRT producer on the renderer's
// actual D3D11 device, drained by the app-driven poll. The lifecycle:
//
//   - Poll drains the bounded pending queue, publishing each frame's
//     immutable destination texture, and returns the drained records in
//     arrival order. The newest becomes the stream's current frame and
//     the ONLY published one: every superseded frame (the older frames
//     of the batch and the previous current) is released — the frame
//     release that fires when the scene stops referencing it (the app
//     paints the current frame, so a superseded frame is no longer
//     referenced).
//   - CurrentFrame returns the frame to paint.
//   - Stop stops the session (the pin's CaptureStream drop ->
//     capture.stop()); frames already pending remain drainable; a poll
//     of a stopped, drained stream reports ErrCaptureStopped.
//   - Destroy drops the session, pool and item and releases every
//     published frame (the native destroy is the whole teardown); the
//     stream is unusable afterwards and reports ErrCaptureStaleHandle.
//
// The stream's Go state is mutex-guarded and its native calls
// serialize through the service's own locks, so methods are safe from
// any goroutine; the ownership contract still places the app's poll
// loop on the foreground thread (the native copies run under the
// renderer's device lock exactly once per poll).
type ScreenCaptureStream struct {
	svc    *native.CaptureService
	handle native.CaptureStreamHandle
	source ScreenCaptureSource
	// initialWidth/initialHeight are the capture item's size at stream
	// creation (CreateStream's layout answer).
	initialWidth  int32
	initialHeight int32

	mu        sync.Mutex
	current   ScreenCaptureFrame
	released  uint64
	stopped   bool
	destroyed bool
}

// ScreenCaptureStream starts capturing the monitor of the given source
// (the pinned ScreenCaptureSource::stream): the native producer is
// created ON THE RENDERER'S ACTUAL D3D11 DEVICE (the IDXGIDevice ->
// WinRT device chain; the ticket's same-device criterion) and the
// capture session starts immediately. The initial item size is
// available through InitialSize.
//
// Call it on the foreground thread. A zero monitor handle is rejected
// with the typed bad-value error before any native work.
func (a *App) ScreenCaptureStream(source ScreenCaptureSource) (*ScreenCaptureStream, error) {
	svc, err := a.CaptureService()
	if err != nil {
		return nil, err
	}
	if source.HMonitor == 0 {
		return nil, fmt.Errorf("gpui: ScreenCaptureStream: %w (a zero monitor handle)", native.ErrCaptureBadValue)
	}
	handle, width, height, err := svc.CreateStream(source.HMonitor)
	if err != nil {
		return nil, fmt.Errorf("gpui: ScreenCaptureStream: %w", err)
	}
	return &ScreenCaptureStream{
		svc:           svc,
		handle:        handle,
		source:        source,
		initialWidth:  width,
		initialHeight: height,
	}, nil
}

// Source returns the source metadata this stream captures (the pinned
// ScreenCaptureStream::metadata).
func (st *ScreenCaptureStream) Source() ScreenCaptureSource { return st.source }

// InitialSize returns the capture item's device-pixel size at stream
// creation (the native CreateStream's layout answer; the pool is
// created at this size).
func (st *ScreenCaptureStream) InitialSize() (int32, int32) {
	return st.initialWidth, st.initialHeight
}

// Poll drains the bounded pending queue (the foreground checkout/copy:
// each pending frame is copied under the renderer's device lock into
// an immutable destination texture) and returns the drained frames in
// arrival order. The newest drained frame becomes the current frame;
// every superseded frame is released (the bounded published set of
// one). An empty queue drains nothing and returns no frames; a
// stopped, drained stream reports ErrCaptureStopped.
//
// A drain that published frames may also report a release error for a
// superseded frame — the frames are still returned alongside it.
func (st *ScreenCaptureStream) Poll() ([]ScreenCaptureFrame, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.destroyed {
		return nil, fmt.Errorf("gpui: capture stream poll: %w", native.ErrCaptureStaleHandle)
	}
	// The capacity protocol: probe with a nil buffer (the native side
	// reports the pending depth and drains nothing).
	_, need, err := st.svc.Poll(st.handle, nil)
	if err != nil {
		if !errors.Is(err, native.ErrCaptureCapacity) {
			// The typed stopped/bad-handle/stale records.
			return nil, fmt.Errorf("gpui: capture stream poll: %w", err)
		}
		if need == 0 || need > 1<<16 {
			return nil, fmt.Errorf("gpui: capture stream poll: %w", native.ErrCaptureCapacity)
		}
	} else if need == 0 {
		// Nothing pending (the stopped-with-empty case already returned
		// the typed stopped error above).
		return nil, nil
	}
	// Drain with the reported capacity; the queue may have grown
	// between the probe and the drain, so a capacity result retries with
	// the new depth (bounded).
	frames := make([]native.CaptureFrameRecord, need)
	var published []native.CaptureFrameRecord
	for attempt := 0; ; attempt++ {
		var count uint32
		published, count, err = st.svc.Poll(st.handle, frames)
		if !errors.Is(err, native.ErrCaptureCapacity) {
			break
		}
		if attempt >= 3 || count <= uint32(len(frames)) || count > 1<<16 {
			return nil, fmt.Errorf("gpui: capture stream poll: %w", native.ErrCaptureCapacity)
		}
		frames = make([]native.CaptureFrameRecord, count)
	}
	if err != nil {
		return nil, fmt.Errorf("gpui: capture stream poll: %w", err)
	}
	out := make([]ScreenCaptureFrame, len(published))
	for i, rec := range published {
		out[i] = ScreenCaptureFrame{
			Handle:        rec.Handle,
			Width:         rec.Width,
			Height:        rec.Height,
			ContentWidth:  rec.ContentWidth,
			ContentHeight: rec.ContentHeight,
			Timestamp:     rec.Timestamp,
		}
	}
	var releaseErr error
	if len(out) > 0 {
		// The bounded published set: the newest drained frame is the
		// current (the one the app paints); every older frame of this
		// batch AND the previous current are released now — the previous
		// current was drawn before this poll in the app's frame loop, so
		// the release fires exactly when the scene stops referencing it.
		previous := st.current
		st.current = out[len(out)-1]
		for _, frame := range out[:len(out)-1] {
			if err := st.svc.FrameRelease(native.CaptureFrameHandle(frame.Handle)); err != nil && releaseErr == nil {
				releaseErr = fmt.Errorf("gpui: releasing a superseded capture frame: %w", err)
			}
			st.released++
		}
		if previous.Handle != 0 {
			if err := st.svc.FrameRelease(native.CaptureFrameHandle(previous.Handle)); err != nil && releaseErr == nil {
				releaseErr = fmt.Errorf("gpui: releasing the superseded capture frame: %w", err)
			}
			st.released++
		}
	}
	if releaseErr != nil {
		return out, releaseErr
	}
	return out, nil
}

// PollForFrame polls until the stream's current frame exists and
// differs from after (0 waits for the first frame), or the timeout
// expires (a bounded wait; the queue drops overflow, so this is a
// convenience, not a guarantee). A stopped stream still answers with
// its last drained current frame.
func (st *ScreenCaptureStream) PollForFrame(after uint64, timeout time.Duration) (ScreenCaptureFrame, error) {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := st.Poll(); err != nil && !errors.Is(err, native.ErrCaptureStopped) {
			return ScreenCaptureFrame{}, err
		}
		if current, ok := st.CurrentFrame(); ok && current.Handle != after {
			return current, nil
		}
		if time.Now().After(deadline) {
			return ScreenCaptureFrame{}, fmt.Errorf("gpui: no capture frame beyond %#x within %v", after, timeout)
		}
		time.Sleep(capturePollInterval)
	}
}

// CurrentFrame returns the stream's current published frame — the
// frame to paint. It stays resolvable until the next poll supersedes
// it; the renderer's surface-view cache holds its own texture
// reference while a scene that references it still draws.
func (st *ScreenCaptureStream) CurrentFrame() (ScreenCaptureFrame, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.destroyed || st.current.Handle == 0 {
		return ScreenCaptureFrame{}, false
	}
	return st.current, true
}

// Stats returns the stream's observability record: the bounded pending
// depth, the published count, the drop counter, the pool size changes
// and the Go model's released-frame counter.
func (st *ScreenCaptureStream) Stats() (CaptureStreamStats, error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.destroyed {
		return CaptureStreamStats{}, fmt.Errorf("gpui: capture stream stats: %w", native.ErrCaptureStaleHandle)
	}
	rec, err := st.svc.StreamStats(st.handle)
	if err != nil {
		return CaptureStreamStats{}, fmt.Errorf("gpui: capture stream stats: %w", err)
	}
	return CaptureStreamStats{
		Pending:        rec.Pending,
		Published:      rec.Published,
		Dropped:        rec.Dropped,
		SizeChanges:    rec.SizeChanges,
		Stopped:        rec.Stopped == 1,
		PoolWidth:      rec.PoolWidth,
		PoolHeight:     rec.PoolHeight,
		LastErrorHR:    rec.LastErrorHR,
		FramesReleased: st.released,
	}, nil
}

// Stop stops the capture session (the pinned CaptureStream drop ->
// capture.stop()): late frame arrivals are refused, frames already
// pending remain drainable (one final Poll returns them), and a poll
// of the stopped, drained stream reports ErrCaptureStopped. Stopping
// an already-stopped stream is a no-op.
func (st *ScreenCaptureStream) Stop() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.destroyed {
		return fmt.Errorf("gpui: capture stream stop: %w", native.ErrCaptureStaleHandle)
	}
	if err := st.svc.StopStream(st.handle); err != nil {
		return fmt.Errorf("gpui: capture stream stop: %w", err)
	}
	st.stopped = true
	return nil
}

// Destroy drops the session, frame pool and item and releases every
// published frame texture (the native destroy is the whole teardown,
// including the apartment-affine WinRT releases). The stream is
// unusable afterwards; every method reports ErrCaptureStaleHandle.
// Destroy after Stop is the normal shutdown order.
func (st *ScreenCaptureStream) Destroy() error {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.destroyed {
		return fmt.Errorf("gpui: capture stream destroy: %w", native.ErrCaptureStaleHandle)
	}
	if err := st.svc.DestroyStream(st.handle); err != nil {
		return fmt.Errorf("gpui: capture stream destroy: %w", err)
	}
	st.destroyed = true
	st.stopped = true
	// The native destroy released the published texture; the current
	// frame's handle is no longer resolvable, so stop advertising it.
	st.current = ScreenCaptureFrame{}
	return nil
}

// ---------------------------------------------------------------------------
// ObjectFit (style.rs 25-141)
// ---------------------------------------------------------------------------

// ObjectFit is how a source's content fits into the bounds of an
// element (the pinned ObjectFit).
type ObjectFit uint8

// The object-fit modes.
const (
	// ObjectFitFill stretches the content to fill the element bounds.
	ObjectFitFill ObjectFit = iota
	// ObjectFitContain scales the content to fit within the element
	// bounds, centered.
	ObjectFitContain
	// ObjectFitCover scales the content to cover the element bounds,
	// centered (the overflow crops through the content mask).
	ObjectFitCover
	// ObjectFitScaleDown scales the content down to fit (never up),
	// centered.
	ObjectFitScaleDown
	// ObjectFitNone keeps the content's original size at the element
	// origin.
	ObjectFitNone
)

// GetBounds returns the bounds of the content within the given bounds
// (the pinned ObjectFit::get_bounds): the content size is the capture
// frame's content size (the crop), treated as logical pixels exactly
// like the pin's Pixels::from(DevicePixels). Fill, contain, cover and
// scale-down match the pin's (and CSS's) math; none is centred instead
// of sitting at the origin (CSS object-position 50% 50%; see the
// default case). Degenerate inputs (non-positive content or bounds
// size) collapse to a zero-size bounds at the element origin instead
// of the pin's NaN propagation — a recorded guard, the pinned math is
// otherwise unchanged.
func (f ObjectFit) GetBounds(bounds Bounds, contentWidth, contentHeight int32) Bounds {
	if contentWidth <= 0 || contentHeight <= 0 || bounds.Size.Width <= 0 || bounds.Size.Height <= 0 {
		return Bounds{Origin: bounds.Origin}
	}
	imageWidth := float32(contentWidth)
	imageHeight := float32(contentHeight)
	imageRatio := imageWidth / imageHeight
	boundsRatio := bounds.Size.Width / bounds.Size.Height

	containSize := func() (float32, float32) {
		if boundsRatio > imageRatio {
			return imageWidth * (bounds.Size.Height / imageHeight), bounds.Size.Height
		}
		return bounds.Size.Width, imageHeight * (bounds.Size.Width / imageWidth)
	}
	center := func(width, height float32) Bounds {
		return Bounds{
			Origin: Point{
				X: bounds.Origin.X + (bounds.Size.Width-width)/2.0,
				Y: bounds.Origin.Y + (bounds.Size.Height-height)/2.0,
			},
			Size: Size{Width: width, Height: height},
		}
	}

	switch f {
	case ObjectFitFill:
		return bounds
	case ObjectFitContain:
		width, height := containSize()
		return center(width, height)
	case ObjectFitScaleDown:
		// Scale down only: an image already fitting keeps its original
		// size, centered.
		if imageWidth > bounds.Size.Width || imageHeight > bounds.Size.Height {
			width, height := containSize()
			return center(width, height)
		}
		return center(imageWidth, imageHeight)
	case ObjectFitCover:
		var width, height float32
		if boundsRatio > imageRatio {
			width = bounds.Size.Width
			height = imageHeight * (bounds.Size.Width / imageWidth)
		} else {
			width = imageWidth * (bounds.Size.Height / imageHeight)
			height = bounds.Size.Height
		}
		return center(width, height)
	default: // ObjectFitNone
		// CSS centres object-fit: none: object-position defaults to 50%
		// 50%, and the none mode places the content at its intrinsic size
		// at that position (Chromium's ComputeObjectFitAndPositionRect
		// resolves the position against base - object_size for every
		// mode, layout_replaced.cc:453-471). Recorded divergence toward
		// CSS: the pin pins it at the bounds' origin (style.rs:134-137).
		return center(imageWidth, imageHeight)
	}
}

// ---------------------------------------------------------------------------
// The Surface element (elements/surface.rs 74-141)
// ---------------------------------------------------------------------------

// SurfaceSource is the source of a surface element's content (the
// pinned SurfaceSource variants this port can express: a native
// Windows Graphics Capture frame, or the pinned Unsupported stand-in
// with a size).
type SurfaceSource struct {
	// frame is the capture frame source (SurfaceSource::WindowsCapture
	// / From<ScreenCaptureFrame>); nil for the unsupported stand-in.
	frame *ScreenCaptureFrame
	// unsupportedWidth/unsupportedHeight carry the pinned
	// SurfaceSource::Unsupported(size) payload.
	unsupportedWidth  int32
	unsupportedHeight int32
}

// CaptureSurfaceSource builds the capture-frame surface source (the
// pinned From<WindowsScreenCaptureFrame> for SurfaceSource).
func CaptureSurfaceSource(frame ScreenCaptureFrame) SurfaceSource {
	return SurfaceSource{frame: &frame}
}

// UnsupportedSurfaceSource builds the pinned SurfaceSource::Unsupported
// stand-in with its device-pixel size.
func UnsupportedSurfaceSource(width, height int32) SurfaceSource {
	return SurfaceSource{unsupportedWidth: width, unsupportedHeight: height}
}

// Size returns the source's content size in device pixels (the pinned
// SurfaceSource::size): the capture frame's CONTENT size (the caller's
// crop — the native frame record carries it next to the pool texture
// size; in the steady state the pool Recreate protocol keeps them
// equal), or the unsupported stand-in's declared size.
func (src SurfaceSource) Size() (int32, int32) {
	if src.frame != nil {
		return src.frame.ContentWidth, src.frame.ContentHeight
	}
	return src.unsupportedWidth, src.unsupportedHeight
}

// SurfaceElement is a surface element (the pinned elements/surface.rs
// Surface; the concrete spelling because the renderer's Surface type
// already owns the package-level identifier). Build one with
// NewSurfaceElement and convert it with IntoElement — the fluent
// builder and the drawable are separate types exactly like the port's
// Svg element.
type SurfaceElement struct {
	source    SurfaceSource
	objectFit ObjectFit
	style     Style
	elementID *string
}

// NewSurfaceElement creates a surface element drawing the given source
// (the pinned surface() constructor): the object fit defaults to
// Contain.
func NewSurfaceElement(source SurfaceSource) *SurfaceElement {
	return &SurfaceElement{
		source:    source,
		objectFit: ObjectFitContain,
		style:     DefaultStyle(),
	}
}

// ObjectFit sets how the source's content fits into the element bounds
// (the pinned Surface::object_fit).
func (e *SurfaceElement) ObjectFit(fit ObjectFit) *SurfaceElement {
	e.objectFit = fit
	return e
}

// ID assigns this element a string identity (the interactivity element
// id): identity participates in the global element id path.
func (e *SurfaceElement) ID(value string) *SurfaceElement {
	e.elementID = &value
	return e
}

// Size sets the element's preferred width and height (the Styled size
// surface the interactivity base style provides).
func (e *SurfaceElement) Size(width, height Length) *SurfaceElement {
	e.style.Size = LengthSize{Width: width, Height: height}
	return e
}

// W sets the element's preferred width (Styled::w).
func (e *SurfaceElement) W(width Length) *SurfaceElement {
	e.style.Size.Width = width
	return e
}

// H sets the element's preferred height (Styled::h).
func (e *SurfaceElement) H(height Length) *SurfaceElement {
	e.style.Size.Height = height
	return e
}

// surfaceDrawable adapts the builder into the Element phases (the
// reference Drawable<E> shape).
type surfaceDrawable struct {
	surface *SurfaceElement
}

// ID implements Element.
func (d *surfaceDrawable) ID() (ElementID, bool) {
	if d.surface.elementID == nil {
		return ElementID{}, false
	}
	return NameElementID(*d.surface.elementID), true
}

// SourceLocation implements Element.
func (d *surfaceDrawable) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element (surface.rs request_layout): one
// layout node under this element's style, no children and no intrinsic
// measurement — the element is sized by its style exactly like the
// pin's window.request_layout(style, [], cx).
func (d *surfaceDrawable) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	layoutID, err := requestLayoutOf(w, d.surface.style)
	if err != nil {
		panic(fmt.Sprintf("gpui: SurfaceElement request_layout: %v", err))
	}
	return layoutID, struct{}{}
}

// Prepaint implements Element (surface.rs prepaint: nothing to
// prepare).
func (d *surfaceDrawable) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) struct{} {
	return struct{}{}
}

// Paint implements Element (surface.rs paint): the object-fit bounds
// of the source's content size inside the element bounds, then the
// surface paint through the window's current frame at the ambient
// element opacity. The capture-frame source paints the tag-1 surface
// record carrying the frame handle; the unsupported stand-in paints
// the tag-0 record the renderer's draw path bails on explicitly (the
// pinned "cannot import this surface source"). Paint errors are logged
// and painting continues — the pinned element surface has no error
// path, so this mirrors the port's Svg element's log_err discipline.
func (d *surfaceDrawable) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *struct{}, w *Window, app *App) {
	s := d.surface
	contentWidth, contentHeight := s.source.Size()
	newBounds := s.objectFit.GetBounds(bounds, contentWidth, contentHeight)
	frame := currentFrame(w)
	ctx := frame.paint
	if source := s.source.frame; source != nil {
		if err := frame.scene.PaintCaptureSurface(newBounds, *source, ctx); err != nil {
			log.Printf("gpui: %v", err)
		}
		return
	}
	if err := frame.scene.PaintSurface(newBounds, SurfaceSourceUnsupported, ctx); err != nil {
		log.Printf("gpui: %v", err)
	}
}

// LayoutNodeStyle implements the root-stretch report.
func (d *surfaceDrawable) LayoutNodeStyle() (Style, bool) { return d.surface.style, true }

// IntoElement implements IntoElement.
func (e *SurfaceElement) IntoElement() AnyElement {
	return CustomElement[struct{}, struct{}](&surfaceDrawable{surface: e})
}
