package gpui

// Ticket 06 (browser-core): the CSS generic families resolve. The three
// keywords map to Chrome's Windows default families (Arial / Times New
// Roman / Consolas — Blink's FontSelector::FamilyNameFromSettings,
// platform/fonts/font_selector.cc:74-83, whose Windows settings match
// Fontique's Windows generic table, fontique-0.11.1/src/backend/
// dwrite.rs:31-39). The mapping happens at the native boundary
// (resolveGenericFontFamily), so resolution AND shaping see the
// concrete family.

import (
	"slices"
	"testing"
)

// TestGenericFontFamiliesResolveToChromeWindowsDefaults pins the alias
// table itself: case-insensitive keyword match, other names untouched,
// fallback lists mapped in place.
func TestGenericFontFamiliesResolveToChromeWindowsDefaults(t *testing.T) {
	cases := []struct {
		family string
		want   string
	}{
		{"sans-serif", "Arial"},
		{"serif", "Times New Roman"},
		{"monospace", "Consolas"},
		{"SANS-SERIF", "Arial"},
		{"Monospace", "Consolas"},
		{"Arial", "Arial"},
		{".SystemUIFont", ".SystemUIFont"},
		{"My Custom Font", "My Custom Font"},
		{"sans", "sans"},
	}
	for _, c := range cases {
		if got := resolveGenericFontFamily(c.family); got != c.want {
			t.Errorf("resolveGenericFontFamily(%q) = %q, want %q", c.family, got, c.want)
		}
	}
	if got := resolveGenericFontFamilies([]string{"serif", "Cambria"}); !slices.Equal(got, []string{"Times New Roman", "Cambria"}) {
		t.Errorf("fallback mapping = %v", got)
	}
	shared := []string{"Cambria"}
	if got := resolveGenericFontFamilies(shared); !slices.Equal(got, shared) {
		t.Errorf("an unmapped fallback list must pass through: %v", got)
	}
}

// TestGenericFontFamilyResolutionAndShaping pins the mapping where it
// matters: resolving and shaping "sans-serif"/"serif"/"monospace" gives
// exactly the concrete family's font and geometry (through the real
// text service; fails, never skips, without the artifact).
func TestGenericFontFamilyResolutionAndShaping(t *testing.T) {
	ts, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("text system: %v", err)
	}
	for _, c := range []struct {
		keyword, concrete string
	}{
		{"sans-serif", "Arial"},
		{"serif", "Times New Roman"},
		{"monospace", "Consolas"},
	} {
		t.Run(c.keyword, func(t *testing.T) {
			resolve := func(family string) FontIdentity {
				identity, err := ts.ResolveFont(FontDescriptor{Family: family, Weight: DefaultFontWeight})
				if err != nil {
					t.Fatalf("resolving %q: %v", family, err)
				}
				return identity
			}
			if got, want := resolve(c.keyword).FontID, resolve(c.concrete).FontID; got != want {
				t.Errorf("ResolveFont(%q) = %v, want the concrete family %q's %v", c.keyword, got, c.concrete, want)
			}

			const text = "Sphinx of black quartz, judge my vow 0123"
			summary := func(family string) TextLayoutSummary {
				line, err := ts.ShapeText(TextLayoutRequest{
					Text:     text,
					FontSize: 16,
					Runs:     []TextRun{{Len: len(text), Font: FontDescriptor{Family: family, Weight: DefaultFontWeight}}},
				})
				if err != nil {
					t.Fatalf("shaping %q: %v", family, err)
				}
				t.Cleanup(func() { _ = line.Dispose() })
				summary, err := line.Summary()
				if err != nil {
					t.Fatalf("summary %q: %v", family, err)
				}
				return summary
			}
			keyword, concrete := summary(c.keyword), summary(c.concrete)
			if keyword.Width != concrete.Width || keyword.Ascent != concrete.Ascent || keyword.Descent != concrete.Descent || keyword.GlyphCount != concrete.GlyphCount {
				t.Errorf("shaping %q = %+v differs from the concrete %q = %+v", c.keyword, keyword, c.concrete, concrete)
			}
		})
	}
}
