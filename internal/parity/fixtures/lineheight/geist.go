//go:build windows

// The Geist half of ticket 37's line-height fixtures: the same CSS
// `normal` rule under a second, embedded face whose metrics differ from
// the system font's (Geist Regular: 1000 units per em, ascent 1005,
// descent 295, line gap 0 — round(16.08) + round(4.72) + 0 = 21 at
// 16px, but 24px rounds differently than Segoe UI: 24 + 7 = 31 vs
// Segoe's 26 + 6 = 32). Both sides shape the identical embedded bytes:
// the page loads the face through an @font-face data URL (headless
// Chrome cannot see a process-registered font) and the replay's
// Configure hook adds the same bytes through TextSystem.AddFont before
// the window lays text out.
package lineheight

import (
	"encoding/base64"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The Geist fixture's window: four rows (16, 24, 48 and the mixed
// 16+24 span) plus the gaps and padding.
const geistWindowHeight = 300.0

// GeistFixture is the Geist twin of Fixture: the default line height
// under three font sizes and the mixed-size inline span, on a face with
// its own metric rounding.
var GeistFixture = parity.Fixture{
	Name:   "lh-normal-geist",
	Window: parity.Size{Width: windowWidth, Height: geistWindowHeight},
	HTML:   page(geistPageFont, geistRows),
	View:   GeistView,
	BoxIDs: []string{
		"root",
		"row-g16", "span-g16",
		"row-g24", "span-g24",
		"row-g48", "span-g48",
		"row-gmix", "span-gmix",
	},
	Tolerance: parity.PixelTolerance{
		// Provisional (research 19's calibration has not run), set from
		// the measured correct-view statistics (whole 0.109 / worst ROI
		// span-g48 0.669 — the large-glyph antialiasing drift) at ~2x
		// each. Tier A is exact — the row boxes carry the rule on the
		// embedded face. See docs/browser-parity-harness.md.
		MaxMeanChannelDiff:    0.25,
		MaxROIMeanChannelDiff: 1.4,
		MaxHotPixelFraction:   0.03,
	},
	// Configure adds the embedded Geist face to the process's font
	// catalog before the window lays text out, so the view's "Geist"
	// family resolves to the same bytes the page's @font-face loads.
	Configure: func(app *gpui.App) error {
		ts, err := gpui.DefaultTextSystem()
		if err != nil {
			return err
		}
		data, err := base64.StdEncoding.DecodeString(geistTTFBase64)
		if err != nil {
			return err
		}
		if _, err := ts.AddFont(data); err != nil {
			return err
		}
		return nil
	},
}

func init() { parity.Register(GeistFixture) }

// geistPageFont is the Geist page's font CSS: the @font-face data URL
// (the same embedded bytes the replay registers) with NO line-height
// pin — the CSS initial normal, the rule under test.
const geistPageFont = `
	@font-face {
		font-family: "Geist";
		src: url(data:font/ttf;base64,` + geistTTFBase64 + `) format("truetype");
	}
	html, body {
		margin: 0;
		padding: 0;
		background: #ffffff;
	}
	body {
		width: 640px;
		height: 300px;
		display: flex;
		flex-direction: column;
		gap: 12px;
		padding: 16px;
		box-sizing: border-box;
		/* No line-height pin: the initial normal (browser-parity D1)
		   is the rule under test, on the embedded face. */
		font: 16px "Geist", "Segoe UI", system-ui, sans-serif;
		color: #0f172a;
	}
`

// geistRows is the Geist fixture's row bodies.
const geistRows = `
	<div class="row" data-id="row-g16"><span class="fill" data-id="span-g16" style="font-family: 'Geist'; font-size: 16px">Geist sixteen point</span></div>
	<div class="row" data-id="row-g24"><span class="fill" data-id="span-g24" style="font-family: 'Geist'; font-size: 24px">Geist twenty-four</span></div>
	<div class="row" data-id="row-g48"><span class="fill" data-id="span-g48" style="font-family: 'Geist'; font-size: 48px">Geist XL</span></div>
	<div class="row" data-id="row-gmix"><span class="fill" data-id="span-gmix" style="font-family: 'Geist'; font-size: 16px">one <span style="font-size: 24px">BIG</span> tail</span></div>
`

// GeistView is the gpui-go twin of the Geist page: the shared root
// with the DEFAULT (CSS normal) line height and the Geist family.
func GeistView(_ *parity.EventLog) gpui.AnyElement {
	root := gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		FlexCol().
		Gap(gpui.Px(rowGap)).
		Bg(gpui.Rgb(pageBackground)).
		P(gpui.Px(pagePadding)).
		Font(gpui.FontDescriptor{Family: "Geist", Weight: 400}).
		TextColor(gpui.Rgb(textColor)).
		Child(geistSizeRow("row-g16", "span-g16", 16.0, "Geist sixteen point")).
		Child(geistSizeRow("row-g24", "span-g24", 24.0, "Geist twenty-four")).
		Child(geistSizeRow("row-g48", "span-g48", 48.0, "Geist XL")).
		Child(geistMixedRow())
	return root.IntoElement()
}

// geistSizeRow builds one Geist font-size row (the sizeRow twin with
// the Geist family).
func geistSizeRow(rowID, spanID string, size float32, text string) *gpui.DivElement {
	return rowBox(rowID).
		Child(gpui.Div().
			ID(spanID).
			DebugSelector(spanID).
			Block().
			W(gpui.Px(spanWidth)).
			Bg(gpui.Rgb(spanBackground)).
			Font(gpui.FontDescriptor{Family: "Geist", Weight: 400}).
			TextSize(gpui.Px(size)).
			Child(gpui.Text(text)))
}

// geistMixedRow builds the Geist mixed row: a 16px paragraph carrying
// one 24px inline span.
func geistMixedRow() *gpui.DivElement {
	return rowBox("row-gmix").
		Child(gpui.Div().
			ID("span-gmix").
			DebugSelector("span-gmix").
			Block().
			W(gpui.Px(spanWidth)).
			Bg(gpui.Rgb(spanBackground)).
			Font(gpui.FontDescriptor{Family: "Geist", Weight: 400}).
			TextSize(gpui.Px(16.0)).
			Child(gpui.Text(mixedPrefix)).
			Child(gpui.Div().Inline().Font(gpui.FontDescriptor{Family: "Geist", Weight: 400}).TextSize(gpui.Px(24.0)).Child(gpui.Text(mixedBig))).
			Child(gpui.Text(mixedSuffix)))
}
