package gpui

import (
	"errors"
	"fmt"
	"reflect"
)

// ErrEntityReleased reports access to an entity whose logical lease count
// reached zero. Logical zero is irreversible: a weak handle never
// upgrades again (contract: "Logical ownership").
var ErrEntityReleased = errors.New("gpui: entity released")

// ErrNoWindow reports an unavailable or absent window (Go adaptation of
// the authoring contract's window-aware access errors; nil/unavailable
// window is ErrNoWindow, closed window is ErrWindowClosed).
var ErrNoWindow = errors.New("gpui: no window")

// ErrWindowClosed reports access through a closed window. Real window
// lifetime tracking arrives with the window tickets; in this slice a
// non-nil *Window is treated as open and only nil windows report
// ErrNoWindow. The window lease (WindowHandle) reuses this error: value
// queries through a lease whose window the host already destroyed fail
// with it.
var ErrWindowClosed = errors.New("gpui: window closed")

// EntityID is the generation-stamped identity of an entity: the low 32
// bits hold the slot index, the high 32 bits the slot generation. The
// zero value is invalid: generation starts at one and is bumped every
// time a slot is reclaimed, so a released identity is never reused, even
// after a failed construction.
type EntityID uint64

// SlotIndex returns the slot index portion of the identity.
func (id EntityID) SlotIndex() uint32 { return uint32(uint64(id) & 0xFFFFFFFF) }

// Generation returns the generation portion of the identity.
func (id EntityID) Generation() uint32 { return uint32(uint64(id) >> 32) }

// makeEntityID packs slot index and generation into an identity.
func makeEntityID(slot, generation uint32) EntityID {
	return EntityID(uint64(generation)<<32 | uint64(slot))
}

// entitySlot is one live slot of the entity map. All slot state is owned
// by the application's dispatcher goroutine.
type entitySlot struct {
	state any // *S once constructed
	// count is the live logical lease count. Reaching zero is
	// irreversible: the identity joins the release queue (the
	// Live -> ReleaseQueued -> Releasing -> Released transition of the
	// ownership contract) and can never be upgraded again.
	count int
	// readers and writer implement the RefCell-style access guards:
	// reads nest, writes are exclusive.
	readers int
	writer  bool

	// observers holds Notify registrations for this entity.
	observers *subscriberSet
	// listeners holds event registrations, keyed by payload type.
	listeners *subscriberSet
	// releaseListeners holds OnRelease registrations.
	releaseListeners *subscriberSet
	// dependent is the entity's dependent-resource scope, closed when the
	// entity releases.
	dependent *Scope
}

// takeObservers removes and returns the observer registrations.
func (s *entitySlot) takeObservers() []*subReg {
	return s.takeSet(&s.observers)
}

// takeListeners removes and returns the event-listener registrations.
func (s *entitySlot) takeListeners() []*subReg {
	return s.takeSet(&s.listeners)
}

// takeReleaseListeners removes and returns the release registrations.
func (s *entitySlot) takeReleaseListeners() []*subReg {
	return s.takeSet(&s.releaseListeners)
}

// takeSet drains one subscriber set.
func (s *entitySlot) takeSet(set **subscriberSet) []*subReg {
	if *set == nil {
		return nil
	}
	subs := (*set).subs
	(*set).subs = nil
	return subs
}

// entityMap is the app's entity storage: slots indexed by slot index,
// generations per slot, and the FIFO queue of identities whose lease
// count reached zero. Storage uses plain Go slices with no external
// dependencies.
type entityMap struct {
	slots   []*entitySlot
	gens    []uint32
	free    []uint32
	dropped []EntityID
}

func newEntityMap() *entityMap { return &entityMap{} }

// newSlot reserves a slot and returns its generation-stamped identity.
func (m *entityMap) newSlot() (EntityID, *entitySlot) {
	var idx uint32
	if len(m.free) > 0 {
		idx = m.free[len(m.free)-1]
		m.free = m.free[:len(m.free)-1]
	} else {
		idx = uint32(len(m.slots))
		m.slots = append(m.slots, nil)
		m.gens = append(m.gens, 0)
	}
	m.gens[idx]++ // generation 1+ so the zero identity is invalid
	slot := &entitySlot{}
	m.slots[idx] = slot
	return makeEntityID(idx, m.gens[idx]), slot
}

// slot resolves the slot for an identity, or nil when the identity is
// unknown or its generation no longer matches (released and reclaimed).
func (m *entityMap) slot(id EntityID) *entitySlot {
	idx := id.SlotIndex()
	if uint64(idx) >= uint64(len(m.slots)) {
		return nil
	}
	slot := m.slots[idx]
	if slot == nil || m.gens[idx] != id.Generation() {
		return nil
	}
	return slot
}

// isLive reports whether the identity has a live logical lease.
func (m *entityMap) isLive(id EntityID) bool {
	slot := m.slot(id)
	return slot != nil && slot.count > 0
}

// addLease increments the logical lease count of a live identity.
func (m *entityMap) addLease(id EntityID) {
	if slot := m.slot(id); slot != nil {
		slot.count++
	}
}

// dropLease decrements the logical lease count. Reaching zero is
// irreversible: the identity is enqueued for release at the next
// effect-flush boundary.
func (m *entityMap) dropLease(id EntityID) {
	slot := m.slot(id)
	if slot == nil || slot.count <= 0 {
		return
	}
	slot.count--
	if slot.count == 0 {
		m.dropped = append(m.dropped, id)
	}
}

// takeDropped drains the release queue in enqueue order.
func (m *entityMap) takeDropped() []EntityID {
	dropped := m.dropped
	m.dropped = nil
	return dropped
}

// pendingDrops reports the number of identities waiting for release.
func (m *entityMap) pendingDrops() int { return len(m.dropped) }

// forEachLive visits every logically live entity in slot order, for
// diagnostics.
func (m *entityMap) forEachLive(visit func(EntityID, *entitySlot)) {
	for idx, slot := range m.slots {
		if slot == nil || slot.count <= 0 {
			continue
		}
		visit(makeEntityID(uint32(idx), m.gens[idx]), slot)
	}
}

// unqueueDrop removes an identity from the release queue (used when a
// failed construction is disposed without hooks).
func (m *entityMap) unqueueDrop(id EntityID) {
	for i, candidate := range m.dropped {
		if candidate == id {
			m.dropped = append(m.dropped[:i], m.dropped[i+1:]...)
			return
		}
	}
}

// reclaim removes the slot's state and frees the slot for reuse, bumping
// its generation so the dead identity never resolves again.
func (m *entityMap) reclaim(id EntityID) {
	idx := id.SlotIndex()
	if uint64(idx) >= uint64(len(m.slots)) || m.slots[idx] == nil {
		return
	}
	m.unqueueDrop(id)
	m.slots[idx] = nil
	m.gens[idx]++
	m.free = append(m.free, idx)
}

// leaseKind distinguishes an owned lease from a callback-borrowed facade
// token.
type leaseKind uint8

const (
	// leaseOwned is an independently owned lease registered with exactly
	// one live scope.
	leaseOwned leaseKind = iota + 1
	// leaseBorrow is a callback-borrowed facade token: valid during the
	// callback, expiring on return, never releasable or transferable.
	leaseBorrow
)

// borrowValidity is the shared validity token of callback-borrowed
// facades issued by one context (the context's callback lifetime).
type borrowValidity struct{ expired bool }

// entityLease is the shared lease token behind every Entity[S] value.
// Copying an Entity[S] copies the token pointer: aliases share one lease
// and releasing any copy invalidates all of them and decrements once
// (contract: "Logical ownership").
type entityLease struct {
	id    EntityID
	app   *App
	kind  leaseKind
	owner *Scope
	// valid is the borrow validity of callback-borrowed facades.
	valid *borrowValidity
	// acquisition order within the owning scope; scopes release their
	// resources in reverse acquisition order.
	seq  int
	dead bool
}

// Entity is a strong, lease-backed handle to an entity of state S.
//
// Assignment aliases the lease: h2 := h1 shares one lease. RetainInto
// creates an independent lease in another scope, TransferInto moves this
// lease to a live scope, and Release is idempotent and releases the lease
// once. A zero Entity is invalid; a released handle is a programmer
// error when used.
type Entity[S any] struct {
	id    EntityID
	app   *App
	lease *entityLease
}

// EntityID returns the generation-stamped identity of the entity.
func (h Entity[S]) EntityID() EntityID { return h.id }

// NewEntity builds an entity whose state is initialized by build, with
// one owned lease registered in owner's scope. The build callback runs
// within an application update and receives a fresh Context; it may
// register subscriptions, observers and release hooks.
//
// The entity also receives a dependent-resource scope (see
// Context.Scope) that follows the entity's lifetime, not the creating
// scope's. A panic in build disposes the lease and the dependent scope
// without publishing the entity, delivering no EntityCreated effect and
// no normal release hooks; the slot generation still advances.
func NewEntity[S any](cx AppContext, owner *Scope, build func(*S, *Context[S])) Entity[S] {
	if build == nil {
		panic("gpui: NewEntity called with a nil build callback")
	}
	app := appFrom(cx)
	if owner == nil {
		panic("gpui: NewEntity requires a non-nil owner scope")
	}
	if owner.app != app {
		panic("gpui: NewEntity owner scope belongs to a different application")
	}
	owner.requireOpen()

	var handle Entity[S]
	app.Update(func(*App) {
		id, slot := app.entities.newSlot()
		dependent := newScope(app, app.rootScope)
		slot.dependent = dependent
		slot.count = 1
		lease := &entityLease{id: id, app: app, kind: leaseOwned, owner: owner}
		owner.addLease(lease)

		published := false
		func() {
			defer func() {
				if !published {
					disposeFailedConstruction(app, slot, id, lease, owner, dependent)
					if r := recover(); r != nil {
						panic(r)
					}
				}
			}()
			cx := &Context[S]{app: app, entity: id, valid: &borrowValidity{}}
			state := new(S)
			defer func() { cx.valid.expired = true }()
			build(state, cx)
			slot.state = state
			published = true
			app.pushEffect(effect{kind: effEntityCreated, emitter: id, entityType: reflect.TypeFor[S]()})
			handle = Entity[S]{id: id, app: app, lease: lease}
		}()
	})
	return handle
}

// disposeFailedConstruction disposes the lease, the dependent scope and
// any partially registered hooks of an entity whose construction panicked
// (contract: "Partial construction failure disposes its dependent scope
// and lease without publishing the incomplete entity or delivering normal
// release hooks").
func disposeFailedConstruction(app *App, slot *entitySlot, id EntityID, lease *entityLease, owner *Scope, dependent *Scope) {
	if !lease.dead {
		lease.dead = true
		owner.removeLease(lease)
	}
	for _, reg := range slot.takeReleaseListeners() {
		reg.cancel()
	}
	for _, reg := range slot.takeObservers() {
		reg.cancel()
	}
	for _, reg := range slot.takeListeners() {
		reg.cancel()
	}
	app.entities.unqueueDrop(id)
	closeScopeTree(dependent, nil)
	app.entities.reclaim(id)
}

// appFor resolves and validates the application behind an access context.
func (h Entity[S]) appFor(cx AppContext, op string) *App {
	app := appFrom(cx)
	if app != h.app {
		panic(fmt.Sprintf("gpui: used an entity with the wrong context (used by %s)", op))
	}
	return app
}

// Read runs f with shared read access to the entity state. Reads can
// nest; reading while the entity is being updated panics with a reentry
// diagnostic. Read does not start an update or flush.
func (h Entity[S]) Read[R any](cx AppContext, f func(*S, *App) R) R {
	app := h.appFor(cx, "Read")
	entityLeaseOf(h, "Read")
	slot := app.entities.slot(h.id)
	if slot == nil {
		panic("gpui: entity handle references a reclaimed identity")
	}
	if slot.writer {
		panic(reentryDiagnostic(h.id, slot))
	}
	state, ok := slot.state.(*S)
	if !ok {
		panic(fmt.Sprintf("gpui: entity %d holds state of type %T, not %T", uint64(h.id), slot.state, new(S)))
	}
	slot.readers++
	defer func() { slot.readers-- }()
	return f(state, app)
}

// Update runs f with exclusive access to the entity state inside one
// application update. Nested updates of the same entity panic with a
// reentry diagnostic. Effects queued by f (notify, emit, defer) are
// flushed when the outermost update returns.
func (h Entity[S]) Update(cx AppContext, f func(*S, *Context[S])) {
	app := h.appFor(cx, "Update")
	entityLeaseOf(h, "Update")
	app.Update(func(*App) { h.access(app, f) })
}

// UpdateWith runs f with exclusive access and returns its result.
func (h Entity[S]) UpdateWith[R any](cx AppContext, f func(*S, *Context[S]) R) R {
	app := h.appFor(cx, "UpdateWith")
	entityLeaseOf(h, "UpdateWith")
	var result R
	app.Update(func(*App) { result = h.accessWith(app, f) })
	return result
}

// UpdateIn runs f with exclusive access and an explicit window context.
// A nil window reports ErrNoWindow. Window lifetime validation
// (ErrWindowClosed) arrives with the window tickets; in this slice any
// non-nil window is treated as open and threaded through to the callback.
func (h Entity[S]) UpdateIn(cx AppContext, w *Window, f func(*S, *Window, *Context[S])) error {
	if w == nil {
		return ErrNoWindow
	}
	h.Update(cx, func(state *S, cx *Context[S]) { f(state, w, cx) })
	return nil
}

// UpdateInWith runs f with exclusive access, an explicit window context,
// and returns its result.
func (h Entity[S]) UpdateInWith[R any](cx AppContext, w *Window, f func(*S, *Window, *Context[S]) R) (R, error) {
	if w == nil {
		var zero R
		return zero, ErrNoWindow
	}
	return h.UpdateWith(cx, func(state *S, cx *Context[S]) R { return f(state, w, cx) }), nil
}

// access runs the guarded update callback without another update wrap.
func (h Entity[S]) access(app *App, f func(*S, *Context[S])) {
	slot := app.entities.slot(h.id)
	if slot == nil {
		panic("gpui: entity handle references a reclaimed identity")
	}
	if slot.writer || slot.readers > 0 {
		panic(reentryDiagnostic(h.id, slot))
	}
	state, ok := slot.state.(*S)
	if !ok {
		panic(fmt.Sprintf("gpui: entity %d holds state of type %T, not %T", uint64(h.id), slot.state, new(S)))
	}
	cx := &Context[S]{app: app, entity: h.id, valid: &borrowValidity{}}
	slot.writer = true
	defer func() {
		slot.writer = false
		cx.valid.expired = true
	}()
	f(state, cx)
}

// accessWith is access with a result.
func (h Entity[S]) accessWith[R any](app *App, f func(*S, *Context[S]) R) R {
	var result R
	h.access(app, func(state *S, cx *Context[S]) { result = f(state, cx) })
	return result
}

// RetainInto creates an INDEPENDENT lease for the same entity in another
// live scope. The returned handle has its own lease count; releasing the
// original does not invalidate it.
//
// Do not retain an entity into its own dependent-resource scope (or
// another entity's, mutually): that creates a strong cycle which is
// reported by App.OwnershipReport and never collected automatically
// (contract: "Logical ownership").
func (h Entity[S]) RetainInto(owner *Scope) Entity[S] {
	if owner == nil {
		panic("gpui: RetainInto requires a non-nil owner scope")
	}
	app := entityLeaseOf(h, "RetainInto").app
	if owner.app != app {
		panic("gpui: RetainInto owner scope belongs to a different application")
	}
	owner.requireOpen()
	lease := &entityLease{id: h.id, app: app, kind: leaseOwned, owner: owner}
	owner.addLease(lease)
	app.entities.addLease(h.id)
	return Entity[S]{id: h.id, app: app, lease: lease}
}

// TransferInto moves this lease to another live scope without
// decrementing it or changing aliases: copies of the handle keep their
// shared lease, now owned by the new scope.
func (h Entity[S]) TransferInto(owner *Scope) {
	if owner == nil {
		panic("gpui: TransferInto requires a non-nil owner scope")
	}
	lease := entityLeaseOf(h, "TransferInto")
	if lease.kind == leaseBorrow {
		panic("gpui: cannot transfer a callback-borrowed entity handle; use RetainInto")
	}
	if owner.app != lease.app {
		panic("gpui: TransferInto owner scope belongs to a different application")
	}
	owner.requireOpen()
	if lease.owner != nil {
		lease.owner.removeLease(lease)
	}
	lease.owner = owner
	owner.addLease(lease)
}

// Release releases this handle's lease. It is idempotent: releasing any
// alias decrements once and invalidates every alias. When the logical
// count reaches zero the release is queued and processed at the next
// effect-flush boundary; logical zero is irreversible.
//
// Releasing a callback-borrowed facade is misuse and panics.
func (h Entity[S]) Release() {
	if h.lease == nil {
		panic("gpui: zero entity handle is invalid (used by Release)")
	}
	if h.lease.kind == leaseBorrow {
		if h.lease.valid.expired {
			panic("gpui: callback-borrowed entity handle expired: handles issued inside a callback are valid only during that callback")
		}
		panic("gpui: cannot release a callback-borrowed entity handle; retain it first if you need ownership")
	}
	leaseRelease(h.lease)
}

// Downgrade returns a weak identity for the entity. Weak handles are
// persistable and never keep the entity alive.
func (h Entity[S]) Downgrade() WeakEntity[S] {
	return WeakEntity[S]{id: h.id, app: h.app}
}

// leaseRelease performs the idempotent lease release.
func leaseRelease(lease *entityLease) {
	if lease.dead {
		return
	}
	lease.dead = true
	if lease.owner != nil {
		lease.owner.removeLease(lease)
	}
	lease.app.entities.dropLease(lease.id)
}

// WeakEntity is a weak identity for an entity: it never keeps the entity
// alive and upgrades only while the entity is logically live.
type WeakEntity[S any] struct {
	id  EntityID
	app *App
}

// EntityID returns the generation-stamped identity of the entity.
func (w WeakEntity[S]) EntityID() EntityID { return w.id }

// IsUpgradable reports whether the weak handle can currently be upgraded
// (the entity has a live logical lease). It never changes the lease count.
// Logical zero is irreversible: once IsUpgradable is false it stays false
// for that identity.
func (w WeakEntity[S]) IsUpgradable() bool { return w.checkLive() }

// UpgradeInto acquires an independent lease for the entity in owner while
// it is logically live. A zero weak handle fails; once the logical count
// reaches zero the failure is permanent: a dead identity never revives.
func (w WeakEntity[S]) UpgradeInto(owner *Scope) (Entity[S], bool) {
	if w.app == nil {
		return Entity[S]{}, false
	}
	if owner == nil {
		panic("gpui: UpgradeInto requires a non-nil owner scope")
	}
	if owner.app != w.app {
		panic("gpui: UpgradeInto owner scope belongs to a different application")
	}
	if !w.app.entities.isLive(w.id) {
		return Entity[S]{}, false
	}
	owner.requireOpen()
	lease := &entityLease{id: w.id, app: w.app, kind: leaseOwned, owner: owner}
	owner.addLease(lease)
	w.app.entities.addLease(w.id)
	return Entity[S]{id: w.id, app: w.app, lease: lease}, true
}

// appFor resolves and validates the application behind an access context.
func (w WeakEntity[S]) appFor(cx AppContext, op string) *App {
	app := appFrom(cx)
	if app != w.app {
		panic(fmt.Sprintf("gpui: used an entity with the wrong context (used by %s)", op))
	}
	return app
}

// checkLive reports whether the weak identity is logically live.
func (w WeakEntity[S]) checkLive() bool {
	return w.app != nil && w.app.entities.isLive(w.id)
}

// TryRead runs f with shared read access when the entity is live, and
// reports ErrEntityReleased otherwise.
func (w WeakEntity[S]) TryRead[R any](cx AppContext, f func(*S, *App) R) (R, error) {
	app := w.appFor(cx, "TryRead")
	if !w.checkLive() {
		var zero R
		return zero, ErrEntityReleased
	}
	return w.upgradeForRead(app).Read(app, f), nil
}

// upgradeForRead builds a transient strong handle for a live weak
// identity. The handle borrows no scope-owned lease: the read completes
// synchronously on the dispatcher goroutine, which pins the state for its
// duration.
func (w WeakEntity[S]) upgradeForRead(app *App) Entity[S] {
	return Entity[S]{id: w.id, app: app, lease: &entityLease{kind: leaseBorrow, id: w.id, app: app, valid: &borrowValidity{}}}
}

// TryUpdate runs f with exclusive access when the entity is live, and
// reports ErrEntityReleased otherwise.
func (w WeakEntity[S]) TryUpdate(cx AppContext, f func(*S, *Context[S])) error {
	_, err := w.TryUpdateWith(cx, func(state *S, cx *Context[S]) struct{} { f(state, cx); return struct{}{} })
	return err
}

// TryUpdateWith runs f with exclusive access and returns its result when
// the entity is live, and reports ErrEntityReleased otherwise.
func (w WeakEntity[S]) TryUpdateWith[R any](cx AppContext, f func(*S, *Context[S]) R) (R, error) {
	app := w.appFor(cx, "TryUpdateWith")
	if !w.checkLive() {
		var zero R
		return zero, ErrEntityReleased
	}
	return w.upgradeForRead(app).UpdateWith(app, f), nil
}

// TryUpdateIn runs f with exclusive access and an explicit window when
// the entity is live; nil windows report ErrNoWindow and released
// identities report ErrEntityReleased.
func (w WeakEntity[S]) TryUpdateIn(cx AppContext, window *Window, f func(*S, *Window, *Context[S])) error {
	if window == nil {
		return ErrNoWindow
	}
	return w.TryUpdate(cx, func(state *S, cx *Context[S]) { f(state, window, cx) })
}

// TryUpdateInWith runs f with exclusive access, an explicit window, and
// returns its result when the entity is live.
func (w WeakEntity[S]) TryUpdateInWith[R any](cx AppContext, window *Window, f func(*S, *Window, *Context[S]) R) (R, error) {
	if window == nil {
		var zero R
		return zero, ErrNoWindow
	}
	return w.TryUpdateWith(cx, func(state *S, cx *Context[S]) R { return f(state, window, cx) })
}
