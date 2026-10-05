// Package authorspec executes the authoring counter example (ticket11)
// against the real gpui runtime: a Counter model entity, its typed event
// and action descriptors, the CounterView render (a div tree with the
// count text and the click/action listener registrations of the
// authoring contract), the reusable CountLabel recipe composing the
// existing authoring Styled helper, and the two-window host shape with
// independent model leases.
//
// This mirrors .scratch/signature-validation/example/counter.go (the
// compiler-validated API shape) with real behavior behind it: the
// entities, events, listeners, element phases, layout, text shaping and
// scene painting run through the port's runtime. Interactivity (focus
// routing, key dispatch, click delivery) is ticket13's slice: the
// handlers are registered and stored, and nothing dispatches them yet.
package authorspec

import (
	"fmt"

	"gpui-go/authoring"
	"gpui-go/gpui"
)

// Increment is the counter's command: please increment.
type Increment struct{}

// CountChanged is the counter's event: the count changed.
type CountChanged struct {
	// Value is the new count.
	Value int
}

// countChanged links the Counter source to the CountChanged payload.
var countChanged = gpui.DefineEvent[Counter, CountChanged]()

// incrementAction is the stable external action descriptor of Increment.
var incrementAction = gpui.DefineUnitAction[Increment]("counter::Increment")

// Counter is the persistent counter state: the count and its focus
// handle.
type Counter struct {
	count int
	focus gpui.FocusHandle
}

// increment mutates the count, notifies observers and emits the typed
// event (the documented Notify + Emit pairing: notify coalesces, each
// emission is separately queued).
func (c *Counter) increment(cx *gpui.Context[Counter]) {
	c.count++
	cx.Notify()
	countChanged.Emit(cx, CountChanged{Value: c.count})
}

// clicked is the increment div's click listener (delivery arrives with
// the input ticket).
func (c *Counter) clicked(_ *gpui.ClickEvent, w *gpui.Window, cx *gpui.Context[Counter]) {
	w.Focus(c.focus)
	c.increment(cx)
}

// incrementAction is the routed-command listener (dispatch arrives with
// the focus/keyboard ticket).
func (c *Counter) incrementAction(_ *Increment, _ *gpui.Window, cx *gpui.Context[Counter]) {
	c.increment(cx)
}

// Render renders the counter view: a div tree with the count label and
// the increment button (the documented counter example shape).
func (c *Counter) Render(w *gpui.Window, cx *gpui.Context[Counter]) gpui.AnyElement {
	return gpui.Div().
		ID("counter").
		TrackFocus(c.focus).
		KeyContext("Counter").
		OnAction(incrementAction, cx.Listener((*Counter).incrementAction)).
		Flex().
		Gap2().
		Child(NewCountLabel(c.count).Px3().Prefix("Count: ")).
		Child(gpui.Div().
			ID("increment").
			OnClick(cx.Listener((*Counter).clicked)).
			Child("Increment")).
		IntoElement()
}

// CountChangedEvent is the typed event descriptor of the counter.
func CountChangedEvent() gpui.Event[Counter, CountChanged] { return countChanged }

// IncrementAction is the typed action descriptor of the counter.
func IncrementAction() gpui.Action[Increment] { return incrementAction }

// Count reads the counter's current value.
func Count(c *Counter) int { return c.count }

// SetFocus installs the counter's focus handle (test support for the
// constructor path).
func SetFocus(c *Counter, focus gpui.FocusHandle) { c.focus = focus }

// Focus returns the counter's focus handle.
func Focus(c *Counter) gpui.FocusHandle { return c.focus }

// ---------------------------------------------------------------------------
// The reusable label recipe (the RenderOnce component of the example)
// ---------------------------------------------------------------------------

// CountLabel is the reusable count label: a stateless recipe carrying
// the count for one tree construction through the existing authoring
// Styled helper (value-embedded, constructor-bound styling).
type CountLabel struct {
	authoring.Styled[*CountLabel]
	value  int
	prefix string
}

// NewCountLabel constructs a bound count label recipe.
func NewCountLabel(value int) *CountLabel {
	return authoring.BindStyled(&CountLabel{value: value})
}

// Prefix sets the label's text prefix.
func (l *CountLabel) Prefix(value string) *CountLabel {
	l.prefix = value
	return l
}

// RenderOnce renders the label: a div styled by the recipe's refinement
// with the formatted count as its text child.
func (l *CountLabel) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return gpui.Div().
		Refine(l.Style()).
		Child(fmt.Sprintf("%s%d", l.prefix, l.value)).
		IntoElement()
}

// ---------------------------------------------------------------------------
// The two-window host shape (independent model leases)
// ---------------------------------------------------------------------------

// Summary observes the counter's typed events and records the last
// value (the subscriber of the documented example).
type Summary struct {
	last int
}

// Last returns the last observed count.
func Last(s *Summary) int { return s.last }

// NewCounter builds a counter entity whose focus handle belongs to the
// given window (the host focus seam of ticket13).
func NewCounter(cx gpui.AppContext, owner *gpui.Scope, w *gpui.Window, initial int) gpui.Entity[Counter] {
	return gpui.NewEntity(cx, owner, func(counter *Counter, cx *gpui.Context[Counter]) {
		counter.count = initial
		if w != nil {
			counter.focus = gpui.NewFocusHandle(w)
		}
	})
}

// NewSummary builds a summary entity subscribed to the counter's typed
// events in its own dependent scope (independent of the window that
// created the counter).
func NewSummary(cx gpui.AppContext, owner *gpui.Scope, counter gpui.Entity[Counter]) gpui.Entity[Summary] {
	return gpui.NewEntity(cx, owner, func(summary *Summary, cx *gpui.Context[Summary]) {
		countChanged.Subscribe(cx, counter, func(s *Summary, _ gpui.Entity[Counter], event *CountChanged, _ *gpui.Context[Summary]) {
			s.last = event.Value
			cx.Notify()
		}).Detach()
	})
}

// TwoOwners mirrors the documented two-owner shape: window B retains an
// independent lease of the counter and both windows' summaries
// subscribe independently (closing window A leaves window B's lease and
// subscription working).
func TwoOwners(app *gpui.App, counter gpui.Entity[Counter], windowAScope, windowBScope *gpui.Scope) (gpui.Entity[Summary], gpui.Entity[Summary]) {
	counterB := counter.RetainInto(windowBScope)

	summaryA := gpui.NewEntity[Summary](app, windowAScope, func(s *Summary, cx *gpui.Context[Summary]) {
		countChanged.Subscribe(cx, counter, (*Summary).changed).Detach()
	})
	summaryB := gpui.NewEntity[Summary](app, windowBScope, func(s *Summary, cx *gpui.Context[Summary]) {
		countChanged.Subscribe(cx, counterB, (*Summary).changed).Detach()
	})
	return summaryA, summaryB
}

// changed records the last observed value (the documented subscriber
// method shape).
func (s *Summary) changed(_ gpui.Entity[Counter], event *CountChanged, cx *gpui.Context[Summary]) {
	s.last = event.Value
	cx.Notify()
}
