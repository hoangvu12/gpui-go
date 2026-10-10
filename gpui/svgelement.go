package gpui

// This file is ticket19's Svg element and its window paint path: the
// pinned crates/gpui/src/elements/svg.rs (the Svg element: path/data/
// external_path, the transformation) and window.rs paint_svg (lines
// 4987-5070: the alpha-mask monochrome sprite path — the
// RenderSvgParams cache key over the snapped bounds scaled by the
// smooth factor, the get_or_insert_with atlas hit, the tinted mask
// draw at the centering math's bounds, the element opacity folded
// into the text color).
//
// Bounded adaptations, recorded at each site:
//
//   - The pin's Interactivity (styled hitboxes, interactive state) is
//     the port's later input ticket; this element carries a plain
//     layout style with the fluent size setters the pin's Styled
//     surface provides, like the port's text element.
//   - The pin paints only when the composed style's text refinement
//     carries a color (style.text.color); the port's ambient text
//     style always resolves one (DefaultTextStyle's opaque black —
//     the pin's TextStyle::default color), so the element paints with
//     the ambient text color unconditionally.
//   - The pin's Window owns the sprite atlas; the port's element
//     paints into the application's image atlas (the port's single
//     application atlas — imagecache.go's recorded adaptation).
//   - The mask rasterization runs synchronously on the foreground
//     paint thread exactly like the pin (render_alpha_mask inside
//     paint_svg; the distribution contract's "CPU decoding need not
//     run on the foreground thread" is permission, not obligation).
//     Only the FONT orchestration is asynchronous (svg.go: the needs-
//     assets loads run on workers; no synchronous foreground callback
//     into the native worker).

import (
	"fmt"
	"log"
	"math"
	"os"
)

// ---------------------------------------------------------------------------
// The transformation (svg.rs Transformation)
// ---------------------------------------------------------------------------

// SvgTransformation is a transformation to apply to an SVG element
// (the pinned svg.rs Transformation, lines 246-312). Note that it
// won't affect the hitbox or layout of the element, only the
// rendering. Build one with the constructors: the pinned Default is
// unit scale, no translation and no rotation, and a zero-value
// SvgTransformation is NOT it (a zero scale collapses the artwork —
// the pin's Default is an explicit scale(1,1)).
type SvgTransformation struct {
	// Scale is the scaling along each axis (the pinned scale field).
	Scale Size
	// Translate is the translation in logical pixels.
	Translate Point
	// Rotate is the rotation in radians.
	Rotate float32
}

// SvgTransformationDefault is the pinned Default: unit scale, no
// translation, no rotation (Transformation::default).
func SvgTransformationDefault() SvgTransformation {
	return SvgTransformation{Scale: Size{Width: 1, Height: 1}}
}

// SvgScaleTransformation creates a transformation with the specified
// scale along each axis (Transformation::scale).
func SvgScaleTransformation(scale Size) SvgTransformation {
	return SvgTransformation{Scale: scale}
}

// SvgTranslateTransformation creates a transformation with the
// specified translation (Transformation::translate).
func SvgTranslateTransformation(translate Point) SvgTransformation {
	return SvgTransformation{Scale: Size{Width: 1, Height: 1}, Translate: translate}
}

// SvgRotateTransformation creates a transformation with the specified
// rotation in radians (Transformation::rotate).
func SvgRotateTransformation(radians float32) SvgTransformation {
	return SvgTransformation{Scale: Size{Width: 1, Height: 1}, Rotate: radians}
}

// WithScaling updates the scaling factor (Transformation::with_scaling).
func (t SvgTransformation) WithScaling(scale Size) SvgTransformation {
	t.Scale = scale
	return t
}

// WithTranslation updates the translation (with_translation).
func (t SvgTransformation) WithTranslation(translate Point) SvgTransformation {
	t.Translate = translate
	return t
}

// WithRotation updates the rotation (with_rotation).
func (t SvgTransformation) WithRotation(radians float32) SvgTransformation {
	t.Rotate = radians
	return t
}

// IntoMatrix composes this transformation into a scene transformation
// matrix around the given center (svg.rs Transformation::into_matrix,
// lines 294-312). Read the composition as a sequence of matrix
// multiplications from the bottom: translate by -center*scale, scale,
// rotate, then translate by (center + translation)*scale.
func (t SvgTransformation) IntoMatrix(center Point, scaleFactor float32) TransformationMatrix {
	//Note: if you read this as a sequence of matrix multiplications, start from the bottom
	return composeTransformation(
		translateTransformation(center.X*scaleFactor+t.Translate.X*scaleFactor,
			center.Y*scaleFactor+t.Translate.Y*scaleFactor),
		composeTransformation(
			rotateTransformation(t.Rotate),
			composeTransformation(
				scaleTransformation(t.Scale),
				translateTransformation(-center.X*scaleFactor, -center.Y*scaleFactor),
			),
		),
	)
}

// translateTransformation is the pinned TransformationMatrix::translate
// (scene.rs lines 986-993): the identity rotation/scale plus the
// translation.
func translateTransformation(x, y float32) TransformationMatrix {
	return TransformationMatrix{
		RotationScale: [2][2]float32{{1, 0}, {0, 1}},
		Translation:   [2]float32{x, y},
	}
}

// rotateTransformation is the pinned TransformationMatrix::rotate
// (scene.rs lines 994-1004): a clockwise rotation in radians around
// the origin. The trig is Go's math.Cos/Sin rounded to f32 (Rust's
// f32::cos/sin may differ in the last ulp; cited deviation).
func rotateTransformation(radians float32) TransformationMatrix {
	c := float32(math.Cos(float64(radians)))
	s := float32(math.Sin(float64(radians)))
	return TransformationMatrix{RotationScale: [2][2]float32{{c, -s}, {s, c}}}
}

// scaleTransformation is the pinned TransformationMatrix::scale
// (scene.rs lines 1005-1012).
func scaleTransformation(size Size) TransformationMatrix {
	return TransformationMatrix{RotationScale: [2][2]float32{{size.Width, 0}, {0, size.Height}}}
}

// composeTransformation is the pinned TransformationMatrix::compose
// (scene.rs lines 1017-1058): the result applies other first, then
// self, with the pinned unit shortcut.
func composeTransformation(self, other TransformationMatrix) TransformationMatrix {
	if other == UnitTransformation() {
		return self
	}
	return TransformationMatrix{
		RotationScale: [2][2]float32{
			{
				self.RotationScale[0][0]*other.RotationScale[0][0] + self.RotationScale[0][1]*other.RotationScale[1][0],
				self.RotationScale[0][0]*other.RotationScale[0][1] + self.RotationScale[0][1]*other.RotationScale[1][1],
			},
			{
				self.RotationScale[1][0]*other.RotationScale[0][0] + self.RotationScale[1][1]*other.RotationScale[1][0],
				self.RotationScale[1][0]*other.RotationScale[0][1] + self.RotationScale[1][1]*other.RotationScale[1][1],
			},
		},
		Translation: [2]float32{
			self.Translation[0] + self.RotationScale[0][0]*other.Translation[0] + self.RotationScale[0][1]*other.Translation[1],
			self.Translation[1] + self.RotationScale[1][0]*other.Translation[0] + self.RotationScale[1][1]*other.Translation[1],
		},
	}
}

// ---------------------------------------------------------------------------
// The Svg element (svg.rs Svg)
// ---------------------------------------------------------------------------

// SvgElement is an SVG element (the pinned svg.rs Svg; the concrete
// spelling because a Go type and the Svg constructor cannot share the
// package-level identifier). Build one with Svg and convert it with
// IntoElement — the fluent builder and the drawable are separate types
// exactly like the port's div.
type SvgElement struct {
	// path is the registry-resolved SVG path (svg.rs path).
	path *string
	// externalPath is the filesystem path loaded asynchronously
	// (svg.rs external_path, through the SvgAsset loader).
	externalPath *string
	// data is the raw SVG bytes (svg.rs data), with dataPath carrying
	// its generated identity path.
	data     []byte
	dataPath *string
	// transformation is the render-time transformation.
	transformation *SvgTransformation
	// elementID is the optional element identity (the interactivity
	// element id).
	elementID *string
	// style is the layout style (the interactivity base style).
	style Style
}

// svgDrawable adapts the builder into the Element phases (the
// reference Drawable<E> shape; the fluent setter surface stays on
// SvgElement because Go cannot overload ID).
type svgDrawable struct {
	svg *SvgElement
}

// Svg creates a new SVG element (svg.rs svg()).
func Svg() *SvgElement {
	return &SvgElement{style: DefaultStyle()}
}

// Path sets the path to the SVG file for this element (Svg::path):
// the renderer's asset registry resolves the bytes during the mask
// paint (render_alpha_mask's registry fallback).
func (s *SvgElement) Path(path string) *SvgElement {
	s.path = &path
	return s
}

// ExternalPath sets an external filesystem path for this element
// (Svg::external_path): the bytes load asynchronously through the
// SvgAsset loader (fs::read on a worker) and the completion redraws.
func (s *SvgElement) ExternalPath(path string) *SvgElement {
	s.externalPath = &path
	return s
}

// Data sets the raw SVG data for this element (Svg::data): the SVG
// renders directly from the provided bytes under a deterministic
// identity path derived from the data hash (the pin's
// __binary_svg__{DefaultHasher hash}; the port's FNV-1a content hash
// — the same adaptation image.go made for the image id).
func (s *SvgElement) Data(data []byte) *SvgElement {
	s.data = data
	path := fmt.Sprintf("__binary_svg__%d", imageContentHash(data))
	s.dataPath = &path
	return s
}

// WithTransformation transforms the SVG element with the given
// transformation (Svg::with_transformation). It affects neither the
// hitbox nor the layout of the element, only the rendering.
func (s *SvgElement) WithTransformation(transformation SvgTransformation) *SvgElement {
	s.transformation = &transformation
	return s
}

// ID assigns this element a string identity (the interactivity
// element id): identity participates in the global element id path.
func (s *SvgElement) ID(value string) *SvgElement {
	s.elementID = &value
	return s
}

// Size sets the element's preferred width and height (the Styled
// size surface the interactivity base style provides).
func (s *SvgElement) Size(width, height Length) *SvgElement {
	s.style.Size = LengthSize{Width: width, Height: height}
	return s
}

// W sets the element's preferred width (Styled::w).
func (s *SvgElement) W(width Length) *SvgElement {
	s.style.Size.Width = width
	return s
}

// H sets the element's preferred height (Styled::h).
func (s *SvgElement) H(height Length) *SvgElement {
	s.style.Size.Height = height
	return s
}

// ID implements Element.
func (d *svgDrawable) ID() (ElementID, bool) {
	if d.svg.elementID == nil {
		return ElementID{}, false
	}
	return NameElementID(*d.svg.elementID), true
}

// SourceLocation implements Element.
func (d *svgDrawable) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element (svg.rs request_layout with
// Interactivity::request_layout, bounded): one layout node under this
// element's style, no children and no intrinsic measurement — the
// element is sized by its style exactly like the pin's
// window.request_layout(style, None, cx).
func (d *svgDrawable) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	layoutID, err := requestLayoutOf(w, d.svg.style)
	if err != nil {
		panic(fmt.Sprintf("gpui: SvgElement request_layout: %v", err))
	}
	return layoutID, struct{}{}
}

// Prepaint implements Element (the interactivity prepaint, bounded to
// the hitbox-free shape of this port's element slice: the committed
// bounds flow into paint).
func (d *svgDrawable) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) struct{} {
	return struct{}{}
}

// Paint implements Element (svg.rs paint): the transformation composed
// at the bounds center with the window scale, then the paint through
// the alpha-mask path in the pinned variant order (data,
// external_path, path). Errors are logged and painting continues —
// the pinned .log_err() surface.
func (d *svgDrawable) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *struct{}, w *Window, app *App) {
	s := d.svg
	// The ambient text color (the pin's style.text.color; the port's
	// ambient style always carries one — the module docs' recorded
	// adaptation).
	color := WindowTextStyle(w).Color

	transformation := UnitTransformation()
	if s.transformation != nil {
		transformation = s.transformation.IntoMatrix(boundsCenter(bounds), ScaleFactorOf(w))
	}

	switch {
	case s.data != nil && s.dataPath != nil:
		if err := PaintSvg(w, app, bounds, *s.dataPath, s.data, transformation, color); err != nil {
			log.Printf("gpui: %v", err)
		}
	case s.externalPath != nil:
		result, done := w.UseSvgAsset(*s.externalPath, app)
		if !done {
			// The load is in flight; the completion redraws the view
			// (use_asset's notification).
			return
		}
		if result.Err != nil {
			log.Printf("gpui: loading the svg asset %q: %v", *s.externalPath, result.Err)
			return
		}
		if err := PaintSvg(w, app, bounds, *s.externalPath, result.Bytes, transformation, color); err != nil {
			log.Printf("gpui: %v", err)
		}
	case s.path != nil:
		if err := PaintSvg(w, app, bounds, *s.path, nil, transformation, color); err != nil {
			log.Printf("gpui: %v", err)
		}
	}
}

// LayoutNodeStyle implements the root-stretch report.
func (d *svgDrawable) LayoutNodeStyle() (Style, bool) { return d.svg.style, true }

// IntoElement implements IntoElement.
func (s *SvgElement) IntoElement() AnyElement {
	return CustomElement[struct{}, struct{}](&svgDrawable{svg: s})
}

// boundsCenter is the pinned Bounds::center (geometry.rs): the origin
// plus half the size.
func boundsCenter(b Bounds) Point {
	return Point{X: b.Origin.X + b.Size.Width/2, Y: b.Origin.Y + b.Size.Height/2}
}

// ---------------------------------------------------------------------------
// The window paint path (window.rs paint_svg, lines 4987-5070)
// ---------------------------------------------------------------------------

// PaintSvg paints a monochrome SVG into the window's current frame at
// the current stacking context (the pinned Window::paint_svg): the
// bounds snap to the device grid, the RenderSvgParams identity keys
// the atlas (bounds × the smooth factor, ceiled), a live tile is the
// cache hit, and a miss renders the alpha mask through the app's SVG
// renderer — gated on the pinned font assets — inserting the tinted
// monochrome sprite at the centering math's bounds. data supplies the
// SVG bytes directly; nil data resolves the path through the
// renderer's asset registry (the pinned Option<&[u8]>).
//
// A pending font load paints nothing this frame: the completion
// re-notifies the requesting window (the pin resolves fonts
// synchronously inside its parse, so it needs no notification; the
// port's async loads must redraw). This method runs inside a frame
// paint (currentFrame).
func PaintSvg(w *Window, app *App, bounds Bounds, path string, data []byte, transformation TransformationMatrix, color Hsla) error {
	if w == nil {
		return fmt.Errorf("gpui: PaintSvg requires a window")
	}
	if app == nil {
		return fmt.Errorf("gpui: PaintSvg requires the application")
	}
	frame := currentFrame(w)
	ctx := frame.paint

	// The pinned element opacity + snapped bounds (window.rs 4997-5010:
	// snap_bounds returns device pixels).
	elementOpacity := ctx.ElementOpacity
	deviceBounds := snapBounds(bounds, ctx.ScaleFactor)

	// RenderSvgParams.size = the snapped bounds × SMOOTH, ceiled
	// (window.rs 5012-5016).
	params := RenderSvgParams{
		Path: path,
		Size: Size{
			Width:  ceilDevice(deviceBounds.Size.Width * SmoothSvgScaleFactor),
			Height: ceilDevice(deviceBounds.Size.Height * SmoothSvgScaleFactor),
		},
	}

	renderer, err := app.SvgRenderer()
	if err != nil {
		return fmt.Errorf("gpui: PaintSvg: %w", err)
	}
	atlas := app.ImageAtlas()
	if atlas == nil {
		return fmt.Errorf("gpui: PaintSvg: the application image atlas is unavailable")
	}

	// The get_or_insert_with cache-hit probe: a live tile draws without
	// any native raster work (the pinned map hit).
	if tile, ok, err := atlas.SvgMaskTile(params); err != nil {
		return fmt.Errorf("gpui: PaintSvg: %w", err)
	} else if ok {
		return insertSvgSprite(frame, ctx, deviceBounds, tile, color, elementOpacity, transformation)
	}

	// The miss: the pinned font assets gate the mask's internal parse
	// (the needs-assets seam). A pending load defers this frame's
	// paint and re-notifies on completion.
	pending, err := renderer.ensureFontAssets(app)
	if err != nil {
		return fmt.Errorf("gpui: PaintSvg: %w", err)
	}
	if len(pending) > 0 {
		view, _ := currentViewOf(w)
		loads := pending
		app.SpawnDelivering(w.Scope(), func(run *TaskRun) struct{} {
			for _, load := range loads {
				_ = run.Await(load)
			}
			return struct{}{}
		}, func(_ struct{}, acx *App) {
			notifyViewLoaded(w, view, acx)
		})
		return nil
	}

	// render_alpha_mask: the bytes (or the registry fallback) rasterize
	// into one alpha byte per pixel.
	mask, width, height, err := renderer.RenderAlphaMask(params, data)
	if err != nil {
		return err
	}
	if mask == nil {
		// The pinned Ok(None): no bytes and no registry asset — nothing
		// to draw.
		return nil
	}
	tile, err := atlas.InsertSvgMask(params, width, height, mask)
	if err != nil {
		return fmt.Errorf("gpui: PaintSvg: %w", err)
	}
	return insertSvgSprite(frame, ctx, deviceBounds, tile, color, elementOpacity, transformation)
}

// insertSvgSprite inserts the tinted monochrome sprite at the pinned
// centering math (window.rs 5038-5068): the svg bounds center the
// tile scaled down by the smooth factor inside the snapped bounds,
// the origin rounds half toward zero, the size ceils, and the sprite
// carries the text color with the element opacity and the element's
// transformation.
func insertSvgSprite(frame *frameDrawState, ctx *PaintContext, deviceBounds Bounds, tile AtlasTile, color Hsla, elementOpacity float32, transformation TransformationMatrix) error {
	contentMask := snappedContentMask(ctx.Mask, ctx.ScaleFactor)
	svgBounds := Bounds{
		Origin: Point{
			X: (deviceBounds.Origin.X + deviceBounds.Size.Width/2) - float32(tile.BoundsW)/SmoothSvgScaleFactor/2,
			Y: (deviceBounds.Origin.Y + deviceBounds.Size.Height/2) - float32(tile.BoundsH)/SmoothSvgScaleFactor/2,
		},
		Size: Size{
			Width:  float32(tile.BoundsW) / SmoothSvgScaleFactor,
			Height: float32(tile.BoundsH) / SmoothSvgScaleFactor,
		},
	}
	finalBounds := Bounds{
		Origin: Point{
			X: roundHalfTowardZero(svgBounds.Origin.X),
			Y: roundHalfTowardZero(svgBounds.Origin.Y),
		},
		Size: Size{
			Width:  ceilDevice(svgBounds.Size.Width),
			Height: ceilDevice(svgBounds.Size.Height),
		},
	}
	return frame.scene.InsertMonochromeSprite(MonochromeSprite{
		Bounds:         finalBounds,
		ContentMask:    contentMask,
		Color:          OpacityHsla(color, elementOpacity),
		Tile:           tile,
		Transformation: transformation,
	})
}

// ceilDevice ceils a device-pixel float to the integral device value
// (the pinned map over f32::ceil; the values here are ceiled integers
// by construction).
func ceilDevice(v float32) float32 {
	return float32(math.Ceil(float64(v)))
}

// ---------------------------------------------------------------------------
// The external-path asset loader (svg.rs SvgAsset + window.rs
// use_asset)
// ---------------------------------------------------------------------------

// SvgAssetResult is the external-path load outcome (the pinned
// Result<Arc<[u8]>, Arc<std::io::Error>>).
type SvgAssetResult struct {
	// Bytes are the SVG file's bytes (nil when Err is set).
	Bytes []byte
	// Err is the filesystem error (nil when Bytes is set).
	Err error
}

// svgAssetLoader is the raw-bytes loader tag keying the app's
// loading-asset cache (the pin's `enum SvgAsset {}` type-level tag,
// svg.rs lines 283-297).
type svgAssetLoader struct{}

// FetchSvgAsset returns the shared raw-bytes load task for the
// filesystem path (App::fetch_asset with the SvgAsset loader): the
// worker reads the file (the pinned SvgAsset::load's fs::read); no App
// state is touched from the worker.
func FetchSvgAsset(cx *App, path string) (Task[SvgAssetResult], bool) {
	return FetchAsset[svgAssetLoader](cx, PathResource(path), func(run *TaskRun) SvgAssetResult {
		if run.Cancelled() {
			return SvgAssetResult{Err: fmt.Errorf("gpui: the svg asset load was cancelled")}
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return SvgAssetResult{Err: err}
		}
		return SvgAssetResult{Bytes: data}
	})
}

// UseSvgAsset asynchronously loads the SVG asset for the path (the
// pinned Window::use_asset with the SvgAsset loader, window.rs lines
// 4013-4033): it returns the finished result, or reports loading and —
// when this call started the load — registers the completion
// notification that redraws the current view. Multiple calls only
// result in one load at a time and the results are cached.
func (w *Window) UseSvgAsset(path string, cx *App) (SvgAssetResult, bool) {
	task, isFirst := FetchSvgAsset(cx, path)
	if task.Done() {
		return task.resultValue(), true
	}
	if isFirst {
		view, _ := currentViewOf(w)
		cx.SpawnDelivering(w.Scope(), func(run *TaskRun) SvgAssetResult {
			return run.Await(task)
		}, func(_ SvgAssetResult, acx *App) {
			notifyViewLoaded(w, view, acx)
		})
	}
	return SvgAssetResult{}, false
}
