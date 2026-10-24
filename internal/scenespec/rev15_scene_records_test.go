//go:build windows

// The browser-core ticket 39 (rev 15) scene-record gates: the gradient
// background record (a two-stop linear gradient with CSS semantics), the
// colour-matrix filter payload, the per-sprite pixelated sampling flag,
// and the drop-shadow border-box clip-out on the real GPU. These run
// through the native ABI directly (the Go styling layer that authors
// gradients, filters and image-rendering is the later browser-core
// ticket; this package pins what the ABI record must carry).
package scenespec

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/native"
)

// nativeScene opens a native scene service and scene, disposed at
// cleanup.
func nativeScene(t *testing.T) *native.Scene {
	t.Helper()
	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Fatalf("native.Load: %v", err)
	}
	svc, err := lib.Scene()
	if err != nil {
		t.Fatalf("lib.Scene: %v", err)
	}
	sc, err := svc.CreateScene()
	if err != nil {
		t.Fatalf("CreateScene: %v", err)
	}
	t.Cleanup(func() { _ = sc.Dispose() })
	return sc
}

// gradientQuad returns a quad record with a two-stop gradient background
// (red to blue, 90deg = to the right) and a solid border color.
func gradientQuad() native.SceneQuadRecord {
	rec := native.NewSceneQuadRecord()
	rec.Bounds = native.SceneBoundsOf(10, 10, 100, 50)
	rec.ContentMask = native.SceneBoundsOf(0, 0, 1000, 1000)
	rec.Background = native.SceneGradientBackground(
		90,
		native.HslaF{H: 0, S: 1, L: 0.5, A: 1}, 0,
		native.HslaF{H: 2.0 / 3.0, S: 1, L: 0.5, A: 1}, 1,
	)
	rec.BorderColor = native.SceneBackgroundOf(0, 0, 0, 1)
	rec.BorderWidths = [4]uint32{
		math.Float32bits(1), math.Float32bits(1), math.Float32bits(1), math.Float32bits(1),
	}
	return rec
}

// TestGradientBackgroundRecordRoundTrips: a quad carrying a two-stop
// linear gradient inserts, finishes, dumps and replays with every
// gradient field intact (the kernel assigns only the order), and the
// compiled plan still draws it as one quad batch.
func TestGradientBackgroundRecordRoundTrips(t *testing.T) {
	sc := nativeScene(t)
	rec := gradientQuad()
	if err := sc.InsertQuad(&rec); err != nil {
		t.Fatalf("InsertQuad(gradient): %v", err)
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	quads, err := sc.DumpQuads()
	if err != nil {
		t.Fatalf("DumpQuads: %v", err)
	}
	if len(quads) != 1 {
		t.Fatalf("dumped %d quads, want 1", len(quads))
	}
	got := quads[0].Background
	want := rec.Background
	if got != want {
		t.Fatalf("gradient background did not round-trip:\n got %+v\nwant %+v", got, want)
	}
	if quads[0].Order == 0 {
		t.Fatal("the kernel did not assign a draw order")
	}
	commands, err := sc.PlanCommands()
	if err != nil {
		t.Fatalf("PlanCommands: %v", err)
	}
	if len(commands) != 1 || commands[0].PrimitiveKind != native.SceneBatchQuads {
		t.Fatalf("plan has %d commands, want one quads batch (kind %d)", len(commands), native.SceneBatchQuads)
	}

	// The gradient background also rides the shadow and path records
	// (they hold the pinned Background too).
	shadow := native.NewSceneShadowRecord()
	shadow.Bounds = native.SceneBoundsOf(0, 0, 20, 20)
	shadow.ElementBounds = native.SceneBoundsOf(2, 2, 16, 16)
	shadow.Color = native.SceneGradientBackground(
		45,
		native.HslaF{H: 0, S: 0, L: 0, A: 1}, 0.25,
		native.HslaF{H: 0, S: 0, L: 0, A: 0}, 0.75,
	)
	if err := sc.InsertShadow(&shadow); err != nil {
		t.Fatalf("InsertShadow(gradient color): %v", err)
	}
}

// TestGradientBackgroundValidationRejectsMalformedRecords: the strict
// payload rules — a known tag and color space, distinct stop positions,
// finite scalars, and a zeroed unused arm.
func TestGradientBackgroundValidationRejectsMalformedRecords(t *testing.T) {
	sc := nativeScene(t)
	insert := func(mutate func(*native.SceneQuadRecord)) error {
		rec := gradientQuad()
		mutate(&rec)
		return sc.InsertQuad(&rec)
	}
	cases := []struct {
		name   string
		mutate func(*native.SceneQuadRecord)
	}{
		{"unknown tag", func(r *native.SceneQuadRecord) { r.Background.Tag = 2 }},
		{"unknown color space", func(r *native.SceneQuadRecord) { r.Background.ColorSpace = 2 }},
		{"equal stop positions", func(r *native.SceneQuadRecord) {
			r.Background.Stop1Percentage = r.Background.Stop0Percentage
		}},
		{"non-finite angle", func(r *native.SceneQuadRecord) {
			r.Background.Angle = math.Float32bits(float32(math.Inf(1)))
		}},
		{"non-finite stop position", func(r *native.SceneQuadRecord) {
			r.Background.Stop1Percentage = math.Float32bits(float32(math.NaN()))
		}},
		{"gradient carries solid payload", func(r *native.SceneQuadRecord) {
			r.Background.A = math.Float32bits(1)
		}},
		{"solid carries gradient payload", func(r *native.SceneQuadRecord) {
			r.Background.Tag = 0
			r.Background.Stop0Percentage = math.Float32bits(0.5)
		}},
		{"non-zero reserved", func(r *native.SceneQuadRecord) { r.Background.Reserved = 1 }},
	}
	for _, tc := range cases {
		err := insert(tc.mutate)
		if !errors.Is(err, native.ErrSceneBadValue) {
			t.Errorf("%s: InsertQuad = %v, want ErrSceneBadValue", tc.name, err)
		}
	}
}

// TestColorMatrixFilterRecordRoundTrips: a filter chain of blur plus one
// colour-matrix slot (rev 15.1: one per function, uncomposed) inserts as
// a boundary and a backdrop, finishes, dumps with the matrix intact, and
// the plan compiles the boundary into begin/end commands with the
// backdrop batch present.
func TestColorMatrixFilterRecordRoundTrips(t *testing.T) {
	sc := nativeScene(t)
	matrix := native.SceneFilterRecord{}
	// grayscale(1): the CSS luminance weights (filter-effects-1
	// §funcdef-filter-grayscale).
	for i := 0; i < 160; i++ {
		matrix.ColorMatrix[i] = math.Float32bits(0)
	}
	matrix.ColorMatrix[0] = math.Float32bits(0.2126)
	matrix.ColorMatrix[5] = math.Float32bits(0.7152)
	matrix.ColorMatrix[10] = math.Float32bits(0.0722)
	matrix.ColorMatrix[15] = math.Float32bits(1)
	matrix.ColorMatrix[19] = math.Float32bits(1)

	start := native.NewSceneFilterRecord()
	start.Bounds = native.SceneBoundsOf(0, 0, 40, 40)
	start.ContentMask = native.SceneBoundsOf(0, 0, 1000, 1000)
	start.IsStart = 1
	start.FilterCount = 2
	start.FilterTags = [8]uint32{0, 1}
	start.BlurRadii = [8]uint32{math.Float32bits(6), 0, 0, 0, 0, 0, 0, 0}
	start.ColorMatrix = matrix.ColorMatrix
	start.ColorMatrixCount = 1
	start.Opacity = math.Float32bits(1)
	if err := sc.InsertFilterBoundary(&start); err != nil {
		t.Fatalf("InsertFilterBoundary: %v", err)
	}
	end := start
	end.IsStart = 0
	if err := sc.InsertFilterBoundary(&end); err != nil {
		t.Fatalf("InsertFilterBoundary(end): %v", err)
	}
	backdrop := start
	backdrop.IsStart = 0
	if err := sc.InsertBackdropFilter(&backdrop); err != nil {
		t.Fatalf("InsertBackdropFilter: %v", err)
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	boundaries, err := sc.DumpBoundaries()
	if err != nil {
		t.Fatalf("DumpBoundaries: %v", err)
	}
	if len(boundaries) != 2 {
		t.Fatalf("dumped %d boundaries, want 2", len(boundaries))
	}
	if boundaries[0].ColorMatrix != matrix.ColorMatrix {
		t.Fatalf("colour matrix did not round-trip:\n got %v\nwant %v", boundaries[0].ColorMatrix, matrix.ColorMatrix)
	}
	if boundaries[0].FilterTags != [8]uint32{0, 1, 0, 0, 0, 0, 0, 0} || boundaries[0].ColorMatrixCount != 1 {
		t.Fatalf("filter tags/count did not round-trip: %+v", boundaries[0])
	}
	if got := boundaries[0].ColorMatrixRows(0); got[0] != [5]float32{0.2126, 0, 0, 0, 0} {
		t.Fatalf("ColorMatrixRows row 0 = %v", got[0])
	}
	backdrops, err := sc.DumpBackdrops()
	if err != nil || len(backdrops) != 1 {
		t.Fatalf("DumpBackdrops: %v (%d)", err, len(backdrops))
	}
	if backdrops[0].ColorMatrix != matrix.ColorMatrix {
		t.Fatal("the backdrop's colour matrix did not round-trip")
	}
	commands, err := sc.PlanCommands()
	if err != nil {
		t.Fatalf("PlanCommands: %v", err)
	}
	var begins, ends, batches int
	for _, command := range commands {
		switch command.CommandKind {
		case native.SceneCommandBeginFilter:
			begins++
		case native.SceneCommandEndFilter:
			ends++
		case native.SceneCommandBatch:
			batches++
		}
	}
	if begins != 1 || ends != 1 {
		t.Fatalf("plan compiled %d begins and %d ends, want 1 and 1", begins, ends)
	}
	if batches != 1 || commands[len(commands)-1].PrimitiveKind != native.SceneBatchBackdropFilters {
		t.Fatalf("the backdrop batch did not compile (batches %d)", batches)
	}
}

// TestColorMatrixFilterValidationRejectsMalformedRecords: the tag count
// must match the tag-1 entries, matrix slots carry no blur radius, the
// payload is finite, and an unused payload is zero.
func TestColorMatrixFilterValidationRejectsMalformedRecords(t *testing.T) {
	sc := nativeScene(t)
	validBlurMatrix := func() native.SceneFilterRecord {
		rec := native.NewSceneFilterRecord()
		rec.Bounds = native.SceneBoundsOf(0, 0, 40, 40)
		rec.ContentMask = native.SceneBoundsOf(0, 0, 1000, 1000)
		rec.FilterCount = 2
		rec.FilterTags = [8]uint32{0, 1, 0, 0, 0, 0, 0, 0}
		rec.BlurRadii = [8]uint32{math.Float32bits(6), 0, 0, 0, 0, 0, 0, 0}
		rec.ColorMatrix[15] = math.Float32bits(1)
		rec.ColorMatrix[19] = math.Float32bits(1)
		rec.ColorMatrixCount = 1
		return rec
	}
	cases := []struct {
		name   string
		mutate func(*native.SceneFilterRecord)
	}{
		{"tag count mismatch", func(r *native.SceneFilterRecord) { r.ColorMatrixCount = 2 }},
		{"matrix entry carries a blur radius", func(r *native.SceneFilterRecord) {
			r.BlurRadii[1] = math.Float32bits(4)
		}},
		{"non-finite matrix entry", func(r *native.SceneFilterRecord) {
			r.ColorMatrix[3] = math.Float32bits(float32(math.Inf(-1)))
		}},
		{"zero matrix count carries payload", func(r *native.SceneFilterRecord) {
			r.FilterTags = [8]uint32{0, 0, 0, 0, 0, 0, 0, 0}
			r.ColorMatrixCount = 0
		}},
		{"matrix slot beyond the count nonzero", func(r *native.SceneFilterRecord) {
			// Two tag-1 entries (count 2) carry slots 0 and 1; a nonzero
			// word in slot 2 (words [40..60)) is rejected.
			r.FilterCount = 3
			r.FilterTags = [8]uint32{0, 1, 1, 0, 0, 0, 0, 0}
			r.ColorMatrixCount = 2
			r.ColorMatrix[40] = math.Float32bits(1)
		}},
		{"unknown filter tag", func(r *native.SceneFilterRecord) { r.FilterTags[0] = 2 }},
		{"unused filter tag nonzero", func(r *native.SceneFilterRecord) { r.FilterTags[2] = 1 }},
		{"negative blur radius", func(r *native.SceneFilterRecord) {
			r.BlurRadii[0] = math.Float32bits(-1)
		}},
	}
	for _, tc := range cases {
		rec := validBlurMatrix()
		tc.mutate(&rec)
		err := sc.InsertBackdropFilter(&rec)
		if !errors.Is(err, native.ErrSceneBadValue) {
			t.Errorf("%s: InsertBackdropFilter = %v, want ErrSceneBadValue", tc.name, err)
		}
	}
}

// TestPixelatedSpriteFlagSplitsBatches: sprites carrying the pixelated
// flag draw through the point sampler, so the plan splits a run of
// same-texture sprites into a bilinear batch and a pixelated batch, and
// the flag survives the dump.
func TestPixelatedSpriteFlagSplitsBatches(t *testing.T) {
	sc := nativeScene(t)
	sprite := func(x float32, pixelated uint32) native.SceneSpriteRecord {
		rec := native.NewSceneSpriteRecord()
		rec.Bounds = native.SceneBoundsOf(x, 5, 8, 8)
		rec.ContentMask = native.SceneBoundsOf(0, 0, 1000, 1000)
		rec.Tile = native.SceneTileRecord{TextureKind: native.SceneSpriteTextureMonochrome, BoundsW: 8, BoundsH: 8}
		rec.Pixelated = pixelated
		return rec
	}
	for _, x := range []float32{0, 10, 20} {
		rec := sprite(x, 0)
		if err := sc.InsertMonochromeSprite(&rec); err != nil {
			t.Fatalf("InsertMonochromeSprite: %v", err)
		}
	}
	for _, x := range []float32{30, 40} {
		rec := sprite(x, 1)
		if err := sc.InsertMonochromeSprite(&rec); err != nil {
			t.Fatalf("InsertMonochromeSprite(pixelated): %v", err)
		}
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	dumped, err := sc.DumpMonochromeSprites()
	if err != nil {
		t.Fatalf("DumpMonochromeSprites: %v", err)
	}
	if len(dumped) != 5 {
		t.Fatalf("dumped %d sprites, want 5", len(dumped))
	}
	if dumped[3].Pixelated != 1 || dumped[4].Pixelated != 1 || dumped[0].Pixelated != 0 {
		t.Fatal("the pixelated flag did not round-trip")
	}
	commands, err := sc.PlanCommands()
	if err != nil {
		t.Fatalf("PlanCommands: %v", err)
	}
	if len(commands) != 2 {
		t.Fatalf("the plan split into %d commands, want 2 (bilinear then pixelated)", len(commands))
	}
	if commands[0].PrimitiveKind != native.SceneBatchMonochrome || commands[0].Pixelated != 0 {
		t.Fatalf("command 0 = %+v, want a bilinear monochrome batch", commands[0])
	}
	if commands[0].RangeEnd-commands[0].RangeStart != 3 {
		t.Fatalf("the bilinear batch covers %d sprites, want 3", commands[0].RangeEnd-commands[0].RangeStart)
	}
	if commands[1].PrimitiveKind != native.SceneBatchMonochrome || commands[1].Pixelated != 1 {
		t.Fatalf("command 1 = %+v, want a pixelated monochrome batch", commands[1])
	}
	if commands[1].RangeEnd-commands[1].RangeStart != 2 {
		t.Fatalf("the pixelated batch covers %d sprites, want 2", commands[1].RangeEnd-commands[1].RangeStart)
	}

	// The polychrome sprites carry the same flag (split together with
	// corner smoothing).
	poly := native.NewScenePolychromeSpriteRecord()
	poly.Bounds = native.SceneBoundsOf(50, 5, 8, 8)
	poly.ContentMask = native.SceneBoundsOf(0, 0, 1000, 1000)
	poly.Tile = native.SceneTileRecord{TextureKind: native.SceneSpriteTexturePolychrome, BoundsW: 8, BoundsH: 8}
	poly.Pixelated = 1
	if err := sc.InsertPolychromeSprite(&poly); err != nil {
		t.Fatalf("InsertPolychromeSprite(pixelated): %v", err)
	}

	// Out-of-range flags are rejected.
	bad := sprite(60, 2)
	if err := sc.InsertMonochromeSprite(&bad); !errors.Is(err, native.ErrSceneBadValue) {
		t.Errorf("InsertMonochromeSprite(pixelated 2) = %v, want ErrSceneBadValue", err)
	}
}

// dropShadowView renders one translucent panel with a hard red drop
// shadow: under CSS (css-backgrounds-3 §box-shadow: the outer shadow is
// clipped by the border box) the shadow never paints under the
// translucent background, so the panel reads as the plain background
// blend; the pinned shader painted it under the element too.
type dropShadowView struct{}

func (v *dropShadowView) Render(w *gpui.Window, cx *gpui.Context[dropShadowView]) gpui.AnyElement {
	panel := gpui.Div().W(gpui.Px(160)).H(gpui.Px(80)).
		Bg(gpui.SolidBackground(gpui.Hsla{H: 0, S: 0, L: 1, A: 0.5})).
		Shadow([]gpui.BoxShadow{{
			Color:      gpui.SolidBackground(gpui.Hsla{H: 0, S: 1, L: 0.5, A: 1}),
			OffsetY:    12,
			BlurRadius: 0,
		}}).
		DebugSelector("drop-panel")
	return gpui.Div().SizeFull().Flex().Child(panel).IntoElement()
}

// TestDropShadowClippedOutOfBorderBoxOnRealGPU: the row at the panel's
// center is the plain white-over-black blend (no red under the
// translucent background), while the row below the panel carries the
// red shadow. Before rev 15 the center row read as white blended over
// the shadow's red.
func TestDropShadowClippedOutOfBorderBoxOnRealGPU(t *testing.T) {
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		<-runDone
	})

	var window *gpui.Window
	sync := func(f func()) {
		done := make(chan struct{})
		if !host.Post(func() {
			defer close(done)
			f()
		}) {
			t.Fatalf("foreground post was dropped: the host loop exited")
		}
		<-done
	}
	sync(func() {
		var openErr error
		window, openErr = app.OpenWindowView(gpui.WindowOptions{
			Title:     "scenespec drop shadow clip-out",
			Show:      true, // shown WITHOUT activating: no focus is taken
			Resizable: true,
			// Parked far off-screen so no window appears on the user's
			// desktop (the headless/offscreen rule; ticket 03's
			// real-window tests use the same origin).
			Bounds: gpui.Bounds{Origin: gpui.Point{X: 20000, Y: 20000}, Size: gpui.Size{Width: 240, Height: 160}},
		}, func(w *gpui.Window, app *gpui.App) gpui.View {
			return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *dropShadowView, cx *gpui.Context[dropShadowView]) {}))
		})
		if openErr != nil {
			t.Fatalf("OpenWindowView: %v — this session is expected to create real windows", openErr)
		}
	})
	t.Cleanup(func() {
		sync(func() {
			if window != nil && window.Alive() {
				window.Close()
			}
		})
		waitForScene(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
	})
	time.Sleep(150 * time.Millisecond)

	renderer, err := gpui.NewRenderer()
	if err != nil {
		t.Fatalf("NewRenderer: %v", err)
	}
	t.Cleanup(func() { _ = renderer.Close() })
	var surface *gpui.Surface
	sync(func() { surface, err = renderer.GetOrCreateSurface(window) })
	if err != nil || surface == nil {
		t.Fatalf("GetOrCreateSurface: %v (%v)", surface, err)
	}
	black, err := renderer.PresentClear(window, gpui.Color{R: 0, G: 0, B: 0, A: 1})
	if err != nil {
		t.Fatalf("black present: %v", err)
	}
	if err := black.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("black present retire: %v", err)
	}
	var scene *gpui.Scene
	sync(func() {
		var drawErr error
		scene, drawErr = gpui.DrawWindowFrame(window)
		if drawErr != nil {
			t.Fatalf("DrawWindowFrame: %v", drawErr)
		}
	})
	shadows, err := scene.Shadows()
	if err != nil {
		t.Fatalf("Shadows: %v", err)
	}
	if len(shadows) != 1 || shadows[0].Inset {
		t.Fatalf("the panel cast %d shadows (inset %v), want one drop shadow", len(shadows), len(shadows) > 0 && shadows[0].Inset)
	}
	panel, ok := gpui.WindowDebugBound(window, "drop-panel")
	if !ok {
		t.Fatal("no panel debug bounds")
	}
	// A transparent clear (the opaque appearance clears white, which
	// washes the blend out): over an empty backdrop the panel reads as
	// the plain white-0.5 blend, and any shadow bleeding under it lifts
	// the red channel.
	if err := surface.RenderScene(scene, nil, gpui.BackgroundTransparent); err != nil {
		t.Fatalf("RenderScene: %v", err)
	}
	pixels, err := surface.ReadPixels()
	if err != nil {
		t.Fatalf("ReadPixels: %v", err)
	}
	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface.Info: %v", err)
	}
	width, scale := int(info.Width), math.Float32frombits(info.ScaleBits)

	// channelRow reads one row's average channel across a band of the
	// panel width (clear of the corners).
	channelRow := func(row float32, index int) int {
		sum, count := 0, 0
		x0 := int((panel.Origin.X + 20) * scale)
		x1 := int((panel.Origin.X + panel.Size.Width - 20) * scale)
		y := int(row * scale)
		if y < 0 {
			return -1
		}
		for x := x0; x < x1; x++ {
			i := (y*width+x)*4 + index
			if i >= 0 && i < len(pixels) {
				sum += int(pixels[i])
				count++
			}
		}
		if count == 0 {
			return -1
		}
		return sum / count
	}

	// Inside the panel: white 0.5 over black = 128 on every channel. A
	// shadow bleeding under the element adds its red to the blend (the
	// pinned behavior read ~191 on red).
	centerRed := channelRow(panel.Origin.Y+panel.Size.Height/2, 0)
	centerGreen := channelRow(panel.Origin.Y+panel.Size.Height/2, 1)
	t.Logf("center row: r=%d g=%d; shadow row: r=%d g=%d", centerRed, centerGreen,
		channelRow(panel.Origin.Y+panel.Size.Height+6, 0), channelRow(panel.Origin.Y+panel.Size.Height+6, 1))
	if centerRed > 128+10 {
		t.Fatalf("the drop shadow paints under the translucent panel: center red = %d, want ~128 (CSS clips outer shadows out of the border box)", centerRed)
	}
	if centerGreen < 128-10 || centerGreen > 128+10 {
		t.Fatalf("center green = %d, want ~128 (the plain white 0.5 blend)", centerGreen)
	}
	// Below the panel (past the offset, inside the shadow region): red.
	shadowRed := channelRow(panel.Origin.Y+panel.Size.Height+6, 0)
	shadowGreen := channelRow(panel.Origin.Y+panel.Size.Height+6, 1)
	if shadowRed < 200 || shadowGreen > 55 {
		t.Fatalf("the shadow region below the panel reads r=%d g=%d, want the red shadow (~255, ~0)", shadowRed, shadowGreen)
	}
}
