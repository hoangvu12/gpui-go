package negative
import (
 gpui "gpui-go"
 "gpui-go/consumer"
)
func check() {
 var capability gpui.A11ySyntheticChildren[int] = &consumer.Mark{}; _ = capability
}

