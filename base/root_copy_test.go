package base

import (
	"runtime"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Go-own tests of the Root's text-selection wiring (ticket 36): the
// TextSelectionLayer child (U:root.rs:293) and the copy binding with
// on_action_copy (U:root.rs:17-21, 264-273). The TestApp has no clipboard
// (App.WriteClipboard reports no host), so the copy tests observe the
// action's propagation: an app-level bubble listener runs only when
// on_action_copy propagates.

// openSelectionRoot opens a Root whose content focuses focus and
// registers selection over a 200x40 box.
func openSelectionRoot(t *testing.T, ta *gpui.TestApp, selection TextSelectionHandle, focus gpui.FocusHandle) *gpui.VisualTestContext {
	t.Helper()
	_, cx := openRoot(ta, func(w *gpui.Window, cx *gpui.Context[Root]) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(cx, w.Scope(), func(v *testView, _ *gpui.Context[testView]) {
			v.build = func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
				return gpui.Div().W(gpui.Px(200)).H(gpui.Px(40)).TrackFocus(focus).Child(registeringDiv(selection)).IntoElement()
			}
		}))
	})
	cx.Update(func(w *gpui.Window, _ *gpui.App) { w.Focus(focus) })
	drawText(t, cx)
	return cx
}

func rootCopyKeystroke() string {
	if runtime.GOOS == "darwin" {
		return "cmd-c"
	}
	return "ctrl-c"
}

// The Root mounts the window's TextSelectionLayer, so content that only
// registers a participant takes part in window selection
// (U:root.rs:293).
func TestRootMountsTextSelectionLayer(t *testing.T) {
	ta := textTestApp(t)
	selection := newHandle(ta, "root text")
	var focus gpui.FocusHandle
	ta.Update(func(app *gpui.App) { focus = app.FocusHandle() })
	cx := openSelectionRoot(t, ta, selection, focus)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		if _, ok := textSelectionStateExisting(w, app); !ok {
			t.Fatal("the Root mounted no TextSelectionLayer (no window selection state)")
		}
		selection.SetLocalSelection(true, app)
		if got := TextSelectionSelectedText(w, app); got != "root text" {
			t.Fatalf("selected %q, want %q", got, "root text")
		}
	})
}

// The copy keystroke dispatches input::Copy in the Root context
// (U:root.rs:17-21; without the binding nothing reaches the listener).
// With a selection on_action_copy handles it (the selection, trimmed,
// goes to the clipboard) and stops; with an empty or blank selection it
// propagates (U:root.rs:264-273).
func TestRootCopyActionPropagatesOnlyWithoutSelectedText(t *testing.T) {
	for _, tc := range []struct {
		name      string
		text      string
		selected  bool
		propagate bool
	}{
		{"no selection", "copied", false, true},
		{"selection", "copied", true, false},
		{"blank selection", "   ", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ta := textTestApp(t)
			selection := newHandle(ta, tc.text)
			var focus gpui.FocusHandle
			bubbled := 0
			ta.Update(func(app *gpui.App) {
				focus = app.FocusHandle()
				app.OnAction(CopyAction, func(_ any, phase gpui.DispatchPhase, _ *gpui.App) {
					if phase == gpui.DispatchBubble {
						bubbled++
					}
				})
			})
			cx := openSelectionRoot(t, ta, selection, focus)
			if tc.selected {
				cx.Update(func(_ *gpui.Window, app *gpui.App) { selection.SetLocalSelection(true, app) })
			}
			cx.SimulateKeystrokes(rootCopyKeystroke())
			if got := bubbled == 1; got != tc.propagate {
				t.Fatalf("copy reached the app listener %d times, want propagate=%v", bubbled, tc.propagate)
			}
		})
	}
}
