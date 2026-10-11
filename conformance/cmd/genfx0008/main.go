// Command genfx0008 generates the fx-0008 image-codecs fixture envelope:
// the committed synthetic test images (PNG with alpha, BMP, an animated
// GIF with distinct rational delays) encoded alongside the existing
// committed EXIF/WebP corpus, all as hex bytes in one self-contained
// envelope (both the reference harness and the Go conformance test
// decode the same bytes — the envelope is the portable input, never an
// expected output).
//
// The image bytes are produced with Go's stdlib encoders (image/png,
// image/gif) and a hand-written minimal BMP; every byte sequence is a
// standard encoding the pinned image crate decodes. Run once; the
// output is committed:
//
//	go run ./conformance/cmd/genfx0008 -out conformance/fixtures/fx-0008-image-codecs.json
package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"os"
	"path/filepath"
)

type caseOut struct {
	Label string `json:"label"`
	Bytes string `json:"bytes"`
}

type inputsOut struct {
	ImageCases struct {
		Cases []caseOut `json:"cases"`
	} `json:"image_cases"`
}

type envelopeOut struct {
	Schema      string    `json:"schema"`
	FixtureID   string    `json:"fixture_id"`
	FixtureKind string    `json:"fixture_kind"`
	Title       string    `json:"title"`
	Profile     string    `json:"profile"`
	Caps        []string  `json:"capabilities"`
	Environment []string  `json:"environment"`
	Inputs      inputsOut `json:"inputs"`
}

func main() {
	out := flag.String("out", "conformance/fixtures/fx-0008-image-codecs.json", "output envelope path")
	flag.Parse()

	cases := []struct {
		label string
		bytes []byte
	}{
		{"png-alpha-16x16", pngAlpha()},
		{"bmp-16x16", bmp16()},
		{"gif-anim-3", gifAnim()},
	}

	// The committed corpus (ticket 17's testdata) rides in as-is: the
	// EXIF orientations and the static/animated WebPs.
	for label, path := range map[string]string{
		"jpeg-exif-1": "internal/native/testdata/exif-orientation-1.jpg",
		"jpeg-exif-6": "internal/native/testdata/exif-orientation-6.jpg",
		"webp-static": "internal/native/testdata/tiny-static.webp",
		"webp-anim":   "internal/native/testdata/tiny-anim.webp",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			fatal(fmt.Errorf("reading %s: %w", path, err))
		}
		cases = append(cases, struct {
			label string
			bytes []byte
		}{label, raw})
	}

	// Stable order: the synthetic set, then the corpus in label order.
	// (A fresh slice: appending into cases[:3] would alias-overwrite
	// the corpus entries sharing the backing array.)
	corpusOrder := []string{"jpeg-exif-1", "jpeg-exif-6", "webp-static", "webp-anim"}
	ordered := make([]struct {
		label string
		bytes []byte
	}, 0, len(cases))
	ordered = append(ordered, cases[:3]...)
	for _, label := range corpusOrder {
		for _, c := range cases[3:] {
			if c.label == label {
				ordered = append(ordered, c)
			}
		}
	}

	env := envelopeOut{
		Schema:      "gpui-go/conformance/envelope@1",
		FixtureID:   "fx-0008-image-codecs",
		FixtureKind: "image-codecs-v1",
		Title:       "Image decode graph: format sniffing, EXIF orientation and animated frame walks (BGRA output)",
		Profile:     "test",
		Caps:        []string{"image-codecs"},
		Environment: []string{"windows"},
	}
	env.Inputs.ImageCases.Cases = make([]caseOut, 0, len(ordered))
	for _, c := range ordered {
		env.Inputs.ImageCases.Cases = append(env.Inputs.ImageCases.Cases, caseOut{
			Label: c.label,
			Bytes: hex.EncodeToString(c.bytes),
		})
	}

	data, err := json.MarshalIndent(&env, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		fatal(err)
	}
	fmt.Printf("wrote %s (%d cases)\n", *out, len(ordered))
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "genfx0008:", err)
	os.Exit(1)
}

// pngAlpha renders a 16x16 RGBA PNG with a deterministic gradient (an
// alpha ramp plus distinct color ramps — alpha survives the BGRA
// roundtrip observably).
func pngAlpha() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(x * 17),
				G: uint8(y * 17),
				B: uint8((x + y) * 8),
				A: uint8(255 - x*8),
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		fatal(err)
	}
	return buf.Bytes()
}

// bmp16 writes a minimal 24-bit bottom-up BMP (BITMAPINFOHEADER, no
// palette, rows padded to 4 bytes — 16x16x3 = 48 bytes per row, already
// a multiple of 4).
func bmp16() []byte {
	const w, h = 16, 16
	rowBytes := w * 3
	imageBytes := rowBytes * h
	var buf bytes.Buffer
	// BITMAPFILEHEADER (14 bytes).
	buf.Write([]byte{'B', 'M'})
	writeU32(&buf, 14+40+uint32(imageBytes))
	writeU16(&buf, 0)
	writeU16(&buf, 0)
	writeU32(&buf, 14+40)
	// BITMAPINFOHEADER (40 bytes).
	writeU32(&buf, 40)
	writeI32(&buf, int32(w))
	writeI32(&buf, int32(h))
	writeU16(&buf, 1)
	writeU16(&buf, 24)
	writeU32(&buf, 0) // BI_RGB
	writeU32(&buf, uint32(imageBytes))
	writeI32(&buf, 2835)
	writeI32(&buf, 2835)
	writeU32(&buf, 0)
	writeU32(&buf, 0)
	// Bottom-up BGR rows.
	for y := h - 1; y >= 0; y-- {
		for x := 0; x < w; x++ {
			buf.Write([]byte{byte(255 - x*16), byte(y * 16), byte((x * y * 7) % 256)})
		}
	}
	return buf.Bytes()
}

// gifAnim writes a 3-frame animated GIF with distinct rational delays
// (20ms, 40ms, 100ms — GIF's 10ms units) and distinct per-frame
// content.
func gifAnim() []byte {
	frames := []*image.Paletted{
		paletteFrame(0),
		paletteFrame(85),
		paletteFrame(170),
	}
	delays := []int{2, 4, 10} // GIF delay units (10ms each).
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{
		Image: frames,
		Delay: delays,
	}); err != nil {
		fatal(err)
	}
	return buf.Bytes()
}

func paletteFrame(base int) *image.Paletted {
	palette := make([]color.Color, 0, 4)
	for i := 0; i < 4; i++ {
		v := uint8(base + i*20)
		palette = append(palette, color.RGBA{R: v, G: uint8(255 - v), B: v, A: 255})
	}
	img := image.NewPaletted(image.Rect(0, 0, 12, 12), palette)
	for y := 0; y < 12; y++ {
		for x := 0; x < 12; x++ {
			img.SetColorIndex(x, y, uint8((x/4+y/4)%4))
		}
	}
	return img
}

func writeU16(buf *bytes.Buffer, v uint16) {
	buf.Write([]byte{byte(v), byte(v >> 8)})
}

func writeU32(buf *bytes.Buffer, v uint32) {
	buf.Write([]byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)})
}

func writeI32(buf *bytes.Buffer, v int32) {
	writeU32(buf, uint32(v))
}
