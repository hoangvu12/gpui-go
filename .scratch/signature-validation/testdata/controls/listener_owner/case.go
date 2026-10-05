package negative
import (
 gpui "gpui-go"
 
)
func check(cx *gpui.Context[int]) {
 _ = cx.Listener(func(*int, *gpui.ClickEvent, *gpui.Window, *gpui.Context[int]) {})
}

