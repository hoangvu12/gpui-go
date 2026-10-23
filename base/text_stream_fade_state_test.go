package base

import (
	"slices"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/state.rs stream_fade module tests (1292-1534): 12 of
// 12, ported.

// fadeTestDuration is long enough that a test cannot outrun the fade
// before it samples the first frame (U:text/state.rs:1300-1302).
const fadeTestDuration = 10 * time.Second

func sfFadingState(t *testing.T, ta *gpui.TestApp, markdown string) gpui.Entity[TextViewState] {
	t.Helper()
	state := newTextViewStateEntity(ta, markdown)
	updateState(ta, state, func(s *TextViewState, _ *gpui.Context[TextViewState]) {
		s.SetMotion(DefaultTextViewMotion().WithStreamFade(fadeTestDuration).WithStreamFadeEasing(EasingLinear()))
	})
	ta.RunUntilParked()
	return state
}

// sfFades is the fade ranges of key sampled right now, or nil when
// the state has nothing fading for it (U:text/state.rs:1319-1335).
func sfFades(t *testing.T, ta *gpui.TestApp, state gpui.Entity[TextViewState], key textLeafKey) []gpui.TextRange {
	t.Helper()
	return readState(ta, state, func(s *TextViewState) []gpui.TextRange {
		frame := s.streamFade.frame(time.Now(), false)
		fades, ok := frame.fades(key)
		if !ok {
			return nil
		}
		var out []gpui.TextRange
		for _, f := range fades {
			if f.fadeOut <= 0.9 {
				t.Errorf("a fade sampled right after it starts is still transparent: %+v", fades)
			}
			out = append(out, f.rng)
		}
		return out
	})
}

func sfPushStr(ta *gpui.TestApp, state gpui.Entity[TextViewState], text string) {
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.PushStr(text, cx) })
	ta.RunUntilParked()
}

func sfSetText(ta *gpui.TestApp, state gpui.Entity[TextViewState], text string) {
	updateState(ta, state, func(s *TextViewState, cx *gpui.Context[TextViewState]) { s.SetText(text, cx) })
	ta.RunUntilParked()
}

func sfWantRanges(t *testing.T, what string, got []gpui.TextRange, want ...gpui.TextRange) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

// U:text/state.rs:1337-1350
func TestPushStrFadesOnlyTheAppendedText(t *testing.T) {
	ta := textTestApp(t)
	state := sfFadingState(t, ta, "hello")
	sfPushStr(ta, state, " world")
	sfWantRanges(t, "fades", sfFades(t, ta, state, *textLeafBlock(0)), mdRange(5, 11))
	readState(ta, state, func(s *TextViewState) struct{} {
		if s.streamFade.frame(time.Now().Add(fadeTestDuration+time.Second), false) != nil {
			t.Error("a finished fade still samples")
		}
		if s.streamFade.frame(time.Now(), false) != nil {
			t.Error("a finished fade was not dropped")
		}
		return struct{}{}
	})
}

// U:text/state.rs:1352-1359
func TestSetTextExtendingTheTextFadesLikePushStr(t *testing.T) {
	ta := textTestApp(t)
	state := sfFadingState(t, ta, "hello")
	sfSetText(ta, state, "hello world")
	sfWantRanges(t, "fades", sfFades(t, ta, state, *textLeafBlock(0)), mdRange(5, 11))
}

// U:text/state.rs:1361-1372
func TestSetTextReplacingTheTextShowsItAtOnce(t *testing.T) {
	ta := textTestApp(t)
	state := sfFadingState(t, ta, "hello")
	sfPushStr(ta, state, " world")
	if sfFades(t, ta, state, *textLeafBlock(0)) == nil {
		t.Fatal("the append does not fade")
	}
	sfSetText(ta, state, "other")
	sfWantRanges(t, "fades", sfFades(t, ta, state, *textLeafBlock(0)))
}

// U:text/state.rs:1374-1391
func TestCompletedMarkupRefadesFromTheDivergence(t *testing.T) {
	ta := textTestApp(t)
	// The paragraph renders `text` (trailing space trimmed), then
	// `text **bo` literally.
	state := sfFadingState(t, ta, "text ")
	sfPushStr(ta, state, "**bo")
	sfWantRanges(t, "fades", sfFades(t, ta, state, *textLeafBlock(0)), mdRange(4, 9))
	// `text **bold**` renders `text bold`: the space keeps its fade, the
	// glyphs from byte 5 on changed and fade again as one run.
	sfPushStr(ta, state, "ld**")
	sfWantRanges(t, "fades", sfFades(t, ta, state, *textLeafBlock(0)), mdRange(4, 5), mdRange(5, 9))
}

// U:text/state.rs:1393-1400
func TestWithoutStaggerAnUpdateFadesAsOneChunk(t *testing.T) {
	ta := textTestApp(t)
	state := sfFadingState(t, ta, "hello")
	sfPushStr(ta, state, " one two three")
	sfWantRanges(t, "fades", sfFades(t, ta, state, *textLeafBlock(0)), mdRange(5, 19))
}

// U:text/state.rs:1402-1431
func TestWordsOfOneUpdateStartOneAfterAnother(t *testing.T) {
	ta := textTestApp(t)
	stagger := 100 * time.Millisecond
	state := sfFadingState(t, ta, "hello")
	updateState(ta, state, func(s *TextViewState, _ *gpui.Context[TextViewState]) {
		s.SetMotion(DefaultTextViewMotion().WithStreamFade(fadeTestDuration).WithStreamFadeStagger(stagger).WithStreamFadeEasing(EasingLinear()))
	})
	sfPushStr(ta, state, " one two three")
	readState(ta, state, func(s *TextViewState) struct{} {
		frame := s.streamFade.frame(time.Now().Add(stagger*3), false)
		if frame == nil {
			t.Fatal("words no longer fading")
		}
		fades, ok := frame.fades(*textLeafBlock(0))
		if !ok {
			t.Fatal("the paragraph does not fade")
		}
		var ranges []gpui.TextRange
		for _, f := range fades {
			ranges = append(ranges, f.rng)
		}
		sfWantRanges(t, "ranges", ranges, mdRange(5, 10), mdRange(10, 14), mdRange(14, 19))
		// A later word has faded less, so it is still more transparent.
		if !(fades[0].fadeOut < fades[1].fadeOut && fades[1].fadeOut < fades[2].fadeOut) {
			t.Errorf("%+v", fades)
		}
		return struct{}{}
	})
}

// U:text/state.rs:1433-1441
func TestANewParagraphFadesAsAWhole(t *testing.T) {
	ta := textTestApp(t)
	state := sfFadingState(t, ta, "first")
	sfPushStr(ta, state, "\n\nsecond")
	sfWantRanges(t, "first", sfFades(t, ta, state, *textLeafBlock(0)))
	sfWantRanges(t, "second", sfFades(t, ta, state, *textLeafBlock(7)), mdRange(0, 6))
}

// U:text/state.rs:1443-1450
func TestCodeBlockTextFadesByBlock(t *testing.T) {
	ta := textTestApp(t)
	state := sfFadingState(t, ta, "```rs\nlet")
	sfPushStr(ta, state, " x")
	sfWantRanges(t, "fades", sfFades(t, ta, state, *textLeafBlock(0)), mdRange(3, 5))
}

// U:text/state.rs:1452-1463
func TestTableCellsFadeByOrdinal(t *testing.T) {
	ta := textTestApp(t)
	state := sfFadingState(t, ta, "| a | b |\n|---|---|\n| c | d")
	sfPushStr(ta, state, "e |")
	sfWantRanges(t, "cell 2", sfFades(t, ta, state, *textLeafTableCell(0, 2)))
	sfWantRanges(t, "cell 3", sfFades(t, ta, state, *textLeafTableCell(0, 3)), mdRange(1, 2))
}

// U:text/state.rs:1465-1476
func TestZeroDurationRecordsNothing(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, "hello")
	ta.RunUntilParked()
	sfPushStr(ta, state, " world")
	if readState(ta, state, func(s *TextViewState) *streamFadeFrame { return s.streamFade.frame(time.Now(), false) }) != nil {
		t.Fatal("an unstyled view fades")
	}
}

// U:text/state.rs:1478-1488
func TestReducedMotionDropsTheFade(t *testing.T) {
	ta := textTestApp(t)
	state := sfFadingState(t, ta, "hello")
	sfPushStr(ta, state, " world")
	readState(ta, state, func(s *TextViewState) struct{} {
		if s.streamFade.frame(time.Now(), true) != nil || s.streamFade.frame(time.Now(), false) != nil {
			t.Fatal("reduced motion kept the fade")
		}
		return struct{}{}
	})
}

type sfObserver struct{}

// U:text/state.rs:1490-1533
func TestAFadeRepaintsOnATimerUntilNothingFades(t *testing.T) {
	ta := textTestApp(t)
	state := sfFadingState(t, ta, "hello")
	_, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return NewTextView(state).IntoElement()
	})
	notifies := new(int)
	ta.Update(func(app *gpui.App) {
		observer := gpui.NewEntity(app, ta.RootScope(), func(*sfObserver, *gpui.Context[sfObserver]) {})
		observer.Update(app, func(_ *sfObserver, ocx *gpui.Context[sfObserver]) {
			ocx.Observe(state, func(*sfObserver, gpui.Entity[TextViewState], *gpui.Context[sfObserver]) { *notifies++ })
		})
	})
	sfPushStr(ta, state, " world")
	drawText(t, cx)
	before := *notifies

	tick := streamFadeTick.Milliseconds()
	ta.AdvanceClock(tick / 2)
	ta.RunUntilParked()
	if *notifies != before {
		t.Fatalf("notified %d times before the first tick", *notifies-before)
	}
	// Each tick repaints once, and that frame schedules the next tick.
	for n := 1; n <= 3; n++ {
		ta.AdvanceClock(tick)
		ta.RunUntilParked()
		if *notifies != before+n {
			t.Fatalf("tick %d: %d notifies, want %d", n, *notifies-before, n)
		}
	}
	// Replacing the text drops the fade, so the ticks stop.
	sfSetText(ta, state, "other")
	ta.AdvanceClock(tick)
	ta.RunUntilParked()
	after := *notifies
	ta.AdvanceClock(tick * 10)
	ta.RunUntilParked()
	if *notifies != after {
		t.Fatalf("ticks continued: %d more notifies", *notifies-after)
	}
	if readState(ta, state, func(s *TextViewState) bool { return s.fadeTick }) {
		t.Fatal("a tick is still scheduled")
	}
}
