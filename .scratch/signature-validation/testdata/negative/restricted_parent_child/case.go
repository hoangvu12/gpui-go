package negative
import (
 gpui "gpui-go"
 "gpui-go/consumer"
"gpui-go/example"
)
func check() {
 new(consumer.RestrictedParent).Child(gpui.Div()); _ = example.CountLabel{}
}

