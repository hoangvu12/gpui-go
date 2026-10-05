// These functions are typechecked but never invoked. They are not runtime tests.
package consumer

import (
	"encoding/json"
	gpui "gpui-go"
	"gpui-go/authoring"
	"gpui-go/example"
	"gpui-go/shared"
)

type Observer struct{ Last int }

func (o *Observer) Changed(source gpui.Entity[example.Counter], p *shared.Payload, cx *gpui.Context[Observer]) {
	o.Last = p.Value
}
func (o *Observer) Observed(source gpui.Entity[example.Counter], cx *gpui.Context[Observer]) {}

type AccessWrapper struct{ gpui.AppContext }

var _ gpui.AppContext = AccessWrapper{}
var _ gpui.AppContext = (*gpui.App)(nil)
var _ gpui.AppContext = (*gpui.Context[Observer])(nil)

func Access(app *gpui.App, w *gpui.Window, e gpui.Entity[example.Counter], owner *gpui.Scope) {
	var n int = e.Read(app, func(*example.Counter, *gpui.App) int { return 1 })
	_ = e.Read[string](AccessWrapper{app}, func(*example.Counter, *gpui.App) string { return "ok" })
	e.Update(app, func(*example.Counter, *gpui.Context[example.Counter]) {})
	var text string = e.UpdateWith(app, func(*example.Counter, *gpui.Context[example.Counter]) string { return "ok" })
	_ = e.UpdateIn(app, w, func(*example.Counter, *gpui.Window, *gpui.Context[example.Counter]) {})
	_, _ = e.UpdateInWith(app, w, func(*example.Counter, *gpui.Window, *gpui.Context[example.Counter]) int { return n })
	weak := e.Downgrade()
	_, _ = weak.TryRead(app, func(*example.Counter, *gpui.App) int { return 1 })
	_ = weak.TryUpdate(app, func(*example.Counter, *gpui.Context[example.Counter]) {})
	_, _ = weak.TryUpdateWith(app, func(*example.Counter, *gpui.Context[example.Counter]) string { return text })
	_ = weak.TryUpdateIn(app, w, func(*example.Counter, *gpui.Window, *gpui.Context[example.Counter]) {})
	_, _ = weak.TryUpdateInWith(app, w, func(*example.Counter, *gpui.Window, *gpui.Context[example.Counter]) int { return 2 })
	retained := e.RetainInto(owner)
	retained.TransferInto(owner.Child())
	retained.Release()
	if upgraded, ok := weak.UpgradeInto(owner); ok {
		upgraded.Release()
	}
	_ = gpui.NewEntity(app, owner, func(*Observer, *gpui.Context[Observer]) {})
}

func Events(app *gpui.App, source gpui.Entity[example.Counter], cx *gpui.Context[example.Counter], observer *gpui.Context[Observer], owner *gpui.Scope) {
	event := gpui.DefineEvent[example.Counter, shared.Payload]()
	var zero gpui.Event[example.Counter, shared.Payload]
	_ = zero == event
	_ = event == gpui.DefineEvent[example.Counter, shared.PayloadAlias]()
	event.Emit(cx, shared.Payload{Value: 3})
	event.EmitOn(app, source, shared.Payload{})
	sub := event.Subscribe(observer, source, (*Observer).Changed)
	_ = event.Subscribe[Observer](observer, source, (*Observer).Changed)
	sub.TransferInto(owner)
	sub.Cancel()
	sub.Detach()
	_ = event.SubscribeSelf(cx, func(*example.Counter, *shared.Payload, *gpui.Context[example.Counter]) {})
	_ = observer.Observe(source, (*Observer).Observed)
	_ = observer.Listener(func(*Observer, *shared.Payload, *gpui.Window, *gpui.Context[Observer]) {})
	_ = observer.Listener[shared.Payload](func(*Observer, *shared.Payload, *gpui.Window, *gpui.Context[Observer]) {})
	gpui.DefineEvent[example.Counter, shared.InterfacePayload]().Emit(cx, nil)
	gpui.DefineEvent[example.Counter, shared.InterfacePayload]().SubscribeSelf(cx, func(*example.Counter, *shared.InterfacePayload, *gpui.Context[example.Counter]) {})
	// Non-comparable payloads must not make the declaration non-comparable.
	_ = gpui.DefineEvent[example.Counter, []int]() == gpui.DefineEvent[example.Counter, []int]()
	gpui.DefineEvent[example.Counter, gpui.Entity[Observer]]().EmitOwned(cx, func(s *gpui.Scope) gpui.Entity[Observer] {
		return observer.Entity().RetainInto(s)
	})
	_ = cx.Entity().RetainInto(cx.Scope()) // typechecks; self-cycle prohibition is runtime policy.
}

type Paste struct{ Bytes []byte }
type Unit struct{}
type UnitAlias = Unit

var unit = gpui.DefineUnitAction[Unit]("fixture::Unit")
var paste = gpui.DefineAction(gpui.ActionSpec[Paste]{
	Name:     "fixture::Paste",
	Clone:    func(p Paste) Paste { return Paste{append([]byte(nil), p.Bytes...)} },
	Equal:    func(a, b Paste) bool { return string(a.Bytes) == string(b.Bytes) },
	FromJSON: func(data json.RawMessage) (Paste, error) { var p Paste; err := json.Unmarshal(data, &p); return p, err },
	Schema:   func() any { return map[string]any{"type": "object"} },
	Aliases:  []string{"fixture::OldPaste"},
})

func Actions(app *gpui.App, w *gpui.Window, cx *gpui.Context[Observer]) {
	// Typed operations compile without any named registration.
	gpui.Div().OnAction(unit, cx.Listener(func(*Observer, *Unit, *gpui.Window, *gpui.Context[Observer]) {})).
		CaptureAction(paste, func(*Paste, *gpui.Window, *gpui.App) {})
	w.Dispatch(unit, UnitAlias{}, cx)
	w.Dispatch(paste, Paste{}, app)
	bound := paste.Box(Paste{[]byte("bound")})
	gpui.Div().OnBoxedAction(bound, func(gpui.BoxedAction, *gpui.Window, *gpui.App) {})
	w.DispatchBoxed(bound, cx)
	app.RegisterActions(unit, paste)
	_, _ = app.BuildAction(paste.Name(), json.RawMessage(`{}`))
	_ = paste.Equal(paste.Clone(Paste{}), Paste{})
}

type Screen struct{}

func (*Screen) Render(*gpui.Window, *gpui.Context[Screen]) gpui.AnyElement { return gpui.AnyElement{} }
func (*Screen) FocusHandle() gpui.FocusHandle                              { return gpui.FocusHandle{} }
func (*Screen) DismissEvent() gpui.Event[Screen, gpui.DismissEvent] {
	return gpui.Event[Screen, gpui.DismissEvent]{}
}

var _ gpui.ManagedView[Screen] = (*Screen)(nil)

type PropertiesView struct {
	Caption string
	Backing gpui.EntityID
}

func (v PropertiesView) EntityID() (gpui.EntityID, bool)                  { return v.Backing, true }
func (PropertiesView) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement { return gpui.AnyElement{} }

var _ gpui.View = PropertiesView{}

func Views(e gpui.Entity[example.Counter], screen gpui.Entity[Screen], owner *gpui.Scope) {
	_ = gpui.ViewOf(e)
	_ = gpui.ViewOf[example.Counter](e)
	_ = gpui.ViewOf[example.Counter, *example.Counter](e)
	_ = gpui.ViewOfIn(owner, e)
	_ = gpui.ManagedViewOf(screen)
	_ = gpui.ManagedViewOf[Screen, *Screen](screen)
	gpui.Div().Child(PropertiesView{Caption: "hello"})
}

type LayoutState struct{ Width int }
type PaintState struct{ Hitbox uint64 }
type Mark struct{}

func (*Mark) ID() (gpui.ElementID, bool)           { return gpui.ElementID{}, true }
func (*Mark) SourceLocation() *gpui.SourceLocation { return nil }
func (*Mark) RequestLayout(*gpui.GlobalElementID, *gpui.InspectorElementID, *gpui.Window, *gpui.App) (gpui.LayoutID, LayoutState) {
	return 0, LayoutState{}
}
func (*Mark) Prepaint(*gpui.GlobalElementID, *gpui.InspectorElementID, gpui.Bounds, *LayoutState, *gpui.Window, *gpui.App) PaintState {
	return PaintState{}
}
func (*Mark) Paint(*gpui.GlobalElementID, *gpui.InspectorElementID, gpui.Bounds, *LayoutState, *PaintState, *gpui.Window, *gpui.App) {
}
func (*Mark) A11yRole() (gpui.Role, bool)                                 { return 0, true }
func (*Mark) IsA11yHidden() bool                                          { return false }
func (*Mark) WriteA11yInfo(*gpui.Node)                                    {}
func (*Mark) A11ySyntheticChildren(*PaintState, *gpui.A11ySubtreeBuilder) {}

var _ gpui.Element[LayoutState, PaintState] = (*Mark)(nil)
var _ gpui.A11yRole = (*Mark)(nil)
var _ gpui.A11yHidden = (*Mark)(nil)
var _ gpui.A11yProperties = (*Mark)(nil)
var _ gpui.A11ySyntheticChildren[PaintState] = (*Mark)(nil)

type Converted struct{}

func (Converted) IntoElement() gpui.AnyElement { return gpui.AnyElement{} }

type RestrictedParent struct{}

func (p *RestrictedParent) Child(child *example.CountLabel) *RestrictedParent          { return p }
func (p *RestrictedParent) Children(children ...*example.CountLabel) *RestrictedParent { return p }

var _ gpui.ParentElementTyped[*example.CountLabel, *RestrictedParent] = (*RestrictedParent)(nil)

func Elements() {
	d := gpui.Div().Child("hello").Child(Converted{}).Child(gpui.Text("world"))
	d.Child(gpui.CustomElement[LayoutState, PaintState](&Mark{}))
	d.Child(gpui.CustomElement(&Mark{}))
	d.Children("text", gpui.Empty(), gpui.OwnRecipe(example.NewCountLabel(3)))
	_, _ = d.TryChildrenSlice([]*example.CountLabel{example.NewCountLabel(1)})
	_, _ = d.TryChildrenSlice[string]([]string{"one", "two"})
	_, _ = d.TryChildren(nil, 42) // accepted at compile time, rejected by future runtime.
	_, _ = gpui.TryIntoElement(Converted{})
	var style gpui.StyleRefinement = authoring.StyleRefinement{}
	d.Refine(style)
	new(RestrictedParent).Children(example.NewCountLabel(4), example.NewCountLabel(5))
}
