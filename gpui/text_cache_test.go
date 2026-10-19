package gpui

import (
	"math"
	"testing"
)

// The line-layout cache key (ticket 02): the reference CacheKey
// (line_layout.rs:1128-1135) identifies a shaped line by text, font
// size, runs, wrap width and line clamp, so equal requests reuse the
// cached line and any distinguishing input misses. These tests pin the
// port's key: canonical, collision-free and total over the request
// surface, without touching the native text stack.

// keyRequest builds one single-run request with the given fields.
func keyRequest(text string, fontSize float32, font FontDescriptor, wrap *float32, clamp *uint32) TextLayoutRequest {
	return TextLayoutRequest{
		Text:      text,
		FontSize:  fontSize,
		Runs:      []TextRun{{Len: len(text), Font: font}},
		WrapWidth: wrap,
		LineClamp: clamp,
	}
}

// TestShapedLineKeyEquality pins the reuse condition: two requests with
// the same text, font size, font (family, weight, style, features,
// fallbacks), letter spacing, wrap width and line clamp produce equal
// keys, whatever the slices' provenance (fresh allocations of equal
// run lists encode identically, like the reference's field-wise
// CacheKeyRef comparison).
func TestShapedLineKeyEquality(t *testing.T) {
	wrap := float32(120)
	clamp := uint32(2)
	letter := float32(0.5)
	font := FontDescriptor{
		Family:    "Segoe UI",
		Weight:    400,
		Style:     FontStyleNormal,
		Features:  []FontFeature{{Tag: "liga", Value: 1}, {Tag: "calt", Value: 1}},
		Fallbacks: []string{"Lilex", "Arial"},
	}
	build := func() TextLayoutRequest {
		// Fresh slices every call: equality must not depend on identity.
		copied := font
		copied.Features = append([]FontFeature{}, font.Features...)
		copied.Fallbacks = append([]string{}, font.Fallbacks...)
		request := keyRequest("the same text", 16, copied, &wrap, &clamp)
		request.Runs[0].LetterSpacing = &letter
		return request
	}
	first, second := shapedLineKeyOf(build()), shapedLineKeyOf(build())
	if first != second {
		t.Fatalf("shapedLineKeyOf of equal requests differs: %+v vs %+v", first, second)
	}
}

// TestShapedLineKeyDistinguishesInputs pins the miss conditions: every
// input the reference CacheKey carries separates keys — text, font
// size, run length, family, weight, style, features, fallbacks, letter
// spacing, wrap width (including presence) and line clamp (including
// presence).
func TestShapedLineKeyDistinguishesInputs(t *testing.T) {
	base := keyRequest("base", 16, FontDescriptor{Family: "Segoe UI", Weight: 400, Style: FontStyleNormal}, nil, nil)
	variations := map[string]TextLayoutRequest{
		"text":      keyRequest("bass", 16, FontDescriptor{Family: "Segoe UI"}, nil, nil),
		"font size": keyRequest("base", 17, FontDescriptor{Family: "Segoe UI"}, nil, nil),
		"run length": {
			Text: "base", FontSize: 16,
			Runs: []TextRun{{Len: 3, Font: FontDescriptor{Family: "Segoe UI"}}},
		},
		"family":    keyRequest("base", 16, FontDescriptor{Family: "Consolas"}, nil, nil),
		"weight":    keyRequest("base", 16, FontDescriptor{Family: "Segoe UI", Weight: 700}, nil, nil),
		"style":     keyRequest("base", 16, FontDescriptor{Family: "Segoe UI", Style: FontStyleItalic}, nil, nil),
		"features":  keyRequest("base", 16, FontDescriptor{Family: "Segoe UI", Features: []FontFeature{{Tag: "liga", Value: 1}}}, nil, nil),
		"fallbacks": keyRequest("base", 16, FontDescriptor{Family: "Segoe UI", Fallbacks: []string{"Arial"}}, nil, nil),
		"letter spacing": {
			Text: "base", FontSize: 16,
			Runs: []TextRun{{Len: 4, Font: FontDescriptor{Family: "Segoe UI"}, LetterSpacing: ptrFloat32(0.5)}},
		},
	}
	wrap := float32(120)
	clamp := uint32(2)
	variations["wrap width"] = keyRequest("base", 16, FontDescriptor{Family: "Segoe UI"}, &wrap, nil)
	variations["wrap width value"] = keyRequest("base", 16, FontDescriptor{Family: "Segoe UI"}, ptrFloat32(121), nil)
	variations["line clamp"] = keyRequest("base", 16, FontDescriptor{Family: "Segoe UI"}, nil, &clamp)
	variations["line clamp value"] = keyRequest("base", 16, FontDescriptor{Family: "Segoe UI"}, nil, ptrUint32(3))

	baseKey := shapedLineKeyOf(base)
	for name, request := range variations {
		if key := shapedLineKeyOf(request); key == baseKey {
			t.Errorf("shapedLineKeyOf ignores %s (equal to the base key)", name)
		}
	}
}

// TestShapedLineKeyRunListOrder pins the run list's structural
// equality: two runs split differently over the same text are different
// cache entries (the reference's SmallVec compared element-wise, order
// included), and distinct run counts never collide.
func TestShapedLineKeyRunListOrder(t *testing.T) {
	font := FontDescriptor{Family: "Segoe UI"}
	one := TextLayoutRequest{
		Text: "abcd", FontSize: 16,
		Runs: []TextRun{{Len: 4, Font: font}},
	}
	two := TextLayoutRequest{
		Text: "abcd", FontSize: 16,
		Runs: []TextRun{{Len: 2, Font: font}, {Len: 2, Font: font}},
	}
	if shapedLineKeyOf(one) == shapedLineKeyOf(two) {
		t.Fatalf("shapedLineKeyOf collides a 1-run and a 2-run request over the same text")
	}
	reordered := TextLayoutRequest{
		Text: "abcd", FontSize: 16,
		Runs: []TextRun{{Len: 2, Font: FontDescriptor{Family: "Consolas"}}, {Len: 2, Font: font}},
	}
	swapped := TextLayoutRequest{
		Text: "abcd", FontSize: 16,
		Runs: []TextRun{{Len: 2, Font: font}, {Len: 2, Font: FontDescriptor{Family: "Consolas"}}},
	}
	if shapedLineKeyOf(reordered) == shapedLineKeyOf(swapped) {
		t.Fatalf("shapedLineKeyOf ignores the run order")
	}
}

// TestShapedLineKeyFloatBits pins the float encoding: bit-identical
// floats are equal keys, and a NaN font size (rejected by ShapeText
// before any entry is cached) still encodes deterministically without
// disturbing other keys.
func TestShapedLineKeyFloatBits(t *testing.T) {
	if shapedLineKeyOf(keyRequest("t", 16, FontDescriptor{}, nil, nil)) !=
		shapedLineKeyOf(keyRequest("t", 16, FontDescriptor{}, nil, nil)) {
		t.Fatalf("equal font sizes produce different keys")
	}
	nan := shapedLineKeyOf(keyRequest("t", float32(math.NaN()), FontDescriptor{}, nil, nil))
	if nan == shapedLineKeyOf(keyRequest("t", 16, FontDescriptor{}, nil, nil)) {
		t.Fatalf("a NaN font size collides with a finite one")
	}
}

// ptrFloat32 boxes a float32.
func ptrFloat32(v float32) *float32 { return &v }

// ptrUint32 boxes a uint32.
func ptrUint32(v uint32) *uint32 { return &v }
