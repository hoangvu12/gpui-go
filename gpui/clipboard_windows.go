//go:build windows

package gpui

// Ticket23's Win32 clipboard, ported from the pinned CE
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a,
// crates/gpui_windows/src/clipboard.rs (the whole module):
//
//   - the registered formats: "GPUI internal text hash", "GPUI internal
//     metadata", "image/svg+xml", "GIF", "PNG", "JFIF" (LazyLock
//     statics; registration failure panics in the reference).
//   - write_to_clipboard: OpenClipboard -> EmptyClipboard -> per-entry
//     writes; String writes CF_UNICODETEXT plus the hash+metadata pair
//     when metadata exists; Image writes its native registered format
//     plus a PNG compatibility copy (SVG skipped); ExternalPaths is
//     skipped on write.
//   - read_from_clipboard: the EnumClipboardFormats walk taking at most
//     one text entry (CF_UNICODETEXT), one image entry (the registered
//     image formats or CF_DIB) and one files entry (CF_HDROP).
//   - CF_DIB -> BMP by prepending the 14-byte BITMAPFILEHEADER.
//   - read_clipboard_metadata gated on the text-hash match.
//   - ClipboardGuard (OpenClipboard/CloseClipboard exactly once) and
//     LockedGlobal (GlobalSize/GlobalLock/GlobalUnlock once, buffers
//     copied before unlock/close).
//   - set_clipboard_bytes: GMEM_MOVEABLE GlobalAlloc, lock, copy,
//     unlock, SetClipboardData — ownership passes to the system on
//     success; a failed set frees locally.
//   - with_file_names: the DragQueryFileW walk shared with drag/drop.
//
// The Windows platform delegates straight here
// (crates/gpui_windows/src/platform.rs:821-827); its test_clipboard
// corpus (~1577: CJK "你好，我是张小白", "12345", JSON metadata) is
// mirrored in internal/clipspec.
//
// Threading: the reference performs plain Win32 calls on the platform
// (foreground) thread with NO wndproc clipboard interception; the Host
// methods marshal through the wake mechanism (queryForeground), so
// OpenClipboard..CloseClipboard pairs and the OLE apartment discipline
// stay on the host thread.
//
// Pure Go: every Win32 call goes through the stdlib syscall package
// (CGO_ENABLED=0). The PNG compatibility copy decodes through
// ticket17's native image service (the ImageCodec's clipboard entry
// seam) and re-encodes with stdlib image/png.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"sync"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Win32 bindings (ticket23's own set; names distinct from the other
// platform slices so concurrent tickets cannot collide)
// ---------------------------------------------------------------------------

var (
	procOpenClipboard          = modUser32.NewProc("OpenClipboard")
	procCloseClipboard         = modUser32.NewProc("CloseClipboard")
	procEmptyClipboard         = modUser32.NewProc("EmptyClipboard")
	procSetClipboardData       = modUser32.NewProc("SetClipboardData")
	procGetClipboardData       = modUser32.NewProc("GetClipboardData")
	procCountClipboardFormats  = modUser32.NewProc("CountClipboardFormats")
	procEnumClipboardFormats   = modUser32.NewProc("EnumClipboardFormats")
	procGetClipboardFormatName = modUser32.NewProc("GetClipboardFormatNameW")
	procRegisterClipboardFmt   = modUser32.NewProc("RegisterClipboardFormatW")

	procGlobalAlloc  = modKernel32.NewProc("GlobalAlloc")
	procGlobalFree   = modKernel32.NewProc("GlobalFree")
	procGlobalLock   = modKernel32.NewProc("GlobalLock")
	procGlobalUnlock = modKernel32.NewProc("GlobalUnlock")
	procGlobalSize   = modKernel32.NewProc("GlobalSize")
	// RtlMoveMemory copies between a locked global and a Go buffer. The
	// locked pointer stays a raw uintptr syscall argument (the
	// vet-safe direction: Go pointers cross as unsafe.Pointer, Win32
	// pointers never convert back).
	procRtlMoveMemory = modKernel32.NewProc("RtlMoveMemory")

	procDragQueryFileW = modShell32.NewProc("DragQueryFileW")
)

// Win32 constants (winuser.h, wingdi.h, shellapi.h).
const (
	cfDIB         = 8  // CF_DIB
	cfUnicodeText = 13 // CF_UNICODETEXT
	cfHDROP       = 15 // CF_HDROP

	gmemMoveable = 0x0002 // GMEM_MOVEABLE

	// dragDropGetFilesCount is DragQueryFileW's FFFFFFFF "count" index
	// (clipboard.rs DRAGDROP_GET_FILES_COUNT).
	dragDropGetFilesCount = 0xFFFFFFFF
)

// ---------------------------------------------------------------------------
// Registered clipboard formats (clipboard.rs LazyLock statics)
// ---------------------------------------------------------------------------

var (
	clipboardFormatsOnce sync.Once

	clipboardHashFormat     uint32 // "GPUI internal text hash"
	clipboardMetadataFormat uint32 // "GPUI internal metadata"
	clipboardSVGFormat      uint32 // "image/svg+xml"
	clipboardGIFFormat      uint32 // "GIF"
	clipboardPNGFormat      uint32 // "PNG"
	clipboardJPGFormat      uint32 // "JFIF"

	// clipboardImageFormats mirrors clipboard.rs IMAGE_FORMATS_MAP; it
	// is built inside the formats' once, after registration.
	clipboardImageFormats map[uint32]ImageFormat
)

// ensureClipboardRegisteredFormats registers the six pinned formats once
// per process (the LazyLock statics). Like the reference's
// register_clipboard_format, a registration failure panics — the
// reference aborts at LazyLock initialization; RegisterClipboardFormatW
// with these fixed names only fails on resource exhaustion.
func ensureClipboardRegisteredFormats() {
	clipboardFormatsOnce.Do(func() {
		clipboardHashFormat = mustRegisterClipboardFormat("GPUI internal text hash")
		clipboardMetadataFormat = mustRegisterClipboardFormat("GPUI internal metadata")
		clipboardSVGFormat = mustRegisterClipboardFormat("image/svg+xml")
		clipboardGIFFormat = mustRegisterClipboardFormat("GIF")
		clipboardPNGFormat = mustRegisterClipboardFormat("PNG")
		clipboardJPGFormat = mustRegisterClipboardFormat("JFIF")
		clipboardImageFormats = map[uint32]ImageFormat{
			clipboardPNGFormat: ImageFormatPNG,
			clipboardGIFFormat: ImageFormatGIF,
			clipboardJPGFormat: ImageFormatJPEG,
			clipboardSVGFormat: ImageFormatSVG,
		}
	})
}

// mustRegisterClipboardFormat is clipboard.rs register_clipboard_format.
func mustRegisterClipboardFormat(name string) uint32 {
	formatName, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		panic(fmt.Sprintf("gpui: Error when registering clipboard format: %v", err))
	}
	ret, _, callErr := procRegisterClipboardFmt.Call(uintptr(unsafe.Pointer(formatName)))
	if ret == 0 {
		panic(fmt.Sprintf("gpui: Error when registering clipboard format: %v", callErr))
	}
	return uint32(ret)
}

// isClipboardImageFormat is clipboard.rs is_image_format: the registered
// image formats or CF_DIB.
func isClipboardImageFormat(format uint32) bool {
	_, known := clipboardImageFormats[format]
	return known || format == cfDIB
}

// ---------------------------------------------------------------------------
// ClipboardGuard (clipboard.rs): open exactly once, close exactly once
// ---------------------------------------------------------------------------

// clipboardGuard is the OpenClipboard/CloseClipboard RAII guard
// (clipboard.rs ClipboardGuard). close runs through defer at every
// exit, including partial failures, so the clipboard is never left open
// by this port.
type clipboardGuard struct{}

// openClipboardGuard opens the clipboard with a NULL owner
// (OpenClipboard(None)). A busy clipboard — another window holds it —
// fails here; the reference logs and gives up, the Go port surfaces the
// typed Denied error (see Host.ReadClipboard/WriteClipboard).
func openClipboardGuard() (*clipboardGuard, error) {
	opened, _, callErr := procOpenClipboard.Call(0)
	if opened == 0 {
		return nil, &ClipboardReadError{
			Kind:    ClipboardReadDenied,
			Message: fmt.Sprintf("OpenClipboard failed: %v", callErr),
		}
	}
	return &clipboardGuard{}, nil
}

// close closes the clipboard (ClipboardGuard::drop). A close failure is
// recorded as a host soft fault like the reference's error log.
func (g *clipboardGuard) close(h *Host) {
	closed, _, callErr := procCloseClipboard.Call()
	if closed == 0 && h != nil {
		h.recordFault(fmt.Sprintf("clipboard CloseClipboard failed: %v", callErr))
	}
}

// ---------------------------------------------------------------------------
// LockedGlobal (clipboard.rs): GlobalSize/GlobalLock/GlobalUnlock once
// ---------------------------------------------------------------------------

// lockedGlobal is one clipboard-owned global locked for reading
// (clipboard.rs LockedGlobal): GlobalSize then GlobalLock; unlock
// exactly once. Buffers are copied before unlock (platform contract:
// "Read buffers are copied before unlock/close").
type lockedGlobal struct {
	global uintptr
	ptr    uintptr
	size   int
}

// lockClipboardGlobal locks one clipboard HGLOBAL (get_clipboard_data /
// LockedGlobal::lock). A failed lock returns nil.
func lockClipboardGlobal(global uintptr) *lockedGlobal {
	size, _, _ := procGlobalSize.Call(global)
	ptr, _, _ := procGlobalLock.Call(global)
	if ptr == 0 {
		return nil
	}
	return &lockedGlobal{global: global, ptr: ptr, size: int(size)}
}

// unlock releases the lock (LockedGlobal::drop); the reference ignores
// the return, so does the port.
func (lg *lockedGlobal) unlock() {
	procGlobalUnlock.Call(lg.global)
}

// bytes copies the locked bytes out (before unlock): RtlMoveMemory
// from the locked global into a Go buffer.
func (lg *lockedGlobal) bytes() []byte {
	if lg.size == 0 {
		return nil
	}
	out := make([]byte, lg.size)
	procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&out[0])), lg.ptr, uintptr(lg.size))
	return out
}

// utf16Words exposes the locked bytes as native UTF-16 code units.
func (lg *lockedGlobal) utf16Words() []uint16 {
	data := lg.bytes()
	words := make([]uint16, len(data)/2)
	for i := range words {
		words[i] = binary.NativeEndian.Uint16(data[i*2:])
	}
	return words
}

// ---------------------------------------------------------------------------
// The Host surface (platform.rs:821-827 delegates straight to the module)
// ---------------------------------------------------------------------------

// ReadClipboard reads the system clipboard (Platform::
// read_from_clipboard -> clipboard.rs read_from_clipboard). The whole
// read — open, the format walk, the copies, close — runs on the host
// (foreground) thread through the wake marshal, matching the
// reference's plain Win32 calls on the platform thread.
//
// A nil item with a nil error is the reference's None outcome: no
// readable content (or only formats this port does not take). The busy
// clipboard is the typed *ClipboardReadError (Denied); a stopped host
// reports ErrHostStopped.
func (h *Host) ReadClipboard() (*ClipboardItem, error) {
	return queryForeground(h, func() (*ClipboardItem, error) {
		return h.readClipboardOnHostThread()
	})
}

// WriteClipboard writes the item to the system clipboard
// (Platform::write_to_clipboard -> clipboard.rs write_to_clipboard) on
// the host (foreground) thread. The reference logs failures and
// returns; the Go port surfaces them typed (busy clipboard as
// *ClipboardReadError (Denied), Win32 failures wrapped).
func (h *Host) WriteClipboard(item ClipboardItem) error {
	_, err := queryForeground(h, func() (struct{}, error) {
		return struct{}{}, h.writeClipboardOnHostThread(item)
	})
	return err
}

// ---------------------------------------------------------------------------
// write_to_clipboard (clipboard.rs)
// ---------------------------------------------------------------------------

// writeClipboardOnHostThread is clipboard.rs write_to_clipboard, minus
// the reference's log-only failure handling (errors propagate to the
// caller here). Runs on the host thread.
func (h *Host) writeClipboardOnHostThread(item ClipboardItem) error {
	ensureClipboardRegisteredFormats()
	guard, err := openClipboardGuard()
	if err != nil {
		return err
	}
	defer guard.close(h)

	if emptied, _, callErr := procEmptyClipboard.Call(); emptied == 0 {
		return fmt.Errorf("gpui: EmptyClipboard: %w", callErr)
	}
	for i := range item.Entries {
		entry := &item.Entries[i]
		switch entry.Kind {
		case ClipboardEntryString:
			if err := writeClipboardString(&entry.String); err != nil {
				return err
			}
		case ClipboardEntryImage:
			if err := writeClipboardImage(h, &entry.Image); err != nil {
				return err
			}
		case ClipboardEntryExternalPaths:
			// ClipboardEntry::ExternalPaths(_) => {}: skipped on write
			// (file lists enter the clipboard through drag/drop's shell
			// data object, not the write path).
		}
	}
	return nil
}

// writeClipboardString is clipboard.rs write_string: CF_UNICODETEXT,
// then — when metadata exists — the 8-byte native-endian text hash in
// the hash format and the metadata as UTF-16 in the metadata format.
func writeClipboardString(value *ClipboardString) error {
	text := utf16.Encode([]rune(value.Text))
	text = append(text, 0)
	if err := setClipboardDataBytes(utf16Bytes(text), cfUnicodeText); err != nil {
		return err
	}
	if value.Metadata == "" {
		return nil
	}
	hashBytes := make([]byte, 8)
	binary.NativeEndian.PutUint64(hashBytes, textHash(value.Text))
	if err := setClipboardDataBytes(hashBytes, clipboardHashFormat); err != nil {
		return err
	}
	metadata := utf16.Encode([]rune(value.Metadata))
	metadata = append(metadata, 0)
	return setClipboardDataBytes(utf16Bytes(metadata), clipboardMetadataFormat)
}

// writeClipboardImage is clipboard.rs write_image: the native
// registered format for SVG/GIF/PNG/JPEG, plus a PNG compatibility
// copy for the other decodable formats (SVG is skipped — it cannot be
// rasterized here).
func writeClipboardImage(h *Host, img *Image) error {
	var nativeFormat uint32
	switch img.Format {
	case ImageFormatSVG:
		nativeFormat = clipboardSVGFormat
	case ImageFormatGIF:
		nativeFormat = clipboardGIFFormat
	case ImageFormatPNG:
		nativeFormat = clipboardPNGFormat
	case ImageFormatJPEG:
		nativeFormat = clipboardJPGFormat
	default:
		// ImageFormat::Webp/Bmp/Tiff/Ico/Pnm => None: no registered
		// native format; only the PNG copy below can publish them.
	}
	if nativeFormat != 0 {
		if err := setClipboardDataBytes(img.Bytes, nativeFormat); err != nil {
			return err
		}
	}
	// Also provide a PNG copy for broad compatibility; skip when the
	// native format IS the PNG copy and skip SVG.
	if img.Format != ImageFormatSVG && nativeFormat != clipboardPNGFormat {
		if pngBytes, ok := convertClipboardImageToPNG(h, img); ok {
			if err := setClipboardDataBytes(pngBytes, clipboardPNGFormat); err != nil {
				return err
			}
		}
	}
	return nil
}

// convertClipboardImageToPNG is clipboard.rs convert_to_png +
// gpui_to_image_format: decode through the native image codec (the
// ImageCodec clipboard-entry seam; the first frame of an animation,
// like image::load_from_memory_with_format) and re-encode as PNG. A
// decode failure skips the copy with a recorded fault — exactly the
// reference's warn-and-skip — so the native format still publishes.
func convertClipboardImageToPNG(h *Host, img *Image) ([]byte, bool) {
	// gpui_to_image_format: Png/Jpeg/Webp/Gif/Bmp/Tiff have codec
	// equivalents; other formats warn and skip.
	switch img.Format {
	case ImageFormatPNG, ImageFormatJPEG, ImageFormatWebP,
		ImageFormatGIF, ImageFormatBMP, ImageFormatTIFF:
	default:
		h.recordFault(fmt.Sprintf("clipboard PNG copy skipped: no image codec for format %s", img.Format))
		return nil, false
	}
	svc, err := imageService()
	if err != nil {
		h.recordFault(fmt.Sprintf("clipboard PNG copy skipped: image service: %v", err))
		return nil, false
	}
	handle, err := svc.DecodeClipboard(img.Bytes, img.Format)
	if err != nil {
		h.recordFault(fmt.Sprintf("clipboard PNG copy failed to decode image: %v", err))
		return nil, false
	}
	defer func() { _ = svc.Dispose(handle) }()
	count, err := svc.FrameCount(handle)
	if err != nil || count == 0 {
		h.recordFault(fmt.Sprintf("clipboard PNG copy: no frames (count %d, err %v)", count, err))
		return nil, false
	}
	info, err := svc.FrameInfo(handle, 0)
	if err != nil {
		h.recordFault(fmt.Sprintf("clipboard PNG copy: frame info: %v", err))
		return nil, false
	}
	pixels, err := svc.FramePixels(handle, 0)
	if err != nil {
		h.recordFault(fmt.Sprintf("clipboard PNG copy: frame pixels: %v", err))
		return nil, false
	}
	if len(pixels) != int(info.Width)*int(info.Height)*4 {
		h.recordFault("clipboard PNG copy: frame pixel length mismatch")
		return nil, false
	}
	// BGRA (straight alpha) -> NRGBA, then stdlib PNG encode.
	nrgba := image.NewNRGBA(image.Rect(0, 0, int(info.Width), int(info.Height)))
	for i := 0; i+3 < len(pixels); i += 4 {
		nrgba.Pix[i] = pixels[i+2]
		nrgba.Pix[i+1] = pixels[i+1]
		nrgba.Pix[i+2] = pixels[i]
		nrgba.Pix[i+3] = pixels[i+3]
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, nrgba); err != nil {
		h.recordFault(fmt.Sprintf("clipboard PNG copy failed to encode PNG: %v", err))
		return nil, false
	}
	return buf.Bytes(), true
}

// setClipboardDataBytes is clipboard.rs set_clipboard_bytes: allocate a
// GMEM_MOVEABLE global, lock, copy, unlock once, then SetClipboardData.
// On success the system owns the HGLOBAL — the memory is intentionally
// NOT freed here (freeing it would corrupt the clipboard; this is the
// ownership transfer). A failed set releases the allocation locally
// (the reference's Owned handle drops when SetClipboardData errors).
func setClipboardDataBytes(data []byte, format uint32) error {
	global, _, callErr := procGlobalAlloc.Call(uintptr(gmemMoveable), uintptr(len(data)))
	if global == 0 {
		return fmt.Errorf("gpui: GlobalAlloc(%d bytes): %w", len(data), callErr)
	}
	ptr, _, lockErr := procGlobalLock.Call(global)
	if ptr == 0 {
		_, _, _ = procGlobalFree.Call(global)
		return fmt.Errorf("gpui: GlobalLock returned null: %w", lockErr)
	}
	// Copy through RtlMoveMemory: the destination is the locked global
	// (a raw uintptr argument), the source is the Go buffer.
	if len(data) > 0 {
		procRtlMoveMemory.Call(ptr, uintptr(unsafe.Pointer(&data[0])), uintptr(len(data)))
	}
	procGlobalUnlock.Call(global)
	set, _, setErr := procSetClipboardData.Call(uintptr(format), global)
	if set == 0 {
		// The system refused the handle: ownership did NOT transfer, so
		// the allocation must be freed locally, exactly once.
		_, _, _ = procGlobalFree.Call(global)
		return fmt.Errorf("gpui: SetClipboardData(format %d): %w", format, setErr)
	}
	return nil
}

// utf16Bytes renders UTF-16 code units as the in-memory byte form
// (native endianness, which CF_UNICODETEXT uses on this platform).
func utf16Bytes(units []uint16) []byte {
	out := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.NativeEndian.PutUint16(out[i*2:], unit)
	}
	return out
}

// ---------------------------------------------------------------------------
// read_from_clipboard (clipboard.rs): one entry per kind
// ---------------------------------------------------------------------------

// readClipboardOnHostThread is clipboard.rs read_from_clipboard: the
// EnumClipboardFormats walk takes at most ONE text entry, ONE image
// entry and ONE files entry; empty results log the unsupported formats
// and return the None outcome. Runs on the host thread.
func (h *Host) readClipboardOnHostThread() (*ClipboardItem, error) {
	ensureClipboardRegisteredFormats()
	guard, err := openClipboardGuard()
	if err != nil {
		return nil, err
	}
	defer guard.close(h)

	var entries []ClipboardEntry
	haveText := false
	haveImage := false
	haveFiles := false

	count, _, _ := procCountClipboardFormats.Call()
	format := uintptr(0)
	for i := uintptr(0); i < count; i++ {
		// EnumClipboardFormats(previous) — 0 starts the walk; a 0 return
		// mid-walk (clipboard changed) is skipped harmlessly, matching
		// the reference's bounded loop.
		format, _, _ = procEnumClipboardFormats.Call(format)
		current := uint32(format)
		if !haveText && current == cfUnicodeText {
			if entry, ok := readClipboardStringEntry(); ok {
				entries = append(entries, entry)
				haveText = true
			}
		} else if !haveImage && isClipboardImageFormat(current) {
			if entry, ok := readClipboardImageEntry(current); ok {
				entries = append(entries, entry)
				haveImage = true
			}
		} else if !haveFiles && current == cfHDROP {
			if entry, ok := readClipboardFilesEntry(h); ok {
				entries = append(entries, entry)
				haveFiles = true
			}
		}
	}

	if len(entries) == 0 {
		logUnsupportedClipboardFormats(h)
		return nil, nil
	}
	return &ClipboardItem{Entries: entries}, nil
}

// readClipboardStringEntry is clipboard.rs read_string: CF_UNICODETEXT
// text plus the hash-gated metadata.
func readClipboardStringEntry() (ClipboardEntry, bool) {
	text, ok := clipboardGetString(cfUnicodeText)
	if !ok {
		return ClipboardEntry{}, false
	}
	metadata := readClipboardMetadata(text)
	return NewStringClipboardEntry(ClipboardString{Text: text, Metadata: metadata}), true
}

// clipboardGetString is clipboard.rs get_clipboard_string: the locked
// global as native UTF-16, NUL-terminated, decoded lossily. A zero-word
// buffer is the empty string (Some(String::new())).
func clipboardGetString(format uint32) (string, bool) {
	global, _, _ := procGetClipboardData.Call(uintptr(format))
	if global == 0 {
		return "", false
	}
	locked := lockClipboardGlobal(global)
	if locked == nil {
		return "", false
	}
	defer locked.unlock()
	words := locked.utf16Words()
	if len(words) == 0 {
		return "", true
	}
	actualLen := len(words)
	for i, unit := range words {
		if unit == 0 {
			actualLen = i
			break
		}
	}
	// from_utf16_lossy: unpaired surrogates decode as U+FFFD.
	return string(utf16.Decode(words[:actualLen])), true
}

// readClipboardMetadata is clipboard.rs read_clipboard_metadata: read
// the 8-byte native-endian hash; only when it matches the read text's
// hash does the metadata format's string come back.
func readClipboardMetadata(text string) string {
	global, _, _ := procGetClipboardData.Call(uintptr(clipboardHashFormat))
	if global == 0 {
		return ""
	}
	locked := lockClipboardGlobal(global)
	if locked == nil {
		return ""
	}
	defer locked.unlock()
	hashBytes := locked.bytes()
	if len(hashBytes) < 8 {
		return ""
	}
	stored := binary.NativeEndian.Uint64(hashBytes[:8])
	if stored != textHash(text) {
		return ""
	}
	metadata, ok := clipboardGetString(clipboardMetadataFormat)
	if !ok {
		return ""
	}
	return metadata
}

// readClipboardImageEntry is clipboard.rs read_image: CF_DIB converts
// to BMP (Go-side); the registered formats keep their bytes; the image
// id is the content hash (gpui.NewImage).
func readClipboardImageEntry(format uint32) (ClipboardEntry, bool) {
	global, _, _ := procGetClipboardData.Call(uintptr(format))
	if global == 0 {
		return ClipboardEntry{}, false
	}
	locked := lockClipboardGlobal(global)
	if locked == nil {
		return ClipboardEntry{}, false
	}
	defer locked.unlock()

	var data []byte
	var imageFormat ImageFormat
	if format == cfDIB {
		bmp, ok := convertDIBToBMP(locked.bytes())
		if !ok {
			// Truncated DIB: skip the entry (the reference's None).
			return ClipboardEntry{}, false
		}
		data = bmp
		imageFormat = ImageFormatBMP
	} else {
		known, isKnown := clipboardImageFormats[format]
		if !isKnown {
			return ClipboardEntry{}, false
		}
		data = locked.bytes()
		imageFormat = known
	}
	return NewImageClipboardEntry(NewImage(imageFormat, data)), true
}

// readClipboardFilesEntry is clipboard.rs read_files: the CF_HDROP
// global, walked with DragQueryFileW through with_file_names.
func readClipboardFilesEntry(h *Host) (ClipboardEntry, bool) {
	global, _, _ := procGetClipboardData.Call(uintptr(cfHDROP))
	if global == 0 {
		return ClipboardEntry{}, false
	}
	locked := lockClipboardGlobal(global)
	if locked == nil {
		return ClipboardEntry{}, false
	}
	defer locked.unlock()
	// The reference casts the LOCKED pointer to HDROP (read_files:
	// HDROP(locked.ptr as *mut _)) and walks it while the global stays
	// locked.
	var paths []string
	withFileNames(h, locked.ptr, func(name string) {
		paths = append(paths, name)
	})
	return NewExternalPathsClipboardEntry(ExternalPaths(paths)), true
}

// withFileNames is clipboard.rs with_file_names: DragQueryFileW's
// count, then each file's length and buffer. A zero-returning query or
// a non-UTF-16 name records a fault and continues, exactly like the
// reference's error-log-and-continue.
func withFileNames(h *Host, hdrop uintptr, f func(name string)) {
	count, _, _ := procDragQueryFileW.Call(hdrop, uintptr(dragDropGetFilesCount), 0, 0)
	for index := uintptr(0); index < count; index++ {
		length, _, _ := procDragQueryFileW.Call(hdrop, index, 0, 0)
		buffer := make([]uint16, int(length)+1)
		ret, _, _ := procDragQueryFileW.Call(hdrop, index,
			uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
		if ret == 0 {
			h.recordFault("unable to read file name of dragged file")
			continue
		}
		name, ok := utf16ToStringStrict(buffer[:length])
		if !ok {
			h.recordFault("dragged file name is not UTF-16")
			continue
		}
		f(name)
	}
}

// utf16ToStringStrict is Rust's String::from_utf16: an unpaired
// surrogate is an error, not a replacement character.
func utf16ToStringStrict(words []uint16) (string, bool) {
	runes := make([]rune, 0, len(words))
	for i := 0; i < len(words); i++ {
		unit := words[i]
		switch {
		case unit >= 0xD800 && unit <= 0xDBFF:
			if i+1 >= len(words) {
				return "", false
			}
			low := words[i+1]
			if low < 0xDC00 || low > 0xDFFF {
				return "", false
			}
			runes = append(runes, (rune(unit)-0xD800)<<10|(rune(low)-0xDC00)+0x10000)
			i++
		case unit >= 0xDC00 && unit <= 0xDFFF:
			return "", false
		default:
			runes = append(runes, rune(unit))
		}
	}
	return string(runes), true
}

// convertDIBToBMP is clipboard.rs convert_dib_to_bmp: DIB is BMP
// without the 14-byte BITMAPFILEHEADER, so prepend one — the file
// header size, the bit count and compression from the DIB header, the
// <=8bpp color table (colorsUsed or 2^bpp entries, 4 bytes each),
// 12 extra bytes for BI_BITFIELDS masks, then the pixel offset and
// file size.
func convertDIBToBMP(dib []byte) ([]byte, bool) {
	if len(dib) < 40 {
		return nil, false
	}
	headerSize := binary.LittleEndian.Uint32(dib[0:4])
	bitCount := binary.LittleEndian.Uint16(dib[14:16])
	compression := binary.LittleEndian.Uint32(dib[16:20])

	var colorTableSize uint32
	if bitCount <= 8 {
		colorsUsed := binary.LittleEndian.Uint32(dib[32:36])
		if colorsUsed == 0 {
			colorsUsed = 1 << bitCount
		}
		colorTableSize = colorsUsed * 4
	} else if compression == 3 {
		colorTableSize = 12 // BI_BITFIELDS
	}

	pixelOffset := 14 + headerSize + colorTableSize
	fileSize := 14 + uint32(len(dib))

	bmp := make([]byte, 0, fileSize)
	bmp = append(bmp, 'B', 'M')
	bmp = binary.LittleEndian.AppendUint32(bmp, fileSize)
	bmp = append(bmp, 0, 0, 0, 0) // reserved
	bmp = binary.LittleEndian.AppendUint32(bmp, pixelOffset)
	bmp = append(bmp, dib...)
	return bmp, true
}

// logUnsupportedClipboardFormats is clipboard.rs
// log_unsupported_clipboard_formats: the read found nothing it takes,
// so record every present format (number + name). The 64-word name
// buffer is trimmed at its first NUL for a readable fault message (a
// cosmetic difference from the raw from_utf16_lossy of the buffer).
func logUnsupportedClipboardFormats(h *Host) {
	count, _, _ := procCountClipboardFormats.Call()
	format := uintptr(0)
	for i := uintptr(0); i < count; i++ {
		format, _, _ = procEnumClipboardFormats.Call(format)
		buffer := make([]uint16, 64)
		procGetClipboardFormatName.Call(format, uintptr(unsafe.Pointer(&buffer[0])), 64)
		name := string(utf16.Decode(buffer))
		for j, unit := range name {
			if unit == 0 {
				name = name[:j]
				break
			}
		}
		h.recordFault(fmt.Sprintf("Try to paste with unsupported clipboard format: %d, %s.", format, name))
	}
}
