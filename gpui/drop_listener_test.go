package gpui

// zterm ticket 12: drop predicates and drag-over styles (div.rs:598-668,
// 1334-1430, 1991-2110, 3391-3445, 3973-4021). The pin has no tests for
// these; the scenarios follow TestInteractDragSourceTranscript: a drag
// source at (0,0)-(50,50) and drop targets on the right.

import "testing"

type otherPayload struct{}

// dragSource is the drag source of these tests, carrying payload.
func dragSource[T any](payload T) *DivElement {
	return Div().ID("source").Absolute().Left(Px(0)).Top(Px(0)).Size(Px(50)).
		OnDrag(payload, func(*T, Point, *Window, *App) View { return nil })
}

// startDrag presses on the source and moves past the threshold.
func (f *interactFixture) startDrag() {
	f.t.Helper()
	f.down(MouseButtonLeft, 10, 10)
	f.draw()
	f.dragMove(20, 20)
	if !f.app.HasActiveDrag() {
		f.t.Fatal("the drag did not start")
	}
	f.draw()
}

// An inner target that refuses the payload (DropListener.CanDropWithin,
// or the element-level CanDrop) leaves the drop to the outer target; an
// accepted drop stops at the inner one.
func TestDropListenerRefusalFallsThroughToOuterTarget(t *testing.T) {
	for _, elementLevel := range []bool{false, true} {
		for _, c := range []struct{ payload, want string }{{"refused", "outer(refused)"}, {"tab", "inner(tab)"}} {
			rec := &transcript{}
			f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
				accepts := func(value *dragPayload) bool { return value.name != "refused" }
				onInner := func(value *dragPayload, e *MouseUpEvent, w *Window, app *App) { rec.add("inner(%s)", value.name) }
				inner := Div().ID("inner").Size(Px(50))
				if elementLevel {
					inner = OnDrop(inner.CanDrop(func(value any, w *Window, app *App) bool {
						payload := value.(dragPayload)
						return accepts(&payload)
					}), onInner)
				} else {
					inner = inner.OnDropAlt(DropListener[dragPayload]{}.
						CanDropWithin(func(value *dragPayload, w *Window, app *App) bool { return accepts(value) }).
						OnDrop(onInner))
				}
				outer := OnDrop(Div().ID("outer").Absolute().Left(Px(200)).Top(Px(100)).Size(Px(100)).Child(inner),
					func(value *dragPayload, e *MouseUpEvent, w *Window, app *App) { rec.add("outer(%s)", value.name) })
				return Div().Relative().SizeFull().Child(dragSource(dragPayload{name: c.payload})).Child(outer).IntoElement()
			})
			f.startDrag()
			f.dragMove(210, 110)
			f.draw()
			f.up(MouseButtonLeft, 210, 110)
			wantTranscript(t, c.payload, rec.take(), []string{c.want})
			if f.app.HasActiveDrag() {
				t.Fatalf("%s: the drop did not end the drag", c.payload)
			}
		}
	}
}

// CanDropAnywhere replaces the hovered gate: the target takes a drop
// released outside it.
func TestDropListenerCanDropAnywhereTakesDropOutside(t *testing.T) {
	rec := &transcript{}
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		target := Div().ID("target").Absolute().Left(Px(200)).Top(Px(100)).Size(Px(50)).
			OnDropAlt(DropListener[dragPayload]{}.
				CanDropAnywhere(func(*dragPayload, *Hitbox, *Window, *App) bool { return true }).
				OnDrop(func(value *dragPayload, e *MouseUpEvent, w *Window, app *App) { rec.add("drop(%s)", value.name) }))
		return Div().Relative().SizeFull().Child(dragSource(dragPayload{name: "tab"})).Child(target).IntoElement()
	})
	f.startDrag()
	f.up(MouseButtonLeft, 100, 250)
	wantTranscript(t, "drop outside", rec.take(), []string{"drop(tab)"})
}

// OnDragOver styles the target only while a drag of its type that can
// drop hovers it; leaving the target drops the style (the pin's only
// "drag leave"), as does another payload type or a CanDrop refusal.
func TestDragOverStyleTracksHoveringDrag(t *testing.T) {
	cases := []struct {
		name    string
		payload any
		refuse  bool
		styled  bool
	}{
		{"matching", dragPayload{name: "tab"}, false, true},
		{"other type", otherPayload{}, false, false},
		{"refused", dragPayload{name: "tab"}, true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
				target := Div().ID("target").Absolute().Left(Px(200)).Top(Px(100)).Size(Px(100)).Bg(interactRed).
					OnDragOver(func(s StyleRefinement, value *dragPayload, w *Window, app *App) StyleRefinement {
						if app.HasActiveDrag() {
							t.Error("the drag-over callback saw the active drag")
						}
						return Div().Refine(s).Bg(interactBlue).Style()
					})
				if c.refuse {
					target = target.CanDrop(func(any, *Window, *App) bool { return false })
				}
				var source *DivElement
				switch payload := c.payload.(type) {
				case dragPayload:
					source = dragSource(payload)
				case otherPayload:
					source = dragSource(payload)
				}
				return Div().Relative().SizeFull().Child(source).Child(target).IntoElement()
			})
			blue := func() int { return quadCount(t, f.window, interactBlue) }
			f.startDrag()
			if blue() != 0 {
				t.Fatal("styled before the drag reached the target")
			}
			f.dragMove(250, 150)
			f.draw()
			if got := blue() == 1; got != c.styled {
				t.Fatalf("over the target: styled %v, want %v", got, c.styled)
			}
			f.dragMove(100, 250)
			f.draw()
			if blue() != 0 {
				t.Fatal("still styled after the drag left the target")
			}
			f.up(MouseButtonLeft, 100, 250)
			f.draw()
			if blue() != 0 {
				t.Fatal("styled after the drag ended")
			}
		})
	}
}

// OnDragOverAny applies while a drop listener of the drag's type exists
// (div.rs:3989-4014 relevant_drop_listener); GroupDragOver applies while
// the named group is hovered by a drag of its type.
func TestDragOverAnyAndGroupDragOver(t *testing.T) {
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		anyTarget := OnDrop(Div().ID("any").Absolute().Left(Px(200)).Top(Px(0)).Size(Px(50)).Bg(interactRed).
			OnDragOverAny(func(s StyleRefinement, value any, w *Window, app *App) StyleRefinement {
				return Div().Refine(s).Bg(interactBlue).Style()
			}), func(*dragPayload, *MouseUpEvent, *Window, *App) {})
		noListener := Div().ID("nolistener").Absolute().Left(Px(300)).Top(Px(0)).Size(Px(50)).Bg(interactRed).
			OnDragOverAny(func(s StyleRefinement, value any, w *Window, app *App) StyleRefinement {
				return Div().Refine(s).Bg(interactBlue).Style()
			})
		groupChild := OnDrop(Div().ID("child").Size(Px(10)).Bg(interactRed).
			GroupDragOver[dragPayload]("row", func(s StyleRefinement) StyleRefinement {
			return Div().Refine(s).Bg(interactGreen).Style()
		}), func(*dragPayload, *MouseUpEvent, *Window, *App) {})
		row := Div().Group("row").Absolute().Left(Px(200)).Top(Px(200)).Size(Px(80)).Child(groupChild)
		return Div().Relative().SizeFull().Child(dragSource(dragPayload{name: "tab"})).
			Child(anyTarget).Child(noListener).Child(row).IntoElement()
	})
	f.startDrag()
	f.dragMove(210, 10)
	f.draw()
	if got := quadCount(t, f.window, interactBlue); got != 1 {
		t.Fatalf("over the listener target: %d blue quads, want 1", got)
	}
	f.dragMove(310, 10)
	f.draw()
	if got := quadCount(t, f.window, interactBlue); got != 0 {
		t.Fatalf("over the target without a drop listener: %d blue quads, want 0", got)
	}
	f.dragMove(250, 250) // inside the group, outside its child
	f.draw()
	if got := quadCount(t, f.window, interactGreen); got != 1 {
		t.Fatalf("over the group: %d green quads, want 1", got)
	}
}
