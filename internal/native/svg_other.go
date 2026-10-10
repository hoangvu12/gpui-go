//go:build !windows

package native

import (
	"errors"
)

// SvgService is the non-Windows stub: the SVG service is a Windows
// AMD64 artifact feature (capability bit 9 never present on this
// platform).
type SvgService struct{}

// Svg fetches the SVG service; it is unsupported off Windows.
func (l *Library) Svg() (*SvgService, error) {
	return nil, ErrSvgUnsupportedPlatform
}

// ErrSvgUnsupportedPlatform reports the SVG service's absence on the
// current platform.
var ErrSvgUnsupportedPlatform = errors.New("native: svg service is unsupported on this platform")
