package dataurl

import (
	"errors"
	"strings"
)

// Parameter is one MIME type parameter (name, value).
type Parameter struct {
	Name, Value string
}

// Mime is a MIME type record (https://mimesniff.spec.whatwg.org/#mime-type-representation),
// ported from data-url/src/mime.rs:5-40.
type Mime struct {
	Type       string
	Subtype    string
	Parameters []Parameter
}

// ErrMimeParsing reports an unparseable MIME type (data-url/src/mime.rs:42-49).
var ErrMimeParsing = errors.New("invalid mime type")

// Matches reports whether the type and subtype match, ignoring parameters.
func (m Mime) Matches(typ, subtype string) bool { return m.Type == typ && m.Subtype == subtype }

// GetParameter returns the value of the first parameter called name.
func (m Mime) GetParameter(name string) (string, bool) {
	for _, p := range m.Parameters {
		if p.Name == name {
			return p.Value, true
		}
	}
	return "", false
}

// ParseMime parses a MIME type (https://mimesniff.spec.whatwg.org/#parsing-a-mime-type),
// data-url/src/mime.rs:54-83.
func ParseMime(s string) (Mime, error) {
	trimmed := strings.Trim(s, " \t\n\r")
	typ, rest, ok := strings.Cut(trimmed, "/")
	if !onlyHTTPTokenCodePoints(typ) || typ == "" || !ok {
		return Mime{}, ErrMimeParsing
	}
	subtype, params, hasParams := strings.Cut(rest, ";")
	subtype = strings.TrimRight(subtype, " \t\n\r")
	if !onlyHTTPTokenCodePoints(subtype) || subtype == "" {
		return Mime{}, ErrMimeParsing
	}
	var parameters []Parameter
	if hasParams {
		parameters = parseParameters(params)
	}
	return Mime{Type: asciiLower(typ), Subtype: asciiLower(subtype), Parameters: parameters}, nil
}

// parseParameters is data-url/src/mime.rs:91-150, including its handling of
// semicolons and escapes inside quoted values.
func parseParameters(s string) []Parameter {
	var parameters []Parameter
	pieces := strings.Split(s, ";")
	pi := 0
	nextPiece := func() (string, bool) {
		if pi < len(pieces) {
			pi++
			return pieces[pi-1], true
		}
		return "", false
	}
	for {
		piece, ok := nextPiece()
		if !ok {
			return parameters
		}
		piece = strings.TrimLeft(piece, " \t\n\r")
		name, value, hasValue := strings.Cut(piece, "=")
		nameValid := name != "" && onlyHTTPTokenCodePoints(name) && !containsParam(parameters, name)
		if !hasValue {
			continue
		}
		var v string
		if stripped, quoted := strings.CutPrefix(value, `"`); quoted {
			var unescaped strings.Builder
			chars := []rune(stripped)
			ci := 0
		untilClosingQuote:
			for {
				for ci < len(chars) {
					c := chars[ci]
					ci++
					switch c {
					case '"':
						break untilClosingQuote
					case '\\':
						if ci < len(chars) {
							unescaped.WriteRune(chars[ci])
							ci++
						} else if p, ok := nextPiece(); ok {
							// A semicolon inside a quoted value is part of it.
							chars, ci = []rune(p), 0
							unescaped.WriteByte(';')
						} else {
							unescaped.WriteByte('\\')
						}
					default:
						unescaped.WriteRune(c)
					}
				}
				p, ok := nextPiece()
				if !ok {
					break
				}
				unescaped.WriteByte(';')
				chars, ci = []rune(p), 0
			}
			if !nameValid || !validValue(value) {
				continue
			}
			v = unescaped.String()
		} else {
			value = strings.TrimRight(value, " \t\n\r")
			if value == "" || !nameValid || !validValue(value) {
				continue
			}
			v = value
		}
		parameters = append(parameters, Parameter{asciiLower(name), v})
	}
}

func containsParam(parameters []Parameter, name string) bool {
	for _, p := range parameters {
		if p.Name == name {
			return true
		}
	}
	return false
}

// validValue: every char is an HTTP quoted-string token code point
// (data-url/src/mime.rs:156-161).
func validValue(s string) bool {
	for _, c := range s {
		if !(c == '\t' || c >= ' ' && c <= '~' || c >= 0x80 && c <= 0xFF) {
			return false
		}
	}
	return true
}

// String serializes the MIME type (data-url/src/mime.rs:163-188).
func (m Mime) String() string {
	var b strings.Builder
	b.WriteString(m.Type)
	b.WriteByte('/')
	b.WriteString(m.Subtype)
	for _, p := range m.Parameters {
		b.WriteByte(';')
		b.WriteString(p.Name)
		b.WriteByte('=')
		if onlyHTTPTokenCodePoints(p.Value) && p.Value != "" {
			b.WriteString(p.Value)
			continue
		}
		b.WriteByte('"')
		for _, c := range p.Value {
			if c == '"' || c == '\\' {
				b.WriteByte('\\')
			}
			b.WriteRune(c)
		}
		b.WriteByte('"')
	}
	return b.String()
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// onlyHTTPTokenCodePoints uses the IS_HTTP_TOKEN table of
// data-url/src/mime.rs:204-223: ASCII alphanumerics and !#$%&'*+-.^_`|~.
func onlyHTTPTokenCodePoints(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		alnum := '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
		if !alnum && strings.IndexByte("!#$%&'*+-.^_`|~", c) < 0 {
			return false
		}
	}
	return true
}
