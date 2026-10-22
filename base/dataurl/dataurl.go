// Package dataurl processes `data:` URLs per the WHATWG Fetch Standard
// (https://fetch.spec.whatwg.org/#data-urls). It is a Go port of the
// data-url 0.3.2 crate (MIT OR Apache-2.0, servo/rust-url), which GPUI Base
// uses in text/utils.rs:56-66 (`data_url_image`) and text_view.rs:1456.
// Sources are cited as data-url/src/<file>:<line>.
//
// Like the crate, processing starts from the string rather than a parsed URL.
// It inherits the crate's one known WPT failure, "data://test:test/,X"
// (data-url/tests/wpt.rs:39), which is accepted instead of rejected.
//
// Mapping an image MIME type to an image format (including the legacy
// image/jpg alias) is the caller's job (gpui's ImageFormat::from_mime_type).
package dataurl

import (
	"errors"
	"strings"
)

// Errors returned by Process (data-url/src/lib.rs:48-64).
var (
	ErrNotADataURL = errors.New("not a valid data url")
	ErrNoComma     = errors.New("data url is missing comma delimiting attributes and body")
)

// DataURL is a processed data: URL (data-url/src/lib.rs:42-46).
type DataURL struct {
	mimeType                Mime
	base64                  bool
	encodedBodyPlusFragment string
}

// FragmentIdentifier is the URL's fragment identifier, after `#`
// (data-url/src/lib.rs:123-144).
type FragmentIdentifier string

// ToPercentEncoded serializes the fragment like a parsed URL would.
func (f FragmentIdentifier) ToPercentEncoded() string {
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		c := f[i]
		switch {
		case c == '\t' || c == '\n' || c == '\r':
			continue
		case c <= ' ' || c == '"' || c == '<' || c == '>' || c == '`' || c >= 0x7F:
			percentEncode(c, &b)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// Process is DataUrl::process (data-url/src/lib.rs:70-87).
func Process(input string) (*DataURL, error) {
	afterColon, ok := pretendParseDataURL(input)
	if !ok {
		return nil, ErrNotADataURL
	}
	header, body, ok := findCommaBeforeFragment(afterColon)
	if !ok {
		return nil, ErrNoComma
	}
	mime, b64 := parseHeader(header)
	return &DataURL{mimeType: mime, base64: b64, encodedBodyPlusFragment: body}, nil
}

// MimeType returns the parsed MIME type (data-url/src/lib.rs:89-91).
func (d *DataURL) MimeType() Mime { return d.mimeType }

// Decode streams the decoded body to writeBodyBytes and returns the
// fragment identifier, or nil if there is none (data-url/src/lib.rs:93-108).
// A writeBodyBytes error is returned unchanged; invalid base64 yields an
// error wrapping ErrInvalidBase64.
func (d *DataURL) Decode(writeBodyBytes func([]byte) error) (*FragmentIdentifier, error) {
	if d.base64 {
		return decodeWithBase64(d.encodedBodyPlusFragment, writeBodyBytes)
	}
	return decodeWithoutBase64(d.encodedBodyPlusFragment, writeBodyBytes)
}

// DecodeToVec returns the decoded body and the fragment identifier, or nil
// if there is none (data-url/src/lib.rs:110-120).
func (d *DataURL) DecodeToVec() ([]byte, *FragmentIdentifier, error) {
	body := []byte{}
	frag, err := d.Decode(func(b []byte) error {
		body = append(body, b...)
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return body, frag, nil
}

func isASCIITabOrNewline(c byte) bool { return c == '\t' || c == '\n' || c == '\r' }

func eqFold(a, b byte) bool {
	if 'A' <= a && a <= 'Z' {
		a += 'a' - 'A'
	}
	return a == b
}

// pretendParseDataURL is data-url/src/lib.rs:146-178: trim C0 controls and
// space, match "data:" case-insensitively while ignoring ASCII tab or
// newline, and return what follows the colon with C0/space trimmed.
func pretendParseDataURL(input string) (string, bool) {
	left := strings.TrimLeftFunc(input, func(r rune) bool { return r <= ' ' })
	want := "data:"
	i := 0
	for k := 0; k < len(want); k++ {
		for i < len(left) && isASCIITabOrNewline(left[i]) {
			i++
		}
		if i >= len(left) {
			return "", false
		}
		if k == 4 && left[i] != ':' || k < 4 && !eqFold(left[i], want[k]) {
			return "", false
		}
		i++
	}
	return strings.TrimRightFunc(left[i:], func(r rune) bool { return r <= ' ' }), true
}

// findCommaBeforeFragment is data-url/src/lib.rs:180-190.
func findCommaBeforeFragment(afterColon string) (string, string, bool) {
	for i := 0; i < len(afterColon); i++ {
		switch afterColon[i] {
		case ',':
			return afterColon[:i], afterColon[i+1:], true
		case '#':
			return "", "", false
		}
	}
	return "", "", false
}

// parseHeader is data-url/src/lib.rs:192-241.
func parseHeader(fromColonToComma string) (Mime, bool) {
	trimmed := strings.Trim(fromColonToComma, " \t\n\r")
	mimeType, base64 := removeBase64Suffix(trimmed)
	if !base64 {
		mimeType = trimmed
	}
	var b strings.Builder
	if strings.HasPrefix(mimeType, ";") {
		b.WriteString("text/plain")
	}
	inQuery := false
	for i := 0; i < len(mimeType); i++ {
		c := mimeType[i]
		switch {
		case isASCIITabOrNewline(c):
			continue
		case c <= 0x1F || c >= 0x7F:
			percentEncode(c, &b)
		case inQuery && (c == ' ' || c == '"' || c == '<' || c == '>'):
			percentEncode(c, &b)
		case c == '?':
			inQuery = true
			b.WriteByte('?')
		default:
			b.WriteByte(c)
		}
	}
	mime, err := ParseMime(b.String())
	if err != nil {
		mime = Mime{Type: "text", Subtype: "plain", Parameters: []Parameter{{"charset", "US-ASCII"}}}
	}
	return mime, base64
}

// removeBase64Suffix is data-url/src/lib.rs:243-265: match "base64" and then
// optional spaces and ';' from the end, ignoring ASCII tab or newline, and
// return what precedes the ';'.
func removeBase64Suffix(s string) (string, bool) {
	j := len(s)
	prev := func() (byte, bool) {
		for j > 0 {
			j--
			if !isASCIITabOrNewline(s[j]) {
				return s[j], true
			}
		}
		return 0, false
	}
	for _, want := range []byte("46esab") {
		c, ok := prev()
		if !ok {
			return "", false
		}
		if want >= 'a' && !eqFold(c, want) || want < 'a' && c != want {
			return "", false
		}
	}
	c, ok := prev()
	for ok && c == ' ' {
		c, ok = prev()
	}
	if !ok || c != ';' {
		return "", false
	}
	return s[:j], true
}

func percentEncode(c byte, b *strings.Builder) {
	const hexUpper = "0123456789ABCDEF"
	b.WriteByte('%')
	b.WriteByte(hexUpper[c>>4])
	b.WriteByte(hexUpper[c&0x0F])
}

func hexDigit(c byte) (byte, bool) {
	switch {
	case '0' <= c && c <= '9':
		return c - '0', true
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10, true
	case 'A' <= c && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// decodeWithoutBase64 is data-url/src/lib.rs:274-331: string percent-decode
// that ignores ASCII tab or newline and stops at the first '#'.
func decodeWithoutBase64(s string, write func([]byte) error) (*FragmentIdentifier, error) {
	sliceStart := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '%' && c != '#' && !isASCIITabOrNewline(c) {
			continue
		}
		if i > sliceStart {
			if err := write([]byte(s[sliceStart:i])); err != nil {
				return nil, err
			}
			sliceStart = i
		}
		switch c {
		case '%':
			if i+2 < len(s) {
				h, okH := hexDigit(s[i+1])
				l, okL := hexDigit(s[i+2])
				if okH && okL {
					if err := write([]byte{h<<4 | l}); err != nil {
						return nil, err
					}
					sliceStart = i + 3
				}
			}
		case '#':
			f := FragmentIdentifier(s[i+1:])
			return &f, nil
		default:
			sliceStart = i + 1
		}
	}
	if err := write([]byte(s[sliceStart:])); err != nil {
		return nil, err
	}
	return nil, nil
}

// decodeWithBase64 is data-url/src/lib.rs:333-347.
func decodeWithBase64(s string, write func([]byte) error) (*FragmentIdentifier, error) {
	dec := NewDecoder(write)
	frag, err := decodeWithoutBase64(s, dec.Feed)
	if err != nil {
		return nil, err
	}
	if err := dec.Finish(); err != nil {
		return nil, err
	}
	return frag, nil
}
