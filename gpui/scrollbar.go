package gpui

import "fmt"

// The overflow: auto scrollbar and scrollbar-gutter slice (browser-core
// ticket 36, spec story 14).
//
// CSS model, verified in the research Chromium checkout (0d4dcd4ffc1d,
// third_party/blink/renderer/core):
//
//   - An `overflow: auto` axis is a SCROLL CONTAINER axis that shows a
//     scrollbar only while its content overflows: scrollbar existence is
//     decided after layout (PaintLayerScrollableArea::
//     ComputeScrollbarExistence, paint/paint_layer_scrollable_area.cc:
//     1852-1861 — `needs_vertical_scrollbar = HasVerticalOverflow() &&
//     VisibleContentRect(...).width()`, where HasVerticalOverflow is
//     scroll size > client size, :1401-1407), and a change relayouts
//     (UpdateAfterOverflowRecalc -> SetNeedsLayoutAndFullPaintInvalidation,
//     :1480-1500).
//   - A shown CLASSIC scrollbar occupies layout space: the scrollbar
//     rect spans the border box's inline-end column inside the border
//     (RectForVerticalScrollbar, paint_layer_scrollable_area.cc:
//     1625-1650), and the content box shrinks by its thickness (the
//     scrollport is the padding box minus the scrollbars).
//   - `scrollbar-gutter: stable` reserves that thickness on the
//     inline-end side even while no scrollbar shows (LayoutBox::
//     HasScrollbarGutters, layout/layout_box.cc:1477-1503 — applies to
//     auto/scroll/hidden overflow, non-overlay; ComputeScrollbarsInternal,
//     :1505-1560).
//
// Port mapping: the reservation rides Taffy's Overflow::Scroll +
// scrollbar_width (the native layout adds the width to the content-box
// inset on the inline-end side, taffy compute/flexbox.rs:459-470 — the
// same arithmetic as a CSS classic scrollbar). The div decides at
// REQUEST time which axes reserve, from the shown flags its PREPAINT
// keeps in the element state: autoOverflowRecordStyle maps a shown (or
// gutter-stable) axis onto Scroll + the scrollbar width, and an unshown
// axis onto Hidden (both zero the automatic minimum size and contain
// the overflow; only Scroll reserves). The first frame after content
// changes lays out unreserved and the prepaint's detection notifies a
// redraw — Chromium's post-layout relayout becomes a one-frame settle
// here (the harness parks frames, so records compare the settled
// frame).
//
// The scrollbar paints in the div's paint, after the children, inside
// the overflow content mask: a track quad over the scrollbar rect and
// a thumb quad at the scroll fraction (thumb length proportional to
// the visible fraction, Chromium's Scrollbar::ThumbLength). The pin
// draws nothing for OverflowScroll (unchanged, a recorded divergence:
// scrollbar painting is the auto policy's; making scroll draw them
// would change existing fixtures' pinned expectations).
//
// The classic width and the track/thumb colours are the Windows
// Chrome measurements of this machine's headless build, read off the
// display fixture's record (internal/parity/fixtures/display).

// ClassicScrollbarWidth is the CSS-pixel thickness of a Windows
// Chrome classic scrollbar — the width an overflow: auto axis uses
// unless ScrollbarWidth overrides it (the pin's ScrollbarWidth default
// is px(0), which reads as "unset" for the auto policy; Chromium's
// default is the native theme's thickness).
const ClassicScrollbarWidth float32 = 15.0

// The Windows Chrome headless scrollbar measurements, read off the
// display fixture's record (internal/parity/testdata/overflow-auto/
// chrome.png): a #fcfcfc track over the whole scrollbar rect, #8b8b8b
// arrow-button glyphs (triangles) centred in WxW buttons at both ends,
// and a #8b8b8b rounded thumb (radius 4) inset 3 from the track
// between the buttons, its length the visible fraction of the slot.
const (
	// ScrollbarTrackColor is the scrollbar track fill.
	ScrollbarTrackColor = 0xfcfcfc
	// ScrollbarThumbColor is the scrollbar thumb and arrow fill.
	ScrollbarThumbColor = 0x8b8b8b
	// scrollbarThumbRadius is the thumb's corner radius (logical px).
	scrollbarThumbRadius = float32(4.0)
	// scrollbarThumbInset contracts the thumb from its track slot on
	// every side.
	scrollbarThumbInset = float32(3.0)
	// scrollbarArrowAcross is the arrow glyph's size across the
	// scrollbar axis; scrollbarArrowAlongV/H its size along the axis
	// (the vertical and horizontal scrollbars measure slightly
	// different glyphs).
	scrollbarArrowAcross = float32(9.0)
	scrollbarArrowAlongV = float32(6.5)
	scrollbarArrowAlongH = float32(7.0)
)

// overflowAxisUserScrolls reports whether an overflow value makes the
// axis respond to user scrolling: scroll always, auto while it
// overflows (the clamp zeroes the range when it does not — so auto
// needs no extra check here), hidden/clip/visible never. The wheel
// listener and the keyboard-scroll default action gate on it.
func overflowAxisUserScrolls(o Overflow) bool {
	return o == OverflowScroll || o == OverflowAuto
}

// overflowAxisScrollable reports whether an overflow value makes the
// axis a SCROLL CONTAINER axis in the CSS sense — everything but
// visible and clip (ComputedStyle::IsScrollContainer's rule,
// style/computed_style.h:1988-1993: "An overflow value of visible or
// clip is not a scroll container, all other values result in a scroll
// container"). Sticky descendants resolve against the nearest such
// container per axis (PaintLayerScrollableArea::ScrollableAxes,
// paint_layer_scrollable_area.cc:2520-2542), and scrollbar-gutter:
// stable applies to these values (layout_box.cc:1485-1503).
func overflowAxisScrollable(o Overflow) bool {
	return o == OverflowScroll || o == OverflowAuto || o == OverflowHidden
}

// styleIsScrollContainer reports whether the style's overflow makes
// the element a scroll container on any axis (an overflow: hidden box
// is one: it clips and is the nearest scroll container for sticky
// descendants — ticket 34's limitation that this ticket owns).
func styleIsScrollContainer(style *Style) bool {
	return overflowAxisScrollable(style.OverflowX) || overflowAxisScrollable(style.OverflowY)
}

// autoScrollbarWidthPx resolves the scrollbar width an overflow: auto
// axis uses: the style's ScrollbarWidth when it is set non-zero, else
// the classic Windows thickness (scrollbar-width: none cannot be
// expressed for the auto policy — the pin's zero default means
// "unset"; a recorded limitation).
func autoScrollbarWidthPx(style *Style, rem float32) float32 {
	if width := style.ScrollbarWidth.ToPixels(rem); width > 0 {
		return width
	}
	return ClassicScrollbarWidth
}

// divAutoScrollbarFlags are the shown-scrollbar flags of one
// overflow: auto element, kept in its scroll element state so the
// request walk of the NEXT frame reserves what this frame's prepaint
// saw overflowing (the one-frame settle documented above).
type divAutoScrollbarFlags struct {
	shownX, shownY bool
}

// update recomputes the flags from the frame's scroll maximum and
// reports whether either changed (Chromium's needs_*_scrollbar test:
// the axis has overflow — maxOffset > 0 — and the container is
// scrollable on it).
func (f *divAutoScrollbarFlags) update(maxOffset Point) bool {
	changed := f.shownX != (maxOffset.X > 0) || f.shownY != (maxOffset.Y > 0)
	f.shownX = maxOffset.X > 0
	f.shownY = maxOffset.Y > 0
	return changed
}

// reserves reports whether an auto axis reserves its scrollbar gutter
// at request time: shown now, or scrollbar-gutter: stable (Chromium:
// ComputeScrollbarsInternal reserves the hypothetical thickness for a
// stable gutter, layout_box.cc:1518-1536).
func autoAxisReserves(style *Style, axis Overflow, shown bool) bool {
	if axis != OverflowAuto {
		return false
	}
	return shown || style.ScrollbarGutter == ScrollbarGutterStable
}

// autoOverflowRecordStyle copies style for the NODE REQUEST of an
// element with overflow: auto axes, folding each auto axis onto the
// record value Taffy needs: a reserving axis becomes Scroll WITH the
// scrollbar width (Taffy's content-box inset reservation, exactly a
// CSS classic scrollbar's space), a non-reserving axis becomes Hidden
// (contain without reservation). It reports whether anything changed
// (the caller skips the copy otherwise). Only the Overflow pair and
// ScrollbarWidth are adjusted — the prepaint and paint keep resolving
// the real style, whose OverflowAuto value they gate on. rem is the
// ambient rem size of the request walk.
func autoOverflowRecordStyle(style *Style, flags *divAutoScrollbarFlags, rem float32) (Style, bool) {
	adjustX := autoAxisReserves(style, style.OverflowX, flags != nil && flags.shownX)
	adjustY := autoAxisReserves(style, style.OverflowY, flags != nil && flags.shownY)
	if !adjustX && !adjustY {
		return Style{}, false
	}
	out := *style
	if adjustX {
		out.OverflowX = OverflowScroll
	} else if out.OverflowX == OverflowAuto {
		out.OverflowX = OverflowHidden
	}
	if adjustY {
		out.OverflowY = OverflowScroll
	} else if out.OverflowY == OverflowAuto {
		out.OverflowY = OverflowHidden
	}
	// Both auto axes share one width (a classic scrollbar pair is the
	// same thickness on both axes, ScrollbarThemeAura::ScrollbarThickness).
	out.ScrollbarWidth = AbsoluteLength{Kind: AbsoluteLengthPx, Value: autoScrollbarWidthPx(style, rem)}
	return out, true
}

// ---------------------------------------------------------------------------
// Paint-time geometry
// ---------------------------------------------------------------------------

// scrollbarAxisFacts is one shown scrollbar's paint geometry, computed
// at prepaint in window coordinates.
type scrollbarAxisFacts struct {
	// track is the scrollbar's rect (the border box's inline-end/bottom
	// column inside the border, contracted by the opposite scrollbar's
	// corner).
	track Bounds
	// thumb is the thumb rect inside the track: the length is the
	// track's port fraction (Chromium's proportional thumb), the
	// position the scroll fraction.
	thumb Bounds
}

// autoScrollbarFacts are the shown auto scrollbars of one scroller.
type autoScrollbarFacts struct {
	vertical, horizontal scrollbarAxisFacts
}

// autoScrollbarPaint computes the paint geometry for the shown flags:
// bounds is the scroller's border box, style its computed style, rem
// the ambient rem, maxOffset and offset the clamp's results (negative
// offsets scroll down/right), and contentSize the scroller's content
// size. Each shown axis paints the measured Chrome model: the track is
// the scrollbar rect (the border box's inline-end/bottom column inside
// the border, contracted by the opposite scrollbar's corner), the two
// WxW button areas at its ends carry the arrow glyphs, and the thumb
// lives in the slot between them — the usable track inset on every
// side — with the length the visible fraction of the slot (Chromium's
// proportional thumb) and the position the scroll fraction.
func autoScrollbarPaint(style *Style, bounds Bounds, rem float32, maxOffset, offset Point, contentSize Size, flags *divAutoScrollbarFlags) *autoScrollbarFacts {
	if flags == nil || (!flags.shownX && !flags.shownY) {
		return nil
	}
	borders := style.BorderWidths.ToPixels(rem)
	width := autoScrollbarWidthPx(style, rem)
	facts := &autoScrollbarFacts{}
	if flags.shownY {
		// The vertical scrollbar: the border box's right column inside
		// the border, the height contracted by the horizontal
		// scrollbar's corner when both show (RectForVerticalScrollbar).
		corner := float32(0)
		if flags.shownX {
			corner = width
		}
		track := Bounds{
			Origin: Point{X: bounds.Right() - borders.Right - width, Y: bounds.Origin.Y + borders.Top},
			Size: Size{
				Width:  width,
				Height: bounds.Size.Height - borders.Top - borders.Bottom - corner,
			},
		}
		facts.vertical = scrollbarAxisFacts{
			track: track,
			thumb: scrollbarThumb(track, maxOffset.Y, offset.Y, contentSize.Height, bounds.Size.Height-borders.Top-borders.Bottom, true),
		}
	}
	if flags.shownX {
		corner := float32(0)
		if flags.shownY {
			corner = width
		}
		track := Bounds{
			Origin: Point{X: bounds.Origin.X + borders.Left, Y: bounds.Bottom() - borders.Bottom - width},
			Size: Size{
				Width:  bounds.Size.Width - borders.Left - borders.Right - corner,
				Height: width,
			},
		}
		facts.horizontal = scrollbarAxisFacts{
			track: track,
			thumb: scrollbarThumb(track, maxOffset.X, offset.X, contentSize.Width, bounds.Size.Width-borders.Left-borders.Right, false),
		}
	}
	return facts
}

// scrollbarThumb places one axis's thumb inside its track: the usable
// track between the two WxW button areas, inset scrollbarThumbInset on
// every side, is the thumb's slot; the length is the slot's port
// fraction of the content (clamped to the slot), the position the
// scroll fraction (offset is negative when scrolled down/right, like
// the element-scroll cell).
func scrollbarThumb(track Bounds, maxOffset, offset, contentExtent, portLen float32, vertical bool) Bounds {
	width := track.Size.Width
	trackLen := track.Size.Height
	if !vertical {
		width = track.Size.Height
		trackLen = track.Size.Width
	}
	button := width
	slotStart := button + scrollbarThumbInset
	slotLen := trackLen - 2*button - 2*scrollbarThumbInset
	thumbLen := slotLen * portLen / contentExtent
	if thumbLen > slotLen {
		thumbLen = slotLen
	}
	if thumbLen < scrollbarThumbInset {
		thumbLen = scrollbarThumbInset
	}
	thumbStart := slotStart
	if maxOffset > 0 {
		fraction := -offset / maxOffset
		if fraction < 0 {
			fraction = 0
		}
		if fraction > 1 {
			fraction = 1
		}
		thumbStart = slotStart + fraction*(slotLen-thumbLen)
	}
	if vertical {
		return Bounds{
			Origin: Point{X: track.Origin.X + scrollbarThumbInset, Y: track.Origin.Y + thumbStart},
			Size:   Size{Width: width - 2*scrollbarThumbInset, Height: thumbLen},
		}
	}
	return Bounds{
		Origin: Point{X: track.Origin.X + thumbStart, Y: track.Origin.Y + scrollbarThumbInset},
		Size:   Size{Width: thumbLen, Height: width - 2*scrollbarThumbInset},
	}
}

// paintAutoScrollbar paints the scroller's auto scrollbar pair after
// its children (inside the content mask opened around them — the
// scrollbar lives inside the clip; scrollbars paint above the content
// they overlay, Chromium's scrollbar layer). Each axis paints the
// measured Chrome look: the track fill, the two arrow glyphs in the
// button areas at the ends, and the rounded thumb.
func paintAutoScrollbar(w *Window, facts *autoScrollbarFacts) {
	if facts == nil {
		return
	}
	frame := currentFrame(w)
	paint := func(track, thumb Bounds, vertical bool) {
		if track.Size.Width <= 0 || track.Size.Height <= 0 {
			return
		}
		fill := Fill(track, SolidBackground(Rgb(ScrollbarTrackColor)))
		if err := frame.scene.PaintQuad(fill, 0, frame.paint); err != nil {
			panic(fmt.Sprintf("gpui: scrollbar track paint: %v", err))
		}
		paintScrollbarArrow(w, track, true, vertical)
		paintScrollbarArrow(w, track, false, vertical)
		if thumb.Size.Width > 0 && thumb.Size.Height > 0 {
			thumbQuad := Fill(thumb, SolidBackground(Rgb(ScrollbarThumbColor)))
			thumbQuad.CornerRadii = Corners{
				TopLeft:     scrollbarThumbRadius,
				TopRight:    scrollbarThumbRadius,
				BottomRight: scrollbarThumbRadius,
				BottomLeft:  scrollbarThumbRadius,
			}
			if err := frame.scene.PaintQuad(thumbQuad, 0, frame.paint); err != nil {
				panic(fmt.Sprintf("gpui: scrollbar thumb paint: %v", err))
			}
		}
	}
	paint(facts.vertical.track, facts.vertical.thumb, true)
	paint(facts.horizontal.track, facts.horizontal.thumb, false)
}

// paintScrollbarArrow paints one button glyph — a filled triangle
// pointing along the scrollbar axis (start = up/left, end = down/right)
// — centred in the WxW button area at the track's start or end, the
// measured Chrome headless glyph.
func paintScrollbarArrow(w *Window, track Bounds, start, vertical bool) {
	frame := currentFrame(w)
	width := track.Size.Width
	if !vertical {
		width = track.Size.Height
	}
	along := scrollbarArrowAlongV
	if !vertical {
		along = scrollbarArrowAlongH
	}
	// The button area: the first/last WxW square of the track.
	var button Bounds
	if vertical {
		button = Bounds{
			Origin: Point{X: track.Origin.X, Y: track.Origin.Y},
			Size:   Size{Width: width, Height: width},
		}
		if !start {
			button.Origin.Y = track.Bottom() - width
		}
	} else {
		button = Bounds{
			Origin: Point{X: track.Origin.X, Y: track.Origin.Y},
			Size:   Size{Width: width, Height: width},
		}
		if !start {
			button.Origin.X = track.Right() - width
		}
	}
	cx := button.Origin.X + button.Size.Width/2
	cy := button.Origin.Y + button.Size.Height/2
	halfAcross := scrollbarArrowAcross / 2
	halfAlong := along / 2
	var path *PathBuilder
	if vertical {
		// Up (start) or down (end) pointing: apex at the button's
		// outer edge, base across the centre.
		apexY := cy - halfAlong
		baseY := cy + halfAlong
		if !start {
			apexY, baseY = cy+halfAlong, cy-halfAlong
		}
		path = FillPathBuilder().
			MoveTo(cx, apexY).
			LineTo(cx-halfAcross, baseY).
			LineTo(cx+halfAcross, baseY).
			Close()
	} else {
		apexX := cx - halfAlong
		baseX := cx + halfAlong
		if !start {
			apexX, baseX = cx+halfAlong, cx-halfAlong
		}
		path = FillPathBuilder().
			MoveTo(apexX, cy).
			LineTo(baseX, cy-halfAcross).
			LineTo(baseX, cy+halfAcross).
			Close()
	}
	tri, err := path.Build()
	if err != nil {
		panic(fmt.Sprintf("gpui: scrollbar arrow path: %v", err))
	}
	if err := frame.scene.PaintPath(tri, SolidBackground(Rgb(ScrollbarThumbColor)), frame.paint); err != nil {
		panic(fmt.Sprintf("gpui: scrollbar arrow paint: %v", err))
	}
}

// recordAutoScrollbarFacts captures one scroller's shown pair into the
// frame under construction (the observable behind
// WindowAutoScrollbarFacts).
func recordAutoScrollbarFacts(w *Window, facts *autoScrollbarFacts) {
	if facts == nil {
		return
	}
	frame := currentFrame(w)
	frame.autoScrollbarFacts = append(frame.autoScrollbarFacts, AutoScrollbarWindowFacts{
		VerticalShown:   facts.vertical.track.Size.Height > 0 || facts.vertical.track.Size.Width > 0,
		HorizontalShown: facts.horizontal.track.Size.Height > 0 || facts.horizontal.track.Size.Width > 0,
		VerticalTrack:   facts.vertical.track,
		VerticalThumb:   facts.vertical.thumb,
		HorizontalTrack: facts.horizontal.track,
		HorizontalThumb: facts.horizontal.thumb,
	})
}

// AutoScrollbarWindowFacts are the observable facts of one scroller's
// shown overflow: auto scrollbar pair of the last completed frame
// (the port's test-support surface, like WindowInlineFacts; the
// reference exposes none — it draws scrollbars through the platform
// scrollbar layer).
type AutoScrollbarWindowFacts struct {
	// VerticalShown and HorizontalShown report which axes show a
	// scrollbar.
	VerticalShown, HorizontalShown bool
	// VerticalTrack and VerticalThumb are the vertical pair's rects in
	// window coordinates.
	VerticalTrack, VerticalThumb Bounds
	// HorizontalTrack and HorizontalThumb are the horizontal pair's.
	HorizontalTrack, HorizontalThumb Bounds
}

// WindowAutoScrollbarFacts returns the last completed frame's shown
// overflow: auto scrollbar pairs, in prepaint order. A window without
// auto scrollers returns nil.
func WindowAutoScrollbarFacts(w *Window) []AutoScrollbarWindowFacts {
	ds, ok := windowDrawStates[w]
	if !ok {
		return nil
	}
	return ds.lastAutoScrollbarFacts
}
