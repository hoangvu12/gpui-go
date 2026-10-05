package portfixture

// This file implements the paths-filters-v1 fixture runner (ticket16):
// one case at a time, each in its own window-equivalent scope (a fresh
// layout engine for the style tree, mirroring the reference's one
// TestAppContext window per case), then the paint script executed
// against the port's real Scene (the native scene kernel behind
// internal/native) through the port's paint API — the path builder
// scripts through the native pinned PathBuilder tessellation
// (gpui.PathBuilder → scene service v3 path_script), the sprite
// records, the surface records and the nested filter groups — then the
// finished scene's plan, primitive arrays and path vertices dumped and
// recorded as the same scene-command/scene-op/path-vertex/scene-meta
// trace events the reference harness records
// (reference/harness/src/fixtures/paths_filters.rs, run_case op for
// op).
//
// The gate (paths_filters_test.go) compares the port trace against the
// recorded reference trace with CompareTraces: every event must match
// exactly, including every f32 bit pattern of every vertex, bound,
// mask and color, every draw order and every plan command (batch
// ranges, vertex and sprite counts, filter-target assignments).

import (
	"fmt"
	"math"

	"gpui-go/conformance"
	"gpui-go/gpui"
	"gpui-go/internal/native"
)

// RunPathsFilters executes the envelope's paths-filters cases against
// the real gpui scene kernel and returns the port-side trace.
func RunPathsFilters(envelope *conformance.Envelope) (*conformance.Trace, error) {
	if envelope == nil {
		return nil, fmt.Errorf("portfixture: nil envelope")
	}
	if envelope.FixtureKind != conformance.FixtureKindPathsFilters {
		return nil, fmt.Errorf("portfixture: unsupported fixture kind %q", envelope.FixtureKind)
	}
	inputs := envelope.Inputs.PathFilterCases
	if inputs == nil {
		return nil, fmt.Errorf("portfixture: fixture kind paths-filters-v1 requires inputs.path_filter_cases")
	}

	rec := &recorder{}
	// The finished scenes stay pinned for later replay ranges until the
	// run ends (native handles do not follow Go garbage collection).
	scenes := make(map[string]*gpui.Scene)
	defer func() {
		for _, scene := range scenes {
			_ = scene.Dispose()
		}
	}()
	for i := range inputs.Cases {
		if err := runPathsFiltersCase(&inputs.Cases[i], rec, scenes); err != nil {
			return nil, fmt.Errorf("paths-filters case %q failed: %w", inputs.Cases[i].Label, err)
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

// pathFilterState is the paint script's execution state.
type pathFilterState struct {
	scene *gpui.Scene
	ctx   *gpui.PaintContext
	// layerCount counts the actual push_layer calls (the scene-meta
	// layerCount observable; this fixture family paints no layers, the
	// field keeps the meta emission uniform).
	layerCount uint32
	// pendingGroups tracks open content-filter groups; identity groups
	// (nil entry) run inline and their end is a no-op.
	pendingGroups []*conformance.PathFilterOp
}

// runPathsFiltersCase executes one case, mirroring the reference
// run_case: case-begin, the layout section, the paint script against a
// fresh scene, the finished scene recorded, case-end.
func runPathsFiltersCase(c *conformance.PathFilterCase, rec *recorder, scenes map[string]*gpui.Scene) error {
	if c.StyleTree == nil {
		return fmt.Errorf("case %q has no style tree", c.Label)
	}

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

	scene, err := gpui.NewScene()
	if err != nil {
		return fmt.Errorf("scene: %w", err)
	}
	defer func() {
		if _, ok := scenes[c.Label]; !ok {
			_ = scene.Dispose()
		}
	}()

	defaultMask := gpui.Bounds{
		Origin: gpui.Point{},
		Size:   gpui.Size{Width: float32(c.AvailableSpace.Width), Height: float32(c.AvailableSpace.Height)},
	}
	state := &pathFilterState{
		scene: scene,
		ctx:   gpui.NewPaintContext(scale, defaultMask, 1.0),
	}

	for i := range c.PaintScript {
		op := &c.PaintScript[i]
		if err := runPathFilterOp(state, op, boundsByLabel, defaultMask, scenes); err != nil {
			return fmt.Errorf("paint op %s: %w", op.Op, err)
		}
	}
	if len(state.pendingGroups) != 0 {
		return fmt.Errorf("unclosed content-filter group(s) at the end of the paint script")
	}
	if err := scene.Finish(); err != nil {
		return fmt.Errorf("scene finish: %w", err)
	}

	if err := recordPathsFiltersScene(scene, rec, state.layerCount); err != nil {
		return fmt.Errorf("recording scene: %w", err)
	}

	scenes[c.Label] = scene
	rec.event("case-end", c.Label)
	return nil
}

// runPathFilterOp executes one paths-filters paint op against the
// port's scene, mirroring the reference fixture's op handling.
func runPathFilterOp(state *pathFilterState, op *conformance.PathFilterOp, boundsByLabel map[string]gpui.Bounds, defaultMask gpui.Bounds, scenes map[string]*gpui.Scene) error {
	nodeBounds := func(label string) (gpui.Bounds, error) {
		bounds, ok := boundsByLabel[label]
		if !ok {
			return gpui.Bounds{}, fmt.Errorf("paint script references unknown node label %q", label)
		}
		return bounds, nil
	}
	rectOf := func(r conformance.RectIn) gpui.Bounds {
		return gpui.Bounds{
			Origin: gpui.Point{X: float32(r.X), Y: float32(r.Y)},
			Size:   gpui.Size{Width: float32(r.Width), Height: float32(r.Height)},
		}
	}
	maskOf := func(m *conformance.RectIn) gpui.Bounds {
		if m == nil {
			return defaultMask
		}
		return rectOf(*m)
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
	tileOf := func(t conformance.TileIn) gpui.AtlasTile {
		return gpui.AtlasTile{
			TextureIndex: t.TextureIndex,
			TextureKind:  gpui.AtlasTextureKind(t.TextureKind),
			TileID:       t.TileID,
			Padding:      t.Padding,
			BoundsX:      int32(t.Bounds.X),
			BoundsY:      int32(t.Bounds.Y),
			BoundsW:      int32(t.Bounds.Width),
			BoundsH:      int32(t.Bounds.Height),
		}
	}

	// opCtx: the per-op paint context (explicit mask or the case mask).
	opCtx := *state.ctx
	opCtx.Mask = maskOf(op.Mask)

	switch op.Op {
	case conformance.OpPaintBox2:
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
		// The reference fixture's PaintBox: transparent border, solid
		// style, the pinned default dash length/gap, background only.
		quad := gpui.PaintQuad{
			Bounds:      bounds,
			Background:  background,
			BorderColor: gpui.SolidBackground(gpui.TransparentBlack()),
			CornerRadii: cornersOf(op.CornerRadius, op.CornerRadii),
		}
		if err := state.scene.PaintQuad(quad, smoothingOf(op.CornerSmoothing), &opCtx); err != nil {
			return err
		}

	case conformance.OpPaintPath:
		color, err := colorOf(op.Color)
		if err != nil {
			return err
		}
		path, err := buildPathScript(op.Commands)
		if err != nil {
			return err
		}
		if err := state.scene.PaintPath(path, gpui.SolidBackground(color), &opCtx); err != nil {
			return err
		}

	case conformance.OpPaintMonochromeSprite, conformance.OpPaintSubpixelSprite:
		bounds, err := nodeBounds(op.Label)
		if err != nil {
			return err
		}
		color, err := colorOf(op.Color)
		if err != nil {
			return err
		}
		tile := tileOf(*op.Tile)
		spriteBounds := gpui.SnapBounds(bounds, state.ctx.ScaleFactor)
		mask := gpui.SnappedContentMask(opCtx.Mask, state.ctx.ScaleFactor)
		shaded := gpui.OpacityHsla(color, opCtx.ElementOpacity)
		if op.Op == conformance.OpPaintMonochromeSprite {
			if tile.TextureKind != gpui.AtlasTextureMonochrome {
				return fmt.Errorf("monochrome sprite tile kind = %d, want the monochrome pool", tile.TextureKind)
			}
			return state.scene.InsertMonochromeSprite(gpui.MonochromeSprite{
				Bounds:         spriteBounds,
				ContentMask:    mask,
				Color:          shaded,
				Tile:           tile,
				Transformation: gpui.UnitTransformation(),
			})
		}
		if tile.TextureKind != gpui.AtlasTextureSubpixel {
			return fmt.Errorf("subpixel sprite tile kind = %d, want the subpixel pool", tile.TextureKind)
		}
		return state.scene.InsertSubpixelSprite(gpui.SubpixelSprite{
			Bounds:         spriteBounds,
			ContentMask:    mask,
			Color:          shaded,
			Tile:           tile,
			Transformation: gpui.UnitTransformation(),
		})

	case conformance.OpPaintPolychromeSprite:
		bounds, err := nodeBounds(op.Label)
		if err != nil {
			return err
		}
		tile := tileOf(*op.Tile)
		if tile.TextureKind != gpui.AtlasTexturePolychrome {
			return fmt.Errorf("polychrome sprite tile kind = %d, want the polychrome pool", tile.TextureKind)
		}
		opacity := float32(1)
		if op.Opacity != nil {
			opacity = float32(*op.Opacity)
		}
		spriteBounds := gpui.SnapBounds(bounds, state.ctx.ScaleFactor)
		mask := gpui.SnappedContentMask(opCtx.Mask, state.ctx.ScaleFactor)
		radii := cornersOf(op.CornerRadius, nil)
		radii = gpui.Corners{
			TopLeft:     radii.TopLeft * state.ctx.ScaleFactor,
			TopRight:    radii.TopRight * state.ctx.ScaleFactor,
			BottomRight: radii.BottomRight * state.ctx.ScaleFactor,
			BottomLeft:  radii.BottomLeft * state.ctx.ScaleFactor,
		}
		return state.scene.InsertPolychromeSprite(gpui.PolychromeSprite{
			Opacity:         opacity * opCtx.ElementOpacity,
			CornerSmoothing: smoothingOf(op.CornerSmoothing),
			Bounds:          spriteBounds,
			ContentMask:     mask,
			CornerRadii:     radii,
			Tile:            tile,
		})

	case conformance.OpPaintSurface:
		bounds, err := nodeBounds(op.Label)
		if err != nil {
			return err
		}
		// The public insertion path: insert_primitive defaults the paired
		// opacity to 1.0.
		snappedBounds := gpui.SnapBounds(bounds, state.ctx.ScaleFactor)
		mask := gpui.SnappedContentMask(opCtx.Mask, state.ctx.ScaleFactor)
		return state.scene.InsertSurface(snappedBounds, mask, gpui.SurfaceSourceNone, 1.0)

	case conformance.OpPaintBackdrop2:
		bounds, err := nodeBounds(op.Label)
		if err != nil {
			return err
		}
		blur := float32(0)
		if op.BlurRadius != nil {
			blur = float32(*op.BlurRadius)
		}
		return state.scene.PaintBackdrop(bounds, blur, cornersOf(op.CornerRadius, nil), smoothingOf(op.CornerSmoothing), &opCtx)

	case conformance.OpBeginFilterGroup2:
		var bounds gpui.Bounds
		switch {
		case op.Label != "":
			var err error
			bounds, err = nodeBounds(op.Label)
			if err != nil {
				return err
			}
		case op.Bounds != nil:
			bounds = rectOf(*op.Bounds)
		default:
			return fmt.Errorf("begin-filter-group requires a label or explicit bounds")
		}
		blur := float32(0)
		if op.BlurRadius != nil {
			blur = float32(*op.BlurRadius)
		}
		group := &conformance.PathFilterOp{
			Op:              conformance.OpBeginFilterGroup2,
			Label:           op.Label,
			Bounds:          op.Bounds,
			BlurRadius:      op.BlurRadius,
			CornerRadius:    op.CornerRadius,
			CornerSmoothing: op.CornerSmoothing,
			Mask:            op.Mask,
		}
		if blur <= 0 {
			// Identity filters run inline (no markers).
			state.pendingGroups = append(state.pendingGroups, nil)
			return nil
		}
		if err := state.scene.BeginFilterGroup(bounds, blur, cornersOf(op.CornerRadius, nil), smoothingOf(op.CornerSmoothing), &opCtx); err != nil {
			return err
		}
		state.pendingGroups = append(state.pendingGroups, group)

	case conformance.OpEndFilterGroup2:
		if len(state.pendingGroups) == 0 {
			return fmt.Errorf("end-filter-group without a matching begin")
		}
		group := state.pendingGroups[len(state.pendingGroups)-1]
		state.pendingGroups = state.pendingGroups[:len(state.pendingGroups)-1]
		if group == nil {
			// Identity group: already ran inline.
			return nil
		}
		var bounds gpui.Bounds
		switch {
		case group.Label != "":
			var err error
			bounds, err = nodeBounds(group.Label)
			if err != nil {
				return err
			}
		case group.Bounds != nil:
			bounds = rectOf(*group.Bounds)
		}
		blur := float32(0)
		if group.BlurRadius != nil {
			blur = float32(*group.BlurRadius)
		}
		return state.scene.EndFilterGroup(bounds, blur, cornersOf(group.CornerRadius, nil), smoothingOf(group.CornerSmoothing), &opCtx)

	case conformance.OpRaiseFloor2:
		return state.scene.RaiseOrderFloor()

	case conformance.OpReplay2:
		prev, ok := scenes[op.Source]
		if !ok {
			return fmt.Errorf("replay references unknown source case %q", op.Source)
		}
		return state.scene.Replay(op.Start, op.End, prev)

	default:
		return fmt.Errorf("unknown paths-filters op %q", op.Op)
	}
	return nil
}

// buildPathScript converts the envelope's path command script into the
// native ABI command array (with the shared word pool) and drives the
// pinned PathBuilder tessellation.
func buildPathScript(commands []conformance.PathCommandIn) (*gpui.Path, error) {
	builder := gpui.NewPathBuilder()
	for i := range commands {
		cmd := &commands[i]
		f32Of := func(v *float64) float32 {
			if v == nil {
				return 0
			}
			return float32(*v)
		}
		switch cmd.Kind {
		case conformance.PathCmdMoveTo:
			builder.MoveTo(f32Of(cmd.X), f32Of(cmd.Y))
		case conformance.PathCmdLineTo:
			builder.LineTo(f32Of(cmd.X), f32Of(cmd.Y))
		case conformance.PathCmdCurveTo:
			builder.CurveTo(f32Of(cmd.ToX), f32Of(cmd.ToY), f32Of(cmd.CtrlX), f32Of(cmd.CtrlY))
		case conformance.PathCmdCubicTo:
			builder.CubicBezierTo(f32Of(cmd.ToX), f32Of(cmd.ToY), f32Of(cmd.AX), f32Of(cmd.AY), f32Of(cmd.BX), f32Of(cmd.BY))
		case conformance.PathCmdArcTo:
			builder.ArcTo(f32Of(cmd.RadiusX), f32Of(cmd.RadiusY), f32Of(cmd.XRotation), derefBool(cmd.LargeArc), derefBool(cmd.Sweep), f32Of(cmd.X), f32Of(cmd.Y))
		case conformance.PathCmdPolygon:
			points := make([]gpui.Point, len(cmd.Points))
			for j, p := range cmd.Points {
				points[j] = gpui.Point{X: float32(p[0]), Y: float32(p[1])}
			}
			builder.AddPolygon(points, derefBool(cmd.Closed))
		case conformance.PathCmdClose:
			builder.Close()
		case conformance.PathCmdStyle:
			switch cmd.Style {
			case "fill":
				options := gpui.DefaultFillOptions()
				if cmd.Tolerance != nil {
					options.Tolerance = f32Of(cmd.Tolerance)
				}
				switch cmd.FillRule {
				case "non-zero":
					options.FillRule = gpui.FillRuleNonZero
				default:
					options.FillRule = gpui.FillRuleEvenOdd
				}
				switch cmd.SweepOrientation {
				case "horizontal":
					options.SweepOrientation = gpui.OrientationHorizontal
				default:
					options.SweepOrientation = gpui.OrientationVertical
				}
				if cmd.HandleIntersections != nil {
					options.HandleIntersections = *cmd.HandleIntersections
				}
				builder = builder.WithStyle(gpui.FillStyle(options))
			case "stroke":
				options := gpui.DefaultStrokeOptions()
				if cmd.Width != nil {
					options.LineWidth = f32Of(cmd.Width)
				}
				options.StartCap = lineCapOf(cmd.StartCap)
				options.EndCap = lineCapOf(cmd.EndCap)
				switch cmd.LineJoin {
				case "round":
					options.LineJoin = gpui.LineJoinRound
				case "bevel":
					options.LineJoin = gpui.LineJoinBevel
				default:
					options.LineJoin = gpui.LineJoinMiter
				}
				if cmd.MiterLimit != nil {
					options.MiterLimit = f32Of(cmd.MiterLimit)
				}
				builder = builder.WithStyle(gpui.StrokeStyle(options))
			default:
				return nil, fmt.Errorf("unsupported path style %q", cmd.Style)
			}
		case conformance.PathCmdDash:
			lengths := make([]float32, len(cmd.Lengths))
			for j, length := range cmd.Lengths {
				lengths[j] = float32(length)
			}
			builder = builder.DashArray(lengths)
		case conformance.PathCmdTranslate:
			builder.Translate(f32Of(cmd.X), f32Of(cmd.Y))
		case conformance.PathCmdScale:
			builder.Scale(f32Of(cmd.Factor))
		case conformance.PathCmdRotate:
			builder.Rotate(f32Of(cmd.Degrees))
		default:
			return nil, fmt.Errorf("unknown path command kind %q", cmd.Kind)
		}
	}
	return builder.Build()
}

func lineCapOf(cap string) gpui.LineCap {
	switch cap {
	case "square":
		return gpui.LineCapSquare
	case "round":
		return gpui.LineCapRound
	default:
		return gpui.LineCapButt
	}
}

func derefBool(v *bool) bool {
	return v != nil && *v
}

// recordPathsFiltersScene emits the scene-command, scene-op,
// path-vertex and scene-meta events for the finished scene, in plan
// order (mirroring the reference record_scene).
func recordPathsFiltersScene(scene *gpui.Scene, rec *recorder, layerCount uint32) error {
	commands, err := scene.Commands()
	if err != nil {
		return fmt.Errorf("scene commands: %w", err)
	}
	quads, err := scene.Quads()
	if err != nil {
		return fmt.Errorf("scene quads: %w", err)
	}
	paths, pathVertices, err := scene.Paths()
	if err != nil {
		return fmt.Errorf("scene paths: %w", err)
	}
	mono, err := scene.MonochromeSprites()
	if err != nil {
		return fmt.Errorf("scene monochrome sprites: %w", err)
	}
	sub, err := scene.SubpixelSprites()
	if err != nil {
		return fmt.Errorf("scene subpixel sprites: %w", err)
	}
	poly, err := scene.PolychromeSprites()
	if err != nil {
		return fmt.Errorf("scene polychrome sprites: %w", err)
	}
	surfaces, opacities, err := scene.Surfaces()
	if err != nil {
		return fmt.Errorf("scene surfaces: %w", err)
	}
	backdrops, err := scene.BackdropFilters()
	if err != nil {
		return fmt.Errorf("scene backdrops: %w", err)
	}
	boundaries, err := scene.FilterBoundaries()
	if err != nil {
		return fmt.Errorf("scene boundaries: %w", err)
	}
	requirements, err := scene.Requirements()
	if err != nil {
		return fmt.Errorf("scene requirements: %w", err)
	}
	meta, err := scene.Meta()
	if err != nil {
		return fmt.Errorf("scene meta: %w", err)
	}

	spriteColorFields := func(prefix string, color gpui.Hsla) []field {
		return colorFields(prefix, color)
	}
	tileFields := func(prefix string, tile gpui.AtlasTile) []field {
		return []field{
			{prefix + "TextureIndex", uint64(tile.TextureIndex)},
			{prefix + "TextureKind", uint64(tile.TextureKind)},
			{prefix + "TileId", uint64(tile.TileID)},
			{prefix + "Padding", uint64(tile.Padding)},
			{prefix + "X", conformance.F32Bits(float32(tile.BoundsX))},
			{prefix + "Y", conformance.F32Bits(float32(tile.BoundsY))},
			{prefix + "W", conformance.F32Bits(float32(tile.BoundsW))},
			{prefix + "H", conformance.F32Bits(float32(tile.BoundsH))},
		}
	}

	for batchIndex, command := range commands {
		if command.Kind == gpui.SceneCommandKindBatch {
			var fields []field
			switch command.BatchKind {
			case gpui.SceneBatchShadows, gpui.SceneBatchQuads:
				fields = []field{
					{"command", "batch"},
					{"batchKind", sceneBatchKindName(command.BatchKind)},
					{"rangeStart", uint64(command.Range[0])},
					{"rangeEnd", uint64(command.Range[1])},
					{"smoothed", uint64(btoi(command.Smoothed))},
				}
			case gpui.SceneBatchPaths:
				fields = []field{
					{"command", "batch"},
					{"batchKind", sceneBatchKindName(command.BatchKind)},
					{"rangeStart", uint64(command.Range[0])},
					{"rangeEnd", uint64(command.Range[1])},
					{"rasterizationVertexCount", uint64(command.RasterizationVertexCount)},
					{"spriteCount", uint64(command.SpriteCount)},
				}
			case gpui.SceneBatchMonochrome, gpui.SceneBatchSubpixel, gpui.SceneBatchPolychrome:
				fields = []field{
					{"command", "batch"},
					{"batchKind", sceneBatchKindName(command.BatchKind)},
					{"rangeStart", uint64(command.Range[0])},
					{"rangeEnd", uint64(command.Range[1])},
					{"textureIndex", uint64(command.TextureIndex)},
				}
			default:
				fields = []field{
					{"command", "batch"},
					{"batchKind", sceneBatchKindName(command.BatchKind)},
					{"rangeStart", uint64(command.Range[0])},
					{"rangeEnd", uint64(command.Range[1])},
				}
			}
			rec.eventWith("scene-command", "", fields...)

			switch command.BatchKind {
			case gpui.SceneBatchQuads:
				for _, quad := range quads[command.Range[0]:command.Range[1]] {
					opFields := commonOpFields("quad", quad.Order, batchIndex, quad.Bounds, quad.ContentMask)
					opFields = append(opFields, spriteColorFields("background", quad.Background.Solid)...)
					opFields = append(opFields, radiiFields("radius", quad.CornerRadii)...)
					opFields = append(opFields, field{"cornerSmoothing", conformance.F32Bits(quad.CornerSmoothing)})
					rec.eventWith("scene-op", "", opFields...)
				}
			case gpui.SceneBatchPaths:
				for i, path := range paths[command.Range[0]:command.Range[1]] {
					vertices := pathVertices[int(command.Range[0])+i]
					opFields := commonOpFields("path", path.Order, batchIndex, path.Bounds, path.ContentMask)
					opFields = append(opFields, spriteColorFields("color", path.Color)...)
					opFields = append(opFields, field{"vertexCount", uint64(len(vertices))})
					// The pinned Path::id is the insertion index; this
					// fixture's paths sort stably in insertion order, so
					// the dumped position matches the id the reference
					// recorded.
					opFields = append(opFields, field{"pathId", uint64(int(command.Range[0]) + i)})
					rec.eventWith("scene-op", "", opFields...)
					for _, vertex := range vertices {
						rec.eventWith("path-vertex", "",
							field{"x", conformance.F32Bits(vertex.X)},
							field{"y", conformance.F32Bits(vertex.Y)},
							field{"s", conformance.F32Bits(vertex.S)},
							field{"t", conformance.F32Bits(vertex.T)},
						)
					}
				}
			case gpui.SceneBatchMonochrome:
				for _, sprite := range mono[command.Range[0]:command.Range[1]] {
					opFields := commonOpFields("monochrome-sprite", sprite.Order, batchIndex, sprite.Bounds, sprite.ContentMask)
					opFields = append(opFields, spriteColorFields("color", sprite.Color)...)
					opFields = append(opFields, tileFields("tile", sprite.Tile)...)
					opFields = append(opFields, transformationFieldsOf("transform", sprite.Transformation)...)
					rec.eventWith("scene-op", "", opFields...)
				}
			case gpui.SceneBatchSubpixel:
				for _, sprite := range sub[command.Range[0]:command.Range[1]] {
					opFields := commonOpFields("subpixel-sprite", sprite.Order, batchIndex, sprite.Bounds, sprite.ContentMask)
					opFields = append(opFields, spriteColorFields("color", sprite.Color)...)
					opFields = append(opFields, tileFields("tile", sprite.Tile)...)
					opFields = append(opFields, transformationFieldsOf("transform", sprite.Transformation)...)
					rec.eventWith("scene-op", "", opFields...)
				}
			case gpui.SceneBatchPolychrome:
				for _, sprite := range poly[command.Range[0]:command.Range[1]] {
					opFields := commonOpFields("polychrome-sprite", sprite.Order, batchIndex, sprite.Bounds, sprite.ContentMask)
					opFields = append(opFields, radiiFields("radius", sprite.CornerRadii)...)
					opFields = append(opFields, field{"grayscale", uint64(btoi(sprite.Grayscale))})
					opFields = append(opFields, field{"opacity", conformance.F32Bits(sprite.Opacity)})
					opFields = append(opFields, tileFields("tile", sprite.Tile)...)
					rec.eventWith("scene-op", "", opFields...)
				}
			case gpui.SceneBatchSurfaces:
				for i, surface := range surfaces[command.Range[0]:command.Range[1]] {
					opFields := commonOpFields("surface", surface.Order, batchIndex, surface.Bounds, surface.ContentMask)
					opFields = append(opFields, field{"sourceTag", uint64(0)})
					opFields = append(opFields, field{"opacity", conformance.F32Bits(opacities[int(command.Range[0])+i])})
					rec.eventWith("scene-op", "", opFields...)
				}
			case gpui.SceneBatchBackdropFilters:
				for _, filter := range backdrops[command.Range[0]:command.Range[1]] {
					opFields := filterOpFields2("backdrop-filter", filter, batchIndex)
					rec.eventWith("scene-op", "", opFields...)
				}
			}
		} else if command.Kind == gpui.SceneCommandKindBeginFilter || command.Kind == gpui.SceneCommandKindEndFilter {
			commandName := "begin-filter"
			if command.Kind == gpui.SceneCommandKindEndFilter {
				commandName = "end-filter"
			}
			fields := []field{
				{"command", commandName},
				{"boundaryIndex", uint64(command.BoundaryIndex)},
			}
			if command.Kind == gpui.SceneCommandKindEndFilter {
				fields = append(fields, field{"closingBoundaryIndex", uint64(command.ClosingBoundaryIndex)})
			}
			fields = append(fields, targetFields(command.Target)...)
			rec.eventWith("scene-command", "", fields...)

			boundaryIndex := command.BoundaryIndex
			if command.Kind == gpui.SceneCommandKindEndFilter {
				boundaryIndex = command.ClosingBoundaryIndex
			}
			boundary := boundaries[boundaryIndex]
			opFields := filterOpFields2("filter-boundary", boundary, batchIndex)
			opFields = append(opFields, field{"isStart", uint64(btoi(boundary.IsStart))})
			rec.eventWith("scene-op", "", opFields...)
		}
	}

	rec.eventWith("scene-meta", "",
		field{"opCount", uint64(meta.OpCount)},
		field{"layerCount", uint64(layerCount)},
		field{"quadCount", uint64(meta.QuadCount)},
		field{"shadowCount", uint64(meta.ShadowCount)},
		field{"underlineCount", uint64(meta.UnderlineCount)},
		field{"pathCount", uint64(meta.PathCount)},
		field{"backdropCount", uint64(meta.BackdropCount)},
		field{"boundaryCount", uint64(meta.BoundaryCount)},
		field{"surfaceCount", uint64(meta.SurfaceCount)},
		field{"monochromeSpriteCount", uint64(meta.MonochromeSpriteCount)},
		field{"subpixelSpriteCount", uint64(meta.SubpixelSpriteCount)},
		field{"polychromeSpriteCount", uint64(meta.PolychromeSpriteCount)},
		field{"commandCount", uint64(requirements.CommandCount)},
		field{"instanceBatchCount", uint64(requirements.InstanceBatchCount)},
		field{"pathRasterizationVertexCount", uint64(requirements.PathRasterizationVertexCount)},
		field{"pathSpriteCount", uint64(requirements.PathSpriteCount)},
		field{"isolatedFilterCount", uint64(requirements.IsolatedFilterCount)},
		field{"isolatedTargetCount", uint64(requirements.IsolatedTargetCount)},
		field{"usesOffscreenTarget", uint64(btoi(requirements.UsesOffscreenTarget))},
		field{"usesPathTarget", uint64(btoi(requirements.UsesPathTarget))},
	)
	return nil
}

func btoi(b bool) uint64 {
	if b {
		return 1
	}
	return 0
}

// sceneBatchKindName maps the batch kinds to the recorded spellings
// (the reference batch_kind_name).
func sceneBatchKindName(kind gpui.SceneBatchKind) string {
	switch kind {
	case gpui.SceneBatchShadows:
		return "shadows"
	case gpui.SceneBatchQuads:
		return "quads"
	case gpui.SceneBatchPaths:
		return "paths"
	case gpui.SceneBatchUnderlines:
		return "underlines"
	case gpui.SceneBatchMonochrome:
		return "monochrome-sprites"
	case gpui.SceneBatchSubpixel:
		return "subpixel-sprites"
	case gpui.SceneBatchPolychrome:
		return "polychrome-sprites"
	case gpui.SceneBatchSurfaces:
		return "surfaces"
	case gpui.SceneBatchBackdropFilters:
		return "backdrop-filters"
	default:
		return "filter-boundary"
	}
}

// filterOpFields2 is the paths-filters variant of filterOpFields
// (without the common-op prefix fields, which this runner builds per
// kind).
func filterOpFields2(kind string, filter interface{}, batchIndex int) []field {
	type filterLike struct {
		order        uint32
		bounds, mask gpui.Bounds
		radii        gpui.Corners
		smoothing    float32
		blur         float32
		opacity      float32
	}
	var f filterLike
	switch record := filter.(type) {
	case gpui.BackdropFilter:
		f = filterLike{order: record.Order, bounds: record.Bounds, mask: record.ContentMask, radii: record.CornerRadii, smoothing: record.CornerSmoothing, blur: record.BlurRadius, opacity: record.Opacity}
	case gpui.FilterBoundary:
		f = filterLike{order: record.Order, bounds: record.Bounds, mask: record.ContentMask, radii: record.CornerRadii, smoothing: record.CornerSmoothing, blur: record.BlurRadius, opacity: record.Opacity}
	default:
		return nil
	}
	fields := commonOpFields(kind, f.order, batchIndex, f.bounds, f.mask)
	fields = append(fields, radiiFields("radius", f.radii)...)
	fields = append(fields, field{"cornerSmoothing", conformance.F32Bits(f.smoothing)})
	fields = append(fields, field{"blurRadius", conformance.F32Bits(f.blur)})
	fields = append(fields, field{"opacity", conformance.F32Bits(f.opacity)})
	return fields
}

// transformationFieldsOf records the sprite transformation matrix (the
// reference transformation_fields).
func transformationFieldsOf(prefix string, transformation gpui.TransformationMatrix) []field {
	return []field{
		{prefix + "00", conformance.F32Bits(transformation.RotationScale[0][0])},
		{prefix + "01", conformance.F32Bits(transformation.RotationScale[0][1])},
		{prefix + "10", conformance.F32Bits(transformation.RotationScale[1][0])},
		{prefix + "11", conformance.F32Bits(transformation.RotationScale[1][1])},
		{prefix + "TX", conformance.F32Bits(transformation.Translation[0])},
		{prefix + "TY", conformance.F32Bits(transformation.Translation[1])},
	}
}

var _ = math.Float32bits
var _ = native.ScenePathCmdMoveTo
