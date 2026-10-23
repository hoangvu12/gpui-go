package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:time_field.rs (10/10).

func hms(t *testing.T, h, m, s uint32) NaiveTime {
	t.Helper()
	v, ok := NaiveTimeFromHMS(h, m, s)
	if !ok {
		t.Fatalf("invalid time %d:%d:%d", h, m, s)
	}
	return v
}

func timeEditor(precision TimePrecision, hourCycle HourCycle) *segmentEditor {
	e := newSegmentEditor()
	e.setPrecision(precision)
	e.setHourCycle(hourCycle)
	return &e
}

func typeKeys(e *segmentEditor, keys string) {
	for _, ch := range keys {
		switch ch {
		case 'a':
			e.inputPeriod(false)
		case 'p':
			e.inputPeriod(true)
		default:
			e.inputDigit(uint32(ch - '0'))
		}
	}
}

func wantTime(t *testing.T, e *segmentEditor, want NaiveTime) {
	t.Helper()
	if e.time != want {
		t.Fatalf("time = %v, want %v", e.time, want)
	}
}

func wantSegment(t *testing.T, e *segmentEditor, want TimeSegment) {
	t.Helper()
	if e.segment != want {
		t.Fatalf("segment = %v, want %v", e.segment, want)
	}
}

// U:time_field.rs:659-666
func TestTimeFieldTypingFillsSegmentsAndAdvances(t *testing.T) {
	e := timeEditor(TimePrecisionSecond, HourCycleH23)
	typeKeys(e, "093015")
	wantTime(t, e, hms(t, 9, 30, 15))
	// The last segment stays selected once it is complete.
	wantSegment(t, e, TimeSegmentSecond)
}

// U:time_field.rs:668-676
func TestTimeFieldADigitThatCannotStartTwoDigitsCompletesTheSegment(t *testing.T) {
	e := timeEditor(TimePrecisionMinute, HourCycleH23)
	typeKeys(e, "7")
	wantTime(t, e, hms(t, 7, 0, 0))
	wantSegment(t, e, TimeSegmentMinute)
	typeKeys(e, "8")
	wantTime(t, e, hms(t, 7, 8, 0))
}

// U:time_field.rs:678-685
func TestTimeFieldAnOutOfRangePairRestartsFromTheSecondDigit(t *testing.T) {
	e := timeEditor(TimePrecisionMinute, HourCycleH23)
	// 2 then 5 cannot form 25 hours, so 5 starts a new entry and
	// completes it.
	typeKeys(e, "25")
	wantTime(t, e, hms(t, 5, 0, 0))
	wantSegment(t, e, TimeSegmentMinute)
}

// U:time_field.rs:687-698
func TestTimeFieldSteppingWrapsWithoutCarry(t *testing.T) {
	e := timeEditor(TimePrecisionMinute, HourCycleH23)
	e.setTime(hms(t, 23, 59, 0))
	e.step(1)
	wantTime(t, e, hms(t, 0, 59, 0))
	e.selectSegment(TimeSegmentMinute)
	e.step(1)
	wantTime(t, e, hms(t, 0, 0, 0))
	e.step(-1)
	wantTime(t, e, hms(t, 0, 59, 0))
}

// U:time_field.rs:700-708
func TestTimeFieldSegmentMovementStopsAtTheEnds(t *testing.T) {
	e := timeEditor(TimePrecisionMinute, HourCycleH23)
	if e.moveSegment(-1) {
		t.Fatal("moved before the first segment")
	}
	if !e.moveSegment(1) {
		t.Fatal("did not move to the minute")
	}
	if e.moveSegment(1) {
		t.Fatal("moved past the last segment")
	}
	if e.selectSegment(TimeSegmentSecond) || e.selectSegment(TimeSegmentPeriod) {
		t.Fatal("selected a segment that is not shown")
	}
}

// U:time_field.rs:710-718
func TestTimeFieldPrecisionTruncatesSeconds(t *testing.T) {
	e := timeEditor(TimePrecisionSecond, HourCycleH23)
	e.setTime(hms(t, 9, 30, 15))
	e.selectSegment(TimeSegmentSecond)
	e.setPrecision(TimePrecisionMinute)
	wantTime(t, e, hms(t, 9, 30, 0))
	wantSegment(t, e, TimeSegmentHour)
}

// U:time_field.rs:720-727
func TestTimeFieldClearingResetsOnlyTheSelectedSegment(t *testing.T) {
	e := timeEditor(TimePrecisionMinute, HourCycleH23)
	e.setTime(hms(t, 9, 30, 0))
	e.selectSegment(TimeSegmentMinute)
	if !e.clearSegment() {
		t.Fatal("clearing reported no change")
	}
	wantTime(t, e, hms(t, 9, 0, 0))
}

// U:time_field.rs:729-742
func TestTimeFieldTwelveHourLabelsMapMidnightAndNoonToTwelve(t *testing.T) {
	e := timeEditor(TimePrecisionMinute, HourCycleH12)
	for _, c := range []struct {
		hour          uint32
		label, period string
	}{
		{0, "12", "AM"}, {9, "09", "AM"}, {12, "12", "PM"}, {23, "11", "PM"},
	} {
		e.setTime(hms(t, c.hour, 0, 0))
		if got := e.label(TimeSegmentHour); got != c.label {
			t.Fatalf("hour %d label = %q, want %q", c.hour, got, c.label)
		}
		if got := e.label(TimeSegmentPeriod); got != c.period {
			t.Fatalf("hour %d period = %q, want %q", c.hour, got, c.period)
		}
	}
}

// U:time_field.rs:744-760
func TestTimeFieldTwelveHourTypingKeepsThePeriodUntilItIsTyped(t *testing.T) {
	e := timeEditor(TimePrecisionMinute, HourCycleH12)
	// "1" may start 10-12, so it waits; "2" makes 12, which is midnight
	// in AM.
	typeKeys(e, "12")
	wantTime(t, e, hms(t, 0, 0, 0))
	typeKeys(e, "30p")
	wantTime(t, e, hms(t, 12, 30, 0))
	wantSegment(t, e, TimeSegmentPeriod)
	e.selectSegment(TimeSegmentHour)
	typeKeys(e, "9")
	wantTime(t, e, hms(t, 21, 30, 0))
	// "00" is not an hour on a 12-hour clock.
	e.selectSegment(TimeSegmentHour)
	typeKeys(e, "00")
	wantSegment(t, e, TimeSegmentHour)
}

// U:time_field.rs:762-774
func TestTimeFieldTwelveHourSteppingWrapsWithinThePeriod(t *testing.T) {
	e := timeEditor(TimePrecisionMinute, HourCycleH12)
	e.setTime(hms(t, 11, 0, 0))
	e.step(1)
	if e.time != hms(t, 0, 0, 0) {
		t.Fatalf("11 AM steps to %v, want 12 AM", e.time)
	}
	e.selectSegment(TimeSegmentPeriod)
	e.step(1)
	wantTime(t, e, hms(t, 12, 0, 0))
	e.step(1)
	wantTime(t, e, hms(t, 0, 0, 0))
	if e.inputDigit(1) {
		t.Fatal("digits do not edit the period")
	}
}

// timeFieldHarness renders a TimeField over its state (Go-own fixture).
type timeFieldHarness struct {
	state gpui.Entity[TimeFieldState]
}

func (h *timeFieldHarness) Render(*gpui.Window, *gpui.Context[timeFieldHarness]) gpui.AnyElement {
	return gpui.OwnRecipe(NewTimeField("time", h.state).W(gpui.Px(100)).H(gpui.Px(20)))
}

// Go-own: the rendered field takes keyboard edits through its key
// context, publishes its formatted value, and Tab leaves only from the
// last segment (U:time_field.rs:20-31, 400-434, 575-632).
func TestTimeFieldKeyboardEditingThroughTheRenderedField(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(Init)
	var state gpui.Entity[TimeFieldState]
	var changes []NaiveTime
	_, cx := ta.AddWindowView(func(h *timeFieldHarness, w *gpui.Window, cx *gpui.Context[timeFieldHarness]) {
		state = gpui.NewEntity(cx, w.Scope(), func(s *TimeFieldState, scx *gpui.Context[TimeFieldState]) {
			*s = NewTimeFieldState(w, scx)
		})
		TimeFieldEvents.Subscribe(cx, state, func(_ *timeFieldHarness, _ gpui.Entity[TimeFieldState], e *TimeFieldEvent, _ *gpui.Context[timeFieldHarness]) {
			changes = append(changes, e.Change)
		})
		h.state = state
	})
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state.Read(app, func(s *TimeFieldState, app *gpui.App) struct{} { s.Focus(w, app); return struct{}{} })
		if _, err := gpui.DrawWindowFrame(w); err != nil {
			t.Fatal(err)
		}
	})
	cx.SimulateKeystrokes("0 9 3 0 up")
	cx.RunUntilParked()
	got := state.Read(ta, func(s *TimeFieldState, _ *gpui.App) NaiveTime { return s.Time() })
	if got != hms(t, 9, 31, 0) {
		t.Fatalf("time = %v, want 09:31:00", got)
	}
	if len(changes) != 4 || changes[3] != hms(t, 9, 31, 0) {
		t.Fatalf("changes = %v", changes)
	}
	if seg := state.Read(ta, func(s *TimeFieldState, _ *gpui.App) TimeSegment { return s.SelectedSegment() }); seg != TimeSegmentMinute {
		t.Fatalf("segment = %v, want minute", seg)
	}
	cx.SimulateKeystrokes("shift-tab")
	if seg := state.Read(ta, func(s *TimeFieldState, _ *gpui.App) TimeSegment { return s.SelectedSegment() }); seg != TimeSegmentHour {
		t.Fatalf("shift-tab: segment = %v, want hour", seg)
	}
	// The aria value is the time in the precision's format.
	if v, err := got.Format(TimePrecisionMinute.Format(HourCycleH12)); err != nil || v != "09:31 AM" {
		t.Fatalf("12-hour format = %q, %v", v, err)
	}
}
