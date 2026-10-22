package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// tooltip.rs tests (3/3 ported, U:tooltip.rs:281-347) plus one Go-own
// test of the delayed show and the grace-period hide on the virtual
// clock.

func tooltipTestBounds(x, y, w, h float32) gpui.Bounds {
	return gpui.Bounds{Origin: ovPoint(x, y), Size: gpui.Size{Width: w, Height: h}}
}

func newTooltipTestState(ta *gpui.TestApp, overlay TooltipOverlay) gpui.Entity[TooltipOverlay] {
	var state gpui.Entity[TooltipOverlay]
	ta.Update(func(app *gpui.App) {
		state = gpui.NewEntity(app, ta.RootScope(), func(s *TooltipOverlay, _ *gpui.Context[TooltipOverlay]) { *s = overlay })
	})
	return state
}

func tooltipRead[R any](cx *gpui.VisualTestContext, state gpui.Entity[TooltipOverlay], f func(*TooltipOverlay) R) R {
	var out R
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		out = state.Read(app, func(o *TooltipOverlay, _ *gpui.App) R { return f(o) })
	})
	return out
}

// U:tooltip.rs:290-312
func TestTooltipProviderOwnsGraceSwitchAndDismiss(t *testing.T) {
	ta := gpui.NewTestApp()
	state := newTooltipTestState(ta, NewTooltipOverlay())
	cx := ta.AddEmptyWindow()
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state.Update(app, func(o *TooltipOverlay, cx *gpui.Context[TooltipOverlay]) {
			o.hadRecentTooltip = true
			o.RequestShow(NewTooltipRequest(tooltipTestBounds(0, 0, 20, 20), func(*gpui.Window, *gpui.App) gpui.View {
				panic("content is not rendered by this lifecycle test")
			}), w, cx)
		})
	})
	if !tooltipRead(cx, state, func(o *TooltipOverlay) bool { return o.content != nil }) {
		t.Fatal("a request within the grace period did not show immediately")
	}

	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		state.Update(app, func(o *TooltipOverlay, cx *gpui.Context[TooltipOverlay]) { o.Hide(cx) })
	})
	if tooltipRead(cx, state, func(o *TooltipOverlay) bool { return o.content != nil }) {
		t.Fatal("hide left the content")
	}
}

// U:tooltip.rs:314-317
func TestTooltipPriorityExceedsPopupLayer(t *testing.T) {
	if !(tooltipPriority > PopupPriority) {
		t.Fatalf("tooltip priority %d <= popup priority %d", tooltipPriority, PopupPriority)
	}
}

// U:tooltip.rs:319-346
func TestTooltipDisabledProviderIgnoresDelayedAndImmediateRequests(t *testing.T) {
	ta := gpui.NewTestApp()
	overlay := NewTooltipOverlay()
	overlay.enabled = false
	state := newTooltipTestState(ta, overlay)
	cx := ta.AddEmptyWindow()
	for _, hadRecent := range []bool{false, true} {
		cx.Update(func(w *gpui.Window, app *gpui.App) {
			state.Update(app, func(o *TooltipOverlay, cx *gpui.Context[TooltipOverlay]) {
				o.hadRecentTooltip = hadRecent
				o.RequestShow(NewTooltipRequest(tooltipTestBounds(0, 0, 20, 20), func(*gpui.Window, *gpui.App) gpui.View {
					panic("disabled tooltips must not build content")
				}), w, cx)
				if o.content != nil || o.showTask != nil || o.hideTask != nil || o.animationEpoch != 0 {
					t.Fatalf("disabled provider changed state (hadRecent=%v)", hadRecent)
				}
			})
		})
	}
}

// Go-own: the show waits for the show delay and the hide for the grace
// period on the scheduler clock (U:tooltip.rs:162-195).
func TestTooltipDelayedShowAndGraceHideFollowTheClock(t *testing.T) {
	ta := gpui.NewTestApp()
	state := newTooltipTestState(ta, NewTooltipOverlay())
	cx := ta.AddEmptyWindow()
	visible := func() bool { return tooltipRead(cx, state, func(o *TooltipOverlay) bool { return o.content != nil }) }
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state.Update(app, func(o *TooltipOverlay, cx *gpui.Context[TooltipOverlay]) {
			o.RequestShow(NewTooltipRequest(tooltipTestBounds(0, 0, 20, 20), func(*gpui.Window, *gpui.App) gpui.View {
				return nil
			}), w, cx)
		})
	})
	cx.RunUntilParked()
	if visible() {
		t.Fatal("the tooltip showed before the show delay")
	}
	cx.AdvanceClock(tooltipShowDelay.Milliseconds() - 1)
	if visible() {
		t.Fatal("the tooltip showed before the show delay elapsed")
	}
	cx.AdvanceClock(1)
	if !visible() {
		t.Fatal("the tooltip did not show after the show delay")
	}

	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state.Update(app, func(o *TooltipOverlay, cx *gpui.Context[TooltipOverlay]) { o.RequestHide(w, cx) })
	})
	cx.AdvanceClock(tooltipGracePeriod.Milliseconds() - 1)
	if !visible() {
		t.Fatal("the tooltip hid before the grace period elapsed")
	}
	cx.AdvanceClock(1)
	if visible() {
		t.Fatal("the tooltip did not hide after the grace period")
	}
}
