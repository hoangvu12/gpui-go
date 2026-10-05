package negative
import (
 gpui "gpui-go"
 
)
func check() {
 _ = gpui.Event[int, string](gpui.DefineEvent[int, int]())
}

