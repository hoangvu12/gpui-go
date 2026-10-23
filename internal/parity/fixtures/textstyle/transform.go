//go:build windows

// The text-transform fixture (ticket 06): uppercase, lowercase and
// capitalize applied — a port gap against the pin (the pin's
// apply_text_transform_preserving_byte_len, ce:crates/gpui/src/
// elements/text.rs:636-691), not a divergence. Chrome's capitalize
// uppercases the first ALPHABETIC character of every UAX #29 word and
// leaves the rest alone (CSS Text 3 §2.1; the pin ports the same
// rule), so "quick-brown" capitalizes both segments and "WORLD" keeps
// its case.
package textstyle

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

const (
	upperSource = "the déjà fox"
	lowerSource = "THE DÉJÀ FOX"
	capSource   = "the déjà-brown fox"
)

// transformHTML is the page: one row per transform.
var transformHTML = page(`
	.row-upper { text-transform: uppercase; }
	.row-lower { text-transform: lowercase; }
	.row-cap { text-transform: capitalize; }
`, `
	<div class="row row-upper" data-id="row-upper">`+upperSource+`</div>
	<div class="row row-lower" data-id="row-lower">`+lowerSource+`</div>
	<div class="row row-cap" data-id="row-cap">`+capSource+`</div>
`)

// TransformFixture is the text-transform fixture.
var TransformFixture = fixture("textstyle-transform", transformHTML, transformView,
	[]string{"row-upper", "row-lower", "row-cap"},
	// Provisional (research 19's calibration has not run), set from the
	// measured correct-view statistics and verified by the
	// discrimination test below.
	// Provisional (research 19's calibration has not run), set from the
	// measured correct-view statistics (whole 0.023 mean / 0.188 in the
	// worst row / 0.0001 hot) with the deliberately-wrong view (the raw
	// untransformed text) measuring 0.310 / 1.880 / 0.0052 — verified by
	// the discrimination test in textstyle_test.go.
	parity.PixelTolerance{
		MaxMeanChannelDiff:    0.06,
		MaxROIMeanChannelDiff: 0.4,
		MaxHotPixelFraction:   0.002,
	})

func init() { parity.Register(TransformFixture) }

// transformView is the gpui twin: the same rows with the style's
// TextTransform (the fluent methods Uppercase/Lowercase/Capitalize set
// the same fields the classes set).
func transformView(log *parity.EventLog) gpui.AnyElement {
	return rootElement(
		rowElement("row-upper", upperSource, func(d *gpui.DivElement) *gpui.DivElement {
			return d.Uppercase()
		}),
		rowElement("row-lower", lowerSource, func(d *gpui.DivElement) *gpui.DivElement {
			return d.Lowercase()
		}),
		rowElement("row-cap", capSource, func(d *gpui.DivElement) *gpui.DivElement {
			return d.Capitalize()
		}),
	)
}

// WrongTransformView is the deliberately-wrong view: no transform at
// all — the port's pre-fix behaviour. The boxes stay identical (the
// rows are fixed-size), so only Tier C fails: the glyphs are the raw
// source's.
func WrongTransformView(log *parity.EventLog) gpui.AnyElement {
	return rootElement(
		rowElement("row-upper", upperSource, nil),
		rowElement("row-lower", lowerSource, nil),
		rowElement("row-cap", capSource, nil),
	)
}
