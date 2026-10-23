package main

import (
	"bytes"
	"regexp"
	"unicode/utf8"
)

// rustTest is one upstream `#[test]` / `#[gpui::test]` function.
type rustTest struct {
	Name    string // the fn name
	Attr    int    // 1-based line of the test attribute
	FnLine  int    // 1-based line of the `fn` keyword
	EndLine int    // 1-based line of the closing brace
}

// maskRust blanks every comment, string and char literal in a Rust
// source (newlines kept), so attribute and brace scanning sees code
// only. This is what keeps doc text such as the `#[test]` inside the
// `//!` prose of dock/mod.rs from being counted.
func maskRust(src []byte) []byte {
	out := append([]byte(nil), src...)
	blank := func(from, to int) {
		if to > len(out) {
			to = len(out)
		}
		for k := from; k < to; k++ {
			if out[k] != '\n' {
				out[k] = ' '
			}
		}
	}
	isIdent := func(b byte) bool {
		return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
	}
	n := len(src)
	i := 0
	for i < n {
		c := src[i]
		switch {
		case c == '/' && i+1 < n && src[i+1] == '/':
			j := bytes.IndexByte(src[i:], '\n')
			if j < 0 {
				j = n - i
			}
			blank(i, i+j)
			i += j
		case c == '/' && i+1 < n && src[i+1] == '*':
			depth, j := 1, i+2
			for j < n && depth > 0 {
				if src[j] == '/' && j+1 < n && src[j+1] == '*' {
					depth++
					j += 2
				} else if src[j] == '*' && j+1 < n && src[j+1] == '/' {
					depth--
					j += 2
				} else {
					j++
				}
			}
			blank(i, j)
			i = j
		case (c == 'r' || c == 'b') && (i == 0 || !isIdent(src[i-1])) && rawStringAt(src, i) > 0:
			j := rawStringAt(src, i)
			blank(i, j)
			i = j
		case c == 'b' && (i == 0 || !isIdent(src[i-1])) && i+1 < n && (src[i+1] == '"' || src[i+1] == '\''):
			i++ // the literal itself is handled on the next iteration
		case c == '"':
			j := i + 1
			for j < n && src[j] != '"' {
				if src[j] == '\\' {
					j++
				}
				j++
			}
			blank(i, j+1)
			i = j + 1
		case c == '\'':
			if j := charLiteralEnd(src, i); j > 0 {
				blank(i, j)
				i = j
			} else {
				i++ // a lifetime or label
			}
		default:
			i++
		}
	}
	return out
}

// rawStringAt returns the end offset (exclusive) of a raw string
// literal (r"..", r#".."#, br#".."#) starting at i, or 0.
func rawStringAt(src []byte, i int) int {
	j := i
	if src[j] == 'b' {
		j++
	}
	if j >= len(src) || src[j] != 'r' {
		return 0
	}
	j++
	hashes := 0
	for j < len(src) && src[j] == '#' {
		hashes++
		j++
	}
	if j >= len(src) || src[j] != '"' {
		return 0
	}
	closer := append([]byte{'"'}, bytes.Repeat([]byte{'#'}, hashes)...)
	k := bytes.Index(src[j+1:], closer)
	if k < 0 {
		return len(src)
	}
	return j + 1 + k + len(closer)
}

// charLiteralEnd returns the end offset (exclusive) of a char literal
// starting at the quote i, or 0 when the quote opens a lifetime.
func charLiteralEnd(src []byte, i int) int {
	if i+1 >= len(src) {
		return 0
	}
	if src[i+1] == '\\' {
		j := i + 3
		for j < len(src) && j < i+14 && src[j] != '\'' {
			j++
		}
		if j < len(src) && src[j] == '\'' {
			return j + 1
		}
		return 0
	}
	_, size := utf8.DecodeRune(src[i+1:])
	if i+1+size < len(src) && src[i+1+size] == '\'' && src[i+1] != '\n' {
		return i + 2 + size
	}
	return 0
}

var (
	testAttrRe = regexp.MustCompile(`#\[\s*(?:gpui\s*::\s*)?test\b`)
	fnNameRe   = regexp.MustCompile(`\bfn\s+([A-Za-z_][A-Za-z0-9_]*)`)
)

// scanRustTests finds every test function of one Rust source.
func scanRustTests(src []byte) []rustTest {
	masked := maskRust(src)
	lineAt := func(off int) int { return bytes.Count(masked[:off], []byte{'\n'}) + 1 }
	var tests []rustTest
	for _, m := range testAttrRe.FindAllIndex(masked, -1) {
		fn := fnNameRe.FindSubmatchIndex(masked[m[1]:])
		if fn == nil {
			continue
		}
		nameStart := m[1] + fn[2]
		name := string(masked[nameStart : m[1]+fn[3]])
		open := bytes.IndexByte(masked[nameStart:], '{')
		if open < 0 {
			continue
		}
		end, depth := nameStart+open, 0
		for ; end < len(masked); end++ {
			if masked[end] == '{' {
				depth++
			} else if masked[end] == '}' {
				depth--
				if depth == 0 {
					break
				}
			}
		}
		if end >= len(masked) {
			end = len(masked) - 1
		}
		tests = append(tests, rustTest{
			Name:    name,
			Attr:    lineAt(m[0]),
			FnLine:  lineAt(m[1] + fn[0]),
			EndLine: lineAt(end),
		})
	}
	return tests
}

var wiringStmtRe = regexp.MustCompile(`(?s)(?:pub(?:\s*\([^)]*\))?\s+)?(?:mod\s+[A-Za-z_][A-Za-z0-9_]*\s*;|use\s[^;]*;)`)

// wiringOnly reports a Rust source holding nothing but mod/use
// declarations (and comments): a module-tree file with no code.
func wiringOnly(src []byte) bool {
	rest := wiringStmtRe.ReplaceAll(maskRust(src), nil)
	return len(bytes.TrimSpace(rest)) == 0
}
