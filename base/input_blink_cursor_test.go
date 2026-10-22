package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:input/base/blink_cursor.rs (5/5).

// blinkObserver records a cursor's notifications (the pinned cx.observe).
type blinkObserver struct{}

func newTestBlinkCursor(ta *gpui.TestApp) gpui.Entity[blinkCursor] {
	var cursor gpui.Entity[blinkCursor]
	ta.Update(func(app *gpui.App) {
		cursor = gpui.NewEntity(app, ta.RootScope(), func(b *blinkCursor, _ *gpui.Context[blinkCursor]) {
			*b = newBlinkCursor()
		})
	})
	return cursor
}

func blinkDo(ta *gpui.TestApp, cursor gpui.Entity[blinkCursor], f func(*blinkCursor, *gpui.Context[blinkCursor])) {
	ta.Update(func(app *gpui.App) { cursor.Update(app, f) })
	ta.RunUntilParked()
}

func blinkVisible(ta *gpui.TestApp, cursor gpui.Entity[blinkCursor]) bool {
	return cursor.Read(ta.App(), func(b *blinkCursor, _ *gpui.App) bool { return b.isVisible() })
}

// observeBlinks counts the cursor's notifications from now on.
func observeBlinks(ta *gpui.TestApp, cursor gpui.Entity[blinkCursor]) *int {
	count := new(int)
	ta.Update(func(app *gpui.App) {
		observer := gpui.NewEntity(app, ta.RootScope(), func(*blinkObserver, *gpui.Context[blinkObserver]) {})
		observer.Update(app, func(_ *blinkObserver, ocx *gpui.Context[blinkObserver]) {
			ocx.Observe(cursor, func(*blinkObserver, gpui.Entity[blinkCursor], *gpui.Context[blinkObserver]) { *count++ })
		})
	})
	ta.RunUntilParked()
	return count
}

func blinkMs(d interface{ Milliseconds() int64 }) int64 { return d.Milliseconds() }

// U:input/base/blink_cursor.rs:127-147
func TestBlinkCursorRepeatedPausesKeepCursorVisibleUntilIdle(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	cursor := newTestBlinkCursor(ta)
	if blinkVisible(ta, cursor) {
		t.Fatal("a new cursor is visible")
	}
	// Only a focused input blinks, and only a blinking cursor pauses.
	blinkDo(ta, cursor, func(b *blinkCursor, cx *gpui.Context[blinkCursor]) { b.start(cx) })
	for range 5 {
		blinkDo(ta, cursor, func(b *blinkCursor, cx *gpui.Context[blinkCursor]) { b.pause(cx) })
		ta.AdvanceClock(200)
		ta.RunUntilParked()
		if !blinkVisible(ta, cursor) {
			t.Fatal("cursor hidden while paused")
		}
	}
	ta.AdvanceClock(100)
	ta.RunUntilParked()
	if blinkVisible(ta, cursor) {
		t.Fatal("cursor still visible after the pause elapsed")
	}
	ta.AdvanceClock(blinkMs(blinkInterval))
	ta.RunUntilParked()
	if !blinkVisible(ta, cursor) {
		t.Fatal("cursor did not blink back on")
	}
}

// U:input/base/blink_cursor.rs:149-178
func TestBlinkCursorPausingACursorThatIsNotBlinkingDoesNotStartIt(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	// Never focused: the epoch is still zero.
	cursor := newTestBlinkCursor(ta)

	// What a programmatic set_value on an unfocused input does.
	blinkDo(ta, cursor, func(b *blinkCursor, cx *gpui.Context[blinkCursor]) { b.pause(cx) })
	ta.AdvanceClock(blinkMs(blinkPauseDelay))
	ta.RunUntilParked()
	if blinkVisible(ta, cursor) {
		t.Fatal("an unstarted cursor became visible")
	}

	notifies := observeBlinks(ta, cursor)
	ta.AdvanceClock(blinkMs(blinkInterval * 6))
	ta.RunUntilParked()
	if *notifies != 0 {
		t.Fatalf("a cursor that was never started blinks: %d notifications", *notifies)
	}
}

// U:input/base/blink_cursor.rs:180-203
func TestBlinkCursorBlurringAPausedCursorLeavesTheNextFocusBlinking(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	cursor := newTestBlinkCursor(ta)
	blinkDo(ta, cursor, func(b *blinkCursor, cx *gpui.Context[blinkCursor]) { b.start(cx) })

	// Typing pauses, then the input blurs before the pause elapses.
	blinkDo(ta, cursor, func(b *blinkCursor, cx *gpui.Context[blinkCursor]) { b.pause(cx) })
	blinkDo(ta, cursor, func(b *blinkCursor, cx *gpui.Context[blinkCursor]) { b.stop(cx) })
	if blinkVisible(ta, cursor) {
		t.Fatal("a stopped cursor is visible")
	}

	// Focusing again shows the cursor and blinks it.
	blinkDo(ta, cursor, func(b *blinkCursor, cx *gpui.Context[blinkCursor]) { b.start(cx) })
	if !blinkVisible(ta, cursor) {
		t.Fatal("a restarted cursor is hidden")
	}
	ta.AdvanceClock(blinkMs(blinkInterval))
	ta.RunUntilParked()
	if blinkVisible(ta, cursor) {
		t.Fatal("a restarted cursor did not blink")
	}
}

// U:input/base/blink_cursor.rs:205-229
func TestBlinkCursorStoppingAPausedCursorEndsTheBlinkLoop(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	cursor := newTestBlinkCursor(ta)
	blinkDo(ta, cursor, func(b *blinkCursor, cx *gpui.Context[blinkCursor]) { b.start(cx) })
	blinkDo(ta, cursor, func(b *blinkCursor, cx *gpui.Context[blinkCursor]) { b.pause(cx) })
	blinkDo(ta, cursor, func(b *blinkCursor, cx *gpui.Context[blinkCursor]) { b.stop(cx) })

	notifications := observeBlinks(ta, cursor)
	ta.AdvanceClock(3000)
	ta.RunUntilParked()

	if blinkVisible(ta, cursor) {
		t.Fatal("a stopped cursor is visible")
	}
	if *notifications != 0 {
		t.Fatalf("a stopped cursor kept blinking: %d notifications", *notifications)
	}

	blinkDo(ta, cursor, func(b *blinkCursor, cx *gpui.Context[blinkCursor]) { b.start(cx) })
	if !blinkVisible(ta, cursor) {
		t.Fatal("a restarted cursor is hidden")
	}
}

// U:input/base/blink_cursor.rs:231-249
func TestBlinkCursorStoppingABlinkingCursorEndsTheBlinkLoop(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	cursor := newTestBlinkCursor(ta)
	blinkDo(ta, cursor, func(b *blinkCursor, cx *gpui.Context[blinkCursor]) { b.start(cx) })
	blinkDo(ta, cursor, func(b *blinkCursor, cx *gpui.Context[blinkCursor]) { b.stop(cx) })

	notifications := observeBlinks(ta, cursor)
	ta.AdvanceClock(3000)
	ta.RunUntilParked()

	if blinkVisible(ta, cursor) {
		t.Fatal("a stopped cursor is visible")
	}
	if *notifications != 0 {
		t.Fatalf("a stopped cursor kept blinking: %d notifications", *notifications)
	}
}
