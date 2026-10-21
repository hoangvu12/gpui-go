// Command compuser is a probe for TestConsumerCompileCost: a package that
// uses five Component[Self] components. It is never run.
package main

import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/buildcostspec/testdata/components"
)

func view() gpui.AnyElement {
	return gpui.Div().
		Child(components.NewButton("ok").Px4().Py2().RoundedMd().Bg(gpui.RgbaToHsla(0x2563ebff)).
			OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {})).
		Child(components.NewCard("card").P4().Child("body")).
		Child(components.NewBadge("new").Px2().TextXs()).
		Child(components.NewToggle("on").When(true, func(t *components.Toggle) *components.Toggle { return t.Opacity(0.5) })).
		Child(components.NewChip("chip").RoundedFull()).
		IntoElement()
}

func main() { _ = view }
