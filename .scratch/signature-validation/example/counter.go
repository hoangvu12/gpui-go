// Extracted from the counter-view documentation. Compile only; never run.
package example

import (
	"fmt"
	gpui "gpui-go"
	"gpui-go/authoring"
)

type Summary struct{ last int }
type Increment struct{}               // A command: please increment.
type CountChanged struct{ Value int } // An event: the count changed.

var countChanged = gpui.DefineEvent[Counter, CountChanged]()
var incrementAction = gpui.DefineUnitAction[Increment]("counter::Increment")

type Counter struct {
	count int
	focus gpui.FocusHandle
}

func (c *Counter) increment(cx *gpui.Context[Counter]) {
	c.count++
	cx.Notify()
	countChanged.Emit(cx, CountChanged{Value: c.count})
}

func (c *Counter) clicked(
	_ *gpui.ClickEvent, w *gpui.Window, cx *gpui.Context[Counter],
) {
	w.Focus(c.focus)
	c.increment(cx)
}

func (c *Counter) incrementAction(
	_ *Increment, _ *gpui.Window, cx *gpui.Context[Counter],
) {
	c.increment(cx)
}

func (c *Counter) Render(
	w *gpui.Window, cx *gpui.Context[Counter],
) gpui.AnyElement {
	return gpui.Div().
		ID("counter").
		TrackFocus(c.focus).
		KeyContext("Counter").
		OnAction(incrementAction, cx.Listener((*Counter).incrementAction)).
		Flex().Gap2().
		Child(NewCountLabel(c.count).Px3().Prefix("Count: ")).
		Child(gpui.Div().
			ID("increment").
			OnClick(cx.Listener((*Counter).clicked)).
			Child("Increment")).
		IntoElement()
}

type CountLabel struct {
	authoring.Styled[*CountLabel]
	value  int
	prefix string
}

func NewCountLabel(value int) *CountLabel {
	return authoring.BindStyled(&CountLabel{value: value})
}

func (l *CountLabel) Prefix(value string) *CountLabel {
	l.prefix = value
	return l
}

func (l *CountLabel) RenderOnce(
	w *gpui.Window, app *gpui.App,
) gpui.AnyElement {
	return gpui.Div().
		Refine(l.Style()).
		Child(fmt.Sprintf("%s%d", l.prefix, l.value)).
		IntoElement()
}

func (s *Summary) changed(
	source gpui.Entity[Counter], event *CountChanged, cx *gpui.Context[Summary],
) {
	s.last = event.Value
	cx.Notify()
}

// During Summary initialization, once, not during every Render:
// subscription := countChanged.Subscribe(cx, counter, (*Summary).changed)
// observer := cx.Observe(counter, (*Summary).counterNotified)

func TwoOwners(app *gpui.App, counter gpui.Entity[Counter], windowAScope, windowBScope *gpui.Scope) {
	counterB := counter.RetainInto(windowBScope)

	summaryA := gpui.NewEntity[Summary](app, windowAScope,
		func(s *Summary, cx *gpui.Context[Summary]) {
			countChanged.Subscribe(cx, counter, (*Summary).changed)
		})
	summaryB := gpui.NewEntity[Summary](app, windowBScope,
		func(s *Summary, cx *gpui.Context[Summary]) {
			countChanged.Subscribe(cx, counterB, (*Summary).changed)
		})
	// The host attaches/render-retains summaryA and summaryB in their windows.
	// Closing window A tears down its callbacks/frames and closes windowAScope.
	// Window B still owns counterB and summaryB, with its own subscription.

	_, _ = summaryA, summaryB
}
