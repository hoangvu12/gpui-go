package base

// Port of U:input/base/mask_pattern.rs.
//
// Go adaptation: the Rust enums with payloads become structs with a Kind.
// MaskToken::Sep(char) is MaskToken{Kind: MaskTokenKindSep, Sep: c}.
// MaskPattern::Pattern{pattern, tokens} and MaskPattern::Number{separator,
// fraction} share one struct; Option<char> separator is 0 for None (NUL is
// never a group separator), and Option<usize> fraction is HasFraction plus
// Fraction. SharedString results are plain strings.

import "strings"

// MaskTokenKind is the MaskToken variant (U:input/base/mask_pattern.rs:5-19).
type MaskTokenKind uint8

const (
	// MaskTokenKindDigit is `[0-9]`.
	MaskTokenKindDigit MaskTokenKind = iota
	// MaskTokenKindLetter is `[a-zA-Z]`.
	MaskTokenKindLetter
	// MaskTokenKindLetterOrDigit is `[a-zA-Z0-9]`.
	MaskTokenKindLetterOrDigit
	// MaskTokenKindSep is a literal separator (MaskToken.Sep).
	MaskTokenKindSep
	// MaskTokenKindAny is any character.
	MaskTokenKindAny
)

// MaskToken is one position of a mask pattern
// (U:input/base/mask_pattern.rs:5-19).
type MaskToken struct {
	Kind MaskTokenKind
	// Sep is the separator char of a MaskTokenKindSep token.
	Sep rune
}

func isASCIIDigit(c rune) bool { return c >= '0' && c <= '9' }
func isASCIIAlpha(c rune) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }

// IsAny reports the any-character token (U:input/base/mask_pattern.rs:23-26).
func (t MaskToken) IsAny() bool { return t.Kind == MaskTokenKindAny }

// isMatch reports whether ch matches the token
// (U:input/base/mask_pattern.rs:28-39).
func (t MaskToken) isMatch(ch rune) bool {
	switch t.Kind {
	case MaskTokenKindDigit:
		return isASCIIDigit(ch)
	case MaskTokenKindLetter:
		return isASCIIAlpha(ch)
	case MaskTokenKindLetterOrDigit:
		return isASCIIDigit(ch) || isASCIIAlpha(ch)
	case MaskTokenKindAny:
		return true
	case MaskTokenKindSep:
		return t.Sep == ch
	}
	return false
}

// isSep is U:input/base/mask_pattern.rs:41-44.
func (t MaskToken) isSep() bool { return t.Kind == MaskTokenKindSep }

// IsNumber reports the digit token (U:input/base/mask_pattern.rs:46-49).
func (t MaskToken) IsNumber() bool { return t.Kind == MaskTokenKindDigit }

// Placeholder is the separator itself or '_'
// (U:input/base/mask_pattern.rs:51-56).
func (t MaskToken) Placeholder() rune {
	if t.Kind == MaskTokenKindSep {
		return t.Sep
	}
	return '_'
}

// maskChar is U:input/base/mask_pattern.rs:58-64.
func (t MaskToken) maskChar(ch rune) rune {
	if t.Kind == MaskTokenKindSep {
		return t.Sep
	}
	return ch
}

// unmaskChar is U:input/base/mask_pattern.rs:66-74.
func (t MaskToken) unmaskChar(ch rune) (rune, bool) {
	if t.Kind == MaskTokenKindSep {
		return 0, false
	}
	return ch, true
}

// MaskPatternKind is the MaskPattern variant
// (U:input/base/mask_pattern.rs:77-91).
type MaskPatternKind uint8

const (
	// MaskPatternKindNone is no mask (the default).
	MaskPatternKindNone MaskPatternKind = iota
	// MaskPatternKindPattern is a token pattern (Pattern, Tokens).
	MaskPatternKindPattern
	// MaskPatternKindNumber is number grouping (Separator, Fraction).
	MaskPatternKindNumber
)

// MaskPattern is an input mask (U:input/base/mask_pattern.rs:77-91). The
// zero value is MaskPattern::None.
type MaskPattern struct {
	Kind MaskPatternKind
	// Pattern and Tokens are the MaskPatternKindPattern payload.
	Pattern string
	Tokens  []MaskToken
	// Separator is the MaskPatternKindNumber group separator, e.g. ',' or
	// ' '; 0 is None.
	Separator rune
	// Fraction is the number of fraction digits when HasFraction, e.g. 2
	// for 123.45.
	Fraction    int
	HasFraction bool
}

// NewMaskPattern is MaskPattern::new and `From<&str>`
// (U:input/base/mask_pattern.rs:93-131): `9` digit, `A` letter, `#` letter
// or digit, `*` any, anything else a separator. For example
// `(999)999-9999` masks a US phone number as (123)456-7890.
func NewMaskPattern(pattern string) MaskPattern {
	tokens := make([]MaskToken, 0, len(pattern))
	for _, ch := range pattern {
		switch ch {
		case '9':
			tokens = append(tokens, MaskToken{Kind: MaskTokenKindDigit})
		case 'A':
			tokens = append(tokens, MaskToken{Kind: MaskTokenKindLetter})
		case '#':
			tokens = append(tokens, MaskToken{Kind: MaskTokenKindLetterOrDigit})
		case '*':
			tokens = append(tokens, MaskToken{Kind: MaskTokenKindAny})
		default:
			tokens = append(tokens, MaskToken{Kind: MaskTokenKindSep, Sep: ch})
		}
	}
	return MaskPattern{Kind: MaskPatternKindPattern, Pattern: pattern, Tokens: tokens}
}

// MaskPatternNumber is MaskPattern::number: number grouping with separator
// sep (0 for None) and no fraction limit (U:input/base/mask_pattern.rs:142-148).
func MaskPatternNumber(sep rune) MaskPattern {
	return MaskPattern{Kind: MaskPatternKindNumber, Separator: sep}
}

// tokens is U:input/base/mask_pattern.rs:133-140.
func (m MaskPattern) tokens() ([]MaskToken, bool) {
	if m.Kind == MaskPatternKindPattern {
		return m.Tokens, true
	}
	return nil, false
}

// Placeholder is each token's placeholder for a token pattern; ok is false
// otherwise (U:input/base/mask_pattern.rs:150-158).
func (m MaskPattern) Placeholder() (string, bool) {
	if m.Kind != MaskPatternKindPattern {
		return "", false
	}
	var b strings.Builder
	for _, t := range m.Tokens {
		b.WriteRune(t.Placeholder())
	}
	return b.String(), true
}

// IsNone reports no mask, or a pattern without tokens
// (U:input/base/mask_pattern.rs:160-167).
func (m MaskPattern) IsNone() bool {
	switch m.Kind {
	case MaskPatternKindPattern:
		return len(m.Tokens) == 0
	case MaskPatternKindNumber:
		return false
	}
	return true
}

func isSign(ch rune) bool { return ch == '+' || ch == '-' } // U:input/base/mask_pattern.rs:414-417

// IsValid reports whether maskText is valid for the mask; always true for
// none (U:input/base/mask_pattern.rs:169-244).
//
// For a token pattern upstream compares the matched char count with the
// byte length of maskText (:191); the port keeps that.
func (m MaskPattern) IsValid(maskText string) bool {
	if m.IsNone() {
		return true
	}
	switch m.Kind {
	case MaskPatternKindPattern:
		chars := []rune(maskText)
		textIndex := 0
		for _, token := range m.Tokens {
			if textIndex >= len(chars) {
				break
			}
			if token.isMatch(chars[textIndex]) {
				textIndex++
			}
		}
		return textIndex == len(maskText)
	case MaskPatternKindNumber:
		if maskText == "" {
			return true
		}
		parts := strings.Split(maskText, ".")
		if len(parts) > 2 {
			return false // only one dot is valid
		}
		intPart := parts[0]
		isSep := func(ch rune) bool { return m.Separator != 0 && ch == m.Separator }
		// Only one sign, and only at the start (char index 0).
		signs, i := 0, 0
		for _, ch := range intPart {
			if isSign(ch) {
				signs++
				if i > 0 {
					return false
				}
			}
			i++
		}
		if signs > 1 {
			return false
		}
		i = 0
		for _, ch := range intPart {
			if !(isASCIIDigit(ch) || isSign(ch) && i == 0 || isSep(ch)) {
				return false
			}
			i++
		}
		if len(parts) == 2 {
			for _, ch := range parts[1] {
				if !(isASCIIDigit(ch) || isSep(ch)) {
					return false
				}
			}
		}
		return true
	}
	return true
}

// IsValidAt reports whether ch may be typed at token position pos; a
// separator position also accepts a match for the following token
// (U:input/base/mask_pattern.rs:246-274).
func (m MaskPattern) IsValidAt(ch rune, pos int) bool {
	if m.IsNone() || m.Kind != MaskPatternKindPattern {
		return true
	}
	if pos < 0 || pos >= len(m.Tokens) {
		return false
	}
	token := m.Tokens[pos]
	if token.isMatch(ch) {
		return true
	}
	if token.isSep() && pos+1 < len(m.Tokens) && m.Tokens[pos+1].isMatch(ch) {
		return true
	}
	return false
}

// Mask formats text with the mask; pattern `(999)999-999` turns 123456789
// into (123)456-789 (U:input/base/mask_pattern.rs:276-371).
func (m MaskPattern) Mask(text string) string {
	if m.IsNone() {
		return text
	}
	switch m.Kind {
	case MaskPatternKindNumber:
		if m.Separator == 0 {
			return text
		}
		sep := m.Separator
		text = strings.ReplaceAll(text, string(sep), "")
		parts := strings.Split(text, ".")
		intPart := parts[0]
		// The fraction is limited to Fraction chars, not padded.
		var fracPart string
		hasFrac := len(parts) > 1
		if hasFrac {
			frac := []rune(parts[1])
			if m.HasFraction && m.Fraction < len(frac) {
				frac = frac[:m.Fraction]
			}
			fracPart = string(frac)
		}
		// Reverse the integer part for grouping, removing the first sign
		// so it is not grouped (avoids -,123).
		chars := []rune(intPart)
		for i, j := 0, len(chars)-1; i < j; i, j = i+1, j-1 {
			chars[i], chars[j] = chars[j], chars[i]
		}
		var sign rune
		for i, ch := range chars {
			if isSign(ch) {
				sign = ch
				chars = append(chars[:i], chars[i+1:]...)
				break
			}
		}
		result := make([]rune, 0, len(chars)+len(chars)/3)
		for i, ch := range chars {
			if i > 0 && i%3 == 0 {
				result = append(result, sep)
			}
			result = append(result, ch)
		}
		for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
			result[i], result[j] = result[j], result[i]
		}
		final := string(result)
		if hasFrac && !(m.HasFraction && m.Fraction == 0) {
			final = final + "." + fracPart
		}
		if sign != 0 {
			final = string(sign) + final
		}
		return final
	case MaskPatternKindPattern:
		var b strings.Builder
		chars := []rune(text)
		textIndex := 0
		for pos, token := range m.Tokens {
			if textIndex >= len(chars) {
				break
			}
			ch := chars[textIndex]
			if !token.isSep() && !m.IsValidAt(ch, pos) {
				break
			}
			maskCh := token.maskChar(ch)
			b.WriteRune(maskCh)
			if ch == maskCh {
				textIndex++
			}
		}
		return b.String()
	}
	return text
}

// Unmask extracts the original text from masked text
// (U:input/base/mask_pattern.rs:373-411).
func (m MaskPattern) Unmask(maskText string) string {
	switch m.Kind {
	case MaskPatternKindNumber:
		if m.Separator == 0 {
			return maskText
		}
		result := strings.ReplaceAll(maskText, string(m.Separator), "")
		if strings.Contains(result, ".") {
			result = strings.TrimRight(result, "0")
		}
		return result
	case MaskPatternKindPattern:
		var b strings.Builder
		chars := []rune(maskText)
		for textIndex, token := range m.Tokens {
			if textIndex >= len(chars) {
				break
			}
			if ch, ok := token.unmaskChar(chars[textIndex]); ok {
				b.WriteRune(ch)
			}
		}
		return b.String()
	}
	return maskText
}

// normalizeNumberChar is normalize_number_input's normalize_char
// (U:input/base/mask_pattern.rs:429-444).
func normalizeNumberChar(ch rune) (rune, bool) {
	switch {
	case ch >= '０' && ch <= '９': // full-width digits
		return ch - 0xFF10 + '0', true
	case ch == '＋': // full-width plus
		return '+', true
	case ch == '－' || ch == '−': // full-width hyphen, minus sign
		return '-', true
	case ch == '．' || ch == '。': // full-width dot, ideographic full stop
		return '.', true
	case ch == '，': // full-width comma
		return ',', true
	}
	return 0, false
}

// normalizeNumberInput maps full-width and CJK number characters to ASCII,
// e.g. `１２３。５` to `123.5`; text without any is returned unchanged
// without allocating (Cow::Borrowed). Each mapping keeps the UTF-16 length
// but may shrink the UTF-8 length (U:input/base/mask_pattern.rs:419-455).
func normalizeNumberInput(text string) string {
	if !strings.ContainsFunc(text, func(ch rune) bool { _, ok := normalizeNumberChar(ch); return ok }) {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	for _, ch := range text {
		if n, ok := normalizeNumberChar(ch); ok {
			ch = n
		}
		b.WriteRune(ch)
	}
	return b.String()
}
