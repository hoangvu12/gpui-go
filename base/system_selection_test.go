package base

// browser-core 28: selection highlight colours come from the system.
// The input resolution is pinned in input_state_style_test.go; these
// pin the SelectableText paint (the drag-selection component) and the
// seam-level system read.

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// withSystemSelectionColors pins the selection colours the platform
// reports on the test's application (App.SetSelectionColors, the
// forced-colours override's pattern — the core's paint path reads the
// app, which is where the seam lives since the selection model moved,
// browser-core 19).
func withSystemSelectionColors(t *testing.T, ta *gpui.TestApp, colors gpui.SelectionColors) {
	t.Helper()
	pinned := colors
	ta.App().SetSelectionColors(&pinned)
	t.Cleanup(func() { ta.App().SetSelectionColors(nil) })
}

// The default (no injection): the recorded wash of the app's own
// system-preference record — the default record for a hostless app, so
// this never depends on the machine's settings.
func TestSelectableTextSelectionPaintsWithTheSystemWash(t *testing.T) {
	ta := textTestApp(t)
	_, cx := openSelectionView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().SizeFull().Child(TextSelectionLayer{}).Child(
			gpui.Div().Flex().W(gpui.Px(240)).H(gpui.Px(32)).Child(NewSelectableText("local", "alpha beta")),
		).IntoElement()
	})
	drawText(t, cx)
	left := gpui.MouseButtonLeft
	cx.SimulateMouseDown(gpui.Point{X: 1, Y: 12}, left, gpui.Modifiers{})
	cx.SimulateMouseMove(gpui.Point{X: 220, Y: 12}, &left, gpui.Modifiers{})
	cx.SimulateMouseUp(gpui.Point{X: 220, Y: 12}, left, gpui.Modifiers{})
	drawText(t, cx)

	want := systemSelectionColors(cx.App()).Background
	if want == (gpui.Hsla{}) {
		t.Fatal("the system selection wash is unset")
	}
	quads, err := gpui.LastDrawnScene(cx.Window()).Quads()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, q := range quads {
		if q.Background.Solid == want && q.Bounds.Size.Width > 0 {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no selection quad with the system wash %v among %d quads", want, len(quads))
	}
}

// The injected spelling: a distinctive platform wash paints the
// selection, and the explicit SelectionColor override still wins (the
// app's own ::selection styling).
func TestSelectableTextSelectionFollowsTheInjectedSystemColors(t *testing.T) {
	injected := gpui.SelectionColors{
		Background: gpui.Rgb(0x123456).Alpha(0.5),
		Foreground: gpui.Rgb(0xFEDCBA),
	}
	ta := textTestApp(t)
	withSystemSelectionColors(t, ta, injected)

	_, cx := openSelectionView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().SizeFull().Child(TextSelectionLayer{}).Child(
			gpui.Div().Flex().W(gpui.Px(240)).H(gpui.Px(32)).Child(NewSelectableText("local", "alpha beta")),
		).IntoElement()
	})
	drawText(t, cx)
	left := gpui.MouseButtonLeft
	cx.SimulateMouseDown(gpui.Point{X: 1, Y: 12}, left, gpui.Modifiers{})
	cx.SimulateMouseMove(gpui.Point{X: 220, Y: 12}, &left, gpui.Modifiers{})
	cx.SimulateMouseUp(gpui.Point{X: 220, Y: 12}, left, gpui.Modifiers{})
	drawText(t, cx)

	quads, err := gpui.LastDrawnScene(cx.Window()).Quads()
	if err != nil {
		t.Fatal(err)
	}
	painted := false
	for _, q := range quads {
		if q.Background.Solid == injected.Background && q.Bounds.Size.Width > 0 {
			painted = true
			break
		}
	}
	if !painted {
		t.Fatalf("the selection did not paint with the injected system wash %v", injected.Background)
	}

	// The explicit override is the app's ::selection styling and wins.
	own := gpui.Rgb(0x00FF00).Alpha(0.3)
	ta2 := textTestApp(t)
	_, cx2 := openSelectionView(t, ta2, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Flex().SizeFull().Child(TextSelectionLayer{}).Child(
			gpui.Div().Flex().W(gpui.Px(240)).H(gpui.Px(32)).Child(
				NewSelectableText("local", "alpha beta").SelectionColor(own),
			),
		).IntoElement()
	})
	drawText(t, cx2)
	cx2.SimulateMouseDown(gpui.Point{X: 1, Y: 12}, left, gpui.Modifiers{})
	cx2.SimulateMouseMove(gpui.Point{X: 220, Y: 12}, &left, gpui.Modifiers{})
	cx2.SimulateMouseUp(gpui.Point{X: 220, Y: 12}, left, gpui.Modifiers{})
	drawText(t, cx2)
	quads, err = gpui.LastDrawnScene(cx2.Window()).Quads()
	if err != nil {
		t.Fatal(err)
	}
	overridden := false
	for _, q := range quads {
		if q.Background.Solid == own && q.Bounds.Size.Width > 0 {
			overridden = true
			break
		}
	}
	if !overridden {
		t.Fatalf("the SelectionColor override did not win over the system wash")
	}
}
