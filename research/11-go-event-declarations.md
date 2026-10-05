# Go event declarations and short checked calls

Research date: 2026-10-04. Documentation-only investigation for [the authoring decision](../.scratch/gpui-core/issues/03-choose-go-authoring-contract.md), prompted by the user's request to investigate whether Go can retain GPUI's one-time event declaration, short emission, and compile-time checks. Nothing here records user acceptance or authorizes runtime implementation. All Go signatures below are **proposals, uncompiled**.

## Finding and recommendation

The earlier choice between an extra event argument and runtime checking was incomplete. Go 1.27's generic methods support plausible short, statically checked calls for restricted event models. The important restriction is whether an event can be emitted by several unrelated state types, including third-party state types using a framework-owned payload.

Recommend retaining a general typed pair declaration as the foundation, and considering the declaration itself as the receiver:

```go
var countChanged = gpui.DefineEvent[Counter, CountChanged]()

countChanged.Emit(cx, CountChanged{Value: 3})
subscription := countChanged.Subscribe(summaryCx, counter, (*Summary).changed)
```

This names the declaration once in each call, allows any number of declared events per state and states per event, and preserves typed source and payload arguments. It is an alternative spelling to `cx.Emit(countChanged, payload)`, not an additional runtime topic. The general pair declaration is still a proposal; exact descriptor representation and compiler validation remain outstanding.

A payload-owned marker can additionally preserve exactly `cx.Emit(payload)` for source-specific events. It must not become the only event contract: core GPUI permits third-party managed views to emit its shared `DismissEvent`. Do not describe this narrower convenience as a complete translation of Rust's `EventEmitter<E>`.

## What the original and pinned CE require

Both Rust revisions put the relation on the emitting state: `EventEmitter<E>` is a marker trait. `Context::emit` requires that relation, while `Context::subscribe` checks it for the source entity and separately types the subscriber callback. This is an emitter/payload compatibility check, not just a typed callback. No declaration value is passed at each call. [CE marker](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/gpui.rs#L325-L329), [CE emission](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/context.rs#L762-L775), [CE subscription](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/context.rs#L98-L115), [original emission](https://github.com/zed-industries/zed/blob/a84689073d296dfd39987bc7dd478e43ef76d83a/crates/gpui/src/app/context.rs#L760-L774), [original subscription](https://github.com/zed-industries/zed/blob/a84689073d296dfd39987bc7dd478e43ef76d83a/crates/gpui/src/app/context.rs#L94-L108).

The original counter declares its relationship with an ordinary trait implementation. More decisively, CE's public `ManagedView` contract requires `EventEmitter<DismissEvent>` and applies to arbitrary qualifying view types. Supporting a shared framework event on a consumer-defined source is therefore a core extensibility requirement, not a speculative Base feature. [Original counter declaration](https://github.com/zed-industries/zed/blob/a84689073d296dfd39987bc7dd478e43ef76d83a/crates/gpui/examples/testing.rs#L20-L31), [CE ManagedView and DismissEvent](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/window.rs#L734-L746).

Rust's coherence rules govern where implementations can be declared and reject overlapping implementations. An exported Go factory for typed descriptors does not reproduce this declaration authority. Keep that separate from whether a particular call has compatible source and payload types. [Rust implementation coherence](https://doc.rust-lang.org/reference/items/implementations.html#trait-implementation-coherence).

## Language facts used

Go 1.27 adds type parameters to concrete methods; interfaces still cannot require generic methods, and generic methods cannot implement ordinary interface methods. [Release notes](https://go.dev/doc/go1.27#language), [official explanation](https://go.dev/blog/generic-methods).

Receiver type parameters are in scope after the method name, so a method constraint can refer to a receiver parameter. Receiver constraints themselves come from the base type. Method names on one base type must be unique, methods belong to locally defined base types, and interface method signatures must match. Embedded methods are promoted subject to selector ambiguity. Constraint-only union interfaces cannot be used as ordinary values or type arguments; a bare type parameter cannot be embedded as a type-set term. Inference uses arguments and constraint equations. These are the specific language rules behind the following assessments, rather than a general claim that Go cannot represent event relations. [Scope](https://go.dev/ref/spec#Declarations_and_scope), [method declarations](https://go.dev/ref/spec#Method_declarations), [method sets](https://go.dev/ref/spec#Method_sets), [selectors](https://go.dev/ref/spec#Selectors), [general interfaces](https://go.dev/ref/spec#General_interfaces), [type inference](https://go.dev/ref/spec#Type_inference).

## Candidate 1: put the declaration on the payload

Proposed framework signatures, with bodies omitted:

```go
type EventFor[T any] interface {
    GPUIEventFor(*T)
}

func (cx *Context[T]) Emit[E EventFor[T]](event E)

func (cx *Context[Observer]) Subscribe[Source any, E EventFor[Source]](
    source Entity[Source],
    callback func(*Observer, Entity[Source], *E, *Context[Observer]),
) Subscription
```

Proposed application declaration and use:

```go
type CountChanged struct{ Value int }
func (CountChanged) GPUIEventFor(*Counter) {}

counterCx.Emit(CountChanged{Value: 3})
subscription := summaryCx.Subscribe(counter, (*Summary).changed)
```

Design inference: the receiver fixes `T`, emission infers `E` from its argument, and subscription infers the source from its entity and the payload from its callback. The constraint expresses the intended compatibility check. The marker is a type declaration aid; dispatch should not invoke it or retain the `*Counter` parameter.

This supports several payload types for one state: each payload defines its own method. It does not support one unchanged payload for several unrelated states through this exact marker scheme: the payload cannot overload `GPUIEventFor` with another parameter type. Nor can a consumer add that method to an imported payload. An embedding helper can shorten the marker declaration but does not remove these restrictions.

Wrapping or defining `CountChanged[Owner]` can restore short calls for multiple owners, but creates distinct event types. A wrapper around imported `DismissEvent` would also change the event observed by subscribers unless an explicit adapter translates it. These are adaptations with identity and API consequences, not proof of parity for the unchanged payload.

## Candidate 2: keep the declaration on the owner

A concrete auxiliary pointer parameter can express a conditional capability:

```go
func (*Counter) GPUIEmits(CountChanged) {}

func (cx *Context[T]) Emit[
    E any,
    P interface { *T; GPUIEmits(E) },
](event E)
```

Design inference: the single type term `*T` offers an inference route for `P`; the method requirement then checks its event declaration. This is a plausible source-side, payload-only emission shape, including an imported event. It must be compiler-checked before treating implicit `P` inference as proven.

The same owner cannot overload `GPUIEmits` for a second payload. Embedding two helpers with competing promoted methods does not create overload resolution. A generic concrete marker method does not solve the interface requirement because generic interface methods are unavailable. Thus this candidate proves that conditional receiver capabilities should not be dismissed outright, but it does not by itself supply an open many-event contract.

## Candidate 3: event sets or a specialized context

An owner can define a basic event interface, have its local payloads implement a marker, and expose an `Emit(event CounterEvent)` facade. That can preserve short calls for several events. It needs a separate facade per event family or an additional ordinary interface type argument carried through the API.

The tempting generic form `Context[T, Events]` cannot accept a constraint-only union such as `CountChanged | Reset` as the value type `Events`, nor can a later method simply write `[E Events]` when `Events` is a type parameter. A basic interface can be passed and accepted as a value, but then a generic relationship between an individual callback payload and that arbitrary interface still needs a mechanism. A single callback accepting the whole interface and switching on it changes the typed per-event subscription experience.

An owner method returning an event-set helper does not automatically give `Context[T]` an associated event-set type. A concrete per-owner context wrapper could carry it, with additional adapter/forwarding work and consequences for the existing `Render(*Context[T])` proposal. This deserves consideration if context customization is independently desired; it is not a demonstrated zero-cost replacement for the general declaration.

There is a more capable concrete facade than the ordinary-interface variant:

```go
type CounterEvents interface { CountChanged | gpui.DismissEvent }
type CounterContext struct { *gpui.Context[Counter] }

func (cx *CounterContext) Emit[E CounterEvents](event E)
```

Here the union is used directly as a constraint. It can include shared/imported payloads and several events, preserving short checked emission inside that facade. An equivalent owner-specific subscription adapter can constrain callback payloads. Application authors would need matching forwarding and context wrapping in their callbacks, or the framework would need an extensible integration contract; generation is another possible source of that boilerplate. The event union alone does not make the unchanged general `Context[T]` discover the relation. This is a viable family of specialized APIs with additional authoring work, not a language impossibility.

## Candidate 4: general typed declaration as receiver

The descriptor fixes the emitter and payload independently, leaving only the observing state generic on subscription:

```go
func (event Event[Source, Payload]) Emit(
    cx *Context[Source], payload Payload,
)

func (event Event[Source, Payload]) Subscribe[Observer any](
    cx *Context[Observer],
    source Entity[Source],
    callback func(*Observer, Entity[Source], *Payload, *Context[Observer]),
) Subscription
```

This keeps a single declaration for each allowed pair and makes wrong contexts, sources and callback payloads signature mismatches. It supports imported/shared payloads without adding methods to them. Emission itself uses only the descriptor's receiver parameters; subscription uses the new generic-method capability. The context-receiver spelling from the earlier round can be equivalent, but adopting both spellings would add surface area and has not been recommended or accepted.

The relation must be represented by the descriptor's actual type arguments, not a mutable name or address. Repeating `DefineEvent[Source, Payload]()` must not split subscriptions into different topics. Descriptor zero values, interface payloads, type conversions, and exported declarations still need a precise contract. The framework must retain the declared payload identity when erasing a value internally; a value's dynamic interface member must not unexpectedly select another event stream. These remain requirements from the [earlier design review](../docs/authoring-decision-round.md), not verified implementation properties.

An exported descriptor factory permits a caller to declare a pair; it is not an authorization token. Preserve runtime lifecycle checks independently of compile-time payload compatibility. Entity lifetime, cancellation, weak observer capture, callback scheduling and mutable payload ownership remain the dependent lifetime decision's work.

The free-standing descriptor also does not make its `Source` type satisfy a discoverable emitter interface. A translated `ManagedView` adapter would need to receive evidence explicitly, such as the entity together with an `Event[Source, DismissEvent]`, or use a separately designed capability contract. Providing that integration is necessary before claiming the descriptor reproduces all uses of upstream's emitter trait. It is an unresolved authoring detail, not permission to omit `ManagedView` capabilities.

## Bounded conclusion and next validation

There is no justification for claiming that short checked Go emission is impossible. Two small marker designs express it, with different restrictions. The investigation did not find a single uniform `Context[T].Emit(payload)` design that also preserves unchanged shared/imported event types, arbitrary multiple events per source, and checked per-event subscriptions without wrappers, extra type plumbing, generated forwarding, or runtime validation. This is a conclusion about the examined designs, not a proof over every possible Go API.

For the accepted open core target, prefer the general typed declaration as the baseline and show its receiver spelling to the user. Treat the payload-marker convenience as optional further design, with its narrower domain stated plainly. Do not ask the user to trade away shared GPUI events merely to preserve the counter's spelling.

When an authorized Go 1.27 compiler is available, validate positive cases for two payloads on one source, one shared payload on two sources, imported payloads, callback inference, and cross-package declarations. Negative cases should include wrong contexts, wrong source entities, wrong callbacks, and attempts to relabel descriptor type arguments. The marker experiments should separately check pointer/value payload identity and auxiliary pointer inference. Runtime delivery is not established by any signature check.

No compiler experiment, installation, runtime source change or toolchain workaround was performed for this report. The root Go 1.27 build remains unverified; previous isolated Go 1.26.5 authoring tests do not validate these signatures. The accepted CE pin and broader implementation boundary remain unchanged.
