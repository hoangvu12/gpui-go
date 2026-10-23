// Package base is the Go port of GPUI Base (gpui-ce/gpui-component
// crates/base at c08206932417f863062d2ab70cc2854c6ca04238; cited below as
// U: = upstream-base/ce/crates/base/src) over the gpui-go core (cited as
// R: = reference/ce-source/crates/gpui/src at 254b5dbd).
//
// # Package layout
//
// One package mirrors the single upstream crate: every Base module lives
// in this package root, one Go file per Rust module (theme.rs ->
// theme.go, root.rs -> root.go, ...). Dependency-free data structures
// (rope, sum tree, segmentation) live in subpackages under base/ (the
// map's package-layout decision, .scratch/gpui-base/map.md).
//
// # Naming
//
// Upstream names are kept, Go-cased. Rust shapes that Go cannot express
// directly follow fixed rules:
//
//   - An associated function `T::f` (no self) becomes the package function
//     TF: Theme::global -> ThemeGlobal, GlobalState::init ->
//     GlobalStateInit, Root::register_plugin -> RootRegisterPlugin.
//     `T::new` becomes NewT and `Default for T` becomes DefaultT.
//   - An extension trait implemented for foreign gpui types (StyledExt,
//     ElementExt, InteractiveElementExt) becomes a small wrapper type
//     built by TraitOf(element): StyledExtOf(gpui.Div()).HFlex() is the
//     Rust div().h_flex(). The wrapper's methods return the element, so
//     the gpui chain continues.
//   - A trait with default methods implemented by Base components
//     (Selectable) becomes an interface of its required methods plus
//     package functions supplying the defaults (SelectableOpen).
//   - A Rust builder method sharing its name with a public field
//     (IndexPath::section) takes the With prefix (IndexPath.WithSection).
//   - Items that only exist inside a non-public Rust module and would
//     collide in one Go package (root.rs's Tab action next to tabs.rs's
//     Tab component) stay unexported; module-qualified public items that
//     collide with crate-root exports take a suffix and say so in their
//     doc comment (component_traits::Collapsible -> CollapsibleTrait).
package base
