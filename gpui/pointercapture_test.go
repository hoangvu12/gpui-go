package gpui

// Browser-core ticket 11's pointer-capture gates (headless, simulated
// input only): the capture retargeting (events go to the captured
// element and its ancestors alone), the gotpointercapture/
// lostpointercapture timing (pending capture processed at the next
// event, implicit release after the up), the captured click, the
// element-local position, the held-button requirement, and the pointer
// cancel (WM_CAPTURECHANGED's pointercancel: no stuck button, no
// click, the drag cancelled). The real-window WM_CAPTURECHANGED twin
// is pointercapture_windows_test.go; the Chrome-checked twin is the
// browser-parity capture fixture (internal/parity/fixtures/capture).

import (
	"strings"
	"testing"
)

// captureFixture is one test window rendering the capture tree: the
// full-window root, a parent p at (100,100) 200x120, and two 80x60
// children — cap (at p's origin) and other (80px right of cap) — all
// recording pointer enter/leave, mouse down/up/move, got/lost capture
// and click events into a transcript. cap's press requests the pointer
// capture, like a page's pointerdown handler calling
// setPointerCapture.
type captureFixture struct {
	t      *testing.T
	app    *App
	window *Window
	view   Entity[interactView]
	events *transcript
}

// captureTree builds the nested tree. p is relative-positioned so the
// children's absolute offsets resolve against it; cap and other sit
// side by side at p's top-left (the pin's flex default, no gap).
func captureTree(f *captureFixture, v *interactView, w *Window, cx *Context[interactView]) AnyElement {
	leaf := func(id string, x float32) *DivElement {
		return Div().ID(id).DebugSelector(id).
			Absolute().Left(Px(x)).Top(Px(0)).Size(Px(80), Px(60)).
			OnPointerEnter(func(*PointerEnterEvent, *Window, *App) { f.events.add("enter:%s", id) }).
			OnPointerLeave(func(*PointerLeaveEvent, *Window, *App) { f.events.add("leave:%s", id) }).
			OnPointerEnter(func(*PointerEnterEvent, *Window, *App) {}).
			OnMouseDown(MouseButtonLeft, func(*MouseDownEvent, *Window, *App) { f.events.add("down:%s", id) }).
			OnMouseMove(func(*MouseMoveEvent, *Window, *App) { f.events.add("move:%s", id) }).
			OnMouseUp(MouseButtonLeft, func(*MouseUpEvent, *Window, *App) { f.events.add("up:%s", id) }).
			OnClick(func(*ClickEvent, *Window, *App) { f.events.add("click:%s", id) }).
			OnGotPointerCapture(func(*GotPointerCaptureEvent, *Window, *App) { f.events.add("got:%s", id) }).
			OnLostPointerCapture(func(*LostPointerCaptureEvent, *Window, *App) { f.events.add("lost:%s", id) })
	}
	root := Div().ID("root").DebugSelector("root").SizeFull().Relative().
		OnPointerEnter(func(*PointerEnterEvent, *Window, *App) { f.events.add("enter:root") }).
		OnPointerLeave(func(*PointerLeaveEvent, *Window, *App) { f.events.add("leave:root") }).
		OnMouseDown(MouseButtonLeft, func(*MouseDownEvent, *Window, *App) { f.events.add("down:root") }).
		OnMouseMove(func(*MouseMoveEvent, *Window, *App) { f.events.add("move:root") }).
		OnMouseUp(MouseButtonLeft, func(*MouseUpEvent, *Window, *App) { f.events.add("up:root") }).
		OnClick(func(*ClickEvent, *Window, *App) { f.events.add("click:root") }).
		OnGotPointerCapture(func(*GotPointerCaptureEvent, *Window, *App) { f.events.add("got:root") }).
		OnLostPointerCapture(func(*LostPointerCaptureEvent, *Window, *App) { f.events.add("lost:root") })
	p := Div().ID("p").DebugSelector("p").
		Absolute().Left(Px(100)).Top(Px(100)).Size(Px(200), Px(120)).
		OnPointerEnter(func(*PointerEnterEvent, *Window, *App) { f.events.add("enter:p") }).
		OnPointerLeave(func(*PointerLeaveEvent, *Window, *App) { f.events.add("leave:p") }).
		OnMouseDown(MouseButtonLeft, func(*MouseDownEvent, *Window, *App) { f.events.add("down:p") }).
		OnMouseMove(func(*MouseMoveEvent, *Window, *App) { f.events.add("move:p") }).
		OnMouseUp(MouseButtonLeft, func(*MouseUpEvent, *Window, *App) { f.events.add("up:p") }).
		OnClick(func(*ClickEvent, *Window, *App) { f.events.add("click:p") }).
		OnGotPointerCapture(func(*GotPointerCaptureEvent, *Window, *App) { f.events.add("got:p") }).
		OnLostPointerCapture(func(*LostPointerCaptureEvent, *Window, *App) { f.events.add("lost:p") })
	cap := leaf("cap", 0)
	// The capture request: a pressed, hovered cap captures the pointer,
	// the port's idiom for a page's pointerdown handler calling
	// setPointerCapture(e.pointerId).
	cap = cap.OnMouseDownAll(func(e *MouseDownEvent, phase DispatchPhase, hitbox *Hitbox, w *Window, app *App) {
		if phase == DispatchBubble && e.Button == MouseButtonLeft && hitbox.IsHovered(w) {
			if !w.CapturePointer(hitbox.ID()) {
				f.events.add("capture-rejected")
			}
		}
	})
	return root.
		Child(p.
			Child(cap.IntoElement()).
			Child(leaf("other", 80).IntoElement())).
		IntoElement()
}

// cap center = (140,130); other center = (220,130); the root's
// background = (20,20).
const (
	captureCapX   = 140.0
	captureCapY   = 130.0
	captureOtherX = 220.0
	captureOtherY = 130.0
)

func newCaptureFixture(t *testing.T) *captureFixture {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	f := &captureFixture{t: t, app: app, window: window, events: &transcript{}}
	view := NewEntity(app, window.Scope(), func(v *interactView, cx *Context[interactView]) {
		v.build = func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			return captureTree(f, v, w, cx)
		}
	})
	f.view = view
	window.SetRootView(ViewOf(view))
	f.draw()
	return f
}

func (f *captureFixture) draw() {
	f.t.Helper()
	if _, err := DrawWindowFrame(f.window); err != nil {
		f.t.Fatalf("DrawWindowFrame: %v", err)
	}
}

func (f *captureFixture) move(x, y float32) {
	f.window.DispatchInput(&MouseMoveEvent{Position: Point{X: x, Y: y}}, f.app)
}

func (f *captureFixture) dragMove(x, y float32) {
	left := MouseButtonLeft
	f.window.DispatchInput(&MouseMoveEvent{Position: Point{X: x, Y: y}, PressedButton: &left}, f.app)
}

func (f *captureFixture) down(x, y float32) {
	f.window.DispatchInput(&MouseDownEvent{Button: MouseButtonLeft, Position: Point{X: x, Y: y}, ClickCount: 1}, f.app)
}

func (f *captureFixture) up(x, y float32) {
	f.window.DispatchInput(&MouseUpEvent{Button: MouseButtonLeft, Position: Point{X: x, Y: y}, ClickCount: 1}, f.app)
}

func (f *captureFixture) cancel() {
	f.window.DispatchInput(&PointerCancelEvent{Position: Point{X: 220, Y: 130}}, f.app)
}

func (f *captureFixture) take() []string { return f.events.take() }

// TestPointerCaptureRetargetsEventsToTheCapturedChain is the ticket's
// first acceptance criterion at unit level: while cap holds the
// capture, a move outside its bounds delivers pointer events to cap
// and its ancestors alone — other, under the pointer, gets nothing and
// no boundary events fire for anyone (Chrome's boundary behaviour
// under capture: the element under the pointer IS the capture target,
// pointer_event_manager.cc:232-239, :1362). The up outside still
// clicks cap (Chromium targets the click at the capture element,
// mouse_event_manager.cc:359-360), and after the release the boundary
// events settle: leave cap, enter other.
func TestPointerCaptureRetargetsEventsToTheCapturedChain(t *testing.T) {
	f := newCaptureFixture(t)

	// Into cap: the entered chain (parent to child) then the move
	// (target first, then ancestors).
	f.move(captureCapX, captureCapY)
	wantTranscript(t, "enter cap", f.take(), []string{
		"enter:root", "enter:p", "enter:cap",
		"move:cap", "move:p", "move:root",
	})

	// The press on cap requests the capture; nothing fires yet.
	f.down(captureCapX, captureCapY)
	wantTranscript(t, "the down", f.take(), []string{
		"down:cap", "down:p", "down:root",
	})
	if _, ok := f.window.CapturedHitbox(); ok {
		t.Fatal("the capture must be pending until the next pointer event (gotpointercapture timing)")
	}

	// The move outside cap: gotpointercapture fires first (target then
	// ancestors — the events bubble), the move still targets cap and p
	// (the retargeted chain), other gets nothing, and no boundary
	// events fire.
	f.dragMove(captureOtherX, captureOtherY)
	wantTranscript(t, "captured move outside", f.take(), []string{
		"got:cap", "got:p", "got:root",
		"move:cap", "move:p", "move:root",
	})
	if _, ok := f.window.CapturedHitbox(); !ok {
		t.Fatal("the capture did not become active at the move")
	}

	// Back over cap: still the chain, no boundary events.
	f.dragMove(captureCapX, captureCapY)
	wantTranscript(t, "captured move back", f.take(), []string{
		"move:cap", "move:p", "move:root",
	})

	// The release outside cap: the up targets the captured chain, the
	// click goes to the captured chain (Chromium's captured click
	// target, mouse_event_manager.cc:359-360 — the pin's model clicks
	// every hovered-chain element that saw the press, which is the
	// click's bubbling shape), the lostpointercapture fires after the
	// up's listeners, and the boundary events settle after the release —
	// leave cap (innermost first), enter other. The per-element
	// click-before-up order is the pin's known divergence from the
	// browser's mouseup-before-click (docs/browser-parity-harness.md,
	// owned by the input bundle, D12) — this ticket only owns the
	// capture order: the click still targets the captured element, and
	// lostpointercapture follows the ups.
	f.up(captureOtherX, captureOtherY)
	wantTranscript(t, "the captured release", f.take(), []string{
		"click:cap", "up:cap",
		"click:p", "up:p",
		"click:root", "up:root",
		"lost:cap", "lost:p", "lost:root",
		"leave:cap", "enter:other",
	})
	if _, ok := f.window.CapturedHitbox(); ok {
		t.Fatal("the up must release the capture")
	}

	// After the release, events follow the hit test again.
	f.move(captureOtherX, captureOtherY)
	wantTranscript(t, "uncaptured move", f.take(), []string{
		"move:other", "move:p", "move:root",
	})
}

// TestPointerCaptureRequiresHeldButton checks Chromium's
// IsActiveButtonsState gate (pointer_event_manager.cc:1440-1447): a
// capture request with no button held is rejected and never becomes
// active.
func TestPointerCaptureRequiresHeldButton(t *testing.T) {
	f := newCaptureFixture(t)

	f.move(captureCapX, captureCapY)
	f.take()

	// With no button held, CapturePointer reports false.
	fs := focusState(f.window)
	if fs.rendered == nil {
		t.Fatal("no rendered frame")
	}
	var hitboxID HitboxID
	for _, record := range fs.rendered.hitboxes {
		if strings.HasSuffix(record.ownerPath, "root.p.cap") {
			hitboxID = HitboxID(record.box.id)
		}
	}
	if hitboxID == 0 {
		t.Fatal("cap's hitbox was not found in the rendered frame")
	}
	if f.window.CapturePointer(hitboxID) {
		t.Fatal("CapturePointer with no button held must be rejected (IsActiveButtonsState)")
	}
	// A rejected request does not even become pending: the next event
	// processes no capture change.
	f.move(captureOtherX, captureOtherY)
	wantTranscript(t, "the move after the rejected request", f.take(), []string{
		"leave:cap", "enter:other", "move:other", "move:p", "move:root",
	})
	if _, ok := f.window.CapturedHitbox(); ok {
		t.Fatal("a rejected capture must not become active")
	}
}

// TestPointerCaptureLocalPosition checks the element-local position
// (spec D12): down/move/up positions relative to a hitbox's origin,
// including the captured drag's out-of-bounds values (the DOM's
// offsetX while captured) and a nested ancestor's own frame.
func TestPointerCaptureLocalPosition(t *testing.T) {
	var capLocal, pLocal Point
	var moveLocal Point
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		return Div().ID("root").SizeFull().Relative().
			Child(Div().ID("p").Absolute().Left(Px(100)).Top(Px(100)).Size(Px(200), Px(120)).
				OnMouseDown(MouseButtonLeft, func(e *MouseDownEvent, w *Window, app *App) {
					pLocal = e.LocalPosition(wHitbox(w, ".root.p"))
				}).
				Child(Div().ID("cap").Absolute().Left(Px(0)).Top(Px(0)).Size(Px(80), Px(60)).
					OnMouseDownAll(func(e *MouseDownEvent, phase DispatchPhase, hitbox *Hitbox, w *Window, app *App) {
						if phase == DispatchBubble && hitbox.IsHovered(w) {
							w.CapturePointer(hitbox.ID())
						}
					}).
					OnMouseDown(MouseButtonLeft, func(e *MouseDownEvent, w *Window, app *App) {
						capLocal = e.LocalPosition(wHitbox(w, ".root.p.cap"))
					}).
					OnMouseMoveAll(func(e *MouseMoveEvent, phase DispatchPhase, hitbox *Hitbox, w *Window, app *App) {
						if phase == DispatchBubble {
							moveLocal = e.LocalPosition(hitbox)
						}
					})).IntoElement()).IntoElement()
	})

	// The press at cap's centre (140,130): cap-local (40,30),
	// p-local (40,30) — p's origin is (100,100).
	f.down(MouseButtonLeft, captureCapX, captureCapY)
	if capLocal != (Point{X: 40, Y: 30}) {
		t.Fatalf("cap-local down position = %v, want (40,30)", capLocal)
	}
	if pLocal != (Point{X: 40, Y: 30}) {
		t.Fatalf("p-local down position = %v, want (40,30)", pLocal)
	}

	// The captured drag outside cap: the move's local position relative
	// to cap runs past its bounds, like the DOM's offsetX under
	// capture.
	f.dragMove(captureOtherX, captureOtherY)
	if moveLocal != (Point{X: 120, Y: 30}) {
		t.Fatalf("captured move local position = %v, want (120,30) — past cap's 80 width", moveLocal)
	}
	f.up(MouseButtonLeft, captureOtherX, captureOtherY)

	// A nil hitbox degenerates to the zero point.
	if got := (&MouseDownEvent{Position: Point{X: 5, Y: 5}}).LocalPosition(nil); got != (Point{}) {
		t.Fatalf("nil hitbox local position = %v, want the zero point", got)
	}
}

// wHitbox looks a rendered element's hitbox up by its owner path
// suffix (the root view's element contributes a view-<entity> segment
// to every path; the test's book-keeping for LocalPosition).
func wHitbox(w *Window, path string) *Hitbox {
	fs := focusState(w)
	if fs.rendered == nil {
		return nil
	}
	for _, record := range fs.rendered.hitboxes {
		if strings.HasSuffix(record.ownerPath, path) {
			box := record.box
			return &box
		}
	}
	return nil
}

// TestPointerCancelClearsPressesAndDrag is the ticket's second
// acceptance criterion at unit level (the real-window twin is
// pointercapture_windows_test.go): a pointer cancel — the
// WM_CAPTURECHANGED translation — fires lostpointercapture, drops the
// pending press so the release that never arrives leaves no active
// style and no later click, cancels an active drag, and clears the
// held-button tracking (a subsequent capture request is refused until
// a fresh press).
func TestPointerCancelClearsPressesAndDrag(t *testing.T) {
	f := newCaptureFixture(t)

	f.move(captureCapX, captureCapY)
	wantTranscript(t, "enter cap", f.take(), []string{"enter:root", "enter:p", "enter:cap", "move:cap", "move:p", "move:root"})

	// The press, the capture, and a move that starts nothing.
	f.down(captureCapX, captureCapY)
	f.take()
	f.dragMove(captureOtherX, captureOtherY)
	wantTranscript(t, "captured move outside", f.take(), []string{"got:cap", "got:p", "got:root", "move:cap", "move:p", "move:root"})

	// The cancel: lostpointercapture fires, and the cancel dispatch
	// itself sends no boundary events (Chromium sends none for a
	// pointercancel, pointer_event_manager.cc:1316-1321). The leave/
	// enter pair that follows comes from the repaint's hover diff (the
	// deferred boundary sweep, hoverboundary.go) once the cancel cleared
	// the capture and the press: the port settles its hover styles at
	// the repaint where Chromium leaves them stale until the next
	// pointer event — a benign, documented divergence in when, not
	// whether, hover settles.
	f.cancel()
	wantTranscript(t, "the cancel", f.take(), []string{
		"lost:cap", "lost:p", "lost:root",
		"leave:cap", "enter:other",
	})
	if _, ok := f.window.CapturedHitbox(); ok {
		t.Fatal("the cancel must release the capture")
	}

	// The up that arrives later (the real window never sees it — this
	// dispatch is the stray case) reaches the elements under the
	// pointer, and fires NO click anywhere: the press was cancelled.
	f.up(captureOtherX, captureOtherY)
	wantTranscript(t, "the stray up after the cancel", f.take(), []string{
		"up:other", "up:p", "up:root",
	})

	// A fresh press works again: the capture is accepted and the click
	// fires.
	f.move(captureCapX, captureCapY)
	f.take()
	f.down(captureCapX, captureCapY)
	f.dragMove(captureCapX, captureCapY)
	f.up(captureCapX, captureCapY)
	wantTranscript(t, "the press after the cancel", f.take(), []string{
		"down:cap", "down:p", "down:root",
		"got:cap", "got:p", "got:root",
		"move:cap", "move:p", "move:root",
		"click:cap", "up:cap", "click:p", "up:p", "click:root", "up:root",
		"lost:cap", "lost:p", "lost:root",
	})
}

// TestPointerCancelCancelsActiveDrag checks the drag arm of the
// cancel: an element drag in flight when another window takes the
// capture ends without a drop (Chromium's pointercancel ends the drag
// when the stream is suppressed, :1316-1321).
func TestPointerCancelCancelsActiveDrag(t *testing.T) {
	rec := &transcript{}
	f := newInteractFixture(t, func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
		source := Div().ID("source").Absolute().Left(Px(0)).Top(Px(0)).Size(Px(50)).
			OnDrag(dragPayload{name: "tab"}, func(value *dragPayload, offset Point, w *Window, app *App) View {
				rec.add("constructor(%s)", value.name)
				return ViewOf(NewEntity(app, w.Scope(), func(*dragPreview, *Context[dragPreview]) {}))
			})
		return Div().Relative().SizeFull().Child(source).Child(&previewHostElement{}).IntoElement()
	})
	f.down(MouseButtonLeft, 10, 10)
	f.draw()
	f.dragMove(60, 60)
	if !f.app.HasActiveDrag() {
		t.Fatal("the move past the threshold started no drag")
	}
	wantTranscript(t, "drag start", rec.take(), []string{"constructor(tab)"})

	// The cancel ends the drag with no drop.
	f.window.DispatchInput(&PointerCancelEvent{Position: Point{X: 60, Y: 60}}, f.app)
	if f.app.HasActiveDrag() {
		t.Fatal("the pointer cancel must cancel the active drag")
	}
	if got := rec.take(); len(got) != 0 {
		t.Fatalf("events after the cancel = %v, want none", got)
	}

	// A fresh press-and-drag works again.
	f.down(MouseButtonLeft, 10, 10)
	f.draw()
	f.dragMove(60, 60)
	if !f.app.HasActiveDrag() {
		t.Fatal("the drag after the cancel did not start")
	}
	wantTranscript(t, "drag restart", rec.take(), []string{"constructor(tab)"})
}

// TestPointerCaptureDropsWhenElementLeavesTree checks the removed-target
// rule: a captured element that leaves the tree drops the capture
// silently at the next dispatch (Chromium re-targets the
// lostpointercapture to the document, :1387-1393; the port has no
// document-level listener, so nothing fires) and events follow the hit
// test again.
func TestPointerCaptureDropsWhenElementLeavesTree(t *testing.T) {
	f := newCaptureFixture(t)

	f.move(captureCapX, captureCapY)
	f.down(captureCapX, captureCapY)
	f.dragMove(captureOtherX, captureOtherY)
	wantTranscript(t, "captured move outside", f.take(), []string{
		"enter:root", "enter:p", "enter:cap", "move:cap", "move:p", "move:root",
		"down:cap", "down:p", "down:root",
		"got:cap", "got:p", "got:root", "move:cap", "move:p", "move:root",
	})

	// Unmount p (cap's parent): the next dispatch drops the capture.
	f.view.Update(f.app, func(v *interactView, cx *Context[interactView]) {
		v.left = 0
		v.build = func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			return Div().ID("root").DebugSelector("root").SizeFull().Relative().
				OnMouseDown(MouseButtonLeft, func(*MouseDownEvent, *Window, *App) { f.events.add("down:root") }).
				OnMouseMove(func(*MouseMoveEvent, *Window, *App) { f.events.add("move:root") }).
				IntoElement()
		}
		cx.Notify()
	})
	f.draw()
	f.dragMove(captureOtherX, captureOtherY)
	if _, ok := f.window.CapturedHitbox(); ok {
		t.Fatal("the capture must drop when the captured element leaves the tree")
	}
	wantTranscript(t, "the move after the unmount", f.take(), []string{"move:root"})
}

// TestPointerCaptureHoverStylesFollowTheChain checks that the hover
// styles track the capture chain: the captured element keeps its hover
// style while the pointer is outside it (Chrome: the element under the
// pointer is the capture target, so :hover stays on it), and a sibling
// the pointer crosses does not pick one up.
func TestPointerCaptureHoverStylesFollowTheChain(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	hover := func(d *DivElement) *DivElement {
		return d.Bg(interactRed).Hover(func(s StyleRefinement) StyleRefinement {
			return Div().Refine(s).Bg(interactBlue).Style()
		})
	}
	view := NewEntity(app, window.Scope(), func(v *interactView, cx *Context[interactView]) {
		v.build = func(v *interactView, w *Window, cx *Context[interactView]) AnyElement {
			return Div().ID("root").SizeFull().Relative().
				Child(Div().ID("p").Absolute().Left(Px(100)).Top(Px(100)).Size(Px(200), Px(120)).
					Child(hover(Div().ID("cap").Absolute().Left(Px(0)).Top(Px(0)).Size(Px(80), Px(60)).
						OnMouseDownAll(func(e *MouseDownEvent, phase DispatchPhase, hitbox *Hitbox, w *Window, app *App) {
							if phase == DispatchBubble && hitbox.IsHovered(w) {
								w.CapturePointer(hitbox.ID())
							}
						})).IntoElement()).
					Child(hover(Div().ID("other").Absolute().Left(Px(80)).Top(Px(0)).Size(Px(80), Px(60))).IntoElement())).
				IntoElement()
		}
	})
	window.SetRootView(ViewOf(view))
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}

	colors := func() (red, blue int) {
		return quadCount(t, window, interactRed), quadCount(t, window, interactBlue)
	}
	red, blue := colors()
	if red != 2 || blue != 0 {
		t.Fatalf("initial colors r%d b%d, want both children un-hovered", red, blue)
	}

	window.DispatchInput(&MouseMoveEvent{Position: Point{X: captureCapX, Y: captureCapY}}, app)
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatal(err)
	}
	if red, blue = colors(); red != 1 || blue != 1 {
		t.Fatalf("hovered colors r%d b%d, want cap hovered", red, blue)
	}

	// Press and move outside cap, over other: cap keeps its hover style
	// (the capture chain), other does not pick one up.
	window.DispatchInput(&MouseDownEvent{Button: MouseButtonLeft, Position: Point{X: captureCapX, Y: captureCapY}, ClickCount: 1}, app)
	left := MouseButtonLeft
	window.DispatchInput(&MouseMoveEvent{Position: Point{X: captureOtherX, Y: captureOtherY}, PressedButton: &left}, app)
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatal(err)
	}
	if red, blue = colors(); red != 1 || blue != 1 {
		t.Fatalf("captured colors r%d b%d, want cap still hovered and other not", red, blue)
	}

	// The release settles the hover to the hit test: other hovered, cap
	// not.
	window.DispatchInput(&MouseUpEvent{Button: MouseButtonLeft, Position: Point{X: captureOtherX, Y: captureOtherY}, ClickCount: 1}, app)
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatal(err)
	}
	if red, blue = colors(); red != 1 || blue != 1 {
		t.Fatalf("released colors r%d b%d, want other hovered and cap not", red, blue)
	}
	// The last draw's hovered element is other: distinguish by a move
	// back over cap after the release.
	window.DispatchInput(&MouseMoveEvent{Position: Point{X: captureCapX, Y: captureCapY}}, app)
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatal(err)
	}
	if red, blue = colors(); red != 1 || blue != 1 {
		t.Fatalf("move back colors r%d b%d, want cap hovered again", red, blue)
	}
}
