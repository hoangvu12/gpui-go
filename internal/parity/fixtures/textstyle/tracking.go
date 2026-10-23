//go:build windows

// The letter-spacing fixture (ticket 06): non-zero letter spacing turns
// off the optional ligatures (CSS Text 3 §8.2: UAs should not apply
// optional ligatures under non-zero letter-spacing; Chromium pushes
// liga=0, clig=0 and calt=0 when spacing != 0, platform/fonts/shaping/
// font_features.cc:59-86). Cambria is the family the native feature
// test observed a liga substitution in (reference/native/src/text.rs:
// 3862-3865): "fi" ligates by default and shapes as separate glyphs
// under spacing.
package textstyle

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

const (
	ligatureText = "affiliation traffic office"
	spacing      = 0.5
)

// trackingHTML is the page: the same Cambria text with no spacing
// (ligatures on) and with 0.5px spacing (ligatures off).
var trackingHTML = page(`
	.row {
		font-family: Constantia, serif;
	}
	.row-spacing { letter-spacing: 0.5px; }
`, `
	<div class="row row-liga" data-id="row-liga">`+ligatureText+`</div>
	<div class="row row-spacing" data-id="row-spacing">`+ligatureText+`</div>
`)

// TrackingFixture is the letter-spacing/ligature fixture.
var TrackingFixture = fixture("textstyle-tracking-ligatures", trackingHTML, trackingView,
	[]string{"row-liga", "row-spacing"},
	// Provisional (research 19's calibration has not run), set from the
	// measured correct-view statistics (whole 0.019 / 0.170 / 0.0001)
	// with the deliberately-wrong view (the ligatures forced back on
	// under spacing) measuring 0.147 / 2.080 / 0.0346 — verified by the
	// discrimination test in textstyle_test.go.
	parity.PixelTolerance{
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.4,
		MaxHotPixelFraction:   0.002,
	})

func init() { parity.Register(TrackingFixture) }

// trackingView is the gpui twin: Constantia at 16px, one row plain and
// one with LetterSpacing(0.5) — the spacing run must shape without the
// ligatures.
func trackingView(log *parity.EventLog) gpui.AnyElement {
	return rootElement(
		rowElement("row-liga", ligatureText, constantiaRow),
		rowElement("row-spacing", ligatureText, func(d *gpui.DivElement) *gpui.DivElement {
			return constantiaRow(d).LetterSpacing(spacing)
		}),
	)
}

// constantiaRow styles one tracking row: Constantia (whose fi/ffi
// substitutions change the advance, where Cambria's only swap
// near-identical glyphs at an equal advance) with the line height the
// page pins PLUS the 3px Constantia needs on the gpui side: Chrome
// reads the font's VDMX metrics (platform/fonts/font_metrics.cc:95-97)
// and places this row's baseline 1.5 logical px below the port's
// hhea-based shaper, and the taller (invisible) line box shifts the
// port's baseline down onto Chrome's row. Without it the metric offset
// alone measures a 1.66 mean channel diff and swamps the ligature
// signal the fixture exists for.
func constantiaRow(d *gpui.DivElement) *gpui.DivElement {
	return d.FontFamily("Constantia").LineHeight(gpui.Px(rowLineHeight + 3))
}

// WrongTrackingView is the deliberately-wrong view: the spaced row
// forces the common ligatures back on with an explicit liga=1 — the
// port's pre-fix behaviour (letter spacing alone never disabled them).
// Chrome keeps them off, so the spaced row's glyphs differ.
func WrongTrackingView(log *parity.EventLog) gpui.AnyElement {
	return rootElement(
		rowElement("row-liga", ligatureText, constantiaRow),
		rowElement("row-spacing", ligatureText, func(d *gpui.DivElement) *gpui.DivElement {
			return constantiaRow(d).LetterSpacing(spacing).
				FontFeatures([]gpui.FontFeature{{Tag: "liga", Value: 1}})
		}),
	)
}
