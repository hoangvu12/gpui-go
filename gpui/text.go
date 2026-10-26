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
// DefaultLineHeight records that exact f32 factor. Browser-parity D1:
// the PORT's default text style carries the CSS `normal` keyword
// instead (div.go DefaultTextStyle, DefiniteLengthNormal), resolved
// from the resolved font's metrics (FontMetrics.LineSpacingPx, the
// lroundf(ascent)+lroundf(descent)+lroundf(line gap) rule of
// Chromium's FontMetrics::LineSpacing); the golden ratio is the
// opt-in (PhiLineHeight).
//
// Handle lifecycle: shaping handles are native resources that do not
// follow Go garbage collection. A WrappedLine holds its native handle
// until Dispose; the live-handle capacity is bounded
// (internal/native's MaxShapingHandles, 256 in the native service) and
// exhaustion is the typed ErrTextShapingLimit. A disposed (or
// slot-reused) handle yields ErrTextStaleHandle from every query; a
// malformed handle yields ErrTextBadHandle. Native panics are contained
// by the service's catch_unwind boundary and surface as typed errors.
// Lines owned by a window's line-layout cache never exhaust the bound:
// a shape that reaches it parks the least recently used cache-owned
// line's handle and retries, and the parked line re-shapes on its next
// query (Base ticket 40; see TextSystem.shapeHandle). Lines a caller
// shapes through ShapeText are never parked: their handles are the
// caller's until Dispose.

import (
	"container/list"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// ---------------------------------------------------------------------------
// Line height default and font identity constants
// ---------------------------------------------------------------------------

// goldenRatio is std::f32::consts::GOLDEN_RATIO, the pinned reference
// text style's line-height factor (phi() in crates/gpui/src/geometry.rs;
// TextStyle::default().line_height = phi().into() in style.rs). The
// untyped constant rounds to the same nearest f32 Rust's constant holds.
const goldenRatio float32 = 1.6180339887498948482045868343656

// NormalLineHeight returns the CSS `normal` line height (browser-parity
// spec D1): the text style's default since the port adopted the browser
// rule — DefiniteLength{Kind: DefiniteLengthNormal}. Pass it to
// Styled.LineHeight (or set TextStyle.LineHeight) to opt back in after
// an explicit value; the resolution computes it from the resolved font's
// metrics (FontMetrics.LineSpacingPx).
func NormalLineHeight() DefiniteLength {
	return DefiniteLength{Kind: DefiniteLengthNormal}
}

// PhiLineHeight returns the golden-ratio relative line height — the
// pinned reference's default, and the port's default before D1 made CSS
// `normal` the default. The opt-in for apps and components that want the
// reference look: DefiniteLength{Kind: DefiniteLengthFraction, Value:
// goldenRatio} (16px -> 25.888544f32, bits 0x41CF1BBD, the same f32
// DefaultLineHeight derives).
func PhiLineHeight() DefiniteLength {
	return DefiniteLength{Kind: DefiniteLengthFraction, Value: goldenRatio}
}

// DefaultLineHeight returns the golden-ratio line height for a font
// size: phi × the size, in f32 arithmetic (px(font_size *
// GOLDEN_RATIO) — the exact pinned derivation, e.g. 16px ->
// 25.888544f32, bits 0x41CF1BBD). This is the PIN's default line
// height; the port's default is CSS `normal` since browser-parity D1
// (NormalLineHeight), so this is the opt-in derivation for the
// reference look — PhiLineHeight is the style-value form.
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
		// The CSS generic family keywords resolve to Chrome's Windows
		// defaults (resolveGenericFontFamily): the native stack resolves
		// named families only, so "sans-serif" etc. never reach it raw.
		Family:    resolveGenericFontFamily(d.Family),
		Weight:    d.Weight,
		Style:     native.TextFontStyle(d.Style),
		Features:  features,
		Fallbacks: resolveGenericFontFamilies(d.Fallbacks),
	}
}

// genericFontFamilyAliases maps the CSS generic family keywords to
// the concrete Windows families Chrome's settings resolve them to:
// sans-serif → Arial, serif → Times New Roman, monospace → Consolas
// (Blink keeps the keyword through FontDescription and resolves it in
// FontSelector::FamilyNameFromSettings, platform/fonts/font_selector.cc
// :74-83; Chrome's Windows default settings match Fontique's Windows
// generic table, fontique-0.11.1/src/backend/dwrite.rs:31-39). The
// keywords are ASCII case-insensitive like every CSS family name.
// The keywords Chrome does not pin in that table (cursive, fantasy,
// math) stay named and fall through the service fallback chain, as
// before.
var genericFontFamilyAliases = [][2]string{
	{"sans-serif", "Arial"},
	{"serif", "Times New Roman"},
	{"monospace", "Consolas"},
}

// resolveGenericFontFamily maps one family name through the generic
// aliases, case-insensitively; every other name passes through.
func resolveGenericFontFamily(family string) string {
	for _, alias := range genericFontFamilyAliases {
		if strings.EqualFold(family, alias[0]) {
			return alias[1]
		}
	}
	return family
}

// resolveGenericFontFamilies maps a fallback list through the generic
// aliases (every entry of a CSS font-family list resolves the same
// way).
func resolveGenericFontFamilies(families []string) []string {
	mapped := false
	for _, family := range families {
		if resolveGenericFontFamily(family) != family {
			mapped = true
			break
		}
	}
	if !mapped {
		return families
	}
	out := make([]string, len(families))
	for i, family := range families {
		out[i] = resolveGenericFontFamily(family)
	}
	return out
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
	// FamilyFound reports whether the descriptor's primary family is
	// present in the font catalog — a system-installed family or one
	// registered through AddFont. False means the FontId resolved
	// through the fallback chain (the descriptor fallbacks, then the
	// service fallbacks Lilex/IBM Plex Sans/Arial): the pinned resolve
	// behavior, silently falling back, is unchanged — this field makes
	// the fallback observable so a missing embedded font is diagnosable
	// (check it after resolving a family you registered).
	FamilyFound bool
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

// LineSpacingPx scales the font's line spacing to a font size — the CSS
// `normal` line height, Chromium's FontMetrics::LineSpacing derivation
// (third_party/blink/renderer/platform/fonts/simple_font_data.cc:
// PlatformInit, lines 158-165: SetLineSpacing(lroundf(ascent) +
// lroundf(descent) + lroundf(line_gap)), where ascent/descent are the
// already-rounded Windows metrics of font_metrics.cc:113-114
// AscentDescentWithHacks — SkScalarRoundToScalar of the scaled hhea
// values). Each term is the design-unit metric scaled to the size
// first, then rounded half away from zero (lroundf); the sum is a whole
// pixel value by construction (Segoe UI 16px: round(17.2656) +
// round(4.0156) + round(0) = 17 + 4 + 0 = 21, Chrome's used line-height
// for `normal`, ComputedStyle::ComputedLineHeight, computed_style.cc:
// 2425-2436).
func (m FontMetrics) LineSpacingPx(fontSize float32) float32 {
	if m.UnitsPerEm == 0 {
		return 0
	}
	ascent := roundToNearest((m.Ascent / float32(m.UnitsPerEm)) * fontSize)
	descent := roundToNearest((m.Descent / float32(m.UnitsPerEm)) * fontSize)
	lineGap := roundToNearest((m.LineGap / float32(m.UnitsPerEm)) * fontSize)
	return ascent + descent + lineGap
}

// roundToNearest rounds v to the nearest whole value, halves away from
// zero (C's lroundf, the rounding of Chromium's line-spacing sum).
func roundToNearest(v float32) float32 {
	return float32(math.Round(float64(v)))
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
	// ErrTextFontData reports that font bytes submitted to AddFont contain
	// no supported font face (the pinned add_fonts registration failure).
	ErrTextFontData = errors.New("gpui: font data rejected")
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
	case errors.Is(err, native.ErrTextFontData):
		return fmt.Errorf("gpui: %s: %w", op, ErrTextFontData)
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
	// liveHandles counts the native shaping handles issued through
	// this system and not yet disposed: the Go-side mirror of the
	// native service's live-handle count (the native ABI exposes only
	// its bound, MaxShapingHandles). Atomic: the system is
	// process-global and safe for concurrent use, so the counter must
	// be too.
	liveHandles atomic.Int64

	// evictMu guards evictable, the least-recently-used order of the
	// cache-owned lines whose native handles may be released under the
	// live-handle bound (Base ticket 40). The reference has no such
	// bound: its LineLayoutCache entries are Arc-held layouts with no
	// platform resource. The port's native service bounds live shaping
	// handles process-wide (256, frozen at rev 11), so a window — or
	// several — needing more distinct shaped lines than that across the
	// current and previous frame (or inside one frame) would fail every
	// further shape. Instead, a shape that hits the bound parks the
	// least recently used cache-owned line (its handle is released, its
	// summary kept) and retries; a parked line re-shapes from its stored
	// request the next time it is queried. Front = least recently used.
	evictMu   sync.Mutex
	evictable list.List

	// raster holds the glyph paint path's caches (glyph_cache.go,
	// gpui-perf ticket 02).
	raster rasterCaches
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

// LiveShapingHandles reports how many native shaping handles this
// system has issued and not disposed — the Go-side mirror of the
// native service's live-handle count, bounded by MaxShapingHandles
// (the native ABI exposes only the bound). A redrawing window whose
// text goes through the element pipeline reuses cached shaped lines
// (text_cache.go), so its count stays flat across frames instead of
// growing with every redraw (ticket 02's observable).
func (ts *TextSystem) LiveShapingHandles() int {
	return int(ts.liveHandles.Load())
}

// FontNames returns the system font family names: the pinned
// all_font_names (catalog families plus ".SystemUIFont", sorted,
// deduplicated). The first call creates the native text stack
// (Fontique's DirectWrite-backed system font enumeration). Families
// registered through AddFont are included from the next call on.
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
// descriptor again yields the same id. A family absent from the
// catalog (system-installed or AddFont-registered) still resolves —
// through the fallback chain, exactly like the pin — and reports it
// through FontIdentity.FamilyFound (false = a fallback font answered).
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
		FamilyFound:   identity.FamilyFound,
	}, nil
}

// FontRegistration is the outcome of one AddFont call.
type FontRegistration struct {
	// Generation is the font catalog generation after this registration
	// (the registration-batch counter: every successful AddFont call
	// bumps it, so 0 → 1 → 2 … tracks registration batches). A resolved
	// FontIdentity.Generation or a TextLayoutSummary.FontGeneration at
	// or above this value saw the registered families.
	Generation uint64
	// Families are the family names the submitted bytes landed under,
	// one entry per face in file order (deduplicated), parsed from each
	// face's name table with the catalog's own precedence: the
	// typographic family (nameID 16) when present, else the legacy
	// family (nameID 1). A font whose two names differ (Geist Medium:
	// nameID 1 "Geist Medium", nameID 16 "Geist") registers under the
	// TYPOGRAPHIC name only — this report is where that becomes
	// visible, so a caller knows which name to request (and
	// FontIdentity.FamilyFound reports a family that resolves nowhere).
	Families []string
}

// AddFont registers in-memory font bytes with the text system's font
// catalog, mirroring the pinned gpui::TextSystem::add_fonts: the bytes
// (an OpenType/TrueType face or collection, e.g. loaded from an
// embedded asset) are registered into the same Fontique-backed catalog
// that resolves, shapes and rasterizes every other font — family names
// come from each font's embedded name table, appear in FontNames and
// resolve by name through ResolveFont from the next call on.
//
// Fonts are process-stable: there is no unregister, and registered
// families need no dispose. Register early (before the first text
// layout) so every window's shaping sees them; registration after
// shaping still applies to every later shape/rasterize call.
//
// One font per call, mirroring the ticket's AddFont(bytes []byte)
// sketch; the pinned add_fonts takes a batch, so N calls are N
// registration batches (N generation bumps — observable only through
// the Generation counters, never through FontIds or resolution).
//
// A family that fails to appear (wrong name inside the bytes, a
// registration that errored) is diagnosable two ways: AddFont errors
// (ErrTextFontData when the bytes contain no supported font face,
// ErrTextValue for empty or oversize data), and ResolveFont reports
// the fallback through FontIdentity.FamilyFound (false = the family is
// absent and a fallback font answered). FontRegistration.Families
// additionally names the families the bytes DID land under, so a
// legacy-versus-typographic mismatch (the silent-fallback shape this
// closes, consumer gap 06) is visible without guessing: request a
// reported family, or check FamilyFound for the name you intended.
func (ts *TextSystem) AddFont(data []byte) (FontRegistration, error) {
	if len(data) == 0 {
		return FontRegistration{}, fmt.Errorf("gpui: AddFont: %w: empty font data", ErrTextValue)
	}
	// The family report comes from the submitted bytes (the catalog's
	// own precedence, parsed before the call): a rejected registration
	// reports NO families — the report never invents names for bytes
	// the catalog would refuse. The report is best-effort: an sfnt the
	// catalog accepts but whose name table the reader cannot decode
	// registers normally and reports no families (nil).
	families, _ := fontFamilyNames(data)
	generation, err := ts.svc.AddFont(data)
	if err != nil {
		return FontRegistration{}, wrapTextError("AddFont", err)
	}
	return FontRegistration{Generation: generation, Families: families}, nil
}

// FontMetrics returns the full face metrics (font units) for a font the
// service knows: resolved ids and every font Parley selected while
// shaping (fragment font ids are registered at shape time).
func (ts *TextSystem) FontMetrics(fontID FontID) (FontMetrics, error) {
	// The pin's TextSystem::font_metrics answers from its font_metrics
	// map after the first query of a font (glyph_cache.go).
	if metrics, ok := ts.raster.lookupFontMetrics(fontID); ok {
		return metrics, nil
	}
	record, err := ts.svc.FontMetrics(native.FontID(fontID))
	if err != nil {
		return FontMetrics{}, wrapTextError("FontMetrics", err)
	}
	metrics := FontMetrics{
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
	}
	ts.raster.storeFontMetrics(fontID, metrics)
	return metrics, nil
}

// NormalLineHeight returns the CSS `normal` line height of a descriptor
// at a font size (browser-parity spec D1): the resolved font's line
// spacing, FontMetrics.LineSpacingPx — the rounded ascent, descent and
// line gap scaled to the size, the value ComputedStyle::ComputedLineHeight
// uses for `line-height: normal` (Chromium computed_style.cc:2425-2436,
// LineSpacing from simple_font_data.cc:158-165). An unresolvable
// descriptor still answers through the pinned fallback chain, like
// ResolveFont; the error is reserved for a text-system failure.
func (ts *TextSystem) NormalLineHeight(font FontDescriptor, fontSize float32) (float32, error) {
	identity, err := ts.ResolveFont(font)
	if err != nil {
		return 0, err
	}
	metrics, err := ts.FontMetrics(identity.FontID)
	if err != nil {
		return 0, err
	}
	return metrics.LineSpacingPx(fontSize), nil
}

// ---------------------------------------------------------------------------
// Shaping
// ---------------------------------------------------------------------------

// TextRun is one style run of a shaped document, mirroring the pinned
// gpui::TextRun (crates/gpui/src/text_system.rs:489-504): a byte length,
// the run's font descriptor, the paint fields (color, background,
// underline, strikethrough) and an optional letter spacing. Runs must
// exactly cover the shaped text on UTF-8 character boundaries.
//
// Only Len, Font and LetterSpacing cross the native shaping ABI (the
// rev-11 run record carries no paint style; ticket 02's decision): the
// paint fields are applied Go-side at paint time (rich_paint.go — shape
// once, paint each run clipped to its selection rectangles), so they
// never change a shaped geometry and are not part of the line-layout
// cache key (text_cache.go encodeShapeRuns).
type TextRun struct {
	// Len is this run's length in UTF-8 bytes.
	Len int
	// Font is the run's font descriptor.
	Font FontDescriptor
	// Color is the run's glyph color (the pinned TextRun::color).
	Color Hsla
	// BackgroundColor is the run's background color, nil for none (the
	// pinned TextRun::background_color).
	BackgroundColor *Hsla
	// Underline is the run's underline, nil for none (the pinned
	// TextRun::underline).
	Underline *UnderlineStyle
	// Strikethrough is the run's strikethrough, nil for none (the pinned
	// TextRun::strikethrough).
	Strikethrough *StrikethroughStyle
	// LetterSpacing, when non-nil, is applied between the run's glyphs
	// (pixels).
	LetterSpacing *float32
	// WordBreak is CSS word-break (browser-core 43): the run's tag the
	// native side pushes as a Parley range style. Zero (normal) keeps
	// the pinned shaping.
	WordBreak WordBreak
	// OverflowWrap is CSS overflow-wrap (browser-core 43): the run's
	// emergency-breaking tag.
	OverflowWrap OverflowWrap
	// TextWrap is CSS text-wrap (browser-core 43): the paragraph's wrap
	// policy tag — balance drives the Go-side bisection, and the
	// white-space modes keep driving nowrap.
	TextWrap TextWrap
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
	// TabSizeSpaces, when non-nil, shapes tabs at the browser's
	// tab-size stops (browser-core 38): the tab advances to the next
	// multiple of tabSize × (the space advance + the run's letter
	// spacing) from the row start — Chromium's TabSize::GetPixelSize
	// (platform/text/tab_size.h:34-38) with the CSS default 8 spaces
	// (core/css/css_properties.json5: tab-size's default_value
	// "TabSize(8)"), plus Font::TabWidth's half-space minimum
	// (platform/fonts/font.cc:319-337). Nil keeps the pin's DirectWrite
	// stops (text_tabs.go, 4 × the font size — the editor and Base's
	// line wrapper).
	TabSizeSpaces *uint32
	// Align is the document's text alignment (browser-core 43):
	// Left/Center/Right keep the pin's paint-time alignment; Start/End/
	// Justify stamp the native align tag on every run (document-level by
	// the any-run rule) — justify stretches the whitespace clusters
	// inside the native layout, and start/end resolve against the
	// document's base direction (the summary's IsRTL).
	Align TextAlign
	// Balance, when true, wraps at the narrowest width that keeps the
	// unbalanced line count (browser-core 43, CSS text-wrap: balance —
	// Chromium's bisection over the available width; see balanceLine).
	// The shaping layer bisects over the shaping handle's relayouts, so
	// it only engages under a definite wrap width.
	Balance bool
}

// ShapeText shapes the request through the native text service (the
// pinned PlatformTextSystem::layout_text call) and returns the wrapped
// line handle carrying the shaping summary. A negative wrap width wraps
// at every break opportunity, like a zero one (nativeWrapWidth).
func (ts *TextSystem) ShapeText(request TextLayoutRequest) (*WrappedLine, error) {
	if len(request.Runs) == 0 {
		return nil, fmt.Errorf("gpui: ShapeText: %w: at least one run is required", ErrTextValue)
	}
	if !finiteFloat32(request.FontSize) {
		return nil, fmt.Errorf("gpui: ShapeText: %w: font size must be finite", ErrTextValue)
	}
	if request.WrapWidth != nil && !finiteFloat32(*request.WrapWidth) {
		return nil, fmt.Errorf("gpui: ShapeText: %w: wrap width must be finite", ErrTextValue)
	}
	request.WrapWidth = nativeWrapWidth(request.WrapWidth)
	if request.LineClamp != nil && *request.LineClamp < 1 {
		return nil, fmt.Errorf("gpui: ShapeText: %w: line clamp must be >= 1", ErrTextValue)
	}
	// Hard tabs shape as spaces fitted to the tab stops (text_tabs.go):
	// the pin's DirectWrite stops, or the browser's tab-size stops when
	// the request carries a tab size (browser-core 38).
	request, tabs := prepareTabs(request)
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
			Len: run.Len,
			// The CSS generic family keywords resolve to Chrome's Windows
			// defaults (resolveGenericFontFamily's table).
			Family:        resolveGenericFontFamily(run.Font.Family),
			Weight:        run.Font.Weight,
			Style:         native.TextFontStyle(run.Font.Style),
			Features:      featuresToNative(letterSpacingLigatureFeatures(run.Font.Features, run.LetterSpacing)),
			Fallbacks:     resolveGenericFontFamilies(run.Font.Fallbacks),
			LetterSpacing: letterSpacing,
			WordBreak:     uint32(run.WordBreak),
			OverflowWrap:  uint32(run.OverflowWrap),
			TextWrap:      textWrapTag(run.TextWrap),
			// The document's alignment, stamped on every run — the native
			// any-run rule reads it (browser-core 43).
			TextAlign: textAlignTag(request.Align),
		}
	}
	// The request is kept (deep-copied: the runs above, the constraint
	// values below) so a parked cache-owned line can re-shape itself.
	shapeRequest := native.ShapeRequest{
		Text:      request.Text,
		FontSize:  request.FontSize,
		Runs:      specs,
		WrapWidth: copyFloat32Ptr(request.WrapWidth),
		LineClamp: copyUint32Ptr(request.LineClamp),
	}
	handle, err := ts.shapeHandle(shapeRequest, nil)
	if err != nil {
		return nil, wrapTextError("ShapeText", err)
	}
	line := &WrappedLine{system: ts, handle: handle, request: shapeRequest, tabs: tabs, tabSizeSpaces: copyUint32Ptr(request.TabSizeSpaces)}
	// The handle counts as live before the summary fetch so the failure
	// path's own Dispose balances the counter.
	ts.liveHandles.Add(1)
	if err := line.refreshSummary(); err != nil {
		_ = line.Dispose()
		return nil, err
	}
	if tabs != nil {
		line.mu.Lock()
		err := line.fitTabsLocked()
		line.mu.Unlock()
		if err != nil {
			_ = line.Dispose()
			return nil, wrapTextError("ShapeText", err)
		}
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

// letterSpacingLigatureFeatures returns the feature list of a run whose
// letter spacing is non-zero, with the optional-ligature features the
// spacing turns off appended: liga, clig and calt = 0 (Chromium's
// FontFeatureRange::FromFontDescription, platform/fonts/shaping/
// font_features.cc:59-86: `letter_spacing` pushes no_liga, no_clig and
// no_calt, and keeps dlig/hlig from being enabled). A tag the caller
// set explicitly is left alone: Chrome appends the font-feature-settings
// values after the letter-spacing-derived ones, and the later feature
// wins in HarfBuzz, so an explicit setting overrides the spacing rule.
// A zero or absent spacing returns the features unchanged.
func letterSpacingLigatureFeatures(features []FontFeature, spacing *float32) []FontFeature {
	if spacing == nil || *spacing == 0 {
		return features
	}
	present := make(map[string]bool, len(features)+3)
	for _, feature := range features {
		present[feature.Tag] = true
	}
	// A copy: never write into the caller's feature slice's spare
	// capacity (the descriptor is shared across frames).
	out := slices.Clone(features)
	for _, tag := range [...]string{"liga", "clig", "calt"} {
		if !present[tag] {
			out = append(out, FontFeature{Tag: tag, Value: 0})
		}
	}
	return out
}

// nativeWrapWidth is the wrap width the native text service is handed: a
// copy of w, with a negative width raised to zero (nil stays nil).
//
// Taffy hands a measured child of a padded box narrower than its padding
// a negative known width (taffy 0.13 flexbox.rs:474 and block.rs:882 do
// not clamp the content box; a flex row's min-content pass sizes a
// full-width, min-w-0 child at zero). The pin's text measure passes that
// width on (R:elements/text.rs:809-827, evaluate_wrap_width), shape_text
// does not check it (R:text_system.rs:350-363), and parley wraps at it
// (gpui_ce_parley text_system.rs:917-919, 1116-1117): no cluster with an
// advance fits, so every break opportunity breaks, as at zero
// (parley line_break.rs:516-523, 626: next_x <= max_advance). The width
// it reports is min(wrap width, advance) (R:text_system/line_layout.rs:
// 646-651), which the measure clamps to zero either way (layout.go
// snapMeasuredSize). The port's native service rejects negative widths
// (reference/native/src/text.rs:1535-1543), so the port hands it zero.
func nativeWrapWidth(w *float32) *float32 {
	if w == nil {
		return nil
	}
	width := max(*w, 0)
	return &width
}

// textWrapTag maps the CSS text-wrap mode onto the native run-record
// tag (0 wrap, 1 nowrap, 2 balance — browser-core 43). The white-space
// nowrap path keeps suppressing the wrap width Go-side (rev 15.1's
// rule), so the port never stamps the nowrap tag here.
func textWrapTag(mode TextWrap) uint32 {
	if mode == TextWrapBalance {
		return native.TextTextWrapBalance
	}
	return native.TextTextWrapWrap
}

// textAlignTag maps the text alignment onto the native run-record tag
// (browser-core 43): Left/Center/Right keep the pinned paint-time
// alignment (tag 0 — the alignment is Go-owned, exactly the pin's
// paint_visual_text model); Start/End/Justify reach the native layout,
// where justify stretches the whitespace clusters and start/end
// resolve against the base direction.
func textAlignTag(align TextAlign) uint32 {
	switch align {
	case TextAlignStart:
		return native.TextTextAlignStart
	case TextAlignEnd:
		return native.TextTextAlignEnd
	case TextAlignJustify:
		return native.TextTextAlignJustify
	default:
		return native.TextTextAlignNone
	}
}

// finiteFloat32 reports whether v is neither NaN nor infinite.
func finiteFloat32(v float32) bool {
	f := float64(v)
	return !math.IsNaN(f) && !math.IsInf(f, 0)
}

// copyFloat32Ptr returns a fresh pointer to a copy of *v (nil for nil).
func copyFloat32Ptr(v *float32) *float32 {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// copyUint32Ptr returns a fresh pointer to a copy of *v (nil for nil).
func copyUint32Ptr(v *uint32) *uint32 {
	if v == nil {
		return nil
	}
	c := *v
	return &c
}

// ---------------------------------------------------------------------------
// The live-handle bound: parking cache-owned lines (Base ticket 40)
// ---------------------------------------------------------------------------

// shapeHandle shapes through the native service and, when the
// process-wide live-handle bound is reached, parks the least recently
// used cache-owned line (never except, the line being re-shaped) and
// retries. It fails with the limit only when no cache-owned line is
// left to park — every live handle is then held outside the caches.
// The caller does not hold except.mu's peers; except.mu may be held.
func (ts *TextSystem) shapeHandle(request native.ShapeRequest, except *WrappedLine) (native.ShapingID, error) {
	for {
		handle, err := ts.svc.Shape(request)
		if err == nil {
			return handle, nil
		}
		if !errors.Is(err, native.ErrTextShapingLimit) || !ts.parkOne(except) {
			return 0, err
		}
	}
}

// parkOne releases the native handle of the least recently used
// cache-owned line that is not in use right now (its mutex is free) and
// is not except. It reports whether a handle was released.
func (ts *TextSystem) parkOne(except *WrappedLine) bool {
	for {
		line := ts.takeParkCandidate(except)
		if line == nil {
			return false
		}
		released := line.parkLocked()
		line.mu.Unlock()
		if released {
			return true
		}
	}
}

// takeParkCandidate removes and returns, locked, the least recently
// used cache-owned line whose mutex is free (a line in use — mid-query
// or mid-re-shape — is skipped, never waited for), or nil.
func (ts *TextSystem) takeParkCandidate(except *WrappedLine) *WrappedLine {
	ts.evictMu.Lock()
	defer ts.evictMu.Unlock()
	for e := ts.evictable.Front(); e != nil; e = e.Next() {
		line := e.Value.(*WrappedLine)
		if line == except || !line.mu.TryLock() {
			continue
		}
		ts.evictable.Remove(e)
		line.lru = nil
		return line
	}
	return nil
}

// markEvictable enrolls a cache-owned line in the parking order (the
// line-layout cache owns it: its only disposer is the cache, and every
// query re-shapes a parked handle transparently). Idempotent.
func (ts *TextSystem) markEvictable(line *WrappedLine) {
	line.mu.Lock()
	defer line.mu.Unlock()
	if line.disposed.Load() || line.evictable {
		return
	}
	line.evictable = true
	if !line.parked {
		ts.evictMu.Lock()
		line.lru = ts.evictable.PushBack(line)
		ts.evictMu.Unlock()
	}
}

// touchLocked marks the line most recently used. The caller holds
// line.mu.
func (ts *TextSystem) touchLocked(line *WrappedLine) {
	if !line.evictable || line.lru == nil {
		return
	}
	ts.evictMu.Lock()
	if line.lru != nil {
		ts.evictable.MoveToBack(line.lru)
	}
	ts.evictMu.Unlock()
}

// forgetLocked removes the line from the parking order. The caller
// holds line.mu.
func (ts *TextSystem) forgetLocked(line *WrappedLine) {
	ts.evictMu.Lock()
	if line.lru != nil {
		ts.evictable.Remove(line.lru)
		line.lru = nil
	}
	ts.evictMu.Unlock()
}

// parkLocked releases the line's native handle, keeping its summary and
// request: the next query re-shapes it. The caller holds line.mu and
// has removed the line from the parking order. Reports whether a live
// handle was released.
func (l *WrappedLine) parkLocked() bool {
	if l.parked || l.disposed.Load() {
		return false
	}
	if err := l.system.svc.Dispose(l.handle); err != nil {
		return false
	}
	l.system.liveHandles.Add(-1)
	l.parked = true
	return true
}

// handleLocked returns the line's live native handle, re-shaping a
// parked line from its stored request first (which may park another
// cache-owned line under the bound). The caller holds l.mu.
func (l *WrappedLine) handleLocked(op string) (native.ShapingID, error) {
	if l.disposed.Load() {
		return 0, fmt.Errorf("gpui: %s: %w", op, ErrTextStaleHandle)
	}
	if !l.parked {
		l.system.touchLocked(l)
		return l.handle, nil
	}
	handle, err := l.system.shapeHandle(l.request, l)
	if err != nil {
		return 0, wrapTextError(op, err)
	}
	l.system.liveHandles.Add(1)
	// The same request re-shapes the same layout; the summary is re-read
	// so it always describes the live handle.
	if err := l.refreshSummaryOf(handle); err != nil {
		if l.system.svc.Dispose(handle) == nil {
			l.system.liveHandles.Add(-1)
		}
		return 0, wrapTextError(op, err)
	}
	l.handle = handle
	l.parked = false
	if l.evictable {
		l.system.evictMu.Lock()
		l.lru = l.system.evictable.PushBack(l)
		l.system.evictMu.Unlock()
	}
	return handle, nil
}

// withHandle runs f with the line's live native handle under the line's
// mutex (a parked line re-shapes first; the handle cannot be parked
// while f runs). f returns the raw native error; withHandle maps it to
// the gpui seam's typed error under op.
func (l *WrappedLine) withHandle(op string, f func(native.ShapingID) error) error {
	if err := l.checkLive(op); err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	handle, err := l.handleLocked(op)
	if err != nil {
		return err
	}
	return wrapTextError(op, f(handle))
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
	// IsRTL reports the document's base direction (browser-core 43,
	// rev 16): the signal the paint layer resolves text-align start/end
	// against.
	IsRTL bool
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
	summary TextLayoutSummary

	// mu guards the native handle's use against its parking (Base
	// ticket 40): every native query runs under it, and the parking
	// walk only takes lines whose mutex is free.
	mu sync.Mutex
	// handle is the native shaping handle; meaningless while parked.
	handle native.ShapingID
	// request is the deep-copied shaping request (the wrap/clamp
	// following Relayout) a parked line re-shapes from.
	request native.ShapeRequest
	// tabs is the hard-tab fit of a text holding tabs (nil otherwise):
	// request then carries the tabs as spaces with their fitted spacing
	// (text_tabs.go).
	tabs *tabFit
	// tabSizeSpaces is the request's browser tab-size (nil: the pin's
	// DirectWrite stops); the browser fit reads it when measuring each
	// tab's stop (browser-core 38).
	tabSizeSpaces *uint32
	// evictable marks a line owned by a line-layout cache (text_cache.go):
	// its handle may be parked under the live-handle bound.
	evictable bool
	// parked reports a released handle: the next query re-shapes.
	parked bool
	// lru is the line's entry in the system's parking order (nil while
	// parked, disposed or not evictable).
	lru *list.Element

	// clustersMu guards the lazily enumerated logical clusters (needed
	// for beyond-end caret/cluster queries; the clusters are pure text
	// facts, so the cache survives relayout).
	clustersMu    sync.Mutex
	clustersCache []TextRange

	// dumps caches the layout's paint dumps (fragments and glyphs) under
	// mu: they are facts of the layout, so paint reads them without the
	// native handle and a parked line paints without re-shaping
	// (gpui-perf ticket 06). Every layout change goes through
	// refreshSummaryOf, which drops them.
	dumps *lineDumps

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
	return wrapTextError("layout summary", l.refreshSummaryOf(l.handle))
}

// refreshSummaryOf re-reads the summary through handle, returning the
// raw native error.
func (l *WrappedLine) refreshSummaryOf(handle native.ShapingID) error {
	l.dumps = nil
	layout, err := l.system.svc.LayoutInfo(handle)
	if err != nil {
		return err
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
		IsRTL:          layout.IsRTL,
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
	var record native.TextLine
	err := l.withHandle("Line", func(handle native.ShapingID) error {
		var err error
		record, err = l.system.svc.Line(handle, index, lineHeight)
		return err
	})
	if err != nil {
		return TextLine{}, err
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
	dumps, err := l.paintDumps("Fragments")
	if err != nil {
		return nil, err
	}
	return slices.Clone(dumps.fragments), nil
}

// Glyphs dumps the positioned glyphs (line-local x, baseline-relative
// y) in fragment order.
func (l *WrappedLine) Glyphs() ([]TextGlyph, error) {
	dumps, err := l.paintDumps("Glyphs")
	if err != nil {
		return nil, err
	}
	return slices.Clone(dumps.glyphs), nil
}

// lineDumps are one layout's paint dumps. They are shared: callers must
// not modify them.
type lineDumps struct {
	fragments []TextFragment
	glyphs    []TextGlyph
}

// paintDumps returns the layout's fragment and glyph dumps, read from the
// native layout once per layout (the paint path reads them every frame).
func (l *WrappedLine) paintDumps(op string) (*lineDumps, error) {
	if err := l.checkLive(op); err != nil {
		return nil, err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.dumps != nil {
		return l.dumps, nil
	}
	handle, err := l.handleLocked(op)
	if err != nil {
		return nil, err
	}
	fragmentRecords, err := l.system.svc.Fragments(handle)
	if err != nil {
		return nil, wrapTextError(op, err)
	}
	glyphRecords, err := l.system.svc.Glyphs(handle)
	if err != nil {
		return nil, wrapTextError(op, err)
	}
	dumps := &lineDumps{
		fragments: make([]TextFragment, len(fragmentRecords)),
		glyphs:    make([]TextGlyph, len(glyphRecords)),
	}
	for i, record := range fragmentRecords {
		dumps.fragments[i] = TextFragment{
			FontID:     FontID(record.FontID),
			FontSize:   record.FontSize,
			XStart:     record.XStart,
			XEnd:       record.XEnd,
			GlyphStart: record.GlyphStart,
			GlyphCount: record.GlyphCount,
		}
	}
	for i, record := range glyphRecords {
		dumps.glyphs[i] = TextGlyph{
			ID:      record.ID,
			X:       record.X,
			Y:       record.Y,
			IsEmoji: record.IsEmoji,
		}
	}
	l.dumps = dumps
	return dumps, nil
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
	var record native.TextCaret
	err := l.withHandle("Caret", func(handle native.ShapingID) error {
		var err error
		record, err = l.system.svc.Caret(handle, byteIndex, native.CaretAffinity(affinity), lineHeight)
		return err
	})
	if err != nil {
		return TextCaret{}, err
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
	var record native.TextHit
	err := l.withHandle("HitTest", func(handle native.ShapingID) error {
		var err error
		record, err = l.system.svc.HitTest(handle, x, y, lineHeight)
		return err
	})
	if err != nil {
		return TextHit{}, err
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
	var records []native.TextRect
	err := l.withHandle("SelectionRects", func(handle native.ShapingID) error {
		var err error
		records, err = l.system.svc.SelectionRects(handle, start, end, lineHeight)
		return err
	})
	if err != nil {
		return nil, err
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
	var record native.TextCluster
	err := l.withHandle("ClusterBefore", func(handle native.ShapingID) error {
		var err error
		record, err = l.system.svc.Cluster(handle, byteIndex, native.CaretAffinity(affinity), native.ClusterBefore)
		return err
	})
	if err != nil {
		return TextCluster{}, err
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
	var record native.TextCluster
	err := l.withHandle("ClusterAfter", func(handle native.ShapingID) error {
		var err error
		record, err = l.system.svc.Cluster(handle, byteIndex, native.CaretAffinity(affinity), native.ClusterAfter)
		return err
	})
	if err != nil {
		return TextCluster{}, err
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
	var advanceErr error
	err := l.withHandle("Clusters", func(handle native.ShapingID) error {
		index := 0
		for {
			cluster, err := l.system.svc.Cluster(handle, index, native.CaretDownstream, native.ClusterAfter)
			if err != nil {
				return err
			}
			if !cluster.Present {
				return nil
			}
			if cluster.Range.End <= index {
				advanceErr = fmt.Errorf("gpui: Clusters: %w: logical cluster %d..%d does not advance caret %d", ErrTextValue, cluster.Range.Start, cluster.Range.End, index)
				return nil
			}
			clusters = append(clusters, TextRange{Start: cluster.Range.Start, End: cluster.Range.End})
			index = cluster.Range.End
		}
	})
	if err != nil {
		return nil, err
	}
	if advanceErr != nil {
		return nil, advanceErr
	}
	return clusters, nil
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
// summary. Wrap widths must be finite (a negative one wraps like zero,
// nativeWrapWidth); clamps >= 1. The logical clusters are pure text facts
// and survive the re-layout.
func (l *WrappedLine) Relayout(wrapWidth *float32, lineClamp *uint32) error {
	if err := l.checkLive("Relayout"); err != nil {
		return err
	}
	if wrapWidth != nil && !finiteFloat32(*wrapWidth) {
		return fmt.Errorf("gpui: Relayout: %w: wrap width must be finite", ErrTextValue)
	}
	wrapWidth = nativeWrapWidth(wrapWidth)
	if lineClamp != nil && *lineClamp < 1 {
		return fmt.Errorf("gpui: Relayout: %w: line clamp must be >= 1", ErrTextValue)
	}
	return l.withHandle("Relayout", func(handle native.ShapingID) error {
		if err := l.system.svc.Relayout(handle, wrapWidth, lineClamp); err != nil {
			return err
		}
		// A parked line re-shapes under the constraints it now carries.
		l.request.WrapWidth = copyFloat32Ptr(wrapWidth)
		l.request.LineClamp = copyUint32Ptr(lineClamp)
		if err := l.refreshSummaryOf(handle); err != nil {
			return err
		}
		// New rows move the tabs: fit them to their stops again.
		if l.tabs != nil {
			return l.fitTabsLocked()
		}
		return nil
	})
}

// Dispose releases the native shaping handle; the line is stale
// afterwards (every query fails with ErrTextStaleHandle). Dispose is
// idempotent. A parked line holds no handle: it only leaves the
// parking order.
func (l *WrappedLine) Dispose() error {
	if l == nil {
		return nil
	}
	if l.disposed.Swap(true) {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.system.forgetLocked(l)
	if l.parked {
		return nil
	}
	if err := l.system.svc.Dispose(l.handle); err != nil {
		return wrapTextError("Dispose", err)
	}
	l.system.liveHandles.Add(-1)
	return nil
}
