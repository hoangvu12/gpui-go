package strftime

import (
	"errors"
	"testing"
	"time"
)

// chrono-0.4.45/src/format/strftime.rs:905-1015 test_strftime_docs, on the
// same wall clock (2001-07-08 00:34:59.026490708). chrono's instant is a leap
// second (nanos 1_026_490_708) so it prints :60; Go time has no leap second,
// so this port expects :59 for %S, %T, %X, %r, %c. Zone specifiers (%z %:z
// %::z %:::z %+) need an offset and are N.A. for Base's naive values; %s is
// the wall clock read as UTC (chrono's 994518299 + the +09:30 offset).
var docInstant = time.Date(2001, 7, 8, 0, 34, 59, 26_490_708, time.UTC)

var docCases = map[string]string{
	// date specifiers
	"%Y": "2001", "%C": "20", "%y": "01", "%q": "3", "%m": "07", "%b": "Jul", "%B": "July",
	"%h": "Jul", "%d": "08", "%e": " 8", "%_d": " 8", "%a": "Sun", "%A": "Sunday", "%w": "0",
	"%u": "7", "%U": "27", "%W": "27", "%G": "2001", "%g": "01", "%V": "27", "%j": "189",
	"%D": "07/08/01", "%x": "07/08/01", "%F": "2001-07-08", "%v": " 8-Jul-2001",
	// time specifiers
	"%H": "00", "%k": " 0", "%_H": " 0", "%I": "12", "%l": "12", "%_I": "12", "%P": "am", "%p": "AM",
	"%M": "34", "%S": "59", "%f": "026490708", "%.f": ".026490708", "%.3f": ".026", "%.6f": ".026490",
	"%.9f": ".026490708", "%3f": "026", "%6f": "026490", "%9f": "026490708",
	"%R": "00:34", "%T": "00:34:59", "%X": "00:34:59", "%r": "12:34:59 AM",
	// date & time specifiers
	"%c": "Sun Jul  8 00:34:59 2001", "%s": "994552499",
	// special and complex specifiers
	"%t": "\t", "%n": "\n", "%%": "%",
	"  %Y%d%m%%%%%t%H%M%S\t": "  20010807%%\t003459\t",
	// padding modifiers (strftime.rs:474-479)
	"%-d": "8", "%_m": " 7", "%0e": "08", "%-H": "0", "%-j": "189", "%_j": "189", "%0k": "00",
}

func TestChronoStrftimeDocs(t *testing.T) {
	for layout, want := range docCases {
		got, err := Format(docInstant, layout)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v want %q", layout, got, err, want)
		}
	}
	// strftime.rs:958: %.f picks the shortest of 3/6/9 digits.
	if got, _ := Format(docInstant.Add(-708), "%.f"); got != ".026490" {
		t.Errorf("%%.f with micros: %q", got)
	}
	if got, _ := Format(docInstant.Truncate(time.Second), "%.f"); got != "" {
		t.Errorf("%%.f with no fraction: %q", got)
	}
}

// Base's own layouts: time_field.rs:44-52 and the NaiveDate Display layout
// behind calendar.rs labels (e.g. "2026-09-07", calendar.rs:903).
func TestBaseLayouts(t *testing.T) {
	for _, c := range []struct {
		at     time.Time
		layout string
		want   string
	}{
		{time.Date(2025, 1, 1, 9, 5, 7, 0, time.UTC), "%H:%M", "09:05"},
		{time.Date(2025, 1, 1, 9, 5, 7, 0, time.UTC), "%H:%M:%S", "09:05:07"},
		{time.Date(2025, 1, 1, 21, 5, 7, 0, time.UTC), "%I:%M %p", "09:05 PM"},
		{time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC), "%I:%M:%S %p", "12:00:00 PM"},
		{time.Date(2025, 1, 1, 0, 30, 0, 0, time.UTC), "%I:%M %p", "12:30 AM"},
		{time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC), "%Y-%m-%d", "2026-09-07"},
	} {
		if got, err := Format(c.at, c.layout); err != nil || got != c.want {
			t.Errorf("%q: got %q, %v want %q", c.layout, got, err, c.want)
		}
	}
}

func TestErrorsAndLenient(t *testing.T) {
	for _, c := range []struct{ layout, lenient string }{
		{"%Q", "%Q"},
		{"a%", "a%"},                     // premature end
		{"%-b", "%-b"},                   // padding on a non-numeric item
		{"%-D", "%-D"},                   // padding on a composite item
		{"%#x", "%#x"},                   // alternate form only exists for z
		{"%.4f", "%.4f"},                 // only 3/6/9
		{"%:x", "%:x"},                   // chrono consumes "%:" and keeps "x" literal
		{"%Z|%z|%:z|%+", "%Z|%z|%:z|%+"}, // need an offset
		{"%Y %Q %m", "2001 %Q 07"},
	} {
		if _, err := Format(docInstant, c.layout); !errors.Is(err, ErrBadFormat) {
			t.Errorf("%q: expected ErrBadFormat, got %v", c.layout, err)
		}
		if got := FormatLenient(docInstant, c.layout); got != c.lenient {
			t.Errorf("lenient %q: got %q want %q", c.layout, got, c.lenient)
		}
	}
}

// formatting.rs:149-180: years outside 1000..=9999 are padded to 4 digits and
// signed outside 0..=9999.
func TestYearPadding(t *testing.T) {
	for _, c := range []struct {
		y      int
		layout string
		want   string
	}{
		{42, "%Y", "0042"}, {42, "%-Y", "42"}, {42, "%_Y", "  42"}, {2001, "%-Y", "2001"},
		{12345, "%Y", "+12345"}, {-5, "%Y", "-0005"}, {-5, "%-Y", "-5"}, {-5, "%y", "95"},
	} {
		got, err := Format(time.Date(c.y, 3, 4, 0, 0, 0, 0, time.UTC), c.layout)
		if err != nil || got != c.want {
			t.Errorf("year %d %q: got %q want %q", c.y, c.layout, got, c.want)
		}
	}
}

// Week numbers and ordinals against a brute-force day count, over a span of
// years that covers every weekday for Jan 1 and leap years.
func TestWeekNumbersAgainstModel(t *testing.T) {
	for d := time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC); d.Year() < 2031; d = d.AddDate(0, 0, 1) {
		sundays, mondays := 0, 0
		for x := time.Date(d.Year(), 1, 1, 0, 0, 0, 0, time.UTC); !x.After(d); x = x.AddDate(0, 0, 1) {
			switch x.Weekday() {
			case time.Sunday:
				sundays++
			case time.Monday:
				mondays++
			}
		}
		isoY, isoW := d.ISOWeek()
		want := pad2(sundays) + " " + pad2(mondays) + " " + itoa(isoY) + " " + pad2(isoW) + " " + pad3(d.YearDay())
		got, _ := Format(d, "%U %W %G %V %j")
		if got != want {
			t.Fatalf("%s: got %q want %q", d.Format("2006-01-02"), got, want)
		}
	}
}

func itoa(v int) string { s, _ := Format(time.Date(v, 1, 1, 0, 0, 0, 0, time.UTC), "%Y"); return s }
func pad2(v int) string { return string([]byte{byte('0' + v/10), byte('0' + v%10)}) }
func pad3(v int) string {
	return string([]byte{byte('0' + v/100), byte('0' + v/10%10), byte('0' + v%10)})
}
