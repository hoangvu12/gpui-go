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

	scene, err := NewScene()
	if err != nil {
		return nil, fmt.Errorf("gpui: DrawWindowFrame: scene: %w", err)
	}
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
	frame := &frameDrawState{
		scene:         scene,
		paint:         NewPaintContext(scale, baseMask, 1.0),
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
			discardDispatchFrame(w)
			abandonFrameTracking(w)
		}
	}()

	app := w.app
	app.Update(func(app *App) {
		// Entity accesses are tracked while the element phases run, not
		// during the flush that closes this update (frameloop.go).
		frame.trackingAccess = true
		defer func() { frame.trackingAccess = false }()
		drawErr = drawRoots(w, app, ds, frame)
	})
	if drawErr != nil {
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
		ds.lastScene = frame.scene
		ds.lastInlineFacts = frame.inlineFacts
		ds.lastDeferredPriorities = deferredPrioritiesOf(frame)
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
	// reset_cursor_style after recording the accessed entities
	// (window.rs 3276-3277; Base ticket 06's interact.go helper): the
	// completed frame's cursor requests resolve the window cursor.
	w.resetCursorStyle(w.app)
	if focusMoved {
		w.Refresh()
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
	layoutID := root.RequestLayout(w, app)

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
	WithAbsoluteElementOffsetVoid(w, Point{}, func(w *Window) {
		root.Prepaint(w, app)
	})
	// prepaint_inspector (window.rs 3429-3437): lay the inspector UI
	// element out in the panel region after the root's prepaint.
	inspectorElement := prepaintInspectorElement(w, app, frame.viewport)

	// prepaint_deferred_draws (window.rs 3436; Base ticket 08,
	// deferred.go).
	prepaintDeferredDraws(w, app)

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
	dragElement, hasDragElement := prepaintActiveDragPreview(w, app)

	root.Paint(w, app)

	// paint_inspector (window.rs 3467-3468), paint_deferred_draws (3470),
	// the drag preview (3474-3475), then the hovered pick highlight
	// (paint_inspector_hitbox, 3480-3482).
	paintInspectorElement(w, app, inspectorElement)
	paintDeferredDraws(w, app)
	if hasDragElement {
		WithAbsoluteElementOffsetVoid(w, dragOffset, func(w *Window) {
			paintActiveDragPreview(dragElement, w, app)
		})
	}
	paintInspectorHighlight(w, app)

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
func LastDrawnScene(w *Window) *Scene {
	ds, ok := windowDrawStates[w]
	if !ok {
		return nil
	}
	return ds.lastScene
}

// GlyphAtlas returns the atlas that holds the window's glyphs: the
// atlas its text system rasterized every frame's glyph sprites into
// (the element pipeline's per-window atlas; the reference's window
// text system owns the atlas behind the platform renderer).
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
