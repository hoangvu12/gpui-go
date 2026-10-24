package gpui

// browser-core ticket 32 (spec D2): text min-content wraps at every
// break opportunity, so text in a flex row shrinks and wraps instead of
// overflowing, and the text measure cache is keyed by sizing mode.
//
// Chromium's rule (the research checkout, %TEMP%\research-src\chromium):
// InlineNode::ComputeMinMaxSizes computes the min-content by breaking
// lines with zero available inline size — "the max of inline sizes of
// all line boxes with 0 available inline size. This gives the
// min-content, the width where lines wrap at every break opportunity"
// (core/layout/inline/inline_node.cc:2314-2323; ComputeContentSize's
// kMinContent arm returns LayoutUnit() at 1966-1967 and takes the
// per-line max at 2265). Min-content is the widest unbreakable segment.
//
// These tests shape through the REAL text service (no window is shown),
// like styled_text_wrap_test.go, and drive each measured text path —
// the Text element's measured leaf, the inline paragraph of a Block
// div, and StyledText — through one frame with a probe element that
// measures its child under min-content and then max-content available
// space on the SAME requested tree. The second measurement is the
// spec's cache risk: a max-content query following a min-content query
// must return the unwrapped size, never the wrapped (min-content) size
// the element cached for the first query.

import (
	"fmt"
	"testing"
)

// mincontentText is the probe body: five 11-13 char words, so shaping
// with wrap width 0 puts each on its own visual line and the widest
// (the min-content) is unambiguous.
const mincontentText = "consequences determination possibilities extraordinary architecture"

// measureProbe is an element whose request_layout measures its child
// under two available spaces (inside the frame, with the engine idle —
// the same standalone computes the inline paragraph collector runs for
// atomic children). The child is requested ONCE; the second
// LayoutAsRoot only recomputes, so the child's measure cache (the
// shapedCell / the paragraph measurement / the TextLayout inner) sees
// the min-content query and then the max-content query in sequence —
// exactly taffy's per-item order (flexbox.rs: the base-size query, then
// the automatic-minimum-size query, then the definite final pass).
//
// The probe also records the ambient text style facts the child
// measured under (style, font size, pixel-snapped line height, frame
// scale), so the test can shape the same text through the text system
// and derive the expected sizes outside the frame.
type measureProbe struct {
	child      AnyElement
	ambient    TextStyle
	fontSize   float32
	lineHeight float32
	scale      float32
	minSize    Size
	maxSize    Size
}

// ID implements Element.
func (p *measureProbe) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element.
func (p *measureProbe) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element: record the ambient text style,
// measure the child under min-content then max-content available space,
// then request this element's own (empty) node.
func (p *measureProbe) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	p.ambient = WindowTextStyle(w)
	// Browser-parity D1: the default style's CSS `normal` line height
	// resolves through the window's text seam (the modeled test face).
	p.lineHeight = ResolveLineHeight(w, p.ambient)
	p.fontSize = p.ambient.FontSizePixels(RemSize(w))
	p.scale = currentFrame(w).scale
	p.minSize = p.child.LayoutAsRoot(AvailableSize{
		Width:  MinContentAvailableSpace(),
		Height: MinContentAvailableSpace(),
	}, w, app)
	p.maxSize = p.child.LayoutAsRoot(AvailableSize{
		Width:  MaxContentAvailableSpace(),
		Height: MaxContentAvailableSpace(),
	}, w, app)
	id, err := requestLayoutNode(w, nil, DefaultStyle(), nil)
	if err != nil {
		panic(fmt.Sprintf("gpui: measure probe request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements Element.
func (p *measureProbe) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) struct{} {
	return struct{}{}
}

// Paint implements Element.
func (p *measureProbe) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *struct{}, w *Window, app *App) {
}

// mincontentProbeView carries the probe as the window's root view.
type mincontentProbeView struct {
	probe *measureProbe
}

// Render implements Render[mincontentProbeView].
func (v *mincontentProbeView) Render(w *Window, cx *Context[mincontentProbeView]) AnyElement {
	return CustomElement[struct{}, struct{}](v.probe)
}

// expectations shapes the probe body directly through the text system
// under the ambient style the probe recorded and reports the sizes the
// measured paths must return: the min-content size (the wrap-0
// shaping's widest line and line count) and the max-content size (the
// unwrapped shaping), snapped the way the layout adapter snaps measured
// sizes (ceil to a whole device pixel at the frame's scale).
func (p *measureProbe) expectations(t *testing.T) (min, max Size) {
	t.Helper()
	system, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	shape := func(wrap *float32) (width float32, lines int) {
		t.Helper()
		line, err := system.ShapeText(TextLayoutRequest{
			Text:      mincontentText,
			FontSize:  p.fontSize,
			Runs:      []TextRun{p.ambient.ToRun(len(mincontentText))},
			WrapWidth: wrap,
		})
		if err != nil {
			t.Fatalf("ShapeText(wrap %v): %v", wrap, err)
		}
		defer func() { _ = line.Dispose() }()
		summary, err := line.Summary()
		if err != nil {
			t.Fatalf("Summary: %v", err)
		}
		return summary.Width, summary.LineCount
	}
	// Wrap width 0: every break opportunity breaks, so the shaping's
	// widest line is the longest unbreakable segment (the min-content)
	// and its line count is the segment count.
	zero := float32(0)
	minWidth, minLines := shape(&zero)
	maxWidth, maxLines := shape(nil)
	min = Size{
		Width:  snapMeasuredSize(minWidth, p.scale) / p.scale,
		Height: snapMeasuredSize(p.lineHeight*float32(minLines), p.scale) / p.scale,
	}
	max = Size{
		Width:  snapMeasuredSize(maxWidth, p.scale) / p.scale,
		Height: snapMeasuredSize(p.lineHeight*float32(maxLines), p.scale) / p.scale,
	}
	return min, max
}

// runMincontentProbe drives one draw frame with the probe measuring the
// given child and returns the probe.
func runMincontentProbe(t *testing.T, child AnyElement) *measureProbe {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 600, Height: 400})
	t.Cleanup(func() { CloseTestWindow(window) })
	text, err := windowTextSystemOf()
	if err != nil {
		t.Fatalf("real text system: %v", err)
	}
	drawState(window).text = text
	probe := &measureProbe{child: child}
	view := NewEntity(app, window.Scope(), func(v *mincontentProbeView, _ *Context[mincontentProbeView]) {
		v.probe = probe
	})
	window.SetRootView(ViewOf(view))
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	return probe
}

// assertMinContent checks one path's recorded sizes against the shaped
// expectations: the min-content query wraps at every break opportunity,
// and the max-content query that follows still returns the unwrapped
// size (the cache-keying rule).
func assertMinContent(t *testing.T, probe *measureProbe) {
	t.Helper()
	wantMin, wantMax := probe.expectations(t)
	if probe.minSize.Width != wantMin.Width || probe.minSize.Height != wantMin.Height {
		t.Errorf("min-content size = %gx%g, want the longest word's %gx%g (wrap 0 shaping)",
			probe.minSize.Width, probe.minSize.Height, wantMin.Width, wantMin.Height)
	}
	if probe.maxSize.Width != wantMax.Width || probe.maxSize.Height != wantMax.Height {
		t.Errorf("max-content size after the min-content query = %gx%g, want the unwrapped %gx%g (the cache must not return the wrapped size)",
			probe.maxSize.Width, probe.maxSize.Height, wantMax.Width, wantMax.Height)
	}
}

// TestTextMinContentMeasureWrapsAtEveryBreakOpportunity pins D2 on the
// Text element's measured leaf: a min-content available width measures
// the text wrapped at every break opportunity (the longest word's
// width, one line per word), and the max-content query that follows on
// the same element still returns the unwrapped size — the measure cache
// is keyed by sizing mode, so the min-content shape never leaks into a
// max-content query (the spec risk).
func TestTextMinContentMeasureWrapsAtEveryBreakOpportunity(t *testing.T) {
	assertMinContent(t, runMincontentProbe(t, Text(mincontentText)))
}

// TestStyledTextMinContentMeasureWrapsAtEveryBreakOpportunity pins the
// same rule on StyledText's measure (the rich-text path).
func TestStyledTextMinContentMeasureWrapsAtEveryBreakOpportunity(t *testing.T) {
	assertMinContent(t, runMincontentProbe(t, NewStyledText(mincontentText).IntoElement()))
}

// TestParagraphMinContentMeasureWrapsAtEveryBreakOpportunity pins the
// same rule on the inline paragraph (a Block div's text child): the
// paragraph measure wraps at every break opportunity under a
// min-content available width, and the max-content query that follows
// still lays the paragraph out unwrapped (the measurement cache is
// keyed by the wrap width, nil against 0).
func TestParagraphMinContentMeasureWrapsAtEveryBreakOpportunity(t *testing.T) {
	child := Div().Block().Child(Text(mincontentText)).IntoElement()
	assertMinContent(t, runMincontentProbe(t, child))
}
