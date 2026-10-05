package gpui

// Context is the typed application context issued for one entity access:
// each update, delivery and construction receives a fresh Context. The
// context implements the sealed AppContext interface.
//
// Context.Entity() and the source handles supplied to observer and
// subscriber callbacks are callback-borrowed facades (contract:
// "Borrowing and retained callbacks"): copies share the callback's
// validity token and expire on return. Access is allowed during that
// callback; saving an owned handle requires RetainInto; releasing or
// transferring a borrowed facade is misuse.
type Context[S any] struct {
	app    *App
	entity EntityID
	// valid is the borrow-validity token shared by every facade this
	// context issues; it expires when the owning callback returns.
	valid *borrowValidity
}

// access seals AppContext for *Context[S].
func (cx *Context[S]) access() appAccess { return appAccess{} }

// appRef implements the internal appCarrier.
func (cx *Context[S]) appRef() *App { return cx.app }

// App returns the application this context carries.
func (cx *Context[S]) App() *App { return cx.app }

// Entity returns a callback-borrowed handle to the entity backing this
// context. The handle (and every copy of it) is valid only during the
// callback that received this context; retain it with RetainInto to keep
// it. Releasing or transferring the borrowed facade is misuse.
func (cx *Context[S]) Entity() Entity[S] {
	if cx.valid == nil {
		panic("gpui: context has no borrow validity")
	}
	if !cx.app.entities.isLive(cx.entity) {
		panic("gpui: the entity must be alive to issue a context handle")
	}
	return Entity[S]{
		id:    cx.entity,
		app:   cx.app,
		lease: &entityLease{kind: leaseBorrow, id: cx.entity, app: cx.app, valid: cx.valid},
	}
}

// EntityID returns the identity of the entity backing this context.
func (cx *Context[S]) EntityID() EntityID { return cx.entity }

// Scope returns the entity's dependent-resource scope. It survives as
// long as the entity has independent owners, even if its original
// creating scope closes, and is closed when the entity releases.
func (cx *Context[S]) Scope() *Scope {
	slot := cx.app.entities.slot(cx.entity)
	if slot == nil {
		panic("gpui: context entity has no dependent scope")
	}
	return slot.dependent
}

// Notify tells the framework that this entity changed: observers are
// notified at the next effect flush, coalesced while pending. Mutation
// through Update does not notify implicitly.
func (cx *Context[S]) Notify() {
	cx.app.notify(cx.entity)
}

// Observe registers f to be invoked whenever source notifies. The
// registration defaults to the observer entity's dependent scope (this
// context's entity); the source handle passed to f is callback-borrowed.
//
// New registrations activate through a queued defer. The returned handle
// can Cancel, TransferInto a scope, or Detach to the app registry.
// Ignoring the handle leaves the registration active under its scope
// (Go adaptation: no drop-driven cancellation).
func (cx *Context[S]) Observe[T any](source Entity[T], f func(*S, Entity[T], *Context[S])) Subscription {
	if f == nil {
		panic("gpui: Observe called with a nil callback")
	}
	lease := entityLeaseOf(source, "Observe")
	if lease.app != cx.app {
		panic("gpui: Observe source entity belongs to a different application")
	}
	slot := cx.app.entities.slot(source.id)
	if slot == nil {
		panic("gpui: Observe source entity references a reclaimed identity")
	}
	observer := cx.entity
	sourceID := source.id
	invoke := func(a *App, _ any) {
		withEntityUpdate[S](a, observer, func(state *S, ocx *Context[S]) {
			f(state, Entity[T]{
				id:    sourceID,
				app:   a,
				lease: &entityLease{kind: leaseBorrow, id: sourceID, app: a, valid: ocx.valid},
			}, ocx)
		})
	}
	reg := cx.app.registerSub(slot.ensureObservers(), observer, nil, cx.dependentScope(), invoke)
	reg.activateViaDefer(cx.app)
	return Subscription{app: cx.app, reg: reg}
}

// OnRelease registers f to be invoked with the still-available state when
// this entity releases, after its observers and listeners are removed.
// Registrations activate immediately and default to the app root scope:
// the entity's own dependent scope is cancelled before the callbacks run,
// so owning them there would suppress the hooks.
func (cx *Context[S]) OnRelease(f func(*S, *App)) Subscription {
	if f == nil {
		panic("gpui: OnRelease called with a nil callback")
	}
	slot := cx.app.entities.slot(cx.entity)
	if slot == nil {
		panic("gpui: OnRelease context entity references a reclaimed identity")
	}
	reg := cx.app.registerSub(slot.ensureReleaseListeners(), 0, nil, cx.app.rootScope, func(a *App, payload any) {
		f(payload.(*S), a)
	})
	reg.active = true
	reg.once = true
	return Subscription{app: cx.app, reg: reg}
}

// Defer schedules f to run at the end of the current effect cycle with a
// fresh context for this entity. The deferred callback runs only if the
// entity is still live when it executes.
func (cx *Context[S]) Defer(f func(*S, *Context[S])) {
	if f == nil {
		panic("gpui: Defer called with a nil callback")
	}
	entity := cx.entity
	cx.app.Defer(func(a *App) {
		withEntityUpdate[S](a, entity, f)
	})
}

// Listener adapts an entity-state callback into the plain
// func(*E, *Window, *App) shape used by window-facing event handlers.
// The adapter holds a weak identity: it upgrades at delivery, supplies a
// fresh context, and is a no-op once the entity releases (contract:
// "Keep capture strength explicit. Listener uses weak identity").
func (cx *Context[S]) Listener[E any](f func(*S, *E, *Window, *Context[S])) func(*E, *Window, *App) {
	if f == nil {
		panic("gpui: Listener called with a nil callback")
	}
	weak := WeakEntity[S]{id: cx.entity, app: cx.app}
	return func(e *E, w *Window, app *App) {
		if app == nil {
			panic("gpui: Listener adapter invoked without an application")
		}
		if app != weak.app {
			panic("gpui: Listener adapter invoked with a foreign application")
		}
		withEntityUpdate[S](app, weak.id, func(state *S, lcx *Context[S]) {
			f(state, e, w, lcx)
		})
	}
}

// dependentScope returns the observer entity's dependent-resource scope,
// the default owner for registrations made through this context.
func (cx *Context[S]) dependentScope() *Scope {
	slot := cx.app.entities.slot(cx.entity)
	if slot == nil {
		panic("gpui: context entity has no dependent scope")
	}
	return slot.dependent
}
