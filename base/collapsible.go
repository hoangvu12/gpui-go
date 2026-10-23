package base

// An unstyled controlled collapsible region (U:collapsible.rs).
//
// Naming: component_traits::Collapsible is CollapsibleTrait (doc.go);
// this crate-root component keeps the name.
//
// Display: the base is a plain `div()` (U:collapsible.rs:24), display
// Block under Base's gpui-ce 0.2.2 but Flex under the core pin
// (R:style.rs:1024-1027, map.md gap 2b): a Flex row would put the
// content beside the trigger instead of below it. NewCollapsible sets
// Block before the caller's style (which writes straight into the base,
// U:collapsible.rs:55-59) refines over it.
//
// ParentElement: the child list interleaves ordinary children and
// content in call order (U:collapsible.rs:7-10, 61-66), so the component
// keeps its own list instead of embedding gpui.Parent; Child, Children
// and Extend follow gpui.Parent's conversion rules (gpui.TryIntoElement).

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
)

// collapsibleChild is one child: an ordinary element or gated content
// (U:collapsible.rs:7-10).
type collapsibleChild struct {
	element gpui.AnyElement
	content bool
}

// collapsibleReveal is the optional reveal id and progress.
type collapsibleReveal struct {
	id       gpui.ElementID
	progress float32
}

// Collapsible is a controlled region whose content can be expanded or
// collapsed (U:collapsible.rs:12-19).
type Collapsible struct {
	gpui.Styled[*Collapsible]

	base     *gpui.DivElement
	children []collapsibleChild
	open     bool
	reveal   *collapsibleReveal
}

// NewCollapsible creates a closed collapsible (U:collapsible.rs:21-29;
// also Default, U:collapsible.rs:49-53).
func NewCollapsible() *Collapsible {
	// Block: the 0.2.2 plain-div default (see the file comment).
	base := gpui.Div().Block()
	return gpui.BindStyledTo(&Collapsible{base: base}, base.StyleMut())
}

// Open sets whether the content is shown (U:collapsible.rs:31-34).
func (c *Collapsible) Open(open bool) *Collapsible {
	c.open = open
	return c
}

// Content adds content that is shown only while open, or revealed when a
// reveal is set (U:collapsible.rs:36-40). It panics on an invalid child
// value like gpui.Parent.Child.
func (c *Collapsible) Content(content any) *Collapsible {
	c.children = append(c.children, collapsibleChild{element: collapsibleConvert(0, content), content: true})
	return c
}

// Reveal keeps content mounted and reveals it at normalized progress
// through MotionReveal (U:collapsible.rs:42-46).
func (c *Collapsible) Reveal(id gpui.ElementID, progress float32) *Collapsible {
	c.reveal = &collapsibleReveal{id: id, progress: progress}
	return c
}

// Extend appends ordinary children (U:collapsible.rs:61-66).
func (c *Collapsible) Extend(elements []gpui.AnyElement) {
	for _, e := range elements {
		c.children = append(c.children, collapsibleChild{element: e})
	}
}

// Child adds one ordinary child (ParentElement::child).
func (c *Collapsible) Child(value any) *Collapsible { return c.Children(value) }

// Children adds ordinary children (ParentElement::children).
func (c *Collapsible) Children(values ...any) *Collapsible {
	for i, v := range values {
		c.children = append(c.children, collapsibleChild{element: collapsibleConvert(i, v)})
	}
	return c
}

func collapsibleConvert(index int, value any) gpui.AnyElement {
	el, err := gpui.TryIntoElement(value)
	if err != nil {
		panic(fmt.Sprintf("base: Collapsible child %d: %v", index, err))
	}
	return el
}

// RenderOnce implements gpui.RenderOnce (U:collapsible.rs:68-81):
// content is wrapped in a MotionReveal when a reveal is set, otherwise
// rendered only while open.
func (c *Collapsible) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	children := make([]gpui.AnyElement, 0, len(c.children))
	for _, child := range c.children {
		switch {
		case !child.content:
			children = append(children, child.element)
		case c.reveal != nil:
			children = append(children, NewMotionReveal(c.reveal.id, c.reveal.progress, child.element).IntoElement())
		case c.open:
			children = append(children, child.element)
		}
	}
	c.children = nil
	return c.base.Extend(children).IntoElement()
}
