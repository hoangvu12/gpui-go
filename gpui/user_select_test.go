package gpui

import "testing"

// The user-select tests (browser-core ticket 20, spec D7): the style's
// inheritance and its ancestor-chain resolution (Chromium's
// Node::CanStartSelection walk, node.cc:2017-2041), plain text
// selecting by drag and multi-click with no opt-in, the none subtree
// that selects nothing, and the automatic I-beam (ShouldShowIBeamForNode,
// event_handler.cc:563-573 — SelectAutoCursor at :839-847). The
// browser fixtures (internal/parity/fixtures/userselect) verify the
// same rules against headless Chrome.

// userSelectTestView opens a window over build and draws one frame.
func userSelectTestView(t *testing.T, ta *TestApp, build func(w *Window, cx *Context[selectionView]) AnyElement) *VisualTestContext {
	t.Helper()
	_, cx := openSelectionView(t, ta, build)
	drawSelectionFrame(t, cx)
	return cx
}

// userSelectRun returns the text run the frame registered — its
// committed bounds and line height — failing when nothing registered:
// the bounds double as the check that the registration happened, and
// the tests aim their pointers at the run's first line (the projection
// band test is per-row, text_selection_band.go).
func userSelectRun(t *testing.T, cx *VisualTestContext) (Bounds, float32) {
	t.Helper()
	var bounds Bounds
	var lineHeight float32
	registered := false
	cx.Update(func(w *Window, app *App) {
		state, ok := textSelectionStateExisting(w, app)
		if !ok {
			return
		}
		_, _ = state.TryRead(app, func(s *windowSelectionState, _ *App) struct{} {
			for _, registration := range s.participants {
				b := registration.registration.bounds
				if !registered || b.Top() < bounds.Top() {
					bounds, registered = b, true
				}
				for _, run := range registration.registration.runs {
					if run.layout != nil {
						lineHeight = run.layout.LineHeight()
					}
				}
			}
			return struct{}{}
		})
	})
	if !registered {
		t.Fatal("the frame registered no selectable text run")
	}
	if lineHeight <= 0 {
		t.Fatalf("the registered run reports no line height (%v)", lineHeight)
	}
	return bounds, lineHeight
}

// divTextBounds returns the debug-selector bounds of the div carrying
// the text — the box the none-subtree tests aim at (the text's first
// line starts at the div's content origin: no padding, one line).
func divTextBounds(t *testing.T, cx *VisualTestContext) Bounds {
	t.Helper()
	bounds, ok := cx.DebugBounds("text")
	if !ok {
		t.Fatal("no debug bounds for the text's div")
	}
	return bounds
}

// userSelectTestAdvance is the deterministic text system's per-glyph
// advance for ASCII at the default font size (testwindow.go's
// em_width = font_size * 600 / 1000 with font_size 16).
const userSelectTestAdvance = float32(16) * 600 / 1000

// caretPoint returns the window position of the text caret at index,
// aimed mid-line (the deterministic system: one visual line, ASCII
// advances of em width 9.6 at font size 16).
func caretPoint(t *testing.T, bounds Bounds, lineHeight float32, index int) Point {
	t.Helper()
	return Point{X: bounds.Origin.X + float32(index)*userSelectTestAdvance, Y: bounds.Origin.Y + lineHeight/2}
}

// divCaretPoint aims at the first line of a div's text (the line
// occupies the div's content box top; the default style's line height
// is 26px at font size 16 — golden ratio 1.618, pixel-snapped).
func divCaretPoint(t *testing.T, bounds Bounds, index int) Point {
	t.Helper()
	return Point{X: bounds.Origin.X + float32(index)*userSelectTestAdvance, Y: bounds.Origin.Y + 13}
}

// dragSelect simulates the press-drag-release between two points.
func dragSelect(cx *VisualTestContext, from, to Point) {
	left := MouseButtonLeft
	cx.SimulateEvent(&MouseDownEvent{Position: from, Button: left, ClickCount: 1})
	cx.SimulateMouseMove(to, &left, Modifiers{})
	cx.SimulateEvent(&MouseUpEvent{Position: to, Button: left, ClickCount: 1})
}

// TestPlainTextStyleSelectsByDrag pins the default: text in an ordinary
// div — no TextSelectionLayer, no SelectableText — is selectable by
// drag (D7; Chrome's default user-select: auto).
func TestPlainTextStyleSelectsByDrag(t *testing.T) {
	ta := newSelectionTestApp(t)
	text := "alpha beta gamma delta"
	cx := userSelectTestView(t, ta, func(w *Window, cx *Context[selectionView]) AnyElement {
		return Div().SizeFull().Child(
			Div().ID("text").DebugSelector("text").W(Px(600)).Child(Text(text)),
		).IntoElement()
	})
	bounds, lineHeight := userSelectRun(t, cx)
	dragSelect(cx, caretPoint(t, bounds, lineHeight, 6), caretPoint(t, bounds, lineHeight, 14))
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "beta gam" {
			t.Fatalf("selected %q, want %q", got, "beta gam")
		}
	})
}

// TestDoubleClickSelectsTheWordOverPlainText pins the double-click's
// word granularity over plain text (the multi-click the model's
// gestures carry, selection_controller.cc's word expansion).
func TestDoubleClickSelectsTheWordOverPlainText(t *testing.T) {
	ta := newSelectionTestApp(t)
	cx := userSelectTestView(t, ta, func(w *Window, cx *Context[selectionView]) AnyElement {
		return Div().SizeFull().Child(
			Div().ID("text").DebugSelector("text").W(Px(600)).Child(Text("one latte, three")),
		).IntoElement()
	})
	bounds, lineHeight := userSelectRun(t, cx)
	point := caretPoint(t, bounds, lineHeight, 5)
	left := MouseButtonLeft
	cx.SimulateEvent(&MouseDownEvent{Position: point, Button: left, ClickCount: 1})
	cx.SimulateEvent(&MouseUpEvent{Position: point, Button: left, ClickCount: 1})
	cx.SimulateEvent(&MouseDownEvent{Position: point, Button: left, ClickCount: 2})
	cx.SimulateEvent(&MouseUpEvent{Position: point, Button: left, ClickCount: 2})
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "latte" {
			t.Fatalf("selected %q, want %q", got, "latte")
		}
	})
}

// TestTripleClickSelectsTheLineOverPlainText pins the triple-click's
// paragraph granularity over plain text.
func TestTripleClickSelectsTheLineOverPlainText(t *testing.T) {
	ta := newSelectionTestApp(t)
	cx := userSelectTestView(t, ta, func(w *Window, cx *Context[selectionView]) AnyElement {
		return Div().SizeFull().Child(
			Div().ID("text").DebugSelector("text").W(Px(600)).Child(Text("one line only")),
		).IntoElement()
	})
	bounds, lineHeight := userSelectRun(t, cx)
	point := caretPoint(t, bounds, lineHeight, 4)
	left := MouseButtonLeft
	for count := 1; count <= 3; count++ {
		cx.SimulateEvent(&MouseDownEvent{Position: point, Button: left, ClickCount: count})
		cx.SimulateEvent(&MouseUpEvent{Position: point, Button: left, ClickCount: count})
	}
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "one line only" {
			t.Fatalf("selected %q, want %q", got, "one line only")
		}
	})
}

// TestUserSelectNoneSubtreeDoesNotSelect pins the opt-out: a drag that
// starts and ends inside a user-select: none subtree selects nothing
// (Chromium's CanMouseDownStartSelect, selection_controller.cc:122-127
// — a mousedown on non-selectable content starts no selection).
func TestUserSelectNoneSubtreeDoesNotSelect(t *testing.T) {
	ta := newSelectionTestApp(t)
	cx := userSelectTestView(t, ta, func(w *Window, cx *Context[selectionView]) AnyElement {
		return Div().SizeFull().Child(
			Div().ID("text").DebugSelector("text").W(Px(600)).SelectNone().Child(Text("alpha beta gamma delta")),
		).IntoElement()
	})
	bounds := divTextBounds(t, cx)
	dragSelect(cx, divCaretPoint(t, bounds, 6), divCaretPoint(t, bounds, 14))
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "" {
			t.Fatalf("selected %q under user-select: none, want nothing", got)
		}
		if TextSelectionHasSelection(w, app) {
			t.Fatal("the window reports a selection under user-select: none")
		}
	})
}

// TestUserSelectNoneDragFromOutsideSelectsNothing pins the gesture
// crossing INTO a none subtree: a press on selectable text outside the
// subtree, dragged across it, selects nothing from the none text (its
// runs never register).
func TestUserSelectNoneDragFromOutsideSelectsNothing(t *testing.T) {
	ta := newSelectionTestApp(t)
	cx := userSelectTestView(t, ta, func(w *Window, cx *Context[selectionView]) AnyElement {
		return Div().SizeFull().Child(
			Div().ID("selectable").DebugSelector("text").W(Px(600)).Child(Text("alpha beta")),
		).Child(
			Div().ID("none").W(Px(600)).SelectNone().Child(Text("gamma delta")),
		).IntoElement()
	})
	bounds, lineHeight := userSelectRun(t, cx)
	// The press lands at the start of "beta" (index 6 of the first
	// text) and the release mid-"delta" (index 13 of the second, one
	// line lower): the diagonal band takes "beta" to the end of the
	// first line — and nothing from the none text on the second.
	release := Point{X: bounds.Origin.X + 13*userSelectTestAdvance, Y: bounds.Origin.Y + lineHeight + lineHeight/2}
	dragSelect(cx, caretPoint(t, bounds, lineHeight, 6), release)
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "beta" {
			t.Fatalf("selected %q, want %q (the selectable run only; the none run contributes nothing)", got, "beta")
		}
	})
}

// TestUserSelectTextReenablesUnderNone pins text's override: a
// user-select: text descendant of a none subtree is selectable
// (Chromium's plain inheritance; event_handler_test.cc:557
// UserSelectTextCanOverrideUserSelectNone).
func TestUserSelectTextReenablesUnderNone(t *testing.T) {
	ta := newSelectionTestApp(t)
	cx := userSelectTestView(t, ta, func(w *Window, cx *Context[selectionView]) AnyElement {
		return Div().SizeFull().SelectNone().Child(
			Div().ID("text").DebugSelector("text").W(Px(600)).SelectText().Child(Text("alpha beta gamma delta")),
		).IntoElement()
	})
	bounds, lineHeight := userSelectRun(t, cx)
	dragSelect(cx, caretPoint(t, bounds, lineHeight, 6), caretPoint(t, bounds, lineHeight, 14))
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "beta gam" {
			t.Fatalf("selected %q, want %q", got, "beta gam")
		}
	})
}

// TestUserSelectAutoUnderNoneStaysNone pins auto's resolution: an
// explicit auto deep under a none subtree does not re-enable selection
// — the value resolves from the ancestor chain (node.cc:2017-2041's
// parent walk), not from the element.
func TestUserSelectAutoUnderNoneStaysNone(t *testing.T) {
	ta := newSelectionTestApp(t)
	cx := userSelectTestView(t, ta, func(w *Window, cx *Context[selectionView]) AnyElement {
		return Div().SizeFull().SelectNone().Child(
			Div().ID("text").DebugSelector("text").W(Px(600)).SelectAuto().Child(Text("alpha beta gamma delta")),
		).IntoElement()
	})
	bounds := divTextBounds(t, cx)
	dragSelect(cx, divCaretPoint(t, bounds, 6), divCaretPoint(t, bounds, 14))
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "" {
			t.Fatalf("selected %q under auto-in-none, want nothing", got)
		}
	})
}

// TestUserSelectAllowsSelectionWalk pins the ancestor-chain resolution
// directly, inside a frame (the walk reads the frame's text style
// stack): the default is selectable, none wins by inheritance, text
// re-enables, and auto consults the parent.
func TestUserSelectAllowsSelectionWalk(t *testing.T) {
	ta := newSelectionTestApp(t)
	none := TextStyleRefinement{UserSelect: Some(UserSelectNone)}
	text := TextStyleRefinement{UserSelect: Some(UserSelectText)}
	auto := TextStyleRefinement{UserSelect: Some(UserSelectAuto)}
	type probe struct {
		name  string
		value bool
	}
	var probes []probe
	_, cx := openSelectionView(t, ta, func(w *Window, cx *Context[selectionView]) AnyElement {
		check := func(name string) {
			probes = append(probes, probe{name, userSelectAllowsSelection(w)})
		}
		check("default")
		WithTextStyleVoid(w, &none, func(w *Window) {
			check("none")
			WithTextStyleVoid(w, &text, func(w *Window) { check("text in none") })
			WithTextStyleVoid(w, &auto, func(w *Window) {
				check("auto in none")
				WithTextStyleVoid(w, &TextStyleRefinement{}, func(w *Window) { check("absent in auto-in-none") })
			})
		})
		WithTextStyleVoid(w, &auto, func(w *Window) { check("auto at the top") })
		return Div().SizeFull().IntoElement()
	})
	drawSelectionFrame(t, cx)
	want := map[string]bool{
		"default":                true,
		"none":                   false,
		"text in none":           true,
		"auto in none":           false,
		"absent in auto-in-none": false,
		"auto at the top":        true,
	}
	for _, p := range probes {
		if p.value != want[p.name] {
			t.Fatalf("%s: allows = %v, want %v", p.name, p.value, want[p.name])
		}
	}
}

// TestCursorOverSelectableTextIsIBeam pins the automatic I-beam: over
// selectable text with no explicit cursor, the window resolves the
// I-beam (SelectAutoCursor, event_handler.cc:839-847).
func TestCursorOverSelectableTextIsIBeam(t *testing.T) {
	ta := newSelectionTestApp(t)
	cx := userSelectTestView(t, ta, func(w *Window, cx *Context[selectionView]) AnyElement {
		return Div().SizeFull().Child(
			Div().ID("text").DebugSelector("text").W(Px(600)).Child(Text("alpha beta gamma")),
		).IntoElement()
	})
	bounds, lineHeight := userSelectRun(t, cx)
	cx.SimulateMouseMove(caretPoint(t, bounds, lineHeight, 4), nil, Modifiers{})
	cx.RunUntilParked()
	if style, ok := WindowCursorStyle(cx.Window()); !ok || style != CursorIBeam {
		t.Fatalf("cursor over selectable text = %v (ok %v), want the I-beam", style, ok)
	}
}

// TestCursorOverUserSelectNoneIsArrow pins the none case: over a
// select-none box the cursor stays the arrow (HitOnUserSelectNoneDoes-
// NotShowIBeam, event_handler_test.cc:489).
func TestCursorOverUserSelectNoneIsArrow(t *testing.T) {
	ta := newSelectionTestApp(t)
	cx := userSelectTestView(t, ta, func(w *Window, cx *Context[selectionView]) AnyElement {
		return Div().SizeFull().Child(
			Div().ID("text").DebugSelector("text").W(Px(600)).SelectNone().Child(Text("alpha beta gamma")),
		).IntoElement()
	})
	cx.SimulateMouseMove(divCaretPoint(t, divTextBounds(t, cx), 4), nil, Modifiers{})
	cx.RunUntilParked()
	if style, ok := WindowCursorStyle(cx.Window()); !ok || style != CursorArrow {
		t.Fatalf("cursor over select-none text = %v (ok %v), want the arrow", style, ok)
	}
}

// TestExplicitCursorWinsOverTheAutomaticIBeam pins the precedence: an
// element's explicit cursor (CSS cursor: pointer) beats the automatic
// I-beam of the text under it (SelectCursor's non-auto arms,
// event_handler.cc:764's kAuto being the only path to
// SelectAutoCursor).
func TestExplicitCursorWinsOverTheAutomaticIBeam(t *testing.T) {
	ta := newSelectionTestApp(t)
	cx := userSelectTestView(t, ta, func(w *Window, cx *Context[selectionView]) AnyElement {
		return Div().SizeFull().Child(
			Div().ID("text").DebugSelector("text").W(Px(600)).CursorPointer().Child(Text("alpha beta gamma")),
		).IntoElement()
	})
	bounds, lineHeight := userSelectRun(t, cx)
	cx.SimulateMouseMove(caretPoint(t, bounds, lineHeight, 4), nil, Modifiers{})
	cx.RunUntilParked()
	if style, ok := WindowCursorStyle(cx.Window()); !ok || style != CursorPointingHand {
		t.Fatalf("cursor over text under cursor:pointer = %v (ok %v), want the hand", style, ok)
	}
}

// TestUserSelectClassesApply pins the Tailwind classes: select-none,
// select-text and select-auto apply their method's refinement, and
// CheckClasses accepts them.
func TestUserSelectClassesApply(t *testing.T) {
	if err := CheckClasses("select-none select-text select-auto"); err != nil {
		t.Fatalf("the user-select classes must apply: %v", err)
	}
	if err := CheckClasses("select-all"); err == nil {
		t.Fatal("select-all must not apply (user-select: all is not modelled)")
	}
	var style StyleRefinement
	applyClasses(nil, &style, "select-none")
	if value, ok := style.Text.UserSelect.Get(); !ok || value != UserSelectNone {
		t.Fatalf("select-none set %v (ok %v), want none", value, ok)
	}
	applyClasses(nil, &style, "select-text")
	if value, ok := style.Text.UserSelect.Get(); !ok || value != UserSelectText {
		t.Fatalf("select-text set %v (ok %v), want text", value, ok)
	}
	applyClasses(nil, &style, "select-auto")
	if value, ok := style.Text.UserSelect.Get(); !ok || value != UserSelectAuto {
		t.Fatalf("select-auto set %v (ok %v), want auto", value, ok)
	}
}

// TestUserSelectStyleMethods pin the fluent methods against the
// refinement they write.
func TestUserSelectStyleMethods(t *testing.T) {
	div := Div().SelectNone()
	if value, ok := div.StyleMut().Text.UserSelect.Get(); !ok || value != UserSelectNone {
		t.Fatalf("SelectNone set %v (ok %v)", value, ok)
	}
	div = Div().SelectText()
	if value, ok := div.StyleMut().Text.UserSelect.Get(); !ok || value != UserSelectText {
		t.Fatalf("SelectText set %v (ok %v)", value, ok)
	}
	div = Div().SelectAuto()
	if value, ok := div.StyleMut().Text.UserSelect.Get(); !ok || value != UserSelectAuto {
		t.Fatalf("SelectAuto set %v (ok %v)", value, ok)
	}
}
