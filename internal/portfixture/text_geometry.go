package portfixture

// This file implements the text-geometry-v1 fixture runner: the fx-0004
// envelope executed against the port's real text adapter (gpui's
// TextSystem over the native Parley/Fontique text service), recording
// the same trace events the reference harness records
// (reference/harness/src/fixtures/text_geometry.rs, run_case op for op):
// text-system-begin with the catalog shape, the default font
// resolution, then per case the case-begin event, the case and run font
// resolutions, the shaping summary, font metrics (case font plus every
// distinct fragment font), the visual lines with their paint placement,
// the paint fragments per line, the logical (grapheme) clusters, the
// caret queries (bounds plus adjacent clusters), the hit tests (closest
// caret plus the byte-index hit test) and the selection rectangles.
//
// The gate (text_geometry_test.go) compares the port trace against the
// recorded reference trace with CompareTraces: every event must match
// exactly, including every f32 bit pattern of every metric, position,
// advance and baseline, every canonical FontId and every affinity.

import (
	"fmt"
	"strings"

	"gpui-go/conformance"
	"gpui-go/gpui"
)

// The pinned Windows platform's text stack construction facts
// (reference/harness/src/fixtures/text_geometry.rs).
const (
	// textServiceSystemFontFamily is the pinned system font family
	// argument of ParleyTextSystem::new_with_rasterizer.
	textServiceSystemFontFamily = "Segoe UI"
	// textServiceFallbackFamilies is the pinned service fallback chain.
	textServiceFallbackFamilies = "Lilex, IBM Plex Sans, Arial"
)

// textCatalogProbeFamilies are the families whose catalog presence is
// recorded in text-system-begin for machine-drift detection.
var textCatalogProbeFamilies = [...]string{
	"Segoe UI",
	"Segoe UI Emoji",
	"Arial",
	"Microsoft YaHei",
	"Microsoft YaHei UI",
	"Lilex",
	"IBM Plex Sans",
}

// RunTextGeometry executes the envelope's text cases against the real
// gpui text adapter and returns the port-side trace (with the port
// harness identity and no envelope hash: run metadata belongs to the
// caller, which pins the envelope bytes it executed).
func RunTextGeometry(envelope *conformance.Envelope) (*conformance.Trace, error) {
	if envelope == nil {
		return nil, fmt.Errorf("portfixture: nil envelope")
	}
	if envelope.FixtureKind != conformance.FixtureKindTextGeometry {
		return nil, fmt.Errorf("portfixture: unsupported fixture kind %q", envelope.FixtureKind)
	}
	inputs := envelope.Inputs.TextCases
	if inputs == nil {
		return nil, fmt.Errorf("portfixture: fixture kind text-geometry-v1 requires inputs.text_cases")
	}

	system, err := gpui.DefaultTextSystem()
	if err != nil {
		return nil, fmt.Errorf("portfixture: text system: %w", err)
	}
	rec := &recorder{}
	if err := runTextSystemBegin(system, rec); err != nil {
		return nil, fmt.Errorf("text-geometry fixture construction failed: %w", err)
	}

	for i := range inputs.Cases {
		// The reference panics with the case label on failure; the port
		// returns the error with it.
		if err := runTextGeometryCase(system, &inputs.Cases[i], rec); err != nil {
			return nil, fmt.Errorf("text-geometry case %q failed: %w", inputs.Cases[i].Label, err)
		}
	}

	trace := &conformance.Trace{
		Schema:      conformance.TraceSchema,
		FixtureID:   envelope.FixtureID,
		FixtureKind: envelope.FixtureKind,
		Harness: conformance.HarnessInfo{
			HarnessVersion: "0.1.0",
			HarnessCrate:   "gpui-go-portfixture",
			GpuiCrate:      "gpui-ce 0.2.2",
			GpuiCommit:     pinnedGpuiCommit,
			Profile:        "test",
		},
		Events: rec.snapshot(),
	}
	return trace, nil
}

// traceFontID canonicalizes a canonical FontId for the trace schema's
// numeric comparison. The recorded reference traces carry JSON numbers
// (decoded as float64 by the conformance loader), and CompareTraces
// compares raw JSON scalars: a uint64 with the canonical bit set
// marshals differently from the recorded side's float64, so the port
// records ids in the same float64 form the comparison canonicalizes
// both sides through. Ids below 2^53 (every small count and index)
// compare identically as uint64 and stay uint64.
func traceFontID(id gpui.FontID) float64 {
	return float64(uint64(id))
}

// runTextSystemBegin records the construction record (text-system-begin)
// and the default font resolution (font-resolved, role default),
// mirroring the reference fixture's run() prologue.
//
// Deviation from the reference's call ORDER (not its events): the
// reference reads font_generation() directly before resolving anything;
// the native ABI exposes the catalog generation only through a resolved
// font's identity record, so the port resolves the pinned default font
// (Font::default(), ".SystemUIFont" 400 Normal — the same first resolve
// the reference performs for its default-font event) and reads the
// generation from it. The catalog generation is a registration-batch
// counter that no resolve changes (it stays 0 with SystemFonts::Load),
// so the recorded value is the pre-resolution generation the reference
// reads; resolving the default descriptor first also preserves the
// FontStore's interning order, keeping every canonical FontId in the
// trace identical to the reference's.
func runTextSystemBegin(system *gpui.TextSystem, rec *recorder) error {
	names, err := system.FontNames()
	if err != nil {
		return fmt.Errorf("font names: %w", err)
	}

	// The pinned default font (Font::default()): the system UI alias,
	// normal weight and style, no features, no fallbacks.
	defaultFont, err := system.ResolveFont(gpui.DefaultFontDescriptor())
	if err != nil {
		return fmt.Errorf("resolving the default system font: %w", err)
	}

	hasFamily := func(family string) uint64 {
		for _, name := range names {
			if name == family {
				return 1
			}
		}
		return 0
	}
	fields := []field{
		{"systemFontFamily", textServiceSystemFontFamily},
		{"fallbackFamilies", textServiceFallbackFamilies},
		{"fontGeneration", defaultFont.Generation},
		{"fontFamilyCount", uint64(len(names))},
	}
	for _, family := range textCatalogProbeFamilies {
		fields = append(fields, field{"has" + strings.ReplaceAll(family, " ", ""), hasFamily(family)})
	}
	rec.eventWith("text-system-begin", "text-system", fields...)

	recordFontResolved(rec, "default-font", textFontRoleDefault, -1, gpui.DefaultFontDescriptor(), defaultFont)
	return nil
}

// Font roles of font-resolved events (the reference's constants).
const (
	textFontRoleDefault = "default"
	textFontRoleCase    = "case"
	textFontRoleRun     = "run"
)

// Font metric sources of font-metrics events.
const (
	textMetricSourceCase     = "case"
	textMetricSourceFragment = "fragment"
)

// recordFontResolved records one font-resolved event: the descriptor
// echo plus the resolved canonical FontId handle (runIndex is included
// only for run roles).
func recordFontResolved(rec *recorder, label, role string, runIndex int, descriptor gpui.FontDescriptor, identity gpui.FontIdentity) {
	fields := []field{
		{"family", descriptor.Family},
		{"fontId", traceFontID(identity.FontID)},
		{"weight", conformance.F32Bits(descriptor.Weight)},
		{"style", descriptor.Style.String()},
		{"featureCount", uint64(len(descriptor.Features))},
		{"fallbackCount", uint64(len(descriptor.Fallbacks))},
		{"role", role},
	}
	if role == textFontRoleRun {
		fields = append(fields, field{"runIndex", uint64(runIndex)})
	}
	rec.eventWith("font-resolved", label, fields...)
}

// recordFontMetrics records one font-metrics event: the full face
// metrics in font units (the same record the native text service
// exposes) plus the pixel-scaled values through the pinned
// FontMetrics::ascent/descent/x_height arithmetic at the case's font
// size.
func recordFontMetrics(rec *recorder, label string, fontID gpui.FontID, source string, fontSize float32, metrics gpui.FontMetrics) {
	rec.eventWith("font-metrics", label,
		field{"fontId", traceFontID(fontID)},
		field{"source", source},
		field{"unitsPerEm", uint64(metrics.UnitsPerEm)},
		field{"ascent", conformance.F32Bits(metrics.Ascent)},
		field{"descent", conformance.F32Bits(metrics.Descent)},
		field{"lineGap", conformance.F32Bits(metrics.LineGap)},
		field{"underlinePosition", conformance.F32Bits(metrics.UnderlinePosition)},
		field{"underlineThickness", conformance.F32Bits(metrics.UnderlineThickness)},
		field{"capHeight", conformance.F32Bits(metrics.CapHeight)},
		field{"xHeight", conformance.F32Bits(metrics.XHeight)},
		field{"bboxX", conformance.F32Bits(metrics.BoundingBoxX)},
		field{"bboxY", conformance.F32Bits(metrics.BoundingBoxY)},
		field{"bboxWidth", conformance.F32Bits(metrics.BoundingBoxWidth)},
		field{"bboxHeight", conformance.F32Bits(metrics.BoundingBoxHeight)},
		field{"ascentPx", conformance.F32Bits(metrics.AscentPx(fontSize))},
		field{"descentPx", conformance.F32Bits(metrics.DescentPx(fontSize))},
		field{"xHeightPx", conformance.F32Bits(metrics.XHeightPx(fontSize))},
	)
}

// ---------------------------------------------------------------------------
// Input conversion (envelope -> adapter)
// ---------------------------------------------------------------------------

// fontStyleFrom parses a style name ("Normal" default, "Italic",
// "Oblique"), failing loudly on unknown values the way the reference's
// parse_style bails.
func fontStyleFrom(style string) (gpui.FontStyle, error) {
	switch style {
	case "", conformance.TextFontStyleNormal:
		return gpui.FontStyleNormal, nil
	case conformance.TextFontStyleItalic:
		return gpui.FontStyleItalic, nil
	case conformance.TextFontStyleOblique:
		return gpui.FontStyleOblique, nil
	default:
		return 0, fmt.Errorf("unsupported font style %q: expected Normal, Italic or Oblique", style)
	}
}

// fontDescriptorFrom converts the envelope's font descriptor into the
// adapter's, defaulting weight to the pinned 400 and style to Normal.
func fontDescriptorFrom(in conformance.TextFontIn) (gpui.FontDescriptor, error) {
	style, err := fontStyleFrom(in.Style)
	if err != nil {
		return gpui.FontDescriptor{}, err
	}
	weight := gpui.DefaultFontWeight
	if in.Weight != nil {
		weight = float32(*in.Weight)
	}
	features := make([]gpui.FontFeature, len(in.Features))
	for i, feature := range in.Features {
		features[i] = gpui.FontFeature{Tag: feature.Tag, Value: feature.Value}
	}
	return gpui.FontDescriptor{
		Family:    in.Family,
		Weight:    weight,
		Style:     style,
		Features:  features,
		Fallbacks: in.Fallbacks,
	}, nil
}

// mergeFontOverride merges one run's font overrides over the case's
// base descriptor (absent fields inherit the base; present fields —
// including explicitly empty feature/fallback lists — replace).
func mergeFontOverride(base gpui.FontDescriptor, overrides *conformance.TextFontOverrideIn) (gpui.FontDescriptor, error) {
	if overrides == nil {
		return base, nil
	}
	merged := base
	if overrides.Family != nil {
		merged.Family = *overrides.Family
	}
	if overrides.Weight != nil {
		merged.Weight = float32(*overrides.Weight)
	}
	if overrides.Style != nil {
		style, err := fontStyleFrom(*overrides.Style)
		if err != nil {
			return gpui.FontDescriptor{}, err
		}
		merged.Style = style
	}
	if overrides.Features != nil {
		features := make([]gpui.FontFeature, len(overrides.Features))
		for i, feature := range overrides.Features {
			features[i] = gpui.FontFeature{Tag: feature.Tag, Value: feature.Value}
		}
		merged.Features = features
	}
	if overrides.Fallbacks != nil {
		merged.Fallbacks = overrides.Fallbacks
	}
	return merged, nil
}

// textRunsFrom builds the case's style runs: an empty run list means
// one run covering the whole text with the case's font. Present runs
// must cover the text exactly and end on UTF-8 character boundaries —
// the pin assumes gpui-validated input, so the runner validates loudly
// instead.
func textRunsFrom(c *conformance.TextGeometryCase, base gpui.FontDescriptor) ([]gpui.TextRun, error) {
	if len(c.Runs) == 0 {
		return []gpui.TextRun{{Len: len(c.Text), Font: base}}, nil
	}
	runs := make([]gpui.TextRun, 0, len(c.Runs))
	covered := 0
	for i, runIn := range c.Runs {
		descriptor, err := mergeFontOverride(base, runIn.Font)
		if err != nil {
			return nil, fmt.Errorf("run %d: %w", i, err)
		}
		var letterSpacing *float32
		if runIn.LetterSpacing != nil {
			spacing := float32(*runIn.LetterSpacing)
			letterSpacing = &spacing
		}
		runs = append(runs, gpui.TextRun{
			Len:           int(runIn.Len),
			Font:          descriptor,
			LetterSpacing: letterSpacing,
		})
		covered += int(runIn.Len)
		if covered > len(c.Text) || !isUTF8Boundary(c.Text, covered) {
			return nil, fmt.Errorf("runs cover %d bytes, which is not a UTF-8 character boundary of the %d-byte text", covered, len(c.Text))
		}
	}
	if covered != len(c.Text) {
		return nil, fmt.Errorf("runs cover %d bytes but the text is %d bytes", covered, len(c.Text))
	}
	return runs, nil
}

// isUTF8Boundary reports whether offset is a UTF-8 character boundary
// of text (a boundary byte is not a continuation byte).
func isUTF8Boundary(text string, offset int) bool {
	if offset == 0 || offset == len(text) {
		return true
	}
	if offset < 0 || offset > len(text) {
		return false
	}
	return text[offset]&0xC0 != 0x80
}

// ---------------------------------------------------------------------------
// Case execution
// ---------------------------------------------------------------------------

// textCaretQuery is one caret query resolved to its input position and
// affinity.
type textCaretQuery struct {
	index    int
	affinity gpui.CaretAffinity
}

// textCaretQueries expands the case's caret mode into the concrete
// query list. all-clusters/all-clusters-both derive the cluster
// boundaries through the cluster API itself (one query per boundary,
// and both affinities for the -both mode so wrap-boundary affinity is
// observable).
func textCaretQueries(c *conformance.TextGeometryCase, clusters []gpui.TextRange) ([]textCaretQuery, error) {
	switch mode := c.CaretMode; mode {
	case "", conformance.TextCaretModeExplicit:
		queries := make([]textCaretQuery, 0, len(c.CaretQueries))
		for _, query := range c.CaretQueries {
			affinity := gpui.CaretAffinityDownstream
			switch query.Affinity {
			case "", conformance.TextAffinityDownstream:
			case conformance.TextAffinityUpstream:
				affinity = gpui.CaretAffinityUpstream
			default:
				return nil, fmt.Errorf("unsupported caret affinity %q: expected downstream or upstream", query.Affinity)
			}
			queries = append(queries, textCaretQuery{index: int(query.Index), affinity: affinity})
		}
		return queries, nil
	case conformance.TextCaretModeAllClusters, conformance.TextCaretModeAllClustersBoth:
		boundaries := []int{0}
		for _, cluster := range clusters {
			if boundaries[len(boundaries)-1] < cluster.End {
				boundaries = append(boundaries, cluster.End)
			}
		}
		queries := make([]textCaretQuery, 0, len(boundaries)*2)
		for _, index := range boundaries {
			queries = append(queries, textCaretQuery{index: index, affinity: gpui.CaretAffinityDownstream})
			if mode == conformance.TextCaretModeAllClustersBoth {
				queries = append(queries, textCaretQuery{index: index, affinity: gpui.CaretAffinityUpstream})
			}
		}
		return queries, nil
	default:
		return nil, fmt.Errorf("unsupported caret mode %q: expected explicit, all-clusters or all-clusters-both", mode)
	}
}

// runTextGeometryCase executes one case, mirroring the reference
// run_case: build the runs, record case-begin, resolve the case font
// and every distinct run font, shape, enumerate the clusters, record
// the shaping summary, the font metrics, the visual lines, the paint
// fragments per line, the clusters, the caret queries, the hit tests
// and the selections, and close with case-end.
func runTextGeometryCase(system *gpui.TextSystem, c *conformance.TextGeometryCase, rec *recorder) error {
	caseFont, err := fontDescriptorFrom(c.Font)
	if err != nil {
		return fmt.Errorf("case font: %w", err)
	}
	runs, err := textRunsFrom(c, caseFont)
	if err != nil {
		return fmt.Errorf("style runs: %w", err)
	}

	fontSize := float32(c.FontSize)
	lineHeight := gpui.DefaultLineHeight(fontSize)
	if c.LineHeight != nil {
		lineHeight = float32(*c.LineHeight)
	}

	caseBeginFields := []field{
		{"label", c.Label},
		{"textLen", uint64(len(c.Text))},
		{"fontSize", conformance.F32Bits(fontSize)},
		{"lineHeight", conformance.F32Bits(lineHeight)},
		{"runCount", uint64(len(runs))},
	}
	if c.WrapWidth != nil {
		caseBeginFields = append(caseBeginFields,
			field{"wrapPresent", uint64(1)},
			field{"wrapWidth", conformance.F32Bits(float32(*c.WrapWidth))},
		)
	} else {
		caseBeginFields = append(caseBeginFields, field{"wrapPresent", uint64(0)})
	}
	if c.LineClamp != nil {
		caseBeginFields = append(caseBeginFields,
			field{"clampPresent", uint64(1)},
			field{"lineClamp", uint64(*c.LineClamp)},
		)
	} else {
		caseBeginFields = append(caseBeginFields, field{"clampPresent", uint64(0)})
	}
	rec.eventWith("case-begin", c.Label, caseBeginFields...)

	// Resolve the case's own descriptor, then every distinct run
	// descriptor (run fonts that differ from an already-resolved
	// descriptor resolve separately — the store may map different
	// descriptors to the same face).
	caseFontID, err := resolveAndRecordFont(system, rec, c.Label, textFontRoleCase, -1, caseFont)
	if err != nil {
		return err
	}
	resolved := []gpui.FontDescriptor{caseFont}
	for runIndex, run := range runs {
		// Skip runs whose effective font is identical to an already
		// resolved descriptor: the descriptor echo would repeat.
		alreadyResolved := false
		for _, descriptor := range resolved {
			if descriptor.Equal(run.Font) {
				alreadyResolved = true
				break
			}
		}
		if alreadyResolved {
			continue
		}
		if _, err := resolveAndRecordFont(system, rec, c.Label, textFontRoleRun, runIndex, run.Font); err != nil {
			return err
		}
		resolved = append(resolved, run.Font)
	}

	// Shape through the port's public API. No window, app or platform
	// context is involved.
	wrapWidth := (*float32)(nil)
	if c.WrapWidth != nil {
		wrap := float32(*c.WrapWidth)
		wrapWidth = &wrap
	}
	lineClamp := (*uint32)(nil)
	if c.LineClamp != nil {
		clamp := uint32(*c.LineClamp)
		lineClamp = &clamp
	}
	line, err := system.ShapeText(gpui.TextLayoutRequest{
		Text:      c.Text,
		FontSize:  fontSize,
		Runs:      runs,
		WrapWidth: wrapWidth,
		LineClamp: lineClamp,
	})
	if err != nil {
		return fmt.Errorf("shaping: %w", err)
	}
	defer func() { _ = line.Dispose() }()

	// Enumerate logical clusters through the cluster API (graphemes in
	// this pin).
	clusters, err := line.Clusters()
	if err != nil {
		return fmt.Errorf("logical clusters: %w", err)
	}
	summary, err := line.Summary()
	if err != nil {
		return err
	}

	rec.eventWith("text-shape-meta", c.Label,
		field{"len", uint64(summary.TextLen)},
		field{"fontSize", conformance.F32Bits(summary.FontSize)},
		field{"lineHeight", conformance.F32Bits(lineHeight)},
		field{"width", conformance.F32Bits(summary.Width)},
		field{"ascent", conformance.F32Bits(summary.Ascent)},
		field{"descent", conformance.F32Bits(summary.Descent)},
		field{"lineCount", uint64(summary.LineCount)},
		field{"fragmentCount", uint64(summary.FragmentCount)},
		field{"glyphCount", uint64(summary.GlyphCount)},
		field{"clusterCount", uint64(len(clusters))},
	)

	// Font metrics: the case's resolved font first, then every distinct
	// font Parley selected while shaping (character-level fallback can
	// pick faces the caller never resolved).
	fragments, err := line.Fragments()
	if err != nil {
		return fmt.Errorf("fragments: %w", err)
	}
	metricFonts := []gpui.FontID{caseFontID}
	for _, fragment := range fragments {
		known := false
		for _, fontID := range metricFonts {
			if fontID == fragment.FontID {
				known = true
				break
			}
		}
		if !known {
			metricFonts = append(metricFonts, fragment.FontID)
		}
	}
	for _, fontID := range metricFonts {
		metrics, err := system.FontMetrics(fontID)
		if err != nil {
			return fmt.Errorf("font metrics for %#x: %w", uint64(fontID), err)
		}
		source := textMetricSourceFragment
		if fontID == caseFontID {
			source = textMetricSourceCase
		}
		recordFontMetrics(rec, c.Label, fontID, source, fontSize, metrics)
	}

	// Visual lines, with the row placement the pinned paint path
	// derives: the native line entry computes the baseline.
	for lineIndex := 0; lineIndex < summary.LineCount; lineIndex++ {
		row, err := line.Line(lineIndex, lineHeight)
		if err != nil {
			return fmt.Errorf("visual line %d: %w", lineIndex, err)
		}
		rec.eventWith("text-line", c.Label,
			field{"lineIndex", uint64(row.Index)},
			field{"textStart", uint64(row.TextStart)},
			field{"textEnd", uint64(row.TextEnd)},
			field{"fragmentStart", uint64(row.FragmentStart)},
			field{"fragmentEnd", uint64(row.FragmentEnd)},
			field{"advanceWidth", conformance.F32Bits(row.AdvanceWidth)},
			field{"lineHeight", conformance.F32Bits(row.LineHeight)},
			field{"baseline", conformance.F32Bits(row.Baseline)},
		)
	}

	// Paint fragments (the public "run" surface) in visual order.
	for lineIndex := 0; lineIndex < summary.LineCount; lineIndex++ {
		row, err := line.Line(lineIndex, lineHeight)
		if err != nil {
			return fmt.Errorf("visual line %d: %w", lineIndex, err)
		}
		for fragmentIndex := row.FragmentStart; fragmentIndex < row.FragmentEnd; fragmentIndex++ {
			if fragmentIndex < 0 || fragmentIndex >= len(fragments) {
				return fmt.Errorf("visual line %d references fragment %d outside the %d-fragment dump", lineIndex, fragmentIndex, len(fragments))
			}
			fragment := fragments[fragmentIndex]
			rec.eventWith("text-run", c.Label,
				field{"lineIndex", uint64(lineIndex)},
				field{"fontId", traceFontID(fragment.FontID)},
				field{"fontSize", conformance.F32Bits(fragment.FontSize)},
				field{"xStart", conformance.F32Bits(fragment.XStart)},
				field{"xEnd", conformance.F32Bits(fragment.XEnd)},
				field{"glyphCount", uint64(fragment.GlyphCount)},
			)
		}
	}

	// Logical clusters in document order.
	for clusterIndex, cluster := range clusters {
		rec.eventWith("text-cluster", c.Label,
			field{"clusterIndex", uint64(clusterIndex)},
			field{"start", uint64(cluster.Start)},
			field{"end", uint64(cluster.End)},
		)
	}

	// Caret queries: full bounds plus the adjacent logical clusters.
	queries, err := textCaretQueries(c, clusters)
	if err != nil {
		return err
	}
	for _, query := range queries {
		caret, err := line.Caret(query.index, query.affinity, lineHeight)
		if err != nil {
			return fmt.Errorf("caret at %d: %w", query.index, err)
		}
		fields := []field{
			{"index", uint64(query.index)},
			{"affinity", query.affinity.String()},
		}
		if caret.Present {
			fields = append(fields,
				field{"present", uint64(1)},
				field{"x", conformance.F32Bits(caret.Bounds.X)},
				field{"y", conformance.F32Bits(caret.Bounds.Y)},
				field{"width", conformance.F32Bits(caret.Bounds.W)},
				field{"height", conformance.F32Bits(caret.Bounds.H)},
			)
		} else {
			fields = append(fields, field{"present", uint64(0)})
		}
		fields = appendClusterFields(fields, "clusterBefore", caret.ClusterBefore)
		fields = appendClusterFields(fields, "clusterAfter", caret.ClusterAfter)
		rec.eventWith("text-caret", c.Label, fields...)
	}

	// Hit tests: the caret hit test (the closest caret; inside = Ok)
	// and the byte-index hit test (the logical start of the cluster
	// under the point).
	for _, pointIn := range c.HitPoints {
		x, y := float32(pointIn.X), float32(pointIn.Y)
		hit, err := line.HitTest(x, y, lineHeight)
		if err != nil {
			return fmt.Errorf("hit test at (%v, %v): %w", x, y, err)
		}
		byteIndex, byteIndexPresent, err := line.ByteIndexForPixelPoint(x, y, lineHeight)
		if err != nil {
			return fmt.Errorf("byte-index hit test at (%v, %v): %w", x, y, err)
		}
		inside := uint64(0)
		if hit.Inside {
			inside = 1
		}
		byteIndexPresentField := uint64(0)
		if byteIndexPresent {
			byteIndexPresentField = 1
		}
		rec.eventWith("text-hit", c.Label,
			field{"x", conformance.F32Bits(x)},
			field{"y", conformance.F32Bits(y)},
			field{"inside", inside},
			field{"index", uint64(hit.Index)},
			field{"affinity", hit.Affinity.String()},
			field{"byteIndexPresent", byteIndexPresentField},
			field{"byteIndex", uint64(byteIndex)},
		)
	}

	// Selection ranges: the visual-order rectangles.
	for _, selection := range c.Selections {
		rects, err := line.SelectionRects(int(selection.Start), int(selection.End), lineHeight)
		if err != nil {
			return fmt.Errorf("selection %d..%d: %w", selection.Start, selection.End, err)
		}
		if len(rects) == 0 {
			rec.eventWith("text-selection", c.Label,
				field{"start", uint64(selection.Start)},
				field{"end", uint64(selection.End)},
				field{"rectIndex", uint64(0)},
				field{"rectPresent", uint64(0)},
			)
			continue
		}
		for rectIndex, rect := range rects {
			rec.eventWith("text-selection", c.Label,
				field{"start", uint64(selection.Start)},
				field{"end", uint64(selection.End)},
				field{"rectIndex", uint64(rectIndex)},
				field{"rectPresent", uint64(1)},
				field{"x", conformance.F32Bits(rect.X)},
				field{"y", conformance.F32Bits(rect.Y)},
				field{"width", conformance.F32Bits(rect.W)},
				field{"height", conformance.F32Bits(rect.H)},
			)
		}
	}

	rec.event("case-end", c.Label)
	return nil
}

// resolveAndRecordFont resolves one descriptor through the port text
// system and records its font-resolved event.
func resolveAndRecordFont(system *gpui.TextSystem, rec *recorder, label, role string, runIndex int, descriptor gpui.FontDescriptor) (gpui.FontID, error) {
	identity, err := system.ResolveFont(descriptor)
	if err != nil {
		return 0, fmt.Errorf("resolving font %q: %w", descriptor.Family, err)
	}
	recordFontResolved(rec, label, role, runIndex, descriptor, identity)
	return identity.FontID, nil
}

// appendClusterFields appends a byte-range cluster field pair (present,
// then start/end when present) onto a field list.
func appendClusterFields(fields []field, prefix string, cluster *gpui.TextRange) []field {
	if cluster == nil {
		return append(fields, field{prefix + "Present", uint64(0)})
	}
	return append(fields,
		field{prefix + "Present", uint64(1)},
		field{prefix + "Start", uint64(cluster.Start)},
		field{prefix + "End", uint64(cluster.End)},
	)
}
