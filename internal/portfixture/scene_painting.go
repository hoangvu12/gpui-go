package portfixture

// This file implements the scene-painting-v1 fixture runner: one case at
// a time, each in its own window-equivalent scope (a fresh layout engine
// for the style tree, mirroring the reference's one TestAppContext
// window per case), then the paint script executed against the port's
// real Scene (the native scene kernel behind internal/native) through
// the port's paint API (gpui.PaintQuad/PaintShadow/PaintUnderline/
// PaintBackdrop/BeginLayer/BeginFilterGroup), then the finished scene's
// plan and primitive arrays dumped and recorded as the same
// scene-command/scene-op/scene-meta trace events the reference harness
// records (reference/harness/src/fixtures/scene_painting.rs, run_case
// op for op).
//
// The gate (scene_painting_test.go) compares the port trace against the
// recorded reference trace with CompareTraces: every event must match
// exactly, including every f32 bit pattern of every color, bound, radius
// and width, and every draw order and plan command.

import (
	"fmt"
	"math"

	"gpui-go/conformance"
	"gpui-go/gpui"
)

// RunScenePainting executes the envelope's scene cases against the real
// gpui scene kernel and returns the port-side trace (with the port
// harness identity and no envelope hash: run metadata belongs to the
// caller, which pins the envelope bytes it executed).
func RunScenePainting(envelope *conformance.Envelope) (*conformance.Trace, error) {
	if envelope == nil {
		return nil, fmt.Errorf("portfixture: nil envelope")
	}
	if envelope.FixtureKind != conformance.FixtureKindScenePainting {
		return nil, fmt.Errorf("portfixture: unsupported fixture kind %q", envelope.FixtureKind)
	}
	inputs := envelope.Inputs.SceneCases
	if inputs == nil {
		return nil, fmt.Errorf("portfixture: fixture kind scene-painting-v1 requires inputs.scene_cases")
	}

	rec := &recorder{}
	// The finished scenes stay pinned for later replay ranges (the
	// contract: a replay reference pins the old scene) until the run
	// ends, when every native scene is disposed (native handles do not
	// follow Go garbage collection).
	scenes := make(map[string]*gpui.Scene)
	defer func() {
		for _, scene := range scenes {
			_ = scene.Dispose()
		}
	}()
	for i := range inputs.Cases {
		if err := runScenePaintingCase(&inputs.Cases[i], rec, scenes); err != nil {
			return nil, fmt.Errorf("scene-painting case %q failed: %w", inputs.Cases[i].Label, err)
		}
	}

	trace := &conformance.Trace{
		Schema:      conformance.TraceSchema,
		FixtureID:   envelope.FixtureID,
		FixtureKind: envelope.FixtureKind,
		Harness: conformance.HarnessInfo{
			HarnessVersion: "0.1.0",
			HarnessCrate:   "gpui-go-portfixture",
			GpuiCrate:      "gpui-ce 0.2.2",
			GpuiCommit:     pinnedGpuiCommit,
			Profile:        "test",
		},
		Events: rec.snapshot(),
	}
	return trace, nil
}

// sceneScriptState is the paint script's execution state.
type sceneScriptState struct {
	scene *gpui.Scene
	// ctx carries the scale, the current mask default and the element
	// opacity (multiplied by nested begin-layer scopes).
	ctx *gpui.PaintContext
	// opacityStack holds the element opacity at each open begin-layer.
	opacityStack []float32
	// layerPushedStack mirrors the reference's clipped-bounds check per
	// begin-layer: pop only when the begin pushed.
	layerPushedStack []bool
	// layerPushCount counts the actual push_layer calls (the scene-meta
	// layerCount observable).
	layerPushCount uint32
	// pendingGroups tracks open content-filter groups; identity groups
	// (nil entry) run inline and their end is a no-op.
	pendingGroups []*conformance.PaintOp
}

// runScenePaintingCase executes one case, mirroring the reference
// run_case: case-begin, the layout section (style tree under the case's
// available space, every node's bounds recorded), the paint script
// against a fresh scene, the finished scene's plan recorded, case-end.
func runScenePaintingCase(c *conformance.ScenePaintingCase, rec *recorder, scenes map[string]*gpui.Scene) error {
	if c.StyleTree == nil {
		return fmt.Errorf("case %q has no style tree", c.Label)
	}

	// Layout section: a fresh engine per case (the reference opens one
	// window per case), the test-profile scale 2.0 and the default rem
	// size 16, exactly like the layout-metrics runner.
	engine, err := gpui.NewLayoutEngine()
	if err != nil {
		return fmt.Errorf("layout engine: %w", err)
	}
	defer engine.Dispose()

	scale := gpui.TestScaleFactor
	remSize := float64(gpui.DefaultRemSize)
	rec.eventWith("case-begin", c.Label,
		field{"label", c.Label},
		field{"remSize", conformance.F32Bits(float32(remSize))},
		field{"scale", conformance.F32Bits(scale)},
	)
	ctx := gpui.NewLayoutContext(float32(remSize), scale)
	runner := &layoutTreeRunner{engine: engine, ctx: ctx, rec: rec}

	var flat []flatStyleNode
	root, err := flattenStyleTree(c.StyleTree, &flat, true)
	if err != nil {
		return fmt.Errorf("style tree: %w", err)
	}
	nodes, err := runner.requestTree(flat, root, nil)
	if err != nil {
		return fmt.Errorf("layout tree: %w", err)
	}
	if len(nodes) == 0 {
		return fmt.Errorf("case %q style tree has no nodes", c.Label)
	}
	available := gpui.AvailableSize{
		Width:  gpui.DefiniteAvailableSpace(float32(c.AvailableSpace.Width)),
		Height: gpui.DefiniteAvailableSpace(float32(c.AvailableSpace.Height)),
	}
	if err := engine.ComputeLayout(ctx, nodes[0].id, available); err != nil {
		return fmt.Errorf("compute layout: %w", err)
	}
	boundsByLabel := make(map[string]gpui.Bounds, len(nodes))
	for _, node := range nodes {
		bounds, err := engine.LayoutBounds(ctx, node.id)
		if err != nil {
			return fmt.Errorf("layout bounds for %q: %w", node.label, err)
		}
		boundsByLabel[node.label] = bounds
		rec.eventWith("layout-bounds", node.label,
			field{"x", conformance.F32Bits(bounds.Origin.X)},
			field{"y", conformance.F32Bits(bounds.Origin.Y)},
			field{"width", conformance.F32Bits(bounds.Size.Width)},
			field{"height", conformance.F32Bits(bounds.Size.Height)},
		)
	}

	// Scene section: the paint script against a fresh port scene.
	scene, err := gpui.NewScene()
	if err != nil {
		return fmt.Errorf("scene: %w", err)
	}
	defer func() {
		// The scene stays pinned in the scenes map for later replay
		// ranges; dispose only when it was never published.
		if _, ok := scenes[c.Label]; !ok {
			_ = scene.Dispose()
		}
	}()

	defaultMask := gpui.Bounds{
		Origin: gpui.Point{},
		Size:   gpui.Size{Width: float32(c.AvailableSpace.Width), Height: float32(c.AvailableSpace.Height)},
	}
	state := &sceneScriptState{
		scene: scene,
		ctx:   gpui.NewPaintContext(scale, defaultMask, 1.0),
	}

	for i := range c.PaintScript {
		op := &c.PaintScript[i]
		if err := runPaintOp(state, op, boundsByLabel, defaultMask, scenes); err != nil {
			return fmt.Errorf("paint op %s: %w", op.Op, err)
		}
	}
	if len(state.pendingGroups) != 0 {
		return fmt.Errorf("unclosed content-filter group(s) at the end of the paint script")
	}
	if len(state.layerPushedStack) != 0 {
		return fmt.Errorf("unbalanced layer(s) at the end of the paint script")
	}
	if err := scene.Finish(); err != nil {
		return fmt.Errorf("scene finish: %w", err)
	}

	if err := recordSceneEvents(scene, rec, state.layerPushCount); err != nil {
		return fmt.Errorf("recording scene: %w", err)
	}

	scenes[c.Label] = scene
	rec.event("case-end", c.Label)
	return nil
}

// runPaintOp executes one paint-script op against the port's scene,
// mirroring the reference fixture's op handling.
func runPaintOp(state *sceneScriptState, op *conformance.PaintOp, boundsByLabel map[string]gpui.Bounds, defaultMask gpui.Bounds, scenes map[string]*gpui.Scene) error {
	nodeBounds := func(label string) (gpui.Bounds, error) {
		bounds, ok := boundsByLabel[label]
		if !ok {
			return gpui.Bounds{}, fmt.Errorf("paint script references unknown node label %q", label)
		}
		return bounds, nil
	}
	maskOf := func(m *conformance.RectIn) gpui.Bounds {
		if m == nil {
			return defaultMask
		}
		return gpui.Bounds{
			Origin: gpui.Point{X: float32(m.X), Y: float32(m.Y)},
			Size:   gpui.Size{Width: float32(m.Width), Height: float32(m.Height)},
		}
	}
	cornersOf := func(uniform *float64, per *conformance.CornersIn) gpui.Corners {
		if per != nil {
			return gpui.Corners{
				TopLeft:     float32(per.TopLeft),
				TopRight:    float32(per.TopRight),
				BottomRight: float32(per.BottomRight),
				BottomLeft:  float32(per.BottomLeft),
			}
		}
		if uniform != nil {
			return gpui.Corners{TopLeft: float32(*uniform), TopRight: float32(*uniform), BottomRight: float32(*uniform), BottomLeft: float32(*uniform)}
		}
		return gpui.Corners{}
	}
	edgesOf := func(uniform *float64, per *conformance.EdgesIn) gpui.Edges {
		if per != nil {
			return gpui.Edges{
				Top:    float32(per.Top),
				Right:  float32(per.Right),
				Bottom: float32(per.Bottom),
				Left:   float32(per.Left),
			}
		}
		if uniform != nil {
			return gpui.Edges{Top: float32(*uniform), Right: float32(*uniform), Bottom: float32(*uniform), Left: float32(*uniform)}
		}
		return gpui.Edges{}
	}
	colorOf := func(hex string) (gpui.Hsla, error) {
		value, err := parseSceneHexColor(hex)
		if err != nil {
			return gpui.Hsla{}, err
		}
		return gpui.RgbaToHsla(value), nil
	}
	smoothingOf := func(v *float64) float32 {
		if v == nil {
			return 0
		}
		return float32(*v)
	}

	// opCtx is the per-op paint context: the element opacity and scale
	// from the script state, with the op's explicit mask applied (the
	// reference computes effective_mask per command; the default mask is
	// the case's available space).
	opCtx := *state.ctx
	opCtx.Mask = maskOf(op.Mask)

	switch op.Op {
	case conformance.OpPaintBox:
		bounds, err := nodeBounds(op.Label)
		if err != nil {
			return err
		}
		background := gpui.SolidBackground(gpui.TransparentBlack())
		if op.Background != "" {
			hsla, err := colorOf(op.Background)
			if err != nil {
				return err
			}
			background = gpui.SolidBackground(hsla)
		}
		borderColor := gpui.SolidBackground(gpui.TransparentBlack())
		if op.BorderColor != "" {
			hsla, err := colorOf(op.BorderColor)
			if err != nil {
				return err
			}
			borderColor = gpui.SolidBackground(hsla)
		}
		style := gpui.BorderStyleSolid
		switch op.BorderStyle {
		case "", "Solid":
			style = gpui.BorderStyleSolid
		case "Dashed":
			style = gpui.BorderStyleDashed
		default:
			return fmt.Errorf("unsupported border style %q", op.BorderStyle)
		}
		pq := gpui.PaintQuad{
			Bounds:             bounds,
			Background:         background,
			BorderColor:        borderColor,
			CornerRadii:        cornersOf(op.CornerRadius, op.CornerRadii),
			BorderWidths:       edgesOf(op.BorderWidth, op.BorderWidths),
			BorderStyle:        style,
			BorderDashedLength: gpui.DefaultBorderDashedLength,
			BorderDashedGap:    gpui.DefaultBorderDashedGap,
		}
		return state.scene.PaintQuad(pq, smoothingOf(op.CornerSmoothing), &opCtx)

	case conformance.OpPaintShadow:
		bounds, err := nodeBounds(op.Label)
		if err != nil {
			return err
		}
		hsla, err := colorOf(op.Color)
		if err != nil {
			return err
		}
		shadow := gpui.BoxShadow{
			Color:        gpui.SolidBackground(hsla),
			OffsetX:      optFloat32(op.OffsetX),
			OffsetY:      optFloat32(op.OffsetY),
			BlurRadius:   optFloat32(op.BlurRadius),
			SpreadRadius: optFloat32(op.SpreadRadius),
			Inset:        op.Inset != nil && *op.Inset,
		}
		return state.scene.PaintShadow(bounds, shadow, cornersOf(op.CornerRadius, op.CornerRadii), smoothingOf(op.CornerSmoothing), &opCtx)

	case conformance.OpPaintUnderline:
		bounds, err := nodeBounds(op.Label)
		if err != nil {
			return err
		}
		hsla, err := colorOf(op.Color)
		if err != nil {
			return err
		}
		width := bounds.Size.Width
		if op.Width != nil {
			width = float32(*op.Width)
		}
		style := gpui.UnderlineStyle{
			Thickness: float32(*op.Thickness),
			Color:     &hsla,
			Wavy:      op.Wavy != nil && *op.Wavy,
		}
		return state.scene.PaintUnderline(bounds.Origin, width, style, &opCtx)

	case conformance.OpBeginLayer:
		var layerBounds gpui.Bounds
		switch {
		case op.Label != "":
			bounds, err := nodeBounds(op.Label)
			if err != nil {
				return err
			}
			layerBounds = bounds
		case op.Bounds != nil:
			layerBounds = gpui.Bounds{
				Origin: gpui.Point{X: float32(op.Bounds.X), Y: float32(op.Bounds.Y)},
				Size:   gpui.Size{Width: float32(op.Bounds.Width), Height: float32(op.Bounds.Height)},
			}
		default:
			return fmt.Errorf("begin-layer requires a label or explicit bounds")
		}
		if err := state.scene.BeginLayer(layerBounds, &opCtx); err != nil {
			return err
		}
		// The reference recomputes the same clipped-bounds emptiness at
		// pop time; track the begin's outcome instead (identical result).
		clipped := intersectLogicalBounds(layerBounds, opCtx.Mask)
		pushed := !isEmptyLogicalBounds(clipped)
		if pushed {
			state.layerPushCount++
		}
		state.layerPushedStack = append(state.layerPushedStack, pushed)
		state.opacityStack = append(state.opacityStack, state.ctx.ElementOpacity)
		if op.Opacity != nil {
			state.ctx = state.ctx.WithElementOpacity(float32(*op.Opacity))
		}
		return nil

	case conformance.OpEndLayer:
		if len(state.layerPushedStack) == 0 {
			return fmt.Errorf("end-layer without a matching begin-layer")
		}
		pushed := state.layerPushedStack[len(state.layerPushedStack)-1]
		state.layerPushedStack = state.layerPushedStack[:len(state.layerPushedStack)-1]
		if pushed {
			if err := state.scene.EndLayer(); err != nil {
				return err
			}
		}
		if len(state.opacityStack) > 0 {
			restored := *state.ctx
			restored.ElementOpacity = state.opacityStack[len(state.opacityStack)-1]
			state.ctx = &restored
			state.opacityStack = state.opacityStack[:len(state.opacityStack)-1]
		}
		return nil

	case conformance.OpRaiseFloor:
		return state.scene.RaiseOrderFloor()

	case conformance.OpBeginFilterGroup:
		if _, err := nodeBounds(op.Label); err != nil {
			return err
		}
		// Identity groups run inline: track the op for the matching end.
		state.pendingGroups = append(state.pendingGroups, op)
		if blurIsIdentity(op.BlurRadius) {
			return nil
		}
		bounds, err := nodeBounds(op.Label)
		if err != nil {
			return err
		}
		return state.scene.BeginFilterGroup(bounds, float32(*op.BlurRadius), cornersOf(op.CornerRadius, op.CornerRadii), smoothingOf(op.CornerSmoothing), &opCtx)

	case conformance.OpEndFilterGroup:
		if len(state.pendingGroups) == 0 {
			return fmt.Errorf("end-filter-group without a matching begin")
		}
		begin := state.pendingGroups[len(state.pendingGroups)-1]
		state.pendingGroups = state.pendingGroups[:len(state.pendingGroups)-1]
		if blurIsIdentity(begin.BlurRadius) {
			// Identity group: already ran inline.
			return nil
		}
		bounds, err := nodeBounds(begin.Label)
		if err != nil {
			return err
		}
		// The end marker uses the SAME snapshot as its begin (the pinned
		// with_filter_layer snapshots once): the begin op's parameters,
		// including its mask.
		endCtx := *state.ctx
		endCtx.Mask = maskOf(begin.Mask)
		return state.scene.EndFilterGroup(bounds, float32(*begin.BlurRadius), cornersOf(begin.CornerRadius, begin.CornerRadii), smoothingOf(begin.CornerSmoothing), &endCtx)

	case conformance.OpPaintBackdrop:
		bounds, err := nodeBounds(op.Label)
		if err != nil {
			return err
		}
		return state.scene.PaintBackdrop(bounds, float32(*op.BlurRadius), cornersOf(op.CornerRadius, op.CornerRadii), smoothingOf(op.CornerSmoothing), &opCtx)

	case conformance.OpReplay:
		prev, ok := scenes[op.Source]
		if !ok {
			return fmt.Errorf("replay references unknown source case %q", op.Source)
		}
		return state.scene.Replay(op.Start, op.End, prev)

	default:
		return fmt.Errorf("unknown paint op %q", op.Op)
	}
}

// blurIsIdentity mirrors the pinned Filter::is_identity for Blur:
// radius <= 0.
func blurIsIdentity(blur *float64) bool {
	return blur == nil || *blur <= 0
}

func optFloat32(v *float64) float32 {
	if v == nil {
		return 0
	}
	return float32(*v)
}

func intersectLogicalBounds(a, b gpui.Bounds) gpui.Bounds {
	upperLeftX := max32(a.Origin.X, b.Origin.X)
	upperLeftY := max32(a.Origin.Y, b.Origin.Y)
	brX := max32(min32(a.Right(), b.Right()), upperLeftX)
	brY := max32(min32(a.Bottom(), b.Bottom()), upperLeftY)
	return gpui.Bounds{Origin: gpui.Point{X: upperLeftX, Y: upperLeftY}, Size: gpui.Size{Width: brX - upperLeftX, Height: brY - upperLeftY}}
}

func isEmptyLogicalBounds(b gpui.Bounds) bool {
	return b.Size.Width <= 0 || b.Size.Height <= 0
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// parseSceneHexColor parses an RRGGBBAA hex string (optional # or 0x
// prefix).
func parseSceneHexColor(hex string) (uint32, error) {
	compact := ""
	for _, c := range hex {
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		compact += string(c)
	}
	stripped := compact
	if len(stripped) >= 1 && stripped[0] == '#' {
		stripped = stripped[1:]
	} else if len(stripped) >= 2 && (stripped[:2] == "0x" || stripped[:2] == "0X") {
		stripped = stripped[2:]
	}
	if len(stripped) != 8 {
		return 0, fmt.Errorf("parsing color %q: expected 8 hex digits RRGGBBAA", hex)
	}
	for _, c := range stripped {
		if !isHexDigit(byte(c)) {
			return 0, fmt.Errorf("parsing color %q: expected 8 hex digits RRGGBBAA", hex)
		}
	}
	var value uint64
	for _, c := range stripped {
		value = value*16 + uint64(hexDigitValue(byte(c)))
	}
	return uint32(value), nil
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func hexDigitValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	default:
		return int(c-'A') + 10
	}
}

// ---------------------------------------------------------------------------
// Trace emission (mirrors the reference record_scene)
// ---------------------------------------------------------------------------

// recordSceneEvents walks the finished scene's compiled plan and records
// the scene-command, scene-op and scene-meta events, in plan order.
func recordSceneEvents(scene *gpui.Scene, rec *recorder, layerCount uint32) error {
	commands, err := scene.Commands()
	if err != nil {
		return fmt.Errorf("plan commands: %w", err)
	}
	quads, err := scene.Quads()
	if err != nil {
		return fmt.Errorf("quads: %w", err)
	}
	shadows, err := scene.Shadows()
	if err != nil {
		return fmt.Errorf("shadows: %w", err)
	}
	underlines, err := scene.Underlines()
	if err != nil {
		return fmt.Errorf("underlines: %w", err)
	}
	backdrops, err := scene.BackdropFilters()
	if err != nil {
		return fmt.Errorf("backdrops: %w", err)
	}
	boundaries, err := scene.FilterBoundaries()
	if err != nil {
		return fmt.Errorf("boundaries: %w", err)
	}
	surfaces, surfaceOpacities, err := scene.Surfaces()
	if err != nil {
		return fmt.Errorf("surfaces: %w", err)
	}
	meta, err := scene.Meta()
	if err != nil {
		return fmt.Errorf("meta: %w", err)
	}
	requirements, err := scene.Requirements()
	if err != nil {
		return fmt.Errorf("requirements: %w", err)
	}

	for batchIndex, command := range commands {
		switch command.Kind {
		case gpui.SceneCommandKindBatch:
			fields := []field{
				{"command", "batch"},
				{"batchKind", command.BatchKind.String()},
			}
			switch command.BatchKind {
			case gpui.SceneBatchShadows, gpui.SceneBatchQuads:
				fields = append(fields,
					field{"rangeStart", uint64(command.Range[0])},
					field{"rangeEnd", uint64(command.Range[1])},
					field{"smoothed", uint64(boolToU(command.Smoothed))},
				)
			case gpui.SceneBatchUnderlines, gpui.SceneBatchSurfaces, gpui.SceneBatchBackdropFilters:
				fields = append(fields,
					field{"rangeStart", uint64(command.Range[0])},
					field{"rangeEnd", uint64(command.Range[1])},
				)
			}
			rec.eventWith("scene-command", "", fields...)

			switch command.BatchKind {
			case gpui.SceneBatchQuads:
				for _, quad := range quads[command.Range[0]:command.Range[1]] {
					fields := commonOpFields("quad", quad.Order, batchIndex, quad.Bounds, quad.ContentMask)
					fields = append(fields, colorFields("background", quad.Background.Solid)...)
					fields = append(fields, colorFields("borderColor", quad.BorderColor.Solid)...)
					fields = append(fields, radiiFields("radius", quad.CornerRadii)...)
					fields = append(fields, widthsFields("border", quad.BorderWidths)...)
					borderStyle := "solid"
					if quad.BorderStyle == gpui.BorderStyleDashed {
						borderStyle = "dashed"
					}
					fields = append(fields,
						field{"borderStyle", borderStyle},
						field{"borderDashedLength", conformance.F32Bits(quad.DashedLength)},
						field{"borderDashedGap", conformance.F32Bits(quad.DashedGap)},
						field{"cornerSmoothing", conformance.F32Bits(quad.CornerSmoothing)},
						field{"padding", uint64(quad.Padding)},
					)
					rec.eventWith("scene-op", "", fields...)
				}
			case gpui.SceneBatchShadows:
				for _, shadow := range shadows[command.Range[0]:command.Range[1]] {
					fields := commonOpFields("shadow", shadow.Order, batchIndex, shadow.Bounds, shadow.ContentMask)
					fields = append(fields, field{"blurRadius", conformance.F32Bits(shadow.BlurRadius)})
					fields = append(fields, colorFields("color", shadow.Color.Solid)...)
					fields = append(fields, boundsFields("elementBounds", shadow.ElementBounds)...)
					fields = append(fields, radiiFields("elementRadius", shadow.ElementCornerRadii)...)
					fields = append(fields, radiiFields("radius", shadow.CornerRadii)...)
					fields = append(fields,
						field{"inset", uint64(boolToU(shadow.Inset))},
						field{"cornerSmoothing", conformance.F32Bits(shadow.CornerSmoothing)},
					)
					rec.eventWith("scene-op", "", fields...)
				}
			case gpui.SceneBatchUnderlines:
				for _, underline := range underlines[command.Range[0]:command.Range[1]] {
					fields := commonOpFields("underline", underline.Order, batchIndex, underline.Bounds, underline.ContentMask)
					fields = append(fields,
						field{"thickness", conformance.F32Bits(underline.Thickness)},
						field{"wavy", uint64(boolToU(underline.Wavy))},
					)
					fields = append(fields, colorFields("color", underline.Color)...)
					rec.eventWith("scene-op", "", fields...)
				}
			case gpui.SceneBatchSurfaces:
				for i, surface := range surfaces[command.Range[0]:command.Range[1]] {
					index := int(command.Range[0]) + i
					fields := commonOpFields("surface", surface.Order, batchIndex, surface.Bounds, surface.ContentMask)
					fields = append(fields,
						field{"sourceTag", uint64(surface.Source)},
						field{"opacity", conformance.F32Bits(surfaceOpacities[index])},
					)
					rec.eventWith("scene-op", "", fields...)
				}
			case gpui.SceneBatchBackdropFilters:
				for _, filter := range backdrops[command.Range[0]:command.Range[1]] {
					fields := filterOpFields("backdrop-filter", filter.Order, batchIndex, filter.Bounds, filter.ContentMask, filter.CornerRadii, filter.CornerSmoothing, filter.BlurRadius, filter.Opacity, false, false)
					rec.eventWith("scene-op", "", fields...)
				}
			}
		case gpui.SceneCommandKindBeginFilter:
			fields := []field{
				{"command", "begin-filter"},
				{"boundaryIndex", uint64(command.BoundaryIndex)},
			}
			fields = append(fields, targetFields(command.Target)...)
			rec.eventWith("scene-command", "", fields...)
			boundary := boundaries[command.BoundaryIndex]
			fields = filterOpFields("filter-boundary", boundary.Order, batchIndex, boundary.Bounds, boundary.ContentMask, boundary.CornerRadii, boundary.CornerSmoothing, boundary.BlurRadius, boundary.Opacity, boundary.IsStart, true)
			rec.eventWith("scene-op", "", fields...)
		case gpui.SceneCommandKindEndFilter:
			fields := []field{
				{"command", "end-filter"},
				{"boundaryIndex", uint64(command.BoundaryIndex)},
				{"closingBoundaryIndex", uint64(command.ClosingBoundaryIndex)},
			}
			fields = append(fields, targetFields(command.Target)...)
			rec.eventWith("scene-command", "", fields...)
			boundary := boundaries[command.ClosingBoundaryIndex]
			fields = filterOpFields("filter-boundary", boundary.Order, batchIndex, boundary.Bounds, boundary.ContentMask, boundary.CornerRadii, boundary.CornerSmoothing, boundary.BlurRadius, boundary.Opacity, boundary.IsStart, true)
			rec.eventWith("scene-op", "", fields...)
		}
	}

	rec.eventWith("scene-meta", "",
		field{"opCount", uint64(meta.OpCount)},
		field{"layerCount", uint64(layerCount)},
		field{"quadCount", uint64(meta.QuadCount)},
		field{"shadowCount", uint64(meta.ShadowCount)},
		field{"underlineCount", uint64(meta.UnderlineCount)},
		field{"backdropCount", uint64(meta.BackdropCount)},
		field{"boundaryCount", uint64(meta.BoundaryCount)},
		field{"surfaceCount", uint64(meta.SurfaceCount)},
		field{"commandCount", uint64(requirements.CommandCount)},
		field{"instanceBatchCount", uint64(requirements.InstanceBatchCount)},
		field{"pathRasterizationVertexCount", uint64(requirements.PathRasterizationVertexCount)},
		field{"pathSpriteCount", uint64(requirements.PathSpriteCount)},
		field{"isolatedFilterCount", uint64(requirements.IsolatedFilterCount)},
		field{"isolatedTargetCount", uint64(requirements.IsolatedTargetCount)},
		field{"usesOffscreenTarget", uint64(boolToU(requirements.UsesOffscreenTarget))},
		field{"usesPathTarget", uint64(boolToU(requirements.UsesPathTarget))},
	)
	return nil
}

// sceneHue reproduces the reference's public color read-back: the stored
// SceneHsla hue converted through palette's Hsla (h*360) and normalized
// (norm(h*360)/360). Both sides derive the emitted hue from the stored
// bits with this formula.
func sceneHue(h float32) float32 {
	return normHueDeg32(h*360) / 360
}

// normHueDeg32 is palette's normalize_unsigned_angle.
func normHueDeg32(x float32) float32 {
	normalized := x - float32(math.Floor(float64(x/360.0)))*360.0
	b := math.Float32bits(normalized)
	subnormal := b&0x7f800000 == 0 && b&0x007fffff != 0
	if subnormal || normalized >= 360.0 {
		return 0
	}
	return normalized
}

func colorFields(prefix string, c gpui.Hsla) []field {
	return []field{
		{prefix + "H", conformance.F32Bits(sceneHue(c.H))},
		{prefix + "S", conformance.F32Bits(c.S)},
		{prefix + "L", conformance.F32Bits(c.L)},
		{prefix + "A", conformance.F32Bits(c.A)},
	}
}

func boundsFields(prefix string, b gpui.Bounds) []field {
	return []field{
		{prefix + "X", conformance.F32Bits(b.Origin.X)},
		{prefix + "Y", conformance.F32Bits(b.Origin.Y)},
		{prefix + "W", conformance.F32Bits(b.Size.Width)},
		{prefix + "H", conformance.F32Bits(b.Size.Height)},
	}
}

func radiiFields(prefix string, c gpui.Corners) []field {
	return []field{
		{prefix + "TopLeft", conformance.F32Bits(c.TopLeft)},
		{prefix + "TopRight", conformance.F32Bits(c.TopRight)},
		{prefix + "BottomRight", conformance.F32Bits(c.BottomRight)},
		{prefix + "BottomLeft", conformance.F32Bits(c.BottomLeft)},
	}
}

func widthsFields(prefix string, e gpui.Edges) []field {
	return []field{
		{prefix + "Top", conformance.F32Bits(e.Top)},
		{prefix + "Right", conformance.F32Bits(e.Right)},
		{prefix + "Bottom", conformance.F32Bits(e.Bottom)},
		{prefix + "Left", conformance.F32Bits(e.Left)},
	}
}

func commonOpFields(kind string, order uint32, batchIndex int, bounds, mask gpui.Bounds) []field {
	fields := []field{
		{"kind", kind},
		{"order", uint64(order)},
		{"batchIndex", uint64(batchIndex)},
	}
	fields = append(fields, boundsFields("bounds", bounds)...)
	fields = append(fields, boundsFields("mask", mask)...)
	return fields
}

func targetFields(target gpui.FilterTarget) []field {
	if target.Isolated {
		return []field{
			{"target", "isolated"},
			{"targetIndex", uint64(target.Index)},
		}
	}
	return []field{{"target", "inline"}}
}

func filterOpFields(
	kind string,
	order uint32,
	batchIndex int,
	bounds, mask gpui.Bounds,
	radii gpui.Corners,
	smoothing, blur, opacity float32,
	isStart, includeIsStart bool,
) []field {
	fields := commonOpFields(kind, order, batchIndex, bounds, mask)
	fields = append(fields, radiiFields("radius", radii)...)
	fields = append(fields,
		field{"cornerSmoothing", conformance.F32Bits(smoothing)},
		field{"blurRadius", conformance.F32Bits(blur)},
		field{"opacity", conformance.F32Bits(opacity)},
	)
	if includeIsStart {
		fields = append(fields, field{"isStart", uint64(boolToU(isStart))})
	}
	return fields
}
