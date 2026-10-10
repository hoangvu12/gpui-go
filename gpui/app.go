// Package gpui is the Go port of the GPUI core runtime.
//
// This slice implements the headless core selected by the runtime
// ownership contract (docs/runtime-ownership-contract.md): scoped entities
// with logical leases, typed borrowed access, generation-stamped
// identities, ordered effects under a controlled foreground dispatcher,
// and a deterministic test application. Windows, rendering and the native
// backend belong to later tickets; the DrawHook left here is the seam
// those tickets will fill.
//
// The public signatures follow the compile-verified fixture in
// .scratch/signature-validation/api.go: those signatures are fixed and
// this package supplies the runtime behind them. Where Go cannot
// reproduce a Rust behavior (drop-driven cancellation, borrow checking,
// coherence), the deliberate adaptation is documented beside the
// source-preserved behavior.
package gpui

import (
	"fmt"
	"reflect"
	"strings"
)

// appAccess is the unexported carrier that seals AppContext. Only types
// defined in this package can implement AppContext: callers pass *App or
// *Context[S], external wrappers embed or delegate one of those.
type appAccess struct{}

// AppContext is the sealed application-context interface implemented by
// *App and *Context[S].
type AppContext interface {
	access() appAccess
}

// appCarrier is the internal companion of AppContext used to recover the
// owning *App from any sealed context value.
type appCarrier interface {
	appRef() *App
}

// appFrom resolves the *App behind any sealed AppContext value.
func appFrom(cx AppContext) *App {
	if cx == nil {
		panic("gpui: nil AppContext")
	}
	carrier, ok := cx.(appCarrier)
	if !ok {
		panic("gpui: AppContext value does not carry an application")
	}
	return carrier.appRef()
}

// DrawHook is the seam later tickets (window drawing, frames) attach to.
// The effect loop consults it when the queue drains; nil hooks are a
// no-op. This slice defines the interface only.
type DrawHook interface {
	// DirtyWindows reports the window identities that need a redraw. It
	// must not mutate application state.
	DirtyWindows() []WindowID
}

// WindowID identifies a window. Window identity is a later-ticket
// concern; the type exists so effect routing and the draw hook are
// forward compatible.
type WindowID uint64

// App is the headless application core: the entity map, the FIFO effect
// queue, globals, the foreground/background executors and the root scope.
//
// Mutation, effect flushing and logical lifetime transitions all happen
// on the dispatcher goroutine that owns the App. Background task bodies
// run on their own goroutines but only touch the App through the
// foreground update machinery (see executor.go).
type App struct {
	entities *entityMap

	// effects is the FIFO effect queue (effects.go).
	effects []effect
	// pendingNotifies coalesces Notify effects per entity (contract:
	// "Effects and payloads").
	pendingNotifies map[EntityID]struct{}
	// pendingGlobalNotifies coalesces NotifyGlobal effects per type.
	pendingGlobalNotifies map[reflect.Type]struct{}
	// globals is the app-global map keyed by declared Go type.
	globals map[reflect.Type]any
	// globalObservers holds global observer registrations per type.
	globalObservers map[reflect.Type]*subscriberSet
	// newEntityObservers holds per-type new-entity registrations.
	newEntityObservers map[reflect.Type]*subscriberSet

	rootScope     *Scope
	background    *BackgroundExecutor
	foreground    *ForegroundExecutor
	sched         *scheduler
	drawHook      DrawHook
	regSeq        uint64
	detachedSubs  []*subReg
	detachedTasks []*taskRun

	// windows is the application's window registry (ticket05): logical
	// windows keyed by WindowID, touched only on the foreground thread.
	// Test applications keep it empty; real applications fill it through
	// OpenWindow with an attached host.
	windows map[WindowID]*Window
	// windowSeq assigns logical window identities in opening order.
	windowSeq uint64
	// host is the real-mode platform host attached by Attach/Run; nil in
	// test applications.
	host *Host
	// platformWake wakes the real-mode foreground dispatcher when the
	// scheduler gains runnable work (installed by Host.attach; nil in
	// test applications, whose tests drive the scheduler themselves).
	platformWake func()

	// keymap is the application's key binding collection (ticket13;
	// the reference Rc<RefCell<Keymap>> on the dispatch tree). Created on
	// first BindKeys/Keymap use.
	keymap *Keymap
	// keystrokeObservers are the app-level keystroke observers and
	// interceptors (window.rs keystroke_observers/interceptors).
	keystrokeObservers []keystrokeObserver
	// globalActionListeners are the app-level action listeners
	// (window.rs global_action_listeners).
	globalActionListeners []globalActionListener
	// propagateEvent is the dispatch propagation flag (App
	// propagate_event): listeners stop it to consume an event. Only
	// meaningful during a dispatch, on the foreground thread.
	propagateEvent bool

	// activeDrag is the in-progress drag operation (ticket24;
	// app.rs active_drag). Only meaningful on the foreground thread.
	activeDrag *AnyDrag
	// platformOwnedDrag tracks a drag handed to the platform's native
	// drag loop (ticket24; app.rs platform_owned_drag). Only meaningful
	// on the foreground thread.
	platformOwnedDrag *platformOwnedDrag

	// updateDepth is the active update nesting count. Only the outermost
	// update exit flushes (contract: "Effects and payloads").
	updateDepth int
	// flushing guards reentrant flushes: callbacks running during a
	// flush never start another one.
	flushing bool
	// arenaWake records that event-arena retirement produced pending
	// entity releases and a subsequent foreground flush is required (the
	// documented Go liveness adaptation of the reference arena clear).
	arenaWake bool
	// arena holds payload scopes of EmitOwned emissions in emission
	// order, retired at the tail of the flush cycle.
	arena []*Scope

	// assetRegistry is the application's asset registry (ticket18; the
	// reference App.asset_registry, app.rs lines 789/840-841): empty by
	// default (the reference Application::new default), replaced through
	// SetAssets/WithAssets.
	assetRegistry *AssetRegistry
	// httpClient is the application's injected HTTP client (the
	// reference App.http_client, app.rs lines 791/841): NullHttpClient by
	// default — zero network attempts.
	httpClient HttpClient
	// loadingAssets is the shared asset-task cache (the reference
	// App.loading_assets, app.rs line 788): keyed by the loader type and
	// the source hash; entries live until RemoveAsset.
	loadingAssets map[assetKey]*taskRun
	// reduceMotion is the animation policy flag (the reference
	// App.reduce_motion / set_reduce_motion, app.rs lines 1119-1128).
	reduceMotion bool
	// imageCodec is the application's image decode+atlas pair the image
	// cache retains frames in (lazily built; the first failure sticks).
	imageCodec    *ImageCodec
	imageCodecErr error
	// svgRenderer is the application's SVG renderer (ticket19; the
	// reference App.svg_renderer, app.rs line 790): lazily built over
	// the process native SVG service and tied to the asset registry
	// (SetAssets rebinds it, the with_assets wiring). The first
	// failure sticks.
	svgRenderer    *SvgRenderer
	svgRendererErr error
}

// newApp builds the headless application core wired to scheduler.
// Applications (and tests) construct the runtime through NewTestApp or
// later platform constructors.
func newApp(sched *scheduler) *App {
	app := &App{
		entities:              newEntityMap(),
		pendingNotifies:       make(map[EntityID]struct{}),
		pendingGlobalNotifies: make(map[reflect.Type]struct{}),
		globals:               make(map[reflect.Type]any),
		globalObservers:       make(map[reflect.Type]*subscriberSet),
		newEntityObservers:    make(map[reflect.Type]*subscriberSet),
		windows:               make(map[WindowID]*Window),
		assetRegistry:         NewAssetRegistry(),
		httpClient:            NullHttpClient{},
		loadingAssets:         make(map[assetKey]*taskRun),
		sched:                 sched,
	}
	app.rootScope = newScope(app, nil)
	app.background = &BackgroundExecutor{sched: sched}
	app.foreground = &ForegroundExecutor{sched: sched, app: app}
	sched.app = app
	return app
}

// access seals AppContext for *App.
func (a *App) access() appAccess { return appAccess{} }

// appRef implements the internal appCarrier.
func (a *App) appRef() *App { return a }

// RootScope returns the application's root ownership scope. It belongs to
// the app (contract: "Scopes form an acyclic ownership tree ... Roots
// belong to the app and its windows") and is the default owner for
// app-level registrations and fixture-owned entities.
func (a *App) RootScope() *Scope { return a.rootScope }

// Background returns the application's background executor.
func (a *App) Background() *BackgroundExecutor { return a.background }

// Foreground returns the application's foreground executor. Its queue is
// drained by the dispatcher that owns this App.
func (a *App) Foreground() *ForegroundExecutor { return a.foreground }

// SetDrawHook installs the window-draw seam the effect loop consults when
// the queue drains. Later tickets own real implementations; the hook is
// only read during flushing.
func (a *App) SetDrawHook(hook DrawHook) { a.drawHook = hook }

// Assets returns the application's asset registry (App::assets, app.rs
// lines 2096-2098). The registry is empty until configured; SvgRenderer
// builds from it (ticket19's lazily-created renderer, the port of the
// pin's with_assets svg_renderer wiring).
func (a *App) Assets() *AssetRegistry { return a.assetRegistry }

// SetAssets replaces the application's asset registry (the Go
// adaptation of Application::with_assets, app.rs lines 218-225: the
// real-application builder chain lands with the app entry
// integration, so applications configure the registry through this
// setter — with the same with_assets observable effect of swapping the
// registry the app resolves assets from, including rebuilding the
// SVG renderer binding from it).
func (a *App) SetAssets(assets *AssetRegistry) {
	if assets == nil {
		assets = NewAssetRegistry()
	}
	a.assetRegistry = assets
	// with_assets constructs a fresh SvgRenderer over the new registry
	// (app.rs lines 218-225); the port's lazy renderer resets so the
	// next use rebinds to this registry.
	a.svgRenderer = nil
	a.svgRendererErr = nil
}

// HTTPClient returns the application's HTTP client (App::http_client,
// app.rs lines 1723-1725): the null client until one is injected — zero
// network attempts by default.
func (a *App) HTTPClient() HttpClient {
	if a.httpClient == nil {
		return NullHttpClient{}
	}
	return a.httpClient
}

// SetHTTPClient sets the HTTP client (App::set_http_client, app.rs
// lines 1727-1729).
func (a *App) SetHTTPClient(client HttpClient) { a.httpClient = client }

// ReduceMotion reports whether non-essential animations should render
// in a static state (App::reduce_motion, app.rs lines 1119-1121).
func (a *App) ReduceMotion() bool { return a.reduceMotion }

// SetReduceMotion sets the reduce-motion flag (App::set_reduce_motion,
// app.rs lines 1123-1129): a change queues the RefreshWindows effect —
// the image playback consults the flag every frame.
func (a *App) SetReduceMotion(reduce bool) {
	if a.reduceMotion != reduce {
		a.reduceMotion = reduce
		a.RefreshWindows()
	}
}

// Update runs f as one application update on the owning dispatcher
// goroutine. Update increments the update nesting count; when the
// OUTERMOST update returns, flush_effects runs: release dropped entities,
// then apply queued effects FIFO until quiet. Nested updates do not
// flush. Callbacks running during a flush cannot start another flush.
//
// A panic in f restores framework bookkeeping (update depth, entity
// access guards, borrowed-facade validity) without flushing further
// application callbacks, and propagates; queued effects stay queued for
// the next update boundary (contract: "Borrowing and retained
// callbacks").
func (a *App) Update(f func(*App)) {
	if f == nil {
		panic("gpui: Update called with a nil callback")
	}
	a.startUpdate()
	completed := false
	defer func() {
		if !completed && a.updateDepth > 0 {
			// Panic unwinding: restore the update boundary only.
			a.updateDepth--
		}
	}()
	f(a)
	completed = true
	a.finishUpdate()
}

// Defer schedules f to run at the end of the current effect cycle, in
// call order, including callbacks scheduled from inside a running defer.
// It matches App::defer in the reference: a defer registered during the
// flush loop runs in that same loop.
func (a *App) Defer(f func(*App)) {
	if f == nil {
		panic("gpui: Defer called with a nil callback")
	}
	a.pushEffect(effect{kind: effDefer, callback: f})
}

// RefreshWindows schedules all windows for redraw. It can be called
// repeatedly in one update cycle and still results in a single redraw
// pass; the effect is not coalesced in the queue, matching the reference.
func (a *App) RefreshWindows() {
	a.effects = append(a.effects, effect{kind: effRefresh})
}

// startUpdate increments the update nesting count.
func (a *App) startUpdate() { a.updateDepth++ }

// finishUpdate decrements the update nesting count and flushes effects
// when the outermost update exits. The flush is guarded against reentry
// and both the depth counter and the flush guard are restored when a
// callback panics during flushing. When event-arena retirement produced
// pending releases, one subsequent foreground flush runs: the documented
// Go liveness adaptation of the reference, which clears the arena at the
// break and leaves those releases to a later boundary.
func (a *App) finishUpdate() {
	defer func() { a.updateDepth-- }()
	if !a.flushing && a.updateDepth == 1 {
		a.flushing = true
		defer func() { a.flushing = false }()
		a.flushEffects()
		for a.arenaWake {
			a.arenaWake = false
			a.flushEffects()
		}
	}
}

// pushEffect appends one effect, coalescing Notify and NotifyGlobal while
// the source is still pending (pending-only coalescing). It mirrors
// App::push_effect in the reference.
func (a *App) pushEffect(e effect) {
	switch e.kind {
	case effNotify:
		if _, pending := a.pendingNotifies[e.emitter]; pending {
			return
		}
		a.pendingNotifies[e.emitter] = struct{}{}
	case effNotifyGlobal:
		if _, pending := a.pendingGlobalNotifies[e.globalType]; pending {
			return
		}
		a.pendingGlobalNotifies[e.globalType] = struct{}{}
	}
	a.effects = append(a.effects, e)
}

// flushEffects is the reference flush loop: repeatedly release dropped
// entities, then apply one queued effect, until the queue is quiet. Newly
// queued effects append and are processed by later iterations of the same
// loop. When the queue is quiet the draw hook is consulted, then the
// event arena is retired in emission order; retirement that produces
// pending releases requests a subsequent flush instead of draining those
// releases in this cycle.
func (a *App) flushEffects() {
	for {
		a.releaseDroppedEntities()

		if len(a.effects) > 0 {
			e := a.effects[0]
			a.effects = a.effects[1:]
			a.applyEffect(e)
			continue
		}

		// Queue drained: let the draw hook (later tickets) observe or
		// enqueue more work. A no-op by default.
		if hook := a.drawHook; hook != nil {
			_ = hook.DirtyWindows()
		}
		if len(a.effects) > 0 {
			continue
		}

		if a.retireEventArena() {
			// Retirement produced pending releases. They are not drained
			// in this cycle; finishUpdate performs the subsequent flush.
			a.arenaWake = true
		}
		break
	}
}

// releaseDroppedEntities releases entities whose logical lease count
// reached zero, in enqueue order (contract: "Enqueue order determines
// release order; dependent releases join that queue"). It runs at the top
// of every flush-loop iteration, so a release never happens in the middle
// of an active access.
func (a *App) releaseDroppedEntities() {
	for {
		dropped := a.entities.takeDropped()
		if len(dropped) == 0 {
			break
		}
		for _, id := range dropped {
			a.releaseEntity(id)
		}
	}
}

// releaseEntity performs the documented release sequence for one entity
// identity:
//
//  1. remove the entity's observers, event listeners and invalidation
//     links first;
//  2. mark its dependent-resource scope closing and cancel its work
//     before callbacks;
//  3. run release callbacks with the still-available state;
//  4. release outgoing scope resources after the callbacks;
//  5. reclaim the slot, bumping its generation so the dead identity can
//     never be upgraded again.
//
// Release callbacks cannot resurrect the entity or add resources to the
// closing scope.
func (a *App) releaseEntity(id EntityID) {
	slot := a.entities.slot(id)
	if slot == nil {
		return
	}
	observers := slot.takeObservers()
	listeners := slot.takeListeners()
	releases := slot.takeReleaseListeners()

	dep := slot.dependent
	if dep != nil && dep.state == ScopeOpen {
		markClosingTree(dep)
		cancelTree(dep)
	}

	// Run the release callbacks with the still-available state, in
	// registration order. Registrations already cancelled before the
	// release do not fire.
	for _, reg := range releases {
		if reg.active && !reg.dropped {
			reg.invoke(a, slot.state)
		}
		reg.cancel()
	}

	// Release outgoing dependencies after the callbacks.
	if dep != nil && dep.state != ScopeClosed {
		releaseTree(dep)
	}

	// Drop now-dead registrations for this identity.
	for _, reg := range observers {
		reg.cancel()
	}
	for _, reg := range listeners {
		reg.cancel()
	}

	a.entities.reclaim(id)
}

// applyEffect dispatches one effect. Delivery order within one effect
// follows registration order; cancellation and endpoint liveness are
// checked before each callback and again after it returns.
func (a *App) applyEffect(e effect) {
	switch e.kind {
	case effNotify:
		a.applyNotify(e.emitter)
	case effEmit:
		a.applyEmit(e)
	case effDefer:
		e.callback(a)
	case effRefresh:
		// Window refresh: the draw hook observes dirtiness; real window
		// invalidation belongs to the window tickets.
		if a.drawHook != nil {
			_ = a.drawHook.DirtyWindows()
		}
	case effNotifyGlobal:
		a.applyNotifyGlobal(e.globalType)
	case effEntityCreated:
		a.applyEntityCreated(e)
	}
}

// notify queues a Notify effect for id, coalesced while pending.
func (a *App) notify(id EntityID) {
	a.pushEffect(effect{kind: effNotify, emitter: id})
}

// applyNotify removes the entity from the pending set (so a callback can
// re-notify) and delivers to active observers in registration order.
func (a *App) applyNotify(id EntityID) {
	delete(a.pendingNotifies, id)
	if slot := a.entities.slot(id); slot != nil {
		a.deliverSubs(slot.observers, nil, id, nil)
	}
}

// applyEmit delivers one emitted event to the matching listeners of the
// source entity, in registration order. Emission neither implies notify
// nor retains the source for routing; a released source has no remaining
// subscriptions to deliver to.
func (a *App) applyEmit(e effect) {
	if slot := a.entities.slot(e.emitter); slot != nil {
		a.deliverSubs(slot.listeners, func(reg *subReg) bool {
			return reg.key == e.payloadType
		}, 0, e.payload)
	}
}

// applyNotifyGlobal removes the type from the pending set and delivers to
// the type's active global observers in registration order.
func (a *App) applyNotifyGlobal(t reflect.Type) {
	delete(a.pendingGlobalNotifies, t)
	if set := a.globalObservers[t]; set != nil {
		a.deliverSubs(set, nil, 0, nil)
	}
}

// applyEntityCreated delivers the creation event to the type's active
// new-entity observers.
func (a *App) applyEntityCreated(e effect) {
	if set := a.newEntityObservers[e.entityType]; set != nil {
		a.deliverSubs(set, nil, 0, e)
	}
}

// SetGlobal stores a global value keyed by its declared Go type.
// Replacing an existing value of the same type notifies the type's global
// observers (coalesced per flush), matching the reference global
// semantics.
func (a *App) SetGlobal(value any) {
	if value == nil {
		panic("gpui: SetGlobal called with nil")
	}
	t := reflect.TypeOf(value)
	_, existed := a.globals[t]
	a.globals[t] = value
	if existed {
		a.pushEffect(effect{kind: effNotifyGlobal, globalType: t})
	}
}

// Global returns the global stored for T. It panics when no global of
// that type was set.
func Global[T any](a *App) T {
	t := reflect.TypeFor[T]()
	v, ok := a.globals[t]
	if !ok {
		panic("gpui: no global registered for " + t.String())
	}
	return v.(T)
}

// NotifyGlobal queues a NotifyGlobal effect for T. Deliveries coalesce
// while a notification of T is pending.
func (a *App) NotifyGlobal[T any]() {
	a.pushEffect(effect{kind: effNotifyGlobal, globalType: reflect.TypeFor[T]()})
}

// ObserveGlobal registers f for notifications of the global type T. New
// registrations activate through a queued defer. The returned handle can
// Cancel, TransferInto a scope, or Detach to the app registry; ignoring
// the handle leaves the registration active under the app root scope
// (Go adaptation: no drop-driven cancellation).
func (a *App) ObserveGlobal[T any](f func(*App)) Subscription {
	if f == nil {
		panic("gpui: ObserveGlobal called with a nil callback")
	}
	t := reflect.TypeFor[T]()
	set, ok := a.globalObservers[t]
	if !ok {
		set = newSubscriberSet()
		a.globalObservers[t] = set
	}
	reg := a.registerSub(set, 0, t, a.rootScope, func(a *App, _ any) { f(a) })
	reg.activateViaDefer(a)
	return Subscription{app: a, reg: reg}
}

// ObserveNewEntities registers f for creations of entities of type S.
// Registrations activate immediately, matching the reference new-entity
// observers. The entity handle passed to f is callback-borrowed: retain
// it explicitly to keep it.
func (a *App) ObserveNewEntities[S any](f func(Entity[S], *App)) Subscription {
	if f == nil {
		panic("gpui: ObserveNewEntities called with a nil callback")
	}
	t := reflect.TypeFor[S]()
	set, ok := a.newEntityObservers[t]
	if !ok {
		set = newSubscriberSet()
		a.newEntityObservers[t] = set
	}
	reg := a.registerSub(set, 0, t, a.rootScope, func(a *App, payload any) {
		e := payload.(effect)
		valid := &borrowValidity{}
		defer func() { valid.expired = true }()
		f(Entity[S]{
			id:    e.emitter,
			app:   a,
			lease: &entityLease{kind: leaseBorrow, id: e.emitter, app: a, valid: valid},
		}, a)
	})
	reg.active = true
	return Subscription{app: a, reg: reg}
}

// registerSub creates one registration in set, owned by scope, recording
// the global registration order. The caller decides activation.
func (a *App) registerSub(set *subscriberSet, endpoint EntityID, key reflect.Type, scope *Scope, invoke func(*App, any)) *subReg {
	if scope == nil {
		panic("gpui: subscription registration requires an owning scope")
	}
	if invoke == nil {
		panic("gpui: subscription registration requires a callback")
	}
	if scope.app != a {
		panic("gpui: subscription owner scope belongs to a different application")
	}
	scope.requireOpen()
	a.regSeq++
	reg := &subReg{
		id:       a.regSeq,
		app:      a,
		endpoint: endpoint,
		key:      key,
		owner:    scope,
		invoke:   invoke,
	}
	set.insert(reg)
	scope.addSub(reg)
	return reg
}

// deliverSubs visits active registrations in registration order and
// invokes them with payload.
//
//   - accept (optional) filters registrations by payload type.
//   - source (optional, non-zero for notify delivery) requires the source
//     entity to be logically live before each callback; a source released
//     mid-traversal unsubscribes its remaining observers.
//
// The active list is taken out for the traversal, so registrations made
// during the traversal are not inserted into it (they also stay inert
// until their deferred activation runs). Cancellation by an earlier
// callback skips later callbacks in the same traversal (cancel-next);
// endpoint liveness is checked before each callback and again after it
// returns.
func (a *App) deliverSubs(set *subscriberSet, accept func(*subReg) bool, source EntityID, payload any) {
	if set == nil || len(set.subs) == 0 {
		return
	}
	subs := set.subs
	set.subs = nil
	keep := make([]*subReg, 0, len(subs))
	for _, reg := range subs {
		if !reg.active {
			// Inert registration awaiting activation.
			keep = append(keep, reg)
			continue
		}
		if reg.dropped {
			// Already cancelled.
			continue
		}
		if reg.endpoint != 0 && !a.entities.isLive(reg.endpoint) {
			// Dead observer endpoint: unsubscribe.
			continue
		}
		if source != 0 && !a.entities.isLive(source) {
			// Source released: unsubscribe the remaining observers.
			continue
		}
		if accept != nil && !accept(reg) {
			// Different payload type: keep for its own deliveries.
			keep = append(keep, reg)
			continue
		}
		reg.invoke(a, payload)
		if reg.once || reg.dropped || (reg.endpoint != 0 && !a.entities.isLive(reg.endpoint)) {
			// One-shot, cancelled by its own callback, or the endpoint
			// died during the callback.
			continue
		}
		keep = append(keep, reg)
	}
	// Merge registrations added during the traversal; they keep their
	// position for later deliveries.
	set.subs = append(keep, set.subs...)
}

// retireEventArena retires the payload scopes of EmitOwned emissions in
// emission order at the tail of the flush cycle. It reports whether the
// retirement produced pending entity releases, which require a subsequent
// foreground flush rather than being drained in this cycle (contract:
// "Effects and payloads").
func (a *App) retireEventArena() bool {
	if len(a.arena) == 0 {
		return false
	}
	scopes := a.arena
	a.arena = nil
	released := false
	for _, scope := range scopes {
		before := a.entities.pendingDrops()
		closeScopeTree(scope, nil)
		if a.entities.pendingDrops() > before {
			released = true
		}
	}
	return released
}

// removeDetachedTask removes a completed task from the app registry
// (contract: "Registries remove completed tasks").
func (a *App) removeDetachedTask(t *taskRun) {
	for i, candidate := range a.detachedTasks {
		if candidate == t {
			a.detachedTasks = append(a.detachedTasks[:i], a.detachedTasks[i+1:]...)
			return
		}
	}
}

// entityLeaseOf returns the lease of an owned handle, or panics with the
// programmer-misuse diagnostic when the handle is zero, stale, or an
// expired borrow.
func entityLeaseOf[S any](h Entity[S], op string) *entityLease {
	if h.lease == nil {
		panic(fmt.Sprintf("gpui: zero entity handle is invalid (used by %s)", op))
	}
	if h.lease.kind == leaseBorrow {
		if h.lease.valid.expired {
			panic("gpui: callback-borrowed entity handle expired: handles issued inside a callback are valid only during that callback; use RetainInto to keep one")
		}
		return h.lease
	}
	if h.lease.dead {
		panic("gpui: entity handle already released: assignment shares one lease, so releasing any copy invalidates all aliases; use RetainInto for independent ownership")
	}
	return h.lease
}

// withEntityUpdate runs f as a pinned update on the entity identified by
// id. It reports false without running when the entity is not logically
// live. The pin is temporary internal bookkeeping (contract: "Borrowing
// and retained callbacks"): it is not an owned lease and does not affect
// the logical count, but the write guard protects the state for the
// duration of the callback and release processing waits for it to return.
func withEntityUpdate[S any](a *App, id EntityID, f func(state *S, cx *Context[S])) bool {
	if !a.entities.isLive(id) {
		return false
	}
	a.Update(func(*App) {
		slot := a.entities.slot(id)
		if slot == nil {
			return
		}
		if slot.writer || slot.readers > 0 {
			panic(reentryDiagnostic(id, slot))
		}
		state, ok := slot.state.(*S)
		if !ok {
			panic(fmt.Sprintf("gpui: entity %d holds state of type %T, not %T", uint64(id), slot.state, new(S)))
		}
		cx := &Context[S]{app: a, entity: id, valid: &borrowValidity{}}
		slot.writer = true
		defer func() {
			slot.writer = false
			cx.valid.expired = true
		}()
		f(state, cx)
	})
	return true
}

// reentryDiagnostic builds the RefCell-style reentry diagnostic.
func reentryDiagnostic(id EntityID, slot *entitySlot) string {
	if slot.writer {
		return fmt.Sprintf("gpui: conflicting reentrant access: entity %d is already being updated (writes are exclusive; reads nest)", uint64(id))
	}
	return fmt.Sprintf("gpui: conflicting reentrant access: entity %d is being read; update requires exclusive access", uint64(id))
}

// OwnershipReport returns shutdown-oriented debug diagnostics (contract:
// "Debug diagnostics should report remaining leases/scopes and cycles at
// shutdown; they do not silently collect them"). It lists every live
// entity with its lease count and the scopes holding its leases, every
// open scope with its resource counts, and any strong cycles among
// entities whose leases are held by other entities' dependent scopes.
//
// A strong cycle is reported, never broken: breaking an edge requires an
// explicit application disposal, a weak backedge or an external
// graph-owner scope. The report only observes state and never mutates
// ownership.
func (a *App) OwnershipReport() []string {
	// Map each entity's dependent scope back to its owning entity.
	dependent := make(map[*Scope]EntityID)
	var live []EntityID
	states := make(map[EntityID]any)
	a.entities.forEachLive(func(id EntityID, slot *entitySlot) {
		live = append(live, id)
		states[id] = slot.state
		if slot.dependent != nil {
			dependent[slot.dependent] = id
		}
	})

	// Collect every lease registration reachable from the scope tree and
	// the event arena, in deterministic order.
	type leaseEdge struct {
		id    EntityID
		scope *Scope
	}
	var leases []leaseEdge
	var collect func(s *Scope)
	collect = func(s *Scope) {
		for _, lease := range s.leases {
			leases = append(leases, leaseEdge{id: lease.id, scope: s})
		}
		for _, child := range s.children {
			collect(child)
		}
	}
	collect(a.rootScope)
	for _, scope := range a.arena {
		for _, lease := range scope.leases {
			leases = append(leases, leaseEdge{id: lease.id, scope: scope})
		}
	}

	scopeLabel := func(s *Scope) string {
		if owner, ok := dependent[s]; ok {
			return fmt.Sprintf("dependent scope of entity %d", uint64(owner))
		}
		if s == a.rootScope {
			return "root scope"
		}
		return "scope"
	}

	leaseCount := make(map[EntityID]int)
	owners := make(map[EntityID][]string)
	for _, edge := range leases {
		leaseCount[edge.id]++
		owners[edge.id] = append(owners[edge.id], scopeLabel(edge.scope))
	}

	var lines []string
	for _, id := range live {
		typ := "unknown state"
		if state, ok := states[id]; ok && state != nil {
			typ = fmt.Sprintf("%T", state)
		}
		owned := owners[id]
		if owned == nil {
			owned = []string{"<unregistered lease>"}
		}
		lines = append(lines, fmt.Sprintf("entity %d (%s) live, %d lease(s): %s",
			uint64(id), typ, leaseCount[id], strings.Join(owned, ", ")))
	}

	// Open scopes with their resource counts.
	var walkScopes func(s *Scope)
	walkScopes = func(s *Scope) {
		if s.state == ScopeOpen {
			lines = append(lines, fmt.Sprintf("open scope %s: %d lease(s), %d subscription(s), %d task(s)",
				scopeLabel(s), len(s.leases), len(s.subs), len(s.tasks)))
		}
		for _, child := range s.children {
			walkScopes(child)
		}
	}
	walkScopes(a.rootScope)

	// Strong-cycle detection: entity X is kept alive by entity Y when a
	// lease of X is registered in Y's dependent scope. A cycle among
	// those edges never reaches logical zero and must be broken
	// explicitly.
	edges := make(map[EntityID][]EntityID)
	for _, edge := range leases {
		if owner, ok := dependent[edge.scope]; ok {
			edges[edge.id] = append(edges[edge.id], owner)
		}
	}
	liveSet := make(map[EntityID]bool, len(live))
	for _, id := range live {
		liveSet[id] = true
	}
	const (
		unvisited = 0
		visiting  = 1
		done      = 2
	)
	color := make(map[EntityID]int)
	var stack []EntityID
	var cycleLines []string
	var visit func(id EntityID)
	visit = func(id EntityID) {
		color[id] = visiting
		stack = append(stack, id)
		for _, next := range edges[id] {
			if !liveSet[next] {
				continue
			}
			switch color[next] {
			case unvisited:
				visit(next)
			case visiting:
				// Found a cycle: report the path from next back to next.
				start := -1
				for i, node := range stack {
					if node == next {
						start = i
						break
					}
				}
				if start >= 0 {
					path := stack[start:]
					parts := make([]string, len(path)+1)
					for i, node := range path {
						parts[i] = fmt.Sprintf("entity %d", uint64(node))
					}
					parts[len(path)] = parts[0]
					cycleLines = append(cycleLines, fmt.Sprintf(
						"strong cycle: %s; break an edge explicitly (weak backedge, external owner scope or disposal) - cycles are not collected",
						strings.Join(parts, " -> ")))
				}
			}
		}
		stack = stack[:len(stack)-1]
		color[id] = done
	}
	for _, id := range live {
		if color[id] == unvisited {
			visit(id)
		}
	}
	lines = append(lines, cycleLines...)
	return lines
}

// ---------------------------------------------------------------------------
// Desktop operations (ticket26)
// ---------------------------------------------------------------------------

// The application-level desktop surface delegates to the attached
// platform host (the reference's Platform trait): cursor control,
// displays, shell operations, notifications, jump lists, power/session
// callbacks and the verified unsupported outcomes. Callbacks cross into
// the application at an update boundary; the host invokes them on the
// foreground thread.

// desktopHost resolves the attached host or reports ErrNoHost.
func (a *App) desktopHost() (*Host, error) {
	if a.host == nil {
		return nil, ErrNoHost
	}
	return a.host, nil
}

// SetCursorStyle sets the platform cursor (Platform::set_cursor_style).
func (a *App) SetCursorStyle(style CursorStyle) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	h.SetCursorStyle(style)
	return nil
}

// CursorStyle returns the platform's recorded cursor style.
func (a *App) CursorStyle() (CursorStyle, error) {
	h, err := a.desktopHost()
	if err != nil {
		return CursorArrow, err
	}
	return h.CursorStyle()
}

// HideCursorUntilMouseMoves hides the cursor until the next mouse move
// (Platform::hide_cursor_until_mouse_moves).
func (a *App) HideCursorUntilMouseMoves() error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	h.HideCursorUntilMouseMoves()
	return nil
}

// IsCursorVisible reports the cursor-visibility flag
// (Platform::is_cursor_visible).
func (a *App) IsCursorVisible() (bool, error) {
	h, err := a.desktopHost()
	if err != nil {
		return false, err
	}
	return h.IsCursorVisible(), nil
}

// Displays enumerates the machine's monitors (Platform::displays).
func (a *App) Displays() ([]DisplayInfo, error) {
	h, err := a.desktopHost()
	if err != nil {
		return nil, err
	}
	return h.Displays()
}

// PrimaryDisplay returns the primary monitor
// (Platform::primary_display).
func (a *App) PrimaryDisplay() (*DisplayInfo, error) {
	h, err := a.desktopHost()
	if err != nil {
		return nil, err
	}
	return h.PrimaryDisplay()
}

// OpenURL opens a URL with the system handler (Platform::open_url).
func (a *App) OpenURL(url string) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	h.OpenURL(url)
	return nil
}

// OpenWithSystem opens a path with the system handler
// (Platform::open_with_system).
func (a *App) OpenWithSystem(path string) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	h.OpenWithSystem(path)
	return nil
}

// RevealPath reveals a path in the shell (Platform::reveal_path).
func (a *App) RevealPath(path string) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	h.RevealPath(path)
	return nil
}

// OnOpenUrls registers the URL-open callback (Platform::on_open_urls).
// Windows registers it but nothing delivers to it (protocol activation
// does not feed an unpackaged process).
func (a *App) OnOpenUrls(cb func(urls []string)) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	return h.OnOpenUrls(cb)
}

// OnSystemWake registers the system wake callback
// (Platform::on_system_wake) and the suspend/resume notification.
func (a *App) OnSystemWake(cb func()) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	if cb == nil {
		return fmt.Errorf("gpui: OnSystemWake requires a non-nil callback")
	}
	return h.OnSystemWake(func() { a.Update(func(*App) { cb() }) })
}

// OnQuit registers the quit callback (Platform::on_quit): it runs after
// the message loop exits and on session end; returning true reports
// completed shutdown.
func (a *App) OnQuit(cb func() bool) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	if cb == nil {
		return fmt.Errorf("gpui: OnQuit requires a non-nil callback")
	}
	return h.OnQuit(func() bool {
		var completed bool
		a.Update(func(*App) { completed = cb() })
		return completed
	})
}

// OnReopen registers the reopen callback (Platform::on_reopen); no
// Windows event delivers to it.
func (a *App) OnReopen(cb func()) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	return h.OnReopen(cb)
}

// ThermalState returns the thermal state (Platform::thermal_state). A
// real app reports the host's answer; test applications (no host)
// report the source's constant Nominal, which is the Windows value.
func (a *App) ThermalState() ThermalState {
	if a.host == nil {
		return ThermalStateNominal
	}
	return a.host.ThermalState()
}

// OnThermalStateChange registers the thermal-state callback
// (Platform::on_thermal_state_change); Windows never fires it.
func (a *App) OnThermalStateChange(cb func()) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	return h.OnThermalStateChange(cb)
}

// ActivateApp activates the application (Platform::activate; empty body
// on Windows).
func (a *App) ActivateApp(ignoringOtherApps bool) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	h.ActivateApp(ignoringOtherApps)
	return nil
}

// HideApp hides the application (Platform::hide; empty body on
// Windows).
func (a *App) HideApp() error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	h.HideApp()
	return nil
}

// HideOtherApps hides other applications. The reference panics with
// unimplemented!(); the port reproduces the panic.
func (a *App) HideOtherApps() error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	h.HideOtherApps()
	return nil
}

// UnhideOtherApps unhides other applications. The reference panics with
// unimplemented!(); the port reproduces the panic.
func (a *App) UnhideOtherApps() error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	h.UnhideOtherApps()
	return nil
}

// PathForAuxiliaryExecutable reproduces the reference's "not yet
// implemented" failure (Platform::path_for_auxiliary_executable).
func (a *App) PathForAuxiliaryExecutable(name string) (string, error) {
	h, err := a.desktopHost()
	if err != nil {
		return "", err
	}
	return h.PathForAuxiliaryExecutable(name)
}

// RegisterURLScheme reproduces the reference's task error
// (Platform::register_url_scheme).
func (a *App) RegisterURLScheme(scheme string) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	return h.RegisterURLScheme(scheme)
}

// AppPath returns the current executable's path (Platform::app_path).
func (a *App) AppPath() (string, error) {
	h, err := a.desktopHost()
	if err != nil {
		return "", err
	}
	return h.AppPath()
}

// Restart relaunches the application after this process exits
// (Platform::restart) through the deferred-launch path.
func (a *App) Restart(binaryPath string, arguments []string) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	return h.Restart(binaryPath, arguments)
}

// SetAppIdentity records the application identity and sets the
// process's AppUserModelID (Platform::set_app_identity).
func (a *App) SetAppIdentity(identifier, name string) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	return h.SetAppIdentity(identifier, name)
}

// ShowSystemNotification shows one notification
// (Platform::show_system_notification). Without an app identity this
// is the source-supported no-op (a recorded warning, no toast); with an
// identity it reports the unported WinRT toast notifier as an explicit
// pending row.
func (a *App) ShowSystemNotification(notification SystemNotification) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	return h.ShowSystemNotification(notification)
}

// DismissSystemNotification dismisses the notification with the given
// tag (Platform::dismiss_system_notification).
func (a *App) DismissSystemNotification(tag string) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	h.DismissSystemNotification(tag)
	return nil
}

// OnSystemNotificationResponse registers the notification response
// callback (Platform::on_system_notification_response).
func (a *App) OnSystemNotificationResponse(cb func(SystemNotificationResponse)) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	if cb == nil {
		return fmt.Errorf("gpui: OnSystemNotificationResponse requires a non-nil callback")
	}
	return h.OnSystemNotificationResponse(func(response SystemNotificationResponse) {
		a.Update(func(*App) { cb(response) })
	})
}

// SetMenus records the application menus (Platform::set_menus).
func (a *App) SetMenus(menus []Menu) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	h.SetMenus(menus)
	return nil
}

// GetMenus returns the recorded menus (Platform::get_menus; always
// present on Windows).
func (a *App) GetMenus() ([]Menu, bool) {
	if a.host == nil {
		return nil, false
	}
	return a.host.GetMenus()
}

// SetDockMenu builds the dock (taskbar) menu items and updates the jump
// list (Platform::set_dock_menu).
func (a *App) SetDockMenu(items []MenuItem) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	h.SetDockMenu(items)
	return nil
}

// UpdateJumpList updates the jump-list state and reports the
// user-removed entries (Platform::update_jump_list). The shell commit
// is not ported; the typed error is the honest outcome.
func (a *App) UpdateJumpList(menus []MenuItem, entries [][]string) ([][]string, error) {
	h, err := a.desktopHost()
	if err != nil {
		return nil, err
	}
	return h.UpdateJumpList(menus, entries)
}

// PerformDockMenuAction dispatches one dock menu action by index
// (Platform::perform_dock_menu_action).
func (a *App) PerformDockMenuAction(index int) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	h.PerformDockMenuAction(index)
	return nil
}

// OnAppMenuAction registers the app menu action callback
// (Platform::on_app_menu_action), the delivery path for dock menu
// actions.
func (a *App) OnAppMenuAction(cb func(action BoxedAction)) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	if cb == nil {
		return fmt.Errorf("gpui: OnAppMenuAction requires a non-nil callback")
	}
	return h.OnAppMenuAction(func(action BoxedAction) {
		a.Update(func(*App) { cb(action) })
	})
}

// OnWillOpenAppMenu registers the will-open callback
// (Platform::on_will_open_app_menu); dormant on Windows.
func (a *App) OnWillOpenAppMenu(cb func()) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	return h.OnWillOpenAppMenu(cb)
}

// OnValidateAppMenuCommand registers the validation callback
// (Platform::on_validate_app_menu_command); dormant on Windows.
func (a *App) OnValidateAppMenuCommand(cb func(action BoxedAction) bool) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	return h.OnValidateAppMenuCommand(cb)
}

// WindowAppearance returns the system appearance
// (Platform::window_appearance: a fresh read).
func (a *App) WindowAppearance() (WindowAppearance, error) {
	h, err := a.desktopHost()
	if err != nil {
		return WindowAppearanceLight, err
	}
	return h.WindowAppearance()
}

// ButtonLayout reports the window-control button layout when the
// platform supports one (Platform::button_layout; Windows reports the
// unsupported trait default).
func (a *App) ButtonLayout() (WindowButtonLayout, bool) {
	h, err := a.desktopHost()
	if err != nil {
		return WindowButtonLayout{}, false
	}
	return h.ButtonLayout()
}

// OnButtonLayoutChanged registers the button-layout observer
// (Platform::on_button_layout_changed; the Windows default never fires).
func (a *App) OnButtonLayoutChanged(cb func()) error {
	h, err := a.desktopHost()
	if err != nil {
		return err
	}
	return h.OnButtonLayoutChanged(cb)
}

// MouseWheelSettings returns the tracked wheel parameters
// (WindowsSystemSettings::mouse_wheel_settings).
func (a *App) MouseWheelSettings() (MouseWheelSettings, error) {
	h, err := a.desktopHost()
	if err != nil {
		return MouseWheelSettings{}, err
	}
	return h.MouseWheelSettings(), nil
}
