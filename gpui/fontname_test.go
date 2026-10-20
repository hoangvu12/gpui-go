package gpui

// The font name-table reader behind FontRegistration.Families
// (consumer gap 06). The parser is pinned to fontique's
// register_font_impl precedence — the typographic family (nameID 16)
// or else the legacy family (nameID 1), each read with
// english_or_first_name's ranking — so the report names exactly what
// the catalog registers. The fixtures: Geist Medium (nameID 1 "Geist
// Medium", nameID 16 "Geist" — the mismatch that motivated the
// report), Geist Regular (both names agree), and malformed data.

import (
	"os"
	"path/filepath"
	"testing"
)

// readTestFont loads one of the committed font fixtures.
func readTestFont(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// TestFontFamilyNamesTypographicWins: a face whose nameID 1 and 16
// differ reports the TYPOGRAPHIC family (the name it lands under in
// the catalog), not the legacy family.
func TestFontFamilyNamesTypographicWins(t *testing.T) {
	names, err := fontFamilyNames(readTestFont(t, "Geist-Medium.ttf"))
	if err != nil {
		t.Fatalf("fontFamilyNames: %v", err)
	}
	if len(names) != 1 || names[0] != "Geist" {
		t.Fatalf("families = %v, want [Geist] (the typographic family, not the legacy \"Geist Medium\")", names)
	}
}

// TestFontFamilyNamesAgreeingNames: a face whose names agree reports
// that family.
func TestFontFamilyNamesAgreeingNames(t *testing.T) {
	for _, font := range []string{"Geist-Regular.ttf", "GeistMono-Regular.ttf"} {
		names, err := fontFamilyNames(readTestFont(t, font))
		if err != nil {
			t.Fatalf("fontFamilyNames(%s): %v", font, err)
		}
		if len(names) != 1 {
			t.Fatalf("%s families = %v, want one entry", font, names)
		}
	}
	if names, _ := mustNames(t, "Geist-Regular.ttf"); names[0] != "Geist" {
		t.Errorf("Geist Regular family = %q, want Geist", names[0])
	}
}

// mustNames reads one font's family names or fails the test.
func mustNames(t *testing.T, name string) ([]string, error) {
	t.Helper()
	names, err := fontFamilyNames(readTestFont(t, name))
	if err != nil {
		t.Fatalf("fontFamilyNames(%s): %v", name, err)
	}
	return names, err
}

// TestFontFamilyNamesRejectsNonFonts: data that is not an sfnt font
// errors — the report never invents a name.
func TestFontFamilyNamesRejectsNonFonts(t *testing.T) {
	for name, data := range map[string][]byte{
		"empty":      {},
		"too short":  {0x00, 0x01, 0x00, 0x00, 0x00},
		"not a font": []byte("definitely not a font header"),
		"PE garbage": {0x4D, 0x5A, 0x90, 0x00, 0x03, 0x00},
	} {
		if _, err := fontFamilyNames(data); err == nil {
			t.Errorf("%s: fontFamilyNames accepted the data", name)
		}
	}
}

// TestFontFamilyNamesTruncatedTableDirectory: a truncated table
// directory errors instead of reporting a partial guess.
func TestFontFamilyNamesTruncatedTableDirectory(t *testing.T) {
	data := append([]byte(nil), readTestFont(t, "Geist-Regular.ttf")...)
	// Claim more tables than the data holds.
	data[4] = 0xFF
	data[5] = 0xFF
	if _, err := fontFamilyNames(data); err == nil {
		t.Error("fontFamilyNames accepted a truncated table directory")
	}
}

// TestFontFamilyNamesOutOfRangeNameTable: a name table pointer that
// runs past the data errors (the reader never guesses from a table it
// cannot read).
func TestFontFamilyNamesOutOfRangeNameTable(t *testing.T) {
	data := append([]byte(nil), readTestFont(t, "Geist-Regular.ttf")...)
	numTables := int(data[4])<<8 | int(data[5])
	for i := 0; i < numTables; i++ {
		rec := data[12+i*16 : 12+i*16+16]
		if string(rec[0:4]) == "name" {
			// Offset 0x7FFFFFFF: out of range.
			rec[8], rec[9], rec[10], rec[11] = 0x7F, 0xFF, 0xFF, 0xFF
			break
		}
	}
	if _, err := fontFamilyNames(data); err == nil {
		t.Error("fontFamilyNames accepted a name table that runs past the data")
	}
}

// TestFontFamilyNamesEnglishFirstRanking: the english-or-first ranking
// picks the 0x0409 record, then language 0, over the first record.
func TestFontFamilyNamesEnglishFirstRanking(t *testing.T) {
	// Handcraft a minimal name table: one record per (nameID, language)
	// pair, each holding the UTF-16BE string "A<i>" so the winner is
	// identifiable.
	build := func(pairs [][2]uint16) []byte {
		table := []byte{0, 0, byte(len(pairs) >> 8), byte(len(pairs)), 0, 0}
		storage := []byte{}
		offsets := make([]int, len(pairs))
		for i := range pairs {
			offsets[i] = len(storage)
			storage = append(storage, 0x00, 'A', 0x00, byte('0'+i))
		}
		for i, pair := range pairs {
			nameID, lang := pair[0], pair[1]
			table = append(table,
				0, 3, // platform Windows
				0, 1, // encoding Unicode BMP
				byte(lang>>8), byte(lang), // language
				byte(nameID>>8), byte(nameID),
				0, 4, // length 4 (two UTF-16 units)
				byte(offsets[i]>>8), byte(offsets[i]))
		}
		table[4], table[5] = byte(len(table)>>8), byte(len(table)) // stringOffset
		return append(table, storage...)
	}
	family := uint16(16)
	// 0x0409 wins over an earlier language-0 record.
	if name, ok, err := nameTableFamilyName(build([][2]uint16{{family, 0}, {family, 0x0409}})); err != nil || !ok || name != "A1" {
		t.Errorf("english ranking = %q %v, want A1 (the 0x0409 record)", name, ok)
	}
	// No 0x0409: language 0 wins over a non-English first record.
	if name, ok, err := nameTableFamilyName(build([][2]uint16{{family, 0x040C}, {family, 0}})); err != nil || !ok || name != "A1" {
		t.Errorf("language-0 ranking = %q %v, want A1 (the language-0 record)", name, ok)
	}
	// Only a non-English first record: the first record ranks.
	if name, ok, err := nameTableFamilyName(build([][2]uint16{{family, 0x040C}})); err != nil || !ok || name != "A0" {
		t.Errorf("first-record ranking = %q %v, want A0 (the only, first record)", name, ok)
	}
	// A non-English nameID-16 record that is NOT first is skipped, and
	// with no nameID-1 record the table answers no family at all.
	if _, ok, _ := nameTableFamilyName(build([][2]uint16{{4, 0x0409}, {family, 0x040C}})); ok {
		t.Error("a non-first non-English record ranked; fontique skips those")
	}
}

// TestFontFamilyNamesCollection: a TrueType Collection reports one
// entry per face in file order, deduplicated — the same families
// fontique registers for the blob.
func TestFontFamilyNamesCollection(t *testing.T) {
	// Build a valid collection from two real faces: the 'ttcf' header
	// (tag, version, count, per-face offsets) with each face's bytes
	// concatenated (a face's table offsets are relative to its own
	// start, so concatenation is layout-correct).
	regular := readTestFont(t, "Geist-Regular.ttf")
	medium := readTestFont(t, "Geist-Medium.ttf")
	mono := readTestFont(t, "GeistMono-Regular.ttf")
	build := func(faces ...[]byte) []byte {
		headerLen := 12 + 4*len(faces)
		out := []byte{'t', 't', 'c', 'f', 0, 2, 0, 0, byte(len(faces) >> 24), byte(len(faces) >> 16), byte(len(faces) >> 8), byte(len(faces))}
		offset := headerLen
		for _, face := range faces {
			out = append(out, byte(offset>>24), byte(offset>>16), byte(offset>>8), byte(offset))
			offset += len(face)
		}
		for _, face := range faces {
			out = append(out, face...)
		}
		return out
	}

	// Two faces of the one family: deduplicated to one entry.
	names, err := fontFamilyNames(build(regular, medium))
	if err != nil {
		t.Fatalf("fontFamilyNames(collection): %v", err)
	}
	if len(names) != 1 || names[0] != "Geist" {
		t.Fatalf("families = %v, want [Geist] (two faces of one family, deduplicated)", names)
	}

	// Two families: one entry per family, in file order.
	names, err = fontFamilyNames(build(medium, mono))
	if err != nil {
		t.Fatalf("fontFamilyNames(collection): %v", err)
	}
	if len(names) != 2 || names[0] != "Geist" || names[1] != "Geist Mono" {
		t.Fatalf("families = %v, want [Geist Geist Mono] in file order", names)
	}

	// A zero-face collection reports no families.
	if names, err = fontFamilyNames(build()); err != nil || len(names) != 0 {
		t.Fatalf("empty collection: %v %v, want no families", names, err)
	}
}
