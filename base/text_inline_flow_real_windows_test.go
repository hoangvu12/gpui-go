//go:build windows

package base

// Real-window slice of ticket 28: the inline flow's re-expressed wrapping
// (the legacy LineWrapper plus shaped spans, U:text/inline_flow.rs:
// 1147-1502) over the native shaping seam with two real families, where a
// code span's family really is wider than the body's. This is the
// property U:text/inline_flow.rs:1827-1933 pins with its WideMono fake,
// checked with the machine's fonts. Environment expectation (the
// richtextspec / inlinespec pattern): this session creates real windows;
// failures fail, never skip.

import (
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

type realProbeView struct {
	run func(w *gpui.Window, app *gpui.App)
}

func (v *realProbeView) Render(w *gpui.Window, cx *gpui.Context[realProbeView]) gpui.AnyElement {
	return gpui.Canvas(func(_ gpui.Bounds, w *gpui.Window, app *gpui.App) struct{} {
		if v.run != nil {
			run := v.run
			v.run = nil
			run(w, app)
		}
		return struct{}{}
	}, func(gpui.Bounds, struct{}, *gpui.Window, *gpui.App) {}).Flex().W(gpui.Px(400)).H(gpui.Px(200)).IntoElement()
}

// inRealPrepaint runs f inside a prepaint pass of a real Win32 window; f
// runs on the host thread and must not call t.Fatal.
func inRealPrepaint(t *testing.T, f func(w *gpui.Window, app *gpui.App)) {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Fatalf("the real-window slice requires the Windows host")
	}
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
	onHost := func(g func()) {
		done := make(chan struct{})
		if !host.Post(func() { defer close(done); g() }) {
			t.Fatalf("foreground post was dropped: the host loop exited")
		}
		<-done
	}
	ran := false
	var window *gpui.Window
	var openErr error
	onHost(func() {
		window, openErr = app.OpenWindowView(gpui.WindowOptions{
			Title: "base text inline flow", Show: true, Resizable: true,
			Bounds: gpui.Bounds{Size: gpui.Size{Width: 480, Height: 240}},
		}, func(w *gpui.Window, app *gpui.App) gpui.View {
			return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *realProbeView, _ *gpui.Context[realProbeView]) {
				v.run = func(w *gpui.Window, app *gpui.App) { ran = true; f(w, app) }
			}))
		})
	})
	if openErr != nil {
		t.Fatalf("app.OpenWindow: %v — this session is expected to create real windows", openErr)
	}
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			deadline := time.Now().Add(10 * time.Second)
			for window.Alive() && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
		}
	})
	onHost(func() {
		if !ran {
			if _, err := gpui.DrawWindowFrame(window); err != nil {
				t.Errorf("DrawWindowFrame: %v", err)
			}
		}
	})
	if !ran {
		t.Fatal("the prepaint probe did not run")
	}
}

// U:text/inline_flow.rs:1827-1933 on real fonts: a span in a wider family
// near the wrap width must not overflow the flow, and must be measured in
// its own family.
func TestRealInlineCodeNearTheWrapWidthDoesNotOverflowTheFlow(t *testing.T) {
	// The probe runs on the host thread, so it reports with Errorf only.
	inRealPrepaint(t, func(w *gpui.Window, app *gpui.App) {
		const fontSize = 14
		body := bodyStyle(fontSize)
		body.FontFamily = "Segoe UI"
		mono := "Consolas"
		code := inlineHighlight{fontFamily: &mono}
		width := func(text string, family string) float32 {
			style := body
			style.FontFamily = family
			got, _, _ := shapedLineMetrics(text, fontSize, []gpui.TextRun{style.ToRun(len(text))}, w)
			return got
		}
		if width("code_span_here", mono) <= width("code_span_here", body.FontFamily) {
			t.Errorf("the mono family is not wider than the body on this machine")
			return
		}
		tail := " with code_span_here end"
		start := strings.Index(tail, "code_span_here")
		items := []inlineFlowItem{flowText("See "), flowText(tail, inlineHighlightRange{mdRange(start, start+len("code_span_here")), code})}
		// The legacy wrapper sums per-character advances, a little wider than
		// a shaped run on real fonts (no kerning), so the budget leaves room
		// for that, well short of the trailing " end".
		wrap := width("See  with ", body.FontFamily) + width("code_span_here", mono) + 8
		if width(" end", body.FontFamily) <= 8 {
			t.Errorf("the trailing word is narrower than the slack")
			return
		}
		layout := layoutFlow(items, make([]*gpui.Size, 2), body, &wrap, w, app)
		if layout.size.Width > wrap {
			t.Errorf("flow width %v exceeds wrap width %v", layout.size.Width, wrap)
		}
		// Lines are told apart by their line box: two families center their
		// glyph boxes differently on a shared baseline.
		var firstY float32
		spanOnFirst, endBelow := false, false
		for i, f := range layout.fragments {
			if i == 0 {
				firstY = f.selectionBounds.Origin.Y
			}
			if strings.Contains(f.text, "code_span_here") {
				if f.size.Width < width("code_span_here", mono) {
					t.Errorf("the code fragment (%v) is narrower than its mono width", f.size.Width)
				}
				spanOnFirst = f.selectionBounds.Origin.Y == firstY
			}
			if strings.TrimSpace(f.text) == "end" && f.selectionBounds.Origin.Y > firstY {
				endBelow = true
			}
		}
		if !spanOnFirst || !endBelow {
			for _, f := range layout.fragments {
				t.Logf("fragment %q at %v size %v", f.text, f.origin, f.size)
			}
			t.Errorf("wrap %v: span on the first line %v, trailing word wrapped %v", wrap, spanOnFirst, endBelow)
		}
	})
}
