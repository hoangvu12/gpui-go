//go:build windows

package script_test

// The script host's behaviour (ticket 62, ADR 0003): the .tsx view runs
// through esbuild and goja and describes a tree; keyed state survives
// reloads; bound values update without re-describing; useState
// re-describes (the documented slow path); a failed load keeps the last
// good tree and reports the .tsx file and line.
//
// These tests are windowless: they compile real .tsx through the
// compile package and observe the host through its public API (Text,
// Get, DescribeCount, Err, Click — zgui's TestApp surface). The reload
// flow is the dev one: the source changes and the change notification
// is injected as Host.Reload.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui/script"
	"github.com/hoangvu12/gpui-go/gpui/script/compile"
)

// counterView is the shared test view: a counter bound to keyed state
// (the fast path) and a row selected through useState (the slow path,
// on purpose: the spec keeps both paths and steers hot paths to bound
// values).
const counterView = `
export function App() {
	const clicks = state("clicks", 0);
	const [selected, setSelected] = useState("selected", -1);
	return (
		<div id="root" style={{ width: 200, height: "100%", background: "#111827" }}>
			<div
				id="counter"
				className="rounded-md"
				style={{ padding: "6px 8px", color: "#f9fafb", fontSize: 13 }}
				onClick={() => clicks.set(clicks.get() + 1)}
			>
				<text>{bind(clicks, (n: number) => ` + "`Clicked ${n} times`" + `)}</text>
			</div>
			<div id="row-0" style={{ padding: 2 }} onClick={() => setSelected(0)}>
				<text>{selected === 0 ? "row 0 selected" : "row 0"}</text>
			</div>
		</div>
	);
}
`

// srcView is a settable dev Source over one in-memory .tsx: a test
// changes the source with Set and injects the save notification with
// Host.Reload (the same call the file watcher makes).
type srcView struct{ src string }

func (s *srcView) Compile() (script.Bundle, error) { return compile.String("view.tsx", s.src) }
func (s *srcView) Set(src string)                  { s.src = src }

// newHost loads the view into a fresh host through its dev source.
func newHost(t testing.TB, src string) (*script.Host, *srcView) {
	t.Helper()
	s := &srcView{src: src}
	h := script.New(script.WithSource(s))
	if err := h.Reload(); err != nil {
		t.Fatalf("load view.tsx: %v", err)
	}
	return h, s
}

// reload compiles the source and reloads the host (the injected change
// notification).
func reload(t testing.TB, h *script.Host, s *srcView, src string) error {
	t.Helper()
	s.Set(src)
	return h.Reload()
}

// loadBundleOf compiles a raw .tsx source and loads it, returning the
// error (for the failure-path tests).
func loadBundleOf(t testing.TB, h *script.Host, src string) error {
	t.Helper()
	b, err := compile.String("view.tsx", src)
	if err != nil {
		return err
	}
	return h.Load(b)
}

func TestLoadDescribesTheTreeOnce(t *testing.T) {
	h, _ := newHost(t, counterView)
	if got, ok := h.Text("counter"); !ok || got != "Clicked 0 times" {
		t.Fatalf("counter text = %q, ok=%v", got, ok)
	}
	if h.DescribeCount() != 1 {
		t.Fatalf("DescribeCount = %d, want 1 (the tree is described once per load)", h.DescribeCount())
	}
}

func TestABoundUpdateChangesTheTextWithoutReDescribing(t *testing.T) {
	h, _ := newHost(t, counterView)
	for range 3 {
		if err := h.Click("counter"); err != nil {
			t.Fatalf("click: %v", err)
		}
	}
	if got, _ := h.Text("counter"); got != "Clicked 3 times" {
		t.Fatalf("counter text = %q, want %q (the bound value recomputed)", got, "Clicked 3 times")
	}
	// The hot-path guard (the spec's "script calls stay at zero" class):
	// a bound update must not re-run App.
	if h.DescribeCount() != 1 {
		t.Fatalf("DescribeCount = %d, want 1 (a bound update re-described the tree)", h.DescribeCount())
	}
	// Keyed state lives in the host.
	if got := fmt.Sprint(h.Get("clicks")); got != "3" {
		t.Fatalf("state clicks = %v", got)
	}
}

func TestUseStateReDescribesTheTree(t *testing.T) {
	h, _ := newHost(t, counterView)
	// The slow path: setting a useState key re-runs App and describes
	// the tree again (the spec documents it as the path to avoid).
	if err := h.Click("row-0"); err != nil {
		t.Fatalf("click: %v", err)
	}
	if got, _ := h.Text("row-0"); got != "row 0 selected" {
		t.Fatalf("row text = %q, want %q (the re-described tree shows it)", got, "row 0 selected")
	}
	if h.DescribeCount() != 2 {
		t.Fatalf("DescribeCount = %d, want 2 (useState re-described the tree)", h.DescribeCount())
	}
}

func TestKeyedStateSurvivesReloads(t *testing.T) {
	h, s := newHost(t, counterView)
	for range 5 {
		if err := h.Click("counter"); err != nil {
			t.Fatalf("click: %v", err)
		}
	}
	if err := h.Click("row-0"); err != nil {
		t.Fatalf("click: %v", err)
	}
	// Reload the same source 30 times: state is host-owned, so it
	// survives; init matters only for new keys (zgui's model).
	for range 30 {
		if err := reload(t, h, s, counterView); err != nil {
			t.Fatalf("reload: %v", err)
		}
	}
	if got, _ := h.Text("counter"); got != "Clicked 5 times" {
		t.Fatalf("counter text after reloads = %q (keyed state lost)", got)
	}
	if err := h.Click("counter"); err != nil {
		t.Fatalf("click after reload: %v", err)
	}
	if got, _ := h.Text("counter"); got != "Clicked 6 times" {
		t.Fatalf("counter text = %q, want %q", got, "Clicked 6 times")
	}
	if got, _ := h.Text("row-0"); got != "row 0 selected" {
		t.Fatalf("row-0 text after reloads = %q (useState state lost)", got)
	}
}

func TestASyntaxErrorKeepsTheLastGoodTree(t *testing.T) {
	h, s := newHost(t, counterView)
	for range 5 {
		if err := h.Click("counter"); err != nil {
			t.Fatalf("click: %v", err)
		}
	}
	// Break the view: the reload fails and reports file:line, the last
	// good tree stays, and the error clears on the next good reload.
	broken := strings.Replace(counterView, "</div>\n\t\t\t<div id=\"row-0\"", "</div\n\t\t\t<div id=\"row-0\"", 1)
	err := reload(t, h, s, broken)
	if err == nil {
		t.Fatal("a syntax error reloaded")
	}
	if !strings.Contains(err.Error(), "view.tsx:") {
		t.Fatalf("the error does not name the .tsx file and line: %v", err)
	}
	if h.Err() == nil {
		t.Fatal("the host does not hold the load error (the banner's source)")
	}
	if got, _ := h.Text("counter"); got != "Clicked 5 times" {
		t.Fatalf("the last good tree was lost: counter text = %q", got)
	}
	// The tree still runs: a click still works.
	if err := h.Click("counter"); err != nil {
		t.Fatalf("click after failed reload: %v", err)
	}
	if got, _ := h.Text("counter"); got != "Clicked 6 times" {
		t.Fatalf("counter text = %q (the last good tree stopped updating)", got)
	}
	// A good reload clears the error.
	if err := reload(t, h, s, counterView); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if h.Err() != nil {
		t.Fatalf("the error did not clear: %v", h.Err())
	}
}

func TestARuntimeErrorMapsToTheTsxLine(t *testing.T) {
	src := strings.Replace(counterView,
		"onClick={() => clicks.set(clicks.get() + 1)}",
		"onClick={() => clicks.set(undefinedThing.count + 1)}", 1)
	h, _ := newHost(t, src)
	err := h.Click("counter")
	if err == nil {
		t.Fatal("a handler that throws did not report an error")
	}
	// goja maps the runtime error through the inline source map back to
	// the .tsx line (probe 09's DX observation).
	if !strings.Contains(err.Error(), "view.tsx:") {
		t.Fatalf("the handler error does not name the .tsx position: %v", err)
	}
	t.Logf("handler error: %v", err)
}

func TestAnUnsupportedStyleKeyReportsTheTsxLine(t *testing.T) {
	src := strings.Replace(counterView,
		`style={{ padding: "6px 8px", color: "#f9fafb", fontSize: 13 }}`,
		`style={{ padding: "6px 8px", durtion: 2, color: "#f9fafb", fontSize: 13 }}`, 1)
	h := script.New()
	s := &srcView{src: src}
	h.SetSource(s)
	err := h.Reload()
	if err == nil {
		t.Fatal("an unsupported style key loaded")
	}
	if !strings.Contains(err.Error(), "view.tsx:") {
		t.Fatalf("the describe error does not name the .tsx position: %v", err)
	}
	if !strings.Contains(err.Error(), "durtion") {
		t.Fatalf("the error does not name the unknown key: %v", err)
	}
	t.Logf("describe error: %v", err)
}

func TestAnUnknownClassNameReportsTheTsxLine(t *testing.T) {
	src := strings.Replace(counterView, `className="rounded-md"`, `className="rounded-md fake-class-xyz"`, 1)
	h := script.New()
	h.SetSource(&srcView{src: src})
	err := h.Reload()
	if err == nil {
		t.Fatal("an unknown class loaded")
	}
	if !strings.Contains(err.Error(), "view.tsx:") || !strings.Contains(err.Error(), "fake-class-xyz") {
		t.Fatalf("the error does not name the class and position: %v", err)
	}
	t.Logf("class error: %v", err)
}

func TestAnUnknownPropOrElementReportsTheTsxLine(t *testing.T) {
	for name, edit := range map[string]struct {
		old, new, needle string
	}{
		"unknown prop":    {`id="row-0"`, `id="row-0" title="hi"`, `title`},
		"unknown element": {`<div id="row-0"`, `<span id="row-0"`, `<span`},
	} {
		t.Run(name, func(t *testing.T) {
			src := strings.Replace(counterView, edit.old, edit.new, 1)
			if name == "unknown element" {
				// Close the swapped tag so only the element name is wrong
				// (the row's own close, not the counter's).
				src = strings.Replace(src,
					"<div id=\"row-0\" style={{ padding: 2 }} onClick={() => setSelected(0)}",
					"<span id=\"row-0\" style={{ padding: 2 }} onClick={() => setSelected(0)}", 1)
				src = strings.Replace(src,
					"</text>\n\t\t\t</div>\n\t\t</div>\n\t);",
					"</text>\n\t\t\t</span>\n\t\t</div>\n\t);", 1)
			}
			h := script.New()
			h.SetSource(&srcView{src: src})
			err := h.Reload()
			if err == nil {
				t.Fatal("an unknown prop/element loaded")
			}
			if !strings.Contains(err.Error(), "view.tsx:") {
				t.Fatalf("the error does not name the .tsx position: %v", err)
			}
			if !strings.Contains(err.Error(), edit.needle) {
				t.Fatalf("the error does not name the offender (%q): %v", edit.needle, err)
			}
			t.Logf("error: %v", err)
		})
	}
}

func TestHandlersReceiveDOMShapedEvents(t *testing.T) {
	src := strings.Replace(counterView,
		"onClick={() => clicks.set(clicks.get() + 1)}",
		"onClick={(e) => clicks.set(e.button + 100)}", 1)
	h, _ := newHost(t, src)
	if err := h.Click("counter"); err != nil {
		t.Fatalf("click: %v", err)
	}
	if got := fmt.Sprint(h.Get("clicks")); got != "100" {
		t.Fatalf("state clicks = %v, want 100 (button 0 of a left click)", got)
	}
}

func TestFragmentsDescribeAndSpliceChildren(t *testing.T) {
	src := `
export function App() {
	return (
		<div id="wrap" style={{ display: "flex", gap: 8 }}>
			<Fragment>
				<div id="a" />
				<div id="b" />
			</Fragment>
			<div id="c" />
		</div>
	);
}
`
	h, _ := newHost(t, src)
	// The tree describes; the fragment's children lower as the parent's
	// children (bounds-checked in the window tests, where the gap
	// between a and b is observable).
	if _, ok := h.Text("wrap"); !ok {
		t.Fatal("the wrap element was not described")
	}
}

func TestErrorHandlerReceivesLoadAndHandlerErrors(t *testing.T) {
	var seen []error
	s := &srcView{src: counterView}
	h := script.New(script.WithErrorHandler(func(err error) { seen = append(seen, err) }), script.WithSource(s))
	if err := h.Reload(); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 0 {
		t.Fatalf("a good load reported %d errors", len(seen))
	}
	// A failed reload.
	if err := reload(t, h, s, "<div"); err == nil {
		t.Fatal("a syntax error reloaded")
	}
	// A throwing handler.
	src := strings.Replace(counterView,
		"onClick={() => clicks.set(clicks.get() + 1)}",
		"onClick={() => { throw new TypeError(\"boom\"); }}", 1)
	if err := reload(t, h, s, src); err != nil {
		t.Fatal(err)
	}
	if err := h.Click("counter"); err == nil {
		t.Fatal("a throwing handler did not report")
	}
	if len(seen) != 2 {
		t.Fatalf("the error handler saw %d errors, want 2: %v", len(seen), seen)
	}
	if !strings.Contains(seen[1].Error(), "boom") {
		t.Fatalf("the handler error was not reported: %v", seen[1])
	}
}

func TestSetGlobalIsAvailableAcrossReloads(t *testing.T) {
	var calls []string
	src := strings.Replace(counterView, "onClick={() => clicks.set(clicks.get() + 1)}",
		"onClick={() => note(\"clicked\")}", 1)
	s := &srcView{src: src}
	h := script.New(script.WithSource(s))
	h.SetGlobal("note", func(s string) { calls = append(calls, s) })
	if err := h.Reload(); err != nil {
		t.Fatal(err)
	}
	// A reload builds a fresh VM; the app's globals are reinstalled.
	if err := reload(t, h, s, src); err != nil {
		t.Fatal(err)
	}
	if err := h.Click("counter"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0] != "clicked" {
		t.Fatalf("SetGlobal calls after reload = %v", calls)
	}
}

func TestTheBundleIsExportedForReleaseUse(t *testing.T) {
	// The release path: an app compiles its .tsx at build time (a build
	// step or go:generate calling the compile package) and embeds the
	// bundle; the host loads it with Load and never reads scripts from
	// disk.
	b, err := compile.String("view.tsx", counterView)
	if err != nil {
		t.Fatal(err)
	}
	h := script.New()
	if err := h.Load(b); err != nil {
		t.Fatal(err)
	}
	if got, _ := h.Text("counter"); got != "Clicked 0 times" {
		t.Fatalf("counter text = %q", got)
	}
}

func TestReloadWithoutASourceIsAProgrammingError(t *testing.T) {
	h := script.New()
	if err := h.Reload(); err == nil {
		t.Fatal("Reload without a source succeeded")
	}
}

func TestAppReturningNothingIsALoadError(t *testing.T) {
	h := script.New()
	err := loadBundleOf(t, h, "export function App() { return null; }")
	if err == nil {
		t.Fatal("App returning null loaded")
	}
	if !strings.Contains(err.Error(), "App returned") {
		t.Fatalf("the error does not explain itself: %v", err)
	}
	t.Logf("error: %v", err)
}
