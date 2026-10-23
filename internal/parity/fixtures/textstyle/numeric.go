//go:build windows

// The numeric-variant fixture (ticket 06): tabular-nums and friends as
// Tailwind classes. The classes map to their OpenType features
// (Chromium's FromFontDescription, platform/fonts/shaping/
// font_features.cc:157-196). The rows use Arial: its default figures
// are proportional (111 advances 24.3px where 888 advances 26.7px at
// 16px), so tabular-nums visibly changes the digit advances — Segoe
// UI's figures are tabular by default and the class would be a no-op.
// Arial's metric rounding also keeps the two sides' baselines on the
// same device row (rowStyles' comment).
package textstyle

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

const numericText = "111 888 111"

// numericHTML is the page: the same digits plain (proportional, Arial's
// default) and with tabular-nums.
var numericHTML = page(`
	.row { font-family: Arial, sans-serif; }
	.row-tnum { font-variant-numeric: tabular-nums; }
`, `
	<div class="row row-plain" data-id="row-plain">`+numericText+`</div>
	<div class="row row-tnum" data-id="row-tnum">`+numericText+`</div>
`)

// NumericFixture is the tabular-nums fixture.
var NumericFixture = fixture("textstyle-tabular-nums", numericHTML, numericView,
	[]string{"row-plain", "row-tnum"},
	// Provisional (research 19's calibration has not run), set from the
	// measured correct-view statistics (whole 0.015 / 0.132 / 0.0001)
	// with the deliberately-wrong view (the tnum row without the
	// feature — Arial's proportional figures) measuring 0.085 / 1.167 /
	// 0.0021 — verified by the discrimination test in textstyle_test.go
	// (the wrong view fails the ROI gate).
	parity.PixelTolerance{
		MaxMeanChannelDiff:    0.05,
		MaxROIMeanChannelDiff: 0.3,
		MaxHotPixelFraction:   0.003,
	})

func init() { parity.Register(NumericFixture) }

// numericView is the gpui twin: the plain row and the tabular-nums
// class.
func numericView(log *parity.EventLog) gpui.AnyElement {
	arial := func(d *gpui.DivElement) *gpui.DivElement { return d.FontFamily("Arial") }
	return rootElement(
		rowElement("row-plain", numericText, arial),
		rowElement("row-tnum", numericText, func(d *gpui.DivElement) *gpui.DivElement {
			return arial(d).Class("tabular-nums")
		}),
	)
}

// WrongNumericView is the deliberately-wrong view: the tnum row without
// the feature — the port's pre-fix behaviour (the class was rejected
// and the digits stayed proportional).
func WrongNumericView(log *parity.EventLog) gpui.AnyElement {
	arial := func(d *gpui.DivElement) *gpui.DivElement { return d.FontFamily("Arial") }
	return rootElement(
		rowElement("row-plain", numericText, arial),
		rowElement("row-tnum", numericText, arial),
	)
}
