package gpui

// Base ticket 36: a drawable paints with the bounds it committed at
// prepaint (R:element.rs:537-545 stores them in ElementDrawPhase::Prepaint;
// Drawable::paint, 557-600, hands that stored value to Element::paint
// rather than querying the layout again). An element prepainted under an
// absolute offset (PrepaintAsRoot / PrepaintAt) and painted without
// re-applying it paints where it was prepainted.

import "testing"

// boundsProbe is a 30x20 element recording the bounds its prepaint and
// paint phases receive; it paints one quad over its paint bounds.
type boundsProbe struct {
	color             Hsla
	prepaint, painted Bounds
	prepaints, paints int
}

func (p *boundsProbe) ID() (ElementID, bool)           { return ElementID{}, false }
func (p *boundsProbe) SourceLocation() *SourceLocation { return nil }
func (p *boundsProbe) RequestLayout(_ *GlobalElementID, _ *InspectorElementID, w *Window, _ *App) (LayoutID, struct{}) {
	style := DefaultStyle()
	style.Size = LengthSize{Width: PxLength(30), Height: PxLength(20)}
	id, err := RequestElementLayout(w, style)
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}

func (p *boundsProbe) Prepaint(_ *GlobalElementID, _ *InspectorElementID, bounds Bounds, _ *struct{}, _ *Window, _ *App) struct{} {
	p.prepaint = bounds
	p.prepaints++
	return struct{}{}
}

func (p *boundsProbe) Paint(_ *GlobalElementID, _ *InspectorElementID, bounds Bounds, _ *struct{}, _ *struct{}, w *Window, _ *App) {
	p.painted = bounds
	p.paints++
	w.PaintQuad(Fill(bounds, SolidBackground(p.color)))
}

// offsetHost is a 10x10 element that prepaints its child as a root at an
// absolute origin (the AnyElement::prepaint_as_root / prepaint_at callers
// of the pin, e.g. window.rs:3451-3456) and paints it with a plain
// AnyElement::paint, without re-applying the offset — as the pin's
// callers do.
type offsetHost struct {
	origin Point
	asRoot bool
	child  AnyElement
}

func (h *offsetHost) ID() (ElementID, bool)           { return ElementID{}, false }
func (h *offsetHost) SourceLocation() *SourceLocation { return nil }
func (h *offsetHost) RequestLayout(_ *GlobalElementID, _ *InspectorElementID, w *Window, _ *App) (LayoutID, struct{}) {
	style := DefaultStyle()
	style.Size = LengthSize{Width: PxLength(10), Height: PxLength(10)}
	id, err := RequestElementLayout(w, style)
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}

func (h *offsetHost) Prepaint(_ *GlobalElementID, _ *InspectorElementID, _ Bounds, _ *struct{}, w *Window, app *App) struct{} {
	available := AvailableSize{Width: DefiniteAvailableSpace(100), Height: DefiniteAvailableSpace(100)}
	if h.asRoot {
		h.child.PrepaintAsRoot(h.origin, available, w, app)
	} else {
		h.child.LayoutAsRoot(available, w, app)
		h.child.PrepaintAt(h.origin, w, app)
	}
	return struct{}{}
}

func (h *offsetHost) Paint(_ *GlobalElementID, _ *InspectorElementID, _ Bounds, _ *struct{}, _ *struct{}, w *Window, app *App) {
	h.child.Paint(w, app)
}

func TestDrawablePaintsWithPrepaintBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		asRoot bool
	}{{"PrepaintAsRoot", true}, {"PrepaintAt", false}} {
		t.Run(tc.name, func(t *testing.T) {
			probe := &boundsProbe{color: paintBlue}
			f := newPaintFixture(t, func(_ *paintView, w *Window, _ *Context[paintView]) AnyElement {
				child := CustomElement[struct{}, struct{}](probe)
				host := CustomElement[struct{}, struct{}](&offsetHost{origin: Point{X: 50, Y: 60}, asRoot: tc.asRoot, child: child})
				return Div().SizeFull().Child(Div().W(Px(25)).H(Px(5))).Child(host).IntoElement()
			})
			scene := f.draw()
			if probe.prepaints == 0 || probe.paints == 0 {
				t.Fatalf("probe phases: %d prepaints, %d paints", probe.prepaints, probe.paints)
			}
			want := logical(50, 60, 30, 20)
			if probe.prepaint != want {
				t.Fatalf("prepaint bounds = %v, want %v (the absolute origin)", probe.prepaint, want)
			}
			if probe.painted != probe.prepaint {
				t.Fatalf("paint bounds = %v, want the prepaint bounds %v (R:element.rs:557-600)", probe.painted, probe.prepaint)
			}
			// Scale factor 2: the quad lands at the prepainted origin.
			if q := oneQuadOf(t, scene, paintBlue, "probe"); q.Bounds != dev(100, 120, 60, 40) {
				t.Fatalf("probe quad at %v, want (100,120 60x40)", q.Bounds)
			}
		})
	}
}

// A drawable prepainted under a relative offset and painted outside it
// (the pin's paint never re-reads the element offset) keeps its
// prepaint bounds too.
func TestDrawablePaintIgnoresPaintTimeOffset(t *testing.T) {
	probe := &boundsProbe{color: paintGreen}
	f := newPaintFixture(t, func(_ *paintView, w *Window, _ *Context[paintView]) AnyElement {
		child := CustomElement[struct{}, struct{}](probe)
		return Div().SizeFull().Child(Canvas(
			func(_ Bounds, w *Window, app *App) struct{} {
				child.LayoutAsRoot(AvailableSize{Width: DefiniteAvailableSpace(100), Height: DefiniteAvailableSpace(100)}, w, app)
				WithAbsoluteElementOffsetVoid(w, Point{X: 7, Y: 9}, func(w *Window) { child.Prepaint(w, app) })
				return struct{}{}
			},
			func(_ Bounds, _ struct{}, w *Window, app *App) {
				WithAbsoluteElementOffsetVoid(w, Point{X: 300, Y: 300}, func(w *Window) { child.Paint(w, app) })
			},
		).SizeFull()).IntoElement()
	})
	f.draw()
	if probe.prepaint != logical(7, 9, 30, 20) || probe.painted != probe.prepaint {
		t.Fatalf("prepaint %v, paint %v; want both (7,9 30x20)", probe.prepaint, probe.painted)
	}
}
