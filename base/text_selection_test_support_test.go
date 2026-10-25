package base

// The Root and ElementExt selection tests' shared helpers — the ones the
// moved model's test file carried before the selection model moved into
// the core (browser-core 19; the model's own tests moved with it to
// gpui/text_selection_test.go). registeringDiv stays Base's: it registers
// through ElementExt's prepaint hook.

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"testing"
)

// newHandle creates a selection participant owned by the test app's root
// scope.
func newHandle(ta *gpui.TestApp, text string) TextSelectionHandle {
	var h TextSelectionHandle
	ta.Update(func(app *gpui.App) { h = NewTextSelectionHandle(text, app, app.RootScope()) })
	return h
}

// registeringDiv is a full-size div registering selection on prepaint with
// its bounds as text bounds (the on_prepaint registrations of
// U:text_selection.rs:2858-2934).
func registeringDiv(selection TextSelectionHandle) *gpui.DivElement {
	return ElementExtOf(gpui.Div().Flex().SizeFull()).OnPrepaint(func(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
		hitbox := w.InsertHitbox(bounds, gpui.HitboxNormal)
		selection.Register(NewTextSelectionRegistration(hitbox, bounds).WithTextBounds([]gpui.Bounds{bounds}), w, app)
	})
}

// textSelectionStateExists reports whether the window runs the selection
// layer's state (the core's query, spelled like the model's internal one
// the tests used before the move).
func textSelectionStateExists(w *gpui.Window, app *gpui.App) bool {
	return gpui.TextSelectionStateExists(w, app)
}

// openSelectionView opens a test window rendering build and draws it.
func openSelectionView(t *testing.T, ta *gpui.TestApp, build func(w *gpui.Window, cx *gpui.Context[testView]) gpui.AnyElement) (gpui.Entity[testView], *gpui.VisualTestContext) {
	t.Helper()
	view, cx := openTestView(t, ta, build)
	drawText(t, cx)
	return view, cx
}
