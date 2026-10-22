//go:build windows

package base

import (
	"syscall"
	"unsafe"

	"github.com/hoangvu12/gpui-go/gpui"
)

// spiGetClientAreaAnimation is SPI_GETCLIENTAREAANIMATION (WinUser.h).
const spiGetClientAreaAnimation = 0x1042

var procSystemParametersInfoW = syscall.NewLazyDLL("user32.dll").NewProc("SystemParametersInfoW")

// platformReadReduceMotion reads the Windows "Animation effects" switch
// (U:reduce_motion.rs:115-139): SPI_GETCLIENTAREAANIMATION writes one BOOL
// through pvParam and ignores uiParam; animations disabled asks for
// reduced motion. A failed call is the unknown preference.
func platformReadReduceMotion() (reduce bool, ok bool) {
	var animationsEnabled int32
	r, _, _ := procSystemParametersInfoW.Call(
		spiGetClientAreaAnimation,
		0,
		uintptr(unsafe.Pointer(&animationsEnabled)),
		0,
	)
	if r == 0 {
		return false, false
	}
	return animationsEnabled == 0, true
}

// platformFollowReduceMotion follows live changes; Windows posts no
// notification Base can subscribe to without a window
// (U:reduce_motion.rs:141).
func platformFollowReduceMotion(*gpui.App) {}
