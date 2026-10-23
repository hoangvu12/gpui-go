//go:build windows

package gpui

// Browser-core ticket 03's cursor gates (research browser-parity 03
// §12; zterm #23): the four CSS cursor styles the reference's loader
// maps to the arrow (gpui_windows/src/util.rs:99-115, the catch-all at
// :114) — grab (open hand), grabbing (closed hand), copy and
// context-menu — load real, distinct Windows cursors built at runtime
// with CreateCursor (the research's recommended alternative to
// bundling .cur assets; no native rebuild). Windows ships no system
// cursors for these glyphs, so the port draws 32x32 monochrome ones.
//
// Nothing here opens a window: the gates are pure table, encoder and
// loader checks.

import "testing"

// builtCursorStyles are the four styles that load runtime-built
// cursors, in cursorGlyphs order.
var builtCursorStyles = []CursorStyle{CursorOpenHand, CursorClosedHand, CursorDragCopy, CursorContextualMenu}

// TestCursorBuiltStylesLoadDistinctCursors is the ticket's cursor-table
// gate: each of the four styles resolves through loadCursorHandle to a
// loaded cursor (non-zero), distinct from the arrow and from each
// other, and the cache keeps the handle stable across calls.
func TestCursorBuiltStylesLoadDistinctCursors(t *testing.T) {
	arrow := loadCursorHandle(CursorArrow)
	if arrow == 0 {
		t.Fatal("the system arrow cursor did not load")
	}
	seen := map[uintptr]CursorStyle{}
	for _, style := range builtCursorStyles {
		handle := loadCursorHandle(style)
		if handle == 0 {
			t.Fatalf("style %d did not load a cursor", style)
		}
		if handle == arrow {
			t.Fatalf("style %d still falls back to the arrow", style)
		}
		if other, clash := seen[handle]; clash {
			t.Fatalf("styles %d and %d share one cursor handle", other, style)
		}
		seen[handle] = style
		if again := loadCursorHandle(style); again != handle {
			t.Fatalf("style %d reloaded as %#x, want the cached %#x", style, again, handle)
		}
	}
	// The mapped styles keep loading their shared system cursors.
	if hand := loadCursorHandle(CursorPointingHand); hand == 0 || hand == arrow {
		t.Fatalf("the pointing hand system cursor did not load (%#x, arrow %#x)", hand, arrow)
	}
}

// TestCursorBuiltGlyphArt pins the glyph table itself: 32 rows of 32
// cells over the three monochrome cursor colors, hotspots inside the
// bitmap, every white cell enclosed by black or white (no ragged
// white-to-transparent edges), and no blank glyph.
func TestCursorBuiltGlyphArt(t *testing.T) {
	if len(cursorGlyphs) != 4 {
		t.Fatalf("the glyph table has %d entries, want 4", len(cursorGlyphs))
	}
	for _, spec := range cursorGlyphs {
		if len(spec.art) != 32 {
			t.Fatalf("style %d: %d art rows, want 32", spec.style, len(spec.art))
		}
		inked := 0
		for y, row := range spec.art {
			if len(row) != 32 {
				t.Fatalf("style %d: row %d has %d cells, want 32", spec.style, y, len(row))
			}
			for x := 0; x < 32; x++ {
				switch row[x] {
				case '#', 'o':
					inked++
				case '.':
				default:
					t.Fatalf("style %d: cell (%d,%d) is %q, want one of '#', 'o', '.'", spec.style, x, y, row[x])
				}
				if row[x] == 'o' {
					for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
						nx, ny := x+d[0], y+d[1]
						if nx < 0 || ny < 0 || nx >= 32 || ny >= 32 {
							continue
						}
						if spec.art[ny][nx] == '.' {
							t.Fatalf("style %d: white at (%d,%d) touches transparent at (%d,%d) — a ragged edge", spec.style, x, y, nx, ny)
						}
					}
				}
			}
		}
		if inked == 0 {
			t.Fatalf("style %d: the glyph is blank", spec.style)
		}
		if spec.hotX < 0 || spec.hotX > 31 || spec.hotY < 0 || spec.hotY > 31 {
			t.Fatalf("style %d: hotspot (%d,%d) is outside the 32x32 bitmap", spec.style, spec.hotX, spec.hotY)
		}
		if spec.art[spec.hotY][spec.hotX] == '.' {
			t.Errorf("style %d: hotspot (%d,%d) sits on a transparent cell", spec.style, spec.hotX, spec.hotY)
		}
	}
}

// TestCursorBuiltPlanesEncoding round-trips the monochrome plane
// encoder: every cell maps to the CreateCursor bits it must (black
// AND=0/XOR=0, white AND=0/XOR=1, transparent AND=1/XOR=0, MSB-first
// leftmost pixel, 4 bytes per 32-cell row).
func TestCursorBuiltPlanesEncoding(t *testing.T) {
	for _, spec := range cursorGlyphs {
		and, xor := cursorPlanes(spec.art)
		if len(and) != 128 || len(xor) != 128 {
			t.Fatalf("style %d: planes are %d/%d bytes, want 128/128", spec.style, len(and), len(xor))
		}
		for y, row := range spec.art {
			for x := 0; x < 32; x++ {
				bit := byte(0x80) >> (x % 8)
				wantAnd, wantXor := byte(0), byte(0)
				switch row[x] {
				case '.':
					wantAnd = bit
				case 'o':
					wantXor = bit
				}
				gotAnd, gotXor := and[y*4+x/8]&bit != 0, xor[y*4+x/8]&bit != 0
				if gotAnd != (wantAnd != 0) || gotXor != (wantXor != 0) {
					t.Fatalf("style %d: cell (%d,%d) %q encodes AND=%v XOR=%v, want AND=%v XOR=%v",
						spec.style, x, y, row[x], gotAnd, gotXor, wantAnd != 0, wantXor != 0)
				}
			}
		}
	}
}
