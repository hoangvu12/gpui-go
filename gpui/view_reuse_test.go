package gpui

// browser-core ticket 59's tests: the automatic view reuse's render
// counts (the unchanged view not re-rendered while a sibling/parent
// change is), the accessibility tree's identity with and without
// reuse, the differential against the per-view opt-out (the pin's
// cached/uncached differential shape — scene, hitboxes, dispatch tree,
// cursor, focus, click delivery), the dependency invalidations
// (entities, globals, scroll versions), the motion engine's keep-going
// (a running transition inside a reused window keeps animating), the
// scene table-index gate (a view with a gradient background after a
// sibling whose gradient count changed paints the RIGHT gradient — the
// native replay's baked-index bug), and the opt-outs.

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
)

// vrRendersEqual compares two render-count slices.
func vrRendersEqual(got, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------------
// The fixtures
// ---------------------------------------------------------------------------

// vrPanel is one reused panel: a Flex+SizeFull root (the provable
// node-style shape), a hoverable button, a label, a focusable field and
// a model it renders from (the entity dependency).
type vrPanel struct {
	index   int
	label   string
	focus   FocusHandle
	counter Entity[vrCounter]
	renders int
}

func (p *vrPanel) Render(w *Window, cx *Context[vrPanel]) AnyElement {
	p.renders++
	model := vrClicksOf(cx.App(), p.counter)
	panel := Div().ID(fmt.Sprintf("panel-%d", p.index)).KeyContext(fmt.Sprintf("Panel%d", p.index)).
		SizeFull().Flex().FlexCol().Gap(Px(4)).P(Px(4)).
		Bg(Hsla{H: float32(p.index) / 8, S: 1, L: 0.3 + model.light/4, A: 1}).
		Child(Div().Flex().ID("button").W(Px(80)).H(Px(20)).Bg(Rgb(0x2563eb)).
			Hover(Styles().Bg(Rgb(0xdc2626))).CursorPointer().
			OnClick(func(*ClickEvent, *Window, *App) { model.clicks++ })).
		Child(Text(p.label)).
		Child(Div().Flex().ID("field").KeyContext("Field").TrackFocus(p.focus).W(Px(60)).H(Px(16)).
			Bg(Rgb(0x16a34a)).Focus(Styles().Bg(Rgb(0xf8fafc)))).
		Child(Div().Flex().ID("light").W(Px(60)).H(Px(8)).
			Bg(Hsla{H: 0.6, S: 1, L: 0.3 + model.light/2, A: 1}))
	return panel.IntoElement()
}

// vrCounter is the model the views render from: a click count and a
// lightness the tests mutate.
type vrCounter struct {
	clicks int
	light  float32
}

// vrClicksOf resolves the model entity, reading it through the frame's
// tracking (the read is the view's entity dependency).
func vrClicksOf(app *App, entity Entity[vrCounter]) *vrCounter {
	return entity.Read(app, func(c *vrCounter, _ *App) *vrCounter { return c })
}

// vrRoot lays the panels out in fixed cells, plain or opted out.
type vrRoot struct {
	panels  []Entity[vrPanel]
	counter Entity[vrCounter]
	optOut  bool
	renders int
}

func (r *vrRoot) Render(w *Window, cx *Context[vrRoot]) AnyElement {
	r.renders++
	_ = vrClicksOf(cx.App(), r.counter)
	grid := Div().KeyContext("Root").SizeFull().Flex().FlexRow().FlexWrap()
	for _, panel := range r.panels {
		view := ViewOf(panel)
		if r.optOut {
			view = WithoutViewReuse(view)
		}
		grid.Child(Div().Flex().W(Px(200)).H(Px(100)).Child(view))
	}
	return grid.IntoElement()
}

// vrWindow is one side of the differential/count tests.
type vrWindow struct {
	ta     *TestApp
	app    *App
	window *Window
	root   Entity[vrRoot]
	panels []Entity[vrPanel]
	focus  []FocusHandle
}

func newVRWindow(t *testing.T, ta *TestApp, optOut bool) *vrWindow {
	t.Helper()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 800, Height: 600})
	t.Cleanup(func() { CloseTestWindow(window) })
	side := &vrWindow{ta: ta, app: app, window: window}
	counter := NewEntity(app, window.Scope(), func(c *vrCounter, _ *Context[vrCounter]) {
		c.light = 0.4
	})
	for i := 0; i < 6; i++ {
		focus := app.FocusHandle()
		side.focus = append(side.focus, focus)
		side.panels = append(side.panels, NewEntity(app, window.Scope(), func(p *vrPanel, _ *Context[vrPanel]) {
			p.index, p.label, p.focus, p.counter = i, fmt.Sprintf("panel %d", i), focus, counter
		}))
	}
	side.root = NewEntity(app, window.Scope(), func(r *vrRoot, _ *Context[vrRoot]) {
		r.panels, r.counter, r.optOut = side.panels, counter, optOut
	})
	window.SetRootView(ViewOf(side.root))
	ta.RunUntilParked()
	return side
}

func (s *vrWindow) panelRenders() []int {
	out := make([]int, len(s.panels))
	for i, panel := range s.panels {
		out[i] = panel.Read(s.app, func(p *vrPanel, _ *App) int { return p.renders })
	}
	return out
}

func (s *vrWindow) rootRenders() int {
	return s.root.Read(s.app, func(r *vrRoot, _ *App) int { return r.renders })
}

// notifyRoot re-renders the whole frame through the root (the pin's
// "a notification of the root alone reuses the children").
func (s *vrWindow) notifyRoot() {
	s.root.Update(s.app, func(r *vrRoot, cx *Context[vrRoot]) { cx.Notify() })
	s.ta.RunUntilParked()
}

// ---------------------------------------------------------------------------
// Acceptance (1): render counts
// ---------------------------------------------------------------------------

// TestViewReuseRenderCounts pins the ticket's first acceptance: an
// unchanged view is not re-rendered while a sibling's change is (the
// changed panel and the root render; the other five panels do not),
// and the root's own change re-renders the root but no panel. The
// frames run through the frame loop (the notify → flush draw path),
// not forced clean draws.
func TestViewReuseRenderCounts(t *testing.T) {
	ta := NewTestApp()
	side := newVRWindow(t, ta, false)
	app := side.app

	// The first frame rendered everything once.
	if got := side.panelRenders(); !vrRendersEqual(got, []int{1, 1, 1, 1, 1, 1}) {
		t.Fatalf("first frame renders = %v, want all 1", got)
	}
	if side.rootRenders() != 1 {
		t.Fatalf("first frame root renders = %d, want 1", side.rootRenders())
	}

	// A panel change: that panel and the root render, the others reuse.
	side.panels[2].Update(app, func(p *vrPanel, cx *Context[vrPanel]) {
		p.label = "panel 2 edited"
		cx.Notify()
	})
	ta.RunUntilParked()
	if got := side.panelRenders(); !vrRendersEqual(got, []int{1, 1, 2, 1, 1, 1}) {
		t.Fatalf("sibling change renders = %v, want [1 1 2 1 1 1]", got)
	}
	if side.rootRenders() != 2 {
		t.Fatalf("sibling change root renders = %d, want 2 (the ancestor rule)", side.rootRenders())
	}

	// The root's own change: the root renders, no panel does.
	side.notifyRoot()
	if got := side.panelRenders(); !vrRendersEqual(got, []int{1, 1, 2, 1, 1, 1}) {
		t.Fatalf("root change renders = %v, want the panels unchanged", got)
	}
	if side.rootRenders() != 3 {
		t.Fatalf("root change root renders = %d, want 3", side.rootRenders())
	}

	// A MODEL the panels read: the reading panels render (the automatic
	// path's dependency index — the pin left model notifications to the
	// observe-and-notify discipline; the default-on reuse cannot).
	side.root.Read(app, func(r *vrRoot, _ *App) Entity[vrCounter] { return r.counter }).Update(app, func(c *vrCounter, cx *Context[vrCounter]) {
		c.light = 0.8
		cx.Notify()
	})
	ta.RunUntilParked()
	if got := side.panelRenders(); !vrRendersEqual(got, []int{2, 2, 3, 2, 2, 2}) {
		t.Fatalf("model change renders = %v, want every panel re-rendered", got)
	}
}

// TestViewReuseForcedCleanDrawRendersFresh pins the explicit-draw rule
// (browser-core 59): a clean window's forced DrawWindowFrame re-renders
// every view — the pin's draw semantics for the hand-drawn frame —
// while the frame loop's dirty draws keep the reuse.
func TestViewReuseForcedCleanDrawRendersFresh(t *testing.T) {
	ta := NewTestApp()
	side := newVRWindow(t, ta, false)
	if _, err := DrawWindowFrame(side.window); err != nil {
		t.Fatal(err)
	}
	if got := side.panelRenders(); !vrRendersEqual(got, []int{2, 2, 2, 2, 2, 2}) {
		t.Fatalf("forced clean draw renders = %v, want all 2 (the pin's re-render)", got)
	}
}

// TestViewReuseOptOut pins the opt-outs: the per-view wrapper renders
// fresh every frame while its siblings reuse, and the per-window switch
// turns the whole window off.
func TestViewReuseOptOut(t *testing.T) {
	ta := NewTestApp()
	side := newVRWindow(t, ta, true)
	side.notifyRoot()
	// The opt-out side re-renders every panel on the root's change.
	if got := side.panelRenders(); !vrRendersEqual(got, []int{2, 2, 2, 2, 2, 2}) {
		t.Fatalf("opt-out renders = %v, want all 2 (no reuse)", got)
	}

	// The per-window switch: flip a reuse window off and back.
	other := newVRWindow(t, ta, false)
	SetWindowViewReuse(other.window, false)
	other.notifyRoot()
	if got := other.panelRenders(); !vrRendersEqual(got, []int{2, 2, 2, 2, 2, 2}) {
		t.Fatalf("window opt-out renders = %v, want all 2", got)
	}
}

// ---------------------------------------------------------------------------
// Acceptance (2): the accessibility tree
// ---------------------------------------------------------------------------

// vraPanel is one a11y-carrying panel: a group role with a labeled
// button child carrying an action listener, a focusable field and
// synthetic children.
type vraPanel struct {
	index   int
	label   string
	focus   FocusHandle
	actions int
	renders int
}

func (p *vraPanel) Render(w *Window, cx *Context[vraPanel]) AnyElement {
	p.renders++
	return Div().ID(fmt.Sprintf("panel-%d", p.index)).SizeFull().Flex().FlexCol().
		Role(RoleGroup).AriaLabel(fmt.Sprintf("panel %d", p.index)).
		Child(Div().ID("button").W(Px(80)).H(Px(20)).Role(RoleButton).AriaLabel("do").
			OnA11yAction(A11yActionClick, func(*A11yActionData, *Window, *App) { p.actions++ })).
		Child(Div().ID("field").TrackFocus(p.focus).W(Px(60)).H(Px(16)).
			Role(RoleTextInput).AriaLabel("edit")).
		Child(Div().ID("caption").W(Px(80)).H(Px(16)).
			A11ySyntheticChildren(func(builder *A11ySubtree) {
				builder.PushChild(builder.SyntheticNodeID(0), A11ySpec{Role: RoleStaticText, Label: "one"})
				builder.PushChild(builder.SyntheticNodeID(1), A11ySpec{Role: RoleStaticText, Label: "two"})
			})).
		Child(Text(p.label)).
		IntoElement()
}

type vraRoot struct {
	panels []Entity[vraPanel]
}

func (r *vraRoot) Render(w *Window, cx *Context[vraRoot]) AnyElement {
	grid := Div().KeyContext("Root").SizeFull().Flex().FlexRow()
	for _, panel := range r.panels {
		grid.Child(Div().Flex().W(Px(200)).H(Px(100)).Child(ViewOf(panel)))
	}
	return grid.IntoElement()
}

// vraSnapshot renders one window's published a11y tree into a
// comparable string: every node with its role, label, children, bounds
// and actions (the tree identity), plus the focus. The node ids and
// focus-handle ids are per-window values (the ids derive from the
// element paths, which carry the entity ids), so the dump keys the
// nodes by their traversal index and records only whether a focus
// handle is bound; the sequence and generation are per-publication
// counters.
func vraSnapshot(t *testing.T, w *Window) string {
	t.Helper()
	snapshot, err := PublishWindowA11y(w)
	if err != nil {
		t.Fatalf("PublishWindowA11y: %v", err)
	}
	nodes := snapshot.Nodes()
	index := map[A11yNodeID]int{}
	for i, node := range nodes {
		index[node.ID] = i
	}
	var b strings.Builder
	var write func(id A11yNodeID, indent string)
	write = func(id A11yNodeID, indent string) {
		slot, ok := index[id]
		if !ok {
			return
		}
		node := nodes[slot]
		actions := make([]string, 0, len(node.Actions))
		for _, action := range node.Actions {
			actions = append(actions, action.String())
		}
		sort.Strings(actions)
		handle := 0
		if node.FocusHandleID != 0 {
			handle = 1
		}
		var children []int
		for _, child := range node.Children {
			if slot, ok := index[child]; ok {
				children = append(children, slot)
			}
		}
		fmt.Fprintf(&b, "%s#%d role=%s label=%q children=%v bounds=%v actions=%v handle=%d\n",
			indent, slot, node.Role, node.Label, children, node.Bounds, actions, handle)
		for _, child := range node.Children {
			write(child, indent+"  ")
		}
	}
	write(snapshot.Root(), "")
	focus := 0
	if slot, ok := index[snapshot.Focus()]; ok {
		focus = slot
	}
	fmt.Fprintf(&b, "focus=%d\n", focus)
	return b.String()
}

func TestViewReuseA11yTreeIdentity(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	newSide := func(reuse bool) (*Window, Entity[vraPanel], []Entity[vraPanel]) {
		window := NewTestWindow(app, Size{Width: 800, Height: 600})
		t.Cleanup(func() { CloseTestWindow(window) })
		SetWindowViewReuse(window, reuse)
		var panels []Entity[vraPanel]
		for i := 0; i < 4; i++ {
			focus := app.FocusHandle()
			panels = append(panels, NewEntity(app, window.Scope(), func(p *vraPanel, _ *Context[vraPanel]) {
				p.index, p.label, p.focus = i, fmt.Sprintf("panel %d", i), focus
			}))
		}
		root := NewEntity(app, window.Scope(), func(r *vraRoot, _ *Context[vraRoot]) { r.panels = panels })
		window.SetRootView(ViewOf(root))
		return window, panels[0], panels
	}
	reuseW, _, reusePanels := newSide(true)
	plainW, _, plainPanels := newSide(false)
	SetA11yWindowTitle(reuseW, "App")
	SetA11yWindowTitle(plainW, "App")

	ActivateWindowA11y(reuseW)
	ActivateWindowA11y(plainW)
	ta.RunUntilParked()
	if got, want := vraSnapshot(t, reuseW), vraSnapshot(t, plainW); got != want {
		t.Fatalf("a11y trees differ after activation:\nreuse:\n%s\nplain:\n%s", got, want)
	}

	// One panel changes; the others replay their recorded a11y ranges.
	for _, panels := range [][]Entity[vraPanel]{reusePanels, plainPanels} {
		panels[1].Update(app, func(p *vraPanel, cx *Context[vraPanel]) {
			p.label = "panel 1 edited"
			cx.Notify()
		})
	}
	ta.RunUntilParked()
	if got, want := vraSnapshot(t, reuseW), vraSnapshot(t, plainW); got != want {
		t.Fatalf("a11y trees differ after the partial update:\nreuse:\n%s\nplain:\n%s", got, want)
	}

	// The focus moves (a refresh — both sides render fresh), then a
	// change whose siblings reuse with the focus inside one of them.
	reusePanels[2].Read(app, func(p *vraPanel, _ *App) FocusHandle { return p.focus }).Focus(reuseW)
	plainPanels[2].Read(app, func(p *vraPanel, _ *App) FocusHandle { return p.focus }).Focus(plainW)
	ta.RunUntilParked()
	if got, want := vraSnapshot(t, reuseW), vraSnapshot(t, plainW); got != want {
		t.Fatalf("a11y trees differ after the focus move:\nreuse:\n%s\nplain:\n%s", got, want)
	}
	for _, panels := range [][]Entity[vraPanel]{reusePanels, plainPanels} {
		panels[3].Update(app, func(p *vraPanel, cx *Context[vraPanel]) { cx.Notify() })
	}
	ta.RunUntilParked()
	if got, want := vraSnapshot(t, reuseW), vraSnapshot(t, plainW); got != want {
		t.Fatalf("a11y trees differ with the focus inside a reused panel:\nreuse:\n%s\nplain:\n%s", got, want)
	}

	// The a11y actions still route on the reused side (the replayed
	// listeners): the focused panel's button node (located structurally
	// through the tree, its id deriving from the full element path).
	snapshot, _ := LastA11ySnapshot(reuseW)
	buttonNodeID, buttonFound := func() (A11yNodeID, bool) {
		for _, node := range snapshot.Nodes() {
			if node.Role != RoleGroup || node.Label != "panel 2" {
				continue
			}
			for _, child := range node.Children {
				for _, candidate := range snapshot.Nodes() {
					if candidate.ID == child && candidate.Role == RoleButton && candidate.Label == "do" {
						return candidate.ID, true
					}
				}
			}
		}
		return 0, false
	}()
	if !buttonFound {
		t.Fatal("the reused panel's button node is not in the tree")
	}
	id := buttonNodeID
	token, err := snapshot.Token(id)
	if err != nil {
		t.Fatalf("minting the token: %v", err)
	}
	resolved, err := ResolveA11yToken(app, token)
	if err != nil {
		t.Fatalf("resolving the token: %v", err)
	}
	if err := DispatchA11yAction(app, resolved, A11yActionClick, nil); err != nil {
		t.Fatalf("DispatchA11yAction on a reused node: %v", err)
	}
	if actions := reusePanels[2].Read(app, func(p *vraPanel, _ *App) int { return p.actions }); actions != 1 {
		t.Fatalf("reused node actions = %d, want 1 (the replayed listener fired)", actions)
	}
}

// ---------------------------------------------------------------------------
// The differential: automatic reuse vs the per-view opt-out
// ---------------------------------------------------------------------------

// vrFrameDump renders one window's last frame into comparable text (the
// pin's cached/uncached differential shape): the quads, the hitboxes
// and the hit test, the dispatch tree, the cursor and the focus.
func vrFrameDump(t *testing.T, side *vrWindow) string {
	t.Helper()
	w := side.window
	var b strings.Builder
	scene := LastDrawnScene(w)
	quads, err := scene.Quads()
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range quads {
		fmt.Fprintf(&b, "quad order %d bounds %v bg kind %d\n", q.Order, q.Bounds, q.Background.Kind)
	}
	rendered := focusState(w).rendered
	position := map[uint64]int{}
	for i, record := range rendered.hitboxes {
		position[record.box.id] = i
		fmt.Fprintf(&b, "hitbox %d %v behavior %v\n", i, record.box.Bounds, record.box.behavior)
	}
	for y := float32(5); y < 220; y += 9 {
		for x := float32(5); x < 800; x += 13 {
			test := rendered.hitTestAt(Point{X: x, Y: y})
			var hits []int
			for _, id := range test.orderedIDs {
				hits = append(hits, position[id])
			}
			fmt.Fprintf(&b, "hit %v,%v %v/%d\n", x, y, hits, test.hoverHitboxCount)
		}
	}
	tree := rendered.dispatchTree
	for i, node := range tree.nodes {
		fmt.Fprintf(&b, "node %d parent %v/%v view %v listeners %d/%d\n", i,
			node.parent.Index, node.hasParent, node.hasViewID, len(node.keyListeners), len(node.actionListeners))
	}
	cursor, ok := WindowCursorStyle(w)
	fmt.Fprintf(&b, "cursor %v %v\n", cursor, ok)
	focused := -1
	if result := w.Focused(); result.Ok {
		for i, handle := range side.focus {
			if handle.id == result.Handle.id {
				focused = i
			}
		}
	}
	fmt.Fprintf(&b, "focused panel %d\n", focused)
	return b.String()
}

// TestViewReuseDifferential drives a reuse window and an opt-out window
// through the same frames — partial updates, hovers, clicks, focus
// moves, a model change and a refresh — and requires identical scenes,
// hit testing, dispatch trees, cursors, focus and click delivery.
func TestViewReuseDifferential(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	reuse := newVRWindow(t, ta, false)
	optOut := newVRWindow(t, ta, true)

	compare := func(step string) {
		t.Helper()
		got, want := vrFrameDump(t, reuse), vrFrameDump(t, optOut)
		if got != want {
			t.Fatalf("%s: reuse and opt-out frames differ:\nreuse:\n%s\nopt-out:\n%s", step, got, want)
		}
	}
	compare("first frame")

	// A partial update: panel 2's label; the others reuse.
	for _, side := range []*vrWindow{reuse, optOut} {
		side.panels[2].Update(app, func(p *vrPanel, cx *Context[vrPanel]) {
			p.label = "panel 2 edited"
			cx.Notify()
		})
	}
	ta.RunUntilParked()
	compare("partial update")

	// A platform click arrives after the pointer moved there.
	click := func(side *vrWindow, at Point) {
		side.window.DispatchInput(&MouseMoveEvent{Position: at, Modifiers: Modifiers{}}, side.app)
		side.window.DispatchInput(&MouseDownEvent{Position: at, Button: MouseButtonLeft, ClickCount: 1}, side.app)
		side.window.DispatchInput(&MouseUpEvent{Position: at, Button: MouseButtonLeft, ClickCount: 1}, side.app)
	}
	for _, side := range []*vrWindow{reuse, optOut} {
		click(side, Point{X: 430, Y: 15})
	}
	ta.RunUntilParked()
	compare("click panel 2 button")
	if clicks := reuse.counter0().Read(app, func(c *vrCounter, _ *App) int { return c.clicks }); clicks != 1 {
		t.Fatalf("reuse side clicks = %d, want 1", clicks)
	}
	if clicks := optOut.counter0().Read(app, func(c *vrCounter, _ *App) int { return c.clicks }); clicks != 1 {
		t.Fatalf("opt-out side clicks = %d, want 1", clicks)
	}

	// The focus moves (a refresh) and a reuse follows with the focus
	// inside a reused panel.
	for _, side := range []*vrWindow{reuse, optOut} {
		side.focus[4].Focus(side.window)
	}
	ta.RunUntilParked()
	compare("focus panel 4")
	for _, side := range []*vrWindow{reuse, optOut} {
		side.root.Update(app, func(r *vrRoot, cx *Context[vrRoot]) { cx.Notify() })
	}
	ta.RunUntilParked()
	compare("reuse with focus inside a reused panel")

	// A model change re-renders every panel (the dependency index).
	for _, side := range []*vrWindow{reuse, optOut} {
		side.counter0().Update(app, func(c *vrCounter, cx *Context[vrCounter]) {
			c.light = 0.9
			cx.Notify()
		})
	}
	ta.RunUntilParked()
	compare("model change")

	// A window refresh renders everything fresh on both sides.
	for _, side := range []*vrWindow{reuse, optOut} {
		side.window.Refresh()
	}
	ta.RunUntilParked()
	compare("window refresh")
}

// counter0 returns the side's model entity (the root's record).
func (s *vrWindow) counter0() Entity[vrCounter] {
	return s.root.Read(s.app, func(r *vrRoot, _ *App) Entity[vrCounter] { return r.counter })
}

// ---------------------------------------------------------------------------
// The scroll-handle version
// ---------------------------------------------------------------------------

// TestViewReuseScrollVersionRerenders pins the scroll version rule: a
// programmatic SetOffset (unnotified — the pin's "the scrollable
// element clamps it at its next prepaint") refuses the reuse of a view
// that read the handle, so the clamp re-runs and the offset holds.
func TestViewReuseScrollVersionRerenders(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	handle := NewScrollHandle()
	window := NewTestWindow(app, Size{Width: 400, Height: 300})
	t.Cleanup(func() { CloseTestWindow(window) })
	root := NewEntity(app, window.Scope(), func(v *scrollVersionView, _ *Context[scrollVersionView]) {
		v.handle = handle
	})
	window.SetRootView(ViewOf(root))
	ta.RunUntilParked()
	renders := root.Read(app, func(v *scrollVersionView, _ *App) int { return v.renders })
	handle.SetOffset(Point{X: 0, Y: -120})
	ta.RunUntilParked()
	// The window is not dirty (nothing notified): the forced draw is
	// the pin's contract for a programmatic scroll. The version check
	// makes the view render fresh instead of replaying the stale
	// offset.
	if _, err := DrawWindowFrame(window); err != nil {
		t.Fatal(err)
	}
	if got := root.Read(app, func(v *scrollVersionView, _ *App) int { return v.renders }); got != renders+1 {
		t.Fatalf("SetOffset without a notify: renders = %d, want %d (the version refused the reuse)", got, renders+1)
	}
	if got := handle.Offset(); got.Y != -120 {
		t.Fatalf("offset = %v, want (0,-120)", got)
	}
}

type scrollVersionView struct {
	handle  ScrollHandle
	renders int
}

func (v *scrollVersionView) Render(w *Window, cx *Context[scrollVersionView]) AnyElement {
	v.renders++
	return Div().Flex().ID("root").SizeFull().Child(
		Div().Flex().ID("scroller").Size(Px(240), Px(300)).FlexCol().OverflowYScroll().TrackScroll(v.handle).
			Child(Div().Flex().ID("content").H(Px(2000)).WFull().FlexShrink0()),
	).IntoElement()
}

// ---------------------------------------------------------------------------
// The global dependency
// ---------------------------------------------------------------------------

type vrGlobal struct {
	Theme string
}

type vrGlobalView struct {
	renders int
}

func (v *vrGlobalView) Render(w *Window, cx *Context[vrGlobalView]) AnyElement {
	v.renders++
	theme := Global[vrGlobal](cx.App()).Theme
	return Div().Flex().ID("root").SizeFull().AriaLabel(theme).IntoElement()
}

// TestViewReuseGlobalNotifyRefreshes pins the global dependency: a view
// that reads a global during its render re-renders when the global is
// replaced (a NotifyGlobal), even though nothing was notified.
func TestViewReuseGlobalNotifyRefreshes(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	app.Update(func(app *App) { app.SetGlobal(vrGlobal{Theme: "light"}) })
	window := NewTestWindow(app, Size{Width: 300, Height: 200})
	t.Cleanup(func() { CloseTestWindow(window) })
	root := NewEntity(app, window.Scope(), func(v *vrGlobalView, _ *Context[vrGlobalView]) {})
	window.SetRootView(ViewOf(root))
	ta.RunUntilParked()
	renders := root.Read(app, func(v *vrGlobalView, _ *App) int { return v.renders })

	app.Update(func(app *App) { app.SetGlobal(vrGlobal{Theme: "dark"}) })
	ta.RunUntilParked()
	if got := root.Read(app, func(v *vrGlobalView, _ *App) int { return v.renders }); got != renders+1 {
		t.Fatalf("global notify: renders = %d, want %d (the reading window refreshed)", got, renders+1)
	}
}

// ---------------------------------------------------------------------------
// The motion engine (tickets 44/61): an animating view keeps animating
// ---------------------------------------------------------------------------

type vrMotionView struct {
	wide    bool
	renders int
}

func (v *vrMotionView) Render(w *Window, cx *Context[vrMotionView]) AnyElement {
	v.renders++
	width := Px(100)
	if v.wide {
		width = Px(200)
	}
	return Div().Flex().ID("root").SizeFull().Child(
		Div().Flex().ID("animated").W(width).H(Px(40)).
			Transition(NewTransitionRule(TransitionAll, 200*time.Millisecond)).
			DebugSelector("animated").
			Bg(Rgb(0x2563eb)),
	).IntoElement()
}

// TestViewReuseTransitionKeepsAnimating pins the motion rule: a running
// transition inside a (would-be-reused) view re-renders every frame —
// the transition's animation-frame request notifies the view — and the
// width lands at its target. A frozen reuse would stall at the start.
func TestViewReuseTransitionKeepsAnimating(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 300, Height: 200})
	t.Cleanup(func() { CloseTestWindow(window) })
	root := NewEntity(app, window.Scope(), func(v *vrMotionView, _ *Context[vrMotionView]) {})
	window.SetRootView(ViewOf(root))
	ta.RunUntilParked()
	if b, ok := WindowDebugBound(window, "animated"); !ok || b.Size.Width != 100 {
		t.Fatalf("animated bounds = %v %v, want width 100", b, ok)
	}

	// Start the transition and let exactly half of it run.
	root.Update(app, func(v *vrMotionView, cx *Context[vrMotionView]) {
		v.wide = true
		cx.Notify()
	})
	ta.RunUntilParked()
	ta.AdvanceClock(100)
	window.SimulateFrameRequest()
	ta.RunUntilParked()
	b, ok := WindowDebugBound(window, "animated")
	if !ok || b.Size.Width < 110 || b.Size.Width > 190 {
		t.Fatalf("animated width mid-transition = %v %v, want ~150", b, ok)
	}
	renders := root.Read(app, func(v *vrMotionView, _ *App) int { return v.renders })
	if renders < 2 {
		t.Fatalf("renders = %d, want the animating view re-rendering each frame", renders)
	}

	ta.AdvanceClock(150)
	window.SimulateFrameRequest()
	ta.RunUntilParked()
	if b, _ := WindowDebugBound(window, "animated"); b.Size.Width != 200 {
		t.Fatalf("animated width after the transition = %v, want 200", b.Size.Width)
	}
}

// ---------------------------------------------------------------------------
// The scene table-index gate (browser-core 57's tables under reuse)
// ---------------------------------------------------------------------------

// vrGradientSibling is the sibling whose gradient TABLE row count
// changes: its enabled state adds one more n-stop (table) gradient
// BEFORE the panel in paint order, shifting the gradient table the
// panel's replayed quad references by its baked index.
type vrGradientSibling struct {
	enabled bool
	renders int
}

func (v *vrGradientSibling) Render(w *Window, cx *Context[vrGradientSibling]) AnyElement {
	v.renders++
	root := Div().Flex().ID("sibling").W(Px(100)).H(Px(20)).
		Bg(LinearGradientToBackground(GradientDirectionToRight,
			GradientStop{Color: Hsla{H: 0, S: 1, L: 0.5, A: 1}, Position: 0},
			GradientStop{Color: Hsla{H: 0.6, S: 1, L: 0.5, A: 1}, Position: 1}))
	if v.enabled {
		root.Child(Div().Flex().ID("extra").W(Px(50)).H(Px(10)).
			Bg(LinearGradientStopsBackground(0,
				GradientStop{Color: Hsla{H: 0.2, S: 1, L: 0.5, A: 1}, Position: 0},
				GradientStop{Color: Hsla{H: 0.5, S: 1, L: 0.5, A: 1}, Position: 0.5},
				GradientStop{Color: Hsla{H: 0.8, S: 1, L: 0.5, A: 1}, Position: 1},
			)))
	}
	return root.IntoElement()
}

// vrGradientPanel is the panel with an n-stop (table) gradient
// background — the arm whose quads reference the gradient table by the
// baked index.
type vrGradientPanel struct {
	renders int
}

func (p *vrGradientPanel) Render(w *Window, cx *Context[vrGradientPanel]) AnyElement {
	p.renders++
	return Div().Flex().ID("panel").SizeFull().
		Bg(LinearGradientStopsBackground(90,
			GradientStop{Color: Hsla{H: 0, S: 1, L: 0.5, A: 1}, Position: 0},
			GradientStop{Color: Hsla{H: 0.5, S: 1, L: 0.5, A: 1}, Position: 0.25},
			GradientStop{Color: Hsla{H: 0.9, S: 1, L: 0.5, A: 1}, Position: 0.75},
			GradientStop{Color: Hsla{H: 1.0 / 6.0, S: 1, L: 0.5, A: 1}, Position: 1},
		)).IntoElement()
}

type vrGradientRoot struct {
	sibling Entity[vrGradientSibling]
	panel   Entity[vrGradientPanel]
	optOut  bool
}

func (r *vrGradientRoot) Render(w *Window, cx *Context[vrGradientRoot]) AnyElement {
	panel := ViewOf(r.panel)
	if r.optOut {
		panel = WithoutViewReuse(panel)
	}
	return Div().Flex().ID("root").SizeFull().FlexCol().
		Child(ViewOf(r.sibling)).
		Child(Div().Flex().W(Px(200)).H(Px(100)).Child(panel)).IntoElement()
}

// TestViewReuseGradientTableStaysRight pins the native replay's baked
// table-index rule (view_reuse.go viewReuseTableSafe): a view whose
// recorded paint range inserts or references scene table rows — an
// n-stop gradient background — never reuses, so the sibling's changing
// gradient count cannot shift the table under the panel's baked
// indices. The panel's quad must reference the row carrying ITS stops
// after the change.
func TestViewReuseGradientTableStaysRight(t *testing.T) {
	ta := NewTestApp()
	app := ta.App()
	newSide := func(optOut bool) (*Window, Entity[vrGradientRoot]) {
		window := NewTestWindow(app, Size{Width: 300, Height: 200})
		t.Cleanup(func() { CloseTestWindow(window) })
		sibling := NewEntity(app, window.Scope(), func(v *vrGradientSibling, _ *Context[vrGradientSibling]) {})
		panel := NewEntity(app, window.Scope(), func(p *vrGradientPanel, _ *Context[vrGradientPanel]) {})
		root := NewEntity(app, window.Scope(), func(r *vrGradientRoot, _ *Context[vrGradientRoot]) {
			r.sibling, r.panel, r.optOut = sibling, panel, optOut
		})
		window.SetRootView(ViewOf(root))
		return window, root
	}
	reuseW, reuseRoot := newSide(false)
	optOutW, optOutRoot := newSide(true)
	ta.RunUntilParked()

	// Enable the sibling's second gradient: the table gains a row
	// BEFORE the panel's, so the panel's row index would shift under a
	// replayed (stale) quad.
	for _, root := range []Entity[vrGradientRoot]{reuseRoot, optOutRoot} {
		root.Read(app, func(r *vrGradientRoot, _ *App) Entity[vrGradientSibling] { return r.sibling }).
			Update(app, func(v *vrGradientSibling, cx *Context[vrGradientSibling]) {
				v.enabled = true
				cx.Notify()
			})
	}
	ta.RunUntilParked()

	// The panel's quad references the gradient table; the row it names
	// must carry the panel's four stops (the sibling's gradients carry
	// two).
	checkPanel := func(w *Window, label string) {
		t.Helper()
		scene := LastDrawnScene(w)
		quads, err := scene.Quads()
		if err != nil {
			t.Fatal(err)
		}
		// The panel's quad is the 200x100 logical (400x200 device) box
		// under the 20-logical sibling — the only table quad of that
		// geometry (the sibling's extra is 50x10 logical).
		panelBox := Bounds{Origin: Point{X: 0, Y: 40}, Size: Size{Width: 400, Height: 200}}
		var panelQuad *Quad
		for i := range quads {
			if quads[i].Background.Kind == BackgroundKindGradientTable && quads[i].Bounds == panelBox {
				panelQuad = &quads[i]
			}
		}
		if panelQuad == nil {
			t.Fatalf("%s: no gradient-table quad found for the panel", label)
		}
		records, _, err := scene.kernel.scene.GradientDump()
		if err != nil {
			t.Fatal(err)
		}
		index := panelQuad.Background.GradientTable
		if int(index) >= len(records) {
			t.Fatalf("%s: the panel quad references gradient row %d past the table (%d rows)", label, index, len(records))
		}
		if count := records[index].StopCount; count != 4 {
			t.Fatalf("%s: the panel quad references gradient row %d with %d stops, want its own 4-stop row (a stale baked index)", label, index, count)
		}
	}
	checkPanel(reuseW, "reuse")
	checkPanel(optOutW, "opt-out")
}

// TestSceneReplayCountsCopiedPaths pins the kernel's pathCount fix: a
// replayed range's path ops advance the destination kernel's counters
// (the coverage seam's nextPathIndex reads pathCount), so a coverage
// row inserted after a replayed range references the right path.
func TestSceneReplayCountsCopiedPaths(t *testing.T) {
	prev, err := NewScene()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = prev.Dispose() }()
	full := Bounds{Origin: Point{}, Size: Size{Width: 300, Height: 300}}
	triangle := func(x float32) *Path {
		builder := NewPathBuilder()
		builder.MoveTo(x, 0).LineTo(x+10, 0).LineTo(x+5, 10).Close()
		path, err := builder.Build()
		if err != nil {
			t.Fatal(err)
		}
		return path
	}
	for i := 0; i < 2; i++ {
		if err := prev.InsertPath(triangle(float32(i*20)), SolidBackground(Hsla{A: 1}), full); err != nil {
			t.Fatal(err)
		}
	}
	if err := prev.Finish(); err != nil {
		t.Fatal(err)
	}

	next, err := NewScene()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = next.Dispose() }()
	if err := next.Replay(0, 2, prev); err != nil {
		t.Fatal(err)
	}
	if got := next.NextPathIndex(); got != 2 {
		t.Fatalf("pathCount after the replay = %d, want 2 (the copied paths counted)", got)
	}
	// A fresh path after the replayed range continues the count.
	if err := next.InsertPath(triangle(100), SolidBackground(Hsla{A: 1}), full); err != nil {
		t.Fatal(err)
	}
	if got := next.NextPathIndex(); got != 3 {
		t.Fatalf("pathCount after the fresh insert = %d, want 3", got)
	}
}
