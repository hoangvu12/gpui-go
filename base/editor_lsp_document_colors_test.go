package base

// Ported tests of U:input/editor/lsp/document_colors.rs (2 of 2).

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// U:input/editor/lsp/document_colors.rs:131-154
func TestDocumentColorsFromResponseEnforcesLimit(t *testing.T) {
	color := LspColorInformation{
		Range: NewLspRange(NewInputPosition(0, 0), NewInputPosition(0, 1)),
		Color: LspColor{Red: 1, Green: 0, Blue: 0, Alpha: 1},
	}
	colors := slices.Repeat([]LspColorInformation{color}, maxDocumentColors+1)

	got, ok := documentColorsFromResponse(colors[:maxDocumentColors])
	if !ok || len(got) != maxDocumentColors {
		t.Fatalf("at the limit: %d colors, ok %v", len(got), ok)
	}
	if _, ok := documentColorsFromResponse(colors); ok {
		t.Fatal("over the limit must be rejected")
	}
}

// U:input/editor/lsp/document_colors.rs:156-179
func TestDocumentColorsForRangeIgnoresEmptyAndInvertedRanges(t *testing.T) {
	text := rope.FromStr("01234567890123456789")
	lsp := DefaultLsp()
	blue := gpui.Hsla{H: 0.6666667, S: 1, L: 0.5, A: 1}
	lsp.documentColors = append(lsp.documentColors,
		lspDocumentColor{NewLspRange(NewInputPosition(0, 10), NewInputPosition(0, 5)), gpui.Red()},
		lspDocumentColor{NewLspRange(NewInputPosition(0, 10), NewInputPosition(0, 10)), gpui.Hsla{H: 1.0 / 3, S: 1, L: 0.25, A: 1}},
		lspDocumentColor{NewLspRange(NewInputPosition(0, 5), NewInputPosition(0, 10)), blue},
	)

	got := lsp.documentColorsForRange(text, NewInputSelection(0, 0))
	want := []inputColoredRange{{rng: NewInputSelection(5, 10), color: blue}}
	if !slices.Equal(got, want) {
		t.Fatalf("colors = %+v, want %+v", got, want)
	}
}

// Go-own: Rgba::new(..).into_color() matches the core's byte-exact
// conversion on byte-representable channels.
func TestLspColorToHslaMatchesCoreConversion(t *testing.T) {
	for _, hex := range []uint32{0xff0000ff, 0x00ff00ff, 0x336699cc, 0x000000ff, 0xffffff80} {
		r, g, b, a := gpui.RgbaFromHex(hex)
		if got, want := lspColorToHsla(LspColor{Red: r, Green: g, Blue: b, Alpha: a}), gpui.RgbaToHsla(hex); got != want {
			t.Fatalf("%08x: %+v, want %+v", hex, got, want)
		}
	}
}
