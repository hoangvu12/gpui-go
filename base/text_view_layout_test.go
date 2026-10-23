package base

import (
	"cmp"
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Go-own layout tests for ticket 41: table rows stack (gap 2b), keyless
// HTML flow paragraphs keep their own element state, and a scrollable
// view's item fills the view's width.

const (
	layoutTestBorder = 0x13579b
	layoutTestCode   = 0x2468ac
)

// drawnQuads draws state's view width wide and returns the frame's quads
// in logical pixels, each distinct one once.
func drawnQuads(t *testing.T, ta *gpui.TestApp, state gpui.Entity[TextViewState], width float32, scrollable bool, table gpui.StyleRefinement) []gpui.Quad {
	t.Helper()
	code := gpui.Rgb(layoutTestCode)
	style := DefaultTextViewStyle().WithBorder(gpui.Rgb(layoutTestBorder)).
		WithCodeBackground(code).WithInlineCode(gpui.HighlightStyle{BackgroundColor: &code}).WithTable(table)
	var scale float32
	_, cx := openTestView(t, ta, func(w *gpui.Window, _ *gpui.Context[testView]) gpui.AnyElement {
		scale = gpui.ScaleFactorOf(w)
		view := NewTextView(state).Style(style)
		if scrollable {
			view = view.SizeFull().Scrollable(true)
		}
		return gpui.Div().Block().W(gpui.Px(width)).H(gpui.Px(400)).Child(view).IntoElement()
	})
	drawText(t, cx)
	drawText(t, cx)
	quads, err := gpui.LastDrawnScene(cx.Window()).Quads()
	if err != nil {
		t.Fatal(err)
	}
	// Each frame's scene is read back as drawn, in paint order; a quad
	// drawn by more than one pass appears once here.
	var out []gpui.Quad
	type quadKey struct {
		bounds     gpui.Bounds
		bg, border gpui.Hsla
		widths     gpui.Edges
	}
	seen := map[quadKey]bool{}
	for _, q := range quads {
		b := q.Bounds
		q.Bounds = gpui.Bounds{Origin: gpui.Point{X: b.Origin.X / scale, Y: b.Origin.Y / scale},
			Size: gpui.Size{Width: b.Size.Width / scale, Height: b.Size.Height / scale}}
		k := quadKey{q.Bounds, q.Background.Solid, q.BorderColor.Solid, q.BorderWidths}
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, q)
	}
	return out
}

func newTextViewStateHTMLEntity(ta *gpui.TestApp, text string) gpui.Entity[TextViewState] {
	var state gpui.Entity[TextViewState]
	ta.Update(func(app *gpui.App) {
		state = gpui.NewEntity(app, ta.RootScope(), func(s *TextViewState, cx *gpui.Context[TextViewState]) {
			*s = TextViewStateHTML(text, cx)
		})
	})
	return state
}

// tableRowQuads are the row boxes: the quads bordered at the bottom in
// the table border color (cells are bordered on the right only).
func tableRowQuads(quads []gpui.Quad) []gpui.Bounds {
	var rows []gpui.Bounds
	for _, q := range quads {
		if q.BorderColor.Solid == gpui.Rgb(layoutTestBorder) && q.BorderWidths.Bottom > 0 &&
			q.BorderWidths.Top == 0 && q.BorderWidths.Right == 0 {
			rows = append(rows, q.Bounds)
		}
	}
	slices.SortFunc(rows, func(a, b gpui.Bounds) int { return cmp.Compare(a.Origin.Y, b.Origin.Y) })
	return rows
}

// Ticket 41 bug 1: every table row rendered on one line. The table frame
// and the scroll layout's row track are plain divs upstream
// (U:text/node.rs:3180, 3269-3276), Block boxes that stack the rows. The
// HTML table sits in a Root block (U:text/format/html.rs parses a document
// into nested roots); in a scrollable view that root is a list item, which
// has to fill the list's width for the table to.
func TestTextViewTableRowsStack(t *testing.T) {
	const (
		markdown = "| Component | Group |\n|---|---|\n| TextView | Text |\n| Plot | Charts |\n| Dock | Layout |\n"
		html     = "<h2>Tags</h2><p>Short.</p><table><thead><tr><th>Tag</th><th>Supported</th></tr></thead><tbody>" +
			"<tr><td>table</td><td>yes</td></tr><tr><td>pre</td><td>yes</td></tr><tr><td>p</td><td>yes</td></tr></tbody></table>"
		width float32 = 400
	)
	var scroll gpui.StyleRefinement
	scroll.OverflowX = gpui.Some(gpui.OverflowScroll)
	for _, c := range []struct {
		name       string
		html       bool
		scrollable bool
		table      gpui.StyleRefinement
	}{
		{"markdown", false, false, gpui.StyleRefinement{}},
		{"markdown scroll table", false, false, scroll},
		{"markdown scrollable view", false, true, gpui.StyleRefinement{}},
		{"html", true, false, gpui.StyleRefinement{}},
		{"html scroll table", true, false, scroll},
		{"html scrollable view", true, true, gpui.StyleRefinement{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			ta := textTestApp(t)
			var state gpui.Entity[TextViewState]
			if c.html {
				state = newTextViewStateHTMLEntity(ta, html)
			} else {
				state = newTextViewStateEntity(ta, markdown)
			}
			rows := tableRowQuads(drawnQuads(t, ta, state, width, c.scrollable, c.table))
			// Four rows; the last has no bottom border.
			if len(rows) != 3 {
				t.Fatalf("%d bordered rows, want 3: %v", len(rows), rows)
			}
			for i, r := range rows {
				if r.Origin.X != rows[0].Origin.X {
					t.Errorf("row %d at x %v, want %v (rows side by side)", i, r.Origin.X, rows[0].Origin.X)
				}
				if i > 0 && r.Origin.Y < rows[i-1].Origin.Y+rows[i-1].Size.Height {
					t.Errorf("row %d at y %v overlaps row %d ending at %v", i, r.Origin.Y, i-1, rows[i-1].Origin.Y+rows[i-1].Size.Height)
				}
				// The frame's border_1 is inside the view's width.
				if r.Size.Width < width-2 || r.Size.Width > width {
					t.Errorf("row %d is %v wide, want the view's %v less the frame border", i, r.Size.Width, width)
				}
			}
		})
	}
}

// Ticket 41 bug 2: an HTML paragraph with inline code drew as an empty
// code-colored bar. HTML nodes carry no span (U:text/format/html.rs), so
// every flow paragraph was named "p" (U:text/node.rs:2505-2507) and shared
// one InlineFlow element state: the paragraph measured and painted the
// <pre> paragraph's layout. Each paragraph now paints its own code span.
func TestTextViewHTMLFlowParagraphsKeepTheirOwnLayout(t *testing.T) {
	const html = "<h2>HTML rendering</h2>\n" +
		"<p>TextView also parses <strong>HTML</strong>: <em>emphasis</em>, <code>x</code>,\n" +
		"and <a href=\"https://example.com\">links</a>.</p>\n" +
		"<ul><li>Unordered item</li></ul>\n" +
		"<pre><code>SELECT name FROM components WHERE unstyled = true;</code></pre>"
	for _, scrollable := range []bool{false, true} {
		ta := textTestApp(t)
		quads := drawnQuads(t, ta, newTextViewStateHTMLEntity(ta, html), 600, scrollable, gpui.StyleRefinement{})
		var code []gpui.Bounds
		for _, q := range quads {
			if q.Background.Solid == gpui.Rgb(layoutTestCode) {
				code = append(code, q.Bounds)
			}
		}
		slices.SortFunc(code, func(a, b gpui.Bounds) int { return cmp.Compare(a.Origin.Y, b.Origin.Y) })
		if len(code) != 2 {
			t.Fatalf("scrollable %v: %d code backgrounds, want 2: %v", scrollable, len(code), code)
		}
		short, long := code[0], code[1]
		if short.Origin.Y >= long.Origin.Y {
			t.Errorf("scrollable %v: paragraph code at y %v, not above the <pre> at %v", scrollable, short.Origin.Y, long.Origin.Y)
		}
		// "x" against the <pre>'s 49 characters.
		if short.Size.Width*10 > long.Size.Width {
			t.Errorf("scrollable %v: paragraph code is %v wide, the <pre> %v: the paragraph drew the <pre>'s layout",
				scrollable, short.Size.Width, long.Size.Width)
		}
	}
}

// A keyless leaf gets its own id, numbered per top-level block; a keyed
// leaf keeps upstream's id.
func TestLeafElementIDNumbersKeylessLeaves(t *testing.T) {
	cx := defaultNodeRenderContext()
	cx.startBlock(2)
	a, b := cx.leafElementID(nil), cx.leafElementID(nil)
	if a == b {
		t.Fatalf("keyless leaves share id %v", a)
	}
	cx.startBlock(2)
	if again := cx.leafElementID(nil); again != a {
		t.Errorf("id not stable across renders: %v, then %v", a, again)
	}
	cx.startBlock(3)
	if other := cx.leafElementID(nil); other == a {
		t.Errorf("blocks 2 and 3 share id %v", a)
	}
	key := textLeafBlock(7)
	if got := cx.leafElementID(key); got != textLeafElementID(key) {
		t.Errorf("keyed leaf id %v, want %v", got, textLeafElementID(key))
	}
}

// A scrollable view's code block background spans the view: the list item
// root fills the list's width as upstream's Block roots do (gap 2b).
func TestTextViewScrollableCodeBlockFillsWidth(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "```\nfn main() {}\n```\n\nAfter the block.\n")
	quads := drawnQuads(t, ta, state, 500, true, gpui.StyleRefinement{})
	for _, q := range quads {
		if q.Background.Solid == gpui.Rgb(layoutTestCode) {
			if q.Bounds.Size.Width != 500 {
				t.Fatalf("code block background is %v wide, want 500", q.Bounds.Size.Width)
			}
			return
		}
	}
	t.Fatal("no code block background")
}
