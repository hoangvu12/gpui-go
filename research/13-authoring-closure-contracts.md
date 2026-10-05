# Authoring closure: access, action dispatch, child staging contracts

Research date: 2026-10-04; revised the same day after main review. Bounded closure selection for [the authoring decision](../.scratch/gpui-core/issues/03-choose-go-authoring-contract.md), covering the three areas the [selected directions](../docs/authoring-decision-round.md) leave open. All signatures are **documentation proposals, uncompiled**; Go 1.27 generic methods appear only on concrete types, never as interface requirements. Lifetime, retention, cancellation, and scheduling policy remain [ticket 04](../.scratch/gpui-core/issues/04-define-runtime-ownership.md); compiler validation of these signatures is tracked in [ticket 10](../.scratch/gpui-core/issues/10-validate-authoring-signatures.md). Only pinned CE sources were consulted, at `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`.

## 1. Access carrier, entity access, and listener

**Source facts.** `Context::listener` captures the context's entity weakly and wraps a typed callback into an untyped-context one; delivery upgrades the weak handle and discards the result — a destroyed target is a silent no-op ([CE listener](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/context.rs#L252-L261)). Entity update leases the entity out of the map, hands the callback `&mut T` plus a fresh `Context<T>`, and reinserts it after ([CE update](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app.rs#L2889-L2915)). Reentrant update or read panics "cannot {update|read} T while it is already being updated" ([lease panic](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/entity_map.rs#L207-L212)); wrong-context handles fail a debug assertion ([context check](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/entity_map.rs#L166-L180)). Reads record accessed entities for invalidation; `update_in` is the only Result-returning access form, erroring on closed windows ([entity surface](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/entity_map.rs#L460-L505)).

**Selection.** A sealed carrier interface with a private, non-generic runtime access surface; no method redeclares its receiver's type parameter, and public code never receives untyped entity state:

```go
type AppContext interface{ access() appAccess } // appAccess is framework-private
// *App and *Context[T] implement AppContext. External wrappers (async, test,
// window-scoped) embed or delegate an already-valid carrier; they cannot forge
// one, and appAccess never exposes entity state as `any`.

func (h Entity[S]) Read[R any](cx AppContext, f func(*S, *App) R) R
func (h Entity[S]) Update(cx AppContext, f func(*S, *Context[S]))          // void callback
func (h Entity[S]) UpdateWith[R any](cx AppContext, f func(*S, *Context[S]) R) R

// Weak forms; ErrEntityReleased is a typed sentinel error:
func (h WeakEntity[S]) TryRead[R any](cx AppContext, f func(*S, *App) R) (R, error)
func (h WeakEntity[S]) TryUpdate(cx AppContext, f func(*S, *Context[S])) error
func (h WeakEntity[S]) TryUpdateWith[R any](cx AppContext, f func(*S, *Context[S]) R) (R, error)

// Visual forms only, with an explicit Window callback; distinct errors so a
// dead entity is never conflated with a window problem:
func (h Entity[S]) UpdateIn(cx AppContext, w *Window, f func(*S, *Window, *Context[S])) error
func (h WeakEntity[S]) TryUpdateIn(cx AppContext, w *Window, f func(*S, *Window, *Context[S])) error
// errors: ErrEntityReleased | ErrWindowClosed | ErrNoWindow

func (cx *Context[T]) Listener[E any](
    f func(*T, *E, *Window, *Context[T]),
) func(*E, *Window, *App) // weak target: dead owner no-ops; other misuse panics
```

- **Misuse vs. error path.** Reentrant same-entity access, stale scope (using a saved `*Context` after its callback), wrong-app handles, and missing foreground where required are programmer misuse: panic with operation, state type, and caller location, mirroring the pinned lease panic. Weak and visual forms are the error path: `ErrEntityReleased`, and — only on the visual forms — `ErrWindowClosed`/`ErrNoWindow`. The listener's dead-target no-op matches upstream's discarded result; every other misuse inside a listener panics.
- **Panic cleanup.** Lease and gate bookkeeping is restored by `defer` during unwinding; there is no `recover`-based rollback, and a panicking callback's writes stand.
- **Access conflict precision.** Read-only callbacks may nest. Updating while a read is active, or accessing the same entity through another handle while its update is active, conflicts with exclusive mutation. Arbitrary writes through a read callback's saved pointer remain outside the supported contract.
- **Honest limits.** The read-only callback contract cannot enforce anything about pointers the callback saves or writes it performs through them; Go has no borrow checker. This is stated, not worked around.

```go
n := counter.Read(app, func(c *Counter, a *App) int { return c.count })
counter.Update(cx, func(c *Counter, ccx *gpui.Context[Counter]) { c.count++; ccx.Notify() })
n = counter.UpdateWith(app, func(c *Counter, ccx *gpui.Context[Counter]) int { c.count++; return c.count })
if err := weakCounter.TryUpdate(app, f); errors.Is(err, gpui.ErrEntityReleased) { /* released; skip */ }
div.OnClick(cx.Listener((*Counter).clicked))
```

`Listener` infers `E` from the method expression's event parameter; that inference, and the generic methods on `Entity[S]`/`WeakEntity[S]`, are Go 1.27 constructs ([language change](https://go.dev/doc/go1.27#language)) and remain uncompiled.

## 2. Canonical action descriptors and typed dispatch

**Source facts.** Dispatch-tree listeners are keyed by action payload type (`TypeId`) ([dispatch node](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/key_dispatch.rs#L333-L341)); the name registry only builds actions from keymap JSON and "is unrelated to binding actions in the element tree" ([registry boundary](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app.rs#L2363-L2372)). Upstream keeps one canonical name per action type ([type-to-name map](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/action.rs#L233-L334)). Bubble-phase handlers stop propagation by default, so multiple handlers for one action do not all necessarily run ([propagation policy](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app.rs#L2373-L2385)). `on_boxed_action` clones the *bound* action instance, keys the listener by that clone's type, and invokes the listener with the captured clone — the incoming dispatched payload is ignored ([boxed handler](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/elements/div.rs#L497-L511)).

**Selection.** Descriptor services are canonical per payload type, enforced by once-only definition — never by comparing functions:

```go
func DefineAction[A any](spec ActionSpec[A]) Action[A] // exactly once per A, process-wide
type ActionSpec[A any] struct {
    Name          string             // full stable external name, e.g. "counter::Increment"
    Clone         func(A) A          // required
    Equal         func(a, b A) bool  // required
    FromJSON      func(json.RawMessage) (A, error) // optional; omitted = NoJSON
    Schema        func() any
    Aliases       []string
    Deprecation   string
    Documentation string
}
func DefineUnitAction[A ~struct{}](name string) Action[A] // full explicit name; trivial services

func (a Action[A]) Box(value A) BoxedAction // descriptor + payload: a bound instance

func (d *Div) OnAction[A any](a Action[A], handler func(*A, *Window, *App)) *Div
func (d *Div) CaptureAction[A any](a Action[A], handler func(*A, *Window, *App)) *Div
func (d *Div) OnBoxedAction(bound BoxedAction, handler func(BoxedAction, *Window, *App)) *Div
func (w *Window) Dispatch[A any](a Action[A], payload A, cx AppContext)
func (w *Window) DispatchBoxed(bound BoxedAction, cx AppContext)
func (app *App) RegisterActions(actions ...AnyAction)
func (app *App) BuildAction(name string, data json.RawMessage) (BoxedAction, error)
```

- **Canonicality.** `DefineAction` for a payload type `A` runs exactly once per process; a second definition — identical or conflicting — panics. Copies and imported declarations reuse the one canonical descriptor, so no function-pointer equality is ever needed (Go functions are not comparable anyway). A zero `Action[A]` is invalid and panics on use: descriptors are produced, not defaulted. `DefineUnitAction` takes the full stable name — no inference from Go type strings — and may supply trivial `Clone`/`Equal` because `~struct{}` payloads have exactly one value.
- **Dispatch and registry.** Dispatch identity is the payload type, mirroring `TypeId`; `OnAction`/`Dispatch` never consult the registry, so unregistered typed handling works, and a handler alone creates no name or keybinding. `RegisterActions` idempotently accepts the same canonical descriptor (or its copies/imports) and rejects type, name, or alias conflicts at setup, mirroring the pinned one-name-per-type map. `BuildAction` distinguishes unknown name, JSON-disabled (`NoJSON` descriptor), and decode failure in its error. Propagation order is policy: bubble handlers stop propagation by default, so registering two handlers does not imply both fire.
- **Boxed actions.** `AnyAction` is the erased descriptor; `BoxedAction` is a descriptor plus payload. `a.Box(value)` binds an instance; `OnBoxedAction` stores a clone of the bound instance (through the descriptor's `Clone`) and later invokes the handler with that captured clone, exactly preserving the pinned semantics — the dispatched payload is not what the handler receives. `Dispatch` and `DispatchBoxed` take the current `AppContext`.

```go
var increment = gpui.DefineUnitAction[Increment]("counter::Increment")
app.RegisterActions(increment) // idempotent for the same canonical descriptor
div.OnAction(increment, cx.Listener((*Counter).incrementAction))
w.Dispatch(increment, Increment{}, cx)
pasteBound := pasteAction.Box(Paste{Content: "hi"})
div.OnBoxedAction(pasteBound, func(b gpui.BoxedAction, w *gpui.Window, app *gpui.App) { ... })
```

## 3. Child staging, reservation, and consumption

**Source facts.** Upstream bulk conversion is `self.extend(children.into_iter().map(|child| child.into_any_element()))` — the `.map` is lazy, so the source does **not** prove conversions finish before `extend`, nor any transactional parent behavior ([CE parent element](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/element.rs#L228-L250)). Views are consumed at first expansion: `ViewElement` takes its stored view with `take().unwrap()`, so reuse of a consumed element panics upstream ([consumption](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/view.rs#L354-L398)). Rust move semantics make double-attachment unrepresentable; a Go contract must be designed and is a deliberate adaptation.

**Selection.** Precedence follows the selected direction: nil/invalid → `AnyElement` → explicit `IntoElement` → `string` → `View` → `RenderOnce`; raw generic `Element[L, P]` requires `CustomElement[L, P]`; raw entities require `ViewOf`. One attachment token per attachment, shared by copied `AnyElement`/wrapper handles: conversion yields a fresh unattached handle; parent commit marks it attached; first render marks it expanded.

```go
func OwnRecipe(recipe RenderOnce) AnyElement // one-time public ownership adapter
func (d *Div) Child(child any) *Div
func TryIntoElement(value any) (AnyElement, error)
func (d *Div) Children(items ...any) *Div
func (d *Div) TryChildren(items ...any) (*Div, error)
func (d *Div) TryChildrenSlice[T any](items []T) (*Div, error)
```

- **Guaranteed and unguaranteed detection.** Framework-produced handles and builders carry tokens reliably: attaching an already-attached or already-expanded handle is always diagnosed. For third-party recipes, the enforceable path is `OwnRecipe`: it wraps a recipe once and returns a handle whose token is shared by copies, so `d.Child(label); d.Child(label)` detects the second attachment immediately — before expansion — and `d.TryChildren(label, label)` rejects the duplicate token before commit. Repeated wrapping of the same raw recipe is caller misuse and is **not** universally detected: two raw wrappers cannot reliably share a token by pointer identity (zero-sized allocations and object lifetimes make pointer identity untrustworthy), and value copies carry no universal provenance. The bare `RenderOnce` convenience remains but promises no global alias detection. Third-party `IntoElement` implementations either return the same token on repeated conversion or honor the single-use contract; raw aliases beyond that contract are not diagnosed.
- **Staging protocol.** Bulk operations stage: they run conversions (user converters run here, never during commit), reserve candidate tokens, and detect nil of any nil-capable kind, invalid zero `AnyElement`, duplicates, and already-attached handles. On failure or panic, reservations are canceled (deferred), the destination parent's children remain unchanged, and the same-destination reentrancy gate — which rejects reentrant child mutations of the parent being staged — is released. Commit is a single append with no converter callbacks. Conversion is not claimed thread-safe: concurrent builder use is unsupported.
- **Honest limits.** A context-free converter signature does not structurally isolate effects: an `IntoElement` method can close over a parent builder, an `App`, or any framework object, so converter side effects on other objects are not rolled back and panics propagate without general `recover`. `TryIntoElement` used outside attachment simply returns a fresh handle; dropping it releases ordinary references — no reservation is held — but a user converter may already have consumed the raw input, and retrying the same raw input is not promised. On a parent-level failure, previously staged handles remain unattached and reusable.

```go
d.Child(42)            // panic: unsupported child type int
d.Child(nil)           // panic: nil child
label := gpui.OwnRecipe(NewCountLabel(3))
d.Child(label)
d.Child(label)         // panic on second: attached token, before expansion
fresh := gpui.OwnRecipe(NewCountLabel(4))
d.TryChildren(fresh, fresh) // error: duplicate fresh token before commit
d.Child(NewCountLabel(3)); d.Child(NewCountLabel(3)) // two fresh recipes: valid
d.Child(rawLabel); d.Child(rawLabel) // bare recipe alias: not universally detected
```

## Conclusion

These three areas can close now at documentation level. The access, action, and staging contracts above are internally consistent, pinned to upstream behavior, and deliberately separate from ownership policy: retention and cancellation remain [ticket 04](../.scratch/gpui-core/issues/04-define-runtime-ownership.md) without blocking these signatures, and compiler proof of the uncompiled Go 1.27 constructs (generic-method and pointer-constraint inference, relabeling negatives, cross-package positives) is tracked openly in [ticket 10](../.scratch/gpui-core/issues/10-validate-authoring-signatures.md). That validation is outstanding engineering work, not an open design choice, and is not a reason to hold the authoring decision open; the main chat records closure after its review. Nothing here claims executable correctness or runtime implementation.
