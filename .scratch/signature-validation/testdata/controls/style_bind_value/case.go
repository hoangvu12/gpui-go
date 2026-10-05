package negative
import (
 gpui "gpui-go"
 "gpui-go/authoring"
)
type component struct{ authoring.Styled[*component] }
func check() {
 _ = authoring.BindStyled(&component{}); _ = gpui.Div
}

