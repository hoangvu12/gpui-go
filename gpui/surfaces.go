package gpui

import "fmt"

// This file is the port's surface half of ticket16: the pinned
// Window::paint_surface record construction (window.rs:5180-5196) over
// the scene kernel's surface primitive (the paired-opacity insertion
// of scene.go). The pinned surface source on Windows is
// SurfaceSource::WindowsCapture (imported by the renderer); the port's
// ABI surface record carries the pinned SurfaceSource::Unsupported
// stand-in, and the renderer's draw path follows the pinned import
// error for it until the capture-producer slice lands.

// SurfaceSourceKind tags the surface source record (the pinned
// SurfaceSource variants the ABI can express).
type SurfaceSourceKind uint32

// Surface source kinds.
const (
	// SurfaceSourceUnsupported is the pinned placeholder for platforms
	// that cannot import native surfaces (SurfaceSource::Unsupported).
	// The pinned renderer's draw path bails on it explicitly.
	SurfaceSourceUnsupported SurfaceSourceKind = 0
)

// PaintSurface paints a surface into the scene at the current z-index
// (the pinned Window::paint_surface, window.rs:5180-5196): snapped
// bounds, the snapped content mask, and the insertion carrying the
// element opacity as the surface's paired opacity (the kernel keeps
// the pairing through the finish sort; the pinned scene stores it in
// the parallel surface_opacities array).
func (s *Scene) PaintSurface(bounds Bounds, source SurfaceSourceKind, ctx *PaintContext) error {
	if ctx == nil {
		return fmt.Errorf("gpui: PaintSurface: nil paint context")
	}
	// window.rs paint_surface: snap_bounds (NOT cover_bounds), then the
	// snapped content mask.
	snappedBounds := snapBounds(bounds, ctx.ScaleFactor)
	mask := snappedContentMask(ctx.Mask, ctx.ScaleFactor)
	return s.kernel.InsertSurface(snappedBounds, mask, SurfaceSourceTag(source), ctx.ElementOpacity)
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
