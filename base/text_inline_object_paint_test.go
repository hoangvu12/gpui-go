package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Go-own (ticket 36): an InlineObject prepainted as a root at its flow
// origin (U:text/inline_flow.rs, InlineFlow::prepaint's
// `element.prepaint_as_root(bounds.origin + origin, ..)`) and painted with
// a plain AnyElement::paint (InlineFlow::paint) paints at that origin:
// gpui's drawables paint with the bounds committed at prepaint
// (R:element.rs:537-545, 557-600), so the object's own paint bounds and
// its content's are the flow position, not the layout origin.
func TestInlineObjectPaintsAtItsPrepaintOrigin(t *testing.T) {
	ta := textTestApp(t)
	content := gpui.Hsla{H: 0.6, S: 1, L: 0.5, A: 1}
	origin := gpui.Point{X: 40, Y: 30}
	var scale float32
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().SizeFull().Child(gpui.Canvas(
			func(_ gpui.Bounds, w *gpui.Window, app *gpui.App) gpui.AnyElement {
				scale = gpui.ScaleFactorOf(w)
				presentation := NewInlineElement(gpui.Div().Flex().W(gpui.Px(24)).H(gpui.Px(12)).Bg(content)).WithBaseline(10)
				measured := measureInlineObject("x", &presentation, gpui.WindowTextStyle(w), w, app)
				size := measured.metrics.size
				selection := gpui.Bounds{Origin: origin, Size: size}
				el := newInlineObject(gpui.NamedIntegerElementID("inline-object", 1), "x", "x", measured, &sharedFlag{}, selection, selection).IntoElement()
				el.PrepaintAsRoot(origin, gpui.AvailableSize{
					Width:  gpui.DefiniteAvailableSpace(size.Width),
					Height: gpui.DefiniteAvailableSpace(size.Height),
				}, w, app)
				return el
			},
			func(_ gpui.Bounds, el gpui.AnyElement, w *gpui.Window, app *gpui.App) {
				el.Paint(w, app)
			},
		).Flex().SizeFull()).IntoElement()
	})
	cx.RunUntilParked()
	scene, err := gpui.DrawWindowFrame(cx.Window())
	if err != nil {
		t.Fatal(err)
	}
	quads, err := scene.Quads()
	if err != nil {
		t.Fatal(err)
	}
	var found []gpui.Bounds
	for _, q := range quads {
		if q.Background.Solid == content {
			found = append(found, q.Bounds)
		}
	}
	want := gpui.Bounds{Origin: gpui.Point{X: origin.X * scale, Y: origin.Y * scale}, Size: gpui.Size{Width: 24 * scale, Height: 12 * scale}}
	if len(found) != 1 || found[0] != want {
		t.Fatalf("inline content quads %v, want one at %v (the prepaint origin)", found, want)
	}
}
