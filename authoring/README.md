# Fluent custom components

The selected method is **value embedding of a generic helper, bound once in a constructor**. Component authors need no generator, reflection, unsafe pointer conversion, or repetitive forwarding methods.

```go
import "gpui-go/authoring"

type Button struct {
    authoring.Styled[*Button]
    disabled bool
}

func NewButton() *Button {
    return authoring.BindStyled(&Button{})
}

func (b *Button) Disabled(value bool) *Button {
    b.disabled = value
    return b
}

// Styling preserves *Button, including its custom methods.
button := NewButton().Flex().Disabled(true).Px3().Gap2()
style := button.Style()
```

`gpui-go` is the local module name; a public distribution path has not been chosen.

## Usage contract

- Embed `Styled[*YourType]` **by value**. Keep using the constructor's pointer.
- `BindStyled` infers the types and binds once. Nil owners and repeated binding panic with an explanation.
- Copies of bound components fail on shared styling and snapshot operations before changing state. Pointer aliases deliberately refer to the same mutable component. Custom methods must respect the same ownership rule.
- The zero value is unbound. Snapshot and fluent operations require binding.
- `Style()` returns an independent value snapshot. `Refine` changes only present fields; explicit zero values remain meaningful.
- Concurrent mutation is unsupported. Frame consumption and entity lifetime rules belong to the future runtime, not this helper.

## Implemented scope

The package supplies binding, copy checks, concrete fluent return types, presence-aware snapshots, and representative style methods: `Flex`, `PaddingX`, `Px3`, `Gap`, `Gap2`, and `Opacity`. The spacing helpers retain rem units.

This is a working authoring mechanism, not the complete GPUI framework or style vocabulary. Rendering, windows, `Div`, child conversion, event/context APIs, and component libraries are not implemented. The accepted `.Child("Hello")` choice remains a design decision for the later element API.

## Validation

With the selected Go 1.27 toolchain available, normal commands are:

```text
go test ./...
go vet ./...
```

Those product commands have **not** been verified on Go 1.27. The authorized MSI upgrade was rejected before execution, leaving Go 1.26.5 installed. Tests and vet passed on byte-identical copies of this package in the isolated `.scratch/authoring-probe` module using that compiler. The root `go.mod` retains Go 1.27.0; the fixture does not change the chosen language floor.

The external-package tests cover two custom component types, concrete return types, refinement ordering and presence, independent snapshots, copy rejection, aliases, nil and unbound misuse. [Research and alternatives](../research/09-fluent-authoring-methods.md), [validation record](../evidence/authoring-validation.json).
