package gpui

import "testing"

// Ticket 41's scene seam (rev 15.1 shape): a filter chain composed into
// one boundary pair carries the composed blur radius and the chain's
// per-function colour matrices through the record (one slot per
// function — the clamp between them is Chrome's semantics, see
// filter_colors.go), and the plan compiles it as one isolated group.
func TestFilterBoundaryCarriesComposedChain(t *testing.T) {
	scene, err := NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })

	bounds := Bounds{Origin: Point{X: 10, Y: 10}, Size: Size{Width: 120, Height: 48}}
	mask := Bounds{Origin: Point{}, Size: Size{Width: 640, Height: 400}}
	ctx := NewPaintContext(2.0, mask, 1.0)

	// blur(3) grayscale(1) hue-rotate(90): one blur entry plus one
	// matrix slot per colour function, in chain order.
	chain := composeFilterChain([]Filter{Blur(3), Grayscale(1), HueRotate(90)})
	if chain.BlurRadius != 3 || chain.ColorMatrixCount != 2 {
		t.Fatalf("chain = %+v, want blur 3 and two matrix slots", chain)
	}
	gray, _ := Grayscale(1).colorMatrix()
	hue, _ := HueRotate(90).colorMatrix()
	var wantSlot0, wantSlot1 [20]float32
	copy(wantSlot0[:], chain.ColorMatrix[0:20])
	copy(wantSlot1[:], chain.ColorMatrix[20:40])
	if wantSlot0 != gray || wantSlot1 != hue {
		t.Fatalf("chain slots = %v, want grayscale then hue-rotate uncomposed", [2][20]float32{wantSlot0, wantSlot1})
	}

	if err := scene.BeginFilterGroupChain(bounds, chain, Corners{}, 0, ctx); err != nil {
		t.Fatalf("BeginFilterGroupChain: %v", err)
	}
	if err := scene.PaintQuad(quadFor(bounds), 0, ctx); err != nil {
		t.Fatalf("PaintQuad: %v", err)
	}
	if err := scene.EndFilterGroupChain(bounds, chain, Corners{}, 0, ctx); err != nil {
		t.Fatalf("EndFilterGroupChain: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}

	boundaries, err := scene.FilterBoundaries()
	if err != nil {
		t.Fatalf("FilterBoundaries: %v", err)
	}
	if len(boundaries) != 2 {
		t.Fatalf("boundaries = %d, want a begin/end pair", len(boundaries))
	}
	for _, b := range boundaries {
		if b.BlurRadius != 6 { // logical 3 * the context's 2.0 scale
			t.Errorf("boundary blur radius = %v, want 6 (device px)", b.BlurRadius)
		}
		if b.ColorMatrixCount != 2 {
			t.Errorf("boundary matrix count = %d, want 2", b.ColorMatrixCount)
		}
		var slot0, slot1 [20]float32
		copy(slot0[:], b.ColorMatrix[0:20])
		copy(slot1[:], b.ColorMatrix[20:40])
		if slot0 != gray || slot1 != hue {
			t.Errorf("boundary slots = %v, want the per-function grayscale then hue-rotate %v", [2][20]float32{slot0, slot1}, [2][20]float32{gray, hue})
		}
		if b.Opacity != 1.0 {
			t.Errorf("boundary opacity = %v, want 1.0 (the pinned group rule)", b.Opacity)
		}
	}
	if !boundaries[0].IsStart || boundaries[1].IsStart {
		t.Errorf("boundary order: start=%v then %v", boundaries[0].IsStart, boundaries[1].IsStart)
	}

	commands, err := scene.Commands()
	if err != nil {
		t.Fatalf("Commands: %v", err)
	}
	var begins, batches, ends int
	for _, command := range commands {
		switch command.Kind {
		case SceneCommandKindBeginFilter:
			begins++
			if !command.Target.Isolated {
				t.Error("the chain's begin command is not an isolated group")
			}
		case SceneCommandKindBatch:
			batches++
		case SceneCommandKindEndFilter:
			ends++
		}
	}
	if begins != 1 || ends != 1 || batches != 1 {
		t.Fatalf("plan compiled %d begins, %d batches, %d ends; want 1, 1, 1", begins, batches, ends)
	}
}

// A matrix-only chain still opens a group (the pinned blur-only gate
// must not drop it), and an identity chain opens none.
func TestFilterChainGroupGating(t *testing.T) {
	bounds := Bounds{Origin: Point{X: 10, Y: 10}, Size: Size{Width: 100, Height: 40}}
	mask := Bounds{Origin: Point{}, Size: Size{Width: 640, Height: 400}}
	ctx := NewPaintContext(1.0, mask, 1.0)

	matrixOnly := composeFilterChain([]Filter{Invert(1)})
	if matrixOnly.IsEmpty() {
		t.Fatal("invert(1) composed to an empty chain")
	}
	scene, err := NewScene()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scene.Dispose() })
	if err := scene.BeginFilterGroupChain(bounds, matrixOnly, Corners{}, 0, ctx); err != nil {
		t.Fatalf("BeginFilterGroupChain: %v", err)
	}
	if err := scene.EndFilterGroupChain(bounds, matrixOnly, Corners{}, 0, ctx); err != nil {
		t.Fatalf("EndFilterGroupChain: %v", err)
	}
	if err := scene.Finish(); err != nil {
		t.Fatal(err)
	}
	boundaries, err := scene.FilterBoundaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(boundaries) != 2 {
		t.Fatalf("a matrix-only chain produced %d boundaries, want a pair (no blur, still a group)", len(boundaries))
	}
	if boundaries[0].BlurRadius != 0 || boundaries[0].ColorMatrixCount != 1 {
		t.Errorf("matrix-only boundary = %+v, want blur 0 and one matrix", boundaries[0])
	}

	identity := composeFilterChain([]Filter{Blur(0), Grayscale(0), Saturate(1)})
	scene2, err := NewScene()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scene2.Dispose() })
	if err := scene2.BeginFilterGroupChain(bounds, identity, Corners{}, 0, ctx); err != nil {
		t.Fatalf("BeginFilterGroupChain(identity): %v", err)
	}
	if err := scene2.EndFilterGroupChain(bounds, identity, Corners{}, 0, ctx); err != nil {
		t.Fatalf("EndFilterGroupChain(identity): %v", err)
	}
	if err := scene2.Finish(); err != nil {
		t.Fatal(err)
	}
	boundaries2, err := scene2.FilterBoundaries()
	if err != nil {
		t.Fatal(err)
	}
	if len(boundaries2) != 0 {
		t.Errorf("an identity chain produced %d boundaries, want none (inline paint)", len(boundaries2))
	}
}

// quadFor builds a plain background quad at bounds (PaintQuad's
// smallest correct payload for a scene seam test).
func quadFor(bounds Bounds) PaintQuad {
	background := SolidBackground(Rgb(0xff0000))
	return PaintQuad{
		Bounds:             bounds,
		Background:         background,
		BorderColor:        background.Opacity(0),
		BorderStyle:        BorderStyleSolid,
		BorderDashedLength: DefaultBorderDashedLength,
		BorderDashedGap:    DefaultBorderDashedGap,
	}
}
