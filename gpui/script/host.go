package script

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

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
// release build compiles at build time and embeds it, so the app ships
// one file and reads no scripts from disk.
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

// New returns an empty host (no tree yet; Element renders an empty
// view until a successful load).
func New(opts ...Option) *Host {
	h := &Host{
		state:     map[string]any{},
		byKey:     map[string][]*binding{},
		dirtyKeys: map[string]bool{},
		banner:    true,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// SetSource sets the dev source Reload compiles.
func (h *Host) SetSource(s Source) { h.source = s }

// SetGlobal installs a Go value as a script global (a function the
// script's handlers can call, an object it can read): the app's bridge
// from script events to Go. It is reinstalled on every (re)load,
// which builds a fresh runtime. Setting a value the host also defines
// (h, Fragment, useState, state, bind) is a panic: those are the
// host's.
func (h *Host) SetGlobal(name string, value any) {
	switch name {
	case "h", "Fragment", "useState", "state", "bind", "__el":
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

// Load runs a compiled bundle in a fresh goja runtime and describes the
// tree, keeping every keyed state value. On any failure the previous
// runtime, tree and bindings stay (the last good UI) and the error is
// recorded (Err, WithErrorHandler, the banner); the error clears on
// the next successful load.
func (h *Host) Load(b Bundle) error {
	vm := goja.New()
	h.install(vm)

	prg, err := goja.Compile(b.Name, b.JS, true)
	if err != nil {
		return h.fail(err)
	}
	if _, err := vm.RunProgram(prg); err != nil {
		return h.fail(err)
	}
	app, ok := goja.AssertFunction(vm.Get(GlobalName).ToObject(vm).Get("App"))
	if !ok {
		return h.fail(fmt.Errorf("%s: the bundle exports no App function (export function App() { ... })", b.Name))
	}

	oldVM := h.vm
	h.vm = vm
	if err := h.describe(app); err != nil {
		h.vm = oldVM
		return h.fail(err)
	}
	h.app = app
	h.err = nil
	h.notify()
	return nil
}

// Reload compiles the source and loads the result: the call a file
// watcher makes on save (the injected change notification in tests).
func (h *Host) Reload() error {
	if h.source == nil {
		return errors.New("script: Reload needs a source (New(WithSource(...)) or SetSource)")
	}
	b, err := h.source.Compile()
	if err != nil {
		return h.fail(err)
	}
	return h.Load(b)
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
// the probe's 1,053-node sidebar).
func (h *Host) NodeCount() int { return countNodes(h.root) }

// Get returns a keyed state value (test support; scripts read state
// through state(key).get()).
func (h *Host) Get(key string) any { return h.state[key] }

// Text returns the text content of the element with the given id, as
// the described tree holds it (test support, like zgui's
// TestApp::text).
func (h *Host) Text(id string) (string, bool) {
	n := findNode(h.root, id)
	if n == nil {
		return "", false
	}
	var b strings.Builder
	collectText(n, &b)
	return b.String(), true
}

// Click invokes the onClick handler of the element with the given id
// and applies what it changed — test support (zgui's TestApp::click):
// the same path a simulated click through hit testing takes, without a
// window. It reports the handler's error, if any.
func (h *Host) Click(id string) error {
	n := findNode(h.root, id)
	if n == nil {
		return fmt.Errorf("script: no element with id %q", id)
	}
	if n.OnClick == nil {
		return fmt.Errorf("script: element %q has no onClick", id)
	}
	return h.dispatch(n.OnClick, h.eventObject(0, 0, 0, gpui.Modifiers{}))
}

// ---------------------------------------------------------------------------
// The describe pass and the update paths
// ---------------------------------------------------------------------------

// describe runs App and replaces the tree and its bindings.
func (h *Host) describe(app goja.Callable) error {
	prevBindings, prevByKey := h.bindings, h.byKey
	h.bindings, h.byKey, h.autoID = nil, map[string][]*binding{}, 0
	h.rootDirty, h.dirtyKeys = false, map[string]bool{}
	h.describes++

	v, err := app(goja.Undefined())
	if err != nil {
		h.bindings, h.byKey = prevBindings, prevByKey
		return err
	}
	n, ok := v.Export().(*Node)
	if !ok {
		h.bindings, h.byKey = prevBindings, prevByKey
		return fmt.Errorf("App returned %s, want an element", jsTypeName(v))
	}
	h.root = n
	return nil
}

// dispatch calls a handler, then applies what it changed: a useState
// change describes the tree again; a state change recomputes only the
// bindings on its key. Either way the window is asked to redraw.
func (h *Host) dispatch(fn goja.Callable, ev goja.Value) error {
	if _, err := fn(goja.Undefined(), ev); err != nil {
		h.fail(err)
		h.flush()
		h.notify()
		return err
	}
	if err := h.flush(); err != nil {
		h.fail(err)
		h.notify()
		return err
	}
	h.notify()
	return nil
}

// flush applies pending state changes.
func (h *Host) flush() error {
	if h.rootDirty {
		h.rootDirty = false
		clear(h.dirtyKeys)
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
	v, err := b.fn(goja.Undefined(), h.vm.ToValue(h.state[b.key]))
	if err != nil {
		return err
	}
	b.node.Text = v.String()
	return nil
}

// fail records a load or handler error for the banner and the error
// handler, and returns it.
func (h *Host) fail(err error) error {
	h.err = err
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

// install defines the runtime's globals: the host's API, then the
// app's (SetGlobal).
func (h *Host) install(vm *goja.Runtime) {
	if _, err := vm.RunProgram(preludeProgram); err != nil {
		panic(err)
	}
	mustSet(vm, "__el", h.jsxElement)
	mustSet(vm, "useState", h.useState)
	mustSet(vm, "state", h.stateHandle)
	mustSet(vm, "bind", h.bind)
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
		h.dirtyKeys[key] = true
		return goja.Undefined()
	})
	return o
}

// bind declares a computed value: fn(value) becomes the text of the
// child position the binding is placed at, recomputed only when the
// state changes.
func (h *Host) bind(call goja.FunctionCall) goja.Value {
	o, ok := call.Argument(0).(*goja.Object)
	if !ok || o.Get("key") == nil {
		panic(h.vm.NewTypeError("bind's first argument must be state(key, init)"))
	}
	fn, ok := goja.AssertFunction(call.Argument(1))
	if !ok {
		panic(h.vm.NewTypeError("bind's second argument must be a function"))
	}
	return h.vm.ToValue(&binding{key: o.Get("key").String(), fn: fn})
}

// ---------------------------------------------------------------------------
// The JSX factory's Go half: __el(type, props, children)
// ---------------------------------------------------------------------------

// jsxElement builds one host element. Function components ran in JS
// (the h wrapper); this is where a div/text/Fragment becomes a Node.
func (h *Host) jsxElement(call goja.FunctionCall) goja.Value {
	n := &Node{}
	switch typ := call.Argument(0).String(); typ {
	case "div", "text":
	case "Fragment":
		n.kind = nodeFragment
	default:
		h.throwf("unknown element <%s> (the host has div and text)", typ)
	}

	var id, key string
	var extraChildren goja.Value
	if o, ok := call.Argument(1).(*goja.Object); ok && !goja.IsNull(call.Argument(1)) {
		for _, k := range o.Keys() {
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
			h.byKey[e.key] = append(h.byKey[e.key], e)
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
	if n.kind == nodeText {
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
