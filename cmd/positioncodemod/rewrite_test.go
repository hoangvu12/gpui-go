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
		panic("positioncodemod test: bad expected source: " + err.Error())
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
		panic("positioncodemod test: bad snippet: " + err.Error())
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

// rewriteOnceAt runs the codemod with a file path (the fixture
// exemption is path-based).
func rewriteOnceAt(t *testing.T, path, src string) *FileRewrite {
	t.Helper()
	res, err := RewriteSource(path, []byte(src))
	if err != nil {
		t.Fatalf("RewriteSource %s: %v", path, err)
	}
	return res
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

// TestParentOfAbsoluteChildPinsRelative covers the parent rule
// (browser-core 76, spec D3): an element whose chain attaches an
// absolute-styled child without stating its own position was the
// containing block under the pin's Relative default (Taffy places
// every absolute child against its direct parent); under the static
// default the child escapes to the nearest positioned ancestor, so
// the codemod pins .Relative() immediately after the constructor —
// restoring the pre-76 layout — and a second run changes nothing.
func TestParentOfAbsoluteChildPinsRelative(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "child with absolute selector",
			body: "d := gpui.Div().Flex().Child(gpui.Div().Absolute().Inset0())",
			want: "d := gpui.Div().Relative().Flex().Child(gpui.Div().Absolute().Inset0())",
		},
		{
			name: "children with one absolute argument",
			body: "d := gpui.Div().Children(gpui.Div().W4(), gpui.Div().Absolute().Top0())",
			want: "d := gpui.Div().Relative().Children(gpui.Div().W4(), gpui.Div().Absolute().Top0())",
		},
		{
			name: "absolute argument built by a component constructor",
			body: "d := gpui.Div().FlexCol().Child(base.NewBadge().Absolute().Right(gpui.Px(4)))",
			want: "d := gpui.Div().Relative().FlexCol().Child(base.NewBadge().Absolute().Right(gpui.Px(4)))",
		},
		{
			name: "absolute argument through a class string",
			body: "d := gpui.Div().P2().Child(gpui.Div().Class(\"absolute inset-0\"))",
			want: "d := gpui.Div().Relative().P2().Child(gpui.Div().Class(\"absolute inset-0\"))",
		},
		{
			name: "chain continues past the child call",
			body: "d := gpui.Div().Flex().Child(gpui.Div().Absolute().Size0()).Bg(gpui.Black())",
			want: "d := gpui.Div().Relative().Flex().Child(gpui.Div().Absolute().Size0()).Bg(gpui.Black())",
		},
		{
			name: "try children",
			body: "d := gpui.Div().P1().TryChildren(gpui.Div().Absolute().Left(gpui.Px(2)))",
			want: "d := gpui.Div().Relative().P1().TryChildren(gpui.Div().Absolute().Left(gpui.Px(2)))",
		},
		{
			name: "variant-prefixed absolute class on the child",
			body: "d := gpui.Div().P2().Child(gpui.Div().Class(\"hover:absolute\"))",
			want: "d := gpui.Div().Relative().P2().Child(gpui.Div().Class(\"hover:absolute\"))",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := snippet(tc.body)
			want := snippet(tc.want)
			got := assertStable(t, src)
			if got != want {
				t.Fatalf("rewrite mismatch:\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TestInsetVocabularyPinsRelative covers the inset rule: an element
// whose chain sets insets without stating position got its offsets
// from the Relative default; under the static default the insets are
// inert (CSS static boxes ignore them; the record translation zeroes
// them), so the codemod pins .Relative().
func TestInsetVocabularyPinsRelative(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "top and left methods",
			body: "d := gpui.Div().Top(gpui.Px(4)).Left(gpui.Px(2))",
			want: "d := gpui.Div().Relative().Top(gpui.Px(4)).Left(gpui.Px(2))",
		},
		{
			name: "inset method",
			body: "d := gpui.Div().Inset(gpui.Px(4))",
			want: "d := gpui.Div().Relative().Inset(gpui.Px(4))",
		},
		{
			name: "inset class string",
			body: "d := gpui.Div().Class(\"top-2 left-[12px]\")",
			want: "d := gpui.Div().Relative().Class(\"top-2 left-[12px]\")",
		},
		{
			name: "variant-prefixed inset class",
			body: "d := gpui.Div().Class(\"hover:top-2\")",
			want: "d := gpui.Div().Relative().Class(\"hover:top-2\")",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := snippet(tc.body)
			want := snippet(tc.want)
			got := assertStable(t, src)
			if got != want {
				t.Fatalf("rewrite mismatch:\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TestExplicitPositionIsNeverTouched covers the explicit forms the
// task names: position selector methods, and Class strings carrying
// relative/absolute/fixed/sticky/static without a variant prefix. A
// chain that states its position — however it states it — is left
// alone even when it attaches absolute children or sets insets.
func TestExplicitPositionIsNeverTouched(t *testing.T) {
	bodies := []string{
		// Position selectors.
		"d := gpui.Div().Relative().Child(gpui.Div().Absolute().Inset0())",
		"d := gpui.Div().Static().Top(gpui.Px(4))",
		"d := gpui.Div().Absolute().Top0().Left0()",
		"d := gpui.Div().Sticky().Top(gpui.Px(4))",
		"d := gpui.Div().Fixed().Inset0()",
		// Class strings without a variant prefix.
		"d := gpui.Div().Class(\"relative p-2\").Child(gpui.Div().Class(\"absolute inset-0\"))",
		"d := gpui.Div().Class(\"static\").Top(gpui.Px(2))",
		"d := gpui.Div().Class(\"fixed inset-0\")",
		"d := gpui.Div().Class(\"sticky top-2\")",
		// The position may be stated after the child attach.
		"d := gpui.Div().Child(gpui.Div().Absolute().Size0()).Relative()",
		// Non-element "Absolute" links on an argument's chain can only
		// be a length conversion outside Child positions; inside a
		// Child argument the chain link is the position selector, and
		// a child that is static instead is not absolute.
		"d := gpui.Div().Child(gpui.Div().Static().Top(gpui.Px(1)))",
		// A plain chain uses the static default deliberately.
		"d := gpui.Div().Flex().P2().Child(gpui.Div().W4())",
		"d := gpui.Div().FlexCol()",
	}
	for _, body := range bodies {
		src := snippet(body)
		got := assertStable(t, src)
		if got != src {
			t.Fatalf("explicit chain was touched:\nsrc:\n%s\ngot:\n%s", src, got)
		}
	}
}

// TestVariantPositionClassIsNotExplicit pins the task's rule: a class
// string whose position class sits under a state variant
// (hover:relative) sets the position only in that state, so the base
// position still relies on the default — the pin applies.
func TestVariantPositionClassIsNotExplicit(t *testing.T) {
	body := "d := gpui.Div().Class(\"hover:relative\").Top(gpui.Px(2))"
	src := snippet(body)
	want := snippet("d := gpui.Div().Relative().Class(\"hover:relative\").Top(gpui.Px(2))")
	got := assertStable(t, src)
	if got != want {
		t.Fatalf("rewrite mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestNonRootReceiversAreNotTouched covers the limitation boundary:
// only Div/Img/Canvas construction chains are rewritten. A parent
// built as a variable, a helper-returned element or another element
// kind (Anchored) is left alone; its own construction site is analyzed
// when it chains a constructor.
func TestNonRootReceiversAreNotTouched(t *testing.T) {
	bodies := []string{
		// A variable-held div: the receiver is an identifier.
		"d := gpui.Div().Flex()\nd.Child(gpui.Div().Absolute().Inset0())",
		// Another element kind's child method.
		"d := gpui.Anchored().Child(gpui.Div().Absolute().Inset0())",
		// A child spread from a slice variable.
		"d := gpui.Div().Flex().Children(items...)",
		// A helper call whose result is styled absolute at its own
		// construction site (that site is the analyzed one).
		"d := gpui.Div().Flex().Child(badge())",
	}
	for _, body := range bodies {
		src := snippet(body)
		got := assertStable(t, src)
		if got != src {
			t.Fatalf("non-root receiver was touched:\nsrc:\n%s\ngot:\n%s", src, got)
		}
	}
}

// TestFixtureViewsExemptFromTheParentRule covers the parity fixture
// exemption: files under internal/parity/fixtures mirror checked-in
// Chrome pages, and an unpositioned parent of an absolute child is
// exactly the page's shape — the static default is the point there.
// The inset rule still applies (a fixture view with inert insets is a
// view bug).
func TestFixtureViewsExemptFromTheParentRule(t *testing.T) {
	body := "d := gpui.Div().Flex().Child(gpui.Div().Absolute().Inset0())"
	src := snippet(body)

	res := rewriteOnceAt(t, `C:/repo/internal/parity/fixtures/positiondefault/positiondefault.go`, src)
	if res.Changed {
		t.Fatalf("the parent rule pinned a fixture view:\n%s", string(res.Final))
	}

	res = rewriteOnceAt(t, `C:/repo/internal/parity/fixtures/positiondefault/positiondefault.go`, snippet("d := gpui.Div().Top(gpui.Px(2))"))
	if !res.Changed {
		t.Fatal("the inset rule skipped a fixture view")
	}
}

// TestMultipleRootsAndOffsets pins the edit mechanics: several roots
// in one file, insertions at the root constructor's end (byte offsets
// ascending, applied back-to-front), comments and line breaks
// preserved, output gofmt-formatted.
func TestMultipleRootsAndOffsets(t *testing.T) {
	body := `d1 := gpui.Div().
		Flex().
		// A comment that must survive.
		Child(gpui.Div().Absolute().Inset0())
	d2 := gpui.Div().Top(gpui.Px(4))
	d3 := gpui.Div().P2()`
	want := `d1 := gpui.Div().Relative().
		Flex().
		// A comment that must survive.
		Child(gpui.Div().Absolute().Inset0())
	d2 := gpui.Div().Relative().Top(gpui.Px(4))
	d3 := gpui.Div().P2()`
	src := snippet(body)
	expected := snippet(want)
	got := assertStable(t, src)
	if got != expected {
		t.Fatalf("rewrite mismatch:\ngot:\n%s\nwant:\n%s", got, expected)
	}
}

// TestImportFormsResolveTheGPUIPackage covers the package resolution:
// the default alias, a renamed import, a dot-import and the gpui
// package's own files (bare constructor names).
func TestImportFormsResolveTheGPUIPackage(t *testing.T) {
	dotPrologue := "package main\n\nimport . \"github.com/hoangvu12/gpui-go/gpui\"\n\nfunc f() {\n"
	renamedPrologue := "package main\n\nimport ui \"github.com/hoangvu12/gpui-go/gpui\"\n\nfunc f() {\n"
	gpuiPrologue := "package gpui\n\nfunc f() {\n"
	unrelatedPrologue := "package main\n\nfunc f() {\n"

	cases := []struct {
		name     string
		prologue string
		body     string
		changed  bool
	}{
		{"default alias", prologue, "d := gpui.Div().Child(gpui.Div().Absolute().Size0())", true},
		{"renamed import", renamedPrologue, "d := ui.Div().Child(ui.Div().Absolute().Size0())", true},
		{"dot import", dotPrologue, "d := Div().Child(Div().Absolute().Size0())", true},
		{"inside package gpui", gpuiPrologue, "d := Div().Child(Div().Absolute().Size0())", true},
		{"no gpui import", unrelatedPrologue, "d := Div().Child(Div().Absolute().Size0())", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := gofmtSrc(tc.prologue + tc.body + epilogue)
			res, err := RewriteSource("snippet.go", []byte(src))
			if err != nil {
				t.Fatalf("RewriteSource: %v", err)
			}
			if res.Changed != tc.changed {
				t.Fatalf("changed = %v, want %v (source:\n%s)", res.Changed, tc.changed, src)
			}
		})
	}
}

// TestGeneratedFilesAreSkipped pins the generated-code rule: a file
// carrying the "Code generated ... DO NOT EDIT." header is never
// rewritten.
func TestGeneratedFilesAreSkipped(t *testing.T) {
	src := gofmtSrc("// Code generated by tailwindgen. DO NOT EDIT.\n\n" + prologue + "d := gpui.Div().Child(gpui.Div().Absolute().Size0())" + epilogue)
	res, err := RewriteSource("generated.go", []byte(src))
	if err != nil {
		t.Fatalf("RewriteSource: %v", err)
	}
	if res.Changed {
		t.Fatal("a generated file was rewritten")
	}
}

// TestParseErrorIsReported pins the error contract: a file that does
// not parse is reported, never rewritten.
func TestParseErrorIsReported(t *testing.T) {
	if _, err := RewriteSource("broken.go", []byte("package main\nfunc f() {")); err == nil {
		t.Fatal("a parse error was not reported")
	}
}

// TestCounts pins the analysis counters reported per file (the numbers
// the -v flag and the ticket's counts come from).
func TestCounts(t *testing.T) {
	body := `d1 := gpui.Div().Relative().Child(gpui.Div().Absolute().Size0())
	d2 := gpui.Div().Child(gpui.Div().Absolute().Size0())
	d3 := gpui.Div().Top(gpui.Px(2))
	d4 := gpui.Div().Flex().P2()`
	res, err := RewriteSource("snippet.go", []byte(snippet(body)))
	if err != nil {
		t.Fatalf("RewriteSource: %v", err)
	}
	if res.Roots != 6 {
		t.Fatalf("roots = %d, want 6 (the four statements plus the two constructed children)", res.Roots)
	}
	if res.Explicit != 3 {
		t.Fatalf("explicit = %d, want 3 (the relative chain and the two absolute children)", res.Explicit)
	}
	if res.Defaults != 1 {
		t.Fatalf("defaults = %d, want 1 (the plain flex chain)", res.Defaults)
	}
	if len(res.Edits) != 2 {
		t.Fatalf("edits = %d, want 2 (the absolute parent and the inset user)", len(res.Edits))
	}
}

// TestChildArgumentChainOrderPinsLastSelector pins the refinement
// order on a child's own chain: a child styled both Static and
// Absolute (the later link wins) is classified by the outer link.
func TestChildArgumentChainOrderPinsLastSelector(t *testing.T) {
	// Absolute applied later: the child is absolute, the parent pins.
	src := snippet("d := gpui.Div().Flex().Child(gpui.Div().Static().Absolute())")
	want := snippet("d := gpui.Div().Relative().Flex().Child(gpui.Div().Static().Absolute())")
	if got := assertStable(t, src); got != want {
		t.Fatalf("rewrite mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
	// Static applied later: the child is static, the parent is left
	// alone (the child is not absolute).
	src = snippet("d := gpui.Div().Flex().Child(gpui.Div().Absolute().Static())")
	if got := assertStable(t, src); got != src {
		t.Fatalf("a static child pinned its parent:\n%s", got)
	}
}

// TestLengthAbsoluteIsNotAMethodChainLink guards the false positive
// displaycodemod-style analysis could produce: DefiniteLength has its
// own Absolute() method, but it can never appear as a Child argument
// (the argument is element-typed), so an "Absolute" link found inside
// a Child call's argument list is the position selector. The guard
// here documents the shape: a length conversion outside Child is not
// analyzed at all.
func TestLengthAbsoluteIsNotAMethodChainLink(t *testing.T) {
	src := snippet("v := d.Absolute().ToPixels(gpui.RemSize(w))")
	res, err := RewriteSource("snippet.go", []byte(src))
	if err != nil {
		t.Fatalf("RewriteSource: %v", err)
	}
	if res.Changed || res.Roots != 0 {
		t.Fatalf("a non-element chain was analyzed: changed=%v roots=%d", res.Changed, res.Roots)
	}
	if !strings.Contains(string(res.Final), "ToPixels") && res.Changed {
		t.Fatal("unexpected rewrite")
	}
}
