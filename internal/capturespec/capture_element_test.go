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

	"gpui-go/gpui"
	"gpui-go/internal/native"
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
			name: "none keeps the original size at the element origin",
			fit:  gpui.ObjectFitNone,
			bounds: gpui.Bounds{
				Origin: gpui.Point{X: 5, Y: 7},
				Size:   gpui.Size{Width: 200, Height: 100},
			},
			contentWidth: 50, contentHeg: 25,
			want: gpui.Bounds{Origin: gpui.Point{X: 5, Y: 7}, Size: gpui.Size{Width: 50, Height: 25}},
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

// TestSurfaceElementCaptureFrameSourceFrozenGate records the frozen
// rev-10 answer for the capture-frame source: the element paints
// through PaintCaptureSurface, whose tag-1 payload record is rejected
// by the artifact's scene_insert_surface export (the pre-ticket28
// check_surface demands zero reserved words), so the element's frame
// paints no surface and the scene-level call surfaces the typed scene
// bad-value error. The state-level insert validation and the
// renderer's capture-surface draw path both accept exactly this
// record, so the approved native one-line fix (check_surface) flips
// this gate — the Go half needs no change.
func TestSurfaceElementCaptureFrameSourceFrozenGate(t *testing.T) {
	// The element path: the frame's paint error is logged (the pinned
	// element surface has no error path), leaving no surface record.
	scene := drawSurfaceWindow(t, func() gpui.AnyElement {
		return gpui.NewSurfaceElement(gpui.CaptureSurfaceSource(syntheticFrame(100, 100))).
			ID("surface").
			ObjectFit(gpui.ObjectFitContain).
			Size(gpui.PxLength(200), gpui.PxLength(100)).
			IntoElement()
	})
	records, _, err := scene.Surfaces()
	if err != nil {
		t.Fatalf("scene.Surfaces: %v", err)
	}
	if len(records) != 0 {
		t.Fatalf("the capture-frame element painted %d surfaces, want 0 (the frozen insert gate rejected the payload record)", len(records))
	}

	// The scene-level record: the typed bad-value error of the frozen
	// gate (a future approved rebuild replaces this with success and
	// the record's bounds assertions above become the capture frame's).
	direct, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("gpui.NewScene: %v", err)
	}
	t.Cleanup(func() { _ = direct.Dispose() })
	ctx := gpui.NewPaintContext(2, gpui.Bounds{Size: gpui.Size{Width: 200, Height: 100}}, 1)
	err = direct.PaintCaptureSurface(gpui.Bounds{Size: gpui.Size{Width: 200, Height: 100}}, syntheticFrame(100, 100), ctx)
	if !errors.Is(err, native.ErrSceneBadValue) {
		t.Fatalf("PaintCaptureSurface with the capture-frame payload = %v, want the frozen-gate scene bad-value error", err)
	}
	t.Logf("the frozen rev-10 gate: scene_insert_surface rejects the tag-1 payload record (%v); the state-level validation and the renderer's draw path both accept it — the native one-line fix lands the full loop", err)
}

// TestSurfaceElementPayloadGuards pins the Go-side payload guards: a
// zero handle and a handle that does not fit the scene ABI's 32-bit
// source payload are typed errors before any native work.
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
	outOfPayload := gpui.ScreenCaptureFrame{Handle: 1 << 44, ContentWidth: 100, ContentHeight: 100}
	if err := guardScene.PaintCaptureSurface(bounds, outOfPayload, ctx); !errors.Is(err, gpui.ErrCaptureFrameHandlePayload) {
		t.Fatalf("PaintCaptureSurface with a handle above 32 bits = %v, want the typed payload error", err)
	}
	if err := guardScene.PaintCaptureSurface(bounds, gpui.ScreenCaptureFrame{}, nil); err == nil {
		t.Fatal("PaintCaptureSurface with a nil paint context = nil, want the nil-context error")
	}
}
