//go:build windows

package script

// The engine side of the .d.ts parity (ticket 63): the declared
// surface's samples run through the engine's own acceptors — parseStyle
// for every style key and value, jsxElement for every element and
// prop, eventObject for the declared event fields — proving the types
// describe what the engine actually accepts (the class half runs in
// internal/tsxgen, against gpui.CheckClasses).
//
// This is an internal test on purpose: parseStyle and jsxElement are
// the engine's own code paths, and the fixture's onMouseDown handler
// is dispatched directly (the public test surface covers onClick
// through Host.Click).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/tsxgen"
)

// declaredSurface parses the committed .d.ts back (the drift tests in
// internal/tsxgen prove it equals the engine's surface; here it is the
// list of what the types say, to run through the engine). The file is
// read from disk rather than through the types package, which imports
// this one (its TypeDiagnostic) and would cycle.
func declaredSurface(t *testing.T) *tsxgen.Surface {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("types", "gpui-script.d.ts"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := tsxgen.ParseDTS(string(b))
	if err != nil {
		t.Fatalf("parsing the committed .d.ts: %v", err)
	}
	return s
}

func TestParseStyleAcceptsEveryDeclaredStyleValue(t *testing.T) {
	s := declaredSurface(t)
	rt := goja.New()
	for _, sp := range s.Styles {
		for _, v := range sp.Type.GoValues(s.Aliases()) {
			o := rt.NewObject()
			if err := o.Set(sp.Name, rt.ToValue(v)); err != nil {
				t.Fatal(err)
			}
			if _, err := parseStyle(o); err != nil {
				t.Errorf("style %s: %v is typed but the engine rejects it: %v", sp.Name, v, err)
			}
		}
	}
}

func TestParseStyleRejectsAnUnknownKeyAndABadEnumValue(t *testing.T) {
	rt := goja.New()
	o := rt.NewObject()
	if err := o.Set("colr", rt.ToValue("#ffffff")); err != nil {
		t.Fatal(err)
	}
	if _, err := parseStyle(o); err == nil {
		t.Errorf("parseStyle accepted an unknown style key")
	}
	o = rt.NewObject()
	if err := o.Set("display", rt.ToValue("grid")); err != nil {
		t.Fatal(err)
	}
	if _, err := parseStyle(o); err == nil {
		t.Errorf("parseStyle accepted a bad display value")
	}
}

func TestJSXElementAcceptsEveryDeclaredElementAndProp(t *testing.T) {
	s := declaredSurface(t)
	rt := goja.New()
	h := New()
	h.vm = rt
	if err := rt.Set("__el", h.jsxElement); err != nil {
		t.Fatal(err)
	}
	// One prop per kind: what each declared prop accepts. The slot
	// element additionally needs its name (its own declared prop, not a
	// shared one).
	props := map[string]string{
		"key":         `key: "x"`,
		"id":          `id: "x"`,
		"className":   `className: "flex items-center"`,
		"style":       `style: {display: "flex"}`,
		"children":    `children: []`,
		"onClick":     `onClick: function () {}`,
		"onMouseDown": `onMouseDown: function () {}`,
	}
	for _, el := range s.Elements {
		extra := ""
		if el == "slot" {
			extra = `, name: "s"`
		}
		for _, p := range s.Props {
			_, ok := props[p]
			if !ok {
				t.Fatalf("no prop sample for %q; teach the test its value", p)
			}
			if _, err := rt.RunString(`__el("` + el + `", {` + props[p] + extra + `}, undefined)`); err != nil {
				t.Errorf("<%s %s>: the prop is typed but the engine rejects it: %v", el, p, err)
			}
		}
	}
	// The slot's own prop is typed (SlotProps) and the engine takes it.
	if _, err := rt.RunString(`__el("slot", {name: "s"}, undefined)`); err != nil {
		t.Errorf("<slot name>: the prop is typed but the engine rejects it: %v", err)
	}
}

func TestJSXElementRejectsAnUnknownElementAndProp(t *testing.T) {
	rt := goja.New()
	h := New()
	h.vm = rt
	if err := rt.Set("__el", h.jsxElement); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.RunString(`__el("span", {}, undefined)`); err == nil {
		t.Errorf("jsxElement accepted an unknown element")
	}
	if _, err := rt.RunString(`__el("div", {onClik: function () {}}, undefined)`); err == nil {
		t.Errorf("jsxElement accepted an unknown prop")
	}
	// name is the slot element's, not a shared prop: div rejects it,
	// and a slot without one is a describe-time error.
	if _, err := rt.RunString(`__el("div", {name: "x"}, undefined)`); err == nil {
		t.Errorf("jsxElement accepted name on a div (it is the slot element's)")
	}
	if _, err := rt.RunString(`__el("slot", {}, undefined)`); err == nil {
		t.Errorf("jsxElement accepted a nameless slot")
	}
}

func TestEventObjectHasTheDeclaredFields(t *testing.T) {
	s := declaredSurface(t)
	rt := goja.New()
	h := New()
	h.vm = rt
	ev := h.eventObject(2, 10, 20, gpui.Modifiers{Shift: true, Control: true, Alt: true, Platform: true})
	o := ev.(*goja.Object)
	for _, f := range s.Event {
		if o.Get(f.Name) == nil {
			t.Errorf("the event object lacks %q, which the .d.ts declares", f.Name)
		}
	}
	for _, k := range o.Keys() {
		known := false
		for _, f := range s.Event {
			if f.Name == k {
				known = true
			}
		}
		if !known {
			t.Errorf("the event object has %q, which the .d.ts does not declare", k)
		}
	}
	// The modifiers arrive as the DOM's booleans.
	if !o.Get("shiftKey").Equals(rt.ToValue(true)) || !o.Get("button").Equals(rt.ToValue(2)) {
		t.Errorf("event object values: shift=%v button=%v", o.Get("shiftKey"), o.Get("button"))
	}
}

func TestSurfaceFixtureCoversEveryStyleKey(t *testing.T) {
	// The fixture exercises the whole style surface; a new style key
	// without fixture coverage fails here (the tsc pass and this run
	// keep the fixture meaningful).
	s := declaredSurface(t)
	fixture, err := os.ReadFile(filepath.Join("testdata", "surface.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(fixture)
	for _, sp := range s.Styles {
		if !strings.Contains(src, sp.Name+":") {
			t.Errorf("surface.tsx does not exercise style %q", sp.Name)
		}
	}
	for _, el := range s.Elements {
		if !strings.Contains(src, "<"+el) {
			t.Errorf("surface.tsx does not exercise element <%s>", el)
		}
	}
	for _, e := range s.Events {
		if !strings.Contains(src, e+"={") {
			t.Errorf("surface.tsx does not exercise event %s", e)
		}
	}
}

// TestParseStyleTransforms checks the transform style keys the TSX
// surface accepts (browser-core ticket 56): the function-list spelling
// and the origin keywords, and a rejection that names the property.
func TestParseStyleTransforms(t *testing.T) {
	rt := goja.New()
	mustStyle := func(key, value string) style {
		t.Helper()
		o := rt.NewObject()
		if err := o.Set(key, rt.ToValue(value)); err != nil {
			t.Fatal(err)
		}
		s, err := parseStyle(o)
		if err != nil {
			t.Fatalf("style %s: %v", key, err)
		}
		return s
	}
	s := mustStyle("transform", "translate(8px, 4px) rotate(45deg) scale(1.5) skewX(3deg)")
	if len(s.transform) != 4 {
		t.Fatalf("transform parsed %d functions, want 4", len(s.transform))
	}
	if s.transform[1].X != 45 || s.transform[1].Kind != gpui.TransformRotate {
		t.Errorf("rotate function = %+v", s.transform[1])
	}
	s = mustStyle("transformOrigin", "top left")
	if s.transformOrigin.X != gpui.Fraction(0) || s.transformOrigin.Y != gpui.Fraction(0) {
		t.Errorf("origin = %+v, want (0, 0)", s.transformOrigin)
	}
	// A bad function names itself.
	o := rt.NewObject()
	if err := o.Set("transform", rt.ToValue("perspective(400px)")); err != nil {
		t.Fatal(err)
	}
	if _, err := parseStyle(o); err == nil {
		t.Error("parseStyle accepted perspective()")
	}
}
