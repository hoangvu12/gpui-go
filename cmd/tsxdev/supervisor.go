package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/hoangvu12/gpui-go/gpui/script"
)

// Supervisor runs one dev session: it builds the app, starts it as its
// child, watches the app's package directory, reloads the child on UI
// changes and rebuilds-and-restarts it on Go changes — tauri dev's
// loop, with the restart handoff through the state file (see the
// package comment and gpui/script/dev.go).
//
// The Builder and Spawner seams are the test surface: the supervisor's
// decisions (which command to send, when to build, what to kill) are
// driven against fakes, with no real rebuilds in tests.
type Supervisor struct {
	// Root is the app's package directory: the build's working
	// directory, the watch root and the child's working directory.
	Root string
	// WorkDir holds the built binaries (a temp directory, removed when
	// the session ends).
	WorkDir string
	// StateFile is the restart handoff's path, passed to every child
	// as script.DevEnv.
	StateFile string
	// Build produces runnable app programs.
	Build Builder
	// Spawn starts one of them as the child.
	Spawn Spawner
	// Poll is the file watch interval; Debounce is the quiet period
	// after a change before acting on it.
	Poll, Debounce time.Duration
	// Grace is how long a save-and-exit is waited for before the child
	// is killed.
	Grace time.Duration
	// Logf logs the session's events.
	Logf func(format string, args ...any)
}

// Builder compiles the app; Build returns the program to run. Each
// call produces a fresh path (a running binary is locked on Windows,
// so a restart cannot rebuild in place).
type Builder interface {
	Build(ctx context.Context) (exe string, err error)
}

// Child is the app process the supervisor started. Exited delivers its
// exit result exactly once; Kill terminates exactly this process — the
// supervisor never touches any other.
type Child interface {
	// Send writes one command line to the child's stdin.
	Send(line string) error
	// Exited delivers the process's exit result once, when it exits.
	Exited() <-chan error
	// Kill terminates only this process.
	Kill() error
	// Pid identifies the child in logs.
	Pid() int
}

// Spawner starts a built program as the supervisor's child.
type Spawner interface {
	Start(exe string) (Child, error)
}

// Run drives the session until it ends: the app exits on its own (the
// user closed its window — the supervisor exits with the same code),
// or the context is cancelled (Ctrl+C, a "quit" line, a closed stdin
// pipe — the graceful stop: the app saves first).
func (s *Supervisor) Run(ctx context.Context) error {
	// The baseline scan happens before the first build starts, so an
	// edit made once the child is up is always a change against what
	// the running program was built from.
	w := newWatcher(s.Root)
	w.scan()
	child, exe, err := s.startOrRetry(ctx)
	if err != nil {
		s.cleanup()
		return err
	}
	changes := s.pollLoop(ctx, w)
	debounce := newDebounce(s.Debounce)

	for {
		select {
		case <-ctx.Done():
			return s.stop(child)
		case change := <-changes:
			if change.any() {
				debounce.ping(change)
			}
		case change := <-debounce.fired:
			if child, exe, err = s.act(ctx, child, exe, change); err != nil {
				s.cleanup()
				return err
			}
		case err := <-child.Exited():
			// Outside a restart this is the app's own exit: the session
			// is over (the user closed the window, or it crashed).
			s.Logf("the app exited (code %d); the dev session is over", exitCodeOf(err))
			s.cleanup()
			if err == nil {
				return nil
			}
			return &exitError{exitCodeOf(err)}
		}
	}
}

// act applies one debounced change batch: a reload command for UI
// changes, a rebuild-and-restart for Go changes. A mixed batch restarts
// (the fresh app loads the current sources; a reload first would be
// work the restart throws away).
func (s *Supervisor) act(ctx context.Context, child Child, exe string, change changeSet) (Child, string, error) {
	if change.restart {
		return s.restart(ctx, child, exe, change)
	}
	if err := child.Send("reload"); err != nil {
		s.Logf("the app is gone before the reload landed (%v)", err)
	} else {
		s.Logf("reload sent, %v after the change", roundMS(time.Since(change.detected)))
	}
	return child, exe, nil
}

// restart is the Go-change path: build first — a failed build keeps
// the running app up (the mirror of a failed reload keeping the last
// good tree) — then ask the app to save and exit, wait for it, kill it
// if it lingered, and start the rebuilt program.
func (s *Supervisor) restart(ctx context.Context, child Child, exe string, change changeSet) (Child, string, error) {
	buildStart := time.Now()
	newExe, err := s.Build.Build(ctx)
	if err != nil {
		s.Logf("build failed; the app keeps running:\n%s", err)
		return child, exe, nil
	}
	built := time.Since(buildStart)

	s.swapOut(child)
	child2, err := s.Spawn.Start(newExe)
	if err != nil {
		return nil, "", fmt.Errorf("start the rebuilt app: %w", err)
	}
	s.Logf("rebuilt in %v; the app restarted (pid %d), %v after the change",
		roundMS(built), child2.Pid(), roundMS(time.Since(change.detected)))

	// The old program's process exited, so its file is unlocked.
	if exe != "" && exe != newExe {
		_ = os.Remove(exe)
	}
	return child2, newExe, nil
}

// stop ends the session the graceful way.
func (s *Supervisor) stop(child Child) error {
	s.Logf("stopping: asking the app to save its state, then exiting")
	s.swapOut(child)
	s.cleanup()
	return nil
}

// swapOut ends one child the graceful way: the save command, a wait up
// to Grace, and a kill of exactly that process if it lingered.
func (s *Supervisor) swapOut(child Child) {
	if child == nil {
		return
	}
	if err := child.Send("save"); err != nil {
		s.Logf("the app is already gone (%v)", err)
	}
	if !s.waitExit(child, s.Grace) {
		s.Logf("the app did not exit within %v; killing it", s.Grace)
		if err := child.Kill(); err != nil {
			s.Logf("kill failed: %v", err)
		}
		s.waitExit(child, 10*time.Second)
	}
}

// waitExit waits for the child's exit up to timeout, reporting whether
// it exited (and consuming the exit, so the run loop will not read it
// as the app's own exit).
func (s *Supervisor) waitExit(child Child, timeout time.Duration) bool {
	select {
	case err := <-child.Exited():
		s.Logf("the app exited (code %d)", exitCodeOf(err))
		return true
	case <-time.After(timeout):
		return false
	}
}

// startOrRetry builds and starts the first child. A failed build waits
// and retries: the sources may be mid-edit, and a dev session should
// come up as soon as they compile again.
func (s *Supervisor) startOrRetry(ctx context.Context) (Child, string, error) {
	for {
		exe, err := s.Build.Build(ctx)
		if err == nil {
			child, err := s.Spawn.Start(exe)
			if err != nil {
				return nil, "", fmt.Errorf("start the app: %w", err)
			}
			s.Logf("the app started (pid %d)", child.Pid())
			return child, exe, nil
		}
		s.Logf("build failed; retrying in %v:\n%s", buildRetry, err)
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-time.After(buildRetry):
		}
	}
}

// pollLoop polls the app's tree for source changes, delivering one
// change set per detected batch (the poll loop sleeps between scans;
// there is no busy loop). The watcher's baseline scan happens before
// the loop starts, in Run.
func (s *Supervisor) pollLoop(ctx context.Context, w *watcher) <-chan changeSet {
	changes := make(chan changeSet, 16)
	go func() {
		ticker := time.NewTicker(s.Poll)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if change := w.scan(); change.any() {
					changes <- change
				}
			}
		}
	}()
	return changes
}

// cleanup removes the work directory (its binaries and, unless -state
// was passed, the session's state file). Best effort: a file still
// locked by an exiting process is left behind in the temp directory.
func (s *Supervisor) cleanup() {
	if s.WorkDir != "" {
		_ = os.RemoveAll(s.WorkDir)
	}
}

// roundMS renders a duration in whole milliseconds.
func roundMS(d time.Duration) time.Duration { return d.Round(time.Millisecond) }

// ---------------------------------------------------------------------------
// The real builder and spawner
// ---------------------------------------------------------------------------

// goBuilder builds the app with the Go toolchain into the work
// directory, one fresh path per build (a running program is locked on
// Windows). It inherits the environment, so GOFLAGS, GOCACHE and
// friends reach the build as usual.
type goBuilder struct {
	root    string
	workDir string
	tags    string
	next    int
}

// Build implements Builder.
func (b *goBuilder) Build(ctx context.Context) (string, error) {
	b.next++
	exe := filepath.Join(b.workDir, fmt.Sprintf("app-%04d%s", b.next, exeSuffix()))
	args := []string{"build", "-o", exe}
	if b.tags != "" {
		args = append(args, "-tags", b.tags)
	}
	args = append(args, ".")
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = b.root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go build %s:\n%s", b.root, strings.TrimSpace(string(out)))
	}
	return exe, nil
}

// exeSuffix names the built program for this platform.
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// execSpawner starts built programs as the supervisor's child: the
// app's package directory as its working directory, the state file in
// script.DevEnv, the supervisor's own stdout and stderr (the app's
// output goes where the dev session's output goes), and a stdin pipe
// the commands travel through. The child runs in its own process
// group, so a console Ctrl+C reaches the supervisor alone — it owns
// the graceful shutdown (proc_windows.go).
type execSpawner struct {
	root      string
	stateFile string
}

// Start implements Spawner.
func (sp *execSpawner) Start(exe string) (Child, error) {
	cmd := exec.Command(exe)
	cmd.Dir = sp.root
	cmd.Env = append(os.Environ(), script.DevEnv+"="+sp.stateFile)
	cmd.SysProcAttr = newProcAttr()
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	return &execChild{cmd: cmd, stdin: stdin, exited: exited}, nil
}

// execChild is a running app process: the supervisor's stdin pipe and
// its Wait result.
type execChild struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	exited chan error

	// sendMu serializes command writes (one line at a time).
	sendMu sync.Mutex
}

// Send implements Child: one command line, the protocol's wire form.
func (c *execChild) Send(line string) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	_, err := io.WriteString(c.stdin, line+"\n")
	return err
}

// Exited implements Child.
func (c *execChild) Exited() <-chan error { return c.exited }

// Kill implements Child: TerminateProcess on exactly this process. The
// child spawns no processes of its own, so killing it cannot orphan
// anything; the supervisor never holds a handle to any other process.
func (c *execChild) Kill() error {
	return c.cmd.Process.Kill()
}

// Pid implements Child.
func (c *execChild) Pid() int { return c.cmd.Process.Pid }
