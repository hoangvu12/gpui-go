package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// focus_trap.rs has no upstream tests (0/0). Go-own: a FocusTrap
// container registers with the manager while it draws, Root's Tab
// handlers cycle inside it (U:focus_trap.rs:21-35, U:root.rs:194-262),
// and the trap stops being active once its handle is released
// (U:focus_trap.rs:97-100).

// focusTrapTestView renders two stops inside a trap and one outside.
type focusTrapTestView struct {
	container gpui.FocusHandle
	a, b, c   gpui.FocusHandle
}

func (v *focusTrapTestView) Render(*gpui.Window, *gpui.Context[focusTrapTestView]) gpui.AnyElement {
	trap := FocusTrapElementOf(gpui.Div()).
		FocusTrap("trap1", v.container).
		Child(gpui.Div().Size(gpui.Px(10)).TrackFocus(v.a)).
		Child(gpui.Div().Size(gpui.Px(10)).TrackFocus(v.b))
	return gpui.Div().SizeFull().
		Child(trap).
		Child(gpui.Div().Size(gpui.Px(10)).TrackFocus(v.c)).
		IntoElement()
}

func TestFocusTrapContainerCyclesTabInsideTheContainer(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	app := ta.App()
	container := app.FocusHandle()
	a := app.FocusHandle().TabStop(true)
	b := app.FocusHandle().TabStop(true)
	c := app.FocusHandle().TabStop(true)
	_, cx := openRoot(ta, func(w *gpui.Window, cx *gpui.Context[Root]) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(cx, w.Scope(), func(v *focusTrapTestView, _ *gpui.Context[focusTrapTestView]) {
			v.container, v.a, v.b, v.c = container, a, b, c
		}))
	})
	focused := func() uint64 {
		var id uint64
		cx.Update(func(w *gpui.Window, _ *gpui.App) {
			if f := w.Focused(); f.Ok {
				id = f.Handle.ID()
			}
		})
		return id
	}

	cx.Update(func(w *gpui.Window, app *gpui.App) {
		w.Focus(b)
		if trap, ok := ActiveFocusTrap(w, app); !ok || trap.ID() != container.ID() {
			t.Fatalf("active trap = %v (%v), want the container", trap.ID(), ok)
		}
	})
	cx.SimulateKeystrokes("tab")
	if focused() != a.ID() {
		t.Fatalf("tab out of the trap landed on %d, want a (%d)", focused(), a.ID())
	}
	cx.SimulateKeystrokes("shift-tab")
	if focused() != b.ID() {
		t.Fatalf("shift-tab out of the trap landed on %d, want b (%d)", focused(), b.ID())
	}

	// Focus outside the container: no trap is active and Tab is plain
	// navigation.
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		w.Focus(c)
		if _, ok := ActiveFocusTrap(w, app); ok {
			t.Fatal("a trap is active while focus is outside every trap")
		}
	})

	// A released container handle is no longer a trap.
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		w.Focus(a)
		container.Release()
		if _, ok := ActiveFocusTrap(w, app); ok {
			t.Fatal("a released trap is still active")
		}
	})
}
