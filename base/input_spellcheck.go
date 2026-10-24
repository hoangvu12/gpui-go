package base

// The spellcheck hook of the text input (browser-core ticket 30):
// the opt-in flag, the request the element queues when the document
// changed, and the misspelled ranges the renderer draws as red wavy
// underlines — the presentation Chromium's Blink gives misspelling
// markers:
//
//   - the marker is a wavy underline: spelling errors use the wavy
//     stroke with the spelling-grammar wave (core/paint/
//     text_decoration_info.cc:467-481 with MakeSpellingGrammarWave,
//     :136-146);
//   - spelling markers are red: LayoutTheme::
//     PlatformSpellingMarkerUnderlineColor, core/layout/layout_theme.cc
//     :413-416 (grammar is grey, :417-420 — the Windows checker does
//     not report grammar here);
//   - the markers are document ranges the renderer composes over the
//     syntax highlighting, like diagnostics (the marker lists of
//     core/editing/markers).
//
// The request path is element-driven: the text element's prepaint
// queues a check whenever the document revision moved past the last
// requested one (input_element.go's requestSpellcheck call). That is
// the smallest hook that covers typing, programmatic text changes and
// the initial value — the state-side text mutation sites are spread
// over four methods and two of them (DefaultValue) run before the
// state is an entity with a window.
//
// Off by default: apps opt in per input with Spellcheck(true) (HTML's
// spellcheck attribute; Chrome's UA default enables it for most text
// fields, but a component library follows the explicit opt-in so no
// application pays for background checks — and no native service
// starts — by surprise). Masked inputs never spellcheck (password
// fields must not leak their text to the checker).

import (
	"github.com/hoangvu12/gpui-go/gpui"
)

// defaultSpellcheckLang is the BCP-47 tag checked when an opted-in
// input sets no language: en-US, the dictionary this port's Windows
// target carries by default.
const defaultSpellcheckLang = "en-US"

// spellcheckNeverRequested marks an input that queued no check yet;
// it differs from every document revision, including the 0 a
// DefaultValue-set document keeps.
const spellcheckNeverRequested = ^uint64(0)

// Spellcheck opts this input into spell checking: misspelled ranges
// draw as red wavy underlines once the platform checker reports them
// (off by default; masked inputs never check).
func (s InputBaseState[M]) Spellcheck(enabled bool) InputBaseState[M] {
	s.spellcheckEnabled = enabled
	return s
}

// SpellcheckLanguage sets the BCP-47 language tag the checks use
// ("en-US" when unset).
func (s InputBaseState[M]) SpellcheckLanguage(lang string) InputBaseState[M] {
	s.spellcheckLang = lang
	return s
}

// SpellcheckEnabled reports the opt-in.
func (s *InputBaseState[M]) SpellcheckEnabled() bool { return s.spellcheckEnabled }

// SpellcheckLanguageTag returns the configured BCP-47 tag (the empty
// string before SpellcheckLanguage set one).
func (s *InputBaseState[M]) SpellcheckLanguageTag() string { return s.spellcheckLang }

// SpellcheckRanges returns the misspelled ranges of the current value,
// in document byte offsets, newest check first applied; nil when
// nothing is misspelled or no result has arrived yet.
func (s *InputBaseState[M]) SpellcheckRanges() []gpui.TextRange {
	return s.spellcheckRanges
}

// queueSpellcheckCheck queues a check of the current text through the
// application's spellcheck service when the document moved past the
// last requested revision. The delivery converts the reported UTF-16
// ranges to document byte ranges and repaints. Runs on the foreground
// (the element's prepaint); queueing never blocks.
func (s *InputBaseState[M]) queueSpellcheckCheck(entity gpui.Entity[InputBaseState[M]], app *gpui.App) {
	if !s.spellcheckEnabled || s.masked || s.spellcheckRequestedRevision == s.documentRevision {
		return
	}
	s.spellcheckRequestedRevision = s.documentRevision
	lang := s.spellcheckLang
	if lang == "" {
		lang = defaultSpellcheckLang
	}
	text := s.text.String()
	// The delivery borrows a weak entity identity: a closed input drops
	// its late result instead of panicking on a released entity.
	input := entity.Downgrade()
	app.Spellcheck().Check(input.EntityID(), text, lang,
		func(result gpui.SpellcheckResult, app *gpui.App) {
			_ = input.TryUpdate(app, func(st *InputBaseState[M], cx *gpui.Context[InputBaseState[M]]) {
				st.applySpellcheckResult(result, cx)
			})
		})
}

// applySpellcheckResult stores one delivered check: the misspelled
// ranges of the checked text, dropped when the text moved on since
// (the newer request supersedes at the service; this guard covers the
// window before the element queues it). A failed check clears the
// markers of the same text — an unknown state draws nothing stale.
func (s *InputBaseState[M]) applySpellcheckResult(result gpui.SpellcheckResult, cx *gpui.Context[InputBaseState[M]]) {
	if result.Text != s.text.String() {
		return
	}
	if result.Err != nil {
		if len(s.spellcheckRanges) > 0 {
			s.spellcheckRanges = nil
			cx.Notify()
		}
		return
	}
	ranges := spellcheckByteRanges(result.Text, result.Misspellings)
	if sameRanges(s.spellcheckRanges, ranges) {
		return
	}
	s.spellcheckRanges = ranges
	cx.Notify()
}

// spellcheckByteRanges converts the checker's UTF-16 misspellings to
// document byte ranges (the offsets the highlight styles use; the
// conversion is gpui's IME utf_range_16to8 rule).
func spellcheckByteRanges(text string, misspellings []gpui.Misspelling) []gpui.TextRange {
	if len(misspellings) == 0 {
		return nil
	}
	var ranges []gpui.TextRange
	for _, m := range misspellings {
		if m.End <= m.Start {
			continue
		}
		start := gpui.UTF16ToUTF8Offset(text, m.Start)
		end := gpui.UTF16ToUTF8Offset(text, m.End)
		if start >= end {
			continue
		}
		ranges = append(ranges, gpui.TextRange{Start: start, End: end})
	}
	return ranges
}

// sameRanges compares two byte-range lists.
func sameRanges(a, b []gpui.TextRange) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// spellcheckHighlightStyles returns the highlight ranges of the
// misspellings intersecting visible, clipped to it (document byte
// offsets, like the diagnostics' entries).
func (s *InputBaseState[M]) spellcheckHighlightStyles(visible gpui.TextRange) []gpui.HighlightRange {
	if !s.spellcheckEnabled || len(s.spellcheckRanges) == 0 {
		return nil
	}
	var styles []gpui.HighlightRange
	for _, r := range s.spellcheckRanges {
		start, end := max(r.Start, visible.Start), min(r.End, visible.End)
		if start >= end {
			continue
		}
		styles = append(styles, gpui.HighlightRange{
			Range: gpui.TextRange{Start: start, End: end},
			Style: spellcheckHighlightStyle(),
		})
	}
	return styles
}

// spellcheckHighlightStyle is the misspelling marker: a red wavy
// underline (Blink's misspelling marker, layout_theme.cc:413-416's
// red over text_decoration_info.cc:467-481's wavy stroke). The shape
// matches the diagnostics' wavy underline (editor_diagnostics.go).
func spellcheckHighlightStyle() gpui.HighlightStyle {
	red := gpui.Red()
	return gpui.HighlightStyle{Underline: &gpui.UnderlineStyle{Color: &red, Thickness: 1, Wavy: true}}
}

// requestSpellcheck queues a spellcheck check from the element's
// prepaint when the document changed since the last request (the
// element drives the request because it owns the frame cadence; see
// the file header). Runs on the foreground.
func (e *textElement[M]) requestSpellcheck(state *InputBaseState[M], app *gpui.App) {
	if !state.spellcheckEnabled || state.masked || state.text.Len() == 0 {
		return
	}
	state.queueSpellcheckCheck(e.state, app)
}
