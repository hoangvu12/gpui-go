package base

// An unstyled slider: the SliderState entity plus the Slider behavior
// root, SliderTrack, SliderIndicator and SliderThumb parts (U:slider.rs).
//
// Go adaptations:
//   - SliderValue's two variants (Single(f32), Range(f32, f32)) are one
//     comparable struct built by SliderValueSingle / SliderValueRange (the
//     From<f32>, From<(f32, f32)> and From<Range<f32>> conversions,
//     U:slider.rs:43-59).
//   - SliderEvent's variants (Change, Release) are a kind plus the value;
//     SliderEvents is the `impl EventEmitter<SliderEvent> for SliderState`
//     descriptor (U:slider.rs:774).
//   - SliderState's by-value builders (min, max, step, scale,
//     default_value) are value-receiver methods returning the modified
//     state; the entity is created by the application
//     (gpui.NewEntity(..., func(s *SliderState, _) { *s = NewSliderState().Max(200) })).
//   - Range<f32> percentage is a (start, end) pair.
//   - window.listener_for is gpui.Window.ListenerFor (R:window.rs:6868).
//   - The drag payloads DragThumb / DragSlider are unexported values whose
//     drag preview is an empty view (U:slider.rs:393-409).
//
// Display (map gap 2b): Slider, SliderTrack, SliderIndicator and
// SliderThumb are plain divs upstream (U:slider.rs:429, 537, 648, 701),
// display Block under crates.io gpui 0.2.2 (the pin
// defaults to Flex, R:style.rs:1024-1027), so each such base is built
// with Block() before the caller's style refines over it: the upstream
// default layout holds and a caller's Flex() still wins.

import (
	"fmt"
	"math"

	"github.com/hoangvu12/gpui-go/gpui"
)

// SliderEventKind selects a SliderEvent variant (U:slider.rs:14-20).
type SliderEventKind uint8

const (
	// SliderEventChange is emitted continuously while the user changes
	// the value.
	SliderEventChange SliderEventKind = iota
	// SliderEventRelease is emitted once when the user releases the
	// slider after a drag or click.
	SliderEventRelease
)

// SliderEvent is an event emitted by SliderState (U:slider.rs:14-20).
type SliderEvent struct {
	Kind  SliderEventKind
	Value SliderValue
}

// SliderEvents is the SliderState event descriptor
// (U:slider.rs:774 impl EventEmitter<SliderEvent> for SliderState).
var SliderEvents = gpui.DefineEvent[SliderState, SliderEvent]()

// SliderValue is a single value or a range of values; the zero value is
// Single(0), Rust's Default (U:slider.rs:22-32, 61-65).
type SliderValue struct {
	isRange    bool
	start, end float32
}

// SliderValueSingle is SliderValue::Single (From<f32>, U:slider.rs:43-47).
func SliderValueSingle(value float32) SliderValue {
	return SliderValue{start: value, end: value}
}

// SliderValueRange is SliderValue::Range (From<(f32, f32)> and
// From<Range<f32>>, U:slider.rs:49-59).
func SliderValueRange(start, end float32) SliderValue {
	return SliderValue{isRange: true, start: start, end: end}
}

// String renders the value (U:slider.rs:34-41 Display).
func (v SliderValue) String() string {
	if v.isRange {
		return fmt.Sprintf("%v..%v", v.start, v.end)
	}
	return fmt.Sprint(v.start)
}

// Clamp clamps the value to [min, max] (U:slider.rs:68-76).
func (v SliderValue) Clamp(min, max float32) SliderValue {
	if v.isRange {
		return SliderValueRange(clamp32(v.start, min, max), clamp32(v.end, min, max))
	}
	return SliderValueSingle(clamp32(v.start, min, max))
}

// IsSingle reports the Single variant (U:slider.rs:78-82).
func (v SliderValue) IsSingle() bool { return !v.isRange }

// IsRange reports the Range variant (U:slider.rs:84-88).
func (v SliderValue) IsRange() bool { return v.isRange }

// Start returns the start value (the value itself when single,
// U:slider.rs:90-96).
func (v SliderValue) Start() float32 { return v.start }

// End returns the end value (the value itself when single,
// U:slider.rs:98-104).
func (v SliderValue) End() float32 { return v.end }

// setStart is set_start (U:slider.rs:106-112).
func (v *SliderValue) setStart(value float32) {
	if v.isRange {
		*v = SliderValueRange(min(value, v.end), v.end)
		return
	}
	*v = SliderValueSingle(value)
}

// setEnd is set_end (U:slider.rs:114-120).
func (v *SliderValue) setEnd(value float32) {
	if v.isRange {
		*v = SliderValueRange(v.start, max(value, v.start))
		return
	}
	*v = SliderValueSingle(value)
}

// SliderScale is the scale mode of the slider (U:slider.rs:123-155).
type SliderScale uint8

const (
	// SliderScaleLinear changes values uniformly across the range (the
	// default).
	SliderScaleLinear SliderScale = iota
	// SliderScaleLogarithmic spaces values exponentially, for volume,
	// frequency or zoom: with min 1 and max 1000, a third of the way is
	// ~10 and two thirds ~100.
	SliderScaleLogarithmic
)

// IsLinear reports the Linear scale (U:slider.rs:158-161).
func (s SliderScale) IsLinear() bool { return s == SliderScaleLinear }

// IsLogarithmic reports the Logarithmic scale (U:slider.rs:163-166).
func (s SliderScale) IsLogarithmic() bool { return s == SliderScaleLogarithmic }

// SliderState is the state of a Slider (U:slider.rs:169-183).
type SliderState struct {
	min, max, step float32
	value          SliderValue
	// percentage is [start, end]; in single mode only end is used.
	percentageStart, percentageEnd float32
	// bounds are the indicator's bounds after prepaint.
	bounds   gpui.Bounds
	scale    SliderScale
	dragging bool
}

// NewSliderState returns min 0, max 100, step 1, value Single(0),
// linear (U:slider.rs:186-198).
func NewSliderState() SliderState {
	return SliderState{min: 0, max: 100, step: 1}
}

// Min sets the minimum value, default 0 (U:slider.rs:200-215). It panics
// for a logarithmic scale unless 0 < min < max.
func (s SliderState) Min(min float32) SliderState {
	if s.scale.IsLogarithmic() {
		if !(min > 0) {
			panic("`min` must be greater than 0 for SliderScale::Logarithmic")
		}
		if !(min < s.max) {
			panic("`min` must be less than `max` for Logarithmic scale")
		}
	}
	s.min = min
	s.updateThumbPos()
	return s
}

// Max sets the maximum value, default 100 (U:slider.rs:217-228).
func (s SliderState) Max(max float32) SliderState {
	if s.scale.IsLogarithmic() && !(max > s.min) {
		panic("`max` must be greater than `min` for Logarithmic scale")
	}
	s.max = max
	s.updateThumbPos()
	return s
}

// Step sets the step, default 1 (U:slider.rs:230-234).
func (s SliderState) Step(step float32) SliderState {
	s.step = step
	return s
}

// Scale sets the scale, default linear (U:slider.rs:236-251).
func (s SliderState) Scale(scale SliderScale) SliderState {
	if scale.IsLogarithmic() {
		if !(s.min > 0) {
			panic("`min` must be greater than 0 for Logarithmic scale")
		}
		if !(s.max > s.min) {
			panic("`max` must be greater than `min` for Logarithmic scale")
		}
	}
	s.scale = scale
	s.updateThumbPos()
	return s
}

// DefaultValue sets the initial value, default Single(0)
// (U:slider.rs:253-258).
func (s SliderState) DefaultValue(value SliderValue) SliderState {
	s.value = value
	s.updateThumbPos()
	return s
}

// SetValue sets the value and notifies (U:slider.rs:260-270).
func (s *SliderState) SetValue(value SliderValue, _ *gpui.Window, cx *gpui.Context[SliderState]) {
	s.value = value
	s.updateThumbPos()
	cx.Notify()
}

// Value returns the value (U:slider.rs:272-275).
func (s *SliderState) Value() SliderValue { return s.value }

// MinValue returns the minimum (U:slider.rs:277-280).
func (s *SliderState) MinValue() float32 { return s.min }

// MaxValue returns the maximum (U:slider.rs:282-285).
func (s *SliderState) MaxValue() float32 { return s.max }

// StepValue returns the step (U:slider.rs:287-290).
func (s *SliderState) StepValue() float32 { return s.step }

// percentageToValue maps [0, 1] to [min, max] by the scale
// (U:slider.rs:292-305).
func (s *SliderState) percentageToValue(percentage float32) float32 {
	if s.scale.IsLogarithmic() {
		base := float64(s.max / s.min)
		return clamp32(float32(math.Pow(base, float64(percentage)))*s.min, s.min, s.max)
	}
	return s.min + (s.max-s.min)*percentage
}

// valueToPercentage maps [min, max] to [0, 1] by the scale
// (U:slider.rs:307-324).
func (s *SliderState) valueToPercentage(value float32) float32 {
	if s.scale.IsLogarithmic() {
		base := float64(s.max / s.min)
		return clamp32(float32(math.Log(float64(value/s.min))/math.Log(base)), 0, 1)
	}
	r := s.max - s.min
	if r <= 0 {
		return 0
	}
	return (value - s.min) / r
}

// updateThumbPos is update_thumb_pos (U:slider.rs:326-339).
func (s *SliderState) updateThumbPos() {
	if s.value.isRange {
		s.percentageStart = s.valueToPercentage(clamp32(s.value.start, s.min, s.max))
		s.percentageEnd = s.valueToPercentage(clamp32(s.value.end, s.min, s.max))
		return
	}
	s.percentageStart = 0
	s.percentageEnd = s.valueToPercentage(clamp32(s.value.start, s.min, s.max))
}

// UpdateValueByPosition updates the value from a pointer position along
// axis, moving the start thumb when isStart (U:slider.rs:341-381; public
// but doc(hidden) upstream).
func (s *SliderState) UpdateValueByPosition(axis gpui.Axis, position gpui.Point, isStart bool, _ *gpui.Window, cx *gpui.Context[SliderState]) {
	s.dragging = true
	bounds := s.bounds
	step := s.step

	var innerPos float32
	if AxisIsHorizontal(axis) {
		innerPos = position.X - bounds.Left()
	} else {
		innerPos = bounds.Bottom() - position.Y
	}
	totalSize := bounds.Size.Along(axis)
	percentage := clamp32(innerPos, 0, totalSize) / totalSize
	if isStart {
		percentage = clamp32(percentage, 0, s.percentageEnd)
	} else {
		percentage = clamp32(percentage, s.percentageStart, 1)
	}

	value := s.percentageToValue(percentage)
	value = float32(math.Round(float64(value/step))) * step

	if isStart {
		s.percentageStart = percentage
		s.value.setStart(value)
	} else {
		s.percentageEnd = percentage
		s.value.setEnd(value)
	}
	SliderEvents.Emit(cx, SliderEvent{Kind: SliderEventChange, Value: s.value})
	cx.Notify()
}

// HandleRelease emits Release once after a real press or drag
// (U:slider.rs:383-390).
func (s *SliderState) HandleRelease(cx *gpui.Context[SliderState]) {
	if !s.dragging {
		return
	}
	s.dragging = false
	SliderEvents.Emit(cx, SliderEvent{Kind: SliderEventRelease, Value: s.value})
}

// Percentage returns the thumb positions in [0, 1] (U:slider.rs:777-780).
func (s *SliderState) Percentage() (start, end float32) {
	return s.percentageStart, s.percentageEnd
}

// Bounds returns the indicator bounds recorded at prepaint
// (U:slider.rs:782-785).
func (s *SliderState) Bounds() gpui.Bounds { return s.bounds }

// SetBounds records the indicator bounds (U:slider.rs:787-790).
func (s *SliderState) SetBounds(bounds gpui.Bounds) { s.bounds = bounds }

// sliderDragThumb is DragThumb((EntityId, bool)) (U:slider.rs:393-400).
type sliderDragThumb struct {
	entity gpui.EntityID
	start  bool
}

// Render renders the empty drag preview (U:slider.rs:396-400).
func (*sliderDragThumb) Render(*gpui.Window, *gpui.Context[sliderDragThumb]) gpui.AnyElement {
	return gpui.Empty()
}

// sliderDragSlider is DragSlider(EntityId) (U:slider.rs:402-409).
type sliderDragSlider struct {
	entity gpui.EntityID
}

// Render renders the empty drag preview (U:slider.rs:405-409).
func (*sliderDragSlider) Render(*gpui.Window, *gpui.Context[sliderDragSlider]) gpui.AnyElement {
	return gpui.Empty()
}

// sliderDragView builds the drag preview view (`cx.new(|_|
// drag.clone())`) after stopping propagation (U:slider.rs:612-615,
// 751-754).
func sliderDragView[T any, PT interface {
	*T
	gpui.Render[T]
}](value *T, w *gpui.Window, app *gpui.App) gpui.View {
	app.StopPropagation()
	drag := *value
	return gpui.ViewOf[T, PT](gpui.NewEntity(app, w.Scope(), func(s *T, _ *gpui.Context[T]) { *s = drag }))
}

// Slider is an unstyled slider behavior root; applications provide the
// track, range and thumb presentation as children (U:slider.rs:411-421).
// Its style is the base div's (U:slider.rs:461-465 `self.base.style()`).
type Slider struct {
	gpui.Styled[*Slider]
	gpui.Parent[*Slider]

	state    gpui.Entity[SliderState]
	axis     gpui.Axis
	disabled bool
	base     *gpui.DivElement
}

// NewSlider creates a horizontal slider over state (U:slider.rs:424-432).
func NewSlider(state gpui.Entity[SliderState]) *Slider {
	base := gpui.Div().Block()
	s := gpui.BindStyledTo(&Slider{state: state, axis: gpui.AxisHorizontal, base: base}, base.StyleMut())
	return gpui.BindParent(s)
}

// Horizontal lays the slider out horizontally (U:slider.rs:434-437).
func (s *Slider) Horizontal() *Slider {
	s.axis = gpui.AxisHorizontal
	return s
}

// Vertical lays the slider out vertically (U:slider.rs:439-442).
func (s *Slider) Vertical() *Slider {
	s.axis = gpui.AxisVertical
	return s
}

// Axis sets the axis (U:slider.rs:444-447).
func (s *Slider) Axis(axis gpui.Axis) *Slider {
	s.axis = axis
	return s
}

// Disabled sets whether the slider ignores input (U:slider.rs:449-452).
func (s *Slider) Disabled(disabled bool) *Slider {
	s.disabled = disabled
	return s
}

// RenderOnce implements gpui.RenderOnce (U:slider.rs:467-519).
func (s *Slider) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return s.render(w, app).IntoElement()
}

func (s *Slider) render(w *gpui.Window, app *gpui.App) *gpui.DivElement {
	axis := s.axis
	entityID := s.state.EntityID()
	read := s.state.Read(app, func(st *SliderState, _ *gpui.App) [4]float32 {
		return [4]float32{st.Value().End(), st.MinValue(), st.MaxValue(), st.StepValue()}
	})
	value, minValue, maxValue, step := read[0], read[1], read[2], read[3]
	state := s.state

	orientation := gpui.A11yOrientationHorizontal
	if AxisIsVertical(axis) {
		orientation = gpui.A11yOrientationVertical
	}
	d := TestSupport(s.base.ID(gpui.NamedIntegerElementID("slider", uint64(entityID)))).
		Role(gpui.RoleSlider).
		AriaNumericValue(float64(value)).
		AriaMinNumericValue(float64(minValue)).
		AriaMaxNumericValue(float64(maxValue)).
		AriaNumericValueStep(float64(step)).
		AriaOrientation(orientation).
		OnA11yAction(gpui.A11yActionIncrement, func(_ *gpui.A11yActionData, w *gpui.Window, app *gpui.App) {
			state.Update(app, func(st *SliderState, cx *gpui.Context[SliderState]) {
				st.SetValue(SliderValueSingle(min(st.Value().End()+st.StepValue(), st.MaxValue())), w, cx)
			})
		}).
		OnA11yAction(gpui.A11yActionDecrement, func(_ *gpui.A11yActionData, w *gpui.Window, app *gpui.App) {
			state.Update(app, func(st *SliderState, cx *gpui.Context[SliderState]) {
				st.SetValue(SliderValueSingle(max(st.Value().End()-st.StepValue(), st.MinValue())), w, cx)
			})
		})
	if !s.disabled {
		release := func(st *SliderState, _ *gpui.MouseUpEvent, _ *gpui.Window, cx *gpui.Context[SliderState]) {
			st.HandleRelease(cx)
		}
		d.OnMouseUp(gpui.MouseButtonLeft, w.ListenerFor(state, release)).
			OnMouseUpOut(gpui.MouseButtonLeft, w.ListenerFor(state, release))
	}
	return d.Extend(s.TakeChildren())
}

// SliderTrack is an unstyled track that records the geometry used to map
// pointer positions (U:slider.rs:521-529).
type SliderTrack struct {
	gpui.Styled[*SliderTrack]
	gpui.Parent[*SliderTrack]
	gpui.Interactive[*SliderTrack]

	state    gpui.Entity[SliderState]
	axis     gpui.Axis
	disabled bool
	base     *gpui.DivElement
}

// NewSliderTrack creates a horizontal track over state
// (U:slider.rs:532-540).
func NewSliderTrack(state gpui.Entity[SliderState]) *SliderTrack {
	base := gpui.Div().Block()
	t := gpui.BindStyledTo(&SliderTrack{state: state, axis: gpui.AxisHorizontal, base: base}, base.StyleMut())
	return gpui.BindInteractive(gpui.BindParent(t), base)
}

// Axis sets the axis (U:slider.rs:542-545).
func (t *SliderTrack) Axis(axis gpui.Axis) *SliderTrack {
	t.axis = axis
	return t
}

// Disabled sets whether the track ignores input (U:slider.rs:546-549).
func (t *SliderTrack) Disabled(disabled bool) *SliderTrack {
	t.disabled = disabled
	return t
}

// RenderOnce implements gpui.RenderOnce (U:slider.rs:570-634).
func (t *SliderTrack) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

func (t *SliderTrack) render(w *gpui.Window, app *gpui.App) *gpui.DivElement {
	axis := t.axis
	entityID := t.state.EntityID()
	type snapshot struct {
		isRange    bool
		start, end float32
	}
	snap := t.state.Read(app, func(st *SliderState, _ *gpui.App) snapshot {
		start, end := st.Percentage()
		return snapshot{isRange: st.Value().IsRange(), start: start, end: end}
	})
	d := TestSupport(t.base.ID("slider-bar-container")).Extend(t.TakeChildren())
	if t.disabled {
		return d
	}
	d.OnMouseDown(gpui.MouseButtonLeft, w.ListenerFor(t.state, func(st *SliderState, event *gpui.MouseDownEvent, w *gpui.Window, cx *gpui.Context[SliderState]) {
		isStart := false
		if snap.isRange {
			size := st.Bounds().Size.Along(axis)
			var position float32
			if AxisIsHorizontal(axis) {
				position = event.Position.X - st.Bounds().Left()
			} else {
				position = st.Bounds().Bottom() - event.Position.Y
			}
			center := ((snap.end-snap.start)/2 + snap.start) * size
			isStart = position < center
		}
		st.UpdateValueByPosition(axis, event.Position, isStart, w, cx)
	}))
	if !snap.isRange {
		d.OnDrag(sliderDragSlider{entity: entityID}, func(value *sliderDragSlider, _ gpui.Point, w *gpui.Window, app *gpui.App) gpui.View {
			return sliderDragView[sliderDragSlider](value, w, app)
		})
		d.OnDragMove(w.ListenerFor(t.state, func(st *SliderState, event *gpui.DragMoveEvent[sliderDragSlider], w *gpui.Window, cx *gpui.Context[SliderState]) {
			if event.Drag(cx.App()).entity == entityID {
				st.UpdateValueByPosition(axis, event.Event.Position, false, w, cx)
			}
		}))
	}
	return d
}

// SliderIndicator is an unstyled indicator that records the value-mapping
// bounds (U:slider.rs:636-642).
type SliderIndicator struct {
	gpui.Styled[*SliderIndicator]
	gpui.Parent[*SliderIndicator]
	gpui.Interactive[*SliderIndicator]

	state gpui.Entity[SliderState]
	base  *gpui.DivElement
}

// NewSliderIndicator creates an indicator over state
// (U:slider.rs:645-651).
func NewSliderIndicator(state gpui.Entity[SliderState]) *SliderIndicator {
	base := gpui.Div().Block()
	i := gpui.BindStyledTo(&SliderIndicator{state: state, base: base}, base.StyleMut())
	return gpui.BindInteractive(gpui.BindParent(i), base)
}

// RenderOnce implements gpui.RenderOnce (U:slider.rs:674-681): the
// indicator records its prepainted bounds into the state.
func (i *SliderIndicator) RenderOnce(_ *gpui.Window, _ *gpui.App) gpui.AnyElement {
	state := i.state
	d := i.base.ID("slider-bar").Extend(i.TakeChildren())
	return ElementExtOf(d).OnPrepaint(func(bounds gpui.Bounds, _ *gpui.Window, app *gpui.App) {
		state.Update(app, func(st *SliderState, _ *gpui.Context[SliderState]) { st.SetBounds(bounds) })
	}).IntoElement()
}

// SliderThumb is an unstyled draggable thumb (U:slider.rs:683-692).
type SliderThumb struct {
	gpui.Styled[*SliderThumb]
	gpui.Parent[*SliderThumb]
	gpui.Interactive[*SliderThumb]

	state    gpui.Entity[SliderState]
	axis     gpui.Axis
	start    bool
	disabled bool
	base     *gpui.DivElement
}

// NewSliderThumb creates an end thumb over state (U:slider.rs:695-704).
func NewSliderThumb(state gpui.Entity[SliderState]) *SliderThumb {
	base := gpui.Div().Block()
	t := gpui.BindStyledTo(&SliderThumb{state: state, axis: gpui.AxisHorizontal, base: base}, base.StyleMut())
	return gpui.BindInteractive(gpui.BindParent(t), base)
}

// Axis sets the axis (U:slider.rs:706-709).
func (t *SliderThumb) Axis(axis gpui.Axis) *SliderThumb {
	t.axis = axis
	return t
}

// Start makes this the range's start thumb (U:slider.rs:710-713).
func (t *SliderThumb) Start(start bool) *SliderThumb {
	t.start = start
	return t
}

// Disabled sets whether the thumb ignores input (U:slider.rs:714-717).
func (t *SliderThumb) Disabled(disabled bool) *SliderThumb {
	t.disabled = disabled
	return t
}

// RenderOnce implements gpui.RenderOnce (U:slider.rs:740-772).
func (t *SliderThumb) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

func (t *SliderThumb) render(w *gpui.Window, _ *gpui.App) *gpui.DivElement {
	entityID := t.state.EntityID()
	axis := t.axis
	start := t.start
	var startIndex uint64
	if start {
		startIndex = 1
	}
	d := TestSupport(t.base.ID(gpui.NamedIntegerElementID("slider-thumb", startIndex))).Extend(t.TakeChildren())
	if t.disabled {
		return d
	}
	d.OnMouseDown(gpui.MouseButtonLeft, stopPropagation).
		OnDrag(sliderDragThumb{entity: entityID, start: start}, func(value *sliderDragThumb, _ gpui.Point, w *gpui.Window, app *gpui.App) gpui.View {
			return sliderDragView[sliderDragThumb](value, w, app)
		})
	d.OnDragMove(w.ListenerFor(t.state, func(st *SliderState, event *gpui.DragMoveEvent[sliderDragThumb], w *gpui.Window, cx *gpui.Context[SliderState]) {
		drag := event.Drag(cx.App())
		if drag.entity == entityID {
			st.UpdateValueByPosition(axis, event.Event.Position, drag.start, w, cx)
		}
	}))
	return d
}
