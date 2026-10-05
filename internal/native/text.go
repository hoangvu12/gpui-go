// Text geometry service access (ticket09).
//
// This file is the Go mirror of the native text service documented in
// reference/native/TEXT_ABI.md and implemented in
// reference/native/src/text.rs: the pinned Parley/Fontique text stack
// (Parley 0.11.1, Fontique 0.11.1, HarfRust 0.12.0, Skrifa 0.44.0, Swash
// 0.2.10) constructed exactly like the pinned Windows platform
// (SystemFonts::Load + "Segoe UI" + Lilex/IBM Plex Sans/Arial service
// fallbacks) behind #[repr(C)] records and an extern "system" function
// table. The service table lives in reserved slot 4 of the bootstrap ABI
// table and is advertised with capability bit 4 ("text-parley-0-11-1").
//
// The service exposes CPU geometry only: system font enumeration, font
// resolution, shaping (visual lines, paint fragments with resolved font
// ids, positioned glyphs), carets, selection rectangles, hit tests and
// grapheme clusters. Glyph rasterization is a later ticket. All byte
// offsets are UTF-8 byte indices; UTF-16 concerns (IME ranges, surrogate
// pairs) belong to the higher-level Go adapter and never cross this ABI.
//
// Every f32 crosses the ABI as IEEE-754 bits; the decoded value types
// resolve them. Records are plain fixed-width Go structs whose layout is
// asserted against the native self-check fields at service-fetch time
// (validateTextTable). No Go pointer is retained across calls; the
// native side copies the text, run records, feature records and packed
// strings into service-owned state before shaping, and output records
// plus dump buffers are borrowed for one call only.

package native

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Status codes and errors
// ---------------------------------------------------------------------------

// Native text service status codes (mirror text_status in
// reference/native/src/text.rs). 0 is success; negative values are
// caller/argument errors; 100+ are internal failures.
const (
	textStatusOK          int32 = 0
	textErrStaleHandle    int32 = -1
	textErrBadHandle      int32 = -2
	textErrNullArg        int32 = -3
	textErrBadValue       int32 = -4
	textErrFontUnresolved int32 = -5
	textErrShapingLimit   int32 = -6
	textErrTextCapacity   int32 = -7
	textErrTextPanic      int32 = 101
)

// textServiceVersion mirrors GPUI_GO_TEXT_SERVICE_VERSION.
const textServiceVersion uint32 = 1

func textStatusName(code int32) string {
	switch code {
	case textStatusOK:
		return "ok"
	case textErrStaleHandle:
		return "stale handle"
	case textErrBadHandle:
		return "bad handle"
	case textErrNullArg:
		return "null argument"
	case textErrBadValue:
		return "bad value (coverage, boundary, tag, bound or non-finite float)"
	case textErrFontUnresolved:
		return "font unresolved (the whole pinned fallback chain failed)"
	case textErrShapingLimit:
		return "live shaping-handle limit exceeded"
	case textErrTextCapacity:
		return "dump capacity too small"
	case textErrTextPanic:
		return "native panic contained"
	default:
		return "unknown text status"
	}
}

// Text service sentinel errors, distinguishable with errors.Is.
var (
	// ErrTextStaleHandle: the shaping handle encoded a past generation
	// (disposed or slot reused).
	ErrTextStaleHandle = errors.New("native: text shaping handle is stale")
	// ErrTextBadHandle: the handle never existed or is malformed, or the
	// font id is not canonical/registered.
	ErrTextBadHandle = errors.New("native: text handle is invalid")
	// ErrTextNullArg: a required pointer argument was null.
	ErrTextNullArg = errors.New("native: text argument was null")
	// ErrTextBadValue: a request failed validation (run coverage, byte
	// boundary, feature tag/value, bound, non-finite float, enumerant).
	ErrTextBadValue = errors.New("native: text value rejected")
	// ErrTextFontUnresolved: no family in the pinned fallback chain
	// resolved.
	ErrTextFontUnresolved = errors.New("native: font unresolved")
	// ErrTextShapingLimit: the live shaping-handle capacity (256) is
	// exhausted.
	ErrTextShapingLimit = errors.New("native: text shaping-handle limit exceeded")
	// ErrTextTextCapacity: a dump capacity was too small; retry with the
	// reported need.
	ErrTextTextCapacity = errors.New("native: text dump capacity too small")
	// ErrTextPanic: a native panic was contained by catch_unwind.
	ErrTextPanic = errors.New("native: text panic contained")
)

// TextStatusError reports a non-zero text service status code.
type TextStatusError struct {
	Code   int32
	Name   string
	Detail string
}

func (e *TextStatusError) Error() string {
	detail := e.Detail
	if detail != "" {
		detail = ": " + detail
	}
	return ErrBadStatus.Error() + ": text status " + fmt.Sprint(e.Code) + " (" + e.Name + ")" + detail
}

func (e *TextStatusError) Unwrap() error {
	switch e.Code {
	case textErrStaleHandle:
		return ErrTextStaleHandle
	case textErrBadHandle:
		return ErrTextBadHandle
	case textErrNullArg:
		return ErrTextNullArg
	case textErrBadValue:
		return ErrTextBadValue
	case textErrFontUnresolved:
		return ErrTextFontUnresolved
	case textErrShapingLimit:
		return ErrTextShapingLimit
	case textErrTextCapacity:
		return ErrTextTextCapacity
	case textErrTextPanic:
		return ErrTextPanic
	default:
		return ErrBadStatus
	}
}

func textStatusError(code int32, detail string) *TextStatusError {
	return &TextStatusError{Code: code, Name: textStatusName(code), Detail: detail}
}

// ---------------------------------------------------------------------------
// Enumerants and record mirrors
// ---------------------------------------------------------------------------

// TextFontStyle selects a face style (native 0/1/2 enumerant).
type TextFontStyle uint32

const (
	// TextFontStyleNormal is neither italic nor obliqued.
	TextFontStyleNormal TextFontStyle = 0
	// TextFontStyleItalic is a cursive form.
	TextFontStyleItalic TextFontStyle = 1
	// TextFontStyleOblique is a sloped form.
	TextFontStyleOblique TextFontStyle = 2
)

// CaretAffinity selects which logical neighbor owns a caret at a shared
// boundary (native 0/1 enumerant).
type CaretAffinity uint32

const (
	// CaretDownstream attaches to the following cluster.
	CaretDownstream CaretAffinity = 0
	// CaretUpstream attaches to the preceding cluster.
	CaretUpstream CaretAffinity = 1
)

// ClusterSide selects the logical cluster adjacent to a caret (native
// 0/1 enumerant).
type ClusterSide uint32

const (
	// ClusterBefore is the cluster preceding the caret.
	ClusterBefore ClusterSide = 0
	// ClusterAfter is the cluster following the caret.
	ClusterAfter ClusterSide = 1
)

// textFontRequest mirrors GpuiGoTextFontRequest (28 bytes, alignment 4):
// weight_bits @0, style @4, feature_count @8, fallback_count @12,
// reserved[3] @16.
type textFontRequest struct {
	WeightBits    uint32
	Style         uint32
	FeatureCount  uint32
	FallbackCount uint32
	Reserved      [3]uint32
}

// FontRecord mirrors GpuiGoTextFontRecord (40 bytes, alignment 8):
// font_id @0 (u64), generation @8 (u64), weight_bits @12, style @16,
// feature_count @20, fallback_count @24, reserved @28, record_size @36.
type FontRecord struct {
	FontID        uint64
	Generation    uint64
	WeightBits    uint32
	Style         uint32
	FeatureCount  uint32
	FallbackCount uint32
	Reserved      uint32
	RecordSize    uint32
}

// FontMetricsRecord mirrors GpuiGoTextFontMetricsRecord (52 bytes,
// alignment 4): font units, exactly the pinned gpui::FontMetrics fields.
type FontMetricsRecord struct {
	UnitsPerEm             uint32
	AscentBits             uint32
	DescentBits            uint32
	LineGapBits            uint32
	UnderlinePositionBits  uint32
	UnderlineThicknessBits uint32
	CapHeightBits          uint32
	XHeightBits            uint32
	BboxXBits              uint32
	BboxYBits              uint32
	BboxWBits              uint32
	BboxHBits              uint32
	RecordSize             uint32
}

// TextFeatureRecord mirrors GpuiGoTextFeature (8 bytes, alignment 4):
// tag @0 (4 ASCII characters, big-endian), value @4.
type TextFeatureRecord struct {
	Tag   uint32
	Value uint32
}

// TextRunRecord mirrors GpuiGoTextRunRecord (48 bytes, alignment 4).
type TextRunRecord struct {
	Len                  uint32
	FamilyOffset         uint32
	FamilyLen            uint32
	FallbacksOffset      uint32
	FallbacksLen         uint32
	WeightBits           uint32
	Style                uint32
	FeatureOffset        uint32
	FeatureCount         uint32
	LetterSpacingPresent uint32
	LetterSpacingBits    uint32
	Reserved             uint32
}

// TextLayoutRecord mirrors GpuiGoTextLayoutRecord (56 bytes, alignment 4).
type TextLayoutRecord struct {
	TextLen        uint32
	FontSizeBits   uint32
	WrapPresent    uint32
	WrapBits       uint32
	ClampPresent   uint32
	LineClamp      uint32
	LineCount      uint32
	FragmentCount  uint32
	GlyphCount     uint32
	WidthBits      uint32
	AscentBits     uint32
	DescentBits    uint32
	FontGeneration uint32
	RecordSize     uint32
}

// TextLineRecord mirrors GpuiGoTextLineRecord (36 bytes, alignment 4).
type TextLineRecord struct {
	LineIndex        uint32
	TextStart        uint32
	TextEnd          uint32
	FragmentStart    uint32
	FragmentEnd      uint32
	AdvanceWidthBits uint32
	LineHeightBits   uint32
	BaselineBits     uint32
	RecordSize       uint32
}

// TextCaretRecord mirrors GpuiGoTextCaretRecord (56 bytes, alignment 4).
type TextCaretRecord struct {
	Index                uint32
	Affinity             uint32
	Present              uint32
	XBits                uint32
	YBits                uint32
	WBits                uint32
	HBits                uint32
	ClusterBeforePresent uint32
	ClusterBeforeStart   uint32
	ClusterBeforeEnd     uint32
	ClusterAfterPresent  uint32
	ClusterAfterStart    uint32
	ClusterAfterEnd      uint32
	RecordSize           uint32
}

// TextHitRecord mirrors GpuiGoTextHitRecord (16 bytes, alignment 4).
type TextHitRecord struct {
	Index      uint32
	Affinity   uint32
	Inside     uint32
	RecordSize uint32
}

// TextSelectionRecord mirrors GpuiGoTextSelectionRecord (20 bytes,
// alignment 4): a bulk-dump record.
type TextSelectionRecord struct {
	XBits      uint32
	YBits      uint32
	WBits      uint32
	HBits      uint32
	RecordSize uint32
}

// TextClusterRecord mirrors GpuiGoTextClusterRecord (16 bytes,
// alignment 4).
type TextClusterRecord struct {
	Present    uint32
	Start      uint32
	End        uint32
	RecordSize uint32
}

// TextFragmentRecord mirrors GpuiGoTextFragmentRecord (32 bytes,
// alignment 8): a bulk-dump record.
type TextFragmentRecord struct {
	FontID       uint64
	FontSizeBits uint32
	XStartBits   uint32
	XEndBits     uint32
	GlyphStart   uint32
	GlyphCount   uint32
	RecordSize   uint32
}

// TextGlyphRecord mirrors GpuiGoTextGlyphRecord (20 bytes, alignment 4):
// a bulk-dump record.
type TextGlyphRecord struct {
	GlyphID    uint32
	XBits      uint32
	YBits      uint32
	IsEmoji    uint32
	RecordSize uint32
}

// ---------------------------------------------------------------------------
// Decoded value types
// ---------------------------------------------------------------------------

// FontID is the pinned canonical font identity handle (bit 63 set, the
// FontStore index below). Fonts are process-stable; there is no dispose.
type FontID uint64

// ShapingID is a validated opaque native shaping handle. It becomes
// stale after Dispose (or native slot reuse).
type ShapingID uint64

// FontIdentity is the resolved font identity (see TEXT_ABI.md for the
// CLARIFIED identity-record scope).
type FontIdentity struct {
	FontID        FontID
	Generation    uint64
	Weight        float32
	Style         TextFontStyle
	FeatureCount  int
	FallbackCount int
}

// FontMetrics is the pinned FontMetrics in font units.
type FontMetrics struct {
	UnitsPerEm         uint32
	Ascent             float32
	Descent            float32
	LineGap            float32
	UnderlinePosition  float32
	UnderlineThickness float32
	CapHeight          float32
	XHeight            float32
	BoundingBoxX       float32
	BoundingBoxY       float32
	BoundingBoxWidth   float32
	BoundingBoxHeight  float32
}

// FontFeature is one OpenType feature setting: a four-character ASCII
// tag and its value (0 off, 1 on, at most 65535).
type FontFeature struct {
	Tag   string
	Value uint32
}

// FontDescriptor selects a font for resolution: family, weight
// (100..900), style, features and fallback family names (tried in order
// before the service's pinned fallbacks Lilex/IBM Plex Sans/Arial).
type FontDescriptor struct {
	Family    string
	Weight    float32
	Style     TextFontStyle
	Features  []FontFeature
	Fallbacks []string
}

// TextRunSpec is one style run of a shape request. The runs must exactly
// cover the text: consecutive Len values (in UTF-8 bytes) summing to the
// full text length, each boundary on a UTF-8 character boundary.
type TextRunSpec struct {
	// Len is this run's length in UTF-8 bytes.
	Len int
	// Family is the font family name (or ".SystemUIFont").
	Family string
	// Weight is the font weight (100..900, default 400).
	Weight float32
	// Style selects normal/italic/oblique.
	Style TextFontStyle
	// Features are the run's OpenType feature settings.
	Features []FontFeature
	// Fallbacks are additional fallback family names for this run.
	Fallbacks []string
	// LetterSpacing, when non-nil, applies between glyphs (pixels).
	LetterSpacing *float32
}

// ShapeRequest is one shaping document.
type ShapeRequest struct {
	// Text is the UTF-8 source (at most 1 MiB).
	Text string
	// FontSize is the shared font size in pixels.
	FontSize float32
	// Runs style the text and must exactly cover it (at most 1024).
	Runs []TextRunSpec
	// WrapWidth, when non-nil, enables soft wrapping at that width.
	WrapWidth *float32
	// LineClamp, when non-nil, caps the visual row count (>= 1).
	LineClamp *uint32
}

// TextLayout is the decoded shaping summary.
type TextLayout struct {
	TextLen        int
	FontSize       float32
	WrapWidth      *float32
	LineClamp      *uint32
	LineCount      int
	FragmentCount  int
	GlyphCount     int
	Width          float32
	Ascent         float32
	Descent        float32
	FontGeneration uint32
}

// TextLine is one visual row. The row occupies
// [Index*LineHeight, (Index+1)*LineHeight) vertically and
// [0, AdvanceWidth) horizontally before paint-time alignment; Baseline
// is the pinned (line_height-ascent-descent)/2+ascent derivation.
type TextLine struct {
	Index         int
	TextStart     int
	TextEnd       int
	FragmentStart int
	FragmentEnd   int
	AdvanceWidth  float32
	LineHeight    float32
	Baseline      float32
}

// TextRect is a geometry rectangle in layout pixels.
type TextRect struct {
	X, Y, W, H float32
}

// TextRange is a UTF-8 byte range.
type TextRange struct {
	Start, End int
}

// TextCaret is the caret geometry for a byte position.
type TextCaret struct {
	Index    int
	Affinity CaretAffinity
	// Present is false when the pinned caret_bounds returned None.
	Present bool
	// Bounds is the caret rectangle (zero when not present).
	Bounds TextRect
	// ClusterBefore/ClusterAfter are the adjacent grapheme clusters.
	ClusterBefore *TextRange
	ClusterAfter  *TextRange
}

// TextHit is a hit-test result.
type TextHit struct {
	Index    int
	Affinity CaretAffinity
	// Inside is true when the point fell inside a visual row (the pinned
	// Ok); false means the record carries the edge caret (the pinned
	// Err).
	Inside bool
}

// TextCluster is one logical cluster (grapheme) query result.
type TextCluster struct {
	Present bool
	Range   TextRange
}

// TextFragment is one shaped run: the resolved font, its size, the
// fragment's x extent and its glyph dump slice.
type TextFragment struct {
	FontID     FontID
	FontSize   float32
	XStart     float32
	XEnd       float32
	GlyphStart int
	GlyphCount int
}

// TextGlyph is one positioned glyph: line-local x, baseline-relative y.
type TextGlyph struct {
	ID      uint32
	X       float32
	Y       float32
	IsEmoji bool
}

// ---------------------------------------------------------------------------
// Service table mirror and validation
// ---------------------------------------------------------------------------

// textTable mirrors the native GpuiGoTextTable: 15 function pointers
// then 20 self-check/capacity scalars; 200 bytes, alignment 8.
type textTable struct {
	fontNames       uintptr
	fontResolve     uintptr
	fontMetrics     uintptr
	shape           uintptr
	layout          uintptr
	dispose         uintptr
	layoutInfo      uintptr
	lineCount       uintptr
	line            uintptr
	caret           uintptr
	hitTest         uintptr
	selectionRects  uintptr
	cluster         uintptr
	fragments       uintptr
	glyphs          uintptr
	serviceVersion  uint32
	sizeOfTable     uint32
	alignOfTable    uint32
	sizeOfFontReq   uint32
	sizeOfFontRec   uint32
	alignOfFontRec  uint32
	sizeOfMetrics   uint32
	sizeOfRunRec    uint32
	sizeOfFeature   uint32
	sizeOfLayoutRec uint32
	sizeOfLineRec   uint32
	sizeOfCaretRec  uint32
	sizeOfHitRec    uint32
	sizeOfSelectRec uint32
	sizeOfClusterRc uint32
	sizeOfFragment  uint32
	alignOfFragment uint32
	sizeOfGlyphRec  uint32
	maxShaping      uint32
	maxTextBytes    uint32
}

// textTableFromSlot reinterprets a reserved-slot word as a table pointer
// (the pointee is a Rust static copied out immediately).
func textTableFromSlot(slot uintptr) *textTable {
	return (*textTable)(*(*unsafe.Pointer)(unsafe.Pointer(&slot)))
}

// validateTextTable checks the text service table copy against the Go
// mirrors before any function slot is trusted.
func validateTextTable(t *textTable, path string) error {
	abi := func(field, expected, actual string) error {
		return &ABIError{Path: path, Field: field, Expected: expected, Actual: actual}
	}
	if t.serviceVersion != textServiceVersion {
		return abi("text service_version", fmt.Sprint(textServiceVersion), fmt.Sprint(t.serviceVersion))
	}
	if t.sizeOfTable != uint32(unsafe.Sizeof(textTable{})) {
		return abi("text size_of_table", fmt.Sprint(unsafe.Sizeof(textTable{})), fmt.Sprint(t.sizeOfTable))
	}
	if t.alignOfTable != uint32(unsafe.Alignof(textTable{})) {
		return abi("text align_of_table", fmt.Sprint(unsafe.Alignof(textTable{})), fmt.Sprint(t.alignOfTable))
	}
	if t.sizeOfFontReq != uint32(unsafe.Sizeof(textFontRequest{})) {
		return abi("text size_of_font_request", fmt.Sprint(unsafe.Sizeof(textFontRequest{})), fmt.Sprint(t.sizeOfFontReq))
	}
	if t.sizeOfFontRec != uint32(unsafe.Sizeof(FontRecord{})) {
		return abi("text size_of_font_record", fmt.Sprint(unsafe.Sizeof(FontRecord{})), fmt.Sprint(t.sizeOfFontRec))
	}
	if t.alignOfFontRec != uint32(unsafe.Alignof(FontRecord{})) {
		return abi("text align_of_font_record", fmt.Sprint(unsafe.Alignof(FontRecord{})), fmt.Sprint(t.alignOfFontRec))
	}
	if t.sizeOfMetrics != uint32(unsafe.Sizeof(FontMetricsRecord{})) {
		return abi("text size_of_metrics_record", fmt.Sprint(unsafe.Sizeof(FontMetricsRecord{})), fmt.Sprint(t.sizeOfMetrics))
	}
	if t.sizeOfRunRec != uint32(unsafe.Sizeof(TextRunRecord{})) {
		return abi("text size_of_run_record", fmt.Sprint(unsafe.Sizeof(TextRunRecord{})), fmt.Sprint(t.sizeOfRunRec))
	}
	if t.sizeOfFeature != uint32(unsafe.Sizeof(TextFeatureRecord{})) {
		return abi("text size_of_feature_record", fmt.Sprint(unsafe.Sizeof(TextFeatureRecord{})), fmt.Sprint(t.sizeOfFeature))
	}
	if t.sizeOfLayoutRec != uint32(unsafe.Sizeof(TextLayoutRecord{})) {
		return abi("text size_of_layout_record", fmt.Sprint(unsafe.Sizeof(TextLayoutRecord{})), fmt.Sprint(t.sizeOfLayoutRec))
	}
	if t.sizeOfLineRec != uint32(unsafe.Sizeof(TextLineRecord{})) {
		return abi("text size_of_line_record", fmt.Sprint(unsafe.Sizeof(TextLineRecord{})), fmt.Sprint(t.sizeOfLineRec))
	}
	if t.sizeOfCaretRec != uint32(unsafe.Sizeof(TextCaretRecord{})) {
		return abi("text size_of_caret_record", fmt.Sprint(unsafe.Sizeof(TextCaretRecord{})), fmt.Sprint(t.sizeOfCaretRec))
	}
	if t.sizeOfHitRec != uint32(unsafe.Sizeof(TextHitRecord{})) {
		return abi("text size_of_hit_record", fmt.Sprint(unsafe.Sizeof(TextHitRecord{})), fmt.Sprint(t.sizeOfHitRec))
	}
	if t.sizeOfSelectRec != uint32(unsafe.Sizeof(TextSelectionRecord{})) {
		return abi("text size_of_selection_record", fmt.Sprint(unsafe.Sizeof(TextSelectionRecord{})), fmt.Sprint(t.sizeOfSelectRec))
	}
	if t.sizeOfClusterRc != uint32(unsafe.Sizeof(TextClusterRecord{})) {
		return abi("text size_of_cluster_record", fmt.Sprint(unsafe.Sizeof(TextClusterRecord{})), fmt.Sprint(t.sizeOfClusterRc))
	}
	if t.sizeOfFragment != uint32(unsafe.Sizeof(TextFragmentRecord{})) {
		return abi("text size_of_fragment_record", fmt.Sprint(unsafe.Sizeof(TextFragmentRecord{})), fmt.Sprint(t.sizeOfFragment))
	}
	if t.alignOfFragment != uint32(unsafe.Alignof(TextFragmentRecord{})) {
		return abi("text align_of_fragment_record", fmt.Sprint(unsafe.Alignof(TextFragmentRecord{})), fmt.Sprint(t.alignOfFragment))
	}
	if t.sizeOfGlyphRec != uint32(unsafe.Sizeof(TextGlyphRecord{})) {
		return abi("text size_of_glyph_record", fmt.Sprint(unsafe.Sizeof(TextGlyphRecord{})), fmt.Sprint(t.sizeOfGlyphRec))
	}
	if t.maxShaping == 0 || t.maxTextBytes == 0 {
		return &CapabilityError{Path: path, Detail: "text capacity bounds are zero"}
	}
	slots := [...]uintptr{
		t.fontNames, t.fontResolve, t.fontMetrics, t.shape, t.layout,
		t.dispose, t.layoutInfo, t.lineCount, t.line, t.caret, t.hitTest,
		t.selectionRects, t.cluster, t.fragments, t.glyphs,
	}
	names := [...]string{
		"text_font_names", "text_font_resolve", "text_font_metrics",
		"text_shape", "text_layout", "text_dispose", "text_layout_info",
		"text_line_count", "text_line", "text_caret", "text_hit_test",
		"text_selection_rects", "text_cluster", "text_fragments",
		"text_glyphs",
	}
	for i, slot := range slots {
		if slot == 0 {
			return &CapabilityError{Path: path, Detail: "text table slot " + names[i] + " is null"}
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Service, fonts and shaping
// ---------------------------------------------------------------------------

// TextService is the typed accessor for the native text service of a
// loaded Library. It holds a Go-owned copy of the validated service
// table. It is safe for concurrent use; the native service serializes
// all work behind its own state mutex.
type TextService struct {
	lib   *Library
	table textTable
}

// Text returns the text geometry service of the loaded artifact. The
// capability bit ("text-parley-0-11-1") must be present and the service
// table in reserved slot 4 must validate before any call is made. An
// artifact without the text capability fails with ErrMissingCapability
// here, not at Load.
func (l *Library) Text() (*TextService, error) {
	if l.released.Load() {
		return nil, ErrClosed
	}
	if l.identity.Capabilities&capTextParley == 0 {
		return nil, &CapabilityError{
			Path:     l.identity.Path,
			Required: capTextParley,
			Actual:   l.identity.Capabilities,
			Detail:   "text-parley-0-11-1 capability bit missing",
		}
	}
	slot := l.table.reserved[4]
	if slot == 0 {
		return nil, &CapabilityError{Path: l.identity.Path, Detail: "text service table (reserved slot 4) is null"}
	}
	table := *textTableFromSlot(slot) // copy out of DLL memory
	if err := validateTextTable(&table, l.identity.Path); err != nil {
		return nil, err
	}
	return &TextService{lib: l, table: table}, nil
}

// MaxShapingHandles is the native live-handle bound.
func (s *TextService) MaxShapingHandles() int { return int(s.table.maxShaping) }

// MaxTextBytes is the native text length bound.
func (s *TextService) MaxTextBytes() int { return int(s.table.maxTextBytes) }

// featureTagBits packs a four-character ASCII feature tag into the ABI's
// big-endian u32.
func featureTagBits(tag string) (uint32, error) {
	if len(tag) != 4 {
		return 0, fmt.Errorf("%w: feature tag %q must be exactly 4 ASCII characters", ErrTextBadValue, tag)
	}
	for i := 0; i < 4; i++ {
		if tag[i] < 0x20 || tag[i] > 0x7E {
			return 0, fmt.Errorf("%w: feature tag %q must be printable ASCII", ErrTextBadValue, tag)
		}
	}
	return binary.BigEndian.Uint32([]byte(tag)), nil
}

// packFallbacks joins fallback family names NUL-separated.
func packFallbacks(fallbacks []string) []byte {
	if len(fallbacks) == 0 {
		return nil
	}
	total := 0
	for _, name := range fallbacks {
		total += len(name) + 1
	}
	buf := make([]byte, 0, total)
	for _, name := range fallbacks {
		buf = append(buf, name...)
		buf = append(buf, 0)
	}
	return buf
}

// FontNames returns the system font family names (the pinned
// all_font_names: catalog families plus ".SystemUIFont", sorted,
// deduplicated). The strings are fetched through the native capacity
// protocol: the empty-buffer probe reports the packed byte count, the
// retry fills. The shared text stack (Fontique/DirectWrite system font
// enumeration) is created on first use.
func (s *TextService) FontNames() ([]string, error) {
	if s.lib.released.Load() {
		return nil, ErrClosed
	}
	var count, needed uint32
	code, err := callTextFontNames(s.table.fontNames, nil, 0, &count, &needed)
	if err != nil {
		return nil, err
	}
	var buf []byte
	if code == textErrTextCapacity {
		buf = make([]byte, needed)
		code, err = callTextFontNames(s.table.fontNames, buf, needed, &count, &needed)
		if err != nil {
			return nil, err
		}
	}
	if code != textStatusOK {
		return nil, textStatusError(code, "text_font_names")
	}
	if needed == 0 {
		return nil, nil
	}
	names := splitNulPacked(buf[:needed])
	if len(names) != int(count) {
		return nil, &ABIError{
			Field:    "font name count",
			Expected: fmt.Sprint(count),
			Actual:   fmt.Sprint(len(names)),
		}
	}
	return names, nil
}

// splitNulPacked splits a packed NUL-terminated string dump.
func splitNulPacked(packed []byte) []string {
	if len(packed) == 0 {
		return nil
	}
	end := len(packed)
	if packed[end-1] == 0 {
		end--
	}
	if end == 0 {
		return nil
	}
	// Split on NUL within packed[:end].
	segments := []string{}
	start := 0
	for i := 0; i <= end; i++ {
		if i == end || packed[i] == 0 {
			if i > start {
				segments = append(segments, string(packed[start:i]))
			}
			start = i + 1
		}
	}
	return segments
}

// ResolveFont resolves a font descriptor through the pinned
// resolve_canonical_font path (family, descriptor fallbacks, then the
// service fallbacks Lilex/IBM Plex Sans/Arial). The returned FontID is
// process-stable; fonts have no dispose.
func (s *TextService) ResolveFont(descriptor FontDescriptor) (FontIdentity, error) {
	if s.lib.released.Load() {
		return FontIdentity{}, ErrClosed
	}
	if descriptor.Family == "" {
		return FontIdentity{}, fmt.Errorf("%w: empty family", ErrTextBadValue)
	}
	features, err := packFeatures(descriptor.Features)
	if err != nil {
		return FontIdentity{}, err
	}
	request := textFontRequest{
		WeightBits:    math.Float32bits(descriptor.Weight),
		Style:         uint32(descriptor.Style),
		FeatureCount:  uint32(len(features)),
		FallbackCount: uint32(len(descriptor.Fallbacks)),
	}
	fallbacks := packFallbacks(descriptor.Fallbacks)
	var record FontRecord
	code, err := callTextFontResolve(
		s.table.fontResolve,
		&request,
		[]byte(descriptor.Family),
		features,
		fallbacks,
		&record,
	)
	if err != nil {
		return FontIdentity{}, err
	}
	if code != textStatusOK {
		return FontIdentity{}, textStatusError(code, "text_font_resolve")
	}
	if record.RecordSize != uint32(unsafe.Sizeof(FontRecord{})) {
		return FontIdentity{}, &ABIError{
			Field:    "font record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(FontRecord{})),
			Actual:   fmt.Sprint(record.RecordSize),
		}
	}
	return FontIdentity{
		FontID:        FontID(record.FontID),
		Generation:    record.Generation,
		Weight:        math.Float32frombits(record.WeightBits),
		Style:         TextFontStyle(record.Style),
		FeatureCount:  int(record.FeatureCount),
		FallbackCount: int(record.FallbackCount),
	}, nil
}

// packFeatures converts feature specs to ABI records.
func packFeatures(features []FontFeature) ([]TextFeatureRecord, error) {
	if len(features) == 0 {
		return nil, nil
	}
	records := make([]TextFeatureRecord, len(features))
	for i, feature := range features {
		tag, err := featureTagBits(feature.Tag)
		if err != nil {
			return nil, err
		}
		if feature.Value > 65535 {
			return nil, fmt.Errorf("%w: feature %q value %d exceeds u16", ErrTextBadValue, feature.Tag, feature.Value)
		}
		records[i] = TextFeatureRecord{Tag: tag, Value: feature.Value}
	}
	return records, nil
}

// FontMetrics returns the pinned FontMetrics (font units) for a font the
// service knows: resolved ids and every font Parley selected during
// shaping (fragment font ids).
func (s *TextService) FontMetrics(fontID FontID) (FontMetrics, error) {
	if s.lib.released.Load() {
		return FontMetrics{}, ErrClosed
	}
	var record FontMetricsRecord
	code, err := callTextFontMetrics(s.table.fontMetrics, uint64(fontID), &record)
	if err != nil {
		return FontMetrics{}, err
	}
	if code != textStatusOK {
		return FontMetrics{}, textStatusError(code, "text_font_metrics")
	}
	if record.RecordSize != uint32(unsafe.Sizeof(FontMetricsRecord{})) {
		return FontMetrics{}, &ABIError{
			Field:    "font metrics record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(FontMetricsRecord{})),
			Actual:   fmt.Sprint(record.RecordSize),
		}
	}
	return FontMetrics{
		UnitsPerEm:         record.UnitsPerEm,
		Ascent:             math.Float32frombits(record.AscentBits),
		Descent:            math.Float32frombits(record.DescentBits),
		LineGap:            math.Float32frombits(record.LineGapBits),
		UnderlinePosition:  math.Float32frombits(record.UnderlinePositionBits),
		UnderlineThickness: math.Float32frombits(record.UnderlineThicknessBits),
		CapHeight:          math.Float32frombits(record.CapHeightBits),
		XHeight:            math.Float32frombits(record.XHeightBits),
		BoundingBoxX:       math.Float32frombits(record.BboxXBits),
		BoundingBoxY:       math.Float32frombits(record.BboxYBits),
		BoundingBoxWidth:   math.Float32frombits(record.BboxWBits),
		BoundingBoxHeight:  math.Float32frombits(record.BboxHBits),
	}, nil
}

// Shape validates and shapes the request with the pinned
// PlatformTextSystem::layout_text call and returns the live shaping
// handle. Runs must exactly cover Text on UTF-8 boundaries.
func (s *TextService) Shape(request ShapeRequest) (ShapingID, error) {
	if s.lib.released.Load() {
		return 0, ErrClosed
	}
	if len(request.Runs) == 0 {
		return 0, fmt.Errorf("%w: at least one run is required", ErrTextBadValue)
	}
	if !isFiniteFloat(float64(request.FontSize)) {
		return 0, fmt.Errorf("%w: font size must be finite", ErrTextBadValue)
	}
	if request.WrapWidth != nil && (!isFiniteFloat(float64(*request.WrapWidth)) || *request.WrapWidth < 0) {
		return 0, fmt.Errorf("%w: wrap width must be finite and >= 0", ErrTextBadValue)
	}
	if request.LineClamp != nil && *request.LineClamp < 1 {
		return 0, fmt.Errorf("%w: line clamp must be >= 1", ErrTextBadValue)
	}
	runs, features, strings, err := packShapeRequest(&request)
	if err != nil {
		return 0, err
	}
	wrapPresent, wrapBits := optionBits(request.WrapWidth)
	clampPresent, clamp := optionU32(request.LineClamp)
	var handle uint64
	code, err := callTextShape(
		s.table.shape,
		[]byte(request.Text),
		runs,
		features,
		strings,
		math.Float32bits(request.FontSize),
		wrapBits, wrapPresent,
		clamp, clampPresent,
		&handle,
	)
	if err != nil {
		return 0, err
	}
	if code != textStatusOK {
		return 0, textStatusError(code, "text_shape")
	}
	return ShapingID(handle), nil
}

// isFiniteFloat reports whether the float is neither NaN nor infinite
// (the native service's finite-float validation mirror).
func isFiniteFloat(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

// optionBits converts an optional float to (present, bits).
func optionBits(value *float32) (uint32, uint32) {
	if value == nil {
		return 0, 0
	}
	return 1, math.Float32bits(*value)
}

// optionU32 converts an optional u32 to (present, value).
func optionU32(value *uint32) (uint32, uint32) {
	if value == nil {
		return 0, 0
	}
	return 1, *value
}

// packShapeRequest packs the run table, feature array and string buffer
// the ABI expects, validating coverage and boundaries.
func packShapeRequest(request *ShapeRequest) ([]TextRunRecord, []TextFeatureRecord, []byte, error) {
	text := request.Text
	if len(text) > maxTextLen {
		return nil, nil, nil, fmt.Errorf("%w: text length %d exceeds %d", ErrTextBadValue, len(text), maxTextLen)
	}
	runs := make([]TextRunRecord, len(request.Runs))
	var features []TextFeatureRecord
	strings := make([]byte, 0, 64)
	covered := 0
	for i, spec := range request.Runs {
		if spec.Len < 0 {
			return nil, nil, nil, fmt.Errorf("%w: run %d length is negative", ErrTextBadValue, i)
		}
		covered += spec.Len
		if covered > len(text) {
			return nil, nil, nil, fmt.Errorf("%w: runs overcover the text at run %d", ErrTextBadValue, i)
		}
		if !isCharBoundary(text, covered) {
			return nil, nil, nil, fmt.Errorf("%w: run %d ends inside a UTF-8 sequence", ErrTextBadValue, i)
		}
		if spec.Family == "" {
			return nil, nil, nil, fmt.Errorf("%w: run %d has an empty family", ErrTextBadValue, i)
		}
		runFeatures, err := packFeatures(spec.Features)
		if err != nil {
			return nil, nil, nil, err
		}
		featureOffset := len(features)
		features = append(features, runFeatures...)

		familyOffset := len(strings)
		strings = append(strings, spec.Family...)
		fallbacksOffset := len(strings)
		strings = append(strings, packFallbacks(spec.Fallbacks)...)

		letterPresent, letterBits := uint32(0), uint32(0)
		if spec.LetterSpacing != nil {
			if !isFiniteFloat(float64(*spec.LetterSpacing)) {
				return nil, nil, nil, fmt.Errorf("%w: letter spacing must be finite", ErrTextBadValue)
			}
			letterPresent, letterBits = 1, math.Float32bits(*spec.LetterSpacing)
		}
		runs[i] = TextRunRecord{
			Len:                  uint32(spec.Len),
			FamilyOffset:         uint32(familyOffset),
			FamilyLen:            uint32(len(spec.Family)),
			FallbacksOffset:      uint32(fallbacksOffset),
			FallbacksLen:         uint32(len(strings) - fallbacksOffset),
			WeightBits:           math.Float32bits(spec.Weight),
			Style:                uint32(spec.Style),
			FeatureOffset:        uint32(featureOffset),
			FeatureCount:         uint32(len(runFeatures)),
			LetterSpacingPresent: letterPresent,
			LetterSpacingBits:    letterBits,
		}
	}
	if covered != len(text) {
		return nil, nil, nil, fmt.Errorf("%w: runs cover %d bytes of a %d-byte text", ErrTextBadValue, covered, len(text))
	}
	return runs, features, strings, nil
}

// isCharBoundary reports whether offset is a UTF-8 character boundary of
// text (mirroring the native is_char_boundary check).
func isCharBoundary(text string, offset int) bool {
	if offset == 0 || offset == len(text) {
		return true
	}
	if offset < 0 || offset > len(text) {
		return false
	}
	// A boundary byte is not a continuation byte (0b10xxxxxx).
	return text[offset]&0xC0 != 0x80
}

// Relayout re-shapes the stored text and runs under a new wrap/clamp
// constraint (the shape handle's layout update).
func (s *TextService) Relayout(handle ShapingID, wrapWidth *float32, lineClamp *uint32) error {
	if s.lib.released.Load() {
		return ErrClosed
	}
	if wrapWidth != nil && (!isFiniteFloat(float64(*wrapWidth)) || *wrapWidth < 0) {
		return fmt.Errorf("%w: wrap width must be finite and >= 0", ErrTextBadValue)
	}
	if lineClamp != nil && *lineClamp < 1 {
		return fmt.Errorf("%w: line clamp must be >= 1", ErrTextBadValue)
	}
	wrapPresent, wrapBits := optionBits(wrapWidth)
	clampPresent, clamp := optionU32(lineClamp)
	code, err := callTextLayout(s.table.layout, uint64(handle), wrapBits, wrapPresent, clamp, clampPresent)
	if err != nil {
		return err
	}
	if code != textStatusOK {
		return textStatusError(code, "text_layout")
	}
	return nil
}

// Dispose releases the shaping handle; it becomes stale afterwards.
func (s *TextService) Dispose(handle ShapingID) error {
	if s.lib.released.Load() {
		return ErrClosed
	}
	code, err := callTextDispose(s.table.dispose, uint64(handle))
	if err != nil {
		return err
	}
	if code != textStatusOK {
		return textStatusError(code, "text_dispose")
	}
	return nil
}

// LayoutInfo returns the shaping summary (counts, width, ascent,
// descent, font generation, constraint echo).
func (s *TextService) LayoutInfo(handle ShapingID) (TextLayout, error) {
	if s.lib.released.Load() {
		return TextLayout{}, ErrClosed
	}
	var record TextLayoutRecord
	code, err := callTextLayoutInfo(s.table.layoutInfo, uint64(handle), &record)
	if err != nil {
		return TextLayout{}, err
	}
	if code != textStatusOK {
		return TextLayout{}, textStatusError(code, "text_layout_info")
	}
	if record.RecordSize != uint32(unsafe.Sizeof(TextLayoutRecord{})) {
		return TextLayout{}, &ABIError{
			Field:    "layout record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(TextLayoutRecord{})),
			Actual:   fmt.Sprint(record.RecordSize),
		}
	}
	layout := TextLayout{
		TextLen:        int(record.TextLen),
		FontSize:       math.Float32frombits(record.FontSizeBits),
		LineCount:      int(record.LineCount),
		FragmentCount:  int(record.FragmentCount),
		GlyphCount:     int(record.GlyphCount),
		Width:          math.Float32frombits(record.WidthBits),
		Ascent:         math.Float32frombits(record.AscentBits),
		Descent:        math.Float32frombits(record.DescentBits),
		FontGeneration: record.FontGeneration,
	}
	if record.WrapPresent != 0 {
		wrap := math.Float32frombits(record.WrapBits)
		layout.WrapWidth = &wrap
	}
	if record.ClampPresent != 0 {
		clamp := record.LineClamp
		layout.LineClamp = &clamp
	}
	return layout, nil
}

// LineCount returns the number of visual lines.
func (s *TextService) LineCount(handle ShapingID) (int, error) {
	if s.lib.released.Load() {
		return 0, ErrClosed
	}
	var count uint32
	code, err := callTextLineCount(s.table.lineCount, uint64(handle), &count)
	if err != nil {
		return 0, err
	}
	if code != textStatusOK {
		return 0, textStatusError(code, "text_line_count")
	}
	return int(count), nil
}

// Line returns one visual row's record (bounds derivation, byte range,
// fragment range, advance width, baseline).
func (s *TextService) Line(handle ShapingID, index int, lineHeight float32) (TextLine, error) {
	if s.lib.released.Load() {
		return TextLine{}, ErrClosed
	}
	if !isFiniteFloat(float64(lineHeight)) {
		return TextLine{}, fmt.Errorf("%w: line height must be finite", ErrTextBadValue)
	}
	var record TextLineRecord
	code, err := callTextLine(s.table.line, uint64(handle), uint32(index), math.Float32bits(lineHeight), &record)
	if err != nil {
		return TextLine{}, err
	}
	if code != textStatusOK {
		return TextLine{}, textStatusError(code, "text_line")
	}
	if record.RecordSize != uint32(unsafe.Sizeof(TextLineRecord{})) {
		return TextLine{}, &ABIError{
			Field:    "line record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(TextLineRecord{})),
			Actual:   fmt.Sprint(record.RecordSize),
		}
	}
	return TextLine{
		Index:         int(record.LineIndex),
		TextStart:     int(record.TextStart),
		TextEnd:       int(record.TextEnd),
		FragmentStart: int(record.FragmentStart),
		FragmentEnd:   int(record.FragmentEnd),
		AdvanceWidth:  math.Float32frombits(record.AdvanceWidthBits),
		LineHeight:    math.Float32frombits(record.LineHeightBits),
		Baseline:      math.Float32frombits(record.BaselineBits),
	}, nil
}

// Caret returns the caret geometry (bounds plus the adjacent logical
// clusters) for a UTF-8 byte index.
func (s *TextService) Caret(handle ShapingID, byteIndex int, affinity CaretAffinity, lineHeight float32) (TextCaret, error) {
	if s.lib.released.Load() {
		return TextCaret{}, ErrClosed
	}
	if !isFiniteFloat(float64(lineHeight)) {
		return TextCaret{}, fmt.Errorf("%w: line height must be finite", ErrTextBadValue)
	}
	if affinity != CaretDownstream && affinity != CaretUpstream {
		return TextCaret{}, fmt.Errorf("%w: unknown affinity %d", ErrTextBadValue, affinity)
	}
	var record TextCaretRecord
	code, err := callTextCaret(s.table.caret, uint64(handle), uint32(byteIndex), uint32(affinity), math.Float32bits(lineHeight), &record)
	if err != nil {
		return TextCaret{}, err
	}
	if code != textStatusOK {
		return TextCaret{}, textStatusError(code, "text_caret")
	}
	if record.RecordSize != uint32(unsafe.Sizeof(TextCaretRecord{})) {
		return TextCaret{}, &ABIError{
			Field:    "caret record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(TextCaretRecord{})),
			Actual:   fmt.Sprint(record.RecordSize),
		}
	}
	caret := TextCaret{
		Index:    int(record.Index),
		Affinity: CaretAffinity(record.Affinity),
		Present:  record.Present != 0,
		Bounds: TextRect{
			X: math.Float32frombits(record.XBits),
			Y: math.Float32frombits(record.YBits),
			W: math.Float32frombits(record.WBits),
			H: math.Float32frombits(record.HBits),
		},
	}
	if record.ClusterBeforePresent != 0 {
		caret.ClusterBefore = &TextRange{Start: int(record.ClusterBeforeStart), End: int(record.ClusterBeforeEnd)}
	}
	if record.ClusterAfterPresent != 0 {
		caret.ClusterAfter = &TextRange{Start: int(record.ClusterAfterStart), End: int(record.ClusterAfterEnd)}
	}
	return caret, nil
}

// HitTest returns the closest caret for a point in layout coordinates.
func (s *TextService) HitTest(handle ShapingID, x, y, lineHeight float32) (TextHit, error) {
	if s.lib.released.Load() {
		return TextHit{}, ErrClosed
	}
	if !isFiniteFloat(float64(x)) || !isFiniteFloat(float64(y)) || !isFiniteFloat(float64(lineHeight)) {
		return TextHit{}, fmt.Errorf("%w: hit-test coordinates must be finite", ErrTextBadValue)
	}
	var record TextHitRecord
	code, err := callTextHitTest(s.table.hitTest, uint64(handle), math.Float32bits(x), math.Float32bits(y), math.Float32bits(lineHeight), &record)
	if err != nil {
		return TextHit{}, err
	}
	if code != textStatusOK {
		return TextHit{}, textStatusError(code, "text_hit_test")
	}
	if record.RecordSize != uint32(unsafe.Sizeof(TextHitRecord{})) {
		return TextHit{}, &ABIError{
			Field:    "hit record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(TextHitRecord{})),
			Actual:   fmt.Sprint(record.RecordSize),
		}
	}
	return TextHit{
		Index:    int(record.Index),
		Affinity: CaretAffinity(record.Affinity),
		Inside:   record.Inside != 0,
	}, nil
}

// SelectionRects returns the visual-order rectangles covering a UTF-8
// byte range (empty ranges produce none).
func (s *TextService) SelectionRects(handle ShapingID, start, end int, lineHeight float32) ([]TextRect, error) {
	if s.lib.released.Load() {
		return nil, ErrClosed
	}
	if start > end {
		return nil, fmt.Errorf("%w: selection start %d exceeds end %d", ErrTextBadValue, start, end)
	}
	if !isFiniteFloat(float64(lineHeight)) {
		return nil, fmt.Errorf("%w: line height must be finite", ErrTextBadValue)
	}
	var needed uint32
	code, err := callTextSelectionRects(s.table.selectionRects, uint64(handle), uint32(start), uint32(end), math.Float32bits(lineHeight), nil, 0, &needed)
	if err != nil {
		return nil, err
	}
	var buf []TextSelectionRecord
	if code == textErrTextCapacity {
		buf = make([]TextSelectionRecord, needed)
		code, err = callTextSelectionRects(s.table.selectionRects, uint64(handle), uint32(start), uint32(end), math.Float32bits(lineHeight), buf, needed, &needed)
		if err != nil {
			return nil, err
		}
	}
	if code != textStatusOK {
		return nil, textStatusError(code, "text_selection_rects")
	}
	if needed == 0 {
		return nil, nil
	}
	rects := make([]TextRect, len(buf))
	for i, record := range buf {
		if record.RecordSize != uint32(unsafe.Sizeof(TextSelectionRecord{})) {
			return nil, &ABIError{
				Field:    "selection record_size",
				Expected: fmt.Sprint(unsafe.Sizeof(TextSelectionRecord{})),
				Actual:   fmt.Sprint(record.RecordSize),
			}
		}
		rects[i] = TextRect{
			X: math.Float32frombits(record.XBits),
			Y: math.Float32frombits(record.YBits),
			W: math.Float32frombits(record.WBits),
			H: math.Float32frombits(record.HBits),
		}
	}
	return rects, nil
}

// Cluster returns the logical cluster (grapheme) before or after a byte
// position.
func (s *TextService) Cluster(handle ShapingID, byteIndex int, affinity CaretAffinity, side ClusterSide) (TextCluster, error) {
	if s.lib.released.Load() {
		return TextCluster{}, ErrClosed
	}
	if affinity != CaretDownstream && affinity != CaretUpstream {
		return TextCluster{}, fmt.Errorf("%w: unknown affinity %d", ErrTextBadValue, affinity)
	}
	if side != ClusterBefore && side != ClusterAfter {
		return TextCluster{}, fmt.Errorf("%w: unknown cluster side %d", ErrTextBadValue, side)
	}
	var record TextClusterRecord
	code, err := callTextCluster(s.table.cluster, uint64(handle), uint32(byteIndex), uint32(affinity), uint32(side), &record)
	if err != nil {
		return TextCluster{}, err
	}
	if code != textStatusOK {
		return TextCluster{}, textStatusError(code, "text_cluster")
	}
	if record.RecordSize != uint32(unsafe.Sizeof(TextClusterRecord{})) {
		return TextCluster{}, &ABIError{
			Field:    "cluster record_size",
			Expected: fmt.Sprint(unsafe.Sizeof(TextClusterRecord{})),
			Actual:   fmt.Sprint(record.RecordSize),
		}
	}
	return TextCluster{
		Present: record.Present != 0,
		Range:   TextRange{Start: int(record.Start), End: int(record.End)},
	}, nil
}

// Fragments bulk-dumps the shaped runs (resolved font ids, sizes, x
// extents, glyph slice indices).
func (s *TextService) Fragments(handle ShapingID) ([]TextFragment, error) {
	if s.lib.released.Load() {
		return nil, ErrClosed
	}
	var needed uint32
	code, err := callTextFragments(s.table.fragments, uint64(handle), nil, 0, &needed)
	if err != nil {
		return nil, err
	}
	var buf []TextFragmentRecord
	if code == textErrTextCapacity {
		buf = make([]TextFragmentRecord, needed)
		code, err = callTextFragments(s.table.fragments, uint64(handle), buf, needed, &needed)
		if err != nil {
			return nil, err
		}
	}
	if code != textStatusOK {
		return nil, textStatusError(code, "text_fragments")
	}
	if needed == 0 {
		return nil, nil
	}
	fragments := make([]TextFragment, len(buf))
	for i, record := range buf {
		if record.RecordSize != uint32(unsafe.Sizeof(TextFragmentRecord{})) {
			return nil, &ABIError{
				Field:    "fragment record_size",
				Expected: fmt.Sprint(unsafe.Sizeof(TextFragmentRecord{})),
				Actual:   fmt.Sprint(record.RecordSize),
			}
		}
		fragments[i] = TextFragment{
			FontID:     FontID(record.FontID),
			FontSize:   math.Float32frombits(record.FontSizeBits),
			XStart:     math.Float32frombits(record.XStartBits),
			XEnd:       math.Float32frombits(record.XEndBits),
			GlyphStart: int(record.GlyphStart),
			GlyphCount: int(record.GlyphCount),
		}
	}
	return fragments, nil
}

// Glyphs bulk-dumps the positioned glyphs (line-local x,
// baseline-relative y).
func (s *TextService) Glyphs(handle ShapingID) ([]TextGlyph, error) {
	if s.lib.released.Load() {
		return nil, ErrClosed
	}
	var needed uint32
	code, err := callTextGlyphs(s.table.glyphs, uint64(handle), nil, 0, &needed)
	if err != nil {
		return nil, err
	}
	var buf []TextGlyphRecord
	if code == textErrTextCapacity {
		buf = make([]TextGlyphRecord, needed)
		code, err = callTextGlyphs(s.table.glyphs, uint64(handle), buf, needed, &needed)
		if err != nil {
			return nil, err
		}
	}
	if code != textStatusOK {
		return nil, textStatusError(code, "text_glyphs")
	}
	if needed == 0 {
		return nil, nil
	}
	glyphs := make([]TextGlyph, len(buf))
	for i, record := range buf {
		if record.RecordSize != uint32(unsafe.Sizeof(TextGlyphRecord{})) {
			return nil, &ABIError{
				Field:    "glyph record_size",
				Expected: fmt.Sprint(unsafe.Sizeof(TextGlyphRecord{})),
				Actual:   fmt.Sprint(record.RecordSize),
			}
		}
		glyphs[i] = TextGlyph{
			ID:      record.GlyphID,
			X:       math.Float32frombits(record.XBits),
			Y:       math.Float32frombits(record.YBits),
			IsEmoji: record.IsEmoji != 0,
		}
	}
	return glyphs, nil
}
