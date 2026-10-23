package base

// Ported tests of U:input/editor/lsp/semantic_tokens.rs (4 of 4).

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// semanticLegend is legend() (U:input/editor/lsp/semantic_tokens.rs:
// 201-206): SemanticTokenType::KEYWORD and COMMENT.
func semanticLegend() LspSemanticTokensLegend {
	return LspSemanticTokensLegend{TokenTypes: []string{"keyword", "comment"}}
}

// semanticTestTheme is TestTheme (U:input/editor/lsp/semantic_tokens.rs:
// 208-217).
type semanticTestTheme struct{}

func (semanticTestTheme) Style(name string) (gpui.HighlightStyle, bool) {
	if name != "keyword" && name != "comment" {
		return gpui.HighlightStyle{}, false
	}
	return gpui.HighlightStyleFromColor(gpui.Hsla{H: 0.5, S: 0.5, L: 0.5, A: 1}), true
}

// U:input/editor/lsp/semantic_tokens.rs:219-252
func TestDecodeSemanticTokensDelta(t *testing.T) {
	// "keyword" at (0, 0..4) and "comment" at (1, 2..7).
	tokens := LspSemanticTokens{Data: []LspSemanticToken{
		{DeltaLine: 0, DeltaStart: 0, Length: 4, TokenType: 0},
		{DeltaLine: 1, DeltaStart: 2, Length: 5, TokenType: 1},
	}}
	decoded := decodeSemanticTokens(tokens, semanticLegend())
	if len(decoded) != 2 {
		t.Fatalf("decoded %d tokens, want 2", len(decoded))
	}
	if decoded[0].rng != NewLspRange(NewInputPosition(0, 0), NewInputPosition(0, 4)) || decoded[0].name != "keyword" {
		t.Fatalf("decoded[0] = %+v", decoded[0])
	}
	// The second line is relative to the first; the character resets
	// because delta_line > 0.
	if decoded[1].rng != NewLspRange(NewInputPosition(1, 2), NewInputPosition(1, 7)) || decoded[1].name != "comment" {
		t.Fatalf("decoded[1] = %+v", decoded[1])
	}
}

// U:input/editor/lsp/semantic_tokens.rs:254-267
func TestDecodeSkipsOutOfLegendIndex(t *testing.T) {
	tokens := LspSemanticTokens{Data: []LspSemanticToken{{Length: 3, TokenType: 99}}}
	if got := decodeSemanticTokens(tokens, semanticLegend()); len(got) != 0 {
		t.Fatalf("decoded = %+v, want none", got)
	}
}

// U:input/editor/lsp/semantic_tokens.rs:269-305
func TestForRangeResolvesAndWindows(t *testing.T) {
	text := rope.FromStr("SELECT * FROM users\n-- a comment line\n")
	lsp := DefaultLsp()
	lsp.semanticTokens = []lspSemanticTokenRange{
		{NewLspRange(NewInputPosition(0, 0), NewInputPosition(0, 6)), "keyword"},
		{NewLspRange(NewInputPosition(1, 0), NewInputPosition(1, 17)), "comment"},
	}

	// The visible range covers only line 0 (bytes 0..19).
	styles := lsp.semanticTokensForRange(text, NewInputSelection(0, 19), semanticTestTheme{})
	if len(styles) != 1 {
		t.Fatalf("styles = %+v: only the line-0 token should be windowed in", styles)
	}
	if styles[0].Range != (gpui.TextRange{Start: 0, End: 6}) {
		t.Fatalf("keyword token maps to %v, want 0..6", styles[0].Range)
	}
	if styles[0].Style == (gpui.HighlightStyle{}) {
		t.Fatal("'keyword' should resolve to a non-default style")
	}
}

// U:input/editor/lsp/semantic_tokens.rs:307-340
func TestForRangeBinarySearchWindow(t *testing.T) {
	// 100 lines of "foo bar\n", one keyword token per line over "foo".
	text := rope.FromStr(strings.Repeat("foo bar\n", 100))
	lsp := DefaultLsp()
	for line := range uint32(100) {
		lsp.semanticTokens = append(lsp.semanticTokens, lspSemanticTokenRange{NewLspRange(NewInputPosition(line, 0), NewInputPosition(line, 3)), "keyword"})
	}

	// Only line 50 is visible; the window returns exactly its token.
	start := 50 * len("foo bar\n")
	styles := lsp.semanticTokensForRange(text, NewInputSelection(start, start+3), semanticTestTheme{})
	if len(styles) != 1 || styles[0].Range != (gpui.TextRange{Start: start, End: start + 3}) {
		t.Fatalf("styles = %+v", styles)
	}

	// An empty viewport before every token windows nothing in.
	if got := lsp.semanticTokensForRange(text, NewInputSelection(0, 0), semanticTestTheme{}); len(got) != 0 {
		t.Fatalf("empty viewport styles = %+v", got)
	}
}
