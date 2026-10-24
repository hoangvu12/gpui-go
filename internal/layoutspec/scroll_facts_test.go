//go:build windows

// The browser-core ticket 07 layout gates: the scroll facts the engine
// derives from the layout record — the content size (descendant
// overflow plus the end padding), the scroll range (the padding-box
// basis plus the in-flow children's trailing margins), the resolved
// padding's percentage basis (the containing block's width) and
// Chromium's border-width floor.
package layoutspec

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// scrollRangeFixture builds the ticket's range fixture at the engine
// level: a 640x400 root (flex row) holding a 240x300 scroller (flex
// column, overflow-y scroll) whose padding-top is 10% and whose
// children are a 40px row, a 60px box with a 200px overflowing child, a
// 100px spacer and a 50px row with a 40px bottom margin.
func scrollRangeFixture(t *testing.T) (*gpui.LayoutEngine, *gpui.LayoutContext, gpui.LayoutID) {
	t.Helper()
	engine := newEngine(t)
	ctx := gpui.NewTestLayoutContext() // scale 2, rem 16

	scrollerStyle := gpui.DefaultStyle()
	scrollerStyle.Display = gpui.DisplayFlex
	scrollerStyle.FlexDirection = gpui.FlexDirectionColumn
	scrollerStyle.OverflowY = gpui.OverflowScroll
	scrollerStyle.Size = gpui.LengthSize{Width: gpui.PxLength(240), Height: gpui.PxLength(300)}
	scrollerStyle.Padding = gpui.DefiniteLengthEdges{
		Top:    gpui.Fraction(0.10),
		Bottom: gpui.DefinitePx(24),
	}

	focus := boxNode(t, engine, 40)
	tall := boxNode(t, engine, 200)
	nested := boxNode(t, engine, 60, tall)
	spacer := boxNode(t, engine, 100)
	lastStyle := gpui.DefaultStyle()
	lastStyle.Display = gpui.DisplayFlex
	lastStyle.FlexShrink = 0
	lastStyle.Size = gpui.LengthSize{Width: gpui.PxLength(240), Height: gpui.PxLength(50)}
	lastStyle.Margin = gpui.LengthEdges{Bottom: gpui.PxLength(40)}
	last, err := engine.RequestLayout(ctx, lastStyle)
	if err != nil {
		t.Fatalf("RequestLayout(last): %v", err)
	}

	scroller, err := engine.RequestLayout(ctx, scrollerStyle, focus, nested, spacer, last)
	if err != nil {
		t.Fatalf("RequestLayout(scroller): %v", err)
	}
	rootStyle := gpui.DefaultStyle()
	rootStyle.Display = gpui.DisplayFlex
	itemsStart := gpui.AlignItemsFlexStart
	rootStyle.AlignItems = &itemsStart
	rootStyle.Size = gpui.LengthSize{Width: gpui.PxLength(640), Height: gpui.PxLength(400)}
	root, err := engine.RequestLayout(ctx, rootStyle, scroller)
	if err != nil {
		t.Fatalf("RequestLayout(root): %v", err)
	}
	computeDefinite(t, engine, ctx, root, 640, 400)
	return engine, ctx, scroller
}

// boxNode requests one plain 240-wide flex box of the given height
// (with optional children).
func boxNode(t *testing.T, engine *gpui.LayoutEngine, height float32, children ...gpui.LayoutID) gpui.LayoutID {
	t.Helper()
	ctx := gpui.NewTestLayoutContext()
	style := gpui.DefaultStyle()
	style.Display = gpui.DisplayFlex
	style.FlexDirection = gpui.FlexDirectionColumn
	itemsStart := gpui.AlignItemsFlexStart
	style.AlignItems = &itemsStart
	style.FlexShrink = 0
	style.Size = gpui.LengthSize{Width: gpui.PxLength(240), Height: gpui.PxLength(height)}
	id, err := engine.RequestLayout(ctx, style, children...)
	if err != nil {
		t.Fatalf("RequestLayout(box %v): %v", height, err)
	}
	return id
}

// TestScrollFactsCoverDescendantOverflowEndPaddingAndMargins pins the
// engine-derived scroll range (browser-core 07): the content size is
// Taffy's — the descendants' boxes (the nested box's overflowing child
// reaches 314) plus the 24px end padding = 338 — and the range is that
// less the 300px border box plus the last child's 40px trailing margin
// = 78, exactly what Chrome's scrollable overflow computes
// (ScrollableOverflowCalculator::Result plus the inflow bounds' end
// margin). The old pin's element-tree union (250) plus its own-height
// percentage padding would have given 4.
func TestScrollFactsCoverDescendantOverflowEndPaddingAndMargins(t *testing.T) {
	engine, ctx, scroller := scrollRangeFixture(t)
	facts, err := engine.ScrollLayoutFacts(ctx, scroller)
	if err != nil {
		t.Fatalf("ScrollLayoutFacts: %v", err)
	}
	if got, want := facts.ContentSize.Height, float32(338); got != want {
		t.Errorf("content height = %v, want %v (the union 314 plus the 24px end padding)", got, want)
	}
	if got, want := facts.ContentSize.Width, float32(240); got != want {
		t.Errorf("content width = %v, want %v (the children's width, no inline padding)", got, want)
	}
	if got, want := facts.MaxOffset.Y, float32(78); got != want {
		t.Errorf("max scroll y = %v, want %v (338 - 300 + the 40px trailing margin)", got, want)
	}
	if got, want := facts.MaxOffset.X, float32(0); got != want {
		t.Errorf("max scroll x = %v, want %v (the content does not overflow inline)", got, want)
	}
}

// TestScrollFactsResolvePaddingAgainstContainingBlockWidth pins the
// percentage basis (browser-core 07): the scroller's 10% top padding
// resolves against the CONTAINING BLOCK's width (the 640px root
// content width — 64px), not the scroller's own height (30px), exactly
// as Taffy lays it out (compute/leaf.rs:19-21: "both horizontal and
// vertical percentage padding are resolved against the container's
// inline size").
func TestScrollFactsResolvePaddingAgainstContainingBlockWidth(t *testing.T) {
	engine, ctx, scroller := scrollRangeFixture(t)
	facts, err := engine.ScrollLayoutFacts(ctx, scroller)
	if err != nil {
		t.Fatalf("ScrollLayoutFacts: %v", err)
	}
	if got, want := facts.Padding.Top, float32(64); got != want {
		t.Errorf("padding top = %v, want %v (10%% of the containing block's 640px width, not the 300px height)", got, want)
	}
	if got, want := facts.Padding.Bottom, float32(24); got != want {
		t.Errorf("padding bottom = %v, want %v", got, want)
	}
	padding, err := engine.ResolvedPadding(ctx, scroller)
	if err != nil {
		t.Fatalf("ResolvedPadding: %v", err)
	}
	if padding != facts.Padding {
		t.Errorf("ResolvedPadding %v != ScrollLayoutFacts.Padding %v", padding, facts.Padding)
	}
}

// TestBorderWidthFloorInsetsTheCSSPixel pins Chromium's border rule at
// the engine level (browser-core 07): a 1.3px border is floored to
// 1 CSS px (ClampLineWidth, style_builder_converter.cc:1971-1987), so
// at the test scale 2.0 the child insets by exactly 1 logical pixel
// where the pin's round-to-nearest-device rule would inset 1.5.
func TestBorderWidthFloorInsetsTheCSSPixel(t *testing.T) {
	engine := newEngine(t)
	ctx := gpui.NewTestLayoutContext() // scale 2

	inner := boxNode(t, engine, 100)
	boxed := gpui.DefaultStyle()
	boxed.Display = gpui.DisplayFlex
	boxed.Size = gpui.LengthSize{Width: gpui.PxLength(200), Height: gpui.PxLength(152)}
	boxed.BorderWidths = gpui.AbsoluteLengthEdges{Top: gpui.Px(1.3), Right: gpui.Px(1.3), Bottom: gpui.Px(1.3), Left: gpui.Px(1.3)}
	node, err := engine.RequestLayout(ctx, boxed, inner)
	if err != nil {
		t.Fatalf("RequestLayout: %v", err)
	}
	rootStyle := gpui.DefaultStyle()
	rootStyle.Display = gpui.DisplayFlex
	rootStyle.Size = gpui.LengthSize{Width: gpui.PxLength(320), Height: gpui.PxLength(240)}
	root, err := engine.RequestLayout(ctx, rootStyle, node)
	if err != nil {
		t.Fatalf("RequestLayout(root): %v", err)
	}
	computeDefinite(t, engine, ctx, root, 320, 240)
	bounds, err := engine.LayoutBounds(ctx, inner)
	if err != nil {
		t.Fatalf("LayoutBounds: %v", err)
	}
	// The child sits at the boxed node's border-box origin plus the
	// floored 1px border (the root places the boxed node at 0,0).
	if got, want := bounds.Origin.X, float32(1); got != want {
		t.Errorf("child x = %v, want %v (the 1.3px border floors to 1 CSS px)", got, want)
	}
}
