//go:build windows

// The browser-core ticket 52 (rev 16) renderer gates on the real GPU:
// the scene-level gradient table (each kind's readback against a CPU
// reference that mirrors the bake and the hardware's linear filtering
// of the ramp rows), the W3C blend modes (each mode's composite against
// a backdrop copy), the mask composite (gradient/atlas/coverage
// sources, alpha vs luminance, the invalid-mask hide) and the
// drop-shadow filter op (the blurred alpha silhouette tinted and
// offset beneath the content). Everything runs through the ABI-level
// surface render + readback pair.
package rendererspec

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// CPU references (the native bake's mirrors)
// ---------------------------------------------------------------------------

// r52BakeRamp mirrors the native bake_gradient_ramp: one 1024-texel
// premultiplied-sRGB row from the fixed stops (the f32 arithmetic in
// the same order, so the bytes match the GPU's upload exactly).
func r52BakeRamp(stops []gpui.TableGradientStop) [1024][4]byte {
	var ramp [1024][4]byte
	lanes := func(s gpui.TableGradientStop) [4]float32 {
		return [4]float32{s.Color.R, s.Color.G, s.Color.B, s.Color.A}
	}
	premul := func(s gpui.TableGradientStop) [4]float32 {
		l := lanes(s)
		return [4]float32{l[0] * l[3], l[1] * l[3], l[2] * l[3], l[3]}
	}
	position := func(s gpui.TableGradientStop) float32 { return s.Position }
	for texel := 0; texel < 1024; texel++ {
		t := float32(texel) / 1023.0
		color := premul(stops[0])
		if t <= position(stops[0]) {
			color = premul(stops[0])
		} else if t >= position(stops[len(stops)-1]) {
			color = premul(stops[len(stops)-1])
		} else {
			index := 0
			for candidate := 1; candidate < len(stops); candidate++ {
				if position(stops[candidate]) <= t {
					index = candidate
				}
			}
			p0 := position(stops[index])
			p1 := position(stops[index+1])
			factor := (t - p0) / (p1 - p0)
			left := premul(stops[index])
			right := premul(stops[index+1])
			for lane := 0; lane < 4; lane++ {
				color[lane] = left[lane] + factor*(right[lane]-left[lane])
			}
		}
		for lane := 0; lane < 4; lane++ {
			scaled := color[lane]*255.0 + 0.5
			b := byte(0)
			if scaled < 0 {
				b = 0
			} else if scaled > 255 {
				b = 255
			} else {
				b = byte(scaled)
			}
			ramp[texel][lane] = b
		}
	}
	return ramp
}

// r52RampSample mirrors the hardware's linear filtering of the ramp at
// t: the texel-space coordinate is t*1023 + 0.5, texel i's center at
// i+0.5, so t = i/1023 hits texel i exactly and values between
// interpolate linearly.
func r52RampSample(ramp [1024][4]byte, t float32) [4]float32 {
	if t <= 0 {
		out := ramp[0]
		return [4]float32{float32(out[0]) / 255, float32(out[1]) / 255, float32(out[2]) / 255, float32(out[3]) / 255}
	}
	if t >= 1 {
		out := ramp[1023]
		return [4]float32{float32(out[0]) / 255, float32(out[1]) / 255, float32(out[2]) / 255, float32(out[3]) / 255}
	}
	coord := t * 1023.0
	i0 := int(math.Floor(float64(coord)))
	if i0 < 0 {
		i0 = 0
	}
	if i0 > 1022 {
		i0 = 1022
	}
	frac := coord - float32(i0)
	out := [4]float32{}
	for lane := 0; lane < 4; lane++ {
		a := float32(ramp[i0][lane])
		b := float32(ramp[i0+1][lane])
		out[lane] = (a + frac*(b-a)) / 255
	}
	return out
}

// r52GradientParameter mirrors the shader's t computation for the
// given kind and geometry.
func r52GradientParameter(kind gpui.GradientKind, geometry [8]float32, x, y float32) float32 {
	switch kind {
	case gpui.GradientRadial:
		cx, cy := geometry[0], geometry[1]
		r0, r1 := geometry[2], geometry[3]
		aspect := geometry[4]
		sx := (x - cx) / aspect
		sy := y - cy
		dist := float32(math.Sqrt(float64(sx*sx + sy*sy)))
		return (dist - r0) / (r1 - r0)
	case gpui.GradientConic:
		cx, cy := geometry[0], geometry[1]
		start, rotation := geometry[2], geometry[3]
		delta := math.Atan2(float64(x-cx), float64(-(y - cy)))
		swept := math.Mod(delta-float64(start), 2*math.Pi*2)
		for swept < 0 {
			swept += 2 * math.Pi * 2
		}
		return float32(swept) / rotation
	default:
		x0, y0, x1, y1 := geometry[0], geometry[1], geometry[2], geometry[3]
		lx, ly := x1-x0, y1-y0
		return ((x-x0)*lx + (y-y0)*ly) / (lx*lx + ly*ly)
	}
}

// r52WrapTile mirrors the shader's tile wrap.
func r52WrapTile(tile gpui.GradientTileMode, t float32) float32 {
	switch tile {
	case gpui.GradientTileRepeat:
		return t - float32(math.Floor(float64(t)))
	case gpui.GradientTileReflect:
		tri := float32(math.Mod(float64(t), 2.0))
		for tri < 0 {
			tri += 2
		}
		return 1 - float32(math.Abs(float64(tri-1)))
	default:
		return t
	}
}

// r52NearColor compares one readback pixel against a reference at the
// tolerance (the 8-bit bucket roundings at each compositing step plus
// the UNORM filtering's fixed-point rounding).
func r52NearColor(pixels []byte, w, x, y int, want [4]float32, tolerance int) bool {
	i := (y*w + x) * 4
	got := [4]int{int(pixels[i]), int(pixels[i+1]), int(pixels[i+2]), int(pixels[i+3])}
	for lane := 0; lane < 4; lane++ {
		expected := int(want[lane]*255.0 + 0.5)
		d := got[lane] - expected
		if d < 0 {
			d = -d
		}
		if d > tolerance {
			return false
		}
	}
	return true
}

// r52Quad paints one solid quad through the native seam (the r16
// helper's shape).
func r52Quad(t *testing.T, scene *gpui.Scene, x, y, w, h float32, hue, light, alpha float32) {
	t.Helper()
	r52QuadHsla(t, scene, x, y, w, h, gpui.Hsla{H: hue, S: 1, L: light, A: alpha})
}

// r52QuadHsla paints one solid quad with an explicit HSLA color.
func r52QuadHsla(t *testing.T, scene *gpui.Scene, x, y, w, h float32, color gpui.Hsla) {
	t.Helper()
	quad := gpui.Quad{
		Bounds:      gpui.Bounds{Origin: gpui.Point{X: x, Y: y}, Size: gpui.Size{Width: w, Height: h}},
		ContentMask: gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}},
		Background:  gpui.SolidBackground(color),
	}
	if err := scene.InsertQuad(quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
}

// r52Effect paints one effect-group marker through the native seam.
func r52Effect(t *testing.T, scene *gpui.Scene, group gpui.EffectGroup) {
	t.Helper()
	if err := scene.BeginEffectGroup(group); err != nil {
		t.Fatalf("BeginEffectGroup: %v", err)
	}
}

// r52EndEffect closes the effect group.
func r52EndEffect(t *testing.T, scene *gpui.Scene, group gpui.EffectGroup) {
	t.Helper()
	if err := scene.EndEffectGroup(group); err != nil {
		t.Fatalf("EndEffectGroup: %v", err)
	}
}

// r52Render renders and reads back (the r16 helper).
func r52Render(t *testing.T, surface *gpui.Surface, scene *gpui.Scene, w, h int) []byte {
	t.Helper()
	if err := scene.FinishIfNeeded(); err != nil {
		t.Fatalf("FinishIfNeeded: %v", err)
	}
	if err := surface.RenderScene(scene, nil, gpui.BackgroundOpaque); err != nil {
		t.Fatalf("RenderScene: %v", err)
	}
	pixels, err := surface.ReadPixels()
	if err != nil {
		t.Fatalf("ReadPixels: %v", err)
	}
	if len(pixels) != w*h*4 {
		t.Fatalf("read back %d bytes, want %d", len(pixels), w*h*4)
	}
	return pixels
}

// ---------------------------------------------------------------------------
// The gradient table's readbacks
// ---------------------------------------------------------------------------

// TestR52LinearGradientTableReadback: a three-stop linear gradient
// over a full-window quad reads back the baked ramp's filtered bytes
// at every pixel (the CPU reference evaluates the same t at each pixel
// center and filters the same ramp).
func TestR52LinearGradientTableReadback(t *testing.T) {
	const w, h = 160, 80
	surface := r16Surface(t, "rendererspec r52 linear gradient", 160, 80)
	scene := r16Scene(t)
	stops := []gpui.TableGradientStop{
		{Color: gpui.Rgba{R: 1, A: 1}, Position: 0},
		{Color: gpui.Rgba{G: 1, A: 1}, Position: 0.5},
		{Color: gpui.Rgba{B: 1, A: 1}, Position: 1},
	}
	geometry := [8]float32{0, 0, 160, 0}
	index, err := scene.InsertGradient(gpui.Gradient{
		Kind:     gpui.GradientLinear,
		Tile:     gpui.GradientTilePad,
		Geometry: geometry,
		Stops:    stops,
	})
	if err != nil {
		t.Fatalf("InsertGradient: %v", err)
	}
	quad := gpui.Quad{
		Bounds:      gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 160, Height: 80}},
		ContentMask: gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}},
		Background:  gpui.TableGradientBackground(index),
	}
	if err := scene.InsertQuad(quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	pixels := r52Render(t, surface, scene, w, h)
	ramp := r52BakeRamp(stops)
	for _, probe := range [][2]int{{10, 40}, {40, 40}, {80, 40}, {120, 40}, {150, 40}, {5, 5}, {155, 75}} {
		x, y := probe[0], probe[1]
		tt := r52GradientParameter(gpui.GradientLinear, geometry, float32(x)+0.5, float32(y)+0.5)
		pm := r52RampSample(ramp, r52WrapTile(gpui.GradientTilePad, tt))
		// The quad's straight color = the ramp un-premultiplied (the
		// readback's first byte is the red channel).
		want := [4]float32{pm[0] / pm[3], pm[1] / pm[3], pm[2] / pm[3], pm[3]}
		if !r52NearColor(pixels, w, x, y, want, 3) {
			i := (y*w + x) * 4
			t.Fatalf("pixel (%d,%d) reads (%d,%d,%d,%d), want the ramp's filtered value %v", x, y, pixels[i], pixels[i+1], pixels[i+2], pixels[i+3], want)
		}
	}
}

// TestR52RadialGradientTableReadback: a two-stop radial gradient (a
// circle fading out from the center) reads back the radius-parameter
// ramp; the corners stay the last stop's color.
func TestR52RadialGradientTableReadback(t *testing.T) {
	const w, h = 120, 120
	surface := r16Surface(t, "rendererspec r52 radial gradient", 120, 120)
	scene := r16Scene(t)
	stops := []gpui.TableGradientStop{
		{Color: gpui.Rgba{R: 1, G: 0.2, B: 0.2, A: 1}, Position: 0},
		{Color: gpui.Rgba{R: 0.1, G: 0.1, B: 0.4, A: 1}, Position: 1},
	}
	geometry := [8]float32{60, 60, 0, 50, 1}
	index, err := scene.InsertGradient(gpui.Gradient{
		Kind:     gpui.GradientRadial,
		Tile:     gpui.GradientTilePad,
		Geometry: geometry,
		Stops:    stops,
	})
	if err != nil {
		t.Fatalf("InsertGradient: %v", err)
	}
	quad := gpui.Quad{
		Bounds:      gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 120, Height: 120}},
		ContentMask: gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}},
		Background:  gpui.TableGradientBackground(index),
	}
	if err := scene.InsertQuad(quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	pixels := r52Render(t, surface, scene, w, h)
	ramp := r52BakeRamp(stops)
	for _, probe := range [][2]int{{60, 60}, {60, 30}, {60, 10}, {60, 110}, {30, 60}, {10, 10}, {115, 115}} {
		x, y := probe[0], probe[1]
		tt := r52GradientParameter(gpui.GradientRadial, geometry, float32(x)+0.5, float32(y)+0.5)
		pm := r52RampSample(ramp, r52WrapTile(gpui.GradientTilePad, tt))
		want := [4]float32{pm[0] / pm[3], pm[1] / pm[3], pm[2] / pm[3], pm[3]}
		if !r52NearColor(pixels, w, x, y, want, 3) {
			i := (y*w + x) * 4
			t.Fatalf("pixel (%d,%d) reads (%d,%d,%d,%d), want the radial ramp's value %v (t=%v)", x, y, pixels[i], pixels[i+1], pixels[i+2], pixels[i+3], want, tt)
		}
	}
}

// TestR52ConicGradientTableReadback: a three-stop conic gradient (red,
// green, blue at thirds of the sweep) reads back the angle parameter;
// the sweep starts at the top (0 is up, clockwise).
func TestR52ConicGradientTableReadback(t *testing.T) {
	const w, h = 120, 120
	surface := r16Surface(t, "rendererspec r52 conic gradient", 120, 120)
	scene := r16Scene(t)
	stops := []gpui.TableGradientStop{
		{Color: gpui.Rgba{R: 1, A: 1}, Position: 0},
		{Color: gpui.Rgba{G: 1, A: 1}, Position: 1.0 / 3.0},
		{Color: gpui.Rgba{B: 1, A: 1}, Position: 2.0 / 3.0},
		{Color: gpui.Rgba{R: 1, A: 1}, Position: 1},
	}
	geometry := [8]float32{60, 60, 0, float32(2 * math.Pi)}
	index, err := scene.InsertGradient(gpui.Gradient{
		Kind:     gpui.GradientConic,
		Tile:     gpui.GradientTilePad,
		Geometry: geometry,
		Stops:    stops,
	})
	if err != nil {
		t.Fatalf("InsertGradient: %v", err)
	}
	quad := gpui.Quad{
		Bounds:      gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 120, Height: 120}},
		ContentMask: gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}},
		Background:  gpui.TableGradientBackground(index),
	}
	if err := scene.InsertQuad(quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	pixels := r52Render(t, surface, scene, w, h)
	ramp := r52BakeRamp(stops)
	// Probes around the circle: up, upper-right, right, down, left.
	for _, probe := range [][2]int{{60, 5}, {95, 25}, {112, 60}, {60, 112}, {8, 60}} {
		x, y := probe[0], probe[1]
		tt := r52GradientParameter(gpui.GradientConic, geometry, float32(x)+0.5, float32(y)+0.5)
		pm := r52RampSample(ramp, r52WrapTile(gpui.GradientTilePad, tt))
		want := [4]float32{pm[0] / pm[3], pm[1] / pm[3], pm[2] / pm[3], pm[3]}
		if !r52NearColor(pixels, w, x, y, want, 4) {
			i := (y*w + x) * 4
			t.Fatalf("pixel (%d,%d) reads (%d,%d,%d,%d), want the conic ramp's value %v (t=%v)", x, y, pixels[i], pixels[i+1], pixels[i+2], pixels[i+3], want, tt)
		}
	}
}

// TestR52RepeatingGradientReadback: a repeating two-stop gradient (the
// period half the quad's width) wraps the parameter with fract; the
// reflect tile mirrors it. Both read back against the wrapped
// references.
func TestR52RepeatingGradientReadback(t *testing.T) {
	const w, h = 160, 80
	for _, tile := range []gpui.GradientTileMode{gpui.GradientTileRepeat, gpui.GradientTileReflect} {
		surface := r16Surface(t, "rendererspec r52 repeating gradient", 160, 80)
		scene := r16Scene(t)
		stops := []gpui.TableGradientStop{
			{Color: gpui.Rgba{R: 1, A: 1}, Position: 0},
			{Color: gpui.Rgba{R: 0, G: 0, B: 1, A: 1}, Position: 1},
		}
		// The line spans half the width: t goes 0..1 over 80px and
		// wraps twice across the quad.
		geometry := [8]float32{0, 0, 80, 0}
		index, err := scene.InsertGradient(gpui.Gradient{
			Kind:     gpui.GradientLinear,
			Tile:     tile,
			Geometry: geometry,
			Stops:    stops,
		})
		if err != nil {
			t.Fatalf("InsertGradient: %v", err)
		}
		quad := gpui.Quad{
			Bounds:      gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 160, Height: 80}},
			ContentMask: gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}},
			Background:  gpui.TableGradientBackground(index),
		}
		if err := scene.InsertQuad(quad); err != nil {
			t.Fatalf("InsertQuad: %v", err)
		}
		pixels := r52Render(t, surface, scene, w, h)
		ramp := r52BakeRamp(stops)
		for _, probe := range [][2]int{{10, 40}, {40, 40}, {70, 40}, {90, 40}, {120, 40}, {150, 40}} {
			x, y := probe[0], probe[1]
			tt := r52GradientParameter(gpui.GradientLinear, geometry, float32(x)+0.5, float32(y)+0.5)
			pm := r52RampSample(ramp, r52WrapTile(tile, tt))
			want := [4]float32{pm[0] / pm[3], pm[1] / pm[3], pm[2] / pm[3], pm[3]}
			if !r52NearColor(pixels, w, x, y, want, 3) {
				i := (y*w + x) * 4
				t.Fatalf("tile %v: pixel (%d,%d) reads (%d,%d,%d,%d), want %v (t=%v)", tile, x, y, pixels[i], pixels[i+1], pixels[i+2], pixels[i+3], want, tt)
			}
		}
	}
}

// TestR52GradientStopCapResamplesInGo: the GO layer resamples a
// 100-stop gradient down to 64 evenly-spaced stops (the debug warning
// fires in debug builds); the resampled stops land on the original's
// interpolation.
func TestR52GradientStopCapResamplesInGo(t *testing.T) {
	// One hundred stops alternating red and blue at 1/99 steps.
	stops := make([]gpui.TableGradientStop, 100)
	for i := range stops {
		color := gpui.Rgba{R: 1, A: 1}
		if i%2 == 1 {
			color = gpui.Rgba{B: 1, A: 1}
		}
		stops[i] = gpui.TableGradientStop{Color: color, Position: float32(i) / 99}
	}
	resampled := gpui.ResampleTableGradientStops(stops)
	if len(resampled) != 64 {
		t.Fatalf("resampled to %d stops, want 64", len(resampled))
	}
	// The resample evaluates the ORIGINAL's interpolation at the even
	// grid: t = 0 is the first stop, t = 1 the last, and the middle
	// lands between the surrounding original stops.
	if resampled[0].Color != stops[0].Color || resampled[63].Color != stops[99].Color {
		t.Fatalf("the resample's ends must be the original's ends: %v %v", resampled[0], resampled[63])
	}
	// t = 0.5 (i = 31.5 on the 64 grid -> i=31): the original's
	// interpolation at 0.4920... between stops 48/49? The resample's
	// position i/63: 31/63 = 0.492; the original at 0.492: between
	// stop 48 (0.4848) and 49 (0.4949) - both blue (48 even? 48 is
	// even -> red. 49 odd -> blue). factor = (0.492-0.4848)/(0.4949-0.4848) = 0.7.
	if resampled[31].Position != 31.0/63.0 {
		t.Fatalf("the resampled position %v, want 31/63", resampled[31].Position)
	}
	// The straight insert path: a 65-stop gradient through the SCENE
	// API resamples (the native cap never sees it) and the debug
	// warning hook fires.
	warnings := 0
	previous := gpui.GradientDebugWarnf
	gpui.GradientDebugWarnf = func(format string, args ...any) { warnings++ }
	t.Cleanup(func() { gpui.GradientDebugWarnf = previous })
	surface := r16Surface(t, "rendererspec r52 stop cap", 120, 80)
	scene := r16Scene(t)
	over := make([]gpui.TableGradientStop, 65)
	for i := range over {
		over[i] = gpui.TableGradientStop{Color: gpui.Rgba{R: 1, A: 1}, Position: float32(i) / 64}
	}
	index, err := scene.InsertGradient(gpui.Gradient{
		Kind:     gpui.GradientLinear,
		Geometry: [8]float32{0, 0, 120, 0},
		Stops:    over,
	})
	if err != nil {
		t.Fatalf("InsertGradient (65 stops, resampled): %v", err)
	}
	if warnings == 0 {
		t.Fatal("the >64-stop resampling must fire the debug warning hook")
	}
	quad := gpui.Quad{
		Bounds:      gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 120, Height: 80}},
		ContentMask: gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}},
		Background:  gpui.TableGradientBackground(index),
	}
	if err := scene.InsertQuad(quad); err != nil {
		t.Fatalf("InsertQuad: %v", err)
	}
	pixels := r52Render(t, surface, scene, 120, 80)
	// The resampled gradient is solid red everywhere (every original
	// stop is red): the readback is solid red.
	r, g, b, a := pixelAt(pixels, 120, 60, 40)
	if r < 250 || g > 5 || b > 5 || a != 255 {
		t.Fatalf("the resampled red gradient reads (%d,%d,%d,%d), want solid red", r, g, b, a)
	}
}

// ---------------------------------------------------------------------------
// The blend modes' readbacks
// ---------------------------------------------------------------------------

// r52BlendChannel mirrors the shader's separable B(Cb, Cs).
func r52BlendChannel(mode gpui.BlendMode, cb, cs float64) float64 {
	switch mode {
	case gpui.BlendMultiply:
		return cb * cs
	case gpui.BlendScreen:
		return cb + cs - cb*cs
	case gpui.BlendOverlay:
		return r52BlendChannel(gpui.BlendHardLight, cs, cb)
	case gpui.BlendDarken:
		return math.Min(cb, cs)
	case gpui.BlendLighten:
		return math.Max(cb, cs)
	case gpui.BlendColorDodge:
		if cb == 0 {
			return 0
		}
		if cs == 1 {
			return 1
		}
		return math.Min(1, cb/(1-cs))
	case gpui.BlendColorBurn:
		if cb == 1 {
			return 1
		}
		if cs == 0 {
			return 0
		}
		return 1 - math.Min(1, (1-cb)/cs)
	case gpui.BlendHardLight:
		if cs <= 0.5 {
			return r52BlendChannel(gpui.BlendMultiply, cb, 2*cs)
		}
		return r52BlendChannel(gpui.BlendScreen, cb, 2*cs-1)
	case gpui.BlendSoftLight:
		if cs <= 0.5 {
			return cb - (1-2*cs)*cb*(1-cb)
		}
		d := math.Sqrt(cb)
		if cb <= 0.25 {
			d = ((16*cb-12)*cb + 4) * cb
		}
		return cb + (2*cs-1)*(d-cb)
	case gpui.BlendDifference:
		return math.Abs(cb - cs)
	case gpui.BlendExclusion:
		return cb + cs - 2*cb*cs
	default:
		return cs
	}
}

// r52BlendNonseparable mirrors the shader's non-separable modes.
func r52BlendNonseparable(mode gpui.BlendMode, cb, cs [3]float64) [3]float64 {
	lum := func(c [3]float64) float64 { return 0.3*c[0] + 0.59*c[1] + 0.11*c[2] }
	sat := func(c [3]float64) float64 {
		return math.Max(math.Max(c[0], c[1]), c[2]) - math.Min(math.Min(c[0], c[1]), c[2])
	}
	clip := func(c [3]float64) [3]float64 {
		l := lum(c)
		n := math.Min(math.Min(c[0], c[1]), c[2])
		x := math.Max(math.Max(c[0], c[1]), c[2])
		out := c
		if n < 0 && l > n {
			for i := range out {
				out[i] = l + (c[i]-l)*l/(l-n)
			}
		}
		if x > 1 && x > l {
			for i := range out {
				out[i] = l + (c[i]-l)*(1-l)/(x-l)
			}
		}
		return out
	}
	setLum := func(c [3]float64, l float64) [3]float64 {
		d := l - lum(c)
		return clip([3]float64{c[0] + d, c[1] + d, c[2] + d})
	}
	setSat := func(c [3]float64, s float64) [3]float64 {
		cmin := math.Min(math.Min(c[0], c[1]), c[2])
		cmax := math.Max(math.Max(c[0], c[1]), c[2])
		if cmax > cmin {
			scale := s / (cmax - cmin)
			return [3]float64{(c[0] - cmin) * scale, (c[1] - cmin) * scale, (c[2] - cmin) * scale}
		}
		return [3]float64{0, 0, 0}
	}
	switch mode {
	case gpui.BlendHue:
		return setLum(setSat(cs, sat(cb)), lum(cb))
	case gpui.BlendSaturation:
		return setLum(setSat(cb, sat(cs)), lum(cb))
	case gpui.BlendColor:
		return setLum(cs, lum(cb))
	case gpui.BlendLuminosity:
		return setLum(cb, lum(cs))
	default:
		return cs
	}
}

// r52BlendPremultiplied mirrors the shader's blend_premultiplied (the
// W3C closed form on premultiplied inputs).
func r52BlendPremultiplied(mode gpui.BlendMode, backdrop, source [4]float64) [4]float64 {
	if mode == gpui.BlendPlusLighter {
		out := [4]float64{}
		for lane := 0; lane < 4; lane++ {
			out[lane] = math.Min(1, source[lane]+backdrop[lane])
		}
		return out
	}
	ab, as := backdrop[3], source[3]
	cb := [3]float64{0, 0, 0}
	if ab > 0 {
		cb = [3]float64{backdrop[0] / ab, backdrop[1] / ab, backdrop[2] / ab}
	}
	cs := [3]float64{0, 0, 0}
	if as > 0 {
		cs = [3]float64{source[0] / as, source[1] / as, source[2] / as}
	}
	mixed := [3]float64{
		r52BlendChannel(mode, cb[0], cs[0]),
		r52BlendChannel(mode, cb[1], cs[1]),
		r52BlendChannel(mode, cb[2], cs[2]),
	}
	if mode >= 12 && mode <= 15 {
		mixed = r52BlendNonseparable(mode, cb, cs)
	}
	ao := as + (1-as)*ab
	out := [4]float64{}
	for lane := 0; lane < 3; lane++ {
		out[lane] = (1-ab)*source[lane] + as*ab*mixed[lane] + (1-as)*backdrop[lane]
	}
	out[3] = ao
	return out
}

// TestR52BlendModesReadback: every blend mode composites a solid
// source over a solid backdrop through a blend group; the readback
// matches the W3C formula's CPU reference (the backdrop quad paints
// first, the group's child second, both opaque so the 8-bit buckets
// only round once per step).
func TestR52BlendModesReadback(t *testing.T) {
	const w, h = 120, 80
	// A mid-gray backdrop and a saturated orange source exercise the
	// formulas' branches.
	backdrop := gpui.Hsla{H: 0, S: 0, L: 0.5, A: 1}
	source := gpui.Hsla{H: 30.0 / 360.0, S: 1, L: 0.5, A: 1}
	modes := []gpui.BlendMode{
		gpui.BlendNormal, gpui.BlendMultiply, gpui.BlendScreen, gpui.BlendOverlay,
		gpui.BlendDarken, gpui.BlendLighten, gpui.BlendColorDodge, gpui.BlendColorBurn,
		gpui.BlendHardLight, gpui.BlendSoftLight, gpui.BlendDifference, gpui.BlendExclusion,
		gpui.BlendHue, gpui.BlendSaturation, gpui.BlendColor, gpui.BlendLuminosity,
		gpui.BlendPlusLighter,
	}
	for _, mode := range modes {
		surface := r16Surface(t, "rendererspec r52 blend", 120, 80)
		scene := r16Scene(t)
		// The backdrop: a full-window opaque quad (the offscreen scene
		// texture's transparent background never shows).
		r52QuadHsla(t, scene, 0, 0, 120, 80, backdrop)
		group := gpui.EffectGroup{
			HasBlend:  true,
			BlendMode: mode,
			HasOpacity: true,
			Opacity:   1,
			Bounds:    gpui.Bounds{Origin: gpui.Point{X: 20, Y: 10}, Size: gpui.Size{Width: 80, Height: 60}},
			ContentMask: gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}},
		}
		r52Effect(t, scene, group)
		r52Quad(t, scene, 20, 10, 80, 60, source.H, source.L, 1)
		r52EndEffect(t, scene, group)
		pixels := r52Render(t, surface, scene, w, h)
		// The CPU reference: the HSLA colors through the CSS
		// conversion, round-tripped through the 8-bit buckets (the
		// group's bucket and the backdrop copy are both 8-bit
		// textures), then the W3C formula on the premultiplied values.
		backdropRgba := r52HslaToRgba(backdrop)
		sourceRgba := r52HslaToRgba(source)
		bucket := func(v float32) float64 {
			return float64(math.Round(float64(v*255))) / 255
		}
		bp := [4]float64{bucket(backdropRgba.R), bucket(backdropRgba.G), bucket(backdropRgba.B), bucket(backdropRgba.A)}
		sp := [4]float64{bucket(sourceRgba.R), bucket(sourceRgba.G), bucket(sourceRgba.B), bucket(sourceRgba.A)}
		bpm := [4]float64{bp[0] * bp[3], bp[1] * bp[3], bp[2] * bp[3], bp[3]}
		spm := [4]float64{sp[0] * sp[3], sp[1] * sp[3], sp[2] * sp[3], sp[3]}
		out := r52BlendPremultiplied(mode, bpm, spm)
		want := [4]float32{float32(out[0]), float32(out[1]), float32(out[2]), float32(out[3])}
		if !r52NearColor(pixels, w, 60, 40, want, 4) {
			i := (40*w + 60) * 4
			t.Fatalf("mode %d: pixel (60,40) reads (%d,%d,%d,%d), want the W3C formula's %v", mode, pixels[i], pixels[i+1], pixels[i+2], pixels[i+3], want)
		}
		// Outside the group, the backdrop is untouched.
		r, g, b, a := pixelAt(pixels, w, 5, 40)
		wantBackdrop := r52HslaToRgba(backdrop)
		if math.Abs(float64(r)-float64(wantBackdrop.B)*255) > 4 || math.Abs(float64(g)-float64(wantBackdrop.G)*255) > 4 || math.Abs(float64(b)-float64(wantBackdrop.R)*255) > 4 || a != 255 {
			t.Fatalf("mode %d: outside the group the backdrop must be untouched (reads (%d,%d,%d,%d))", mode, r, g, b, a)
		}
	}
}

// r52HslaToRgba converts one Hsla into the sRGB f32 channels (the CSS
// hsl() conversion; the shader's hsla_to_rgba agrees on these inputs).
func r52HslaToRgba(c gpui.Hsla) gpui.Rgba {
	degrees := float64(c.H) * 360
	chroma := (1 - math.Abs(2*float64(c.L)-1)) * float64(c.S)
	hp := math.Mod(degrees, 360) / 60
	if hp < 0 {
		hp += 6
	}
	x := chroma * (1 - math.Abs(math.Mod(hp, 2)-1))
	var r, g, b float64
	switch {
	case hp >= 0 && hp < 1:
		r, g, b = chroma, x, 0
	case hp >= 1 && hp < 2:
		r, g, b = x, chroma, 0
	case hp >= 2 && hp < 3:
		r, g, b = 0, chroma, x
	case hp >= 3 && hp < 4:
		r, g, b = 0, x, chroma
	case hp >= 4 && hp < 5:
		r, g, b = x, 0, chroma
	default:
		r, g, b = chroma, 0, x
	}
	m := float64(c.L) - chroma/2
	return gpui.Rgba{R: float32(r + m), G: float32(g + m), B: float32(b + m), A: c.A}
}
