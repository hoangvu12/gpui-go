package gpui

// This file is Base ticket 08's img element: the pinned
// crates/gpui/src/elements/img.rs (GPUI-CE 254b5dbd, R: below) — the
// ImageSource enum and its conversions (R:img.rs:40-126), the StyledImage
// style (R:img.rs:128-189), the Img element's request_layout / prepaint /
// paint (R:img.rs:191-512) and ImageSource::use_data / get_data /
// remove_asset (R:img.rs:536-598) with the ImageDecoder asset behind
// ImageSource::Image (R:img.rs:600-614). The resource loader, the image
// cache, the playback state machine (ImgState) and LOADING_DELAY are
// ticket 18's imagecache.go; the sprite painting is Window.PaintImage
// (paintapi.go).
//
// Bounded deviations, recorded:
//
//   - The pin's Img carries a full div Interactivity (hitbox, listeners,
//     hover/active styles; InteractiveElement + StatefulInteractiveElement).
//     The port's interactivity lives inside DivElement (div.go, not a
//     reusable Interactivity), so the img element carries the base style
//     refinement (the Styled vocabulary) and an element id only: it
//     paints its style box, opacity, text style and overflow mask around
//     the image like Interactivity::paint, but registers no hitbox or
//     listeners. Wrap it in a div for interactivity.
//   - There is no ImageCacheElement / window image_cache_stack in the
//     port (imagecache.go's deferred pieces), so an img without an
//     explicit ImageCache uses the window's use_asset path.
//   - SVG bytes (ImageSourceImage with ImageFormatSVG, and resource
//     bytes the codec cannot sniff) rasterize through the App's
//     SvgRenderer (svg.go, ticket19) at render_single_frame(bytes, 1.0)
//     like the pin (platform.rs:3659-3663, img.rs:737-740). The
//     renderer's parse does not run ticket19's bundled-font
//     orchestration (ParseSvg's needs-assets suspension) first: SVG
//     text resolves against the fonts already pushed to the native
//     registry plus the system fonts, where the pin's resolver would
//     load the bundled fonts on the first text lookup.
//   - A zero-frame image (or a zero-height frame) sets no intrinsic
//     aspect ratio: the pin stores the NaN of 0/0 into the style, which
//     the port's native layout engine does not accept.
//   - The animation clock is the application clock (App.Now, ticket 18's
//     documented adaptation of the pin's Instant::now()).

import (
	"fmt"
	"log"
	"net/url"
)

// ImageSourceKind selects the ImageSource variant (R:img.rs:41-51).
type ImageSourceKind uint8

const (
	// ImageSourceResource loads the content from a resource location
	// (ImageSource::Resource).
	ImageSourceResource ImageSourceKind = iota + 1
	// ImageSourceRender is already-decoded image data
	// (ImageSource::Render(Arc<RenderImage>)).
	ImageSourceRender
	// ImageSourceImage is encoded image bytes with a known format, decoded
	// through the asset system (ImageSource::Image(Arc<Image>)).
	ImageSourceImage
	// ImageSourceCustom is a custom loading function
	// (ImageSource::Custom).
	ImageSourceCustom
)

// ImageSource is a source of image content (R:img.rs:41-51). Build one
// with the ImageSource* constructors, or pass any convertible value to
// Img.
type ImageSource struct {
	// Kind selects the variant.
	Kind ImageSourceKind
	// Resource is the ImageSourceResource location.
	Resource Resource
	// Render is the ImageSourceRender data.
	Render *LoadedImage
	// Image is the ImageSourceImage data.
	Image Image
	// Custom is the ImageSourceCustom loading function: the finished
	// result, or false while loading (the pinned
	// Option<Result<Arc<RenderImage>, ImageCacheError>>).
	Custom func(w *Window, app *App) (ImageAssetResult, bool)
}

// ImageSourceOfResource is the pinned ImageSource::Resource.
func ImageSourceOfResource(resource Resource) ImageSource {
	return ImageSource{Kind: ImageSourceResource, Resource: resource}
}

// ImageSourceFromString converts a string like the pinned From<&str> /
// From<String> / From<SharedString> (R:img.rs:53-87): a string that
// parses as a URI is a URI resource, anything else an embedded resource.
// The pinned is_uri is url::Url::from_str(..).is_ok(), which requires a
// scheme; the port accepts what net/url parses with a non-empty scheme.
func ImageSourceFromString(s string) ImageSource {
	if isImageURI(s) {
		return ImageSourceOfResource(URIResource(s))
	}
	return ImageSourceOfResource(EmbeddedResource(s))
}

// isImageURI is the pinned is_uri (R:img.rs:53-55), bounded to net/url.
func isImageURI(s string) bool {
	u, err := url.Parse(s)
	return err == nil && u.Scheme != ""
}

// ImageSourceFromPath converts a filesystem path (the pinned From<&Path> /
// From<PathBuf> / From<Arc<Path>>, R:img.rs:89-105).
func ImageSourceFromPath(path string) ImageSource {
	return ImageSourceOfResource(PathResource(path))
}

// ImageSourceFromRender wraps already-decoded image data (the pinned
// From<Arc<RenderImage>>, R:img.rs:107-111).
func ImageSourceFromRender(image *LoadedImage) ImageSource {
	if image == nil {
		panic("gpui: ImageSourceFromRender requires an image")
	}
	return ImageSource{Kind: ImageSourceRender, Render: image}
}

// ImageSourceFromImage wraps encoded image bytes (the pinned
// From<Arc<Image>>, R:img.rs:113-117).
func ImageSourceFromImage(image Image) ImageSource {
	return ImageSource{Kind: ImageSourceImage, Image: image}
}

// ImageSourceFromCustom wraps a custom loading function (the pinned
// From<F: Fn(&mut Window, &mut App) -> Option<Result<..>>>,
// R:img.rs:119-126).
func ImageSourceFromCustom(load func(w *Window, app *App) (ImageAssetResult, bool)) ImageSource {
	if load == nil {
		panic("gpui: ImageSourceFromCustom requires a loading function")
	}
	return ImageSource{Kind: ImageSourceCustom, Custom: load}
}

// imageSourceOf converts an Img source value (the pinned
// `impl Into<ImageSource>`).
func imageSourceOf(value any) ImageSource {
	switch v := value.(type) {
	case ImageSource:
		return v
	case *ImageSource:
		if v == nil {
			panic("gpui: Img: nil image source")
		}
		return *v
	case Resource:
		return ImageSourceOfResource(v)
	case string:
		return ImageSourceFromString(v)
	case *LoadedImage:
		return ImageSourceFromRender(v)
	case Image:
		return ImageSourceFromImage(v)
	case *Image:
		if v == nil {
			panic("gpui: Img: nil image")
		}
		return ImageSourceFromImage(*v)
	case func(w *Window, app *App) (ImageAssetResult, bool):
		return ImageSourceFromCustom(v)
	}
	panic(fmt.Sprintf("gpui: Img: unsupported image source %T (use an ImageSource constructor)", value))
}

// UseData resolves the source's image data, registering the redraw of
// the current view when a load completes (R:img.rs:537-555
// ImageSource::use_data): a resource loads through the cache when one is
// given, the window's use_asset otherwise; decoded data resolves at once;
// encoded bytes decode through the ImageDecoder asset; a custom source
// calls its function. The second result is false while loading.
func (s ImageSource) UseData(cache AnyImageCache, w *Window, app *App) (ImageAssetResult, bool) {
	switch s.Kind {
	case ImageSourceResource:
		if cache != nil {
			return cache.LoadImage(s.Resource, w, app)
		}
		return w.UseImageAsset(s.Resource, app)
	case ImageSourceCustom:
		return s.Custom(w, app)
	case ImageSourceRender:
		return ImageAssetResult{Image: s.Render}, true
	case ImageSourceImage:
		return w.useDecodedImage(s.Image, app, true)
	}
	return ImageAssetResult{Err: &ImageCacheError{Kind: ImageCacheErrorOther, Message: "invalid image source"}}, true
}

// GetData resolves the source's image data without the redraw
// registration (R:img.rs:557-575 ImageSource::get_data).
func (s ImageSource) GetData(cache AnyImageCache, w *Window, app *App) (ImageAssetResult, bool) {
	switch s.Kind {
	case ImageSourceResource:
		if cache != nil {
			return cache.LoadImage(s.Resource, w, app)
		}
		return w.GetImageAsset(s.Resource, app)
	case ImageSourceCustom:
		return s.Custom(w, app)
	case ImageSourceRender:
		return ImageAssetResult{Image: s.Render}, true
	case ImageSourceImage:
		return w.useDecodedImage(s.Image, app, false)
	}
	return ImageAssetResult{Err: &ImageCacheError{Kind: ImageCacheErrorOther, Message: "invalid image source"}}, true
}

// RemoveAsset removes this image source from the asset system
// (R:img.rs:578-586 ImageSource::remove_asset): custom and decoded
// sources hold no asset.
func (s ImageSource) RemoveAsset(app *App) {
	switch s.Kind {
	case ImageSourceResource:
		RemoveImageAsset(app, s.Resource)
	case ImageSourceImage:
		RemoveAsset[imageDecoderLoader](app, s.Image)
	}
}

// IsAssetCached reports whether the source is present in the asset
// system, loading or loaded, without fetching it (the pinned
// test-support is_asset_cached, R:img.rs:590-597).
func (s ImageSource) IsAssetCached(app *App) bool {
	switch s.Kind {
	case ImageSourceResource:
		return HasImageAsset(app, s.Resource)
	case ImageSourceImage:
		return HasAsset[imageDecoderLoader](app, s.Image)
	}
	return false
}

// imageDecoderLoader is the pinned ImageDecoder asset tag
// (R:img.rs:600-614, used as AssetLogger<ImageDecoder>).
type imageDecoderLoader struct{}

// useDecodedImage is window.use_asset::<AssetLogger<ImageDecoder>> (and
// get_asset when notify is false; window.rs use_asset/get_asset): the
// shared decode task resolves or reports loading, and the first requester
// registers the completion redraw of the current view.
func (w *Window) useDecodedImage(image Image, app *App, notify bool) (ImageAssetResult, bool) {
	codec, codecErr := app.appImageCodec()
	// The pinned `let renderer = cx.svg_renderer();` (R:img.rs:611).
	svg, svgErr := app.SvgRenderer()
	source := image
	task, isFirst := FetchAsset[imageDecoderLoader](app, source, func(run *TaskRun) ImageAssetResult {
		result := decodeImageData(codec, codecErr, svg, svgErr, source)
		if result.Err != nil {
			// The pinned AssetLogger wrapping (asset_cache.rs:40-63).
			log.Printf("gpui: Failed to load asset: %v", result.Err)
		}
		return result
	})
	if task.Done() {
		return task.resultValue(), true
	}
	if notify && isFirst {
		view, _ := currentViewOf(w)
		app.SpawnDelivering(w.Scope(), func(run *TaskRun) ImageAssetResult {
			return run.Await(task)
		}, func(_ ImageAssetResult, acx *App) {
			notifyViewLoaded(w, view, acx)
		})
	}
	return ImageAssetResult{}, false
}

// decodeImageData is the pinned ImageDecoder::load body
// (R:img.rs:607-613 → Image::to_image_data, platform.rs:3626-3668): the
// known-format clipboard entry decodes the bytes (GIF frames walked, the
// other formats statically) and ImageFormat::Svg renders through
// svg_renderer.render_single_frame(&self.bytes, 1.0)
// (platform.rs:3659-3663); errors become ImageCacheError::Other (the
// anyhow conversion: to_image_data returns anyhow::Result, so even the
// SVG error lands in Other, R:img.rs:612).
func decodeImageData(codec *ImageCodec, codecErr error, svg *SvgRenderer, svgErr error, image Image) ImageAssetResult {
	if image.Format == ImageFormatSVG {
		if svgErr != nil {
			return imageErrorResult(ImageCacheErrorOther, svgErr, "")
		}
		rendered, err := svg.RenderSingleFrame(image.Bytes, 1.0)
		if err != nil {
			return imageErrorResult(ImageCacheErrorOther, err, "")
		}
		return ImageAssetResult{Image: newLoadedSvgImage(rendered)}
	}
	if codecErr != nil {
		return imageErrorResult(ImageCacheErrorOther, codecErr, "")
	}
	render, err := codec.DecodeClipboard(image.Bytes, image.Format)
	if err != nil {
		return imageErrorResult(ImageCacheErrorOther, err, "")
	}
	return ImageAssetResult{Image: newLoadedImage(render)}
}

// ---------------------------------------------------------------------------
// The img element (img.rs Img)
// ---------------------------------------------------------------------------

// ImgElement is an image element (R:img.rs:191-197 Img). Build one with
// Img; it carries the Styled vocabulary and the StyledImage setters.
type ImgElement struct {
	// Styled is the element's base style refinement (R:img.rs:514-518).
	Styled[*ImgElement]
	elementID  *ElementID
	source     ImageSource
	grayscale  bool
	objectFit  ObjectFit
	loading    func() AnyElement
	fallback   func() AnyElement
	imageCache AnyImageCache
	lastStyle  Style
}

// Img creates a new image element (R:img.rs:201 img): source is an
// ImageSource or any value the pinned conversions accept — a Resource, a
// string (URI or embedded key), a *LoadedImage, an Image, or a custom
// loading function.
func Img(source any) *ImgElement {
	return BindStyled(&ImgElement{source: imageSourceOf(source), objectFit: ObjectFitContain})
}

// ImgExtensions lists every format extension the img element supports
// (R:img.rs:212-218 Img::extensions: image::ImageFormat::from_extension's
// list plus svg).
func ImgExtensions() []string {
	return []string{
		"avif", "jpg", "jpeg", "png", "gif", "webp", "tif", "tiff", "tga", "dds", "bmp", "ico",
		"hdr", "exr", "pbm", "pam", "ppm", "pgm", "ff", "farbfeld", "qoi", "svg",
	}
}

// ID assigns the element an identity (InteractiveElement::id): an
// identified img retains its playback state, animates and shows the
// loading replacement.
func (i *ImgElement) ID[I IntoElementID](value I) *ImgElement {
	id := ElementIDOf(value)
	i.elementID = &id
	return i
}

// Grayscale sets the image to be displayed in grayscale
// (R:img.rs:153 StyledImage::grayscale).
func (i *ImgElement) Grayscale(grayscale bool) *ImgElement {
	i.grayscale = grayscale
	return i
}

// ObjectFit sets the object fit for the image (R:img.rs:159; Contain by
// default, R:img.rs:136-144).
func (i *ImgElement) ObjectFit(fit ObjectFit) *ImgElement {
	i.objectFit = fit
	return i
}

// WithFallback sets the element rendered should the image fail to load
// (R:img.rs:166 with_fallback).
func (i *ImgElement) WithFallback(fallback func() AnyElement) *ImgElement {
	i.fallback = fallback
	return i
}

// WithLoading sets the element rendered while the image is still loading
// after LoadingDelay (R:img.rs:173 with_loading).
func (i *ImgElement) WithLoading(loading func() AnyElement) *ImgElement {
	i.loading = loading
	return i
}

// ImageCache sets the image cache for this node (R:img.rs:230
// image_cache).
func (i *ImgElement) ImageCache(cache AnyImageCache) *ImgElement {
	i.imageCache = cache
	return i
}

// IntoElement implements IntoElement (R:img.rs:526-532).
func (i *ImgElement) IntoElement() AnyElement {
	return CustomElement[imgLayoutState, struct{}](&imgDrawable{img: i})
}

// computeStyle is the base style (Style::default() refined by the base
// style refinement).
func (i *ImgElement) computeStyle() Style {
	style := DefaultStyle()
	style.Refine(i.Styled.storage())
	return style
}

// imgLayoutState is R:img.rs:259-263 ImgLayoutState.
type imgLayoutState struct {
	frameIndex  int
	replacement *AnyElement
}

// imgDrawable adapts the builder into the Element phases.
type imgDrawable struct {
	img *ImgElement
}

// ID implements Element (R:img.rs:269).
func (d *imgDrawable) ID() (ElementID, bool) {
	if d.img.elementID == nil {
		return ElementID{}, false
	}
	return *d.img.elementID, true
}

// SourceLocation implements Element.
func (d *imgDrawable) SourceLocation() *SourceLocation { return nil }

// absoluteLengthPixels resolves a definite absolute length (px or rem) to
// pixels; false for fractions (the pinned
// Length::Definite(DefiniteLength::Absolute(_)) match).
func absoluteLengthPixels(l Length, rem float32) (float32, bool) {
	if l.Auto {
		return 0, false
	}
	switch l.Definite.Kind {
	case DefiniteLengthPx:
		return l.Definite.Value, true
	case DefiniteLengthRem:
		return l.Definite.Value * rem, true
	}
	return 0, false
}

// renderSize is the pinned RenderImage::render_size (assets.rs:267-270):
// the frame's pixel size over the image's scale factor (1.0 for decoded
// raster images, SmoothSvgScaleFactor for a rendered SVG).
func renderSize(image *LoadedImage, frameIndex int) Size {
	w, h := image.Size(frameIndex)
	scale := image.ScaleFactor()
	return Size{Width: float32(w) / scale, Height: float32(h) / scale}
}

// RequestLayout implements Element (R:img.rs:277-433): inside the
// element's optional playback state, the source resolves — loaded data
// advances the playback, sizes the auto axes from the image's intrinsic
// size and aspect ratio and requests the next animation frame; an error
// lays out the fallback; a load still in flight starts the loading delay
// or, once it elapsed, lays out the loading replacement — then the node
// is requested with the replacement as its child.
func (d *imgDrawable) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, imgLayoutState) {
	img := d.img
	layout := imgLayoutState{}
	layoutID := WithOptionalElementState(w, global, func(slot *ImagePlayback, w *Window) (LayoutID, *ImagePlayback) {
		var state *ImagePlayback
		if global != nil {
			state = slot
			if state == nil {
				state = &ImagePlayback{}
			}
		}
		frameIndex := 0
		if state != nil {
			frameIndex = state.FrameIndex
		}
		style := img.computeStyle()
		var id LayoutID
		WithTextStyleVoid(w, style.TextStyle(), func(w *Window) {
			var replacementIDs []LayoutID
			result, done := img.source.UseData(img.imageCache, w, app)
			switch {
			case done && result.Err == nil && result.Image != nil:
				data := result.Image
				policy := ImagePlaybackPolicy{ReduceMotion: app.ReduceMotion(), WindowActive: w.isWindowActiveCached()}
				if state != nil {
					frameIndex = state.Advance(data.image, app.Now(), policy)
					state.Resolved()
				}
				size := renderSize(data, frameIndex)
				if style.AspectRatio == nil && size.Height != 0 {
					ratio := size.Width / size.Height
					style.AspectRatio = &ratio
				}
				rem := RemSize(w)
				if style.Size.Width.Auto {
					if height, ok := absoluteLengthPixels(style.Size.Height, rem); ok && size.Height != 0 {
						style.Size.Width = PxLength(size.Width * height / size.Height)
					} else {
						style.Size.Width = PxLength(size.Width)
					}
				}
				if style.Size.Height.Auto {
					if width, ok := absoluteLengthPixels(style.Size.Width, rem); ok && size.Width != 0 {
						style.Size.Height = PxLength(size.Height * width / size.Width)
					} else {
						style.Size.Height = PxLength(size.Height)
					}
				}
				if NeedsAnimationFrame(data.image, global != nil, policy) {
					RequestAnimationFrame(w)
				}
			case done:
				if img.fallback != nil {
					element := img.fallback()
					replacementIDs = append(replacementIDs, element.RequestLayout(w, app))
					layout.replacement = &element
				}
				if state != nil {
					state.Resolved()
				}
			default:
				if state != nil {
					if _, started := state.StartedLoadingAt(); started {
						if state.ShowLoadingReplacement(app.Now()) && img.loading != nil {
							element := img.loading()
							replacementIDs = append(replacementIDs, element.RequestLayout(w, app))
							layout.replacement = &element
						}
					} else {
						view, _ := currentViewOf(w)
						state.StartLoadingDelay(w, view, app)
					}
				}
			}
			img.lastStyle = style
			requested, err := requestLayoutOf(w, style, replacementIDs...)
			if err != nil {
				panic(fmt.Sprintf("gpui: Img request_layout: %v", err))
			}
			id = requested
		})
		layout.frameIndex = frameIndex
		return id, state
	})
	return layoutID, layout
}

// Prepaint implements Element (R:img.rs:435-459): the replacement
// prepaints (the port's img registers no hitbox; see the file comment).
func (d *imgDrawable) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *imgLayoutState, w *Window, app *App) struct{} {
	if layout.replacement != nil {
		style := d.img.computeStyle()
		WithTextStyleVoid(w, style.TextStyle(), func(w *Window) {
			WithContentMaskVoid(w, style.OverflowMask(bounds, RemSize(w)), func(w *Window) {
				layout.replacement.Prepaint(w, app)
			})
		})
	}
	return struct{}{}
}

// Paint implements Element (R:img.rs:461-511 inside the
// Interactivity::paint scopes): a hidden element paints nothing; the
// element opacity scopes the style box around the text style and overflow
// mask, inside which the loaded image paints through the object fit
// with the style's corner radii and smoothing — or the replacement
// paints. The object fit runs in the element's content box (border box
// inset by the border widths and the padding), as CSS fits replaced
// content (Chromium's LayoutReplaced::ReplacedContentRect from
// PhysicalContentBoxRect, layout_replaced.cc:474-477), and the image is
// clipped to that content box: css-images-3 §the-object-fit — "replaced
// elements always clip their contents to the content box" (Chromium
// crops the painted source rect to it, image_painter.cc:224-240).
// Recorded divergence toward CSS: the pin fits and clips in the border
// box (img.rs:492-493 passes the element bounds to both roles).
func (d *imgDrawable) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *imgLayoutState, prepaint *struct{}, w *Window, app *App) {
	img := d.img
	style := img.computeStyle()
	if style.Visibility == VisibilityHidden {
		return
	}
	WithElementOpacityVoid(w, style.Opacity, func(w *Window) {
		style.paint(bounds, w, app, func(w *Window) {
			WithTextStyleVoid(w, style.TextStyle(), func(w *Window) {
				WithContentMaskVoid(w, style.OverflowMask(bounds, RemSize(w)), func(w *Window) {
					result, done := img.source.UseData(img.imageCache, w, app)
					if done && result.Err == nil && result.Image != nil {
						data := result.Image
						if data.FrameCount() == 0 {
							return
						}
						fw, fh := data.Size(layout.frameIndex)
						// The content box, not the border box (see the method
						// comment): the object fit's base rect and the image's
						// clip region.
						contentBox := style.ContentBox(bounds, RemSize(w))
						newBounds := img.objectFit.GetBounds(contentBox, int32(fw), int32(fh))
						radii := style.CornerRadii.ToPixels(RemSize(w))
						var smoothing float32
						if style.CornerSmoothing != nil {
							smoothing = *style.CornerSmoothing
						}
						if err := w.PaintImageWithCornerSmoothing(contentBox, newBounds, radii, smoothing, data, layout.frameIndex, img.grayscale); err != nil {
							log.Printf("gpui: %v", err)
						}
					} else if layout.replacement != nil {
						layout.replacement.Paint(w, app)
					}
				})
			})
		})
	})
}

// LayoutNodeStyle implements the root-stretch report.
func (d *imgDrawable) LayoutNodeStyle() (Style, bool) { return d.img.lastStyle, true }
