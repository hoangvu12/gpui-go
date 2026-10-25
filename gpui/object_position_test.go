package gpui

import "testing"

// Ticket 41's object-position: the placement math against Chromium's
// ComputeObjectFitAndPositionRect (core/layout/layout_replaced.cc:
// 410-477): the position's axes resolve against each axis's FREE space
// (content box - the mode's fitted size) through
// MinimumValueForLength, so keywords are percentages (left/top 0%,
// center 50%, right/bottom 100%) and lengths add after.

func positionedBounds(t *testing.T, fit ObjectFit, position ObjectPosition, boxW, boxH, imgW, imgH float32) Bounds {
	t.Helper()
	return fit.GetPositionedBounds(
		Bounds{Origin: Point{X: 100, Y: 50}, Size: Size{Width: boxW, Height: boxH}},
		int32(imgW), int32(imgH), position,
	)
}

func TestObjectPositionDefaultCentersEveryMode(t *testing.T) {
	// GetBounds (the default position) equals the centered placement of
	// every mode — the CSS initial value 50% 50%
	// (css_properties.json5:4693).
	box := Bounds{Origin: Point{X: 100, Y: 50}, Size: Size{Width: 96, Height: 76}}
	for _, fit := range []ObjectFit{ObjectFitFill, ObjectFitContain, ObjectFitCover, ObjectFitScaleDown, ObjectFitNone} {
		want := fit.GetBounds(box, 60, 40)
		got := fit.GetPositionedBounds(box, 60, 40, ObjectPositionCenter())
		if got != want {
			t.Errorf("fit %v: centered = %v, GetBounds = %v", fit, got, want)
		}
		// (The Go zero value is NOT the default: Fraction 0 is 0% —
		// left/top. A missing style property never reaches this method
		// as a zero; the img paint fills in ObjectPositionCenter.)
		if zero := fit.GetPositionedBounds(box, 60, 40, ObjectPosition{}); zero != (Bounds{Origin: Point{X: 100, Y: 50}, Size: want.Size}) {
			t.Errorf("fit %v: the zero position is %v, want the left-top placement of %v", fit, zero, want.Size)
		}
	}
}

func TestObjectPositionContain(t *testing.T) {
	// A 60x40 image (ratio 1.5) in a 96x76 box: contain gives 96x64, so
	// the free space is 0 x 12.
	// left top: x = 0, y = 0.
	got := positionedBounds(t, ObjectFitContain, ObjectPosition{X: ObjectLeft(), Y: ObjectTop()}, 96, 76, 60, 40)
	if got.Origin != (Point{X: 100, Y: 50}) || got.Size != (Size{Width: 96, Height: 64}) {
		t.Errorf("contain left top = %v", got)
	}
	// right bottom: x = 0 (no free width), y = 12.
	got = positionedBounds(t, ObjectFitContain, ObjectPosition{X: ObjectRight(), Y: ObjectBottom()}, 96, 76, 60, 40)
	if got.Origin != (Point{X: 100, Y: 62}) {
		t.Errorf("contain right bottom origin = %v, want (100, 62)", got.Origin)
	}
	// 25% 75%: x = 0, y = 0.75·12 = 9.
	got = positionedBounds(t, ObjectFitContain, ObjectPosition{X: ObjectPercent(25), Y: ObjectPercent(75)}, 96, 76, 60, 40)
	if got.Origin != (Point{X: 100, Y: 59}) {
		t.Errorf("contain 25%% 75%% origin = %v, want (100, 59)", got.Origin)
	}
	// 12px 8px: lengths add after the fraction.
	got = positionedBounds(t, ObjectFitContain, ObjectPosition{X: ObjectPx(12), Y: ObjectPx(8)}, 96, 76, 60, 40)
	if got.Origin != (Point{X: 112, Y: 58}) {
		t.Errorf("contain 12px 8px origin = %v, want (112, 58)", got.Origin)
	}
	// "right 10px" spelled Fraction 1, OffsetPx -10.
	got = positionedBounds(t, ObjectFitContain, ObjectPosition{X: ObjectPositionAxis{Fraction: 1, OffsetPx: -10}, Y: ObjectTop()}, 96, 76, 60, 40)
	if got.Origin != (Point{X: 90, Y: 50}) {
		t.Errorf("contain right 10px top origin = %v, want (90, 50)", got.Origin)
	}
}

func TestObjectPositionNone(t *testing.T) {
	// none keeps the 60x40 intrinsic size: free space 36 x 36.
	got := positionedBounds(t, ObjectFitNone, ObjectPosition{X: ObjectLeft(), Y: ObjectTop()}, 96, 76, 60, 40)
	if got.Origin != (Point{X: 100, Y: 50}) || got.Size != (Size{Width: 60, Height: 40}) {
		t.Errorf("none left top = %v", got)
	}
	got = positionedBounds(t, ObjectFitNone, ObjectPosition{X: ObjectRight(), Y: ObjectBottom()}, 96, 76, 60, 40)
	if got.Origin != (Point{X: 136, Y: 86}) {
		t.Errorf("none right bottom origin = %v, want (136, 86)", got.Origin)
	}
	// 25% 75%: 9 x 27.
	got = positionedBounds(t, ObjectFitNone, ObjectPosition{X: ObjectPercent(25), Y: ObjectPercent(75)}, 96, 76, 60, 40)
	if got.Origin != (Point{X: 109, Y: 77}) {
		t.Errorf("none 25%% 75%% origin = %v, want (109, 77)", got.Origin)
	}
}

func TestObjectPositionCoverPansTheCrop(t *testing.T) {
	// A 60x40 image in a 96x76 box: cover gives 114x76 (width-bound),
	// so the free width is -18: left/top pins the crop's top-left,
	// right shows the other end.
	got := positionedBounds(t, ObjectFitCover, ObjectPosition{X: ObjectLeft(), Y: ObjectTop()}, 96, 76, 60, 40)
	if got.Size != (Size{Width: 114, Height: 76}) || got.Origin != (Point{X: 100, Y: 50}) {
		t.Errorf("cover left top = %v", got)
	}
	got = positionedBounds(t, ObjectFitCover, ObjectPosition{X: ObjectRight(), Y: ObjectCenter()}, 96, 76, 60, 40)
	if got.Origin != (Point{X: 82, Y: 50}) {
		t.Errorf("cover right origin = %v, want (82, 50) (panned by the -18 free width)", got.Origin)
	}
}

func TestObjectPositionScaleDown(t *testing.T) {
	// A 60x40 image in a 96x76 box fits already: scale-down keeps the
	// intrinsic size, free space 36 x 36.
	got := positionedBounds(t, ObjectFitScaleDown, ObjectPosition{X: ObjectRight(), Y: ObjectBottom()}, 96, 76, 60, 40)
	if got.Origin != (Point{X: 136, Y: 86}) || got.Size != (Size{Width: 60, Height: 40}) {
		t.Errorf("scale-down right bottom = %v", got)
	}
	// An 80x80 image in the same box scales down to 76x76: free space
	// 20 x 0.
	got = positionedBounds(t, ObjectFitScaleDown, ObjectPosition{X: ObjectRight(), Y: ObjectBottom()}, 96, 76, 80, 80)
	if got.Origin != (Point{X: 120, Y: 50}) || got.Size != (Size{Width: 76, Height: 76}) {
		t.Errorf("scale-down (shrunk) right bottom = %v", got)
	}
}

func TestObjectPositionFillShiftsOnlyByLengths(t *testing.T) {
	// Chromium's fast path: fill + the default position returns the
	// base rect; with any other position the fitted size IS the base
	// size, so the free space is zero — percentages do nothing, lengths
	// still shift (layout_replaced.cc:410-477).
	got := positionedBounds(t, ObjectFitFill, ObjectPosition{X: ObjectPercent(75), Y: ObjectPercent(25)}, 96, 76, 60, 40)
	if got.Origin != (Point{X: 100, Y: 50}) || got.Size != (Size{Width: 96, Height: 76}) {
		t.Errorf("fill 75%% 25%% = %v, want the base rect", got)
	}
	got = positionedBounds(t, ObjectFitFill, ObjectPosition{X: ObjectPx(12), Y: ObjectPx(8)}, 96, 76, 60, 40)
	if got.Origin != (Point{X: 112, Y: 58}) {
		t.Errorf("fill 12px 8px origin = %v, want (112, 58)", got.Origin)
	}
}

func TestObjectPositionDegenerateInputs(t *testing.T) {
	// Degenerate inputs (non-positive content or bounds size) collapse
	// to a zero-size bounds at the element origin — the recorded guard.
	box := Bounds{Origin: Point{X: 100, Y: 50}, Size: Size{Width: 96, Height: 76}}
	got := ObjectFitContain.GetPositionedBounds(box, 0, 40, ObjectPosition{X: ObjectLeft(), Y: ObjectTop()})
	if got != (Bounds{Origin: Point{X: 100, Y: 50}}) {
		t.Errorf("degenerate content = %v", got)
	}
}

func TestObjectPositionStyleRefinement(t *testing.T) {
	// The style property refines like any paint option: a later
	// refinement wins, the default keeps the element unperturbed.
	st := Div().Flex().ObjectPosition(ObjectLeft(), ObjectTop()).StyleMut()
	pos, ok := st.ObjectPosition.Get()
	if !ok || pos != (ObjectPosition{X: ObjectLeft(), Y: ObjectTop()}) {
		t.Errorf("ObjectPosition refinement = %+v, %v", pos, ok)
	}
	style := DefaultStyle()
	if style.ObjectPosition != nil {
		t.Error("the default style carries an object position")
	}
	style.Refine(st)
	if style.ObjectPosition == nil || *style.ObjectPosition != (ObjectPosition{X: ObjectLeft(), Y: ObjectTop()}) {
		t.Errorf("refined style's object position = %+v", style.ObjectPosition)
	}
	// A second refinement replaces it.
	style.Refine(Div().Flex().ObjectPosition(ObjectRight(), ObjectBottom()).StyleMut())
	if style.ObjectPosition == nil || *style.ObjectPosition != (ObjectPosition{X: ObjectRight(), Y: ObjectBottom()}) {
		t.Errorf("re-refined style's object position = %+v", style.ObjectPosition)
	}
}

func TestImageRenderingStyleRefinement(t *testing.T) {
	st := Div().Flex().RenderingPixelated().StyleMut()
	rendering, ok := st.ImageRendering.Get()
	if !ok || rendering != ImageRenderingPixelated {
		t.Errorf("RenderingPixelated refinement = %+v, %v", rendering, ok)
	}
	style := DefaultStyle()
	style.Refine(st)
	if style.ImageRendering == nil || !style.ImageRendering.PointSampled() {
		t.Error("the refined style does not point-sample")
	}
	if ImageRenderingAuto.PointSampled() {
		t.Error("auto point-samples")
	}
	if !ImageRenderingCrispEdges.PointSampled() {
		t.Error("crisp-edges does not point-sample (Chromium maps it to kInterpolationNone)")
	}
}

func TestObjectFitStyleRefinement(t *testing.T) {
	st := Div().Flex().ObjectCover().StyleMut()
	fit, ok := st.ObjectFit.Get()
	if !ok || fit != ObjectFitCover {
		t.Errorf("ObjectCover refinement = %+v, %v", fit, ok)
	}
	style := DefaultStyle()
	if style.ObjectFit != nil {
		t.Error("the default style carries an object fit")
	}
	style.Refine(st)
	if style.ObjectFit == nil || *style.ObjectFit != ObjectFitCover {
		t.Errorf("refined style's object fit = %+v", style.ObjectFit)
	}
}
