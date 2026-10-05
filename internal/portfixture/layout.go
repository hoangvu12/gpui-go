package portfixture

// This file is the shared style-tree machinery of the layout fixture
// sections: parsing the fixture's style key set into real gpui Styles,
// flattening the labeled style tree, requesting it through the real gpui
// layout adapter (the port's Taffy engine behind internal/native), and
// emitting the layout trace events. It mirrors the reference harness's
// fixtures (reference/harness/src/fixtures/layout_effects.rs and
// layout_metrics.rs) op for op: build_style, flatten_tree, build_layout,
// measure_fn and the prepaint bounds loop.
//
// The port's test-profile layout context uses the reference test window's
// hardcoded scale factor 2.0 (platform/test/window.rs) and the default
// rem size of 16 logical px, with a per-case rem override applied at
// REQUEST time (Window::with_rem_size scoping).

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gpui-go/conformance"
	"gpui-go/gpui"
)

// flatStyleNode is one flattened style-tree node (reference FlatNode).
type flatStyleNode struct {
	label    string
	style    gpui.Style
	children []int
}

// flattenStyleTree flattens the labeled style tree in preorder (the
// reference flatten_tree), parsing each node's style map.
func flattenStyleTree(node *conformance.StyleNode, out *[]flatStyleNode, extended bool) (int, error) {
	idx := len(*out)
	*out = append(*out, flatStyleNode{label: node.Label})
	style, err := parseStyleMap(node.Style, extended)
	if err != nil {
		return 0, fmt.Errorf("node %q: %w", node.Label, err)
	}
	(*out)[idx].style = style
	for i := range node.Children {
		childIdx, err := flattenStyleTree(&node.Children[i], out, extended)
		if err != nil {
			return 0, err
		}
		(*out)[idx].children = append((*out)[idx].children, childIdx)
	}
	return idx, nil
}

// parseGridTemplate parses a grid template value. The pinned reference's
// GridTemplate expresses exactly the forms its taffy adapter spells in
// crates/gpui/src/taffy.rs::to_grid_repeat:
//
//   - repeat(<n>, minmax(0, 1fr))         -> GridTemplateMinSize::Zero
//   - repeat(<n>, minmax(min-content, 1fr)) -> GridTemplateMinSize::MinContent
//   - repeat(<n>, minmax(0, max-content))  -> GridTemplateMinSize::MaxContent
//
// The zero minimum may be written 0 or 0px; whitespace is ignored. Any
// other form fails the run (the reference harness's parse_grid_template
// refuses to invent broader CSS).
func parseGridTemplate(value string) (gpui.GridTemplate, error) {
	compact := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, value)
	body, ok := strings.CutPrefix(compact, "repeat(")
	if !ok {
		return gpui.GridTemplate{}, fmt.Errorf("parsing grid template %q: expected repeat(<n>, <minmax>)", value)
	}
	body, ok = strings.CutSuffix(body, ")")
	if !ok {
		return gpui.GridTemplate{}, fmt.Errorf("parsing grid template %q: expected repeat(<n>, <minmax>)", value)
	}
	count, minmax, ok := strings.Cut(body, ",")
	if !ok {
		return gpui.GridTemplate{}, fmt.Errorf("parsing grid template %q: expected a count and a minmax()", value)
	}
	repeat, err := strconv.ParseUint(count, 10, 16)
	if err != nil {
		return gpui.GridTemplate{}, fmt.Errorf("parsing grid template repeat count %q in %q: %v", count, value, err)
	}
	inner, ok := strings.CutPrefix(minmax, "minmax(")
	if !ok {
		return gpui.GridTemplate{}, fmt.Errorf("parsing grid template %q: expected minmax(<min>, <max>)", value)
	}
	inner, ok = strings.CutSuffix(inner, ")")
	if !ok {
		return gpui.GridTemplate{}, fmt.Errorf("parsing grid template %q: expected minmax(<min>, <max>)", value)
	}
	min, max, ok := strings.Cut(inner, ",")
	if !ok {
		return gpui.GridTemplate{}, fmt.Errorf("parsing grid template %q: expected a min and a max", value)
	}
	var minSize gpui.GridTemplateMinSize
	switch {
	case (min == "0" || min == "0px") && max == "1fr":
		minSize = gpui.GridTemplateMinZero
	case min == "min-content" && max == "1fr":
		minSize = gpui.GridTemplateMinMinContent
	case (min == "0" || min == "0px") && max == "max-content":
		minSize = gpui.GridTemplateMinMaxContent
	default:
		return gpui.GridTemplate{}, fmt.Errorf(
			"unsupported grid template %q: the reference GridTemplate supports repeat(<n>, minmax(0, 1fr)), repeat(<n>, minmax(min-content, 1fr)) and repeat(<n>, minmax(0, max-content))", value)
	}
	return gpui.GridTemplate{Repeat: uint16(repeat), MinSize: minSize}, nil
}

// parseF32 parses an f32 like the reference harness's parse_f32.
func parseF32(value string) (float32, error) {
	f, err := strconv.ParseFloat(value, 32)
	if err != nil {
		return 0, fmt.Errorf("parsing f32 %q: %v", value, err)
	}
	return float32(f), nil
}

// setEdgesLength sets all four edges to one Length (reference
// set_edges_length).
func setEdgesLength(edges *gpui.LengthEdges, value gpui.Length) {
	edges.Top = value
	edges.Right = value
	edges.Bottom = value
	edges.Left = value
}

// setEdgesDefinite sets all four edges to one DefiniteLength (reference
// set_edges_definite).
func setEdgesDefinite(edges *gpui.DefiniteLengthEdges, value gpui.DefiniteLength) {
	edges.Top = value
	edges.Right = value
	edges.Bottom = value
	edges.Left = value
}

// setEdgesAbsolute sets all four edges to one AbsoluteLength (reference
// set_edges_absolute).
func setEdgesAbsolute(edges *gpui.AbsoluteLengthEdges, value gpui.AbsoluteLength) {
	edges.Top = value
	edges.Right = value
	edges.Bottom = value
	edges.Left = value
}

// parseStyleMap builds a gpui Style from the fixture's documented key
// set, mirroring the reference build_style: the base set shared with
// layout-effects-v1 plus (when extended is set) the layout-metrics keys
// (display, grid templates, position/inset, overflow, flex wrap,
// align-content, border widths). Unknown keys fail the run.
func parseStyleMap(m map[string]string, extended bool) (gpui.Style, error) {
	style := gpui.DefaultStyle()
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	// Sorted for deterministic error reporting (the reference iterates a
	// BTreeMap).
	sort.Strings(keys)
	for _, key := range keys {
		value := m[key]
		switch key {
		// Base set shared with the layout-effects-v1 fixture.
		case "width":
			length, err := gpui.ParseLength(value)
			if err != nil {
				return style, err
			}
			style.Size.Width = length
		case "height":
			length, err := gpui.ParseLength(value)
			if err != nil {
				return style, err
			}
			style.Size.Height = length
		case "minWidth":
			length, err := gpui.ParseLength(value)
			if err != nil {
				return style, err
			}
			style.MinSize.Width = length
		case "minHeight":
			length, err := gpui.ParseLength(value)
			if err != nil {
				return style, err
			}
			style.MinSize.Height = length
		case "maxWidth":
			length, err := gpui.ParseLength(value)
			if err != nil {
				return style, err
			}
			style.MaxSize.Width = length
		case "maxHeight":
			length, err := gpui.ParseLength(value)
			if err != nil {
				return style, err
			}
			style.MaxSize.Height = length
		case "flexGrow":
			v, err := parseF32(value)
			if err != nil {
				return style, err
			}
			style.FlexGrow = v
		case "flexShrink":
			v, err := parseF32(value)
			if err != nil {
				return style, err
			}
			style.FlexShrink = v
		case "flexBasis":
			length, err := gpui.ParseLength(value)
			if err != nil {
				return style, err
			}
			style.FlexBasis = length
		case "flexDirection":
			v, err := gpui.ParseFlexDirection(value)
			if err != nil {
				return style, err
			}
			style.FlexDirection = v
		case "justifyContent":
			v, err := gpui.ParseAlignContent(value)
			if err != nil {
				return style, err
			}
			style.JustifyContent = &v
		case "alignItems":
			v, err := gpui.ParseAlignItems(value)
			if err != nil {
				return style, err
			}
			style.AlignItems = &v
		case "alignSelf":
			v, err := gpui.ParseAlignItems(value)
			if err != nil {
				return style, err
			}
			style.AlignSelf = &v
		case "gap":
			length, err := gpui.ParseDefiniteLength(value)
			if err != nil {
				return style, err
			}
			style.Gap.Width = length
			style.Gap.Height = length
		case "padding":
			length, err := gpui.ParseDefiniteLength(value)
			if err != nil {
				return style, err
			}
			setEdgesDefinite(&style.Padding, length)
		case "margin":
			length, err := gpui.ParseLength(value)
			if err != nil {
				return style, err
			}
			setEdgesLength(&style.Margin, length)
		case "aspectRatio":
			v, err := parseF32(value)
			if err != nil {
				return style, err
			}
			style.AspectRatio = &v
		default:
			// The extended set exists only in the layout-metrics fixture;
			// the layout-effects fixture rejects those keys, exactly like
			// the reference's two separate build_style functions.
			if !extended {
				return style, fmt.Errorf("unsupported style key %q in layout-effects fixture", key)
			}
			if err := applyExtendedStyleKey(&style, key, value); err != nil {
				return style, err
			}
		}
	}
	return style, nil
}

// applyExtendedStyleKey applies one layout-metrics-v1 style key (the
// extended set: display, position/inset, overflow, flex wrap,
// align-content, border widths, grid templates). Unknown keys fail the
// run.
func applyExtendedStyleKey(style *gpui.Style, key, value string) error {
	switch key {
	case "display":
		v, err := gpui.ParseDisplay(value)
		if err != nil {
			return err
		}
		style.Display = v
	case "position":
		v, err := gpui.ParsePosition(value)
		if err != nil {
			return err
		}
		style.Position = v
	case "top":
		length, err := gpui.ParseLength(value)
		if err != nil {
			return err
		}
		style.Inset.Top = length
	case "right":
		length, err := gpui.ParseLength(value)
		if err != nil {
			return err
		}
		style.Inset.Right = length
	case "bottom":
		length, err := gpui.ParseLength(value)
		if err != nil {
			return err
		}
		style.Inset.Bottom = length
	case "left":
		length, err := gpui.ParseLength(value)
		if err != nil {
			return err
		}
		style.Inset.Left = length
	case "overflow":
		v, err := gpui.ParseOverflow(value)
		if err != nil {
			return err
		}
		style.OverflowX = v
		style.OverflowY = v
	case "flexWrap":
		v, err := gpui.ParseFlexWrap(value)
		if err != nil {
			return err
		}
		style.FlexWrap = v
	case "alignContent":
		v, err := gpui.ParseAlignContent(value)
		if err != nil {
			return err
		}
		style.AlignContent = &v
	case "borderWidth":
		length, err := gpui.ParseAbsoluteLength(value)
		if err != nil {
			return err
		}
		setEdgesAbsolute(&style.BorderWidths, length)
	case "gridTemplateColumns":
		template, err := parseGridTemplate(value)
		if err != nil {
			return err
		}
		style.GridCols = &template
	case "gridTemplateRows":
		template, err := parseGridTemplate(value)
		if err != nil {
			return err
		}
		style.GridRows = &template
	default:
		return fmt.Errorf("unsupported style key %q in layout-metrics fixture", key)
	}
	return nil
}

// laidOutNode is one tree node with its layout id (the reference
// build_layout's out slot).
type laidOutNode struct {
	label string
	id    gpui.LayoutID
}

// layoutTreeRunner requests one style tree through the REAL gpui layout
// adapter and emits the layout trace events.
type layoutTreeRunner struct {
	engine *gpui.LayoutEngine
	ctx    *gpui.LayoutContext
	rec    *recorder
}

// requestTree builds the layout tree bottom-up, mirroring the reference
// build_layout: children are requested first, then the parent with their
// ids; a node whose label carries a measurement spec is requested as a
// measured leaf (which must not have children). The returned slice holds
// the nodes in preorder (the order the reference's prepaint loop uses to
// emit layout-bounds events).
func (r *layoutTreeRunner) requestTree(flat []flatStyleNode, root int, measured map[string]conformance.MeasuredSpec) ([]laidOutNode, error) {
	var out []laidOutNode
	if _, err := r.buildLayout(flat, root, measured, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// buildLayout recursively requests one node (reference build_layout).
func (r *layoutTreeRunner) buildLayout(flat []flatStyleNode, idx int, measured map[string]conformance.MeasuredSpec, out *[]laidOutNode) (gpui.LayoutID, error) {
	slot := len(*out)
	*out = append(*out, laidOutNode{label: flat[idx].label})
	node := flat[idx]
	var id gpui.LayoutID
	if spec, ok := measured[node.label]; ok {
		if len(node.children) > 0 {
			return gpui.LayoutID{}, fmt.Errorf(
				"measured node %q must not have children: request_measured_layout creates a leaf", node.label)
		}
		var err error
		id, err = r.engine.RequestMeasuredLayout(r.ctx, node.style, makeMeasureFn(node.label, spec, r.rec))
		if err != nil {
			return gpui.LayoutID{}, fmt.Errorf("requesting measured layout for %q: %w", node.label, err)
		}
	} else {
		childIDs := make([]gpui.LayoutID, 0, len(node.children))
		for _, child := range node.children {
			childID, err := r.buildLayout(flat, child, measured, out)
			if err != nil {
				return gpui.LayoutID{}, err
			}
			childIDs = append(childIDs, childID)
		}
		var err error
		id, err = r.engine.RequestLayout(r.ctx, node.style, childIDs...)
		if err != nil {
			return gpui.LayoutID{}, fmt.Errorf("requesting layout for %q: %w", node.label, err)
		}
	}
	(*out)[slot].id = id
	return id, nil
}

// emitLayoutBounds computes nothing; it queries the bounds of every node
// in tree order and records one layout-bounds event per node with the
// exact f32 bit patterns of the logical coordinates (the reference
// prepaint loop).
func (r *layoutTreeRunner) emitLayoutBounds(nodes []laidOutNode) error {
	for _, node := range nodes {
		bounds, err := r.engine.LayoutBounds(r.ctx, node.id)
		if err != nil {
			return fmt.Errorf("layout bounds for %q: %w", node.label, err)
		}
		r.rec.eventWith("layout-bounds", node.label,
			field{"x", conformance.F32Bits(bounds.Origin.X)},
			field{"y", conformance.F32Bits(bounds.Origin.Y)},
			field{"width", conformance.F32Bits(bounds.Size.Width)},
			field{"height", conformance.F32Bits(bounds.Size.Height)},
		)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Measured nodes (reference measure_fn / compute_measured_size)
// ---------------------------------------------------------------------------

// makeMeasureFn builds the deterministic measure function for one
// measured node: it records a measure-query event with everything the
// layout engine passed (known dimensions, per-axis available space, in
// logical pixels exactly as the reference passes them) and a
// measure-result event with the raw logical size it returned. It never
// snaps: the adapter's snap_measured_size_to_device_pixels applies the
// clamp-and-ceil, and the trace must show the raw value the fixture
// returned.
func makeMeasureFn(label string, spec conformance.MeasuredSpec, rec *recorder) gpui.MeasureFunc {
	return func(req gpui.MeasureRequest) gpui.Size {
		fields := []field{
			{"label", label},
			{"knownWidthPresent", uint64(boolToU(req.KnownWidthPresent))},
			{"knownHeightPresent", uint64(boolToU(req.KnownHeightPresent))},
			{"availWidthTag", req.AvailWidth.String()},
			{"availHeightTag", req.AvailHeight.String()},
		}
		if req.KnownWidthPresent {
			fields = append(fields, field{"knownWidth", conformance.F32Bits(req.KnownWidth)})
		}
		if req.KnownHeightPresent {
			fields = append(fields, field{"knownHeight", conformance.F32Bits(req.KnownHeight)})
		}
		if req.AvailWidth.Kind == gpui.AvailDefinite {
			fields = append(fields, field{"availWidth", conformance.F32Bits(req.AvailWidth.Definite)})
		}
		if req.AvailHeight.Kind == gpui.AvailDefinite {
			fields = append(fields, field{"availHeight", conformance.F32Bits(req.AvailHeight.Definite)})
		}
		rec.eventWith("measure-query", label, fields...)

		measured := computeMeasuredSize(spec, req)
		rec.eventWith("measure-result", label,
			field{"label", label},
			field{"width", conformance.F32Bits(measured.Width)},
			field{"height", conformance.F32Bits(measured.Height)},
		)
		return measured
	}
}

// computeMeasuredSize computes the measured size for one query per the
// spec kind (reference compute_measured_size). All values are raw
// logical pixels; no clamping or snapping happens here.
//
//   - fixed: the spec size, verbatim.
//   - echo-known: the known dimension when present, else the spec size.
//   - minmax: the known dimension clamped to [floor, max] per axis (a
//     missing max is unbounded), else the floor.
func computeMeasuredSize(spec conformance.MeasuredSpec, known gpui.MeasureRequest) gpui.Size {
	switch spec.Kind {
	case conformance.MeasuredKindFixed:
		return gpui.Size{Width: float32(spec.Width), Height: float32(spec.Height)}
	case conformance.MeasuredKindEchoKnown:
		width := float32(spec.Width)
		if known.KnownWidthPresent {
			width = known.KnownWidth
		}
		height := float32(spec.Height)
		if known.KnownHeightPresent {
			height = known.KnownHeight
		}
		return gpui.Size{Width: width, Height: height}
	case conformance.MeasuredKindMinMax:
		return gpui.Size{
			Width:  minmaxAxis(known.KnownWidthPresent, known.KnownWidth, float32(spec.Width), float32(spec.MaxWidth), spec.MaxWidth != 0),
			Height: minmaxAxis(known.KnownHeightPresent, known.KnownHeight, float32(spec.Height), float32(spec.MaxHeight), spec.MaxHeight != 0),
		}
	default:
		// Validated before the layout runs; unreachable here.
		panic(fmt.Sprintf("unsupported measured spec kind %q", spec.Kind))
	}
}

// minmaxAxis clamps one axis per the minmax spec kind (reference
// minmax_axis): the known value clamped up to the floor and down to the
// max when present, else the floor.
func minmaxAxis(present bool, known, floor, max float32, hasMax bool) float32 {
	if !present {
		return floor
	}
	value := known
	if value < floor {
		value = floor
	}
	if hasMax && value > max {
		value = max
	}
	return value
}

// boolToU converts a bool to 0/1 (Rust's `as u64`).
func boolToU(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

// axisAvailable maps an available-space mode string plus a definite
// fallback value to the AvailableSpace offered on that axis (reference
// axis_available).
func axisAvailable(mode string, definite float64) (gpui.AvailableSpace, error) {
	switch mode {
	case "", conformance.AvailModeDefinite:
		return gpui.DefiniteAvailableSpace(float32(definite)), nil
	case conformance.AvailModeMinContent:
		return gpui.MinContentAvailableSpace(), nil
	case conformance.AvailModeMaxContent:
		return gpui.MaxContentAvailableSpace(), nil
	default:
		return gpui.AvailableSpace{}, fmt.Errorf("unsupported available space mode %q", mode)
	}
}
