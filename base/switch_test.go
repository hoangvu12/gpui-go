package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:switch.rs tests (9/9).

func switchBool(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// U:switch.rs:412-454
func TestSwitchTrackProjectsCheckedDisabledAndInstanceStylePriority(t *testing.T) {
	normal := gpui.Hsla{H: 0, S: 0, L: 0.2, A: 1}
	checkedColor := gpui.Hsla{H: 0.6, S: 0.7, L: 0.5, A: 1}
	disabledColor := gpui.Hsla{H: 0.6, S: 0.7, L: 0.5, A: 0.5}
	track := func(checked, disabled bool) *SwitchTrack {
		return NewSwitchTrack(gpui.NamedIntegerElementID("track", switchBool(checked)*2+switchBool(disabled))).
			Checked(checked).
			Disabled(disabled).
			When(!checked, func(t *SwitchTrack) *SwitchTrack { return t.Bg(normal) }).
			Styles(func(s SwitchTrackStyles) SwitchTrackStyles {
				return s.
					Checked(func(style *StateStyle) *StateStyle { return style.Bg(checkedColor) }).
					Disabled(func(style *StateStyle) *StateStyle {
						return style.When(checked, func(s *StateStyle) *StateStyle { return s.Bg(disabledColor) })
					})
			})
	}
	wantBackground(t, "normal", track(false, false).resolvedStyle(), &normal)
	wantBackground(t, "disabled", track(false, true).resolvedStyle(), &normal)
	wantBackground(t, "checked", track(true, false).resolvedStyle(), &checkedColor)
	wantBackground(t, "checked+disabled", track(true, true).resolvedStyle(), &disabledColor)
	wantBackground(t, "instance", track(true, true).Bg(normal).resolvedStyle(), &disabledColor)
}

// U:switch.rs:456-477
func TestSwitchTrackIdentityIsScopedToItsSwitch(t *testing.T) {
	trackID := func(switchID string) gpui.ElementID {
		return gpui.NamedChildElementID(gpui.NameElementID(switchID), "track")
	}
	first, _ := NewSwitchTrack(trackID("first-switch")).IntoElement().ID()
	second, _ := NewSwitchTrack(trackID("second-switch")).IntoElement().ID()
	if first.String() != trackID("first-switch").String() || second.String() != trackID("second-switch").String() {
		t.Fatalf("ids = %v, %v", first, second)
	}
	if first.String() == second.String() {
		t.Fatal("track ids collide")
	}
}

// U:switch.rs:479-508
func TestSwitchThumbProjectsCheckedDisabledAndInstanceStylePriority(t *testing.T) {
	checkedColor := gpui.Hsla{H: 0.6, S: 0.7, L: 0.5, A: 1}
	disabledColor := gpui.Hsla{H: 0.1, S: 0.2, L: 0.3, A: 0.5}
	thumb := func(checked, disabled bool) *SwitchThumb {
		return NewSwitchThumb(checked).Disabled(disabled).
			Styles(func(s SwitchThumbStyles) SwitchThumbStyles {
				return s.
					Checked(func(style *StateStyle) *StateStyle { return style.Bg(checkedColor) }).
					Disabled(func(style *StateStyle) *StateStyle { return style.Bg(disabledColor) })
			})
	}
	wantBackground(t, "checked", thumb(true, false).resolvedStyle(), &checkedColor)
	wantBackground(t, "checked+disabled", thumb(true, true).resolvedStyle(), &disabledColor)
	wantBackground(t, "instance", thumb(true, true).Bg(checkedColor).resolvedStyle(), &disabledColor)
}

type switchRecord struct {
	toggles, keyboard, parent int
	last                      bool
}

func switchHarness(t *testing.T, checked, disabled bool) (*gpui.VisualTestContext, *switchRecord) {
	t.Helper()
	rec := &switchRecord{last: checked}
	cx := drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().
			ID("switch-parent").
			TabGroup().
			Size(gpui.Px(100)).
			OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { rec.parent++ }).
			Child(NewSwitch("switch").
				Checked(checked).
				Disabled(disabled).
				SizeFull().
				OnChange(func(value bool, event *gpui.ClickEvent, _ *gpui.Window, _ *gpui.App) {
					rec.toggles++
					rec.last = value
					if event.IsKeyboard() {
						rec.keyboard++
					}
				})).
			IntoElement()
	})
	return cx, rec
}

// U:switch.rs:590-599
func TestSwitchPointerReportsTheNextValueOnce(t *testing.T) {
	cx, rec := switchHarness(t, false, false)
	clickAt(cx, 10, 10)
	if rec.toggles != 1 || rec.keyboard != 0 || !rec.last {
		t.Fatalf("record = %+v", rec)
	}
}

// U:switch.rs:601-617
func TestSwitchEnterAndSpaceReportOneNativeKeyboardActivationEach(t *testing.T) {
	cx, rec := switchHarness(t, false, false)
	clickAt(cx, 10, 10)
	rec.toggles = 0
	if !cx.Window().Focused().Ok {
		t.Fatal("pointer press did not focus the switch")
	}
	redraw(t, cx)
	pressKeys(cx, "enter", "space")
	if rec.toggles != 2 || rec.keyboard != 2 || !rec.last {
		t.Fatalf("record = %+v", rec)
	}
}

// U:switch.rs:619-629
func TestSwitchDisabledIsInertAndBlocksParentActivation(t *testing.T) {
	cx, rec := switchHarness(t, false, true)
	clickAt(cx, 10, 10)
	pressKeys(cx, "enter", "space")
	if rec.toggles != 0 || rec.parent != 0 {
		t.Fatalf("record = %+v", rec)
	}
}

// U:switch.rs:631-642
func TestSwitchApplicationOwnedStateStylesAreAvailable(t *testing.T) {
	_ = NewSwitch("states").
		Styles(func(s SwitchStyles) SwitchStyles {
			return s.
				Checked(func(style *StateStyle) *StateStyle { return style.Opacity(0.8) }).
				Disabled(func(style *StateStyle) *StateStyle { return style.Opacity(0.5) })
		}).
		Hover(gpui.Styles().Opacity(0.9)).
		Active(gpui.Styles().Opacity(0.8)).
		FocusVisible(gpui.Styles().Opacity(0.7))
}

// U:switch.rs:644-678
func TestSwitchSemanticRootStylesFollowSwitchPriority(t *testing.T) {
	styled := func(s *Switch) *Switch {
		return s.Styles(func(st SwitchStyles) SwitchStyles {
			return st.
				Checked(func(style *StateStyle) *StateStyle { return style.Opacity(0.8) }).
				Disabled(func(style *StateStyle) *StateStyle { return style.Opacity(0.5) })
		})
	}
	if styled(NewSwitch("normal")).resolvedStyle().Opacity.IsSome() {
		t.Fatal("normal switch has an opacity")
	}
	if got := opacityOf(t, styled(NewSwitch("checked").Checked(true)).resolvedStyle()); got != 0.8 {
		t.Fatalf("checked opacity = %v", got)
	}
	if got := opacityOf(t, styled(NewSwitch("checked-disabled").Checked(true).Disabled(true)).resolvedStyle()); got != 0.5 {
		t.Fatalf("checked-disabled opacity = %v", got)
	}
	if got := opacityOf(t, styled(NewSwitch("state-over-instance").Checked(true).Disabled(true).Opacity(0.9)).resolvedStyle()); got != 0.5 {
		t.Fatalf("state-over-instance opacity = %v", got)
	}
}

// U:switch.rs:680-738
func TestSwitchAccessibilityExposesSwitchRoleLabelAndToggledState(t *testing.T) {
	noop := func(bool, *gpui.ClickEvent, *gpui.Window, *gpui.App) {}
	got := a11yOf(t,
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewSwitch("enabled").Checked(true).AccessibilityLabel("Airplane mode").OnChange(noop).render(w, app)
		},
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewSwitch("disabled").Checked(false).Disabled(true).AccessibilityLabel("Airplane mode").OnChange(noop).render(w, app)
		},
	)
	enabled, disabled := got[0], got[1]
	if enabled.role != gpui.RoleSwitch || enabled.spec.Label != "Airplane mode" || !hasAction(enabled.spec, gpui.A11yActionClick) {
		t.Fatalf("enabled = %+v", enabled)
	}
	if v, ok := enabled.spec.Toggled.Get(); !ok || v != gpui.ToggledTrue {
		t.Fatalf("enabled toggled = %v (%v)", v, ok)
	}
	if v, ok := disabled.spec.Toggled.Get(); disabled.role != gpui.RoleSwitch || !ok || v != gpui.ToggledFalse {
		t.Fatalf("disabled = %+v", disabled)
	}
	if hasAction(disabled.spec, gpui.A11yActionClick) {
		t.Fatal("disabled switch advertises Click")
	}
	// No aria-disabled setter upstream (U:switch.rs:735-737).
	if disabled.spec.Disabled {
		t.Fatal("disabled switch reports the Disabled flag")
	}
}
