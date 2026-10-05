package negative
import (
 gpui "gpui-go"
 
)
func check(cx *gpui.Context[int], source gpui.Entity[string]) {
 gpui.DefineEvent[string, int]().Subscribe(cx, source, func(*bool, gpui.Entity[string], *int, *gpui.Context[bool]) {})
}

