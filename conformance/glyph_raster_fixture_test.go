package conformance

import (
	"os"
	"testing"
)

// glyphRasterFixturePath is the fx-0005 fixture envelope, and
// glyphRasterRecordedTracePath is the reference trace recorded from it
// by conformance/cmd/recordreference.
const (
	glyphRasterFixturePath       = "fixtures/fx-0005-glyph-raster.json"
	glyphRasterRecordedTracePath = "recorded/fx-0005-glyph-raster/trace.json"
)

// wantGlyphRasterCaseLabels lists the six fx-0005 cases in execution
// order.
var wantGlyphRasterCaseLabels = []string{
	"latin-grayscale",
	"latin-subpixel",
	"latin-color-tint",
	"space-empty-raster",
	"emoji-color",
	"missing-glyph",
}

// glyphCaseByLabel returns the case with the given label.
func glyphCaseByLabel(t *testing.T, env *Envelope, label string) GlyphRasterCase {
	t.Helper()
	for _, c := range env.Inputs.GlyphCases.Cases {
		if c.Label == label {
			return c
		}
	}
	t.Fatalf("no glyph-raster case with label %q", label)
	return GlyphRasterCase{}
}

// glyphCaseField returns one case field pointer or a typed zero for the
// absent case (the fixtures carry optional pointers).
func glyphCaseCharCount(t *testing.T, c GlyphRasterCase) int {
	t.Helper()
	return len(c.Chars)
}

// TestGlyphRasterEnvelope checks the shipped fx-0005 fixture against the
// glyph-raster wire format: fixture kind detection, six cases with the
// expected labels, and the per-case inputs the recorded trace depends
// on.
func TestGlyphRasterEnvelope(t *testing.T) {
	env, err := LoadEnvelope(glyphRasterFixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope(%s): %v", glyphRasterFixturePath, err)
	}
	if env.FixtureID != "fx-0005-glyph-raster" {
		t.Errorf("fixture_id = %q, want %q", env.FixtureID, "fx-0005-glyph-raster")
	}
	if env.FixtureKind != FixtureKindGlyphRaster {
		t.Fatalf("fixture_kind = %q, want %q", env.FixtureKind, FixtureKindGlyphRaster)
	}
	if !env.IsGlyphRasterKind() {
		t.Errorf("IsGlyphRasterKind() = false, want true")
	}
	if env.IsTextGeometryKind() {
		t.Errorf("IsTextGeometryKind() = true, want false for glyph-raster-v1")
	}
	if env.Profile != "test" {
		t.Errorf("profile = %q, want %q", env.Profile, "test")
	}
	if want := []string{"windows", "system-fonts", "directwrite"}; len(env.Environment) != 3 {
		t.Errorf("environment = %q, want %q", env.Environment, want)
	} else {
		for i, w := range want {
			if env.Environment[i] != w {
				t.Errorf("environment[%d] = %q, want %q", i, env.Environment[i], w)
			}
		}
	}

	// The glyph-raster inputs live in inputs.glyph_cases; every other
	// fixture's inputs stay zero-valued for this fixture kind.
	if env.Inputs.GlyphCases == nil {
		t.Fatalf("inputs.glyph_cases is nil")
	}
	if env.Inputs.TextCases != nil || env.Inputs.LayoutCases != nil || env.Inputs.SceneCases != nil {
		t.Errorf("text_cases = %+v, layout_cases = %+v, scene_cases = %+v; all want nil for glyph-raster-v1",
			env.Inputs.TextCases, env.Inputs.LayoutCases, env.Inputs.SceneCases)
	}
	cases := env.Inputs.GlyphCases.Cases
	if len(cases) != len(wantGlyphRasterCaseLabels) {
		t.Fatalf("glyph_cases has %d cases, want %d", len(cases), len(wantGlyphRasterCaseLabels))
	}
	for i, want := range wantGlyphRasterCaseLabels {
		if got := cases[i].Label; got != want {
			t.Errorf("case %d label = %q, want %q", i, got, want)
		}
	}

	// Every case rasters a system font with at least one query per axis.
	for i, c := range cases {
		if c.Font.Family == "" {
			t.Errorf("case %d (%q) has an empty family", i, c.Label)
		}
		if len(c.Chars) == 0 {
			t.Errorf("case %q has no characters", c.Label)
		}
		if len(c.Sizes) == 0 {
			t.Errorf("case %q has no sizes", c.Label)
		}
		if len(c.Scales) == 0 {
			t.Errorf("case %q has no scales", c.Label)
		}
		switch c.Mode {
		case "grayscale", "subpixel", "color":
		default:
			t.Errorf("case %q mode = %q, want grayscale/subpixel/color", c.Label, c.Mode)
		}
		if c.Mode == "color" && c.SceneColor == nil {
			t.Errorf("case %q is color mode without a scene color", c.Label)
		}
	}

	// The color-tint case carries the pinned currentColor quantization
	// input; the emoji case records whatever the pinned fallback routing
	// actually produces (never a claimed COLRv1 support).
	tint := glyphCaseByLabel(t, env, "latin-color-tint")
	if tint.SceneColor == nil {
		t.Fatalf("latin-color-tint has no scene color")
	}
	emoji := glyphCaseByLabel(t, env, "emoji-color")
	if emoji.Font.Family != "Segoe UI Emoji" {
		t.Errorf("emoji-color family = %q, want Segoe UI Emoji", emoji.Font.Family)
	}
	missing := glyphCaseByLabel(t, env, "missing-glyph")
	if glyphCaseCharCount(t, missing) != 1 {
		t.Errorf("missing-glyph has %d characters, want 1", glyphCaseCharCount(t, missing))
	}
}

// TestGlyphRasterRecordedTracePresent checks the recorded reference trace
// and its run metadata: the events the gate compares against must exist
// and the recorded run must have used the same envelope bytes.
func TestGlyphRasterRecordedTracePresent(t *testing.T) {
	trace, err := LoadTrace(glyphRasterRecordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}
	if trace.FixtureID != "fx-0005-glyph-raster" {
		t.Errorf("trace fixture_id = %q, want fx-0005-glyph-raster", trace.FixtureID)
	}
	if trace.FixtureKind != FixtureKindGlyphRaster {
		t.Errorf("trace fixture_kind = %q, want %q", trace.FixtureKind, FixtureKindGlyphRaster)
	}
	if err := VerifyEnvelopeSHA256(trace, glyphRasterFixturePath); err != nil {
		t.Errorf("envelope hash mismatch: %v", err)
	}
	// The recorded trace: 1 construction record, 6 case-begin/end pairs,
	// 26 glyph-raster events (39 total) — every glyph event carrying the
	// exact raster outcome (format, bounds, byte count, pixel hash).
	names := map[string]int{}
	for _, event := range trace.Events {
		names[event.Name]++
	}
	for _, want := range []struct {
		name string
		min  int
	}{
		{"raster-system-begin", 1},
		{"case-begin", 6},
		{"case-end", 6},
		{"glyph-raster", 26},
	} {
		if names[want.name] < want.min {
			t.Errorf("trace has %d %q events, want at least %d", names[want.name], want.name, want.min)
		}
	}
	// The missing-glyph outcome is recorded honestly (hasGlyph 0).
	sawMissing := false
	for _, event := range trace.Events {
		if event.Name == "glyph-raster" {
			if has, ok := event.Fields["hasGlyph"].(float64); ok && has == 0 {
				sawMissing = true
			}
		}
	}
	if !sawMissing {
		t.Errorf("trace records no missing-glyph outcome (hasGlyph 0)")
	}
	// The recorded run metadata exists.
	if _, err := os.Stat("recorded/fx-0005-glyph-raster/run-meta.json"); err != nil {
		t.Errorf("run-meta.json missing: %v", err)
	}
}
