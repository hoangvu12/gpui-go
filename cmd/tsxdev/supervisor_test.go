//go:build windows

package main

// The supervisor's decisions (ticket 66), driven against fakes: no
// real rebuilds, no real app processes. The watcher and the debounce
// are real (a temp tree and short intervals), because what is under
// test is the wiring between a file edit and the command the child
// receives — the decisions a real session makes on a real save.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeChild is one running app, as the supervisor sees it: the lines
// it was sent, whether it was killed, and an exit the test controls.
type fakeChild struct {
	pid int
	exe string

	mu     sync.Mutex
	sent   []string
	killed bool

	exit chan error // cap 1: the test (or Kill) delivers the exit
}

func (c *fakeChild) Send(line string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sent = append(c.sent, line)
	return nil
}

func (c *fakeChild) Exited() <-chan error { return c.exit }

// Kill records the kill and ends the process, like TerminateProcess
// making cmd.Wait return.
func (c *fakeChild) Kill() error {
	c.mu.Lock()
	c.killed = true
	c.mu.Unlock()
	select {
	case c.exit <- errors.New("killed"):
	default:
	}
	return nil
}

func (c *fakeChild) Pid() int { return c.pid }

func (c *fakeChild) lines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.sent...)
}

func (c *fakeChild) wasKilled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.killed
}

// exitNow delivers a clean exit.
func (c *fakeChild) exitNow() {
	c.exit <- nil
}

// fakeBuilder hands out program paths; results scripts what each Build
// returns (a path or an error).
type fakeBuilder struct {
	mu      sync.Mutex
	results []buildResult
	calls   int
}

type buildResult struct {
	exe string
	err error
}

func (b *fakeBuilder) Build(context.Context) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	if b.calls <= len(b.results) {
		r := b.results[b.calls-1]
		return r.exe, r.err
	}
	return fmt.Sprintf("app-%04d.exe", b.calls), nil
}

func (b *fakeBuilder) buildCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// fakeSpawner starts fake children and remembers them.
type fakeSpawner struct {
	mu       sync.Mutex
	children []*fakeChild
	nextPid  int
}

func (s *fakeSpawner) Start(exe string) (Child, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextPid++
	child := &fakeChild{pid: s.nextPid, exe: exe, exit: make(chan error, 1)}
	s.children = append(s.children, child)
	return child, nil
}

func (s *fakeSpawner) started() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.children)
}

func (s *fakeSpawner) child(n int) *fakeChild {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.children[n]
}

// devSession is one supervised run: a temp app tree, fakes for the
// build and the child, fast poll and debounce.
type devSession struct {
	t       *testing.T
	root    string
	build   *fakeBuilder
	spawn   *fakeSpawner
	sup     *Supervisor
	cancel  context.CancelFunc
	done    chan error
	logsMu  sync.Mutex
	logs    []string
	started chan struct{}
}

// newDevSession sets the tree up and starts the supervisor. When
// waitStart is set, it waits for the first child (every test but the
// first-build-retry one, which must observe the childless waiting).
func newDevSession(t *testing.T, grace time.Duration, waitStart bool, builds ...buildResult) *devSession {
	t.Helper()
	root := t.TempDir()
	writeDevFile(t, root, "main.go", "package main\n\nfunc main() {}\n")
	writeDevFile(t, root, filepath.Join("ui", "app.tsx"), "export function App() {}\n")

	build := &fakeBuilder{results: builds}
	spawn := &fakeSpawner{}
	s := &devSession{
		t:       t,
		root:    root,
		build:   build,
		spawn:   spawn,
		done:    make(chan error, 1),
		started: make(chan struct{}, 1),
	}
	s.sup = &Supervisor{
		Root:      root,
		WorkDir:   t.TempDir(),
		StateFile: filepath.Join(t.TempDir(), "state.json"),
		Build:     build,
		Spawn:     spawn,
		Poll:      10 * time.Millisecond,
		Debounce:  10 * time.Millisecond,
		Grace:     grace,
		Logf:      s.log,
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	go func() {
		s.done <- s.sup.Run(ctx)
	}()
	if waitStart {
		s.waitStarted()
	}
	return s
}

// log collects the session's events.
func (s *devSession) log(format string, args ...any) {
	s.logsMu.Lock()
	defer s.logsMu.Unlock()
	s.logs = append(s.logs, fmt.Sprintf(format, args...))
}

// waitStarted waits for the first child.
func (s *devSession) waitStarted() {
	s.t.Helper()
	waitFor(s.t, 5*time.Second, func() bool { return s.spawn.started() >= 1 }, "the first child started")
}

// log reports whether the session logged something containing substr.
func (s *devSession) logHas(substr string) bool {
	s.logsMu.Lock()
	defer s.logsMu.Unlock()
	for _, line := range s.logs {
		if strings.Contains(line, substr) {
			return true
		}
	}
	return false
}

// stop cancels and waits for Run's result.
func (s *devSession) stop() error {
	s.t.Helper()
	s.cancel()
	select {
	case err := <-s.done:
		return err
	case <-time.After(30 * time.Second):
		s.t.Fatal("the supervisor did not stop")
		return nil
	}
}

// gracefulStop ends the session the way Ctrl+C does: the app is asked
// to save, it exits when asked, and nothing is killed.
func (s *devSession) gracefulStop(child *fakeChild) error {
	s.t.Helper()
	s.cancel()
	waitFor(s.t, 5*time.Second, func() bool {
		for _, line := range child.lines() {
			if line == "save" {
				return true
			}
		}
		return false
	}, "the stop's save command")
	child.exitNow()
	select {
	case err := <-s.done:
		return err
	case <-time.After(30 * time.Second):
		s.t.Fatal("the supervisor did not stop after the graceful save")
		return nil
	}
}

// edit rewrites (or creates) a file in the app tree — the save the
// watcher has to see.
func (s *devSession) edit(rel string, content string) {
	s.t.Helper()
	writeDevFile(s.t, s.root, rel, content)
}

func writeDevFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// waitFor polls cond until it holds or the timeout passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

// A .tsx edit sends the reload command to the running app, and nothing
// else: no build, no exit, no kill.
func TestSupervisorSendsReloadForATsxEdit(t *testing.T) {
	s := newDevSession(t, 40*time.Millisecond, true)
	child := s.spawn.child(0)

	s.edit(filepath.Join("ui", "app.tsx"), "export function App() { return <div/>; }\n")

	waitFor(t, 5*time.Second, func() bool {
		for _, line := range child.lines() {
			if line == "reload" {
				return true
			}
		}
		return false
	}, "the reload command")

	if lines := child.lines(); len(lines) != 1 {
		t.Fatalf("the child received %v, want only the reload", lines)
	}
	if s.build.buildCount() != 1 {
		t.Fatalf("a .tsx edit built: %d builds", s.build.buildCount())
	}
	if child.wasKilled() {
		t.Fatal("a .tsx edit killed the app")
	}
	if err := s.stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

// A .go edit restarts: the save command, the app's exit, a rebuild,
// and a fresh child. The old child was asked to save and was not
// killed (it exited when asked).
func TestSupervisorRestartsOnAGoEdit(t *testing.T) {
	s := newDevSession(t, 5*time.Second, true)
	child := s.spawn.child(0)

	s.edit("main.go", "package main\n\nfunc main() { println(2) }\n")

	waitFor(t, 5*time.Second, func() bool {
		for _, line := range child.lines() {
			if line == "save" {
				return true
			}
		}
		return false
	}, "the save command")

	child.exitNow() // the app saved and exited, like the protocol says
	waitFor(t, 5*time.Second, func() bool { return s.spawn.started() >= 2 }, "the replacement child")

	child2 := s.spawn.child(1)
	if s.build.buildCount() != 2 {
		t.Fatalf("the restart built %d times, want 2", s.build.buildCount())
	}
	if child.wasKilled() {
		t.Fatal("an app that exited when asked was killed")
	}
	if child2.wasKilled() {
		t.Fatal("the replacement was killed")
	}
	if lines := child2.lines(); len(lines) != 0 {
		t.Fatalf("the replacement received %v before any edit", lines)
	}
	// The graceful stop asks the replacement to save too.
	if err := s.gracefulStop(child2); err != nil {
		t.Fatalf("stop: %v", err)
	}
	for _, line := range child2.lines() {
		if line != "save" {
			t.Fatalf("the replacement received %q, want only the save", line)
		}
	}
}

// An app that does not exit when asked is killed — exactly that
// process, after the grace period — and the replacement still starts.
func TestSupervisorKillsAnAppThatLingers(t *testing.T) {
	s := newDevSession(t, 30*time.Millisecond, true)
	child := s.spawn.child(0)

	s.edit("main.go", "package main\n\nfunc main() { println(3) }\n")

	waitFor(t, 5*time.Second, func() bool { return child.wasKilled() }, "the lingering app's kill")
	waitFor(t, 5*time.Second, func() bool { return s.spawn.started() >= 2 }, "the replacement child")

	for _, line := range child.lines() {
		if line != "save" {
			t.Fatalf("the lingering app received %q before being killed", line)
		}
	}
	if err := s.stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

// A rebuild that fails keeps the running app up: no save, no kill, no
// replacement — the mirror of a failed reload keeping the last good
// tree. The next edit retries.
func TestSupervisorKeepsTheAppWhenTheRebuildFails(t *testing.T) {
	s := newDevSession(t, 5*time.Second, true,
		buildResult{exe: "app-0001.exe"},                          // the initial build
		buildResult{err: errors.New("main.go:5:2: undefined: x")}, // the restart's
	)
	child := s.spawn.child(0)

	s.edit("main.go", "package main\n\nfunc main() { x }\n")

	waitFor(t, 5*time.Second, func() bool { return s.build.buildCount() >= 2 }, "the failed rebuild attempt")
	// Give the supervisor a moment to (wrongly) act on the failure.
	time.Sleep(100 * time.Millisecond)

	if lines := child.lines(); len(lines) != 0 {
		t.Fatalf("the app received %v while its rebuild failed", lines)
	}
	if child.wasKilled() {
		t.Fatal("a failed rebuild killed the running app")
	}
	if s.spawn.started() != 1 {
		t.Fatalf("a failed rebuild started a replacement: %d children", s.spawn.started())
	}

	// The sources are fixed; the next edit restarts: this build
	// succeeds, so the app is asked to save, exits, and a replacement
	// starts.
	s.edit("main.go", "package main\n\nfunc main() { println(4) }\n")
	waitFor(t, 5*time.Second, func() bool {
		for _, line := range child.lines() {
			if line == "save" {
				return true
			}
		}
		return false
	}, "the retry's save command")
	child.exitNow()
	waitFor(t, 5*time.Second, func() bool { return s.spawn.started() >= 2 }, "the retry's replacement")
	if err := s.gracefulStop(s.spawn.child(1)); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

// The session's own stop (Ctrl+C, a quit line, a closed stdin pipe)
// is graceful: the app is asked to save and exits; it is killed only
// if it lingers (the reload test's short grace covers that half).
func TestSupervisorStopsGracefully(t *testing.T) {
	s := newDevSession(t, 5*time.Second, true)
	child := s.spawn.child(0)
	if err := s.gracefulStop(child); err != nil {
		t.Fatalf("graceful stop: %v", err)
	}
	if child.wasKilled() {
		t.Fatal("a graceful stop killed the app")
	}
}

// When the app exits on its own (the user closed its window), the
// session is over: the supervisor exits with the app's code.
func TestSupervisorEndsWithTheApp(t *testing.T) {
	s := newDevSession(t, 5*time.Second, true)
	child := s.spawn.child(0)
	child.exitNow()

	select {
	case err := <-s.done:
		if err != nil {
			t.Fatalf("a clean app exit ended the session with %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the supervisor kept running after the app exited")
	}
}

func TestSupervisorEndsWithTheAppsExitCode(t *testing.T) {
	s := newDevSession(t, 5*time.Second, true)
	child := s.spawn.child(0)
	child.exit <- errors.New("crash") // a non-exit error: exit code 1

	select {
	case err := <-s.done:
		var code *exitError
		if !errors.As(err, &code) || code.code != 1 {
			t.Fatalf("a crashed app ended the session with %v, want exit code 1", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the supervisor kept running after the app crashed")
	}
}

// The first build failing is not the end: the supervisor waits and
// retries, and the session starts when the sources compile again.
func TestSupervisorRetriesTheFirstBuild(t *testing.T) {
	oldRetry := buildRetry
	buildRetry = 20 * time.Millisecond
	t.Cleanup(func() { buildRetry = oldRetry })

	s := newDevSession(t, 5*time.Second, false, buildResult{err: errors.New("main.go:3:1: syntax error")})
	// The build fails first (logged), no child runs while the sources
	// do not compile, and the retry once they do (the queue is
	// exhausted) starts the session.
	waitFor(t, 5*time.Second, func() bool { return s.logHas("retrying") }, "the retry wait's log")
	if s.spawn.started() != 0 {
		t.Fatalf("a failed first build started a child: %d", s.spawn.started())
	}
	waitFor(t, 5*time.Second, func() bool { return s.spawn.started() >= 1 }, "the retried start")
	if err := s.gracefulStop(s.spawn.child(0)); err != nil {
		t.Fatalf("stop: %v", err)
	}
}
