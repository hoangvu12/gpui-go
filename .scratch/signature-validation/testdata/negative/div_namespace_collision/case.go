package negative
import (
 gpui "gpui-go"
 
)
type Div struct{}
func Div() *Div { return nil }
func check() {
 _ = gpui.Div
}

