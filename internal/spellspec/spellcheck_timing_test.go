package spellspec

import (
	"fmt"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// TestSpellcheckCheckNeverBlocksTheUIThread is the ticket's second
// acceptance test: while a deliberately slow checker runs a check, the
// UI thread keeps drawing and dispatching.
//
// The "UI thread" of a test application is the dispatcher goroutine the
// test drives (RunUntilParked + DrawWindowFrame); the slow fake sleeps
// on the WORKER thread only. The beats below are one dispatch-and-draw
// cycle each. A check that blocked the UI thread (running on it, or
// making it wait) would leave exactly one gap the size of the whole
// check; the assertion bounds the worst gap at half the check duration
// and requires a healthy beat count, margins that hold under CPU load
// (each beat is scheduler drain plus a full frame draw, not a tight
// spin).
//
// The scenario runs three times in one test to catch scheduling
// flakiness.
func TestSpellcheckCheckNeverBlocksTheUIThread(t *testing.T) {
	const checkDelay = 500 * time.Millisecond
	for run := 1; run <= 3; run++ {
		t.Run(fmt.Sprintf("run%d", run), func(t *testing.T) {
			ta := gpui.NewTestApp()
			defer ta.App().Shutdown()
			cx := ta.AddEmptyWindow()
			w := cx.Window()

			fake := &fakeSpellChecker{
				delay: checkDelay,
				misspellBy: map[string][]gpui.Misspelling{
					"the slow checker text": {{Start: 4, End: 8}},
				},
			}
			svc := gpui.NewSpellcheckService(ta.App(), fake)
			defer svc.Close()

			var delivered bool
			var result gpui.SpellcheckResult
			issueStart := time.Now()
			svc.Check("timing", "the slow checker text", "en-US", func(r gpui.SpellcheckResult, _ *gpui.App) {
				result = r
				delivered = true
			})
			if issueTime := time.Since(issueStart); issueTime > 50*time.Millisecond {
				t.Fatalf("Check blocked its caller for %v; queueing must be immediate", issueTime)
			}

			var beats []time.Time
			deadline := time.Now().Add(4 * time.Second)
			for !delivered && time.Now().Before(deadline) {
				ta.RunUntilParked()
				if _, err := gpui.DrawWindowFrame(w); err != nil {
					t.Fatalf("DrawWindowFrame: %v", err)
				}
				svc.FlushDeliveries()
				beats = append(beats, time.Now())
			}
			if !delivered {
				t.Fatalf("the check never delivered within %v while the UI thread kept cycling", 4*time.Second)
			}
			if result.Err != nil {
				t.Fatalf("check error = %v", result.Err)
			}
			if len(result.Misspellings) != 1 || result.Misspellings[0] != (gpui.Misspelling{Start: 4, End: 8}) {
				t.Fatalf("misspellings = %v, want one range 4..8", result.Misspellings)
			}

			var maxGap time.Duration
			for i := 1; i < len(beats); i++ {
				if gap := beats[i].Sub(beats[i-1]); gap > maxGap {
					maxGap = gap
				}
			}
			if len(beats) < 20 {
				t.Fatalf("only %d UI beats while a %v check ran; the UI thread stalled", len(beats), checkDelay)
			}
			if maxGap > checkDelay/2 {
				t.Fatalf("max UI beat gap %v while the check ran %v — a gap that large means the UI thread was blocked", maxGap, checkDelay)
			}
			t.Logf("run %d: %d UI beats in %v of checking; max gap %v (bound %v)",
				run, len(beats), checkDelay, maxGap, checkDelay/2)
		})
	}
}
