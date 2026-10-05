package negative
import (
 gpui "gpui-go"
 
)
type First struct{ Value int }
type Second struct{ Value int }
func check() {
 _ = gpui.Event[int, Second](gpui.DefineEvent[int, First]())
}

