package negative
import (
 gpui "gpui-go"
 
)
type fake struct{}
func (fake) access() any { return nil }
func check(e gpui.Entity[int]) {
 e.Update((*gpui.App)(nil), func(*int, *gpui.Context[int]) {})
}

