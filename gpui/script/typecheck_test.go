//go:build windows

package script_test

// The reload-time type check (ticket 63, ADR 0003's "debug builds
// check after every reload"): a host with a TypeChecker installed runs
// it after every reload that succeeds, delivers the diagnostics on the
// host thread, never lets them gate the running UI, and drops the
// result of a check a newer reload replaced.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui/script"
)

// fakeChecker is a TypeChecker whose checks return fixed diagnostics
// (or block on a gate), recording every call.
type fakeChecker struct {
	mu      sync.Mutex
	calls   int
	diags   []script.TypeDiagnostic
	err     error
	gate    chan struct{} // when non-nil, each Check blocks until it is closed
	checked chan struct{} // when non-nil, signals each Check's start
}

func (f *fakeChecker) Check() ([]script.TypeDiagnostic, error) {
	f.mu.Lock()
	f.calls++
	f.mu.Unlock()
	if f.checked != nil {
		f.checked <- struct{}{}
	}
	if f.gate != nil {
		<-f.gate
	}
	return f.diags, f.err
}

func (f *fakeChecker) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeScheduler is a Scheduler whose Run does the work on a goroutine
// and marshals the completion back through Post (the shape the host's
// own scheduler has), so the async delivery is exercised without a
// gpui app.
type fakeScheduler struct {
	mu    sync.Mutex
	posts int
}

func (s *fakeScheduler) Post(fn func()) {
	s.mu.Lock()
	s.posts++
	s.mu.Unlock()
	fn()
}

func (s *fakeScheduler) After(d time.Duration, fn func()) func() {
	go fn()
	return func() {}
}

func (s *fakeScheduler) Run(ctx context.Context, work func(context.Context), done func()) func() {
	go func() {
		work(ctx)
		s.Post(done)
	}()
	return func() {}
}

func (s *fakeScheduler) Now() int64 { return 0 }

const simpleView = `
export function App() {
	return <div id="root"><text>hi</text></div>;
}
`

// newTypedHost loads a view into a host wired to the given checker and
// diagnostics hook.
func newTypedHost(t testing.TB, src string, c script.TypeChecker, onDiags func([]script.TypeDiagnostic)) (*script.Host, *srcView) {
	t.Helper()
	s := &srcView{src: src}
	opts := []script.Option{script.WithSource(s)}
	if c != nil {
		opts = append(opts, script.WithTypeCheck(c))
	}
	if onDiags != nil {
		opts = append(opts, script.WithTypeDiagnostics(onDiags))
	}
	h := script.New(opts...)
	if err := h.Reload(); err != nil {
		t.Fatalf("load view.tsx: %v", err)
	}
	return h, s
}

func TestSuccessfulReloadRunsTheTypeCheck(t *testing.T) {
	// The initial load's check runs synchronously (no scheduler); a
	// reload runs another.
	fc := &fakeChecker{diags: []script.TypeDiagnostic{{
		Kind: "error", Code: "TS2322", File: "view.tsx", Line: 3, Column: 7, Message: "bad",
	}}}
	got := make(chan []script.TypeDiagnostic, 4)
	h, s := newTypedHost(t, simpleView, fc, func(d []script.TypeDiagnostic) { got <- d })
	select {
	case diags := <-got:
		if len(diags) != 1 || diags[0].Message != "bad" || diags[0].Line != 3 {
			t.Fatalf("diagnostics = %+v", diags)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("the load's check never ran")
	}
	if err := reload(t, h, s, simpleView); err != nil {
		t.Fatalf("reload: %v", err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatalf("the reload's check never ran")
	}
	if fc.count() != 2 {
		t.Fatalf("checks = %d, want 2 (one per load and reload)", fc.count())
	}
}

func TestAFailedReloadSkipsTheTypeCheck(t *testing.T) {
	fc := &fakeChecker{}
	h, s := newTypedHost(t, simpleView, fc, nil)
	if err := reload(t, h, s, "export function App("); err == nil {
		t.Fatalf("the broken source loaded")
	}
	if fc.count() != 1 {
		t.Fatalf("checks = %d, want 1 (only the successful load; the failed reload skips the check)", fc.count())
	}
}

func TestTheCheckRunsOffTheHostThreadAndDeliversThroughTheScheduler(t *testing.T) {
	fc := &fakeChecker{diags: []script.TypeDiagnostic{{Kind: "error", Code: "TS1", File: "view.tsx", Line: 1, Message: "m"}}}
	sched := &fakeScheduler{}
	got := make(chan []script.TypeDiagnostic, 4)
	s := &srcView{src: simpleView}
	h := script.New(
		script.WithSource(s),
		script.WithScheduler(sched),
		script.WithTypeCheck(fc),
		script.WithTypeDiagnostics(func(d []script.TypeDiagnostic) { got <- d }))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatalf("the diagnostics never arrived through the scheduler")
	}
	sched.mu.Lock()
	posts := sched.posts
	sched.mu.Unlock()
	if posts == 0 {
		t.Fatalf("the completion never marshalled back through Post")
	}
}

func TestAReloadThatLandsDuringACheckDropsItsResult(t *testing.T) {
	// The first check blocks; a second reload replaces the sources; the
	// blocked check's result is dropped, the fresh one's delivered. The
	// scheduler path keeps Reload returning while a check is in flight.
	fc := &fakeChecker{
		diags:   []script.TypeDiagnostic{{Kind: "error", Code: "TS1", File: "view.tsx", Line: 1, Message: "stale"}},
		gate:    make(chan struct{}),
		checked: make(chan struct{}, 4),
	}
	sched := &fakeScheduler{}
	got := make(chan []script.TypeDiagnostic, 4)
	s := &srcView{src: simpleView}
	h := script.New(
		script.WithSource(s),
		script.WithScheduler(sched),
		script.WithTypeCheck(fc),
		script.WithTypeDiagnostics(func(d []script.TypeDiagnostic) { got <- d }))
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
	<-fc.checked // the first check is in flight (blocked on its gate)
	// A second, different source: a newer generation.
	s.Set(simpleView + "\n")
	if err := h.Reload(); err != nil {
		t.Fatalf("reload 2: %v", err)
	}
	<-fc.checked
	// Release both checks: the older generation's result is dropped,
	// the fresh one delivered.
	close(fc.gate)
	select {
	case diags := <-got:
		if len(diags) != 1 {
			t.Fatalf("diagnostics = %+v, want the one fresh delivery", diags)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("the fresh check's diagnostics never arrived")
	}
	select {
	case extra := <-got:
		t.Fatalf("a stale check's result was delivered too: %+v", extra)
	default:
	}
}

func TestACheckerErrorIsReportedNotSwallowed(t *testing.T) {
	// The hook receives nothing when the checker itself fails (the
	// message went to stderr); the host stays alive and reloads.
	fc := &fakeChecker{err: context.Canceled}
	h, s := newTypedHost(t, simpleView, fc, func(d []script.TypeDiagnostic) {
		t.Errorf("the hook ran for a checker error: %+v", d)
	})
	if err := reload(t, h, s, simpleView); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got, _ := h.Text("root"); got != "hi" {
		t.Fatalf("root text = %q, want the running UI", got)
	}
}
