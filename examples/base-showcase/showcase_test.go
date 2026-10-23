package main

import (
	"testing"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

// TestShowcaseBuildsAndDraws builds the whole component tree in a TestApp
// window (no native window) and draws it closed, then with every overlay
// and alternate state open.
func TestShowcaseBuildsAndDraws(t *testing.T) {
	ta := gpui.NewTestApp()
	t.Cleanup(func() { ta.App().Shutdown() })
	ta.Update(func(app *gpui.App) { base.Init(app) })

	var content gpui.Entity[showcase]
	_, cx := ta.AddWindowView(func(r *base.Root, w *gpui.Window, rcx *gpui.Context[base.Root]) {
		content = gpui.NewEntity(rcx, w.Scope(), func(s *showcase, scx *gpui.Context[showcase]) {
			*s = newShowcase(w, scx)
		})
		base.NewRoot(r, gpui.ViewOf(content), w, rcx)
	})
	draw := func(stage string) {
		t.Helper()
		cx.RunUntilParked()
		if _, err := gpui.DrawWindowFrame(cx.Window()); err != nil {
			t.Fatalf("%s: %v", stage, err)
		}
		cx.RunUntilParked()
	}
	draw("initial")

	content.Update(ta, func(s *showcase, scx *gpui.Context[showcase]) {
		s.selectOpen, s.comboOpen, s.dialogOpen = true, true, true
		s.selectedTab, s.checked, s.switchOn, s.toggled = 2, true, true, true
		scx.Notify()
	})
	draw("overlays open")

	content.Read(ta, func(s *showcase, app *gpui.App) struct{} {
		if n := s.tree.Read(app, func(st *base.TreeState, _ *gpui.App) int { return st.EntryCount() }); n != 7 {
			t.Errorf("tree shows %d entries, want 7 (src and components expanded)", n)
		}
		return struct{}{}
	})
}
