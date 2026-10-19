// Command hello opens one window whose root view is a centered box
// with a label.
//
// The window is driven by the frame loop: the framework draws the root
// view whenever the window needs a frame (it opens dirty; a resize,
// cx.Notify on a rendered view, Window.Refresh or an animation frame
// dirties it again), and the window's present hook (OnRedraw) hands the
// drawn scene to the window's surface. The application never calls
// DrawWindowFrame itself.
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
			Flex().
			Justify(gpui.AlignContentCenter).
			AlignItems(gpui.AlignItemsCenter).
			Size(gpui.PxLength(160), gpui.PxLength(96)).
			Bg(gpui.RgbaToHsla(0x2563ebff)).
			TextColor(gpui.RgbaToHsla(0xffffffff)).
			TextLg().
			Child("Hello!")).
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

	// The window is opened on the host's foreground thread, where its
	// present hook also runs, so shown needs no synchronization.
	var shown *gpui.Window
	present := func() {
		if shown == nil || !shown.Alive() {
			return
		}
		scene := gpui.LastDrawnScene(shown)
		if scene == nil {
			return
		}
		// GetOrCreateSurface also resizes the surface to the window.
		target, err := renderer.GetOrCreateSurface(shown)
		if err != nil {
			log.Print(err)
			return
		}
		// The scene's glyph sprites reference the atlas the window's
		// text system rasterized them into: pass that atlas along with
		// the scene (a sprite-free scene would ignore it).
		atlas, err := shown.GlyphAtlas()
		if err != nil {
			log.Print(err)
			return
		}
		if _, err := target.DrawScene(scene, atlas, gpui.BackgroundOpaque); err != nil {
			log.Print(err)
		}
	}

	host.Post(func() {
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
		shown = window
	})

	// Run returns when the last window closes.
	if err := app.Run(host); err != nil {
		log.Fatal(err)
	}
}
