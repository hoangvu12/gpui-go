package gpui

// Ticket 06 (browser-core): non-zero letter-spacing turns off the
// optional ligatures at shaping time. Chromium's rule (platform/fonts/
// shaping/font_features.cc:59-86): letter-spacing != 0 pushes liga=0,
// clig=0 and calt=0, and keeps dlig/hlig from being enabled; a feature
// the author set explicitly wins (the font-feature-settings values are
// appended after the spacing-derived ones, and the later setting wins
// in HarfBuzz).

import (
	"slices"
	"testing"
)

// TestLetterSpacingLigatureFeatures pins the pure mapping: the three
// optional-ligature features appear with value 0 exactly when the
// spacing is non-zero, an explicit setting of a tag is preserved (and
// not duplicated), and the caller's slice is never mutated.
func TestLetterSpacingLigatureFeatures(t *testing.T) {
	zero := float32(0)
	nonzero := float32(0.5)
	cases := []struct {
		name     string
		features []FontFeature
		spacing  *float32
		want     []FontFeature
	}{
		{"no spacing keeps the features", []FontFeature{{Tag: "tnum", Value: 1}}, nil, []FontFeature{{Tag: "tnum", Value: 1}}},
		{"zero spacing keeps the features", []FontFeature{{Tag: "tnum", Value: 1}}, &zero, []FontFeature{{Tag: "tnum", Value: 1}}},
		{"spacing adds the three zeros", nil, &nonzero, []FontFeature{{Tag: "liga", Value: 0}, {Tag: "clig", Value: 0}, {Tag: "calt", Value: 0}}},
		{"an explicit tag is not duplicated or overridden", []FontFeature{{Tag: "liga", Value: 1}, {Tag: "tnum", Value: 1}}, &nonzero,
			[]FontFeature{{Tag: "liga", Value: 1}, {Tag: "tnum", Value: 1}, {Tag: "clig", Value: 0}, {Tag: "calt", Value: 0}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := letterSpacingLigatureFeatures(c.features, c.spacing)
			if !slices.Equal(got, c.want) {
				t.Errorf("features = %+v, want %+v", got, c.want)
			}
		})
	}
	// The caller's slice is never mutated (no writing into spare
	// capacity).
	shared := make([]FontFeature, 0, 8)
	shared = append(shared, FontFeature{Tag: "tnum", Value: 1})
	letterSpacingLigatureFeatures(shared, &nonzero)
	if len(shared) != 1 || shared[0].Tag != "tnum" {
		t.Errorf("the shared slice was mutated: %+v", shared)
	}
}

// TestLetterSpacingDisablesLigaturesAtShaping pins the rule end to end
// through the real text service: shaping "fi" in Cambria (the one
// system family the native feature test observed a liga substitution
// in, reference/native/src/text.rs:3862-3865) with non-zero letter
// spacing produces exactly the glyphs explicit liga/clig/calt = 0
// produce, and differs from the zero-spacing shaping. Letter spacing
// alone changes positions, so the comparison is over glyph ids.
//
// This test needs the native text service; it fails (never skips) when
// the artifact is unavailable.
func TestLetterSpacingDisablesLigaturesAtShaping(t *testing.T) {
	ts, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	const text = "fi"
	spacing := float32(0.5)
	shape := func(spacing *float32, features []FontFeature) []TextGlyph {
		font := FontDescriptor{Family: "Cambria", Weight: DefaultFontWeight, Features: features}
		run := TextRun{Len: len(text), Font: font, LetterSpacing: spacing}
		line, err := ts.ShapeText(TextLayoutRequest{Text: text, FontSize: 24, Runs: []TextRun{run}})
		if err != nil {
			t.Fatalf("shaping (%v, %+v): %v", spacing, features, err)
		}
		t.Cleanup(func() { _ = line.Dispose() })
		glyphs, err := line.Glyphs()
		if err != nil {
			t.Fatalf("glyphs (%v, %+v): %v", spacing, features, err)
		}
		return glyphs
	}
	ids := func(glyphs []TextGlyph) []uint32 {
		ids := make([]uint32, len(glyphs))
		for i, glyph := range glyphs {
			ids[i] = glyph.ID
		}
		return ids
	}

	spaced := shape(&spacing, nil)
	explicitOff := shape(&spacing, []FontFeature{
		{Tag: "liga", Value: 0}, {Tag: "clig", Value: 0}, {Tag: "calt", Value: 0},
	})
	unspaced := shape(nil, nil)

	if !slices.Equal(ids(spaced), ids(explicitOff)) {
		t.Errorf("spaced glyphs %v differ from the explicit liga/clig/calt=0 shaping %v", ids(spaced), ids(explicitOff))
	}
	if slices.Equal(ids(spaced), ids(unspaced)) {
		// Cambria's liga substitution makes the f glyph differ; if this
		// ever holds, the fixture font lost its ligature and the test's
		// font choice needs revisiting.
		t.Errorf("spaced glyphs %v equal the ligated unspaced %v: the letter-spacing rule is not observable in Cambria on this machine", ids(spaced), ids(unspaced))
	}
	// An explicit liga=1 overrides the spacing rule (Chrome: the
	// font-feature-settings entries win over the spacing-derived ones).
	explicitOn := shape(&spacing, []FontFeature{{Tag: "liga", Value: 1}})
	if !slices.Equal(ids(explicitOn), ids(unspaced)) {
		t.Errorf("explicit liga=1 under spacing gave %v, want the ligated unspaced glyphs %v", ids(explicitOn), ids(unspaced))
	}
}
