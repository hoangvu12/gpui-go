//go:build windows

package gpui

// Ticket 06 (browser-core): text on a non-opaque window renders
// grayscale. The end-to-end window half: a hidden, off-screen app
// window (never shown — the openHiddenAppWindow pattern) draws text
// first opaque (the platform recommendation: subpixel on a ClearType
// machine, whose recommended mode the fx-0005 recorded trace pins)
// and then with a transparent background: the frame's paint context
// carries the appearance, and the scene's sprite class flips to
// monochrome (grayscale). The scene-level halves of the rule are in
// internal/glyphspec/text_aa_spec_test.go.

import (
	"testing"
)

func TestWindowTextRenderingModeFollowsBackgroundAppearance(t *testing.T) {
	ts, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	recommended, err := ts.RecommendedRenderingMode()
	if err != nil {
		t.Fatalf("recommended rendering mode: %v", err)
	}

	h := openHiddenAppWindow(t, "ticket06 text aa background", Size{Width: 320, Height: 160}, func(w *Window, app *App) View {
		return transformRoot{build: func() AnyElement {
			return Div().TextSize(Px(20)).Child("Grayscale on glass").IntoElement()
		}}
	})

	// drawOnce draws one frame on the host thread and reports the
	// scene's sprite class counts.
	drawOnce := func() (monos, subs int) {
		t.Helper()
		h.onHost(t, func() {
			scene, err := DrawWindowFrame(h.window)
			if err != nil {
				t.Errorf("DrawWindowFrame: %v", err)
				return
			}
			monochrome, err := scene.MonochromeSprites()
			if err != nil {
				t.Errorf("dump monochrome sprites: %v", err)
				return
			}
			subpixel, err := scene.SubpixelSprites()
			if err != nil {
				t.Errorf("dump subpixel sprites: %v", err)
				return
			}
			monos, subs = len(monochrome), len(subpixel)
		})
		return monos, subs
	}

	// The opaque default follows the recommendation.
	monos, subs := drawOnce()
	if monos+subs == 0 {
		t.Fatal("the opaque frame painted no text sprites")
	}
	if recommended == TextRenderingSubpixel && subs == 0 {
		t.Errorf("the opaque frame painted %d subpixel sprites, want the platform recommendation (subpixel) to hold", subs)
	}
	if recommended != TextRenderingSubpixel && monos == 0 {
		t.Errorf("the opaque frame painted %d monochrome sprites, want the platform recommendation (grayscale) to hold", monos)
	}

	// A transparent window background forces grayscale, whatever the
	// recommendation: no subpixel sprite survives the frame.
	h.onHost(t, func() { h.window.SetBackgroundAppearance(WindowBackgroundTransparent) })
	monos, subs = drawOnce()
	if subs != 0 {
		t.Errorf("the transparent frame painted %d subpixel sprites, want 0 (grayscale text on non-opaque windows)", subs)
	}
	if monos == 0 {
		t.Fatal("the transparent frame painted no text sprites at all")
	}
}
