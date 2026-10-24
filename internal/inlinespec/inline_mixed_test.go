//go:build windows

package inlinespec

import (
	"runtime"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
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
	// One row carrying both sizes: the 1.5rem (24px) segment grows the
	// row beyond the ambient 16px strut. Browser-parity D1: the default
	// line height is CSS `normal`, so the 24px segment contributes its
	// font's line spacing — the rounded metrics [−26, +6] (ascent
	// 25.8984 → 26, descent 6.0234 → 6, line gap 0) — and the row is
	// 32 tall with baseline 26, Chrome's line box for mixed normal
	// text (the 16px strut's [−17, +4] unites under it). The old
	// golden-ratio row (≥ 39) was the pre-D1 default.
	if len(p.Rows) != 1 {
		t.Fatalf("rows = %d, want 1 (the corpus fits one row)", len(p.Rows))
	}
	if row := p.Rows[0]; !f32eq(row.Size.Height, 32) {
		t.Fatalf("row height = %v, want 32 (the 24px segment's normal line spacing)", row.Size.Height)
	}
	if row := p.Rows[0]; !f32eq(row.Baseline, 26) {
		t.Fatalf("row baseline = %v, want 26 (the 24px segment's rounded ascent)", row.Baseline)
	}
	// The paragraph height follows the row.
	if !f32eq(p.Size.Height, 32) {
		t.Fatalf("paragraph height = %v, want 32", p.Size.Height)
	}
}
