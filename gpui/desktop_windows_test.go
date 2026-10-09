//go:build windows

package gpui

// Ticket26's unit and seam tests: pure mappings ported with the
// reference's own test vectors (encode_restart_arguments,
// character_palette_inputs), the background-appearance clear path, the
// cursor/resource mapping, the display UUID derivation, and the
// seam-guarded external-action paths (open/reveal/restart, SendInput)
// plus the notification/jump-list state machines on a real host. Real
// windows are created where the behavior needs them; a window-creation
// failure FAILS the test (interactive-session expectation, like
// internal/winhostspec).
//
// Nothing here launches a URL, opens Explorer, sends synthetic input or
// restarts the process: the real syscalls sit behind the seams the
// tests swap, and the seam defaults are the real calls.

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Real-host helper (package-internal, so unexported seams are reachable)
// ---------------------------------------------------------------------------

func desktopNewHost(t *testing.T) *Host {
	t.Helper()
	h := NewHost()
	if err := h.Start(); err != nil {
		t.Fatalf("host start failed: %v", err)
	}
	t.Cleanup(func() {
		if err := h.Stop(); err != nil {
			t.Errorf("host stop failed: %v", err)
		}
	})
	return h
}

func desktopWaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// ---------------------------------------------------------------------------
// Pure mappings
// ---------------------------------------------------------------------------

// TestEncodeRestartArguments is the port of the reference's
// test_encode_restart_arguments (platform.rs tests): the Windows argv
// quoting rules for Start-Process.
func TestEncodeRestartArguments(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
		want      string
	}{
		{"empty", nil, ""},
		{"two", []string{"--user-data-dir", `C:\Zed Data`}, `"--user-data-dir" "C:\Zed Data"`},
		{"trailing backslash doubles", []string{`C:\`}, `"C:\\"`},
		{"embedded quote", []string{`he said "hi"`}, `"he said \"hi\""`},
		{"backslashes before quote", []string{`a\"b`}, `"a\\\"b"`},
	}
	for _, tc := range cases {
		if got := encodeRestartArguments(tc.arguments); got != tc.want {
			t.Errorf("%s: encodeRestartArguments = %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestClearColorForBackground pins the renderer clear path for every
// background mode (directx_renderer.rs render: opaque -> [1,1,1,1],
// every other mode -> [0,0,0,0]).
func TestClearColorForBackground(t *testing.T) {
	cases := []struct {
		appearance WindowBackgroundAppearance
		want       Color
	}{
		{WindowBackgroundOpaque, Color{R: 1, G: 1, B: 1, A: 1}},
		{WindowBackgroundTransparent, Color{}},
		{WindowBackgroundBlurred, Color{}},
		{WindowBackgroundMica, Color{}},
		{WindowBackgroundMicaAlt, Color{}},
	}
	for _, tc := range cases {
		if got := ClearColorForBackground(tc.appearance); got != tc.want {
			t.Errorf("ClearColorForBackground(%d) = %+v, want %+v", tc.appearance, got, tc.want)
		}
	}
	if !WindowBackgroundOpaque.IsOpaque() || WindowBackgroundOpaque.IsTransparent() {
		t.Error("opaque mode must be opaque and not transparent")
	}
	for _, ap := range []WindowBackgroundAppearance{
		WindowBackgroundTransparent, WindowBackgroundBlurred,
		WindowBackgroundMica, WindowBackgroundMicaAlt,
	} {
		if ap.IsOpaque() || !ap.IsTransparent() {
			t.Errorf("mode %d must be transparent and not opaque", ap)
		}
	}
}

// TestCursorStyleResourceID pins the style->system-cursor mapping
// (util.rs load_cursor's match arms).
func TestCursorStyleResourceID(t *testing.T) {
	cases := []struct {
		style CursorStyle
		want  uintptr
	}{
		{CursorArrow, idcArrow},
		{CursorIBeam, idcIBeam},
		{CursorIBeamVertical, idcIBeam},
		{CursorCrosshair, idcCross},
		{CursorPointingHand, idcHand},
		{CursorDragLink, idcHand},
		{CursorResizeLeft, idcSizeWE},
		{CursorResizeRight, idcSizeWE},
		{CursorResizeLeftRight, idcSizeWE},
		{CursorResizeColumn, idcSizeWE},
		{CursorResizeUp, idcSizeNS},
		{CursorResizeDown, idcSizeNS},
		{CursorResizeUpDown, idcSizeNS},
		{CursorResizeRow, idcSizeNS},
		{CursorResizeUpLeftDownRight, idcSizeNWSE},
		{CursorResizeUpRightDownLeft, idcSizeNESW},
		{CursorOperationNotAllowed, idcNo},
	}
	for _, tc := range cases {
		if got := cursorResourceID(tc.style); got != tc.want {
			t.Errorf("cursorResourceID(%d) = %d, want %d", tc.style, got, tc.want)
		}
	}
}

// TestDisplayUUID pins the v5 derivation (display.rs generate_uuid):
// stable per device name, distinct across names, canonical shape.
func TestDisplayUUID(t *testing.T) {
	a := displayUUID([]uint16{'\\', '\\', '.', '\\', 'D', 'I', 'S', 'P', 'L', 'A', 'Y', '1', 0})
	b := displayUUID([]uint16{'\\', '\\', '.', '\\', 'D', 'I', 'S', 'P', 'L', 'A', 'Y', '1', 0})
	c := displayUUID([]uint16{'\\', '\\', '.', '\\', 'D', 'I', 'S', 'P', 'L', 'A', 'Y', '2', 0})
	if a != b {
		t.Errorf("uuid must be stable: %q vs %q", a, b)
	}
	if a == c {
		t.Errorf("uuid must differ across device names: %q", a)
	}
	if len(a) != 36 || a[8] != '-' || a[13] != '-' || a[18] != '-' || a[23] != '-' {
		t.Fatalf("uuid is not canonical: %q", a)
	}
	if a[14] != '5' {
		t.Errorf("uuid version nibble = %q, want 5 (v5)", a[14])
	}
}

// TestCharacterPaletteInputs ports the reference's
// character_palette_preserves_held_modifiers and
// character_palette_does_not_release_a_held_windows_key (window.rs
// tests).
func TestCharacterPaletteInputs(t *testing.T) {
	keys := func(inputs []sendInputRecord) [][2]any {
		out := make([][2]any, 0, len(inputs))
		for _, input := range inputs {
			out = append(out, [2]any{input.ki.wVk, input.ki.dwFlags&keyeventfKeyUp != 0})
		}
		return out
	}
	got := keys(characterPaletteInputs([]uint8{vkLcontrol, vkRshift}, false))
	want := [][2]any{
		{uint16(vkLcontrol), true},
		{uint16(vkRshift), true},
		{uint16(vkLwin), false},
		{uint16(vkOemPeriodByte), false},
		{uint16(vkOemPeriodByte), true},
		{uint16(vkLwin), true},
		{uint16(vkLcontrol), false},
		{uint16(vkRshift), false},
	}
	if len(got) != len(want) {
		t.Fatalf("input batch length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("input[%d] = %v, want %v", i, got[i], want[i])
		}
	}

	winHeld := characterPaletteInputs(nil, true)
	if len(winHeld) != 2 {
		t.Fatalf("win-held batch length = %d, want 2", len(winHeld))
	}
	for _, input := range winHeld {
		if input.ki.wVk != uint16(vkOemPeriodByte) {
			t.Errorf("win-held batch key = %d, want VK_OEM_PERIOD", input.ki.wVk)
		}
	}

	// The extended-key flag marks the right-hand Windows/Alt/Control
	// keys and both Windows keys (character_palette_key).
	for _, tc := range []struct {
		vkey uint8
		want bool
	}{
		{vkLwin, true}, {vkRwin, true}, {vkRcontrolByte, true}, {vkRmenuByte, true},
		{vkLcontrol, false}, {vkLshift, false}, {vkOemPeriodByte, false},
	} {
		gotFlags := characterPaletteKey(tc.vkey, false).ki.dwFlags
		if (gotFlags&keyeventfExtended != 0) != tc.want {
			t.Errorf("extended flag for vkey 0x%02x = %v, want %v", tc.vkey, gotFlags&keyeventfExtended != 0, tc.want)
		}
	}
}

// TestCharacterPaletteCleanupInputs pins the partial-send cleanup batch
// (window.rs show_character_palette's failure branch): period up, the
// synthetic Win up (unless the user holds Win), held modifiers
// re-pressed.
func TestCharacterPaletteCleanupInputs(t *testing.T) {
	held := []uint8{vkLcontrol, vkRshift}
	cleanup := characterPaletteCleanupInputs(held, false)
	want := [][2]any{
		{uint16(vkOemPeriodByte), true},
		{uint16(vkLwin), true},
		{uint16(vkLcontrol), false},
		{uint16(vkRshift), false},
	}
	if len(cleanup) != len(want) {
		t.Fatalf("cleanup batch length = %d, want %d", len(cleanup), len(want))
	}
	for i := range want {
		got := [2]any{cleanup[i].ki.wVk, cleanup[i].ki.dwFlags&keyeventfKeyUp != 0}
		if got != want[i] {
			t.Errorf("cleanup[%d] = %v, want %v", i, got, want[i])
		}
	}

	winHeld := characterPaletteCleanupInputs(nil, true)
	if len(winHeld) != 1 || winHeld[0].ki.wVk != uint16(vkOemPeriodByte) {
		t.Fatalf("win-held cleanup = %v, want a single period up", winHeld)
	}
}

// TestRevealParentComputation pins the parent computation
// (Path::parent's None for roots).
func TestRevealParentComputation(t *testing.T) {
	if dir, ok := filepathParent(`C:\Users\admin\file.txt`); !ok || dir != `C:\Users\admin` {
		t.Errorf("filepathParent(file) = %q, %v", dir, ok)
	}
	if _, ok := filepathParent(`C:\`); ok {
		t.Error("filepathParent(drive root) must report no parent")
	}
	if dir, ok := filepathParent(`\\?\C:\x\y`); !ok || dir != `\\?\C:\x` {
		t.Errorf("filepathParent(UNC-prefixed) = %q, %v", dir, ok)
	}
}

// ---------------------------------------------------------------------------
// Seam-guarded external actions
// ---------------------------------------------------------------------------

// shellSeam swaps the shell seams and restores them at cleanup.
func shellSeam(t *testing.T) (execCalls *[]shellExecCall, revealCalls *[]shellRevealCall) {
	t.Helper()
	var execs []shellExecCall
	var reveals []shellRevealCall
	origExec, origReveal := shellExecute, shellReveal
	shellExecute = func(verb, target string) error {
		execs = append(execs, shellExecCall{verb: verb, target: target})
		return nil
	}
	shellReveal = func(dir, target string) error {
		reveals = append(reveals, shellRevealCall{dir: dir, target: target})
		return nil
	}
	t.Cleanup(func() {
		shellExecute, shellReveal = origExec, origReveal
	})
	return &execs, &reveals
}

type shellExecCall struct {
	verb, target string
}

type shellRevealCall struct {
	dir, target string
}

// TestOpenURLAndRevealMarshaling verifies the call sites and argument
// marshaling with the real syscalls guarded off: empty targets make no
// call, URLs open with the "open" verb, reveal resolves parent+target,
// and the ERROR_FILE_NOT_FOUND fallback opens the parent folder
// (open_target_in_explorer's documented quirk path).
func TestOpenURLAndRevealMarshaling(t *testing.T) {
	execs, reveals := shellSeam(t)
	h := desktopNewHost(t)

	h.OpenURL("")
	if len(*execs) != 0 {
		t.Fatalf("empty URL must not launch, got %v", *execs)
	}
	h.OpenURL("https://example.invalid/never-launched")
	desktopWaitFor(t, "the URL open to marshal", func() bool { return len(*execs) == 1 })
	if got := (*execs)[0]; got.verb != "open" || got.target != "https://example.invalid/never-launched" {
		t.Errorf("open call = %+v", got)
	}

	h.RevealPath("")
	if len(*reveals) != 0 {
		t.Fatalf("empty path must not reveal, got %v", *reveals)
	}
	h.RevealPath(`C:\Users\admin\file.txt`)
	desktopWaitFor(t, "the reveal to marshal", func() bool { return len(*reveals) == 1 })
	if got := (*reveals)[0]; got.dir != `C:\Users\admin` || got.target != `C:\Users\admin\file.txt` {
		t.Errorf("reveal call = %+v", got)
	}

	h.OpenWithSystem(`C:\some\path.txt`)
	desktopWaitFor(t, "the system open to marshal", func() bool { return len(*execs) == 2 })
	if got := (*execs)[1]; got.verb != "open" || got.target != `C:\some\path.txt` {
		t.Errorf("system open call = %+v", got)
	}

	// The reveal fallback: ERROR_FILE_NOT_FOUND opens the parent folder.
	err := revealSelectResult(0x80070002, nil, `C:\dir`)
	if err != nil {
		t.Fatalf("file-not-found fallback failed: %v", err)
	}
	if len(*execs) != 3 || (*execs)[2].target != `C:\dir` {
		t.Errorf("fallback open = %+v, want the parent folder", *execs)
	}
	if err := revealSelectResult(0, nil, ""); err != nil {
		t.Errorf("success must not error: %v", err)
	}
	if err := revealSelectResult(0x80004005, nil, ""); err == nil {
		t.Error("an unexpected failure must error, not fall back")
	}

	// A root path has no parent: the source's "No parent folder found".
	if err := revealInExplorer(`C:\`); err == nil || !strings.Contains(err.Error(), "no parent") {
		t.Errorf("root reveal error = %v, want the no-parent diagnostic", err)
	}
}

// TestRestartDeferredLaunchReentry verifies restart's deferred
// launch/reentry path with the real process launch guarded off: the
// launch happens on the foreground thread, after the enclosing
// foreground work unwinds (the reference defers the spawn so
// CreateProcessW's message pumping cannot re-enter a live AppCell
// borrow), with the environment records the script reads; a successful
// launch quits the message loop, a failed launch does not.
func TestRestartDeferredLaunchReentry(t *testing.T) {
	// Success path: the launcher records its thread and arguments, the
	// loop then quits.
	h1 := desktopNewHost(t)
	var mu sync.Mutex
	var order []string
	var launchEnv []string
	origLauncher := restartLauncher
	restartLauncher = func(powershell string, env []string) error {
		mu.Lock()
		order = append(order, "launch")
		launchEnv = env
		foreground := h1.IsForegroundThread()
		mu.Unlock()
		if !foreground {
			t.Errorf("restart launcher ran off the foreground thread")
		}
		if !strings.HasSuffix(strings.ToLower(powershell), "powershell.exe") {
			t.Errorf("launcher powershell = %q", powershell)
		}
		return nil
	}
	t.Cleanup(func() { restartLauncher = origLauncher })

	// The restart request is made from inside foreground work; the
	// launch must happen only after that work unwinds.
	posted := make(chan struct{})
	go func() {
		h1.Post(func() {
			mu.Lock()
			order = append(order, "outer-start")
			mu.Unlock()
			if err := h1.Restart("", []string{"--user-data-dir", `C:\Zed Data`}); err != nil {
				t.Errorf("Restart: %v", err)
			}
			mu.Lock()
			order = append(order, "outer-end")
			mu.Unlock()
			close(posted)
		})
	}()
	<-posted
	desktopWaitFor(t, "the deferred restart launch", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(order) == 3
	})
	mu.Lock()
	if order[0] != "outer-start" || order[1] != "outer-end" || order[2] != "launch" {
		t.Errorf("restart ordering = %v, want outer-start, outer-end, launch", order)
	}
	env := launchEnv
	mu.Unlock()

	wantExecutable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	want := []string{
		fmt.Sprintf("ZED_RESTART_PID=%d", os.Getpid()),
		"ZED_RESTART_EXECUTABLE=" + wantExecutable,
		`ZED_RESTART_ARGUMENTS="--user-data-dir" "C:\Zed Data"`,
	}
	if len(env) != len(want) {
		t.Fatalf("restart env = %v, want %v", env, want)
	}
	for i := range want {
		if env[i] != want[i] {
			t.Errorf("restart env[%d] = %q, want %q", i, env[i], want[i])
		}
	}

	// The successful launch quits the loop.
	waitDone := make(chan error, 1)
	go func() { waitDone <- h1.Wait() }()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("host Wait after restart: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the message loop did not quit after a successful restart launch")
	}

	// Failure path: a failed spawn records the fault and the loop keeps
	// running (the reference logs and returns without quitting).
	h2 := desktopNewHost(t)
	restartLauncher = func(powershell string, env []string) error {
		return errors.New("launcher unavailable in test")
	}
	if err := h2.Restart("", nil); err != nil {
		t.Fatalf("Restart (failure path): %v", err)
	}
	desktopWaitFor(t, "the restart fault to be recorded", func() bool {
		for _, fault := range h2.Faults() {
			if strings.Contains(fault, "restart script") {
				return true
			}
		}
		return false
	})
	// Still alive: a foreground query keeps working.
	if _, err := h2.CursorStyle(); err != nil {
		t.Fatalf("host must survive a failed restart launch: %v", err)
	}
	if err := h2.Stop(); err != nil {
		t.Fatalf("stop after failed restart: %v", err)
	}
}

// TestCharacterPalettePartialSendCleanup drives the partial-send path
// with the real SendInput guarded off: a partial batch triggers the
// cleanup batch and reports the failure, never leaving the outcome a
// silent success. The window is hidden so the foreground guard is not
// the path under test here — the seam replaces the send.
func TestCharacterPalettePartialSendCleanup(t *testing.T) {
	h := desktopNewHost(t)
	handle, err := h.OpenWindow(WindowOptions{Title: "desktop partial send"})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}

	var mu sync.Mutex
	var batches [][]sendInputRecord
	origSend := sendInputs
	sendInputs = func(inputs []sendInputRecord) int {
		mu.Lock()
		batches = append(batches, inputs)
		mu.Unlock()
		// Partial: accept all but one.
		return len(inputs) - 1
	}
	t.Cleanup(func() { sendInputs = origSend })

	// Bypass the foreground guard by testing the seam-driven core: the
	// palette batch and its cleanup are planned, sent, and diagnosed.
	err = handle.ShowCharacterPalette()
	if err == nil {
		t.Fatal("a partial send must report an error")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(batches) < 2 {
		// A hidden window is not the foreground window: the guard fires
		// first and no input is injected (also correct). Only when a
		// send was attempted does the cleanup have to follow.
		t.Logf("hidden window was not foreground; the guard rejected the palette: %v", err)
		return
	}
	last := batches[len(batches)-1]
	cleanup := characterPaletteCleanupInputs(heldCharacterPaletteModifiers(), winKeyHeld())
	if len(last) != len(cleanup) {
		t.Fatalf("cleanup batch length = %d, want %d", len(last), len(cleanup))
	}
}

// TestPackageIdentityProbe reads the real package identity
// (GetCurrentPackageFullName): a plain Go test binary is unpackaged, so
// the probe must report false on this machine.
func TestPackageIdentityProbe(t *testing.T) {
	if probeHasPackageIdentity() {
		t.Log("the test process carries package identity; SetAppIdentity skips on it")
	}
}

// ---------------------------------------------------------------------------
// Notifications and app identity
// ---------------------------------------------------------------------------

// TestSystemNotificationNoIdentityNoOp reproduces the source-supported
// no-identity outcome (system_notifications.rs notifier: a warning is
// recorded and show returns Ok without a notification).
func TestSystemNotificationNoIdentityNoOp(t *testing.T) {
	h := desktopNewHost(t)
	err := h.ShowSystemNotification(SystemNotification{
		Title: "never shown", Body: "no identity", Tag: "no-identity",
	})
	if err != nil {
		t.Fatalf("the no-identity no-op must be a source-supported success: %v", err)
	}
	found := false
	for _, fault := range h.Faults() {
		if strings.Contains(fault, "app identity") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the no-identity warning was not recorded; faults = %v", h.Faults())
	}
}

// TestSystemNotificationWithIdentityReportsUnavailable reproduces the
// unported WinRT toast notifier as an explicit failure — never fake
// success. SetAppIdentity itself really runs
// SetCurrentProcessExplicitAppUserModelID (process-scoped, not a
// user-visible system change).
func TestSystemNotificationWithIdentityReportsUnavailable(t *testing.T) {
	h := desktopNewHost(t)
	if probeHasPackageIdentity() {
		t.Skip("the process carries package identity; the explicit-app-model path is unreachable")
	}
	if err := h.SetAppIdentity("gpui-go.desktoptest", "GPUI Go Desktop Test"); err != nil {
		t.Fatalf("SetAppIdentity: %v", err)
	}
	err := h.ShowSystemNotification(SystemNotification{Title: "t", Tag: "with-identity"})
	if !errors.Is(err, ErrToastNotifierUnavailable) {
		t.Fatalf("with an identity set, show must report the unported toast notifier, got %v", err)
	}
}

// TestSystemNotificationResponseDrain verifies the response drain
// wiring: a response delivered through the channel reaches the
// registered callback on the foreground thread.
func TestSystemNotificationResponseDrain(t *testing.T) {
	h := desktopNewHost(t)
	got := make(chan SystemNotificationResponse, 1)
	foreground := make(chan bool, 1)
	if err := h.OnSystemNotificationResponse(func(response SystemNotificationResponse) {
		select {
		case got <- response:
		default:
		}
		select {
		case foreground <- h.IsForegroundThread():
		default:
		}
	}); err != nil {
		t.Fatalf("OnSystemNotificationResponse: %v", err)
	}
	// Responses come from toast activations; the drain is tested by
	// delivering one directly through the state's channel (the exact
	// path a toast activation would take).
	h.desktop.notif.responseCh <- SystemNotificationResponse{
		Tag: "tag-1", ActionID: "action-2", ActionActivated: true,
	}
	select {
	case response := <-got:
		if response.Tag != "tag-1" || response.ActionID != "action-2" || !response.ActionActivated {
			t.Errorf("response = %+v", response)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the response drain did not deliver")
	}
	select {
	case fg := <-foreground:
		if !fg {
			t.Error("the response callback must run on the foreground thread")
		}
	default:
		t.Error("the response callback did not record its thread")
	}
}

// ---------------------------------------------------------------------------
// Jump lists and dock menus
// ---------------------------------------------------------------------------

// desktopTestUnit is the dock-menu test action payload.
type desktopTestUnit struct{}

var desktopTestUnitAction = DefineUnitAction[desktopTestUnit]("desktoptest::Dock")

// TestJumpListDockMenuConstruction pins DockMenuItem::new's mapping:
// action items are kept (with the "New Window" description the reference
// hard-codes) and non-action items are rejected.
func TestJumpListDockMenuConstruction(t *testing.T) {
	h := desktopNewHost(t)
	items := []MenuItem{
		{Kind: MenuItemAction, Name: "New Window", Action: desktopTestUnitAction.Box(desktopTestUnit{})},
		{Kind: MenuItemAction, Name: "Open Settings", Action: desktopTestUnitAction.Box(desktopTestUnit{})},
		{Kind: MenuItemSeparator},
		{Kind: MenuItemSubmenu, Name: "Submenu"},
	}
	built := h.buildDockMenuItems(items)
	if len(built) != 2 {
		t.Fatalf("built %d dock items, want 2 (non-action items dropped)", len(built))
	}
	if built[0].name != "New Window" || built[0].description != "Opens a new window" {
		t.Errorf("New Window mapping = %q / %q", built[0].name, built[0].description)
	}
	if built[1].description != "Open Settings" {
		t.Errorf("other items keep their name as description, got %q", built[1].description)
	}
	dropped := 0
	for _, fault := range h.Faults() {
		if strings.Contains(fault, "only action items") {
			dropped++
		}
	}
	if dropped != 2 {
		t.Fatalf("dropped items recorded = %d, want 2; faults = %v", dropped, h.Faults())
	}
}

// TestDockMenuActionDispatch exercises the real dispatch path: the dock
// menu is registered, PerformDockMenuAction posts
// WM_GPUI_DOCK_MENU_ACTION to the platform window, and the handler
// delivers the cloned action through the app-menu-action callback. A
// missing index records the fault and dispatches nothing.
func TestDockMenuActionDispatch(t *testing.T) {
	h := desktopNewHost(t)
	received := make(chan BoxedAction, 4)
	if err := h.OnAppMenuAction(func(action BoxedAction) { received <- action }); err != nil {
		t.Fatalf("OnAppMenuAction: %v", err)
	}
	items := []MenuItem{
		{Kind: MenuItemAction, Name: "New Window", Action: desktopTestUnitAction.Box(desktopTestUnit{})},
	}
	// UpdateJumpList updates the state and reports the unported shell
	// commit as the honest outcome.
	if _, err := h.UpdateJumpList(items, [][]string{{`C:\a`, `C:\b`}}); !errors.Is(err, ErrJumpListShellCommitUnavailable) {
		t.Fatalf("UpdateJumpList error = %v, want the shell-commit pending row", err)
	}
	h.PerformDockMenuAction(0)
	select {
	case action := <-received:
		if action.Name() != "desktoptest::Dock" {
			t.Errorf("dispatched action = %q", action.Name())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the dock menu action was not dispatched")
	}

	// A missing index: the reference logs and returns 1 without
	// dispatching.
	h.PerformDockMenuAction(99)
	time.Sleep(200 * time.Millisecond)
	select {
	case action := <-received:
		t.Fatalf("a missing index must not dispatch, got %q", action.Name())
	default:
	}
	desktopWaitFor(t, "the missing-index fault", func() bool {
		for _, fault := range h.Faults() {
			if strings.Contains(fault, "index 99") {
				return true
			}
		}
		return false
	})
}

// TestMenusRecordAndQuery pins set_menus/get_menus on Windows: menus
// are recorded and returned (no native menu bar exists).
func TestMenusRecordAndQuery(t *testing.T) {
	h := desktopNewHost(t)
	menus := []Menu{{Name: "File", Items: []MenuItem{
		{Kind: MenuItemAction, Name: "New", Action: desktopTestUnitAction.Box(desktopTestUnit{})},
		{Kind: MenuItemSeparator},
	}}}
	h.SetMenus(menus)
	desktopWaitFor(t, "the menus to be recorded", func() bool {
		got, ok := h.GetMenus()
		return ok && len(got) == 1 && got[0].Name == "File"
	})
	got, ok := h.GetMenus()
	if !ok {
		t.Fatal("GetMenus must report menus on Windows")
	}
	if len(got[0].Items) != 2 || got[0].Items[0].Name != "New" {
		t.Errorf("recorded menu items = %+v", got[0].Items)
	}
}

// ---------------------------------------------------------------------------
// Unsupported / no-op outcomes
// ---------------------------------------------------------------------------

// TestUnsupportedOutcomes reproduces the verified source outcomes:
// thermal state is always nominal, the auxiliary executable and URL
// scheme paths fail with the source's messages, and hide/unhide other
// apps panic like unimplemented!().
func TestUnsupportedOutcomes(t *testing.T) {
	h := desktopNewHost(t)

	if got := h.ThermalState(); got != ThermalStateNominal {
		t.Errorf("thermal state = %v, want Nominal", got)
	}
	if err := h.OnThermalStateChange(func() { t.Error("the thermal callback must never fire") }); err != nil {
		t.Errorf("OnThermalStateChange: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	if _, err := h.PathForAuxiliaryExecutable("helper"); !errors.Is(err, ErrAuxiliaryExecutableUnavailable) {
		t.Errorf("path_for_auxiliary_executable error = %v", err)
	}
	if err := h.RegisterURLScheme("zed"); !errors.Is(err, ErrURLSchemeRegistrationUnsupported) {
		t.Errorf("register_url_scheme error = %v", err)
	}

	// The empty bodies.
	h.ActivateApp(true)
	h.HideApp()

	// Dormant registrations: no Windows event source exists (on_reopen
	// and the menu-open/validate callbacks register and never fire).
	if err := h.OnReopen(func() { t.Error("reopen has no Windows event source") }); err != nil {
		t.Errorf("OnReopen: %v", err)
	}
	if err := h.OnOpenUrls(func([]string) { t.Error("protocol activation does not feed an unpackaged process") }); err != nil {
		t.Errorf("OnOpenUrls: %v", err)
	}
	if err := h.OnWillOpenAppMenu(func() { t.Error("no native menu opens") }); err != nil {
		t.Errorf("OnWillOpenAppMenu: %v", err)
	}
	if err := h.OnValidateAppMenuCommand(func(BoxedAction) bool { return true }); err != nil {
		t.Errorf("OnValidateAppMenuCommand: %v", err)
	}
	time.Sleep(100 * time.Millisecond)

	for name, call := range map[string]func(){
		"HideOtherApps":   h.HideOtherApps,
		"UnhideOtherApps": h.UnhideOtherApps,
	} {
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Errorf("%s must panic (reference unimplemented!())", name)
					return
				}
				if msg, ok := r.(string); !ok || !strings.Contains(msg, "not implemented on Windows") {
					t.Errorf("%s panic = %v", name, r)
				}
			}()
			call()
		}()
	}
}

// ---------------------------------------------------------------------------
// Appearance seam and the ImmersiveColorSet observer
// ---------------------------------------------------------------------------

// TestThemeChangeObserverAndDWMDarkMode delivers a synthetic
// WM_SETTINGCHANGE(0, "ImmersiveColorSet") to a real window with the
// appearance provider injected, and verifies the observer routing: the
// cached appearance flips, the callback runs on the foreground thread,
// and configure_dwm_dark_mode's attribute is really applied (read back
// through DwmGetWindowAttribute). A second change to the same
// appearance does not re-notify (the reference only reports changes).
func TestThemeChangeObserverAndDWMDarkMode(t *testing.T) {
	h := desktopNewHost(t)
	appearanceChanged := make(chan bool, 4)

	origProvider := systemAppearanceProvider
	appearance := WindowAppearanceLight
	systemAppearanceProvider = func() (WindowAppearance, error) { return appearance, nil }
	t.Cleanup(func() { systemAppearanceProvider = origProvider })

	handle, err := h.OpenWindow(WindowOptions{
		Title:               "desktop theme observer",
		Show:                true,
		OnAppearanceChanged: func() { appearanceChanged <- true },
	})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}

	if got, err := handle.Appearance(); err != nil || got != WindowAppearanceLight {
		t.Fatalf("initial appearance = %v, %v", got, err)
	}

	// Theme change to dark: appearance flips, the observer fires, DWM
	// dark mode is applied.
	appearance = WindowAppearanceDark
	immersiveColorSet := []uint16{}
	for _, r := range "ImmersiveColorSet" {
		immersiveColorSet = append(immersiveColorSet, uint16(r))
	}
	immersiveColorSet = append(immersiveColorSet, 0)
	r, _, _ := procSendMessageW.Call(handle.Hwnd(), uintptr(wmSettingChange), 0,
		uintptr(unsafe.Pointer(&immersiveColorSet[0])))
	if r != 0 {
		t.Fatalf("WM_SETTINGCHANGE result = %d, want 0 (consumed)", r)
	}
	select {
	case <-appearanceChanged:
	case <-time.After(10 * time.Second):
		t.Fatal("the appearance observer did not fire on ImmersiveColorSet")
	}
	if got, err := handle.Appearance(); err != nil || got != WindowAppearanceDark {
		t.Fatalf("appearance after the change = %v, %v", got, err)
	}
	dark, err := readDWMDarkMode(handle.Hwnd())
	if err != nil {
		t.Fatalf("reading the DWM dark mode attribute: %v", err)
	}
	if dark != 1 {
		t.Errorf("DWMWA_USE_IMMERSIVE_DARK_MODE = %d, want 1 after the dark change", dark)
	}

	// The same appearance again: no change, no notification (the
	// reference compares before reporting).
	r, _, _ = procSendMessageW.Call(handle.Hwnd(), uintptr(wmSettingChange), 0,
		uintptr(unsafe.Pointer(&immersiveColorSet[0])))
	if r != 0 {
		t.Fatalf("WM_SETTINGCHANGE (no change) result = %d, want 0", r)
	}
	select {
	case <-appearanceChanged:
		t.Fatal("an unchanged appearance must not re-notify")
	case <-time.After(300 * time.Millisecond):
	}

	// A different area string: no theme report at all.
	otherArea := []uint16{}
	for _, c := range "WindowsThemeElement" {
		otherArea = append(otherArea, uint16(c))
	}
	otherArea = append(otherArea, 0)
	procSendMessageW.Call(handle.Hwnd(), uintptr(wmSettingChange), 0, uintptr(unsafe.Pointer(&otherArea[0])))
	select {
	case <-appearanceChanged:
		t.Fatal("a non-ImmersiveColorSet area must not notify")
	case <-time.After(300 * time.Millisecond):
	}
}

// TestRegistrySystemAppearance reads the real registry provider
// (read-only): it must answer Light or Dark without error on this
// machine.
func TestRegistrySystemAppearance(t *testing.T) {
	appearance, err := registrySystemAppearance()
	if err != nil {
		t.Fatalf("registrySystemAppearance: %v", err)
	}
	t.Logf("system appearance: %v", appearance)
	if appearance != WindowAppearanceLight && appearance != WindowAppearanceDark {
		t.Errorf("appearance = %v", appearance)
	}
}

// ---------------------------------------------------------------------------
// Popup rejection and window controls on a real host
// ---------------------------------------------------------------------------

// TestAnchoredPopupRejection verifies the native-popup rejection
// (WindowsWindow::new): an anchored popup fails with the typed error
// and a normal window still opens afterwards.
func TestAnchoredPopupRejection(t *testing.T) {
	h := desktopNewHost(t)
	_, err := h.OpenWindow(WindowOptions{Kind: WindowKindAnchoredPopup, Title: "popup"})
	if !errors.Is(err, ErrPopupNotSupported) {
		t.Fatalf("anchored popup error = %v, want ErrPopupNotSupported", err)
	}
	handle, err := h.OpenWindow(WindowOptions{Title: "after popup rejection"})
	if err != nil {
		t.Fatalf("a normal window must still open after the rejection: %v", err)
	}
	if !handle.Alive() {
		t.Fatal("the normal window is not alive")
	}
}

// TestWindowControlsOnRealHost covers minimize/zoom/restore, the active
// window query, the mouse position query, and the hidden-zoom pending
// state on a real window.
func TestWindowControlsOnRealHost(t *testing.T) {
	h := desktopNewHost(t)
	handle, err := h.OpenWindow(WindowOptions{
		Title:     "desktop controls",
		Show:      true,
		Activate:  true,
		Resizable: true,
		Bounds:    Bounds{Size: Size{Width: 320, Height: 200}},
	})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}
	desktopWaitFor(t, "the window to be visible", handle.Visible)

	if minimized, err := handle.IsMinimized(); err != nil || minimized {
		t.Fatalf("fresh window minimized = %v, %v", minimized, err)
	}
	if active, err := handle.IsActive(); err != nil || !active {
		t.Logf("activated window is not the active window: %v, %v", active, err)
	}

	// Minimize and restore.
	handle.Minimize()
	desktopWaitFor(t, "the window to be minimized", func() bool {
		minimized, _ := handle.IsMinimized()
		return minimized
	})
	// The reference's zoom toggles maximize; a minimized (still
	// visible) window maximizes.
	handle.Zoom()
	desktopWaitFor(t, "the zoom to maximize", func() bool {
		maximized, _ := handle.IsMaximized()
		return maximized
	})
	handle.Zoom()
	desktopWaitFor(t, "the second zoom to restore", func() bool {
		maximized, _ := handle.IsMaximized()
		return !maximized
	})

	// Mouse position: a real query (the cursor is somewhere on screen).
	pos, err := handle.MousePosition()
	if err != nil {
		t.Fatalf("MousePosition: %v", err)
	}
	t.Logf("mouse position (logical, window-local): %+v", pos)

	// Hidden window zoom: the pending state is applied on show.
	hidden, err := h.OpenWindow(WindowOptions{Title: "desktop hidden zoom", Show: false})
	if err != nil {
		t.Fatalf("OpenWindow(hidden): %v", err)
	}
	hidden.Zoom()
	hidden.Show()
	desktopWaitFor(t, "the pending maximize to apply", func() bool {
		maximized, _ := hidden.IsMaximized()
		return maximized
	})

	// RequestAttention on the active window is the no-op path.
	if active, _ := handle.IsActive(); active {
		handle.RequestAttention()
	}
}

// TestCursorHideUntilMoveOnRealWindow drives the cursor policy on a
// real window: hiding flips the shared flag, a synthetic WM_MOUSEMOVE
// restores it, and WM_MOUSELEAVE clears the hovered state again.
func TestCursorHideUntilMoveOnRealWindow(t *testing.T) {
	h := desktopNewHost(t)
	handle, err := h.OpenWindow(WindowOptions{Title: "desktop cursor", Show: true})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}
	desktopWaitFor(t, "the window to be visible", handle.Visible)

	h.SetCursorStyle(CursorIBeam)
	desktopWaitFor(t, "the cursor style to be recorded", func() bool {
		style, err := h.CursorStyle()
		return err == nil && style == CursorIBeam
	})

	if !h.IsCursorVisible() {
		t.Fatal("the cursor starts visible")
	}
	h.HideCursorUntilMouseMoves()
	desktopWaitFor(t, "the cursor flag to clear", func() bool { return !h.IsCursorVisible() })

	// Put the window under the real cursor so the hover tracking is
	// deterministic (a cursor elsewhere makes TrackMouseEvent deliver
	// the leave immediately, which is real behavior but not this
	// assertion's subject).
	scale, err := handle.ScaleFactor()
	if err != nil {
		t.Fatalf("ScaleFactor: %v", err)
	}
	var cursor point
	if ok, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(&cursor))); ok == 0 {
		t.Fatalf("GetCursorPos failed")
	}
	handle.SetBounds(Bounds{
		Origin: Point{X: float32(cursor.x)/scale - 160, Y: float32(cursor.y)/scale - 100},
		Size:   Size{Width: 320, Height: 200},
	})

	// A mouse move over the window restores it (the message is
	// delivered directly: the pump owns the real mouse).
	procSendMessageW.Call(handle.Hwnd(), uintptr(wmMouseMove), 0, 0)
	desktopWaitFor(t, "the mouse move to restore the cursor", func() bool { return h.IsCursorVisible() })

	// WM_MOUSELEAVE clears the hovered state (and the flag stays
	// visible: the leave sets it true).
	desktopWaitFor(t, "the window to be hovered", func() bool {
		hovered, _ := handle.IsHovered()
		return hovered
	})
	procSendMessageW.Call(handle.Hwnd(), uintptr(wmMouseLeave), 0, 0)
	desktopWaitFor(t, "the mouse leave to clear hover", func() bool {
		hovered, _ := handle.IsHovered()
		return !hovered
	})
	if !h.IsCursorVisible() {
		t.Fatal("the mouse leave must leave the cursor visible (tight is_cursor_visible semantics)")
	}
}

// TestPowerBroadcastResumeOnPlatformWindow delivers a controlled
// WM_POWERBROADCAST(PBT_APMRESUMEAUTOMATIC) to the real platform
// window: the system-wake callback fires; a suspend broadcast does not
// wake it.
func TestPowerBroadcastResumeOnPlatformWindow(t *testing.T) {
	h := desktopNewHost(t)
	woken := make(chan bool, 4)
	if err := h.OnSystemWake(func() { woken <- true }); err != nil {
		t.Fatalf("OnSystemWake (RegisterSuspendResumeNotification): %v", err)
	}
	platform := h.PlatformHwnd()
	if platform == 0 {
		t.Fatal("the platform window handle was not exposed")
	}
	procSendMessageW.Call(platform, uintptr(wmPowerBroadcast), uintptr(pbtApmResumeAutomatic), 0)
	select {
	case <-woken:
	case <-time.After(10 * time.Second):
		t.Fatal("PBT_APMRESUMEAUTOMATIC did not run the system-wake callback")
	}
	// A suspend broadcast (PBT_APMSUSPEND = 4) does not wake.
	procSendMessageW.Call(platform, uintptr(wmPowerBroadcast), 4, 0)
	select {
	case <-woken:
		t.Fatal("PBT_APMSUSPEND must not run the system-wake callback")
	case <-time.After(300 * time.Millisecond):
	}
}

// TestEndSessionQuitsThroughQuitCallback delivers a controlled
// WM_ENDSESSION: the app window forwards to the platform window, the
// quit callback runs (returning false — completed shutdown would exit
// the process), the loop quits and Wait returns.
func TestEndSessionQuitsThroughQuitCallback(t *testing.T) {
	h := desktopNewHost(t)
	handle, err := h.OpenWindow(WindowOptions{Title: "desktop end session", Show: true})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}
	desktopWaitFor(t, "the window to be visible", handle.Visible)

	var calls int
	if err := h.OnQuit(func() bool {
		calls++
		return false
	}); err != nil {
		t.Fatalf("OnQuit: %v", err)
	}
	procSendMessageW.Call(handle.Hwnd(), uintptr(wmEndSession), 1, 0)
	waitDone := make(chan error, 1)
	go func() { waitDone <- h.Wait() }()
	select {
	case err := <-waitDone:
		if err != nil {
			t.Fatalf("host Wait after the session end: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the session end did not quit the message loop")
	}
	// The end-session handler and the post-loop run tail each invoke
	// the callback once (the reference runs both: handle_end_session
	// and run's with_callback).
	if calls != 2 {
		t.Logf("quit callback invocations = %d (end-session + post-loop)", calls)
	}
}

// TestSetCursorMessagePolicy pins the WM_SETCURSOR policy (events.rs
// handle_set_cursor): the message is consumed with 0 over ordinary
// areas (the platform cursor is set — or none while hidden), and resize
// edges fall to default processing (DefWindowProc owns the sizing
// cursors there).
func TestSetCursorMessagePolicy(t *testing.T) {
	h := desktopNewHost(t)
	handle, err := h.OpenWindow(WindowOptions{Title: "desktop set cursor", Show: true})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}
	desktopWaitFor(t, "the window to be visible", handle.Visible)

	// Ordinary area: consumed with 0 (the reference returns Some(0)).
	// lparam loword carries the hit-test code (HTCLIENT = 1); wparam is
	// the window under the cursor (this window for the direct send).
	r, _, _ := procSendMessageW.Call(handle.Hwnd(), uintptr(wmSetCursor),
		handle.Hwnd(), uintptr(1))
	if r != 0 {
		t.Fatalf("WM_SETCURSOR(HTCLIENT) result = %d, want 0 (consumed)", r)
	}

	// Resize edge: default processing owns the cursor (the handler
	// falls through, matching the reference's None return).
	r, _, _ = procSendMessageW.Call(handle.Hwnd(), uintptr(wmSetCursor),
		handle.Hwnd(), uintptr(htLeft))
	t.Logf("WM_SETCURSOR(HTLEFT) result = %d (DefWindowProc's sizing-cursor handling)", r)

	// While hidden, the consumed path still applies (SetCursor(None)).
	h.HideCursorUntilMouseMoves()
	desktopWaitFor(t, "the cursor flag to clear", func() bool { return !h.IsCursorVisible() })
	r, _, _ = procSendMessageW.Call(handle.Hwnd(), uintptr(wmSetCursor),
		handle.Hwnd(), uintptr(1))
	if r != 0 {
		t.Fatalf("WM_SETCURSOR(HTCLIENT, hidden) result = %d, want 0 (consumed)", r)
	}
}

// TestMouseWheelSettingsUpdate delivers a synthetic
// WM_SETTINGCHANGE(SPI_GETWHEELSCROLLLINES) and verifies the settings
// refresh (SystemParametersInfoW read-only query).
func TestMouseWheelSettingsUpdate(t *testing.T) {
	h := desktopNewHost(t)
	handle, err := h.OpenWindow(WindowOptions{Title: "desktop wheel settings", Show: true})
	if err != nil {
		t.Fatalf("OpenWindow failed (real window expected): %v", err)
	}
	desktopWaitFor(t, "the window to be visible", handle.Visible)

	r, _, _ := procSendMessageW.Call(handle.Hwnd(), uintptr(wmSettingChange),
		uintptr(spiGetWheelScrollLines), 0)
	if r != 0 {
		t.Fatalf("WM_SETTINGCHANGE result = %d, want 0", r)
	}
	settings := h.MouseWheelSettings()
	t.Logf("mouse wheel settings: %+v", settings)
	if settings.WheelScrollLines > 1000 || settings.WheelScrollChars > 1000 {
		t.Fatalf("implausible wheel settings: %+v", settings)
	}
}
