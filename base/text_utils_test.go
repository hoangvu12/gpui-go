package base

import (
	"bytes"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of the U:text/utils.rs tests (74-124): 2 of 2.

// U:text/utils.rs:74-100
func TestDataURLImage(t *testing.T) {
	image := func(url string) (gpui.ImageFormat, []byte) {
		t.Helper()
		img := dataURLImage(url)
		if img == nil {
			t.Fatalf("expected an embedded image for %q", url)
		}
		return img.Format, img.Bytes
	}
	for _, c := range []struct {
		url    string
		format gpui.ImageFormat
		bytes  []byte
	}{
		{"data:image/png;base64,iVBORw0KGgo=", gpui.ImageFormatPNG, []byte("\x89PNG\r\n\x1a\n")},
		// Legacy mime aliases and the percent-encoded (non-base64) body form.
		{"data:image/jpg;base64,/9j/4A==", gpui.ImageFormatJPEG, []byte("\xff\xd8\xff\xe0")},
		{"data:image/svg+xml,%3Csvg%3E%3C/svg%3E", gpui.ImageFormatSVG, []byte("<svg></svg>")},
	} {
		format, got := image(c.url)
		if format != c.format || !bytes.Equal(got, c.bytes) {
			t.Errorf("%q = (%v, %q), want (%v, %q)", c.url, format, got, c.format, c.bytes)
		}
	}
	for _, url := range []string{
		"https://example.com/logo.png",
		"data:text/plain;base64,aGVsbG8=",
		"data:image/png;base64,not*base64",
		"data:image/png",
	} {
		if dataURLImage(url) != nil {
			t.Errorf("%q decoded to an image", url)
		}
	}
}

// U:text/utils.rs:102-124
func TestListItemPrefix(t *testing.T) {
	one, three, four, zero := uint32(1), uint32(3), uint32(4), uint32(0)
	for _, c := range []struct {
		ix      int
		start   *uint32
		ordered bool
		depth   int
		want    string
	}{
		{0, &one, true, 0, "1. "}, {1, &one, true, 0, "2. "}, {2, &one, true, 0, "3. "},
		{10, &one, true, 0, "11. "}, {0, &three, true, 0, "3. "}, {1, &three, true, 0, "4. "},
		{0, &one, true, 1, "A. "}, {1, &one, true, 1, "B. "}, {0, &four, true, 1, "D. "},
		{1, &four, true, 1, "E. "}, {0, &one, true, 2, "a. "}, {1, &one, true, 2, "b. "},
		{6, &one, true, 2, "g. "}, {0, &zero, true, 1, "0. "}, {1, &zero, true, 1, "1. "},
		{0, nil, false, 0, "• "}, {0, nil, false, 1, "◦ "}, {0, nil, false, 2, "▪ "},
		{0, nil, false, 3, "‣ "}, {0, nil, false, 4, "⁃ "},
	} {
		if got := listItemPrefix(c.ix, c.start, c.ordered, c.depth); got != c.want {
			t.Errorf("listItemPrefix(%d, %v, %v, %d) = %q, want %q", c.ix, c.start, c.ordered, c.depth, got, c.want)
		}
	}
}
