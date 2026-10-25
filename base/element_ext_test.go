package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// ElementExt.TextSelectionScope (U:element_ext.rs:7-13) in the form
// U:text_selection.rs:2913-2934 FirstFrameScopedSelectionView uses it
// (`div().size_full().on_prepaint(..).text_selection_scope(scope)`): the
// participant registered inside the scoped subtree registers in that
// scope on the first frame (U:text_selection.rs:4069-4092).
func TestElementExtTextSelectionScopeScopesTheSubtree(t *testing.T) {
	ta := textTestApp(t)
	selection := newHandle(ta, "scoped")
	scope := TextSelectionScopeID(23)
	_, cx := openSelectionView(t, ta, func(w *gpui.Window, cx *gpui.Context[testView]) gpui.AnyElement {
		TextSelectionActivateScope(scope, w, cx.App())
		return gpui.Div().Flex().Child(TextSelectionLayer{}).Child(ElementExtOf(registeringDiv(selection)).TextSelectionScope(scope)).IntoElement()
	})
	drawText(t, cx)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		if !textSelectionStateExists(w, app) {
			t.Fatal("no state")
		}
		got, ok := gpui.TextSelectionParticipantScope(w, app, selection.EntityID())
		if !ok {
			t.Fatal("the participant did not register on the first frame")
		}
		if got != scope {
			t.Errorf("registration scope %v, want %v", got, scope)
		}
	})
}

// A value that is not an element panics (the `Self: IntoElement` bound,
// U:element_ext.rs:8-10, checked at run time).
func TestElementExtTextSelectionScopeRejectsNonElements(t *testing.T) {
	ext := ElementExt[int]{element: 1}
	defer func() {
		if recover() == nil {
			t.Fatal("no panic for a non-element")
		}
	}()
	ext.TextSelectionScope(1)
}
