package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// popover.rs tests (6/6 ported, U:popover.rs:378-589).

// ovInit runs the base initialization (the upstream crate::init,
// U:lib.rs:227-245): the popover's key bindings among it.
func ovInit(app *gpui.App) {
	Init(app)
}

// browser-core 21 (spec risk, research/browser-parity 06 §1: "Base
// popover dismissal faults"): HTML popovers keep the parent open when
// the user clicks inside a nested child popover — the child is a DOM
// descendant of the parent, so light dismiss treats it as inside. Base
// dismissed every popover independently on a mousedown outside its own
// hitbox, so a press inside the child (rendered deferred, outside the
// parent's box) closed the parent. This test pins the fixed behaviour:
// a click inside the child keeps BOTH popovers open.
func TestPopoverClickInsideNestedChildKeepsTheParentOpen(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewPopover("parent-popover").
			TriggerWith(func(bool, *gpui.Window, *gpui.App) gpui.AnyElement {
				return gpui.Div().Flex().DebugSelector("parent-trigger").Size(gpui.Px(40)).IntoElement()
			}).
			Content(func(*PopoverState, *gpui.Window, *gpui.Context[PopoverState]) any {
				return gpui.Div().Flex().DebugSelector("parent-content").Size(gpui.Px(40)).
					Child(gpui.OwnRecipe(NewPopover("child-popover").
						TriggerWith(func(bool, *gpui.Window, *gpui.App) gpui.AnyElement {
							return gpui.Div().Flex().DebugSelector("child-trigger").Size(gpui.Px(40)).IntoElement()
						}).
						Content(func(*PopoverState, *gpui.Window, *gpui.Context[PopoverState]) any {
							return gpui.Div().Flex().DebugSelector("child-content").Size(gpui.Px(40))
						})))
			}))
	})
	ovDraw(t, cx)

	// Open the parent, then the child: the child's trigger renders inside
	// the parent's content, and the child's content renders deferred below
	// it — outside the parent's 40x40 box at (0,40).
	cx.SimulateClick(ovPoint(20, 10), gpui.NoModifiers())
	ovDraw(t, cx)
	ovDraw(t, cx)
	if !ovHasBounds(cx, "parent-content") {
		t.Fatal("a click on the parent trigger did not open the popover")
	}
	cx.SimulateClick(ovPoint(20, 60), gpui.NoModifiers())
	ovDraw(t, cx)
	ovDraw(t, cx)
	if !ovHasBounds(cx, "child-content") {
		t.Fatal("a click on the child trigger inside the parent did not open the child popover")
	}

	// The fault: a click inside the child's content box — outside the
	// parent's box — must not dismiss the parent (HTML popover light
	// dismiss: the child popover is inside the parent).
	cx.SimulateClick(ovPoint(20, 100), gpui.NoModifiers())
	ovDraw(t, cx)

	if !ovHasBounds(cx, "child-content") {
		t.Fatal("a click inside the child popover closed the child")
	}
	if !ovHasBounds(cx, "parent-content") {
		t.Fatal("a click inside the nested child popover dismissed the parent (HTML popovers keep it open)")
	}
}

// U:popover.rs:384-416: the deferred-popover liveness tests are gone
// with the liveness list (browser-core ticket 72: the top-layer entry's
// registration is the popup's liveness).

// popoverHarness is U:popover.rs:418-456 PopoverHarness.
func popoverHarness(changes *[]bool, defaultOpen bool) func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
	return func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewPopover("base-popover").
			DefaultOpen(defaultOpen).
			TriggerWith(func(bool, *gpui.Window, *gpui.App) gpui.AnyElement {
				return gpui.Div().Flex().Child("Open").IntoElement()
			}).
			Content(func(*PopoverState, *gpui.Window, *gpui.Context[PopoverState]) any {
				return gpui.Div().Flex().DebugSelector("base-popover-content").Size(gpui.Px(40))
			}).
			OnOpenChange(func(open bool, _ *gpui.Window, _ *gpui.App) { *changes = append(*changes, open) }))
	}
}

// U:popover.rs:458-479
func TestPopoverUnstyledPopoverOwnsPointerOpenAndOutsideDismiss(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	var changes []bool
	_, cx := openTestView(t, ta, popoverHarness(&changes, false))
	ovDraw(t, cx)

	cx.SimulateClick(ovPoint(20, 10), gpui.NoModifiers())
	ovDraw(t, cx)
	if !ovHasBounds(cx, "base-popover-content") {
		t.Fatal("a click on the trigger did not open the popover")
	}

	cx.SimulateClick(ovPoint(300, 300), gpui.NoModifiers())
	ovDraw(t, cx)
	if ovHasBounds(cx, "base-popover-content") {
		t.Fatal("a click outside did not dismiss the popover")
	}
	if !slices.Equal(changes, []bool{true, false}) {
		t.Fatalf("open changes = %v, want [true false]", changes)
	}
}

// U:popover.rs:481-491
func TestPopoverDefaultOpenRendersContentWithoutActivation(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	var changes []bool
	_, cx := openTestView(t, ta, popoverHarness(&changes, true))
	ovDraw(t, cx)
	ovDraw(t, cx)
	if !ovHasBounds(cx, "base-popover-content") {
		t.Fatal("a default-open popover rendered no content")
	}
}

// ovRecordingCall is one recorded Selectable call.
type ovRecordingCall struct {
	name  string
	value bool
}

// ovRecordingTrigger keeps open and selected apart, like a downstream
// sidebar row (U:popover.rs:493-529 RecordingTrigger).
type ovRecordingTrigger struct {
	calls    *[]ovRecordingCall
	selected bool
	open     bool
}

func (r *ovRecordingTrigger) Selected(selected bool) *ovRecordingTrigger {
	*r.calls = append(*r.calls, ovRecordingCall{"selected", selected})
	r.selected = selected
	return r
}

func (r *ovRecordingTrigger) IsSelected() bool { return r.selected }

func (r *ovRecordingTrigger) Open(open bool) *ovRecordingTrigger {
	*r.calls = append(*r.calls, ovRecordingCall{"open", open})
	r.open = open
	return r
}

func (r *ovRecordingTrigger) IsOpen() bool { return r.open }

func (r *ovRecordingTrigger) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	return gpui.Div().Flex().Size(gpui.Px(40)).Child("Open").IntoElement()
}

// U:popover.rs:547-570
func TestPopoverAnOpenPopoverTellsItsTriggerItIsOpenNotSelected(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	var calls []ovRecordingCall
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewPopover("recording-popover").
			Trigger(&ovRecordingTrigger{calls: &calls}).
			Content(func(*PopoverState, *gpui.Window, *gpui.Context[PopoverState]) any {
				return gpui.Div().Flex().Size(gpui.Px(40))
			}))
	})
	ovDraw(t, cx)
	calls = calls[:0]

	cx.SimulateClick(ovPoint(20, 10), gpui.NoModifiers())
	ovDraw(t, cx)

	if !slices.Contains(calls, ovRecordingCall{"open", true}) {
		t.Fatalf("an open popover marks its trigger open, got %v", calls)
	}
	for _, call := range calls {
		if call.name == "selected" {
			t.Fatalf("opening must not touch the trigger's own selection, got %v", calls)
		}
	}
}

// U:popover.rs:572-588
func TestPopoverKeyboardActivationOpensThePopover(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	triggerFocus := ta.App().FocusHandle()
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewPopover("keyboard-popover").
			Trigger(NewButton("keyboard-trigger").TrackFocus(triggerFocus).Child("Open")).
			Content(func(*PopoverState, *gpui.Window, *gpui.Context[PopoverState]) any {
				return gpui.Div().Flex().DebugSelector("keyboard-popover-content").Size(gpui.Px(40))
			}))
	})
	ovDraw(t, cx)

	cx.Update(func(w *gpui.Window, _ *gpui.App) { triggerFocus.Focus(w) })
	cx.SimulateKeystrokes("enter")
	ovDraw(t, cx)

	if !ovHasBounds(cx, "keyboard-popover-content") {
		t.Fatal("enter on the focused trigger did not open the popover")
	}
}

// browser-core ticket 72: Escape closes the popover through the
// top-layer entry's close watcher (ticket 22 — popover=auto entries
// create one per show, html_element.cc:2146-2157): the key never
// dispatches an action, the default Escape arm signals the newest
// watcher, and the entry's toggle runs the state's own close (the
// focus restore and the open-change announcement).
func TestPopoverEscapeClosesThroughTheCloseWatcher(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(ovInit)
	var changes []bool
	_, cx := openTestView(t, ta, popoverHarness(&changes, false))
	ovDraw(t, cx)

	cx.SimulateClick(ovPoint(20, 10), gpui.NoModifiers())
	ovDraw(t, cx)
	if !ovHasBounds(cx, "base-popover-content") {
		t.Fatal("a click on the trigger did not open the popover")
	}

	// Escape with focus inside the popover's content: the content has no
	// Escape listener, so the default action reaches the watcher.
	ovDraw(t, cx)
	cx.SimulateKeystrokes("escape")
	ovDraw(t, cx)
	if ovHasBounds(cx, "base-popover-content") {
		t.Fatal("escape did not close the popover through the close watcher")
	}
	if !slices.Equal(changes, []bool{true, false}) {
		t.Fatalf("open changes = %v, want [true false]", changes)
	}
}
