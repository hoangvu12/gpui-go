package base

// Port of U:time_field.rs: the segmented time editor.
//
// chrono's NaiveTime is NaiveTime below: the hour, minute and second of
// a time of day (time_field.rs reads no finer component). Its strftime
// formatting goes through base/strftime (ticket 15).

import (
	"fmt"
	"slices"
	"time"

	"github.com/hoangvu12/gpui-go/base/strftime"
	"github.com/hoangvu12/gpui-go/gpui"
)

// NaiveTime is a time of day without a time zone: the chrono NaiveTime
// subset time_field.rs uses (NaiveTime::MIN, from_hms_opt, Timelike's
// hour / minute / second / with_*, format).
type NaiveTime struct {
	hour, minute, second uint32
}

// NaiveTimeMin is NaiveTime::MIN, midnight.
var NaiveTimeMin = NaiveTime{}

// NaiveTimeFromHMS is NaiveTime::from_hms_opt: ok is false outside
// 0..=23, 0..=59, 0..=59.
func NaiveTimeFromHMS(hour, minute, second uint32) (NaiveTime, bool) {
	if hour > 23 || minute > 59 || second > 59 {
		return NaiveTime{}, false
	}
	return NaiveTime{hour, minute, second}, true
}

// Hour is Timelike::hour.
func (t NaiveTime) Hour() uint32 { return t.hour }

// Minute is Timelike::minute.
func (t NaiveTime) Minute() uint32 { return t.minute }

// Second is Timelike::second.
func (t NaiveTime) Second() uint32 { return t.second }

// withHour, withMinute and withSecond are Timelike::with_* (None out of
// range).
func (t NaiveTime) withHour(h uint32) (NaiveTime, bool) {
	return NaiveTimeFromHMS(h, t.minute, t.second)
}
func (t NaiveTime) withMinute(m uint32) (NaiveTime, bool) {
	return NaiveTimeFromHMS(t.hour, m, t.second)
}
func (t NaiveTime) withSecond(s uint32) (NaiveTime, bool) {
	return NaiveTimeFromHMS(t.hour, t.minute, s)
}

// Format is `NaiveTime::format(layout).to_string()` (chrono strftime,
// base/strftime). A malformed layout yields ErrBadFormat where chrono's
// to_string would panic.
func (t NaiveTime) Format(layout string) (string, error) {
	return strftime.Format(time.Date(1970, 1, 1, int(t.hour), int(t.minute), int(t.second), 0, time.UTC), layout)
}

// String is NaiveTime's Display, `%H:%M:%S` with no fraction.
func (t NaiveTime) String() string { return fmt.Sprintf("%02d:%02d:%02d", t.hour, t.minute, t.second) }

// timeFieldContext is the TimeField key context (U:time_field.rs:18).
const timeFieldContext = "TimeField"

// timeFieldInit binds the time field keys (U:time_field.rs:20-31).
//
// browser-core 73: the tab/shift-tab rows stay — Chrome's date/time
// input keeps Tab walking its segments: every subfield is a focusable
// shadow element (DateTimeFieldElement::SupportsFocus,
// date_time_field_element.cc:232-238), so the sequential focus order
// the default Tab handler follows runs segment to segment and out of
// the control after the last one — exactly onNextColumn/onPrevColumn's
// walk, whose boundary propagates the key so the core's Tab default
// action moves focus out (the last segment's rule). The left/right
// rows are the field's own segment moves (DefaultKeyboardEventHandler,
// date_time_field_element.cc:77-110).
func timeFieldInit(app *gpui.App) {
	ctx := timeFieldContext
	app.BindKeys(
		gpui.NewKeyBinding("up", IncrementAction.Box(Increment{}), &ctx),
		gpui.NewKeyBinding("down", DecrementAction.Box(Decrement{}), &ctx),
		gpui.NewKeyBinding("left", SelectLeftAction.Box(SelectLeft{}), &ctx),
		gpui.NewKeyBinding("right", SelectRightAction.Box(SelectRight{}), &ctx),
		gpui.NewKeyBinding("tab", SelectNextColumnAction.Box(SelectNextColumn{}), &ctx),
		gpui.NewKeyBinding("shift-tab", SelectPrevColumnAction.Box(SelectPrevColumn{}), &ctx),
		gpui.NewKeyBinding("backspace", DeleteAction.Box(Delete{}), &ctx),
		gpui.NewKeyBinding("delete", DeleteAction.Box(Delete{}), &ctx),
	)
}

// TimePrecision is the smallest unit a TimeField edits
// (U:time_field.rs:33-41). The zero value is Minute, Rust's Default.
type TimePrecision uint8

const (
	// TimePrecisionMinute is hours and minutes, e.g. 09:30.
	TimePrecisionMinute TimePrecision = iota
	// TimePrecisionSecond is hours, minutes and seconds, e.g. 09:30:15.
	TimePrecisionSecond
)

// Format is the chrono format that displays a time at this precision
// and cycle (U:time_field.rs:44-52).
func (p TimePrecision) Format(hourCycle HourCycle) string {
	switch {
	case p == TimePrecisionMinute && hourCycle == HourCycleH23:
		return "%H:%M"
	case p == TimePrecisionSecond && hourCycle == HourCycleH23:
		return "%H:%M:%S"
	case p == TimePrecisionMinute:
		return "%I:%M %p"
	default:
		return "%I:%M:%S %p"
	}
}

// Truncate drops the components finer than this precision
// (U:time_field.rs:54-61).
func (p TimePrecision) Truncate(t NaiveTime) NaiveTime {
	second := uint32(0)
	if p == TimePrecisionSecond {
		second = t.second
	}
	if v, ok := NaiveTimeFromHMS(t.hour, t.minute, second); ok {
		return v
	}
	return t
}

// HourCycle is how a TimeField counts the hours of a day, named after
// the Unicode hourCycle values (U:time_field.rs:64-74). The zero value
// is H23, Rust's Default.
type HourCycle uint8

const (
	// HourCycleH23 is a 24-hour clock from 00 to 23.
	HourCycleH23 HourCycle = iota
	// HourCycleH12 is a 12-hour clock from 12 to 11, with an AM/PM
	// segment.
	HourCycleH12
)

// TimeSegment is one editable component of a TimeField
// (U:time_field.rs:76-84).
type TimeSegment uint8

const (
	// TimeSegmentHour is the hour.
	TimeSegmentHour TimeSegment = iota
	// TimeSegmentMinute is the minute.
	TimeSegmentMinute
	// TimeSegmentSecond is the second.
	TimeSegmentSecond
	// TimeSegmentPeriod is AM or PM, present with HourCycleH12.
	TimeSegmentPeriod
)

// name is U:time_field.rs:86-95.
func (s TimeSegment) name() string {
	switch s {
	case TimeSegmentHour:
		return "hour"
	case TimeSegmentMinute:
		return "minute"
	case TimeSegmentSecond:
		return "second"
	default:
		return "period"
	}
}

// TimeFieldEvent is a semantic notification from a TimeFieldState
// (U:time_field.rs:97-102): keyboard editing changed the time
// (TimeFieldEvent::Change).
type TimeFieldEvent struct {
	Change NaiveTime
}

// TimeFieldEvents is the TimeFieldState event descriptor
// (U:time_field.rs:447).
var TimeFieldEvents = gpui.DefineEvent[TimeFieldState, TimeFieldEvent]()

// segmentEditor is the pure editing rules of a TimeFieldState, kept apart
// from focus so they can be tested without a window
// (U:time_field.rs:104-114).
type segmentEditor struct {
	time      NaiveTime
	precision TimePrecision
	hourCycle HourCycle
	segment   TimeSegment
	// pendingDigit is the first digit typed into the selected segment,
	// awaiting a second.
	pendingDigit    uint32
	hasPendingDigit bool
}

// newSegmentEditor is U:time_field.rs:117-125.
func newSegmentEditor() segmentEditor {
	return segmentEditor{time: NaiveTimeMin, segment: TimeSegmentHour}
}

// segments are the segments shown, in reading order
// (U:time_field.rs:127-136).
func (e *segmentEditor) segments() []TimeSegment {
	switch {
	case e.precision == TimePrecisionMinute && e.hourCycle == HourCycleH23:
		return []TimeSegment{TimeSegmentHour, TimeSegmentMinute}
	case e.precision == TimePrecisionSecond && e.hourCycle == HourCycleH23:
		return []TimeSegment{TimeSegmentHour, TimeSegmentMinute, TimeSegmentSecond}
	case e.precision == TimePrecisionMinute:
		return []TimeSegment{TimeSegmentHour, TimeSegmentMinute, TimeSegmentPeriod}
	default:
		return []TimeSegment{TimeSegmentHour, TimeSegmentMinute, TimeSegmentSecond, TimeSegmentPeriod}
	}
}

// bounds is the inclusive range a segment's displayed value takes
// (U:time_field.rs:138-146).
func (e *segmentEditor) bounds(segment TimeSegment) (uint32, uint32) {
	switch {
	case segment == TimeSegmentHour && e.hourCycle == HourCycleH23:
		return 0, 23
	case segment == TimeSegmentHour:
		return 1, 12
	case segment == TimeSegmentPeriod:
		return 0, 1
	default:
		return 0, 59
	}
}

// value is the displayed value of a segment: the hour follows the cycle,
// and the period is 0 for AM and 1 for PM (U:time_field.rs:148-159).
func (e *segmentEditor) value(segment TimeSegment) uint32 {
	hour := e.time.hour
	switch {
	case segment == TimeSegmentHour && e.hourCycle == HourCycleH23:
		return hour
	case segment == TimeSegmentHour:
		return (hour+11)%12 + 1
	case segment == TimeSegmentMinute:
		return e.time.minute
	case segment == TimeSegmentSecond:
		return e.time.second
	default:
		return hour / 12
	}
}

// label is U:time_field.rs:161-167.
func (e *segmentEditor) label(segment TimeSegment) string {
	if segment == TimeSegmentPeriod {
		if e.value(segment) == 0 {
			return "AM"
		}
		return "PM"
	}
	return fmt.Sprintf("%02d", e.value(segment))
}

// withValue is U:time_field.rs:169-181.
func (e *segmentEditor) withValue(segment TimeSegment, value uint32) NaiveTime {
	t := e.time
	pmOffset := t.hour / 12 * 12
	var next NaiveTime
	var ok bool
	switch {
	case segment == TimeSegmentHour && e.hourCycle == HourCycleH23:
		next, ok = t.withHour(value)
	case segment == TimeSegmentHour:
		// 12 AM is midnight and 12 PM is noon.
		next, ok = t.withHour(value%12 + pmOffset)
	case segment == TimeSegmentMinute:
		next, ok = t.withMinute(value)
	case segment == TimeSegmentSecond:
		next, ok = t.withSecond(value)
	default:
		next, ok = t.withHour(t.hour%12 + value*12)
	}
	if !ok {
		return t
	}
	return next
}

// setPrecision is U:time_field.rs:183-187.
func (e *segmentEditor) setPrecision(precision TimePrecision) {
	e.precision = precision
	e.time = precision.Truncate(e.time)
	e.resetSegment()
}

// setHourCycle is U:time_field.rs:189-192.
func (e *segmentEditor) setHourCycle(hourCycle HourCycle) {
	e.hourCycle = hourCycle
	e.resetSegment()
}

// resetSegment is U:time_field.rs:194-199.
func (e *segmentEditor) resetSegment() {
	if !slices.Contains(e.segments(), e.segment) {
		e.segment = TimeSegmentHour
	}
	e.hasPendingDigit = false
}

// setTime is U:time_field.rs:201-210.
func (e *segmentEditor) setTime(t NaiveTime) bool {
	t = e.precision.Truncate(t)
	if e.time == t {
		// Keep a half-typed segment when the owner echoes the same value
		// back.
		return false
	}
	e.time = t
	e.hasPendingDigit = false
	return true
}

// selectSegment is U:time_field.rs:212-219.
func (e *segmentEditor) selectSegment(segment TimeSegment) bool {
	if !slices.Contains(e.segments(), segment) {
		return false
	}
	e.segment = segment
	e.hasPendingDigit = false
	return true
}

// moveSegment moves the selected segment by offset; it returns false at
// either end (U:time_field.rs:221-235).
func (e *segmentEditor) moveSegment(offset int) bool {
	segments := e.segments()
	ix := slices.Index(segments, e.segment)
	if ix < 0 {
		ix = 0
	}
	next := ix + offset
	if next < 0 || next >= len(segments) {
		return false
	}
	return e.selectSegment(segments[next])
}

// step steps the selected segment by delta, wrapping within the segment
// (U:time_field.rs:237-244).
func (e *segmentEditor) step(delta int32) bool {
	e.hasPendingDigit = false
	min, max := e.bounds(e.segment)
	span := int32(max - min + 1)
	v := (int32(e.value(e.segment)) - int32(min) + delta) % span
	if v < 0 { // rem_euclid
		v += span
	}
	return e.replaceSegment(uint32(v) + min)
}

// inputDigit is U:time_field.rs:246-266.
func (e *segmentEditor) inputDigit(digit uint32) bool {
	if e.segment == TimeSegmentPeriod {
		return false
	}
	min, max := e.bounds(e.segment)
	first, had := e.pendingDigit, e.hasPendingDigit
	e.hasPendingDigit = false
	var value uint32
	var complete bool
	if pair := first*10 + digit; had && pair >= min && pair <= max {
		value, complete = pair, true
	} else {
		// The two digits cannot form a valid value, so the new digit
		// starts over.
		value, complete = digit, digit*10 > max
	}
	if !complete {
		e.pendingDigit, e.hasPendingDigit = digit, true
	}
	changed := e.replaceSegment(value)
	if complete {
		e.moveSegment(1)
	}
	return changed
}

// inputPeriod types a or p into the period segment
// (U:time_field.rs:268-274).
func (e *segmentEditor) inputPeriod(pm bool) bool {
	if e.segment != TimeSegmentPeriod {
		return false
	}
	v := uint32(0)
	if pm {
		v = 1
	}
	return e.replaceSegment(v)
}

// clearSegment resets the selected segment to its first value: zero, or
// 12 / AM on a 12-hour clock (U:time_field.rs:276-281).
func (e *segmentEditor) clearSegment() bool {
	e.hasPendingDigit = false
	return e.replaceSegment(0)
}

// replaceSegment is U:time_field.rs:283-288.
func (e *segmentEditor) replaceSegment(value uint32) bool {
	t := e.withValue(e.segment, value)
	changed := t != e.time
	e.time = t
	return changed
}

// TimeFieldState is the retained editing behavior for a segmented time
// field (U:time_field.rs:291-305).
//
// The field is one Tab stop. Inside it, one segment is selected at a
// time: Up/Down step the selected segment and wrap within it (no carry
// into the next unit), Left/Right and Tab/Shift-Tab move between
// segments, digits are typed with a two-digit buffer that advances to the
// next segment once no further digit could fit, a/p set the AM/PM
// segment, and Backspace/Delete reset the segment. The owner supplies the
// value with SetTime, which does not emit; user edits emit
// TimeFieldEvent.
type TimeFieldState struct {
	focusHandle gpui.FocusHandle
	editor      segmentEditor
}

// NewTimeFieldState is TimeFieldState::new (U:time_field.rs:308-313).
func NewTimeFieldState(_ *gpui.Window, cx *gpui.Context[TimeFieldState]) TimeFieldState {
	return TimeFieldState{
		focusHandle: cx.App().FocusHandle().TabStop(true),
		editor:      newSegmentEditor(),
	}
}

// Precision sets the precision, default TimePrecisionMinute
// (U:time_field.rs:315-319).
func (s TimeFieldState) Precision(precision TimePrecision) TimeFieldState {
	s.editor.setPrecision(precision)
	return s
}

// SetPrecision is U:time_field.rs:321-331.
func (s *TimeFieldState) SetPrecision(precision TimePrecision, _ *gpui.Window, cx *gpui.Context[TimeFieldState]) {
	if s.editor.precision != precision {
		s.editor.setPrecision(precision)
		cx.Notify()
	}
}

// HourCycle sets the hour cycle, default HourCycleH23
// (U:time_field.rs:333-337).
func (s TimeFieldState) HourCycle(hourCycle HourCycle) TimeFieldState {
	s.editor.setHourCycle(hourCycle)
	return s
}

// SetHourCycle is U:time_field.rs:339-349.
func (s *TimeFieldState) SetHourCycle(hourCycle HourCycle, _ *gpui.Window, cx *gpui.Context[TimeFieldState]) {
	if s.editor.hourCycle != hourCycle {
		s.editor.setHourCycle(hourCycle)
		cx.Notify()
	}
}

// Time is U:time_field.rs:351-353.
func (s *TimeFieldState) Time() NaiveTime { return s.editor.time }

// SetTime replaces the time without emitting TimeFieldEvent
// (U:time_field.rs:355-360).
func (s *TimeFieldState) SetTime(t NaiveTime, _ *gpui.Window, cx *gpui.Context[TimeFieldState]) {
	if s.editor.setTime(t) {
		cx.Notify()
	}
}

// SelectedSegment is the segment keyboard editing applies to
// (U:time_field.rs:362-365).
func (s *TimeFieldState) SelectedSegment() TimeSegment { return s.editor.segment }

// Focus is U:time_field.rs:367-369.
func (s *TimeFieldState) Focus(w *gpui.Window, _ *gpui.App) { s.focusHandle.Focus(w) }

// FocusHandle implements Focusable (U:time_field.rs:442-446).
func (s *TimeFieldState) FocusHandle(*gpui.App) gpui.FocusHandle { return s.focusHandle }

// edit is U:time_field.rs:371-376.
func (s *TimeFieldState) edit(cx *gpui.Context[TimeFieldState], edit func(*segmentEditor) bool) {
	if edit(&s.editor) {
		TimeFieldEvents.Emit(cx, TimeFieldEvent{Change: s.editor.time})
	}
	cx.Notify()
}

// onIncrement is U:time_field.rs:378-380.
func (s *TimeFieldState) onIncrement(_ *Increment, _ *gpui.Window, cx *gpui.Context[TimeFieldState]) {
	s.edit(cx, func(e *segmentEditor) bool { return e.step(1) })
}

// onDecrement is U:time_field.rs:382-384.
func (s *TimeFieldState) onDecrement(_ *Decrement, _ *gpui.Window, cx *gpui.Context[TimeFieldState]) {
	s.edit(cx, func(e *segmentEditor) bool { return e.step(-1) })
}

// onDelete is U:time_field.rs:386-388.
func (s *TimeFieldState) onDelete(_ *Delete, _ *gpui.Window, cx *gpui.Context[TimeFieldState]) {
	s.edit(cx, (*segmentEditor).clearSegment)
}

// onSelectLeft is U:time_field.rs:390-393.
func (s *TimeFieldState) onSelectLeft(_ *SelectLeft, _ *gpui.Window, cx *gpui.Context[TimeFieldState]) {
	s.editor.moveSegment(-1)
	cx.Notify()
}

// onSelectRight is U:time_field.rs:395-398.
func (s *TimeFieldState) onSelectRight(_ *SelectRight, _ *gpui.Window, cx *gpui.Context[TimeFieldState]) {
	s.editor.moveSegment(1)
	cx.Notify()
}

// onNextColumn walks the segments first and leaves the field only from
// the last one (U:time_field.rs:400-407).
func (s *TimeFieldState) onNextColumn(_ *SelectNextColumn, _ *gpui.Window, cx *gpui.Context[TimeFieldState]) {
	if s.editor.moveSegment(1) {
		cx.Notify()
	} else {
		cx.Propagate()
	}
}

// onPrevColumn is U:time_field.rs:409-415.
func (s *TimeFieldState) onPrevColumn(_ *SelectPrevColumn, _ *gpui.Window, cx *gpui.Context[TimeFieldState]) {
	if s.editor.moveSegment(-1) {
		cx.Notify()
	} else {
		cx.Propagate()
	}
}

// onKeyDown is U:time_field.rs:417-434.
func (s *TimeFieldState) onKeyDown(event *gpui.KeyDownEvent, w *gpui.Window, cx *gpui.Context[TimeFieldState]) {
	keystroke := event.Keystroke
	runes := []rune(keystroke.Key)
	if keystroke.Modifiers.Modified() || len(runes) != 1 {
		return
	}
	var edit func(*segmentEditor) bool
	switch ch := runes[0]; {
	case ch >= '0' && ch <= '9':
		edit = func(e *segmentEditor) bool { return e.inputDigit(uint32(ch - '0')) }
	case ch == 'a' || ch == 'A':
		edit = func(e *segmentEditor) bool { return e.inputPeriod(false) }
	case ch == 'p' || ch == 'P':
		edit = func(e *segmentEditor) bool { return e.inputPeriod(true) }
	default:
		return
	}
	w.PreventDefault()
	cx.StopPropagation()
	s.edit(cx, edit)
}

// selectSegment is U:time_field.rs:436-439.
func (s *TimeFieldState) selectSegment(segment TimeSegment, cx *gpui.Context[TimeFieldState]) {
	s.editor.selectSegment(segment)
	cx.Notify()
}

// Render renders nothing (U:time_field.rs:448-452).
func (s *TimeFieldState) Render(*gpui.Window, *gpui.Context[TimeFieldState]) gpui.AnyElement {
	return gpui.Empty()
}

// TimeFieldSegmentState is the state exposed to a TimeField segment slot
// for decoration (U:time_field.rs:454-483).
type TimeFieldSegmentState struct {
	segment  TimeSegment
	value    uint32
	selected bool
	disabled bool
}

// Segment is U:time_field.rs:464-466.
func (s TimeFieldSegmentState) Segment() TimeSegment { return s.segment }

// Value is the displayed value: the hour on the configured clock, the
// minute or second, or 0 for AM and 1 for PM (U:time_field.rs:468-472).
func (s TimeFieldSegmentState) Value() uint32 { return s.value }

// IsSelected reports whether keyboard editing applies to this segment:
// it is the selected segment and the field has focus
// (U:time_field.rs:474-478).
func (s TimeFieldSegmentState) IsSelected() bool { return s.selected }

// IsDisabled is U:time_field.rs:480-482.
func (s TimeFieldSegmentState) IsDisabled() bool { return s.disabled }

// TimeFieldSegmentRenderer decorates a segment (U:time_field.rs:485-486).
type TimeFieldSegmentRenderer = func(segment *TimeFieldSegment, state TimeFieldSegmentState, w *gpui.Window, app *gpui.App) gpui.AnyElement

// TimeFieldSegment is an unstyled, pre-wired segment passed to the
// TimeField segment slot (U:time_field.rs:488-524). Its interactivity
// is the inner div's.
type TimeFieldSegment struct {
	gpui.Component[*TimeFieldSegment]

	base *gpui.DivElement
}

func newTimeFieldSegment(segment TimeSegment) *TimeFieldSegment {
	// Gap 2b: the plain upstream div() is a block.
	base := TestSupport(gpui.Div().ID(segment.name()).Block())
	return gpui.BindComponent(&TimeFieldSegment{base: base}, base)
}

// ClearChildren removes the default label so a styled facade can provide
// custom content (U:time_field.rs:497-501).
func (s *TimeFieldSegment) ClearChildren() *TimeFieldSegment {
	s.TakeChildren()
	return s
}

// RenderOnce implements gpui.RenderOnce (U:time_field.rs:520-524).
func (s *TimeFieldSegment) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	style := s.Style()
	s.base.Extend(s.TakeChildren())
	return StyledExtOf(s.base).RefineStyle(&style).IntoElement()
}

// TimeField is the unstyled segmented time editor
// (U:time_field.rs:526-539). Base owns focus, the keyboard model and
// pointer segment selection; the presentation lays out and decorates the
// field with Styled and each segment through RenderSegment. A `:`
// separates the numeric segments; the AM/PM segment follows without one.
type TimeField struct {
	gpui.Styled[*TimeField]

	id       gpui.ElementID
	state    gpui.Entity[TimeFieldState]
	disabled bool
	segment  TimeFieldSegmentRenderer
}

// NewTimeField is TimeField::new (U:time_field.rs:542-550).
func NewTimeField[I gpui.IntoElementID](id I, state gpui.Entity[TimeFieldState]) *TimeField {
	return gpui.BindStyled(&TimeField{
		id:    gpui.ElementIDOf(id),
		state: state,
		segment: func(segment *TimeFieldSegment, _ TimeFieldSegmentState, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
			return gpui.OwnRecipe(segment)
		},
	})
}

// Disabled is U:time_field.rs:552-555.
func (f *TimeField) Disabled(disabled bool) *TimeField {
	f.disabled = disabled
	return f
}

// RenderSegment decorates each segment; the segment already carries its
// label, two digits or AM/PM (U:time_field.rs:557-566).
func (f *TimeField) RenderSegment(render TimeFieldSegmentRenderer) *TimeField {
	f.segment = render
	return f
}

// RenderOnce implements gpui.RenderOnce (U:time_field.rs:575-632).
func (f *TimeField) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	disabled := f.disabled
	read := f.state.Read(app, func(s *TimeFieldState, _ *gpui.App) TimeFieldState { return *s })
	focusHandle, editor := read.focusHandle, read.editor
	focused := focusHandle.IsFocused(w)
	value, err := editor.time.Format(editor.precision.Format(editor.hourCycle))
	if err != nil {
		panic("base: TimeField: " + err.Error())
	}

	// Gap 2b: the plain upstream div() is a block.
	root := TestSupport(gpui.Div().ID(f.id).Block()).
		Role(gpui.RoleTimeInput).
		AriaValue(value).
		TrackFocus(focusHandle.TabStop(!disabled))
	if !disabled {
		state := f.state
		root.KeyContext(timeFieldContext).
			OnAction(IncrementAction, w.ListenerFor(state, (*TimeFieldState).onIncrement)).
			OnAction(DecrementAction, w.ListenerFor(state, (*TimeFieldState).onDecrement)).
			OnAction(DeleteAction, w.ListenerFor(state, (*TimeFieldState).onDelete)).
			OnAction(SelectLeftAction, w.ListenerFor(state, (*TimeFieldState).onSelectLeft)).
			OnAction(SelectRightAction, w.ListenerFor(state, (*TimeFieldState).onSelectRight)).
			OnAction(SelectNextColumnAction, w.ListenerFor(state, (*TimeFieldState).onNextColumn)).
			OnAction(SelectPrevColumnAction, w.ListenerFor(state, (*TimeFieldState).onPrevColumn)).
			OnKeyDown(w.ListenerFor(state, (*TimeFieldState).onKeyDown))
	}

	for ix, segment := range editor.segments() {
		if ix > 0 && segment != TimeSegmentPeriod {
			root.Child(gpui.Div().Block().Child(":"))
		}
		segmentState := TimeFieldSegmentState{
			segment:  segment,
			value:    editor.value(segment),
			selected: focused && segment == editor.segment,
			disabled: disabled,
		}
		item := newTimeFieldSegment(segment).Child(editor.label(segment))
		if !disabled {
			state := f.state
			item.OnMouseDown(gpui.MouseButtonLeft, func(_ *gpui.MouseDownEvent, w *gpui.Window, app *gpui.App) {
				focusHandle.Focus(w)
				state.Update(app, func(s *TimeFieldState, cx *gpui.Context[TimeFieldState]) { s.selectSegment(segment, cx) })
			})
		}
		root.Child(f.segment(item, segmentState, w, app))
	}

	style := f.Style()
	return StyledExtOf(root).RefineStyle(&style).IntoElement()
}
