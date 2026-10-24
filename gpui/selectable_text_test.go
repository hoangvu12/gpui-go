package gpui

import (
	"reflect"
	"testing"
)

// Ports of the U:selectable_text.rs tests (239-317): 3 tests, 3 ported
// — moved from base with the element (browser-core 19; the view helpers
// are text_selection_test.go's).

// U:selectable_text.rs:262-268
func TestExplicitHandleConstructorPreservesDocumentContract(t *testing.T) {
	ta := newSelectionTestApp(t)
	ta.Update(func(app *App) {
		handle := NewTextSelectionHandle("alpha beta", app, app.RootScope())
		text := SelectableTextWithHandle("plain", handle, "alpha beta").DocumentOrder(42)
		if text.handle == nil || text.handle.EntityID() != handle.EntityID() || text.documentOrder != 42 {
			t.Fatalf("text %+v", text)
		}
	})
}

// U:selectable_text.rs:270-296 over SelectableTextTestView (249-260).
func TestLocalHandleParticipatesInWindowSelection(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, cx := openSelectionView(t, ta, func(*Window, *Context[selectionView]) AnyElement {
		return Div().SizeFull().Child(TextSelectionLayer{}).Child(
			Div().W(Px(240)).H(Px(32)).Child(NewSelectableText("local", "alpha beta")),
		).IntoElement()
	})
	drawSelectionFrame(t, cx)
	left := MouseButtonLeft
	cx.SimulateMouseDown(Point{X: 1, Y: 12}, left, Modifiers{})
	cx.SimulateMouseMove(Point{X: 220, Y: 12}, &left, Modifiers{})
	cx.SimulateMouseUp(Point{X: 220, Y: 12}, left, Modifiers{})
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "alpha beta" {
			t.Fatalf("selected %q", got)
		}
	})
}

// U:selectable_text.rs:298-316
func TestWrappedSelectionPaintsFullWidthMiddleLines(t *testing.T) {
	bounds := Bounds{Origin: Point{X: 10, Y: 20}, Size: Size{Width: 100, Height: 100}}
	quads := selectionQuadBounds(Point{X: 40, Y: 20}, Point{X: 30, Y: 80}, bounds, 20)
	want := []Bounds{
		boundsFromCorners(Point{X: 40, Y: 20}, Point{X: 110, Y: 40}),
		boundsFromCorners(Point{X: 10, Y: 40}, Point{X: 110, Y: 80}),
		boundsFromCorners(Point{X: 10, Y: 80}, Point{X: 30, Y: 100}),
	}
	if !reflect.DeepEqual(quads, want) {
		t.Fatalf("quads %v, want %v", quads, want)
	}
}
