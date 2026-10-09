package imagespec

import (
	"bytes"
	"image/jpeg"
	"testing"

	"gpui-go/gpui"
)

// The full EXIF orientation suite: one committed JPEG per orientation
// tag 1..8 (an asymmetric 3x2 image; generated with Pillow's EXIF
// writer). The native decode applies the orientation exactly like the
// pin's apply_orientation, so it must equal the Go stdlib decode (which
// does not) transformed by the EXIF mapping — verified against
// Pillow's exif_transpose ground truth at fixture time.

// exifTransform maps the unoriented pixel grid through the EXIF
// orientation tag (the spec's transforms; verified against Pillow's
// exif_transpose).
func exifTransform(tag int, w, h int, at func(x, y int) [4]byte) (int, int, func(x, y int) [4]byte) {
	switch tag {
	case 1:
		return w, h, func(x, y int) [4]byte { return at(x, y) }
	case 2:
		return w, h, func(x, y int) [4]byte { return at(w-1-x, y) }
	case 3:
		return w, h, func(x, y int) [4]byte { return at(w-1-x, h-1-y) }
	case 4:
		return w, h, func(x, y int) [4]byte { return at(x, h-1-y) }
	case 5:
		return h, w, func(x, y int) [4]byte { return at(y, x) }
	case 6:
		return h, w, func(x, y int) [4]byte { return at(y, h-1-x) }
	case 7:
		return h, w, func(x, y int) [4]byte { return at(w-1-y, h-1-x) }
	case 8:
		return h, w, func(x, y int) [4]byte { return at(w-1-y, x) }
	}
	return w, h, at
}

// TestDecodeResourceEveryExifOrientation runs the suite: the native
// decode of each orientation-tagged JPEG equals the stdlib decode
// transformed by the EXIF mapping.
func TestDecodeResourceEveryExifOrientation(t *testing.T) {
	codec := mustCodec(t)

	for tag := 1; tag <= 8; tag++ {
		tag := tag
		t.Run(orientationName(tag), func(t *testing.T) {
			jpegBytes := fixture(t, exifFixtureName(tag))

			// The stdlib decode: the unoriented pixel grid.
			goImage, err := jpeg.Decode(bytes.NewReader(jpegBytes))
			if err != nil {
				t.Fatalf("stdlib decode: %v", err)
			}
			bounds := goImage.Bounds()
			w, h := bounds.Dx(), bounds.Dy()
			at := func(x, y int) [4]byte {
				r, g, b, a := goImage.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
				return [4]byte{byte(b >> 8), byte(g >> 8), byte(r >> 8), byte(a >> 8)}
			}

			// The expected oriented grid.
			ew, eh, eat := exifTransform(tag, w, h, at)

			render, err := codec.DecodeResource(jpegBytes)
			if err != nil {
				t.Fatalf("DecodeResource: %v", err)
			}
			frame := render.Frames[0]
			if frame.Width != ew || frame.Height != eh {
				t.Fatalf("dims = %dx%d, want the oriented %dx%d", frame.Width, frame.Height, ew, eh)
			}
			// Two independent lossy JPEG decoders (the image crate's
			// zune-jpeg and Go's stdlib) may differ by a rounding step
			// on reconstructed coefficients, so channels compare within
			// a tolerance of 2; the geometry and the transform carry
			// the orientation proof.
			for y := 0; y < eh; y++ {
				for x := 0; x < ew; x++ {
					want := eat(x, y)
					offset := (y*ew + x) * 4
					for c := 0; c < 4; c++ {
						got, expected := int(frame.Pixels[offset+c]), int(want[c])
						if diff := got - expected; diff > 2 || diff < -2 {
							t.Fatalf("pixel (%d,%d) channel %d = %d, want %d±2 (the EXIF %s transform)", x, y, c, got, expected, orientationName(tag))
						}
					}
				}
			}
		})
	}
}

// orientationName names the tag.
func orientationName(tag int) string {
	switch tag {
	case 1:
		return "identity"
	case 2:
		return "flip-horizontal"
	case 3:
		return "rotate-180"
	case 4:
		return "flip-vertical"
	case 5:
		return "transpose"
	case 6:
		return "rotate-90-cw"
	case 7:
		return "transverse"
	case 8:
		return "rotate-270-cw"
	}
	return "unknown"
}

// exifFixtureName names the committed fixture.
func exifFixtureName(tag int) string {
	digits := "0123456789"
	return "exif-orientation-" + string(digits[tag]) + ".jpg"
}

// TestDecodeClipboardStaticWebP checks the ticket's explicit
// clipboard-vs-resource WebP split: a static WebP through the clipboard
// entry decodes exactly like the resource entry (one frame, the pixel,
// the zero delay).
func TestDecodeClipboardStaticWebP(t *testing.T) {
	codec := mustCodec(t)
	webpBytes := fixture(t, "tiny-static.webp")

	resource, err := codec.DecodeResource(webpBytes)
	if err != nil {
		t.Fatalf("DecodeResource: %v", err)
	}
	clipboard, err := codec.DecodeClipboard(webpBytes, gpui.ImageFormatWebP)
	if err != nil {
		t.Fatalf("DecodeClipboard: %v", err)
	}
	if clipboard.FrameCount() != 1 || resource.FrameCount() != 1 {
		t.Fatalf("frames = clipboard %d / resource %d, want 1/1", clipboard.FrameCount(), resource.FrameCount())
	}
	if !bytes.Equal(clipboard.Frames[0].Pixels, resource.Frames[0].Pixels) {
		t.Fatal("the static WebP must decode identically through both entries")
	}
}
