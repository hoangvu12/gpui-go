package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// The appear tests of U:plot/appear.rs (11/11): the eight TestAppContext
// recorder tests on the virtual clock, and the three pure `at(time)` ones.

// plotSamples is the recorder's `Rc<RefCell<Vec<f32>>>`.
type plotSamples struct{ values []float32 }

func (s *plotSamples) first() (float32, bool) {
	if len(s.values) == 0 {
		return 0, false
	}
	return s.values[0], true
}

func (s *plotSamples) last() (float32, bool) {
	if len(s.values) == 0 {
		return 0, false
	}
	return s.values[len(s.values)-1], true
}

// plotRecorder is a plot that records the appear progress it is handed
// each frame (U:plot/appear.rs:275-308).
type plotRecorder struct {
	PlotDefaults
	samples       *plotSamples
	generation    uint64
	hasGeneration bool
}

func (r *plotRecorder) IntoElement() gpui.AnyElement { return NewPlotElement(r).IntoElement() }

func (r *plotRecorder) Paint(gpui.Bounds, *gpui.Window, *gpui.App) {}

func (r *plotRecorder) ID() (gpui.ElementID, bool) { return gpui.ElementIDOf("recorder"), true }

// Interactive is false: appear motion rides on the id alone, without the
// interactive layer.
func (r *plotRecorder) Interactive() bool { return false }

func (r *plotRecorder) Appear(appear PlotAppear, _ *gpui.Window, _ *gpui.App) {
	r.samples.values = append(r.samples.values, appear.Progress())
}

func (r *plotRecorder) AppearGeneration() (uint64, bool) { return r.generation, r.hasGeneration }

// plotRecorderView is U:plot/appear.rs:310-322.
type plotRecorderView struct {
	samples       *plotSamples
	generation    uint64
	hasGeneration bool
}

func (v *plotRecorderView) Render(*gpui.Window, *gpui.Context[plotRecorderView]) gpui.AnyElement {
	return (&plotRecorder{samples: v.samples, generation: v.generation, hasGeneration: v.hasGeneration}).IntoElement()
}

// plotScopedView is a recorder that can be taken out of the tree and put
// back, inside a PlotAppearScope named scope or none
// (U:plot/appear.rs:324-345).
type plotScopedView struct {
	samples    *plotSamples
	scope      uint64
	hasScope   bool
	mounted    bool
	generation uint64
}

func (v *plotScopedView) Render(*gpui.Window, *gpui.Context[plotScopedView]) gpui.AnyElement {
	body := gpui.Div().Flex().SizeFull()
	if v.mounted {
		body = body.Child(&plotRecorder{samples: v.samples, generation: v.generation, hasGeneration: true})
	}
	if v.hasScope {
		return NewPlotAppearScope(gpui.NamedIntegerElementID("scope", v.scope), body).IntoElement()
	}
	return body.IntoElement()
}

// setPlotAppearTheme is U:plot/appear.rs:347-355.
func setPlotAppearTheme(ta *gpui.TestApp) {
	ta.Update(func(app *gpui.App) {
		theme := DefaultTheme()
		app.SetGlobal(&theme)
		ThemeGlobalMut(app).Plot = NewPlotTheme().WithMotion(
			DefaultPlotMotion().WithAppear(NewTransition(ms(100)).Ease(linear)),
		)
	})
}

var plotAppearWindowOptions = gpui.WindowOptions{Bounds: gpui.Bounds{Size: gpui.Size{Width: 100, Height: 100}}}

// openPlotScoped is U:plot/appear.rs:357-374.
func openPlotScoped(ta *gpui.TestApp, scope uint64, hasScope bool) (*gpui.TestAppWindow[plotScopedView], *plotSamples) {
	setPlotAppearTheme(ta)
	samples := &plotSamples{}
	window := ta.OpenWindowWithOptions[plotScopedView](plotAppearWindowOptions,
		func(v *plotScopedView, _ *gpui.Window, _ *gpui.Context[plotScopedView]) {
			*v = plotScopedView{samples: samples, scope: scope, hasScope: hasScope, mounted: true}
		})
	ta.RunUntilParked()
	return window, samples
}

// updatePlotScoped changes the view, then returns the progress the plot
// was handed on the frame that follows, if it was painted
// (U:plot/appear.rs:376-394).
func updatePlotScoped(window *gpui.TestAppWindow[plotScopedView], f func(*plotScopedView)) (float32, bool) {
	window.Update(func(v *plotScopedView, _ *gpui.Window, cx *gpui.Context[plotScopedView]) {
		f(v)
		v.samples.values = nil
		cx.Notify()
	})
	return window.Read[*plotSamples](func(v *plotScopedView, _ *gpui.App) *plotSamples { return v.samples }).last()
}

// finishPlotAppear is U:plot/appear.rs:396-402.
func finishPlotAppear(ta *gpui.TestApp, window *gpui.TestAppWindow[plotScopedView]) {
	ta.AdvanceClock(100)
	window.Window().SimulateNextFrame(ta.App())
	ta.RunUntilParked()
}

// openPlotRecorder is U:plot/appear.rs:404-419.
func openPlotRecorder(ta *gpui.TestApp, generation uint64, hasGeneration bool) (*gpui.TestAppWindow[plotRecorderView], *plotSamples) {
	setPlotAppearTheme(ta)
	samples := &plotSamples{}
	window := ta.OpenWindowWithOptions[plotRecorderView](plotAppearWindowOptions,
		func(v *plotRecorderView, _ *gpui.Window, _ *gpui.Context[plotRecorderView]) {
			*v = plotRecorderView{samples: samples, generation: generation, hasGeneration: hasGeneration}
		})
	ta.RunUntilParked()
	return window, samples
}

// nextPlotFrame is U:plot/appear.rs:421-427.
func nextPlotFrame(ta *gpui.TestApp, window *gpui.TestAppWindow[plotRecorderView]) int {
	frames := window.Window().SimulateNextFrame(ta.App())
	ta.RunUntilParked()
	return frames
}

func expectSample(t *testing.T, what string, got float32, ok bool, want float32) {
	t.Helper()
	if !ok || got != want {
		t.Fatalf("%s = (%v, %v), want Some(%v)", what, got, ok, want)
	}
}

// U:plot/appear.rs:429-447
func TestPlotAppearsOnceOverTheThemeDuration(t *testing.T) {
	ta := gpui.NewTestApp()
	w, samples := openPlotRecorder(ta, 0, true)
	got, ok := samples.last()
	expectSample(t, "first frame", got, ok, 0)

	ta.AdvanceClock(50)
	nextPlotFrame(ta, w)
	got, ok = samples.last()
	expectSample(t, "at 50ms", got, ok, 0.5)

	ta.AdvanceClock(50)
	nextPlotFrame(ta, w)
	got, ok = samples.last()
	expectSample(t, "at 100ms", got, ok, 1)

	// Once whole, the plot stops asking for frames and stays whole.
	if n := nextPlotFrame(ta, w); n != 0 {
		t.Fatalf("frames after the appear = %d, want 0", n)
	}
	ta.Update(func(*gpui.App) { w.Window().Refresh() })
	ta.RunUntilParked()
	got, ok = samples.last()
	expectSample(t, "after refresh", got, ok, 1)
}

// U:plot/appear.rs:449-455
func TestReducedMotionSkipsTheAppear(t *testing.T) {
	ta := gpui.NewTestApp()
	reduceMotion(ta)
	w, samples := openPlotRecorder(ta, 0, true)
	got, ok := samples.first()
	expectSample(t, "first", got, ok, 1)
	if n := nextPlotFrame(ta, w); n != 0 {
		t.Fatalf("frames = %d, want 0", n)
	}
}

// A plot that does not opt in is whole at once and asks for no frames,
// even with an appear duration in the theme (U:plot/appear.rs:457-464).
func TestPlotWithoutAGenerationDoesNotAppear(t *testing.T) {
	ta := gpui.NewTestApp()
	w, samples := openPlotRecorder(ta, 0, false)
	got, ok := samples.first()
	expectSample(t, "first", got, ok, 1)
	if n := nextPlotFrame(ta, w); n != 0 {
		t.Fatalf("frames = %d, want 0", n)
	}
}

// A new generation starts the appear over (U:plot/appear.rs:466-482).
func TestNewGenerationReplaysTheAppear(t *testing.T) {
	ta := gpui.NewTestApp()
	w, samples := openPlotRecorder(ta, 0, true)
	ta.AdvanceClock(100)
	nextPlotFrame(ta, w)
	got, ok := samples.last()
	expectSample(t, "finished", got, ok, 1)

	w.Update(func(v *plotRecorderView, _ *gpui.Window, cx *gpui.Context[plotRecorderView]) {
		v.generation, v.hasGeneration = 1, true
		cx.Notify()
	})
	got, ok = samples.last()
	expectSample(t, "replay", got, ok, 0)
}

// Inside a scope, a plot painted again after a gap is whole at once and
// asks for no frames (U:plot/appear.rs:484-508).
func TestScopeKeepsAFinishedAppearAcrossARemount(t *testing.T) {
	ta := gpui.NewTestApp()
	window, samples := openPlotScoped(ta, 0, true)
	got, ok := samples.last()
	expectSample(t, "first frame", got, ok, 0)
	finishPlotAppear(ta, window)
	got, ok = samples.last()
	expectSample(t, "finished", got, ok, 1)

	if got, ok := updatePlotScoped(window, func(v *plotScopedView) { v.mounted = false }); ok {
		t.Fatalf("unmounted = Some(%v), want None", got)
	}
	got, ok = updatePlotScoped(window, func(v *plotScopedView) { v.mounted = true })
	expectSample(t, "remounted", got, ok, 1)
	if n := window.Window().SimulateNextFrame(ta.App()); n != 0 {
		t.Fatalf("frames after the remount = %d, want 0", n)
	}

	// A new generation still replays.
	got, ok = updatePlotScoped(window, func(v *plotScopedView) { v.generation = 1 })
	expectSample(t, "new generation", got, ok, 0)
}

// A plot taken away before its appear finished draws in again
// (U:plot/appear.rs:510-519).
func TestScopeReplaysAnUnfinishedAppear(t *testing.T) {
	ta := gpui.NewTestApp()
	window, _ := openPlotScoped(ta, 0, true)
	if got, ok := updatePlotScoped(window, func(v *plotScopedView) { v.mounted = false }); ok {
		t.Fatalf("unmounted = Some(%v), want None", got)
	}
	got, ok := updatePlotScoped(window, func(v *plotScopedView) { v.mounted = true })
	expectSample(t, "remounted", got, ok, 0)
}

// The memory goes with the scope: a scope under a new id, or one that
// stops being painted, draws its plots in afresh
// (U:plot/appear.rs:521-542).
func TestScopeForgetsWhenItGoes(t *testing.T) {
	ta := gpui.NewTestApp()
	window, _ := openPlotScoped(ta, 0, true)
	finishPlotAppear(ta, window)
	got, ok := updatePlotScoped(window, func(v *plotScopedView) { v.scope, v.hasScope = 1, true })
	expectSample(t, "new scope id", got, ok, 0)

	finishPlotAppear(ta, window)
	got, ok = updatePlotScoped(window, func(v *plotScopedView) { v.hasScope = false })
	expectSample(t, "no scope", got, ok, 0)
	finishPlotAppear(ta, window)
	got, ok = updatePlotScoped(window, func(v *plotScopedView) { v.scope, v.hasScope = 1, true })
	expectSample(t, "scope back", got, ok, 0)
}

// Without a scope, a plot painted again after a gap draws in again
// (U:plot/appear.rs:544-554).
func TestWithoutAScopeARemountReplays(t *testing.T) {
	ta := gpui.NewTestApp()
	window, _ := openPlotScoped(ta, 0, false)
	finishPlotAppear(ta, window)
	if got, ok := updatePlotScoped(window, func(v *plotScopedView) { v.mounted = false }); ok {
		t.Fatalf("unmounted = Some(%v), want None", got)
	}
	got, ok := updatePlotScoped(window, func(v *plotScopedView) { v.mounted = true })
	expectSample(t, "remounted", got, ok, 0)
}

// plotAppearAt is U:plot/appear.rs:556-561.
func plotAppearAt(time float32) PlotAppear {
	return PlotAppear{time: time, easing: EasingLinear()}
}

// U:plot/appear.rs:563-569
func TestCompleteAppear(t *testing.T) {
	appear := PlotAppearComplete()
	if appear.IsAppearing() {
		t.Fatal("complete appear is appearing")
	}
	if got := appear.Progress(); got != 1 {
		t.Fatalf("progress = %v, want 1", got)
	}
	if got := appear.Staggered(3, 10, 0.5); got != 1 {
		t.Fatalf("staggered = %v, want 1", got)
	}
}

// U:plot/appear.rs:571-582
func TestStaggeredMarksShareTheAppear(t *testing.T) {
	cases := []struct {
		time         float32
		index, count int
		spread, want float32
	}{
		// The first mark starts at once, the last once the spread has
		// passed.
		{0, 0, 5, 0.5, 0},
		{0.25, 0, 5, 0.5, 0.5},
		{0.5, 4, 5, 0.5, 0},
		{0.75, 4, 5, 0.5, 0.5},
	}
	for _, c := range cases {
		if got := plotAppearAt(c.time).Staggered(c.index, c.count, c.spread); got != c.want {
			t.Fatalf("at(%v).staggered(%d, %d, %v) = %v, want %v", c.time, c.index, c.count, c.spread, got, c.want)
		}
	}
	// Every mark finishes with the appear.
	for index := range 5 {
		if got := plotAppearAt(1).Staggered(index, 5, 0.5); got != 1 {
			t.Fatalf("at(1).staggered(%d, 5, 0.5) = %v, want 1", index, got)
		}
	}
}

// U:plot/appear.rs:584-590
func TestStaggeredWithoutSpreadMovesTogether(t *testing.T) {
	if got := plotAppearAt(0.4).Staggered(0, 3, 0); got != 0.4 {
		t.Fatalf("staggered(0, 3, 0) = %v", got)
	}
	if got := plotAppearAt(0.4).Staggered(2, 3, 0); got != 0.4 {
		t.Fatalf("staggered(2, 3, 0) = %v", got)
	}
	// A lone mark ignores the spread.
	if got := plotAppearAt(0.4).Staggered(0, 1, 0.5); got != 0.4 {
		t.Fatalf("staggered(0, 1, 0.5) = %v", got)
	}
}
