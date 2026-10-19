//go:build windows

// Package fontspec holds the consumer-gaps embedded-font gates: the
// public AddFont registration path (the pinned gpui::TextSystem::add_fonts
// over the shared Fontique catalog), resolution of registered families by
// name through ResolveFont with the FamilyFound fallback diagnostic, the
// pinned service-fallback behavior for families that resolve nowhere, and
// a real-window staging readback proving the registered font's glyph
// pixels differ from the system default's through the full shape →
// rasterize → atlas → scene → render path.
//
// The fixtures are the consumer's exact fonts: Geist and Geist Mono
// (SIL Open Font License 1.1, the license committed beside them), loaded
// through embed.FS exactly the way a consumer app embeds its typefaces
// (mordbard embeds the same faces; testdata/Geist-OFL.txt is the license).
//
// ORDER NOTE, recorded honestly: registration is process-global and has no
// reset (the pinned add_fonts has none), so the AddFont gates in this file
// observe the pre-registration catalog exactly once per test process and
// the package never runs recorded-trace fixtures (whose FontIds pin
// resolution order).
package fontspec

import (
	"embed"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// The consumer's embedded font assets: Geist and Geist Mono, SIL OFL 1.1
// (testdata/Geist-OFL.txt). embed.FS is the exact consumption path the
// ticket names ("font bytes loaded from an embedded asset").
//
//go:embed testdata/Geist-Regular.ttf
//go:embed testdata/GeistMono-Regular.ttf
var embeddedFonts embed.FS

// mustEmbeddedFont reads one embedded face.
func mustEmbeddedFont(t *testing.T, name string) []byte {
	t.Helper()
	data, err := embeddedFonts.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading the embedded font %s: %v", name, err)
	}
	if len(data) == 0 {
		t.Fatalf("the embedded font %s is empty", name)
	}
	return data
}

// mustAddFont registers one embedded face and returns the registration.
func mustAddFont(t *testing.T, ts *gpui.TextSystem, name string) gpui.FontRegistration {
	t.Helper()
	registration, err := ts.AddFont(mustEmbeddedFont(t, name))
	if err != nil {
		t.Fatalf("AddFont(%s): %v", name, err)
	}
	if registration.Generation == 0 {
		t.Fatalf("AddFont(%s) reported generation 0, want >= 1 (a registration batch)", name)
	}
	return registration
}

// familyListed reports whether family appears in the catalog enumeration.
func familyListed(t *testing.T, ts *gpui.TextSystem, family string) bool {
	t.Helper()
	names, err := ts.FontNames()
	if err != nil {
		t.Fatalf("FontNames: %v", err)
	}
	for _, name := range names {
		if name == family {
			return true
		}
	}
	return false
}

// waitFor polls cond until it holds or the timeout expires.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

// TestEmbeddedFontsRegisterResolveAndReport is the first acceptance gate:
// bytes from an embedded asset (embed.FS) register through the public
// AddFont, their families (from each font's embedded name table) appear in
// FontNames and resolve by name through ResolveFont with FamilyFound true,
// and the resolved faces are their own fonts (distinct from each other and
// from the system default's face), with the consumer's exact ramp of
// sizes shaping real glyph runs in every registered face.
func TestEmbeddedFontsRegisterResolveAndReport(t *testing.T) {
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}

	// Pre-registration state, recorded honestly: Geist is not a system
	// family on this machine, so resolving it before registration falls
	// back (the pinned chain ends in Arial here). A machine with Geist
	// installed reports the family already present; the registration
	// assertions below hold either way.
	before, err := ts.ResolveFont(gpui.FontDescriptor{Family: "Geist", Weight: 400})
	if err != nil {
		t.Fatalf("pre-registration resolve: %v", err)
	}
	geistInstalled := before.FamilyFound

	// THE GATE: embed.FS bytes → AddFont → resolve by family name.
	geistReg := mustAddFont(t, ts, "Geist-Regular.ttf")
	monoReg := mustAddFont(t, ts, "GeistMono-Regular.ttf")

	if !familyListed(t, ts, "Geist") {
		t.Fatalf("Geist missing from the catalog after AddFont (FontNames must list the registered family)")
	}
	if !familyListed(t, ts, "Geist Mono") {
		t.Fatalf("Geist Mono missing from the catalog after AddFont")
	}
	if monoReg.Generation <= geistReg.Generation {
		t.Errorf("the Geist Mono registration generation %d does not follow Geist's %d (each AddFont is a registration batch)",
			monoReg.Generation, geistReg.Generation)
	}

	// Resolution by name: the registered families resolve with the
	// FamilyFound diagnostic true and distinct canonical ids.
	geist, err := ts.ResolveFont(gpui.FontDescriptor{Family: "Geist", Weight: 400})
	if err != nil {
		t.Fatalf("resolve Geist: %v", err)
	}
	if !geist.FontID.IsCanonical() {
		t.Errorf("Geist FontID %#x lacks the canonical bit", geist.FontID)
	}
	if !geist.FamilyFound {
		t.Errorf("resolve Geist reports FamilyFound = false after AddFont, want true")
	}
	mono, err := ts.ResolveFont(gpui.FontDescriptor{Family: "Geist Mono", Weight: 400})
	if err != nil {
		t.Fatalf("resolve Geist Mono: %v", err)
	}
	if !mono.FamilyFound {
		t.Errorf("resolve Geist Mono reports FamilyFound = false after AddFont, want true")
	}
	if mono.FontID == geist.FontID {
		t.Errorf("Geist and Geist Mono resolved to the same FontID %d", geist.FontID)
	}
	if !geistInstalled && geist.FontID == before.FontID {
		t.Errorf("Geist resolved to the fallback id %d it had before registration (the registered face never took over)", geist.FontID)
	}

	// The default system font stays itself: a distinct face from both
	// registered families.
	system, err := ts.ResolveFont(gpui.DefaultFontDescriptor())
	if err != nil {
		t.Fatalf("resolve the default font: %v", err)
	}
	if !system.FamilyFound {
		t.Errorf("the default font reports FamilyFound = false, want true (the system UI alias is in the catalog)")
	}
	if system.FontID == geist.FontID || system.FontID == mono.FontID {
		t.Errorf("the system default resolved to a registered face (%d)", system.FontID)
	}

	// The metrics of the registered face are its own (not the system
	// default's): Geist's units-per-em/ascent/x-height come from the
	// embedded name-table face.
	geistMetrics, err := ts.FontMetrics(geist.FontID)
	if err != nil {
		t.Fatalf("FontMetrics(Geist): %v", err)
	}
	systemMetrics, err := ts.FontMetrics(system.FontID)
	if err != nil {
		t.Fatalf("FontMetrics(default): %v", err)
	}
	if geistMetrics.UnitsPerEm == 0 {
		t.Errorf("Geist units_per_em = 0")
	}
	if geistMetrics.UnitsPerEm == systemMetrics.UnitsPerEm &&
		geistMetrics.Ascent == systemMetrics.Ascent &&
		geistMetrics.XHeight == systemMetrics.XHeight {
		t.Errorf("Geist metrics equal the system default's exactly (%+v); the registered face did not take over", geistMetrics)
	}

	// The consumer's exact ramp (mordbard's 1:1 type spec): every size in
	// both faces shapes real glyph runs.
	shapesAt := func(t *testing.T, family string, size float32) gpui.TextLayoutSummary {
		t.Helper()
		const text = "Practice makes perfect 0123456789"
		run := gpui.TextRun{Len: len(text), Font: gpui.FontDescriptor{Family: family, Weight: 400}}
		line, err := ts.ShapeText(gpui.TextLayoutRequest{
			Text:     text,
			FontSize: size,
			Runs:     []gpui.TextRun{run},
		})
		if err != nil {
			t.Fatalf("shape %s at %vpx: %v", family, size, err)
		}
		t.Cleanup(func() { _ = line.Dispose() })
		summary, err := line.Summary()
		if err != nil {
			t.Fatalf("summary: %v", err)
		}
		if summary.GlyphCount == 0 {
			t.Fatalf("shaping %s at %vpx produced no glyphs", family, size)
		}
		if summary.Width <= 0 {
			t.Fatalf("shaping %s at %vpx produced width %v", family, size, summary.Width)
		}
		return summary
	}
	for _, size := range []float32{11, 12, 13, 17, 22, 28} {
		shapesAt(t, "Geist", size)
	}
	shapesAt(t, "Geist Mono", 12)

	// Re-registration is idempotent for resolution (the pinned path has
	// no duplicate-family rule): same id, same diagnostic.
	again, err := ts.AddFont(mustEmbeddedFont(t, "Geist-Regular.ttf"))
	if err != nil {
		t.Fatalf("re-AddFont(Geist): %v", err)
	}
	if again.Generation <= geistReg.Generation {
		t.Errorf("re-registration generation %d not > %d", again.Generation, geistReg.Generation)
	}
	regeist, err := ts.ResolveFont(gpui.FontDescriptor{Family: "Geist", Weight: 400})
	if err != nil || regeist.FontID != geist.FontID || !regeist.FamilyFound {
		t.Errorf("re-resolve Geist: id %d (want %d), FamilyFound=%v, err=%v",
			regeist.FontID, geist.FontID, regeist.FamilyFound, err)
	}
}

// TestUnresolvedFamilyReportsFallback is the second acceptance gate: a
// family that resolves nowhere does not error (the pinned resolve chain
// silently falls back) but REPORTS the fallback through
// FontIdentity.FamilyFound, and bad AddFont input is a typed error — so a
// missing embedded font is diagnosable in both directions.
func TestUnresolvedFamilyReportsFallback(t *testing.T) {
	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}

	// The nonsense family from the consumer's evidence: same behavior as
	// before (no error, the fallback font's id), but now observable.
	fallback, err := ts.ResolveFont(gpui.FontDescriptor{Family: "Definitely Not A Font 12345", Weight: 400})
	if err != nil {
		t.Fatalf("resolving a nonsense family errored (%v); the pinned chain falls back silently", err)
	}
	if !fallback.FontID.IsCanonical() {
		t.Errorf("fallback FontID %#x lacks the canonical bit", fallback.FontID)
	}
	if fallback.FamilyFound {
		t.Fatalf("the nonsense family reports FamilyFound = true; the fallback must be observable")
	}

	// Every nonsense family agrees (the diagnostic is family-presence,
	// not a cached accident).
	for _, family := range []string{"Also Not A Font xyz", "Nope 999", "Geist Variable"} {
		identity, err := ts.ResolveFont(gpui.FontDescriptor{Family: family})
		if err != nil {
			t.Fatalf("resolve %q: %v", family, err)
		}
		if identity.FamilyFound {
			t.Errorf("resolve %q reports FamilyFound = true, want the fallback report", family)
		}
	}

	// A registered-but-misspelled family reports the fallback too — the
	// exact missing-embedded-font case (the app embedded "Geist" but asked
	// for "Geist UI").
	if _, err := ts.AddFont(mustEmbeddedFont(t, "Geist-Regular.ttf")); err != nil {
		t.Fatalf("AddFont(Geist): %v", err)
	}
	misspelled, err := ts.ResolveFont(gpui.FontDescriptor{Family: "Geist UI"})
	if err != nil {
		t.Fatalf("resolve the misspelled family: %v", err)
	}
	if misspelled.FamilyFound {
		t.Errorf("the misspelled family reports FamilyFound = true; the app's embedded font is missing under that name")
	}

	// Bad registration input: typed errors, never silent success.
	if _, err := ts.AddFont(nil); !errors.Is(err, gpui.ErrTextValue) {
		t.Errorf("AddFont(nil) error = %v, want ErrTextValue", err)
	}
	if _, err := ts.AddFont([]byte("definitely not a font")); !errors.Is(err, gpui.ErrTextFontData) {
		t.Errorf("AddFont(no-face bytes) error = %v, want ErrTextFontData", err)
	}
	if _, err := ts.AddFont([]byte("MZ not a font either")); !errors.Is(err, gpui.ErrTextFontData) {
		t.Errorf("AddFont(PE-header bytes) error = %v, want ErrTextFontData", err)
	}
	// A failed registration published nothing: the families it would have
	// registered still resolve nowhere.
	if familyListed(t, ts, "Definitely Not A Font 12345") {
		t.Errorf("a rejected registration published a family into the catalog")
	}
}

// TestRegisteredFontRendersDistinctPixels is the readback half of the
// first gate, on a real window and GPU: the same text shaped in Geist and
// in the system default, rasterized, atlas-packed and rendered through
// the public scene path, read back from the surface — the glyph pixels
// must differ (the registered font drove the rendering, not the fallback).
func TestRegisteredFontRendersDistinctPixels(t *testing.T) {
	app := gpui.NewApp()
	h := gpui.NewHost()
	if err := app.Attach(h); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := h.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(h) }()
	t.Cleanup(func() {
		if err := h.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		<-runDone
	})

	window, err := app.OpenWindow(gpui.WindowOptions{
		Title:     "fontspec embedded fonts",
		Show:      true,
		Resizable: true,
		Bounds:    gpui.Bounds{Size: gpui.Size{Width: 480, Height: 200}},
	})
	if err != nil {
		t.Fatalf("app.OpenWindow: %v — this session is expected to create real windows", err)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitFor(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})

	ts, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	mustAddFont(t, ts, "Geist-Regular.ttf")

	// Deterministic white background first, so every ink pixel is text.
	renderer, err := gpui.NewRenderer()
	if err != nil {
		t.Fatalf("gpui.NewRenderer: %v", err)
	}
	t.Cleanup(func() {
		if err := renderer.Close(); err != nil {
			t.Errorf("renderer close: %v", err)
		}
	})
	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}
	white, err := renderer.PresentClear(window, gpui.Color{R: 1, G: 1, B: 1, A: 1})
	if err != nil {
		t.Fatalf("the white present: %v", err)
	}
	if err := white.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("the white present did not retire: %v", err)
	}

	info, err := surface.Info()
	if err != nil {
		t.Fatalf("surface.Info: %v", err)
	}
	width, height := int(info.Width), int(info.Height)
	scale := float32FromBits(info.ScaleBits)
	if width == 0 || height == 0 || scale == 0 {
		t.Fatalf("surface extent = %dx%d scale %v, want non-zero", width, height, scale)
	}
	logicalWidth := float32(width) / scale
	logicalHeight := float32(height) / scale
	windowLogical := gpui.Bounds{Size: gpui.Size{Width: logicalWidth, Height: logicalHeight}}
	ctx := gpui.NewPaintContext(scale, windowLogical, 1)
	whiteBackground := gpui.RgbaToHsla(0xFFFFFFFF)
	ink := gpui.Hsla{H: 0, S: 0, L: 0.1, A: 1}

	// One shared atlas holds both fonts' tiles (the public NewAtlas path
	// the consumer's hand-built scenes use).
	atlas, err := gpui.NewAtlas()
	if err != nil {
		t.Fatalf("new atlas: %v", err)
	}
	t.Cleanup(func() { _ = atlas.Dispose() })

	// Shape the SAME text at the SAME size/position in the registered
	// face and the system default.
	const sample = "The quick brown fox jumps"
	const size = float32(28)
	shapeLine := func(family string) *gpui.WrappedLine {
		t.Helper()
		run := gpui.TextRun{Len: len(sample), Font: gpui.FontDescriptor{Family: family, Weight: 400}}
		line, err := ts.ShapeText(gpui.TextLayoutRequest{
			Text:     sample,
			FontSize: size,
			Runs:     []gpui.TextRun{run},
		})
		if err != nil {
			t.Fatalf("shape %q: %v", family, err)
		}
		t.Cleanup(func() { _ = line.Dispose() })
		return line
	}
	geistLine, systemLine := shapeLine("Geist"), shapeLine(gpui.DefaultFontFamily)

	// The two shaped lines chose different faces (fragments carry the
	// resolved canonical ids).
	geistFragments, err := geistLine.Fragments()
	if err != nil || len(geistFragments) == 0 {
		t.Fatalf("Geist fragments: %v (%d)", err, len(geistFragments))
	}
	systemFragments, err := systemLine.Fragments()
	if err != nil || len(systemFragments) == 0 {
		t.Fatalf("default-font fragments: %v (%d)", err, len(systemFragments))
	}
	if geistFragments[0].FontID == systemFragments[0].FontID {
		t.Fatalf("Geist and the system default shaped to the same font id %d; the registered face never took over", geistFragments[0].FontID)
	}

	buildTextScene := func(line *gpui.WrappedLine) *gpui.Scene {
		t.Helper()
		scene, err := gpui.NewScene()
		if err != nil {
			t.Fatalf("NewScene: %v", err)
		}
		t.Cleanup(func() { _ = scene.Dispose() })
		if err := scene.PaintQuad(gpui.PaintQuad{
			Bounds:     windowLogical,
			Background: gpui.SolidBackground(whiteBackground),
		}, 0, ctx); err != nil {
			t.Fatalf("PaintQuad (background): %v", err)
		}
		if err := scene.PaintShapedText(
			line,
			gpui.Point{X: 16, Y: 16},
			gpui.DefaultLineHeight(size),
			ink,
			ts, atlas, ctx,
			gpui.PaintTextOptions{},
		); err != nil {
			t.Fatalf("PaintShapedText: %v", err)
		}
		if err := scene.Finish(); err != nil {
			t.Fatalf("scene finish: %v", err)
		}
		return scene
	}

	// Render without presenting, read the staging pixels back (the
	// documented RenderScene + ReadPixels pair).
	render := func(scene *gpui.Scene) []byte {
		t.Helper()
		if err := surface.RenderScene(scene, atlas, gpui.BackgroundOpaque); err != nil {
			t.Fatalf("RenderScene: %v", err)
		}
		pixels, err := surface.ReadPixels()
		if err != nil {
			t.Fatalf("ReadPixels: %v", err)
		}
		if len(pixels) != width*height*4 {
			t.Fatalf("readback = %d bytes, want %d", len(pixels), width*height*4)
		}
		return pixels
	}
	geistPixels := render(buildTextScene(geistLine))
	systemPixels := render(buildTextScene(systemLine))

	// Both renders carry real ink; the pixels differ (different glyph
	// coverage from different faces).
	inkCount := func(pixels []byte) int {
		count := 0
		for i := 0; i+3 < len(pixels); i += 4 {
			if pixels[i] != 0xFF || pixels[i+1] != 0xFF || pixels[i+2] != 0xFF {
				count++
			}
		}
		return count
	}
	geistInk, systemInk := inkCount(geistPixels), inkCount(systemPixels)
	if geistInk < 20 {
		t.Fatalf("the Geist render carries only %d ink pixels, want >= 20 (text drew)", geistInk)
	}
	if systemInk < 20 {
		t.Fatalf("the default-font render carries only %d ink pixels, want >= 20 (text drew)", systemInk)
	}
	differing := 0
	for i := range geistPixels {
		if geistPixels[i] != systemPixels[i] {
			differing++
		}
	}
	if differing < 40 {
		t.Fatalf("the Geist and default-font readbacks differ in only %d byte values, want >= 40 (the registered font's glyph pixels must differ from the system default's)",
			differing)
	}
	t.Logf("embedded-font readback: %d Geist ink pixels vs %d default ink pixels, %d differing byte values",
		geistInk, systemInk, differing)

	// The registered font's frame presents through the ledger like any
	// scene (the full shape → raster → atlas → scene → draw path on the
	// real GPU).
	submission, err := surface.DrawScene(buildTextScene(geistLine), atlas, gpui.BackgroundOpaque)
	if err != nil {
		t.Fatalf("DrawScene: %v", err)
	}
	if err := submission.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("the Geist text frame did not retire: %v", err)
	}
}

// float32FromBits decodes an f32 from its IEEE-754 bits (the surface info
// reports the scale factor as bits).
func float32FromBits(bits uint32) float32 {
	return math.Float32frombits(bits)
}
