package negative
import (
 gpui "gpui-go"
 
)
func check(app *gpui.App, scope *gpui.Scope) {
 _ = gpui.NewEntity(app, scope, func(*int, *gpui.Context[string]) {})
}

