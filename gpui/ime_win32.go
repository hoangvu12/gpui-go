//go:build windows

package gpui

// The Win32/IMM32 side of ticket20, ported from the pinned CE
// reference 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui_windows/src/events.rs — ImeContext (ImmGetContext /
//     ImmReleaseContext RAII), parse_ime_composition_string /
//     retrieve_composition_cursor_position / should_use_ime_cursor_position
//     (ImmGetCompositionStringW, the GCS_COMPATTR ATTR_INPUT adjacency
//     test), handle_ime_position (WM_IME_STARTCOMPOSITION: caret bounds
//     scaled to client device pixels, ImmSetCompositionWindow CFS_POINT +
//     ImmSetCandidateWindow CFS_CANDIDATEPOS), handle_ime_composition
//     (WM_IME_COMPOSITION routing through imeCompositionDriver), and
//     update_ime_enabled (query_accepts_text_input; ImmAssociateContextEx
//     with the focused-HWND guard and ImmNotifyIME(NI_COMPOSITIONSTR,
//     CPS_COMPLETE) before disassociating — the IME context is
//     per-thread, so without the GetFocus guard a change in this window's
//     text-input state could commit a composition belonging to another
//     window).
//   - crates/gpui_windows/src/window.rs — update_ime_position (the
//     PlatformWindow trait method: logical bounds -> client device
//     POINT, same formula as retrieve_caret_position), set_input_handler /
//     take_input_handler (the take/put seam), and ime_enabled's initial
//     value (true).
//
// Coordinate convention (see ime.go's header for the full resolution):
// the caret point is CLIENT-AREA device pixels —
// (origin * scale).trunc() for x and
// (origin.y * scale).trunc() + (height * scale).trunc() / 2 for y —
// with NO screen-origin translation, exactly as events.rs
// retrieve_caret_position computes it and imm.h's COMPOSITIONFORM /
// CANDIDATEFORM require. The nonzero-window-origin fixture
// (internal/imespec) proves the convention against a real window:
// the point stays inside the client rect and is unchanged by window
// moves.
//
// Pure Go: every IMM32 call goes through syscall (NewLazyDLL +
// NewProc; CGO_ENABLED=0, no new module dependencies). All IME entry
// points run on the host (foreground) thread, exactly like the
// reference's window procedure.

import (
	"fmt"
	"syscall"
	"unsafe"
)

// ---------------------------------------------------------------------------
// IMM32 bindings
// ---------------------------------------------------------------------------

var (
	modImm32 = syscall.NewLazyDLL("imm32.dll")

	procImmGetContext            = modImm32.NewProc("ImmGetContext")
	procImmReleaseContext        = modImm32.NewProc("ImmReleaseContext")
	procImmGetCompositionStringW = modImm32.NewProc("ImmGetCompositionStringW")
	procImmSetCompositionWindow  = modImm32.NewProc("ImmSetCompositionWindow")
	procImmSetCandidateWindow    = modImm32.NewProc("ImmSetCandidateWindow")
	procImmAssociateContextEx    = modImm32.NewProc("ImmAssociateContextEx")
	procImmNotifyIME             = modImm32.NewProc("ImmNotifyIME")
	procGetKeyboardLayoutList    = modUser32.NewProc("GetKeyboardLayoutList")
	procGetFocus                 = modUser32.NewProc("GetFocus")
)

// Win32 IME message ids (winuser.h) and IMM32 constants (imm.h).
const (
	wmIMEStartComposition = 0x010D
	wmIMEEndComposition   = 0x010E
	wmIMEComposition      = 0x010F
	wmIMESetContext       = 0x0281
	wmIMENotify           = 0x0282
	wmIMEChar             = 0x0286
	wmIMERequest          = 0x0288

	cfsPoint        = 0x0002 // CFS_POINT
	cfsCandidatePos = 0x0040 // CFS_CANDIDATEPOS

	niCompositionStr = 0x0015 // NI_COMPOSITIONSTR
	cpsComplete      = 0x0001 // CPS_COMPLETE

	iaceDefault = 0x00010000 // IACE_DEFAULT
)

// compositionForm is Win32 COMPOSITIONFORM (imm.h).
type compositionForm struct {
	dwStyle      uint32
	ptCurrentPos point
	rcArea       rect
}

// candidateForm is Win32 CANDIDATEFORM (imm.h).
type candidateForm struct {
	dwIndex      uint32
	dwStyle      uint32
	ptCurrentPos point
	rcArea       rect
}

// imeContext is the IMM32 input-context handle pair (events.rs
// ImeContext: get/release around every use).
type imeContext struct {
	hwnd uintptr
	himc uintptr
}

// imeContextGet acquires the window's input context (ImeContext::get;
// a zero HIMC reports no context — for example after the window
// disassociated the default context, or before any IME is attached).
func imeContextGet(hwnd uintptr) imeContext {
	himc, _, _ := procImmGetContext.Call(hwnd)
	return imeContext{hwnd: hwnd, himc: himc}
}

// valid reports a usable context handle.
func (c imeContext) valid() bool { return c.himc != 0 }

// release releases the context (ImeContext::drop).
func (c imeContext) release() {
	if c.valid() {
		procImmReleaseContext.Call(c.hwnd, c.himc)
	}
}

// imeContextSource implements imeCompositionSource over a real HIMC
// (parse_ime_composition_string / retrieve_composition_cursor_position
// / should_use_ime_cursor_position).
type imeContextSource struct {
	ctx imeContext
}

// compositionString reads one GCS_* string buffer
// (parse_ime_composition_string: query the byte length, then read the
// bytes and reinterpret as UTF-16; a negative length is a failure).
func (s imeContextSource) compositionString(compType uint32) ([]uint16, bool) {
	length, _, _ := procImmGetCompositionStringW.Call(s.ctx.himc, uintptr(compType), 0, 0)
	if int64(length) < 0 {
		return nil, false
	}
	n := int(length)
	if n == 0 {
		return nil, true
	}
	buffer := make([]uint16, n/2)
	got, _, _ := procImmGetCompositionStringW.Call(
		s.ctx.himc, uintptr(compType),
		uintptr(unsafe.Pointer(&buffer[0])), uintptr(n))
	if int64(got) != int64(length) {
		return nil, false
	}
	return buffer, true
}

// cursorPosition reads GCS_CURSORPOS
// (retrieve_composition_cursor_position: the IMM32 return reinterpreted
// as usize; a negative return is reported as the reference would see
// it — a huge position that fails the adjacency test).
func (s imeContextSource) cursorPosition() int {
	pos, _, _ := procImmGetCompositionStringW.Call(s.ctx.himc, uintptr(GCSCursorPos), 0, 0)
	return int(int64(pos))
}

// shouldUseIMECursorPosition ports should_use_ime_cursor_position:
// read the GCS_COMPATTR bytes and use the suggested cursor position
// only when the cursor sits adjacent to unconverted (ATTR_INPUT)
// composition text.
func (s imeContextSource) shouldUseIMECursorPosition(cursorPos int) bool {
	size, _, _ := procImmGetCompositionStringW.Call(s.ctx.himc, uintptr(GCSCompAttr), 0, 0)
	if int64(size) <= 0 {
		return false
	}
	attrs := make([]byte, int(size))
	got, _, _ := procImmGetCompositionStringW.Call(
		s.ctx.himc, uintptr(GCSCompAttr),
		uintptr(unsafe.Pointer(&attrs[0])), size)
	if int64(got) <= 0 {
		return false
	}
	// Keep the cursor adjacent to the inserted text by only using the
	// suggested position if it's adjacent to unconverted text.
	atCursorIsInput := cursorPos >= 0 && cursorPos < len(attrs) && attrs[cursorPos] == ATTRInput
	beforeCursorIsInput := cursorPos > 0 && cursorPos-1 < len(attrs) && attrs[cursorPos-1] == ATTRInput
	return atCursorIsInput || beforeCursorIsInput
}

// ---------------------------------------------------------------------------
// The host window's IME handling (events.rs WindowsWindowInner)
// ---------------------------------------------------------------------------

// imeTraceEntry records one IMM32 interaction for diagnostics and the
// real-window coordinate fixture (host-thread written, marshaled reads).
type imeTraceEntry struct {
	// CaretPoint is the composition/candidate caret point last computed
	// (client device pixels; retrieve_caret_position's formula).
	CaretX, CaretY int32
	// Scale is the scale factor the conversion used.
	Scale float32
	// WindowOrigin is the window's screen origin at that moment (the
	// fixture's independence proof: the point must not include it).
	WindowOriginX, WindowOriginY float32
	// SetCompositionWindowOK / SetCandidateWindowOK record the IMM32
	// call results (ImmSetCompositionWindow/ImmSetCandidateWindow
	// return FALSE outside a composition or with no IME; recorded
	// honestly).
	SetCompositionWindowOK bool
	SetCandidateWindowOK   bool
	// NotifyComplete reports that the disable path completed the
	// composition (ImmNotifyIME NI_COMPOSITIONSTR CPS_COMPLETE) under
	// the focused-HWND guard.
	NotifyComplete bool
	// AssociateEnabled is the last association state applied.
	AssociateEnabled bool
	// Disassociate reports the ImmAssociateContextEx-disable call.
	Disassociate bool
	// HandledComposition is the last WM_IME_COMPOSITION's consumed
	// state.
	HandledComposition bool
}

// withIMEHandler runs f with the window's active IME input handler
// under the take/put discipline (events.rs with_input_handler +
// with_input_handler_and_scale_factor): the handler is taken out for
// the call — nested messages dispatched during the callback observe no
// handler and fall to DefWindowProc — and the callback runs inside one
// App.Update so effects land at an update boundary (the same wrapping
// App.OpenWindow gives OnText). Reports false when the window has no
// app, no logical window, or no handler implementing IMEInputHandler.
func (w *hostWindow) withIMEHandler(f func(h IMEInputHandler, lw *Window, app *App)) bool {
	host := w.host
	app := host.app
	if app == nil {
		return false
	}
	lw := host.logicalWindowFor(w.hwnd)
	if lw == nil {
		return false
	}
	handler := imeTakeHandler(lw)
	ime, ok := handler.(IMEInputHandler)
	if !ok {
		imeRestoreHandler(lw, handler)
		return false
	}
	defer imeRestoreHandler(lw, handler)
	app.Update(func(a *App) {
		f(ime, lw, a)
	})
	return true
}

// logicalWindowFor resolves the app-layer logical window for a host
// hwnd (foreground thread only: the registry is touched there).
func (h *Host) logicalWindowFor(hwnd uintptr) *Window {
	if h.app == nil {
		return nil
	}
	for _, w := range h.app.windows {
		if w.handle.hwnd == hwnd {
			return w
		}
	}
	return nil
}

// retrieveCaretPosition ports retrieve_caret_position: the caret
// bounds from the input handler (window-local logical pixels) scaled
// into a client device pixel POINT with the y at the caret's vertical
// middle. NO screen-origin translation — see the file header.
func (w *hostWindow) retrieveCaretPosition() (point, bool) {
	var result point
	found := false
	ok := w.withIMEHandler(func(h IMEInputHandler, lw *Window, app *App) {
		selection, ok := h.SelectedTextRange(false, lw, app)
		if !ok {
			return
		}
		bounds, ok := h.BoundsForRange(selection.Range, lw, app)
		if !ok {
			return
		}
		found = true
		// logical to physical (retrieve_caret_position's casts:
		// f32 -> i32 truncation, the height term truncated before the
		// integer /2).
		result = point{
			x: int32(bounds.Origin.X * w.scale),
			y: int32(bounds.Origin.Y*w.scale) + int32(bounds.Size.Height*w.scale)/2,
		}
	})
	if !ok || !found {
		return point{}, false
	}
	return result, true
}

// handleIMEPosition ports handle_ime_position (WM_IME_
// STARTCOMPOSITION): position the composition and candidate windows at
// the caret and report the message consumed (Some(0) unconditionally,
// even without a caret or context).
func (w *hostWindow) handleIMEPosition() uintptr {
	if caret, ok := w.retrieveCaretPosition(); ok {
		w.updateIMEPosition(caret)
	}
	return 0
}

// updateIMEPosition ports update_ime_position: set the composition
// window (CFS_POINT) and the candidate window (CFS_CANDIDATEPOS) to
// the caret point. A missing IME context skips the IMM32 calls
// (ImeContext::get's None path — for example before any IME is
// associated); the computed point and the honest call results are
// recorded in the trace either way (the coordinate fixture observes
// the convention even without an installed IME).
func (w *hostWindow) updateIMEPosition(caret point) {
	compOK, candOK := false, false
	if ctx := imeContextGet(w.hwnd); ctx.valid() {
		defer ctx.release()
		comp := compositionForm{dwStyle: cfsPoint, ptCurrentPos: caret}
		compRet, _, _ := procImmSetCompositionWindow.Call(ctx.himc, uintptr(unsafe.Pointer(&comp)))
		compOK = compRet != 0
		cand := candidateForm{dwStyle: cfsCandidatePos, ptCurrentPos: caret}
		candRet, _, _ := procImmSetCandidateWindow.Call(ctx.himc, uintptr(unsafe.Pointer(&cand)))
		candOK = candRet != 0
	}
	w.recordIMETrace(func(t *imeTraceEntry) {
		t.CaretX, t.CaretY = caret.x, caret.y
		t.Scale = w.scale
		t.WindowOriginX, t.WindowOriginY = w.originX, w.originY
		t.SetCompositionWindowOK = compOK
		t.SetCandidateWindowOK = candOK
	})
}

// updateIMEEnabled ports update_ime_enabled (draw_window's tail): ask
// the input handler whether it accepts text input; on a change either
// (re-)associate the default IME context (IACE_DEFAULT) or — under the
// GetFocus guard, because the IME context is per-thread and shared
// across a thread's windows — complete this window's composition
// (ImmNotifyIME NI_COMPOSITIONSTR CPS_COMPLETE) before disassociating.
func (w *hostWindow) updateIMEEnabled() {
	imeEnabled := false
	w.withIMEHandler(func(h IMEInputHandler, lw *Window, app *App) {
		imeEnabled = h.AcceptsTextInput(lw, app)
	})
	if imeEnabled == w.imeEnabled {
		return
	}
	w.imeEnabled = imeEnabled
	if imeEnabled {
		procImmAssociateContextEx.Call(w.hwnd, 0, uintptr(iaceDefault))
		w.recordIMETrace(func(t *imeTraceEntry) { t.AssociateEnabled = true })
		return
	}
	// The IME context is per-thread, so without this check a change in
	// this window's text input state could commit an IME composition
	// happening in another window (events.rs comment).
	notifyComplete := false
	if focus, _, _ := procGetFocus.Call(); focus == w.hwnd {
		if ctx := imeContextGet(w.hwnd); ctx.valid() {
			procImmNotifyIME.Call(ctx.himc, uintptr(niCompositionStr), uintptr(cpsComplete), 0)
			ctx.release()
			notifyComplete = true
		}
	}
	procImmAssociateContextEx.Call(w.hwnd, 0, 0)
	w.recordIMETrace(func(t *imeTraceEntry) {
		t.NotifyComplete = notifyComplete
		t.Disassociate = true
		t.AssociateEnabled = false
	})
}

// handleIMEComposition ports handle_ime_composition (WM_IME_
// COMPOSITION): acquire the context (no context -> DefWindowProc),
// drive the composition routing, and record the outcome. Returns
// (result, handled); handled=false means DefWindowProcW.
func (w *hostWindow) handleIMEComposition(lparam uintptr) (uintptr, bool) {
	ctx := imeContextGet(w.hwnd)
	if !ctx.valid() {
		return 0, false
	}
	defer ctx.release()
	driver := &imeCompositionDriver{seam: w, src: imeContextSource{ctx: ctx}}
	result, handled := driver.handleComposition(lparam)
	w.recordIMETrace(func(t *imeTraceEntry) { t.HandledComposition = handled })
	return result, handled
}

// recordIMETrace stores the last trace entry (host thread only).
func (w *hostWindow) recordIMETrace(f func(t *imeTraceEntry)) {
	w.imeTraceMu.Lock()
	f(&w.imeTrace)
	w.imeTraceMu.Unlock()
}

// ---------------------------------------------------------------------------
// The push path (window.rs PlatformWindow::update_ime_position)
// ---------------------------------------------------------------------------

// UpdateIMEPosition pushes new IME panel position suggestions for the
// given logical bounds (PlatformWindow::update_ime_position): convert
// window-local logical bounds into the client device caret POINT and
// set the composition/candidate windows. Stale leases are no-ops.
func (wh WindowHandle) UpdateIMEPosition(bounds Bounds) {
	if wh.host == nil {
		return
	}
	wh.host.runForegroundAsync(func() {
		w := wh.record()
		if w == nil {
			return
		}
		caret := point{
			x: int32(bounds.Origin.X * w.scale),
			y: int32(bounds.Origin.Y*w.scale) + int32(bounds.Size.Height*w.scale)/2,
		}
		w.updateIMEPosition(caret)
	})
}

// IMETraceEntry records one window's last IMM32 interaction for
// diagnostics and tests: the computed caret point, the scale and
// window origin at that moment, and the honest IMM32 call results.
type IMETraceEntry struct {
	// CaretX/CaretY is the composition/candidate caret point last
	// computed (client device pixels; retrieve_caret_position's
	// formula).
	CaretX, CaretY int32
	// Scale is the scale factor the conversion used.
	Scale float32
	// WindowOriginX/WindowOriginY is the window's screen origin (device
	// pixels) at that moment: the nonzero-origin fixture proves the
	// caret point does NOT include it.
	WindowOriginX, WindowOriginY float32
	// SetCompositionWindowOK/SetCandidateWindowOK record the IMM32 call
	// results (FALSE outside a composition or with no IME — honest
	// records, not gates).
	SetCompositionWindowOK bool
	SetCandidateWindowOK   bool
	// NotifyComplete reports that the disable path completed the
	// composition (ImmNotifyIME NI_COMPOSITIONSTR CPS_COMPLETE) under
	// the focused-HWND guard.
	NotifyComplete bool
	// AssociateEnabled is the last association state applied.
	AssociateEnabled bool
	// Disassociate reports the ImmAssociateContextEx-disable call.
	Disassociate bool
	// HandledComposition is the last WM_IME_COMPOSITION's consumed
	// state.
	HandledComposition bool
}

// IMETrace returns the window's last recorded IMM32 interaction
// (diagnostics and the coordinate fixture's observation surface; the
// trace reflects real ImmSetCompositionWindow/ImmSetCandidateWindow
// call results). A stale lease reports ErrWindowClosed.
func (wh WindowHandle) IMETrace() (IMETraceEntry, error) {
	if wh.host == nil {
		return IMETraceEntry{}, ErrWindowClosed
	}
	return queryForeground(wh.host, func() (IMETraceEntry, error) {
		w := wh.record()
		if w == nil {
			return IMETraceEntry{}, ErrWindowClosed
		}
		w.imeTraceMu.Lock()
		t := w.imeTrace
		w.imeTraceMu.Unlock()
		return IMETraceEntry{
			CaretX:                 t.CaretX,
			CaretY:                 t.CaretY,
			Scale:                  t.Scale,
			WindowOriginX:          t.WindowOriginX,
			WindowOriginY:          t.WindowOriginY,
			SetCompositionWindowOK: t.SetCompositionWindowOK,
			SetCandidateWindowOK:   t.SetCandidateWindowOK,
			NotifyComplete:         t.NotifyComplete,
			AssociateEnabled:       t.AssociateEnabled,
			Disassociate:           t.Disassociate,
			HandledComposition:     t.HandledComposition,
		}, nil
	})
}

// ---------------------------------------------------------------------------
// IME availability (honest environment recording; no installs)
// ---------------------------------------------------------------------------

// IMELanguageAvailability records one installed-or-template CJK
// keyboard-layout/IME entry, for the ticket's honest environment
// ledger.
type IMELanguageAvailability struct {
	// KLID is the keyboard-layout identifier (e.g. "00000411" or an
	// IME-prefixed "E0010411").
	KLID string
	// LayoutText is the registry "Layout Text" (e.g. "Japanese").
	LayoutText string
	// LayoutFile is the registry "Layout File" (KBDJPN.DLL for a plain
	// layout; an IME DLL for a real IME entry).
	LayoutFile string
	// Installed reports whether the entry is part of the user's active
	// input profile (GetKeyboardLayoutList) rather than a registry
	// template.
	Installed bool
}

// IMEInstalledLayouts enumerates the machine's CJK IME/keyboard
// entries honestly: the user's actually loaded input profiles
// (GetKeyboardLayoutList, HKL layout words) plus the registry's
// Japanese/Chinese/Korean template keys (KLIDs whose low word names a
// CJK input locale: 0411 Japanese, 0412 Korean, 0804/0404 Chinese
// simplified/traditional). Registry templates without an installed
// profile are NOT sessions: this records availability, never installs
// anything, and the composition-session evidence marks them
// unavailable.
func IMEInstalledLayouts() []IMELanguageAvailability {
	cjk := func(word uint16) bool {
		switch word {
		case 0x0411, 0x0412, 0x0804, 0x0404:
			return true
		}
		return false
	}
	// The installed-profile keys match registry KLIDs in both forms:
	// the base-layout KLID ("00000411" — the HKL's low word) and the
	// IME KLID ("E0010411" — the full HKL value for IME profiles).
	installed := map[string]bool{}
	if n, _, _ := procGetKeyboardLayoutList.Call(0, 0); n != 0 {
		hkls := make([]uintptr, int(n))
		if got, _, _ := procGetKeyboardLayoutList.Call(n, uintptr(unsafe.Pointer(&hkls[0]))); int(got) > 0 {
			for _, hkl := range hkls[:int(got)] {
				if cjk(uint16(hkl)) {
					installed[fmt.Sprintf("%08X", uint32(uint16(hkl)))] = true
					installed[fmt.Sprintf("%08X", uint32(hkl))] = true
				}
			}
		}
	}

	// Registry template enumeration (HKLM Keyboard Layouts, the same
	// source currentKeyboardLayout reads for names).
	out := []IMELanguageAvailability{}
	add := func(klid string, installedHere bool) {
		out = append(out, IMELanguageAvailability{
			KLID:       klid,
			LayoutText: readLayoutText(klid),
			LayoutFile: readLayoutFile(klid),
			Installed:  installedHere,
		})
	}
	for _, klid := range imeCJKRegistryKLIDs() {
		add(klid, installed[klid])
	}
	return out
}

// readLayoutFile reads a KLID's "Layout File" from the HKLM keyboard
// layouts registry (mirrors readLayoutText).
func readLayoutFile(id string) string {
	path := fmt.Sprintf("System\\CurrentControlSet\\Control\\Keyboard Layouts\\%s", id)
	pathUTF16, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var key uintptr
	ret, _, _ := procRegOpenKeyExW.Call(
		hklmRoot, // HKEY_LOCAL_MACHINE
		uintptr(unsafe.Pointer(pathUTF16)),
		0, hklmKeyRead, uintptr(unsafe.Pointer(&key)))
	if ret != 0 || key == 0 {
		return ""
	}
	defer procRegCloseKey.Call(key)

	valueUTF16, err := syscall.UTF16PtrFromString("Layout File")
	if err != nil {
		return ""
	}
	var dataType, dataSize uint32
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueUTF16)),
		0,
		uintptr(unsafe.Pointer(&dataType)),
		0,
		uintptr(unsafe.Pointer(&dataSize)))
	if ret != 0 || dataSize == 0 {
		return ""
	}
	data := make([]byte, dataSize)
	ret, _, _ = procRegQueryValueExW.Call(
		key,
		uintptr(unsafe.Pointer(valueUTF16)),
		0,
		0,
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(unsafe.Pointer(&dataSize)))
	if ret != 0 {
		return ""
	}
	units := make([]uint16, dataSize/2)
	for i := range units {
		units[i] = uint16(data[2*i]) | uint16(data[2*i+1])<<8
	}
	return utf16String(units)
}

// imeCJKRegistryKLIDs lists the CJK-language KLID templates present in
// the HKLM keyboard-layouts registry (advapi32 key enumeration via
// RegEnumKeyExW).
func imeCJKRegistryKLIDs() []string {
	pathUTF16, err := syscall.UTF16PtrFromString("System\\CurrentControlSet\\Control\\Keyboard Layouts")
	if err != nil {
		return nil
	}
	var key uintptr
	ret, _, _ := procRegOpenKeyExW.Call(
		hklmRoot, // HKEY_LOCAL_MACHINE
		uintptr(unsafe.Pointer(pathUTF16)),
		0, hklmKeyRead, uintptr(unsafe.Pointer(&key)))
	if ret != 0 || key == 0 {
		return nil
	}
	defer procRegCloseKey.Call(key)

	var klids []string
	for index := 0; ; index++ {
		var name [16]uint16 // KL_NAMELENGTH+ pad
		nameLen := uint32(len(name))
		ret, _, _ := procRegEnumKeyExW.Call(
			key,
			uintptr(index),
			uintptr(unsafe.Pointer(&name[0])),
			uintptr(unsafe.Pointer(&nameLen)),
			0, 0, 0, 0)
		if ret != 0 {
			break
		}
		klid := utf16String(name[:nameLen])
		// The KLID's low 16 bits are the input-language id; CJK ranges:
		// 0411 Japanese, 0412 Korean, 0804/0404 Chinese.
		var v uint64
		fmt.Sscanf(klid, "%X", &v)
		switch uint16(v) {
		case 0x0411, 0x0412, 0x0804, 0x0404:
			klids = append(klids, klid)
		}
	}
	return klids
}

var procRegEnumKeyExW = modAdvapi32.NewProc("RegEnumKeyExW")
