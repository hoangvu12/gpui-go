package base

// Port of U:input/base/blink_cursor.rs: the 500ms caret blink that pauses
// while the user types.
//
// Go adaptations, as in auto_scroll.go: cx.spawn (an entity-bound
// foreground task) is App.Spawn over a weak handle of the cursor entity,
// and BackgroundExecutor::timer is a background task sleeping on the
// scheduler clock, so TestApp's virtual clock drives it. Replacing the
// pinned `_task` drops (cancels) the previous task; the port cancels it
// explicitly.

import (
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

const (
	// blinkInterval is INTERVAL (U:input/base/blink_cursor.rs:4) — and
	// the rate a hostless test application reports, because the default
	// system-preference record carries 500 ms (system_prefs.go). The
	// live rate comes from the record instead (blinkIntervalFor).
	blinkInterval = 500 * time.Millisecond
	// blinkPauseDelay is PAUSE_DELAY (U:input/base/blink_cursor.rs:5).
	blinkPauseDelay = 300 * time.Millisecond
	// cursorWidth is CURSOR_WIDTH off macOS: an integer width avoids a
	// blurry caret on Windows and Linux (U:input/base/blink_cursor.rs:7-11).
	cursorWidth float32 = 2
)

// systemCaretBlinkInterval reads the caret blink rate the platform
// reports: the system-preference record's CaretBlinkInterval — the
// GetCaretBlinkTime mapping, where 0 is the 500 ms default, INFINITE a
// steady caret and anything else the period in milliseconds
// (ui/native_theme/os_settings_provider_win.cc:147-160; the default at
// os_settings_provider.h:57). The var is the test seam: the rate is
// injected without a host (a hostless app always answers the default).
var systemCaretBlinkInterval = func(app *gpui.App) time.Duration {
	if app == nil {
		return blinkInterval
	}
	return app.SystemPreferences().CaretBlinkInterval
}

// blinkCursor manages the input caret blinking
// (U:input/base/blink_cursor.rs:13-25): every blink notifies the entity,
// and the input observes it to repaint.
type blinkCursor struct {
	visible bool
	paused  bool
	epoch   int

	task    gpui.Task[struct{}]
	hasTask bool
}

// newBlinkCursor is BlinkCursor::new (U:input/base/blink_cursor.rs:28-35).
func newBlinkCursor() blinkCursor { return blinkCursor{} }

// start starts the blinking (U:input/base/blink_cursor.rs:37-40).
func (b *blinkCursor) start(cx *gpui.Context[blinkCursor]) {
	b.blink(b.epoch, cx)
}

// stop stops the blinking and clears the blink state, so the next start
// begins from a visible cursor (U:input/base/blink_cursor.rs:42-50).
func (b *blinkCursor) stop(cx *gpui.Context[blinkCursor]) {
	b.epoch = 0
	b.paused = false
	b.visible = false
	b.dropTask()
	cx.Notify()
}

// nextEpoch is U:input/base/blink_cursor.rs:52-55.
func (b *blinkCursor) nextEpoch() int {
	b.epoch++
	return b.epoch
}

// blink toggles the caret and schedules the next blink
// (U:input/base/blink_cursor.rs:57-79). The blink interval is the
// system's caret blink rate (browser-core 28): Chromium reads the rate
// when it (re)starts the blink timer (frame_caret.cc StartBlinkCaret:
// 176-193) and Windows exposes no change notification for it
// (os_settings_provider_win.cc:148-153, "Given how rarely users change
// this"), so the port re-reads it on every tick — a rate change takes
// effect on the next blink, and a caret stopped by 'never' restarts on
// the next focus or caret move, like Chromium's timer restart.
func (b *blinkCursor) blink(epoch int, cx *gpui.Context[blinkCursor]) {
	// A task from an earlier blink lifecycle must not mutate the current
	// state.
	if epoch != b.epoch {
		return
	}
	if b.paused {
		b.visible = true
		return
	}

	if systemCaretBlinkInterval(cx.App()) <= 0 {
		// The system asks for a steady caret ("never blink"): the caret
		// stays visible and no timer is armed (frame_caret.cc:190-192 — a
		// zero blink interval starts no repeating timer).
		if !b.visible {
			b.visible = true
			cx.Notify()
		}
		return
	}

	b.visible = !b.visible
	cx.Notify()

	// Schedule the next blink at the system's rate.
	epoch = b.nextEpoch()
	b.replaceTask(cx, systemCaretBlinkInterval(cx.App()), func(c *blinkCursor, ccx *gpui.Context[blinkCursor]) {
		c.blink(epoch, ccx)
	})
}

// isVisible keeps the cursor showing while paused
// (U:input/base/blink_cursor.rs:81-84).
func (b *blinkCursor) isVisible() bool { return b.paused || b.visible }

// pause shows the cursor immediately and restarts the idle delay before
// blinking resumes. It is a no-op on a cursor that is not blinking: a
// zero epoch means the input is not focused
// (U:input/base/blink_cursor.rs:86-118).
func (b *blinkCursor) pause(cx *gpui.Context[blinkCursor]) {
	if b.epoch == 0 {
		return
	}

	b.paused = true
	b.visible = true
	cx.Notify()

	// Every pause replaces the pending timer, keeping repeated input
	// visible.
	epoch := b.nextEpoch()
	b.replaceTask(cx, blinkPauseDelay, func(c *blinkCursor, ccx *gpui.Context[blinkCursor]) {
		if epoch != c.epoch {
			return
		}
		c.paused = false
		c.blink(epoch, ccx)
	})
}

// replaceTask is `self._task = cx.spawn(...)`: cancel the previous task,
// then wait delay on the scheduler clock and run then on the entity while
// it is alive.
func (b *blinkCursor) replaceTask(cx *gpui.Context[blinkCursor], delay time.Duration, then func(*blinkCursor, *gpui.Context[blinkCursor])) {
	b.dropTask()
	this := cx.Entity().Downgrade()
	b.task = cx.App().Spawn(func(acx *gpui.AsyncApp) struct{} {
		slept := acx.Await(acx.App().Background().Spawn(func(run *gpui.TaskRun) bool {
			run.Sleep(delay)
			return true
		}))
		// A cancelled sleep (the task dropped, or the application shutting
		// down) ends the body, as a dropped Rust task stops at its await.
		if !slept {
			return struct{}{}
		}
		acx.Update(func(app *gpui.App) {
			_ = this.TryUpdate(app, then)
		})
		return struct{}{}
	})
	b.hasTask = true
}

// dropTask is `self._task = Task::ready(())`.
func (b *blinkCursor) dropTask() {
	if b.hasTask {
		b.task.Cancel()
		b.task = gpui.Task[struct{}]{}
		b.hasTask = false
	}
}
