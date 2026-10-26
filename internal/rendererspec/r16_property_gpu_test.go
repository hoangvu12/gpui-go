//go:build windows

// The browser-core ticket 51 (rev 16) renderer gate for property-node
// updates: a finished scene rendered, mutated through
// scene_update_property, and rendered again through the SAME handle
// draws the new transform, and the readback changes ONLY inside the
// old ∪ new mapped bounds the update reports.
package rendererspec

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// TestR16PropertyUpdateRedrawsOnlyInDamage: the rotated property node
// re-renders at the new rotation with the damage-limited change.
func TestR16PropertyUpdateRedrawsOnlyInDamage(t *testing.T) {
	const w, h = 640, 480
	surface := r16Surface(t, "rendererspec r16 property update", 640, 480)

	// The rotation about the local center (30,30) placed at (250,200):
	// two angle values for the before/after.
	rotationAbout := func(degrees float64) gpui.TransformationMatrix {
		angle := degrees * math.Pi / 180
		return r16MatrixAbout(
			[2][2]float32{
				{float32(math.Cos(angle)), float32(-math.Sin(angle))},
				{float32(math.Sin(angle)), float32(math.Cos(angle))},
			},
			[2]float32{30, 30},
			[2]float32{250, 200},
		)
	}

	scene := r16Scene(t)
	fullMask := gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}
	group := gpui.EffectGroup{
		HasTransform: true,
		PropertyNode: true,
		NodeID:       7,
		// The composite always multiplies by the record's opacity - 1
		// for a transform-only group.
		Opacity:     1,
		Transform:   rotationAbout(0),
		Bounds:      gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 60, Height: 60}},
		ContentMask: fullMask,
	}
	if err := scene.BeginEffectGroup(group); err != nil {
		t.Fatalf("BeginEffectGroup: %v", err)
	}
	r16LocalQuads(t, scene)
	end := group
	end.IsStart = false
	if err := scene.EndEffectGroup(end); err != nil {
		t.Fatalf("EndEffectGroup: %v", err)
	}
	before := r16RenderAt(t, surface, scene, nil, w, h)

	// The property tick: rotate to 40 degrees through the update entry
	// (no re-sort, no re-plan - the same scene handle).
	oldBounds, newBounds, err := scene.UpdateEffectMatrix(7, rotationAbout(40))
	if err != nil {
		t.Fatalf("UpdateEffectMatrix: %v", err)
	}
	after := r16RenderAt(t, surface, scene, nil, w, h)

	// The old bounds map the identity rotation: the local 60x60 at
	// (250, 200).
	ox, oy, ow, oh := oldBounds.Origin.X, oldBounds.Origin.Y, oldBounds.Size.Width, oldBounds.Size.Height
	if ox != 250 || oy != 200 || ow != 60 || oh != 60 {
		t.Fatalf("old bounds = (%v,%v,%v,%v), want the unrotated (250,200,60,60)", ox, oy, ow, oh)
	}
	// The new bounds cover the rotated box (a 40-degree rotation of a
	// 60x60 square about its center spans ~66.4 px).
	nx, ny, nw, nh := newBounds.Origin.X, newBounds.Origin.Y, newBounds.Size.Width, newBounds.Size.Height
	if nx >= 250 || ny >= 200 || nw < 66 || nh < 66 {
		t.Fatalf("new bounds = (%v,%v,%v,%v), want the rotated box (bigger than 66x66, min corner left of the origin)", nx, ny, nw, nh)
	}

	// Inside the damage union the pixels change (the rotation moved the
	// content); outside it they are byte-identical. The union is
	// dilated by the composite's bilinear half-texel support: the
	// mapped bounds are exact geometry, and the linear filter lets the
	// content's edge bleed up to half a texel past them (the Go damage
	// computation of ticket 58/60 dilates the same way - the design's
	// blur-damage expansion rule).
	damage := func(x, y int) bool {
		const dilation = 2.0
		dilated := func(b gpui.Bounds) gpui.Bounds {
			return gpui.Bounds{
				Origin: gpui.Point{X: b.Origin.X - dilation, Y: b.Origin.Y - dilation},
				Size:   gpui.Size{Width: b.Size.Width + 2*dilation, Height: b.Size.Height + 2*dilation},
			}
		}
		return intersects(x, y, dilated(oldBounds)) || intersects(x, y, dilated(newBounds))
	}
	changedInside, totalInside := 0, 0
	dirtyOutside := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			i := (y*w + x) * 4
			changed := before[i] != after[i] || before[i+1] != after[i+1] ||
				before[i+2] != after[i+2] || before[i+3] != after[i+3]
			if damage(x, y) {
				totalInside++
				if changed {
					changedInside++
				}
			} else if changed {
				dirtyOutside++
			}
		}
	}
	if dirtyOutside != 0 {
		t.Fatalf("%d pixels changed OUTSIDE the old-union-new damage the update reported", dirtyOutside)
	}
	if changedInside == 0 {
		t.Fatalf("no pixel changed inside the damage - the update did not redraw")
	}
}

// intersects reports whether the pixel center (x, y) falls inside the
// mapped bounds (the damage rectangles are inclusive of their far
// edge).
func intersects(x, y int, bounds gpui.Bounds) bool {
	bx, by, bw, bh := bounds.Origin.X, bounds.Origin.Y, bounds.Size.Width, bounds.Size.Height
	fx, fy := float64(x)+0.5, float64(y)+0.5
	return fx >= float64(bx) && fx <= float64(bx+bw) && fy >= float64(by) && fy <= float64(by+bh)
}
