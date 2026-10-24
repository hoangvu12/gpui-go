// Command tsxdev is gpui-go's dev command (ADR 0003, ticket 66): the
// `tauri dev` loop on Go. It builds an app, runs it as its child
// process and watches the app's package directory:
//
//		go run github.com/hoangvu12/gpui-go/cmd/tsxdev .
//
//	  - a .tsx/.ts/.js/.json change (a UI save) tells the running app to
//	    reload in place — gpui/script's reload: the fresh goja runtime
//	    re-describes the tree, keyed state keeps its values (~4 ms of
//	    script work), and a compile error keeps the last good tree;
//	  - a .go/go.mod/go.sum change rebuilds first (a failed build keeps
//	    the running app up, like a failed reload keeps the last good
//	    tree), then asks the app to save its state and exit, and starts
//	    the rebuilt binary — which restores what was saved.
//
// What survives a restart, through a state file:
//
//	state (keyed)  the host's keyed state map — script.WithStateSnapshot
//	shared         the app's shared store's values — Shared.Restore
//	window         the window's logical bounds — Window.SetBounds
//
// The app opts in with a dev-mode file behind the dev build tag (see
// examples/tsx-hello: compile.Open for the .tsx entry, script.RunDev
// for the command stream), which is why the supervisor builds with
// `-tags dev` by default — a release build of the same app embeds the
// bundle and links no esbuild, while the dev build compiles .tsx on
// demand.
//
// The protocol between this supervisor and the app is small, robust and
// versioned in gpui/script/dev.go (one implementation of each half, so
// they cannot drift): the GPUI_DEV_STATE environment variable names the
// state file, and the app's stdin carries newline-terminated commands
// ("reload", "save"). The supervisor never interprets the state file —
// it is a handoff between two runs of the app.
//
// Process discipline: the supervisor starts exactly one child at a
// time and kills only the process it started (its own exec.Cmd), never
// another go or app process. Ctrl+C (or a "quit" line, or a closed
// stdin pipe) stops the session the graceful way: the app is asked to
// save and exit, waited for, and killed only if it lingered.
//
// The watcher is cheap: a poll of the tree's source files (path, size
// and a content hash — Windows updates a rewritten file's modification
// time lazily, so a same-size save needs the hash) every -poll, with a
// -debounce quiet period so one save (or an editor's write-then-rename)
// lands as one reload, not a burst. No busy loop: between polls the
// watcher goroutine sleeps.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// toolName names the supervisor in diagnostics.
const toolName = "tsxdev"

// buildRetry is how long a failed build waits before trying again (the
// sources may be mid-edit).
var buildRetry = 2 * time.Second

func main() {
	code, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, toolName+":", err)
		code = 1
	}
	os.Exit(code)
}

// run is the command, testable without a process: it parses the flags,
// builds the supervisor and runs it to completion.
func run(args []string) (int, error) {
	flags := flag.NewFlagSet(toolName, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	stateFlag := flags.String("state", "", "the restart state file (default: a fresh file in the supervisor's work directory, gone when it exits)")
	tags := flags.String("tags", "dev", "build tags the app's dev mode compiles under (the app's dev file is `dev`-gated; empty: none)")
	poll := flags.Duration("poll", 200*time.Millisecond, "file poll interval")
	debounce := flags.Duration("debounce", 250*time.Millisecond, "quiet period after a change before acting on it")
	grace := flags.Duration("grace", 5*time.Second, "how long to wait for the app to save its state and exit before killing it")
	if err := flags.Parse(args); err != nil {
		return 2, err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return 2, fmt.Errorf("usage: %s [flags] <app-package-dir>", toolName)
	}
	root, err := filepath.Abs(flags.Arg(0))
	if err != nil {
		return 2, err
	}
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return 2, fmt.Errorf("%s is not a directory", root)
	}

	workDir, err := os.MkdirTemp("", "tsxdev-")
	if err != nil {
		return 1, err
	}
	// Absolute: the child runs in the app's directory, so a relative
	// -state would name a different file there.
	stateFile := *stateFlag
	if stateFile != "" {
		if stateFile, err = filepath.Abs(stateFile); err != nil {
			return 1, err
		}
	} else {
		stateFile = filepath.Join(workDir, "devstate.json")
	}

	sup := &Supervisor{
		Root:      root,
		WorkDir:   workDir,
		StateFile: stateFile,
		Build:     &goBuilder{root: root, workDir: workDir, tags: *tags},
		Spawn:     &execSpawner{root: root, stateFile: stateFile},
		Poll:      *poll,
		Debounce:  *debounce,
		Grace:     *grace,
		Logf:      func(format string, args ...any) { fmt.Printf("%s: "+format+"\n", append([]any{toolName}, args...)...) },
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	watchStdin(sup.Logf, cancel)
	watchSignals(sup.Logf, cancel)

	err = sup.Run(ctx)
	var code *exitError
	if errors.As(err, &code) {
		return code.code, nil
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}

// exitError carries the app's exit code out of Run: when the app exits
// on its own, the dev session is over and the supervisor exits with
// the same code.
type exitError struct{ code int }

func (e *exitError) Error() string { return fmt.Sprintf("the app exited with code %d", e.code) }

// exitCodeOf maps a cmd.Wait result to a process exit code (a clean
// exit is 0; a non-exit error is reported as 1).
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return 1
}

// watchStdin ends the session on the supervisor's stdin: an explicit
// "quit" (or "exit") line, or a closed pipe — the channel a harness
// (or an enclosing tool) uses to stop the supervisor cleanly, letting
// the app save its state first. EOF on a terminal or a character
// device like NUL is not a stop (a script that redirects from NUL
// still gets a running session); Ctrl+C works everywhere.
func watchStdin(logf func(string, ...any), cancel context.CancelFunc) {
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			switch line := strings.TrimSpace(scanner.Text()); line {
			case "quit", "exit":
				logf("stopping (%q on stdin)", line)
				cancel()
				return
			}
		}
		if stat, err := os.Stdin.Stat(); err == nil && stat.Mode()&os.ModeCharDevice == 0 {
			logf("stdin closed; stopping")
			cancel()
		}
	}()
}

// watchSignals ends the session on Ctrl+C (or SIGTERM where it
// exists): the cancel is the graceful stop — the app is asked to save
// its state and exit before it is killed. The child sits in its own
// process group (proc_windows.go), so the console's Ctrl+C reaches
// this process alone.
func watchSignals(logf func(string, ...any), cancel context.CancelFunc) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-signals
		logf("stopping (%v)", sig)
		cancel()
	}()
}
