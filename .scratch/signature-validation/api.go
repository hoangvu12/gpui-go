// Package gpui is a compile-only model of the selected public signatures.
// It is not the framework. Operation bodies deliberately panic if executed.
package gpui

import (
	"encoding/json"
	"gpui-go/authoring"
)

const fixtureOnly = "signature fixture: no runtime implementation"

type appAccess struct{}
type AppContext interface{ access() appAccess }
type App struct{}

func (*App) access() appAccess { return appAccess{} }

type Context[S any] struct{}

func (*Context[S]) access() appAccess { return appAccess{} }
func (*Context[S]) Notify()           { panic(fixtureOnly) }
func (*Context[S]) Entity() Entity[S] { panic(fixtureOnly) }
func (*Context[S]) Scope() *Scope     { panic(fixtureOnly) }
func (*Context[S]) Listener[E any](f func(*S, *E, *Window, *Context[S])) func(*E, *Window, *App) {
	panic(fixtureOnly)
}
func (*Context[S]) Observe[T any](source Entity[T], f func(*S, Entity[T], *Context[S])) Subscription {
	panic(fixtureOnly)
}

type Scope struct{}

func (*Scope) Child() *Scope { panic(fixtureOnly) }
func (*Scope) Close()        { panic(fixtureOnly) }

type entityKind[S any] struct{}
type Entity[S any] struct{ kind entityKind[S] }
type WeakEntity[S any] struct{ kind entityKind[S] }
type EntityID uint64

func NewEntity[S any](cx AppContext, owner *Scope, build func(*S, *Context[S])) Entity[S] {
	panic(fixtureOnly)
}
func (Entity[S]) RetainInto(owner *Scope) Entity[S]                            { panic(fixtureOnly) }
func (Entity[S]) TransferInto(owner *Scope)                                    { panic(fixtureOnly) }
func (Entity[S]) Release()                                                     { panic(fixtureOnly) }
func (Entity[S]) Downgrade() WeakEntity[S]                                     { panic(fixtureOnly) }
func (WeakEntity[S]) UpgradeInto(owner *Scope) (Entity[S], bool)               { panic(fixtureOnly) }
func (Entity[S]) Read[R any](cx AppContext, f func(*S, *App) R) R              { panic(fixtureOnly) }
func (Entity[S]) Update(cx AppContext, f func(*S, *Context[S]))                { panic(fixtureOnly) }
func (Entity[S]) UpdateWith[R any](cx AppContext, f func(*S, *Context[S]) R) R { panic(fixtureOnly) }
func (Entity[S]) UpdateIn(cx AppContext, w *Window, f func(*S, *Window, *Context[S])) error {
	panic(fixtureOnly)
}
func (Entity[S]) UpdateInWith[R any](cx AppContext, w *Window, f func(*S, *Window, *Context[S]) R) (R, error) {
	panic(fixtureOnly)
}
func (WeakEntity[S]) TryRead[R any](cx AppContext, f func(*S, *App) R) (R, error) { panic(fixtureOnly) }
func (WeakEntity[S]) TryUpdate(cx AppContext, f func(*S, *Context[S])) error      { panic(fixtureOnly) }
func (WeakEntity[S]) TryUpdateWith[R any](cx AppContext, f func(*S, *Context[S]) R) (R, error) {
	panic(fixtureOnly)
}
func (WeakEntity[S]) TryUpdateIn(cx AppContext, w *Window, f func(*S, *Window, *Context[S])) error {
	panic(fixtureOnly)
}
func (WeakEntity[S]) TryUpdateInWith[R any](cx AppContext, w *Window, f func(*S, *Window, *Context[S]) R) (R, error) {
	panic(fixtureOnly)
}

type eventPair[S, P any] struct{}
type Event[S, P any] struct{ pair eventPair[S, P] }

func DefineEvent[S, P any]() Event[S, P]                              { return Event[S, P]{} }
func (Event[S, P]) Emit(cx *Context[S], payload P)                    { panic(fixtureOnly) }
func (Event[S, P]) EmitOn(cx AppContext, source Entity[S], payload P) { panic(fixtureOnly) }
func (Event[S, P]) EmitOwned(cx *Context[S], build func(*Scope) P)    { panic(fixtureOnly) }
func (Event[S, P]) Subscribe[O any](cx *Context[O], source Entity[S], f func(*O, Entity[S], *P, *Context[O])) Subscription {
	panic(fixtureOnly)
}
func (Event[S, P]) SubscribeSelf(cx *Context[S], f func(*S, *P, *Context[S])) Subscription {
	panic(fixtureOnly)
}

type Subscription struct{}

func (Subscription) Cancel()             { panic(fixtureOnly) }
func (Subscription) TransferInto(*Scope) { panic(fixtureOnly) }
func (Subscription) Detach()             { panic(fixtureOnly) }

type FocusHandle struct{}
type DismissEvent struct{}
type Render[S any] interface {
	Render(*Window, *Context[S]) AnyElement
}
type RenderOnce interface {
	RenderOnce(*Window, *App) AnyElement
}
type View interface {
	EntityID() (EntityID, bool)
	RenderOnce(*Window, *App) AnyElement
}
type ManagedView[S any] interface {
	Render[S]
	FocusHandle() FocusHandle
	DismissEvent() Event[S, DismissEvent]
}

func ViewOf[S any, PS interface {
	*S
	Render[S]
}](entity Entity[S]) View { panic(fixtureOnly) }
func ViewOfIn[S any, PS interface {
	*S
	Render[S]
}](owner *Scope, entity Entity[S]) View { panic(fixtureOnly) }
func ManagedViewOf[S any, PS interface {
	*S
	ManagedView[S]
}](entity Entity[S]) View { panic(fixtureOnly) }

type ElementID struct{ value uint64 }
type GlobalElementID struct{}
type InspectorElementID struct{}
type SourceLocation struct {
	File string
	Line int
}
type LayoutID uint64
type Bounds struct{ X, Y, Width, Height float32 }
type Role uint16
type Node struct{}
type A11ySubtreeBuilder struct{}
type Element[L, P any] interface {
	ID() (ElementID, bool)
	SourceLocation() *SourceLocation
	RequestLayout(*GlobalElementID, *InspectorElementID, *Window, *App) (LayoutID, L)
	Prepaint(*GlobalElementID, *InspectorElementID, Bounds, *L, *Window, *App) P
	Paint(*GlobalElementID, *InspectorElementID, Bounds, *L, *P, *Window, *App)
}
type A11yRole interface{ A11yRole() (Role, bool) }
type A11yHidden interface{ IsA11yHidden() bool }
type A11yProperties interface{ WriteA11yInfo(*Node) }
type A11ySyntheticChildren[P any] interface{ A11ySyntheticChildren(*P, *A11ySubtreeBuilder) }

func CustomElement[L, P any](impl Element[L, P]) AnyElement { panic(fixtureOnly) }

type AnyElement struct{ token *struct{} }
type IntoElement interface{ IntoElement() AnyElement }

func TryIntoElement(value any) (AnyElement, error) { panic(fixtureOnly) }
func OwnRecipe(recipe RenderOnce) AnyElement       { panic(fixtureOnly) }
func Empty() AnyElement                            { panic(fixtureOnly) }
func Text(text string) AnyElement                  { panic(fixtureOnly) }

// DivElement preserves the gpui.Div() constructor spelling. A type and
// function cannot both occupy the package-level identifier Div.
type DivElement struct{}
type StyleRefinement = authoring.StyleRefinement

func Div() *DivElement                                                       { panic(fixtureOnly) }
func (d *DivElement) ID(value string) *DivElement                            { panic(fixtureOnly) }
func (d *DivElement) TrackFocus(value FocusHandle) *DivElement               { panic(fixtureOnly) }
func (d *DivElement) KeyContext(value string) *DivElement                    { panic(fixtureOnly) }
func (d *DivElement) Flex() *DivElement                                      { panic(fixtureOnly) }
func (d *DivElement) Gap2() *DivElement                                      { panic(fixtureOnly) }
func (d *DivElement) Refine(value StyleRefinement) *DivElement               { panic(fixtureOnly) }
func (d *DivElement) OnClick(f func(*ClickEvent, *Window, *App)) *DivElement { panic(fixtureOnly) }
func (d *DivElement) OnAction[A any](a Action[A], f func(*A, *Window, *App)) *DivElement {
	panic(fixtureOnly)
}
func (d *DivElement) CaptureAction[A any](a Action[A], f func(*A, *Window, *App)) *DivElement {
	panic(fixtureOnly)
}
func (d *DivElement) OnBoxedAction(a BoxedAction, f func(BoxedAction, *Window, *App)) *DivElement {
	panic(fixtureOnly)
}
func (d *DivElement) Child(value any) *DivElement                             { panic(fixtureOnly) }
func (d *DivElement) Children(values ...any) *DivElement                      { panic(fixtureOnly) }
func (d *DivElement) TryChildren(values ...any) (*DivElement, error)          { panic(fixtureOnly) }
func (d *DivElement) TryChildrenSlice[T any](values []T) (*DivElement, error) { panic(fixtureOnly) }
func (d *DivElement) IntoElement() AnyElement                                 { panic(fixtureOnly) }

type ParentElementTyped[C, Self any] interface {
	Child(C) Self
	Children(...C) Self
}

type actionKind[A any] struct{}
type Action[A any] struct {
	kind       actionKind[A]
	descriptor *struct{}
}
type ActionSpec[A any] struct {
	Name                       string
	Clone                      func(A) A
	Equal                      func(A, A) bool
	FromJSON                   func(json.RawMessage) (A, error)
	Schema                     func() any
	Aliases                    []string
	Deprecation, Documentation string
}
type AnyAction interface{ actionDescriptor() }

func (Action[A]) actionDescriptor() {}

type BoxedAction struct{}

func DefineAction[A any](spec ActionSpec[A]) Action[A]                          { return Action[A]{} }
func DefineUnitAction[A ~struct{}](name string) Action[A]                       { return Action[A]{} }
func (Action[A]) Box(value A) BoxedAction                                       { panic(fixtureOnly) }
func (Action[A]) Clone(value A) A                                               { panic(fixtureOnly) }
func (Action[A]) Equal(a, b A) bool                                             { panic(fixtureOnly) }
func (Action[A]) Name() string                                                  { panic(fixtureOnly) }
func (*App) RegisterActions(actions ...AnyAction)                               { panic(fixtureOnly) }
func (*App) BuildAction(name string, data json.RawMessage) (BoxedAction, error) { panic(fixtureOnly) }

type Window struct{}
type ClickEvent struct{}

func (*Window) Focus(FocusHandle)                                          { panic(fixtureOnly) }
func (*Window) Dispatch[A any](action Action[A], payload A, cx AppContext) { panic(fixtureOnly) }
func (*Window) DispatchBoxed(action BoxedAction, cx AppContext)            { panic(fixtureOnly) }
