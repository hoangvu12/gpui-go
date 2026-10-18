package gpui

import (
	"testing"
	"time"
)

// TestClockTestAppStaysVirtual: under TestApp the scheduler keeps the
// deterministic virtual clock (reference advance_clock is test-only and
// "does not run any tasks, but does make timers ready",
// executor.rs:210-214): real time passing never moves App.Now or fires a
// timer; only AdvanceClock does, to the exact millisecond.
func TestClockTestAppStaysVirtual(t *testing.T) {
	ta := NewTestApp()
	if ta.sched.realtime() {
		t.Fatal("TestApp scheduler must not run on the wall clock")
	}
	woke := int64(-1)
	task := ta.Background().Spawn(func(run *TaskRun) int {
		run.Sleep(50 * time.Millisecond)
		woke = ta.NowMs()
		return 1
	})
	ta.RunUntilParked()
	time.Sleep(70 * time.Millisecond) // real time must not leak in
	ta.RunUntilParked()
	if task.Done() || ta.NowMs() != 0 || ta.App().Now() != 0 {
		t.Fatalf("virtual clock moved with real time: done=%v now=%d", task.Done(), ta.NowMs())
	}
	ta.AdvanceClock(49)
	if task.Done() {
		t.Fatal("50ms sleep woke at 49ms")
	}
	ta.AdvanceClock(1)
	if !task.Done() || woke != 50 {
		t.Fatalf("50ms sleep: done=%v woke=%d, want done at 50", task.Done(), woke)
	}
	if got := ta.App().Now(); got != 50*time.Millisecond {
		t.Fatalf("App.Now = %v, want 50ms", got)
	}
}

// TestClockTestAppDeterministicReplay: two identical TestApp runs with
// interleaved timers produce the identical wake order and clock
// readings (bit-deterministic virtual clock).
func TestClockTestAppDeterministicReplay(t *testing.T) {
	runOnce := func() []int64 {
		ta := NewTestApp()
		var log []int64
		for _, d := range []int{30, 10, 20, 10} {
			d := d
			ta.Background().Spawn(func(run *TaskRun) int {
				run.Sleep(time.Duration(d) * time.Millisecond)
				log = append(log, int64(d)*1000+ta.NowMs())
				return 0
			})
		}
		ta.RunUntilParked()
		time.Sleep(5 * time.Millisecond)
		ta.AdvanceClock(100)
		return log
	}
	a, b := runOnce(), runOnce()
	want := []int64{10010, 10010, 20020, 30030}
	if len(a) != len(want) {
		t.Fatalf("wake log = %v, want %v", a, want)
	}
	for i := range want {
		if a[i] != want[i] || b[i] != want[i] {
			t.Fatalf("wake logs %v / %v, want %v", a, b, want)
		}
	}
}

// TestClockWallDeadlineRoundsUp: a wall-clock deadline never precedes
// the requested duration (the reference timer completes after the given
// duration, executor.rs:193-202), including sub-millisecond sleeps.
func TestClockWallDeadlineRoundsUp(t *testing.T) {
	w := &wallClock{origin: time.Now().Add(-1500 * time.Microsecond), baseMs: 7}
	for _, d := range []time.Duration{time.Microsecond, 999 * time.Microsecond, time.Millisecond, 50 * time.Millisecond} {
		before := w.elapsed()
		deadline := w.deadlineAfter(d)
		if got := time.Duration(deadline-w.baseMs) * time.Millisecond; got < before+d {
			t.Fatalf("deadlineAfter(%v) = %v since origin, want >= %v", d, got, before+d)
		}
	}
	if w.nowMs() < 8 {
		t.Fatalf("nowMs = %d, want >= baseMs + 1", w.nowMs())
	}
}
