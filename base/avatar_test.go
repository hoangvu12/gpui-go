package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:avatar.rs tests (2/2, both deviated): the core ImgElement
// has no debug selector (no interactivity, see avatar.go), so the image
// slot's presence is asserted through the Avatar root's bounds (it sizes
// to the 20px image) instead of an "avatar-image" selector.

func avatarHarness(t *testing.T, image bool) *gpui.VisualTestContext {
	t.Helper()
	return drawn(t, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		a := NewAvatar().DebugSelector("avatar")
		if image {
			a.Image(NewAvatarImage("avatar.png").Size(gpui.Px(20)))
		}
		a.Fallback(NewAvatarFallback().Child(gpui.Div().Flex().DebugSelector("avatar-fallback").Size(gpui.Px(20))))
		// ItemsStart keeps the root at its content height (map gap 2b:
		// the pin's flex parent would stretch it).
		return gpui.Div().Flex().ItemsStart().Child(a).IntoElement()
	})
}

// U:avatar.rs:172-178
func TestAvatarImageSlotTakesPrecedenceOverFallback(t *testing.T) {
	cx := avatarHarness(t, true)
	root := debugBounds(t, cx, "avatar")
	if root.Size.Width != 20 || root.Size.Height != 20 {
		t.Fatalf("avatar root = %v, want the 20px image slot", root.Size)
	}
	if _, ok := cx.DebugBounds("avatar-fallback"); ok {
		t.Fatal("fallback rendered alongside the image")
	}
}

// U:avatar.rs:180-186
func TestAvatarFallbackRendersWithoutAnImage(t *testing.T) {
	cx := avatarHarness(t, false)
	if _, ok := cx.DebugBounds("avatar-fallback"); !ok {
		t.Fatal("fallback not rendered")
	}
	if root := debugBounds(t, cx, "avatar"); root.Size.Width != 20 {
		t.Fatalf("avatar root = %v, want the 20px fallback", root.Size)
	}
}
