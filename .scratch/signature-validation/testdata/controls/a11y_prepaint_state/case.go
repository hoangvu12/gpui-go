package negative
import (
 gpui "gpui-go"
 "gpui-go/consumer"
)
func check() {
 var capability gpui.A11ySyntheticChildren[consumer.PaintState] = &consumer.Mark{}; _ = capability
}

