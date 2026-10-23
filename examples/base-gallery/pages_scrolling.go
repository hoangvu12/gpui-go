package main

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

var scrollingPages = []page{
	{"scrollbar", "Scrollbar", "scrollbar.rs", (*gallery).scrollbarPage},
	{"virtual-list", "VirtualList", "virtual_list.rs", (*gallery).virtualListPage},
}

const virtualRows = 10_000

type scrollingState struct {
	activityScroll gpui.ScrollHandle
	gridScroll     gpui.ScrollHandle
	hoverScroll    gpui.ScrollHandle
	rowsScroll     base.VirtualListScrollHandle
	chipsScroll    base.VirtualListScrollHandle
	rowSizes       []gpui.Size
	chipSizes      []gpui.Size
}

func (g *gallery) initScrolling(_ *gpui.Window, _ *gpui.Context[gallery]) {
	g.activityScroll = gpui.NewScrollHandle()
	g.gridScroll = gpui.NewScrollHandle()
	g.hoverScroll = gpui.NewScrollHandle()
	g.rowsScroll = base.NewVirtualListScrollHandle()
	g.chipsScroll = base.NewVirtualListScrollHandle()
	g.rowSizes = make([]gpui.Size, virtualRows)
	for ix := range g.rowSizes {
		g.rowSizes[ix] = gpui.Size{Width: 420, Height: 32}
	}
	g.chipSizes = make([]gpui.Size, 500)
	for ix := range g.chipSizes {
		// Variable widths: the list measures each item from this table.
		g.chipSizes[ix] = gpui.Size{Width: float32(70 + (ix%5)*18), Height: 40}
	}
}

// activityRows builds n rows with one text run each: the native text
// service holds a bounded number of live shaped lines (256, across the
// current and the previous frame), so a page keeps its text count low.
func activityRows(n int) *gpui.DivElement {
	rows := gpui.Div().WFull().Flex().FlexCol()
	for r := 1; r <= n; r++ {
		status := "pending"
		dot := pal.series[2]
		if r%3 == 0 {
			status, dot = "completed", pal.series[1]
		}
		rows = rows.Child(gpui.Div().H8().Px3().Flex().ItemsCenter().Gap2().
			BorderB1().BorderColor(pal.border).TextXs().
			Child(gpui.Div().Size2().FlexNone().RoundedFull().Bg(dot)).
			Child(fmt.Sprintf("Activity %d · %s", r, status)))
	}
	return rows
}

func (g *gallery) scrollbarPage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	region := func(id string, handle gpui.ScrollHandle, both bool, child any, bar *base.Scrollbar) any {
		d := gpui.Div().ID(id).Relative().Flex().FlexCol().W(gpui.Px(360)).H(gpui.Px(200)).
			Border1().BorderColor(pal.border).RoundedLg().Bg(pal.surface)
		if both {
			d = d.OverflowScroll()
		} else {
			d = d.OverflowYScroll()
		}
		return d.TrackScroll(handle).Child(child).Child(bar)
	}
	// An explicit width: as a flex item the grid would otherwise stretch
	// to the viewport and never overflow horizontally.
	grid := gpui.Div().W(gpui.Px(12*72 + 40)).FlexNone().P3().Flex().FlexCol().Gap2()
	for r := range 12 {
		line := gpui.Div().Flex().ItemsCenter().Gap2().
			Child(gpui.Div().W6().FlexNone().TextXs().TextColor(pal.muted).Child(itoa(r + 1)))
		for c := range 12 {
			line = line.Child(gpui.Div().W(gpui.Px(64)).H8().FlexNone().RoundedMd().
				Bg(pal.series[(r+c)%len(pal.series)].Opacity(0.12 + 0.05*float32(c%4))))
		}
		grid = grid.Child(line)
	}
	styled := base.NewScrollbar(g.hoverScroll).
		Mode(base.ScrollbarModeHover).
		Styles(func(st base.ScrollbarStyles) base.ScrollbarStyles {
			return st.
				Thumb(func(t base.ScrollbarThumbStyle) base.ScrollbarThumbStyle {
					return t.Bg(pal.accent.Opacity(0.5)).Width(6).Radius(3).Inset(2)
				}).
				ThumbHover(func(t base.ScrollbarThumbStyle) base.ScrollbarThumbStyle {
					return t.Bg(pal.accent).Width(10).Radius(5)
				}).
				TrackHover(func(t base.ScrollbarTrackStyle) base.ScrollbarTrackStyle {
					return t.Bg(pal.elevated)
				})
		})
	return col(
		demo("Vertical, always visible", region("scroll-activity", g.activityScroll, false, activityRows(24),
			base.NewScrollbar(g.activityScroll).Mode(base.ScrollbarModeAlways))),
		demo("Both axes (scroll a 12 × 12 grid)", region("scroll-grid", g.gridScroll, true, grid,
			base.NewScrollbar(g.gridScroll).Axis(base.ScrollbarAxisBoth).Mode(base.ScrollbarModeAlways))),
		demo("Shown on hover, styled thumb", region("scroll-hover", g.hoverScroll, false, activityRows(24), styled)),
		note("The page itself scrolls inside a base.Scrollbar in the default scrolling mode."),
	)
}

func (g *gallery) virtualListPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	rows := base.VVirtualList(cx.Entity(), "virtual-rows", g.rowSizes,
		func(_ *gallery, start, end int, _ *gpui.Window, _ *gpui.Context[gallery]) []gpui.AnyElement {
			out := make([]gpui.AnyElement, 0, end-start)
			for ix := start; ix < end; ix++ {
				out = append(out, gpui.Div().WFull().H8().Px3().Flex().ItemsCenter().JustifyBetween().
					BorderB1().BorderColor(pal.border).TextXs().
					Child(gpui.Div().Flex().ItemsCenter().Gap2().
						Child(gpui.Div().Size(gpui.Px(20)).FlexNone().Flex().ItemsCenter().JustifyCenter().
							RoundedFull().Bg(pal.series[ix%len(pal.series)].Opacity(0.2)).Child(itoa(ix%9+1))).
						Child(fmt.Sprintf("Customer %05d", ix+1))).
					Child(gpui.Div().TextColor(pal.muted).Child(fmt.Sprintf("ID-%06d", 100_000+ix))).
					IntoElement())
			}
			return out
		}).
		TrackScroll(g.rowsScroll).
		SizeFull()
	chips := base.HVirtualList(cx.Entity(), "virtual-chips", g.chipSizes,
		func(_ *gallery, start, end int, _ *gpui.Window, _ *gpui.Context[gallery]) []gpui.AnyElement {
			out := make([]gpui.AnyElement, 0, end-start)
			for ix := start; ix < end; ix++ {
				out = append(out, gpui.Div().HFull().Py1().Pr2().
					Child(gpui.Div().HFull().Px2().Flex().ItemsCenter().JustifyCenter().
						TextXs().RoundedFull().Border1().BorderColor(pal.border).Bg(pal.surface).
						Child(fmt.Sprintf("tag-%d", ix+1))).
					IntoElement())
			}
			return out
		}).
		TrackScroll(g.chipsScroll).
		SizeFull()
	jump := func(label string, ix int) any {
		handle := g.rowsScroll
		return btn("jump-"+label, label, outline, sm).OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, _ *gpui.App) {
			handle.ScrollToItem(ix, gpui.ScrollTop)
			w.Refresh()
		})
	}
	return col(
		demo(fmt.Sprintf("Vertical: %d rows, only the visible ones are built", virtualRows), col(
			gpui.Div().Relative().W(gpui.Px(440)).H(gpui.Px(280)).OverflowHidden().
				Border1().BorderColor(pal.border).RoundedLg().Bg(pal.surface).
				Child(rows).
				Child(base.ScrollbarVertical(g.rowsScroll).Mode(base.ScrollbarModeAlways)),
			row(jump("Top", 0), jump("Row 5,000", 4999), jump("Last row", virtualRows-1)),
		)),
		demo("Horizontal: 500 items of varying width", gpui.Div().Relative().W(gpui.Px(440)).H(gpui.Px(52)).
			OverflowHidden().Border1().BorderColor(pal.border).RoundedLg().Px1().
			Child(chips).
			Child(base.ScrollbarHorizontal(g.chipsScroll))),
	)
}
