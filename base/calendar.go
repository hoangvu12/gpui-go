package base

// Port of U:calendar.rs: the controlled calendar value, the disabled-date
// matchers, CalendarState and the unstyled Calendar.
//
// chrono's NaiveDate is NaiveDate below (a proleptic Gregorian date
// without a time zone); chrono's Weekday is time.Weekday, whose values
// are num_days_from_sunday. Date::format goes through base/strftime
// (ticket 15).

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/hoangvu12/gpui-go/base/strftime"
	"github.com/hoangvu12/gpui-go/gpui"
)

// NaiveDate is a calendar date without a time zone: the chrono NaiveDate
// subset calendar.rs uses (from_ymd_opt, Datelike's year / month / day /
// weekday, ordering, day arithmetic, Display and format). The zero value
// is not a valid date; build dates with NaiveDateFromYMD.
type NaiveDate struct {
	year  int
	month time.Month
	day   int
}

// NaiveDateFromYMD is NaiveDate::from_ymd_opt: ok is false for a day that
// does not exist.
func NaiveDateFromYMD(year int, month, day uint32) (NaiveDate, bool) {
	if month < 1 || month > 12 || day < 1 {
		return NaiveDate{}, false
	}
	t := time.Date(year, time.Month(month), int(day), 0, 0, 0, 0, time.UTC)
	if t.Month() != time.Month(month) || t.Day() != int(day) {
		return NaiveDate{}, false
	}
	return NaiveDate{year, time.Month(month), int(day)}, true
}

func naiveDateOf(t time.Time) NaiveDate {
	return NaiveDate{t.Year(), t.Month(), t.Day()}
}

func (d NaiveDate) time() time.Time {
	return time.Date(d.year, d.month, d.day, 0, 0, 0, 0, time.UTC)
}

// Year is Datelike::year.
func (d NaiveDate) Year() int { return d.year }

// Month is Datelike::month, 1-based.
func (d NaiveDate) Month() uint32 { return uint32(d.month) }

// Day is Datelike::day, 1-based.
func (d NaiveDate) Day() uint32 { return uint32(d.day) }

// Weekday is Datelike::weekday.
func (d NaiveDate) Weekday() time.Weekday { return d.time().Weekday() }

// AddDays is `date + Duration::days(n)`.
func (d NaiveDate) AddDays(n int) NaiveDate { return naiveDateOf(d.time().AddDate(0, 0, n)) }

// Compare orders dates (NaiveDate's Ord): -1, 0 or +1.
func (d NaiveDate) Compare(other NaiveDate) int { return d.time().Compare(other.time()) }

// daysSince is `(d - other).num_days()`.
func (d NaiveDate) daysSince(other NaiveDate) int {
	return int(d.time().Sub(other.time()).Hours() / 24)
}

// String is NaiveDate's Display: %Y-%m-%d, the year with a sign outside
// 0..=9999 (chrono naive/date Debug/Display).
func (d NaiveDate) String() string {
	if d.year >= 0 && d.year <= 9999 {
		return fmt.Sprintf("%04d-%02d-%02d", d.year, int(d.month), d.day)
	}
	return fmt.Sprintf("%+05d-%02d-%02d", d.year, int(d.month), d.day)
}

// Format is `NaiveDate::format(layout).to_string()` (chrono strftime,
// base/strftime). A malformed layout yields ErrBadFormat where chrono's
// to_string would panic; time specifiers render midnight.
func (d NaiveDate) Format(layout string) (string, error) {
	return strftime.Format(d.time(), layout)
}

// Date is a controlled calendar value (U:calendar.rs:12-17): a single
// date or a range, each end optional. Dates compare with ==.
type Date struct {
	isRange  bool
	start    NaiveDate
	hasStart bool
	end      NaiveDate
	hasEnd   bool
}

// DateSingle is Date::Single(value); nil is None.
func DateSingle(value *NaiveDate) Date {
	d := Date{}
	if value != nil {
		d.start, d.hasStart = *value, true
	}
	return d
}

// DateRange is Date::Range(start, end); nil is None.
func DateRange(start, end *NaiveDate) Date {
	d := Date{isRange: true}
	if start != nil {
		d.start, d.hasStart = *start, true
	}
	if end != nil {
		d.end, d.hasEnd = *end, true
	}
	return d
}

// DateOf is `From<NaiveDate> for Date` (U:calendar.rs:31-35).
func DateOf(value NaiveDate) Date { return DateSingle(&value) }

// DateOfRange is `From<(NaiveDate, NaiveDate)> for Date`
// (U:calendar.rs:36-40).
func DateOfRange(start, end NaiveDate) Date { return DateRange(&start, &end) }

// String is Date's Display (U:calendar.rs:19-29).
func (d Date) String() string {
	switch {
	case !d.isRange && d.hasStart:
		return d.start.String()
	case !d.isRange || (!d.hasStart && !d.hasEnd):
		return "nil"
	case d.hasStart && d.hasEnd:
		return d.start.String() + " - " + d.end.String()
	case d.hasStart:
		return d.start.String() + " - nil"
	default:
		return "nil - " + d.end.String()
	}
}

// IsSome is U:calendar.rs:43-45.
func (d Date) IsSome() bool { return d.hasStart }

// IsComplete is U:calendar.rs:46-48.
func (d Date) IsComplete() bool { return d.hasStart && (!d.isRange || d.hasEnd) }

// Start is U:calendar.rs:49-54.
func (d Date) Start() (NaiveDate, bool) { return d.start, d.hasStart }

// End is U:calendar.rs:55-60.
func (d Date) End() (NaiveDate, bool) {
	if d.isRange && d.hasEnd {
		return d.end, true
	}
	return NaiveDate{}, false
}

// Format is U:calendar.rs:61-69: ok is false for an incomplete value. A
// malformed layout is formatted leniently (chrono's to_string panics).
func (d Date) Format(layout string) (string, bool) {
	switch {
	case !d.isRange && d.hasStart:
		return strftime.FormatLenient(d.start.time(), layout), true
	case d.isRange && d.hasStart && d.hasEnd:
		return strftime.FormatLenient(d.start.time(), layout) + " - " + strftime.FormatLenient(d.end.time(), layout), true
	}
	return "", false
}

// IsActive is U:calendar.rs:70-75.
func (d Date) IsActive(value NaiveDate) bool {
	if !d.isRange {
		return d.hasStart && d.start == value
	}
	return (d.hasStart && d.start == value) || (d.hasEnd && d.end == value)
}

// IsSingle is U:calendar.rs:76-78.
func (d Date) IsSingle() bool { return !d.isRange }

// IsInRange is U:calendar.rs:79-81.
func (d Date) IsInRange(value NaiveDate) bool {
	return d.isRange && d.hasStart && d.hasEnd && value.Compare(d.start) >= 0 && value.Compare(d.end) <= 0
}

// matcherKind selects a Matcher variant (U:calendar.rs:92-97).
type matcherKind uint8

const (
	matcherDayOfWeek matcherKind = iota
	matcherInterval
	matcherRange
	matcherCustom
)

// Matcher matches disabled dates (U:calendar.rs:84-97): days of the
// week, an open interval outside [before, after], a closed range, or a
// custom predicate. The IntervalMatcher and RangeMatcher payloads
// (U:calendar.rs:84-91, fields private, no methods) are the bounds
// carried here.
type Matcher struct {
	kind   matcherKind
	days   []uint32
	lo, hi *NaiveDate
	custom func(NaiveDate) bool
}

// MatcherDayOfWeek is Matcher::DayOfWeek, also `From<Vec<u32>>`
// (U:calendar.rs:93, 98-102): days are num_days_from_sunday values.
func MatcherDayOfWeek(days ...uint32) Matcher {
	return Matcher{kind: matcherDayOfWeek, days: slices.Clone(days)}
}

// MatcherInterval is Matcher::interval (U:calendar.rs:109-111): dates
// before `before` or after `after` match.
func MatcherInterval(before, after *NaiveDate) Matcher {
	return Matcher{kind: matcherInterval, lo: before, hi: after}
}

// MatcherRange is Matcher::range (U:calendar.rs:112-114): dates within
// [from, to] match; a nil bound is open.
func MatcherRange(from, to *NaiveDate) Matcher {
	return Matcher{kind: matcherRange, lo: from, hi: to}
}

// MatcherCustom is Matcher::custom, also `From<F>`
// (U:calendar.rs:103-107, 115-117).
func MatcherCustom(f func(NaiveDate) bool) Matcher {
	if f == nil {
		panic("base: MatcherCustom requires a function")
	}
	return Matcher{kind: matcherCustom, custom: f}
}

// IsMatch is U:calendar.rs:118-124.
func (m *Matcher) IsMatch(date Date) bool {
	switch {
	case !date.isRange && date.hasStart:
		return m.Matched(date.start)
	case date.isRange && date.hasStart && date.hasEnd:
		return m.Matched(date.start) || m.Matched(date.end)
	}
	return false
}

// Matched is U:calendar.rs:125-136.
func (m *Matcher) Matched(date NaiveDate) bool {
	switch m.kind {
	case matcherDayOfWeek:
		return slices.Contains(m.days, uint32(date.Weekday()))
	case matcherInterval:
		return (m.lo != nil && date.Compare(*m.lo) < 0) || (m.hi != nil && date.Compare(*m.hi) > 0)
	case matcherRange:
		return !(m.lo != nil && date.Compare(*m.lo) < 0) && !(m.hi != nil && date.Compare(*m.hi) > 0)
	default:
		return m.custom(date)
	}
}

// CalendarView is the calendar's view (U:calendar.rs:139-155).
type CalendarView uint8

const (
	// CalendarViewDay shows the days of the visible months.
	CalendarViewDay CalendarView = iota
	// CalendarViewMonth shows the months of the year.
	CalendarViewMonth
	// CalendarViewYear shows a page of years.
	CalendarViewYear
)

// IsDay is U:calendar.rs:146-148.
func (v CalendarView) IsDay() bool { return v == CalendarViewDay }

// IsMonth is U:calendar.rs:149-151.
func (v CalendarView) IsMonth() bool { return v == CalendarViewMonth }

// IsYear is U:calendar.rs:152-154.
func (v CalendarView) IsYear() bool { return v == CalendarViewYear }

// pickerGridLayout is the column count and horizontal gap of the month
// and year grids (U:calendar.rs:157-163).
func pickerGridLayout(view CalendarView) (columns uint16, gap float32, ok bool) {
	switch view {
	case CalendarViewMonth:
		return 3, 4, true
	case CalendarViewYear:
		return 5, 4, true
	}
	return 0, 0, false
}

// CalendarEvent is emitted when the controlled value is complete
// (U:calendar.rs:165-167, CalendarEvent::Selected).
type CalendarEvent struct {
	Selected Date
}

// CalendarEvents is the CalendarState event descriptor
// (U:calendar.rs:390).
var CalendarEvents = gpui.DefineEvent[CalendarState, CalendarEvent]()

// CalendarState is the calendar's state (U:calendar.rs:169-180).
type CalendarState struct {
	FocusHandle    gpui.FocusHandle
	view           CalendarView
	date           Date
	currentYear    int
	currentMonth   uint8
	years          [][]int
	yearPage       int
	today          NaiveDate
	numberOfMonths int
	// disabledMatcher is shared like upstream's Option<Rc<Matcher>>.
	disabledMatcher *Matcher
}

// NewCalendarState is CalendarState::new (U:calendar.rs:183-198): it
// shows today's month with years today±50.
func NewCalendarState(_ *gpui.Window, cx *gpui.Context[CalendarState]) CalendarState {
	today := naiveDateOf(time.Now())
	return CalendarState{
		FocusHandle:    cx.App().FocusHandle(),
		view:           CalendarViewDay,
		date:           DateSingle(nil),
		currentYear:    today.Year(),
		currentMonth:   uint8(today.Month()),
		today:          today,
		numberOfMonths: 1,
	}.YearRange(today.Year()-50, today.Year()+50)
}

// DisabledMatcher is U:calendar.rs:199-202.
func (s CalendarState) DisabledMatcher(matcher Matcher) CalendarState {
	s.disabledMatcher = &matcher
	return s
}

// SetDisabledMatcher is U:calendar.rs:203-210.
func (s *CalendarState) SetDisabledMatcher(matcher Matcher, _ *gpui.Window, _ *gpui.Context[CalendarState]) {
	s.disabledMatcher = &matcher
}

// SetDisabledMatcherShared is U:calendar.rs:211-213.
func (s *CalendarState) SetDisabledMatcherShared(matcher *Matcher) { s.disabledMatcher = matcher }

// DisabledMatcherRef is U:calendar.rs:214-216.
func (s *CalendarState) DisabledMatcherRef() *Matcher { return s.disabledMatcher }

// SetDate is U:calendar.rs:217-221.
func (s *CalendarState) SetDate(date Date, _ *gpui.Window, cx *gpui.Context[CalendarState]) {
	if s.ApplyDate(date) {
		cx.Notify()
	}
}

// ApplyDate is U:calendar.rs:222-236: a disabled value is refused.
func (s *CalendarState) ApplyDate(date Date) bool {
	if s.disabledMatcher != nil && s.disabledMatcher.IsMatch(date) {
		return false
	}
	s.date = date
	if v, ok := date.Start(); ok {
		s.currentMonth = uint8(v.Month())
		s.currentYear = v.Year()
	}
	return true
}

// SelectDate is U:calendar.rs:237-257: it reports whether the value is
// complete afterwards.
func (s *CalendarState) SelectDate(value NaiveDate) bool {
	if s.disabledMatcher != nil && s.disabledMatcher.Matched(value) {
		return false
	}
	var next Date
	d := s.date
	switch {
	case !d.isRange:
		next = DateSingle(&value)
	case !d.hasStart:
		next = DateRange(&value, nil)
	case !d.hasEnd && value.Compare(d.start) >= 0:
		start := d.start
		next = DateRange(&start, &value)
	default:
		next = DateRange(&value, nil)
	}
	s.ApplyDate(next)
	return s.date.IsComplete()
}

// ActivateDate activates a day item and emits CalendarEvent once the
// controlled value is complete; it is the single pointer/keyboard
// activation path used by the calendar root (U:calendar.rs:258-268).
func (s *CalendarState) ActivateDate(value NaiveDate, cx *gpui.Context[CalendarState]) bool {
	complete := s.SelectDate(value)
	if complete {
		CalendarEvents.Emit(cx, CalendarEvent{Selected: s.date})
	}
	cx.Notify()
	return complete
}

// Date is U:calendar.rs:269-271.
func (s *CalendarState) Date() Date { return s.date }

// SetNumberOfMonths is U:calendar.rs:272-275.
func (s *CalendarState) SetNumberOfMonths(n int, _ *gpui.Window, cx *gpui.Context[CalendarState]) {
	s.numberOfMonths = n
	cx.Notify()
}

// NumberOfMonths is U:calendar.rs:276-278.
func (s *CalendarState) NumberOfMonths() int { return s.numberOfMonths }

// YearRange sets the selectable years [start, end) (U:calendar.rs:279-282).
func (s CalendarState) YearRange(start, end int) CalendarState {
	s.applyYearRange(start, end)
	return s
}

// SetYearRange is U:calendar.rs:283-286.
func (s *CalendarState) SetYearRange(start, end int, cx *gpui.Context[CalendarState]) {
	s.applyYearRange(start, end)
	cx.Notify()
}

// applyYearRange pages the years by 20 (U:calendar.rs:287-298).
func (s *CalendarState) applyYearRange(start, end int) {
	s.years = nil
	for y := start; y < end; y += 20 {
		page := make([]int, 0, 20)
		for v := y; v < end && v < y+20; v++ {
			page = append(page, v)
		}
		s.years = append(s.years, page)
	}
	s.yearPage = 0
	for i, page := range s.years {
		if slices.Contains(page, s.currentYear) {
			s.yearPage = i
			break
		}
	}
}

// OffsetYearMonth is the year and month offset months after the current
// one (U:calendar.rs:299-305).
func (s *CalendarState) OffsetYearMonth(offset int) (int, uint32) {
	n := int64(s.currentMonth) - 1 + int64(offset)
	return s.currentYear + int(floorDiv64(n, 12)), uint32(floorMod64(n, 12)) + 1
}

// Days is every visible week (U:calendar.rs:306-308).
func (s *CalendarState) Days() [][]NaiveDate {
	var out [][]NaiveDate
	for _, month := range s.MonthDays() {
		out = append(out, month...)
	}
	return out
}

// MonthDays is the calendar weeks grouped by visible month; it preserves
// six-week months and is the preferred rendering API
// (U:calendar.rs:309-321).
func (s *CalendarState) MonthDays() [][][]NaiveDate {
	out := make([][][]NaiveDate, 0, s.numberOfMonths)
	for n := 0; n < s.numberOfMonths; n++ {
		out = append(out, daysInMonth(s.currentYear, uint32(s.currentMonth)+uint32(n), time.Sunday))
	}
	return out
}

// HasPrevYearPage is U:calendar.rs:322-324.
func (s *CalendarState) HasPrevYearPage() bool { return s.yearPage > 0 }

// HasNextYearPage is U:calendar.rs:325-327.
func (s *CalendarState) HasNextYearPage() bool { return s.yearPage < len(s.years)-1 }

// PrevYearPage is U:calendar.rs:328-335.
func (s *CalendarState) PrevYearPage() bool {
	if !s.HasPrevYearPage() {
		return false
	}
	s.yearPage--
	return true
}

// NextYearPage is U:calendar.rs:336-343.
func (s *CalendarState) NextYearPage() bool {
	if !s.HasNextYearPage() {
		return false
	}
	s.yearPage++
	return true
}

// PrevMonth is U:calendar.rs:344-351.
func (s *CalendarState) PrevMonth() {
	if s.currentMonth == 1 {
		s.currentYear--
		s.currentMonth = 12
	} else {
		s.currentMonth--
	}
}

// NextMonth is U:calendar.rs:352-359.
func (s *CalendarState) NextMonth() {
	if s.currentMonth == 12 {
		s.currentYear++
		s.currentMonth = 1
	} else {
		s.currentMonth++
	}
}

// View is U:calendar.rs:360-362.
func (s *CalendarState) View() CalendarView { return s.view }

// SetView is U:calendar.rs:363-365.
func (s *CalendarState) SetView(view CalendarView) { s.view = view }

// CurrentYear is U:calendar.rs:366-368.
func (s *CalendarState) CurrentYear() int { return s.currentYear }

// CurrentMonth is U:calendar.rs:369-371.
func (s *CalendarState) CurrentMonth() uint8 { return s.currentMonth }

// Today is U:calendar.rs:372-374.
func (s *CalendarState) Today() NaiveDate { return s.today }

// YearsOnPage is U:calendar.rs:375-380.
func (s *CalendarState) YearsOnPage() []int {
	if s.yearPage >= 0 && s.yearPage < len(s.years) {
		return s.years[s.yearPage]
	}
	return nil
}

// SelectMonth is U:calendar.rs:381-384.
func (s *CalendarState) SelectMonth(month uint8) {
	s.currentMonth = month
	s.view = CalendarViewDay
}

// SelectYear is U:calendar.rs:385-388.
func (s *CalendarState) SelectYear(year int) {
	s.currentYear = year
	s.view = CalendarViewDay
}

// Render renders nothing (U:calendar.rs:391-395).
func (s *CalendarState) Render(*gpui.Window, *gpui.Context[CalendarState]) gpui.AnyElement {
	return gpui.Empty()
}

// CalendarItemKind is the semantic kind of a calendar control rendered by
// Calendar (U:calendar.rs:397-408).
type CalendarItemKind uint8

const (
	CalendarItemPrevious CalendarItemKind = iota
	CalendarItemMonthToggle
	CalendarItemYearToggle
	CalendarItemNext
	CalendarItemWeekday
	CalendarItemDay
	CalendarItemMonth
	CalendarItemYear
)

// String is the variant name (the derived Debug the item ids use).
func (k CalendarItemKind) String() string {
	return [...]string{"Previous", "MonthToggle", "YearToggle", "Next", "Weekday", "Day", "Month", "Year"}[k]
}

// CalendarItemState is the state exposed to a calendar item slot;
// applications may use it solely to decorate the unstyled primitive
// (U:calendar.rs:410-489).
type CalendarItemState struct {
	kind     CalendarItemKind
	active   bool
	inRange  bool
	muted    bool
	disabled bool
	today    bool
}

// NewCalendarItemState creates a state for kind with every flag off
// (U:calendar.rs:426-436).
func NewCalendarItemState(kind CalendarItemKind) CalendarItemState {
	return CalendarItemState{kind: kind}
}

// Active is U:calendar.rs:438-441.
func (s CalendarItemState) Active(active bool) CalendarItemState { s.active = active; return s }

// InRange sets whether the item is between the two ends of a range
// selection (U:calendar.rs:443-447).
func (s CalendarItemState) InRange(inRange bool) CalendarItemState { s.inRange = inRange; return s }

// Muted sets whether the item is shown as secondary, e.g. a weekday
// header or a day of the neighboring month (U:calendar.rs:449-454).
func (s CalendarItemState) Muted(muted bool) CalendarItemState { s.muted = muted; return s }

// Disabled is U:calendar.rs:456-459.
func (s CalendarItemState) Disabled(disabled bool) CalendarItemState { s.disabled = disabled; return s }

// Today is U:calendar.rs:461-464.
func (s CalendarItemState) Today(today bool) CalendarItemState { s.today = today; return s }

// Kind is U:calendar.rs:466-468.
func (s CalendarItemState) Kind() CalendarItemKind { return s.kind }

// IsActive is U:calendar.rs:470-472.
func (s CalendarItemState) IsActive() bool { return s.active }

// IsInRange is U:calendar.rs:474-476.
func (s CalendarItemState) IsInRange() bool { return s.inRange }

// IsMuted is U:calendar.rs:478-480.
func (s CalendarItemState) IsMuted() bool { return s.muted }

// IsDisabled is U:calendar.rs:482-484.
func (s CalendarItemState) IsDisabled() bool { return s.disabled }

// IsToday is U:calendar.rs:486-488.
func (s CalendarItemState) IsToday() bool { return s.today }

// CalendarItem is an unstyled, pre-wired calendar item passed to the
// item slot (U:calendar.rs:491-550). Its interactivity is the inner
// div's.
type CalendarItem struct {
	gpui.Component[*CalendarItem]

	base  *gpui.DivElement
	state CalendarItemState
}

// newCalendarItem is U:calendar.rs:501-508.
func newCalendarItem(id string, state CalendarItemState) *CalendarItem {
	// Gap 2b: the plain upstream div() is a block.
	base := TestSupport(gpui.Div().ID(id).Block())
	return gpui.BindComponent(&CalendarItem{base: base, state: state}, base)
}

// withLabel keeps the default visible label available to assistive
// technology even when a presentation assigns a role that does not
// derive a name from children (U:calendar.rs:509-513).
func (i *CalendarItem) withLabel(label string) *CalendarItem {
	i.base.AriaLabel(label)
	return i.Child(label)
}

// ItemState is U:calendar.rs:515-517.
func (i *CalendarItem) ItemState() CalendarItemState { return i.state }

// ClearChildren removes the default label so a styled facade can provide
// custom content (U:calendar.rs:519-523).
func (i *CalendarItem) ClearChildren() *CalendarItem {
	i.TakeChildren()
	return i
}

// RenderOnce implements gpui.RenderOnce (U:calendar.rs:546-550).
func (i *CalendarItem) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	style := i.Style()
	i.base.Extend(i.TakeChildren())
	return StyledExtOf(i.base).RefineStyle(&style).IntoElement()
}

// CalendarItemRenderer decorates an item (U:calendar.rs:552-553).
type CalendarItemRenderer = func(item *CalendarItem, state CalendarItemState, w *gpui.Window, app *gpui.App) gpui.AnyElement

// CalendarLabeler names an item from its kind and value
// (U:calendar.rs:554).
type CalendarLabeler = func(kind CalendarItemKind, value int32) string

// Calendar is the complete unstyled calendar structure and behavior
// (U:calendar.rs:556-569). Base owns navigation, view switching, grids,
// disabled/selection state and click handling; the UI only decorates the
// pre-wired item slot.
type Calendar struct {
	gpui.Styled[*Calendar]

	id             gpui.ElementID
	state          gpui.Entity[CalendarState]
	numberOfMonths int
	firstDayOfWeek time.Weekday
	item           CalendarItemRenderer
	label          CalendarLabeler
}

// NewCalendar is Calendar::new (U:calendar.rs:572-587).
func NewCalendar[I gpui.IntoElementID](id I, state gpui.Entity[CalendarState]) *Calendar {
	return gpui.BindStyled(&Calendar{
		id:             gpui.ElementIDOf(id),
		state:          state,
		numberOfMonths: 1,
		firstDayOfWeek: time.Sunday,
		item: func(item *CalendarItem, _ CalendarItemState, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
			return gpui.OwnRecipe(item)
		},
		label: func(kind CalendarItemKind, value int32) string {
			switch kind {
			case CalendarItemPrevious:
				return "‹"
			case CalendarItemNext:
				return "›"
			}
			return strconv.Itoa(int(value))
		},
	})
}

// NumberOfMonths is U:calendar.rs:588-591.
func (c *Calendar) NumberOfMonths(count int) *Calendar {
	c.numberOfMonths = max(count, 1)
	return c
}

// FirstDayOfWeek is U:calendar.rs:592-595.
func (c *Calendar) FirstDayOfWeek(day time.Weekday) *Calendar {
	c.firstDayOfWeek = day
	return c
}

// Item sets the item slot (U:calendar.rs:596-602).
func (c *Calendar) Item(render CalendarItemRenderer) *Calendar {
	c.item = render
	return c
}

// Label sets the labeler (U:calendar.rs:603-609).
func (c *Calendar) Label(label CalendarLabeler) *Calendar {
	c.label = label
	return c
}

// renderItem is U:calendar.rs:611-626.
func (c *Calendar) renderItem(id string, state CalendarItemState, value int32, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	label := c.label(state.Kind(), value)
	return c.item(newCalendarItem(id, state).withLabel(label), state, w, app)
}

// readCalendar snapshots the state for rendering.
func readCalendar(app *gpui.App, state gpui.Entity[CalendarState]) CalendarState {
	return state.Read(app, func(s *CalendarState, _ *gpui.App) CalendarState { return *s })
}

// RenderOnce implements gpui.RenderOnce (U:calendar.rs:634-829).
func (c *Calendar) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	count := c.numberOfMonths
	c.state.Update(app, func(s *CalendarState, cx *gpui.Context[CalendarState]) { s.SetNumberOfMonths(count, w, cx) })
	s := readCalendar(app, c.state)
	view := s.View()
	entity := c.state

	header := HFlex().ItemsCenter().JustifyBetween()
	{
		st := NewCalendarItemState(CalendarItemPrevious).Disabled(view.IsMonth() || (view.IsYear() && !s.HasPrevYearPage()))
		item := newCalendarItem("calendar-prev", st).withLabel(c.label(st.Kind(), 0))
		if !st.IsDisabled() {
			item.OnClick(func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
				entity.Update(app, func(s *CalendarState, cx *gpui.Context[CalendarState]) {
					if s.View().IsDay() {
						s.PrevMonth()
					} else {
						s.PrevYearPage()
					}
					cx.Notify()
				})
			})
		}
		header.Child(c.item(item, st, w, app))
	}
	if count == 1 {
		for _, toggle := range []struct {
			kind   CalendarItemKind
			value  int32
			active bool
			target CalendarView
		}{
			{CalendarItemMonthToggle, int32(s.CurrentMonth()), view.IsMonth(), CalendarViewMonth},
			{CalendarItemYearToggle, int32(s.CurrentYear()), view.IsYear(), CalendarViewYear},
		} {
			st := NewCalendarItemState(toggle.kind).Active(toggle.active)
			target := toggle.target
			item := newCalendarItem("calendar-"+toggle.kind.String(), st).withLabel(c.label(toggle.kind, toggle.value))
			item.OnClick(func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
				entity.Update(app, func(s *CalendarState, cx *gpui.Context[CalendarState]) {
					if s.View() == target {
						s.SetView(CalendarViewDay)
					} else {
						s.SetView(target)
					}
					cx.Notify()
				})
			})
			header.Child(c.item(item, st, w, app))
		}
	} else {
		for offset := 0; offset < count; offset++ {
			y, m := s.OffsetYearMonth(offset)
			title := StyledExtOf(gpui.Div().Block().TextSm()).FontMedium().
				Child(VFlex().ItemsCenter().
					Child(c.label(CalendarItemMonthToggle, int32(m))).
					Child(strconv.Itoa(y)))
			header.Child(title)
		}
	}
	{
		st := NewCalendarItemState(CalendarItemNext).Disabled(view.IsMonth() || (view.IsYear() && !s.HasNextYearPage()))
		item := newCalendarItem("calendar-next", st).withLabel(c.label(st.Kind(), 0))
		if !st.IsDisabled() {
			item.OnClick(func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
				entity.Update(app, func(s *CalendarState, cx *gpui.Context[CalendarState]) {
					if s.View().IsDay() {
						s.NextMonth()
					} else {
						s.NextYearPage()
					}
					cx.Notify()
				})
			})
		}
		header.Child(c.item(item, st, w, app))
	}

	var body *gpui.DivElement
	if columns, gap, ok := pickerGridLayout(view); ok {
		body = gpui.Div().Grid().GridCols(columns).GapX(gpui.Px(gap))
	} else {
		body = HFlex().JustifyAround()
	}
	switch {
	case view.IsDay():
		for offset := 0; offset < count; offset++ {
			year, monthNumber := s.OffsetYearMonth(offset)
			weeks := daysInMonth(year, monthNumber, c.firstDayOfWeek)
			month := VFlex()
			headerRow := HFlex()
			for weekday := int32(0); weekday < 7; weekday++ {
				st := NewCalendarItemState(CalendarItemWeekday).Muted(true).Disabled(true)
				headerRow.Child(c.renderItem(
					fmt.Sprintf("weekday-%d-%d", offset, weekday),
					st,
					(weekday+int32(c.firstDayOfWeek))%7,
					w, app,
				))
			}
			month.Child(headerRow)
			for weekIndex, week := range weeks {
				weekRow := HFlex()
				for _, date := range week {
					disabled := s.DisabledMatcherRef() != nil && s.DisabledMatcherRef().Matched(date)
					st := NewCalendarItemState(CalendarItemDay).
						Active(s.Date().IsActive(date)).
						InRange(s.Date().IsInRange(date)).
						Muted(date.Month() != monthNumber || disabled).
						Disabled(disabled).
						Today(date == s.Today())
					item := newCalendarItem(fmt.Sprintf("calendar-%s-%d-%d", date, offset, weekIndex), st).
						withLabel(c.label(st.Kind(), int32(date.Day())))
					item.base.AriaLabel(date.String())
					if !st.IsDisabled() {
						item.OnClick(func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
							entity.Update(app, func(s *CalendarState, cx *gpui.Context[CalendarState]) { s.ActivateDate(date, cx) })
						})
					}
					weekRow.Child(c.item(item, st, w, app))
				}
				month.Child(weekRow)
			}
			body.Child(month)
		}
	case view.IsMonth():
		current := s.CurrentMonth()
		for month := uint8(1); month <= 12; month++ {
			st := NewCalendarItemState(CalendarItemMonth).Active(month == current)
			item := newCalendarItem(fmt.Sprintf("calendar-month-%d", month), st).
				withLabel(c.label(st.Kind(), int32(month)))
			item.OnClick(func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
				entity.Update(app, func(s *CalendarState, cx *gpui.Context[CalendarState]) {
					s.SelectMonth(month)
					cx.Notify()
				})
			})
			body.Child(c.item(item, st, w, app))
		}
	default:
		current := s.CurrentYear()
		for _, year := range slices.Clone(s.YearsOnPage()) {
			st := NewCalendarItemState(CalendarItemYear).Active(year == current)
			item := newCalendarItem(fmt.Sprintf("calendar-year-%d", year), st).
				withLabel(c.label(st.Kind(), int32(year)))
			item.OnClick(func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
				entity.Update(app, func(s *CalendarState, cx *gpui.Context[CalendarState]) {
					s.SelectYear(year)
					cx.Notify()
				})
			})
			body.Child(c.item(item, st, w, app))
		}
	}

	root := VFlex().ID(c.id).TrackFocus(s.FocusHandle).Child(header).Child(body)
	style := c.Style()
	return StyledExtOf(root).RefineStyle(&style).IntoElement()
}

// daysInMonth is the weeks covering a month, starting on firstDay; the
// month wraps into the neighboring years (U:calendar.rs:831-851).
func daysInMonth(year int, month uint32, firstDay time.Weekday) [][]NaiveDate {
	total := int64(year)*12 + int64(month) - 1
	year = int(floorDiv64(total, 12))
	month = uint32(floorMod64(total, 12)) + 1
	first, _ := NaiveDateFromYMD(year, month, 1)
	var next NaiveDate
	if month == 12 {
		next, _ = NaiveDateFromYMD(year+1, 1, 1)
	} else {
		next, _ = NaiveDateFromYMD(year, month+1, 1)
	}
	offset := (int(first.Weekday()) + 7 - int(firstDay)) % 7
	start := first.AddDays(-offset)
	count := (next.daysSince(start) + 6) / 7 * 7
	weeks := make([][]NaiveDate, 0, count/7)
	for n := 0; n < count; n += 7 {
		week := make([]NaiveDate, 7)
		for i := range week {
			week[i] = start.AddDays(n + i)
		}
		weeks = append(weeks, week)
	}
	return weeks
}

// floorDiv64 and floorMod64 are i64::div_euclid / rem_euclid for a
// positive divisor.
func floorDiv64(a, b int64) int64 {
	q := a / b
	if a%b < 0 {
		q--
	}
	return q
}

func floorMod64(a, b int64) int64 {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}
