package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// gpuiModulePath is the import path of the package that owns the
// default-position element constructors.
const gpuiModulePath = "github.com/hoangvu12/gpui-go/gpui"

// fixturePathMarker identifies the parity fixture packages: their
// views mirror checked-in Chrome pages, and an unpositioned parent of
// an absolute child is exactly what those pages contain (the static
// default is the point there). The parent-of-absolute rule skips them;
// the inset rule still applies.
const fixturePathMarker = "internal/parity/fixtures"

// rootConstructors are the gpui constructors whose element style starts
// at Style::default() with an empty caller refinement (BindStyled with
// no style set): see gpui/div.go Div, gpui/img.go Img and
// gpui/canvas.go Canvas — the same root set displaycodemod uses.
var rootConstructors = map[string]bool{
	"Div":    true,
	"Img":    true,
	"Canvas": true,
}

// positionSelectors are the Styled-vocabulary method names that set the
// position property (gpui/styled.go and gpui/styled_generated.go:
// Static, Fixed and Sticky are hand-written, Relative and Absolute
// generated). A chain carrying one of them states its position
// explicitly and is never touched.
var positionSelectors = map[string]bool{
	"Relative": true,
	"Absolute": true,
	"Fixed":    true,
	"Sticky":   true,
	"Static":   true,
}

// insetSelectors are the Styled-vocabulary method names that set the
// inset properties (gpui/styled_generated.go Inset/Top/Bottom/Left/
// Right). Insets apply only to positioned boxes (css-position-3:
// "top, right, bottom, and left ... apply to positioned elements");
// the record translation zeroes them for static nodes, so under the
// static default (browser-core 76, spec D3) an inset-vocabulary chain
// without a position selector silently lost its offsets — exactly the
// silent-breakage pattern displaycodemod pins for flex vocabulary.
var insetSelectors = map[string]bool{
	"Inset":  true,
	"Top":    true,
	"Bottom": true,
	"Left":   true,
	"Right":  true,
}

// childSelectors are the DivElement method names that attach children
// (gpui/div.go Child/Children/TryChildren/TryChildrenSlice). A link
// with one of these names carries the receiver's children; the
// parent-of-absolute rule reads its arguments.
var childSelectors = map[string]bool{
	"Child":            true,
	"Children":         true,
	"TryChildren":      true,
	"TryChildrenSlice": true,
}

// tailwindPositionClasses are the Tailwind classes that set position
// (gpui/class_generated.go). A Class literal containing one of them
// without a variant prefix makes the chain's position explicit; under a
// variant (hover:relative) it does not, because the base position still
// relies on the default.
var tailwindPositionClasses = map[string]bool{
	"relative": true,
	"absolute": true,
	"fixed":    true,
	"sticky":   true,
	"static":   true,
}

// tailwindInsetPrefixes are the Tailwind class prefixes that set insets
// (top-4, left-[12px], inset-0, ...). A Class literal carrying one
// counts as inset vocabulary for the inert-inset rule; a variant
// prefix is looked through (hover:top-2 still needs the base position).
var tailwindInsetPrefixes = []string{
	"top-",
	"left-",
	"right-",
	"bottom-",
	"inset-",
}

// relativeInsertion is the text inserted after a root constructor call.
const relativeInsertion = ".Relative()"

// Insertion is one .Relative() insertion into the original source.
type Insertion struct {
	// Offset is the byte offset in the ORIGINAL source where Text is
	// inserted (the end of the root constructor call).
	Offset int
	// Text is the inserted text (".Relative()").
	Text string
}

// FileRewrite is the analysis outcome for one file.
type FileRewrite struct {
	// Path is the file path as given to RewriteSource.
	Path string
	// Changed reports whether any insertion applies.
	Changed bool
	// Edits are the insertions, ascending by offset.
	Edits []Insertion
	// Final is the rewritten, gofmt-formatted source (set when Changed).
	Final []byte
	// Roots is the number of root constructor calls seen.
	Roots int
	// Explicit is the number of roots skipped because their chain
	// already sets position.
	Explicit int
	// Defaults is the number of roots left at the static default: no
	// position selector, no inert inset vocabulary and no absolute
	// child — the browser-like default browser-core 76 (spec D3)
	// flipped to, which a plain chain now deliberately uses.
	Defaults int
}

// RewriteSource analyzes one Go source file and returns the rewrite.
// src may be nil, in which case the file is read from path. A parse
// error is returned as-is (the file is left alone).
func RewriteSource(path string, src []byte) (*FileRewrite, error) {
	fset := token.NewFileSet()
	// A nil src must reach the parser as a nil interface, not a typed
	// nil []byte (which the any parameter would hold as an empty
	// source instead of reading the file).
	var parseSrc any
	if src != nil {
		parseSrc = src
	}
	file, err := parser.ParseFile(fset, path, parseSrc, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	if isGenerated(file) {
		// Generated output is never hand-edited: its generator is
		// fixed and rerun instead (the codemod must not widen the
		// diff between a generator and its committed output).
		return &FileRewrite{Path: path}, nil
	}
	facts := fileImportFacts(file)
	res := &FileRewrite{Path: path}
	if !facts.relevant() {
		return res, nil
	}

	// Parents of every node, so a root's chain can be walked upward.
	parents := nodeParents(file)

	var roots []*ast.CallExpr
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if isRootConstructor(call, facts) {
			roots = append(roots, call)
		}
		return true
	})

	// The parity fixture packages mirror their Chrome pages: an
	// unpositioned parent of an absolute child there is the page's own
	// shape, not a reliance on the pin's default. The inset rule still
	// applies (a fixture view with inert insets is a view bug).
	fixtures := isFixturePath(path)

	for _, root := range roots {
		res.Roots++
		if chainSetsPosition(root, parents) {
			res.Explicit++
			continue
		}
		if chainUsesInsets(root, parents) {
			pos := fset.Position(root.End())
			res.Edits = append(res.Edits, Insertion{Offset: pos.Offset, Text: relativeInsertion})
			continue
		}
		if !fixtures && chainAttachesAbsoluteChild(root, parents) {
			pos := fset.Position(root.End())
			res.Edits = append(res.Edits, Insertion{Offset: pos.Offset, Text: relativeInsertion})
			continue
		}
		// Post-76 rule (browser-core 76, spec D3): the default position
		// is Static, so a plain chain with no inset vocabulary and no
		// absolute child deliberately uses the browser default and is
		// left alone.
		res.Defaults++
	}
	if len(res.Edits) == 0 {
		return res, nil
	}

	// Sort ascending and apply back-to-front so offsets stay valid.
	edits := res.Edits
	for i := 1; i < len(edits); i++ {
		for j := i; j > 0 && edits[j].Offset < edits[j-1].Offset; j-- {
			edits[j], edits[j-1] = edits[j-1], edits[j]
		}
	}
	if src == nil {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read: %w", err)
		}
		src = data
	}
	out := make([]byte, 0, len(src)+len(edits)*len(relativeInsertion))
	prev := 0
	for _, e := range edits {
		if e.Offset < prev {
			return nil, fmt.Errorf("%s: internal error: overlapping insertions at %d", path, e.Offset)
		}
		out = append(out, src[prev:e.Offset]...)
		out = append(out, e.Text...)
		prev = e.Offset
	}
	out = append(out, src[prev:]...)
	formatted, err := format.Source(out)
	if err != nil {
		return nil, fmt.Errorf("%s: gofmt on the rewritten source: %w", path, err)
	}
	res.Changed = true
	res.Final = formatted
	return res, nil
}

// isFixturePath reports whether the file lives under the parity
// fixture packages (slash-normalized so it holds on Windows too).
func isFixturePath(path string) bool {
	return strings.Contains(filepath.ToSlash(path), fixturePathMarker)
}

// isGenerated reports whether the file carries a generated-code
// header: the Go convention's "Code generated" line with "DO NOT
// EDIT." — the same rule displaycodemod applies.
func isGenerated(file *ast.File) bool {
	for _, cg := range file.Comments {
		if cg.Pos() > file.Package {
			break
		}
		gen, edit := false, false
		for _, c := range cg.List {
			if strings.HasPrefix(c.Text, "// Code generated") {
				gen = true
			}
			if strings.Contains(c.Text, "DO NOT EDIT") {
				edit = true
			}
		}
		if gen && edit {
			return true
		}
	}
	return false
}

// importFacts records how the file refers to the gpui package.
type importFacts struct {
	// aliases are the identifiers the file can qualify gpui with.
	aliases map[string]bool
	// dotImport reports a dot-import of gpui (unqualified roots).
	dotImport bool
	// inGPUI reports that the file itself is package gpui.
	inGPUI bool
}

// relevant reports whether the file can mention a root constructor at
// all: it imports gpui (under any name) or is in the gpui package.
func (f *importFacts) relevant() bool {
	return f.inGPUI || f.dotImport || len(f.aliases) > 0
}

// fileImportFacts extracts the file's gpui references and its own
// package name.
func fileImportFacts(file *ast.File) *importFacts {
	facts := &importFacts{aliases: map[string]bool{}}
	if file.Name != nil && file.Name.Name == "gpui" {
		facts.inGPUI = true
	}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if path != gpuiModulePath {
			continue
		}
		switch {
		case spec.Name == nil:
			facts.aliases["gpui"] = true
		case spec.Name.Name == ".":
			facts.dotImport = true
		case spec.Name.Name == "_":
			// A blank import cannot be referenced.
		default:
			facts.aliases[spec.Name.Name] = true
		}
	}
	return facts
}

// isRootConstructor reports whether call is a Div/Img/Canvas
// constructor call of the gpui package: pkgalias.Name(...), or a bare
// Name(...) inside package gpui or a file that dot-imports it.
func isRootConstructor(call *ast.CallExpr, facts *importFacts) bool {
	switch fun := call.Fun.(type) {
	case *ast.SelectorExpr:
		ident, ok := fun.X.(*ast.Ident)
		if !ok {
			return false
		}
		return rootConstructors[fun.Sel.Name] && facts.aliases[ident.Name]
	case *ast.Ident:
		if !rootConstructors[fun.Name] {
			return false
		}
		// A bare constructor name is gpui's only inside package gpui
		// itself or through a dot-import; another package's local
		// helper of the same name is left alone.
		return facts.inGPUI || facts.dotImport
	default:
		return false
	}
}

// chainLink is one link of a method chain: the selector's name and the
// call it belongs to (nil for a selector that is never called).
type chainLink struct {
	sel  *ast.SelectorExpr
	call *ast.CallExpr
}

// chainOf walks the maximal run of selector method calls built
// directly on the root's result (displaycodemod's chain walk:
// the receiver expression is the X of a SelectorExpr whose
// SelectorExpr is the Fun of the next CallExpr).
func chainOf(root ast.Node, parents map[ast.Node]ast.Node) []chainLink {
	var links []chainLink
	cur := root
	for {
		sel, ok := parents[cur].(*ast.SelectorExpr)
		if !ok || sel.X != cur {
			return links
		}
		call, ok := parents[sel].(*ast.CallExpr)
		if !ok || call.Fun != sel {
			return links
		}
		links = append(links, chainLink{sel: sel, call: call})
		cur = call
	}
}

// chainSetsPosition walks the root's method chain and reports whether
// any link sets position explicitly: a position selector method, or a
// Class call whose literal string contains a Tailwind position class
// without a state-variant prefix.
func chainSetsPosition(root *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	for _, link := range chainOf(root, parents) {
		if positionSelectors[link.sel.Sel.Name] {
			return true
		}
		if link.sel.Sel.Name == "Class" && classLiteralSetsPosition(link.call) {
			return true
		}
	}
	return false
}

// classLiteralSetsPosition reports whether a Class call's first
// argument is a string literal containing a Tailwind position class.
// Non-literal arguments are unknown (reported as not explicit; pinning
// the root is safe because a class applied later overwrites the pin).
func classLiteralSetsPosition(call *ast.CallExpr) bool {
	value, ok := classLiteralOf(call)
	if !ok {
		return false
	}
	for _, field := range strings.Fields(value) {
		// A variant-prefixed class (hover:relative) sets position only
		// in that state, so the base position still relies on the
		// default.
		if strings.Contains(field, ":") {
			continue
		}
		class := strings.TrimSuffix(strings.TrimPrefix(field, "!"), "!")
		if tailwindPositionClasses[class] {
			return true
		}
	}
	return false
}

// chainUsesInsets walks the root's method chain and reports whether
// any link sets insets without the position being stated elsewhere in
// the chain: an inset method selector, or a Class literal carrying a
// Tailwind inset class. Such a chain got its offsets from the Relative
// default before browser-core 76 flipped the default position to
// Static; under the static default the insets are inert (CSS static
// boxes ignore them; the record translation zeroes them), so the
// codemod pins .Relative() to restore the offsets.
func chainUsesInsets(root *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	for _, link := range chainOf(root, parents) {
		if insetSelectors[link.sel.Sel.Name] {
			return true
		}
		if link.sel.Sel.Name == "Class" && classLiteralUsesInsets(link.call) {
			return true
		}
	}
	return false
}

// classLiteralUsesInsets reports whether a Class call's first argument
// is a string literal containing a Tailwind inset class (top-4,
// left-[12px], inset-0, ...). Non-literal arguments are unknown and do
// not trigger the pin: a chain that styles itself through an unknown
// class string is the author's statement, and the class layer (later
// wins) still governs position.
func classLiteralUsesInsets(call *ast.CallExpr) bool {
	value, ok := classLiteralOf(call)
	if !ok {
		return false
	}
	for _, field := range strings.Fields(value) {
		// A variant-prefixed class (hover:top-2) sets the property only
		// in that state, but the base position still comes from the
		// default — the element needs the relative pin for the variant
		// to work — so the variant prefix is looked through.
		class := field
		if idx := strings.LastIndex(class, ":"); idx >= 0 {
			class = class[idx+1:]
		}
		class = strings.TrimSuffix(strings.TrimPrefix(class, "!"), "!")
		for _, prefix := range tailwindInsetPrefixes {
			if strings.HasPrefix(class, prefix) {
				return true
			}
		}
	}
	return false
}

// chainAttachesAbsoluteChild walks the root's method chain and reports
// whether any link attaches an absolute-styled child: a Child/
// Children/TryChildren call with an argument whose own construction
// chain carries .Absolute() or a Class literal containing the
// "absolute" class. Under the pin's Relative default the receiving
// element was the containing block of that child (Taffy places every
// absolute child against its direct parent); under the static default
// the child escapes to the nearest positioned ancestor, so the codemod
// pins .Relative() on the receiver, restoring the direct-parent
// resolution. The argument expressions are element-typed by the
// compile-time contract of Child/Children, so an "Absolute" link on an
// argument's chain is the position selector (a DefiniteLength's
// Absolute() could never appear there).
func chainAttachesAbsoluteChild(root *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	for _, link := range chainOf(root, parents) {
		if !childSelectors[link.sel.Sel.Name] || link.call == nil {
			continue
		}
		for _, arg := range link.call.Args {
			if exprIsAbsoluteElement(arg) {
				return true
			}
		}
	}
	return false
}

// exprIsAbsoluteElement reports whether an element-typed expression is
// styled position: absolute: its own method chain (the maximal run of
// selector calls whose receiver chain it is) carries an Absolute link
// or a Class literal with the "absolute" class (a variant prefix is
// looked through: hover:absolute still makes the child absolute in
// that state, and the parent relied on the Relative default there
// too). Identifiers and other non-chained expressions are unknown and
// report false (the construction site of a variable-held element is
// analyzed at its own Div() root).
func exprIsAbsoluteElement(expr ast.Expr) bool {
	cur := ast.Node(expr)
	for {
		call, ok := cur.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		if positionSelectors[sel.Sel.Name] {
			// A non-absolute position selector on the child's own
			// chain: the child is not absolute.
			return sel.Sel.Name == "Absolute"
		}
		if sel.Sel.Name == "Class" {
			if value, ok := classLiteralOf(call); ok && classHasAbsolute(value) {
				return true
			}
		}
		cur = sel.X
	}
}

// classHasAbsolute reports whether a class string's fields contain the
// absolute class (any variant prefix looked through, the important
// modifier stripped).
func classHasAbsolute(value string) bool {
	for _, field := range strings.Fields(value) {
		class := field
		if idx := strings.LastIndex(class, ":"); idx >= 0 {
			class = class[idx+1:]
		}
		class = strings.TrimSuffix(strings.TrimPrefix(class, "!"), "!")
		if class == "absolute" {
			return true
		}
	}
	return false
}

// classLiteralOf returns the first argument of a Class call when it is
// a string literal.
func classLiteralOf(call *ast.CallExpr) (string, bool) {
	if call == nil || len(call.Args) == 0 {
		return "", false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

// nodeParents maps every node to its parent node.
func nodeParents(file *ast.File) map[ast.Node]ast.Node {
	parents := make(map[ast.Node]ast.Node, 1024)
	v := &parentVisitor{parents: parents}
	ast.Walk(v, file)
	return parents
}

// parentVisitor records each node's parent while ast.Walk descends.
type parentVisitor struct {
	parents map[ast.Node]ast.Node
	stack   []ast.Node
}

// Visit implements ast.Visitor: push on entry, pop on the nil call.
func (p *parentVisitor) Visit(node ast.Node) ast.Visitor {
	if node == nil {
		p.stack = p.stack[:len(p.stack)-1]
		return nil
	}
	if n := len(p.stack); n > 0 {
		p.parents[node] = p.stack[n-1]
	}
	p.stack = append(p.stack, node)
	return p
}
