// Package assetspec holds ticket18's asset-loading gates at the gpui
// model level: the asset registry with duplicate-path diagnostics, the
// on-demand/pre-loaded source entries, the HTTP client policy (the
// null default, the blocked client, the fakes and the explicit
// net/http adapter), the image cache's three-state loads with
// shared-task dedup, clear/remove/release invalidation and retained
// atlas-frame ownership, the app-level fetch cache, the loading/error
// states in real elements through the deterministic test window, and
// the animation scheduling model with rational per-frame delays, the
// reduce-motion/active-window policies and the 200ms loading delay.
//
// The fixtures: stdlib-encoded PNG/GIF plus copies of the committed
// ticket17 WebPs (a 36-byte static and a 3-frame animated). Tests that
// need the native image codec FAIL when it is unavailable — they never
// skip.
package assetspec

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"io/fs"
	"os"
	"sort"
	"testing"

	"gpui-go/gpui"
)

// pngBytes encodes a WxH RGBA image with red/blue vertical halves (the
// imagespec fixture helper, reused so the decode oracle matches).
func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x < width/2 {
				img.SetRGBA(x, y, color.RGBA{R: 255, A: 255})
			} else {
				img.SetRGBA(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// gifBytes encodes an animated GIF whose frames have the given colors
// and delays (GIF.Delay is in 100ths of a second, so 4 = 40ms).
func gifBytes(t *testing.T, colors []color.RGBA, delays []int) []byte {
	t.Helper()
	palette := make(color.Palette, 0, len(colors)+1)
	palette = append(palette, color.Transparent)
	for _, c := range colors {
		palette = append(palette, c)
	}
	frames := make([]*image.Paletted, len(colors))
	for i, c := range colors {
		frames[i] = image.NewPaletted(image.Rect(0, 0, 2, 1), palette)
		frames[i].Set(0, 0, c)
		frames[i].Set(1, 0, c)
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{Image: frames, Delay: delays}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// fixture reads a committed fixture from this package's testdata (the
// ticket17 WebPs, copied from internal/native/testdata).
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return data
}

// countingRegistry builds a registry whose single entry counts every
// load — the load-count oracle for dedup and invalidation tests.
func countingRegistry(t *testing.T, path string, data []byte, loads *int) *gpui.AssetRegistry {
	t.Helper()
	registry := gpui.NewAssetRegistry()
	if err := registry.Insert(path, gpui.OnDemand(func() (gpui.AssetData, bool) {
		*loads++
		return data, true
	})); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return registry
}

// mustAtlas resolves the application's image atlas, failing the test
// when the native image service is unavailable (real-codec tests fail,
// never skip).
func mustAtlas(t *testing.T, app *gpui.App) *gpui.Atlas {
	t.Helper()
	atlas := app.ImageAtlas()
	if atlas == nil {
		t.Fatal("the application image atlas is unavailable (the native image service failed to load)")
	}
	return atlas
}

// tileCount reads the atlas tile count.
func tileCount(t *testing.T, atlas *gpui.Atlas) int {
	t.Helper()
	count, err := atlas.TileCount()
	if err != nil {
		t.Fatalf("TileCount: %v", err)
	}
	return count
}

// assetFixture is a deterministic app with a test window and one
// counting on-demand registry entry. loads is a pointer so fixtures
// embedding this struct by value still share the counter with the
// registry's closure.
type assetFixture struct {
	ta     *gpui.TestApp
	app    *gpui.App
	window *gpui.Window
	loads  *int
}

func newAssetFixture(t *testing.T, path string, data []byte) *assetFixture {
	t.Helper()
	f := &assetFixture{loads: new(int)}
	f.ta = gpui.NewTestApp().WithAssets(countingRegistry(t, path, data, f.loads))
	f.app = f.ta.App()
	f.window = gpui.NewTestWindow(f.app, gpui.Size{Width: 100, Height: 100})
	disposeAppImageAssets(t, f.app)
	return f
}

// disposeAppImageAssets releases the app's lazily built image codec and
// its native atlas when the test ends: the native service bounds live
// atlas handles (8), so every app that decodes must return its slot —
// tests fail rather than leak when the bound is hit.
func disposeAppImageAssets(t *testing.T, app *gpui.App) {
	t.Helper()
	t.Cleanup(func() {
		if atlas := app.ImageAtlas(); atlas != nil {
			_ = atlas.Dispose()
		}
	})
}

// ---------------------------------------------------------------------------
// The asset registry (assets.rs)
// ---------------------------------------------------------------------------

// TestAssetRegistryInsertLoadAndDuplicate pins AssetRegistry::insert /
// load and the DuplicateAssetPath diagnostic (assets.rs lines 141-171
// and the insert unit test): inserts load back, the first entry
// survives a rejected duplicate, and the error renders the pinned
// message.
func TestAssetRegistryInsertLoadAndDuplicate(t *testing.T) {
	registry := gpui.NewAssetRegistry()
	assets := map[string][]byte{
		"asset0": {0},
		"asset1": {1},
		"asset2": {2},
		"asset3": {3},
	}
	for path, data := range assets {
		if err := registry.Insert(path, gpui.PreLoaded(data)); err != nil {
			t.Fatalf("Insert(%s): %v", path, err)
		}
	}
	for path, want := range assets {
		got, found := registry.Load(path)
		if !found || !bytes.Equal(got, want) {
			t.Fatalf("Load(%s) = (%v, %v)", path, got, found)
		}
	}

	// The duplicate insert: the typed diagnostic, the pinned message and
	// the surviving first entry.
	err := registry.Insert("asset0", gpui.PreLoaded([]byte{9}))
	if err == nil {
		t.Fatal("duplicate Insert succeeded")
	}
	dup, ok := err.(gpui.DuplicateAssetPath)
	if !ok {
		t.Fatalf("Insert error %v (%T) is not a DuplicateAssetPath", err, err)
	}
	if dup.Path != "asset0" {
		t.Fatalf("duplicate path = %q, want asset0", dup.Path)
	}
	if want := `An asset with the pathkey "asset0" already exists`; err.Error() != want {
		t.Fatalf("duplicate message = %q, want %q", err.Error(), want)
	}
	if got, _ := registry.Load("asset0"); !bytes.Equal(got, []byte{0}) {
		t.Fatalf("the first entry was replaced: %v", got)
	}
}

// TestAssetRegistryExtend pins AssetRegistry::extend (assets.rs lines
// 173-195): inserts in order, logs and collects duplicate-path errors.
func TestAssetRegistryExtend(t *testing.T) {
	registry := gpui.NewAssetRegistry()
	if duplicates := registry.Extend([]gpui.AssetPathEntry{
		{Path: "a", Entry: gpui.PreLoaded([]byte{1})},
		{Path: "b", Entry: gpui.PreLoaded([]byte{2})},
	}); len(duplicates) != 0 {
		t.Fatalf("Extend reported duplicates: %v", duplicates)
	}
	// Duplicates against the registry and inside the same extend.
	duplicates := registry.Extend([]gpui.AssetPathEntry{
		{Path: "c", Entry: gpui.PreLoaded([]byte{3})},
		{Path: "a", Entry: gpui.OnDemand(func() (gpui.AssetData, bool) { return []byte{9}, true })},
		{Path: "b", Entry: gpui.PreLoaded([]byte{9})},
	})
	if len(duplicates) != 2 || duplicates[0].Path != "a" || duplicates[1].Path != "b" {
		t.Fatalf("Extend duplicates = %v, want [a b]", duplicates)
	}
	// The first values survive; the new entry landed.
	for path, want := range map[string][]byte{"a": {1}, "b": {2}, "c": {3}} {
		if got, found := registry.Load(path); !found || !bytes.Equal(got, want) {
			t.Fatalf("Load(%s) = (%v, %v)", path, got, found)
		}
	}
}

// countingSource is the pinned StubAssetSource shape (the assets.rs
// unit tests) with a load counter.
type countingSource struct {
	assets map[string][]byte
	loads  *int
}

func (s *countingSource) Load(path string) (gpui.AssetData, bool, error) {
	*s.loads++
	data, ok := s.assets[path]
	return data, ok, nil
}

func (s *countingSource) List(path string) ([]string, error) {
	keys := make([]string, 0, len(s.assets))
	for key := range s.assets {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

// TestAssetRegistrySourceEntries pins the source-driven construction
// paths (assets.rs From<AssetSource>/From<Arc<dyn AssetSource>> over
// iter_ondemand, FromIterator, iter_preloaded, and the
// from/from_arc/from_iter/extend_assetsource unit tests): on-demand
// entries load lazily, pre-loaded entries never call the source, and
// every listed path resolves.
func TestAssetRegistrySourceEntries(t *testing.T) {
	assets := map[string][]byte{
		"asset0": {0},
		"asset1": {1},
		"asset2": {2},
	}
	loads := 0
	source := &countingSource{assets: assets, loads: &loads}

	// From<AssetSource> (the on-demand-from-source registry).
	fromSource := gpui.NewAssetRegistryFromSource(source)
	if loads != 0 {
		t.Fatalf("the on-demand registry pre-loaded %d assets", loads)
	}
	for path, want := range assets {
		if got, found := fromSource.Load(path); !found || !bytes.Equal(got, want) {
			t.Fatalf("on-demand Load(%s) = (%v, %v)", path, got, found)
		}
	}
	if loads != len(assets) {
		t.Fatalf("on-demand loads = %d, want %d", loads, len(assets))
	}

	// AssetEntriesPreLoaded (iter_preloaded) loads every asset now.
	loads = 0
	preloaded := gpui.NewAssetRegistry()
	if duplicates := preloaded.Extend(gpui.AssetEntriesPreLoaded(source)); len(duplicates) != 0 {
		t.Fatalf("preloaded Extend duplicates: %v", duplicates)
	}
	if loads != len(assets) {
		t.Fatalf("pre-loaded loads = %d, want %d", loads, len(assets))
	}
	for path, want := range assets {
		if got, found := preloaded.Load(path); !found || !bytes.Equal(got, want) {
			t.Fatalf("pre-loaded Load(%s) = (%v, %v)", path, got, found)
		}
	}

	// AssetEntriesOnDemand (iter_ondemand) wraps the shared source.
	loads = 0
	onDemand := gpui.NewAssetRegistry()
	if duplicates := onDemand.Extend(gpui.AssetEntriesOnDemand(source)); len(duplicates) != 0 {
		t.Fatalf("on-demand Extend duplicates: %v", duplicates)
	}
	if loads != 0 {
		t.Fatalf("iter_ondemand pre-loaded %d assets", loads)
	}
	for path, want := range assets {
		if got, found := onDemand.Load(path); !found || !bytes.Equal(got, want) {
			t.Fatalf("iter_ondemand Load(%s) = (%v, %v)", path, got, found)
		}
	}

	// FromIterator over (path, entry) pairs with an on-demand entry; a
	// duplicate is logged and the first value survives.
	loads = 0
	fromEntries := gpui.AssetRegistryFromEntries([]gpui.AssetPathEntry{
		{Path: "x", Entry: gpui.PreLoaded([]byte{7})},
		{Path: "y", Entry: gpui.OnDemand(func() (gpui.AssetData, bool) {
			loads++
			return []byte{8}, true
		})},
		{Path: "x", Entry: gpui.PreLoaded([]byte{9})},
	})
	if got, found := fromEntries.Load("x"); !found || !bytes.Equal(got, []byte{7}) {
		t.Fatalf("FromIterator Load(x) = (%v, %v)", got, found)
	}
	if got, found := fromEntries.Load("y"); !found || loads != 1 || !bytes.Equal(got, []byte{8}) {
		t.Fatalf("FromIterator Load(y) = (%v, %v) after %d loads", got, found, loads)
	}
}

// TestAssetRegistryMissingAndEmptySource pins the miss and the pinned
// `impl AssetSource for ()` (assets.rs lines 65-79).
func TestAssetRegistryMissingAndEmptySource(t *testing.T) {
	registry := gpui.NewAssetRegistry()
	if _, found := registry.Load("missing"); found {
		t.Fatal("Load found a missing path")
	}
	// A nil registry behaves as the empty registry.
	var nilRegistry *gpui.AssetRegistry
	if _, found := nilRegistry.Load("x"); found {
		t.Fatal("nil registry Load found a path")
	}

	empty := gpui.NewAssetRegistryFromSource(gpui.EmptyAssetSource{})
	if got, found := empty.Load("anything"); found {
		t.Fatalf("empty source Load = (%v, true)", got)
	}

	// The empty source itself.
	if data, found, err := (gpui.EmptyAssetSource{}).Load("x"); found || err != nil || data != nil {
		t.Fatalf("EmptyAssetSource.Load = (%v, %v, %v)", data, found, err)
	}
	if paths, err := (gpui.EmptyAssetSource{}).List(""); err != nil || len(paths) != 0 {
		t.Fatalf("EmptyAssetSource.List = (%v, %v)", paths, err)
	}
}

// TestResourceIdentity pins the source/caching identity (asset_cache.rs
// Resource + hash): equal resources hash equal, kinds distinguish, and
// distinct values hash distinct.
func TestResourceIdentity(t *testing.T) {
	uriA := gpui.URIResource("https://example.com/a.png")
	if uriA.AssetHash() != gpui.URIResource("https://example.com/a.png").AssetHash() {
		t.Fatal("equal URI resources hash differently")
	}
	if uriA.AssetHash() == gpui.URIResource("https://example.com/b.png").AssetHash() {
		t.Fatal("different URI resources hash equal")
	}
	// The same value under different kinds is a different resource.
	same := "icon.png"
	embeds := gpui.EmbeddedResource(same)
	if embeds.AssetHash() == gpui.PathResource(same).AssetHash() ||
		embeds.AssetHash() == gpui.URIResource(same).AssetHash() {
		t.Fatal("resource kinds are not distinguished by the hash")
	}
	if embeds.AssetHash() != gpui.EmbeddedResource(same).AssetHash() {
		t.Fatal("equal embedded resources hash differently")
	}
}

// ---------------------------------------------------------------------------
// The HTTP client policy (http_client.rs)
// ---------------------------------------------------------------------------

// scriptedClient records every request and answers through a handler —
// the fake for the 200/404/redirect/cancel flows and the
// redirect-setting assertion.
type scriptedClient struct {
	requests *[]scriptedRequest
	handler  func(url string, followRedirects bool) (gpui.HttpResponse, error)
}

type scriptedRequest struct {
	url             string
	followRedirects bool
}

func (c *scriptedClient) Get(url string, followRedirects bool) (gpui.HttpResponse, error) {
	*c.requests = append(*c.requests, scriptedRequest{url: url, followRedirects: followRedirects})
	return c.handler(url, followRedirects)
}

// okBodyClient answers 200 with the given body.
func okBodyClient(body []byte, requests *[]scriptedRequest) *scriptedClient {
	return &scriptedClient{requests: requests, handler: func(string, bool) (gpui.HttpResponse, error) {
		return gpui.HttpResponse{Status: 200, Body: body}, nil
	}}
}

// statusClient answers a fixed status and body.
func statusClient(status int, body []byte, requests *[]scriptedRequest) *scriptedClient {
	return &scriptedClient{requests: requests, handler: func(string, bool) (gpui.HttpResponse, error) {
		return gpui.HttpResponse{Status: status, Body: body}, nil
	}}
}

// TestNullHTTPClientIsDefault pins the default client (http_client.rs
// NullHttpClient; app.rs line 187 constructs it as the application
// default): a fresh app makes zero network attempts and a URI image
// load fails with the pinned message wrapped in the "loading image
// asset from" context.
func TestNullHTTPClientIsDefault(t *testing.T) {
	if _, err := (gpui.NullHttpClient{}).Get("https://example.com/a.png", true); err == nil || err.Error() != "No HttpClient available" {
		t.Fatalf("NullHttpClient.Get error = %v, want \"No HttpClient available\"", err)
	}

	ta := gpui.NewTestApp()
	app := ta.App()
	disposeAppImageAssets(t, app)
	// The app's default client IS the null client.
	if _, err := app.HTTPClient().Get("https://example.com/a.png", true); err == nil || err.Error() != "No HttpClient available" {
		t.Fatalf("default app client error = %v, want \"No HttpClient available\"", err)
	}

	window := gpui.NewTestWindow(app, gpui.Size{Width: 100, Height: 100})
	resource := gpui.URIResource("https://example.com/a.png")
	if _, done := window.GetImageAsset(resource, app); done {
		t.Fatal("a URI load finished with the null client")
	}
	ta.RunUntilParked()
	result, done := window.GetImageAsset(resource, app)
	if !done {
		t.Fatal("the URI load never finished")
	}
	if result.Image != nil {
		t.Fatal("the null client produced an image")
	}
	err := result.Err
	if err == nil {
		t.Fatal("the null client produced no error")
	}
	want := `error: loading image asset from "https://example.com/a.png": No HttpClient available`
	if err.Error() != want {
		t.Fatalf("null load error = %q, want %q", err.Error(), want)
	}
	if err.Kind != gpui.ImageCacheErrorOther {
		t.Fatalf("null load error kind = %v, want Other", err.Kind)
	}
}

// TestBlockedHTTPClient pins BlockedHttpClient (http_client.rs lines
// 40-63): the permission error, its message, and the wrapped form an
// image load surfaces.
func TestBlockedHTTPClient(t *testing.T) {
	_, err := gpui.NewBlockedHttpClient().Get("https://example.com/a.png", true)
	if err == nil {
		t.Fatal("BlockedHttpClient.Get succeeded")
	}
	if err.Error() != "BlockedHttpClient disallowed request" {
		t.Fatalf("blocked message = %q", err.Error())
	}
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("blocked error %v does not wrap a permission error", err)
	}

	ta := gpui.NewTestApp().WithHTTPClient(gpui.NewBlockedHttpClient())
	app := ta.App()
	disposeAppImageAssets(t, app)
	window := gpui.NewTestWindow(app, gpui.Size{Width: 100, Height: 100})
	resource := gpui.URIResource("https://example.com/x.png")
	// Start the load, then let the worker run.
	if _, done := window.GetImageAsset(resource, app); done {
		t.Fatal("the blocked load finished immediately")
	}
	ta.RunUntilParked()
	result, done := window.GetImageAsset(resource, app)
	if !done {
		t.Fatal("the blocked load never finished")
	}
	if result.Image != nil || result.Err == nil {
		t.Fatalf("blocked load result = %+v", result)
	}
	want := `error: loading image asset from "https://example.com/x.png": BlockedHttpClient disallowed request`
	if result.Err.Error() != want {
		t.Fatalf("blocked load error = %q, want %q", result.Err.Error(), want)
	}
	if !errors.Is(result.Err.Err, fs.ErrPermission) {
		t.Fatalf("the wrapped error %v does not wrap a permission error", result.Err.Err)
	}
}

// TestAppHTTPClientAccessors pins App::http_client / set_http_client
// (app.rs lines 1723-1729): the injected client replaces the null
// default and reads back.
func TestAppHTTPClientAccessors(t *testing.T) {
	app := gpui.NewTestApp().App()
	if _, err := app.HTTPClient().Get("https://example.com/x.png", true); err == nil || err.Error() != "No HttpClient available" {
		t.Fatalf("the default client is not null: %v", err)
	}
	app.SetHTTPClient(gpui.With404Response())
	response, err := app.HTTPClient().Get("https://example.com/x.png", true)
	if err != nil || response.Status != 404 {
		t.Fatalf("the injected client = (%+v, %v)", response, err)
	}
}

// TestAppAssetsAccessors pins App::assets / the with_assets observable
// (app.rs lines 2096-2098, 218-225): the default registry is empty and
// the assigned registry resolves.
func TestAppAssetsAccessors(t *testing.T) {
	app := gpui.NewTestApp().App()
	if _, found := app.Assets().Load("x"); found {
		t.Fatal("the default registry is not empty")
	}
	registry := gpui.NewAssetRegistry()
	if err := registry.Insert("x", gpui.PreLoaded([]byte{1})); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	app.SetAssets(registry)
	if data, found := app.Assets().Load("x"); !found || data[0] != 1 {
		t.Fatalf("the assigned registry = (%v, %v)", data, found)
	}
}

// TestFakeHTTPClients pins the pinned test-support fakes
// (http_client.rs FakeHttpClient::with_200_response / with_404_response).
func TestFakeHTTPClients(t *testing.T) {
	response, err := gpui.With200Response().Get("https://example.com/a.png", true)
	if err != nil || response.Status != 200 || len(response.Body) != 0 {
		t.Fatalf("200 fake = (%+v, %v)", response, err)
	}
	response, err = gpui.With404Response().Get("https://example.com/a.png", true)
	if err != nil || response.Status != 404 || len(response.Body) != 0 {
		t.Fatalf("404 fake = (%+v, %v)", response, err)
	}
}
