//go:build windows

package script_test

// Plugins (ticket 67, ADR 0003): apps declare slots (<slot name=.../>)
// and each plugin runs in its own goja runtime with a per-call time
// budget, contributing components to the slots its manifest declares.
// Grants gate what a plugin reaches (actions, shared state, network
// prefixes); a missing grant is a named error. Containment: a plugin
// that throws, loops past its budget or renders an invalid element is
// left out — an error placeholder in its slot with the .tsx file and
// line — while the app and the other plugins keep running. A plugin's
// memory and VM live exactly as long as its slot registration.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/gpui/script"
	"github.com/hoangvu12/gpui-go/gpui/script/compile"
)

// pluginAppView is the app: two slots, one with fallback children.
const pluginAppView = `
export function App() {
	return (
		<div id="root" style={{ width: 320, height: "100%", background: "#111827", padding: 4 }}>
			<div id="header" style={{ padding: 4, color: "#f9fafb", fontSize: 13 }}>header</div>
			<slot id="main-slot" name="main" style={{ padding: 4, background: "#1f2937" }} />
			<slot id="side-slot" name="side" style={{ padding: 4, background: "#1f2937" }}>no side plugin</slot>
		</div>
	);
}
`

// pluginA fills main and side, and keeps keyed state (the same key
// name pluginB uses: the two run in separate runtimes, so the values
// must stay independent).
const pluginA = `
export function Main() {
	const clicks = state("clicks", 0);
	return (
		<div id="a-main" style={{ padding: 4, color: "#f9fafb" }} onClick={() => clicks.set(clicks.get() + 1)}>
			<text>{bind(clicks, (n: number) => ` + "`A main ${n}`" + `)}</text>
		</div>
	);
}
export function Side() {
	return <div id="a-side" style={{ padding: 4, color: "#fecaca" }}>A side</div>;
}
export const plugin = { slots: { main: Main, side: Side } };
`

// pluginB fills main only, with the same state key as pluginA.
const pluginB = `
export function Main() {
	const clicks = state("clicks", 0);
	return (
		<div id="b-main" style={{ padding: 4, color: "#bbf7d0" }} onClick={() => clicks.set(clicks.get() + 1)}>
			<text>{bind(clicks, (n: number) => ` + "`B main ${n}`" + `)}</text>
		</div>
	);
}
export const plugin = { slots: { main: Main } };
`

// newPluginApp loads an app view and returns its host and slot
// registry, optionally with a shared store and a scheduler (for
// plugin timers and fetch).
func newPluginApp(t testing.TB, src string, opts ...script.Option) (*script.Host, *script.Slots) {
	t.Helper()
	h := script.New(opts...)
	b, err := compile.String("view.tsx", src)
	if err != nil {
		t.Fatalf("compile view.tsx: %v", err)
	}
	if err := h.Load(b); err != nil {
		t.Fatalf("load view.tsx: %v", err)
	}
	return h, h.Slots()
}

// addPlugin registers a plugin bundle from raw .tsx source.
func addPlugin(t testing.TB, slots *script.Slots, name, src string, opts ...script.PluginOption) {
	t.Helper()
	b, err := compile.String("plugin.tsx", src)
	if err != nil {
		t.Fatalf("compile plugin.tsx: %v", err)
	}
	if err := slots.Add(name, b, opts...); err != nil {
		t.Fatalf("add plugin %q: %v", name, err)
	}
}

// pluginSrc is a settable dev Source over one in-memory plugin .tsx,
// compiled under the plugin's file name (srcView's view.tsx twin).
type pluginSrc struct{ src string }

func (s *pluginSrc) Compile() (script.Bundle, error) { return compile.String("plugin.tsx", s.src) }
func (s *pluginSrc) Set(src string)                  { s.src = src }

func TestTwoPluginsFillTheSameAndDifferentSlotsInSeparateVMs(t *testing.T) {
	h, slots := newPluginApp(t, pluginAppView)
	addPlugin(t, slots, "a", pluginA)
	addPlugin(t, slots, "b", pluginB)

	// The same slot: both contributions, registration order. Different
	// slots: each plugin fills what it declared.
	for id, want := range map[string]string{
		"a-main":    "A main 0",
		"b-main":    "B main 0",
		"a-side":    "A side",
		"side-slot": "A side",
		"header":    "header",
	} {
		if got, ok := h.Text(id); !ok || got != want {
			t.Errorf("Text(%q) = %q, ok=%v, want %q", id, got, ok, want)
		}
	}
	// Two plugins, two runtimes: the same state key is two values. The
	// fallback is gone while the side plugin fills the slot.
	for range 2 {
		if err := h.Click("a-main"); err != nil {
			t.Fatalf("click a-main: %v", err)
		}
	}
	if err := h.Click("b-main"); err != nil {
		t.Fatalf("click b-main: %v", err)
	}
	if got, _ := h.Text("a-main"); got != "A main 2" {
		t.Errorf("a-main text = %q, want %q (plugin a's own state)", got, "A main 2")
	}
	if got, _ := h.Text("b-main"); got != "B main 1" {
		t.Errorf("b-main text = %q, want %q (plugin b's own state, not a's)", got, "B main 1")
	}
	// The registry reports both plugins; each has its own host.
	if got := strings.Join(slots.Names(), ","); got != "a,b" {
		t.Errorf("plugin names = %q, want %q", got, "a,b")
	}
	pa, _ := slots.Host("a")
	pb, _ := slots.Host("b")
	if pa == nil || pb == nil || pa == pb {
		t.Fatal("each plugin needs its own host (one goja runtime per plugin)")
	}
}

func TestASlotShowsItsFallbackUntilAPluginFillsIt(t *testing.T) {
	h, slots := newPluginApp(t, pluginAppView)
	// No plugin: the fallback children render.
	if got, _ := h.Text("side-slot"); got != "no side plugin" {
		t.Fatalf("fallback text = %q, want %q", got, "no side plugin")
	}
	addPlugin(t, slots, "a", pluginA)
	if got, _ := h.Text("side-slot"); got != "A side" {
		t.Fatalf("slot text with a plugin = %q, want %q (the fallback is replaced)", got, "A side")
	}
	// Unload: the slot falls back again.
	if !slots.Remove("a") {
		t.Fatal("Remove(a) reported nothing registered")
	}
	if got, _ := h.Text("side-slot"); got != "no side plugin" {
		t.Fatalf("fallback text after remove = %q, want %q (the fallback returns)", got, "no side plugin")
	}
	if _, ok := slots.Host("a"); ok {
		t.Fatal("the plugin's host survived its removal (VM lifetime must follow the registration)")
	}
}

func TestAPluginCanDeclareASlotAnotherPluginFills(t *testing.T) {
	// Nested slots: a plugin's contribution can itself host a slot.
	const shell = `
export function Main() {
	return (
		<div id="shell" style={{ padding: 4 }}>
			<div id="shell-label">shell</div>
			<slot id="inner" name="tools" style={{ padding: 4, background: "#374151" }} />
		</div>
	);
}
export const plugin = { slots: { main: Main } };
`
	const tool = `
export function Tools() {
	return <div id="tool">tool</div>;
}
export const plugin = { slots: { tools: Tools } };
`
	h, slots := newPluginApp(t, pluginAppView)
	addPlugin(t, slots, "shell", shell)
	addPlugin(t, slots, "tool", tool)
	for _, id := range []string{"shell", "shell-label", "tool"} {
		if _, ok := h.Text(id); !ok {
			t.Errorf("the nested contribution lacks %q", id)
		}
	}
	// The inner slot's own fallback (none here) shows without plugins.
	if !slots.Remove("tool") {
		t.Fatal("Remove(tool) reported nothing registered")
	}
	if _, ok := h.Text("tool"); ok {
		t.Error("the removed plugin's content is still rendered")
	}
	if _, ok := h.Text("shell"); !ok {
		t.Error("the shell plugin lost its tree when another plugin was removed")
	}
}

func TestPluginsRenderOrderAndRespondThroughAWindow(t *testing.T) {
	// The windowed path: slot contributions lower with the app's tree,
	// a click through real hit testing runs the plugin's handler in its
	// own runtime, and the update redraws the window.
	ta := gpui.NewTestApp()
	t.Cleanup(func() { ta.App().Shutdown() })
	h, slots := newPluginApp(t, pluginAppView, script.WithScheduler(script.AppScheduler(ta.App())))
	addPlugin(t, slots, "a", pluginA)
	addPlugin(t, slots, "b", pluginB)

	w := openScriptWindowWithApp(t, h, ta)
	// Both contributions are in the slot, in registration order (the
	// slot lays its children out left to right).
	ab, _ := w.cx.DebugBounds("a-main")
	bb, _ := w.cx.DebugBounds("b-main")
	if !ab.Contains(gpui.Point{X: ab.Origin.X + 1, Y: ab.Origin.Y + ab.Size.Height/2}) {
		t.Fatalf("a-main bounds are empty: %+v", ab)
	}
	if ab.Origin.X >= bb.Origin.X {
		t.Errorf("plugin b renders before plugin a: a at %v, b at %v", ab.Origin, bb.Origin)
	}
	before := gpui.WindowDrawCount(w.window)
	w.clickBox(t, "a-main")
	if got, _ := h.Text("a-main"); got != "A main 1" {
		t.Fatalf("a-main text after the click = %q, want %q", got, "A main 1")
	}
	if got, _ := h.Text("b-main"); got != "B main 0" {
		t.Fatalf("b-main text after a's click = %q, want %q (separate state)", got, "B main 0")
	}
	if gpui.WindowDrawCount(w.window) <= before {
		t.Fatal("the plugin's update did not redraw the window")
	}
}

func TestTheBudgetInterruptsARunawayPlugin(t *testing.T) {
	// A runaway loop in a handler: the per-call budget interrupts it
	// (goja's Interrupt), the plugin is left out with an error
	// placeholder carrying the .tsx position, and the app and the other
	// plugin keep running.
	const looper = `
export function Main() {
	return <div id="looper" style={{ padding: 4 }} onClick={() => { while (true) { } }}>looper</div>;
}
export const plugin = { slots: { main: Main } };
`
	h, slots := newPluginApp(t, pluginAppView)
	addPlugin(t, slots, "a", pluginA)
	addPlugin(t, slots, "looper", looper, script.WithPluginBudget(50*time.Millisecond))

	start := time.Now()
	err := h.Click("looper")
	if err == nil {
		t.Fatal("a runaway loop was not interrupted")
	}
	if !strings.Contains(err.Error(), "budget") {
		t.Fatalf("the error does not name the budget: %v", err)
	}
	if !strings.Contains(err.Error(), "plugin.tsx:") {
		t.Fatalf("the error does not name the .tsx position: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the interrupt took %v to land", elapsed)
	}
	// The plugin is left out: its slot shows the placeholder with the
	// budget message and position, not the looper's tree.
	placeholder, ok := h.Text(script.PluginErrorSelector)
	if !ok || !strings.Contains(placeholder, "budget") || !strings.Contains(placeholder, "plugin.tsx:") {
		t.Fatalf("the placeholder = %q, ok=%v, want the budget error with its position", placeholder, ok)
	}
	if _, still := h.Text("looper"); still {
		t.Fatal("the runaway plugin's tree is still rendered")
	}
	// The app and the other plugin keep working.
	if err := h.Click("a-main"); err != nil {
		t.Fatalf("click a-main after the runaway: %v", err)
	}
	if got, _ := h.Text("a-main"); got != "A main 1" {
		t.Fatalf("a-main text = %q, want %q (the app kept running)", got, "A main 1")
	}
	if got, _ := h.Text("header"); got != "header" {
		t.Fatalf("the app's own tree was lost: header = %q", got)
	}
	// The parked plugin runs no script: a later click on its (gone)
	// tree is impossible, and its host reports the failure.
	ph, _ := slots.Host("looper")
	if ph == nil || ph.Err() == nil {
		t.Fatal("the runaway plugin's host does not hold its failure")
	}
}

func TestARunawayLoopAtLoadTimeIsContained(t *testing.T) {
	const loopsForever = `
while (true) { }
export function Main() {
	return <div>never</div>;
}
export const plugin = { slots: { main: Main } };
`
	var reported []error
	h, slots := newPluginApp(t, pluginAppView)
	addPlugin(t, slots, "a", pluginA)
	b, err := compile.String("plugin.tsx", loopsForever)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	addErr := slots.Add("looper", b, script.WithPluginBudget(50*time.Millisecond),
		script.WithPluginErrorHandler(func(err error) { reported = append(reported, err) }))
	if addErr == nil || !strings.Contains(addErr.Error(), "budget") {
		t.Fatalf("a load-time runaway loop = %v, want a budget error", addErr)
	}
	// The failure is reported to the app's handler (the module never
	// ran, so the manifest — and the slots it fills — were never read:
	// there is no slot to place a placeholder in; the plugin is simply
	// left out until a reload of a fixed source recovers it).
	if len(reported) != 1 || !strings.Contains(reported[0].Error(), "budget") {
		t.Fatalf("the app's plugin error handler saw %v", reported)
	}
	if got, _ := h.Text("a-main"); got != "A main 0" {
		t.Fatalf("a-main text = %q (the app was taken down by the load)", got)
	}
	if _, ok := h.Text(script.PluginErrorSelector); ok {
		t.Fatal("a plugin that never established its slots left a placeholder")
	}
	// The app's own tree is intact and the other plugin responds.
	if err := h.Click("a-main"); err != nil {
		t.Fatalf("click a-main: %v", err)
	}
	if got, _ := h.Text("a-main"); got != "A main 1" {
		t.Fatalf("a-main text = %q, want %q", got, "A main 1")
	}
}

func TestAThrowingPluginIsLeftOutWhileTheAppKeepsRunning(t *testing.T) {
	const thrower = `
export function Main() {
	return <div id="thrower" style={{ padding: 4 }} onClick={() => { throw new TypeError("boom"); }}>thrower</div>;
}
export const plugin = { slots: { main: Main } };
`
	h, slots := newPluginApp(t, pluginAppView)
	addPlugin(t, slots, "a", pluginA)
	addPlugin(t, slots, "thrower", thrower)

	err := h.Click("thrower")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("the throw surfaced as %v, want the plugin's error", err)
	}
	// Contained: the placeholder shows in the slot (the other plugin's
	// contribution with it), and the app keeps running.
	placeholder, _ := h.Text(script.PluginErrorSelector)
	if !strings.Contains(placeholder, "boom") || !strings.Contains(placeholder, "plugin.tsx:") {
		t.Fatalf("the placeholder = %q, want the throw with its position", placeholder)
	}
	if got, _ := h.Text("a-main"); got != "A main 0" {
		t.Fatalf("a-main text = %q, want %q (the other plugin kept running)", got, "A main 0")
	}
	if err := h.Click("a-main"); err != nil {
		t.Fatalf("click a-main after the throw: %v", err)
	}
	// The failed plugin is parked: no script of its runs again.
	if ph, _ := slots.Host("thrower"); ph.Err() == nil {
		t.Fatal("the throwing plugin's host does not hold its failure")
	}
}

func TestAnInvalidElementFromAPluginIsAPlaceholder(t *testing.T) {
	const invalid = `
export function Main() {
	return <span id="bad">no such element</span>;
}
export const plugin = { slots: { main: Main } };
`
	h, slots := newPluginApp(t, pluginAppView)
	addPlugin(t, slots, "a", pluginA)
	b, err := compile.String("plugin.tsx", invalid)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if addErr := slots.Add("bad", b); addErr == nil {
		t.Fatal("an invalid element loaded")
	}
	placeholder, _ := h.Text(script.PluginErrorSelector)
	if !strings.Contains(placeholder, "span") || !strings.Contains(placeholder, "plugin.tsx:") {
		t.Fatalf("the placeholder = %q, want the element error with its position", placeholder)
	}
	if got, _ := h.Text("a-main"); got != "A main 0" {
		t.Fatalf("a-main text = %q (the app was taken down)", got)
	}
}

func TestTheGrantGateDeniesUngGrantedActionsAndAllowsGrantedOnes(t *testing.T) {
	// The app registers two actions and grants the plugin one of them:
	// the granted invoke works, the ungranted one is a named error (the
	// plugin is then failed and left out, its placeholder naming the
	// action and the plugin).
	const caller = `
export function Main() {
	return (
		<div id="go" style={{ padding: 4 }} onClick={() => { invoke("allowed"); }}>go</div>
	);
}
export const plugin = { slots: { main: Main } };
`
	var called []string
	h, slots := newPluginApp(t, pluginAppView)
	h.Action("allowed", func(args []any) (any, error) {
		called = append(called, "allowed")
		return "ok", nil
	})
	h.Action("secret", func(args []any) (any, error) {
		called = append(called, "secret")
		return nil, fmt.Errorf("the secret ran")
	})
	src := &srcView{src: caller}
	if err := slots.AddSource("caller", src,
		script.WithPluginGrants(script.Grants{Actions: []string{"allowed"}})); err != nil {
		t.Fatalf("add the plugin: %v", err)
	}

	// The granted action runs.
	if err := h.Click("go"); err != nil {
		t.Fatalf("click go: %v", err)
	}
	if len(called) != 1 || called[0] != "allowed" {
		t.Fatalf("the granted action did not run: %v", called)
	}
	// Reach for the ungranted one: a named error, the plugin is left
	// out, and the app keeps running.
	src.Set(strings.Replace(caller, `invoke("allowed")`, `invoke("secret")`, 1))
	if err := slots.Reload("caller"); err != nil {
		t.Fatalf("reload the plugin: %v", err)
	}
	err := h.Click("go")
	if err == nil {
		t.Fatal("an ungranted action was invoked")
	}
	if !strings.Contains(err.Error(), `"secret"`) || !strings.Contains(err.Error(), "plugin \"caller\" was not granted") {
		t.Fatalf("the denial = %v, want a named missing-grant error", err)
	}
	if len(called) != 1 {
		t.Fatalf("the ungranted action ran: %v", called)
	}
	placeholder, _ := h.Text(script.PluginErrorSelector)
	if !strings.Contains(placeholder, `"secret"`) || !strings.Contains(placeholder, "not granted") {
		t.Fatalf("the placeholder = %q, want the named grant error", placeholder)
	}
	if got, _ := h.Text("header"); got != "header" {
		t.Fatalf("the app's tree was lost: header = %q", got)
	}
}

func TestSharedStateIsGatedByGrants(t *testing.T) {
	// A granted plugin reads and writes the app's store (its bindings
	// recompute when the app writes); an ungranted key is a named
	// error at use.
	const reader = `
export function Main() {
	const note = shared("note", "initial");
	return (
		<div id="reader" style={{ padding: 4 }}>
			<text>{bind(note, (v: string) => "note: " + v)}</text>
		</div>
	);
}
export const plugin = { slots: { main: Main } };
`
	const snoop = `
export function Main() {
	const secret = shared("secret", "?");
	return <div id="snoop">{secret.get()}</div>;
}
export const plugin = { slots: { main: Main } };
`
	store := script.NewShared()
	store.Write("note", "hello")
	store.Write("secret", "classified")
	h, slots := newPluginApp(t, pluginAppView, script.WithShared(store))
	addPlugin(t, slots, "reader", reader,
		script.WithPluginGrants(script.Grants{Shared: []string{"note"}}))

	if got, _ := h.Text("reader"); got != "note: hello" {
		t.Fatalf("the granted shared value = %q, want %q", got, "note: hello")
	}
	// A Go write recomputes the plugin's binding (no re-describe).
	store.Write("note", "again")
	if got, _ := h.Text("reader"); got != "note: again" {
		t.Fatalf("the plugin did not see the app's write: %q", got)
	}
	// The ungranted plugin fails at load with the named error, while
	// the granted one keeps running.
	b, err := compile.String("plugin.tsx", snoop)
	if err != nil {
		t.Fatal(err)
	}
	if addErr := slots.Add("snoop", b); addErr == nil {
		t.Fatal("an ungranted shared key loaded")
	} else if !strings.Contains(addErr.Error(), `"secret"`) || !strings.Contains(addErr.Error(), "not granted") {
		t.Fatalf("the denial = %v, want a named missing-grant error", addErr)
	}
	if got, _ := h.Text("reader"); got != "note: again" {
		t.Fatalf("the granted plugin lost its tree when another failed: %q", got)
	}
	if got, _ := h.Text("header"); got != "header" {
		t.Fatalf("the app's tree was lost: header = %q", got)
	}
}

func TestTheNetworkGrantGatesFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "hello")
	}))
	defer srv.Close()

	const netView = `
export function Main() {
	const status = state("status", "idle");
	const err = state("err", "");
	return (
		<div
			id="go"
			style={{ padding: 4 }}
			onClick={() => {
				fetch("%s").then((r) => r.text()).then((t) => status.set("ok: " + t));
				fetch("http://127.0.0.1:9/forbidden").catch((e) => err.set(e.message));
			}}
		>
			<div id="status-line"><text>{bind(status, (v: string) => v)}</text></div>
			<div id="err-line"><text>{bind(err, (v: string) => v)}</text></div>
		</div>
	);
}
export const plugin = { slots: { main: Main } };
`
	ta := gpui.NewTestApp()
	t.Cleanup(func() { ta.App().Shutdown() })
	h, slots := newPluginApp(t, pluginAppView, script.WithScheduler(script.AppScheduler(ta.App())))
	addPlugin(t, slots, "net", fmt.Sprintf(netView, srv.URL),
		script.WithPluginGrants(script.Grants{Network: []string{srv.URL}}))

	if err := h.Click("go"); err != nil {
		t.Fatalf("click go: %v", err)
	}
	// The denied fetch rejects with the named error; the granted one
	// resolves through the app's transport. Both settle from the host
	// thread through the scheduler.
	wait(t, ta, func() bool {
		got, _ := h.Text("err-line")
		return strings.HasPrefix(got, "fetch http://127.0.0.1:9/forbidden: the plugin \"net\" was not granted")
	})
	wait(t, ta, func() bool {
		got, _ := h.Text("status-line")
		return got == "ok: hello"
	})
	if ph, _ := slots.Host("net"); ph.Err() != nil {
		t.Fatalf("the network-gated plugin failed: %v", ph.Err())
	}
}

func TestUnloadAndReloadAPlugin(t *testing.T) {
	h, slots := newPluginApp(t, pluginAppView)
	src := &pluginSrc{src: pluginA}
	if err := slots.AddSource("a", src); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := h.Click("a-main"); err != nil {
		t.Fatalf("click: %v", err)
	}
	if got, _ := h.Text("a-main"); got != "A main 1" {
		t.Fatalf("a-main text = %q, want %q", got, "A main 1")
	}

	// Reload with a changed source: keyed state keeps its values (the
	// plugin host's model, like the app's).
	src.Set(strings.Replace(pluginA, "A main", "A fresh", 1))
	if err := slots.Reload("a"); err != nil {
		t.Fatalf("reload a: %v", err)
	}
	if got, _ := h.Text("a-main"); got != "A fresh 1" {
		t.Fatalf("a-main text after reload = %q, want %q (keyed state kept)", got, "A fresh 1")
	}

	// A failed reload is contained: the placeholder replaces the tree
	// (a failed plugin is left out, not kept on screen), and a good
	// reload recovers.
	src.Set(strings.Replace(pluginA, "</div>", "</div", 1))
	if err := slots.Reload("a"); err == nil {
		t.Fatal("a broken source reloaded")
	}
	placeholder, _ := h.Text(script.PluginErrorSelector)
	if !strings.Contains(placeholder, "plugin.tsx:") {
		t.Fatalf("the placeholder = %q, want the .tsx file and line", placeholder)
	}
	src.Set(strings.Replace(pluginA, "A main", "A back", 1))
	if err := slots.Reload("a"); err != nil {
		t.Fatalf("reload a good source: %v", err)
	}
	if got, _ := h.Text("a-main"); got != "A back 1" {
		t.Fatalf("a-main text after recovery = %q, want %q", got, "A back 1")
	}

	// Unload drops the plugin with its VM; re-adding it starts fresh.
	if !slots.Remove("a") {
		t.Fatal("Remove(a) reported nothing registered")
	}
	if _, ok := h.Text("a-main"); ok {
		t.Fatal("the unloaded plugin's content is still rendered")
	}
	if err := slots.AddSource("a", src); err != nil {
		t.Fatalf("re-add a: %v", err)
	}
	if got, _ := h.Text("a-main"); got != "A back 0" {
		t.Fatalf("a fresh plugin's text = %q, want %q (state does not survive unload)", got, "A back 0")
	}
	if err := h.Click("a-main"); err != nil {
		t.Fatalf("click after re-add: %v", err)
	}
	if got, _ := h.Text("a-main"); got != "A back 1" {
		t.Fatalf("a-main text = %q, want %q", got, "A back 1")
	}
}

func TestPluginsReloadWithTheAppsDevReload(t *testing.T) {
	// The dev command's reload (cmd/tsxdev sends "reload", the app
	// runs Host.Reload) extends to the plugins' sources: the whole .tsx
	// surface reloads in place, keyed state kept on both sides.
	appSrc := &srcView{src: pluginAppView}
	h := script.New(script.WithSource(appSrc))
	if err := h.Reload(); err != nil {
		t.Fatalf("load app: %v", err)
	}
	slots := h.Slots()
	pluginSrc := &srcView{src: pluginA}
	if err := slots.AddSource("a", pluginSrc); err != nil {
		t.Fatalf("add a: %v", err)
	}
	if err := h.Click("a-main"); err != nil {
		t.Fatalf("click: %v", err)
	}

	// Edit the plugin's source only; the app's reload picks it up.
	pluginSrc.Set(strings.Replace(pluginA, "A main", "A dev", 1))
	if err := h.Reload(); err != nil {
		t.Fatalf("app reload: %v", err)
	}
	if got, _ := h.Text("a-main"); got != "A dev 1" {
		t.Fatalf("a-main text after the dev reload = %q, want %q (plugin reloaded, state kept)", got, "A dev 1")
	}
	// The app's own source still reloads through the same call.
	appSrc.Set(strings.Replace(pluginAppView, ">header<", ">edited<", 1))
	if err := h.Reload(); err != nil {
		t.Fatalf("app reload: %v", err)
	}
	if got, _ := h.Text("header"); got != "edited" {
		t.Fatalf("header text = %q, want %q", got, "edited")
	}
	if got, _ := h.Text("a-main"); got != "A dev 1" {
		t.Fatalf("a-main text = %q, want %q (the app reload kept the plugin's state)", got, "A dev 1")
	}
}

func TestAPluginGetsTheGlobalsTheAppHandsIt(t *testing.T) {
	var seen []string
	const greeter = `
export function Main() {
	return <div id="greet" style={{ padding: 4 }} onClick={() => note("clicked")}>greet</div>;
}
export const plugin = { slots: { main: Main } };
`
	h, slots := newPluginApp(t, pluginAppView)
	addPlugin(t, slots, "greet", greeter,
		script.WithPluginGlobals(map[string]any{"note": func(s string) { seen = append(seen, s) }}))
	if err := h.Click("greet"); err != nil {
		t.Fatalf("click greet: %v", err)
	}
	if len(seen) != 1 || seen[0] != "clicked" {
		t.Fatalf("the plugin's global calls = %v", seen)
	}
}

func TestAnUnloadedPluginsTimersStopWithIt(t *testing.T) {
	// The plugin's VM lifetime is its registration: unloading cancels
	// its pending work (timers), so nothing calls into the dropped
	// runtime afterwards.
	const ticker = `
let n = 0;
export function Main() {
	return <div id="tick">{n}</div>;
}
export const plugin = { slots: { main: Main } };
setInterval(() => { n += 1; }, 10);
`
	ta := gpui.NewTestApp()
	t.Cleanup(func() { ta.App().Shutdown() })
	h, slots := newPluginApp(t, pluginAppView, script.WithScheduler(script.AppScheduler(ta.App())))
	addPlugin(t, slots, "tick", ticker)
	ta.AdvanceClock(30)
	if !slots.Remove("tick") {
		t.Fatal("Remove(tick) reported nothing registered")
	}
	// The interval belonged to the dropped VM: advancing the clock
	// neither crashes nor resurrects the plugin.
	ta.AdvanceClock(100)
	if _, ok := h.Text("tick"); ok {
		t.Fatal("the unloaded plugin's content is still rendered")
	}
}
