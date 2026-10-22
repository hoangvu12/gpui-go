package dataurl

import (
	"errors"
	"fmt"
)

// ErrInvalidBase64 is wrapped by every forgiving-base64 failure
// (data-url/src/forgiving_base64.rs:6-33).
var ErrInvalidBase64 = errors.New("base64 not valid")

func invalidBase64(detail string) error { return fmt.Errorf("%w: %s", ErrInvalidBase64, detail) }

// DecodeBase64 is forgiving_base64::decode_to_vec
// (https://infra.spec.whatwg.org/#forgiving-base64-decode,
// data-url/src/forgiving_base64.rs:70-82). input is ASCII-compatible.
func DecodeBase64(input []byte) ([]byte, error) {
	out := []byte{}
	d := NewDecoder(func(b []byte) error {
		out = append(out, b...)
		return nil
	})
	if err := d.Feed(input); err != nil {
		return nil, err
	}
	if err := d.Finish(); err != nil {
		return nil, err
	}
	return out, nil
}

// Decoder is the streaming forgiving-base64 decoder
// (data-url/src/forgiving_base64.rs:84-174).
type Decoder struct {
	writeBytes      func([]byte) error
	bitBuffer       uint32
	bufferBitLength uint8
	paddingSymbols  uint8
}

// NewDecoder returns a decoder that writes decoded bytes to writeBytes.
func NewDecoder(writeBytes func([]byte) error) *Decoder {
	return &Decoder{writeBytes: writeBytes}
}

func base64Value(c byte) int {
	switch {
	case 'A' <= c && c <= 'Z':
		return int(c - 'A')
	case 'a' <= c && c <= 'z':
		return int(c-'a') + 26
	case '0' <= c && c <= '9':
		return int(c-'0') + 52
	case c == '+':
		return 62
	case c == '/':
		return 63
	}
	return -1
}

// Feed decodes a partial input.
func (d *Decoder) Feed(input []byte) error {
	for _, c := range input {
		v := base64Value(c)
		if v < 0 {
			if c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == 0x0C {
				continue
			}
			if c == '=' {
				if d.paddingSymbols < 255 {
					d.paddingSymbols++
				}
				continue
			}
			return invalidBase64(fmt.Sprintf("symbol with codepoint %d not expected", c))
		}
		if d.paddingSymbols > 0 {
			return invalidBase64("alphabet symbol present after padding")
		}
		d.bitBuffer = d.bitBuffer<<6 | uint32(v)
		if d.bufferBitLength < 18 {
			d.bufferBitLength += 6
		} else {
			if err := d.writeBytes([]byte{byte(d.bitBuffer >> 16), byte(d.bitBuffer >> 8), byte(d.bitBuffer)}); err != nil {
				return err
			}
			d.bufferBitLength = 0
		}
	}
	return nil
}

// Finish signals the end of input and flushes the trailing bytes.
func (d *Decoder) Finish() error {
	switch {
	case d.bufferBitLength == 0 && d.paddingSymbols == 0:
		return nil
	case d.bufferBitLength == 12 && (d.paddingSymbols == 2 || d.paddingSymbols == 0):
		return d.writeBytes([]byte{byte(d.bitBuffer >> 4)})
	case d.bufferBitLength == 18 && (d.paddingSymbols == 1 || d.paddingSymbols == 0):
		return d.writeBytes([]byte{byte(d.bitBuffer >> 10), byte(d.bitBuffer >> 2)})
	case d.bufferBitLength == 6:
		return invalidBase64("lone alphabet symbol present")
	}
	return invalidBase64("incorrect padding")
}
