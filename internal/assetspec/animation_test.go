package assetspec

import (
	"testing"
	"time"

	"gpui-go/gpui"
)

// animatedImage builds a synthetic decoded image with exact rational
// delays — the deterministic playback fixture (no native calls).
func animatedImage(delays ...time.Duration) *gpui.RenderImage {
	frames := make([]gpui.ImageFrame, len(delays))
	for i, delay := range delays {
		frames[i] = gpui.ImageFrame{
			Width:  2,
			Height: 1,
			Pixels: []byte{0, 0, 0, 255, 0, 0, 0, 255},
			Delay:  delay,
		}
	}
	return &gpui.RenderImage{Frames: frames}
}

// activePolicy is the animating configuration (motion allowed, window
// active).
func activePolicy() gpui.ImagePlaybackPolicy {
	return gpui.ImagePlaybackPolicy{ReduceMotion: false, WindowActive: true}
}

// TestPlaybackAdvanceRational pins the pinned frame advance (img.rs
// lines 343-371): the first render only records the time, a frame
// advances once its rational delay elapses, one render is one step,
// and the last_frame_time backdating lets a long gap catch up.
func TestPlaybackAdvanceRational(t *testing.T) {
	image := animatedImage(40*time.Millisecond, 100*time.Millisecond, 250*time.Millisecond)
	state := gpui.ImagePlayback{}

	// First advance: no last frame time — record it, keep frame 0.
	if got := state.Advance(image, 0, activePolicy()); got != 0 {
		t.Fatalf("first frame = %d, want 0", got)
	}

	// 39ms elapsed: below the 40ms frame delay — no advance.
	if got := state.Advance(image, 39*time.Millisecond, activePolicy()); got != 0 {
		t.Fatalf("frame at 39ms = %d, want 0", got)
	}
	// 40ms elapsed: exactly the delay — advance (elapsed >= duration).
	if got := state.Advance(image, 40*time.Millisecond, activePolicy()); got != 1 {
		t.Fatalf("frame at 40ms = %d, want 1", got)
	}
	// 139ms since the (backdated) last frame: below 100ms — no advance.
	if got := state.Advance(image, 139*time.Millisecond, activePolicy()); got != 1 {
		t.Fatalf("frame at 139ms = %d, want 1", got)
	}
	// 140ms: the 100ms frame delay elapsed — advance to the 250ms frame.
	if got := state.Advance(image, 140*time.Millisecond, activePolicy()); got != 2 {
		t.Fatalf("frame at 140ms = %d, want 2", got)
	}
	// 390ms: the 250ms delay elapsed — wraparound to frame 0.
	if got := state.Advance(image, 390*time.Millisecond, activePolicy()); got != 0 {
		t.Fatalf("frame at 390ms = %d, want 0 (wraparound)", got)
	}

	// The backdated catch-up: a 600ms gap over a 40ms frame advances
	// once, and the NEXT render advances immediately (the pinned
	// current_time - (elapsed - frame_duration)).
	state = gpui.ImagePlayback{}
	state.Advance(image, 0, activePolicy())
	if got := state.Advance(image, 600*time.Millisecond, activePolicy()); got != 1 {
		t.Fatalf("one step for a long gap = %d, want 1", got)
	}
	if got := state.Advance(image, 600*time.Millisecond, activePolicy()); got != 2 {
		t.Fatalf("the backdated catch-up = %d, want 2", got)
	}
}

// TestPlaybackPolicies pins the reduce-motion and active-window
// policies (img.rs lines 345-371): either freezes the frame and clears
// the last frame time; the animation-frame request follows the same
// conjunction.
func TestPlaybackPolicies(t *testing.T) {
	image := animatedImage(40*time.Millisecond, 100*time.Millisecond)
	state := gpui.ImagePlayback{}
	state.Advance(image, 0, activePolicy())

	// Reduce motion: no advance, the frame time clears.
	reduced := activePolicy()
	reduced.ReduceMotion = true
	if got := state.Advance(image, time.Hour, reduced); got != 0 {
		t.Fatalf("reduce-motion frame = %d, want 0", got)
	}
	// Back on an active window, playback restarts from the recorded
	// time (the pin sets last_frame_time only when it was None).
	if got := state.Advance(image, time.Hour, activePolicy()); got != 0 {
		t.Fatalf("frame after the motion pause = %d, want 0 (the pause cleared the frame time)", got)
	}
	if got := state.Advance(image, time.Hour+40*time.Millisecond, activePolicy()); got != 1 {
		t.Fatalf("frame after restart = %d, want 1", got)
	}

	// Inactive window: no advance either.
	state = gpui.ImagePlayback{FrameIndex: 1}
	state.Advance(image, 0, activePolicy())
	inactive := activePolicy()
	inactive.WindowActive = false
	if got := state.Advance(image, time.Hour, inactive); got != 1 {
		t.Fatalf("inactive frame = %d, want 1", got)
	}

	// NeedsAnimationFrame follows the pinned condition (img.rs lines
	// 411-416): identified, multi-frame, active window, motion allowed.
	if !gpui.NeedsAnimationFrame(image, true, activePolicy()) {
		t.Fatal("an identified animated active image needs no frame")
	}
	if gpui.NeedsAnimationFrame(image, false, activePolicy()) {
		t.Fatal("an id-less image requested a frame")
	}
	if gpui.NeedsAnimationFrame(animatedImage(40*time.Millisecond), true, activePolicy()) {
		t.Fatal("a single-frame image requested a frame")
	}
	if gpui.NeedsAnimationFrame(image, true, inactive) {
		t.Fatal("an inactive image requested a frame")
	}
	if gpui.NeedsAnimationFrame(image, true, reduced) {
		t.Fatal("a reduced-motion image requested a frame")
	}
}

// TestPlaybackClampsAndFallback pins the stale-index clamp (the pinned
// stale_frame_index test: a cached index from a longer image must not
// panic on a shorter one) and the 100ms out-of-range delay fallback
// (assets.rs RenderImage::delay).
func TestPlaybackClampsAndFallback(t *testing.T) {
	long := animatedImage(10*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond)
	state := gpui.ImagePlayback{}
	for i := 0; i < 5; i++ {
		state.Advance(long, time.Duration(i)*10*time.Millisecond, activePolicy())
	}
	if state.FrameIndex != 4 {
		t.Fatalf("seeded frame = %d, want 4", state.FrameIndex)
	}

	// A single-frame replacement clamps the stale index to 0.
	short := animatedImage(0)
	if got := state.Advance(short, time.Hour, activePolicy()); got != 0 {
		t.Fatalf("clamped frame = %d, want 0", got)
	}
	// A zero-frame image clamps to 0 too (the pin's
	// saturating_sub(1) = 0) and never panics.
	if got := (&gpui.ImagePlayback{}).Advance(&gpui.RenderImage{}, time.Hour, activePolicy()); got != 0 {
		t.Fatalf("zero-frame clamp = %d, want 0", got)
	}

	// The pinned out-of-range delay fallback: 100ms (not zero).
	if got := short.Delay(7); got != 100*time.Millisecond {
		t.Fatalf("out-of-range delay = %v, want 100ms", got)
	}
	if got := (&gpui.LoadedImage{}).Delay(3); got != 100*time.Millisecond {
		t.Fatalf("zero-image delay = %v, want 100ms", got)
	}
}

// TestLoadingDelayStateMachine pins the 200ms loading delay (img.rs
// LOADING_DELAY and the started_loading tracker): the replacement
// state appears only strictly after the delay, the timer notifies the
// requesting view, and a resolved load cancels the tracker.
func TestLoadingDelayStateMachine(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 100, Height: 100})

	state := gpui.ImagePlayback{}
	if state.ShowLoadingReplacement(0) {
		t.Fatal("a fresh playback shows the loading replacement")
	}

	// Start the delay notification: the window owns the timer task.
	state.StartLoadingDelay(window, 0, app)
	if state.ShowLoadingReplacement(ta.Now() + 200*time.Millisecond) {
		// elapsed == LOADING_DELAY exactly is NOT > LOADING_DELAY (the
		// pinned strictly-greater comparison).
		t.Fatal("elapsed == LOADING_DELAY already shows the replacement")
	}
	if !state.ShowLoadingReplacement(ta.Now() + 200*time.Millisecond + time.Nanosecond) {
		t.Fatal("elapsed just over LOADING_DELAY does not show the replacement")
	}
	if state.ShowLoadingReplacement(ta.Now() + 199*time.Millisecond) {
		t.Fatal("the replacement appeared before the delay")
	}

	// A second start does not restart the timer (the pinned Some
	// branch).
	startedAt, running := state.StartedLoadingAt()
	if !running {
		t.Fatal("the loading delay is not running")
	}
	state.StartLoadingDelay(window, 0, app)
	if again, _ := state.StartedLoadingAt(); again != startedAt {
		t.Fatal("the loading delay restarted")
	}

	// The timer fires at the virtual 200ms and nudges the window.
	ta.AdvanceClock(200)
	if !window.RefreshRequested() {
		t.Fatal("the loading-delay notification did not refresh the window")
	}

	// Resolution clears the tracker (the pinned started_loading = None)
	// and cancels the task.
	state.Resolved()
	if state.ShowLoadingReplacement(ta.Now() + time.Hour) {
		t.Fatal("a resolved playback still shows the loading replacement")
	}
	ta.RunUntilParked()
}

// TestLoadingDelayWindowCloseCancelsNotification pins the scope-owned
// loading delay (the ticket04 delivery task): closing the window
// before the timer fires suppresses the notification.
func TestLoadingDelayWindowCloseCancelsNotification(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 100, Height: 100})

	state := gpui.ImagePlayback{}
	state.StartLoadingDelay(window, 0, app)
	gpui.CloseTestWindow(window)

	ta.AdvanceClock(300)
	ta.RunUntilParked()
	// The closed window's refresh seam is gone; the notification never
	// marked it. No panic, no unresolved delivery.
	if window.RefreshRequested() {
		t.Fatal("a closed window was refreshed")
	}
}

// TestAppReduceMotionAccessors pin App::reduce_motion /
// set_reduce_motion (app.rs lines 1119-1128) — the policy source the
// image playback consults.
func TestAppReduceMotionAccessors(t *testing.T) {
	app := gpui.NewTestApp().App()
	if app.ReduceMotion() {
		t.Fatal("reduce motion defaults on")
	}
	app.SetReduceMotion(true)
	if !app.ReduceMotion() {
		t.Fatal("SetReduceMotion(true) did not stick")
	}
	app.SetReduceMotion(false)
	if app.ReduceMotion() {
		t.Fatal("SetReduceMotion(false) did not stick")
	}
}

// TestAppNowIsVirtualClock pins App.Now (the pinned Instant::now()
// source): the deterministic scheduler's clock.
func TestAppNowIsVirtualClock(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	if app.Now() != 0 {
		t.Fatalf("Now = %v, want 0", app.Now())
	}
	if app.Now() != ta.Now() {
		t.Fatalf("App.Now = %v, TestApp.Now = %v", app.Now(), ta.Now())
	}
	ta.AdvanceClock(250)
	if app.Now() != 250*time.Millisecond {
		t.Fatalf("Now after 250ms = %v", app.Now())
	}
}
