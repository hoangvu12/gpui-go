package conformance

import (
	"encoding/json"
	"fmt"
)

// FixtureKindScenePainting identifies the scene-painting fixture kind
// ("scene-painting-v1"). It must match the fixture kind the reference
// harness dispatches in reference/harness/src/main.rs.
const FixtureKindScenePainting = "scene-painting-v1"

// ScenePaintingInputs carries the per-case inputs of the
// scene-painting-v1 fixture kind. The JSON field names match the Rust
// reference struct fields exactly.
type ScenePaintingInputs struct {
	// Cases are executed in order; a later case may replay a range of an
	// earlier case's scene (the source scenes stay pinned for the run).
	Cases []ScenePaintingCase `json:"cases"`
}

// ScenePaintingCase is one scene-painting case: a labeled style tree laid
// out under the case's available space, then a paint script executed
// against a fresh scene through the scene kernel, mirroring the pinned
// Window paint methods' record construction.
type ScenePaintingCase struct {
	// Label identifies the case; it is recorded in the case-begin and
	// case-end trace events and names the scene for later replay
	// references.
	Label string `json:"label"`
	// AvailableSpace is the definite available space for the root layout,
	// in logical pixels.
	AvailableSpace SizeInput `json:"available_space"`
	// StyleTree is the labeled style tree providing node bounds for the
	// paint script, using the extended layout-metrics style key set.
	StyleTree *StyleNode `json:"style_tree"`
	// PaintScript is the ordered semantic paint script.
	PaintScript []PaintOp `json:"paint_script"`
}

// CornersIn is a corner-radii input in logical pixels (top-left,
// top-right, bottom-right, bottom-left).
type CornersIn struct {
	TopLeft     float64 `json:"top_left"`
	TopRight    float64 `json:"top_right"`
	BottomRight float64 `json:"bottom_right"`
	BottomLeft  float64 `json:"bottom_left"`
}

// EdgesIn is an edge-widths input in logical pixels (top, right, bottom,
// left).
type EdgesIn struct {
	Top    float64 `json:"top"`
	Right  float64 `json:"right"`
	Bottom float64 `json:"bottom"`
	Left   float64 `json:"left"`
}

// RectIn is an explicit bounds/mask rectangle in logical pixels.
type RectIn struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Paint op wire tags. They match the kebab-case variant names of the Rust
// reference's internally tagged PaintOp enum.
const (
	OpPaintBox         = "paint-box"
	OpPaintShadow      = "paint-shadow"
	OpPaintUnderline   = "paint-underline"
	OpBeginLayer       = "begin-layer"
	OpEndLayer         = "end-layer"
	OpRaiseFloor       = "raise-floor"
	OpBeginFilterGroup = "begin-filter-group"
	OpEndFilterGroup   = "end-filter-group"
	OpPaintBackdrop    = "paint-backdrop"
	OpReplay           = "replay"
)

// paintOpRequiredFields lists the fields each Rust PaintOp variant
// requires on the wire (optional fields are excluded).
var paintOpRequiredFields = map[string][]string{
	OpPaintBox:         {"label"},
	OpPaintShadow:      {"label", "color"},
	OpPaintUnderline:   {"label", "thickness", "color"},
	OpBeginLayer:       nil, // label or bounds, both optional
	OpEndLayer:         nil,
	OpRaiseFloor:       nil,
	OpBeginFilterGroup: {"label", "blur_radius"},
	OpEndFilterGroup:   nil,
	OpPaintBackdrop:    {"label", "blur_radius"},
	OpReplay:           {"source", "start", "end"},
}

// PaintOp is one operation of a scene-painting script. On the wire it is
// an internally tagged object, e.g.
//
//	{"op": "paint-box", "label": "b1", "background": "dc2626ff"}
//
// matching the Rust PaintOp enum (#[serde(tag = "op", rename_all =
// "kebab-case")]). The flat struct covers the union of every variant's
// fields; op tag and required fields are validated during unmarshaling
// so malformed fixtures fail loudly, mirroring the serde enum decoding.
type PaintOp struct {
	Op string `json:"op"`
	// Label references the style-tree node providing the bounds.
	Label string `json:"label,omitempty"`
	// Colors are 8-hex-digit RRGGBBAA strings.
	Background  string `json:"background,omitempty"`
	BorderColor string `json:"border_color,omitempty"`
	Color       string `json:"color,omitempty"`
	// BorderStyle is "Solid" (default) or "Dashed".
	BorderStyle string `json:"border_style,omitempty"`
	// CornerRadii override CornerRadius when present.
	CornerRadii  *CornersIn `json:"corner_radii,omitempty"`
	CornerRadius *float64   `json:"corner_radius,omitempty"`
	// BorderWidths override BorderWidth when present.
	BorderWidths    *EdgesIn `json:"border_widths,omitempty"`
	BorderWidth     *float64 `json:"border_width,omitempty"`
	CornerSmoothing *float64 `json:"corner_smoothing,omitempty"`
	// Shadow parameters.
	OffsetX      *float64 `json:"offset_x,omitempty"`
	OffsetY      *float64 `json:"offset_y,omitempty"`
	BlurRadius   *float64 `json:"blur_radius,omitempty"`
	SpreadRadius *float64 `json:"spread_radius,omitempty"`
	Inset        *bool    `json:"inset,omitempty"`
	// Underline parameters.
	Width     *float64 `json:"width,omitempty"`
	Thickness *float64 `json:"thickness,omitempty"`
	Wavy      *bool    `json:"wavy,omitempty"`
	// Layer parameters.
	Bounds  *RectIn  `json:"bounds,omitempty"`
	Opacity *float64 `json:"opacity,omitempty"`
	// Explicit content mask; the default is the case mask.
	Mask *RectIn `json:"mask,omitempty"`
	// Replay parameters.
	Source string `json:"source,omitempty"`
	Start  uint32 `json:"start,omitempty"`
	End    uint32 `json:"end,omitempty"`
}

// UnmarshalJSON decodes one paint op, rejecting unknown op tags and
// missing required fields the way the Rust enum deserializer would.
func (op *PaintOp) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("paint op: %w", err)
	}
	tagRaw, ok := raw["op"]
	if !ok {
		return fmt.Errorf("paint op: missing \"op\" tag")
	}
	var tag string
	if err := json.Unmarshal(tagRaw, &tag); err != nil {
		return fmt.Errorf("paint op: \"op\" tag is not a string: %w", err)
	}
	required, known := paintOpRequiredFields[tag]
	if !known {
		return fmt.Errorf("paint op: unknown op tag %q", tag)
	}
	for _, field := range required {
		if _, ok := raw[field]; !ok {
			return fmt.Errorf("paint op %s: missing required field %q", tag, field)
		}
	}
	type plain PaintOp // avoid recursion on the custom unmarshaler
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("paint op %s: %w", tag, err)
	}
	*op = PaintOp(p)
	return nil
}

// MarshalJSON encodes one paint op in the exact Rust wire shape: the op
// tag plus the fields of that variant (required fields always, optional
// fields only when set).
func (op PaintOp) MarshalJSON() ([]byte, error) {
	m := map[string]any{"op": op.Op}
	optFloat := func(key string, v *float64) {
		if v != nil {
			m[key] = *v
		}
	}
	optRect := func(key string, v *RectIn) {
		if v != nil {
			m[key] = *v
		}
	}
	optCorners := func(key string, v *CornersIn) {
		if v != nil {
			m[key] = *v
		}
	}
	optEdges := func(key string, v *EdgesIn) {
		if v != nil {
			m[key] = *v
		}
	}
	optBool := func(key string, v *bool) {
		if v != nil {
			m[key] = *v
		}
	}
	switch op.Op {
	case OpPaintBox:
		m["label"] = op.Label
		if op.Background != "" {
			m["background"] = op.Background
		}
		if op.BorderColor != "" {
			m["border_color"] = op.BorderColor
		}
		if op.BorderStyle != "" {
			m["border_style"] = op.BorderStyle
		}
		optFloat("corner_radius", op.CornerRadius)
		optCorners("corner_radii", op.CornerRadii)
		optFloat("border_width", op.BorderWidth)
		optEdges("border_widths", op.BorderWidths)
		optFloat("corner_smoothing", op.CornerSmoothing)
		optRect("mask", op.Mask)
	case OpPaintShadow:
		m["label"] = op.Label
		m["color"] = op.Color
		optFloat("offset_x", op.OffsetX)
		optFloat("offset_y", op.OffsetY)
		optFloat("blur_radius", op.BlurRadius)
		optFloat("spread_radius", op.SpreadRadius)
		optBool("inset", op.Inset)
		optFloat("corner_radius", op.CornerRadius)
		optCorners("corner_radii", op.CornerRadii)
		optFloat("corner_smoothing", op.CornerSmoothing)
		optRect("mask", op.Mask)
	case OpPaintUnderline:
		m["label"] = op.Label
		m["thickness"] = derefFloat(op.Thickness)
		m["color"] = op.Color
		optFloat("width", op.Width)
		optBool("wavy", op.Wavy)
		optRect("mask", op.Mask)
	case OpBeginLayer:
		if op.Label != "" {
			m["label"] = op.Label
		}
		optRect("bounds", op.Bounds)
		optFloat("opacity", op.Opacity)
		optRect("mask", op.Mask)
	case OpEndLayer, OpRaiseFloor, OpEndFilterGroup:
		// Tag only.
	case OpBeginFilterGroup, OpPaintBackdrop:
		m["label"] = op.Label
		m["blur_radius"] = derefFloat(op.BlurRadius)
		optFloat("corner_radius", op.CornerRadius)
		optCorners("corner_radii", op.CornerRadii)
		optFloat("corner_smoothing", op.CornerSmoothing)
		optRect("mask", op.Mask)
	case OpReplay:
		m["source"] = op.Source
		m["start"] = op.Start
		m["end"] = op.End
	default:
		return nil, fmt.Errorf("paint op: unknown op tag %q", op.Op)
	}
	return json.Marshal(m)
}

func derefFloat(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// IsScenePaintingKind reports whether the envelope declares the
// scene-painting-v1 fixture kind.
func (env *Envelope) IsScenePaintingKind() bool {
	return env != nil && env.FixtureKind == FixtureKindScenePainting
}
