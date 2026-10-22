package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// textTestApp is a test app with Base and the text bindings initialized
// (the tests' cx.update(crate::init)).
func textTestApp(t testing.TB) *gpui.TestApp {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	ta.Update(func(app *gpui.App) {
		Init(app)
	})
	return ta
}

// newTextViewStateEntity is cx.new(|cx| TextViewState::markdown(text, cx)).
func newTextViewStateEntity(ta *gpui.TestApp, text string) gpui.Entity[TextViewState] {
	var state gpui.Entity[TextViewState]
	ta.Update(func(app *gpui.App) {
		state = gpui.NewEntity(app, ta.RootScope(), func(s *TextViewState, cx *gpui.Context[TextViewState]) {
			*s = TextViewStateMarkdown(text, cx)
		})
	})
	return state
}

func drawText(t *testing.T, cx *gpui.VisualTestContext) {
	t.Helper()
	cx.RunUntilParked()
	if _, err := gpui.DrawWindowFrame(cx.Window()); err != nil {
		t.Fatal(err)
	}
	cx.RunUntilParked()
}

// Go-own smoke test: every block kind renders and paints.
func TestTextViewRendersEveryBlockKind(t *testing.T) {
	ta := textTestApp(t)
	source := "# Title\n\nSome **bold** and `code` text with a [link](https://example.com).\n\n" +
		"- one\n- [x] two\n\n1. first\n2. second\n\n> quote\n\n```rust\nfn main() {}\n```\n\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n---\n\n![img](https://example.com/a.png) after"
	state := newTextViewStateEntity(ta, source)
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(400)).Child(NewTextView(state)).IntoElement()
	})
	drawText(t, cx)
	drawText(t, cx)
}
