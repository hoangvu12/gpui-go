//go:build windows

package inlinespec

import (
	"runtime"
	"testing"

	"gpui-go/gpui"
)

// realMixedView is the mixed-font-size corpus: normal text, an Inline
// container refining the text size to 1.5rem for its child (a larger
// font segment in the same paragraph), and trailing normal text.
type realMixedView struct{}

// Render implements gpui.Render[realMixedView].
func (v *realMixedView) Render(w *gpui.Window, cx *gpui.Context[realMixedView]) gpui.AnyElement {
	return gpui.Div().
		Block().
		W(gpui.PxLength(200)).
		Child("small ").
		Child(gpui.Div().Inline().TextSize(gpui.RemsOf(1.5)).ID("big").Child("big")).
		Child(" tail").
		IntoElement()
}

// TestRealInlineMixedFontSizes checks the mixed-style document: the
// inline container's refined text size publishes a larger font segment
// in the same paragraph, and the shared row grows to the larger
// segment's line height (the reference run-metrics model: per-piece
// ascent/descent plus the piece's line height).
func TestRealInlineMixedFontSizes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Fatalf("the real inline slice requires the Windows host")
	}
	_, host, window := bootInlineRealWindow(t, "inlinespec real mixed", 260, 200, func(w *gpui.Window, app *gpui.App) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *realMixedView, cx *gpui.Context[realMixedView]) {}))
	})

	facts := drawInlineRealFrame(t, host, window)
	if len(facts) != 1 {
		t.Fatalf("inline paragraphs = %d, want 1 (the mixed corpus is one paragraph)", len(facts))
	}
	p := facts[0]
	if p.Text != "small big tail" {
		t.Fatalf("paragraph text = %q, want %q", p.Text, "small big tail")
	}
	// One row carrying both sizes: the 1.5rem (24px) segment's line
	// height (~39px) grows the row beyond the ambient 26px.
	if len(p.Rows) != 1 {
		t.Fatalf("rows = %d, want 1 (the corpus fits one row)", len(p.Rows))
	}
	if row := p.Rows[0]; row.Size.Height < 39 {
		t.Fatalf("row height = %v, want >= 39 (the 24px segment's line height)", row.Size.Height)
	}
	// The paragraph height follows the row.
	if p.Size.Height < 39 {
		t.Fatalf("paragraph height = %v, want >= 39", p.Size.Height)
	}
}
