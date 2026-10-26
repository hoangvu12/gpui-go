package gpui

// This file is the port's window frame draw path (ticket11): one frame
// of a window's root view drawn through the full element phase pipeline
// — request_layout, the root stretch, compute_layout, prepaint at the
// root origin, paint — finishing the scene, plus the real-window
// presentation path through the renderer.
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a
// crates/gpui/src/window.rs:
//
//   - Window::draw — set up the draw, draw_roots, clear the layout
//     engine, finish the scene and swap the rendered frame;
//   - Window::draw_roots — "window roots fill the window when their size
//     is auto": request the root element, stretch_auto_size_to_fill the
//     root node to the viewport, prepaint as root at the origin, then
//     paint;
//   - Window::present / present_if_needed — submitting the rendered
//     scene to the platform window.
//
// Bounded deviations, recorded honestly:
//
//   - The renderer slice (ticket07) submits clear+present frames only;
//     drawing a scene into a surface is not part of the landed native
//     renderer service yet. PresentWindow therefore presents the drawn
//     frame as a clear-colored submission through the real GPU ledger
//     (acceptance → poll → retire), and the scene itself is observable
//     through DrawWindowFrame's returned scene and the window's
//     last-scene/debug-bounds/primitive-count accessors.
//   - The layout engine is reset at the START of each frame instead of
//     the end (the reference clears it at the end of draw); both keep one
//     native engine per window and a fresh tree per frame.

import (
	"fmt"
	"sort"
	"time"
)

// activeFrame is the window whose frame is being constructed (the
// "active foreground frame-construction scope" of the resolved ownership
// contract: ViewOf retains into this window's scope while it is set).
var activeFrame *Window

// activeFrameWindow returns the window whose frame is being drawn, or
// nil outside a draw.
func activeFrameWindow() *Window { return activeFrame }

// DrawWindowFrame draws one frame of the window's root view through the
// full element phase pipeline and returns the finished scene (the
// reference Window::draw + draw_roots). Windows enrolled in the frame
// loop (frameloop.go) are drawn by their frame requests; an explicit call
// remains valid and leaves the frame awaiting presentation. The frame's paint operations go
// into a fresh scene; the scene is finished (sorted, plan compiled) and
// becomes the window's last drawn scene; per-window retained element
// state survives across frames keyed by element identity paths.
//
// The draw runs inside one application update: notifications emitted
// while rendering flush when the frame completes, like the reference
// update_window-wrapped draws.
//
// Scene lifetime (gpui-perf ticket 07): the returned scene is the
// window's last drawn scene. It stays valid until the window's next
// completed frame replaces it or the window closes; it is then disposed
// and every read reports ErrSceneClosed. Read or present it before the
// next frame, or take LastDrawnScene afresh.
func DrawWindowFrame(w *Window) (*Scene, error) {
	if w == nil {
		return nil, fmt.Errorf("gpui: DrawWindowFrame requires a window")
	}
	ds := drawState(w)
	if ds.root == nil {
		return nil, fmt.Errorf("gpui: DrawWindowFrame: the window has no root view")
	}
	if ds.frame != nil {
		return nil, fmt.Errorf("gpui: DrawWindowFrame: a frame is already being drawn for this window")
	}

	// Resolve the window's draw geometry: test windows carry it in the
	// draw state (deterministic); real windows read the live host lease.
	viewport, scale, err := windowDrawGeometry(w)
	if err != nil {
		return nil, fmt.Errorf("gpui: DrawWindowFrame: %w", err)
	}
	ds.viewport, ds.scale = viewport, scale
	if ds.text == nil {
		// Real windows draw through the port's real text stack; test
		// windows install their deterministic system at creation.
		text, err := windowTextSystemOf()
		if err != nil {
			return nil, fmt.Errorf("gpui: DrawWindowFrame: text system: %w", err)
		}
		ds.text = text
	}
	if w.app != nil {
		// D11 (browser-core 28, text_scale.go): the window's rem follows
		// the Windows text size by default — the base rem (16) scaled by
		// the app's effective text scale — with SetRemFollowsSystemText
		// Scale(false) holding it at 16. The scale comes from the
		// preference-version cache, so a frame never calls into the host
		// for it; a record change repaints (system_prefs_windows.go) and
		// this frame's rebuilt layout tree reflows at the new rem.
		ds.rem = w.app.frameRemSize()
	}

	scene, err := NewScene()
	if err != nil {
		return nil, fmt.Errorf("gpui: DrawWindowFrame: scene: %w", err)
	}
	// invalidate_entities (window.rs:3161, 3316-3322): the views notified
	// since the last draw, and their ancestor views, are dirty for this
	// frame's cached views (view_cache.go).
	invalidateEntities(w, ds)
	// One fresh layout tree per frame: the reference reuses the window's
	// engine and clears it every frame; the port resets the engine at
	// frame start (every outstanding LayoutID of the previous frame
	// becomes stale, which is safe: layout ids are frame-scoped).
	if err := ds.engine.Reset(); err != nil {
		return nil, fmt.Errorf("gpui: DrawWindowFrame: engine reset: %w", err)
	}
	// A fresh dispatch frame for the element prepaint registrations
	// (focus.go/key_dispatch.go): the completed tree and tab stops swap
	// into the rendered state when the frame completes.
	beginDispatchFrame(w)
	// The frame loop's rendered-view collection (Base ticket 03,
	// frameloop.go): the views rendered by this frame become the
	// window's tracked entities when it completes.
	beginFrameTracking(w)

	baseMask := Bounds{Origin: Point{}, Size: viewport}
	paint := NewPaintContext(scale, baseMask, 1.0)
	// The frame's text paint mode input: a non-opaque window background
	// forces grayscale text (the pin's should_use_subpixel_rendering
	// first check, window.rs:4926-4931; ticket 06). Test windows are
	// opaque (the reference test window's default); a real window's
	// appearance is read once per frame from the live host record, and
	// a closed-host race falls back to the Opaque default rather than
	// failing the frame.
	paint.Background = windowDrawBackground(w)
	frame := &frameDrawState{
		scene:         scene,
		paint:         paint,
		viewport:      viewport,
		scale:         scale,
		rem:           ds.rem,
		contentMask:   baseMask,
		elementOffset: Point{},
		text:          ds.text,
	}
	ds.frame = frame
	previousActive := activeFrame
	activeFrame = w
	var drawErr error
	completed := false
	// The frame draw's duration (window.rs draw's frame_dirty →
	// window_profiler.end_draw measurement; ticket27's overlay
	// records it as one sample at completion).
	drawStart := time.Now()
	defer func() {
		activeFrame = previousActive
		ds.frame = nil
		// An abandoned build preserves the prior published frame: the
		// completed observables (scene, debug bounds, inline facts,
		// dispatch tree, element-state retention, the draw counter)
		// swap only when the frame completed — a returned error OR a
		// panicking element build leaves everything at the previous
		// frame; application mutations made during the failed build are
		// NOT rolled back.
		if completed {
			completeFrame(w, ds, frame, drawStart)
		} else {
			// The abandoned frame's scene is never published: free it
			// (gpui-perf ticket 07).
			_ = scene.Dispose()
			discardDispatchFrame(w)
			abandonFrameTracking(w)
			// The abandoned build may have rewritten cached views' ranges
			// against the discarded frame: the next frame renders every
			// cached view afresh (view_cache.go) AND the damage state is
			// untrustworthy — the next frame is a full redraw (damage.go).
			ds.dirtyViews = nil
			ds.refreshing = true
			ds.lastDamageValid = false
		}
	}()

	app := w.app
	app.Update(func(app *App) {
		// Entity accesses are tracked while the element phases run, not
		// during the flush that closes this update (frameloop.go).
		frame.trackingAccess = true
		defer func() { frame.trackingAccess = false }()
		drawErr = drawRoots(w, app, ds, frame)
		// self.dirty_views.clear() after draw_roots (window.rs:3197).
		ds.dirtyViews = nil
	})
	if drawErr != nil {
		return nil, drawErr
	}
	// browser-core 58: compute the frame's damage — the build's
	// replayed ranges (the reused view ranges at the same bounds and
	// mask) are the undamaged regions; everything else damages its old
	// and new bounds — and tint the paint-flashing overlay, both before
	// the scene's finish: the overlay's tints must not enter this
	// frame's damage, and the previous scene is still alive here
	// (completeFrame disposes it once the frame publishes).
	frame.damageRects, frame.damageFull = computeFrameDamage(w, ds, frame, scene)
	if err := paintDamageFlashingOverlay(ds, frame, scene, frame.damageRects, frame.damageFull); err != nil {
		drawErr = fmt.Errorf("gpui: DrawWindowFrame: %w", err)
		return nil, drawErr
	}
	if err := scene.Finish(); err != nil {
		drawErr = fmt.Errorf("gpui: DrawWindowFrame: scene finish: %w", err)
		return nil, drawErr
	}
	completed = true
	return scene, nil
}

// finishingFrames counts frame completions in progress (completeFrame):
// the test flush does not start another draw while one is finishing.
var finishingFrames int

// completeFrame publishes a completed frame (the tail of Window::draw,
// window.rs:3233-3290). While it runs the window still counts as drawing
// (isDrawing), so invalidations raised by the frame-end work are ignored
// like the reference's draw phase; the frame is then published as clean,
// and a focus move made by a focus listener refreshes the window
// afterwards (window.rs:3279-3285) so that frame is not lost.
func completeFrame(w *Window, ds *windowDrawState, frame *frameDrawState, drawStart time.Time) {
	// The frame's line-layout cache rotation (window.rs:3230
	// self.text_system().finish_frame(), the tail of Window::draw after
	// draw_roots): the shaped lines this frame requested survive into
	// the next frame's previous-frame cache; the lines no frame
	// requested release their native handles. An abandoned build does
	// not finish the frame, like the reference: its entries stay for the
	// next completed frame's rotation (nothing leaks either way).
	if ds.text != nil {
		ds.text.finishFrame()
	}
	var focusMoved bool
	func() {
		ds.finishing = true
		finishingFrames++
		defer func() {
			ds.finishing = false
			finishingFrames--
		}()
		ds.draws++
		ds.lastDebugBounds = frame.debugBounds
		ds.lastAnchorBounds = frame.anchorBounds
		ds.lastInvokerPaths = frame.invokerPaths
		// The replaced scene is freed (gpui-perf ticket 07): this frame's
		// cached views replayed from it during the draw (view_cache.go
		// reusePaint), which has finished, and every reader takes the
		// last scene afresh (LastDrawnScene's documented lifetime).
		if previous := ds.lastScene; previous != nil && previous != frame.scene {
			_ = previous.Dispose()
		}
		ds.lastScene = frame.scene
		// browser-core 58: publish the frame's damage state — the merged
		// rects the present passes to Surface.DrawSceneDamage through
		// Window.LastFrameDamage / PresentFrame, and the geometry keys the
		// next frame's rule compares (damage.go).
		ds.lastDamageRects = frame.damageRects
		ds.lastDamageFull = frame.damageFull
		ds.lastDamageValid = true
		ds.lastDamageViewport = frame.viewport
		ds.lastDamageScale = frame.scale
		ds.lastDamageBackground = frame.paint.Background
		ds.lastInlineFacts = frame.inlineFacts
		ds.lastAutoScrollbarFacts = frame.autoScrollbarFacts
		ds.lastDeferredPriorities = deferredPrioritiesOf(frame)
		// The rendered frame's ordered lists a cached view's reused ranges
		// index into (view_cache.go; the pin keeps them on rendered_frame).
		ds.lastAccessedElementStateLog = frame.accessedElementStateLog
		ds.lastDeferredDraws = frame.deferredDraws
		evicted := retainElementStates(ds, frame.accessedElementStates)
		// The frame-finish hook (helpers_frame.go): the completed dispatch
		// frame swaps into the rendered state through endDispatchFrame (the
		// focus/dispatch runtime reads it for input dispatch), the evicted
		// element states release and the focus listeners run.
		focusMoved = finishFrame(w, evicted)
		// Publish the inspector tree of the completed frame and swap its
		// hitbox map into the window state (ticket27; window.rs
		// Frame::finish + mem::swap(next_frame, rendered_frame) carry the
		// inspector members over). An abandoned build keeps the previously
		// published tree.
		inspectorFinishFrame(w, frame, ds.draws)
		// Record the draw duration as one overlay sample (window.rs
		// 3291-3298, cfg(feature = "profiler") → the port's
		// CapFrameOverlay capability).
		RecordDebugFrame(w, time.Since(drawStart))
	}()
	// The frame loop: tracked entities swap in, the window is clean and the
	// frame awaits presentation (window.rs 3276/3287).
	ds.refreshRequested = false
	completeFrameTracking(w, ds.root)
	// self.refreshing = false (window.rs:3278).
	ds.refreshing = false
	// reset_cursor_style after recording the accessed entities
	// (window.rs 3276-3277; Base ticket 06's interact.go helper): the
	// completed frame's cursor requests resolve the window cursor.
	w.resetCursorStyle(w.app)
	if focusMoved {
		w.Refresh()
	}
	// browser-core 36: an auto scrollbar that appeared or disappeared
	// this frame re-dirties the window (the notify raised during the
	// draw is ignored for the dirty marking, frameloop.go's
	// invalidateViewWindows draw-phase guard); the next frame requests
	// layout with the updated reservation flags — the one-frame settle
	// documented in scrollbar.go.
	if ds.autoScrollbarAppearanceChanged {
		ds.autoScrollbarAppearanceChanged = false
		w.markFrameDirty()
	}
}

// drawRoots is the reference Window::draw_roots: request the root
// element's layout, stretch auto-sized roots to fill the viewport,
// compute the layout, prepaint as root at the origin, then paint.
// Ticket27's bounded integration (window.rs 3398-3481): while the
// window's inspector is on, the root's width shrinks by the 30rem
// inspector panel, the inspector UI element (the app renderer's)
// prepaints after the root and paints after the root, the hovered
// pick highlight paints after that, and the debug frame overlay
// paints into the frame's scene (window.rs 3187-3197, the
// cfg(feature = "profiler") call site). Base ticket 08: the deferred
// draws prepaint after the inspector (window.rs 3436) and paint after it
// above a raised order floor (3470, deferred.go); the active drag's
// preview prepaints after them (3451-3456) and paints after the deferred
// draws (3474-3475).
func drawRoots(w *Window, app *App, ds *windowDrawState, frame *frameDrawState) error {
	root := viewElementOf(ds.root)
	// browser-core 76 (spec D3): the root element's request walk is the
	// window's initial-containing-block stand-in. Chromium's LayoutView
	// is always a fixed — and therefore absolute — container
	// (ComputeIsFixedContainer's IsA<LayoutView> arm,
	// core/layout/layout_object.cc:2035-2041), so the root opens its
	// containing-block scope even when its own style is static: a
	// static chain with no positioned ancestor resolves against the
	// root node (the ICB) instead of falling back to the direct parent.
	// The flag is consumed by the root's own scope open (or discarded by
	// a non-participating root's plain request; containingblock.go).
	frame.icbPending = true
	layoutID := root.RequestLayout(w, app)
	frame.icbPending = false

	// The root size: the viewport, shrunk by the inspector panel's
	// width when the window's inspector is on (window.rs 3403-3415:
	// rems(30.0).to_pixels(rem_size), clamped at zero).
	rootSize := frame.viewport
	if shrink := inspectorRootShrink(w, frame.rem); shrink > 0 {
		rootSize.Width = rootSize.Width - shrink
		if rootSize.Width < 0 {
			rootSize.Width = 0
		}
	}

	// "Window roots fill the window when their size is auto": the
	// reference stretches the ROOT NODE's style through the layout
	// engine. The port asks the root element's node style through the
	// drawable's report (the engine cannot query node styles).
	if style, ok := root.obj.rootStretchStyle(); ok {
		if err := StretchAutoSizeToFill(w, layoutID, style, rootSize); err != nil {
			return fmt.Errorf("root stretch: %w", err)
		}
	}

	available := AvailableSize{
		Width:  DefiniteAvailableSpace(rootSize.Width),
		Height: DefiniteAvailableSpace(rootSize.Height),
	}
	if err := computeLayoutOf(w, layoutID, available); err != nil {
		return fmt.Errorf("root layout compute: %w", err)
	}
	// Browser-core 20 (spec D7): the window's selection gestures install
	// before the root's phases run, so every element that prepaints or
	// paints after this point — the root tree, the overlays, the deferred
	// draws — registers its participants and mouse listeners inside a
	// window that already has its selection state, and the selection
	// listeners sit outermost in the frame's listener order.
	installWindowTextSelection(w, app)
	WithAbsoluteElementOffsetVoid(w, Point{}, func(w *Window) {
		root.Prepaint(w, app)
	})
	// prepaint_inspector (window.rs 3429-3437): lay the inspector UI
	// element out in the panel region after the root's prepaint.
	inspectorElement := prepaintInspectorElement(w, app, frame.viewport)

	// browser-core ticket 24: the window's open tooltips prepaint as
	// roots here too (the inspector element's pattern) — after the app
	// tree, so their anchor bounds cells are current, and before the
	// deferred round, so the entries' deferred composites join it and
	// paint at their layer priorities in paintDeferredDraws
	// (tooltip.go prepaintTooltipElements).
	tooltipLayers := prepaintTooltipElements(w, app, frame.viewport)

	// prepaint_deferred_draws (window.rs 3436; Base ticket 08,
	// deferred.go).
	prepaintDeferredDraws(w, app)
	// browser-core ticket 24: the deferred round's hitboxes (the
	// window-rendered tooltips' surfaces, and every deferred overlay's
	// content) join the paint-time hit test now, before their paints
	// read it (interact.go refreshPaintHitTestAfterDeferred).
	refreshPaintHitTestAfterDeferred(w)

	// The active drag's preview prepaints as a root at
	// mouse_position - cursor_offset (window.rs 3451-3456; the port has no
	// in-window prompt and no tooltips, the branches before and after it).
	// The port's drawables re-resolve their bounds at paint, so the same
	// absolute offset is re-applied around the preview's paint.
	var dragOffset Point
	if drag := app.ActiveDrag(); drag != nil && drag.View != nil {
		mouse := w.TrackedMousePosition()
		dragOffset = Point{X: mouse.X - drag.CursorOffset.X, Y: mouse.Y - drag.CursorOffset.Y}
	}
	// Browser-core 20: the drag preview's tree draws under selection
	// suppression — Chrome's drag image is a snapshot, not hit-testable
	// DOM, so its text never joins the window selection.
	dragElement, hasDragElement := func() (AnyElement, bool) {
		type pair struct {
			element AnyElement
			has     bool
		}
		p := WithUserSelectSuppressed(w, func(w *Window) pair {
			element, has := prepaintActiveDragPreview(w, app)
			return pair{element, has}
		})
		return p.element, p.has
	}()

	root.Paint(w, app)

	// paint_inspector (window.rs 3467-3468), paint_deferred_draws (3470),
	// the drag preview (3474-3475), then the hovered pick highlight
	// (paint_inspector_hitbox, 3480-3482). The tooltips' registrations
	// paint with the inspector's (their composites paint in the
	// deferred round below, tooltip.go paintTooltipElements).
	paintInspectorElement(w, app, inspectorElement)
	paintTooltipElements(w, app, tooltipLayers)
	paintDeferredDraws(w, app)
	if hasDragElement {
		WithAbsoluteElementOffsetVoid(w, dragOffset, func(w *Window) {
			WithUserSelectSuppressedVoid(w, func(w *Window) {
				paintActiveDragPreview(dragElement, w, app)
			})
		})
	}
	paintInspectorHighlight(w, app)

	// Browser-core 20: apply the frame's collected automatic selection
	// registrations — the element phases are complete, so the pending
	// list is full; one entity update, before the frame completes and
	// before any input dispatch reads the participants.
	flushUserSelectionRegistrations(w, app)

	// The debug frame overlay (window.rs 3187-3197: painted after
	// draw_roots, into the frame under construction, with the viewport
	// size and scale factor; cfg(feature = "profiler") → the port's
	// CapFrameOverlay capability).
	if err := paintDebugFrameOverlay(w, frame); err != nil {
		return fmt.Errorf("debug frame overlay: %w", err)
	}
	return nil
}

// windowDrawGeometry resolves the window's viewport size and scale
// factor: test windows carry the deterministic profile (scale 2.0, the
// reference test window's hardcoded factor), real windows read the live
// host lease.
func windowDrawGeometry(w *Window) (Size, float32, error) {
	ds := drawState(w)
	if ds.testWindow {
		return ds.viewport, ds.scale, nil
	}
	bounds, err := w.Bounds()
	if err != nil {
		return Size{}, 0, err
	}
	scale, err := w.ScaleFactor()
	if err != nil {
		return Size{}, 0, err
	}
	return bounds.Size, scale, nil
}

// windowDrawBackground resolves the window's background appearance for
// the frame's text paint mode (ticket 06): test windows are opaque (the
// reference test window's background_appearance default), real windows
// read the live host record; an unreadable host (a closed-window race)
// falls back to Opaque, the platform default, never failing the frame.
func windowDrawBackground(w *Window) WindowBackgroundAppearance {
	ds := drawState(w)
	if ds.testWindow {
		return WindowBackgroundOpaque
	}
	appearance, err := w.BackgroundAppearance()
	if err != nil {
		return WindowBackgroundOpaque
	}
	return appearance
}

// PresentWindow presents the window's last drawn frame through the
// renderer (the reference Window::present submitting the rendered
// scene). This slice's native renderer submits clear+present frames, so
// the submission presents the clear color while the scene is observable
// through DrawWindowFrame's return value; the submission is tracked,
// polled and retired through the real GPU ledger exactly like the
// renderer's own present path.
func PresentWindow(w *Window, renderer *Renderer, color Color) (*Submission, error) {
	if renderer == nil {
		return nil, fmt.Errorf("gpui: PresentWindow requires a renderer")
	}
	if w == nil {
		return nil, fmt.Errorf("gpui: PresentWindow requires a window")
	}
	ds, ok := windowDrawStates[w]
	if !ok || ds.lastScene == nil {
		return nil, fmt.Errorf("gpui: PresentWindow: the window has no drawn frame to present")
	}
	return renderer.PresentClear(w, color)
}

// WindowDebugBounds returns the last completed frame's debug-selector
// bounds map (the reference rendered_frame.debug_bounds through the
// VisualTestContext accessors).
func WindowDebugBounds(w *Window) map[string]Bounds {
	ds, ok := windowDrawStates[w]
	if !ok {
		return nil
	}
	return ds.lastDebugBounds
}

// WindowDebugBound returns one debug-selector bounds of the last
// completed frame.
func WindowDebugBound(w *Window, selector string) (Bounds, bool) {
	bounds, ok := WindowDebugBounds(w)[selector]
	return bounds, ok
}

// LastDrawnScene returns the window's last completed frame's finished
// scene (the reference rendered_frame.scene; painted_quads and the
// primitive counts are observable through it).
// It is valid until the window's next completed frame or its close (see
// DrawWindowFrame's scene lifetime); callers must not dispose it.
func LastDrawnScene(w *Window) *Scene {
	ds, ok := windowDrawStates[w]
	if !ok {
		return nil
	}
	return ds.lastScene
}

// GlyphAtlas returns the window's sprite atlas: the atlas its text
// system rasterized every frame's glyph sprites into, and the atlas the
// window's img() and svg() sprites are painted into as well (the pin's
// one per-window sprite_atlas; see spriteAtlas). It is the element
// pipeline's per-window atlas; the reference's platform window owns it
// behind the renderer.
//
// A consumer-owned present loop passes it to Surface.DrawScene and
// Surface.RenderScene whenever it presents LastDrawnScene(w): the
// scene's sprite batches resolve their textures against exactly this
// atlas, and a draw that does not receive it fails with the typed
// ErrSceneSpriteAtlas (a nil atlas presents only sprite-free scenes,
// which is why sprite-free windows present unchanged). The atlas is
// created with the window's text system (lazily, on first use), stays
// the same for the window's lifetime and is rebuilt after device loss
// by the renderer's recovery (the atlas invalidation + forced redraw).
//
// Like every window accessor it must be called on the foreground
// (host) thread. Deterministic test windows never rasterize glyphs,
// so they answer ErrNoGlyphAtlas.
func (w *Window) GlyphAtlas() (*Atlas, error) {
	if w == nil {
		return nil, fmt.Errorf("gpui: GlyphAtlas requires a window")
	}
	ds, ok := windowDrawStates[w]
	if !ok {
		return nil, fmt.Errorf("gpui: GlyphAtlas: the window has no element runtime")
	}
	if ds.text == nil {
		// The same lazy creation the frame draw and Window.ShapeText
		// use: a present loop may ask before the first frame.
		text, err := windowTextSystemOf()
		if err != nil {
			return nil, fmt.Errorf("gpui: GlyphAtlas: text system: %w", err)
		}
		ds.text = text
	}
	rts, ok := ds.text.(*realWindowTextSystem)
	if !ok || rts.atlas == nil {
		return nil, fmt.Errorf("gpui: GlyphAtlas: %w (deterministic test windows shape no glyph sprites)", ErrNoGlyphAtlas)
	}
	return rts.atlas, nil
}

// spriteAtlas returns the window's sprite atlas: the ONE atlas the pin
// paints every sprite kind into — glyphs (window.rs:4865), SVG masks
// (window.rs:5009) and image frames (window.rs:5104) all go through
// self.sprite_atlas, the platform window's renderer atlas
// (window.rs:1541; gpui_windows window.rs:1113 → directx_renderer.rs:387),
// and the renderer resolves every sprite batch's texture through that
// same atlas (directx_renderer.rs:1133-1170 draw_*_sprites →
// self.atlas.get_texture_view). The port's per-window atlas is the one
// the window's text system owns (GlyphAtlas), so a present loop passing
// GlyphAtlas() draws image and SVG sprites too (Base ticket 40: images
// painted into the application's image atlas failed at present with
// "polychrome sprite batch needs texture 0").
//
// Deterministic test windows own no window atlas (they rasterize no
// glyphs and never present): their image and SVG sprites go into the
// application image atlas, where the test-support observers read them.
func (w *Window) spriteAtlas() (*Atlas, error) {
	ds, ok := windowDrawStates[w]
	if ok {
		text := ds.text
		if ds.frame != nil && ds.frame.text != nil {
			text = ds.frame.text
		}
		if rts, ok := text.(*realWindowTextSystem); ok && rts.atlas != nil {
			return rts.atlas, nil
		}
	}
	if atlas := w.app.ImageAtlas(); atlas != nil {
		return atlas, nil
	}
	return nil, fmt.Errorf("gpui: the window has no sprite atlas (the application image atlas is unavailable)")
}

// windowSpriteAtlases returns the sprite atlases of the application's
// open real windows (the pinned App::drop_image walk over
// self.windows, app.rs:2815-2825), in window id order.
func (a *App) windowSpriteAtlases() []*Atlas {
	ids := make([]WindowID, 0, len(a.windows))
	for id := range a.windows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var atlases []*Atlas
	for _, id := range ids {
		ds, ok := windowDrawStates[a.windows[id]]
		if !ok {
			continue
		}
		if rts, ok := ds.text.(*realWindowTextSystem); ok && rts.atlas != nil {
			atlases = append(atlases, rts.atlas)
		}
	}
	return atlases
}

// WindowPrimitiveCounts returns the current frame's quad and glyph
// sprite counts (the reference Window::rendered_primitive_counts:
// quads, monochrome sprites, subpixel sprites, polychrome sprites).
func WindowPrimitiveCounts(w *Window) (quads, monochrome, subpixel, polychrome int, err error) {
	scene := LastDrawnScene(w)
	if scene == nil {
		return 0, 0, 0, 0, fmt.Errorf("gpui: WindowPrimitiveCounts: the window has no drawn frame")
	}
	quads_, err := scene.Quads()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	mono, err := scene.MonochromeSprites()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	sub, err := scene.SubpixelSprites()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	poly, err := scene.PolychromeSprites()
	if err != nil {
		return 0, 0, 0, 0, err
	}
	return len(quads_), len(mono), len(sub), len(poly), nil
}

// WindowDrawCount returns how many frames this window has drawn (render
// lifecycle evidence).
func WindowDrawCount(w *Window) uint64 {
	ds, ok := windowDrawStates[w]
	if !ok {
		return 0
	}
	return ds.draws
}

// RemoveWindowView closes the window's element runtime: the root view is
// dropped and the retained element states are released (the reference
// window removal drops the window's frame state; the window itself is
// removed from the application registry through the existing close
// path).
func RemoveWindowView(w *Window) {
	if w == nil {
		return
	}
	dropDrawState(w)
}
