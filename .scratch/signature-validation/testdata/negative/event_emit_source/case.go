package negative
import (
 gpui "gpui-go"
 
)
func check(cx *gpui.Context[int]) {
 gpui.DefineEvent[string, string]().Emit(cx, "x")
}

