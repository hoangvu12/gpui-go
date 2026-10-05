# Selected runtime ownership contract

Status: selected contract for [Define entity ownership, effects, and scheduling](../.scratch/gpui-core/issues/04-define-runtime-ownership.md), updated 2026-10-05. The decision ticket records selection and rationale; this document supplies the detailed contract. The bounded ownership-facing declarations now compile under Go 1.27.1 in the [signature fixture](../.scratch/signature-validation/README.md). Runtime behavior remains unimplemented and unverified.

The reference remains GPUI-CE `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`. [Runtime research](../research/14-runtime-ownership-contracts.md) supplies the source comparison. The rules below include deliberate Go adaptations; they do not claim Rust's automatic clone/drop or borrow checking exists in Go.

## Logical ownership

Use logical reference counts, generation-stamped entity identities, and explicit owning scopes. GC reachability is independent of logical liveness. Cleanup/finalizer execution and `weak.Pointer` cannot decide when an entity stops accepting callbacks. Go's [cleanup](https://pkg.go.dev/runtime#AddCleanup) and [weak pointer](https://pkg.go.dev/weak#Pointer) contracts do not supply that guarantee.

```go
// Selected shapes, covered by the bounded compiler fixture; not implementations.
func NewEntity[S any](cx AppContext, owner *Scope,
    build func(*S, *Context[S])) Entity[S]
func (h Entity[S]) RetainInto(owner *Scope) Entity[S]
func (h Entity[S]) Release()
func (h Entity[S]) TransferInto(owner *Scope)
func (h Entity[S]) Downgrade() WeakEntity[S]
func (h WeakEntity[S]) UpgradeInto(owner *Scope) (Entity[S], bool)
func (cx *Context[S]) Entity() Entity[S] // callback-borrowed facade
func (cx *Context[S]) Scope() *Scope    // entity's dependent-resource scope
func (s *Scope) Child() *Scope
func (s *Scope) Close()
```

Each independently owned `Entity` contains a shared lease token registered with exactly one live scope. Assignment aliases that token: releasing either copy invalidates both and decrements once. `RetainInto` creates another lease/count; `TransferInto` moves the same lease to a live scope without decrementing or changing aliases. `Release` and `Close` are idempotent. There is no ownerless entity detach. Closed/wrong-app owners and stale strong handles are programmer errors. A zero weak handle fails upgrade; a dead identity never revives. Slot reuse changes generation, including after a failed construction.

Scopes form an acyclic ownership tree and carry stable cancellation tokens. Roots belong to the app and its windows; every entity also has a dependent-resource scope closed when that entity releases. That dependent scope is not a child of the first window that happened to create the entity: an independent lease in another window keeps the entity and its dependent resources alive. Do not put a self-retaining lease into the entity's own dependent scope. Child scopes allow shorter lifetimes without creating another entity.

An entity transitions `Live(count > 0) → ReleaseQueued(count = 0) → Releasing → Released`. Zero is irrevocable. Enqueue order determines release order; dependent releases join that queue. Release callbacks receive the still-available state after its observers/listeners/invalidation links are removed. Mark its dependent scope closing and cancel work before callbacks, then release outgoing dependencies after callbacks. Hooks cannot resurrect the entity or add resources to its closing scope. Partial construction failure disposes its dependent scope and lease without publishing the incomplete entity or delivering normal release hooks.

Scope close first marks the whole closing subtree unavailable to new ownership/result delivery, then cancels registrations/tasks, then closes children and releases resources in reverse acquisition order. Actual entity hooks run at the next eligible effect-flush boundary, never in the middle of an active access. A close initiated outside an update enters the app's foreground update/flush machinery. Cleanup uses ordered records, not Go map iteration. This ordering is a Go selection; it is not an assertion about Rust hash-map destruction order.

Strong cycles are not collected. Use weak backedges, an external graph-owner scope, or an explicit application disposal operation that breaks an edge while entities are still live. `OnRelease` cannot solve a cycle that prevents its own invocation. Debug diagnostics should report remaining leases/scopes and cycles at shutdown; they do not silently collect them.

## Borrowing and retained callbacks

`Context.Entity()` and source handles supplied to observer/subscriber callbacks are callback-borrowed facades. Copies share the callback's validity token and expire on return. Access is allowed during that callback; saving an owned handle requires `RetainInto`. Releasing or transferring a borrowed facade is misuse. Downgrading produces a persistable weak identity. Temporary internal pins protect each active callback's state, and are released on normal return or panic; they do not leak one owned lease per render. A callback may close an external owner while its current access finishes, but cannot register more work into that closing owner.

Reads can nest, writes are exclusive, and conflicting reentry panics as selected in the [authoring contract](authoring-decision-round.md). A fresh context is issued for each delivery. `*S`, `*Context`, `*Window`, and callback payload pointers must not escape their permitted access; Go cannot enforce arbitrary pointer capture. Panic unwinding restores framework bookkeeping, not application data. Do not flush further application callbacks while unwinding a panic; restore the boundary and propagate it.

Keep capture strength explicit. `Listener` uses weak identity. A retained processor requires a caller-supplied owning scope; its closure retains the entity until cancellation/scope close. `OnNextFrame` uses a window-owned one-shot registration with an independent entity lease, released after execution or cancellation/window closure. Closing an unrelated lease must not cancel that strongly retained callback. This follows the distinct [pinned context adapters](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/context.rs#L252-L301).

`ViewOf(entity)` retains independently into the active frame-construction scope associated with the entity's app; absence of that foreground construction boundary is programmer misuse and panics. `ViewOfIn(owner, entity)` supplies an explicit owner for recipes constructed elsewhere. A recipe's resource scope follows its committed placement, closes if unused/abandoned, and is transferred or retained across cache reuse. Thus copying an entity into an adapter is not mistaken for retaining it, and releasing the caller's lease after adapter construction does not destroy the view. These are scoped convenience rules, not GC-based recipe destruction. The same rule applies to framework recipe helpers that capture strong focus/resource handles; purely stylistic construction requires no app scope.

Focus handles use independent logical leases and weak identities as well. Releasing the final strong focus lease schedules focus cleanup before the next effect; affected windows blur through normal focus notification machinery. Focus APIs and native callbacks must honor the owning app/thread. Detailed Windows focus/input integration remains its existing decision.

## Subscriptions and tasks

`Subscribe`, `SubscribeSelf`, and `Observe` default to the observer entity's dependent scope. Discarding a Go subscription variable does not cancel anything. The scope retains the registration, whose endpoints remain weak. An app-level registration takes an explicit scope. Window-only bindings transfer into a window or child scope; an entity-owned registration does not become window-owned merely because it was created while rendering a window.

Subscription handle copies alias one cancellation token. `Cancel()` is idempotent; `TransferInto(scope)` changes its cancellation owner; `Detach()` transfers to an app-owned registration registry until cancellation or endpoint release. Detach does not retain either endpoint. `Join` requires one common owner (transfer explicitly first) and groups cancellation without changing endpoint or activation rules. Canceling the joined handle cancels all members; canceling an original alias cancels only that member. Transfer/detach of the joined handle moves its group; member aliases retain cancellation authority but may not independently transfer while grouped. Destroying one of two observers never cancels the other's registration.

New registrations activate through a queued defer. Delivery visits active registrations in registration order; check cancellation and endpoint liveness before each callback and again after it returns. Cancellation by an earlier callback skips a later callback in that traversal. New registrations are not inserted into the active traversal. Cancellation cannot undo the callback already running. See the [pinned subscriber set](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/subscription.rs#L62-L180).

Tasks likewise belong to an explicit scope, defaulting to the originating entity's dependent scope. Handle copies alias the task. `Cancel()` signals cancellation and closes result delivery, without waiting for worker exit. `Detach()` atomically transfers cancellation ownership to the app's detached-task registry; it does not remove an explicitly window-bound result gate or retain a weak target. Registries remove completed tasks. The app can cancel them at shutdown.

Workers receive owned inputs, cancellation context, weak identities and a posting capability, never escaped UI state/context/window pointers. Explicit task dependencies have a separate execution scope retained until actual worker termination; canceling the result scope does not free memory still being used. Workers post results/completion; only foreground code changes entity leases. Delivery checks the current task delivery token, target generation and optional window token immediately before obtaining access. Closing a scope suppresses late delivery even if the target entity has another live owner. A result accepted before close executes normally; close and delivery are serialized.

Execution resources remain registered until the foreground acknowledges completion. That handoff first transfers explicitly declared output dependencies into a message-owned result scope, then closes execution ownership. The result scope closes after delivery or discard; cancellation cannot leave a queued result pointing into freed execution storage. Copying an arbitrary result value does not discover or transfer nested handles. Worker posts carry completion/ownership records; workers do not mutate scopes themselves. Shutdown continues accepting terminal completion/retirement messages after rejecting new application work.

Cancellation is cooperative: a worker may ignore it forever. [Go cancellation](https://pkg.go.dev/context#CancelFunc) does not wait for termination. Do not claim force-killing goroutines or bounded successful shutdown of arbitrary application workers. Framework/native work must expose completion or safe abort; shutdown keeps required dependencies alive until that contract is satisfied.

## Effects and payloads

Keep a FIFO effect queue and update nesting counter. Only the outermost successful update exit starts flushing, and callbacks during flushing cannot recursively start another flush. An iteration drains released entities, processes released focus handles, then applies one effect. Newly queued effects append; repeat until quiet. App/global observers and refresh effects retain their source-specific behavior; they are not folded into a once-per-frame notification system. This follows the [pinned effect loop](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app.rs#L1743-L1830).

- `Notify` coalesces while that entity is in the pending set. Remove it before invoking observers, allowing a callback to enqueue another notify. Global notifications similarly coalesce by declared global type.
- `Emit` queues each occurrence using source identity and the declared payload type, including interface payloads. It neither implies notify nor retains the source merely for routing. A released source has no remaining subscriptions to deliver to.
- `Defer` appends in call order, including nested defers. An already queued effect precedes a newly deferred callback.
- Mutation through ordinary `Update` does not automatically notify. Keep explicit notification behavior for operations that separately promise it.

`Emit` copies the value `P`, not its reachable object graph. The producer freezes reachable mutable data until the effect cycle retires its payload storage; subscribers treat it as read-only and must clone data they need to mutate or retain independently. Later producer mutation of a shared slice/map is a contract violation. Immutable scalar copies remain usable. The typed envelope preserves nil interface payloads and declared dispatch identity.

Arbitrary payload fields are not scanned for entity/native handles. Add `event.EmitOwned(cx, func(payloadScope *Scope) P)` when an event needs independent owned dependencies: the factory explicitly retains into that scope. It has the same source/payload typing and dispatch identity as `Emit`. Factory failure closes partial ownership and enqueues nothing. Subscribers retaining a payload entity beyond delivery must retain into their own live scope. Basic `Emit` neither retains nor releases nested handle aliases.

Payload scopes last until the entire effect cycle drains, including when individual emissions are discarded. Retire them in emission order at the tail, matching the source's arena lifetime. The pin clears its event arena after the last release scan; payload-held entities can consequently reach zero after that scan. Preserve that boundary and schedule a subsequent foreground flush when retirement creates pending releases, rather than claiming the pin drains those releases in the same cycle. The extra wakeup is a documented Go liveness adaptation.

## Foreground, frames, and native resources

Run app mutation, logical lifetime transitions, input dispatch and frame construction on one foreground goroutine locked to the owning OS thread. Native callbacks enter that dispatcher and may not access state directly from arbitrary threads. Foreground runnable tasks are FIFO; idle tasks are separate and do not promise progress under constant load. Locking is the Go mechanism for thread affinity ([runtime documentation](https://pkg.go.dev/runtime#LockOSThread)); Windows loop/COM details remain the platform decision.

Retained element state is keyed by window identity, global element path and state type, with a generation for each live entry. A completed frame carries accessed entries forward and retires unaccessed entries. Removing an element for a committed frame then reinserting the same key creates fresh state and a new entry generation, independently of entity-slot generations. A conditional subtree that disappears and returns before any committed frame has removed it has not necessarily lost its state.

Cache reuse must preserve retained state, dependency subscriptions, dispatch/input callbacks, hitboxes and scene artifacts, not just pixels. Validate the source cache conditions (dirty/refresh status, bounds, mask, text style). Transfer or independently retain these resources before closing prior-frame ownership. Nested draws have separate construction scopes. An abandoned build closes uncommitted resources and keeps the previously published artifacts valid; arbitrary application mutations during rendering are not rolled back. This failure-handling rule is a Go adaptation, not an upstream transactional guarantee. See [view caching](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/view.rs#L354-L398) and the [window snapshot](../evidence/core/crates__gpui__src__window.rs).

Logical resource release and physical native destruction are distinct. Each submitted frame/work item holds explicit resource leases until backend-confirmed completion or safe abort. Neither GC, arena reset, nor an assumed number of elapsed frames proves completion. Retirement happens on the resource's required thread. App shutdown closes delivery scopes, cancels tasks, tears down windows and drains framework completion/retirement work before destroying native owners. Backend-specific fences, native pointer representation and teardown mechanics belong to the platform/renderer/distribution decisions; this contract constrains those choices.

## Verification obligations

The deterministic test dispatcher controls runnable selection (including seeded background interleavings), virtual time, timers and worker completions. External input must be injected or recorded/replayed; one UI thread alone cannot make real OS/I/O races reproducible. Include tick/run-until-parked, clock advance, parking diagnostics and configured worker-count behavior.

Conformance must cover independent two-window leases; alias release; rejected resurrection; a reported strong cycle; borrowed-handle expiry; strong versus weak callbacks; source/observer death with queued events; deferred activation; notify coalescing and re-notify; cancel-next during delivery; payload ownership and tail retirement; late worker completion; same-key removal/reinsertion; cache dependency reuse; abandoned frames; focus release; and native work still in flight during shutdown. [Conformance planning](../.scratch/gpui-core/issues/09-define-conformance-and-update-policy.md) owns those runtime checks. [Signature validation](../.scratch/gpui-core/issues/10-validate-authoring-signatures.md) owns compiler proof. No listed behavior has been executed here.
