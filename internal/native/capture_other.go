//go:build !windows

package native

import (
	"errors"
)

// CaptureService is the non-Windows stub: the capture service is a
// Windows AMD64 artifact feature (capability bit 11 never present on
// this platform).
type CaptureService struct{}

// Capture fetches the capture service; it is unsupported off Windows.
func (l *Library) Capture() (*CaptureService, error) {
	return nil, ErrCaptureUnsupportedPlatform
}

// ErrCaptureUnsupportedPlatform reports the capture service's absence
// on the current platform.
var ErrCaptureUnsupportedPlatform = errors.New("native: capture service requires the Windows AMD64 artifact")
