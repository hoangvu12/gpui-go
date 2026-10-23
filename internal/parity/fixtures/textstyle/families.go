//go:build windows

// The generic-families fixture (ticket 06): the CSS generic keywords
// resolve, and the Tailwind font-sans/serif/mono classes work. The
// keywords map to Chrome's Windows defaults (sans-serif → Arial,
// serif → Times New Roman, monospace → Consolas — Blink's
// FontSelector::FamilyNameFromSettings, platform/fonts/
// font_selector.cc:74-83, whose Windows settings match Fontique's
// Windows generic table, fontique-0.11.1/src/backend/dwrite.rs:31-39);
// the Tailwind default stacks resolve on Windows Chrome to the system
// UI font (Segoe UI, via system-ui), Georgia and Consolas.
package textstyle

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

const familiesText = "Sphinx 123"

// Tailwind v3's default theme stacks (tailwindcss/stubs/config.full.js
// theme.fontFamily), the CSS the classes stand for.
const (
	tailwindSansStack  = `ui-sans-serif, system-ui, sans-serif, Arial, Helvetica, sans-serif`
	tailwindSerifStack = `ui-serif, Georgia, Cambria, "Times New Roman", Times, serif`
	tailwindMonoStack  = `ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace`
)

// familiesHTML is the page: one row per generic keyword and one per
// Tailwind stack.
var familiesHTML = page(`
	.row-gen-sans { font-family: sans-serif; }
	.row-gen-serif { font-family: serif; }
	.row-gen-mono { font-family: monospace; }
	.row-tw-sans { font-family: `+tailwindSansStack+`; }
	.row-tw-serif { font-family: `+tailwindSerifStack+`; }
	.row-tw-mono { font-family: `+tailwindMonoStack+`; }
`, `
	<div class="row row-gen-sans" data-id="row-gen-sans">`+familiesText+`</div>
	<div class="row row-gen-serif" data-id="row-gen-serif">`+familiesText+`</div>
	<div class="row row-gen-mono" data-id="row-gen-mono">`+familiesText+`</div>
	<div class="row row-tw-sans" data-id="row-tw-sans">`+familiesText+`</div>
	<div class="row row-tw-serif" data-id="row-tw-serif">`+familiesText+`</div>
	<div class="row row-tw-mono" data-id="row-tw-mono">`+familiesText+`</div>
`)

// FamiliesFixture is the generic-families fixture.
var FamiliesFixture = fixture("textstyle-generic-families", familiesHTML, familiesView,
	[]string{"row-gen-sans", "row-gen-serif", "row-gen-mono", "row-tw-sans", "row-tw-serif", "row-tw-mono"},
	// Provisional (research 19's calibration has not run), set from the
	// measured correct-view statistics (whole 0.028 / 0.089 in the worst
	// row / 0.0001 hot) with the deliberately-wrong view (every row in
	// the default family) measuring 0.232 / 1.225 / 0.0209 — verified by
	// the discrimination test in textstyle_test.go.
	parity.PixelTolerance{
		MaxMeanChannelDiff:    0.07,
		MaxROIMeanChannelDiff: 0.2,
		MaxHotPixelFraction:   0.001,
	})

func init() { parity.Register(FamiliesFixture) }

// familiesView is the gpui twin: the generic keywords as FontFamily
// values (the alias table resolves them) and the Tailwind classes.
func familiesView(log *parity.EventLog) gpui.AnyElement {
	return rootElement(
		rowElement("row-gen-sans", familiesText, func(d *gpui.DivElement) *gpui.DivElement {
			return d.FontFamily("sans-serif")
		}),
		rowElement("row-gen-serif", familiesText, func(d *gpui.DivElement) *gpui.DivElement {
			return d.FontFamily("serif")
		}),
		rowElement("row-gen-mono", familiesText, consolasRow),
		rowElement("row-tw-sans", familiesText, func(d *gpui.DivElement) *gpui.DivElement {
			return d.Class("font-sans")
		}),
		rowElement("row-tw-serif", familiesText, func(d *gpui.DivElement) *gpui.DivElement {
			return d.Class("font-serif")
		}),
		rowElement("row-tw-mono", familiesText, func(d *gpui.DivElement) *gpui.DivElement {
			return consolasRow(d).Class("font-mono")
		}),
	)
}

// consolasRow styles one monospace row: Consolas with the line height
// the page pins PLUS the 3px Consolas needs on the gpui side — like the
// tracking fixture's Constantia (tracking.go constantiaRow): Chrome
// reads the font's VDMX metrics (platform/fonts/font_metrics.cc:95-97)
// and places the baseline 1.5 logical px below the port's hhea-based
// shaper; the taller (invisible, the rows are fixed-height) line box
// shifts the port's baseline onto Chrome's row.
func consolasRow(d *gpui.DivElement) *gpui.DivElement {
	return d.FontFamily("monospace").LineHeight(gpui.Px(rowLineHeight + 3))
}

// WrongFamiliesView is the deliberately-wrong view: every row in the
// default family — the port's pre-fix behaviour (the generic keywords
// resolved to nothing and the classes were rejected).
func WrongFamiliesView(log *parity.EventLog) gpui.AnyElement {
	return rootElement(
		rowElement("row-gen-sans", familiesText, nil),
		rowElement("row-gen-serif", familiesText, nil),
		rowElement("row-gen-mono", familiesText, consolasRow),
		rowElement("row-tw-sans", familiesText, nil),
		rowElement("row-tw-serif", familiesText, nil),
		rowElement("row-tw-mono", familiesText, consolasRow),
	)
}
