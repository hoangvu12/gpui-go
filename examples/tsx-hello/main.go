// Command tsx-hello is the TSX UI scripting sample (ADR 0003): the
// window's content is the .tsx view in ui/app.tsx, compiled to
// ui.tsxbundle.js at build time (go generate) and embedded in this
// binary — the release shape of scripting. The shipped app reads no
// scripts from disk and links no esbuild; development would instead
// compile on demand and reload on save (gpui/script/compile, the dev
// command of ticket 66).
//
// The same binary self-tests its release shape: -selftest loads the
// embedded bundle, renders one frame in an offscreen test window (no
// visible window, no platform host) and exits 0 — the check the
// bundling tests run with the .tsx sources removed.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/gpui/script"
)

// The build-time bundle: go generate runs cmd/tsxbundle (esbuild) on
// ui/app.tsx and writes ui.tsxbundle.{js,go,_test.go} beside this
// file; the compiled UI is embedded in the binary at build time.
//
//go:generate go run ../../cmd/tsxbundle -var UI -entry ui/app.tsx

func main() {
	selftest := flag.Bool("selftest", false, "load the embedded UI bundle, render one frame headless and exit (the release check; no window is shown)")
	flag.Parse()
	if *selftest {
		if err := selfTest(); err != nil {
			fmt.Fprintln(os.Stderr, "tsx-hello -selftest:", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

// selfTest is the release check, headless: it loads the embedded
// bundle, mounts the host in an offscreen test window (a deterministic
// window with no HWND and no platform host — nothing is shown), draws
// a frame, and exercises one click through the host so the check proves
// the embedded script actually runs (keyed state, a bound value and an
// event handler), not merely that it parses.
func selfTest() error {
	host := script.New(script.WithoutBanner())
	if err := host.Load(UI.Bundle()); err != nil {
		return fmt.Errorf("load the embedded bundle: %w", err)
	}

	ta := gpui.NewTestApp()
	window := gpui.NewTestWindow(ta.App(), gpui.Size{Width: 480, Height: 320})
	defer func() {
		gpui.CloseTestWindow(window)
		ta.App().Shutdown()
	}()
	ta.Update(func(app *gpui.App) {
		root := gpui.NewEntity(app, window.Scope(), func(v *script.View, _ *gpui.Context[script.View]) { v.Host = host })
		window.SetRootView(gpui.ViewOf(root))
	})
	cx := gpui.NewVisualTestContext(window, ta)
	host.Notify = window.Refresh
	cx.RunUntilParked()

	if _, err := gpui.DrawWindowFrame(window); err != nil {
		return fmt.Errorf("render a frame: %w", err)
	}
	// The frame shows the scripted view: the card is laid out where the
	// tree put it.
	bounds, ok := cx.DebugBounds("card")
	if !ok {
		return errors.New("no frame bounds for the card: the embedded view did not render")
	}
	if bounds.Size.Width != 160 || bounds.Size.Height != 96 {
		return fmt.Errorf("the card rendered at %v, want 160x96 (the .tsx sizes)", bounds)
	}

	// One click through the host: the bound label recomputes.
	if got, _ := host.Text("label"); got != "Hello TSX! 0" {
		return fmt.Errorf("label before the click = %q", got)
	}
	if err := host.Click("card"); err != nil {
		return fmt.Errorf("click the card: %w", err)
	}
	if got, _ := host.Text("label"); got != "Hello TSX! 1" {
		return fmt.Errorf("label after the click = %q, want the count to update", got)
	}
	return nil
}

// run is the windowed app: the embedded view is the window's root, and
// the host redraws it whenever the script changes what it shows.
func run() error {
	host := script.New(
		script.WithoutBanner(),
		script.WithErrorHandler(func(err error) { log.Print(err) }),
	)
	if err := host.Load(UI.Bundle()); err != nil {
		return fmt.Errorf("load the embedded bundle: %w", err)
	}

	app := gpui.NewApp()
	h := gpui.NewHost()
	if err := app.Attach(h); err != nil {
		return err
	}
	if err := h.Start(); err != nil {
		return err
	}

	renderer, err := gpui.NewRenderer()
	if err != nil {
		return err
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

	h.Post(func() {
		window, err := app.OpenWindowView(gpui.WindowOptions{
			Title:     "gpui-go tsx-hello",
			Show:      true,
			Activate:  true,
			Resizable: true,
			Bounds: gpui.Bounds{
				Origin: gpui.Point{X: 160, Y: 120},
				Size:   gpui.Size{Width: 480, Height: 320},
			},
			OnRedraw: present,
		}, func(w *gpui.Window, app *gpui.App) gpui.View {
			return gpui.ViewOf(gpui.NewEntity(app, w.Scope(),
				func(v *script.View, _ *gpui.Context[script.View]) { v.Host = host }))
		})
		if err != nil {
			log.Fatal(err)
		}
		shown = window
	})

	// Run returns when the last window closes.
	return app.Run(h)
}
