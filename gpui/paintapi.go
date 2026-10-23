package gpui

// This file is Base ticket 08's exported element paint API: the pinned
// Window paint methods custom elements call from Element.Paint, plus the
// window geometry reads the overlay elements need. The pinned reference
// is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a
// crates/gpui/src/window.rs (R: below):
//
//   - viewport_size R:2779, set_client_inset / client_inset R:2822-2830,
//     layout_bounds R:5303;
//   - paint_layer R:4339, paint_drop_shadows(_with_corner_smoothing)
//     R:4364-4408, paint_inset_shadows(_with_corner_smoothing)
//     R:4413-4466, paint_quad(_with_corner_smoothing) R:4634-4710,
//     paint_path R:4715, paint_underline R:4732, paint_strikethrough
//     R:4771, paint_image(_with_corner_smoothing) R:5057-5175 and
//     drop_image R:5203.
//
// Every method paints into the frame under construction (the pin's
// next_frame.scene) with the window's current content mask, element
// opacity and scale factor — exactly the state the port's own div paint
// path reads (frame.paint). The pin's debug_assert_paint phase checks map
// to currentFrame's "no frame is being drawn" panic.
//
// Go adaptation: the pinned insert_primitive cannot fail; the port's
// scene inserts through the native scene kernel and report errors. The
// unit-returning pinned methods panic with the operation's context on
// such a failure (the div paint path's convention, div.go paintBox);
// paint_image keeps its pinned Result as an error return.

import (
	"fmt"
	"math"
)

// ViewportSize returns the size of the drawable area within the window
// (R:window.rs:2779 viewport_size): the frame's viewport while a frame is
// being drawn, the last drawn frame's viewport otherwise.
func (w *Window) ViewportSize() Size {
	ds, ok := windowDrawStates[w]
	if !ok {
		return Size{}
	}
	if ds.frame != nil {
		return ds.frame.viewport
	}
	return ds.viewport
}

// SetClientInset records the width of the invisible client-side
// decorations (R:window.rs:2822 set_client_inset). The pin also forwards
// it to the platform window, which only Wayland and X11 consume; the
// Windows platform window has no client inset, so the port only records
// it for ClientInset (the anchored element's snapping margin).
func (w *Window) SetClientInset(inset float32) {
	value := inset
	drawState(w).clientInset = &value
}

// ClientInset returns the inset set by SetClientInset
// (R:window.rs:2828 client_inset; the pinned Option<Pixels>).
func (w *Window) ClientInset() (float32, bool) {
	ds, ok := windowDrawStates[w]
	if !ok || ds.clientInset == nil {
		return 0, false
	}
	return *ds.clientInset, true
}

// LayoutBounds returns the bounds computed for the layout node relative
// to the window (R:window.rs:5303 layout_bounds): the engine's bounds
// plus the pixel-snapped current element offset. It runs inside element
// drawing and panics when the node cannot be resolved (the pin unwraps
// the layout engine).
func (w *Window) LayoutBounds(id LayoutID) Bounds {
	bounds, err := layoutBoundsOf(w, id)
	if err != nil {
		panic(fmt.Sprintf("gpui: Window.LayoutBounds: %v", err))
	}
	return bounds
}

// mustPaintScene panics with the operation's context when a scene insert
// failed (the pinned insert_primitive is infallible; see the file
// comment).
func mustPaintScene(err error, what string) {
	if err != nil {
		panic(fmt.Sprintf("gpui: Window.%s: %v", what, err))
	}
}

// PaintLayer creates a new painting layer for the bounds around f
// (R:window.rs:4339 paint_layer): the layer is pushed with the cover
// bounds of bounds ∩ the content mask when that clip is non-empty, and
// popped after f. Call it only during paint.
func (w *Window) PaintLayer(bounds Bounds, f func(*Window)) {
	frame := currentFrame(w)
	clipped := intersectLogical(bounds, frame.paint.Mask)
	pushed := !isEmptyBounds(clipped)
	if pushed {
		mustPaintScene(frame.scene.PushLayer(coverBounds(clipped, frame.paint.ScaleFactor)), "PaintLayer")
	}
	f(w)
	if pushed {
		mustPaintScene(currentFrame(w).scene.PopLayer(), "PaintLayer")
	}
}

// PaintDropShadows paints the drop (non-inset) shadows into the scene at
// the current z-index (R:window.rs:4364 paint_drop_shadows); inset
// shadows are skipped.
func (w *Window) PaintDropShadows(bounds Bounds, cornerRadii Corners, shadows []BoxShadow) {
	w.PaintDropShadowsWithCornerSmoothing(bounds, cornerRadii, 0, shadows)
}

// PaintDropShadowsWithCornerSmoothing paints drop shadows with smoothed
// corners (R:window.rs:4375-4408).
func (w *Window) PaintDropShadowsWithCornerSmoothing(bounds Bounds, cornerRadii Corners, cornerSmoothing float32, shadows []BoxShadow) {
	frame := currentFrame(w)
	for _, shadow := range shadows {
		if shadow.Inset {
			continue
		}
		mustPaintScene(frame.scene.PaintShadow(bounds, shadow, cornerRadii, cornerSmoothing, frame.paint), "PaintDropShadows")
	}
}

// PaintInsetShadows paints the inset shadows into the scene at the
// current z-index, after the element's background
// (R:window.rs:4413 paint_inset_shadows); drop shadows are skipped.
func (w *Window) PaintInsetShadows(bounds Bounds, cornerRadii Corners, shadows []BoxShadow) {
	w.PaintInsetShadowsWithCornerSmoothing(bounds, cornerRadii, 0, shadows)
}

// PaintInsetShadowsWithCornerSmoothing paints inset shadows with
// smoothed corners (R:window.rs:4424-4466).
func (w *Window) PaintInsetShadowsWithCornerSmoothing(bounds Bounds, cornerRadii Corners, cornerSmoothing float32, shadows []BoxShadow) {
	frame := currentFrame(w)
	for _, shadow := range shadows {
		if !shadow.Inset {
			continue
		}
		mustPaintScene(frame.scene.PaintShadow(bounds, shadow, cornerRadii, cornerSmoothing, frame.paint), "PaintInsetShadows")
	}
}

// PaintQuad paints a quad into the scene for the frame at the current
// stacking context (R:window.rs:4634 paint_quad): see Fill and Outline
// to construct one. A border-only quad is split into strips around its
// empty interior (R:window.rs:4660-4709).
func (w *Window) PaintQuad(quad PaintQuad) {
	w.PaintQuadWithCornerSmoothing(quad, 0)
}

// PaintQuadWithCornerSmoothing paints the quad with smoothed corners
// (R:window.rs:4639 paint_quad_with_corner_smoothing).
func (w *Window) PaintQuadWithCornerSmoothing(quad PaintQuad, cornerSmoothing float32) {
	frame := currentFrame(w)
	mustPaintScene(frame.scene.PaintQuad(quad, cornerSmoothing, frame.paint), "PaintQuad")
}

// PaintPath paints the path into the scene at the current z-index
// (R:window.rs:4715 paint_path): the path takes the current (unsnapped)
// content mask and the color with the element opacity, then scales to
// device pixels.
func (w *Window) PaintPath(path *Path, color Background) {
	frame := currentFrame(w)
	mustPaintScene(frame.scene.PaintPath(path, color, frame.paint), "PaintPath")
}

// PaintUnderline paints an underline at the origin with the width
// (R:window.rs:4732 paint_underline): the stroke-snapped thickness (three
// times for a wavy underline), the origin rounded to the device grid and
// the color with the element opacity.
func (w *Window) PaintUnderline(origin Point, width float32, style UnderlineStyle) {
	frame := currentFrame(w)
	mustPaintScene(frame.scene.PaintUnderline(origin, width, style, frame.paint), "PaintUnderline")
}

// PaintStrikethrough paints a strikethrough at the origin with the width
// (R:window.rs:4771 paint_strikethrough): an underline record whose
// height and thickness are the stroke-snapped thickness, never wavy.
func (w *Window) PaintStrikethrough(origin Point, width float32, style StrikethroughStyle) {
	frame := currentFrame(w)
	ctx := frame.paint
	mustPaintScene(frame.scene.InsertUnderline(strikethroughFor(origin, width, style, ctx.Mask, ctx.ScaleFactor, ctx.ElementOpacity)), "PaintStrikethrough")
}

// strikethroughFor builds the pinned paint_strikethrough record
// (R:window.rs:4779-4795): bounds at the device-rounded origin sized
// (snap_stroke(width), snap_stroke(thickness)), the snapped content mask,
// thickness snap_stroke(thickness), the color with the element opacity
// (palette Hsla opacity, alpha only) and wavy false.
func strikethroughFor(origin Point, width float32, style StrikethroughStyle, mask Bounds, scale, elementOpacity float32) Underline {
	color := Hsla{}
	if style.Color != nil {
		color = *style.Color
	}
	return Underline{
		Bounds: Bounds{
			Origin: Point{
				X: roundToDevicePixel(origin.X, scale),
				Y: roundToDevicePixel(origin.Y, scale),
			},
			Size: Size{Width: snapStroke(width, scale), Height: snapStroke(style.Thickness, scale)},
		},
		ContentMask: snappedContentMask(mask, scale),
		Color:       OpacityHsla(color, elementOpacity),
		Thickness:   snapStroke(style.Thickness, scale),
		Wavy:        false,
	}
}

// PaintImage paints frame frameIndex of the image into bounds,
// positioned and scaled by imageBounds (R:window.rs:5057 paint_image):
// the visible region is bounds ∩ imageBounds with cornerRadii applied.
func (w *Window) PaintImage(bounds, imageBounds Bounds, cornerRadii Corners, data *LoadedImage, frameIndex int, grayscale bool) error {
	return w.PaintImageWithCornerSmoothing(bounds, imageBounds, cornerRadii, 0, data, frameIndex, grayscale)
}

// PaintImageWithCornerSmoothing paints an image with smoothed corners
// (R:window.rs:5078-5175): empty visible or image bounds paint nothing;
// the frame's atlas tile is fetched or inserted under the pinned
// RenderImageParams { image_id, frame_index } key in the window's sprite
// atlas (the atlas its glyphs live in and the present loop passes —
// Window.GlyphAtlas; deterministic test windows fall back to the
// application image atlas); a partially visible image samples the proportional sub-tile
// (rounded and clamped to the tile); the sprite carries the snapped
// visible bounds, the snapped content mask, the corner radii clamped to
// the visible size and scaled, the clamped smoothing and the element
// opacity. An invalid frame index is an error (the pin's "caller's job"
// expect).
func (w *Window) PaintImageWithCornerSmoothing(bounds, imageBounds Bounds, cornerRadii Corners, cornerSmoothing float32, data *LoadedImage, frameIndex int, grayscale bool) error {
	frame := currentFrame(w)
	if data == nil || data.image == nil {
		return fmt.Errorf("gpui: PaintImage: nil image")
	}
	visible := intersectLogical(bounds, imageBounds)
	if visible.Size.Width <= 0 || visible.Size.Height <= 0 {
		return nil
	}
	if imageBounds.Size.Width <= 0 || imageBounds.Size.Height <= 0 {
		return nil
	}
	if frameIndex < 0 || frameIndex >= len(data.image.Frames) {
		return fmt.Errorf("gpui: PaintImage: frame index %d out of range (%d frames)", frameIndex, len(data.image.Frames))
	}
	// The window's sprite atlas (R:window.rs:5103-5104
	// self.sprite_atlas.get_or_insert_with): the atlas the present loop
	// passes, so the sprite batch's texture resolves (Base ticket 40).
	atlas, err := w.spriteAtlas()
	if err != nil {
		return fmt.Errorf("gpui: PaintImage: %w", err)
	}
	tile, err := atlas.InsertImageFrame(data.id, frameIndex, &data.image.Frames[frameIndex])
	if err != nil {
		return fmt.Errorf("gpui: PaintImage: %w", err)
	}

	ctx := frame.paint
	visibleSnapped := snapBounds(visible, ctx.ScaleFactor)
	subTile := tile
	if visible != imageBounds {
		xOffsetRatio := (visible.Origin.X - imageBounds.Origin.X) / imageBounds.Size.Width
		yOffsetRatio := (visible.Origin.Y - imageBounds.Origin.Y) / imageBounds.Size.Height
		widthRatio := visible.Size.Width / imageBounds.Size.Width
		heightRatio := visible.Size.Height / imageBounds.Size.Height

		tileOriginX, tileOriginY := tile.BoundsX, tile.BoundsY
		tileWidth, tileHeight := tile.BoundsW, tile.BoundsH

		subOriginX := tileOriginX + roundToInt32(xOffsetRatio*float32(tileWidth))
		subOriginY := tileOriginY + roundToInt32(yOffsetRatio*float32(tileHeight))
		subWidth := roundToInt32(widthRatio * float32(tileWidth))
		subHeight := roundToInt32(heightRatio * float32(tileHeight))

		maxX := tileOriginX + tileWidth
		maxY := tileOriginY + tileHeight

		clampedOriginX := clampInt32(subOriginX, tileOriginX, maxX)
		clampedOriginY := clampInt32(subOriginY, tileOriginY, maxY)
		clampedWidth := maxInt32(minInt32(subWidth, maxX-clampedOriginX), 0)
		clampedHeight := maxInt32(minInt32(subHeight, maxY-clampedOriginY), 0)

		subTile.BoundsX, subTile.BoundsY = clampedOriginX, clampedOriginY
		subTile.BoundsW, subTile.BoundsH = clampedWidth, clampedHeight
	}

	// CSS Backgrounds 3 §corner-overlap: overflowing radii scale by one
	// common factor (the pin clamped each corner to half the shortest
	// side; see Corners.ConstrainRadiiForQuadSize).
	radii := scaleCorners(cornerRadii.ConstrainRadiiForQuadSize(visible.Size), ctx.ScaleFactor)
	if err := frame.scene.InsertPolychromeSprite(PolychromeSprite{
		Grayscale:       grayscale,
		CornerSmoothing: clamp01(cornerSmoothing),
		Bounds:          visibleSnapped,
		ContentMask:     snappedContentMask(ctx.Mask, ctx.ScaleFactor),
		CornerRadii:     radii,
		Tile:            subTile,
		Opacity:         ctx.ElementOpacity,
	}); err != nil {
		return fmt.Errorf("gpui: PaintImage: %w", err)
	}
	return nil
}

// DropImage removes every frame of the image from the sprite atlases
// (R:window.rs:5203 drop_image). The port removes it through the app's
// drop_image walk (R:app.rs:2815-2825): every open window's sprite atlas
// plus the application image atlas the image cache retains frames in.
func (w *Window) DropImage(data *LoadedImage) error {
	if data == nil || data.image == nil {
		return nil
	}
	return w.app.dropImage(data)
}

// roundToInt32 is the pinned `(value).round() as i32` (f32::round rounds
// half away from zero; `as` saturates).
func roundToInt32(value float32) int32 {
	r := math.Round(float64(value))
	switch {
	case math.IsNaN(r):
		return 0
	case r > math.MaxInt32:
		return math.MaxInt32
	case r < math.MinInt32:
		return math.MinInt32
	}
	return int32(r)
}

// clampInt32 is the pinned i32::clamp.
func clampInt32(v, lo, hi int32) int32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minInt32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

func maxInt32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
