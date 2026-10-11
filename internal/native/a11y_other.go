//go:build !windows

package native

import (
	"errors"
)

// A11yService is the non-Windows stub: the a11y provider service is a
// Windows AMD64 artifact feature (capability bit 10 never present on
// this platform).
type A11yService struct{}

// A11y fetches the a11y provider service; it is unsupported off
// Windows.
func (l *Library) A11y() (*A11yService, error) {
	return nil, ErrA11yUnsupportedPlatform
}

// ErrA11yUnsupportedPlatform reports the a11y service's absence on the
// current platform.
var ErrA11yUnsupportedPlatform = errors.New("native: a11y service requires the Windows AMD64 artifact")
