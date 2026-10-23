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
// script never runs during a frame).
func (v *View) Render(w *gpui.Window, cx *gpui.Context[View]) gpui.AnyElement {
	if v.Host == nil {
		return gpui.Div().IntoElement()
	}
	if v.Host.Notify == nil && w != nil {
		v.Host.Notify = w.Refresh
	}
	return v.Host.Element()
}
