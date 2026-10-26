//go:build windows

// The browser-core ticket 55 renderer gate for 52's handed-off
// leftover: a PIXEL gate for a smoothed content mask. The ABI record
// and the shader have carried the mask's corner smoothing since rev 16
// (ticket 50), but the Go surface could not author a quad mask with
// radii until ticket 55 (gpui.Quad's mask grew MaskRadii and
// MaskCornerSmoothing; the kernel seam packs them into the 56-byte ABI
// mask). This gate authors one through the Go record seam and proves
// the rounded mask clips INLINE (no effect group: the record's own
// mask does the work in the vendored quad pipeline) and that the
// smoothing reshapes the corner - the coverage a plain-rrect mask
// cannot produce.
package rendererspec

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// r55Quad paints one solid quad whose CONTENT MASK carries the rounded
// clip (the record-level inline encoding - no effect group).
func r55Quad(t *testing.T, scene *gpui.Scene, radii gpui.Corners, smoothing float32) {
	t.Helper()
	quad := gpui.Quad{
		Bounds: gpui.Bounds{
			Origin: gpui.Point{X: 20, Y: 20},
			Size:   gpui.Size{Width: 80, Height: 80},
		},
		ContentMask: gpui.Bounds{
			Origin: gpui.Point{X: 20, Y: 20},
			Size:   gpui.Size{Width: 80, Height: 80},
		},
		MaskRadii:           radii,
		MaskCornerSmoothing: smoothing,
		Background:          gpui.SolidBackground(gpui.Hsla{H: 0, S: 1, L: 0.5, A: 1}),
	}
	if err := scene.InsertQuad(quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
}

// TestR55RoundedContentMaskClipsInline: a quad whose content mask
// carries radii clips by the rrect INLINE (the vendored quad
// pipeline's mask SDF - no effect group in the scene): the corner
// outside the rrect reads the window background and the center reads
// the solid quad.
func TestR55RoundedContentMaskClipsInline(t *testing.T) {
	const w, h = 120, 120
	surface := r16Surface(t, "rendererspec r55 rounded mask", 120, 120)
	scene := r16Scene(t)
	radii := gpui.Corners{TopLeft: 24, TopRight: 24, BottomRight: 24, BottomLeft: 24}
	r55Quad(t, scene, radii, 0)
	pixels := r16Render(t, surface, scene, w, h)

	// The corner OUTSIDE the rrect reads the white background: the
	// point (22, 22) is 2px inside the quad but well outside the
	// 24px-radius arc.
	r, g, b, a := pixelAt(pixels, w, 22, 22)
	if !nearByte(r, 255) || !nearByte(g, 255) || !nearByte(b, 255) || a != 255 {
		t.Fatalf("the corner outside the mask rrect reads (%d,%d,%d,%d), want the white background", r, g, b, a)
	}
	// The center reads the solid red quad.
	r, g, b, a = pixelAt(pixels, w, 60, 60)
	if r < 240 || g > 10 || b > 10 || a != 255 {
		t.Fatalf("the mask's center reads (%d,%d,%d,%d), want solid red", r, g, b, a)
	}
	// The straight edge inside the mask reads the quad.
	r, g, b, a = pixelAt(pixels, w, 60, 24)
	if r < 240 || g > 10 || b > 10 {
		t.Fatalf("the top edge inside the mask reads (%d,%d,%d,%d), want solid red", r, g, b, a)
	}
	// No effect groups: the clip is the record's own mask, inline.
	groups, err := scene.EffectGroups()
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Fatalf("effect groups = %d, want none (the inline mask path): %+v", len(groups), groups)
	}
}

// TestR55SmoothedContentMaskReshapesTheCorner: the mask's own corner
// smoothing changes the corner coverage - the smoothed SDF (a
// superellipse between the corner and the straight edges) reads
// measurably different bytes in the corner band than the plain rrect,
// while the deep interior and the far background stay identical. This
// is the pixel gate 52 could not author (the Go paint surface could
// not write mask radii or smoothing until ticket 55).
func TestR55SmoothedContentMaskReshapesTheCorner(t *testing.T) {
	const w, h = 120, 120
	surface := r16Surface(t, "rendererspec r55 smoothed mask", 120, 120)
	radii := gpui.Corners{TopLeft: 24, TopRight: 24, BottomRight: 24, BottomLeft: 24}

	plain := r16Scene(t)
	r55Quad(t, plain, radii, 0)
	plainPixels := r16Render(t, surface, plain, w, h)

	smoothed := r16Scene(t)
	r55Quad(t, smoothed, radii, 0.6)
	smoothedPixels := r16Render(t, surface, smoothed, w, h)

	// The far background and the deep interior are identical (all four
	// channels compared - the quad is red and the background white, so
	// the R channel alone cannot separate them).
	for _, p := range [][2]int{{5, 5}, {100, 100}, {60, 60}, {60, 95}} {
		pr, pg, pb, pa := pixelAt(plainPixels, w, p[0], p[1])
		sr, sg, sb, sa := pixelAt(smoothedPixels, w, p[0], p[1])
		if pr != sr || pg != sg || pb != sb || pa != sa {
			t.Fatalf("pixel (%d,%d) changed outside the corner band: plain (%d,%d,%d,%d) smoothed (%d,%d,%d,%d)",
				p[0], p[1], pr, pg, pb, pa, sr, sg, sb, sa)
		}
	}
	// The corner band differs: the smoothed corner's superellipse arc
	// (smoothing 0.6 -> power ~3.4, reaches 24*1.6) rounds differently
	// than the plain rrect's circular arc, so the anti-aliased corner
	// band reads measurably different bytes (52's own smoothed-clip gate
	// asserts the same shape of fact for the composite path).
	differ := 0
	for y := 20; y < 46; y++ {
		for x := 20; x < 46; x++ {
			i := (y*w + x) * 4
			for lane := 0; lane < 3; lane++ {
				if plainPixels[i+lane] != smoothedPixels[i+lane] {
					differ++
					break
				}
			}
		}
	}
	if differ < 8 {
		t.Fatalf("the smoothing changed only %d corner-band pixels - the smoothed SDF is not being evaluated", differ)
	}
}
