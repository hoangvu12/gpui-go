package script

import (
	"github.com/hoangvu12/gpui-go/gpui"
)

// View is the entity state that renders a Host: mount it as the
// window's root and the host's tree becomes the window's content,
// redrawn whenever the host asks (it wires Host.Notify to
// Window.Refresh on first render).
//
//	host := script.New(script.WithSource(source))
//	host.Reload()
//	ta.Update(func(app *gpui.App) {
//		root := gpui.NewEntity(app, window.Scope(),
//			func(v *script.View, _ *gpui.Context[script.View]) { v.Host = host })
//		window.SetRootView(gpui.ViewOf(root))
//	})
type View struct {
	// Host is the script host this view renders.
	Host *Host
}

// Render implements Render: the described tree, lowered fresh (the
// script never runs during a frame — the one exception is
// requestAnimationFrame, whose callbacks run at the frame request
// before this frame paints).
//
// The first render also wires what the host needs from the window:
// Notify to Refresh, and the scheduler to the window's application
// (AppScheduler), so timers, fetch and async actions work without
// explicit wiring when the host is mounted.
func (v *View) Render(w *gpui.Window, cx *gpui.Context[View]) gpui.AnyElement {
	if v.Host == nil {
		return gpui.Div().IntoElement()
	}
	if cx != nil {
		if v.Host.Notify == nil && w != nil {
			v.Host.Notify = w.Refresh
		}
		if v.Host.scheduler == nil {
			v.Host.scheduler = AppScheduler(cx.App())
		}
	}
	if w != nil && v.Host.frameCallbacksPending() {
		// The pending requestAnimationFrame callbacks run at the
		// window's next frame request, before that frame paints: what
		// they change shows in the frame they were drawn for.
		w.OnNextFrame(func(*gpui.Window, *gpui.App) {
			v.Host.runFrameCallbacks()
		})
	}
	return v.Host.Element()
}
