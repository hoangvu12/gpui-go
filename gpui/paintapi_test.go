package gpui

// Base ticket 08's tests of the exported element paint API (paintapi.go)
// and the canvas element (canvas.go), plus the shared fixture the
// deferred / anchored / img / animation tests use. Every test drives a
// deterministic test window (scale factor 2) and reads the finished
// scene's records — the scene transcripts of the ticket's acceptance.

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// paintView renders whatever its build function returns each frame.
type paintView struct {
	build func(v *paintView, w *Window, cx *Context[paintView]) AnyElement
	// renders counts the view's renders.
	renders int
}

func (v *paintView) Render(w *Window, cx *Context[paintView]) AnyElement {
	v.renders++
	return v.build(v, w, cx)
}

// paintFixture is one test app, window and root view.
type paintFixture struct {
	t      *testing.T
	ta     *TestApp
	app    *App
	window *Window
	view   Entity[paintView]
}

// newPaintFixture opens an 800x600 test window rendering build; the
// window's creation flush draws its first frame.
func newPaintFixture(t *testing.T, build func(v *paintView, w *Window, cx *Context[paintView]) AnyElement) *paintFixture {
	t.Helper()
	ta := NewTestApp()
	return newPaintFixtureIn(t, ta, build)
}

func newPaintFixtureIn(t *testing.T, ta *TestApp, build func(v *paintView, w *Window, cx *Context[paintView]) AnyElement) *paintFixture {
	t.Helper()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 800, Height: 600})
	view := NewEntity(app, window.Scope(), func(v *paintView, cx *Context[paintView]) { v.build = build })
	window.SetRootView(ViewOf(view))
	f := &paintFixture{t: t, ta: ta, app: app, window: window, view: view}
	t.Cleanup(func() {
		CloseTestWindow(window)
		releaseAppImageCodec(app)
	})
	return f
}

// releaseAppImageCodec disposes the application's image codec atlas: the
// native atlas service bounds live atlases at 8 per process, and the test
// apps of this package would otherwise exhaust the slots the real-window
// tests need.
func releaseAppImageCodec(app *App) {
	if app.imageCodec != nil {
		_ = app.imageCodec.Dispose()
		app.imageCodec = nil
	}
}

// draw draws one frame and returns its finished scene.
func (f *paintFixture) draw() *Scene {
	f.t.Helper()
	scene, err := DrawWindowFrame(f.window)
	if err != nil {
		f.t.Fatalf("DrawWindowFrame: %v", err)
	}
	return scene
}

// notify notifies the root view inside an update (its flush redraws the
// window).
func (f *paintFixture) notify() {
	f.view.Update(f.app, func(_ *paintView, cx *Context[paintView]) { cx.Notify() })
}

// dev builds device-pixel bounds.
func dev(x, y, w, h float32) Bounds {
	return Bounds{Origin: Point{X: x, Y: y}, Size: Size{Width: w, Height: h}}
}

// logical builds logical-pixel bounds.
func logical(x, y, w, h float32) Bounds { return dev(x, y, w, h) }

var (
	paintRed    = Hsla{H: 0, S: 1, L: 0.5, A: 1}
	paintBlue   = Hsla{H: 0.6, S: 1, L: 0.5, A: 1}
	paintGreen  = Hsla{H: 0.3, S: 1, L: 0.5, A: 1}
	paintYellow = Hsla{H: 0.15, S: 1, L: 0.5, A: 1}
	paintGray   = Hsla{H: 0, S: 0, L: 0.5, A: 1}
)

// solid is the stored form of a painted solid color (opacity 1).
func solid(c Hsla) Background { return SolidBackground(c).Opacity(1) }

// sceneQuads returns the finished scene's quads.
func sceneQuads(t *testing.T, scene *Scene) []Quad {
	t.Helper()
	quads, err := scene.Quads()
	if err != nil {
		t.Fatal(err)
	}
	return quads
}

// quadsOf returns the scene's quads with the background, in draw order.
func quadsOf(t *testing.T, scene *Scene, color Hsla) []Quad {
	t.Helper()
	var out []Quad
	for _, q := range sceneQuads(t, scene) {
		if q.Background == solid(color) {
			out = append(out, q)
		}
	}
	return out
}

// oneQuadOf returns the single quad with the background.
func oneQuadOf(t *testing.T, scene *Scene, color Hsla, what string) Quad {
	t.Helper()
	quads := quadsOf(t, scene, color)
	if len(quads) != 1 {
		t.Fatalf("%s: %d quads with %v, want 1 (scene %s)", what, len(quads), color, quadTranscript(t, scene))
	}
	return quads[0]
}

// quadTranscript renders the scene's quads (draw order) for diagnostics.
func quadTranscript(t *testing.T, scene *Scene) string {
	t.Helper()
	var parts []string
	for _, q := range sceneQuads(t, scene) {
		parts = append(parts, fmt.Sprintf("#%d %v bg=%v border=%v", q.Order, q.Bounds, q.Background.Solid, q.BorderColor.Solid))
	}
	return strings.Join(parts, " | ")
}

// TestPaintAPICanvasSceneTranscript pins the exported Window paint
// methods from a canvas paint closure (R:elements/canvas.rs:74-88; the
// R:window.rs paint methods): the canvas's own style box, then a filled
// quad, a border-only quad split into strips, drop and inset shadows,
// an underline, a strikethrough, a path and a layer — each record with
// the pinned device-pixel math at scale 2 — and the prepaint closure's
// result handed to paint with the committed bounds.
func TestPaintAPICanvasSceneTranscript(t *testing.T) {
	var prepaintBounds, paintBounds Bounds
	var handed string
	var viewport Size
	path, err := NewPathBuilder().MoveTo(10, 60).LineTo(30, 60).LineTo(20, 80).Close().Build()
	if err != nil {
		t.Fatalf("PathBuilder: %v", err)
	}
	f := newPaintFixture(t, func(_ *paintView, w *Window, _ *Context[paintView]) AnyElement {
		canvas := Canvas(func(bounds Bounds, w *Window, _ *App) string {
			prepaintBounds = bounds
			viewport = w.ViewportSize()
			return "prepaint-result"
		}, func(bounds Bounds, state string, w *Window, _ *App) {
			paintBounds, handed = bounds, state
			w.PaintDropShadows(bounds, Corners{}, []BoxShadow{
				NewBoxShadow(2, 2, SolidBackground(paintGray)).WithBlurRadius(4),
				NewBoxShadow(0, 0, SolidBackground(paintGray)).WithInset(),
			})
			w.PaintQuad(Fill(logical(20, 10, 30, 20), SolidBackground(paintBlue)))
			w.PaintInsetShadows(bounds, Corners{}, []BoxShadow{
				NewBoxShadow(2, 2, SolidBackground(paintGray)).WithBlurRadius(4),
				NewBoxShadow(0, 0, SolidBackground(paintGray)).WithSpreadRadius(1).WithInset(),
			})
			w.PaintQuad(Outline(bounds, SolidBackground(paintGreen), BorderStyleSolid))
			w.PaintUnderline(Point{X: 10, Y: 40}, 50, UnderlineStyle{Thickness: 1, Color: &paintBlue})
			w.PaintStrikethrough(Point{X: 10, Y: 20}, 50, StrikethroughStyle{Thickness: 1.5, Color: &paintGreen})
			w.PaintPath(path, SolidBackground(paintYellow))
			w.PaintLayer(logical(60, 10, 20, 20), func(w *Window) {
				w.PaintQuad(Fill(logical(60, 10, 20, 20), SolidBackground(paintYellow)))
			})
		}).W(Px(100)).H(Px(50)).Bg(paintRed)
		return Div().SizeFull().Child(Div().W(Px(10)).H(Px(10))).Child(canvas).IntoElement()
	})
	scene := f.draw()

	if want := logical(10, 0, 100, 50); prepaintBounds != want || paintBounds != want {
		t.Fatalf("canvas bounds prepaint %v paint %v, want %v", prepaintBounds, paintBounds, want)
	}
	if handed != "prepaint-result" {
		t.Fatalf("paint received %q, want the prepaint result", handed)
	}
	if viewport != (Size{Width: 800, Height: 600}) {
		t.Fatalf("ViewportSize = %v, want 800x600", viewport)
	}

	// The canvas's style box (Style::paint) precedes the closure's paint.
	bg := oneQuadOf(t, scene, paintRed, "canvas background")
	if bg.Bounds != dev(20, 0, 200, 100) {
		t.Fatalf("canvas background %v, want (20,0 200x100)", bg.Bounds)
	}
	fill := oneQuadOf(t, scene, paintBlue, "fill")
	if fill.Bounds != dev(40, 20, 60, 40) || fill.ContentMask != dev(0, 0, 1600, 1200) {
		t.Fatalf("fill quad %v mask %v", fill.Bounds, fill.ContentMask)
	}
	if !(bg.Order < fill.Order) {
		t.Fatalf("background order %d not below the fill's %d", bg.Order, fill.Order)
	}

	// The border-only outline splits into four strips (R:window.rs:4665-4709).
	var strips []Bounds
	for _, q := range sceneQuads(t, scene) {
		if q.BorderColor == solid(paintGreen) && q.Background.IsTransparent() {
			if q.Bounds != dev(20, 0, 200, 100) || q.BorderWidths != (Edges{Top: 2, Right: 2, Bottom: 2, Left: 2}) {
				t.Fatalf("outline strip record %v widths %v", q.Bounds, q.BorderWidths)
			}
			strips = append(strips, q.ContentMask)
		}
	}
	sort.Slice(strips, func(i, j int) bool { return strips[i].String() < strips[j].String() })
	wantStrips := []Bounds{dev(20, 0, 200, 3), dev(20, 3, 3, 94), dev(20, 97, 200, 3), dev(217, 3, 3, 94)}
	sort.Slice(wantStrips, func(i, j int) bool { return wantStrips[i].String() < wantStrips[j].String() })
	if fmt.Sprint(strips) != fmt.Sprint(wantStrips) {
		t.Fatalf("outline strips %v, want %v", strips, wantStrips)
	}

	// Drop then inset shadows: one of each list's kind.
	shadows, err := scene.Shadows()
	if err != nil {
		t.Fatal(err)
	}
	if len(shadows) != 2 {
		t.Fatalf("shadows = %d, want 2 (one drop, one inset)", len(shadows))
	}
	var drop, inset *Shadow
	for i := range shadows {
		if shadows[i].Inset {
			inset = &shadows[i]
		} else {
			drop = &shadows[i]
		}
	}
	if drop == nil || drop.Bounds != dev(24, 4, 200, 100) || drop.BlurRadius != 8 {
		t.Fatalf("drop shadow %+v", drop)
	}
	if inset == nil || inset.Bounds != dev(22, 2, 196, 96) || inset.ElementBounds != dev(20, 0, 200, 100) {
		t.Fatalf("inset shadow %+v", inset)
	}

	// Underline (R:window.rs:4732) and strikethrough (R:window.rs:4771).
	underlines, err := scene.Underlines()
	if err != nil {
		t.Fatal(err)
	}
	if len(underlines) != 2 {
		t.Fatalf("underlines = %d, want 2", len(underlines))
	}
	var under, strike *Underline
	for i := range underlines {
		if underlines[i].Color == paintBlue {
			under = &underlines[i]
		} else {
			strike = &underlines[i]
		}
	}
	if under == nil || under.Bounds != dev(20, 80, 100, 2) || under.Thickness != 2 || under.Wavy {
		t.Fatalf("underline %+v", under)
	}
	if strike == nil || strike.Bounds != dev(20, 40, 100, 3) || strike.Thickness != 3 || strike.Wavy || strike.Color != paintGreen {
		t.Fatalf("strikethrough %+v", strike)
	}

	// The path scales to device pixels (R:window.rs:4715).
	paths, _, err := scene.Paths()
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %d, want 1", len(paths))
	}

	// The layer pushed once around its quad (R:window.rs:4339).
	meta, err := scene.Meta()
	if err != nil {
		t.Fatal(err)
	}
	if meta.LayerPushCount != 1 {
		t.Fatalf("layer pushes = %d, want 1", meta.LayerPushCount)
	}
	layered := oneQuadOf(t, scene, paintYellow, "layered quad")
	if layered.Bounds != dev(120, 20, 40, 40) {
		t.Fatalf("layered quad %v", layered.Bounds)
	}
}

// layoutProbe is a custom element recording its own layout id's
// Window.LayoutBounds at prepaint (R:window.rs:5303).
type layoutProbe struct {
	got   *Bounds
	given *Bounds
}

func (p *layoutProbe) ID() (ElementID, bool)           { return ElementID{}, false }
func (p *layoutProbe) SourceLocation() *SourceLocation { return nil }
func (p *layoutProbe) RequestLayout(_ *GlobalElementID, _ *InspectorElementID, w *Window, _ *App) (LayoutID, LayoutID) {
	style := DefaultStyle()
	style.Size = LengthSize{Width: PxLength(30), Height: PxLength(40)}
	id, err := RequestElementLayout(w, style)
	if err != nil {
		panic(err)
	}
	return id, id
}
func (p *layoutProbe) Prepaint(_ *GlobalElementID, _ *InspectorElementID, bounds Bounds, id *LayoutID, w *Window, _ *App) struct{} {
	*p.got, *p.given = w.LayoutBounds(*id), bounds
	return struct{}{}
}
func (p *layoutProbe) Paint(*GlobalElementID, *InspectorElementID, Bounds, *LayoutID, *struct{}, *Window, *App) {
}

// TestPaintAPILayoutBoundsAndClientInset pins Window.LayoutBounds against
// the drawable's committed bounds (both window.layout_bounds) under a
// parent offset, and the client inset record (R:window.rs:2822-2830).
func TestPaintAPILayoutBoundsAndClientInset(t *testing.T) {
	var got, given Bounds
	f := newPaintFixture(t, func(_ *paintView, w *Window, _ *Context[paintView]) AnyElement {
		return Div().SizeFull().Child(Div().W(Px(25)).H(Px(5))).Child(CustomElement[LayoutID, struct{}](&layoutProbe{got: &got, given: &given})).IntoElement()
	})
	f.draw()
	if got != given || got != logical(25, 0, 30, 40) {
		t.Fatalf("LayoutBounds = %v, drawable bounds %v, want (25,0 30x40)", got, given)
	}
	if _, ok := f.window.ClientInset(); ok {
		t.Fatal("a fresh window reports a client inset")
	}
	f.window.SetClientInset(4)
	if inset, ok := f.window.ClientInset(); !ok || inset != 4 {
		t.Fatalf("ClientInset = %v, %v", inset, ok)
	}
	if f.window.ViewportSize() != (Size{Width: 800, Height: 600}) {
		t.Fatalf("ViewportSize outside a draw = %v", f.window.ViewportSize())
	}
}

// TestPaintAPIDragPreviewPainted pins the active-drag preview in a test
// window (R:window.rs:3451-3456 prepaint, 3474-3475 paint; ticket 06's
// prepaintActiveDragPreview / paintActiveDragPreview wired into
// drawRoots): the drag's view paints after the root at
// mouse_position - cursor_offset, above the main scene.
func TestPaintAPIDragPreviewPainted(t *testing.T) {
	f := newPaintFixture(t, func(_ *paintView, w *Window, _ *Context[paintView]) AnyElement {
		return Div().SizeFull().Bg(paintGray).IntoElement()
	})
	preview := NewEntity(f.app, f.window.Scope(), func(v *paintView, _ *Context[paintView]) {
		v.build = func(*paintView, *Window, *Context[paintView]) AnyElement {
			return Div().W(Px(40)).H(Px(20)).Bg(paintBlue).IntoElement()
		}
	})
	f.draw()
	f.window.DispatchInput(&MouseMoveEvent{Position: Point{X: 100, Y: 80}}, f.app)
	f.app.Update(func(app *App) {
		app.StartDrag(AnyDrag{Value: "payload", View: ViewOf(preview), CursorOffset: Point{X: 10, Y: 5}})
	})
	scene := f.draw()
	quad := oneQuadOf(t, scene, paintBlue, "drag preview")
	if quad.Bounds != dev(180, 150, 80, 40) {
		t.Fatalf("drag preview at %v, want (180,150 80x40) = (mouse - cursor offset) x 2", quad.Bounds)
	}
	root := oneQuadOf(t, scene, paintGray, "root")
	if !(root.Order < quad.Order) {
		t.Fatalf("preview order %d not above the root's %d", quad.Order, root.Order)
	}
	f.app.Update(func(app *App) { app.StopDrag(f.window) })
	scene = f.draw()
	if n := len(quadsOf(t, scene, paintBlue)); n != 0 {
		t.Fatalf("preview quads after the drag ended = %d, want 0", n)
	}
}

// TestPaintAPIFrameResetsCursorStyle pins reset_cursor_style at frame
// completion (R:window.rs:3277): a drawn frame resolves the window cursor
// from its cursor requests (none here: the arrow).
func TestPaintAPIFrameResetsCursorStyle(t *testing.T) {
	f := newPaintFixture(t, func(_ *paintView, w *Window, _ *Context[paintView]) AnyElement {
		return Div().SizeFull().IntoElement()
	})
	f.draw()
	if style, ok := resolvedCursorStyle(f.window); !ok || style != CursorArrow {
		t.Fatalf("resolved cursor after a frame = %v, %v; want the arrow", style, ok)
	}
}
