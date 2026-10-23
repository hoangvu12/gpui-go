package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:radio.rs tests (5/5). radio_group.rs and toggle_group.rs
// have no upstream tests; the two group tests below are Go-own.

// U:radio.rs:254-267
func TestRadioSemanticStateStylesAreAvailableToApplications(t *testing.T) {
	opacity := float32(0.4)
	_ = NewRadio("states").Styles(func(s RadioStyles) RadioStyles {
		return s.
			Checked(func(style *StateStyle) *StateStyle { return style.Opacity(0.8) }).
			Disabled(func(style *StateStyle) *StateStyle {
				return style.
					Opacity(0.5).
					When(true, func(s *StateStyle) *StateStyle { return s.Border1() }).
					WhenSome(&opacity, func(s *StateStyle, o float32) *StateStyle { return s.Opacity(o) }).
					WhenNone((*float32)(nil), func(s *StateStyle) *StateStyle { return s.RoundedSm() })
			})
	})
}

// U:radio.rs:269-303
func TestRadioSemanticRootStylesFollowRadioPriority(t *testing.T) {
	styled := func(r *Radio) *Radio {
		return r.Styles(func(s RadioStyles) RadioStyles {
			return s.
				Checked(func(style *StateStyle) *StateStyle { return style.Opacity(0.8) }).
				Disabled(func(style *StateStyle) *StateStyle { return style.Opacity(0.5) })
		})
	}
	if styled(NewRadio("normal")).resolvedStyle().Opacity.IsSome() {
		t.Fatal("normal radio has an opacity")
	}
	if got := opacityOf(t, styled(NewRadio("checked").Checked(true)).resolvedStyle()); got != 0.8 {
		t.Fatalf("checked opacity = %v", got)
	}
	if got := opacityOf(t, styled(NewRadio("checked-disabled").Checked(true).Disabled(true)).resolvedStyle()); got != 0.5 {
		t.Fatalf("checked-disabled opacity = %v", got)
	}
	if got := opacityOf(t, styled(NewRadio("state-over-instance").Checked(true).Disabled(true).Opacity(0.9)).resolvedStyle()); got != 0.5 {
		t.Fatalf("state-over-instance opacity = %v", got)
	}
}

type radioRecord struct{ changes, keyboard int }

func radioHarness(t *testing.T, checked, disabled bool) (*gpui.VisualTestContext, *radioRecord) {
	t.Helper()
	rec := &radioRecord{}
	cx := drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return elementOf(t, NewRadio("radio").
			Checked(checked).
			Disabled(disabled).
			Size(gpui.Px(100)).
			OnChange(func(next bool, event *gpui.ClickEvent, _ *gpui.Window, _ *gpui.App) {
				if !next {
					t.Error("radio requested false")
				}
				rec.changes++
				if event.IsKeyboard() {
					rec.keyboard++
				}
			}))
	})
	return cx, rec
}

// U:radio.rs:351-370
func TestRadioPointerAndKeyboardActivationFireOnce(t *testing.T) {
	cx, rec := radioHarness(t, false, false)
	clickAt(cx, 10, 10)
	if rec.changes != 1 {
		t.Fatalf("pointer changes = %d, want 1", rec.changes)
	}
	rec.changes = 0
	redraw(t, cx)
	pressKeys(cx, "enter", "space")
	if rec.changes != 2 || rec.keyboard != 2 {
		t.Fatalf("changes = %d keyboard = %d; want 2, 2", rec.changes, rec.keyboard)
	}
}

// U:radio.rs:372-380
func TestRadioCheckedAndDisabledRadiosAreInert(t *testing.T) {
	for _, c := range []struct{ checked, disabled bool }{{true, false}, {false, true}} {
		cx, rec := radioHarness(t, c.checked, c.disabled)
		clickAt(cx, 10, 10)
		cx.SimulateKeystrokes("enter space")
		if rec.changes != 0 {
			t.Fatalf("%+v: changes = %d, want 0", c, rec.changes)
		}
	}
}

// U:radio.rs:382-413
func TestRadioAccessibilityExposesRoleStateAndAction(t *testing.T) {
	got := a11yOf(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewRadio("probe").Checked(true).AccessibilityLabel("Choice").render(w, app)
	})[0]
	if got.role != gpui.RoleRadioButton || got.spec.Label != "Choice" {
		t.Fatalf("node = %+v", got)
	}
	if v, ok := got.spec.Toggled.Get(); !ok || v != gpui.ToggledTrue {
		t.Fatalf("toggled = %v (%v)", v, ok)
	}
	if hasAction(got.spec, gpui.A11yActionClick) {
		t.Fatal("checked radio advertises Click")
	}
}

// Go-own: Radio.SetPosition and the selected mirror reach the node
// (U:radio.rs:145-151, 209-219).
func TestRadioSetPositionAndSelectedReachTheNode(t *testing.T) {
	got := a11yOf(t, func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
		return NewRadio("probe").Checked(true).SetPosition(2, 5).render(w, app)
	})[0]
	pos, _ := got.spec.PositionInSet.Get()
	size, _ := got.spec.SizeOfSet.Get()
	sel, _ := got.spec.Selected.Get()
	if pos != 2 || size != 5 || !sel {
		t.Fatalf("position %d size %d selected %v", pos, size, sel)
	}
}

// Go-own: RadioGroup and ToggleGroup publish their role and the axis
// orientation, vertical and horizontal by default respectively
// (U:radio_group.rs:56-66, U:toggle_group.rs:56-66).
func TestRadioGroupAndToggleGroupRoleAndOrientation(t *testing.T) {
	cases := []struct {
		name string
		d    *gpui.DivElement
		role gpui.Role
		want gpui.A11yOrientation
	}{
		{"radio-group", NewRadioGroup("rg").render(), gpui.RoleRadioGroup, gpui.A11yOrientationVertical},
		{"radio-group-h", NewRadioGroup("rg").Axis(gpui.AxisHorizontal).render(), gpui.RoleRadioGroup, gpui.A11yOrientationHorizontal},
		{"toggle-group", NewToggleGroup("tg").render(), gpui.RoleToolbar, gpui.A11yOrientationHorizontal},
		{"toggle-group-v", NewToggleGroup("tg").Axis(gpui.AxisVertical).render(), gpui.RoleToolbar, gpui.A11yOrientationVertical},
	}
	for _, c := range cases {
		role, _ := c.d.A11yRole()
		var spec gpui.A11ySpec
		c.d.WriteA11yInfo(&spec)
		if o, ok := spec.Orientation.Get(); role != c.role || !ok || o != c.want {
			t.Fatalf("%s: role %v orientation %v (%v)", c.name, role, o, ok)
		}
	}
}
