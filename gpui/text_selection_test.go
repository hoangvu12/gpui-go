package gpui

import (
	"reflect"
	"testing"
)

// Ports of the U:text_selection.rs tests (2792-4391): 45 tests, 45
// ported — moved from base with the selection model (browser-core 19;
// the helpers are the base ones, in-package: a plain TestApp stands in
// for Base's Init'd app, and the registering views are custom elements
// where Base used ElementExt's prepaint hook). Go adaptations shared by
// all of them: participants are owned by the test application's root
// scope; a placeholder Hitbox is Hitbox{Bounds: b} (its content mask
// falls back to the bounds, as the upstream fixtures set it); window.draw
// is drawSelectionFrame.

// newSelectionTestApp is the model tests' TestApp (base's textTestApp
// without Base's Init — the model needs no application state).
func newSelectionTestApp(t testing.TB) *TestApp {
	ta := NewTestApp()
	t.Cleanup(func() { _ = ta.App().Shutdown() })
	return ta
}

// selectionView renders its build closure (base's testView).
type selectionView struct {
	build func(w *Window, cx *Context[selectionView]) AnyElement
}

func (v *selectionView) Render(w *Window, cx *Context[selectionView]) AnyElement {
	return v.build(w, cx)
}

// drawSelectionFrame parks and draws one frame (base's drawText).
func drawSelectionFrame(t *testing.T, cx *VisualTestContext) {
	t.Helper()
	cx.RunUntilParked()
	if _, err := DrawWindowFrame(cx.Window()); err != nil {
		t.Fatal(err)
	}
	cx.RunUntilParked()
}

// fakeParticipant is FakeParticipant (U:text_selection.rs:2806-2808,
// 2953-2985).
type fakeParticipant struct {
	selection TextSelectionHandle
}

func newFakeParticipant(text string, app *App) fakeParticipant {
	return fakeParticipant{selection: NewTextSelectionHandle(text, app, app.RootScope())}
}

func fakeHitbox(bounds Bounds) Hitbox { return Hitbox{Bounds: bounds} }

func (p fakeParticipant) register(s *windowSelectionState, y float32, scope TextSelectionScopeID, order uint64, app *App) {
	bounds := Bounds{Origin: Point{Y: y}, Size: Size{Width: 100, Height: 10}}
	registration := NewTextSelectionRegistration(fakeHitbox(bounds), bounds).
		WithScope(scope).
		WithDocumentOrder(order).
		WithTextBounds([]Bounds{bounds})
	s.registerParticipant(p.selection, &registration, app)
}

func selPt(x, y float32) Point { return Point{X: x, Y: y} }

// laidOutRun is a text with the layout a window painted it with.
type laidOutRun struct {
	text   string
	layout *TextLayout
}

// laidOutRuns is laid_out_runs (U:text_selection.rs:2987-3005) over
// PlainRunLayoutView (2936-2951): each text absolutely placed 40px below
// the previous one.
func laidOutRuns(t *testing.T, ta *TestApp, texts ...string) []laidOutRun {
	t.Helper()
	var layouts []*TextLayout
	_, cx := ta.AddWindowView(func(v *selectionView, _ *Window, _ *Context[selectionView]) {
		v.build = func(*Window, *Context[selectionView]) AnyElement {
			layouts = layouts[:0]
			root := Div().Flex().SizeFull()
			for i, text := range texts {
				styled := NewStyledText(text)
				layouts = append(layouts, styled.Layout())
				root = root.Child(Div().Flex().Absolute().Top(Px(float32(i) * 40)).Child(styled))
			}
			return root.IntoElement()
		}
	})
	drawSelectionFrame(t, cx)
	out := make([]laidOutRun, len(texts))
	for i := range texts {
		out[i] = laidOutRun{texts[i], layouts[i]}
	}
	return out
}

func plainSnapshot(anchor, cursor Point) TextSelectionSnapshot {
	none := Optional[EntityID]{}
	return newTextSelectionSnapshot(newTextSelectionEndpoint(none, anchor), newTextSelectionEndpoint(none, cursor)).
		withWindowPoints(Some(TextSelectionWindowPoints{anchor: anchor, cursor: cursor}))
}

func textRun(order uint64, run laidOutRun) TextSelectionRun {
	return NewTextSelectionRun(run.text, run.layout, run.layout.Bounds()).WithDocumentOrder(order)
}

func positionFor(t *testing.T, layout *TextLayout, index int) Point {
	t.Helper()
	p, ok := layout.PositionForIndex(index)
	if !ok {
		t.Fatalf("no position for index %d", index)
	}
	return p
}

func someRange(start, end int) Optional[TextRange] {
	return Some(TextRange{Start: start, End: end})
}

func expectRanges(t *testing.T, got TextSelectionProjection, want ...Optional[TextRange]) {
	t.Helper()
	if !reflect.DeepEqual(got.Ranges(), want) {
		t.Fatalf("ranges %v, want %v", got.Ranges(), want)
	}
}

func newWindowSelectionStateEntity(app *App) Entity[windowSelectionState] {
	return NewEntity(app, app.RootScope(), func(*windowSelectionState, *Context[windowSelectionState]) {})
}

// openSelectionView opens a test window rendering build and draws it.
func openSelectionView(t *testing.T, ta *TestApp, build func(w *Window, cx *Context[selectionView]) AnyElement) (Entity[selectionView], *VisualTestContext) {
	t.Helper()
	view, cx := ta.AddWindowView(func(v *selectionView, _ *Window, _ *Context[selectionView]) { v.build = build })
	return view, cx
}

// selectionElementOnly is SelectionElementOnlyView (U:text_selection.rs:
// 2843-2856).
func selectionElementOnly(*Window, *Context[selectionView]) AnyElement {
	return Div().Flex().SizeFull().Child(TextSelectionLayer{}).Child(
		Div().Flex().SizeFull().OnMouseDown(MouseButtonLeft, func(_ *MouseDownEvent, _ *Window, app *App) {
			SuppressTextSelection(app)
		}),
	).IntoElement()
}

// registeringSelectionElement is a full-size custom element registering
// selection on prepaint with its bounds as text bounds (base's
// registeringDiv, the on_prepaint registrations of U:text_selection.rs:
// 2858-2934 — a custom element in the core, where Base used ElementExt's
// prepaint hook).
type registeringSelectionElement struct {
	selection TextSelectionHandle
}

// ID implements Element.
func (e *registeringSelectionElement) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element.
func (e *registeringSelectionElement) SourceLocation() *SourceLocation { return nil }

// RequestLayout requests a full-size node.
func (e *registeringSelectionElement) RequestLayout(_ *GlobalElementID, _ *InspectorElementID, w *Window, _ *App) (LayoutID, struct{}) {
	style := DefaultStyle()
	style.Size = LengthSize{Width: DefiniteLengthOf(Fraction(1)), Height: DefiniteLengthOf(Fraction(1))}
	id, err := RequestElementLayout(w, style)
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}

// Prepaint registers this frame's geometry.
func (e *registeringSelectionElement) Prepaint(_ *GlobalElementID, _ *InspectorElementID, bounds Bounds, _ *struct{}, w *Window, app *App) struct{} {
	hitbox := w.InsertHitbox(bounds, HitboxNormal)
	e.selection.Register(NewTextSelectionRegistration(hitbox, bounds).WithTextBounds([]Bounds{bounds}), w, app)
	return struct{}{}
}

// Paint implements Element.
func (e *registeringSelectionElement) Paint(_ *GlobalElementID, _ *InspectorElementID, _ Bounds, _ *struct{}, _ *struct{}, _ *Window, _ *App) {
}

// registeringDiv is base's registeringDiv: the full-size registering
// element as an AnyElement.
func registeringDiv(selection TextSelectionHandle) AnyElement {
	return CustomElement[struct{}, struct{}](&registeringSelectionElement{selection: selection})
}

// doubleSelectionElement is DoubleSelectionElementView
// (U:text_selection.rs:2876-2893): two TextSelectionLayers over one
// registration.
func doubleSelectionElement(selection TextSelectionHandle) func(*Window, *Context[selectionView]) AnyElement {
	return func(*Window, *Context[selectionView]) AnyElement {
		root := Div().Flex().SizeFull().Child(TextSelectionLayer{}).Child(TextSelectionLayer{})
		root = root.Child(registeringDiv(selection))
		return root.IntoElement()
	}
}

// windowOwnedSelection is WindowOwnedSelectionView
// (U:text_selection.rs:2895-2911).
func windowOwnedSelection(selection TextSelectionHandle) func(*Window, *Context[selectionView]) AnyElement {
	return func(*Window, *Context[selectionView]) AnyElement {
		return Div().Flex().SizeFull().Child(TextSelectionLayer{}).Child(registeringDiv(selection)).IntoElement()
	}
}

func newHandle(ta *TestApp, text string) TextSelectionHandle {
	var h TextSelectionHandle
	ta.Update(func(app *App) { h = NewTextSelectionHandle(text, app, app.RootScope()) })
	return h
}

func emptyView(*Window, *Context[selectionView]) AnyElement {
	return Div().Flex().IntoElement()
}

// U:text_selection.rs:3015-3031
func TestScopeStackIsCleanedAfterPanickingSubtree(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, cx := openSelectionView(t, ta, selectionElementOnly)
	windowID := cx.Window().ID()
	scope := TextSelectionScopeID(41)
	ta.Update(func(app *App) {
		panicked := func() (p bool) {
			defer func() { p = recover() != nil }()
			withTextSelectionScope(windowID, scope, app, func(*App) struct{} { panic("subtree failed") })
			return false
		}()
		if !panicked {
			t.Fatal("the subtree did not panic")
		}
		if _, ok := currentTextSelectionScope(windowID, app); ok {
			t.Fatal("the scope stayed pushed")
		}
	})
}

// U:text_selection.rs:3033-3054
func TestReentrantScopeFromOneWindowDoesNotPolluteAnother(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, first := openSelectionView(t, ta, selectionElementOnly)
	_, second := openSelectionView(t, ta, selectionElementOnly)
	firstID, secondID := first.Window().ID(), second.Window().ID()
	scope := TextSelectionScopeID(42)
	ta.Update(func(app *App) {
		withTextSelectionScope(firstID, scope, app, func(app *App) struct{} {
			if _, ok := currentTextSelectionScope(secondID, app); ok {
				t.Error("the scope leaked into the other window")
			}
			if got, ok := currentTextSelectionScope(firstID, app); !ok || got != scope {
				t.Errorf("scope %v %v", got, ok)
			}
			return struct{}{}
		})
	})
}

// U:text_selection.rs:3056-3088
func TestSelectionCallbackCanReenterItsSelectionState(t *testing.T) {
	ta := newSelectionTestApp(t)
	called := false
	var state Entity[windowSelectionState]
	var participant fakeParticipant
	ta.Update(func(app *App) {
		state = newWindowSelectionStateEntity(app)
		participant = newFakeParticipant("participant", app)
		participant.selection.Subscribe(func(event *TextSelectionEvent, app *App) {
			if event.Kind == TextSelectionEventSelectionChanged && event.Snapshot.IsSome() {
				state.Update(app, func(*windowSelectionState, *Context[windowSelectionState]) { called = true })
			}
		}, app).Detach()
	})
	ta.RunUntilParked()
	ta.Update(func(app *App) {
		state.Update(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			participant.register(s, 0, 0, 0, cx.App())
			s.begin(selPt(1, 1), false, cx.App())
			s.update(selPt(20, 1), cx.App())
		})
	})
	ta.RunUntilParked()
	if !called {
		t.Fatal("the callback did not run")
	}
}

// U:text_selection.rs:3090-3120
func TestSelectionEventsPreserveSnapshotThenClearOrder(t *testing.T) {
	ta := newSelectionTestApp(t)
	var observed []bool
	selection := newHandle(ta, "selection")
	ta.Update(func(app *App) {
		selection.Subscribe(func(event *TextSelectionEvent, _ *App) {
			if event.Kind == TextSelectionEventSelectionChanged {
				observed = append(observed, event.Snapshot.IsSome())
			}
		}, app).Detach()
	})
	ta.RunUntilParked()
	ta.Update(func(app *App) {
		selection.entity.Update(app, func(s *selectableTextState, cx *Context[selectableTextState]) {
			s.setSnapshot(Some(plainSnapshot(selPt(1, 1), selPt(8, 1))), cx)
			s.clearState(cx)
		})
	})
	ta.RunUntilParked()
	if !reflect.DeepEqual(observed, []bool{true, false}) {
		t.Fatalf("observed %v", observed)
	}
}

// U:text_selection.rs:3127-3189
func TestPublicSelectionDataUsesBuildersAndReaders(t *testing.T) {
	ta := newSelectionTestApp(t)
	bounds := Bounds{Origin: selPt(1, 2), Size: Size{Width: 30, Height: 10}}
	scope := TextSelectionScopeID(7)
	endpoint := newTextSelectionEndpoint(Optional[EntityID]{}, bounds.Origin).withContentKey(NewTextSelectionContentKey(11))
	points := TextSelectionWindowPoints{anchor: bounds.Origin, cursor: bounds.BottomRight()}
	snapshot := newTextSelectionSnapshot(endpoint, endpoint).
		withSelecting(true).
		withWindowPoints(Some(points)).
		withCoverage(TextSelectionCoverageFull)
	registration := NewTextSelectionRegistration(fakeHitbox(bounds), bounds).
		WithScrollOffset(selPt(3, 4)).
		WithScope(scope).
		WithDocumentOrder(9).
		WithTextBounds([]Bounds{bounds})

	if endpoint.EntityID().IsSome() || endpoint.ContentPoint() != bounds.Origin || endpoint.ContentKey() != Some(NewTextSelectionContentKey(11)) {
		t.Fatalf("endpoint %+v", endpoint)
	}
	if snapshot.Anchor() != endpoint || snapshot.Cursor() != endpoint || !snapshot.IsSelecting() ||
		snapshot.Coverage() != TextSelectionCoverageFull || snapshot.WindowPoints() != Some(points) {
		t.Fatalf("snapshot %+v", snapshot)
	}
	if registration.Bounds() != bounds || registration.ScrollOffset() != selPt(3, 4) || registration.Scope() != scope ||
		registration.DocumentOrder() != 9 || !reflect.DeepEqual(registration.TextBounds(), []Bounds{bounds}) {
		t.Fatalf("registration %+v", registration)
	}

	run := laidOutRuns(t, ta, "aé")[0]
	textRun := NewTextSelectionRun(run.text, run.layout, run.layout.Bounds()).WithDocumentOrder(3)
	if textRun.DocumentOrder() != 3 || textRun.Text() != run.text || textRun.Layout().Len() != run.layout.Len() || textRun.Bounds() != run.layout.Bounds() {
		t.Fatalf("run %+v", textRun)
	}
	projection := TextSelectionProjection{ranges: []Optional[TextRange]{someRange(1, 3)}, isActive: true}
	expectRanges(t, projection, someRange(1, 3))
	if !projection.IsActive() {
		t.Fatal("inactive")
	}
}

// U:text_selection.rs:3191-3222
func TestSelectionHandleIsThePublicAdapterSeam(t *testing.T) {
	ta := newSelectionTestApp(t)
	selected := false
	ta.Update(func(app *App) {
		selection := NewTextSelectionHandle("initial", app, app.RootScope())
		entityID := selection.EntityID()
		selection.SetFallbackCopyText("updated", app)
		selection.SetLocalSelection(true, app)
		selection.Subscribe(func(event *TextSelectionEvent, _ *App) {
			if event.Kind == TextSelectionEventSelectionChanged {
				selected = event.Snapshot.IsSome()
			}
		}, app).Detach()
		selection.FocusWith(func(*Window, *App) {}, app)
		selection.CopyWith(func(*App) string { return "copied" }, app)
		selection.ResolveContentKeyWith(func(Point, *App) (TextSelectionContentKey, bool) {
			return NewTextSelectionContentKey(3), true
		}, app)
		if selection.EntityID() != entityID || selection.Snapshot(app).IsSome() {
			t.Fatal("identity or snapshot")
		}
		projection := selection.UpdateRuns(nil, app)
		if len(projection.Ranges()) != 0 || projection.IsActive() {
			t.Fatalf("projection %+v", projection)
		}
	})
	if selected {
		t.Fatal("a selection was published")
	}
}

// U:text_selection.rs:3224-3233
func TestSelectionHandleCanSubscribeItsWindowToRefresh(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, cx := openSelectionView(t, ta, emptyView)
	cx.Update(func(w *Window, app *App) {
		selection := NewTextSelectionHandle("refresh", app, app.RootScope())
		selection.RefreshWindowOnChange(w, app).Detach()
	})
}

// U:text_selection.rs:3235-3249
func TestPlainProjectionPreservesForwardReversedAndUnicodeRanges(t *testing.T) {
	ta := newSelectionTestApp(t)
	run := laidOutRuns(t, ta, "aé🙂z")[0]
	r := textRun(0, run)
	start, end := positionFor(t, run.layout, 1), positionFor(t, run.layout, 7)
	forward := projectTextSelectionRanges(Some(plainSnapshot(start, end)), []TextSelectionRun{r})
	reversed := projectTextSelectionRanges(Some(plainSnapshot(end, start)), []TextSelectionRun{r})
	expectRanges(t, forward, someRange(1, 7))
	expectRanges(t, reversed, someRange(1, 7))
	if !forward.IsActive() || !reversed.IsActive() {
		t.Fatal("inactive")
	}
}

// U:text_selection.rs:3251-3262
func TestDoubleClickExpandsAPlainRunToTheInputWordBoundary(t *testing.T) {
	ta := newSelectionTestApp(t)
	run := laidOutRuns(t, ta, "one café, three")[0]
	r := textRun(0, run)
	anchor, cursor, _, ok := textSelectionPointsForMultiClick([]TextSelectionRun{r}, positionFor(t, run.layout, 6), 2)
	if !ok {
		t.Fatal("no word")
	}
	expectRanges(t, projectTextSelectionRanges(Some(plainSnapshot(anchor, cursor)), []TextSelectionRun{r}), someRange(4, 9))
}

// U:text_selection.rs:3264-3279
func TestMultiClickUsesTextLayoutWindowCoordinatesAtANonzeroOrigin(t *testing.T) {
	ta := newSelectionTestApp(t)
	run := laidOutRuns(t, ta, "above", "alpha beta")[1]
	if !(run.layout.Bounds().Origin.Y > 0) {
		t.Fatalf("origin %v", run.layout.Bounds().Origin)
	}
	r := textRun(0, run)
	anchor, cursor, _, ok := textSelectionPointsForMultiClick([]TextSelectionRun{r}, positionFor(t, run.layout, 7), 2)
	if !ok {
		t.Fatal("no word")
	}
	expectRanges(t, projectTextSelectionRanges(Some(plainSnapshot(anchor, cursor)), []TextSelectionRun{r}), someRange(6, 10))
}

// U:text_selection.rs:3281-3293. The line-range expectation diverged
// from the upstream port when the boundary seam moved to the core
// (browser-core 19): the core default is the browser's paragraph rule —
// the line plus its break when another line follows
// (chromeParagraphRangeAt, selection_adjuster.cc:318-334) — where the
// upstream line range excluded the break (Base keeps that rule through
// its seam install).
func TestTripleClickExpandsToTheInputLogicalLineNotTheVisualRow(t *testing.T) {
	ta := newSelectionTestApp(t)
	run := laidOutRuns(t, ta, "second line")[0]
	r := textRun(0, run)
	anchor, cursor, _, ok := textSelectionPointsForMultiClick([]TextSelectionRun{r}, positionFor(t, run.layout, 4), 4)
	if !ok {
		t.Fatal("no line")
	}
	expectRanges(t, projectTextSelectionRanges(Some(plainSnapshot(anchor, cursor)), []TextSelectionRun{r}), someRange(0, 11))
	if got := textSelectionLineRangeAt("first line\nsecond line\nthird", 15); got != (TextRange{Start: 11, End: 23}) {
		t.Fatalf("line %v", got)
	}
	if got := textSelectionLineRangeAt("first line\nsecond line", 15); got != (TextRange{Start: 11, End: 22}) {
		t.Fatalf("final line %v", got)
	}
}

// U:text_selection.rs:3295-3316
func TestPlainProjectionSpansMultipleRunsAndLeavesEmptyGuttersUnselected(t *testing.T) {
	ta := newSelectionTestApp(t)
	runs := laidOutRuns(t, ta, "first", "", "second")
	start, end := positionFor(t, runs[0].layout, 2), positionFor(t, runs[2].layout, 3)
	states := projectTextSelectionRanges(Some(plainSnapshot(start, end)), []TextSelectionRun{
		textRun(2, runs[2]), textRun(1, runs[1]), textRun(0, runs[0]),
	})
	expectRanges(t, states, someRange(0, 3), Optional[TextRange]{}, someRange(2, 5))
	if !states.IsActive() {
		t.Fatal("inactive")
	}
}

// U:text_selection.rs:3318-3369
func TestPlainProjectionCachesMultipleParticipantCopiesInDocumentOrder(t *testing.T) {
	ta := newSelectionTestApp(t)
	runs := laidOutRuns(t, ta, "one", "two")
	snapshot := plainSnapshot(positionFor(t, runs[0].layout, 1), positionFor(t, runs[1].layout, 2))
	ta.Update(func(app *App) {
		var s windowSelectionState
		first, second := newFakeParticipant("", app), newFakeParticipant("", app)
		first.register(&s, 0, 0, 1, app)
		second.register(&s, 20, 0, 0, app)

		first.selection.entity.Update(app, func(p *selectableTextState, cx *Context[selectableTextState]) {
			p.setSnapshot(Some(snapshot), cx)
		})
		projection := first.selection.UpdateRuns([]TextSelectionRun{textRun(0, runs[0])}, app)
		expectRanges(t, projection, someRange(1, 3))
		second.selection.entity.Update(app, func(p *selectableTextState, cx *Context[selectableTextState]) {
			p.setSnapshot(Some(snapshot), cx)
		})
		projection = second.selection.UpdateRuns([]TextSelectionRun{textRun(0, runs[1])}, app)
		expectRanges(t, projection, someRange(0, 2))
		if got := s.selectedText(app); got != "tw\nne" {
			t.Fatalf("selected %q", got)
		}
	})
}

// U:text_selection.rs:3371-3410
func TestPlainProjectionInvalidatesCachedCopyWhenTheSnapshotChanges(t *testing.T) {
	ta := newSelectionTestApp(t)
	run := laidOutRuns(t, ta, "first")[0]
	firstSnapshot := plainSnapshot(positionFor(t, run.layout, 1), positionFor(t, run.layout, 3))
	changed := plainSnapshot(positionFor(t, run.layout, 3), positionFor(t, run.layout, 5))
	r := textRun(0, run)
	ta.Update(func(app *App) {
		var s windowSelectionState
		participant := newFakeParticipant("", app)
		participant.register(&s, 0, 0, 0, app)
		participant.selection.entity.Update(app, func(p *selectableTextState, cx *Context[selectableTextState]) {
			p.setSnapshot(Some(firstSnapshot), cx)
			p.updateRuns([]TextSelectionRun{r})
		})
		if got := s.selectedText(app); got != "ir" {
			t.Fatalf("selected %q", got)
		}
		participant.selection.entity.Update(app, func(p *selectableTextState, cx *Context[selectableTextState]) {
			p.setSnapshot(Some(changed), cx)
		})
		if got := s.selectedText(app); got != "" {
			t.Fatalf("stale copy %q", got)
		}
		participant.selection.UpdateRuns([]TextSelectionRun{r}, app)
		if got := s.selectedText(app); got != "st" {
			t.Fatalf("selected %q", got)
		}
		s.clear(app)
		participant.selection.SetLocalSelection(true, app)
		if got := s.selectedText(app); got != "" {
			t.Fatalf("after clear %q", got)
		}
	})
}

// U:text_selection.rs:3412-3441
func TestPlainProjectionOrdersCachedRunsByFrameOrderNotInputOrder(t *testing.T) {
	ta := newSelectionTestApp(t)
	runs := laidOutRuns(t, ta, "one", "two")
	snapshot := plainSnapshot(positionFor(t, runs[0].layout, 1), positionFor(t, runs[1].layout, 2))
	ta.Update(func(app *App) {
		var s windowSelectionState
		participant := newFakeParticipant("", app)
		participant.register(&s, 0, 0, 0, app)
		participant.selection.entity.Update(app, func(p *selectableTextState, cx *Context[selectableTextState]) {
			p.setSnapshot(Some(snapshot), cx)
			p.updateRuns([]TextSelectionRun{textRun(1, runs[0]), textRun(0, runs[1])})
		})
		if got := s.selectedText(app); got != "twne" {
			t.Fatalf("selected %q", got)
		}
	})
}

// U:text_selection.rs:3443-3455
func TestPlainProjectionSafelyRejectsATextLayoutLengthMismatch(t *testing.T) {
	ta := newSelectionTestApp(t)
	run := laidOutRuns(t, ta, "short")[0]
	start, end := positionFor(t, run.layout, 0), positionFor(t, run.layout, 5)
	states := projectTextSelectionRanges(Some(plainSnapshot(start, end)), []TextSelectionRun{textRun(0, laidOutRun{"longer", run.layout})})
	expectRanges(t, states, Optional[TextRange]{})
	if !states.IsActive() {
		t.Fatal("inactive")
	}
}

// U:text_selection.rs:3457-3486
func TestBeginUpdateAndEndPublishACrossParticipantSelection(t *testing.T) {
	ta := newSelectionTestApp(t)
	ta.Update(func(app *App) {
		var s windowSelectionState
		first, second := newFakeParticipant("first", app), newFakeParticipant("second", app)
		first.register(&s, 0, 0, 0, app)
		second.register(&s, 20, 0, 1, app)
		s.begin(selPt(1, 1), false, app)
		s.update(selPt(1, 25), app)
		if !s.hasSelection(app) {
			t.Fatal("no selection")
		}
		if got := s.selectedText(app); got != "first\nsecond" {
			t.Fatalf("selected %q", got)
		}
		s.end(app)
		if s.isSelecting {
			t.Fatal("still selecting")
		}
	})
}

// U:text_selection.rs:3488-3513
func TestShiftExtensionKeepsItsOriginalAnchorWhenReversed(t *testing.T) {
	ta := newSelectionTestApp(t)
	ta.Update(func(app *App) {
		var s windowSelectionState
		participant := newFakeParticipant("participant", app)
		participant.register(&s, 0, 0, 0, app)
		s.begin(selPt(2, 2), false, app)
		s.end(app)
		s.begin(selPt(8, 2), true, app)
		s.end(app)
		firstAnchor := s.snapshot()
		snap, ok := firstAnchor.Get()
		if !ok {
			t.Fatal("no snapshot")
		}
		s.begin(selPt(0, 2), true, app)
		s.end(app)
		reversed, ok := s.snapshot().Get()
		if !ok {
			t.Fatal("no reversed snapshot")
		}
		if reversed.Anchor() != snap.Anchor() || !(reversed.Cursor().ContentPoint().X < reversed.Anchor().ContentPoint().X) {
			t.Fatalf("reversed %+v", reversed)
		}
	})
}

// U:text_selection.rs:3515-3541
func TestContentKeyResolverRunsOutsideTheWindowStateLease(t *testing.T) {
	ta := newSelectionTestApp(t)
	ta.Update(func(app *App) {
		state := newWindowSelectionStateEntity(app)
		participant := newFakeParticipant("virtual", app)
		participant.selection.ResolveContentKeyWith(func(_ Point, app *App) (TextSelectionContentKey, bool) {
			state.Read(app, func(s *windowSelectionState, _ *App) struct{} { _ = s.snapshot(); return struct{}{} })
			return NewTextSelectionContentKey(7), true
		}, app)
		state.Update(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			participant.register(s, 0, 0, 0, cx.App())
			s.begin(selPt(1, 1), false, cx.App())
			s.update(selPt(8, 1), cx.App())
		})
		textSelectionResolveContentKeys(state.Downgrade(), app)
		snap, ok := state.Read(app, func(s *windowSelectionState, _ *App) Optional[TextSelectionSnapshot] { return s.snapshot() }).Get()
		if !ok || snap.Cursor().ContentKey() != Some(NewTextSelectionContentKey(7)) {
			t.Fatalf("snapshot %+v", snap)
		}
	})
}

// U:text_selection.rs:3543-3559
func TestActiveDndDoesNotMoveATextSelectionCursor(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, cx := openSelectionView(t, ta, emptyView)
	cx.Update(func(w *Window, app *App) {
		var s windowSelectionState
		participant := newFakeParticipant("participant", app)
		participant.register(&s, 0, 0, 0, app)
		s.begin(selPt(1, 1), false, app)
		before := s.cursor.point
		s.updateInWindowWithActiveDrag(selPt(80, 1), true, w, app)
		if s.cursor.point != before {
			t.Fatalf("cursor moved to %v", s.cursor.point)
		}
	})
}

// U:text_selection.rs:3561-3593
func TestShiftExtensionFallsBackWhenTheAnchorParticipantWasSwept(t *testing.T) {
	ta := newSelectionTestApp(t)
	ta.Update(func(app *App) {
		var s windowSelectionState
		first, second := newFakeParticipant("first", app), newFakeParticipant("second", app)
		first.register(&s, 0, 0, 0, app)
		s.begin(selPt(1, 1), false, app)
		s.update(selPt(8, 1), app)
		s.end(app)
		s.finishFrame(app)
		s.finishFrame(app)
		second.register(&s, 20, 0, 1, app)
		s.begin(selPt(1, 21), true, app)
		s.update(selPt(8, 21), app)
		s.end(app)
		if got := s.selectedText(app); got != "second" {
			t.Fatalf("selected %q", got)
		}
	})
}

// U:text_selection.rs:3595-3625
func TestScopeAndSuppressionPreventUnrelatedParticipantsFromParticipating(t *testing.T) {
	ta := newSelectionTestApp(t)
	ta.Update(func(app *App) {
		var s windowSelectionState
		base, modal := newFakeParticipant("base", app), newFakeParticipant("modal", app)
		base.register(&s, 0, 0, 0, app)
		modal.register(&s, 20, 1, 1, app)
		s.setActiveScope(1, app)
		s.begin(selPt(1, 21), false, app)
		s.update(selPt(8, 21), app)
		s.end(app)
		if got := s.selectedText(app); got != "modal" {
			t.Fatalf("selected %q", got)
		}
		s.clear(app)

		SuppressTextSelection(app)
		s.begin(selPt(1, 21), false, app)
		s.update(selPt(8, 21), app)
		if s.hasSelection(app) {
			t.Fatal("a suppressed gesture selected")
		}
	})
}

// U:text_selection.rs:3627-3647
func TestDeadParticipantsArePrunedAndEmptySelectionFallsBackSafely(t *testing.T) {
	ta := newSelectionTestApp(t)
	var state Entity[windowSelectionState]
	ta.Update(func(app *App) {
		state = newWindowSelectionStateEntity(app)
		scope := app.RootScope().Child()
		participant := fakeParticipant{selection: NewTextSelectionHandle("gone", app, scope)}
		state.Update(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			participant.register(s, 0, 0, 0, cx.App())
		})
		// The participant drops at the end of the Rust closure.
		scope.Close()
	})
	ta.RunUntilParked()
	ta.Update(func(app *App) {
		state.Update(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			app := cx.App()
			s.begin(selPt(1, 1), false, app)
			s.update(selPt(8, 1), app)
			s.end(app)
			if got := s.selectedText(app); got != "" {
				t.Fatalf("selected %q", got)
			}
			if s.hasSelection(app) {
				t.Fatal("selection from a dead participant")
			}
		})
	})
}

// U:text_selection.rs:3649-3677
func TestTextSelectionNamespaceReportsCopiesEndsAndClearsSelection(t *testing.T) {
	ta := newSelectionTestApp(t)
	selection := newHandle(ta, "copied")
	_, cx := openSelectionView(t, ta, emptyView)
	cx.Update(func(w *Window, app *App) {
		state := textSelectionStateEnsure(w, app)
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			fakeParticipant{selection}.register(s, 0, 0, 0, cx.App())
			s.begin(selPt(1, 1), false, cx.App())
			s.update(selPt(8, 1), cx.App())
		})
		if !TextSelectionHasSelection(w, app) {
			t.Fatal("no selection")
		}
		if got := TextSelectionSelectedText(w, app); got != "copied" {
			t.Fatalf("selected %q", got)
		}
		TextSelectionEnd(w, app)
		if !TextSelectionHasSelection(w, app) {
			t.Fatal("end dropped the selection")
		}
		TextSelectionClear(w, app)
		if TextSelectionHasSelection(w, app) || TextSelectionSelectedText(w, app) != "" {
			t.Fatal("clear kept the selection")
		}
	})
}

// U:text_selection.rs:3679-3729
func TestTwoWindowsIsolateSelectionCopyClearAndReleaseOwnership(t *testing.T) {
	ta := newSelectionTestApp(t)
	firstSelection, secondSelection := newHandle(ta, "first"), newHandle(ta, "second")
	_, first := openSelectionView(t, ta, windowOwnedSelection(firstSelection))
	_, second := openSelectionView(t, ta, windowOwnedSelection(secondSelection))

	drawSelectionFrame(t, first)
	var firstState WeakEntity[windowSelectionState]
	first.Update(func(w *Window, app *App) {
		firstSelection.SetLocalSelection(true, app)
		if got := TextSelectionSelectedText(w, app); got != "first" {
			t.Fatalf("first %q", got)
		}
		state, ok := textSelectionStateExisting(w, app)
		if !ok {
			t.Fatal("no first state")
		}
		firstState = state
	})
	drawSelectionFrame(t, second)
	second.Update(func(w *Window, app *App) {
		secondSelection.SetLocalSelection(true, app)
		if got := TextSelectionSelectedText(w, app); got != "second" {
			t.Fatalf("second %q", got)
		}
	})
	first.Update(func(w *Window, app *App) {
		TextSelectionClear(w, app)
		if got := TextSelectionSelectedText(w, app); got != "" {
			t.Fatalf("first after clear %q", got)
		}
	})
	second.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "second" {
			t.Fatalf("second after first clear %q", got)
		}
	})

	CloseTestWindow(first.Window())
	ta.RunUntilParked()

	if firstState.IsUpgradable() {
		t.Fatal("the closed window's selection state is alive")
	}
	second.Update(func(w *Window, app *App) {
		if got := TextSelectionSelectedText(w, app); got != "second" {
			t.Fatalf("second after close %q", got)
		}
	})
	ta.Update(func(app *App) {
		if n := len(Global[*selectionStateRegistry](app).states); n != 1 {
			t.Fatalf("%d registry entries", n)
		}
	})
}

// U:text_selection.rs:3731-3763
func TestCopyCallbackCanReenterWindowAndHandleSelection(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, cx := openSelectionView(t, ta, selectionElementOnly)
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		state, ok := textSelectionStateExisting(w, app)
		if !ok {
			t.Fatal("no state")
		}
		selection := NewTextSelectionHandle("fallback", app, app.RootScope())
		selection.CopyWith(func(app *App) string {
			_ = state.TryUpdate(app, func(s *windowSelectionState, _ *Context[windowSelectionState]) {
				if !s.snapshot().IsSome() {
					t.Error("no window snapshot in the copy callback")
				}
			})
			if !selection.Snapshot(app).IsSome() {
				t.Error("no participant snapshot in the copy callback")
			}
			selection.SetFallbackCopyText("reentered", app)
			return "reentrant copy"
		}, app)
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			fakeParticipant{selection}.register(s, 0, 0, 0, cx.App())
			s.begin(selPt(1, 1), false, cx.App())
			s.update(selPt(8, 1), cx.App())
			s.end(cx.App())
		})
		if got := TextSelectionSelectedText(w, app); got != "reentrant copy" {
			t.Fatalf("selected %q", got)
		}
	})
}

// U:text_selection.rs:3765-3803
func TestCrossParticipantSelectionExcludesParticipantsOutsideItsDocumentInterval(t *testing.T) {
	ta := newSelectionTestApp(t)
	ta.Update(func(app *App) {
		var s windowSelectionState
		first, second, third := newFakeParticipant("first", app), newFakeParticipant("second", app), newFakeParticipant("third", app)
		first.register(&s, 0, 0, 0, app)
		second.register(&s, 20, 0, 1, app)
		third.register(&s, 40, 0, 2, app)
		s.begin(selPt(1, 1), false, app)
		s.update(selPt(1, 25), app)
		s.end(app)
		if got := s.selectedText(app); got != "first\nsecond" {
			t.Fatalf("selected %q", got)
		}
		if third.selection.Snapshot(app).IsSome() {
			t.Fatal("the third participant was selected")
		}
	})
}

// U:text_selection.rs:3805-3834
func TestChangingScopeClearsThePreviousScopeSelection(t *testing.T) {
	ta := newSelectionTestApp(t)
	ta.Update(func(app *App) {
		var s windowSelectionState
		base, modal := newFakeParticipant("base", app), newFakeParticipant("modal", app)
		base.register(&s, 0, 0, 0, app)
		modal.register(&s, 20, 1, 1, app)
		s.begin(selPt(1, 1), false, app)
		s.update(selPt(8, 1), app)
		s.end(app)
		s.setActiveScope(1, app)
		if s.hasSelection(app) || base.selection.Snapshot(app).IsSome() {
			t.Fatal("the previous scope kept its selection")
		}
	})
}

// U:text_selection.rs:3836-3857
func TestBlankOnlyDragNeverPublishesOrCopiesSelection(t *testing.T) {
	ta := newSelectionTestApp(t)
	ta.Update(func(app *App) {
		var s windowSelectionState
		participant := newFakeParticipant("participant", app)
		participant.register(&s, 0, 0, 0, app)
		s.begin(selPt(200, 1), false, app)
		s.update(selPt(200, 8), app)
		s.end(app)
		if s.hasSelection(app) || s.selectedText(app) != "" || participant.selection.Snapshot(app).IsSome() {
			t.Fatal("a blank drag selected")
		}
	})
}

// U:text_selection.rs:3859-3880
func TestStaleLiveParticipantsAreRemovedWhenTheNextFrameBegins(t *testing.T) {
	ta := newSelectionTestApp(t)
	ta.Update(func(app *App) {
		var s windowSelectionState
		participant := newFakeParticipant("stale", app)
		participant.register(&s, 0, 0, 0, app)
		s.begin(selPt(1, 1), false, app)
		s.update(selPt(8, 1), app)
		s.end(app)
		s.finishFrame(app)
		s.finishFrame(app)
		if s.selectedText(app) != "" || participant.selection.Snapshot(app).IsSome() {
			t.Fatal("a stale participant kept its selection")
		}
	})
}

// U:text_selection.rs:3882-3919
func TestClearStopsAnchorAutoScrollBeforeDiscardingTheAnchor(t *testing.T) {
	ta := newSelectionTestApp(t)
	var commands []Optional[float32]
	var participant fakeParticipant
	ta.Update(func(app *App) {
		participant = newFakeParticipant("scroll", app)
		participant.selection.Subscribe(func(event *TextSelectionEvent, _ *App) {
			if event.Kind == TextSelectionEventAutoScroll {
				commands = append(commands, event.Delta)
			}
		}, app).Detach()
	})
	ta.RunUntilParked()
	var s windowSelectionState
	ta.Update(func(app *App) {
		participant.register(&s, 0, 0, 0, app)
		s.begin(selPt(1, 1), false, app)
		s.update(selPt(1, 25), app)
		s.clear(app)
	})
	ta.RunUntilParked()
	some := false
	for _, c := range commands {
		some = some || c.IsSome()
	}
	if !some || len(commands) == 0 || commands[len(commands)-1].IsSome() {
		t.Fatalf("commands %v", commands)
	}
}

// U:text_selection.rs:3921-3958
func TestDragAutoScrollStopsWhenTheContentMaskCollapses(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, cx := openSelectionView(t, ta, emptyView)
	cx.Update(func(w *Window, app *App) {
		state := newWindowSelectionStateEntity(app)
		participant := newFakeParticipant("participant", app)
		state.Update(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			participant.register(s, 0, 0, 0, cx.App())
			s.begin(selPt(1, 1), false, cx.App())
		})
		// The scrollable ancestor got clipped away mid-drag, so the
		// refreshed registration carries a collapsed content mask.
		collapsed := Bounds{Size: Size{Width: 100}}
		state.Update(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			registration := NewTextSelectionRegistration(fakeHitbox(collapsed), collapsed).WithTextBounds([]Bounds{collapsed})
			s.registerParticipant(participant.selection, &registration, cx.App())
			s.updateInWindow(selPt(1, 50), w, cx)
			if s.autoScroll.IsActive() || s.autoScroll.LastDragPosition.IsSome() {
				t.Fatal("auto-scroll ran on a collapsed mask")
			}
		})
	})
}

// U:text_selection.rs:3960-3981
func TestPointerMovesAfterAClickDoNotAutoScroll(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, cx := openSelectionView(t, ta, emptyView)
	cx.Update(func(w *Window, app *App) {
		state := newWindowSelectionStateEntity(app)
		participant := newFakeParticipant("participant", app)
		state.Update(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			participant.register(s, 0, 0, 0, cx.App())
			// A click on text keeps its anchor so shift-click can extend it.
			s.begin(selPt(1, 1), false, cx.App())
			s.end(cx.App())
			if s.anchor == nil {
				t.Fatal("the click dropped its anchor")
			}
			s.updateInWindow(selPt(1, 50), w, cx)
			if s.autoScroll.IsActive() {
				t.Fatal("a finished click auto-scrolls")
			}
		})
	})
}

// U:text_selection.rs:3983-4010
func TestProxyEndpointsBreakEqualPositionTiesByDocumentOrder(t *testing.T) {
	ta := newSelectionTestApp(t)
	ta.Update(func(app *App) {
		var s windowSelectionState
		later, earlier := newFakeParticipant("later", app), newFakeParticipant("earlier", app)
		later.register(&s, 0, 0, 2, app)
		earlier.register(&s, 0, 0, 1, app)
		s.begin(selPt(1, 1), false, app)
		s.update(selPt(200, 25), app)
		snap, ok := s.snapshot().Get()
		if !ok || snap.Cursor().EntityID() != Some(earlier.selection.EntityID()) {
			t.Fatalf("cursor %+v", snap.Cursor())
		}
	})
}

// U:text_selection.rs:4012-4051
func TestEqualAreaHoveredParticipantsBreakTiesByDocumentOrder(t *testing.T) {
	ta := newSelectionTestApp(t)
	ta.Update(func(app *App) {
		for range 64 {
			var s windowSelectionState
			later, earliest, middle := newFakeParticipant("later", app), newFakeParticipant("earliest", app), newFakeParticipant("middle", app)
			later.register(&s, 0, 0, 30, app)
			earliest.register(&s, 0, 0, 10, app)
			middle.register(&s, 0, 0, 20, app)
			s.begin(selPt(1, 1), false, app)
			s.update(selPt(8, 1), app)
			snap, ok := s.snapshot().Get()
			if !ok || snap.Anchor().EntityID() != Some(earliest.selection.EntityID()) {
				t.Fatalf("anchor %+v", snap.Anchor())
			}
		}
	})
}

// U:text_selection.rs:4053-4067
func TestTextSelectionNamespaceIsASafeNoOpUntilTheElementIsRendered(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, cx := openSelectionView(t, ta, emptyView)
	cx.Update(func(w *Window, app *App) {
		if TextSelectionHasSelection(w, app) || TextSelectionSelectedText(w, app) != "" {
			t.Fatal("selection without the element")
		}
		TextSelectionClear(w, app)
		TextSelectionEnd(w, app)
		if TextSelectionHasSelection(w, app) {
			t.Fatal("selection after clear")
		}
	})
}

// U:text_selection.rs:4069-4092 over FirstFrameScopedSelectionView
// (2913-2934).
func TestUnitSelectionElementSupportsScopeAndRegistrationOnTheFirstFrame(t *testing.T) {
	ta := newSelectionTestApp(t)
	selection := newHandle(ta, "first frame")
	scope := TextSelectionScopeID(23)
	_, cx := openSelectionView(t, ta, func(w *Window, cx *Context[selectionView]) AnyElement {
		TextSelectionActivateScope(scope, w, cx.App())
		return Div().Flex().Child(TextSelectionLayer{}).Child(textSelectionScope(scope, registeringDiv(selection))).IntoElement()
	})
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		state, ok := textSelectionStateExisting(w, app)
		if !ok {
			t.Fatal("no state")
		}
		_, _ = state.TryRead(app, func(s *windowSelectionState, _ *App) struct{} {
			if s.activeScope != scope {
				t.Errorf("active scope %v", s.activeScope)
			}
			if _, ok := s.participants[selection.EntityID()]; !ok {
				t.Error("the participant did not register on the first frame")
			}
			return struct{}{}
		})
	})
}

// U:text_selection.rs:4094-4119, updated by browser-core 20: every
// window now has its selection state (the automatic install), so a bare
// Register lands in the map — but the element never renders, and the
// frame-end sweep drops the registration: without its element the
// participant enables no queries.
func TestLazyRegistrationDoesNotEnableQueriesWithoutTheElement(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, cx := openSelectionView(t, ta, emptyView)
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		selection := NewTextSelectionHandle("registered", app, app.RootScope())
		selection.SetLocalSelection(true, app)
		bounds := Bounds{Size: Size{Width: 100, Height: 20}}
		selection.Register(NewTextSelectionRegistration(fakeHitbox(bounds), bounds).WithTextBounds([]Bounds{bounds}), w, app)
	})
	// The participant's element never renders: the completing frames'
	// sweep drops it (the frame it registered in keeps it — the sweep
	// compares against the generation that frame registered under — and
	// the NEXT frame's sweep drops it).
	drawSelectionFrame(t, cx)
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if TextSelectionSelectedText(w, app) != "" || TextSelectionHasSelection(w, app) {
			t.Fatal("registration enabled queries")
		}
		TextSelectionClear(w, app)
		if TextSelectionSelectedText(w, app) != "" {
			t.Fatal("selection after clear")
		}
	})
}

// U:text_selection.rs:4121-4168 over ToggleSelectionElementView
// (2858-2874).
func TestRetainedSelectionStateReleasesAndDoesNotResurrectSelection(t *testing.T) {
	ta := newSelectionTestApp(t)
	selection := newHandle(ta, "local")
	enabled := true
	view, cx := openSelectionView(t, ta, func(*Window, *Context[selectionView]) AnyElement {
		div := Div().Flex()
		if enabled {
			div = div.Child(TextSelectionLayer{}).Child(registeringDiv(selection))
		}
		return div.IntoElement()
	})
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		selection.SetLocalSelection(true, app)
		if !TextSelectionHasSelection(w, app) {
			t.Fatal("no local selection")
		}
		w.SimulateNextFrame(app)
		if !TextSelectionHasSelection(w, app) {
			t.Fatal("lost after a frame")
		}
	})
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if !TextSelectionHasSelection(w, app) {
			t.Fatal("lost after a redraw")
		}
	})
	enabled = false
	view.Update(ta, func(_ *selectionView, cx *Context[selectionView]) { cx.Notify() })
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) { w.SimulateNextFrame(app) })
	cx.Update(func(w *Window, app *App) { w.SimulateNextFrame(app) })
	ta.RunUntilParked()
	cx.Update(func(w *Window, app *App) {
		if TextSelectionHasSelection(w, app) || TextSelectionSelectedText(w, app) != "" {
			t.Fatal("the unmounted layer kept its selection")
		}
		if selection.HasLocalSelection(app) {
			t.Fatal("the participant kept its local selection")
		}
		TextSelectionClear(w, app)
	})
	enabled = true
	view.Update(ta, func(_ *selectionView, cx *Context[selectionView]) { cx.Notify() })
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if TextSelectionHasSelection(w, app) || TextSelectionSelectedText(w, app) != "" {
			t.Fatal("the selection resurrected")
		}
	})
}

// U:text_selection.rs:4170-4179
func TestMountedSelectionElementDoesNotKeepAnIdleFrameQueueAlive(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, cx := openSelectionView(t, ta, selectionElementOnly)
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		if n := w.SimulateNextFrame(app); n != 0 {
			t.Fatalf("%d frame callbacks", n)
		}
		if n := w.SimulateNextFrame(app); n != 0 {
			t.Fatalf("%d frame callbacks", n)
		}
		if _, ok := textSelectionStateExisting(w, app); !ok {
			t.Fatal("no live state")
		}
	})
}

// U:text_selection.rs:4181-4203
func TestSelectionElementInitializesSuppressionAndRespectsBubbleSuppression(t *testing.T) {
	ta := newSelectionTestApp(t)
	_, cx := openSelectionView(t, ta, selectionElementOnly)
	drawSelectionFrame(t, cx)
	cx.SimulateMouseDown(selPt(1, 1), MouseButtonLeft, Modifiers{})
	cx.SimulateMouseUp(selPt(1, 1), MouseButtonLeft, Modifiers{})
	cx.Update(func(w *Window, app *App) {
		if !IsTextSelectionSuppressed(app) {
			t.Fatal("not suppressed")
		}
		if TextSelectionHasSelection(w, app) {
			t.Fatal("a suppressed press selected")
		}
	})
}

// U:text_selection.rs:4205-4228
func TestFrameSweepKeepsAParticipantRegisteredBeforeTheSelectionElementPaints(t *testing.T) {
	ta := newSelectionTestApp(t)
	ta.Update(func(app *App) {
		var s windowSelectionState
		participant := newFakeParticipant("painted first", app)
		participant.register(&s, 0, 0, 0, app)
		s.begin(selPt(1, 1), false, app)
		s.update(selPt(8, 1), app)
		s.end(app)
		s.finishFrame(app)
		if got := s.selectedText(app); got != "painted first" {
			t.Fatalf("selected %q", got)
		}
		if !participant.selection.Snapshot(app).IsSome() {
			t.Fatal("no snapshot")
		}
	})
}

// U:text_selection.rs:4230-4257
func TestTwoSelectionElementsScheduleOnlyOnePostFrameSweep(t *testing.T) {
	ta := newSelectionTestApp(t)
	selection := newHandle(ta, "once")
	_, cx := openSelectionView(t, ta, doubleSelectionElement(selection))
	cx.Update(func(w *Window, app *App) {
		state := textSelectionStateEnsure(w, app)
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *Context[windowSelectionState]) {
			fakeParticipant{selection}.register(s, 0, 0, 0, cx.App())
			s.begin(selPt(1, 1), false, cx.App())
			s.update(selPt(8, 1), cx.App())
			s.end(cx.App())
		})
	})
	drawSelectionFrame(t, cx)
	cx.Update(func(w *Window, app *App) {
		w.SimulateNextFrame(app)
		state, _ := textSelectionStateExisting(w, app)
		items, _ := state.TryRead(app, func(s *windowSelectionState, app *App) []textSelectionCopyItem { return s.copyItems(app) })
		if got := resolveTextSelectionCopyItems(items, app); got != "once" {
			t.Fatalf("selected %q", got)
		}
	})
}

// U:text_selection.rs:4259-4328
func TestDuplicateSelectionElementsGateRealPointerGesturesAndReentrantClear(t *testing.T) {
	ta := newSelectionTestApp(t)
	selection := newHandle(ta, "once")
	_, cx := openSelectionView(t, ta, doubleSelectionElement(selection))
	clearCount := 0
	cx.Update(func(w *Window, app *App) {
		state := textSelectionStateEnsure(w, app)
		selection.Subscribe(func(event *TextSelectionEvent, app *App) {
			if event.Kind == TextSelectionEventCleared {
				clearCount++
				_, _ = state.TryRead(app, func(s *windowSelectionState, _ *App) struct{} { _ = s.snapshot(); return struct{}{} })
			}
		}, app).Detach()
	})
	drawSelectionFrame(t, cx)

	left := MouseButtonLeft
	cx.SimulateMouseDown(selPt(10, 10), left, Modifiers{})
	cx.SimulateMouseUp(selPt(10, 10), left, Modifiers{})
	cx.SimulateMouseDown(selPt(70, 10), left, Modifiers{Shift: true})
	cx.SimulateMouseUp(selPt(70, 10), left, Modifiers{})
	cx.Update(func(w *Window, app *App) {
		if !TextSelectionHasSelection(w, app) {
			t.Fatal("no shift-click selection")
		}
	})

	cx.SimulateMouseDown(selPt(15, 10), left, Modifiers{})
	cx.SimulateMouseMove(selPt(85, 10), &left, Modifiers{})
	cx.SimulateMouseUp(selPt(85, 10), left, Modifiers{})
	cx.Update(func(w *Window, app *App) {
		if !TextSelectionHasSelection(w, app) {
			t.Fatal("no drag selection")
		}
	})
	if clearCount != 3 {
		t.Fatalf("%d clears", clearCount)
	}
}

// U:text_selection.rs:4330-4390
func TestSelectionLayerHandlesRealDoubleAndTripleClickEvents(t *testing.T) {
	ta := newSelectionTestApp(t)
	run := laidOutRuns(t, ta, "alpha beta")[0]
	selection := newHandle(ta, "")
	_, cx := openSelectionView(t, ta, doubleSelectionElement(selection))
	drawSelectionFrame(t, cx)
	ta.Update(func(app *App) {
		selection.ResolveContentKeyWith(func(Point, *App) (TextSelectionContentKey, bool) {
			return NewTextSelectionContentKey(17), true
		}, app)
		selection.UpdateRuns([]TextSelectionRun{textRun(0, run)}, app)
	})

	position := positionFor(t, run.layout, 7)
	click := func(count int) {
		cx.SimulateEvent(&MouseDownEvent{Position: position, Button: MouseButtonLeft, ClickCount: count})
		cx.SimulateEvent(&MouseUpEvent{Position: position, Button: MouseButtonLeft, ClickCount: count})
	}
	click(2)
	cx.Update(func(w *Window, app *App) {
		selection.UpdateRuns([]TextSelectionRun{textRun(0, run)}, app)
		if got := TextSelectionSelectedText(w, app); got != "beta" {
			t.Fatalf("double click selected %q", got)
		}
		snap, ok := selection.Snapshot(app).Get()
		key := Some(NewTextSelectionContentKey(17))
		if !ok || snap.Anchor().ContentKey() != key || snap.Cursor().ContentKey() != key {
			t.Fatalf("snapshot %+v", snap)
		}
	})
	click(3)
	cx.Update(func(w *Window, app *App) {
		selection.UpdateRuns([]TextSelectionRun{textRun(0, run)}, app)
		if got := TextSelectionSelectedText(w, app); got != "alpha beta" {
			t.Fatalf("triple click selected %q", got)
		}
	})
}
