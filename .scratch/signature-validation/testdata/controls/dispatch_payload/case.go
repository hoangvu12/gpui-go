package negative
import (
 gpui "gpui-go"
 
)
func check(w *gpui.Window, app *gpui.App) {
 w.Dispatch(gpui.DefineUnitAction[struct{}]("x"), struct{}{}, app)
}

