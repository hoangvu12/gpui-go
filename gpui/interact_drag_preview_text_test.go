package gpui

// Consumer gap 08: text inside drag preview views. The preview root
// prepaints under a non-zero absolute element offset
// (prepaintActiveDragPreview → PrepaintAsRoot at mouse - cursor
// offset), so the text element's parent-relative placement — the only
// layout path that consults the element offset during prepaint — must
// add the offset exactly once (window.rs parent_relative_layout_
// bounds adds pixel_snap_point(element_offset()) after the engine's
// raw computation). These tests pin the whole element path: the text
// commits bounds inside the preview card and follows the cursor, and
// the preview re-renders its label when the view entity is notified
// mid-drag.

import (
	"testing"
)

// previewTextView renders the drag preview card with one text child.
type previewTextView struct {
	label  string
	textEl AnyElement
}

func (v *previewTextView) Render(w *Window, cx *Context[previewTextView]) AnyElement {
	v.textEl = Text(v.label)
	return Div().W(Px(200)).H(Px(20)).
		Bg(Hsla{H: 0, S: 0, L: 0.5, A: 1}).
		Child(v.textEl).
		DebugSelector("preview-card").
		IntoElement()
}

// previewTextFixture drives a drag with a text-bearing preview and
// returns the fixture plus the preview view's entity.
func previewTextFixture(t *testing.T, label string) (*interactFixture, Entity[previewTextView]) {
	t.Helper()
	var preview Entity[previewTextView]
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		source := Div().ID("source").Absolute().Left(Px(0)).Top(Px(0)).Size(Px(50)).CursorGrab().
			OnDrag(dragPayload{name: "tab"}, func(value *dragPayload, offset Point, w *Window, app *App) View {
				preview = NewEntity(app, w.Scope(), func(v *previewTextView, cx *Context[previewTextView]) {
					v.label = label
				})
				return ViewOf(preview)
			})
		return Div().Relative().SizeFull().Child(source).IntoElement()
	})
	f.down(MouseButtonLeft, 10, 10)
	f.draw()
	f.dragMove(20, 20)
	f.draw()
	if drag := f.app.ActiveDrag(); drag == nil || drag.View == nil {
		t.Fatal("no active drag with a view")
	}
	return f, preview
}

// previewTextBounds reads the preview's text element's committed
// paint bounds (the parent-relative bounds textElement.Prepaint
// commits).
func previewTextBounds(t *testing.T, f *interactFixture, entity Entity[previewTextView]) Bounds {
	t.Helper()
	var bounds *Bounds
	entity.Update(f.app, func(v *previewTextView, cx *Context[previewTextView]) {
		if v.textEl.obj == nil {
			t.Fatal("the preview view rendered no text element")
		}
		text, ok := v.textEl.obj.(*drawable[stringLayoutState, struct{}, Element[stringLayoutState, struct{}]])
		if !ok {
			t.Fatalf("the preview's text child is a %T, want the text element drawable", v.textEl.obj)
		}
		bounds = text.requestState.bounds
	})
	if bounds == nil {
		t.Fatal("the text element's prepaint committed no bounds")
	}
	return *bounds
}

// TestInteractDragPreviewTextCommitsBoundsInsideCard: with the mouse
// at (120,100) and the drag's cursor offset (20,20), the preview card
// paints at (100,80) and the text's committed bounds must sit inside
// the card — the parent-relative path adds the drag offset exactly
// once (twice lands at (200,160), outside the card and usually the
// window, which is the empty-preview-card symptom).
func TestInteractDragPreviewTextCommitsBoundsInsideCard(t *testing.T) {
	f, preview := previewTextFixture(t, "Row 0")
	f.dragMove(120, 100)
	f.draw()

	card, ok := WindowDebugBound(f.window, "preview-card")
	if !ok {
		t.Fatal("no preview card bounds recorded")
	}
	if card.Origin.X != 100 || card.Origin.Y != 80 {
		t.Fatalf("preview card origin = %v, want mouse - cursor offset = (100,80)", card.Origin)
	}
	text := previewTextBounds(t, f, preview)
	if text.Origin.X < card.Origin.X || text.Origin.Y < card.Origin.Y ||
		text.Right() > card.Right() || text.Bottom() > card.Bottom() {
		t.Fatalf("text bounds %+v lie outside the preview card %+v (the drag offset was added more than once)", text, card)
	}
	if text.Origin.X != card.Origin.X || text.Origin.Y != card.Origin.Y {
		t.Errorf("text origin = %v, want the card's padding-free origin %v", text.Origin, card.Origin)
	}
}

// TestInteractDragPreviewTextFollowsCursorAcrossFrames: the committed
// text bounds track the preview card as the drag moves, frame over
// frame.
func TestInteractDragPreviewTextFollowsCursorAcrossFrames(t *testing.T) {
	f, preview := previewTextFixture(t, "Row 0")
	for _, pos := range [][2]float32{{120, 100}, {200, 150}, {60, 40}} {
		f.dragMove(pos[0], pos[1])
		f.draw()
		card, ok := WindowDebugBound(f.window, "preview-card")
		if !ok {
			t.Fatal("no preview card bounds recorded")
		}
		want := Point{X: pos[0] - 20, Y: pos[1] - 20}
		if card.Origin != want {
			t.Fatalf("card origin at %v = %v, want %v", pos, card.Origin, want)
		}
		text := previewTextBounds(t, f, preview)
		if text.Origin != card.Origin {
			t.Fatalf("text origin at %v = %v, want the card origin %v (the offset doubled again)", pos, text.Origin, card.Origin)
		}
	}
}

// TestInteractDragPreviewTextUpdatesWhenNotifiedMidDrag: the preview
// view's entity re-renders its label when notified between frames and
// the new text keeps its placement inside the card.
func TestInteractDragPreviewTextUpdatesWhenNotifiedMidDrag(t *testing.T) {
	f, preview := previewTextFixture(t, "Row 0")
	f.dragMove(120, 100)
	f.draw()

	preview.Update(f.app, func(v *previewTextView, cx *Context[previewTextView]) {
		v.label = "Row 42"
		cx.Notify()
	})
	f.draw()

	card, ok := WindowDebugBound(f.window, "preview-card")
	if !ok {
		t.Fatal("no preview card bounds recorded")
	}
	text := previewTextBounds(t, f, preview)
	if text.Origin != card.Origin {
		t.Fatalf("text origin after the mid-drag notify = %v, want the card origin %v", text.Origin, card.Origin)
	}
	var rendered string
	preview.Update(f.app, func(v *previewTextView, cx *Context[previewTextView]) {
		if te, ok := v.textEl.obj.(*drawable[stringLayoutState, struct{}, Element[stringLayoutState, struct{}]]); ok {
			if element, ok := te.element.(*textElement); ok {
				rendered = element.text
			}
		}
	})
	if rendered != "Row 42" {
		t.Fatalf("the preview rendered %q after the notify, want the updated label", rendered)
	}
}
