package negative
import (
 gpui "gpui-go"
 
)
func check(e gpui.Entity[int], app *gpui.App) {
 _ = e.Read(app, func(*string, *gpui.App) int { return 0 })
}

