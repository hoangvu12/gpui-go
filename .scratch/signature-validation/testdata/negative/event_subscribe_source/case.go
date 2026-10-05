package negative
import (
 gpui "gpui-go"
 
)
func check(cx *gpui.Context[int], source gpui.Entity[string]) {
 gpui.DefineEvent[bool, int]().Subscribe(cx, source, func(*int, gpui.Entity[bool], *int, *gpui.Context[int]) {})
}

