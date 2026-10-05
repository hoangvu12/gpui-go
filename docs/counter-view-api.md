# Counter-view authoring example

Status: documentation for the resolved [Go authoring decision](../.scratch/gpui-core/issues/03-choose-go-authoring-contract.md), 2026-10-04. **Authoring contract selected; counter/two-window signatures compiler-validated on Go 1.27.1; runtime unimplemented.** The snippets illustrate the interface, not a runnable application. Only the existing `authoring` helper is implemented. The `gpui` qualifier below names a future core package; it does not choose a public module path.

The [current Wayfinder round](authoring-decision-round.md) records the accepted typed declaration direction from [focused event research](../research/11-go-event-declarations.md): `countChanged.Emit(cx, payload)` and declaration-based subscription. The calls below follow that direction; their selected Go signatures and inference now pass the isolated compiler fixture. The user delegated remaining routine engineering choices to research and review.

This continues the accepted Go 1.27 floor, convenient string children, and constructor-bound fluent styling. It uses the existing [core audit](../research/01-gpui-core-contracts.md) and [interface/lifetime analysis](../research/04-go-api-and-lifetimes.md), pinned to GPUI-CE `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`. Base remains later work.

Following the user's request to research the original, this draft was also checked against Zed GPUI at comparison commit `a84689073d296dfd39987bc7dd478e43ef76d83a`. That comparison does not change the accepted CE parity reference. The [original-source report](../research/10-original-gpui-authoring.md) separates upstream behavior from Go-specific adaptations.

## The small counter

Assume a host has created one `gpui.Entity[Counter]` in an explicit owner scope, initialized its focus handle in the counter's dependent-resource scope, registered the `incrementAction` descriptor for keymap construction, bound a key to it in the `Counter` key context, and initially focused the counter. The [runtime contract](runtime-ownership-contract.md) defines creation, ownership and shutdown. The action definition below is independent of named registration; typed dispatch does not need a keymap entry.

```go
type Increment struct{}             // A command: please increment.
type CountChanged struct{ Value int } // An event: the count changed.

var countChanged = gpui.DefineEvent[Counter, CountChanged]()
var incrementAction = gpui.DefineUnitAction[Increment]("counter::Increment")

type Counter struct {
    count int
    focus gpui.FocusHandle
}

func (c *Counter) increment(cx *gpui.Context[Counter]) {
    c.count++
    cx.Notify()
    countChanged.Emit(cx, CountChanged{Value: c.count})
}

func (c *Counter) clicked(
    _ *gpui.ClickEvent, w *gpui.Window, cx *gpui.Context[Counter],
) {
    w.Focus(c.focus)
    c.increment(cx)
}

func (c *Counter) incrementAction(
    _ *Increment, _ *gpui.Window, cx *gpui.Context[Counter],
) {
    c.increment(cx)
}

func (c *Counter) Render(
    w *gpui.Window, cx *gpui.Context[Counter],
) gpui.AnyElement {
    return gpui.Div().
        ID("counter").
        TrackFocus(c.focus).
        KeyContext("Counter").
        OnAction(incrementAction, cx.Listener((*Counter).incrementAction)).
        Flex().Gap2().
        Child(NewCountLabel(c.count).Px3().Prefix("Count: ")).
        Child(gpui.Div().
            ID("increment").
            OnClick(cx.Listener((*Counter).clicked)).
            Child("Increment")).
        IntoElement()
}
```

This demonstrates routing rather than a finished accessible control. A production clickable control also needs semantic role/name, focus traversal, keyboard activation, and accessible actions. Their omission here is not a core-parity exception or a request to implement Base.

There is an actual upstream [counter example](https://github.com/zed-industries/zed/blob/a84689073d296dfd39987bc7dd478e43ef76d83a/crates/gpui/examples/testing.rs#L20-L205) with actions, `cx.listener(Self::increment)`, focus, keybindings, string children, and explicit `notify`. The sketch above follows that structure. Its `CountChanged` payload and reusable label are deliberate additions for reviewing the Go interface, not claims about the exact Rust example. Upstream's accompanying tests were read, not executed here.

`cx.Listener((*Counter).clicked)` receives a method expression, not a closure retaining `c` or the render-time `cx`. Selected behavior: capture weak logical entity identity; on delivery, obtain live state and a fresh callback-scoped context. A dead target makes the listener inactive. This is a documented lifetime rule, not behavior implemented by the styling helper. Users can still write closures that capture inappropriate pointers; the adapter cannot make arbitrary Go closures safe.

`Context[Counter]` is a concrete generic type, so typed convenience methods need not be requirements on a runtime interface. Event/listener inference and concrete generic-method declarations passed the Go 1.27.1 signature fixture. This says nothing about callback delivery or ownership behavior.

The click and action invoke the same mutation, but use different routing. Mouse events propagate by default; action bubble handlers stop by default. Explicit propagation must remain available. Merely declaring `Increment` or installing `OnAction` does not create a keyboard shortcut: registration, keybindings, and a focus path are required.

## A reusable component, without another entity

This component carries the count for one tree construction. The persistent count stays in `Counter`.

```go
type CountLabel struct {
    authoring.Styled[*CountLabel]
    value  int
    prefix string
}

func NewCountLabel(value int) *CountLabel {
    return authoring.BindStyled(&CountLabel{value: value})
}

func (l *CountLabel) Prefix(value string) *CountLabel {
    l.prefix = value
    return l
}

func (l *CountLabel) RenderOnce(
    w *gpui.Window, app *gpui.App,
) gpui.AnyElement {
    return gpui.Div().
        Refine(l.Style()).
        Child(fmt.Sprintf("%s%d", l.prefix, l.value)).
        IntoElement()
}
```

Here `authoring` is the real local helper package and `fmt` is the standard formatting package. `Div`, its `Refine` method, and all rendering operations are proposed. `Child(NewCountLabel(...))` would recognize the ordinary `RenderOnce` interface and wrap the recipe for expansion during element processing. The method name expresses consuming a recipe, not running once for the whole application's lifetime. A fresh recipe can be created each render.

Selected direction: attach each mutable recipe or element builder once; keeping immutable input data and constructing a fresh recipe is the normal reuse path. Framework handles share an attachment token, so aliases diagnose repeated attachment immediately. For a third-party recipe, `owned := gpui.OwnRecipe(NewCountLabel(3))` gives that explicit shared handle. Independently wrapping the same raw recipe again, or copying arbitrary third-party values, is caller misuse that cannot be universally detected. Bare `RenderOnce` conversion remains convenient under this single-use contract. Existing `Styled` copy checks do not enforce attachment.

This restriction must not be generalized to persistent entity handles. Upstream `Entity<T>` supports cloning a handle to the same state, and renderable entities can produce view elements. In Go, `RetainInto(scope)` creates an independent lease; ordinary assignment aliases one lease. Rendering consumes the recipe, not the shared entity's state. Both checked revisions warn that views keyed by the same entity must not be siblings under the same parent: their internal element state would collide. Identity is scoped by the parent path. Distinct views may share a model entity without sharing their own view identity. See the [accepted CE view identity contract](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/view.rs#L185-L208).

## Roles and conversion

Candidate interfaces make the return type uniform at the composition seam:

```go
type Render[T any] interface {
    Render(*Window, *Context[T]) AnyElement
}

type RenderOnce interface {
    RenderOnce(*Window, *App) AnyElement
}
```

`*Counter` satisfies `Render[Counter]`; `*CountLabel` satisfies `RenderOnce`. `Entity[Counter]` identifies persistent state and does not mean `*Counter` may be freely retained or mutated outside controlled access. `Context[Counter]` supplies operations while that entity is being accessed; it must not be stored for a later callback or goroutine.

| Input at a child position | Proposed conversion | Important limit |
|---|---|---|
| `"Hello"` | Text element | Accepted convenience; no arbitrary `Stringer` conversion proposed |
| `gpui.Text("Hello")` or a built-in element builder | Its ordinary element conversion | Concrete fluent methods remain available before conversion |
| `NewCountLabel(3)` | `RenderOnce` recipe adapter | No persistent entity is created |
| `gpui.ViewOf(counter)` where `counter` is `Entity[Counter]` | Typed view adapter | Require a renderable state type at this seam |
| `gpui.CustomElement[L, P](custom)` | Typed low-level adapter | External packages may define their own phase-state types |
| An unsupported value, including accidental nil | Diagnostic at child attachment | Panic with type and parent/child context; checked conversion is available |

`Child(any)` is already accepted. The [reviewed engineering directions](authoring-decision-round.md) select immediate programmer-error diagnostics plus checked conversion, and explicit `ViewOf(entity)` for entities. Non-view entities remain useful and should not all claim to be renderable. The pointer/state constraint and call inference passed the fixture; this notation does not assert that an unconstrained `Entity[T]` can implement conditional methods.

`AnyElement` is the uniform result for heterogeneous trees. It hides concrete phase state; it is not permission for application code to exchange untyped state everywhere. Use one conversion policy: reject nil/invalid handles, accept `AnyElement`, honor explicit `IntoElement`, convert strings, then preserve an identity-bearing `View` before falling back to stateless `RenderOnce`. Raw generic custom elements require the typed adapter. `TryChildren` and `TryChildrenSlice` validate/reserve candidates before committing attachment, with a gate against reentrant child mutation/conversion of the destination. Failure releases reservations and preserves the destination's child list; effects of user converters on other objects are not rolled back. The [selected contract](authoring-decision-round.md) defines token and failure behavior; runtime enforcement remains unimplemented.

Both revisions have a public `View` abstraction unifying renderable entities and recipes, including custom views with parent-supplied properties and backing entity identity. Reserve `View` for that interface and use `ViewOf(counter)` for the entity adapter. A custom Go view holds its properties in ordinary fields and reports optional backing identity. A stateless wrapper supplies absent identity for a recipe: `RenderOnce` alone does not automatically supply an `EntityID` method. The completion report contains the candidate pointer/state constraint; exact inference passed the fixture.

## Typed events and observation

The example deliberately calls both `Notify` and `Emit`:

- `Notify` means readers/observers should reconsider the entity's state. Pending notifications coalesce by entity. Mutation alone is not an implicit notification.
- `countChanged.Emit(cx, CountChanged{...})` reports a particular occurrence with typed data. Each emission remains separately queued; it does not replace `Notify`.
- `Increment` is a routed command, not an emitted entity event. Registration supplies its stable external name and payload construction, without mandatory application code generation.

Upstream also requires an emitter to declare which event types it supports. `DefineEvent[Counter, CountChanged]` links source and payload at emission and subscription. The [selected authoring contract](authoring-decision-round.md) defines descriptor identity, zero values, cross-package authority limits, managed-view capabilities, action schemas, equality and cloning. Named lookup needs registration; direct typed dispatch does not.

Effects run after the active outer update completes, not while the counter is mutably accessed. For two increments during one update, an observer may see the final count while subscribers receive both recorded values. Coalescing applies only while a notification remains pending; this is not a promise of at most one notification per frame.

A proposed subscriber method has this shape:

```go
func (s *Summary) changed(
    source gpui.Entity[Counter], event *CountChanged, cx *gpui.Context[Summary],
) {
    s.last = event.Value
    cx.Notify()
}

// During Summary initialization, once, not during every Render:
// subscription := countChanged.Subscribe(cx, counter, (*Summary).changed)
// observer := cx.Observe(counter, (*Summary).counterNotified)
```

Both registrations default to the summary entity's dependent scope. Ignoring the returned handle leaves the registration active until that scope closes or an endpoint dies; `Cancel()` stops it early. A window-only registration can `TransferInto(windowScope)`. An observer receives a callback-borrowed source entity and `Context[Summary]`, without a `CountChanged` payload. It can use `source.Read(cx, callback)`; persisting the source requires `RetainInto` a live scope. Strong/stale/reentrant misuse panics, while weak access reports released entities. Compiler signature validation passed; lifetime implementation remains outstanding.

## Two independent owners

The following host sketch uses an existing counter lease owned by window A. `windowAScope` and `windowBScope` are the host's distinct live scopes; `app` is the foreground app context. It omits window creation and rendering.

```go
counterB := counter.RetainInto(windowBScope)

summaryA := gpui.NewEntity[Summary](app, windowAScope,
    func(s *Summary, cx *gpui.Context[Summary]) {
        countChanged.Subscribe(cx, counter, (*Summary).changed)
    })
summaryB := gpui.NewEntity[Summary](app, windowBScope,
    func(s *Summary, cx *gpui.Context[Summary]) {
        countChanged.Subscribe(cx, counterB, (*Summary).changed)
    })
// The host attaches/render-retains summaryA and summaryB in their windows.
// Closing window A tears down its callbacks/frames and closes windowAScope.
// Window B still owns counterB and summaryB, with its own subscription.
```

Here `Summary` has a `last int` field and the `changed` method above; a displayed summary would also implement `Render`. Copying `counter` into `counterB` with ordinary assignment would share A's lease and would not achieve this lifetime. The summaries' subscriptions are independent and weak at both endpoints. A late callback targeting the released summary is skipped; a task result additionally checks its cancellation scope even if another owner keeps its target entity alive.

Render-time `ViewOf(counterB)` independently retains into the active frame-construction scope. `ViewOfIn(owner, counterB)` provides an owner outside that boundary. Discarded/abandoned constructions and retired placements close their resource scopes; cached placements carry their dependencies forward. Frame ownership can keep a view alive until that placement retires, so host teardown includes frame/callback cleanup as well as closing ordinary entity leases.

The integer event value is an independent value copy. For a slice/map payload, the producer must freeze reachable data until effect-cycle retirement and subscribers must treat it as read-only. When a payload needs entity/native ownership, `EmitOwned(cx, func(payloadScope *Scope) P)` lets its factory retain explicit dependencies into that scope. Basic `Emit` does not discover nested handles automatically.

## Low-level Element remains separate

`RenderOnce` expands into ordinary elements. A custom editor surface or measured graphic needs the lower-level `Element` contract. A candidate typed phase seam is shown below; this is an excerpt, **not the complete interface**:

```go
type Element[L, P any] interface {
    RequestLayout(*Window, *App) (LayoutID, L)
    Prepaint(Bounds, *L, *Window, *App) P
    Paint(Bounds, *L, *P, *Window, *App)
    // Identity, source location, and accessibility hooks also belong here
    // or in explicitly defined companion capabilities.
}

// An external CountMark implements Element[MarkLayout, MarkPrepaint].
// Its measured layout data and prepaint hitbox data have distinct types.
// A counter could add it with:
// Child(gpui.CustomElement[MarkLayout, MarkPrepaint](NewCountMark(c.count)))
```

Selected direction: retain typed `L` and `P` inside the adapter and erase them only for internal dispatch. The adapter owns phase ordering and ties those values to the correct element/frame. Persistent state keyed by element identity is a different facility. Public `any` states with author-written type assertions would save an adapter call but make layout/paint mismatches runtime mistakes. A closed list of allowed element types would prevent third-party extensibility and is unsuitable for the accepted destination.

The phase signatures above abbreviate upstream's optional global element and inspector identities. The selected authoring contract carries those through, plus source location and accessibility role, hidden-subtree behavior, property writing, and synthetic children (which can use mutable prepaint state). A cached render can reuse artifacts without calling all these methods again; “request layout, prepaint, paint” is the normal construction path, not a claim that every phase executes on every frame. This example selects no layout engine or renderer.

Keep the CE target's `is_a11y_hidden` hook and `ParentElementTyped` capability in the final contract. The original Zed revision checked lacks these particular declarations; that absence does not authorize dropping them from this port. The comparison is a focused source inspection, not proof that all other original/CE behavior is identical.

## What the source settles, and what remains a Go tradeoff

The original-source check narrowed the earlier questions. Consuming recipes and typed phase state follow upstream; the selected Go adapters and enforcement limits are engineering adaptations. These are recorded in the resolved authoring and runtime ownership decisions; compiler validation is recorded separately from unimplemented runtime behavior.

| Choice | Recommendation and practical cost | Alternative |
|---|---|---|
| Reusing the same mutable label/element object in two places | Preserve upstream's consuming-recipe model with a Go misuse check; make a fresh recipe for each place. Cloning/sharing an entity handle is a separate operation. | Explicit recipe cloning can be added where its children/listener semantics are defined; aliasing is not cloning |
| A programmer passes an unsupported child value | Fail immediately with a useful panic. Rust rejects unsupported children at compilation; this panic would be a Go-specific adaptation of the already accepted dynamic `Child(any)` choice. | Provide checked construction returning errors; logging and omission can leave an incomplete screen |
| What low-level custom element authors must write | Preserve upstream's typed layout/prepaint states through one Go adapter invocation. The extra call is a Go proposal; Rust uses associated types and conversion traits. | Manually exchanged `any` states save the adapter call but require author-written assertions |

Runtime validation is already accepted. Delegated engineering review selected immediate panic for programmer misuse plus a checked path for dynamic input, fresh consumed recipes, and typed custom-element phase adapters. See the [current decisions and limits](authoring-decision-round.md). Do not repeat earlier preference questions; complete the outstanding signatures and validation instead.

The [selected authoring contract](authoring-decision-round.md) now records action definition/registration, event declarations, conversion precedence and attachment, controlled access/error shapes, and identity/accessibility hooks. Familiar `cx.Listener(...)` has a concrete generic-method candidate; its inference and the other signature constraints passed the [validation task](../.scratch/gpui-core/issues/10-validate-authoring-signatures.md).

The [runtime ownership contract](runtime-ownership-contract.md) defines handle retention, subscription/task cancellation and cross-window sharing, reflected in this example. The [layout contract](layout-contract.md) places the pinned Taffy engine behind the Go element/window surface while retaining GPUI rounding, measurement and inline behavior. The selected renderer, Windows/text and distribution contracts complete those architecture choices.

## Review and validation limits

The counter, reusable-label, subscriber and two-window snippets are extracted into a separate compile-only module, with external custom-element and managed-view fixtures. Stub bodies do not implement the framework. Compiler acceptance does not establish parity or executable runtime correctness.

Focused source checks used the saved [element contracts](../evidence/core/crates__gpui__src__element.rs), [context adapters](../evidence/core/crates__gpui__src__app__context.rs), [application effects](../evidence/core/crates__gpui__src__app.rs), [action contract](../evidence/core/crates__gpui__src__action.rs), and [window routing](../evidence/core/crates__gpui__src__window.rs). Their pin and provenance are recorded in the [manifest](../evidence/core/manifest.json).

The user-installed Go 1.27.1 runs the root styling tests/vet and isolated signature checks. See the [fixture](../.scratch/signature-validation/README.md) and [validation record](../evidence/authoring-signature-validation.json). Historical Go 1.26.5 styling evidence remains separate. The assistant did not retry the earlier blocked installer.
