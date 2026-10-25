package gpui

// Ticket 38 (browser-core): the white-space collapse (gpui/
// white_space.go) over every mode, the caret byte map's offset
// conversions (Chromium's OffsetMapping semantics), the caret geometry
// through StyledText under a collapse, and the tab-size stops under
// pre. The collapser table tests every rule the fixtures record end to
// end; the Chromium file:line citations live in white_space.go's
// header.

import (
	"fmt"
	"strings"
	"testing"
)

// collapseCase is one (mode, source) → display expectation.
type collapseCase struct {
	name   string
	mode   WhiteSpace
	source string
	want   string
}

// TestWhiteSpaceCollapseTable pins the collapse of every mode: the
// space-run rules (one space, removed leading/trailing/paragraph-start
// runs), the pre-line forced breaks, and the preserve modes'
// carriage-return dropping.
func TestWhiteSpaceCollapseTable(t *testing.T) {
	cases := []collapseCase{
		// normal / nowrap: collapse (inline_items_builder.cc
		// AppendCollapseWhitespace).
		{"normal one space stays", WhiteSpaceNormal, "a b", "a b"},
		{"normal run collapses", WhiteSpaceNormal, "a  b", "a b"},
		{"normal tabs collapse", WhiteSpaceNormal, "a\t\t\tb", "a b"},
		{"normal newline collapses", WhiteSpaceNormal, "a\nb", "a b"},
		{"normal newlines collapse", WhiteSpaceNormal, "a\n\nb", "a b"},
		{"normal mixed run collapses", WhiteSpaceNormal, "a \n \n b", "a b"},
		{"normal leading run removed", WhiteSpaceNormal, "   a", "a"},
		{"normal leading newlines removed", WhiteSpaceNormal, "\n\n  a", "a"},
		{"normal trailing run removed", WhiteSpaceNormal, "a   ", "a"},
		{"normal trailing newline removed", WhiteSpaceNormal, "a \n ", "a"},
		{"normal all-space paragraph empties", WhiteSpaceNormal, " \n\t ", ""},
		{"normal CR collapses", WhiteSpaceNormal, "a\rb", "a b"},
		{"normal form feed is a character", WhiteSpaceNormal, "a\x0cb", "a\x0cb"},
		{"nowrap collapses like normal", WhiteSpaceNowrap, "a \n\n  b", "a b"},
		{"nowrap trailing run removed", WhiteSpaceNowrap, "a\t", "a"},
		// pre-line: spaces collapse, newlines break
		// (inline_items_builder.cc AppendPreserveNewline).
		{"pre-line keeps the newline", WhiteSpacePreLine, "a\nb", "a\nb"},
		{"pre-line collapses spaces", WhiteSpacePreLine, "a  b", "a b"},
		{"pre-line folds a mixed run before the break", WhiteSpacePreLine, "a \n b", "a\nb"},
		{"pre-line removes the trailing spaces before the break", WhiteSpacePreLine, "a  \nb", "a\nb"},
		{"pre-line removes the leading spaces after the break", WhiteSpacePreLine, "a\n  b", "a\nb"},
		{"pre-line keeps an empty middle line", WhiteSpacePreLine, "a\n\nb", "a\n\nb"},
		{"pre-line CR before the newline collapses", WhiteSpacePreLine, "a\r\nb", "a\nb"},
		{"pre-line trailing run removed", WhiteSpacePreLine, "a \n", "a\n"},
		{"pre-line leading run removed", WhiteSpacePreLine, "  a\nb", "a\nb"},
		// pre / pre-wrap: preserve (AppendPreserveWhitespace).
		{"pre keeps everything", WhiteSpacePre, "a  \n\t b", "a  \n\t b"},
		{"pre drops CR", WhiteSpacePre, "a\r\nb", "a\nb"},
		{"pre drops FF", WhiteSpacePre, "a\x0cb", "ab"},
		{"pre keeps trailing spaces", WhiteSpacePre, "a  ", "a  "},
		{"pre keeps leading spaces", WhiteSpacePre, "  a", "  a"},
		{"pre-wrap keeps everything", WhiteSpacePreWrap, "a \n\t b", "a \n\t b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			display, m := collapseWhiteSpace(c.source, c.mode)
			if display != c.want {
				t.Errorf("collapseWhiteSpace(%q, %v) = %q, want %q", c.source, c.mode, display, c.want)
			}
			if m != nil {
				if got := m.domToText(len(c.source)); got != len(display) {
					t.Errorf("domToText(len(source)) = %d, want len(display) %d", got, len(display))
				}
				if got := m.textToDomLast(len(display)); got != len(c.source) {
					t.Errorf("textToDomLast(len(display)) = %d, want len(source) %d", got, len(c.source))
				}
			}
		})
	}
}

// TestWhiteSpaceCollapseAcrossSegments pins the cross-record rules: a
// space run spanning two records collapses to the FIRST record's kept
// space, a preserve record joins verbatim, and an atomic box breaks the
// collapse chain: the box is the builder's last item to collapse with
// and ends kNotCollapsible (AppendAtomicInline appends it through
// Append, not AppendOpaque — inline_items_builder.cc:218-228 and
// 1281-1289), so a sub-run starting at the box keeps its one space and
// a space before a trailing box survives the final removal. The three
// box cases were re-measured in Chrome 155 (browser-core 78: the
// original table folded the space after a box into the space before it,
// which Chrome does not do — `a <box> b` renders one space on each
// side).
func TestWhiteSpaceCollapseAcrossSegments(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		segments []whiteSpaceSegment
		boxes    []int
		want     string
	}{
		{
			name:   "two records' spaces collapse to one",
			source: "a   b",
			segments: []whiteSpaceSegment{
				{start: 0, end: 3, mode: WhiteSpaceNormal}, // "a  "
				{start: 3, end: 5, mode: WhiteSpaceNormal}, // " b"
			},
			want: "a b",
		},
		{
			name:   "three records' spaces collapse to one",
			source: "a   b",
			segments: []whiteSpaceSegment{
				{start: 0, end: 2, mode: WhiteSpaceNormal},
				{start: 2, end: 4, mode: WhiteSpaceNormal},
				{start: 4, end: 5, mode: WhiteSpaceNormal},
			},
			want: "a b",
		},
		{
			name:   "a normal record's trailing space survives into a pre record",
			source: "a   b",
			segments: []whiteSpaceSegment{
				{start: 0, end: 3, mode: WhiteSpaceNormal},  // "a  " → "a "
				{start: 3, end: 5, mode: WhiteSpacePreWrap}, // " b" verbatim
			},
			want: "a  b",
		},
		{
			name:   "a pre record's spaces survive into a normal record",
			source: "a   b",
			segments: []whiteSpaceSegment{
				{start: 0, end: 3, mode: WhiteSpacePre},    // "a  " verbatim
				{start: 3, end: 5, mode: WhiteSpaceNormal}, // " b" keeps (last item kNotCollapsible)
			},
			want: "a   b",
		},
		{
			name:     "a box splits a run, each side keeps one space",
			source:   "a   b", // run [1,4), box at 2 splits it
			segments: []whiteSpaceSegment{{start: 0, end: 5, mode: WhiteSpaceNormal}},
			boxes:    []int{2},
			want:     "a  b",
		},
		{
			name:     "a box after a kept space keeps the following space",
			source:   "a  b",
			segments: []whiteSpaceSegment{{start: 0, end: 4, mode: WhiteSpaceNormal}},
			boxes:    []int{2}, // after "a ": the run [1,2) keeps, [2,3) keeps too
			want:     "a  b",
		},
		{
			name:     "a paragraph-start run before a box is removed, the run after the box keeps",
			source:   "   b",
			segments: []whiteSpaceSegment{{start: 0, end: 4, mode: WhiteSpaceNormal}},
			boxes:    []int{2},
			want:     " b",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			display, _, _ := collapseSegments(c.source, c.segments, c.boxes, WhiteSpaceNormal)
			if display != c.want {
				t.Errorf("collapseSegments = %q, want %q", display, c.want)
			}
		})
	}
}

// TestCollapseMapConversions pins the byte map's offset conversions on
// "Alpha   beta\n\ngamma" under normal (Alpha = 0..5, spaces = 5..8,
// beta = 8..12, newlines = 12..14, gamma = 14..19): the kept space is
// identity-mapped to the run's first character, the removed bytes map
// to the collapse point, and the boundary choices follow
// GetFirstMappingUnit / GetLastMappingUnit (offset_mapping.cc:
// 506-560).
func TestCollapseMapConversions(t *testing.T) {
	const source = "Alpha   beta\n\ngamma"
	display, m := collapseWhiteSpace(source, WhiteSpaceNormal)
	if want := "Alpha beta gamma"; display != want {
		t.Fatalf("display = %q, want %q", display, want)
	}
	if m == nil {
		t.Fatal("the collapse changed the text; the map must be non-nil")
	}
	domCases := []struct {
		dom  int
		want int
	}{
		{0, 0}, {5, 5}, // before the space run
		{6, 6}, {7, 6}, // inside the removed run → the collapse point
		{8, 6},             // beta starts at the same display offset
		{12, 10}, {13, 11}, // the newline run behaves the same
		{14, 11},           // gamma starts at display 11
		{19, 16}, {20, 16}, // the end clamps
	}
	for _, c := range domCases {
		if got := m.domToText(c.dom); got != c.want {
			t.Errorf("domToText(%d) = %d, want %d", c.dom, got, c.want)
		}
	}
	firstCases := []struct {
		text int
		want int
	}{
		{0, 0}, {5, 5}, // the kept space's start
		{6, 6},   // the removed run's start (first wins at the boundary)
		{10, 12}, // the newline run's start
		{11, 13}, // the second newline's removed run
		{16, 19}, // the end
	}
	for _, c := range firstCases {
		if got := m.textToDomFirst(c.text); got != c.want {
			t.Errorf("textToDomFirst(%d) = %d, want %d", c.text, got, c.want)
		}
	}
	lastCases := []struct {
		text int
		want int
	}{
		{0, 0}, {5, 5},
		{6, 8},   // beta's start: the last unit containing 6
		{11, 14}, // gamma's start
		{16, 19},
	}
	for _, c := range lastCases {
		if got := m.textToDomLast(c.text); got != c.want {
			t.Errorf("textToDomLast(%d) = %d, want %d", c.text, got, c.want)
		}
	}
}

// TestCollapseMapIdentityUnits covers the map's invariants: units tile
// the source, domToText is monotone, and textToDomFirst/Last bracket
// every display offset.
func TestCollapseMapIdentityUnits(t *testing.T) {
	const source = "  a \n\n b  "
	display, m := collapseWhiteSpace(source, WhiteSpaceNormal)
	if want := "a b"; display != want {
		t.Fatalf("display = %q, want %q", display, want)
	}
	covered := 0
	prevText := 0
	for _, u := range m.units {
		if u.domStart != covered {
			t.Fatalf("units must tile the source: unit %+v starts at %d, want %d", u, u.domStart, covered)
		}
		if u.domEnd < u.domStart || u.textEnd < u.textStart || u.textStart != prevText {
			t.Fatalf("bad unit %+v", u)
		}
		if !u.collapsed() && u.domEnd-u.domStart != u.textEnd-u.textStart {
			t.Fatalf("identity unit maps different lengths: %+v", u)
		}
		covered, prevText = u.domEnd, u.textEnd
	}
	if covered != len(source) {
		t.Errorf("units cover %d bytes, want %d", covered, len(source))
	}
	if prevText != len(display) {
		t.Errorf("units end at display %d, want %d", prevText, len(display))
	}
	lastDom := -1
	for dom := 0; dom <= len(source); dom++ {
		got := m.domToText(dom)
		if got < lastDom || got > len(display) {
			t.Errorf("domToText(%d) = %d is not monotone in [0, %d]", dom, got, len(display))
		}
		lastDom = got
	}
	for text := 0; text <= len(display); text++ {
		first, last := m.textToDomFirst(text), m.textToDomLast(text)
		if first > last || first < 0 || last > len(source) {
			t.Errorf("textToDomFirst/Last(%d) = %d/%d out of range", text, first, last)
		}
		if m.domToText(first) != m.domToText(last) {
			t.Errorf("textToDomFirst/Last(%d) = %d/%d map to different display offsets", text, first, last)
		}
	}
}

// ---------------------------------------------------------------------------
// Element-level tests (the caret byte map acceptance)
// ---------------------------------------------------------------------------

// whiteSpaceWindow opens a fresh test window on the REAL text system
// (the styled_text_wrap_test.go discipline: the row breaks and the
// caret geometry under test are the shaper's, and the deterministic
// stub never breaks rows), one per case — a window keeps its root view.
func whiteSpaceWindow(t *testing.T) (*Window, *App) {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 120})
	text, err := windowTextSystemOf()
	if err != nil {
		CloseTestWindow(window)
		t.Fatalf("real text system: %v", err)
	}
	drawState(window).text = text
	return window, app
}

// whiteSpaceRootView is the window's root view: it renders the tree the
// test builds fresh each frame.
type whiteSpaceRootView struct {
	build func() AnyElement
}

// Render implements Render[whiteSpaceRootView].
func (v *whiteSpaceRootView) Render(w *Window, cx *Context[whiteSpaceRootView]) AnyElement {
	return v.build()
}

// setWhiteSpaceRoot installs a root view that builds the given tree.
func setWhiteSpaceRoot(t *testing.T, window *Window, app *App, build func() AnyElement) {
	t.Helper()
	window.SetRootView(ViewOf(NewEntity(app, window.Scope(), func(v *whiteSpaceRootView, cx *Context[whiteSpaceRootView]) {
		v.build = build
	})))
}

// TestWhiteSpaceStyledTextCaretByteMap is the ticket's caret geometry
// test: a StyledText holding collapsed source text answers
// PositionForIndex and IndexForPosition in SOURCE byte offsets — the
// removed offsets share the collapse point's position, and a hit maps
// back to a source offset whose position is the hit's.
func TestWhiteSpaceStyledTextCaretByteMap(t *testing.T) {
	window, app := whiteSpaceWindow(t)
	defer CloseTestWindow(window)
	const source = "Alpha   beta\n\ngamma"
	styled := NewStyledText(source)
	setWhiteSpaceRoot(t, window, app, func() AnyElement {
		return Div().W(Px(360)).Child(styled).IntoElement()
	})
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	layout := styled.Layout()

	if layout.Len() != len(source) {
		t.Errorf("Len() = %d, want the source length %d", layout.Len(), len(source))
	}
	if layout.Text() != source {
		t.Errorf("Text() = %q, want the source %q", layout.Text(), source)
	}
	// The removed offsets 6 and 7 (inside the space run) map to the same
	// caret position as 8 (beta's start); 5 (before the run) sits
	// strictly left.
	p5, ok := layout.PositionForIndex(5)
	if !ok {
		t.Fatal("PositionForIndex(5) not ok")
	}
	for _, index := range []int{6, 7, 8} {
		p, ok := layout.PositionForIndex(index)
		if !ok {
			t.Fatalf("PositionForIndex(%d) not ok", index)
		}
		if p.X <= p5.X {
			t.Errorf("PositionForIndex(%d).X = %v is not right of the run's start %v", index, p.X, p5.X)
		}
	}
	p6, _ := layout.PositionForIndex(6)
	p7, _ := layout.PositionForIndex(7)
	p8, _ := layout.PositionForIndex(8)
	if p6 != p7 || p7 != p8 {
		t.Errorf("the removed offsets must share the collapse point: 6=%v 7=%v 8=%v", p6, p7, p8)
	}
	// The newline run: 13 (removed) and 14 (gamma) share the collapse
	// point after beta.
	p13, _ := layout.PositionForIndex(13)
	p14, _ := layout.PositionForIndex(14)
	if p13 != p14 {
		t.Errorf("PositionForIndex(13) = %v must equal gamma's start %v", p13, p14)
	}
	// Hits between ADJACENT caret positions map back to a source offset
	// whose position is one of the two bracketing carets (the shaper's
	// hit test snaps a point inside a cluster's span to one of its
	// edges; the byte map must not move the caret off the bracket). The
	// adjacent pairs bracket the two collapse points: the source offsets
	// 4,5,6,8 (the space run) and 11,12,13,14 (the newline run).
	for _, pair := range [][2]int{{4, 5}, {5, 6}, {6, 8}, {11, 12}, {12, 13}, {13, 14}} {
		left, ok := layout.PositionForIndex(pair[0])
		if !ok {
			t.Fatalf("PositionForIndex(%d) not ok", pair[0])
		}
		right, ok := layout.PositionForIndex(pair[1])
		if !ok {
			t.Fatalf("PositionForIndex(%d) not ok", pair[1])
		}
		if left == right {
			continue // a collapsed pair shares the collapse point
		}
		mid := Point{X: (left.X + right.X) / 2, Y: left.Y}
		index, ok := layout.IndexForPosition(mid)
		if !ok {
			t.Fatalf("IndexForPosition(%v) not ok", mid)
		}
		back, ok := layout.PositionForIndex(index)
		if !ok {
			t.Fatalf("PositionForIndex(%d) not ok", index)
		}
		if back != left && back != right {
			t.Errorf("hit between %v and %v mapped to %d at %v, want one of the bracketing carets", left, right, index, back)
		}
	}
}

// TestWhiteSpaceStyledTextPreKeepsRows pins the preserve modes through
// the same element: pre, pre-wrap and pre-line keep the newlines (three
// rows), and WrappedText returns the source.
func TestWhiteSpaceStyledTextPreKeepsRows(t *testing.T) {
	const source = "a\n\nb"
	for _, mode := range []WhiteSpace{WhiteSpacePre, WhiteSpacePreWrap, WhiteSpacePreLine} {
		window, app := whiteSpaceWindow(t)
		styled := NewStyledText(source)
		mode := mode
		setWhiteSpaceRoot(t, window, app, func() AnyElement {
			return Div().W(Px(360)).Whitespace(mode).Child(styled).IntoElement()
		})
		if _, err := DrawWindowFrame(window); err != nil {
			CloseTestWindow(window)
			t.Fatalf("DrawWindowFrame: %v", err)
		}
		inner := styled.layout.state.inner
		if got := inner.document.lineCount(); got != 3 {
			t.Errorf("mode %v: lineCount = %d, want 3 (the two newlines break rows)", mode, got)
		}
		if wrapped := styled.Layout().WrappedText(); wrapped != source {
			t.Errorf("mode %v: WrappedText = %q, want the source %q", mode, wrapped, source)
		}
		CloseTestWindow(window)
	}
}

// whiteSpaceProbe measures its child under a mode's ambient text style
// and a definite available width (the measureProbe pattern of
// text_mincontent_test.go), recording the size the element measured.
type whiteSpaceProbe struct {
	child      AnyElement
	mode       WhiteSpace
	lineHeight float32
	size       Size
}

// ID implements Element.
func (p *whiteSpaceProbe) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element.
func (p *whiteSpaceProbe) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element.
func (p *whiteSpaceProbe) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	p.size = WithTextStyle(w, &TextStyleRefinement{
		WhiteSpace: Some(p.mode),
		LineHeight: Some(DefinitePx(p.lineHeight)),
	}, func(w *Window) Size {
		return p.child.LayoutAsRoot(AvailableSize{
			Width:  DefiniteAvailableSpace(360),
			Height: MaxContentAvailableSpace(),
		}, w, app)
	})
	id, err := requestLayoutNode(w, nil, DefaultStyle(), nil)
	if err != nil {
		panic(fmt.Sprintf("gpui: white-space probe request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements Element.
func (p *whiteSpaceProbe) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) struct{} {
	return struct{}{}
}

// Paint implements Element.
func (p *whiteSpaceProbe) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *struct{}, w *Window, app *App) {
}

// TestWhiteSpaceElementEmptyCollapse pins the empty paragraph: an
// element whose whole content collapses away measures zero height (no
// line box — Chromium's self-collapsing empty line,
// inline_layout_algorithm.cc:1488-1492), and a non-collapsing mode
// keeps its rows.
func TestWhiteSpaceElementEmptyCollapse(t *testing.T) {
	const source = "  \n\t "
	cases := []struct {
		mode       WhiteSpace
		wantHeight float32
	}{
		{WhiteSpaceNormal, 0},
		{WhiteSpaceNowrap, 0},
		// The preserve modes keep the newline: the shaper gives the
		// hard-break row plus the trailing row (a lone \\n" is two rows
		// in the pin's shaper — textspec's TestNewlineOnlyEdgeCase).
		{WhiteSpacePreLine, 50},
		{WhiteSpacePre, 50},
		{WhiteSpacePreWrap, 50},
	}
	for _, c := range cases {
		// The Text leaf (div.go's textElement) through the probe.
		probe := &whiteSpaceProbe{child: Text(source), mode: c.mode, lineHeight: 25}
		window, app := whiteSpaceWindow(t)
		setWhiteSpaceRoot(t, window, app, func() AnyElement {
			return CustomElement[struct{}, struct{}](probe)
		})
		if _, err := DrawWindowFrame(window); err != nil {
			CloseTestWindow(window)
			t.Fatalf("DrawWindowFrame: %v", err)
		}
		if got := probe.size.Height; got != c.wantHeight {
			t.Errorf("mode %v: Text height = %v, want %v", c.mode, got, c.wantHeight)
		}
		CloseTestWindow(window)
		// The StyledText path (styled_text.go) empties the same way (a
		// fresh window: the root view is not replaceable).
		window2, app2 := whiteSpaceWindow(t)
		styled := NewStyledText(source)
		setWhiteSpaceRoot(t, window2, app2, func() AnyElement {
			return Div().W(Px(360)).LineHeight(Px(25)).Whitespace(c.mode).Child(styled).IntoElement()
		})
		if _, err := DrawWindowFrame(window2); err != nil {
			CloseTestWindow(window2)
			t.Fatalf("DrawWindowFrame: %v", err)
		}
		if got := styled.layout.state.inner.size.Height; got != c.wantHeight {
			t.Errorf("mode %v: StyledText height = %v, want %v", c.mode, got, c.wantHeight)
		}
		CloseTestWindow(window2)
	}
}

// TestWhiteSpaceStyledTextRunsRemapped pins the run remap: runs covering
// collapsed bytes keep their styles on the display text, and the runs
// still cover the display text exactly.
func TestWhiteSpaceStyledTextRunsRemapped(t *testing.T) {
	const source = "aa   bb  cc"
	// One run over the whole source, one over "  cc".
	spacing := float32(1)
	runs := []TextRun{
		{Len: 8, Font: FontDescriptor{Family: "Consolas"}},
		{Len: 3, Font: FontDescriptor{Family: "Arial"}, LetterSpacing: &spacing},
	}
	display, remapped, _ := collapseStyledRuns(source, runs, WhiteSpaceNormal)
	if want := "aa bb cc"; display != want {
		t.Fatalf("display = %q, want %q", display, want)
	}
	if err := validateRuns(display, remapped); err != nil {
		t.Fatalf("the remapped runs do not cover the display text: %v", err)
	}
	total := 0
	for _, run := range remapped {
		total += run.Len
	}
	if total != len(display) {
		t.Errorf("remapped runs cover %d bytes, want %d", total, len(display))
	}
	// The letter spacing of the run that styled "  cc" survives on the
	// piece that reaches the display text.
	foundSpaced := false
	cursor := 0
	for _, run := range remapped {
		if run.LetterSpacing != nil && display[cursor:cursor+run.Len] == "cc" {
			foundSpaced = true
		}
		cursor += run.Len
	}
	if !foundSpaced {
		cursor = 0
		var spans []string
		for _, run := range remapped {
			spans = append(spans, display[cursor:cursor+run.Len])
			cursor += run.Len
		}
		t.Errorf("the letter-spaced run lost its display text (pieces %v)", spans)
	}
}

// ---------------------------------------------------------------------------
// The tab-size stops and the em tracking
// ---------------------------------------------------------------------------

// TestWhiteSpaceTabSizeStops pins the browser tab stops through the real
// shaping service: a pre-mode tab advances to the next multiple of
// 8 × (the space advance + the letter spacing) from its row start
// (tab_size.h GetPixelSize over Font::TabWidth, font.cc:319-337), which
// differs from the pin's DirectWrite 4em stops. This test needs the
// native text service.
func TestWhiteSpaceTabSizeStops(t *testing.T) {
	ts, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	const fontSize = 16.0
	const lineHeight = 25.0
	shape := func(text string, tabSizeSpaces *uint32) *WrappedLine {
		t.Helper()
		run := TextRun{Len: len(text), Font: FontDescriptor{Family: "Segoe UI", Weight: DefaultFontWeight}}
		line, err := ts.ShapeText(TextLayoutRequest{
			Text:          text,
			FontSize:      fontSize,
			Runs:          []TextRun{run},
			TabSizeSpaces: tabSizeSpaces,
		})
		if err != nil {
			t.Fatalf("ShapeText(%q, tabSize %v): %v", text, tabSizeSpaces, err)
		}
		t.Cleanup(func() { _ = line.Dispose() })
		return line
	}
	// The space advance: "a b"'s space cluster advance (the browser tab
	// shapes as a space, so its initial advance is the same).
	spaceLine := shape("a b", nil)
	spaceStart, err := spaceLine.Caret(1, CaretAffinityDownstream, lineHeight)
	if err != nil {
		t.Fatalf("Caret(1): %v", err)
	}
	spaceEnd, err := spaceLine.Caret(2, CaretAffinityUpstream, lineHeight)
	if err != nil {
		t.Fatalf("Caret(2): %v", err)
	}
	spaceAdvance := spaceEnd.Bounds.X - spaceStart.Bounds.X

	// The browser-mode tab (byte 1 of "a\tb") starts at 'a's advance and
	// must reach the next multiple of 8 × spaceAdvance past x(a).
	browser := shape("a\tb", ptrUint32T(CSSDefaultTabSizeSpaces))
	aEnd, err := browser.Caret(1, CaretAffinityDownstream, lineHeight)
	if err != nil {
		t.Fatalf("browser Caret(1): %v", err)
	}
	tabEnd, err := browser.Caret(2, CaretAffinityUpstream, lineHeight)
	if err != nil {
		t.Fatalf("browser Caret(2): %v", err)
	}
	stop := 8 * spaceAdvance
	x := aEnd.Bounds.X
	advance := tabEnd.Bounds.X - aEnd.Bounds.X
	want := browserTabTarget(x, stop, spaceAdvance/2)
	if !nearTab(advance, want) {
		t.Errorf("browser tab advance = %v, want %v (x %v, stop %v, space %v)", advance, want, x, stop, spaceAdvance)
	}

	// The pin's DirectWrite stop differs (4 × fontSize = 64 at 16px), and
	// the two must not agree (the modes are observably different).
	pin := shape("a\tb", nil)
	pinTabEnd, err := pin.Caret(2, CaretAffinityUpstream, lineHeight)
	if err != nil {
		t.Fatalf("pin Caret(2): %v", err)
	}
	pinAdvance := pinTabEnd.Bounds.X - aEnd.Bounds.X
	if nearTab(pinAdvance, advance) {
		t.Errorf("the pin stop %v equals the browser stop %v; the modes are not distinguishable", pinAdvance, advance)
	}
}

// nearTab compares tab advances at the fit's tolerance.
func nearTab(a, b float32) bool {
	d := a - b
	return d < tabFitTolerance*5 && d > -tabFitTolerance*5
}

// TestWhiteSpaceWrapsAtWidth pins the mode matrix's wrap rule
// (white_space.h ShouldWrapLine) against the element wrap evaluation.
func TestWhiteSpaceWrapsAtWidth(t *testing.T) {
	cases := []struct {
		mode  WhiteSpace
		wraps bool
	}{
		{WhiteSpaceNormal, true},
		{WhiteSpaceNowrap, false},
		{WhiteSpacePre, false},
		{WhiteSpacePreLine, true},
		{WhiteSpacePreWrap, true},
	}
	for _, c := range cases {
		if got := c.mode.whiteSpaceWrapsAtWidth(); got != c.wraps {
			t.Errorf("%v.whiteSpaceWrapsAtWidth() = %v, want %v", c.mode, got, c.wraps)
		}
	}
}

// TestLetterSpacingEmResolution pins the em resolution: the em folds
// against the font size, and an explicit pixel spacing wins.
func TestLetterSpacingEmResolution(t *testing.T) {
	px := float32(2)
	style := DefaultTextStyle()
	style.LetterSpacingEm = ptrFloat32T(0.025)
	if got := style.resolveLetterSpacing(16); got == nil || *got != 0.4 {
		t.Errorf("resolveLetterSpacing(16) = %v, want 0.4 (0.025 × 16)", got)
	}
	if got := style.resolveLetterSpacing(32); got == nil || *got != 0.8 {
		t.Errorf("resolveLetterSpacing(32) = %v, want 0.8", got)
	}
	style.LetterSpacing = &px
	if got := style.resolveLetterSpacing(32); got == nil || *got != 2 {
		t.Errorf("resolveLetterSpacing with px set = %v, want the px value 2", got)
	}
	// ToRunAt folds the em; ToRun keeps the pin's pixel-only field.
	style.LetterSpacing = nil
	run := style.ToRunAt(4, 16)
	if run.LetterSpacing == nil || *run.LetterSpacing != 0.4 {
		t.Errorf("ToRunAt(4, 16).LetterSpacing = %v, want 0.4", run.LetterSpacing)
	}
	pin := style.ToRun(4)
	if pin.LetterSpacing != nil {
		t.Errorf("ToRun(4).LetterSpacing = %v, want nil (the pin's pixel-only field)", pin.LetterSpacing)
	}
}

// TestWhiteSpaceTransformOrder pins the pipeline order: the transform
// (byte-preserving) precedes the collapse, so an uppercased source
// collapses the same way.
func TestWhiteSpaceTransformOrder(t *testing.T) {
	upper := TextTransformUppercase
	source := applyTextTransformPreservingByteLen("alpha   beta", &upper)
	display, _ := collapseWhiteSpace(source, WhiteSpaceNormal)
	if display != "ALPHA BETA" {
		t.Errorf("transform-then-collapse = %q, want %q", display, "ALPHA BETA")
	}
}

// TestCollapseWhiteSpaceTabUnderPreIsKept guards the tab pipeline: the
// preserve modes keep tabs in the display text (they reach the tab-stop
// shaping); the collapsing modes fold them into the space run.
func TestCollapseWhiteSpaceTabUnderPreIsKept(t *testing.T) {
	if display, _ := collapseWhiteSpace("a\tb", WhiteSpacePre); !strings.ContainsRune(display, '\t') {
		t.Error("pre must keep the tab")
	}
	if display, _ := collapseWhiteSpace("a\tb", WhiteSpaceNormal); strings.ContainsRune(display, '\t') {
		t.Error("normal must collapse the tab into the space run")
	}
}

// ptrUint32T boxes a uint32.
func ptrUint32T(v uint32) *uint32 { return &v }

// ptrFloat32T boxes a float32.
func ptrFloat32T(v float32) *float32 { return &v }
