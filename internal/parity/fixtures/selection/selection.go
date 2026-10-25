//go:build windows

// Package selection is browser-core ticket 19's fixture set: static text
// selection with the mouse — press-drag across words, double-click a word
// with punctuation next to it, triple-click a line, and double-click then
// drag (the granularity extension) — recorded in headless Chrome and
// replayed against the core selection model (gpui/text_selection.go) over
// the core's static selectable text (gpui/selectable_text.go).
//
// The comparison is the harness's three tiers plus the ticket's own
// selection-range check (Tier B-like): the page's
// window.getSelection() — anchor/focus offsets within the text node and
// the selected text — is recorded through the fixture's ComputedScript,
// and the replay's model projection is read back through the
// participant's ProjectedRanges (the capture Script step's Go half runs
// after the input steps). Both sides' offsets are byte offsets of the
// same ASCII sample text (the DOM's UTF-16 offsets agree with UTF-8 on
// ASCII; non-ASCII samples stay in the model's unit tests for that
// reason), and the port's are SOURCE offsets (browser-core 38's caret
// byte map — the run's layout answers in source offsets, and the run's
// text is the source text).
//
// The Chromium rules these fixtures record (the citations live in
// gpui/text_selection.go and gpui/text_boundary.go):
//
//   - The double-click word: SelectClosestWordFromHitTestResult's
//     ExpandWithGranularity(kWord) — the ICU word-break segment — with
//     Windows' trailing-whitespace extension
//     (selection_controller.cc:187-209, 842-849).
//   - The triple-click paragraph: HandleTripleClick's
//     ExpandWithGranularity(kParagraph) including the paragraph break
//     (selection_controller.cc:1110-1155, selection_adjuster.cc:318-334).
//   - The drag after a multi-click extends by the same granularity:
//     HandleMouseDragged's ExtendSelectionAsDirectional
//     (selection_controller.cc:316-375, 693-699).
package selection

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The page's numbers, shared by the HTML and the gpui view (the tracer
// pattern): body 640x400 flex-centering a 380-wide pre-wrap paragraph of
// 16px/25px Segoe UI, the port's TextSelectionLayer under the root.
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	paragraphWidth = 380.0

	textSize       = 16.0
	textLineHeight = 25.0

	pageBackground = 0xffffff
	textColor      = 0xdc2626
)

// The sample texts (ASCII only: the DOM's selection offsets are UTF-16
// units, which agree with the port's UTF-8 source offsets on ASCII).
const (
	// dragText is the drag and word-drag fixtures' body: four words wide
	// enough that the press and the release land mid-word.
	dragText = "alpha beta gamma delta"
	// wordText is the double-click fixture's body: a word with a comma
	// directly after it (the punctuation-next-to-the-word case).
	wordText = "one latte, three"
	// lineText is the triple-click fixture's body: three pre-wrap lines
	// (hard \n breaks — the paragraph separators of the plain run).
	lineText = "first line\nsecond line\nthird"
)

// The pointer positions, in window coordinates (the paragraph is
// flex-centered: x = (640-380)/2 + the glyph position, y = the line's
// middle). The glyph positions come from the real text system's layout of
// the same text at the same style: "beta" spans x≈58-96 and "gamma"
// x≈106-154 of the drag text, "latte" x≈38-91 of the word text, and the
// second line's "second" starts at x≈105 of the line text.
const (
	paragraphX = (windowWidth - paragraphWidth) / 2

	// The drag fixture: the press mid-"beta", the release mid-"gamma".
	dragPressX   = paragraphX + 70
	dragReleaseX = paragraphX + 130
	// The word-drag fixture: the double-click on "beta", the drag to
	// "gamma" while held. The release x is inside "gamma" in BOTH sides'
	// layouts (the port's Segoe UI advances differ from Chrome's — the
	// drag fixture's record pins Chrome's carets around caret 9/16 at
	// x 196/261.5 and the port's at 216/283; 248 lands mid-word in both),
	// so the word-granularity extension takes the same segment on both
	// sides.
	wordDragClickX = paragraphX + 70
	wordDragEndX   = paragraphX + 118
	// The word fixture: the double-click mid-"latte".
	wordClickX = paragraphX + 55
	// The line fixture: the triple-click on the middle line.
	lineClickX = paragraphX + 50
)

// lineY returns the window y of a 25px line's middle, for a
// lines-line paragraph flex-centered in the 400px window.
func lineY(lines, line int) float32 {
	height := float32(lines) * textLineHeight
	return (windowHeight-height)/2 + float32(line)*textLineHeight + textLineHeight/2
}

// selectionFixture is one fixture plus its replay-side capture state: the
// participant handle (created once per window, the focus-handle pattern —
// the view function re-runs every frame, and Replay runs the same fixture
// twice with fresh windows) and the capture the final Script step reads
// into. The fixture value is copied wholesale when a test swaps the view,
// and the copies share this state through the map. The map keys on the
// WINDOW POINTER, not the id: window ids are per-application counters, so
// they repeat across the replays of one process.
type selectionFixture struct {
	fixture parity.Fixture
	text    string
	handles map[*gpui.Window]gpui.TextSelectionHandle
}

// newSelectionFixture builds one fixture of text with the input steps:
// the capture Script step is appended, and the view and capture closures
// share the handle map.
func newSelectionFixture(name, text string, steps []parity.Step, tolerance parity.PixelTolerance) parity.Fixture {
	s := &selectionFixture{handles: map[*gpui.Window]gpui.TextSelectionHandle{}, text: text}
	steps = append(append([]parity.Step(nil), steps...), parity.Script(readSelectionScript, s.capture))
	return parity.Fixture{
		Name:           name,
		Window:         parity.Size{Width: windowWidth, Height: windowHeight},
		HTML:           page(text),
		Steps:          steps,
		BoxIDs:         []string{"root", "text"},
		ComputedScript: computedSelectionScript,
		ViewWithWindow: s.view,
		Configure:      configureWashOf(name),
		Tolerance:      tolerance,
	}
}

// Fixture is the press-drag fixture: a drag from mid-"beta" to
// mid-"gamma" selects the characters between the two carets.
var Fixture = newSelectionFixture("selection-drag", dragText, []parity.Step{
	parity.Move(dragPressX, lineY(1, 0)),
	parity.Down(dragPressX, lineY(1, 0), parity.ButtonLeft),
	parity.Move(dragReleaseX, lineY(1, 0)),
	parity.Up(dragReleaseX, lineY(1, 0), parity.ButtonLeft),
}, parity.PixelTolerance{
	// Provisional (research 19's calibration has not run), set at 2x the
	// measured correct-view statistics: whole 0.1219/0.00182, text 3.1159/0.04558. The floor is ticket 28's
	// recorded divergence — the port keeps the selected text's own colour
	// over the translucent wash where Chrome paints HighlightText — plus
	// the textpaint fixtures' ink noise.
	MaxMeanChannelDiff:    0.25,
	MaxROIMeanChannelDiff: 6.5,
	MaxHotPixelFraction:   0.095,
})

// WordFixture is the double-click fixture: a double-click on "latte"
// with a comma directly after it selects the word (the comma is its own
// segment; the word has no trailing whitespace to extend over).
var WordFixture = newSelectionFixture("selection-word", wordText, []parity.Step{
	parity.Click(wordClickX, lineY(1, 0)),
	parity.MultiClick(wordClickX, lineY(1, 0), 2),
}, parity.PixelTolerance{
	// Provisional (research 19's calibration has not run), set at 2x the
	// measured correct-view statistics: whole 0.0760/0.00115, text 1.9684/0.02929. The floor is ticket 28's
	// recorded divergence — the port keeps the selected text's own colour
	// over the translucent wash where Chrome paints HighlightText — plus
	// the textpaint fixtures' ink noise.
	MaxMeanChannelDiff:    0.16,
	MaxROIMeanChannelDiff: 4.0,
	MaxHotPixelFraction:   0.06,
})

// LineFixture is the triple-click fixture: a triple-click on the middle
// line of a three-line pre-wrap paragraph selects the line plus its
// paragraph break (Chromium's kParagraph expansion).
var LineFixture = newSelectionFixture("selection-line", lineText, []parity.Step{
	parity.Click(lineClickX, lineY(3, 1)),
	parity.MultiClick(lineClickX, lineY(3, 1), 2),
	parity.MultiClick(lineClickX, lineY(3, 1), 3),
}, parity.PixelTolerance{
	// Provisional (research 19's calibration has not run), set at 2x the
	// measured correct-view statistics: whole 0.1382/0.00222, text 1.2410/0.01992. The floor is ticket 28's
	// recorded divergence — the port keeps the selected text's own colour
	// over the translucent wash where Chrome paints HighlightText — plus
	// the textpaint fixtures' ink noise.
	MaxMeanChannelDiff:    0.28,
	MaxROIMeanChannelDiff: 2.5,
	MaxHotPixelFraction:   0.04,
})

// WordDragFixture is the double-click-then-drag fixture: a double-click
// on "beta" followed by a drag to mid-"gamma" while the button is held —
// the drag extends the selection by the word granularity (whole words,
// the moving endpoint snapping to the word boundary).
var WordDragFixture = newSelectionFixture("selection-word-drag", dragText, []parity.Step{
	parity.Click(wordDragClickX, lineY(1, 0)),
	// The press is still held for the drag: the double-click's second
	// press is spelled out (MultiClick expands to move+down+up, so the
	// drag needs the down held open across the moves).
	parity.DownWithClicks(wordDragClickX, lineY(1, 0), parity.ButtonLeft, 2),
	parity.Move(wordDragEndX, lineY(1, 0)),
	parity.UpWithClicks(wordDragEndX, lineY(1, 0), parity.ButtonLeft, 2),
}, parity.PixelTolerance{
	// Provisional (research 19's calibration has not run), set at 2x the
	// measured correct-view statistics: whole 0.1403/0.00216, text 3.5538/0.05342. The floor is ticket 28's
	// recorded divergence — the port keeps the selected text's own colour
	// over the translucent wash where Chrome paints HighlightText — plus
	// the textpaint fixtures' ink noise.
	MaxMeanChannelDiff:    0.29,
	MaxROIMeanChannelDiff: 7.2,
	MaxHotPixelFraction:   0.11,
})

func init() {
	parity.Register(Fixture)
	parity.Register(WordFixture)
	parity.Register(LineFixture)
	parity.Register(WordDragFixture)
}

// ---------------------------------------------------------------------------
// The page
// ---------------------------------------------------------------------------

// page is the HTML twin of the view: a centered 380px pre-wrap paragraph
// of 16px/25px Segoe UI. The text is selectable by default
// (user-select: auto); nothing pins it off.
func page(text string) string {
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
		width: 640px;
		height: 400px;
		display: flex;
		align-items: center;
		justify-content: center;
	}
	p {
		box-sizing: border-box;
		width: 380px;
		margin: 0;
		white-space: pre-wrap;
		color: #dc2626;
		font: 16px/25px "Segoe UI", system-ui, sans-serif;
	}
	.probe {
		display: none;
	}
</style>
</head>
<body data-id="root">
	<p data-id="text">` + text + `</p>
	<i class="probe" data-color-probe="selection" style="background: Highlight"></i>
</body>
</html>
`
}

// computedSelectionScript reads the page's final selection into the
// record: the anchor and focus offsets within the text node (UTF-16
// units — byte offsets for the ASCII samples), the selected text,
// whether both endpoints sit in the paragraph's text node, and the
// ::selection wash colour the browser resolved — the computed Highlight
// system colour (the same source Blink's ::selection UA default reads,
// layout_theme.cc:725-728) — so the replay paints exactly the wash the
// record was rendered with, independent of the machine's theme (the
// forced-colours fixture's probe pattern).
const computedSelectionScript = `JSON.stringify((() => {
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
	return {
		anchor: String(s.anchorOffset),
		focus: String(s.focusOffset),
		text: String(s),
		anchorNode: nodeOf(s.anchorNode),
		focusNode: nodeOf(s.focusNode),
		wash: wash,
	};
})())`

// readSelectionScript is the capture step's page half: a harmless read
// (the record's own selection values come from the ComputedScript).
const readSelectionScript = `window.__selectionRead = String(window.getSelection())`

// ---------------------------------------------------------------------------
// The replay side
// ---------------------------------------------------------------------------

// view builds the fixture's tree: the root with the TextSelectionLayer
// (the selection model's gestures) over the 380px text block, the core's
// SelectableText inside it carrying this window's participant handle.
func (s *selectionFixture) view(_ *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	handle, ok := s.handles[w]
	if !ok {
		handle = gpui.NewTextSelectionHandle(s.text, app, w.Scope())
		s.handles[w] = handle
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
		Child(gpui.TextSelectionLayer{}).
		// The pin's default display (Flex) stays: the selection
		// gesture's text-collection path was built and recorded against
		// the measured-leaf text child (a flex item), not the paragraph
		// span; the page twin's <p> block text is a follow-up (selection
		// over inline fragments, ticket 19's corpus).
		Child(gpui.Div().Flex().
			ID("text").
			DebugSelector("text").
			W(gpui.Px(paragraphWidth)).
			Child(gpui.SelectableTextWithHandle("text", handle, s.text))).
		IntoElement()
}

// capture is the appended Script step's Go half: after the input steps,
// read the model's projection — the participant's selected source ranges
// and the window's selected text — into the per-window capture the
// fixture's test compares with the record's ComputedScript values.
func (s *selectionFixture) capture(_ *parity.EventLog, w *gpui.Window, app *gpui.App) {
	handle, ok := s.handles[w]
	if !ok {
		// A swapped view (the wrong-view test) renders no run of this
		// fixture's handle: nothing is captured, and the previous test's
		// capture must not survive.
		last = nil
		return
	}
	last = &SelectionCapture{Ranges: handle.ProjectedRanges(app), Text: handle.ProjectedText(app)}
}

// configureWashOf builds the fixture's app configuration: pin the
// selection wash the record actually painted, sampled from the recorded
// screenshot (the pure wash pixels over the white page — the dominant
// colour that is neither the page's white nor the text's red). Chrome's
// ::selection default is not readable from the page (getComputedStyle's
// ::selection pseudo answers transparent), and headless Chrome's wash is
// the embedder default's blend (kDefaultActiveSelectionBgColor
// 0xFF1967D2, renderer_preferences.h:26) rather than the OS palette the
// computed Highlight system colour resolves — so the record's own pixels
// are the only deterministic source. The replay then paints exactly the
// wash the record was rendered with, and the comparison measures the
// selection's geometry, not the theme.
func configureWashOf(name string) func(*gpui.App) error {
	return func(app *gpui.App) error {
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
}

// recordedWashOf samples the recorded screenshot's wash colour: the most
// frequent pixel colour that is neither the page's white background nor
// the sample text's red ink (the wash over white dominates every other
// wash blend in the area).
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
			// The page's white and the text's red ink (plus their
			// anti-aliased neighbours) are skipped: everything else in
			// these pages is the wash.
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
	// BlendWithWhite ladder's last step (alpha 204, color.cc:69-71 — the
	// ladder's end for the saturated selection blues; the port's own
	// blendWithWhite uses the same ladder), so the source colour solves
	// back out of the composite. (Whether Chrome's wash is this
	// translucent colour or the same composite painted opaque is not
	// distinguishable over these pages' white background — both paint the
	// sampled pixels.)
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

// last is the most recent replay's capture (the replays run sequentially,
// so the fixture's test reads the capture its own Replay's Script step
// just wrote).
var last *SelectionCapture

// LastCapture returns the most recent replay's capture (the test reads it
// right after its Replay; the replays run sequentially).
func LastCapture() *SelectionCapture { return last }

// SelectionCapture is one replay's selection read: the participant's
// projected source ranges paired with its runs, and the window's selected
// text.
type SelectionCapture struct {
	// Ranges is the projection of the participant's single run.
	Ranges []gpui.Optional[gpui.TextRange]
	// Text is the window's selected text in document order.
	Text string
}

// Range is the capture's single-run selected range (the fixtures' views
// register exactly one run); absent when nothing is selected.
func (c *SelectionCapture) Range() (gpui.TextRange, bool) {
	if c == nil || len(c.Ranges) == 0 {
		return gpui.TextRange{}, false
	}
	r, ok := c.Ranges[0].Get()
	return r, ok
}
