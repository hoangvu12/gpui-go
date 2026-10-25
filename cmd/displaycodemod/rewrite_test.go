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

// TestDefaultReliancePinsFlex covers the core rule: an element whose
// chain never selects display gets .Flex() immediately after the
// constructor, and a second run changes nothing.
func TestDefaultReliancePinsFlex(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "bare constructor",
			body: "d := gpui.Div()",
			want: "d := gpui.Div().Flex()",
		},
		{
			name: "plain chain",
			body: "d := gpui.Div().W4().P2()",
			want: "d := gpui.Div().Flex().W4().P2()",
		},
		{
			name: "multi-line chain",
			body: "d := gpui.Div().\n\tID(\"root\").\n\tSizeFull()",
			want: "d := gpui.Div().Flex().\n\tID(\"root\").\n\tSizeFull()",
		},
		{
			name: "constructor passed to a helper",
			body: "d := wrap(gpui.Div().W4(), cx)",
			want: "d := wrap(gpui.Div().Flex().W4(), cx)",
		},
		{
			name: "constructor passed bare to a helper",
			body: "d := z.dragRoot(gpui.Div(), cx)",
			want: "d := z.dragRoot(gpui.Div().Flex(), cx)",
		},
		{
			name: "constructor in a composite literal",
			body: "d := box{inner: gpui.Div().ID(\"x\")}",
			want: "d := box{inner: gpui.Div().Flex().ID(\"x\")}",
		},
		{
			name: "returned from a helper",
			body: "return gpui.Div()",
			want: "return gpui.Div().Flex()",
		},
		{
			name: "flex direction vocabulary is not display",
			body: "d := gpui.Div().FlexCol().Gap(gpui.Px(4))",
			want: "d := gpui.Div().Flex().FlexCol().Gap(gpui.Px(4))",
		},
		{
			name: "grow and wrap vocabulary is not display",
			body: "d := gpui.Div().Flex1().FlexWrap().FlexBasisAuto()",
			want: "d := gpui.Div().Flex().Flex1().FlexWrap().FlexBasisAuto()",
		},
		{
			name: "IntoElement tail",
			body: "d := gpui.Div().W4().IntoElement()",
			want: "d := gpui.Div().Flex().W4().IntoElement()",
		},
		{
			name: "state styles do not pin the base display",
			body: "d := gpui.Div().W4().Hover(gpui.Styles().Bg(x))",
			want: "d := gpui.Div().Flex().W4().Hover(gpui.Styles().Bg(x))",
		},
		{
			name: "img relies on the default",
			body: "d := gpui.Img(source).W4()",
			want: "d := gpui.Img(source).Flex().W4()",
		},
		{
			name: "canvas relies on the default",
			body: "d := gpui.Canvas(p, q).SizeFull()",
			want: "d := gpui.Canvas(p, q).Flex().SizeFull()",
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

// TestClassNonDisplayStillPins covers Class strings without a display
// class and non-literal Class arguments: the chain still relies on the
// default, so the root is pinned (a later class application would
// overwrite the pin, keeping behaviour identical).
func TestClassNonDisplayStillPins(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "class without display token",
			body: `d := gpui.Div().Class("p-2 w-full")`,
			want: `d := gpui.Div().Flex().Class("p-2 w-full")`,
		},
		{
			name: "class with non-literal argument",
			body: "d := gpui.Div().Class(classes)",
			want: "d := gpui.Div().Flex().Class(classes)",
		},
		{
			name: "class with variant of a non-display class",
			body: `d := gpui.Div().Class("hover:bg-slate-700")`,
			want: `d := gpui.Div().Flex().Class("hover:bg-slate-700")`,
		},
		{
			// A variant sets display only in that state; the base
			// display still comes from the default.
			name: "class with a display class under a state variant",
			body: `d := gpui.Div().ID("x").Class("hover:hidden p-2")`,
			want: `d := gpui.Div().Flex().ID("x").Class("hover:hidden p-2")`,
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

// TestNestedRoots covers chains inside chains: the inner root's chain
// is analyzed independently of the outer one.
func TestNestedRoots(t *testing.T) {
	src := snippet("d := gpui.Div().W4().\n\tChild(gpui.Div().Block().P2())")
	want := snippet("d := gpui.Div().Flex().W4().\n\tChild(gpui.Div().Block().P2())")
	if got := assertStable(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestPackageGpuiUnqualified covers the gpui package's own files, where
// the constructors are unqualified.
func TestPackageGpuiUnqualified(t *testing.T) {
	src := "package gpui\n\nfunc f() *DivElement {\n\treturn Div().ID(\"x\")\n}\n"
	want := "package gpui\n\nfunc f() *DivElement {\n\treturn Div().Flex().ID(\"x\")\n}\n"
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
	d := g.Div().W4()
}
`
	want := strings.Replace(src, "g.Div().W4()", "g.Div().Flex().W4()", 1)
	if got := assertStable(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	src = `package main

import . "github.com/hoangvu12/gpui-go/gpui"

func f() {
	d := Div().W4()
}
`
	want = strings.Replace(src, "Div().W4()", "Div().Flex().W4()", 1)
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
	src := snippet("d := gpui.Div()\nd = d.W4()\nd.Block()")
	want := snippet("d := gpui.Div().Flex()\nd = d.W4()\nd.Block()")
	if got := assertStable(t, src); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

// TestCommentsPreserved covers comments interleaved with a rewritten
// chain: byte-offset insertion leaves them in place.
func TestCommentsPreserved(t *testing.T) {
	src := `package main

import "github.com/hoangvu12/gpui-go/gpui"

func f() {
	d := gpui.Div(). // the box
		W4(). // its size
		P2()
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
		return header + "package main\n\nimport \"github.com/hoangvu12/gpui-go/gpui\"\n\nfunc f() {\n\td := gpui.Div().W4()\n}\n"
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
				src = strings.Replace(src, "d := gpui.Div().W4()",
					"d := gpui.Div().W4() // Code generated; DO NOT EDIT.", 1)
			}
			got := rewriteOnce(t, src)
			want := strings.Replace(src, "gpui.Div().W4()", "gpui.Div().Flex().W4()", 1)
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
// mixes pinned, default-reliant and explicit chains.
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
		"e := gpui.Div().Flex().P2()",
		"g := gpui.Img(src).Flex()",
		"h := gpui.Img(src).Flex().SizeFull()",
		"i := gpui.Div().Flex().FlexCol()",
	} {
		if !strings.Contains(first, want) {
			t.Fatalf("missing %q in:\n%s", want, first)
		}
	}
}
