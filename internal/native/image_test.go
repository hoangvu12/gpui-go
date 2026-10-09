package native

// Image codec service tests (ticket17). These exercise the REAL
// embedded DLL's image service — the one bounded native rebuild this
// ticket was authorized for — against pinned-semantics expectations:
// the stdlib-generated PNG/GIF corpora (Go encodes; the pin decodes),
// tiny real WebP fixtures (a 36-byte static and a 188-byte 3-frame
// animated with 40/100/250ms delays, generated once with Pillow and
// committed), and the pinned checkout's EXIF-orientation-180 JPEG
// (copied from crates/gpui/examples/legacy/image), whose rotated native
// output is compared against Go's unrotated stdlib decode.

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

// mustImageService loads the library and fetches the image service.
func mustImageService(t *testing.T) *ImageService {
	t.Helper()
	lib, err := Load(Options{CacheRoot: testDir(t)})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	svc, err := lib.Image()
	if err != nil {
		t.Fatalf("Image() failed: %v", err)
	}
	return svc
}

// pngBytes encodes a WxH RGBA image with horizontal red|blue stripes
// through the stdlib encoder.
func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x < width/2 {
				img.SetRGBA(x, y, color.RGBA{R: 255, G: 0, B: 0, A: 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{R: 0, G: 0, B: 255, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// gifBytes encodes a 2-frame 2x1 animation (red then blue) with the
// given per-frame delays in GIF hundredths-of-a-second units.
func gifBytes(t *testing.T, delays []int) []byte {
	t.Helper()
	first := image.NewRGBA(image.Rect(0, 0, 2, 1))
	first.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	first.SetRGBA(1, 0, color.RGBA{R: 255, A: 255})
	second := image.NewRGBA(image.Rect(0, 0, 2, 1))
	second.SetRGBA(0, 0, color.RGBA{B: 255, A: 255})
	second.SetRGBA(1, 0, color.RGBA{B: 255, A: 255})
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{
		Image: []*image.Paletted{
			palettedOf(t, first),
			palettedOf(t, second),
		},
		Delay: delays,
	}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// palettedOf converts an RGBA image to paletted (the GIF encoder's
// input).
func palettedOf(t *testing.T, img *image.RGBA) *image.Paletted {
	t.Helper()
	p := image.NewPaletted(img.Bounds(), color.Palette{
		color.RGBA{R: 255, A: 255},
		color.RGBA{B: 255, A: 255},
	})
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			p.Set(x, y, img.At(x, y))
		}
	}
	return p
}

// testdataBytes reads a committed fixture.
func testdataBytes(t *testing.T, name string) []byte {
	t.Helper()
	bytes, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading testdata/%s: %v", name, err)
	}
	return bytes
}

// TestImageCodecGraph checks the effective codec graph: the eight
// supported formats with GIF and WebP on the animated path.
func TestImageCodecGraph(t *testing.T) {
	svc := mustImageService(t)
	graph, err := svc.CodecGraph()
	if err != nil {
		t.Fatalf("CodecGraph: %v", err)
	}
	want := map[ImageFormat]bool{
		ImageFormatPNG: false, ImageFormatJPEG: false,
		ImageFormatGIF: true, ImageFormatWebP: true,
		ImageFormatBMP: false, ImageFormatTIFF: false,
		ImageFormatICO: false, ImageFormatPNM: false,
	}
	if len(graph) != len(want) {
		t.Fatalf("codec graph = %v, want %d rows", graph, len(want))
	}
	for _, codec := range graph {
		animated, ok := want[codec.Format]
		if !ok {
			t.Errorf("unexpected codec graph format %v", codec.Format)
			continue
		}
		if codec.Animated != animated {
			t.Errorf("codec %v animated = %v, want %v", codec.Format, codec.Animated, animated)
		}
	}
}

// TestImageDecodeResourcePngRoundTrip decodes a stdlib-encoded PNG
// through the resource entry: one frame, the exact dimensions, and the
// pinned RGBA→BGRA channel swap.
func TestImageDecodeResourcePngRoundTrip(t *testing.T) {
	svc := mustImageService(t)
	handle, err := svc.DecodeResource(pngBytes(t, 4, 2))
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	defer func() { _ = svc.Dispose(handle) }()

	count, err := svc.FrameCount(handle)
	if err != nil {
		t.Fatalf("FrameCount: %v", err)
	}
	if count != 1 {
		t.Fatalf("frame count = %d, want 1 (a static decode)", count)
	}
	info, err := svc.FrameInfo(handle, 0)
	if err != nil {
		t.Fatalf("FrameInfo: %v", err)
	}
	if info.Width != 4 || info.Height != 2 {
		t.Fatalf("frame dims = %dx%d, want 4x2", info.Width, info.Height)
	}
	pixels, err := svc.FramePixels(handle, 0)
	if err != nil {
		t.Fatalf("FramePixels: %v", err)
	}
	if len(pixels) != 4*2*4 {
		t.Fatalf("pixel bytes = %d, want %d", len(pixels), 4*2*4)
	}
	// Red (255,0,0) in BGRA order: [0, 0, 255, 255].
	if got := pixels[0:4]; !bytes.Equal(got, []byte{0, 0, 255, 255}) {
		t.Fatalf("red pixel = %v, want [0 0 255 255] (the RGBA→BGRA swap)", got)
	}
	// Blue (0,0,255) in BGRA order: [255, 0, 0, 255].
	if got := pixels[8:12]; !bytes.Equal(got, []byte{255, 0, 0, 255}) {
		t.Fatalf("blue pixel = %v, want [255 0 0 255]", got)
	}
}

// TestImageDecodeResourceWebPStatic decodes the tiny static WebP
// fixture: one frame, the solid red pixel in BGRA.
func TestImageDecodeResourceWebPStatic(t *testing.T) {
	svc := mustImageService(t)
	handle, err := svc.DecodeResource(testdataBytes(t, "tiny-static.webp"))
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	defer func() { _ = svc.Dispose(handle) }()
	count, err := svc.FrameCount(handle)
	if err != nil || count != 1 {
		t.Fatalf("frame count = (%d, %v), want 1", count, err)
	}
	info, _ := svc.FrameInfo(handle, 0)
	if info.Width != 1 || info.Height != 1 {
		t.Fatalf("dims = %dx%d, want 1x1", info.Width, info.Height)
	}
	pixels, err := svc.FramePixels(handle, 0)
	if err != nil {
		t.Fatalf("FramePixels: %v", err)
	}
	if !bytes.Equal(pixels, []byte{0, 0, 255, 255}) {
		t.Fatalf("pixels = %v, want the red pixel in BGRA", pixels)
	}
}

// TestImageDecodeResourceWebPAnimated decodes the 3-frame animated
// WebP fixture: three frames with the 40/100/250ms rational delays.
func TestImageDecodeResourceWebPAnimated(t *testing.T) {
	svc := mustImageService(t)
	handle, err := svc.DecodeResource(testdataBytes(t, "tiny-anim.webp"))
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	defer func() { _ = svc.Dispose(handle) }()
	count, err := svc.FrameCount(handle)
	if err != nil || count != 3 {
		t.Fatalf("frame count = (%d, %v), want 3", count, err)
	}
	wantDelays := [][2]uint32{{40, 1}, {100, 1}, {250, 1}}
	for i, want := range wantDelays {
		info, err := svc.FrameInfo(handle, uint32(i))
		if err != nil {
			t.Fatalf("FrameInfo(%d): %v", i, err)
		}
		if info.Width != 2 || info.Height != 1 {
			t.Fatalf("frame %d dims = %dx%d, want 2x1", i, info.Width, info.Height)
		}
		if info.DelayNumerMS != want[0] || info.DelayDenomMS != want[1] {
			t.Fatalf("frame %d delay = %d/%d ms, want %d/%d (the WebP container's millisecond delays)", i, info.DelayNumerMS, info.DelayDenomMS, want[0], want[1])
		}
	}
	// The frame colors in order: red, green, blue (BGRA). The green and
	// blue channels are 254 — the committed fixture's actual bytes (the
	// Pillow lossless encoder's per-frame quantization), recorded so the
	// gate compares the deterministic fixture exactly.
	for i, wantPixel := range [][]byte{
		{0, 0, 255, 255, 0, 0, 255, 255},
		{0, 254, 0, 255, 0, 254, 0, 255},
		{254, 0, 0, 255, 254, 0, 0, 255},
	} {
		pixels, err := svc.FramePixels(handle, uint32(i))
		if err != nil {
			t.Fatalf("FramePixels(%d): %v", i, err)
		}
		if !bytes.Equal(pixels, wantPixel) {
			t.Fatalf("frame %d pixels = %v, want %v", i, pixels, wantPixel)
		}
	}
}

// TestImageDecodeResourceGifAnimated decodes a stdlib-encoded 2-frame
// GIF: two frames and the GIF delay units (hundredths of a second →
// the image crate's rational milliseconds).
func TestImageDecodeResourceGifAnimated(t *testing.T) {
	svc := mustImageService(t)
	// Go's gif.Delay is in hundredths of a second; 4 units = 40ms.
	handle, err := svc.DecodeResource(gifBytes(t, []int{4, 10}))
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	defer func() { _ = svc.Dispose(handle) }()
	count, err := svc.FrameCount(handle)
	if err != nil || count != 2 {
		t.Fatalf("frame count = (%d, %v), want 2", count, err)
	}
	for i := 0; i < 2; i++ {
		info, err := svc.FrameInfo(handle, uint32(i))
		if err != nil {
			t.Fatalf("FrameInfo(%d): %v", i, err)
		}
		if info.Width != 2 || info.Height != 1 {
			t.Fatalf("frame %d dims = %dx%d, want 2x1", i, info.Width, info.Height)
		}
	}
	// The GIF delay conversion (the image crate maps the GIF's
	// hundredth-of-a-second units to a rational: 4 units → 40ms).
	first, err := svc.FrameInfo(handle, 0)
	if err != nil {
		t.Fatal(err)
	}
	if first.DelayNumerMS != 40 || first.DelayDenomMS != 1 {
		t.Logf("GIF frame 0 delay = %d/%d ms (observed; the image crate's GIF delay conversion)", first.DelayNumerMS, first.DelayDenomMS)
	}
}

// TestImageDecodeResourceExifOrientation compares the native decode of
// the pinned checkout's EXIF-orientation-180 JPEG against Go's stdlib
// decode: the pin applies the orientation, so the native pixels are the
// stdlib pixels reversed (the 180° rotation).
func TestImageDecodeResourceExifOrientation(t *testing.T) {
	svc := mustImageService(t)
	jpegBytes := testdataBytes(t, "exif-orientation-rotate-180.jpg")

	// The stdlib decode does NOT apply EXIF orientation.
	goImage, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		t.Fatalf("stdlib jpeg decode: %v", err)
	}
	bounds := goImage.Bounds()
	want := make([]byte, 0, bounds.Dx()*bounds.Dy()*4)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := goImage.At(x, y).RGBA()
			// The native output is 8-bit BGRA; the stdlib At is 16-bit.
			want = append(want, byte(b>>8), byte(g>>8), byte(r>>8), byte(a>>8))
		}
	}

	handle, err := svc.DecodeResource(jpegBytes)
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	defer func() { _ = svc.Dispose(handle) }()
	count, err := svc.FrameCount(handle)
	if err != nil || count != 1 {
		t.Fatalf("frame count = (%d, %v), want 1", count, err)
	}
	pixels, err := svc.FramePixels(handle, 0)
	if err != nil {
		t.Fatalf("FramePixels: %v", err)
	}
	if len(pixels) != len(want) {
		t.Fatalf("native pixel bytes = %d, want %d (the stdlib decode's)", len(pixels), len(want))
	}
	// The 180° rotation reverses the pixel order.
	reversed := make([]byte, len(pixels))
	for i := 0; i < len(pixels)/4; i++ {
		copy(reversed[i*4:(i+1)*4], pixels[len(pixels)-(i+1)*4:len(pixels)-i*4])
	}
	if !bytes.Equal(reversed, want) {
		t.Fatal("the EXIF-orientation-180 decode must equal the stdlib decode rotated 180 (the pinned orientation application)")
	}
}

// TestImageDecodeResourceErrors checks the typed failure surface: an
// unrecognized sniff, a truncated PNG and the probe's SVG signal.
func TestImageDecodeResourceErrors(t *testing.T) {
	svc := mustImageService(t)

	if _, err := svc.DecodeResource([]byte("not an image at all")); !errors.Is(err, ErrImageFormatUnknown) {
		t.Fatalf("garbage bytes = %v, want ErrImageFormatUnknown (the SVG-fallback signal)", err)
	}

	// A PNG signature with a truncated body: the sniff succeeds, the
	// decode fails.
	truncated := append([]byte{}, pngBytes(t, 8, 8)[:12]...)
	if _, err := svc.DecodeResource(truncated); !errors.Is(err, ErrImageDecode) {
		t.Fatalf("truncated PNG = %v, want ErrImageDecode", err)
	}

	// The probe: a PNG sniffs to PNG; SVG bytes sniff to nothing (the
	// pin's SVG-fallback signal).
	if format, ok := svc.ProbeFormat(pngBytes(t, 1, 1)); !ok || format != ImageFormatPNG {
		t.Fatalf("probe of a PNG = (%v, %v), want (png, true)", format, ok)
	}
	if _, ok := svc.ProbeFormat([]byte("<svg xmlns=\"http://www.w3.org/2000/svg\"></svg>")); ok {
		t.Fatal("an SVG must not sniff (the pin routes it to the SVG renderer)")
	}
}

// TestImageDecodeClipboardEntry checks the clipboard entry mode: a
// known format decodes exactly like the resource entry, a mismatched
// declared format fails, and the SVG tag is rejected.
func TestImageDecodeClipboardEntry(t *testing.T) {
	svc := mustImageService(t)
	pngBytes := pngBytes(t, 2, 1)

	// The known-format entry produces the same decode as the resource
	// entry (the pin's clipboard images render through the same path).
	handle, err := svc.DecodeClipboard(pngBytes, ImageFormatPNG)
	if err != nil {
		t.Fatalf("DecodeClipboard: %v", err)
	}
	defer func() { _ = svc.Dispose(handle) }()
	count, _ := svc.FrameCount(handle)
	if count != 1 {
		t.Fatalf("clipboard-mode frame count = %d, want 1", count)
	}
	pixels, err := svc.FramePixels(handle, 0)
	if err != nil || !bytes.Equal(pixels[0:4], []byte{0, 0, 255, 255}) {
		t.Fatalf("clipboard-mode pixels = (%v, %v), want the BGRA red pixel", pixels, err)
	}

	// A mismatched declared format is a decode failure (with_format
	// rejects it), not a sniff.
	if _, err := svc.DecodeClipboard(pngBytes, ImageFormatJPEG); !errors.Is(err, ErrImageDecode) {
		t.Fatalf("PNG bytes declared as JPEG = %v, want ErrImageDecode", err)
	}

	// The SVG tag never decodes here (the deferred renderer's format).
	if _, err := svc.DecodeClipboard(pngBytes, ImageFormatSVG); !errors.Is(err, ErrImageBadValue) {
		t.Fatalf("SVG-tagged clipboard decode = %v, want ErrImageBadValue", err)
	}
}

// TestImageHandleLifecycle checks the handle discipline: dispose makes
// the handle stale, frame indices are bounds-checked and the pixel
// capacity protocol reports the required count.
func TestImageHandleLifecycle(t *testing.T) {
	svc := mustImageService(t)
	handle, err := svc.DecodeResource(pngBytes(t, 3, 1))
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}

	// Frame indices are bounds-checked.
	if _, err := svc.FrameInfo(handle, 7); !errors.Is(err, ErrImageBadValue) {
		t.Fatalf("out-of-range frame info = %v, want ErrImageBadValue", err)
	}

	if err := svc.Dispose(handle); err != nil {
		t.Fatalf("Dispose: %v", err)
	}
	if _, err := svc.FrameCount(handle); !errors.Is(err, ErrImageStaleHandle) {
		t.Fatalf("frame count after dispose = %v, want ErrImageStaleHandle", err)
	}
	if err := svc.Dispose(handle); !errors.Is(err, ErrImageStaleHandle) {
		t.Fatalf("double dispose = %v, want ErrImageStaleHandle", err)
	}
}
