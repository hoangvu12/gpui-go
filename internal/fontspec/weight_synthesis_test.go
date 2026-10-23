//go:build windows

// The browser-like bold synthesis gate (zterm ticket 13, native revision
// 14). Fontique 0.11.1 synthesizes bold whenever a request lands on a
// lighter static face; the native text service keeps the synthesis only
// where a browser draws it: the request must be at least 600 (CSS) and
// more than 200 above the matched face's OS/2 weight (Blink
// font_cache_skia.cc @ d139f187, lines 347-353). Otherwise it requests the
// face's own weight, on both the resolve and the per-run shaping paths.
//
// The fixtures are four static Geist faces (testdata/weights, the copies
// zterm bundles; SIL OFL 1.1, testdata/Geist-OFL.txt). Each family of the
// matrix registers a subset under its own renamed family (the name table's
// "Geist" rewritten in place), so the subsets never merge with each other
// or with the "Geist" family the other gates in this package register.
package fontspec

import (
	"bytes"
	"encoding/binary"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

//go:embed testdata/weights/Geist-Regular.ttf
//go:embed testdata/weights/Geist-Medium.ttf
//go:embed testdata/weights/Geist-SemiBold.ttf
//go:embed testdata/weights/Geist-Bold.ttf
var weightFonts embed.FS

// weightFaces maps a static face weight to its fixture file.
var weightFaces = map[int]string{
	400: "Geist-Regular.ttf",
	500: "Geist-Medium.ttf",
	600: "Geist-SemiBold.ttf",
	700: "Geist-Bold.ttf",
}

// renameFamily returns a copy of a font whose name table spells family
// (exactly len("Geist") bytes) wherever it spelled "Geist", in both the
// Windows (UTF-16BE) and the Mac (single-byte) records.
func renameFamily(t *testing.T, data []byte, family string) []byte {
	t.Helper()
	if len(family) != len("Geist") {
		t.Fatalf("renamed family %q must be %d bytes", family, len("Geist"))
	}
	out := bytes.Clone(data)
	tables := int(binary.BigEndian.Uint16(out[4:]))
	for i := 0; i < tables; i++ {
		record := out[12+16*i:]
		if string(record[:4]) != "name" {
			continue
		}
		offset := binary.BigEndian.Uint32(record[8:])
		length := binary.BigEndian.Uint32(record[12:])
		name := out[offset : offset+length]
		utf16 := func(s string) []byte {
			b := make([]byte, 0, 2*len(s))
			for _, c := range []byte(s) {
				b = append(b, 0, c)
			}
			return b
		}
		replaced := bytes.ReplaceAll(name, utf16("Geist"), utf16(family))
		replaced = bytes.ReplaceAll(replaced, []byte("Geist"), []byte(family))
		copy(name, replaced)
		return out
	}
	t.Fatalf("font has no name table")
	return nil
}

// weightSample is one cell of the matrix.
type weightSample struct {
	Family        string  `json:"family"`
	Faces         []int   `json:"faces"`
	Requested     int     `json:"requested"`
	MatchedFace   int     `json:"matched_face"`
	WantSynthesis bool    `json:"want_synthesis"`
	FontID        string  `json:"font_id"`
	FaceFontID    string  `json:"face_font_id"`
	Ink           float64 `json:"ink"`
	FaceInk       float64 `json:"face_ink"`
	InkRatio      float64 `json:"ink_ratio"`
	Width         float32 `json:"width"`
}

// cssMatch is CSS Fonts 4 weight matching over a family's static faces.
func cssMatch(faces []int, desired int) int {
	has := func(w int) bool {
		for _, f := range faces {
			if f == w {
				return true
			}
		}
		return false
	}
	down := func(from int) int {
		for w := from; w >= 100; w -= 100 {
			if has(w) {
				return w
			}
		}
		return 0
	}
	up := func(from int) int {
		for w := from; w <= 900; w += 100 {
			if has(w) {
				return w
			}
		}
		return 0
	}
	switch {
	case desired >= 400 && desired <= 500:
		for w := desired; w <= 500; w += 100 {
			if has(w) {
				return w
			}
		}
		if w := down(desired - 100); w != 0 {
			return w
		}
		return up(600)
	case desired < 400:
		if w := down(desired); w != 0 {
			return w
		}
		return up(desired + 100)
	default:
		if w := up(desired); w != 0 {
			return w
		}
		return down(desired - 100)
	}
}

// TestBoldSynthesisFollowsBrowserRule is the ticket 13 gate: weights
// 400-900 against families with 400 only, 400/600, 400/500/600 and
// 400/700. Where a browser would not synthesize, the run and the resolve
// carry the matched face's own FontId and exactly its ink; where it would
// (more than 200 over the face, from 600 up), they carry a distinct,
// heavier synthesized instance. FONTSPEC_WEIGHTS_OUT names a JSON file
// for the measured matrix.
func TestBoldSynthesisFollowsBrowserRule(t *testing.T) {
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	gray, err := ts.PrepareRasterStyle(gpui.Rgba{A: 1}, gpui.GlyphRenderGrayscale)
	if err != nil {
		t.Fatalf("PrepareRasterStyle: %v", err)
	}
	const sample = "Bold"
	const size = float32(14)

	// shape shapes the sample in one run and returns its fragment font,
	// width and summed grayscale coverage.
	shape := func(family string, weight int) (gpui.FontID, float32, float64) {
		t.Helper()
		line, err := ts.ShapeText(gpui.TextLayoutRequest{
			Text:     sample,
			FontSize: size,
			Runs:     []gpui.TextRun{{Len: len(sample), Font: gpui.FontDescriptor{Family: family, Weight: float32(weight)}}},
		})
		if err != nil {
			t.Fatalf("shape %s %d: %v", family, weight, err)
		}
		defer line.Dispose()
		frags, err := line.Fragments()
		if err != nil || len(frags) != 1 {
			t.Fatalf("fragments %s %d: %v (%d fragments, want 1)", family, weight, err, len(frags))
		}
		glyphs, err := line.Glyphs()
		if err != nil {
			t.Fatalf("glyphs %s %d: %v", family, weight, err)
		}
		ink := 0.0
		for _, g := range glyphs[frags[0].GlyphStart : frags[0].GlyphStart+frags[0].GlyphCount] {
			r, err := ts.RasterizeGlyph(gpui.RasterGlyphParams{
				FontID: frags[0].FontID, GlyphID: g.ID, FontSize: size, ScaleFactor: 1, RasterStyle: gray,
			})
			if err != nil {
				t.Fatalf("rasterize %s %d: %v", family, weight, err)
			}
			for _, p := range r.Pixels {
				ink += float64(p) / 255
			}
		}
		return frags[0].FontID, frags[0].XEnd - frags[0].XStart, ink
	}

	families := []struct {
		name  string
		faces []int
	}{
		{"Wt400", []int{400}},
		{"Wt460", []int{400, 600}},
		{"Wt456", []int{400, 500, 600}},
		{"Wt470", []int{400, 700}},
	}
	var samples []weightSample
	for _, family := range families {
		for _, face := range family.faces {
			data, err := weightFonts.ReadFile("testdata/weights/" + weightFaces[face])
			if err != nil {
				t.Fatalf("reading %s: %v", weightFaces[face], err)
			}
			if _, err := ts.AddFont(renameFamily(t, data, family.name)); err != nil {
				t.Fatalf("AddFont(%s %d): %v", family.name, face, err)
			}
		}
		for weight := 400; weight <= 900; weight += 100 {
			matched := cssMatch(family.faces, weight)
			synth := weight >= 600 && weight > matched+200

			id, width, ink := shape(family.name, weight)
			faceID, _, faceInk := shape(family.name, matched)
			resolved, err := ts.ResolveFont(gpui.FontDescriptor{Family: family.name, Weight: float32(weight)})
			if err != nil {
				t.Fatalf("resolve %s %d: %v", family.name, weight, err)
			}
			if !resolved.FamilyFound {
				t.Fatalf("resolve %s %d: FamilyFound = false (the renamed family did not register)", family.name, weight)
			}
			if resolved.FontID != id {
				t.Errorf("%s %d: resolve FontId %#x differs from the shaped run's %#x", family.name, weight, uint64(resolved.FontID), uint64(id))
			}
			if synth {
				if id == faceID {
					t.Errorf("%s %d over the %d face: FontId %#x is the plain face, want the synthesized bold a browser draws", family.name, weight, matched, uint64(id))
				}
				if ink <= faceInk*1.05 {
					t.Errorf("%s %d over the %d face: ink %.1f not heavier than the face's %.1f", family.name, weight, matched, ink, faceInk)
				}
			} else {
				if id != faceID {
					t.Errorf("%s %d over the %d face: FontId %#x, want the face's own %#x (no synthesis within 200)", family.name, weight, matched, uint64(id), uint64(faceID))
				}
				if ink != faceInk {
					t.Errorf("%s %d over the %d face: ink %.1f, want the face's %.1f", family.name, weight, matched, ink, faceInk)
				}
			}
			samples = append(samples, weightSample{
				Family: family.name, Faces: family.faces, Requested: weight, MatchedFace: matched,
				WantSynthesis: synth, FontID: fmt.Sprintf("%#x", uint64(id)), FaceFontID: fmt.Sprintf("%#x", uint64(faceID)),
				Ink: ink, FaceInk: faceInk, InkRatio: ink / faceInk, Width: width,
			})
			t.Logf("%s %d -> face %d synth=%v id=%#x ink=%.1f face=%.1f ratio=%.3f width=%.2f",
				family.name, weight, matched, synth, uint64(id), ink, faceInk, ink/faceInk, width)
		}
	}
	if out := os.Getenv("FONTSPEC_WEIGHTS_OUT"); out != "" {
		data, err := json.MarshalIndent(samples, "", "  ")
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatalf("write %s: %v", out, err)
		}
	}
}
