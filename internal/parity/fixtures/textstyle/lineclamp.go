//go:build windows

// The line-clamp fixture (ticket 06): line-clamp ends the last visible
// line with an ellipsis (CSS Overflow 4 §line-clamp). Chrome's
// LineTruncator draws U+2026 when the primary font has the glyph
// (Segoe UI does) — the port resolves the same marker through
// TextSystem.GlyphForChar (text_truncate.go cssEllipsisAffix). A
// divergence toward CSS over the pin (the pin's line clamp cut the
// text with no marker).
package textstyle

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

const clampText = "one two three four five six seven eight nine ten eleven twelve thirteen fourteen"

// clampHTML is the page: a 260x68px row (two 25px lines) with the text
// clamped to 2 lines and hidden overflow.
var clampHTML = page(`
	.row-clamp {
		width: 260px;
		height: 68px;
	}
	.row-clamp > span {
		line-clamp: 2;
		overflow: hidden;
		display: block;
	}
`, `
	<div class="row row-clamp" data-id="row-clamp"><span>`+clampText+`</span></div>
`)

// LineClampFixture is the line-clamp ellipsis fixture.
var LineClampFixture = fixture("textstyle-line-clamp", clampHTML, lineClampView,
	[]string{"row-clamp"},
	// Provisional (research 19's calibration has not run), set from the
	// measured correct-view statistics (whole 0.126 / 0.579 / 0.0045)
	// with the deliberately-wrong view (a clamp of 1 instead of 2)
	// measuring 0.208 / 1.769 / 0.0278 — verified by the discrimination
	// test in textstyle_test.go. The correct view's floor is the
	// truncation point: the port's grapheme bisection keeps about two
	// glyphs more than Chrome's line truncator on the clamped line (a
	// pin behaviour, not this ticket's change).
	parity.PixelTolerance{
		MaxMeanChannelDiff:    0.26,
		MaxROIMeanChannelDiff: 1.2,
		MaxHotPixelFraction:   0.01,
	})

func init() { parity.Register(LineClampFixture) }

// lineClampView is the gpui twin: the row is a 260x54px div with
// LineClamp(2) and hidden overflow — the clamp itself synthesizes the
// end-ellipsis truncation.
func lineClampView(log *parity.EventLog) gpui.AnyElement {
	row := gpui.Div().Flex().
		ID("row-clamp").
		DebugSelector("row-clamp").
		FlexCol().
		Size(gpui.Px(260), gpui.Px(68)).
		Bg(gpui.Rgb(rowBackground)).
		P(gpui.Px(rowPad)).
		Border(gpui.Px(rowBorder)).
		BorderColor(gpui.Rgb(borderColor)).
		TextColor(gpui.Rgb(textColor)).
		TextSize(gpui.Px(textSize)).
		LineHeight(gpui.Px(rowLineHeight)).
		OverflowHidden().
		LineClamp(2)
	return rootElement(row.Child(clampText))
}

// WrongLineClampView is the deliberately-wrong view: the clamp count is
// 1 instead of 2, so the whole second line (with its ellipsis) is gone
// and the first line truncates early. Tier C fails loudly. (The subtler
// defect — the ellipsis missing with the clamp otherwise right — sits
// inside the fixture's truncation-point noise: the port's grapheme
// bisection keeps about two glyphs more than Chrome's line truncator,
// which the correct view carries as its measured Tier C floor; see the
// fixture's tolerance comment.)
func WrongLineClampView(log *parity.EventLog) gpui.AnyElement {
	row := gpui.Div().Flex().
		ID("row-clamp").
		DebugSelector("row-clamp").
		FlexCol().
		Size(gpui.Px(260), gpui.Px(68)).
		Bg(gpui.Rgb(rowBackground)).
		P(gpui.Px(rowPad)).
		Border(gpui.Px(rowBorder)).
		BorderColor(gpui.Rgb(borderColor)).
		TextColor(gpui.Rgb(textColor)).
		TextSize(gpui.Px(textSize)).
		LineHeight(gpui.Px(rowLineHeight)).
		OverflowHidden().
		LineClamp(1)
	return rootElement(row.Child(clampText))
}
