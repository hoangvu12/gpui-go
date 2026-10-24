//go:build windows

// Package anchoring is browser-core ticket 46's fixture for scroll
// anchoring: a scroller at the end of its range, then a row inserted
// above the viewport — the visible content stays in place because the
// scroll offset follows the insertion, exactly as Chrome's scroll
// anchoring adjusts it.
//
// The page is text-free geometry (the boxpaint discipline). The
// insertion is the harness's page-script step (parity.Script): the
// recorder evaluates the page's insertBefore on the settled page and
// the replay flips the view's per-run state and refreshes, so both
// sides make the same change after the End-key scroll.
//
// Chromium's rules the fixture pins (core/layout/scroll_anchor.cc):
//
//   - the anchor node is the first DOM-order box whose rect intersects
//     the scrollport — the DFS descends past the partially-visible
//     #rows wrapper to the first visible row (FindAnchor /
//     FindAnchorRecursive, :397-475, Examine, :360-395);
//   - NotifyBeforeLayout clears the anchor at a zero block offset
//     (:621-626) — anchoring never fires from rest;
//   - Adjust moves the offset by the anchor's rounded content-space
//     delta, block axis only (:713-760, ComputeAdjustment, :653-695).
package anchoring

import (
	"fmt"
	"sync"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers, shared by the HTML and the gpui view.
const (
	windowWidth  = 320.0
	windowHeight = 240.0

	scrollerW = 240.0
	scrollerH = 200.0
	scrollerX = 10.0
	scrollerY = 10.0

	rowH = 50.0
	// rowsCount is the number of plain rows after the focus row: 9 rows
	// of 50px = 450px of content, scroll max 250.
	rowsCount = 8
)

// The palette, shared by the page and the view.
const (
	pageBackground = 0xffffff
	scrollerBg     = 0xf1f5f9
	focusBg        = 0xfca5a5
	rowBg          = 0xcbd5e1
	insertedBg     = 0x2563eb
)

// boxIDs are the compared boxes: the scroller, the rows around the
// anchor (r6 is the anchor row at the scroll end), and the inserted row.
var boxIDs = []string{"scroller", "r5", "r6", "r7", "r8", "inserted"}

// insertScript is the page half of the insertion step: a 50px row
// inserted at the top of #rows, above the whole scrolled viewport.
const insertScript = `(() => {
	const rows = document.getElementById('rows');
	const r = document.createElement('div');
	r.id = 'inserted';
	r.setAttribute('data-id', 'inserted');
	r.className = 'row';
	rows.insertBefore(r, rows.firstChild);
	return 'inserted';
})()`

// Fixture is the scroll-anchoring fixture: scroll to the end (the End
// key over a clicked focus row), then insert a row above the viewport.
// Chrome's record carries the adjusted offset — the anchor row r6 stays
// at the scrollport's top edge (y = 10) after the insertion — where a
// port without anchoring leaves the offset at 250 and every visible box
// lands 50px low.
var Fixture = parity.Fixture{
	Name:   "anchoring",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	Steps: []parity.Step{
		parity.Click(scrollerX+scrollerW/2, scrollerY+rowH/2),
		parity.Key("end"),
		parity.Script(insertScript, insertRow),
	},
	BoxIDs:         boxIDs,
	ViewWithWindow: View,
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run); flat-colour
		// geometry, pixel-exact at the 2.0 device scale (verified by
		// anchoring_test.go).
		MaxMeanChannelDiff:    0.02,
		MaxROIMeanChannelDiff: 0.1,
		MaxHotPixelFraction:   0.002,
	},
}

func init() {
	parity.Register(Fixture)
}

// The per-run insertion state, keyed on the run's event log (the
// transition fixture's pattern: the second Replay of the fixture — the
// tolerance check — starts uninserted again).
type state struct {
	inserted bool
}

var (
	stateMu    sync.Mutex
	stateByLog = map[*parity.EventLog]*state{}
)

func stateOf(log *parity.EventLog) *state {
	stateMu.Lock()
	defer stateMu.Unlock()
	s, ok := stateByLog[log]
	if !ok {
		s = &state{}
		stateByLog[log] = s
	}
	return s
}

// insertRow is the Go half of the insertion step: flip the view's state
// and refresh, so the next frame's request walk sees the new row and
// the scroller's prepaint anchors against it.
func insertRow(log *parity.EventLog, w *gpui.Window, _ *gpui.App) {
	stateOf(log).inserted = true
	w.Refresh()
}

// The per-window focus handle (the sticky fixtures' scrollFocus
// pattern: the handle lives outside the view function so its identity
// survives the per-frame rebuilds).
var (
	focusMu    sync.Mutex
	focusByWin = map[*gpui.Window]gpui.FocusHandle{}
)

func focusOf(log *parity.EventLog, w *gpui.Window) gpui.FocusHandle {
	focusMu.Lock()
	defer focusMu.Unlock()
	if focus, ok := focusByWin[w]; ok {
		return focus
	}
	focus := gpui.NewFocusHandle(w).TabStop(true)
	focusByWin[w] = focus
	log.TrackFocus(focus, "focus")
	return focus
}

// f2s renders one shared float constant for the page's CSS.
func f2s(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

// page is the fixture's HTML.
var page = `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>
	html, body {
		margin: 0;
		padding: 0;
		background: #ffffff;
	}
	body {
		width: ` + f2s(windowWidth) + `px;
		height: ` + f2s(windowHeight) + `px;
		display: flex;
	}
	#scroller {
		box-sizing: border-box;
		width: ` + f2s(scrollerW) + `px;
		height: ` + f2s(scrollerH) + `px;
		margin-left: ` + f2s(scrollerX) + `px;
		margin-top: ` + f2s(scrollerY) + `px;
		display: flex;
		flex-direction: column;
		overflow-y: scroll;
		/* The port draws no scrollbars and reserves no gutter. */
		scrollbar-width: none;
		background: #f1f5f9;
	}
	#rows {
		display: flex;
		flex-direction: column;
	}
	#rows > div {
		box-sizing: border-box;
		width: ` + f2s(scrollerW) + `px;
		height: ` + f2s(rowH) + `px;
		flex-shrink: 0;
	}
	#focus {
		background: #fca5a5;
		outline: none;
	}
	.row {
		background: #cbd5e1;
	}
	#inserted {
		background: #2563eb;
	}
</style>
</head>
<body>
	<div id="scroller" data-id="scroller">
		<div id="rows">
			<div id="focus" data-id="focus" tabindex="0"></div>
			<div class="row" data-id="r2"></div>
			<div class="row" data-id="r3"></div>
			<div class="row" data-id="r4"></div>
			<div class="row" data-id="r5"></div>
			<div class="row" data-id="r6"></div>
			<div class="row" data-id="r7"></div>
			<div class="row" data-id="r8"></div>
			<div class="row" data-id="r9"></div>
		</div>
	</div>
</body>
</html>`

// View is the fixture's gpui twin.
func View(log *parity.EventLog, w *gpui.Window, _ *gpui.App) gpui.AnyElement {
	s := stateOf(log)
	rows := gpui.Div().
		ID("rows").
		FlexCol()
	if s.inserted {
		rows = rows.Child(gpui.Div().
			ID("inserted").
			DebugSelector("inserted").
			Size(gpui.Px(scrollerW), gpui.Px(rowH)).
			FlexShrink0().
			Bg(gpui.Rgb(insertedBg)))
	}
	rows = rows.Child(gpui.Div().
		ID("focus").
		DebugSelector("focus").
		Size(gpui.Px(scrollerW), gpui.Px(rowH)).
		FlexShrink0().
		TrackFocus(focusOf(log, w)).
		Bg(gpui.Rgb(focusBg)))
	for i := 2; i <= rowsCount+1; i++ {
		rows = rows.Child(gpui.Div().
			ID(fmt.Sprintf("r%d", i)).
			DebugSelector(fmt.Sprintf("r%d", i)).
			Size(gpui.Px(scrollerW), gpui.Px(rowH)).
			FlexShrink0().
			Bg(gpui.Rgb(rowBg)))
	}
	scroller := gpui.Div().
		ID("scroller").
		DebugSelector("scroller").
		Size(gpui.Px(scrollerW), gpui.Px(scrollerH)).
		Ml(gpui.Px(scrollerX)).
		Mt(gpui.Px(scrollerY)).
		FlexCol().
		OverflowYScroll().
		Bg(gpui.Rgb(scrollerBg)).
		Child(rows)
	return gpui.Div().
		ID("root").
		SizeFull().
		Flex().
		Bg(gpui.Rgb(pageBackground)).
		Child(scroller).
		IntoElement()
}
