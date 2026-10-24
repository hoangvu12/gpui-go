package gpui

// The spellcheck service (browser-core ticket 30): text inputs can opt
// into misspelling markers through a core service backed by the
// Windows spell checker (the ISpellCheckerFactory / ISpellChecker COM
// API, spellcheck.h, Windows 8+), running off the UI thread.
//
// The design follows Chromium's WindowsSpellChecker
// (components/spellcheck/browser/windows_spell_checker.cc):
//
//   - every native call runs on ONE dedicated background sequence whose
//     thread owns a COM STA apartment (BackgroundHelper, the class
//     comment at :40-48: "All invocations of these methods must be
//     posted to the same COM |SingleThreadTaskRunner|";
//     CreateSpellCheckerFactory asserts the STA apartment type and
//     CoCreates the factory at :158-184). Chromium builds that sequence
//     with base::ThreadPool::CreateCOMSTATaskRunner
//     (windows_spell_checker_unittest.cc:51-52); the Go port pins its
//     own goroutine with runtime.LockOSThread and CoInitializeEx
//     instead, because COM objects are thread-affine and Go goroutines
//     are not OS threads.
//   - requests are posted to that sequence and results are posted back
//     to the caller's thread (WindowsSpellChecker::RequestTextCheck
//     runs the check on the background runner and replies through
//     PostTaskAndReplyWithResult, :512-524); the port queues requests
//     on a service channel and posts completions to the foreground
//     (the host thread of a real app, the dispatcher goroutine of a
//     test app).
//   - a check NEVER runs on, or waits on, the UI thread: queueing is
//     non-blocking, and the worker goroutine is not a scheduler task,
//     so the deterministic foreground drain never waits on it.
//
// Supersession: a newer request for the same input key supersedes the
// older one — still-queued older requests are skipped before the
// checker runs, and an in-flight request's result is dropped at
// delivery. Keys are arbitrary comparable values (the base input uses
// its entity id); a nil key is never superseded.
//
// The checking seam (SpellChecker) is injectable: the Windows
// implementation lives in spellcheck_windows.go
// (windowsSpellChecker); tests inject fakes through
// NewSpellcheckService. The platform default is reachable through
// App.Spellcheck().

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// ErrSpellcheckLanguageUnsupported is the check error for a language
// the platform spell checker has no dictionary for (the Windows
// ISpellCheckerFactory::IsSupported false path,
// windows_spell_checker.cc:345-360).
var ErrSpellcheckLanguageUnsupported = errors.New("gpui: the platform spell checker has no dictionary for the language")

// Misspelling is one misspelled range of the checked text: UTF-16
// code-unit offsets, the unit the Windows spell checker reports
// (ISpellingError::get_StartIndex / get_Length,
// windows_spell_checker.cc:238-244). End is one past the last code
// unit, matching gpui's UTF16Range convention.
type Misspelling struct {
	// Start is the first UTF-16 code unit of the misspelled range.
	Start int
	// End is one past the last UTF-16 code unit.
	End int
}

// SpellcheckResult is one completed check: the text as checked, its
// misspelled ranges, or the failure that replaced them (a prepare
// failure, an unsupported language, a COM error). A nil Err with no
// Misspellings is a clean check.
type SpellcheckResult struct {
	// Text is the text that was checked.
	Text string
	// Misspellings are the reported ranges, in report order.
	Misspellings []Misspelling
	// Err is the check's failure.
	Err error
}

// SpellChecker is the checking seam the service drives on its
// dedicated thread. Every method runs on that one thread only — the
// service's worker goroutine, pinned to its OS thread and initialized
// by Prepare — so implementations may hold thread-affine native state
// (the Windows implementation's COM apartment). Implementations must
// be created per service and never shared.
type SpellChecker interface {
	// Prepare initializes the thread's native state (the Windows
	// implementation calls CoInitializeEx and creates the
	// ISpellCheckerFactory). An error fails every later operation with
	// that error, like Chromium's null factory
	// (windows_spell_checker.cc:427-430, IsSpellCheckerFactory-
	// Initialized's false path).
	Prepare() error
	// Check returns the misspelled ranges of text under the BCP-47
	// language tag lang (the ISpellChecker::ComprehensiveCheck pass,
	// windows_spell_checker.cc:228-244).
	Check(text, lang string) ([]Misspelling, error)
	// Suggest returns spelling suggestions for word under lang
	// (ISpellChecker::Suggest, windows_spell_checker.cc:296-315).
	Suggest(word, lang string) ([]string, error)
	// Add adds word to the language's user dictionary
	// (windows_spell_checker.cc:317-321).
	Add(word, lang string) error
	// Ignore ignores word for the session
	// (windows_spell_checker.cc:331-338).
	Ignore(word, lang string) error
	// Release disposes the thread's native state (releases the COM
	// interfaces and balances CoInitializeEx).
	Release()
}

// ---------------------------------------------------------------------------
// The service
// ---------------------------------------------------------------------------

// spellcheckOpKind is one queued operation.
type spellcheckOpKind uint8

const (
	spellcheckOpCheck spellcheckOpKind = iota
	spellcheckOpSuggest
	spellcheckOpAdd
	spellcheckOpIgnore
)

// String names the op for diagnostics.
func (k spellcheckOpKind) String() string {
	switch k {
	case spellcheckOpCheck:
		return "Check"
	case spellcheckOpSuggest:
		return "Suggest"
	case spellcheckOpAdd:
		return "Add"
	default:
		return "Ignore"
	}
}

// spellcheckOp is one queued request. All fields are immutable once
// queued except superseded, which the foreground sets atomically when
// a newer request for the same key arrives.
type spellcheckOp struct {
	key        any
	kind       spellcheckOpKind
	text       string
	lang       string
	generation uint64
	superseded atomic.Bool
	// deliverCheck runs on the foreground with the check's result;
	// nil for fire-and-forget checks.
	deliverCheck func(SpellcheckResult, *App)
	// deliverSuggest runs on the foreground with the suggestions.
	deliverSuggest func([]string, error, *App)
}

// SpellcheckService checks text off the UI thread through an injected
// SpellChecker and delivers results on it. Construct with
// NewSpellcheckService (or reach the platform default through
// App.Spellcheck).
//
// Threading contract:
//   - Check, Suggest, Add, Ignore, FlushDeliveries and Close run on the
//     foreground (the host thread of a real application, the
//     dispatcher goroutine of a test application). They never block on
//     the worker: queueing appends to a mutex-guarded queue and
//     returns.
//   - The worker goroutine is pinned to its OS thread (LockOSThread),
//     prepares the checker on it, and runs every operation there. It
//     never touches application state.
//   - Completions are posted to the foreground: through the host's
//     message loop (Host.Post, the Go port of
//     WindowsDispatcher::dispatch_on_main_thread) in a real
//     application, or a queue drained by FlushDeliveries in a test
//     application (there is no host to post to).
type SpellcheckService struct {
	app     *App
	checker SpellChecker

	// queue holds pending ops; notify (capacity 1) wakes the worker.
	// Both live under mu; the worker drains the whole queue each pass,
	// so one notify covers any number of enqueues.
	mu    sync.Mutex
	queue []*spellcheckOp
	// queuedDeliveries holds completions waiting for a foreground
	// drain in hostless (test) mode.
	queuedDeliveries []func()
	closed           bool

	notify chan struct{}
	done   chan struct{}

	// hostSnapshot is the host to post completions to, captured on the
	// foreground when an operation is queued (a host attaches before
	// any window renders, so a service created by an opt-in input
	// during rendering sees the live host; a pre-Run capture of nil
	// keeps the hostless queue until the next queued operation).
	hostSnapshot atomic.Pointer[Host]

	// The fields below are foreground-only (touched by the queueing
	// methods and by completions, both on the foreground):
	// generations is the per-key request counter behind supersession.
	generations map[any]uint64
	// pending holds the not-yet-completed ops per key, so a newer
	// request can mark them superseded.
	pending map[any][]*spellcheckOp
	// retired is set by Close and observed by completions to drop late
	// results (foreground like the maps; the atomic only keeps that
	// read unfussy under the same-thread posting machinery).
	retired atomic.Bool
}

// NewSpellcheckService creates a service over checker that delivers
// on app's foreground thread. The worker goroutine starts immediately
// and prepares the checker on its own locked OS thread; close the
// service (Close, or the application's Shutdown, which retires it)
// when done.
func NewSpellcheckService(app *App, checker SpellChecker) *SpellcheckService {
	if app == nil {
		panic("gpui: NewSpellcheckService requires an owning application")
	}
	if checker == nil {
		panic("gpui: NewSpellcheckService requires a checker")
	}
	s := &SpellcheckService{
		app:         app,
		checker:     checker,
		notify:      make(chan struct{}, 1),
		done:        make(chan struct{}),
		generations: make(map[any]uint64),
		pending:     make(map[any][]*spellcheckOp),
	}
	go s.run()
	return s
}

// Check queues a check of text under lang and delivers the result on
// the foreground through deliver (nil deliver: fire and forget). key
// identifies the requesting input for supersession: a newer Check or
// Suggest with the same key supersedes this one while it is queued or
// in flight, and its result is dropped instead of delivered. The key
// must be comparable; nil never supersedes.
func (s *SpellcheckService) Check(key any, text, lang string, deliver func(SpellcheckResult, *App)) {
	s.queueOp(&spellcheckOp{
		key:          key,
		kind:         spellcheckOpCheck,
		text:         text,
		lang:         lang,
		deliverCheck: deliver,
	})
}

// Suggest queues a suggestion lookup for word under lang and delivers
// the suggestions (or the failure) on the foreground through deliver.
// key supersedes like Check's.
func (s *SpellcheckService) Suggest(key any, word, lang string, deliver func(suggestions []string, err error, app *App)) {
	s.queueOp(&spellcheckOp{
		key:            key,
		kind:           spellcheckOpSuggest,
		text:           word,
		lang:           lang,
		deliverSuggest: deliver,
	})
}

// Add queues adding word to lang's user dictionary. Fire and forget:
// dictionary changes reach later checks; the operation never blocks
// the caller.
func (s *SpellcheckService) Add(word, lang string) {
	s.queueOp(&spellcheckOp{kind: spellcheckOpAdd, text: word, lang: lang})
}

// Ignore queues ignoring word under lang for the session. Fire and
// forget, like Add.
func (s *SpellcheckService) Ignore(word, lang string) {
	s.queueOp(&spellcheckOp{kind: spellcheckOpIgnore, text: word, lang: lang})
}

// queueOp registers and enqueues one operation on the foreground.
func (s *SpellcheckService) queueOp(op *spellcheckOp) {
	s.app.sched.rejectIfShuttingDown("SpellcheckService." + op.kind.String())
	// Capture the posting target for completions (foreground read; see
	// the type's threading contract).
	s.hostSnapshot.Store(s.app.host)
	if op.key != nil {
		s.generations[op.key]++
		op.generation = s.generations[op.key]
		// Every still-pending op of this key is superseded: a queued
		// one will be skipped before the checker runs, an in-flight
		// one will drop its result at delivery.
		for _, old := range s.pending[op.key] {
			old.superseded.Store(true)
		}
		delete(s.pending, op.key)
		s.pending[op.key] = []*spellcheckOp{op}
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.queue = append(s.queue, op)
	s.mu.Unlock()
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// FlushDeliveries drains the completions that arrived while no host
// was there to post them to, running each on the calling (foreground)
// goroutine, and returns how many ran. Real applications never need
// this — completions post through the host's message loop; test
// applications call this from their dispatcher loop.
func (s *SpellcheckService) FlushDeliveries() int {
	s.mu.Lock()
	queued := s.queuedDeliveries
	s.queuedDeliveries = nil
	s.mu.Unlock()
	for _, f := range queued {
		f()
	}
	return len(queued)
}

// Close retires the service: queued operations are dropped, the worker
// finishes its in-flight operation, releases the checker and exits.
// Close is idempotent and safe to call from the foreground; it waits
// a bounded time (2s) for the worker so a stuck COM call cannot hang
// teardown forever.
func (s *SpellcheckService) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.queue = nil
	s.queuedDeliveries = nil
	s.mu.Unlock()
	s.retired.Store(true)
	// Pending foreground bookkeeping is retired with the service.
	for key, ops := range s.pending {
		for _, op := range ops {
			op.superseded.Store(true)
		}
		delete(s.pending, key)
	}
	select {
	case s.notify <- struct{}{}:
	default:
	}
	select {
	case <-s.done:
	case <-time.After(2 * time.Second):
	}
}

// run is the worker: one goroutine, pinned to one OS thread, owning
// the checker for the service's lifetime.
func (s *SpellcheckService) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(s.done)
	prepareErr := s.checker.Prepare()
	defer s.checker.Release()
	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			if s.closed {
				s.mu.Unlock()
				return
			}
			s.mu.Unlock()
			<-s.notify
			continue
		}
		batch := s.queue
		s.queue = nil
		s.mu.Unlock()
		for _, op := range batch {
			s.runOp(op, prepareErr)
		}
	}
}

// runOp executes one operation on the worker thread and posts its
// completion to the foreground.
func (s *SpellcheckService) runOp(op *spellcheckOp, prepareErr error) {
	if op.superseded.Load() {
		// A newer request for this key superseded this one while it
		// was queued; its foreground bookkeeping was already retired.
		return
	}
	switch op.kind {
	case spellcheckOpCheck:
		result := SpellcheckResult{Text: op.text}
		if prepareErr != nil {
			result.Err = prepareErr
		} else {
			misspellings, err := guarded(func() ([]Misspelling, error) {
				return s.checker.Check(op.text, op.lang)
			})
			result.Err, result.Misspellings = err, misspellings
		}
		s.post(func() {
			s.complete(op, func(app *App) {
				if op.deliverCheck != nil {
					op.deliverCheck(result, app)
				}
			})
		})
	case spellcheckOpSuggest:
		var suggestions []string
		var err error
		if prepareErr != nil {
			err = prepareErr
		} else {
			suggestions, err = guarded(func() ([]string, error) {
				return s.checker.Suggest(op.text, op.lang)
			})
		}
		s.post(func() {
			s.complete(op, func(app *App) {
				if op.deliverSuggest != nil {
					op.deliverSuggest(suggestions, err, app)
				}
			})
		})
	case spellcheckOpAdd:
		if prepareErr == nil {
			_, _ = guarded(func() (struct{}, error) {
				return struct{}{}, s.checker.Add(op.text, op.lang)
			})
		}
	case spellcheckOpIgnore:
		if prepareErr == nil {
			_, _ = guarded(func() (struct{}, error) {
				return struct{}{}, s.checker.Ignore(op.text, op.lang)
			})
		}
	}
}

// guarded runs one checker call, converting a panic inside the seam
// into an error: the worker must never die from a bad checker (its
// completions would stop arriving and Close would hang on it).
func guarded[T any](f func() (T, error)) (value T, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("gpui: the spell checker panicked: %v", r)
		}
	}()
	return f()
}

// post delivers one completion on the foreground: through the host's
// message loop when a live host exists (real applications), otherwise
// queued for FlushDeliveries (test applications). Called from the
// worker goroutine.
func (s *SpellcheckService) post(f func()) {
	if f == nil {
		return
	}
	if spellcheckPostToHost(s.hostSnapshot.Load(), f) {
		return
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.queuedDeliveries = append(s.queuedDeliveries, f)
	s.mu.Unlock()
}

// complete runs one posted completion on the foreground: retire the
// op's bookkeeping, drop superseded or stale results, and deliver.
func (s *SpellcheckService) complete(op *spellcheckOp, deliver func(app *App)) {
	if op.key != nil {
		pending := s.pending[op.key]
		for i, candidate := range pending {
			if candidate == op {
				pending = append(pending[:i], pending[i+1:]...)
				break
			}
		}
		if len(pending) == 0 {
			delete(s.pending, op.key)
		} else {
			s.pending[op.key] = pending
		}
	}
	if s.retired.Load() || op.superseded.Load() {
		return
	}
	if op.key != nil && s.generations[op.key] != op.generation {
		// A newer request superseded this result while it was in
		// flight.
		return
	}
	if deliver != nil {
		s.app.Update(func(cx *App) { deliver(cx) })
	}
}

// ---------------------------------------------------------------------------
// The App surface
// ---------------------------------------------------------------------------

// Spellcheck returns the application's spellcheck service, creating
// it on first use with the platform's default checker (the Windows
// spell checker on Windows; a checker that fails every operation on
// other platforms). Applications replace it (for tests, or to wrap it)
// with SetSpellcheck before first use.
func (a *App) Spellcheck() *SpellcheckService {
	if s := a.spellcheck; s != nil {
		return s
	}
	a.sched.rejectIfShuttingDown("Spellcheck")
	s := NewSpellcheckService(a, newPlatformSpellChecker())
	a.spellcheck = s
	return s
}

// SetSpellcheck installs service as the application's spellcheck
// service, retiring the previous one. A nil service clears the slot
// (the next Spellcheck call re-creates the platform default). The
// service must belong to this application.
func (a *App) SetSpellcheck(service *SpellcheckService) {
	if service != nil && service.app != a {
		panic("gpui: SetSpellcheck requires a service of this application")
	}
	if old := a.spellcheck; old != nil && old != service {
		old.Close()
	}
	a.spellcheck = service
}
