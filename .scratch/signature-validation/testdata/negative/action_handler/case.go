package negative
import (
 gpui "gpui-go"
 
)
func check() {
 gpui.Div().OnAction(gpui.DefineUnitAction[struct{}]("x"), func(*int, *gpui.Window, *gpui.App) {})
}

