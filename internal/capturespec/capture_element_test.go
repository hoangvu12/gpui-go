//go:build windows

// This file drives ticket28's ObjectFit math and the Surface element
// (the elements/surface.rs port) through the deterministic test
// window's full element phase pipeline into a real scene: the
// object-fit crop of the source's content size inside the element
// bounds, the snapped surface record, and the paired element opacity.
//
// The element's geometry gates run on the pinned Unsupported stand-in
// source (tag 0): the object-fit crop is identical for every source
// kind (the element fits SurfaceSource::size()), and the tag-0 record
// inserts through the frozen ABI. The capture-frame source's tag-1
// record is BLOCKED by the rev-10 artifact's scene_insert_surface
// export (its pre-ticket28 check_surface rejects any nonzero source
// payload even though the state-level validation and the renderer's
// draw path both accept exactly that record) — the dedicated
// frozen-gate test below records that answer honestly, and the
// pixel-level loop of capture_draw_test.go records the same gate. The
// real frame's pixel path lands when the native one-line fix ships in
// an approved rebuild.

package capturespec

import (
	"errors"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// closeFloat compares two logical floats at a 1e-4 tolerance (the
// pinned f32 math's exact values in these tables).
func closeFloat(a, b float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-4
}

func closeBounds(got, want gpui.Bounds) bool {
	return closeFloat(got.Origin.X, want.Origin.X) &&
		closeFloat(got.Origin.Y, want.Origin.Y) &&
		closeFloat(got.Size.Width, want.Size.Width) &&
		closeFloat(got.Size.Height, want.Size.Height)
}

// TestObjectFitGetBounds pins the pinned ObjectFit::get_bounds math on
// representative aspect combinations (style.rs 38-141): every case's
// expected value is hand-derived from the pinned formulas.
func TestObjectFitGetBounds(t *testing.T) {
	cases := []struct {
		name                     string
		fit                      gpui.ObjectFit
		bounds                   gpui.Bounds
		contentWidth, contentHeg int32
		want                     gpui.Bounds
	}{
		{
			name: "fill stretches to the element bounds",
			fit:  gpui.ObjectFitFill,
			bounds: gpui.Bounds{
				Origin: gpui.Point{X: 0, Y: 0},
				Size:   gpui.Size{Width: 200, Height: 100},
			},
			contentWidth: 100, contentHeg: 100,
			want: gpui.Bounds{Origin: gpui.Point{X: 0, Y: 0}, Size: gpui.Size{Width: 200, Height: 100}},
		},
		{
			name: "contain fits a square into a wide element, centered",
			fit:  gpui.ObjectFitContain,
			bounds: gpui.Bounds{
				Origin: gpui.Point{X: 0, Y: 0},
				Size:   gpui.Size{Width: 200, Height: 100},
			},
			contentWidth: 100, contentHeg: 100,
			want: gpui.Bounds{Origin: gpui.Point{X: 50, Y: 0}, Size: gpui.Size{Width: 100, Height: 100}},
		},
		{
			name: "contain fits a 2:1 content exactly into a 2:1 element",
			fit:  gpui.ObjectFitContain,
			bounds: gpui.Bounds{
				Origin: gpui.Point{X: 10, Y: 20},
				Size:   gpui.Size{Width: 100, Height: 50},
			},
			contentWidth: 400, contentHeg: 200,
			want: gpui.Bounds{Origin: gpui.Point{X: 10, Y: 20}, Size: gpui.Size{Width: 100, Height: 50}},
		},
		{
			name: "cover overflows a square in a wide element, centered and cropped by the mask",
			fit:  gpui.ObjectFitCover,
			bounds: gpui.Bounds{
				Origin: gpui.Point{X: 0, Y: 0},
				Size:   gpui.Size{Width: 200, Height: 100},
			},
			contentWidth: 100, contentHeg: 100,
			want: gpui.Bounds{Origin: gpui.Point{X: 0, Y: -50}, Size: gpui.Size{Width: 200, Height: 200}},
		},
		{
			name: "cover fits a 2:1 content exactly into a 2:1 element",
			fit:  gpui.ObjectFitCover,
			bounds: gpui.Bounds{
				Origin: gpui.Point{X: 10, Y: 20},
				Size:   gpui.Size{Width: 100, Height: 50},
			},
			contentWidth: 400, contentHeg: 200,
			want: gpui.Bounds{Origin: gpui.Point{X: 10, Y: 20}, Size: gpui.Size{Width: 100, Height: 50}},
		},
		{
			name: "scale-down shrinks an oversized content like contain",
			fit:  gpui.ObjectFitScaleDown,
			bounds: gpui.Bounds{
				Origin: gpui.Point{X: 10, Y: 20},
				Size:   gpui.Size{Width: 100, Height: 50},
			},
			contentWidth: 400, contentHeg: 200,
			want: gpui.Bounds{Origin: gpui.Point{X: 10, Y: 20}, Size: gpui.Size{Width: 100, Height: 50}},
		},
		{
			name: "scale-down keeps a fitting content at its original size, centered",
			fit:  gpui.ObjectFitScaleDown,
			bounds: gpui.Bounds{
				Origin: gpui.Point{X: 0, Y: 0},
				Size:   gpui.Size{Width: 200, Height: 100},
			},
			contentWidth: 50, contentHeg: 25,
			want: gpui.Bounds{Origin: gpui.Point{X: 75, Y: 37.5}, Size: gpui.Size{Width: 50, Height: 25}},
		},
		{
			// CSS centres object-fit: none at the default object-position
			// 50% 50% (Chromium's ComputeObjectFitAndPositionRect resolves
			// the position against base - object_size for every mode,
			// layout_replaced.cc:453-471): a recorded divergence toward CSS
			// from the pin, which pins none at the origin (style.rs:134-137).
			name: "none keeps the original size, centered",
			fit:  gpui.ObjectFitNone,
			bounds: gpui.Bounds{
				Origin: gpui.Point{X: 5, Y: 7},
				Size:   gpui.Size{Width: 200, Height: 100},
			},
			contentWidth: 50, contentHeg: 25,
			want: gpui.Bounds{Origin: gpui.Point{X: 80, Y: 44.5}, Size: gpui.Size{Width: 50, Height: 25}},
		},
	}
	for _, tc := range cases {
		got := tc.fit.GetBounds(tc.bounds, tc.contentWidth, tc.contentHeg)
		if !closeBounds(got, tc.want) {
			t.Errorf("%s: GetBounds = (%v %v), want (%v %v)", tc.name, got.Origin, got.Size, tc.want.Origin, tc.want.Size)
		}
	}

	// The degenerate guard: non-positive content collapses instead of
	// propagating NaN (the recorded adaptation).
	degenerate := gpui.ObjectFitContain.GetBounds(gpui.Bounds{Origin: gpui.Point{X: 3, Y: 4}, Size: gpui.Size{Width: 100, Height: 50}}, 0, 0)
	if degenerate.Size.Width != 0 || degenerate.Size.Height != 0 || degenerate.Origin.X != 3 || degenerate.Origin.Y != 4 {
		t.Errorf("the degenerate content fit = %+v, want a zero size at the element origin", degenerate)
	}
}

// surfaceRootView is a stateless window root building one element tree
// per frame (the svgspec fixture pattern).
type surfaceRootView struct {
	build func() gpui.AnyElement
}

// EntityID implements gpui.View: stateless recipe.
func (v *surfaceRootView) EntityID() (gpui.EntityID, bool) { return 0, false }

// RenderOnce implements gpui.View.
func (v *surfaceRootView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return v.build()
}

// syntheticFrame is a slot-0-shaped capture frame record (generation
// 1, sequence 1): the handle travels through the scene ABI's 32-bit
// payload.
func syntheticFrame(contentWidth, contentHeight int32) gpui.ScreenCaptureFrame {
	return gpui.ScreenCaptureFrame{
		Handle:        0x01000001,
		Width:         1920,
		Height:        1080,
		ContentWidth:  contentWidth,
		ContentHeight: contentHeight,
		Timestamp:     123456,
	}
}

// drawSurfaceWindow opens a 200x100 test window (scale 2.0) rendering
// the given root and draws one frame, returning the finished scene.
func drawSurfaceWindow(t *testing.T, build func() gpui.AnyElement) *gpui.Scene {
	t.Helper()
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 200, Height: 100})
	t.Cleanup(func() { gpui.CloseTestWindow(window) })
	window.SetRootView(&surfaceRootView{build: build})
	scene, err := gpui.DrawWindowFrame(window)
	if err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return scene
}

// TestSurfaceElementFitsContentAndPaintsTheRecord drives the Surface
// element through the element pipeline on the Unsupported stand-in
// source (the object-fit crop is source-kind-independent): the contain
// fit of a 100x100 content in a 200x100 element, snapped to the test
// window's device grid (scale 2), painted as a surface record with the
// div's 0.5 element opacity paired to it.
func TestSurfaceElementFitsContentAndPaintsTheRecord(t *testing.T) {
	scene := drawSurfaceWindow(t, func() gpui.AnyElement {
		return gpui.Div().
			Size(gpui.PxLength(200), gpui.PxLength(100)).
			Opacity(0.5).
			Child(gpui.NewSurfaceElement(gpui.UnsupportedSurfaceSource(100, 100)).
				ID("surface").
				ObjectFit(gpui.ObjectFitContain).
				Size(gpui.PxLength(200), gpui.PxLength(100))).
			IntoElement()
	})

	records, opacities, err := scene.Surfaces()
	if err != nil {
		t.Fatalf("scene.Surfaces: %v", err)
	}
	if len(records) != 1 || len(opacities) != 1 {
		t.Fatalf("the frame painted %d surfaces with %d opacities, want 1/1", len(records), len(opacities))
	}
	record := records[0]
	// The tag-0 stand-in source kind.
	if uint32(record.Source) != uint32(gpui.SurfaceSourceUnsupported) {
		t.Fatalf("the surface record's source tag = %d, want the unsupported stand-in kind %d", record.Source, gpui.SurfaceSourceUnsupported)
	}
	// Contain of a 100x100 content in a 200x100 element: (50, 0, 100x100)
	// logical, snapped at scale 2 → (100, 0, 200x200) device.
	wantBounds := gpui.Bounds{Origin: gpui.Point{X: 100, Y: 0}, Size: gpui.Size{Width: 200, Height: 200}}
	if record.Bounds != wantBounds {
		t.Fatalf("the surface record's bounds = %v %v, want %v %v (the contain fit snapped at scale 2)",
			record.Bounds.Origin, record.Bounds.Size, wantBounds.Origin, wantBounds.Size)
	}
	// The content mask is the snapped window bounds (cover bounds of
	// 200x100 at scale 2: 400x200 device).
	wantMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 400, Height: 200}}
	if record.ContentMask != wantMask {
		t.Fatalf("the surface record's content mask = %v %v, want %v %v", record.ContentMask.Origin, record.ContentMask.Size, wantMask.Origin, wantMask.Size)
	}
	// The div's element opacity is the surface's paired opacity.
	if opacities[0] != 0.5 {
		t.Fatalf("the surface's paired opacity = %v, want the div's 0.5", opacities[0])
	}
	meta, err := scene.Meta()
	if err != nil {
		t.Fatalf("scene.Meta: %v", err)
	}
	if meta.SurfaceCount != 1 {
		t.Fatalf("the frame's meta surface count = %d, want 1", meta.SurfaceCount)
	}
}

// TestSurfaceElementCoverCropsThroughTheMask pins the cover crop: the
// 100x100 content covers the 200x100 element with a 200x200 surface
// centered at (0, -50) logical → (0, -100, 400x400) device — the
// overflow is cropped by the snapped content mask (400x200), exactly
// the pinned overflow-cropping path.
func TestSurfaceElementCoverCropsThroughTheMask(t *testing.T) {
	scene := drawSurfaceWindow(t, func() gpui.AnyElement {
		return gpui.NewSurfaceElement(gpui.UnsupportedSurfaceSource(100, 100)).
			ID("surface").
			ObjectFit(gpui.ObjectFitCover).
			Size(gpui.PxLength(200), gpui.PxLength(100)).
			IntoElement()
	})
	records, _, err := scene.Surfaces()
	if err != nil {
		t.Fatalf("scene.Surfaces: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("the frame painted %d surfaces, want 1", len(records))
	}
	record := records[0]
	wantBounds := gpui.Bounds{Origin: gpui.Point{X: 0, Y: -100}, Size: gpui.Size{Width: 400, Height: 400}}
	if record.Bounds != wantBounds {
		t.Fatalf("the cover surface bounds = %v %v, want %v %v", record.Bounds.Origin, record.Bounds.Size, wantBounds.Origin, wantBounds.Size)
	}
	wantMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 400, Height: 200}}
	if record.ContentMask != wantMask {
		t.Fatalf("the cover surface's content mask = %v %v, want the snapped window mask %v %v (the crop)",
			record.ContentMask.Origin, record.ContentMask.Size, wantMask.Origin, wantMask.Size)
	}
}

// TestSurfaceElementCaptureFrameSourceRecord pins the capture-frame
// source record through the FIXED artifact (the rev-11 fix-up): the
// element paints through PaintCaptureSurface, whose tag-1 payload
// record (the 64-bit frame handle packed across the two reserved
// words) is accepted by the scene insert, so the element's frame
// paints exactly one surface record carrying the full handle and the
// pinned snapped bounds/mask. (The rev-10 artifact rejected the tag-1
// payload at the export gate — the frozen-gate record this test
// replaced.)
func TestSurfaceElementCaptureFrameSourceRecord(t *testing.T) {
	// The element path: one surface record carrying the frame handle.
	frame := syntheticFrame(100, 100)
	scene := drawSurfaceWindow(t, func() gpui.AnyElement {
		return gpui.NewSurfaceElement(gpui.CaptureSurfaceSource(frame)).
			ID("surface").
			ObjectFit(gpui.ObjectFitContain).
			Size(gpui.PxLength(200), gpui.PxLength(100)).
			IntoElement()
	})
	records, _, err := scene.Surfaces()
	if err != nil {
		t.Fatalf("scene.Surfaces: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("the capture-frame element painted %d surfaces, want 1 (the fixed insert accepts the tag-1 payload)", len(records))
	}
	record := records[0]
	if uint32(record.Source) != uint32(gpui.SurfaceSourceCaptureFrame) {
		t.Fatalf("the record's source tag = %d, want the capture-frame tag", record.Source)
	}
	if record.SourceHandle != frame.Handle {
		t.Fatalf("the record's source handle = %#x, want the frame handle %#x (the two-word packing)", record.SourceHandle, frame.Handle)
	}

	// The scene-level record: the same payload accepted directly.
	direct, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("gpui.NewScene: %v", err)
	}
	t.Cleanup(func() { _ = direct.Dispose() })
	ctx := gpui.NewPaintContext(2, gpui.Bounds{Size: gpui.Size{Width: 200, Height: 100}}, 1)
	if err := direct.PaintCaptureSurface(gpui.Bounds{Size: gpui.Size{Width: 200, Height: 100}}, frame, ctx); err != nil {
		t.Fatalf("PaintCaptureSurface with the capture-frame payload = %v, want nil (the fixed insert accepts the tag-1 record)", err)
	}
	if err := direct.Finish(); err != nil {
		t.Fatalf("direct.Finish: %v", err)
	}
	records, _, err = direct.Surfaces()
	if err != nil {
		t.Fatalf("direct.Surfaces: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("the direct scene painted %d surfaces, want 1", len(records))
	}
}

// TestSurfaceElementPayloadGuards pins the Go-side payload guards: a
// zero handle is a typed error before any native work, and a FULL
// 64-bit handle (bits above 32 — a higher-slot stream) now travels
// through the two reserved words since the rev-11 fix-up (the rev-10
// 32-bit guard is retired).
func TestSurfaceElementPayloadGuards(t *testing.T) {
	guardScene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("gpui.NewScene: %v", err)
	}
	t.Cleanup(func() { _ = guardScene.Dispose() })
	ctx := gpui.NewPaintContext(1, gpui.Bounds{Size: gpui.Size{Width: 100, Height: 100}}, 1)
	bounds := gpui.Bounds{Size: gpui.Size{Width: 100, Height: 100}}
	if err := guardScene.PaintCaptureSurface(bounds, gpui.ScreenCaptureFrame{}, ctx); !errors.Is(err, gpui.ErrCaptureFrameHandleZero) {
		t.Fatalf("PaintCaptureSurface with a zero handle = %v, want the typed zero-handle error", err)
	}
	if err := guardScene.PaintCaptureSurface(bounds, gpui.ScreenCaptureFrame{}, nil); err == nil {
		t.Fatal("PaintCaptureSurface with a nil paint context = nil, want the nil-context error")
	}
	// The full 64-bit handle (a higher-slot stream frame): accepted and
	// packed across the two reserved words.
	wideHandle, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("gpui.NewScene (wide): %v", err)
	}
	t.Cleanup(func() { _ = wideHandle.Dispose() })
	full := gpui.ScreenCaptureFrame{Handle: (5 << 44) | (7 << 24) | 9, ContentWidth: 100, ContentHeight: 100}
	if err := wideHandle.PaintCaptureSurface(bounds, full, ctx); err != nil {
		t.Fatalf("PaintCaptureSurface with a full 64-bit handle = %v, want nil (the two-word packing)", err)
	}
	if err := wideHandle.Finish(); err != nil {
		t.Fatalf("wideHandle.Finish: %v", err)
	}
	records, _, err := wideHandle.Surfaces()
	if err != nil {
		t.Fatalf("wideHandle.Surfaces: %v", err)
	}
	if len(records) != 1 || records[0].SourceHandle != full.Handle {
		t.Fatalf("the wide-handle record = %+v, want the handle %#x packed", records, full.Handle)
	}
}
