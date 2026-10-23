package base

import (
	"reflect"
	"testing"
)

// The 9 tests of U:input/base/mask_pattern.rs:457-725.

func inpBool(t *testing.T, what string, got, want bool) {
	t.Helper()
	if got != want {
		t.Fatalf("%s = %v want %v", what, got, want)
	}
}

func inpMasks(t *testing.T, m MaskPattern, cases ...string) {
	t.Helper()
	for i := 0; i < len(cases); i += 2 {
		inpEqStr(t, m.Mask(cases[i]), cases[i+1])
	}
}

func inpUnmasks(t *testing.T, m MaskPattern, cases ...string) {
	t.Helper()
	for i := 0; i < len(cases); i += 2 {
		inpEqStr(t, m.Unmask(cases[i]), cases[i+1])
	}
}

func inpValid(t *testing.T, m MaskPattern, want bool, texts ...string) {
	t.Helper()
	for _, s := range texts {
		inpBool(t, "IsValid("+s+")", m.IsValid(s), want)
	}
}

var (
	inpSep = func(c rune) MaskToken { return MaskToken{Kind: MaskTokenKindSep, Sep: c} }
	inpD   = MaskToken{Kind: MaskTokenKindDigit}
	inpL   = MaskToken{Kind: MaskTokenKindLetter}
	inpLD  = MaskToken{Kind: MaskTokenKindLetterOrDigit}
	inpAny = MaskToken{Kind: MaskTokenKindAny}
)

// U:input/base/mask_pattern.rs:461-487 test_is_match.
func TestMaskTokenIsMatch(t *testing.T) {
	for _, c := range []struct {
		tok  MaskToken
		ch   rune
		want bool
	}{
		{inpSep('('), '(', true}, {inpSep('-'), '(', false}, {inpSep('-'), '3', false},
		{inpD, '0', true}, {inpD, '9', true}, {inpD, 'a', false}, {inpD, 'C', false},
		{inpL, 'a', true}, {inpL, 'Z', true}, {inpL, '3', false}, {inpL, '-', false},
		{inpLD, '0', true}, {inpLD, '9', true}, {inpLD, 'a', true}, {inpLD, 'Z', true}, {inpLD, '3', true},
		{inpAny, 'a', true}, {inpAny, '3', true}, {inpAny, '-', true}, {inpAny, ' ', true},
	} {
		inpBool(t, string(c.ch), c.tok.isMatch(c.ch), c.want)
	}
}

// U:input/base/mask_pattern.rs:489-496 test_mask_none.
func TestMaskPatternNone(t *testing.T) {
	var m MaskPattern
	inpBool(t, "IsNone", m.IsNone(), true)
	inpValid(t, m, true, "1124124ASLDJKljk")
	inpMasks(t, m, "hello-world", "hello-world")
	inpUnmasks(t, m, "hello-world", "hello-world")
}

// U:input/base/mask_pattern.rs:498-547 test_mask_pattern1.
func TestMaskPatternPattern1(t *testing.T) {
	m := NewMaskPattern("(AA)999-999")
	tokens, ok := m.tokens()
	if want := []MaskToken{inpSep('('), inpL, inpL, inpSep(')'), inpD, inpD, inpD, inpSep('-'), inpD, inpD, inpD}; !ok || !reflect.DeepEqual(tokens, want) {
		t.Fatalf("tokens %+v", tokens)
	}
	for _, c := range []struct {
		ch   rune
		pos  int
		want bool
	}{{'(', 0, true}, {'H', 0, true}, {'3', 0, false}, {'-', 0, false}, {')', 1, false}, {'H', 1, true},
		{'1', 1, false}, {'e', 2, true}, {')', 3, true}, {'1', 3, true}, {'2', 4, true}} {
		inpBool(t, "IsValidAt", m.IsValidAt(c.ch, c.pos), c.want)
	}
	inpValid(t, m, true, "(AB)123-456")
	inpMasks(t, m,
		"AB123456", "(AB)123-456",
		"(AB)123-456", "(AB)123-456",
		"(AB123456", "(AB)123-456",
		"AB123-456", "(AB)123-456",
		"AB123-", "(AB)123-",
		"AB123--", "(AB)123-",
		"AB123-4", "(AB)123-4")
	inpUnmasks(t, m, "(AB)123-456", "AB123456")
	inpValid(t, m, false, "12AB345", "(11)123-456", "##")
	inpValid(t, m, true, "(AB)123456")
}

// U:input/base/mask_pattern.rs:549-578 test_mask_pattern2.
func TestMaskPatternPattern2(t *testing.T) {
	m := NewMaskPattern("999-999-******")
	tokens, _ := m.tokens()
	want := []MaskToken{inpD, inpD, inpD, inpSep('-'), inpD, inpD, inpD, inpSep('-'), inpAny, inpAny, inpAny, inpAny, inpAny, inpAny}
	if !reflect.DeepEqual(tokens, want) {
		t.Fatalf("tokens %+v", tokens)
	}
	masked := m.Mask("123456A(111)")
	inpEqStr(t, masked, "123-456-A(111)")
	inpEqStr(t, m.Unmask(masked), "123456A(111)")
	inpValid(t, m, true, masked)
}

// U:input/base/mask_pattern.rs:580-606 test_number_with_group_separator.
func TestMaskPatternNumberWithGroupSeparator(t *testing.T) {
	m := MaskPatternNumber(',')
	inpMasks(t, m, "1234567", "1,234,567", "1,234,567", "1,234,567", "1234567.89", "1,234,567.89")
	inpUnmasks(t, m, "1,234,567", "1234567", "1,234,567.89", "1234567.89")

	m = MaskPatternNumber(' ')
	inpMasks(t, m, "1234567", "1 234 567", "1234567.89", "1 234 567.89")
	inpUnmasks(t, m, "1 234 567", "1234567", "1 234 567.89", "1234567.89")

	m = MaskPatternNumber(0)
	inpMasks(t, m, "1234567", "1234567", "1234567.89", "1234567.89")
	inpUnmasks(t, m, "1234567", "1234567", "1234567.89", "1234567.89")
}

// U:input/base/mask_pattern.rs:608-636 test_number_with_fraction_digits.
func TestMaskPatternNumberWithFractionDigits(t *testing.T) {
	m := MaskPattern{Kind: MaskPatternKindNumber, Separator: ',', Fraction: 4, HasFraction: true}
	inpMasks(t, m,
		"1234567", "1,234,567",
		"1234567.", "1,234,567.",
		"1234567.89", "1,234,567.89",
		"1234567.891", "1,234,567.891",
		"1234567.891234", "1,234,567.8912")
	inpUnmasks(t, m, "1,234,567", "1234567", "1,234,567.890", "1234567.89")

	m = MaskPattern{Kind: MaskPatternKindNumber, Separator: ','}
	inpMasks(t, m, "1234567.1234567", "1,234,567.1234567")

	m = MaskPattern{Kind: MaskPatternKindNumber, Separator: ',', Fraction: 0, HasFraction: true}
	inpMasks(t, m, "1234567.1234567", "1,234,567")
}

// U:input/base/mask_pattern.rs:638-677 test_signed_number_numbers.
func TestMaskPatternSignedNumberNumbers(t *testing.T) {
	m := MaskPattern{Kind: MaskPatternKindNumber, Separator: ',', Fraction: 2, HasFraction: true}
	inpValid(t, m, true, "-", "-1234567", "-1,234,567", "-1234567.", "-1234567.89",
		"+", "+1234567", "+1,234,567", "+1234567.", "+1234567.89")
	inpValid(t, m, false, "+-", "-+", "+-1234567", "1,-234,567", "12-34567.89", "+1234567.-")
	inpMasks(t, m,
		"-123", "-123",
		"-1234567", "-1,234,567",
		"+1234567", "+1,234,567",
		"-1234567.", "-1,234,567.",
		"-1234567.89", "-1,234,567.89")
	inpUnmasks(t, m, "-1,234,567", "-1234567")
}

// U:input/base/mask_pattern.rs:679-699 test_number_leading_dot.
func TestMaskPatternNumberLeadingDot(t *testing.T) {
	m := MaskPatternNumber(0)
	inpValid(t, m, true, ".", ".5", "-.", "-.5")
	inpValid(t, m, false, "1.2.3", "1..")
	inpMasks(t, m, ".", ".", ".5", ".5", "-.5", "-.5", "+.5", "+.5")

	m = MaskPatternNumber(',')
	inpMasks(t, m, ".5", ".5", "-.5", "-.5")
}

// U:input/base/mask_pattern.rs:701-724 test_normalize_number_input. The
// Cow::Borrowed fast path is checked as zero allocations.
func TestNormalizeNumberInput(t *testing.T) {
	if n := testing.AllocsPerRun(10, func() { _ = normalizeNumberInput("-1,234.5") }); n != 0 {
		t.Fatalf("fast path allocated %v times", n)
	}
	for _, c := range [][2]string{
		{"0123456789", "0123456789"}, // upstream :714 (ASCII as written)
		{"０１２３４５６７８９", "0123456789"}, // full-width digits, the intent of :713
		{"＋1．5", "+1.5"},
		{"－1，234", "-1,234"},
		{"−1.5", "-1.5"},
		{"12。5", "12.5"},
		{"ab 中 1", "ab 中 1"},
	} {
		inpEqStr(t, normalizeNumberInput(c[0]), c[1])
	}
}
