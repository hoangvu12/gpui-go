// Command tailwindgen emits the generated halves of the Tailwind
// authoring layer (ADR 0001):
//
//   - gpui/class_generated.go: the class switch behind Styled.Class. It
//     maps zero-argument Styled methods to their Tailwind class names
//     (ItemsCenter → "items-center", ShadowMd → "shadow-md") and calls
//     the method, so a class and its method can never disagree. The
//     spacing and sizing classes (W1_2 → "w-1/2", P0p5 → "p-0.5", MtNeg2
//     → "-mt-2") are left to Class's length fallback, which is cheaper to
//     compile.
//   - gpui/class_generated_test.go: each of those spacing and sizing
//     methods' field writes, which the tests compare with the fallback,
//     and the GPUI-only method classes Class rejects.
//   - gpui/tailwind_palette_generated.go: the default color palette the
//     class parser resolves (bg-slate-800, text-white, …).
//   - tw/palette_generated.go: the same palette as Go values
//     (tw.Slate800, tw.White, …).
//
// It reads the Styled method declarations from source (go/parser), so it
// needs no compiled gpui. It is a maintainer tool: the outputs are
// committed and consumers never run it. Regenerate from the gpui
// directory with `go generate` (class.go carries the directive) or:
//
//	go run ./cmd/tailwindgen -gpui gpui -tw tw
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

func main() {
	gpuiDir := flag.String("gpui", ".", "the gpui package directory")
	twDir := flag.String("tw", "../tw", "the tw package directory")
	flag.Parse()

	fset, methods, err := zeroArgStyledMethods(*gpuiDir)
	if err != nil {
		fail(err)
	}
	seen := map[string]string{}          // class → method
	switchClasses := map[string]string{} // class → method
	var lengthClasses []lengthClass
	var gpuiOnly []string
	for _, m := range methods {
		name := m.Name.Name
		class := classOf(name)
		if prev, dup := seen[class]; dup {
			fail(fmt.Errorf("methods %s and %s both map to class %q", prev, name, class))
		}
		seen[class] = name
		switch classKind(class) {
		case kindSwitch:
			switchClasses[class] = name
		case kindLength:
			lengthClasses = append(lengthClasses, lengthClass{class, name, fieldWrites(fset, m)})
		case kindGPUIOnly:
			gpuiOnly = append(gpuiOnly, class)
		}
	}
	write(filepath.Join(*gpuiDir, "class_generated.go"), classSwitch(switchClasses))
	write(filepath.Join(*gpuiDir, "class_generated_test.go"), classTables(lengthClasses, gpuiOnly))
	write(filepath.Join(*gpuiDir, "tailwind_palette_generated.go"), gpuiPalette())
	write(filepath.Join(*twDir, "palette_generated.go"), twPalette())
	fmt.Printf("tailwindgen: %d method classes (%d in the switch, %d by the length fallback, %d GPUI-only), %d palette colors\n",
		len(seen), len(switchClasses), len(lengthClasses), len(gpuiOnly), len(palette))
}

// Method classes split three ways. Instantiating a Styled method for
// *StyleBuilder costs compile time inside gpui (~1.5 ms each), so the
// switch holds only the classes Class has no other way to apply; the
// spacing and sizing classes go through Class's length fallback
// (gpui/class.go lengthProps), and the generated test proves the
// fallback writes exactly what each method writes.
const (
	kindSwitch   = iota // applied by calling the method
	kindLength          // applied by the length fallback
	kindGPUIOnly        // no Tailwind class: Class rejects it
)

// lengthShape matches a box-prefix class with a scale, fraction, px,
// full or auto value: the classes the length fallback parses.
var lengthShape = regexp.MustCompile(`^(-?)(inset|top|right|bottom|left|gap-x|gap-y|gap|px|py|pt|pr|pb|pl|p|mx|my|mt|mr|mb|ml|m|min-w|min-h|max-w|max-h|min-size|max-size|size|w|h)-([0-9][0-9./]*|px|full|auto)$`)

// negativePrefixes are the prefixes Tailwind (and the fallback) allow a
// negative value on: margins and insets.
var negativePrefixes = map[string]bool{
	"inset": true, "top": true, "right": true, "bottom": true, "left": true,
	"m": true, "mx": true, "my": true, "mt": true, "mr": true, "mb": true, "ml": true,
}

func classKind(class string) int {
	m := lengthShape.FindStringSubmatch(class)
	switch {
	case m == nil:
		return kindSwitch
	case m[2] == "min-size" || m[2] == "max-size" || (m[1] == "-" && !negativePrefixes[m[2]]):
		return kindGPUIOnly
	}
	return kindLength
}

// lengthClass is a method class the length fallback applies, with the
// method's field writes.
type lengthClass struct {
	class, method, writes string
}

// fieldWrites renders the body of a generated length method
// (`style := s.StyleMut(); style.F = Some(…); …; return s.self`) as
// writes to a refinement named st.
func fieldWrites(fset *token.FileSet, fn *ast.FuncDecl) string {
	var writes []string
	for _, stmt := range fn.Body.List {
		switch st := stmt.(type) {
		case *ast.ReturnStmt:
			continue
		case *ast.AssignStmt:
			if st.Tok == token.DEFINE {
				continue // style := s.StyleMut()
			}
			sel, ok := st.Lhs[0].(*ast.SelectorExpr)
			if len(st.Lhs) == 1 && len(st.Rhs) == 1 && ok {
				var rhs bytes.Buffer
				if err := format.Node(&rhs, fset, st.Rhs[0]); err != nil {
					fail(err)
				}
				writes = append(writes, "st."+sel.Sel.Name+" = "+rhs.String())
				continue
			}
		}
		fail(fmt.Errorf("%s: unexpected statement shape in a length method", fn.Name.Name))
	}
	return strings.Join(writes, "; ")
}

// styledSources are the files declaring Styled methods.
var styledSources = []string{"styled.go", "styled_generated.go", "styled_tailwind.go", "styled_masks.go"}

// zeroArgStyledMethods returns the Styled[Self] methods that take no
// arguments and carry only the lazy marker: the ones a bare class can
// name.
func zeroArgStyledMethods(dir string) (*token.FileSet, []*ast.FuncDecl, error) {
	fset := token.NewFileSet()
	var out []*ast.FuncDecl
	for _, name := range styledSources {
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, nil, err
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !fn.Name.IsExported() || !isStyledReceiver(fn.Recv) {
				continue
			}
			if fn.Type.Params.NumFields() != 0 || !onlyLazyMarker(fn.Type.TypeParams) {
				continue
			}
			if skipMethods[fn.Name.Name] {
				continue
			}
			out = append(out, fn)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name.Name < out[j].Name.Name })
	return fset, out, nil
}

// skipMethods are zero-argument Styled methods that are not style
// utilities.
var skipMethods = map[string]bool{
	"StyleBase": true,
}

// isStyledReceiver reports whether the receiver is *Styled[Self].
func isStyledReceiver(recv *ast.FieldList) bool {
	if len(recv.List) != 1 {
		return false
	}
	star, ok := recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	idx, ok := star.X.(*ast.IndexExpr)
	if !ok {
		return false
	}
	id, ok := idx.X.(*ast.Ident)
	return ok && id.Name == "Styled"
}

// onlyLazyMarker reports whether the type parameters are exactly
// [_ lazy].
func onlyLazyMarker(tparams *ast.FieldList) bool {
	if tparams == nil || len(tparams.List) != 1 || len(tparams.List[0].Names) != 1 {
		return false
	}
	id, ok := tparams.List[0].Type.(*ast.Ident)
	return ok && id.Name == "lazy" && tparams.List[0].Names[0].Name == "_"
}

// classOf converts a Styled method name to its Tailwind class name:
// CamelCase words become kebab-case, "Neg" moves to a leading minus,
// a "p" between digits is a decimal point (0p5 → 0.5) and an underscore
// between digits is a fraction slash (1_2 → 1/2).
func classOf(method string) string {
	words := splitWords(method)
	neg := false
	parts := words[:0:0]
	for _, w := range words {
		if w == "Neg" {
			neg = true
			continue
		}
		w = strings.ToLower(w)
		w = strings.ReplaceAll(w, "_", "/")
		if len(w) > 2 && unicode.IsDigit(rune(w[0])) && strings.Contains(w, "p") {
			// 0p5 → 0.5, 2p5 → 2.5 (never 2xl: those have no digit after p).
			if i := strings.IndexByte(w, 'p'); i > 0 && i+1 < len(w) && unicode.IsDigit(rune(w[i+1])) {
				w = w[:i] + "." + w[i+1:]
			}
		}
		parts = append(parts, w)
	}
	class := strings.Join(parts, "-")
	if neg {
		class = "-" + class
	}
	return class
}

// splitWords splits a CamelCase method name into words. A run of digits
// is one word together with a decimal ("0p5") or fraction ("1_2")
// continuation and any trailing lower-case letters ("2xl").
func splitWords(name string) []string {
	var words []string
	r := []rune(name)
	for i := 0; i < len(r); {
		start := i
		switch {
		case unicode.IsUpper(r[i]):
			i++
			for i < len(r) && unicode.IsLower(r[i]) {
				i++
			}
		case unicode.IsDigit(r[i]):
			for i < len(r) && unicode.IsDigit(r[i]) {
				i++
			}
			if i+1 < len(r) && (r[i] == 'p' || r[i] == '_') && unicode.IsDigit(r[i+1]) {
				i++
				for i < len(r) && unicode.IsDigit(r[i]) {
					i++
				}
			}
			for i < len(r) && unicode.IsLower(r[i]) {
				i++
			}
		case r[i] == '_':
			i++
			continue
		default:
			i++
		}
		words = append(words, string(r[start:i]))
	}
	return words
}

const header = "// Code generated by cmd/tailwindgen; DO NOT EDIT.\n\n"

// classSwitch emits applyMethodClass.
func classSwitch(classes map[string]string) []byte {
	names := make([]string, 0, len(classes))
	for c := range classes {
		names = append(names, c)
	}
	sort.Strings(names)
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString("package gpui\n\n")
	b.WriteString("// applyMethodClass applies the class named after a zero-argument Styled\n")
	b.WriteString("// method by calling that method, and reports whether the class is one.\n")
	b.WriteString("func applyMethodClass(class string, b *StyleBuilder) bool {\n\tswitch class {\n")
	for _, c := range names {
		fmt.Fprintf(&b, "\tcase %q:\n\t\tb.%s()\n", c, classes[c])
	}
	b.WriteString("\tdefault:\n\t\treturn false\n\t}\n\treturn true\n}\n\n")
	fmt.Fprintf(&b, "// methodClassCount is the number of classes applyMethodClass knows.\nconst methodClassCount = %d\n", len(names))
	return b.Bytes()
}

// classTables emits the test tables for the method classes outside the
// switch.
func classTables(lengths []lengthClass, gpuiOnly []string) []byte {
	sort.Slice(lengths, func(i, j int) bool { return lengths[i].class < lengths[j].class })
	sort.Strings(gpuiOnly)
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString("package gpui\n\n")
	b.WriteString("// lengthMethodClasses are the zero-argument Styled methods whose classes\n")
	b.WriteString("// Class applies with its length fallback rather than the method switch,\n")
	b.WriteString("// each with the method's own field writes.\n")
	b.WriteString("var lengthMethodClasses = []struct {\n\tclass, method string\n\twant          func(st *StyleRefinement)\n}{\n")
	for _, l := range lengths {
		fmt.Fprintf(&b, "\t{%q, %q, func(st *StyleRefinement) { %s }},\n", l.class, l.method, l.writes)
	}
	b.WriteString("}\n\n")
	b.WriteString("// gpuiOnlyMethodClasses are zero-argument Styled methods with no Tailwind\n")
	b.WriteString("// class (negative padding, gap and sizes; min-size, max-size). Class\n")
	b.WriteString("// rejects them; the methods remain.\n")
	b.WriteString("var gpuiOnlyMethodClasses = []string{\n")
	for _, c := range gpuiOnly {
		fmt.Fprintf(&b, "\t%q,\n", c)
	}
	b.WriteString("}\n")
	return b.Bytes()
}

// gpuiPalette emits the palette map the class parser reads.
func gpuiPalette() []byte {
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString("package gpui\n\n")
	b.WriteString("// tailwindPalette is Tailwind CSS v3.4's default color palette\n")
	b.WriteString("// (MIT, Copyright (c) Tailwind Labs, Inc.; see THIRD_PARTY_NOTICES.md),\n")
	b.WriteString("// keyed by class color name, as opaque 0xRRGGBB values.\n")
	b.WriteString("var tailwindPalette = map[string]uint32{\n")
	b.WriteString("\t\"black\": 0x000000,\n\t\"white\": 0xffffff,\n")
	for _, fam := range families {
		for _, sh := range shades {
			key := fam + "-" + sh
			fmt.Fprintf(&b, "\t%q: 0x%06x,\n", key, palette[key])
		}
	}
	b.WriteString("}\n")
	return b.Bytes()
}

// twPalette emits the tw package's color values.
func twPalette() []byte {
	var b bytes.Buffer
	b.WriteString(header)
	b.WriteString("package tw\n\nimport \"github.com/hoangvu12/gpui-go/gpui\"\n\n")
	b.WriteString("// The Tailwind CSS v3.4 default palette (MIT, Copyright (c) Tailwind\n")
	b.WriteString("// Labs, Inc.; see THIRD_PARTY_NOTICES.md). Each value is the color the\n")
	b.WriteString("// class of the same name uses: tw.Slate800 is bg-slate-800's color.\n")
	b.WriteString("var (\n")
	b.WriteString("\t// Black is #000000 (black).\n\tBlack = gpui.Rgb(0x000000)\n")
	b.WriteString("\t// White is #ffffff (white).\n\tWhite = gpui.Rgb(0xffffff)\n")
	b.WriteString("\t// Transparent is fully transparent black (transparent).\n\tTransparent = gpui.Rgb(0x000000).Alpha(0)\n")
	for _, fam := range families {
		b.WriteString("\n")
		for _, sh := range shades {
			key := fam + "-" + sh
			name := strings.ToUpper(fam[:1]) + fam[1:] + sh
			fmt.Fprintf(&b, "\t// %s is #%06x (%s).\n\t%s = gpui.Rgb(0x%06x)\n", name, palette[key], key, name, palette[key])
		}
	}
	b.WriteString(")\n")
	return b.Bytes()
}

// write formats src and writes it to path.
func write(path string, src []byte) {
	formatted, err := format.Source(src)
	if err != nil {
		os.WriteFile(path+".broken", src, 0o644)
		fail(fmt.Errorf("%s: %w", path, err))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fail(err)
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "tailwindgen:", err)
	os.Exit(1)
}
