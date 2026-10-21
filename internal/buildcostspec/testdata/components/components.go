// Package components is a probe for TestConsumerCompileCost: five custom
// components with the Base shape, each embedding gpui.Component[Self].
package components

import "github.com/hoangvu12/gpui-go/gpui"

// Button is a probe component.
type Button struct {
	gpui.Component[*Button]
	label string
}

// NewButton returns a Button.
func NewButton(label string) *Button {
	return gpui.BindComponent(&Button{label: label}, gpui.Div())
}

// IntoElement renders the inner div refined by the component's style.
func (c *Button) IntoElement() gpui.AnyElement {
	return c.Interactivity().Refine(c.Style()).Child(c.label).Extend(c.TakeChildren()).IntoElement()
}

// Card is a probe component.
type Card struct {
	gpui.Component[*Card]
	label string
}

// NewCard returns a Card.
func NewCard(label string) *Card {
	return gpui.BindComponent(&Card{label: label}, gpui.Div())
}

// IntoElement renders the inner div refined by the component's style.
func (c *Card) IntoElement() gpui.AnyElement {
	return c.Interactivity().Refine(c.Style()).Child(c.label).Extend(c.TakeChildren()).IntoElement()
}

// Badge is a probe component.
type Badge struct {
	gpui.Component[*Badge]
	label string
}

// NewBadge returns a Badge.
func NewBadge(label string) *Badge {
	return gpui.BindComponent(&Badge{label: label}, gpui.Div())
}

// IntoElement renders the inner div refined by the component's style.
func (c *Badge) IntoElement() gpui.AnyElement {
	return c.Interactivity().Refine(c.Style()).Child(c.label).Extend(c.TakeChildren()).IntoElement()
}

// Toggle is a probe component.
type Toggle struct {
	gpui.Component[*Toggle]
	label string
}

// NewToggle returns a Toggle.
func NewToggle(label string) *Toggle {
	return gpui.BindComponent(&Toggle{label: label}, gpui.Div())
}

// IntoElement renders the inner div refined by the component's style.
func (c *Toggle) IntoElement() gpui.AnyElement {
	return c.Interactivity().Refine(c.Style()).Child(c.label).Extend(c.TakeChildren()).IntoElement()
}

// Chip is a probe component.
type Chip struct {
	gpui.Component[*Chip]
	label string
}

// NewChip returns a Chip.
func NewChip(label string) *Chip {
	return gpui.BindComponent(&Chip{label: label}, gpui.Div())
}

// IntoElement renders the inner div refined by the component's style.
func (c *Chip) IntoElement() gpui.AnyElement {
	return c.Interactivity().Refine(c.Style()).Child(c.label).Extend(c.TakeChildren()).IntoElement()
}
