//go:build windows

// Package textspec holds ticket09's text adapter tests: real font
// resolution, shaping and query geometry through the public gpui API
// against the embedded native Parley/Fontique text stack. They exercise
// the text system from outside the runtime package, matching the repo
// convention (see internal/layoutspec and internal/taskspec).
//
// Environment expectation: the native text artifact is Windows AMD64
// (loaded from the embedded bytes, materialized into the per-user cache
// by internal/native); this machine is expected to run these tests
// interactively. The fx-0004 conformance gate lives in
// internal/portfixture; this package pins the adapter behaviors the
// fixture does not reach (handle lifecycle, capacity, GC, error
// mapping) and re-asserts the recorded observations the gate depends
// on, directly through the public API.
//
// FontId determinism: the native FontStore interns faces in resolution
// order, so the canonical ids these tests pin (the recorded trace's
// ids) hold as long as each test resolves and shapes in the recorded
// fixture's order. The tests below are written so every test is
// self-contained in that order (the Segoe UI regular face is always the
// first interned face of a test process, exactly as in the recorded
// trace, where the default .SystemUIFont resolve and every case font
// select it).
package textspec

import (
	"errors"
	"math"
	"runtime"
	"testing"

	"gpui-go/conformance"
	"gpui-go/gpui"
)

// The recorded fx-0004 observations these tests pin
// (conformance/recorded/fx-0004-text-geometry/trace.json).
const (
	// wantDefaultFontID is the canonical FontId of the pinned default
	// font (.SystemUIFont, which selects the Segoe UI face on this
	// machine) — the first interned face of the trace.
	wantDefaultFontID = uint64(0x8000000000000000)
	// wantEmojiFragmentFontID is the canonical FontId Parley selects for
	// the ZWJ family in the grapheme case: the second interned face.
	wantEmojiFragmentFontID = uint64(0x8000000000000001)
	// wantLatinHitXBits is the recorded x of the (10.25, 3) hit point.
	wantLatinHitXBits = "f32:41240000"
	// wantNewlineSelectionWidthBits is the recorded width of the
	// trailing-newline selection rectangle (5.3203125).
	wantNewlineSelectionWidthBits = "f32:40AA4000"
	// wantSegoeAscentBits / wantSegoeDescentBits are the recorded
	// document ascent/descent of Segoe UI at 16px.
	wantSegoeAscentBits  = "f32:418A2000"
	wantSegoeDescentBits = "f32:40808000"
)

// textSystem returns the process-global text system.
func textSystem(t *testing.T) *gpui.TextSystem {
	t.Helper()
	system, err := gpui.DefaultTextSystem()
	if err != nil {
		t.Fatalf("gpui.DefaultTextSystem: %v", err)
	}
	return system
}

// segoeUI returns the fixture's standard case descriptor.
func segoeUI() gpui.FontDescriptor {
	return gpui.FontDescriptor{Family: "Segoe UI", Weight: 400, Style: gpui.FontStyleNormal}
}

// shapeText shapes one single-run document and disposes it at cleanup.
func shapeText(t *testing.T, system *gpui.TextSystem, text string, wrap *float32, clamp *uint32) *gpui.WrappedLine {
	t.Helper()
	line, err := system.ShapeText(gpui.TextLayoutRequest{
		Text:      text,
		FontSize:  16,
		Runs:      []gpui.TextRun{{Len: len(text), Font: segoeUI()}},
		WrapWidth: wrap,
		LineClamp: clamp,
	})
	if err != nil {
		t.Fatalf("ShapeText(%q): %v", text, err)
	}
	t.Cleanup(func() { _ = line.Dispose() })
	return line
}

// defaultLineHeight is the reference default line height at 16px.
func defaultLineHeight() float32 { return gpui.DefaultLineHeight(16) }

// TestResolveFontDeterminism pins the recorded resolve identity: the
// pinned default font (.SystemUIFont) and an explicit Segoe UI regular
// descriptor intern to the same canonical FontId (the first interned
// face, bit 63 set, store index 0), and repeated resolves of the same
// descriptor yield the same id (the store only interns).
func TestResolveFontDeterminism(t *testing.T) {
	system := textSystem(t)

	defaultFont, err := system.ResolveFont(gpui.DefaultFontDescriptor())
	if err != nil {
		t.Fatalf("resolving the default font: %v", err)
	}
	if got := uint64(defaultFont.FontID); got != wantDefaultFontID {
		t.Errorf("default font FontID = %#x, want %#x (the recorded trace's first interned face)", got, wantDefaultFontID)
	}
	if !defaultFont.FontID.IsCanonical() {
		t.Errorf("default font FontID %#x lacks the canonical bit", uint64(defaultFont.FontID))
	}
	if defaultFont.Generation != 0 {
		t.Errorf("default font generation = %d, want 0 (the registration-batch counter)", defaultFont.Generation)
	}
	if defaultFont.Weight != 400 || defaultFont.Style != gpui.FontStyleNormal {
		t.Errorf("default font echo = %v/%v, want 400/Normal", defaultFont.Weight, defaultFont.Style)
	}

	for i := 0; i < 2; i++ {
		identity, err := system.ResolveFont(segoeUI())
		if err != nil {
			t.Fatalf("resolve %d: %v", i, err)
		}
		if got := uint64(identity.FontID); got != wantDefaultFontID {
			t.Errorf("resolve %d FontID = %#x, want %#x (.SystemUIFont and Segoe UI intern to the same face)", i, got, wantDefaultFontID)
		}
	}
}

// TestShapeLatinBasic pins the recorded shape facts of the basic Latin
// case: one row, one fragment, one glyph per character, the recorded
// ascent/descent bits, and the golden-ratio default line height's
// baseline derivation.
func TestShapeLatinBasic(t *testing.T) {
	system := textSystem(t)
	line := shapeText(t, system, "Hello, gpui!", nil, nil)
	lineHeight := defaultLineHeight()

	summary, err := line.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.TextLen != 12 || summary.LineCount != 1 || summary.FragmentCount != 1 || summary.GlyphCount != 12 {
		t.Fatalf("summary = %+v, want 12 bytes / 1 line / 1 fragment / 12 glyphs", summary)
	}
	if got := conformance.F32Bits(summary.Ascent); got != wantSegoeAscentBits {
		t.Errorf("ascent = %q, want %q", got, wantSegoeAscentBits)
	}
	if got := conformance.F32Bits(summary.Descent); got != wantSegoeDescentBits {
		t.Errorf("descent = %q, want %q", got, wantSegoeDescentBits)
	}
	if summary.Width <= 0 {
		t.Errorf("width = %v, want the shaped advance", summary.Width)
	}

	row, err := line.Line(0, lineHeight)
	if err != nil {
		t.Fatalf("Line(0): %v", err)
	}
	if row.TextStart != 0 || row.TextEnd != 12 || row.FragmentStart != 0 || row.FragmentEnd != 1 {
		t.Errorf("row 0 = %+v, want the whole text with the single fragment", row)
	}
	if row.AdvanceWidth != summary.Width {
		t.Errorf("row advance = %v, want the summary width %v", row.AdvanceWidth, summary.Width)
	}
	// The pinned baseline derivation: (lh - ascent - descent)/2 + ascent
	// in f32 — the native computes it; re-derive to confirm.
	wantBaseline := (lineHeight-summary.Ascent-summary.Descent)/2.0 + summary.Ascent
	if row.Baseline != wantBaseline {
		t.Errorf("baseline = %v (%s), want %v (%s)", row.Baseline, conformance.F32Bits(row.Baseline), wantBaseline, conformance.F32Bits(wantBaseline))
	}
	if row.LineHeight != lineHeight {
		t.Errorf("row lineHeight = %v, want the query's %v", row.LineHeight, lineHeight)
	}

	// The fragment carries the case font's canonical id.
	fragments, err := line.Fragments()
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	if len(fragments) != 1 {
		t.Fatalf("fragments = %d, want 1", len(fragments))
	}
	if got := uint64(fragments[0].FontID); got != wantDefaultFontID {
		t.Errorf("fragment font = %#x, want %#x", got, wantDefaultFontID)
	}
	if fragments[0].XStart != 0 || fragments[0].XEnd != summary.Width || fragments[0].GlyphCount != 12 {
		t.Errorf("fragment = %+v, want x [0, width) with 12 glyphs", fragments[0])
	}
}

// TestHitUpstreamAffinityAtClusterRightHalf pins the recorded (10.25, 3)
// hit of the latin case: the point sits in the right half of the first
// cluster, so the closest caret is the cluster END with upstream
// affinity, and the byte-index hit test reports the logical cluster
// START under the point.
func TestHitUpstreamAffinityAtClusterRightHalf(t *testing.T) {
	system := textSystem(t)
	line := shapeText(t, system, "Hello, gpui!", nil, nil)
	lineHeight := defaultLineHeight()

	hit, err := line.HitTest(10.25, 3, lineHeight)
	if err != nil {
		t.Fatalf("HitTest(10.25, 3): %v", err)
	}
	if !hit.Inside {
		t.Fatalf("HitTest(10.25, 3) inside = false, want true")
	}
	if hit.Index != 1 {
		t.Errorf("HitTest(10.25, 3) index = %d, want 1 (the first cluster's end)", hit.Index)
	}
	if hit.Affinity != gpui.CaretAffinityUpstream {
		t.Errorf("HitTest(10.25, 3) affinity = %v, want upstream", hit.Affinity)
	}

	byteIndex, present, err := line.ByteIndexForPixelPoint(10.25, 3, lineHeight)
	if err != nil {
		t.Fatalf("ByteIndexForPixelPoint(10.25, 3): %v", err)
	}
	if !present {
		t.Fatalf("ByteIndexForPixelPoint(10.25, 3) present = false, want true")
	}
	if byteIndex != 0 {
		t.Errorf("ByteIndexForPixelPoint(10.25, 3) = %d, want 0 (the logical cluster start under the point)", byteIndex)
	}

	// The mid-cluster caret snaps inside the cluster: the recorded query
	// at index 1 (the boundary inside "a"+U+0301 of the grapheme case
	// uses the same rule; here the plain Latin boundary 1 is a cluster
	// boundary).
	caret, err := line.Caret(1, gpui.CaretAffinityDownstream, lineHeight)
	if err != nil {
		t.Fatalf("Caret(1): %v", err)
	}
	if !caret.Present || caret.Bounds.X == 0 {
		t.Fatalf("Caret(1) = %+v, want present with a nonzero x", caret)
	}
	if caret.ClusterBefore == nil || caret.ClusterBefore.Start != 0 || caret.ClusterBefore.End != 1 {
		t.Errorf("Caret(1) clusterBefore = %+v, want [0, 1)", caret.ClusterBefore)
	}
	if caret.ClusterAfter == nil || caret.ClusterAfter.Start != 1 || caret.ClusterAfter.End != 2 {
		t.Errorf("Caret(1) clusterAfter = %+v, want [1, 2)", caret.ClusterAfter)
	}
}

// TestHitCaretRoundTrip walks every cluster boundary of the latin case:
// the caret x positions advance monotonically, and hitting the midpoint
// of each cluster's x range returns that cluster's start with
// downstream affinity (the pinned Cursor::from_point left-half rule).
func TestHitCaretRoundTrip(t *testing.T) {
	system := textSystem(t)
	line := shapeText(t, system, "Hello, gpui!", nil, nil)
	lineHeight := defaultLineHeight()

	clusters, err := line.Clusters()
	if err != nil {
		t.Fatalf("Clusters: %v", err)
	}
	if len(clusters) != 12 {
		t.Fatalf("clusters = %d, want 12 (one per character)", len(clusters))
	}

	var lastX float32
	for i, cluster := range clusters {
		caret, err := line.Caret(cluster.Start, gpui.CaretAffinityDownstream, lineHeight)
		if err != nil {
			t.Fatalf("Caret(%d): %v", cluster.Start, err)
		}
		if !caret.Present {
			t.Fatalf("Caret(%d) not present", cluster.Start)
		}
		if caret.Bounds.Y != 0 {
			t.Errorf("Caret(%d) y = %v, want 0 (single row)", cluster.Start, caret.Bounds.Y)
		}
		if caret.Bounds.H != lineHeight {
			t.Errorf("Caret(%d) height = %v, want the line height %v", cluster.Start, caret.Bounds.H, lineHeight)
		}
		if caret.Bounds.W != 0 {
			t.Errorf("Caret(%d) width = %v, want a zero-width caret", cluster.Start, caret.Bounds.W)
		}
		if i > 0 && caret.Bounds.X < lastX {
			t.Errorf("caret x regressed at cluster %d: %v < %v", i, caret.Bounds.X, lastX)
		}
		lastX = caret.Bounds.X

		// Round trip: the midpoint of the cluster's advance hits the
		// cluster start (left half => downstream at the start).
		endCaret, err := line.Caret(cluster.End, gpui.CaretAffinityDownstream, lineHeight)
		if err != nil {
			t.Fatalf("Caret(%d): %v", cluster.End, err)
		}
		mid := (caret.Bounds.X + endCaret.Bounds.X) / 2
		hit, err := line.HitTest(mid, lineHeight/2, lineHeight)
		if err != nil {
			t.Fatalf("HitTest(%v, %v): %v", mid, lineHeight/2, err)
		}
		if !hit.Inside {
			t.Fatalf("HitTest(%v, %v) inside = false, want true", mid, lineHeight/2)
		}
		if hit.Index != cluster.Start {
			t.Errorf("HitTest(mid of cluster %d) index = %d, want %d", i, hit.Index, cluster.Start)
		}
		byteIndex, present, err := line.ByteIndexForPixelPoint(mid, lineHeight/2, lineHeight)
		if err != nil {
			t.Fatalf("ByteIndexForPixelPoint(%v, %v): %v", mid, lineHeight/2, err)
		}
		if !present || byteIndex != cluster.Start {
			t.Errorf("ByteIndexForPixelPoint(mid of cluster %d) = (%d, %v), want (%d, true)", i, byteIndex, present, cluster.Start)
		}
	}

	// The end caret is present and the outside hit beyond the row end
	// carries the edge caret.
	endCaret, err := line.Caret(12, gpui.CaretAffinityUpstream, lineHeight)
	if err != nil {
		t.Fatalf("Caret(12): %v", err)
	}
	if !endCaret.Present || endCaret.Bounds.X < lastX {
		t.Errorf("Caret(12) = %+v, want present at or past the final cluster start %v", endCaret, lastX)
	}
	hit, err := line.HitTest(200, 3, lineHeight)
	if err != nil {
		t.Fatalf("HitTest(200, 3): %v", err)
	}
	if hit.Inside {
		t.Errorf("HitTest(200, 3) inside = true, want false (past the row end)")
	}
	if hit.Index != 12 || hit.Affinity != gpui.CaretAffinityUpstream {
		t.Errorf("HitTest(200, 3) = (%d, %v), want (12, upstream) — the recorded edge caret", hit.Index, hit.Affinity)
	}
	byteIndex, present, err := line.ByteIndexForPixelPoint(200, 3, lineHeight)
	if err != nil {
		t.Fatalf("ByteIndexForPixelPoint(200, 3): %v", err)
	}
	if present || byteIndex != 12 {
		t.Errorf("ByteIndexForPixelPoint(200, 3) = (%d, %v), want (12, false) — the boundary at that edge", byteIndex, present)
	}

	// Above and below the rows: outside hits with the edge caret.
	for _, y := range []float32{100, -3} {
		hit, err := line.HitTest(5, y, lineHeight)
		if err != nil {
			t.Fatalf("HitTest(5, %v): %v", y, err)
		}
		if hit.Inside || hit.Index != 0 || hit.Affinity != gpui.CaretAffinityDownstream {
			t.Errorf("HitTest(5, %v) = (%d, %v, inside=%v), want (0, downstream, false) — the recorded edge caret", y, hit.Index, hit.Affinity, hit.Inside)
		}
	}
}

// caretEqual compares two carets by value (the cluster pointers are
// compared by their ranges, not their addresses).
func caretEqual(a, b gpui.TextCaret) bool {
	if a.Index != b.Index || a.Affinity != b.Affinity || a.Present != b.Present || a.Bounds != b.Bounds {
		return false
	}
	if (a.ClusterBefore == nil) != (b.ClusterBefore == nil) || (a.ClusterAfter == nil) != (b.ClusterAfter == nil) {
		return false
	}
	if a.ClusterBefore != nil && *a.ClusterBefore != *b.ClusterBefore {
		return false
	}
	if a.ClusterAfter != nil && *a.ClusterAfter != *b.ClusterAfter {
		return false
	}
	return true
}

// TestGraphemeClustersAndFallbackFont pins the grapheme case's recorded
// facts: the decomposed combining marks and the ZWJ family are single
// logical clusters ([0,3], [3,6], [6,7], [7,32]), the caret beyond the
// text end has no bounds but still reports the adjacent clusters, and
// the family's fragment runs on the emoji face interned right after the
// case font (the second face of the trace).
func TestGraphemeClustersAndFallbackFont(t *testing.T) {
	system := textSystem(t)
	// The fixture's grapheme text: decomposed combining marks (a+U+0301,
	// e+U+0301), a space, then the ZWJ emoji family — 32 bytes.
	text := "a\u0301e\u0301 \U0001F468\u200D\U0001F469\u200D\U0001F467\u200D\U0001F466"
	if len(text) != 32 {
		t.Fatalf("grapheme text length = %d, want 32", len(text))
	}
	line := shapeText(t, system, text, nil, nil)
	lineHeight := defaultLineHeight()

	clusters, err := line.Clusters()
	if err != nil {
		t.Fatalf("Clusters: %v", err)
	}
	want := []gpui.TextRange{{Start: 0, End: 3}, {Start: 3, End: 6}, {Start: 6, End: 7}, {Start: 7, End: 32}}
	if len(clusters) != len(want) {
		t.Fatalf("clusters = %v, want %v", clusters, want)
	}
	for i, cluster := range want {
		if clusters[i] != cluster {
			t.Errorf("cluster %d = %v, want %v", i, clusters[i], cluster)
		}
	}

	// The caret beyond the text end (the recorded query at 33): no
	// bounds, the last cluster before, none after.
	caret, err := line.Caret(33, gpui.CaretAffinityDownstream, lineHeight)
	if err != nil {
		t.Fatalf("Caret(33): %v", err)
	}
	if caret.Present {
		t.Errorf("Caret(33) present = true, want false (beyond the 32-byte text)")
	}
	if caret.Index != 33 || caret.Affinity != gpui.CaretAffinityDownstream {
		t.Errorf("Caret(33) echo = (%d, %v), want (33, downstream)", caret.Index, caret.Affinity)
	}
	if caret.ClusterBefore == nil || *caret.ClusterBefore != (gpui.TextRange{Start: 7, End: 32}) {
		t.Errorf("Caret(33) clusterBefore = %+v, want [7, 32)", caret.ClusterBefore)
	}
	if caret.ClusterAfter != nil {
		t.Errorf("Caret(33) clusterAfter = %+v, want none", caret.ClusterAfter)
	}

	// The hit inside the family (the recorded (30, 4) point): the caret
	// lands on the shaping stop at 18 (downstream) and the byte index
	// matches it.
	hit, err := line.HitTest(30, 4, lineHeight)
	if err != nil {
		t.Fatalf("HitTest(30, 4): %v", err)
	}
	if !hit.Inside || hit.Index != 18 || hit.Affinity != gpui.CaretAffinityDownstream {
		t.Errorf("HitTest(30, 4) = (%d, %v, inside=%v), want (18, downstream, true)", hit.Index, hit.Affinity, hit.Inside)
	}
	byteIndex, present, err := line.ByteIndexForPixelPoint(30, 4, lineHeight)
	if err != nil {
		t.Fatalf("ByteIndexForPixelPoint(30, 4): %v", err)
	}
	if !present || byteIndex != 18 {
		t.Errorf("ByteIndexForPixelPoint(30, 4) = (%d, %v), want (18, true)", byteIndex, present)
	}

	// Character-level fallback: the family's fragment runs on the emoji
	// face, interned directly after the case font.
	fragments, err := line.Fragments()
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	if len(fragments) != 2 {
		t.Fatalf("fragments = %d, want 2 (the Latin prefix and the family)", len(fragments))
	}
	if got := uint64(fragments[1].FontID); got != wantEmojiFragmentFontID {
		t.Errorf("family fragment font = %#x, want %#x (the emoji face interned after the case font)", got, wantEmojiFragmentFontID)
	}
	// The fallback face's metrics resolve through the service (fragment
	// font ids are registered at shape time).
	metrics, err := system.FontMetrics(fragments[1].FontID)
	if err != nil {
		t.Fatalf("FontMetrics(emoji face): %v", err)
	}
	if metrics.UnitsPerEm == 0 || metrics.Ascent <= 0 {
		t.Errorf("emoji metrics = %+v, want a real face record", metrics)
	}
	// The pixel scaling is the pinned (metric / upem) * size arithmetic.
	wantAscentPx := (metrics.Ascent / float32(metrics.UnitsPerEm)) * 16
	if metrics.AscentPx(16) != wantAscentPx {
		t.Errorf("AscentPx(16) = %v, want %v", metrics.AscentPx(16), wantAscentPx)
	}
}

// TestResolveDistinctFacesAreDistinctInterns pins that descriptors that
// select different faces intern to distinct canonical ids: bold, italic
// and the regular face of one family are three store entries. (The exact
// indices depend on the process's interning order — the recorded trace
// interns them after the emoji and CJK fallback faces — so this test only
// pins distinctness, not specific ids.)
func TestResolveDistinctFacesAreDistinctInterns(t *testing.T) {
	system := textSystem(t)

	regular, err := system.ResolveFont(segoeUI())
	if err != nil {
		t.Fatalf("resolving regular: %v", err)
	}
	bold := segoeUI()
	bold.Weight = 700
	boldIdentity, err := system.ResolveFont(bold)
	if err != nil {
		t.Fatalf("resolving bold: %v", err)
	}
	italic := segoeUI()
	italic.Style = gpui.FontStyleItalic
	italicIdentity, err := system.ResolveFont(italic)
	if err != nil {
		t.Fatalf("resolving italic: %v", err)
	}
	ids := []gpui.FontID{regular.FontID, boldIdentity.FontID, italicIdentity.FontID}
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[i] == ids[j] {
				t.Errorf("faces %d and %d share FontID %#x, want distinct interns", i, j, uint64(ids[i]))
			}
		}
	}
	if got := uint64(regular.FontID); got != wantDefaultFontID {
		t.Errorf("regular FontID = %#x, want the stable %#x", got, wantDefaultFontID)
	}
}

// TestEmptyStringEdgeCase pins the recorded empty-string case: one empty
// row, no fragments, no clusters, a present zero-width caret at the
// origin with the line height, the empty selection producing no
// rectangles, and every hit test reporting the edge caret.
func TestEmptyStringEdgeCase(t *testing.T) {
	system := textSystem(t)
	line := shapeText(t, system, "", nil, nil)
	lineHeight := defaultLineHeight()

	summary, err := line.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.TextLen != 0 || summary.LineCount != 1 || summary.FragmentCount != 0 || summary.GlyphCount != 0 {
		t.Fatalf("summary = %+v, want 0 bytes / 1 line / 0 fragments / 0 glyphs", summary)
	}
	if summary.Width != 0 {
		t.Errorf("width = %v, want 0", summary.Width)
	}

	row, err := line.Line(0, lineHeight)
	if err != nil {
		t.Fatalf("Line(0): %v", err)
	}
	if row.TextStart != 0 || row.TextEnd != 0 || row.FragmentStart != 0 || row.FragmentEnd != 0 || row.AdvanceWidth != 0 {
		t.Errorf("row 0 = %+v, want the empty row", row)
	}

	clusters, err := line.Clusters()
	if err != nil {
		t.Fatalf("Clusters: %v", err)
	}
	if len(clusters) != 0 {
		t.Errorf("clusters = %v, want none", clusters)
	}

	caret, err := line.Caret(0, gpui.CaretAffinityDownstream, lineHeight)
	if err != nil {
		t.Fatalf("Caret(0): %v", err)
	}
	if !caret.Present {
		t.Fatalf("Caret(0) present = false, want the empty-layout caret")
	}
	if caret.Bounds != (gpui.TextRect{X: 0, Y: 0, W: 0, H: lineHeight}) {
		t.Errorf("Caret(0) bounds = %+v, want the zero-width origin caret with the line height", caret.Bounds)
	}
	if caret.ClusterBefore != nil || caret.ClusterAfter != nil {
		t.Errorf("Caret(0) clusters = %+v/%+v, want none", caret.ClusterBefore, caret.ClusterAfter)
	}

	rects, err := line.SelectionRects(0, 0, lineHeight)
	if err != nil {
		t.Fatalf("SelectionRects(0, 0): %v", err)
	}
	if len(rects) != 0 {
		t.Errorf("empty selection rects = %v, want none", rects)
	}

	hit, err := line.HitTest(0, 2, lineHeight)
	if err != nil {
		t.Fatalf("HitTest(0, 2): %v", err)
	}
	if hit.Inside || hit.Index != 0 || hit.Affinity != gpui.CaretAffinityDownstream {
		t.Errorf("HitTest(0, 2) = (%d, %v, inside=%v), want (0, downstream, false)", hit.Index, hit.Affinity, hit.Inside)
	}
}

// TestNewlineOnlyEdgeCase pins the recorded newline-only case: two rows
// with the separator byte kept in the first row's range, the caret after
// the newline on the second row, and the selection over the separator
// extending onto its row with the recorded width.
func TestNewlineOnlyEdgeCase(t *testing.T) {
	system := textSystem(t)
	line := shapeText(t, system, "\n", nil, nil)
	lineHeight := defaultLineHeight()

	summary, err := line.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.LineCount != 2 || summary.FragmentCount != 0 || summary.GlyphCount != 0 {
		t.Fatalf("summary = %+v, want 2 rows / 0 fragments / 0 glyphs", summary)
	}

	row0, err := line.Line(0, lineHeight)
	if err != nil {
		t.Fatalf("Line(0): %v", err)
	}
	if row0.TextStart != 0 || row0.TextEnd != 1 {
		t.Errorf("row 0 = [%d, %d), want [0, 1) (the separator stays in its row)", row0.TextStart, row0.TextEnd)
	}
	row1, err := line.Line(1, lineHeight)
	if err != nil {
		t.Fatalf("Line(1): %v", err)
	}
	if row1.TextStart != 1 || row1.TextEnd != 1 {
		t.Errorf("row 1 = [%d, %d), want [1, 1)", row1.TextStart, row1.TextEnd)
	}

	// The caret after the newline sits one row down.
	caret, err := line.Caret(1, gpui.CaretAffinityDownstream, lineHeight)
	if err != nil {
		t.Fatalf("Caret(1): %v", err)
	}
	if !caret.Present || caret.Bounds.Y != lineHeight || caret.Bounds.X != 0 {
		t.Errorf("Caret(1) = %+v, want present at (0, lineHeight)", caret)
	}
	if caret.ClusterBefore == nil || *caret.ClusterBefore != (gpui.TextRange{Start: 0, End: 1}) {
		t.Errorf("Caret(1) clusterBefore = %+v, want [0, 1)", caret.ClusterBefore)
	}
	if caret.ClusterAfter != nil {
		t.Errorf("Caret(1) clusterAfter = %+v, want none (end of text)", caret.ClusterAfter)
	}

	// The trailing-newline selection extends onto the separator's row.
	rects, err := line.SelectionRects(0, 1, lineHeight)
	if err != nil {
		t.Fatalf("SelectionRects(0, 1): %v", err)
	}
	if len(rects) != 1 {
		t.Fatalf("selection rects = %v, want the one trailing-newline rectangle", rects)
	}
	rect := rects[0]
	if conformance.F32Bits(rect.X) != "f32:00000000" || conformance.F32Bits(rect.Y) != "f32:00000000" {
		t.Errorf("selection rect origin = (%s, %s), want (0, 0)", conformance.F32Bits(rect.X), conformance.F32Bits(rect.Y))
	}
	if got := conformance.F32Bits(rect.W); got != wantNewlineSelectionWidthBits {
		t.Errorf("selection rect width = %s, want %s (the recorded separator width)", got, wantNewlineSelectionWidthBits)
	}
	if rect.H != lineHeight {
		t.Errorf("selection rect height = %v, want the line height %v", rect.H, lineHeight)
	}

	// Both hit points (on either row of a zero-advance layout) report
	// the edge carets: index 0 on the first row, index 1 on the second.
	hit, err := line.HitTest(2, 2, lineHeight)
	if err != nil {
		t.Fatalf("HitTest(2, 2): %v", err)
	}
	if hit.Inside || hit.Index != 0 || hit.Affinity != gpui.CaretAffinityDownstream {
		t.Errorf("HitTest(2, 2) = (%d, %v, inside=%v), want (0, downstream, false)", hit.Index, hit.Affinity, hit.Inside)
	}
	hit, err = line.HitTest(2, 30, lineHeight)
	if err != nil {
		t.Fatalf("HitTest(2, 30): %v", err)
	}
	if hit.Inside || hit.Index != 1 || hit.Affinity != gpui.CaretAffinityDownstream {
		t.Errorf("HitTest(2, 30) = (%d, %v, inside=%v), want (1, downstream, false)", hit.Index, hit.Affinity, hit.Inside)
	}
}

// TestWrapAndClamp pins the wrapping case's row coverage and the
// clamped case's recorded facts: a 2-row clamp whose last row holds
// every remaining cluster (the clamp drops the wrap constraint past the
// limit), and the outside hit below the rows reporting the recorded
// edge caret.
func TestWrapAndClamp(t *testing.T) {
	system := textSystem(t)

	// Wrapping: contiguous rows covering the text.
	wrap := float32(70)
	line := shapeText(t, system, "The quick brown fox", &wrap, nil)
	lineHeight := defaultLineHeight()
	summary, err := line.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.LineCount != 3 {
		t.Fatalf("wrapped line count = %d, want 3 (the recorded wrap-narrow rows)", summary.LineCount)
	}
	if summary.WrapWidth == nil || *summary.WrapWidth != wrap {
		t.Errorf("summary wrap = %+v, want %v", summary.WrapWidth, wrap)
	}
	lastEnd := 0
	for i := 0; i < summary.LineCount; i++ {
		row, err := line.Line(i, lineHeight)
		if err != nil {
			t.Fatalf("Line(%d): %v", i, err)
		}
		if row.TextStart != lastEnd {
			t.Errorf("row %d starts at %d, want the previous row's end %d", i, row.TextStart, lastEnd)
		}
		lastEnd = row.TextEnd
	}
	if lastEnd != 19 {
		t.Errorf("last row ends at %d, want 19 (the text length)", lastEnd)
	}

	// Re-layout without the wrap collapses to one row (the same text and
	// runs, new constraint).
	if err := line.Relayout(nil, nil); err != nil {
		t.Fatalf("Relayout: %v", err)
	}
	summary, err = line.Summary()
	if err != nil {
		t.Fatalf("Summary after relayout: %v", err)
	}
	if summary.LineCount != 1 || summary.WrapWidth != nil {
		t.Errorf("relayout summary = %+v, want 1 row without a wrap", summary)
	}
	if err := line.Dispose(); err != nil {
		t.Fatalf("Dispose: %v", err)
	}

	// The clamped case: 48 bytes wrapped at 60px with a 2-row clamp.
	clampText := "one two three four five six seven eight nine ten"
	wrap60 := float32(60)
	clamp := uint32(2)
	clamped := shapeText(t, system, clampText, &wrap60, &clamp)
	summary, err = clamped.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.LineCount != 2 {
		t.Fatalf("clamped line count = %d, want 2 (the clamp)", summary.LineCount)
	}
	lastRow, err := clamped.Line(1, lineHeight)
	if err != nil {
		t.Fatalf("Line(1): %v", err)
	}
	if lastRow.TextStart != 8 || lastRow.TextEnd != 48 {
		t.Errorf("clamped last row = [%d, %d), want [8, 48) (the remaining text on the unbounded row)", lastRow.TextStart, lastRow.TextEnd)
	}
	if lastRow.AdvanceWidth <= wrap60 {
		t.Errorf("clamped last row advance = %v, want beyond the wrap width (the clamp drops it)", lastRow.AdvanceWidth)
	}
	// The selection across the whole clamped text spans both rows.
	rects, err := clamped.SelectionRects(0, 48, lineHeight)
	if err != nil {
		t.Fatalf("SelectionRects(0, 48): %v", err)
	}
	if len(rects) != 2 {
		t.Errorf("clamped selection rects = %d, want 2 (one per row)", len(rects))
	}
	// The hit below the rows reports the recorded edge caret.
	hit, err := clamped.HitTest(5, 80, lineHeight)
	if err != nil {
		t.Fatalf("HitTest(5, 80): %v", err)
	}
	if hit.Inside || hit.Index != 9 || hit.Affinity != gpui.CaretAffinityUpstream {
		t.Errorf("HitTest(5, 80) = (%d, %v, inside=%v), want (9, upstream, false) — the recorded edge caret", hit.Index, hit.Affinity, hit.Inside)
	}
}

// TestMultiRunFragments pins the multi-run case's fragment structure:
// one fragment per styled run in run order, with the resolved bold and
// italic faces distinct from the inherited regular face, and run fonts
// that equal the case font reusing its id.
func TestMultiRunFragments(t *testing.T) {
	system := textSystem(t)
	bold := segoeUI()
	bold.Weight = 700
	italic := segoeUI()
	italic.Style = gpui.FontStyleItalic
	line, err := system.ShapeText(gpui.TextLayoutRequest{
		Text:     "Bold Ital plain",
		FontSize: 16,
		Runs: []gpui.TextRun{
			{Len: 5, Font: bold},
			{Len: 5, Font: italic},
			{Len: 5, Font: segoeUI()},
		},
	})
	if err != nil {
		t.Fatalf("ShapeText: %v", err)
	}
	t.Cleanup(func() { _ = line.Dispose() })

	summary, err := line.Summary()
	if err != nil {
		t.Fatalf("Summary: %v", err)
	}
	if summary.LineCount != 1 || summary.FragmentCount != 3 {
		t.Fatalf("summary = %+v, want 1 row / 3 fragments (one per run)", summary)
	}
	fragments, err := line.Fragments()
	if err != nil {
		t.Fatalf("Fragments: %v", err)
	}
	if len(fragments) != 3 {
		t.Fatalf("fragments = %d, want 3", len(fragments))
	}
	// Run order: bold, italic, then the inherited regular face.
	if fragments[0].FontID == fragments[1].FontID || fragments[1].FontID == fragments[2].FontID || fragments[0].FontID == fragments[2].FontID {
		t.Errorf("fragment fonts = %v, want three distinct faces", []gpui.FontID{fragments[0].FontID, fragments[1].FontID, fragments[2].FontID})
	}
	if got := uint64(fragments[2].FontID); got != wantDefaultFontID {
		t.Errorf("inherited run fragment font = %#x, want %#x (the case face)", got, wantDefaultFontID)
	}
	// The fragment x ranges partition the row.
	if fragments[0].XStart != 0 {
		t.Errorf("fragment 0 xStart = %v, want 0", fragments[0].XStart)
	}
	for i := 1; i < len(fragments); i++ {
		if fragments[i].XStart != fragments[i-1].XEnd {
			t.Errorf("fragment %d xStart = %v, want the previous fragment's end %v", i, fragments[i].XStart, fragments[i-1].XEnd)
		}
	}
	if fragments[2].XEnd != summary.Width {
		t.Errorf("fragment 2 xEnd = %v, want the row width %v", fragments[2].XEnd, summary.Width)
	}

	// The glyphs partition across the fragments and sit inside the row.
	glyphs, err := line.Glyphs()
	if err != nil {
		t.Fatalf("Glyphs: %v", err)
	}
	if len(glyphs) != summary.GlyphCount {
		t.Fatalf("glyphs = %d, want %d", len(glyphs), summary.GlyphCount)
	}
	covered := 0
	for _, fragment := range fragments {
		covered += fragment.GlyphCount
	}
	if covered != summary.GlyphCount {
		t.Errorf("fragment glyph coverage = %d, want %d", covered, summary.GlyphCount)
	}
	for _, glyph := range glyphs {
		if glyph.X < 0 || glyph.X > summary.Width+1 {
			t.Errorf("glyph x = %v, want inside the row [0, %v]", glyph.X, summary.Width)
		}
		if math.IsNaN(float64(glyph.Y)) || math.IsInf(float64(glyph.Y), 0) {
			t.Errorf("glyph y = %v, want finite (baseline-relative)", glyph.Y)
		}
	}
}

// TestLetterSpacingAndFeatures pins the features + letter-spacing case:
// the run's feature settings and letter spacing shape without error and
// the letter-spaced advance exceeds the unspaced one.
func TestLetterSpacingAndFeatures(t *testing.T) {
	system := textSystem(t)
	spacing := float32(1.5)
	features := []gpui.FontFeature{{Tag: "calt", Value: 0}}
	spaced, err := system.ShapeText(gpui.TextLayoutRequest{
		Text:     "office flask ffi",
		FontSize: 16,
		Runs: []gpui.TextRun{{
			Len:           16,
			Font:          segoeUI(),
			LetterSpacing: &spacing,
		}},
	})
	if err != nil {
		t.Fatalf("shaping letter-spaced text: %v", err)
	}
	t.Cleanup(func() { _ = spaced.Dispose() })

	plain := shapeText(t, system, "office flask ffi", nil, nil)
	spacedSummary, err := spaced.Summary()
	if err != nil {
		t.Fatalf("spaced Summary: %v", err)
	}
	plainSummary, err := plain.Summary()
	if err != nil {
		t.Fatalf("plain Summary: %v", err)
	}
	if spacedSummary.Width <= plainSummary.Width {
		t.Errorf("letter-spaced width = %v, want beyond the plain %v", spacedSummary.Width, plainSummary.Width)
	}

	descriptor := segoeUI()
	descriptor.Features = features
	identity, err := system.ResolveFont(descriptor)
	if err != nil {
		t.Fatalf("resolving a feature descriptor: %v", err)
	}
	if !identity.FontID.IsCanonical() || identity.FeatureCount != 1 {
		t.Errorf("feature resolve identity = %+v, want a canonical id echoing the feature", identity)
	}
}

// TestStaleHandleLifecycle pins the handle lifecycle: queries on a
// disposed line fail with the gpui stale-handle sentinel before native
// entry, Dispose is idempotent, and a foreign (malformed) handle is
// rejected by the native service as a bad handle.
func TestStaleHandleLifecycle(t *testing.T) {
	system := textSystem(t)
	line := shapeText(t, system, "Hello, gpui!", nil, nil)
	lineHeight := defaultLineHeight()

	if err := line.Dispose(); err != nil {
		t.Fatalf("Dispose: %v", err)
	}
	if err := line.Dispose(); err != nil {
		t.Fatalf("second Dispose: %v (want idempotent nil)", err)
	}

	if _, err := line.Summary(); !errors.Is(err, gpui.ErrTextStaleHandle) {
		t.Errorf("Summary after dispose = %v, want ErrTextStaleHandle", err)
	}
	if _, err := line.Line(0, lineHeight); !errors.Is(err, gpui.ErrTextStaleHandle) {
		t.Errorf("Line after dispose = %v, want ErrTextStaleHandle", err)
	}
	if _, err := line.Caret(0, gpui.CaretAffinityDownstream, lineHeight); !errors.Is(err, gpui.ErrTextStaleHandle) {
		t.Errorf("Caret after dispose = %v, want ErrTextStaleHandle", err)
	}
	if _, err := line.HitTest(0, 0, lineHeight); !errors.Is(err, gpui.ErrTextStaleHandle) {
		t.Errorf("HitTest after dispose = %v, want ErrTextStaleHandle", err)
	}
	if _, err := line.SelectionRects(0, 1, lineHeight); !errors.Is(err, gpui.ErrTextStaleHandle) {
		t.Errorf("SelectionRects after dispose = %v, want ErrTextStaleHandle", err)
	}
	if _, err := line.Clusters(); !errors.Is(err, gpui.ErrTextStaleHandle) {
		t.Errorf("Clusters after dispose = %v, want ErrTextStaleHandle", err)
	}

	// Font ids are process-stable and have no dispose: resolving still
	// works after the shaping handle is gone, but unknown ids are typed
	// bad-handle errors.
	if _, err := system.FontMetrics(gpui.FontID(1)); !errors.Is(err, gpui.ErrTextBadHandle) {
		t.Errorf("FontMetrics(1) = %v, want ErrTextBadHandle (not canonical)", err)
	}
	identity, err := system.ResolveFont(segoeUI())
	if err != nil {
		t.Fatalf("ResolveFont after dispose: %v", err)
	}
	if got := uint64(identity.FontID); got != wantDefaultFontID {
		t.Errorf("re-resolve FontID = %#x, want the stable %#x", got, wantDefaultFontID)
	}
}

// TestShapingHandleCapacity pins the native live-handle capacity: the
// bound is 256, holding it makes the next shape fail with the
// shaping-limit sentinel before any shaping work, and disposing one
// handle frees its slot.
func TestShapingHandleCapacity(t *testing.T) {
	system := textSystem(t)
	if got, want := system.MaxShapingHandles(), 256; got != want {
		t.Fatalf("MaxShapingHandles = %d, want %d", got, want)
	}

	// Every line shaped so far was disposed at cleanup; hold the full
	// capacity now.
	held := make([]*gpui.WrappedLine, 0, system.MaxShapingHandles())
	defer func() {
		for _, line := range held {
			_ = line.Dispose()
		}
	}()
	for i := 0; i < system.MaxShapingHandles(); i++ {
		line, err := system.ShapeText(gpui.TextLayoutRequest{
			Text:     "capacity",
			FontSize: 16,
			Runs:     []gpui.TextRun{{Len: 8, Font: segoeUI()}},
		})
		if err != nil {
			t.Fatalf("shape %d within the capacity: %v", i, err)
		}
		held = append(held, line)
	}

	_, err := system.ShapeText(gpui.TextLayoutRequest{
		Text:     "capacity",
		FontSize: 16,
		Runs:     []gpui.TextRun{{Len: 8, Font: segoeUI()}},
	})
	if !errors.Is(err, gpui.ErrTextShapingLimit) {
		t.Fatalf("shape past the capacity = %v, want ErrTextShapingLimit", err)
	}

	// Disposing one handle frees its slot.
	if err := held[0].Dispose(); err != nil {
		t.Fatalf("Dispose: %v", err)
	}
	held[0] = held[len(held)-1]
	held = held[:len(held)-1]
	line, err := system.ShapeText(gpui.TextLayoutRequest{
		Text:     "capacity",
		FontSize: 16,
		Runs:     []gpui.TextRun{{Len: 8, Font: segoeUI()}},
	})
	if err != nil {
		t.Fatalf("shape after disposing one handle: %v", err)
	}
	held = append(held, line)
}

// TestForcedGCAroundQueries pins that native shaping handles do not
// follow Go garbage collection: forcing collections between shaping and
// every query leaves the handle valid and the answers identical.
func TestForcedGCAroundQueries(t *testing.T) {
	system := textSystem(t)
	line := shapeText(t, system, "Hello, gpui!", nil, nil)
	lineHeight := defaultLineHeight()

	before, err := line.Caret(6, gpui.CaretAffinityDownstream, lineHeight)
	if err != nil {
		t.Fatalf("Caret(6) before GC: %v", err)
	}
	runtime.GC()
	runtime.GC()

	after, err := line.Caret(6, gpui.CaretAffinityDownstream, lineHeight)
	if err != nil {
		t.Fatalf("Caret(6) after GC: %v", err)
	}
	if !caretEqual(before, after) {
		t.Errorf("caret after GC = %+v, want the pre-GC %+v", after, before)
	}
	summary, err := line.Summary()
	if err != nil {
		t.Fatalf("Summary after GC: %v", err)
	}
	if summary.LineCount != 1 || summary.GlyphCount != 12 {
		t.Errorf("summary after GC = %+v, want the pre-GC facts", summary)
	}
}

// TestShapeValidation pins the adapter's typed validation: runs that do
// not cover the text, non-finite floats, zero clamps and empty run
// lists are rejected with the value sentinel, and a font id past the
// text end beyond the pinned None semantics (a negative index) is a
// caller error.
func TestShapeValidation(t *testing.T) {
	system := textSystem(t)

	_, err := system.ShapeText(gpui.TextLayoutRequest{
		Text:     "hello",
		FontSize: 16,
		Runs:     nil,
	})
	if !errors.Is(err, gpui.ErrTextValue) {
		t.Errorf("no runs = %v, want ErrTextValue", err)
	}

	_, err = system.ShapeText(gpui.TextLayoutRequest{
		Text:     "hello",
		FontSize: 16,
		Runs:     []gpui.TextRun{{Len: 3, Font: segoeUI()}},
	})
	if !errors.Is(err, gpui.ErrTextValue) {
		t.Errorf("undercovering runs = %v, want ErrTextValue (runs must cover the text)", err)
	}

	_, err = system.ShapeText(gpui.TextLayoutRequest{
		Text:     "hello",
		FontSize: 16,
		Runs:     []gpui.TextRun{{Len: 6, Font: segoeUI()}},
	})
	if !errors.Is(err, gpui.ErrTextValue) {
		t.Errorf("overcovering runs = %v, want ErrTextValue", err)
	}

	inf := float32(math.Inf(1))
	_, err = system.ShapeText(gpui.TextLayoutRequest{
		Text:      "hello",
		FontSize:  inf,
		Runs:      []gpui.TextRun{{Len: 5, Font: segoeUI()}},
		WrapWidth: &inf,
		LineClamp: nil,
	})
	if !errors.Is(err, gpui.ErrTextValue) {
		t.Errorf("non-finite float request = %v, want ErrTextValue", err)
	}

	zero := uint32(0)
	_, err = system.ShapeText(gpui.TextLayoutRequest{
		Text:      "hello",
		FontSize:  16,
		Runs:      []gpui.TextRun{{Len: 5, Font: segoeUI()}},
		LineClamp: &zero,
	})
	if !errors.Is(err, gpui.ErrTextValue) {
		t.Errorf("zero clamp = %v, want ErrTextValue", err)
	}

	if _, err := system.ResolveFont(gpui.FontDescriptor{Family: ""}); !errors.Is(err, gpui.ErrTextValue) {
		t.Errorf("empty family resolve = %v, want ErrTextValue", err)
	}

	// Negative caret indices are caller errors; the pinned beyond-end
	// None semantics only cover indices at or past the end.
	line := shapeText(t, system, "hi", nil, nil)
	if _, err := line.Caret(-1, gpui.CaretAffinityDownstream, 16); !errors.Is(err, gpui.ErrTextValue) {
		t.Errorf("Caret(-1) = %v, want ErrTextValue", err)
	}
	// Selection ranges must be ordered and inside the text.
	if _, err := line.SelectionRects(2, 1, 16); !errors.Is(err, gpui.ErrTextValue) {
		t.Errorf("SelectionRects(2, 1) = %v, want ErrTextValue (start exceeds end)", err)
	}
}
