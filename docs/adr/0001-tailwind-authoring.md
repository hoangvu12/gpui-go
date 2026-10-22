# ADR 0001: Tailwind-named authoring extensions beyond the GPUI-CE pin

- Status: Accepted
- Date: 2026-10-22

## Context

gpui-go reproduces GPUI-CE at a pinned commit (core parity, [CONTEXT.md](../../CONTEXT.md)). The pinned `Styled` vocabulary (`gpui/styled.go`, `gpui/styled_generated.go`) already uses Tailwind's names and scale for layout, spacing, sizing, borders, radii, shadows and cursors.

Much gpui-go UI code will be written by LLM coding agents. They know HTML, CSS, Tailwind, React and shadcn well, and they have never seen gpui-go. They reach for Tailwind and do not find:

- the color palette;
- common utilities such as `font-bold`, `leading-tight`, `opacity-50` and `uppercase`;
- one-line state styles. A hover style means a closure that sets `StyleRefinement` fields by hand.
- a way to paste a class string.

## Decision

Add a Tailwind-named authoring layer on top of the pin, for agent and LLM familiarity:

- Tailwind-named `Styled` methods that the pin lacks (`styled_tailwind.go`), so every Tailwind class with a zero-argument equivalent has a method of the same name.
- The Tailwind CSS v3.4 palette as values in package `tw` and as names for `gpui.TailwindColor` (`tailwind_palette_generated.go`, `tw/`), plus the color helpers `Rgb`, `Hex`, `ParseHex`, `Hsla.Opacity` and `Hsla.Alpha` (`color_helpers.go`).
- `gpui.Styles()`, a standalone `*StyleBuilder` with the whole vocabulary (`style_builder.go`), accepted by the state builders.
- `Styled.Class(string)`, `CheckClasses` and `SetClassErrorHandler` (`class.go`, plus `class_generated.go` emitted by `cmd/tailwindgen` from the method names), and the checker `cmd/twcheck`.

The pinned vocabulary stays byte-identical: `styled.go` and `styled_generated.go` are unchanged. The extensions live in separate files, kept apart from the ported code: `styled_tailwind.go`, `class.go`, `class_generated.go`, `tailwind_palette_generated.go`, `color_helpers.go`, `style_builder.go`, `tw/`, `cmd/tailwindgen` and `cmd/twcheck`.

The only change to pinned signatures is in the seven state builders: `Hover`, `GroupHover`, `Active`, `GroupActive`, `Focus`, `InFocus` and `FocusVisible`, on both `*DivElement` and `Interactive[Self]`. Their style parameter widens from `func(StyleRefinement) StyleRefinement` to the `StyleArg` constraint, which accepts that closure or a `*StyleBuilder`. A prebuilt refinement is equivalent to the closure because the pin runs the closure once, on an empty refinement, when the element is built (div.rs:970-977). Closure call sites compile unchanged. A bare `nil` argument no longer compiles, because `F` cannot be inferred; it used to compile and panic at run time.

## Consequences

- Agents write code they already know. The class-to-method rule is mechanical, and `cmd/tailwindgen` derives the class switch from the method names, so a class and its method cannot disagree. [docs/tailwind.md](../tailwind.md) is the guide.
- `Class` parses at run time, so a typo in a class string is a run-time report (stderr, once per string) and not a compile error, unless `cmd/twcheck` runs or a panicking handler is installed. Methods stay compile-checked and are the better choice for computed values. Each distinct string is parsed once and cached.
- The class switch calls `Styled` methods on `*StyleBuilder`, and each one is instantiated inside package `gpui`. A switch over all 3,210 zero-argument methods doubled gpui's own compile time (about 5.6 s to 10.7 s on the maintainer PC; object 26.7 MB to 39.4 MB). So the switch holds only the 399 classes Class cannot apply otherwise. The 1,864 spacing and sizing classes go through the length fallback, and a generated test proves it writes exactly what each method writes. The 947 GPUI-only method classes (negative padding, gap and sizes; `min-size-*`, `max-size-*`) are rejected because Tailwind has no such classes; their methods remain. Measured after the split: gpui compiles in about 6.6 s and its object is 29.8 MB. Consumers are unaffected either way: the lazy type-parameter marker still compiles only the methods a package calls, and `internal/buildcostspec` guards it (probe sizes unchanged).
- Two Tailwind versions meet. The palette, blur scale, `max-w-*` scale, bare `ring` (3px) and bare `shadow` follow v3.4. The pinned `rounded-*` and `shadow-*` tables use v4-style names (`rounded-sm` is 0.25rem, `shadow-sm` is v3's `shadow`). docs/tailwind.md lists the differences.
- Utilities with no GPUI equivalent (z-index, transitions, transforms, responsive and dark variants, gradients, …) are rejected with a reason rather than approximated.
- Code that uses these names does not port back to Rust GPUI verbatim. Core parity is unaffected: the layer is additive, and the pinned closure form still works.
- The palette values are Tailwind CSS's (MIT). Their notice is in [THIRD_PARTY_NOTICES.md](../../THIRD_PARTY_NOTICES.md).
