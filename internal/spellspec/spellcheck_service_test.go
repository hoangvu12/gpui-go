// Package spellspec holds browser-core ticket 30's spellcheck tests:
// the service against an injected fake checker (misspellings reported
// for a sample string, supersession, prepare failure, the
// suggest/add/ignore surface, and delivery gating), the timing test
// proving a check never blocks the UI thread, the worker-thread pinning
// proof, and one test against the real Windows spell checker with
// en-US (skipped with a reason when that dictionary is not installed).
package spellspec

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// fakeSpellChecker is the injectable SpellChecker: canned results by
// exact text/word, an optional prepare failure, an optional per-call
// delay (the timing test's slow checker), a gate that can block
// individual checks (the supersession test's in-flight control), and a
// probe recording the calling OS thread (the pinning test).
type fakeSpellChecker struct {
	prepareErr  error
	checkErr    error
	suggestErr  error
	delay       time.Duration
	checkGate   func(text string)
	misspellBy  map[string][]gpui.Misspelling
	suggestBy   map[string][]string
	threadProbe func() uint32

	mu       sync.Mutex
	checks   []string
	suggests []string
	adds     []string
	ignores  []string
	threads  []uint32
	released bool
}

func (f *fakeSpellChecker) Prepare() error {
	f.recordThread()
	return f.prepareErr
}

func (f *fakeSpellChecker) Check(text, lang string) ([]gpui.Misspelling, error) {
	f.recordThread()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.checkGate != nil {
		f.checkGate(text)
	}
	f.mu.Lock()
	f.checks = append(f.checks, text)
	f.mu.Unlock()
	if f.checkErr != nil {
		return nil, f.checkErr
	}
	return f.misspellBy[text], nil
}

func (f *fakeSpellChecker) Suggest(word, lang string) ([]string, error) {
	f.recordThread()
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	f.mu.Lock()
	f.suggests = append(f.suggests, word)
	f.mu.Unlock()
	return f.suggestBy[word], f.suggestErr
}

func (f *fakeSpellChecker) Add(word, lang string) error {
	f.recordThread()
	f.mu.Lock()
	f.adds = append(f.adds, word)
	f.mu.Unlock()
	return nil
}

func (f *fakeSpellChecker) Ignore(word, lang string) error {
	f.recordThread()
	f.mu.Lock()
	f.ignores = append(f.ignores, word)
	f.mu.Unlock()
	return nil
}

func (f *fakeSpellChecker) Release() {
	f.mu.Lock()
	f.released = true
	f.mu.Unlock()
}

func (f *fakeSpellChecker) recordThread() {
	if f.threadProbe == nil {
		return
	}
	f.mu.Lock()
	f.threads = append(f.threads, f.threadProbe())
	f.mu.Unlock()
}

// checksSnapshot copies the recorded check texts.
func (f *fakeSpellChecker) checksSnapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.checks...)
}

func (f *fakeSpellChecker) threadsSnapshot() []uint32 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uint32(nil), f.threads...)
}

func (f *fakeSpellChecker) wasReleased() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.released
}

// flushUntil drives the hostless delivery queue and the test app's
// dispatcher until ok reports true or the timeout elapses, returning
// whether ok ever held. This is exactly what a test application's UI
// loop looks like from the service's perspective: it keeps dispatching
// and flushing while the worker checks in the background.
func flushUntil(t *testing.T, ta *gpui.TestApp, svc *gpui.SpellcheckService, ok func() bool, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if ok() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		ta.RunUntilParked()
		svc.FlushDeliveries()
		time.Sleep(2 * time.Millisecond)
	}
}

// wantMisspellings compares misspelling slices.
func wantMisspellings(t *testing.T, got, want []gpui.Misspelling) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("misspellings = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("misspellings[%d] = %v, want %v", i, got[i], want[i])
		}
	}
}

// TestSpellcheckServiceReportsKnownMisspellingsForSampleString is the
// ticket's first acceptance test: known misspellings are reported for a
// sample string. The fake checker maps Chromium's test vocabulary
// (windows_spell_checker_unittest.cc:150-159) onto misspelled ranges,
// and the service delivers them on the foreground with the checked
// text.
func TestSpellcheckServiceReportsKnownMisspellingsForSampleString(t *testing.T) {
	ta := gpui.NewTestApp()
	defer ta.App().Shutdown()
	fake := &fakeSpellChecker{
		misspellBy: map[string][]gpui.Misspelling{
			"The quikc brown fox jumpped over the fence": {
				{Start: 4, End: 9},   // "quikc"
				{Start: 20, End: 27}, // "jumpped"
			},
		},
	}
	svc := gpui.NewSpellcheckService(ta.App(), fake)
	defer svc.Close()

	var result gpui.SpellcheckResult
	delivered := false
	svc.Check("input", "The quikc brown fox jumpped over the fence", "en-US",
		func(r gpui.SpellcheckResult, _ *gpui.App) {
			result = r
			delivered = true
		})

	if !flushUntil(t, ta, svc, func() bool { return delivered }, 2*time.Second) {
		t.Fatal("the check never delivered within 2s")
	}
	if result.Err != nil {
		t.Fatalf("check error = %v", result.Err)
	}
	if result.Text != "The quikc brown fox jumpped over the fence" {
		t.Errorf("result text = %q, want the checked text", result.Text)
	}
	wantMisspellings(t, result.Misspellings, []gpui.Misspelling{{Start: 4, End: 9}, {Start: 20, End: 27}})

	// A clean text reports no misspellings.
	delivered = false
	svc.Check("input", "The quick brown fox jumped over the fence", "en-US",
		func(r gpui.SpellcheckResult, _ *gpui.App) {
			result = r
			delivered = true
		})
	if !flushUntil(t, ta, svc, func() bool { return delivered }, 2*time.Second) {
		t.Fatal("the clean check never delivered within 2s")
	}
	if result.Err != nil || len(result.Misspellings) != 0 {
		t.Fatalf("clean check = (%v misspellings, err %v), want none and no error", result.Misspellings, result.Err)
	}
}

// TestSpellcheckServiceNewerRequestSupersedesOlderForSameInput pins the
// supersession rule: with the first check held in flight (the gate),
// a newer request for the same key marks it superseded — the older
// result is dropped at delivery and never reaches the app, while the
// newer one is delivered.
func TestSpellcheckServiceNewerRequestSupersedesOlderForSameInput(t *testing.T) {
	ta := gpui.NewTestApp()
	defer ta.App().Shutdown()
	block := make(chan struct{})
	started := make(chan string, 1)
	fake := &fakeSpellChecker{
		misspellBy: map[string][]gpui.Misspelling{
			"first":  {{Start: 0, End: 5}},
			"second": {{Start: 1, End: 4}},
		},
		checkGate: func(text string) {
			if text == "first" {
				started <- text
				<-block
			}
		},
	}
	svc := gpui.NewSpellcheckService(ta.App(), fake)
	defer svc.Close()

	var deliveredTexts []string
	check := func(text string) {
		svc.Check("input", text, "en-US", func(r gpui.SpellcheckResult, _ *gpui.App) {
			deliveredTexts = append(deliveredTexts, r.Text)
		})
	}

	// Hold the first check in flight on the worker.
	check("first")
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("the first check never started on the worker")
	}

	// A newer request for the same input supersedes it.
	check("second")

	// Release the in-flight first check: its result must be dropped.
	close(block)
	if !flushUntil(t, ta, svc, func() bool { return len(deliveredTexts) > 0 }, 2*time.Second) {
		t.Fatal("the superseding check never delivered")
	}
	// Let any late straggler arrive before asserting (the dropped
	// completion would show up as a second delivery if supersession
	// failed).
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		ta.RunUntilParked()
		svc.FlushDeliveries()
		time.Sleep(2 * time.Millisecond)
	}
	if len(deliveredTexts) != 1 || deliveredTexts[0] != "second" {
		t.Fatalf("delivered results = %v, want only the newer request's (\"second\")", deliveredTexts)
	}
	if checks := fake.checksSnapshot(); len(checks) != 2 || checks[0] != "first" || checks[1] != "second" {
		t.Errorf("worker checks = %v, want both to have run", checks)
	}
}

// TestSpellcheckServicePrepareFailureFailsEveryCheck: a checker whose
// native state never initialized fails every delivered result with the
// prepare error and runs no checks (Chromium's null-factory path,
// windows_spell_checker.cc:487-491).
func TestSpellcheckServicePrepareFailureFailsEveryCheck(t *testing.T) {
	ta := gpui.NewTestApp()
	defer ta.App().Shutdown()
	prepareErr := errors.New("no COM apartment")
	fake := &fakeSpellChecker{prepareErr: prepareErr}
	svc := gpui.NewSpellcheckService(ta.App(), fake)
	defer svc.Close()

	var result gpui.SpellcheckResult
	delivered := false
	svc.Check("input", "quikc", "en-US", func(r gpui.SpellcheckResult, _ *gpui.App) {
		result = r
		delivered = true
	})
	if !flushUntil(t, ta, svc, func() bool { return delivered }, 2*time.Second) {
		t.Fatal("the failed prepare never delivered")
	}
	if !errors.Is(result.Err, prepareErr) {
		t.Fatalf("result error = %v, want the prepare error", result.Err)
	}
	if checks := fake.checksSnapshot(); len(checks) != 0 {
		t.Errorf("prepare failure still ran %d checks", len(checks))
	}
}

// TestSpellcheckServiceSuggestAddAndIgnore covers the rest of the core
// service surface (the research doc's Core gap: Check, Suggest,
// Add/Ignore): suggestions deliver on the foreground, and the
// fire-and-forget dictionary operations run on the worker.
func TestSpellcheckServiceSuggestAddAndIgnore(t *testing.T) {
	ta := gpui.NewTestApp()
	defer ta.App().Shutdown()
	fake := &fakeSpellChecker{
		suggestBy: map[string][]string{"absense": {"absence", "absencee"}},
	}
	svc := gpui.NewSpellcheckService(ta.App(), fake)
	defer svc.Close()

	var suggestions []string
	var suggestErr error
	delivered := false
	svc.Suggest("menu", "absense", "en-US", func(words []string, err error, _ *gpui.App) {
		suggestions, suggestErr, delivered = words, err, true
	})
	if !flushUntil(t, ta, svc, func() bool { return delivered }, 2*time.Second) {
		t.Fatal("the suggestions never delivered")
	}
	if suggestErr != nil {
		t.Fatalf("suggest error = %v", suggestErr)
	}
	if fmt.Sprint(suggestions) != fmt.Sprint([]string{"absence", "absencee"}) {
		t.Fatalf("suggestions = %v, want [absence absencee]", suggestions)
	}

	// Fire-and-forget dictionary operations run on the worker.
	svc.Add("absense", "en-US")
	svc.Ignore("teh", "en-US")
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(fake.adds) == 1 && len(fake.ignores) == 1 {
			break
		}
		svc.FlushDeliveries()
		time.Sleep(2 * time.Millisecond)
	}
	if len(fake.adds) != 1 || fake.adds[0] != "absense" {
		t.Fatalf("adds = %v, want [absense]", fake.adds)
	}
	if len(fake.ignores) != 1 || fake.ignores[0] != "teh" {
		t.Fatalf("ignores = %v, want [teh]", fake.ignores)
	}
}

// TestSpellcheckServiceCloseRetiresWorkerAndChecker: Close is
// idempotent, releases the checker on the worker, and drops queued
// operations.
func TestSpellcheckServiceCloseRetiresWorkerAndChecker(t *testing.T) {
	ta := gpui.NewTestApp()
	defer ta.App().Shutdown()
	fake := &fakeSpellChecker{}
	svc := gpui.NewSpellcheckService(ta.App(), fake)

	svc.Add("word", "en-US")
	svc.Close()
	svc.Close()
	if !fake.wasReleased() {
		t.Fatal("Close did not release the checker")
	}
	if checks := fake.checksSnapshot(); len(checks) != 0 {
		t.Fatalf("checks after close = %v, want none", checks)
	}
	// A closed service drops new work without panicking.
	svc.Check("input", "text", "en-US", func(gpui.SpellcheckResult, *gpui.App) {
		t.Error("a closed service delivered a result")
	})
	if n := svc.FlushDeliveries(); n != 0 {
		t.Fatalf("flushed %d deliveries after close, want 0", n)
	}
}

// TestSpellcheckAppSpellcheckLazyDefaultAndSetSpellcheck pins the App
// surface: Spellcheck creates the service once, SetSpellcheck replaces
// (and retires) it, and the shutdown integration retires the installed
// service.
func TestSpellcheckAppSpellcheckLazyDefaultAndSetSpellcheck(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	fake := &fakeSpellChecker{}
	injected := gpui.NewSpellcheckService(app, fake)
	app.SetSpellcheck(injected)
	if got := app.Spellcheck(); got != injected {
		t.Fatalf("Spellcheck() = %p, want the injected service", got)
	}

	// Replacing retires the previous service.
	fake2 := &fakeSpellChecker{}
	replacement := gpui.NewSpellcheckService(app, fake2)
	app.SetSpellcheck(replacement)
	if !fake.wasReleased() {
		t.Fatal("SetSpellcheck did not retire the previous service's checker")
	}

	// App.Shutdown retires the installed service (the task-ownership
	// hook).
	ta.App().Shutdown()
	if !fake2.wasReleased() {
		t.Fatal("App.Shutdown did not retire the spellcheck service")
	}
	// A fresh app without a host still lazily creates a service whose
	// operations fail with the platform error on this build shape.
	ta2 := gpui.NewTestApp()
	defer ta2.App().Shutdown()
	created := ta2.App().Spellcheck()
	if created == nil {
		t.Fatal("Spellcheck() returned nil")
	}
	if again := ta2.App().Spellcheck(); again != created {
		t.Fatal("Spellcheck() created a second service for one app")
	}
}

// TestSpellcheckServiceDeliveriesWaitForFlushWithoutHost: in a hostless
// (test) application nothing delivers until the foreground drains the
// queue, proving the delivery path never runs inline on the worker.
func TestSpellcheckServiceDeliveriesWaitForFlushWithoutHost(t *testing.T) {
	ta := gpui.NewTestApp()
	defer ta.App().Shutdown()
	fake := &fakeSpellChecker{misspellBy: map[string][]gpui.Misspelling{
		"quikc": {{Start: 0, End: 5}},
	}}
	svc := gpui.NewSpellcheckService(ta.App(), fake)
	defer svc.Close()

	delivered := make(chan struct{}, 1)
	svc.Check("input", "quikc", "en-US", func(gpui.SpellcheckResult, *gpui.App) {
		delivered <- struct{}{}
	})
	// Wait until the worker certainly finished (the check ran) without
	// flushing: a completion sitting in the queue is the expectation.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && len(fake.checksSnapshot()) == 0 {
		time.Sleep(2 * time.Millisecond)
	}
	if len(fake.checksSnapshot()) == 0 {
		t.Fatal("the check never ran on the worker")
	}
	select {
	case <-delivered:
		t.Fatal("the result delivered without a foreground flush — deliveries must not run on the worker")
	default:
	}
	if n := svc.FlushDeliveries(); n != 1 {
		t.Fatalf("flush drained %d deliveries, want 1", n)
	}
	select {
	case <-delivered:
	default:
		t.Fatal("the result did not deliver after the flush")
	}
}
