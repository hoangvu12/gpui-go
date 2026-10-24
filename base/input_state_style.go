package base

// InputEditorStyle and DiagnosticColors (U:input/editor/highlighting.rs:
// 99-180): the application-owned colors the input paints with. The state
// keeps the projected style verbatim and resolves it against the theme
// palette at the top of every render (U:input/base/state.rs:4358-4360).
//
// The resolver and the fold-icon renderer are interface / func values, so
// a nil HighlightStyles resolves nothing (NoHighlightStyles) and a nil
// FoldIconRenderer is None.

import "github.com/hoangvu12/gpui-go/gpui"

// DiagnosticColors are the diagnostic severity colors
// (U:input/editor/highlighting.rs:99-105).
type DiagnosticColors struct {
	Error   gpui.Hsla
	Warning gpui.Hsla
	Info    gpui.Hsla
	Hint    gpui.Hsla
}

// InputEditorStyle is the application-owned colors consumed by input
// painting (U:input/editor/highlighting.rs:107-122). A transparent color
// (alpha 0, the zero value) is "unset" and resolves from the palette. Nil
// optional colors are Rust's None.
type InputEditorStyle struct {
	Foreground      gpui.Hsla
	MutedForeground gpui.Hsla
	Background      gpui.Hsla
	Border          gpui.Hsla
	Selection       gpui.Hsla
	Caret           gpui.Hsla
	Diagnostics     DiagnosticColors
	// HighlightStyles resolves semantic highlight names; nil resolves
	// nothing (U:input/editor/highlighting.rs:117).
	HighlightStyles        SharedHighlightStyleResolver
	EditorInvisible        *gpui.Hsla
	EditorActiveLine       *gpui.Hsla
	EditorGutterBackground *gpui.Hsla
	// FoldIconRenderer renders the gutter fold icons; nil uses the
	// built-in chevrons (U:input/editor/highlighting.rs:121).
	FoldIconRenderer FoldIconRenderer
}

// DefaultInputEditorStyle is `impl Default for InputEditorStyle`: every
// color unset (U:input/editor/highlighting.rs:162-180). The Go zero value
// is the same style.
func DefaultInputEditorStyle() InputEditorStyle {
	unset := gpui.TransparentBlack()
	return InputEditorStyle{
		Foreground:      unset,
		MutedForeground: unset,
		Background:      unset,
		Border:          unset,
		Selection:       unset,
		Caret:           unset,
		HighlightStyles: noHighlightStyles{},
	}
}

// Resolved fills in every color that was left unset from the palette;
// whatever a consumer projected is kept exactly
// (U:input/editor/highlighting.rs:124-160). An unset selection takes
// the system's selection wash instead of the pinned accent rule
// (browser-core 28, D0): selected text paints with the colours the
// platform reports, the ::selection default Blink resolves from the
// theme's active selection background (highlight_style_utils.cc:188-
// 201) — a translucent blend-with-white wash, so the glyphs stay
// readable through it (the pinned "must not hide the glyphs" property).
func (s InputEditorStyle) Resolved(app *gpui.App, tokens *SemanticThemeTokens) InputEditorStyle {
	colors := &tokens.Colors
	unset := func(value gpui.Hsla) bool { return value.A == 0 }
	or := func(value, fallback gpui.Hsla) gpui.Hsla {
		if unset(value) {
			return fallback
		}
		return value
	}

	foreground := or(s.Foreground, colors.Foreground)
	selection := s.Selection
	if unset(selection) {
		selection = systemSelectionColors(app).Background
	}

	out := s
	out.Foreground = foreground
	out.MutedForeground = or(s.MutedForeground, colors.MutedForeground)
	out.Background = or(s.Background, colors.Surface)
	out.Border = or(s.Border, colors.Border)
	out.Selection = selection
	out.Caret = or(s.Caret, foreground)
	return out
}
