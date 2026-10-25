package gpui_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// card is a third-party component with the Base shape (button.rs:187-205):
// its own style and children, interactivity delegated to an inner div,
// the whole vocabulary from one embedded Component.
type card struct {
	gpui.Component[*card]
	title    string
	disabled bool
}

func newCard(id string) *card {
	return gpui.BindComponent(&card{}, gpui.Div().Flex().ID(id))
}

func (c *card) Title(value string) *card  { c.title = value; return c }
func (c *card) Disabled(value bool) *card { c.disabled = value; return c }

// IntoElement renders the card: the inner div refined by the card's
// style and extended with its children.
func (c *card) IntoElement() gpui.AnyElement {
	return c.Interactivity().Refine(c.Style()).Child(c.title).Extend(c.TakeChildren()).IntoElement()
}

// badge uses the individual mixins with delegated style storage (the
// `fn style(&mut self) { self.base.style() }` shape).
type badge struct {
	gpui.Styled[*badge]
	gpui.Parent[*badge]
	base *gpui.DivElement
}

func newBadge() *badge {
	b := &badge{base: gpui.Div().Flex()}
	gpui.BindStyledTo(b, b.base.StyleMut())
	return gpui.BindParent(b)
}

func (b *badge) IntoElement() gpui.AnyElement {
	return b.base.Extend(b.TakeChildren()).IntoElement()
}

// TestComponentGetsTheWholeVocabularyFromOneMixin chains shared styling,
// fluent, parent, interactive and component methods in mixed order and
// draws the result.
func TestComponentGetsTheWholeVocabularyFromOneMixin(t *testing.T) {
	var c *card = newCard("card").
		W(gpui.Px(120)).Title("Hello").H(gpui.Px(50)).Bg(red).Disabled(true).RoundedLg().
		When(true, func(c *card) *card { return c.Border1().BorderColor(blue) }).
		When(false, func(c *card) *card { return c.Bg(green) }).
		Child(gpui.Div().Flex().Size(gpui.Px(8)).Bg(green)).
		OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {}).
		DebugSelector("card").P1()
	if !c.disabled || c.title != "Hello" || c.ChildCount() != 1 {
		t.Fatal("chain lost the component's own state")
	}
	if v, _ := c.Style().Background.Get(); v != gpui.SolidBackground(red) {
		t.Errorf("When(false) applied: %+v", v)
	}
	window, scene := drawStyled(t, gpui.Div().Flex().Child(c).IntoElement())
	b, ok := gpui.WindowDebugBound(window, "card")
	if !ok || b.Size != (gpui.Size{Width: 120, Height: 50}) {
		t.Fatalf("card bounds = %+v, %v", b, ok)
	}
	quads := quadsOf(t, scene)
	if len(quads) < 3 || quads[0].Background != painted(red) || quads[0].CornerRadii != corners(8) {
		t.Fatalf("card background = %+v", quads)
	}
	// The badge delegates its style storage into the inner div.
	bd := newBadge().Bg(blue).Size4().Child("x")
	if v, _ := bd.base.Style().Background.Get(); v != gpui.SolidBackground(blue) {
		t.Errorf("delegated style did not reach the inner div: %+v", v)
	}
	if _, scene := drawStyled(t, bd.IntoElement()); len(quadsOf(t, scene)) != 1 {
		t.Error("badge did not paint its background")
	}
}

// TestFluentBuilder ports util.rs:11-61 FluentBuilder.
func TestFluentBuilder(t *testing.T) {
	value := 3
	var missing *int
	d := gpui.Div().Flex().
		WhenSome(&value, func(d *gpui.DivElement, v int) *gpui.DivElement { return d.Opacity(float32(v) / 10) }).
		WhenSome(missing, func(d *gpui.DivElement, v int) *gpui.DivElement { return d.Hidden() }).
		WhenNone(missing, func(d *gpui.DivElement) *gpui.DivElement { return d.Flex() }).
		WhenNone(&value, func(d *gpui.DivElement) *gpui.DivElement { return d.Grid() }).
		WhenElse(false, func(d *gpui.DivElement) *gpui.DivElement { return d.Block() }, func(d *gpui.DivElement) *gpui.DivElement { return d.FlexCol() })
	s := d.Style()
	if v, _ := s.Opacity.Get(); v != 0.3 {
		t.Errorf("when_some = %v", v)
	}
	if v, _ := s.Display.Get(); v != gpui.DisplayFlex {
		t.Errorf("when_none/when_some(None) display = %v", v)
	}
	if v, _ := s.FlexDirection.Get(); v != gpui.FlexDirectionColumn {
		t.Errorf("when_else = %v", v)
	}
	name := gpui.Div().Flex().Map(func(d *gpui.DivElement) string {
		v, _ := d.Style().Display.Get()
		return v.String()
	})
	if name != "Flex" {
		t.Errorf("map = %q", name)
	}
}

// TestComponentBindingMisuseFailsClearly checks the mixins' binding
// rules (research/09: nil, unbound, rebinding, copies).
func TestComponentBindingMisuseFailsClearly(t *testing.T) {
	wantPanic := func(message string, f func()) {
		t.Helper()
		defer func() {
			if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), message) {
				t.Errorf("panic = %v; want %q", r, message)
			}
		}()
		f()
	}
	var nilCard *card
	wantPanic("nil component", func() { gpui.BindComponent(nilCard, gpui.Div().Flex()) })
	wantPanic("non-nil inner div", func() { gpui.BindComponent(&card{}, nil) })
	var zero card
	wantPanic("unbound Styled", func() { zero.Flex() })
	wantPanic("unbound Parent", func() { zero.Child("x") })
	wantPanic("unbound Interactive", func() { zero.ID("x") })
	c := newCard("c")
	wantPanic("already bound", func() { gpui.BindComponent(c, gpui.Div().Flex()) })
	copied := *c
	wantPanic("copied bound component", func() { copied.Flex() })
	wantPanic("copied bound component", func() { copied.Child("x") })
	wantPanic("copied bound component", func() { copied.OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {}) })
	wantPanic("one or two lengths", func() { gpui.Div().Flex().Size(gpui.Px(1), gpui.Px(2), gpui.Px(3)) })
}

// TestDivIDAcceptsStringsAndElementIDs checks DivElement.ID's
// `impl Into<ElementId>` port: a string is ElementId::Name, any
// ElementID (e.g. a named integer) is used as is.
func TestDivIDAcceptsStringsAndElementIDs(t *testing.T) {
	if id, ok := gpui.Div().Flex().ID("row").IntoElement().ID(); !ok || id != gpui.NameElementID("row") {
		t.Errorf("string id = %+v, %v", id, ok)
	}
	want := gpui.NamedIntegerElementID("row", 3)
	if id, ok := gpui.Div().Flex().ID(want).IntoElement().ID(); !ok || id != want {
		t.Errorf("element id = %+v, %v", id, ok)
	}
	if id, ok := newCard("x").ID(want).IntoElement().ID(); !ok || id != want {
		t.Errorf("component id = %+v, %v", id, ok)
	}
}
