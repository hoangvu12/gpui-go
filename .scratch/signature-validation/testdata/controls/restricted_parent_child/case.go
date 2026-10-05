package negative
import (
 gpui "gpui-go"
 "gpui-go/consumer"
"gpui-go/example"
)
func check() {
 new(consumer.RestrictedParent).Child(example.NewCountLabel(1)); _ = gpui.Div
}

