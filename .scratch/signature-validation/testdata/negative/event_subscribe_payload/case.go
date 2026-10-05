package negative
import (
 gpui "gpui-go"
 
)
func check(cx *gpui.Context[int], source gpui.Entity[string]) {
 gpui.DefineEvent[string, int]().Subscribe(cx, source, func(*int, gpui.Entity[string], *string, *gpui.Context[int]) {})
}

