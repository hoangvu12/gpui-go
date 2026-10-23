package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:calendar.rs (9/9).

func ymd(t *testing.T, y int, m, d uint32) NaiveDate {
	t.Helper()
	v, ok := NaiveDateFromYMD(y, m, d)
	if !ok {
		t.Fatalf("invalid date %d-%d-%d", y, m, d)
	}
	return v
}

// calendarNames is U:calendar.rs:864-881 Names: every item records its
// kind and accessible label.
type calendarNames struct {
	calendar gpui.Entity[CalendarState]
	labels   *[]calendarLabel
}

type calendarLabel struct {
	kind  CalendarItemKind
	label string
}

func (n *calendarNames) Render(*gpui.Window, *gpui.Context[calendarNames]) gpui.AnyElement {
	labels := n.labels
	*labels = (*labels)[:0]
	return gpui.OwnRecipe(NewCalendar("named-calendar", n.calendar).Item(
		func(item *CalendarItem, state CalendarItemState, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
			var spec gpui.A11ySpec
			item.base.WriteA11yInfo(&spec)
			*labels = append(*labels, calendarLabel{state.Kind(), spec.Label})
			return gpui.OwnRecipe(item)
		}))
}

// U:calendar.rs:861-908
func TestCalendarItemsExposeNamesInEveryView(t *testing.T) {
	ta := gpui.NewTestApp()
	view, cx := ta.AddWindowView(func(n *calendarNames, w *gpui.Window, cx *gpui.Context[calendarNames]) {
		n.labels = new([]calendarLabel)
		n.calendar = gpui.NewEntity(cx, w.Scope(), func(s *CalendarState, scx *gpui.Context[CalendarState]) {
			*s = NewCalendarState(w, scx)
		})
	})
	for _, mode := range []CalendarView{CalendarViewDay, CalendarViewMonth, CalendarViewYear} {
		cx.Update(func(w *gpui.Window, app *gpui.App) {
			calendar := view.Read(app, func(n *calendarNames, _ *gpui.App) gpui.Entity[CalendarState] { return n.calendar })
			calendar.Update(app, func(s *CalendarState, cx *gpui.Context[CalendarState]) {
				s.ApplyDate(DateOf(ymd(t, 2026, 9, 7)))
				s.SetView(mode)
				cx.Notify()
			})
			if _, err := gpui.DrawWindowFrame(w); err != nil {
				t.Fatal(err)
			}
			labels := view.Read(app, func(n *calendarNames, _ *gpui.App) []calendarLabel { return slices.Clone(*n.labels) })
			if len(labels) == 0 {
				t.Fatalf("view %v rendered no items", mode)
			}
			for _, l := range labels {
				if l.label == "" {
					t.Fatalf("missing name for %v", l.kind)
				}
			}
			if mode.IsDay() && !slices.Contains(labels, calendarLabel{CalendarItemDay, "2026-09-07"}) {
				t.Fatal("the day item is not named by its date")
			}
		})
	}
}

// calendarState is U:calendar.rs:936-942: a state view with date.
func calendarState(t *testing.T, date Date) (*gpui.TestApp, gpui.Entity[CalendarState]) {
	t.Helper()
	ta := gpui.NewTestApp()
	state, _ := ta.AddWindowView(func(s *CalendarState, w *gpui.Window, cx *gpui.Context[CalendarState]) {
		*s = NewCalendarState(w, cx)
	})
	state.Update(ta, func(s *CalendarState, _ *gpui.Context[CalendarState]) { s.date = date })
	return ta, state
}

func updateCalendar(ta *gpui.TestApp, s gpui.Entity[CalendarState], f func(*CalendarState, *gpui.Context[CalendarState])) {
	s.Update(ta, f)
}

func readDate(ta *gpui.TestApp, s gpui.Entity[CalendarState]) Date {
	return s.Read(ta, func(s *CalendarState, _ *gpui.App) Date { return s.Date() })
}

// U:calendar.rs:943-958
func TestCalendarRangeSelectionRestartsAndCompletes(t *testing.T) {
	ta, s := calendarState(t, DateRange(nil, nil))
	a, b := ymd(t, 2025, 2, 10), ymd(t, 2025, 2, 12)
	updateCalendar(ta, s, func(s *CalendarState, _ *gpui.Context[CalendarState]) {
		if s.SelectDate(a) {
			t.Fatal("a range start completed the value")
		}
		if !s.SelectDate(b) {
			t.Fatal("the range end did not complete the value")
		}
	})
	if got := readDate(ta, s); got != DateRange(&a, &b) {
		t.Fatalf("date = %v", got)
	}
	updateCalendar(ta, s, func(s *CalendarState, _ *gpui.Context[CalendarState]) {
		if s.SelectDate(a) {
			t.Fatal("restarting completed the value")
		}
	})
	if got := readDate(ta, s); got != DateRange(&a, nil) {
		t.Fatalf("date = %v", got)
	}
}

// U:calendar.rs:959-971
func TestCalendarDisabledDateIsRejected(t *testing.T) {
	ta, s := calendarState(t, DateSingle(nil))
	from, to := ymd(t, 2025, 1, 1), ymd(t, 2025, 1, 31)
	updateCalendar(ta, s, func(s *CalendarState, _ *gpui.Context[CalendarState]) {
		m := MatcherRange(&from, &to)
		s.disabledMatcher = &m
	})
	updateCalendar(ta, s, func(s *CalendarState, _ *gpui.Context[CalendarState]) {
		if s.SelectDate(ymd(t, 2025, 1, 2)) {
			t.Fatal("a disabled date was selected")
		}
	})
}

// U:calendar.rs:972-993
func TestCalendarMonthNavigationCrossesYear(t *testing.T) {
	ta, s := calendarState(t, DateOf(ymd(t, 2025, 1, 1)))
	ym := func() (int, uint8) {
		type pair struct {
			y int
			m uint8
		}
		p := s.Read(ta, func(s *CalendarState, _ *gpui.App) pair { return pair{s.CurrentYear(), s.CurrentMonth()} })
		return p.y, p.m
	}
	updateCalendar(ta, s, func(s *CalendarState, _ *gpui.Context[CalendarState]) {
		s.ApplyDate(DateOf(ymd(t, 2025, 1, 1)))
		s.PrevMonth()
	})
	if y, m := ym(); y != 2024 || m != 12 {
		t.Fatalf("prev = %d-%d, want 2024-12", y, m)
	}
	updateCalendar(ta, s, func(s *CalendarState, _ *gpui.Context[CalendarState]) { s.NextMonth() })
	if y, m := ym(); y != 2025 || m != 1 {
		t.Fatalf("next = %d-%d, want 2025-1", y, m)
	}
}

// U:calendar.rs:995-1010
func TestCalendarSixWeekMonthIsNotTruncated(t *testing.T) {
	ta, s := calendarState(t, DateOf(ymd(t, 2025, 8, 1)))
	updateCalendar(ta, s, func(s *CalendarState, _ *gpui.Context[CalendarState]) {
		s.ApplyDate(DateOf(ymd(t, 2025, 8, 1)))
		months := s.MonthDays()
		if len(months) != 1 || len(months[0]) != 6 || len(s.Days()) != 6 {
			t.Fatalf("months = %d, weeks = %d, days = %d", len(months), len(months[0]), len(s.Days()))
		}
		if d := months[0][5][0].Day(); d != 31 {
			t.Fatalf("last week starts on %d, want 31", d)
		}
	})
}

// U:calendar.rs:1012-1024
func TestCalendarDayMonthAndYearViewsHaveCompleteTransitions(t *testing.T) {
	ta, s := calendarState(t, DateSingle(nil))
	updateCalendar(ta, s, func(s *CalendarState, _ *gpui.Context[CalendarState]) {
		if s.View() != CalendarViewDay {
			t.Fatal("not in day view")
		}
		s.SetView(CalendarViewMonth)
		s.SelectMonth(11)
		if s.View() != CalendarViewDay || s.CurrentMonth() != 11 {
			t.Fatalf("after month pick: %v, %d", s.View(), s.CurrentMonth())
		}
		s.SetView(CalendarViewYear)
		s.SelectYear(2032)
		if s.View() != CalendarViewDay || s.CurrentYear() != 2032 {
			t.Fatalf("after year pick: %v, %d", s.View(), s.CurrentYear())
		}
	})
}

// U:calendar.rs:1026-1031
func TestCalendarPickerViewsUseStableGridLayouts(t *testing.T) {
	if c, g, ok := pickerGridLayout(CalendarViewMonth); !ok || c != 3 || g != 4 {
		t.Fatal("month grid")
	}
	if c, g, ok := pickerGridLayout(CalendarViewYear); !ok || c != 5 || g != 4 {
		t.Fatal("year grid")
	}
	if _, _, ok := pickerGridLayout(CalendarViewDay); ok {
		t.Fatal("day view has a grid")
	}
}

// U:calendar.rs:1033-1046
func TestCalendarYearPageNavigationRespectsBothBounds(t *testing.T) {
	ta, s := calendarState(t, DateSingle(nil))
	updateCalendar(ta, s, func(s *CalendarState, _ *gpui.Context[CalendarState]) {
		s.applyYearRange(2000, 2041)
		for s.PrevYearPage() {
		}
		if s.HasPrevYearPage() || s.PrevYearPage() {
			t.Fatal("paged before the first page")
		}
		if !s.NextYearPage() {
			t.Fatal("could not page forward")
		}
		for s.NextYearPage() {
		}
		if s.HasNextYearPage() || s.NextYearPage() {
			t.Fatal("paged past the last page")
		}
	})
}

// calendarEventHarness is U:calendar.rs:910-935 EventHarness.
type calendarEventHarness struct {
	calendar gpui.Entity[CalendarState]
	events   []Date
}

func (*calendarEventHarness) Render(*gpui.Window, *gpui.Context[calendarEventHarness]) gpui.AnyElement {
	return gpui.Empty()
}

// U:calendar.rs:1048-1077
func TestCalendarActivationEmitsOnlyForCompleteEnabledValues(t *testing.T) {
	ta := gpui.NewTestApp()
	harness, _ := ta.AddWindowView(func(h *calendarEventHarness, w *gpui.Window, cx *gpui.Context[calendarEventHarness]) {
		h.calendar = gpui.NewEntity(cx, w.Scope(), func(s *CalendarState, scx *gpui.Context[CalendarState]) {
			*s = NewCalendarState(w, scx)
		})
		CalendarEvents.Subscribe(cx, h.calendar, func(h *calendarEventHarness, _ gpui.Entity[CalendarState], e *CalendarEvent, _ *gpui.Context[calendarEventHarness]) {
			h.events = append(h.events, e.Selected)
		})
	})
	s := harness.Read(ta, func(h *calendarEventHarness, _ *gpui.App) gpui.Entity[CalendarState] { return h.calendar })
	events := func() []Date {
		ta.RunUntilParked()
		return harness.Read(ta, func(h *calendarEventHarness, _ *gpui.App) []Date { return slices.Clone(h.events) })
	}
	updateCalendar(ta, s, func(s *CalendarState, _ *gpui.Context[CalendarState]) { s.date = DateRange(nil, nil) })
	start, end := ymd(t, 2025, 4, 4), ymd(t, 2025, 4, 8)
	updateCalendar(ta, s, func(s *CalendarState, cx *gpui.Context[CalendarState]) {
		if s.ActivateDate(start, cx) {
			t.Fatal("the range start completed the value")
		}
		if !s.ActivateDate(end, cx) {
			t.Fatal("the range end did not complete the value")
		}
		if s.Date() != DateRange(&start, &end) {
			t.Fatalf("date = %v", s.Date())
		}
		m := MatcherCustom(func(d NaiveDate) bool { return d == start })
		s.SetDisabledMatcherShared(&m)
		if s.ActivateDate(start, cx) {
			t.Fatal("a disabled date was activated")
		}
		if s.Date() != DateRange(&start, &end) {
			t.Fatalf("date = %v", s.Date())
		}
	})
	if got := events(); !slices.Equal(got, []Date{DateRange(&start, &end)}) {
		t.Fatalf("events = %v", got)
	}

	updateCalendar(ta, s, func(s *CalendarState, cx *gpui.Context[CalendarState]) {
		s.date = DateSingle(nil)
		if !s.ActivateDate(end, cx) {
			t.Fatal("a single date did not complete the value")
		}
		if s.Date() != DateSingle(&end) {
			t.Fatalf("date = %v", s.Date())
		}
	})
	if got := events(); !slices.Equal(got, []Date{DateRange(&start, &end), DateSingle(&end)}) {
		t.Fatalf("events = %v", got)
	}
}

// Go-own: NaiveDate's Display, Date's Display and format
// (U:calendar.rs:19-29, 61-69; chrono NaiveDate Display).
func TestCalendarDateDisplayAndFormat(t *testing.T) {
	a, b := ymd(t, 2025, 4, 4), ymd(t, 2025, 4, 8)
	if got := DateRange(&a, nil).String(); got != "2025-04-04 - nil" {
		t.Fatalf("display = %q", got)
	}
	if got := DateSingle(nil).String(); got != "nil" {
		t.Fatalf("display = %q", got)
	}
	if got, ok := DateOfRange(a, b).Format("%d/%m"); !ok || got != "04/04 - 08/04" {
		t.Fatalf("format = %q, %v", got, ok)
	}
	if _, ok := DateRange(&a, nil).Format("%d"); ok {
		t.Fatal("an incomplete range formatted")
	}
	if _, ok := NaiveDateFromYMD(2025, 2, 29); ok {
		t.Fatal("2025-02-29 exists")
	}
}
