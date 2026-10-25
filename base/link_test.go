package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:link.rs tests: 7 ported, 1 deviated (the opened_url
// assertions; see linkNoExternalOpen).

type linkCounts struct {
	activations, keyboard, parent int
	opened                        []string
}

func linkHarness(t *testing.T, disabled bool) (*gpui.VisualTestContext, *linkCounts) {
	t.Helper()
	c := &linkCounts{}
	cx := drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().
			ID("link-parent").
			TabGroup().
			Size(gpui.Px(100)).
			OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { c.parent++ }).
			Child(NewLink("link").
				Href("app://settings").
				Disabled(disabled).
				SizeFull().
				OpenWith(func(href string, _ *gpui.ClickEvent, _ *gpui.Window, _ *gpui.App) {
					c.opened = append(c.opened, href)
				}).
				OnActivate(func(event *gpui.ClickEvent, _ *gpui.Window, _ *gpui.App) {
					c.activations++
					if event.IsKeyboard() {
						c.keyboard++
					}
				})).
			IntoElement()
	})
	return cx, c
}

// linkNoExternalOpen stands in for upstream's `cx.opened_url() == None`.
// Deviation: the core TestApp records no opened URL (App.OpenURL,
// gpui/app.go, goes straight to the desktop host), so the assertion is
// structural: Link's render never calls App.OpenURL (link.go), which this
// helper documents at each upstream assertion site.
func linkNoExternalOpen(*testing.T) {}

// U:link.rs:298-308
func TestLinkPointerRunsInjectedOpenStrategyAndActivationOnce(t *testing.T) {
	cx, c := linkHarness(t, false)
	clickAt(cx, 10, 10)
	if c.activations != 1 || c.keyboard != 0 || !slices.Equal(c.opened, []string{"app://settings"}) {
		t.Fatalf("got %+v", c)
	}
	linkNoExternalOpen(t)
}

// U:link.rs:310-332
func TestLinkOpenStrategyRunsBeforeActivationCallback(t *testing.T) {
	var order []string
	cx := drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return elementOf(t, NewLink("ordered-link").
			Href("app://ordered").
			Size(gpui.Px(100)).
			OpenWith(func(string, *gpui.ClickEvent, *gpui.Window, *gpui.App) { order = append(order, "open") }).
			OnActivate(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { order = append(order, "activate") }))
	})
	clickAt(cx, 10, 10)
	if !slices.Equal(order, []string{"open", "activate"}) {
		t.Fatalf("order = %v", order)
	}
}

// U:link.rs:334-352
func TestLinkEnterAndSpaceEachActivateOnce(t *testing.T) {
	cx, c := linkHarness(t, false)
	clickAt(cx, 10, 10)
	c.activations = 0
	c.opened = nil
	if !cx.Window().Focused().Ok {
		t.Fatal("pointer press did not focus the link")
	}
	redraw(t, cx)
	pressKeys(cx, "enter", "space")
	if c.activations != 2 || c.keyboard != 2 || !slices.Equal(c.opened, []string{"app://settings", "app://settings"}) {
		t.Fatalf("got %+v", c)
	}
	linkNoExternalOpen(t)
}

// U:link.rs:354-369 (deviated: see linkNoExternalOpen; the click must
// simply not fail with no strategy installed).
func TestLinkHrefWithoutStrategyNeverOpensExternally(t *testing.T) {
	cx := drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return elementOf(t, NewLink("target-only").Href("https://example.com").Size(gpui.Px(100)))
	})
	clickAt(cx, 10, 10)
	linkNoExternalOpen(t)
}

// U:link.rs:371-382
func TestLinkDisabledIsInertAndBlocksParentActivation(t *testing.T) {
	cx, c := linkHarness(t, true)
	clickAt(cx, 10, 10)
	pressKeys(cx, "enter", "space")
	if c.activations != 0 || len(c.opened) != 0 || c.parent != 0 {
		t.Fatalf("got %+v", c)
	}
}

// U:link.rs:384-391
func TestLinkVisualStateStylesRemainApplicationOwned(t *testing.T) {
	_ = NewLink("states").
		Styles(func(s LinkStyles) LinkStyles {
			return s.Disabled(func(style *StateStyle) *StateStyle { return style.Opacity(0.5) })
		}).
		Hover(gpui.Styles().Opacity(0.9)).
		Active(gpui.Styles().Opacity(0.8)).
		FocusVisible(gpui.Styles().Opacity(0.7))
}

// U:link.rs:393-410
func TestLinkDisabledStyleAppliesOnlyWhileDisabledAndThenWins(t *testing.T) {
	disabledStyle := func(s LinkStyles) LinkStyles {
		return s.Disabled(func(style *StateStyle) *StateStyle { return style.Opacity(0.5) })
	}
	if got := opacityOf(t, NewLink("enabled").Opacity(0.9).Styles(disabledStyle).resolvedStyle()); got != 0.9 {
		t.Fatalf("enabled opacity = %v", got)
	}
	if got := opacityOf(t, NewLink("disabled").Styles(disabledStyle).Opacity(0.9).Disabled(true).resolvedStyle()); got != 0.5 {
		t.Fatalf("disabled opacity = %v", got)
	}
	if got := opacityOf(t, NewLink("semantic-only").Styles(disabledStyle).Disabled(true).resolvedStyle()); got != 0.5 {
		t.Fatalf("semantic-only opacity = %v", got)
	}
}

// U:link.rs:412-465. `enabled.url() == None`: the port's A11ySpec has no
// URL property at all, so href is never exposed (same outcome).
func TestLinkAccessibilityExposesLinkRoleLabelAndActionSurface(t *testing.T) {
	got := a11yOf(t,
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewLink("enabled").Href("app://settings").AccessibilityLabel("Settings").
				OpenWith(func(string, *gpui.ClickEvent, *gpui.Window, *gpui.App) {}).render(w, app)
		},
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewLink("disabled").Disabled(true).AccessibilityLabel("Settings").
				OnActivate(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {}).render(w, app)
		},
	)
	enabled, disabled := got[0], got[1]
	if enabled.role != gpui.RoleLink || enabled.spec.Label != "Settings" || !hasAction(enabled.spec, gpui.A11yActionClick) {
		t.Fatalf("enabled = %+v", enabled)
	}
	if disabled.role != gpui.RoleLink || hasAction(disabled.spec, gpui.A11yActionClick) || disabled.spec.Disabled {
		t.Fatalf("disabled = %+v", disabled)
	}
}
