package negative
import (
 gpui "gpui-go"
 
)
func check() {
 _ = gpui.ActionSpec[int]{Clone: func(v int) int { return v }}
}

