// Command sample is the displaycodemod behaviour witness: a small view
// whose layout depends on the display of elements that rely on the
// default (plain Divs, a FlexCol div, a div styled by a helper, an img
// wrapped in a div) and of elements that state their display
// explicitly (Block, Grid). It renders one frame in the hostless test
// window and prints every debug-selector's bounds; the codemod's
// before and after output must match line for line.
package main

import (
	"fmt"
	"sort"

	"github.com/hoangvu12/gpui-go/gpui"
)

// sampleView is the rendered root view.
type sampleView struct{}

// Render implements Render.
func (v *sampleView) Render(w *gpui.Window, cx *gpui.Context[sampleView]) gpui.AnyElement {
	return gpui.Div().
		DebugSelector("root").
		SizeFull().
		// A default-reliant div: its children sit in a row while the
		// default display is Flex.
		Child(gpui.Div().
			DebugSelector("row-a").
			W(gpui.PxLength(40)).
			H(gpui.PxLength(20))).
		// FlexCol sets the direction, not the display: still reliant.
		Child(gpui.Div().
			DebugSelector("col").
			FlexCol().
			Child(gpui.Div().
				DebugSelector("col-b1").
				W(gpui.PxLength(30)).
				H(gpui.PxLength(10))).
			Child(gpui.Div().
				DebugSelector("col-b2").
				W(gpui.PxLength(30)).
				H(gpui.PxLength(10)))).
		// A div whose display is set by the helper it is passed to: the
		// codemod pins the constructor (redundant, later setter wins)
		// and the bounds must stay block-flow.
		Child(blockBox(gpui.Div().
			DebugSelector("helper-block").
			W(gpui.PxLength(24)).
			H(gpui.PxLength(24)))).
		// Explicit display: unchanged by the codemod.
		Child(gpui.Div().
			DebugSelector("grid").
			Grid().
			GridCols(2).
			W(gpui.PxLength(50)).
			H(gpui.PxLength(20)).
			Child(gpui.Div().
				DebugSelector("grid-cell").
				W(gpui.PxLength(10)).
				H(gpui.PxLength(10)))).
		// A helper-returned default-reliant div.
		Child(chip()).
		// A text child inside a default-reliant host.
		Child(gpui.Div().
			DebugSelector("text-host").
			Child("hi")).
		// A canvas wrapped in a default-reliant host.
		Child(gpui.Div().
			DebugSelector("canvas-host").
			Child(gpui.Canvas(
				func(b gpui.Bounds, w *gpui.Window, app *gpui.App) struct{} { return struct{}{} },
				func(b gpui.Bounds, _ struct{}, w *gpui.Window, app *gpui.App) {},
			).
				Size(gpui.PxLength(12)))).
		IntoElement()
}

// chip returns a default-reliant element the codemod pins inside this
// helper.
func chip() *gpui.DivElement {
	return gpui.Div().
		DebugSelector("chip").
		W(gpui.PxLength(14)).
		H(gpui.PxLength(14))
}

// blockBox sets the display on an element passed to it (the helper
// styling the task's rule speaks about: the codemod pins the root at
// the call site, and this later setter still wins).
func blockBox(d *gpui.DivElement) *gpui.DivElement {
	return d.Block()
}

// main renders one frame in the hostless test window (deterministic
// text system, scale 2.0) and prints every selector's bounds sorted by
// name.
func main() {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := gpui.NewTestWindow(app, gpui.Size{Width: 240, Height: 160})
	view := gpui.NewEntity(app, window.Scope(), func(v *sampleView, cx *gpui.Context[sampleView]) {})
	window.SetRootView(gpui.ViewOf(view))
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		fmt.Println("draw error:", err)
		return
	}
	bounds := gpui.WindowDebugBounds(window)
	selectors := make([]string, 0, len(bounds))
	for selector := range bounds {
		selectors = append(selectors, selector)
	}
	sort.Strings(selectors)
	for _, selector := range selectors {
		b := bounds[selector]
		fmt.Printf("%s: x=%.2f y=%.2f w=%.2f h=%.2f\n",
			selector, b.Origin.X, b.Origin.Y, b.Size.Width, b.Size.Height)
	}
}
