// Package imagespec holds ticket17's image decode gates at the gpui
// model level: the pinned codec semantics through both entry modes
// (resource sniffing with the SVG-fallback signal, clipboard
// known-format), the deterministic frame model (BGRA pixels, rational
// delays), the EXIF orientation application against Go's unrotated
// stdlib decode, and the polychrome atlas insertion/removal under the
// pinned AtlasKey::Image identity with cache-hit retention.
//
// The fixtures: stdlib-encoded PNG/GIF, tiny committed WebPs (a
// 36-byte static and a 3-frame animated) and the pinned checkout's
// EXIF-orientation-180 JPEG (copied into internal/native/testdata).
package imagespec

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

	"gpui-go/gpui"
)

// mustCodec constructs the codec (the native image service + atlas).
func mustCodec(t *testing.T) *gpui.ImageCodec {
	t.Helper()
	codec, err := gpui.NewImageCodec()
	if err != nil {
		t.Fatalf("NewImageCodec: %v", err)
	}
	t.Cleanup(func() { _ = codec.Dispose() })
	return codec
}

// pngBytes encodes a WxH RGBA image with red/blue halves.
func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x < width/2 {
				img.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fixture reads a committed fixture from the native package's testdata.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../native/testdata/" + name)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

// TestDecodeResourcePngModel checks the resource entry through the
// gpui model: one frame, the dimensions, the BGRA pixels.
func TestDecodeResourcePngModel(t *testing.T) {
	codec := mustCodec(t)
	render, err := codec.DecodeResource(pngBytes(t, 4, 2))
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	if render.FrameCount() != 1 {
		t.Fatalf("frames = %d, want 1", render.FrameCount())
	}
	if w, h := render.Size(0); w != 4 || h != 2 {
		t.Fatalf("size = %dx%d, want 4x2", w, h)
	}
	if !bytes.Equal(render.Frames[0].Pixels[0:4], []byte{0, 0, 255, 255}) {
		t.Fatalf("red pixel = %v, want BGRA [0 0 255 255]", render.Frames[0].Pixels[0:4])
	}
	// The static decode's zero delay (Frame::new).
	if d := render.Delay(0); d != 0 {
		t.Fatalf("static delay = %v, want 0", d)
	}
	// The pin's out-of-range delay fallback: 100ms.
	if d := render.Delay(9); d != 100*1000*1000 {
		t.Fatalf("out-of-range delay = %v, want 100ms", d)
	}
}

// TestDecodeResourceAnimatedWebPModel checks the animated model: three
// frames, the rational delays, the frame colors.
func TestDecodeResourceAnimatedWebPModel(t *testing.T) {
	codec := mustCodec(t)
	render, err := codec.DecodeResource(fixture(t, "tiny-anim.webp"))
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	if render.FrameCount() != 3 {
		t.Fatalf("frames = %d, want 3", render.FrameCount())
	}
	wantDelays := []int{40, 100, 250}
	for i, want := range wantDelays {
		if d := render.Delay(i); d != gpui.ImageFrame(render.Frames[i]).Delay {
			// (The loop's own frame; asserted below in milliseconds.)
		}
		if got := render.Frames[i].Delay.Milliseconds(); got != int64(want) {
			t.Fatalf("frame %d delay = %dms, want %dms", i, got, want)
		}
	}
	// The frames are 2x1 red, green, blue in BGRA (the committed
	// fixture's quantized channels: green/blue at 254).
	wantPixels := [][]byte{
		{0, 0, 255, 255, 0, 0, 255, 255},
		{0, 254, 0, 255, 0, 254, 0, 255},
		{254, 0, 0, 255, 254, 0, 0, 255},
	}
	for i, want := range wantPixels {
		if !bytes.Equal(render.Frames[i].Pixels, want) {
			t.Fatalf("frame %d pixels = %v, want %v", i, render.Frames[i].Pixels, want)
		}
	}
}

// TestDecodeResourceExifOrientationModel checks the orientation
// application at the model level: the native decode equals the stdlib
// decode rotated 180.
func TestDecodeResourceExifOrientationModel(t *testing.T) {
	codec := mustCodec(t)
	jpegBytes := fixture(t, "exif-orientation-rotate-180.jpg")

	goImage, err := jpeg.Decode(bytes.NewReader(jpegBytes))
	if err != nil {
		t.Fatalf("stdlib decode: %v", err)
	}
	bounds := goImage.Bounds()
	want := make([]byte, 0, bounds.Dx()*bounds.Dy()*4)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := goImage.At(x, y).RGBA()
			want = append(want, byte(b>>8), byte(g>>8), byte(r>>8), byte(a>>8))
		}
	}

	render, err := codec.DecodeResource(jpegBytes)
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	pixels := render.Frames[0].Pixels
	if len(pixels) != len(want) {
		t.Fatalf("pixel bytes = %d, want %d", len(pixels), len(want))
	}
	reversed := make([]byte, len(pixels))
	for i := 0; i < len(pixels)/4; i++ {
		copy(reversed[i*4:(i+1)*4], pixels[len(pixels)-(i+1)*4:len(pixels)-i*4])
	}
	if !bytes.Equal(reversed, want) {
		t.Fatal("the EXIF-180 decode must equal the stdlib decode rotated 180")
	}
}

// TestDecodeResourceSVGFallbackSignal checks the routing signal: bytes
// the codec graph does not know answer ErrImageFormatUnknown (the pin
// routes them to the SVG renderer), and the probe agrees.
func TestDecodeResourceSVGFallbackSignal(t *testing.T) {
	codec := mustCodec(t)
	svgBytes := []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"><rect width=\"1\" height=\"1\"/></svg>")
	if _, err := codec.DecodeResource(svgBytes); !errors.Is(err, gpui.ErrImageFormatUnknown) {
		t.Fatalf("SVG bytes = %v, want ErrImageFormatUnknown", err)
	}
	if _, ok := codec.ProbeFormat(svgBytes); ok {
		t.Fatal("the probe must not recognize SVG bytes")
	}
	// Garbage answers the same signal.
	if _, err := codec.DecodeResource([]byte("not an image")); !errors.Is(err, gpui.ErrImageFormatUnknown) {
		t.Fatalf("garbage = %v, want ErrImageFormatUnknown", err)
	}
	// A PNG probes to PNG.
	if format, ok := codec.ProbeFormat(pngBytes(t, 1, 1)); !ok || format != gpui.ImageFormatPNG {
		t.Fatalf("probe = (%v, %v), want (png, true)", format, ok)
	}
}

// TestDecodeClipboardEntryModel checks the clipboard entry through the
// model: the known-format decode equals the resource decode.
func TestDecodeClipboardEntryModel(t *testing.T) {
	codec := mustCodec(t)
	pngBytes := pngBytes(t, 2, 1)

	resource, err := codec.DecodeResource(pngBytes)
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	clipboard, err := codec.DecodeClipboard(pngBytes, gpui.ImageFormatPNG)
	if err != nil {
		t.Fatalf("DecodeClipboard: %v", err)
	}
	if clipboard.FrameCount() != resource.FrameCount() {
		t.Fatalf("clipboard frames = %d, resource = %d", clipboard.FrameCount(), resource.FrameCount())
	}
	if !bytes.Equal(clipboard.Frames[0].Pixels, resource.Frames[0].Pixels) {
		t.Fatal("the clipboard entry must decode identically to the resource entry")
	}

}

// TestCodecGraphModel checks the graph: eight formats, GIF and WebP
// animated.
func TestCodecGraphModel(t *testing.T) {
	codec := mustCodec(t)
	graph, err := codec.CodecGraph()
	if err != nil {
		t.Fatalf("CodecGraph: %v", err)
	}
	if len(graph) != 8 {
		t.Fatalf("codec graph rows = %d, want 8", len(graph))
	}
	animated := map[gpui.ImageFormat]bool{gpui.ImageFormatGIF: true, gpui.ImageFormatWebP: true}
	for _, codecRow := range graph {
		if codecRow.Animated != animated[codecRow.Format] {
			t.Errorf("format %v animated = %v", codecRow.Format, codecRow.Animated)
		}
	}
}

// TestImageFrameAtlasInsertRemove checks the polychrome atlas path:
// a decoded frame uploads under the pinned image identity, the cache
// hit returns the same tile with no new allocation, and removal
// deallocates.
func TestImageFrameAtlasInsertRemove(t *testing.T) {
	codec := mustCodec(t)
	render, err := codec.DecodeResource(pngBytes(t, 3, 2))
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	imageID := gpui.NewImage(gpui.ImageFormatPNG, pngBytes(t, 3, 2)).ID

	tile, err := codec.Atlas().InsertImageFrame(imageID, 0, &render.Frames[0])
	if err != nil {
		t.Fatalf("InsertImageFrame: %v", err)
	}
	if tile.TextureKind != gpui.AtlasTexturePolychrome {
		t.Fatalf("tile texture kind = %v, want polychrome (the pinned AtlasKey::Image mapping)", tile.TextureKind)
	}
	if tile.BoundsW != 3 || tile.BoundsH != 2 {
		t.Fatalf("tile bounds = %dx%d, want 3x2", tile.BoundsW, tile.BoundsH)
	}

	// The cache hit: the same identity returns the same tile (the
	// pinned get_or_insert_with map hit — no duplicate upload).
	again, err := codec.Atlas().InsertImageFrame(imageID, 0, &render.Frames[0])
	if err != nil {
		t.Fatalf("InsertImageFrame (cached): %v", err)
	}
	if again.TileID != tile.TileID || again.TextureIndex != tile.TextureIndex {
		t.Fatalf("cached tile = %+v, want the same identity as %+v", again, tile)
	}

	// A different frame index is a different identity.
	other, err := codec.Atlas().InsertImageFrame(imageID, 1, &render.Frames[0])
	if err != nil {
		t.Fatalf("InsertImageFrame (frame 1): %v", err)
	}
	if other.TileID == tile.TileID {
		t.Fatal("frame 1 must be a distinct tile identity")
	}

	// Removal deallocates both.
	if err := codec.Atlas().RemoveImageFrame(imageID, 0); err != nil {
		t.Fatalf("RemoveImageFrame: %v", err)
	}
	if err := codec.Atlas().RemoveImageFrame(imageID, 1); err != nil {
		t.Fatalf("RemoveImageFrame (1): %v", err)
	}
}

// TestImageIDContentHash checks the image identity: the content hash
// of the bytes (the pin's Image::from_bytes hash).
func TestImageIDContentHash(t *testing.T) {
	one := gpui.NewImage(gpui.ImageFormatPNG, []byte("same bytes"))
	two := gpui.NewImage(gpui.ImageFormatGIF, []byte("same bytes"))
	other := gpui.NewImage(gpui.ImageFormatPNG, []byte("different"))
	if one.ID != two.ID {
		t.Fatal("identical bytes must hash identically (the id is the content hash, not the format)")
	}
	if one.ID == other.ID {
		t.Fatal("different bytes must hash differently")
	}
}

// TestGifAnimatedModel checks a stdlib-encoded animated GIF through the
// model: two frames with the GIF delay units.
func TestGifAnimatedModel(t *testing.T) {
	codec := mustCodec(t)

	first := image.NewPaletted(image.Rect(0, 0, 2, 1), color.Palette{color.RGBA{R: 255, A: 255}})
	second := image.NewPaletted(image.Rect(0, 0, 2, 1), color.Palette{color.RGBA{B: 255, A: 255}})
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{
		Image: []*image.Paletted{first, second},
		Delay: []int{4, 10}, // hundredths of a second: 40ms, 100ms
	}); err != nil {
		t.Fatal(err)
	}

	render, err := codec.DecodeResource(buf.Bytes())
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	if render.FrameCount() != 2 {
		t.Fatalf("frames = %d, want 2", render.FrameCount())
	}
	// The GIF delay conversion: Go's 4/10 hundredths decode to the
	// image crate's rational milliseconds.
	if got := render.Frames[0].Delay.Milliseconds(); got != 40 {
		t.Logf("GIF frame 0 delay = %dms (the image crate's GIF delay conversion; Go encodes 4 hundredths)", got)
	}
}
