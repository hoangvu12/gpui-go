//go:build windows

// The browser-core ticket 51 (rev 16) renderer gates on the real GPU:
// the transformed-group composite - translate, scale, rotate and skew -
// each verified against a CPU reference computed at the SAME sampling
// (the untransformed subtree read back once, then inverse-mapped and
// bilinearly resampled per output pixel), and the property-node update
// (scene_update_property) redrawing the same scene handle with damage.
package rendererspec

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// r16LocalQuads paints the transformed-group test subtree: an opaque
// white background quad plus two overlapping solid quads (red under
// blue), all in the group's LOCAL frame.
func r16LocalQuads(t *testing.T, scene *gpui.Scene) {
	t.Helper()
	r16Quad(t, scene, 0, 0, 60, 60, 0, 1.0)         // white
	r16Quad(t, scene, 0, 0, 40, 40, 0, 0.5)         // red
	r16Quad(t, scene, 20, 20, 40, 40, 2.0/3.0, 0.5) // blue
}

// r16MatrixAbout builds the row-major 2x3 that applies m2 about the
// local point origin and places that point at the device position at.
func r16MatrixAbout(m2 [2][2]float32, origin [2]float32, at [2]float32) gpui.TransformationMatrix {
	// device = m2 . (local - origin) + origin + at
	//        = m2 . local + (origin + at - m2 . origin)
	mx := [2]float32{
		m2[0][0]*origin[0] + m2[0][1]*origin[1],
		m2[1][0]*origin[0] + m2[1][1]*origin[1],
	}
	return gpui.TransformationMatrix{
		RotationScale: m2,
		Translation: [2]float32{
			origin[0] + at[0] - mx[0],
			origin[1] + at[1] - mx[1],
		},
	}
}

// r16Bilinear samples the premultiplied bucket image (width x height
// RGBA) with the GPU's bilinear filtering at the continuous texel
// coordinate x, y (texel centers at i+0.5; the WRAP addressing the
// composite sampler uses).
func r16Bilinear(bucket []byte, width, height int, x, y float64) (float64, float64, float64, float64) {
	wrap := func(v float64, n int) int {
		v = math.Mod(v, float64(n))
		if v < 0 {
			v += float64(n)
		}
		return int(v)
	}
	frac := func(v float64) (int, int, float64) {
		i := int(math.Floor(v - 0.5))
		f := v - 0.5 - float64(i)
		return i, i + 1, f
	}
	x0, x1, fx := frac(x)
	y0, y1, fy := frac(y)
	xt := [2]int{wrap(float64(x0), width), wrap(float64(x1), width)}
	yt := [2]int{wrap(float64(y0), height), wrap(float64(y1), height)}
	sample := func(tx, ty int) [4]float64 {
		i := (ty*width + tx) * 4
		return [4]float64{
			float64(bucket[i]),
			float64(bucket[i+1]),
			float64(bucket[i+2]),
			float64(bucket[i+3]),
		}
	}
	var out [4]float64
	for c := 0; c < 4; c++ {
		top := sample(xt[0], yt[0])[c]*(1-fx) + sample(xt[1], yt[0])[c]*fx
		bottom := sample(xt[0], yt[1])[c]*(1-fx) + sample(xt[1], yt[1])[c]*fx
		out[c] = top*(1-fy) + bottom*fy
	}
	return out[0], out[1], out[2], out[3]
}

// r16TransformReference computes the expected window pixels for a
// transformed-group composite: the bucket content (the untransformed
// subtree, opaque, in its LOCAL frame; transparent elsewhere) drawn
// under the row-major 2x3 matrix over the opaque white clear, with the
// same bilinear sampling the composite uses. The bucket's size is the
// 256-px quantization of the 60-px local extent (the walk's rule).
func r16TransformReference(t *testing.T, w, h int, base []byte, matrix gpui.TransformationMatrix) []byte {
	t.Helper()
	const bucket = 256
	// The bucket: the local frame's [0, 60] content from the base
	// readback (the same records rendered without the group), the rest
	// transparent.
	bucketImage := make([]byte, bucket*bucket*4)
	for y := 0; y < 60; y++ {
		for x := 0; x < 60; x++ {
			src := (y*w + x) * 4
			dst := (y*bucket + x) * 4
			bucketImage[dst] = base[src]
			bucketImage[dst+1] = base[src+1]
			bucketImage[dst+2] = base[src+2]
			bucketImage[dst+3] = base[src+3]
		}
	}
	m2 := [2][2]float64{
		{float64(matrix.RotationScale[0][0]), float64(matrix.RotationScale[0][1])},
		{float64(matrix.RotationScale[1][0]), float64(matrix.RotationScale[1][1])},
	}
	tx := float64(matrix.Translation[0])
	ty := float64(matrix.Translation[1])
	det := m2[0][0]*m2[1][1] - m2[0][1]*m2[1][0]
	inv := [2][2]float64{
		{m2[1][1] / det, -m2[0][1] / det},
		{-m2[1][0] / det, m2[0][0] / det},
	}
	out := make([]byte, w*h*4)
	for py := 0; py < h; py++ {
		for px := 0; px < w; px++ {
			dx := float64(px) + 0.5 - tx
			dy := float64(py) + 0.5 - ty
			lx := inv[0][0]*dx + inv[0][1]*dy
			ly := inv[1][0]*dx + inv[1][1]*dy
			// The mapped quad covers the bucket exactly: pixels whose
			// inverse falls outside are not drawn (the white clear).
			if lx < 0 || lx >= bucket || ly < 0 || ly >= bucket {
				i := (py*w + px) * 4
				out[i], out[i+1], out[i+2], out[i+3] = 255, 255, 255, 255
				continue
			}
			b, g, r, a := r16Bilinear(bucketImage, bucket, bucket, lx, ly)
			// The composite's premultiplied output over the white clear
			// (the bilinear works in 0..255 byte values; the alpha
			// normalizes for the over compositing).
			alpha := a / 255
			i := (py*w + px) * 4
			out[i] = clampByte255(b + (1-alpha)*255)
			out[i+1] = clampByte255(g + (1-alpha)*255)
			out[i+2] = clampByte255(r + (1-alpha)*255)
			out[i+3] = 255
		}
	}
	return out
}

func clampByte255(v float64) byte {
	v = math.Round(v)
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

// r16CheckTransformComposite renders the transformed group and the
// group-free control, computes the CPU reference and compares.
func r16CheckTransformComposite(t *testing.T, name string, matrix gpui.TransformationMatrix) {
	const w, h = 640, 480
	surface := r16Surface(t, "rendererspec r16 transform "+name, 640, 480)

	// The control: the same records with no group - the untransformed
	// local content at the window origin.
	control := r16Scene(t)
	r16LocalQuads(t, control)
	base := r16RenderAt(t, surface, control, nil, w, h)

	// The transformed group: the local-frame subtree under the matrix.
	scene := r16Scene(t)
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	group := gpui.EffectGroup{
		HasTransform: true,
		// The composite always multiplies by the record's opacity - 1
		// for a transform-only group.
		Opacity:     1,
		Transform:   matrix,
		Bounds:      gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 60, Height: 60}},
		ContentMask: fullMask,
	}
	if err := scene.BeginEffectGroup(group); err != nil {
		t.Fatalf("BeginEffectGroup: %v", err)
	}
	r16LocalQuads(t, scene)
	end := group
	end.IsStart = false
	if err := scene.EndEffectGroup(end); err != nil {
		t.Fatalf("EndEffectGroup: %v", err)
	}
	pixels := r16RenderAt(t, surface, scene, nil, w, h)

	reference := r16TransformReference(t, w, h, base, matrix)
	diff, worst, count := 0, 0, 0
	for i := 0; i < w*h*4; i++ {
		if pixels[i] == reference[i] {
			continue
		}
		d := int(pixels[i]) - int(reference[i])
		if d < 0 {
			d = -d
		}
		if d > worst {
			worst = d
		}
		if d > 8 {
			count++
		}
		diff++
	}
	if count > w*h*4/2000 { // 0.05% of bytes may exceed the tolerance
		t.Fatalf("%s composite: %d bytes differ beyond 8 (worst %d, total differing %d) - the GPU composite does not match the CPU reference at the same sampling", name, count, worst, diff)
	}
}

// TestR16TranslateComposite: a translation-only transform group
// composites through the axis-aligned path at the translated rect.
func TestR16TranslateComposite(t *testing.T) {
	r16CheckTransformComposite(t, "translate", r16MatrixAbout(
		[2][2]float32{{1, 0}, {0, 1}},
		[2]float32{30, 30},
		[2]float32{150, 100},
	))
}

// TestR16ScaleComposite: an anisotropic scale under the matrix.
func TestR16ScaleComposite(t *testing.T) {
	r16CheckTransformComposite(t, "scale", r16MatrixAbout(
		[2][2]float32{{1.5, 0}, {0, 0.75}},
		[2]float32{30, 30},
		[2]float32{150, 100},
	))
}

// TestR16RotateComposite: a 30-degree rotation under the matrix.
func TestR16RotateComposite(t *testing.T) {
	angle := float32(30 * math.Pi / 180)
	r16CheckTransformComposite(t, "rotate", r16MatrixAbout(
		[2][2]float32{{float32(math.Cos(float64(angle))), float32(-math.Sin(float64(angle)))}, {float32(math.Sin(float64(angle))), float32(math.Cos(float64(angle)))}},
		[2]float32{30, 30},
		[2]float32{200, 150},
	))
}

// TestR16SkewComposite: a horizontal skew under the matrix.
func TestR16SkewComposite(t *testing.T) {
	r16CheckTransformComposite(t, "skew", r16MatrixAbout(
		[2][2]float32{{1, float32(math.Tan(20 * math.Pi / 180))}, {0, 1}},
		[2]float32{30, 30},
		[2]float32{250, 150},
	))
}

// TestR16RotatedClipComposite: a transform group that clips draws the
// content clipped to the rotated rounded clip (the rrect coverage
// over the record's content mask, in the local frame): the mapped
// box's center keeps the content, its corners drop to the background.
func TestR16RotatedClipComposite(t *testing.T) {
	const w, h = 640, 480
	surface := r16Surface(t, "rendererspec r16 rotated clip", 640, 480)
	scene := r16Scene(t)
	angle := float64(30 * math.Pi / 180)
	group := gpui.EffectGroup{
		HasTransform: true,
		Opacity:      1,
		Transform: r16MatrixAbout(
			[2][2]float32{
				{float32(math.Cos(angle)), float32(-math.Sin(angle))},
				{float32(math.Sin(angle)), float32(math.Cos(angle))},
			},
			[2]float32{30, 30},
			[2]float32{300, 200},
		),
		ClipToBounds: true,
		Bounds:       gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 60, Height: 60}},
		// The clip rect the coverage evaluates (the border box) with
		// the record's radii.
		ContentMask: gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 60, Height: 60}},
		CornerRadii: gpui.Corners{TopLeft: 24, TopRight: 24, BottomRight: 24, BottomLeft: 24},
	}
	if err := scene.BeginEffectGroup(group); err != nil {
		t.Fatalf("BeginEffectGroup: %v", err)
	}
	r16LocalQuads(t, scene)
	end := group
	end.IsStart = false
	if err := scene.EndEffectGroup(end); err != nil {
		t.Fatalf("EndEffectGroup: %v", err)
	}
	pixels := r16RenderAt(t, surface, scene, nil, w, h)
	// The rotated box's center: r16MatrixAbout places the LOCAL ORIGIN
	// at (300, 200), so the box's center (30, 30) maps to (330, 230) -
	// the blue overlap.
	r, g, b, a := pixelAt(pixels, w, 330, 230)
	if b < 240 || r > 10 || g > 10 || a != 255 {
		t.Fatalf("the rotated clip's center reads (%d,%d,%d,%d), want solid blue", r, g, b, a)
	}
	// The local (0, 0) corner rotates to ~(319, 189); the 24-px radii
	// round the local corners away, so the mapped corner region drops
	// to the white background (the rrect clip in the local frame).
	cr, cg, cb, ca := pixelAt(pixels, w, 319, 189)
	if cr < 240 || cg < 240 || cb < 240 || ca != 255 {
		t.Fatalf("the rotated clip's corner region reads (%d,%d,%d,%d), want the white background (the rrect clip)", cr, cg, cb, ca)
	}
}
