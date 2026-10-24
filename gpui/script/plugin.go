package script

// Plugins (ticket 67, ADR 0003): apps declare slots and plugins fill
// them. A slot is the <slot name="..."/> element of the app's view —
// a div-like container whose children the registry owns. A plugin is
// a compiled bundle whose `plugin` export names the slots it fills:
//
//	export function Main() {
//		const clicks = state("clicks", 0);
//		return <div id="main" onClick={() => clicks.set(clicks.get() + 1)}>{bind(clicks, (n) => n)}</div>;
//	}
//	export const plugin = { slots: { main: Main } };
//
// The app registers it on its host's registry and grants what it may
// reach:
//
//	slots := host.Slots()
//	slots.Add("clock", clockBundle,
//		script.WithPluginGrants(script.Grants{Actions: []string{"time.now"]}),
//		script.WithPluginBudget(50*time.Millisecond))
//
// Each plugin runs in its own goja runtime — one Host, one VM, its
// own keyed state, bindings, timers and grants — with a per-call time
// budget (budget.go). A plugin that fails — a throw, a loop past its
// budget, an invalid element — is left out: its slots show an error
// placeholder with the .tsx file and line, and the app and the other
// plugins keep running. A plugin's memory and VM live exactly as long
// as its registration: Remove drops them (cancelling the plugin's
// pending work first, so nothing calls into the dropped runtime).
//
// Containment model, recorded in ADR 0003: time budget only, in
// process. goja cannot cap a VM's memory; the budget bounds how much
// script a plugin can run per turn, the failure paths bound what a
// broken plugin can do, and unloading bounds its lifetime — a plugin
// that allocates within its budget is reclaimed by the Go collector
// once (or while) unloaded, which is the memory story. A separate
// plugin process would add a real heap cap at the cost of an IPC
// boundary per contribution; that is future work if a plugin
// ecosystem needs it.
//
// Everything here runs on the thread that renders, like the rest of
// the host (doc.go's threading model).

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/hoangvu12/gpui-go/gpui"
)

// PluginErrorSelector is the debug selector (and node id) of a failed
// plugin's error placeholder — the plugin twin of the host's error
// banner: bounds key and text source for tests.
const PluginErrorSelector = "plugin-error"

// Grants are what a plugin may reach. Nothing is granted by default;
// a plugin reaching past its grants gets a named error (the action,
// key or URL, and the plugin's name).
type Grants struct {
	// Actions are the action names the plugin may invoke (the app
	// host's registrations — host.Action / host.AsyncAction).
	Actions []string
	// Shared are the shared-state keys the plugin may read and write
	// (the app's store, granted as whole keys).
	Shared []string
	// Network are URL prefixes the plugin's fetch may reach (a grant
	// is a plain prefix: "https://api.example.com" grants every URL
	// under it).
	Network []string
}

func (g Grants) allowsAction(name string) bool { return containsName(g.Actions, name) }
func (g Grants) allowsShared(key string) bool  { return containsName(g.Shared, key) }

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// allowsURL reports whether the plugin may fetch url: a granted
// prefix must match from the start of the URL.
func (g Grants) allowsURL(url string) bool {
	for _, prefix := range g.Network {
		if prefix != "" && strings.HasPrefix(url, prefix) {
			return true
		}
	}
	return false
}

// PluginOption configures one plugin registration.
type PluginOption func(*pluginReg)

// WithPluginGrants sets what the plugin may reach (nothing by
// default). See Grants.
func WithPluginGrants(g Grants) PluginOption {
	return func(r *pluginReg) { r.grants = g }
}

// WithPluginBudget sets the plugin's per-call time budget (the
// default is DefaultPluginBudget, 50 ms). Zero is an explicit trust
// decision: no budget, no watchdog.
func WithPluginBudget(d time.Duration) PluginOption {
	return func(r *pluginReg) { r.budget = d }
}

// WithPluginGlobals installs Go values as globals in the plugin's
// runtime (the app's own bridge into a plugin it trusts — the plugin
// does not inherit the app's globals, which would bypass its grants).
func WithPluginGlobals(values map[string]any) PluginOption {
	return func(r *pluginReg) { r.globals = values }
}

// WithPluginErrorHandler receives every failure of this plugin (a
// load, handler, budget or grant error) — the app's log or crash
// report hook for one plugin.
func WithPluginErrorHandler(f func(error)) PluginOption {
	return func(r *pluginReg) { r.onError = f }
}

// pluginReg is one registered plugin: the registration's identity and
// configuration, and the plugin's host (its goja runtime, keyed state
// and pending work). The host lives exactly as long as the
// registration.
type pluginReg struct {
	name    string
	source  Source // the dev source Reload recompiles (nil: bundles only)
	grants  Grants
	budget  time.Duration
	globals map[string]any
	onError func(error)
	host    *Host
}

// Slots is the app host's plugin registry: the slots the app's view
// declares, the plugins registered on them, and the grants each runs
// under. Create it with Host.Slots.
type Slots struct {
	app     *Host
	plugins []*pluginReg
}

// Names returns the registered plugin names in registration order.
func (s *Slots) Names() []string {
	names := make([]string, len(s.plugins))
	for i, p := range s.plugins {
		names[i] = p.name
	}
	return names
}

// Host returns the named plugin's script host — its goja runtime's
// owner (keyed state via Get, its failure via Err, its describe count,
// its text). Read-only observability: the plugin is driven by its
// slots and its own events, not by the app.
func (s *Slots) Host(name string) (*Host, bool) {
	p := s.find(name)
	if p == nil {
		return nil, false
	}
	return p.host, true
}

func (s *Slots) find(name string) *pluginReg {
	for _, p := range s.plugins {
		if p.name == name {
			return p
		}
	}
	return nil
}

// Add registers a plugin from a compiled bundle: it builds the
// plugin's host (a fresh goja runtime) and loads the bundle. The
// plugin is registered even when the load fails — a failing plugin is
// left out with an error placeholder in its slots, which is the
// containment the ticket asks for — and the error is returned (and
// reported through WithPluginErrorHandler). A duplicate name is a
// programming error (panics, like a duplicate action).
func (s *Slots) Add(name string, b Bundle, opts ...PluginOption) error {
	if name == "" {
		panic("script: a plugin needs a name")
	}
	if s.find(name) != nil {
		panic("script: a plugin named " + name + " is already registered")
	}
	r := &pluginReg{name: name, budget: DefaultPluginBudget}
	for _, opt := range opts {
		opt(r)
	}
	s.start(r)
	s.plugins = append(s.plugins, r)
	err := r.host.Load(b)
	s.changed(r)
	return err
}

// AddSource registers a plugin from a development source (the shape
// Reload and the app's Reload-driven dev reload recompile): the first
// load compiles the source, and the registration keeps it.
func (s *Slots) AddSource(name string, src Source, opts ...PluginOption) error {
	if src == nil {
		panic("script: AddSource needs a source")
	}
	r := &pluginReg{name: name, source: src, budget: DefaultPluginBudget}
	for _, opt := range opts {
		opt(r)
	}
	b, err := src.Compile()
	if err != nil {
		// A compile error registers the plugin in its failed state (the
		// module never ran, so the manifest — and the slots it fills —
		// were never read: there is no slot to place a placeholder in;
		// the failure reaches the app through this error and
		// WithPluginErrorHandler). A reload of a fixed source recovers
		// it.
		s.start(r)
		s.plugins = append(s.plugins, r)
		s.changed(r)
		return r.host.fail(err)
	}
	s.start(r)
	s.plugins = append(s.plugins, r)
	err = r.host.Load(b)
	s.changed(r)
	return err
}

// start builds the plugin's host: its own goja runtime, gated on the
// app's actions, shared store and fetch transport, under its budget.
// Its notifications (a plugin state change or reload) re-link the
// slots it fills and redraw the app.
func (s *Slots) start(r *pluginReg) {
	h := New(
		WithActionResolver(s.gateActions(r)),
		WithShared(s.gateShared(r)),
		WithFetcher(s.gateFetcher(r)),
		WithErrorHandler(r.onError),
	)
	h.isPlugin = true
	h.pluginName = r.name
	h.budget = r.budget
	h.slots = s
	h.Notify = func() { s.changed(r) }
	// A plugin's console output stays the app's stderr but carries the
	// plugin's name, so third-party noise is attributable.
	h.consoleSink = func(kind, text string) {
		fmt.Fprintf(os.Stderr, "[plugin %s] %s: %s\n", quotePlugin(r.name), kind, text)
	}
	for name, value := range r.globals {
		h.SetGlobal(name, value)
	}
	if s.app.scheduler != nil {
		h.scheduler = s.app.scheduler
	}
	r.host = h
}

// Remove unregisters the plugin: its pending work is cancelled (no
// timer or completion calls into the dropped runtime) and its host —
// the plugin's VM, keyed state and memory — becomes unreachable, so
// the collector reclaims it. The slots it filled fall back to their
// own content or the next plugin. Reports whether a plugin by that
// name was registered.
func (s *Slots) Remove(name string) bool {
	for i, p := range s.plugins {
		if p.name != name {
			continue
		}
		s.plugins = append(s.plugins[:i], s.plugins[i+1:]...)
		p.host.park() // the VM dies with the registration
		s.relinkAll()
		s.app.notify()
		return true
	}
	return false
}

// Reload recompiles the plugin's source and reloads its host in place
// (a fresh runtime; the plugin's keyed state keeps its values, like
// the app's). Plugins registered from a bare bundle cannot reload
// (there is nothing to recompile). A failed reload leaves the plugin
// failed — its slots show the placeholder — and the error is returned;
// the app keeps running.
func (s *Slots) Reload(name string) error {
	p := s.find(name)
	if p == nil {
		return fmt.Errorf("script: no plugin named %q", name)
	}
	if p.source == nil {
		return fmt.Errorf("script: the plugin %q has no source (registered with a bundle); re-register it to change it", name)
	}
	b, err := p.source.Compile()
	if err != nil {
		// The compile error fails the plugin (its last known slots —
		// read from a previous good manifest — show the placeholder; a
		// first load that never compiled has no slots, so the plugin is
		// simply absent).
		err = p.host.fail(err)
		s.changed(p)
		return err
	}
	err = p.host.Load(b)
	s.changed(p)
	return err
}

// ReloadAll reloads every plugin registered from a source — the
// plugin half of the app's dev reload (Host.Reload calls this, so the
// dev command's reload command reloads the whole .tsx surface). Each
// failure is contained to its plugin; the first error is returned.
func (s *Slots) ReloadAll() error {
	var first error
	for _, p := range s.plugins {
		if p.source == nil {
			continue
		}
		if err := s.Reload(p.name); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// changed is what a plugin's host reports: re-link every slot the
// plugin could fill (its contributions, or its placeholder) and ask
// the app to redraw.
func (s *Slots) changed(r *pluginReg) {
	s.relinkAll()
	s.app.notify()
}

// gateActions is the plugin's action resolver: only granted names,
// resolved from the app host's own registrations.
func (s *Slots) gateActions(r *pluginReg) ActionResolver {
	return func(name string) (ResolvedAction, error) {
		if !r.grants.allowsAction(name) {
			return ResolvedAction{}, fmt.Errorf("action %q: the plugin %q was not granted it", name, r.name)
		}
		if ra, ok := s.app.actions[name]; ok {
			return ra, nil
		}
		return ResolvedAction{}, fmt.Errorf("no host action named %q", name)
	}
}

// gatedShared is a plugin's view of the app's shared store: only the
// granted keys, with a named missing-grant error for the rest. It
// forwards store notifications (filtered to granted keys) so the
// plugin's bindings recompute when the app writes.
type gatedShared struct {
	inner  SharedStore
	plugin string
	grants Grants
}

// gateShared wraps the app's shared store for a plugin. A plugin host
// without a shared grant still gets a gate over the app's store (or a
// store-less gate when the app has none): every key is denied by
// name, which is the informative failure.
func (s *Slots) gateShared(r *pluginReg) SharedStore {
	return &gatedShared{inner: s.app.shared, plugin: r.name, grants: r.grants}
}

// Load implements SharedStore.
func (g *gatedShared) Load(key string, init any) (any, error) {
	if !g.grants.allowsShared(key) {
		return nil, fmt.Errorf("shared state %q: the plugin %q was not granted it", key, g.plugin)
	}
	if g.inner == nil {
		return nil, fmt.Errorf("shared state %q: the app has no shared store (build the app host with script.WithShared(script.NewShared()))", key)
	}
	return g.inner.Load(key, init)
}

// Read implements SharedStore.
func (g *gatedShared) Read(key string) (any, error) {
	if !g.grants.allowsShared(key) {
		return nil, fmt.Errorf("shared state %q: the plugin %q was not granted it", key, g.plugin)
	}
	if g.inner == nil {
		return nil, fmt.Errorf("shared state %q: the app has no shared store (build the app host with script.WithShared(script.NewShared()))", key)
	}
	return g.inner.Read(key)
}

// Write implements SharedStore.
func (g *gatedShared) Write(key string, value any) error {
	if !g.grants.allowsShared(key) {
		return fmt.Errorf("shared state %q: the plugin %q was not granted it", key, g.plugin)
	}
	if g.inner == nil {
		return fmt.Errorf("shared state %q: the app has no shared store (build the app host with script.WithShared(script.NewShared()))", key)
	}
	return g.inner.Write(key, value)
}

// attach forwards the store's notifications to the plugin host,
// filtered to the granted keys (the plugin can only have bindings on
// those; the filter keeps the plugin from hearing about the rest).
func (g *gatedShared) attach(notify func(string)) {
	if a, ok := g.inner.(interface{ attach(func(string)) }); ok {
		a.attach(func(key string) {
			if g.grants.allowsShared(key) {
				notify(key)
			}
		})
	}
}

// gateFetcher wraps the app's fetch transport (or the default one)
// with the plugin's network grant: a URL outside the granted
// prefixes rejects the fetch's promise with a named error before any
// bytes move.
func (s *Slots) gateFetcher(r *pluginReg) Fetcher {
	inner := s.app.fetcher
	if inner == nil {
		inner = HTTPFetcher()
	}
	return func(ctx context.Context, req FetchRequest) (FetchResponse, error) {
		if !r.grants.allowsURL(req.URL) {
			return FetchResponse{}, fmt.Errorf("the plugin %q was not granted it", r.name)
		}
		return inner(ctx, req)
	}
}

// ---------------------------------------------------------------------------
// The slot linkage
// ---------------------------------------------------------------------------

// relinkAll recomposes every declared slot's children from the
// registry: each plugin that fills the slot contributes its described
// root, or its error placeholder when it has failed, in registration
// order; a slot with no contributing plugin falls back to the children
// its own JSX gave it (a web component's slot fallback).
func (s *Slots) relinkAll() {
	for _, host := range s.hosts() {
		for name, nodes := range host.slotNodes {
			kids := s.contributions(name)
			for _, n := range nodes {
				if len(kids) > 0 {
					n.Children = kids
				} else {
					n.Children = n.slotFallback
				}
			}
		}
	}
}

// hosts is every host whose slot declarations take part: the app's
// and every plugin's (a plugin can declare a slot another plugin
// fills — nested slots).
func (s *Slots) hosts() []*Host {
	out := make([]*Host, 0, len(s.plugins)+1)
	out = append(out, s.app)
	for _, p := range s.plugins {
		out = append(out, p.host)
	}
	return out
}

// contributions is one slot's children: the plugins that fill it, in
// registration order, each healthy (its described root) or failed
// (its error placeholder).
func (s *Slots) contributions(name string) []*Node {
	var out []*Node
	for _, p := range s.plugins {
		root, fills := p.host.pluginRoot(name)
		if !fills {
			continue
		}
		if err := p.host.Err(); err != nil {
			out = append(out, &Node{
				kind:     nodePlaceholder,
				ID:       PluginErrorSelector,
				selector: PluginErrorSelector,
				Text:     fmt.Sprintf("plugin %q: %v", p.name, err),
			})
			continue
		}
		out = append(out, root)
	}
	return out
}

// wireScheduler propagates the app host's scheduler to the plugin
// hosts that lack one (the app's is wired on the window's first
// render, after plugins usually register).
func (s *Slots) wireScheduler() {
	if s.app.scheduler == nil {
		return
	}
	for _, p := range s.plugins {
		if p.host.scheduler == nil {
			p.host.scheduler = s.app.scheduler
		}
	}
}

// placeholderElement renders a failed plugin's contribution: the
// budget/banner styling, in flow (the slot's child), one line per
// line of the error.
func placeholderElement(msg string) *gpui.DivElement {
	b := gpui.Div().
		ID(PluginErrorSelector).
		DebugSelector(PluginErrorSelector).
		Bg(gpui.Rgb(0x7f1d1d)).
		TextColor(gpui.Rgb(0xfecaca)).
		TextSize(gpui.Px(12)).
		FontFamily("Consolas").
		P(gpui.Px(8))
	for _, line := range strings.Split(msg, "\n") {
		b = b.Child(line)
	}
	return b
}

// ---------------------------------------------------------------------------
// The plugin entry: the manifest and the slot components
// ---------------------------------------------------------------------------

// slotComp is one slot a plugin fills: the component and its
// described root.
type slotComp struct {
	slot string
	fn   goja.Callable
	root *Node
}

// readManifest reads the bundle's `plugin` export after the module
// ran: the object naming the slots the plugin fills.
func (h *Host) readManifest(b Bundle) ([]slotComp, error) {
	vm := h.vm
	exports := vm.Get(GlobalName).ToObject(vm)
	manifest := exports.Get("plugin")
	if goja.IsUndefined(manifest) || goja.IsNull(manifest) {
		return nil, fmt.Errorf("%s: the bundle exports no plugin manifest (export const plugin = { slots: { main: Main } })", b.Name)
	}
	mo, ok := manifest.(*goja.Object)
	if !ok {
		return nil, fmt.Errorf("%s: the plugin manifest must be an object, like { slots: { main: Main } }", b.Name)
	}
	slotsVal := mo.Get("slots")
	so, ok := slotsVal.(*goja.Object)
	if !ok {
		return nil, fmt.Errorf("%s: the plugin manifest's slots must be an object, like { slots: { main: Main } }", b.Name)
	}
	var comps []slotComp
	for _, name := range so.Keys() {
		fn, ok := goja.AssertFunction(so.Get(name))
		if !ok {
			return nil, fmt.Errorf("%s: plugin.slots.%s is not a function component", b.Name, name)
		}
		comps = append(comps, slotComp{slot: name, fn: fn})
	}
	if len(comps) == 0 {
		return nil, fmt.Errorf("%s: the plugin fills no slots (declare one: export const plugin = { slots: { main: Main } })", b.Name)
	}
	return comps, nil
}

// describePlugin runs every slot component and replaces the roots,
// atomically. A component that fails fails the plugin: the last good
// roots are not restored (a failed plugin is left out — its slots
// show the placeholder, not the stale tree), but the slot names stay
// recorded so the placeholder lands in the slots the plugin fills,
// and the bindings and slot declarations are restored for the reload
// that recovers it.
func (h *Host) describePlugin(comps []slotComp) error {
	prevBindings, prevByKey := h.bindings, h.byKey
	prevAuto, prevSlotNodes := h.autoID, h.slotNodes
	h.bindings, h.byKey, h.autoID = nil, map[string][]*binding{}, 0
	h.slotNodes = nil
	h.describes++
	restore := func() {
		h.slotComps = comps // the names stay: the placeholder needs them
		h.bindings, h.byKey = prevBindings, prevByKey
		h.autoID, h.slotNodes = prevAuto, prevSlotNodes
	}
	fresh := make([]slotComp, len(comps))
	for i, sc := range comps {
		props := h.vm.NewObject()
		mustSet(props, "slot", sc.slot)
		v, err := sc.fn(goja.Undefined(), props)
		if err != nil {
			restore()
			return err
		}
		n, ok := v.Export().(*Node)
		if !ok {
			restore()
			return fmt.Errorf("the plugin's %s component returned %s, want an element", sc.slot, jsTypeName(v))
		}
		fresh[i] = slotComp{slot: sc.slot, fn: sc.fn, root: n}
	}
	h.slotComps = fresh
	if h.slots != nil {
		h.slots.relinkAll()
	}
	return nil
}

// pluginRoot returns the plugin's described root for a slot, and
// whether the plugin fills it at all.
func (h *Host) pluginRoot(slot string) (*Node, bool) {
	for _, sc := range h.slotComps {
		if sc.slot == slot {
			return sc.root, true
		}
	}
	return nil, false
}

// enterPlugin runs after the module body: read the manifest and
// describe every slot component. All-or-nothing with the caller's
// give-up path.
func (h *Host) enterPlugin(b Bundle) error {
	comps, err := h.readManifest(b)
	if err != nil {
		return err
	}
	return h.describePlugin(comps)
}

// errNoSlots is the failure of a Slots() call on a plugin host.
var errNoSlots = errors.New("script: a plugin host's slots are managed by its registry; grant plugins through the app host")
