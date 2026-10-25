package base

import (
	"reflect"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:button.rs tests (8/8). The harness shapes are shared by the
// other simple-control tests (controls_harness_test helpers below).

// drawn opens a test view, draws it once and returns its context (the
// upstream `cx.add_window_view(...)` + `window.draw(cx).clear(cx)`).
func drawn(t *testing.T, build func(w *gpui.Window, cx *gpui.Context[testView]) gpui.AnyElement) *gpui.VisualTestContext {
	t.Helper()
	ta := gpui.NewTestApp()
	_, cx := openTestView(t, ta, build)
	cx.RunUntilParked()
	if _, err := gpui.DrawWindowFrame(cx.Window()); err != nil {
		t.Fatal(err)
	}
	return cx
}

// redraw draws the window again after a state change.
func redraw(t *testing.T, cx *gpui.VisualTestContext) {
	t.Helper()
	cx.RunUntilParked()
	if _, err := gpui.DrawWindowFrame(cx.Window()); err != nil {
		t.Fatal(err)
	}
}

// elementOf converts a component (a RenderOnce recipe) into an element
// for a test root.
func elementOf(t *testing.T, value any) gpui.AnyElement {
	t.Helper()
	el, err := gpui.TryIntoElement(value)
	if err != nil {
		t.Fatal(err)
	}
	return el
}

// clickAt simulates a left click (upstream `cx.simulate_click(point,
// Modifiers::default())`).
func clickAt(cx *gpui.VisualTestContext, x, y float32) {
	cx.SimulateClick(gpui.Point{X: x, Y: y}, gpui.Modifiers{})
}

// pressKeys sends a key down and up per key (the upstream
// simulate_event(KeyDownEvent) + simulate_event(KeyUpEvent) loop).
func pressKeys(cx *gpui.VisualTestContext, keys ...string) {
	for _, key := range keys {
		k := gpui.MustParseKeystroke(key)
		cx.SimulateEvent(gpui.KeyDownEvent{Keystroke: k})
		cx.SimulateEvent(gpui.KeyUpEvent{Keystroke: k})
	}
}

// hasAction reports whether spec advertises action.
func hasAction(spec gpui.A11ySpec, action gpui.A11yAction) bool {
	for _, a := range spec.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// a11yOf captures the rendered root div's role and a11y info during a
// canvas prepaint (the upstream canvas + `render(window, cx)
// .into_element().write_a11y_info(&mut node)` probe).
func a11yOf(t *testing.T, renders ...func(w *gpui.Window, app *gpui.App) *gpui.DivElement) []a11yProbe {
	t.Helper()
	var got []a11yProbe
	drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Canvas(func(_ gpui.Bounds, w *gpui.Window, app *gpui.App) struct{} {
			got = got[:0]
			for _, render := range renders {
				d := render(w, app)
				var p a11yProbe
				p.role, p.hasRole = d.A11yRole()
				d.WriteA11yInfo(&p.spec)
				got = append(got, p)
			}
			return struct{}{}
		}, func(gpui.Bounds, struct{}, *gpui.Window, *gpui.App) {}).Flex().SizeFull().IntoElement()
	})
	if len(got) != len(renders) {
		t.Fatalf("captured %d a11y nodes, want %d", len(got), len(renders))
	}
	return got
}

type a11yProbe struct {
	role    gpui.Role
	hasRole bool
	spec    gpui.A11ySpec
}

type buttonCounts struct{ button, parent, keyboard int }

func buttonHarness(t *testing.T, disabled bool) (*gpui.VisualTestContext, *buttonCounts) {
	t.Helper()
	counts := &buttonCounts{}
	cx := drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().
			ID("button-parent").
			TabGroup().
			Size(gpui.Px(100)).
			OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { counts.parent++ }).
			Child(NewButton("button").
				Disabled(disabled).
				SizeFull().
				OnClick(func(event *gpui.ClickEvent, _ *gpui.Window, _ *gpui.App) {
					counts.button++
					if event.IsKeyboard() {
						counts.keyboard++
					}
				})).
			IntoElement()
	})
	return cx, counts
}

// U:button.rs:336-344
func TestButtonPointerActivationFiresOnce(t *testing.T) {
	cx, counts := buttonHarness(t, false)
	clickAt(cx, 10, 10)
	if counts.button != 1 || counts.keyboard != 0 {
		t.Fatalf("clicks = %d, keyboard = %d; want 1, 0", counts.button, counts.keyboard)
	}
}

// U:button.rs:346-368
func TestButtonEnterAndSpaceUseOneNativeKeyboardClickEach(t *testing.T) {
	cx, counts := buttonHarness(t, false)
	clickAt(cx, 10, 10)
	counts.button = 0
	if !cx.Window().Focused().Ok {
		t.Fatal("pointer press did not focus the button")
	}
	redraw(t, cx)
	pressKeys(cx, "enter", "space")
	if counts.button != 2 || counts.keyboard != 2 {
		t.Fatalf("clicks = %d, keyboard = %d; want 2, 2", counts.button, counts.keyboard)
	}
}

// U:button.rs:370-380
func TestButtonDisabledIsInertAndBlocksParentActivation(t *testing.T) {
	cx, counts := buttonHarness(t, true)
	clickAt(cx, 10, 10)
	cx.Update(func(w *gpui.Window, _ *gpui.App) { w.FocusNext() })
	cx.SimulateKeystrokes("enter space")
	if counts.button != 0 || counts.parent != 0 {
		t.Fatalf("button = %d, parent = %d; want 0, 0", counts.button, counts.parent)
	}
}

// U:button.rs:382-429
func TestButtonFixedHeightCentersOrdinaryChildGeometry(t *testing.T) {
	var root, child gpui.Bounds
	var haveRoot, haveChild bool
	drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		b := ElementExtOf(NewButton("alignment-button").
			W(gpui.Px(120)).
			H(gpui.Px(40)).
			Child(ElementExtOf(gpui.Div().Flex().W(gpui.Px(48)).H(gpui.Px(12))).
				OnPrepaint(func(bounds gpui.Bounds, _ *gpui.Window, _ *gpui.App) { child, haveChild = bounds, true }))).
			OnPrepaint(func(bounds gpui.Bounds, _ *gpui.Window, _ *gpui.App) { root, haveRoot = bounds, true })
		return elementOf(t, b)
	})
	if !haveRoot || !haveChild {
		t.Fatalf("bounds captured: root %v child %v", haveRoot, haveChild)
	}
	if child.Center() != root.Center() {
		t.Fatalf("child center %v != button center %v", child.Center(), root.Center())
	}
}

// U:button.rs:431-446
func TestButtonStateStylingMethodsAreAvailableToApplications(t *testing.T) {
	opacity := float32(0.4)
	_ = NewButton("states").
		Styles(func(s ButtonStyles) ButtonStyles {
			return s.Disabled(func(style *StateStyle) *StateStyle {
				return style.
					Opacity(0.5).
					When(true, func(s *StateStyle) *StateStyle { return s.Border1() }).
					WhenSome(&opacity, func(s *StateStyle, o float32) *StateStyle { return s.Opacity(o) }).
					WhenNone((*float32)(nil), func(s *StateStyle) *StateStyle { return s.RoundedSm() })
			})
		}).
		Hover(gpui.Styles().Opacity(0.9)).
		Active(gpui.Styles().Opacity(0.8)).
		FocusVisible(gpui.Styles().Opacity(0.7))
}

func opacityOf(t *testing.T, r gpui.StyleRefinement) float32 {
	t.Helper()
	v, ok := r.Opacity.Get()
	if !ok {
		t.Fatal("opacity not set")
	}
	return v
}

// U:button.rs:448-465
func TestButtonDisabledStyleAppliesOnlyWhileDisabledAndThenWins(t *testing.T) {
	disabledStyle := func(s ButtonStyles) ButtonStyles {
		return s.Disabled(func(style *StateStyle) *StateStyle { return style.Opacity(0.5) })
	}
	enabled := NewButton("enabled").Opacity(0.9).Styles(disabledStyle)
	if got := opacityOf(t, enabled.resolvedStyle()); got != 0.9 {
		t.Fatalf("enabled opacity = %v, want 0.9", got)
	}
	disabled := NewButton("disabled").Styles(disabledStyle).Opacity(0.9).Disabled(true)
	if got := opacityOf(t, disabled.resolvedStyle()); got != 0.5 {
		t.Fatalf("disabled opacity = %v, want 0.5", got)
	}
	semanticOnly := NewButton("semantic-only").Styles(disabledStyle).Disabled(true)
	if got := opacityOf(t, semanticOnly.resolvedStyle()); got != 0.5 {
		t.Fatalf("semantic-only opacity = %v, want 0.5", got)
	}
}

// wantBackground asserts a refinement's background.
func wantBackground(t *testing.T, name string, r gpui.StyleRefinement, want *gpui.Hsla) {
	t.Helper()
	got, ok := r.Background.Get()
	if want == nil {
		if ok {
			t.Fatalf("%s: background = %v, want none", name, got)
		}
		return
	}
	if !ok || !reflect.DeepEqual(got, gpui.BackgroundOf(*want)) {
		t.Fatalf("%s: background = %v (set %v), want %v", name, got, ok, *want)
	}
}

// U:button.rs:467-498
func TestButtonSelectedDisabledAndInstanceStylesFollowTheSharedPriority(t *testing.T) {
	selectedColor := gpui.Hsla{H: 0.6, S: 0.7, L: 0.5, A: 1}
	disabledColor := gpui.Hsla{H: 0.1, S: 0.2, L: 0.3, A: 0.5}
	button := func(selected, disabled bool) *Button {
		return NewButton("button").Selected(selected).Disabled(disabled).
			Styles(func(s ButtonStyles) ButtonStyles {
				return s.
					Selected(func(style *StateStyle) *StateStyle { return style.Bg(selectedColor) }).
					Disabled(func(style *StateStyle) *StateStyle { return style.Bg(disabledColor) })
			})
	}
	wantBackground(t, "plain", button(false, false).resolvedStyle(), nil)
	wantBackground(t, "selected", button(true, false).resolvedStyle(), &selectedColor)
	wantBackground(t, "selected+disabled", button(true, true).resolvedStyle(), &disabledColor)
	wantBackground(t, "instance bg", button(true, true).Bg(selectedColor).resolvedStyle(), &disabledColor)
}

// U:button.rs:500-557
func TestButtonAccessibilityRoleLabelAndDisabledActionSurface(t *testing.T) {
	got := a11yOf(t,
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewButton("enabled").AccessibilityLabel("Save").
				OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {}).render(w, app)
		},
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewButton("disabled").Disabled(true).AccessibilityLabel("Save").
				OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {}).render(w, app)
		},
	)
	enabled, disabled := got[0], got[1]
	if enabled.role != gpui.RoleButton || enabled.spec.Label != "Save" || !hasAction(enabled.spec, gpui.A11yActionClick) {
		t.Fatalf("enabled = %+v", enabled)
	}
	if disabled.role != gpui.RoleButton || hasAction(disabled.spec, gpui.A11yActionClick) {
		t.Fatalf("disabled = %+v", disabled)
	}
	// Upstream records that GPUI's button has no aria-disabled setter
	// (U:button.rs:553-556); the port keeps the same surface.
	if disabled.spec.Disabled {
		t.Fatal("disabled button reports the Disabled flag")
	}
}
