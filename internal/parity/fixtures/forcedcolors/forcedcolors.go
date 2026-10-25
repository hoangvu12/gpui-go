//go:build windows

// Package forcedcolors is ticket 29's fixture for the forced-colours
// pass: the page is recorded in headless Chrome with the
// forced-colors media feature emulated active (CDP
// Emulation.setEmulatedMedia), so Chrome's forced-colours mode maps the
// page's authored colours onto the system palette exactly as the
// gpui-go pass does (StyleAdjuster::AdjustForForcedColorsMode,
// third_party/blink/renderer/core/css/resolver/style_adjuster.cc:1034-
// 1058 @ 0d4dcd4f): the coloured text becomes CanvasText, the
// half-transparent blue background becomes Canvas with its alpha kept,
// the green border becomes CanvasText, the box shadow disappears, and
// the forced-color-adjust: none box keeps everything.
//
// Chrome's emulated forced-colours palette is NOT this PC's Windows
// palette, so the record carries the palette the browser used: the
// page's hidden [data-color-probe] elements each author one CSS
// system-colour keyword (a system colour passes through forced colors
// unforced — ShouldForceColor's IsSystemColorIncludingDeprecated check,
// core/style/computed_style.h:2917-2922), the fixture's ComputedScript
// reads their computed colours into Record.Computed, and the replay's
// Configure installs exactly those values through
// gpui.App.SetForcedColors — the test is deterministic and independent
// of the user's theme.
//
// The fixture is a pure painting fixture (no instrumented events, like
// boxpaint): every behaviour is a pixel.
package forcedcolors

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The numbers shared by the page and its gpui view (the tracer's
// discipline: both sides are built from one set of constants).
const (
	windowWidth  = 640.0
	windowHeight = 400.0

	boxWidth  = 240.0
	boxHeight = 64.0
	boxGap    = 20.0

	textSize       = 16.0
	textLineHeight = 25.0

	alphaBorder = 3.0
	keepBorder  = 2.0

	shadowOffsetY = 6.0
	shadowBlur    = 12.0
)

// The authored palette, shared by the page and the views; the pass maps
// every one of these onto the system palette except inside the opt-out
// box.
const (
	pageBackground = 0xffffff
	textRed        = 0xdc2626
	alphaBlue      = 0x2563eb
	alphaBorderGre = 0x16a34a
	shadowBlue     = 0x2563eb
	keepPurple     = 0x9333ea
	keepText       = 0xfde047
	keepBorderOrng = 0xf97316
	shadowInk      = 0xffffff
)

// alphaAlpha is the alpha background's authored alpha: the forced
// background keeps it on the Canvas colour (Chromium copies the
// unforced alpha onto the forced colour, longhands_custom.cc:6140-6156).
const alphaAlpha = 0.5

// shadowAlpha is the authored shadow colour's alpha. The shadow ink is
// white: the emulated palette's Canvas is black, so a black shadow
// would be invisible against it and would not discriminate the
// pass-off replay (which paints the shadow the record dropped).
const shadowAlpha = 0.6

// textLineText is the coloured text line's text.
const textLineText = "The quick brown fox"

// keepText is the opt-out box's text.
const keepBoxText = "Kept"

// Fixture is the forced-colours fixture: recorded under emulated
// forced-colors active, replayed with the pass on and the recorded
// palette injected.
var Fixture = parity.Fixture{
	Name:   "forcedcolors",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page,
	View:   View,
	// A pure painting fixture: no instrumented events, no steps.
	BoxIDs: []string{"root", "text-line", "alpha", "shadow", "keep"},
	EmulatedMedia: []parity.EmulatedMediaFeature{
		{Name: "forced-colors", Value: "active"},
	},
	ComputedScript: computedScript,
	Configure:      Configure,
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics: whole mean 0.1416 / hot
		// 0.00172, and per box text-line 1.6748 / 0.02295 (white
		// CanvasText ink — the port's ClearType fringes against Chrome's
		// antialiasing, the textpaint fixtures' known text noise), keep
		// 0.4107 / 0.00575 (the kept text, border and shadow), alpha and
		// shadow 0.0000 (the mapped Canvas fills and the dropped shadow
		// are pixel-exact). The gates sit at 2x those. The deliberately-
		// wrong replays (the pass-off view and the swapped-palette view,
		// forcedcolors_test.go) fail by an order of magnitude. See
		// docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.3,
		MaxROIMeanChannelDiff: 3.6,
		MaxHotPixelFraction:   0.05,
	},
}

func init() {
	parity.Register(Fixture)
}

// ---------------------------------------------------------------------------
// The page
// ---------------------------------------------------------------------------

// page is the HTML twin of View. The probes at the end author one CSS
// system-colour keyword each; ComputedScript reads their computed
// colours, which is how the record captures Chrome's emulated palette.
const page = `<!doctype html>
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
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 20px;
		color: #dc2626;
		font: 16px/25px "Segoe UI", system-ui, sans-serif;
	}
	.box {
		box-sizing: border-box;
		width: 240px;
		height: 64px;
		display: flex;
		align-items: center;
		justify-content: center;
	}
	/* The coloured text line: transparent background, red text. Under
	   forced colors the text becomes CanvasText and the (transparent)
	   background stays unpainted. */
	.text-line {
		color: #dc2626;
	}
	/* The alpha background: half-transparent blue with a green border.
	   Under forced colors the background becomes Canvas with the 0.5
	   alpha kept and the border becomes CanvasText. */
	.alpha {
		background: rgba(37, 99, 235, 0.5);
		border: 3px solid #16a34a;
	}
	/* The shadow: a blue box with a soft shadow. Under forced colors
	   the background becomes Canvas and the shadow disappears. */
	.shadow {
		background: #2563eb;
		box-shadow: 0 6px 12px rgba(255, 255, 255, 0.6);
	}
	/* The opt-out: forced-color-adjust: none keeps every authored
	   colour, the background, the border, the text colour and the
	   shadow. */
	.keep {
		forced-color-adjust: none;
		background: #9333ea;
		color: #fde047;
		border: 2px solid #f97316;
		box-shadow: 0 6px 12px rgba(255, 255, 255, 0.6);
	}
	.probe {
		display: none;
	}
</style>
</head>
<body data-id="root">
	<div class="box text-line" data-id="text-line">The quick brown fox</div>
	<div class="box alpha" data-id="alpha"></div>
	<div class="box shadow" data-id="shadow"></div>
	<div class="box keep" data-id="keep">Kept</div>
	<i class="probe" data-color-probe="canvas" style="color: Canvas"></i>
	<i class="probe" data-color-probe="canvastext" style="color: CanvasText"></i>
	<i class="probe" data-color-probe="linktext" style="color: LinkText"></i>
	<i class="probe" data-color-probe="visitedtext" style="color: VisitedText"></i>
	<i class="probe" data-color-probe="activetext" style="color: ActiveText"></i>
	<i class="probe" data-color-probe="buttonface" style="color: ButtonFace"></i>
	<i class="probe" data-color-probe="buttontext" style="color: ButtonText"></i>
	<i class="probe" data-color-probe="buttonborder" style="color: ButtonBorder"></i>
	<i class="probe" data-color-probe="field" style="color: Field"></i>
	<i class="probe" data-color-probe="fieldtext" style="color: FieldText"></i>
	<i class="probe" data-color-probe="highlight" style="color: Highlight"></i>
	<i class="probe" data-color-probe="highlighttext" style="color: HighlightText"></i>
	<i class="probe" data-color-probe="selecteditem" style="color: SelectedItem"></i>
	<i class="probe" data-color-probe="selecteditemtext" style="color: SelectedItemText"></i>
	<i class="probe" data-color-probe="mark" style="color: Mark"></i>
	<i class="probe" data-color-probe="marktext" style="color: MarkText"></i>
	<i class="probe" data-color-probe="graytext" style="color: GrayText"></i>
	<i class="probe" data-color-probe="accentcolor" style="color: AccentColor"></i>
	<i class="probe" data-color-probe="accentcolortext" style="color: AccentColorText"></i>
</body>
</html>
`

// computedScript reads the probe elements' computed colours into a JSON
// object: each entry is the probe's name mapped to its colour in CSS
// hex (#rrggbb or #rrggbbaa). The colours are page-computed values of
// system-colour keywords, so under emulated forced-colors active they
// are exactly the palette the browser rendered the page with.
const computedScript = `JSON.stringify(Object.fromEntries(Array.from(document.querySelectorAll('[data-color-probe]')).map(el => {
	const c = getComputedStyle(el).color;
	const m = c.match(/^rgba?\((\d+), (\d+), (\d+)(?:, ([\d.]+))?\)$/);
	if (!m) throw new Error('probe color is not rgb(): ' + c);
	const h = n => Number(n).toString(16).padStart(2, '0');
	let out = '#' + h(m[1]) + h(m[2]) + h(m[3]);
	if (m[4] !== undefined) out += h(Math.round(parseFloat(m[4]) * 255));
	return [el.dataset.colorProbe, out];
})))`

// ---------------------------------------------------------------------------
// The replay side: the view and the app configuration
// ---------------------------------------------------------------------------

// View is the gpui-go twin of page: the same authored colours, which
// the pass (installed by Configure) maps onto the recorded palette.
func View(_ *parity.EventLog) gpui.AnyElement {
	return root()
}

// root builds the fixture's root: the white flex column with the four
// boxes, centered.
func root() gpui.AnyElement {
	return gpui.Div().Flex().
		ID("root").
		DebugSelector("root").
		SizeFull().
		FlexCol().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Gap(gpui.Px(boxGap)).
		Bg(gpui.Rgb(pageBackground)).
		TextColor(gpui.Rgb(textRed)).
		TextSize(gpui.Px(textSize)).
		LineHeight(gpui.Px(textLineHeight)).
		Child(box("text-line").Child(textLineText)).
		Child(alphaBox()).
		Child(shadowBox()).
		Child(keepBox()).
		IntoElement()
}

// box builds the fixture's 240x64 centered flex box.
func box(id string) *gpui.DivElement {
	return gpui.Div().
		ID(id).
		DebugSelector(id).
		W(gpui.Px(boxWidth)).
		H(gpui.Px(boxHeight)).
		Flex().
		AlignItems(gpui.AlignItemsCenter).
		Justify(gpui.AlignContentCenter)
}

// alphaBox builds the alpha fixture box: a half-transparent blue
// background with a green border.
func alphaBox() *gpui.DivElement {
	return box("alpha").
		Bg(gpui.Rgb(alphaBlue).Alpha(alphaAlpha)).
		Border(gpui.Px(alphaBorder)).
		BorderColor(gpui.Rgb(alphaBorderGre))
}

// shadowBox builds the shadow fixture box: a blue background with a
// soft shadow that disappears under forced colours.
func shadowBox() *gpui.DivElement {
	return box("shadow").
		Bg(gpui.Rgb(shadowBlue)).
		Shadow(boxShadow())
}

// keepBox builds the opt-out box: forced-color-adjust: none keeps the
// purple background, the yellow text, the orange border and the
// shadow.
func keepBox() *gpui.DivElement {
	return box("keep").
		ForcedColorAdjustNone().
		Bg(gpui.Rgb(keepPurple)).
		TextColor(gpui.Rgb(keepText)).
		Border(gpui.Px(keepBorder)).
		BorderColor(gpui.Rgb(keepBorderOrng)).
		Shadow(boxShadow()).
		Child(keepBoxText)
}

// boxShadow is the shared 0 6px 12px rgba(255,255,255,0.6) shadow
// (white ink: the emulated palette's Canvas is black, so the shadow
// discriminates).
func boxShadow() []gpui.BoxShadow {
	return []gpui.BoxShadow{
		gpui.NewBoxShadow(0, shadowOffsetY, gpui.SolidBackground(gpui.Rgb(shadowInk).Alpha(shadowAlpha))).
			WithBlurRadius(shadowBlur),
	}
}

// ---------------------------------------------------------------------------
// The recorded palette
// ---------------------------------------------------------------------------

// Configure pins the pass on with the palette the record was rendered
// with: the probe colours Chrome resolved under emulated forced-colors
// active, read out of Record.Computed. The replay is deterministic —
// it does not depend on this machine's Windows theme.
func Configure(app *gpui.App) error {
	record, err := parity.LoadRecord("forcedcolors")
	if err != nil {
		return err
	}
	colors, err := SystemColorsOfRecord(record)
	if err != nil {
		return err
	}
	app.SetForcedColors(&gpui.ForcedColorsState{Active: true, Colors: colors})
	return nil
}

// ConfigureWithPalette is Configure with an explicit palette: the
// discrimination tests pin a wrong palette through it to prove the
// mapping really reads the palette (a hardcoded white-on-black pass
// fails against an inverted palette).
func ConfigureWithPalette(colors gpui.SystemColors) func(*gpui.App) error {
	return func(app *gpui.App) error {
		app.SetForcedColors(&gpui.ForcedColorsState{Active: true, Colors: colors})
		return nil
	}
}

// SystemColorsOfRecord builds the system colour record from the
// recorded probe colours (CSS hex per system colour). Exported for the
// fixture's own tests (reading the record's palette back).
func SystemColorsOfRecord(record *parity.Record) (gpui.SystemColors, error) {
	var colors gpui.SystemColors
	fields := []struct {
		key   string
		field *gpui.Hsla
	}{
		{"canvas", &colors.Canvas},
		{"canvastext", &colors.CanvasText},
		{"linktext", &colors.LinkText},
		{"visitedtext", &colors.VisitedText},
		{"activetext", &colors.ActiveText},
		{"buttonface", &colors.ButtonFace},
		{"buttontext", &colors.ButtonText},
		{"buttonborder", &colors.ButtonBorder},
		{"field", &colors.Field},
		{"fieldtext", &colors.FieldText},
		{"highlight", &colors.Highlight},
		{"highlighttext", &colors.HighlightText},
		{"selecteditem", &colors.SelectedItem},
		{"selecteditemtext", &colors.SelectedItemText},
		{"mark", &colors.Mark},
		{"marktext", &colors.MarkText},
		{"graytext", &colors.GrayText},
		{"accentcolor", &colors.AccentColor},
		{"accentcolortext", &colors.AccentColorText},
	}
	for _, field := range fields {
		hex, ok := record.Computed[field.key]
		if !ok {
			return colors, fmt.Errorf("the record carries no %q probe colour (re-record the fixture: go run ./cmd/parityrecord -fixture %s)", field.key, record.Name)
		}
		color, err := gpui.ParseHex(hex)
		if err != nil {
			return colors, fmt.Errorf("the record's %q probe colour %q: %w", field.key, hex, err)
		}
		*field.field = color
	}
	return colors, nil
}
