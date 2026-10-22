package base

// Port of U:text/style.rs: TextViewStyle, the presentation of a TextView
// (ticket 28).
//
// Go adaptations:
//   - Rems (paragraph_gap) is a gpui.AbsoluteLength in rems.
//   - The heading callback is a func(level uint8) StyleRefinement; equality
//     compares its result for levels 1-6, as upstream
//     (U:text/style.rs:31-48). StyleRefinement has slice-valued fields, so
//     refinements are compared with reflect.DeepEqual (the derived
//     PartialEq compares the same fields).
//   - Builders return a modified copy (Rust's `mut self` builders).

import (
	"reflect"

	"github.com/hoangvu12/gpui-go/gpui"
)

// TextViewStyle customizes the style of a TextView (U:text/style.rs:7-29).
// Build one with the With* methods and read it back through the accessors.
type TextViewStyle struct {
	foreground      gpui.Hsla
	mutedForeground gpui.Hsla
	link            gpui.Hsla
	selection       gpui.Hsla
	codeBackground  gpui.Hsla
	border          gpui.Hsla
	paragraphGap    gpui.AbsoluteLength
	heading         func(level uint8) gpui.StyleRefinement
	codeBlock       gpui.StyleRefinement
	table           gpui.StyleRefinement
	tableHead       gpui.StyleRefinement
	tableCell       gpui.StyleRefinement
	inlineCode      gpui.HighlightStyle
	isDark          bool
}

// DefaultTextViewStyle is TextViewStyle::default: the light palette
// (U:text/style.rs:50-54).
func DefaultTextViewStyle() TextViewStyle {
	return textViewStyleFromColors(LightColorTokens(), false)
}

// TextViewStyleFromTheme derives rich-text colors from Base semantic theme
// tokens (U:text/style.rs:57-63).
func TextViewStyleFromTheme(theme Theme) TextViewStyle {
	return textViewStyleFromColors(theme.Tokens.Colors, theme.Appearance == ThemeAppearanceDark)
}

// textViewStyleFromColors maps a palette onto the rich-text roles
// (U:text/style.rs:65-90).
func textViewStyleFromColors(colors ColorTokens, isDark bool) TextViewStyle {
	accent := colors.Accent
	return TextViewStyle{
		foreground:      colors.Foreground,
		mutedForeground: colors.MutedForeground,
		link:            colors.Primary,
		selection:       colors.Selection,
		codeBackground:  colors.Accent,
		border:          colors.Border,
		paragraphGap:    gpui.RemsOf(1),
		heading:         func(uint8) gpui.StyleRefinement { return gpui.StyleRefinement{} },
		inlineCode:      gpui.HighlightStyle{BackgroundColor: &accent},
		isDark:          isDark,
	}
}

// WithForeground sets the default body-text color (U:text/style.rs:92-96).
func (s TextViewStyle) WithForeground(color gpui.Hsla) TextViewStyle { s.foreground = color; return s }

// WithMutedForeground sets the secondary text color (U:text/style.rs:98-102).
func (s TextViewStyle) WithMutedForeground(color gpui.Hsla) TextViewStyle {
	s.mutedForeground = color
	return s
}

// WithLink sets the link text color (U:text/style.rs:104-108).
func (s TextViewStyle) WithLink(color gpui.Hsla) TextViewStyle { s.link = color; return s }

// WithSelection sets the background painted behind selected text, normally
// a translucent wash (U:text/style.rs:110-117).
func (s TextViewStyle) WithSelection(color gpui.Hsla) TextViewStyle { s.selection = color; return s }

// WithCodeBackground sets the background of fenced code blocks and table
// header rows (U:text/style.rs:119-123).
func (s TextViewStyle) WithCodeBackground(color gpui.Hsla) TextViewStyle {
	s.codeBackground = color
	return s
}

// WithBorder sets the color of borders and horizontal rules
// (U:text/style.rs:125-129).
func (s TextViewStyle) WithBorder(color gpui.Hsla) TextViewStyle { s.border = color; return s }

// WithParagraphGap sets the gap between paragraphs, 1 rem by default
// (U:text/style.rs:131-135).
func (s TextViewStyle) WithParagraphGap(gap gpui.AbsoluteLength) TextViewStyle {
	s.paragraphGap = gap
	return s
}

// WithHeading sets the style refinement for headings, selected by heading
// level 1-6 (U:text/style.rs:137-144).
func (s TextViewStyle) WithHeading(heading func(level uint8) gpui.StyleRefinement) TextViewStyle {
	s.heading = heading
	return s
}

// WithCodeBlock sets the style refinement for code blocks
// (U:text/style.rs:146-150).
func (s TextViewStyle) WithCodeBlock(style gpui.StyleRefinement) TextViewStyle {
	s.codeBlock = style
	return s
}

// WithInlineCode sets the highlight style for inline code spans; without a
// background color the neutral code background is used
// (U:text/style.rs:152-159).
func (s TextViewStyle) WithInlineCode(style gpui.HighlightStyle) TextViewStyle {
	s.inlineCode = style
	return s
}

// WithTable sets the style refinement for the table container; overflow-x
// scroll opts into the adaptive scroll layout (U:text/style.rs:161-171).
func (s TextViewStyle) WithTable(style gpui.StyleRefinement) TextViewStyle {
	s.table = style
	return s
}

// WithTableHead sets the style refinement for the header row
// (U:text/style.rs:173-178).
func (s TextViewStyle) WithTableHead(style gpui.StyleRefinement) TextViewStyle {
	s.tableHead = style
	return s
}

// WithTableCell sets the style refinement for each table cell
// (U:text/style.rs:180-188).
func (s TextViewStyle) WithTableCell(style gpui.StyleRefinement) TextViewStyle {
	s.tableCell = style
	return s
}

// WithDark sets whether content-specific assets use their dark variant
// (U:text/style.rs:190-194).
func (s TextViewStyle) WithDark(isDark bool) TextViewStyle { s.isDark = isDark; return s }

// Foreground is the default body-text color.
func (s TextViewStyle) Foreground() gpui.Hsla { return s.foreground }

// MutedForeground is the secondary text color.
func (s TextViewStyle) MutedForeground() gpui.Hsla { return s.mutedForeground }

// Link is the link text color.
func (s TextViewStyle) Link() gpui.Hsla { return s.link }

// Selection is the background painted behind selected text.
func (s TextViewStyle) Selection() gpui.Hsla { return s.selection }

// CodeBackground is the background of fenced code blocks and table header
// rows.
func (s TextViewStyle) CodeBackground() gpui.Hsla { return s.codeBackground }

// Border is the color of borders and horizontal rules.
func (s TextViewStyle) Border() gpui.Hsla { return s.border }

// ParagraphGap is the gap between paragraphs.
func (s TextViewStyle) ParagraphGap() gpui.AbsoluteLength { return s.paragraphGap }

// Heading is the style refinement for a heading at level 1-6
// (U:text/style.rs:231-234).
func (s TextViewStyle) Heading(level uint8) gpui.StyleRefinement {
	if s.heading == nil {
		return gpui.StyleRefinement{}
	}
	return s.heading(level)
}

// CodeBlock is the style refinement for code blocks.
func (s TextViewStyle) CodeBlock() gpui.StyleRefinement { return s.codeBlock }

// Table is the style refinement for the table container.
func (s TextViewStyle) Table() gpui.StyleRefinement { return s.table }

// TableHead is the style refinement for table header rows.
func (s TextViewStyle) TableHead() gpui.StyleRefinement { return s.tableHead }

// TableCell is the style refinement for table cells.
func (s TextViewStyle) TableCell() gpui.StyleRefinement { return s.tableCell }

// InlineCode is the highlight style for inline code, before the
// code-background fallback (U:text/style.rs:256-260).
func (s TextViewStyle) InlineCode() gpui.HighlightStyle { return s.inlineCode }

// IsDark reports whether content-specific assets use their dark variant.
func (s TextViewStyle) IsDark() bool { return s.isDark }

// inlineCodeHighlight is the inline code highlight, falling back to the
// code background without a custom background (U:text/style.rs:267-275).
func (s TextViewStyle) inlineCodeHighlight() gpui.HighlightStyle {
	style := s.inlineCode
	if style.BackgroundColor == nil {
		bg := s.codeBackground
		style.BackgroundColor = &bg
	}
	return style
}

// Equal is TextViewStyle's PartialEq: every field, the heading callback by
// its refinement at levels 1-6 (U:text/style.rs:31-48).
func (s TextViewStyle) Equal(o TextViewStyle) bool {
	if s.paragraphGap != o.paragraphGap || s.foreground != o.foreground ||
		s.mutedForeground != o.mutedForeground || s.link != o.link ||
		s.selection != o.selection || s.codeBackground != o.codeBackground ||
		s.border != o.border || s.isDark != o.isDark || !s.inlineCode.Equal(o.inlineCode) {
		return false
	}
	for level := uint8(1); level <= 6; level++ {
		if !reflect.DeepEqual(s.Heading(level), o.Heading(level)) {
			return false
		}
	}
	return reflect.DeepEqual(s.codeBlock, o.codeBlock) && reflect.DeepEqual(s.table, o.table) &&
		reflect.DeepEqual(s.tableHead, o.tableHead) && reflect.DeepEqual(s.tableCell, o.tableCell)
}
