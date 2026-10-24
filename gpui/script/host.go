package script

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/hoangvu12/gpui-go/gpui"
)

// GlobalName is the IIFE global the compiled bundle's exports land on
// (esbuild's global-name); the host reads App from it.
const GlobalName = "gpui_script"

// ErrorBannerSelector is the debug selector of the error banner element
// (test support: bounds of the banner, like a fixture's data-id).
const ErrorBannerSelector = "script-error"

// Bundle is a compiled script: what esbuild produced and what a host
// runs. Development compiles it on demand (gpui/script/compile); a
// release build compiles at build time and embeds it — the Embedded-
// Bundle shape, see doc.go — so the app ships one file and reads no
// scripts from disk.
type Bundle struct {
	// Name is the bundle's file name, used for diagnostics that carry
	// no position (e.g. "a missing App export").
	Name string
	// JS is the compiled ES2017 code, with its inline source map (goja
	// reads it and maps runtime errors back to the .tsx).
	JS string
}

// Source is a development-time source of compiled bundles: whatever
// Reload compiles. compile.Dir (a directory of .tsx on disk) is the
// implementation; tests inject their own.
type Source interface {
	// Compile returns the bundle for the source's current state.
	Compile() (Bundle, error)
}

// prelude is the JS the host installs before the bundle runs: the JSX
// factory's JS half (function components run in JS; host elements are
// built in Go through __el) and Fragment.
const prelude = `
function Fragment(p, ...c) { return __el("Fragment", p, c); }
function h(t, p, ...c) {
	if (typeof t === "function") {
		p = Object.assign({}, p);
		p.children = c;
		return t(p);
	}
	return __el(t, p, c);
}
`

var preludeProgram = goja.MustCompile("gpui/script/prelude.js", prelude, false)

// Host runs one scripted UI: it owns the keyed state (which survives
// reloads), the current goja runtime and the described tree. All
// methods run on the thread that renders (see doc.go).
type Host struct {
	// Notify is called after an event, a binding update or a reload
	// changed what the window shows, so it redraws (script.View wires
	// it to Window.Refresh; set it yourself for other mounts).
	Notify func()

	source  Source
	globals map[string]any
	banner  bool
	onError func(error)

	state    map[string]any
	vm       *goja.Runtime
	app      goja.Callable
	root     *Node
	bindings []*binding
	byKey    map[string][]*binding

	rootDirty bool
	dirtyKeys map[string]bool

	autoID    int
	describes int

	err error

	// The host services (ticket 64). See scheduler.go and bridge.go.
	scheduler      Scheduler
	shared         SharedStore
	fetcher        Fetcher
	consoleSink    func(kind, text string)
	actions        map[string]ResolvedAction
	actionResolver ActionResolver

	// The plugin half (ticket 67, plugin.go and budget.go): slots is
	// the registry this host serves (the app host owns it, a plugin
	// host references the registry it was registered on), slotNodes
	// are this host's <slot> declarations (rebuilt every describe),
	// slotComps are a plugin host's slot components and their roots,
	// budget is the per-call time budget (plugins only), parked is a
	// failed plugin's inert state, and watchdog arms the budget.
	slots      *Slots
	slotNodes  map[string][]*Node
	slotComps  []slotComp
	isPlugin   bool
	pluginName string
	budget     time.Duration
	parked     bool
	watchdog   watchdog

	// The reload-time type check (ticket 63): see typecheck.go.
	typeCheck TypeChecker
	typeDiags func([]TypeDiagnostic)
	// reportedTypeErr is the last checker error already reported to
	// stderr: the same error is reported once per session (see
	// reportTypes).
	reportedTypeErr string

	// vmGen is the generation of the live VM: every Load builds a fresh
	// one, and the timers, frame callbacks and in-flight fetches a
	// script registered belong to the generation that made them — a
	// reload drops them (no callbacks into a dead runtime), a failed
	// reload keeps the old generation alive with its tree.
	vmGen uint64
	// inJS counts the Go→JS calls on the stack: a shared-state write
	// that happens inside one defers its flush to the turn's end.
	inJS int

	timers         map[int]*timer
	nextTimerID    int
	frameCallbacks []*frameCallback
	nextFrameID    int
	async          []*asyncWork
	// rejections holds the promises rejected with no handler yet, by
	// generation: they are reported (banner and the error hook) only if
	// nothing handles them by the end of the script turn that rejected
	// them, as a browser reports unhandled rejections at the end of the
	// microtask drain.
	rejections map[*goja.Promise]uint64

	// epoch is performance.now's origin when there is no scheduler.
	epoch time.Time
}

// Option configures a host at construction.
type Option func(*Host)

// WithoutBanner keeps the error banner out of Element: release builds
// report errors through WithErrorHandler instead (the banner is the
// development default).
func WithoutBanner() Option { return func(h *Host) { h.banner = false } }

// WithErrorHandler receives every load and handler error (an app hook
// for logging or crash reports; zgui's host.on_error).
func WithErrorHandler(f func(error)) Option {
	return func(h *Host) { h.onError = f }
}

// WithSource sets the dev source Reload compiles.
func WithSource(s Source) Option { return func(h *Host) { h.source = s } }

// WithScheduler wires the host to the app's foreground executor and
// clock (AppScheduler): timers, fetch and async-action completions
// marshal through it onto the host thread. Without one the host still
// loads and runs handlers; the globals that need it raise a named
// error. script.View installs the window's app automatically when it
// renders, so a mounted host needs this only for windowless use or a
// different clock.
func WithScheduler(s Scheduler) Option { return func(h *Host) { h.scheduler = s } }

// WithShared hands the host the app's shared state (NewShared): the
// script's shared(key, init) handles read and write it, Go reads and
// writes it directly, and the bindings on a key recompute on every
// write (no re-describe). Ticket 67 hands a plugin's host a gated view
// of the app's store instead.
func WithShared(s SharedStore) Option { return func(h *Host) { h.setShared(s) } }

// WithFetcher sets the transport the script's fetch uses. The default
// is HTTPFetcher (a plain net/http client, no credentials); a plugin
// host (ticket 67) gets its own, or none.
func WithFetcher(f Fetcher) Option { return func(h *Host) { h.fetcher = f } }

// WithConsoleSink routes the script's console output to the app
// (one call per console.log and friends, with the formatted text);
// the default writes one line to the app's stderr.
func WithConsoleSink(f func(kind, text string)) Option { return func(h *Host) { h.consoleSink = f } }

// WithActionResolver replaces how invoke/call resolve action names
// (ticket 67's plugin gate): the default answers from the host's own
// registrations, a resolver answers only what its grants allow, with a
// named missing-grant error.
func WithActionResolver(r ActionResolver) Option { return func(h *Host) { h.actionResolver = r } }

// New returns an empty host (no tree yet; Element renders an empty
// view until a successful load).
func New(opts ...Option) *Host {
	h := &Host{
		state:      map[string]any{},
		byKey:      map[string][]*binding{},
		dirtyKeys:  map[string]bool{},
		banner:     true,
		timers:     map[int]*timer{},
		rejections: map[*goja.Promise]uint64{},
		// fetch works out of the box (the spec's ask); the app installs
		// its own transport with WithFetcher, and ticket 67's plugin
		// hosts get their own, gated one (or none).
		fetcher: HTTPFetcher(),
		epoch:   time.Now(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// SetSource sets the dev source Reload compiles.
func (h *Host) SetSource(s Source) { h.source = s }

// Slots returns the app host's plugin registry, creating it on first
// use (the registry is the host's: its slot elements render the
// plugins registered on it, and its actions, shared store and fetch
// transport are what the plugins' grants reach):
//
//	slots := host.Slots()
//	slots.Add("clock", clockBundle, script.WithPluginGrants(script.Grants{...}))
//
// Call it on the app host only; a plugin host's slots are managed by
// the registry it was registered on (its own slot declarations do
// take contributions — a plugin can host a slot another fills).
func (h *Host) Slots() *Slots {
	if h.isPlugin {
		panic(errNoSlots)
	}
	if h.slots == nil {
		h.slots = &Slots{app: h}
	}
	return h.slots
}

// park takes a failed plugin out of rotation: its pending work —
// timers, frame callbacks, in-flight fetches and async actions — is
// cancelled so nothing calls into a runtime that no longer renders,
// and runJS drops its callbacks. Load is the recovery path (a reload
// un-parks on success).
func (h *Host) park() {
	h.parked = true
	for id := range h.timers {
		h.clearTimer(id)
	}
	h.frameCallbacks = nil
	for _, a := range h.async {
		if a.cancel != nil {
			a.cancel()
		}
	}
	h.async = nil
}

// SetGlobal installs a Go value as a script global (a function the
// script's handlers can call, an object it can read): the app's bridge
// from script events to Go. It is reinstalled on every (re)load,
// which builds a fresh runtime. Setting a value the host also defines
// (h, Fragment, useState, state, bind, and every global globals.go
// and bridge.go provide — setTimeout, fetch, console, call, invoke,
// shared, ...) is a panic: those are the host's.
var hostGlobalNames = map[string]bool{
	"h": true, "Fragment": true, "useState": true, "state": true, "bind": true, "__el": true,
	"setTimeout": true, "clearTimeout": true, "setInterval": true, "clearInterval": true,
	"queueMicrotask": true, "requestAnimationFrame": true, "cancelAnimationFrame": true,
	"performance": true, "console": true, "atob": true, "btoa": true,
	"TextEncoder": true, "TextDecoder": true, "URL": true, "URLSearchParams": true,
	"fetch": true, "call": true, "invoke": true, "shared": true,
}

// SetGlobal installs the value; see hostGlobalNames for the reserved
// names.
func (h *Host) SetGlobal(name string, value any) {
	if hostGlobalNames[name] {
		panic("script: SetGlobal(" + name + ") collides with the host's globals")
	}
	if h.globals == nil {
		h.globals = map[string]any{}
	}
	h.globals[name] = value
	if h.vm != nil {
		mustSet(h.vm, name, value)
	}
}

// setShared installs the shared store and subscribes this host to its
// writes (a write recomputes the bindings on that key).
func (h *Host) setShared(s SharedStore) {
	h.shared = s
	if a, ok := s.(interface{ attach(func(string)) }); ok {
		a.attach(h.sharedChanged)
	}
}

// Load runs a compiled bundle in a fresh goja runtime and describes the
// tree, keeping every keyed state value. On any failure the previous
// runtime, tree and bindings stay (the last good UI) and the error is
// recorded (Err, WithErrorHandler, the banner); the error clears on
// the next successful load.
//
// A successful load drops everything the replaced VM had pending — its
// timers and frame callbacks are cancelled, its in-flight fetches and
// async actions are aborted (their promises never settle: the browser
// aborts in-flight fetches on navigation the same way) — while the
// keyed state and the shared store keep their values. A failed load
// keeps the old VM, so its timers and completions keep running.
//
// A plugin host (Slots.Add) loads the same way but reads the plugin
// manifest instead of the App export, and a failed plugin load parks
// the plugin (no script of its runs until it reloads): its slots show
// the error placeholder while the app keeps running. The whole load —
// module run and describe — is one budgeted turn (budget.go).
func (h *Host) Load(b Bundle) error {
	oldVM, oldGen := h.vm, h.vmGen
	vm := goja.New()
	gen := oldGen + 1
	// The candidate VM is the host's from the start: everything the
	// module's top level does (state, shared, setTimeout) runs in it,
	// and its errors carry its positions. Every failure path restores
	// the old VM with its tree, timers and completions.
	h.vm, h.vmGen = vm, gen
	h.parked = false
	h.install(vm, gen)
	giveUp := func(err error) error {
		h.vm, h.vmGen = oldVM, oldGen
		h.dropGeneration(gen) // the failed VM's registrations die with it
		return h.fail(err)
	}

	prg, err := goja.Compile(b.Name, b.JS, true)
	if err != nil {
		return giveUp(err)
	}
	// One budgeted turn: the module run, the entry lookup and the
	// describe. For app hosts the budget is zero (no watchdog, no
	// overhead); for plugins a runaway top level or component is
	// interrupted and the load fails.
	if err := h.runBudgeted(vm, func() error {
		h.inJS++
		_, runErr := vm.RunProgram(prg)
		h.inJS--
		if runErr != nil {
			return runErr
		}
		if h.isPlugin {
			return h.enterPlugin(b)
		}
		app, ok := goja.AssertFunction(vm.Get(GlobalName).ToObject(vm).Get("App"))
		if !ok {
			return fmt.Errorf("%s: the bundle exports no App function (export function App() { ... })", b.Name)
		}
		if err := h.describe(app); err != nil {
			return err
		}
		h.app = app
		return nil
	}); err != nil {
		return giveUp(err)
	}
	h.err = nil
	h.dropGeneration(oldGen) // the replaced VM's pending work dies with it
	h.reportRejections()
	h.notify()
	return nil
}

// Reload compiles the source and loads the result: the call a file
// watcher makes on save (the injected change notification in tests).
// A reload that succeeds is followed by the type check WithTypeCheck
// installed (ADR 0003: debug builds check after every reload when tsc
// is present) — reported, never gating the running UI.
//
// The reload also reaches the plugins registered from sources (the
// dev shape, Slots.AddSource): the dev command's reload command
// reloads the whole .tsx surface in place. A plugin that fails to
// reload is contained to its placeholder — the app's reload still
// succeeds (see Slots.ReloadAll).
func (h *Host) Reload() error {
	if h.source == nil {
		return errors.New("script: Reload needs a source (New(WithSource(...)) or SetSource)")
	}
	b, err := h.source.Compile()
	if err != nil {
		return h.fail(err)
	}
	if err := h.Load(b); err != nil {
		return err
	}
	if h.slots != nil && !h.isPlugin {
		_ = h.slots.ReloadAll()
	}
	h.checkTypes()
	return nil
}

// Err returns the last load or handler error (nil after a good load):
// the message the banner shows, with the .tsx file and line.
func (h *Host) Err() error { return h.err }

// DescribeCount reports how many times App has run — once per load or
// reload, once per useState re-describe. Test support for hot-path
// guards (zgui's script_calls): a bound update must not move it.
func (h *Host) DescribeCount() int { return h.describes }

// NodeCount returns the size of the described tree, nodes and text
// runs (test support: the timing thresholds are stated at a tree size,
// the probe's 1,053-node sidebar). On the app host this includes the
// plugin trees spliced into its slots; on a plugin host, its roots.
func (h *Host) NodeCount() int {
	if !h.isPlugin {
		return countNodes(h.root)
	}
	c := 0
	for _, sc := range h.slotComps {
		c += countNodes(sc.root)
	}
	return c
}

// Get returns a keyed state value (test support; scripts read state
// through state(key).get()).
func (h *Host) Get(key string) any { return h.state[key] }

// Text returns the text content of the element with the given id, as
// the described tree holds it (test support, like zgui's
// TestApp::text). On the app host this spans the plugin trees spliced
// into its slots; on a plugin host, its own slot roots.
func (h *Host) Text(id string) (string, bool) {
	n := h.findNode(id)
	if n == nil {
		return "", false
	}
	var b strings.Builder
	collectText(n, &b)
	return b.String(), true
}

// findNode locates the element by id across this host's roots.
func (h *Host) findNode(id string) *Node {
	if !h.isPlugin {
		return findNode(h.root, id)
	}
	for _, sc := range h.slotComps {
		if n := findNode(sc.root, id); n != nil {
			return n
		}
	}
	return nil
}

// Click invokes the onClick handler of the element with the given id
// and applies what it changed — test support (zgui's TestApp::click):
// the same path a simulated click through hit testing takes, without a
// window. The element's own host runs the handler — a plugin's node
// dispatches into its runtime, under its budget. It reports the
// handler's error, if any.
func (h *Host) Click(id string) error {
	n := h.findNode(id)
	if n == nil {
		return fmt.Errorf("script: no element with id %q", id)
	}
	if n.OnClick == nil {
		return fmt.Errorf("script: element %q has no onClick", id)
	}
	owner := n.dispatchOwner(h)
	return owner.dispatch(n.OnClick, owner.eventObject(0, 0, 0, gpui.Modifiers{}))
}

// MouseDown invokes the onMouseDown handler of the element with the
// given id — test support, Click's twin for the other declared event:
// the same path a simulated left-button press through hit testing
// takes, without a window. It reports the handler's error, if any.
func (h *Host) MouseDown(id string) error {
	n := h.findNode(id)
	if n == nil {
		return fmt.Errorf("script: no element with id %q", id)
	}
	if n.OnMouseDown == nil {
		return fmt.Errorf("script: element %q has no onMouseDown", id)
	}
	owner := n.dispatchOwner(h)
	return owner.dispatch(n.OnMouseDown, owner.eventObject(0, 0, 0, gpui.Modifiers{}))
}

// ---------------------------------------------------------------------------
// The describe pass and the update paths
// ---------------------------------------------------------------------------

// describe runs App and replaces the tree and its bindings.
func (h *Host) describe(app goja.Callable) error {
	prevBindings, prevByKey := h.bindings, h.byKey
	prevSlotNodes, prevAuto := h.slotNodes, h.autoID
	h.bindings, h.byKey, h.autoID = nil, map[string][]*binding{}, 0
	h.rootDirty, h.dirtyKeys = false, map[string]bool{}
	h.slotNodes = nil
	h.describes++
	restore := func() {
		h.bindings, h.byKey = prevBindings, prevByKey
		h.slotNodes, h.autoID = prevSlotNodes, prevAuto
	}

	v, err := app(goja.Undefined())
	if err != nil {
		restore()
		return err
	}
	n, ok := v.Export().(*Node)
	if !ok {
		restore()
		return fmt.Errorf("App returned %s, want an element", jsTypeName(v))
	}
	h.root = n
	if h.slots != nil {
		h.slots.relinkAll() // the fresh tree's slot declarations take their contributions
	}
	return nil
}

// runJS runs one script turn: the body (a handler, a timer callback,
// a frame callback, a promise settlement), then what it changed — a
// useState change re-describes the tree, a state or shared change
// recomputes only the bindings on that key — then a redraw request.
// body's error (and flush's) is recorded like a handler error: the
// banner and the error hook, while the last good tree keeps running.
// Unhandled promise rejections are reported here too, after goja has
// drained the microtask queue at the end of the call (a browser
// reports them at the end of the microtask checkpoint).
//
// A plugin host runs its turn under the per-call budget (a runaway
// loop is interrupted and the plugin fails); a parked plugin — one
// that already failed — runs nothing until it reloads.
func (h *Host) runJS(body func() error) error {
	h.inJS++
	// A panic in a Go half-installed function must not leave the host
	// thinking a script turn is still on the stack.
	defer func() { h.inJS-- }()
	if h.parked {
		// The plugin is left out: its callbacks are dropped, not run.
		return nil
	}
	bodyErr := h.runBudgeted(h.vm, body)
	if bodyErr != nil {
		h.fail(bodyErr)
		h.flush()
		h.reportRejections()
		h.notify()
		return bodyErr
	}
	if err := h.flush(); err != nil {
		h.fail(err)
		h.reportRejections()
		h.notify()
		return err
	}
	h.reportRejections()
	h.notify()
	return nil
}

// dispatch calls a handler as one script turn (runJS).
func (h *Host) dispatch(fn goja.Callable, ev goja.Value) error {
	return h.runJS(func() error {
		_, err := fn(goja.Undefined(), ev)
		return err
	})
}

// flush applies pending state changes. A state or shared write marks
// one key dirty (state: the host's map, shared: the store); flush
// recomputes the bindings on those keys only. rootDirty (a useState
// write) re-describes the tree — for a plugin host, every slot root
// it fills.
func (h *Host) flush() error {
	if h.rootDirty {
		h.rootDirty = false
		clear(h.dirtyKeys)
		if h.isPlugin {
			return h.describePlugin(h.slotComps)
		}
		return h.describe(h.app)
	}
	var firstErr error
	for key := range h.dirtyKeys {
		for _, b := range h.byKey[key] {
			if err := h.compute(b); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	clear(h.dirtyKeys)
	return firstErr
}

// compute recomputes one binding into its text node.
func (h *Host) compute(b *binding) error {
	v, err := b.fn(goja.Undefined(), h.vm.ToValue(h.bindingValue(b)))
	if err != nil {
		return err
	}
	b.node.Text = v.String()
	return nil
}

// bindingValue reads what a binding is bound over: the host's state
// map for state(key) handles, the shared store for shared(key) ones.
func (h *Host) bindingValue(b *binding) any {
	if b.bucket == sharedBucket {
		v, err := h.shared.Read(b.key)
		if err != nil {
			return nil // compute's error path covers the report
		}
		return v
	}
	return h.state[b.key]
}

// dropGeneration kills the pending work of a VM generation: its timers
// are cancelled, its frame callbacks dropped, its in-flight fetches
// and async actions aborted (their promises never settle). Keyed state
// and the shared store are untouched.
func (h *Host) dropGeneration(gen uint64) {
	for id, t := range h.timers {
		if t.gen == gen {
			h.clearTimer(id)
		}
	}
	if len(h.frameCallbacks) > 0 {
		keep := h.frameCallbacks[:0]
		for _, c := range h.frameCallbacks {
			if c.gen != gen {
				keep = append(keep, c)
			}
		}
		h.frameCallbacks = keep
	}
	if len(h.async) > 0 {
		keep := h.async[:0]
		for _, a := range h.async {
			if a.gen != gen {
				keep = append(keep, a)
			} else if a.cancel != nil {
				a.cancel()
			}
		}
		h.async = keep
	}
	for p, g := range h.rejections {
		if g == gen {
			delete(h.rejections, p)
		}
	}
}

// reportRejections reports the promises that were rejected and are
// still unhandled at the end of a script turn (goja has drained the
// microtask queue by then): the banner and the error hook, like a
// handler error, while the last good tree keeps running. A handler
// attached later is too late — the browser's unhandled-rejection
// reporting has the same shape.
func (h *Host) reportRejections() {
	for p, gen := range h.rejections {
		if gen != h.vmGen {
			delete(h.rejections, p)
			continue
		}
		delete(h.rejections, p)
		reason := p.Result()
		h.fail(fmt.Errorf("unhandled promise rejection: %s", reason.String()))
	}
}

// trackRejection records a rejection (the tracker goja calls when a
// promise is rejected without a handler), or clears it when a handler
// arrives.
func (h *Host) trackRejection(p *goja.Promise, op goja.PromiseRejectionOperation, gen uint64) {
	switch op {
	case goja.PromiseRejectionReject:
		h.rejections[p] = gen
	case goja.PromiseRejectionHandle:
		delete(h.rejections, p)
	}
}

// fail records a load or handler error for the banner and the error
// handler, and returns it. A plugin host is parked with it: its
// pending work is cancelled and no script of its runs until it
// reloads (a failed plugin is left out, its slots show the
// placeholder).
func (h *Host) fail(err error) error {
	h.err = err
	if h.isPlugin {
		h.park()
	}
	if h.onError != nil {
		h.onError(err)
	}
	return err
}

func (h *Host) notify() {
	if h.Notify != nil {
		h.Notify()
	}
}

// ---------------------------------------------------------------------------
// The script's globals
// ---------------------------------------------------------------------------

// install defines the runtime's globals: the host's API (the prelude
// and globals.go's browser globals, bridge.go's Go bridge), then the
// app's (SetGlobal). gen is the VM's generation — the callbacks a
// script registers belong to it.
func (h *Host) install(vm *goja.Runtime, gen uint64) {
	if _, err := vm.RunProgram(preludeProgram); err != nil {
		panic(err)
	}
	// The Go halves first: browserPrelude wires the JS front ends to
	// them (__timer, __atob, __hostLog, ...) at run time.
	h.installGlobals(vm, gen)
	if _, err := vm.RunProgram(browserPreludeProgram); err != nil {
		panic(err)
	}
	vm.SetPromiseRejectionTracker(func(p *goja.Promise, op goja.PromiseRejectionOperation) {
		h.trackRejection(p, op, gen)
	})
	mustSet(vm, "__el", h.jsxElement)
	mustSet(vm, "useState", h.useState)
	mustSet(vm, "state", h.stateHandle)
	mustSet(vm, "bind", h.bind)
	mustSet(vm, "invoke", h.jsInvoke)
	mustSet(vm, "call", func(call goja.FunctionCall) goja.Value {
		return h.jsCall(call, gen)
	})
	mustSet(vm, "shared", h.jsShared)
	mustSet(vm, "fetch", func(call goja.FunctionCall) goja.Value {
		return h.jsFetch(call, gen)
	})
	for name, value := range h.globals {
		mustSet(vm, name, value)
	}
}

// useState is the React-like keyed state: [value, set]. Setting marks
// the root dirty; the next flush re-runs App and describes the tree
// again (the slow path — prefer state + bind on hot paths).
func (h *Host) useState(call goja.FunctionCall) goja.Value {
	key := call.Argument(0).String()
	if key == "" {
		panic(h.vm.NewTypeError("useState needs a key, like useState(\"selected\", -1)"))
	}
	if _, ok := h.state[key]; !ok {
		h.state[key] = call.Argument(1).Export()
	}
	set := func(call goja.FunctionCall) goja.Value {
		h.state[key] = call.Argument(0).Export()
		h.rootDirty = true
		return goja.Undefined()
	}
	return h.vm.ToValue([]any{h.state[key], set})
}

// stateHandle is the zgui-style keyed state: { key, get, set }. Setting
// marks only that key dirty; the flush recomputes its bindings.
func (h *Host) stateHandle(call goja.FunctionCall) goja.Value {
	key := call.Argument(0).String()
	if key == "" {
		panic(h.vm.NewTypeError("state needs a key, like state(\"clicks\", 0)"))
	}
	if _, ok := h.state[key]; !ok {
		h.state[key] = call.Argument(1).Export()
	}
	o := h.vm.NewObject()
	mustSet(o, "key", key)
	mustSet(o, "get", func(call goja.FunctionCall) goja.Value {
		return h.vm.ToValue(h.state[key])
	})
	mustSet(o, "set", func(call goja.FunctionCall) goja.Value {
		h.state[key] = call.Argument(0).Export()
		h.dirtyKeys[stateBucket+key] = true
		return goja.Undefined()
	})
	return o
}

// bind declares a computed value: fn(value) becomes the text of the
// child position the binding is placed at, recomputed only when the
// state changes. Both state(key, init) and shared(key, init) handles
// bind (the same shape); state buckets and shared buckets are distinct
// namespaces.
func (h *Host) bind(call goja.FunctionCall) goja.Value {
	o, ok := call.Argument(0).(*goja.Object)
	if !ok || o.Get("key") == nil {
		panic(h.vm.NewTypeError("bind's first argument must be state(key, init) or shared(key, init)"))
	}
	fn, ok := goja.AssertFunction(call.Argument(1))
	if !ok {
		panic(h.vm.NewTypeError("bind's second argument must be a function"))
	}
	bucket := stateBucket
	if o.Get("__shared") != nil {
		bucket = sharedBucket
	}
	return h.vm.ToValue(&binding{bucket: bucket, key: o.Get("key").String(), fn: fn})
}

// ---------------------------------------------------------------------------
// The JSX factory's Go half: __el(type, props, children)
// ---------------------------------------------------------------------------

// jsxElement builds one host element. Function components ran in JS
// (the h wrapper); this is where a div/text/slot/Fragment becomes a
// Node. The node records its host (owner) so its handlers dispatch in
// the runtime that built them — a plugin's nodes run in the plugin's
// VM, under its budget, even though they lower with the app's tree.
func (h *Host) jsxElement(call goja.FunctionCall) goja.Value {
	n := &Node{owner: h}
	slot := false
	switch typ := call.Argument(0).String(); typ {
	case "div", "text":
	case "Fragment":
		n.kind = nodeFragment
	case "slot":
		slot = true
	default:
		h.throwf("unknown element <%s> (the host has div, text and slot)", typ)
	}

	var id, key string
	var extraChildren goja.Value
	if o, ok := call.Argument(1).(*goja.Object); ok && !goja.IsNull(call.Argument(1)) {
		if slot {
			// The slot's name is its identity — which plugins fill it.
			// Parsed here rather than in the shared prop switch so `name`
			// stays a slot-only prop (div and text reject it).
			name := o.Get("name")
			if name == nil || goja.IsUndefined(name) || name.String() == "" {
				h.throwf("a slot needs a name, like <slot name=\"sidebar\" />")
			}
			n.slotName = name.String()
		}
		for _, k := range o.Keys() {
			if slot && k == "name" {
				continue // parsed above
			}
			v := o.Get(k)
			switch k {
			case "key":
				key = v.String()
			case "id":
				id = v.String()
			case "className":
				n.Class = v.String()
				if err := gpui.CheckClasses(n.Class); err != nil {
					h.throwf("className: %v", err)
				}
			case "style":
				if so, ok := v.(*goja.Object); ok {
					s, err := parseStyle(so)
					if err != nil {
						h.throwf("%v", err)
					}
					n.Style = s
				} else {
					h.throwf("style must be an object, like style={{ padding: 8 }}")
				}
			case "onClick":
				fn, ok := goja.AssertFunction(v)
				if !ok {
					h.throwf("onClick must be a function")
				}
				n.OnClick = fn
			case "onMouseDown":
				fn, ok := goja.AssertFunction(v)
				if !ok {
					h.throwf("onMouseDown must be a function")
				}
				n.OnMouseDown = fn
			case "children":
				extraChildren = v
			default:
				h.throwf("unknown prop %q (the host has key, id, className, style, onClick and onMouseDown)", k)
			}
		}
	}
	if id != "" {
		n.ID, n.selector = id, id
	} else if key != "" {
		n.ID = key
	}
	// Interactive elements need an identity (the pinned stateful
	// element methods); give them one when the script did not.
	if (n.OnClick != nil || n.OnMouseDown != nil || n.Style.overflowYScroll()) && n.ID == "" {
		h.autoID++
		n.ID = "auto-" + strconv.Itoa(h.autoID)
	}
	h.addChildren(n, []goja.Value{call.Argument(2)})
	if extraChildren != nil {
		h.addChildren(n, []goja.Value{extraChildren})
	}
	if slot {
		// The JSX children are the slot's fallback (shown when no
		// registered plugin fills it, like a web component's slot); the
		// registry replaces them with the contributions when there are
		// any, and restores them when the last plugin is removed.
		n.slotFallback = n.Children
		if h.slotNodes == nil {
			h.slotNodes = map[string][]*Node{}
		}
		h.slotNodes[n.slotName] = append(h.slotNodes[n.slotName], n)
	}
	return h.vm.ToValue(n)
}

// addChildren flattens JSX children into the node: elements, text,
// bindings, arrays and false/null/undefined (skipped, so
// {cond && <x />} works).
func (h *Host) addChildren(n *Node, kids []goja.Value) {
	for _, k := range kids {
		if k == nil || goja.IsNull(k) || goja.IsUndefined(k) {
			continue
		}
		if arr, ok := k.(*goja.Object); ok && arr.ClassName() == "Array" {
			l := int(arr.Get("length").ToInteger())
			sub := make([]goja.Value, l)
			for i := range l {
				sub[i] = arr.Get(strconv.Itoa(i))
			}
			h.addChildren(n, sub)
			continue
		}
		switch e := k.Export().(type) {
		case *Node:
			n.Children = append(n.Children, e)
		case *binding:
			t := &Node{kind: nodeText}
			e.node = t
			h.bindings = append(h.bindings, e)
			h.byKey[e.bucket+e.key] = append(h.byKey[e.bucket+e.key], e)
			if err := h.compute(e); err != nil {
				h.throwf("bind: %v", err)
			}
			n.Children = append(n.Children, t)
		case bool:
			// {cond && <x/>} renders nothing when false.
		default:
			n.Children = append(n.Children, &Node{kind: nodeText, Text: k.String()})
		}
	}
}

// throwf raises a describe-time error as a JS TypeError, prefixed with
// the .tsx position of the element being described (goja's call stack
// at the failing __el call, mapped through the inline source map).
func (h *Host) throwf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if pos := h.callerPosition(); pos != "" {
		msg = pos + ": " + msg
	}
	panic(h.vm.NewTypeError("%s", msg))
}

// callerPosition returns the first .tsx position on the JS call stack
// (the JSX call site of the element being described).
func (h *Host) callerPosition() string {
	for _, f := range h.vm.CaptureCallStack(8, nil) {
		p := f.Position()
		if p.Filename != "" && p.Line > 0 &&
			(strings.HasSuffix(p.Filename, ".tsx") || strings.HasSuffix(p.Filename, ".ts")) {
			return fmt.Sprintf("%s:%d:%d", p.Filename, p.Line, p.Column)
		}
	}
	return ""
}

// eventObject builds the DOM-shaped event handlers receive.
func (h *Host) eventObject(button int, x, y float32, m gpui.Modifiers) goja.Value {
	o := h.vm.NewObject()
	mustSet(o, "button", button)
	mustSet(o, "x", x)
	mustSet(o, "y", y)
	mustSet(o, "shiftKey", m.Shift)
	mustSet(o, "ctrlKey", m.Control)
	mustSet(o, "altKey", m.Alt)
	mustSet(o, "metaKey", m.Platform)
	return o
}

// ---------------------------------------------------------------------------
// Node lookup and helpers
// ---------------------------------------------------------------------------

func findNode(n *Node, id string) *Node {
	if n == nil {
		return nil
	}
	if n.ID == id {
		return n
	}
	for _, c := range n.Children {
		if f := findNode(c, id); f != nil {
			return f
		}
	}
	return nil
}

func collectText(n *Node, b *strings.Builder) {
	if n.kind == nodeText || n.kind == nodePlaceholder {
		b.WriteString(n.Text)
	}
	for _, c := range n.Children {
		collectText(c, b)
	}
}

func countNodes(n *Node) int {
	if n == nil {
		return 0
	}
	c := 1
	for _, k := range n.Children {
		c += countNodes(k)
	}
	return c
}

func jsTypeName(v goja.Value) string {
	if v == nil {
		return "nothing"
	}
	if o, ok := v.(*goja.Object); ok {
		return "an object (" + o.ClassName() + ")"
	}
	if e := v.Export(); e != nil {
		return fmt.Sprintf("%v", e)
	}
	return "null"
}

// mustSet installs a Go value under a name on a runtime or object,
// panicking only if goja rejects the assignment (a scripted global is
// always writable, so this is a programming-error check).
func mustSet(vm objectSetter, name string, value any) {
	if err := vm.Set(name, value); err != nil {
		panic(err)
	}
}

// objectSetter is satisfied by *goja.Runtime and *goja.Object (both
// install named values).
type objectSetter interface {
	Set(name string, value any) error
}
