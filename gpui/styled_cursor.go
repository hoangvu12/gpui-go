package gpui

// The pinned CursorStyle variants the Styled cursor methods name but the
// port's cursor enum (desktop.go, bounded to the Windows loader's
// distinct cases) does not yet declare (ticket base-05; reference
// crates/gpui/src/platform.rs CursorStyle, used by gpui_macros
// styles.rs:595 cursor_move, :609 cursor_context_menu, :637 cursor_copy,
// :651 cursor_grab, :658 cursor_grabbing). They continue the desktop.go
// numbering; the Windows loader's default arm maps them to the arrow
// cursor, matching its handling of every unlisted style. They belong in
// desktop.go's enum and should move there with its owner.
const (
	// CursorClosedHand is the closed (grabbing) hand.
	CursorClosedHand CursorStyle = CursorOperationNotAllowed + 1 + iota
	// CursorOpenHand is the open (grab) hand.
	CursorOpenHand
	// CursorDragCopy is the drag-copy cursor.
	CursorDragCopy
	// CursorContextualMenu is the contextual-menu cursor.
	CursorContextualMenu
)
