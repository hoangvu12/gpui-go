package negative
import (
 gpui "gpui-go"
 "gpui-go/consumer"
)
func check() {
 _ = gpui.CustomElement[consumer.LayoutState, consumer.PaintState](&consumer.Mark{})
}

