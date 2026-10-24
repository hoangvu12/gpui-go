package base

// Port of U:selectable_text.rs: plain text that participates in the
// window-scoped TextSelection — moved into the core with the selection
// model (browser-core 19, gpui/selectable_text.go). This file keeps the
// Base constructors over the core element; the type is the core's, so
// its styling methods come with it.

import "github.com/hoangvu12/gpui-go/gpui"

// SelectableText is plain text that participates in the window-scoped
// TextSelection (U:selectable_text.rs:11-27): the core element.
type SelectableText = gpui.SelectableText

// NewSelectableText creates a run that owns its own selection
// (U:selectable_text.rs:30-33).
func NewSelectableText[I gpui.IntoElementID](id I, text string) *SelectableText {
	return gpui.NewSelectableText(id, text)
}

// SelectableTextWithHandle creates a run that joins the document handle
// belongs to (U:selectable_text.rs:35-42).
func SelectableTextWithHandle[I gpui.IntoElementID](id I, handle TextSelectionHandle, text string) *SelectableText {
	return gpui.SelectableTextWithHandle(id, handle, text)
}
