package base

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of the pure value modules: theme.rs (1/1),
// theme_tokens.rs (1/1), state_style.rs (4/4), component_traits.rs (2/2),
// actions.rs (1/1), index_path.rs (3/3). Go-own tests are marked.

// U:theme.rs:143-156
func TestThemeScrollbarThemeSupportsBuilderConstruction(t *testing.T) {
	mode := ScrollbarModeHover
	motion := DefaultScrollbarMotion().WithEnter(120 * time.Millisecond)
	styles := ScrollbarStyles{}
	theme := NewScrollbarTheme().WithMode(mode).WithMotion(motion).WithStyles(styles)

	if theme.Mode() != mode {
		t.Fatalf("mode = %v", theme.Mode())
	}
	if theme.Motion() != motion {
		t.Fatalf("motion = %+v", theme.Motion())
	}
	_ = theme.Styles()
}

// U:theme_tokens.rs:262-273
func TestThemeTokensDefaultColorsAreTheLightPaletteAndBothPalettesAreReadable(t *testing.T) {
	light := LightColorTokens()
	dark := DarkColorTokens()

	if DefaultColorTokens() != light {
		t.Fatal("default colors are not the light palette")
	}
	if light.Background.L != 1 {
		t.Fatalf("light background lightness = %v", light.Background.L)
	}
	if !(light.Foreground.L < light.Background.L) {
		t.Fatal("light foreground is not darker than its background")
	}
	if !(dark.Background.L < dark.Foreground.L) {
		t.Fatal("dark background is not darker than its foreground")
	}
	if light.Primary.A != 1 || dark.Primary.A != 1 {
		t.Fatal("primary is not opaque")
	}
}

// Go-own: a palette written before the selection token loads with the
// default selection (U:theme_tokens.rs:57-64 serde default).
func TestThemeTokensSelectionDefaultsWhenMissing(t *testing.T) {
	data, err := json.Marshal(DarkColorTokens())
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	delete(fields, "selection")
	data, _ = json.Marshal(fields)
	var decoded ColorTokens
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Selection != LightColorTokens().Selection || decoded.Background != DarkColorTokens().Background {
		t.Fatalf("decoded = %+v", decoded)
	}
	var appearance ThemeAppearance
	if err := json.Unmarshal([]byte(`"dark"`), &appearance); err != nil || appearance != ThemeAppearanceDark {
		t.Fatalf("appearance = %v, %v", appearance, err)
	}
}

func stateRefinement(build func(*StateStyle) *StateStyle) gpui.StyleRefinement {
	return build(NewStateStyle()).intoRefinement()
}

// U:state_style.rs:63-70
func TestStateStyleInstanceStyleIsTheBaselineWhenNoStateIsActive(t *testing.T) {
	instance := stateRefinement(func(s *StateStyle) *StateStyle { return s.Opacity(0.9) })
	resolved := resolveStyle(&instance)
	if v, ok := resolved.Opacity.Get(); !ok || v != 0.9 {
		t.Fatalf("opacity = %v, %v", v, ok)
	}
}

// U:state_style.rs:72-80
func TestStateStyleAnActiveStateOverridesTheInstanceStyle(t *testing.T) {
	instance := stateRefinement(func(s *StateStyle) *StateStyle { return s.Opacity(0.9) })
	checked := stateRefinement(func(s *StateStyle) *StateStyle { return s.Opacity(0.8) })
	resolved := resolveStyle(&instance, &checked)
	if v, ok := resolved.Opacity.Get(); !ok || v != 0.8 {
		t.Fatalf("opacity = %v, %v", v, ok)
	}
}

// U:state_style.rs:82-91
func TestStateStyleLaterStatesOverrideEarlierStates(t *testing.T) {
	instance := stateRefinement(func(s *StateStyle) *StateStyle { return s.Opacity(0.9) })
	checked := stateRefinement(func(s *StateStyle) *StateStyle { return s.Opacity(0.8) })
	disabled := stateRefinement(func(s *StateStyle) *StateStyle { return s.Opacity(0.5) })
	resolved := resolveStyle(&instance, &checked, &disabled)
	if v, ok := resolved.Opacity.Get(); !ok || v != 0.5 {
		t.Fatalf("opacity = %v, %v", v, ok)
	}
}

// U:state_style.rs:93-102
func TestStateStyleStatesOnlyOverrideTheFieldsTheySet(t *testing.T) {
	instance := stateRefinement(func(s *StateStyle) *StateStyle { return s.Opacity(0.9).Border1() })
	disabled := stateRefinement(func(s *StateStyle) *StateStyle { return s.Opacity(0.5) })
	resolved := resolveStyle(&instance, &disabled)
	if v, ok := resolved.Opacity.Get(); !ok || v != 0.5 {
		t.Fatalf("opacity = %v, %v", v, ok)
	}
	if instance.BorderWidthsTop != resolved.BorderWidthsTop ||
		instance.BorderWidthsRight != resolved.BorderWidthsRight ||
		instance.BorderWidthsBottom != resolved.BorderWidthsBottom ||
		instance.BorderWidthsLeft != resolved.BorderWidthsLeft {
		t.Fatal("border widths changed")
	}
	if !resolved.BorderWidthsTop.IsSome() {
		t.Fatal("the instance border was dropped")
	}
}

// selectedOnlyTrigger only knows about selection, the way every trigger
// did before open existed (U:component_traits.rs:66-81).
type selectedOnlyTrigger struct{ selected bool }

func (s selectedOnlyTrigger) Selected(selected bool) selectedOnlyTrigger {
	s.selected = selected
	return s
}
func (s selectedOnlyTrigger) IsSelected() bool { return s.selected }

// U:component_traits.rs:83-92
func TestComponentTraitsOpenFallsBackToSelectedUnlessOverridden(t *testing.T) {
	trigger := SelectableOpen(selectedOnlyTrigger{selected: false}, true)
	if !trigger.IsSelected() || !SelectableIsOpen(trigger) {
		t.Fatal("open(true) did not select")
	}
	trigger = SelectableOpen(selectedOnlyTrigger{selected: true}, false)
	if trigger.IsSelected() || SelectableIsOpen(trigger) {
		t.Fatal("open(false) did not deselect")
	}
}

// customControl carries focus-ring state (U:component_traits.rs:62-64,
// 94-103).
type customControl struct{ focusRingEnabled bool }

func (c customControl) FocusRing(enabled bool) customControl {
	c.focusRingEnabled = enabled
	return c
}
func (c customControl) IsFocusRingEnabled() bool { return c.focusRingEnabled }

var _ FocusableExt[customControl] = customControl{}

// U:component_traits.rs:105-113
func TestComponentTraitsFocusRingAPICarriesStateWithoutVisuals(t *testing.T) {
	control := customControl{focusRingEnabled: true}.FocusRing(false)
	if control.IsFocusRingEnabled() {
		t.Fatal("focus ring still enabled")
	}
}

// browser-core 48/73: FocusRing(false) maps onto the core's outline
// opt-out, and FocusRing(true) leaves the outline at the default (the
// core's auto kind — the default focus ring on a focus-visible
// element). The trait stays state-only; this pins the visual layer's
// mapping helper.
func TestComponentTraitsFocusRingOutlineOptsTheCoreRingOut(t *testing.T) {
	disabled := FocusRingOutline(gpui.Div().Flex(), false)
	if style, ok := disabled.StyleMut().OutlineStyle.Get(); !ok || style != gpui.OutlineNone {
		t.Fatalf("FocusRing(false) outline style = %v, %v; want none", style, ok)
	}
	enabled := FocusRingOutline(gpui.Div().Flex().TrackFocus(gpui.FocusHandle{}), true)
	if _, ok := enabled.StyleMut().OutlineStyle.Get(); ok {
		t.Fatal("FocusRing(true) pinned an outline style; want the default auto kind")
	}
}

// U:actions.rs:34-38
func TestActionsConfirmKeepsItsSecondaryPayload(t *testing.T) {
	if !(Confirm{Secondary: true}).Secondary || (Confirm{Secondary: false}).Secondary {
		t.Fatal("payload lost")
	}
	// Go-own: the boxed action keeps the payload and the namespaced name.
	boxed := ConfirmAction.Box(Confirm{Secondary: true})
	if boxed.Name() != "ui::Confirm" || CancelAction.Name() != "ui::Cancel" {
		t.Fatalf("names = %q %q", boxed.Name(), CancelAction.Name())
	}
}

// U:index_path.rs:75-80
func TestIndexPathIntoElementID(t *testing.T) {
	id := NewIndexPath(2).WithSection(1).WithColumn(3).ElementID()
	if id.String() != "index-path(1,2,3)" {
		t.Fatalf("id = %q", id.String())
	}
}

// U:index_path.rs:82-88
func TestIndexPathDisplay(t *testing.T) {
	if got := NewIndexPath(2).WithSection(1).WithColumn(3).String(); got != "IndexPath(section: 1, row: 2, column: 3)" {
		t.Fatalf("display = %q", got)
	}
}

// U:index_path.rs:90-101
func TestIndexPath(t *testing.T) {
	var p IndexPath
	if p.Section != 0 || p.Row != 0 || p.Column != 0 {
		t.Fatalf("default = %+v", p)
	}
	p = p.WithSection(1).WithRow(2).WithColumn(3)
	if p.Section != 1 || p.Row != 2 || p.Column != 3 {
		t.Fatalf("built = %+v", p)
	}
}

// Go-own: geometry.rs has no upstream tests; the pinned conversions.
func TestGeometryPlacementAndLength(t *testing.T) {
	if PlacementTop.Axis() != gpui.AxisVertical || PlacementLeft.Axis() != gpui.AxisHorizontal {
		t.Fatal("placement axis")
	}
	if !PlacementRight.IsHorizontal() || PlacementBottom.IsHorizontal() || PlacementTop.String() != "Top" {
		t.Fatal("placement predicates")
	}
	if data, _ := json.Marshal(PlacementBottom); string(data) != `"bottom"` {
		t.Fatalf("placement json = %s", data)
	}
	if _, ok := LengthToPixels(gpui.AutoLength(), gpui.Px(100), 16); ok {
		t.Fatal("auto has pixels")
	}
	cases := []struct {
		length gpui.Length
		base   gpui.AbsoluteLength
		want   float32
	}{
		{gpui.PxLength(12), gpui.Px(100), 12},
		{gpui.RemsLength(2), gpui.Px(100), 32},
		{gpui.DefiniteLengthOf(gpui.Fraction(0.5)), gpui.Px(100), 50},
		{gpui.DefiniteLengthOf(gpui.Fraction(0.5)), gpui.RemsOf(10), 80},
	}
	for _, c := range cases {
		if got, ok := LengthToPixels(c.length, c.base, 16); !ok || got != c.want {
			t.Fatalf("%v of %v = %v, want %v", c.length, c.base, got, c.want)
		}
	}
	if e := EdgesAll(3); e.Top != 3 || e.Left != 3 || e.Right != 3 || e.Bottom != 3 {
		t.Fatalf("edges = %+v", e)
	}
}

// Go-own: measure.rs has no upstream tests; the environment gate.
func TestMeasureRunsTheBodyEitherWay(t *testing.T) {
	ran := 0
	MeasureIf("off", false, func() { ran++ })
	MeasureFunc("maybe", func() { ran++ })
	if ran != 2 {
		t.Fatalf("ran = %d", ran)
	}
	_, zed := os.LookupEnv("ZED_MEASUREMENTS")
	_, gpuiEnv := os.LookupEnv("GPUI_MEASUREMENTS")
	if MeasurementEnabled() != (zed || gpuiEnv) {
		t.Fatal("measurement_enabled disagrees with the environment")
	}
}

// Go-own: RoleOverride (U:styled.rs:6-32 has no upstream tests).
func TestStyledRoleOverrideResolves(t *testing.T) {
	implicit := func() gpui.Role { return gpui.RoleButton }
	if r, ok := RoleOverrideImplicit().Resolve(implicit); !ok || r != gpui.RoleButton {
		t.Fatal("implicit")
	}
	if _, ok := RoleOverridePresentational().Resolve(implicit); ok {
		t.Fatal("presentational")
	}
	if r, ok := RoleOverrideRole(gpui.RoleDialog).Resolve(implicit); !ok || r != gpui.RoleDialog {
		t.Fatal("role")
	}
	if !RoleOverrideFromOption(0, false).IsPresentational() || (RoleOverride{}) != RoleOverrideImplicit() {
		t.Fatal("from option / default")
	}
}
