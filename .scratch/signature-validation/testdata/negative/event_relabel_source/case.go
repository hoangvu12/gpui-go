package negative
import (
 gpui "gpui-go"
 
)
func check() {
 _ = gpui.Event[string, int](gpui.DefineEvent[int, int]())
}

