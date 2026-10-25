package main

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"strconv"
	"strings"
)

// gpuiModulePath is the import path of the package that owns the
// default-display element constructors.
const gpuiModulePath = "github.com/hoangvu12/gpui-go/gpui"

// rootConstructors are the gpui constructors whose element style starts
// at Style::default() with an empty caller refinement (BindStyled with
// no style set): see gpui/div.go Div, gpui/img.go Img and
// gpui/canvas.go Canvas.
var rootConstructors = map[string]bool{
	"Div":    true,
	"Img":    true,
	"Canvas": true,
}

// displaySelectors are the Styled-vocabulary method names that set the
// display property (gpui/styled.go). FlexCol, FlexRow, Flex1, FlexWrap
// and friends set flexbox sub-properties, NOT display, so they are
// deliberately absent: an element laid out with FlexCol still relies on
// the default display.
var displaySelectors = map[string]bool{
	"Display":     true,
	"Block":       true,
	"Flex":        true,
	"Grid":        true,
	"Inline":      true,
	"InlineFlex":  true,
	"InlineBlock": true,
	"InlineGrid":  true,
	"Contents":    true,
	"FlowRoot":    true,
	"Hidden":      true,
}

// flexContainerSelectors are the Styled-vocabulary method names that
// set properties applying only when the element ITSELF is a flex (or
// grid) container: direction, wrap, gap, item alignment, content
// distribution and grid templates. They are live under DisplayFlex
// (or DisplayGrid for the grid half) and inert under DisplayBlock, so
// since browser-core 71 (spec D6) flipped the default to Block, a
// chain carrying one of them WITHOUT an explicit display selector
// silently lost its layout: the properties went inert. The codemod
// restores the pre-71 behaviour by pinning .Flex() on exactly those
// chains. Flex ITEM vocabulary (Flex1, FlexGrow, FlexShrink,
// FlexBasis, Self*) is deliberately absent: those properties depend
// on the PARENT being a flex container, not on the element's own
// display, so an unpinned item inside a flex parent is correct.
var flexContainerSelectors = map[string]bool{
	"FlexDirection":   true,
	"FlexCol":         true,
	"FlexColReverse":  true,
	"FlexRow":         true,
	"FlexRowReverse":  true,
	"FlexWrap":        true,
	"FlexWrapReverse": true,
	"FlexNowrap":      true,
	"Gap":             true,
	"ItemsStart":      true,
	"ItemsEnd":        true,
	"ItemsCenter":     true,
	"ItemsBaseline":   true,
	"ItemsStretch":    true,
	"JustifyStart":    true,
	"JustifyEnd":      true,
	"JustifyCenter":   true,
	"JustifyBetween":  true,
	"JustifyAround":   true,
	"JustifyEvenly":   true,
	"ContentNormal":   true,
	"ContentCenter":   true,
	"ContentStart":    true,
	"ContentEnd":      true,
	"ContentBetween":  true,
	"ContentAround":   true,
	"ContentEvenly":   true,
	"ContentStretch":  true,
	// Grid templates: live only under DisplayGrid. A chain that sets a
	// grid template without .Grid() was a flex box before 71 (the
	// template inert); the pin restores that pre-71 behaviour.
	"GridTemplateCols":   true,
	"GridTemplateRows":   true,
	"GridCols":           true,
	"GridColsMinContent": true,
	"GridColsMaxContent": true,
	"GridRows":           true,
	"GridRowsMinContent": true,
	"GridRowsMaxContent": true,
}

// tailwindFlexContainerPrefixes are the Tailwind class prefixes whose
// classes apply only under a flex (or grid) container display — the
// class layer of flexContainerSelectors (gap-2, flex-col, items-center,
// justify-between, content-start, grid-cols-3). A variant prefix
// (hover:flex-col) still counts: the base display comes from the
// default, so the element needs the pin for the variant to work.
var tailwindFlexContainerPrefixes = []string{
	"flex-col",
	"flex-row",
	"flex-wrap",
	"flex-nowrap",
	"gap-",
	"items-",
	"justify-",
	"content-",
	"grid-cols-",
	"grid-rows-",
}

// tailwindDisplayClasses are the Tailwind classes that set display
// (gpui/class_generated.go). A Class literal containing one of them
// without a variant prefix makes the chain's display explicit; under a
// variant (hover:flex) it doesn't.
var tailwindDisplayClasses = map[string]bool{
	"flex":         true,
	"block":        true,
	"grid":         true,
	"hidden":       true,
	"inline":       true,
	"inline-flex":  true,
	"inline-block": true,
	"inline-grid":  true,
	"contents":     true,
	"flow-root":    true,
}

// flexInsertion is the text inserted after a root constructor call.
const flexInsertion = ".Flex()"

// Insertion is one .Flex() insertion into the original source.
type Insertion struct {
	// Offset is the byte offset in the ORIGINAL source where Text is
	// inserted (the end of the root constructor call).
	Offset int
	// Text is the inserted text (".Flex()").
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
	// already sets display.
	Explicit int
	// Defaults is the number of roots left at the block default: no
	// display selector and no flex/grid container vocabulary — the
	// browser-like default browser-core 71 (spec D6) flipped to, which
	// a plain chain now deliberately uses.
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

	for _, root := range roots {
		res.Roots++
		if chainSetsDisplay(root, parents) {
			res.Explicit++
			continue
		}
		// Post-71 rule (browser-core 71, spec D6): the default display
		// is Block, so a plain chain with no flex/grid container
		// vocabulary deliberately uses the browser default and is left
		// alone. Only a chain whose container vocabulary went inert
		// under Block — the elements that silently changed layout at
		// the flip — is pinned back to Flex.
		if !chainUsesFlexContainer(root, parents) {
			res.Defaults++
			continue
		}
		pos := fset.Position(root.End())
		res.Edits = append(res.Edits, Insertion{Offset: pos.Offset, Text: flexInsertion})
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
	out := make([]byte, 0, len(src)+len(edits)*len(flexInsertion))
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

// isGenerated reports whether the file carries a generated-code
// header: the Go convention's "Code generated" line with "DO NOT
// EDIT." — both spellings the tree uses, on one line (tsxbundle,
// class_generated.go) or split across the leading comment group
// (styled_generated.go). The marker must sit in a comment group before
// the package clause (a "Code generated" string inside the body, like
// a generator's template or a test assertion, is not a header), and a
// "Code generated" without "DO NOT EDIT." (tailwindgen's committed
// palette source) is not machine-recognized as generated and is
// rewritten like any other file.
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

// chainSetsDisplay walks the root's method chain (the consecutive
// selector calls built directly on the root's result) and reports
// whether any link sets display explicitly.
//
// The AST shape of a chain link: the receiver expression (the root or
// the previous call) is the X of a SelectorExpr, and that SelectorExpr
// is the Fun of the next CallExpr — the parent of a receiver is the
// selector, not the call (gpui.Div().W4() nests as Call(Fun: Sel(X:
// Call(Fun: Sel(X: pkg, Sel: Div)), Sel: W4))). A selector that is not
// called (a method value) ends the chain.
func chainSetsDisplay(root *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	cur := ast.Node(root)
	for {
		sel, ok := parents[cur].(*ast.SelectorExpr)
		if !ok || sel.X != cur {
			return false
		}
		call, ok := parents[sel].(*ast.CallExpr)
		if !ok || call.Fun != sel {
			return false
		}
		if displaySelectors[sel.Sel.Name] {
			return true
		}
		if sel.Sel.Name == "Class" && classLiteralSetsDisplay(call) {
			return true
		}
		cur = call
	}
}

// classLiteralSetsDisplay reports whether a Class call's first argument
// is a string literal containing a Tailwind display class. Non-literal
// arguments are unknown (reported as not explicit; pinning the root is
// safe because a class applied later overwrites the pin).
func classLiteralSetsDisplay(call *ast.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return false
	}
	for _, field := range strings.Fields(value) {
		// A variant-prefixed class (hover:hidden) sets display only in
		// that state, so the base display still relies on the default.
		if strings.Contains(field, ":") {
			continue
		}
		// The important modifier (!flex, flex!) still sets the base.
		class := strings.TrimSuffix(strings.TrimPrefix(field, "!"), "!")
		if tailwindDisplayClasses[class] {
			return true
		}
	}
	return false
}

// chainUsesFlexContainer walks the root's method chain (the same
// maximal run of selector calls chainSetsDisplay walks) and reports
// whether any link uses flex/grid CONTAINER vocabulary without the
// display being stated elsewhere: a container-only method selector, or
// a Class literal carrying a flex/grid container class. Such a chain
// was a flex box before browser-core 71 flipped the default display to
// Block (its container properties were live); under the block default
// they are inert, so the codemod pins .Flex() to restore the layout.
// Item vocabulary (Flex1, FlexShrink0, Self*) is parent-relative and
// does not count.
func chainUsesFlexContainer(root *ast.CallExpr, parents map[ast.Node]ast.Node) bool {
	cur := ast.Node(root)
	for {
		sel, ok := parents[cur].(*ast.SelectorExpr)
		if !ok || sel.X != cur {
			return false
		}
		call, ok := parents[sel].(*ast.CallExpr)
		if !ok || call.Fun != sel {
			return false
		}
		if flexContainerSelectors[sel.Sel.Name] {
			return true
		}
		if sel.Sel.Name == "Class" && classLiteralUsesFlexContainer(call) {
			return true
		}
		cur = call
	}
}

// classLiteralUsesFlexContainer reports whether a Class call's first
// argument is a string literal containing a Tailwind flex/grid
// container class (gap-2, flex-col, items-center, ...). Non-literal
// arguments are unknown and do not trigger the pin: a chain that
// styles itself through an unknown class string is the author's
// statement, and the class layer (later wins) still governs display.
func classLiteralUsesFlexContainer(call *ast.CallExpr) bool {
	if len(call.Args) == 0 {
		return false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return false
	}
	for _, field := range strings.Fields(value) {
		// A variant-prefixed class (hover:flex-col) sets the property
		// only in that state, but the base display still comes from the
		// default — the element needs the flex pin for the variant to
		// work — so the variant prefix is looked through.
		class := field
		if idx := strings.LastIndex(class, ":"); idx >= 0 {
			class = class[idx+1:]
		}
		class = strings.TrimSuffix(strings.TrimPrefix(class, "!"), "!")
		for _, prefix := range tailwindFlexContainerPrefixes {
			if strings.HasPrefix(class, prefix) {
				return true
			}
		}
	}
	return false
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
