package negative
import (
 gpui "gpui-go"
 
)
func check() {
 var e gpui.Event[string, int] = gpui.DefineEvent[int, int](); _ = e
}

