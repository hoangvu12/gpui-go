# Authoring contract completion: descriptor, actions, children, view/element shape

Status note after Wayfinder closure: this is supporting research from before the final access/action/attachment selections. The [selected authoring contract](../docs/authoring-decision-round.md) and [closure research](13-authoring-closure-contracts.md) supersede its remaining-open wording and any intermediate candidate choices. Compiler proof remains outstanding.

Research date: 2026-10-04; revised the same day after main-chat review. Documentation-only completion work for [the authoring decision](../.scratch/gpui-core/issues/03-choose-go-authoring-contract.md), filling the four engineering gaps left open by the [decision round](../docs/authoring-decision-round.md). The typed declaration receiver (`countChanged.Emit(cx, payload)`, `countChanged.Subscribe(...)`) is the accepted foundation; this report proposes the descriptor contract and adjacent authoring contracts. All Go signatures are **proposals, uncompiled**, and this report does not complete every signature: exact context/entity access, error, and action-handler association details, plus lifecycle, remain review items. No runtime, renderer, compiler probe, toolchain change, or implementation is part of this work; the CE pin `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a` is unchanged and the Zed comparison pin remains evidence only. Sources were read directly from the pinned snapshots in `evidence/core` and `evidence/original-gpui-authoring` (manifests record provenance).

## 1. Event descriptor: identity, zero value, export, and dismissal

What the pin settles: `EventEmitter<E>` is a marker trait; `Context::emit` and `subscribe` are bounded by it; the callback receives `(state, emitter entity, &event, context)`. Emission allocates the payload in an arena and queues `Effect::Emit { emitter: EntityId, event_type: TypeId, event }`; each emission is one effect, and `apply_emit_effect` delivers only to subscribers whose stored type equals the emitted `TypeId`, with `downcast_ref().expect("invalid event type")` inside each handler. The runtime topic is therefore the pair `(EntityId, TypeId of the declared payload type)` — not the payload's dynamic type. [CE marker](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/gpui.rs#L325-L329), [CE emit](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/context.rs#L765-L775), [CE subscribe](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/context.rs#L98-L118), [effect queue](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app.rs#L3043-L3066), [dispatch filter](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app.rs#L1884-L1895), [subscriber keying](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app.rs#L1271-L1296). `ManagedView: Focusable + EventEmitter<DismissEvent> + Render` is a blanket trait with a unit `DismissEvent`; a grep across the *selected evidence snapshots* finds no other use of either name — the snapshots, not the whole CE crate, were scanned, so additional internal uses are not ruled out. The contract must be exposed regardless. [CE contract](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/window.rs#L724-L742).

Recommended descriptor:

```go
// Documentation syntax; uncompiled.
type eventPair[Source, Payload any] struct{}
type Event[Source, Payload any] struct{ _ eventPair[Source, Payload] }
func DefineEvent[Source, Payload any]() Event[Source, Payload]

func (e Event[S, P]) Emit(cx *Context[S], payload P)
func (e Event[S, P]) EmitOn(app *App, entity Entity[S], payload P)
func (e Event[S, P]) Subscribe[Observer any](
    cx *Context[Observer], source Entity[S],
    callback func(*Observer, Entity[S], *P, *Context[Observer]),
) Subscription
func (e Event[S, P]) SubscribeSelf(cx *Context[S], callback func(*S, *P, *Context[S])) Subscription
```

- **Identity.** The value carries no state; the runtime topic is `(EntityId, reflect.TypeFor[P]())`, mirroring the pinned `(EntityId, TypeId)` key exactly. Repeated `DefineEvent[Counter, CountChanged]()` calls, and independent declarations of the same pair in different packages, all denote the same topic per entity. Two independently declared descriptors therefore *communicate*: a subscriber registered through one receives emissions made through the other, because delivery filters by entity id and payload type — never by descriptor value or declaration site. `EmitOn` covers upstream's context-free `AppContext::emit` path. ([reflect.TypeFor](https://pkg.go.dev/reflect#TypeFor), Go 1.22, inside the accepted 1.27 floor.)
- **Zero value.** `var countChanged gpui.Event[Counter, CountChanged]` is valid and equal to every other value of that instantiation: the struct is comparable with a single zero-size field, so there is exactly one value per instantiation. `DefineEvent` is declaration sugar and documentation anchor, not initialization. No package-init order hazards.
- **Relabeling.** A bare `type Event[S, P any] struct{}` would be dangerous: Go converts between types with identical underlying types regardless of their names, so `Event[Counter, CountChanged](x)` from a differently-instantiated descriptor would compile. The unexported `eventPair[Source, Payload]` field makes the underlying types of distinct instantiations differ, so cross-pair explicit conversions are illegal. This is spec reasoning ([conversions](https://go.dev/ref/spec#Conversions)), not a compiler check; `unsafe` circumvention is out of scope.
- **Interface payloads.** A declaration `Event[S, fmt.Stringer]` keys on the interface type itself: all emissions share one topic, subscribers receive `*fmt.Stringer`, and no dynamic fan-out into concrete types occurs. The erased queue must retain a typed envelope such as `eventEnvelope[P]` rather than only the interface's dynamic value. This preserves even a nil interface payload and allows delivery of `*P` without a failing assertion on a nil `any`. Envelope mismatches are internal invariant failures; payload mutability remains a lifetime-review item.
- **Export and authority.** Any package can construct `Event[Source, Payload]` for any pair. Go has no equivalent of Rust's coherence/orphan rules restricting where declarations live, so the descriptor is a claim, not an authorization token. This is a Go adaptation of the accepted declaration direction, not a user-accepted divergence: treating the descriptor as a claim rather than an authorization is this report's recommendation, recorded for the decision record, and runtime lifecycle checks remain independent and required.
- **ManagedView.** Keep `type DismissEvent struct{}` exported and translate the trait with a declaration accessor:

```go
type ManagedView[S any] interface {
    Render(*Window, *Context[S]) AnyElement
    FocusHandle(*App) FocusHandle
    DismissEvent() Event[S, DismissEvent]
}
```

The author opts in by implementing the accessor returning their typed pair (`func (m *Modal) DismissEvent() gpui.Event[Modal, gpui.DismissEvent]`) and emits with `modalDismiss.Emit(cx, gpui.DismissEvent{})` — the shared-payload case the declaration design exists for. The accessor is named `DismissEvent()` to read as a declaration accessor, not an imperative dismiss action. Downstream helpers must bind the receiver to the state type through the pointer constraint: accepting an unconstrained `V ManagedView[S]` alongside a separate `Entity[S]` would let the render, focus, and dismissal capabilities belong to a different receiver state than the entity being presented. `func Present[S any, PS interface{ *S; ManagedView[S] }](view PS, ...)` ties them to one state type. These are ordinary interface type parameters in method signatures, not generic methods; still unvalidated by a compiler.

`Subscription` return values, drop/detach/join semantics, weak-observer capture, and delivery scheduling are decision 04 territory; only the shapes above are proposed here. ([upstream Subscription](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/subscription.rs#L150-L180))

## 2. Actions: descriptor services separated from registration

The pin's contract: unit actions come from a macro deriving `Clone + PartialEq + Default + Debug` plus registration under `namespace::Name`; richer actions derive `Action` and *must* implement clone, partial equality, `name`/`name_for_type`, `build(serde_json::Value)`, and optionally JSON schema, deprecated aliases, a deprecation message, and documentation. Attributes cover `namespace`, `name` (no `::`), `no_json`, `no_register`, `deprecated_aliases`, `deprecated`. Linked registrations are collected through `inventory`, and `ActionRegistry::default` loads them during registry initialization; duplicate names and alias collisions panic; `build_action` returns `NotFound`/`BuildError`; the registry also serves schemas, aliases, and docs. Registration "only allows for actions to be built dynamically, and is unrelated to binding actions in the element tree" — typed `on_action` dispatch is keyed by `TypeId` with an internal downcast, and a dynamic boxed path clones the action. [CE action trait](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/action.rs#L117-L170), [attributes](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/action.rs#L60-L103), [registry](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/action.rs#L233-L334), [name-lookup separation](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app.rs#L2363-L2372), [typed dispatch](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/elements/div.rs#L484-L495), [boxed dispatch](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/elements/div.rs#L497-L511).

Recommendation: a typed descriptor carrying the stable external name and payload services, with registration as a separate step:

```go
// Documentation syntax; uncompiled.
func DefineAction[A any](spec ActionSpec[A]) Action[A]

type ActionSpec[A any] struct {
    Name          string              // stable external name, e.g. "counter::Increment"
    Clone         func(A) A           // required
    Equal         func(a, b A) bool   // required
    FromJSON      func(json.RawMessage) (A, error) // optional; omitting it is NoJSON
    Schema        func() any          // optional
    Aliases       []string            // deprecated names; must not collide
    Deprecation   string
    Documentation string
}

type Action[A any] struct{ /* immutable descriptor carrying the spec */ }
func (a Action[A]) Name() string
func (a Action[A]) Clone(v A) A
func (a Action[A]) Equal(x, y A) bool

// Unit helper: for empty-struct payloads, trivial services are exact.
func DefineUnitAction[A ~struct{}](name string) Action[A]

// Registration for name-based dynamic construction (keymaps); separate.
func (app *App) RegisterActions(actions ...AnyAction) // AnyAction is the erased descriptor
```

- **Clone and equality are explicit, with no defaults.** `Clone func(A) A` and `Equal func(a, b A) bool` are required fields for parameterized actions: a default shallow copy would silently alias pointer or slice fields, and a default deep comparison would silently accept map/slice semantics the author never chose. Requiring the author to state the payload's copying and equality behavior removes both hidden behaviors. The unit helper may safely provide trivial services because an empty-struct payload has exactly one value: clone returns it and equality is always true. Rust's derived `Clone` is field-wise and can share (an `Arc` field shares on clone), so deep copying is not a universal Rust property either; the Go contract just makes the choice explicit. The dynamic boxed path (the `on_boxed_action` translation) obtains owned values through the descriptor's `Clone`, preserving that service.
- **Registration is separate from the descriptor.** An unregistered action still has name, clone, and equality services and still dispatches through typed handlers; `no_register` is simply not calling `RegisterActions`, and `NoJSON` is omitting `FromJSON`/`Schema` (building that action by name then errors, matching the pin's `no_json`). Duplicate names or alias collisions panic during registration at `App` setup, matching the pin. The unit helper takes the full stable external name, such as `"counter::Increment"`; it does not infer persistent names from Go type spelling. Its candidate `~struct{}` constraint restricts trivial services to empty-struct payloads, pending compiler validation.
- **Descriptor/handler association still needs final design.** Upstream keys element listeners by the action *type* (`TypeId::of::<A>()`), independent of the registry. The Go equivalent could key by `reflect.TypeFor[A]()`, by descriptor value, or by name; this report does not settle it, and the choice interacts with the boxed path and keymap validation.
- Keep `zed::NoAction` and the parameterized `zed::Unbind(SharedString)` as framework-registered special actions carrying their keymap semantics — `Unbind` is the pin's own precedent for a payload action with JSON decoding ([CE special actions](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/action.rs#L413-L457)). Preserve these reference-facing names for compatibility; the public Go module path does not require changing persisted keymap action names. Schema representation (`func() any`) defers the schema-library selection to implementation time.

## 3. Child conversion: precedence, nil, bulk, and the checked path

The pin accepts children as `IntoElement` values; strings convert through concrete impls for `&'static str`, `String`, `Cow<'static, str>`, and `SharedString` — a closed convenience set, not arbitrary display formatting. Bulk conversion maps each item through `into_any_element` before `extend`. A `ParentElementTyped` trait exists in the pin with a `Child`-typed method set; it is a parity capability to translate, not an optional extra. [CE parent element](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/element.rs#L228-L250), [CE typed parent](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/element.rs#L200-L226), [original string impls](https://github.com/zed-industries/zed/blob/a84689073d296dfd39987bc7dd478e43ef76d83a/crates/gpui/src/elements/text.rs#L304-L325).

A dynamic `any`-typed seam cannot discover `Element[L, P]` implementations: the generic interface's instantiations are unknown at runtime, so no type assertion can find them. Low-level element authors therefore either implement a public conversion interface directly or wrap their element in the explicit `CustomElement[L, P]` adapter (§4). Recommended public seam and precedence:

```go
type IntoElement interface{ IntoElement() AnyElement }
```

1. nil of any nil-capable kind — untyped nil, a typed-nil pointer or interface (detected via reflection), and a zero/invalid `AnyElement` — → error;
2. a valid `AnyElement` → pass through;
3. `IntoElement` → its explicit conversion (low-level elements and external packages), validating the returned handle before use;
4. `string` → text element; no arbitrary `fmt.Stringer` conversion;
5. `View` → view element adapter, preserving entity identity and notification scope — checked **before** `RenderOnce` so an entity-backed view is never degraded to a stateless recipe;
6. `RenderOnce` recipe → the stateless view wrapper (§4);
7. `Entity[S]` → **not** implicitly converted; the explicit `ViewOf(entity)` adapter remains required, per the counter-view draft.

`Child(any)` panics on failure with a diagnostic naming the received `%T` and the parent's element ID when set; the opt-in checked path `TryIntoElement(value any) (AnyElement, error)` evaluates the same table. Typed bulk candidates:

```go
func (d *Div) Children(items ...any) *Div                 // panics on invalid items
func (d *Div) TryChildren(items ...any) (*Div, error)     // checked bulk
func ChildrenSlice[T any](items []T) ([]AnyElement, error) // staged typed conversion
```

Bulk conversion stages: every item is converted and validated into a slice *before* anything is attached, and duplicate or already-consumed handles (an `AnyElement` or recipe attached elsewhere) are rejected during staging. On error the destination parent is unchanged — nothing is attached. The limits are deliberate and stated: application-supplied converters (`IntoElement` implementations) can run arbitrary code whose effects the framework cannot roll back, and the framework performs no catch-all `recover` of converter panics — a panicking converter propagates. The remaining gap is honest: the reservation/consumption bookkeeping that would make duplicate/consumed detection reliable across frames is not designed here; it is the counter-view's future runtime responsibility, and per-call atomicity (earlier children in the same chain stay attached when a later call fails) is the guaranteed scope.

`ParentElementTyped` translation: restricted-parent elements expose `Child`/`Children` methods whose parameter type is the restricted child type — ordinary Go methods on the element builder — and framework code that must accept such parents generically uses a generic constraint interface carrying those method signatures. The capability is preserved for parity, with the constraint-interface spelling to be finalized with the element API.

## 4. View, properties, and the complete custom Element shape

The pin's `View` trait is the unifying model: `entity_id()` (identity → unique element-id space, `notify` scoping; `None` = stateless) and a consuming `render`. Blanket impls make every `RenderOnce` recipe a stateless `View` and every `Entity<T: Render>` an entity-backed `View`; hand implementation is reserved for components needing both parent-supplied props *and* backing-entity identity. `AnyView` erases the pair as entity + render function pointer, with `cached`, `downgrade`, `downcast`, and entity-based equality. [CE View](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/view.rs#L196-L216), [blanket impls](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/view.rs#L226-L266), [AnyView](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/view.rs#L19-L113).

Naming: Go cannot have a function and a type named `View` in one package, so the draft's provisional `gpui.View(counter)` is retired; the **type** keeps the upstream concept name. Identity uses an optional form rather than conflating a zero value with "absent":

```go
type View interface {
    EntityID() (EntityID, bool) // (zero, false) = stateless
    RenderOnce(*Window, *App) AnyElement
}
type Render[S any] interface{ Render(*Window, *Context[S]) AnyElement }

// Documentation syntax; uncompiled.
func ViewOf[S any, PS interface{ *S; Render[S] }](entity Entity[S]) View

```

`ViewOf[S Render]` is invalid because `Render` must be instantiated with a state type. In the counter contract the rendering implementation also lives on `*S`, so the entity's value-state parameter alone does not supply its method set. The auxiliary pointer parameter `PS` links `*S` to its `Render[S]` implementation — the same pattern research 11 identified for conditional receiver capabilities; whether `PS` infers from the call site is unvalidated. The adapter must acquire controlled entity access and a fresh `*Context[S]` before invoking `PS(state).Render`; its implementation body is intentionally omitted until access/error signatures are settled. Erasing the adapter carries its typed rendering behavior. `AnyView` translates as that erased pair, built by the adapter, with `Cached`, `Downgrade`, `Downcast`, and entity-based equality carried over.

A `RenderOnce` recipe alone does **not** satisfy `View`, because it lacks `EntityID`; the framework supplies a stateless wrapper (used by child-conversion row 6):

```go
type statelessView[R RenderOnce] struct{ recipe R }
func (statelessView[R]) EntityID() (EntityID, bool) { var id EntityID; return id, false }
func (v statelessView[R]) RenderOnce(w *Window, app *App) AnyElement { return v.recipe.RenderOnce(w, app) }
```

Properties need **no new API**: a hand-implemented `View` is an ordinary Go struct whose fields are the parent-supplied properties and whose `EntityID` returns the backing entity's id — the contract is the interface, not a props mechanism. The same-entity-siblings rule and `cached` (entity-backed only, notify-invalidated) carry over unchanged from the pin.

Custom elements get the complete capability shape, restoring what the counter draft abbreviated — the global and inspector identity parameters upstream passes into every phase, plus the optional hooks, with identity again in optional form:

```go
type Element[L, P any] interface {
    ID() (ElementID, bool)             // false = no identity
    SourceLocation() *SourceLocation   // nil unless provided
    RequestLayout(id *GlobalElementID, inspectorID *InspectorElementID, w *Window, app *App) (LayoutID, L)
    Prepaint(id *GlobalElementID, inspectorID *InspectorElementID, bounds Bounds, layout *L, w *Window, app *App) P
    Paint(id *GlobalElementID, inspectorID *InspectorElementID, bounds Bounds, layout *L, prepaint *P, w *Window, app *App)
}
func CustomElement[L, P any](impl Element[L, P]) AnyElement
```

Accessibility arrives as companion capability interfaces the adapter type-asserts at construction, with parameters matching the pin: `A11yRole() (Role, bool)` (upstream `Option<accesskit::Role>`), `IsA11yHidden() bool`, `WriteA11yInfo(*Node)` (upstream `&mut accesskit::Node`), and synthetic children via an instantiated generic interface `interface{ A11ySyntheticChildren(*P, *A11ySubtreeBuilder) }` (upstream takes the prepaint state and the subtree builder). Tree inclusion still requires an ID plus a role or hidden flag, per the pin's prepaint rules. [CE Element](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/element.rs#L51-L171), [a11y gating](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/element.rs#L105-L152). `ElementID` is an opaque comparable type with constructors covering the pin's ten variants (Name, Integer, View, FocusHandle, CodeLocation, NamedChild, OpaqueId, Uuid, Path, NamedInteger); `GlobalElementID` is the path. `CustomElement` returns an `AnyElement` wrapping a concrete `elementAdapter[L, P]` holding the author's `Element[L, P]` value plus its phase state (layout id, the `L` and `P` values, a phase marker). The adapter's phase methods carry the concrete typed states, compute the `GlobalElementID` and `InspectorElementID` exactly as `Drawable` does (id-stack push/pop), and panic on phase misuse for parity; erasure happens only at the `AnyElement` boundary. Authors keep typed phase states; framework dispatch may erase the adapter internally. `AnyElement` supplies the passthrough element with no identity of its own, and `gpui.Empty()` renders nothing.

`source_location` is an **optional** source-location hook, not a universal compile-time mechanism: upstream returns `Option<&'static Location>` and `AnyElement`, `Empty`, and `SharedString` all return `None` in the pin. Go proposal: the `CustomElement` adapter captures `runtime.Caller` in debug/inspector builds — a stack walk per element construction with measurable cost in element-heavy frames, hence debug-only — and authors can supply an explicit source location through the `SourceLocation()` method or a builder option, overriding capture. No claim of Rust compile-time semantics is made.

## Unsolved facts and validation limits

Honest residue after this round: (1) every signature is uncompiled — the Go 1.27 root build remains unverified; among the proposals only `Subscribe` is a generic method introducing a fresh type parameter (`EmitOn` uses only the receiver's parameters); inference from method-expression callbacks and the auxiliary pointer constraints (`ViewOf`, managed-view helpers) is unvalidated ([Go 1.27 language](https://go.dev/doc/go1.27#language)); (2) the conversion-relabeling fix is spec reasoning, not a compiler-verified property; (3) declaration authority remains weaker than Rust coherence — an engineering consequence of the accepted declaration direction, recorded for review, not a user-accepted divergence; (4) greps covered the selected evidence snapshots only, not the whole CE crate; (5) this report does not complete all signatures — exact context/entity access and error signatures, the action descriptor/handler association, and all `Subscription`, ownership, cancellation, and delivery-order semantics remain review items, with the latter reserved to decision 04; (6) the JSON schema library, accesskit node-id hashing determinism, and `runtime.Caller` cost are implementation-time facts. The researcher wrote only this report. Main-chat integration separately updated project/tracker/API/handoff documents and applied final report consistency corrections after the research turn completed.
