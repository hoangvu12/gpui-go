package gpui

import (
	"errors"
	"math"
	"testing"
)

// Base ticket 43: a StyledText under a padded, full-width box in a flex
// row failed the frame with "StyledText shaping failed ... wrap width must
// be finite and >= 0". Both tests shape through the real text service
// (no window is shown).

// paddedTextView is Base's code block in a flex row: the row holds a
// column whose child is a padded, full-width, min-w-0 column around the
// text (U:text/node.rs:1914-1981; Base's text blocks are flex columns,
// base/text_node_render.go textBlockDiv).
type paddedTextView struct{}

// Render implements Render[paddedTextView].
func (v *paddedTextView) Render(w *Window, cx *Context[paddedTextView]) AnyElement {
	return Div().Flex().Child(Div().Flex().FlexCol().
		Child(Div().Flex().FlexCol().WFull().MinW(Px(0)).P(Px(12)).
			Child(NewStyledText("fn main() {\n    hi();\n}")))).
		IntoElement()
}

// The flex row's min-content pass sizes the padded box at zero width, so
// Taffy hands the text a known width of 0 - 24 = -24 (taffy 0.13
// flexbox.rs:474 does not clamp the content box). The pin passes that on
// as Some(-24) (R:elements/text.rs:809-827) and parley breaks at every
// opportunity; the port's native text service rejected it.
func TestStyledTextUnderPaddedBoxInFlexRowShapes(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 600, Height: 200})
	t.Cleanup(func() { CloseTestWindow(window) })
	text, err := windowTextSystemOf()
	if err != nil {
		t.Fatalf("real text system: %v", err)
	}
	drawState(window).text = text
	view := NewEntity(app, window.Scope(), func(*paddedTextView, *Context[paddedTextView]) {})
	window.SetRootView(ViewOf(view))
	for frame := range 2 {
		if _, err := DrawWindowFrame(window); err != nil {
			t.Fatalf("frame %d: DrawWindowFrame: %v", frame, err)
		}
	}
}

// A negative wrap width shapes like a zero one (gpui/text.go
// nativeWrapWidth); a non-finite one is still refused.
func TestShapeTextNegativeWrapWidthWrapsLikeZero(t *testing.T) {
	system, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	shape := func(wrap float32) TextLayoutSummary {
		t.Helper()
		const text = "fn main() { hi(); }"
		line, err := system.ShapeText(TextLayoutRequest{
			Text: text, FontSize: 16, Runs: []TextRun{DefaultTextStyle().ToRun(len(text))}, WrapWidth: &wrap,
		})
		if err != nil {
			t.Fatalf("ShapeText(wrap %v): %v", wrap, err)
		}
		defer func() { _ = line.Dispose() }()
		summary, err := line.Summary()
		if err != nil {
			t.Fatalf("Summary: %v", err)
		}
		if wrap < 0 {
			// Relayout takes the same widths.
			if err := line.Relayout(&wrap, nil); err != nil {
				t.Fatalf("Relayout(wrap %v): %v", wrap, err)
			}
		}
		return summary
	}
	zero, negative := shape(0), shape(-24)
	if negative.LineCount != zero.LineCount || negative.Width != zero.Width {
		t.Errorf("wrap -24: %d lines %v wide, want wrap 0's %d lines %v wide",
			negative.LineCount, negative.Width, zero.LineCount, zero.Width)
	}
	if zero.LineCount < 4 {
		t.Errorf("wrap 0: %d lines, want one per word", zero.LineCount)
	}
	nan := float32(math.NaN())
	if _, err := system.ShapeText(TextLayoutRequest{
		Text: "x", FontSize: 16, Runs: []TextRun{DefaultTextStyle().ToRun(1)}, WrapWidth: &nan,
	}); !errors.Is(err, ErrTextValue) {
		t.Errorf("wrap NaN: %v, want ErrTextValue", err)
	}
}
