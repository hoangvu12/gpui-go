package base

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// Base ticket 44: hard tabs in the input display expand to tab-size
// columns, Zed's tab map rule (input_tab_map.go).

func f32Near(a, b float32) bool { return math.Abs(float64(a-b)) < 0.05 }

func TestInputTabsColumnsFollowZedsTabMap(t *testing.T) {
	tabs := newInputTabs(4, 10, gpui.FontDescriptor{})
	for _, c := range []struct {
		text string
		col  int
		want int
	}{
		{"\t", 0, 4},
		{"a\t", 0, 4},
		{"abc\t", 0, 4},
		{"abcd\t", 0, 8},
		{" \t", 0, 4},
		{"\t\t", 0, 8},
		{"\tx\t", 0, 8},
		{"\t", 3, 4},
		{"\t", 4, 8},
		{"中\t", 0, 4},
	} {
		if got := tabs.columnAfter(c.text, c.col); got != c.want {
			t.Errorf("columnAfter(%q, %d) = %d, want %d", c.text, c.col, got, c.want)
		}
	}
	if got := newInputTabs(0, 10, gpui.FontDescriptor{}).columnAfter("a\tb", 0); got != 3 {
		t.Errorf("tab size 0 spans one column per tab: %d, want 3", got)
	}
}

// A row keeps its bytes: the tab becomes a space in its own run, in the
// input font, widened by letter spacing to its columns.
func TestInputTabsExpandKeepsByteRanges(t *testing.T) {
	base := gpui.FontDescriptor{Family: "Mono", Weight: 400}
	bold := gpui.FontDescriptor{Family: "Mono", Weight: 700}
	tabs := newInputTabs(4, 10, base)
	spacing := float32(2)
	runs := []gpui.TextRun{{Len: 2, Font: bold}, {Len: 4, Font: bold, LetterSpacing: &spacing}}
	text, out := tabs.expand("a\tbc\td", 1, runs)
	if text != "a bc d" {
		t.Errorf("text %q, want the tabs as spaces", text)
	}
	type run struct {
		len     int
		weight  float32
		spacing float32
	}
	var got []run
	for _, r := range out {
		var ls float32
		if r.LetterSpacing != nil {
			ls = *r.LetterSpacing
		}
		got = append(got, run{r.Len, r.Font.Weight, ls})
	}
	// Column 1 'a' → 2; the tab spans 2..4 (2 columns); "bc" → 6; the
	// tab spans 6..8 (2 columns) on top of its run's own spacing.
	want := []run{{1, 700, 0}, {1, 400, 10}, {2, 700, 2}, {1, 400, 12}, {1, 700, 2}}
	if !slices.Equal(got, want) {
		t.Errorf("runs %+v, want %+v", got, want)
	}
	if plain, same := tabs.expand("abc", 0, runs[:1]); plain != "abc" || len(same) != 1 {
		t.Errorf("a row without tabs changed: %q %v", plain, same)
	}
}

// A state's display map is created before its first paint sets the
// font (size 0): an edit then must not measure the space through the
// real text service, which rejects a zero size (the gallery capture
// panicked so).
func TestInputTabsSkipMeasuringBeforeTheFontIsSet(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	window := gpui.NewTestWindow(ta.App(), gpui.Size{Width: 200, Height: 100})
	t.Cleanup(func() { gpui.CloseTestWindow(window) })
	if err := gpui.UseRealTextSystem(window); err != nil {
		t.Fatalf("UseRealTextSystem: %v", err)
	}
	wrapper := newTextWrapper(gpui.FontDescriptor{}, 0, nil)
	wrapper.updateAll(rope.FromStr("a\tb"), window)
	if tabs := wrapper.tabsFor(window); tabs.active() {
		t.Errorf("tabs measured a space (%v) at font size 0", tabs.spaceWidth)
	}
}

// buildRealTextEditor is an editor on a test window switched to the real
// text service (nothing is shown), so rows are shaped by the platform's
// fonts: the fake test text system draws every character at one advance
// and ignores letter spacing.
func buildRealTextEditor(t *testing.T, value string, tab TabSize, softWrap bool) *inputView[EditorMode] {
	t.Helper()
	v := buildEditorWith(t, gpui.Size{Width: 800, Height: 400}, func(s EditorState) EditorState {
		return s.TabSize(tab).SoftWrap(softWrap).DefaultValue(value)
	})
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		if err := gpui.UseRealTextSystem(w); err != nil {
			t.Fatalf("UseRealTextSystem: %v", err)
		}
		// Forget what the first frame measured with the fake system.
		wrapper := &s.displayMap.wrap.wrapper
		wrapper.lineWrapper, wrapper.spaceWidth = nil, 0
		wrapper.updateAll(s.text, w)
		s.Focus(w, cx)
		cx.Notify()
	})
	v.cx.RunUntilParked()
	return v
}

// x of a byte column on a buffer row of the painted layout.
func layoutX[M InputModeKind](t *testing.T, s *InputBaseState[M], row, col int) float32 {
	t.Helper()
	line, ok := s.lastLayout.line(row)
	if !ok {
		t.Fatalf("row %d is not laid out", row)
	}
	pos, ok := line.positionForIndex(col, s.lastLayout, false)
	if !ok {
		t.Fatalf("row %d column %d has no position", row, col)
	}
	return pos.X
}

func TestEditorHardTabSpansItsTabSizeColumns(t *testing.T) {
	v := buildRealTextEditor(t, "\tfoo\nab\tc\nabcd\te\n  \tx", TabSize{TabSize: 4, HardTabs: true}, false)
	v.read(func(s *EditorState) {
		space := s.lastLayout.spaceWidth
		if !(space > 0) {
			t.Fatalf("space width %v", space)
		}
		// The window font may be proportional: a tab spans its columns at
		// the space's width from wherever the text before it ends.
		for _, c := range []struct {
			row, col int
			span     int
		}{
			{0, 1, 4}, // "\t": columns 0..4
			{1, 3, 2}, // "ab\t": columns 2..4
			{2, 5, 4}, // "abcd\t": columns 4..8
			{3, 3, 2}, // "  \t": columns 2..4
		} {
			start := layoutX(t, s, c.row, c.col-1)
			want := start + float32(c.span)*space
			if got := layoutX(t, s, c.row, c.col); !f32Near(got, want) {
				t.Errorf("row %d: the tab spans %v..%v, want %d columns to %v", c.row, start, got, c.span, want)
			}
		}
		// The row keeps its tabs for the whitespace markers.
		if text := s.lastLayout.lines[0].wrappedLines[0].text; text != "\tfoo" {
			t.Errorf("row text %q, want the tab kept", text)
		}
	})
}

func TestEditorCaretStepsOverAHardTabAndClicksSnapToItsNearerEdge(t *testing.T) {
	v := buildRealTextEditor(t, "\tfoo\n    x", TabSize{TabSize: 4, HardTabs: true}, false)
	setCursor := func(offset int) {
		v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
			s.moveTo(offset, nil, cx)
		})
		v.cx.RunUntilParked()
	}
	cursor := func() int {
		var c int
		v.read(func(s *EditorState) { c = s.Cursor() })
		return c
	}
	setCursor(0)
	v.cx.SimulateKeystrokes("right")
	if got := cursor(); got != 1 {
		t.Errorf("right from the line start: cursor %d, want 1 (past the whole tab)", got)
	}
	v.cx.SimulateKeystrokes("left")
	if got := cursor(); got != 0 {
		t.Errorf("left back: cursor %d, want 0", got)
	}

	// Up from the spaces below goes by x: four spaces end where the tab
	// does; one space is nearer the tab's start, three nearer its end.
	for _, c := range []struct{ spaces, want int }{{4, 1}, {1, 0}, {3, 1}} {
		setCursor(len("\tfoo\n") + c.spaces)
		v.cx.SimulateKeystrokes("up")
		if got := cursor(); got != c.want {
			t.Errorf("up from %d spaces: cursor %d, want %d", c.spaces, got, c.want)
		}
	}

	var space float32
	var origin gpui.Point
	v.read(func(s *EditorState) {
		space = s.lastLayout.spaceWidth
		origin = gpui.Point{X: s.lastBounds.Origin.X + s.lastLayout.lineNumberWidth, Y: s.lastBounds.Origin.Y + s.lastLayout.lineHeight*0.5}
	})
	for _, c := range []struct {
		columns float32
		want    int
	}{{1.5, 0}, {2.5, 1}, {3.9, 1}} {
		at := gpui.Point{X: origin.X + c.columns*space, Y: origin.Y}
		v.cx.SimulateMouseDown(at, gpui.MouseButtonLeft, gpui.Modifiers{})
		v.cx.SimulateMouseUp(at, gpui.MouseButtonLeft, gpui.Modifiers{})
		if got := cursor(); got != c.want {
			t.Errorf("click at column %v inside the tab: cursor %d, want %d", c.columns, got, c.want)
		}
	}
}

// The wrapper measures a tab at its columns: a row of "\taaaa bbbb" at a
// width of 9.5 columns breaks before "bbbb" (4 + 4 + 1 columns fit).
func TestEditorSoftWrapMeasuresHardTabsAtTheirColumns(t *testing.T) {
	const value = "\taaaa bbbb"
	v := buildRealTextEditor(t, value, TabSize{TabSize: 4, HardTabs: true}, false)
	v.update(func(s *EditorState, w *gpui.Window, _ *gpui.Context[InputBaseState[EditorMode]]) {
		tabs := s.displayTabs(w)
		font := s.displayMap.wrap.wrapper.font
		size := s.displayMap.wrap.wrapper.fontSize
		width := tabs.tabWidth(0) + w.TextSystem().LayoutWidth(font, size, 'a')*4 +
			tabs.spaceWidth + w.TextSystem().LayoutWidth(font, size, 'b')/2
		s.displayMap.onLayoutChanged(&width, w)
		got := s.displayMap.wrappedRanges(0)
		want := []InputSelection{sel(0, 6), sel(6, len(value))}
		if !slices.Equal(got, want) {
			t.Errorf("wrapped rows %v, want %v (break before \"bbbb\")", got, want)
		}
	})
}

// A wrapped continuation row keeps counting tab columns from the buffer
// line's start (Zed's tab map sits before its wrap map): "aaaaaaaa " is
// columns 0..9, so on the next row "bb" is 9..11 and the tab 11..12, one
// column (counted from the row's start it would span two).
func TestEditorHardTabOnAWrappedRowKeepsItsBufferColumns(t *testing.T) {
	const value = "aaaaaaaa bb\tc"
	v := buildRealTextEditor(t, value, TabSize{TabSize: 4, HardTabs: true}, false)
	var bWidth float32
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		tabs := s.displayTabs(w)
		font := s.displayMap.wrap.wrapper.font
		size := s.displayMap.wrap.wrapper.fontSize
		bWidth = w.TextSystem().LayoutWidth(font, size, 'b')
		// Wrap after "aaaaaaaa " (soft wrap is off, so the frame keeps it).
		width := w.TextSystem().LayoutWidth(font, size, 'a')*8 + tabs.spaceWidth + bWidth/2
		s.displayMap.onLayoutChanged(&width, w)
		cx.Notify()
	})
	v.cx.RunUntilParked()
	v.read(func(s *EditorState) {
		rows := s.displayMap.wrappedRanges(0)
		if want := []InputSelection{sel(0, 9), sel(9, len(value))}; !slices.Equal(rows, want) {
			t.Fatalf("rows %v, want %v", rows, want)
		}
		line, _ := s.lastLayout.line(0)
		second := line.wrappedLines[1]
		space := s.lastLayout.spaceWidth
		want := 2*bWidth + space
		if got := second.xForIndex(strings.Index(value[9:], "c")); !f32Near(got, want) {
			t.Errorf("'c' at x %v on the continuation row, want \"bb\" + 1 column = %v", got, want)
		}
	})
}
