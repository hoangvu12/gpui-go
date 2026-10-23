//go:build windows

// The underline fixture (ticket 06): Chrome's thickness and offset —
// a recorded divergence toward CSS over the pin's 1px at baseline +
// descent*0.618. The rules (verified in the Chromium checkout):
//
//   - auto thickness = max(1, font size / 10) — core/style/
//     text_decoration_thickness.cc:29-31 with the floor in core/paint/
//     text_decoration_info.cc:715-719;
//   - the underline top sits max(1, ceil(thickness / 2)) below the
//     alphabetic baseline — core/layout/text_decoration_offset.cc:
//     16-35.
//
// The words are chosen without descenders so Chrome's skip-ink (on by
// default, not ported — a native gap) never triggers and the fixture
// isolates the geometry.
package textstyle

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

const underlineText = "underline words"

// underlineHTML is the page: the same words at 16px and at 40px, where
// the auto geometry differs the most (1.6px thick at 16px, 4px at 40px).
var underlineHTML = page(`
	.row-underline-40 { height: 84px; font-size: 40px; line-height: 63px; }
	.underlined { text-decoration: underline; }
`, `
	<div class="row underlined row-underline-16" data-id="row-underline-16">`+underlineText+`</div>
	<div class="row underlined row-underline-40" data-id="row-underline-40">`+underlineText+`</div>
`)

// UnderlineFixture is the underline geometry fixture.
var UnderlineFixture = fixture("textstyle-underline", underlineHTML, underlineView,
	[]string{"row-underline-16", "row-underline-40"},
	// Provisional (research 19's calibration has not run), set from the
	// measured correct-view statistics (whole 0.116 / 0.813 mean and
	// 0.0118 hot in the 40px row) with the deliberately-wrong view (the
	// pin's fixed 1px thickness) measuring 0.304 / 2.384 / 0.0563 —
	// verified by the discrimination test in textstyle_test.go. The
	// 40px row's floor is large-size LCD rasterization noise, not
	// geometry: the 16px row measures 0.220 mean, 0.0035 hot.
	parity.PixelTolerance{
		MaxMeanChannelDiff:    0.24,
		MaxROIMeanChannelDiff: 1.7,
		MaxHotPixelFraction:   0.024,
	})

func init() { parity.Register(UnderlineFixture) }

// underlineView is the gpui twin: the same rows with .Underline() —
// the auto thickness and Chrome's offset.
func underlineView(log *parity.EventLog) gpui.AnyElement {
	return rootElement(
		rowElement("row-underline-16", underlineText, func(d *gpui.DivElement) *gpui.DivElement {
			return d.Underline()
		}),
		rowElement("row-underline-40", underlineText, func(d *gpui.DivElement) *gpui.DivElement {
			return d.Underline().H(gpui.Px(84)).TextSize(gpui.Px(40)).LineHeight(gpui.Px(63))
		}),
	)
}

// WrongUnderlineView is the deliberately-wrong view: the pin's fixed
// 1px thickness (TextDecoration1) instead of Chrome's auto. At 40px
// Chrome's underline is 4px thick, so the pixels differ loudly; at
// 16px 1.6px vs 1px keeps a smaller but visible difference.
func WrongUnderlineView(log *parity.EventLog) gpui.AnyElement {
	return rootElement(
		rowElement("row-underline-16", underlineText, func(d *gpui.DivElement) *gpui.DivElement {
			return d.TextDecoration1()
		}),
		rowElement("row-underline-40", underlineText, func(d *gpui.DivElement) *gpui.DivElement {
			return d.TextDecoration1().H(gpui.Px(84)).TextSize(gpui.Px(40)).LineHeight(gpui.Px(63))
		}),
	)
}
