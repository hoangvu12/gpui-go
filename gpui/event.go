package gpui

import "reflect"

// eventPair ties an event descriptor to its declared source/payload type
// pair. It is unexported, so Event[S, P] values of different pairs cannot
// be converted into each other, mirroring the Rust coherence restriction
// as closely as Go allows.
type eventPair[S, P any] struct{}

// Event is a typed event descriptor for payload P emitted by entities of
// state S. Its zero value is valid and stateless: declarations are not
// authority tokens, and repeated declarations for the same pair
// communicate.
//
// Dispatch is keyed by entity identity and the declared payload type,
// never the declaration address or the dynamic member of an interface
// payload. The typed envelope preserves nil interface payloads.
type Event[S, P any] struct {
	pair eventPair[S, P]
}

// DefineEvent declares the event descriptor for the source/payload pair.
func DefineEvent[S, P any]() Event[S, P] { return Event[S, P]{} }

// payloadBox preserves the declared payload type through effect storage,
// including nil interface payloads that a bare `any` would erase.
type payloadBox[P any] struct{ value P }

// Emit queues an occurrence of this event from the context's entity.
// Each occurrence is queued separately (FIFO delivery) and neither
// implies notify nor retains the source for routing. The payload value P
// is copied; reachable mutable data stays frozen until the effect cycle
// retires its payload storage, and subscribers must treat it as
// read-only.
func (e Event[S, P]) Emit(cx *Context[S], payload P) {
	if cx == nil {
		panic("gpui: Emit called with a nil context")
	}
	cx.app.pushEffect(effect{
		kind:        effEmit,
		emitter:     cx.entity,
		payloadType: reflect.TypeFor[P](),
		payload:     payloadBox[P]{value: payload},
	})
}

// EmitOn queues an occurrence of this event from source through an
// application context (the app-context emission path). The same FIFO and
// payload rules as Emit apply.
func (e Event[S, P]) EmitOn(cx AppContext, source Entity[S], payload P) {
	app := appFrom(cx)
	lease := entityLeaseOf(source, "EmitOn")
	if lease.app != app {
		panic("gpui: EmitOn source entity belongs to a different application")
	}
	app.pushEffect(effect{
		kind:        effEmit,
		emitter:     source.id,
		payloadType: reflect.TypeFor[P](),
		payload:     payloadBox[P]{value: payload},
	})
}

// EmitOwned queues an occurrence whose payload carries independent owned
// dependencies: build receives a payload scope and explicitly retains
// entities into it. It has the same source/payload typing and dispatch
// identity as Emit.
//
// The payload scope lasts until the entire effect cycle drains (the event
// arena is retired in emission order at the tail); if a factory panic
// occurs, partial ownership is closed and nothing is enqueued. If arena
// retirement releases the last lease of an entity, a subsequent
// foreground flush processes that release (the documented Go liveness
// adaptation).
func (e Event[S, P]) EmitOwned(cx *Context[S], build func(payloadScope *Scope) P) {
	if cx == nil {
		panic("gpui: EmitOwned called with a nil context")
	}
	if build == nil {
		panic("gpui: EmitOwned called with a nil payload factory")
	}
	app := cx.app
	scope := newScope(app, nil) // app-owned arena scope, not a user child
	var payload P
	func() {
		defer func() {
			if r := recover(); r != nil {
				// Factory failure closes partial ownership and enqueues
				// nothing; the panic propagates.
				closeScopeTree(scope, nil)
				panic(r)
			}
		}()
		payload = build(scope)
	}()
	app.arena = append(app.arena, scope)
	app.pushEffect(effect{
		kind:        effEmit,
		emitter:     cx.entity,
		payloadType: reflect.TypeFor[P](),
		payload:     payloadBox[P]{value: payload},
	})
}

// Subscribe registers f for occurrences of this event from source,
// delivered with a fresh context for the observer. The registration
// defaults to the observer entity's dependent scope; the source handle
// passed to f is callback-borrowed. New registrations activate through a
// queued defer and are not inserted into an active traversal.
func (e Event[S, P]) Subscribe[O any](cx *Context[O], source Entity[S], f func(*O, Entity[S], *P, *Context[O])) Subscription {
	if cx == nil {
		panic("gpui: Subscribe called with a nil context")
	}
	if f == nil {
		panic("gpui: Subscribe called with a nil callback")
	}
	lease := entityLeaseOf(source, "Subscribe")
	if lease.app != cx.app {
		panic("gpui: Subscribe source entity belongs to a different application")
	}
	slot := cx.app.entities.slot(source.id)
	if slot == nil {
		panic("gpui: Subscribe source entity references a reclaimed identity")
	}
	observer := cx.entity
	sourceID := source.id
	invoke := func(a *App, payload any) {
		box := payload.(payloadBox[P])
		withEntityUpdate[O](a, observer, func(state *O, ocx *Context[O]) {
			f(state, Entity[S]{
				id:    sourceID,
				app:   a,
				lease: &entityLease{kind: leaseBorrow, id: sourceID, app: a, valid: ocx.valid},
			}, &box.value, ocx)
		})
	}
	reg := cx.app.registerSub(slot.ensureListeners(), observer, reflect.TypeFor[P](), cx.dependentScope(), invoke)
	reg.activateViaDefer(cx.app)
	return Subscription{app: cx.app, reg: reg}
}

// SubscribeSelf registers f for occurrences of this event emitted by the
// context's own entity.
func (e Event[S, P]) SubscribeSelf(cx *Context[S], f func(*S, *P, *Context[S])) Subscription {
	if cx == nil {
		panic("gpui: SubscribeSelf called with a nil context")
	}
	if f == nil {
		panic("gpui: SubscribeSelf called with a nil callback")
	}
	source := cx.Entity()
	return e.Subscribe(cx, source, func(state *S, _ Entity[S], payload *P, scx *Context[S]) {
		f(state, payload, scx)
	})
}

// Subscription is the cancellation handle for one registration. Handle
// copies alias one cancellation token.
//
// Go adaptation (contract: "Subscriptions and tasks"): discarding a
// subscription variable does not cancel anything. The scope retains the
// registration until the scope closes; the reference's drop-driven
// cancellation cannot be reproduced without GC hooks and is not claimed.
type Subscription struct {
	app *App
	reg *subReg
}

// Cancel cancels the registration. It is idempotent and safe on a zero
// Subscription.
func (s Subscription) Cancel() {
	if s.reg == nil {
		return
	}
	s.reg.cancel()
}

// TransferInto changes the registration's cancellation owner to scope.
func (s Subscription) TransferInto(scope *Scope) {
	if s.reg == nil {
		panic("gpui: TransferInto on a zero subscription")
	}
	s.reg.transferInto(scope)
}

// Detach transfers the registration to the app-owned registration
// registry until cancellation or endpoint release. Detach does not retain
// either endpoint: the endpoints stay weak.
func (s Subscription) Detach() {
	if s.reg == nil {
		panic("gpui: Detach on a zero subscription")
	}
	s.reg.detach()
}

// subReg is one registration in a subscriber set: an observer, event
// listener, release listener, global observer or new-entity observer.
type subReg struct {
	id       uint64 // global registration order
	app      *App
	endpoint EntityID // observer entity; 0 for endpoint-free registrations
	key      reflect.Type
	owner    *Scope
	detached bool
	once     bool
	active   bool
	dropped  bool
	invoke   func(a *App, payload any)
}

// cancel idempotently cancels the registration and removes it from its
// bookkeeping (set membership is dropped by the owner set's traversal).
func (reg *subReg) cancel() {
	if reg.dropped {
		return
	}
	reg.dropped = true
	if reg.owner != nil {
		reg.owner.removeSub(reg)
	}
	if reg.detached {
		for i, candidate := range reg.app.detachedSubs {
			if candidate == reg {
				reg.app.detachedSubs = append(reg.app.detachedSubs[:i], reg.app.detachedSubs[i+1:]...)
				break
			}
		}
	}
}

// transferInto moves the cancellation ownership to another live scope.
func (reg *subReg) transferInto(scope *Scope) {
	if scope == nil {
		panic("gpui: Subscription.TransferInto requires a non-nil owner scope")
	}
	if scope.app != reg.app {
		panic("gpui: Subscription.TransferInto owner scope belongs to a different application")
	}
	if reg.dropped {
		return
	}
	scope.requireOpen()
	if reg.detached {
		for i, candidate := range reg.app.detachedSubs {
			if candidate == reg {
				reg.app.detachedSubs = append(reg.app.detachedSubs[:i], reg.app.detachedSubs[i+1:]...)
				break
			}
		}
		reg.detached = false
	}
	if reg.owner != nil {
		reg.owner.removeSub(reg)
	}
	reg.owner = scope
	scope.addSub(reg)
}

// detach moves the registration to the app registry. Detach does not
// retain either endpoint.
func (reg *subReg) detach() {
	if reg.detached || reg.dropped {
		return
	}
	if reg.owner != nil {
		reg.owner.removeSub(reg)
	}
	reg.detached = true
	reg.app.detachedSubs = append(reg.app.detachedSubs, reg)
}

// activateViaDefer activates the registration through a queued defer
// (contract: "New registrations activate through a queued defer").
func (reg *subReg) activateViaDefer(a *App) {
	a.Defer(func(*App) {
		if !reg.dropped {
			reg.active = true
		}
	})
}

// subscriberSet is one ordered registration list. Insertion appends;
// registration order is the global registration id order.
type subscriberSet struct {
	subs []*subReg
}

func newSubscriberSet() *subscriberSet { return &subscriberSet{} }

// insert appends one registration.
func (set *subscriberSet) insert(reg *subReg) {
	set.subs = append(set.subs, reg)
}

// ensureObservers lazily creates the observer set.
func (s *entitySlot) ensureObservers() *subscriberSet {
	if s.observers == nil {
		s.observers = newSubscriberSet()
	}
	return s.observers
}

// ensureListeners lazily creates the listener set.
func (s *entitySlot) ensureListeners() *subscriberSet {
	if s.listeners == nil {
		s.listeners = newSubscriberSet()
	}
	return s.listeners
}

// ensureReleaseListeners lazily creates the release-listener set.
func (s *entitySlot) ensureReleaseListeners() *subscriberSet {
	if s.releaseListeners == nil {
		s.releaseListeners = newSubscriberSet()
	}
	return s.releaseListeners
}
