package base

// Unstyled accordion parts (U:accordion.rs): Accordion, AccordionItem,
// AccordionHeader, AccordionPanel and AccordionTrigger, built on the
// Button pattern (base/button.go). Parts without interactivity embed only
// gpui.Styled and gpui.Parent (their Rust types implement Styled and
// ParentElement only).
//
// Display: every part's root is a plain div (U:accordion.rs:26, 82, 190,
// 261, 290), display Block under Base's gpui-ce 0.2.2 but Flex under the
// core pin (R:style.rs:1024-1027, map.md gap 2b): a Flex item would lay
// header and panel side by side instead of stacking them. Each root sets
// Block before the caller's style refines over it.

import "github.com/hoangvu12/gpui-go/gpui"

// AccordionChangeHandler handles a requested change of the controlled
// expanded state (U:accordion.rs:13 ChangeHandler).
type AccordionChangeHandler = func(open bool, event *gpui.ClickEvent, w *gpui.Window, app *gpui.App)

// Accordion is an unstyled accordion root for application-owned items
// (U:accordion.rs:15-21). It exposes RoleGroup.
type Accordion struct {
	gpui.Component[*Accordion]

	base *gpui.DivElement
}

// NewAccordion creates an accordion root (U:accordion.rs:23-31).
func NewAccordion[I gpui.IntoElementID](id I) *Accordion {
	base := TestSupport(gpui.Div().ID(id).Block())
	return gpui.BindComponent(&Accordion{base: base}, base)
}

// RenderOnce implements gpui.RenderOnce (U:accordion.rs:58-65).
func (a *Accordion) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return a.render(w, app).IntoElement()
}

func (a *Accordion) render(*gpui.Window, *gpui.App) *gpui.DivElement {
	style := a.Style()
	d := a.base.Role(gpui.RoleGroup)
	d.Extend(a.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}

// AccordionItem connects one trigger with its panel content
// (U:accordion.rs:67-77).
type AccordionItem struct {
	gpui.Styled[*AccordionItem]
	gpui.Parent[*AccordionItem]

	open     bool
	disabled bool
	header   *AccordionHeader
	panel    *AccordionPanel
}

// NewAccordionItem creates an item (U:accordion.rs:79-90).
func NewAccordionItem() *AccordionItem {
	return gpui.BindParent(gpui.BindStyled(&AccordionItem{}))
}

// Open sets the controlled expanded state (U:accordion.rs:91-94).
func (i *AccordionItem) Open(open bool) *AccordionItem {
	i.open = open
	return i
}

// Disabled disables the item's trigger (U:accordion.rs:96-99).
func (i *AccordionItem) Disabled(disabled bool) *AccordionItem {
	i.disabled = disabled
	return i
}

// Header sets the item's header (U:accordion.rs:101-104).
func (i *AccordionItem) Header(header *AccordionHeader) *AccordionItem {
	i.header = header
	return i
}

// Panel sets the item's panel (U:accordion.rs:106-109).
func (i *AccordionItem) Panel(panel *AccordionPanel) *AccordionItem {
	i.panel = panel
	return i
}

// RenderOnce implements gpui.RenderOnce (U:accordion.rs:124-134): the
// header receives the item's open and disabled state, the panel its open
// state.
func (i *AccordionItem) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	style := i.Style()
	d := gpui.Div().Block()
	if i.header != nil {
		d.Child(i.header.open(i.open).disabled(i.disabled))
	}
	if i.panel != nil {
		d.Child(i.panel.Open(i.open))
	}
	d.Extend(i.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style).IntoElement()
}

// AccordionHeader is the heading that owns one item's trigger
// (U:accordion.rs:136-144). With an id it exposes RoleHeading at its
// level (default 3).
type AccordionHeader struct {
	gpui.Styled[*AccordionHeader]
	gpui.Parent[*AccordionHeader]

	id      *gpui.ElementID
	level   int
	trigger *AccordionTrigger
}

// NewAccordionHeader creates a header around trigger
// (U:accordion.rs:146-155).
func NewAccordionHeader(trigger *AccordionTrigger) *AccordionHeader {
	return gpui.BindParent(gpui.BindStyled(&AccordionHeader{level: 3, trigger: trigger}))
}

// Level sets the heading level (U:accordion.rs:156-159).
func (h *AccordionHeader) Level(level int) *AccordionHeader {
	h.level = level
	return h
}

// ID sets the header's element id, which also enables its heading
// semantics (U:accordion.rs:161-164).
func (h *AccordionHeader) ID[I gpui.IntoElementID](id I) *AccordionHeader {
	eid := gpui.ElementIDOf(id)
	h.id = &eid
	return h
}

// open forwards the item's state to the trigger (U:accordion.rs:165-168).
func (h *AccordionHeader) open(open bool) *AccordionHeader {
	h.trigger.Open(open)
	return h
}

// disabled forwards the item's state to the trigger
// (U:accordion.rs:170-173).
func (h *AccordionHeader) disabled(disabled bool) *AccordionHeader {
	h.trigger.Disabled(disabled)
	return h
}

// RenderOnce implements gpui.RenderOnce (U:accordion.rs:188-204).
func (h *AccordionHeader) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return h.render(w, app).IntoElement()
}

func (h *AccordionHeader) render(*gpui.Window, *gpui.App) *gpui.DivElement {
	style := h.Style()
	content := gpui.Div().Block().Child(h.trigger)
	content.Extend(h.TakeChildren())
	StyledExtOf(content).RefineStyle(&style)
	if h.id != nil {
		TestSupport(content.ID(*h.id)).Role(gpui.RoleHeading).AriaLevel(h.level)
	}
	return content
}

// AccordionPanel is an accordion panel with controlled mounting
// (U:accordion.rs:206-214). With an id it exposes RoleRegion.
type AccordionPanel struct {
	gpui.Styled[*AccordionPanel]
	gpui.Parent[*AccordionPanel]

	id          *gpui.ElementID
	open        bool
	keepMounted bool
}

// NewAccordionPanel creates a panel (U:accordion.rs:216-225).
func NewAccordionPanel() *AccordionPanel {
	return gpui.BindParent(gpui.BindStyled(&AccordionPanel{}))
}

// Open sets whether the panel is shown (U:accordion.rs:227-230).
func (p *AccordionPanel) Open(open bool) *AccordionPanel {
	p.open = open
	return p
}

// KeepMounted keeps the closed panel's content mounted
// (U:accordion.rs:232-235).
func (p *AccordionPanel) KeepMounted(keepMounted bool) *AccordionPanel {
	p.keepMounted = keepMounted
	return p
}

// ID sets the panel's element id, which also enables its region
// semantics (U:accordion.rs:237-240).
func (p *AccordionPanel) ID[I gpui.IntoElementID](id I) *AccordionPanel {
	eid := gpui.ElementIDOf(id)
	p.id = &eid
	return p
}

// RenderOnce implements gpui.RenderOnce (U:accordion.rs:255-271): a
// closed panel that is not kept mounted renders gpui.Empty.
func (p *AccordionPanel) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	if d := p.render(w, app); d != nil {
		return d.IntoElement()
	}
	return gpui.Empty()
}

// render returns the panel's div, or nil when unmounted.
func (p *AccordionPanel) render(*gpui.Window, *gpui.App) *gpui.DivElement {
	if !p.open && !p.keepMounted {
		return nil
	}
	style := p.Style()
	content := gpui.Div().Block()
	content.Extend(p.TakeChildren())
	StyledExtOf(content).RefineStyle(&style)
	if p.id != nil {
		TestSupport(content.ID(*p.id)).Role(gpui.RoleRegion)
	}
	return content
}

// AccordionTrigger is an accordion trigger with controlled expanded
// state (U:accordion.rs:273-285). The application owns the title, icon,
// content, layout and animation; activation requests the opposite of
// open. It exposes RoleButton with aria_expanded.
type AccordionTrigger struct {
	gpui.Component[*AccordionTrigger]

	base     *gpui.DivElement
	open     bool
	disabled bool
	onChange AccordionChangeHandler
}

// NewAccordionTrigger creates a trigger (U:accordion.rs:287-297).
func NewAccordionTrigger[I gpui.IntoElementID](id I) *AccordionTrigger {
	base := TestSupport(gpui.Div().ID(id).Block())
	return gpui.BindComponent(&AccordionTrigger{base: base}, base)
}

// Open sets the controlled expanded state (U:accordion.rs:299-302).
func (t *AccordionTrigger) Open(open bool) *AccordionTrigger {
	t.open = open
	return t
}

// Disabled disables activation (U:accordion.rs:304-307).
func (t *AccordionTrigger) Disabled(disabled bool) *AccordionTrigger {
	t.disabled = disabled
	return t
}

// OnChange handles a requested change to the controlled expanded state
// (U:accordion.rs:309-316).
func (t *AccordionTrigger) OnChange(handler AccordionChangeHandler) *AccordionTrigger {
	t.onChange = handler
	return t
}

// RenderOnce implements gpui.RenderOnce (U:accordion.rs:344-360).
func (t *AccordionTrigger) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return t.render(w, app).IntoElement()
}

func (t *AccordionTrigger) render(*gpui.Window, *gpui.App) *gpui.DivElement {
	style := t.Style()
	nextOpen := !t.open
	d := t.base.Role(gpui.RoleButton).AriaExpanded(t.open)
	if onChange := t.onChange; !t.disabled && onChange != nil {
		d.OnClick(func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			onChange(nextOpen, event, w, app)
		})
	}
	d.Extend(t.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}
