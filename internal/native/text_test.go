//go:build windows

package native

import (
	"errors"
	"math"
	"runtime"
	"testing"
	"unsafe"
)

// Text service tests (ticket09) run against the REAL rebuilt DLL embedded
// in this package. They exercise FFI correctness: table validation,
// record binary layout, the string-buffer and capacity protocols,
// shaping/geometry round trips through the pinned Parley stack, stale
// handles, and forced GC. The differential gates against the reference
// oracle live with the port adapter in a later subagent.

func mustTextService(t *testing.T) *TextService {
	t.Helper()
	lib, err := Load(Options{CacheRoot: testDir(t)})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	svc, err := lib.Text()
	if err != nil {
		t.Fatalf("Text() failed: %v", err)
	}
	return svc
}

// segoeRun returns a one-run spec covering the whole text in Segoe UI.
func segoeRun(text string) []TextRunSpec {
	return []TextRunSpec{{Len: len(text), Family: "Segoe UI", Weight: 400}}
}

func mustShape(t *testing.T, svc *TextService, request ShapeRequest) ShapingID {
	t.Helper()
	handle, err := svc.Shape(request)
	if err != nil {
		t.Fatalf("Shape failed: %v", err)
	}
	t.Cleanup(func() { _ = svc.Dispose(handle) })
	return handle
}

func mustLayoutInfo(t *testing.T, svc *TextService, handle ShapingID) TextLayout {
	t.Helper()
	layout, err := svc.LayoutInfo(handle)
	if err != nil {
		t.Fatalf("LayoutInfo failed: %v", err)
	}
	return layout
}

// (a) The text service table validates: capability bit, service version,
// record-size self-checks against the Go mirrors, non-null slots and the
// capacity bounds.
func TestTextServiceTableValidation(t *testing.T) {
	svc := mustTextService(t)
	if svc.MaxShapingHandles() != 256 {
		t.Errorf("MaxShapingHandles = %d, want 256", svc.MaxShapingHandles())
	}
	if svc.MaxTextBytes() != 1<<20 {
		t.Errorf("MaxTextBytes = %d, want 1 MiB", svc.MaxTextBytes())
	}

	// The service is reachable only with the capability bit present.
	lib, err := Load(Options{CacheRoot: testDir(t)})
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	if lib.Identity().Capabilities&capTextParley == 0 {
		t.Fatalf("identity capabilities %#x missing the text bit", lib.Identity().Capabilities)
	}
	if got := lib.Identity().NativeRevision; got != nativeRevision {
		t.Errorf("native revision = %d, want %d", got, nativeRevision)
	}
}

// (b) The Go record mirrors agree with the native layout the service
// table reports for itself (the load-time self-check, verified
// explicitly here).
func TestTextRecordBinaryLayout(t *testing.T) {
	if got := unsafe.Sizeof(textFontRequest{}); got != 28 {
		t.Errorf("sizeof(textFontRequest) = %d, want 28", got)
	}
	if got := unsafe.Sizeof(FontRecord{}); got != 40 {
		t.Errorf("sizeof(FontRecord) = %d, want 40", got)
	}
	if got := unsafe.Alignof(FontRecord{}); got != 8 {
		t.Errorf("alignof(FontRecord) = %d, want 8", got)
	}
	if got := unsafe.Offsetof(FontRecord{}.Generation); got != 8 {
		t.Errorf("offsetof(FontRecord.Generation) = %d, want 8", got)
	}
	if got := unsafe.Sizeof(FontMetricsRecord{}); got != 52 {
		t.Errorf("sizeof(FontMetricsRecord) = %d, want 52", got)
	}
	if got := unsafe.Sizeof(TextFeatureRecord{}); got != 8 {
		t.Errorf("sizeof(TextFeatureRecord) = %d, want 8", got)
	}
	if got := unsafe.Sizeof(TextRunRecord{}); got != 48 {
		t.Errorf("sizeof(TextRunRecord) = %d, want 48", got)
	}
	if got := unsafe.Sizeof(TextLayoutRecord{}); got != 56 {
		t.Errorf("sizeof(TextLayoutRecord) = %d, want 56", got)
	}
	if got := unsafe.Sizeof(TextLineRecord{}); got != 36 {
		t.Errorf("sizeof(TextLineRecord) = %d, want 36", got)
	}
	if got := unsafe.Sizeof(TextCaretRecord{}); got != 56 {
		t.Errorf("sizeof(TextCaretRecord) = %d, want 56", got)
	}
	if got := unsafe.Sizeof(TextHitRecord{}); got != 16 {
		t.Errorf("sizeof(TextHitRecord) = %d, want 16", got)
	}
	if got := unsafe.Sizeof(TextSelectionRecord{}); got != 20 {
		t.Errorf("sizeof(TextSelectionRecord) = %d, want 20", got)
	}
	if got := unsafe.Sizeof(TextClusterRecord{}); got != 16 {
		t.Errorf("sizeof(TextClusterRecord) = %d, want 16", got)
	}
	if got := unsafe.Sizeof(TextFragmentRecord{}); got != 32 {
		t.Errorf("sizeof(TextFragmentRecord) = %d, want 32", got)
	}
	if got := unsafe.Alignof(TextFragmentRecord{}); got != 8 {
		t.Errorf("alignof(TextFragmentRecord) = %d, want 8", got)
	}
	if got := unsafe.Offsetof(TextFragmentRecord{}.FontSizeBits); got != 8 {
		t.Errorf("offsetof(TextFragmentRecord.FontSizeBits) = %d, want 8", got)
	}
	if got := unsafe.Sizeof(TextGlyphRecord{}); got != 20 {
		t.Errorf("sizeof(TextGlyphRecord) = %d, want 20", got)
	}
	if got := unsafe.Sizeof(textTable{}); got != 200 {
		t.Errorf("sizeof(textTable) = %d, want 200", got)
	}
	if got := unsafe.Alignof(textTable{}); got != 8 {
		t.Errorf("alignof(textTable) = %d, want 8", got)
	}
}

// (c) Font enumeration through the capacity protocol: the probe reports
// the need, the retry fills, and the result is the real system catalog.
func TestTextFontNames(t *testing.T) {
	svc := mustTextService(t)
	names, err := svc.FontNames()
	if err != nil {
		t.Fatalf("FontNames failed: %v", err)
	}
	if len(names) < 10 {
		t.Fatalf("expected a real system font catalog, got %d names", len(names))
	}
	foundSegoeUI, foundAlias := false, false
	for _, name := range names {
		switch name {
		case "Segoe UI":
			foundSegoeUI = true
		case ".SystemUIFont":
			foundAlias = true
		}
	}
	if !foundSegoeUI {
		t.Errorf("Segoe UI missing from %d families", len(names))
	}
	if !foundAlias {
		t.Errorf(".SystemUIFont alias missing from the enumeration")
	}
}

// (d) Font resolution and metrics: canonical identity handle, sane
// metrics, unknown ids rejected with typed errors.
func TestTextFontResolveAndMetrics(t *testing.T) {
	svc := mustTextService(t)
	identity, err := svc.ResolveFont(FontDescriptor{Family: "Segoe UI", Weight: 400})
	if err != nil {
		t.Fatalf("ResolveFont failed: %v", err)
	}
	const canonicalBit = FontID(1 << 63)
	if identity.FontID&canonicalBit == 0 {
		t.Errorf("resolved FontID %#x lacks the canonical bit", identity.FontID)
	}
	metrics, err := svc.FontMetrics(identity.FontID)
	if err != nil {
		t.Fatalf("FontMetrics failed: %v", err)
	}
	if metrics.UnitsPerEm == 0 {
		t.Errorf("units_per_em = 0")
	}
	if metrics.Ascent <= 0 || metrics.Descent <= 0 {
		t.Errorf("ascent %v / descent %v must be positive", metrics.Ascent, metrics.Descent)
	}
	if metrics.XHeight <= 0 || metrics.XHeight > metrics.Ascent {
		t.Errorf("x_height %v must be within (0, ascent %v]", metrics.XHeight, metrics.Ascent)
	}

	// Deterministic re-resolution: the canonical id is stable.
	again, err := svc.ResolveFont(FontDescriptor{Family: "Segoe UI", Weight: 400})
	if err != nil {
		t.Fatalf("ResolveFont again failed: %v", err)
	}
	if again.FontID != identity.FontID {
		t.Errorf("re-resolution moved %d -> %d", identity.FontID, again.FontID)
	}

	// An unknown family resolves through the pinned fallback chain (the
	// service fallbacks end in Arial on this machine).
	fallback, err := svc.ResolveFont(FontDescriptor{Family: "Definitely Not A Real Family Zq7"})
	if err != nil {
		t.Fatalf("fallback resolution failed: %v (expected the pinned chain to resolve)", err)
	}
	if fallback.FontID&canonicalBit == 0 {
		t.Errorf("fallback FontID %#x lacks the canonical bit", fallback.FontID)
	}

	// Unknown font ids are typed errors, not panics.
	if _, err := svc.FontMetrics(1); !errors.Is(err, ErrTextBadHandle) {
		t.Errorf("FontMetrics(1) error = %v, want ErrTextBadHandle", err)
	}

	// A bogus feature tag is a typed Go-side validation error.
	if _, err := svc.ResolveFont(FontDescriptor{
		Family:   "Segoe UI",
		Features: []FontFeature{{Tag: "toolongtag", Value: 1}},
	}); !errors.Is(err, ErrTextBadValue) {
		t.Errorf("bad feature tag error = %v, want ErrTextBadValue", err)
	}
}

// (e) Shaping a known string: run/cluster/glyph counts and non-degenerate
// geometry, exactly as the pinned stack shapes it.
func TestTextShapeGeometry(t *testing.T) {
	svc := mustTextService(t)
	const text = "Hello, gpui-go world!"
	handle := mustShape(t, svc, ShapeRequest{
		Text:     text,
		FontSize: 16,
		Runs:     segoeRun(text),
	})
	layout := mustLayoutInfo(t, svc, handle)
	if layout.TextLen != len(text) {
		t.Errorf("text_len = %d, want %d", layout.TextLen, len(text))
	}
	if layout.FontSize != 16 {
		t.Errorf("font_size = %v, want 16", layout.FontSize)
	}
	if layout.LineCount != 1 {
		t.Errorf("line_count = %d, want 1 (no wrap)", layout.LineCount)
	}
	if layout.FragmentCount < 1 {
		t.Errorf("fragment_count = %d, want >= 1", layout.FragmentCount)
	}
	// The fixture is 21 characters, no ligature through this stack: one
	// glyph per character.
	if layout.GlyphCount != 21 {
		t.Errorf("glyph_count = %d, want 21", layout.GlyphCount)
	}
	if layout.Width < 80 {
		t.Errorf("width %v too small", layout.Width)
	}
	if layout.Ascent < 8 || layout.Descent < 2 {
		t.Errorf("ascent %v / descent %v too small", layout.Ascent, layout.Descent)
	}

	fragments, err := svc.Fragments(handle)
	if err != nil {
		t.Fatalf("Fragments failed: %v", err)
	}
	if len(fragments) != layout.FragmentCount {
		t.Errorf("fragment dump length = %d, want %d", len(fragments), layout.FragmentCount)
	}
	glyphs, err := svc.Glyphs(handle)
	if err != nil {
		t.Fatalf("Glyphs failed: %v", err)
	}
	if len(glyphs) != layout.GlyphCount {
		t.Errorf("glyph dump length = %d, want %d", len(glyphs), layout.GlyphCount)
	}
	// Fragments partition the glyph dump.
	covered, glyphStart := 0, 0
	for i, fragment := range fragments {
		if fragment.GlyphStart != glyphStart {
			t.Errorf("fragment %d glyph_start = %d, want %d", i, fragment.GlyphStart, glyphStart)
		}
		if fragment.FontSize != 16 {
			t.Errorf("fragment %d font_size = %v, want 16", i, fragment.FontSize)
		}
		if fragment.XEnd <= fragment.XStart {
			t.Errorf("fragment %d empty x range [%v, %v)", i, fragment.XStart, fragment.XEnd)
		}
		covered += fragment.GlyphCount
		glyphStart += fragment.GlyphCount
	}
	if covered != layout.GlyphCount {
		t.Errorf("fragments cover %d glyphs, want %d", covered, layout.GlyphCount)
	}
	// Glyph geometry is line-local/baseline-relative and advancing.
	previousX := float32(math.Inf(-1))
	for i, glyph := range glyphs {
		if glyph.ID == 0 {
			t.Errorf("glyph %d has id 0", i)
		}
		if glyph.X < previousX-1e-4 {
			t.Errorf("glyph %d x %v regressed below %v", i, glyph.X, previousX)
		}
		previousX = glyph.X
	}
	// The fragment font is resolvable through the metrics entry.
	if _, err := svc.FontMetrics(fragments[0].FontID); err != nil {
		t.Errorf("FontMetrics(fragment font) failed: %v", err)
	}
}

// (f) Wrapping, re-layout and clamping: line_count changes with the
// constraint, line records partition the text.
func TestTextWrapRelayoutAndClamp(t *testing.T) {
	svc := mustTextService(t)
	const text = "The quick brown fox jumps over the lazy dog"
	const lineHeight = float32(24)

	wrap := float32(100)
	handle := mustShape(t, svc, ShapeRequest{
		Text:      text,
		FontSize:  16,
		Runs:      segoeRun(text),
		WrapWidth: &wrap,
	})
	layout := mustLayoutInfo(t, svc, handle)
	if layout.LineCount < 2 {
		t.Fatalf("wrapped line_count = %d, want >= 2", layout.LineCount)
	}
	if layout.WrapWidth == nil || *layout.WrapWidth != 100 {
		t.Errorf("wrap echo = %v, want 100", layout.WrapWidth)
	}

	// Line records partition the text; the baseline matches the pinned
	// derivation.
	covered, lastEnd := 0, 0
	ascent, descent := layout.Ascent, layout.Descent
	for index := 0; index < layout.LineCount; index++ {
		line, err := svc.Line(handle, index, lineHeight)
		if err != nil {
			t.Fatalf("Line(%d) failed: %v", index, err)
		}
		if line.Index != index {
			t.Errorf("line %d echo = %d", index, line.Index)
		}
		if line.TextStart != lastEnd {
			t.Errorf("line %d starts at %d, want %d", index, line.TextStart, lastEnd)
		}
		lastEnd = line.TextEnd
		covered += line.TextEnd - line.TextStart
		expected := (lineHeight-ascent-descent)/2 + ascent
		if math.Abs(float64(line.Baseline-expected)) > 1e-3 {
			t.Errorf("line %d baseline %v, want %v", index, line.Baseline, expected)
		}
	}
	if covered != len(text) {
		t.Errorf("lines cover %d bytes, want %d", covered, len(text))
	}

	// Re-layout without wrapping collapses to one row.
	if err := svc.Relayout(handle, nil, nil); err != nil {
		t.Fatalf("Relayout failed: %v", err)
	}
	layout = mustLayoutInfo(t, svc, handle)
	if layout.LineCount != 1 {
		t.Errorf("unwrapped line_count = %d, want 1", layout.LineCount)
	}
	if layout.WrapWidth != nil {
		t.Errorf("wrap echo = %v, want nil", layout.WrapWidth)
	}

	// Line clamp caps the rows.
	wrap = 100
	clamp := uint32(2)
	handle = mustShape(t, svc, ShapeRequest{
		Text:      text,
		FontSize:  16,
		Runs:      segoeRun(text),
		WrapWidth: &wrap,
		LineClamp: &clamp,
	})
	if layout, err := svc.LayoutInfo(handle); err != nil {
		t.Fatalf("LayoutInfo(clamped) failed: %v", err)
	} else if layout.LineCount != 2 || layout.LineClamp == nil || *layout.LineClamp != 2 {
		t.Errorf("clamped layout = %+v, want 2 rows with clamp echo", layout)
	}
}

// (g) Caret monotonicity and the hit-test round trip
// (position -> offset -> position).
func TestTextCaretAndHitTestRoundTrip(t *testing.T) {
	svc := mustTextService(t)
	const text = "abcdefghij"
	const lineHeight = float32(24)
	handle := mustShape(t, svc, ShapeRequest{
		Text:     text,
		FontSize: 16,
		Runs:     segoeRun(text),
	})

	previousX := float32(math.Inf(-1))
	for offset := range text {
		caret, err := svc.Caret(handle, offset, CaretDownstream, lineHeight)
		if err != nil {
			t.Fatalf("Caret(%d) failed: %v", offset, caret)
		}
		if !caret.Present {
			t.Fatalf("Caret(%d) has no geometry", offset)
		}
		if caret.Bounds.Y != 0 {
			t.Errorf("Caret(%d) y = %v, want 0 (single line)", offset, caret.Bounds.Y)
		}
		if caret.Bounds.X < previousX {
			t.Errorf("Caret(%d) x %v regressed below %v", offset, caret.Bounds.X, previousX)
		}
		previousX = caret.Bounds.X
		// Cluster granularity: ASCII graphemes after a boundary.
		if caret.ClusterAfter == nil || caret.ClusterAfter.Start != offset || caret.ClusterAfter.End != offset+1 {
			t.Errorf("Caret(%d) cluster_after = %+v, want %d..%d", offset, caret.ClusterAfter, offset, offset+1)
		}
	}

	// Round trip: caret position -> hit -> caret position (exact).
	for offset := 2; offset < len(text); offset += 2 {
		caret, err := svc.Caret(handle, offset, CaretDownstream, lineHeight)
		if err != nil {
			t.Fatalf("Caret(%d) failed: %v", offset, err)
		}
		hit, err := svc.HitTest(handle, caret.Bounds.X, caret.Bounds.Y+lineHeight/2, lineHeight)
		if err != nil {
			t.Fatalf("HitTest at offset %d failed: %v", offset, err)
		}
		if !hit.Inside {
			t.Errorf("hit at caret position (offset %d) reported outside", offset)
		}
		round, err := svc.Caret(handle, hit.Index, CaretDownstream, lineHeight)
		if err != nil {
			t.Fatalf("Caret(round %d) failed: %v", hit.Index, err)
		}
		if round.Bounds.X != caret.Bounds.X || round.Bounds.Y != caret.Bounds.Y {
			t.Errorf("position round trip failed at offset %d: (%v,%v) -> %d -> (%v,%v)",
				offset, caret.Bounds.X, caret.Bounds.Y, hit.Index, round.Bounds.X, round.Bounds.Y)
		}
	}

	// A far outside point maps to the edge caret (not inside).
	hit, err := svc.HitTest(handle, 1e6, 1e6, lineHeight)
	if err != nil {
		t.Fatalf("HitTest(outside) failed: %v", err)
	}
	if hit.Inside {
		t.Errorf("far outside point reported inside")
	}
}

// (h) Selection rectangles for a mid-line range.
func TestTextSelectionRects(t *testing.T) {
	svc := mustTextService(t)
	const text = "select some geometry here"
	const lineHeight = float32(24)
	handle := mustShape(t, svc, ShapeRequest{
		Text:     text,
		FontSize: 16,
		Runs:     segoeRun(text),
	})
	rects, err := svc.SelectionRects(handle, 7, 14, lineHeight)
	if err != nil {
		t.Fatalf("SelectionRects failed: %v", err)
	}
	if len(rects) != 1 {
		t.Fatalf("selection rects = %d, want 1", len(rects))
	}
	rect := rects[0]
	if rect.W <= 0 {
		t.Errorf("selection width %v must be positive", rect.W)
	}
	if math.Abs(float64(rect.H-lineHeight)) > 1e-3 {
		t.Errorf("selection height %v, want %v", rect.H, lineHeight)
	}
	if rect.Y < 0 || rect.Y >= lineHeight {
		t.Errorf("selection y %v outside the row", rect.Y)
	}

	// An empty range produces no rectangles (the pinned rule).
	rects, err = svc.SelectionRects(handle, 7, 7, lineHeight)
	if err != nil {
		t.Fatalf("SelectionRects(empty) failed: %v", err)
	}
	if len(rects) != 0 {
		t.Errorf("empty selection rects = %d, want 0", len(rects))
	}
}

// (i) Cluster boundaries on multibyte text (the pin's logical clusters
// are graphemes).
func TestTextClusters(t *testing.T) {
	svc := mustTextService(t)
	// "a" + combining acute (U+0301, 2 bytes: one grapheme 0..3), "z"
	// (3..4), flag pair (4..12, one grapheme), "w" (12..13).
	text := "a\u0301z\U0001F1FA\U0001F1F8w"
	handle := mustShape(t, svc, ShapeRequest{
		Text:     text,
		FontSize: 16,
		Runs:     segoeRun(text),
	})
	cases := []struct {
		index      int
		side       ClusterSide
		present    bool
		start, end int
	}{
		{0, ClusterAfter, true, 0, 3},
		{2, ClusterAfter, false, 0, 0}, // inside the combining mark: typed error below
		{3, ClusterAfter, true, 3, 4},
		{4, ClusterAfter, true, 4, 12},
		{0, ClusterBefore, false, 0, 0},
		{12, ClusterBefore, true, 4, 12},
	}
	for _, want := range cases {
		cluster, err := svc.Cluster(handle, want.index, CaretDownstream, want.side)
		if want.index == 2 {
			// Byte 2 is inside the combining mark: not a boundary.
			if !errors.Is(err, ErrTextBadValue) {
				t.Errorf("Cluster(%d) error = %v, want ErrTextBadValue", want.index, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("Cluster(%d, %d) failed: %v", want.index, want.side, err)
		}
		if cluster.Present != want.present {
			t.Errorf("Cluster(%d, %d) present = %v, want %v", want.index, want.side, cluster.Present, want.present)
		}
		if cluster.Range.Start != want.start || cluster.Range.End != want.end {
			t.Errorf("Cluster(%d, %d) = %d..%d, want %d..%d", want.index, want.side,
				cluster.Range.Start, cluster.Range.End, want.start, want.end)
		}
	}
}

// (j) Multi-run shaping resolves distinct fonts per run.
func TestTextMultiRunFragments(t *testing.T) {
	svc := mustTextService(t)
	const text = "plainboldplain"
	handle := mustShape(t, svc, ShapeRequest{
		Text:     text,
		FontSize: 16,
		Runs: []TextRunSpec{
			{Len: 5, Family: "Segoe UI", Weight: 400},
			{Len: 9, Family: "Arial", Weight: 700},
		},
	})
	fragments, err := svc.Fragments(handle)
	if err != nil {
		t.Fatalf("Fragments failed: %v", err)
	}
	if len(fragments) < 2 {
		t.Fatalf("two styled runs produced %d fragments, want >= 2", len(fragments))
	}
	first := fragments[0].FontID
	distinct := false
	for _, fragment := range fragments {
		if fragment.FontID != first {
			distinct = true
		}
	}
	if !distinct {
		t.Errorf("expected at least two distinct resolved font ids")
	}
	// Fragment x ranges tile the line.
	x := fragments[0].XStart
	for i, fragment := range fragments {
		if math.Abs(float64(fragment.XStart-x)) > 1e-2 {
			t.Errorf("fragment %d starts at %v, want %v", i, fragment.XStart, x)
		}
		x = fragment.XEnd
	}
}

// (k) Shape validation: coverage, boundaries, non-finite floats.
func TestTextShapeValidation(t *testing.T) {
	svc := mustTextService(t)

	// Run coverage mismatch.
	if _, err := svc.Shape(ShapeRequest{
		Text:     "hello",
		FontSize: 16,
		Runs:     []TextRunSpec{{Len: 3, Family: "Segoe UI", Weight: 400}},
	}); !errors.Is(err, ErrTextBadValue) {
		t.Errorf("coverage mismatch error = %v, want ErrTextBadValue", err)
	}

	// A run ending inside a UTF-8 sequence.
	if _, err := svc.Shape(ShapeRequest{
		Text:     "h\u00e9llo",
		FontSize: 16,
		Runs:     []TextRunSpec{{Len: 2, Family: "Segoe UI", Weight: 400}},
	}); !errors.Is(err, ErrTextBadValue) {
		t.Errorf("mid-sequence run error = %v, want ErrTextBadValue", err)
	}

	// Non-finite font size.
	inf := float32(math.Inf(1))
	if _, err := svc.Shape(ShapeRequest{
		Text:     "hi",
		FontSize: inf,
		Runs:     segoeRun("hi"),
	}); !errors.Is(err, ErrTextBadValue) {
		t.Errorf("non-finite font size error = %v, want ErrTextBadValue", err)
	}

	// A bad line clamp.
	zero := uint32(0)
	if _, err := svc.Shape(ShapeRequest{
		Text:      "hi",
		FontSize:  16,
		Runs:      segoeRun("hi"),
		LineClamp: &zero,
	}); !errors.Is(err, ErrTextBadValue) {
		t.Errorf("zero clamp error = %v, want ErrTextBadValue", err)
	}
}

// (l) Empty text shapes one empty line (the pinned behavior).
func TestTextEmptyTextShapesOneEmptyLine(t *testing.T) {
	svc := mustTextService(t)
	handle := mustShape(t, svc, ShapeRequest{
		Text:     "",
		FontSize: 16,
		Runs:     []TextRunSpec{{Len: 0, Family: "Segoe UI", Weight: 400}},
	})
	layout := mustLayoutInfo(t, svc, handle)
	if layout.TextLen != 0 || layout.LineCount != 1 || layout.GlyphCount != 0 {
		t.Errorf("empty layout = %+v, want len 0, 1 line, 0 glyphs", layout)
	}
	if layout.Width != 0 {
		t.Errorf("empty layout width = %v, want 0", layout.Width)
	}
	fragments, err := svc.Fragments(handle)
	if err != nil || len(fragments) != 0 {
		t.Errorf("empty fragments = %v, %v, want none", fragments, err)
	}
	// The empty document's caret still has zero-width geometry.
	caret, err := svc.Caret(handle, 0, CaretDownstream, 24)
	if err != nil {
		t.Fatalf("Caret(0) on empty text failed: %v", err)
	}
	if !caret.Present || caret.Bounds.W != 0 {
		t.Errorf("empty caret = %+v, want present with zero width", caret)
	}
}

// (m) Hard newlines produce multiple rows; the separator stays in the
// paragraph's last row (the pinned document merge).
func TestTextNewlineShapesMultipleLines(t *testing.T) {
	svc := mustTextService(t)
	const text = "one\ntwo"
	handle := mustShape(t, svc, ShapeRequest{
		Text:     text,
		FontSize: 16,
		Runs:     segoeRun(text),
	})
	count, err := svc.LineCount(handle)
	if err != nil {
		t.Fatalf("LineCount failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("line_count = %d, want 2", count)
	}
	line, err := svc.Line(handle, 0, 24)
	if err != nil {
		t.Fatalf("Line(0) failed: %v", err)
	}
	if line.TextStart != 0 || line.TextEnd != 4 {
		t.Errorf("first row range = %d..%d, want 0..4 (separator included)", line.TextStart, line.TextEnd)
	}
	// The caret after the newline sits on row 2.
	caret, err := svc.Caret(handle, 4, CaretDownstream, 24)
	if err != nil {
		t.Fatalf("Caret(4) failed: %v", err)
	}
	if math.Abs(float64(caret.Bounds.Y-24)) > 1e-3 {
		t.Errorf("Caret(4) y = %v, want 24 (row 2)", caret.Bounds.Y)
	}
}

// (n) Stale and malformed handles: typed errors after dispose, and for
// handles that never existed.
func TestTextStaleHandles(t *testing.T) {
	svc := mustTextService(t)
	const text = "stale handle probe"
	handle, err := svc.Shape(ShapeRequest{Text: text, FontSize: 16, Runs: segoeRun(text)})
	if err != nil {
		t.Fatalf("Shape failed: %v", err)
	}
	if err := svc.Dispose(handle); err != nil {
		t.Fatalf("Dispose failed: %v", err)
	}
	if _, err := svc.LayoutInfo(handle); !errors.Is(err, ErrTextStaleHandle) {
		t.Errorf("LayoutInfo(disposed) error = %v, want ErrTextStaleHandle", err)
	}
	if _, err := svc.LineCount(handle); !errors.Is(err, ErrTextStaleHandle) {
		t.Errorf("LineCount(disposed) error = %v, want ErrTextStaleHandle", err)
	}
	if err := svc.Dispose(handle); !errors.Is(err, ErrTextStaleHandle) {
		t.Errorf("second Dispose error = %v, want ErrTextStaleHandle", err)
	}
	// A handle whose slot never existed (slot index far beyond the cap).
	var bogus ShapingID = 0x00FF_FFFF << 20
	if _, err := svc.LayoutInfo(bogus); !errors.Is(err, ErrTextBadHandle) {
		t.Errorf("LayoutInfo(bogus) error = %v, want ErrTextBadHandle", err)
	}
}

// (o) The live-handle capacity bound: fill to 256, observe the typed
// limit, dispose one, shape again.
func TestTextShapingHandleLimit(t *testing.T) {
	svc := mustTextService(t)
	var live []ShapingID
	defer func() {
		for _, handle := range live {
			_ = svc.Dispose(handle)
		}
	}()
	for {
		handle, err := svc.Shape(ShapeRequest{
			Text:     "x",
			FontSize: 16,
			Runs:     segoeRun("x"),
		})
		if errors.Is(err, ErrTextShapingLimit) {
			break
		}
		if err != nil {
			t.Fatalf("fill Shape failed: %v", err)
		}
		live = append(live, handle)
		if len(live) >= svc.MaxShapingHandles() {
			break
		}
	}
	if len(live) > svc.MaxShapingHandles() {
		t.Fatalf("live handles %d exceed the bound %d", len(live), svc.MaxShapingHandles())
	}
	// The limit is now observable for one more.
	if _, err := svc.Shape(ShapeRequest{
		Text:     "y",
		FontSize: 16,
		Runs:     segoeRun("y"),
	}); !errors.Is(err, ErrTextShapingLimit) {
		t.Errorf("overflow Shape error = %v, want ErrTextShapingLimit", err)
	}
	// Disposing one frees a slot.
	freed := live[len(live)-1]
	live = live[:len(live)-1]
	if err := svc.Dispose(freed); err != nil {
		t.Fatalf("Dispose failed: %v", err)
	}
	handle, err := svc.Shape(ShapeRequest{
		Text:     "z",
		FontSize: 16,
		Runs:     segoeRun("z"),
	})
	if err != nil {
		t.Fatalf("Shape after dispose failed: %v", err)
	}
	live = append(live, handle)
}

// (p) Forced GC between calls: no Go pointer is retained across the
// boundary, so collection must never disturb the service (the layout
// service's GC-hazard pattern applied to text).
func TestTextForcedGCDuringCalls(t *testing.T) {
	svc := mustTextService(t)
	const text = "collect me safely"
	handle := mustShape(t, svc, ShapeRequest{Text: text, FontSize: 16, Runs: segoeRun(text)})
	for i := 0; i < 20; i++ {
		runtime.GC()
		layout, err := svc.LayoutInfo(handle)
		if err != nil {
			t.Fatalf("LayoutInfo after GC %d failed: %v", i, err)
		}
		if layout.GlyphCount == 0 {
			t.Fatalf("layout lost its glyphs after GC %d", i)
		}
		runtime.GC()
		fragments, err := svc.Fragments(handle)
		if err != nil {
			t.Fatalf("Fragments after GC %d failed: %v", i, err)
		}
		if len(fragments) != layout.FragmentCount {
			t.Fatalf("fragment count changed after GC %d", i)
		}
	}
	// A fresh library load also survives forced GC (the shared stack is
	// process-global; the load path must not double-create it).
	runtime.GC()
	lib, err := Load(Options{CacheRoot: testDir(t)})
	if err != nil {
		t.Fatalf("second Load failed: %v", err)
	}
	defer lib.Close()
	svc2, err := lib.Text()
	if err != nil {
		t.Fatalf("second Text() failed: %v", err)
	}
	if _, err := svc2.FontNames(); err != nil {
		t.Fatalf("FontNames on the second service failed: %v", err)
	}
}

// (q) The manifest records the text stack resolution (the cumulative
// mask now includes the text bit).
func TestTextManifestIdentity(t *testing.T) {
	auth := testAuthority(t)
	if auth.Text == nil {
		t.Fatal("manifest missing the text resolution record")
	}
	for crate, want := range map[string]string{
		"parley":   "0.11.1",
		"fontique": "0.11.1",
		"harfrust": "0.12.0",
		"skrifa":   "0.44.0",
		"swash":    "0.2.10",
	} {
		var got TextCrateResolution
		switch crate {
		case "parley":
			got = auth.Text.Parley
		case "fontique":
			got = auth.Text.Fontique
		case "harfrust":
			got = auth.Text.Harfrust
		case "skrifa":
			got = auth.Text.Skrifa
		case "swash":
			got = auth.Text.Swash
		}
		if got.Version != want || !got.PinSatisfied {
			t.Errorf("manifest %s version = %q pin=%v, want %q true", crate, got.Version, got.PinSatisfied, want)
		}
		if got.Checksum == "" {
			t.Errorf("manifest %s checksum is empty", crate)
		}
	}
	if auth.Text.GpuiCe.Path == "" || auth.Text.GpuiCeParley.Path == "" {
		t.Errorf("manifest text path pins missing: %+v", auth.Text)
	}
}
