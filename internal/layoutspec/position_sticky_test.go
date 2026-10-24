//go:build windows

package layoutspec

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// TestPositionStickyRecordIsInFlowWithoutInsets pins the record
// translation of browser-core ticket 34's PositionSticky (spec L3):
// Taffy has no sticky value, so styleToRecord maps it onto the in-flow
// Relative and zeroes the inset (the insets are sticky constraints
// resolved at prepaint against the scrollport, gpui/sticky.go — they
// must not act as relative offsets, or the flow position the
// constraints clamp from would be shifted). At the ENGINE level the
// sticky offset is the caller's (the window layer's prepaint); what
// the engine must guarantee is only that a sticky node is an ordinary
// in-flow item whose insets do not offset it.
//
// The root is a 100x80 flex column with border 6: its content box
// origin is (6, 6). The sticky child carries a 30px top inset and a
// 40px left inset, which must have no effect (a relative child with
// the same insets would sit at (46, 36)); the sticky child sits at the
// flow position (6, 6), like the static one of ticket 33's test.
func TestPositionStickyRecordIsInFlowWithoutInsets(t *testing.T) {
	engine := newEngine(t)
	ctx := gpui.NewTestLayoutContext() // scale 2

	stickyStyle := gpui.DefaultStyle()
	stickyStyle.Position = gpui.PositionSticky
	stickyStyle.Size = gpui.LengthSize{Width: gpui.PxLength(60), Height: gpui.PxLength(50)}
	stickyStyle.Inset = gpui.LengthEdges{
		Top:  gpui.PxLength(30),
		Left: gpui.PxLength(40),
	}

	sticky, err := engine.RequestLayout(ctx, stickyStyle)
	if err != nil {
		t.Fatalf("RequestLayout(sticky): %v", err)
	}
	rootStyle := gpui.DefaultStyle()
	rootStyle.Size = gpui.LengthSize{Width: gpui.PxLength(100), Height: gpui.PxLength(80)}
	rootStyle.BorderWidths = gpui.AbsoluteLengthEdges{
		Top: gpui.Px(6), Right: gpui.Px(6), Bottom: gpui.Px(6), Left: gpui.Px(6),
	}
	root, err := engine.RequestLayout(ctx, rootStyle, sticky)
	if err != nil {
		t.Fatalf("RequestLayout(root): %v", err)
	}
	computeDefinite(t, engine, ctx, root, 100, 80)

	stickyBounds, err := engine.LayoutBounds(ctx, sticky)
	if err != nil {
		t.Fatalf("LayoutBounds(sticky): %v", err)
	}
	if stickyBounds.Origin.X != 6 || stickyBounds.Origin.Y != 6 ||
		stickyBounds.Size.Width != 60 || stickyBounds.Size.Height != 50 {
		t.Errorf("sticky node bounds = %v, want origin (6,6) size 60x50 (in flow, insets zeroed in the record; a relative node with the same insets would sit at (46,36))", stickyBounds)
	}
}
