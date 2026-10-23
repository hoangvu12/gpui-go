package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// popover.rs tests (6/6 ported, U:popover.rs:378-589).

// ovInit runs the base initialization plus the overlay initializers this
// ticket adds (the upstream crate::init, U:lib.rs:227-245). lib.go's Init
// does not call them yet (wiring is outside this ticket's files).
func ovInit(app *gpui.App) {
	Init(app)
}

// U:popover.rs:384-400: a state collected while open closes the deferred
// interaction context.
func TestPopoverAStateDroppedWhileOpenClosesTheDeferredContext(t *testing.T) {
	ta := gpui.NewTestApp()
	var state gpui.Entity[PopoverState]
	ta.Update(func(app *gpui.App) {
		GlobalStateInit(app)
		state = gpui.NewEntity(app, ta.RootScope(), func(s *PopoverState, cx *gpui.Context[PopoverState]) {
			*s = NewPopoverState(false, cx)
		})
		state.Update(app, func(s *PopoverState, cx *gpui.Context[PopoverState]) { s.SetOpen(true, cx) })
		if !GlobalStateIsInDeferredContext(app) {
			t.Fatal("an open state did not register the deferred context")
		}
	})
	ta.Update(func(*gpui.App) { state.Release() })
	ta.Update(func(app *gpui.App) {
		if GlobalStateIsInDeferredContext(app) {
			t.Fatal("a dropped open state left the deferred context open")
		}
	})
}

// U:popover.rs:402-416
func TestPopoverOpenStateRegistersAndUnregistersDeferredContext(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(func(app *gpui.App) {
		GlobalStateInit(app)
		state := gpui.NewEntity(app, ta.RootScope(), func(s *PopoverState, cx *gpui.Context[PopoverState]) {
			*s = NewPopoverState(false, cx)
		})
		isOpen := func() bool { return state.Read(app, func(s *PopoverState, _ *gpui.App) bool { return s.IsOpen() }) }

		state.Update(app, func(s *PopoverState, cx *gpui.Context[PopoverState]) { s.SetOpen(true, cx) })
		if !isOpen() || !GlobalStateIsInDeferredContext(app) {
			t.Fatal("opening did not register")
		}
		state.Update(app, func(s *PopoverState, cx *gpui.Context[PopoverState]) { s.SetOpen(false, cx) })
		if isOpen() || GlobalStateIsInDeferredContext(app) {
			t.Fatal("closing did not unregister")
		}
	})
}

// popoverHarness is U:popover.rs:418-456 PopoverHarness.
func popoverHarness(changes *[]bool, defaultOpen bool) func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
	return func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewPopover("base-popover").
			DefaultOpen(defaultOpen).
			TriggerWith(func(bool, *gpui.Window, *gpui.App) gpui.AnyElement {
				return gpui.Div().Child("Open").IntoElement()
			}).
			Content(func(*PopoverState, *gpui.Window, *gpui.Context[PopoverState]) any {
				return gpui.Div().DebugSelector("base-popover-content").Size(gpui.Px(40))
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
	return gpui.Div().Size(gpui.Px(40)).Child("Open").IntoElement()
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
				return gpui.Div().Size(gpui.Px(40))
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
				return gpui.Div().DebugSelector("keyboard-popover-content").Size(gpui.Px(40))
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
