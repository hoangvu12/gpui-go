package gpui

import "testing"

// The core selection's own behaviour tests (browser-core ticket 19): the
// multi-click granularities against real pointer gestures, the drag that
// extends a double-click by whole words, and the copy command's fall to
// the window selection when nothing editable is focused. The browser
// fixtures (internal/parity/fixtures/selection) verify the same rules
// against headless Chrome.

// selectableTestView opens a window with a selection layer over one
// selectable text run, returning the run's participant handle and the
// layout the frame laid the text out with (for computing pointer
// positions).
func selectableTestView(t *testing.T, ta *TestApp, text string) (TextSelectionHandle, *TextLayout, *VisualTestContext) {
	t.Helper()
	var handle TextSelectionHandle
	ta.Update(func(app *App) { handle = NewTextSelectionHandle("", app, app.RootScope()) })
	var layout *TextLayout
	_, cx := openSelectionView(t, ta, func(*Window, *Context[selectionView]) AnyElement {
		run := SelectableTextWithHandle("text", handle, text)
		layout = run.styledText.Layout()
		return Div().SizeFull().Child(TextSelectionLayer{}).Child(
			Div().W(Px(600)).H(Px(80)).Child(run),
		).IntoElement()
	})
	drawSelectionFrame(t, cx)
	return handle, layout, cx
}

// TestDragSelectsTheCharactersBetweenThePressAndTheRelease pins the plain
// press-drag: character precision between the two carets.
func TestDragSelectsTheCharactersBetweenThePressAndTheRelease(t *testing.T) {
	ta := newSelectionTestApp(t)
	handle, layout, cx := selectableTestView(t, ta, "alpha beta gamma delta")
	_ = handle
	left := MouseButtonLeft
	cx.SimulateEvent(&MouseDownEvent{Position: positionFor(t, layout, 6), Button: left, ClickCount: 1})
	cx.SimulateMouseMove(positionFor(t, layout, 14), &left, Modifiers{})
	cx.SimulateEvent(&MouseUpEvent{Position: positionFor(t, layout, 14), Button: left, ClickCount: 1})
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "beta gam" {
			t.Fatalf("selected %q", got)
		}
	})
}

// TestDoubleClickSelectsTheWordWithPunctuationNextToIt pins the
// double-click's word granularity: the word under the pointer, punctuation
// excluded (the comma is its own segment).
func TestDoubleClickSelectsTheWordWithPunctuationNextToIt(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, layout, cx := selectableTestView(t, ta, "one café, three")
	left := MouseButtonLeft
	press := func(count int, position Point) {
		cx.SimulateEvent(&MouseDownEvent{Position: position, Button: left, ClickCount: count})
		cx.SimulateEvent(&MouseUpEvent{Position: position, Button: left, ClickCount: count})
	}
	press(1, positionFor(t, layout, 6))
	press(2, positionFor(t, layout, 6))
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "café" {
			t.Fatalf("selected %q", got)
		}
	})
}

// TestDoubleClickExtendsOverTheTrailingWhitespace pins the Windows rule:
// a double-clicked word takes the whitespace that follows it
// (AdjustSelectionWithTrailingWhitespace).
func TestDoubleClickExtendsOverTheTrailingWhitespace(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, layout, cx := selectableTestView(t, ta, "alpha beta gamma delta")
	left := MouseButtonLeft
	press := func(count int, position Point) {
		cx.SimulateEvent(&MouseDownEvent{Position: position, Button: left, ClickCount: count})
		cx.SimulateEvent(&MouseUpEvent{Position: position, Button: left, ClickCount: count})
	}
	press(1, positionFor(t, layout, 6))
	press(2, positionFor(t, layout, 6))
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "beta " {
			t.Fatalf("selected %q", got)
		}
	})
}

// TestTripleClickSelectsTheLineAndItsBreak pins the paragraph granularity:
// the logical line, plus its line break when another line follows.
func TestTripleClickSelectsTheLineAndItsBreak(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, layout, cx := selectableTestView(t, ta, "first line\nsecond line\nthird")
	left := MouseButtonLeft
	press := func(count int, position Point) {
		cx.SimulateEvent(&MouseDownEvent{Position: position, Button: left, ClickCount: count})
		cx.SimulateEvent(&MouseUpEvent{Position: position, Button: left, ClickCount: count})
	}
	click := positionFor(t, layout, 15)
	press(1, click)
	press(2, click)
	press(3, click)
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "second line\n" {
			t.Fatalf("selected %q", got)
		}
	})
}

// TestDoubleClickThenDragExtendsByWholeWords pins the granularity drag
// (Chromium's HandleMouseDragged extending by Selection().Granularity()):
// after a double-click, a still-held drag snaps its endpoints to word
// boundaries — forward keeps the anchor word's start and takes the word
// end under the pointer.
func TestDoubleClickThenDragExtendsByWholeWords(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, layout, cx := selectableTestView(t, ta, "alpha beta gamma delta")
	left := MouseButtonLeft
	cx.SimulateEvent(&MouseDownEvent{Position: positionFor(t, layout, 6), Button: left, ClickCount: 1})
	cx.SimulateEvent(&MouseUpEvent{Position: positionFor(t, layout, 6), Button: left, ClickCount: 1})
	cx.SimulateEvent(&MouseDownEvent{Position: positionFor(t, layout, 6), Button: left, ClickCount: 2})
	cx.SimulateMouseMove(positionFor(t, layout, 14), &left, Modifiers{})
	cx.SimulateEvent(&MouseUpEvent{Position: positionFor(t, layout, 14), Button: left, ClickCount: 2})
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "beta gamma" {
			t.Fatalf("selected %q", got)
		}
	})
}

// TestDoubleClickThenDragBackwardKeepsTheAnchorWordWhole pins the
// backward granularity drag: the anchor becomes the word start under the
// pointer and the cursor the double-clicked word's end — the
// double-clicked word stays whole
// (ExtendSelectionAsDirectional's SetBaseAndExtent(new_end, new_start)).
func TestDoubleClickThenDragBackwardKeepsTheAnchorWordWhole(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, layout, cx := selectableTestView(t, ta, "alpha beta gamma delta")
	left := MouseButtonLeft
	cx.SimulateEvent(&MouseDownEvent{Position: positionFor(t, layout, 13), Button: left, ClickCount: 1})
	cx.SimulateEvent(&MouseUpEvent{Position: positionFor(t, layout, 13), Button: left, ClickCount: 1})
	cx.SimulateEvent(&MouseDownEvent{Position: positionFor(t, layout, 13), Button: left, ClickCount: 2})
	cx.SimulateMouseMove(positionFor(t, layout, 1), &left, Modifiers{})
	cx.SimulateEvent(&MouseUpEvent{Position: positionFor(t, layout, 1), Button: left, ClickCount: 2})
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "alpha beta gamma" {
			t.Fatalf("selected %q", got)
		}
	})
}

// TestTripleClickThenDragExtendsByWholeLines pins the paragraph-granularity
// drag after a triple click.
func TestTripleClickThenDragExtendsByWholeLines(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, layout, cx := selectableTestView(t, ta, "first line\nsecond line\nthird")
	left := MouseButtonLeft
	click := positionFor(t, layout, 15)
	cx.SimulateEvent(&MouseDownEvent{Position: click, Button: left, ClickCount: 1})
	cx.SimulateEvent(&MouseUpEvent{Position: click, Button: left, ClickCount: 1})
	cx.SimulateEvent(&MouseDownEvent{Position: click, Button: left, ClickCount: 2})
	cx.SimulateEvent(&MouseUpEvent{Position: click, Button: left, ClickCount: 2})
	cx.SimulateEvent(&MouseDownEvent{Position: click, Button: left, ClickCount: 3})
	cx.SimulateMouseMove(positionFor(t, layout, 23), &left, Modifiers{})
	cx.SimulateEvent(&MouseUpEvent{Position: positionFor(t, layout, 23), Button: left, ClickCount: 3})
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "second line\nthird" {
			t.Fatalf("selected %q", got)
		}
	})
}

// TestCopyCommandFallsToTheWindowSelection pins the core action wiring
// (browser-core 19): with no focused editable, the copy command runs on
// the window's selection — the handler the model stands in for — and the
// selected text is what the clipboard write receives (observed through the
// participant's CopyWith projection, the write's source).
func TestCopyCommandFallsToTheWindowSelection(t *testing.T) {
	ta := newSelectionTestApp(t)
	app := ta.App()
	handle, layout, cx := selectableTestView(t, ta, "alpha beta gamma delta")
	copied := ""
	handle.CopyWith(func(app *App) string {
		copied = "the projection ran"
		return "the selected text"
	}, app)
	left := MouseButtonLeft
	cx.SimulateEvent(&MouseDownEvent{Position: positionFor(t, layout, 6), Button: left, ClickCount: 1})
	cx.SimulateMouseMove(positionFor(t, layout, 16), &left, Modifiers{})
	cx.SimulateEvent(&MouseUpEvent{Position: positionFor(t, layout, 16), Button: left, ClickCount: 1})
	drawSelectionFrame(t, cx)

	key := MustParseKeystroke("ctrl-insert")
	// The browser dispatch order (the default-action position's
	// consumption rule — the GPUI-order windows leave propagation to the
	// bindings): the key is consumed exactly when the command ran.
	cx.Window().SetKeyDispatchOrder(ListenersBeforeBindings)
	if propagate := cx.Window().DispatchKeyEvent(&KeyDownEvent{Keystroke: key}, app); propagate {
		t.Fatal("the copy keydown propagated: the editor command did not consume it")
	}
	if copied != "the projection ran" {
		t.Fatalf("the window selection's copy projection never ran (copied %q)", copied)
	}
	// The write itself: the hostless application has no clipboard (the
	// fault is silent, like the reference's log-only handling); the
	// internal/clipspec round-trip pins the real write.
}
