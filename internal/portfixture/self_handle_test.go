package portfixture

// View self-handle tests (consumer gap 05:
// .scratch/gpui-consumer-gaps/issues/05-strong-self-entity-handle.md).
//
// The natural authoring pattern — capture the view's own entity handle
// inside Render and call Update on it from an OnClick listener — needs a
// handle whose lease outlives the render callback. Context.Entity is a
// callback-borrowed facade that expires when Render returns (the runtime
// ownership contract, "Borrowing and retained callbacks"), so a listener
// that captured it is a delayed panic. These tests pin Context.StrongSelf
// — the window-owned strong self-handle mirroring the reference
// Context::entity (R:app/context.rs:50-56 "Returns a handle to the
// entity belonging to this context"; strong in Rust because clones own
// ref-counts and the element tree owns the listener closures) — for the
// listener-capture pattern, together with its lease, release and misuse
// semantics:
//
//   - the handle keeps updating the view after Render returned, both
//     directly and from a click listener delivered after the render;
//   - one lease per (window, view), shared by every read: repeated
//     renders do not accumulate leases;
//   - the lease is owned by the rendering window's scope: closing a
//     window releases its placement (the last window's close releases
//     the view), an independent window keeps it alive, and the leak
//     detector stays green;
//   - misuse diagnostics: StrongSelf outside a window frame, from an
//     entity that is not the rendering view, and Release/TransferInto
//     on the self-handle; RetainInto and Downgrade stay available.

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// selfCounterView is the consumer pattern's view: its Render captures
// the strong self-handle and its increment listener updates the view
// through that handle (never through the escaped state pointer).
type selfCounterView struct {
	count   int
	clicks  int
	renders int
	// self is the strong self-handle read in Render (the documented
	// pattern field; the facade never owns a lease, so storing it in the
	// view's own state creates no cycle).
	self gpui.Entity[selfCounterView]
	// borrowed is the cx.Entity() facade captured in Render, kept only
	// to pin the contrast: it expires when Render returns.
	borrowed gpui.Entity[selfCounterView]
	// model is a foreign non-view entity, for the wrong-entity misuse
	// case.
	model gpui.Entity[Counter]
	// armForeignSelfUpdate arms the wrong-entity misuse in Render: the
	// next render updates the model and calls StrongSelf from the
	// model's context.
	armForeignSelfUpdate bool
}

// Render implements gpui.Render[selfCounterView]: the counter tree with
// an increment div whose click listener updates the view through the
// handle captured during this render.
func (v *selfCounterView) Render(w *gpui.Window, cx *gpui.Context[selfCounterView]) gpui.AnyElement {
	v.renders++
	v.self = cx.StrongSelf()
	v.borrowed = cx.Entity()
	if v.armForeignSelfUpdate {
		// The wrong-entity misuse probe: a context for an entity that is
		// not the view being rendered must refuse the self-handle.
		v.model.Update(cx, func(_ *Counter, mcx *gpui.Context[Counter]) {
			mcx.StrongSelf()
		})
	}
	self := v.self
	return gpui.Div().
		ID("self-counter-root").
		Flex().
		SizeFull().
		Child(gpui.Div().
			ID("increment").
			Flex().
			SizeFull().
			OnClick(func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
				self.Update(app, func(v *selfCounterView, cx *gpui.Context[selfCounterView]) {
					v.clicks++
					v.count++
					cx.Notify()
				})
			}).
			Child("Increment")).
		IntoElement()
}

// selfViewFixture is one test window with the self-counter root view.
type selfViewFixture struct {
	ta     *gpui.TestApp
	app    *gpui.App
	window *gpui.Window
	view   gpui.Entity[selfCounterView]
	vcx    *gpui.VisualTestContext
	// model is a non-view entity owned by the root scope.
	model gpui.Entity[Counter]
	// released records the view's release hook.
	released atomic.Bool
}

// newSelfViewFixture opens a test window whose root view is the
// self-counter, drawn once before returning. The placement lease is
// explicitly the window's (ViewOfIn): the test-app's own
// openTestWindowView helper builds outside a frame boundary, while
// OpenWindowView (the real-window path this ticket also fixes) makes the
// window the construction boundary; both name the same owner.
func newSelfViewFixture(t *testing.T, size gpui.Size) *selfViewFixture {
	t.Helper()
	ta := gpui.NewTestApp()
	app := ta.App()
	f := &selfViewFixture{ta: ta, app: app}
	f.model = gpui.NewEntity(app, app.RootScope(), func(c *Counter, _ *gpui.Context[Counter]) {
		c.value = 0
	})
	f.window = gpui.NewTestWindow(app, size)
	app.Update(func(app *gpui.App) {
		f.view = gpui.NewEntity(app, f.window.Scope(), func(v *selfCounterView, cx *gpui.Context[selfCounterView]) {
			v.model = f.model
			cx.OnRelease(func(_ *selfCounterView, _ *gpui.App) { f.released.Store(true) }).Detach()
		})
		f.window.SetRootView(gpui.ViewOfIn(f.window.Scope(), f.view))
		if _, err := gpui.DrawWindowFrame(f.window); err != nil {
			t.Fatalf("opening draw: %v", err)
		}
	})
	f.vcx = gpui.NewVisualTestContext(f.window, ta)
	return f
}

// facts reads the view's counters.
func (f *selfViewFixture) facts() (count, clicks, renders int) {
	return readSelfView(f.app, f.view)
}

// capturedSelf returns the self-handle the view stored during Render.
func (f *selfViewFixture) capturedSelf() gpui.Entity[selfCounterView] {
	return f.view.Read(f.app, func(v *selfCounterView, _ *gpui.App) gpui.Entity[selfCounterView] {
		return v.self
	})
}

// readSelfView reads the view's counters through an app context.
func readSelfView(app *gpui.App, view gpui.Entity[selfCounterView]) (count, clicks, renders int) {
	type values struct{ count, clicks, renders int }
	got := view.Read(app, func(v *selfCounterView, _ *gpui.App) values {
		return values{count: v.count, clicks: v.clicks, renders: v.renders}
	})
	return got.count, got.clicks, got.renders
}

// selfHandleLeaseCount reads the view's live lease count from the
// ownership report (one report line per live entity).
func selfHandleLeaseCount(t *testing.T, app *gpui.App, id gpui.EntityID) int {
	t.Helper()
	prefix := fmt.Sprintf("entity %d (", uint64(id))
	for _, line := range app.OwnershipReport() {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		// "entity N (type) live, K lease(s): ..."
		rest := line[strings.Index(line, " live, ")+len(" live, "):]
		rest = rest[:strings.Index(rest, " lease(s)")]
		var count int
		if _, err := fmt.Sscanf(rest, "%d", &count); err != nil {
			t.Fatalf("parsing lease count from report line %q: %v", line, err)
		}
		return count
	}
	t.Fatalf("no ownership report line for entity %d", uint64(id))
	return 0
}

// TestViewStrongSelfUpdatesViewAfterRenderReturns is the ticket's core
// acceptance, deterministically: the OnClick listener updates the view
// through the handle captured in Render, and the click fires after
// Render returned (a whole frame completed in between).
func TestViewStrongSelfUpdatesViewAfterRenderReturns(t *testing.T) {
	f := newSelfViewFixture(t, gpui.Size{Width: 240, Height: 120})

	if _, _, renders := f.facts(); renders != 1 {
		t.Fatalf("renders after the opening draw = %d, want 1", renders)
	}

	// A direct update through the captured handle, after the render
	// callback returned: the strong self-handle did not expire with the
	// callback (cx.Entity()'s facade below did).
	self := f.capturedSelf()
	self.Update(f.app, func(v *selfCounterView, cx *gpui.Context[selfCounterView]) {
		v.count++
		cx.Notify()
	})
	f.ta.RunUntilParked()
	if count, _, renders := f.facts(); count != 1 || renders != 2 {
		t.Fatalf("after the direct update: count = %d, renders = %d; want 1/2 (the handle updated the view and the notify redrew)", count, renders)
	}

	// The click: delivered a frame after the render that captured the
	// handle, through the real dispatch path (press + release over the
	// increment div). The dispatch itself redraws the window once and the
	// listener's notify once more (the interact machinery's frame
	// request), so two renders accompany the one update.
	f.vcx.SimulateClick(gpui.Point{X: 40, Y: 40}, gpui.NoModifiers())
	count, clicks, renders := f.facts()
	if count != 2 || clicks != 1 || renders != 4 {
		t.Fatalf("after the click: count = %d, clicks = %d, renders = %d; want 2/1/4 (the listener updated the view through the render-captured handle; the dispatch redraw plus the notify redraw)", count, clicks, renders)
	}

	// A second click through the SAME handle (each render captures an
	// alias of the one window-owned lease).
	f.vcx.SimulateClick(gpui.Point{X: 60, Y: 80}, gpui.NoModifiers())
	if count, clicks, renders := f.facts(); count != 3 || clicks != 2 || renders != 6 {
		t.Fatalf("after the second click: count = %d, clicks = %d, renders = %d; want 3/2/6", count, clicks, renders)
	}
}

// TestViewStrongSelfContrastWithBorrowedEntity pins why the self-handle
// exists: cx.Entity() captured in Render is a callback-borrowed facade
// that expires when the render returns, while cx.StrongSelf() stays
// usable (the runtime ownership contract, "Borrowing and retained
// callbacks").
func TestViewStrongSelfContrastWithBorrowedEntity(t *testing.T) {
	f := newSelfViewFixture(t, gpui.Size{Width: 240, Height: 120})

	borrowed := f.view.Read(f.app, func(v *selfCounterView, _ *gpui.App) gpui.Entity[selfCounterView] {
		return v.borrowed
	})
	expectPanic(t, "callback-borrowed entity handle expired", func() {
		borrowed.Update(f.app, func(_ *selfCounterView, _ *gpui.Context[selfCounterView]) {})
	})

	// The strong self-handle performs the same update without the
	// expiry panic, and both handles address the same entity.
	self := f.capturedSelf()
	if self.EntityID() != borrowed.EntityID() {
		t.Fatalf("self id %d != borrowed id %d (both back this context's entity)", uint64(self.EntityID()), uint64(borrowed.EntityID()))
	}
	self.Update(f.app, func(v *selfCounterView, cx *gpui.Context[selfCounterView]) {
		v.count++
		cx.Notify()
	})
	f.ta.RunUntilParked()
	if count, _, _ := f.facts(); count != 1 {
		t.Fatalf("count after the strong-handle update = %d, want 1", count)
	}
}

// TestViewStrongSelfSharesOneLeaseAcrossRenders pins the lease shape:
// ONE window-owned lease per view, shared by every render's read —
// repeated draws must not accumulate leases (the leak that a
// retain-per-render design would have).
func TestViewStrongSelfSharesOneLeaseAcrossRenders(t *testing.T) {
	f := newSelfViewFixture(t, gpui.Size{Width: 240, Height: 120})

	// The opening draw installed the self lease; the view's leases are
	// its construction lease, the ViewOfIn placement lease and the
	// self-handle lease — all in the window's scope.
	before := selfHandleLeaseCount(t, f.app, f.view.EntityID())
	if before != 3 {
		t.Fatalf("leases after the opening draw = %d, want 3 (construction + placement + self-handle)", before)
	}

	// Five more redraws: each Render reads the self-handle again; the
	// count must not move.
	for i := 0; i < 5; i++ {
		f.view.Update(f.app, func(_ *selfCounterView, cx *gpui.Context[selfCounterView]) {
			cx.Notify()
		})
		f.ta.RunUntilParked()
	}
	if _, _, renders := f.facts(); renders != 6 {
		t.Fatalf("renders after five redraws = %d, want 6", renders)
	}
	if after := selfHandleLeaseCount(t, f.app, f.view.EntityID()); after != before {
		t.Fatalf("leases after five more renders = %d, want %d (one shared window-owned lease, not one per render)", after, before)
	}
}

// TestViewStrongSelfReleasesWithWindow pins the release semantics: the
// window's scope owns the self-handle lease, so closing the window
// releases it — the view releases with its last window, and the leak
// detector reports nothing (the ownership contract's "no leaked
// leases").
func TestViewStrongSelfReleasesWithWindow(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	// The snapshot predates the view: any live entity afterwards is a
	// leak.
	snapshot := app.LeakDetectorSnapshot()

	var (
		window   *gpui.Window
		view     gpui.Entity[selfCounterView]
		released atomic.Bool
	)
	window = gpui.NewTestWindow(app, gpui.Size{Width: 240, Height: 120})
	app.Update(func(app *gpui.App) {
		view = gpui.NewEntity(app, window.Scope(), func(v *selfCounterView, cx *gpui.Context[selfCounterView]) {
			cx.OnRelease(func(_ *selfCounterView, _ *gpui.App) { released.Store(true) }).Detach()
		})
		window.SetRootView(gpui.ViewOfIn(window.Scope(), view))
		if _, err := gpui.DrawWindowFrame(window); err != nil {
			t.Fatalf("opening draw: %v", err)
		}
	})
	vcx := gpui.NewVisualTestContext(window, ta)
	vcx.SimulateClick(gpui.Point{X: 40, Y: 40}, gpui.NoModifiers())
	if _, clicks, _ := readSelfView(app, view); clicks != 1 {
		t.Fatalf("clicks before the close = %d, want 1", clicks)
	}

	// Close the window: the scope releases the construction, placement
	// and self-handle leases; the flush at the update boundary runs the
	// release hook.
	gpui.CloseTestWindow(window)
	ta.Update(func(_ *gpui.App) {})
	if !released.Load() {
		t.Fatal("the view did not release with its window (the self-handle lease must be the window's)")
	}
	if lines := app.AssertEntityReleased(view.EntityID()); len(lines) > 0 {
		t.Errorf("the closed window's view still has leases:\n%s", strings.Join(lines, "\n"))
	}
	if lines := app.AssertNoNewLeaks(snapshot); len(lines) > 0 {
		t.Errorf("window closure leaked entities:\n%s", strings.Join(lines, "\n"))
	}
}

// TestViewStrongSelfSurvivesOneWindowsCloseThroughAnother pins the
// per-window ownership: the same view entity rendered by two windows
// gets one self-handle lease per window; closing one window keeps the
// view alive through the other, and the last close releases it.
func TestViewStrongSelfSurvivesOneWindowsCloseThroughAnother(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	windowA := gpui.NewTestWindow(app, gpui.Size{Width: 240, Height: 120})
	windowB := gpui.NewTestWindow(app, gpui.Size{Width: 240, Height: 120})
	var view gpui.Entity[selfCounterView]
	var released atomic.Bool
	app.Update(func(app *gpui.App) {
		// The view's construction lease belongs to the app root (the
		// test's own read lease): the window story under test is the
		// per-window placement + self-handle leases, released by each
		// window's close.
		view = gpui.NewEntity(app, app.RootScope(), func(v *selfCounterView, cx *gpui.Context[selfCounterView]) {
			cx.OnRelease(func(_ *selfCounterView, _ *gpui.App) { released.Store(true) }).Detach()
		})
		windowA.SetRootView(gpui.ViewOfIn(windowA.Scope(), view))
		windowB.SetRootView(gpui.ViewOfIn(windowB.Scope(), view))
		if _, err := gpui.DrawWindowFrame(windowA); err != nil {
			t.Fatalf("opening draw A: %v", err)
		}
		if _, err := gpui.DrawWindowFrame(windowB); err != nil {
			t.Fatalf("opening draw B: %v", err)
		}
	})
	vcxB := gpui.NewVisualTestContext(windowB, ta)

	// Both windows' frames rendered the view once; each holds its own
	// placement + self-handle lease.
	if _, _, renders := readSelfView(app, view); renders != 2 {
		t.Fatalf("renders after both opening draws = %d, want 2 (one per window)", renders)
	}

	// Close window A: its scope releases A's placement and self-handle
	// leases; the view survives through window B (and the test's root
	// lease).
	gpui.CloseTestWindow(windowA)
	ta.Update(func(_ *gpui.App) {})
	if released.Load() {
		t.Fatal("the view released while window B still renders it")
	}
	if !view.Downgrade().IsUpgradable() {
		t.Fatal("the view must stay live through window B's leases")
	}

	// Window B's listener still updates the view through the handle its
	// own render captured.
	vcxB.SimulateClick(gpui.Point{X: 40, Y: 40}, gpui.NoModifiers())
	count, clicks, renders := readSelfView(app, view)
	if count != 1 || clicks != 1 || renders != 4 {
		t.Fatalf("after window B's click: count = %d, clicks = %d, renders = %d; want 1/1/4 (window B's dispatch and notify redraws)", count, clicks, renders)
	}

	// The last close releases the window-owned leases; with the test's
	// root lease gone too, the view releases.
	gpui.CloseTestWindow(windowB)
	view.Release()
	ta.Update(func(_ *gpui.App) {})
	if !released.Load() {
		t.Fatal("the view did not release after its last window and lease closed")
	}
}

// TestViewStrongSelfMisusePanics pins the misuse diagnostics: StrongSelf
// outside a window frame, and Release/TransferInto on the self-handle
// (the window owns the lease). RetainInto and Downgrade remain available
// for callers that need their own lifetime or a weak identity. The
// wrong-entity diagnostic has its own render-path test below.
func TestViewStrongSelfMisusePanics(t *testing.T) {
	f := newSelfViewFixture(t, gpui.Size{Width: 240, Height: 120})

	// Outside a window frame (no draw is running).
	expectPanic(t, "requires a window frame", func() {
		f.view.Update(f.app, func(_ *selfCounterView, cx *gpui.Context[selfCounterView]) {
			cx.StrongSelf()
		})
	})

	// The self-handle is not releasable or transferable: the window owns
	// the lease.
	self := f.capturedSelf()
	expectPanic(t, "cannot release a view's strong self-handle", func() {
		self.Release()
	})
	expectPanic(t, "cannot transfer a view's strong self-handle", func() {
		self.TransferInto(f.app.RootScope())
	})

	// RetainInto creates an independently owned copy, and Downgrade a
	// persistable weak identity.
	scope := f.app.RootScope().Child()
	owned := self.RetainInto(scope)
	if !owned.Downgrade().IsUpgradable() {
		t.Fatal("the retained self-handle copy must be live")
	}
	weak := self.Downgrade()
	if !weak.IsUpgradable() {
		t.Fatal("the self-handle downgrades to a live weak identity")
	}
	// Releasing the caller's own copy does not touch the window's
	// self-handle lease.
	scope.Close()
	f.ta.Update(func(_ *gpui.App) {})
	if !weak.IsUpgradable() {
		t.Fatal("the view must stay live after releasing a retained copy (the self-handle lease is the window's)")
	}
	if _, _, renders := f.facts(); renders != 1 {
		t.Fatalf("renders = %d, want 1 (the misuse probes drew no new frames)", renders)
	}
}

// TestViewStrongSelfWrongEntityPanicsInsideRender pins the
// wrong-entity diagnostic through the real render path: a Render that
// calls StrongSelf from a foreign entity's context panics inside the
// draw, and the abandoned frame leaves the previously published frame
// intact (the window recovers on its next draw).
func TestViewStrongSelfWrongEntityPanicsInsideRender(t *testing.T) {
	f := newSelfViewFixture(t, gpui.Size{Width: 240, Height: 120})
	draws := gpui.WindowDrawCount(f.window)

	// Arm the wrong-entity probe: the next render updates the model and
	// calls StrongSelf from the model's context. The arming update must
	// NOT notify: the flush would draw the dirty window before the
	// recover-wrapped explicit draw below.
	f.view.Update(f.app, func(v *selfCounterView, _ *gpui.Context[selfCounterView]) {
		v.armForeignSelfUpdate = true
	})

	// The next draw panics with the misuse diagnostic; the abandoned
	// frame preserves the published one.
	func() {
		defer func() {
			r := recover()
			if r == nil {
				t.Fatal("the foreign-entity StrongSelf did not panic")
			}
			msg, ok := r.(string)
			if !ok {
				t.Fatalf("panic value is not a string: %v", r)
			}
			if !strings.Contains(msg, "the self-handle belongs to the view being rendered") {
				t.Fatalf("panic %q does not contain the wrong-view diagnostic", msg)
			}
		}()
		f.ta.Update(func(_ *gpui.App) {
			if _, err := gpui.DrawWindowFrame(f.window); err != nil {
				t.Fatalf("draw: %v", err)
			}
		})
	}()

	// The abandoned frame did not advance the draw counter, and the
	// window still renders normally afterwards.
	if got := gpui.WindowDrawCount(f.window); got != draws {
		t.Fatalf("draws after the abandoned frame = %d, want %d", got, draws)
	}
	f.view.Update(f.app, func(v *selfCounterView, cx *gpui.Context[selfCounterView]) {
		v.armForeignSelfUpdate = false
		cx.Notify()
	})
	f.ta.RunUntilParked()
	// Three render invocations total: the opening draw, the abandoned
	// render (it panicked after entering Render, but published nothing)
	// and the recovered draw.
	if _, _, renders := f.facts(); renders != 3 {
		t.Fatalf("renders after recovery = %d, want 3 (opening + abandoned + recovered)", renders)
	}
}
