//go:build windows

package spellspec

import (
	"errors"
	"runtime"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

var (
	probeKernel32   = syscall.NewLazyDLL("kernel32.dll")
	probeCurrentTID = probeKernel32.NewProc("GetCurrentThreadId")
)

// currentThreadID returns the calling OS thread's id.
func currentThreadID() uint32 {
	r, _, _ := probeCurrentTID.Call()
	return uint32(r)
}

// TestSpellcheckServiceDeliversThroughHostMessageLoop runs the
// production delivery path: a real app on a real Win32 host (the
// message loop draining in App.Run), a check queued from the host
// thread (an App.Update marshals there), and the completion posted
// back through Host.Post — the Go port of Chromium's
// PostTaskAndReplyWithResult reply to the caller's sequence
// (windows_spell_checker.cc:512-524). No FlushDeliveries exists on
// this path: nothing would deliver if the completion did not ride
// the message loop.
//
// Environment expectation: like internal/winhostspec, this creates a
// real (off-screen-under-go-test) host; it FAILS on host errors, never
// skips. No real input is ever injected.
func TestSpellcheckServiceDeliversThroughHostMessageLoop(t *testing.T) {
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
	})

	fake := &fakeSpellChecker{
		delay:       100 * time.Millisecond,
		misspellBy:  map[string][]gpui.Misspelling{"real host text": {{Start: 5, End: 9}}},
		threadProbe: currentThreadID,
	}
	svc := gpui.NewSpellcheckService(app, fake)
	t.Cleanup(svc.Close)

	delivered := make(chan gpui.SpellcheckResult, 1)
	// App.Update marshals onto the host thread: the check is queued
	// from the foreground, like an input's prepaint during a frame.
	app.Update(func(*gpui.App) {
		svc.Check("host", "real host text", "en-US", func(r gpui.SpellcheckResult, _ *gpui.App) {
			delivered <- r
		})
	})

	select {
	case result := <-delivered:
		if result.Err != nil {
			t.Fatalf("check error = %v", result.Err)
		}
		wantMisspellings(t, result.Misspellings, []gpui.Misspelling{{Start: 5, End: 9}})
	case <-time.After(10 * time.Second):
		t.Fatal("the completion never arrived through the host message loop")
	}

	// The checker ran on its own pinned OS thread, not the host's.
	threads := fake.threadsSnapshot()
	if len(threads) == 0 {
		t.Fatal("the checker never ran")
	}
	if hostTID := host.ThreadID(); threads[0] == hostTID {
		t.Fatalf("the checker ran on the host thread %d; it must own its pinned thread", hostTID)
	}
	for i, tid := range threads {
		if tid != threads[0] {
			t.Fatalf("checker call %d ran on OS thread %d, want the pinned thread %d", i, tid, threads[0])
		}
	}

	if err := host.Stop(); err != nil {
		t.Fatalf("host stop: %v", err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("app.Run terminal error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the run loop never exited")
	}
}

// TestSpellcheckWorkerRunsPinnedToItsOwnThread pins the threading
// design (the ticket's design contract): the COM checker runs on the
// service's own background goroutine, pinned to one OS thread with
// runtime.LockOSThread — the Go equivalent of Chromium's dedicated COM
// STA sequence (BackgroundHelper's same-thread rule,
// windows_spell_checker.cc:40-48; the STA runner itself,
// windows_spell_checker_unittest.cc:51-52). The checker's OS thread
// must differ from the UI thread's and stay the same across calls:
// COM objects are thread-affine.
func TestSpellcheckWorkerRunsPinnedToItsOwnThread(t *testing.T) {
	// Pin this goroutine's OS thread for the test's duration: the
	// production UI thread is a LockOSThread-pinned, message-pumping
	// thread (win32host_windows.go:1604), and a locked thread can never
	// run the worker. Without the pin the test's premise is unsound in
	// Go's scheduler: a freshly spawned goroutine lands in the current
	// P's runnext slot, so when this goroutine blocks (flushUntil's
	// waits) the worker runs on this very OS thread, locks it, and the
	// unpinned test goroutine migrates elsewhere — the "UI thread's
	// OS thread" it sampled before spawning then IS the worker's
	// thread, a scheduling artifact that flips with the binary's code
	// layout (observed both before and after the ticket-16 merge), not
	// a threading regression. Pinned, the sampling is deterministic and
	// the assertion means what it says.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	ta := gpui.NewTestApp()
	defer ta.App().Shutdown()
	fake := &fakeSpellChecker{
		misspellBy:  map[string][]gpui.Misspelling{"a": {{Start: 0, End: 1}}},
		threadProbe: currentThreadID,
	}
	svc := gpui.NewSpellcheckService(ta.App(), fake)
	defer svc.Close()

	uiThread := currentThreadID()
	var delivered int32
	check := func(key, text string) {
		svc.Check(key, text, "en-US", func(gpui.SpellcheckResult, *gpui.App) {
			atomic.AddInt32(&delivered, 1)
		})
	}
	check("pin1", "a")
	check("pin2", "b")
	if !flushUntil(t, ta, svc, func() bool { return atomic.LoadInt32(&delivered) == 2 }, 2*time.Second) {
		t.Fatal("the checks never delivered")
	}

	threads := fake.threadsSnapshot()
	// Prepare plus two checks, all on the worker.
	if len(threads) < 3 {
		t.Fatalf("worker recorded %d thread ids, want at least 3 (prepare + two checks)", len(threads))
	}
	for i, tid := range threads {
		if tid == uiThread {
			t.Fatalf("worker call %d ran on the UI thread's OS thread %d", i, tid)
		}
		if tid != threads[0] {
			t.Fatalf("worker call %d ran on OS thread %d, want the pinned thread %d", i, tid, threads[0])
		}
	}
	workerTID := threads[0]
	if svcClosed := fake.wasReleased(); svcClosed {
		t.Fatal("the checker was released while the service is live")
	}
	_ = workerTID
}

// TestWindowsSpellCheckerRealEnUS runs the service against the REAL
// Windows spell checker with en-US (the platform dictionary): known
// misspellings are reported for a sample string, and suggestions come
// back for a misspelled word. Skipped with a clear reason when this
// machine has no en-US dictionary (the ISpellCheckerFactory::IsSupported
// false path) or the COM factory itself is unavailable —
// windows_spell_checker_unittest.cc's en-US test row and vocabulary
// (the "absense" case, :150) are the reference.
func TestWindowsSpellCheckerRealEnUS(t *testing.T) {
	ta := gpui.NewTestApp()
	defer ta.App().Shutdown()
	// The lazy default service builds the real Windows checker.
	svc := ta.App().Spellcheck()
	defer svc.Close()

	const sample = "Helllo there. This sentense has mispellings."
	var checkDone bool
	var result gpui.SpellcheckResult
	svc.Check("real", sample, "en-US", func(r gpui.SpellcheckResult, _ *gpui.App) {
		result = r
		checkDone = true
	})
	if !flushUntil(t, ta, svc, func() bool { return checkDone }, 5*time.Second) {
		t.Fatal("the real check never delivered within 5s")
	}
	if result.Err != nil {
		if errors.Is(result.Err, gpui.ErrSpellcheckLanguageUnsupported) {
			t.Skipf("skipped: %v (no en-US Windows spell checker on this machine)", result.Err)
		}
		t.Fatalf("real check error = %v", result.Err)
	}
	if result.Text != sample {
		t.Errorf("result text = %q, want the checked text", result.Text)
	}
	if len(result.Misspellings) == 0 {
		t.Fatalf("no misspellings reported for %q — the real checker must flag known misspellings", sample)
	}
	// "Helllo" is the first misspelling: UTF-16 offsets 0..6.
	first := result.Misspellings[0]
	if first.Start != 0 || first.End != 6 {
		t.Errorf("first misspelling = %v, want 0..6 (\"Helllo\")", first)
	}
	for _, m := range result.Misspellings {
		if m.Start < 0 || m.End > len([]rune(sample)) || m.Start >= m.End {
			t.Errorf("misspelling %v is not a range inside the text", m)
		}
	}

	// Suggestions for a misspelled word (Chromium's test word).
	var suggestDone bool
	var suggestions []string
	var suggestErr error
	svc.Suggest("real", "absense", "en-US", func(words []string, err error, _ *gpui.App) {
		suggestions, suggestErr, suggestDone = words, err, true
	})
	if !flushUntil(t, ta, svc, func() bool { return suggestDone }, 5*time.Second) {
		t.Fatal("the real suggestions never delivered within 5s")
	}
	if suggestErr != nil {
		t.Fatalf("real suggest error = %v", suggestErr)
	}
	found := false
	for _, s := range suggestions {
		if s == "absence" {
			found = true
		}
	}
	if !found {
		t.Errorf("suggestions for \"absense\" = %v, want \"absence\" among them", suggestions)
	}
	t.Logf("real en-US checker: %d misspellings in %q; suggestions for \"absense\": %v",
		len(result.Misspellings), sample, suggestions)
}
