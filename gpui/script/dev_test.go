//go:build windows

package script_test

// The dev command's child-side hooks (ticket 66): the state handoff
// (DevState save/read, keyed state and shared values surviving a
// simulated restart, window bounds as data) and the command stream
// (RunDev: reload commands reload in place, the save command writes the
// state file and exits). Everything runs headless: a test window (no
// HWND, nothing shown) and an injected command stream — exactly the
// lines cmd/tsxdev writes to the child's stdin.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/gpui/script"
	"github.com/hoangvu12/gpui-go/gpui/script/compile"
)

// devView is the dev test view: a clickable box with a bound counter
// (keyed state), a bound note (shared state) and a plain text the
// source swaps between reloads — the three things a restart must carry
// and one a reload must pick up.
const devView = `
export function App() {
	const clicks = state("clicks", 0);
	const note = shared("note", "none");
	return (
		<div
			id="box"
			style={{ width: 160, height: 80, padding: 8, background: "#e2e8f0" }}
			onClick={() => clicks.set(clicks.get() + 1)}
		>
			<text id="label">{bind(clicks, (n: number) => ` + "`Clicked ${n}`" + `)}</text>
			<text id="note">{bind(note, (v: string) => ` + "`note ${v}`" + `)}</text>
			<text id="tag">{TAG}</text>
		</div>
	);
}
`

// devViewTag returns the view with the given tag text.
func devViewTag(tag string) string {
	return "const TAG = " + quote(tag) + ";\n" + devView
}

func quote(s string) string { return `"` + s + `"` }

// editSource is a dev Source whose content the test swaps between
// reloads (the edit a developer's save makes).
type editSource struct {
	mu  sync.Mutex
	src string
}

func (s *editSource) Compile() (script.Bundle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return compile.String("view.tsx", s.src)
}

func (s *editSource) set(src string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.src = src
}

// TestDevStateFileRoundTrip: the handoff file carries keyed state,
// shared values and window bounds, survives save→read, and treats a
// missing or corrupt file as a fresh start.
func TestDevStateFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	// A fresh start: no path, and no file yet.
	for _, empty := range []string{"", path} {
		state, err := script.ReadDevState(empty)
		if err != nil {
			t.Fatalf("ReadDevState(%q): %v", empty, err)
		}
		if state.State != nil || state.Shared != nil || state.Window != nil {
			t.Fatalf("ReadDevState(%q) of a fresh start = %+v", empty, state)
		}
		if _, ok := state.WindowBounds(); ok {
			t.Fatalf("a fresh start restored window bounds: %+v", state)
		}
	}

	full := script.DevState{
		State:  map[string]any{"clicks": float64(1), "name": "dev"},
		Shared: map[string]any{"note": "hello"},
		Window: &script.DevBounds{X: 160.5, Y: 120, Width: 480, Height: 320},
	}
	if err := full.Save(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	back, err := script.ReadDevState(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(back.State) != 2 || back.State["clicks"] != float64(1) || back.State["name"] != "dev" {
		t.Fatalf("state round trip: %+v", back.State)
	}
	if len(back.Shared) != 1 || back.Shared["note"] != "hello" {
		t.Fatalf("shared round trip: %+v", back.Shared)
	}
	bounds, ok := back.WindowBounds()
	if !ok || bounds.Origin.X != 160.5 || bounds.Origin.Y != 120 ||
		bounds.Size.Width != 480 || bounds.Size.Height != 320 {
		t.Fatalf("bounds round trip: %+v (ok=%v)", bounds, ok)
	}

	// A corrupt file is an error naming the file, not a crash; the app
	// starts fresh.
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	state, err := script.ReadDevState(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("a corrupt file must error naming the file, got %v", err)
	}
	if state.State != nil || state.Window != nil {
		t.Fatalf("a corrupt file must read as a fresh start, got %+v", state)
	}

	// A saved window with no size restores nothing (ok=false keeps the
	// app's default placement).
	if _, ok := (script.DevState{Window: &script.DevBounds{X: 1, Y: 2}}).WindowBounds(); ok {
		t.Fatal("a bounds with no size restored a window position")
	}
}

// TestKeyedStateAndSharedSurviveASimulatedRestart: the exact handoff a
// dev restart performs — snapshot the host's keyed state and the store,
// write them to the state file, read them back into a fresh host and
// store, reload — and the values the scripts see are the ones the
// previous run left (init matters only for keys that did not exist).
func TestKeyedStateAndSharedSurviveASimulatedRestart(t *testing.T) {
	source := &editSource{src: devViewTag("v1")}
	store := script.NewShared()
	host := script.New(script.WithSource(source), script.WithShared(store))
	if err := host.Reload(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}

	// The run that gets restarted: one click (keyed state), one Go-side
	// shared write, and a binding recompute each.
	if err := host.Click("box"); err != nil {
		t.Fatalf("click: %v", err)
	}
	if err := store.Write("note", "hi"); err != nil {
		t.Fatalf("shared write: %v", err)
	}
	if got, _ := host.Text("label"); got != "Clicked 1" {
		t.Fatalf("label after the click = %q", got)
	}
	if got, _ := host.Text("note"); got != "note hi" {
		t.Fatalf("note after the shared write = %q", got)
	}

	// The save half of the restart handoff.
	path := filepath.Join(t.TempDir(), "state.json")
	saved := script.DevState{State: host.StateSnapshot(), Shared: store.Snapshot()}
	if err := saved.Save(path); err != nil {
		t.Fatalf("save the handoff: %v", err)
	}

	// The restore half: a fresh host and store, seeded from the file,
	// loading the same sources.
	restored, err := script.ReadDevState(path)
	if err != nil {
		t.Fatalf("read the handoff: %v", err)
	}
	store2 := script.NewShared()
	store2.Restore(restored.Shared)
	host2 := script.New(
		script.WithSource(source),
		script.WithShared(store2),
		script.WithStateSnapshot(restored.State),
	)
	if err := host2.Reload(); err != nil {
		t.Fatalf("the replacement's reload: %v", err)
	}

	// The keyed state and shared values are the previous run's, not the
	// script's init: the bindings on the fresh host compute from them.
	if got, _ := host2.Text("label"); got != "Clicked 1" {
		t.Fatalf("label after the restart = %q, want the saved count", got)
	}
	if got, _ := host2.Text("note"); got != "note hi" {
		t.Fatalf("note after the restart = %q, want the saved value", got)
	}
	if v, _ := store2.Read("note"); v != "hi" {
		t.Fatalf("store after the restore = %v", v)
	}

	// The snapshot is a copy: later writes on one host do not leak into
	// another's snapshot.
	if err := host.Click("box"); err != nil {
		t.Fatal(err)
	}
	if host.Get("clicks") == host2.Get("clicks") {
		t.Fatalf("the hosts share state: %v == %v", host.Get("clicks"), host2.Get("clicks"))
	}
}

// TestRunDevReloadsAndSaves: the command stream — the supervisor's
// lines through an injected reader, the host mounted in a headless test
// window, commands marshalled onto the thread that renders (the test
// app's dispatcher). A reload picks up new sources in place with keyed
// state kept (and a failed one keeps the last good tree); the save
// writes the state file and exits.
func TestRunDevReloadsAndSaves(t *testing.T) {
	source := &editSource{src: devViewTag("v1")}
	store := script.NewShared()
	host := script.New(
		script.WithSource(source),
		script.WithShared(store),
		script.WithoutBanner(),
	)
	if err := host.Reload(); err != nil {
		t.Fatalf("initial reload: %v", err)
	}
	sw := openScriptWindow(t, host)
	if err := host.Click("box"); err != nil {
		t.Fatalf("click: %v", err)
	}
	if got, _ := host.Text("label"); got != "Clicked 1" {
		t.Fatalf("label after the click = %q", got)
	}

	// The command stream: the supervisor's stdin pipe, as a pipe.
	pipeReader, pipeWriter, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pipeWriter.Close(); pipeReader.Close() })

	statePath := filepath.Join(t.TempDir(), "state.json")
	reloaded := make(chan int, 8)
	exited := make(chan struct{}, 1)
	var logs []string
	var mu sync.Mutex
	logf := func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		logs = append(logs, "dev: "+strings.TrimSpace(fmt.Sprintf(format, args...)))
	}
	session := script.RunDev(script.DevConfig{
		App:       sw.ta.App(),
		Host:      host,
		Shared:    store,
		StateFile: statePath,
		Input:     pipeReader,
		Logf:      logf,
		Exit:      func() { close(exited) },
		OnReload:  func() { reloaded <- host.DescribeCount() },
	})

	// An unknown command is logged and ignored; a reload follows it
	// fine (the protocol can grow without breaking old apps).
	writeLine(t, pipeWriter, "bogus")
	writeLine(t, pipeWriter, "reload")
	pumpUntil(t, sw.ta, 5*time.Second, func() bool { return len(reloaded) > 0 })
	if got, _ := host.Text("tag"); got != "v1" {
		t.Fatalf("tag after the reload = %q, want v1", got)
	}
	if got, _ := host.Text("label"); got != "Clicked 1" {
		t.Fatalf("keyed state did not survive the reload: %q", got)
	}
	mu.Lock()
	unknownLogged := strings.Contains(strings.Join(logs, "\n"), "unknown command")
	mu.Unlock()
	if !unknownLogged {
		t.Fatalf("the unknown command was not logged: %v", logs)
	}

	// A source edit lands on the next reload, in place, state kept.
	source.set(devViewTag("v2"))
	writeLine(t, pipeWriter, "reload")
	pumpUntil(t, sw.ta, 5*time.Second, func() bool {
		tag, _ := host.Text("tag")
		return tag == "v2"
	})
	if got, _ := host.Text("label"); got != "Clicked 1" {
		t.Fatalf("keyed state did not survive the second reload: %q", got)
	}

	// A broken source: the reload fails, the session logs it, and the
	// last good tree keeps showing.
	source.set(devViewTag("v3") + "\nexport function broken(")
	writeLine(t, pipeWriter, "reload")
	pumpUntil(t, sw.ta, 5*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return strings.Contains(strings.Join(logs, "\n"), "reload error")
	})
	if got, _ := host.Text("tag"); got != "v2" {
		t.Fatalf("a failed reload replaced the last good tree: %q", got)
	}

	// The save: keyed state, the store's values, and the window's
	// bounds (a headless test window has none — that is the hostless
	// path) land in the state file, then Exit runs.
	store.Write("note", "hi")
	writeLine(t, pipeWriter, "save")
	pumpUntil(t, sw.ta, 5*time.Second, func() bool {
		select {
		case <-exited:
			return true
		default:
			return false
		}
	})
	state, err := script.ReadDevState(statePath)
	if err != nil {
		t.Fatalf("read the saved state: %v", err)
	}
	if state.State["clicks"] != float64(1) {
		t.Fatalf("saved keyed state = %+v, want clicks=1", state.State)
	}
	if state.Shared["note"] != "hi" {
		t.Fatalf("saved shared state = %+v, want note=hi", state.Shared)
	}
	if state.Window != nil {
		t.Fatalf("a hostless window saved bounds: %+v", state.Window)
	}
	select {
	case <-session.Done():
	default:
		t.Fatal("the session did not end after the save")
	}
}

// TestRunDevEndsAtEOF: the session ends when the command stream does
// (the supervisor is gone), without running anything.
func TestRunDevEndsAtEOF(t *testing.T) {
	source := &editSource{src: devViewTag("v1")}
	host := script.New(script.WithSource(source), script.WithShared(script.NewShared()))
	if err := host.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	ta := gpui.NewTestApp()
	t.Cleanup(func() { ta.App().Shutdown() })

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writer.Close(); reader.Close() })
	session := script.RunDev(script.DevConfig{
		App:       ta.App(),
		Host:      host,
		StateFile: filepath.Join(t.TempDir(), "state.json"),
		Input:     reader,
	})
	writer.Close() // EOF: nothing more will arrive
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the session did not end at EOF")
	}
	if host.DescribeCount() != 1 {
		t.Fatalf("EOF ran something: describe count %d", host.DescribeCount())
	}
}

// writeLine writes one command line, the supervisor's wire form.
func writeLine(t *testing.T, w *os.File, line string) {
	t.Helper()
	if _, err := w.WriteString(line + "\n"); err != nil {
		t.Fatalf("write %q: %v", line, err)
	}
}

// pumpUntil drains the test app until cond holds, so a command posted
// by the session (a foreground spawn) has run. Commands are read by
// the session's own goroutine, so the pump also yields to it.
func pumpUntil(t *testing.T, ta *gpui.TestApp, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ta.RunUntilParked()
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out after %v", timeout)
}
