package conformance

import (
	"encoding/json"
	"fmt"
)

// FixtureKindPathsFilters identifies the paths-filters fixture kind
// ("paths-filters-v1", ticket16). It must match the fixture kind the
// reference harness dispatches in reference/harness/src/main.rs.
const FixtureKindPathsFilters = "paths-filters-v1"

// PathFilterInputs carries the per-case inputs of the
// paths-filters-v1 fixture kind. The JSON field names match the Rust
// reference struct fields exactly.
type PathFilterInputs struct {
	// Cases are executed in order; a later case may replay a range of
	// an earlier case's scene (the source scenes stay pinned for the
	// run).
	Cases []PathFilterCase `json:"cases"`
}

// PathFilterCase is one paths-filters case: a labeled style tree laid
// out under the case's available space (node bounds for boxes, sprites,
// surfaces and filter groups), then a paint script executed against a
// fresh scene through the scene kernel, mirroring the pinned
// Window/PathBuilder paint methods' record construction.
type PathFilterCase struct {
	// Label identifies the case; recorded in case-begin/case-end events
	// and names the scene for replay references.
	Label string `json:"label"`
	// AvailableSpace is the definite available space for the root
	// layout, in logical pixels.
	AvailableSpace SizeInput `json:"available_space"`
	// StyleTree is the labeled style tree providing node bounds.
	StyleTree *StyleNode `json:"style_tree"`
	// PaintScript is the ordered semantic paint script.
	PaintScript []PathFilterOp `json:"paint_script"`
}

// TileIn is one atlas tile reference of a sprite op (the pinned
// AtlasTile).
type TileIn struct {
	TextureIndex uint32 `json:"texture_index"`
	// TextureKind: 0 monochrome, 1 polychrome, 2 subpixel.
	TextureKind uint32 `json:"texture_kind"`
	TileID      uint32 `json:"tile_id"`
	Padding     uint32 `json:"padding"`
	// Bounds is the tile bounds inside the atlas texture, device pixels.
	Bounds RectIn `json:"bounds"`
}

// Path command wire tags (the kebab-case `kind` values of the Rust
// PathCommandIn enum).
const (
	PathCmdMoveTo    = "move-to"
	PathCmdLineTo    = "line-to"
	PathCmdCurveTo   = "curve-to"
	PathCmdCubicTo   = "cubic-to"
	PathCmdArcTo     = "arc-to"
	PathCmdPolygon   = "polygon"
	PathCmdClose     = "close"
	PathCmdStyle     = "style"
	PathCmdDash      = "dash"
	PathCmdTranslate = "translate"
	PathCmdScale     = "scale"
	PathCmdRotate    = "rotate"
)

// PathCommandIn is one path builder command: a kind tag plus the union
// of the variants' fields. On the wire it is an internally tagged
// object matching the Rust PathCommandIn enum.
type PathCommandIn struct {
	Kind string `json:"kind"`
	// Geometry coordinates, logical pixels.
	X     *float64 `json:"x,omitempty"`
	Y     *float64 `json:"y,omitempty"`
	ToX   *float64 `json:"to_x,omitempty"`
	ToY   *float64 `json:"to_y,omitempty"`
	CtrlX *float64 `json:"ctrl_x,omitempty"`
	CtrlY *float64 `json:"ctrl_y,omitempty"`
	AX    *float64 `json:"a_x,omitempty"`
	AY    *float64 `json:"a_y,omitempty"`
	BX    *float64 `json:"b_x,omitempty"`
	BY    *float64 `json:"b_y,omitempty"`
	// Arc parameters.
	RadiusX   *float64 `json:"radius_x,omitempty"`
	RadiusY   *float64 `json:"radius_y,omitempty"`
	XRotation *float64 `json:"x_rotation,omitempty"`
	LargeArc  *bool    `json:"large_arc,omitempty"`
	Sweep     *bool    `json:"sweep,omitempty"`
	// Polygon parameters.
	Points [][2]float64 `json:"points,omitempty"`
	Closed *bool        `json:"closed,omitempty"`
	// Style parameters ("fill" or "stroke").
	Style               string   `json:"style,omitempty"`
	Tolerance           *float64 `json:"tolerance,omitempty"`
	FillRule            string   `json:"fill_rule,omitempty"`
	SweepOrientation    string   `json:"sweep_orientation,omitempty"`
	HandleIntersections *bool    `json:"handle_intersections,omitempty"`
	Width               *float64 `json:"width,omitempty"`
	StartCap            string   `json:"start_cap,omitempty"`
	EndCap              string   `json:"end_cap,omitempty"`
	LineJoin            string   `json:"line_join,omitempty"`
	MiterLimit          *float64 `json:"miter_limit,omitempty"`
	// Dash lengths, logical pixels.
	Lengths []float64 `json:"lengths,omitempty"`
	// Transform parameters.
	Factor  *float64 `json:"factor,omitempty"`
	Degrees *float64 `json:"degrees,omitempty"`
}

// pathCommandRequiredFields lists the fields each Rust PathCommandIn
// variant requires on the wire.
var pathCommandRequiredFields = map[string][]string{
	PathCmdMoveTo:    {"x", "y"},
	PathCmdLineTo:    {"x", "y"},
	PathCmdCurveTo:   {"to_x", "to_y", "ctrl_x", "ctrl_y"},
	PathCmdCubicTo:   {"to_x", "to_y", "a_x", "a_y", "b_x", "b_y"},
	PathCmdArcTo:     {"radius_x", "radius_y", "x_rotation", "large_arc", "sweep", "x", "y"},
	PathCmdPolygon:   {"points", "closed"},
	PathCmdClose:     nil,
	PathCmdStyle:     {"style"},
	PathCmdDash:      {"lengths"},
	PathCmdTranslate: {"x", "y"},
	PathCmdScale:     {"factor"},
	PathCmdRotate:    {"degrees"},
}

// UnmarshalJSON decodes one path command, rejecting unknown kind tags
// and missing required fields the way the Rust enum deserializer would.
func (cmd *PathCommandIn) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("path command: %w", err)
	}
	tagRaw, ok := raw["kind"]
	if !ok {
		return fmt.Errorf("path command: missing \"kind\" tag")
	}
	var tag string
	if err := json.Unmarshal(tagRaw, &tag); err != nil {
		return fmt.Errorf("path command: \"kind\" tag is not a string: %w", err)
	}
	required, known := pathCommandRequiredFields[tag]
	if !known {
		return fmt.Errorf("path command: unknown kind tag %q", tag)
	}
	for _, field := range required {
		if _, ok := raw[field]; !ok {
			return fmt.Errorf("path command %s: missing required field %q", tag, field)
		}
	}
	type plain PathCommandIn // avoid recursion on the custom unmarshaler
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("path command %s: %w", tag, err)
	}
	*cmd = PathCommandIn(p)
	return nil
}

// Paint op wire tags of the paths-filters script (the kebab-case
// variant names of the Rust PathFilterOp enum).
const (
	OpPaintBox2             = "paint-box"
	OpPaintPath             = "paint-path"
	OpPaintMonochromeSprite = "paint-monochrome-sprite"
	OpPaintSubpixelSprite   = "paint-subpixel-sprite"
	OpPaintPolychromeSprite = "paint-polychrome-sprite"
	OpPaintSurface          = "paint-surface"
	OpPaintBackdrop2        = "paint-backdrop"
	OpBeginFilterGroup2     = "begin-filter-group"
	OpEndFilterGroup2       = "end-filter-group"
	OpRaiseFloor2           = "raise-floor"
	OpReplay2               = "replay"
)

// pathFilterOpRequiredFields lists the fields each Rust PathFilterOp
// variant requires on the wire.
var pathFilterOpRequiredFields = map[string][]string{
	OpPaintBox2:             {"label"},
	OpPaintPath:             {"commands", "color"},
	OpPaintMonochromeSprite: {"label", "color", "tile"},
	OpPaintSubpixelSprite:   {"label", "color", "tile"},
	OpPaintPolychromeSprite: {"label", "tile"},
	OpPaintSurface:          {"label"},
	OpPaintBackdrop2:        {"label", "blur_radius"},
	OpBeginFilterGroup2:     {"blur_radius"}, // label or bounds, both optional
	OpEndFilterGroup2:       nil,
	OpRaiseFloor2:           nil,
	OpReplay2:               {"source", "start", "end"},
}

// PathFilterOp is one paths-filters paint-script operation: a flat
// struct covering the union of every variant's fields, validated during
// unmarshaling so malformed fixtures fail loudly.
type PathFilterOp struct {
	Op string `json:"op"`
	// Label references the style-tree node providing the bounds.
	Label string `json:"label,omitempty"`
	// Bounds is an explicit group bounds rectangle (filter groups).
	Bounds *RectIn `json:"bounds,omitempty"`
	// Colors are 8-hex-digit RRGGBBAA strings.
	Background string `json:"background,omitempty"`
	Color      string `json:"color,omitempty"`
	// Corner radii/smoothing.
	CornerRadii     *CornersIn `json:"corner_radii,omitempty"`
	CornerRadius    *float64   `json:"corner_radius,omitempty"`
	CornerSmoothing *float64   `json:"corner_smoothing,omitempty"`
	// Path builder script (paint-path).
	Commands []PathCommandIn `json:"commands,omitempty"`
	// Sprite parameters.
	Tile    *TileIn  `json:"tile,omitempty"`
	Opacity *float64 `json:"opacity,omitempty"`
	// Filter parameters.
	BlurRadius *float64 `json:"blur_radius,omitempty"`
	// Explicit content mask; the default is the case mask.
	Mask *RectIn `json:"mask,omitempty"`
	// Replay parameters.
	Source string `json:"source,omitempty"`
	Start  uint32 `json:"start,omitempty"`
	End    uint32 `json:"end,omitempty"`
}

// UnmarshalJSON decodes one paths-filters paint op, rejecting unknown
// op tags and missing required fields the way the Rust enum
// deserializer would.
func (op *PathFilterOp) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("paths-filters op: %w", err)
	}
	tagRaw, ok := raw["op"]
	if !ok {
		return fmt.Errorf("paths-filters op: missing \"op\" tag")
	}
	var tag string
	if err := json.Unmarshal(tagRaw, &tag); err != nil {
		return fmt.Errorf("paths-filters op: \"op\" tag is not a string: %w", err)
	}
	required, known := pathFilterOpRequiredFields[tag]
	if !known {
		return fmt.Errorf("paths-filters op: unknown op tag %q", tag)
	}
	for _, field := range required {
		if _, ok := raw[field]; !ok {
			return fmt.Errorf("paths-filters op %s: missing required field %q", tag, field)
		}
	}
	type plain PathFilterOp // avoid recursion on the custom unmarshaler
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("paths-filters op %s: %w", tag, err)
	}
	*op = PathFilterOp(p)
	return nil
}

// IsPathsFiltersKind reports whether the envelope is a paths-filters-v1
// fixture.
func (env *Envelope) IsPathsFiltersKind() bool {
	return env.FixtureKind == FixtureKindPathsFilters
}
