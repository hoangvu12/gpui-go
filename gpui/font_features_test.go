package gpui

// Ticket 06 (browser-core): tabular-nums and friends work as Tailwind
// classes. The classes map to their OpenType features (class.go,
// numericVariantClass — Chromium's FromFontDescription, platform/fonts/
// shaping/font_features.cc:157-196), and the features demonstrably
// reach the shaper: in Arial the default figures are proportional (111
// advances 24.3px where 888 advances 26.7px at 16px) and tnum makes
// every digit share the advance. (Segoe UI's figures are tabular by
// default, so the class is a no-op there — the reason the parity
// fixture's rows use Arial.)

import (
	"testing"
)

// TestTabularNumsReachTheShaper pins the feature effect through the
// real text service: shaping "111" and "888" in Arial gives different
// advances by default and equal advances under tnum (fails, never
// skips, without the artifact).
func TestTabularNumsReachTheShaper(t *testing.T) {
	ts, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	tnum := []FontFeature{{Tag: "tnum", Value: 1}}
	width := func(features []FontFeature, text string) float32 {
		t.Helper()
		line, err := ts.ShapeText(TextLayoutRequest{
			Text:     text,
			FontSize: 16,
			Runs:     []TextRun{{Len: len(text), Font: FontDescriptor{Family: "Arial", Weight: DefaultFontWeight, Features: features}}},
		})
		if err != nil {
			t.Fatalf("shaping %q: %v", text, err)
		}
		t.Cleanup(func() { _ = line.Dispose() })
		summary, err := line.Summary()
		if err != nil {
			t.Fatalf("summary %q: %v", text, err)
		}
		return summary.Width
	}
	ones, eights := width(nil, "111"), width(nil, "888")
	if ones == eights {
		// Arial's default figures are proportional; if this ever fails,
		// the fixture font changed its default figures and the test's
		// font choice needs revisiting.
		t.Fatalf("the unfeatured 111 and 888 advances are equal (%v): Arial's proportional figures are not observable", ones)
	}
	tnumOnes, tnumEights := width(tnum, "111"), width(tnum, "888")
	if tnumOnes != tnumEights {
		t.Errorf("tnum advances differ: 111 = %v, 888 = %v, want equal (tabular figures)", tnumOnes, tnumEights)
	}
}
