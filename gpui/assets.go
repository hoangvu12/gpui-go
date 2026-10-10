package gpui

// This file is ticket18's port of the pinned asset registry and asset
// model: crates/gpui/src/assets.rs (AssetSource, AssetEntry, the
// DuplicateAssetPath diagnostic, AssetRegistry) and
// crates/gpui/src/asset_cache.rs (Resource, the Asset trait shape, the
// hash identity), plus the application loading-asset cache from
// crates/gpui/src/app.rs (loading_assets, fetch_asset, remove_asset,
// has_asset).
//
// It deliberately lives in package gpui, not a subpackage: the registry
// and the loading cache are wired into App state (fields in app.go,
// the scheduler's task machinery, the image loader's codec seam in
// imagecache.go). A separate asset package would have to import gpui
// for App/Window/Scope while gpui wires it back into the application —
// an import cycle. The pinned crate keeps the same layering inside one
// crate.
//
// Registry reads on the image-load workers are safe under this port's
// runtime model: the deterministic scheduler drives one worker body at
// a time while the dispatcher goroutine waits, so the foreground
// (registry mutation) never overlaps a worker (registry read).

import (
	"fmt"
	"log"
	"reflect"
)

// ---------------------------------------------------------------------------
// Asset sources (assets.rs AssetSource)
// ---------------------------------------------------------------------------

// AssetData is the pinned Cow<'static, [u8]> (assets.rs line 73): the
// asset's bytes, borrowed from embedded/bundled storage or owned by the
// registry. Go slices express the borrow/owned distinction natively.
type AssetData = []byte

// AssetSource is one way to store a set of assets for gpui to access
// (assets.rs lines 12-63, the deprecated-for-new-usage trait). It can
// be provided to AssetRegistry to load asset binaries on demand, or
// its entries can be pre-loaded (AssetEntriesPreLoaded) or wrapped
// on demand (AssetEntriesOnDemand).
type AssetSource interface {
	// Load loads the given asset from the source path (AssetSource::load):
	// the data and whether it was found, or the failure. The pinned
	// Result<Option<Cow>> maps to (data, found, err).
	Load(path string) (data AssetData, found bool, err error)
	// List lists the assets at the given path (AssetSource::list).
	List(path string) ([]string, error)
}

// EmptyAssetSource is the pinned `impl AssetSource for ()`
// (assets.rs lines 65-79): every load misses and every list is empty.
type EmptyAssetSource struct{}

// Load implements AssetSource: nothing is found.
func (EmptyAssetSource) Load(path string) (AssetData, bool, error) {
	return nil, false, nil
}

// List implements AssetSource: no assets.
func (EmptyAssetSource) List(path string) ([]string, error) {
	return nil, nil
}

// AssetPathEntry is one (path, entry) pair — the item shape the pinned
// iterator/FromIterator surfaces consume (extend, from_iter,
// iter_preloaded, iter_ondemand).
type AssetPathEntry struct {
	// Path is the asset's registry key.
	Path string
	// Entry is the inserted entry.
	Entry AssetEntry
}

// ---------------------------------------------------------------------------
// Asset entries (assets.rs AssetEntry)
// ---------------------------------------------------------------------------

// AssetEntry is the entry stored in the AssetRegistry (assets.rs lines
// 84-91): pre-loaded data or an on-demand loader.
type AssetEntry interface {
	// loadData resolves the entry's bytes (the registry's Load).
	loadData() (AssetData, bool)
	// isAssetEntry seals the sum to this package's variants.
	isAssetEntry()
}

// PreLoaded is the pinned AssetEntry::PreLoaded(AssetData) (assets.rs
// lines 92-96): the asset is pre-loaded — stored statically/embedded,
// or already loaded from disk. Construction from data is the pinned
// From<AssetData> impl.
type PreLoaded AssetData

// loadData implements AssetEntry.
func (e PreLoaded) loadData() (AssetData, bool) { return e, true }

// isAssetEntry seals the variant.
func (e PreLoaded) isAssetEntry() {}

// OnDemand is the pinned AssetEntry::OnDemand(FnAssetLoader)
// (assets.rs lines 97-101): a thread-safe callback invoked when the
// path is requested. Construction from the function is the pinned
// From<F> impl; nil loaders miss like a failed load.
type OnDemand func() (AssetData, bool)

// loadData implements AssetEntry.
func (e OnDemand) loadData() (AssetData, bool) {
	if e == nil {
		return nil, false
	}
	return e()
}

// isAssetEntry seals the variant.
func (e OnDemand) isAssetEntry() {}

// ---------------------------------------------------------------------------
// The asset registry (assets.rs AssetRegistry)
// ---------------------------------------------------------------------------

// DuplicateAssetPath is caused when multiple assets are registered to
// the AssetRegistry with the same path (assets.rs lines 104-108).
type DuplicateAssetPath struct {
	// Path is the colliding registry key.
	Path string
}

// Error renders the pinned message ("An asset with the pathkey {0:?}
// already exists"; the {:?} of SharedString is a quoted string).
func (e DuplicateAssetPath) Error() string {
	return fmt.Sprintf("An asset with the pathkey %q already exists", e.Path)
}

// AssetRegistry is the collection of assets known to the App (assets.rs
// lines 111-117). Users provide assets through an AssetSource
// (NewAssetRegistryFromSource), an entry list
// (AssetRegistryFromEntries) or direct inserts (Insert, Extend).
type AssetRegistry struct {
	entries map[string]AssetEntry
}

// NewAssetRegistry creates the empty registry (the pinned Default).
func NewAssetRegistry() *AssetRegistry {
	return &AssetRegistry{entries: make(map[string]AssetEntry)}
}

// NewAssetRegistryFromSource converts the provided AssetSource into a
// fresh registry where every listed path is loaded on demand (the
// pinned From<Arc<dyn AssetSource>> via iter_ondemand, assets.rs lines
// 135-143). Duplicate-path errors are logged and discarded (the pinned
// `let _ = assets.extend(...)`).
func NewAssetRegistryFromSource(source AssetSource) *AssetRegistry {
	registry := NewAssetRegistry()
	_ = registry.Extend(AssetEntriesOnDemand(source))
	return registry
}

// AssetRegistryFromEntries builds a registry from (path, entry) pairs
// (the pinned FromIterator, assets.rs lines 145-158): insert errors are
// logged and the remaining entries still land.
func AssetRegistryFromEntries(entries []AssetPathEntry) *AssetRegistry {
	registry := NewAssetRegistry()
	for _, entry := range entries {
		if err := registry.Insert(entry.Path, entry.Entry); err != nil {
			logAssetError(err)
		}
	}
	return registry
}

// AssetEntriesPreLoaded iterates all paths of the source (List("")) and
// loads each in turn (AssetSource::iter_preloaded, assets.rs lines
// 33-46): failed or missing loads are skipped (the pinned
// `.ok().flatten()?`), successful ones become PreLoaded entries.
func AssetEntriesPreLoaded(source AssetSource) []AssetPathEntry {
	paths, err := source.List("")
	if err != nil {
		return nil
	}
	entries := make([]AssetPathEntry, 0, len(paths))
	for _, path := range paths {
		data, found, err := source.Load(path)
		if err != nil || !found {
			continue
		}
		entries = append(entries, AssetPathEntry{Path: path, Entry: PreLoaded(data)})
	}
	return entries
}

// AssetEntriesOnDemand iterates all paths of the source and constructs
// an OnDemand entry for each (AssetSource::iter_ondemand, assets.rs
// lines 48-62): the loader calls back into the shared source when the
// path is requested, logging load errors (the pinned log_err().flatten()).
func AssetEntriesOnDemand(source AssetSource) []AssetPathEntry {
	paths, err := source.List("")
	if err != nil {
		return nil
	}
	entries := make([]AssetPathEntry, 0, len(paths))
	for _, path := range paths {
		path := path
		loader := func() (AssetData, bool) {
			data, found, err := source.Load(path)
			if err != nil {
				logAssetError(err)
				return nil, false
			}
			return data, found
		}
		entries = append(entries, AssetPathEntry{Path: path, Entry: OnDemand(loader)})
	}
	return entries
}

// Insert inserts a single asset into the registry
// (AssetRegistry::insert, assets.rs lines 141-160). If there is already
// an entry at the provided path, DuplicateAssetPath is returned and the
// registry is unchanged.
func (r *AssetRegistry) Insert(path string, entry AssetEntry) error {
	if entry == nil {
		panic("gpui: AssetRegistry.Insert requires a non-nil entry")
	}
	if r.entries == nil {
		r.entries = make(map[string]AssetEntry)
	}
	if _, exists := r.entries[path]; exists {
		return DuplicateAssetPath{Path: path}
	}
	r.entries[path] = entry
	return nil
}

// Load returns the asset data for a given path (AssetRegistry::load,
// assets.rs lines 162-171). Pre-loaded entries return their stored
// bytes; on-demand entries query the loader. A missing path (or a
// failed on-demand load) reports found=false.
func (r *AssetRegistry) Load(path string) (AssetData, bool) {
	if r == nil || r.entries == nil {
		return nil, false
	}
	entry, ok := r.entries[path]
	if !ok {
		return nil, false
	}
	return entry.loadData()
}

// Extend inserts each entry in order (AssetRegistry::extend, assets.rs
// lines 173-195): duplicate-path errors are logged and collected. The
// returned slice is empty when every insert succeeded; each element is
// one collision in insert order.
func (r *AssetRegistry) Extend(entries []AssetPathEntry) []DuplicateAssetPath {
	var duplicates []DuplicateAssetPath
	for _, entry := range entries {
		if err := r.Insert(entry.Path, entry.Entry); err != nil {
			logAssetError(err)
			if dup, ok := err.(DuplicateAssetPath); ok {
				duplicates = append(duplicates, dup)
			}
		}
	}
	return duplicates
}

// logAssetError logs one discarded registry error (the pinned
// gpui_util::log_err / log::error! surfaces).
func logAssetError(err error) {
	log.Printf("gpui: %v", err)
}

// ---------------------------------------------------------------------------
// Resources (asset_cache.rs Resource)
// ---------------------------------------------------------------------------

// ResourceKind selects the Resource variant (asset_cache.rs lines
// 15-22).
type ResourceKind uint8

const (
	// ResourceURI is Resource::Uri(SharedUri): the resource is at a URI.
	ResourceURI ResourceKind = iota + 1
	// ResourcePath is Resource::Path(Arc<Path>): the resource is at a
	// filesystem path.
	ResourcePath
	// ResourceEmbedded is Resource::Embedded(SharedString): the resource
	// is embedded in the application binary.
	ResourceEmbedded
)

// Resource identifies one asset location (asset_cache.rs Resource). The
// pinned payload types (SharedUri, Arc<Path>, SharedString) are all
// string-shaped in the Go port; the struct stays comparable for the
// cache identity.
type Resource struct {
	// Kind selects the variant.
	Kind ResourceKind
	// Value is the URI, the filesystem path or the embedded key.
	Value string
}

// URIResource builds a URI resource (Resource::Uri).
func URIResource(uri string) Resource {
	return Resource{Kind: ResourceURI, Value: uri}
}

// PathResource builds a filesystem-path resource (Resource::Path).
func PathResource(path string) Resource {
	return Resource{Kind: ResourcePath, Value: path}
}

// EmbeddedResource builds an embedded-key resource
// (Resource::Embedded).
func EmbeddedResource(key string) Resource {
	return Resource{Kind: ResourceEmbedded, Value: key}
}

// String renders the resource for diagnostics.
func (r Resource) String() string {
	switch r.Kind {
	case ResourceURI:
		return "uri:" + r.Value
	case ResourcePath:
		return "path:" + r.Value
	case ResourceEmbedded:
		return "embedded:" + r.Value
	default:
		return fmt.Sprintf("resource(%d):%s", uint8(r.Kind), r.Value)
	}
}

// AssetHash returns the source's cache identity — the pinned
// collections::FxBuildHasher::hash_one over the Resource's Hash derive
// (asset_cache.rs `hash`, lines 90-92). The port uses FNV-1a over the
// variant and the payload: the same quick, non-cryptographically
// secure hash family image.go's content hash uses.
func (r Resource) AssetHash() uint64 {
	const offsetBasis uint64 = 0xcbf29ce484222325
	const prime uint64 = 0x00000100000001b3
	h := offsetBasis
	for _, b := range []byte{byte(r.Kind)} {
		h ^= uint64(b)
		h *= prime
	}
	for i := 0; i < len(r.Value); i++ {
		h ^= uint64(r.Value[i])
		h *= prime
	}
	return h
}

// ---------------------------------------------------------------------------
// The Asset model and the application loading cache (asset_cache.rs
// Asset, app.rs loading_assets/fetch_asset/remove_asset/has_asset)
// ---------------------------------------------------------------------------

// AssetSourceKey is the pinned Asset::Source requirement — Clone + Hash
// + Send (asset_cache.rs lines 27-31). Go value copies are the clone;
// AssetHash supplies the loading-cache identity.
type AssetSourceKey interface {
	// AssetHash is the source's cache identity.
	AssetHash() uint64
}

// assetKey is the loading-asset cache key: the loader's type identity
// plus the source hash (the pinned (TypeId, u64) pair, app.rs lines
// 2757-2789).
type assetKey struct {
	loader reflect.Type
	source uint64
}

// FetchAsset returns the shared task loading the asset for source,
// reporting whether this call started the load (App::fetch_asset,
// app.rs lines 2769-2789). Multiple calls only result in one load at a
// time and the results are cached until RemoveAsset; a completed task
// stays cached and resolves immediately.
//
// L is the loader tag type — the pinned Asset implementor used as the
// TypeId key (the pin uses empty enums like ImageAssetLoader purely as
// type-level tags). load is the asset's load body, created on the
// foreground and executed on a background worker: capture what it needs
// from cx before returning, the worker never touches the App (the pin
// creates the future with &mut App and moves its clones into it).
func FetchAsset[L any, S AssetSourceKey, O any](cx *App, source S, load func(run *TaskRun) O) (Task[O], bool) {
	key := assetKey{loader: reflect.TypeFor[L](), source: source.AssetHash()}
	if cx.loadingAssets == nil {
		cx.loadingAssets = make(map[assetKey]*taskRun)
	}
	if existing, ok := cx.loadingAssets[key]; ok {
		return Task[O]{t: existing}, false
	}
	task := cx.background.Spawn(load)
	cx.loadingAssets[key] = task.t
	return task, true
}

// RemoveAsset removes the asset from GPUI's cache (App::remove_asset,
// app.rs lines 2753-2760): a later fetch starts a fresh load. The
// in-flight worker is unaffected — cancellation is cooperative and the
// cache entry is not a cancellation owner.
func RemoveAsset[L any, S AssetSourceKey](cx *App, source S) {
	delete(cx.loadingAssets, assetKey{loader: reflect.TypeFor[L](), source: source.AssetHash()})
}

// HasAsset reports whether the asset is present in GPUI's cache
// (loading or loaded) without fetching it (the pinned test-support
// App::has_asset, app.rs lines 2762-2767).
func HasAsset[L any, S AssetSourceKey](cx *App, source S) bool {
	_, ok := cx.loadingAssets[assetKey{loader: reflect.TypeFor[L](), source: source.AssetHash()}]
	return ok
}
