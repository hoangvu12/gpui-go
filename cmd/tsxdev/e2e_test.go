//go:build windows

package main

// The dev command, end to end (ticket 66): a real supervisor process
// running a real app — examples/tsx-hello in dev shape, copied to a
// temp module so the edits the test makes never touch the repository —
// with a real .tsx edit (an in-place reload, same process) and a real
// .go edit (a rebuild, a graceful restart, state restored).
//
// Headless throughout: the supervisor runs with script.DevHeadlessEnv
// set, which the dev app honours by opening its window far off the
// desktop and never activating it (the child is not under `go test`,
// so gpui's automatic off-screen placement does not apply to it — the
// app places itself). Nothing is ever shown on the user's screen, and
// no real input is used: the reload and the restart are observed from
// what the app prints, which is what the supervisor passes through.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui/script"
)

// The seeded state: a click count of 5 and an off-screen window
// position, both of which have to come back after a restart.
var (
	seedClicks = 5.0
	seedBounds = script.DevBounds{X: 20240, Y: 20180, Width: 400, Height: 260}
)

// the move the headless dev app makes (dev.go: the user's drag, which
// the restart must restore — larger than the comparison tolerance, so
// "restored the moved position" can never pass by rounding luck).
const (
	dragDX = 17.0
	dragDY = 13.0
)

// boundsTolerance absorbs logical→device→logical rounding on a scaled
// monitor (a DPI scale of 1.5 loses up to a third of a pixel each way).
const boundsTolerance = 2.0

func TestEndToEndDevCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the supervisor and the app, and runs a real dev session")
	}
	started := time.Now()

	// The supervisor binary: the real command, built once.
	supBuildStart := time.Now()
	supBin := filepath.Join(t.TempDir(), "tsxdev.exe")
	if out, err := exec.Command("go", "build", "-o", supBin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build the supervisor: %v\n%s", err, out)
	}
	supBuild := time.Since(supBuildStart)

	// The app: examples/tsx-hello copied into a temp module beside the
	// repository (a replace directive points at it), so the test's
	// edits — a .tsx save and a .go save — land in the copy.
	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	appDir := copyExampleApp(t, repo)

	// The restart handoff the app restores from: keyed state and a
	// window position, seeded as if a previous dev run had left them.
	statePath := filepath.Join(t.TempDir(), "devstate.json")
	if err := (script.DevState{
		State:  map[string]any{"clicks": seedClicks},
		Window: &seedBounds,
	}).Save(statePath); err != nil {
		t.Fatalf("seed the state file: %v", err)
	}

	// The supervisor: fast poll and debounce (the defaults are 200ms
	// and 250ms; the measured latencies below carry these flags).
	cmd := exec.Command(supBin,
		"-state", statePath,
		"-poll", "100ms",
		"-debounce", "250ms",
		"-grace", "8s",
		appDir)
	cmd.Env = append(os.Environ(), script.DevHeadlessEnv+"=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	log := &lineLog{}
	go log.read(stdout.(*os.File))
	go log.read(stderr.(*os.File))

	// Every child pid the app prints, for the cleanup's last resort.
	var pids []int
	t.Cleanup(func() {
		// The graceful path is the test's own last step; this guard is
		// for failures: close the supervisor's stdin (EOF stops it the
		// graceful way), and only if it hangs, kill the exact processes
		// involved — the supervisor this test started and, as a last
		// resort, the child it started, by the pid the app printed.
		// Nothing else is ever touched.
		stdin.Close()
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
			return
		case <-time.After(30 * time.Second):
		}
		_ = cmd.Process.Kill()
		for _, pid := range pids {
			if proc, err := os.FindProcess(pid); err == nil {
				_ = proc.Kill()
			}
		}
		<-done
	})

	if err := cmd.Start(); err != nil {
		t.Fatalf("start the supervisor: %v", err)
	}

	// The first app: the seed's window position (what the window reads
	// back, which is what a save captures), then its move, then the
	// started line with the seed's state visible.
	restoredA := log.expectBounds(t, "dev: window restored")
	// What a set reads back is not always exactly what was set — the
	// platform carries a client/frame offset (reference-inherited:
	// windowRectFromClient splits the frame symmetrically). Calibrate
	// it from this restore and reuse it for the restart's assertion,
	// so the test tolerates the offset without magic numbers: a
	// missed restore (the app's own default position) is still far
	// outside these bounds.
	var frameOffset [4]float64
	want := [4]float64{float64(seedBounds.X), float64(seedBounds.Y), float64(seedBounds.Width), float64(seedBounds.Height)}
	for i := range frameOffset {
		frameOffset[i] = restoredA[i] - want[i]
		if frameOffset[i] > 48 || frameOffset[i] < -48 {
			t.Fatalf("the seeded position did not restore: %v (want %v; the frame offset is small)", restoredA, want)
		}
	}
	movedA := log.expectBounds(t, "dev: window moved")
	// The move is the drag a user would make — it must actually show
	// in the window's position, beyond the rounding tolerance.
	if dx, dy := movedA[0]-restoredA[0], movedA[1]-restoredA[1]; dx < dragDX-boundsTolerance || dy < dragDY-boundsTolerance {
		t.Fatalf("the dev app did not move its window: %v -> %v", restoredA, movedA)
	}
	startLine := log.expectNext(t, "dev: started", 90*time.Second)
	pidA, labelA := parseStarted(t, startLine)
	pids = append(pids, pidA)
	if labelA != fmt.Sprintf("Hello TSX! %d", int(seedClicks)) {
		t.Fatalf("the first app's label = %q, want the seeded count %v", labelA, seedClicks)
	}

	// A slow machine gets out rather than taking the whole budget: what
	// is left is the restart — one more app build and start, roughly
	// the first phase minus the supervisor's own build — plus the
	// graceful stop. The state restore and first start above already
	// passed; the estimate says why the rest is skipped.
	firstPhase := time.Since(started)
	if estimated := firstPhase + (firstPhase - supBuild) + 5*time.Second; estimated > 60*time.Second {
		t.Skipf("the first phase (supervisor build %v + app build and start %v) took %v on this loaded machine; the restart would repeat the app build, putting the whole session at ~%v, over the ~60s end-to-end budget", supBuild, firstPhase-supBuild, firstPhase, estimated)
	}

	// A .tsx save: an in-place reload — the same process, the fresh
	// sources, the keyed state kept.
	editTsx(t, appDir)
	t.Logf("edit-to-reload: %s", log.eventually(t, "tsxdev: reload sent", 30*time.Second))
	reload := log.expectNext(t, "dev: label=", 30*time.Second)
	if !strings.Contains(reload, fmt.Sprintf("pid=%d", pidA)) {
		t.Fatalf("the .tsx edit restarted the app instead of reloading in place: %q (pid %d)", reload, pidA)
	}
	if label := parseField(reload, "label="); label != fmt.Sprintf("Hello DEV! %d", int(seedClicks)) {
		t.Fatalf("the reloaded label = %q, want the edited text with the kept count", label)
	}

	// A .go save: a rebuild, a graceful save-and-exit, a restart — and
	// the state comes back: the same keyed count, the position the app
	// was moved to, and the key the edited .tsx added (which only the
	// save could have written: the seed did not have it).
	editGo(t, appDir)
	restoredB := log.expectBounds(t, "dev: window restored")
	// The replacement restores what the first app saved: the moved
	// position, one frame offset above it (the same offset every set
	// carries, calibrated above).
	assertBoundsNear(t, "the moved position", restoredB, [4]float64{
		movedA[0] + frameOffset[0], movedA[1] + frameOffset[1], movedA[2] + frameOffset[2], movedA[3] + frameOffset[3]})
	startLine = log.expectNext(t, "dev: started", 90*time.Second)
	pidB, labelB := parseStarted(t, startLine)
	pids = append(pids, pidB)
	if pidB == pidA {
		t.Fatal("the .go edit did not restart the app: the same pid came back")
	}
	if labelB != fmt.Sprintf("Hello DEV! %d", int(seedClicks)) {
		t.Fatalf("the restarted app's label = %q, want the kept count over the edited text", labelB)
	}
	// The restart's own timing line may land before or after the
	// replacement's startup lines (the child starts concurrently).
	t.Logf("edit-to-restart: %s", log.eventually(t, "tsxdev: rebuilt", 30*time.Second))

	// The handoff file the first app wrote on its way out: exactly the
	// position the window read after its move (the save reads the
	// window, so no offset sits in between).
	state, err := script.ReadDevState(statePath)
	if err != nil {
		t.Fatalf("read the handoff: %v", err)
	}
	if state.State["clicks"] != seedClicks {
		t.Fatalf("the handoff's keyed state = %+v, want clicks=%v", state.State, seedClicks)
	}
	if state.State["devEdit"] != 1.0 {
		t.Fatalf("the handoff's keyed state = %+v, want devEdit=1 (the key the reloaded .tsx added)", state.State)
	}
	if state.Window == nil {
		t.Fatal("the handoff saved no window position")
	}
	assertBoundsNear(t, "the saved position", [4]float64{
		float64(state.Window.X), float64(state.Window.Y),
		float64(state.Window.Width), float64(state.Window.Height)},
		movedA)

	// The graceful stop: a closed stdin (the supervisor's other stop
	// channel) asks the app to save and exit; the supervisor exits 0.
	stdin.Close()
	log.expectNext(t, "tsxdev: stopping", 15*time.Second)
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("the supervisor exited with %v, want 0", err)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("the supervisor did not exit after its stdin closed")
	}
	t.Logf("the whole dev session ran in %v", time.Since(started).Round(time.Second))
}

// repoModule is the repository's module path, renamed in the temp
// module's go.mod.
const repoModule = "github.com/hoangvu12/gpui-go"

// copyExampleApp copies examples/tsx-hello into a temp module that
// builds against the repository through a replace directive, and
// returns the module's directory.
func copyExampleApp(t *testing.T, repo string) string {
	t.Helper()
	appDir := t.TempDir()
	src := filepath.Join(repo, "examples", "tsx-hello")
	for _, name := range []string{
		"main.go", "dev.go", "dev_stub.go",
		"ui.tsxbundle.go", "ui.tsxbundle.js",
		filepath.Join("ui", "app.tsx"),
	} {
		data, err := os.ReadFile(filepath.Join(src, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		dst := filepath.Join(appDir, name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// The module: the repository's own go.mod with the module renamed
	// and a require+replace pointing back at it — same Go version, same
	// dependency graph (the require blocks are what a standalone module
	// needs; a bare `require gpui-go` alone would not build), and the
	// repository's go.sum covers every module the app links.
	goModSrc, err := os.ReadFile(filepath.Join(repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	goMod := strings.Replace(string(goModSrc), "module "+repoModule, "module tsxdev-e2e-app", 1)
	goMod += "\nrequire " + repoModule + " v0.0.0\n\nreplace " + repoModule + " => " + filepath.ToSlash(repo) + "\n"
	goSum, err := os.ReadFile(filepath.Join(repo, "go.sum"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "go.sum"), goSum, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "go.mod"), []byte(goMod), 0o644); err != nil {
		t.Fatal(err)
	}
	return appDir
}

// editTsx makes the save a developer makes: the label's text changes
// (what the reload must show) and a new keyed state appears (what the
// next save must write, proving the handoff captured post-reload
// state, not the seed).
func editTsx(t *testing.T, appDir string) {
	t.Helper()
	path := filepath.Join(appDir, "ui", "app.tsx")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if !strings.Contains(text, "Hello TSX!") || !strings.Contains(text, `const clicks = state("clicks", 0);`) {
		t.Fatalf("ui/app.tsx drifted from what this test edits:\n%s", text)
	}
	text = strings.Replace(text, "Hello TSX!", "Hello DEV!", 1)
	text = strings.Replace(text, `const clicks = state("clicks", 0);`,
		`const clicks = state("clicks", 0);`+"\n\t"+`const devEdit = state("devEdit", 1);`, 1)
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// editGo makes the save that rebuilds: a comment in the app's main.
func editGo(t *testing.T, appDir string) {
	t.Helper()
	path := filepath.Join(appDir, "main.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	src = append(src, []byte("\n// a dev edit: the supervisor must rebuild and restart\n")...)
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
}

// lineLog is the dev session's transcript: every line the supervisor
// and the app print, in arrival order, with a cursor the ordered
// expectations advance (each line is logged, so a failure shows what
// the session said).
type lineLog struct {
	mu   sync.Mutex
	all  []string
	next int
}

// read drains one of the supervisor's output streams into the log.
func (l *lineLog) read(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		l.mu.Lock()
		l.all = append(l.all, scanner.Text())
		l.mu.Unlock()
	}
}

// expectNext returns the next line (from the cursor) containing
// substr, logging every line it passes.
func (l *lineLog) expectNext(t *testing.T, substr string, timeout time.Duration) string {
	t.Helper()
	deadline := time.After(timeout)
	for {
		l.mu.Lock()
		if l.next < len(l.all) {
			line := l.all[l.next]
			l.next++
			l.mu.Unlock()
			t.Logf("| %s", line)
			if strings.Contains(line, substr) {
				return line
			}
			continue
		}
		l.mu.Unlock()
		select {
		case <-deadline:
			t.Fatalf("timed out after %v waiting for %q", timeout, substr)
		case <-time.After(10 * time.Millisecond):
			// More lines may still be arriving.
		}
	}
}

// eventually returns any line containing substr, wherever it sits in
// the transcript (for lines that may land before or after the ones
// the ordered expectations wait on).
func (l *lineLog) eventually(t *testing.T, substr string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		l.mu.Lock()
		for _, line := range l.all {
			if strings.Contains(line, substr) {
				l.mu.Unlock()
				return line
			}
		}
		l.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %q anywhere in the transcript", timeout, substr)
	return ""
}

// expectBounds waits for a window-bounds line and parses it.
func (l *lineLog) expectBounds(t *testing.T, substr string) [4]float64 {
	t.Helper()
	line := l.expectNext(t, substr, 90*time.Second)
	var kind string
	var x, y, w, h float64
	if n, _ := fmt.Sscanf(line, "dev: window %s x=%f y=%f w=%f h=%f", &kind, &x, &y, &w, &h); n != 5 {
		t.Fatalf("could not parse the bounds line %q", line)
	}
	return [4]float64{x, y, w, h}
}

// assertBoundsNear compares a parsed bounds against the expected one
// with the rounding tolerance.
func assertBoundsNear(t *testing.T, what string, got, want [4]float64) {
	t.Helper()
	for i, label := range []string{"x", "y", "width", "height"} {
		if diff := got[i] - want[i]; diff > boundsTolerance || diff < -boundsTolerance {
			t.Fatalf("%s: %s = %v, want %v (±%v)", what, label, got[i], want[i], boundsTolerance)
		}
	}
}

// parseStarted pulls the pid and label out of a started line.
func parseStarted(t *testing.T, line string) (int, string) {
	t.Helper()
	pid, err := strconv.Atoi(parseField(line, "pid="))
	if err != nil {
		t.Fatalf("parse the pid from %q: %v", line, err)
	}
	return pid, parseField(line, "label=")
}

// parseField returns the value of "key=" up to the next space — or,
// when the value is quoted, up to the closing quote (labels contain
// spaces).
func parseField(line, key string) string {
	at := strings.Index(line, key)
	if at < 0 {
		return ""
	}
	rest := line[at+len(key):]
	if strings.HasPrefix(rest, `"`) {
		if end := strings.Index(rest[1:], `"`); end >= 0 {
			return rest[1 : 1+end]
		}
		return strings.Trim(rest, `"`)
	}
	if end := strings.IndexByte(rest, ' '); end >= 0 {
		rest = rest[:end]
	}
	return rest
}
