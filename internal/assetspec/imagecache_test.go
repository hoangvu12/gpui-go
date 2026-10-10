package assetspec

import (
	"bytes"
	"image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"gpui-go/gpui"
)

// newCacheFixture builds an app with one embedded asset, a test window
// and a retain-all image cache owned by the root scope (so closing the
// window does not release the cache).
type cacheFixture struct {
	assetFixture
	cache  gpui.Entity[gpui.RetainAllImageCache]
	handle gpui.AnyImageCache
}

func newCacheFixture(t *testing.T, path string, data []byte) *cacheFixture {
	t.Helper()
	f := &cacheFixture{}
	f.assetFixture = *newAssetFixture(t, path, data)
	f.cache = gpui.NewRetainAllImageCache(f.app, f.ta.RootScope())
	f.handle = gpui.AnyImageCacheOf(f.cache)
	return f
}

// clearCache runs the pinned clear inside the cache entity's update.
func (f *cacheFixture) clearCache() {
	f.cache.Update(f.app, func(state *gpui.RetainAllImageCache, cx *gpui.Context[gpui.RetainAllImageCache]) {
		state.Clear(cx.App())
	})
}

// removeCache runs the pinned remove inside the cache entity's update.
func (f *cacheFixture) removeCache(resource gpui.Resource) {
	f.cache.Update(f.app, func(state *gpui.RetainAllImageCache, cx *gpui.Context[gpui.RetainAllImageCache]) {
		state.Remove(resource, cx.App())
	})
}

// ---------------------------------------------------------------------------
// The app-level fetch cache (app.rs fetch_asset / remove_asset)
// ---------------------------------------------------------------------------

// TestFetchImageResourceSharesOneLoad pins App::fetch_asset with the
// image resource loader (app.rs lines 2769-2789, window.rs get_asset):
// two fetches share one task (only the first starts the load), the
// result stays cached, and remove_asset restarts loads on demand.
func TestFetchImageResourceSharesOneLoad(t *testing.T) {
	f := newAssetFixture(t, "img.png", pngBytes(t, 4, 2))
	resource := gpui.EmbeddedResource("img.png")

	if gpui.HasImageAsset(f.app, resource) {
		t.Fatal("an un-fetched resource is cached")
	}
	first, started := gpui.FetchImageResource(f.app, resource)
	if !started {
		t.Fatal("the first fetch did not start the load")
	}
	second, startedAgain := gpui.FetchImageResource(f.app, resource)
	if startedAgain {
		t.Fatal("the second fetch started another load")
	}
	if !gpui.HasImageAsset(f.app, resource) {
		t.Fatal("the in-flight resource is not cached")
	}
	if first.Done() || second.Done() {
		t.Fatal("the loads finished before the scheduler ran")
	}

	f.ta.RunUntilParked()
	if *f.loads != 1 {
		t.Fatalf("asset loads = %d, want 1", *f.loads)
	}
	// Both fetches resolved through the same shared task.
	if !first.Done() || !second.Done() {
		t.Fatal("the shared load did not finish")
	}
	result, done := f.window.GetImageAsset(resource, f.app)
	if !done || result.Image == nil {
		t.Fatalf("GetImageAsset = (%+v, %v)", result, done)
	}
	if w, h := result.Image.Size(0); w != 4 || h != 2 {
		t.Fatalf("decoded size = %dx%d, want 4x2", w, h)
	}

	// The completed task stays cached: a later get resolves with no new
	// load.
	if _, done := f.window.GetImageAsset(resource, f.app); !done || *f.loads != 1 {
		t.Fatalf("cached get = %v after %d loads", done, *f.loads)
	}

	// remove_asset drops the cache entry; the next fetch re-loads.
	gpui.RemoveImageAsset(f.app, resource)
	if gpui.HasImageAsset(f.app, resource) {
		t.Fatal("remove_asset left the resource cached")
	}
	if _, started := gpui.FetchImageResource(f.app, resource); !started {
		t.Fatal("the post-remove fetch did not start a fresh load")
	}
	f.ta.RunUntilParked()
	if *f.loads != 2 {
		t.Fatalf("asset loads after re-fetch = %d, want 2", *f.loads)
	}
}

// TestFetchImageResourceDistinctSources pins the cache identity: the
// loader/source key separates resources (asset_cache.rs hash).
func TestFetchImageResourceDistinctSources(t *testing.T) {
	f := newAssetFixture(t, "img.png", pngBytes(t, 4, 2))
	embedded := gpui.EmbeddedResource("img.png")
	uri := gpui.URIResource("https://example.com/img.png")
	if _, started := gpui.FetchImageResource(f.app, embedded); !started {
		t.Fatal("the embedded fetch did not start")
	}
	if _, started := gpui.FetchImageResource(f.app, uri); !started {
		t.Fatal("the URI fetch shared the embedded load")
	}
	if !gpui.HasImageAsset(f.app, uri) {
		t.Fatal("the URI load is not separately cached")
	}
	f.ta.RunUntilParked()
	if *f.loads != 1 {
		t.Fatalf("asset loads = %d, want 1 (the URI load failed on the null client)", *f.loads)
	}
}

// ---------------------------------------------------------------------------
// Three-state loads and shared tasks (image_cache.rs RetainAllImageCache)
// ---------------------------------------------------------------------------

// TestImageCacheThreeStatesEmbedded pins the pinned
// RetainAllImageCache::load (image_cache.rs lines 268-297): the first
// load reports loading (None), the completion notification fires, and
// the next load resolves the decoded image — retained in the atlas
// under the pinned image identity.
func TestImageCacheThreeStatesEmbedded(t *testing.T) {
	f := newCacheFixture(t, "img.png", pngBytes(t, 4, 2))
	resource := gpui.EmbeddedResource("img.png")
	atlas := mustAtlas(t, f.app)

	result, done := f.handle.LoadImage(resource, f.window, f.app)
	if done {
		t.Fatalf("the first load finished immediately: %+v", result)
	}
	if result.Image != nil || result.Err != nil {
		t.Fatalf("the loading state carries a result: %+v", result)
	}
	if *f.loads != 0 {
		t.Fatalf("the load ran before the scheduler: %d", *f.loads)
	}
	if got := cacheLen(t, f); got != 1 {
		t.Fatalf("cache len after the miss = %d, want 1", got)
	}

	// The completion notification: the shared load runs, the awaiter
	// delivers and the window's refresh seam is marked.
	f.ta.RunUntilParked()
	if *f.loads != 1 {
		t.Fatalf("asset loads = %d, want 1", *f.loads)
	}
	if !f.window.RefreshRequested() {
		t.Fatal("the completion did not request a window refresh")
	}
	if tileCount(t, atlas) != 0 {
		t.Fatalf("atlas tiles before the resolving get = %d, want 0", tileCount(t, atlas))
	}

	// The resolving load: the image decodes through the real codec.
	result, done = f.handle.LoadImage(resource, f.window, f.app)
	if !done || result.Image == nil || result.Err != nil {
		t.Fatalf("the second load = (%+v, %v)", result, done)
	}
	if result.Image.FrameCount() != 1 {
		t.Fatalf("frames = %d, want 1", result.Image.FrameCount())
	}
	if w, h := result.Image.Size(0); w != 4 || h != 2 {
		t.Fatalf("size = %dx%d, want 4x2", w, h)
	}
	// The BGRA swap (the imagespec decode oracle): the first pixel of
	// the red half is [0 0 255 255].
	pixels := result.Image.RenderImage().Frames[0].Pixels
	if !bytes.Equal(pixels[0:4], []byte{0, 0, 255, 255}) {
		t.Fatalf("first pixel = %v, want BGRA [0 0 255 255]", pixels[0:4])
	}
	if d := result.Image.Delay(0); d != 0 {
		t.Fatalf("static frame delay = %v, want 0", d)
	}

	// Retention: the decoded frames are in the atlas under the image's
	// identity, exactly one tile per frame.
	if !atlas.HasImageAtlasEntry(result.Image) {
		t.Fatal("the loaded image is not retained in the atlas")
	}
	if got := tileCount(t, atlas); got != 1 {
		t.Fatalf("atlas tiles = %d, want 1", got)
	}
	if result.Image.ID() == 0 {
		t.Fatal("the loaded image has no identity")
	}

	// The third load is the cached Loaded state: the same image, no
	// new load, the same identity.
	again, done := f.handle.LoadImage(resource, f.window, f.app)
	if !done || again.Image != result.Image || again.Image.ID() != result.Image.ID() {
		t.Fatalf("the cached load = (%+v, %v), want the same image", again, done)
	}
	if *f.loads != 1 {
		t.Fatalf("asset loads = %d, want 1", *f.loads)
	}
	if got := tileCount(t, atlas); got != 1 {
		t.Fatalf("re-getting the image duplicated atlas tiles: %d", got)
	}
}

// cacheLen reads the cache length through an entity read.
func cacheLen(t *testing.T, f *cacheFixture) int {
	t.Helper()
	return f.cache.Read(f.app, func(state *gpui.RetainAllImageCache, _ *gpui.App) int {
		return state.Len()
	})
}

// TestImageCacheTwoConsumersOneLoad pins the shared-task dedup
// (image_cache.rs lines 278-281: one spawned task, every later hit
// resolves through the same item).
func TestImageCacheTwoConsumersOneLoad(t *testing.T) {
	f := newCacheFixture(t, "img.png", pngBytes(t, 2, 2))
	resource := gpui.EmbeddedResource("img.png")

	// Two consumers request the same resource before any completion.
	first, doneFirst := f.handle.LoadImage(resource, f.window, f.app)
	second, doneSecond := f.handle.LoadImage(resource, f.window, f.app)
	if doneFirst || doneSecond {
		t.Fatalf("both loads finished: (%+v, %v) (%+v, %v)", first, doneFirst, second, doneSecond)
	}
	if got := cacheLen(t, f); got != 1 {
		t.Fatalf("cache len = %d, want a single item", got)
	}

	f.ta.RunUntilParked()
	if *f.loads != 1 {
		t.Fatalf("asset loads = %d, want 1 (the shared task dedup)", *f.loads)
	}

	// Both consumers resolve the SAME loaded image (the pinned Arc
	// shared through the item).
	first, doneFirst = f.handle.LoadImage(resource, f.window, f.app)
	second, doneSecond = f.handle.LoadImage(resource, f.window, f.app)
	if !doneFirst || !doneSecond || first.Image == nil || second.Image == nil {
		t.Fatalf("consumers = (%+v, %v) (%+v, %v)", first, doneFirst, second, doneSecond)
	}
	if first.Image != second.Image || first.Image.ID() != second.Image.ID() {
		t.Fatalf("the two consumers got different images: %d vs %d", first.Image.ID(), second.Image.ID())
	}
	if *f.loads != 1 {
		t.Fatalf("asset loads after both resolves = %d, want 1", *f.loads)
	}
}

// TestImageCacheErrorState pins the error branch of the three states:
// an embedded miss resolves to the pinned Asset error (img.rs lines
// 649-658), which stays cached like a loaded image.
func TestImageCacheErrorState(t *testing.T) {
	f := newCacheFixture(t, "img.png", pngBytes(t, 2, 2))
	resource := gpui.EmbeddedResource("missing.png")

	if _, done := f.handle.LoadImage(resource, f.window, f.app); done {
		t.Fatal("the miss finished immediately")
	}
	f.ta.RunUntilParked()
	result, done := f.handle.LoadImage(resource, f.window, f.app)
	if !done || result.Image != nil || result.Err == nil {
		t.Fatalf("the error state = (%+v, %v)", result, done)
	}
	err := result.Err
	if err.Kind != gpui.ImageCacheErrorAsset {
		t.Fatalf("error kind = %v, want Asset", err.Kind)
	}
	if err.Message != "Embedded resource not found: missing.png" {
		t.Fatalf("asset message = %q", err.Message)
	}
	if err.Error() != "asset error: Embedded resource not found: missing.png" {
		t.Fatalf("asset error = %q", err.Error())
	}
	// The error stays cached (the pinned Loaded(result) item).
	if _, done := f.handle.LoadImage(resource, f.window, f.app); !done {
		t.Fatal("the error state did not stay cached")
	}
	if *f.loads != 0 {
		t.Fatalf("a missing path still hit the source: %d loads", *f.loads)
	}
}

// TestImageCachePathResource pins Resource::Path loads (img.rs lines
// 625-626): a filesystem read decodes through the real codec, and a
// missing file is the IO error.
func TestImageCachePathResource(t *testing.T) {
	path := t.TempDir() + "/img.png"
	if err := os.WriteFile(path, pngBytes(t, 2, 2), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newCacheFixture(t, "unused.png", []byte{})
	resource := gpui.PathResource(path)

	if _, done := f.handle.LoadImage(resource, f.window, f.app); done {
		t.Fatal("the path load finished immediately")
	}
	f.ta.RunUntilParked()
	result, done := f.handle.LoadImage(resource, f.window, f.app)
	if !done || result.Image == nil {
		t.Fatalf("the path load = (%+v, %v)", result, done)
	}
	if w, h := result.Image.Size(0); w != 2 || h != 2 {
		t.Fatalf("size = %dx%d, want 2x2", w, h)
	}

	// A missing path is the IO variant.
	missing := gpui.PathResource(t.TempDir() + "/missing.png")
	if _, done := f.handle.LoadImage(missing, f.window, f.app); done {
		t.Fatal("the missing path finished immediately")
	}
	f.ta.RunUntilParked()
	result, done = f.handle.LoadImage(missing, f.window, f.app)
	if !done || result.Err == nil || result.Err.Kind != gpui.ImageCacheErrorIO {
		t.Fatalf("the missing path = (%+v, %v)", result, done)
	}
	if result.Err.Error()[:9] != "IO error:" {
		t.Fatalf("IO error = %q", result.Err.Error())
	}
}

// TestImageCacheAnimatedWebPFrames pins the animated path through the
// cache: the committed 3-frame WebP decodes with its rational delays
// and every frame is retained.
func TestImageCacheAnimatedWebPFrames(t *testing.T) {
	f := newCacheFixture(t, "anim.webp", fixture(t, "tiny-anim.webp"))
	resource := gpui.EmbeddedResource("anim.webp")
	atlas := mustAtlas(t, f.app)

	if _, done := f.handle.LoadImage(resource, f.window, f.app); done {
		t.Fatal("the animated load finished immediately")
	}
	f.ta.RunUntilParked()
	result, done := f.handle.LoadImage(resource, f.window, f.app)
	if !done || result.Image == nil {
		t.Fatalf("the animated load = (%+v, %v)", result, done)
	}
	if result.Image.FrameCount() != 3 {
		t.Fatalf("frames = %d, want 3", result.Image.FrameCount())
	}
	wantDelays := []time.Duration{40 * time.Millisecond, 100 * time.Millisecond, 250 * time.Millisecond}
	for i, want := range wantDelays {
		if got := result.Image.Delay(i); got != want {
			t.Fatalf("frame %d delay = %v, want %v", i, got, want)
		}
	}
	// The pinned out-of-range delay fallback: 100ms.
	if got := result.Image.Delay(99); got != 100*time.Millisecond {
		t.Fatalf("out-of-range delay = %v, want 100ms", got)
	}
	if got := tileCount(t, atlas); got != 3 {
		t.Fatalf("atlas tiles = %d, want one per frame (3)", got)
	}
	if !atlas.HasImageAtlasEntry(result.Image) {
		t.Fatal("the animated image is not fully retained")
	}
}

// TestImageCacheAnimatedGIFDelays pins the rational-delay preservation
// for a stdlib-encoded GIF through the real codec.
func TestImageCacheAnimatedGIFDelays(t *testing.T) {
	f := newCacheFixture(t, "anim.gif", gifBytes(t,
		[]color.RGBA{{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}},
		[]int{4, 10, 25}))
	resource := gpui.EmbeddedResource("anim.gif")

	if _, done := f.handle.LoadImage(resource, f.window, f.app); done {
		t.Fatal("the GIF load finished immediately")
	}
	f.ta.RunUntilParked()
	result, done := f.handle.LoadImage(resource, f.window, f.app)
	if !done || result.Image == nil {
		t.Fatalf("the GIF load = (%+v, %v)", result, done)
	}
	if result.Image.FrameCount() != 3 {
		t.Fatalf("frames = %d, want 3", result.Image.FrameCount())
	}
	wantDelays := []time.Duration{40 * time.Millisecond, 100 * time.Millisecond, 250 * time.Millisecond}
	for i, want := range wantDelays {
		if got := result.Image.Delay(i); got != want {
			t.Fatalf("frame %d delay = %v, want %v", i, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Clear, remove and release: retained atlas-frame ownership
// ---------------------------------------------------------------------------

// TestImageCacheClearRemovesAtlasExactlyOnce pins the pinned clear
// (image_cache.rs lines 299-305) over the atlas retention: loaded
// frames are removed exactly once, a repeated clear is a no-op, and a
// second cached image keeps its frames.
func TestImageCacheClearRemovesAtlasExactlyOnce(t *testing.T) {
	f := newCacheFixture(t, "img.png", pngBytes(t, 2, 2))
	atlas := mustAtlas(t, f.app)
	other := gpui.EmbeddedResource("other.png")
	if err := f.app.Assets().Insert("other.png", gpui.PreLoaded(pngBytes(t, 2, 2))); err != nil {
		t.Fatalf("Insert(other): %v", err)
	}

	resolve := func(resource gpui.Resource) *gpui.LoadedImage {
		t.Helper()
		if _, done := f.handle.LoadImage(resource, f.window, f.app); !done {
			f.ta.RunUntilParked()
			result, done := f.handle.LoadImage(resource, f.window, f.app)
			if !done || result.Image == nil {
				t.Fatalf("resolve(%s) = (%+v, %v)", resource, result, done)
			}
			return result.Image
		}
		result, _ := f.handle.LoadImage(resource, f.window, f.app)
		return result.Image
	}

	first := resolve(gpui.EmbeddedResource("img.png"))
	second := resolve(other)
	if got := tileCount(t, atlas); got != 2 {
		t.Fatalf("atlas tiles = %d, want 2", got)
	}

	// Clear removes every loaded image's frames — exactly once.
	f.clearCache()
	if got := tileCount(t, atlas); got != 0 {
		t.Fatalf("atlas tiles after clear = %d, want 0", got)
	}
	if atlas.HasImageAtlasEntry(first) || atlas.HasImageAtlasEntry(second) {
		t.Fatal("cleared images are still retained")
	}
	if got := cacheLen(t, f); got != 0 {
		t.Fatalf("cache len after clear = %d, want 0", got)
	}

	// A second clear (and the later release) must not remove again: the
	// map is empty and the tile count stays 0.
	f.clearCache()
	if got := tileCount(t, atlas); got != 0 {
		t.Fatalf("atlas tiles after the repeated clear = %d, want 0", got)
	}

	// Re-load after the clear: fresh load, fresh identity, fresh tiles.
	reloaded := resolve(gpui.EmbeddedResource("img.png"))
	if reloaded.ID() == first.ID() {
		t.Fatal("the re-loaded image kept the old identity")
	}
	if got := tileCount(t, atlas); got != 1 {
		t.Fatalf("atlas tiles after re-load = %d, want 1", got)
	}
	if *f.loads != 2 {
		t.Fatalf("asset loads = %d, want 2 (the clear invalidated the cache)", *f.loads)
	}
}

// TestImageCacheRemoveInvalidation pins the pinned remove
// (image_cache.rs lines 307-315): only the removed source's frames
// leave the atlas and the next load re-fetches.
func TestImageCacheRemoveInvalidation(t *testing.T) {
	f := newCacheFixture(t, "img.png", pngBytes(t, 2, 2))
	atlas := mustAtlas(t, f.app)
	resource := gpui.EmbeddedResource("img.png")
	keep := gpui.EmbeddedResource("keep.png")
	if err := f.app.Assets().Insert("keep.png", gpui.PreLoaded(pngBytes(t, 2, 2))); err != nil {
		t.Fatalf("Insert(keep): %v", err)
	}

	// Load both.
	if _, done := f.handle.LoadImage(resource, f.window, f.app); !done {
		f.ta.RunUntilParked()
	}
	if _, done := f.handle.LoadImage(keep, f.window, f.app); !done {
		f.ta.RunUntilParked()
	}
	removed, _ := f.handle.LoadImage(resource, f.window, f.app)
	kept, _ := f.handle.LoadImage(keep, f.window, f.app)
	if got := tileCount(t, atlas); got != 2 {
		t.Fatalf("atlas tiles = %d, want 2", got)
	}

	// Remove one: its frames are gone, the other's survive.
	f.removeCache(resource)
	if atlas.HasImageAtlasEntry(removed.Image) {
		t.Fatal("the removed image is still retained")
	}
	if !atlas.HasImageAtlasEntry(kept.Image) {
		t.Fatal("the remove dropped the other image's frames")
	}
	if got := tileCount(t, atlas); got != 1 {
		t.Fatalf("atlas tiles after remove = %d, want 1", got)
	}
	if got := cacheLen(t, f); got != 1 {
		t.Fatalf("cache len after remove = %d, want 1", got)
	}

	// The next load of the removed resource re-fetches.
	if _, done := f.handle.LoadImage(resource, f.window, f.app); done {
		t.Fatal("the removed resource resolved from an empty cache")
	}
	f.ta.RunUntilParked()
	if *f.loads != 2 {
		t.Fatalf("asset loads after remove = %d, want 2", *f.loads)
	}
}

// TestImageCacheReleaseDrivenDrop pins the release-driven drop (the
// pinned RetainAllImageCache::new observe_release, image_cache.rs
// lines 256-266): releasing the cache entity drops every loaded image
// with no current window.
func TestImageCacheReleaseDrivenDrop(t *testing.T) {
	f := newCacheFixture(t, "img.png", pngBytes(t, 2, 2))
	atlas := mustAtlas(t, f.app)
	resource := gpui.EmbeddedResource("img.png")

	if _, done := f.handle.LoadImage(resource, f.window, f.app); !done {
		f.ta.RunUntilParked()
	}
	result, _ := f.handle.LoadImage(resource, f.window, f.app)
	if !atlas.HasImageAtlasEntry(result.Image) {
		t.Fatal("the image was not retained before the release")
	}

	// Release and flush: the release observer drops the frames.
	f.cache.Release()
	f.ta.Update(func(*gpui.App) {})
	if atlas.HasImageAtlasEntry(result.Image) {
		t.Fatal("the release did not drop the retained frames")
	}
	if got := tileCount(t, atlas); got != 0 {
		t.Fatalf("atlas tiles after the release = %d, want 0", got)
	}
}

// TestImageCacheClearDuringLoadDiscardsLateResult pins the loading
// race (the pinned clear's take of still-Loading items): the load
// worker finishes, but the removed item never retains — no atlas
// frames appear and nothing panics.
func TestImageCacheClearDuringLoadDiscardsLateResult(t *testing.T) {
	f := newCacheFixture(t, "img.png", pngBytes(t, 2, 2))
	atlas := mustAtlas(t, f.app)
	resource := gpui.EmbeddedResource("img.png")

	if _, done := f.handle.LoadImage(resource, f.window, f.app); done {
		t.Fatal("the load finished before the clear")
	}
	// Clear while the load task is still queued.
	f.clearCache()
	f.ta.RunUntilParked()
	if *f.loads != 1 {
		t.Fatalf("asset loads = %d, want 1 (the worker still completed)", *f.loads)
	}
	if got := tileCount(t, atlas); got != 0 {
		t.Fatalf("atlas tiles after the discarded completion = %d, want 0", got)
	}
	if got := cacheLen(t, f); got != 0 {
		t.Fatalf("cache len = %d, want 0", got)
	}
}

// TestImageCacheReleaseDuringLoadDiscardsLateResult pins the release
// race: the cache entity releases while the load is in flight; the
// late completion is discarded by the entity delivery gate (ticket04)
// and nothing is retained.
func TestImageCacheReleaseDuringLoadDiscardsLateResult(t *testing.T) {
	f := newCacheFixture(t, "img.png", pngBytes(t, 2, 2))
	atlas := mustAtlas(t, f.app)
	resource := gpui.EmbeddedResource("img.png")

	if _, done := f.handle.LoadImage(resource, f.window, f.app); done {
		t.Fatal("the load finished before the release")
	}
	f.cache.Release()
	f.ta.Update(func(*gpui.App) {})
	f.ta.RunUntilParked()
	if *f.loads != 1 {
		t.Fatalf("asset loads = %d, want 1", *f.loads)
	}
	if got := tileCount(t, atlas); got != 0 {
		t.Fatalf("atlas tiles after the discarded completion = %d, want 0", got)
	}
}

// ---------------------------------------------------------------------------
// URI flows: status bodies, redirect settings, fakes
// ---------------------------------------------------------------------------

// TestImageLoadURIBadStatusFirstLine pins the pinned non-2xx mapping
// (img.rs lines 629-648): a BadStatus error carrying the URI, the
// status and ONLY the first body line with trailing whitespace
// trimmed, including the lossy UTF-8 replacement policy.
func TestImageLoadURIBadStatusFirstLine(t *testing.T) {
	uriFlows(t, "https://example.com/missing.png", 404,
		[]byte("Not Found: mult-line body\r\nsecond line\nthird  \n"),
		func(t *testing.T, err *gpui.ImageCacheError) {
			t.Helper()
			if err.Kind != gpui.ImageCacheErrorBadStatus {
				t.Fatalf("kind = %v, want BadStatus", err.Kind)
			}
			if err.URI != "https://example.com/missing.png" {
				t.Fatalf("uri = %q", err.URI)
			}
			if err.Status != 404 {
				t.Fatalf("status = %d", err.Status)
			}
			if err.Body != "Not Found: mult-line body" {
				t.Fatalf("body = %q, want the trimmed first line", err.Body)
			}
			want := `unexpected http status for "https://example.com/missing.png": 404 Not Found, body: Not Found: mult-line body`
			if err.Error() != want {
				t.Fatalf("error = %q, want %q", err.Error(), want)
			}
		})
}

// TestImageLoadURIEmptyBodyAndInvalidUTF8 pins the pinned first-line
// edge cases: an empty body stays empty, trailing whitespace is
// trimmed, and invalid UTF-8 becomes U+FFFD per maximal subpart (the
// from_utf8_lossy policy).
func TestImageLoadURIEmptyBodyAndInvalidUTF8(t *testing.T) {
	// Empty body: "" (lines().next().unwrap_or("")).
	uriFlows(t, "https://example.com/e.png", 500, nil, func(t *testing.T, err *gpui.ImageCacheError) {
		t.Helper()
		if err.Body != "" {
			t.Fatalf("empty body became %q", err.Body)
		}
		if err.Status != 500 || !bytes.Contains([]byte(err.Error()), []byte("500 Internal Server Error")) {
			t.Fatalf("500 error = %q", err.Error())
		}
	})

	// Two discrete invalid bytes are two maximal subparts: two U+FFFD
	// (Go's ToValidUTF8 would collapse them into one).
	uriFlows(t, "https://example.com/u.png", 502, []byte{0xff, 0xfe, 'x', 'y', 'z', ' ', ' '},
		func(t *testing.T, err *gpui.ImageCacheError) {
			t.Helper()
			if err.Body != "\uFFFD\uFFFDxyz" {
				t.Fatalf("lossy body = %q, want two replacements then xyz", err.Body)
			}
		})

	// A truncated 3-byte sequence is ONE maximal subpart, then the
	// ASCII continues.
	uriFlows(t, "https://example.com/v.png", 502, []byte{0xe2, 0x82, 'a', 'b'},
		func(t *testing.T, err *gpui.ImageCacheError) {
			t.Helper()
			if err.Body != "\uFFFDab" {
				t.Fatalf("truncated body = %q, want one replacement then ab", err.Body)
			}
		})
}

// TestImageLoadURIRecordsRedirectSetting pins the required redirect
// setting: resource URI loads call GET(url, follow_redirects = true)
// (img.rs line 629) — the scripted client records the request.
func TestImageLoadURIRecordsRedirectSetting(t *testing.T) {
	requests := []scriptedRequest{}
	uri := "https://example.com/img.png"
	client := okBodyClient(pngBytes(t, 2, 2), &requests)
	ta := gpui.NewTestApp().WithHTTPClient(client)
	app := ta.App()
	disposeAppImageAssets(t, app)
	window := gpui.NewTestWindow(app, gpui.Size{Width: 100, Height: 100})

	resource := gpui.URIResource(uri)
	if _, done := window.GetImageAsset(resource, app); done {
		t.Fatal("the URI load finished immediately")
	}
	ta.RunUntilParked()
	result, done := window.GetImageAsset(resource, app)
	if !done || result.Image == nil {
		t.Fatalf("the URI load = (%+v, %v)", result, done)
	}
	if len(requests) != 1 {
		t.Fatalf("requests = %v, want exactly one", requests)
	}
	if requests[0].url != uri || !requests[0].followRedirects {
		t.Fatalf("request = %+v, want (%q, follow=true)", requests[0], uri)
	}
}

// TestImageLoadURIRedirectResponseIsBadStatus pins the non-following
// server shape: when the client returns the redirect response itself
// (a server that does not redirect further), the load maps it to the
// BadStatus error like any non-2xx.
func TestImageLoadURIRedirectResponseIsBadStatus(t *testing.T) {
	uriFlows(t, "https://example.com/moved.png", 302,
		[]byte("Found\n"),
		func(t *testing.T, err *gpui.ImageCacheError) {
			t.Helper()
			if err.Status != 302 || err.Body != "Found" {
				t.Fatalf("redirect response error = %+v", err)
			}
		})
}

// uriFlows drives one URI load through a scripted client and checks
// the resulting BadStatus error.
func uriFlows(t *testing.T, uri string, status int, body []byte, check func(*testing.T, *gpui.ImageCacheError)) {
	t.Helper()
	requests := []scriptedRequest{}
	ta := gpui.NewTestApp().WithHTTPClient(statusClient(status, body, &requests))
	app := ta.App()
	disposeAppImageAssets(t, app)
	window := gpui.NewTestWindow(app, gpui.Size{Width: 100, Height: 100})

	resource := gpui.URIResource(uri)
	if _, done := window.GetImageAsset(resource, app); done {
		t.Fatal("the URI load finished immediately")
	}
	ta.RunUntilParked()
	result, done := window.GetImageAsset(resource, app)
	if !done || result.Image != nil || result.Err == nil {
		t.Fatalf("the URI load = (%+v, %v)", result, done)
	}
	check(t, result.Err)
}

// TestImageLoadURIOKDecodes pins the 200 flow end to end: the body
// bytes decode through the real codec and the load resolves.
func TestImageLoadURIOKDecodes(t *testing.T) {
	requests := []scriptedRequest{}
	uri := "https://example.com/ok.png"
	ta := gpui.NewTestApp().WithHTTPClient(okBodyClient(pngBytes(t, 4, 2), &requests))
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 100, Height: 100})

	resource := gpui.URIResource(uri)
	if _, done := window.GetImageAsset(resource, app); done {
		t.Fatal("the URI load finished immediately")
	}
	ta.RunUntilParked()
	result, done := window.GetImageAsset(resource, app)
	if !done || result.Image == nil || result.Err != nil {
		t.Fatalf("the URI load = (%+v, %v)", result, done)
	}
	if w, h := result.Image.Size(0); w != 4 || h != 2 {
		t.Fatalf("size = %dx%d, want 4x2", w, h)
	}
}

// TestImageLoadSVGFallbackIsExplicitUnsupported pins the SVG signal:
// bytes the resource entry cannot sniff (the pinned SVG path) fail
// with the explicit deferred-service error, never a silent skip.
func TestImageLoadSVGFallbackIsExplicitUnsupported(t *testing.T) {
	svg := []byte("<svg xmlns=\"http://www.w3.org/2000/svg\"/>")
	f := newCacheFixture(t, "icon.svg", svg)
	resource := gpui.EmbeddedResource("icon.svg")

	if _, done := f.handle.LoadImage(resource, f.window, f.app); done {
		t.Fatal("the SVG load finished immediately")
	}
	f.ta.RunUntilParked()
	result, done := f.handle.LoadImage(resource, f.window, f.app)
	if !done || result.Image != nil || result.Err == nil {
		t.Fatalf("the SVG load = (%+v, %v)", result, done)
	}
	if result.Err.Kind != gpui.ImageCacheErrorSVG {
		t.Fatalf("SVG error kind = %v, want the Usvg/deferred slot", result.Err.Kind)
	}
	if result.Err.Error() != "svg error: gpui: SVG rendering is a deferred service" {
		t.Fatalf("SVG error = %q", result.Err.Error())
	}
}

// TestImageLoadMalformedBytesIsImageError pins the decode-failure
// variant: recognizable-but-undecodable bytes are the Image error.
func TestImageLoadMalformedBytesIsImageError(t *testing.T) {
	// A truncated PNG: the sniff succeeds, the decode fails.
	f := newCacheFixture(t, "broken.png", pngBytes(t, 2, 2)[:20])
	resource := gpui.EmbeddedResource("broken.png")

	if _, done := f.handle.LoadImage(resource, f.window, f.app); done {
		t.Fatal("the malformed load finished immediately")
	}
	f.ta.RunUntilParked()
	result, done := f.handle.LoadImage(resource, f.window, f.app)
	if !done || result.Image != nil || result.Err == nil {
		t.Fatalf("the malformed load = (%+v, %v)", result, done)
	}
	if result.Err.Kind != gpui.ImageCacheErrorImage {
		t.Fatalf("malformed error kind = %v, want Image", result.Err.Kind)
	}
	if result.Err.Error()[:12] != "image error:" {
		t.Fatalf("malformed error = %q", result.Err.Error())
	}
}

// ---------------------------------------------------------------------------
// The net/http adapter (explicit configuration)
// ---------------------------------------------------------------------------

// TestGoHTTPClientAdapter pins the adapter's own policy: body/status
// round trip, redirect following on/off, and the per-request timeout.
// The server is a local httptest instance (loopback only).
func TestGoHTTPClientAdapter(t *testing.T) {
	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte("the body"))
		case "/redirect":
			http.Redirect(w, r, "/ok", http.StatusFound)
		case "/slow":
			time.Sleep(80 * time.Millisecond)
			_, _ = w.Write([]byte("late"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := gpui.NewGoHTTPClient(2 * time.Second)

	// 200 with the body.
	response, err := client.Get(server.URL+"/ok", true)
	if err != nil || response.Status != 200 || string(response.Body) != "the body" {
		t.Fatalf("ok = (%+v, %v)", response, err)
	}

	// Following redirects: the 302 resolves to the 200 body.
	response, err = client.Get(server.URL+"/redirect", true)
	if err != nil || response.Status != 200 || string(response.Body) != "the body" {
		t.Fatalf("follow = (%+v, %v)", response, err)
	}

	// Not following redirects: the 302 response itself is returned
	// (http.ErrUseLastResponse), exactly like the pinned
	// follow_redirects = false behavior.
	response, err = client.Get(server.URL+"/redirect", false)
	if err != nil || response.Status != 302 {
		t.Fatalf("no-follow = (%+v, %v)", response, err)
	}

	// The timeout policy: a slow endpoint exceeds a small timeout.
	slow := gpui.NewGoHTTPClient(10 * time.Millisecond)
	if _, err := slow.Get(server.URL+"/slow", true); err == nil {
		t.Fatal("the slow request survived the timeout")
	}

	if hits < 5 {
		t.Fatalf("server hits = %d, want at least 5", hits)
	}
}

// TestGoHTTPClientAdapterIsExplicit pins that the adapter is never the
// default: a fresh app keeps the null client even after constructing
// an adapter.
func TestGoHTTPClientAdapterIsExplicit(t *testing.T) {
	_ = gpui.NewGoHTTPClient(time.Second)
	app := gpui.NewTestApp().App()
	if _, err := app.HTTPClient().Get("https://example.com/x.png", true); err == nil || err.Error() != "No HttpClient available" {
		t.Fatalf("the default client changed after adapter construction: %v", err)
	}
}
