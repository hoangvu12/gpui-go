//go:build windows

package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

// TestCapturePages renders every page offscreen to a PNG, for checking
// the layout without a visible window. It runs only when
// GALLERY_CAPTURE_DIR names the output directory:
//
//	GALLERY_CAPTURE_DIR=/tmp/gallery go test ./examples/base-gallery -run TestCapturePages
//
// It opens one hidden native window (never shown or activated), draws
// each page into it and reads the frame back through
// Renderer.RenderToImage, which renders without presenting.
// GALLERY_CAPTURE_DARK=1 captures the dark theme.
func TestCapturePages(t *testing.T) {
	dir := os.Getenv("GALLERY_CAPTURE_DIR")
	if dir == "" {
		t.Skip("set GALLERY_CAPTURE_DIR to capture the gallery pages")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dark := os.Getenv("GALLERY_CAPTURE_DARK") == "1"

	app := gpui.NewApp()
	host := gpui.NewHost()
	if err := app.Attach(host); err != nil {
		t.Fatal(err)
	}
	if err := host.Start(); err != nil {
		t.Fatal(err)
	}
	runDone := make(chan error, 1)
	go func() { runDone <- app.Run(host) }()
	renderer, err := gpui.NewRenderer()
	if err != nil {
		t.Fatal(err)
	}

	// onHost runs f on the host's foreground thread and waits for it.
	onHost := func(f func()) {
		done := make(chan struct{})
		host.Post(func() {
			defer close(done)
			f()
		})
		select {
		case <-done:
		case <-time.After(30 * time.Second):
			t.Fatal("timed out waiting for the host thread")
		}
	}

	var window *gpui.Window
	var content gpui.Entity[gallery]
	var openErr error
	onHost(func() {
		base.Init(app)
		setTheme(app, dark)
		window, openErr = app.OpenWindowView(gpui.WindowOptions{
			Title:  "gallery capture",
			Show:   false,
			Bounds: gpui.Bounds{Origin: gpui.Point{X: -32000, Y: -32000}, Size: gpui.Size{Width: windowWidth, Height: windowHeight}},
		}, func(w *gpui.Window, app *gpui.App) gpui.View {
			content = gpui.NewEntity(app, w.Scope(), func(g *gallery, cx *gpui.Context[gallery]) {
				*g = newGallery(w, cx)
				g.dark = dark
			})
			root := gpui.NewEntity(app, w.Scope(), func(r *base.Root, cx *gpui.Context[base.Root]) {
				base.NewRoot(r, gpui.ViewOf(content), w, cx)
			})
			return gpui.ViewOf(root)
		})
	})
	t.Cleanup(func() {
		onHost(func() {
			if window != nil && window.Alive() {
				window.Close()
			}
			renderer.Close()
		})
		if err := host.Stop(); err != nil {
			t.Errorf("host stop: %v", err)
		}
		<-runDone
	})
	if openErr != nil {
		t.Fatal(openErr)
	}

	capture := func(name string) {
		// A few frames let deferred work (popups, parses, appear motion)
		// settle.
		for range 4 {
			onHost(func() {
				if _, err := gpui.DrawWindowFrame(window); err != nil {
					t.Errorf("%s: draw: %v", name, err)
				}
			})
			time.Sleep(120 * time.Millisecond)
		}
		var img *gpui.RenderedImage
		var capErr error
		onHost(func() {
			scene := gpui.LastDrawnScene(window)
			if scene == nil {
				capErr = fmt.Errorf("no drawn scene")
				return
			}
			atlas, err := window.GlyphAtlas()
			if err != nil {
				capErr = err
				return
			}
			img, capErr = renderer.RenderToImage(window, scene, atlas, gpui.WindowBackgroundOpaque)
		})
		if capErr != nil {
			t.Fatalf("%s: capture: %v", name, capErr)
		}
		rgba := &image.NRGBA{
			Pix:    append([]byte(nil), img.Pixels()...),
			Stride: img.BytesPerRow(),
			Rect:   image.Rect(0, 0, int(img.Width()), int(img.Height())),
		}
		out, err := os.Create(filepath.Join(dir, name+".png"))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(out, rgba); err != nil {
			t.Fatal(err)
		}
		_ = out.Close()
	}
	// show selects the page through the sidebar tree, as a click does.
	show := func(id string, f func(g *gallery, w *gpui.Window, app *gpui.App)) {
		onHost(func() {
			nav := content.Read(app, func(g *gallery, _ *gpui.App) gpui.Entity[base.TreeState] { return g.nav })
			nav.Update(app, func(st *base.TreeState, tcx *gpui.Context[base.TreeState]) {
				st.SetSelectedIndex(st.IndexOf(id), true, tcx)
			})
			if f != nil {
				content.Update(app, func(g *gallery, cx *gpui.Context[gallery]) {
					f(g, window, app)
					cx.Notify()
				})
			}
		})
	}

	// GALLERY_CAPTURE_PAGES limits the run to a comma-separated id list.
	only := map[string]bool{}
	for _, id := range strings.Split(os.Getenv("GALLERY_CAPTURE_PAGES"), ",") {
		if id != "" {
			only[id] = true
		}
	}
	wanted := func(id string) bool { return len(only) == 0 || only[id] }
	for _, p := range allPages() {
		if !wanted(p.id) {
			continue
		}
		show(p.id, nil)
		capture(p.id)
	}
	// The overlay and alternate states, captured as <id>-open.png.
	for _, id := range []string{"select", "combobox", "date-picker", "color-picker", "popup", "popover",
		"tooltip", "dialog", "alert-dialog", "sheet", "toast", "checkbox", "accordion", "motion"} {
		if !wanted(id) {
			continue
		}
		show(id, func(g *gallery, w *gpui.Window, app *gpui.App) {
			openEverything(g)
			if id == "color-picker" {
				g.colorPicker.Update(app, func(st *base.ColorPickerState, pcx *gpui.Context[base.ColorPickerState]) {
					st.SetOpen(true, pcx)
				})
			}
		})
		capture(id + "-open")
		show(id, func(g *gallery, w *gpui.Window, app *gpui.App) {
			g.selectOpen, g.comboOpen, g.dateOpen = false, false, false
			g.popupOpen, g.popupAboveOpen, g.popoverOpen, g.tooltipPinned = false, false, false, false
			g.dialogOpen, g.alertOpen, g.sheetOpen = false, false, false
			g.colorPicker.Update(app, func(st *base.ColorPickerState, pcx *gpui.Context[base.ColorPickerState]) {
				st.SetOpen(false, pcx)
			})
		})
	}
}
