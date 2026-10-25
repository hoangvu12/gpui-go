// Command sample is the positioncodemod behaviour witness: a view pair
// whose layout depends on the position of elements that rely on the
// default. The PINNED variant states .Relative() explicitly (the
// pre-76 semantics stand-in); the RELIANT variant is the same tree
// with the pins removed — a parent that attaches an absolute child and
// an inset user. It renders both in the hostless test window and
// prints every debug-selector's bounds, tagged by variant. Under the
// static default (browser-core 76, spec D3) the two differ — the
// witness that the flip has teeth — and the codemod pins the reliant
// variant's construction sites, after which the two outputs match line
// for line.
package main

import (
	"fmt"
	"sort"

	"github.com/hoangvu12/gpui-go/gpui"
)

// sampleView is the rendered root view; pinned selects the explicitly
// positioned variant over the default-reliant one.
type sampleView struct {
	pinned bool
}

// Render implements Render.
func (v *sampleView) Render(w *gpui.Window, cx *gpui.Context[sampleView]) gpui.AnyElement {
	if v.pinned {
		return gpui.Div().
			DebugSelector("root").
			SizeFull().
			Flex().
			// The pinned parent: the absolute child resolves against it
			// (the pin's direct-parent rule, which the explicit Relative
			// keeps under the static default).
			Child(gpui.Div().
				DebugSelector("parent").
				Relative().
				Mt(gpui.PxLength(20)).
				Ml(gpui.PxLength(30)).
				W(gpui.PxLength(80)).
				H(gpui.PxLength(60)).
				Child(gpui.Div().
					DebugSelector("abs").
					Absolute().
					Left(gpui.PxLength(12)).
					Top(gpui.PxLength(8)).
					W(gpui.PxLength(20)).
					H(gpui.PxLength(14)))).
			// The pinned inset user: the relative offsets apply.
			Child(gpui.Div().
				DebugSelector("inset").
				Relative().
				Top(gpui.PxLength(10)).
				Left(gpui.PxLength(6)).
				W(gpui.PxLength(40)).
				H(gpui.PxLength(18))).
			IntoElement()
	}
	// The reliant variant: the same tree with the pins removed. The
	// parent relies on the default position (Relative before ticket
	// 76, Static after it) and the inset user does too. These are the
	// construction sites the codemod pins.
	return gpui.Div().
		DebugSelector("root").
		SizeFull().
		Flex().
		Child(gpui.Div().
			DebugSelector("parent").
			Mt(gpui.PxLength(20)).
			Ml(gpui.PxLength(30)).
			W(gpui.PxLength(80)).
			H(gpui.PxLength(60)).
			Child(gpui.Div().
				DebugSelector("abs").
				Absolute().
				Left(gpui.PxLength(12)).
				Top(gpui.PxLength(8)).
				W(gpui.PxLength(20)).
				H(gpui.PxLength(14)))).
		Child(gpui.Div().
			DebugSelector("inset").
			Top(gpui.PxLength(10)).
			Left(gpui.PxLength(6)).
			W(gpui.PxLength(40)).
			H(gpui.PxLength(18))).
		IntoElement()
}

// main renders one frame per variant in the hostless test window
// (deterministic text system, scale 2.0) and prints every selector's
// bounds sorted by name, prefixed with the variant's tag.
func main() {
	ta := gpui.NewTestApp()
	app := ta.App()

	// One window per variant: the hostless test windows are independent,
	// so each variant's frame captures its own debug bounds.
	window := gpui.NewTestWindow(app, gpui.Size{Width: 240, Height: 160})
	pinned := gpui.NewEntity(app, window.Scope(), func(v *sampleView, cx *gpui.Context[sampleView]) {
		v.pinned = true
	})
	window.SetRootView(gpui.ViewOf(pinned))
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		fmt.Println("draw error:", err)
		return
	}
	printBounds("P", gpui.WindowDebugBounds(window))
	gpui.CloseTestWindow(window)

	window2 := gpui.NewTestWindow(app, gpui.Size{Width: 240, Height: 160})
	reliant := gpui.NewEntity(app, window2.Scope(), func(v *sampleView, cx *gpui.Context[sampleView]) {})
	window2.SetRootView(gpui.ViewOf(reliant))
	if _, err := gpui.DrawWindowFrame(window2); err != nil {
		fmt.Println("draw error:", err)
		return
	}
	printBounds("U", gpui.WindowDebugBounds(window2))
	gpui.CloseTestWindow(window2)
}

// printBounds prints one variant's selector bounds sorted by name.
func printBounds(tag string, bounds map[string]gpui.Bounds) {
	selectors := make([]string, 0, len(bounds))
	for selector := range bounds {
		selectors = append(selectors, selector)
	}
	sort.Strings(selectors)
	for _, selector := range selectors {
		b := bounds[selector]
		fmt.Printf("%s:%s: x=%.2f y=%.2f w=%.2f h=%.2f\n",
			tag, selector, b.Origin.X, b.Origin.Y, b.Size.Width, b.Size.Height)
	}
}
