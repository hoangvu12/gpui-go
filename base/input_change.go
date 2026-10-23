package base

// Port of U:input/base/change.rs.

// change is one text replacement in the coordinates of the document as it
// stood immediately before the replacement was applied
// (U:input/base/change.rs:5-14).
//
// tokenDelta is Option<Box<inline_tokens::TokenDelta>>: nil is None
// (U:input/base/inline_tokens.rs:210-215).
type change struct {
	tokenDelta *tokenDelta
	oldRange   InputSelection
	oldText    string
	newRange   InputSelection
	newText    string
}

// newChange is Change::new (U:input/base/change.rs:17-30).
func newChange(oldRange InputSelection, oldText string, newRange InputSelection, newText string) change {
	return change{oldRange: oldRange, oldText: oldText, newRange: newRange, newText: newText}
}

// shifted is the same change as it reads after delta bytes were inserted
// (positive) or removed (negative) ahead of it; offsets saturate at 0
// (U:input/base/change.rs:32-43). tokenDelta is copied as-is.
func (c change) shifted(delta int) change {
	shift := func(offset int) int { return max(offset+delta, 0) }
	return change{
		tokenDelta: c.tokenDelta,
		oldRange:   NewInputSelection(shift(c.oldRange.Start), shift(c.oldRange.End)),
		oldText:    c.oldText,
		newRange:   NewInputSelection(shift(c.newRange.Start), shift(c.newRange.End)),
		newText:    c.newText,
	}
}
