package gpui

import (
	"errors"
	"fmt"

	"gpui-go/internal/native"
)

// This file is the port's surface half of ticket16 and ticket28: the
// pinned Window::paint_surface record construction (window.rs:5180-
// 5196) over the scene kernel's surface primitive (the paired-opacity
// insertion of scene.go), the source-kind extension (the pinned
// SurfaceSource::WindowsCapture import the renderer's draw path
// resolves through the capture service), and the capture-frame paint
// entry carrying the frame handle in the surface record's source
// payload. The ticket16 state is preserved: the ABI surface record
// still carries the pinned SurfaceSource::Unsupported stand-in (tag
// 0) whose draw path bails explicitly; tag 1 carries a native capture
// frame handle (the pinned WindowsCapture import).

// SurfaceSourceKind tags the surface source record (the pinned
// SurfaceSource variants the ABI can express).
type SurfaceSourceKind uint32

// Surface source kinds.
const (
	// SurfaceSourceUnsupported is the pinned placeholder for platforms
	// that cannot import native surfaces (SurfaceSource::Unsupported).
	// The pinned renderer's draw path bails on it explicitly.
	SurfaceSourceUnsupported SurfaceSourceKind = 0
	// SurfaceSourceCaptureFrame is a native Windows Graphics Capture
	// frame handle (the pinned SurfaceSource::WindowsCapture import,
	// surface.rs From<WindowsScreenCaptureFrame>): the renderer's draw
	// path resolves the handle through the capture service under its
	// device lock and imports the frame's destination texture directly
	// (the same-device adapter). The frame handle travels in the
	// surface record's 32-bit source payload word.
	SurfaceSourceCaptureFrame SurfaceSourceKind = 1
)

// The capture-frame surface source payload errors.
var (
	// ErrCaptureFrameHandleZero reports a capture surface painted with
	// no published frame handle (the scene ABI rejects a zero payload).
	ErrCaptureFrameHandleZero = errors.New("gpui: a capture surface requires a published frame handle")
	// ErrCaptureFrameHandlePayload reports a capture frame handle that
	// cannot travel through the scene surface record's source payload:
	// the frozen ABI carries the handle in one 32-bit reserved word, so
	// only handles with no bits above 32 (slot-0 streams) are paintable.
	// The native slot registry appends a new slot per created stream
	// until sixteen exist, so in a fresh process only the first stream
	// ever created is paintable (a second concurrently live stream, or a
	// later one before sixteen slots exist, lands in an unpaintable
	// slot).
	ErrCaptureFrameHandlePayload = errors.New("gpui: the capture frame handle does not fit the surface record's 32-bit source payload")
)

// PaintSurface paints a surface into the scene at the current z-index
// (the pinned Window::paint_surface, window.rs:5180-5196): snapped
// bounds, the snapped content mask, and the insertion carrying the
// element opacity as the surface's paired opacity (the kernel keeps
// the pairing through the finish sort; the pinned scene stores it in
// the parallel surface_opacities array). This entry paints the
// unsupported source kind (the tag-0 stand-in record); the
// capture-frame entry is PaintCaptureSurface.
func (s *Scene) PaintSurface(bounds Bounds, source SurfaceSourceKind, ctx *PaintContext) error {
	if ctx == nil {
		return fmt.Errorf("gpui: PaintSurface: nil paint context")
	}
	if source != SurfaceSourceUnsupported {
		return fmt.Errorf("gpui: PaintSurface: unknown surface source kind %d (the capture-frame source goes through PaintCaptureSurface)", source)
	}
	// window.rs paint_surface: snap_bounds (NOT cover_bounds), then the
	// snapped content mask.
	snappedBounds := snapBounds(bounds, ctx.ScaleFactor)
	mask := snappedContentMask(ctx.Mask, ctx.ScaleFactor)
	return s.kernel.InsertSurface(snappedBounds, mask, SurfaceSourceTag(source), ctx.ElementOpacity)
}

// PaintCaptureSurface paints a captured frame's texture into the
// scene at the current z-index (ticket28: the pinned
// Window::paint_surface with a SurfaceSource::WindowsCapture source):
// snapped bounds, the snapped content mask, and the tag-1 surface
// record carrying the frame handle in the source payload with the
// element opacity as the surface's paired opacity. The renderer's
// draw path resolves the handle through the capture service (its
// immutable destination texture — the same-device adapter) and
// imports it directly; the surface-view cache keeps its own texture
// reference while a scene that references this handle still draws.
//
// FROZEN ARTIFACT RECORD (rev-10): the scene service's export entry
// scene_insert_surface pre-validates every surface record with a
// zero-reserved-words requirement that predates ticket28, so this
// insert is currently rejected with the scene bad-value error even
// though the state-level insert_surface validation (the ticket28
// payload rules: tag 1 with the handle in reserved[0] and the other
// words zero) and the renderer's draw path both accept exactly this
// record. The Go half builds the documented payload (the ABI's state
// level and the renderer both expect it); the blocked gate is the
// native-side one-line fix (check_surface in scene.rs) recorded for
// the next approved DLL rebuild, after which this method works
// unchanged and the full capture→draw loop lands.
//
// The frame must be the stream's current frame: the handle stays
// resolvable only while the capture service keeps it published, and
// the stream releases superseded frames at the poll that replaces
// them (capture.go's bounded published set). Drawing a scene whose
// frame was already released fails with the renderer's typed
// bad-handle error — the explicit record, never a silent blank
// surface.
func (s *Scene) PaintCaptureSurface(bounds Bounds, frame ScreenCaptureFrame, ctx *PaintContext) error {
	if ctx == nil {
		return fmt.Errorf("gpui: PaintCaptureSurface: nil paint context")
	}
	if frame.Handle == 0 {
		return fmt.Errorf("gpui: PaintCaptureSurface: %w", ErrCaptureFrameHandleZero)
	}
	// The scene ABI carries the frame handle in a single 32-bit
	// reserved word (source_reserved[0]; the remaining words must stay
	// zero), so a handle with bits above 32 would be corrupted in
	// transit — reject it before insertion.
	if frame.Handle > 0xFFFFFFFF {
		return fmt.Errorf("gpui: PaintCaptureSurface: frame handle %#x: %w", frame.Handle, ErrCaptureFrameHandlePayload)
	}
	if s.kernel == nil || s.kernel.scene == nil {
		return fmt.Errorf("gpui: PaintCaptureSurface: %w", ErrSceneClosed)
	}
	// window.rs paint_surface: snap_bounds (NOT cover_bounds), then the
	// snapped content mask.
	snappedBounds := snapBounds(bounds, ctx.ScaleFactor)
	mask := snappedContentMask(ctx.Mask, ctx.ScaleFactor)
	rec := native.NewSceneSurfaceRecord()
	rec.Bounds = sceneBoundsRecord(snappedBounds)
	rec.ContentMask = sceneBoundsRecord(mask)
	rec.SourceTag = uint32(SurfaceSourceCaptureFrame)
	rec.SourceReserved[0] = uint32(frame.Handle)
	if err := s.kernel.scene.InsertSurface(&rec, ctx.ElementOpacity); err != nil {
		return fmt.Errorf("gpui: scene insert surface: %w", err)
	}
	return nil
}

// SnapBounds rounds a logical bounds to the device grid (the pinned
// window.rs snap_bounds: each edge to the device grid, far edges
// clamped to at least the near edges). Exported for paint-path ports
// (surface and sprite record construction) that run outside this
// package's internal helpers.
func SnapBounds(bounds Bounds, scale float32) Bounds {
	return snapBounds(bounds, scale)
}

// SnappedContentMask builds the snapped content-mask record of a paint
// call (the pinned snapped_content_mask: cover_bounds of the mask).
func SnappedContentMask(mask Bounds, scale float32) Bounds {
	return snappedContentMask(mask, scale)
}
