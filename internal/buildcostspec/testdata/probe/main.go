// Command probe is the consumer package TestConsumerCompileCost compiles:
// a view that uses the built-in styled elements (Div, Img, Canvas) the
// way application code does. It is never run.
package main

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
)

type probe struct{ count int }

func (p *probe) Render(w *gpui.Window, cx *gpui.Context[probe]) gpui.AnyElement {
	return gpui.Div().
		ID("root").
		Flex().
		FlexCol().
		SizeFull().
		P4().
		Gap2().
		RoundedLg().
		Border1().
		Bg(gpui.RgbaToHsla(0x1e293bff)).
		TextColor(gpui.RgbaToHsla(0xffffffff)).
		When(p.count > 0, func(d *gpui.DivElement) *gpui.DivElement { return d.Opacity(0.5) }).
		OnClick(cx.Listener(func(v *probe, _ *gpui.ClickEvent, _ *gpui.Window, cx *gpui.Context[probe]) {
			v.count++
			cx.Notify()
		})).
		Child(fmt.Sprintf("Count: %d", p.count)).
		Child(gpui.Img("logo.png").Size(gpui.PxLength(32))).
		Child(gpui.Canvas(
			func(gpui.Bounds, *gpui.Window, *gpui.App) int { return p.count },
			func(gpui.Bounds, int, *gpui.Window, *gpui.App) {},
		).W(gpui.PxLength(10)).H(gpui.PxLength(10))).
		IntoElement()
}

func main() { _ = &probe{} }
