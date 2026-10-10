package svgspec

// This file drives ticket19's Svg element through the deterministic
// test window's full phase pipeline (request_layout / prepaint /
// paint) into a real scene: the tinted mask paint at real bounds (the
// centering math, the element opacity, the ambient text color), the
// three byte sources (data, the registry path with the pinned Ok(None)
// miss, the external file path through the use_asset redraw), the
// transformation composition, and malformed inputs failing honestly
// through the pinned log_err surface.
//
// These gates are order-tolerant: they run after font_test.go has
// pushed the pinned fonts, so every paint renders immediately (the
// pending flow is font_test.go's phase-3/5 story).

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gpui-go/gpui"
)

// resolvedDraw draws one frame, drains the scheduler (completing any
// pending font loads this fixture's registry can resolve) and draws
// again, returning the last scene. Order-tolerant: with the pinned
// fonts already pushed the first frame renders and the drain is a
// no-op (the second frame is then the cache-hit redraw); while a font
// load is pending the completion redraws through the real pipeline.
func (f *svgFixture) resolvedDraw(t *testing.T, window *gpui.Window) *gpui.Scene {
	t.Helper()
	f.draw(t, window)
	f.ta.RunUntilParked()
	return f.draw(t, window)
}

// svgWindowOf opens one window rendering one Svg element directly as
// the root (the definite style keeps the root unstretched). The tree
// is built fresh per frame.
func svgWindowOf(t *testing.T, f *svgFixture, elem func() *gpui.SvgElement) (*gpui.Window, *int) {
	t.Helper()
	return f.newSvgWindow(t, func() gpui.AnyElement { return elem().IntoElement() })
}

// svgElem builds the element gate's root element (the 24x12 text
// document at a definite size).
func svgElem() *gpui.SvgElement {
	return gpui.Svg().Data([]byte(smallTextSvg)).ID("svg").Size(gpui.PxLength(24), gpui.PxLength(12))
}

// divSvgWindowOf opens one window rendering a div wrapping the Svg
// element, so the div's text color and element opacity cascade into
// the child paint. The tree is built fresh per frame.
func divSvgWindowOf(t *testing.T, f *svgFixture, div func() *gpui.DivElement) (*gpui.Window, *int) {
	t.Helper()
	return f.newSvgWindow(t, func() gpui.AnyElement { return div().IntoElement() })
}

// TestSvgElementCenteringAndTint pins the paint geometry and the tint
// through a real element: a 24x24 element bounds over the 24x12
// document (the aspect-mismatched case), the text color and element
// opacity from the wrapping div, and the cache hit across redraws.
func TestSvgElementCenteringAndTint(t *testing.T) {
	f := newSvgFixture(t, bothFonts(t))
	red := gpui.RgbaToHsla(0xff0000ff)
	newDiv := func() *gpui.DivElement {
		return gpui.Div().
			Size(gpui.PxLength(24), gpui.PxLength(24)).
			Opacity(0.5).
			TextColor(red).
			Child(gpui.Svg().Data([]byte(smallTextSvg)).ID("svg").Size(gpui.PxLength(24), gpui.PxLength(24)))
	}
	window, _ := divSvgWindowOf(t, f, newDiv)

	sprites := monochromeSprites(t, f.resolvedDraw(t, window))
	if len(sprites) != 1 {
		t.Fatalf("the frame painted %d monochrome sprites, want 1", len(sprites))
	}
	sprite := sprites[0]
	// The mismatched aspect: the element's device bounds are (0,0,48,48)
	// (scale 2), the mask renders 96x48 (the document's 2:1 aspect under
	// the Size mode), and the sprite draws the tile at half that,
	// vertically centered: origin (0, 12), size 48x24.
	if got := sprite.Bounds; got.Origin.X != 0 || got.Origin.Y != 12 || got.Size.Width != 48 || got.Size.Height != 24 {
		t.Fatalf("the sprite bounds = %v, want (0, 12, 48 x 24) (the aspect-mismatched centering)", got)
	}
	if sprite.Tile.BoundsW != 96 || sprite.Tile.BoundsH != 48 {
		t.Fatalf("the tile = %dx%d, want 96x48", sprite.Tile.BoundsW, sprite.Tile.BoundsH)
	}
	// The tinted mask: the div's text color with the div's element
	// opacity folded into the alpha (the pinned
	// color.opacity(element_opacity)).
	want := gpui.Hsla{H: red.H, S: red.S, L: red.L, A: 0.5}
	if sprite.Color != want {
		t.Fatalf("the sprite color = %+v, want the tinted text color %+v", sprite.Color, want)
	}
	// The default transformation is the unit matrix (the element has
	// none; integral centers compose exactly).
	if sprite.Transformation != gpui.UnitTransformation() {
		t.Fatalf("the sprite transformation = %+v, want unit", sprite.Transformation)
	}
	// The content mask is the snapped viewport (cover bounds of the
	// 200x100 logical window at scale 2: 400x200 device pixels).
	if got := sprite.ContentMask; got.Size.Width != 400 || got.Size.Height != 200 {
		t.Fatalf("the sprite content mask = %v, want the snapped window bounds 400x200", got)
	}

	// The redraw is the cache hit: the same tile, no new allocation.
	before, err := f.app.ImageAtlas().TileCount()
	if err != nil {
		t.Fatalf("tile count: %v", err)
	}
	sprites = monochromeSprites(t, f.draw(t, window))
	if len(sprites) != 1 || sprites[0].Tile.TileID != sprite.Tile.TileID {
		t.Fatalf("the cached redraw changed the tile: %+v vs %+v", sprites[0].Tile, sprite.Tile)
	}
	after, err := f.app.ImageAtlas().TileCount()
	if err != nil {
		t.Fatalf("tile count: %v", err)
	}
	if before != after || before != 1 {
		t.Fatalf("the cached redraw changed the tile count: %d → %d, want a stable 1", before, after)
	}
}

// TestSvgElementTransformation pins the pinned into_matrix composition
// at the element: a logical translation of (3, 1) composes at the
// bounds center with the window scale (2), leaving the scaled
// translation (6, 2) in the sprite's matrix.
func TestSvgElementTransformation(t *testing.T) {
	f := newSvgFixture(t, bothFonts(t))
	elem := func() *gpui.SvgElement {
		return gpui.Svg().
			Data([]byte(smallTextSvg)).
			ID("svg").
			Size(gpui.PxLength(24), gpui.PxLength(12)).
			WithTransformation(gpui.SvgTranslateTransformation(gpui.Point{X: 3, Y: 1}))
	}
	window, _ := svgWindowOf(t, f, elem)

	sprites := monochromeSprites(t, f.resolvedDraw(t, window))
	if len(sprites) != 1 {
		t.Fatalf("the frame painted %d monochrome sprites, want 1", len(sprites))
	}
	transformation := sprites[0].Transformation
	if transformation.Translation != [2]float32{6, 2} {
		t.Fatalf("the sprite transformation translation = %v, want [6 2] (the translation scaled by the window factor)", transformation.Translation)
	}
	if transformation.RotationScale != [2][2]float32{{1, 0}, {0, 1}} {
		t.Fatalf("the sprite transformation rotation/scale = %v, want the identity", transformation.RotationScale)
	}
}

// TestSvgElementRegistryPath pins the path variant: the renderer's
// asset registry resolves the bytes during the mask paint
// (render_alpha_mask's registry fallback), and a path the registry
// does not carry paints nothing — the pinned Ok(None), no error.
func TestSvgElementRegistryPath(t *testing.T) {
	f := newSvgFixture(t, bothFonts(t))
	if err := f.app.Assets().Insert("icons/x.svg", gpui.PreLoaded([]byte(smallTextSvg))); err != nil {
		t.Fatalf("registry insert: %v", err)
	}

	window, _ := svgWindowOf(t, f, func() *gpui.SvgElement {
		return gpui.Svg().Path("icons/x.svg").ID("svg").Size(gpui.PxLength(24), gpui.PxLength(12))
	})
	sprites := monochromeSprites(t, f.resolvedDraw(t, window))
	if len(sprites) != 1 {
		t.Fatalf("the registry-path frame painted %d monochrome sprites, want 1", len(sprites))
	}
	if sprites[0].Tile.BoundsW != 96 || sprites[0].Tile.BoundsH != 48 {
		t.Fatalf("the registry-path tile = %dx%d, want 96x48", sprites[0].Tile.BoundsW, sprites[0].Tile.BoundsH)
	}

	// The pinned Ok(None): a missing registry path paints nothing and
	// logs nothing.
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)
	missingWindow, _ := svgWindowOf(t, f, func() *gpui.SvgElement {
		return gpui.Svg().Path("icons/missing.svg").ID("svg").Size(gpui.PxLength(24), gpui.PxLength(12))
	})
	if sprites := monochromeSprites(t, f.draw(t, missingWindow)); len(sprites) != 0 {
		t.Fatalf("the missing-path frame painted %d sprites, want 0 (the pinned Ok(None))", len(sprites))
	}
	if logs.Len() != 0 {
		t.Fatalf("the missing-path frame logged %q, want silence (Ok(None) is not an error)", logs.String())
	}
}

// TestSvgElementExternalPath pins the external_path variant: the bytes
// load asynchronously off the filesystem (the pinned SvgAsset's
// fs::read), the completion notification redraws, and the redraw paints
// the mask.
func TestSvgElementExternalPath(t *testing.T) {
	f := newSvgFixture(t, bothFonts(t))
	path := filepath.Join(t.TempDir(), "icon.svg")
	if err := os.WriteFile(path, []byte(smallTextSvg), 0o644); err != nil {
		t.Fatalf("writing the external svg: %v", err)
	}

	window, notifications := svgWindowOf(t, f, func() *gpui.SvgElement {
		return gpui.Svg().ExternalPath(path).ID("svg").Size(gpui.PxLength(24), gpui.PxLength(12))
	})
	if sprites := monochromeSprites(t, f.draw(t, window)); len(sprites) != 0 {
		t.Fatalf("the external-path frame painted %d sprites before the file read, want 0", len(sprites))
	}
	f.ta.RunUntilParked()
	// The completion notification count is order-dependent: with the
	// pinned fonts already pushed only the file read notifies (1);
	// while a font load is also pending its completion notifies too.
	// Either way the file load's completion must have notified.
	if *notifications < 1 {
		t.Fatal("the external-path completion did not notify the view (the use_asset redraw)")
	}
	if !window.RefreshRequested() {
		t.Fatal("the external-path completion did not mark the window for redraw")
	}
	// The redraw resolves the bytes and paints the mask (a further
	// pending font load — the isolated ordering — resolves through the
	// same draw/drain/draw sequence).
	sprites := monochromeSprites(t, f.resolvedDraw(t, window))
	if len(sprites) != 1 {
		t.Fatalf("the external-path redraw painted %d sprites, want 1", len(sprites))
	}
	if sprites[0].Tile.BoundsW != 96 || sprites[0].Tile.BoundsH != 48 {
		t.Fatalf("the external-path tile = %dx%d, want 96x48", sprites[0].Tile.BoundsW, sprites[0].Tile.BoundsH)
	}
}

// TestSvgElementMalformedData pins the honest failure: garbage data
// fails the mask's parse with the typed parse error (logged and
// painting continues — the pinned log_err surface), painting no sprite.
func TestSvgElementMalformedData(t *testing.T) {
	f := newSvgFixture(t, bothFonts(t))
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)

	window, _ := svgWindowOf(t, f, func() *gpui.SvgElement {
		return gpui.Svg().Data([]byte("not an svg at all")).ID("svg").Size(gpui.PxLength(24), gpui.PxLength(12))
	})
	if sprites := monochromeSprites(t, f.resolvedDraw(t, window)); len(sprites) != 0 {
		t.Fatalf("the malformed-data frame painted %d sprites, want 0", len(sprites))
	}
	// The typed parse error surfaced through the log_err path.
	if !strings.Contains(logs.String(), "native: svg parse or rasterize failed") {
		t.Fatalf("the malformed-data failure logged %q, want the typed svg parse error", logs.String())
	}
}

// TestSvgEmojiPresentation exercises the emoji-presentation selection
// through a real render (the native fallback picks Segoe UI Emoji for
// emoji-presentation characters — the selection lives inside the
// native service). The availability record: this machine's Windows 11
// system fonts carry Segoe UI Emoji, so the render resolves a color
// glyph; the colored-pixel count below is the honest observation (a
// monochrome fallback would report zero).
func TestSvgEmojiPresentation(t *testing.T) {
	f := newSvgFixture(t, bothFonts(t))
	svg := f.parseResolved(t, []byte(emojiSvg))
	defer func() { _ = svg.Dispose() }()
	image, err := f.renderer.RenderParsed(svg, gpui.ExactSizeSvg(64, 64))
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if w, h := image.Image.Size(0); w != 64 || h != 64 {
		t.Fatalf("the emoji render = %dx%d, want 64x64", w, h)
	}
	ink, colored := 0, 0
	pixels := image.Image.Frames[0].Pixels
	for i := 0; i+3 < len(pixels); i += 4 {
		if pixels[i+3] != 0 {
			ink++
			b, g, r := pixels[i], pixels[i+1], pixels[i+2]
			if b != r || g != r {
				colored++
			}
		}
	}
	if ink == 0 {
		t.Fatal("the emoji render carries no glyph coverage; the presentation fallback resolved nothing")
	}
	t.Logf("the emoji presentation through the real render: %d ink pixels, %d colored (Segoe UI Emoji availability on this machine)", ink, colored)
}
