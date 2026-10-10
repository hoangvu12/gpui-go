//go:build windows

// Package clipspec holds ticket23's clipboard gates against the REAL
// system clipboard: host-level multi-entry writes and reads (text with
// the hash-matched metadata pair, images through the registered
// formats plus the PNG compatibility copy, CF_DIB converted to BMP,
// CF_HDROP files through DragQueryFileW), foreign Win32 writers
// emulating other applications, the reference's round-trip corpus
// (crates/gpui_windows/src/platform.rs test_clipboard ~1577), the
// busy/malformed failure handling with the clipboard always closed,
// the async ready-wrap and the primary/find-pasteboard unavailable
// outcomes, and a native interop check through PowerShell.
//
// WARNING: these tests OPEN AND OVERWRITE the machine's real system
// clipboard (that is the ticket's demand — no mocking layer). Run them
// in an interactive session where clobbering the clipboard is
// acceptable. Real-window expectations FAIL instead of skipping (the
// host boots a real platform window; the busy test opens a real holder
// window); the PowerShell interop check skips only when powershell.exe
// is absent from the machine.
package clipspec

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image/color"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// Shared assertions
// ---------------------------------------------------------------------------

// requireStringItem asserts the item is exactly one string entry with
// the given text and metadata ("" = none).
func requireStringItem(t *testing.T, item *gpui.ClipboardItem, text, metadata string) {
	t.Helper()
	if item == nil {
		t.Fatalf("read clipboard item = nil, want one string entry %q", text)
	}
	if len(item.Entries) != 1 || item.Entries[0].Kind != gpui.ClipboardEntryString {
		t.Fatalf("read item entries = %+v, want exactly one string entry", item.Entries)
	}
	got := item.Entries[0].String
	if got.Text != text || got.Metadata != metadata {
		t.Fatalf("string entry = {%q %q}, want {%q %q}", got.Text, got.Metadata, text, metadata)
	}
}

// mustCodec constructs the image codec (the native artifact must be
// present for the image-pixel gates; a missing artifact FAILS).
func mustCodec(t *testing.T) *gpui.ImageCodec {
	t.Helper()
	codec, err := gpui.NewImageCodec()
	if err != nil {
		t.Fatalf("NewImageCodec: %v (the image-pixel gates need the frozen native artifact)", err)
	}
	t.Cleanup(func() { _ = codec.Dispose() })
	return codec
}

// requireDecoded asserts the decode of one image through the codec's
// clipboard entry: dimensions and BGRA pixel samples at positions
// (x, y) with y=0 the TOP row.
func requireDecoded(t *testing.T, codec *gpui.ImageCodec, data []byte, format gpui.ImageFormat, width, height int, samples [][3]interface{}) {
	t.Helper()
	render, err := codec.DecodeClipboard(data, format)
	if err != nil {
		t.Fatalf("DecodeClipboard(%s): %v", format, err)
	}
	if render.FrameCount() < 1 {
		t.Fatalf("DecodeClipboard(%s): no frames", format)
	}
	if w, h := render.Size(0); w != width || h != height {
		t.Fatalf("decoded size = %dx%d, want %dx%d", w, h, width, height)
	}
	frame := render.Frames[0]
	if len(frame.Pixels) != width*height*4 {
		t.Fatalf("pixel buffer = %d bytes, want %d", len(frame.Pixels), width*height*4)
	}
	for _, sample := range samples {
		x := sample[0].(int)
		y := sample[1].(int)
		want := sample[2].([4]byte)
		at := (y*width + x) * 4
		got := [4]byte{frame.Pixels[at], frame.Pixels[at+1], frame.Pixels[at+2], frame.Pixels[at+3]}
		if got != want {
			t.Fatalf("pixel (%d,%d) = BGRA %v, want %v", x, y, got, want)
		}
	}
}

// requireDenied asserts the error is the typed busy-clipboard outcome.
func requireDenied(t *testing.T, op string, err error) {
	t.Helper()
	var readErr *gpui.ClipboardReadError
	if !errors.As(err, &readErr) || readErr.Kind != gpui.ClipboardReadDenied {
		t.Fatalf("%s error = %v, want *ClipboardReadError{Denied}", op, err)
	}
}

// appRead/appWrite are hostRead/hostWrite's App-surface twins: the
// same bounded retry for the machine's transient clipboard-viewer
// contention.
func appRead(t *testing.T, app *gpui.App) *gpui.ClipboardItem {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		item, err := app.ReadClipboard()
		if err == nil {
			return item
		}
		var readErr *gpui.ClipboardReadError
		if !errors.As(err, &readErr) || readErr.Kind != gpui.ClipboardReadDenied {
			t.Fatalf("app read: %v", err)
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("app read still busy after 2s of retries: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func appWrite(t *testing.T, app *gpui.App, item gpui.ClipboardItem) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		err := app.WriteClipboard(item)
		if err == nil {
			return
		}
		var readErr *gpui.ClipboardReadError
		if !errors.As(err, &readErr) || readErr.Kind != gpui.ClipboardReadDenied {
			t.Fatalf("app write: %v", err)
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("app write still busy after 2s of retries: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ---------------------------------------------------------------------------
// The reference round-trip corpus (platform.rs test_clipboard ~1577)
// ---------------------------------------------------------------------------

// TestClipboardReferenceCorpusRoundTrip mirrors the pinned Windows
// test_clipboard corpus exactly: CJK text, plain digits, and JSON
// metadata — each written then read back through the host, plus the
// hash/metadata format pair the metadata write publishes.
func TestClipboardReferenceCorpusRoundTrip(t *testing.T) {
	h := newHost(t)

	// ClipboardItem::new_string("你好，我是张小白".to_string())
	hostWrite(t, h, gpui.NewClipboardStringItem("你好，我是张小白"))
	item := hostRead(t, h)
	requireStringItem(t, item, "你好，我是张小白", "")
	if text, ok := item.Text(); !ok || text != "你好，我是张小白" {
		t.Fatalf("item.Text() = %q,%v", text, ok)
	}
	clipboardStillOpenable(t)

	// ClipboardItem::new_string("12345".to_string())
	hostWrite(t, h, gpui.NewClipboardStringItem("12345"))
	item = hostRead(t, h)
	requireStringItem(t, item, "12345", "")

	// ClipboardItem::new_string_with_json_metadata("abcdef", vec![3, 4])
	item334, err := gpui.NewClipboardStringItemWithJSONMetadata("abcdef", []int{3, 4})
	if err != nil {
		t.Fatalf("json metadata item: %v", err)
	}
	hostWrite(t, h, item334)
	item = hostRead(t, h)
	requireStringItem(t, item, "abcdef", "[3,4]")
	if metadata, ok := item.Metadata(); !ok || metadata != "[3,4]" {
		t.Fatalf("item.Metadata() = %q,%v, want [3,4],true", metadata, ok)
	}
	var decoded []int
	if !item.Entries[0].String.UnmarshalMetadataJSON(&decoded) || len(decoded) != 2 || decoded[0] != 3 || decoded[1] != 4 {
		t.Fatalf("UnmarshalMetadataJSON = %v, want [3 4]", decoded)
	}

	// The metadata write publishes the pinned format pair: an 8-byte
	// native hash and the UTF-16 metadata string.
	hashFormat := foreignRegisterFormat(t, "GPUI internal text hash")
	metadataFormat := foreignRegisterFormat(t, "GPUI internal metadata")
	hashBytes, ok := foreignFormatData(t, hashFormat)
	if !ok || len(hashBytes) != 8 {
		t.Fatalf("text-hash format data = %v (len %d), want 8 bytes", ok, len(hashBytes))
	}
	metadataBytes, ok := foreignFormatData(t, metadataFormat)
	if !ok {
		t.Fatal("metadata format is not on the clipboard after the metadata write")
	}
	units := make([]uint16, len(metadataBytes)/2)
	for i := range units {
		units[i] = binary.NativeEndian.Uint16(metadataBytes[i*2:])
	}
	if text := strings.TrimRight(string(utf16.Decode(units)), "\x00"); text != "[3,4]" {
		t.Fatalf("metadata format content = %q, want [3,4]", text)
	}

	// The hash is a deterministic function of the TEXT only: the same
	// text (with different metadata) publishes the same hash; a
	// different text publishes a different one.
	firstHash := append([]byte(nil), hashBytes...)
	hostWrite(t, h, gpui.NewClipboardStringItemWithMetadata("abcdef", "other"))
	sameTextHash, ok := foreignFormatData(t, hashFormat)
	if !ok || !bytes.Equal(firstHash, sameTextHash) {
		t.Fatalf("hash of the same text changed: %x vs %x", firstHash, sameTextHash)
	}
	hostWrite(t, h, gpui.NewClipboardStringItemWithMetadata("abcdefg", "meta"))
	changedHash, _ := foreignFormatData(t, hashFormat)
	if bytes.Equal(firstHash, changedHash) {
		t.Fatal("hash of a different text equals the original text's hash")
	}
	clipboardStillOpenable(t)
}

// ---------------------------------------------------------------------------
// The metadata hash gate
// ---------------------------------------------------------------------------

// TestClipboardMetadataHashGate proves the metadata read is gated on
// the text-hash match: a foreign app replacing only the text leaves a
// stale hash, so the metadata is dropped; a foreign app writing text
// only (the common case) yields no metadata either.
func TestClipboardMetadataHashGate(t *testing.T) {
	h := newHost(t)

	// Our app writes text + metadata.
	hostWrite(t, h, gpui.NewClipboardStringItemWithMetadata("original", `{"n":1}`))

	// A foreign app replaces ONLY CF_UNICODETEXT (SetClipboardData
	// without EmptyClipboard): the hash and metadata stay, the text
	// changes — the hash no longer matches, so the metadata is gated
	// out.
	foreignSetUTF16(t, "replaced by a foreign app", false)
	item := hostRead(t, h)
	requireStringItem(t, item, "replaced by a foreign app", "")
	clipboardStillOpenable(t)

	// A foreign app writing its own text (the common case: another
	// editor's copy) carries no GPUI metadata at all.
	foreignSetUTF16(t, "notepad copy", true)
	item = hostRead(t, h)
	requireStringItem(t, item, "notepad copy", "")

	// Restore our text: our own hash matches again, so the metadata
	// comes back.
	hostWrite(t, h, gpui.NewClipboardStringItemWithMetadata("original", `{"n":1}`))
	item = hostRead(t, h)
	requireStringItem(t, item, "original", `{"n":1}`)
	clipboardStillOpenable(t)
}

// ---------------------------------------------------------------------------
// Image writes: registered formats plus the PNG compatibility copy
// ---------------------------------------------------------------------------

// TestClipboardImageWritesNativeFormatAndPngCopy checks write_image's
// two halves: the native registered format for the image's own format
// (PNG, GIF, SVG) and the PNG compatibility copy for non-PNG
// decodable formats (GIF gets both), with SVG skipped. The read side
// hands back the entry with the content-hash id intact, and the
// round-tripped bytes decode to the source pixels.
func TestClipboardImageWritesNativeFormatAndPngCopy(t *testing.T) {
	h := newHost(t)
	codec := mustCodec(t)

	pngBytes := pngHalves(6, 4)
	pngImage := gpui.NewImage(gpui.ImageFormatPNG, pngBytes)
	hostWrite(t, h, gpui.NewClipboardImageItem(pngImage))
	formats := foreignEnumFormats(t)
	if _, ok := formats[foreignRegisterFormat(t, "PNG")]; !ok {
		t.Fatalf("PNG format missing after a PNG write: %v", formats)
	}
	if _, ok := formats[foreignRegisterFormat(t, "GIF")]; ok {
		t.Fatalf("GIF format present after a PNG write: %v", formats)
	}
	published, ok := foreignFormatData(t, foreignRegisterFormat(t, "PNG"))
	if !ok || !bytes.Equal(published, pngBytes) {
		t.Fatalf("published PNG bytes differ from the image bytes (%v, %d vs %d bytes)", ok, len(published), len(pngBytes))
	}
	item := hostRead(t, h)
	if item == nil || len(item.Entries) != 1 || item.Entries[0].Kind != gpui.ClipboardEntryImage {
		t.Fatalf("PNG read = %+v, want one image entry", item)
	}
	entry := item.Entries[0].Image
	if entry.Format != gpui.ImageFormatPNG || !bytes.Equal(entry.Bytes, pngBytes) || entry.ID != pngImage.ID {
		t.Fatalf("PNG entry = %s/%d bytes/id %d, want the written image (%d bytes/id %d)",
			entry.Format, len(entry.Bytes), entry.ID, len(pngBytes), pngImage.ID)
	}
	// Image pixels round-trip: top red, bottom blue (BGRA).
	requireDecoded(t, codec, entry.Bytes, gpui.ImageFormatPNG, 6, 4, [][3]interface{}{
		{0, 0, [4]byte{0, 0, 255, 255}},
		{5, 1, [4]byte{0, 0, 255, 255}},
		{0, 2, [4]byte{255, 0, 0, 255}},
		{5, 3, [4]byte{255, 0, 0, 255}},
	})

	// GIF writes BOTH the registered GIF format and the PNG
	// compatibility copy (decoded and re-encoded through the codec).
	gifBytes := gifSolid(5, 5, color.RGBA{G: 255, A: 255})
	gifImage := gpui.NewImage(gpui.ImageFormatGIF, gifBytes)
	hostWrite(t, h, gpui.NewClipboardImageItem(gifImage))
	formats = foreignEnumFormats(t)
	if _, ok := formats[foreignRegisterFormat(t, "GIF")]; !ok {
		t.Fatalf("GIF format missing after a GIF write: %v", formats)
	}
	if _, ok := formats[foreignRegisterFormat(t, "PNG")]; !ok {
		t.Fatalf("PNG compatibility copy missing after a GIF write: %v", formats)
	}
	publishedGIF, ok := foreignFormatData(t, foreignRegisterFormat(t, "GIF"))
	if !ok || !bytes.Equal(publishedGIF, gifBytes) {
		t.Fatalf("published GIF bytes differ from the image bytes")
	}
	publishedPng, ok := foreignFormatData(t, foreignRegisterFormat(t, "PNG"))
	if !ok || len(publishedPng) == 0 || bytes.Equal(publishedPng, gifBytes) {
		t.Fatalf("PNG copy = %v (%d bytes): want a real re-encoded PNG, not the raw GIF", ok, len(publishedPng))
	}
	item = hostRead(t, h)
	if item == nil || len(item.Entries) != 1 || item.Entries[0].Kind != gpui.ClipboardEntryImage {
		t.Fatalf("GIF read = %+v, want one image entry", item)
	}
	entry = item.Entries[0].Image
	// The one-entry-per-kind policy: the first image format in the
	// enumeration wins; it is the GIF or the PNG copy, and either
	// decodes to the same 5x5 green frame.
	if entry.Format != gpui.ImageFormatGIF && entry.Format != gpui.ImageFormatPNG {
		t.Fatalf("image entry format = %s, want GIF or PNG", entry.Format)
	}
	requireDecoded(t, codec, entry.Bytes, entry.Format, 5, 5, [][3]interface{}{
		{0, 0, [4]byte{0, 255, 0, 255}},
		{4, 4, [4]byte{0, 255, 0, 255}},
	})

	// SVG writes only the registered "image/svg+xml" format (no PNG
	// copy — it cannot be rasterized here).
	svgBytes := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="4" height="4"><rect width="4" height="4" fill="red"/></svg>`)
	svgImage := gpui.NewImage(gpui.ImageFormatSVG, svgBytes)
	hostWrite(t, h, gpui.NewClipboardImageItem(svgImage))
	formats = foreignEnumFormats(t)
	if _, ok := formats[foreignRegisterFormat(t, "image/svg+xml")]; !ok {
		t.Fatalf("image/svg+xml format missing after an SVG write: %v", formats)
	}
	if _, ok := formats[foreignRegisterFormat(t, "PNG")]; ok {
		t.Fatalf("PNG copy present after an SVG write (SVG must be skipped): %v", formats)
	}
	item = hostRead(t, h)
	if item == nil || len(item.Entries) != 1 || item.Entries[0].Kind != gpui.ClipboardEntryImage {
		t.Fatalf("SVG read = %+v, want one image entry", item)
	}
	if entry := item.Entries[0].Image; entry.Format != gpui.ImageFormatSVG || !bytes.Equal(entry.Bytes, svgBytes) {
		t.Fatalf("SVG entry = %s (%d bytes), want the written bytes", entry.Format, len(entry.Bytes))
	}
	clipboardStillOpenable(t)
}

// ---------------------------------------------------------------------------
// CF_DIB -> BMP (clipboard.rs convert_dib_to_bmp)
// ---------------------------------------------------------------------------

// TestClipboardDIBReadConvertsToBmp checks the CF_DIB read: the
// 14-byte BITMAPFILEHEADER is prepended (file size, pixel offset with
// the <=8bpp color table and the BI_BITFIELDS 12), the entry's format
// is BMP, and the converted bytes decode to the DIB's pixels. A
// truncated DIB skips the entry entirely.
func TestClipboardDIBReadConvertsToBmp(t *testing.T) {
	h := newHost(t)
	codec := mustCodec(t)

	// 24bpp DIB: top half red, bottom half blue.
	dib := dib24bpp(6, 4, func(x, y int) (r, g, b uint8) {
		if y < 2 {
			return 255, 0, 0
		}
		return 0, 0, 255
	})
	foreignSetBytes(t, cfDIB, dib, true)
	item := hostRead(t, h)
	if item == nil || len(item.Entries) != 1 || item.Entries[0].Kind != gpui.ClipboardEntryImage {
		t.Fatalf("CF_DIB read = %+v, want one image entry", item)
	}
	bmp := item.Entries[0].Image.Bytes
	if item.Entries[0].Image.Format != gpui.ImageFormatBMP {
		t.Fatalf("CF_DIB entry format = %s, want BMP", item.Entries[0].Image.Format)
	}
	// rowSize = 20, 4 rows, 40-byte header: file = 14 + 40 + 80.
	if len(bmp) != 14+40+80 {
		t.Fatalf("BMP length = %d, want 134", len(bmp))
	}
	if string(bmp[0:2]) != "BM" {
		t.Fatalf("BMP magic = %q", bmp[0:2])
	}
	if got := binary.LittleEndian.Uint32(bmp[2:6]); got != uint32(len(bmp)) {
		t.Fatalf("BMP file size = %d, want %d", got, len(bmp))
	}
	if got := binary.LittleEndian.Uint32(bmp[10:14]); got != 54 {
		t.Fatalf("24bpp BMP pixel offset = %d, want 54 (14+40, no color table)", got)
	}
	if !bytes.Equal(bmp[14:], dib) {
		t.Fatal("BMP payload differs from the published DIB bytes")
	}
	// The converted BMP decodes back to the DIB's pixels (bottom-up
	// rows in the DIB, top-down pixels in the frame).
	requireDecoded(t, codec, bmp, gpui.ImageFormatBMP, 6, 4, [][3]interface{}{
		{0, 0, [4]byte{0, 0, 255, 255}},
		{5, 1, [4]byte{0, 0, 255, 255}},
		{0, 2, [4]byte{255, 0, 0, 255}},
		{5, 3, [4]byte{255, 0, 0, 255}},
	})

	// 8bpp palette DIB: colorsUsed 0 -> 2^8 table entries x4 bytes, so
	// the pixel offset is 14+40+1024 = 1078.
	dib8 := dib8bpp(4, 3, color.RGBA{R: 10, G: 200, B: 30, A: 255})
	foreignSetBytes(t, cfDIB, dib8, true)
	item = hostRead(t, h)
	if item == nil || len(item.Entries) != 1 {
		t.Fatalf("palette CF_DIB read = %+v, want one image entry", item)
	}
	bmp = item.Entries[0].Image.Bytes
	if got := binary.LittleEndian.Uint32(bmp[10:14]); got != 1078 {
		t.Fatalf("8bpp BMP pixel offset = %d, want 1078", got)
	}
	if got := binary.LittleEndian.Uint32(bmp[2:6]); got != uint32(14+len(dib8)) {
		t.Fatalf("8bpp BMP file size = %d, want %d", got, 14+len(dib8))
	}
	requireDecoded(t, codec, bmp, gpui.ImageFormatBMP, 4, 3, [][3]interface{}{
		{0, 0, [4]byte{30, 200, 10, 255}},
		{3, 2, [4]byte{30, 200, 10, 255}},
	})

	// BI_BITFIELDS (compression 3) 32bpp DIB: 12 mask bytes count as
	// the color table, so the pixel offset is 14+40+12 = 66. The DIB's
	// bottom row is blue and its top row red (bottom-up rows).
	const (
		bfWidth  = 3
		bfHeight = 2
	)
	dibBF := make([]byte, 40+12+bfWidth*4*bfHeight)
	binary.LittleEndian.PutUint32(dibBF[0:4], 40)
	binary.LittleEndian.PutUint32(dibBF[4:8], bfWidth)
	binary.LittleEndian.PutUint32(dibBF[8:12], bfHeight)
	binary.LittleEndian.PutUint16(dibBF[12:14], 1)
	binary.LittleEndian.PutUint16(dibBF[14:16], 32)
	binary.LittleEndian.PutUint32(dibBF[16:20], 3)          // BI_BITFIELDS
	binary.LittleEndian.PutUint32(dibBF[40:44], 0x00FF0000) // R mask
	binary.LittleEndian.PutUint32(dibBF[44:48], 0x0000FF00) // G mask
	binary.LittleEndian.PutUint32(dibBF[48:52], 0x000000FF) // B mask
	pixels := dibBF[52:]
	for x := 0; x < bfWidth; x++ {
		// DIB row 0 is the image's BOTTOM row: red (BGRX 0x00FF0000
		// little-endian, R mask hit); the top row is blue
		// (0x000000FF, B mask hit).
		binary.LittleEndian.PutUint32(pixels[x*4:], 0x00FF0000)
		binary.LittleEndian.PutUint32(pixels[bfWidth*4+x*4:], 0x000000FF)
	}
	foreignSetBytes(t, cfDIB, dibBF, true)
	item = hostRead(t, h)
	if item == nil || len(item.Entries) != 1 {
		t.Fatalf("bitfields CF_DIB read = %+v, want one image entry", item)
	}
	bmp = item.Entries[0].Image.Bytes
	if got := binary.LittleEndian.Uint32(bmp[10:14]); got != 66 {
		t.Fatalf("BI_BITFIELDS BMP pixel offset = %d, want 66", got)
	}
	// Bottom-up rows: the frame's top row (y=0) is the DIB's LAST row
	// (blue), its bottom row (y=1) is the DIB's row 0 (red).
	requireDecoded(t, codec, bmp, gpui.ImageFormatBMP, bfWidth, bfHeight, [][3]interface{}{
		{0, 0, [4]byte{255, 0, 0, 255}},
		{2, 0, [4]byte{255, 0, 0, 255}},
		{0, 1, [4]byte{0, 0, 255, 255}},
		{2, 1, [4]byte{0, 0, 255, 255}},
	})

	// Truncated DIB (< 40 bytes): the conversion bails, the entry is
	// skipped, the read returns the None outcome — and the clipboard
	// is still closed afterwards.
	foreignSetBytes(t, cfDIB, dib[:20], true)
	item = hostRead(t, h)
	if item != nil {
		t.Fatalf("truncated CF_DIB read = %+v, want the None outcome", item)
	}
	clipboardStillOpenable(t)
}

// ---------------------------------------------------------------------------
// CF_HDROP files (DragQueryFileW)
// ---------------------------------------------------------------------------

// TestClipboardFilesCFHDROPDragQuery checks the CF_HDROP read through
// DragQueryFileW: the DROPFILES walk yields the file list in order,
// including spaces and non-ASCII names.
func TestClipboardFilesCFHDROPDragQuery(t *testing.T) {
	h := newHost(t)

	paths := []string{
		`C:\clipspec\first file.txt`,
		`C:\clipspec\nested\你好 张小白.png`,
		`D:\third`,
	}
	foreignSetBytes(t, cfHDROP, dropFiles(paths), true)
	item := hostRead(t, h)
	if item == nil || len(item.Entries) != 1 || item.Entries[0].Kind != gpui.ClipboardEntryExternalPaths {
		t.Fatalf("CF_HDROP read = %+v, want one external-paths entry", item)
	}
	got := item.Entries[0].Paths.Paths()
	if len(got) != len(paths) {
		t.Fatalf("paths = %v, want %v", got, paths)
	}
	for i, want := range paths {
		if got[i] != want {
			t.Fatalf("path %d = %q, want %q", i, got[i], want)
		}
	}
	// The paths fallback of item.Text(): a files-only item reports the
	// concatenated path displays as its text.
	if text, ok := item.Text(); !ok || text != strings.Join(paths, "") {
		t.Fatalf("files-only item.Text() = %q,%v, want the joined paths", text, ok)
	}
	clipboardStillOpenable(t)
}

// ---------------------------------------------------------------------------
// Busy clipboard
// ---------------------------------------------------------------------------

// TestClipboardBusyDeniedNoHang holds the clipboard open with a real
// owner window (the deterministic blocker) and proves the host's read
// and write fail FAST with the typed Denied error — no retry loop, no
// hang — and that normal operation resumes after the holder releases.
func TestClipboardBusyDeniedNoHang(t *testing.T) {
	h := newHost(t)
	_, hwnd := holdWindow(t, h)

	hostWrite(t, h, gpui.NewClipboardStringItem("before-busy"))

	hold := holdClipboard(t, hwnd)
	start := time.Now()
	if _, err := h.ReadClipboard(); err == nil {
		t.Fatal("read during a busy clipboard succeeded; want the typed Denied error")
	} else {
		requireDenied(t, "read", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("busy read took %v; the pinned behavior is an immediate give-up", elapsed)
	}

	start = time.Now()
	if err := h.WriteClipboard(gpui.NewClipboardStringItem("during-busy")); err == nil {
		t.Fatal("write during a busy clipboard succeeded; want the typed Denied error")
	} else {
		requireDenied(t, "write", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("busy write took %v", elapsed)
	}
	hold.release()

	// The pre-busy write survived; normal operation resumes.
	item := hostRead(t, h)
	requireStringItem(t, item, "before-busy", "")
	hostWrite(t, h, gpui.NewClipboardStringItem("after-busy"))
	item = hostRead(t, h)
	requireStringItem(t, item, "after-busy", "")
	clipboardStillOpenable(t)
}

// ---------------------------------------------------------------------------
// Malformed foreign data
// ---------------------------------------------------------------------------

// TestClipboardMalformedForeignData feeds malformed clipboard content
// a foreign app could produce: bad UTF-16 (lossy replacement), an
// empty-string global, a truncated hash global, an unsupported custom
// format and an empty clipboard. None of these may error, and none may
// leave the clipboard open.
func TestClipboardMalformedForeignData(t *testing.T) {
	h := newHost(t)

	// Bad UTF-16: an unpaired high surrogate decodes lossily to
	// U+FFFD (from_utf16_lossy).
	badUTF16 := []byte{0x00, 0xD8, 0x00, 0x00} // [0xD800, 0]
	foreignSetBytes(t, cfUnicodeText, badUTF16, true)
	item := hostRead(t, h)
	requireStringItem(t, item, "\uFFFD", "")
	clipboardStillOpenable(t)

	// Empty string global: one NUL code unit -> the empty string entry
	// (get_clipboard_string's zero-length-after-NUL path).
	foreignSetUTF16(t, "", true)
	item = hostRead(t, h)
	requireStringItem(t, item, "", "")
	clipboardStillOpenable(t)

	// Truncated hash global (fewer than 8 bytes): the metadata read
	// bails gracefully; the text still comes back.
	hashFormat := foreignRegisterFormat(t, "GPUI internal text hash")
	metadataFormat := foreignRegisterFormat(t, "GPUI internal metadata")
	foreignSetUTF16(t, "hashgate", true)
	foreignSetBytes(t, hashFormat, []byte{1, 2, 3, 4}, false)
	foreignSetUTF16To(t, metadataFormat, "stale-metadata", false)
	item = hostRead(t, h)
	requireStringItem(t, item, "hashgate", "")
	clipboardStillOpenable(t)

	// An unsupported custom format only: the None outcome.
	custom := foreignRegisterFormat(t, "clipspec custom format")
	foreignSetBytes(t, custom, []byte{9, 9, 9}, true)
	item = hostRead(t, h)
	if item != nil {
		t.Fatalf("custom-format-only read = %+v, want the None outcome", item)
	}
	clipboardStillOpenable(t)

	// An empty clipboard: the None outcome.
	foreignOpenWithRetry(t)
	if r, _, _ := foreignEmptyClipboard.Call(); r == 0 {
		t.Fatal("foreign EmptyClipboard failed")
	}
	foreignCloseClipboard.Call()
	item = hostRead(t, h)
	if item != nil {
		t.Fatalf("empty clipboard read = %+v, want the None outcome", item)
	}
	clipboardStillOpenable(t)
}

// foreignSetUTF16To publishes text under an arbitrary format (the
// metadata format's UTF-16 shape).
func foreignSetUTF16To(t *testing.T, format uint32, text string, emptyFirst bool) {
	t.Helper()
	units, err := utf16FromString(text)
	if err != nil {
		t.Fatalf("UTF16FromString: %v", err)
	}
	data := make([]byte, len(units)*2)
	for i, unit := range units {
		binary.NativeEndian.PutUint16(data[i*2:], unit)
	}
	foreignSetBytes(t, format, data, emptyFirst)
}

func utf16FromString(text string) ([]uint16, error) {
	runes := []rune(text)
	units := utf16.Encode(runes)
	return append(units, 0), nil
}

// ---------------------------------------------------------------------------
// Multi-entry writes and the one-entry-per-kind read policy
// ---------------------------------------------------------------------------

// TestClipboardMultiEntryWriteAndReadPolicy writes mixed-kind items
// (text + image; text + skipped ExternalPaths; the multi-string case)
// and reads a foreign board carrying text, image AND files at once —
// the read takes exactly one entry per kind.
func TestClipboardMultiEntryWriteAndReadPolicy(t *testing.T) {
	h := newHost(t)
	codec := mustCodec(t)

	// Text + image in one item: both kinds publish and both come back.
	pngBytes := pngHalves(4, 2)
	mixed := gpui.ClipboardItem{Entries: []gpui.ClipboardEntry{
		gpui.NewStringClipboardEntry(gpui.NewClipboardString("mixed text")),
		gpui.NewImageClipboardEntry(gpui.NewImage(gpui.ImageFormatPNG, pngBytes)),
	}}
	hostWrite(t, h, mixed)
	item := hostRead(t, h)
	if len(item.Entries) != 2 ||
		item.Entries[0].Kind != gpui.ClipboardEntryString ||
		item.Entries[1].Kind != gpui.ClipboardEntryImage {
		t.Fatalf("mixed read = %+v, want [string image]", item.Entries)
	}
	if text, ok := item.Text(); !ok || text != "mixed text" {
		t.Fatalf("mixed Text() = %q,%v", text, ok)
	}
	// The single-string metadata rule: a multi-entry item reports no
	// metadata even when its string entry carries one.
	if _, ok := item.Metadata(); ok {
		t.Fatal("multi-entry item reported metadata; the rule requires exactly one string entry")
	}
	if !bytes.Equal(item.Entries[1].Image.Bytes, pngBytes) {
		t.Fatal("mixed read image bytes differ")
	}
	requireDecoded(t, codec, item.Entries[1].Image.Bytes, item.Entries[1].Image.Format, 4, 2, nil)

	// ExternalPaths entries are skipped on write (clipboard.rs
	// `ClipboardEntry::ExternalPaths(_) => {}`): the write succeeds,
	// the read finds no files entry.
	withPaths := gpui.ClipboardItem{Entries: []gpui.ClipboardEntry{
		gpui.NewStringClipboardEntry(gpui.NewClipboardString("with paths")),
		gpui.NewExternalPathsClipboardEntry(gpui.ExternalPaths{`C:\one`, `C:\two`}),
	}}
	hostWrite(t, h, withPaths)
	item = hostRead(t, h)
	if len(item.Entries) != 1 || item.Entries[0].Kind != gpui.ClipboardEntryString {
		t.Fatalf("with-paths read = %+v, want one string entry (ExternalPaths skipped on write)", item.Entries)
	}

	// Multi-string items: Windows replaces the same format's data, so
	// the LAST string's CF_UNICODETEXT is what the board holds — the
	// pinned write path simply sets the format per entry.
	multiString := gpui.ClipboardItem{Entries: []gpui.ClipboardEntry{
		gpui.NewStringClipboardEntry(gpui.NewClipboardString("first")),
		gpui.NewStringClipboardEntry(gpui.NewClipboardString("second")),
	}}
	hostWrite(t, h, multiString)
	item = hostRead(t, h)
	requireStringItem(t, item, "second", "")

	// A foreign board with text + TWO image formats + files at once:
	// the read takes exactly one entry per kind.
	foreignSetUTF16(t, "kind policy", true)
	foreignSetBytes(t, foreignRegisterFormat(t, "PNG"), pngBytes, false)
	foreignSetBytes(t, cfDIB, dib24bpp(2, 2, func(x, y int) (r, g, b uint8) { return 9, 9, 9 }), false)
	foreignSetBytes(t, cfHDROP, dropFiles([]string{`C:\policy\a.txt`}), false)
	item = hostRead(t, h)
	kinds := map[gpui.ClipboardEntryKind]int{}
	for _, entry := range item.Entries {
		kinds[entry.Kind]++
	}
	if len(item.Entries) != 3 ||
		kinds[gpui.ClipboardEntryString] != 1 ||
		kinds[gpui.ClipboardEntryImage] != 1 ||
		kinds[gpui.ClipboardEntryExternalPaths] != 1 {
		t.Fatalf("kind policy read = %+v, want exactly one entry of each kind", item.Entries)
	}
	if text, ok := item.Text(); !ok || text != "kind policy" {
		t.Fatalf("kind policy text = %q,%v", text, ok)
	}
	clipboardStillOpenable(t)
}

// ---------------------------------------------------------------------------
// Ownership transfer across repeated write cycles
// ---------------------------------------------------------------------------

// TestClipboardWriteCyclesOwnership repeats the write/read cycle with
// varying sizes (including empty and a ~115 KiB text), proving the
// GMEM_MOVEABLE allocations transfer to the system on every
// SetClipboardData (no double free, no leak): every cycle reads back
// its own text and the clipboard stays openable throughout.
func TestClipboardWriteCyclesOwnership(t *testing.T) {
	h := newHost(t)

	for i := 0; i < 8; i++ {
		text := strings.Repeat("ab", i*1024/2)
		if i == 0 {
			text = ""
		}
		if i == 7 {
			text = strings.Repeat("ownership-transfer-", 6000) // ~114 KiB
		}
		hostWrite(t, h, gpui.NewClipboardStringItem(text))
		item := hostRead(t, h)
		requireStringItem(t, item, text, "")
		clipboardStillOpenable(t)
	}

	// Metadata cycles exercise the three-format write repeatedly.
	for i := 0; i < 4; i++ {
		hostWrite(t, h, gpui.NewClipboardStringItemWithMetadata(
			fmt.Sprintf("meta cycle %d", i), fmt.Sprintf(`{"i":%d}`, i)))
		item := hostRead(t, h)
		requireStringItem(t, item, fmt.Sprintf("meta cycle %d", i), fmt.Sprintf(`{"i":%d}`, i))
	}
	clipboardStillOpenable(t)
}

// ---------------------------------------------------------------------------
// The application surface: sync, async ready-wrap, selections
// ---------------------------------------------------------------------------

// TestAppClipboardSurfaceAsyncAndSelections drives the App-level
// surface with a real attached host: Write/ReadClipboard, the async
// read as the pinned Task::ready wrap of the sync read (Done
// immediately, awaiting returns the sync outcome), the busy-clipboard
// async mapping, and the primary-selection/find-pasteboard explicit
// unavailable outcomes.
func TestAppClipboardSurfaceAsyncAndSelections(t *testing.T) {
	before := runtime.NumGoroutine()
	app := gpui.NewApp()
	h := gpui.NewHost()
	if err := app.Attach(h); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	t.Cleanup(func() {
		if err := h.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		expectNoLeakedGoroutines(t, before)
	})

	// Sync surface through the app.
	appWrite(t, app, gpui.NewClipboardStringItemWithMetadata("app-surface", `{"k":1}`))
	item := appRead(t, app)
	requireStringItem(t, item, "app-surface", `{"k":1}`)

	// The async read is the sync read wrapped ready: it is Done the
	// moment it is constructed, and a foreground task awaiting it gets
	// the same outcome.
	task := app.ReadClipboardAsync()
	if !task.Done() {
		t.Fatal("ReadClipboardAsync task is not ready immediately (the pinned default is Task::ready of the sync read)")
	}
	outcomes := make(chan gpui.ClipboardReadOutcome, 1)
	app.Spawn(func(cx *gpui.AsyncApp) gpui.ClipboardReadOutcome {
		outcome := cx.Await(task)
		outcomes <- outcome
		return outcome
	})
	select {
	case outcome := <-outcomes:
		if outcome.Err != nil {
			t.Fatalf("async outcome err = %v", outcome.Err)
		}
		requireStringItem(t, outcome.Item, "app-surface", `{"k":1}`)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out awaiting the ready clipboard task")
	}

	// Busy clipboard through the async surface: the typed Denied error
	// passes through the ready wrap. The hold uses a real owner window
	// (the deterministic blocker).
	_, hwnd := holdWindow(t, h)
	hold := holdClipboard(t, hwnd)
	busyTask := app.ReadClipboardAsync()
	if !busyTask.Done() {
		t.Fatal("busy async task is not ready immediately")
	}
	busyOutcomes := make(chan gpui.ClipboardReadOutcome, 1)
	app.Spawn(func(cx *gpui.AsyncApp) gpui.ClipboardReadOutcome {
		outcome := cx.Await(busyTask)
		busyOutcomes <- outcome
		return outcome
	})
	select {
	case outcome := <-busyOutcomes:
		if outcome.Err == nil || outcome.Err.Kind != gpui.ClipboardReadDenied {
			t.Fatalf("busy async outcome err = %v, want Denied", outcome.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out awaiting the busy clipboard task")
	}
	hold.release()

	// Primary selection and find pasteboard: the pinned Windows
	// cfg-gate surfaces report the explicit unavailable outcomes.
	if _, err := app.ReadPrimary(); !errors.Is(err, gpui.ErrPrimarySelectionUnavailable) {
		t.Fatalf("ReadPrimary error = %v, want ErrPrimarySelectionUnavailable", err)
	}
	if err := app.WritePrimary(gpui.NewClipboardStringItem("primary")); !errors.Is(err, gpui.ErrPrimarySelectionUnavailable) {
		t.Fatalf("WritePrimary error = %v, want ErrPrimarySelectionUnavailable", err)
	}
	if _, err := app.ReadFindPasteboard(); !errors.Is(err, gpui.ErrFindPasteboardUnavailable) {
		t.Fatalf("ReadFindPasteboard error = %v, want ErrFindPasteboardUnavailable", err)
	}
	if err := app.WriteFindPasteboard(gpui.NewClipboardStringItem("find")); !errors.Is(err, gpui.ErrFindPasteboardUnavailable) {
		t.Fatalf("WriteFindPasteboard error = %v, want ErrFindPasteboardUnavailable", err)
	}

	// A hostless application reports ErrNoHost for the sync surface
	// and the Unavailable variant through the async surface (consumed
	// through a deterministic test application's scheduler).
	hostless := gpui.NewTestApp()
	if _, err := hostless.App().ReadClipboard(); !errors.Is(err, gpui.ErrNoHost) {
		t.Fatalf("hostless ReadClipboard error = %v, want ErrNoHost", err)
	}
	if err := hostless.App().WriteClipboard(gpui.NewClipboardStringItem("x")); !errors.Is(err, gpui.ErrNoHost) {
		t.Fatalf("hostless WriteClipboard error = %v, want ErrNoHost", err)
	}
	hostlessTask := hostless.App().ReadClipboardAsync()
	if !hostlessTask.Done() {
		t.Fatal("hostless async task is not ready immediately")
	}
	hostlessOutcomes := make(chan gpui.ClipboardReadOutcome, 1)
	hostless.App().Spawn(func(cx *gpui.AsyncApp) gpui.ClipboardReadOutcome {
		outcome := cx.Await(hostlessTask)
		hostlessOutcomes <- outcome
		return outcome
	})
	hostless.RunUntilParked()
	select {
	case outcome := <-hostlessOutcomes:
		if outcome.Err == nil || outcome.Err.Kind != gpui.ClipboardReadUnavailable {
			t.Fatalf("hostless async outcome err = %v, want Unavailable", outcome.Err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out awaiting the hostless clipboard task")
	}
}

// ---------------------------------------------------------------------------
// Native application interop (PowerShell)
// ---------------------------------------------------------------------------

// TestClipboardPowerShellNativeInterop exchanges text with a real
// external application: PowerShell's Set-Clipboard publishes text our
// host reads back exactly, and our write is what Get-Clipboard
// returns (including CJK). Skips only when powershell.exe is absent.
func TestClipboardPowerShellNativeInterop(t *testing.T) {
	h := newHost(t)

	ps, err := exec.LookPath("powershell.exe")
	if err != nil {
		t.Skipf("powershell.exe not found — the native interop check cannot run on this machine: %v", err)
	}

	const text = "clipspec 你好 powershell interop 12345"
	getClipboard := func() (string, error) {
		cmd := exec.Command(ps, "-NoProfile", "-Command",
			"[Console]::OutputEncoding=[Text.Encoding]::UTF8; [Console]::Out.Write((Get-Clipboard -Raw))")
		out, err := cmd.Output()
		return string(out), err
	}

	// An external process writes; our host reads.
	set := exec.Command(ps, "-NoProfile", "-Command", fmt.Sprintf("Set-Clipboard -Value '%s'", text))
	if out, err := set.CombinedOutput(); err != nil {
		t.Fatalf("powershell Set-Clipboard: %v\n%s", err, out)
	}
	item := hostRead(t, h)
	requireStringItem(t, item, text, "")
	clipboardStillOpenable(t)

	// Our host writes; the external process reads.
	hostWrite(t, h, gpui.NewClipboardStringItem(text))
	out, err := getClipboard()
	if err != nil {
		t.Fatalf("powershell Get-Clipboard: %v", err)
	}
	if out != text {
		t.Fatalf("powershell read back %q, want %q", out, text)
	}

	// Our metadata pair does not disturb the external reader: the
	// text still comes through.
	hostWrite(t, h, gpui.NewClipboardStringItemWithMetadata(text, `{"interop":true}`))
	out, err = getClipboard()
	if err != nil {
		t.Fatalf("powershell Get-Clipboard after metadata write: %v", err)
	}
	if out != text {
		t.Fatalf("powershell read back %q after the metadata write, want %q", out, text)
	}
	clipboardStillOpenable(t)
}
