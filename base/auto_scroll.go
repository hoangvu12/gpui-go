package base

// Timer-based auto-scrolling during drag interactions (U:auto_scroll.rs)
// — moved into the core with the selection model (browser-core 19,
// gpui/auto_scroll.go: the model's drag auto-scroll owns it); this file
// re-exports the surface Base's TextViews use.

import "github.com/hoangvu12/gpui-go/gpui"

// AutoScroll manages timer-based auto-scrolling during drag interactions.
type AutoScroll = gpui.AutoScroll

// DefaultAutoScroll returns an idle auto-scroll.
func DefaultAutoScroll() AutoScroll { return gpui.DefaultAutoScroll() }

// AutoScrollComputeDelta computes the scroll delta for a pointer y
// position within the viewport.
var AutoScrollComputeDelta = gpui.AutoScrollComputeDelta
