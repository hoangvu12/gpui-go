package negative
import (
 gpui "gpui-go"
 
)
type Div struct{}
func NewDiv() *Div { return nil }
func check() {
 _ = gpui.Div
}

