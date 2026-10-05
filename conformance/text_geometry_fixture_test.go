package conformance

import (
	"encoding/json"
	"os"
	"testing"
)

// textGeometryFixturePath is the fx-0004 fixture envelope, and
// textGeometryRecordedTracePath is the reference trace recorded from it
// by conformance/cmd/recordreference.
const (
	textGeometryFixturePath       = "fixtures/fx-0004-text-geometry.json"
	textGeometryRecordedTracePath = "recorded/fx-0004-text-geometry/trace.json"
)

// wantTextGeometryCaseLabels lists the ten fx-0004 cases in execution
// order.
var wantTextGeometryCaseLabels = []string{
	"latin-basic",
	"grapheme-clusters",
	"cjk-latin-mixed",
	"wrap-narrow",
	"line-height-override",
	"features-letter-spacing",
	"multi-run-weights",
	"wrap-clamp",
	"empty-string",
	"newline-only",
}

// textGoldenRatioDefaultLineHeight is the f32 bit pattern of the
// reference text style's default line height for a 16px font size: 16 ×
// f32::consts::GOLDEN_RATIO (TextStyle::default().line_height = phi(),
// crates/gpui/src/style.rs).
const textGoldenRatioDefaultLineHeight = "f32:41CF1BBD"

// textCaseByLabel returns the case with the given label.
func textCaseByLabel(t *testing.T, env *Envelope, label string) TextGeometryCase {
	t.Helper()
	for _, c := range env.Inputs.TextCases.Cases {
		if c.Label == label {
			return c
		}
	}
	t.Fatalf("no text-geometry case with label %q", label)
	return TextGeometryCase{}
}

// countField returns a trace event's numeric field as a float64.
func countField(t *testing.T, event TraceEvent, name string) float64 {
	t.Helper()
	value, ok := event.Fields[name].(float64)
	if !ok {
		t.Fatalf("%s %q field %q = %v, want a number", event.Name, event.Label, name, event.Fields[name])
	}
	return value
}

// TestTextGeometryEnvelope checks the shipped fx-0004 fixture against
// the text-geometry wire format: fixture kind detection, ten cases with
// the expected labels, and the per-case inputs the recorded trace
// depends on.
func TestTextGeometryEnvelope(t *testing.T) {
	env, err := LoadEnvelope(textGeometryFixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope(%s): %v", textGeometryFixturePath, err)
	}
	if env.FixtureID != "fx-0004-text-geometry" {
		t.Errorf("fixture_id = %q, want %q", env.FixtureID, "fx-0004-text-geometry")
	}
	if env.FixtureKind != FixtureKindTextGeometry {
		t.Fatalf("fixture_kind = %q, want %q", env.FixtureKind, FixtureKindTextGeometry)
	}
	if !env.IsTextGeometryKind() {
		t.Errorf("IsTextGeometryKind() = false, want true")
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

	// The text-geometry inputs live in inputs.text_cases; the legacy
	// inputs stay zero-valued for this fixture kind.
	if env.Inputs.TextCases == nil {
		t.Fatalf("inputs.text_cases is nil")
	}
	if env.Inputs.LayoutCases != nil || env.Inputs.SceneCases != nil {
		t.Errorf("layout_cases = %+v, scene_cases = %+v; both want nil for text-geometry-v1", env.Inputs.LayoutCases, env.Inputs.SceneCases)
	}
	cases := env.Inputs.TextCases.Cases
	if len(cases) != len(wantTextGeometryCaseLabels) {
		t.Fatalf("text_cases has %d cases, want %d", len(cases), len(wantTextGeometryCaseLabels))
	}
	for i, want := range wantTextGeometryCaseLabels {
		if got := cases[i].Label; got != want {
			t.Errorf("case %d label = %q, want %q", i, got, want)
		}
	}

	// Every case shapes a system font: Segoe UI is present on this
	// machine and its identity is recorded in every case.
	for i, c := range cases {
		if c.Font.Family != "Segoe UI" {
			t.Errorf("case %d (%q) font family = %q, want %q", i, c.Label, c.Font.Family, "Segoe UI")
		}
		if c.FontSize <= 0 {
			t.Errorf("case %q font_size = %v, want > 0", c.Label, c.FontSize)
		}
		if c.Font.Style != "" && c.Font.Style != TextFontStyleNormal {
			t.Errorf("case %q font style = %q, want %q or empty", c.Label, c.Font.Style, TextFontStyleNormal)
		}
		// Run coverage: present runs must cover the text exactly (the
		// reference harness validates the same rule before shaping).
		textLen := len(c.Text)
		if len(c.Runs) == 0 {
			continue
		}
		covered := uint64(0)
		for j, run := range c.Runs {
			covered += run.Len
			if run.Len > uint64(textLen) {
				t.Fatalf("case %q run %d len %d exceeds text length %d", c.Label, j, run.Len, textLen)
			}
		}
		if covered != uint64(textLen) {
			t.Fatalf("case %q runs cover %d bytes, want %d", c.Label, covered, textLen)
		}
	}

	// The basic Latin case: 12 bytes, cluster-boundary caret mode, five
	// hit points and a mid-line selection.
	latin := textCaseByLabel(t, env, "latin-basic")
	if want := "Hello, gpui!"; latin.Text != want {
		t.Errorf("latin-basic text = %q, want %q", latin.Text, want)
	}
	if want := 16.0; latin.FontSize != want {
		t.Errorf("latin-basic font_size = %v, want %v", latin.FontSize, want)
	}
	if latin.CaretMode != TextCaretModeAllClusters {
		t.Errorf("latin-basic caret_mode = %q, want %q", latin.CaretMode, TextCaretModeAllClusters)
	}
	if len(latin.CaretQueries) != 0 {
		t.Errorf("latin-basic caret_queries = %d, want 0 (all-clusters derives them)", len(latin.CaretQueries))
	}
	if len(latin.HitPoints) != 5 {
		t.Errorf("latin-basic hit_points = %d, want 5", len(latin.HitPoints))
	}
	if want := (TextPointIn{X: 10.25, Y: 3}); latin.HitPoints[1] != want {
		t.Errorf("latin-basic hit_points[1] = %+v, want %+v", latin.HitPoints[1], want)
	}
	if want := (TextRangeIn{Start: 2, End: 8}); len(latin.Selections) != 1 || latin.Selections[0] != want {
		t.Errorf("latin-basic selections = %+v, want [%+v]", latin.Selections, want)
	}

	// The grapheme case: decomposed combining marks plus a ZWJ emoji
	// family. "a"+U+0301, "e"+U+0301, space, then the 25-byte family.
	grapheme := textCaseByLabel(t, env, "grapheme-clusters")
	if want := 32; len(grapheme.Text) != want {
		t.Errorf("grapheme-clusters text byte length = %d, want %d (decomposed marks + ZWJ family)", len(grapheme.Text), want)
	}
	if got := len(grapheme.CaretQueries); got != 7 {
		t.Errorf("grapheme-clusters caret_queries = %d, want 7", got)
	}
	if got := grapheme.CaretQueries[1].Index; got != 1 {
		t.Errorf("grapheme-clusters caret_queries[1].index = %d, want 1 (mid-cluster, inside \"a\"+U+0301)", got)
	}
	if got := grapheme.CaretQueries[6].Index; got != 33 {
		t.Errorf("grapheme-clusters caret_queries[6].index = %d, want 33 (beyond the 32-byte text)", got)
	}
	if got := grapheme.CaretQueries[4].Affinity; got != TextAffinityUpstream {
		t.Errorf("grapheme-clusters caret_queries[4].affinity = %q, want %q", got, TextAffinityUpstream)
	}
	if want := (TextRangeIn{Start: 7, End: 32}); len(grapheme.Selections) != 2 || grapheme.Selections[1] != want {
		t.Errorf("grapheme-clusters selections = %+v, want last one %+v", grapheme.Selections, want)
	}

	// The CJK case: 18 bytes of mixed scripts (6 CJK + ABC + 2 CJK).
	if want := 18; len(textCaseByLabel(t, env, "cjk-latin-mixed").Text) != want {
		t.Errorf("cjk-latin-mixed text byte length = %d, want %d", len(textCaseByLabel(t, env, "cjk-latin-mixed").Text), want)
	}

	// The wrapping case: a narrow wrap width, both affinities at every
	// cluster boundary, and a multi-row selection.
	wrap := textCaseByLabel(t, env, "wrap-narrow")
	if wrap.WrapWidth == nil || *wrap.WrapWidth != 70 {
		t.Errorf("wrap-narrow wrap_width = %v, want 70", wrap.WrapWidth)
	}
	if wrap.CaretMode != TextCaretModeAllClustersBoth {
		t.Errorf("wrap-narrow caret_mode = %q, want %q", wrap.CaretMode, TextCaretModeAllClustersBoth)
	}
	if want := (TextRangeIn{Start: 4, End: 16}); len(wrap.Selections) != 1 || wrap.Selections[0] != want {
		t.Errorf("wrap-narrow selections = %+v, want [%+v]", wrap.Selections, want)
	}

	// The line-height override case.
	lineHeight := textCaseByLabel(t, env, "line-height-override")
	if lineHeight.LineHeight == nil || *lineHeight.LineHeight != 40 {
		t.Errorf("line-height-override line_height = %v, want 40", lineHeight.LineHeight)
	}

	// The features + letter-spacing case: calt disabled on the font
	// descriptor and 1.5px spacing on the run.
	features := textCaseByLabel(t, env, "features-letter-spacing")
	if want := 1; len(features.Font.Features) != want {
		t.Fatalf("features-letter-spacing font features = %d, want %d", len(features.Font.Features), want)
	}
	if got := features.Font.Features[0]; got.Tag != "calt" || got.Value != 0 {
		t.Errorf("features-letter-spacing feature = %+v, want {calt 0}", got)
	}
	if len(features.Runs) != 1 {
		t.Fatalf("features-letter-spacing runs = %d, want 1", len(features.Runs))
	}
	if features.Runs[0].LetterSpacing == nil || *features.Runs[0].LetterSpacing != 1.5 {
		t.Errorf("features-letter-spacing run letter_spacing = %v, want 1.5", features.Runs[0].LetterSpacing)
	}
	if features.Runs[0].Font != nil {
		t.Errorf("features-letter-spacing run font = %+v, want nil (inherits the case font)", features.Runs[0].Font)
	}

	// The multi-run case: bold run, italic run, inherited run.
	multi := textCaseByLabel(t, env, "multi-run-weights")
	if len(multi.Runs) != 3 {
		t.Fatalf("multi-run-weights runs = %d, want 3", len(multi.Runs))
	}
	if multi.Runs[0].Font == nil || multi.Runs[0].Font.Weight == nil || *multi.Runs[0].Font.Weight != 700 {
		t.Errorf("multi-run-weights run 0 font = %+v, want weight 700", multi.Runs[0].Font)
	}
	if multi.Runs[1].Font == nil || multi.Runs[1].Font.Style == nil || *multi.Runs[1].Font.Style != TextFontStyleItalic {
		t.Errorf("multi-run-weights run 1 font = %+v, want style Italic", multi.Runs[1].Font)
	}
	if multi.Runs[2].Font != nil {
		t.Errorf("multi-run-weights run 2 font = %+v, want nil (inherits the case font)", multi.Runs[2].Font)
	}

	// The clamped case: 48 bytes wrapped at 60px with a 2-row clamp.
	clamp := textCaseByLabel(t, env, "wrap-clamp")
	if want := 48; len(clamp.Text) != want {
		t.Errorf("wrap-clamp text byte length = %d, want %d", len(clamp.Text), want)
	}
	if clamp.WrapWidth == nil || *clamp.WrapWidth != 60 {
		t.Errorf("wrap-clamp wrap_width = %v, want 60", clamp.WrapWidth)
	}
	if clamp.LineClamp == nil || *clamp.LineClamp != 2 {
		t.Errorf("wrap-clamp line_clamp = %v, want 2", clamp.LineClamp)
	}

	// The empty and newline-only edge cases.
	if got := textCaseByLabel(t, env, "empty-string").Text; got != "" {
		t.Errorf("empty-string text = %q, want empty", got)
	}
	if want := (TextRangeIn{Start: 0, End: 0}); len(textCaseByLabel(t, env, "empty-string").Selections) != 1 || textCaseByLabel(t, env, "empty-string").Selections[0] != want {
		t.Errorf("empty-string selections = %+v, want the empty range", textCaseByLabel(t, env, "empty-string").Selections)
	}
	if got := textCaseByLabel(t, env, "newline-only").Text; got != "\n" {
		t.Errorf("newline-only text = %q, want a single newline", got)
	}

	// Affinity values are from the documented set.
	for _, c := range cases {
		for _, query := range c.CaretQueries {
			if query.Affinity != "" && query.Affinity != TextAffinityDownstream && query.Affinity != TextAffinityUpstream {
				t.Errorf("case %q caret query %d affinity = %q, not a documented affinity", c.Label, query.Index, query.Affinity)
			}
		}
	}
}

// TestTextGeometryEnvelopeRoundTrip checks that the fx-0004 envelope
// marshals back into the same wire shape (text_cases wrapper,
// snake_case keys, pointer optionals), the same way the fx-0002
// round-trip test does for the layout inputs.
func TestTextGeometryEnvelopeRoundTrip(t *testing.T) {
	env, err := LoadEnvelope(textGeometryFixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope: %v", err)
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshaling envelope: %v", err)
	}
	var again Envelope
	if err := json.Unmarshal(data, &again); err != nil {
		t.Fatalf("unmarshaling envelope: %v", err)
	}
	remarshal, err := json.Marshal(again)
	if err != nil {
		t.Fatalf("re-marshaling envelope: %v", err)
	}
	if string(data) != string(remarshal) {
		t.Errorf("envelope does not round-trip:\ngot  %s\nwant %s", remarshal, data)
	}
	if again.Inputs.TextCases == nil || len(again.Inputs.TextCases.Cases) != len(wantTextGeometryCaseLabels) {
		t.Fatalf("round-tripped text_cases = %+v, want %d cases", again.Inputs.TextCases, len(wantTextGeometryCaseLabels))
	}
}

// wantTextGeometryEventCounts pins the event-name histogram of the
// recorded fx-0004 trace.
var wantTextGeometryEventCounts = map[string]int{
	"text-system-begin": 1,
	"font-resolved":     13,
	"case-begin":        10,
	"text-shape-meta":   10,
	"font-metrics":      14,
	"text-line":         14,
	"text-run":          16,
	"text-cluster":      133,
	"text-caret":        80,
	"text-hit":          25,
	"text-selection":    13,
	"case-end":          10,
}

// TestTextGeometryRecordedTrace checks the recorded reference trace for
// fx-0004: identity, envelope hash agreement, the per-case event shape
// (construction, font resolution, shaping meta, lines, runs, clusters,
// carets, hit tests and selections), selected recorded invariants and
// exact self-comparison.
func TestTextGeometryRecordedTrace(t *testing.T) {
	raw, err := os.ReadFile(textGeometryRecordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}
	var trace Trace
	if err := json.Unmarshal(raw, &trace); err != nil {
		t.Fatalf("parsing recorded trace: %v", err)
	}

	if trace.Schema != TraceSchema {
		t.Fatalf("schema = %q, want %q", trace.Schema, TraceSchema)
	}
	if trace.FixtureID != "fx-0004-text-geometry" {
		t.Fatalf("fixture id = %q", trace.FixtureID)
	}
	if trace.FixtureKind != FixtureKindTextGeometry {
		t.Fatalf("fixture kind = %q, want %q", trace.FixtureKind, FixtureKindTextGeometry)
	}
	if trace.Harness.GpuiCommit != "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a" {
		t.Fatalf("gpui commit = %q", trace.Harness.GpuiCommit)
	}

	// The recorded envelope hash must match the fixture file in the repo.
	envHash, err := SHA256Envelope(textGeometryFixturePath)
	if err != nil {
		t.Fatalf("hashing envelope: %v", err)
	}
	if trace.EnvelopeSHA256 != envHash {
		t.Fatalf("recorded trace envelope hash %s does not match current envelope %s; the fixture changed after the recording", trace.EnvelopeSHA256, envHash)
	}

	// The first event records the construction: the pinned Windows
	// platform path with its system font family, service fallbacks, the
	// catalog generation and the presence of the families this fixture
	// depends on.
	first := trace.Events[0]
	if first.Name != "text-system-begin" {
		t.Fatalf("first event = %q, want text-system-begin", first.Name)
	}
	for _, field := range []string{
		"systemFontFamily", "fallbackFamilies", "fontGeneration", "fontFamilyCount",
		"hasSegoeUI", "hasSegoeUIEmoji", "hasArial", "hasMicrosoftYaHei", "hasMicrosoftYaHeiUI", "hasLilex", "hasIBMPlexSans",
	} {
		if _, ok := first.Fields[field]; !ok {
			t.Fatalf("text-system-begin missing field %q", field)
		}
	}
	if got, _ := first.Fields["systemFontFamily"].(string); got != "Segoe UI" {
		t.Errorf("text-system-begin systemFontFamily = %q, want Segoe UI", got)
	}
	if got, _ := first.Fields["fallbackFamilies"].(string); got != "Lilex, IBM Plex Sans, Arial" {
		t.Errorf("text-system-begin fallbackFamilies = %q, want %q", got, "Lilex, IBM Plex Sans, Arial")
	}

	// Event-shape walk: only the text-geometry event names appear, seq is
	// dense, f32 fields are valid bit patterns, font ids carry the
	// canonical high bit, and per-case counts line up with the shape meta.
	caseBeginOrder := []string{}
	caseEnds := 0
	resolvedByCase := map[string]int{}
	resolvedRuns := 0
	defaultResolved := false
	defaultFontID := uint64(0)
	latinCaseFontID := uint64(0)
	perCase := map[string]map[string]int{}
	latinCaretX := []string{}
	latinCaretY := []string{}
	newlineRow1Y := ""
	graphemeBeyondEnd := -1
	hitsInside, hitsOutside := 0, 0
	nameCounts := map[string]int{}
	for i, event := range trace.Events {
		if want := uint64(i + 1); event.Seq != want {
			t.Fatalf("event %d seq = %d, want %d (dense ordered seq)", i, event.Seq, event.Seq)
		}
		nameCounts[event.Name]++
		switch event.Name {
		case "text-system-begin":
			// Checked above.
		case "font-resolved":
			for _, field := range []string{"family", "fontId", "weight", "style", "featureCount", "fallbackCount", "role"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("font-resolved %q missing field %q", event.Label, field)
				}
			}
			fontID, ok := event.Fields["fontId"].(float64)
			if !ok || fontID < float64(CanonicalFontIDBit) {
				t.Fatalf("font-resolved %q fontId = %v, want >= 2^63 (the canonical FontId bit)", event.Label, event.Fields["fontId"])
			}
			if _, err := asF32Bits(event.Fields["weight"]); err != nil {
				t.Fatalf("font-resolved %q weight is not a valid f32 bit pattern: %v", event.Label, err)
			}
			role, _ := event.Fields["role"].(string)
			switch role {
			case "default":
				defaultResolved = true
				defaultFontID = uint64(fontID)
				if event.Label != "default-font" {
					t.Errorf("default font-resolved label = %q, want default-font", event.Label)
				}
				if got, _ := event.Fields["family"].(string); got != ".SystemUIFont" {
					t.Errorf("default font-resolved family = %q, want .SystemUIFont", got)
				}
			case "case":
				resolvedByCase[event.Label]++
				if event.Label == "latin-basic" {
					latinCaseFontID = uint64(fontID)
				}
			case "run":
				resolvedRuns++
				if _, ok := event.Fields["runIndex"]; !ok {
					t.Fatalf("run font-resolved %q missing field runIndex", event.Label)
				}
			default:
				t.Fatalf("font-resolved %q role = %q, want default, case or run", event.Label, role)
			}
		case "case-begin":
			caseBeginOrder = append(caseBeginOrder, event.Label)
			perCase[event.Label] = map[string]int{}
			for _, field := range []string{"label", "textLen", "fontSize", "lineHeight", "runCount", "wrapPresent", "clampPresent"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("case-begin %q missing field %q", event.Label, field)
				}
			}
			if _, err := asF32Bits(event.Fields["fontSize"]); err != nil {
				t.Fatalf("case-begin %q fontSize is not a valid f32 bit pattern: %v", event.Label, err)
			}
			if got, err := asF32Bits(event.Fields["lineHeight"]); err != nil {
				t.Fatalf("case-begin %q lineHeight is not a valid f32 bit pattern: %v", event.Label, err)
			} else if event.Label == "line-height-override" && got != F32Bits(40) {
				t.Errorf("line-height-override lineHeight = %q, want %q", got, F32Bits(40))
			} else if event.Label != "line-height-override" && got != textGoldenRatioDefaultLineHeight {
				t.Errorf("case %q lineHeight = %q, want %q (16 x golden ratio)", event.Label, got, textGoldenRatioDefaultLineHeight)
			}
			wrapPresent, _ := event.Fields["wrapPresent"].(float64)
			if _, has := event.Fields["wrapWidth"]; has != (wrapPresent == 1) {
				t.Errorf("case-begin %q wrapWidth present = %v but wrapPresent = %v", event.Label, has, wrapPresent)
			}
			clampPresent, _ := event.Fields["clampPresent"].(float64)
			if _, has := event.Fields["lineClamp"]; has != (clampPresent == 1) {
				t.Errorf("case-begin %q lineClamp present = %v but clampPresent = %v", event.Label, has, clampPresent)
			}
		case "text-shape-meta":
			for _, field := range []string{"len", "fontSize", "lineHeight", "width", "ascent", "descent", "lineCount", "fragmentCount", "glyphCount", "clusterCount"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("text-shape-meta %q missing field %q", event.Label, field)
				}
			}
			for _, field := range []string{"fontSize", "lineHeight", "width", "ascent", "descent"} {
				if _, err := asF32Bits(event.Fields[field]); err != nil {
					t.Fatalf("text-shape-meta %q field %q is not a valid f32 bit pattern: %v", event.Label, field, err)
				}
			}
			perCase[event.Label]["meta-lineCount"] = int(countField(t, event, "lineCount"))
			perCase[event.Label]["meta-fragmentCount"] = int(countField(t, event, "fragmentCount"))
			perCase[event.Label]["meta-clusterCount"] = int(countField(t, event, "clusterCount"))
			perCase[event.Label]["meta-glyphCount"] = int(countField(t, event, "glyphCount"))
		case "font-metrics":
			for _, field := range []string{"fontId", "source", "unitsPerEm", "ascent", "descent", "lineGap", "underlinePosition", "underlineThickness", "capHeight", "xHeight", "bboxX", "bboxY", "bboxWidth", "bboxHeight", "ascentPx", "descentPx", "xHeightPx"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("font-metrics %q missing field %q", event.Label, field)
				}
			}
			fontID, ok := event.Fields["fontId"].(float64)
			if !ok || fontID < float64(CanonicalFontIDBit) {
				t.Fatalf("font-metrics %q fontId = %v, want >= 2^63", event.Label, event.Fields["fontId"])
			}
			if upem, _ := event.Fields["unitsPerEm"].(float64); upem <= 0 {
				t.Errorf("font-metrics %q unitsPerEm = %v, want > 0", event.Label, event.Fields["unitsPerEm"])
			}
			for _, field := range []string{"ascent", "descent", "lineGap", "capHeight", "xHeight", "ascentPx", "descentPx", "xHeightPx"} {
				if _, err := asF32Bits(event.Fields[field]); err != nil {
					t.Fatalf("font-metrics %q field %q is not a valid f32 bit pattern: %v", event.Label, field, err)
				}
			}
			perCase[event.Label]["font-metrics"]++
		case "text-line":
			for _, field := range []string{"lineIndex", "textStart", "textEnd", "fragmentStart", "fragmentEnd", "advanceWidth", "lineHeight", "baseline"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("text-line %q missing field %q", event.Label, field)
				}
			}
			for _, field := range []string{"advanceWidth", "lineHeight", "baseline"} {
				if _, err := asF32Bits(event.Fields[field]); err != nil {
					t.Fatalf("text-line %q field %q is not a valid f32 bit pattern: %v", event.Label, field, err)
				}
			}
			perCase[event.Label]["text-line"]++
		case "text-run":
			for _, field := range []string{"lineIndex", "fontId", "fontSize", "xStart", "xEnd", "glyphCount"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("text-run %q missing field %q", event.Label, field)
				}
			}
			fontID, ok := event.Fields["fontId"].(float64)
			if !ok || fontID < float64(CanonicalFontIDBit) {
				t.Fatalf("text-run %q fontId = %v, want >= 2^63", event.Label, event.Fields["fontId"])
			}
			start, errS := asF32Bits(event.Fields["xStart"])
			end, errE := asF32Bits(event.Fields["xEnd"])
			if errS != nil || errE != nil {
				t.Fatalf("text-run %q x range is not valid f32 bit patterns: %v / %v", event.Label, errS, errE)
			}
			if start > end {
				t.Errorf("text-run %q xStart %q > xEnd %q", event.Label, start, end)
			}
			perCase[event.Label]["text-run"]++
		case "text-cluster":
			for _, field := range []string{"clusterIndex", "start", "end"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("text-cluster %q missing field %q", event.Label, field)
				}
			}
			start, _ := event.Fields["start"].(float64)
			end, _ := event.Fields["end"].(float64)
			if start >= end {
				t.Fatalf("text-cluster %q range = %v..%v, want ordered", event.Label, start, end)
			}
			perCase[event.Label]["text-cluster"]++
		case "text-caret":
			for _, field := range []string{"index", "affinity", "present"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("text-caret %q missing field %q", event.Label, field)
				}
			}
			present, _ := event.Fields["present"].(float64)
			for _, field := range []string{"x", "y", "width", "height"} {
				if _, has := event.Fields[field]; has != (present == 1) {
					t.Fatalf("text-caret %q field %q presence = %v but present = %v", event.Label, field, has, present)
				}
			}
			for _, prefix := range []string{"clusterBefore", "clusterAfter"} {
				clusterPresent, _ := event.Fields[prefix+"Present"].(float64)
				for _, suffix := range []string{"Start", "End"} {
					if _, has := event.Fields[prefix+suffix]; has != (clusterPresent == 1) {
						t.Fatalf("text-caret %q field %q presence = %v but %sPresent = %v", event.Label, prefix+suffix, has, prefix, clusterPresent)
					}
				}
			}
			if event.Label == "latin-basic" {
				x, err := asF32Bits(event.Fields["x"])
				if err != nil {
					t.Fatalf("text-caret latin-basic x is not a valid f32 bit pattern: %v", err)
				}
				latinCaretX = append(latinCaretX, x)
				y, err := asF32Bits(event.Fields["y"])
				if err != nil {
					t.Fatalf("text-caret latin-basic y is not a valid f32 bit pattern: %v", err)
				}
				latinCaretY = append(latinCaretY, y)
			}
			if event.Label == "newline-only" {
				index, _ := event.Fields["index"].(float64)
				if index == 1 {
					y, err := asF32Bits(event.Fields["y"])
					if err != nil {
						t.Fatalf("text-caret newline-only y is not a valid f32 bit pattern: %v", err)
					}
					newlineRow1Y = y
				}
			}
			if event.Label == "grapheme-clusters" {
				index, _ := event.Fields["index"].(float64)
				if index == 33 {
					graphemeBeyondEnd = int(present)
				}
			}
			perCase[event.Label]["text-caret"]++
		case "text-hit":
			for _, field := range []string{"x", "y", "inside", "index", "affinity", "byteIndexPresent", "byteIndex"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("text-hit %q missing field %q", event.Label, field)
				}
			}
			for _, field := range []string{"x", "y"} {
				if _, err := asF32Bits(event.Fields[field]); err != nil {
					t.Fatalf("text-hit %q field %q is not a valid f32 bit pattern: %v", event.Label, field, err)
				}
			}
			inside, _ := event.Fields["inside"].(float64)
			if inside == 1 {
				hitsInside++
			} else {
				hitsOutside++
			}
			perCase[event.Label]["text-hit"]++
		case "text-selection":
			for _, field := range []string{"start", "end", "rectIndex", "rectPresent"} {
				if _, ok := event.Fields[field]; !ok {
					t.Fatalf("text-selection %q missing field %q", event.Label, field)
				}
			}
			rectPresent, _ := event.Fields["rectPresent"].(float64)
			for _, field := range []string{"x", "y", "width", "height"} {
				if _, has := event.Fields[field]; has != (rectPresent == 1) {
					t.Fatalf("text-selection %q field %q presence = %v but rectPresent = %v", event.Label, field, has, rectPresent)
				}
			}
			if rectPresent == 1 {
				for _, field := range []string{"x", "y", "width", "height"} {
					if _, err := asF32Bits(event.Fields[field]); err != nil {
						t.Fatalf("text-selection %q field %q is not a valid f32 bit pattern: %v", event.Label, field, err)
					}
				}
				wantHeight := textGoldenRatioDefaultLineHeight
				if event.Label == "line-height-override" {
					wantHeight = F32Bits(40)
				}
				if got, err := asF32Bits(event.Fields["height"]); err != nil || got != wantHeight {
					t.Errorf("text-selection %q height = %q (%v), want the line height %q", event.Label, got, err, wantHeight)
				}
			}
			perCase[event.Label]["text-selection"]++
		case "case-end":
			caseEnds++
		default:
			t.Fatalf("unexpected event name %q at seq %d", event.Name, event.Seq)
		}
	}

	// The recorded histogram.
	if len(trace.Events) != 339 {
		t.Errorf("event count = %d, want 339", len(trace.Events))
	}
	for name, want := range wantTextGeometryEventCounts {
		if nameCounts[name] != want {
			t.Errorf("event %q count = %d, want %d", name, nameCounts[name], want)
		}
	}
	if len(nameCounts) != len(wantTextGeometryEventCounts) {
		t.Errorf("distinct event names = %d, want %d", len(nameCounts), len(wantTextGeometryEventCounts))
	}

	// All ten cases begin and end, in fixture order.
	if len(caseBeginOrder) != len(wantTextGeometryCaseLabels) {
		t.Errorf("case-begin count = %d, want %d", len(caseBeginOrder), len(wantTextGeometryCaseLabels))
	}
	for i, want := range wantTextGeometryCaseLabels {
		if i < len(caseBeginOrder) && caseBeginOrder[i] != want {
			t.Errorf("case-begin %d = %q, want %q", i, caseBeginOrder[i], want)
		}
	}
	if caseEnds != len(wantTextGeometryCaseLabels) {
		t.Errorf("case-end count = %d, want %d", caseEnds, len(wantTextGeometryCaseLabels))
	}

	// Every case resolved its own descriptor once; the multi-run case
	// resolved two additional run descriptors; the default font resolved
	// once. The default .SystemUIFont and the explicit Segoe UI of the
	// first case intern to the same canonical FontId on this machine
	// (both select the Segoe UI face).
	for _, label := range wantTextGeometryCaseLabels {
		if resolvedByCase[label] != 1 {
			t.Errorf("case %q has %d case font-resolved events, want 1", label, resolvedByCase[label])
		}
	}
	if resolvedRuns != 2 {
		t.Errorf("run font-resolved events = %d, want 2 (multi-run-weights)", resolvedRuns)
	}
	if !defaultResolved {
		t.Errorf("no default font-resolved event")
	}
	if defaultFontID != latinCaseFontID {
		t.Errorf("default-font fontId = %#x, want %#x (.SystemUIFont and Segoe UI intern to the same face)", defaultFontID, latinCaseFontID)
	}

	// Per-case structural invariants: the shape meta counts agree with
	// the recorded events of that case, and every case shaped at least
	// one row (empty and newline-only cases included) with metrics for
	// the case font.
	for _, label := range wantTextGeometryCaseLabels {
		counts := perCase[label]
		if got, want := counts["text-line"], int(counts["meta-lineCount"]); got != want {
			t.Errorf("case %q: %d text-line events but lineCount = %d", label, got, want)
		}
		if got, want := counts["text-run"], int(counts["meta-fragmentCount"]); got != want {
			t.Errorf("case %q: %d text-run events but fragmentCount = %d", label, got, want)
		}
		if got, want := counts["text-cluster"], int(counts["meta-clusterCount"]); got != want {
			t.Errorf("case %q: %d text-cluster events but clusterCount = %d", label, got, want)
		}
		if counts["font-metrics"] < 1 {
			t.Errorf("case %q has no font-metrics event", label)
		}
	}

	// Caret geometry of the basic Latin case: 13 boundary queries (12
	// clusters plus the end), strictly advancing and monotonically
	// non-decreasing in x on one row.
	if want := 13; len(latinCaretX) != want {
		t.Fatalf("latin-basic text-caret count = %d, want %d", len(latinCaretX), want)
	}
	for i := 1; i < len(latinCaretX); i++ {
		if latinCaretX[i-1] > latinCaretX[i] {
			t.Errorf("latin-basic caret x is not monotonic at %d: %q > %q", i, latinCaretX[i-1], latinCaretX[i])
		}
	}
	for i, y := range latinCaretY {
		if y != latinCaretY[0] {
			t.Errorf("latin-basic caret %d y = %q, want %q (single row)", i, y, latinCaretY[0])
		}
	}

	// The caret after the trailing newline sits on the second row.
	if newlineRow1Y == "" {
		t.Fatalf("newline-only has no text-caret at index 1")
	}
	if newlineRow1Y != textGoldenRatioDefaultLineHeight {
		t.Errorf("newline-only caret at 1 y = %q, want %q (one line height down)", newlineRow1Y, textGoldenRatioDefaultLineHeight)
	}

	// The caret beyond the text end has no bounds.
	if graphemeBeyondEnd != 0 {
		t.Errorf("grapheme-clusters caret at 33 present = %d, want 0 (beyond the 32-byte text)", graphemeBeyondEnd)
	}

	// Hit tests cover both outcomes: points inside a visual row and
	// points outside (beyond the line end, above or below the rows).
	if hitsInside == 0 {
		t.Errorf("no text-hit event with inside = 1")
	}
	if hitsOutside == 0 {
		t.Errorf("no text-hit event with inside = 0")
	}

	// Selection rectangles: the wrapping case spans two rows; the
	// empty-string case records the empty result.
	wrapRects := 0
	for _, event := range trace.Events {
		if event.Name == "text-selection" && event.Label == "wrap-narrow" {
			if event.Fields["start"] != float64(4) || event.Fields["end"] != float64(16) {
				t.Errorf("wrap-narrow selection range = %v..%v, want 4..16", event.Fields["start"], event.Fields["end"])
			}
			if event.Fields["rectPresent"] == float64(1) {
				wrapRects++
			}
		}
	}
	if wrapRects != 2 {
		t.Errorf("wrap-narrow selection rects = %d, want 2 (multi-row selection)", wrapRects)
	}
	emptySelection, ok := firstEvent(&trace, "text-selection", "empty-string")
	if !ok {
		t.Fatalf("missing text-selection event for empty-string")
	}
	if emptySelection.Fields["rectPresent"] != float64(0) {
		t.Errorf("empty-string selection rectPresent = %v, want 0", emptySelection.Fields["rectPresent"])
	}

	// Exact self-comparison must hold.
	if result := CompareTraces(&trace, &trace); !result.Equal {
		t.Fatalf("self comparison failed: %s", result.Diff)
	}
}
