package base

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/format/html.rs tests (673-821): 6 of 6, and the
// U:text/format/html5minify/mod.rs tests (762-899): 2 of 2.

func htmlMustParse(t *testing.T, source string) *parsedDocument {
	t.Helper()
	doc, err := htmlParse(source, &nodeContext{})
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

// U:text/format/html.rs:685-708
func TestCleanupHTML(t *testing.T) {
	for _, c := range []struct{ html, want string }{
		{"<p>\n            and\n            <code>code</code>\n            text\n        </p>", "<p>and <code>code</code> text"},
		{"<p>\n            and\n            <em>   <code>code</code>   <i>italic</i>   </em>\n            text\n        </p>", "<p>and <em><code>code</code> <i>italic</i></em> text"},
	} {
		if got := string(htmlCleanup(c.html)); got != c.want {
			t.Errorf("cleanup = %q, want %q", got, c.want)
		}
	}
}

// U:text/format/html.rs:710-713
func TestTrimText(t *testing.T) {
	if got := htmlTrimText("  \n\tHello world \t\r "); got != " Hello world " {
		t.Fatalf("trim = %q", got)
	}
}

// U:text/format/html.rs:715-727
func TestHTMLMark(t *testing.T) {
	if got := htmlMustParse(t, `<p>Hello <mark>world</mark></p>`).toMarkdown(); got != "Hello ==world==" {
		t.Errorf("markdown = %q", got)
	}
	if got := htmlMustParse(t, `<p><mark color="blue">blue</mark> and <mark style="background-color: #336699">hex</mark></p>`).toMarkdown(); got != "==blue== and ==hex==" {
		t.Errorf("markdown = %q", got)
	}
}

// U:text/format/html.rs:729-766
func TestHTMLKeepSpaces(t *testing.T) {
	if got := htmlMustParse(t, `<p>and <code>code</code> text</p>`).toMarkdown(); got != "and `code` text" {
		t.Errorf("markdown = %q", got)
	}
	html := `
            <div>
            <p>
                and
                <em>   <code>code</code>   <i>italic</i>   </em>
                text
            </p>
            <p>
                <img src="https://example.com/image.png" alt="Example" width="100" height="200" title="Example Image" />
            </p>
            <ul>
                <li>Item 1</li>
                <li>Item 2
                </li>
            </ul>
            </div>
        `
	want := "and *code italic* text\n\n![Example](https://example.com/image.png \"Example Image\")\n\n- Item 1\n- Item 2"
	if got := htmlMustParse(t, html).toMarkdown(); got != want {
		t.Errorf("markdown = %q\nwant %q", got, want)
	}
}

// U:text/format/html.rs:768-774
func TestValueToLength(t *testing.T) {
	for _, c := range []struct {
		value string
		want  gpui.DefiniteLength
	}{
		{"100px", gpui.DefinitePx(100)},
		{"100%", gpui.Fraction(1)},
		{"56%", gpui.Fraction(0.56)},
		{"240", gpui.DefinitePx(240)},
	} {
		if got, ok := htmlValueToLength(c.value); !ok || *got != c.want {
			t.Errorf("%q = %v %v, want %v", c.value, got, ok, c.want)
		}
	}
}

// U:text/format/html.rs:776-820
func TestHTMLImage(t *testing.T) {
	str := func(s string) *string { return &s }
	length := func(l gpui.DefiniteLength) *gpui.DefiniteLength { return &l }
	for _, c := range []struct {
		html  string
		image ImageNode
	}{
		{
			`<img src="https://example.com/image.png" alt="Example" width="100" height="200" title="Example Image" />`,
			ImageNode{URL: "https://example.com/image.png", Alt: str("Example"), Width: length(gpui.DefinitePx(100)), Height: length(gpui.DefinitePx(200)), Title: str("Example Image")},
		},
		{
			`<img src="https://example.com/image.png" alt="Example" style="width: 80%" title="Example Image" />`,
			ImageNode{URL: "https://example.com/image.png", Alt: str("Example"), Width: length(gpui.Fraction(0.8)), Title: str("Example Image")},
		},
	} {
		got := htmlMustParse(t, c.html)
		p := defaultParagraph()
		p.children = []inlineNode{inlineNodeImage(c.image)}
		want := &parsedDocument{source: c.html, blocks: []blockNode{p}}
		if !got.equal(want) {
			t.Errorf("%s:\n got %#v\nwant %#v", c.html, got.blocks, want.blocks)
		}
	}
}

// U:text/format/html5minify/mod.rs:767-794
func TestWriteCollapseWhitespace(t *testing.T) {
	for _, c := range []struct {
		input, want string
		preceding   bool
	}{
		{"", "", false},
		{"  ", " ", false},
		{"   ", " ", false},
		{"   ", "", true},
		{" x      y  ", " x y ", false},
		{" x      y  ", "x y ", true},
		{" x   \n  \t \n   y  ", " x y ", false},
		{" x   \n  \t \n   y  ", "x y ", true},
	} {
		var w strings.Builder
		m := newHTML5Minifier(&w)
		m.precedingWhitespace = c.preceding
		preceding := c.preceding
		if err := m.writeCollapseWhitespace([]byte(c.input), reservedEntity, &preceding); err != nil {
			t.Fatal(err)
		}
		if w.String() != c.want {
			t.Errorf("%q (preceding %v) = %q, want %q", c.input, c.preceding, w.String(), c.want)
		}
	}
}

// U:text/format/html5minify/mod.rs:796-898
func TestOmitTags(t *testing.T) {
	for _, c := range []struct {
		input, want                          string
		collapseWhitespace, preserveComments bool
	}{
		// <html>
		{"<html>", "", true, false},
		// Comments ignored
		{"<html><!-- -->", "", true, false},
		// Comments preserved
		{"<html>     <!-- -->    ", "<html><!-- -->", true, true},
		{"<html><!-- --></html>", "<html><!-- -->", true, true},
		{"<html><!-- --></html><!-- -->", "<html><!-- --></html><!-- -->", true, true},
		{"<html>    <!-- -->    </html>    <!-- -->    ", "<html><!-- --></html><!-- -->", true, true},
		// <body> is implicitly added to the DOM
		{"<html>    <!-- -->    </html>    <!-- -->    ", "<html><!-- --><body>        </html><!-- -->", false, true},
		// <head>
		{"<html>   <head>   <title>A</title>     </head>   <body><p>     B  </p> </body>", "<title>A</title><p>B", true, false},
		{"<html>   <head>   <title>A</title>     </head>   <body><p>     B  </p> </body>", "<head>   <title>A</title>     </head>   <p>     B   ", false, false},
		{"<html>   <head><!-- -->   <title>A</title>     </head>   <body><p>     B  </p> </body>", "<head><!-- --><title>A</title><p>B", true, true},
		// <body>
		{"<body>", "", true, false},
		{"<body>    <script>let x = 1;</script>   ", "<body><script>let x = 1;</script>", true, false},
		{"<body>        <style>body{margin:1em}</style>", "<body><style>body{margin:1em}</style>", true, false},
		{"<body>    <p>A", "<p>A", true, false},
		{"<body id=main>    <p>A", "<body id=main><p>A", true, false},
		// Retain whitespace, whitespace before <p>
		{"    <body>    <p>A      ", "<body>    <p>A      ", false, false},
		// Retain whitespace, touching <p>
		{"<body><p>A</body>", "<p>A", false, false},
		// Comments ignored
		{"<body><p>A</body><!-- -->", "<p>A", false, false},
		// Comments preserved
		{"<body><p>A</body><!-- -->", "<body><p>A</body><!-- -->", false, true},
		// Retain end tag if touching inline element
		{"<p>Some text</p><button></button>", "<p>Some text</p><button></button>", false, false},
	} {
		got, err := minifyHTML(c.input, func(m *html5Minifier) {
			m.setOmitDoctype(true).setCollapseWhitespace(c.collapseWhitespace).setPreserveComments(c.preserveComments)
		})
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != c.want {
			t.Errorf("%q (collapse %v, comments %v) = %q, want %q", c.input, c.collapseWhitespace, c.preserveComments, got, c.want)
		}
	}
}
