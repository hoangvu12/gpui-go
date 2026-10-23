//go:build windows

package script_test

// The host in a (hostless, headless) test window: a save reloads the
// window in place with keyed state kept, a failed reload keeps the last
// good tree and mounts the error banner, and fragments splice their
// children. The change notification is injected (Host.Reload), exactly
// as the dev file watcher delivers it, so nothing here depends on
// timing.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/gpui/script"
	"github.com/hoangvu12/gpui-go/gpui/script/compile"
)

// boxView is the windowed test view: a clickable box whose width the
// tests edit, with a bound counter label (keyed state).
const boxView = `
export function App() {
	const clicks = state("clicks", 0);
	return (
		<div
			id="box"
			style={{ width: {WIDTH}, height: 80, padding: 8, background: "#e2e8f0", color: "#0f172a", fontSize: 16 }}
			onClick={() => clicks.set(clicks.get() + 1)}
		>
			<text id="label">{bind(clicks, (n: number) => ` + "`Clicked ${n}`" + `)}</text>
		</div>
	);
}
`

// scriptWindow is a headless test window showing a host's view.
type scriptWindow struct {
	ta     *gpui.TestApp
	window *gpui.Window
	cx     *gpui.VisualTestContext
}

func openScriptWindow(t *testing.T, host *script.Host) *scriptWindow {
	t.Helper()
	ta := gpui.NewTestApp()
	window := gpui.NewTestWindow(ta.App(), gpui.Size{Width: 400, Height: 300})
	t.Cleanup(func() {
		gpui.CloseTestWindow(window)
		ta.App().Shutdown()
	})
	if err := gpui.UseRealTextSystem(window); err != nil {
		t.Skipf("real text system unavailable: %v", err)
	}
	ta.Update(func(app *gpui.App) {
		root := gpui.NewEntity(app, window.Scope(), func(v *script.View, _ *gpui.Context[script.View]) { v.Host = host })
		window.SetRootView(gpui.ViewOf(root))
	})
	cx := gpui.NewVisualTestContext(window, ta)
	host.Notify = window.Refresh
	cx.RunUntilParked()
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("initial draw: %v", err)
	}
	return &scriptWindow{ta: ta, window: window, cx: cx}
}

// clickBox clicks the element with the given debug selector (clear of
// its text run), through real hit testing.
func (w *scriptWindow) clickBox(t *testing.T, id string) {
	t.Helper()
	b, ok := w.cx.DebugBounds(id)
	if !ok {
		t.Fatalf("no bounds for %s", id)
	}
	w.cx.SimulateClick(gpui.Point{
		X: b.Origin.X + b.Size.Width - 6,
		Y: b.Origin.Y + b.Size.Height/2,
	}, gpui.NoModifiers())
}

// reloadFrame injects the change notification (the watcher's call) and
// draws the frame it asks for.
func (w *scriptWindow) reloadFrame(t *testing.T, h *script.Host) {
	t.Helper()
	before := gpui.WindowDrawCount(w.window)
	if err := h.Reload(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if gpui.WindowDrawCount(w.window) == before {
		w.window.SimulateFrameRequest()
	}
	if _, err := gpui.DrawWindowFrame(w.window); err != nil {
		t.Fatalf("draw: %v", err)
	}
}

func TestSavingReloadsTheWindowInPlaceWithKeyedState(t *testing.T) {
	// A real dev source on disk: the .tsx file is written, edited and
	// reloaded through the injected notification.
	dir := t.TempDir()
	write := func(width string) {
		t.Helper()
		src := strings.Replace(boxView, "{WIDTH}", width, 1)
		if err := os.WriteFile(filepath.Join(dir, "view.tsx"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("200")
	source, err := compile.Open(filepath.Join(dir, "view.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	host := script.New(script.WithSource(source))
	if err := host.Reload(); err != nil {
		t.Fatal(err)
	}
	w := openScriptWindow(t, host)

	// The box is laid out at its initial width.
	b, ok := w.cx.DebugBounds("box")
	if !ok {
		t.Fatal("no bounds for box")
	}
	if b.Size.Width != 200 {
		t.Fatalf("box width = %v, want 200", b.Size.Width)
	}

	// Click through the real input path: the bound label updates.
	for range 3 {
		w.clickBox(t, "box")
	}
	if got, _ := host.Text("label"); got != "Clicked 3" {
		t.Fatalf("label = %q, want %q", got, "Clicked 3")
	}

	// Save an edit: the file changes, the injected notification reloads,
	// and the window shows the new tree in place.
	write("240")
	w.reloadFrame(t, host)

	b, ok = w.cx.DebugBounds("box")
	if !ok || b.Size.Width != 240 {
		t.Fatalf("box after reload = %+v, want width 240 (the window did not reload in place)", b)
	}
	// Keyed state survived the reload: the counter continues.
	w.clickBox(t, "box")
	if got, _ := host.Text("label"); got != "Clicked 4" {
		t.Fatalf("label after reload = %q, want %q (keyed state lost)", got, "Clicked 4")
	}
}

func TestAFailedReloadKeepsTheLastGoodTreeAndShowsTheBanner(t *testing.T) {
	dir := t.TempDir()
	write := func(src string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "view.tsx"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(strings.Replace(boxView, "{WIDTH}", "200", 1))
	source, err := compile.Open(filepath.Join(dir, "view.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	host := script.New(script.WithSource(source))
	if err := host.Reload(); err != nil {
		t.Fatal(err)
	}
	w := openScriptWindow(t, host)
	w.clickBox(t, "box")
	if got, _ := host.Text("label"); got != "Clicked 1" {
		t.Fatalf("label = %q", got)
	}
	goodBox, _ := w.cx.DebugBounds("box")

	// Break the file (line 2, the unclosed tag) and inject the save.
	write("export function App() {\n\treturn <div id=\"box\"\n}\n")
	before := gpui.WindowDrawCount(w.window)
	if err := host.Reload(); err == nil {
		t.Fatal("the broken file reloaded")
	} else if !strings.Contains(err.Error(), "view.tsx:") {
		t.Fatalf("the error does not carry the .tsx file and line: %v", err)
	}
	if gpui.WindowDrawCount(w.window) == before {
		w.window.SimulateFrameRequest()
	}
	if _, err := gpui.DrawWindowFrame(w.window); err != nil {
		t.Fatal(err)
	}

	// The last good tree is still what the window shows.
	if b, ok := w.cx.DebugBounds("box"); !ok || b != goodBox {
		t.Fatalf("the last good tree was lost: box = %+v, want %+v", b, goodBox)
	}
	// The banner is mounted over the bottom of the window and carries
	// the error's file:line.
	banner, ok := w.cx.DebugBounds(script.ErrorBannerSelector)
	if !ok {
		t.Fatal("the error banner is not mounted")
	}
	viewport, _, _, err := gpui.TestWindowDrawGeometry(w.window)
	if err != nil {
		t.Fatal(err)
	}
	if banner.Origin.Y+banner.Size.Height < viewport.Height-1 {
		t.Fatalf("the banner is not at the window's bottom: %+v (viewport %v)", banner, viewport)
	}
	if banner.Origin.X > 1 || banner.Size.Width < viewport.Width-1 {
		t.Fatalf("the banner does not span the window: %+v (viewport %v)", banner, viewport)
	}
	if !strings.Contains(host.Err().Error(), "view.tsx:") {
		t.Fatalf("the banner's error has no .tsx position: %v", host.Err())
	}

	// A good save clears it and keeps state.
	write(strings.Replace(boxView, "{WIDTH}", "220", 1))
	w.reloadFrame(t, host)
	if _, ok := w.cx.DebugBounds(script.ErrorBannerSelector); ok {
		t.Fatal("the banner is still mounted after a good reload")
	}
	if b, ok := w.cx.DebugBounds("box"); !ok || b.Size.Width != 220 {
		t.Fatalf("box after the good reload = %+v, want width 220", b)
	}
	w.clickBox(t, "box")
	if got, _ := host.Text("label"); got != "Clicked 2" {
		t.Fatalf("label after the good reload = %q, want %q", got, "Clicked 2")
	}
}

func TestFragmentChildrenLowerAsTheirParentsChildren(t *testing.T) {
	src := `
export function App() {
	return (
		<div id="wrap" style={{ display: "flex", gap: 8 }}>
			<Fragment>
				<div id="a" style={{ width: 50, height: 20, background: "#ff0000" }} />
				<div id="b" style={{ width: 50, height: 20, background: "#00ff00" }} />
			</Fragment>
		</div>
	);
}
`
	host := script.New()
	mustLoad(t, host, src)
	w := openScriptWindow(t, host)
	a, ok := w.cx.DebugBounds("a")
	if !ok {
		t.Fatal("no bounds for a")
	}
	b, ok := w.cx.DebugBounds("b")
	if !ok {
		t.Fatal("no bounds for b")
	}
	// Spliced: a and b are the wrap's children, so the 8px gap applies
	// between them. A wrapper div would put b at a's right edge.
	if gap := b.Origin.X - (a.Origin.X + a.Size.Width); gap != 8 {
		t.Fatalf("b follows a at %v, want the 8 gap of the parent (fragment not spliced)", gap)
	}
}

func mustLoad(t testing.TB, h *script.Host, src string) {
	t.Helper()
	s := &srcView{src: src}
	h.SetSource(s)
	if err := h.Reload(); err != nil {
		t.Fatalf("load: %v", err)
	}
}
