package main

import (
	"go/format"
	"strings"
	"testing"
)

// gofmtSrc canonicalizes a hand-written expected source.
func gofmtSrc(src string) string {
	formatted, err := format.Source([]byte(src))
	if err != nil {
		panic("displaycodemod test: bad expected source: " + err.Error())
	}
	return string(formatted)
}

// prologue is the standard file header for snippets that exercise
// gpui-qualified chains.
const prologue = `package main

import "github.com/hoangvu12/gpui-go/gpui"

func f() {
`

const epilogue = "}\n"

// snippet builds a full, gofmt-formatted source file around the
// statement body, so expected and actual compare canonically.
func snippet(body string) string {
	formatted, err := format.Source([]byte(prologue + body + epilogue))
	if err != nil {
		panic("displaycodemod test: bad snippet: " + err.Error())
	}
	return string(formatted)
}

// rewriteOnce runs the codemod over one full source text and returns
// the rewritten source (the input unchanged when nothing applies).
func rewriteOnce(t *testing.T, src string) string {
	t.Helper()
	res, err := RewriteSource("snippet.go", []byte(src))
	if err != nil {
		t.Fatalf("RewriteSource: %v", err)
	}
	if !res.Changed {
		return src
	}
	return string(res.Final)
}

// assertStable rewrites until a fixed point (at most a couple of
// rounds) and reports the result; the codemod must be idempotent.
func assertStable(t *testing.T, src string) string {
	t.Helper()
	out := rewriteOnce(t, src)
	again := rewriteOnce(t, out)
	if again != out {
		t.Fatalf("codemod not idempotent:\nfirst:\n%s\nsecond:\n%s", out, again)
	}
	return out
}

// TestFlexVocabularyPinsFlex covers the core post-71 rule
// (browser-core 71, spec D6): an element whose chain carries flex/grid
// CONTAINER vocabulary without stating display gets .Flex()
// immediately after the constructor — under the block default those
// properties are inert, so the pin restores the pre-71 layout — and a
// second run changes nothing.
func TestFlexVocabularyPinsFlex(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "flex direction vocabulary",
			body: "d := gpui.Div().FlexCol().W4()",
			want: "d := gpui.Div().Flex().FlexCol().W4()",
		},
		{
			name: "gap vocabulary",
			body: "d := gpui.Div().W4().P2().Gap(gpui.Px(4))",
			want: "d := gpui.Div().Flex().W4().P2().Gap(gpui.Px(4))",
		},
		{
			name: "item alignment vocabulary",
			body: "d := gpui.Div().ItemsCenter()",
			want: "d := gpui.Div().Flex().ItemsCenter()",
		},
		{
			name: "justify vocabulary",
			body: "d := gpui.Div().JustifyBetween()",
			want: "d := gpui.Div().Flex().JustifyBetween()",
		},
		{
			name: "wrap vocabulary",
			body: "d := gpui.Div().FlexWrap()",
			want: "d := gpui.Div().Flex().FlexWrap()",
		},
		{
			name: "grid template vocabulary",
			body: "d := gpui.Div().GridCols(2).W4()",
			want: "d := gpui.Div().Flex().GridCols(2).W4()",
		},
		{
			name: "multi-line chain",
			body: "d := gpui.Div().\n\tID(\"root\").\n\tFlexCol()",
			want: "d := gpui.Div().Flex().\n\tID(\"root\").\n\tFlexCol()",
		},
		{
			name: "constructor passed to a helper",
			body: "d := wrap(gpui.Div().FlexCol(), cx)",
			want: "d := wrap(gpui.Div().Flex().FlexCol(), cx)",
		},
		{
			name: "constructor in a composite literal",
			body: "d := box{inner: gpui.Div().Gap(gpui.Px(4))}",
			want: "d := box{inner: gpui.Div().Flex().Gap(gpui.Px(4))}",
		},
		{
			name: "returned from a helper",
			body: "return gpui.Div().ItemsStart()",
			want: "return gpui.Div().Flex().ItemsStart()",
		},
		{
			name: "IntoElement tail",
			body: "d := gpui.Div().FlexCol().W4().IntoElement()",
			want: "d := gpui.Div().Flex().FlexCol().W4().IntoElement()",
		},
		{
			name: "img with flex vocabulary",
			body: "d := gpui.Img(source).FlexCol()",
			want: "d := gpui.Img(source).Flex().FlexCol()",
		},
		{
			name: "canvas with flex vocabulary",
			body: "d := gpui.Canvas(p, q).Gap(gpui.Px(4))",
			want: "d := gpui.Canvas(p, q).Flex().Gap(gpui.Px(4))",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertStable(t, snippet(tc.body))
			if got != snippet(tc.want) {
				t.Fatalf("got:\n%s\nwant:\n%s", got, snippet(tc.want))
			}
		})
	}
}

// TestPlainChainsUseTheBlockDefault covers the other half of the
// post-71 rule: a chain with no display selector and no flex/grid
// container vocabulary deliberately uses the browser default (Block,
// like an HTML div), and the tool must leave it byte-for-byte alone.
// Flex ITEM vocabulary (grow/shrink/basis/self) is parent-relative and
// does not pin either.
func TestPlainChainsUseTheBlockDefault(t *testing.T) {
	bodies := []string{
		"d := gpui.Div()",
		"d := gpui.Div().W4().P2()",
		"d := gpui.Div().\n\tID(\"root\").\n\tSizeFull()",
		"d := wrap(gpui.Div().W4(), cx)",
		"d := z.dragRoot(gpui.Div(), cx)",
		"d := box{inner: gpui.Div().ID(\"x\")}",
		"return gpui.Div()",
		// Item vocabulary: live whenever the PARENT is a flex container,
		// regardless of this element's own display.
		"d := gpui.Div().Flex1().FlexBasis(gpui.AutoLength()).FlexShrink0().SelfStart()",
		"d := gpui.Div().FlexGrow(1).W4()",
		// State styles build a refinement; the base chain stays plain.
		"d := gpui.Div().W4().Hover(gpui.Styles().Bg(x))",
		"d := gpui.Img(source).W4()",
		"d := gpui.Canvas(p, q).SizeFull()",
		"d := gpui.Div().W4().IntoElement()",
	}
	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			src := snippet(body)
			if got := assertStable(t, src); got != src {
				t.Fatalf("changed a plain (deliberate block) chain:\n%s", got)
			}
		})
	}
}

// TestExplicitDisplayLeavesChainUnchanged covers every way a chain can
// state its display: the selector in any position of the chain, the
// general Display(...), a Class literal with a Tailwind display class
// (plain or state-prefixed). All must be left byte-for-byte alone.
func TestExplicitDisplayLeavesChainUnchanged(t *testing.T) {
	selectors := []string{
		"Flex",
		"Block",
		"Grid",
		"Inline",
		"InlineFlex",
		"InlineBlock",
		"InlineGrid",
		"Contents",
		"FlowRoot",
		"Hidden",
	}
	for _, sel := range selectors {
		t.Run(sel+"_as_first_link", func(t *testing.T) {
			src := snippet("d := gpui.Div()." + sel + "().W4()")
			if got := assertStable(t, src); got != src {
				t.Fatalf("changed despite explicit display:\n%s", got)
			}
		})
		t.Run(sel+"_as_later_link", func(t *testing.T) {
			src := snippet("d := gpui.Div().W4()." + sel + "().P2()")
			if got := assertStable(t, src); got != src {
				t.Fatalf("changed despite explicit display later in the chain:\n%s", got)
			}
		})
		t.Run(sel+"_on_multiline_chain", func(t *testing.T) {
			src := snippet("d := gpui.Div().\n\tW4().\n\t" + sel + "().\n\tP2()")
			if got := assertStable(t, src); got != src {
				t.Fatalf("changed despite explicit display:\n%s", got)
			}
		})
	}
	t.Run("Display_call", func(t *testing.T) {
		src := snippet("d := gpui.Div().W4().Display(gpui.DisplayFlex).P2()")
		if got := assertStable(t, src); got != src {
			t.Fatalf("changed despite Display():\n%s", got)
		}
	})
	t.Run("img_with_explicit_display", func(t *testing.T) {
		src := snippet("d := gpui.Img(source).W4().Hidden()")
		if got := assertStable(t, src); got != src {
			t.Fatalf("changed despite explicit display:\n%s", got)
		}
	})
	classCases := []struct {
		name string
		body string
	}{
		{"class flex", `d := gpui.Div().Class("flex items-center gap-2")`},
		{"class block", `d := gpui.Div().Class("p-2 block w-full")`},
		{"class grid", `d := gpui.Div().Class("grid grid-cols-3")`},
		{"class hidden", `d := gpui.Div().Class("hidden")`},
		{"class contents", `d := gpui.Div().Class("contents")`},
		{"class flow-root", `d := gpui.Div().Class("flow-root")`},
		{"class inline-flex", `d := gpui.Div().Class("inline-flex")`},
		{"class inline-block", `d := gpui.Div().Class("inline-block")`},
		{"class inline-grid", `d := gpui.Div().Class("inline-grid")`},
		{"class inline", `d := gpui.Div().Class("inline")`},
		{"class before other links", `d := gpui.Div().Class("flex").W4()`},
	}
	for _, tc := range classCases {
		t.Run(tc.name, func(t *testing.T) {
			src := snippet(tc.body)
			if got := assertStable(t, src); got != src {
				t.Fatalf("changed despite a display class:\n%s", got)
			}
		})
	}
}

// TestClassFlexContainerVocabularyPins covers Class strings that
// carry flex/grid container classes without a display class: under
// the block default those classes are inert, so the root is pinned
// (a display class applied later would still overwrite the pin).
func TestClassFlexContainerVocabularyPins(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "flex-col class",
			body: `d := gpui.Div().Class("flex-col p-2")`,
			want: `d := gpui.Div().Flex().Class("flex-col p-2")`,
		},
		{
			name: "gap class",
			body: `d := gpui.Div().Class("w-full gap-2")`,
			want: `d := gpui.Div().Flex().Class("w-full gap-2")`,
		},
		{
			name: "items class",
			body: `d := gpui.Div().Class("items-center")`,
			want: `d := gpui.Div().Flex().Class("items-center")`,
		},
		{
			name: "justify class",
			body: `d := gpui.Div().Class("justify-between p-2")`,
			want: `d := gpui.Div().Flex().Class("justify-between p-2")`,
		},
		{
			name: "grid-cols class",
			body: `d := gpui.Div().Class("grid-cols-3")`,
			want: `d := gpui.Div().Flex().Class("grid-cols-3")`,
		},
		{
			// A variant sets the property only in that state, and the
			// base display still comes from the default — the pin keeps
			// the variant live.
			name: "container class under a state variant",
			body: `d := gpui.Div().ID("x").Class("hover:flex-col p-2")`,
			want: `d := gpui.Div().Flex().ID("x").Class("hover:flex-col p-2")`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := assertStable(t, snippet(tc.body))
			if got != snippet(tc.want) {
				t.Fatalf("got:\n%s\nwant:\n%s", got, snippet(tc.want))
			}
		})
	}
}

// TestClassNonDisplayNonContainerLeftAlone covers Class strings that
// neither set display nor carry flex/grid container vocabulary, and
// non-literal Class arguments: the chain uses the block default
// deliberately (or styles itself through the class layer at runtime),
// and the tool leaves it alone.
func TestClassNonDisplayNonContainerLeftAlone(t *testing.T) {
	bodies := []string{
		`d := gpui.Div().Class("p-2 w-full")`,
		"d := gpui.Div().Class(classes)",
		`d := gpui.Div().Class("hover:bg-slate-700")`,
		`d := gpui.Div().ID("x").Class("hover:hidden p-2")`,
	}
	for _, body := range bodies {
		t.Run(body, func(t *testing.T) {
			src := snippet(body)
			if got := assertStable(t, src); got != src {
				t.Fatalf("changed a plain class chain:\n%s", got)
			}
		})
	}
}

// TestNestedRoots covers chains inside chains: the inner root's chain
// is analyzed independently of the outer one.
func TestNestedRoots(t *testing.T) {
	src := snippet("d := gpui.Div().FlexCol().\n\tChild(gpui.Div().Block().P2())")
	want := snippet("d := gpui.Div().Flex().FlexCol().\n\tChild(gpui.Div().Block().P2())")
	if got := assertStable(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestPackageGpuiUnqualified covers the gpui package's own files, where
// the constructors are unqualified.
func TestPackageGpuiUnqualified(t *testing.T) {
	src := "package gpui\n\nfunc f() *DivElement {\n\treturn Div().FlexCol().ID(\"x\")\n}\n"
	want := "package gpui\n\nfunc f() *DivElement {\n\treturn Div().Flex().FlexCol().ID(\"x\")\n}\n"
	if got := assertStable(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	src = "package gpui\n\nfunc f() *DivElement {\n\treturn Div().Block().ID(\"x\")\n}\n"
	if got := assertStable(t, src); got != src {
		t.Fatalf("changed despite Block:\n%s", got)
	}
}

// TestAliasedAndDotImports covers non-default import spellings.
func TestAliasedAndDotImports(t *testing.T) {
	src := `package main

import g "github.com/hoangvu12/gpui-go/gpui"

func f() {
	d := g.Div().FlexCol().W4()
}
`
	want := strings.Replace(src, "g.Div().FlexCol().W4()", "g.Div().Flex().FlexCol().W4()", 1)
	if got := assertStable(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	src = `package main

import . "github.com/hoangvu12/gpui-go/gpui"

func f() {
	d := Div().FlexCol().W4()
}
`
	want = strings.Replace(src, "Div().FlexCol().W4()", "Div().Flex().FlexCol().W4()", 1)
	if got := assertStable(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestLocalDivIsNotARoot covers a package that defines its own Div
// helper without importing gpui: unqualified Div() there is not a gpui
// root and must not be touched.
func TestLocalDivIsNotARoot(t *testing.T) {
	src := "package main\n\nfunc Div() *box { return &box{} }\n\nfunc f() *box {\n\treturn Div().w4()\n}\n"
	if got := assertStable(t, src); got != src {
		t.Fatalf("changed a local Div:\n%s", got)
	}
}

// TestStylesBuilderIsNotARoot covers gpui.Styles(): it builds a bare
// refinement for state styles, not an element relying on the default
// display.
func TestStylesBuilderIsNotARoot(t *testing.T) {
	src := snippet("s := gpui.Styles().W4()")
	if got := assertStable(t, src); got != src {
		t.Fatalf("changed a Styles builder:\n%s", got)
	}
}

// TestVariableReassignmentPinsTheRoot covers chains split across
// statements: the constructor call is pinned even though the display
// is set (or not) in later statements — a later setter overwrites the
// pin, so behaviour is preserved either way.
func TestVariableReassignmentPinsTheRoot(t *testing.T) {
	src := snippet("d := gpui.Div().FlexCol()\nd = d.W4()\nd.Block()")
	want := snippet("d := gpui.Div().Flex().FlexCol()\nd = d.W4()\nd.Block()")
	if got := assertStable(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	// No container vocabulary anywhere: the plain chain stays at the
	// block default.
	src = snippet("d := gpui.Div()\nd = d.W4()")
	if got := assertStable(t, src); got != src {
		t.Fatalf("changed a plain reassigned chain:\n%s", got)
	}
}

// TestCommentsPreserved covers comments interleaved with a rewritten
// chain: byte-offset insertion leaves them in place.
func TestCommentsPreserved(t *testing.T) {
	src := `package main

import "github.com/hoangvu12/gpui-go/gpui"

func f() {
	d := gpui.Div(). // the box
		FlexCol(). // its direction
		W4()
	_ = d
}
`
	want := gofmtSrc(strings.Replace(src, "gpui.Div(). // the box", "gpui.Div().Flex(). // the box", 1))
	if got := assertStable(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestGeneratedFilesSkipped covers the generated-file skip: a file
// whose header says "Code generated ... DO NOT EDIT." is left alone
// however pin-able its elements are, in both spellings the tree uses
// (single line, and split across the leading comment group). A "Code
// generated" header without "DO NOT EDIT." (tailwindgen's committed
// palette) and a marker inside the body (a generator template or a
// test assertion) do not skip the file.
func TestGeneratedFilesSkipped(t *testing.T) {
	pkg := func(header string) string {
		return header + "package main\n\nimport \"github.com/hoangvu12/gpui-go/gpui\"\n\nfunc f() {\n\td := gpui.Div().FlexCol()\n}\n"
	}
	skipped := []struct {
		name   string
		header string
	}{
		{
			name:   "single line",
			header: "// Code generated by cmd/tool; DO NOT EDIT.\n\n",
		},
		{
			name: "split across the comment group",
			header: "// Code generated by cmd/stylegen from the pinned\n" +
				"// gpui_macros/src/styles.rs (254b5db);\n" +
				"// DO NOT EDIT.\n\n",
		},
		{
			name:   "after a build tag",
			header: "//go:build windows\n\n// Code generated by cmd/tool; DO NOT EDIT.\n\n",
		},
	}
	for _, tc := range skipped {
		t.Run(tc.name, func(t *testing.T) {
			src := pkg(tc.header)
			res, err := RewriteSource("generated.go", []byte(src))
			if err != nil {
				t.Fatalf("RewriteSource: %v", err)
			}
			if res.Changed {
				t.Fatalf("rewrote a generated file:\n%s", res.Final)
			}
		})
	}
	rewritten := []struct {
		name   string
		header string
	}{
		{
			// Not machine-recognized as generated (no DO NOT EDIT),
			// so it is rewritten like any committed source.
			name:   "no DO NOT EDIT",
			header: "// Code generated from a source file by a one-time conversion.\n\n",
		},
		{
			name:   "marker inside the body is not a header",
			header: "",
		},
	}
	for _, tc := range rewritten {
		t.Run(tc.name, func(t *testing.T) {
			src := pkg(tc.header)
			if tc.name == "marker inside the body is not a header" {
				src = strings.Replace(src, "d := gpui.Div().FlexCol()",
					"d := gpui.Div().FlexCol() // Code generated; DO NOT EDIT.", 1)
			}
			got := rewriteOnce(t, src)
			want := strings.Replace(src, "gpui.Div().FlexCol()", "gpui.Div().Flex().FlexCol()", 1)
			if got != want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TestParseErrorReported covers a broken file: the error is returned
// and nothing is rewritten.
func TestParseErrorReported(t *testing.T) {
	if _, err := RewriteSource("bad.go", []byte("package main\nfunc (")); err == nil {
		t.Fatal("expected a parse error")
	}
}

// TestIdempotenceOnMixedFile runs the codemod twice over a file that
// mixes pinned, flex-vocabulary, explicit and plain (deliberate
// block) chains.
func TestIdempotenceOnMixedFile(t *testing.T) {
	src := `package main

import "github.com/hoangvu12/gpui-go/gpui"

func f() {
	a := gpui.Div().Flex().W4()
	b := gpui.Div().W4().Block()
	c := gpui.Div().W4().Class("flex")
	d := gpui.Div().Hidden()
	e := gpui.Div().P2()
	g := gpui.Img(src).Flex()
	h := gpui.Img(src).SizeFull()
	i := gpui.Div().FlexCol()
	j := gpui.Div().FlexShrink0()
	k := gpui.Div().Class("items-center p-2")
}
`
	first := rewriteOnce(t, src)
	second := rewriteOnce(t, first)
	if second != first {
		t.Fatalf("not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	for _, want := range []string{
		"a := gpui.Div().Flex().W4()",
		"b := gpui.Div().W4().Block()",
		"c := gpui.Div().W4().Class(\"flex\")",
		"d := gpui.Div().Hidden()",
		"e := gpui.Div().P2()",
		"g := gpui.Img(src).Flex()",
		"h := gpui.Img(src).SizeFull()",
		"i := gpui.Div().Flex().FlexCol()",
		// Item vocabulary and no-vocabulary chains stay at the block
		// default.
		"j := gpui.Div().FlexShrink0()",
		"k := gpui.Div().Flex().Class(\"items-center p-2\")",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("missing %q in:\n%s", want, first)
		}
	}
}
