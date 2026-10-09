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
// reference Window::draw + draw_roots). The frame's paint operations go
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

	baseMask := Bounds{Origin: Point{}, Size: viewport}
	frame := &frameDrawState{
		scene:             scene,
		paint:             NewPaintContext(scale, baseMask, 1.0),
		viewport:          viewport,
		scale:             scale,
		rem:               ds.rem,
		contentMask:       baseMask,
		elementOffset:     Point{},
		text:              ds.text,
		inlineShapedLines: make(map[*WrappedLine]struct{}),
	}
	ds.frame = frame
	previousActive := activeFrame
	activeFrame = w
	var drawErr error
	completed := false
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
			ds.draws++
			ds.lastDebugBounds = frame.debugBounds
			ds.lastScene = scene
			ds.lastInlineFacts = frame.inlineFacts
			retainElementStates(ds, frame.accessedElementStates)
			// Swap the completed dispatch frame into the rendered state
			// (the focus/dispatch runtime reads it for input dispatch).
			endDispatchFrame(w)
			ds.refreshRequested = false
		} else {
			discardDispatchFrame(w)
		}
	}()

	app := w.app
	app.Update(func(app *App) {
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

// drawRoots is the reference Window::draw_roots: request the root
// element's layout, stretch auto-sized roots to fill the viewport,
// compute the layout, prepaint as root at the origin, then paint.
func drawRoots(w *Window, app *App, ds *windowDrawState, frame *frameDrawState) error {
	root := viewElementOf(ds.root)
	layoutID := root.RequestLayout(w, app)

	// "Window roots fill the window when their size is auto": the
	// reference stretches the ROOT NODE's style through the layout
	// engine. The port asks the root element's node style through the
	// drawable's report (the engine cannot query node styles).
	if style, ok := root.obj.rootStretchStyle(); ok {
		if err := StretchAutoSizeToFill(w, layoutID, style, frame.viewport); err != nil {
			return fmt.Errorf("root stretch: %w", err)
		}
	}

	available := AvailableSize{
		Width:  DefiniteAvailableSpace(frame.viewport.Width),
		Height: DefiniteAvailableSpace(frame.viewport.Height),
	}
	if err := computeLayoutOf(w, layoutID, available); err != nil {
		return fmt.Errorf("root layout compute: %w", err)
	}
	WithAbsoluteElementOffsetVoid(w, Point{}, func(w *Window) {
		root.Prepaint(w, app)
	})
	root.Paint(w, app)
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
