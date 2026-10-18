package gpui

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"gpui-go/internal/native"
)

// This file is the port's Go half of ticket32: the render-to-image
// model over the native renderer service's test-support entry
// (surface_render_to_image, the rev-10 artifact) plus the pinned
// headless-API applicability trace.
//
// The pin is CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui_windows/src/directx_renderer.rs:786-860
//     (DirectXRenderer::render_to_image, cfg any(test,
//     feature="test-support")): the recovery refusal, render without
//     presenting, the staging readback with row-pitch handling and the
//     BGRA->RGBA swap. The port renders through a real window surface
//     (the pin's own tests drive a HiddenWindow + DirectXRenderer;
//     the native Windows profile has no windowless renderer).
//   - crates/gpui_windows/src/window.rs:1105-1110
//     (WindowsWindow::render_to_image: the window's background
//     appearance forwarded to the renderer).
//   - crates/gpui/src/platform.rs:1099-1128 (the PlatformWindow::
//     render_to_image trait method with its "not implemented for this
//     platform" default, and the PlatformHeadlessRenderer trait).
//   - crates/gpui/src/window.rs:2715-2721 (the public
//     Window::render_to_image: the last rendered frame's scene).
//   - crates/gpui/src/platform/test/platform.rs and window.rs (the
//     test-support headless surface: the renderer factory, the
//     TestWindow draw/render_to_image delegation and its "no
//     HeadlessRenderer configured" refusal).
//   - crates/gpui_wgpu/src/wgpu_renderer/headless.rs (the alternate
//     backend's PlatformHeadlessRenderer implementation) and
//     crates/gpui_apple/src/metal_renderer.rs (macOS: the layer-backed
//     render_to_image and the offscreen render_scene_to_image) —
//     recorded by the applicability trace below, not implemented.
//
// Native half (done, ticket32's rev-10 artifact):
// internal/native/renderer_wave4.go RendererService.RenderToImage —
// the capacity protocol (a nil or too-small buffer returns the needed
// size with a nil error; retry with it) and ErrRendererRecovering
// (the pinned device-recovery refusal).

// ---------------------------------------------------------------------------
// Typed error
// ---------------------------------------------------------------------------

// The recovery-gate refusal sentinel (ErrRendererRecovering) is the
// shared device-loss seam declared in deviceloss.go (ticket29): the
// pin's render_to_image body opens with the same skip_draws gate —
// `ensure!(!self.skip_draws, "render_to_image unavailable while
// recovering from a lost device")` — so render-to-image is refused
// before any GPU work while the replacement device's resources are
// not yet rebuilt (a skipped frame, not a failed surface), and the
// wrapper below wraps the native sentinel too, so errors.Is matches
// either gpui's or native's.
//
// ABI note (honest deviation, recorded for the orchestrator): the
// native renderer service currently assigns ERR_RECOVERING and
// ERR_SCENE the same status code (-12), so a rejected scene handle
// returned by the render-to-image entry is indistinguishable from the
// recovery refusal at this seam. Scene-rejection identity is asserted
// through the draw entries (renderer service -12 / ERR_SCENE) as in
// internal/pathspec; a native-side code split is required before the
// render-to-image entry can report the two failures distinctly.

// ---------------------------------------------------------------------------
// The owned image
// ---------------------------------------------------------------------------

// PixelFormat describes the channel representation of a
// [RenderedImage]'s CPU bytes.
type PixelFormat uint8

const (
	// PixelFormatRGBA8 is 4 bytes per pixel in R, G, B, A order, 8 bits
	// per channel: the render target's stored channel values with the
	// byte order swapped from the BGRA8_UNORM target (the pin's
	// render_to_image swaps channels 0 and 2; no alpha conversion is
	// performed — premultiplied targets read back premultiplied).
	PixelFormatRGBA8 PixelFormat = 0
)

func (f PixelFormat) String() string {
	if f == PixelFormatRGBA8 {
		return "rgba8"
	}
	return "unknown"
}

// RenderedImage is an owned immutable CPU image captured from a
// surface without presenting: the exact device-pixel dimensions, the
// format metadata and the raw pixels of one rendered frame.
//
// Ownership mirrors the pin's `image::RgbaImage` hand-off: the image
// owns its pixel buffer; Pixels hands the buffer out under a read-only
// contract (mutating it violates the image's immutability), and every
// new capture allocates a fresh buffer — later renders never alias an
// earlier image's bytes. Use Pixel for safe reads.
type RenderedImage struct {
	width  uint32
	height uint32
	format PixelFormat
	// sourceFormat is the DXGI format of the render target the readback
	// copied from (DXGI_FORMAT_B8G8R8A8_UNORM = 87) — the capture-stage
	// format the conformance fixtures record.
	sourceFormat uint32
	// appearance is the WindowBackgroundAppearance the capture ran with.
	appearance WindowBackgroundAppearance
	// surfaceMode is the composition mode of the capturing surface.
	surfaceMode native.SurfaceMode
	// pixels is the owned, packed row-major RGBA8 buffer
	// (width*height*4 bytes; rows are tightly packed — the staging
	// texture's row pitch is stripped natively).
	pixels []byte
}

// Width returns the image's device-pixel width (the render target's
// extent, not the caller's window size).
func (img *RenderedImage) Width() uint32 { return img.width }

// Height returns the image's device-pixel height.
func (img *RenderedImage) Height() uint32 { return img.height }

// Format returns the CPU channel representation.
func (img *RenderedImage) Format() PixelFormat { return img.format }

// SourceFormat returns the DXGI format of the render target the
// readback copied from (87 = DXGI_FORMAT_B8G8R8A8_UNORM), the
// capture-stage format before the native BGRA->RGBA swap.
func (img *RenderedImage) SourceFormat() uint32 { return img.sourceFormat }

// Appearance returns the background appearance the capture ran with.
func (img *RenderedImage) Appearance() WindowBackgroundAppearance { return img.appearance }

// SurfaceMode returns the composition mode of the capturing surface.
func (img *RenderedImage) SurfaceMode() native.SurfaceMode { return img.surfaceMode }

// BytesPerRow returns the packed row stride in bytes (width * 4). The
// staging texture's own row pitch is a native-internal detail and is
// already stripped; every row starts at y*BytesPerRow.
func (img *RenderedImage) BytesPerRow() int { return int(img.width) * 4 }

// Len returns the pixel byte count (width * height * 4).
func (img *RenderedImage) Len() int { return len(img.pixels) }

// Pixels returns the image's owned raw pixel bytes: packed row-major
// RGBA8, exactly Len bytes. The buffer is owned by the image under a
// read-only contract — do not mutate it; later captures allocate fresh
// buffers and never alias this one.
func (img *RenderedImage) Pixels() []byte { return img.pixels }

// Pixel reads one pixel as R, G, B, A. Coordinates outside the image
// are an error (the pin's RgbaImage::get_pixel panics; the port
// returns a typed error instead of crashing the host).
func (img *RenderedImage) Pixel(x, y uint32) (r, g, b, a byte, err error) {
	if img == nil {
		return 0, 0, 0, 0, errors.New("gpui: Pixel on a nil image")
	}
	if x >= img.width || y >= img.height {
		return 0, 0, 0, 0, fmt.Errorf("gpui: Pixel(%d, %d) outside the %dx%d image", x, y, img.width, img.height)
	}
	offset := int(y)*img.BytesPerRow() + int(x)*4
	return img.pixels[offset], img.pixels[offset+1], img.pixels[offset+2], img.pixels[offset+3], nil
}

// ---------------------------------------------------------------------------
// Render to image (the pinned test-support path)
// ---------------------------------------------------------------------------

// RenderToImage captures the scene as an owned CPU image through the
// window's surface: it renders the finished scene WITHOUT presenting
// (the pin's render body) and stages the render target back into
// owned RGBA8 bytes (the pin's render_to_image tail: staging copy,
// row-pitch handling, BGRA->RGBA swap — all native). The surface is
// created on demand in DComp mode.
//
// The appearance follows the pinned render's match: opaque modes clear
// the target to opaque white and every other mode (transparent,
// blurred, the Mica materials) clears to fully transparent — the
// behind-window compositing those modes imply is the DWM half
// (SetBackgroundAppearance), not render-target content.
//
// The capacity protocol is internal to the gpui wrapper: the native
// entry first reports the needed byte count, then the retry fills the
// caller-owned buffer. A refusal while the renderer is recovering from
// device loss fails with the shared [ErrRendererRecovering] sentinel
// (the frame was skipped; the surface remains usable and no partial
// image is returned). Every attempt is recorded in the renderer's
// headless trace (see [Renderer.HeadlessTrace]).
func (r *Renderer) RenderToImage(w *Window, scene *Scene, atlas *Atlas, appearance WindowBackgroundAppearance) (*RenderedImage, error) {
	surface, err := r.GetOrCreateSurface(w)
	if err != nil {
		return nil, err
	}
	return surface.RenderToImage(scene, atlas, appearance)
}

// RenderToImage captures the scene as an owned CPU image through this
// surface without presenting (see [Renderer.RenderToImage]; the
// surface-scoped entry for callers that manage the surface lifecycle
// themselves). The atlas may be nil when the scene has no sprite
// batches, as with DrawScene/RenderScene.
func (s *Surface) RenderToImage(scene *Scene, atlas *Atlas, appearance WindowBackgroundAppearance) (*RenderedImage, error) {
	if scene == nil {
		return nil, fmt.Errorf("gpui: RenderToImage: nil scene")
	}
	var atlasHandle native.AtlasHandle
	if atlas != nil {
		atlasHandle = atlas.handle
	}
	r := s.renderer
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, errors.New("gpui: renderer is closed")
	}
	// Sync the surface extent with the window at this safe point (the
	// stale-extent rule; the capture records the surface's current
	// device-pixel extent).
	if s.window != nil && s.window.Alive() {
		if width, height, scale, err := deviceExtent(s.window); err == nil {
			if err := r.resizeSurfaceLocked(s, width, height, scale); err != nil {
				return nil, err
			}
		}
	}
	if err := scene.FinishIfNeeded(); err != nil {
		return nil, fmt.Errorf("gpui: RenderToImage: %w", err)
	}
	// The pinned appearance match: opaque clears white, everything else
	// clears transparent (directx_renderer.rs render's
	// `appearance.is_opaque()` arm).
	nativeAppearance := native.BackgroundOpaque
	if !appearance.IsOpaque() {
		nativeAppearance = native.BackgroundTransparent
	}
	// The capacity retry protocol: a nil buffer reports the needed
	// byte count with a nil error; retry with it. The renderer lock is
	// held across both legs, so the extent cannot change between them.
	pixels, needed, err := r.svc.RenderToImage(
		s.handle, native.SceneHandle(scene.Handle()), nativeAppearance, atlasHandle, nil,
	)
	if err != nil {
		if errors.Is(err, native.ErrRendererRecovering) {
			r.recordHeadless(s, appearance, HeadlessRenderRecovering, 0, 0, 0, err)
			return nil, fmt.Errorf("gpui: RenderToImage window %d: %w: %w", s.windowID, ErrRendererRecovering, err)
		}
		r.recordHeadless(s, appearance, HeadlessRenderFailed, 0, 0, 0, err)
		return nil, fmt.Errorf("gpui: RenderToImage window %d: %w", s.windowID, err)
	}
	if pixels == nil {
		if needed == 0 {
			err := fmt.Errorf("gpui: RenderToImage window %d: the render target reported a zero-sized readback", s.windowID)
			r.recordHeadless(s, appearance, HeadlessRenderFailed, 0, 0, 0, err)
			return nil, err
		}
		r.recordHeadless(s, appearance, HeadlessCapacityProbe, needed, 0, 0, nil)
		buf := make([]byte, needed)
		pixels, needed, err = r.svc.RenderToImage(
			s.handle, native.SceneHandle(scene.Handle()), nativeAppearance, atlasHandle, buf,
		)
		if err != nil {
			if errors.Is(err, native.ErrRendererRecovering) {
				r.recordHeadless(s, appearance, HeadlessRenderRecovering, 0, 0, 0, err)
				return nil, fmt.Errorf("gpui: RenderToImage window %d: %w: %w", s.windowID, ErrRendererRecovering, err)
			}
			r.recordHeadless(s, appearance, HeadlessRenderFailed, 0, 0, 0, err)
			return nil, fmt.Errorf("gpui: RenderToImage window %d: %w", s.windowID, err)
		}
		if pixels == nil {
			err := fmt.Errorf("gpui: RenderToImage window %d: the capacity retry reported %d needed again with a %d-byte buffer", s.windowID, needed, len(buf))
			r.recordHeadless(s, appearance, HeadlessRenderFailed, needed, 0, 0, err)
			return nil, err
		}
	}
	// Exact dims/format metadata: the surface's current extent must
	// match the readback (the native entry sizes the image from the
	// render target's own descriptor).
	info, err := s.renderer.svc.Info(s.handle)
	if err != nil {
		r.recordHeadless(s, appearance, HeadlessRenderFailed, needed, 0, 0, err)
		return nil, fmt.Errorf("gpui: RenderToImage window %d: %w", s.windowID, err)
	}
	if uint64(info.Width)*uint64(info.Height)*4 != uint64(len(pixels)) {
		err := fmt.Errorf("gpui: RenderToImage window %d: readback of %d bytes does not match the %dx%d surface extent", s.windowID, len(pixels), info.Width, info.Height)
		r.recordHeadless(s, appearance, HeadlessRenderFailed, needed, 0, 0, err)
		return nil, err
	}
	image := &RenderedImage{
		width:        info.Width,
		height:       info.Height,
		format:       PixelFormatRGBA8,
		sourceFormat: native.RenderTargetFormatValue,
		appearance:   appearance,
		surfaceMode:  s.mode,
		pixels:       pixels,
	}
	r.recordHeadless(s, appearance, HeadlessRenderCompleted, needed, image.width, image.height, nil)
	return image, nil
}

// ---------------------------------------------------------------------------
// The headless attempt trace
// ---------------------------------------------------------------------------

// HeadlessEventKind identifies one render-to-image attempt outcome.
type HeadlessEventKind uint8

const (
	// HeadlessCapacityProbe: the capacity protocol reported the needed
	// byte count for a capture (the retry leg follows).
	HeadlessCapacityProbe HeadlessEventKind = iota
	// HeadlessRenderCompleted: an owned image was produced.
	HeadlessRenderCompleted
	// HeadlessRenderRecovering: the capture was refused while the
	// renderer was recovering from device loss (the pinned skip_draws
	// gate; a skipped frame, not a failed surface).
	HeadlessRenderRecovering
	// HeadlessRenderFailed: a capture failed (stale surface, rejected
	// scene, zero-sized readback, metadata mismatch). No partial image
	// was returned and in-flight resources were not released.
	HeadlessRenderFailed
)

func (k HeadlessEventKind) String() string {
	switch k {
	case HeadlessCapacityProbe:
		return "capacity-probe"
	case HeadlessRenderCompleted:
		return "completed"
	case HeadlessRenderRecovering:
		return "recovering"
	case HeadlessRenderFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// HeadlessEvent is one recorded render-to-image attempt: the canceled
// and partial-failure records of the headless path. Failed and refused
// attempts record the typed error text; completed attempts record the
// produced image's exact extent and byte count.
type HeadlessEvent struct {
	// Kind is the attempt outcome.
	Kind HeadlessEventKind
	// Attempt is the renderer's sequential capture identity (from 1).
	Attempt uint64
	// Surface is the capturing window's identity.
	Surface WindowID
	// Appearance the capture ran with.
	Appearance WindowBackgroundAppearance
	// Needed is the byte count the capacity probe reported (capacity
	// events) or the capture's byte count (completed events).
	Needed uint32
	// Width and Height are the produced image's extent (completed
	// events).
	Width  uint32
	Height uint32
	// At is the event timestamp.
	At time.Time
	// Err is the failure's error text (failed and refused events).
	Err string
}

// headlessTraceBound caps the retained trace per renderer (the
// submission ledger is unbounded because every submission is a
// terminal-state obligation; render-to-image attempts are observations
// and only the recent window is retained).
const headlessTraceBound = 256

// headlessLog is one renderer's render-to-image trace. Entries are
// appended under the renderer lock (the capture path holds it) and a
// private mutex guards reads from HeadlessTrace.
type headlessLog struct {
	mu     sync.Mutex
	events []HeadlessEvent
	seq    uint64
}

func (l *headlessLog) append(event HeadlessEvent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.events) >= headlessTraceBound {
		kept := l.events[headlessTraceBound/2:]
		l.events = append(l.events[:0], kept...)
	}
	l.events = append(l.events, event)
}

func (l *headlessLog) snapshot() []HeadlessEvent {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]HeadlessEvent(nil), l.events...)
}

// The render-to-image trace registry. The Renderer struct is owned by
// renderer.go (unmodified by this ticket), so the per-renderer log
// lives here. Entries retain their renderer for post-mortem inspection
// after Close (the trace survives teardown); renderers are one-per-app
// in the normal shape, and the trace only grows when render-to-image
// runs.
var (
	headlessLogsMu sync.Mutex
	headlessLogs   = map[*Renderer]*headlessLog{}
)

// headlessLogFor returns (creating on first use) the renderer's trace.
func headlessLogFor(r *Renderer) *headlessLog {
	headlessLogsMu.Lock()
	defer headlessLogsMu.Unlock()
	if log, ok := headlessLogs[r]; ok {
		return log
	}
	log := &headlessLog{}
	headlessLogs[r] = log
	return log
}

// recordHeadless appends one attempt event. The renderer lock is held
// by the caller (the capture path); the log's own mutex serializes the
// append against HeadlessTrace readers.
func (r *Renderer) recordHeadless(s *Surface, appearance WindowBackgroundAppearance, kind HeadlessEventKind, needed, width, height uint32, err error) {
	log := headlessLogFor(r)
	log.mu.Lock()
	log.seq++
	attempt := log.seq
	log.mu.Unlock()
	event := HeadlessEvent{
		Kind:       kind,
		Attempt:    attempt,
		Surface:    s.windowID,
		Appearance: appearance,
		Needed:     needed,
		Width:      width,
		Height:     height,
		At:         time.Now(),
	}
	if err != nil {
		event.Err = err.Error()
	}
	log.append(event)
}

// HeadlessTrace returns a copy of the renderer's render-to-image
// attempt trace: capacity probes, completed captures, recovery
// refusals and failures, in order. The trace survives renderer Close
// (entries are retained by the registry for post-mortem inspection).
func (r *Renderer) HeadlessTrace() []HeadlessEvent {
	headlessLogsMu.Lock()
	log, ok := headlessLogs[r]
	headlessLogsMu.Unlock()
	if !ok {
		return nil
	}
	return log.snapshot()
}

// ---------------------------------------------------------------------------
// The headless API applicability trace (the pinned profiles)
// ---------------------------------------------------------------------------

// HeadlessProfile identifies one reference profile of the headless /
// render-to-image API family.
type HeadlessProfile string

const (
	// HeadlessProfileNativeWindows: the pinned native Windows host
	// (gpui_windows, the port's selected D3D11 adapter).
	HeadlessProfileNativeWindows HeadlessProfile = "native-windows"
	// HeadlessProfileTestSupport: the gpui crate's test platform
	// (crates/gpui/src/platform/test).
	HeadlessProfileTestSupport HeadlessProfile = "test-support"
	// HeadlessProfileWgpu: the alternate wgpu backend
	// (crates/gpui_wgpu).
	HeadlessProfileWgpu HeadlessProfile = "wgpu"
	// HeadlessProfileAppleMetal: the macOS Metal host (gpui_apple /
	// gpui_macos).
	HeadlessProfileAppleMetal HeadlessProfile = "apple-metal"
	// HeadlessProfilePlatformDefault: the PlatformWindow trait default
	// (platforms without a render_to_image override).
	HeadlessProfilePlatformDefault HeadlessProfile = "platform-default"
)

// HeadlessPortStatus reports what this port implements of a profile.
type HeadlessPortStatus uint8

const (
	// HeadlessPortImplemented: the profile's path is implemented and
	// tested in this port.
	HeadlessPortImplemented HeadlessPortStatus = iota
	// HeadlessPortPartial: the profile's observable behavior is
	// implemented through a different surface than the pin's, and
	// applicable missing work is recorded.
	HeadlessPortPartial
	// HeadlessPortMissing: applicable work is missing (blocks a
	// full-core claim until implemented or excepted).
	HeadlessPortMissing
	// HeadlessPortNotApplicable: the profile is not the selected
	// adapter and not required for the Windows-first core claim; the
	// applicability trace itself is the deliverable.
	HeadlessPortNotApplicable
)

func (s HeadlessPortStatus) String() string {
	switch s {
	case HeadlessPortImplemented:
		return "implemented"
	case HeadlessPortPartial:
		return "partial"
	case HeadlessPortMissing:
		return "missing"
	case HeadlessPortNotApplicable:
		return "not-applicable"
	default:
		return "unknown"
	}
}

// HeadlessSourceRef pins one source location a record was traced from.
type HeadlessSourceRef struct {
	// File is the pinned source path under reference/ce-source.
	File string
	// Lines is the line range.
	Lines string
	// Note describes what the location establishes.
	Note string
}

// HeadlessProfileRecord is the per-profile trace of what the pinned
// source supports for the headless / render-to-image family, with the
// source-supported failure behavior recorded SEPARATELY from what this
// port implements. Reference unsupported behavior is a real outcome to
// reproduce and identify (conformance contract), not a gap.
type HeadlessProfileRecord struct {
	// Profile identifies the record.
	Profile HeadlessProfile
	// Summary is the profile's shape in one sentence.
	Summary string
	// WindowRenderToImage reports whether the profile overrides
	// PlatformWindow::render_to_image (platform.rs:1104; the trait
	// default bails — see the platform-default record).
	WindowRenderToImage bool
	// WindowRenderToImageCfg is the cfg gate of that override (the
	// test-support profile closure).
	WindowRenderToImageCfg string
	// HeadlessRenderer reports whether the profile implements or
	// consumes the PlatformHeadlessRenderer trait (platform.rs:1111,
	// cfg any(test, feature="test-support", feature="bench-support")).
	HeadlessRenderer bool
	// HeadlessRendererNote qualifies how the trait is provided.
	HeadlessRendererNote string
	// Windowless reports whether rendering without a real platform
	// window is source-supported by the profile.
	Windowless bool
	// SourceSupportedErrors are the profile's source-supported failure
	// behaviors (exact messages or families), distinct from the port's
	// implemented path.
	SourceSupportedErrors []string
	// PortStatus is what this port implements of the profile.
	PortStatus HeadlessPortStatus
	// PortPath describes the implemented path (when not missing).
	PortPath string
	// MissingWork lists applicable missing work with its bounded
	// follow-up (empty when nothing applicable is missing).
	MissingWork []string
	// Notes carry profile facts that are neither errors nor gaps
	// (appearance semantics, availability, pin test shapes).
	Notes []string
	// Sources pin the trace's evidence.
	Sources []HeadlessSourceRef
}

// headlessProfileRecords is the pinned applicability trace. Built once;
// HeadlessProfileRecords hands out copies so callers cannot mutate it.
var headlessProfileRecords = []HeadlessProfileRecord{
	{
		Profile:                HeadlessProfileNativeWindows,
		Summary:                "The pinned native Windows host: a window-bound DirectXRenderer with the test-support render_to_image (render without present + staging readback).",
		WindowRenderToImage:    true,
		WindowRenderToImageCfg: `any(test, feature = "test-support")`,
		HeadlessRenderer:       false,
		HeadlessRendererNote:   "gpui_windows implements no PlatformHeadlessRenderer: a windowless headless surface is not source-supported on native Windows (the pin's own render_to_image tests drive a HiddenWindow + DirectXRenderer).",
		Windowless:             false,
		SourceSupportedErrors: []string{
			"render_to_image unavailable while recovering from a lost device (the skip_draws ensure at the entry of DirectXRenderer::render_to_image)",
		},
		PortStatus: HeadlessPortImplemented,
		PortPath:   "gpui.Surface.RenderToImage / gpui.Renderer.RenderToImage over the native renderer service's surface_render_to_image entry (rev-10 artifact, renderer service v3): render without presenting, staging readback with row-pitch handling and the BGRA->RGBA swap, the capacity protocol at the ABI, the recovery refusal surfaced as gpui.ErrRendererRecovering, the headless attempt trace recording capacity/completed/recovering/failed.",
		MissingWork: []string{
			"The Window-level render_to_image (the last rendered frame's scene plus the window's background appearance, gpui window.rs:2715-2721) requires the port's frame/paint-loop integration; until then callers pass the Scene and appearance explicitly. Bounded follow-up with the window frame slice.",
		},
		Notes: []string{
			"Appearance follows the pinned render's match: opaque modes clear the target to opaque white, every other mode (transparent, blurred, Mica) clears to fully transparent — the behind-window compositing is DWM work (SetBackgroundAppearance), not render-target content; the port's ABI carries the opaque/transparent distinction the render path makes.",
			"The pin's tests render through a HiddenWindow; the port's tests use shown windows on this machine's interactive session (same window-lease shape, no hidden-window assertion).",
			"The readback is the target's stored channel values with channels 0 and 2 swapped; no alpha conversion (premultiplied targets read back premultiplied), exactly as the pin.",
		},
		Sources: []HeadlessSourceRef{
			{File: "crates/gpui_windows/src/directx_renderer.rs", Lines: "786-860", Note: "render_to_image: the skip_draws refusal, render(scene, appearance), the staging copy, the RowPitch row loop, the BGRA->RGBA swap, RgbaImage::from_raw."},
			{File: "crates/gpui_windows/src/window.rs", Lines: "1105-1110", Note: "WindowsWindow::render_to_image forwards the window's background appearance to the renderer."},
			{File: "crates/gpui_windows/src/directx_renderer.rs", Lines: "544-551", Note: "render's pre_draw clear match on background_appearance.is_opaque()."},
			{File: "crates/gpui_windows/src/directx_renderer.rs", Lines: "2961-2985", Note: "the pin's default_image/custom_image test (dashed borders reaching both quad pipelines)."},
		},
	},
	{
		Profile:                HeadlessProfileTestSupport,
		Summary:                "The gpui crate's test platform: TestWindow consumes an optionally-installed PlatformHeadlessRenderer for drawing and render_to_image.",
		WindowRenderToImage:    true,
		WindowRenderToImageCfg: `any(test, feature = "test-support")`,
		HeadlessRenderer:       true,
		HeadlessRendererNote:   "Consumed, not implemented here: TestPlatform::with_platform installs a headless_renderer_factory; TestWindow::new receives its product. The gpui crate ships no implementation of its own (the wgpu crate provides one).",
		Windowless:             true,
		SourceSupportedErrors: []string{
			"render_to_image not available: no HeadlessRenderer configured (TestWindow::render_to_image without a factory-installed renderer)",
		},
		PortStatus: HeadlessPortPartial,
		PortPath:   "The test-support render-to-image behavior (render a scene to an owned image without presenting) is implemented through the native-Windows window surface (this ticket); the port's test tooling drives the same observable capture the pin's test platform provides.",
		MissingWork: []string{
			"The windowless TestWindow shape (TestPlatform::with_platform's headless_renderer_factory behind TestWindow::draw/render_to_image) has no port: native Windows cannot render without a window surface, so a Go headless window would need the wgpu alternate backend or a windowless native offscreen path. Bounded follow-up before any full-core headless-window claim; the plain 'no HeadlessRenderer configured' refusal is the source-supported outcome to reproduce meanwhile.",
		},
		Notes: []string{
			"Platform creation/availability: TestPlatform is pub(crate), so the factory is installable only within the gpui crate; TestAppContext::build constructs its platform with TestPlatform::new (no factory), which is why the plain test-support profile yields the 'no HeadlessRenderer configured' refusal.",
			"TestWindow::draw delegates to render_scene (the headless present analogue: submission without readback) and takes the sprite atlas from the renderer when one is configured, falling back to TestAtlas.",
			"TestWindow's device size is bounds.size.to_device_pixels(scale_factor = 2.0).",
		},
		Sources: []HeadlessSourceRef{
			{File: "crates/gpui/src/platform.rs", Lines: "1099-1106", Note: "PlatformWindow::render_to_image trait method, cfg any(test, feature=\"test-support\"), default bails."},
			{File: "crates/gpui/src/platform.rs", Lines: "1109-1128", Note: "PlatformHeadlessRenderer: render_scene_to_image, render_scene, sprite_atlas."},
			{File: "crates/gpui/src/platform/test/platform.rs", Lines: "24,44,132-164,437-451", Note: "TestPlatform (pub(crate)), the headless_renderer_factory, with_platform, open_window installing the renderer."},
			{File: "crates/gpui/src/platform/test/window.rs", Lines: "32,57-88,432-445,447-458", Note: "TestWindowState.renderer, the atlas choice, draw -> render_scene, render_to_image -> render_scene_to_image or the refusal."},
			{File: "crates/gpui/src/app/test_context.rs", Lines: "162", Note: "TestAppContext::build uses TestPlatform::new (no renderer factory)."},
		},
	},
	{
		Profile:                HeadlessProfileWgpu,
		Summary:                "The alternate wgpu backend: WgpuHeadlessRenderer implements PlatformHeadlessRenderer over an offscreen target with a padded-row readback.",
		WindowRenderToImage:    false,
		WindowRenderToImageCfg: "",
		HeadlessRenderer:       true,
		HeadlessRendererNote:   "impl gpui::PlatformHeadlessRenderer for WgpuHeadlessRenderer (render_scene_to_image, render_scene, sprite_atlas).",
		Windowless:             true,
		SourceSupportedErrors: []string{
			"headless render target must have positive dimensions (ensure_target)",
			"failed to render headless scene (render path)",
			"failed to poll offscreen readback: {error} (device poll)",
			"offscreen readback callback dropped: {error} / failed to map offscreen readback: {error} (map_async)",
			"offscreen readback dimensions did not match its data (from_raw guard)",
		},
		PortStatus: HeadlessPortNotApplicable,
		PortPath:   "Not selected: the renderer contract chose the native D3D11 host; wgpu remains a credible alternate backend, not the baseline.",
		MissingWork: []string{
			"None applicable to the selected adapter; if the backend selection ever changes, WgpuHeadlessRenderer's offscreen readback (padded bytes per row over wgpu::COPY_BYTES_PER_ROW_ALIGNMENT, BGRA8Unorm channel swap) becomes required work. Applicability traced here per the conformance contract's alternate-backend rule.",
		},
		Notes: []string{
			"The wgpu readback pads each row to the copy alignment and strips the padding on read — the same row-pitch concern the native D3D11 staging path handles; both produce packed RGBA rows.",
			"render_scene is the submission-without-readback analogue; render_scene_and_wait polls the submission to completion.",
		},
		Sources: []HeadlessSourceRef{
			{File: "crates/gpui_wgpu/src/wgpu_renderer/headless.rs", Lines: "30-146", Note: "OffscreenTarget: padded_bytes_per_row, the readback buffer, read_offscreen_target's row loop and Bgra8Unorm swap."},
			{File: "crates/gpui_wgpu/src/wgpu_renderer/headless.rs", Lines: "149-202", Note: "WgpuHeadlessRenderer::new, ensure_target (positive dimensions), render_to_image, render_scene_and_wait."},
			{File: "crates/gpui_wgpu/src/wgpu_renderer/headless.rs", Lines: "204-231", Note: "the PlatformHeadlessRenderer implementation."},
		},
	},
	{
		Profile:                HeadlessProfileAppleMetal,
		Summary:                "The macOS Metal host: a layer-backed render_to_image plus an offscreen render_scene_to_image (the platform's headless path).",
		WindowRenderToImage:    true,
		WindowRenderToImageCfg: `any(test, feature = "test-support")`,
		HeadlessRenderer:       false,
		HeadlessRendererNote:   "No PlatformHeadlessRenderer trait implementation; MetalRenderer::render_scene_to_image is the inherent offscreen method ('the primary method for headless rendering').",
		Windowless:             true,
		SourceSupportedErrors: []string{
			"render_to_image requires a layer-backed renderer",
			"Failed to get drawable for render_to_image",
			"Failed to create RgbaImage from pixel data",
		},
		PortStatus:  HeadlessPortNotApplicable,
		PortPath:    "Not applicable to the Windows-first port; recorded for applicability (macOS support is out of scope before the Windows milestone).",
		MissingWork: []string{},
		Notes: []string{
			"The layer-backed path reads the drawable's texture with get_bytes at width*4 bytes per row and swaps channels 0 and 2; the offscreen path renders into a private texture.",
			"The macOS wgpu renderer additionally provides a test-support render_to_image and a cfg(test, not(test-support)) refusal ('rendering to an image needs the test-support feature').",
		},
		Sources: []HeadlessSourceRef{
			{File: "crates/gpui_apple/src/metal_renderer.rs", Lines: "698-755", Note: "MetalRenderer::render_to_image (layer-backed; the drawable readback)."},
			{File: "crates/gpui_apple/src/metal_renderer.rs", Lines: "757+", Note: "MetalRenderer::render_scene_to_image (offscreen, no window/layer)."},
			{File: "crates/gpui_macos/src/window.rs", Lines: "2417", Note: "the macOS window's PlatformWindow::render_to_image."},
			{File: "crates/gpui_macos/src/wgpu_renderer.rs", Lines: "121-133", Note: "the macOS wgpu render_to_image and its no-test-support refusal."},
		},
	},
	{
		Profile:                HeadlessProfilePlatformDefault,
		Summary:                "The PlatformWindow trait default: every platform without a render_to_image override refuses the capture.",
		WindowRenderToImage:    false,
		WindowRenderToImageCfg: `any(test, feature = "test-support")`,
		HeadlessRenderer:       false,
		HeadlessRendererNote:   "The trait exists but the default profile ships no implementation.",
		Windowless:             false,
		SourceSupportedErrors: []string{
			"render_to_image not implemented for this platform (the trait default's bail)",
		},
		PortStatus:  HeadlessPortNotApplicable,
		PortPath:    "Not applicable: the Windows port always carries the native-windows override, so the default refusal is unreachable on the selected platform.",
		MissingWork: []string{},
		Notes: []string{
			"Reference unsupported behavior is a real outcome to reproduce and identify (the conformance contract), not a gap to fill.",
		},
		Sources: []HeadlessSourceRef{
			{File: "crates/gpui/src/platform.rs", Lines: "1099-1106", Note: "the trait method's default body."},
		},
	},
}

// HeadlessProfileRecords returns the pinned headless-API applicability
// trace, one record per profile: what the source supports, its
// source-supported errors, and what this port implements of it — with
// applicable missing work recorded as bounded follow-ups. The returned
// slice is a copy.
func HeadlessProfileRecords() []HeadlessProfileRecord {
	records := make([]HeadlessProfileRecord, len(headlessProfileRecords))
	copy(records, headlessProfileRecords)
	return records
}
