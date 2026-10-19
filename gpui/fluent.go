package gpui

// This file ports the remaining builder traits Base components rely on
// (ticket base-05):
//
//   - FluentBuilder (reference crates/gpui/src/util.rs:11-61): map, when,
//     when_else, when_some, when_none. They are methods of the bound
//     Styled[Self] helper, so every styled element (DivElement and every
//     component embedding Styled) gets them.
//   - ParentElement (element.rs:228-249): Parent[Self] owns a child list
//     like the Base components' `children: SmallVec<[AnyElement; _]>`
//     with `fn extend` (e.g. upstream base/src/button.rs:193-197).
//   - InteractiveElement (elements/div.rs; the Base shape
//     `fn interactivity(&mut self) -> &mut Interactivity {
//     self.base.interactivity() }`, button.rs:199-203): Interactive[Self]
//     forwards the div's interactivity builders to an inner *DivElement
//     and returns the owner.
//   - Component[Self] embeds all three, so a custom component gets the
//     whole vocabulary from one embedded mixin and one BindComponent call.
//
// The binding and copy rules are Styled's: embed by value, bind once in
// the constructor, use the original pointer.

// ---------------------------------------------------------------------------
// FluentBuilder (util.rs:11-61)
// ---------------------------------------------------------------------------

// Map imperatively modifies the element with f and returns f's result
// (util.rs:13 FluentBuilder::map<U>).
func (s *Styled[Self]) Map[U any](f func(Self) U) U {
	s.check()
	return f(s.self)
}

// When applies then when condition holds (util.rs:21).
func (s *Styled[Self]) When(condition bool, then func(Self) Self) Self {
	s.check()
	if condition {
		return then(s.self)
	}
	return s.self
}

// WhenElse applies then or elseFn by condition (util.rs:29).
func (s *Styled[Self]) WhenElse(condition bool, then, elseFn func(Self) Self) Self {
	s.check()
	if condition {
		return then(s.self)
	}
	return elseFn(s.self)
}

// WhenSome applies then with the value when option is present (util.rs:42;
// the pinned Option<T> is a nil-able pointer).
func (s *Styled[Self]) WhenSome[T any](option *T, then func(Self, T) Self) Self {
	s.check()
	if option != nil {
		return then(s.self, *option)
	}
	return s.self
}

// WhenNone applies then when option is absent (util.rs:55).
func (s *Styled[Self]) WhenNone[T any](option *T, then func(Self) Self) Self {
	s.check()
	if option == nil {
		return then(s.self)
	}
	return s.self
}

// ---------------------------------------------------------------------------
// ParentElement (element.rs:228-249)
// ---------------------------------------------------------------------------

// Parent supplies the ParentElement methods over an owned child list.
// Embed Parent[*YourComponent] by value and bind it with BindParent (or
// embed Component and use BindComponent).
type Parent[Self any] struct {
	self       Self
	anchor     *Parent[Self]
	children   []AnyElement
	converting bool
}

// ParentBase is the binding hook promoted through value embedding.
func (p *Parent[Self]) ParentBase() *Parent[Self] { return p }

// BindParent binds a newly constructed component to its embedded Parent
// helper and returns the component. It panics like BindStyled.
func BindParent[Value any, Self interface {
	*Value
	ParentBase() *Parent[Self]
}](self Self) Self {
	if self == nil {
		panic("gpui: cannot bind a nil component")
	}
	p := self.ParentBase()
	if p == nil {
		panic("gpui: component has no Parent helper")
	}
	p.bind(self)
	return self
}

func (p *Parent[Self]) bind(self Self) {
	if p.anchor != nil {
		panic("gpui: Parent is already bound; create a new component instead of rebinding or copying")
	}
	p.self, p.anchor = self, p
}

func (p *Parent[Self]) check() {
	if p == nil || p.anchor == nil {
		panic("gpui: unbound Parent; construct the component with BindParent or BindComponent")
	}
	if p.anchor != p {
		panic("gpui: copied bound component; use its original pointer")
	}
}

// Extend appends converted child elements (element.rs:230
// ParentElement::extend).
func (p *Parent[Self]) Extend(elements []AnyElement) {
	p.check()
	if p.converting {
		panic("gpui: reentrant child mutation of Parent during child conversion")
	}
	p.children = append(p.children, elements...)
}

// Child adds one child, converting it with the selected precedence and
// panicking on invalid input (element.rs:233 ParentElement::child).
func (p *Parent[Self]) Child(value any) Self {
	return p.Children(value)
}

// Children adds multiple children (element.rs:242
// ParentElement::children).
func (p *Parent[Self]) Children(values ...any) Self {
	p.check()
	if len(values) == 0 {
		return p.self
	}
	if p.converting {
		panic("gpui: reentrant child mutation of Parent during child conversion")
	}
	p.converting = true
	converted := make([]AnyElement, 0, len(values))
	func() {
		defer func() { p.converting = false }()
		for i, value := range values {
			converted = append(converted, intoChild("Parent", i, value))
		}
	}()
	reserveAndCommitRecipes(converted)
	p.children = append(p.children, converted...)
	return p.self
}

// TakeChildren removes and returns the collected children (the render
// step's `self.children` move into the inner element).
func (p *Parent[Self]) TakeChildren() []AnyElement {
	p.check()
	children := p.children
	p.children = nil
	return children
}

// ChildCount reports the collected child count.
func (p *Parent[Self]) ChildCount() int {
	p.check()
	return len(p.children)
}

// ---------------------------------------------------------------------------
// InteractiveElement delegation (div.rs; button.rs:199-203)
// ---------------------------------------------------------------------------

// Interactive forwards the div's interactivity builders to an inner
// *DivElement and returns the owner. Embed Interactive[*YourComponent]
// by value and bind it with BindInteractive (or embed Component and use
// BindComponent).
type Interactive[Self any] struct {
	self   Self
	anchor *Interactive[Self]
	base   *DivElement
}

// InteractiveBase is the binding hook promoted through value embedding.
func (i *Interactive[Self]) InteractiveBase() *Interactive[Self] { return i }

// BindInteractive binds a newly constructed component's Interactive
// helper to the inner div that carries its interactivity. It panics
// like BindStyled, and on a nil div.
func BindInteractive[Value any, Self interface {
	*Value
	InteractiveBase() *Interactive[Self]
}](self Self, base *DivElement) Self {
	if self == nil {
		panic("gpui: cannot bind a nil component")
	}
	i := self.InteractiveBase()
	if i == nil {
		panic("gpui: component has no Interactive helper")
	}
	i.bind(self, base)
	return self
}

func (i *Interactive[Self]) bind(self Self, base *DivElement) {
	if base == nil {
		panic("gpui: Interactive requires a non-nil inner div")
	}
	if i.anchor != nil {
		panic("gpui: Interactive is already bound; create a new component instead of rebinding or copying")
	}
	i.self, i.anchor, i.base = self, i, base
}

func (i *Interactive[Self]) check() {
	if i == nil || i.anchor == nil {
		panic("gpui: unbound Interactive; construct the component with BindInteractive or BindComponent")
	}
	if i.anchor != i {
		panic("gpui: copied bound component; use its original pointer")
	}
}

// Interactivity returns the inner div carrying the interactivity (the
// pinned InteractiveElement::interactivity accessor).
func (i *Interactive[Self]) Interactivity() *DivElement {
	i.check()
	return i.base
}

// ID assigns the inner div's element identity (DivElement.ID).
func (i *Interactive[Self]) ID[I IntoElementID](value I) Self {
	i.Interactivity().ID(value)
	return i.self
}

// TrackFocus tracks a focus handle on the inner div (DivElement.TrackFocus).
func (i *Interactive[Self]) TrackFocus(value FocusHandle) Self {
	i.Interactivity().TrackFocus(value)
	return i.self
}

// KeyContext sets the inner div's key context (DivElement.KeyContext).
func (i *Interactive[Self]) KeyContext(value string) Self {
	i.Interactivity().KeyContext(value)
	return i.self
}

// OnClick binds a click listener on the inner div (DivElement.OnClick).
func (i *Interactive[Self]) OnClick(f func(*ClickEvent, *Window, *App)) Self {
	i.Interactivity().OnClick(f)
	return i.self
}

// OnAction binds an action listener on the inner div
// (DivElement.OnAction).
func (i *Interactive[Self]) OnAction[A any](a Action[A], f func(*A, *Window, *App)) Self {
	i.Interactivity().OnAction(a, f)
	return i.self
}

// CaptureAction binds a capture-phase action listener on the inner div
// (DivElement.CaptureAction).
func (i *Interactive[Self]) CaptureAction[A any](a Action[A], f func(*A, *Window, *App)) Self {
	i.Interactivity().CaptureAction(a, f)
	return i.self
}

// OnBoxedAction binds a boxed-action listener on the inner div
// (DivElement.OnBoxedAction).
func (i *Interactive[Self]) OnBoxedAction(a BoxedAction, f func(BoxedAction, *Window, *App)) Self {
	i.Interactivity().OnBoxedAction(a, f)
	return i.self
}

// OnKeyDown binds a bubble key-down listener (DivElement.OnKeyDown).
func (i *Interactive[Self]) OnKeyDown(f func(*KeyDownEvent, *Window, *App)) Self {
	i.Interactivity().OnKeyDown(f)
	return i.self
}

// CaptureKeyDown binds a capture key-down listener
// (DivElement.CaptureKeyDown).
func (i *Interactive[Self]) CaptureKeyDown(f func(*KeyDownEvent, *Window, *App)) Self {
	i.Interactivity().CaptureKeyDown(f)
	return i.self
}

// OnKeyUp binds a bubble key-up listener (DivElement.OnKeyUp).
func (i *Interactive[Self]) OnKeyUp(f func(*KeyUpEvent, *Window, *App)) Self {
	i.Interactivity().OnKeyUp(f)
	return i.self
}

// CaptureKeyUp binds a capture key-up listener (DivElement.CaptureKeyUp).
func (i *Interactive[Self]) CaptureKeyUp(f func(*KeyUpEvent, *Window, *App)) Self {
	i.Interactivity().CaptureKeyUp(f)
	return i.self
}

// OnModifiersChanged binds a modifiers-changed listener
// (DivElement.OnModifiersChanged).
func (i *Interactive[Self]) OnModifiersChanged(f func(*ModifiersChangedEvent, *Window, *App)) Self {
	i.Interactivity().OnModifiersChanged(f)
	return i.self
}

// OnMouseMove binds a hover-gated bubble mouse-move listener
// (DivElement.OnMouseMove, div.rs:1155). OnMouseMoveAll receives every
// move.
func (i *Interactive[Self]) OnMouseMove(f func(*MouseMoveEvent, *Window, *App)) Self {
	i.Interactivity().OnMouseMove(f)
	return i.self
}

// OnMouseUp binds a hover-gated bubble mouse-up listener for one button
// (DivElement.OnMouseUp, div.rs:1051). OnMouseUpAll receives every mouse
// up.
func (i *Interactive[Self]) OnMouseUp(button MouseButton, f func(*MouseUpEvent, *Window, *App)) Self {
	i.Interactivity().OnMouseUp(button, f)
	return i.self
}

// OnScrollWheel binds a scroll listener (DivElement.OnScrollWheel).
func (i *Interactive[Self]) OnScrollWheel(f func(*ScrollWheelEvent, DispatchPhase, *Hitbox, *Window, *App)) Self {
	i.Interactivity().OnScrollWheel(f)
	return i.self
}

// OnPinch binds a pinch listener (DivElement.OnPinch).
func (i *Interactive[Self]) OnPinch(f func(*PinchEvent, DispatchPhase, *Hitbox, *Window, *App)) Self {
	i.Interactivity().OnPinch(f)
	return i.self
}

// Occlude makes the inner div's hitbox block the mouse
// (DivElement.Occlude).
func (i *Interactive[Self]) Occlude() Self {
	i.Interactivity().Occlude()
	return i.self
}

// TabIndex sets the inner div's tab index (DivElement.TabIndex).
func (i *Interactive[Self]) TabIndex(index int64) Self {
	i.Interactivity().TabIndex(index)
	return i.self
}

// TabGroup makes the inner div a tab group (DivElement.TabGroup).
func (i *Interactive[Self]) TabGroup() Self {
	i.Interactivity().TabGroup()
	return i.self
}

// DebugSelector sets the inner div's test bounds key
// (DivElement.DebugSelector).
func (i *Interactive[Self]) DebugSelector(selector string) Self {
	i.Interactivity().DebugSelector(selector)
	return i.self
}

// ---------------------------------------------------------------------------
// Component: the one-embed mixin
// ---------------------------------------------------------------------------

// Component gives a custom component the whole vocabulary — Styled
// (with FluentBuilder), Parent and Interactive — from one value
// embedding, bound with one BindComponent call. Its shape is the common
// Base component (button.rs:187-205): its own style refinement, its own
// child list, and interactivity delegated to an inner div. Render it by
// refining the inner div with the style and extending it with the
// children:
//
//	return c.Interactivity().Refine(c.Style()).Extend(c.TakeChildren())
type Component[Self any] struct {
	Styled[Self]
	Parent[Self]
	Interactive[Self]
}

// ComponentBase is the binding hook promoted through value embedding.
func (c *Component[Self]) ComponentBase() *Component[Self] { return c }

// BindComponent binds a newly constructed component's embedded
// Component: the style helper with its own storage, the parent helper,
// and the interactive helper delegating to base. It panics on a nil
// owner or div, a missing helper, or an already-bound (or copied bound)
// helper.
func BindComponent[Value any, Self interface {
	*Value
	ComponentBase() *Component[Self]
}](self Self, base *DivElement) Self {
	if self == nil {
		panic("gpui: cannot bind a nil component")
	}
	c := self.ComponentBase()
	if c == nil {
		panic("gpui: component has no Component helper")
	}
	if c.Styled.anchor != nil {
		panic("gpui: Styled is already bound; create a new component instead of rebinding or copying")
	}
	if base == nil {
		panic("gpui: Interactive requires a non-nil inner div")
	}
	c.Styled.bind(self, &c.Styled.own)
	c.Parent.bind(self)
	c.Interactive.bind(self, base)
	return self
}
