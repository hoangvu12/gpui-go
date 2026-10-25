package base

// The cached-view consumer tests of U:input/base/state.rs, ported now that
// the core caches views (gpui-perf ticket 04: gpui.CachedView is the
// pinned AnyView::cached / Entity::cached): an input rendered inside a
// cached panel must reach its layout consumers, and must not bust the
// cached parent while idle.

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// cursorLayout is the upstream Option<(Bounds<Pixels>, Pixels)>.
type cursorLayout struct {
	bounds gpui.Bounds
	height float32
	ok     bool
}

func cursorLayoutOf[M InputModeKind](s *InputBaseState[M]) cursorLayout {
	bounds, height, ok := s.CursorLayout()
	return cursorLayout{bounds: bounds, height: height, ok: ok}
}

// cursorConsumerPanel is the upstream test's Panel: it reads the input's
// cursor layout while rendering and renders the input.
type cursorConsumerPanel struct {
	input    gpui.Entity[EditorState]
	observed *cursorLayout
}

func (p *cursorConsumerPanel) Render(_ *gpui.Window, cx *gpui.Context[cursorConsumerPanel]) gpui.AnyElement {
	*p.observed = p.input.Read(cx.App(), func(s *EditorState, _ *gpui.App) cursorLayout { return cursorLayoutOf(s) })
	return gpui.Div().Flex().SizeFull().Child(gpui.ViewOf[EditorState](p.input)).IntoElement()
}

// cursorConsumerRoot is the upstream Root: the panel, cached.
type cursorConsumerRoot struct {
	panel gpui.Entity[cursorConsumerPanel]
}

func (r *cursorConsumerRoot) Render(*gpui.Window, *gpui.Context[cursorConsumerRoot]) gpui.AnyElement {
	return gpui.Div().Flex().SizeFull().Child(gpui.CachedView(gpui.ViewOf[cursorConsumerPanel](r.panel), gpui.Styles().SizeFull())).IntoElement()
}

// U:input/base/state.rs:4957-5019
func TestCursorLayoutConsumerUpdatesAfterSelection(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	ta.Update(func(app *gpui.App) {
		theme := DefaultTheme()
		app.SetGlobal(&theme)
		inputInit(app)
	})
	observed := new(cursorLayout)
	var input gpui.Entity[EditorState]
	opts := gpui.WindowOptions{Bounds: gpui.Bounds{Size: gpui.Size{Width: 400, Height: 100}}}
	tw := ta.OpenWindowWithOptions[cursorConsumerRoot](opts, func(r *cursorConsumerRoot, w *gpui.Window, cx *gpui.Context[cursorConsumerRoot]) {
		input = gpui.NewEntity(cx, w.Scope(), func(s *EditorState, scx *gpui.Context[EditorState]) {
			*s = NewEditorState(w, scx)
			s.SetValue("abcdefgh", w, scx)
		})
		r.panel = gpui.NewEntity(cx, w.Scope(), func(p *cursorConsumerPanel, _ *gpui.Context[cursorConsumerPanel]) {
			p.input, p.observed = input, observed
		})
	})
	v := &inputView[EditorMode]{input: input, cx: gpui.NewVisualTestContext(tw.Window(), ta)}
	ta.RunUntilParked()
	var before cursorLayout
	v.read(func(s *EditorState) { before = cursorLayoutOf(s) })

	// The caret geometry only exists once the input has painted, and a
	// notify sent from inside a draw marks the view dirty without asking
	// for another frame, so the consumer reads the geometry on the next
	// invalidation rather than during the paint that produced it.
	v.update(func(_ *EditorState, _ *gpui.Window, cx *gpui.Context[EditorState]) { cx.Notify() })
	ta.RunUntilParked()
	if *observed != before {
		t.Fatalf("observed %+v, want %+v", *observed, before)
	}
	v.update(func(s *EditorState, _ *gpui.Window, cx *gpui.Context[EditorState]) {
		s.selectToWithAffinity(3, false, cx)
	})
	ta.RunUntilParked()
	tw.Update(func(_ *cursorConsumerRoot, _ *gpui.Window, cx *gpui.Context[cursorConsumerRoot]) { cx.Notify() })
	var after cursorLayout
	v.read(func(s *EditorState) { after = cursorLayoutOf(s) })
	if after == before {
		t.Fatalf("the caret must actually move (still %+v)", after)
	}
	if *observed != after {
		t.Fatalf("render consumers must receive the newly painted caret geometry: observed %+v, want %+v", *observed, after)
	}
}

// idleInputPanel is the upstream test's Panel: it counts its renders.
type idleInputPanel struct {
	input   gpui.Entity[InputState]
	renders *int
}

func (p *idleInputPanel) Render(*gpui.Window, *gpui.Context[idleInputPanel]) gpui.AnyElement {
	*p.renders++
	return gpui.Div().Flex().SizeFull().Child(gpui.ViewOf[InputState](p.input)).IntoElement()
}

// idleInputRoot is the upstream Root: the panel, cached.
type idleInputRoot struct {
	panel gpui.Entity[idleInputPanel]
}

func (r *idleInputRoot) Render(*gpui.Window, *gpui.Context[idleInputRoot]) gpui.AnyElement {
	return gpui.Div().Flex().SizeFull().Child(gpui.CachedView(gpui.ViewOf[idleInputPanel](r.panel), gpui.Styles().SizeFull())).IntoElement()
}

// U:input/base/state.rs:5123-5192
func TestInputDoesNotInvalidateCachedParentDuringPaint(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	ta.Update(func(app *gpui.App) {
		theme := DefaultTheme()
		app.SetGlobal(&theme)
		inputInit(app)
	})
	renders := new(int)
	var input gpui.Entity[InputState]
	opts := gpui.WindowOptions{Bounds: gpui.Bounds{Size: gpui.Size{Width: 400, Height: 100}}}
	tw := ta.OpenWindowWithOptions[idleInputRoot](opts, func(r *idleInputRoot, w *gpui.Window, cx *gpui.Context[idleInputRoot]) {
		input = gpui.NewEntity(cx, w.Scope(), func(s *InputState, scx *gpui.Context[InputState]) { *s = NewInputState(w, scx) })
		r.panel = gpui.NewEntity(cx, w.Scope(), func(p *idleInputPanel, _ *gpui.Context[idleInputPanel]) {
			p.input, p.renders = input, renders
		})
	})
	v := &inputView[InputMode]{input: input, cx: gpui.NewVisualTestContext(tw.Window(), ta)}
	ta.RunUntilParked()
	notifyRoot := func() {
		tw.Update(func(_ *idleInputRoot, _ *gpui.Window, cx *gpui.Context[idleInputRoot]) { cx.Notify() })
	}

	before := *renders
	if before == 0 {
		t.Fatal("the panel never rendered")
	}
	for range 60 {
		notifyRoot()
	}
	if *renders != before {
		t.Fatalf("an idle input must not invalidate its cached parent: %d -> %d renders", before, *renders)
	}

	v.update(func(s *InputState, w *gpui.Window, cx *gpui.Context[InputState]) { s.SetValue("changed", w, cx) })
	if *renders <= before {
		t.Fatal("text changes must invalidate the parent")
	}
	// The first paint reports the changed text extent to layout consumers.
	notifyRoot()
	before = *renders
	for range 60 {
		notifyRoot()
	}
	if *renders != before {
		t.Fatalf("the edited input must settle again: %d -> %d renders", before, *renders)
	}

	ta.SimulateWindowResize(tw.Window(), gpui.Size{Width: 240, Height: 100})
	ta.RunUntilParked()
	if *renders <= before {
		t.Fatal("resizing must invalidate layout")
	}
}
