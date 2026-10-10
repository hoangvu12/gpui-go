//go:build windows

package gpui

// Ticket24's OLE drag/drop, ported from the pinned CE
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui_windows/src/window.rs:1201-1345 — the
//     #[implement(IDropTarget)] WindowsDragDropHandler: DragEnter
//     (QueryGetData CF_HDROP, the DROPEFFECT_COPY/DROPEFFECT_NONE
//     negotiation, GetData + with_file_names + ReleaseStgMedium, the
//     window-relative conversion through ScreenToClient and the scale
//     factor, the IDropTargetHelper DragEnter), DragOver (COPY effect,
//     helper DragOver, ScreenToClient), DragLeave (helper DragLeave,
//     the Exited event) and Drop (helper Drop, the Submit event).
//   - crates/gpui_windows/src/window.rs:1574-1586 — register_drag_drop
//     (RegisterDragDrop on the window handle; the IDropTarget lifetime
//     is owned by Windows until RevokeDragDrop).
//   - crates/gpui_windows/src/window.rs:590-602 — the window drop:
//     RevokeDragDrop immediately before DestroyWindow, on the
//     foreground executor.
//   - crates/gpui_windows/src/platform.rs:177-184 — the platform's
//     IDropTargetHelper: CoCreateInstance(CLSID_DragDropHelper,
//     CLSCTX_INPROC_SERVER), created once for the platform.
//
// The IDropTargetHelper calls use the SCREEN cursor position in every
// arm: window.rs's DragEnter shadows `cursor_position` with a mutable
// copy for the ScreenToClient conversion, so the helper call after the
// if/else still receives the outer (screen) binding — same as
// DragOver and Drop, whose helper calls run before their conversion.
//
// Go COM implementation pattern (the reverse direction of
// dialogs_windows.go's caller-side comIFace): the Go object's first
// field is its vtable pointer, so the COM self pointer is the object
// address; the vtable slots are syscall.NewCallback trampolines
// created once per process (NewCallback's pool is bounded), which
// resolve the object through a registry keyed by the self value. The
// registry never converts a self value back to a Go pointer
// (the vet discipline win32host_windows.go documents); it keeps the
// registered Go object alive, which is also the GC-side half of the
// COM lifetime: OLE holds a COM reference the Go GC cannot see.
//
// The generation/window-lifetime discipline: every IDropTarget
// callback resolves the window through the host's hwnd binding table
// (windowFor) and checks the host window id, so a callback racing a
// destroyed window — including inside OLE's modal drag loop — never
// dereferences an expired record (a stale lease answers
// DROPEFFECT_NONE with E_UNEXPECTED and a recorded fault; the pin
// would keep the Rc<WindowsWindowInner> alive instead, which Go's
// host-side record retirement cannot offer).
//
// The OLE SOURCE side (IDataObject + IDropSource + DoDragDrop) is this
// port's driver for verifying the native lifecycle: the pinned
// Windows platform never starts an external drag
// (can_start_external_drag/start_external_drag keep the platform.rs
// trait defaults, both false — window.go's CanStartExternalDrag
// documents the seam), so the source adapter exists to synthesize a
// real OLE drag session against the registered target and to hand a
// CF_HDROP data object to the deterministic transcript driver. Pure
// Go: CGO_ENABLED=0, stdlib syscall trampolines only.

import (
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Win32 bindings (ticket24's own set)
// ---------------------------------------------------------------------------

var (
	procRegisterDragDrop = modOle32.NewProc("RegisterDragDrop")
	procRevokeDragDrop   = modOle32.NewProc("RevokeDragDrop")
	procDoDragDrop       = modOle32.NewProc("DoDragDrop")
	procReleaseStgMedium = modOle32.NewProc("ReleaseStgMedium")
	// procScreenToClient and procGetCursorPos live in desktop_windows.go
	// (ticket26's cursor slice owns the declarations).
)

// Win32 constants (oleidl.h, shlobj_core.h ShlGuid.h, shellapi.h).
const (
	// DROPEFFECT values.
	dropEffectNone = 0
	dropEffectCopy = 1
	dropEffectMove = 2
	dropEffectLink = 4

	// QueryContinueDrag completion codes (oleidl.h).
	dragDropSDrop              = uintptr(0x00040101)
	dragDropSCancel            = uintptr(0x00040102)
	dragDropSUseDefaultCursors = uintptr(0x00040103)

	dvaspectContent = 1 // DVASPECT_CONTENT
	tymedHGlobal    = 1 // TYMED_HGLOBAL
)

// GUIDs (oleidl.h, objidl.h, shobjidl_core.h, ShlGuid.h) from the
// installed Windows SDK 10.0.26100.0 headers, verified against
// ShlGuid.h (CLSID_DragDropHelper is 4657278A-…, one off the
// IDropTargetHelper interface IID 4657278B-…).
var (
	clsidDragDropHelper  = guid{0x4657278A, 0x411B, 0x11D2, [8]byte{0x83, 0x9A, 0x00, 0xC0, 0x4F, 0xD9, 0x18, 0xD0}}
	iidIDropTarget       = guid{0x00000122, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIDropSource       = guid{0x00000121, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIDataObject       = guid{0x0000010E, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidIDropTargetHelper = guid{0x4657278B, 0x411B, 0x11D2, [8]byte{0x83, 0x9A, 0x00, 0xC0, 0x4F, 0xD9, 0x18, 0xD0}}
	iidIUnknown          = guid{0x00000000, 0x0000, 0x0000, [8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
)

// HRESULTs.
const (
	eNoInterface = uintptr(0x80004002)
	eUnexpected  = uintptr(0x8000FFFF)
	eFail        = uintptr(0x80004005)
	eNotImpl     = uintptr(0x80004001)
	ePointer     = uintptr(0x80004003)
)

// pointl is Win32 POINTL (the DragEnter/DragOver/Drop coordinate
// argument: LONG x, y in screen coordinates, passed BY VALUE in the
// COM ABI — one 8-byte register slot).
type pointl struct {
	x, y int32
}

// packPointl packs a POINTL into the register slot value.
func packPointl(x, y int32) uintptr {
	return uintptr(uint64(uint32(x)) | uint64(uint32(y))<<32)
}

// unpackPointl unpacks a POINTL from a register slot value.
func unpackPointl(packed uintptr) pointl {
	return pointl{x: int32(uint32(packed)), y: int32(uint32(packed >> 32))}
}

// formatEtc is Win32 FORMATETC (objidl.h; 32 bytes on amd64).
type formatEtc struct {
	cfFormat uint16
	_pad     [6]byte
	ptd      uintptr
	dwAspect uint32
	lindex   int32
	tymed    uint32
	_pad2    [4]byte
}

// stgMedium is Win32 STGMEDIUM (objidl.h; 24 bytes on amd64: DWORD
// tymed, the 8-byte union (the HGLOBAL arm is the one the CF_HDROP
// read uses), and the trailing pUnkForRelease pointer — which
// ReleaseStgMedium consults, so the struct must carry it or the call
// reads past the buffer).
type stgMedium struct {
	tymed          uint32
	_pad           [4]byte
	hGlobal        uintptr
	pUnkForRelease uintptr
}

// ---------------------------------------------------------------------------
// The Go COM object registry
// ---------------------------------------------------------------------------

// comObjectRegistry maps a COM self value (the address of a Go
// object's vtable-pointer field) to its Go object. The trampolines
// look instances up by the self argument; the registry's strong
// reference keeps the object alive for as long as COM holds a
// reference the Go GC cannot see (unregistration happens at refcount
// zero or at explicit teardown).
var (
	comObjectsMu sync.Mutex
	comObjects   = map[uintptr]any{}
)

// comRegister registers obj under the COM self value.
func comRegister(self uintptr, obj any) {
	comObjectsMu.Lock()
	comObjects[self] = obj
	comObjectsMu.Unlock()
}

// comUnregister drops the registration for a self value.
func comUnregister(self uintptr) {
	comObjectsMu.Lock()
	delete(comObjects, self)
	comObjectsMu.Unlock()
}

// comResolve looks the object up for a self value (nil when unknown —
// a released or never-registered object).
func comResolve(self uintptr) any {
	comObjectsMu.Lock()
	obj := comObjects[self]
	comObjectsMu.Unlock()
	return obj
}

// comTrampolineResult is the recover handler shared by every COM
// trampoline: a Go panic is caught at the trampoline (the native ABI
// rule) and reported as E_FAIL instead of unwinding into the COM call
// frame.
func comTrampolineResult(self uintptr, r any) uintptr {
	if host, isHost := comHostOf(self); isHost {
		host.recordPanic(r)
	} else {
		tracef("com trampoline panic (self=%x): %v", self, r)
	}
	return eFail
}

// comHostOf best-effort resolves a recording Host for a trampoline
// panic. The trampolines pass their object; when the registry lookup
// misses (the object already released), the panic is only traced.
func comHostOf(self uintptr) (*Host, bool) {
	switch obj := comResolve(self).(type) {
	case *oleDropTarget:
		return obj.host, true
	case *goDataObject:
		return obj.host, true
	case *goDropSource:
		return obj.host, true
	case *dmEventHandlerObj:
		return obj.host, true
	case *dmFakeContent:
		return obj.host, true
	case *dmFakeViewport:
		return obj.host, true
	}
	return nil, false
}

// comSelf returns the COM self value for a Go object whose first
// field is its vtable pointer (the interface pointer handed to COM).
// The uintptr never converts back to a pointer (the registry key
// discipline above).
func comSelf(obj any) uintptr {
	// The vtable-pointer field is the struct's first word; the self
	// value is the object's address.
	switch typed := obj.(type) {
	case *oleDropTarget:
		return uintptr(unsafe.Pointer(&typed.vtbl))
	case *goDataObject:
		return uintptr(unsafe.Pointer(&typed.vtbl))
	case *goDropSource:
		return uintptr(unsafe.Pointer(&typed.vtbl))
	case *dmEventHandlerObj:
		return uintptr(unsafe.Pointer(&typed.vtbl))
	case *dmFakeContent:
		return uintptr(unsafe.Pointer(&typed.vtbl))
	case *dmFakeViewport:
		return uintptr(unsafe.Pointer(&typed.vtbl))
	}
	panic("gpui: comSelf on an unsupported COM object type")
}

// hrOK reports a success HRESULT (S_OK or S_FALSE).
func hrOK(hr uintptr) bool { return int32(hr) >= 0 }

// hresultString renders an HRESULT for diagnostics.
func hresultString(hr uintptr) string {
	return fmt.Sprintf("0x%08x", uint32(hr))
}

// ---------------------------------------------------------------------------
// The IDropTarget implementation (window.rs WindowsDragDropHandler)
// ---------------------------------------------------------------------------

// oleDropTarget is the port of WindowsDragDropHandler: the Go object
// implementing IDropTarget for one host window. Its first field is
// the vtable pointer (the COM self pointer); the registry keeps it
// alive while OLE holds a reference.
type oleDropTarget struct {
	vtbl *dropTargetVtable

	// host is the owning host (for faults, panics and the helper).
	host *Host
	// hwnd is the registered window handle.
	hwnd uintptr
	// windowID is the host window id at registration (the generation
	// stamp: callbacks validate the binding table entry against it).
	windowID uint64
	// refs is the COM reference count (atomic; OLE AddRef/Release).
	refs int32
	// revoked records that RevokeDragDrop ran for the window: further
	// COM callbacks are refused (the window lease is gone).
	revoked atomic.Bool
}

// dropTargetVtable is the IDropTarget vtable (oleidl.h: QueryInterface,
// AddRef, Release, DragEnter, DragOver, DragLeave, Drop).
type dropTargetVtable struct {
	queryInterface uintptr
	addRef         uintptr
	release        uintptr
	dragEnter      uintptr
	dragOver       uintptr
	dragLeave      uintptr
	drop           uintptr
}

var (
	dropTargetVtableOnce sync.Once
	dropTargetVtableVal  dropTargetVtable
)

// dropTargetVtableGet creates the shared trampolines once per process.
func dropTargetVtableGet() *dropTargetVtable {
	dropTargetVtableOnce.Do(func() {
		dropTargetVtableVal = dropTargetVtable{
			queryInterface: syscall.NewCallback(dropTargetQueryInterface),
			addRef:         syscall.NewCallback(dropTargetAddRef),
			release:        syscall.NewCallback(dropTargetRelease),
			dragEnter:      syscall.NewCallback(dropTargetDragEnter),
			dragOver:       syscall.NewCallback(dropTargetDragOver),
			dragLeave:      syscall.NewCallback(dropTargetDragLeave),
			drop:           syscall.NewCallback(dropTargetDrop),
		}
	})
	return &dropTargetVtableVal
}

// newOleDropTarget creates and registers the drop target object for a
// host window (the register_drag_drop port). Runs on the host thread.
func newOleDropTarget(w *hostWindow) (*oleDropTarget, error) {
	target := &oleDropTarget{
		vtbl:     dropTargetVtableGet(),
		host:     w.host,
		hwnd:     w.hwnd,
		windowID: w.id,
	}
	self := comSelf(target)
	comRegister(self, target)
	registered, _, callErr := procRegisterDragDrop.Call(w.hwnd, self)
	if int32(registered) < 0 {
		comUnregister(self)
		return nil, fmt.Errorf("gpui: unable to register drag-drop event: RegisterDragDrop: %v (HRESULT %s)",
			callErr, hresultString(registered))
	}
	// RegisterDragDrop AddRef'd the target (the registration reference
	// OLE releases at RevokeDragDrop). The Go-side lifetime is the
	// registry entry, released by Release at refcount zero.
	return target, nil
}

// revoke releases the registration exactly once (window.rs Drop's
// RevokeDragDrop before DestroyWindow). Runs on the host thread.
func (t *oleDropTarget) revoke() {
	if t == nil || !t.revoked.CompareAndSwap(false, true) {
		return
	}
	procRevokeDragDrop.Call(t.hwnd)
	// OLE released its registration reference; the object stays valid
	// for any still-pinned references, and the registry entry drops at
	// refcount zero.
}

// liveWindow resolves the window record generation-checked: the hwnd
// binding plus the id stamp. Nil means the lease is expired (a stale
// callback answers NONE with E_UNEXPECTED instead of dereferencing).
func (t *oleDropTarget) liveWindow() *hostWindow {
	if t.revoked.Load() {
		return nil
	}
	w := windowFor(t.hwnd)
	if w == nil || w.host != t.host || w.id != t.windowID {
		return nil
	}
	return w
}

// deliver routes one file-drop event into the window's input callback
// (handle_drag_drop: the callback runs synchronously, taken and
// restored around the call).
func (t *oleDropTarget) deliver(w *hostWindow, event FileDropEvent) {
	if w.onFileDrop != nil {
		w.onFileDrop(event)
	}
}

// logicalWindowPoint converts a client device-pixel point to
// window-local logical pixels with the window's current scale factor
// (logical_point in window.rs).
func (w *hostWindow) logicalWindowPoint(x, y int32) Point {
	return Point{X: float32(x) / w.scale, Y: float32(y) / w.scale}
}

// screenToClientPoint converts a screen point to client device pixels
// through the window's HWND.
func screenToClientPoint(hwnd uintptr, x, y int32) (int32, int32, bool) {
	pt := point{x: x, y: y}
	converted, _, _ := procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
	return pt.x, pt.y, converted != 0
}

// hdropFormatEtc builds the CF_HDROP query the pin uses (window.rs
// DragEnter's config).
func hdropFormatEtc() formatEtc {
	return formatEtc{
		cfFormat: cfHDROP,
		ptd:      0,
		dwAspect: dvaspectContent,
		lindex:   -1,
		tymed:    tymedHGlobal,
	}
}

// dataObjectHasFormat asks the data object whether the CF_HDROP
// format is available (IDataObject::QueryGetData, vtable slot 5).
func dataObjectHasFormat(data *comIFace, config *formatEtc) bool {
	hr, _, _ := syscall.SyscallN(data.vtbl[5],
		uintptr(unsafe.Pointer(data)), uintptr(unsafe.Pointer(config)))
	return uint32(hr) == uint32(sOK)
}

// dataObjectGetCFHDROP reads the CF_HDROP global from the data object
// (IDataObject::GetData, vtable slot 3) and walks its file names with
// ticket23's DragQueryFileW helper (with_file_names — window.rs
// passes the HGLOBAL handle itself, unlike clipboard.rs's locked
// pointer). The medium is released exactly once (ReleaseStgMedium).
// ok=false mirrors the pin's invalid-hGlobal / GetData-error arms.
func dataObjectGetCFHDROP(h *Host, data *comIFace) (paths ExternalPaths, ok bool) {
	config := hdropFormatEtc()
	var medium stgMedium
	hr, _, _ := syscall.SyscallN(data.vtbl[3],
		uintptr(unsafe.Pointer(data)), uintptr(unsafe.Pointer(&config)),
		uintptr(unsafe.Pointer(&medium)))
	if int32(hr) < 0 || medium.tymed != tymedHGlobal || medium.hGlobal == 0 {
		return nil, false
	}
	hdrop := medium.hGlobal
	var collected []string
	withFileNames(h, hdrop, func(name string) {
		collected = append(collected, name)
	})
	procReleaseStgMedium.Call(uintptr(unsafe.Pointer(&medium)))
	return ExternalPaths(collected), true
}

// ---------------------------------------------------------------------------
// The IDropTarget trampolines (oleidl.h vtable order)
// ---------------------------------------------------------------------------

// dropTargetQueryInterface is IUnknown::QueryInterface: IUnknown and
// IDropTarget are the supported identities.
func dropTargetQueryInterface(self uintptr, riid, ppv unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	target, _ := comResolve(self).(*oleDropTarget)
	if target == nil || ppv == nil {
		return ePointer
	}
	out := (*uintptr)(ppv)
	*out = 0
	if riid == nil {
		return ePointer
	}
	iid := (*guid)(riid)
	if *iid == iidIUnknown || *iid == iidIDropTarget {
		*out = self
		atomic.AddInt32(&target.refs, 1)
		return sOK
	}
	return eNoInterface
}

// dropTargetAddRef is IUnknown::AddRef.
func dropTargetAddRef(self uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	target, _ := comResolve(self).(*oleDropTarget)
	if target == nil {
		return 0
	}
	return uintptr(uint32(atomic.AddInt32(&target.refs, 1)))
}

// dropTargetRelease is IUnknown::Release: at refcount zero the object
// is dead COM-side, so the registry entry (the Go GC anchor) drops.
func dropTargetRelease(self uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	target, _ := comResolve(self).(*oleDropTarget)
	if target == nil {
		return 0
	}
	refs := atomic.AddInt32(&target.refs, -1)
	if refs <= 0 {
		comUnregister(self)
		return 0
	}
	return uintptr(uint32(refs))
}

// dropTargetDragEnter is IDropTarget::DragEnter (window.rs
// 1211-1275): negotiate the effect, read the CF_HDROP paths, convert
// the position to window-relative logical pixels, deliver Entered and
// inform the drag-image helper (with the screen position — see the
// file header's shadowing note).
func dropTargetDragEnter(self uintptr, pdataobj unsafe.Pointer, grfKeyState uintptr, packedPT uintptr, pdweffect unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	target, _ := comResolve(self).(*oleDropTarget)
	if target == nil {
		return eUnexpected
	}
	if pdweffect == nil {
		return ePointer
	}
	w := target.liveWindow()
	if w == nil {
		*(*uint32)(pdweffect) = dropEffectNone
		return eUnexpected
	}
	effect := *(*uint32)(pdweffect)
	screen := unpackPointl(packedPT)

	data := (*comIFace)(pdataobj)
	if data == nil {
		// pdataobj.ok()? — the pin errors out of the method.
		*(*uint32)(pdweffect) = dropEffectNone
		return ePointer
	}

	// The pin: QueryGetData(&config) == S_OK negotiates COPY and reads
	// the paths; anything else negotiates NONE without an event.
	if dataObjectHasFormat(data, configPtr()) {
		effect = dropEffectCopy
		paths, ok := dataObjectGetCFHDROP(target.host, data)
		if !ok {
			// GetData failed or the hGlobal is invalid: the pin returns
			// Ok(()) here — no event, no helper call, effect left COPY.
			*(*uint32)(pdweffect) = effect
			return sOK
		}
		clientX, clientY, _ := screenToClientPoint(w.hwnd, screen.x, screen.y)
		position := w.logicalWindowPoint(clientX, clientY)
		target.deliver(w, NewFileDropEntered(position, paths))
	} else {
		effect = dropEffectNone
	}

	// The helper's DragEnter (shell32's CDropTargetHelper): best-effort
	// like the pin's log_err, with the screen position.
	target.helperDragEnter(w.hwnd, data, screen.x, screen.y, effect)

	*(*uint32)(pdweffect) = effect
	return sOK
}

// dropTargetDragOver is IDropTarget::DragOver (window.rs
// 1277-1305): COPY effect, helper DragOver (screen position),
// ScreenToClient, Pending event.
func dropTargetDragOver(self, grfKeyState uintptr, packedPT uintptr, pdweffect unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	target, _ := comResolve(self).(*oleDropTarget)
	if target == nil {
		return eUnexpected
	}
	if pdweffect == nil {
		return ePointer
	}
	w := target.liveWindow()
	if w == nil {
		*(*uint32)(pdweffect) = dropEffectNone
		return eUnexpected
	}
	var effect uint32 = dropEffectCopy
	screen := unpackPointl(packedPT)

	// helper DragOver first (screen position), then the conversion.
	target.helperDragOver(screen.x, screen.y, effect)

	clientX, clientY, _ := screenToClientPoint(w.hwnd, screen.x, screen.y)
	position := w.logicalWindowPoint(clientX, clientY)
	target.deliver(w, NewFileDropPending(position))

	*(*uint32)(pdweffect) = effect
	return sOK
}

// dropTargetDragLeave is IDropTarget::DragLeave (window.rs
// 1307-1315): helper DragLeave, Exited event.
func dropTargetDragLeave(self uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	target, _ := comResolve(self).(*oleDropTarget)
	if target == nil {
		return eUnexpected
	}
	w := target.liveWindow()
	if w == nil {
		return eUnexpected
	}
	target.helperDragLeave()
	target.deliver(w, NewFileDropExited())
	return sOK
}

// dropTargetDrop is IDropTarget::Drop (window.rs 1317-1347): COPY
// effect, helper Drop (screen position), ScreenToClient, Submit
// event.
func dropTargetDrop(self uintptr, pdataobj unsafe.Pointer, grfKeyState uintptr, packedPT uintptr, pdweffect unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	target, _ := comResolve(self).(*oleDropTarget)
	if target == nil {
		return eUnexpected
	}
	if pdweffect == nil {
		return ePointer
	}
	w := target.liveWindow()
	if w == nil {
		*(*uint32)(pdweffect) = dropEffectNone
		return eUnexpected
	}
	var effect uint32 = dropEffectCopy
	screen := unpackPointl(packedPT)
	data := (*comIFace)(pdataobj)

	target.helperDrop(data, screen.x, screen.y, effect)

	clientX, clientY, _ := screenToClientPoint(w.hwnd, screen.x, screen.y)
	position := w.logicalWindowPoint(clientX, clientY)
	target.deliver(w, NewFileDropSubmit(position))

	*(*uint32)(pdweffect) = effect
	return sOK
}

// configPtr is the shared CF_HDROP FORMATETC instance for the
// QueryGetData/GetData calls.
var hdropConfig = hdropFormatEtc()

// configPtr returns the shared CF_HDROP FORMATETC address.
func configPtr() *formatEtc { return &hdropConfig }

// ---------------------------------------------------------------------------
// The IDropTargetHelper calls (shell32 CDropTargetHelper)
// ---------------------------------------------------------------------------

// helperDragEnter forwards to the host's helper (IDropTargetHelper::
// DragEnter, vtable slot 3) with the screen position. Best-effort: a
// failure records a fault like the pin's log_err.
func (t *oleDropTarget) helperDragEnter(hwnd uintptr, data *comIFace, x, y int32, effect uint32) {
	helper := t.host.dropTargetHelper
	if helper == nil {
		return
	}
	pt := point{x: x, y: y}
	hr, _, _ := syscall.SyscallN(helper.vtbl[3],
		uintptr(unsafe.Pointer(helper)), hwnd, uintptr(unsafe.Pointer(data)),
		uintptr(unsafe.Pointer(&pt)), uintptr(effect))
	if int32(hr) < 0 {
		t.host.recordFault(fmt.Sprintf("IDropTargetHelper::DragEnter failed: %s", hresultString(hr)))
	}
}

// helperDragOver is IDropTargetHelper::DragOver (vtable slot 5).
func (t *oleDropTarget) helperDragOver(x, y int32, effect uint32) {
	helper := t.host.dropTargetHelper
	if helper == nil {
		return
	}
	pt := point{x: x, y: y}
	hr, _, _ := syscall.SyscallN(helper.vtbl[5],
		uintptr(unsafe.Pointer(helper)), uintptr(unsafe.Pointer(&pt)), uintptr(effect))
	if int32(hr) < 0 {
		t.host.recordFault(fmt.Sprintf("IDropTargetHelper::DragOver failed: %s", hresultString(hr)))
	}
}

// helperDragLeave is IDropTargetHelper::DragLeave (vtable slot 4).
func (t *oleDropTarget) helperDragLeave() {
	helper := t.host.dropTargetHelper
	if helper == nil {
		return
	}
	hr, _, _ := syscall.SyscallN(helper.vtbl[4], uintptr(unsafe.Pointer(helper)))
	if int32(hr) < 0 {
		t.host.recordFault(fmt.Sprintf("IDropTargetHelper::DragLeave failed: %s", hresultString(hr)))
	}
}

// helperDrop is IDropTargetHelper::Drop (vtable slot 6).
func (t *oleDropTarget) helperDrop(data *comIFace, x, y int32, effect uint32) {
	helper := t.host.dropTargetHelper
	if helper == nil {
		return
	}
	pt := point{x: x, y: y}
	hr, _, _ := syscall.SyscallN(helper.vtbl[6],
		uintptr(unsafe.Pointer(helper)), uintptr(unsafe.Pointer(data)),
		uintptr(unsafe.Pointer(&pt)), uintptr(effect))
	if int32(hr) < 0 {
		t.host.recordFault(fmt.Sprintf("IDropTargetHelper::Drop failed: %s", hresultString(hr)))
	}
}

// ---------------------------------------------------------------------------
// Host integration
// ---------------------------------------------------------------------------

// createDropTargetHelper creates the platform's IDropTargetHelper once
// (platform.rs:177-184: CoCreateInstance(CLSID_DragDropHelper,
// CLSCTX_INPROC_SERVER), fatal on failure). Runs on the host thread
// after OleInitialize.
func (h *Host) createDropTargetHelper() error {
	var helper *comIFace
	hr, _, callErr := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidDragDropHelper)),
		0,
		uintptr(1), // CLSCTX_INPROC_SERVER
		uintptr(unsafe.Pointer(&iidIDropTargetHelper)),
		uintptr(unsafe.Pointer(&helper)),
	)
	if int32(hr) < 0 || helper == nil {
		if callErr == nil {
			callErr = fmt.Errorf("CoCreateInstance returned %s", hresultString(hr))
		}
		return fmt.Errorf("gpui: Error creating drop target helper: %w", callErr)
	}
	h.dropTargetHelper = helper
	return nil
}

// releaseDropTargetHelper releases the platform helper at shutdown
// (before OleUninitialize, on the owning apartment).
func (h *Host) releaseDropTargetHelper() {
	if h.dropTargetHelper == nil {
		return
	}
	comRelease(h.dropTargetHelper)
	h.dropTargetHelper = nil
}

// registerWindowDragDrop creates and registers the window's drop
// target (window.rs's register_drag_drop at creation). Runs on the
// host thread; a failure fails the window creation like the pin's `?`.
func (h *Host) registerWindowDragDrop(w *hostWindow) error {
	target, err := newOleDropTarget(w)
	if err != nil {
		return err
	}
	w.dropTarget = target
	return nil
}

// revokeWindowDragDrop revokes the window's drop registration exactly
// once (window.rs's Drop: RevokeDragDrop before DestroyWindow, on the
// foreground thread). WM_DESTROY runs it before the record retires at
// WM_NCDESTROY.
func (w *hostWindow) revokeDragDrop() {
	if w.dropTarget == nil {
		return
	}
	w.dropTarget.revoke()
}

// ---------------------------------------------------------------------------
// The OLE source adapter: a Go IDataObject carrying CF_HDROP
// (objidl.h IDataObject vtable order)
// ---------------------------------------------------------------------------

// goDataObject is a Go-owned IDataObject whose CF_HDROP arm carries a
// DROPFILES global with the given paths. GetData answers the CF_HDROP
// query (a fresh GlobalAlloc'd medium the caller releases through
// ReleaseStgMedium); QueryGetData reports CF_HDROP availability; the
// other arms return E_NOTIMPL (the pin's source surface never needs
// them — the shell data objects provide them).
type goDataObject struct {
	vtbl *dataObjectVtable

	host  *Host
	paths []string
	refs  int32
}

// dataObjectVtable is the IDataObject vtable (objidl.h).
type dataObjectVtable struct {
	queryInterface        uintptr
	addRef                uintptr
	release               uintptr
	getData               uintptr
	getDataHere           uintptr
	queryGetData          uintptr
	getCanonicalFormatEtc uintptr
	setData               uintptr
	enumFormatEtc         uintptr
	dAdvise               uintptr
	dUnadvise             uintptr
	enumDAdvise           uintptr
}

var (
	dataObjectVtableOnce sync.Once
	dataObjectVtableVal  dataObjectVtable
)

func dataObjectVtableGet() *dataObjectVtable {
	dataObjectVtableOnce.Do(func() {
		dataObjectVtableVal = dataObjectVtable{
			queryInterface:        syscall.NewCallback(dataObjectQueryInterface),
			addRef:                syscall.NewCallback(dataObjectAddRef),
			release:               syscall.NewCallback(dataObjectRelease),
			getData:               syscall.NewCallback(dataObjectGetData),
			getDataHere:           syscall.NewCallback(dataObjectNotImpl3),
			queryGetData:          syscall.NewCallback(goDataObjectQueryGetData),
			getCanonicalFormatEtc: syscall.NewCallback(dataObjectGetCanonicalFormatEtc),
			setData:               syscall.NewCallback(dataObjectNotImpl4),
			enumFormatEtc:         syscall.NewCallback(dataObjectNotImpl3),
			dAdvise:               syscall.NewCallback(dataObjectNotImpl5),
			dUnadvise:             syscall.NewCallback(dataObjectNotImpl2),
			enumDAdvise:           syscall.NewCallback(dataObjectNotImpl2),
		}
	})
	return &dataObjectVtableVal
}

// newGoDataObject creates a registered CF_HDROP data object.
func newGoDataObject(h *Host, paths []string) *goDataObject {
	obj := &goDataObject{
		vtbl:  dataObjectVtableGet(),
		host:  h,
		paths: paths,
	}
	comRegister(comSelf(obj), obj)
	return obj
}

// dropFilesGlobal allocates a double-NUL-terminated wide-char
// DROPFILES global (shellapi.h) for the paths.
func dropFilesGlobal(paths []string) (uintptr, error) {
	// DROPFILES: DWORD pFiles; POINT pt; BOOL fNC; BOOL fWide.
	buf := make([]byte, 0, 20+64*len(paths))
	buf = append(buf, 20, 0, 0, 0)            // pFiles
	buf = append(buf, 0, 0, 0, 0, 0, 0, 0, 0) // pt
	buf = append(buf, 0, 0, 0, 0)             // fNC = FALSE
	buf = append(buf, 1, 0, 0, 0)             // fWide = TRUE
	for _, path := range paths {
		for _, unit := range syscall.StringToUTF16(path) {
			buf = append(buf, byte(unit), byte(unit>>8))
		}
		// Each path is NUL-terminated; the list's final NUL doubles as
		// the terminator.
	}
	buf = append(buf, 0, 0)
	global, _, callErr := procGlobalAlloc.Call(uintptr(gmemMoveable), uintptr(len(buf)))
	if global == 0 {
		return 0, fmt.Errorf("gpui: GlobalAlloc for DROPFILES: %w", callErr)
	}
	ptr, _, _ := procGlobalLock.Call(global)
	if ptr == 0 {
		_, _, _ = procGlobalFree.Call(global)
		return 0, fmt.Errorf("gpui: GlobalLock for DROPFILES failed")
	}
	procRtlMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	procGlobalUnlock.Call(global)
	return global, nil
}

// dataObjectQueryInterface supports IUnknown and IDataObject.
func dataObjectQueryInterface(self uintptr, riid, ppv unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*goDataObject)
	if obj == nil || ppv == nil {
		return ePointer
	}
	out := (*uintptr)(ppv)
	*out = 0
	iid := (*guid)(riid)
	if *iid == iidIUnknown || *iid == iidIDataObject {
		*out = self
		atomic.AddInt32(&obj.refs, 1)
		return sOK
	}
	return eNoInterface
}

// dataObjectAddRef is IUnknown::AddRef.
func dataObjectAddRef(self uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*goDataObject)
	if obj == nil {
		return 0
	}
	return uintptr(uint32(atomic.AddInt32(&obj.refs, 1)))
}

// dataObjectRelease is IUnknown::Release.
func dataObjectRelease(self uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*goDataObject)
	if obj == nil {
		return 0
	}
	refs := atomic.AddInt32(&obj.refs, -1)
	if refs <= 0 {
		comUnregister(self)
		return 0
	}
	return uintptr(uint32(refs))
}

// dataObjectGetData is IDataObject::GetData for the CF_HDROP arm: a
// fresh TYMED_HGLOBAL medium the caller releases with
// ReleaseStgMedium.
func dataObjectGetData(self uintptr, pformatetc, pmedium unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*goDataObject)
	if obj == nil || pformatetc == nil || pmedium == nil {
		return ePointer
	}
	config := (*formatEtc)(pformatetc)
	if config.cfFormat != cfHDROP || config.dwAspect != dvaspectContent ||
		config.tymed != tymedHGlobal {
		return eNotImpl // DV_E_FORMATETC-shaped refusal
	}
	global, err := dropFilesGlobal(obj.paths)
	if err != nil {
		obj.host.recordFault(fmt.Sprintf("go data object GetData: %v", err))
		return eFail
	}
	medium := (*stgMedium)(pmedium)
	medium.tymed = tymedHGlobal
	medium.hGlobal = global
	return sOK
}

// goDataObjectQueryGetData is IDataObject::QueryGetData: CF_HDROP is
// available (a nil path list models a data object without the
// CF_HDROP format, so the negotiation reports the miss).
func goDataObjectQueryGetData(self uintptr, pformatetc unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*goDataObject)
	if obj == nil || pformatetc == nil {
		return ePointer
	}
	if obj.paths == nil {
		return eNotImpl // the format is not offered
	}
	config := (*formatEtc)(pformatetc)
	if config.cfFormat == cfHDROP && config.tymed == tymedHGlobal {
		return sOK
	}
	return eNotImpl // DV_E_FORMATETC-shaped miss
}

// dataObjectGetCanonicalFormatEtc mirrors the standard passthrough.
func dataObjectGetCanonicalFormatEtc(self uintptr, pformatetc, pcanonical unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	if pcanonical != nil {
		*(*uintptr)(pcanonical) = 0
	}
	return eNotImpl
}

// dataObjectNotImpl2/3/4/5 answer the unsupported IDataObject arms
// with E_NOTIMPL at each slot's arity (syscall.NewCallback requires
// fixed signatures).
func dataObjectNotImpl2(self, a uintptr) uintptr { return eNotImpl }

func dataObjectNotImpl3(self, a, b uintptr) uintptr { return eNotImpl }

func dataObjectNotImpl4(self, a, b, c uintptr) uintptr { return eNotImpl }

func dataObjectNotImpl5(self, a, b, c, d uintptr) uintptr { return eNotImpl }

// comPointer returns the IDataObject interface pointer (for syscall
// arguments; the caller must AddRef when retaining it).
func (o *goDataObject) comPointer() uintptr { return comSelf(o) }

// release drops one Go-side reference (the trampoline Release).
func (o *goDataObject) release() {
	dataObjectRelease(comSelf(o))
}

// ---------------------------------------------------------------------------
// The OLE source adapter: a Go IDropSource
// ---------------------------------------------------------------------------

// goDropSource is a Go-owned IDropSource controlling an OLE drag loop:
// QueryContinueDrag decides when the drag ends and GiveFeedback uses
// the default cursors.
type goDropSource struct {
	vtbl *dropSourceVtable

	host *Host
	refs int32
	// dropOnNextQuery ends the drag with DRAGDROP_S_DROP at the next
	// QueryContinueDrag (deterministic loop termination for drivers).
	dropOnNextQuery atomic.Bool
	// dropWhenReleased ends the drag at the first QueryContinueDrag
	// after the button was held and released (the driver's physical
	// sequence: press, moves, release).
	dropWhenReleased atomic.Bool
	// sawButton records a QueryContinueDrag with the button held.
	sawButton atomic.Bool
	// closedPosted guards the window-closure-during-loop seam's single
	// WM_CLOSE post.
	closedPosted atomic.Bool
	// ignoreButtonState suppresses the no-button-drop rule: the counted
	// termination owns the loop's end (the drag loop starts before any
	// button is physically held).
	ignoreButtonState atomic.Bool
	// closeHwnd, when set, is the window a GiveFeedback posts WM_CLOSE
	// to (the window-closure-during-OLE-loop test seam; 0 = none).
	closeHwnd uintptr
}

// dropSourceVtable is the IDropSource vtable (oleidl.h).
type dropSourceVtable struct {
	queryInterface    uintptr
	addRef            uintptr
	release           uintptr
	queryContinueDrag uintptr
	giveFeedback      uintptr
}

var (
	dropSourceVtableOnce sync.Once
	dropSourceVtableVal  dropSourceVtable
)

func dropSourceVtableGet() *dropSourceVtable {
	dropSourceVtableOnce.Do(func() {
		dropSourceVtableVal = dropSourceVtable{
			queryInterface:    syscall.NewCallback(dropSourceQueryInterface),
			addRef:            syscall.NewCallback(dropSourceAddRef),
			release:           syscall.NewCallback(dropSourceRelease),
			queryContinueDrag: syscall.NewCallback(dropSourceQueryContinueDrag),
			giveFeedback:      syscall.NewCallback(dropSourceGiveFeedback),
		}
	})
	return &dropSourceVtableVal
}

// newGoDropSource creates a registered drop source.
func newGoDropSource(h *Host) *goDropSource {
	obj := &goDropSource{
		vtbl: dropSourceVtableGet(),
		host: h,
	}
	comRegister(comSelf(obj), obj)
	return obj
}

// DropOnNextQuery arms the deterministic termination.
func (s *goDropSource) DropOnNextQuery() { s.dropOnNextQuery.Store(true) }

// DropWhenButtonReleased arms the press-moves-release termination: the
// drag continues (S_OK) through the no-button start and the held
// queries, and ends with DRAGDROP_S_DROP at the first released query
// after the button was held — so OLE delivers the DragEnter/DragOver
// calls the injected input drives before the drop ends the loop.
func (s *goDropSource) DropWhenButtonReleased() {
	s.dropWhenReleased.Store(true)
	s.ignoreButtonState.Store(true)
}

// comPointer returns the IDropSource interface pointer.
func (s *goDropSource) comPointer() uintptr { return comSelf(s) }

// release drops one Go-side reference.
func (s *goDropSource) release() { dropSourceRelease(comSelf(s)) }

func dropSourceQueryInterface(self uintptr, riid, ppv unsafe.Pointer) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*goDropSource)
	if obj == nil || ppv == nil {
		return ePointer
	}
	out := (*uintptr)(ppv)
	*out = 0
	iid := (*guid)(riid)
	if *iid == iidIUnknown || *iid == iidIDropSource {
		*out = self
		atomic.AddInt32(&obj.refs, 1)
		return sOK
	}
	return eNoInterface
}

func dropSourceAddRef(self uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*goDropSource)
	if obj == nil {
		return 0
	}
	return uintptr(uint32(atomic.AddInt32(&obj.refs, 1)))
}

func dropSourceRelease(self uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*goDropSource)
	if obj == nil {
		return 0
	}
	refs := atomic.AddInt32(&obj.refs, -1)
	if refs <= 0 {
		comUnregister(self)
		return 0
	}
	return uintptr(uint32(refs))
}

// dropSourceQueryContinueDrag is IDropSource::QueryContinueDrag: the
// armed driver ends the drag with DRAGDROP_S_DROP; the Escape key
// cancels; a released left button drops (the default COM rules).
func dropSourceQueryContinueDrag(self, fEscape, grfKeyState uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	obj, _ := comResolve(self).(*goDropSource)
	if obj == nil {
		return eUnexpected
	}
	if fEscape != 0 {
		return dragDropSCancel
	}
	if obj.dropOnNextQuery.Load() {
		return dragDropSDrop
	}
	// MK_LBUTTON (0x0001) held: remember it for the release rule.
	if grfKeyState&0x0001 != 0 {
		obj.sawButton.Store(true)
		return sOK
	}
	// Released: the default rule drops; the armed driver drops only
	// after the button was held (the press-moves-release sequence).
	if obj.dropWhenReleased.Load() {
		if obj.sawButton.Load() {
			return dragDropSDrop
		}
		return sOK
	}
	if !obj.ignoreButtonState.Load() {
		return dragDropSDrop
	}
	return sOK
}

// dropSourceGiveFeedback is IDropSource::GiveFeedback: use the
// default drag cursors.
func dropSourceGiveFeedback(self, dwEffect uintptr) (result uintptr) {
	defer func() {
		if r := recover(); r != nil {
			result = comTrampolineResult(self, r)
		}
	}()
	return dragDropSUseDefaultCursors
}

// ---------------------------------------------------------------------------
// Public driver surface (deterministic native-interaction replay)
// ---------------------------------------------------------------------------

// OLEDragData is a Go-owned OLE data object carrying a CF_HDROP file
// list (the drag data a shell drag or a test provides). It exists to
// drive and verify the native drop lifecycle; the pinned Windows
// platform itself never starts an external drag (the platform.rs
// trait defaults are false, see Window.CanStartExternalDrag).
type OLEDragData struct {
	obj *goDataObject
}

// NewOLEDragData creates the data object on the host (STA) thread.
// The paths are delivered verbatim through DragQueryFileW.
func NewOLEDragData(h *Host, paths []string) *OLEDragData {
	return &OLEDragData{obj: newGoDataObject(h, paths)}
}

// Release drops the construction reference (the COM refcount reaches
// zero when OLE released its references).
func (d *OLEDragData) Release() {
	if d == nil || d.obj == nil {
		return
	}
	d.obj.release()
	d.obj = nil
}

// OLEDropSource drives an OLE drag loop's continuation
// (IDropSource).
type OLEDropSource struct {
	obj *goDropSource
}

// NewOLEDropSource creates the drop source on the host (STA) thread.
func NewOLEDropSource(h *Host) *OLEDropSource {
	return &OLEDropSource{obj: newGoDropSource(h)}
}

// DropOnNextQuery arms the deterministic termination: the next
// QueryContinueDrag ends the drag with a drop at the current cursor.
func (s *OLEDropSource) DropOnNextQuery() {
	if s == nil || s.obj == nil {
		return
	}
	s.obj.DropOnNextQuery()
}

// Release drops the construction reference.
func (s *OLEDropSource) Release() {
	if s == nil || s.obj == nil {
		return
	}
	s.obj.release()
	s.obj = nil
}

// DoDragDropResult is the native drag outcome: the loop's terminal
// status (DRAGDROP_S_DROP / DRAGDROP_S_CANCEL when the loop ended
// through the source) and the effect the target negotiated.
type DoDragDropResult struct {
	// Status is DoDragDrop's HRESULT (read as uint32; the
	// DRAGDROP_S_USEDEFAULTCURSORS prefix bits never appear here).
	Status uint32
	// FinalEffect is the effect the drop negotiated (DROPEFFECT_NONE
	// when the drag was cancelled or nothing accepted it).
	FinalEffect uint32
}

// DoDragDrop runs the native OLE drag loop on the host (STA) thread
// with the given data object and drop source (ole32 DoDragDrop). The
// loop pumps messages until the source ends it, exactly like a
// shell-initiated drag; the registered drop targets receive the real
// DragEnter/DragOver/Drop calls through OLE's hit testing.
func DoDragDrop(h *Host, data *OLEDragData, source *OLEDropSource, allowedEffects uint32) (DoDragDropResult, error) {
	if h == nil || data == nil || data.obj == nil || source == nil || source.obj == nil {
		return DoDragDropResult{}, fmt.Errorf("gpui: DoDragDrop requires a host, a data object and a drop source")
	}
	var finalEffect uint32
	hr, _, callErr := procDoDragDrop.Call(
		data.obj.comPointer(),
		source.obj.comPointer(),
		uintptr(allowedEffects),
		uintptr(unsafe.Pointer(&finalEffect)),
	)
	if int32(hr) < 0 {
		return DoDragDropResult{}, fmt.Errorf("gpui: DoDragDrop failed: %v (HRESULT %s)", callErr, hresultString(hr))
	}
	return DoDragDropResult{Status: uint32(hr), FinalEffect: finalEffect}, nil
}

// FileDropDriver deterministically replays the shell's IDropTarget
// calls against a window's registered drop target: the same COM
// entry points OLE invokes, driven through the vtable with a
// synthesized CF_HDROP data object, so positions, event order and
// effects follow the pinned semantics without a physical mouse.
type FileDropDriver struct {
	host   *Host
	hwnd   uintptr
	window uint64
	target *oleDropTarget
}

// FileDropDriver returns the driver for this window's registered drop
// target (nil when the lease is stale or the window has none).
func (wh WindowHandle) FileDropDriver() *FileDropDriver {
	if wh.host == nil {
		return nil
	}
	driver, err := queryForeground(wh.host, func() (*FileDropDriver, error) {
		w := wh.record()
		if w == nil || w.dropTarget == nil {
			return nil, nil
		}
		return &FileDropDriver{host: wh.host, hwnd: wh.hwnd, window: w.id, target: w.dropTarget}, nil
	})
	if err != nil || driver == nil {
		return nil
	}
	return driver
}

// live resolves the target generation-checked (a destroyed window's
// driver refuses the call).
func (d *FileDropDriver) live() *oleDropTarget {
	w := windowFor(d.hwnd)
	if w == nil || w.host != d.host || w.id != d.window {
		return nil
	}
	return d.target
}

// dataObject builds a transient CF_HDROP data object for the replay.
func (d *FileDropDriver) dataObject(paths []string) *goDataObject {
	return newGoDataObject(d.host, paths)
}

// call runs one vtable slot with the arguments the shell would pass.
func (d *FileDropDriver) call(slot int, args ...uintptr) uintptr {
	fn := (*[7]uintptr)(unsafe.Pointer(d.target.vtbl))[slot]
	callArgs := append([]uintptr{comSelf(d.target)}, args...)
	hr, _, _ := syscall.SyscallN(fn, callArgs...)
	return hr
}

// DragEnter replays IDropTarget::DragEnter with a CF_HDROP data
// object at the screen position; it returns the negotiated effect.
func (d *FileDropDriver) DragEnter(paths []string, x, y int32) uint32 {
	target := d.live()
	if target == nil {
		return dropEffectNone
	}
	data := d.dataObject(paths)
	defer data.release()
	effect := uint32(dropEffectCopy | dropEffectMove | dropEffectLink)
	d.call(3, data.comPointer(), 0, packPointl(x, y), uintptr(unsafe.Pointer(&effect)))
	return effect
}

// DragOver replays IDropTarget::DragOver; it returns the negotiated
// effect.
func (d *FileDropDriver) DragOver(x, y int32) uint32 {
	target := d.live()
	if target == nil {
		return dropEffectNone
	}
	effect := uint32(dropEffectCopy | dropEffectMove | dropEffectLink)
	d.call(4, 0, packPointl(x, y), uintptr(unsafe.Pointer(&effect)))
	return effect
}

// DragLeave replays IDropTarget::DragLeave.
func (d *FileDropDriver) DragLeave() error {
	target := d.live()
	if target == nil {
		return fmt.Errorf("gpui: the drop target's window lease is gone")
	}
	if hr := d.call(5); int32(hr) < 0 {
		return fmt.Errorf("gpui: DragLeave returned %s", hresultString(hr))
	}
	return nil
}

// Drop replays IDropTarget::Drop with a CF_HDROP data object at the
// screen position; it returns the negotiated effect.
func (d *FileDropDriver) Drop(paths []string, x, y int32) uint32 {
	target := d.live()
	if target == nil {
		return dropEffectNone
	}
	data := d.dataObject(paths)
	defer data.release()
	effect := uint32(dropEffectCopy | dropEffectMove | dropEffectLink)
	d.call(6, data.comPointer(), 0, packPointl(x, y), uintptr(unsafe.Pointer(&effect)))
	return effect
}
