package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// motion/reveal.rs has no upstream tests. Go-own: the reveal measures its
// child, requests a frame for the measured height, then lays out at the
// height times the progress (U:motion/reveal.rs:48-104).
func TestMotionRevealSizesToTheMeasuredChildTimesProgress(t *testing.T) {
	ta := gpui.NewTestApp()
	progress := float32(0.5)
	m := openMotionWindow(t, ta, func(*gpui.Window, *gpui.App) gpui.AnyElement {
		child := gpui.Div().Flex().H(gpui.Px(40)).W(gpui.Px(10)).IntoElement()
		return gpui.Div().Flex().W(gpui.Px(80)).Child(
			gpui.Div().Flex().DebugSelector("reveal").Child(NewMotionReveal("reveal", progress, child)),
		).IntoElement()
	})
	height := func() float32 {
		b, ok := m.visual().DebugBounds("reveal")
		if !ok {
			t.Fatal("reveal not drawn")
		}
		return b.Size.Height
	}
	// Unmeasured and revealing: no explicit height yet (an auto height
	// with no layout children).
	if h := height(); h != 0 {
		t.Fatalf("unmeasured height = %v, want 0", h)
	}
	if n := m.pendingFrame(); n != 1 {
		t.Fatalf("the first measurement requested %d frames, want 1", n)
	}
	ta.RunUntilParked()
	if h := height(); h != 20 {
		t.Fatalf("height = %v, want 40 * 0.5", h)
	}
	// An unchanged measurement requests nothing more.
	m.pendingFrame()
	ta.RunUntilParked()
	if n := m.pendingFrame(); n != 0 {
		t.Fatalf("a settled reveal requested %d frames", n)
	}

	progress = 1
	m.refresh()
	if h := height(); h != 40 {
		t.Fatalf("height = %v, want 40", h)
	}
}
