// Command base-gallery opens one window with a page for every GPUI Base
// component (package github.com/hoangvu12/gpui-go/base): a sidebar tree
// lists the groups and components, and clicking an entry shows that
// component's page, in a few representative states, in the scrollable
// area on the right. The header switches the Base theme between light
// and dark.
//
//	go run ./examples/base-gallery           # opens on the Button page
//	go run ./examples/base-gallery slider    # opens on a page by id
//
// Base components are unstyled: they own interaction, focus, keyboard and
// accessibility, and every visual on these pages is application styling,
// drawn from the Base theme's colour tokens. The gallery uses the public
// base API only, as an outside application would.
//
// The bootstrap follows examples/base-showcase: the framework draws the
// root view when it is dirty and the present hook hands the scene to the
// window's surface. The root view is a base.Root around the gallery, so
// overlays (dialogs, popovers, sheets) and text selection have their
// window layers.
package main

import (
	"log"
	"os"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

func main() {
	if len(os.Args) > 1 {
		if _, ok := pageByID(os.Args[1]); !ok {
			log.Fatalf("unknown page %q", os.Args[1])
		}
		startPage = os.Args[1]
	}
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
		target, err := renderer.GetOrCreateSurface(shown)
		if err != nil {
			log.Print(err)
			return
		}
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
		base.Init(app)
		window, err := app.OpenWindowView(gpui.WindowOptions{
			Title:     "gpui-go Base gallery",
			Show:      true,
			Activate:  true,
			Resizable: true,
			Bounds: gpui.Bounds{
				Origin: gpui.Point{X: 60, Y: 40},
				Size:   gpui.Size{Width: windowWidth, Height: windowHeight},
			},
			OnRedraw: present,
		}, newRootView)
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

const (
	windowWidth  = 1240
	windowHeight = 860
)

// newRootView builds the window's root: a base.Root around the gallery.
func newRootView(w *gpui.Window, app *gpui.App) gpui.View {
	g := gpui.NewEntity(app, w.Scope(), func(g *gallery, cx *gpui.Context[gallery]) {
		*g = newGallery(w, cx)
	})
	root := gpui.NewEntity(app, w.Scope(), func(r *base.Root, cx *gpui.Context[base.Root]) {
		base.NewRoot(r, gpui.ViewOf(g), w, cx)
	})
	return gpui.ViewOf(root)
}
