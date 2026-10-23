package base

import (
	"reflect"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/style.rs tests (284-379): 6 of 6.

func textSizeRefinement(px float32) gpui.StyleRefinement {
	return NewStateStyle().TextSize(gpui.Px(px)).intoRefinement()
}

// U:text/style.rs:284-308
func TestSelectionLayoutFingerprintCoversCallbackTableAndThemeFields(t *testing.T) {
	base := DefaultTextViewStyle()
	heading := base.WithHeading(func(uint8) gpui.StyleRefinement { return textSizeRefinement(14) })
	if !heading.Equal(base.WithHeading(func(uint8) gpui.StyleRefinement { return textSizeRefinement(14) })) {
		t.Fatal("equal heading callbacks compare unequal")
	}
	if heading.Equal(base.WithHeading(func(uint8) gpui.StyleRefinement { return textSizeRefinement(28) })) {
		t.Fatal("different heading callbacks compare equal")
	}
	var table gpui.StyleRefinement
	table.Text.WhiteSpace = gpui.Some(gpui.WhiteSpaceNowrap)
	if base.Equal(base.WithTableCell(table)) {
		t.Fatal("table cell refinement is not compared")
	}
	if base.Equal(base.WithDark(true)) {
		t.Fatal("is_dark is not compared")
	}
}

// U:text/style.rs:310-315
func TestCloningPreservesTheSameHeadingCallbackFingerprint(t *testing.T) {
	style := DefaultTextViewStyle().WithHeading(func(uint8) gpui.StyleRefinement { return textSizeRefinement(14) })
	clone := style
	if !style.Equal(clone) {
		t.Fatal("a clone compares unequal")
	}
}

// U:text/style.rs:317-331
func TestDefaultStyleIsReadableWithoutAnApplicationTheme(t *testing.T) {
	style := DefaultTextViewStyle()
	if style.Foreground().A != 1 || style.Link().A != 1 {
		t.Fatalf("foreground/link alpha = %v/%v", style.Foreground().A, style.Link().A)
	}
	if !(style.Selection().A > 0) || style.InlineCode().BackgroundColor == nil ||
		!(style.CodeBackground().A > 0) || !(style.Border().A > 0) {
		t.Fatal("default style has an invisible role")
	}
	cb := style.CodeBlock()
	for _, c := range []gpui.Optional[gpui.AbsoluteLength]{cb.CornerRadiiTopLeft, cb.CornerRadiiTopRight, cb.CornerRadiiBottomLeft, cb.CornerRadiiBottomRight} {
		if _, ok := c.Get(); ok {
			t.Fatal("the code block refinement has a corner radius")
		}
	}
}

// U:text/style.rs:333-349
func TestHeadingRefinementDefaultsEmptyAndResolvesByLevel(t *testing.T) {
	base := DefaultTextViewStyle()
	if !reflect.DeepEqual(base.Heading(1), gpui.StyleRefinement{}) {
		t.Fatal("the default heading refinement is not empty")
	}
	h1 := func() gpui.StyleRefinement {
		return NewStateStyle().Pt(gpui.RemsOf(1)).Pb(gpui.RemsOf(0.5)).intoRefinement()
	}
	rest := func() gpui.StyleRefinement { return NewStateStyle().Pb(gpui.RemsOf(0.25)).intoRefinement() }
	style := base.WithHeading(func(level uint8) gpui.StyleRefinement {
		if level == 1 {
			return h1()
		}
		return rest()
	})
	if !reflect.DeepEqual(style.Heading(1), h1()) || !reflect.DeepEqual(style.Heading(2), rest()) {
		t.Fatal("heading refinements do not resolve by level")
	}
	if style.Equal(base) {
		t.Fatal("a heading callback does not change equality")
	}
}

// U:text/style.rs:351-361
func TestInlineCodeFallsBackToTheCodeBackground(t *testing.T) {
	style := DefaultTextViewStyle().WithCodeBackground(gpui.Rgb(0x123456)).WithInlineCode(gpui.HighlightStyle{})
	bg := style.inlineCodeHighlight().BackgroundColor
	if bg == nil || *bg != gpui.Rgb(0x123456) {
		t.Fatalf("inline code background = %v", bg)
	}
}

// U:text/style.rs:363-379
func TestFromThemeMapsBaseSemanticTokens(t *testing.T) {
	theme := DefaultTheme()
	theme.Tokens.Colors.Foreground = gpui.Rgb(0x112233)
	theme.Tokens.Colors.MutedForeground = gpui.Rgb(0x445566)
	theme.Tokens.Colors.Primary = gpui.Rgb(0x3366ff)
	theme.Tokens.Colors.Accent = gpui.Rgb(0xddeeff)
	theme.Tokens.Colors.Border = gpui.Rgb(0x778899)
	theme.Tokens.Colors.Selection = gpui.Rgb(0x55a0fc)
	style := TextViewStyleFromTheme(theme)
	c := theme.Tokens.Colors
	if style.Foreground() != c.Foreground || style.Link() != c.Primary || style.Selection() != c.Selection ||
		style.CodeBackground() != c.Accent || style.Border() != c.Border {
		t.Fatal("theme tokens are not mapped")
	}
}
