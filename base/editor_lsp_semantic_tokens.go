package base

// Port of U:input/editor/lsp/semantic_tokens.rs: the range semantic
// tokens provider, its debounced whole-document fetch, the delta decoding
// and the visible-range styles resolved through the highlight theme.

import (
	"slices"
	"sort"
	"time"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// semanticTokensDebounce is the refresh debounce
// (U:input/editor/lsp/semantic_tokens.rs:127-131).
const semanticTokensDebounce = 100 * time.Millisecond

// DocumentRangeSemanticTokensProvider supplies semantic highlighting
// tokens layered over the syntax highlighter: the editor side of
// textDocument/semanticTokens/range (U:input/editor/lsp/
// semantic_tokens.rs:11-55). Each token's type name (Legend().TokenTypes)
// is resolved against the active highlight theme at paint time, so theme
// switches recolor tokens without a refetch; modifiers are not mapped.
type DocumentRangeSemanticTokensProvider interface {
	// Legend names the numeric token_type of the returned tokens.
	Legend() LspSemanticTokensLegend
	// SemanticTokens fetches the tokens of the byte range.
	SemanticTokens(text rope.Rope, rng InputSelection, w *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[LspSemanticTokens]]
}

// lspSemanticTokenRange is one cached (lsp_types::Range, SharedString)
// pair (U:input/editor/lsp/mod.rs:62-65).
type lspSemanticTokenRange struct {
	rng  LspRange
	name string
}

// semanticTokensForRange is the styles of the cached tokens touching the
// visible byte range, resolved through theme; tokens resolving to an
// empty byte range or an unknown name are skipped. The cache is sorted by
// start, so the candidates are binary-searched
// (U:input/editor/lsp/semantic_tokens.rs:57-106).
func (l *Lsp) semanticTokensForRange(text rope.Rope, visibleRange InputSelection, theme HighlightStyleResolver) []gpui.HighlightRange {
	if len(l.semanticTokens) == 0 {
		return nil
	}
	ext := RopeExtOf(&text)
	visibleStart := ext.OffsetToPosition(visibleRange.Start)
	visibleEnd := ext.OffsetToPosition(visibleRange.End)

	// A token can touch the viewport only if it starts before visibleEnd
	// and not on a line above the first visible one (tokens are
	// single-line).
	hi := sort.Search(len(l.semanticTokens), func(i int) bool {
		return comparePosition(l.semanticTokens[i].rng.Start, visibleEnd) >= 0
	})
	lo := sort.Search(len(l.semanticTokens), func(i int) bool {
		return l.semanticTokens[i].rng.Start.Line >= visibleStart.Line
	})

	var out []gpui.HighlightRange
	for i := lo; i < hi; i++ {
		token := l.semanticTokens[i]
		start := ext.PositionToOffset(token.rng.Start)
		end := ext.PositionToOffset(token.rng.End)
		if start >= end || start >= visibleRange.End || end <= visibleRange.Start {
			continue
		}
		style, ok := theme.Style(token.name)
		if !ok {
			continue
		}
		out = append(out, gpui.HighlightRange{Range: gpui.TextRange{Start: start, End: end}, Style: style})
	}
	return out
}

// updateSemanticTokens refetches the whole document's tokens after a
// 100ms debounce; the cache is windowed at paint time, so a scroll never
// refetches (U:input/editor/lsp/semantic_tokens.rs:108-150).
func (l *Lsp) updateSemanticTokens(text rope.Rope, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
	provider := l.SemanticTokensProvider
	if provider == nil {
		return
	}
	legend := provider.Legend()
	rng := NewInputSelection(0, text.Len())
	inputState := cx.Entity().Downgrade()
	l.semanticTokensTask.set(cx.SpawnIn(w, func(_ gpui.WeakEntity[EditorState], acx *gpui.AsyncWindowContext) struct{} {
		if !lspSleep(acx.AsyncApp, semanticTokensDebounce) {
			return struct{}{}
		}
		var task gpui.Task[ProviderResult[LspSemanticTokens]]
		if acx.Update(func(w *gpui.Window, app *gpui.App) { task = provider.SemanticTokens(text, rng, w, app) }) != nil {
			return struct{}{}
		}
		tokens, ok := awaitProvider(acx.AsyncApp, task)
		if !ok {
			return struct{}{}
		}
		decoded := decodeSemanticTokens(tokens, legend)
		acx.AsyncApp.Update(func(app *gpui.App) {
			_ = inputState.TryUpdate(app, func(s *EditorState, cx *gpui.Context[InputBaseState[EditorMode]]) {
				lsp := &s.editorExtras().lsp
				if !slices.Equal(decoded, lsp.semanticTokens) {
					lsp.semanticTokens = decoded
					cx.Notify()
				}
			})
		})
		return struct{}{}
	}))
}

// decodeSemanticTokens decodes the LSP delta encoding into absolute
// (position range, type name) pairs sorted by start; a token type outside
// the legend is skipped (U:input/editor/lsp/semantic_tokens.rs:152-193).
func decodeSemanticTokens(tokens LspSemanticTokens, legend LspSemanticTokensLegend) []lspSemanticTokenRange {
	out := make([]lspSemanticTokenRange, 0, len(tokens.Data))
	var line, character uint32
	for _, token := range tokens.Data {
		if token.DeltaLine > 0 {
			line += token.DeltaLine
			character = token.DeltaStart
		} else {
			character += token.DeltaStart
		}
		if int(token.TokenType) >= len(legend.TokenTypes) {
			continue
		}
		start := NewInputPosition(line, character)
		end := NewInputPosition(line, character+token.Length)
		out = append(out, lspSemanticTokenRange{rng: LspRange{Start: start, End: end}, name: legend.TokenTypes[token.TokenType]})
	}
	slices.SortStableFunc(out, func(a, b lspSemanticTokenRange) int { return comparePosition(a.rng.Start, b.rng.Start) })
	return out
}
