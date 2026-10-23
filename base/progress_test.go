package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:progress.rs tests (3/3).

// progressA11y renders d in an empty window and returns its a11y info
// (upstream `cx.add_empty_window()` + `render(window, cx).into_element()
// .write_a11y_info(&mut node)`).
func progressA11y(t *testing.T, render func(w *gpui.Window, app *gpui.App) *gpui.DivElement) a11yProbe {
	t.Helper()
	cx := gpui.NewTestApp().AddEmptyWindow()
	var p a11yProbe
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		d := render(w, app)
		p.role, p.hasRole = d.A11yRole()
		d.WriteA11yInfo(&p.spec)
	})
	return p
}

func progressWantFloat(t *testing.T, name string, got gpui.Optional[float64], want float64) {
	t.Helper()
	if v, ok := got.Get(); !ok || v != want {
		t.Fatalf("%s = %v (set %v), want %v", name, v, ok, want)
	}
}

// U:progress.rs:135-150
func TestProgressClampsAndProjectsNumericAccessibility(t *testing.T) {
	p := progressA11y(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewProgress("progress").Value(120).render(w, app)
	})
	progressWantFloat(t, "numeric", p.spec.NumericValue, 100)
	progressWantFloat(t, "min", p.spec.MinNumericValue, 0)
	progressWantFloat(t, "max", p.spec.MaxNumericValue, 100)
	if p.role != gpui.RoleProgressIndicator {
		t.Fatalf("role = %v", p.role)
	}
}

// U:progress.rs:152-166
func TestProgressIndeterminateOmitsNumericValue(t *testing.T) {
	p := progressA11y(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewProgress("loading").Value(40).Indeterminate(true).render(w, app)
	})
	if p.spec.NumericValue.IsSome() {
		t.Fatalf("numeric value = %v, want none", p.spec.NumericValue)
	}
}

// U:progress.rs:168-181
func TestProgressProjectsItsAccessibleName(t *testing.T) {
	p := progressA11y(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewProgress("download").AccessibilityLabel("Downloading release").render(w, app)
	})
	if p.spec.Label != "Downloading release" {
		t.Fatalf("label = %q", p.spec.Label)
	}
}
