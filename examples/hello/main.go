// Command hello opens one window whose root view is a centered box.
//
// The frame loop is explicit: the element tree draws into a scene with
// DrawWindowFrame, and the window's surface presents that scene. The
// host's paint hook (OnRedraw) repeats both steps whenever Windows asks
// the window to repaint, for example after a resize.
package main

import (
	"log"

	"github.com/hoangvu12/gpui-go/gpui"
)

type hello struct{}

func (h *hello) Render(w *gpui.Window, cx *gpui.Context[hello]) gpui.AnyElement {
	return gpui.Div().
		Flex().
		SizeFull().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.RgbaToHsla(0x1e293bff)).
		Child(gpui.Div().
			Size(gpui.PxLength(160), gpui.PxLength(96)).
			Bg(gpui.RgbaToHsla(0x2563ebff))).
		IntoElement()
}

func main() {
	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		log.Fatal(err)
	}
	if err := host.Start(); err != nil {
		log.Fatal(err)
	}

	renderer, err := gpui.NewRenderer()
	if err != nil {
		log.Fatal(err)
	}
	defer renderer.Close()

	// shown and target are only touched on the host's foreground thread,
	// where OnRedraw and posted work run.
	var shown *gpui.Window
	var target *gpui.Surface
	present := func() {
		if shown == nil || target == nil || !shown.Alive() {
			return
		}
		scene, err := gpui.DrawWindowFrame(shown)
		if err != nil {
			log.Print(err)
			return
		}
		// The scene has no glyph sprites, so it needs no atlas.
		if _, err := target.DrawScene(scene, nil, gpui.BackgroundOpaque); err != nil {
			log.Print(err)
		}
	}

	window, err := app.OpenWindowView(gpui.WindowOptions{
		Title:     "gpui-go hello",
		Show:      true,
		Activate:  true,
		Resizable: true,
		Bounds: gpui.Bounds{
			Origin: gpui.Point{X: 160, Y: 120},
			Size:   gpui.Size{Width: 480, Height: 320},
		},
		OnRedraw: present,
	}, func(w *gpui.Window, app *gpui.App) gpui.View {
		return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(*hello, *gpui.Context[hello]) {}))
	})
	if err != nil {
		log.Fatal(err)
	}
	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		log.Fatal(err)
	}
	host.Post(func() {
		shown, target = window, surface
		present()
	})

	// Run returns when the last window closes.
	if err := app.Run(host); err != nil {
		log.Fatal(err)
	}
}
