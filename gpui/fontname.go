package gpui

// The font name-table reader behind FontRegistration.Families
// (consumer gap 06): AddFont reports the family name(s) the submitted
// bytes landed under, parsed with the pinned fontique precedence.
//
// Fontique's register_font_impl picks ONE family per face:
// english_or_first_name(TYPOGRAPHIC_FAMILY_NAME) (nameID 16) or else
// english_or_first_name(FAMILY_NAME) (nameID 1) — so a font whose
// legacy family (nameID 1) differs from its typographic family
// (Geist Medium: nameID 1 "Geist Medium", nameID 16 "Geist") registers
// under the typographic name only, and a descriptor naming the legacy
// family silently resolves through the fallback (the gap this report
// closes: the caller can SEE the name to request).
//
// english_or_first_name (fontique 0.11.1 src/scan.rs) ranks records
// for a name id: language 0x0409 (English US) wins outright (first
// such record), else language 0, else the first record; other
// languages are skipped unless they hold the first slot. The parser
// below reproduces that ranking exactly, including its skip rule.

import (
	"encoding/binary"
	"fmt"
	"unicode/utf16"
)

// nameIDFamily is the legacy family name (name table nameID 1).
const nameIDFamily = 1

// nameIDTypographicFamily is the typographic family name (nameID 16).
const nameIDTypographicFamily = 16

// fontTagTrueType is the sfnt version tag of a TrueType face.
const fontTagTrueType = 0x00010000

// fontTagOpenType is the sfnt version tag of a CFF OpenType face.
const fontTagOpenType = 0x4F54544F // 'OTTO'

// fontTagTrueTypeApple is Apple's TrueType sfnt version tag.
const fontTagTrueTypeApple = 0x74727565 // 'true'

// fontTagCollection is the TrueType Collection tag ('ttcf').
const fontTagCollection = 0x74746366

// fontFamilyNames parses the family name each face in font data would
// register under, in file order (fontique's register_font_impl
// precedence: english-or-first nameID 16, else nameID 1). Collections
// ('ttcf') contribute one entry per face; single faces answer a
// one-element slice; a face whose name table carries neither name is
// skipped (fontique skips it too). Malformed data answers a typed
// error — the caller reports NO families for bytes the catalog would
// reject, never a guessed name.
func fontFamilyNames(data []byte) ([]string, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("font data too short for an sfnt header")
	}
	tag := binary.BigEndian.Uint32(data[0:4])
	switch tag {
	case fontTagCollection:
		count := binary.BigEndian.Uint32(data[8:12])
		if count == 0 {
			return nil, nil
		}
		if len(data) < 12+int(count)*4 {
			return nil, fmt.Errorf("truncated collection header")
		}
		names := make([]string, 0, count)
		seen := make(map[string]bool, count)
		for i := uint32(0); i < count; i++ {
			offset := binary.BigEndian.Uint32(data[12+int(i)*4 : 16+int(i)*4])
			if int(offset) >= len(data) {
				return nil, fmt.Errorf("collection face offset %d out of range", offset)
			}
			name, ok, err := faceFamilyName(data[offset:])
			if err != nil {
				return nil, err
			}
			if ok && !seen[name] {
				seen[name] = true
				names = append(names, name)
			}
		}
		return names, nil
	case fontTagTrueType, fontTagOpenType, fontTagTrueTypeApple:
		name, ok, err := faceFamilyName(data)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, nil
		}
		return []string{name}, nil
	default:
		return nil, fmt.Errorf("not an sfnt font (version tag %#x)", tag)
	}
}

// faceFamilyName reads one face's registering family name from its
// name table. ok is false when the table carries neither the
// typographic nor the legacy family name (fontique skips such faces).
func faceFamilyName(face []byte) (string, bool, error) {
	if len(face) < 12 {
		return "", false, fmt.Errorf("font data too short for a table directory")
	}
	numTables := binary.BigEndian.Uint16(face[4:6])
	if numTables == 0 {
		return "", false, nil
	}
	directoryLen := 12 + int(numTables)*16
	if len(face) < directoryLen {
		return "", false, fmt.Errorf("truncated table directory")
	}
	for i := 0; i < int(numTables); i++ {
		rec := face[12+i*16 : 12+i*16+16]
		if string(rec[0:4]) != "name" {
			continue
		}
		offset := binary.BigEndian.Uint32(rec[8:12])
		length := binary.BigEndian.Uint32(rec[12:16])
		if int(offset) >= len(face) || int(offset)+int(length) > len(face) {
			return "", false, fmt.Errorf("name table out of range")
		}
		return nameTableFamilyName(face[offset : offset+length])
	}
	// No name table: the face registers under no family (fontique's
	// family_chars lookup misses and register_font_impl skips it).
	return "", false, nil
}

// nameTableFamilyName applies fontique's precedence over one name
// table: the english-or-first record for nameID 16, else for nameID 1.
func nameTableFamilyName(table []byte) (string, bool, error) {
	if len(table) < 6 {
		return "", false, fmt.Errorf("truncated name table header")
	}
	format := binary.BigEndian.Uint16(table[0:2])
	count := binary.BigEndian.Uint16(table[2:4])
	stringOffset := binary.BigEndian.Uint16(table[4:6])
	if format != 0 {
		// Format 1 adds a language-tag record the ranking ignores; the
		// records are laid out the same way. Parse it with the same
		// reader (no consumer hits it; honest reject keeps us from
		// guessing the layout).
		return "", false, fmt.Errorf("unsupported name table format %d", format)
	}
	recordsLen := 6 + int(count)*12
	if len(table) < recordsLen {
		return "", false, fmt.Errorf("truncated name records")
	}
	lookup := func(nameID uint16) (string, bool) {
		// fontique's english_or_first ranking: 0x0409 breaks the scan;
		// language 0 ranks 2; only the first record can rank 1.
		best := ""
		bestRank := -1
		for i := 0; i < int(count); i++ {
			rec := table[6+i*12 : 6+i*12+12]
			if binary.BigEndian.Uint16(rec[6:8]) != nameID {
				continue
			}
			platformID := binary.BigEndian.Uint16(rec[0:2])
			encodingID := binary.BigEndian.Uint16(rec[2:4])
			languageID := binary.BigEndian.Uint16(rec[4:6])
			length := binary.BigEndian.Uint16(rec[8:10])
			offset := binary.BigEndian.Uint16(rec[10:12])
			rank := 0
			switch {
			case languageID == 0x0409:
				rank = 3
			case languageID == 0:
				rank = 2
			case i == 0:
				rank = 1
			default:
				continue
			}
			if rank > bestRank {
				if rank == 3 {
					// English US wins outright (fontique breaks the scan).
					if text, ok := decodeNameRecord(table, stringOffset, offset, length, platformID, encodingID); ok {
						return text, true
					}
					continue
				}
				bestRank = rank
				if text, ok := decodeNameRecord(table, stringOffset, offset, length, platformID, encodingID); ok {
					best = text
				}
			}
		}
		if bestRank > 0 && best != "" {
			return best, true
		}
		return "", false
	}
	if name, ok := lookup(nameIDTypographicFamily); ok {
		return name, true, nil
	}
	if name, ok := lookup(nameIDFamily); ok {
		return name, true, nil
	}
	return "", false, nil
}

// decodeNameRecord extracts one record's string from the table's
// string storage, decoding the encodings a family name can carry:
// Windows Unicode (platform 3, encoding 1) and the Unicode platform as
// UTF-16BE, Macintosh Roman (platform 1, encoding 0) as Latin-1.
// Undecodable strings answer false (the ranking falls through, exactly
// like skrifa's lossy decode never inventing a name).
func decodeNameRecord(table []byte, stringOffset, offset, length uint16, platformID, encodingID uint16) (string, bool) {
	start := int(stringOffset) + int(offset)
	end := start + int(length)
	if start < 0 || end > len(table) || length == 0 {
		return "", false
	}
	raw := table[start:end]
	switch {
	case platformID == 3 && encodingID == 1, platformID == 3 && encodingID == 10, platformID == 0:
		if len(raw)%2 != 0 {
			return "", false
		}
		units := make([]uint16, len(raw)/2)
		for i := range units {
			units[i] = binary.BigEndian.Uint16(raw[i*2 : i*2+2])
		}
		runes := utf16.Decode(units)
		text := string(runes)
		if text == "" {
			return "", false
		}
		return text, true
	case platformID == 1 && encodingID == 0:
		text := string(raw)
		if text == "" {
			return "", false
		}
		return text, true
	default:
		return "", false
	}
}
