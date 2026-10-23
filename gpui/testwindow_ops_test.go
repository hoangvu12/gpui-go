package gpui

import "testing"

// TestTestWindowOperationsAreNoOps checks that a test window, which has
// no platform host, takes the fire-and-forget window operations as
// no-ops instead of panicking: a view can set its window's title from
// Render, or close its window, under a TestApp as under a real host.
func TestTestWindowOperationsAreNoOps(t *testing.T) {
	ta := NewTestApp()
	t.Cleanup(func() { ta.App().Shutdown() })
	w := NewTestWindow(ta.App(), Size{Width: 200, Height: 100})
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("a test window operation panicked: %v", r)
		}
	}()
	w.SetTitle("title")
	w.Show()
	w.Hide()
	w.Activate()
	w.SetBounds(Bounds{Size: Size{Width: 300, Height: 200}})
	w.Close()
}
