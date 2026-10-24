//go:build dev

package main

// The dev-mode half of tsx-hello (ticket 66, ADR 0003's "development
// reads and watches .tsx files and reloads in place"): the shape
// cmd/tsxdev runs. It compiles the same ui/app.tsx the release build
// embeds, but from disk on every reload (esbuild in the binary — this
// file is behind the `dev` tag so a release build links none of it);
// it restores the restart handoff the previous run saved; and it
// answers the supervisor's commands through script.RunDev: a .tsx
// save reloads in place, and a save-and-exit before a rebuild hands
// the state file to the replacement process.
//
// Start it with the dev command (from this directory):
//
//	go run github.com/hoangvu12/gpui-go/cmd/tsxdev .
//
// Nothing here runs in a release build: dev_stub.go answers instead.

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/gpui/script"
	"github.com/hoangvu12/gpui-go/gpui/script/compile"
	"github.com/hoangvu12/gpui-go/gpui/script/types"
)

// devEntry is the .tsx the dev mode compiles on demand — the same file
// the release bundle was generated from (go:generate in main.go).
const devEntry = "ui/app.tsx"

// runDev is run()'s dev shape: compile.Open instead of the embedded
// bundle, the saved state restored before the window opens, the
// reload-time type check (ticket 63: debug builds check after every
// reload when tsc is present), and the dev session answering the
// supervisor.
func runDev() error {
	// What the previous run left: keyed state, shared values and the
	// window position. A missing file is a fresh start; a corrupt one
	// is reported and also starts fresh.
	state, err := script.ReadDevState(os.Getenv(script.DevEnv))
	if err != nil {
		log.Print(err)
	}

	source, err := compile.Open(devEntry)
	if err != nil {
		return fmt.Errorf("open %s: %w", devEntry, err)
	}
	store := script.NewShared()
	store.Restore(state.Shared)
	host := script.New(
		script.WithSource(source),
		script.WithStateSnapshot(state.State),
		script.WithShared(store),
		// The banner is the dev default: a failed reload mounts it over
		// the window's bottom with the .tsx file and line.
		script.WithErrorHandler(func(err error) { log.Print(err) }),
		script.WithTypeCheck(types.NewChecker(filepath.Dir(devEntry))),
	)
	if err := host.Reload(); err != nil {
		log.Print(err) // the last good tree (here: none yet) and the banner carry it
	}

	// The headless switch (script.DevHeadlessEnv, passed through by
	// whoever runs the supervisor — a test or CI): the window opens far
	// off the desktop and never activates, exactly where `go test`
	// parks its own real windows, so a supervised dev app never shows
	// on the user's screen. A real dev session keeps the app's own
	// position and focus.
	headless := os.Getenv(script.DevHeadlessEnv) == "1"
	origin := gpui.Point{X: 160, Y: 120}
	if headless {
		origin = gpui.Point{X: 20040, Y: 20040}
	}

	return runApp(host, gpui.WindowOptions{
		Title:     "gpui-go tsx-hello (dev)",
		Show:      true,
		Activate:  !headless,
		Resizable: true,
		Bounds: gpui.Bounds{
			Origin: origin,
			Size:   gpui.Size{Width: 480, Height: 320},
		},
	}, func(app *gpui.App, window *gpui.Window) {
		// Restore where the previous run left the window — through the
		// public window API, like everything else here. What the window
		// reads back after a set is not always exactly what was set (the
		// platform's client/frame offset, inherited from the reference:
		// windowRectFromClient splits the frame symmetrically); the
		// prints below read the window, so what they show is what a
		// save will capture and a restart compares against.
		restored := false
		if saved, ok := state.WindowBounds(); ok {
			window.SetBounds(saved)
			restored = true
		}
		if bounds, err := window.Bounds(); err == nil {
			at := "default"
			if restored {
				at = "restored"
			}
			fmt.Printf("dev: window %s x=%.0f y=%.0f w=%.0f h=%.0f\n",
				at, bounds.Origin.X, bounds.Origin.Y, bounds.Size.Width, bounds.Size.Height)
		}
		if headless && restored {
			// A user moves the window with the mouse; a headless run
			// cannot, so it moves itself once — the observable a restart
			// must restore (the dev-command test asserts the replacement
			// reopens here, not at its own default).
			if bounds, err := window.Bounds(); err == nil {
				bounds.Origin.X += 17
				bounds.Origin.Y += 13
				window.SetBounds(bounds)
				if after, err := window.Bounds(); err == nil {
					fmt.Printf("dev: window moved x=%.0f y=%.0f w=%.0f h=%.0f\n",
						after.Origin.X, after.Origin.Y, after.Size.Width, after.Size.Height)
				}
			}
		}

		// The dev session: the supervisor's commands (reload, save)
		// run on the app's foreground thread. Each reload reports what
		// the fresh sources show — the label the view binds over the
		// keyed clicks state.
		script.RunDev(script.DevConfig{
			App:       app,
			Host:      host,
			Window:    window,
			Shared:    store,
			StateFile: os.Getenv(script.DevEnv),
			OnReload: func() {
				fmt.Printf("dev: label=%q pid=%d\n", devLabel(host), os.Getpid())
			},
		})
		fmt.Printf("dev: started pid=%d label=%q\n", os.Getpid(), devLabel(host))
	})
}

// devLabel returns the view's bound label (the id the .tsx gives it),
// the text a reload or a restart has to keep honest.
func devLabel(host *script.Host) string {
	text, _ := host.Text("label")
	return text
}
