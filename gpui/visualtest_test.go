package gpui

// Base ticket 12: the test harness (R:app/test_context.rs,
// R:app/test_app.rs, R:window.rs:5697-5720, R:platform/keystroke.rs:
// 115-220, R:elements/div.rs:3882-3901; R: = reference/ce-source/crates/
// gpui/src at 254b5dbd). Every test drives a deterministic test window
// through the simulation helpers only: no explicit DrawWindowFrame
// between events (the flush draws the dirty test windows, Base ticket
// 33), no real input.

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// harnessView renders whatever its build function returns each frame.
type harnessView struct {
	build   func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement
	count   int
	renders int
}

func (v *harnessView) Render(w *Window, cx *Context[harnessView]) AnyElement {
	v.renders++
	if v.build == nil {
		return Div().Flex().IntoElement()
	}
	return v.build(v, w, cx)
}

// addHarnessWindow is the ported tests' `let (view, cx) =
// cx.add_window_view(...)`.
func addHarnessWindow(t *testing.T, build func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement) (*TestApp, Entity[harnessView], *VisualTestContext) {
	t.Helper()
	ta := NewTestApp()
	view, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) { v.build = build })
	return ta, view, cx
}

var (
	harnessUnitAction  = DefineUnitAction[harnessUnit]("harnesstest::Unit")
	harnessChordAction = DefineUnitAction[harnessChord]("harnesstest::Chord")
)

type harnessUnit struct{}
type harnessChord struct{}

// TestVisualTestContextAddWindowView pins add_window_view
// (R:app/test_context.rs:313-336): the window opens at the test
// display's maximized bounds (R:platform/test/display.rs:16), with the
// test window profile, drawn once before returning, and the returned
// view is the window's root.
func TestVisualTestContextAddWindowView(t *testing.T) {
	ta, view, cx := addHarnessWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		return Div().Flex().SizeFull().DebugSelector("root").IntoElement()
	})
	size, scale, rem, err := TestWindowDrawGeometry(cx.Window())
	if err != nil || size != (Size{Width: 1920, Height: 1080}) || scale != 2 || rem != 16 {
		t.Fatalf("geometry = %v %v %v %v", size, scale, rem, err)
	}
	if b, ok := cx.DebugBounds("root"); !ok || b.Size != (Size{Width: 1920, Height: 1080}) {
		t.Fatalf("root bounds = %+v %v", b, ok)
	}
	if renders := view.Read(ta, func(v *harnessView, _ *App) int { return v.renders }); renders != 1 {
		t.Fatalf("renders after open = %d, want 1 (open_window draws once)", renders)
	}
	root, ok := cx.Window().Root[harnessView]()
	if !ok || root.EntityID() != view.EntityID() {
		t.Fatal("the returned view is not the window root")
	}
	// The context is an application context (impl AppContext for
	// VisualTestContext).
	child := NewEntity(cx, cx.Window().Scope(), func(v *harnessView, _ *Context[harnessView]) { v.count = 7 })
	if got := child.Read(cx, func(v *harnessView, _ *App) int { return v.count }); got != 7 {
		t.Fatalf("entity through the context = %d", got)
	}

	empty := ta.AddEmptyWindow()
	if empty.Window() == cx.Window() {
		t.Fatal("AddEmptyWindow reused the window")
	}
	if _, ok := empty.Window().RootView(); !ok {
		t.Fatal("the empty window has no root view")
	}
}

// TestVisualTestContextSimulateClickDrivesDivClick drives a div click
// with simulate_click (R:app/test_context.rs:871-886): a press and a
// release at the point fire the div's click once, with the pinned
// mouse click facts; a click elsewhere fires nothing (the Base
// pointer_activation_fires_once shape, U:button.rs:337-344).
func TestVisualTestContextSimulateClickDrivesDivClick(t *testing.T) {
	var clicks []string
	_, _, cx := addHarnessWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		return Div().Flex().SizeFull().Child(Div().Flex().ID("button").Size(Px(50)).
			OnClick(func(e *ClickEvent, w *Window, app *App) {
				clicks = append(clicks, fmt.Sprintf("%s count=%d at=%v mods=%v", e.Kind, e.ClickCount(), e.Position(), e.Modifiers().Shift))
			})).IntoElement()
	})
	cx.SimulateClick(Point{X: 10, Y: 10}, NoModifiers())
	cx.SimulateClick(Point{X: 20, Y: 30}, ShiftModifiers())
	cx.SimulateClick(Point{X: 300, Y: 300}, NoModifiers())
	want := []string{"Mouse count=1 at={10 10} mods=false", "Mouse count=1 at={20 30} mods=true"}
	if !reflect.DeepEqual(clicks, want) {
		t.Fatalf("clicks = %q, want %q", clicks, want)
	}
}

// TestVisualTestContextKeyboardActivationThroughSimulatedEvents ports
// the Base enter_and_space_use_one_native_keyboard_click_each shape
// (U:button.rs:347-369) over a div: a click focuses the tracked element,
// then each KeyDown/KeyUp pair of simulate_event fires one keyboard
// click. simulate_keystrokes dispatches key downs only
// (R:window.rs:5697-5706), so it never completes a keyboard click.
func TestVisualTestContextKeyboardActivationThroughSimulatedEvents(t *testing.T) {
	var clicks []string
	ta := NewTestApp()
	focus := ta.App().FocusHandle()
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			return Div().Flex().SizeFull().Child(Div().Flex().ID("button").Size(Px(50)).TrackFocus(focus).
				OnClick(func(e *ClickEvent, w *Window, app *App) { clicks = append(clicks, e.Kind.String()) })).IntoElement()
		}
	})
	cx.SimulateClick(Point{X: 10, Y: 10}, NoModifiers())
	focused := false
	cx.Update(func(w *Window, app *App) { focused = focus.IsFocused(w) })
	if !focused {
		t.Fatal("the click did not focus the tracked element")
	}
	clicks = nil
	for _, key := range []string{"enter", "space"} {
		keystroke := MustParseKeystroke(key)
		cx.SimulateEvent(KeyDownEvent{Keystroke: keystroke})
		cx.SimulateEvent(&KeyUpEvent{Keystroke: keystroke})
	}
	if want := []string{"Keyboard", "Keyboard"}; !reflect.DeepEqual(clicks, want) {
		t.Fatalf("keyboard clicks = %q, want %q", clicks, want)
	}
	clicks = nil
	// simulate_keystrokes dispatches key downs only; browser-core
	// ticket 12 (divergence toward Chrome, spec D12) split the pin's
	// one rule in two: Enter activates at the KEYDOWN (the DOM keypress
	// rides the platform key event — html_element.cc:4318-4324 — so a
	// down-only Enter already clicks), while Space only arms at the
	// keydown and clicks at the keyup it never sees. The pin completed
	// neither without the keyup; the expectation changed with the
	// ticket.
	cx.SimulateKeystrokes("space")
	if len(clicks) != 0 {
		t.Fatalf("key-down-only space clicked: %q (Space clicks at the keyup)", clicks)
	}
	cx.SimulateKeystrokes("enter")
	if want := []string{"Keyboard"}; !reflect.DeepEqual(clicks, want) {
		t.Fatalf("key-down-only enter clicks = %q, want %q (Enter clicks at the keydown)", clicks, want)
	}
}

// TestSimulateKeystrokesChord drives a two-keystroke binding through
// "ctrl-a backspace" (R:app/test_context.rs:516-530: split on ' ', each
// component parsed per R:platform/keystroke.rs:120-220 and dispatched
// through dispatch_keystroke): the chord's action fires once, also when
// its keystrokes arrive in separate calls.
func TestSimulateKeystrokesChord(t *testing.T) {
	var fired []string
	ta := NewTestApp()
	ta.Update(func(app *App) {
		app.BindKeys(
			NewKeyBinding("ctrl-a backspace", harnessChordAction.Box(harnessChord{}), nil),
			NewKeyBinding("ctrl-shift-x", harnessUnitAction.Box(harnessUnit{}), nil),
		)
	})
	focus := ta.App().FocusHandle()
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			return Div().Flex().SizeFull().TrackFocus(focus).
				OnAction(harnessChordAction, func(*harnessChord, *Window, *App) { fired = append(fired, "chord") }).
				OnAction(harnessUnitAction, func(*harnessUnit, *Window, *App) { fired = append(fired, "unit") }).
				IntoElement()
		}
	})
	cx.Update(func(w *Window, app *App) { w.Focus(focus) })
	cx.RunUntilParked()

	cx.SimulateKeystrokes("ctrl-a backspace")
	if want := []string{"chord"}; !reflect.DeepEqual(fired, want) {
		t.Fatalf("after the chord: %q, want %q", fired, want)
	}
	cx.SimulateKeystrokes("ctrl-a")
	if len(fired) != 1 {
		t.Fatalf("the chord prefix fired: %q", fired)
	}
	cx.SimulateKeystrokes("backspace")
	cx.SimulateKeystrokes("ctrl-shift-x")
	if want := []string{"chord", "chord", "unit"}; !reflect.DeepEqual(fired, want) {
		t.Fatalf("after the split chord: %q, want %q", fired, want)
	}
	// Uppercase single keys parse as shift (R:platform/keystroke.rs:
	// 179-182): "ctrl-X" is ctrl-shift-x.
	cx.SimulateKeystrokes("ctrl-X")
	if want := []string{"chord", "chord", "unit", "unit"}; !reflect.DeepEqual(fired, want) {
		t.Fatalf("after ctrl-X: %q, want %q", fired, want)
	}
}

// TestSimulateKeystrokesParsesEveryComponent pins the key downs
// simulate_keystrokes dispatches: the parsed modifiers and keys, and the
// key_char with_simulated_ime fills (R:platform/keystroke.rs:241-263).
func TestSimulateKeystrokesParsesEveryComponent(t *testing.T) {
	var downs []string
	ta := NewTestApp()
	focus := ta.App().FocusHandle()
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			return Div().Flex().SizeFull().TrackFocus(focus).OnKeyDown(func(e *KeyDownEvent, w *Window, app *App) {
				downs = append(downs, fmt.Sprintf("%s>%q", e.Keystroke.Unparse(), e.Keystroke.KeyChar))
			}).IntoElement()
		}
	})
	cx.Update(func(w *Window, app *App) { w.Focus(focus) })
	cx.SimulateKeystrokes("ctrl-a backspace space B win-k a->b -")
	want := []string{
		`ctrl-a>""`,
		`backspace>""`,
		`space>" "`,
		`shift-b>"B"`,
		`win-k>""`,
		`a>"b"`,
		`->"-"`,
	}
	if !reflect.DeepEqual(downs, want) {
		t.Fatalf("key downs:\n got  %s\n want %s", strings.Join(downs, " "), strings.Join(want, " "))
	}
	expectHarnessPanic(t, "Invalid keystroke", func() { cx.SimulateKeystrokes("ctrl-a-b") })
}

// recordingInput is an input handler recording its deliveries.
type recordingInput struct{ dispatched, replaced []string }

func (r *recordingInput) AcceptsTextInput(*Window, *App) bool { return true }
func (r *recordingInput) ReplaceTextInRange(_ *UTF16Range, text string, _ *Window, _ *App) {
	r.replaced = append(r.replaced, text)
}
func (r *recordingInput) DispatchInput(input string, _ *Window, _ *App) {
	r.dispatched = append(r.dispatched, input)
}

// TestSimulateInputTypesThroughTheInputHandler pins simulate_input
// (R:app/test_context.rs:532-542: split on "", so empty keystrokes lead
// and trail) and dispatch_keystroke's text path (R:window.rs:5711-5717:
// a propagating key down's key_char goes to the input handler); a bound
// keystroke that stops propagation types nothing.
func TestSimulateInputTypesThroughTheInputHandler(t *testing.T) {
	var downs []string
	input := &recordingInput{}
	ta := NewTestApp()
	ta.Update(func(app *App) {
		app.BindKeys(NewKeyBinding("x", harnessUnitAction.Box(harnessUnit{}), nil))
	})
	focus := ta.App().FocusHandle()
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			w.HandleInput(focus, input)
			return Div().Flex().SizeFull().TrackFocus(focus).
				OnKeyDown(func(e *KeyDownEvent, w *Window, app *App) { downs = append(downs, e.Keystroke.Key) }).
				OnAction(harnessUnitAction, func(*harnessUnit, *Window, *App) { downs = append(downs, "action") }).
				IntoElement()
		}
	})
	cx.Update(func(w *Window, app *App) { w.Focus(focus) })
	cx.RunUntilParked()

	cx.SimulateInput("aB")
	if want := []string{"a", "B"}; !reflect.DeepEqual(input.dispatched, want) {
		t.Fatalf("typed %q, want %q", input.dispatched, want)
	}
	if want := []string{"", "a", "b", ""}; !reflect.DeepEqual(downs, want) {
		t.Fatalf("key downs %q, want %q (split(\"\") leads and trails with empty keystrokes)", downs, want)
	}
	downs, input.dispatched = nil, nil
	cx.SimulateKeystrokes("x")
	if len(input.dispatched) != 0 || !reflect.DeepEqual(downs, []string{"action"}) {
		t.Fatalf("bound keystroke: typed %q, downs %q", input.dispatched, downs)
	}
	if handled := cx.Window().DispatchKeystroke(MustParseKeystroke("z"), cx); !handled || !reflect.DeepEqual(input.dispatched, []string{"z"}) {
		t.Fatalf("DispatchKeystroke(z) handled=%v typed %q", handled, input.dispatched)
	}
	if len(input.replaced) != 0 {
		t.Fatalf("replace_text_in_range ran: %q", input.replaced)
	}
}

type harnessPayload struct{ name string }

type harnessPreview struct{}

func (p *harnessPreview) Render(w *Window, cx *Context[harnessPreview]) AnyElement {
	return Div().Flex().Size(Px(10)).IntoElement()
}

// TestVisualTestContextDragAndDrop drives a div drag with
// simulate_mouse_down / simulate_mouse_move(pressed Left) /
// simulate_mouse_up (R:app/test_context.rs:826-869): the move past the
// 2px threshold starts the drag once, drag moves reach the target, the
// release drops on the target and ends the drag, and the press that
// became a drag does not click.
func TestVisualTestContextDragAndDrop(t *testing.T) {
	rec := &transcript{}
	ta, _, cx := addHarnessWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		source := Div().Flex().ID("source").Absolute().Left(Px(0)).Top(Px(0)).Size(Px(50)).
			OnClick(func(*ClickEvent, *Window, *App) { rec.add("source.click") }).
			OnDrag(harnessPayload{name: "tab"}, func(value *harnessPayload, offset Point, w *Window, app *App) View {
				rec.add("drag-start(%s,%v)", value.name, offset)
				return ViewOf(NewEntity(app, w.Scope(), func(*harnessPreview, *Context[harnessPreview]) {}))
			})
		target := Div().Flex().ID("target").Absolute().Left(Px(200)).Top(Px(100)).Size(Px(100)).
			OnDragMove(func(e *DragMoveEvent[harnessPayload], w *Window, app *App) {
				rec.add("drag-move(%v)", e.Event.Position)
			})
		target = OnDrop(target, func(value *harnessPayload, e *MouseUpEvent, w *Window, app *App) {
			rec.add("drop(%s,%v)", value.name, e.Position)
		})
		return Div().Flex().Relative().SizeFull().Child(source).Child(target).IntoElement()
	})
	left := MouseButtonLeft
	cx.SimulateMouseDown(Point{X: 10, Y: 10}, MouseButtonLeft, NoModifiers())
	cx.SimulateMouseMove(Point{X: 11, Y: 11}, &left, NoModifiers())
	if ta.App().HasActiveDrag() {
		t.Fatal("a move inside the drag threshold started the drag")
	}
	cx.SimulateMouseMove(Point{X: 20, Y: 20}, &left, NoModifiers())
	if !ta.App().HasActiveDrag() {
		t.Fatal("the move past the threshold did not start the drag")
	}
	cx.SimulateMouseMove(Point{X: 250, Y: 150}, &left, NoModifiers())
	cx.SimulateMouseUp(Point{X: 250, Y: 150}, MouseButtonLeft, NoModifiers())
	// The starting move reaches no drag-move listener: those run in the
	// capture phase (div.rs:1194), before the source's bubble listener
	// starts the drag.
	wantTranscript(t, "drag", rec.take(), []string{
		"drag-start(tab,{20 20})",
		"drag-move({250 150})",
		"drop(tab,{250 150})",
	})
	if ta.App().HasActiveDrag() {
		t.Fatal("the drop did not end the drag")
	}
	cx.SimulateClick(Point{X: 10, Y: 10}, NoModifiers())
	wantTranscript(t, "click after drag", rec.take(), []string{"source.click"})
}

// TestVisualTestContextHoverAndModifiers covers simulate_mouse_move
// without a button (hover transitions, R:app/test_context.rs:826-838)
// and simulate_modifiers_change (R:app/test_context.rs:888-894 → the
// window's tracked modifiers, R:window.rs:5787-5790).
func TestVisualTestContextHoverAndModifiers(t *testing.T) {
	var hovers []bool
	_, _, cx := addHarnessWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		return Div().Flex().SizeFull().Child(Div().Flex().ID("h").Size(Px(50)).
			OnHover(func(hovered *bool, w *Window, app *App) { hovers = append(hovers, *hovered) })).IntoElement()
	})
	cx.SimulateMouseMove(Point{X: 10, Y: 10}, nil, NoModifiers())
	cx.SimulateMouseMove(Point{X: 20, Y: 20}, nil, NoModifiers())
	cx.SimulateMouseMove(Point{X: 100, Y: 100}, nil, NoModifiers())
	if want := []bool{true, false}; !reflect.DeepEqual(hovers, want) {
		t.Fatalf("hover transitions %v, want %v", hovers, want)
	}
	cx.SimulateModifiersChange(ControlModifiers())
	if !cx.Window().Modifiers().Control {
		t.Fatal("simulate_modifiers_change did not reach the window")
	}
	cx.SimulateCapslockChange(true)
	if cx.Window().Modifiers().Modified() {
		t.Fatal("simulate_capslock_change reports no modifiers")
	}
}

// TestVisualTestContextDebugBoundsAndResize covers debug_bounds
// (R:app/test_context.rs:909-912) across simulate_resize
// (R:platform/test/window.rs:157-168 → R:window.rs:2694-2705: the
// viewport follows and the refresh redraws).
func TestVisualTestContextDebugBoundsAndResize(t *testing.T) {
	_, view, cx := addHarnessWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		return Div().Flex().SizeFull().DebugSelector("root").Child(Div().Flex().Size(Px(40)).DebugSelector("box")).IntoElement()
	})
	if b, ok := cx.DebugBounds("box"); !ok || b != (Bounds{Size: Size{Width: 40, Height: 40}}) {
		t.Fatalf("box = %+v %v", b, ok)
	}
	if _, ok := cx.DebugBounds("missing"); ok {
		t.Fatal("an unknown selector reported bounds")
	}
	cx.SimulateResize(Size{Width: 800, Height: 600})
	if b, ok := cx.DebugBounds("root"); !ok || b.Size != (Size{Width: 800, Height: 600}) {
		t.Fatalf("root after resize = %+v %v", b, ok)
	}
	if renders := view.Read(cx, func(v *harnessView, _ *App) int { return v.renders }); renders != 2 {
		t.Fatalf("renders = %d, want 2 (open + the resize refresh)", renders)
	}
}

// TestVisualTestContextDispatchAction covers dispatch_action
// (R:app/test_context.rs:502-514, 791-797): the action reaches the
// focused element's listener, then the scheduler parks.
func TestVisualTestContextDispatchAction(t *testing.T) {
	fired := 0
	ta := NewTestApp()
	focus := ta.App().FocusHandle()
	_, cx := ta.AddWindowView(func(v *harnessView, w *Window, cx *Context[harnessView]) {
		v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
			return Div().Flex().SizeFull().TrackFocus(focus).
				OnAction(harnessUnitAction, func(*harnessUnit, *Window, *App) { fired++ }).IntoElement()
		}
	})
	cx.Update(func(w *Window, app *App) { w.Focus(focus) })
	cx.DispatchAction(harnessUnitAction.Box(harnessUnit{}))
	if fired != 1 {
		t.Fatalf("action fired %d times", fired)
	}
}

// TestDivComputeStyle pins Interactivity::compute_style
// (R:elements/div.rs:3882-3901): the style a div computes through its
// retained interactive state follows the pressed state the simulated
// press records (the active style, div.rs:4027-4041) and returns to the
// base style on release. The div has a click listener: an active style
// alone inserts no hitbox (div.rs:2887-2911), so nothing could press it.
func TestDivComputeStyle(t *testing.T) {
	makeDiv := func() *DivElement {
		return Div().Flex().ID("t").Size(Px(50)).Bg(interactRed).
			OnClick(func(*ClickEvent, *Window, *App) {}).
			Active(func(s StyleRefinement) StyleRefinement { return Div().Flex().Refine(s).Bg(interactGreen).Style() })
	}
	_, view, cx := addHarnessWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		return Div().Flex().SizeFull().Child(makeDiv()).IntoElement()
	})
	global := &GlobalElementID{Path: []ElementID{ViewElementID(view.EntityID()), NameElementID("t")}}
	background := func() Background {
		var style Style
		cx.Update(func(w *Window, app *App) { style = makeDiv().ComputeStyle(global, nil, w, app) })
		if style.Background == nil {
			t.Fatal("no background")
		}
		return *style.Background
	}
	if got := background(); got != SolidBackground(interactRed) {
		t.Fatalf("base background = %+v", got)
	}
	cx.SimulateMouseDown(Point{X: 10, Y: 10}, MouseButtonLeft, NoModifiers())
	if got := background(); got != SolidBackground(interactGreen) {
		t.Fatalf("pressed background = %+v", got)
	}
	cx.SimulateMouseUp(Point{X: 10, Y: 10}, MouseButtonLeft, NoModifiers())
	if got := background(); got != SolidBackground(interactRed) {
		t.Fatalf("released background = %+v", got)
	}
	// An id-less div has no retained state: the base style.
	var style Style
	cx.Update(func(w *Window, app *App) { style = Div().Flex().Bg(interactBlue).ComputeStyle(nil, nil, w, app) })
	if style.Background == nil || *style.Background != SolidBackground(interactBlue) {
		t.Fatalf("id-less style = %+v", style.Background)
	}
}

// a11yProbe is a custom element publishing a fixed accessibility spec.
type a11yProbe struct{ spec A11ySpec }

func (p *a11yProbe) ID() (ElementID, bool)           { return NameElementID("probe"), true }
func (p *a11yProbe) SourceLocation() *SourceLocation { return nil }
func (p *a11yProbe) A11ySpec() A11ySpec              { return p.spec }
func (p *a11yProbe) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	style := DefaultStyle()
	style.Size.Width = PxLength(30)
	style.Size.Height = PxLength(20)
	id, err := requestLayoutOf(w, style)
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}
func (p *a11yProbe) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) struct{} {
	return struct{}{}
}
func (p *a11yProbe) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *struct{}, w *Window, app *App) {
}

// TestVisualTestContextReadsPerElementA11yInfo reads one element's
// published accessibility facts through the existing snapshot API
// (ActivateWindowA11y / PublishWindowA11y / A11yNodeIDOf).
func TestVisualTestContextReadsPerElementA11yInfo(t *testing.T) {
	label := "Go"
	_, view, cx := addHarnessWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		return Div().Flex().SizeFull().Child(Accessibility[struct{}, struct{}](&a11yProbe{spec: A11ySpec{Role: RoleButton, Label: label, Value: "v"}})).IntoElement()
	})
	node, ok := cx.A11yNode(ViewElementID(view.EntityID()), NameElementID("probe"))
	if !ok {
		snapshot, err := cx.A11ySnapshot()
		t.Fatalf("probe node missing (err %v):\n%s", err, snapshot.Trace())
	}
	if node.Role != RoleButton || node.Label != "Go" || node.Value != "v" || node.Bounds.Size != (Size{Width: 30, Height: 20}) {
		t.Fatalf("probe node = %s", node)
	}
	// A11ySnapshot refreshes before publishing, so a later read reflects
	// the element's current spec.
	label = "Stop"
	node, _ = cx.A11yNode(ViewElementID(view.EntityID()), NameElementID("probe"))
	if node.Label != "Stop" {
		t.Fatalf("label after refresh = %q", node.Label)
	}
}

// TestTestAppWindowSimulation covers TestAppWindow
// (R:app/test_app.rs:320-494): click, keystrokes (run until parked after
// each), mouse move, scroll, update/read and draw on a sized window
// (open_window_with_options).
func TestTestAppWindowSimulation(t *testing.T) {
	var events []string
	ta := NewTestApp()
	ta.Update(func(app *App) {
		app.BindKeys(NewKeyBinding("ctrl-a backspace", harnessChordAction.Box(harnessChord{}), nil))
	})
	focus := ta.App().FocusHandle()
	window := ta.OpenWindowWithOptions(WindowOptions{Bounds: Bounds{Size: Size{Width: 400, Height: 300}}},
		func(v *harnessView, w *Window, cx *Context[harnessView]) {
			v.build = func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
				return Div().Flex().ID("root").SizeFull().TrackFocus(focus).DebugSelector("root").
					OnClick(func(e *ClickEvent, w *Window, app *App) { events = append(events, "click") }).
					OnAuxClick(func(e *ClickEvent, w *Window, app *App) { events = append(events, "aux") }).
					OnHover(func(h *bool, w *Window, app *App) { events = append(events, fmt.Sprintf("hover(%v)", *h)) }).
					OnScrollWheel(func(e *ScrollWheelEvent, phase DispatchPhase, hitbox *Hitbox, w *Window, app *App) {
						if phase == DispatchBubble {
							events = append(events, fmt.Sprintf("scroll(%v)", e.Delta.Pixels))
						}
					}).
					OnAction(harnessChordAction, func(*harnessChord, *Window, *App) { events = append(events, "chord") }).
					IntoElement()
			}
		})
	if b, ok := WindowDebugBound(window.Window(), "root"); !ok || b.Size != (Size{Width: 400, Height: 300}) {
		t.Fatalf("root = %+v %v", b, ok)
	}
	window.SimulateMouseMove(Point{X: 5, Y: 5})
	window.SimulateClick(Point{X: 5, Y: 5}, MouseButtonLeft)
	window.SimulateClick(Point{X: 5, Y: 5}, MouseButtonRight)
	window.SimulateScroll(Point{X: 5, Y: 5}, Point{X: 0, Y: -12})
	window.SimulateKeystrokes("ctrl-a backspace")
	// The key down switches the input modality to the keyboard
	// (R:window.rs:5746-5750), which applies focus-visible styles but no
	// longer clears hover: browser-core ticket 10 (spec D12, hover
	// survives typing) removed the R:window.rs:887-897 suppression, so
	// the redraw's hover check reports the element still hovered and
	// OnHover does not fire (the previous expectation ended with
	// "hover(false)" before the chord; recorded as an intended behaviour
	// change).
	want := []string{"hover(true)", "click", "aux", "scroll({0 -12})", "chord"}
	if !reflect.DeepEqual(events, want) {
		t.Fatalf("events %q, want %q", events, want)
	}

	window.Update(func(v *harnessView, w *Window, cx *Context[harnessView]) { v.count++; cx.Notify() })
	if got := window.Read(func(v *harnessView, _ *App) int { return v.count }); got != 1 {
		t.Fatalf("count = %d", got)
	}
	renders := window.Read(func(v *harnessView, _ *App) int { return v.renders })
	window.Draw()
	if got := window.Read(func(v *harnessView, _ *App) int { return v.renders }); got != renders+1 {
		t.Fatalf("Draw rendered %d times", got-renders)
	}
	window.SimulateResize(Size{Width: 200, Height: 100})
	if b, _ := WindowDebugBound(window.Window(), "root"); b.Size != (Size{Width: 200, Height: 100}) {
		t.Fatalf("root after resize = %+v", b)
	}
	if window.Root().EntityID() == 0 {
		t.Fatal("no root")
	}
}

// TestSimulateEventValidation pins the simulated input's preconditions:
// value events are accepted like pointers, non-input values and closed
// windows panic (update_window's unwrap, R:app/test_context.rs:546-549).
func TestSimulateEventValidation(t *testing.T) {
	clicks := 0
	_, _, cx := addHarnessWindow(t, func(v *harnessView, w *Window, cx *Context[harnessView]) AnyElement {
		return Div().Flex().ID("b").SizeFull().OnClick(func(*ClickEvent, *Window, *App) { clicks++ }).IntoElement()
	})
	cx.SimulateEvent(MouseDownEvent{Button: MouseButtonLeft, Position: Point{X: 1, Y: 1}, ClickCount: 1})
	cx.SimulateEvent(MouseUpEvent{Button: MouseButtonLeft, Position: Point{X: 1, Y: 1}, ClickCount: 1})
	if clicks != 1 {
		t.Fatalf("value events clicked %d times", clicks)
	}
	expectHarnessPanic(t, "not a platform input event", func() { cx.SimulateEvent("click") })
	expectHarnessPanic(t, "non-nil event", func() { cx.SimulateEvent(nil) })
	CloseTestWindow(cx.Window())
	expectHarnessPanic(t, "window not found", func() { cx.SimulateClick(Point{}, NoModifiers()) })
	expectHarnessPanic(t, "window not found", func() { cx.SimulateKeystrokes("a") })
}

// TestKeystrokeSplitting pins the Rust split semantics the simulators
// reproduce: str::split(' ') keeps empty components, str::split("")
// leads and trails with an empty component.
func TestKeystrokeSplitting(t *testing.T) {
	if got := splitOnSpace("ctrl-a  b"); !reflect.DeepEqual(got, []string{"ctrl-a", "", "b"}) {
		t.Fatalf("split(' ') = %q", got)
	}
	if got := splitOnSpace(""); !reflect.DeepEqual(got, []string{""}) {
		t.Fatalf("split(' ') of empty = %q", got)
	}
	if got := splitOnEmpty("ab"); !reflect.DeepEqual(got, []string{"", "a", "b", ""}) {
		t.Fatalf("split(\"\") = %q", got)
	}
	if got := splitOnEmpty(""); !reflect.DeepEqual(got, []string{"", ""}) {
		t.Fatalf("split(\"\") of empty = %q", got)
	}
}

// ---------------------------------------------------------------------------
// The pinned TestApp tests (R:app/test_app.rs:507-608)
// ---------------------------------------------------------------------------

// harnessCounter is the pinned test module's Counter view.
type harnessCounter struct {
	count int
	focus FocusHandle
}

func (c *harnessCounter) Render(w *Window, cx *Context[harnessCounter]) AnyElement {
	return Div().Flex().Child(fmt.Sprintf("Count: %d", c.count)).IntoElement()
}

// TestTestAppBasicUsage ports test_basic_usage (R:app/test_app.rs:
// 543-559). The pin shuts down inside an update; the port's
// App.Shutdown drains the scheduler, so it runs outside one.
func TestTestAppBasicUsage(t *testing.T) {
	ta := NewTestApp()
	window := ta.OpenWindow(func(c *harnessCounter, w *Window, cx *Context[harnessCounter]) {
		c.focus = cx.App().FocusHandle()
	})
	window.Update(func(c *harnessCounter, w *Window, cx *Context[harnessCounter]) { c.count++ })
	if got := window.Read(func(c *harnessCounter, _ *App) int { return c.count }); got != 1 {
		t.Fatalf("count = %d, want 1", got)
	}
	ta.App().Shutdown()
}

// TestTestAppEntityCreation ports test_entity_creation (R:app/
// test_app.rs:561-581) over the TestApp's AppContext (the port spells
// new_entity / read_entity / update_entity as NewEntity, Entity.Read and
// Entity.Update through the context).
func TestTestAppEntityCreation(t *testing.T) {
	ta := NewTestApp()
	entity := NewEntity(ta, ta.RootScope(), func(c *harnessCounter, cx *Context[harnessCounter]) {
		c.count = 42
		c.focus = cx.App().FocusHandle()
	})
	if got := entity.Read(ta, func(c *harnessCounter, _ *App) int { return c.count }); got != 42 {
		t.Fatalf("count = %d", got)
	}
	entity.Update(ta, func(c *harnessCounter, _ *Context[harnessCounter]) { c.count++ })
	if got := entity.Read(ta, func(c *harnessCounter, _ *App) int { return c.count }); got != 43 {
		t.Fatalf("count = %d", got)
	}
}

type harnessGlobal struct{ value string }

// TestTestAppGlobals ports test_globals (R:app/test_app.rs:583-607)
// over the App's global functions (TestApp has no global helpers; the
// port has no update_global, so the update re-sets the global).
func TestTestAppGlobals(t *testing.T) {
	ta := NewTestApp()
	if HasGlobal[harnessGlobal](ta.App()) {
		t.Fatal("global present before set")
	}
	ta.Update(func(app *App) { app.SetGlobal(harnessGlobal{value: "hello"}) })
	if !HasGlobal[harnessGlobal](ta.App()) || Global[harnessGlobal](ta.App()).value != "hello" {
		t.Fatal("set_global")
	}
	ta.Update(func(app *App) {
		g := Global[harnessGlobal](app)
		g.value = "world"
		app.SetGlobal(g)
	})
	if Global[harnessGlobal](ta.App()).value != "world" {
		t.Fatal("update_global")
	}
}

func expectHarnessPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Fatalf("no panic, want %q", want)
		}
		if msg := fmt.Sprint(r); !strings.Contains(msg, want) {
			t.Fatalf("panic %q, want it to contain %q", msg, want)
		}
	}()
	f()
}
