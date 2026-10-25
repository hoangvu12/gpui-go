package base

import (
	"slices"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:nav_stack.rs (7/7).

// navTestPage is U:nav_stack.rs:541-547 Page.
type navTestPage struct{}

func (*navTestPage) Render(*gpui.Window, *gpui.Context[navTestPage]) gpui.AnyElement {
	return gpui.Div().Flex().IntoElement()
}

// navPage is U:nav_stack.rs:549-551 page.
func navPage(ta *gpui.TestApp) gpui.View {
	var view gpui.View
	ta.Update(func(app *gpui.App) {
		view = gpui.ViewOf(gpui.NewEntity(app, ta.RootScope(), func(*navTestPage, *gpui.Context[navTestPage]) {}))
	})
	return view
}

func navPages(ta *gpui.TestApp, n int) []gpui.View {
	pages := make([]gpui.View, n)
	for i := range pages {
		pages[i] = navPage(ta)
	}
	return pages
}

// sameView is AnyView's PartialEq: the entity identity.
func sameView(a, b gpui.View) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	ia, _ := a.EntityID()
	ib, _ := b.EntityID()
	return ia == ib
}

func sameViews(a, b []gpui.View) bool {
	return slices.EqualFunc(a, b, sameView)
}

// navEventRecorder collects the stack's events (the upstream App-level
// cx.subscribe(...).detach()).
type navEventRecorder struct {
	events *[]NavStackEvent
}

// navStack is U:nav_stack.rs:553-567 stack.
func navStack(t *testing.T) (*gpui.TestApp, gpui.Entity[NavStackState], *[]NavStackEvent) {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	events := new([]NavStackEvent)
	var stack gpui.Entity[NavStackState]
	ta.Update(func(app *gpui.App) {
		stack = gpui.NewEntity(app, ta.RootScope(), func(s *NavStackState, _ *gpui.Context[NavStackState]) {
			*s = NewNavStackState()
		})
		gpui.NewEntity(app, ta.RootScope(), func(r *navEventRecorder, cx *gpui.Context[navEventRecorder]) {
			r.events = events
			NavStackEvents.Subscribe(cx, stack, func(r *navEventRecorder, _ gpui.Entity[NavStackState], e *NavStackEvent, _ *gpui.Context[navEventRecorder]) {
				*r.events = append(*r.events, *e)
			})
		})
	})
	ta.RunUntilParked()
	return ta, stack, events
}

func updateNav[R any](ta *gpui.TestApp, stack gpui.Entity[NavStackState], f func(*NavStackState, *gpui.Context[NavStackState]) R) R {
	var out R
	ta.Update(func(app *gpui.App) { out = stack.UpdateWith(app, f) })
	ta.RunUntilParked()
	return out
}

func readNav(ta *gpui.TestApp, stack gpui.Entity[NavStackState], f func(*NavStackState)) {
	ta.Update(func(app *gpui.App) {
		stack.Read(app, func(s *NavStackState, _ *gpui.App) struct{} { f(s); return struct{}{} })
	})
}

func pushNav(ta *gpui.TestApp, stack gpui.Entity[NavStackState], view gpui.View, motion NavMotion) {
	updateNav(ta, stack, func(s *NavStackState, cx *gpui.Context[NavStackState]) struct{} {
		s.Push(view, motion, cx)
		return struct{}{}
	})
}

func popNav(ta *gpui.TestApp, stack gpui.Entity[NavStackState], motion NavMotion) gpui.View {
	return updateNav(ta, stack, func(s *NavStackState, cx *gpui.Context[NavStackState]) gpui.View {
		v, _ := s.Pop(motion, cx)
		return v
	})
}

// U:nav_stack.rs:569-616
func TestNavStackPushAndPopKeepTheRoot(t *testing.T) {
	ta, stack, events := navStack(t)
	root, second := navPage(ta), navPage(ta)

	pushNav(ta, stack, root, NavMotionAnimated)
	readNav(ta, stack, func(s *NavStackState) {
		if s.transit != nil {
			t.Fatal("a push into an empty stack transitions")
		}
	})

	pushNav(ta, stack, second, NavMotionAnimated)
	readNav(ta, stack, func(s *NavStackState) {
		if s.Depth() != 2 {
			t.Fatalf("depth = %d", s.Depth())
		}
		if cur, _ := s.Current(); !sameView(cur, second) {
			t.Fatal("current is not the second page")
		}
		if s.transit == nil {
			t.Fatal("push over a view transitions")
		}
		if s.transit.operation != NavOperationPush || !sameView(s.transit.outgoing, root) {
			t.Fatalf("transit = %+v", *s.transit)
		}
	})

	if popped := popNav(ta, stack, NavMotionAnimated); !sameView(popped, second) {
		t.Fatal("pop did not return the second page")
	}
	readNav(ta, stack, func(s *NavStackState) {
		if !sameViews(s.Views(), []gpui.View{root}) {
			t.Fatal("views != [root]")
		}
		if s.transit == nil {
			t.Fatal("pop transitions")
		}
		if s.transit.operation != NavOperationPop || !sameView(s.transit.outgoing, second) {
			t.Fatalf("transit = %+v", *s.transit)
		}
		if s.transit.index != 1 {
			t.Fatalf("the popped view keeps its position: index = %d", s.transit.index)
		}
	})

	if popped := popNav(ta, stack, NavMotionAnimated); popped != nil {
		t.Fatal("the root was popped")
	}
	readNav(ta, stack, func(s *NavStackState) {
		if s.Depth() != 1 {
			t.Fatalf("depth = %d", s.Depth())
		}
	})
	if want := []NavStackEvent{NavStackPushed, NavStackPushed, NavStackPopped}; !slices.Equal(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
}

// U:nav_stack.rs:618-643
func TestNavStackPopToRootReturnsEverythingAboveIt(t *testing.T) {
	ta, stack, _ := navStack(t)
	pages := navPages(ta, 3)
	for _, view := range pages {
		pushNav(ta, stack, view, NavMotionAnimated)
	}

	popped := updateNav(ta, stack, func(s *NavStackState, cx *gpui.Context[NavStackState]) []gpui.View {
		return s.PopToRoot(NavMotionAnimated, cx)
	})
	if !sameViews(popped, pages[1:]) {
		t.Fatal("pop_to_root did not return pages[1..]")
	}
	readNav(ta, stack, func(s *NavStackState) {
		if !sameViews(s.Views(), pages[:1]) {
			t.Fatal("views != pages[..1]")
		}
		if s.transit == nil {
			t.Fatal("pop_to_root transitions")
		}
		if !sameView(s.transit.outgoing, pages[2]) {
			t.Fatal("the outgoing view is not the previous top")
		}
		if s.transit.index != 2 {
			t.Fatalf("the previous top keeps its position: index = %d", s.transit.index)
		}
	})
	again := updateNav(ta, stack, func(s *NavStackState, cx *gpui.Context[NavStackState]) []gpui.View {
		return s.PopToRoot(NavMotionAnimated, cx)
	})
	if len(again) != 0 {
		t.Fatalf("second pop_to_root returned %d views", len(again))
	}
}

// U:nav_stack.rs:645-690
func TestNavStackReplaceSwapsTheTopAndPushesIntoAnEmptyStack(t *testing.T) {
	ta, stack, events := navStack(t)
	first, second := navPage(ta), navPage(ta)

	replace := func(view gpui.View) gpui.View {
		return updateNav(ta, stack, func(s *NavStackState, cx *gpui.Context[NavStackState]) gpui.View {
			v, _ := s.Replace(view, NavMotionAnimated, cx)
			return v
		})
	}
	if v := replace(first); v != nil {
		t.Fatal("replace into an empty stack returned a view")
	}
	if v := replace(second); !sameView(v, first) {
		t.Fatal("replace did not return the first page")
	}
	readNav(ta, stack, func(s *NavStackState) {
		if !sameViews(s.Views(), []gpui.View{second}) {
			t.Fatal("views != [second]")
		}
		if s.transit == nil {
			t.Fatal("replace transitions")
		}
		if s.transit.operation != NavOperationReplace || !sameView(s.transit.outgoing, first) {
			t.Fatalf("transit = %+v", *s.transit)
		}
		if s.transit.index != 0 {
			t.Fatalf("the replaced view sat where the new one sits: index = %d", s.transit.index)
		}
	})

	updateNav(ta, stack, func(s *NavStackState, cx *gpui.Context[NavStackState]) struct{} {
		s.Clear(cx)
		return struct{}{}
	})
	readNav(ta, stack, func(s *NavStackState) {
		if !s.IsEmpty() || s.transit != nil {
			t.Fatal("clear left views or a transit")
		}
	})
	if want := []NavStackEvent{NavStackPushed, NavStackReplaced, NavStackCleared}; !slices.Equal(*events, want) {
		t.Fatalf("events = %v, want %v", *events, want)
	}
}

// U:nav_stack.rs:692-740
func TestNavStackPoppedViewsWaitForForwardUntilTheNextPush(t *testing.T) {
	ta, stack, events := navStack(t)
	pages := navPages(ta, 3)
	for _, view := range pages {
		pushNav(ta, stack, view, NavMotionAnimated)
	}
	forward := func() gpui.View {
		return updateNav(ta, stack, func(s *NavStackState, cx *gpui.Context[NavStackState]) gpui.View {
			v, _ := s.Forward(NavMotionAnimated, cx)
			return v
		})
	}
	if forward() != nil {
		t.Fatal("forward with nothing popped returned a view")
	}

	updateNav(ta, stack, func(s *NavStackState, cx *gpui.Context[NavStackState]) struct{} {
		s.Pop(NavMotionAnimated, cx)
		s.Pop(NavMotionAnimated, cx)
		return struct{}{}
	})
	readNav(ta, stack, func(s *NavStackState) {
		if s.Depth() != 1 {
			t.Fatalf("depth = %d", s.Depth())
		}
		if !sameViews(s.ForwardViews(), pages[1:]) {
			t.Fatal("forward views != pages[1..]")
		}
	})

	if back := forward(); !sameView(back, pages[1]) {
		t.Fatal("forward did not bring back pages[1]")
	}
	readNav(ta, stack, func(s *NavStackState) {
		if cur, _ := s.Current(); !sameView(cur, pages[1]) {
			t.Fatal("current != pages[1]")
		}
		if s.transit == nil {
			t.Fatal("forward transitions like a push")
		}
		if s.transit.operation != NavOperationPush || !sameView(s.transit.outgoing, pages[0]) {
			t.Fatalf("transit = %+v", *s.transit)
		}
		if n := len(s.ForwardViews()); n != 1 {
			t.Fatalf("forward views = %d", n)
		}
	})

	pushNav(ta, stack, navPage(ta), NavMotionAnimated)
	readNav(ta, stack, func(s *NavStackState) {
		if n := len(s.ForwardViews()); n != 0 {
			t.Fatalf("forward views = %d after a push", n)
		}
	})
	if (*events)[len(*events)-1] != NavStackPushed {
		t.Fatalf("last event = %v", (*events)[len(*events)-1])
	}
	if !slices.Contains(*events, NavStackForwarded) {
		t.Fatal("no Forwarded event")
	}
}

// U:nav_stack.rs:742-769
func TestNavStackAnImmediateChangeRecordsItsMotionAndSupersedesTheRunningOne(t *testing.T) {
	ta, stack, events := navStack(t)
	root, second, third := navPage(ta), navPage(ta), navPage(ta)
	updateNav(ta, stack, func(s *NavStackState, cx *gpui.Context[NavStackState]) struct{} {
		s.Push(root, NavMotionAnimated, cx)
		s.Push(second, NavMotionAnimated, cx)
		s.Push(third, NavMotionImmediate, cx)
		return struct{}{}
	})
	readNav(ta, stack, func(s *NavStackState) {
		if cur, _ := s.Current(); !sameView(cur, third) {
			t.Fatal("current != third")
		}
		if s.transit == nil {
			t.Fatal("the change is recorded")
		}
		if s.transit.motion != NavMotionImmediate {
			t.Fatal("the motion is not Immediate")
		}
		if !sameView(s.transit.outgoing, second) {
			t.Fatal("the running push was superseded")
		}
	})
	if popped := popNav(ta, stack, NavMotionImmediate); !sameView(popped, third) {
		t.Fatal("pop did not return the third page")
	}
	readNav(ta, stack, func(s *NavStackState) {
		if s.transit == nil || s.transit.motion != NavMotionImmediate || !sameView(s.transit.outgoing, third) {
			t.Fatal("the immediate pop was not recorded")
		}
	})
	if len(*events) != 4 {
		t.Fatalf("events = %v, want 4", *events)
	}
}

// navHost is U:nav_stack.rs:771-781 Host.
type navHost struct {
	stack gpui.Entity[NavStackState]
}

func (h *navHost) Render(*gpui.Window, *gpui.Context[navHost]) gpui.AnyElement {
	return gpui.OwnRecipe(NewNavStack(h.stack).SizeFull().Transition(NewTransition(200 * time.Millisecond)))
}

// U:nav_stack.rs:783-813
func TestNavStackTheOutgoingViewIsDroppedOnceItsExitHasRun(t *testing.T) {
	ta, stack, _ := navStack(t)
	root, second := navPage(ta), navPage(ta)
	_, cx := ta.AddWindowView(func(h *navHost, _ *gpui.Window, _ *gpui.Context[navHost]) { h.stack = stack })
	updateNav(ta, stack, func(s *NavStackState, cx *gpui.Context[NavStackState]) struct{} {
		s.Push(root, NavMotionImmediate, cx)
		s.Push(second, NavMotionAnimated, cx)
		return struct{}{}
	})
	draw := func() {
		t.Helper()
		if _, err := gpui.DrawWindowFrame(cx.Window()); err != nil {
			t.Fatal(err)
		}
	}
	inTransit := func() bool {
		var some bool
		readNav(ta, stack, func(s *NavStackState) { some = s.transit != nil })
		return some
	}

	draw()
	if !inTransit() {
		t.Fatal("the transit ended on its first frame")
	}
	ta.AdvanceClock(100)
	draw()
	if !inTransit() {
		t.Fatal("the transit ended halfway")
	}
	ta.AdvanceClock(150)
	draw()
	if inTransit() {
		t.Fatal("the transit outlived its exit")
	}

	// An immediate change is gone after the frame that draws it.
	popNav(ta, stack, NavMotionImmediate)
	draw()
	if inTransit() {
		t.Fatal("an immediate change outlived its frame")
	}
}

// U:nav_stack.rs:815-832
func TestNavStackANewOperationReplacesTheRunningTransition(t *testing.T) {
	ta, stack, _ := navStack(t)
	pages := navPages(ta, 3)
	for _, view := range pages {
		pushNav(ta, stack, view, NavMotionAnimated)
	}
	popNav(ta, stack, NavMotionAnimated)
	readNav(ta, stack, func(s *NavStackState) {
		if s.transit == nil || s.transit.operation != NavOperationPop || !sameView(s.transit.outgoing, pages[2]) {
			t.Fatal("the pop did not replace the running push")
		}
	})
}
