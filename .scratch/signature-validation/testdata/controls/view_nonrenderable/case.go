package negative
import (
 gpui "gpui-go"
 
)
type viewState struct{}
func (*viewState) Render(*gpui.Window, *gpui.Context[viewState]) gpui.AnyElement { return gpui.AnyElement{} }
func check(e gpui.Entity[int]) {
 _ = e; _ = gpui.ViewOf[viewState](gpui.Entity[viewState]{})
}

