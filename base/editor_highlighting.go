package base

// Port of U:input/editor/highlighting.rs: the parser-independent
// highlighting seams the editor consumes. InputEditorStyle and
// DiagnosticColors are in input_state_style.go; SyntaxContext is in
// input_kind.go (the editing_syntax_context hook returns it).
//
// Go adaptations:
//
//   - InputHighlighter's default methods (update_batch,
//     fold_ranges_for_edit) are optional interfaces checked by type
//     assertion: InputHighlighterBatch and InputHighlighterEditFolds. A
//     highlighter without them gets the trait's default bodies.
//   - Option<Box<dyn InputHighlighter>> / Option<Rc<dyn ...>> are nil
//     interfaces; Option<HighlightStyle> is (style, ok).
//   - Ranges are gpui.TextRange, as in gpui.HighlightRange (the
//     (Range<usize>, HighlightStyle) pair).

import (
	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// HighlightStyleResolver resolves semantic highlight names into styles;
// Base knows nothing about a concrete syntax theme
// (U:input/editor/highlighting.rs:9-15).
type HighlightStyleResolver interface {
	Style(name string) (gpui.HighlightStyle, bool)
}

// SharedHighlightStyleResolver is the shared resolver type
// (U:input/editor/highlighting.rs:77).
type SharedHighlightStyleResolver = HighlightStyleResolver

// noHighlightStyles resolves nothing (U:input/editor/highlighting.rs:17-24).
type noHighlightStyles struct{}

func (noHighlightStyles) Style(string) (gpui.HighlightStyle, bool) {
	return gpui.HighlightStyle{}, false
}

// InputEditWithText is one (InputEdit, Rope) entry of a batch: an edit
// with the text as it stood right after it (U:input/editor/highlighting.rs:
// 48-54).
type InputEditWithText struct {
	Edit InputEdit
	Text rope.Rope
}

// InputHighlighter is the syntax highlighting seam: implementations own
// parsing and incremental state; Base asks for styled ranges and fold
// candidates (U:input/editor/highlighting.rs:26-74).
type InputHighlighter interface {
	Language() string
	// Update applies one edit (nil: a full refresh) to the highlighter.
	Update(edit *InputEdit, text rope.Rope, folding bool, w *gpui.Window, cx *gpui.Context[EditorState])
	// Styles returns ordered, non-overlapping runs that fully cover rng;
	// unstyled text uses the zero HighlightStyle.
	Styles(rng gpui.TextRange, resolver HighlightStyleResolver) []gpui.HighlightRange
	FoldRanges(text rope.Rope) []FoldRange
}

// InputHighlighterBatch is InputHighlighter::update_batch: several edits
// made as one change, in application order. Without it each edit is
// handed to Update in turn (U:input/editor/highlighting.rs:42-58).
type InputHighlighterBatch interface {
	UpdateBatch(edits []InputEditWithText, folding bool, w *gpui.Window, cx *gpui.Context[EditorState])
}

// InputHighlighterEditFolds is InputHighlighter::fold_ranges_for_edit.
// Without it the full FoldRanges is used (U:input/editor/highlighting.rs:
// 70-73).
type InputHighlighterEditFolds interface {
	FoldRangesForEdit(rng InputSelection, text rope.Rope) []FoldRange
}

// highlighterUpdateBatch dispatches update_batch or its default body.
func highlighterUpdateBatch(h InputHighlighter, edits []InputEditWithText, folding bool, w *gpui.Window, cx *gpui.Context[EditorState]) {
	if b, ok := h.(InputHighlighterBatch); ok {
		b.UpdateBatch(edits, folding, w, cx)
		return
	}
	for _, e := range edits {
		edit := e.Edit
		h.Update(&edit, e.Text, folding, w, cx)
	}
}

// highlighterFoldRangesForEdit dispatches fold_ranges_for_edit or its
// default body.
func highlighterFoldRangesForEdit(h InputHighlighter, rng InputSelection, text rope.Rope) []FoldRange {
	if f, ok := h.(InputHighlighterEditFolds); ok {
		return f.FoldRangesForEdit(rng, text)
	}
	return h.FoldRanges(text)
}

// InputHighlighterFactory creates a highlighter for a language; nil means
// none (U:input/editor/highlighting.rs:76).
type InputHighlighterFactory func(language string) InputHighlighter

// FoldIconRenderer renders the fold icon of the ix-th visible fold
// candidate (U:input/editor/highlighting.rs:78).
type FoldIconRenderer func(ix int, folded bool) gpui.AnyElement

// SyntaxContextProvider answers the syntax context for editing decisions
// (pairing, skip, indent); created per editor by the application's
// LanguageProvider (U:input/editor/highlighting.rs:91-97).
type SyntaxContextProvider interface {
	ContextAt(text rope.Rope, offset int) SyntaxContext
}

// highlighterCell is Rc<RefCell<Option<Box<dyn InputHighlighter>>>>: the
// highlighter shared by every copy of the layout mode.
type highlighterCell struct{ h InputHighlighter }
