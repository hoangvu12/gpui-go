//go:build windows

// Real-host keyboard evidence for ticket13: real Win32 windows in the
// user's interactive session driven by posted keyboard messages, the
// WM_CHAR surrogate assembly and the WM_INPUTLANGCHANGE layout report.
// These tests FAIL (never skip) when window creation fails, like
// internal/winhostspec.

package focusspec

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"gpui-go/gpui"
)

var (
	realModUser32        = syscall.NewLazyDLL("user32.dll")
	realProcPostMessageW = realModUser32.NewProc("PostMessageW")
)

// Win32 message constants (winuser.h).
const (
	realWMKeydown         = 0x0100
	realWMKeyup           = 0x0101
	realWMChar            = 0x0102
	realWMInputLangChange = 0x0051
	realWMGPUIKeydown     = 0x0400 + 8 // WM_GPUI_KEYDOWN
)

func realPostMessage(hwnd uintptr, message uint32, wparam, lparam uintptr) bool {
	ret, _, _ := realProcPostMessageW.Call(hwnd, uintptr(message), wparam, lparam)
	return ret != 0
}

// realEventLog records the real-window keyboard evidence.
type realEventLog struct {
	mu      sync.Mutex
	keys    []string
	texts   []string
	layouts []gpui.KeyboardLayoutInfo
}

func (l *realEventLog) recordKey(entry string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.keys = append(l.keys, entry)
}

func (l *realEventLog) recordText(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.texts = append(l.texts, text)
}

func (l *realEventLog) recordLayout(layout gpui.KeyboardLayoutInfo) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.layouts = append(l.layouts, layout)
}

func (l *realEventLog) snapshot() (keys, texts []string, layouts []gpui.KeyboardLayoutInfo) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string{}, l.keys...), append([]string{}, l.texts...), append([]gpui.KeyboardLayoutInfo{}, l.layouts...)
}

func realWaitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

// newRealAppHost boots a real host attached to a real application and
// stops it at cleanup.
func newRealAppHost(t *testing.T) (*gpui.App, *gpui.Host) {
	t.Helper()
	before := runtime.NumGoroutine()
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("app attach failed: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start failed: %v", err)
	}
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop failed: %v", err)
		}
		realWaitFor(t, 5*time.Second, "goroutines to drain back near the baseline", func() bool {
			return runtime.NumGoroutine() <= before+2
		})
	})
	return app, host
}

// openRealKeyWindow opens one shown real window whose keyboard hooks
// record into the log.
func openRealKeyWindow(t *testing.T, app *gpui.App, title string, log *realEventLog) *gpui.Window {
	t.Helper()
	window, err := app.OpenWindow(gpui.WindowOptions{
		Title:  title,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 320, Height: 200}},
		Show:   true,
		OnKey: func(event any) bool {
			switch e := event.(type) {
			case *gpui.KeyDownEvent:
				log.recordKey(fmt.Sprintf("down:%s:char=%q:prefer=%t:held=%t",
					e.Keystroke.Key, e.Keystroke.KeyChar, e.PreferCharacterInput, e.IsHeld))
			case *gpui.KeyUpEvent:
				log.recordKey(fmt.Sprintf("up:%s", e.Keystroke.Key))
			case *gpui.ModifiersChangedEvent:
				log.recordKey(fmt.Sprintf("mods:%+v:caps=%t", e.Modifiers, e.Capslock.On))
			}
			// Let the window's dispatch tree run too (the user hook is
			// the interceptor seam, not a replacement for dispatch).
			return false
		},
		OnText: func(text string) {
			log.recordText(text)
		},
		OnKeyboardLayoutChange: func(layout gpui.KeyboardLayoutInfo) {
			log.recordLayout(layout)
		},
	})
	if err != nil {
		t.Fatalf("OpenWindow(%q) failed: %v — this session is expected to create real windows", title, err)
	}
	return window
}

// TestRealWindowKeyboardTranslation exercises the real Win32 keyboard
// pipeline: the pump's WM_GPUI_KEYDOWN redirection, the VK translation
// (key name, key char, modifiers, held bit), the WM_CHAR surrogate
// assembly and the WM_INPUTLANGCHANGE layout report, all against a real
// window on the real host thread.
func TestRealWindowKeyboardTranslation(t *testing.T) {
	app, _ := newRealAppHost(t)
	log := &realEventLog{}
	window := openRealKeyWindow(t, app, "gpui-go focusspec keyboard", log)
	hwnd := window.Handle().Hwnd()
	if hwnd == 0 {
		t.Fatal("the real window has no HWND")
	}

	// 'a' (VK 0x41, scan 0x1E): the translation yields the key name
	// with its typed character.
	if !realPostMessage(hwnd, realWMGPUIKeydown, 0x41, 0x1E<<16) {
		t.Fatal("posting WM_GPUI_KEYDOWN(a) failed")
	}
	realWaitFor(t, 5*time.Second, "the 'a' keydown", func() bool {
		keys, _, _ := log.snapshot()
		return len(keys) > 0 && keys[0] == "down:a:char=\"a\":prefer=false:held=false"
	})

	// The repeat bit (lparam bit 30) reports the held state.
	if !realPostMessage(hwnd, realWMGPUIKeydown, 0x41, (0x1E<<16)|(1<<30)) {
		t.Fatal("posting the held 'a' keydown failed")
	}
	realWaitFor(t, 5*time.Second, "the held 'a' keydown", func() bool {
		keys, _, _ := log.snapshot()
		return len(keys) > 1 && keys[1] == "down:a:char=\"a\":prefer=false:held=true"
	})

	// A key-up of 'a' (WM_KEYUP dispatches directly through the
	// wndproc; lparam bit 31 is the key-up transition flag).
	if !realPostMessage(hwnd, realWMKeyup, 0x41, (0x1E<<16)|(1<<31)) {
		t.Fatal("posting WM_KEYUP(a) failed")
	}
	realWaitFor(t, 5*time.Second, "the 'a' keyup", func() bool {
		keys, _, _ := log.snapshot()
		return len(keys) > 2 && keys[2] == "up:a"
	})

	// 'z' (VK 0x5A, scan 0x2C) through the pump path: a plain
	// WM_KEYDOWN is redirected to WM_GPUI_KEYDOWN by the pump.
	if !realPostMessage(hwnd, realWMKeydown, 0x5A, 0x2C<<16) {
		t.Fatal("posting WM_KEYDOWN(z) failed")
	}
	realWaitFor(t, 5*time.Second, "the 'z' keydown", func() bool {
		keys, _, _ := log.snapshot()
		return len(keys) > 3 && keys[3] == "down:z:char=\"z\":prefer=false:held=false"
	})
	// The unconsumed 'z' translates to a WM_CHAR 'z' text delivery (the
	// accelerator contract: only consumed keys skip translation).
	realWaitFor(t, 5*time.Second, "the unconsumed 'z' to translate to text", func() bool {
		_, texts, _ := log.snapshot()
		return len(texts) == 1 && texts[0] == "z"
	})

	// WM_CHAR surrogate assembly: U+1F600 (grinning face) arrives as
	// the high surrogate then the low surrogate and assembles into one
	// text delivery.
	if !realPostMessage(hwnd, realWMChar, 0xD83D, 0) {
		t.Fatal("posting the high surrogate failed")
	}
	if !realPostMessage(hwnd, realWMChar, 0xDE00, 0) {
		t.Fatal("posting the low surrogate failed")
	}
	realWaitFor(t, 5*time.Second, "the surrogate pair text", func() bool {
		_, texts, _ := log.snapshot()
		return len(texts) == 2 && texts[1] == "\U0001F600"
	})

	// A control character is dropped (parse_char_message filters).
	if !realPostMessage(hwnd, realWMChar, 0x01, 0) {
		t.Fatal("posting the control character failed")
	}
	realWaitFor(t, 5*time.Second, "the control character to be dropped", func() bool {
		_, texts, _ := log.snapshot()
		return len(texts) == 2
	})

	// WM_INPUTLANGCHANGE reports the active layout (this machine's
	// layout, read through GetKeyboardLayoutNameW + the registry).
	if !realPostMessage(hwnd, realWMInputLangChange, 1, 0) {
		t.Fatal("posting WM_INPUTLANGCHANGE failed")
	}
	realWaitFor(t, 5*time.Second, "the keyboard layout report", func() bool {
		_, _, layouts := log.snapshot()
		return len(layouts) == 1 && layouts[0].ID != "" && layouts[0].Name != ""
	})
	_, _, layouts := log.snapshot()
	t.Logf("reported keyboard layout: id=%s name=%q", layouts[0].ID, layouts[0].Name)

	window.Close()
	realWaitFor(t, 5*time.Second, "the window to close", func() bool {
		return !window.Alive()
	})
}

// TestRealWindowKeyboardIsolation checks two real windows: a key posted
// to one window dispatches only there, and closing one leaves the other
// fully functional.
func TestRealWindowKeyboardIsolation(t *testing.T) {
	app, _ := newRealAppHost(t)
	log1 := &realEventLog{}
	log2 := &realEventLog{}
	window1 := openRealKeyWindow(t, app, "gpui-go focusspec isolation one", log1)
	window2 := openRealKeyWindow(t, app, "gpui-go focusspec isolation two", log2)

	hwnd1 := window1.Handle().Hwnd()
	hwnd2 := window2.Handle().Hwnd()

	// A key in window one reaches only window one.
	if !realPostMessage(hwnd1, realWMGPUIKeydown, 0x42, 0x30<<16) {
		t.Fatal("posting to window one failed")
	}
	realWaitFor(t, 5*time.Second, "window one's 'b' keydown", func() bool {
		keys, _, _ := log1.snapshot()
		return len(keys) == 1 && keys[0] == "down:b:char=\"b\":prefer=false:held=false"
	})
	keys2, _, _ := log2.snapshot()
	if len(keys2) != 0 {
		t.Fatalf("window two observed %v, want isolation", keys2)
	}

	// A key in window two reaches only window two.
	if !realPostMessage(hwnd2, realWMGPUIKeydown, 0x43, 0x2E<<16) {
		t.Fatal("posting to window two failed")
	}
	realWaitFor(t, 5*time.Second, "window two's 'c' keydown", func() bool {
		_, _, layouts := log2.snapshot()
		keys, _, _ := log2.snapshot()
		_ = layouts
		return len(keys) == 1 && keys[0] == "down:c:char=\"c\":prefer=false:held=false"
	})
	keys1, _, _ := log1.snapshot()
	if len(keys1) != 1 {
		t.Fatalf("window one observed %v, want isolation", keys1)
	}

	// Closing window one leaves window two functional.
	window1.Close()
	realWaitFor(t, 5*time.Second, "window one to close", func() bool {
		return !window1.Alive()
	})
	if !realPostMessage(hwnd2, realWMGPUIKeydown, 0x44, 0x20<<16) {
		t.Fatal("posting to window two after window one closed failed")
	}
	realWaitFor(t, 5*time.Second, "window two's 'd' keydown after the close", func() bool {
		keys, _, _ := log2.snapshot()
		return len(keys) == 2 && keys[1] == "down:d:char=\"d\":prefer=false:held=false"
	})
	window2.Close()
	realWaitFor(t, 5*time.Second, "window two to close", func() bool {
		return !window2.Alive()
	})
}

// Unused import guard (keeps unsafe for future window procedures).
var _ = unsafe.Pointer(nil)
