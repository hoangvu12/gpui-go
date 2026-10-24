package script

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
)

// The dev command's child-side hooks (ticket 66, ADR 0003's "Go-side
// dev reload, like tauri dev"). cmd/tsxdev is the supervisor: it
// rebuilds and restarts the app when a Go file changes and tells a
// running app to reload when a .tsx file changes. This file is
// everything an app needs on the child side of that protocol, so the
// protocol has one implementation and the supervisor cannot drift from
// it:
//
//   - the state handoff: the app the supervisor is about to restart
//     saves its keyed state, shared values and window bounds to the
//     state file, and the replacement restores them (below);
//   - the command stream: the supervisor writes newline-terminated
//     commands to the app's stdin — "reload" and "save" — and RunDev
//     runs them on the thread that renders (see doc.go).
//
// The protocol in full, v1 (the supervisor's half is in cmd/tsxdev):
//
//	GPUI_DEV_STATE=<path>   environment: the state file path. The
//	                        supervisor sets it on every app it starts;
//	                        its presence is what puts an app in dev
//	                        mode (examples/tsx-hello's dev build reads
//	                        it in a -tags dev file).
//	stdin: "reload\n"       reload the .tsx sources in place
//	                        (Host.Reload: keyed state keeps its values).
//	stdin: "save\n"         save the dev state and exit 0 — the
//	                        supervisor waits (its -grace), then rebuilds
//	                        and starts the replacement, which restores.
//	stdin: anything else    logged and ignored (a future protocol can
//	                        grow new commands without breaking old
//	                        supervisors or apps).
//
// The state file is JSON, written atomically (a temp file in the same
// directory, then renamed):
//
//	{
//		"state":  {"clicks": 5},                  // the host's keyed state
//		"shared": {"note": "hello"},              // the shared store's values
//		"window": {"x": 160, "y": 120, "width": 480, "height": 320}
//	}
//
// Bounds are logical pixels, exactly what Window.Bounds reports and
// SetBounds takes. A missing file or field is a fresh start; a corrupt
// file is reported (ReadDevState) but does not crash the app.

// DevEnv names the environment variable the dev supervisor sets on the
// app it starts: the path of the dev state file. Its presence is the
// app's signal that it runs under cmd/tsxdev.
const DevEnv = "GPUI_DEV_STATE"

// DevHeadlessEnv names the environment variable that keeps a dev app's
// windows off the user's screen (opened far off the desktop and never
// activated). The supervisor passes its environment through, so
// whatever sets it on the supervisor (a test, a CI job) reaches the
// app; the supervisor itself never sets it — a real dev session wants
// the window on screen. The app side decides what to do with it (see
// examples/tsx-hello's dev build: the position it asks for is the
// off-screen one test windows use).
const DevHeadlessEnv = "GPUI_DEV_HEADLESS"

// DevState is what a dev app leaves for its restart: the host's keyed
// state, the shared store's values and the window's bounds. ReadDevState
// reads it at startup, DevState.Save writes it on the save command,
// and the supervisor never interprets it — the file is a handoff
// between two runs of the app.
type DevState struct {
	// State seeds the host's keyed state map (WithStateSnapshot): the
	// values state(key, init) handed out survive the restart, and each
	// key's init matters only if it did not exist.
	State map[string]any `json:"state,omitempty"`
	// Shared seeds the app's shared store (Shared.Restore).
	Shared map[string]any `json:"shared,omitempty"`
	// Window is the window's last logical bounds, restored with
	// Window.SetBounds.
	Window *DevBounds `json:"window,omitempty"`
}

// DevBounds is a window position and client size in logical pixels —
// the JSON shape of Window.Bounds' result.
type DevBounds struct {
	// X and Y are the window origin in logical screen pixels.
	X float32 `json:"x"`
	Y float32 `json:"y"`
	// Width and Height are the client size in logical pixels.
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

// WindowBounds returns the saved window bounds (ok is false when no
// position was saved — a fresh start keeps the app's own default).
// The restore goes through the public window API: pass the result to
// Window.SetBounds.
func (state DevState) WindowBounds() (gpui.Bounds, bool) {
	if state.Window == nil {
		return gpui.Bounds{}, false
	}
	return state.Window.Bounds()
}

// Bounds returns the bounds as gpui values (ok is false when the saved
// size is not a window: nothing sensible to restore).
func (b DevBounds) Bounds() (gpui.Bounds, bool) {
	if b.Width <= 0 || b.Height <= 0 {
		return gpui.Bounds{}, false
	}
	return gpui.Bounds{
		Origin: gpui.Point{X: b.X, Y: b.Y},
		Size:   gpui.Size{Width: b.Width, Height: b.Height},
	}, true
}

// ReadDevState reads the state file the supervisor pointed DevEnv at.
// A nil path (the app is not under the supervisor), a missing file (a
// fresh start) and a file with no fields all return the zero state. A
// corrupt file returns the zero state and an error naming the file; the
// caller decides whether that is fatal (the sample app logs it and
// starts fresh).
func ReadDevState(path string) (DevState, error) {
	if path == "" {
		return DevState{}, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return DevState{}, nil
	}
	if err != nil {
		return DevState{}, err
	}
	var state DevState
	if err := json.Unmarshal(data, &state); err != nil {
		return DevState{}, fmt.Errorf("script: dev state file %s is corrupt (%v); starting fresh", path, err)
	}
	return state, nil
}

// Save writes the state to path atomically (a uniquely named temp file
// in the same directory, fsynced, then renamed over the target), so a
// crash mid-save can never leave the restart handoff half written. The
// directory must exist.
func (state DevState) Save(path string) error {
	if path == "" {
		return errors.New("script: DevState.Save needs a path")
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("script: dev state is not serializable (keyed state must be data: %w)", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".devstate-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err == nil {
		err = tmp.Sync()
	}
	closeErr := tmp.Close()
	if err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmpName, path)
	}
	if err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

// devBoundsOf is the bounds' JSON shape.
func devBoundsOf(b gpui.Bounds) *DevBounds {
	return &DevBounds{
		X:      b.Origin.X,
		Y:      b.Origin.Y,
		Width:  b.Size.Width,
		Height: b.Size.Height,
	}
}

// ---------------------------------------------------------------------------
// The state hooks: snapshot and restore
// ---------------------------------------------------------------------------

// StateSnapshot returns a copy of the host's keyed state map — the
// values that survive reloads and, through the dev state file, a
// rebuild-and-restart. Call it from the thread that renders, like every
// host method; the snapshot is what DevState.State carries.
func (h *Host) StateSnapshot() map[string]any {
	snapshot := make(map[string]any, len(h.state))
	for key, value := range h.state {
		snapshot[key] = value
	}
	return snapshot
}

// WithStateSnapshot seeds the host's keyed state map before the first
// load — the restore half of the dev restart handoff, seeded from
// DevState.State. The keyed-state contract holds either way: init
// matters only for keys that did not exist, so a seeded value wins over
// the script's init.
func WithStateSnapshot(values map[string]any) Option {
	return func(h *Host) {
		for key, value := range values {
			h.state[key] = value
		}
	}
}

// Snapshot returns a copy of the store's values — the shared half of
// the dev restart handoff (DevState.Shared). Safe from any goroutine.
func (s *Shared) Snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make(map[string]any, len(s.values))
	for key, value := range s.values {
		values[key] = value
	}
	return values
}

// Restore seeds the store with values (DevState.Shared), overwriting
// any same-named keys. It does not notify: the hosts read the values
// back on their next binding recompute or reload — call it before the
// hosts load, like the sample app does at startup.
func (s *Shared) Restore(values map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, value := range values {
		s.values[key] = value
	}
}

// ---------------------------------------------------------------------------
// The command stream: RunDev
// ---------------------------------------------------------------------------

// DevConfig wires an app into the dev supervisor's command stream. The
// app builds it once, after its window is open and the host mounted,
// and hands it to RunDev; the session reads commands until the stream
// ends.
type DevConfig struct {
	// App is the running application. Commands marshal onto its
	// foreground (host) thread — the thread that renders — because the
	// host is not safe for concurrent use (see doc.go). Required.
	App *gpui.App

	// Host is the scripted host the reload command reloads. Required.
	Host *Host

	// Window is the window whose bounds the save command writes (and
	// whose Close ends the app after a save, unless Exit is set). A
	// windowless host can omit it; the saved state then carries no
	// position, and Exit must be set so the save can still end the app.
	Window *gpui.Window

	// Shared is the app's shared store; its values are saved and, on
	// the next run, restored by the app (Shared.Restore). Optional.
	Shared *Shared

	// StateFile is where the save command writes (DevState.Save);
	// the value the supervisor passed in DevEnv. Required.
	StateFile string

	// Input is the command stream; the default is os.Stdin, which is
	// the supervisor's pipe. Tests inject their own reader; the session
	// ends when it returns EOF.
	Input io.Reader

	// Exit runs (on the foreground thread) after a save wrote the
	// state. The default closes the window (Window.Close), which ends
	// app.Run and the process with exit code 0 — the state the
	// replacement app restores. Set it when there is no window, or to
	// leave the app running after a save.
	Exit func()

	// OnReload runs (on the foreground thread) after each successful
	// reload command — the app's chance to report what the fresh
	// sources show (the sample prints the view's label). Reload errors
	// are logged by the session instead.
	OnReload func()

	// Logf logs the session's diagnostics (reload and save results).
	// The default prints to the app's stdout with a "dev:" prefix.
	Logf func(format string, args ...any)
}

// RunDev starts the dev session for cfg and returns it. The session
// reads commands (see the protocol above) on its own goroutine and runs
// each on the application's foreground thread:
//
//	"reload" — Host.Reload; a failed reload keeps the last good tree
//	           and is logged (the banner still shows it).
//	"save"   — DevState{keyed state, shared values, window bounds} is
//	           written to the state file, then Exit runs and the session
//	           ends.
//
// Unknown lines are logged and ignored. The session ends by itself
// when the stream ends (EOF — the supervisor is gone); the session's
// goroutine does not keep the process alive. See doc.go for the
// threading model (every host method runs on the thread that renders;
// that is what the marshalling here is for).
func RunDev(cfg DevConfig) *DevSession {
	if cfg.App == nil {
		panic("script: RunDev needs DevConfig.App")
	}
	if cfg.Host == nil {
		panic("script: RunDev needs DevConfig.Host")
	}
	if cfg.StateFile == "" {
		panic("script: RunDev needs DevConfig.StateFile (the " + DevEnv + " value)")
	}
	if cfg.Input == nil {
		cfg.Input = os.Stdin
	}
	if cfg.Logf == nil {
		cfg.Logf = func(format string, args ...any) {
			fmt.Printf("dev: "+format+"\n", args...)
		}
	}
	if cfg.Exit == nil {
		window := cfg.Window
		cfg.Exit = func() {
			if window == nil {
				cfg.Logf("no window and no DevConfig.Exit: the app keeps running after the save")
				return
			}
			window.Close()
		}
	}
	s := &DevSession{cfg: cfg, done: make(chan struct{})}
	go s.loop()
	return s
}

// DevSession is the running command-stream half of the dev protocol.
type DevSession struct {
	cfg  DevConfig
	done chan struct{}
}

// Done closes when the session's command stream ended (after a save,
// or at EOF) and its last command has been posted. It does not wait
// for the posted command to run.
func (s *DevSession) Done() <-chan struct{} { return s.done }

// loop reads the command lines until the stream ends. Commands are
// posted to the foreground thread one at a time; a post after the app
// began shutting down is dropped (the scheduler's shutdown guard), so a
// save that races the app's own exit is harmlessly lost.
func (s *DevSession) loop() {
	defer close(s.done)
	scanner := bufio.NewScanner(s.cfg.Input)
	for scanner.Scan() {
		switch line := strings.TrimSpace(scanner.Text()); line {
		case "":
			// A blank line (a terminal echo, a stray newline) is not a
			// command.
		case "reload":
			s.reload()
		case "save":
			s.save()
			return
		default:
			s.cfg.Logf("unknown command %q (the protocol has reload and save)", line)
		}
	}
	s.cfg.Logf("command stream ended")
}

// reload runs the reload command on the foreground thread.
func (s *DevSession) reload() {
	s.post(func() {
		if err := s.cfg.Host.Reload(); err != nil {
			// The last good tree keeps running (Load's contract); the
			// banner and the error handler already have the error.
			s.cfg.Logf("reload error: %v", err)
			return
		}
		s.cfg.Logf("reloaded")
		if s.cfg.OnReload != nil {
			s.cfg.OnReload()
		}
	})
}

// save runs the save command on the foreground thread: snapshot the
// host and the store, read the window's bounds, write the state file
// atomically, then exit.
func (s *DevSession) save() {
	s.post(func() {
		state := DevState{State: s.cfg.Host.StateSnapshot()}
		if s.cfg.Shared != nil {
			state.Shared = s.cfg.Shared.Snapshot()
		}
		if s.cfg.Window != nil {
			if bounds, err := s.cfg.Window.Bounds(); err == nil {
				state.Window = devBoundsOf(bounds)
			} else {
				s.cfg.Logf("window bounds unavailable: %v", err)
			}
		}
		if err := state.Save(s.cfg.StateFile); err != nil {
			s.cfg.Logf("save failed: %v", err)
		} else {
			s.cfg.Logf("saved %s", s.cfg.StateFile)
		}
		s.cfg.Exit()
	})
}

// post runs fn on the application's foreground thread (the thread that
// renders), like the scheduler's Post: a spawn after the app began
// shutting down is dropped rather than fatal.
func (s *DevSession) post(fn func()) {
	postOnForeground(s.cfg.App, fn)
}
