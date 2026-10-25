package base

import (
	"reflect"
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:checkbox.rs tests (8/8).

func checkboxWantBorder(t *testing.T, name string, r gpui.StyleRefinement, want gpui.Hsla) {
	t.Helper()
	got, ok := r.BorderColor.Get()
	if !ok || !reflect.DeepEqual(got, gpui.BackgroundOf(want)) {
		t.Fatalf("%s: border color = %v (set %v), want %v", name, got, ok, want)
	}
}

// U:checkbox.rs:415-457
func TestCheckboxIndicatorProjectsStateStylesOverTheInstanceLayer(t *testing.T) {
	checkedColor := gpui.Hsla{H: 0.6, S: 0.7, L: 0.5, A: 1}
	disabledColor := gpui.Hsla{H: 0.1, S: 0.2, L: 0.3, A: 0.5}
	indicator := func(state CheckboxState, disabled bool) *CheckboxIndicator {
		return NewCheckboxIndicator().State(state).Disabled(disabled).
			Styles(func(s CheckboxIndicatorStyles) CheckboxIndicatorStyles {
				return s.
					Checked(func(style *StateStyle) *StateStyle { return style.BorderColor(checkedColor) }).
					Indeterminate(func(style *StateStyle) *StateStyle { return style.Opacity(0.7) }).
					Disabled(func(style *StateStyle) *StateStyle { return style.BorderColor(disabledColor) })
			})
	}
	checkboxWantBorder(t, "checked", indicator(CheckboxStateChecked, false).resolvedStyle(), checkedColor)
	checkboxWantBorder(t, "checked+disabled", indicator(CheckboxStateChecked, true).resolvedStyle(), disabledColor)
	if got := opacityOf(t, indicator(CheckboxStateIndeterminate, false).resolvedStyle()); got != 0.7 {
		t.Fatalf("indeterminate opacity = %v, want 0.7", got)
	}
	checkboxWantBorder(t, "instance", indicator(CheckboxStateChecked, true).BorderColor(checkedColor).resolvedStyle(), disabledColor)
}

type checkboxRecord struct {
	changes []CheckboxState
	parent  int
}

func checkboxHarness(t *testing.T, state CheckboxState, disabled bool) (*gpui.VisualTestContext, *checkboxRecord) {
	t.Helper()
	rec := &checkboxRecord{}
	cx := drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().
			ID("checkbox-parent").
			TabGroup().
			Size(gpui.Px(100)).
			OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { rec.parent++ }).
			Child(NewCheckbox("checkbox").
				State(state).
				Disabled(disabled).
				SizeFull().
				OnChange(func(next CheckboxState, _ *gpui.ClickEvent, _ *gpui.Window, _ *gpui.App) {
					rec.changes = append(rec.changes, next)
				})).
			IntoElement()
	})
	return cx, rec
}

// U:checkbox.rs:510-515
func TestCheckboxPointerActivationEmitsTheNextStateOnce(t *testing.T) {
	cx, rec := checkboxHarness(t, CheckboxStateUnchecked, false)
	clickAt(cx, 10, 10)
	if !slices.Equal(rec.changes, []CheckboxState{CheckboxStateChecked}) {
		t.Fatalf("changes = %v", rec.changes)
	}
}

// U:checkbox.rs:517-522
func TestCheckboxIndeterminateActivationBecomesChecked(t *testing.T) {
	cx, rec := checkboxHarness(t, CheckboxStateIndeterminate, false)
	clickAt(cx, 10, 10)
	if !slices.Equal(rec.changes, []CheckboxState{CheckboxStateChecked}) {
		t.Fatalf("changes = %v", rec.changes)
	}
}

// U:checkbox.rs:524-548
func TestCheckboxEnterAndSpaceEachEmitOnce(t *testing.T) {
	cx, rec := checkboxHarness(t, CheckboxStateChecked, false)
	clickAt(cx, 10, 10)
	rec.changes = nil
	if !cx.Window().Focused().Ok {
		t.Fatal("pointer press did not focus the checkbox")
	}
	redraw(t, cx)
	pressKeys(cx, "enter", "space")
	if !slices.Equal(rec.changes, []CheckboxState{CheckboxStateUnchecked, CheckboxStateUnchecked}) {
		t.Fatalf("changes = %v", rec.changes)
	}
}

// U:checkbox.rs:550-558
func TestCheckboxDisabledIsInertAndAllowsPointerEventsToBubble(t *testing.T) {
	cx, rec := checkboxHarness(t, CheckboxStateUnchecked, true)
	clickAt(cx, 10, 10)
	cx.Update(func(w *gpui.Window, _ *gpui.App) { w.FocusNext() })
	cx.SimulateKeystrokes("enter space")
	if len(rec.changes) != 0 || rec.parent != 1 {
		t.Fatalf("changes = %v parent = %d; want none, 1", rec.changes, rec.parent)
	}
}

// U:checkbox.rs:560-574
func TestCheckboxControlledStateAndStylingAreApplicationOwned(t *testing.T) {
	_ = NewCheckbox("states").
		Checked(true).
		Indeterminate(true).
		Styles(func(s CheckboxStyles) CheckboxStyles {
			return s.
				Checked(func(style *StateStyle) *StateStyle { return style.Opacity(0.8) }).
				Indeterminate(func(style *StateStyle) *StateStyle { return style.Opacity(0.7) }).
				Disabled(func(style *StateStyle) *StateStyle {
					return style.When(true, func(s *StateStyle) *StateStyle { return s.Opacity(0.5) })
				})
		}).
		Hover(gpui.Styles().Opacity(0.9)).
		Active(gpui.Styles().Opacity(0.8)).
		FocusVisible(gpui.Styles().Opacity(0.7))
}

// U:checkbox.rs:576-640
func TestCheckboxSemanticRootStylesFollowCheckboxPriority(t *testing.T) {
	styles := func(c *Checkbox) *Checkbox {
		return c.Styles(func(s CheckboxStyles) CheckboxStyles {
			return s.
				Checked(func(style *StateStyle) *StateStyle { return style.Opacity(0.8) }).
				Indeterminate(func(style *StateStyle) *StateStyle { return style.Opacity(0.7) }).
				Disabled(func(style *StateStyle) *StateStyle { return style.Opacity(0.5) })
		})
	}
	if styles(NewCheckbox("normal")).resolvedStyle().Opacity.IsSome() {
		t.Fatal("normal checkbox has an opacity")
	}
	for _, c := range []struct {
		name string
		cb   *Checkbox
		want float32
	}{
		{"checked", NewCheckbox("checked").Checked(true), 0.8},
		{"indeterminate", NewCheckbox("indeterminate").Indeterminate(true), 0.7},
		{"disabled", NewCheckbox("disabled").Disabled(true), 0.5},
		{"checked-disabled", NewCheckbox("checked-disabled").Checked(true).Disabled(true), 0.5},
	} {
		if got := opacityOf(t, styles(c.cb).resolvedStyle()); got != c.want {
			t.Fatalf("%s opacity = %v, want %v", c.name, got, c.want)
		}
	}

	checkedColor := gpui.Hsla{H: 0.6, S: 0.7, L: 0.5, A: 1}
	combined := NewCheckbox("combined").Checked(true).Disabled(true).
		Styles(func(s CheckboxStyles) CheckboxStyles {
			return s.
				Checked(func(style *StateStyle) *StateStyle { return style.BorderColor(checkedColor) }).
				Disabled(func(style *StateStyle) *StateStyle { return style.Opacity(0.5) })
		}).resolvedStyle()
	checkboxWantBorder(t, "combined", combined, checkedColor)
	if got := opacityOf(t, combined); got != 0.5 {
		t.Fatalf("combined opacity = %v, want 0.5", got)
	}

	over := styles(NewCheckbox("state-over-instance").Checked(true).Disabled(true).Opacity(0.9))
	if got := opacityOf(t, over.resolvedStyle()); got != 0.5 {
		t.Fatalf("state-over-instance opacity = %v, want 0.5", got)
	}
}

// U:checkbox.rs:642-704
func TestCheckboxAccessibilityExposesRoleLabelAndAllToggleStates(t *testing.T) {
	noop := func(CheckboxState, *gpui.ClickEvent, *gpui.Window, *gpui.App) {}
	got := a11yOf(t,
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewCheckbox("unchecked").AccessibilityLabel("Remember me").OnChange(noop).render(w, app)
		},
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewCheckbox("checked").Checked(true).OnChange(noop).render(w, app)
		},
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewCheckbox("mixed").Indeterminate(true).OnChange(noop).render(w, app)
		},
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewCheckbox("disabled").Disabled(true).OnChange(noop).render(w, app)
		},
	)
	unchecked, checked, mixed, disabled := got[0], got[1], got[2], got[3]
	if unchecked.role != gpui.RoleCheckBox || unchecked.spec.Label != "Remember me" {
		t.Fatalf("unchecked = %+v", unchecked)
	}
	for _, c := range []struct {
		name string
		p    a11yProbe
		want gpui.Toggled
	}{{"unchecked", unchecked, gpui.ToggledFalse}, {"checked", checked, gpui.ToggledTrue}, {"mixed", mixed, gpui.ToggledMixed}} {
		if v, ok := c.p.spec.Toggled.Get(); !ok || v != c.want {
			t.Fatalf("%s toggled = %v (%v), want %v", c.name, v, ok, c.want)
		}
	}
	if !hasAction(unchecked.spec, gpui.A11yActionClick) || hasAction(disabled.spec, gpui.A11yActionClick) {
		t.Fatal("click action surface mismatch")
	}
}
