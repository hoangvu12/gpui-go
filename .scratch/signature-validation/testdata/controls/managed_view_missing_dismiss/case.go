package negative
import (
 gpui "gpui-go"
 
)
type screen struct{}
func (*screen) Render(*gpui.Window, *gpui.Context[screen]) gpui.AnyElement { return gpui.AnyElement{} }
func (*screen) FocusHandle() gpui.FocusHandle { return gpui.FocusHandle{} }
func check(e gpui.Entity[screen]) {
 _ = gpui.ViewOf(e)
}

