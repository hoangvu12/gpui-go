//go:build windows

package textframespec

import (
	"fmt"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// This file is ticket 02's real-window spec (the consumer-gaps effort,
// .scratch/gpui-consumer-gaps/issues/02-text-shaping-handle-leak.md):
// the shaped-line lifetime across window frames — the window's
// line-layout cache (gpui/text_cache.go, the reference LineLayoutCache
// port) keeps a redrawing window's native shaping handles bounded
// instead of re-shaping every text on every frame.
//
// The failing behavior it pins out (mordbard's readiness spike,
// 2026-10-19): 17 forced redraws of a 7-Text window exhausted the
// native live-handle bound (256, MaxShapingHandles) — every further
// frame failed in layout with "gpui: text element shaping failed:
// ErrTextShapingLimit" and a fresh ShapeText itself returned the limit
// error. The environment expectation follows the winhostspec /
// inlinespec real-window pattern: this session creates real windows —
// failures fail the tests, never skip.
//
// The live-handle observable is TextSystem.LiveShapingHandles (the
// Go-side mirror of the native service's live-handle count; the native
// ABI exposes only its bound). Both real shaped paths run through the
// corpus: the animation test drives the text element's own measure
// path (Flex children), the list test drives the inline paragraph path
// (Block rows), and the resize test drives wrap-width re-shaping with
// the eviction of the superseded lines.

// bootTextFrameWindow opens a shown app window on a started host (the
// inlinespec real-window boot pattern) with the given root view
// builder.
func bootTextFrameWindow(t *testing.T, title string, w, h float32, build func(*gpui.Window, *gpui.App) gpui.View) (*gpui.App, *gpui.Host, *gpui.Window) {
	t.Helper()
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		<-runDone
	})
	var window *gpui.Window
	var openErr error
	sync := func(f func()) {
		done := make(chan struct{})
		if !host.Post(func() {
			defer close(done)
			f()
		}) {
			t.Fatalf("foreground post was dropped: the host loop exited")
		}
		<-done
	}
	sync(func() {
		window, openErr = app.OpenWindowView(gpui.WindowOptions{
			Title:     title,
			Show:      true,
			Resizable: true,
			Bounds:    gpui.Bounds{Size: gpui.Size{Width: w, Height: h}},
		}, build)
	})
	if openErr != nil {
		t.Fatalf("app.OpenWindowView(%q): %v — this session is expected to create real windows", title, openErr)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			waitTextFrame(t, 10*time.Second, "the window to be destroyed", func() bool { return !window.Alive() })
		}
	})
	return app, host, window
}

// waitTextFrame polls a condition until the timeout (fail, never skip).
func waitTextFrame(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out after %v waiting for %s", timeout, what)
}

// frameSystem returns the process-global text system.
func frameSystem(t *testing.T) *gpui.TextSystem {
	t.Helper()
	system, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("gpui.DefaultTextSystem: %v", err)
	}
	return system
}

// driveFrameRequests delivers count frame requests to the window on
// the foreground thread (the platform frame source's request_frame
// path — the same delivery WM_PAINT performs; the test paces it
// instead of vblank). Each request runs the pending next-frame
// callbacks (an active animation's next-frame request among them) and
// draws the dirty window, so one request is one animation frame.
func driveFrameRequests(t *testing.T, host *gpui.Host, window *gpui.Window, count int) {
	t.Helper()
	// Batches keep the host thread responsive to the OS between
	// stretches of frames (activation and paint messages still land).
	const perBatch = 200
	remaining := count
	for remaining > 0 {
		n := perBatch
		if remaining < n {
			n = remaining
		}
		done := make(chan struct{})
		if !host.Post(func() {
			defer close(done)
			for i := 0; i < n; i++ {
				window.SimulateFrameRequest()
			}
		}) {
			t.Fatalf("foreground post was dropped: the host loop exited")
		}
		<-done
		remaining -= n
	}
}

// drawTextFrame draws one explicit frame of the window on the
// foreground thread (the README's consumer present loop's draw step)
// and fails the test when the draw fails — the leak's failure mode
// surfaces as a shaping error here or as the measure closure's panic.
func drawTextFrame(t *testing.T, host *gpui.Host, window *gpui.Window) {
	t.Helper()
	done := make(chan struct{})
	if !host.Post(func() {
		defer close(done)
		if _, err := gpui.DrawWindowFrame(window); err != nil {
			t.Errorf("DrawWindowFrame: %v", err)
		}
	}) {
		t.Fatalf("foreground post was dropped: the host loop exited")
	}
	<-done
}

// animatedTextView is the animation corpus: a column of text elements
// (the text element's own measure path — the ticket's element path)
// re-rendered by a continuous, never-ending WithAnimation loop.
type animatedTextView struct{}

// animatedTextCount is the corpus's text-element count (the spike's
// 7-Text view shape).
const animatedTextCount = 7

// Render implements Render[animatedTextView].
func (v *animatedTextView) Render(w *gpui.Window, cx *gpui.Context[animatedTextView]) gpui.AnyElement {
	column := gpui.Div().Flex().FlexCol().Gap(gpui.Px(6))
	for i := 0; i < animatedTextCount; i++ {
		column = column.Child(gpui.Text(fmt.Sprintf("animated window text %d — redraws keep reusing this shaped line", i)))
	}
	return gpui.WithAnimation(
		gpui.Div().Flex().SizeFull().Child(column),
		"animated-root",
		gpui.NewAnimation(250*time.Millisecond).Repeat(),
		func(d *gpui.DivElement, _ float32) *gpui.DivElement { return d },
	).IntoElement()
}

// TestTextUnderAnimationRedrawsBounded is the first acceptance
// criterion: a real window with a Text element inside a continuous
// WithAnimation loop runs for at least 10,000 frames without
// ErrTextShapingLimit and without unbounded handle growth. Pre-fix,
// the first ~37 redraws exhaust the 256-handle bound (7 texts per
// frame) and the frame loop dies in layout.
func TestTextUnderAnimationRedrawsBounded(t *testing.T) {
	system := frameSystem(t)
	_, host, window := bootTextFrameWindow(t, "textframespec animation", 420, 320, func(w *gpui.Window, app *gpui.App) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *animatedTextView, cx *gpui.Context[animatedTextView]) {}))
	})

	// Wait for the frame loop's own first draw (a real window starts
	// dirty and the host's vblank invalidation serves it), then record
	// the steady-state handle count the first frame leaves.
	waitTextFrame(t, 10*time.Second, "the window's first frame", func() bool {
		return gpui.WindowDrawCount(window) >= 1
	})
	liveAfterFirst := system.LiveShapingHandles()
	t.Logf("DEBUG liveAfterFirst=%d draws=%d", liveAfterFirst, gpui.WindowDrawCount(window))
	if liveAfterFirst == 0 {
		t.Fatalf("the first frame shaped no handles: LiveShapingHandles = 0, want the frame's text lines")
	}
	// The measure passes shape each text under its three sizing-mode
	// keys: the wrap-0 min-content shape, the unwrapped max-content
	// shape and the wrapped known-width shape (browser-core 32's D2
	// rule: a min-content query wraps at width zero, so it is a distinct
	// native shape — the deliberate cost of the correct min-content,
	// like Chrome's zero-available-inline-size line breaking). All
	// three are re-queried every animation frame, so each is a live
	// shapedLineCache entry at the steady state: three handles per text
	// element, bounded by the two-frame rotation and the native handle
	// bound.
	if want := animatedTextCount * 3; liveAfterFirst > want {
		t.Fatalf("LiveShapingHandles after the first frame = %d, want <= %d (three handles per text: wrap-0, unwrapped and wrapped — browser-core 32)", liveAfterFirst, want)
	}

	// A short run first: a hundred further animation frames must not
	// create a single new native handle — every text element's shape is
	// a cache hit under unchanged inputs (the reuse half of the fix).
	driveFrameRequests(t, host, window, 100)
	if got := system.LiveShapingHandles(); got != liveAfterFirst {
		t.Fatalf("LiveShapingHandles after 100 reuse frames = %d, want %d (unchanged inputs must reuse the shaped lines)", got, liveAfterFirst)
	}

	// The full criterion: 10,000 frames of the continuous animation
	// loop. A shaping-handle leak dies here — pre-fix at frame ~37,
	// every further draw fails with ErrTextShapingLimit and the measure
	// closure's panic kills the frame loop.
	const frames = 10000
	driveFrameRequests(t, host, window, frames)
	draws := gpui.WindowDrawCount(window)
	if draws < frames {
		t.Fatalf("window drew %d frames, want at least %d", draws, frames)
	}

	// Bounded growth: the live-handle count stayed exactly at the
	// steady state (no growth at all under unchanged inputs) and far
	// under the native bound.
	if got, want := system.LiveShapingHandles(), liveAfterFirst; got != want {
		t.Errorf("LiveShapingHandles after %d animation frames = %d, want %d (bounded, no growth)", frames, got, want)
	}
	t.Logf("steady state: %d live shaping handles over %d drawn frames (the corpus's %d texts, two keys each)", liveAfterFirst, gpui.WindowDrawCount(window), animatedTextCount)
	if got, bound := system.LiveShapingHandles(), system.MaxShapingHandles(); got > bound {
		t.Errorf("LiveShapingHandles = %d exceeds the native bound %d", got, bound)
	}

	// The bound itself is still available to a fresh shape: the run
	// exhausted nothing.
	probeText := "a fresh shape after ten thousand frames"
	probe, err := system.ShapeText(gpui.TextLayoutRequest{
		Text:     probeText,
		FontSize: 16,
		Runs:     []gpui.TextRun{{Len: len(probeText), Font: gpui.DefaultFontDescriptor()}},
	})
	if err != nil {
		t.Fatalf("a fresh ShapeText after %d frames: %v (want the capacity intact — no ErrTextShapingLimit)", frames, err)
	}
	if err := probe.Dispose(); err != nil {
		t.Fatalf("probe dispose: %v", err)
	}
}

// scrollingTextView is the list corpus: a 300-row uniform list of text
// rows over a view-held scroll handle the test drives directly.
type scrollingTextView struct {
	handle *gpui.UniformListScrollHandle
}

// scrollingRowCount is the corpus's row count (the acceptance's
// 300-row list).
const scrollingRowCount = 300

// scrollingRowHeight is the fixed row pitch (logical pixels).
const scrollingRowHeight = 28

// scrollingRowText is one row's text — distinct per row, so rows
// entering the visible range shape fresh lines and rows leaving it
// evict theirs.
func scrollingRowText(ix int) string {
	return fmt.Sprintf("row %03d — scrolling list text shaped for the visible range", ix)
}

// Render implements Render[scrollingTextView]. The rows are Block divs
// with text children — the inline paragraph path (the animation test
// above covers the text element's own measure path); both real shaped
// paths run through the window's line-layout cache.
func (v *scrollingTextView) Render(w *gpui.Window, cx *gpui.Context[scrollingTextView]) gpui.AnyElement {
	return gpui.UniformList(v.handle, scrollingRowCount, func(start, end int, w *gpui.Window, app *gpui.App) []gpui.AnyElement {
		rows := make([]gpui.AnyElement, 0, end-start)
		for ix := start; ix < end; ix++ {
			rows = append(rows, gpui.Div().Flex().
				H(gpui.PxLength(scrollingRowHeight)).
				Child(gpui.Text(scrollingRowText(ix))).
				IntoElement())
		}
		return rows
	}).SizeFull().IntoElement()
}

// TestScrollingTextListStaysUnderHandleBound is the second acceptance
// criterion: scrolling a 300-row UniformList of text rows (rows
// entering and leaving the visible range) for thousands of frames
// stays under the live-handle bound. Each frame travels one row, so
// every frame's visible range shifts one row in and one row out,
// cycling through all 300 rows roughly ten times.
func TestScrollingTextListStaysUnderHandleBound(t *testing.T) {
	system := frameSystem(t)
	handle := gpui.NewUniformListScrollHandle()
	_, host, window := bootTextFrameWindow(t, "textframespec scrolling", 420, 340, func(w *gpui.Window, app *gpui.App) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *scrollingTextView, cx *gpui.Context[scrollingTextView]) {
			v.handle = handle
		}))
	})

	// The first draw installs the list and its initial visible range.
	drawTextFrame(t, host, window)
	if system.LiveShapingHandles() == 0 {
		t.Fatalf("the first list frame shaped no handles: LiveShapingHandles = 0")
	}

	// Thousands of scrolled frames: set the handle's offset (the list
	// reads it at request layout; the y convention is <= 0, scroll-down
	// more negative) then draw an explicit frame. The offset cycles
	// through every row over the course of the run.
	const frames = 3000
	const perBatch = 100
	maxLive := system.LiveShapingHandles()
	for base := 0; base < frames; base += perBatch {
		done := make(chan struct{})
		if !host.Post(func() {
			defer close(done)
			for j := 0; j < perBatch; j++ {
				step := base + j + 1
				offsetY := -float32((step % scrollingRowCount) * scrollingRowHeight)
				handle.SetOffset(gpui.Point{X: 0, Y: offsetY})
				if _, err := gpui.DrawWindowFrame(window); err != nil {
					t.Errorf("DrawWindowFrame at scroll step %d: %v", step, err)
					return
				}
			}
		}) {
			t.Fatalf("foreground post was dropped: the host loop exited")
		}
		<-done
		if live := system.LiveShapingHandles(); live > maxLive {
			maxLive = live
		}
	}

	// The bound: the sampled live-handle count stayed under half the
	// native bound. The leak it replaces accumulates one handle per
	// row-frame — 3000 frames of a ~12-row visible range is tens of
	// thousands of live handles, an order past the bound.
	if bound := system.MaxShapingHandles(); maxLive > bound {
		t.Fatalf("max LiveShapingHandles over %d scrolled frames = %d, want <= %d (the bound)", frames, maxLive, bound)
	}
	if bound := system.MaxShapingHandles(); maxLive > bound/2 {
		t.Errorf("max LiveShapingHandles over %d scrolled frames = %d, want <= %d (half the bound — the visible range's lines, not the scroll history)", frames, maxLive, bound/2)
	}
	t.Logf("steady state: max %d live shaping handles over %d scrolled frames (the visible range's rows)", maxLive, frames)
	// A fresh shape still succeeds after the whole scroll.
	probeText := "a fresh shape after three thousand scrolled frames"
	probe, err := system.ShapeText(gpui.TextLayoutRequest{
		Text:     probeText,
		FontSize: 16,
		Runs:     []gpui.TextRun{{Len: len(probeText), Font: gpui.DefaultFontDescriptor()}},
	})
	if err != nil {
		t.Fatalf("a fresh ShapeText after %d scrolled frames: %v (want the capacity intact)", frames, err)
	}
	if err := probe.Dispose(); err != nil {
		t.Fatalf("probe dispose: %v", err)
	}
}

// wrappingTextView is the resize corpus: text elements that wrap under
// the window's width, so every resize changes their wrap widths.
type wrappingTextView struct{}

// wrappingTextCount is the corpus's text-element count.
const wrappingTextCount = 3

// wrappingText is one wrapping text (long enough to wrap at every
// width the test uses).
func wrappingText(ix int) string {
	return fmt.Sprintf("wrapping text %d: the quick brown fox jumps over the lazy dog and keeps jumping over the next lazy dog to fill the line", ix)
}

// Render implements Render[wrappingTextView].
func (v *wrappingTextView) Render(w *gpui.Window, cx *gpui.Context[wrappingTextView]) gpui.AnyElement {
	column := gpui.Div().Flex().FlexCol().Gap(gpui.Px(4))
	for i := 0; i < wrappingTextCount; i++ {
		column = column.Child(gpui.Text(wrappingText(i)))
	}
	return gpui.Div().Flex().SizeFull().Child(column).IntoElement()
}

// TestResizingWindowDoesNotLeakSupersededLines is the third acceptance
// criterion: resizing a window with text (wrap widths changing) does
// not leak the superseded lines — every resize re-shapes the texts
// under the new width, and the lines shaped under the superseded
// widths release at the frame rotations instead of accumulating.
func TestResizingWindowDoesNotLeakSupersededLines(t *testing.T) {
	system := frameSystem(t)
	_, host, window := bootTextFrameWindow(t, "textframespec resize", 400, 300, func(w *gpui.Window, app *gpui.App) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *wrappingTextView, cx *gpui.Context[wrappingTextView]) {}))
	})

	// The first draw records the baseline: the texts' handles at the
	// initial width — three keys per text (browser-core 32's D2 adds
	// the wrap-0 min-content shape next to the unwrapped max-content
	// and the wrapped final-pass shapes; all three are live
	// shapedLineCache entries at the steady state, re-queried by every
	// frame's measure passes).
	drawTextFrame(t, host, window)
	baseline := system.LiveShapingHandles()
	if baseline == 0 {
		t.Fatalf("the first resize-corpus frame shaped no handles: LiveShapingHandles = 0")
	}
	if want := wrappingTextCount * 3; baseline > want {
		t.Fatalf("LiveShapingHandles after the first frame = %d, want <= %d (three handles per text: wrap-0, unwrapped and wrapped — browser-core 32)", baseline, want)
	}

	// Resizing with text: cycle through distinct widths (a distinct
	// wrap width per frame, so every frame re-shapes and supersedes the
	// previous width's lines), waiting for each resize to land before
	// drawing, exactly like a user dragging the resize frame.
	const resizes = 200
	maxLive := baseline
	for i := 1; i <= resizes; i++ {
		width := 240 + float32((i*37)%240) // 240..479, distinct per step
		done := make(chan struct{})
		if !host.Post(func() {
			defer close(done)
			window.SetBounds(gpui.Bounds{Size: gpui.Size{Width: width, Height: 300}})
			// SetBounds runs inline on the host thread (SetWindowPos
			// delivers WM_SIZE synchronously), so the bounds report the
			// new size by the time the frame reads them.
			if _, err := gpui.DrawWindowFrame(window); err != nil {
				t.Errorf("DrawWindowFrame at resize %d (width %v): %v", i, width, err)
			}
		}) {
			t.Fatalf("foreground post was dropped: the host loop exited")
		}
		<-done
		if live := system.LiveShapingHandles(); live > maxLive {
			maxLive = live
		}
	}

	// No leak: through every distinct width, the live count stayed
	// within a small factor of the corpus (the current and previous
	// frame's entries at most — the leak accumulates one full set of
	// lines per distinct width, 200 widths deep).
	if want := wrappingTextCount * 8; maxLive > want {
		t.Errorf("max LiveShapingHandles over %d resizes = %d, want <= %d (the rotating two frames' entries, not the resize history)", resizes, maxLive, want)
	}
	// The superseded lines released: after the last resize frame, the
	// live count is back at the corpus's steady state (its final
	// width's lines), not the accumulated history.
	if got := system.LiveShapingHandles(); got > wrappingTextCount*4 {
		t.Errorf("LiveShapingHandles after the resize loop = %d, want <= %d (the superseded widths' lines released)", got, wrappingTextCount*4)
	}
	t.Logf("steady state: max %d, final %d live shaping handles over %d resizes through distinct widths", maxLive, system.LiveShapingHandles(), resizes)
	if bound := system.MaxShapingHandles(); maxLive > bound {
		t.Fatalf("max LiveShapingHandles = %d exceeds the native bound %d", maxLive, bound)
	}
}
