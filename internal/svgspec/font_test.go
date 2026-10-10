// Package svgspec holds ticket19's SVG gates at the gpui model level:
// the pinned SvgRenderer semantics through the native resvg/usvg
// service (parse/render at the three sizing modes with the
// smooth-scale math, the alpha-mask path, the typed errors, resource
// retirement), the Go-owned pinned-font orchestration (the
// NeedsFontAssets seam: missing → shared async load → AddFont →
// resume exactly once, cancellation discards, missing paths resolve to
// explicit errors), and the Svg element's paint through the
// alpha-mask monochrome sprite path at real bounds.
//
// The fixtures are inline SVGs plus copies of the committed CE corpus
// fonts (the same bytes as internal/native/testdata — the pinned
// checkout's own bundled fonts).
//
// ORDER DEPENDENCE, recorded honestly: the native SVG service's font
// registry is process-global and the frozen artifact exposes no reset
// (svg_add_font's duplicate rule is the only boundary), so the
// pending-font gates in this file observe the pre-push state exactly
// once per test process. This file's orchestration test therefore runs
// FIRST (Go runs tests in file order: font_test.go before the
// svg_element/svg_model files) and asserts its precondition loudly.
package svgspec

import (
	"errors"
	"os"
	"testing"

	"gpui-go/gpui"
)

// The pinned bundled-font asset paths (svg_renderer.rs
// load_bundled_fonts' list, reported by the native service).
const (
	ibmPath   = "fonts/ibm-plex-sans/IBMPlexSans-Regular.ttf"
	lilexPath = "fonts/lilex/Lilex-Regular.ttf"
)

// mustFont reads one committed CE corpus font copy from this package's
// testdata (the bytes internal/native/testdata committed).
func mustFont(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading the font fixture %s: %v (the CE corpus copies must be committed)", name, err)
	}
	return data
}

// bothFonts returns the pinned font fixtures keyed by their pinned
// asset paths.
func bothFonts(t *testing.T) map[string][]byte {
	t.Helper()
	return map[string][]byte{
		ibmPath:   mustFont(t, "IBMPlexSans-Regular.ttf"),
		lilexPath: mustFont(t, "Lilex-Regular.ttf"),
	}
}

// The inline SVG fixtures: the native suite's proven text document and
// a 24x12 variant for the element geometry gates.
const (
	// textSvg is the text-plex fixture: IBM Plex Sans "Ag" in a
	// 120x40 box (the native suite's own oracle shape).
	textSvg = `<svg xmlns="http://www.w3.org/2000/svg" width="120" height="40"><text x="4" y="28" font-family="IBM Plex Sans" font-size="24">Ag</text></svg>`
	// smallTextSvg is the same family at 24x12 — the element gate's
	// document (a 2:1 aspect for the centering math).
	smallTextSvg = `<svg xmlns="http://www.w3.org/2000/svg" width="24" height="12"><text x="2" y="9" font-family="IBM Plex Sans" font-size="8">Ag</text></svg>`
	// emojiSvg carries an emoji-presentation character (the native
	// fallback selection's Segoe UI Emoji path).
	emojiSvg = `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64"><text x="4" y="48" font-family="Segoe UI Emoji" font-size="48">😀</text></svg>`
)

// svgFixture is a deterministic app with an asset registry carrying
// the given pinned fonts (each path's registry reads counted) and the
// app's SVG renderer. Windows are opened per test through
// newSvgWindow.
type svgFixture struct {
	ta        *gpui.TestApp
	app       *gpui.App
	renderer  *gpui.SvgRenderer
	fontLoads map[string]*int
}

// newSvgFixture builds the app. A nil fonts map is an empty registry
// (the missing-asset path).
func newSvgFixture(t *testing.T, fonts map[string][]byte) *svgFixture {
	t.Helper()
	f := &svgFixture{fontLoads: map[string]*int{}}
	registry := gpui.NewAssetRegistry()
	for path, data := range fonts {
		path, data := path, data
		counter := new(int)
		f.fontLoads[path] = counter
		if err := registry.Insert(path, gpui.OnDemand(func() (gpui.AssetData, bool) {
			*counter++
			return data, true
		})); err != nil {
			t.Fatalf("registry insert %s: %v", path, err)
		}
	}
	f.ta = gpui.NewTestApp().WithAssets(registry)
	f.app = f.ta.App()
	renderer, err := f.app.SvgRenderer()
	if err != nil {
		t.Fatalf("the application svg renderer: %v (the native svg service must be available; these tests fail, never skip)", err)
	}
	f.renderer = renderer
	// The app's image atlas slot returns when the test ends: the
	// native service bounds live atlas handles (8), so every app that
	// inserts tiles must return its slot.
	t.Cleanup(func() {
		if atlas := f.app.ImageAtlas(); atlas != nil {
			_ = atlas.Dispose()
		}
	})
	return f
}

// fontLoadTotal sums the registry reads across the pinned paths (the
// shared-load evidence).
func (f *svgFixture) fontLoadTotal() int {
	total := 0
	for _, counter := range f.fontLoads {
		total += *counter
	}
	return total
}

// svgRootView is the view state rendering a root element tree built
// fresh on every frame (the reference view render: the element tree
// is reconstructed per frame, so builders convert inside Render).
type svgRootView struct {
	build func() gpui.AnyElement
}

// Render implements gpui.Render: a fresh root element tree.
func (v *svgRootView) Render(w *gpui.Window, cx *gpui.Context[svgRootView]) gpui.AnyElement {
	return v.build()
}

// newSvgWindow opens a test window on the fixture's app whose root
// view renders the tree built by build (fresh per frame), observing
// the view's notifications into the returned counter.
func (f *svgFixture) newSvgWindow(t *testing.T, build func() gpui.AnyElement) (*gpui.Window, *int) {
	t.Helper()
	notifications := new(int)
	view := gpui.NewEntity(f.app, f.ta.RootScope(), func(v *svgRootView, cx *gpui.Context[svgRootView]) {
		v.build = build
		cx.Observe(cx.Entity(), func(_ *svgRootView, _ gpui.Entity[svgRootView], _ *gpui.Context[svgRootView]) {
			*notifications++
		})
	})
	window := gpui.NewTestWindow(f.app, gpui.Size{Width: 200, Height: 100})
	window.SetRootView(gpui.ViewOf(view))
	return window, notifications
}

// parseResolved parses through the public surface, handling both
// orderings: with the pinned fonts pushed the parse is immediate (the
// pinned synchronous parse_svg); while a load is pending the drain
// completes it and the suspended parse delivers exactly once.
func (f *svgFixture) parseResolved(t *testing.T, bytes []byte) *gpui.ParsedSvg {
	t.Helper()
	var delivered *gpui.ParsedSvg
	outcome, done := f.renderer.ParseSvg(f.app, f.ta.RootScope(), bytes, func(o gpui.SvgParseOutcome, cx *gpui.App) {
		if o.Err != nil {
			t.Errorf("the suspended parse delivered %v", o.Err)
			return
		}
		delivered = o.Svg
	})
	if done {
		if outcome.Svg == nil || outcome.Err != nil {
			t.Fatalf("parse = (%+v, %v), want an immediate parsed document", outcome, done)
		}
		return outcome.Svg
	}
	f.ta.RunUntilParked()
	if delivered == nil {
		t.Fatal("the suspended parse did not resolve after the drain")
	}
	return delivered
}

// draw runs one window frame and returns the finished scene.
func (f *svgFixture) draw(t *testing.T, window *gpui.Window) *gpui.Scene {
	t.Helper()
	scene, err := gpui.DrawWindowFrame(window)
	if err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return scene
}

// monochromeSprites reads a finished scene's monochrome sprites.
func monochromeSprites(t *testing.T, scene *gpui.Scene) []gpui.MonochromeSprite {
	t.Helper()
	sprites, err := scene.MonochromeSprites()
	if err != nil {
		t.Fatalf("MonochromeSprites: %v", err)
	}
	return sprites
}

// TestSvgFontOrchestration walks the whole pending-font story in one
// strict sequence — see the package docs for the order dependence. The
// phases:
//
//  1. the precondition: both pinned paths report missing;
//  2. a missing asset path resolves to the explicit typed error, never
//     a hang (an empty-registry app);
//  3. the element paint with pending fonts: two windows share the one
//     pending load per path, the first frames paint nothing, and
//     closing a window mid-load discards its notification;
//  4. the parse orchestration: two suspended consumers share the load,
//     resume exactly once when the bytes arrive (AddFont happened),
//     the cancelled scope's parse discards, and a post-push parse is
//     immediate;
//     4.5. application shutdown drains and discards a suspended parse
//     (the ticket04 cancel-before-start retirement);
//  5. the element windows' completions: the closed window's
//     notification discards, the open window is notified and redraws
//     the tinted mask through the real pipeline.
func TestSvgFontOrchestration(t *testing.T) {
	// ---- Phase 1: the precondition (loud failure when polluted).
	probe := newSvgFixture(t, nil)
	paths, err := probe.renderer.FontAssetPaths()
	if err != nil {
		t.Fatalf("FontAssetPaths: %v", err)
	}
	if len(paths) != 2 || paths[0] != ibmPath || paths[1] != lilexPath {
		t.Fatalf("pinned font asset paths = %v, want [%s %s]", paths, ibmPath, lilexPath)
	}
	for _, path := range paths {
		has, err := probe.renderer.HasFontAsset(path)
		if err != nil {
			t.Fatalf("HasFontAsset(%s): %v", path, err)
		}
		if has {
			t.Fatalf("the pinned font %q is already pushed at the native service; the pending gates need the pre-push state and this test must run first in the process", path)
		}
	}

	// ---- Phase 2: a missing asset path resolves to an explicit error,
	// never a hang. This app's registry carries nothing, so both pinned
	// loads fail and the suspended parse delivers the typed error.
	missing := newSvgFixture(t, nil)
	deliveries := 0
	var outcomeErr error
	if _, done := missing.renderer.ParseSvg(missing.app, missing.ta.RootScope(), []byte(textSvg), func(o gpui.SvgParseOutcome, cx *gpui.App) {
		deliveries++
		outcomeErr = o.Err
	}); done {
		t.Fatal("the parse over an empty registry finished immediately; the missing font assets must suspend it")
	}
	// The suspended parse must resolve during the drain (no parked
	// worker waiting for bytes that never arrive).
	missing.ta.RunUntilParked()
	if deliveries != 1 {
		t.Fatalf("missing-path deliveries = %d, want exactly 1 (the explicit error, never a hang)", deliveries)
	}
	var missingErr *gpui.SvgFontAssetMissingError
	if !errors.As(outcomeErr, &missingErr) {
		t.Fatalf("the missing-path outcome error = %v, want SvgFontAssetMissingError", outcomeErr)
	}
	if missingErr.Path != ibmPath && missingErr.Path != lilexPath {
		t.Fatalf("the missing-path error names %q, want a pinned font path", missingErr.Path)
	}

	// ---- Phase 3: the element paint with pending font assets. Both
	// windows' first frames paint nothing (the fonts gate the mask's
	// internal parse) and share one pending load per path.
	elemF := newSvgFixture(t, bothFonts(t))
	newSvg := func() *gpui.SvgElement {
		return gpui.Svg().Data([]byte(smallTextSvg)).ID("svg").Size(gpui.PxLength(24), gpui.PxLength(12))
	}
	w1, w1Notifications := elemF.newSvgWindow(t, func() gpui.AnyElement { return newSvg().IntoElement() })
	w2, _ := elemF.newSvgWindow(t, func() gpui.AnyElement { return newSvg().IntoElement() })

	if sprites := monochromeSprites(t, elemF.draw(t, w1)); len(sprites) != 0 {
		t.Fatalf("the pending-font frame painted %d monochrome sprites, want 0", len(sprites))
	}
	if sprites := monochromeSprites(t, elemF.draw(t, w2)); len(sprites) != 0 {
		t.Fatalf("the second window's pending-font frame painted %d sprites, want 0", len(sprites))
	}
	if got := elemF.fontLoadTotal(); got != 0 {
		t.Fatalf("font registry reads before the scheduler runs = %d, want 0", got)
	}
	// The window closure DURING the pending load: the closed window's
	// notification discards through its scope (ticket04's delivery
	// gate) — nothing panics or hangs below.
	gpui.CloseTestWindow(w2)

	// ---- Phase 4: the parse orchestration on a second app. Two
	// suspended consumers share the pending loads; a third parse in a
	// child scope is cancelled before any completion.
	parseF := newSvgFixture(t, bothFonts(t))

	consumer1Deliveries := 0
	var consumer1Svg *gpui.ParsedSvg
	if _, done := parseF.renderer.ParseSvg(parseF.app, parseF.ta.RootScope(), []byte(textSvg), func(o gpui.SvgParseOutcome, cx *gpui.App) {
		consumer1Deliveries++
		consumer1Svg = o.Svg
		if o.Err != nil {
			t.Errorf("consumer 1 error: %v", o.Err)
		}
	}); done {
		t.Fatal("the first consumer's parse finished immediately; the missing font assets must suspend it")
	}
	consumer2Deliveries := 0
	var consumer2Svg *gpui.ParsedSvg
	if _, done := parseF.renderer.ParseSvg(parseF.app, parseF.ta.RootScope(), []byte(textSvg), func(o gpui.SvgParseOutcome, cx *gpui.App) {
		consumer2Deliveries++
		consumer2Svg = o.Svg
		if o.Err != nil {
			t.Errorf("consumer 2 error: %v", o.Err)
		}
	}); done {
		t.Fatal("the second consumer's parse finished immediately; the shared load must suspend it")
	}
	cancelledScope := parseF.ta.RootScope().Child()
	cancelledDeliveries := 0
	if _, done := parseF.renderer.ParseSvg(parseF.app, cancelledScope, []byte(textSvg), func(o gpui.SvgParseOutcome, cx *gpui.App) {
		cancelledDeliveries++
	}); done {
		t.Fatal("the cancelled consumer's parse finished immediately")
	}
	// The discard: closing the owning scope cancels the suspended
	// parse before any bytes arrive.
	cancelledScope.Close()
	if got := parseF.fontLoadTotal(); got != 0 {
		t.Fatalf("font registry reads before the scheduler runs = %d, want 0", got)
	}

	// ---- Phase 4.5: application shutdown discards a suspended parse.
	// A third app's pending parse is cancelled by App.Shutdown before
	// any load body runs (an unstarted cancelled task retires at cancel
	// time): no registry read, no push, no delivery — the retirement
	// discipline through the ticket04 drain.
	shutF := newSvgFixture(t, bothFonts(t))
	shutDeliveries := 0
	if _, done := shutF.renderer.ParseSvg(shutF.app, shutF.ta.RootScope(), []byte(textSvg), func(o gpui.SvgParseOutcome, cx *gpui.App) {
		shutDeliveries++
	}); done {
		t.Fatal("the shutdown-phase parse finished immediately")
	}
	shutF.ta.App().Shutdown()
	if shutDeliveries != 0 {
		t.Fatalf("the shutdown-drained parse delivered %d times, want 0", shutDeliveries)
	}
	if got := shutF.fontLoadTotal(); got != 0 {
		t.Fatalf("the shutdown-drained app read the font registry %d times, want 0 (the cancelled loads never started)", got)
	}

	// The completions: the shared loads run once per path, push the
	// fonts (AddFont), and both live consumers resume exactly once.
	parseF.ta.RunUntilParked()
	if got := parseF.fontLoadTotal(); got != 2 {
		t.Fatalf("font registry reads = %d, want 2 (one per pinned path — the consumers shared the loads)", got)
	}
	if consumer1Deliveries != 1 || consumer2Deliveries != 1 {
		t.Fatalf("consumer deliveries = (%d, %d), want (1, 1): each suspended parse resumes exactly once", consumer1Deliveries, consumer2Deliveries)
	}
	if consumer1Svg == nil || consumer2Svg == nil {
		t.Fatal("a resumed consumer produced no parsed document")
	}
	if consumer1Svg == consumer2Svg {
		t.Fatal("the two consumers share one ParsedSvg value; each resumed parse owns its handle")
	}
	if cancelledDeliveries != 0 {
		t.Fatalf("the cancelled parse delivered %d times, want 0 (the scope closure discards)", cancelledDeliveries)
	}
	for _, path := range paths {
		has, err := parseF.renderer.HasFontAsset(path)
		if err != nil || !has {
			t.Fatalf("HasFontAsset(%s) after the loads = (%v, %v); the bytes must have been pushed via AddFont", path, has, err)
		}
	}
	// The resumed parse resolved the bundled font: the render carries
	// real glyph coverage (ink in the text band).
	image, err := parseF.renderer.RenderParsed(consumer1Svg, gpui.ExactSizeSvg(120, 40))
	if err != nil {
		t.Fatalf("RenderParsed of the resumed parse: %v", err)
	}
	if w, h := image.Image.Size(0); w != 120 || h != 40 {
		t.Fatalf("the resumed parse rendered %dx%d, want 120x40", w, h)
	}
	ink := 0
	pixels := image.Image.Frames[0].Pixels
	for i := 3; i < len(pixels); i += 4 {
		if pixels[i] != 0 {
			ink++
		}
	}
	if ink == 0 {
		t.Fatal("the resumed parse's render carries no glyph coverage; the bundled font did not reach the parse")
	}

	// A post-push parse is immediate (the pin's synchronous parse_svg):
	// no new load, the outcome final.
	postLoads := parseF.fontLoadTotal()
	immediate, done := parseF.renderer.ParseSvg(parseF.app, parseF.ta.RootScope(), []byte(textSvg), func(o gpui.SvgParseOutcome, cx *gpui.App) {
		t.Error("the immediate parse must not deliver asynchronously")
	})
	if !done || immediate.Svg == nil || immediate.Err != nil {
		t.Fatalf("the post-push parse = (%+v, %v), want an immediate parsed document", immediate, done)
	}
	if got := parseF.fontLoadTotal(); got != postLoads {
		t.Fatalf("the post-push parse started new font loads: %d → %d", postLoads, got)
	}
	defer func() {
		_ = consumer2Svg.Dispose()
	}()

	// ---- Phase 5: the element windows' completions. The loads this
	// app's paints started complete (the fonts were already pushed by
	// the parse app — the duplicate push is tolerated), the open
	// window's notification fires once, and the redraw paints the
	// tinted mask through the real pipeline.
	elemF.ta.RunUntilParked()
	if got := elemF.fontLoadTotal(); got != 2 {
		t.Fatalf("the element app's font registry reads = %d, want 2 (the two windows shared the loads)", got)
	}
	if *w1Notifications != 1 {
		t.Fatalf("the open window's view notifications = %d, want exactly 1", *w1Notifications)
	}
	if !w1.RefreshRequested() {
		t.Fatal("the font completion did not mark the window for redraw")
	}

	// The redraw renders the mask: one monochrome sprite at the
	// centering math's bounds, a monochrome-pool tile at the smooth-
	// factor size.
	scene := elemF.draw(t, w1)
	sprites := monochromeSprites(t, scene)
	if len(sprites) != 1 {
		t.Fatalf("the redraw painted %d monochrome sprites, want 1", len(sprites))
	}
	sprite := sprites[0]
	// The test window's profile: scale 2, the element at (0,0,24,12)
	// logical → the device bounds (0,0,48,24); the mask renders at
	// 96x48 (bounds × the smooth 2x) and the sprite draws the tile at
	// half that, centered: exactly (0,0,48,24).
	if got := sprite.Bounds; got.Origin.X != 0 || got.Origin.Y != 0 || got.Size.Width != 48 || got.Size.Height != 24 {
		t.Fatalf("the sprite bounds = %v, want (0, 0, 48 x 24) (the centering math)", got)
	}
	if sprite.Tile.TextureKind != gpui.AtlasTextureMonochrome {
		t.Fatalf("the sprite's tile kind = %d, want the monochrome pool (the pinned AtlasKey::Svg mapping)", sprite.Tile.TextureKind)
	}
	if sprite.Tile.BoundsW != 96 || sprite.Tile.BoundsH != 48 {
		t.Fatalf("the tile = %dx%d, want 96x48 (the snapped bounds × the smooth factor)", sprite.Tile.BoundsW, sprite.Tile.BoundsH)
	}
	// The tinted mask draw: the ambient text color (the default opaque
	// black) with the element opacity (1 here) and the unit
	// transformation (the default at integral centers).
	if sprite.Color != (gpui.Hsla{H: 0, S: 0, L: 0, A: 1}) {
		t.Fatalf("the sprite color = %+v, want the tinted ambient text color (opaque black)", sprite.Color)
	}
	if sprite.Transformation != gpui.UnitTransformation() {
		t.Fatalf("the sprite transformation = %+v, want the unit matrix", sprite.Transformation)
	}

	// The cache hit: a redraw re-draws the SAME tile with no new
	// raster work or atlas allocation.
	tileCountBefore, err := elemF.app.ImageAtlas().TileCount()
	if err != nil {
		t.Fatalf("tile count: %v", err)
	}
	scene = elemF.draw(t, w1)
	sprites = monochromeSprites(t, scene)
	if len(sprites) != 1 || sprites[0].Tile.TileID != sprite.Tile.TileID || sprites[0].Tile.TextureIndex != sprite.Tile.TextureIndex {
		t.Fatalf("the cached redraw's sprite = %+v, want the same tile identity as %+v", sprites, sprite.Tile)
	}
	tileCountAfter, err := elemF.app.ImageAtlas().TileCount()
	if err != nil {
		t.Fatalf("tile count: %v", err)
	}
	if tileCountBefore != tileCountAfter || tileCountBefore != 1 {
		t.Fatalf("the cached redraw changed the tile count: %d → %d, want a stable 1", tileCountBefore, tileCountAfter)
	}

	// Resource retirement: the resumed parses' handles dispose and go
	// stale.
	if err := consumer1Svg.Dispose(); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if err := consumer2Svg.Dispose(); err != nil {
		t.Fatalf("dispose: %v", err)
	}
	if err := immediate.Svg.Dispose(); err != nil {
		t.Fatalf("dispose: %v", err)
	}
}
