# Go interface fidelity and lifetime design

**Follow-up:** the later [focused research](09-fluent-authoring-methods.md) selects a constructor-bound generic embedded helper over required per-component generation. The [authoring package](../authoring/README.md) implements that bounded mechanism. Earlier rankings and statements that no authoring code exists are historical; the other API/lifetime proposals remain unresolved.

Research date: 2026-10-04. This is a source-backed design investigation, not an implemented interface or a settled specification. Examples below are proposed API shapes; they were not compiled.

## Findings that change the design

1. Fluent styling is readily expressible, but preserving the concrete component type throughout a chain needs deliberate design.
2. Go 1.27 adds generic methods. The installed toolchain is Go 1.26.5. This version decision affects context/listener syntax, and older claims that Go never supports generic methods are obsolete.
3. Go 1.27 still excludes generic methods from interface contracts. Keep runtime-polymorphic drawing and platform interfaces concrete, while typed convenience operations can live on concrete contexts.
4. Rust's deterministic Drop behavior cannot be replaced by garbage-collector timing. Entity, subscription, task, and GPU-resource lifetimes are public behavior.
5. Style refinements require explicit presence information; a zero value cannot always mean unspecified.

The versioned generic-method findings are grounded in the [Go 1.27 release notes](https://go.dev/doc/go1.27), [generic-method explanation](https://go.dev/blog/generic-methods), and [method specification](https://go.dev/ref/spec#Method_declarations). Local evidence in [evidence/go-language](../evidence/go-language/) records the older installed language grammar and runtime documentation. No upgrade was performed.

## What the upstream interface actually asks of Go

GPUI-CE's `Styled` trait requires access to a `StyleRefinement` and supplies numerous consuming methods that return `Self`. Several groups are expanded through procedural macros. `InteractiveElement` and related traits layer interaction methods onto elements. This is why styling works across many concrete types while preserving access to each type's own methods. [Styled source](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/styled.rs), [style macros](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_macros/src/styles.rs), [interaction traits](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/interactive.rs).

The Go target should preserve the useful composition property, not merely produce one attractive `Div` example. In particular, both `Button(...).Disabled(true).Px3()` and `Button(...).Px3().Disabled(true)` should remain possible if equivalent upstream chains are part of the chosen contract. A widget author's custom methods must remain available after common styling operations.

### Candidate fluent designs

| Proposal | Strength | Cost or risk | Assessment |
|---|---|---|---|
| Generate small forwarding methods on concrete element/component types | Concrete return types; familiar chains; ordinary Go debugging | Generated source volume; generator/version management | Leading option for fidelity; benchmark compile cost |
| Embed a generic helper parameterized by the outer type | Reuses method definitions while returning outer type | Initialization, owner references, aliasing, copying, and zero-value traps | Worth a later isolated interface experiment |
| Return one universal builder type from everything | Small common interface | Component-specific methods and compile-time restrictions can be lost | Insufficient alone for an extensible ecosystem |
| Apply standalone style functions or a style configuration object | Simple implementation; reusable operations | Moves away from the user's preferred syntax | Useful internal representation, not default public replacement |

This is analysis from Go method-set rules and upstream `Self`-returning traits. Plain embedding of a non-generic base does not rewrite the return type of its promoted methods: a method returning the base still returns the base. The same-package restriction on method declarations also means third-party packages cannot add methods directly to a core type; extension points need wrappers, helpers, or supported generation. See [Go method sets](https://go.dev/ref/spec#Method_sets) and [method declarations](https://go.dev/ref/spec#Method_declarations).

Recommended experiment, once implementation is authorized: translate one Base button, one input, and one user-defined component into each candidate design. Exercise common-style/component-specific call interleaving, copying, aliases, and zero values. Evaluate actual compile diagnostics and allocations. No approach is selected by this report.

### Recognizable view syntax

An illustrative shape is:

```go
// Proposed usage only. No gpui-go package exists yet.
return gpui.Div().
    Flex().
    ItemsCenter().
    Gap2().
    Child(base.Button("save").
        Px3().
        H7().
        Child(gpui.Text("Save changes")))
```

The method chain is feasible. Exact names, the constructor's concrete return type, and whether `Child("text")` is accepted are unresolved. Capitals and trailing dots are ordinary Go adaptations. The explicit `Text` above is a proposed type-safe conversion, not a restriction the user has accepted.

### Children and implicit conversions

GPUI's `IntoElement` covers a convenient heterogeneous set. A Go method taking a small `Element` interface can accept concrete elements and wrappers, but a built-in string does not acquire methods. Three plausible choices are an explicit text constructor, a checked `Child(any)` conversion layer, or separate convenience methods. The second most closely reproduces call-site brevity while weakening static validation. Do not silently choose that tradeoff for the entire ecosystem. [Upstream element conversions and Render](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/element.rs).

`[]ConcreteElement` and `[]Element` also need an explicit conversion or an iterator/helper contract. Bulk child construction is a common path in lists and should be part of the syntax corpus. Avoid solving every conversion with reflection in a rendering hot path.

### Typed contexts and generic methods

For Go 1.26 compatibility, operations introducing an independent type parameter use package functions or generated specialized methods. A method on `Context[T]` can already use its receiver's `T`; an operation generic over a separate event type needs another mechanism.

With a Go 1.27 floor, a concrete context can introduce an event parameter on a method, making a typed listener operation closer to GPUI's ergonomics. Runtime interfaces still cannot demand that generic method. A compatible design may use concrete generic contexts plus ordinary non-generic internal dispatch interfaces.

These are language-level possibilities, not proof a complete listener adapter has been designed. The next review must cover event-type inference, callback argument order, weak entity capture, return/error handling, and whether retaining a context beyond its phase is valid. A Go closure makes capturing a context easy; the port must prevent or clearly reject stale context use.

Go's toolchain selection can download a newer toolchain when a module requires one. If Go 1.27 is selected, installation/download/storage implications belong in the developer-experience budget. [Toolchain documentation](https://go.dev/doc/toolchain).

### Custom Element state and Render

The Rust `Element` trait has associated request-layout and prepaint state, while `Render` can return an opaque concrete element. A Go runtime interface needs a uniform method signature. Candidate implementations include typed adapters around custom elements, internally erased state records, or callbacks closing over typed state. All should preserve phase order and prevent state from being reused in a later frame accidentally.

A broad `any` field for each state stage is possible but shifts errors to runtime. A giant closed union prevents external widgets from defining new element types. The preferred direction is an open custom-element interface with typed helpers; the exact representation remains an experiment. Tests must include a component defined in a separate package, because core-only examples conceal extension problems.

### Mutation and builder ownership

Rust's consuming builder methods naturally discourage retaining two independent mutable references to one partially built element. Go pointer builders can be aliased. Value builders still share slice/map backing storage unless copying is specified carefully.

Proposed policy for evaluation: frame-local mutable builders with a defined consumed/finalized state, explicit cloning where supported, and debug checks for cross-frame retention or double attachment. Another option is value-style construction with copy-on-write storage, whose costs need measurement. The report does not claim Go can enforce Rust move semantics at compile time.

### Style refinement is a three-state problem in places

Upstream distinguishes absent style fields from explicit changes. An override setting opacity to zero, disabling a flag, or clearing a value must not vanish because a Go field has its zero value. A Go refinement can use presence bits, optional fields, or tagged values with explicit clear semantics. Avoid a design where `false`, `0`, and an empty string universally mean no override. Validate ordering across base styles, inheritance, hover/focus/disabled state, and transition values. [Styled refinement entry point](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/styled.rs).

## Lifetimes: the largest semantic incompatibility

GPUI subscriptions unsubscribe when dropped; `detach` deliberately preserves the registration until the relevant entities are dropped. The implementation has a real destructor, so keeping or releasing the handle changes behavior. [Subscription implementation](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/subscription.rs).

Go `runtime.AddCleanup` runs asynchronously after loss of reachability, without specified ordering, and is not guaranteed before program exit. `weak.Pointer` follows garbage-collector reachability, not a UI entity's logical lifetime. These facilities are useful, but they do not reproduce deterministic entity release or unsubscribe. [AddCleanup](https://pkg.go.dev/runtime#AddCleanup), [weak.Pointer](https://pkg.go.dev/weak#Pointer).

### Lifetime choices to resolve

| Object | Required observable contract | Proposed Go mechanism to assess |
|---|---|---|
| Entity | Valid while owned; stale access rejected; release hooks ordered | Typed ID plus generation, explicit owner/scope, logical disposal |
| Weak entity | Does not retain entity; upgrade fails after logical disposal | Registry lookup by ID/generation, independent of GC timing |
| Subscription | Deterministic stop; clear semantics for detach | Explicit Cancel/Close or owner scope registration |
| Task | Cancellation/detach semantics; late result cannot mutate removed view | Context cancellation plus scope/epoch checks |
| Native/GPU resource | Released on correct thread when no longer in flight | Explicit release queue coordinated with backend completion |
| Element state | Identity persists appropriately; temporary state expires | Frame storage plus retained state registry and generation checks |

Automatic scope ownership may offer friendlier Go usage, but it changes what holding an entity/subscription means. Exact refcount behavior could instead be expressed through explicit Retain/Release operations, at the cost of a more error-prone application interface. Neither choice is accepted yet.

No proposal should rely on a finalizer firing to stop a listener, close a window, cancel work, or free a texture within a frame budget. Cleanups can provide a fallback or diagnostic path. A root-owned registry that keeps every entity strongly referenced also defeats GC-based weak lifetime handling until explicit removal is implemented.

## Threading and allocation proposals

Confine UI state and lifecycle transitions to a dispatcher associated with the native UI thread; worker goroutines send results back through it. Respect each platform's main-thread constraints. `runtime.LockOSThread` provides thread affinity, not permission to violate a platform's startup-thread requirement. [Runtime thread documentation](https://pkg.go.dev/runtime#LockOSThread).

Keep draw primitives in reusable typed slices, clear retained pointers when recycling storage, reuse shaping buffers, and measure closures and interface escapes. A reset slice can keep objects reachable in its backing array; capacity management and pointer clearing must be intentional. `sync.Pool` is a reuse mechanism whose contents may disappear, so logical UI state must not depend on it. [Pool contract](https://pkg.go.dev/sync#Pool).

GC work depends on allocation and live-pointer scanning. Reducing allocations is a plausible optimization, not a measured speedup. CPU scene construction, layout, input latency, and GPU execution all need separate measurement; moving paint to the GPU does not remove CPU-side work. [Go GC guide](https://go.dev/doc/gc-guide).

## Interface review corpus

The future compatibility specification should contain side-by-side upstream and proposed Go usage for:

1. A counter with a typed listener, notification, and two observing views.
2. A Base button combining generic styles and button-specific methods in both orders.
3. A reusable component written outside the core package, with child slots and optional styles.
4. A keyed list reordered while one item owns focus and another has an active async task.
5. A dialog with focus restoration, click-outside behavior, and nested popovers.
6. An input holding subscriptions and IME composition state across redraws.
7. A custom element with distinct layout/prepaint/paint state and custom hit testing.
8. A dropped view whose pending background work and detached listeners must obey declared lifecycle rules.
9. An explicit zero/false/clear style override through hover and disabled refinements.
10. An application that imports only core versus one importing Base/editor packages.

This corpus is proposed research output. No fixtures, generator, Go module, implementation, or tests were created. See [decision brief](06-decision-brief.md) for choices requiring user input.
