package base

// Port of U:input/editor/display_map/folding.rs: the foldable line range
// a highlighter (or another source) supplies to the display map. The
// fold-range extraction the README names (extract_fold_ranges over a
// syntax tree) is not in the pinned folding.rs: candidates come from the
// InputHighlighter (FoldRanges / FoldRangesForEdit, editor.go) or
// ApplyHighlighterFoldCandidates.

// FoldRange is a foldable line range: the start and end lines stay
// visible, the lines between them hide when it is folded
// (U:input/editor/display_map/folding.rs:1-6).
type FoldRange struct {
	StartLine int
	EndLine   int
}

// NewFoldRange is FoldRange::new; it panics when startLine > endLine like
// the upstream assert (U:input/editor/display_map/folding.rs:8-19).
func NewFoldRange(startLine, endLine int) FoldRange {
	if startLine > endLine {
		panic("base: fold start_line must be <= end_line")
	}
	return FoldRange{StartLine: startLine, EndLine: endLine}
}
