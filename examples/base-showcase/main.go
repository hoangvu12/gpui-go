// Command base-showcase opens one window showing a representative set of
// GPUI Base components (package github.com/hoangvu12/gpui-go/base):
// buttons and toggles, an input, a textarea and a code editor, a select
// and a combobox, tabs, a dialog and a popover, a markdown TextView, a
// plot, a resizable split, a dock area and a tree.
//
// Base components are unstyled: they own interaction, focus, keyboard
// and accessibility, and the application supplies every visual through
// the Styled API. The sections follow the upstream showcase
// (gpui-component crates/base/examples/showcase at c0820693).
//
//	go run ./examples/base-showcase
//
// The window is driven by the frame loop exactly like examples/hello: the
// framework draws the root view when it is dirty and the present hook
// hands the scene to the window's surface. The root view is a base.Root
// around the showcase, so overlays (dialog, popover, select popups) and
// text selection have their window layers.
package main

import (
	"log"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

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
			Title:     "gpui-go Base showcase",
			Show:      true,
			Activate:  true,
			Resizable: true,
			Bounds: gpui.Bounds{
				Origin: gpui.Point{X: 80, Y: 60},
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
	windowWidth  = 1180
	windowHeight = 860
)

// newRootView builds the window's root: a base.Root around the showcase
// (U:root.rs:147-162 Root::new).
func newRootView(w *gpui.Window, app *gpui.App) gpui.View {
	showcase := gpui.NewEntity(app, w.Scope(), func(s *showcase, cx *gpui.Context[showcase]) {
		*s = newShowcase(w, cx)
	})
	root := gpui.NewEntity(app, w.Scope(), func(r *base.Root, cx *gpui.Context[base.Root]) {
		base.NewRoot(r, gpui.ViewOf(showcase), w, cx)
	})
	return gpui.ViewOf(root)
}
