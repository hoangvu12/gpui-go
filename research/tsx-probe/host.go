// Package tsxprobe is a throwaway probe (research/browser-parity/09): a
// TSX view transformed by esbuild and run in goja, lowered to gpui-go
// elements. It follows zgui-script's model: the script describes the
// tree once per (re)load, the host owns keyed state that survives a
// reload, and the script runs again only for event handlers and bound
// (computed) values. A React-like useState is also offered: setting it
// re-runs App and describes the whole tree again.
package tsxprobe

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/evanw/esbuild/pkg/api"
	"github.com/hoangvu12/gpui-go/gpui"
)

// Node is one described element: a div, or a text run (Kind text).
type Node struct {
	text     bool
	Text     string
	ID       string
	Class    string
	Style    Style
	OnClick  goja.Callable
	Children []*Node
}

// binding is a bound (computed) value: fn(state[key]) becomes node's text.
type binding struct {
	key  string
	fn   goja.Callable
	node *Node
}

// Timings of the last load.
type Timings struct {
	Transform, VMStart, Compile, Run, Describe time.Duration
}

// Host owns the keyed state, the VM and the described tree.
type Host struct {
	state    map[string]any
	vm       *goja.Runtime
	app      goja.Callable
	Root     *Node
	bindings []*binding
	byKey    map[string][]*binding

	rootDirty bool
	dirtyKeys map[string]bool
	autoID    int

	// Notify is called after an event changed the tree (the view's
	// cx.Notify in a real app).
	Notify func()
	Last   Timings
	// Debug tags keyed elements with debug selectors (tests).
	Debug bool
}

// NewHost returns a host with empty state.
func NewHost() *Host {
	return &Host{state: map[string]any{}, dirtyKeys: map[string]bool{}}
}

// Transform lowers TSX to a JS IIFE (global __view) for goja.
func Transform(src, file string, target api.Target) (string, error) {
	r := api.Transform(src, api.TransformOptions{
		Loader:      api.LoaderTSX,
		JSX:         api.JSXTransform,
		JSXFactory:  "h",
		JSXFragment: "Fragment",
		Format:      api.FormatIIFE,
		GlobalName:  "__view",
		Target:      target,
		Sourcemap:   api.SourceMapInline,
		Sourcefile:  file,
	})
	if len(r.Errors) > 0 {
		e := r.Errors[0]
		if e.Location != nil {
			return "", fmt.Errorf("%s:%d:%d: %s", e.Location.File, e.Location.Line, e.Location.Column+1, e.Text)
		}
		return "", fmt.Errorf("%s", e.Text)
	}
	return string(r.Code), nil
}

// Load transforms and runs src in a fresh VM and describes the tree. The
// state is kept; on any error the previous VM and tree stay (the last
// good UI).
func (h *Host) Load(src, file string) error {
	var t Timings
	start := Now()
	js, err := Transform(src, file, api.ES2017)
	if err != nil {
		return err
	}
	t.Transform = Since(start)

	start = Now()
	vm := goja.New()
	h.install(vm)
	t.VMStart = Since(start)

	start = Now()
	prg, err := goja.Compile(file+".js", js, true)
	if err != nil {
		return err
	}
	t.Compile = Since(start)

	start = Now()
	if _, err := vm.RunProgram(prg); err != nil {
		return err
	}
	app, ok := goja.AssertFunction(vm.Get("__view").ToObject(vm).Get("App"))
	if !ok {
		return fmt.Errorf("%s: no exported App function", file)
	}
	t.Run = Since(start)

	old := h.vm
	h.vm = vm
	start = Now()
	if err := h.describeWith(app); err != nil {
		h.vm = old
		return err
	}
	t.Describe = Since(start)
	h.app = app
	h.Last = t
	return nil
}

// describeWith runs App and replaces the tree and its bindings.
func (h *Host) describeWith(app goja.Callable) error {
	prevB, prevK := h.bindings, h.byKey
	h.bindings, h.byKey, h.autoID = nil, map[string][]*binding{}, 0
	v, err := app(goja.Undefined())
	if err != nil {
		h.bindings, h.byKey = prevB, prevK
		return err
	}
	n, ok := v.Export().(*Node)
	if !ok {
		h.bindings, h.byKey = prevB, prevK
		return fmt.Errorf("App returned %T, want an element", v.Export())
	}
	h.Root = n
	return nil
}

// Dispatch calls an event handler, then applies what it changed: a
// useState change describes the tree again; a state change only
// recomputes the bindings on its key.
func (h *Host) Dispatch(fn goja.Callable, args ...goja.Value) error {
	if _, err := fn(goja.Undefined(), args...); err != nil {
		return err
	}
	if err := h.Flush(); err != nil {
		return err
	}
	if h.Notify != nil {
		h.Notify()
	}
	return nil
}

// Flush applies pending state changes.
func (h *Host) Flush() error {
	if h.rootDirty {
		h.rootDirty = false
		clear(h.dirtyKeys)
		return h.describeWith(h.app)
	}
	for key := range h.dirtyKeys {
		for _, b := range h.byKey[key] {
			if err := h.compute(b); err != nil {
				return err
			}
		}
	}
	clear(h.dirtyKeys)
	return nil
}

func (h *Host) compute(b *binding) error {
	v, err := b.fn(goja.Undefined(), h.vm.ToValue(h.state[b.key]))
	if err != nil {
		return err
	}
	b.node.Text = v.String()
	return nil
}

// Get returns a state value (for tests).
func (h *Host) Get(key string) any { return h.state[key] }

// install defines the script's globals: h, Fragment, useState, state, bind.
func (h *Host) install(vm *goja.Runtime) {
	must := func(err error) {
		if err != nil {
			panic(err)
		}
	}
	must(vm.Set("Fragment", "Fragment"))
	must(vm.Set("__el", h.jsx))
	// Function components run in JS while describing; host elements are
	// made in Go.
	_, err := vm.RunString(`function h(t, p, ...c) {
		if (typeof t === "function") { p = Object.assign({}, p); p.children = c; return t(p); }
		return __el(t, p, c);
	}`)
	must(err)
	must(vm.Set("useState", func(key string, init goja.Value) []any {
		if _, ok := h.state[key]; !ok {
			h.state[key] = init.Export()
		}
		set := func(v goja.Value) {
			h.state[key] = v.Export()
			h.rootDirty = true
		}
		return []any{h.state[key], set}
	}))
	must(vm.Set("state", func(key string, init goja.Value) *goja.Object {
		if _, ok := h.state[key]; !ok {
			h.state[key] = init.Export()
		}
		o := vm.NewObject()
		must(o.Set("key", key))
		must(o.Set("get", func() any { return h.state[key] }))
		must(o.Set("set", func(v goja.Value) {
			h.state[key] = v.Export()
			h.dirtyKeys[key] = true
		}))
		return o
	}))
	must(vm.Set("bind", func(s *goja.Object, fn goja.Callable) *binding {
		return &binding{key: s.Get("key").String(), fn: fn}
	}))
}

// jsx is the JSX factory: h(type, props, ...children).
func (h *Host) jsx(call goja.FunctionCall) goja.Value {
	vm := h.vm
	typ := call.Argument(0)
	props := call.Argument(1)
	kids := []goja.Value{call.Argument(2)}

	n := &Node{}
	switch t := typ.String(); t {
	case "div", "Fragment":
	case "text":
		// A text element is a div whose children are text runs.
	default:
		panic(vm.NewTypeError("unknown element <%s>", t))
	}
	if o, ok := props.(*goja.Object); ok && !goja.IsNull(props) && !goja.IsUndefined(props) {
		for _, k := range o.Keys() {
			v := o.Get(k)
			switch k {
			case "key":
				n.ID = v.String()
			case "className":
				n.Class = v.String()
			case "style":
				if so, ok := v.(*goja.Object); ok {
					if err := n.Style.parse(so); err != nil {
						panic(vm.NewTypeError("%v", err))
					}
				}
			case "onClick":
				fn, ok := goja.AssertFunction(v)
				if !ok {
					panic(vm.NewTypeError("onClick must be a function"))
				}
				n.OnClick = fn
			default:
				panic(vm.NewTypeError("unknown prop %q", k))
			}
		}
	}
	// Clicks and scrolling need an element ID (stateful elements).
	if (n.OnClick != nil || n.Style.overflowY) && n.ID == "" {
		h.autoID++
		n.ID = "auto-" + strconv.Itoa(h.autoID)
	}
	h.addChildren(n, kids)
	return vm.ToValue(n)
}

func (h *Host) addChildren(n *Node, kids []goja.Value) {
	for _, k := range kids {
		if goja.IsNull(k) || goja.IsUndefined(k) {
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
			t := &Node{text: true}
			e.node = t
			h.bindings = append(h.bindings, e)
			h.byKey[e.key] = append(h.byKey[e.key], e)
			if err := h.compute(e); err != nil {
				panic(h.vm.NewGoError(err))
			}
			n.Children = append(n.Children, t)
		case bool:
			// {cond && <x/>} renders nothing when false.
		default:
			n.Children = append(n.Children, &Node{text: true, Text: k.String()})
		}
	}
}

// Style is the parsed CSS subset of a style object.
type Style struct {
	set                               uint32
	display, dir, align, justify      int8
	flex                              float32
	gap, w, h, minH                   float32
	hFull, wFull, overflowY           bool
	pad                               [4]float32 // top right bottom left
	bg, color                         gpui.Hsla
	radius, fontSize, fontWeight, opa float32
}

const (
	sDisplay = 1 << iota
	sDir
	sAlign
	sJustify
	sFlex
	sGap
	sW
	sH
	sMinH
	sPad
	sBg
	sColor
	sRadius
	sFontSize
	sFontWeight
	sOpacity
	sOverflowY
)

func (s *Style) parse(o *goja.Object) error {
	for _, k := range o.Keys() {
		v := o.Get(k)
		str := v.String()
		switch k {
		case "display":
			s.set |= sDisplay
			switch str {
			case "flex":
				s.display = 1
			case "block":
				s.display = 2
			default:
				return fmt.Errorf("display: %q not supported", str)
			}
		case "flexDirection":
			s.set |= sDir
			s.dir = map[string]int8{"row": 1, "column": 2}[str]
		case "alignItems":
			s.set |= sAlign
			s.align = map[string]int8{"center": 1, "flex-start": 2, "start": 2}[str]
		case "justifyContent":
			s.set |= sJustify
			s.justify = map[string]int8{"center": 1, "space-between": 2}[str]
		case "flex":
			s.set |= sFlex
			s.flex = float32(v.ToFloat())
		case "gap":
			s.set |= sGap
			s.gap = px(str)
		case "width":
			s.set |= sW
			s.wFull = str == "100%"
			s.w = px(str)
		case "height":
			s.set |= sH
			s.hFull = str == "100%"
			s.h = px(str)
		case "minHeight":
			s.set |= sMinH
			s.minH = px(str)
		case "padding":
			s.set |= sPad
			f := strings.Fields(str)
			switch len(f) {
			case 1:
				p := px(f[0])
				s.pad = [4]float32{p, p, p, p}
			case 2:
				y, x := px(f[0]), px(f[1])
				s.pad = [4]float32{y, x, y, x}
			case 4:
				s.pad = [4]float32{px(f[0]), px(f[1]), px(f[2]), px(f[3])}
			default:
				return fmt.Errorf("padding: %q not supported", str)
			}
		case "background":
			s.set |= sBg
			c, err := gpui.ParseHex(str)
			if err != nil {
				return fmt.Errorf("background: %w", err)
			}
			s.bg = c
		case "color":
			s.set |= sColor
			c, err := gpui.ParseHex(str)
			if err != nil {
				return fmt.Errorf("color: %w", err)
			}
			s.color = c
		case "borderRadius":
			s.set |= sRadius
			s.radius = px(str)
		case "fontSize":
			s.set |= sFontSize
			s.fontSize = px(str)
		case "fontWeight":
			s.set |= sFontWeight
			s.fontWeight = float32(v.ToFloat())
		case "opacity":
			s.set |= sOpacity
			s.opa = float32(v.ToFloat())
		case "overflowY":
			s.set |= sOverflowY
			s.overflowY = str == "scroll" || str == "auto"
		default:
			return fmt.Errorf("style: %q not supported", k)
		}
	}
	return nil
}

// px reads "12", "12px" (a number becomes its string first).
func px(s string) float32 {
	f, _ := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 32)
	return float32(f)
}

func (s *Style) apply(d *gpui.DivElement) *gpui.DivElement {
	if s.set == 0 {
		return d
	}
	if s.set&sDisplay != 0 {
		if s.display == 1 {
			d = d.Flex()
		} else {
			d = d.Block()
		}
	}
	if s.set&sDir != 0 {
		if s.dir == 2 {
			d = d.FlexCol()
		} else {
			d = d.FlexRow()
		}
	}
	if s.set&sAlign != 0 {
		if s.align == 1 {
			d = d.ItemsCenter()
		} else {
			d = d.ItemsStart()
		}
	}
	if s.set&sJustify != 0 {
		if s.justify == 1 {
			d = d.JustifyCenter()
		} else {
			d = d.JustifyBetween()
		}
	}
	if s.set&sFlex != 0 && s.flex == 1 {
		d = d.Flex1()
	}
	if s.set&sGap != 0 {
		d = d.Gap(gpui.Px(s.gap))
	}
	if s.set&sW != 0 {
		if s.wFull {
			d = d.WFull()
		} else {
			d = d.W(gpui.Px(s.w))
		}
	}
	if s.set&sH != 0 {
		if s.hFull {
			d = d.HFull()
		} else {
			d = d.H(gpui.Px(s.h))
		}
	}
	if s.set&sMinH != 0 {
		d = d.MinH(gpui.Px(s.minH))
	}
	if s.set&sPad != 0 {
		d = d.Pt(gpui.Px(s.pad[0])).Pr(gpui.Px(s.pad[1])).Pb(gpui.Px(s.pad[2])).Pl(gpui.Px(s.pad[3]))
	}
	if s.set&sBg != 0 {
		d = d.Bg(s.bg)
	}
	if s.set&sColor != 0 {
		d = d.TextColor(s.color)
	}
	if s.set&sRadius != 0 {
		d = d.Rounded(gpui.Px(s.radius))
	}
	if s.set&sFontSize != 0 {
		d = d.TextSize(gpui.Px(s.fontSize))
	}
	if s.set&sFontWeight != 0 {
		d = d.FontWeight(s.fontWeight)
	}
	if s.set&sOpacity != 0 {
		d = d.Opacity(s.opa)
	}
	if s.set&sOverflowY != 0 && s.overflowY {
		d = d.OverflowYScroll()
	}
	return d
}

// Lower builds gpui elements from the described tree: once per frame.
// The script does not run.
func (h *Host) Lower() *gpui.DivElement { return h.lower(h.Root) }

func (h *Host) lower(n *Node) *gpui.DivElement {
	d := gpui.Div()
	if n.ID != "" {
		d = d.ID(n.ID)
		if h.Debug {
			d = d.DebugSelector(n.ID)
		}
	}
	if n.Class != "" {
		d = d.Class(n.Class)
	}
	d = n.Style.apply(d)
	if n.OnClick != nil {
		fn := n.OnClick
		d = d.OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, _ *gpui.App) {
			if err := h.Dispatch(fn); err != nil {
				fmt.Println("tsxprobe: handler:", err)
			}
		})
	}
	for _, c := range n.Children {
		if c.text {
			d = d.Child(c.Text)
		} else {
			d = d.Child(h.lower(c))
		}
	}
	return d
}

// Find returns the first node with the given ID (for tests).
func (h *Host) Find(id string) *Node { return find(h.Root, id) }

func find(n *Node, id string) *Node {
	if n.ID == id {
		return n
	}
	for _, c := range n.Children {
		if f := find(c, id); f != nil {
			return f
		}
	}
	return nil
}

// Count returns the number of nodes in the tree.
func (h *Host) Count() int { return count(h.Root) }

func count(n *Node) int {
	c := 1
	for _, k := range n.Children {
		c += count(k)
	}
	return c
}
