package gpui

// This file is ticket19's SVG model: the pinned SvgRenderer
// (reference/ce-source/crates/gpui/src/svg_renderer.rs at
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a) over the native SVG
// service (internal/native/svg.go, slot 8, capability bit 9 — the
// frozen revision-9 artifact): the process service behind the
// imageService sync.Once pattern, the parsed-document handle model,
// the three sizing modes with the smooth 2x factor, the alpha-mask
// entry, and the App surface (app.rs svg_renderer/with_assets
// wiring).
//
// The Go side OWNS the font orchestration the native side reports: the
// native SvgRenderer's font resolver (load_bundled_fonts consulting
// the two pinned TTF paths, fix_generic_font_families, the
// emoji-presentation fallback) lives behind the service's
// SvgFontAssetCount/SvgFontAssetPath/HasFontAsset/AddFont surface
// (svg.rs's NeedsFontAssets seam — the native worker never invokes a
// Go closure during a parse). Before the first text parse, this file
// resolves the pinned paths through the App's asset registry
// (ticket18's FetchAsset machinery: one shared load per path, running
// on a background worker) and pushes the bytes via AddFont. A parse
// that arrives while a font is missing is SUSPENDED — the needs-assets
// status of the distribution contract — and resumes exactly once when
// the bytes arrive, with no synchronous foreground callback into the
// native worker (the push happens inside the load's worker body).
//
// Deviations from the pin (bounded, recorded):
//
//   - The pin's SvgRenderer resolves its bundled fonts synchronously
//     inside usvg's resolver closures during the parse; the frozen
//     native service instead exposes the registry push/query surface,
//     so the port checks and pushes BEFORE the parse. A font added
//     after a first parse is seen by the NEXT parse (the native
//     renderer rebuilds its font snapshot when the pushed set grows —
//     the port's font-snapshot lifetime rule; old handles stay valid).
//   - The native font registry is process-scoped (the frozen service's
//     single global state), so the push happens once per process, not
//     once per App renderer as the pin's per-renderer OnceLock would
//     suggest; every App's renderer observes the same pushed set.
//   - The pin's render_parsed returns Arc<RenderImage> with
//     image.scale_factor; ticket17's frozen RenderImage model has no
//     scale-factor field, so RenderParsed returns SvgImage (the frame
//     model plus the applied factor) instead of reshaping image.go.
//   - The pin's render_alpha_mask zero-size rejection ("can't render
//     at a zero size", an anyhow error) surfaces as the native
//     ErrSvgBadValue through this model.

import (
	"fmt"
	"log"
	"math"
	"sync"

	"gpui-go/internal/native"
)

// ---------------------------------------------------------------------------
// The process SVG service (the imageService sync.Once pattern)
// ---------------------------------------------------------------------------

var (
	svgServiceOnce sync.Once
	svgServiceVal  *native.SvgService
	svgServiceErr  error
)

// ErrSvgService reports a failure to load the native SVG service.
var ErrSvgService = fmt.Errorf("gpui: native svg service unavailable")

// svgService loads the native library once and fetches the SVG
// service (imageService's pattern).
func svgService() (*native.SvgService, error) {
	svgServiceOnce.Do(func() {
		lib, err := native.Load(native.Options{})
		if err != nil {
			svgServiceErr = fmt.Errorf("gpui: %w: loading the native artifact: %w", ErrSvgService, err)
			return
		}
		svc, err := lib.Svg()
		if err != nil {
			svgServiceErr = fmt.Errorf("gpui: %w: %w", ErrSvgService, err)
			return
		}
		svgServiceVal = svc
	})
	return svgServiceVal, svgServiceErr
}

// The pinned SVG errors surfaced through this package (the native
// client's typed values, aliased so callers can errors.Is them without
// importing internal/native — image.go's ErrImageFormatUnknown
// pattern).
var (
	// ErrSvgParse is the pinned usvg parse/rasterize rejection.
	ErrSvgParse = native.ErrSvgParse
	// ErrSvgBadValue is the native argument rejection (a zero render
	// size, an invalid mode).
	ErrSvgBadValue = native.ErrSvgBadValue
)

// SmoothSvgScaleFactor is the pinned SMOOTH_SVG_SCALE_FACTOR
// (svg_renderer.rs line 133): SVGs render at twice the size for a
// higher-quality result. The native service reports the same value
// (SvgRenderer.SmoothScaleFactor).
const SmoothSvgScaleFactor = float32(2)

// ---------------------------------------------------------------------------
// Sizing (the pinned SvgSize) and render params
// ---------------------------------------------------------------------------

// SvgSizeMode selects the pinned SvgSize variant (svg_renderer.rs
// lines 84-92).
type SvgSizeMode uint8

const (
	// SvgSizeModeSize is SvgSize::Size: a width in device pixels, the
	// aspect ratio preserved.
	SvgSizeModeSize SvgSizeMode = iota + 1
	// SvgSizeModeExactSize is SvgSize::ExactSize: exact device
	// dimensions.
	SvgSizeModeExactSize
	// SvgSizeModeScaleFactor is SvgSize::ScaleFactor: a logical scaling
	// factor over the SVG's intrinsic size (the smooth 2x applies
	// inside the render and is reported back).
	SvgSizeModeScaleFactor
)

// SvgSize is the size in which to rasterize the SVG (the pinned
// SvgSize). Build one with SizeSvg, ExactSizeSvg or ScaleFactorSvg.
type SvgSize struct {
	// Mode selects the variant.
	Mode SvgSizeMode
	// Width and Height are the requested device pixels (Size and
	// ExactSize modes).
	Width, Height int
	// Scale is the logical scaling factor (ScaleFactor mode).
	Scale float32
}

// SizeSvg renders at a width in device pixels, preserving the aspect
// ratio (SvgSize::Size).
func SizeSvg(width, height int) SvgSize {
	return SvgSize{Mode: SvgSizeModeSize, Width: width, Height: height}
}

// ExactSizeSvg renders at exact device dimensions (SvgSize::ExactSize).
func ExactSizeSvg(width, height int) SvgSize {
	return SvgSize{Mode: SvgSizeModeExactSize, Width: width, Height: height}
}

// ScaleFactorSvg renders at a logical scaling factor
// (SvgSize::ScaleFactor; the pinned From<f32>).
func ScaleFactorSvg(scale float32) SvgSize {
	return SvgSize{Mode: SvgSizeModeScaleFactor, Scale: scale}
}

// RenderSvgParams identifies one mask cache entry (the pinned
// RenderSvgParams, svg_renderer.rs lines 118-122): the SVG's path
// identity and the rasterized size in device pixels.
type RenderSvgParams struct {
	// Path is the SVG's identity path (a registry path, an external
	// path, or the data element's __binary_svg__ hash path).
	Path string
	// Size is the raster size in device pixels (the element path
	// passes the snapped bounds scaled by SmoothSvgScaleFactor).
	Size Size
}

// SvgImage is a rendered SVG (the pinned render_parsed's
// Arc<RenderImage> with its scale factor): one BGRA frame plus the
// applied image scale factor. The pin's RenderImage.scale_factor
// rides beside ticket17's frozen frame model here.
type SvgImage struct {
	// Image is the rendered frame model (one BGRA frame; the pinned
	// Frame::new with a zero delay).
	Image *RenderImage
	// ScaleFactor is the applied image scale factor
	// (render_parsed: SMOOTH_SVG_SCALE_FACTOR for the ScaleFactor
	// mode, 1 otherwise).
	ScaleFactor float32
}

// ---------------------------------------------------------------------------
// The parsed document (the pinned ParsedSvg)
// ---------------------------------------------------------------------------

// ParsedSvg is a parsed SVG document that can be rasterized at any
// scale (the pinned ParsedSvg: usvg::Tree behind a native
// generation-stamped handle). Parsing resolves fonts and converts text
// to paths; callers that rasterize the same SVG at multiple scales
// retain this value to avoid re-paying the parse cost. Go has no drop,
// so retirement is explicit: Dispose releases the native handle, and
// the handle is stale afterwards (the native's typed error).
type ParsedSvg struct {
	svc    *native.SvgService
	handle native.SvgHandle
}

// Dispose releases the parsed document's native tree handle. It is
// idempotent-rejecting through the native lifecycle: a second dispose
// reports the stale-handle error.
func (p *ParsedSvg) Dispose() error {
	if p == nil {
		return fmt.Errorf("gpui: Dispose on a nil ParsedSvg")
	}
	if err := p.svc.Dispose(p.handle); err != nil {
		return fmt.Errorf("gpui: ParsedSvg dispose: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// The renderer (SvgRenderer) and the App surface
// ---------------------------------------------------------------------------

// SvgRenderer is everything necessary to render SVGs (the pinned
// SvgRenderer): the process native service plus the App's asset
// registry. The usvg options with the bundled-font resolver and the
// emoji fallback are native-side; this side owns the font
// orchestration (see the module docs) and the byte resolution for the
// registry-backed render paths.
type SvgRenderer struct {
	svc    *native.SvgService
	assets *AssetRegistry
}

// NewSvgRenderer creates a renderer over the process SVG service and
// the asset registry (SvgRenderer::new, svg_renderer.rs lines
// 154-215: the enriched font database and the resolver closures are
// native-side in this port).
func NewSvgRenderer(assets *AssetRegistry) (*SvgRenderer, error) {
	svc, err := svgService()
	if err != nil {
		return nil, err
	}
	if assets == nil {
		assets = NewAssetRegistry()
	}
	return &SvgRenderer{svc: svc, assets: assets}, nil
}

// SmoothScaleFactor reports the smooth factor the native service
// applies in the ScaleFactor mode (the pinned constant, reported
// through the service table).
func (r *SvgRenderer) SmoothScaleFactor() float32 {
	if r == nil {
		return 0
	}
	return r.svc.SmoothScaleFactor()
}

// FontAssetPaths returns the pinned bundled-font asset paths the
// renderer's native font resolution consults (svg_renderer.rs
// load_bundled_fonts' list, reported through the service surface).
// The Go orchestration loads exactly these paths through the App's
// asset registry.
func (r *SvgRenderer) FontAssetPaths() ([]string, error) {
	if r == nil {
		return nil, fmt.Errorf("gpui: no svg renderer")
	}
	count, err := r.svc.SvgFontAssetCount()
	if err != nil {
		return nil, fmt.Errorf("gpui: svg font asset count: %w", err)
	}
	paths := make([]string, 0, count)
	for i := uint32(0); i < count; i++ {
		path, err := r.svc.SvgFontAssetPath(i)
		if err != nil {
			return nil, fmt.Errorf("gpui: svg font asset path %d: %w", i, err)
		}
		paths = append(paths, path)
	}
	return paths, nil
}

// HasFontAsset reports whether the pinned font-asset path's bytes
// were pushed into the native registry (the missing set the
// orchestration resolves through the App's asset registry).
func (r *SvgRenderer) HasFontAsset(path string) (bool, error) {
	if r == nil {
		return false, fmt.Errorf("gpui: no svg renderer")
	}
	has, err := r.svc.HasFontAsset(path)
	if err != nil {
		return false, fmt.Errorf("gpui: svg font asset query: %w", err)
	}
	return has, nil
}

// SvgParseOutcome is a parse request's terminal result (the pinned
// Result<ParsedSvg, usvg::Error>, plus the orchestration's explicit
// font errors).
type SvgParseOutcome struct {
	// Svg is the parsed document (nil when Err is set).
	Svg *ParsedSvg
	// Err is the parse error (nil when Svg is set).
	Err error
}

// SvgFontAssetMissingError reports a pinned bundled-font asset path
// the application's asset registry does not carry (the explicit
// resolution of a missing asset path — never a hang: the pending parse
// fails with this error once the load completes).
type SvgFontAssetMissingError struct {
	// Path is the missing pinned font-asset path.
	Path string
}

// Error renders the pinned warning's information (load_bundled_fonts'
// "Bundled font not found: {path}", as the typed orchestration error).
func (e *SvgFontAssetMissingError) Error() string {
	return fmt.Sprintf("gpui: bundled font not found: %s", e.Path)
}

// ParseSvg parses SVG bytes into a ParsedSvg (svg_renderer.rs
// parse_svg, lines 236-239).
//
// Font orchestration (the NeedsFontAssets seam): the pinned bundled
// font assets must be pushed into the native service before the first
// text parse resolves them. When one is missing, this parse is
// SUSPENDED — every missing path's shared load is started (or joined)
// through the app's loading-asset cache, and the parse resumes
// exactly once when the loads complete (the bytes are pushed inside
// the load's worker body; the parse itself runs on the worker). The
// outcome is delivered into owner while it is open, on the dispatcher
// goroutine inside one application update; closing owner discards a
// pending parse (the ticket04 delivery gate — no delivery, no parse
// result escapes). A missing asset path fails the load explicitly:
// the pending parse resolves with the SvgFontAssetMissingError.
//
// The second return reports whether the outcome is already final: a
// ready parse (fonts pushed) parses synchronously exactly like the
// pin; deliver is only invoked for the suspended completion.
func (r *SvgRenderer) ParseSvg(cx *App, owner *Scope, bytes []byte, deliver func(SvgParseOutcome, *App)) (SvgParseOutcome, bool) {
	if r == nil {
		return SvgParseOutcome{Err: fmt.Errorf("gpui: no svg renderer")}, true
	}
	pending, err := r.ensureFontAssets(cx)
	if err != nil {
		return SvgParseOutcome{Err: err}, true
	}
	if len(pending) == 0 {
		// Fonts pushed: parse now (parse_svg).
		svg, err := r.parseNow(bytes)
		return SvgParseOutcome{Svg: svg, Err: err}, true
	}
	// The needs-assets suspension: one worker per request, awaiting
	// every missing path's shared load, then parsing once.
	loads := pending
	_ = cx.SpawnDelivering(owner, func(run *TaskRun) SvgParseOutcome {
		return r.resumeParse(run, loads, bytes)
	}, deliver)
	return SvgParseOutcome{}, false
}

// resumeParse is the suspended parse's worker body: await every
// missing font load (the pushes happen inside the load bodies), then
// parse exactly once. A cancelled request discards without parsing.
func (r *SvgRenderer) resumeParse(run *TaskRun, loads []Task[svgFontAssetResult], bytes []byte) SvgParseOutcome {
	for _, load := range loads {
		result := run.Await(load)
		if run.Cancelled() {
			return SvgParseOutcome{Err: fmt.Errorf("gpui: pending svg parse cancelled (the owner scope closed)")}
		}
		if result.Err != nil {
			return SvgParseOutcome{Err: fmt.Errorf("gpui: loading the pinned svg font asset: %w", result.Err)}
		}
	}
	svg, err := r.parseNow(bytes)
	return SvgParseOutcome{Svg: svg, Err: err}
}

// parseNow runs the native parse (parse_svg).
func (r *SvgRenderer) parseNow(bytes []byte) (*ParsedSvg, error) {
	handle, err := r.svc.Parse(bytes)
	if err != nil {
		return nil, fmt.Errorf("gpui: SvgRenderer parse: %w", err)
	}
	return &ParsedSvg{svc: r.svc, handle: handle}, nil
}

// RenderParsed rasterizes a previously parsed SVG at the requested
// size (svg_renderer.rs render_parsed, lines 242-273): the pinned
// sizing (the smooth 2x factor for the ScaleFactor mode, the 8192
// clamp, the premultiplied-RGBA→straight-BGRA conversion) runs
// native-side; the BGRA frame and the applied scale factor come back
// through the capacity protocol.
func (r *SvgRenderer) RenderParsed(svg *ParsedSvg, size SvgSize) (*SvgImage, error) {
	if r == nil {
		return nil, fmt.Errorf("gpui: no svg renderer")
	}
	if svg == nil {
		return nil, fmt.Errorf("gpui: RenderParsed on a nil ParsedSvg")
	}
	mode, width, height, scale, err := svgSizeNative(size)
	if err != nil {
		return nil, err
	}
	pixels, info, err := r.svc.Render(svg.handle, mode, uint32(width), uint32(height), scale)
	if err != nil {
		return nil, fmt.Errorf("gpui: SvgRenderer render: %w", err)
	}
	return &SvgImage{
		Image: &RenderImage{Frames: []ImageFrame{{
			Width:  int(info.Width),
			Height: int(info.Height),
			Pixels: pixels,
		}}},
		ScaleFactor: info.ScaleFactor,
	}, nil
}

// RenderSingleFrame renders the given bytes into an image buffer at a
// scaling factor (svg_renderer.rs render_single_frame, lines 275-282:
// parse, then render_parsed at the factor). This is the resource-image
// entry the img element's SVG fallback consumes (a deferred element
// path — ticket17's ErrSVGRendererDeferred seam).
func (r *SvgRenderer) RenderSingleFrame(bytes []byte, scale float32) (*SvgImage, error) {
	svg, err := r.parseNow(bytes)
	if err != nil {
		return nil, err
	}
	defer func() { _ = svg.Dispose() }()
	return r.RenderParsed(svg, ScaleFactorSvg(scale))
}

// RenderAlphaMask rasterizes the SVG's alpha mask (the pinned
// pub(crate) render_alpha_mask, svg_renderer.rs lines 284-314): the
// SVG renders at SvgSize::Size(params.Size) — aspect preserved — and
// the returned buffer carries one alpha byte per pixel. Bytes are
// used when non-nil; a nil bytes slice consults the renderer's asset
// registry for params.Path (the pin's asset_registry.load fallback),
// and a registry miss returns a nil mask with no error — the pinned
// Ok(None), nothing to draw. A zero params size is rejected (the
// pin's "can't render at a zero size" ensure; the native typed
// rejection).
func (r *SvgRenderer) RenderAlphaMask(params RenderSvgParams, bytes []byte) (mask []byte, width, height int, err error) {
	if r == nil {
		return nil, 0, 0, fmt.Errorf("gpui: no svg renderer")
	}
	if !svgSizeValid(params.Size.Width) || !svgSizeValid(params.Size.Height) {
		return nil, 0, 0, fmt.Errorf("gpui: RenderAlphaMask: %w: can't render at a zero size", ErrSvgBadValue)
	}
	if bytes == nil {
		data, found := r.assets.Load(params.Path)
		if !found {
			// The pinned Ok(None) branch: no bytes, no registry asset,
			// nothing to draw.
			return nil, 0, 0, nil
		}
		bytes = data
	}
	maskBytes, info, err := r.svc.RenderAlphaMask(bytes, uint32(int(params.Size.Width)), uint32(int(params.Size.Height)))
	if err != nil {
		return nil, 0, 0, fmt.Errorf("gpui: SvgRenderer render alpha mask: %w", err)
	}
	return maskBytes, int(info.Width), int(info.Height), nil
}

// svgSizeNative validates a SvgSize and maps it to the native render
// arguments (the render entry's argument rules: positive dimensions
// for the Size/ExactSize modes, a positive finite factor otherwise).
func svgSizeNative(size SvgSize) (mode uint32, width, height int, scale float32, err error) {
	switch size.Mode {
	case SvgSizeModeSize:
		mode, width, height, scale = native.SvgModeSize, size.Width, size.Height, 1
	case SvgSizeModeExactSize:
		mode, width, height, scale = native.SvgModeExactSize, size.Width, size.Height, 1
	case SvgSizeModeScaleFactor:
		mode, scale = native.SvgModeScaleFactor, size.Scale
		if !svgSizeValid(size.Scale) {
			return 0, 0, 0, 0, fmt.Errorf("gpui: SvgSize scale factor = %v, want finite and positive", size.Scale)
		}
	default:
		return 0, 0, 0, 0, fmt.Errorf("gpui: SvgSize mode %d is invalid", uint8(size.Mode))
	}
	if mode != native.SvgModeScaleFactor && (!svgSizeValid(float32(size.Width)) || !svgSizeValid(float32(size.Height))) {
		return 0, 0, 0, 0, fmt.Errorf("gpui: SvgSize dimensions %dx%d are zero or invalid", size.Width, size.Height)
	}
	return mode, width, height, scale, nil
}

// svgSizeValid reports a finite, positive value.
func svgSizeValid(v float32) bool {
	return !math.IsInf(float64(v), 0) && !math.IsNaN(float64(v)) && v > 0
}

// SvgRenderer returns the application's SVG renderer
// (App::svg_renderer, app.rs lines 1739-1741): lazily created over the
// process native service and tied to the application's asset registry
// (the with_assets wiring, app.rs lines 218-225 — SetAssets rebinds a
// fresh lazy renderer, exactly like with_assets replaces the pin's).
// The first failure sticks and every later call reports it.
func (a *App) SvgRenderer() (*SvgRenderer, error) {
	if r := a.svgRenderer; r != nil {
		return r, nil
	}
	if err := a.svgRendererErr; err != nil {
		return nil, err
	}
	r, err := NewSvgRenderer(a.assetRegistry)
	if err != nil {
		a.svgRendererErr = err
		return nil, err
	}
	a.svgRenderer = r
	return r, nil
}

// ---------------------------------------------------------------------------
// Font orchestration (the NeedsFontAssets seam)
// ---------------------------------------------------------------------------

// svgFontAssetLoader is the type-level tag keying the app's
// loading-asset cache for the pinned-font loads (the empty-enum tag
// pattern of ImageAssetLoader; the pin has no tag because
// load_bundled_fonts reads the registry synchronously inside the
// resolver — the port's needs-assets orchestration runs the read on a
// background worker and shares it through App::fetch_asset).
type svgFontAssetLoader struct{}

// svgFontAssetResult is one pinned font load's outcome: the bytes, or
// the explicit missing-asset error (never a hang — the load task
// completes either way).
type svgFontAssetResult struct {
	// Path is the pinned font-asset path.
	Path string
	// Bytes are the loaded font bytes (nil when Err is set).
	Bytes []byte
	// Err is the load error (nil when Bytes is set).
	Err error
}

// ensureFontAssets checks every pinned bundled-font path against the
// native registry (the missing set) and starts — or joins — one
// shared load per missing path through the app's loading-asset cache
// (App::fetch_asset: multiple calls only result in one load at a
// time). The returned tasks are the still-pending loads; a completed
// load has already pushed its bytes (or logged the pinned
// not-found warning). An error reports a font-surface failure (the
// service is broken); the caller surfaces it.
func (r *SvgRenderer) ensureFontAssets(cx *App) ([]Task[svgFontAssetResult], error) {
	count, err := r.svc.SvgFontAssetCount()
	if err != nil {
		return nil, fmt.Errorf("gpui: svg font asset count: %w", err)
	}
	var pending []Task[svgFontAssetResult]
	for i := uint32(0); i < count; i++ {
		path, err := r.svc.SvgFontAssetPath(i)
		if err != nil {
			return nil, fmt.Errorf("gpui: svg font asset path %d: %w", i, err)
		}
		has, err := r.svc.HasFontAsset(path)
		if err != nil {
			return nil, fmt.Errorf("gpui: svg font asset query: %w", err)
		}
		if has {
			continue
		}
		task, _ := r.fetchFontAsset(cx, path)
		if task.Done() {
			// The load already finished: its body pushed the bytes (or
			// logged the not-found warning — the pinned degraded
			// rendering with the system fonts).
			continue
		}
		pending = append(pending, task)
	}
	return pending, nil
}

// fetchFontAsset starts (or joins) the shared load for one pinned
// font-asset path (App::fetch_asset with the svg font loader tag). The
// worker body reads the captured registry (the established
// worker-safe registry read: the deterministic scheduler drives one
// worker body at a time), logs the pinned not-found warning on a
// miss, and pushes the bytes into the native registry exactly once —
// a duplicate push means a concurrent consumer already pushed the
// same path (the registry's DuplicateAssetPath rule) and is tolerated.
// No App state is touched from the worker.
func (r *SvgRenderer) fetchFontAsset(cx *App, path string) (Task[svgFontAssetResult], bool) {
	registry := r.assets
	svc := r.svc
	return FetchAsset[svgFontAssetLoader](cx, EmbeddedResource(path), func(run *TaskRun) svgFontAssetResult {
		if run.Cancelled() {
			return svgFontAssetResult{Path: path, Err: fmt.Errorf("gpui: the font asset load was cancelled")}
		}
		data, found := registry.Load(path)
		if !found || len(data) == 0 {
			// The pinned load_bundled_fonts miss: warn and continue with
			// the system fonts (svg_renderer.rs lines 399-404).
			log.Printf("gpui: bundled font not found: %s", path)
			return svgFontAssetResult{Path: path, Err: &SvgFontAssetMissingError{Path: path}}
		}
		if err := pushSvgFontAsset(svc, path, data); err != nil {
			return svgFontAssetResult{Path: path, Err: err}
		}
		return svgFontAssetResult{Path: path, Bytes: data}
	})
}

// pushSvgFontAsset pushes loaded font bytes under their pinned path
// (svg_add_font), tolerating a duplicate path: a second pusher of the
// same bytes lost the race to a concurrent consumer and the registry
// already holds the font — the exactly-once push under concurrency.
func pushSvgFontAsset(svc *native.SvgService, path string, data []byte) error {
	if has, err := svc.HasFontAsset(path); err == nil && has {
		return nil
	}
	if err := svc.AddFont(path, data); err != nil {
		if err == native.ErrSvgBadValue {
			// The duplicate rule fired between the check and the push:
			// a concurrent load already pushed this path.
			return nil
		}
		return fmt.Errorf("gpui: pushing the svg font asset %q: %w", path, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// The SVG mask atlas identity (the pinned AtlasKey::Svg adaptation)
// ---------------------------------------------------------------------------

// The pinned AtlasKey::Svg(RenderSvgParams) maps to the MONOCHROME
// pool (platform.rs lines 1840-1841) — one coverage byte per pixel,
// exactly the render_alpha_mask output. The frozen native atlas ABI
// has no Svg key variant (reference/native/src/atlas.rs: "Svg keys
// remain a later ticket; not silently accepted"), so this port rides
// the glyph key's AlphaMask format — the monochrome pool the pin
// selects — with the SVG identity in the key's font-id slot. The
// identity sets bit 62 (FNV-1a over the path and size in the low 62
// bits), structurally disjoint from both native font-id shapes: fontdb
// indices carry no high bits and the text stack's canonical ids are
// 1<<63|index. Every other glyph-key field is the zeroed raster
// identity (glyph 0, size 0, grayscale/independent style), so a real
// glyph key never collides: real glyphs carry a nonzero font size.
// The distinct AtlasKey::Svg variant is the recorded follow-up for the
// next artifact revision (the frozen ABI cannot carry it).

// svgMaskFontIdentity builds the disjoint font-id slot value for the
// SVG params (FNV-1a over the path and size, bit 62 set).
func svgMaskFontIdentity(params RenderSvgParams) uint64 {
	const offsetBasis uint64 = 0xcbf29ce484222325
	const prime uint64 = 0x00000100000001b3
	h := offsetBasis
	for _, b := range []byte(params.Path) {
		h ^= uint64(b)
		h *= prime
	}
	h ^= uint64(int(params.Size.Width))
	h *= prime
	h ^= uint64(int(params.Size.Height))
	h *= prime
	return 1<<62 | (h &^ (1<<62 | 1<<63))
}

// svgMaskAtlasKey builds the monochrome-pool key record for the SVG
// params (the pinned From<RenderSvgParams> for AtlasKey, adapted per
// the module docs).
func svgMaskAtlasKey(params RenderSvgParams) native.AtlasKeyRecord {
	key := native.NewAtlasKeyRecord()
	key.Kind = uint32(native.AtlasKeyGlyph)
	key.FontID = svgMaskFontIdentity(params)
	// The zeroed glyph raster identity: font size 0.0, scale 1.0,
	// grayscale/independent style, AlphaMask format (the monochrome
	// pool, one byte per pixel).
	key.FontSizeBits = 0
	key.ScaleBits = math.Float32bits(1)
	key.Style = native.NewRasterStyleRecord()
	key.Format = uint32(RasterFormatAlphaMask)
	return key
}

// InsertSvgMask uploads one rendered SVG alpha mask into the atlas's
// monochrome pool under the SVG params identity, returning its tile
// (the pinned sprite_atlas.get_or_insert_with with the
// AtlasKey::Svg(params) key, window.rs 5009-5021). A cached identity
// returns the existing tile with no upload (the map hit). width and
// height are the mask's actual raster dimensions (the aspect-
// preserving Size mode may render smaller than the request); mask is
// exactly one byte per pixel.
func (a *Atlas) InsertSvgMask(params RenderSvgParams, width, height int, mask []byte) (AtlasTile, error) {
	if a == nil {
		return AtlasTile{}, fmt.Errorf("gpui: InsertSvgMask: nil atlas")
	}
	if mask == nil {
		return AtlasTile{}, fmt.Errorf("gpui: InsertSvgMask: nil mask")
	}
	key := svgMaskAtlasKey(params)
	tile, err := a.svc.Insert(a.handle, key, int32(width), int32(height), mask)
	if err != nil {
		return AtlasTile{}, fmt.Errorf("gpui: InsertSvgMask: %w", err)
	}
	return AtlasTile{
		TextureIndex: tile.TextureIndex,
		TextureKind:  AtlasTextureKind(tile.TextureKind),
		TileID:       tile.TileID,
		Padding:      tile.Padding,
		BoundsX:      tile.BoundsX,
		BoundsY:      tile.BoundsY,
		BoundsW:      tile.BoundsW,
		BoundsH:      tile.BoundsH,
		Generation:   tile.Generation,
	}, nil
}

// RemoveSvgMask removes the SVG params' tile, deallocating its atlas
// space (PlatformAtlas::remove). Cache release and GPU completion are
// separate prerequisites, exactly like the glyph and image removals.
func (a *Atlas) RemoveSvgMask(params RenderSvgParams) error {
	if a == nil {
		return fmt.Errorf("gpui: RemoveSvgMask: nil atlas")
	}
	key := svgMaskAtlasKey(params)
	if err := a.svc.Remove(a.handle, key); err != nil {
		return fmt.Errorf("gpui: RemoveSvgMask: %w", err)
	}
	return nil
}

// SvgMaskTile returns the live tile of the SVG params identity (the
// get_or_insert_with cache-hit probe; false when no tile is live).
func (a *Atlas) SvgMaskTile(params RenderSvgParams) (AtlasTile, bool, error) {
	if a == nil {
		return AtlasTile{}, false, fmt.Errorf("gpui: SvgMaskTile: nil atlas")
	}
	key := svgMaskAtlasKey(params)
	tile, err := a.svc.Query(a.handle, key)
	if err != nil {
		if isNativeError(err, native.ErrAtlasNotFound) {
			return AtlasTile{}, false, nil
		}
		return AtlasTile{}, false, fmt.Errorf("gpui: SvgMaskTile: %w", err)
	}
	return AtlasTile{
		TextureIndex: tile.TextureIndex,
		TextureKind:  AtlasTextureKind(tile.TextureKind),
		TileID:       tile.TileID,
		Padding:      tile.Padding,
		BoundsX:      tile.BoundsX,
		BoundsY:      tile.BoundsY,
		BoundsW:      tile.BoundsW,
		BoundsH:      tile.BoundsH,
		Generation:   tile.Generation,
	}, true, nil
}
