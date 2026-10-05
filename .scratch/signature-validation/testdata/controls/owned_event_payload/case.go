package negative
import (
 gpui "gpui-go"
 
)
func check(cx *gpui.Context[int]) {
 gpui.DefineEvent[int, string]().EmitOwned(cx, func(*gpui.Scope) string { return "" })
}

