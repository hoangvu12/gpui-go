package negative
import (
 gpui "gpui-go"
 
)
type Payload struct{ Value int }
func check() {
 _ = gpui.DefineUnitAction[Payload]("x")
}

