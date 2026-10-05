package gpui

// Pure unit tests for the text adapter's Go-side derivations: the
// golden-ratio default line height, the pinned FontMetrics pixel
// scaling arithmetic, descriptor equality and the native-error mapping.
// The real-service behaviors (resolution, shaping, queries, lifecycle)
// live in internal/textspec (the repo convention: engine tests outside
// the runtime package), and the conformance gate lives in
// internal/portfixture. The service-backed smoke test at the bottom is
// Windows-only, like layout_engine_test.go.

import (
	"errors"
	"math"
	"runtime"
	"testing"

	"gpui-go/internal/native"
)

// TestDefaultLineHeightGoldenRatio pins the reference text style's
// default line height factor: 16 x f32::consts::GOLDEN_RATIO is exactly
// the recorded 0x41CF1BBD (25.888544), and every size scales the same
// constant in f32 arithmetic.
func TestDefaultLineHeightGoldenRatio(t *testing.T) {
	if got, want := math.Float32bits(DefaultLineHeight(16)), uint32(0x41CF1BBD); got != want {
		t.Errorf("DefaultLineHeight(16) bits = %08X, want %08X", got, want)
	}
	if got, want := DefaultLineHeight(0), float32(0); got != want {
		t.Errorf("DefaultLineHeight(0) = %v, want %v", got, want)
	}
	// The f32 factor itself: the nearest f32 to the golden ratio.
	if got, want := math.Float32bits(goldenRatio), uint32(0x3FCF1BBD); got != want {
		t.Errorf("goldenRatio bits = %08X, want %08X", got, want)
	}
	// 12px -> phi * 12 in f32.
	if got, want := DefaultLineHeight(12), float32(12)*float32(1.6180339887498948482045868343656); got != want {
		t.Errorf("DefaultLineHeight(12) = %v, want %v", got, want)
	}
}

// TestFontMetricsPixelScaling pins the pinned FontMetrics::ascent /
// descent / x_height arithmetic: (metric / units_per_em) * font_size in
// f32, division first (the recorded Segoe UI metrics at 16px).
func TestFontMetricsPixelScaling(t *testing.T) {
	metrics := FontMetrics{
		UnitsPerEm: 2048,
		Ascent:     2210,
		Descent:    514,
		XHeight:    1024,
	}
	if got, want := math.Float32bits(metrics.AscentPx(16)), uint32(0x418A2000); got != want {
		t.Errorf("AscentPx(16) bits = %08X, want %08X (17.265625)", got, want)
	}
	if got, want := math.Float32bits(metrics.DescentPx(16)), uint32(0x40808000); got != want {
		t.Errorf("DescentPx(16) bits = %08X, want %08X (4.015625)", got, want)
	}
	if got, want := math.Float32bits(metrics.XHeightPx(16)), uint32(0x41000000); got != want {
		t.Errorf("XHeightPx(16) bits = %08X, want %08X (8.0)", got, want)
	}
	// The division-first order matters when the quotient rounds.
	odd := FontMetrics{UnitsPerEm: 3, Ascent: 1}
	if got, want := odd.AscentPx(1), (float32(1)/float32(3))*float32(1); got != want {
		t.Errorf("AscentPx with a rounding quotient = %v, want the division-first %v", got, want)
	}
}

// TestDefaultFontDescriptor pins the pinned Font::default() shape: the
// system UI alias, normal weight and style, no features, no fallbacks.
func TestDefaultFontDescriptor(t *testing.T) {
	descriptor := DefaultFontDescriptor()
	if descriptor.Family != ".SystemUIFont" {
		t.Errorf("default family = %q, want .SystemUIFont", descriptor.Family)
	}
	if descriptor.Weight != 400 {
		t.Errorf("default weight = %v, want 400 (FontWeight::NORMAL)", descriptor.Weight)
	}
	if descriptor.Style != FontStyleNormal {
		t.Errorf("default style = %v, want Normal", descriptor.Style)
	}
	if len(descriptor.Features) != 0 || len(descriptor.Fallbacks) != 0 {
		t.Errorf("default features/fallbacks = %v/%v, want none", descriptor.Features, descriptor.Fallbacks)
	}
}

// TestFontDescriptorEqual pins the pinned Font PartialEq semantics the
// runner's run-resolution skip depends on: full descriptor equality
// with element-wise feature and fallback comparison.
func TestFontDescriptorEqual(t *testing.T) {
	base := FontDescriptor{Family: "Segoe UI", Weight: 400, Style: FontStyleNormal}
	if base.Equal(DefaultFontDescriptor()) {
		t.Errorf("different families compared equal")
	}
	same := base
	if !base.Equal(same) {
		t.Errorf("identical descriptors did not compare equal")
	}
	bold := base
	bold.Weight = 700
	if base.Equal(bold) {
		t.Errorf("different weights compared equal")
	}
	italic := base
	italic.Style = FontStyleItalic
	if base.Equal(italic) {
		t.Errorf("different styles compared equal")
	}
	features := base
	features.Features = []FontFeature{{Tag: "calt", Value: 0}}
	if base.Equal(features) {
		t.Errorf("feature list presence compared equal to absence")
	}
	moreFeatures := features
	moreFeatures.Features = append(moreFeatures.Features, FontFeature{Tag: "liga", Value: 1})
	if features.Equal(moreFeatures) {
		t.Errorf("different feature lists compared equal")
	}
	fallbacks := base
	fallbacks.Fallbacks = []string{"Arial"}
	if base.Equal(fallbacks) {
		t.Errorf("fallback list presence compared equal to absence")
	}
	// Empty and nil feature lists are equal: the pinned builder maps an
	// empty list to the default (absent) feature set.
	emptyFeatures := base
	emptyFeatures.Features = []FontFeature{}
	if !base.Equal(emptyFeatures) {
		t.Errorf("nil and empty feature lists did not compare equal")
	}
}

// TestTextStyleAndAffinityNames pins the reference's variant names the
// trace records.
func TestTextStyleAndAffinityNames(t *testing.T) {
	if got := FontStyleNormal.String(); got != "Normal" {
		t.Errorf("FontStyleNormal.String() = %q, want Normal", got)
	}
	if got := FontStyleItalic.String(); got != "Italic" {
		t.Errorf("FontStyleItalic.String() = %q, want Italic", got)
	}
	if got := FontStyleOblique.String(); got != "Oblique" {
		t.Errorf("FontStyleOblique.String() = %q, want Oblique", got)
	}
	if got := CaretAffinityDownstream.String(); got != "downstream" {
		t.Errorf("CaretAffinityDownstream.String() = %q, want downstream", got)
	}
	if got := CaretAffinityUpstream.String(); got != "upstream" {
		t.Errorf("CaretAffinityUpstream.String() = %q, want upstream", got)
	}
}

// TestWrapTextErrorMapping pins the native sentinel -> gpui sentinel
// mapping the adapter's error surface depends on.
func TestWrapTextErrorMapping(t *testing.T) {
	cases := []struct {
		native error
		want   error
	}{
		{native.ErrTextStaleHandle, ErrTextStaleHandle},
		{native.ErrTextBadHandle, ErrTextBadHandle},
		{native.ErrTextBadValue, ErrTextValue},
		{native.ErrTextFontUnresolved, ErrTextFontUnresolved},
		{native.ErrTextShapingLimit, ErrTextShapingLimit},
	}
	for _, c := range cases {
		err := wrapTextError("op", c.native)
		if !errors.Is(err, c.want) {
			t.Errorf("wrapTextError(%v) = %v, want the gpui %v sentinel", c.native, err, c.want)
		}
	}
	if err := wrapTextError("op", nil); err != nil {
		t.Errorf("wrapTextError(nil) = %v, want nil", err)
	}
	other := errors.New("some other failure")
	err := wrapTextError("op", other)
	if !errors.Is(err, other) {
		t.Errorf("wrapTextError(unknown) = %v, want the wrapped original", err)
	}
}

// TestTextSystemSmoke is the Windows-only service smoke test: the
// process-global text system loads the native text service, resolves
// the default font to a canonical id and shapes a basic document. The
// full service behavior is pinned in internal/textspec and the fx-0004
// conformance gate in internal/portfixture.
func TestTextSystemSmoke(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native text artifact is Windows AMD64; the text system cannot run on %s", runtime.GOOS)
	}
	system, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	again, err := DefaultTextSystem()
	if err != nil || again != system {
		t.Fatalf("DefaultTextSystem again = (%v, %v), want the same instance", again, err)
	}

	identity, err := system.ResolveFont(DefaultFontDescriptor())
	if err != nil {
		t.Fatalf("ResolveFont(default): %v", err)
	}
	if !identity.FontID.IsCanonical() {
		t.Errorf("resolved font id %#x lacks the canonical bit", uint64(identity.FontID))
	}

	line, err := system.ShapeText(TextLayoutRequest{
		Text:     "Hello, gpui!",
		FontSize: 16,
		Runs:     []TextRun{{Len: 12, Font: FontDescriptor{Family: "Segoe UI", Weight: 400}}},
	})
	if err != nil {
		t.Fatalf("ShapeText: %v", err)
	}
	defer func() { _ = line.Dispose() }()
	summary, err := line.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.TextLen != 12 || summary.LineCount != 1 || summary.Width <= 0 {
		t.Errorf("summary = %+v, want 12 bytes on one row with a real advance", summary)
	}
	if got, want := summary.FontSize, float32(16); got != want {
		t.Errorf("summary font size = %v, want %v", got, want)
	}
	// The default line height is the golden ratio derivation.
	if got, want := DefaultLineHeight(16), float32(16)*goldenRatio; got != want {
		t.Errorf("DefaultLineHeight(16) = %v, want %v", got, want)
	}
}
