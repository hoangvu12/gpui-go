package portfixture

import (
	"runtime"
	"testing"

	"gpui-go/conformance"
)

const (
	// textGeometryFixturePath is the fx-0004 envelope this gate executes.
	textGeometryFixturePath = "../../conformance/fixtures/fx-0004-text-geometry.json"
	// textGeometryRecordedTracePath is the oracle recorded from the
	// pinned reference harness.
	textGeometryRecordedTracePath = "../../conformance/recorded/fx-0004-text-geometry/trace.json"
)

// TestFx0004TextGeometryMatchesRecordedReference is the ticket09 gate
// for the text-geometry fixture: the port runner must reproduce the
// FULL recorded reference trace (all 339 events: the construction
// record with the catalog shape, the default font resolution, every
// case's font resolutions with the same canonical FontIds, the shaping
// summaries, font metrics, visual lines, paint fragments, grapheme
// clusters, caret bounds, hit tests and selection rectangles — every
// f32 bit pattern, every FontId, every affinity) through the real gpui
// text adapter over the native Parley text service.
func TestFx0004TextGeometryMatchesRecordedReference(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native text artifact is Windows AMD64; the text geometry cannot run on %s", runtime.GOOS)
	}
	envelope, err := conformance.LoadEnvelope(textGeometryFixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, err := RunTextGeometry(envelope)
	if err != nil {
		t.Fatalf("running port text geometry: %v", err)
	}

	// Run metadata: pin the envelope bytes this run executed.
	sha, err := conformance.SHA256Envelope(textGeometryFixturePath)
	if err != nil {
		t.Fatalf("hashing envelope: %v", err)
	}
	trace.EnvelopeSHA256 = sha

	reference, err := conformance.LoadTrace(textGeometryRecordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}

	result := conformance.CompareTraces(reference, trace)
	if !result.Equal {
		t.Fatalf("text geometry trace mismatch:\n%s", result.Diff)
	}
	if result.Diff != "" {
		t.Fatalf("equal comparison must report an empty diff, got %q", result.Diff)
	}
	if got, want := len(trace.Events), len(reference.Events); got != want {
		t.Errorf("event count = %d, want %d", got, want)
	}
	if got, want := len(trace.Events), 339; got != want {
		t.Errorf("event count = %d, want %d (the recorded fx-0004 trace)", got, want)
	}
}

// TestFx0004TextGeometryBehavioralFacts pins the observable text
// behaviors directly from the port run, independent of the file-based
// comparison: the event-name histogram, the golden-ratio default line
// height and the override case, the catalog family count, the canonical
// FontId of the default font, the upstream affinity at the first
// cluster's right half, the grapheme cluster structure, the wrapped row
// coverage, the clamped last row and the trailing-newline selection
// extension.
func TestFx0004TextGeometryBehavioralFacts(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native text artifact is Windows AMD64; the text geometry cannot run on %s", runtime.GOOS)
	}
	envelope, err := conformance.LoadEnvelope(textGeometryFixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, err := RunTextGeometry(envelope)
	if err != nil {
		t.Fatalf("running port text geometry: %v", err)
	}

	// The recorded event-name histogram of fx-0004.
	wantCounts := map[string]int{
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
	counts := map[string]int{}
	for _, event := range trace.Events {
		counts[event.Name]++
	}
	for name, want := range wantCounts {
		if counts[name] != want {
			t.Errorf("event %q count = %d, want %d", name, counts[name], want)
		}
	}
	if len(counts) != len(wantCounts) {
		t.Errorf("distinct event names = %d, want %d", len(counts), len(wantCounts))
	}

	// The construction record: the pinned platform path with the system
	// font family, the service fallback chain, a nonempty catalog and
	// the probe families the fixture depends on.
	var begin *conformance.TraceEvent
	for i := range trace.Events {
		if trace.Events[i].Name == "text-system-begin" {
			begin = &trace.Events[i]
			break
		}
	}
	if begin == nil {
		t.Fatalf("no text-system-begin event")
	}
	if got, _ := begin.Fields["systemFontFamily"].(string); got != "Segoe UI" {
		t.Errorf("text-system-begin systemFontFamily = %q, want Segoe UI", got)
	}
	if got, _ := begin.Fields["fallbackFamilies"].(string); got != "Lilex, IBM Plex Sans, Arial" {
		t.Errorf("text-system-begin fallbackFamilies = %q, want the pinned service fallback chain", got)
	}
	familyCount, _ := begin.Fields["fontFamilyCount"].(uint64)
	if familyCount == 0 {
		t.Errorf("text-system-begin fontFamilyCount = %v, want a real system catalog", begin.Fields["fontFamilyCount"])
	}
	for _, probe := range []string{"hasSegoeUI", "hasSegoeUIEmoji", "hasArial"} {
		if got, _ := begin.Fields[probe].(uint64); got != 1 {
			t.Errorf("text-system-begin %s = %v, want 1 (the fixture depends on the family)", probe, begin.Fields[probe])
		}
	}
	if got, _ := begin.Fields["fontGeneration"].(uint64); got != 0 {
		t.Errorf("text-system-begin fontGeneration = %v, want 0 (the registration-batch counter)", got)
	}

	// The default font resolves to a canonical id and shares the first
	// case's id (both select the Segoe UI face).
	var defaultFontID, latinFontID float64
	for _, event := range trace.Events {
		if event.Name != "font-resolved" {
			continue
		}
		id, _ := event.Fields["fontId"].(float64)
		if event.Label == "default-font" {
			defaultFontID = id
			if got, _ := event.Fields["family"].(string); got != ".SystemUIFont" {
				t.Errorf("default-font family = %q, want .SystemUIFont", got)
			}
		}
		if event.Label == "latin-basic" {
			latinFontID = id
		}
	}
	if defaultFontID != float64(0x8000000000000000) {
		t.Errorf("default-font fontId = %v, want %v (the first interned face)", defaultFontID, float64(0x8000000000000000))
	}
	if defaultFontID != latinFontID {
		t.Errorf("default-font fontId = %v, want %v (.SystemUIFont and Segoe UI intern to the same face)", defaultFontID, latinFontID)
	}

	// The golden-ratio default line height in every case-begin that has
	// no override; the override case records its own.
	for _, event := range trace.Events {
		if event.Name != "case-begin" {
			continue
		}
		want := "f32:41CF1BBD"
		if event.Label == "line-height-override" {
			want = conformance.F32Bits(40)
		}
		if got, _ := event.Fields["lineHeight"].(string); got != want {
			t.Errorf("case-begin %q lineHeight = %q, want %q", event.Label, got, want)
		}
	}

	// The grapheme case records the four graphemes: a+U+0301, e+U+0301,
	// the space and the 25-byte ZWJ family.
	var graphemeClusters [][2]uint64
	for _, event := range trace.Events {
		if event.Name == "text-cluster" && event.Label == "grapheme-clusters" {
			start, _ := event.Fields["start"].(uint64)
			end, _ := event.Fields["end"].(uint64)
			graphemeClusters = append(graphemeClusters, [2]uint64{start, end})
		}
	}
	wantClusters := [][2]uint64{{0, 3}, {3, 6}, {6, 7}, {7, 32}}
	if len(graphemeClusters) != len(wantClusters) {
		t.Fatalf("grapheme-clusters cluster count = %d, want %d", len(graphemeClusters), len(wantClusters))
	}
	for i, want := range wantClusters {
		if graphemeClusters[i] != want {
			t.Errorf("grapheme-clusters cluster %d = %v, want %v", i, graphemeClusters[i], want)
		}
	}

	// The wrapped case covers the text with contiguous rows.
	var wrapStarts, wrapEnds []uint64
	for _, event := range trace.Events {
		if event.Name == "text-line" && event.Label == "wrap-narrow" {
			wrapStarts = append(wrapStarts, event.Fields["textStart"].(uint64))
			wrapEnds = append(wrapEnds, event.Fields["textEnd"].(uint64))
		}
	}
	if len(wrapStarts) != 3 {
		t.Fatalf("wrap-narrow row count = %d, want 3", len(wrapStarts))
	}
	for i, start := range wrapStarts {
		if i > 0 && start != wrapEnds[i-1] {
			t.Errorf("wrap-narrow row %d starts at %d, want the previous row's end %d", i, start, wrapEnds[i-1])
		}
	}
	if wrapEnds[len(wrapEnds)-1] != 19 {
		t.Errorf("wrap-narrow last row ends at %d, want 19 (the text length)", wrapEnds[len(wrapEnds)-1])
	}

	// The clamped case holds every remaining cluster on its unbounded
	// last row (the clamp drops the wrap constraint past the limit).
	var clampLastStart, clampLastEnd uint64
	var clampLineCount uint64
	for _, event := range trace.Events {
		if event.Label != "wrap-clamp" {
			continue
		}
		switch event.Name {
		case "text-shape-meta":
			clampLineCount, _ = event.Fields["lineCount"].(uint64)
		case "text-line":
			if index, _ := event.Fields["lineIndex"].(uint64); index == 1 {
				clampLastStart, _ = event.Fields["textStart"].(uint64)
				clampLastEnd, _ = event.Fields["textEnd"].(uint64)
			}
		}
	}
	if clampLineCount != 2 {
		t.Errorf("wrap-clamp lineCount = %d, want 2 (the clamp)", clampLineCount)
	}
	if clampLastStart != 8 || clampLastEnd != 48 {
		t.Errorf("wrap-clamp last row = %d..%d, want 8..48 (the clamped last row holds the remaining text)", clampLastStart, clampLastEnd)
	}

	// The trailing-newline selection extends onto the separator's row.
	var newlineRects []conformance.TraceEvent
	for _, event := range trace.Events {
		if event.Name == "text-selection" && event.Label == "newline-only" {
			newlineRects = append(newlineRects, event)
		}
	}
	if len(newlineRects) != 1 {
		t.Fatalf("newline-only selection rects = %d, want 1 (the trailing-newline extension)", len(newlineRects))
	}
	rect := newlineRects[0]
	if got, _ := rect.Fields["rectPresent"].(uint64); got != 1 {
		t.Fatalf("newline-only rectPresent = %v, want 1", rect.Fields["rectPresent"])
	}
	if got, _ := rect.Fields["height"].(string); got != "f32:41CF1BBD" {
		t.Errorf("newline-only selection height = %q, want the line height", got)
	}
	if got, _ := rect.Fields["y"].(string); got != conformance.F32Bits(0) {
		t.Errorf("newline-only selection y = %q, want 0 (the separator's row)", got)
	}

	// The empty-string case records the empty selection result.
	for _, event := range trace.Events {
		if event.Name == "text-selection" && event.Label == "empty-string" {
			if got, _ := event.Fields["rectPresent"].(uint64); got != 0 {
				t.Errorf("empty-string rectPresent = %v, want 0", event.Fields["rectPresent"])
			}
		}
	}

	// Hit tests cover both outcomes.
	hitsInside, hitsOutside := 0, 0
	for _, event := range trace.Events {
		if event.Name != "text-hit" {
			continue
		}
		if got, _ := event.Fields["inside"].(uint64); got == 1 {
			hitsInside++
		} else {
			hitsOutside++
		}
	}
	if hitsInside == 0 || hitsOutside == 0 {
		t.Errorf("hit outcomes = %d inside / %d outside, want both nonzero", hitsInside, hitsOutside)
	}

	// The upstream affinity at (10.25, 3) in the latin case: the point
	// sits in the right half of the first cluster, so the closest caret
	// is the cluster end with upstream affinity and the byte-index hit
	// test reports the cluster start.
	for _, event := range trace.Events {
		if event.Name != "text-hit" || event.Label != "latin-basic" {
			continue
		}
		if got, _ := event.Fields["x"].(string); got != conformance.F32Bits(10.25) {
			continue
		}
		if got, _ := event.Fields["affinity"].(string); got != "upstream" {
			t.Errorf("latin-basic hit at (10.25, 3) affinity = %q, want upstream", got)
		}
		if got, _ := event.Fields["index"].(uint64); got != 1 {
			t.Errorf("latin-basic hit at (10.25, 3) index = %d, want 1 (the cluster end)", got)
		}
		if got, _ := event.Fields["byteIndex"].(uint64); got != 0 {
			t.Errorf("latin-basic hit at (10.25, 3) byteIndex = %d, want 0 (the logical cluster start under the point)", got)
		}
	}
}
