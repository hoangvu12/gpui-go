package base

import (
	"slices"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// toast.rs tests (11/11 ported, U:toast.rs:664-1014).

func toastIDs[T any](entries []ToastEntry[string, T]) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ID)
	}
	return out
}

func toastTimeout(d time.Duration) ToastOptions {
	return ToastOptions{Timeout: gpui.Some(d)}
}

// U:toast.rs:669-696
func TestToastManagerPausesTimeoutAndRemovesOnlyAfterExit(t *testing.T) {
	motion := ToastMotionSonner()
	start := time.Now()
	m := NewToastManager[string, int](motion)
	m.Push("a", 1, toastTimeout(5*time.Second), start)
	m.Advance(start.Add(motion.Duration), false)
	m.Advance(start.Add(motion.Duration+4*time.Second), true)
	if status := m.Iter()[0].Status; status != ToastPresent {
		t.Fatalf("status = %v, want Present", status)
	}
	ending := m.Advance(start.Add(motion.Duration+9*time.Second), false)
	if !slices.Equal(ending.Ending, []string{"a"}) || len(ending.Removed) != 0 {
		t.Fatalf("ending = %+v", ending)
	}
	removed := m.Advance(start.Add(motion.Duration+motion.ExitDuration+9*time.Second), false)
	if !slices.Equal(removed.Removed, []ToastRemoved[string, int]{{ID: "a", Value: 1}}) {
		t.Fatalf("removed = %+v", removed.Removed)
	}
}

// U:toast.rs:698-710
func TestToastManagerLimitKeepsEndingToastsMounted(t *testing.T) {
	now := time.Now()
	m := NewToastManager[string, string](ToastMotionSonner())
	for _, id := range []string{"a", "b", "c"} {
		m.Push(id, id, ToastOptions{}, now)
	}
	m.Dismiss("a", now)
	if got := toastIDs(m.Visible(1)); !slices.Equal(got, []string{"a", "c"}) {
		t.Fatalf("visible = %v, want [a c]", got)
	}
}

// U:toast.rs:712-726
func TestToastManagerReplacesDuplicateIDsAsTheNewestToast(t *testing.T) {
	now := time.Now()
	m := NewToastManager[string, int](ToastMotionSonner())
	m.Push("a", 1, ToastOptions{}, now)
	m.Push("b", 2, ToastOptions{}, now)
	if replaced, ok := m.Push("a", 3, ToastOptions{}, now); !ok || replaced != 1 {
		t.Fatalf("replaced = %v, %v", replaced, ok)
	}
	var got []ToastRemoved[string, int]
	for _, e := range m.Iter() {
		got = append(got, ToastRemoved[string, int]{ID: e.ID, Value: e.Value})
	}
	if want := []ToastRemoved[string, int]{{"b", 2}, {"a", 3}}; !slices.Equal(got, want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
}

// U:toast.rs:728-743
func TestToastManagerResetsClockWhenReusedAfterBecomingEmpty(t *testing.T) {
	motion := ToastMotionSonner()
	start := time.Now()
	m := NewToastManager[string, int](motion)
	m.Push("old", 1, ToastOptions{}, start)
	m.Dismiss("old", start)
	m.Advance(start.Add(motion.ExitDuration), false)
	later := start.Add(60 * time.Second)
	m.Push("new", 2, ToastOptions{}, later)
	m.Advance(later.Add(50*time.Millisecond), false)
	if status := m.Iter()[0].Status; status != ToastStarting {
		t.Fatalf("status = %v, want Starting", status)
	}
}

// U:toast.rs:745-761
func TestToastNewlyPushedToastDoesNotInheritExistingEntryClock(t *testing.T) {
	motion := ToastMotionSonner()
	start := time.Now()
	m := NewToastManager[string, int](motion)
	m.Push("old", 1, ToastOptions{}, start)
	later := start.Add(60 * time.Second)
	m.Push("new", 2, ToastOptions{}, later)
	m.Advance(later.Add(50*time.Millisecond), false)
	for _, e := range m.Iter() {
		if e.ID == "new" && e.Status != ToastStarting {
			t.Fatalf("new status = %v, want Starting", e.Status)
		}
	}
}

// U:toast.rs:763-794
func TestToastStackGeometryAnchorsNewestItemAndSupportsVariableHeights(t *testing.T) {
	heights := []float32{40, 60, 80}
	collapsed, expanded, top := toastStackGeometry(heights, 12, 12, false)
	if collapsed != 104 || expanded != 204 {
		t.Fatalf("collapsed=%v expanded=%v", collapsed, expanded)
	}
	if want := []toastStackOffset{{24, 164}, {12, 92}, {0, 0}}; !slices.Equal(top, want) {
		t.Fatalf("top = %v, want %v", top, want)
	}
	_, _, bottom := toastStackGeometry(heights, 12, 12, true)
	if want := []toastStackOffset{{40, 0}, {32, 52}, {24, 124}}; !slices.Equal(bottom, want) {
		t.Fatalf("bottom = %v, want %v", bottom, want)
	}

	tallBehind := []float32{180, 60}
	collapsed, _, top = toastStackGeometry(tallBehind, 14, 14, false)
	if collapsed != 194 || top[0].collapsed != 14 || top[0].collapsed+tallBehind[0] > collapsed || top[0].collapsed-top[1].collapsed != 14 {
		t.Fatalf("tall behind top: collapsed=%v offsets=%v", collapsed, top)
	}
	_, _, bottom = toastStackGeometry(tallBehind, 14, 14, true)
	if bottom[0].collapsed < 0 || bottom[0].collapsed+tallBehind[0] > collapsed ||
		(bottom[1].collapsed+tallBehind[1])-(bottom[0].collapsed+tallBehind[0]) != 14 {
		t.Fatalf("tall behind bottom: offsets=%v", bottom)
	}
}

// U:toast.rs:796-807
func TestToastExposesAlertSemantics(t *testing.T) {
	ta := gpui.NewTestApp()
	cx := ta.AddEmptyWindow()
	cx.Update(func(*gpui.Window, *gpui.App) {
		role, ok := NewToast("toast").render().A11yRole()
		if !ok || role != gpui.RoleAlert {
			t.Fatalf("role = %v (%v), want Alert", role, ok)
		}
	})
}

// U:toast.rs:809-836
func TestToastStackBootstrapsMeasurementWithoutZeroHeightClipping(t *testing.T) {
	ta := gpui.NewTestApp()
	state := DefaultToastStackState()
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewToastStack("stack", state).
			W(gpui.Px(300)).
			Item("first", gpui.Div().Flex().H(gpui.Px(80)).Child("Visible")))
	})
	ovDraw(t, cx)
	if h, ok := state.shared.heights[gpui.NameElementID("first").String()]; !ok || h != 80 {
		t.Fatalf("measured height = %v (%v), want 80", h, ok)
	}
}

// U:toast.rs:838-881
func TestToastStackExpandsForHoverAndFocus(t *testing.T) {
	ta := gpui.NewTestApp()
	state := DefaultToastStackState()
	focus := ta.App().FocusHandle().TabStop(true)
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.OwnRecipe(NewToastStack("stack", state).
			FocusHandle(focus).
			W(gpui.Px(300)).
			Item("first", gpui.Div().Flex().H(gpui.Px(80))))
	})
	ovDraw(t, cx)

	for i, step := range []struct {
		at       gpui.Point
		expanded bool
	}{{ovPoint(10, 10), true}, {ovPoint(400, 400), false}, {ovPoint(10, 10), true}, {ovPoint(400, 400), false}} {
		cx.SimulateMouseMove(step.at, nil, gpui.NoModifiers())
		ovDraw(t, cx)
		if state.IsExpanded() != step.expanded {
			t.Fatalf("step %d: expanded = %v, want %v", i, state.IsExpanded(), step.expanded)
		}
	}

	cx.Update(func(w *gpui.Window, _ *gpui.App) {
		focus.Focus(w)
		if _, err := gpui.DrawWindowFrame(w); err != nil {
			t.Fatal(err)
		}
	})
	if !state.IsExpanded() {
		t.Fatal("focus inside the stack's focus scope did not expand it")
	}
}

// toastReflowHarness renders a stack whose second item appears on
// demand (U:toast.rs:884-904, 942-969).
type toastReflowHarness struct {
	state      ToastStackState
	showSecond bool
}

// U:toast.rs:883-940: a keyed stack reflows from the current visual
// position, carrying the springs across the change.
func TestToastKeyedStackReflowMovesFromTheCurrentVisualPosition(t *testing.T) {
	ta := gpui.NewTestApp()
	h := &toastReflowHarness{state: DefaultToastStackState()}
	view, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		stack := NewToastStack("stack", h.state).
			W(gpui.Px(300)).
			Item("first", gpui.Div().Flex().DebugSelector("first-toast").H(gpui.Px(40)))
		if h.showSecond {
			stack.Item("second", gpui.Div().Flex().DebugSelector("second-toast").H(gpui.Px(80)))
		}
		return gpui.OwnRecipe(stack)
	})
	ovDraw(t, cx)
	ovDraw(t, cx)
	initialY := debugBounds(t, cx, "first-toast").Origin.Y

	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		view.Update(app, func(_ *testView, vcx *gpui.Context[testView]) {
			h.showSecond = true
			vcx.Notify()
		})
	})
	cx.RunUntilParked()
	if y := debugBounds(t, cx, "first-toast").Origin.Y; y != initialY {
		t.Fatalf("first reflow y = %v, want the initial %v", y, initialY)
	}

	cx.AdvanceClock(200)
	ovDraw(t, cx)
	middleY := debugBounds(t, cx, "first-toast").Origin.Y
	if !(middleY > initialY && middleY < initialY+14) {
		t.Fatalf("middle y = %v, want strictly between %v and %v", middleY, initialY, initialY+14)
	}

	cx.AdvanceClock(200)
	ovDraw(t, cx)
	if y := debugBounds(t, cx, "first-toast").Origin.Y; y != initialY+14 {
		t.Fatalf("settled y = %v, want %v", y, initialY+14)
	}
	first, second := debugBounds(t, cx, "first-toast"), debugBounds(t, cx, "second-toast")
	if !(first.Size.Width < second.Size.Width) {
		t.Fatalf("collapsed layer width %v is not narrower than the front %v", first.Size.Width, second.Size.Width)
	}
}

// U:toast.rs:942-1013: a bottom-anchored stack moves up without a jump
// in the opposite direction.
func TestToastBottomStackReflowMovesUpWithoutAnOppositeDirectionJump(t *testing.T) {
	ta := gpui.NewTestApp()
	h := &toastReflowHarness{state: DefaultToastStackState()}
	view, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		stack := NewToastStack("bottom-stack", h.state).
			Placement(gpui.AnchorBottomRight).
			Absolute().
			Bottom0().
			W(gpui.Px(300)).
			Item("bottom-first", gpui.Div().Flex().DebugSelector("bottom-first-toast").H(gpui.Px(40)))
		if h.showSecond {
			stack.Item("bottom-second", gpui.Div().Flex().H(gpui.Px(80)))
		}
		return gpui.Div().Flex().Relative().Size(gpui.Px(300)).Child(stack).IntoElement()
	})
	ovDraw(t, cx)
	ovDraw(t, cx)
	// Both the baseline and the final reading are taken settled
	// (U:toast.rs:979-983).
	cx.AdvanceClock(time.Second.Milliseconds())
	ovDraw(t, cx)
	initialY := debugBounds(t, cx, "bottom-first-toast").Origin.Y

	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		view.Update(app, func(_ *testView, vcx *gpui.Context[testView]) {
			h.showSecond = true
			vcx.Notify()
		})
	})
	cx.RunUntilParked()
	if y := debugBounds(t, cx, "bottom-first-toast").Origin.Y; y != initialY {
		t.Fatalf("first reflow y = %v, want the initial %v", y, initialY)
	}

	cx.AdvanceClock(200)
	ovDraw(t, cx)
	middleY := debugBounds(t, cx, "bottom-first-toast").Origin.Y
	if !(middleY < initialY && middleY >= initialY-14) {
		t.Fatalf("middle=%v, initial=%v", middleY, initialY)
	}

	cx.AdvanceClock(time.Second.Milliseconds())
	ovDraw(t, cx)
	if y := debugBounds(t, cx, "bottom-first-toast").Origin.Y; y != initialY-14 {
		t.Fatalf("settled y = %v, want %v", y, initialY-14)
	}
}
