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
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	app := ta.App()
	tokens := inputStyleDark()
	resolved := DefaultInputEditorStyle().Resolved(app, &tokens)

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

// U:input/editor/highlighting.rs:221-225, diverged toward the browser
// (D0, browser-core 28): the pin resolved an unset selection from the
// palette accent at 0.4 alpha; the port takes the system's selection
// wash, which keeps the same read-through property.
func TestInputEditorStyleASelectionStaysTranslucentEnoughToReadThrough(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	app := ta.App()
	tokens := inputStyleDark()
	selection := DefaultInputEditorStyle().Resolved(app, &tokens).Selection
	want := app.SelectionColors().Background
	if selection != want {
		t.Fatalf("selection = %v, want the system wash %v", selection, want)
	}
	if !(selection.A > 0 && selection.A < 1) {
		t.Fatalf("selection alpha = %v, want a translucent wash the glyphs read through", selection.A)
	}
}

// browser-core 28: a projected selection is kept verbatim — the app's
// own ::selection styling wins over the system.
func TestInputEditorStyleAProjectedSelectionIsKeptVerbatim(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	app := ta.App()
	tokens := inputStyleDark()
	chosen := gpui.Hsla{H: 0.3, S: 0.4, L: 0.5, A: 0.6}
	style := DefaultInputEditorStyle()
	style.Selection = chosen
	if resolved := style.Resolved(app, &tokens); resolved.Selection != chosen {
		t.Fatalf("projected selection = %v, want %v", resolved.Selection, chosen)
	}
}

// U:input/editor/highlighting.rs:227-241
func TestInputEditorStyleProjectedColoursAreKeptVerbatim(t *testing.T) {
	chosen := gpui.Hsla{H: 0.3, S: 0.4, L: 0.5, A: 1}
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	app := ta.App()
	style := DefaultInputEditorStyle()
	style.Foreground, style.Caret = chosen, chosen
	tokens := inputStyleDark()
	resolved := style.Resolved(app, &tokens)
	if resolved.Foreground != chosen || resolved.Caret != chosen {
		t.Fatalf("projected colours changed: %+v", resolved)
	}
	if resolved.Border != tokens.Colors.Border {
		t.Fatalf("border = %v, want the palette's", resolved.Border)
	}
}

// U:input/editor/highlighting.rs:243-257
func TestInputEditorStyleResolutionNeverConsumesItsOwnOutput(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	app := ta.App()
	projected := DefaultInputEditorStyle()
	dark := inputStyleDark()
	first := projected.Resolved(app, &dark)

	light := DefaultSemanticThemeTokens()
	light.Colors.Foreground = gpui.Hsla{H: 0, S: 0, L: 0.04, A: 1}
	second := projected.Resolved(app, &light)

	if first.Foreground == second.Foreground {
		t.Fatal("the second resolution kept the first palette")
	}
	if second.Foreground != light.Colors.Foreground {
		t.Fatalf("foreground = %v, want %v", second.Foreground, light.Colors.Foreground)
	}
}
