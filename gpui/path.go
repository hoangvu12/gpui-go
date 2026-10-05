package gpui

import (
	"fmt"
	"math"

	"gpui-go/internal/native"
)

// This file is the port's path half of ticket16: the PathBuilder API of
// the pin (crates/gpui/src/path_builder.rs at 254b5dbd…) over the
// native scene service's `path_script` tessellation entry, plus the
// Path record type and the pinned Window::paint_path record
// construction.
//
// The builder records the pinned command script (move/line/curve/cubic/
// arc/polygon/close, the style and dash setters, the transform
// mutators); Build() drives the NATIVE tessellation — the pinned
// `gpui::PathBuilder` itself (the same lyon code the reference oracle
// drives), so the vertex output is bit-identical to the pin's. A Go
// reimplementation of the tessellator would be a from-scratch
// approximation with no bit-exactness guarantee; the native service IS
// the pinned code. See reference/native/SCENE_ABI.md (`path_script`).
//
// Units: the builder works in LOGICAL pixels (Path<Pixels> in the pin);
// PaintPath scales by the paint context's factor (the pinned
// `path.scale(scale_factor)`) before inserting into the device-pixel
// scene.

// ---------------------------------------------------------------------------
// Style records (the pinned PathStyle / lyon options)
// ---------------------------------------------------------------------------

// LineCap is the stroke cap style (the pinned lyon LineCap).
type LineCap uint32

// Line cap values.
const (
	// LineCapButt ends the stroke exactly at the end point.
	LineCapButt LineCap = 0
	// LineCapSquare extends the stroke by half the line width.
	LineCapSquare LineCap = 1
	// LineCapRound caps the stroke with a half-circle.
	LineCapRound LineCap = 2
)

// LineJoin is the stroke join style (the pinned lyon LineJoin).
type LineJoin uint32

// Line join values.
const (
	// LineJoinMiter joins strokes with a miter.
	LineJoinMiter LineJoin = 0
	// LineJoinRound joins strokes with a round corner.
	LineJoinRound LineJoin = 1
	// LineJoinBevel joins strokes with a beveled corner.
	LineJoinBevel LineJoin = 2
)

// FillRule is the fill rule (the pinned lyon FillRule).
type FillRule uint32

// Fill rule values.
const (
	// FillRuleEvenOdd is the SVG even-odd rule.
	FillRuleEvenOdd FillRule = 0
	// FillRuleNonZero is the SVG non-zero rule.
	FillRuleNonZero FillRule = 1
)

// FillOptions mirrors the pinned lyon FillOptions.
type FillOptions struct {
	// Tolerance is the maximum allowed distance to the path when
	// building an approximation (the lyon default is 0.1).
	Tolerance float32
	// FillRule selects even-odd or non-zero.
	FillRule FillRule
	// SweepOrientation is the tessellation traversal orientation.
	SweepOrientation Orientation
	// HandleIntersections enables the self-intersection handling (the
	// lyon default is true).
	HandleIntersections bool
}

// DefaultFillOptions returns the lyon defaults (tolerance 0.1,
// even-odd, vertical sweep, intersections handled).
func DefaultFillOptions() FillOptions {
	return FillOptions{
		Tolerance:           0.1,
		FillRule:            FillRuleEvenOdd,
		SweepOrientation:    OrientationVertical,
		HandleIntersections: true,
	}
}

// Orientation is the fill tessellation traversal orientation.
type Orientation uint32

// Orientation values.
const (
	// OrientationVertical traverses the geometry vertically.
	OrientationVertical Orientation = 0
	// OrientationHorizontal traverses the geometry horizontally.
	OrientationHorizontal Orientation = 1
)

// StrokeOptions mirrors the pinned lyon StrokeOptions.
type StrokeOptions struct {
	// StartCap is the cap at the start of each sub-path (butt).
	StartCap LineCap
	// EndCap is the cap at the end of each sub-path (butt).
	EndCap LineCap
	// LineJoin is the join style (miter).
	LineJoin LineJoin
	// LineWidth of the stroke.
	LineWidth float32
	// MiterLimit must be >= 1 (the lyon default is 4.0).
	MiterLimit float32
}

// DefaultStrokeOptions returns the lyon defaults (butt caps, miter
// joins, line width 1.0, miter limit 4.0).
func DefaultStrokeOptions() StrokeOptions {
	return StrokeOptions{
		StartCap:   LineCapButt,
		EndCap:     LineCapButt,
		LineJoin:   LineJoinMiter,
		LineWidth:  1.0,
		MiterLimit: 4.0,
	}
}

// PathStyle mirrors the pinned PathStyle: fill or stroke with options.
type PathStyle struct {
	// Fill is set when the style kind is fill.
	Fill FillOptions
	// Stroke is set when the style kind is stroke.
	Stroke StrokeOptions
	// IsStroke selects the stroke variant.
	IsStroke bool
}

// FillStyle returns a fill style with the given options.
func FillStyle(options FillOptions) PathStyle {
	return PathStyle{Fill: options}
}

// StrokeStyle returns a stroke style with the given options.
func StrokeStyle(options StrokeOptions) PathStyle {
	return PathStyle{Stroke: options, IsStroke: true}
}

// ---------------------------------------------------------------------------
// The tessellated path (the pinned Path<Pixels>)
// ---------------------------------------------------------------------------

// PathVertex is one tessellated path vertex (the pinned PathVertex's
// xy position and st texture coordinate; the pinned per-vertex content
// mask is always the default and unused by the rasterization path).
type PathVertex struct {
	// X, Y are the vertex position in the path's units (logical pixels
	// from the builder, device pixels after PaintPath's scaling).
	X, Y float32
	// S, T are the vertex texture coordinates (the pinned constant
	// (0, 1) for tessellated paths).
	S, T float32
}

// Path is a tessellated path: the triangle list plus its bounds (the
// pinned Path<Pixels> the builder returns). Logical pixels from
// PathBuilder.Build.
type Path struct {
	// Vertices are the tessellated triangles (three per triangle).
	Vertices []PathVertex
	// Bounds is the union of the path's triangles ((0,0,0,0) for an
	// empty tessellation — the pinned build_path fallback).
	Bounds Bounds
}

// VertexCount returns the vertex count (the rasterization vertex count
// of the plan).
func (p *Path) VertexCount() int { return len(p.Vertices) }

// ---------------------------------------------------------------------------
// The path builder (the pinned PathBuilder public API)
// ---------------------------------------------------------------------------

// PathBuilder builds a tessellated Path through the native scene
// service's pinned PathBuilder port (the same lyon code the reference
// oracle drives). The zero value is a fill builder with default
// options, like the pin's PathBuilder::default; NewPathBuilder is the
// preferred constructor.
type PathBuilder struct {
	commands []native.PathCommandRecord
	// words is the shared pool: point pairs for polygons and dash
	// lengths.
	words []uint32
	// style/dash are the setter state applied at Build (the pin's
	// with_style/dash_array setters).
	style       PathStyle
	styleSet    bool
	dashLengths []float32
}

// NewPathBuilder returns a fill builder with default options (the
// pinned PathBuilder::fill()).
func NewPathBuilder() *PathBuilder {
	return &PathBuilder{}
}

// FillPathBuilder returns a fill builder (the pinned PathBuilder::fill()).
func FillPathBuilder() *PathBuilder { return NewPathBuilder() }

// StrokePathBuilder returns a stroke builder with the given line width
// (the pinned PathBuilder::stroke(width), other options at the lyon
// defaults).
func StrokePathBuilder(width float32) *PathBuilder {
	options := DefaultStrokeOptions()
	options.LineWidth = width
	return &PathBuilder{style: StrokeStyle(options), styleSet: true}
}

// WithStyle sets the builder's style (the pinned with_style).
func (b *PathBuilder) WithStyle(style PathStyle) *PathBuilder {
	b.style = style
	b.styleSet = true
	return b
}

// DashArray sets the stroke dash array (the pinned dash_array: an odd
// count is repeated to an even one — 5,3,2 behaves as 5,3,2,5,3,2).
func (b *PathBuilder) DashArray(lengths []float32) *PathBuilder {
	if len(lengths)%2 == 1 {
		repeated := make([]float32, 0, len(lengths)*2)
		repeated = append(repeated, lengths...)
		repeated = append(repeated, lengths...)
		lengths = repeated
	}
	b.dashLengths = lengths
	return b
}

// MoveTo moves the current point (the pinned move_to).
func (b *PathBuilder) MoveTo(x, y float32) *PathBuilder {
	cmd := native.NewPathCommandRecord(native.ScenePathCmdMoveTo)
	cmd.SetXY(0, x, y)
	b.commands = append(b.commands, cmd)
	return b
}

// LineTo draws a straight line to the point (the pinned line_to).
func (b *PathBuilder) LineTo(x, y float32) *PathBuilder {
	cmd := native.NewPathCommandRecord(native.ScenePathCmdLineTo)
	cmd.SetXY(0, x, y)
	b.commands = append(b.commands, cmd)
	return b
}

// CurveTo draws a quadratic Bézier to (x, y) with the control point
// (ctrlX, ctrlY) (the pinned curve_to(to, ctrl)).
func (b *PathBuilder) CurveTo(x, y, ctrlX, ctrlY float32) *PathBuilder {
	cmd := native.NewPathCommandRecord(native.ScenePathCmdCurveTo)
	cmd.SetXY(0, x, y)
	cmd.SetXY(2, ctrlX, ctrlY)
	b.commands = append(b.commands, cmd)
	return b
}

// CubicBezierTo draws a cubic Bézier to (x, y) with the control points
// (ax, ay) and (bx, by) (the pinned cubic_bezier_to).
func (b *PathBuilder) CubicBezierTo(x, y, ax, ay, bx, by float32) *PathBuilder {
	cmd := native.NewPathCommandRecord(native.ScenePathCmdCubicBezierTo)
	cmd.SetXY(0, x, y)
	cmd.SetXY(2, ax, ay)
	cmd.SetXY(4, bx, by)
	b.commands = append(b.commands, cmd)
	return b
}

// ArcTo adds an elliptical arc (the pinned arc_to: radii, the x
// rotation in DEGREES, the large-arc and sweep flags, and the end
// point).
func (b *PathBuilder) ArcTo(radiusX, radiusY, xRotation float32, largeArc, sweep bool, x, y float32) *PathBuilder {
	cmd := native.NewPathCommandRecord(native.ScenePathCmdArcTo)
	cmd.SetXY(0, radiusX, radiusY)
	cmd.SetFloat(2, xRotation)
	cmd.Data[3] = boolToWord(largeArc)
	cmd.Data[4] = boolToWord(sweep)
	cmd.SetXY(5, x, y)
	b.commands = append(b.commands, cmd)
	return b
}

// RelativeArcTo adds an elliptical arc in relative coordinates (the
// pinned relative_arc_to).
func (b *PathBuilder) RelativeArcTo(radiusX, radiusY, xRotation float32, largeArc, sweep bool, x, y float32) *PathBuilder {
	cmd := native.NewPathCommandRecord(native.ScenePathCmdRelativeArcTo)
	cmd.SetXY(0, radiusX, radiusY)
	cmd.SetFloat(2, xRotation)
	cmd.Data[3] = boolToWord(largeArc)
	cmd.Data[4] = boolToWord(sweep)
	cmd.SetXY(5, x, y)
	b.commands = append(b.commands, cmd)
	return b
}

// AddPolygon adds a closed or open polygon (the pinned add_polygon).
func (b *PathBuilder) AddPolygon(points []Point, closed bool) *PathBuilder {
	first := uint32(len(b.words))
	for _, p := range points {
		b.words = append(b.words, math.Float32bits(p.X), math.Float32bits(p.Y))
	}
	cmd := native.NewPathCommandRecord(native.ScenePathCmdPolygon)
	cmd.Data[0] = first
	cmd.Data[1] = uint32(len(points))
	cmd.Data[2] = boolToWord(closed)
	b.commands = append(b.commands, cmd)
	return b
}

// Close closes the current sub-path (the pinned close).
func (b *PathBuilder) Close() *PathBuilder {
	cmd := native.NewPathCommandRecord(native.ScenePathCmdClose)
	b.commands = append(b.commands, cmd)
	return b
}

// Translate applies a translation (the pinned translate: composes with
// any accumulated transform).
func (b *PathBuilder) Translate(x, y float32) *PathBuilder {
	cmd := native.NewPathCommandRecord(native.ScenePathCmdTranslate)
	cmd.SetXY(0, x, y)
	b.commands = append(b.commands, cmd)
	return b
}

// Scale applies a uniform scale (the pinned scale).
func (b *PathBuilder) Scale(factor float32) *PathBuilder {
	cmd := native.NewPathCommandRecord(native.ScenePathCmdScale)
	cmd.SetFloat(0, factor)
	b.commands = append(b.commands, cmd)
	return b
}

// Rotate applies a rotation in degrees (the pinned rotate: 0..360).
func (b *PathBuilder) Rotate(degrees float32) *PathBuilder {
	cmd := native.NewPathCommandRecord(native.ScenePathCmdRotate)
	cmd.SetFloat(0, degrees)
	b.commands = append(b.commands, cmd)
	return b
}

// Transform replaces the accumulated transform with a row-major 2x2
// matrix plus translation (the pinned transform).
func (b *PathBuilder) Transform(m00, m01, m10, m11, tx, ty float32) *PathBuilder {
	cmd := native.NewPathCommandRecord(native.ScenePathCmdTransform)
	cmd.SetFloat(0, m00)
	cmd.SetFloat(1, m01)
	cmd.SetFloat(2, m10)
	cmd.SetFloat(3, m11)
	cmd.SetFloat(4, tx)
	cmd.SetFloat(5, ty)
	b.commands = append(b.commands, cmd)
	return b
}

func boolToWord(value bool) uint32 {
	if value {
		return 1
	}
	return 0
}

// Build tessellates the script through the native pinned PathBuilder
// and returns the path (the pinned PathBuilder::build): the vertices
// are the tessellated triangles (st constants (0, 1)), the bounds the
// union of the triangles, or (0,0,0,0) for an empty tessellation.
func (b *PathBuilder) Build() (*Path, error) {
	commands := b.commands
	if b.styleSet {
		style := native.NewPathCommandRecord(native.ScenePathCmdStyle)
		if b.style.IsStroke {
			style.Data[0] = native.ScenePathStyleStroke
			style.SetFloat(1, b.style.Stroke.LineWidth)
			style.Data[2] = uint32(b.style.Stroke.StartCap)
			style.Data[3] = uint32(b.style.Stroke.EndCap)
			style.Data[4] = uint32(b.style.Stroke.LineJoin)
			style.SetFloat(5, b.style.Stroke.MiterLimit)
		} else {
			style.Data[0] = native.ScenePathStyleFill
			style.SetFloat(1, b.style.Fill.Tolerance)
			style.Data[2] = uint32(b.style.Fill.FillRule)
			style.Data[3] = uint32(b.style.Fill.SweepOrientation)
			style.Data[4] = boolToWord(b.style.Fill.HandleIntersections)
		}
		// The style is a builder setter: replay it first.
		commands = append([]native.PathCommandRecord{style}, b.commands...)
	}
	words := b.words
	if len(b.dashLengths) > 0 {
		dash := native.NewPathCommandRecord(native.ScenePathCmdDash)
		dash.Data[0] = uint32(len(words))
		dash.Data[1] = uint32(len(b.dashLengths))
		for _, length := range b.dashLengths {
			words = append(words, math.Float32bits(length))
		}
		commands = append(commands, dash)
	}

	svc, err := sceneService()
	if err != nil {
		return nil, err
	}
	vertices, bounds, err := svc.TessellatePath(commands, words)
	if err != nil {
		return nil, fmt.Errorf("gpui: PathBuilder.Build: %w", err)
	}
	path := &Path{Bounds: boundsRecordOf(bounds)}
	path.Vertices = make([]PathVertex, len(vertices))
	for i, vertex := range vertices {
		path.Vertices[i] = PathVertex{
			X: math.Float32frombits(vertex.XYX),
			Y: math.Float32frombits(vertex.XYY),
			S: math.Float32frombits(vertex.STU),
			T: math.Float32frombits(vertex.STV),
		}
	}
	return path, nil
}

func boundsRecordOf(b native.SceneBounds) Bounds {
	x, y, w, h := b.Rect()
	return Bounds{Origin: Point{X: x, Y: y}, Size: Size{Width: w, Height: h}}
}

// ---------------------------------------------------------------------------
// The pinned paint_path record construction
// ---------------------------------------------------------------------------

// InsertPath inserts a pre-tessellated path in DEVICE pixels (the
// low-level seam PaintPath builds on): the bounds, content mask and
// vertex positions are already scaled.
func (s *Scene) InsertPath(path *Path, color Background, contentMask Bounds) error {
	if path == nil {
		return fmt.Errorf("gpui: InsertPath: nil path")
	}
	return s.kernel.InsertPath(pathRecordOf(path, color, contentMask))
}

// PaintPath paints the tessellated path at the current z-index (the
// pinned Window::paint_path, window.rs:4715): the path's content mask
// is the paint context's (UNSNAPPED, per the pin), the color carries
// the element opacity, and the geometry scales by the context's factor
// before insertion (the pinned path.scale(scale_factor)).
func (s *Scene) PaintPath(path *Path, color Background, ctx *PaintContext) error {
	if ctx == nil {
		return fmt.Errorf("gpui: PaintPath: nil paint context")
	}
	if path == nil {
		return fmt.Errorf("gpui: PaintPath: nil path")
	}
	scaled := scalePath(path, ctx.ScaleFactor)
	mask := scaleBounds(ctx.Mask, ctx.ScaleFactor)
	shaded := Background{Solid: OpacityHsla(color.Solid, ctx.ElementOpacity)}
	return s.InsertPath(scaled, shaded, mask)
}

// scalePath scales a logical-pixel path into device pixels (the pinned
// Path::scale: bounds, per-vertex xy positions and the content mask
// scale; the st coordinates do not).
func scalePath(path *Path, factor float32) *Path {
	scaled := &Path{
		Bounds:   scaleBounds(path.Bounds, factor),
		Vertices: make([]PathVertex, len(path.Vertices)),
	}
	for i, vertex := range path.Vertices {
		scaled.Vertices[i] = PathVertex{
			X: vertex.X * factor,
			Y: vertex.Y * factor,
			S: vertex.S,
			T: vertex.T,
		}
	}
	return scaled
}

// scaleBounds multiplies a bounds' components by a factor (the pinned
// Bounds::scale).
func scaleBounds(bounds Bounds, factor float32) Bounds {
	return Bounds{
		Origin: Point{X: bounds.Origin.X * factor, Y: bounds.Origin.Y * factor},
		Size:   Size{Width: bounds.Size.Width * factor, Height: bounds.Size.Height * factor},
	}
}

func pathRecordOf(path *Path, color Background, contentMask Bounds) *PathRecord {
	return &PathRecord{
		Bounds:      path.Bounds,
		ContentMask: contentMask,
		Color:       color.Solid,
		Vertices:    path.Vertices,
	}
}

// ---------------------------------------------------------------------------
// The ABI path record (device pixels)
// ---------------------------------------------------------------------------

// PathRecord is the device-pixel path record inserted into the kernel
// (the pinned Path<ScaledPixels> scene primitive: the tessellated
// triangle list plus the record fields the kernel and renderer
// consume). Built by PaintPath; Order is kernel-assigned.
type PathRecord struct {
	// Order is the draw order (kernel-assigned on insert).
	Order uint32
	// Bounds is the union of the path's triangles, device pixels.
	Bounds Bounds
	// ContentMask is the clip bounds, device pixels.
	ContentMask Bounds
	// Color is the path fill color.
	Color Hsla
	// Vertices are the tessellated triangles (xy device pixels, st the
	// pinned constant (0, 1)).
	Vertices []PathVertex
}

// VertexCount returns the vertex count.
func (r *PathRecord) VertexCount() int { return len(r.Vertices) }

// InsertPath inserts one device-pixel path record (the pinned
// Scene::insert_primitive(Primitive::Path(path))).
func (k *SceneKernel) InsertPath(rec *PathRecord) error {
	if err := k.checkLive("InsertPath"); err != nil {
		return err
	}
	if rec.Order != 0 {
		return fmt.Errorf("gpui: scene insert path: record order must be zero, got %d (the kernel assigns the draw order)", rec.Order)
	}
	if len(rec.Vertices)%3 != 0 {
		return fmt.Errorf("gpui: scene insert path: vertex count %d is not a multiple of 3 (the tessellated triangle list)", len(rec.Vertices))
	}
	nativeRec := native.NewScenePathRecord()
	nativeRec.Bounds = sceneBoundsRecord(rec.Bounds)
	nativeRec.ContentMask = sceneBoundsRecord(rec.ContentMask)
	nativeRec.Color = sceneColorRecord(rec.Color)
	nativeRec.VertexCount = uint32(len(rec.Vertices))
	vertices := make([]native.ScenePathVertexRecord, len(rec.Vertices))
	for i, vertex := range rec.Vertices {
		vertices[i] = native.ScenePathVertexOf(vertex.X, vertex.Y, vertex.S, vertex.T)
	}
	if err := k.scene.InsertPath(&nativeRec, vertices); err != nil {
		return fmt.Errorf("gpui: scene insert path: %w", err)
	}
	return nil
}

// Paths returns the finished scene's path records with their
// kernel-assigned orders (the pinned scene.paths), with the vertices
// concatenated per record. Requires Finish.
func (k *SceneKernel) Paths() ([]PathRecord, [][]PathVertex, error) {
	if err := k.checkLive("Paths"); err != nil {
		return nil, nil, err
	}
	records, vertices, err := k.scene.PathDump()
	if err != nil {
		return nil, nil, fmt.Errorf("gpui: scene path dump: %w", err)
	}
	outRecords := make([]PathRecord, len(records))
	outVertices := make([][]PathVertex, len(records))
	cursor := 0
	for i, rec := range records {
		if int(rec.VertexCount) > len(vertices)-cursor {
			return nil, nil, fmt.Errorf("gpui: scene path dump: record %d claims %d vertices, %d remain", i, rec.VertexCount, len(vertices)-cursor)
		}
		outVertices[i] = make([]PathVertex, rec.VertexCount)
		for j := 0; j < int(rec.VertexCount); j++ {
			vertex := vertices[cursor+j]
			outVertices[i][j] = PathVertex{
				X: math.Float32frombits(vertex.XYX),
				Y: math.Float32frombits(vertex.XYY),
				S: math.Float32frombits(vertex.STU),
				T: math.Float32frombits(vertex.STV),
			}
		}
		cursor += int(rec.VertexCount)
		x, y, w, height := rec.Bounds.Rect()
		maskX, maskY, maskW, maskH := rec.ContentMask.Rect()
		hue, sat, light, alpha := rec.Color.HSLA()
		outRecords[i] = PathRecord{
			Order:       rec.Order,
			Bounds:      Bounds{Origin: Point{X: x, Y: y}, Size: Size{Width: w, Height: height}},
			ContentMask: Bounds{Origin: Point{X: maskX, Y: maskY}, Size: Size{Width: maskW, Height: maskH}},
			Color:       Hsla{H: hue, S: sat, L: light, A: alpha},
		}
	}
	return outRecords, outVertices, nil
}
