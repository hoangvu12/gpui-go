package base

// browser-core ticket 74: reduced motion comes from the core's single
// source — App.MotionReduced, the App's own flag OR the live
// SystemPreferences record — with no Base-side platform read. The pinned
// preference record (gpui.TestApp.WithSystemPreferences) stands in for
// the record an attached host reads from Windows (SPI_GETCLIENTAREAANIMATION,
// refreshed on WM_SETTINGCHANGE).

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// systemReduced is the preference record with reduced motion on.
func systemReduced() gpui.SystemPreferences {
	return gpui.SystemPreferences{ReducedMotion: true}
}

// The OS preference alone — the App's own flag stays false — completes
// every preset on its first sample, with no frame requested.
func TestSystemReducedMotionCompletesPresetsImmediately(t *testing.T) {
	ta := gpui.NewTestApp().WithSystemPreferences(systemReduced())
	if ta.App().ReduceMotion() {
		t.Fatal("the pinned preference must not set the App's own flag")
	}

	// The keyed transition: both the first target change and a retarget
	// resolve at once.
	f := openFixture(t, ta, ms(100))
	if v := f.render(1); v != 1 {
		t.Fatalf("transition value = %v, want 1", v)
	}
	if v := f.render(2); v != 2 {
		t.Fatalf("retargeted value = %v, want 2 (no transition under reduced motion)", v)
	}
	if n := f.pendingFrame(); n != 0 {
		t.Fatalf("transition pending frames = %d, want 0", n)
	}

	// Presence enters and exits without pending frames.
	p := openPresenceFixture(t, ta, true)
	if phase := p.last().Phase; phase != PresencePhasePresent {
		t.Fatalf("presence phase = %v, want Present", phase)
	}
	if n := p.pendingFrame(); n != 0 {
		t.Fatalf("presence pending frames = %d, want 0", n)
	}
	if phase := p.render(false).Phase; phase != PresencePhaseAbsent {
		t.Fatalf("presence phase after exit = %v, want Absent", phase)
	}

	// A sequence adopts its last target.
	s := openSequenceFixture(t, ta, []SequenceStep[float32]{
		NewSequenceStep[float32](10, linearTransition(100)),
		NewSequenceStep[float32](20, linearTransition(100)),
	})
	s.assertSample(s.last(), 20, 1, MotionStatusFinished)
	if n := s.pendingFrame(); n != 0 {
		t.Fatalf("sequence pending frames = %d, want 0", n)
	}

	// A spring settles on the spot, retarget included.
	sp := openSpringFixture(t, ta, NewSpring(ms(350)).WithDamping(0.7))
	if v := sp.render(1); v != 1 {
		t.Fatalf("spring value = %v, want 1", v)
	}
	if v := sp.render(0); v != 0 {
		t.Fatalf("retargeted spring value = %v, want 0", v)
	}
	if n := sp.pendingFrame(); n != 0 {
		t.Fatalf("spring pending frames = %d, want 0", n)
	}
}

// A preference that turns on mid-flight resolves a moving preset at
// once: the gate reads the record on every sample, so there is no
// one-shot read to go stale (the read Base's reduce_motion.go used to
// make at Init).
func TestSystemReducedMotionResolvesAMovingPresetAtOnce(t *testing.T) {
	ta := gpui.NewTestApp()
	f := openFixture(t, ta, ms(100))
	if v := f.render(1); v != 0 {
		t.Fatalf("moving value = %v, want 0", v)
	}
	if n := f.pendingFrame(); n != 1 {
		t.Fatalf("pending frames = %d, want 1 (the transition is running)", n)
	}
	ta.RunUntilParked()

	ta.WithSystemPreferences(systemReduced())
	if v := f.render(1); v != 1 {
		t.Fatalf("value after the preference turned on = %v, want 1", v)
	}
	// Consume the frame the running transition had requested, then
	// nothing more is asked for.
	f.pendingFrame()
	ta.RunUntilParked()
	if n := f.pendingFrame(); n != 0 {
		t.Fatalf("pending frames = %d, want 0", n)
	}
}
