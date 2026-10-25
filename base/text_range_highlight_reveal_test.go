package base

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/state.rs reveal_range module tests (2645-3186). Of
// 22: 8 ported here, 3 deviated here, 11 ported as real-window tests
// (text_range_highlight_reveal_real_windows_test.go, ticket 37; one of
// them deviated, see there).
//   - deviated (they pass, but the test text system cannot exercise what
//     they pin): an_empty_range_reveals_its_line and
//     revealing_a_visible_block_does_not_scroll. gpui's TestApp text
//     system lays a whole document out on one visual line (no soft wrap,
//     no hard line breaks, gpui/testwindow.go testTextSystem), so the
//     words(400) paragraphs are one row and nothing scrolls before the
//     "unmoved" check. a_reveal_not_carried_out_in_time_is_dropped
//     backdates its request instead of advancing the clock inside the
//     update (see the test).
//   - real windows: the 11 that need a block laid out on many rows, most
//     also an enclosing list scrolling to a line inside one item through
//     Window.RequestAutoscroll (gpui/window.go, taken by gpui.List in
//     prepaint since ticket 37).

// revealContainer is where the view sits in a 200 x 100 frame
// (U:text/state.rs:2654-2668).
type revealContainer struct {
	kind  string // "scrollable", "list", "clamped", "fixed"
	outer *gpui.ListState
}

func revealWindow(t *testing.T, ta *gpui.TestApp, markdown string, container revealContainer) (gpui.Entity[TextViewState], *gpui.VisualTestContext) {
	t.Helper()
	state := newTextViewStateEntity(ta, markdown)
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		frame := gpui.Div().Flex().W(gpui.Px(200)).H(gpui.Px(100))
		row := func(clamp bool) gpui.AnyElement {
			return gpui.List(container.outer, func(ix int, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
				if ix == 0 {
					return gpui.Div().Flex().H(gpui.Px(40)).IntoElement()
				}
				view := NewTextView(state)
				if clamp {
					view = view.MaxLines(2)
				}
				return view.IntoElement()
			}).SizeFull().IntoElement()
		}
		switch container.kind {
		case "scrollable":
			return frame.Child(NewTextView(state).Scrollable(true)).IntoElement()
		case "list":
			return frame.Child(row(false)).IntoElement()
		case "clamped":
			return frame.Child(row(true)).IntoElement()
		}
		return frame.Child(NewTextView(state)).IntoElement()
	})
	ta.RunUntilParked()
	revealDraw(t, cx)
	return state, cx
}

func revealDraw(t *testing.T, cx *gpui.VisualTestContext) {
	t.Helper()
	if _, err := gpui.DrawWindowFrame(cx.Window()); err != nil {
		t.Fatal(err)
	}
}

// revealRequest asks to reveal the first occurrence of needle, then runs
// then before anything is drawn (U:text/state.rs:2746-2760).
func revealRequestNeedle(t *testing.T, ta *gpui.TestApp, state gpui.Entity[TextViewState], needle string, then func(*TextViewState, *gpui.Context[TextViewState])) {
	t.Helper()
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		start := strings.Index(s.RenderedText().String(), needle)
		if err := s.RevealRange(mdRange(start, start+len(needle)), cx); err != nil {
			t.Fatal(err)
		}
		if then != nil {
			then(s, cx)
		}
	})
}

// revealNeedle reveals the first occurrence of needle and draws a few
// frames (U:text/state.rs:2762-2768).
func revealNeedle(t *testing.T, ta *gpui.TestApp, cx *gpui.VisualTestContext, state gpui.Entity[TextViewState], needle string) {
	t.Helper()
	revealRequestNeedle(t, ta, state, needle, nil)
	for range 3 {
		revealDraw(t, cx)
	}
}

func revealPending(ta *gpui.TestApp, state gpui.Entity[TextViewState]) bool {
	return readState(ta, state, func(s *TextViewState) bool { return s.pendingReveal != nil })
}

func revealScrollTop(ta *gpui.TestApp, state gpui.Entity[TextViewState]) gpui.ListOffset {
	return readState(ta, state, func(s *TextViewState) gpui.ListOffset { return s.listState.LogicalScrollTop() })
}

func revealParagraphs(count int) string {
	out := make([]string, count)
	for ix := range out {
		out[ix] = fmt.Sprintf("paragraph %d", ix)
	}
	return strings.Join(out, "\n\n")
}

func revealWords(count int) string {
	out := make([]string, count)
	for ix := range out {
		out[ix] = fmt.Sprintf("w%d", ix)
	}
	return strings.Join(out, " ")
}

func revealAssertUnmoved(t *testing.T, ta *gpui.TestApp, state gpui.Entity[TextViewState], top gpui.ListOffset) {
	t.Helper()
	if revealPending(ta, state) {
		t.Fatal("the reveal is still pending")
	}
	if now := revealScrollTop(ta, state); now != top {
		t.Fatalf("scrolled from %+v to %+v", top, now)
	}
}

// U:text/state.rs:2795-2806
func TestAScrollableViewScrollsToAnOffscreenBlock(t *testing.T) {
	ta := textTestApp(t)
	state, cx := revealWindow(t, ta, revealParagraphs(200), revealContainer{kind: "scrollable"})
	revealNeedle(t, ta, cx, state, "paragraph 150")
	if revealPending(ta, state) {
		t.Fatal("the reveal is still pending")
	}
	if top := revealScrollTop(ta, state); top.ItemIx < 140 || top.ItemIx > 150 {
		t.Fatalf("top %+v", top)
	}
	revealNeedle(t, ta, cx, state, "paragraph 3")
	if revealPending(ta, state) || revealScrollTop(ta, state).ItemIx > 3 {
		t.Fatalf("top %+v, pending %v", revealScrollTop(ta, state), revealPending(ta, state))
	}
}

// U:text/state.rs:2808-2823
func TestAnAppendAddingBlocksKeepsTheScrollPosition(t *testing.T) {
	ta := textTestApp(t)
	state, cx := revealWindow(t, ta, revealParagraphs(200), revealContainer{kind: "scrollable"})
	revealNeedle(t, ta, cx, state, "paragraph 100")
	top := revealScrollTop(ta, state)
	if top.ItemIx == 0 {
		t.Fatalf("top %+v", top)
	}
	sfPushStr(ta, state, "\n\nparagraph 200")
	revealDraw(t, cx)
	if n := readState(ta, state, func(s *TextViewState) int { return s.listState.ItemCount() }); n != 201 {
		t.Fatalf("%d items", n)
	}
	revealAssertUnmoved(t, ta, state, top)
}

// U:text/state.rs:2884-2894 (deviated: the request is backdated, below).
func TestARevealNotCarriedOutInTimeIsDropped(t *testing.T) {
	ta := textTestApp(t)
	state, cx := revealWindow(t, ta, revealParagraphs(200), revealContainer{kind: "scrollable"})
	// Deviation: upstream advances the clock 2s inside the update. The
	// TestApp clock only advances between updates, and advancing it serves
	// the frames the request scheduled, carrying the reveal out on time; so
	// the request is backdated by those 2s instead, which is the same age.
	revealRequestNeedle(t, ta, state, "paragraph 150", func(s *TextViewState, _ *gpui.Context[TextViewState]) {
		s.pendingReveal.requestedAt -= 2 * time.Second
	})
	revealDraw(t, cx)
	if revealPending(ta, state) {
		t.Fatal("the reveal is still pending")
	}
	if top := revealScrollTop(ta, state); top.ItemIx != 0 {
		t.Fatalf("top %+v", top)
	}
}

// U:text/state.rs:2896-2916 (deviated: one visual line, see the file
// comment).
func TestAnEmptyRangeRevealsItsLine(t *testing.T) {
	ta := textTestApp(t)
	state, cx := revealWindow(t, ta, revealWords(400), revealContainer{kind: "scrollable"})
	revealNeedle(t, ta, cx, state, "w390")
	top := revealScrollTop(ta, state)
	// A position on a visible line leaves the view where it is.
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		start := strings.Index(s.RenderedText().String(), "w391")
		if err := s.RevealRange(mdRange(start, start), cx); err != nil {
			t.Fatal(err)
		}
	})
	for range 3 {
		revealDraw(t, cx)
	}
	revealAssertUnmoved(t, ta, state, top)
}

// U:text/state.rs:3023-3032
func TestAnEmptyViewHasNothingToReveal(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "")
	ta.RunUntilParked()
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		if err := s.RevealRange(gpui.TextRange{}, cx); err != nil || s.pendingReveal != nil {
			t.Fatalf("err %v, pending %v", err, s.pendingReveal != nil)
		}
	})
}

// U:text/state.rs:3034-3045 (deviated: one visual line, see the file
// comment).
func TestRevealingAVisibleBlockDoesNotScroll(t *testing.T) {
	ta := textTestApp(t)
	markdown := revealWords(400) + "\n\n<div>html</div>\n\n" + revealWords(400)
	state, cx := revealWindow(t, ta, markdown, revealContainer{kind: "scrollable"})
	revealNeedle(t, ta, cx, state, "html")
	// Still in view a little further down.
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		list := state.Read(app, func(s *TextViewState, _ *gpui.App) *gpui.ListState { return s.listState })
		list.ScrollBy(30, w, app)
	})
	revealDraw(t, cx)
	top := revealScrollTop(ta, state)
	revealNeedle(t, ta, cx, state, "html")
	revealAssertUnmoved(t, ta, state, top)
}

// U:text/state.rs:3096-3114
func TestARevealIsDroppedWhenItsTextBeforeItChanges(t *testing.T) {
	ta := textTestApp(t)
	state, _ := revealWindow(t, ta, revealParagraphs(200), revealContainer{kind: "scrollable"})
	const target = "first words then the target"
	markdown := target + "\n\n" + revealParagraphs(200)
	sfSetText(ta, state, markdown)
	// Appending to its paragraph keeps it.
	revealRequestNeedle(t, ta, state, "target", func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		s.SetText(strings.Replace(markdown, "target", "target and more", 1), cx)
		if s.pendingReveal == nil {
			t.Error("an append after the target dropped the reveal")
		}
	})
	// An edit before it in its paragraph drops it.
	revealRequestNeedle(t, ta, state, "target", func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		s.SetText(strings.Replace(markdown, "words", "WORDS", 1), cx)
		if s.pendingReveal != nil {
			t.Error("an edit before the target kept the reveal")
		}
	})
}

// U:text/state.rs:3116-3123
func TestAClampedViewDoesNotReveal(t *testing.T) {
	ta := textTestApp(t)
	outer := gpui.NewListState(2, 1000)
	state, cx := revealWindow(t, ta, revealWords(400), revealContainer{kind: "clamped", outer: outer})
	revealNeedle(t, ta, cx, state, "w390")
	if revealPending(ta, state) {
		t.Fatal("a clamped view kept its reveal")
	}
	if top := outer.LogicalScrollTop(); top.ItemIx != 0 {
		t.Fatalf("outer top %+v", top)
	}
}

// U:text/state.rs:3125-3136
func TestTextOutsideEveryBlockRevealsItsBlock(t *testing.T) {
	ta := textTestApp(t)
	markdown := revealParagraphs(100) + "\n\n<div>html text</div>"
	state, cx := revealWindow(t, ta, markdown, revealContainer{kind: "scrollable"})
	revealNeedle(t, ta, cx, state, "html text")
	if revealPending(ta, state) {
		t.Fatal("the reveal is still pending")
	}
	if top := revealScrollTop(ta, state); top.ItemIx < 90 {
		t.Fatalf("top %+v", top)
	}
}

// U:text/state.rs:3138-3158
func TestARevealFollowsItsTextPastAnEditBeforeIt(t *testing.T) {
	ta := textTestApp(t)
	state, cx := revealWindow(t, ta, revealParagraphs(200), revealContainer{kind: "scrollable"})
	// An inserted paragraph moves the target down one block.
	revealRequestNeedle(t, ta, state, "paragraph 150", func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		s.SetText("inserted\n\n"+revealParagraphs(200), cx)
		if s.pendingReveal == nil {
			t.Error("an edit before the target dropped the reveal")
		}
	})
	for range 3 {
		revealDraw(t, cx)
	}
	if revealPending(ta, state) {
		t.Fatal("the reveal is still pending")
	}
	if top := revealScrollTop(ta, state); top.ItemIx < 141 || top.ItemIx > 151 {
		t.Fatalf("top %+v", top)
	}
	// A reveal whose text changed is dropped.
	revealRequestNeedle(t, ta, state, "paragraph 150", func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		s.SetText(strings.Replace(revealParagraphs(200), "paragraph 150", "changed", 1), cx)
		if s.pendingReveal != nil {
			t.Error("a changed target kept the reveal")
		}
	})
}

// U:text/state.rs:3160-3185
func TestMalformedRangesAndHTMLViewsAreRejected(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "first\n\nsecond")
	ta.RunUntilParked()
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		for _, r := range []gpui.TextRange{{Start: 5, End: 3}, mdRange(0, 100)} {
			if err := s.RevealRange(r, cx); err != (RangeHighlightError{Kind: RangeHighlightInvalidRange, Index: 0}) {
				t.Errorf("%v: %v", r, err)
			}
		}
		if err := s.RevealRange(mdRange(0, 5), cx); err != nil {
			t.Error(err)
		}
	})
	html := newTextViewStateEntityHTML(ta, "<p>one</p>")
	ta.RunUntilParked()
	updateState(ta, html, func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		if err := s.RevealRange(mdRange(0, 3), cx); err != (RangeHighlightError{Kind: RangeHighlightUnsupported}) {
			t.Errorf("html: %v", err)
		}
	})
}
