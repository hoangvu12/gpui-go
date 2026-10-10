package gpui

// This file is ticket18's image cache and image asset loading: the
// pinned crates/gpui/src/elements/image_cache.rs (the three-state
// ImageCacheItem with the shared-task dedup, RetainAllImageCache with
// clear/remove and the release-driven drop, AnyImageCache) and the
// loader half of crates/gpui/src/elements/img.rs (ImageAssetLoader:
// Path = filesystem read, Uri = GET(url, follow_redirects = true) with
// the non-2xx first-line BadStatus error, Embedded = registry lookup
// or the Asset error; the ImageCacheError variants; LOADING_DELAY and
// the rational frame-delay advance with the reduce-motion and
// active-window policies; the 100ms out-of-range delay fallback).
//
// Like assets.go it stays in package gpui: the cache is entity state
// driven through App/Window/Scope, and a subpackage would import-cycle
// with the App wiring.
//
// Deferred pieces (owned by later tickets): the img element itself and
// its interactivity/object-fit/sprite painting (the scene/sprite
// tickets), the window image_cache_stack and ImageCacheElement
// (window.rs/element machinery), the ImageDecoder asset behind
// ImageSource::Image — the pinned to_image_data path that routes WebP
// through static decode, deliberately separate from this resource
// loader's animated path (the distribution contract's mode distinction)
// — the SVG renderer behind the format fallback (ticket19), and wiring
// is_window_active to real platform activation (the window tickets —
// the playback policy carries the flag as a parameter).

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"
)

// ---------------------------------------------------------------------------
// Errors (img.rs ImageCacheError)
// ---------------------------------------------------------------------------

// ImageCacheErrorKind selects the pinned ImageCacheError variant
// (img.rs lines 702-725).
type ImageCacheErrorKind uint8

const (
	// ImageCacheErrorOther is ImageCacheError::Other: some other error
	// (the pinned Arc<anyhow::Error>).
	ImageCacheErrorOther ImageCacheErrorKind = iota + 1
	// ImageCacheErrorIO is ImageCacheError::Io: an error reading the
	// image from disk.
	ImageCacheErrorIO
	// ImageCacheErrorBadStatus is ImageCacheError::BadStatus: a
	// non-success HTTP status for a URI resource.
	ImageCacheErrorBadStatus
	// ImageCacheErrorAsset is ImageCacheError::Asset: an error while
	// processing an embedded asset (the pinned SharedString).
	ImageCacheErrorAsset
	// ImageCacheErrorImage is ImageCacheError::Image: an error while
	// processing an image (the pinned Arc<image::ImageError>).
	ImageCacheErrorImage
	// ImageCacheErrorSVG is ImageCacheError::Usvg: an error processing
	// an SVG. The pin's slot carries usvg::Error; the port's SVG
	// renderer is a deferred service (ticket19), so un-sniffed bytes
	// report the explicit unsupported error here.
	ImageCacheErrorSVG
)

// ImageCacheError is the error surface of the image cache (the pinned
// img.rs enum, Clone via value copy).
type ImageCacheError struct {
	// Kind selects the variant.
	Kind ImageCacheErrorKind
	// URI, Status and Body carry the BadStatus variant (the pinned
	// uri/status/body fields; the body is the first response line).
	URI    string
	Status int
	Body   string
	// Err carries the underlying error of the Other/IO/Image/SVG
	// variants.
	Err error
	// Message carries the Asset variant's string and the Other
	// variant's message when no error is wrapped.
	Message string
}

// Error renders the pinned #[error] formats.
func (e *ImageCacheError) Error() string {
	if e == nil {
		return "gpui: nil image cache error"
	}
	switch e.Kind {
	case ImageCacheErrorOther:
		if e.Err != nil {
			return fmt.Sprintf("error: %v", e.Err)
		}
		return fmt.Sprintf("error: %s", e.Message)
	case ImageCacheErrorIO:
		return fmt.Sprintf("IO error: %v", e.Err)
	case ImageCacheErrorBadStatus:
		return fmt.Sprintf("unexpected http status for %q: %s, body: %s",
			e.URI, httpStatusString(e.Status), e.Body)
	case ImageCacheErrorAsset:
		return fmt.Sprintf("asset error: %s", e.Message)
	case ImageCacheErrorImage:
		return fmt.Sprintf("image error: %v", e.Err)
	case ImageCacheErrorSVG:
		return fmt.Sprintf("svg error: %v", e.Err)
	default:
		return fmt.Sprintf("gpui: unknown image cache error kind %d", uint8(e.Kind))
	}
}

// httpStatusString renders a status code like the http crate's
// StatusCode Display ("404 Not Found").
func httpStatusString(status int) string {
	if text := http.StatusText(status); text != "" {
		return strconv.Itoa(status) + " " + text
	}
	return strconv.Itoa(status)
}

// ErrSVGRendererDeferred reports bytes the resource entry could not
// recognize — the pinned path routes them to the SVG renderer
// (img.rs's `svg_renderer.render_single_frame`), which is a deferred
// service in this port, so image loads fail explicitly instead.
var ErrSVGRendererDeferred = errors.New("gpui: SVG rendering is a deferred service")

// ---------------------------------------------------------------------------
// Loaded images (assets.rs ImageId/RenderImage identity)
// ---------------------------------------------------------------------------

// nextImageID assigns unique image identities (the pinned ImageId
// NEXT_ID AtomicUsize, assets.rs lines 180-186; the port starts at 1 so
// the zero value is never a live identity).
var nextImageID atomic.Uint64

// LoadedImage is the cache's loaded image: the decoded frames (ticket17's
// RenderImage — BGRA frames with rational per-frame delays) plus the
// pinned ImageId identity that keys atlas retention. The pinned
// RenderImage carries the id itself; ticket17's frozen frame model does
// not, so the cache wraps it.
type LoadedImage struct {
	id    uint64
	image *RenderImage
}

// ID returns the image's unique identity (the pinned RenderImage.id).
func (i *LoadedImage) ID() uint64 { return i.id }

// RenderImage returns the decoded frame model.
func (i *LoadedImage) RenderImage() *RenderImage { return i.image }

// FrameCount returns the number of frames.
func (i *LoadedImage) FrameCount() int {
	if i == nil || i.image == nil {
		return 0
	}
	return i.image.FrameCount()
}

// Size returns frame ix's pixel size.
func (i *LoadedImage) Size(ix int) (int, int) {
	if i == nil || i.image == nil {
		return 0, 0
	}
	return i.image.Size(ix)
}

// Delay returns frame ix's delay from the previous frame, with the
// pinned 100ms fallback for an out-of-range index
// (RenderImage::delay, assets.rs lines 232-238).
func (i *LoadedImage) Delay(ix int) time.Duration {
	if i == nil || i.image == nil {
		return 100 * time.Millisecond
	}
	return i.image.Delay(ix)
}

// ImageAssetResult is the pinned Result<Arc<RenderImage>,
// ImageCacheError>: exactly one of Image/Err is set on a resolved load.
type ImageAssetResult struct {
	// Image is the loaded image (nil when Err is set).
	Image *LoadedImage
	// Err is the load error (nil on success).
	Err *ImageCacheError
}

// ---------------------------------------------------------------------------
// The image asset loader (img.rs ImageAssetLoader)
// ---------------------------------------------------------------------------

// ImageAssetLoader is the Go port of the pinned type-level tag
// (img.rs lines 611-613: `pub enum ImageAssetLoader {}` — an empty enum
// existing only to key the app's loading-asset cache). The load body
// lives in imageLoadCapture.load.
type ImageAssetLoader struct{}

// FetchImageResource returns the shared image-load task for the
// resource (App::fetch_asset instantiated with the pinned
// ImgResourceLoader = AssetLogger<ImageAssetLoader>): one load at a
// time per resource, the result cached until RemoveImageAsset. The
// second return reports whether this call started the load.
func FetchImageResource(cx *App, source Resource) (Task[ImageAssetResult], bool) {
	capture := newImageLoadCapture(cx)
	return FetchAsset[ImageAssetLoader](cx, source, func(run *TaskRun) ImageAssetResult {
		return capture.loadLogged(run, source)
	})
}

// RemoveImageAsset removes the resource's image asset from GPUI's cache
// (ImageSource::remove_asset → App::remove_asset::<ImgResourceLoader>,
// img.rs lines 576-586): a later fetch starts a fresh load.
func RemoveImageAsset(cx *App, source Resource) {
	RemoveAsset[ImageAssetLoader](cx, source)
}

// HasImageAsset reports whether the resource's image asset is present
// in GPUI's cache (loading or loaded) without fetching it (the pinned
// test-support ImageSource::is_asset_cached).
func HasImageAsset(cx *App, source Resource) bool {
	return HasAsset[ImageAssetLoader](cx, source)
}

// imageLoadCapture is the foreground snapshot the load captures (the
// pin's client/asset_registry clones moved into the
// ImageAssetLoader::load future, img.rs lines 614-617). The worker
// never touches App state.
type imageLoadCapture struct {
	client   HttpClient
	registry *AssetRegistry
	codec    *ImageCodec
	codecErr error
}

// newImageLoadCapture snapshots the app's HTTP client, asset registry
// and image codec on the foreground (the load runs on a worker).
func newImageLoadCapture(cx *App) imageLoadCapture {
	codec, err := cx.appImageCodec()
	return imageLoadCapture{
		client:   cx.HTTPClient(),
		registry: cx.Assets(),
		codec:    codec,
		codecErr: err,
	}
}

// loadLogged applies the pinned AssetLogger wrapping (asset_cache.rs
// lines 40-63): the error variant is logged during loading.
func (c imageLoadCapture) loadLogged(run *TaskRun, source Resource) ImageAssetResult {
	result := c.load(source)
	if result.Err != nil {
		log.Printf("gpui: Failed to load asset: %v", result.Err)
	}
	return result
}

// load is the pinned ImageAssetLoader::load body (img.rs lines
// 618-700): resolve the bytes by resource kind, decode through the
// resource entry (the sniffed path with the SVG-fallback signal), and
// preserve the pinned error contracts.
func (c imageLoadCapture) load(source Resource) ImageAssetResult {
	var bytes []byte
	switch source.Kind {
	case ResourcePath:
		// Resource::Path → fs::read, IO errors kept (img.rs lines
		// 625-626).
		data, err := os.ReadFile(source.Value)
		if err != nil {
			return imageErrorResult(ImageCacheErrorIO, err, "")
		}
		bytes = data

	case ResourceURI:
		// Resource::Uri → client.get(url, true) — redirects ARE
		// followed (img.rs lines 627-648), the transport error is
		// wrapped with the "loading image asset from {uri}" context,
		// and a non-2xx status becomes BadStatus keeping only the
		// first body line.
		uri := source.Value
		response, err := c.client.Get(uri, true)
		if err != nil {
			return imageErrorResult(ImageCacheErrorOther,
				fmt.Errorf("loading image asset from %q: %w", uri, err), "")
		}
		if !httpStatusSuccess(response.Status) {
			return ImageAssetResult{Err: &ImageCacheError{
				Kind:   ImageCacheErrorBadStatus,
				URI:    uri,
				Status: response.Status,
				Body:   firstErrorBodyLine(response.Body),
			}}
		}
		bytes = response.Body

	case ResourceEmbedded:
		// Resource::Embedded → registry lookup; a miss is the Asset
		// error "Embedded resource not found: {path}" (img.rs lines
		// 649-658).
		path := source.Value
		data, found := c.registry.Load(path)
		if !found {
			return imageErrorResult(ImageCacheErrorAsset, nil,
				fmt.Sprintf("Embedded resource not found: %s", path))
		}
		bytes = data

	default:
		return imageErrorResult(ImageCacheErrorOther,
			fmt.Errorf("gpui: unknown resource kind %d", uint8(source.Kind)), "")
	}

	if c.codecErr != nil {
		return imageErrorResult(ImageCacheErrorOther, c.codecErr, "")
	}
	// The pinned decode branches by sniffed format (GIF and animated
	// WebP frame walks, static decode otherwise); ticket17's frozen
	// ImageCodec owns all of that behind DecodeResource, including the
	// un-sniffed signal (ErrImageFormatUnknown), which the pin routes
	// to the SVG renderer — a deferred service here.
	render, err := c.codec.DecodeResource(bytes)
	if err != nil {
		if errors.Is(err, ErrImageFormatUnknown) {
			return imageErrorResult(ImageCacheErrorSVG,
				fmt.Errorf("%w", ErrSVGRendererDeferred), "")
		}
		return imageErrorResult(ImageCacheErrorImage, err, "")
	}
	return ImageAssetResult{Image: newLoadedImage(render)}
}

// imageErrorResult builds an error result for the variant.
func imageErrorResult(kind ImageCacheErrorKind, err error, message string) ImageAssetResult {
	return ImageAssetResult{Err: &ImageCacheError{Kind: kind, Err: err, Message: message}}
}

// httpStatusSuccess reports the 2xx range (the pinned
// http::StatusCode::is_success).
func httpStatusSuccess(status int) bool { return status >= 200 && status < 300 }

// firstErrorBodyLine keeps only the first line of the response body,
// trimmed of trailing whitespace (the pinned BadStatus body handling,
// img.rs lines 634-645: from_utf8_lossy, lines().next().unwrap_or(""),
// trim_end, truncate).
func firstErrorBodyLine(body []byte) string {
	text := utf8Lossy(body)
	if lineEnd := strings.IndexByte(text, '\n'); lineEnd >= 0 {
		text = text[:lineEnd]
	}
	return strings.TrimRightFunc(text, unicode.IsSpace)
}

// utf8Lossy decodes bytes like the pin's String::from_utf8_lossy: every
// valid subsequence passes through and every maximal subpart of an
// ill-formed subsequence becomes exactly one U+FFFD (Unicode §3.9
// Table 3-7, the WHATWG decoder's policy). strings.ToValidUTF8 is not
// used: it collapses a run of discrete invalid bytes into a single
// replacement, which would under-count the replacements for inputs
// like 0xff 0xfe (two maximal subparts, two U+FFFD).
func utf8Lossy(data []byte) string {
	var out strings.Builder
	out.Grow(len(data))
	i := 0
	for i < len(data) {
		b := data[i]
		if b < 0x80 {
			out.WriteByte(b)
			i++
			continue
		}
		// The lead byte decides the sequence length and the second byte's
		// valid range (Table 3-7; C0, C1 and F5..FF never lead a
		// well-formed sequence).
		var size int
		var min, max byte
		switch {
		case b >= 0xC2 && b <= 0xDF:
			size, min, max = 2, 0x80, 0xBF
		case b == 0xE0:
			size, min, max = 3, 0xA0, 0xBF
		case b >= 0xE1 && b <= 0xEC, b >= 0xEE && b <= 0xEF:
			size, min, max = 3, 0x80, 0xBF
		case b == 0xED:
			size, min, max = 3, 0x80, 0x9F
		case b == 0xF0:
			size, min, max = 4, 0x90, 0xBF
		case b >= 0xF1 && b <= 0xF3:
			size, min, max = 4, 0x80, 0xBF
		case b == 0xF4:
			size, min, max = 4, 0x80, 0x8F
		default:
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		if i+1 >= len(data) || data[i+1] < min || data[i+1] > max {
			// The maximal subpart is the lone lead byte.
			out.WriteRune(utf8.RuneError)
			i++
			continue
		}
		// Consume the maximal prefix of continuation bytes.
		end := i + 2
		for end < i+size && end < len(data) && data[end]&0xC0 == 0x80 {
			end++
		}
		if end == i+size {
			out.Write(data[i : i+size])
			i += size
		} else {
			// Truncated sequence: the maximal subpart becomes one
			// replacement.
			out.WriteRune(utf8.RuneError)
			i = end
		}
	}
	return out.String()
}

// newLoadedImage wraps one decoded frame model with a fresh identity.
func newLoadedImage(image *RenderImage) *LoadedImage {
	return &LoadedImage{id: nextImageID.Add(1), image: image}
}

// ---------------------------------------------------------------------------
// The application image codec and atlas retention
// ---------------------------------------------------------------------------

// appImageCodec lazily builds the application's image codec — the
// decode + atlas pair the image cache retains frames in (the pin pairs
// the platform image system with the renderer's atlas; the port's
// ImageCodec owns its polychrome atlas). Foreground only; the first
// failure sticks and every later load reports it.
func (a *App) appImageCodec() (*ImageCodec, error) {
	if a.imageCodec != nil {
		return a.imageCodec, nil
	}
	if a.imageCodecErr != nil {
		return nil, a.imageCodecErr
	}
	codec, err := NewImageCodec()
	if err != nil {
		err = fmt.Errorf("gpui: the application image codec: %w", err)
		a.imageCodecErr = err
		return nil, err
	}
	a.imageCodec = codec
	return codec, nil
}

// ImageAtlas returns the application's image atlas (the decode+atlas
// pair the image cache retains decoded frames in). It is the reach the
// pinned test-support has into the window sprite atlas, adapted to the
// port's single application atlas; nil when the codec is unavailable.
func (a *App) ImageAtlas() *Atlas {
	codec, err := a.appImageCodec()
	if err != nil {
		return nil
	}
	return codec.Atlas()
}

// retainImage inserts every frame of the loaded image into the
// polychrome atlas under the pinned AtlasKey::Image { image_id,
// frame_index } identity (the pin's paint-time sprite insertion; the
// port's cache-level retention makes that ownership observable before
// the img element's painting lands). A cached key returns the existing
// tile, so re-retention is idempotent.
func (a *App) retainImage(image *LoadedImage) error {
	codec, err := a.appImageCodec()
	if err != nil {
		return err
	}
	for i := range image.image.Frames {
		frame := &image.image.Frames[i]
		if _, err := codec.Atlas().InsertImageFrame(image.id, i, frame); err != nil {
			return err
		}
	}
	return nil
}

// dropImage removes the image's atlas tiles (App::drop_image /
// Window::drop_image, app.rs lines 2815-2825 and window.rs lines
// 5202-5214: every frame index under the image's identity). The pin
// removes from every window's sprite atlas plus the current window;
// the port has one application atlas, so the walk collapses to a
// single removal per frame.
func (a *App) dropImage(image *LoadedImage) error {
	codec, err := a.appImageCodec()
	if err != nil {
		return err
	}
	for i := range image.image.Frames {
		if err := codec.Atlas().RemoveImageFrame(image.id, i); err != nil {
			return err
		}
	}
	return nil
}

// HasImageAtlasEntry reports whether every frame of the image is
// present in the atlas (the pinned Window::has_image_atlas_entry test
// support, window.rs lines 5216-5229).
func (a *Atlas) HasImageAtlasEntry(image *LoadedImage) bool {
	if a == nil || image == nil || image.image == nil {
		return false
	}
	count := len(image.image.Frames)
	if count == 0 {
		return false
	}
	for i := 0; i < count; i++ {
		if _, err := a.svc.Query(a.handle, imageAtlasKey(image.id, uint32(i))); err != nil {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// The image cache (image_cache.rs)
// ---------------------------------------------------------------------------

// imageCacheItem is the pinned ImageCacheItem (image_cache.rs lines
// 145-172): Loading (the shared load task) or Loaded (the resolved
// result). The transition happens lazily on get, exactly like the
// pinned now_or_never.
type imageCacheItem struct {
	// task is the shared load task while the item is Loading.
	task Task[ImageAssetResult]
	// result is the resolved result once Loaded.
	result *ImageAssetResult
	// dropped records that the loaded image's atlas frames were
	// removed: the loading/close races below must drop exactly once.
	dropped bool
}

// get resolves the item's state (ImageCacheItem::get, image_cache.rs
// lines 174-186): while Loading, a finished task transitions the item
// to Loaded — retaining the decoded frames in the atlas — and reports
// (result, true); an unfinished task reports (zero, false), the pinned
// None.
func (item *imageCacheItem) get(cx *App) (ImageAssetResult, bool) {
	if item.task.t != nil {
		if !item.task.Done() {
			return ImageAssetResult{}, false
		}
		result := item.task.resultValue()
		if result.Image == nil && result.Err == nil {
			// A cancelled load — the pin's shared task is never cancelled
			// because the cache item holds a reference; the Go task
			// registry keeps workers running, so this only happens when a
			// caller cancelled the fetch task explicitly. Surface it as an
			// error instead of a silent third state.
			result.Err = &ImageCacheError{Kind: ImageCacheErrorOther, Message: "image load cancelled"}
		}
		item.task = Task[ImageAssetResult]{}
		item.result = &result
		if result.Image != nil {
			if err := cx.retainImage(result.Image); err != nil {
				log.Printf("gpui: retaining image frames: %v", err)
			}
		}
	}
	if item.result != nil {
		return *item.result, true
	}
	return ImageAssetResult{}, false
}

// drop releases the item's loaded image from the atlas exactly once
// (the pinned clear/remove/observe_release bodies: get() first — a
// completed-but-unread task still resolves — then drop_image when the
// result is an image).
func (item *imageCacheItem) drop(cx *App) {
	if item.dropped {
		return
	}
	_, _ = item.get(cx)
	item.dropped = true
	if item.result != nil && item.result.Image != nil {
		if err := cx.dropImage(item.result.Image); err != nil {
			log.Printf("gpui: dropping image frames: %v", err)
		}
	}
}

// RetainAllImageCache is the pinned RetainAllImageCache (image_cache.rs
// lines 249-330): a hash-keyed map of image cache items with the
// shared-task dedup, clear/remove invalidation and the release-driven
// drop. Create it as an entity with NewRetainAllImageCache and use it
// through AnyImageCacheOf.
type RetainAllImageCache struct {
	items map[uint64]*imageCacheItem
}

// Len returns the number of images in the cache (the pinned len).
func (c *RetainAllImageCache) Len() int { return len(c.items) }

// IsEmpty reports whether the cache is empty (the pinned is_empty).
func (c *RetainAllImageCache) IsEmpty() bool { return len(c.items) == 0 }

// NewRetainAllImageCache creates the cache entity (the pinned
// RetainAllImageCache::new, image_cache.rs lines 256-266): the entity
// plus the release observer that drops every loaded image when the
// cache releases — the release-driven drop, with no current window.
func NewRetainAllImageCache(cx *App, owner *Scope) Entity[RetainAllImageCache] {
	return NewEntity(cx, owner, func(state *RetainAllImageCache, ccx *Context[RetainAllImageCache]) {
		state.items = make(map[uint64]*imageCacheItem)
		ccx.OnRelease(func(cache *RetainAllImageCache, a *App) {
			cache.dropAll(a)
		})
	})
}

// load loads the image for the resource (the pinned
// RetainAllImageCache::load, image_cache.rs lines 268-297): a cache hit
// resolves through the item's three states; a miss spawns the shared
// load task, records the Loading item and registers the completion
// notification that redraws the requesting view. Reports (zero, false)
// while loading — exactly the pinned None. Runs inside the cache
// entity's update (the pinned AnyImageCache downcast update); use
// AnyImageCacheOf's handle.
func (c *RetainAllImageCache) load(resource Resource, w *Window, cx *App, self Entity[RetainAllImageCache]) (ImageAssetResult, bool) {
	if w == nil {
		panic("gpui: image cache load requires a window")
	}
	hash := resource.AssetHash()
	if item, ok := c.items[hash]; ok {
		return item.get(cx)
	}
	capture := newImageLoadCapture(cx)
	task := cx.background.Spawn(func(run *TaskRun) ImageAssetResult {
		return capture.loadLogged(run, resource)
	})
	c.items[hash] = &imageCacheItem{task: task}
	view, _ := currentViewOf(w)
	cx.SpawnDeliveringInto(w.Scope(), self, func(run *TaskRun) ImageAssetResult {
		return run.Await(task)
	}, func(_ *RetainAllImageCache, _ ImageAssetResult, lcx *Context[RetainAllImageCache]) {
		notifyViewLoaded(w, view, lcx.App())
	})
	return ImageAssetResult{}, false
}

// Clear clears the image cache (the pinned clear, image_cache.rs lines
// 299-305): every item's loaded image is dropped from the atlas. Items
// still loading are discarded — their late results never retain.
func (c *RetainAllImageCache) Clear(cx *App) {
	items := c.items
	c.items = make(map[uint64]*imageCacheItem)
	dropItemsInHashOrder(items, cx)
}

// Remove removes the image for the given source from the cache (the
// pinned remove, image_cache.rs lines 307-315): a later load re-fetches.
func (c *RetainAllImageCache) Remove(resource Resource, cx *App) {
	hash := resource.AssetHash()
	if item, ok := c.items[hash]; ok {
		delete(c.items, hash)
		item.drop(cx)
	}
}

// dropAll drops every item (the pinned observe_release body,
// image_cache.rs lines 259-264) in deterministic hash order — the
// runtime ownership contract's ordered-cleanup rule over the pinned
// unordered HashMap walk.
func (c *RetainAllImageCache) dropAll(cx *App) {
	items := c.items
	c.items = nil
	dropItemsInHashOrder(items, cx)
}

// dropItemsInHashOrder drops the items sorted by their hash keys.
func dropItemsInHashOrder(items map[uint64]*imageCacheItem, cx *App) {
	if len(items) == 0 {
		return
	}
	keys := make([]uint64, 0, len(items))
	for hash := range items {
		keys = append(keys, hash)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, hash := range keys {
		items[hash].drop(cx)
	}
}

// AnyImageCache is a dynamically typed image cache which can be used to
// store any image cache (the pinned AnyImageCache, image_cache.rs lines
// 55-88). The pinned struct stores an AnyEntity plus a downcast load
// function; Go erases the type natively through the interface, and the
// retain-all entity adapts to it through AnyImageCacheOf — the pinned
// From<Entity<I>> for AnyImageCache.
type AnyImageCache interface {
	// LoadImage loads the image for the resource (the pinned
	// AnyImageCache::load / ImageCache::load): the finished result, or
	// loading. Implementations ensure images are removed from the
	// renderer when no longer needed (the pinned trait obligation).
	LoadImage(resource Resource, w *Window, cx *App) (ImageAssetResult, bool)
}

// imageLoadOutcome carries the two return values through the entity
// update.
type imageLoadOutcome struct {
	result ImageAssetResult
	done   bool
}

// retainAllCacheHandle adapts Entity[RetainAllImageCache] into
// AnyImageCache (the pinned any_image_cache::load's entity update).
type retainAllCacheHandle struct {
	entity Entity[RetainAllImageCache]
}

// AnyImageCacheOf converts the retain-all cache entity into an
// AnyImageCache (the pinned From<Entity<I>> for AnyImageCache): the
// load runs inside the cache entity's exclusive update.
func AnyImageCacheOf(cache Entity[RetainAllImageCache]) AnyImageCache {
	return retainAllCacheHandle{entity: cache}
}

// LoadImage implements AnyImageCache.
func (h retainAllCacheHandle) LoadImage(resource Resource, w *Window, cx *App) (ImageAssetResult, bool) {
	outcome := h.entity.UpdateWith(cx, func(state *RetainAllImageCache, lcx *Context[RetainAllImageCache]) imageLoadOutcome {
		result, done := state.load(resource, w, lcx.App(), h.entity)
		return imageLoadOutcome{result: result, done: done}
	})
	return outcome.result, outcome.done
}

// ---------------------------------------------------------------------------
// Window asset surfaces (window.rs use_asset/get_asset)
// ---------------------------------------------------------------------------

// UseImageAsset asynchronously loads the image asset for the resource
// (the pinned Window::use_asset with the img resource loader,
// window.rs lines 4013-4033). It returns the finished result, or
// reports loading and — when this call started the load — registers
// the completion notification that redraws the current view on the
// next frame. Multiple calls only result in one load at a time and the
// results are cached.
func (w *Window) UseImageAsset(resource Resource, cx *App) (ImageAssetResult, bool) {
	task, isFirst := FetchImageResource(cx, resource)
	if task.Done() {
		return task.resultValue(), true
	}
	if isFirst {
		view, _ := currentViewOf(w)
		cx.SpawnDelivering(w.Scope(), func(run *TaskRun) ImageAssetResult {
			return run.Await(task)
		}, func(_ ImageAssetResult, acx *App) {
			notifyViewLoaded(w, view, acx)
		})
	}
	return ImageAssetResult{}, false
}

// GetImageAsset asynchronously loads the image asset without the
// redraw registration (the pinned Window::get_asset, window.rs lines
// 4035-4043): the view will not be re-drawn once the asset finishes.
func (w *Window) GetImageAsset(resource Resource, cx *App) (ImageAssetResult, bool) {
	task, _ := FetchImageResource(cx, resource)
	if task.Done() {
		return task.resultValue(), true
	}
	return ImageAssetResult{}, false
}

// currentViewOf resolves the currently rendering view (window.rs
// current_view, lines 5497-5500). The pinned method unwraps the
// rendered-entity stack; the port reports no view outside a draw and
// the completion notification degrades to the window refresh.
func currentViewOf(w *Window) (EntityID, bool) {
	if w == nil {
		return 0, false
	}
	if ds, ok := windowDrawStates[w]; ok && ds.frame != nil {
		return CurrentView(w)
	}
	return 0, false
}

// notifyViewLoaded delivers the redraw nudge (the pinned
// cx.on_next_frame(move |_, cx| cx.notify(entity))): the requesting
// view is notified and the window's refresh seam is marked. The port's
// redraw path is the explicit DrawWindowFrame, so the pinned frame-tick
// deferral becomes the refresh request.
func notifyViewLoaded(w *Window, view EntityID, cx *App) {
	if view != 0 {
		cx.notify(view)
	}
	if w != nil {
		w.requestRefresh()
	}
}

// ---------------------------------------------------------------------------
// Animation scheduling (img.rs ImgState / LOADING_DELAY)
// ---------------------------------------------------------------------------

// LoadingDelay is the delay before showing the loading state (the
// pinned LOADING_DELAY, img.rs line 46: 200ms).
const LoadingDelay = 200 * time.Millisecond

// ImagePlaybackPolicy carries the animation policies the pinned img
// element consults during request_layout (img.rs lines 345-371 and
// 411-416): reduce motion (App::reduce_motion, app.rs lines
// 1119-1128) and the active window (Window::is_window_active).
// Wiring is_window_active to real platform activation belongs to the
// window tickets; until then callers pass the window's status.
type ImagePlaybackPolicy struct {
	// ReduceMotion selects the static rendering of non-essential
	// animations.
	ReduceMotion bool
	// WindowActive reports whether the owning window is active.
	WindowActive bool
}

// ImagePlayback is the pinned ImgState (img.rs lines 254-258): the
// element's animation state between frames — the displayed frame
// index, the last frame time and the started-loading tracker. Retain
// it as element state (WithElementState) and drive it from
// request_layout.
type ImagePlayback struct {
	// FrameIndex is the currently displayed frame (the pinned
	// state.frame_index).
	FrameIndex int
	// lastFrameTimeSet/lastFrameTime are the pinned Option<Instant>
	// last_frame_time.
	lastFrameTimeSet bool
	lastFrameTime    time.Duration
	// startedLoadingSet/startedLoading/startedLoadingTask are the
	// pinned Option<(Instant, Task<()>)> started_loading.
	startedLoadingSet  bool
	startedLoading     time.Duration
	startedLoadingTask Task[struct{}]
}

// Now returns the application's clock reading (the pinned
// Instant::now() the img element consults; the port reads the
// scheduler's virtual clock, so animation timing is deterministic
// under TestApp.RunUntilParked/AdvanceClock).
func (a *App) Now() time.Duration {
	return time.Duration(a.sched.nowMs()) * time.Millisecond
}

// Advance advances the playback over the image's frames (the pinned
// request_layout advance, img.rs lines 343-371): the frame index is
// clamped to the image's frames, then — for a multi-frame image while
// motion is allowed and the window is active — advanced by exactly one
// frame whenever the elapsed time covers the current frame's rational
// delay, with the pinned backdated last_frame_time (current_time -
// (elapsed - frame_duration)) so a long gap catches up on subsequent
// renders. Inactive windows and reduce-motion freeze the index and
// clear the frame time, matching the pin. Returns the frame index to
// display.
func (s *ImagePlayback) Advance(image *RenderImage, now time.Duration, policy ImagePlaybackPolicy) int {
	frameCount := image.FrameCount()
	maxFrameIndex := frameCount - 1
	if maxFrameIndex < 0 {
		maxFrameIndex = 0
	}
	if s.FrameIndex > maxFrameIndex {
		s.FrameIndex = maxFrameIndex
	}
	if frameCount > 1 && !policy.ReduceMotion {
		if policy.WindowActive {
			if s.lastFrameTimeSet {
				elapsed := now - s.lastFrameTime
				frameDuration := image.Delay(s.FrameIndex)
				if elapsed >= frameDuration {
					s.FrameIndex = (s.FrameIndex + 1) % frameCount
					s.lastFrameTime = now - (elapsed - frameDuration)
				}
			} else {
				s.lastFrameTime = now
				s.lastFrameTimeSet = true
			}
		} else {
			s.lastFrameTimeSet = false
		}
	} else {
		s.lastFrameTimeSet = false
	}
	return s.FrameIndex
}

// Resolved clears the started-loading tracker (the pinned
// state.started_loading = None on both the loaded and error branches,
// img.rs lines 372 and 390): the loading-delay notification task is
// cancelled — the pin drops the task handle.
func (s *ImagePlayback) Resolved() {
	s.startedLoadingSet = false
	if s.startedLoadingTask.t != nil {
		s.startedLoadingTask.Cancel()
		s.startedLoadingTask = Task[struct{}]{}
	}
}

// StartLoadingDelay starts the loading-delay notification (the pinned
// timer task, img.rs lines 392-405): a first render of a
// still-loading image starts a LOADING_DELAY timer that notifies the
// requesting view when it fires, so the loading state can appear. The
// timer is a ticket04 delivery task owned by the window's scope —
// closing the window cancels it — over the pinned window.spawn timer
// (the background executor timer maps to the scheduler's virtual
// timer; real-time firing in non-test apps arrives with the real-time
// timer integration).
func (s *ImagePlayback) StartLoadingDelay(w *Window, view EntityID, cx *App) {
	if s.startedLoadingSet {
		return
	}
	s.startedLoading = cx.Now()
	s.startedLoadingSet = true
	s.startedLoadingTask = cx.SpawnDelivering(w.Scope(), func(run *TaskRun) struct{} {
		run.Sleep(LoadingDelay)
		return struct{}{}
	}, func(_ struct{}, acx *App) {
		notifyViewLoaded(w, view, acx)
	})
}

// ShowLoadingReplacement reports whether the loading replacement should
// render (the pinned started_loading.elapsed() > LOADING_DELAY
// condition, img.rs lines 381-390 — strictly greater).
func (s *ImagePlayback) ShowLoadingReplacement(now time.Duration) bool {
	return s.startedLoadingSet && now-s.startedLoading > LoadingDelay
}

// StartedLoadingAt reports the instant the loading delay started and
// whether it is running (the pinned started_loading's Instant; a
// restart must not move it).
func (s *ImagePlayback) StartedLoadingAt() (time.Duration, bool) {
	return s.startedLoading, s.startedLoadingSet
}

// NeedsAnimationFrame reports whether an animated image should request
// another frame (the pinned request_animation_frame condition, img.rs
// lines 411-416: an identified element, more than one frame, the
// window active and motion allowed).
func NeedsAnimationFrame(image *RenderImage, hasElementID bool, policy ImagePlaybackPolicy) bool {
	return hasElementID && image.FrameCount() > 1 && policy.WindowActive && !policy.ReduceMotion
}

// RequestAnimationFrame marks the window for another frame (the pinned
// Window::request_animation_frame, window.rs lines 2633-2638, called by
// animated images): the port's redraw seam is the refresh request the
// explicit DrawWindowFrame path consumes.
func RequestAnimationFrame(w *Window) {
	if w != nil {
		w.requestRefresh()
	}
}
