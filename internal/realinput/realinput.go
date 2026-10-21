// Package realinput gates the tests that drive the physical console:
// the real cursor (SetCursorPos), injected hardware input (SendInput /
// mouse_event), or the foreground (activating windows, SetFocus,
// SetForegroundWindow/AttachThreadInput). They are opt-in through
// GPUI_REAL_INPUT_TESTS=1 so a plain `go test ./...` never moves the
// user's mouse or steals their focus — an interactive session (a game,
// a typing human) must stay undisturbed.
//
// Windows that merely show without activating (SW_SHOWNOACTIVATE, the
// default for Show without Activate) and synthetic in-process messages
// (SendMessageW) do not touch the console and stay ungated.
package realinput

import (
	"os"
	"testing"
)

// EnvVar opts the real-input/foreground corpus in.
const EnvVar = "GPUI_REAL_INPUT_TESTS"

// Enabled reports whether the real-input/foreground corpus is enabled.
func Enabled() bool { return os.Getenv(EnvVar) == "1" }

// Require skips t unless the real-input/foreground corpus is enabled
// (EnvVar=1). Call it at the top of every test that moves the real
// cursor, injects hardware input, or opens/activates a window into the
// foreground.
func Require(t testing.TB) {
	t.Helper()
	if !Enabled() {
		t.Skip("skipped: drives the real cursor/input or takes the foreground; set " + EnvVar + "=1 to run")
	}
}
