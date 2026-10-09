package accessspec

import (
	"testing"

	"gpui-go/gpui"
)

// The accessibility corpus (ticket21): a real counter/editor drawn
// through the element phases, with the semantic tree published and
// actions routed through generation-checked foreground tokens. The
// fixtures in fixtures.go record the exact traces this corpus produces;
// they are PORT-RECORDED golden fixtures (the reference harness has no
// a11y trace fixture in this repository), pinned by determinism: the
// node ids derive from element id paths only, so the traces are stable
// across runs.

// a11yHandlerRuns counts listener invocations independent of entity
// lifetime, proving stale dispatch never runs handlers.
var a11yHandlerRuns int

// a11yFixture is one test window with its counter.
type a11yFixture struct {
	testApp *gpui.TestApp
	app     *gpui.App
	window  *gpui.Window
	counter gpui.Entity[Counter]
}

// newA11yFixture builds the app, window, counter and root view.
func newA11yFixture(t *testing.T, size gpui.Size) *a11yFixture {
	t.Helper()
	testApp := gpui.NewTestApp()
	app := testApp.App()
	w := gpui.NewTestWindow(app, size)
	gpui.SetA11yWindowTitle(w, "Counter App")
	counter := newCounter(app, w)
	w.SetRootView(&counterRootView{counter: counter})
	return &a11yFixture{testApp: testApp, app: app, window: w, counter: counter}
}

// activate turns the a11y runtime on and returns the minimal activation
// snapshot (the pinned activation pattern: minimal root immediately, a
// refresh posted, the complete update on the next draw).
func (f *a11yFixture) activate(t *testing.T) *gpui.A11ySnapshot {
	t.Helper()
	if _, err := gpui.PublishWindowA11y(f.window); err == nil {
		t.Fatal("publishing before activation must fail")
	}
	minimal := gpui.ActivateWindowA11y(f.window)
	if minimal == nil {
		t.Fatal("activation returned no snapshot")
	}
	return minimal
}

// drawAndPublish draws one frame and publishes the accessibility tree.
func (f *a11yFixture) drawAndPublish(t *testing.T) *gpui.A11ySnapshot {
	t.Helper()
	if _, err := gpui.DrawWindowFrame(f.window); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	snapshot, err := gpui.PublishWindowA11y(f.window)
	if err != nil {
		t.Fatalf("PublishWindowA11y: %v", err)
	}
	return snapshot
}

// update applies one model mutation through the entity.
func (f *a11yFixture) update(t *testing.T, mutate func(c *Counter, cx *gpui.Context[Counter])) {
	t.Helper()
	f.counter.Update(f.app, mutate)
}

// facts reads the model observables.
func (f *a11yFixture) facts(t *testing.T) Counter {
	t.Helper()
	return f.counter.Read(f.app, func(c *Counter, _ *gpui.App) Counter { return *c })
}

// tokenOf mints and resolves an action token for one node of the
// snapshot (generation-checked: the node generation is pinned against
// the live identity model).
func tokenOf(t *testing.T, app *gpui.App, snapshot *gpui.A11ySnapshot, node gpui.A11yNodeID) gpui.A11yToken {
	t.Helper()
	token, err := snapshot.Token(node)
	if err != nil {
		t.Fatalf("minting token for %s: %v", node, err)
	}
	if _, err := gpui.ResolveA11yToken(nil, token); err == nil {
		t.Fatal("resolving a token without an app must fail")
	}
	resolved, err := gpui.ResolveA11yToken(app, token)
	if err != nil {
		t.Fatalf("resolving token for %s: %v", node, err)
	}
	return resolved
}

// dispatch routes one action through the foreground dispatcher.
func (f *a11yFixture) dispatch(t *testing.T, token gpui.A11yToken, action gpui.A11yAction, data *gpui.A11yActionData) error {
	t.Helper()
	return gpui.DispatchA11yAction(f.app, token, action, data)
}

// nodeByAuthor finds a published node by its author id.
func nodeByAuthor(t *testing.T, snapshot *gpui.A11ySnapshot, author string) gpui.A11ySnapshotNode {
	t.Helper()
	for _, node := range snapshot.Nodes() {
		if node.AuthorID == author {
			return node
		}
	}
	t.Fatalf("no published node with author id %q in %s", author, snapshot.Trace())
	return gpui.A11ySnapshotNode{}
}

// TestActivationMinimalRootThenCompleteTree pins the activation pattern:
// a minimal valid root is returned immediately, a refresh is requested,
// and the next draw publishes the complete update.
func TestActivationMinimalRootThenCompleteTree(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newA11yFixture(t, gpui.Size{Width: 320, Height: 240})
	minimal := f.activate(t)

	if got := minimal.NodeCount(); got != 1 {
		t.Errorf("minimal snapshot node count = %d, want 1 (the window root)", got)
	}
	root, ok := minimal.Node(gpui.RootA11yNodeID)
	if !ok || root.Role != gpui.RoleWindow || root.Label != "Counter App" {
		t.Errorf("minimal root = %+v, want the labeled window root", root)
	}
	if got := minimal.Sequence(); got != 1 {
		t.Errorf("minimal sequence = %d, want 1", got)
	}
	if !f.window.RefreshRequested() {
		t.Error("activation did not request a refresh (the complete update must follow)")
	}

	full := f.drawAndPublish(t)
	if got := full.Sequence(); got != 2 {
		t.Errorf("full snapshot sequence = %d, want 2 (after the minimal activation)", got)
	}
	if got := full.NodeCount(); got != 8 {
		// root + container + label + button + editor + 3 synthetic runs.
		t.Errorf("full snapshot node count = %d, want 8 (root, container, label, button, editor, 3 runs)", got)
	}
	// The complete update's trace matches the port-recorded fixture
	// (fixture comparison of the activation/update path).
	if got := full.Trace(); got != fixtureFullTreeTrace {
		t.Errorf("full tree trace mismatch:\ngot:\n%s\nwant:\n%s", got, fixtureFullTreeTrace)
	}
}

// TestStableNodeIdentityAndPropertyUpdates pins node identity across
// frames: the count label keeps its node id while its value updates,
// and the published snapshot is immutable against later app mutation.
func TestStableNodeIdentityAndPropertyUpdates(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newA11yFixture(t, gpui.Size{Width: 320, Height: 240})
	f.activate(t)
	first := f.drawAndPublish(t)
	labelBefore := nodeByAuthor(t, first, "count-label")
	if labelBefore.Value != "Count: 0" {
		t.Errorf("label value = %q, want Count: 0", labelBefore.Value)
	}

	// Update the model, redraw, republish: same node id, new value.
	f.update(t, func(c *Counter, cx *gpui.Context[Counter]) {
		c.count = 7
		cx.Notify()
	})
	second := f.drawAndPublish(t)
	labelAfter := nodeByAuthor(t, second, "count-label")
	if labelAfter.ID != labelBefore.ID {
		t.Errorf("label node id changed across frames: %s -> %s", labelBefore.ID, labelAfter.ID)
	}
	if labelAfter.Value != "Count: 7" {
		t.Errorf("label value after update = %q, want Count: 7", labelAfter.Value)
	}
	if labelAfter.Bounds != labelBefore.Bounds {
		t.Errorf("label bounds changed across a value-only update: %s -> %s", labelBefore.Bounds, labelAfter.Bounds)
	}

	// Immutability: mutate the app WITHOUT redrawing; the published
	// snapshot keeps its values (it copies everything at finalize).
	f.update(t, func(c *Counter, cx *gpui.Context[Counter]) {
		c.count = 99
		cx.Notify()
	})
	stillSecond, err := gpui.PublishWindowA11y(f.window)
	if err != nil || stillSecond != second {
		t.Fatalf("republish without a draw must return the same snapshot: %v", err)
	}
	labelImmutable := nodeByAuthor(t, stillSecond, "count-label")
	if labelImmutable.Value != "Count: 7" {
		t.Errorf("published snapshot mutated with app state: value = %q, want Count: 7", labelImmutable.Value)
	}
	// The returned node slice is a copy: mutating it cannot reach the
	// snapshot.
	nodes := stillSecond.Nodes()
	nodes[0].Label = "tampered"
	if nodeByAuthor(t, stillSecond, "count-label").Value != "Count: 7" {
		t.Error("snapshot nodes alias the returned slice")
	}
	if stillSecond.Trace() != second.Trace() {
		t.Error("trace changed after a copy was mutated")
	}
}

// TestSyntheticChildrenFromPrepaintState pins the editor's synthetic
// TextRun children: derived after prepaint from prepaint state, one per
// run, with the parent-node mutation visible on the published record.
func TestSyntheticChildrenFromPrepaintState(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newA11yFixture(t, gpui.Size{Width: 320, Height: 240})
	f.activate(t)
	snapshot := f.drawAndPublish(t)

	editor := nodeByAuthor(t, snapshot, "editor")
	if editor.Role != gpui.RoleTextInput {
		t.Errorf("editor role = %s, want TextInput", editor.Role)
	}
	if editor.Value != "hello a11y world" {
		t.Errorf("editor value = %q, want the model text", editor.Value)
	}
	if editor.FocusHandleID == 0 {
		t.Error("editor node has no focus handle binding")
	}
	if editor.Description != "caret at byte 0" {
		t.Errorf("editor description = %q, want the caret record (synthetic builder parent mutation)", editor.Description)
	}
	// "hello a11y world" splits into three runs.
	if len(editor.Children) != 3 {
		t.Fatalf("editor synthetic children = %d, want 3", len(editor.Children))
	}
	wantValues := []string{"hello", "a11y", "world"}
	for i, childID := range editor.Children {
		child, ok := snapshot.Node(childID)
		if !ok {
			t.Fatalf("synthetic child %d (%s) missing from the snapshot", i, childID)
		}
		if !child.Synthetic {
			t.Errorf("child %d is not marked synthetic", i)
		}
		if child.Role != gpui.RoleTextRun {
			t.Errorf("child %d role = %s, want TextRun", i, child.Role)
		}
		if child.Value != wantValues[i] {
			t.Errorf("child %d value = %q, want %q", i, child.Value, wantValues[i])
		}
		if child.Bounds != editor.Bounds {
			t.Errorf("child %d bounds = %s, want the parent element bounds %s (leaf nodes inherit)", i, child.Bounds, editor.Bounds)
		}
	}
	// Changing the text changes the runs; synthetic ids derive from
	// (parent, index), so run 0 keeps its id while its value updates.
	f.update(t, func(c *Counter, cx *gpui.Context[Counter]) {
		c.text = "one two"
		c.caret = 3
		cx.Notify()
	})
	next := f.drawAndPublish(t)
	editorNext := nodeByAuthor(t, next, "editor")
	if editorNext.ID != editor.ID {
		t.Error("editor node id changed across a text update")
	}
	if len(editorNext.Children) != 2 {
		t.Fatalf("editor synthetic children after update = %d, want 2", len(editorNext.Children))
	}
	if editorNext.Children[0] != editor.Children[0] {
		t.Errorf("run 0 id changed: %s -> %s (same key under the same parent)", editor.Children[0], editorNext.Children[0])
	}
	if editorNext.Description != "caret at byte 3" {
		t.Errorf("editor description after update = %q, want the new caret record", editorNext.Description)
	}
}

// TestFocusSemanticsReportFocusedNode pins the focus semantics: a bound
// focus handle makes the node focusable, and when the handle holds
// window focus the published tree reports the node as focused.
func TestFocusSemanticsReportFocusedNode(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newA11yFixture(t, gpui.Size{Width: 320, Height: 240})
	f.activate(t)
	unfocused := f.drawAndPublish(t)
	if got := unfocused.Focus(); got != gpui.RootA11yNodeID {
		t.Errorf("unfocused tree focus = %s, want the root", got)
	}
	editor := nodeByAuthor(t, unfocused, "editor")

	// Focus through the existing focus machinery, then draw: the
	// published tree reports the editor node.
	editorHandle := f.facts(t).editorFocus
	f.window.Focus(editorHandle)
	focused := f.drawAndPublish(t)
	if got := focused.Focus(); got != editor.ID {
		t.Errorf("focused tree focus = %s, want the editor node %s", got, editor.ID)
	}
	if !editorHandle.IsFocused(f.window) {
		t.Error("the focus handle lost window focus")
	}
}

// TestActionRoutingThroughForegroundTokens pins the listener path: a
// Click token increments the counter entity inside a foreground
// application update, an editor SetValue carries action data, and the
// next publish reports the updated properties (the update trace).
func TestActionRoutingThroughForegroundTokens(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newA11yFixture(t, gpui.Size{Width: 320, Height: 240})
	f.activate(t)
	snapshot := f.drawAndPublish(t)

	button := nodeByAuthor(t, snapshot, "counter.increment")
	if len(button.Actions) != 2 || button.Actions[0] != gpui.A11yActionClick || button.Actions[1] != gpui.A11yActionIncrement {
		t.Errorf("button actions = %v, want [Click Increment]", button.Actions)
	}
	editor := nodeByAuthor(t, snapshot, "editor")

	runsBefore := a11yHandlerRuns
	token := tokenOf(t, f.app, snapshot, button.ID)
	if err := f.dispatch(t, token, gpui.A11yActionClick, &gpui.A11yActionData{Value: "voice"}); err != nil {
		t.Fatalf("dispatching Click: %v", err)
	}
	if a11yHandlerRuns != runsBefore+1 {
		t.Errorf("handler runs = %d, want %d (one Click listener ran)", a11yHandlerRuns, runsBefore+1)
	}
	facts := f.facts(t)
	if facts.count != 1 || facts.clickRuns != 1 {
		t.Errorf("counter after Click = count %d clickRuns %d, want 1/1 (the entity updated on the foreground thread)", facts.count, facts.clickRuns)
	}
	if facts.lastValue != "voice" {
		t.Errorf("action data did not reach the handler: lastValue = %q", facts.lastValue)
	}
	// The Click handler focuses the button: focus moved through the
	// real focus machinery.
	if !facts.incFocus.IsFocused(f.window) {
		t.Error("the Click handler's window focus did not land")
	}

	// Increment (the data-free listener action).
	if err := f.dispatch(t, token, gpui.A11yActionIncrement, nil); err != nil {
		t.Fatalf("dispatching Increment: %v", err)
	}
	if facts := f.facts(t); facts.count != 2 {
		t.Errorf("counter after Increment = %d, want 2", facts.count)
	}

	// Editor SetValue with data.
	editorToken := tokenOf(t, f.app, snapshot, editor.ID)
	if err := f.dispatch(t, editorToken, gpui.A11yActionSetValue, &gpui.A11yActionData{Value: "typed by a11y"}); err != nil {
		t.Fatalf("dispatching SetValue: %v", err)
	}
	facts = f.facts(t)
	if facts.text != "typed by a11y" || facts.valueRuns != 1 {
		t.Errorf("editor after SetValue = text %q runs %d, want the data value and 1 run", facts.text, facts.valueRuns)
	}

	// The next publish reports the updated properties on the SAME node
	// ids (the update trace fixture).
	next := f.drawAndPublish(t)
	buttonNext := nodeByAuthor(t, next, "counter.increment")
	editorNext := nodeByAuthor(t, next, "editor")
	if buttonNext.ID != button.ID || editorNext.ID != editor.ID {
		t.Error("node ids changed across an action-driven update")
	}
	labelNext := nodeByAuthor(t, next, "count-label")
	if labelNext.Value != "Count: 2" {
		t.Errorf("label value after actions = %q, want Count: 2", labelNext.Value)
	}
	if editorNext.Value != "typed by a11y" {
		t.Errorf("editor value after SetValue = %q, want the action data", editorNext.Value)
	}
	if got := next.Focus(); got != button.ID {
		t.Errorf("updated tree focus = %s, want the focused button node", got)
	}
	if got, want := next.Trace(), fixtureUpdatedTreeTrace; got != want {
		t.Errorf("updated tree trace mismatch:\ngot:\n%s\nwant:\n%s", got, want)
	}
}

// TestFocusActionBuiltInFallback pins the built-in Focus fallback (no
// listener): the action routes through the focus machinery to the
// node's bound handle, and Blur clears it.
func TestFocusActionBuiltInFallback(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newA11yFixture(t, gpui.Size{Width: 320, Height: 240})
	f.activate(t)
	snapshot := f.drawAndPublish(t)

	// The button has no Focus listener: dispatch falls back to
	// window.Focus(handle) through the focusable binding.
	button := nodeByAuthor(t, snapshot, "counter.increment")
	token := tokenOf(t, f.app, snapshot, button.ID)
	if err := f.dispatch(t, token, gpui.A11yActionFocus, nil); err != nil {
		t.Fatalf("dispatching Focus (built-in fallback): %v", err)
	}
	focused := f.window.Focused()
	if !focused.Ok || focused.Handle.ID() != button.FocusHandleID {
		t.Errorf("window focus after the Focus action = ok %v handle %d, want the button's handle %d", focused.Ok, focused.Handle.ID(), button.FocusHandleID)
	}
	// The published tree now reports the button as focused.
	next := f.drawAndPublish(t)
	if got := next.Focus(); got != button.ID {
		t.Errorf("tree focus after the Focus action = %s, want the button node", got)
	}

	// Blur clears window focus (the second built-in fallback).
	if err := f.dispatch(t, token, gpui.A11yActionBlur, nil); err != nil {
		t.Fatalf("dispatching Blur: %v", err)
	}
	if focused := f.window.Focused(); focused.Ok {
		t.Error("window still focused after Blur")
	}
	// An action with no listener and no built-in fallback is a typed
	// error, not a panic.
	if err := f.dispatch(t, token, gpui.A11yActionShowMenu, nil); err == nil {
		t.Error("dispatching an unhandled action must fail")
	} else if err != gpui.ErrA11yNoHandler {
		t.Errorf("unhandled action error = %v, want ErrA11yNoHandler", err)
	}
}

// TestRemovalReinsertionAndTokenGenerations pins the removal/
// reinsertion identity policy: the node id is stable (the reinserted
// node is the same id), but a token resolved before the removal is
// permanently stale — it may never target the replacement.
func TestRemovalReinsertionAndTokenGenerations(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newA11yFixture(t, gpui.Size{Width: 320, Height: 240})
	f.activate(t)
	first := f.drawAndPublish(t)
	editor := nodeByAuthor(t, first, "editor")
	staleToken := tokenOf(t, f.app, first, editor.ID)

	// Remove the editor: its node leaves the published tree.
	f.update(t, func(c *Counter, cx *gpui.Context[Counter]) {
		c.showEditor = false
		cx.Notify()
	})
	second := f.drawAndPublish(t)
	if _, ok := second.Node(editor.ID); ok {
		t.Fatal("the removed editor node is still published")
	}
	if err := f.dispatch(t, staleToken, gpui.A11yActionSetValue, &gpui.A11yActionData{Value: "stale"}); err != gpui.ErrA11yNodeRemoved {
		t.Errorf("dispatch on a removed node = %v, want ErrA11yNodeRemoved", err)
	}
	if facts := f.facts(t); facts.text != "hello a11y world" || facts.valueRuns != 0 {
		t.Errorf("the removed node's handler ran: text %q runs %d", facts.text, facts.valueRuns)
	}

	// Reinsert: the same node id (identity for assistive technology),
	// but a fresh node generation.
	f.update(t, func(c *Counter, cx *gpui.Context[Counter]) {
		c.showEditor = true
		cx.Notify()
	})
	third := f.drawAndPublish(t)
	reinserted, ok := third.Node(editor.ID)
	if !ok {
		t.Fatal("the reinserted editor node is missing")
	}
	if reinserted.ID != editor.ID {
		t.Errorf("reinserted node id = %s, want the stable id %s", reinserted.ID, editor.ID)
	}
	// The OLD token may not target the replacement.
	if err := f.dispatch(t, staleToken, gpui.A11yActionSetValue, &gpui.A11yActionData{Value: "replacement"}); err != gpui.ErrA11yStaleGeneration {
		t.Errorf("dispatch on the replacement = %v, want ErrA11yStaleGeneration (removed nodes never target replacements)", err)
	}
	if facts := f.facts(t); facts.text != "hello a11y world" || facts.valueRuns != 0 {
		t.Errorf("the replacement was targeted by a stale token: text %q runs %d", facts.text, facts.valueRuns)
	}
	// A freshly minted token for the same node id works.
	freshToken := tokenOf(t, f.app, third, editor.ID)
	if freshToken.Node() != staleToken.Node() {
		t.Errorf("fresh token targets %s, stale token targeted %s (same node id expected)", freshToken.Node(), staleToken.Node())
	}
	if err := f.dispatch(t, freshToken, gpui.A11yActionSetValue, &gpui.A11yActionData{Value: "fresh"}); err != nil {
		t.Fatalf("dispatching with a fresh token: %v", err)
	}
	if facts := f.facts(t); facts.text != "fresh" {
		t.Errorf("editor text after a fresh token = %q, want fresh", facts.text)
	}
}

// TestHiddenSubtreeFlagAndIdentity pins aria_hidden semantics: the
// hidden node keeps its stable id and stays in the update with the
// hidden flag (exposure filtering is the native provider's job,
// ticket22).
func TestHiddenSubtreeFlagAndIdentity(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newA11yFixture(t, gpui.Size{Width: 320, Height: 240})
	f.activate(t)
	first := f.drawAndPublish(t)
	label := nodeByAuthor(t, first, "count-label")
	if label.Hidden {
		t.Fatal("the label starts hidden")
	}

	f.update(t, func(c *Counter, cx *gpui.Context[Counter]) {
		c.hiddenLabel = true
		cx.Notify()
	})
	second := f.drawAndPublish(t)
	hidden, ok := second.Node(label.ID)
	if !ok {
		t.Fatal("the hidden label node left the tree (hidden subtrees keep a stable node)")
	}
	if !hidden.Hidden {
		t.Error("the hidden node is not flagged hidden")
	}
	if hidden.ID != label.ID || hidden.Value != label.Value {
		t.Errorf("hidden node changed identity: %+v vs %+v", hidden, label)
	}

	// Unhide: same id, flag cleared.
	f.update(t, func(c *Counter, cx *gpui.Context[Counter]) {
		c.hiddenLabel = false
		cx.Notify()
	})
	third := f.drawAndPublish(t)
	unhidden := nodeByAuthor(t, third, "count-label")
	if unhidden.ID != label.ID || unhidden.Hidden {
		t.Errorf("unhidden node = id %s hidden %v, want the stable id and no flag", unhidden.ID, unhidden.Hidden)
	}
}

// TestStaleActionsAfterCloseAreRejectedWithoutReviving pins the close
// path: after the window's accessibility runtime retires and the window
// closes (its scope releasing the model entity's lease), outstanding
// tokens are rejected before any handler runs — nothing revives the
// entity.
func TestStaleActionsAfterCloseAreRejectedWithoutReviving(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newA11yFixture(t, gpui.Size{Width: 320, Height: 240})
	f.activate(t)
	snapshot := f.drawAndPublish(t)
	button := nodeByAuthor(t, snapshot, "counter.increment")
	token := tokenOf(t, f.app, snapshot, button.ID)

	// Teardown: retire the a11y runtime, then close the window.
	gpui.CloseWindowA11y(f.window)
	gpui.CloseTestWindow(f.window)

	runsBefore := a11yHandlerRuns
	if err := f.dispatch(t, token, gpui.A11yActionClick, nil); err != gpui.ErrA11yWindowGone {
		t.Errorf("dispatch after close = %v, want ErrA11yWindowGone", err)
	}
	if a11yHandlerRuns != runsBefore {
		t.Error("a handler ran for a stale post-close token")
	}
	// The minted-but-unresolved token is rejected too (validation never
	// reaches the identity model for a gone window).
	unresolved, err := snapshot.Token(button.ID)
	if err != nil {
		t.Fatalf("minting: %v", err)
	}
	if err := f.dispatch(t, unresolved, gpui.A11yActionClick, nil); err != gpui.ErrA11yWindowGone {
		t.Errorf("dispatch of an unresolved token after close = %v, want ErrA11yWindowGone", err)
	}
	// The window is gone from the registry; publishing is a typed error,
	// not a panic.
	if _, err := gpui.PublishWindowA11y(f.window); err == nil {
		t.Error("publishing a closed window's a11y runtime must fail")
	}
}

// TestPerWindowGenerationSeparation pins per-window generation
// separation: two windows render the SAME recipe type, so their element
// id paths and node ids are IDENTICAL — the tokens stay separated by
// window identity and generation, closing one window never disturbs the
// other, and window A's tokens never route into window B.
func TestPerWindowGenerationSeparation(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	testApp := gpui.NewTestApp()
	app := testApp.App()
	size := gpui.Size{Width: 320, Height: 240}

	wA := gpui.NewTestWindow(app, size)
	wB := gpui.NewTestWindow(app, size)
	gpui.SetA11yWindowTitle(wA, "Window A")
	gpui.SetA11yWindowTitle(wB, "Window B")
	counterA := newCounter(app, wA)
	counterB := newCounter(app, wB)
	wA.SetRootView(&counterRootView{counter: counterA})
	wB.SetRootView(&counterRootView{counter: counterB})
	gpui.ActivateWindowA11y(wA)
	gpui.ActivateWindowA11y(wB)

	draw := func(w *gpui.Window) *gpui.A11ySnapshot {
		if _, err := gpui.DrawWindowFrame(w); err != nil {
			t.Fatalf("DrawWindowFrame: %v", err)
		}
		snapshot, err := gpui.PublishWindowA11y(w)
		if err != nil {
			t.Fatalf("PublishWindowA11y: %v", err)
		}
		return snapshot
	}
	snapA, snapB := draw(wA), draw(wB)
	if snapA.WindowID() == snapB.WindowID() {
		t.Fatal("the two windows share a window identity")
	}

	// Identical element paths -> identical node ids (the separation
	// below rides on the tokens' window identity and generation, not on
	// the node ids).
	nodeA := nodeByAuthor(t, snapA, "counter.increment")
	nodeB := nodeByAuthor(t, snapB, "counter.increment")
	if nodeA.ID != nodeB.ID {
		t.Fatalf("same-path nodes got different ids: %s vs %s — the separation test needs identical ids", nodeA.ID, nodeB.ID)
	}

	// Tokens route to their own window's entity.
	tokenA, err := snapA.Token(nodeA.ID)
	if err != nil {
		t.Fatalf("minting window A token: %v", err)
	}
	tokenA, err = gpui.ResolveA11yToken(app, tokenA)
	if err != nil {
		t.Fatalf("resolving window A token: %v", err)
	}
	tokenB, err := snapB.Token(nodeB.ID)
	if err != nil {
		t.Fatalf("minting window B token: %v", err)
	}
	tokenB, err = gpui.ResolveA11yToken(app, tokenB)
	if err != nil {
		t.Fatalf("resolving window B token: %v", err)
	}

	if err := gpui.DispatchA11yAction(app, tokenA, gpui.A11yActionClick, nil); err != nil {
		t.Fatalf("window A click: %v", err)
	}
	factsA := counterA.Read(app, func(c *Counter, _ *gpui.App) Counter { return *c })
	factsB := counterB.Read(app, func(c *Counter, _ *gpui.App) Counter { return *c })
	if factsA.clickRuns != 1 || factsB.clickRuns != 0 {
		t.Errorf("clicks routed cross-window: A=%d B=%d, want 1/0", factsA.clickRuns, factsB.clickRuns)
	}

	// Close window A only: A's tokens go stale, B keeps working, and
	// B's generation is untouched.
	genB := snapB.Generation()
	gpui.CloseWindowA11y(wA)
	gpui.CloseTestWindow(wA)
	if err := gpui.DispatchA11yAction(app, tokenA, gpui.A11yActionClick, nil); err != gpui.ErrA11yWindowGone {
		t.Errorf("window A token after its close = %v, want ErrA11yWindowGone", err)
	}
	runsBefore := a11yHandlerRuns
	if err := gpui.DispatchA11yAction(app, tokenB, gpui.A11yActionClick, nil); err != nil {
		t.Fatalf("window B click after window A closed: %v", err)
	}
	if a11yHandlerRuns != runsBefore+1 {
		t.Error("window B's handler did not run")
	}
	snapB2 := draw(wB)
	if snapB2.Generation() != genB {
		t.Errorf("window B generation changed from %d to %d while window A closed", genB, snapB2.Generation())
	}
	factsB = counterB.Read(app, func(c *Counter, _ *gpui.App) Counter { return *c })
	if factsB.clickRuns != 1 {
		t.Errorf("window B clicks = %d, want 1", factsB.clickRuns)
	}
	gpui.CloseWindowA11y(wB)
	gpui.CloseTestWindow(wB)
}

// TestDuplicateNodeIDCollisionRecorded pins the collision policy: two
// sibling elements with the same id path collide; the second push is
// discarded from the tree and recorded on the published snapshot (the
// port records where the reference asserts in debug builds and silently
// drops in release).
func TestDuplicateNodeIDCollisionRecorded(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	testApp := gpui.NewTestApp()
	app := testApp.App()
	w := gpui.NewTestWindow(app, gpui.Size{Width: 320, Height: 240})
	gpui.SetA11yWindowTitle(w, "Collision")
	w.SetRootView(&collisionRootView{})
	gpui.ActivateWindowA11y(w)

	if _, err := gpui.DrawWindowFrame(w); err != nil {
		t.Fatalf("DrawWindowFrame: %v", err)
	}
	snapshot, err := gpui.PublishWindowA11y(w)
	if err != nil {
		t.Fatalf("PublishWindowA11y: %v", err)
	}

	// Exactly one count-label node: the first push wins, the second is
	// dropped.
	labels := 0
	var first gpui.A11ySnapshotNode
	for _, node := range snapshot.Nodes() {
		if node.AuthorID == "count-label" {
			labels++
			first = node
			if node.Value != "first" {
				t.Errorf("the surviving collision node value = %q, want the first push", node.Value)
			}
		}
	}
	if labels != 1 {
		t.Errorf("count-label nodes = %d, want 1 (the duplicate is dropped)", labels)
	}
	collisions := snapshot.Collisions()
	if len(collisions) != 1 {
		t.Fatalf("recorded collisions = %d, want 1", len(collisions))
	}
	if collisions[0] == gpui.RootA11yNodeID {
		t.Error("the root was recorded as a collision")
	}
	// The recorded collision id is the duplicated path's id.
	if collisions[0] != first.ID {
		t.Errorf("collision id = %s, want the duplicated path id %s", collisions[0], first.ID)
	}
	// A collision is an authoring error, not a builder failure: the
	// frame completed and the snapshot published.
	if snapshot.NodeCount() != 3 {
		t.Errorf("snapshot node count = %d, want 3 (root, container, one surviving label)", snapshot.NodeCount())
	}
	gpui.CloseWindowA11y(w)
	gpui.CloseTestWindow(w)
}

// collisionRootView renders two same-id labels for the collision test.
type collisionRootView struct{}

// EntityID implements gpui.View.
func (v *collisionRootView) EntityID() (gpui.EntityID, bool) { return 0, false }

// RenderOnce implements gpui.View.
func (v *collisionRootView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return gpui.Accessibility(&appContainer{children: []gpui.AnyElement{
		gpui.Accessibility(&countLabel{value: "first"}),
		gpui.Accessibility(&countLabel{value: "second"}),
	}})
}

// TestUnpublishedBuildIsDiscardedByNextFrame pins the frame lifecycle:
// a completed-but-unpublished build is dropped when the next frame
// starts pushing (the per-frame state clears at begin_frame, a11y.rs),
// so publishing after two draws reports the LAST frame's tree with the
// publication sequence still advancing by exactly one.
func TestUnpublishedBuildIsDiscardedByNextFrame(t *testing.T) {
	if testing.Short() {
		t.Skip("draws through the native layout service")
	}
	f := newA11yFixture(t, gpui.Size{Width: 320, Height: 240})
	f.activate(t)

	// Draw twice without publishing between: only the second frame's
	// build survives.
	f.update(t, func(c *Counter, cx *gpui.Context[Counter]) { c.count = 1; cx.Notify() })
	if _, err := gpui.DrawWindowFrame(f.window); err != nil {
		t.Fatalf("first draw: %v", err)
	}
	f.update(t, func(c *Counter, cx *gpui.Context[Counter]) { c.count = 2; cx.Notify() })
	if _, err := gpui.DrawWindowFrame(f.window); err != nil {
		t.Fatalf("second draw: %v", err)
	}
	snapshot := f.drawAndPublishT(t)
	if got := snapshot.Sequence(); got != 2 {
		t.Errorf("publication sequence = %d, want 2 (activation + one publish; the skipped frame published nothing)", got)
	}
	label := nodeByAuthor(t, snapshot, "count-label")
	if label.Value != "Count: 2" {
		t.Errorf("label value = %q, want Count: 2 (the last draw's tree)", label.Value)
	}
	if got := snapshot.NodeCount(); got != 8 {
		t.Errorf("node count = %d, want 8 (one frame's tree, not two merged)", got)
	}
	// A token minted from the stale first build's identity model can
	// still resolve: the nodes persisted across every frame.
	button := nodeByAuthor(t, snapshot, "counter.increment")
	if _, err := snapshot.Token(button.ID); err != nil {
		t.Fatalf("minting a token for a node that drew every frame: %v", err)
	}
}

// drawAndPublishT draws one more frame and publishes (the T form keeps
// the original helper's name available for the earlier tests).
func (f *a11yFixture) drawAndPublishT(t *testing.T) *gpui.A11ySnapshot {
	t.Helper()
	return f.drawAndPublish(t)
}
