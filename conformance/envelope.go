package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
)

// EnvelopeSchema identifies the fixture envelope format version. It must
// match ENVELOPE_SCHEMA in reference/harness/src/envelope.rs.
const EnvelopeSchema = "gpui-go/conformance/envelope@1"

// Envelope is a portable, semantic description of one deterministic test
// case: stable ids, capability links, the reference profile and
// fixture-specific semantic inputs. It never contains expected outputs;
// those live in recorded reference traces.
//
// The JSON field names match the Rust reference struct fields exactly
// (plain snake_case; the Rust structs derive Deserialize with no renames).
type Envelope struct {
	Schema       string   `json:"schema"`
	FixtureID    string   `json:"fixture_id"`
	FixtureKind  string   `json:"fixture_kind"`
	Title        string   `json:"title"`
	Profile      string   `json:"profile"`
	Capabilities []string `json:"capabilities"`
	// Environment dependencies the fixture declares (e.g. which platforms
	// it can run on). Recorded but not interpreted by either side.
	Environment []string      `json:"environment"`
	Inputs      FixtureInputs `json:"inputs"`
}

// FixtureInputs carries the semantic inputs shared by the fixture kinds of
// the conformance slice. Fixture kinds read only their own inputs; a kind
// that does not use a field leaves it at its zero value on the wire (the
// reference harness mirrors this with serde defaults).
type FixtureInputs struct {
	// AvailableSpace is the available space for the root layout, in
	// logical pixels (layout-effects-v1).
	AvailableSpace SizeInput `json:"available_space"`
	// StyleTree is the labeled style tree laid out by the layout section
	// (layout-effects-v1).
	StyleTree StyleNode `json:"style_tree"`
	// EffectScript is the ordered effect operations executed by the
	// effects section (layout-effects-v1).
	EffectScript []EffectOp `json:"effect_script"`
	// LayoutCases carries the per-case inputs of the layout-metrics-v1
	// fixture kind. It is nil for fixture kinds that do not use it, so
	// existing fixtures' wire shape is unaffected.
	LayoutCases *LayoutMetricsInputs `json:"layout_cases,omitempty"`
	// SceneCases carries the per-case inputs of the scene-painting-v1
	// fixture kind. It is nil for fixture kinds that do not use it, so
	// existing fixtures' wire shape is unaffected.
	SceneCases *ScenePaintingInputs `json:"scene_cases,omitempty"`
	// TextCases carries the per-case inputs of the text-geometry-v1
	// fixture kind. It is nil for fixture kinds that do not use it, so
	// existing fixtures' wire shape is unaffected.
	TextCases *TextGeometryInputs `json:"text_cases,omitempty"`
	// GlyphCases carries the per-case inputs of the glyph-raster-v1
	// fixture kind. It is nil for fixture kinds that do not use it, so
	// existing fixtures' wire shape is unaffected.
	GlyphCases *GlyphRasterInputs `json:"glyph_cases,omitempty"`
	// CounterCases carries the per-case inputs of the
	// authoring-counter-v1 fixture kind. It is nil for fixture kinds
	// that do not use it, so existing fixtures' wire shape is
	// unaffected.
	CounterCases *AuthoringCounterInputs `json:"counter_cases,omitempty"`
	// PathFilterCases carries the per-case inputs of the
	// paths-filters-v1 fixture kind (ticket16). It is nil for fixture
	// kinds that do not use it, so existing fixtures' wire shape is
	// unaffected.
	PathFilterCases *PathFilterInputs `json:"path_filter_cases,omitempty"`
}

// SizeInput is a definite size in logical pixels.
type SizeInput struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// StyleNode is one node of the labeled style tree. The keys of Style are
// parsed by the reference harness's own style parser (a documented subset
// of gpui style properties), so their spelling follows that parser, not
// this struct's field naming. The layout-metrics-v1 fixture kind reuses
// StyleNode with an extended style key set.
type StyleNode struct {
	Label    string            `json:"label"`
	Style    map[string]string `json:"style"`
	Children []StyleNode       `json:"children"`
}

// FixtureKindLayoutMetrics identifies the layout-metrics fixture kind
// ("layout-metrics-v1"). It must match the fixture kind the reference
// harness dispatches in reference/harness/src/main.rs.
const FixtureKindLayoutMetrics = "layout-metrics-v1"

// Measured spec kinds of the layout-metrics-v1 fixture kind.
const (
	// MeasuredKindFixed returns the spec size on every query.
	MeasuredKindFixed = "fixed"
	// MeasuredKindEchoKnown returns the known dimension when present, else
	// the spec size.
	MeasuredKindEchoKnown = "echo-known"
	// MeasuredKindMinMax clamps the known dimension to [floor, max] per
	// axis, else returns the floor.
	MeasuredKindMinMax = "minmax"
)

// Available-space mode names for layout-metrics cases. They match the
// availWidthTag/availHeightTag values recorded in measure-query trace
// events, and the default is AvailModeDefinite.
const (
	AvailModeDefinite   = "definite"
	AvailModeMinContent = "min-content"
	AvailModeMaxContent = "max-content"
)

// LayoutMetricsInputs carries the per-case inputs of the
// layout-metrics-v1 fixture kind. The JSON field names match the Rust
// reference struct fields exactly.
type LayoutMetricsInputs struct {
	// Cases are executed in order, each in its own window.
	Cases []LayoutMetricsCase `json:"cases"`
}

// LayoutMetricsCase is one layout-metrics case: a labeled style tree drawn
// under its own available space, rem size and optional measurement specs.
type LayoutMetricsCase struct {
	// Label identifies the case; it is recorded in the case-begin and
	// case-end trace events.
	Label string `json:"label"`
	// RemSize is the rem override in logical pixels. Nil keeps the
	// reference window default (16).
	RemSize *float64 `json:"rem_size,omitempty"`
	// AvailableSpace is the definite available space in logical pixels;
	// the per-axis mode fields select how each axis is offered to the
	// layout.
	AvailableSpace SizeInput `json:"available_space"`
	// AvailableWidthMode selects how the width axis is offered to the root
	// layout: AvailModeDefinite (default), AvailModeMinContent or
	// AvailModeMaxContent.
	AvailableWidthMode string `json:"available_width_mode,omitempty"`
	// AvailableHeightMode selects how the height axis is offered; same
	// values as AvailableWidthMode, defaulting to AvailModeDefinite.
	AvailableHeightMode string `json:"available_height_mode,omitempty"`
	// StyleTree is the labeled style tree laid out as the case's root
	// element, using the extended layout-metrics style key set.
	StyleTree *StyleNode `json:"style_tree"`
	// Measured holds deterministic measurement specs keyed by node label.
	// A node whose label appears here is laid out through the reference's
	// request_measured_layout instead of children.
	Measured map[string]MeasuredSpec `json:"measured,omitempty"`
}

// MeasuredSpec is a deterministic measurement spec for one measured node.
type MeasuredSpec struct {
	// Kind is one of MeasuredKindFixed, MeasuredKindEchoKnown or
	// MeasuredKindMinMax.
	Kind string `json:"kind"`
	// Width and Height are the fixed size (fixed, echo-known defaults) or
	// the per-axis floor (minmax), in logical pixels.
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
	// MaxWidth and MaxHeight are the per-axis maxima for minmax, in
	// logical pixels; the zero value means unbounded.
	MaxWidth  float64 `json:"max_width,omitempty"`
	MaxHeight float64 `json:"max_height,omitempty"`
}

// FixtureKindTextGeometry identifies the text-geometry fixture kind
// ("text-geometry-v1"). It must match the fixture kind the reference
// harness dispatches in reference/harness/src/main.rs.
const FixtureKindTextGeometry = "text-geometry-v1"

// FixtureKindGlyphRaster identifies the glyph-raster fixture kind
// ("glyph-raster-v1"). It must match the fixture kind the reference
// harness dispatches in reference/harness/src/main.rs.
const FixtureKindGlyphRaster = "glyph-raster-v1"

// TextGeometryInputs carries the per-case inputs of the
// text-geometry-v1 fixture kind. The JSON field names match the Rust
// reference struct fields exactly.
type TextGeometryInputs struct {
	// Cases are executed in order against one process-global reference
	// text system (the pinned Parley/Fontique stack).
	Cases []TextGeometryCase `json:"cases"`
}

// TextGeometryCase is one text-geometry case: a font descriptor, text
// and font size shaped through the reference's public shaping API, then
// queried for line, run, cluster, caret, hit-test and selection geometry.
type TextGeometryCase struct {
	// Label identifies the case; it is recorded in the case-begin and
	// case-end trace events and labels every per-case event.
	Label string `json:"label"`
	// Font is the base font descriptor; run descriptors inherit every
	// field it carries.
	Font TextFontIn `json:"font"`
	// Text is the UTF-8 source text. Every byte index in this fixture is
	// a UTF-8 byte offset (the pin's layout coordinate system).
	Text string `json:"text"`
	// FontSize is the font size in logical pixels, shared by all runs.
	FontSize float64 `json:"font_size"`
	// WrapWidth is the soft-wrap width in logical pixels; nil disables
	// wrapping.
	WrapWidth *float64 `json:"wrap_width,omitempty"`
	// LineClamp is the maximum number of visual rows; nil disables
	// clamping.
	LineClamp *uint64 `json:"line_clamp,omitempty"`
	// LineHeight overrides the line height used for every
	// caret/hit-test/selection query and the derived row placement, in
	// logical pixels; nil keeps the reference text style default (the
	// golden ratio times the font size).
	LineHeight *float64 `json:"line_height,omitempty"`
	// Runs holds the style runs. Empty means one run covering the whole
	// text with the case's font. When present, the runs must cover the
	// text exactly and end on UTF-8 character boundaries.
	Runs []TextRunIn `json:"runs,omitempty"`
	// CaretMode selects how caret queries are derived:
	// TextCaretModeExplicit (default) uses CaretQueries,
	// TextCaretModeAllClusters queries every cluster boundary with
	// downstream affinity, and TextCaretModeAllClustersBoth queries every
	// boundary with both affinities.
	CaretMode string `json:"caret_mode,omitempty"`
	// CaretQueries are the explicit caret queries: UTF-8 byte offsets
	// with an affinity (TextAffinityDownstream default).
	CaretQueries []TextCaretQuery `json:"caret_queries,omitempty"`
	// HitPoints are the hit-test points in logical pixels.
	HitPoints []TextPointIn `json:"hit_points,omitempty"`
	// Selections are the selection ranges (UTF-8 byte offsets).
	Selections []TextRangeIn `json:"selections,omitempty"`
}

// TextFontIn is a font descriptor: the public gpui Font input shape.
// The pinned descriptor has no stretch axis (family, weight and style
// only), which the fixture records as an API discovery.
type TextFontIn struct {
	// Family is the font family name; ".SystemUIFont" identifies the
	// system UI font.
	Family string `json:"family"`
	// Weight is the font weight, 100..=900; nil keeps the reference
	// default (400).
	Weight *float64 `json:"weight,omitempty"`
	// Style is "Normal" (default), "Italic" or "Oblique".
	Style string `json:"style,omitempty"`
	// Features are the OpenType feature settings of the descriptor.
	Features []TextFeatureIn `json:"features,omitempty"`
	// Fallbacks are additional fallback family names tried after the
	// main family.
	Fallbacks []string `json:"fallbacks,omitempty"`
}

// TextFeatureIn is one OpenType feature setting of a font descriptor.
type TextFeatureIn struct {
	// Tag is the four-character feature tag (e.g. "calt").
	Tag string `json:"tag"`
	// Value disables (0) or enables (1) the feature.
	Value uint32 `json:"value"`
}

// TextRunIn is one style run: a byte length plus optional font and
// letter-spacing overrides over the case's descriptor.
type TextRunIn struct {
	// Len is the number of UTF-8 bytes this run styles.
	Len uint64 `json:"len"`
	// Font holds per-field overrides merged over the case's descriptor;
	// absent fields inherit the case's.
	Font *TextFontOverrideIn `json:"font,omitempty"`
	// LetterSpacing is applied between glyphs of this run, in logical
	// pixels (TextRun.letter_spacing).
	LetterSpacing *float64 `json:"letter_spacing,omitempty"`
}

// TextFontOverrideIn is the per-field font override of one style run.
type TextFontOverrideIn struct {
	Family    *string         `json:"family,omitempty"`
	Weight    *float64        `json:"weight,omitempty"`
	Style     *string         `json:"style,omitempty"`
	Features  []TextFeatureIn `json:"features,omitempty"`
	Fallbacks []string        `json:"fallbacks,omitempty"`
}

// TextCaretQuery is one caret query: a UTF-8 byte offset plus affinity.
type TextCaretQuery struct {
	// Index is the UTF-8 byte offset queried.
	Index uint64 `json:"index"`
	// Affinity is TextAffinityDownstream (default) or
	// TextAffinityUpstream.
	Affinity string `json:"affinity,omitempty"`
}

// TextPointIn is a point input in logical pixels.
type TextPointIn struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// TextRangeIn is a UTF-8 byte range input.
type TextRangeIn struct {
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
}

// Caret query modes of the text-geometry-v1 fixture kind. They match the
// caret_mode values the reference harness dispatches.
const (
	// TextCaretModeExplicit queries the case's caret_queries.
	TextCaretModeExplicit = "explicit"
	// TextCaretModeAllClusters queries every cluster boundary with
	// downstream affinity.
	TextCaretModeAllClusters = "all-clusters"
	// TextCaretModeAllClustersBoth queries every cluster boundary with
	// both affinities, so wrap-boundary affinity is observable.
	TextCaretModeAllClustersBoth = "all-clusters-both"
)

// Caret affinity names of the text-geometry-v1 fixture kind. They match
// the CaretAffinity variants of the pinned reference.
const (
	// TextAffinityDownstream attaches the caret to the following
	// cluster.
	TextAffinityDownstream = "downstream"
	// TextAffinityUpstream attaches the caret to the preceding cluster.
	TextAffinityUpstream = "upstream"
)

// TextFontStyle values of the text-geometry-v1 fixture kind. They match
// the FontStyle variants of the pinned reference.
const (
	// TextFontStyleNormal is a face that is neither italic nor obliqued.
	TextFontStyleNormal = "Normal"
	// TextFontStyleItalic is a generally cursive face.
	TextFontStyleItalic = "Italic"
	// TextFontStyleOblique is a typically sloped regular face.
	TextFontStyleOblique = "Oblique"
)

// CanonicalFontIDBit is the high bit the pinned reference's FontStore
// sets on canonical font ids (FontStore::intern: FontId(1 << 63 | store
// index)). Recorded fontId fields carry it.
const CanonicalFontIDBit uint64 = 1 << 63

// IsTextGeometryKind reports whether the envelope declares the
// text-geometry-v1 fixture kind.
func (env *Envelope) IsTextGeometryKind() bool {
	return env != nil && env.FixtureKind == FixtureKindTextGeometry
}

// GlyphRasterInputs carries the per-case inputs of the glyph-raster-v1
// fixture kind. The JSON field names match the Rust reference struct
// fields exactly.
type GlyphRasterInputs struct {
	// Cases are executed in order against one process-global reference
	// text system constructed with the pinned DirectWrite rasterizer.
	Cases []GlyphRasterCase `json:"cases"`
}

// GlyphRasterCase is one glyph-raster case: a font, characters, sizes,
// scale factors, subpixel variants and a render mode rasterized through
// the pinned public raster API.
type GlyphRasterCase struct {
	// Label identifies the case; it is recorded in the case-begin and
	// case-end trace events and labels every per-case event.
	Label string `json:"label"`
	// Font is the case's font descriptor.
	Font TextFontIn `json:"font"`
	// Characters are mapped to glyphs before rasterization (one Unicode
	// scalar each). A character with no glyph in the resolved face records
	// the pinned None outcome (the missing-glyph case).
	Chars []string `json:"chars"`
	// Sizes are the font sizes in logical pixels.
	Sizes []float64 `json:"sizes"`
	// Scales are the scale factors (RenderGlyphParams::scale_factor).
	Scales []float64 `json:"scales"`
	// SubpixelVariants are the [x, y] subpixel variant pairs.
	SubpixelVariants [][2]uint64 `json:"subpixel_variants"`
	// Mode is the render mode: "grayscale", "subpixel" or "color".
	Mode string `json:"mode"`
	// SceneColor is [red, green, blue, alpha] in 0..1 (the
	// RasterStyleRequest scene color; required for color mode).
	SceneColor *[4]float64 `json:"scene_color,omitempty"`
}

// IsGlyphRasterKind reports whether the envelope declares the
// glyph-raster-v1 fixture kind.
func (env *Envelope) IsGlyphRasterKind() bool {
	return env != nil && env.FixtureKind == FixtureKindGlyphRaster
}

// IsLayoutMetricsKind reports whether the envelope declares the
// layout-metrics-v1 fixture kind.
func (env *Envelope) IsLayoutMetricsKind() bool {
	return env != nil && env.FixtureKind == FixtureKindLayoutMetrics
}

// Effect op wire tags. They match the kebab-case variant names of the Rust
// reference's internally tagged EffectOp enum, and are also the "op" field
// values the reference harness records in its op-begin trace events.
const (
	OpCreateEntity   = "create-entity"
	OpDropEntity     = "drop-entity"
	OpObserveRelease = "observe-release"
	OpSubscribe      = "subscribe"
	OpNotify         = "notify"
	OpEmit           = "emit"
	OpDefer          = "defer"
	OpSpawnTask      = "spawn-task"
	OpAdvanceClock   = "advance-clock"
	OpRunTasks       = "run-tasks"
	OpRunEffects     = "run-effects"
)

// effectOpRequiredFields lists the fields each Rust EffectOp variant
// requires on the wire. Optional fields (defer's schedule) are excluded.
var effectOpRequiredFields = map[string][]string{
	OpCreateEntity:   {"label", "value"},
	OpDropEntity:     {"label"},
	OpObserveRelease: {"label"},
	OpSubscribe:      {"observer", "source", "event_type"},
	OpNotify:         {"entity", "count"},
	OpEmit:           {"entity", "values"},
	OpDefer:          {"label"},
	OpSpawnTask:      {"label", "result", "delay_ms"},
	OpAdvanceClock:   {"ms"},
	OpRunTasks:       nil,
	OpRunEffects:     nil,
}

// EffectOp is one operation in a fixture's effect script. On the wire it is
// an internally tagged object, e.g.
//
//	{"op": "notify", "entity": "counter", "count": 2}
//
// matching the Rust EffectOp enum (#[serde(tag = "op", rename_all =
// "kebab-case")]). The flat struct covers the union of every variant's
// fields; op tag, required fields and value types are validated during
// unmarshaling so malformed fixtures fail loudly, mirroring the serde enum
// decoding. Marshaling always emits the tag plus the variant's required
// fields, so ops round-trip even when values are zero.
type EffectOp struct {
	Op        string   `json:"op"`
	Label     string   `json:"label,omitempty"`
	Value     uint64   `json:"value,omitempty"`
	Observer  string   `json:"observer,omitempty"`
	Source    string   `json:"source,omitempty"`
	EventType string   `json:"event_type,omitempty"`
	Entity    string   `json:"entity,omitempty"`
	Count     uint32   `json:"count,omitempty"`
	Values    []uint32 `json:"values,omitempty"`
	// Schedule is the optional label of a defer scheduled from inside this
	// defer. Rust's Option<String>: absent or null on the wire means "no
	// follow-up defer", which maps to a nil pointer here.
	Schedule *string `json:"schedule,omitempty"`
	Result   int64   `json:"result,omitempty"`
	DelayMs  uint64  `json:"delay_ms,omitempty"`
	Ms       uint64  `json:"ms,omitempty"`
}

// UnmarshalJSON decodes one effect op, rejecting unknown op tags and
// missing required fields the way the Rust enum deserializer would.
func (op *EffectOp) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("effect op: %w", err)
	}
	tagRaw, ok := raw["op"]
	if !ok {
		return fmt.Errorf("effect op: missing \"op\" tag")
	}
	var tag string
	if err := json.Unmarshal(tagRaw, &tag); err != nil {
		return fmt.Errorf("effect op: \"op\" tag is not a string: %w", err)
	}
	required, known := effectOpRequiredFields[tag]
	if !known {
		return fmt.Errorf("effect op: unknown op tag %q", tag)
	}
	for _, field := range required {
		if _, ok := raw[field]; !ok {
			return fmt.Errorf("effect op %s: missing required field %q", tag, field)
		}
	}
	type plain EffectOp // avoid recursion on the custom unmarshaler
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("effect op %s: %w", tag, err)
	}
	*op = EffectOp(p)
	return nil
}

// MarshalJSON encodes one effect op in the exact Rust wire shape: the op
// tag plus the fields of that variant (required fields always, optional
// fields only when set).
func (op EffectOp) MarshalJSON() ([]byte, error) {
	m := map[string]any{"op": op.Op}
	switch op.Op {
	case OpCreateEntity:
		m["label"] = op.Label
		m["value"] = op.Value
	case OpDropEntity, OpObserveRelease:
		m["label"] = op.Label
	case OpSubscribe:
		m["observer"] = op.Observer
		m["source"] = op.Source
		m["event_type"] = op.EventType
	case OpNotify:
		m["entity"] = op.Entity
		m["count"] = op.Count
	case OpEmit:
		if op.Values == nil {
			return nil, fmt.Errorf("effect op %s: nil values", op.Op)
		}
		m["entity"] = op.Entity
		m["values"] = op.Values
	case OpDefer:
		m["label"] = op.Label
		if op.Schedule != nil {
			m["schedule"] = *op.Schedule
		}
	case OpSpawnTask:
		m["label"] = op.Label
		m["result"] = op.Result
		m["delay_ms"] = op.DelayMs
	case OpAdvanceClock:
		m["ms"] = op.Ms
	case OpRunTasks, OpRunEffects:
		// Tag only.
	default:
		return nil, fmt.Errorf("effect op: unknown op tag %q", op.Op)
	}
	return json.Marshal(m)
}

// LoadEnvelope reads, parses and validates the fixture envelope at path.
// It fails on unreadable files, malformed JSON, wrong schema version and
// invalid effect ops.
func LoadEnvelope(path string) (*Envelope, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading envelope %s: %w", path, err)
	}
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("parsing envelope %s: %w", path, err)
	}
	if env.Schema != EnvelopeSchema {
		return nil, fmt.Errorf("unsupported envelope schema %q (expected %q) in %s", env.Schema, EnvelopeSchema, path)
	}
	return &env, nil
}

// SHA256Envelope returns the lowercase hex SHA-256 of the raw bytes of the
// envelope file at path. Envelopes are pinned by these bytes: the recorded
// trace carries this hash so any fixture edit invalidates the trace.
func SHA256Envelope(path string) (string, error) {
	sum, err := sha256File(path)
	if err != nil {
		return "", fmt.Errorf("hashing envelope: %w", err)
	}
	return sum, nil
}

// sha256File returns the lowercase hex SHA-256 of the raw bytes at path.
func sha256File(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
