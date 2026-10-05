package negative
import (
 gpui "gpui-go"
 
)
func check(e gpui.Entity[int], app *gpui.App) {
 var result int = e.Read(app, func(*int, *gpui.App) int { return 0 }); _ = result
}

