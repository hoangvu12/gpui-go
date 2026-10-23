package base

import (
	"reflect"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text_selection.rs tests (2792-4391): 45 tests, 45 ported.
// Go adaptations shared by all of them: participants are owned by the test
// application's root scope; a placeholder Hitbox is gpui.Hitbox{Bounds: b}
// (its content mask falls back to the bounds, as the upstream fixtures set
// it); window.draw is drawText.

// fakeParticipant is FakeParticipant (U:text_selection.rs:2806-2808,
// 2953-2985).
type fakeParticipant struct {
	selection TextSelectionHandle
}

func newFakeParticipant(text string, app *gpui.App) fakeParticipant {
	return fakeParticipant{selection: NewTextSelectionHandle(text, app, app.RootScope())}
}

func fakeHitbox(bounds gpui.Bounds) gpui.Hitbox { return gpui.Hitbox{Bounds: bounds} }

func (p fakeParticipant) register(s *windowSelectionState, y float32, scope TextSelectionScopeID, order uint64, app *gpui.App) {
	bounds := gpui.Bounds{Origin: gpui.Point{Y: y}, Size: gpui.Size{Width: 100, Height: 10}}
	registration := NewTextSelectionRegistration(fakeHitbox(bounds), bounds).
		WithScope(scope).
		WithDocumentOrder(order).
		WithTextBounds([]gpui.Bounds{bounds})
	s.registerParticipant(p.selection, &registration, app)
}

func selPt(x, y float32) gpui.Point { return gpui.Point{X: x, Y: y} }

// laidOutRun is a text with the layout a window painted it with.
type laidOutRun struct {
	text   string
	layout *gpui.TextLayout
}

// laidOutRuns is laid_out_runs (U:text_selection.rs:2987-3005) over
// PlainRunLayoutView (2936-2951): each text absolutely placed 40px below
// the previous one.
func laidOutRuns(t *testing.T, ta *gpui.TestApp, texts ...string) []laidOutRun {
	t.Helper()
	var layouts []*gpui.TextLayout
	_, cx := ta.AddWindowView(func(v *testView, _ *gpui.Window, _ *gpui.Context[testView]) {
		v.build = func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
			layouts = layouts[:0]
			root := gpui.Div().SizeFull()
			for i, text := range texts {
				styled := gpui.NewStyledText(text)
				layouts = append(layouts, styled.Layout())
				root = root.Child(gpui.Div().Absolute().Top(gpui.Px(float32(i) * 40)).Child(styled))
			}
			return root.IntoElement()
		}
	})
	drawText(t, cx)
	out := make([]laidOutRun, len(texts))
	for i := range texts {
		out[i] = laidOutRun{texts[i], layouts[i]}
	}
	return out
}

func plainSnapshot(anchor, cursor gpui.Point) TextSelectionSnapshot {
	none := gpui.Optional[gpui.EntityID]{}
	return newTextSelectionSnapshot(newTextSelectionEndpoint(none, anchor), newTextSelectionEndpoint(none, cursor)).
		withWindowPoints(gpui.Some(TextSelectionWindowPoints{anchor: anchor, cursor: cursor}))
}

func textRun(order uint64, run laidOutRun) TextSelectionRun {
	return NewTextSelectionRun(run.text, run.layout, run.layout.Bounds()).WithDocumentOrder(order)
}

func positionFor(t *testing.T, layout *gpui.TextLayout, index int) gpui.Point {
	t.Helper()
	p, ok := layout.PositionForIndex(index)
	if !ok {
		t.Fatalf("no position for index %d", index)
	}
	return p
}

func someRange(start, end int) gpui.Optional[gpui.TextRange] {
	return gpui.Some(gpui.TextRange{Start: start, End: end})
}

func expectRanges(t *testing.T, got TextSelectionProjection, want ...gpui.Optional[gpui.TextRange]) {
	t.Helper()
	if !reflect.DeepEqual(got.Ranges(), want) {
		t.Fatalf("ranges %v, want %v", got.Ranges(), want)
	}
}

func newWindowSelectionStateEntity(app *gpui.App) gpui.Entity[windowSelectionState] {
	return gpui.NewEntity(app, app.RootScope(), func(*windowSelectionState, *gpui.Context[windowSelectionState]) {})
}

// openSelectionView opens a test window rendering build and draws it.
func openSelectionView(t *testing.T, ta *gpui.TestApp, build func(w *gpui.Window, cx *gpui.Context[testView]) gpui.AnyElement) (gpui.Entity[testView], *gpui.VisualTestContext) {
	t.Helper()
	view, cx := ta.AddWindowView(func(v *testView, _ *gpui.Window, _ *gpui.Context[testView]) { v.build = build })
	return view, cx
}

// selectionElementOnly is SelectionElementOnlyView (U:text_selection.rs:
// 2843-2856).
func selectionElementOnly(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
	return gpui.Div().SizeFull().Child(TextSelectionLayer{}).Child(
		gpui.Div().SizeFull().OnMouseDown(gpui.MouseButtonLeft, func(_ *gpui.MouseDownEvent, _ *gpui.Window, app *gpui.App) {
			GlobalStateSuppressTextSelection(app)
		}),
	).IntoElement()
}

// registeringDiv is a full-size div registering selection on prepaint with
// its bounds as text bounds (the on_prepaint registrations of
// U:text_selection.rs:2858-2934).
func registeringDiv(selection TextSelectionHandle) *gpui.DivElement {
	return ElementExtOf(gpui.Div().SizeFull()).OnPrepaint(func(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
		hitbox := w.InsertHitbox(bounds, gpui.HitboxNormal)
		selection.Register(NewTextSelectionRegistration(hitbox, bounds).WithTextBounds([]gpui.Bounds{bounds}), w, app)
	})
}

// doubleSelectionElement is DoubleSelectionElementView
// (U:text_selection.rs:2876-2893).
func doubleSelectionElement(selection TextSelectionHandle) func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
	return func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		div := gpui.Div().SizeFull().Child(TextSelectionLayer{}).Child(TextSelectionLayer{})
		return ElementExtOf(div).OnPrepaint(func(bounds gpui.Bounds, w *gpui.Window, app *gpui.App) {
			hitbox := w.InsertHitbox(bounds, gpui.HitboxNormal)
			selection.Register(NewTextSelectionRegistration(hitbox, bounds).WithTextBounds([]gpui.Bounds{bounds}), w, app)
		}).IntoElement()
	}
}

// windowOwnedSelection is WindowOwnedSelectionView
// (U:text_selection.rs:2895-2911).
func windowOwnedSelection(selection TextSelectionHandle) func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
	return func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().SizeFull().Child(TextSelectionLayer{}).Child(registeringDiv(selection)).IntoElement()
	}
}

func newHandle(ta *gpui.TestApp, text string) TextSelectionHandle {
	var h TextSelectionHandle
	ta.Update(func(app *gpui.App) { h = NewTextSelectionHandle(text, app, app.RootScope()) })
	return h
}

func emptyView(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
	return gpui.Div().IntoElement()
}

// U:text_selection.rs:3015-3031
func TestScopeStackIsCleanedAfterPanickingSubtree(t *testing.T) {
	ta := textTestApp(t)
	_, cx := openSelectionView(t, ta, selectionElementOnly)
	windowID := cx.Window().ID()
	scope := TextSelectionScopeID(41)
	ta.Update(func(app *gpui.App) {
		panicked := func() (p bool) {
			defer func() { p = recover() != nil }()
			withTextSelectionScope(windowID, scope, app, func(*gpui.App) struct{} { panic("subtree failed") })
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
	ta := textTestApp(t)
	_, first := openSelectionView(t, ta, selectionElementOnly)
	_, second := openSelectionView(t, ta, selectionElementOnly)
	firstID, secondID := first.Window().ID(), second.Window().ID()
	scope := TextSelectionScopeID(42)
	ta.Update(func(app *gpui.App) {
		withTextSelectionScope(firstID, scope, app, func(app *gpui.App) struct{} {
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
	ta := textTestApp(t)
	called := false
	var state gpui.Entity[windowSelectionState]
	var participant fakeParticipant
	ta.Update(func(app *gpui.App) {
		state = newWindowSelectionStateEntity(app)
		participant = newFakeParticipant("participant", app)
		participant.selection.Subscribe(func(event *TextSelectionEvent, app *gpui.App) {
			if event.Kind == TextSelectionEventSelectionChanged && event.Snapshot.IsSome() {
				state.Update(app, func(*windowSelectionState, *gpui.Context[windowSelectionState]) { called = true })
			}
		}, app).Detach()
	})
	ta.RunUntilParked()
	ta.Update(func(app *gpui.App) {
		state.Update(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
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
	ta := textTestApp(t)
	var observed []bool
	selection := newHandle(ta, "selection")
	ta.Update(func(app *gpui.App) {
		selection.Subscribe(func(event *TextSelectionEvent, _ *gpui.App) {
			if event.Kind == TextSelectionEventSelectionChanged {
				observed = append(observed, event.Snapshot.IsSome())
			}
		}, app).Detach()
	})
	ta.RunUntilParked()
	ta.Update(func(app *gpui.App) {
		selection.entity.Update(app, func(s *selectableTextState, cx *gpui.Context[selectableTextState]) {
			s.setSnapshot(gpui.Some(plainSnapshot(selPt(1, 1), selPt(8, 1))), cx)
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
	ta := textTestApp(t)
	bounds := gpui.Bounds{Origin: selPt(1, 2), Size: gpui.Size{Width: 30, Height: 10}}
	scope := TextSelectionScopeID(7)
	endpoint := newTextSelectionEndpoint(gpui.Optional[gpui.EntityID]{}, bounds.Origin).withContentKey(NewTextSelectionContentKey(11))
	points := TextSelectionWindowPoints{anchor: bounds.Origin, cursor: bounds.BottomRight()}
	snapshot := newTextSelectionSnapshot(endpoint, endpoint).
		withSelecting(true).
		withWindowPoints(gpui.Some(points)).
		withCoverage(TextSelectionCoverageFull)
	registration := NewTextSelectionRegistration(fakeHitbox(bounds), bounds).
		WithScrollOffset(selPt(3, 4)).
		WithScope(scope).
		WithDocumentOrder(9).
		WithTextBounds([]gpui.Bounds{bounds})

	if endpoint.EntityID().IsSome() || endpoint.ContentPoint() != bounds.Origin || endpoint.ContentKey() != gpui.Some(NewTextSelectionContentKey(11)) {
		t.Fatalf("endpoint %+v", endpoint)
	}
	if snapshot.Anchor() != endpoint || snapshot.Cursor() != endpoint || !snapshot.IsSelecting() ||
		snapshot.Coverage() != TextSelectionCoverageFull || snapshot.WindowPoints() != gpui.Some(points) {
		t.Fatalf("snapshot %+v", snapshot)
	}
	if registration.Bounds() != bounds || registration.ScrollOffset() != selPt(3, 4) || registration.Scope() != scope ||
		registration.DocumentOrder() != 9 || !reflect.DeepEqual(registration.TextBounds(), []gpui.Bounds{bounds}) {
		t.Fatalf("registration %+v", registration)
	}

	run := laidOutRuns(t, ta, "aé")[0]
	textRun := NewTextSelectionRun(run.text, run.layout, run.layout.Bounds()).WithDocumentOrder(3)
	if textRun.DocumentOrder() != 3 || textRun.Text() != run.text || textRun.Layout().Len() != run.layout.Len() || textRun.Bounds() != run.layout.Bounds() {
		t.Fatalf("run %+v", textRun)
	}
	projection := TextSelectionProjection{ranges: []gpui.Optional[gpui.TextRange]{someRange(1, 3)}, isActive: true}
	expectRanges(t, projection, someRange(1, 3))
	if !projection.IsActive() {
		t.Fatal("inactive")
	}
}

// U:text_selection.rs:3191-3222
func TestSelectionHandleIsThePublicAdapterSeam(t *testing.T) {
	ta := textTestApp(t)
	selected := false
	ta.Update(func(app *gpui.App) {
		selection := NewTextSelectionHandle("initial", app, app.RootScope())
		entityID := selection.EntityID()
		selection.SetFallbackCopyText("updated", app)
		selection.SetLocalSelection(true, app)
		selection.Subscribe(func(event *TextSelectionEvent, _ *gpui.App) {
			if event.Kind == TextSelectionEventSelectionChanged {
				selected = event.Snapshot.IsSome()
			}
		}, app).Detach()
		selection.FocusWith(func(*gpui.Window, *gpui.App) {}, app)
		selection.CopyWith(func(*gpui.App) string { return "copied" }, app)
		selection.ResolveContentKeyWith(func(gpui.Point, *gpui.App) (TextSelectionContentKey, bool) {
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
	ta := textTestApp(t)
	_, cx := openSelectionView(t, ta, emptyView)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		selection := NewTextSelectionHandle("refresh", app, app.RootScope())
		selection.RefreshWindowOnChange(w, app).Detach()
	})
}

// U:text_selection.rs:3235-3249
func TestPlainProjectionPreservesForwardReversedAndUnicodeRanges(t *testing.T) {
	ta := textTestApp(t)
	run := laidOutRuns(t, ta, "aé🙂z")[0]
	r := textRun(0, run)
	start, end := positionFor(t, run.layout, 1), positionFor(t, run.layout, 7)
	forward := projectTextSelectionRanges(gpui.Some(plainSnapshot(start, end)), []TextSelectionRun{r})
	reversed := projectTextSelectionRanges(gpui.Some(plainSnapshot(end, start)), []TextSelectionRun{r})
	expectRanges(t, forward, someRange(1, 7))
	expectRanges(t, reversed, someRange(1, 7))
	if !forward.IsActive() || !reversed.IsActive() {
		t.Fatal("inactive")
	}
}

// U:text_selection.rs:3251-3262
func TestDoubleClickExpandsAPlainRunToTheInputWordBoundary(t *testing.T) {
	ta := textTestApp(t)
	run := laidOutRuns(t, ta, "one café, three")[0]
	r := textRun(0, run)
	anchor, cursor, ok := textSelectionPointsForMultiClick([]TextSelectionRun{r}, positionFor(t, run.layout, 6), 2)
	if !ok {
		t.Fatal("no word")
	}
	expectRanges(t, projectTextSelectionRanges(gpui.Some(plainSnapshot(anchor, cursor)), []TextSelectionRun{r}), someRange(4, 9))
}

// U:text_selection.rs:3264-3279
func TestMultiClickUsesTextLayoutWindowCoordinatesAtANonzeroOrigin(t *testing.T) {
	ta := textTestApp(t)
	run := laidOutRuns(t, ta, "above", "alpha beta")[1]
	if !(run.layout.Bounds().Origin.Y > 0) {
		t.Fatalf("origin %v", run.layout.Bounds().Origin)
	}
	r := textRun(0, run)
	anchor, cursor, ok := textSelectionPointsForMultiClick([]TextSelectionRun{r}, positionFor(t, run.layout, 7), 2)
	if !ok {
		t.Fatal("no word")
	}
	expectRanges(t, projectTextSelectionRanges(gpui.Some(plainSnapshot(anchor, cursor)), []TextSelectionRun{r}), someRange(6, 10))
}

// U:text_selection.rs:3281-3293
func TestTripleClickExpandsToTheInputLogicalLineNotTheVisualRow(t *testing.T) {
	ta := textTestApp(t)
	run := laidOutRuns(t, ta, "second line")[0]
	r := textRun(0, run)
	anchor, cursor, ok := textSelectionPointsForMultiClick([]TextSelectionRun{r}, positionFor(t, run.layout, 4), 4)
	if !ok {
		t.Fatal("no line")
	}
	expectRanges(t, projectTextSelectionRanges(gpui.Some(plainSnapshot(anchor, cursor)), []TextSelectionRun{r}), someRange(0, 11))
	if got := textBoundaryLineRangeAt("first line\nsecond line\nthird", 15); got != (gpui.TextRange{Start: 11, End: 22}) {
		t.Fatalf("line %v", got)
	}
}

// U:text_selection.rs:3295-3316
func TestPlainProjectionSpansMultipleRunsAndLeavesEmptyGuttersUnselected(t *testing.T) {
	ta := textTestApp(t)
	runs := laidOutRuns(t, ta, "first", "", "second")
	start, end := positionFor(t, runs[0].layout, 2), positionFor(t, runs[2].layout, 3)
	states := projectTextSelectionRanges(gpui.Some(plainSnapshot(start, end)), []TextSelectionRun{
		textRun(2, runs[2]), textRun(1, runs[1]), textRun(0, runs[0]),
	})
	expectRanges(t, states, someRange(0, 3), gpui.Optional[gpui.TextRange]{}, someRange(2, 5))
	if !states.IsActive() {
		t.Fatal("inactive")
	}
}

// U:text_selection.rs:3318-3369
func TestPlainProjectionCachesMultipleParticipantCopiesInDocumentOrder(t *testing.T) {
	ta := textTestApp(t)
	runs := laidOutRuns(t, ta, "one", "two")
	snapshot := plainSnapshot(positionFor(t, runs[0].layout, 1), positionFor(t, runs[1].layout, 2))
	ta.Update(func(app *gpui.App) {
		var s windowSelectionState
		first, second := newFakeParticipant("", app), newFakeParticipant("", app)
		first.register(&s, 0, 0, 1, app)
		second.register(&s, 20, 0, 0, app)

		first.selection.entity.Update(app, func(p *selectableTextState, cx *gpui.Context[selectableTextState]) {
			p.setSnapshot(gpui.Some(snapshot), cx)
		})
		projection := first.selection.UpdateRuns([]TextSelectionRun{textRun(0, runs[0])}, app)
		expectRanges(t, projection, someRange(1, 3))
		second.selection.entity.Update(app, func(p *selectableTextState, cx *gpui.Context[selectableTextState]) {
			p.setSnapshot(gpui.Some(snapshot), cx)
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
	ta := textTestApp(t)
	run := laidOutRuns(t, ta, "first")[0]
	firstSnapshot := plainSnapshot(positionFor(t, run.layout, 1), positionFor(t, run.layout, 3))
	changed := plainSnapshot(positionFor(t, run.layout, 3), positionFor(t, run.layout, 5))
	r := textRun(0, run)
	ta.Update(func(app *gpui.App) {
		var s windowSelectionState
		participant := newFakeParticipant("", app)
		participant.register(&s, 0, 0, 0, app)
		participant.selection.entity.Update(app, func(p *selectableTextState, cx *gpui.Context[selectableTextState]) {
			p.setSnapshot(gpui.Some(firstSnapshot), cx)
			p.updateRuns([]TextSelectionRun{r})
		})
		if got := s.selectedText(app); got != "ir" {
			t.Fatalf("selected %q", got)
		}
		participant.selection.entity.Update(app, func(p *selectableTextState, cx *gpui.Context[selectableTextState]) {
			p.setSnapshot(gpui.Some(changed), cx)
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
	ta := textTestApp(t)
	runs := laidOutRuns(t, ta, "one", "two")
	snapshot := plainSnapshot(positionFor(t, runs[0].layout, 1), positionFor(t, runs[1].layout, 2))
	ta.Update(func(app *gpui.App) {
		var s windowSelectionState
		participant := newFakeParticipant("", app)
		participant.register(&s, 0, 0, 0, app)
		participant.selection.entity.Update(app, func(p *selectableTextState, cx *gpui.Context[selectableTextState]) {
			p.setSnapshot(gpui.Some(snapshot), cx)
			p.updateRuns([]TextSelectionRun{textRun(1, runs[0]), textRun(0, runs[1])})
		})
		if got := s.selectedText(app); got != "twne" {
			t.Fatalf("selected %q", got)
		}
	})
}

// U:text_selection.rs:3443-3455
func TestPlainProjectionSafelyRejectsATextLayoutLengthMismatch(t *testing.T) {
	ta := textTestApp(t)
	run := laidOutRuns(t, ta, "short")[0]
	start, end := positionFor(t, run.layout, 0), positionFor(t, run.layout, 5)
	states := projectTextSelectionRanges(gpui.Some(plainSnapshot(start, end)), []TextSelectionRun{textRun(0, laidOutRun{"longer", run.layout})})
	expectRanges(t, states, gpui.Optional[gpui.TextRange]{})
	if !states.IsActive() {
		t.Fatal("inactive")
	}
}

// U:text_selection.rs:3457-3486
func TestBeginUpdateAndEndPublishACrossParticipantSelection(t *testing.T) {
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
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
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
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
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
		state := newWindowSelectionStateEntity(app)
		participant := newFakeParticipant("virtual", app)
		participant.selection.ResolveContentKeyWith(func(_ gpui.Point, app *gpui.App) (TextSelectionContentKey, bool) {
			state.Read(app, func(s *windowSelectionState, _ *gpui.App) struct{} { _ = s.snapshot(); return struct{}{} })
			return NewTextSelectionContentKey(7), true
		}, app)
		state.Update(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
			participant.register(s, 0, 0, 0, cx.App())
			s.begin(selPt(1, 1), false, cx.App())
			s.update(selPt(8, 1), cx.App())
		})
		textSelectionResolveContentKeys(state.Downgrade(), app)
		snap, ok := state.Read(app, func(s *windowSelectionState, _ *gpui.App) gpui.Optional[TextSelectionSnapshot] { return s.snapshot() }).Get()
		if !ok || snap.Cursor().ContentKey() != gpui.Some(NewTextSelectionContentKey(7)) {
			t.Fatalf("snapshot %+v", snap)
		}
	})
}

// U:text_selection.rs:3543-3559
func TestActiveDndDoesNotMoveATextSelectionCursor(t *testing.T) {
	ta := textTestApp(t)
	_, cx := openSelectionView(t, ta, emptyView)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
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
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
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
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
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
		GlobalStateInit(app)
		GlobalStateSuppressTextSelection(app)
		s.begin(selPt(1, 21), false, app)
		s.update(selPt(8, 21), app)
		if s.hasSelection(app) {
			t.Fatal("a suppressed gesture selected")
		}
	})
}

// U:text_selection.rs:3627-3647
func TestDeadParticipantsArePrunedAndEmptySelectionFallsBackSafely(t *testing.T) {
	ta := textTestApp(t)
	var state gpui.Entity[windowSelectionState]
	ta.Update(func(app *gpui.App) {
		state = newWindowSelectionStateEntity(app)
		scope := app.RootScope().Child()
		participant := fakeParticipant{selection: NewTextSelectionHandle("gone", app, scope)}
		state.Update(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
			participant.register(s, 0, 0, 0, cx.App())
		})
		// The participant drops at the end of the Rust closure.
		scope.Close()
	})
	ta.RunUntilParked()
	ta.Update(func(app *gpui.App) {
		state.Update(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
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
	ta := textTestApp(t)
	selection := newHandle(ta, "copied")
	_, cx := openSelectionView(t, ta, emptyView)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state := textSelectionStateEnsure(w, app)
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
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
	ta := textTestApp(t)
	firstSelection, secondSelection := newHandle(ta, "first"), newHandle(ta, "second")
	_, first := openSelectionView(t, ta, windowOwnedSelection(firstSelection))
	_, second := openSelectionView(t, ta, windowOwnedSelection(secondSelection))

	drawText(t, first)
	var firstState gpui.WeakEntity[windowSelectionState]
	first.Update(func(w *gpui.Window, app *gpui.App) {
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
	drawText(t, second)
	second.Update(func(w *gpui.Window, app *gpui.App) {
		secondSelection.SetLocalSelection(true, app)
		if got := TextSelectionSelectedText(w, app); got != "second" {
			t.Fatalf("second %q", got)
		}
	})
	first.Update(func(w *gpui.Window, app *gpui.App) {
		TextSelectionClear(w, app)
		if got := TextSelectionSelectedText(w, app); got != "" {
			t.Fatalf("first after clear %q", got)
		}
	})
	second.Update(func(w *gpui.Window, app *gpui.App) {
		if got := TextSelectionSelectedText(w, app); got != "second" {
			t.Fatalf("second after first clear %q", got)
		}
	})

	gpui.CloseTestWindow(first.Window())
	ta.RunUntilParked()

	if firstState.IsUpgradable() {
		t.Fatal("the closed window's selection state is alive")
	}
	second.Update(func(w *gpui.Window, app *gpui.App) {
		if got := TextSelectionSelectedText(w, app); got != "second" {
			t.Fatalf("second after close %q", got)
		}
	})
	ta.Update(func(app *gpui.App) {
		if n := len(gpui.Global[*selectionStateRegistry](app).states); n != 1 {
			t.Fatalf("%d registry entries", n)
		}
	})
}

// U:text_selection.rs:3731-3763
func TestCopyCallbackCanReenterWindowAndHandleSelection(t *testing.T) {
	ta := textTestApp(t)
	_, cx := openSelectionView(t, ta, selectionElementOnly)
	drawText(t, cx)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state, ok := textSelectionStateExisting(w, app)
		if !ok {
			t.Fatal("no state")
		}
		selection := NewTextSelectionHandle("fallback", app, app.RootScope())
		selection.CopyWith(func(app *gpui.App) string {
			_ = state.TryUpdate(app, func(s *windowSelectionState, _ *gpui.Context[windowSelectionState]) {
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
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
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
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
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
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
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
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
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
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
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
	ta := textTestApp(t)
	var commands []gpui.Optional[float32]
	var participant fakeParticipant
	ta.Update(func(app *gpui.App) {
		participant = newFakeParticipant("scroll", app)
		participant.selection.Subscribe(func(event *TextSelectionEvent, _ *gpui.App) {
			if event.Kind == TextSelectionEventAutoScroll {
				commands = append(commands, event.Delta)
			}
		}, app).Detach()
	})
	ta.RunUntilParked()
	var s windowSelectionState
	ta.Update(func(app *gpui.App) {
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
	ta := textTestApp(t)
	_, cx := openSelectionView(t, ta, emptyView)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state := newWindowSelectionStateEntity(app)
		participant := newFakeParticipant("participant", app)
		state.Update(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
			participant.register(s, 0, 0, 0, cx.App())
			s.begin(selPt(1, 1), false, cx.App())
		})
		// The scrollable ancestor got clipped away mid-drag, so the
		// refreshed registration carries a collapsed content mask.
		collapsed := gpui.Bounds{Size: gpui.Size{Width: 100}}
		state.Update(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
			registration := NewTextSelectionRegistration(fakeHitbox(collapsed), collapsed).WithTextBounds([]gpui.Bounds{collapsed})
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
	ta := textTestApp(t)
	_, cx := openSelectionView(t, ta, emptyView)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state := newWindowSelectionStateEntity(app)
		participant := newFakeParticipant("participant", app)
		state.Update(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
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
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
		var s windowSelectionState
		later, earlier := newFakeParticipant("later", app), newFakeParticipant("earlier", app)
		later.register(&s, 0, 0, 2, app)
		earlier.register(&s, 0, 0, 1, app)
		s.begin(selPt(1, 1), false, app)
		s.update(selPt(200, 25), app)
		snap, ok := s.snapshot().Get()
		if !ok || snap.Cursor().EntityID() != gpui.Some(earlier.selection.EntityID()) {
			t.Fatalf("cursor %+v", snap.Cursor())
		}
	})
}

// U:text_selection.rs:4012-4051
func TestEqualAreaHoveredParticipantsBreakTiesByDocumentOrder(t *testing.T) {
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
		for range 64 {
			var s windowSelectionState
			later, earliest, middle := newFakeParticipant("later", app), newFakeParticipant("earliest", app), newFakeParticipant("middle", app)
			later.register(&s, 0, 0, 30, app)
			earliest.register(&s, 0, 0, 10, app)
			middle.register(&s, 0, 0, 20, app)
			s.begin(selPt(1, 1), false, app)
			s.update(selPt(8, 1), app)
			snap, ok := s.snapshot().Get()
			if !ok || snap.Anchor().EntityID() != gpui.Some(earliest.selection.EntityID()) {
				t.Fatalf("anchor %+v", snap.Anchor())
			}
		}
	})
}

// U:text_selection.rs:4053-4067
func TestTextSelectionNamespaceIsASafeNoOpUntilTheElementIsRendered(t *testing.T) {
	ta := textTestApp(t)
	_, cx := openSelectionView(t, ta, emptyView)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
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
	ta := textTestApp(t)
	selection := newHandle(ta, "first frame")
	scope := TextSelectionScopeID(23)
	_, cx := openSelectionView(t, ta, func(w *gpui.Window, cx *gpui.Context[testView]) gpui.AnyElement {
		TextSelectionActivateScope(scope, w, cx.App())
		return gpui.Div().Child(TextSelectionLayer{}).Child(textSelectionScope(scope, registeringDiv(selection).IntoElement())).IntoElement()
	})
	drawText(t, cx)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state, ok := textSelectionStateExisting(w, app)
		if !ok {
			t.Fatal("no state")
		}
		_, _ = state.TryRead(app, func(s *windowSelectionState, _ *gpui.App) struct{} {
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

// U:text_selection.rs:4094-4119
func TestLazyRegistrationDoesNotEnableQueriesWithoutTheElement(t *testing.T) {
	ta := textTestApp(t)
	_, cx := openSelectionView(t, ta, emptyView)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		selection := NewTextSelectionHandle("registered", app, app.RootScope())
		selection.SetLocalSelection(true, app)
		bounds := gpui.Bounds{Size: gpui.Size{Width: 100, Height: 20}}
		selection.Register(NewTextSelectionRegistration(fakeHitbox(bounds), bounds).WithTextBounds([]gpui.Bounds{bounds}), w, app)
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
	ta := textTestApp(t)
	selection := newHandle(ta, "local")
	enabled := true
	view, cx := openSelectionView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		div := gpui.Div()
		if enabled {
			div = div.Child(TextSelectionLayer{}).Child(registeringDiv(selection))
		}
		return div.IntoElement()
	})
	drawText(t, cx)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		selection.SetLocalSelection(true, app)
		if !TextSelectionHasSelection(w, app) {
			t.Fatal("no local selection")
		}
		w.SimulateNextFrame(app)
		if !TextSelectionHasSelection(w, app) {
			t.Fatal("lost after a frame")
		}
	})
	drawText(t, cx)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		if !TextSelectionHasSelection(w, app) {
			t.Fatal("lost after a redraw")
		}
	})
	enabled = false
	view.Update(ta, func(_ *testView, cx *gpui.Context[testView]) { cx.Notify() })
	drawText(t, cx)
	cx.Update(func(w *gpui.Window, app *gpui.App) { w.SimulateNextFrame(app) })
	cx.Update(func(w *gpui.Window, app *gpui.App) { w.SimulateNextFrame(app) })
	ta.RunUntilParked()
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		if TextSelectionHasSelection(w, app) || TextSelectionSelectedText(w, app) != "" {
			t.Fatal("the unmounted layer kept its selection")
		}
		if selection.HasLocalSelection(app) {
			t.Fatal("the participant kept its local selection")
		}
		TextSelectionClear(w, app)
	})
	enabled = true
	view.Update(ta, func(_ *testView, cx *gpui.Context[testView]) { cx.Notify() })
	drawText(t, cx)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		if TextSelectionHasSelection(w, app) || TextSelectionSelectedText(w, app) != "" {
			t.Fatal("the selection resurrected")
		}
	})
}

// U:text_selection.rs:4170-4179
func TestMountedSelectionElementDoesNotKeepAnIdleFrameQueueAlive(t *testing.T) {
	ta := textTestApp(t)
	_, cx := openSelectionView(t, ta, selectionElementOnly)
	drawText(t, cx)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
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
	ta := textTestApp(t)
	_, cx := openSelectionView(t, ta, selectionElementOnly)
	drawText(t, cx)
	cx.SimulateMouseDown(selPt(1, 1), gpui.MouseButtonLeft, gpui.Modifiers{})
	cx.SimulateMouseUp(selPt(1, 1), gpui.MouseButtonLeft, gpui.Modifiers{})
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		if !GlobalStateIsTextSelectionSuppressed(app) {
			t.Fatal("not suppressed")
		}
		if TextSelectionHasSelection(w, app) {
			t.Fatal("a suppressed press selected")
		}
	})
}

// U:text_selection.rs:4205-4228
func TestFrameSweepKeepsAParticipantRegisteredBeforeTheSelectionElementPaints(t *testing.T) {
	ta := textTestApp(t)
	ta.Update(func(app *gpui.App) {
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
	ta := textTestApp(t)
	selection := newHandle(ta, "once")
	_, cx := openSelectionView(t, ta, doubleSelectionElement(selection))
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state := textSelectionStateEnsure(w, app)
		_ = state.TryUpdate(app, func(s *windowSelectionState, cx *gpui.Context[windowSelectionState]) {
			fakeParticipant{selection}.register(s, 0, 0, 0, cx.App())
			s.begin(selPt(1, 1), false, cx.App())
			s.update(selPt(8, 1), cx.App())
			s.end(cx.App())
		})
	})
	drawText(t, cx)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		w.SimulateNextFrame(app)
		state, _ := textSelectionStateExisting(w, app)
		items, _ := state.TryRead(app, func(s *windowSelectionState, app *gpui.App) []textSelectionCopyItem { return s.copyItems(app) })
		if got := resolveTextSelectionCopyItems(items, app); got != "once" {
			t.Fatalf("selected %q", got)
		}
	})
}

// U:text_selection.rs:4259-4328
func TestDuplicateSelectionElementsGateRealPointerGesturesAndReentrantClear(t *testing.T) {
	ta := textTestApp(t)
	selection := newHandle(ta, "once")
	_, cx := openSelectionView(t, ta, doubleSelectionElement(selection))
	clearCount := 0
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state := textSelectionStateEnsure(w, app)
		selection.Subscribe(func(event *TextSelectionEvent, app *gpui.App) {
			if event.Kind == TextSelectionEventCleared {
				clearCount++
				_, _ = state.TryRead(app, func(s *windowSelectionState, _ *gpui.App) struct{} { _ = s.snapshot(); return struct{}{} })
			}
		}, app).Detach()
	})
	drawText(t, cx)

	left := gpui.MouseButtonLeft
	cx.SimulateMouseDown(selPt(10, 10), left, gpui.Modifiers{})
	cx.SimulateMouseUp(selPt(10, 10), left, gpui.Modifiers{})
	cx.SimulateMouseDown(selPt(70, 10), left, gpui.Modifiers{Shift: true})
	cx.SimulateMouseUp(selPt(70, 10), left, gpui.Modifiers{})
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		if !TextSelectionHasSelection(w, app) {
			t.Fatal("no shift-click selection")
		}
	})

	cx.SimulateMouseDown(selPt(15, 10), left, gpui.Modifiers{})
	cx.SimulateMouseMove(selPt(85, 10), &left, gpui.Modifiers{})
	cx.SimulateMouseUp(selPt(85, 10), left, gpui.Modifiers{})
	cx.Update(func(w *gpui.Window, app *gpui.App) {
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
	ta := textTestApp(t)
	run := laidOutRuns(t, ta, "alpha beta")[0]
	selection := newHandle(ta, "")
	_, cx := openSelectionView(t, ta, doubleSelectionElement(selection))
	drawText(t, cx)
	ta.Update(func(app *gpui.App) {
		selection.ResolveContentKeyWith(func(gpui.Point, *gpui.App) (TextSelectionContentKey, bool) {
			return NewTextSelectionContentKey(17), true
		}, app)
		selection.UpdateRuns([]TextSelectionRun{textRun(0, run)}, app)
	})

	position := positionFor(t, run.layout, 7)
	click := func(count int) {
		cx.SimulateEvent(&gpui.MouseDownEvent{Position: position, Button: gpui.MouseButtonLeft, ClickCount: count})
		cx.SimulateEvent(&gpui.MouseUpEvent{Position: position, Button: gpui.MouseButtonLeft, ClickCount: count})
	}
	click(2)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		selection.UpdateRuns([]TextSelectionRun{textRun(0, run)}, app)
		if got := TextSelectionSelectedText(w, app); got != "beta" {
			t.Fatalf("double click selected %q", got)
		}
		snap, ok := selection.Snapshot(app).Get()
		key := gpui.Some(NewTextSelectionContentKey(17))
		if !ok || snap.Anchor().ContentKey() != key || snap.Cursor().ContentKey() != key {
			t.Fatalf("snapshot %+v", snap)
		}
	})
	click(3)
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		selection.UpdateRuns([]TextSelectionRun{textRun(0, run)}, app)
		if got := TextSelectionSelectedText(w, app); got != "alpha beta" {
			t.Fatalf("triple click selected %q", got)
		}
	})
}
