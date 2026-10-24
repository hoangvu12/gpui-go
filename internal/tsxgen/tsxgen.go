// Package tsxgen extracts the TSX authoring surface from the engine's
// own source (ticket 63, ADR 0003's "generated .d.ts") and renders it
// as the gpui-script.d.ts ambient declarations. It is the generator
// behind cmd/tsxdtsgen and the drift tests; nothing at run time
// imports it, so it never enters an app binary.
//
// The surface is read from the code that accepts it, never from a
// hand-written list, so the two cannot drift silently:
//
//   - elements, props, the event object's fields and the host globals
//     are the switch cases and tables of gpui/script (host.go);
//   - style properties, their value shapes and their keyword values
//     are the switch cases of parseStyle (gpui/script/node.go), with
//     each case's body pattern deciding the value's type;
//   - Tailwind classes come from the generated tables the class layer
//     itself reads (gpui/class_generated.go's method-class switch,
//     gpui/class.go's length/color/grid/scale tables and variants,
//     gpui/tailwind_palette_generated.go's palette) plus the family
//     patterns the class parser's stages accept (typography, border
//     widths, gradients, ...), which the drift tests verify against
//     gpui.CheckClasses — the same check Styled.Class runs.
//
// A shape the extractor does not recognize is an error, not a silent
// gap: adding a style key, an element, a prop or a global to the host
// without teaching this package its shape fails generation loudly.
package tsxgen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"sort"
	"strconv"
)

// Surface is the TSX authoring surface: the elements, props, style
// properties, events, globals and Tailwind classes the engine accepts.
type Surface struct {
	// Elements are the JSX element names the host builds (jsxElement's
	// element switch).
	Elements []string
	// Props are the props jsxElement accepts, in source order; Events
	// are the subset named on* (the event handlers).
	Props  []string
	Events []string
	// Event is the DOM-shaped object handlers receive (eventObject's
	// fields, with their TS types).
	Event []Field
	// Styles are the CSSProperties the style prop accepts, in source
	// order, each with the value type parseStyle accepts.
	Styles []StyleProp
	// Globals are the host's script globals (hostGlobalNames), each
	// with its declared signature.
	Globals []Global

	// Classes is the Tailwind class surface: the class strings
	// Styled.Class applies, as a type the .d.ts declares and the drift
	// tests match against gpui.CheckClasses.
	Classes ClassSurface
}

// Field is one field of the event object: Name and its TS type.
type Field struct {
	Name string
	Type TSType
}

// StyleProp is one CSSProperties key: the camelCase name parseStyle
// accepts and the value type its case accepts.
type StyleProp struct {
	Name string
	Type TSType
	Doc  string
}

// Global is one host global with the signature the .d.ts declares
// (the fixed shapes tsxgen knows; an unknown global name is an error).
type Global struct {
	Name string
	Sig  string
}

// ClassSurface is the Tailwind class surface as one TS type: the
// literal classes and the family patterns (template literal types).
type ClassSurface struct {
	// Aliases are the shared value types the Class union references,
	// rendered as `type Name = ...` before Class.
	Aliases []Alias
	// Class is the class type itself (rendered as `type Class = ...`).
	Class TSType
}

// Alias is one named type the .d.ts declares (a value scale or a
// prefix list shared by the class families).
type Alias struct {
	Name string
	Type TSType
}

// Extract reads the engine's surface from the module's sources. root
// is the module root (the directory holding go.mod).
func Extract(root string) (*Surface, error) {
	e := &extractor{fset: token.NewFileSet(), root: root}
	s := &Surface{}
	if err := e.hostSurface(s); err != nil {
		return nil, err
	}
	if err := e.styleSurface(s); err != nil {
		return nil, err
	}
	if err := e.globalSignatures(s); err != nil {
		return nil, err
	}
	if err := e.classSurface(s); err != nil {
		return nil, err
	}
	return s, nil
}

// ---------------------------------------------------------------------------
// Source loading
// ---------------------------------------------------------------------------

// extractor parses the engine's sources once and answers surface
// queries against the parsed files.
type extractor struct {
	fset *token.FileSet
	root string

	files map[string]*ast.File
}

// file parses path (module-root-relative) once and returns it.
func (e *extractor) file(path string) (*ast.File, error) {
	if e.files == nil {
		e.files = map[string]*ast.File{}
	}
	if f, ok := e.files[path]; ok {
		return f, nil
	}
	f, err := parser.ParseFile(e.fset, filepath.Join(e.root, filepath.FromSlash(path)), nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("tsxgen: %s: %w", path, err)
	}
	e.files[path] = f
	return f, nil
}

// funcDecl finds the top-level function declaration named name in
// path, failing when it is absent (the extraction sites are fixed).
func (e *extractor) funcDecl(path, name string) (*ast.FuncDecl, error) {
	f, err := e.file(path)
	if err != nil {
		return nil, err
	}
	for _, d := range f.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Name.Name == name {
			return fn, nil
		}
	}
	return nil, fmt.Errorf("tsxgen: %s: function %s not found", path, name)
}

// varDecl finds the top-level variable declaration named name.
func (e *extractor) varDecl(path, name string) (*ast.ValueSpec, error) {
	f, err := e.file(path)
	if err != nil {
		return nil, err
	}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.VAR {
			continue
		}
		for _, spec := range g.Specs {
			if vs, ok := spec.(*ast.ValueSpec); ok && len(vs.Names) == 1 && vs.Names[0].Name == name {
				return vs, nil
			}
		}
	}
	return nil, fmt.Errorf("tsxgen: %s: var %s not found", path, name)
}

// intConst finds a top-level const declaration holding an integer.
func (e *extractor) intConst(path, name string) (int, error) {
	f, err := e.file(path)
	if err != nil {
		return 0, err
	}
	for _, d := range f.Decls {
		g, ok := d.(*ast.GenDecl)
		if !ok || g.Tok != token.CONST {
			continue
		}
		for _, spec := range g.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok || len(vs.Names) != 1 || vs.Names[0].Name != name || len(vs.Values) != 1 {
				continue
			}
			if lit, ok := vs.Values[0].(*ast.BasicLit); ok && lit.Kind == token.INT {
				n, err := strconv.Atoi(lit.Value)
				if err == nil {
					return n, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("tsxgen: %s: const %s not found", path, name)
}

// stringCases returns the string literals of one switch's cases, in
// source order (each case is one literal — the engine's tables are
// written that way; a multi-literal case is an error, so a new table
// shape cannot be silently half-read).
func stringCases(sw *ast.SwitchStmt, where string) ([]string, error) {
	var out []string
	for _, stmt := range sw.Body.List {
		cc, ok := stmt.(*ast.CaseClause)
		if !ok {
			return nil, fmt.Errorf("tsxgen: %s: a non-case statement in the switch", where)
		}
		if cc.List == nil {
			continue // default
		}
		for _, expr := range cc.List {
			lit, ok := expr.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return nil, fmt.Errorf("tsxgen: %s: a non-literal case", where)
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil {
				return nil, fmt.Errorf("tsxgen: %s: %w", where, err)
			}
			out = append(out, s)
		}
	}
	return out, nil
}

// findSwitch returns the switch statements of a function's body whose
// tag is the identifier named tag (init matches a `switch x := ...; x`
// assignment, which jsxElement's element switch uses).
func findSwitch(fn *ast.FuncDecl, tag string, init bool) []*ast.SwitchStmt {
	var out []*ast.SwitchStmt
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok {
			return true
		}
		if init {
			if _, ok := sw.Init.(*ast.AssignStmt); ok {
				out = append(out, sw)
			}
			return false
		}
		if id, ok := sw.Tag.(*ast.Ident); ok && id.Name == tag {
			out = append(out, sw)
		}
		return false
	})
	return out
}

// stringLit returns the value of a string literal expression.
func stringLit(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return s, true
}

// boolLit reports whether expr is the boolean literal true.
func boolLit(expr ast.Expr) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == "true"
}

// callsNamed returns the string arguments of calls to a function or
// method with the given name inside root (e.g. every mustSet(o, "x",
// ...) call of eventObject).
func callsNamed(root ast.Node, name string) []string {
	var out []string
	ast.Inspect(root, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var id *ast.Ident
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			id = fn
		case *ast.SelectorExpr:
			id = fn.Sel
		default:
			return true
		}
		if id.Name != name {
			return true
		}
		if len(call.Args) >= 2 {
			if s, ok := stringLit(call.Args[1]); ok {
				out = append(out, s)
			}
		}
		return true
	})
	return out
}

// mapKeys returns the keys of a map composite literal, sorted.
func mapKeys(lit ast.Expr, where string) ([]string, error) {
	cl, ok := lit.(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("tsxgen: %s: not a composite literal", where)
	}
	var out []string
	for _, elt := range cl.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			return nil, fmt.Errorf("tsxgen: %s: a non-keyed element", where)
		}
		s, ok := stringLit(kv.Key)
		if !ok {
			return nil, fmt.Errorf("tsxgen: %s: a non-string key", where)
		}
		out = append(out, s)
	}
	sort.Strings(out)
	return out, nil
}

// structLits returns the struct composite literals of a slice literal
// (e.g. lengthProps' entries).
func structLits(lit ast.Expr, where string) ([]*ast.CompositeLit, error) {
	cl, ok := lit.(*ast.CompositeLit)
	if !ok {
		return nil, fmt.Errorf("tsxgen: %s: not a composite literal", where)
	}
	var out []*ast.CompositeLit
	for _, elt := range cl.Elts {
		sc, ok := elt.(*ast.CompositeLit)
		if !ok {
			return nil, fmt.Errorf("tsxgen: %s: a non-struct element", where)
		}
		out = append(out, sc)
	}
	return out, nil
}

// fieldValue reads a named string or bool field of one struct literal
// (the key is the field identifier; map literals use stringLit keys).
func fieldValue(sc *ast.CompositeLit, field string) (string, bool, error) {
	for _, elt := range sc.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		var key string
		switch k := kv.Key.(type) {
		case *ast.Ident:
			key = k.Name
		case *ast.BasicLit:
			key, _ = stringLit(k)
		}
		if key != field {
			continue
		}
		if s, ok := stringLit(kv.Value); ok {
			return s, false, nil
		}
		if boolLit(kv.Value) {
			return "", true, nil
		}
		return "", false, fmt.Errorf("tsxgen: field %s is neither a string nor true", field)
	}
	return "", false, nil
}

// structStringFields reads the first positional string of each struct
// literal (gridClass's prefix table is positional).
func structStringFields(lit ast.Expr, where string) ([]string, error) {
	structs, err := structLits(lit, where)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, sc := range structs {
		if len(sc.Elts) == 0 {
			return nil, fmt.Errorf("tsxgen: %s: an empty struct element", where)
		}
		s, ok := stringLit(sc.Elts[0])
		if !ok {
			return nil, fmt.Errorf("tsxgen: %s: the first element is not a string", where)
		}
		out = append(out, s)
	}
	return out, nil
}

// isEmptyReturn3 reports whether a case's body is a three-value return
// whose last two values are the empty string (parseVariant's accepted
// variants return (kind, group, "")).
func isEmptyReturn3(stmt ast.Stmt) bool {
	ret, ok := stmt.(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 3 {
		return false
	}
	for _, r := range ret.Results[1:] {
		if s, ok := stringLit(r); !ok || s != "" {
			return false
		}
	}
	return true
}

// prefixLiterals returns the string arguments of CutPrefix calls in
// root (parseVariant's named group variants).
func prefixLiterals(root ast.Node) []string {
	var out []string
	ast.Inspect(root, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "CutPrefix" {
			if len(call.Args) >= 1 {
				if s, ok := stringLit(call.Args[1]); ok {
					out = append(out, s)
				}
			}
		}
		return true
	})
	return out
}

// containsCall reports whether root calls a function (or method) with
// the given name.
func containsCall(root ast.Node, name string) bool {
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			found = fn.Name == name
		case *ast.SelectorExpr:
			found = fn.Sel.Name == name
		}
		return !found
	})
	return found
}

// comparesTo1 reports whether root contains a != comparison with the
// integer literal 1 (parseStyle's flex case).
func comparesTo1(root ast.Node) bool {
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if found {
			return false
		}
		if bin, ok := n.(*ast.BinaryExpr); ok && bin.Op == token.NEQ {
			if lit, ok := bin.Y.(*ast.BasicLit); ok && lit.Kind == token.INT && lit.Value == "1" {
				found = true
			}
		}
		return !found
	})
	return found
}

// hasString reports whether root contains the string literal s.
func hasString(root ast.Node, s string) bool {
	found := false
	ast.Inspect(root, func(n ast.Node) bool {
		if found {
			return false
		}
		if lit, ok := n.(*ast.BasicLit); ok {
			if got, okLit := stringLit(lit); okLit && got == s {
				found = true
			}
		}
		return !found
	})
	return found
}
