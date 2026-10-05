# Fluent custom components without author generation

Research date: 2026-10-04. Scope: the custom-component authoring mechanism only. The user asked to research the best method and then implement it; this report recommends the narrow mechanism. It does not select rendering, layout, entity ownership, or Base implementation.

## Recommendation

Use a generic styling helper embedded **by value** in a custom component, instantiated with the concrete component pointer. Bind the helper to its owner once in the component constructor. Common styling methods return that concrete pointer, preserving chains before and after component-specific methods. Component authors write ordinary Go and run ordinary builds; they do not generate forwarding methods.

Illustrative authoring shape (names subject to the accompanying implementation):

```go
type Badge struct {
    gpui.Styled[*Badge]
    label string
}

func NewBadge(label string) *Badge {
    return gpui.BindStyled(&Badge{label: label})
}

func (b *Badge) Label(label string) *Badge {
    b.label = label
    return b
}

// Common methods return *Badge, retaining Label in either ordering.
// NewBadge("New").Px3().Label("Updated").Bg(color)
```

This supersedes the earlier research ranking of generated per-component forwarding methods as the leading option. That ranking did not sufficiently account for the user's requirement to preserve the Rust authoring convenience for third-party component authors. The new recommendation deliberately accepts constructor initialization and pointer ownership rules instead.

## What GPUI supplies

At the pinned GPUI-CE revision, `Styled` requires one style-storage accessor and supplies methods returning `Self`; procedural macros provide many repetitive method families. Component authors implement the trait and receive its defaults through Rust's normal compilation. There is no separate user-run generation command in this path. This was checked against the local source snapshot and GitHub CLI contents API at the pinned revision, whose `styled.rs` Git blob is `2bcfda641d6cd0bb9fed81e976f99a51fb226dae`. [Pinned Styled source](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/styled.rs), [style macros](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_macros/src/styles.rs).

Go embedding promotes methods but does not change the actual receiver to the enclosing component. Consequently, a plain helper returning itself loses the component's methods after a style call. An instantiated generic helper can explicitly return the owner type, but needs an owner reference supplied during initialization. [Effective Go: embedding](https://go.dev/doc/effective_go#embedding).

## Alternatives compared

The following assessments are design inferences for this project's requirements, not claims that Go recommends a particular GUI architecture.

| Method | Concrete component survives style calls | Custom author workflow | Main cost | Result |
|---|---|---|---|---|
| Generic embedded helper with owner binding | Yes | Embed helper and use a constructor | Initialization and no-copy contract | Recommended |
| Generated forwarding methods per component | Yes | Generate/update source when component or supported surface changes | Extra author workflow, generated source and tooling | Optional escape hatch only |
| Handwritten forwarding methods | Yes | Implement every desired shared method | Repetition scales with components and styles | Poor default |
| Generic wrapper `Style(component)` | Returns wrapper unless explicitly unwrapped | Wrap, style, unwrap for custom calls | Breaks arbitrary method interleaving | Useful optional adapter |
| Standalone style functions | Can return their input type | Nested calls or separate statements | Different authoring syntax | Useful secondary interface |
| Single universal builder | Common methods only | Simple core calls | Custom methods disappear or require runtime dispatch | Does not satisfy the main requirement |
| Reflection or unsafe outer-pointer reconstruction | Could simulate ownership | Hidden mechanism | Fragile layout assumptions or runtime type machinery | Unnecessary here |

Go's generator is a separate command, not part of `go build`. Library maintainers may use it to maintain the helper's own repetitive methods and commit the output. That would not require consumers or custom component authors to run a generator. [Go generation documentation](https://go.dev/blog/generate).

## Binding and misuse checks

Recommended binding derives the helper from the owner, rather than accepting an unrelated helper/owner pair. A public accessor such as `StyleBase() *Styled[T]` is promoted from the embedded helper, so ordinary authors need not implement it manually. A pointer-constrained package function can reject nil pointers before calling the accessor:

```go
// Signature validated in an isolated compiler probe; implementation tests follow.
func BindStyled[V any, T interface {
    *V
    StyleBase() *Styled[T]
}](owner T) T
```

The Go team's generic-interface article demonstrates the related two-type-parameter pattern connecting a value type to its pointer and required methods. Applying it here permits a direct nil check and avoids reflection. During this task, an isolated probe run by the implementation agent with installed Go 1.26.5 confirmed inference of both type parameters, the nil check, promoted accessor binding, and the helper-copy check. That probe validates the language mechanism; the actual package still needs its own tests. [Generic interfaces: pointer receivers](https://go.dev/blog/generic-interfaces).

Implementation requirements:

1. Store the owner and the address of the initialized helper. Check this address before every shared style mutation and snapshot read.
2. Reject unbound use with an actionable diagnostic naming the constructor/binding requirement. A zero-value component is not a valid fluent builder until bound.
3. Reject repeated binding, including rebinding a copied initialized helper. Silent repair conceals accidental copies and is not cloning.
4. Reject copied bound helpers before mutation: a copied self reference otherwise points back to the original component.
5. Require value embedding of the helper. Pointer embedding shares the exact helper address between copied outer structs, defeating this copy check.
6. Treat pointer aliases as intentional aliases to one mutable builder. Assignment of a pointer is not cloning. Concurrent mutation is unsupported.
7. Return an independent style snapshot; if fields later contain slices, maps, or pointers, a shallow struct copy will no longer suffice.

These checks only cover methods owned by the helper. Arbitrary custom methods remain the component author's responsibility. A copied component's custom setter can run before a shared styling method detects the copy; the library cannot intercept every user method. Likewise, deliberately overriding the accessor incorrectly can defeat the binding convention. These are public authoring rules, not memory-safety guarantees inferred from Go's type system.

## Extensibility and limits

An external package can embed the exported helper and add its own methods to its own component type. Method-name conflicts need ordinary Go handling: a directly declared component method can hide a promoted method, and competing embedded helpers can introduce ambiguity. Keep capability helpers distinct; supporting styles should not automatically add children, focus, or event capabilities to every component. [Go struct promotion](https://go.dev/ref/spec#Struct_types), [method sets](https://go.dev/ref/spec#Method_sets).

This does not recreate Rust's entire extension-trait system. Go still prevents an unrelated package from declaring new methods on another package's type. External style bundles can use ordinary helper functions, explicitly supported customization operations, or a wrapper type. Generic fluent helpers solve reusable common methods on opt-in types; they do not change that language restriction. [Go method declarations](https://go.dev/ref/spec#Method_declarations).

The helper uses methods on a generic receiver type, a facility that predates Go 1.27. It does not depend on newly introduced independent method type parameters. The accepted project floor remains Go 1.27, and the policy-blocked installation should not be retried or bypassed for this feature. No toolchain upgrade was performed in this research.

The self reference makes a component/helper reference cycle. It must not be mistaken for ownership of a GPUI entity, subscription, task, or native resource. Those lifetimes remain separate decisions. Nor does this design reproduce Rust's consuming builder semantics: it preserves fluent return types through a documented mutable pointer interface.

## Required validation for the narrow implementation

Follow-up: the [authoring package](../authoring/README.md) now implements the selected approach. [Validation results](../evidence/authoring-validation.json) record tests and vet on byte-identical sources under installed Go 1.26.5. This confirms the mechanism and documented misuse checks within that environment; the accepted Go 1.27 project build remains unverified.

- A custom component in an external test package mixes shared styles and its own methods in both orders; return values retain the exact concrete pointer type and identity.
- Nil binding, unbound use, repeated binding, copied bound structs, and repeated style overrides behave as documented.
- The copy failure occurs before shared style state changes; the original component remains unchanged.
- Explicit zero values in styles retain presence information.
- Returned snapshots cannot mutate the builder, and aliases intentionally see shared changes.
- Builds require no generated consumer files, reflection, unsafe pointer arithmetic, native libraries, or installation workarounds.

This mechanism demonstrates authoring feasibility. It does not demonstrate rendering parity, the whole upstream styling inventory, GPU performance, or framework readiness. Test results and the actual method subset belong in the implementation record rather than being assumed by this report.
