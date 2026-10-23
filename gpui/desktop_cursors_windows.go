//go:build windows

package gpui

// Browser-core ticket 03 (research browser-parity 03 §12, zterm
// #23): the four cursor styles the reference's Windows loader maps to
// the arrow (gpui_windows/src/util.rs:99-115, the catch-all at :114) --
// grab (CursorOpenHand), grabbing (CursorClosedHand), copy
// (CursorDragCopy) and context-menu (CursorContextualMenu) -- load
// real, distinct cursors built at runtime with CreateCursor.
//
// Windows ships no system cursors for these glyphs (that is why the
// reference falls back), and Chromium draws its own .cur resources for
// them (blink platform/cursors.cc:70, 90, 281, 286 -- resource cursors
// not in the sparse checkout). The research recommends building them
// with CreateIconIndirect (browser-parity README Q22); the port uses
// the monochrome sibling CreateCursor, which needs no GDI bitmaps:
// each glyph is a 32x32 grid of cells -- '#' black, 'o' white,
// '.' transparent -- encoded into the AND and XOR planes CreateCursor
// takes (see cursorPlanes).
//
// Deliberate divergence toward the browser under D0: the shapes follow
// the CSS cursor meanings (an open hand, a closed fist, two sheets
// with a plus, an arrow with a menu panel). Monochrome (the classic
// system-cursor look, and CreateCursor's limit); 32x32 at 96dpi -- the
// built cursors do not scale with the window's DPI the way
// LR_DEFAULTSIZE system cursors do, a documented limitation. The
// handles are cached per style for the process lifetime
// (loadCursorHandle); created cursors need DestroyCursor only when
// freed, and the cache never frees.

import (
	"sync"
	"unsafe"
)

// procCreateCursor is user32 CreateCursor (the runtime-built cursors).
var procCreateCursor = modUser32.NewProc("CreateCursor")

// cursorGlyph is one built cursor's frozen bitmap: the art rows, one
// 32-cell string per row, and the hotspot inside the bitmap.
type cursorGlyph struct {
	style CursorStyle
	hotX  int
	hotY  int
	art   []string
}

// cursorGlyphs is the built-cursor table: one entry per style that
// loads a runtime-built cursor. desktop_cursors_windows_test.go pins
// the table's shape (32 rows of 32 cells over '#', 'o', '.'; hotspots
// inside the bitmap; white cells enclosed -- no ragged edges).
var cursorGlyphs = []cursorGlyph{
	{
		style: CursorOpenHand,
		hotX:  15,
		hotY:  19,
		art: []string{
			"................................",
			"................................",
			"................................",
			"............###.................",
			"...........#ooo#..###...........",
			"...........#ooo#.#ooo#..........",
			"......###..#ooo#.#ooo#..........",
			".....#ooo#.#ooo#.#ooo#..###.....",
			".....#ooo#.#ooo#.#ooo#.#ooo#....",
			".....#ooo#.#ooo#.#ooo#.#ooo#....",
			".....#ooo#.#ooo#.#ooo#.#ooo#....",
			".....#ooo#.#ooo#.#ooo#.#ooo#....",
			".....#ooo#.#ooo#.#ooo#.#ooo#....",
			".....#ooo#.#ooo#.#ooo#.#ooo#....",
			".....#ooo#.#ooo#.#ooo#.#ooo#....",
			"....########################....",
			"....#oooooooooooooooooooooo#....",
			"....#oooooooooooooooooooooo###..",
			"....#oooooooooooooooooooooo#oo#.",
			"....#oooooooooooooooooooooo#oo#.",
			"....#oooooooooooooooooooooo#oo#.",
			"....#oooooooooooooooooooooo#oo#.",
			"....#oooooooooooooooooooooo#oo#.",
			"....#oooooooooooooooooooooo###..",
			"....#oooooooooooooooooooooo#....",
			".....######################.....",
			"................................",
			"................................",
			"................................",
			"................................",
			"................................",
			"................................",
		},
	},
	{
		style: CursorClosedHand,
		hotX:  15,
		hotY:  15,
		art: []string{
			"................................",
			"................................",
			"................................",
			"................................",
			"................................",
			"............###...###...........",
			"......###..#ooo#.#ooo#..###.....",
			".....#ooo#.#ooo#.#ooo#.#ooo#....",
			".....#ooo#.#ooo#.#ooo#.#ooo#....",
			".....#ooo#.#ooo#.#ooo#.#ooo#....",
			".....######################.....",
			"....#oooooooooooooooooooooo#....",
			"....#oooooooooooooooooooooo#....",
			"....#############oooooooooo#....",
			"....##ooooooooooo#ooooooooo#....",
			"....##ooooooooooo#ooooooooo#....",
			"....##ooooooooooo#ooooooooo#....",
			"....##ooooooooooo#ooooooooo#....",
			"....#############oooooooooo#....",
			"....#oooooooooooooooooooooo#....",
			"....#oooooooooooooooooooooo#....",
			"....#oooooooooooooooooooooo#....",
			"....#oooooooooooooooooooooo#....",
			"....#oooooooooooooooooooooo#....",
			"....#oooooooooooooooooooooo#....",
			".....######################.....",
			"................................",
			"................................",
			"................................",
			"................................",
			"................................",
			"................................",
		},
	},
	{
		style: CursorDragCopy,
		hotX:  1,
		hotY:  4,
		art: []string{
			"................................",
			"................................",
			"................................",
			"...........#################....",
			".#########.#ooooooooooooooo#....",
			".###oo####.#ooooooooooooooo#....",
			".###oo####.#ooooooooooooooo#....",
			".#ooooooo#.#ooooooooooooooo#....",
			".#ooooooo#.#ooooooooooooooo#....",
			".###oo####.#ooooooooooooooo#....",
			".###oo###############oooooo#....",
			".#########oooooooooo#oooooo#....",
			".#########oooooooooo#oooooo#....",
			"....#ooooooooooooooo#oooooo#....",
			"....#ooooooooooooooo#oooooo#....",
			"....#ooooooooooooooo#oooooo#....",
			"....#ooooooooooooooo#oooooo#....",
			"....#ooooooooooooooo#oooooo#....",
			"....#ooooooooooooooo#oooooo#....",
			"....#ooooooooooooooo########....",
			"....#ooooooooooooooo#...........",
			"....#ooooooooooooooo#...........",
			"....#ooooooooooooooo#...........",
			"....#ooooooooooooooo#...........",
			"....#ooooooooooooooo#...........",
			"....#ooooooooooooooo#...........",
			"....#ooooooooooooooo#...........",
			"....#################...........",
			"................................",
			"................................",
			"................................",
			"................................",
		},
	},
	{
		style: CursorContextualMenu,
		hotX:  0,
		hotY:  1,
		art: []string{
			"................................",
			"#...............................",
			"##..............................",
			"#o#.............................",
			"#oo#............................",
			"#ooo#...........................",
			"#oooo#..........................",
			"#ooooo#.........................",
			"#oooooo#........................",
			"#ooooooo#.......................",
			"#oooooooo#......................",
			"#ooooooooo#.....................",
			"#oooooooooo#....................",
			"#oooo#######...################.",
			"#####oo#.......################.",
			"....#oo#.......################.",
			"....#oo#.......#oooooooooooooo#.",
			"....#oo#.......#oooooooooooooo#.",
			"....#oo#.......#o########ooooo#.",
			"....####.......#o########ooooo#.",
			"...............#oooooooooooooo#.",
			"...............#oooooooooooooo#.",
			"...............#o########ooooo#.",
			"...............#o########ooooo#.",
			"...............#oooooooooooooo#.",
			"...............#oooooooooooooo#.",
			"...............#oooooooooooooo#.",
			"...............################.",
			"................................",
			"................................",
			"................................",
			"................................",
		},
	},
}

// builtCursorSpec returns the style's glyph table entry.
func builtCursorSpec(style CursorStyle) *cursorGlyph {
	for i := range cursorGlyphs {
		if cursorGlyphs[i].style == style {
			return &cursorGlyphs[i]
		}
	}
	return nil
}

// builtCursors caches the created HCURSORs per style for the process
// lifetime (see the file comment).
var (
	builtCursorsMu sync.Mutex
	builtCursors   = map[CursorStyle]uintptr{}
)

// builtCursorHandle returns the style's built cursor, creating it
// (once, cached) with CreateCursor. A style without a glyph table
// entry reports 0; a failed CreateCursor also reports 0 and is retried
// on the next call.
func builtCursorHandle(style CursorStyle) uintptr {
	spec := builtCursorSpec(style)
	if spec == nil {
		return 0
	}
	builtCursorsMu.Lock()
	defer builtCursorsMu.Unlock()
	if h, ok := builtCursors[style]; ok {
		return h
	}
	and, xor := cursorPlanes(spec.art)
	if and == nil {
		return 0
	}
	inst, _, _ := procGetModuleHandleW.Call(0)
	h, _, _ := procCreateCursor.Call(
		inst, uintptr(spec.hotX), uintptr(spec.hotY), 32, 32,
		uintptr(unsafe.Pointer(&and[0])), uintptr(unsafe.Pointer(&xor[0])))
	if h == 0 {
		return 0
	}
	builtCursors[style] = h
	return h
}

// cursorPlanes encodes 32 art rows into CreateCursor's monochrome
// planes: each 32-cell row packs into 4 bytes, the leftmost cell in
// the most significant bit (the Windows monochrome bitmap layout).
// Per cell: black '#' is AND 0 / XOR 0, white 'o' is AND 0 / XOR 1
// (the screen inverted, which paints white), transparent '.' is
// AND 1 / XOR 0 (the screen kept). Malformed art reports nil planes.
func cursorPlanes(art []string) (and, xor []byte) {
	if len(art) != 32 {
		return nil, nil
	}
	and = make([]byte, 128)
	xor = make([]byte, 128)
	for y, row := range art {
		if len(row) != 32 {
			return nil, nil
		}
		for x := 0; x < 32; x++ {
			bit := byte(0x80) >> (x % 8)
			switch row[x] {
			case 'o':
				xor[y*4+x/8] |= bit
			case '#':
				// AND 0, XOR 0: black.
			case '.':
				and[y*4+x/8] |= bit
			default:
				return nil, nil
			}
		}
	}
	return and, xor
}
