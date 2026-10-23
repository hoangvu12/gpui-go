// Package strftime formats dates and times with chrono 0.4's strftime syntax
// (chrono::format::strftime), the format strings GPUI Base exposes in
// calendar.rs:61-68 (`Date::format`, a user-supplied layout over NaiveDate)
// and time_field.rs:44-52 (`%H:%M`, `%H:%M:%S`, `%I:%M %p`, `%I:%M:%S %p`).
// Behaviour follows chrono-0.4.45/src/format/strftime.rs (parser) and
// chrono-0.4.45/src/format/formatting.rs (rendering); comments cite them.
//
// The time.Time is read as a naive (zone-less) value, like Base's NaiveDate
// and NaiveTime: its wall-clock fields in its own Location are formatted and
// its offset is ignored. Hence, as with chrono's naive types, the time-zone
// specifiers %Z %z %:z %::z %:::z %#z and %+ are format errors, and %s is the
// wall clock read as UTC (formatting.rs:211-215 with no offset).
//
// Deviations: a time.Time always has both a date and a time, so formatting a
// date-only value (callers pass midnight) prints 00 for %H where chrono's
// NaiveDate fails; Go time has no leap second, so %S never prints 60.
// Localized formatting (unstable-locales) is not ported.
package strftime

import (
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ErrBadFormat is chrono's BAD_FORMAT: the layout has a malformed or
// unsupported specifier.
var ErrBadFormat = errors.New("bad or unsupported format string")

// Format renders t with layout like `NaiveDateTime::format(layout).to_string()`
// (chrono's StrftimeItems::new). Where chrono's Display would fail (and
// `to_string` panic), Format returns ErrBadFormat.
func Format(t time.Time, layout string) (string, error) {
	return format(t, layout, false)
}

// FormatLenient renders t with layout using chrono's lenient parser
// (StrftimeItems::new_lenient, strftime.rs:263-273): a malformed specifier is
// written out verbatim instead of failing. Zone specifiers are written out
// verbatim too, where chrono parses them and then fails to render them for a
// naive value.
func FormatLenient(t time.Time, layout string) string {
	s, _ := format(t, layout, true)
	return s
}

type pad uint8

const (
	padNone pad = iota
	padZero
	padSpace
)

var (
	dFmt     = "%m/%d/%y"             // D_FMT, strftime.rs:695-701
	tFmt     = "%H:%M:%S"             // T_FMT, strftime.rs:717-723
	dtFmt    = "%a %b %e %H:%M:%S %Y" // D_T_FMT, strftime.rs:702-716
	tAmPmFmt = "%I:%M:%S %p"          // T_FMT_AMPM, strftime.rs:724-732
)

var errSpec = errors.New("spec")

func format(t time.Time, layout string, lenient bool) (string, error) {
	var b strings.Builder
	rest := layout
	for rest != "" {
		if rest[0] != '%' {
			// Literal and Space items render as themselves
			// (strftime.rs:438-458).
			end := strings.IndexByte(rest, '%')
			if end < 0 {
				end = len(rest)
			}
			b.WriteString(rest[:end])
			rest = rest[end:]
			continue
		}
		consumed, err := spec(&b, t, rest)
		if err != nil {
			if !lenient {
				return "", ErrBadFormat
			}
			b.WriteString(rest[:consumed])
		}
		rest = rest[consumed:]
	}
	return b.String(), nil
}

// spec renders the specifier at the start of s (which begins with '%') and
// returns how many bytes it consumed (parse_next_item, strftime.rs:429-643).
// On error nothing is written and consumed covers the malformed text.
func spec(b *strings.Builder, t time.Time, s string) (int, error) {
	i := 1
	next := func() (rune, bool) {
		if i >= len(s) {
			return 0, false
		}
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		return r, true
	}
	c, ok := next()
	if !ok {
		return i, errSpec
	}
	var override *pad
	switch c {
	case '-', '0', '_':
		p := map[rune]pad{'-': padNone, '0': padZero, '_': padSpace}[c]
		override = &p
	}
	alternate := c == '#'
	if override != nil || alternate {
		if c, ok = next(); !ok {
			return i, errSpec
		}
	}
	if alternate && c != 'z' { // HAVE_ALTERNATES, strftime.rs:734
		return i, errSpec
	}
	num := func(n numeric, def pad) (int, error) {
		p := def
		if override != nil {
			p = *override
		}
		b.WriteString(n.render(t, p))
		return i, nil
	}
	// Composite and non-numeric items reject a padding override
	// (strftime.rs:630-642).
	text := func(sub string) (int, error) {
		if override != nil {
			return i, errSpec
		}
		b.WriteString(sub)
		return i, nil
	}
	composite := func(layout string) (int, error) {
		if override != nil {
			return i, errSpec
		}
		out, _ := format(t, layout, false)
		b.WriteString(out)
		return i, nil
	}
	nanos := t.Nanosecond()
	switch c {
	case 'A':
		return text(t.Weekday().String())
	case 'B':
		return text(t.Month().String())
	case 'C':
		return num(yearDiv100, padZero)
	case 'D':
		return composite(dFmt)
	case 'F':
		return composite("%Y-%m-%d")
	case 'G':
		return num(isoYear, padZero)
	case 'H':
		return num(hour, padZero)
	case 'I':
		return num(hour12N, padZero)
	case 'M':
		return num(minute, padZero)
	case 'P':
		return text(map[bool]string{false: "am", true: "pm"}[t.Hour() >= 12])
	case 'R':
		return composite("%H:%M")
	case 'S':
		return num(second, padZero)
	case 'T', 'X':
		return composite(tFmt)
	case 'U':
		return num(weekFromSun, padZero)
	case 'V':
		return num(isoWeek, padZero)
	case 'W':
		return num(weekFromMon, padZero)
	case 'Y':
		return num(year, padZero)
	case 'a':
		return text(t.Weekday().String()[:3])
	case 'b', 'h':
		return text(t.Month().String()[:3])
	case 'c':
		return composite(dtFmt)
	case 'd':
		return num(day, padZero)
	case 'e':
		return num(day, padSpace)
	case 'f':
		return num(nanosecond, padZero)
	case 'g':
		return num(isoYearMod100, padZero)
	case 'j':
		return num(ordinal, padZero)
	case 'k':
		return num(hour, padSpace)
	case 'l':
		return num(hour12N, padSpace)
	case 'm':
		return num(month, padZero)
	case 'n':
		return text("\n")
	case 'p':
		return text(map[bool]string{false: "AM", true: "PM"}[t.Hour() >= 12])
	case 'q':
		return num(quarter, padNone)
	case 'r':
		return composite(tAmPmFmt)
	case 's':
		return num(timestamp, padNone)
	case 't':
		return text("\t")
	case 'u':
		return num(weekdayFromMon, padNone)
	case 'v':
		return composite("%e-%b-%Y")
	case 'w':
		return num(numDaysFromSun, padNone)
	case 'x':
		return composite(dFmt)
	case 'y':
		return num(yearMod100, padZero)
	case '%':
		return text("%")
	case '.':
		d, ok := next()
		if !ok {
			return i, errSpec
		}
		if d == 'f' { // Fixed::Nanosecond, formatting.rs:250-264
			switch {
			case nanos == 0:
				return text("")
			case nanos%1_000_000 == 0:
				return text("." + zeroPad(nanos/1_000_000, 3))
			case nanos%1_000 == 0:
				return text("." + zeroPad(nanos/1_000, 6))
			}
			return text("." + zeroPad(nanos, 9))
		}
		if w, ok := fracWidth(d, next); ok {
			return text("." + frac(nanos, w))
		}
		return i, errSpec
	case '3', '6', '9':
		if w, ok := fracWidth(c, next); ok {
			return text(frac(nanos, w))
		}
		return i, errSpec
	}
	// Z, z, :z, ::z, :::z, #z and + need an offset, which naive values lack;
	// everything else is unknown.
	if c == ':' {
		for _, suffix := range []string{"::z", ":z", "z"} {
			if strings.HasPrefix(s[i:], suffix) {
				i += len(suffix)
				break
			}
		}
	}
	return i, errSpec
}

// fracWidth reads the 'f' after a 3/6/9 digit (strftime.rs:598-625).
func fracWidth(d rune, next func() (rune, bool)) (int, bool) {
	if d != '3' && d != '6' && d != '9' {
		return 0, false
	}
	if f, ok := next(); !ok || f != 'f' {
		return 0, false
	}
	return int(d - '0'), true
}

// frac is Nanosecond3/6/9 (formatting.rs:265-285).
func frac(nanos, width int) string {
	switch width {
	case 3:
		return zeroPad(nanos/1_000_000%1000, 3)
	case 6:
		return zeroPad(nanos/1_000%1_000_000, 6)
	}
	return zeroPad(nanos%1_000_000_000, 9)
}

func zeroPad(v, n int) string {
	s := strconv.Itoa(v)
	for len(s) < n {
		s = "0" + s
	}
	return s
}

type numeric uint8

const (
	year numeric = iota
	yearDiv100
	yearMod100
	isoYear
	isoYearMod100
	quarter
	month
	day
	weekFromSun
	weekFromMon
	isoWeek
	numDaysFromSun
	weekdayFromMon
	ordinal
	hour
	hour12N
	minute
	second
	nanosecond
	timestamp
)

// render is Formatter::format_numeric (formatting.rs:127-219).
func (n numeric) render(t time.Time, p pad) string {
	wd := int(t.Weekday()) // num_days_from_sunday
	isoY, isoW := t.ISOWeek()
	switch n {
	case year:
		return writeYear(t.Year(), p)
	case yearDiv100:
		return writeTwo(floorDiv(t.Year(), 100), p)
	case yearMod100:
		return writeTwo(t.Year()-100*floorDiv(t.Year(), 100), p)
	case isoYear:
		return writeYear(isoY, p)
	case isoYearMod100:
		return writeTwo(isoY-100*floorDiv(isoY, 100), p)
	case quarter:
		return strconv.Itoa((int(t.Month()) + 2) / 3)
	case month:
		return writeTwo(int(t.Month()), p)
	case day:
		return writeTwo(t.Day(), p)
	case weekFromSun: // weeks_from, chrono naive/date/mod.rs:124-126
		return writeTwo((t.YearDay()-wd+6)/7, p)
	case weekFromMon:
		return writeTwo((t.YearDay()-(wd+6)%7+6)/7, p)
	case isoWeek:
		return writeTwo(isoW, p)
	case numDaysFromSun:
		return strconv.Itoa(wd)
	case weekdayFromMon:
		return strconv.Itoa((wd+6)%7 + 1)
	case ordinal:
		return writeN(3, int64(t.YearDay()), p, false)
	case hour:
		return writeTwo(t.Hour(), p)
	case hour12N:
		h := t.Hour() % 12
		if h == 0 {
			h = 12
		}
		return writeTwo(h, p)
	case minute:
		return writeTwo(t.Minute(), p)
	case second:
		return writeTwo(t.Second(), p)
	case nanosecond:
		return writeN(9, int64(t.Nanosecond()), p, false)
	}
	// timestamp: the wall clock read as UTC (no offset).
	y, mo, d := t.Date()
	ts := time.Date(y, mo, d, t.Hour(), t.Minute(), t.Second(), 0, time.UTC).Unix()
	return writeN(9, ts, p, false)
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b != 0 && a < 0 {
		q--
	}
	return q
}

// writeTwo is formatting.rs:139-147 (values are assumed below 100).
func writeTwo(v int, p pad) string {
	ones := byte('0' + v%10)
	tens := v / 10
	switch {
	case tens == 0 && p == padNone:
		return string(ones)
	case tens == 0 && p == padSpace:
		return " " + string(ones)
	}
	return string([]byte{byte('0' + tens), ones})
}

// writeYear is formatting.rs:149-158.
func writeYear(y int, p pad) string {
	if y >= 1000 && y <= 9999 {
		return strconv.Itoa(y)
	}
	return writeN(4, int64(y), p, y < 0 || y >= 10_000)
}

// writeN is formatting.rs:160-180 (Rust's `{:+0n$}` / `{:n$}` widths).
func writeN(n int, v int64, p pad, alwaysSign bool) string {
	digits := strconv.FormatInt(v, 10)
	sign := ""
	if v < 0 {
		sign, digits = "-", digits[1:]
	} else if alwaysSign {
		sign = "+"
	}
	width := n
	if alwaysSign {
		width = n + 1
	}
	switch p {
	case padZero:
		for len(sign)+len(digits) < width {
			digits = "0" + digits
		}
		return sign + digits
	case padSpace:
		s := sign + digits
		for len(s) < width {
			s = " " + s
		}
		return s
	}
	return sign + digits
}
