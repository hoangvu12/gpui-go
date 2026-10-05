package authorspec

import (
	"testing"

	"gpui-go/gpui"
)

// counterView is a window root view over the Counter entity: it renders
// the counter's div tree and records its render and notification counts
// (the observables of the authoring runtime).
type counterView struct {
	counter  gpui.Entity[Counter]
	renders  int
	notified int
}

// Render implements gpui.Render[Counter]... no: the view entity's own
// state renders; this view IS an entity whose Render reads the counter.
func (v *counterView) Render(w *gpui.Window, cx *gpui.Context[counterView]) gpui.AnyElement {
	v.renders++
	value := v.counter.Read(cx, func(c *Counter, _ *gpui.App) int { return c.count })
	return gpui.Div().
		ID("counter-view").
		DebugSelector("root").
		Flex().
		Gap2().
		P(gpui.DefiniteRem(0.75)).
		Justify(gpui.AlignContentCenter).
		Bg(gpui.RgbaToHsla(0xdc2626ff)).
		SizeFull().
		TextSize(gpui.RemsOf(1.25)).
		Child(gpui.Div().ID("label").DebugSelector("label").Flex().Child(labelText(value))).
		IntoElement()
}

// labelText renders the count text (the fixture's text child).
func labelText(value int) string {
	return labelPrefix + string(rune('0'+value%10)) + suffix
}

const labelPrefix = "Count: "
const suffix = ""

// testAppView bundles one test window's app-side handles.
type testAppView struct {
	app     *gpui.App
	window  *gpui.Window
	view    gpui.Entity[counterView]
	counter gpui.Entity[Counter]
}

// viewFacts reads the view entity's render and notification counts (the
// entity owns the mutable state; the counts are read through it).
func (t *testAppView) viewFacts() (renders, notified int) {
	type facts struct{ renders, notified int }
	f := t.view.Read(t.app, func(v *counterView, _ *gpui.App) facts {
		return facts{renders: v.renders, notified: v.notified}
	})
	return f.renders, f.notified
}

// newTestAppView builds the app + one test window with a counter view
// entity observing the counter entity.
func newTestAppView(t *testing.T, initial int, size gpui.Size) *testAppView {
	t.Helper()
	app := gpui.NewTestApp().App()
	w := gpui.NewTestWindow(app, size)
	counter := NewCounter(app, w.Scope(), w, initial)
	viewEntity := gpui.NewEntity(app, w.Scope(), func(v *counterView, cx *gpui.Context[counterView]) {
		v.counter = counter
		cx.Observe(counter, func(_ *counterView, _ gpui.Entity[Counter], cx *gpui.Context[counterView]) {
			v.notified++
			cx.Notify()
		}).Detach()
	})
	w.SetRootView(gpui.ViewOf(viewEntity))
	return &testAppView{app: app, window: w, view: viewEntity, counter: counter}
}

// TestCounterViewDrawsThroughElementPhases is the headless authoring
// runtime smoke: one test window whose root view renders the counter's
// div tree through the real element phases (request → compute →
// prepaint → paint), with the layout facts, the scene's background quad
// and the text's shaped geometry observable.
func TestCounterViewDrawsThroughElementPhases(t *testing.T) {
	if testing.Short() {
		t.Skip("element runtime draws through the native layout/scene services")
	}
	tv := newTestAppView(t, 8, gpui.Size{Width: 320, Height: 200})
	w, counter := tv.window, tv.counter

	scene, err := gpui.DrawWindowFrame(w)
	if err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	if scene == nil {
		t.Fatal("DrawWindowFrame returned no scene")
	}
	if got := gpui.WindowDrawCount(w); got != 1 {
		t.Errorf("draw count = %d, want 1", got)
	}
	renders, _ := tv.viewFacts()
	if renders != 1 {
		t.Errorf("view renders = %d, want 1 (one render per drawn frame)", renders)
	}

	// The layout facts: the root div fills the window (size_full), the
	// label is centered by justify-center within the padded content box.
	root, ok := gpui.WindowDebugBound(w, "root")
	if !ok {
		t.Fatal("no debug bounds recorded for the root selector")
	}
	if root.Size != (gpui.Size{Width: 320, Height: 200}) {
		t.Errorf("root bounds = %+v, want the full 320x200 window", root)
	}
	label, ok := gpui.WindowDebugBound(w, "label")
	if !ok {
		t.Fatal("no debug bounds recorded for the label selector")
	}
	if label.Size.Width <= 0 || label.Size.Height <= 0 {
		t.Errorf("label bounds = %+v, want a content-sized label", label)
	}
	// justify-center: the label is horizontally centered in the root's
	// content box (padding 0.75rem = 12px per edge).
	padding := float32(0.75 * 16)
	wantCenter := padding + (320-2*padding)/2
	gotCenter := label.Origin.X + label.Size.Width/2
	if diff := gotCenter - wantCenter; diff < -0.51 || diff > 0.51 {
		t.Errorf("label center x = %f (bounds %+v), want ~%f (justify center)", gotCenter, label, wantCenter)
	}

	// The scene: one background quad (the root div's bg), no sprites (the
	// test text system's rasters are empty, like the reference test
	// window).
	quads, mono, sub, poly, err := gpui.WindowPrimitiveCounts(w)
	if err != nil {
		t.Fatalf("WindowPrimitiveCounts: %v", err)
	}
	if quads != 1 {
		t.Errorf("quad count = %d, want 1 (the root background)", quads)
	}
	if mono != 0 || sub != 0 || poly != 0 {
		t.Errorf("sprite counts = %d/%d/%d, want 0/0/0 (the test text system rasterizes empty glyphs)", mono, sub, poly)
	}

	// The text facts: shaping "Count: 8" at 1.25rem through the window's
	// text system matches the deterministic reference test layout (each
	// of the 8 ASCII glyphs advances em_width = 0.6 * font_size).
	style := gpui.DefaultTextStyle()
	style.FontSize = gpui.RemsOf(1.25)
	shaped, err := w.ShapeText("Count: 8", style, nil)
	if err != nil {
		t.Fatalf("ShapeText: %v", err)
	}
	if shaped.TextLen != 8 || shaped.LineCount != 1 {
		t.Errorf("shaped text len/lines = %d/%d, want 8/1", shaped.TextLen, shaped.LineCount)
	}
	wantWidth := float32(8 * 0.6 * 20)
	if shaped.Width < wantWidth-0.01 || shaped.Width > wantWidth+0.01 {
		t.Errorf("shaped width = %f, want ~%f (8 glyphs at em width 0.6*20)", shaped.Width, wantWidth)
	}
	if len(shaped.Glyphs) != 8 {
		t.Errorf("glyph count = %d, want 8", len(shaped.Glyphs))
	}

	// The label's width follows the shaped text width (the flex item is
	// content-sized).
	if label.Size.Width < shaped.Width-0.51 || label.Size.Width > shaped.Width+0.51 {
		t.Errorf("label width = %f, want ~%f (the shaped text width)", label.Size.Width, shaped.Width)
	}

	// An increment from within the window re-renders: the entity update
	// notifies the observer (the view entity), and the next draw renders
	// the new count.
	counter.Update(tv.app, func(c *Counter, cx *gpui.Context[Counter]) {
		c.increment(cx)
	})
	_, notified := tv.viewFacts()
	if notified != 1 {
		t.Errorf("view notified = %d, want 1 (coalesced notify delivery)", notified)
	}
	if _, err := gpui.DrawWindowFrame(w); err != nil {
		t.Fatalf("second DrawWindowFrame: %v", err)
	}
	if got := gpui.WindowDrawCount(w); got != 2 {
		t.Errorf("draw count = %d, want 2", got)
	}
	renders, _ = tv.viewFacts()
	if renders != 2 {
		t.Errorf("view renders = %d, want 2", renders)
	}
}
