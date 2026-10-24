package gpui

// The keyed motion engine's core-surface tests (browser-core ticket 74):
// TransitionFromWithStatus, the presence-seeding seam Base's Presence
// runs on, and App.MotionReduced, the single reduced-motion source the
// engine and Base's presets gate on. The engine's timing rules are pinned
// by Base's motion tests through the aliases (ticket 44); these tests pin
// the new seams themselves.

import (
	"testing"
	"time"
)

// linearEase is the identity easing.
func linearEase(p float32) float32 { return p }

// motionEngineView renders its closure each frame (Base's motionView).
type motionEngineView struct {
	render func(w *Window, cx *App) AnyElement
}

// Render implements Render: the closure's element, or Empty.
func (v *motionEngineView) Render(w *Window, cx *Context[motionEngineView]) AnyElement {
	if el := v.render(w, cx.App()); el != (AnyElement{}) {
		return el
	}
	return Empty()
}

// openMotionEngineWindow opens a 100x100 test window that renders render,
// running until parked (the window's first render is the closure's first
// sample).
func openMotionEngineWindow(t *testing.T, ta *TestApp, render func(w *Window, cx *App) AnyElement) *Window {
	t.Helper()
	win := ta.OpenWindowWithOptions[motionEngineView](WindowOptions{Bounds: Bounds{Size: Size{Width: 100, Height: 100}}},
		func(v *motionEngineView, _ *Window, _ *Context[motionEngineView]) { v.render = render })
	return win.Window()
}

// refreshMotionEngine re-renders the window like Base's motionWindow.
func refreshMotionEngine(ta *TestApp, w *Window) {
	ta.Update(func(*App) { w.Refresh() })
	ta.RunUntilParked()
}

// TestTransitionFromWithStatusSeedsItsKeyedStateFromFrom pins the
// presence seeding: the first sample of a keyed state begins a transition
// from `from` toward the target instead of adopting it, and later
// samples follow TransitionWithStatus's retarget and reversal rules
// unchanged (U:motion/presence.rs seeds its state this way).
func TestTransitionFromWithStatusSeedsItsKeyedStateFromFrom(t *testing.T) {
	ta := NewTestApp()
	target := float32(10)
	var samples []MotionValue[float32]
	w := openMotionEngineWindow(t, ta, func(w *Window, cx *App) AnyElement {
		samples = append(samples, TransitionFromWithStatus("seeded", float32(0), target,
			NewTransition(100*time.Millisecond).Ease(linearEase), w, cx))
		return AnyElement{}
	})
	last := func() MotionValue[float32] { return samples[len(samples)-1] }

	// The first sample starts at from (0) toward the target (10) —
	// Running, with a frame requested: the enter half of presence.
	if last().Value != 0 || last().Status != MotionStatusRunning {
		t.Fatalf("first sample = %+v, want 0 Running", last())
	}

	// Halfway.
	ta.AdvanceClock(50)
	refreshMotionEngine(ta, w)
	if last().Value != 5 {
		t.Fatalf("value = %v, want 5", last().Value)
	}

	// A direct reversal shortens the return by the eased progress made
	// (the reversing-shortening rule): 100ms × 0.5 = a 50ms return.
	target = 0
	refreshMotionEngine(ta, w)
	if last().Value != 5 || last().Status != MotionStatusRunning {
		t.Fatalf("reversal start = %+v, want 5 Running", last())
	}
	ta.AdvanceClock(25)
	refreshMotionEngine(ta, w)
	if last().Value != 2.5 {
		t.Fatalf("reversed value = %v, want 2.5", last().Value)
	}
	ta.AdvanceClock(25)
	refreshMotionEngine(ta, w)
	if last().Value != 0 || last().Status != MotionStatusFinished {
		t.Fatalf("final sample = %+v, want 0 Finished", last())
	}
}

// TestTransitionFromWithStatusRunsOutItsDurationWhenFromEqualsTarget
// pins the mount-clock rule a transit driven by an outgoing presence
// measures (Base's nav_stack): a seeded state whose from equals its
// target still reports Running and requests frames until the policy
// duration has elapsed, where the adopting form reports Idle at rest.
func TestTransitionFromWithStatusRunsOutItsDurationWhenFromEqualsTarget(t *testing.T) {
	ta := NewTestApp()
	var samples []MotionValue[float32]
	w := openMotionEngineWindow(t, ta, func(w *Window, cx *App) AnyElement {
		samples = append(samples, TransitionFromWithStatus("seeded-clock", float32(0), float32(0),
			NewTransition(100*time.Millisecond).Ease(linearEase), w, cx))
		return AnyElement{}
	})
	last := func() MotionValue[float32] { return samples[len(samples)-1] }
	if last().Status != MotionStatusRunning || last().Value != 0 {
		t.Fatalf("first sample = %+v, want 0 Running", last())
	}
	if n := w.SimulateNextFrame(ta.App()); n != 1 {
		t.Fatalf("pending frames = %d, want 1", n)
	}
	ta.RunUntilParked()
	ta.AdvanceClock(100)
	refreshMotionEngine(ta, w)
	if last().Status != MotionStatusFinished || last().Value != 0 {
		t.Fatalf("final sample = %+v, want 0 Finished", last())
	}
	refreshMotionEngine(ta, w)
	if last().Status != MotionStatusFinished {
		t.Fatalf("the finished clock must stay finished, got %v", last().Status)
	}
}

// TestTransitionWithStatusStillAdoptsItsFirstTarget pins the original
// entry point after the refactor: the first target is adopted at once
// (from == target, so the status is Idle), unlike the seeded form.
func TestTransitionWithStatusStillAdoptsItsFirstTarget(t *testing.T) {
	ta := NewTestApp()
	var samples []MotionValue[float32]
	openMotionEngineWindow(t, ta, func(w *Window, cx *App) AnyElement {
		samples = append(samples, TransitionWithStatus("adopted", float32(10),
			NewTransition(100*time.Millisecond).Ease(linearEase), w, cx))
		return AnyElement{}
	})
	if samples[0].Value != 10 || samples[0].Status != MotionStatusIdle {
		t.Fatalf("first sample = %+v, want 10 Idle", samples[0])
	}
}

// TestMotionReducedIsTheAppFlagOrTheSystemPreference pins the single
// reduced-motion source: the App's own flag, or the OS preference through
// SystemPreferences — pinned for a hostless application by
// TestApp.WithSystemPreferences, the record an attached host would read.
func TestMotionReducedIsTheAppFlagOrTheSystemPreference(t *testing.T) {
	ta := NewTestApp()
	if ta.App().MotionReduced() {
		t.Fatal("motion is reduced by default")
	}
	ta.Update(func(app *App) {
		app.SetReduceMotion(true)
		if !app.MotionReduced() {
			t.Fatal("the app flag did not reduce motion")
		}
		app.SetReduceMotion(false)
		if app.MotionReduced() {
			t.Fatal("the flag change did not clear the gate")
		}
	})
	pinned := NewTestApp().WithSystemPreferences(SystemPreferences{ReducedMotion: true})
	if pinned.App().ReduceMotion() {
		t.Fatal("the pinned preference must not touch the App's own flag")
	}
	if !pinned.App().MotionReduced() {
		t.Fatal("the system preference did not reduce motion")
	}
}

// TestTransitionFromWithStatusCompletesAtOnceUnderTheSystemPreference
// pins that the engine honours the gate through the preference record:
// the seeded transition resolves on its first sample and never requests
// a frame.
func TestTransitionFromWithStatusCompletesAtOnceUnderTheSystemPreference(t *testing.T) {
	ta := NewTestApp().WithSystemPreferences(SystemPreferences{ReducedMotion: true})
	var samples []MotionValue[float32]
	w := openMotionEngineWindow(t, ta, func(w *Window, cx *App) AnyElement {
		samples = append(samples, TransitionFromWithStatus("seeded-reduced", float32(0), float32(10),
			NewTransition(100*time.Millisecond), w, cx))
		return AnyElement{}
	})
	if samples[0].Value != 10 || samples[0].Status != MotionStatusFinished {
		t.Fatalf("sample = %+v, want 10 Finished", samples[0])
	}
	if n := w.SimulateNextFrame(ta.App()); n != 0 {
		t.Fatalf("pending frames = %d, want 0", n)
	}
}
