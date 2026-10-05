package negative
import (
 gpui "gpui-go"
 "gpui-go/consumer"
)
func check() {
 _ = gpui.CustomElement[int, consumer.PaintState](&consumer.Mark{})
}

