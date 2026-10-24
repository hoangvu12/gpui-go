//go:build windows

// Package userselect is browser-core ticket 20's fixture set: text
// selectable by default (spec D7) — a plain drag across the text of an
// ordinary div with NO opt-in (no TextSelectionLayer, no
// SelectableText), and the user-select: none opt-out (a drag across the
// same text selecting nothing) — recorded in headless Chrome and
// replayed against the automatic registration (gpui/user_select.go).
//
// The comparison is the harness's three tiers plus the selection read
// (Tier B-like): the page's window.getSelection() — anchor/focus offsets
// within the text node and the selected text — is recorded through the
// fixture's ComputedScript, and the replay's model is read back through
// the window's selected text (the on-demand projection the copy path
// uses). Both sides' offsets are byte offsets of the same ASCII sample.
//
// The cursor is the resolution the harness cannot see (the OS cursor is
// not in the screenshot): the page records the paragraph's computed
// cursor (CSS "auto" in both fixtures — the I-beam is auto's RESOLUTION
// over selectable text), and the capture logs the replay's resolved
// cursor (WindowCursorStyle). The unit tests pin the resolution rule
// (gpui/user_select_test.go: the I-beam over selectable text, the arrow
// over select-none, an explicit cursor winning — Chromium's
// SelectAutoCursor, input/event_handler.cc:839-847, and
// ShouldShowIBeamForNode at :563-573).
package userselect

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers, shared by the HTML and the gpui view (the tracer
// pattern): body 640x400 flex-centering a 380-wide paragraph of 16px/25px
// Segoe UI — the selection fixture's geometry (the drag lands mid-word
// on both sides), over the plain Text element (Div().Child("…")) rather
// than SelectableText: the point of the ticket is that the ordinary div
// needs no opt-in.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	paragraphWidth = 380.0

	textSize       = 16.0
	textLineHeight = 25.0

	pageBackground = 0xffffff
	textColor      = 0xdc2626
)

// dragText is the fixtures' body: four words wide enough that the press
// and the release land mid-word (the selection fixture's sample).
const dragText = "alpha beta gamma delta"

// The drag: the press mid-"beta", the release mid-"gamma" (the selection
// fixture's coordinates — the paragraph is flex-centered, so x is the
// same in both sides' layouts and lands mid-word in both).
const (
	paragraphX = (windowWidth - paragraphWidth) / 2

	dragPressX   = paragraphX + 70
	dragReleaseX = paragraphX + 130
)

// lineY returns the window y of the single 25px line's middle for a
// one-line paragraph flex-centered in the 400px window.
func lineY() float32 {
	return (windowHeight-textLineHeight)/2 + textLineHeight/2
}

// Fixture is the plain-drag fixture: a drag from mid-"beta" to
// mid-"gamma" selects the characters between the two carets, with no
// opt-in anywhere in the view.
var Fixture = parity.Fixture{
	Name:   "userselect-drag",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(false),
	Steps: []parity.Step{
		parity.Move(dragPressX, lineY()),
		parity.Down(dragPressX, lineY(), parity.ButtonLeft),
		parity.Move(dragReleaseX, lineY()),
		parity.Up(dragReleaseX, lineY(), parity.ButtonLeft),
		parity.Script(readSelectionScript, capture),
	},
	BoxIDs:         []string{"root", "text"},
	ComputedScript: computedScript,
	View:           view,
	Configure:      configureWashOf("userselect-drag"),
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set at 2x
		// the selection fixtures' measured correct-view statistics — the
		// same wash-over-text floor they carry (the port keeps the
		// selected text's own colour over the translucent wash where
		// Chrome paints HighlightText) plus the textpaint ink noise.
		MaxMeanChannelDiff:    0.25,
		MaxROIMeanChannelDiff: 6.5,
		MaxHotPixelFraction:   0.095,
	},
}

// NoneFixture is the opt-out fixture: the same page with user-select:
// none on the paragraph, the same drag — and nothing is selected on
// either side (a mousedown on non-selectable content starts no
// selection, selection_controller.cc:122-127).
var NoneFixture = parity.Fixture{
	Name:   "userselect-none",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(true),
	Steps: []parity.Step{
		parity.Move(dragPressX, lineY()),
		parity.Down(dragPressX, lineY(), parity.ButtonLeft),
		parity.Move(dragReleaseX, lineY()),
		parity.Up(dragReleaseX, lineY(), parity.ButtonLeft),
		parity.Script(readSelectionScript, capture),
	},
	BoxIDs:         []string{"root", "text"},
	ComputedScript: computedScript,
	View:           noneView,
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set at 2x
		// the measured correct-view statistics: whole 0.0763 / hot
		// 0.0287, text ROI 2.0466 / hot 0.0274 — the textpaint fixtures'
		// rasterization noise over the same red-on-white 16px text.
		MaxMeanChannelDiff:    0.16,
		MaxROIMeanChannelDiff: 4.2,
		MaxHotPixelFraction:   0.06,
	},
}

func init() {
	parity.Register(Fixture)
	parity.Register(NoneFixture)
}

// ---------------------------------------------------------------------------
// The page
// ---------------------------------------------------------------------------

// page is the HTML twin of the view: a centered 380px paragraph of
// 16px/25px Segoe UI. none controls the paragraph's user-select (the
// second fixture opts out; the first is the default auto).
func page(none bool) string {
	userSelect := ""
	if none {
		userSelect = "\n\t\tuser-select: none;"
	}
	return `<!doctype html>
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
		align-items: center;
		justify-content: center;
	}
	p {
		box-sizing: border-box;
		width: ` + f2s(paragraphWidth) + `px;
		margin: 0;
		color: #dc2626;
		font: 16px/25px "Segoe UI", system-ui, sans-serif;` + userSelect + `
	}
	.probe {
		display: none;
	}
</style>
</head>
<body data-id="root">
	<p data-id="text">` + dragText + `</p>
	<i class="probe" data-color-probe="selection" style="background: Highlight"></i>
</body>
</html>
`
}

// f2s renders a shared float constant for the page's CSS.
func f2s(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

// computedScript reads the page's final state into the record: the
// selection (the anchor and focus offsets within the text node, the
// selected text, whether both endpoints sit in the paragraph's text
// node), the paragraph's computed user-select and cursor (the CSS
// values — "auto" for the cursor in both fixtures, the resolution being
// the I-beam the harness cannot see), and the ::selection wash colour
// the browser resolved (the selection fixture's probe pattern).
const computedScript = `JSON.stringify((() => {
	const s = window.getSelection();
	const nodeOf = n => {
		if (!n) return "";
		if (n.nodeType === 3 && n.parentElement) return n.parentElement.dataset.id || "";
		return n.dataset ? (n.dataset.id || "") : "";
	};
	const c = getComputedStyle(document.querySelector('[data-color-probe="selection"]')).backgroundColor;
	const m = c.match(/^rgba?\((\d+), (\d+), (\d+)(?:, ([\d.]+))?\)$/);
	if (!m) throw new Error('selection probe is not rgb(): ' + c);
	const h = n => Number(n).toString(16).padStart(2, '0');
	let wash = '#' + h(m[1]) + h(m[2]) + h(m[3]);
	if (m[4] !== undefined) wash += h(Math.round(parseFloat(m[4]) * 255));
	const p = document.querySelector('[data-id="text"]');
	return {
		anchor: String(s.anchorOffset),
		focus: String(s.focusOffset),
		text: String(s),
		anchorNode: nodeOf(s.anchorNode),
		focusNode: nodeOf(s.focusNode),
		userSelect: getComputedStyle(p).userSelect,
		cursor: getComputedStyle(p).cursor,
		wash: wash,
	};
})())`

// readSelectionScript is the capture step's page half: a harmless read
// (the record's own selection values come from the ComputedScript).
const readSelectionScript = `window.__selectionRead = String(window.getSelection())`

// ---------------------------------------------------------------------------
// The replay side
// ---------------------------------------------------------------------------

// view builds the fixture's tree: the root over the 380px text block —
// a plain Text element (Div().Child("…")), with no selection layer and
// no SelectableText: the registration is the automatic one
// (gpui/user_select.go). The cursor is automatic (the I-beam while no
// explicit cursor is set).
func view(_ *parity.EventLog) gpui.AnyElement {
	return paragraph(false)
}

// NoneView is the opt-out tree: the paragraph's div carries
// user-select: none (Tailwind select-none) — the subtree selects
// nothing. Exported for the wrong-view test, which mounts it under the
// default fixture's record.
func NoneView() gpui.AnyElement {
	return paragraph(true)
}

// noneView is NoneView's signature as a fixture view.
func noneView(_ *parity.EventLog) gpui.AnyElement {
	return paragraph(true)
}

func paragraph(none bool) gpui.AnyElement {
	text := gpui.Div().
		ID("text").
		DebugSelector("text").
		W(gpui.Px(paragraphWidth))
	if none {
		text = text.SelectNone()
	}
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsCenter).
		Justify(gpui.AlignContentCenter).
		Bg(gpui.Rgb(pageBackground)).
		TextSize(gpui.Px(textSize)).
		LineHeight(gpui.Px(textLineHeight)).
		TextColor(gpui.Rgb(textColor)).
		Child(text.Child(dragText)).
		IntoElement()
}

// capture is the Script step's Go half: after the input steps, read the
// model — the window's selected text (the on-demand projection) and the
// resolved cursor (the automatic I-beam over the selectable text, the
// arrow over the none subtree) — into the capture the fixture's test
// compares with the record's ComputedScript values.
func capture(_ *parity.EventLog, w *gpui.Window, app *gpui.App) {
	text := gpui.TextSelectionSelectedText(w, app)
	cursor, _ := gpui.WindowCursorStyle(w)
	last = &Capture{Text: text, Cursor: cursorStyleName(cursor)}
}

// cursorStyleName renders the resolved cursor for the capture.
func cursorStyleName(style gpui.CursorStyle) string {
	switch style {
	case gpui.CursorIBeam:
		return "ibeam"
	case gpui.CursorArrow:
		return "arrow"
	case gpui.CursorPointingHand:
		return "hand"
	}
	return "other"
}

// last is the most recent replay's capture (the replays run
// sequentially, so the fixture's test reads the capture its own Replay's
// Script step just wrote).
var last *Capture

// LastCapture returns the most recent replay's capture.
func LastCapture() *Capture { return last }

// Capture is one replay's selection read.
type Capture struct {
	// Text is the window's selected text.
	Text string
	// Cursor is the cursor the replay resolved at the final position
	// (the automatic I-beam over selectable text, the arrow over a none
	// subtree).
	Cursor string
}

// configureWashOf builds the drag fixture's app configuration: pin the
// selection wash the record actually painted, sampled from the recorded
// screenshot (the selection fixture's sampler — Chrome's ::selection
// default is not readable from the page, and headless Chrome's wash is
// the embedder default, so the record's own pixels are the only
// deterministic source).
func configureWashOf(name string) func(*gpui.App) error {
	return func(app *gpui.App) error {
		err := configureWash(app, name)
		if err != nil {
			return err
		}
		return nil
	}
}

func configureWash(app *gpui.App, name string) error {
	record, err := parity.LoadRecord(name)
	if err != nil {
		return err
	}
	color, err := recordedWashOf(record)
	if err != nil {
		return err
	}
	app.SetSelectionColors(&gpui.SelectionColors{Background: color})
	return nil
}

// recordedWashOf samples the recorded screenshot's wash colour: the most
// frequent pixel colour that is neither the page's white background nor
// the sample text's red ink (the selection fixture's sampler, copied —
// its packages are self-contained by convention).
func recordedWashOf(record *parity.Record) (gpui.Hsla, error) {
	image, err := record.Image()
	if err != nil {
		return gpui.Hsla{}, err
	}
	const minimum = 100
	counts := map[[3]uint8]int{}
	bounds := image.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := image.At(x, y).RGBA()
			cr, cg, cb := uint8(r>>8), uint8(g>>8), uint8(b>>8)
			if cr > 240 && cg > 240 && cb > 240 {
				continue
			}
			if cr > 180 && cg < 130 && cb < 130 {
				continue
			}
			counts[[3]uint8{cr, cg, cb}]++
		}
	}
	best, count := [3]uint8{}, 0
	for color, n := range counts {
		if n > count {
			best, count = color, n
		}
	}
	if count < minimum {
		return gpui.Hsla{}, fmt.Errorf("no wash pixels in the record's screenshot (best %d of %d)", count, len(counts))
	}
	// Reconstruct the translucent wash Chrome painted: the sampled pixel
	// is the wash composited over the white page, and the wash is the
	// BlendWithWhite ladder's last step (alpha 204), so the source colour
	// solves back out of the composite.
	const alpha = float32(204) / 255
	var source [3]uint8
	for i, sampled := range best {
		v := (float32(sampled) - (1-alpha)*255) / alpha
		if v < 0 {
			v = 0
		}
		if v > 255 {
			v = 255
		}
		source[i] = uint8(v + 0.5)
	}
	rgba := uint32(source[0])<<24 | uint32(source[1])<<16 | uint32(source[2])<<8 | 204
	return gpui.RgbaToHsla(rgba), nil
}
