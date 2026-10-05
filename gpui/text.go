package gpui

// The text geometry adapter (ticket09): the port's TextSystem over the
// native text service.
//
// The pinned GPUI-CE Windows platform exposes text through
// gpui::TextSystem wrapping a ParleyTextSystem
// (crates/gpui_windows/src/platform.rs::WindowsPlatform::new, lines
// 117-126 at 254b5dbd…): SystemFonts::Load + "Segoe UI" + the service
// fallbacks Lilex/IBM Plex Sans/Arial. The native text service
// (reference/native/src/text.rs, TEXT_ABI.md, internal/native/text.go)
// constructs exactly that stack process-globally behind its own state
// mutex and exposes its CPU geometry surface: system font enumeration,
// descriptor resolution to the canonical FontId, font metrics, shaping
// (visual lines, paint fragments with resolved font ids, positioned
// glyphs), carets, hit tests, selection rectangles and logical
// (grapheme) clusters. Glyph rasterization is a later ticket and is not
// part of this adapter.
//
// This file is that surface's Go-side seam: a process-global TextSystem
// (lazy, thread-safe — the native service serializes every entry behind
// its mutex, so the Go handle is stateless and safe for concurrent use)
// and a WrappedLine handle owning one native shaping document. The
// handle carries the shaping summary (LineLayout facts: length, font
// size, wrap/clamp constraints, line/fragment/glyph counts, width,
// ascent, descent) fetched once at shape time, mirroring the reference
// LineLayout fields the pinned public API exposes; every geometry query
// (line records, fragments, glyphs, carets, hits, selections, clusters)
// is served by the native layout through its shaping handle.
//
// Byte offsets are UTF-8 byte indices everywhere — the pin's layout
// coordinate system. UTF-16 (surrogate pairs, IME ranges) never crosses
// the native ABI and is NOT converted here; it is a later input-ticket
// concern for higher-level Go text inputs.
//
// Line height is not a shaping input in the pin (shape_text takes none):
// the caller's line height parameterizes caret/hit-test/selection
// geometry and row placement. Row i occupies
// [i*line_height, (i+1)*line_height) with baseline =
// (line_height - ascent - descent)/2 + ascent from the row top
// (crates/gpui/src/text_system/line.rs::paint_visual_text) — the native
// line entry computes that derivation, so this adapter passes the line
// height through verbatim. The reference text style's DEFAULT line
// height is the golden ratio times the font size
// (TextStyle::default().line_height = phi() = relative(GOLDEN_RATIO),
// crates/gpui/src/style.rs line 657; phi() at geometry.rs line 3833):
// DefaultLineHeight records that exact f32 factor.
//
// Handle lifecycle: shaping handles are native resources that do not
// follow Go garbage collection. A WrappedLine holds its native handle
// until Dispose; the live-handle capacity is bounded
// (internal/native's MaxShapingHandles, 256 in the native service) and
// exhaustion is the typed ErrTextShapingLimit. A disposed (or
// slot-reused) handle yields ErrTextStaleHandle from every query; a
// malformed handle yields ErrTextBadHandle. Native panics are contained
// by the service's catch_unwind boundary and surface as typed errors.

import (
	"errors"
	"fmt"
	"math"
	"sync"
	"sync/atomic"

	"gpui-go/internal/native"
)

// ---------------------------------------------------------------------------
// Line height default and font identity constants
// ---------------------------------------------------------------------------

// goldenRatio is std::f32::consts::GOLDEN_RATIO, the pinned reference
// text style's line-height factor (phi() in crates/gpui/src/geometry.rs;
// TextStyle::default().line_height = phi().into() in style.rs). The
// untyped constant rounds to the same nearest f32 Rust's constant holds.
const goldenRatio float32 = 1.6180339887498948482045868343656

// DefaultLineHeight returns the reference text style's default line
// height for a font size: the golden ratio times the size, in f32
// arithmetic (px(font_size * GOLDEN_RATIO) — the exact pinned
// derivation, e.g. 16px -> 25.888544f32, bits 0x41CF1BBD).
func DefaultLineHeight(fontSize float32) float32 {
	return fontSize * goldenRatio
}

// DefaultFontFamily is the pinned Font::default() family: the platform
// system UI font alias (crates/gpui/src/text_system.rs::font).
const DefaultFontFamily = ".SystemUIFont"

// DefaultFontWeight is FontWeight::default() (FontWeight::NORMAL, 400).
const DefaultFontWeight float32 = 400

// CanonicalFontIDBit is the high bit the pinned reference FontStore sets
// on canonical font ids (FontStore::intern: FontId(1 << 63 | index));
// resolved FontID values carry it.
const CanonicalFontIDBit uint64 = 1 << 63

// ---------------------------------------------------------------------------
// Descriptor, feature and style types
// ---------------------------------------------------------------------------

// FontStyle selects a face style: the pinned gpui::FontStyle variants.
type FontStyle uint32

const (
	// FontStyleNormal is neither italic nor obliqued.
	FontStyleNormal FontStyle = 0
	// FontStyleItalic is a generally cursive face.
	FontStyleItalic FontStyle = 1
	// FontStyleOblique is a typically sloped regular face.
	FontStyleOblique FontStyle = 2
)

// String renders the style the way the reference names its variants.
func (s FontStyle) String() string {
	switch s {
	case FontStyleNormal:
		return "Normal"
	case FontStyleItalic:
		return "Italic"
	case FontStyleOblique:
		return "Oblique"
	default:
		return fmt.Sprintf("FontStyle(%d)", uint32(s))
	}
}

// FontFeature is one OpenType feature setting: a four-character ASCII
// tag and its value (0 disables, 1 enables, at most 65535 — the pinned
// FontFeatures pairs).
type FontFeature struct {
	// Tag is the feature tag ("calt", "liga", …).
	Tag string
	// Value is the feature setting.
	Value uint32
}

// FontDescriptor selects a font for resolution, mirroring the pinned
// gpui::Font (family, features, fallbacks, weight, style — the pinned
// descriptor has no stretch axis). The pinned resolve chain tries the
// family, then the descriptor fallbacks, then the service fallbacks
// (Lilex, IBM Plex Sans, Arial).
type FontDescriptor struct {
	// Family is the font family name; ".SystemUIFont" identifies the
	// system UI font.
	Family string
	// Weight is the font weight (100..900; 400 is normal).
	Weight float32
	// Style selects normal/italic/oblique.
	Style FontStyle
	// Features are the descriptor's OpenType feature settings.
	Features []FontFeature
	// Fallbacks are additional fallback family names tried after the
	// main family. An empty list is the pinned None (no fallbacks).
	Fallbacks []string
}

// DefaultFontDescriptor returns the pinned Font::default(): the system
// UI font alias, normal weight and style, no features, no fallbacks.
func DefaultFontDescriptor() FontDescriptor {
	return FontDescriptor{
		Family: DefaultFontFamily,
		Weight: DefaultFontWeight,
		Style:  FontStyleNormal,
	}
}

// Equal reports whether two descriptors select the same pinned Font
// (the reference's derived PartialEq: family, weight, style, the feature
// pairs and the fallback list; an empty fallback list equals an absent
// one because the pinned builder maps both to None).
func (d FontDescriptor) Equal(other FontDescriptor) bool {
	if d.Family != other.Family || d.Weight != other.Weight || d.Style != other.Style {
		return false
	}
	if len(d.Features) != len(other.Features) {
		return false
	}
	for i, feature := range d.Features {
		if other.Features[i] != feature {
			return false
		}
	}
	if len(d.Fallbacks) != len(other.Fallbacks) {
		return false
	}
	for i, family := range d.Fallbacks {
		if other.Fallbacks[i] != family {
			return false
		}
	}
	return true
}

func (d FontDescriptor) toNative() native.FontDescriptor {
	features := make([]native.FontFeature, len(d.Features))
	for i, feature := range d.Features {
		features[i] = native.FontFeature{Tag: feature.Tag, Value: feature.Value}
	}
	return native.FontDescriptor{
		Family:    d.Family,
		Weight:    d.Weight,
		Style:     native.TextFontStyle(d.Style),
		Features:  features,
		Fallbacks: d.Fallbacks,
	}
}

// ---------------------------------------------------------------------------
// Resolved identity and metrics
// ---------------------------------------------------------------------------

// FontID is the pinned canonical font identity handle (bit 63 set, the
// FontStore index below). Resolved ids are process-stable: the store
// only interns, so fonts have no dispose.
type FontID uint64

// IsCanonical reports whether the id carries the canonical high bit.
func (id FontID) IsCanonical() bool { return uint64(id)&CanonicalFontIDBit != 0 }

// FontIdentity is a resolved descriptor's identity record: the canonical
// FontId plus the echoed descriptor facts (the pinned public surface
// resolves a descriptor to the FontId; the selected face's index, data
// identity and variation axes are not observable through it).
type FontIdentity struct {
	// FontID is the canonical resolved identity.
	FontID FontID
	// Generation is the font catalog generation at registration (the
	// registration-batch counter; 0 with SystemFonts::Load).
	Generation uint64
	// Weight echoes the requested weight.
	Weight float32
	// Style echoes the requested style.
	Style FontStyle
	// FeatureCount echoes the requested feature count.
	FeatureCount int
	// FallbackCount echoes the requested fallback count.
	FallbackCount int
}

// FontMetrics is the pinned gpui::FontMetrics in font units: the full
// face record the native service returns for any font it knows
// (resolved ids and every font Parley selected while shaping).
type FontMetrics struct {
	// UnitsPerEm is the number of font units per em square.
	UnitsPerEm uint32
	// Ascent is the distance above the baseline (positive up).
	Ascent float32
	// Descent is the distance below the baseline (positive down).
	Descent float32
	// LineGap is the recommended additional space between lines.
	LineGap float32
	// UnderlinePosition is the suggested underline position.
	UnderlinePosition float32
	// UnderlineThickness is the suggested underline thickness.
	UnderlineThickness float32
	// CapHeight is the capital-letter height above the baseline.
	CapHeight float32
	// XHeight is the lowercase-x height above the baseline.
	XHeight float32
	// BoundingBoxX is the glyph bounding box origin x.
	BoundingBoxX float32
	// BoundingBoxY is the glyph bounding box origin y.
	BoundingBoxY float32
	// BoundingBoxWidth is the glyph bounding box width.
	BoundingBoxWidth float32
	// BoundingBoxHeight is the glyph bounding box height.
	BoundingBoxHeight float32
}

// AscentPx scales the ascent to a font size with the pinned
// FontMetrics::ascent arithmetic: (ascent / units_per_em) * font_size,
// in f32 (TextSystem::ascent).
func (m FontMetrics) AscentPx(fontSize float32) float32 {
	return (m.Ascent / float32(m.UnitsPerEm)) * fontSize
}

// DescentPx scales the descent to a font size (FontMetrics::descent).
func (m FontMetrics) DescentPx(fontSize float32) float32 {
	return (m.Descent / float32(m.UnitsPerEm)) * fontSize
}

// XHeightPx scales the x-height to a font size (FontMetrics::x_height).
func (m FontMetrics) XHeightPx(fontSize float32) float32 {
	return (m.XHeight / float32(m.UnitsPerEm)) * fontSize
}

// ---------------------------------------------------------------------------
// Errors
// ---------------------------------------------------------------------------

// Text errors, distinguishable with errors.Is. They wrap the native
// text service's status errors (internal/native) with this seam's
// context, mapping each native sentinel to its gpui counterpart.
var (
	// ErrTextService reports a failure to load/validate the native text
	// service.
	ErrTextService = errors.New("gpui: native text service unavailable")
	// ErrTextStaleHandle reports a shaping handle from a past generation
	// (disposed, or its native slot was reused).
	ErrTextStaleHandle = errors.New("gpui: text shaping handle is stale")
	// ErrTextBadHandle reports a handle that never existed or is
	// malformed, or a font id that is not canonical/registered.
	ErrTextBadHandle = errors.New("gpui: text handle is invalid")
	// ErrTextValue reports a request the adapter or the native service
	// rejected (run coverage, byte boundary, feature tag/value, bound or
	// non-finite float).
	ErrTextValue = errors.New("gpui: text value rejected")
	// ErrTextFontUnresolved reports that the whole pinned fallback chain
	// failed for a descriptor.
	ErrTextFontUnresolved = errors.New("gpui: font unresolved")
	// ErrTextShapingLimit reports the native live shaping-handle
	// capacity is exhausted.
	ErrTextShapingLimit = errors.New("gpui: text shaping handle limit exceeded")
)

// wrapTextError maps a native text error to the gpui seam's context.
func wrapTextError(op string, err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, native.ErrTextStaleHandle):
		return fmt.Errorf("gpui: %s: %w", op, ErrTextStaleHandle)
	case errors.Is(err, native.ErrTextBadHandle):
		return fmt.Errorf("gpui: %s: %w", op, ErrTextBadHandle)
	case errors.Is(err, native.ErrTextBadValue):
		return fmt.Errorf("gpui: %s: %w", op, ErrTextValue)
	case errors.Is(err, native.ErrTextFontUnresolved):
		return fmt.Errorf("gpui: %s: %w", op, ErrTextFontUnresolved)
	case errors.Is(err, native.ErrTextShapingLimit):
		return fmt.Errorf("gpui: %s: %w", op, ErrTextShapingLimit)
	default:
		return fmt.Errorf("gpui: %s: %w", op, err)
	}
}

// ---------------------------------------------------------------------------
// The text system
// ---------------------------------------------------------------------------

// TextSystem is the port's text system: the typed Go seam over the
// process-global native text service (the pinned Parley/Fontique stack
// constructed like the pinned Windows platform). It is created lazily
// on first use and stays resident for the process lifetime (the
// distribution contract: no FreeLibrary, no hot unload).
//
// A TextSystem is stateless beyond its native service handle and is
// safe for concurrent use: the native service serializes every entry
// behind its own state mutex.
type TextSystem struct {
	svc *native.TextService
}

var (
	textSystemOnce sync.Once
	textSystemVal  *TextSystem
	textSystemErr  error
)

// DefaultTextSystem returns the process-global text system, creating it
// (and the native text stack behind it) on first use. Every call returns
// the same instance; the error is remembered from the failed attempt.
func DefaultTextSystem() (*TextSystem, error) {
	textSystemOnce.Do(func() {
		lib, err := native.Load(native.Options{})
		if err != nil {
			textSystemErr = fmt.Errorf("gpui: %w: loading the native artifact: %w", ErrTextService, err)
			return
		}
		svc, err := lib.Text()
		if err != nil {
			textSystemErr = fmt.Errorf("gpui: %w: %w", ErrTextService, err)
			return
		}
		textSystemVal = &TextSystem{svc: svc}
	})
	return textSystemVal, textSystemErr
}

// MaxShapingHandles is the native live shaping-handle bound (256).
func (ts *TextSystem) MaxShapingHandles() int {
	return ts.svc.MaxShapingHandles()
}

// FontNames returns the system font family names: the pinned
// all_font_names (catalog families plus ".SystemUIFont", sorted,
// deduplicated). The first call creates the native text stack
// (Fontique's DirectWrite-backed system font enumeration).
func (ts *TextSystem) FontNames() ([]string, error) {
	names, err := ts.svc.FontNames()
	if err != nil {
		return nil, wrapTextError("FontNames", err)
	}
	return names, nil
}

// ResolveFont resolves a descriptor through the pinned
// resolve_canonical_font chain (family, descriptor fallbacks, then the
// service fallbacks Lilex/IBM Plex Sans/Arial). The returned FontID is
// process-stable: the store only interns, and resolving the same
// descriptor again yields the same id.
func (ts *TextSystem) ResolveFont(descriptor FontDescriptor) (FontIdentity, error) {
	if descriptor.Family == "" {
		return FontIdentity{}, fmt.Errorf("gpui: ResolveFont: %w: empty family", ErrTextValue)
	}
	identity, err := ts.svc.ResolveFont(descriptor.toNative())
	if err != nil {
		return FontIdentity{}, wrapTextError("ResolveFont", err)
	}
	return FontIdentity{
		FontID:        FontID(identity.FontID),
		Generation:    identity.Generation,
		Weight:        identity.Weight,
		Style:         FontStyle(identity.Style),
		FeatureCount:  identity.FeatureCount,
		FallbackCount: identity.FallbackCount,
	}, nil
}

// FontMetrics returns the full face metrics (font units) for a font the
// service knows: resolved ids and every font Parley selected while
// shaping (fragment font ids are registered at shape time).
func (ts *TextSystem) FontMetrics(fontID FontID) (FontMetrics, error) {
	record, err := ts.svc.FontMetrics(native.FontID(fontID))
	if err != nil {
		return FontMetrics{}, wrapTextError("FontMetrics", err)
	}
	return FontMetrics{
		UnitsPerEm:         record.UnitsPerEm,
		Ascent:             record.Ascent,
		Descent:            record.Descent,
		LineGap:            record.LineGap,
		UnderlinePosition:  record.UnderlinePosition,
		UnderlineThickness: record.UnderlineThickness,
		CapHeight:          record.CapHeight,
		XHeight:            record.XHeight,
		BoundingBoxX:       record.BoundingBoxX,
		BoundingBoxY:       record.BoundingBoxY,
		BoundingBoxWidth:   record.BoundingBoxWidth,
		BoundingBoxHeight:  record.BoundingBoxHeight,
	}, nil
}

// ---------------------------------------------------------------------------
// Shaping
// ---------------------------------------------------------------------------

// TextRun is one style run of a shaped document, mirroring the pinned
// gpui::TextRun: a byte length, the run's font descriptor and an
// optional letter spacing. Runs must exactly cover the shaped text on
// UTF-8 character boundaries.
type TextRun struct {
	// Len is this run's length in UTF-8 bytes.
	Len int
	// Font is the run's font descriptor.
	Font FontDescriptor
	// LetterSpacing, when non-nil, is applied between the run's glyphs
	// (pixels).
	LetterSpacing *float32
}

// TextLayoutRequest is one shaping document: the text, its shared font
// size, the style runs and the optional wrap/clamp constraints. This is
// the pinned TextLayoutRequest the platform's layout_text receives
// (shape_text's wrapped/clamped path).
type TextLayoutRequest struct {
	// Text is the UTF-8 source.
	Text string
	// FontSize is the shared font size in pixels.
	FontSize float32
	// Runs style the text and must exactly cover it.
	Runs []TextRun
	// WrapWidth, when non-nil, enables soft wrapping at that width.
	WrapWidth *float32
	// LineClamp, when non-nil, caps the visual row count (>= 1).
	LineClamp *uint32
}

// ShapeText shapes the request through the native text service (the
// pinned PlatformTextSystem::layout_text call) and returns the wrapped
// line handle carrying the shaping summary.
func (ts *TextSystem) ShapeText(request TextLayoutRequest) (*WrappedLine, error) {
	if len(request.Runs) == 0 {
		return nil, fmt.Errorf("gpui: ShapeText: %w: at least one run is required", ErrTextValue)
	}
	if !finiteFloat32(request.FontSize) {
		return nil, fmt.Errorf("gpui: ShapeText: %w: font size must be finite", ErrTextValue)
	}
	if request.WrapWidth != nil && (!finiteFloat32(*request.WrapWidth) || *request.WrapWidth < 0) {
		return nil, fmt.Errorf("gpui: ShapeText: %w: wrap width must be finite and >= 0", ErrTextValue)
	}
	if request.LineClamp != nil && *request.LineClamp < 1 {
		return nil, fmt.Errorf("gpui: ShapeText: %w: line clamp must be >= 1", ErrTextValue)
	}
	specs := make([]native.TextRunSpec, len(request.Runs))
	for i, run := range request.Runs {
		var letterSpacing *float32
		if run.LetterSpacing != nil {
			if !finiteFloat32(*run.LetterSpacing) {
				return nil, fmt.Errorf("gpui: ShapeText: %w: run %d letter spacing must be finite", ErrTextValue, i)
			}
			spacing := *run.LetterSpacing
			letterSpacing = &spacing
		}
		specs[i] = native.TextRunSpec{
			Len:           run.Len,
			Family:        run.Font.Family,
			Weight:        run.Font.Weight,
			Style:         native.TextFontStyle(run.Font.Style),
			Features:      featuresToNative(run.Font.Features),
			Fallbacks:     run.Font.Fallbacks,
			LetterSpacing: letterSpacing,
		}
	}
	handle, err := ts.svc.Shape(native.ShapeRequest{
		Text:      request.Text,
		FontSize:  request.FontSize,
		Runs:      specs,
		WrapWidth: request.WrapWidth,
		LineClamp: request.LineClamp,
	})
	if err != nil {
		return nil, wrapTextError("ShapeText", err)
	}
	line := &WrappedLine{system: ts, handle: handle}
	if err := line.refreshSummary(); err != nil {
		_ = line.Dispose()
		return nil, err
	}
	return line, nil
}

// featuresToNative converts feature settings to the native records.
func featuresToNative(features []FontFeature) []native.FontFeature {
	if len(features) == 0 {
		return nil
	}
	records := make([]native.FontFeature, len(features))
	for i, feature := range features {
		records[i] = native.FontFeature{Tag: feature.Tag, Value: feature.Value}
	}
	return records
}

// finiteFloat32 reports whether v is neither NaN nor infinite.
func finiteFloat32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// ---------------------------------------------------------------------------
// The wrapped line handle
// ---------------------------------------------------------------------------

// TextLayoutSummary is the shaping summary of one wrapped line: the
// pinned LineLayout facts (len, font_size, width, ascent, descent, the
// line/fragment/glyph counts) plus the active wrap/clamp constraints.
type TextLayoutSummary struct {
	// TextLen is the shaped text's UTF-8 length.
	TextLen int
	// FontSize is the document's font size.
	FontSize float32
	// WrapWidth is the active wrap constraint (nil when unwrapped).
	WrapWidth *float32
	// LineClamp is the active row clamp (nil when unclamped).
	LineClamp *uint32
	// LineCount is the number of visual lines.
	LineCount int
	// FragmentCount is the number of paint fragments (shaped runs).
	FragmentCount int
	// GlyphCount is the total positioned glyphs across all fragments.
	GlyphCount int
	// Width is the widest visual line's advance.
	Width float32
	// Ascent is the document's ascent.
	Ascent float32
	// Descent is the document's descent.
	Descent float32
	// FontGeneration is the font catalog generation at layout time.
	FontGeneration uint32
}

// TextLine is one visual row: the pinned VisualLine facts
// (text_range, fragment_range, advance_width) plus the row's placement
// under a line height: row i occupies
// [i*LineHeight, (i+1)*LineHeight) vertically and [0, AdvanceWidth)
// horizontally before paint-time alignment, with Baseline =
// (LineHeight - Ascent - Descent)/2 + Ascent from the row top (the
// pinned paint_visual_text derivation, computed natively).
type TextLine struct {
	// Index is the row's visual index.
	Index int
	// TextStart is the row's logical UTF-8 range start.
	TextStart int
	// TextEnd is the row's logical UTF-8 range end (a trailing paragraph
	// separator byte stays in its paragraph's last row).
	TextEnd int
	// FragmentStart is the row's first paint-fragment index.
	FragmentStart int
	// FragmentEnd is one-past the row's last paint-fragment index.
	FragmentEnd int
	// AdvanceWidth is the row's shaped content advance.
	AdvanceWidth float32
	// LineHeight echoes the query's line height.
	LineHeight float32
	// Baseline is the baseline offset from the row's top edge.
	Baseline float32
}

// TextRect is a geometry rectangle in layout pixels.
type TextRect struct {
	// X and Y are the rectangle's origin.
	X, Y float32
	// W and H are the rectangle's extents.
	W, H float32
}

// TextRange is a UTF-8 byte range.
type TextRange struct {
	// Start is the range's first byte.
	Start int
	// End is one past the range's last byte.
	End int
}

// TextFragment is one shaped run: the resolved canonical font, its
// size, the fragment's x extent and its glyph dump slice (the pinned
// PaintFragment's public facts — fragment byte ranges are not public in
// the pin).
type TextFragment struct {
	// FontID is the run's resolved canonical font.
	FontID FontID
	// FontSize is the run's resolved font size.
	FontSize float32
	// XStart and XEnd are the fragment's x range in the line.
	XStart, XEnd float32
	// GlyphStart is the fragment's first glyph in the glyph dump.
	GlyphStart int
	// GlyphCount is the fragment's glyph count.
	GlyphCount int
}

// TextGlyph is one positioned glyph: line-local x and
// baseline-relative y (the painter adds baseline_y + Y).
type TextGlyph struct {
	// ID is the glyph id.
	ID uint32
	// X is the glyph's line-local x position.
	X float32
	// Y is the glyph's baseline-relative y position.
	Y float32
	// IsEmoji reports color artwork the rasterizer supports.
	IsEmoji bool
}

// CaretAffinity selects which logical neighbor owns a caret at a shared
// boundary: the pinned CaretAffinity variants.
type CaretAffinity uint32

const (
	// CaretAffinityDownstream attaches the caret to the following
	// cluster.
	CaretAffinityDownstream CaretAffinity = 0
	// CaretAffinityUpstream attaches the caret to the preceding
	// cluster.
	CaretAffinityUpstream CaretAffinity = 1
)

// String renders the affinity the way the reference names its variants.
func (a CaretAffinity) String() string {
	switch a {
	case CaretAffinityDownstream:
		return "downstream"
	case CaretAffinityUpstream:
		return "upstream"
	default:
		return fmt.Sprintf("CaretAffinity(%d)", uint32(a))
	}
}

// TextCaret is the caret geometry for a byte position: the full bounds
// (the pinned caret_bounds; Present is false when it returns None) plus
// the adjacent logical clusters.
type TextCaret struct {
	// Index is the caret's UTF-8 byte index.
	Index int
	// Affinity is the caret's affinity (echo).
	Affinity CaretAffinity
	// Present reports whether caret geometry exists.
	Present bool
	// Bounds is the caret rectangle (zero when not present).
	Bounds TextRect
	// ClusterBefore is the cluster preceding the caret, when any.
	ClusterBefore *TextRange
	// ClusterAfter is the cluster following the caret, when any.
	ClusterAfter *TextRange
}

// TextHit is a hit-test result: the closest caret for a point, with
// Inside reporting whether the point fell inside a visual row (the
// pinned Ok; outside points carry the edge caret, the pinned Err).
type TextHit struct {
	// Index is the closest caret's UTF-8 byte index.
	Index int
	// Affinity is the closest caret's affinity.
	Affinity CaretAffinity
	// Inside reports whether the point was inside a visual row.
	Inside bool
}

// WrappedLine is a shaped document handle: the shaping summary plus
// every geometry query the pinned WrappedLineLayout exposes, served by
// the native layout through its shaping handle.
//
// A WrappedLine is safe for concurrent use (the native service
// serializes its entries); it is a native resource that does not follow
// Go garbage collection — Dispose releases it, after which every query
// fails with ErrTextStaleHandle. Relayout re-shapes the same text under
// new wrap/clamp constraints.
type WrappedLine struct {
	system  *TextSystem
	handle  native.ShapingID
	summary TextLayoutSummary

	// clustersMu guards the lazily enumerated logical clusters (needed
	// for beyond-end caret/cluster queries; the clusters are pure text
	// facts, so the cache survives relayout).
	clustersMu    sync.Mutex
	clustersCache []TextRange

	// disposed marks a locally disposed handle so pre-native-entry
	// guards can fail fast with the stale-handle error.
	disposed atomic.Bool
}

// summary returns the shaping summary, failing fast on a disposed line.
func (l *WrappedLine) checkLive(op string) error {
	if l == nil || l.disposed.Load() {
		return fmt.Errorf("gpui: %s: %w", op, ErrTextStaleHandle)
	}
	return nil
}

// refreshSummary re-reads the native layout summary (after shape and
// after relayout).
func (l *WrappedLine) refreshSummary() error {
	layout, err := l.system.svc.LayoutInfo(l.handle)
	if err != nil {
		return wrapTextError("layout summary", err)
	}
	summary := TextLayoutSummary{
		TextLen:        layout.TextLen,
		FontSize:       layout.FontSize,
		LineCount:      layout.LineCount,
		FragmentCount:  layout.FragmentCount,
		GlyphCount:     layout.GlyphCount,
		Width:          layout.Width,
		Ascent:         layout.Ascent,
		Descent:        layout.Descent,
		FontGeneration: layout.FontGeneration,
	}
	if layout.WrapWidth != nil {
		wrap := *layout.WrapWidth
		summary.WrapWidth = &wrap
	}
	if layout.LineClamp != nil {
		clamp := *layout.LineClamp
		summary.LineClamp = &clamp
	}
	l.summary = summary
	return nil
}

// Summary returns the shaping summary (the pinned LineLayout facts).
func (l *WrappedLine) Summary() (TextLayoutSummary, error) {
	if err := l.checkLive("Summary"); err != nil {
		return TextLayoutSummary{}, err
	}
	return l.summary, nil
}

// Len returns the length of the shaped text in UTF-8 bytes.
func (l *WrappedLine) Len() (int, error) {
	if err := l.checkLive("Len"); err != nil {
		return 0, err
	}
	return l.summary.TextLen, nil
}

// LineCount returns the number of visual lines.
func (l *WrappedLine) LineCount() (int, error) {
	if err := l.checkLive("LineCount"); err != nil {
		return 0, err
	}
	return l.summary.LineCount, nil
}

// Line returns one visual row's record under the given line height,
// with the row placement the pinned paint path derives.
func (l *WrappedLine) Line(index int, lineHeight float32) (TextLine, error) {
	if err := l.checkLive("Line"); err != nil {
		return TextLine{}, err
	}
	record, err := l.system.svc.Line(l.handle, index, lineHeight)
	if err != nil {
		return TextLine{}, wrapTextError("Line", err)
	}
	return TextLine{
		Index:         record.Index,
		TextStart:     record.TextStart,
		TextEnd:       record.TextEnd,
		FragmentStart: record.FragmentStart,
		FragmentEnd:   record.FragmentEnd,
		AdvanceWidth:  record.AdvanceWidth,
		LineHeight:    record.LineHeight,
		Baseline:      record.Baseline,
	}, nil
}

// Fragments dumps the paint fragments in visual order.
func (l *WrappedLine) Fragments() ([]TextFragment, error) {
	if err := l.checkLive("Fragments"); err != nil {
		return nil, err
	}
	records, err := l.system.svc.Fragments(l.handle)
	if err != nil {
		return nil, wrapTextError("Fragments", err)
	}
	fragments := make([]TextFragment, len(records))
	for i, record := range records {
		fragments[i] = TextFragment{
			FontID:     FontID(record.FontID),
			FontSize:   record.FontSize,
			XStart:     record.XStart,
			XEnd:       record.XEnd,
			GlyphStart: record.GlyphStart,
			GlyphCount: record.GlyphCount,
		}
	}
	return fragments, nil
}

// Glyphs dumps the positioned glyphs (line-local x, baseline-relative
// y) in fragment order.
func (l *WrappedLine) Glyphs() ([]TextGlyph, error) {
	if err := l.checkLive("Glyphs"); err != nil {
		return nil, err
	}
	records, err := l.system.svc.Glyphs(l.handle)
	if err != nil {
		return nil, wrapTextError("Glyphs", err)
	}
	glyphs := make([]TextGlyph, len(records))
	for i, record := range records {
		glyphs[i] = TextGlyph{
			ID:      record.ID,
			X:       record.X,
			Y:       record.Y,
			IsEmoji: record.IsEmoji,
		}
	}
	return glyphs, nil
}

// Caret returns the caret geometry for a UTF-8 byte index: the full
// bounds plus the adjacent logical (grapheme) clusters.
//
// An index past the text end answers with the pinned public behavior —
// caret_bounds returns None (Present false) while the adjacent logical
// clusters are still the graphemes around the position — instead of the
// native ABI's typed caller error. An index inside a UTF-8 sequence is
// a typed error (the ABI's documented boundary validation; the pinned
// caret_bounds would snap it to the containing cluster, which no
// recorded fixture exercises).
func (l *WrappedLine) Caret(byteIndex int, affinity CaretAffinity, lineHeight float32) (TextCaret, error) {
	if err := l.checkLive("Caret"); err != nil {
		return TextCaret{}, err
	}
	if err := checkCaretIndex(byteIndex); err != nil {
		return TextCaret{}, fmt.Errorf("gpui: Caret: %w", err)
	}
	if byteIndex > l.summary.TextLen {
		before, err := l.logicalClusterBefore(byteIndex)
		if err != nil {
			return TextCaret{}, err
		}
		after, err := l.logicalClusterAfter(byteIndex)
		if err != nil {
			return TextCaret{}, err
		}
		caret := TextCaret{Index: byteIndex, Affinity: affinity, Present: false}
		if before.Present {
			caret.ClusterBefore = &before.Range
		}
		if after.Present {
			caret.ClusterAfter = &after.Range
		}
		return caret, nil
	}
	record, err := l.system.svc.Caret(l.handle, byteIndex, native.CaretAffinity(affinity), lineHeight)
	if err != nil {
		return TextCaret{}, wrapTextError("Caret", err)
	}
	caret := TextCaret{
		Index:    record.Index,
		Affinity: CaretAffinity(record.Affinity),
		Present:  record.Present,
		Bounds: TextRect{
			X: record.Bounds.X,
			Y: record.Bounds.Y,
			W: record.Bounds.W,
			H: record.Bounds.H,
		},
	}
	if record.ClusterBefore != nil {
		caret.ClusterBefore = &TextRange{Start: record.ClusterBefore.Start, End: record.ClusterBefore.End}
	}
	if record.ClusterAfter != nil {
		caret.ClusterAfter = &TextRange{Start: record.ClusterAfter.Start, End: record.ClusterAfter.End}
	}
	return caret, nil
}

// HitTest returns the closest caret for a point in layout coordinates
// (the pinned closest_caret_for_pixel_point: Ok = inside a visual row,
// Err = the edge caret for an outside point).
func (l *WrappedLine) HitTest(x, y, lineHeight float32) (TextHit, error) {
	if err := l.checkLive("HitTest"); err != nil {
		return TextHit{}, err
	}
	record, err := l.system.svc.HitTest(l.handle, x, y, lineHeight)
	if err != nil {
		return TextHit{}, wrapTextError("HitTest", err)
	}
	return TextHit{
		Index:    record.Index,
		Affinity: CaretAffinity(record.Affinity),
		Inside:   record.Inside,
	}, nil
}

// ByteIndexForPixelPoint mirrors the pinned byte_index_for_pixel_point:
// the logical start of the cluster under the point when the point is
// inside a visual row, and the closest caret's index (the boundary at
// that visual edge) when it is outside. The boolean reports the inside
// case.
//
// Derivation (recorded in the native ABI's output-surface notes): the
// native hit test reports the closest caret, whose affinity encodes the
// hit cluster — a downstream caret sits at a cluster START (the
// byte index), and an upstream caret sits at a cluster END, whose
// cluster starts at the preceding logical cluster's start. The port
// recovers the cluster start from the caret through the grapheme
// clusters the native cluster queries expose (the pin's documented
// logical clusters). This matches the reference for every recorded
// fx-0004 hit; it can differ from the reference's shaping-cluster start
// only for an upstream caret whose preceding shaping cluster is a
// strict sub-cluster of its grapheme (ZWJ-emoji splits), which the
// recorded fixture does not exercise.
func (l *WrappedLine) ByteIndexForPixelPoint(x, y, lineHeight float32) (int, bool, error) {
	if err := l.checkLive("ByteIndexForPixelPoint"); err != nil {
		return 0, false, err
	}
	hit, err := l.HitTest(x, y, lineHeight)
	if err != nil {
		return 0, false, err
	}
	if !hit.Inside || hit.Affinity == CaretAffinityDownstream {
		// Outside points carry the edge caret's index; downstream
		// carets sit at the hit cluster's start.
		return hit.Index, hit.Inside, nil
	}
	// An upstream caret sits at the hit cluster's end: the cluster
	// starts where the preceding logical cluster starts.
	cluster, err := l.ClusterBefore(hit.Index, hit.Affinity)
	if err != nil {
		return 0, false, err
	}
	if !cluster.Present {
		return hit.Index, true, nil
	}
	return cluster.Range.Start, true, nil
}

// SelectionRects returns the visual-order rectangles covering a UTF-8
// byte range under a line height (empty ranges produce none; rectangle
// heights are exactly the line height).
func (l *WrappedLine) SelectionRects(start, end int, lineHeight float32) ([]TextRect, error) {
	if err := l.checkLive("SelectionRects"); err != nil {
		return nil, err
	}
	records, err := l.system.svc.SelectionRects(l.handle, start, end, lineHeight)
	if err != nil {
		return nil, wrapTextError("SelectionRects", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	rects := make([]TextRect, len(records))
	for i, record := range records {
		rects[i] = TextRect{X: record.X, Y: record.Y, W: record.W, H: record.H}
	}
	return rects, nil
}

// ClusterBefore returns the logical cluster (grapheme) preceding a byte
// position, when one exists. In this pin logical clusters are
// graphemes: one backend-defined caret step. An index past the text end
// answers with the last cluster whose start precedes it (the pinned
// pure-grapheme semantics) instead of the native ABI's caller error.
func (l *WrappedLine) ClusterBefore(byteIndex int, affinity CaretAffinity) (TextCluster, error) {
	if err := l.checkLive("ClusterBefore"); err != nil {
		return TextCluster{}, err
	}
	if err := checkCaretIndex(byteIndex); err != nil {
		return TextCluster{}, fmt.Errorf("gpui: ClusterBefore: %w", err)
	}
	if byteIndex > l.summary.TextLen {
		return l.logicalClusterBefore(byteIndex)
	}
	record, err := l.system.svc.Cluster(l.handle, byteIndex, native.CaretAffinity(affinity), native.ClusterBefore)
	if err != nil {
		return TextCluster{}, wrapTextError("ClusterBefore", err)
	}
	return TextCluster{Present: record.Present, Range: TextRange{Start: record.Range.Start, End: record.Range.End}}, nil
}

// ClusterAfter returns the logical cluster (grapheme) following a byte
// position, when one exists. An index past the text end answers with
// the first cluster whose end follows it, or none.
func (l *WrappedLine) ClusterAfter(byteIndex int, affinity CaretAffinity) (TextCluster, error) {
	if err := l.checkLive("ClusterAfter"); err != nil {
		return TextCluster{}, err
	}
	if err := checkCaretIndex(byteIndex); err != nil {
		return TextCluster{}, fmt.Errorf("gpui: ClusterAfter: %w", err)
	}
	if byteIndex > l.summary.TextLen {
		return l.logicalClusterAfter(byteIndex)
	}
	record, err := l.system.svc.Cluster(l.handle, byteIndex, native.CaretAffinity(affinity), native.ClusterAfter)
	if err != nil {
		return TextCluster{}, wrapTextError("ClusterAfter", err)
	}
	return TextCluster{Present: record.Present, Range: TextRange{Start: record.Range.Start, End: record.Range.End}}, nil
}

// checkCaretIndex validates a caret/cluster query's byte index: a
// negative index is a caller error; indices past the text end are
// valid public queries answered with the pinned None semantics by the
// callers.
func checkCaretIndex(byteIndex int) error {
	if byteIndex < 0 {
		return fmt.Errorf("%w: byte index %d is negative", ErrTextValue, byteIndex)
	}
	return nil
}

// logicalClusterBefore answers a beyond-end cluster-before query from
// the enumerated graphemes: the last cluster whose start precedes the
// index (the pinned logical_cluster_before is a pure grapheme query).
func (l *WrappedLine) logicalClusterBefore(byteIndex int) (TextCluster, error) {
	clusters, err := l.clusters()
	if err != nil {
		return TextCluster{}, err
	}
	for i := len(clusters) - 1; i >= 0; i-- {
		if clusters[i].Start < byteIndex {
			return TextCluster{Present: true, Range: clusters[i]}, nil
		}
	}
	return TextCluster{}, nil
}

// logicalClusterAfter answers a beyond-end cluster-after query from the
// enumerated graphemes: the first cluster whose end follows the index.
func (l *WrappedLine) logicalClusterAfter(byteIndex int) (TextCluster, error) {
	clusters, err := l.clusters()
	if err != nil {
		return TextCluster{}, err
	}
	for _, cluster := range clusters {
		if cluster.End > byteIndex {
			return TextCluster{Present: true, Range: cluster}, nil
		}
	}
	return TextCluster{}, nil
}

// clusters returns the cached logical-cluster (grapheme) enumeration.
func (l *WrappedLine) clusters() ([]TextRange, error) {
	l.clustersMu.Lock()
	defer l.clustersMu.Unlock()
	if l.clustersCache != nil {
		return l.clustersCache, nil
	}
	clusters, err := l.clustersUncached()
	if err != nil {
		return nil, err
	}
	l.clustersCache = clusters
	return clusters, nil
}

// Clusters enumerates the document's logical clusters (graphemes) in
// document order, walking the public cluster API exactly as the
// reference fixture does: from the caret attached to the next cluster
// at 0, stepping each returned cluster's end.
func (l *WrappedLine) Clusters() ([]TextRange, error) {
	if err := l.checkLive("Clusters"); err != nil {
		return nil, err
	}
	return l.clusters()
}

// clustersUncached performs the cluster enumeration without the cache
// lock (the caller holds clustersMu).
func (l *WrappedLine) clustersUncached() ([]TextRange, error) {
	var clusters []TextRange
	index := 0
	for {
		cluster, err := l.system.svc.Cluster(l.handle, index, native.CaretDownstream, native.ClusterAfter)
		if err != nil {
			return nil, wrapTextError("Clusters", err)
		}
		if !cluster.Present {
			return clusters, nil
		}
		if cluster.Range.End <= index {
			return nil, fmt.Errorf("gpui: Clusters: %w: logical cluster %d..%d does not advance caret %d", ErrTextValue, cluster.Range.Start, cluster.Range.End, index)
		}
		clusters = append(clusters, TextRange{Start: cluster.Range.Start, End: cluster.Range.End})
		index = cluster.Range.End
	}
}

// TextCluster is one logical-cluster (grapheme) query result.
type TextCluster struct {
	// Present reports whether the queried side has a cluster.
	Present bool
	// Range is the cluster's UTF-8 byte range.
	Range TextRange
}

// Relayout re-shapes the stored text and runs under a new wrap/clamp
// constraint (the shaping handle's layout update) and refreshes the
// summary. Wrap widths must be finite and >= 0; clamps >= 1. The
// logical clusters are pure text facts and survive the re-layout.
func (l *WrappedLine) Relayout(wrapWidth *float32, lineClamp *uint32) error {
	if err := l.checkLive("Relayout"); err != nil {
		return err
	}
	if wrapWidth != nil && (!finiteFloat32(*wrapWidth) || *wrapWidth < 0) {
		return fmt.Errorf("gpui: Relayout: %w: wrap width must be finite and >= 0", ErrTextValue)
	}
	if lineClamp != nil && *lineClamp < 1 {
		return fmt.Errorf("gpui: Relayout: %w: line clamp must be >= 1", ErrTextValue)
	}
	if err := l.system.svc.Relayout(l.handle, wrapWidth, lineClamp); err != nil {
		return wrapTextError("Relayout", err)
	}
	return l.refreshSummary()
}

// Dispose releases the native shaping handle; the line is stale
// afterwards (every query fails with ErrTextStaleHandle). Dispose is
// idempotent.
func (l *WrappedLine) Dispose() error {
	if l == nil {
		return nil
	}
	if l.disposed.Swap(true) {
		return nil
	}
	if err := l.system.svc.Dispose(l.handle); err != nil {
		return wrapTextError("Dispose", err)
	}
	return nil
}
