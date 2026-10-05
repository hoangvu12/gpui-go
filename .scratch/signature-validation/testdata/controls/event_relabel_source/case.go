package negative
import (
 gpui "gpui-go"
 
)
func check() {
 _ = gpui.Event[int, int](gpui.DefineEvent[int, int]())
}

