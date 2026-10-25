package base

import (
	"runtime"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Go-own tests of the Root's text-selection wiring (ticket 36): the
// TextSelectionLayer child (U:root.rs:293) and the copy keystroke
// (U:root.rs:17-21, 264-273). The TestApp has no clipboard
// (App.WriteClipboard reports no host), so the copy tests observe the
// dispatch: what the keystroke runs and whether it consumes the key.

// openSelectionRoot opens a Root whose content focuses focus and
// registers selection over a 200x40 box.
func openSelectionRoot(t *testing.T, ta *gpui.TestApp, selection TextSelectionHandle, focus gpui.FocusHandle) *gpui.VisualTestContext {
	t.Helper()
	_, cx := openRoot(ta, func(w *gpui.Window, cx *gpui.Context[Root]) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(cx, w.Scope(), func(v *testView, _ *gpui.Context[testView]) {
			v.build = func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
				return gpui.Div().Flex().W(gpui.Px(200)).H(gpui.Px(40)).TrackFocus(focus).Child(registeringDiv(selection)).IntoElement()
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
		if !textSelectionStateExists(w, app) {
			t.Fatal("the Root mounted no TextSelectionLayer (no window selection state)")
		}
		selection.SetLocalSelection(true, app)
		if got := TextSelectionSelectedText(w, app); got != "root text" {
			t.Fatalf("selected %q, want %q", got, "root text")
		}
	})
}

// On macOS the Root still binds cmd-c (browser-core 73: the core's key
// table carries only the !Mac rows), so the action path is unchanged
// there: with a selection on_action_copy handles it (the selection,
// trimmed, goes to the clipboard) and stops; with an empty or blank
// selection it propagates (U:root.rs:17-21, 264-273).
func TestRootCopyActionPropagatesOnlyWithoutSelectedText(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("the !mac copy keystroke runs the core command (TestRootCopyKeystrokeRunsTheCoreEditCommand)")
	}
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

// browser-core 73: off macOS the Root no longer binds the copy
// keystroke — the core's copy command (browser-core 26) runs it from
// the keydown's default-action arm on the window selection's stand-in
// handler (gpui/text_selection_copy.go, browser-core 19) instead, so
// the keystroke dispatches no action whether or not a selection
// exists (Chrome's ExecuteCopy fires the copy event with a collapsed
// caret too; only the write gates on it, clipboard_commands.cc:
// 321-371). The command's own dispatch through the default-action arm
// is pinned by gpui's TestEditActions* suite (gpui/
// edit_action_test.go); the write is hostless here (the TestApp has
// no clipboard). With the binding present this test fails — the
// action reaches the app listener.
func TestRootCopyKeystrokeRunsTheCoreEditCommand(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin keeps the Root's cmd-c binding (TestRootCopyActionPropagatesOnlyWithoutSelectedText)")
	}
	for _, tc := range []struct {
		name     string
		selected bool
	}{
		{"selection", true},
		{"no selection", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ta := textTestApp(t)
			selection := newHandle(ta, "copied")
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
			if bubbled != 0 {
				t.Fatalf("the copy keystroke dispatched the action %d times; want the core command (no action dispatch)", bubbled)
			}
		})
	}
}

// Without a window selection state (no selectable text ever
// registered), the core has no stand-in handler, so the copy
// keystroke still dispatches nothing and the key is left untouched
// (Chromium's CanEdit gate: a frame with no selection surface runs no
// editor command) — the plain propagation of a key nothing handles.
func TestRootCopyKeystrokeStaysUnboundWithoutSelectionState(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin keeps the Root's cmd-c binding")
	}
	ta := textTestApp(t)
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
	_, cx := openRoot(ta, func(w *gpui.Window, cx *gpui.Context[Root]) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(cx, w.Scope(), func(v *testView, _ *gpui.Context[testView]) {
			v.build = func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
				return gpui.Div().Flex().W(gpui.Px(200)).H(gpui.Px(40)).TrackFocus(focus).IntoElement()
			}
		}))
	})
	cx.Update(func(w *gpui.Window, _ *gpui.App) { w.Focus(focus) })
	drawText(t, cx)
	cx.SimulateKeystrokes(rootCopyKeystroke())
	if bubbled != 0 {
		t.Fatalf("the copy keystroke dispatched the action %d times; want nothing bound", bubbled)
	}
}
