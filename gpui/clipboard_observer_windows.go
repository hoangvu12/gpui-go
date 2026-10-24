//go:build windows

package gpui

import "fmt"

// The clipboard-change observer (browser-core ticket 26): apps observe
// system-wide clipboard changes through AddClipboardFormatListener on
// the host's platform (message-only) window and the WM_CLIPBOARDUPDATE
// message it posts, the Windows shape Chromium's clipboard monitor
// uses (the remoting host's clipboard_win.cc:158-171 registers its
// message window with AddClipboardFormatListener and runs the update
// from WM_CLIPBOARDUPDATE at :312-318; the browser process notifies
// ClipboardMonitor observers the same way, clipboard_win.cc:1040-1043).
//
//   - Host.OnClipboardChange installs the callback and registers the
//     listener once on the platform window (the foreground marshal,
//     like OnSystemWake's suspend/resume registration).
//   - The platform window's WM_CLIPBOARDUPDATE runs the callback
//     synchronously on the host thread (platformWndProc's case in
//     win32host_windows.go).
//   - App.OnClipboardChange wraps the callback in an application
//     update (the OnSystemWake shape).
//   - The host shutdown removes the listener before destroying the
//     platform window (shutdownDesktopStateOnHostThread).

// OnClipboardChange registers the clipboard-change callback
// (browser-core ticket 26): the callback runs on the host thread for
// every WM_CLIPBOARDUPDATE — any application's write, including this
// process's own (the observer is system-wide; a self-write notifies
// too, exactly like Chromium's ClipboardMonitor). Registering is
// idempotent: the last callback wins, the listener registers once.
func (h *Host) OnClipboardChange(cb func()) error {
	if h.platformHWND == 0 {
		return ErrHostNotStarted
	}
	if cb == nil {
		return fmt.Errorf("gpui: OnClipboardChange requires a non-nil callback")
	}
	h.setDesktopCallback(func(c *desktopCallbacks) *func() { return &c.clipboardChange }, cb)
	if !h.runForegroundSync(func() {
		if !h.desktop.clipboardFormatListener {
			if r, _, callErr := procAddClipboardFormatListener.Call(h.platformHWND); r == 0 {
				// The reference's remoting host logs the failure and
				// continues (clipboard_win.cc:168-170); the port records
				// the fault — the observer simply never fires.
				h.recordFault(fmt.Sprintf("AddClipboardFormatListener failed: %v", callErr))
				return
			}
			h.desktop.clipboardFormatListener = true
		}
	}) {
		return ErrHostStopped
	}
	return nil
}

// handleClipboardUpdateMsg is the platform window's WM_CLIPBOARDUPDATE
// policy: run the clipboard-change observer (synchronously, on the
// host thread — the message marks the whole system clipboard changed).
func (h *Host) handleClipboardUpdateMsg() {
	h.desktop.mu.Lock()
	cb := h.desktop.callbacks.clipboardChange
	h.desktop.mu.Unlock()
	if cb != nil {
		cb()
	}
}
