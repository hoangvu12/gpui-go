package negative
import (
 gpui "gpui-go"
 
)
type First struct{ Value int }
type Second struct{ Value int }
func check() {
 _ = gpui.Event[int, First](gpui.DefineEvent[int, First]())
}

