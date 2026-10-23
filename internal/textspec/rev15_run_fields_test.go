//go:build windows

// The browser-core ticket 39 (rev 15) text-run-field gates: the shape
// request's browser text fields (word-break, overflow-wrap, text-wrap,
// locale, font width, variation settings, optical size, justify) cross
// the ABI, validate, and round-trip through the shaping handle's
// `run_info` echo. The shaping behavior for them is unchanged in rev 15
// (the pinned TextLayoutRequest has no slots — see TEXT_ABI.md), so
// these gates pin the interface side the later Go styling tickets
// consume.
package textspec

import (
	"errors"
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// textService opens the native text service.
func textService(t *testing.T) *native.TextService {
	t.Helper()
	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Fatalf("native.Load: %v", err)
	}
	svc, err := lib.Text()
	if err != nil {
		t.Fatalf("lib.Text: %v", err)
	}
	return svc
}

// browserRun returns one run spec carrying every rev-15 field.
func browserRun() native.TextRunSpec {
	width := float32(1.25)
	opsz := float32(14)
	return native.TextRunSpec{
		// Weight 400 keeps this test's interning identical to the
		// recorded trace's first face (Segoe UI regular); the package's
		// other tests pin that FontId.
		Len:          len("Browser text"),
		Family:       ".SystemUIFont",
		Weight:       400,
		WordBreak:    native.TextWordBreakBreakAll,
		OverflowWrap: native.TextOverflowWrapAnywhere,
		TextWrap:     native.TextTextWrapBalance,
		Locale:       "ja-JP",
		FontWidth:    &width,
		Variations: []native.FontVariation{
			{Tag: "wght", Value: 650},
			{Tag: "wdth", Value: 75},
		},
		OpticalSize: &opsz,
		Justify:     true,
	}
}

// TestBrowserRunFieldsRoundTripThroughRunInfo: shape with every field
// set, then read each run back through run_info and compare field for
// field.
func TestBrowserRunFieldsRoundTripThroughRunInfo(t *testing.T) {
	svc := textService(t)
	handle, err := svc.Shape(native.ShapeRequest{
		Text:     "Browser text",
		FontSize: 14,
		Runs:     []native.TextRunSpec{browserRun()},
	})
	if err != nil {
		t.Fatalf("Shape: %v", err)
	}
	t.Cleanup(func() { _ = svc.Dispose(handle) })
	record, err := svc.RunInfo(handle, 0)
	if err != nil {
		t.Fatalf("RunInfo: %v", err)
	}
	if record.WordBreak != native.TextWordBreakBreakAll {
		t.Errorf("word_break = %d, want break-all", record.WordBreak)
	}
	if record.OverflowWrap != native.TextOverflowWrapAnywhere {
		t.Errorf("overflow_wrap = %d, want anywhere", record.OverflowWrap)
	}
	if record.TextWrap != native.TextTextWrapBalance {
		t.Errorf("text_wrap = %d, want balance", record.TextWrap)
	}
	if got := math.Float32frombits(record.FontWidthBits); got != 1.25 {
		t.Errorf("font_width = %v, want 1.25", got)
	}
	if record.FontWidthPresent != 1 {
		t.Errorf("font_width_present = %d, want 1", record.FontWidthPresent)
	}
	if got := math.Float32frombits(record.OpSZBits); got != 14 {
		t.Errorf("opsz = %v, want 14", got)
	}
	if record.VariationCount != 2 {
		t.Fatalf("variation_count = %d, want 2", record.VariationCount)
	}
	if record.Justify != 1 {
		t.Errorf("justify = %d, want 1", record.Justify)
	}

	// The locale rides the strings buffer; the record addresses it.
	if record.LocaleLen != uint32(len("ja-JP")) {
		t.Fatalf("locale_len = %d, want %d", record.LocaleLen, len("ja-JP"))
	}

	// Out-of-range indices are typed errors; a disposed handle is
	// stale.
	if _, err := svc.RunInfo(handle, 1); !errors.Is(err, native.ErrTextBadValue) {
		t.Errorf("RunInfo(handle, 1) = %v, want ErrTextBadValue", err)
	}
	if _, err := svc.RunInfo(handle, -1); err == nil {
		t.Error("RunInfo(handle, -1) accepted a negative index")
	}
}

// TestBrowserRunFieldsDefaultToCSS: an empty spec (all fields zero)
// is valid and reads back as the CSS defaults.
func TestBrowserRunFieldsDefaultToCSS(t *testing.T) {
	svc := textService(t)
	handle, err := svc.Shape(native.ShapeRequest{
		Text:     "Plain",
		FontSize: 14,
		Runs: []native.TextRunSpec{{
			Len:    len("Plain"),
			Family: ".SystemUIFont",
			Weight: 400,
		}},
	})
	if err != nil {
		t.Fatalf("Shape: %v", err)
	}
	t.Cleanup(func() { _ = svc.Dispose(handle) })
	record, err := svc.RunInfo(handle, 0)
	if err != nil {
		t.Fatalf("RunInfo: %v", err)
	}
	if record.WordBreak != 0 || record.OverflowWrap != 0 || record.TextWrap != 0 ||
		record.LocaleLen != 0 || record.FontWidthPresent != 0 ||
		record.VariationCount != 0 || record.OpSZPresent != 0 || record.Justify != 0 {
		t.Fatalf("the default run echoed non-default fields: %+v", record)
	}
}

// TestBrowserRunFieldsRejectMalformedValues: unknown tags, non-finite
// scalars and malformed variation tags are typed caller errors.
func TestBrowserRunFieldsRejectMalformedValues(t *testing.T) {
	svc := textService(t)
	width := float32(-1)
	opsz := float32(math.NaN())
	shape := func(mutate func(*native.TextRunSpec)) error {
		run := browserRun()
		mutate(&run)
		_, err := svc.Shape(native.ShapeRequest{
			Text:     "Browser text",
			FontSize: 14,
			Runs:     []native.TextRunSpec{run},
		})
		return err
	}
	cases := []struct {
		name   string
		mutate func(*native.TextRunSpec)
	}{
		{"unknown word-break", func(r *native.TextRunSpec) { r.WordBreak = 3 }},
		{"unknown overflow-wrap", func(r *native.TextRunSpec) { r.OverflowWrap = 3 }},
		{"unknown text-wrap", func(r *native.TextRunSpec) { r.TextWrap = 5 }},
		{"negative font width", func(r *native.TextRunSpec) { r.FontWidth = &width }},
		{"NaN optical size", func(r *native.TextRunSpec) { r.OpticalSize = &opsz }},
		{"short variation tag", func(r *native.TextRunSpec) {
			r.Variations = []native.FontVariation{{Tag: "wg", Value: 1}}
		}},
		{"non-ASCII variation tag", func(r *native.TextRunSpec) {
			r.Variations = []native.FontVariation{{Tag: "w\x7fht", Value: 1}}
		}},
		{"non-finite variation value", func(r *native.TextRunSpec) {
			r.Variations = []native.FontVariation{{Tag: "wght", Value: float32(math.Inf(1))}}
		}},
	}
	for _, tc := range cases {
		err := shape(tc.mutate)
		if err == nil {
			t.Errorf("%s: Shape accepted a malformed run", tc.name)
		}
	}
}
