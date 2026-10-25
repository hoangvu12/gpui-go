package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Window-level tests on the deterministic test window: styled.rs (1/1),
// observe.rs (1/1), root.rs (1/1). Go-own tests are marked.

// testView renders its build closure.
type testView struct {
	build func(w *gpui.Window, cx *gpui.Context[testView]) gpui.AnyElement
}

func (v *testView) Render(w *gpui.Window, cx *gpui.Context[testView]) gpui.AnyElement {
	return v.build(w, cx)
}

func openTestView(t *testing.T, ta *gpui.TestApp, build func(w *gpui.Window, cx *gpui.Context[testView]) gpui.AnyElement) (gpui.Entity[testView], *gpui.VisualTestContext) {
	t.Helper()
	shutdownAtCleanup(t, ta)
	return ta.AddWindowView(func(v *testView, _ *gpui.Window, _ *gpui.Context[testView]) { v.build = build })
}

// shutdownAtCleanup shuts the test application down when the test ends,
// which retires its image codec's native atlas slot (the service bounds
// live atlases at 8 per process; gpui App.Shutdown).
func shutdownAtCleanup(t testing.TB, ta *gpui.TestApp) {
	t.Cleanup(func() { ta.App().Shutdown() })
}

func debugBounds(t *testing.T, cx *gpui.VisualTestContext, selector string) gpui.Bounds {
	t.Helper()
	b, ok := cx.DebugBounds(selector)
	if !ok {
		t.Fatalf("no debug bounds for %q", selector)
	}
	return b
}

// column is the fixed-width test column (U:styled.rs:225-231).
func column(selector string, height float32) *gpui.DivElement {
	return gpui.Div().Flex().W(gpui.Px(20)).H(gpui.Px(height)).FlexShrink0().DebugSelector(selector)
}

// U:styled.rs:261-284: h_flex centers on the cross axis while v_flex
// leaves the default stretch.
func TestStyledHFlexCentersAndVFlexStretchesOnTheCrossAxis(t *testing.T) {
	ta := gpui.NewTestApp()
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().
			W(gpui.Px(200)).
			H(gpui.Px(100)).
			Child(HFlex().
				SizeFull().
				Child(column("row-child", 40)).
				Child(column("row-overflowing-child", 140)).
				Child(gpui.Div().Flex().W(gpui.Px(20)).DebugSelector("row-stretch"))).
			Child(VFlex().SizeFull().DebugSelector("col").Child(gpui.Div().Flex().H(gpui.Px(20)).DebugSelector("col-stretch"))).
			IntoElement()
	})
	cx.RunUntilParked()
	if _, err := gpui.DrawWindowFrame(cx.Window()); err != nil {
		t.Fatal(err)
	}

	// A shorter child of a 100px row sits at (100 - 40) / 2, and a
	// height-less child keeps its content height instead of filling.
	if top := debugBounds(t, cx, "row-child").Origin.Y; top != 30 {
		t.Fatalf("row-child top = %v, want 30", top)
	}
	if h := debugBounds(t, cx, "row-stretch").Size.Height; h != 0 {
		t.Fatalf("row-stretch height = %v, want 0", h)
	}
	// A child taller than the row is centered too.
	if top := debugBounds(t, cx, "row-overflowing-child").Origin.Y; top != -20 {
		t.Fatalf("row-overflowing-child top = %v, want -20", top)
	}
	// A width-less child of a column fills the cross axis.
	//
	// Deviation: upstream asserts 200px (Base builds against crates.io
	// gpui-ce 0.2.2, map.md §2, not the pin). Under the pin a div defaults
	// to a flex row
	// (R:style.rs:1024-1027, display: Display::Flex), so the two size_full
	// children share the 200px and the column is 100px wide. The asserted
	// property — the child fills the column's cross axis — is checked
	// against the column's own width.
	col := debugBounds(t, cx, "col")
	if w := debugBounds(t, cx, "col-stretch").Size.Width; w != col.Size.Width || w <= 0 {
		t.Fatalf("col-stretch width = %v, want the column width %v", w, col.Size.Width)
	}
}

// Go-own: the StyledExt wrapper writes the pinned fields.
func TestStyledExtWritesThePinnedFields(t *testing.T) {
	d := StyledExtOf(gpui.Div().Flex()).Paddings(EdgesAll(gpui.Px(4)))
	d = StyledExtOf(d).CornerRadii(gpui.Corners{TopLeft: 1, TopRight: 2, BottomRight: 3, BottomLeft: 4})
	d = StyledExtOf(d).FontSemibold()
	d = StyledExtOf(d).DebugRed()
	style := d.Style()
	if v, _ := style.PaddingLeft.Get(); v != gpui.DefinitePx(4) {
		t.Fatalf("padding = %v", v)
	}
	if v, _ := style.CornerRadiiBottomLeft.Get(); v != gpui.Px(4) {
		t.Fatalf("bottom-left radius = %v", v)
	}
	if v, _ := style.Text.FontWeight.Get(); v != 600 {
		t.Fatalf("font weight = %v", v)
	}
	if v, ok := style.BorderWidthsTop.Get(); !ok || v != gpui.Px(1) {
		t.Fatalf("debug border = %v %v", v, ok)
	}
	refined := StyledExtOf(gpui.Styles()).RefineStyle(&style).Refinement()
	if v, _ := refined.Text.FontWeight.Get(); v != 600 {
		t.Fatal("refine_style lost the font weight")
	}
}

// U:observe.rs:45-51: observation preserves identity and the native type
// in normal builds.
func TestObservationPreservesIdentityAndNativeTypeInNormalBuilds(t *testing.T) {
	div := gpui.Div().Flex().ID("target")
	element := TestSupport(TestSupport(div))
	if element != div {
		t.Fatal("test_support changed the element")
	}
	if id, ok := element.IntoElement().ID(); !ok || id.String() != "target" {
		t.Fatalf("id = %v, %v", id, ok)
	}
}

// Go-own: ElementExt.OnPrepaint reports the element's bounds
// (U:element_ext.rs:15-28).
func TestElementExtOnPrepaintReportsBounds(t *testing.T) {
	ta := gpui.NewTestApp()
	var seen []gpui.Bounds
	openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		inner := gpui.Div().Flex().Relative().W(gpui.Px(40)).H(gpui.Px(30)).Mt(gpui.Px(10))
		return gpui.Div().Flex().Child(ElementExtOf(inner).OnPrepaint(func(b gpui.Bounds, _ *gpui.Window, _ *gpui.App) {
			seen = append(seen, b)
		})).IntoElement()
	})
	if len(seen) == 0 {
		t.Fatal("the prepaint callback never ran")
	}
	if got := seen[len(seen)-1]; got.Size != (gpui.Size{Width: 40, Height: 30}) || got.Origin.Y != 10 {
		t.Fatalf("bounds = %+v", got)
	}
}

// Go-own: InteractiveElementExt.OnDoubleClick fires only on the second
// click (U:event.rs:43-57).
func TestInteractiveElementExtOnDoubleClick(t *testing.T) {
	ta := gpui.NewTestApp()
	fired := 0
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		target := gpui.Div().Flex().ID("target").Size(gpui.Px(50))
		return gpui.Div().Flex().SizeFull().Child(InteractiveElementExtOf(target).OnDoubleClick(
			func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { fired++ })).IntoElement()
	})
	at := gpui.Point{X: 10, Y: 10}
	cx.SimulateClick(at, gpui.NoModifiers())
	if fired != 0 {
		t.Fatalf("a single click fired the double-click listener (%d)", fired)
	}
	// The platform numbers the clicks (the test harness's SimulateClick
	// always reports a single click), so the second click is dispatched
	// with its count.
	cx.SimulateEvent(&gpui.MouseDownEvent{Position: at, Button: gpui.MouseButtonLeft, ClickCount: 2})
	cx.SimulateEvent(&gpui.MouseUpEvent{Position: at, Button: gpui.MouseButtonLeft, ClickCount: 2})
	if fired != 1 {
		t.Fatalf("double click fired %d times, want 1", fired)
	}
}

// rootTestContent and rootTestLayer are the root.rs test views
// (U:root.rs:317-332).
type rootTestContent struct{}

func (*rootTestContent) Render(*gpui.Window, *gpui.Context[rootTestContent]) gpui.AnyElement {
	return gpui.Div().Flex().IntoElement()
}

type rootTestLayer struct{}

func (*rootTestLayer) Render(*gpui.Window, *gpui.Context[rootTestLayer]) gpui.AnyElement {
	return gpui.Div().Flex().IntoElement()
}

func rootTestLayerBuild(*rootTestLayer, *gpui.Window, *gpui.Context[rootTestLayer]) {}

func openRoot(ta *gpui.TestApp, content func(w *gpui.Window, cx *gpui.Context[Root]) gpui.View) (gpui.Entity[Root], *gpui.VisualTestContext) {
	return ta.AddWindowView(func(r *Root, w *gpui.Window, cx *gpui.Context[Root]) {
		NewRoot(r, content(w, cx), w, cx)
	})
}

func emptyContent(w *gpui.Window, cx *gpui.Context[Root]) gpui.View {
	return gpui.ViewOf(gpui.NewEntity(cx, w.Scope(), func(*rootTestContent, *gpui.Context[rootTestContent]) {}))
}

// U:root.rs:334-354
func TestRootPluginRegistrationIsIdempotentAndStateIsPerWindow(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		Init(app)
		RootRegisterPlugin[rootTestLayer](app, rootTestLayerBuild)
		RootRegisterPlugin[rootTestLayer](app, rootTestLayerBuild)
	})
	var ids []gpui.EntityID
	for range 2 {
		root, _ := openRoot(ta, emptyContent)
		id := root.Read(ta, func(r *Root, _ *gpui.App) gpui.EntityID {
			if len(r.plugins) != 1 {
				t.Fatalf("plugins = %d, want 1", len(r.plugins))
			}
			layer, ok := r.Plugin[rootTestLayer]()
			if !ok {
				t.Fatal("plugin not found")
			}
			return layer.EntityID()
		})
		ids = append(ids, id)
	}
	if ids[0] == ids[1] {
		t.Fatal("plugin state is shared between windows")
	}
}

// hookPlugin records its stages (Go-own fixture).
type hookPlugin struct {
	log *[]string
}

func (*hookPlugin) Render(*gpui.Window, *gpui.Context[hookPlugin]) gpui.AnyElement {
	return gpui.Div().Flex().DebugSelector("plugin-overlay").SizeFull().IntoElement()
}

func (p *hookPlugin) Prepare(*gpui.Window, *gpui.Context[hookPlugin]) {
	*p.log = append(*p.log, "prepare")
}

func (p *hookPlugin) Style(surface *gpui.DivElement, _ *gpui.Window, _ *gpui.App) {
	*p.log = append(*p.log, "style")
	surface.Opacity(0.5).W(gpui.Px(300))
}

func (p *hookPlugin) Decorate(surface gpui.AnyElement, root *Root, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
	*p.log = append(*p.log, "decorate")
	if root.View() == nil {
		panic("decorate saw no view")
	}
	return gpui.Div().Flex().DebugSelector("decorated").SizeFull().Child(surface).IntoElement()
}

// Go-own: the three plugin stages run in order, the instance style wins
// over plugin defaults, and the overlay mounts above the content
// (U:root.rs:280-310).
func TestRootPluginStagesAndInstanceStyle(t *testing.T) {
	ta := gpui.NewTestApp()
	var log []string
	ta.Update(func(app *gpui.App) {
		RootRegisterPlugin[hookPlugin](app, func(p *hookPlugin, _ *gpui.Window, _ *gpui.Context[hookPlugin]) {
			p.log = &log
		})
	})
	_, cx := ta.AddWindowView(func(r *Root, w *gpui.Window, cx *gpui.Context[Root]) {
		NewRoot(r, emptyContent(w, cx), w, cx)
		r.W(gpui.Px(200)) // the instance style
	})
	if len(log) < 3 || log[0] != "prepare" || log[1] != "style" || log[2] != "decorate" {
		t.Fatalf("stages = %v", log)
	}
	if _, ok := cx.DebugBounds("decorated"); !ok {
		t.Fatal("decorate's wrapper was not drawn")
	}
	overlay := debugBounds(t, cx, "plugin-overlay")
	if overlay.Size.Width != 200 {
		t.Fatalf("overlay width = %v, want the instance width 200 over the plugin's 300", overlay.Size.Width)
	}
}

// tabView renders tab stops inside the root content (Go-own fixture).
type tabView struct {
	container gpui.FocusHandle
	inside    []gpui.FocusHandle
	outside   gpui.FocusHandle
}

func (v *tabView) Render(*gpui.Window, *gpui.Context[tabView]) gpui.AnyElement {
	trap := gpui.Div().Flex().TrackFocus(v.container)
	for _, stop := range v.inside {
		trap = trap.Child(gpui.Div().Flex().Size(gpui.Px(10)).TrackFocus(stop))
	}
	return gpui.Div().Flex().SizeFull().
		Child(trap).
		Child(gpui.Div().Flex().Size(gpui.Px(10)).TrackFocus(v.outside)).
		IntoElement()
}

// Go-own: Tab and Shift-Tab move focus through the core's default
// action (browser-core 14 — the Root's Tab bindings and the focus trap
// are gone, ticket 72: the browser's Tab is a default action, and a
// modal dialog's inert outside is the trap's replacement).
func TestRootTabNavigationUsesTheCoreDefaultAction(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	ta.Update(func(app *gpui.App) { Init(app) })
	container := app.FocusHandle()
	a := app.FocusHandle().TabStop(true)
	b := app.FocusHandle().TabStop(true)
	c := app.FocusHandle().TabStop(true)
	_, cx := openRoot(ta, func(w *gpui.Window, cx *gpui.Context[Root]) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(cx, w.Scope(), func(v *tabView, _ *gpui.Context[tabView]) {
			v.container, v.inside, v.outside = container, []gpui.FocusHandle{a, b}, c
		}))
	})
	focused := func() uint64 {
		var id uint64
		cx.Update(func(w *gpui.Window, _ *gpui.App) {
			if f := w.Focused(); f.Ok {
				id = f.Handle.ID()
			}
		})
		return id
	}
	cx.Update(func(w *gpui.Window, _ *gpui.App) { w.Focus(a) })
	cx.SimulateKeystrokes("tab")
	if focused() != b.ID() {
		t.Fatal("tab did not move focus to the next stop")
	}
	cx.SimulateKeystrokes("shift-tab")
	if focused() != a.ID() {
		t.Fatal("shift-tab did not move focus back")
	}

	// Tab from b reaches c outside the container: the document order
	// rules, no container keeps focus inside.
	cx.Update(func(w *gpui.Window, _ *gpui.App) { w.Focus(b) })
	cx.SimulateKeystrokes("tab")
	if focused() != c.ID() {
		t.Fatal("tab from b did not reach c")
	}
}
