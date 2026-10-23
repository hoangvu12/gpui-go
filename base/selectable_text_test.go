package base

import (
	"reflect"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:selectable_text.rs tests (239-317): 3 tests, 3 ported.

// U:selectable_text.rs:262-268
func TestExplicitHandleConstructorPreservesDocumentContract(t *testing.T) {
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
		handle := NewTextSelectionHandle("alpha beta", app, app.RootScope())
		text := SelectableTextWithHandle("plain", handle, "alpha beta").DocumentOrder(42)
		if text.handle == nil || text.handle.EntityID() != handle.EntityID() || text.documentOrder != 42 {
			t.Fatalf("text %+v", text)
		}
	})
}

// U:selectable_text.rs:270-296 over SelectableTextTestView (249-260).
func TestLocalHandleParticipatesInWindowSelection(t *testing.T) {
	ta := textTestApp(t)
	_, cx := openSelectionView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().SizeFull().Child(TextSelectionLayer{}).Child(
			gpui.Div().W(gpui.Px(240)).H(gpui.Px(32)).Child(NewSelectableText("local", "alpha beta")),
		).IntoElement()
	})
	drawText(t, cx)
	left := gpui.MouseButtonLeft
	cx.SimulateMouseDown(gpui.Point{X: 1, Y: 12}, left, gpui.Modifiers{})
	cx.SimulateMouseMove(gpui.Point{X: 220, Y: 12}, &left, gpui.Modifiers{})
	cx.SimulateMouseUp(gpui.Point{X: 220, Y: 12}, left, gpui.Modifiers{})
	drawText(t, cx)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		if got := TextSelectionSelectedText(w, app); got != "alpha beta" {
			t.Fatalf("selected %q", got)
		}
	})
}

// U:selectable_text.rs:298-316
func TestWrappedSelectionPaintsFullWidthMiddleLines(t *testing.T) {
	bounds := gpui.Bounds{Origin: gpui.Point{X: 10, Y: 20}, Size: gpui.Size{Width: 100, Height: 100}}
	quads := selectionQuadBounds(gpui.Point{X: 40, Y: 20}, gpui.Point{X: 30, Y: 80}, bounds, 20)
	want := []gpui.Bounds{
		textBoundsFromCorners(gpui.Point{X: 40, Y: 20}, gpui.Point{X: 110, Y: 40}),
		textBoundsFromCorners(gpui.Point{X: 10, Y: 40}, gpui.Point{X: 110, Y: 80}),
		textBoundsFromCorners(gpui.Point{X: 10, Y: 80}, gpui.Point{X: 30, Y: 100}),
	}
	if !reflect.DeepEqual(quads, want) {
		t.Fatalf("quads %v, want %v", quads, want)
	}
}
