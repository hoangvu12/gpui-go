// Package authoring supplies fluent styling for custom Go components without
// requiring component authors to generate forwarding methods.
//
// Embed Styled[*YourComponent] by value and return BindStyled(&YourComponent{})
// from the component's constructor. Shared styling methods return the concrete
// component pointer, so they can be interleaved with component-specific methods.
//
// A bound component is mutable and must be used through its original pointer.
// Pointer aliases share mutations. Copying the component by value is unsupported;
// subsequent shared style operations on the copy panic before changing state.
// Embed the helper by value: embedding *Styled would share the helper and defeat
// that copy check. Custom methods remain the component author's responsibility.
// Components are not safe for concurrent mutation.
//
// This package is the initial authoring mechanism, not a renderer, an element
// lifecycle, or the complete GPUI style vocabulary. Its style snapshots can be
// consumed by a later rendering layer without depending on a GPU backend.
package authoring
