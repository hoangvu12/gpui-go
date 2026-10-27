package gpui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// The CSS-value parsers the TSX style keys take (browser-core ticket
// 57): the clip-path and mask-image spellings and the drop-shadow
// value — the same grammar the class layer's arbitrary values accept,
// shared by parseStyle (gpui/script/node.go) so a .tsx style object
// can carry the CSS strings verbatim.

// ParseClipPath parses a CSS basic-shape clip-path value (the subset
// the ClipPath carries): circle(<length> [at <x> <y>]),
// ellipse(<rx> <ry> [at <x> <y>]), inset(<top> <right> <bottom>
// <left>) and polygon(<x> <y>[, <x> <y>]…) with lengths as pixels or
// percentages of the reference box (the element's border box). The
// circle's center and the inset's edges default per CSS.
func ParseClipPath(value string) (ClipPath, error) {
	kind, inner, err := parseFunctionOf(value)
	if err != nil {
		return ClipPath{}, err
	}
	switch kind {
	case "circle":
		fields := splitTopLevel(inner, ' ')
		if len(fields) == 0 {
			return ClipPath{}, fmt.Errorf("clip-path circle(…) takes a radius, like circle(40px)")
		}
		radius, err := parseClipLength(fields[0])
		if err != nil {
			return ClipPath{}, fmt.Errorf("clip-path circle radius: %v", err)
		}
		cx, cy, err := parseAtPosition(fields[1:])
		if err != nil {
			return ClipPath{}, fmt.Errorf("clip-path circle position: %v", err)
		}
		if cx == nil {
			return ClipPathCircle(radius), nil
		}
		return ClipPathCircleAt(radius, *cx, *cy), nil
	case "ellipse":
		fields := splitTopLevel(inner, ' ')
		if len(fields) < 2 {
			return ClipPath{}, fmt.Errorf("clip-path ellipse(…) takes two radii, like ellipse(30%% 50%%)")
		}
		rx, err := parseClipLength(fields[0])
		if err != nil {
			return ClipPath{}, fmt.Errorf("clip-path ellipse radii: %v", err)
		}
		ry, err := parseClipLength(fields[1])
		if err != nil {
			return ClipPath{}, fmt.Errorf("clip-path ellipse radii: %v", err)
		}
		cx, cy, err := parseAtPosition(fields[2:])
		if err != nil {
			return ClipPath{}, fmt.Errorf("clip-path ellipse position: %v", err)
		}
		if cx == nil {
			return ClipPathEllipse(rx, ry), nil
		}
		return ClipPathEllipseAt(rx, ry, *cx, *cy), nil
	case "inset":
		fields := splitTopLevel(inner, ' ')
		if len(fields) == 0 || len(fields) > 4 {
			return ClipPath{}, fmt.Errorf("clip-path inset(…) takes one to four edges, like inset(10px 20px)")
		}
		edges := make([]ClipLength, 4)
		for i, field := range fields {
			length, err := parseClipLength(field)
			if err != nil {
				return ClipPath{}, fmt.Errorf("clip-path inset edges: %v", err)
			}
			edges[i] = length
		}
		// The CSS shorthand: 1 = all, 2 = vertical horizontal, 3 = top
		// horizontal bottom, 4 = top right bottom left.
		top, right, bottom, left := edges[0], edges[0], edges[0], edges[0]
		switch len(fields) {
		case 2:
			right, left = edges[1], edges[1]
		case 3:
			right, left = edges[1], edges[1]
			bottom = edges[2]
		case 4:
			right, bottom, left = edges[1], edges[2], edges[3]
		}
		return ClipPathInset(top, right, bottom, left), nil
	case "polygon":
		var points []ClipPoint
		for _, pair := range splitTopLevel(inner, ',') {
			fields := splitTopLevel(pair, ' ')
			if len(fields) != 2 {
				return ClipPath{}, fmt.Errorf("clip-path polygon(…) takes x y pairs, like polygon(50%% 0, 100%% 100%%, 0 100%%)")
			}
			x, err := parseClipLength(fields[0])
			if err != nil {
				return ClipPath{}, fmt.Errorf("clip-path polygon vertex: %v", err)
			}
			y, err := parseClipLength(fields[1])
			if err != nil {
				return ClipPath{}, fmt.Errorf("clip-path polygon vertex: %v", err)
			}
			points = append(points, ClipPoint{X: x, Y: y})
		}
		if len(points) < 3 {
			return ClipPath{}, fmt.Errorf("clip-path polygon(…) needs at least three vertices")
		}
		return ClipPathPolygon(points...), nil
	}
	return ClipPath{}, fmt.Errorf("clip-path %q is not supported (use circle(…), ellipse(…), inset(…) or polygon(…))", kind)
}

// ParseMaskImage parses a CSS mask-image value (the subset the
// MaskImage carries): a linear-gradient —
// linear-gradient(<direction>, <color> [<pos>][, <color> [<pos>]]…)
// with the direction a `to <side>` keyword or an angle and the stops
// hex colors (optionally with a percentage position). mask-mode is the
// alpha default.
func ParseMaskImage(value string) (*MaskImage, error) {
	kind, inner, err := parseFunctionOf(value)
	if err != nil {
		return nil, err
	}
	if kind != "linear-gradient" {
		return nil, fmt.Errorf("mask-image %q is not supported (use linear-gradient(…))", kind)
	}
	parts := splitTopLevel(inner, ',')
	if len(parts) < 2 {
		return nil, fmt.Errorf("mask-image linear-gradient(…) takes a direction and at least one stop, like linear-gradient(to bottom, #000000, #00000000)")
	}
	gradient := CssGradient{Kind: CssGradientLinear}
	fields := splitTopLevel(parts[0], ' ')
	if len(fields) > 0 && strings.HasPrefix(parts[0], "to ") {
		direction, err := parseGradientSide(parts[0])
		if err != nil {
			return nil, err
		}
		gradient.Direction = direction
		parts = parts[1:]
	} else if len(fields) == 1 && strings.HasSuffix(parts[0], "deg") {
		degrees, err := strconv.ParseFloat(strings.TrimSuffix(parts[0], "deg"), 32)
		if err != nil {
			return nil, fmt.Errorf("mask-image linear-gradient angle: %v", err)
		}
		gradient.Direction = GradientDirectionAngle
		gradient.Angle = float32(degrees)
		parts = parts[1:]
	}
	for _, part := range parts {
		fields := splitTopLevel(part, ' ')
		color, err := ParseHex(fields[0])
		if err != nil {
			if fields[0] == "transparent" {
				color = Hsla{}
			} else {
				return nil, fmt.Errorf("mask-image stop colors take hex, like #000000 (got %q)", fields[0])
			}
		}
		stop := GradientStop{Color: color, Position: float32(math.NaN())}
		if len(fields) > 1 {
			position, err := parseGradientPosition(fields[1])
			if err != nil {
				return nil, err
			}
			stop.Position = position
		}
		gradient.Stops = append(gradient.Stops, stop)
	}
	if len(gradient.Stops) == 0 {
		return nil, fmt.Errorf("mask-image linear-gradient(…) needs at least one stop")
	}
	return GradientMaskImage(gradient), nil
}

// ParseDropShadowValues parses a CSS filter drop-shadow value list —
// "0 4px 6px #00000040[, <x> <y> <blur> <color>…]" — one or more
// shadows, each the two offsets, the blur radius and a hex color.
func ParseDropShadowValues(value string) ([]Filter, error) {
	var out []Filter
	for _, shadow := range splitTopLevel(value, ',') {
		fields := splitTopLevel(shadow, ' ')
		if len(fields) < 3 || len(fields) > 4 {
			return nil, fmt.Errorf("drop-shadow takes two offsets, an optional blur and a color, like 0 4px 6px #00000040")
		}
		var lengths [3]float32
		for i, field := range fields[:3] {
			v, ok := parsePixelValue(field)
			if !ok || v < 0 {
				return nil, fmt.Errorf("drop-shadow takes pixel lengths, like 0 4px 6px #00000040")
			}
			lengths[i] = v
		}
		color := Hsla{H: 0, S: 0, L: 0, A: 0.25}
		if len(fields) == 4 {
			parsed, err := ParseHex(fields[3])
			if err != nil {
				return nil, fmt.Errorf("drop-shadow colors take hex, like #00000040 (got %q)", fields[3])
			}
			color = parsed
		}
		out = append(out, DropShadow(lengths[0], lengths[1], lengths[2], color))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("drop-shadow needs at least one shadow")
	}
	return out, nil
}

// parseFunctionOf splits "name(inner)" into its parts.
func parseFunctionOf(value string) (kind, inner string, err error) {
	value = strings.TrimSpace(value)
	open := strings.Index(value, "(")
	if open <= 0 || !strings.HasSuffix(value, ")") {
		return "", "", fmt.Errorf("%q is not a CSS function value", value)
	}
	return strings.TrimSpace(value[:open]), strings.TrimSpace(value[open+1 : len(value)-1]), nil
}

// splitTopLevel splits on the separator outside any parentheses.
func splitTopLevel(s string, sep byte) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case sep:
			if depth == 0 {
				if i > start {
					out = append(out, strings.TrimSpace(s[start:i]))
				}
				start = i + 1
			}
		}
	}
	if start < len(s) {
		out = append(out, strings.TrimSpace(s[start:]))
	}
	return out
}

// parseClipLength parses a clip-path length: Npx or N%.
func parseClipLength(field string) (ClipLength, error) {
	field = strings.TrimSpace(field)
	if strings.HasSuffix(field, "%") {
		f, err := strconv.ParseFloat(strings.TrimSuffix(field, "%"), 32)
		if err != nil {
			return ClipLength{}, fmt.Errorf("%q is not a percentage", field)
		}
		return ClipFraction(float32(f) / 100), nil
	}
	v, ok := parsePixelValue(field)
	if !ok {
		return ClipLength{}, fmt.Errorf("%q is not a pixel length or percentage", field)
	}
	return ClipPx(v), nil
}

// parseAtPosition parses the trailing "at x y" of a shape's grammar.
func parseAtPosition(fields []string) (*ClipLength, *ClipLength, error) {
	if len(fields) == 0 {
		return nil, nil, nil
	}
	if len(fields) != 3 || fields[0] != "at" {
		return nil, nil, fmt.Errorf("a shape's position is `at <x> <y>`, like at 50%% 25%%")
	}
	cx, err := parseClipLength(fields[1])
	if err != nil {
		return nil, nil, err
	}
	cy, err := parseClipLength(fields[2])
	if err != nil {
		return nil, nil, err
	}
	return &cx, &cy, nil
}

// parseGradientSide parses `to <side>` keywords into the direction.
func parseGradientSide(value string) (GradientDirection, error) {
	switch strings.TrimSpace(value) {
	case "to top":
		return GradientDirectionToTop, nil
	case "to top right", "to right top":
		return GradientDirectionToTopRight, nil
	case "to right":
		return GradientDirectionToRight, nil
	case "to bottom right", "to right bottom":
		return GradientDirectionToBottomRight, nil
	case "to bottom":
		return GradientDirectionToBottom, nil
	case "to bottom left", "to left bottom":
		return GradientDirectionToBottomLeft, nil
	case "to left":
		return GradientDirectionToLeft, nil
	case "to top left", "to left top":
		return GradientDirectionToTopLeft, nil
	}
	return 0, fmt.Errorf("%q is not a `to <side>` direction", value)
}

// parseGradientPosition parses a stop position: N%.
func parseGradientPosition(field string) (float32, error) {
	if !strings.HasSuffix(field, "%") {
		return 0, fmt.Errorf("%q is not a percentage position", field)
	}
	f, err := strconv.ParseFloat(strings.TrimSuffix(field, "%"), 32)
	if err != nil {
		return 0, fmt.Errorf("%q is not a percentage position", field)
	}
	return float32(f) / 100, nil
}

// parsePixelValue parses "N" or "Npx".
func parsePixelValue(field string) (float32, bool) {
	field = strings.TrimSpace(field)
	if strings.HasSuffix(field, "px") {
		field = strings.TrimSuffix(field, "px")
	}
	f, err := strconv.ParseFloat(field, 32)
	if err != nil {
		return 0, false
	}
	return float32(f), true
}
