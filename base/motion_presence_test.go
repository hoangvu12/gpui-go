package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// The presence tests of motion.rs (3), on the virtual clock.

// presenceFixture is U:motion.rs:1073-1124 (PresenceView / PresenceFixture).
type presenceFixture struct {
	*motionWindow
	present bool
	samples []PresenceSample
}

func openPresenceFixture(t *testing.T, ta *gpui.TestApp, initiallyPresent bool) *presenceFixture {
	f := &presenceFixture{present: initiallyPresent}
	f.motionWindow = openMotionWindow(t, ta, func(w *gpui.Window, cx *gpui.App) gpui.AnyElement {
		f.samples = append(f.samples, NewPresence("presence-test", f.present).
			Transition(NewTransition(ms(100)).Ease(linear)).
			Sample(w, cx))
		return gpui.AnyElement{}
	})
	return f
}

func (f *presenceFixture) last() PresenceSample { return f.samples[len(f.samples)-1] }

func (f *presenceFixture) render(present bool) PresenceSample {
	f.present = present
	f.refresh()
	return f.last()
}

// U:motion.rs:1126-1149
func TestPresenceEntersExitsAndOnlyUnmountsAfterExit(t *testing.T) {
	ta := gpui.NewTestApp()
	f := openPresenceFixture(t, ta, true)
	if s := f.last(); s.Phase != PresencePhaseEntering || s.Progress != 0 || !s.ShouldRender() {
		t.Fatalf("entering = %+v", s)
	}

	ta.AdvanceClock(100)
	if s := f.render(true); s.Phase != PresencePhasePresent || s.Progress != 1 {
		t.Fatalf("present = %+v", s)
	}

	if s := f.render(false); s.Phase != PresencePhaseExiting || s.Progress != 1 || !s.ShouldRender() {
		t.Fatalf("exiting = %+v", s)
	}

	ta.AdvanceClock(100)
	if s := f.render(false); s.Phase != PresencePhaseAbsent || s.Progress != 0 || s.ShouldRender() {
		t.Fatalf("absent = %+v", s)
	}
}

// U:motion.rs:1151-1162
func TestPresenceReentryReversesFromTheExitSample(t *testing.T) {
	ta := gpui.NewTestApp()
	f := openPresenceFixture(t, ta, true)
	ta.AdvanceClock(100)
	f.render(true)
	f.render(false)
	ta.AdvanceClock(40)
	if s := f.render(true); s.Phase != PresencePhaseEntering || s.Progress != 0.6 {
		t.Fatalf("reentering = %+v", s)
	}
}

// U:motion.rs:1164-1180
func TestReducedMotionResolvesPresenceWithoutAPendingFrame(t *testing.T) {
	ta := gpui.NewTestApp()
	reduceMotion(ta)
	f := openPresenceFixture(t, ta, true)
	if p := f.last().Phase; p != PresencePhasePresent {
		t.Fatalf("phase = %v, want Present", p)
	}
	if n := f.pendingFrame(); n != 0 {
		t.Fatalf("pending frames = %d, want 0", n)
	}
	if p := f.render(false).Phase; p != PresencePhaseAbsent {
		t.Fatalf("phase = %v, want Absent", p)
	}
}
