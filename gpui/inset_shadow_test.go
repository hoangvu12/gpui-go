package gpui

// Consumer gap 09: inset shadows cast from the padding edge. CSS casts
// inner box shadows "as if everything outside the padding edge were
// opaque" — the hard band of `inset 0 1px 0` lands just INSIDE the
// border, where the reference frames (and mordbard's design) draw it.
// The pinned GPUI casts them from the border box, so the band lands
// under the border quad (which paints after) and is invisible. These
// tests pin the element path's record geometry: a bordered element's
// inset shadow region and hole derive from the padding box (the CSS
// model, the recorded deviation); a borderless element keeps today's
// border-box geometry exactly.

import (
	"testing"
)

// insetPanelView renders one inset panel with the frame's shape:
// background, 1px border, 12px radii, one inset shadow.
type insetPanelView struct {
	border  float32
	shadow  BoxShadow
	element *DivElement
}

func (v *insetPanelView) Render(w *Window, cx *Context[insetPanelView]) AnyElement {
	panel := Div().W(Px(120)).H(Px(60)).
		Bg(SolidBackground(Hsla{H: 0, S: 0, L: 0.15, A: 1}))
	if v.border > 0 {
		panel = panel.Border(Px(v.border)).
			BorderColor(SolidBackground(Hsla{H: 0, S: 0, L: 0.2, A: 1}))
	}
	panel = panel.Rounded(Px(12)).
		Shadow([]BoxShadow{v.shadow}).
		DebugSelector("inset-panel")
	v.element = panel
	return Div().SizeFull().Child(panel).IntoElement()
}

// insetPanelScene draws one frame of the inset panel and returns its
// shadow records.
func insetPanelScene(t *testing.T, border float32, shadow BoxShadow) []Shadow {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 200, Height: 150})
	view := NewEntity(app, window.Scope(), func(v *insetPanelView, cx *Context[insetPanelView]) {
		v.border = border
		v.shadow = shadow
	})
	window.SetRootView(ViewOf(view))
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	scene := LastDrawnScene(window)
	if scene == nil {
		t.Fatal("no drawn scene")
	}
	shadows, err := scene.Shadows()
	if err != nil {
		t.Fatalf("scene shadows: %v", err)
	}
	return shadows
}

// TestInsetShadowRecordUsesPaddingEdgeForBorderedElements: a 1px-bordered
// 120x60 logical panel at (0,0) casts its `inset 0 1px 0` shadow from
// the padding box — element bounds (2,2,236x116) device, hole
// (2,4,236x116) at the test window's scale 2 — so the hard band is the
// row just inside the top border, visible over the background (CSS's
// model), not under the border quad.
func TestInsetShadowRecordUsesPaddingEdgeForBorderedElements(t *testing.T) {
	shadows := insetPanelScene(t, 1, BoxShadow{
		Color:   SolidBackground(Hsla{H: 0, S: 0, L: 1, A: 0.03}),
		OffsetY: 1,
		Inset:   true,
	})
	if len(shadows) != 1 {
		t.Fatalf("the panel cast %d shadows, want 1", len(shadows))
	}
	got := shadows[0]
	if !got.Inset {
		t.Fatalf("shadow inset = %v, want true", got.Inset)
	}
	if got.BlurRadius != 0 {
		t.Errorf("shadow blur = %v, want 0", got.BlurRadius)
	}
	if got.ElementBounds.Origin != (Point{X: 2, Y: 2}) || got.ElementBounds.Size != (Size{Width: 236, Height: 116}) {
		t.Errorf("inset element bounds = %+v, want the padding box (2,2,236x116) device — CSS casts inner shadows from the padding edge", got.ElementBounds)
	}
	if got.Bounds.Origin != (Point{X: 2, Y: 4}) || got.Bounds.Size != (Size{Width: 236, Height: 116}) {
		t.Errorf("inset hole bounds = %+v, want (2,4,236x116) — the padding box offset by (0,1)", got.Bounds)
	}
}

// TestInsetShadowRecordUnchangedWithoutBorder: a borderless element's
// padding box IS its border box, so the record keeps today's geometry
// exactly (no regression for the pinned path).
func TestInsetShadowRecordUnchangedWithoutBorder(t *testing.T) {
	shadows := insetPanelScene(t, 0, BoxShadow{
		Color:   SolidBackground(Hsla{H: 0, S: 0, L: 1, A: 0.03}),
		OffsetY: 1,
		Inset:   true,
	})
	if len(shadows) != 1 {
		t.Fatalf("the panel cast %d shadows, want 1", len(shadows))
	}
	got := shadows[0]
	if got.ElementBounds.Origin != (Point{X: 0, Y: 0}) || got.ElementBounds.Size != (Size{Width: 240, Height: 120}) {
		t.Errorf("borderless inset element bounds = %+v, want the border box (0,0,240x120) device", got.ElementBounds)
	}
	if got.Bounds.Origin != (Point{X: 0, Y: 2}) {
		t.Errorf("borderless inset hole origin = %v, want (0,2)", got.Bounds.Origin)
	}
}

// TestInsetShadowRadiiShrinkByBorder: the region's corner radii are the
// element's radii reduced by the border width (clamped at zero, then
// scaled), so the band follows the padding edge's rounded corners.
func TestInsetShadowRadiiShrinkByBorder(t *testing.T) {
	shadows := insetPanelScene(t, 2, BoxShadow{
		Color:   SolidBackground(Hsla{H: 0, S: 0, L: 1, A: 0.03}),
		OffsetY: 1,
		Inset:   true,
	})
	if len(shadows) != 1 {
		t.Fatalf("the panel cast %d shadows, want 1", len(shadows))
	}
	radii := shadows[0].CornerRadii
	if radii.TopLeft != 20 || radii.TopRight != 20 || radii.BottomRight != 20 || radii.BottomLeft != 20 {
		t.Errorf("inset hole radii = %+v, want 20 device per corner ((12 - the 2px border) x scale 2)", radii)
	}
	if er := shadows[0].ElementCornerRadii; er.TopLeft != 20 || er.TopRight != 20 {
		t.Errorf("inset element radii = %+v, want 20 device per corner", er)
	}
}

// TestDropShadowRecordKeepsBorderBox: the drop-shadow path is untouched —
// it still casts from the element's border box with the element radii.
func TestDropShadowRecordKeepsBorderBox(t *testing.T) {
	shadows := insetPanelScene(t, 1, BoxShadow{
		Color:      SolidBackground(Hsla{H: 0, S: 0, L: 0, A: 0.5}),
		OffsetY:    12,
		BlurRadius: 3,
	})
	if len(shadows) != 1 {
		t.Fatalf("the panel cast %d shadows, want 1", len(shadows))
	}
	got := shadows[0]
	if got.Inset {
		t.Fatal("the drop shadow is marked inset")
	}
	if got.ElementBounds.Origin != (Point{X: 0, Y: 0}) || got.ElementBounds.Size != (Size{Width: 240, Height: 120}) {
		t.Errorf("drop shadow element bounds = %+v, want the border box (0,0,240x120) device", got.ElementBounds)
	}
	if got.CornerRadii.TopLeft != 24 {
		t.Errorf("drop shadow radii = %+v, want the element radii (24 device)", got.CornerRadii)
	}
}
