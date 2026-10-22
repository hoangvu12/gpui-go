package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of auto_scroll.rs (2/2).

// U:auto_scroll.rs:132-142
func TestAutoScrollDeltaUsesDeadZoneAndSymmetricEdgeRamps(t *testing.T) {
	bounds := gpui.Bounds{Origin: gpui.Point{X: 0, Y: 100}, Size: gpui.Size{Width: 200, Height: 100}}

	if d := AutoScrollComputeDelta(150, bounds); d.IsSome() {
		t.Fatalf("dead zone delta = %v", d)
	}

	top, ok := AutoScrollComputeDelta(80, bounds).Get()
	if !ok {
		t.Fatal("no delta above the top edge")
	}
	bottom, ok := AutoScrollComputeDelta(220, bounds).Get()
	if !ok {
		t.Fatal("no delta below the bottom edge")
	}
	if top != -bottom {
		t.Fatalf("top = %v, bottom = %v: not symmetric", top, bottom)
	}
	if !(top < -12) {
		t.Fatalf("top = %v, want < -12", top)
	}
}

// U:auto_scroll.rs:144-154
func TestAutoScrollStopClearsDeltaAndDragPosition(t *testing.T) {
	scroll := DefaultAutoScroll()
	scroll.cell().set(gpui.Some[float32](20))
	scroll.LastDragPosition = gpui.Some(gpui.Point{X: 1, Y: 2})

	scroll.Stop()

	if scroll.IsActive() {
		t.Fatal("still active after stop")
	}
	if scroll.LastDragPosition.IsSome() {
		t.Fatalf("last drag position = %v", scroll.LastDragPosition)
	}
}

// Go-own: the tick task runs every 16ms on the scheduler clock and Set
// with no delta cancels it (the pinned drop of the Task).
func TestAutoScrollTicksOnTheClockAndStopsOnNone(t *testing.T) {
	ta := gpui.NewTestApp()
	type ticker struct {
		scroll AutoScroll
		ticks  []float32
	}
	var entity gpui.Entity[ticker]
	ta.Update(func(app *gpui.App) {
		entity = gpui.NewEntity(app, ta.RootScope(), func(*ticker, *gpui.Context[ticker]) {})
		entity.Update(app, func(s *ticker, cx *gpui.Context[ticker]) {
			s.scroll.Set(gpui.Some[float32](12), cx, func(delta float32, s *ticker, _ *gpui.Context[ticker]) {
				s.ticks = append(s.ticks, delta)
			})
		})
	})
	ta.RunUntilParked()
	ta.AdvanceClock(16)
	ta.AdvanceClock(16)
	count := func() int {
		return entity.Read(ta.App(), func(s *ticker, _ *gpui.App) int { return len(s.ticks) })
	}
	if n := count(); n != 2 {
		t.Fatalf("ticks after 32ms = %d, want 2", n)
	}
	ta.Update(func(app *gpui.App) {
		entity.Update(app, func(s *ticker, cx *gpui.Context[ticker]) {
			s.scroll.Set(gpui.Optional[float32]{}, cx, func(float32, *ticker, *gpui.Context[ticker]) {})
		})
	})
	ta.AdvanceClock(48)
	if n := count(); n != 2 {
		t.Fatalf("ticks after stop = %d, want 2", n)
	}
}
