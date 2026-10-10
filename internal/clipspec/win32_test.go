//go:build windows

package clipspec

// The foreign-side Win32 bindings and fixtures for ticket23's
// clipboard spec tests: these helpers emulate another application
// writing to and reading from the real system clipboard (raw
// CF_UNICODETEXT, CF_DIB, CF_HDROP and custom registered formats),
// bypassing the gpui port entirely. They mirror the same pure-stdlib
// lazy-proc pattern the gpui package uses (CGO_ENABLED=0; pointers to
// Go buffers cross as unsafe.Pointer, Win32 pointers never convert
// back — copies go through RtlMoveMemory).

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// Foreign Win32 bindings
// ---------------------------------------------------------------------------

var (
	modUser32   = syscall.NewLazyDLL("user32.dll")
	modKernel32 = syscall.NewLazyDLL("kernel32.dll")

	foreignOpenClipboard   = modUser32.NewProc("OpenClipboard")
	foreignCloseClipboard  = modUser32.NewProc("CloseClipboard")
	foreignEmptyClipboard  = modUser32.NewProc("EmptyClipboard")
	foreignSetClipboard    = modUser32.NewProc("SetClipboardData")
	foreignGetClipboard    = modUser32.NewProc("GetClipboardData")
	foreignEnumFormatsProc = modUser32.NewProc("EnumClipboardFormats")
	foreignCountFormats    = modUser32.NewProc("CountClipboardFormats")
	foreignRegisterFmtProc = modUser32.NewProc("RegisterClipboardFormatW")
	foreignFormatName      = modUser32.NewProc("GetClipboardFormatNameW")

	foreignGlobalAlloc   = modKernel32.NewProc("GlobalAlloc")
	foreignGlobalFree    = modKernel32.NewProc("GlobalFree")
	foreignGlobalLock    = modKernel32.NewProc("GlobalLock")
	foreignGlobalUnlock  = modKernel32.NewProc("GlobalUnlock")
	foreignGlobalSize    = modKernel32.NewProc("GlobalSize")
	foreignRtlMoveMemory = modKernel32.NewProc("RtlMoveMemory")
)

// Standard clipboard format ids (winuser.h).
const (
	cfDIB         = 8
	cfUnicodeText = 13
	cfHDROP       = 15
)

// ---------------------------------------------------------------------------
// Test helpers (mirroring internal/winhostspec)
// ---------------------------------------------------------------------------

// waitFor polls cond until it holds or the timeout expires.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
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

// expectNoLeakedGoroutines waits for the goroutine count to fall back
// near before, proving the host thread joined and no worker leaked.
func expectNoLeakedGoroutines(t *testing.T, before int) {
	t.Helper()
	waitFor(t, 5*time.Second, "goroutines to drain back near the baseline", func() bool {
		return runtime.NumGoroutine() <= before+2
	})
}

// newHost boots a fresh host and stops it at cleanup, checking the
// thread join and the goroutine baseline (winhostspec's helper).
func newHost(t *testing.T) *gpui.Host {
	t.Helper()
	before := runtime.NumGoroutine()
	h := gpui.NewHost()
	if err := h.Start(); err != nil {
		t.Fatalf("host start failed: %v", err)
	}
	t.Cleanup(func() {
		if err := h.Stop(); err != nil {
			t.Errorf("host stop failed: %v", err)
		}
		expectNoLeakedGoroutines(t, before)
	})
	return h
}

// holdWindow opens one real window on the host and hands back its
// HWND — the clipboard holder the busy test needs (OpenClipboard with
// an OWNER window blocks every other open, including other threads of
// this process; a NULL-owner hold does not, verified on this machine).
func holdWindow(t *testing.T, h *gpui.Host) (gpui.WindowHandle, uintptr) {
	t.Helper()
	wh, err := h.OpenWindow(gpui.WindowOptions{
		Title:  "clipspec busy holder",
		Show:   true,
		Bounds: gpui.Bounds{Size: gpui.Size{Width: 200, Height: 120}},
	})
	if err != nil {
		t.Fatalf("OpenWindow for the busy holder failed: %v — this session is expected to create real windows", err)
	}
	t.Cleanup(func() { wh.Close() })
	return wh, wh.Hwnd()
}

// hostRead reads the clipboard through the host, retrying while the
// busy-window outcome lasts. THIS MACHINE has an active clipboard
// viewer that opens the clipboard for a few milliseconds after every
// SetClipboardData (a WM_DRAWCLIPBOARD consumer — Windows' own
// clipboard-history service or the terminal host); the pinned port
// keeps its fail-fast OpenClipboard semantics, and the retry here is
// the test-side tolerance for that machine noise (the deterministic
// busy behavior is proven by TestClipboardBusyDeniedNoHang with a real
// owner-window hold).
func hostRead(t *testing.T, h *gpui.Host) *gpui.ClipboardItem {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		item, err := h.ReadClipboard()
		if err == nil {
			return item
		}
		var readErr *gpui.ClipboardReadError
		if !errors.As(err, &readErr) || readErr.Kind != gpui.ClipboardReadDenied {
			t.Fatalf("host read: %v", err)
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("host read still busy after 2s of retries: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// hostWrite is hostRead's write-side twin: retry the busy outcome for
// the same machine-noise reason.
func hostWrite(t *testing.T, h *gpui.Host, item gpui.ClipboardItem) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := h.WriteClipboard(item)
		if err == nil {
			return
		}
		var readErr *gpui.ClipboardReadError
		if !errors.As(err, &readErr) || readErr.Kind != gpui.ClipboardReadDenied {
			t.Fatalf("host write: %v", err)
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("host write still busy after 2s of retries: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// Foreign clipboard accessors (the "another application" side)
// ---------------------------------------------------------------------------

// foreignHold keeps the system clipboard open from this goroutine,
// emulating a busy holder. The hold MUST pass an owner HWND:
// OpenClipboard(hwnd) blocks every other open (other threads, other
// processes) until release, while a NULL-owner hold does NOT block
// other threads of the same process (verified on this machine — the
// NULL association is too weak for a deterministic busy test).
//
// The caller's goroutine is OS-thread-locked for the hold's lifetime:
// CloseClipboard must run on the thread that opened the clipboard,
// and Go goroutines migrate between OS threads — without the lock,
// release occasionally ran on a different thread and the close
// failed, wedging the clipboard for every later open (observed as a
// ~1-in-8 flake before the lock).
type foreignHold struct {
	t *testing.T
}

// holdClipboard opens the clipboard with hwnd as the owner and keeps
// it open. The open itself tolerates the machine's transient viewer
// contention (see hostRead) with a bounded retry.
func holdClipboard(t *testing.T, hwnd uintptr) *foreignHold {
	t.Helper()
	runtime.LockOSThread()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if r, _, callErr := foreignOpenClipboard.Call(hwnd); r != 0 {
			return &foreignHold{t: t}
		} else if !time.Now().Before(deadline) {
			runtime.UnlockOSThread()
			t.Fatalf("foreign OpenClipboard(hwnd) stayed busy for 2s: %v", callErr)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// release closes the held clipboard (from the opening thread, which
// the hold keeps locked) and unlocks the OS thread. A close that
// stays failing is fatal: it would wedge the clipboard for every
// later open in this test.
func (hold *foreignHold) release() {
	deadline := time.Now().Add(2 * time.Second)
	for {
		if r, _, _ := foreignCloseClipboard.Call(); r != 0 {
			runtime.UnlockOSThread()
			return
		}
		if !time.Now().Before(deadline) {
			runtime.UnlockOSThread()
			hold.t.Fatalf("CloseClipboard kept failing for 2s; the clipboard is wedged")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// clipboardStillOpenable proves the clipboard is not permanently
// wedged after a host operation: a fresh foreign open/close pair
// succeeds immediately. (Cross-thread NULL-owner opens coexist with a
// same-process NULL hold on this Windows build, so the authoritative
// host-closure proof is each test's SUBSEQUENT host-thread operation:
// a host that left the clipboard open fails every following open, and
// hostRead/hostWrite fail their retry budgets on that.)
func clipboardStillOpenable(t *testing.T) {
	t.Helper()
	foreignOpenWithRetry(t)
	foreignCloseClipboard.Call()
}

// foreignOpenWithRetry opens the clipboard (NULL owner) from this
// goroutine, tolerating the machine's transient clipboard-viewer
// contention (see hostRead): a permanent failure still fails the test.
func foreignOpenWithRetry(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if r, _, _ := foreignOpenClipboard.Call(0); r != 0 {
			return
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("foreign OpenClipboard stayed busy for 2s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// foreignAllocCopy allocates a GMEM_MOVEABLE global, copies data in,
// unlocks it, and returns the handle. A zero-length request returns
// the handle without a lock/copy (a 0-byte GMEM_MOVEABLE object is
// "discarded" and cannot be locked — the shape a foreign app would
// publish for empty data). Ownership of the handle passes to the
// caller (who transfers it with SetClipboardData or frees it).
func foreignAllocCopy(data []byte) uintptr {
	global, _, _ := foreignGlobalAlloc.Call(0x0002, uintptr(len(data)))
	if global == 0 {
		return 0
	}
	if len(data) == 0 {
		return global
	}
	ptr, _, _ := foreignGlobalLock.Call(global)
	if ptr == 0 {
		foreignGlobalFree.Call(global)
		return 0
	}
	foreignRtlMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	foreignGlobalUnlock.Call(global)
	return global
}

// foreignSetBytes opens the clipboard, optionally empties it, publishes
// data under format, and closes it — the minimal foreign writer. On a
// failed SetClipboardData the allocation is freed (ownership did not
// transfer); on success the SYSTEM owns it.
func foreignSetBytes(t *testing.T, format uint32, data []byte, emptyFirst bool) {
	t.Helper()
	foreignOpenWithRetry(t)
	defer foreignCloseClipboard.Call()
	if emptyFirst {
		if r, _, _ := foreignEmptyClipboard.Call(); r == 0 {
			t.Fatalf("foreign EmptyClipboard failed")
		}
	}
	global := foreignAllocCopy(data)
	if global == 0 {
		t.Fatalf("foreign GlobalAlloc/lock failed for %d bytes", len(data))
	}
	if r, _, callErr := foreignSetClipboard.Call(uintptr(format), global); r == 0 {
		foreignGlobalFree.Call(global)
		t.Fatalf("foreign SetClipboardData(%d) failed: %v", format, callErr)
	}
}

// foreignSetUTF16 publishes text as CF_UNICODETEXT (UTF-16 with the
// NUL terminator), like any Windows text application.
func foreignSetUTF16(t *testing.T, text string, emptyFirst bool) {
	t.Helper()
	units, err := syscall.UTF16FromString(text)
	if err != nil {
		t.Fatalf("UTF16FromString: %v", err)
	}
	data := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.NativeEndian.PutUint16(data[i*2:], unit)
	}
	foreignSetBytes(t, cfUnicodeText, data, emptyFirst)
}

// foreignRegisterFormat resolves a registered format's id by name.
func foreignRegisterFormat(t *testing.T, name string) uint32 {
	t.Helper()
	ptr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		t.Fatalf("UTF16PtrFromString(%q): %v", name, err)
	}
	r, _, callErr := foreignRegisterFmtProc.Call(uintptr(unsafe.Pointer(ptr)))
	if r == 0 {
		t.Fatalf("RegisterClipboardFormatW(%q) failed: %v", name, callErr)
	}
	return uint32(r)
}

// foreignFormatData reads the raw bytes currently published under
// format (ok=false when the clipboard does not carry it).
func foreignFormatData(t *testing.T, format uint32) ([]byte, bool) {
	t.Helper()
	foreignOpenWithRetry(t)
	defer foreignCloseClipboard.Call()
	global, _, _ := foreignGetClipboard.Call(uintptr(format))
	if global == 0 {
		return nil, false
	}
	size, _, _ := foreignGlobalSize.Call(global)
	ptr, _, _ := foreignGlobalLock.Call(global)
	if ptr == 0 {
		return nil, false
	}
	defer foreignGlobalUnlock.Call(global)
	out := make([]byte, int(size))
	if size > 0 {
		foreignRtlMoveMemory.Call(uintptr(unsafe.Pointer(&out[0])), ptr, size)
	}
	return out, true
}

// foreignEnumFormats lists the formats currently on the clipboard with
// their printable names (built-in CF_* formats are unnamed by
// GetClipboardFormatNameW and get a synthetic label).
func foreignEnumFormats(t *testing.T) map[uint32]string {
	t.Helper()
	formats := map[uint32]string{}
	foreignOpenWithRetry(t)
	defer foreignCloseClipboard.Call()
	count, _, _ := foreignCountFormats.Call()
	format := uintptr(0)
	for i := uintptr(0); i < count; i++ {
		format, _, _ = foreignEnumFormatsProc.Call(format)
		if format == 0 {
			break
		}
		buffer := make([]uint16, 64)
		n, _, _ := foreignFormatName.Call(
			format, uintptr(unsafe.Pointer(&buffer[0])), 64)
		name := syscall.UTF16ToString(buffer[:n])
		if name == "" {
			switch uint32(format) {
			case cfDIB:
				name = "CF_DIB"
			case cfUnicodeText:
				name = "CF_UNICODETEXT"
			case cfHDROP:
				name = "CF_HDROP"
			default:
				name = fmt.Sprintf("format-%d", format)
			}
		}
		formats[uint32(format)] = name
	}
	return formats
}

// ---------------------------------------------------------------------------
// CF_HDROP fixture (shellapi.h DROPFILES)
// ---------------------------------------------------------------------------

// dropFiles builds a CF_HDROP global's bytes: the 20-byte DROPFILES
// header (pFiles=20, fWide=TRUE at offset 16) followed by the
// double-NUL-terminated UTF-16 path list.
func dropFiles(paths []string) []byte {
	var body []uint16
	for _, path := range paths {
		units, err := syscall.UTF16FromString(path)
		if err != nil {
			panic(err)
		}
		body = append(body, units...)
	}
	body = append(body, 0) // the list's terminating NUL

	data := make([]byte, 20+len(body)*2)
	binary.LittleEndian.PutUint32(data[0:4], 20)  // pFiles
	binary.LittleEndian.PutUint32(data[16:20], 1) // fWide = TRUE
	for i, unit := range body {
		binary.NativeEndian.PutUint16(data[20+i*2:], unit)
	}
	return data
}

// ---------------------------------------------------------------------------
// Image fixtures
// ---------------------------------------------------------------------------

// pngHalves encodes a width x height PNG whose top half is red and
// bottom half is blue (imagespec's fixture shape).
func pngHalves(width, height int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		c := color.NRGBA{R: 255, A: 255}
		if y >= height/2 {
			c = color.NRGBA{B: 255, A: 255}
		}
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// gifSolid encodes a single-frame GIF filled with one color.
func gifSolid(width, height int, c color.Color) []byte {
	img := image.NewPaletted(image.Rect(0, 0, width, height), color.Palette{c})
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

// ---------------------------------------------------------------------------
// DIB fixtures (the CF_DIB payload: BITMAPINFOHEADER [+ palette] +
// rows, bottom-up)
// ---------------------------------------------------------------------------

// dib24bpp builds a 24bpp bottom-up DIB: fill(x, y) yields the RGB at
// image coordinates (y=0 top); the DIB stores BGR rows bottom-up with
// 4-byte row padding.
func dib24bpp(width, height int, fill func(x, y int) (r, g, b uint8)) []byte {
	rowSize := ((width*3 + 3) / 4) * 4
	dib := make([]byte, 40+rowSize*height)
	binary.LittleEndian.PutUint32(dib[0:4], 40)              // biSize
	binary.LittleEndian.PutUint32(dib[4:8], uint32(width))   // biWidth
	binary.LittleEndian.PutUint32(dib[8:12], uint32(height)) // biHeight (positive = bottom-up)
	binary.LittleEndian.PutUint16(dib[12:14], 1)             // biPlanes
	binary.LittleEndian.PutUint16(dib[14:16], 24)            // biBitCount
	// biCompression 0 (BI_RGB) and biClrUsed 0 stay zero.
	for y := 0; y < height; y++ {
		row := dib[40+(height-1-y)*rowSize:]
		for x := 0; x < width; x++ {
			r, g, b := fill(x, y)
			row[x*3+0] = b
			row[x*3+1] = g
			row[x*3+2] = r
		}
	}
	return dib
}

// dib8bpp builds an 8bpp palette DIB: the 256-entry color table maps
// index 0 to black and every other index to the given color; every
// pixel uses index 1.
func dib8bpp(width, height int, c color.RGBA) []byte {
	rowSize := (width + 3) / 4 * 4
	dib := make([]byte, 40+1024+rowSize*height)
	binary.LittleEndian.PutUint32(dib[0:4], 40)
	binary.LittleEndian.PutUint32(dib[4:8], uint32(width))
	binary.LittleEndian.PutUint32(dib[8:12], uint32(height))
	binary.LittleEndian.PutUint16(dib[12:14], 1)
	binary.LittleEndian.PutUint16(dib[14:16], 8)
	// biCompression 0; biClrUsed 0 -> 2^8 = 256 table entries.
	table := dib[40 : 40+1024]
	for i := 0; i < 256; i++ {
		entry := table[i*4:]
		if i == 0 {
			entry[0], entry[1], entry[2], entry[3] = 0, 0, 0, 0
		} else {
			entry[0], entry[1], entry[2], entry[3] = c.B, c.G, c.R, 0
		}
	}
	pixels := dib[40+1024:]
	for i := range pixels {
		pixels[i] = 1
	}
	return dib
}
