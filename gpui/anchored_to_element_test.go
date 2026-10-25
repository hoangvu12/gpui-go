package gpui

// Ticket 25 (browser-core): the anchored element's anchor-to-element
// mode — CSS anchor positioning (position-anchor, the side/align/gap
// placement of position-area / anchor(), position-try-fallbacks
// flip-block/flip-inline, the shift clamp, and position-visibility:
// anchors-visible) — tested through the public Anchored API against the
// 800x600 paint-fixture window. The parity twins of these tests are
// internal/parity/fixtures/anchor.

import "testing"

// anchoredAnchorView renders one anchor element plus one deferred,
// element-anchored popup built by configure, mirroring the popover
// pattern (the popup is a deferred sibling of the anchor so it is not
// clipped by any ancestor and prepaints after the anchor's main pass).
func anchoredAnchorView(anchorID string, anchor Bounds, configure func(*AnchoredElement) *AnchoredElement) func(*paintView, *Window, *Context[paintView]) AnyElement {
	return func(*paintView, *Window, *Context[paintView]) AnyElement {
		popup := configure(Anchored().AnchorTo(anchorID))
		return Div().SizeFull().Flex().Child(
			Div().Flex().ID(anchorID).DebugSelector("ANCHOR").
				Absolute().Top(Px(anchor.Origin.Y)).Left(Px(anchor.Origin.X)).
				W(Px(anchor.Size.Width)).H(Px(anchor.Size.Height)).
				Bg(paintBlue),
		).Child(
			Deferred(popup.Child(
				Div().Flex().DebugSelector("POPUP").W(Px(60)).H(Px(40)).Bg(paintRed),
			)),
		).IntoElement()
	}
}

// anchoredSideDraws draws one configured anchored-anchor view and
// returns the popup's and the anchor's final-frame bounds.
func anchoredSideDraw(t *testing.T, anchor Bounds, configure func(*AnchoredElement) *AnchoredElement) (Bounds, Bounds) {
	t.Helper()
	f := newPaintFixture(t, anchoredAnchorView("trigger", anchor, configure))
	f.draw()
	f.ta.RunUntilParked()
	return debugBoundsOf(t, f, "POPUP"), debugBoundsOf(t, f, "ANCHOR")
}

// debugBoundsOf reads one debug-selector bounds from the last frame.
func debugBoundsOf(t *testing.T, f *paintFixture, selector string) Bounds {
	t.Helper()
	bounds, ok := WindowDebugBound(f.window, selector)
	if !ok {
		t.Fatalf("%s debug bounds not found", selector)
	}
	return bounds
}

// TestAnchoredSidePlacesEachSideWithAlignmentAndGap pins the side
// placement: the popup sits on the chosen side of the anchor, gap
// pixels away, aligned start, center or end along the side (CSS
// anchor(): bottom is top: anchor(bottom)+gap; end is left:
// anchor(right)-width; center is anchor(center)-size/2).
func TestAnchoredSidePlacesEachSideWithAlignmentAndGap(t *testing.T) {
	anchor := logical(240, 180, 160, 40)
	cases := []struct {
		name      string
		configure func(*AnchoredElement) *AnchoredElement
		want      Bounds
	}{
		{
			name: "bottom start",
			configure: func(a *AnchoredElement) *AnchoredElement {
				return a.Side(AnchorSideBottom).Align(AnchorAlignStart).Gap(8)
			},
			want: logical(240, 228, 60, 40),
		},
		{
			name: "bottom end",
			configure: func(a *AnchoredElement) *AnchoredElement {
				return a.Side(AnchorSideBottom).Align(AnchorAlignEnd).Gap(8)
			},
			want: logical(340, 228, 60, 40),
		},
		{
			name: "bottom center",
			configure: func(a *AnchoredElement) *AnchoredElement {
				return a.Side(AnchorSideBottom).Align(AnchorAlignCenter).Gap(8)
			},
			want: logical(290, 228, 60, 40),
		},
		{
			name:      "top end",
			configure: func(a *AnchoredElement) *AnchoredElement { return a.Side(AnchorSideTop).Align(AnchorAlignEnd).Gap(8) },
			want:      logical(340, 132, 60, 40),
		},
		{
			name: "left center",
			configure: func(a *AnchoredElement) *AnchoredElement {
				return a.Side(AnchorSideLeft).Align(AnchorAlignCenter).Gap(8)
			},
			want: logical(172, 180, 60, 40),
		},
		{
			name: "right center",
			configure: func(a *AnchoredElement) *AnchoredElement {
				return a.Side(AnchorSideRight).Align(AnchorAlignCenter).Gap(8)
			},
			want: logical(408, 180, 60, 40),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := anchoredSideDraw(t, anchor, tc.configure)
			if got != tc.want {
				t.Fatalf("POPUP = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAnchoredSideDefaults pins the side-mode defaults: bottom side,
// start alignment, no gap, no fallbacks.
func TestAnchoredSideDefaults(t *testing.T) {
	got, _ := anchoredSideDraw(t, logical(240, 180, 160, 40), func(a *AnchoredElement) *AnchoredElement { return a })
	if got != logical(240, 220, 60, 40) {
		t.Fatalf("POPUP = %v, want (240,220 60x40): side bottom, align start, gap 0", got)
	}
}

// TestAnchoredSideOffsetAppliesAfterPlacement pins that the existing
// Offset (R:anchored.rs:54) still offsets the final position in side
// mode, on top of the side placement.
func TestAnchoredSideOffsetAppliesAfterPlacement(t *testing.T) {
	got, _ := anchoredSideDraw(t, logical(240, 180, 160, 40), func(a *AnchoredElement) *AnchoredElement {
		return a.Side(AnchorSideBottom).Align(AnchorAlignStart).Gap(8).Offset(Point{X: 12, Y: -4})
	})
	if got != logical(252, 224, 60, 40) {
		t.Fatalf("POPUP = %v, want (252,224 60x40)", got)
	}
}

// TestAnchoredFlipBlockFlipsTheSide pins flip-block near a window edge:
// a bottom popup that would overflow the bottom flips above the anchor
// (CSS position-try-fallbacks: flip-block; Chromium
// out_of_flow_layout_part.cc tries the base style, then each fallback,
// until one fits, and falls back to the base style when none does).
func TestAnchoredFlipBlockFlipsTheSide(t *testing.T) {
	// The anchor's bottom edge is the window's bottom edge (600): a
	// 40px popup 8px below overflows, the flipped top side fits.
	got, _ := anchoredSideDraw(t, logical(40, 560, 120, 40), func(a *AnchoredElement) *AnchoredElement {
		return a.Side(AnchorSideBottom).Align(AnchorAlignStart).Gap(8).TryFallbacks(AnchorFlipBlock)
	})
	if got != logical(40, 512, 60, 40) {
		t.Fatalf("POPUP = %v, want (40,512 60x40): flipped above the anchor", got)
	}
}

// TestAnchoredFlipBlockKeepsBaseWhenBothSidesOverflow pins the
// no-fitting-fallback rule (Chromium's
// MoveToLastSuccessfulOrStyleWithoutFallbacks): when neither the base
// side nor the flipped side fits, the base placement stands.
func TestAnchoredFlipBlockKeepsBaseWhenBothSidesOverflow(t *testing.T) {
	// Anchor at the exact bottom edge: below overflows by 8+40, above
	// would need y=600-8-40-40… the flipped top side fits, so use a
	// popup taller than the window's remaining space on both sides: the
	// window is 600 tall; the anchor at (40,300) leaves 260 below and
	// 300 above — a 500px popup fits neither.
	f := newPaintFixture(t, anchoredAnchorViewWithPopup("trigger", logical(40, 300, 120, 40), logical(0, 0, 60, 500), func(a *AnchoredElement) *AnchoredElement {
		return a.Side(AnchorSideBottom).Align(AnchorAlignStart).Gap(8).TryFallbacks(AnchorFlipBlock)
	}))
	f.draw()
	f.ta.RunUntilParked()
	if got := debugBoundsOf(t, f, "POPUP"); got != logical(40, 348, 60, 500) {
		t.Fatalf("POPUP = %v, want (40,348 60x500): no fallback fits, the base side stands", got)
	}
}

// TestAnchoredFlipInlineFlipsAlignmentOnVerticalSide pins flip-inline
// on a vertical side: the horizontal relationship mirrors, so the
// alignment flips start<->end and the side stays.
func TestAnchoredFlipInlineFlipsAlignmentOnVerticalSide(t *testing.T) {
	// Anchor at the right edge: a start-aligned 100px popup at x=720
	// overflows right, the flipped end alignment puts its right edge at
	// the anchor's right edge and fits.
	f := newPaintFixture(t, anchoredAnchorViewWithPopup("trigger", logical(720, 300, 60, 40), logical(0, 0, 100, 40), func(a *AnchoredElement) *AnchoredElement {
		return a.Side(AnchorSideBottom).Align(AnchorAlignStart).Gap(8).TryFallbacks(AnchorFlipInline)
	}))
	f.draw()
	f.ta.RunUntilParked()
	if got := debugBoundsOf(t, f, "POPUP"); got != logical(680, 348, 100, 40) {
		t.Fatalf("POPUP = %v, want (680,348 100x40): alignment flipped to end", got)
	}
}

// TestAnchoredFlipInlineFlipsSideOnHorizontalSide pins flip-inline on
// a horizontal side: the side flips left<->right.
func TestAnchoredFlipInlineFlipsSideOnHorizontalSide(t *testing.T) {
	// A left popup near the left window edge overflows; flip-inline
	// moves it to the anchor's right.
	got, _ := anchoredSideDraw(t, logical(40, 300, 60, 40), func(a *AnchoredElement) *AnchoredElement {
		return a.Side(AnchorSideLeft).Align(AnchorAlignStart).Gap(8).TryFallbacks(AnchorFlipInline)
	})
	if got != logical(108, 300, 60, 40) {
		t.Fatalf("POPUP = %v, want (108,300 60x40): side flipped to right", got)
	}
}

// TestAnchoredFlipBlockFlipsAlignmentOnHorizontalSide pins flip-block
// on a horizontal side: the vertical relationship mirrors, so the
// alignment along the side flips.
func TestAnchoredFlipBlockFlipsAlignmentOnHorizontalSide(t *testing.T) {
	// A right-side popup at the bottom window edge: the start (top)
	// alignment overflows bottom, flip-block flips to end alignment.
	got, _ := anchoredSideDraw(t, logical(600, 560, 60, 40), func(a *AnchoredElement) *AnchoredElement {
		return a.Side(AnchorSideRight).Align(AnchorAlignStart).Gap(8).TryFallbacks(AnchorFlipBlock)
	})
	if got != logical(668, 560, 60, 40) {
		t.Fatalf("POPUP = %v, want (668,560 60x40): alignment flipped to end (y at the anchor's bottom)", got)
	}
}

// TestAnchoredShiftClampsIntoViewport pins the shift fallback: the base
// placement clamps into the viewport on both axes (Base's positioner
// clamp; CSS expresses it with min()/max() over the anchor() inset).
func TestAnchoredShiftClampsIntoViewport(t *testing.T) {
	// Near the right window edge: the popup's right edge clamps to 800.
	f := newPaintFixture(t, anchoredAnchorViewWithPopup("trigger", logical(720, 200, 60, 40), logical(0, 0, 100, 60), func(a *AnchoredElement) *AnchoredElement {
		return a.Side(AnchorSideBottom).Align(AnchorAlignStart).Gap(8).TryFallbacks(AnchorShift)
	}))
	f.draw()
	f.ta.RunUntilParked()
	if got := debugBoundsOf(t, f, "POPUP"); got != logical(700, 248, 100, 60) {
		t.Fatalf("POPUP = %v, want (700,248 100x60): shifted left to the window's right edge", got)
	}
	// Near the bottom edge on a left-side popup: the bottom clamps to 600.
	f = newPaintFixture(t, anchoredAnchorViewWithPopup("trigger", logical(100, 560, 60, 40), logical(0, 0, 80, 50), func(a *AnchoredElement) *AnchoredElement {
		return a.Side(AnchorSideLeft).Align(AnchorAlignStart).Gap(8).TryFallbacks(AnchorShift)
	}))
	f.draw()
	f.ta.RunUntilParked()
	if got := debugBoundsOf(t, f, "POPUP"); got != logical(12, 550, 80, 50) {
		t.Fatalf("POPUP = %v, want (12,550 80x50): shifted up to the window's bottom edge", got)
	}
}

// TestAnchoredShiftMarginKeepsDistanceFromTheEdge pins the shift's
// viewport margin (Base's Positioner::margin).
func TestAnchoredShiftMarginKeepsDistanceFromTheEdge(t *testing.T) {
	f := newPaintFixture(t, anchoredAnchorViewWithPopup("trigger", logical(720, 200, 60, 40), logical(0, 0, 100, 60), func(a *AnchoredElement) *AnchoredElement {
		return a.Side(AnchorSideBottom).Align(AnchorAlignStart).Gap(8).
			TryFallbacks(AnchorShift).Margin(Edges{Top: 4, Right: 4, Bottom: 4, Left: 4})
	}))
	f.draw()
	f.ta.RunUntilParked()
	if got := debugBoundsOf(t, f, "POPUP"); got != logical(696, 248, 100, 60) {
		t.Fatalf("POPUP = %v, want (696,248 100x60): 4px from the window's right edge", got)
	}
}

// TestAnchoredFallbacksAreNotTriedWhenTheBaseFits pins the fallback
// iteration's first rule (Chromium's OOFCandidateStyleIterator: the
// base style is candidate 0): a placement that already fits keeps the
// base side even when fallbacks are listed — the flip and shift never
// run.
func TestAnchoredFallbacksAreNotTriedWhenTheBaseFits(t *testing.T) {
	got, _ := anchoredSideDraw(t, logical(240, 180, 160, 40), func(a *AnchoredElement) *AnchoredElement {
		return a.Side(AnchorSideBottom).Align(AnchorAlignStart).Gap(8).
			TryFallbacks(AnchorFlipBlock, AnchorFlipInline, AnchorShift)
	})
	if got != logical(240, 228, 60, 40) {
		t.Fatalf("POPUP = %v, want (240,228 60x40): the base placement fits, no fallback is tried", got)
	}
}

// TestAnchoredNoFallbacksLeavesTheBasePlacement pins CSS's default:
// position-try-fallbacks: none — an overflowing popup keeps its
// placement instead of being snapped into the window (the corner modes
// keep their snap; the side mode follows the browser).
func TestAnchoredNoFallbacksLeavesTheBasePlacement(t *testing.T) {
	got, _ := anchoredSideDraw(t, logical(720, 200, 60, 40), func(a *AnchoredElement) *AnchoredElement {
		return a.Side(AnchorSideBottom).Align(AnchorAlignStart).Gap(8)
	})
	if got != logical(720, 248, 60, 40) {
		t.Fatalf("POPUP = %v, want (720,248 60x40): no fallbacks, no snap", got)
	}
}

// anchoredAnchorViewWithPopup is anchoredAnchorView with the popup's
// size and color configurable.
func anchoredAnchorViewWithPopup(anchorID string, anchor, popup Bounds, configure func(*AnchoredElement) *AnchoredElement) func(*paintView, *Window, *Context[paintView]) AnyElement {
	return func(*paintView, *Window, *Context[paintView]) AnyElement {
		popupElement := configure(Anchored().AnchorTo(anchorID))
		return Div().SizeFull().Flex().Child(
			Div().Flex().ID(anchorID).DebugSelector("ANCHOR").
				Absolute().Top(Px(anchor.Origin.Y)).Left(Px(anchor.Origin.X)).
				W(Px(anchor.Size.Width)).H(Px(anchor.Size.Height)).
				Bg(paintBlue),
		).Child(
			Deferred(popupElement.Child(
				Div().Flex().DebugSelector("POPUP").W(Px(popup.Size.Width)).H(Px(popup.Size.Height)).Bg(paintRed),
			)),
		).IntoElement()
	}
}

// TestAnchoredAnchorBoundsCellTracksTheAnchor pins the tracked-cell
// anchor (Base's Popup measure pattern): the popup follows the cell
// frame by frame.
func TestAnchoredAnchorBoundsCellTracksTheAnchor(t *testing.T) {
	cell := &Bounds{Origin: Point{X: 240, Y: 180}, Size: Size{Width: 160, Height: 40}}
	f := newPaintFixture(t, func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().SizeFull().Flex().Child(
			Deferred(Anchored().AnchorBounds(cell).Side(AnchorSideBottom).Align(AnchorAlignStart).Gap(8).Child(
				Div().Flex().DebugSelector("POPUP").W(Px(60)).H(Px(40)).Bg(paintRed),
			)),
		).IntoElement()
	})
	f.draw()
	f.ta.RunUntilParked()
	if got := debugBoundsOf(t, f, "POPUP"); got != logical(240, 228, 60, 40) {
		t.Fatalf("POPUP = %v, want (240,228 60x40)", got)
	}
	cell.Origin = Point{X: 300, Y: 100}
	f.notify()
	f.draw()
	f.ta.RunUntilParked()
	if got := debugBoundsOf(t, f, "POPUP"); got != logical(300, 148, 60, 40) {
		t.Fatalf("POPUP after the cell moved = %v, want (300,148 60x40)", got)
	}
}

// TestAnchoredFollowsAScrolledAnchorAndHidesWhenClipped pins the
// scrolled-anchor behavior and position-visibility: anchors-visible
// (Chromium anchor_position_scroll_data.cc — the popup follows the
// anchor's scroll offset — and anchor_position_visibility_observer.cc —
// the popup hides when the anchor no longer intersects its clipping
// scroller).
func TestAnchoredFollowsAScrolledAnchorAndHidesWhenClipped(t *testing.T) {
	// A 200x300 scroller at (20,40) over 200x800 content; the anchor
	// (60,240,120,40) sits 200px into the content. The popup is a
	// deferred sibling, anchored to it 8px below, center-aligned.
	f := newPaintFixture(t, func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().SizeFull().Flex().Child(
			Div().Flex().ID("scroller").DebugSelector("SCROLLER").
				Absolute().Top(Px(40)).Left(Px(20)).
				W(Px(200)).H(Px(300)).
				OverflowYScroll().
				Child(Div().Flex().DebugSelector("CONTENT").W(Px(200)).H(Px(800)).FlexCol().
					Child(Div().Flex().H(Px(200))).
					Child(Div().Flex().ID("trigger").DebugSelector("ANCHOR").W(Px(120)).H(Px(40)).Bg(paintBlue))),
		).Child(
			Deferred(Anchored().AnchorTo("trigger").Side(AnchorSideBottom).Align(AnchorAlignCenter).Gap(8).HideWhenAnchorClipped().Child(
				Div().Flex().DebugSelector("POPUP").W(Px(100)).H(Px(50)).Bg(paintRed),
			)),
		).IntoElement()
	})
	f.draw()
	f.ta.RunUntilParked()
	if got := debugBoundsOf(t, f, "ANCHOR"); got != logical(20, 240, 120, 40) {
		t.Fatalf("ANCHOR = %v, want (20,240 120x40)", got)
	}
	if got := debugBoundsOf(t, f, "POPUP"); got != logical(30, 288, 100, 50) {
		t.Fatalf("POPUP = %v, want (30,288 100x50)", got)
	}
	if quads := quadsOf(t, f.draw(), paintRed); len(quads) != 1 {
		t.Fatalf("the visible-anchor popup painted %d red quads, want 1", len(quads))
	}

	// Scroll 100px down: the anchor (y=140) still intersects the
	// scroller's visible 40..340, so the popup follows and stays drawn.
	f.window.DispatchInput(&ScrollWheelEvent{
		Position:   Point{X: 100, Y: 100},
		Delta:      ScrollDeltaPixels(0, -100),
		TouchPhase: TouchPhaseMoved,
	}, f.app)
	f.ta.RunUntilParked()
	f.draw()
	if got := debugBoundsOf(t, f, "POPUP"); got != logical(30, 188, 100, 50) {
		t.Fatalf("POPUP after scrolling 100 = %v, want (30,188 100x50): the popup follows the anchor", got)
	}
	if quads := quadsOf(t, f.draw(), paintRed); len(quads) != 1 {
		t.Fatalf("the partially-visible anchor's popup painted %d red quads, want 1", len(quads))
	}

	// Scroll 280px total: the anchor sits fully above the scroller's
	// clip (its bottom edge reaches y=0), the popup follows its box but
	// paints nothing.
	f.window.DispatchInput(&ScrollWheelEvent{
		Position:   Point{X: 100, Y: 100},
		Delta:      ScrollDeltaPixels(0, -180),
		TouchPhase: TouchPhaseMoved,
	}, f.app)
	f.ta.RunUntilParked()
	scene := f.draw()
	if got := debugBoundsOf(t, f, "ANCHOR"); got != logical(20, -40, 120, 40) {
		t.Fatalf("ANCHOR after scrolling 280 = %v, want (20,-40 120x40)", got)
	}
	if got := debugBoundsOf(t, f, "POPUP"); got != logical(30, 8, 100, 50) {
		t.Fatalf("POPUP after scrolling 280 = %v, want (30,8 100x50): the box follows the clipped anchor", got)
	}
	if quads := quadsOf(t, scene, paintRed); len(quads) != 0 {
		t.Fatalf("the clipped anchor's popup painted %d red quads, want 0 (anchors-visible hides it)", len(quads))
	}
}

// TestAnchoredAnchorToMissingAnchorRendersAtTheLaidOutPosition pins the
// missing-anchor behavior: with no recorded anchor yet, the popup keeps
// its laid-out position (the first frame before the anchor's prepaint,
// or a misspelled id).
func TestAnchoredAnchorToMissingAnchorRendersAtTheLaidOutPosition(t *testing.T) {
	f := newPaintFixture(t, func(*paintView, *Window, *Context[paintView]) AnyElement {
		return Div().SizeFull().Flex().Child(
			// No element carries the id "missing" and no anchor cell is
			// set: the popup stays where the layout placed it.
			Deferred(Anchored().AnchorTo("missing").Side(AnchorSideBottom).Align(AnchorAlignStart).Gap(8).Child(
				Div().Flex().DebugSelector("POPUP").W(Px(60)).H(Px(40)).Bg(paintRed),
			)),
		).IntoElement()
	})
	f.draw()
	f.ta.RunUntilParked()
	bounds, ok := WindowDebugBound(f.window, "POPUP")
	if !ok {
		t.Fatal("POPUP debug bounds not found")
	}
	// The deferred anchored node is an absolute child of the root: its
	// children stay at the node's laid-out origin, not at a window-
	// shifted anchor position.
	if bounds.Origin != (Point{}) {
		t.Fatalf("POPUP = %v, want the laid-out origin (0,0)", bounds)
	}
}
