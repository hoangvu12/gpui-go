package gpui

import (
	"fmt"
	"strings"
)

// This file is the CSS grid track-list and named-area surface
// (browser-core ticket 42): the full `grid-template-columns`/
// `grid-template-rows` track lists beyond the pinned `repeat(N,
// minmax(min, 1fr))` form (a fixed `240px` track next to a `1fr` one,
// auto rows), `grid-template-areas` and the item-side `grid-area:
// <name>` placement.
//
// CSS references (css-grid-1 at the spec level; the numerics below are
// the box model the browser-parity fixtures record against Chrome):
//
//   - §7.2.3: a bare track size is the minmax pair with itself on both
//     sides — except an `<flex>`, which is minmax(auto, <flex>).
//   - §8.3 (grid-template-areas): a template of quoted row strings;
//     each cell is a name or the null token `.`; every name must form
//     one rectangle and all rows must have the same column count.
//   - §8.4 (grid-area): the shorthand `grid-area: <name>` sets all
//     four placements to the named line, which resolves to the area's
//     edges through the implicit `<name>-start`/`<name>-end` line
//     names the areas define.

// TrackSizingKind is one side of a track's minmax pair.
type TrackSizingKind uint8

const (
	// TrackLength is a definite track length in pixels.
	TrackLength TrackSizingKind = iota
	// TrackMinContent is the CSS min-content keyword.
	TrackMinContent
	// TrackMaxContent is the CSS max-content keyword.
	TrackMaxContent
	// TrackAuto is the CSS auto keyword.
	TrackAuto
	// TrackFr is a flexible fraction (the maximum side only).
	TrackFr
)

// TrackSizing is one side of a track's minmax pair: a definite length,
// a content keyword, auto, or (as a maximum) an fr factor.
type TrackSizing struct {
	// Kind selects the keyword.
	Kind TrackSizingKind
	// Length is the definite length for TrackLength, or the fr factor
	// for TrackFr; ignored otherwise.
	Length float32
}

// PxTrackSizing returns a definite length sizing.
func PxTrackSizing(px float32) TrackSizing {
	return TrackSizing{Kind: TrackLength, Length: px}
}

// FrTrackSizing returns a flexible fraction sizing.
func FrTrackSizing(factor float32) TrackSizing {
	return TrackSizing{Kind: TrackFr, Length: factor}
}

// MinContentTrackSizing returns the min-content sizing.
func MinContentTrackSizing() TrackSizing { return TrackSizing{Kind: TrackMinContent} }

// MaxContentTrackSizing returns the max-content sizing.
func MaxContentTrackSizing() TrackSizing { return TrackSizing{Kind: TrackMaxContent} }

// AutoTrackSizing returns the auto sizing.
func AutoTrackSizing() TrackSizing { return TrackSizing{Kind: TrackAuto} }

// GridTrack is one track of a CSS track list: a minmax pair. The
// constructors below spell the bare CSS forms (§7.2.3: a bare size
// repeats itself on both sides, a bare fr takes auto as its minimum).
type GridTrack struct {
	// Min is the minimum sizing function.
	Min TrackSizing
	// Max is the maximum sizing function.
	Max TrackSizing
}

// PxTrack returns a fixed track (grid-template-columns: 240px —
// minmax(240px, 240px)).
func PxTrack(px float32) GridTrack {
	return GridTrack{Min: PxTrackSizing(px), Max: PxTrackSizing(px)}
}

// FrTrack returns a flexible track (1fr — minmax(auto, 1fr)).
func FrTrack(factor float32) GridTrack {
	return GridTrack{Min: AutoTrackSizing(), Max: FrTrackSizing(factor)}
}

// AutoTrack returns an auto track (minmax(auto, auto)).
func AutoTrack() GridTrack {
	return GridTrack{Min: AutoTrackSizing(), Max: AutoTrackSizing()}
}

// MinContentTrack returns a min-content track.
func MinContentTrack() GridTrack {
	return GridTrack{Min: MinContentTrackSizing(), Max: MinContentTrackSizing()}
}

// MaxContentTrack returns a max-content track.
func MaxContentTrack() GridTrack {
	return GridTrack{Min: MaxContentTrackSizing(), Max: MaxContentTrackSizing()}
}

// MinMaxTrack returns an explicit minmax pair.
func MinMaxTrack(min, max TrackSizing) GridTrack {
	return GridTrack{Min: min, Max: max}
}

// gridAreaRect is one named area's rectangle in 1-based grid line
// numbers (the convention the native template carries).
type gridAreaRect struct {
	rowStart    int
	rowEnd      int
	columnStart int
	columnEnd   int
}

// GridAreaTemplate is a parsed `grid-template-areas` value: the rows of
// cell names as authored plus the rectangle each name occupies.
type GridAreaTemplate struct {
	// Rows holds the template's rows, each the row's cell names in
	// column order (the null cell is "."), as authored.
	Rows [][]string
	// rects maps each area name to its rectangle (1-based grid lines).
	rects map[string]gridAreaRect
}

// AreaNames returns the template's area names in first-appearance
// order (row-major over the authored rows).
func (t *GridAreaTemplate) AreaNames() []string {
	seen := make(map[string]bool, len(t.rects))
	names := make([]string, 0, len(t.rects))
	for _, row := range t.Rows {
		for _, cell := range row {
			if cell == "." || seen[cell] {
				continue
			}
			seen[cell] = true
			names = append(names, cell)
		}
	}
	return names
}

// ParseGridTemplateAreas parses a CSS `grid-template-areas` value: a
// sequence of quoted row strings, each holding whitespace-separated
// cell names (the null cell is "."), e.g.
// `"head head" "side main"`. All rows must have the same column count
// (css-grid-1 §8.3 requires a rectangular template) and each name must
// occupy exactly one rectangle.
func ParseGridTemplateAreas(template string) (*GridAreaTemplate, error) {
	rows, err := splitAreaTemplateRows(template)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("gpui: grid-template-areas: the template is empty")
	}
	width := len(rows[0])
	for _, row := range rows {
		if len(row) != width {
			return nil, fmt.Errorf("gpui: grid-template-areas: every row must have the same column count (got %d and %d)", width, len(row))
		}
	}
	bounds := make(map[string][4]int) // name -> minRow, maxRow, minCol, maxCol
	cells := make(map[string]struct{})
	for rowIndex, row := range rows {
		for colIndex, cell := range row {
			if cell == "." {
				continue
			}
			if !validGridAreaName(cell) {
				return nil, fmt.Errorf("gpui: grid-template-areas: %q is not a valid area name", cell)
			}
			b, ok := bounds[cell]
			if !ok {
				b = [4]int{rowIndex, rowIndex, colIndex, colIndex}
				bounds[cell] = b
				cells[cell] = struct{}{}
				continue
			}
			b[0] = min(b[0], rowIndex)
			b[1] = max(b[1], rowIndex)
			b[2] = min(b[2], colIndex)
			b[3] = max(b[3], colIndex)
			bounds[cell] = b
		}
	}
	// Every name's cells must form one rectangle (§8.3: "if a named
	// grid area spans multiple grid cells, those cells must form a
	// rectangular area").
	rects := make(map[string]gridAreaRect, len(bounds))
	for name, b := range bounds {
		for row := b[0]; row <= b[1]; row++ {
			for col := b[2]; col <= b[3]; col++ {
				if rows[row][col] != name {
					return nil, fmt.Errorf("gpui: grid-template-areas: the area %q does not form a rectangle", name)
				}
			}
		}
		rects[name] = gridAreaRect{
			rowStart:    b[0] + 1,
			rowEnd:      b[1] + 2,
			columnStart: b[2] + 1,
			columnEnd:   b[3] + 2,
		}
	}
	return &GridAreaTemplate{Rows: rows, rects: rects}, nil
}

// joinAreaRows joins a variadic row list into one template string: one
// argument is the whole CSS value (quoted rows), several arguments are
// the rows themselves (quoted here).
func joinAreaRows(rows []string) string {
	if len(rows) == 1 {
		return rows[0]
	}
	quoted := make([]string, len(rows))
	for i, row := range rows {
		quoted[i] = "\"" + row + "\""
	}
	return strings.Join(quoted, " ")
}

// splitAreaTemplateRows splits a template string into its rows of cell
// names: quoted segments separated by whitespace. Quotes may be single
// or double.
func splitAreaTemplateRows(template string) ([][]string, error) {
	var rows [][]string
	rest := strings.TrimSpace(template)
	if rest == "" {
		return nil, nil
	}
	for rest != "" {
		if rest[0] != '"' && rest[0] != '\'' {
			return nil, fmt.Errorf("gpui: grid-template-areas: each row must be a quoted string, found %q", rest[0])
		}
		quote := rest[0]
		end := strings.IndexByte(rest[1:], quote)
		if end < 0 {
			return nil, fmt.Errorf("gpui: grid-template-areas: an unterminated row string")
		}
		row := rest[1 : 1+end]
		fields := strings.Fields(row)
		if len(fields) == 0 {
			return nil, fmt.Errorf("gpui: grid-template-areas: a row string is empty")
		}
		rows = append(rows, fields)
		rest = strings.TrimSpace(rest[2+end:])
	}
	return rows, nil
}

// validGridAreaName reports whether s is a CSS <custom-ident> usable as
// an area name: non-empty, not the null token, not a digit start, not
// a CSS-wide keyword (the css-values-4 §custom-ident rule).
func validGridAreaName(s string) bool {
	if s == "" || s == "." || s == "none" || s == "inherit" || s == "initial" || s == "unset" || s == "revert" {
		return false
	}
	first := s[0]
	if first >= '0' && first <= '9' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x21 || c == '"' || c == '\'' || c == '(' || c == ')' || c == '[' || c == ']' || c == '{' || c == '}' {
			return false
		}
	}
	return true
}

// parseTrackListText parses one CSS track list body (already
// underscore-unescaped by the Tailwind layer): whitespace- or
// comma-free track functions separated by whitespace — `240px 1fr`,
// `auto auto`, `minmax(10px, 1fr) min-content`. Commas separate the
// track list from grid-auto-flow's dense keyword in real CSS; here the
// value holds the track list alone, commas rejected.
func parseTrackListText(value string) ([]GridTrack, error) {
	fields, err := splitTrackTokens(value)
	if err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("gpui: the grid track list is empty")
	}
	tracks := make([]GridTrack, 0, len(fields))
	for _, field := range fields {
		track, err := parseTrackToken(field)
		if err != nil {
			return nil, err
		}
		tracks = append(tracks, track)
	}
	return tracks, nil
}

// splitTrackTokens splits a track list into its function tokens,
// keeping minmax(...) together.
func splitTrackTokens(value string) ([]string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil
	}
	var tokens []string
	depth := 0
	current := strings.Builder{}
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	for i := 0; i < len(trimmed); i++ {
		c := trimmed[i]
		switch c {
		case '(':
			depth++
			current.WriteByte(c)
		case ')':
			depth--
			if depth < 0 {
				return nil, fmt.Errorf("gpui: the grid track list has an unmatched ')'")
			}
			current.WriteByte(c)
		case ' ', '\t', '\n', '\r':
			if depth == 0 {
				flush()
			} else {
				current.WriteByte(c)
			}
		case ',':
			if depth == 0 {
				return nil, fmt.Errorf("gpui: the grid track list does not take commas")
			}
			current.WriteByte(c)
		default:
			current.WriteByte(c)
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("gpui: the grid track list has an unclosed minmax(")
	}
	flush()
	return tokens, nil
}

// parseTrackToken parses one track function: a bare size or
// minmax(min, max).
func parseTrackToken(token string) (GridTrack, error) {
	if strings.HasPrefix(token, "minmax(") && strings.HasSuffix(token, ")") {
		inner := token[len("minmax(") : len(token)-1]
		comma := strings.IndexByte(inner, ',')
		if comma < 0 {
			return GridTrack{}, fmt.Errorf("gpui: minmax(%s) needs a minimum and a maximum", inner)
		}
		minSizing, err := parseTrackSide(strings.TrimSpace(inner[:comma]), true)
		if err != nil {
			return GridTrack{}, err
		}
		maxSizing, err := parseTrackSide(strings.TrimSpace(inner[comma+1:]), false)
		if err != nil {
			return GridTrack{}, err
		}
		return GridTrack{Min: minSizing, Max: maxSizing}, nil
	}
	// A bare fr takes auto as its minimum (§7.2.3); every other bare
	// size repeats itself on both sides.
	if strings.HasSuffix(token, "fr") {
		maxSizing, err := parseTrackSide(token, false)
		if err != nil {
			return GridTrack{}, err
		}
		return GridTrack{Min: AutoTrackSizing(), Max: maxSizing}, nil
	}
	side, err := parseTrackSide(token, true)
	if err != nil {
		return GridTrack{}, err
	}
	return GridTrack{Min: side, Max: side}, nil
}

// parseTrackSide parses one minmax side: a length (px suffix or bare
// number), a keyword, or fr (the maximum side only).
func parseTrackSide(token string, minSide bool) (TrackSizing, error) {
	switch token {
	case "auto":
		return AutoTrackSizing(), nil
	case "min-content":
		return MinContentTrackSizing(), nil
	case "max-content":
		return MaxContentTrackSizing(), nil
	}
	if strings.HasSuffix(token, "fr") {
		if minSide {
			return TrackSizing{}, fmt.Errorf("gpui: %q: an fr is a track maximum, not a minimum", token)
		}
		factor, err := parseTrackNumber(strings.TrimSuffix(token, "fr"))
		if err != nil {
			return TrackSizing{}, fmt.Errorf("gpui: %q: %w", token, err)
		}
		if factor <= 0 {
			return TrackSizing{}, fmt.Errorf("gpui: %q: the fr factor must be positive", token)
		}
		return FrTrackSizing(factor), nil
	}
	// A track length is a number with a px unit (css-grid-1: a bare
	// number is not a track size).
	if !strings.HasSuffix(token, "px") {
		return TrackSizing{}, fmt.Errorf("gpui: %q: expected a length like 12px, a keyword or an fr", token)
	}
	number, err := parseTrackNumber(strings.TrimSuffix(token, "px"))
	if err != nil {
		return TrackSizing{}, fmt.Errorf("gpui: %q: %w", token, err)
	}
	if number < 0 {
		return TrackSizing{}, fmt.Errorf("gpui: %q: a track length cannot be negative", token)
	}
	return PxTrackSizing(number), nil
}

// parseTrackNumber parses a plain non-negative float.
func parseTrackNumber(value string) (float32, error) {
	if value == "" {
		return 0, fmt.Errorf("expected a number")
	}
	var number float64
	dot := false
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= '0' && c <= '9':
		case c == '.' && !dot:
			dot = true
		default:
			return 0, fmt.Errorf("expected a number")
		}
	}
	if _, err := fmt.Sscanf(value, "%g", &number); err != nil {
		return 0, fmt.Errorf("expected a number")
	}
	return float32(number), nil
}
