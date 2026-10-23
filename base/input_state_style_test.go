package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of InputEditorStyle (U:input/editor/highlighting.rs:
// 182-258, 4/4).

func inputStyleDark() SemanticThemeTokens {
	tokens := DefaultSemanticThemeTokens()
	tokens.Colors.Foreground = gpui.Hsla{H: 0, S: 0, L: 0.98, A: 1}
	tokens.Colors.MutedForeground = gpui.Hsla{H: 0, S: 0, L: 0.64, A: 1}
	tokens.Colors.Surface = gpui.Hsla{H: 0, S: 0, L: 0.04, A: 1}
	tokens.Colors.Border = gpui.Hsla{H: 0, S: 0, L: 0.15, A: 1}
	tokens.Colors.Accent = gpui.Hsla{H: 0.6, S: 0.5, L: 0.5, A: 1}
	return tokens
}

// U:input/editor/highlighting.rs:199-219
func TestInputEditorStyleAnUnprojectedStyleTakesItsInkFromThePalette(t *testing.T) {
	tokens := inputStyleDark()
	resolved := DefaultInputEditorStyle().Resolved(&tokens)

	if resolved.Foreground != tokens.Colors.Foreground || resolved.Caret != tokens.Colors.Foreground ||
		resolved.MutedForeground != tokens.Colors.MutedForeground || resolved.Background != tokens.Colors.Surface ||
		resolved.Border != tokens.Colors.Border {
		t.Fatalf("resolved = %+v", resolved)
	}
	for _, c := range []gpui.Hsla{resolved.Foreground, resolved.Caret, resolved.MutedForeground, resolved.Selection} {
		if !(c.A > 0) {
			t.Fatalf("%v is still invisible", c)
		}
	}
}

// U:input/editor/highlighting.rs:221-225
func TestInputEditorStyleASelectionStaysTranslucentEnoughToReadThrough(t *testing.T) {
	tokens := inputStyleDark()
	if a := DefaultInputEditorStyle().Resolved(&tokens).Selection.A; a != 0.4 {
		t.Fatalf("selection alpha = %v, want 0.4", a)
	}
}

// U:input/editor/highlighting.rs:227-241
func TestInputEditorStyleProjectedColoursAreKeptVerbatim(t *testing.T) {
	chosen := gpui.Hsla{H: 0.3, S: 0.4, L: 0.5, A: 1}
	style := DefaultInputEditorStyle()
	style.Foreground, style.Caret = chosen, chosen
	tokens := inputStyleDark()
	resolved := style.Resolved(&tokens)
	if resolved.Foreground != chosen || resolved.Caret != chosen {
		t.Fatalf("projected colours changed: %+v", resolved)
	}
	if resolved.Border != tokens.Colors.Border {
		t.Fatalf("border = %v, want the palette's", resolved.Border)
	}
}

// U:input/editor/highlighting.rs:243-257
func TestInputEditorStyleResolutionNeverConsumesItsOwnOutput(t *testing.T) {
	projected := DefaultInputEditorStyle()
	dark := inputStyleDark()
	first := projected.Resolved(&dark)

	light := DefaultSemanticThemeTokens()
	light.Colors.Foreground = gpui.Hsla{H: 0, S: 0, L: 0.04, A: 1}
	second := projected.Resolved(&light)

	if first.Foreground == second.Foreground {
		t.Fatal("the second resolution kept the first palette")
	}
	if second.Foreground != light.Colors.Foreground {
		t.Fatalf("foreground = %v, want %v", second.Foreground, light.Colors.Foreground)
	}
}
