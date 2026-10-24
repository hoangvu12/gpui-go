package tsxgen

import (
	"fmt"
	"go/ast"
	"strings"
)

// hostSurface extracts the element, prop, event and global names from
// gpui/script/host.go's own code: jsxElement's two switches, the
// eventObject's mustSet fields and the hostGlobalNames map.
func (e *extractor) hostSurface(s *Surface) error {
	fn, err := e.funcDecl("gpui/script/host.go", "jsxElement")
	if err != nil {
		return err
	}
	// The element switch's tag is an assignment (typ := ...String());
	// the prop switch's tag is the key identifier k.
	elemSwitches := findSwitch(fn, "", true)
	propSwitches := findSwitch(fn, "k", false)
	if len(elemSwitches) != 1 {
		return fmt.Errorf("tsxgen: jsxElement: expected one element switch (tag assignment), found %d", len(elemSwitches))
	}
	if len(propSwitches) != 1 {
		return fmt.Errorf("tsxgen: jsxElement: expected one prop switch (tag k), found %d", len(propSwitches))
	}
	elements, err := stringCases(elemSwitches[0], "jsxElement's element switch")
	if err != nil {
		return err
	}
	if len(elements) == 0 {
		return fmt.Errorf("tsxgen: jsxElement: no elements found")
	}
	s.Elements = elements

	props, err := stringCases(propSwitches[0], "jsxElement's prop switch")
	if err != nil {
		return err
	}
	for _, p := range props {
		if strings.HasPrefix(p, "on") {
			s.Events = append(s.Events, p)
		}
	}
	s.Props = props

	// The event object handlers receive: eventObject's mustSet fields.
	ev, err := e.funcDecl("gpui/script/host.go", "eventObject")
	if err != nil {
		return err
	}
	names := callsNamed(ev, "mustSet")
	if len(names) == 0 {
		return fmt.Errorf("tsxgen: eventObject: no mustSet fields found")
	}
	for _, name := range names {
		typ, ok := eventFieldTypes[name]
		if !ok {
			return fmt.Errorf("tsxgen: eventObject: field %q has no known type; teach tsxgen its shape", name)
		}
		s.Event = append(s.Event, Field{Name: name, Type: typ})
	}

	// The host's script globals: the hostGlobalNames map's keys.
	spec, err := e.varDecl("gpui/script/host.go", "hostGlobalNames")
	if err != nil {
		return err
	}
	globals, err := mapKeys(spec.Values[0], "hostGlobalNames")
	if err != nil {
		return err
	}
	for _, name := range globals {
		s.Globals = append(s.Globals, Global{Name: name})
	}
	return nil
}

// eventFieldTypes are the TS types of the event object's fields. A new
// field fails extraction until its type is added here (loudly, so the
// .d.ts cannot silently lack it).
var eventFieldTypes = map[string]TSType{
	"button":   numType,
	"x":        numType,
	"y":        numType,
	"shiftKey": boolType,
	"ctrlKey":  boolType,
	"altKey":   boolType,
	"metaKey":  boolType,
}

// styleSurface extracts the style properties from parseStyle's switch
// in gpui/script/node.go: the keys, and each case's value shape read
// from the code that accepts the value.
func (e *extractor) styleSurface(s *Surface) error {
	fn, err := e.funcDecl("gpui/script/node.go", "parseStyle")
	if err != nil {
		return err
	}
	switches := findSwitch(fn, "k", false)
	if len(switches) != 1 {
		return fmt.Errorf("tsxgen: parseStyle: expected one style switch (tag k), found %d", len(switches))
	}
	for _, stmt := range switches[0].Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok || cc.List == nil {
			continue
		}
		for _, expr := range cc.List {
			lit, ok := stringLit(expr)
			if !ok {
				return fmt.Errorf("tsxgen: parseStyle: a non-literal case")
			}
			typ, doc, err := styleValueType(lit, cc.Body)
			if err != nil {
				return err
			}
			s.Styles = append(s.Styles, StyleProp{Name: lit, Type: typ, Doc: doc})
		}
	}
	if len(s.Styles) == 0 {
		return fmt.Errorf("tsxgen: parseStyle: no style properties found")
	}
	return nil
}

// lengthType is a px length: a number (8) or an "8px" string.
var lengthType = Union(
	numType,
	Template([]string{"", "px"}, numType),
)

// colorType is a CSS hex color string (#rgb, #rgba, #rrggbb, #rrggbbaa).
var colorType = Template([]string{"#", ""}, strType)

// styleValueType reads one parseStyle case's body and returns the
// value type it accepts. A body matching no known shape is an error —
// the extraction cannot silently lack a new property's type.
func styleValueType(name string, body []ast.Stmt) (TSType, string, error) {
	var node ast.Node = &ast.BlockStmt{List: body}
	kind := ""
	switch {
	case containsCall(node, "Fields"):
		kind = "lengthList"
	case containsCall(node, "ParseHex"):
		kind = "color"
	case containsCall(node, "px"):
		kind = "length"
	case containsCall(node, "ToFloat"):
		if comparesTo1(node) {
			kind = "one"
		} else {
			kind = "number"
		}
	default:
		// An enum: an inner switch over the value's string form.
		inner := findSwitchIn(node, "str")
		if len(inner) != 1 {
			return none, "", fmt.Errorf("tsxgen: style %q: unrecognized value shape; teach tsxgen its type", name)
		}
		values, err := stringCases(inner[0], "parseStyle's "+name+" switch")
		if err != nil {
			return none, "", err
		}
		return Lits(values...), enumDoc(name, values), nil
	}
	doc := styleDocs[name]
	switch kind {
	case "number":
		return numType, doc, nil
	case "one":
		return NumLit("1"), doc, nil
	case "length":
		if hasString(node, "100%") {
			return Union(lengthType, Lit("100%")), doc, nil
		}
		return lengthType, doc, nil
	case "color":
		return colorType, doc, nil
	case "lengthList":
		return lengthListType(), doc, nil
	}
	panic("unreachable")
}

// lengthListType is the padding value type: one, two or four lengths,
// space-separated (parseStyle splits on whitespace), with each length
// a number or an Npx string in any mix.
func lengthListType() TSType {
	suffixes := []string{"", "px"}
	var arms []TSType
	arms = append(arms, lengthType)
	for _, n := range []int{2, 4} {
		for _, combo := range combos(suffixes, n) {
			parts := make([]string, n+1)
			holes := make([]TSType, n)
			for i := range holes {
				holes[i] = numType
			}
			parts[0] = ""
			for i := 1; i <= n; i++ {
				parts[i] = combo[i-1]
				if i < n {
					parts[i] += " "
				}
			}
			arms = append(arms, TSType{Templates: []TSTemplate{{Parts: parts, Holes: holes}}})
		}
	}
	return Union(arms...)
}

// combos returns the cartesian product of items taken n times.
func combos(items []string, n int) [][]string {
	if n == 0 {
		return [][]string{nil}
	}
	var out [][]string
	for _, c := range combos(items, n-1) {
		for _, item := range items {
			out = append(out, append(append([]string(nil), c...), item))
		}
	}
	return out
}

// findSwitchIn returns the switch statements with the given identifier
// tag inside a block.
func findSwitchIn(node ast.Node, tag string) []*ast.SwitchStmt {
	var out []*ast.SwitchStmt
	ast.Inspect(node, func(n ast.Node) bool {
		if sw, ok := n.(*ast.SwitchStmt); ok {
			if id, ok := sw.Tag.(*ast.Ident); ok && id.Name == tag {
				out = append(out, sw)
				return false
			}
		}
		return true
	})
	return out
}

// enumDoc renders the enum's doc line.
func enumDoc(name string, values []string) string {
	return fmt.Sprintf("%s: %s.", name, strings.Join(values, ", "))
}

// styleDocs are the one-line docs the .d.ts carries (the value shapes
// parseStyle's comments already explain; unknown keys get none).
var styleDocs = map[string]string{
	"display":        `display: "flex" or "block".`,
	"flexDirection":  `flexDirection: "row" or "column".`,
	"alignItems":     `alignItems: "center", "start" or "end".`,
	"justifyContent": `justifyContent: "center", "start", "end" or "space-between".`,
	"flex":           `flex: 1 (the only flex value the engine applies).`,
	"gap":            `gap: a px length (8 or "8px").`,
	"width":          `width: a px length, or "100%".`,
	"height":         `height: a px length, or "100%".`,
	"minHeight":      `minHeight: a px length.`,
	"padding":        `padding: one, two or four px lengths ("8", "8 16", "8 16 8 16").`,
	"background":     `background: a CSS hex color (#rrggbb).`,
	"color":          `color: a CSS hex color.`,
	"borderColor":    `borderColor: a CSS hex color.`,
	"border":         `border: a px length.`,
	"borderRadius":   `borderRadius: a px length.`,
	"fontSize":       `fontSize: a px length.`,
	"fontWeight":     `fontWeight: a number (100–900).`,
	"opacity":        `opacity: a number (0–1).`,
	"overflowY":      `overflowY: "scroll", "auto" or "hidden".`,
}

// globalSignatures fills in each global's declared signature. The
// names came from hostGlobalNames (so they cannot drift); the fixed
// shapes live here, and a name without one fails loudly.
func (e *extractor) globalSignatures(s *Surface) error {
	for i, g := range s.Globals {
		sig, ok := globalSigs[g.Name]
		if !ok {
			return fmt.Errorf("tsxgen: global %q has no signature; teach tsxgen its shape", g.Name)
		}
		s.Globals[i].Sig = sig
	}
	return nil
}

// globalSigs are the host globals' declared signatures (the behaviour
// tickets 62 and 64 built and their tests pin; the .d.ts states it).
// __el is the factory's Go half the prelude calls; scripts reach it
// through h and Fragment.
var globalSigs = map[string]string{
	"h":                     "declare function h(tag: any, props: any, ...children: any[]): HostElement;",
	"Fragment":              "declare function Fragment(props: any, ...children: any[]): HostElement;",
	"__el":                  "declare function __el(type: string, props: any, children: any[]): HostElement;",
	"useState":              "declare function useState<S>(key: string, init: S): [S, (value: S) => void];",
	"state":                 "declare function state<S>(key: string, init: S): StateHandle<S>;",
	"bind":                  "declare function bind<S>(handle: StateHandle<S>, fn: (value: S) => string | number): Child;",
	"shared":                "declare function shared<S>(key: string, init: S): StateHandle<S>;",
	"invoke":                "declare function invoke(name: string, ...args: any[]): any;",
	"call":                  "declare function call(name: string, ...args: any[]): Promise<any>;",
	"fetch":                 "declare function fetch(url: string, init?: RequestInit): Promise<Response>;",
	"setTimeout":            "declare function setTimeout(fn: (...args: any[]) => void, delay?: number, ...args: any[]): number;",
	"clearTimeout":          "declare function clearTimeout(id: number): void;",
	"setInterval":           "declare function setInterval(fn: (...args: any[]) => void, delay?: number, ...args: any[]): number;",
	"clearInterval":         "declare function clearInterval(id: number): void;",
	"queueMicrotask":        "declare function queueMicrotask(fn: () => void): void;",
	"requestAnimationFrame": "declare function requestAnimationFrame(fn: (time: number) => void): number;",
	"cancelAnimationFrame":  "declare function cancelAnimationFrame(id: number): void;",
	"performance":           "declare const performance: { now(): number };",
	"console":               "declare const console: Console;",
	"atob":                  "declare function atob(data: string): string;",
	"btoa":                  "declare function btoa(data: string): string;",
	"TextEncoder":           "declare class TextEncoder { readonly encoding: string; encode(input?: string): Uint8Array; }",
	"TextDecoder":           "declare class TextDecoder { constructor(label?: string, options?: { fatal?: boolean }); readonly encoding: string; readonly fatal: boolean; readonly ignoreBOM: boolean; decode(input?: Uint8Array): string; }",
	"URL":                   "declare class URL {\n\tconstructor(url: string, base?: string);\n\treadonly href: string;\n\treadonly protocol: string;\n\treadonly host: string;\n\treadonly hostname: string;\n\treadonly port: string;\n\treadonly pathname: string;\n\treadonly search: string;\n\treadonly hash: string;\n\treadonly origin: string;\n\ttoString(): string;\n}",
	"URLSearchParams":       "declare class URLSearchParams {\n\tconstructor(init?: string | string[][] | Record<string, string> | URLSearchParams);\n\tappend(name: string, value: string): void;\n\tdelete(name: string): void;\n\tget(name: string): string | null;\n\tgetAll(name: string): string[];\n\thas(name: string): boolean;\n\tset(name: string, value: string): void;\n\ttoString(): string;\n\tforEach(callback: (value: string, key: string, searchParams: URLSearchParams) => void, thisArg?: any): void;\n\tentries(): IterableIterator<[string, string]>;\n\tkeys(): IterableIterator<string>;\n\tvalues(): IterableIterator<string>;\n\t[Symbol.iterator](): IterableIterator<[string, string]>;\n}",
}
