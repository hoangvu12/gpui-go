package inlinespec

import (
	"testing"

	"gpui-go/authoring"
	"gpui-go/gpui"
)

// paddedView is the proportional-padding corpus: a baseline-aligned
// auto-sized inline-flex box whose refinement pads both horizontal
// edges around a 10x10 child, so its standalone atomic measurement is
// the content plus the padding.
type paddedView struct{}

// Render implements Render[paddedView].
func (v *paddedView) Render(w *gpui.Window, cx *gpui.Context[paddedView]) gpui.AnyElement {
	return gpui.Div().
		Block().
		W(gpui.PxLength(200)).
		Child("a").
		Child(gpui.Div().
			InlineFlex().
			AlignBaseline().
			DebugSelector("padded-box").
			Bg(gpui.Hsla{H: 0.45, S: 0.5, L: 0.5, A: 1}).
			Refine(gpui.StyleRefinement{
				PaddingLeft:  authoring.Some(authoring.Rem(0.375)),
				PaddingRight: authoring.Some(authoring.Rem(0.375)),
			}).
			Child(gpui.Div().Size(gpui.PxLength(10), gpui.PxLength(10)))).
		Child("b").
		IntoElement()
}

// TestInlinePaddedBoxMeasurementIncludesPadding checks the atomic box
// measurement of a padded auto-sized inline-flex box: the standalone
// compute under max-content sizes the box to its content plus the
// refined padding (the pinned border-box semantics make an explicit
// size include padding, so the auto-sized corpus isolates the padding
// contribution).
func TestInlinePaddedBoxMeasurementIncludesPadding(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 240, Height: 120})
	view := gpui.NewEntity(app, window.Scope(), func(v *paddedView, cx *gpui.Context[paddedView]) {})
	window.SetRootView(gpui.ViewOf(view))
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}

	facts := gpui.WindowInlineFacts(window)
	if len(facts) != 1 {
		t.Fatalf("inline paragraphs = %d, want 1", len(facts))
	}
	p := facts[0]
	if p.Text != "ab" {
		t.Fatalf("paragraph text = %q, want %q", p.Text, "ab")
	}
	if len(p.Boxes) != 1 {
		t.Fatalf("boxes = %d, want 1", len(p.Boxes))
	}
	box := p.Boxes[0]
	// 0.375rem = 6px on each side around the 10x10 content: the
	// standalone measurement is 10 + 12 = 22 wide, 10 tall.
	if !f32eq(box.Bounds.Size.Width, 22) {
		t.Fatalf("padded box width = %v, want 22 (10px content plus 6px padding on both sides)", box.Bounds.Size.Width)
	}
	if !f32eq(box.Bounds.Size.Height, 10) {
		t.Fatalf("padded box height = %v, want 10 (no vertical padding)", box.Bounds.Size.Height)
	}
	// The box sits after "a" (9.6) and the trailing "b" after the
	// padded extent.
	if !f32eq(box.Bounds.Origin.X, 9.6) {
		t.Fatalf("padded box x = %v, want 9.6 (after %q)", box.Bounds.Origin.X, "a")
	}
	placed, ok := gpui.WindowDebugBound(window, "padded-box")
	if !ok {
		t.Fatal("the padded box's placed bounds were not recorded")
	}
	if !f32eq(placed.Size.Width, 22) {
		t.Fatalf("padded box placed width = %v, want 22 (paint uses the padded placement)", placed.Size.Width)
	}
}
