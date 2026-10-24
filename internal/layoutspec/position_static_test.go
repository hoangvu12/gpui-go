//go:build windows

package layoutspec

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// TestPositionStaticRecordIsInFlowWithoutInsets pins the record
// translation of browser-core ticket 33's PositionStatic (spec D3):
// Taffy has no static value, so styleToRecord maps it onto the in-flow
// Relative and zeroes the inset (CSS static ignores
// top/right/bottom/left). At the ENGINE level the containing-block
// reparenting is the caller's (the window layer's scope walk in
// gpui/containingblock.go); what the engine must guarantee is only that
// a static node is an ordinary in-flow item whose insets do not offset
// it and whose absolute children still lay out against their direct
// parent when the caller attaches them there.
//
// The root is a 100x80 flex column with border 6: its content box
// origin is (6, 6). The static child carries a 30px top inset and a
// 40px left inset, which must have no effect (a relative child with the
// same insets would sit at (46, 36), the record's relative-offset
// behaviour); the static child sits at the flow position (6, 6), and
// its absolute child with left 10 / top 8 resolves against the static
// parent's border box (the direct parent, the engine-level attach),
// landing at (16, 14).
func TestPositionStaticRecordIsInFlowWithoutInsets(t *testing.T) {
	engine := newEngine(t)
	ctx := gpui.NewTestLayoutContext() // scale 2

	absStyle := gpui.DefaultStyle()
	absStyle.Position = gpui.PositionAbsolute
	absStyle.Size = gpui.LengthSize{Width: gpui.PxLength(40), Height: gpui.PxLength(30)}
	absStyle.Inset = gpui.LengthEdges{
		Top:  gpui.PxLength(8),
		Left: gpui.PxLength(10),
	}

	staticStyle := gpui.DefaultStyle()
	staticStyle.Position = gpui.PositionStatic
	staticStyle.Size = gpui.LengthSize{Width: gpui.PxLength(60), Height: gpui.PxLength(50)}
	staticStyle.Inset = gpui.LengthEdges{
		Top:  gpui.PxLength(30),
		Left: gpui.PxLength(40),
	}

	absolute, err := engine.RequestLayout(ctx, absStyle)
	if err != nil {
		t.Fatalf("RequestLayout(absolute): %v", err)
	}
	static, err := engine.RequestLayout(ctx, staticStyle, absolute)
	if err != nil {
		t.Fatalf("RequestLayout(static): %v", err)
	}
	rootStyle := gpui.DefaultStyle()
	rootStyle.Size = gpui.LengthSize{Width: gpui.PxLength(100), Height: gpui.PxLength(80)}
	rootStyle.BorderWidths = gpui.AbsoluteLengthEdges{
		Top: gpui.Px(6), Right: gpui.Px(6), Bottom: gpui.Px(6), Left: gpui.Px(6),
	}
	root, err := engine.RequestLayout(ctx, rootStyle, static)
	if err != nil {
		t.Fatalf("RequestLayout(root): %v", err)
	}
	computeDefinite(t, engine, ctx, root, 100, 80)

	staticBounds, err := engine.LayoutBounds(ctx, static)
	if err != nil {
		t.Fatalf("LayoutBounds(static): %v", err)
	}
	if staticBounds.Origin.X != 6 || staticBounds.Origin.Y != 6 ||
		staticBounds.Size.Width != 60 || staticBounds.Size.Height != 50 {
		t.Errorf("static node bounds = %v, want origin (6,6) size 60x50 (in flow, insets ignored; a relative node with the same insets would sit at (46,36))", staticBounds)
	}

	// The engine-level attach keeps the pin's direct-parent rule for
	// absolute children (the window layer's static scope is what moves
	// them; see gpui/containingblock.go).
	absBounds, err := engine.LayoutBounds(ctx, absolute)
	if err != nil {
		t.Fatalf("LayoutBounds(absolute): %v", err)
	}
	if absBounds.Origin.X != 16 || absBounds.Origin.Y != 14 ||
		absBounds.Size.Width != 40 || absBounds.Size.Height != 30 {
		t.Errorf("absolute under static at the engine level = %v, want origin (16,14) size 40x30 (the direct-parent attach)", absBounds)
	}
}
