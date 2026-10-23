package gpui

// Base ticket 11's tests: the ports of the pinned write_a11y_info tests
// (R:elements/div.rs:5690-5765), the pinned active-descendant builder
// tests (R:window/a11y.rs:703-933), port-recorded golden snapshot traces
// for every new property drawn through a real test window, and the
// ticket-02 publication policy (a tree with unsupported roles
// serializes with the nearest supported role instead of aborting).
//
// HONEST SOURCE NOTE on the goldens: like ticket 21's fixtures
// (internal/accessspec/fixtures.go) these traces are produced by THIS
// port's deterministic builder and pinned; the pin has no a11y trace
// fixtures. Node ids derive from element id paths only.

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// ---------------------------------------------------------------------------
// Ports of the pinned write_a11y_info tests (R:elements/div.rs:5690-5765)
// ---------------------------------------------------------------------------

// TestA11yDivAccessibilityIDBuilderWritesAuthorID ports
// test_accessibility_id_builder_writes_author_id (div.rs:5690-5700).
func TestA11yDivAccessibilityIDBuilderWritesAuthorID(t *testing.T) {
	d := Div().ID("buffer-font-size").AccessibilityID("settings.buffer-font-size")
	spec := A11ySpec{Role: RoleSpinButton}
	d.WriteA11yInfo(&spec)
	if spec.AuthorID != "settings.buffer-font-size" {
		t.Fatalf("author id = %q, want settings.buffer-font-size", spec.AuthorID)
	}
}

// TestA11yDivWriteInfoStringAndNumericProperties ports
// test_write_a11y_info_string_and_numeric_properties (div.rs:5702-5725).
// The pin sets Interactivity.aria directly; the port sets it through
// the builders (the Go aria state is private to the div).
func TestA11yDivWriteInfoStringAndNumericProperties(t *testing.T) {
	d := Div().
		AccessibilityID("settings.buffer-font-size").
		AriaLabel("Buffer Font Size").
		AriaValue("15").
		AriaPlaceholder("Search").
		AriaNumericValue(15.0).
		AriaMinNumericValue(6.0).
		AriaMaxNumericValue(72.0).
		AriaNumericValueStep(1.0)
	spec := A11ySpec{Role: RoleSpinButton}
	d.WriteA11yInfo(&spec)
	if spec.AuthorID != "settings.buffer-font-size" {
		t.Errorf("author id = %q", spec.AuthorID)
	}
	if spec.Label != "Buffer Font Size" {
		t.Errorf("label = %q", spec.Label)
	}
	if spec.Value != "15" {
		t.Errorf("value = %q", spec.Value)
	}
	if spec.Placeholder != "Search" {
		t.Errorf("placeholder = %q", spec.Placeholder)
	}
	checks := []struct {
		name string
		got  Optional[float64]
		want float64
	}{
		{"numeric_value", spec.NumericValue, 15},
		{"min_numeric_value", spec.MinNumericValue, 6},
		{"max_numeric_value", spec.MaxNumericValue, 72},
		{"numeric_value_step", spec.NumericValueStep, 1},
	}
	for _, c := range checks {
		if v, ok := c.got.Get(); !ok || v != c.want {
			t.Errorf("%s = %v (present %v), want Some(%v)", c.name, v, ok, c.want)
		}
	}
}

// TestA11yDivWriteInfoDisabledModalAndLiveRegion ports
// test_write_a11y_info_disabled_modal_and_live_region (div.rs:5727-5742).
func TestA11yDivWriteInfoDisabledModalAndLiveRegion(t *testing.T) {
	d := Div().AriaDisabled(true).AriaModal(true).AriaLive(A11yLiveAssertive).AriaLiveAtomic(true)
	spec := A11ySpec{Role: RoleDialog}
	d.WriteA11yInfo(&spec)
	if !spec.Disabled {
		t.Error("is_disabled = false")
	}
	if !spec.Modal {
		t.Error("is_modal = false")
	}
	if v, ok := spec.Live.Get(); !ok || v != A11yLiveAssertive {
		t.Errorf("live = %v (present %v), want Some(Assertive)", v, ok)
	}
	if !spec.LiveAtomic {
		t.Error("is_live_atomic = false")
	}
}

// TestA11yDivWriteInfoFalseClearsFlags ports
// test_write_a11y_info_disabled_and_modal_false_clear_the_flags
// (div.rs:5744-5765): Some(false) explicitly CLEARS a flag a previous
// frame set.
func TestA11yDivWriteInfoFalseClearsFlags(t *testing.T) {
	d := Div().AriaDisabled(false).AriaModal(false).AriaLiveAtomic(false)
	spec := A11ySpec{Role: RoleDialog}
	spec.Disabled, spec.Modal, spec.LiveAtomic = true, true, true
	d.WriteA11yInfo(&spec)
	if spec.Disabled || spec.Modal || spec.LiveAtomic {
		t.Errorf("flags after Some(false) = disabled %v modal %v live_atomic %v, want all cleared", spec.Disabled, spec.Modal, spec.LiveAtomic)
	}
	// None leaves a flag untouched (write_a11y_info writes only present
	// properties).
	untouched := A11ySpec{}
	untouched.Disabled = true
	Div().WriteA11yInfo(&untouched)
	if !untouched.Disabled {
		t.Error("an absent aria_disabled cleared the flag")
	}
}

// TestA11yDivRoleAndAdvertisedActions covers the element hooks
// (div.rs:2261-2271) and the action advertisement (div.rs:4153-4161).
func TestA11yDivRoleAndAdvertisedActions(t *testing.T) {
	if _, ok := Div().A11yRole(); ok {
		t.Error("a plain div reports a role")
	}
	if _, ok := Div().Role(RoleGenericContainer).A11yRole(); ok {
		t.Error("GenericContainer was not filtered (div.rs:2264-2266)")
	}
	if role, ok := Div().Role(RoleTab).A11yRole(); !ok || role != RoleTab {
		t.Errorf("A11yRole = %s %v, want Tab true", role, ok)
	}
	if !Div().AriaHidden().IsA11yHidden() || Div().IsA11yHidden() {
		t.Error("IsA11yHidden does not mirror aria_hidden")
	}
	ta := NewTestApp()
	focus := ta.App().FocusHandle()
	d := Div().
		OnClick(func(*ClickEvent, *Window, *App) {}).
		TrackFocus(focus).
		OnA11yAction(A11yActionIncrement, func(*A11yActionData, *Window, *App) {}).
		OnA11yAction(A11yActionClick, func(*A11yActionData, *Window, *App) {})
	spec := A11ySpec{}
	d.WriteA11yInfo(&spec)
	got := append([]A11yAction(nil), spec.Actions...)
	sortA11yActions(got)
	want := []A11yAction{A11yActionClick, A11yActionFocus, A11yActionIncrement}
	if len(got) != len(want) {
		t.Fatalf("advertised actions = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("advertised actions = %v, want %v", got, want)
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("a negative usize builder value did not panic")
		}
	}()
	Div().AriaLevel(-1)
}

// TestA11yDivStateLivesOnTheDiv pins the STORAGE note: the authoring
// state is the div's own field, absent until a builder runs and shared
// by every builder call on that div.
func TestA11yDivStateLivesOnTheDiv(t *testing.T) {
	plain := Div()
	if plain.hasA11yAuthoring() {
		t.Fatal("a div with no a11y builder reports authoring state")
	}
	d := Div().Role(RoleButton)
	st := d.a11yStateIfAny()
	if st == nil || d.AriaLabel("x").a11yStateIfAny() != st {
		t.Fatal("builders on one div do not share its authoring state")
	}
	if Div().Role(RoleButton).a11yStateIfAny() == st {
		t.Fatal("two divs share authoring state")
	}
}

// ---------------------------------------------------------------------------
// Ports of the pinned active-descendant tests (R:window/a11y.rs:703-933)
// ---------------------------------------------------------------------------

// a11yTestBuild is a bare frame build over a windowA11yState (the
// pin's new_builder / new_a11y helpers, a11y.rs:668-682).
type a11yTestBuild struct {
	st *windowA11yState
}

func newA11yTestBuild() *a11yTestBuild {
	st := &windowA11yState{
		focusable:       make(map[A11yNodeID]uint64),
		listeners:       make(map[A11yNodeID]map[A11yAction][]A11yActionListener),
		nodeGenerations: make(map[A11yNodeID]uint64),
	}
	st.beginBuild(&frameDrawState{})
	return &a11yTestBuild{st: st}
}

func (b *a11yTestBuild) push(t *testing.T, id A11yNodeID) *a11yPendingNode {
	t.Helper()
	if !b.st.beginNode(id, A11ySpec{Role: RoleGenericContainer}, Bounds{}, false) {
		t.Fatalf("push %s failed", id)
	}
	return b.st.stack[len(b.st.stack)-1]
}

func (b *a11yTestBuild) pop(node *a11yPendingNode) { b.st.endNode(node) }

// setFocus is the builder-level set_focus (registered focusable first,
// like the A11y-level tests).
func (b *a11yTestBuild) setFocus(id A11yNodeID) {
	b.st.focusable[id] = 1
	b.st.setFocus(id)
}

func (b *a11yTestBuild) finalize() A11yNodeID {
	return b.st.finalizeFocus(b.st.hasNode)
}

func (b *a11yTestBuild) diagnostics() string {
	var kinds []string
	for _, d := range b.st.pendingDiagnostics {
		kinds = append(kinds, d.Kind)
	}
	return strings.Join(kinds, ",")
}

// TestA11yActiveDescendant ports the pinned builder/A11y tests.
func TestA11yActiveDescendant(t *testing.T) {
	// a11y.rs:703-722 active_descendant_honored_when_container_focused.
	t.Run("honored_when_container_focused", func(t *testing.T) {
		b := newA11yTestBuild()
		container := b.push(t, 1)
		b.setFocus(1)
		item := b.push(t, 2)
		if !b.st.focusIsAncestorOfCurrent() {
			t.Fatal("the focused container is not an ancestor of the item")
		}
		b.st.claimActiveDescendant(2)
		b.pop(item)
		b.pop(container)
		if got := b.finalize(); got != 2 {
			t.Fatalf("focus = %s, want the item", got)
		}
	})
	// a11y.rs:724-746 active_descendant_honored_for_deep_descendant.
	t.Run("honored_for_deep_descendant", func(t *testing.T) {
		b := newA11yTestBuild()
		container := b.push(t, 1)
		b.setFocus(1)
		group := b.push(t, 2)
		item := b.push(t, 3)
		if !b.st.focusIsAncestorOfCurrent() {
			t.Fatal("depth broke the ancestor gate")
		}
		b.st.claimActiveDescendant(3)
		b.pop(item)
		b.pop(group)
		b.pop(container)
		if got := b.finalize(); got != 3 {
			t.Fatalf("focus = %s, want the grandchild", got)
		}
	})
	// a11y.rs:748-773 active_descendant_ignored_when_focus_in_other_subtree.
	t.Run("ignored_when_focus_in_other_subtree", func(t *testing.T) {
		b := newA11yTestBuild()
		fc := b.push(t, 1)
		fl := b.push(t, 2)
		b.setFocus(2)
		b.pop(fl)
		b.pop(fc)
		oc := b.push(t, 3)
		oi := b.push(t, 4)
		if b.st.focusIsAncestorOfCurrent() {
			t.Fatal("the gate passed for a subtree the focus is not in")
		}
		b.pop(oi)
		b.pop(oc)
		if got := b.finalize(); got != 2 {
			t.Fatalf("focus = %s, want the focused leaf", got)
		}
	})
	// a11y.rs:775-792 active_descendant_ignored_when_nothing_focused.
	t.Run("ignored_when_nothing_focused", func(t *testing.T) {
		b := newA11yTestBuild()
		c := b.push(t, 1)
		i := b.push(t, 2)
		if b.st.focusIsAncestorOfCurrent() {
			t.Fatal("the gate passed with nothing focused")
		}
		b.pop(i)
		b.pop(c)
		if got := b.finalize(); got != RootA11yNodeID {
			t.Fatalf("focus = %s, want the root", got)
		}
	})
	// a11y.rs:794-805 regular_focus_used_when_no_active_descendant.
	t.Run("regular_focus_used_when_no_active_descendant", func(t *testing.T) {
		b := newA11yTestBuild()
		n := b.push(t, 1)
		b.setFocus(1)
		b.pop(n)
		if got := b.finalize(); got != 1 {
			t.Fatalf("focus = %s, want the focused node", got)
		}
	})
	// a11y.rs:807-826 focus_is_ancestor_excludes_self_and_non_ancestors.
	t.Run("focus_is_ancestor_excludes_self_and_non_ancestors", func(t *testing.T) {
		b := newA11yTestBuild()
		c := b.push(t, 1)
		b.setFocus(1)
		if b.st.focusIsAncestorOfCurrent() {
			t.Fatal("the focused node is its own strict ancestor")
		}
		i := b.push(t, 2)
		if !b.st.focusIsAncestorOfCurrent() {
			t.Fatal("the focused container is not a strict ancestor of its child")
		}
		b.pop(i)
		b.pop(c)
	})
	// a11y.rs:828-839 multiple_active_descendant_claims_panic_in_debug
	// (DEVIATED: the port records the misuse and keeps the release
	// last-wins behavior).
	t.Run("multiple_active_descendant_claims", func(t *testing.T) {
		b := newA11yTestBuild()
		b.st.claimActiveDescendant(1)
		b.st.claimActiveDescendant(2)
		if got := b.diagnostics(); got != "active-descendant-claimed-twice" {
			t.Fatalf("diagnostics = %q", got)
		}
		if b.st.activeDescendant != 2 {
			t.Fatalf("active descendant = %s, want last-wins 2", b.st.activeDescendant)
		}
	})
	// a11y.rs:869-880 set_active_descendant_on_focused_node (DEVIATED:
	// recorded, the claim dropped — the release warn-and-return).
	t.Run("set_active_descendant_on_focused_node", func(t *testing.T) {
		b := newA11yTestBuild()
		n := b.push(t, 1)
		b.setFocus(1)
		b.st.setActiveDescendant(1)
		b.pop(n)
		if got := b.diagnostics(); got != "active-descendant-on-focused-node" {
			t.Fatalf("diagnostics = %q", got)
		}
		if b.st.activeDescendantSet {
			t.Fatal("the focused node's claim was recorded")
		}
	})
	// a11y.rs:882-909 two_siblings_claiming_active_descendant (DEVIATED:
	// recorded, last-wins).
	t.Run("two_siblings_claiming_active_descendant", func(t *testing.T) {
		b := newA11yTestBuild()
		c := b.push(t, 1)
		b.setFocus(1)
		first := b.push(t, 2)
		b.st.setActiveDescendant(2)
		b.pop(first)
		second := b.push(t, 3)
		b.st.setActiveDescendant(3)
		b.pop(second)
		b.pop(c)
		if got := b.diagnostics(); got != "active-descendant-claimed-twice" {
			t.Fatalf("diagnostics = %q", got)
		}
		if got := b.finalize(); got != 3 {
			t.Fatalf("focus = %s, want last-wins 3", got)
		}
	})
	// a11y.rs:911-933 active_descendant_in_unfocused_subtree_keeps_real_focus.
	t.Run("active_descendant_in_unfocused_subtree_keeps_real_focus", func(t *testing.T) {
		b := newA11yTestBuild()
		a := b.push(t, 1)
		b.setFocus(1)
		b.pop(a)
		bn := b.push(t, 2)
		c := b.push(t, 3)
		b.st.setActiveDescendant(3)
		b.pop(c)
		b.pop(bn)
		if got := b.finalize(); got != 1 {
			t.Fatalf("focus = %s, want the real focus", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Golden snapshot traces through a real test window
// ---------------------------------------------------------------------------

// a11yDivView renders whatever its build function returns.
type a11yDivView struct {
	build func(w *Window, cx *Context[a11yDivView]) AnyElement
}

func (v *a11yDivView) Render(w *Window, cx *Context[a11yDivView]) AnyElement {
	return v.build(w, cx)
}

// a11yDivFixture is one test app + window with the a11y runtime active.
type a11yDivFixture struct {
	t      *testing.T
	app    *App
	window *Window
	view   Entity[a11yDivView]
}

func newA11yDivFixture(t *testing.T, title string, build func(w *Window, cx *Context[a11yDivView]) AnyElement) *a11yDivFixture {
	t.Helper()
	ta := NewTestApp()
	app := ta.App()
	window := NewTestWindow(app, Size{Width: 400, Height: 600})
	SetA11yWindowTitle(window, title)
	view := NewEntity(app, window.Scope(), func(v *a11yDivView, cx *Context[a11yDivView]) { v.build = build })
	window.SetRootView(ViewOf(view))
	ActivateWindowA11y(window)
	return &a11yDivFixture{t: t, app: app, window: window, view: view}
}

func (f *a11yDivFixture) drawAndPublish() *A11ySnapshot {
	f.t.Helper()
	if _, err := DrawWindowFrame(f.window); err != nil {
		f.t.Fatalf("DrawWindowFrame: %v", err)
	}
	snapshot, err := PublishWindowA11y(f.window)
	if err != nil {
		f.t.Fatalf("PublishWindowA11y: %v", err)
	}
	return snapshot
}

func (f *a11yDivFixture) node(snapshot *A11ySnapshot, author string) A11ySnapshotNode {
	f.t.Helper()
	for _, n := range snapshot.Nodes() {
		if n.AuthorID == author {
			return n
		}
	}
	f.t.Fatalf("no node with author id %q in\n%s", author, snapshot.Trace())
	return A11ySnapshotNode{}
}

// ax converts an authored div through the accessibility companion (the
// route div.go's IntoElement takes once ticket 11's hand-off lands).
func ax(d *DivElement) AnyElement { return divIntoA11yElement(d) }

// leaf is a fixed-size authored div.
func leaf(id string) *DivElement {
	return Div().ID(id).AccessibilityID(id).W(Px(40)).H(Px(10))
}

// a11yPropertiesTree is the property corpus: one node per new property
// and every Base role.
func a11yPropertiesTree(incrementRuns *int) func(w *Window, cx *Context[a11yDivView]) AnyElement {
	return func(w *Window, cx *Context[a11yDivView]) AnyElement {
		row := func(id string, role Role, children ...AnyElement) AnyElement {
			d := Div().ID(id).AccessibilityID(id).Role(role).FlexRow()
			for _, c := range children {
				d = d.Child(c)
			}
			return ax(d)
		}
		return ax(Div().ID("controls").AccessibilityID("controls").Role(RoleGroup).AriaLabel("Controls").FlexCol().SizeFull().
			Child(ax(leaf("check").Role(RoleCheckBox).AriaLabel("Agree").AriaToggled(ToggledMixed).AriaDisabled(true))).
			Child(ax(leaf("switch").Role(RoleSwitch).AriaToggled(ToggledTrue).OnClick(func(*ClickEvent, *Window, *App) {}))).
			Child(ax(leaf("volume").Role(RoleSlider).AriaValue("15").
				AriaNumericValue(15).AriaMinNumericValue(6).AriaMaxNumericValue(72).AriaNumericValueStep(0.5).
				AriaOrientation(A11yOrientationHorizontal).
				OnA11yAction(A11yActionIncrement, func(*A11yActionData, *Window, *App) { *incrementRuns++ }).
				OnA11yAction(A11yActionDecrement, func(*A11yActionData, *Window, *App) {}))).
			Child(ax(Div().ID("tabs").AccessibilityID("tabs").Role(RoleTabList).AriaOrientation(A11yOrientationVertical).FlexRow().
				Child(ax(leaf("tab-1").Role(RoleTab).AriaSelected(true).AriaPositionInSet(1).AriaSizeOfSet(2))).
				Child(ax(leaf("tab-2").Role(RoleTab).AriaSelected(false).AriaPositionInSet(2).AriaSizeOfSet(2))))).
			Child(ax(leaf("combo").Role(RoleComboBox).AriaExpanded(false).AriaPlaceholder("Pick one").AriaKeyshortcuts("Alt+Down"))).
			Child(ax(leaf("title").Role(RoleHeading).AriaLevel(2).AriaLabel("Title"))).
			Child(ax(Div().ID("grid").AccessibilityID("grid").Role(RoleTable).AriaRowCount(4).AriaColumnCount(3).
				Child(ax(Div().ID("row-1").AccessibilityID("row-1").Role(RoleRow).AriaRowIndex(1).FlexRow().
					Child(ax(leaf("head").Role(RoleColumnHeader).AriaColumnIndex(0))).
					Child(ax(leaf("cell").Role(RoleCell).AriaRowIndex(1).AriaColumnIndex(2))))))).
			Child(ax(leaf("dialog").Role(RoleDialog).AriaModal(true).AriaDescription("Confirm"))).
			Child(ax(leaf("status").Role(RoleAlert).AriaLive(A11yLiveAssertive).AriaLiveAtomic(true))).
			// aria_hidden without a role: a GenericContainer node
			// (element.rs:429-430) whose subtree keeps its nodes.
			Child(ax(Div().ID("decor").AccessibilityID("decor").AriaHidden().
				Child(ax(leaf("inner").Role(RoleButton))))).
			// GenericContainer is filtered (no node); an id-less role
			// publishes no node (element.rs:110).
			Child(ax(leaf("plain").Role(RoleGenericContainer))).
			Child(ax(Div().Role(RoleButton).AriaLabel("no id").W(Px(10)).H(Px(10)))).
			Child(ax(leaf("doc").Role(RoleTextInput).A11ySyntheticChildren(func(b *A11ySubtree) {
				b.PushChild(b.SyntheticNodeID(0), A11ySpec{Role: RoleTextRun, Value: "run"})
			}))).
			Child(row("tree", RoleTree, ax(leaf("item").Role(RoleTreeItem).AriaExpanded(true).AriaLevel(1).AriaSelected(true)))).
			Child(row("radios", RoleRadioGroup, ax(leaf("radio-a").Role(RoleRadioButton).AriaToggled(ToggledTrue)))).
			Child(ax(leaf("progress").Role(RoleProgressIndicator).AriaNumericValue(0.5))).
			Child(row("misc", RoleToolbar,
				ax(leaf("link").Role(RoleLink).AriaLabel("Docs")),
				ax(leaf("spin").Role(RoleSpinButton)),
				ax(leaf("time").Role(RoleTimeInput)),
				ax(leaf("tip").Role(RoleTooltip)),
				ax(leaf("region").Role(RoleRegion)),
				ax(leaf("nav").Role(RoleNavigation)),
				ax(leaf("rows").Role(RoleRowGroup)),
				ax(leaf("alertdialog").Role(RoleAlertDialog)),
				ax(leaf("unknown").Role(RoleUnknown)),
			)))
	}
}

// fixtureA11yDivPropertiesTrace is the golden trace of the property
// corpus (port-recorded, see the file header).
const fixtureA11yDivPropertiesTrace = `window "A11y Properties" gen=1 seq=2 nodes=34 focus=#0000000000000000
#0000000000000000 Window label="A11y Properties" bounds=(0, 0, 0 x 0)
  #e2d612ae9fcdf9ae Group id="controls" label="Controls" bounds=(0, 0, 400 x 600)
    #33ef6ba51a531d13 CheckBox id="check" label="Agree" toggled=Mixed disabled bounds=(0, 0, 40 x 10)
    #24f388a52a40ad7d Switch id="switch" toggled=True actions=[Click] bounds=(0, 10, 40 x 10)
    #24b9be05438b9c5b Slider id="volume" value="15" numeric=15 min=6 max=72 step=0.5 orientation=Horizontal actions=[Increment,Decrement] bounds=(0, 20, 40 x 10)
    #973d376da0dedd61 TabList id="tabs" orientation=Vertical bounds=(0, 30, 400 x 10)
      #7fce60ee32d3a2cb Tab id="tab-1" selected=true posinset=1 setsize=2 bounds=(0, 30, 40 x 10)
      #7fce61ee32d3a47e Tab id="tab-2" selected=false posinset=2 setsize=2 bounds=(40, 30, 40 x 10)
    #bf14277b3b9f8f5d ComboBox id="combo" placeholder="Pick one" shortcut="Alt+Down" expanded=false bounds=(0, 40, 40 x 10)
    #9d121d061709b7fd Heading id="title" label="Title" level=2 bounds=(0, 50, 40 x 10)
    #b7436bfba2b6d08d Table id="grid" rows=4 cols=3 bounds=(0, 60, 400 x 10)
      #31f46521d5773778 Row id="row-1" row=1 bounds=(0, 60, 80 x 10)
        #f9330b7169fb13b1 ColumnHeader id="head" col=0 bounds=(0, 60, 40 x 10)
        #28c31f9b216fc987 Cell id="cell" row=1 col=2 bounds=(40, 60, 40 x 10)
    #77e176a6fa356e55 Dialog id="dialog" description="Confirm" modal bounds=(0, 70, 40 x 10)
    #1e314085a0bccf13 Alert id="status" live=Assertive live-atomic bounds=(0, 80, 40 x 10)
    #def1afa14e198dde GenericContainer id="decor" hidden bounds=(0, 90, 400 x 10)
      #e5d5cd87fbc75d73 Button id="inner" bounds=(0, 90, 40 x 10)
    #1e734cc359087f19 TextInput id="doc" bounds=(0, 120, 40 x 10)
      #91c59072e351bb89 TextRun value="run" synthetic bounds=(0, 120, 40 x 10)
    #0318846d4cb5c7a9 Tree id="tree" bounds=(0, 130, 400 x 10)
      #601cd4333fc5fbbd TreeItem id="item" selected=true expanded=true level=1 bounds=(0, 130, 40 x 10)
    #580f7e226f93d701 RadioGroup id="radios" bounds=(0, 140, 400 x 10)
      #687b0ca2a818d4e3 RadioButton id="radio-a" toggled=True bounds=(0, 140, 40 x 10)
    #4a4ce91f0ed7e43a ProgressIndicator id="progress" numeric=0.5 bounds=(0, 150, 40 x 10)
    #2336642c56ef90cd Toolbar id="misc" bounds=(0, 160, 400 x 10)
      #4c00e401c83dcb5a Link id="link" label="Docs" bounds=(0, 160, 40 x 10)
      #c98e301a025c410a SpinButton id="spin" bounds=(40, 160, 40 x 10)
      #aa88d6438d20a983 TimeInput id="time" bounds=(80, 160, 40 x 10)
      #8eaba561423c82b3 Tooltip id="tip" bounds=(120, 160, 40 x 10)
      #e2d343af3627efde Region id="region" bounds=(160, 160, 40 x 10)
      #37f221611116460b Navigation id="nav" bounds=(200, 160, 40 x 10)
      #cb9d4e0f32c513c7 RowGroup id="rows" bounds=(240, 160, 40 x 10)
      #a93f5575bcde66a0 AlertDialog id="alertdialog" bounds=(280, 160, 40 x 10)
`

// TestA11yDivPropertiesGoldenTrace draws the property corpus and pins
// its trace: every new property and role is visible in the Go snapshot.
func TestA11yDivPropertiesGoldenTrace(t *testing.T) {
	var increments int
	f := newA11yDivFixture(t, "A11y Properties", a11yPropertiesTree(&increments))
	snapshot := f.drawAndPublish()
	trace := snapshot.Trace()
	if trace != fixtureA11yDivPropertiesTrace {
		t.Fatalf("properties trace mismatch:\n--- got ---\n%s--- want ---\n%s", trace, fixtureA11yDivPropertiesTrace)
	}
	// No node: role(GenericContainer) is filtered (div.rs:2264-2266),
	// and role(Unknown) is the port's untyped zero role (DEVIATION: the
	// pin would publish a Role::Unknown node).
	for _, n := range snapshot.Nodes() {
		if n.AuthorID == "plain" || n.AuthorID == "unknown" {
			t.Errorf("node %q was published: %s", n.AuthorID, n)
		}
	}
	// Determinism: a second frame yields the same node set.
	again := f.drawAndPublish()
	if strings.SplitN(again.Trace(), "\n", 2)[1] != strings.SplitN(trace, "\n", 2)[1] {
		t.Fatal("the second frame's tree differs")
	}
	// The registered listener answers through the generation-checked
	// dispatch (div.rs:3086-3098).
	volume := f.node(again, "volume")
	token, err := again.Token(volume.ID)
	if err != nil {
		t.Fatal(err)
	}
	token, err = ResolveA11yToken(f.app, token)
	if err != nil {
		t.Fatal(err)
	}
	if err := DispatchA11yAction(f.app, token, A11yActionIncrement, nil); err != nil {
		t.Fatalf("Increment dispatch: %v", err)
	}
	if increments != 1 {
		t.Fatalf("increment listener runs = %d, want 1", increments)
	}
}

// TestA11yDivUnsupportedRolesPublish pins ticket 02's decision on the
// serialized update: the property corpus (which contains every Base
// role the frozen ABI lacks) serializes instead of aborting, each node
// carrying the nearest supported tag, and no state slot is invented.
func TestA11yDivUnsupportedRolesPublish(t *testing.T) {
	var increments int
	f := newA11yDivFixture(t, "A11y Publish", a11yPropertiesTree(&increments))
	snapshot := f.drawAndPublish()
	update, err := serializeA11ySnapshot(snapshot, 2)
	if err != nil {
		t.Fatalf("serialize with unsupported roles: %v (ticket 02: publication must not abort)", err)
	}
	if len(update.Nodes) != snapshot.NodeCount() {
		t.Fatalf("serialized %d nodes, want %d", len(update.Nodes), snapshot.NodeCount())
	}
	byID := map[uint64]native.A11yNodeRecord{}
	for _, r := range update.Nodes {
		if r.Role < native.A11yRoleWindow || r.Role > native.A11yRoleSlider {
			t.Errorf("node %#x has tag %d outside the rev-11 set", r.ID, r.Role)
		}
		byID[r.ID] = r
	}
	want := map[string]uint32{
		"switch":      native.A11yRoleButton,
		"tab-1":       native.A11yRoleButton,
		"combo":       native.A11yRoleButton,
		"link":        native.A11yRoleButton,
		"title":       native.A11yRoleStaticText,
		"status":      native.A11yRoleStaticText,
		"tip":         native.A11yRoleStaticText,
		"time":        native.A11yRoleTextInput,
		"dialog":      native.A11yRoleWindow,
		"alertdialog": native.A11yRoleWindow,
		"radio-a":     native.A11yRoleCheckBox,
		"spin":        native.A11yRoleSlider,
		"volume":      native.A11yRoleSlider,
		"check":       native.A11yRoleCheckBox,
		"progress":    native.A11yRoleGenericContainer,
		"tabs":        native.A11yRoleGenericContainer,
		"grid":        native.A11yRoleGenericContainer,
		"row-1":       native.A11yRoleGenericContainer,
		"cell":        native.A11yRoleGenericContainer,
		"head":        native.A11yRoleGenericContainer,
		"tree":        native.A11yRoleGenericContainer,
		"item":        native.A11yRoleGenericContainer,
		"radios":      native.A11yRoleGenericContainer,
		"misc":        native.A11yRoleGenericContainer,
		"region":      native.A11yRoleGenericContainer,
		"nav":         native.A11yRoleGenericContainer,
		"rows":        native.A11yRoleGenericContainer,
		"controls":    native.A11yRoleGenericContainer,
		"decor":       native.A11yRoleGenericContainer,
	}
	for author, tag := range want {
		node := f.node(snapshot, author)
		r, ok := byID[uint64(node.ID)]
		if !ok {
			t.Errorf("%s: not serialized", author)
			continue
		}
		if r.Role != tag {
			t.Errorf("%s (%s): tag %d, want %d", author, node.Role, r.Role, tag)
		}
	}
	// The advertised actions reach the mask (rev-11 presence set): the
	// switch's click listener advertises Click; the slider advertises
	// Increment|Decrement.
	sw := byID[uint64(f.node(snapshot, "switch").ID)]
	if sw.ActionsMask&a11yMaskBit(native.A11yActionClick) == 0 {
		t.Errorf("switch mask %#x lacks Click", sw.ActionsMask)
	}
	vol := byID[uint64(f.node(snapshot, "volume").ID)]
	if wantMask := a11yMaskBit(native.A11yActionIncrement) | a11yMaskBit(native.A11yActionDecrement); vol.ActionsMask != wantMask {
		t.Errorf("slider mask %#x, want %#x", vol.ActionsMask, wantMask)
	}
	// Hidden survives (the one state flag the ABI carries).
	if decor := byID[uint64(f.node(snapshot, "decor").ID)]; decor.Flags&native.A11yNodeFlagHidden == 0 {
		t.Error("the hidden container lost its hidden flag")
	}
}

// TestA11yPublishedRoleTable pins the nearest-role table for every role
// value (and an out-of-range one).
func TestA11yPublishedRoleTable(t *testing.T) {
	want := map[Role]Role{
		RoleUnknown: RoleGenericContainer, RoleWindow: RoleWindow, RoleGenericContainer: RoleGenericContainer,
		RoleButton: RoleButton, RoleTextInput: RoleTextInput, RoleTextRun: RoleTextRun,
		RoleStaticText: RoleStaticText, RoleCheckBox: RoleCheckBox, RoleSlider: RoleSlider,
		RoleRadioButton: RoleCheckBox, RoleSwitch: RoleButton, RoleProgressIndicator: RoleGenericContainer,
		RoleLink: RoleButton, RoleGroup: RoleGenericContainer, RoleToolbar: RoleGenericContainer,
		RoleTabList: RoleGenericContainer, RoleTab: RoleButton, RoleRegion: RoleGenericContainer,
		RoleNavigation: RoleGenericContainer, RoleHeading: RoleStaticText, RoleComboBox: RoleButton,
		RoleAlert: RoleStaticText, RoleTable: RoleGenericContainer, RoleRowGroup: RoleGenericContainer,
		RoleRow: RoleGenericContainer, RoleDialog: RoleWindow, RoleCell: RoleGenericContainer,
		RoleTreeItem: RoleGenericContainer, RoleTree: RoleGenericContainer, RoleTooltip: RoleStaticText,
		RoleTimeInput: RoleTextInput, RoleSpinButton: RoleSlider, RoleRadioGroup: RoleGenericContainer,
		RoleColumnHeader: RoleGenericContainer, RoleAlertDialog: RoleWindow, Role(9999): RoleGenericContainer,
	}
	for role := RoleUnknown; role <= RoleAlertDialog; role++ {
		if _, ok := want[role]; !ok {
			t.Errorf("role %s has no expected mapping", role)
		}
		if strings.HasPrefix(role.String(), "Role(") {
			t.Errorf("role %d has no name", role)
		}
	}
	for role, published := range want {
		if got := a11yPublishedRole(role); got != published {
			t.Errorf("a11yPublishedRole(%s) = %s, want %s", role, got, published)
		}
		if _, err := a11yRoleTag(role); err != nil {
			t.Errorf("a11yRoleTag(%s): %v", role, err)
		}
	}
	if RoleLabel != RoleStaticText {
		t.Error("RoleLabel is not the static text role")
	}
}

// fixtureA11yDivActiveDescendantTrace is the golden trace of the
// active-descendant corpus with the container focused (port-recorded).
const fixtureA11yDivActiveDescendantTrace = `window "Listbox" gen=1 seq=3 nodes=4 focus=#a1724d45acfef40e
#0000000000000000 Window label="Listbox" bounds=(0, 0, 0 x 0)
  #3e53920550143f55 Group id="listbox" label="Fruit" focus-handle=9223372036854775809 actions=[Focus] bounds=(0, 0, 400 x 600)
    #dbf179ed6487f4c6 Button id="opt-apple" label="Apple" selected=false bounds=(0, 0, 40 x 10)
    #a1724d45acfef40e Button id="opt-pear" label="Pear" selected=true bounds=(0, 10, 40 x 10)
`

// TestA11yDivActiveDescendantGoldenTrace drives aria_active_descendant
// through real prepaint (div.rs:2821-2827): ignored while the container
// is not focused, honored once it is.
func TestA11yDivActiveDescendantGoldenTrace(t *testing.T) {
	var focus FocusHandle
	f := newA11yDivFixture(t, "Listbox", func(w *Window, cx *Context[a11yDivView]) AnyElement {
		return ax(Div().ID("listbox").AccessibilityID("listbox").Role(RoleGroup).AriaLabel("Fruit").TrackFocus(focus).FlexCol().SizeFull().
			Child(ax(leaf("opt-apple").Role(RoleButton).AriaLabel("Apple").AriaSelected(false))).
			Child(ax(leaf("opt-pear").Role(RoleButton).AriaLabel("Pear").AriaSelected(true).AriaActiveDescendant())))
	})
	// The build closure first runs at the first draw below, so the
	// handle is allocated before any frame reads it.
	focus = f.app.FocusHandle()
	unfocused := f.drawAndPublish()
	if unfocused.Focus() != RootA11yNodeID {
		t.Fatalf("unfocused container: focus = %s, want the root (the claim is gated)\n%s", unfocused.Focus(), unfocused.Trace())
	}
	if d := unfocused.Diagnostics(); len(d) != 0 {
		t.Fatalf("diagnostics while unfocused: %v", d)
	}
	f.window.Focus(focus)
	focused := f.drawAndPublish()
	if focused.Focus() != f.node(focused, "opt-pear").ID {
		t.Fatalf("focused container: focus = %s, want the active descendant\n%s", focused.Focus(), focused.Trace())
	}
	if trace := focused.Trace(); trace != fixtureA11yDivActiveDescendantTrace {
		t.Fatalf("active-descendant trace mismatch:\n--- got ---\n%s--- want ---\n%s", trace, fixtureA11yDivActiveDescendantTrace)
	}
}
