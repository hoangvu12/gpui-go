//go:build windows

package base

// Real-window slice of ticket 37: the 11 U:text/state.rs reveal_range
// tests (2645-3186) ticket 30 could not port. Each needs a block laid out
// on many rows, which gpui's TestApp text system cannot produce (it lays a
// document out on one visual line, see text_range_highlight_reveal_test.go),
// and most need an enclosing list to scroll to a line inside one item,
// through Window.RequestAutoscroll (gpui/window.go, ticket 37). So they run
// on the machine's fonts in a real window, in the 200 x 100 frame of
// upstream's Root (U:text/state.rs:2654-2720). Environment expectation:
// this session creates real windows; failures fail, never skip.
//
// Deviation common to all: in a real window the frame loop also draws
// while a reveal is pending (revealFrame requests an animation frame), so
// a reveal may progress between the frames the test draws itself. The
// "draw three frames" steps stand; frames the loop adds can only carry a
// reveal further, and the "unmoved" checks still hold. See
// TestRealARevealThatCannotBeShownGivesUp for the one test this changes.

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// realRevealRoot is upstream's Root (U:text/state.rs:2654-2720): the view
// in a 200 x 100 frame, in the container kind names ("scrollable", "list",
// "div", "fixed").
type realRevealRoot struct {
	state  gpui.Entity[TextViewState]
	kind   string
	outer  *gpui.ListState
	handle gpui.ScrollHandle
	// hidden counts the frames a "fixed" view's line was laid out hidden
	// (its OnReveal calls); nil adds no handler.
	hidden *int
}

func (r *realRevealRoot) Render(w *gpui.Window, cx *gpui.Context[realRevealRoot]) gpui.AnyElement {
	// Upstream's div() is Block (see text_view.go, Gap 2b); gpui-go's Div()
	// is not, so the frame and the scrolling div say so, or a fit-content
	// view would be laid out at its max-content width, on one row.
	frame := gpui.Div().Block().W(gpui.Px(200)).H(gpui.Px(100))
	state := r.state
	switch r.kind {
	case "scrollable":
		return frame.Child(NewTextView(state).Scrollable(true)).IntoElement()
	case "list":
		return frame.Child(gpui.List(r.outer, func(ix int, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
			if ix == 0 {
				return gpui.Div().Flex().H(gpui.Px(40)).IntoElement()
			}
			return NewTextView(state).IntoElement()
		}).SizeFull()).IntoElement()
	case "div":
		scroll := r.handle
		return frame.Child(gpui.Div().Block().ID("scroll").SizeFull().OverflowYScroll().TrackScroll(scroll).
			Child(NewTextView(state).OnReveal(func(line gpui.Bounds, _ *gpui.Window, _ *gpui.App) {
				viewport := scroll.Bounds()
				offset := scroll.Offset()
				if line.Bottom() > viewport.Bottom() {
					offset.Y -= line.Bottom() - viewport.Bottom()
				} else if line.Top() < viewport.Top() {
					offset.Y += viewport.Top() - line.Top()
				}
				scroll.SetOffset(offset)
			}))).IntoElement()
	}
	view := NewTextView(state)
	if hidden := r.hidden; hidden != nil {
		view = view.OnReveal(func(gpui.Bounds, *gpui.Window, *gpui.App) { *hidden++ })
	}
	return frame.Child(view).IntoElement()
}

// realReveal is a real window showing a TextView of markdown in root's
// container (upstream's window(), U:text/state.rs:2722-2734).
type realReveal struct {
	t      *testing.T
	app    *gpui.App
	host   *gpui.Host
	window *gpui.Window
	state  gpui.Entity[TextViewState]
}

func openRealReveal(t *testing.T, markdown string, root realRevealRoot) *realReveal {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Fatalf("the real-window slice requires the Windows host")
	}
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatalf("attach: %v", err)
	}
	if err := host.Start(); err != nil {
		t.Fatalf("host start: %v", err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	t.Cleanup(func() {
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		<-runDone
	})
	f := &realReveal{t: t, app: app, host: host}
	app.Update(func(a *gpui.App) { Init(a) })
	var openErr error
	f.onHost(func() {
		f.window, openErr = app.OpenWindowView(gpui.WindowOptions{
			Title: "base text reveal", Show: true, Resizable: true,
			Bounds: gpui.Bounds{Size: gpui.Size{Width: 320, Height: 240}},
		}, func(w *gpui.Window, app *gpui.App) gpui.View {
			f.state = gpui.NewEntity(app, w.Scope(), func(s *TextViewState, cx *gpui.Context[TextViewState]) {
				*s = TextViewStateMarkdown(markdown, cx)
			})
			root.state = f.state
			return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *realRevealRoot, _ *gpui.Context[realRevealRoot]) { *v = root }))
		})
	})
	if openErr != nil {
		t.Fatalf("app.OpenWindow: %v — this session is expected to create real windows", openErr)
	}
	window := f.window
	t.Cleanup(func() {
		if window.Alive() {
			window.Close()
			deadline := time.Now().Add(10 * time.Second)
			for window.Alive() && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
		}
	})
	f.waitParsed()
	f.draw()
	return f
}

func (f *realReveal) onHost(g func()) {
	f.t.Helper()
	done := make(chan struct{})
	if !f.host.Post(func() { defer close(done); g() }) {
		f.t.Fatalf("foreground post was dropped: the host loop exited")
	}
	<-done
}

// draw draws one frame (upstream's draw(), U:text/state.rs:2736-2738).
func (f *realReveal) draw() {
	f.t.Helper()
	var err error
	f.onHost(func() { _, err = gpui.DrawWindowFrame(f.window) })
	if err != nil {
		f.t.Fatalf("DrawWindowFrame: %v", err)
	}
}

// waitParsed is cx.run_until_parked() for the view's parse: it waits until
// the last update to the text has landed.
func (f *realReveal) waitParsed() {
	f.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		parsed := realRevealRead(f, func(s *TextViewState) bool { return s.committedRevision == s.revision })
		if parsed {
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("the text did not parse in time")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func realRevealRead[R any](f *realReveal, read func(*TextViewState) R) R {
	var out R
	f.app.Update(func(a *gpui.App) {
		out = f.state.Read(a, func(s *TextViewState, _ *gpui.App) R { return read(s) })
	})
	return out
}

func (f *realReveal) update(g func(*TextViewState, *gpui.Context[TextViewState])) {
	f.app.Update(func(a *gpui.App) { f.state.Update(a, g) })
}

// revealAt reveals the range of the current text rng picks and draws a
// few frames (U:text/state.rs:2942-2955, and reveal(), 2762-2768).
func (f *realReveal) revealAt(rng func(text string) gpui.TextRange) {
	f.t.Helper()
	var err error
	f.update(func(s *TextViewState, cx *gpui.Context[TextViewState]) {
		err = s.RevealRange(rng(s.RenderedText().String()), cx)
	})
	if err != nil {
		f.t.Fatalf("RevealRange: %v", err)
	}
	for range 3 {
		f.draw()
	}
}

// reveal reveals the first occurrence of needle (U:text/state.rs:2762-2768).
func (f *realReveal) reveal(needle string) {
	f.t.Helper()
	f.revealAt(func(text string) gpui.TextRange {
		start := strings.Index(text, needle)
		if start < 0 {
			f.t.Errorf("%q is not in the rendered text", needle)
			start = 0
		}
		return mdRange(start, start+len(needle))
	})
}

func (f *realReveal) pending() bool {
	return realRevealRead(f, func(s *TextViewState) bool { return s.pendingReveal != nil })
}

func (f *realReveal) scrollTop() gpui.ListOffset {
	return realRevealRead(f, func(s *TextViewState) gpui.ListOffset { return s.listState.LogicalScrollTop() })
}

// assertUnmoved is U:text/state.rs:2957-2968.
func (f *realReveal) assertUnmoved(top gpui.ListOffset, what string) {
	f.t.Helper()
	if f.pending() {
		f.t.Fatalf("%s: the reveal is still pending", what)
	}
	if now := f.scrollTop(); now != top {
		f.t.Fatalf("%s: scrolled from %+v to %+v", what, top, now)
	}
}

func realRevealCode() string {
	lines := make([]string, 200)
	for ix := range lines {
		lines[ix] = fmt.Sprintf("line %d", ix)
	}
	return "```\n" + strings.Join(lines, "\n") + "\n```"
}

// U:text/state.rs:2825-2833
func TestRealAScrollableViewScrollsToALineInsideALongParagraph(t *testing.T) {
	f := openRealReveal(t, revealWords(400), realRevealRoot{kind: "scrollable"})
	f.reveal("w390")
	if f.pending() {
		t.Fatal("the reveal is still pending")
	}
	if top := f.scrollTop(); top.ItemIx != 0 || top.OffsetInItem <= 100 {
		t.Fatalf("top %+v", top)
	}
}

// U:text/state.rs:2835-2851
func TestRealRevealingAVisibleLineDoesNotScroll(t *testing.T) {
	f := openRealReveal(t, revealWords(400), realRevealRoot{kind: "scrollable"})
	f.reveal("w200")
	top := f.scrollTop()
	if top.OffsetInItem <= 0 {
		t.Fatalf("top %+v", top)
	}
	for _, word := range []string{"w199", "w198", "w197", "w196"} {
		f.reveal(word)
		f.assertUnmoved(top, word)
	}
}

// U:text/state.rs:2853-2862
func TestRealAnEnclosingListScrollsToALineOfAFitContentView(t *testing.T) {
	outer := gpui.NewListState(2, 1000)
	f := openRealReveal(t, revealWords(400), realRevealRoot{kind: "list", outer: outer})
	f.reveal("w390")
	if f.pending() {
		t.Fatal("the reveal is still pending")
	}
	var top gpui.ListOffset
	f.onHost(func() { top = outer.LogicalScrollTop() })
	if top.ItemIx != 1 || top.OffsetInItem <= 100 {
		t.Fatalf("outer top %+v", top)
	}
}

// U:text/state.rs:2864-2871
func TestRealOnRevealScrollsAContainerThatIgnoresScrollRequests(t *testing.T) {
	handle := gpui.NewScrollHandle()
	f := openRealReveal(t, revealWords(400), realRevealRoot{kind: "div", handle: handle})
	f.reveal("w390")
	if f.pending() {
		t.Fatal("the reveal is still pending")
	}
	var offset gpui.Point
	f.onHost(func() { offset = handle.Offset() })
	if offset.Y >= -100 {
		t.Fatalf("offset %v", offset)
	}
}

// U:text/state.rs:2873-2882 (deviated). Upstream draws three frames, sees
// the reveal still pending, draws ten more and sees it given up. Here the
// frame loop keeps drawing while the reveal is pending (above), so the
// frame count is not the test's: it checks instead that the view tried in
// frames where the line was laid out hidden (an OnReveal handler that does
// not scroll counts them; upstream's fixed view has none), no more than
// the attempts allowed, and then gave up. And it reveals w390 of 400 words
// rather than w3990 of 4000: on real fonts a 4000-word paragraph takes
// seconds a frame to paint (gpui's per-row glyph paint), so the reveal
// would only ever give up on its one-second timeout; 400 words are as far
// out of a 100px frame.
func TestRealARevealThatCannotBeShownGivesUp(t *testing.T) {
	hidden := 0
	f := openRealReveal(t, revealWords(400), realRevealRoot{kind: "fixed", hidden: &hidden})
	f.reveal("w390")
	deadline := time.Now().Add(5 * time.Second)
	for f.pending() && time.Now().Before(deadline) {
		f.draw()
	}
	if f.pending() {
		t.Fatal("the reveal is still pending")
	}
	var tries int
	f.onHost(func() { tries = hidden })
	if tries < 1 || tries > revealAttempts {
		t.Fatalf("the line was laid out hidden in %d frames, want 1..%d", tries, revealAttempts)
	}
	t.Logf("gave up after %d hidden frames", tries)
}

// U:text/state.rs:2918-2940
func TestRealAPositionAfterTheLastCharacterRevealsItsLine(t *testing.T) {
	f := openRealReveal(t, revealWords(400), realRevealRoot{kind: "scrollable"})
	f.reveal("w399")
	top := f.scrollTop()
	if top.OffsetInItem <= 1000 {
		t.Fatalf("top %+v", top)
	}
	// The end of the text, on the visible last line.
	f.revealAt(func(text string) gpui.TextRange {
		end := strings.Index(text, "w399") + len("w399")
		return mdRange(end, end)
	})
	f.assertUnmoved(top, "the end of w399")
}

// U:text/state.rs:2970-3000
func TestRealTheEndOfTheTextAndItsSeparatorsRevealTheLastLine(t *testing.T) {
	f := openRealReveal(t, revealWords(400), realRevealRoot{kind: "scrollable"})
	f.reveal("w399")
	top := f.scrollTop()
	if top.OffsetInItem <= 1000 {
		t.Fatalf("top %+v", top)
	}
	// The end of the text, after the separator that ends the block.
	f.revealAt(func(text string) gpui.TextRange { return mdRange(len(text), len(text)) })
	f.assertUnmoved(top, "the end of the text")
	// Only that separator.
	f.revealAt(func(text string) gpui.TextRange { return mdRange(len(text)-1, len(text)) })
	f.assertUnmoved(top, "the separator")

	f.update(func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.SetText(realRevealCode(), cx) })
	f.waitParsed()
	f.reveal("line 199")
	top = f.scrollTop()
	if top.OffsetInItem <= 1000 {
		t.Fatalf("code top %+v", top)
	}
	f.revealAt(func(text string) gpui.TextRange { return mdRange(len(text), len(text)) })
	f.assertUnmoved(top, "the end of the code")
}

// U:text/state.rs:3002-3021
func TestRealTheEndOfABlockAboveRevealsItsLastLine(t *testing.T) {
	second := make([]string, 400)
	for ix := range second {
		second[ix] = fmt.Sprintf("v%d", ix)
	}
	f := openRealReveal(t, revealWords(400)+"\n\n"+strings.Join(second, " "), realRevealRoot{kind: "scrollable"})
	f.reveal("v399")
	if top := f.scrollTop(); top.ItemIx != 1 {
		t.Fatalf("top %+v", top)
	}
	// The end of the first paragraph, on the separator after it.
	f.revealAt(func(text string) gpui.TextRange {
		end := strings.Index(text, "w399") + len("w399")
		return mdRange(end, end)
	})
	if f.pending() {
		t.Fatal("the reveal is still pending")
	}
	if top := f.scrollTop(); top.ItemIx != 0 || top.OffsetInItem <= 1000 {
		t.Fatalf("top %+v", top)
	}
}

// U:text/state.rs:3047-3066
func TestRealALineOfAnInlineFlowCountsAsShownOnceScrolledTo(t *testing.T) {
	// Inline code every tenth word, so the rows are taller than the body
	// line and land between pixels.
	words := make([]string, 400)
	for ix := range words {
		if ix%10 == 0 {
			words[ix] = fmt.Sprintf("`c%d`", ix)
		} else {
			words[ix] = fmt.Sprintf("w%d", ix)
		}
	}
	f := openRealReveal(t, strings.Join(words, " "), realRevealRoot{kind: "scrollable"})
	f.reveal("c390")
	if f.pending() {
		t.Fatal("the reveal is still pending")
	}
	if top := f.scrollTop(); top.OffsetInItem <= 1000 {
		t.Fatalf("top %+v", top)
	}
}

// U:text/state.rs:3068-3080
func TestRealARangeStartingOnALineBreakRevealsTheNextLine(t *testing.T) {
	f := openRealReveal(t, realRevealCode(), realRevealRoot{kind: "scrollable"})
	f.reveal("\nline 190")
	if f.pending() {
		t.Fatal("the reveal is still pending")
	}
	if top := f.scrollTop(); top.OffsetInItem <= 1000 {
		t.Fatalf("top %+v", top)
	}
}

// U:text/state.rs:3082-3094
func TestRealARangeStartingOnALineBreakInAnInlineFlowRevealsTheNextLine(t *testing.T) {
	// Inline code lays the paragraph out as an inline flow.
	lines := make([]string, 200)
	for ix := range lines {
		lines[ix] = fmt.Sprintf("line %d `code`", ix)
	}
	f := openRealReveal(t, strings.Join(lines, "\\\n"), realRevealRoot{kind: "scrollable"})
	f.reveal("\nline 190")
	if f.pending() {
		t.Fatal("the reveal is still pending")
	}
	if top := f.scrollTop(); top.OffsetInItem <= 1000 {
		t.Fatalf("top %+v", top)
	}
}
