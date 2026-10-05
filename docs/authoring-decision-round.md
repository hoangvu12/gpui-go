# Selected Go authoring contract

Status: selected documentation contract; authoring decision resolved, 2026-10-04. [Choose the Go language and authoring contract](../.scratch/gpui-core/issues/03-choose-go-authoring-contract.md) records the decision and rationale. Selected declarations and external call sites were compiler-validated with installed Go 1.27.1 on 2026-10-05 in the [isolated signature fixture](../.scratch/signature-validation/README.md). Operation bodies are stubs; this is not runtime implementation or behavioral proof. [Recorded validation](../evidence/authoring-signature-validation.json) preserves commands, diagnostics and source hashes.

The user accepted Go 1.27, convenient string children, constructor-bound fluent styling, and typed event declaration receivers. Subsequent technical choices below were selected under the user's research-first delegation, using Roboco `pi` / `iroha/dashscope/glm-5.3`, main-chat source review, and the [completion](../research/12-authoring-contract-completion.md) and [closure](../research/13-authoring-closure-contracts.md) reports. The CE parity pin is unchanged.

## Fluent authoring and extension

Keep value-embedded `Styled[*Component]` with constructor-time `BindStyled`; shared style methods retain the concrete pointer type without mandatory author generation. Style refinements preserve absent, explicit zero/false, and clear states where applicable. Only the bounded [styling helper](../authoring/README.md) is implemented.

Keep `Render[S]` for entity state, `RenderOnce` for component recipes, a public `View` for properties plus optional backing identity, and `Element[L, P]` for low-level typed phases. Parent properties are ordinary view fields.

```go
type Render[S any] interface {
    Render(*Window, *Context[S]) AnyElement
}
type RenderOnce interface { RenderOnce(*Window, *App) AnyElement }
type View interface {
    EntityID() (EntityID, bool)
    RenderOnce(*Window, *App) AnyElement
}
func ViewOf[S any, PS interface{ *S; Render[S] }](entity Entity[S]) View
func ViewOfIn[S any, PS interface{ *S; Render[S] }](owner *Scope, entity Entity[S]) View
func CustomElement[L, P any](impl Element[L, P]) AnyElement
```

Recipes get a stateless wrapper; Go does not add identity methods automatically. The entity adapter connects `*S` to `Render[S]`; auxiliary pointer-parameter inference passed the compiler fixture. Entity handles identify shared state; component recipes describe a placement. Same-entity sibling views must not collide in the same parent identity path.

`ViewOf` acquires an independent lease in the active foreground frame-construction scope; `ViewOfIn` names an owner when constructing elsewhere. Accepted placements carry that ownership forward; unused/abandoned recipes release it with their construction scope. This preserves the short render-time call without treating Go assignment as a clone. See the [runtime ownership contract](runtime-ownership-contract.md) for scope and cache rules.

The custom-element interface carries optional element identity/source location, optional global/inspector identities through all phases, typed mutable layout/prepaint state, and accessibility companions for optional role, hidden subtree, properties, and synthetic children receiving mutable prepaint state. See the complete candidate in report 12 and the [pinned trait](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/element.rs#L51-L152). Preserve restricted-parent child capabilities. Source-location capture can use a caller-supplied location or runtime capture; no compile-time Go guarantee is implied.

## Context, listeners, and controlled access

Use a shared nongeneric access carrier. Public entity operations remain typed; the carrier conveys the application and scope check, not untyped entity state.

```go
type AppContext interface { access() appAccess } // private framework carrier
// *App and *Context[T] implement AppContext.
// External wrappers may embed/delegate an existing carrier.

func (h Entity[S]) Read[R any](cx AppContext, f func(*S, *App) R) R
func (h Entity[S]) Update(cx AppContext, f func(*S, *Context[S]))
func (h Entity[S]) UpdateWith[R any](cx AppContext, f func(*S, *Context[S]) R) R

func (h WeakEntity[S]) TryRead[R any](cx AppContext, f func(*S, *App) R) (R, error)
func (h WeakEntity[S]) TryUpdate(cx AppContext, f func(*S, *Context[S])) error
func (h WeakEntity[S]) TryUpdateWith[R any](cx AppContext, f func(*S, *Context[S]) R) (R, error)

func (cx *Context[T]) Listener[E any](
    f func(*T, *E, *Window, *Context[T]),
) func(*E, *Window, *App)
func (cx *Context[T]) Observe[S any](
    source Entity[S], f func(*T, Entity[S], *Context[T]),
) Subscription
```

`Update` accepts ordinary void mutation callbacks; `UpdateWith` returns a callback result. The Go visual forms take an explicit window: `UpdateIn(cx, w, func(*S, *Window, *Context[S])) error`, with weak `TryUpdateIn` and result-returning `UpdateInWith[R]` / `TryUpdateInWith[R]` following the same convention. They report missing/closed windows distinctly from released weak entities; nil/unavailable window is `ErrNoWindow`, closed window is `ErrWindowClosed`. This explicit-window spelling is a Go adaptation, while the ability to update with window context follows upstream. These signatures describe access, not what keeps a handle alive.

Operations check foreground access, application identity, scope validity, and conflicting same-entity access. Violations panic as programmer misuse. Weak access reports `ErrEntityReleased`; it does not turn stale-context misuse or arbitrary callback panics into ordinary errors. Reads register dependencies and callbacks treat state as read-only. Updates do not implicitly notify unless an operation explicitly promises it.

Read-only access can nest; mutation is exclusive. Accessing an entity through another handle while its update callback is active, or updating it while a read callback is active, is a conflict. Callbacks use the state pointer already provided for their current access.

A listener captures weak logical identity and supplies a fresh context at delivery; a released target is a no-op. Go cannot stop application code retaining or mutating a borrowed `*S`; that violates the access contract and is not claimed to be fully detectable. Panic unwinding must release framework bookkeeping without rolling back callback writes. The [pinned listener](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/context.rs#L252-L261) and [entity access](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/entity_map.rs#L460-L505) supply the reference.

## Entity events

```go
var countChanged = gpui.DefineEvent[Counter, CountChanged]()
countChanged.Emit(cx, CountChanged{Value: c.count})
subscription := countChanged.Subscribe(summaryCx, counter, (*Summary).changed)
```

`Event[Source, Payload]` is stateless with a valid zero value. Dispatch is keyed by entity identity and declared payload type, never declaration address or the dynamic member of an interface payload. A private defined field type depending on both parameters prevents ordinary cross-pair conversion; ordinary cross-pair conversions failed the negative compiler cases, including pairs whose payloads have identical underlying structure. Repeated declarations for the same pair communicate. Typed envelopes retain nil interface payloads through internal erasure.

`Emit` checks source/context/payload types; `Subscribe[Observer]` also retains observer/callback typing. `EmitOn` supplies the app-context emission path and `SubscribeSelf` supplies self-subscription. Declarations are not authority tokens: callers can declare pairs, unlike Rust coherence restrictions. The managed-view contract combines rendering, focus and `DismissEvent() Event[S, DismissEvent]`; generic helpers constrain the receiver to `*S`.

Plain emission copies `P`; mutable reachable data stays frozen until end-of-cycle payload retirement, and subscribers treat it as read-only. Arbitrary nested entity/native handles are neither automatically retained nor released. `event.EmitOwned(cx, func(payloadScope *Scope) P)` provides explicitly owned payload dependencies, retained by the factory into that scope and released at cycle retirement. Subscription registration defaults to the observer entity's dependent scope, with explicit cancellation and ownership transfer. [Runtime ownership](runtime-ownership-contract.md) defines ordering and late-delivery rules.

## Lifetime-facing authoring rules

`NewEntity(cx, owner, build)` creates one scope-owned lease. Copying `Entity[S]` aliases that lease; `RetainInto(owner)` creates independent ownership, `TransferInto(owner)` moves it, and `Release()` releases it once. `Downgrade()` produces weak identity; `UpgradeInto(owner)` acquires ownership only while logically live. GC never determines these transitions.

`Context.Entity()` and callback source handles are borrowed for that callback. Persist by explicitly retaining; releasing/transferring a borrowed facade is misuse. `Context.Scope()` returns the entity's dependent-resource scope, which survives as long as the entity has independent owners, even if its original window closes. Context/state pointers remain callback-limited.

`Subscription.Cancel()` is idempotent. `TransferInto(scope)` changes its cancellation owner; `Detach()` transfers to an app registry while retaining weak endpoints. Ignoring a subscription variable leaves the scope-owned registration active. Tasks have analogous scope cancellation and explicit app-registry detach, with cooperative worker cancellation and a foreground result gate. Strong processors require an owner; one-shot `OnNextFrame` registrations retain through the window's callback owner. `Listener` stays weak. Full rules and deliberate Rust/Go differences live in the selected runtime contract.

## Action definitions, dispatch, and registration

Define one canonical descriptor per payload type, once per process. Import or copy that declaration for reuse; every second `DefineAction[A]` is a definition error, even if the name matches. This prevents inconsistent clone/equality functions without trying to compare functions. The descriptor is immutable, its zero value invalid, and its services must not depend on transient application state.

Rich descriptors require explicit `Clone` and `Equal`; the unit helper restricts payloads to `~struct{}` and accepts the full external name:

```go
var increment = gpui.DefineUnitAction[Increment]("counter::Increment")
div.OnAction(increment, cx.Listener((*Counter).incrementAction))
w.Dispatch(increment, Increment{}, cx)
// Named JSON construction is separate:
app.RegisterActions(increment)
```

Handler and dispatch calls carry the descriptor. Routing remains keyed by payload type, preserving typed unregistered actions. Registration enables named construction and may accept the same canonical descriptor idempotently; conflicting names, types and deprecated aliases are setup errors. JSON-disabled and unregistered are independent choices. `BuildAction(name, data)` returns a boxed instance and an error distinguishing unknown name, disabled JSON and decode failure. Keep optional schema, documentation and deprecation metadata. Reference-facing action names are independent of Go module branding.

`AnyAction` erases a descriptor; `BoxedAction` erases a descriptor plus payload. `a.Box(value)` uses its clone service for owned storage. `OnBoxedAction(bound, handler)` clones the bound instance and routes by its type, passing that stored clone to the handler, as the [pinned boxed handler](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/elements/div.rs#L497-L519) does. It must not silently substitute the incoming payload. Multiple handlers still obey capture/bubble and propagation rules; their mere registration does not guarantee all run.

## Child conversion and attachment

Compiler validation caught the package-level `Div` naming collision: preserve the accepted `Div()` constructor and name its concrete builder type `DivElement`. This is a spelling correction; fluent calls are unchanged. `StyleRefinement` shares the existing authoring type identity through an alias, avoiding a root/authoring dependency cycle.

Use one precedence: reject nil of every nil-capable kind and invalid zero handles; accept valid `AnyElement`; honor explicit `IntoElement`; convert strings; adapt identity-bearing `View`; then stateless `RenderOnce`. No implicit arbitrary formatting, raw entity conversion, or dynamic discovery of all `Element[L, P]` instantiations. Use `ViewOf` and `CustomElement` at those typed seams.

```go
type IntoElement interface { IntoElement() AnyElement }
func Div() *DivElement
func TryIntoElement(value any) (AnyElement, error)
func OwnRecipe(recipe RenderOnce) AnyElement
func (d *DivElement) Child(value any) *DivElement
func (d *DivElement) Children(values ...any) *DivElement
func (d *DivElement) TryChildren(values ...any) (*DivElement, error)
func (d *DivElement) TryChildrenSlice[T any](values []T) (*DivElement, error)
```

Fluent methods panic with type and parent/child location on invalid input; checked methods return conversion/attachment errors. All paths use the same checks. A framework handle has a shared attachment token: aliases of it remain one attachment. Conversion creates an unattached handle; parent commit marks it attached; first expansion consumes its recipe. Reusing an attached handle fails immediately, before expansion.

Use `OwnRecipe` once for a third-party mutable recipe when shared-handle misuse diagnostics are needed. Bare recipes remain convenient, but independently wrapping the same raw object or copying arbitrary third-party values cannot be universally detected. There is no claimed global pointer-provenance registry. Authors must honor single-use semantics; explicit converters must preserve their owned token or honor that contract. The same limitation applies to independently wrapping one raw custom element.

For each parent operation, hold a child-mutation/conversion gate, convert candidates, validate/reserve all tokens, then commit without further application callbacks. Reject duplicates, attached handles, invalid values and reentrant child mutation/conversion of that destination. Failure or panic releases reservations and the gate; successful commit attaches all candidates. The destination's child list is unchanged on failure. Previously supplied fresh handles stay reusable; temporary wrappers may be discarded. Concurrent builder use is unsupported.

Converters can capture and mutate other framework or application objects; the framework cannot roll those effects back. No catch-all panic recovery or guaranteed retry of the same raw converter input is promised. `TryIntoElement` alone holds no reservation across calls. Upstream uses a lazy mapping iterator in `children`; transactional staging is a deliberate Go adaptation, not a property proved by that Rust call.

## Decision boundaries and verification

The [fixture API](../.scratch/signature-validation/api.go) records the complete compiled phase signatures, typed accessibility companions, `ManagedView[S]` plus its constrained adapter, event-pair field, action descriptors, and window-aware access return types. It also fixes the exact observer callback as `func(*Observer, Entity[Source], *Payload, *Context[Observer])`. `Element` phases carry optional global/inspector IDs; the shorter counter excerpt is illustrative only. Positive calls infer auxiliary pointers and method type parameters; explicit spellings compile too. Negative cases cover descriptor relabeling, wrong source/payload/observer, non-renderable/managed views, action constraints, custom phase states, restricted children and the private access carrier.

The [layout contract](layout-contract.md) keeps native Taffy behind the existing Go window/element surface. Layout IDs are frame/engine-generation scoped; callbacks receive logical-pixel known dimensions and per-axis available-space modes. Element authors do not manage DLL pointers, and a measurement callback must not re-enter its busy layout engine. Layout/ABI conformance is a separate obligation from compiling the authoring signatures.

These are compiler-validated authoring selections, not a claim of implemented behavior. The [signature-validation task](../.scratch/gpui-core/issues/10-validate-authoring-signatures.md) covers positive external-package examples and negative constraints. Behavioral cases include conflicting action definitions, bound-versus-dispatched boxed payloads, same-token duplicates, converter reentrancy, failed batches and panic cleanup; conformance planning must retain them.

[Define entity ownership, effects, and scheduling](../.scratch/gpui-core/issues/04-define-runtime-ownership.md) records the selected [runtime contract](runtime-ownership-contract.md) for retention, disposal, cancellation, effects, payload ownership, thread affinity and persistent/frame state. Its lifetime-facing declarations passed the signature fixture; the behavior remains unimplemented.

The user installed Go 1.27.1 themselves and authorized the signature fixture on 2026-10-05. Root and fixture tests/vet run with the local compiler and network/toolchain fetching disabled. Existing styling tests execute; the counter and broader API functions are typechecked only. No native or broader runtime implementation occurred.
