package negative
import (
 gpui "gpui-go"
 
)
func check() {
 _ = gpui.DefineUnitAction[struct{}]("x")
}

