package gpui

import "fmt"

// This file is browser-core ticket 57's mask/clip-path/blend styling:
// the CSS mask-image, clip-path and mix-blend-mode/isolation
// descriptions the Style carries, and the effect-group scopes the paint
// path brackets an element's box in (rev 16's group mechanism, ticket
// 50/52's native composites). The group order is Chromium's effect-tree
// order (paint_property_tree_builder.cc, 0d4dcd4f):
//
//	blend(opacity(mask(clip_path(filter(content)))))
//
// The element's effect node carries opacity AND blend together
// (state.opacity + state.blend_mode, :2285-2289); the mask node is its
// child (UpdateMask with the effect node as parent, :2392), the
// clip-path mask node is the mask's child (:2420), and the filter node
// is the content's (:2777-2784) — so one blend group carries the element
// opacity (never two nested groups), the mask and clip-path groups
// nest inside it, and the filter group (the existing rev-15 path) wraps
// the painted unit innermost. The backdrop-filter record paints outside
// every group, before the scopes open (paintBox's existing order).

// ---------------------------------------------------------------------------
// mask-image (css-masking-1 §mask-image)
// ---------------------------------------------------------------------------

// MaskImage is the element's mask description (CSS mask-image,
// browser-core ticket 57): a gradient mask whose alpha (or luminance,
// mask-mode: luminance) multiplies the element's rendered image — the
// whole box: decorations, border and children, like the opacity group
// (the pin has no mask; the R16 effect group's dst_in composite is the
// mask, rev 16 ticket 52).
//
// An IMAGE mask (mask-image: url(…)) is not expressible through this
// style yet: the native atlas mask stretches the WHOLE polychrome
// texture over the group bounds (ticket 52's recorded gap), so a mask
// image needs a dedicated texture per image — the scene seam
// (EffectGroup with MaskKindAtlas and the texture index) is the direct
// spelling until the atlas grows one.
type MaskImage struct {
	// Gradient is the mask's gradient (the CSS mask-image: <gradient>),
	// resolved against the element's border box at paint time — the
	// mask's alpha (or luminance) is the gradient's.
	Gradient CssGradient
	// Mode selects what the gradient contributes: the sample's alpha
	// (MaskModeAlpha, the CSS default for image masks) or its REC-601
	// luminance (MaskModeLuminance, CSS mask-mode: luminance).
	Mode MaskMode
}

// GradientMaskImage builds a gradient mask-image (CSS
// mask-image: <gradient>; the alpha form). The gradient is the same CSS
// description the background carries.
func GradientMaskImage(gradient CssGradient) *MaskImage {
	return &MaskImage{Gradient: gradient}
}

// LuminanceMaskImage switches the mask to mask-mode: luminance (the
// mask's brightness, not its alpha, shapes the element).
func (m *MaskImage) LuminanceMaskImage() *MaskImage {
	if m == nil {
		return nil
	}
	out := *m
	out.Mode = MaskModeLuminance
	return &out
}

// ---------------------------------------------------------------------------
// clip-path (css-masking-1 §clip-path)
// ---------------------------------------------------------------------------

// ClipLength is a clip-path geometry length: logical pixels or a
// fraction of the reference box (the element's border box).
type ClipLength struct {
	// fraction of the reference box when set.
	fraction bool
	// value is the fraction (0.5 = 50%) or the logical pixel count.
	value float32
}

// ClipFraction is a clip-path length as a fraction of the reference box
// (CSS 50%).
func ClipFraction(f float32) ClipLength { return ClipLength{fraction: true, value: f} }

// ClipPx is a clip-path length in logical pixels (CSS 10px).
func ClipPx(v float32) ClipLength { return ClipLength{value: v} }

// resolve evaluates the length against the reference box extent.
func (l ClipLength) resolve(extent float32) float32 {
	if l.fraction {
		return l.value * extent
	}
	return l.value
}

// centerOf resolves the shape's center (the box center when centered,
// CSS `at 50% 50%`).
func (c ClipPath) centerOf(size Size) (float32, float32) {
	if c.centered {
		return size.Width * 0.5, size.Height * 0.5
	}
	return c.cx.resolve(size.Width), c.cy.resolve(size.Height)
}

// resolveDevice evaluates a clip length against the DEVICE box: a
// fraction of the device extent, or a logical pixel count scaled by
// the paint factor (a 40px radius at 2x paints 80 device pixels).
func (l ClipLength) resolveDevice(extent, scale float32) float32 {
	if l.fraction {
		return l.value * extent
	}
	return l.value * scale
}

// centerOfDevice resolves the shape's center against the DEVICE box
// (the fractions scale with the extent, the pixel lengths with the
// paint factor).
func (c ClipPath) centerOfDevice(x, y, w, h, scale float32) (float32, float32) {
	if c.centered {
		return x + w*0.5, y + h*0.5
	}
	return x + c.cx.resolveDevice(w, scale), y + c.cy.resolveDevice(h, scale)
}

// ClipPoint is one clip-path polygon vertex.
type ClipPoint struct {
	// X and Y are the vertex coordinates.
	X, Y ClipLength
}

// ClipPointFraction builds a vertex from fractions of the reference box
// (CSS 50% 0).
func ClipPointFraction(x, y float32) ClipPoint {
	return ClipPoint{X: ClipFraction(x), Y: ClipFraction(y)}
}

// ClipPointPx builds a vertex in logical pixels from the box's
// top-left.
func ClipPointPx(x, y float32) ClipPoint {
	return ClipPoint{X: ClipPx(x), Y: ClipPx(y)}
}

// clipShapeKind selects the clip-path shape.
type clipShapeKind uint8

const (
	clipShapeCircle clipShapeKind = iota
	clipShapeEllipse
	clipShapeInset
	clipShapePolygon
	clipShapePath
)

// ClipPath is the element's clip-path (CSS clip-path,
// browser-core ticket 57): a basic shape in element-local coordinates
// (fractions of the border box or logical pixels from its top-left).
// The paint path tessellates the shape into a scene coverage entry
// (rev 16 ticket 52's A8 rasterization) and brackets the element's box
// in an effect group compositing dst_in against it; the HIT TEST is
// point-in-shape (css-masking-1 §5: the clip-path shapes pointer
// events too), so a click outside the shape falls through to whatever
// is behind.
type ClipPath struct {
	shape clipShapeKind
	// circle/ellipse: the radii and center (center defaults to the box
	// center, CSS `at 50% 50%`).
	rx, ry   ClipLength
	cx, cy   ClipLength
	centered bool
	// inset: the four edges from the box sides.
	top, right, bottom, left ClipLength
	// polygon: the vertices.
	points []ClipPoint
	// path: a tessellated element-local path (ClipPathOfPath).
	path *Path
}

// ClipPathCircle builds circle(<radius>) — the default center is the
// box center (CSS `circle(40px)`).
func ClipPathCircle(radius ClipLength) ClipPath {
	return ClipPath{shape: clipShapeCircle, rx: radius, ry: radius, centered: true}
}

// ClipPathCircleAt builds circle(<radius> at <cx> <cy>).
func ClipPathCircleAt(radius, cx, cy ClipLength) ClipPath {
	return ClipPath{shape: clipShapeCircle, rx: radius, ry: radius, cx: cx, cy: cy}
}

// ClipPathEllipse builds ellipse(<rx> <ry>) at the box center.
func ClipPathEllipse(rx, ry ClipLength) ClipPath {
	return ClipPath{shape: clipShapeEllipse, rx: rx, ry: ry, centered: true}
}

// ClipPathEllipseAt builds ellipse(<rx> <ry> at <cx> <cy>).
func ClipPathEllipseAt(rx, ry, cx, cy ClipLength) ClipPath {
	return ClipPath{shape: clipShapeEllipse, rx: rx, ry: ry, cx: cx, cy: cy}
}

// ClipPathInset builds inset(<top> <right> <bottom> <left>) — the
// reference box inset by the edges.
func ClipPathInset(top, right, bottom, left ClipLength) ClipPath {
	return ClipPath{shape: clipShapeInset, top: top, right: right, bottom: bottom, left: left}
}

// ClipPathPolygon builds polygon(<points>) — the vertices in order.
func ClipPathPolygon(points ...ClipPoint) ClipPath {
	return ClipPath{shape: clipShapePolygon, points: append([]ClipPoint(nil), points...)}
}

// ClipPathOfPath builds path(<svg path>) — a tessellated element-local
// path (the FillPathBuilder output); the hit test answers through the
// tessellated triangles.
func ClipPathOfPath(path *Path) ClipPath {
	return ClipPath{shape: clipShapePath, path: path}
}

// Contains reports whether a point of the reference box (element-local
// logical pixels, the box's size given) lies inside the clip shape —
// the clip-path hit-test rule (css-masking-1: the shape shapes pointer
// events; a click outside misses the element). The circle, ellipse,
// inset and polygon forms are exact; the path form tests the
// tessellated triangles (the same fill the coverage rasterization
// paints).
func (c ClipPath) Contains(p Point, size Size) bool {
	switch c.shape {
	case clipShapeCircle, clipShapeEllipse:
		cx, cy := c.centerOf(size)
		rx := c.rx.resolve(size.Width)
		ry := c.ry.resolve(size.Height)
		if rx <= 0 || ry <= 0 {
			return false
		}
		dx := (p.X - cx) / rx
		dy := (p.Y - cy) / ry
		return dx*dx+dy*dy <= 1
	case clipShapeInset:
		minX := c.left.resolve(size.Width)
		maxX := size.Width - c.right.resolve(size.Width)
		minY := c.top.resolve(size.Height)
		maxY := size.Height - c.bottom.resolve(size.Height)
		return p.X >= minX && p.X < maxX && p.Y >= minY && p.Y < maxY
	case clipShapePolygon:
		return pointInPolygon(p, c.points, size)
	case clipShapePath:
		if c.path == nil {
			return false
		}
		return pointInTriangles(p, c.path.Vertices)
	}
	return false
}

// pointInPolygon is the even-odd ray cast (a simple polygon's
// inside/outside test).
func pointInPolygon(p Point, points []ClipPoint, size Size) bool {
	inside := false
	n := len(points)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		pi := Point{X: points[i].X.resolve(size.Width), Y: points[i].Y.resolve(size.Height)}
		pj := Point{X: points[j].X.resolve(size.Width), Y: points[j].Y.resolve(size.Height)}
		if (pi.Y > p.Y) != (pj.Y > p.Y) {
			cross := (pj.X-pi.X)*(p.Y-pi.Y)/(pj.Y-pi.Y) + pi.X
			if p.X < cross {
				inside = !inside
			}
		}
	}
	return inside
}

// pointInTriangles tests the tessellated triangle list (the coverage
// rasterization's own fill).
func pointInTriangles(p Point, vertices []PathVertex) bool {
	for i := 0; i+2 < len(vertices); i += 3 {
		a := Point{X: vertices[i].X, Y: vertices[i].Y}
		b := Point{X: vertices[i+1].X, Y: vertices[i+1].Y}
		c := Point{X: vertices[i+2].X, Y: vertices[i+2].Y}
		d1 := (p.X-b.X)*(a.Y-b.Y) - (a.X-b.X)*(p.Y-b.Y)
		d2 := (p.X-c.X)*(b.Y-c.Y) - (b.X-c.X)*(p.Y-c.Y)
		d3 := (p.X-a.X)*(c.Y-a.Y) - (c.X-a.X)*(p.Y-a.Y)
		hasNeg := d1 < 0 || d2 < 0 || d3 < 0
		hasPos := d1 > 0 || d2 > 0 || d3 > 0
		if !hasNeg || !hasPos {
			return true
		}
	}
	return false
}

// buildClipPath tessellates the clip shape in window DEVICE
// coordinates — the reference box (the element's border box) scaled
// and translated by the paint factor, the shape's lengths resolved
// against the device size. Tessellating AT the final coordinates
// matters: the tessellator's curve parameters (each vertex's S/T) feed
// the rasterization pipeline's edge coverage, and scaling the vertices
// after the fact would desynchronize them (the coverage would
// mis-shape). The path form (ClipPathOfPath, an element-local
// tessellation) is the exception — its vertices scale with its S/T
// carried (the curve coverage can mis-AA there; the shape forms are
// the exact ones).
func buildClipPath(c ClipPath, box Bounds, scale float32) (*Path, error) {
	if c.shape == clipShapePath {
		if c.path == nil {
			return nil, fmt.Errorf("gpui: clip-path path is empty")
		}
		local := c.path
		scaled := &Path{Bounds: scaleBounds(local.Bounds, scale), Vertices: make([]PathVertex, len(local.Vertices))}
		for i, vertex := range local.Vertices {
			scaled.Vertices[i] = PathVertex{X: vertex.X * scale, Y: vertex.Y * scale, S: vertex.S, T: vertex.T}
		}
		return translatePath(scaled, box.Origin.X*scale, box.Origin.Y*scale), nil
	}
	builder := FillPathBuilder()
	// The reference box in device pixels: the shape resolves against it.
	x := box.Origin.X * scale
	y := box.Origin.Y * scale
	w := box.Size.Width * scale
	h := box.Size.Height * scale
	switch c.shape {
	case clipShapeCircle, clipShapeEllipse:
		cx, cy := c.centerOfDevice(x, y, w, h, scale)
		rx := c.rx.resolveDevice(w, scale)
		ry := c.ry.resolveDevice(h, scale)
		if rx <= 0 || ry <= 0 {
			return nil, fmt.Errorf("gpui: clip-path ellipse has a non-positive radius at this size")
		}
		// Two half-arcs around the center (the SVG arc command's sweep).
		builder.MoveTo(cx+rx, cy)
		builder.ArcTo(rx, ry, 0, false, true, cx-rx, cy)
		builder.ArcTo(rx, ry, 0, false, true, cx+rx, cy)
		builder.Close()
	case clipShapeInset:
		minX := x + c.left.resolveDevice(w, scale)
		maxX := x + w - c.right.resolveDevice(w, scale)
		minY := y + c.top.resolveDevice(h, scale)
		maxY := y + h - c.bottom.resolveDevice(h, scale)
		if maxX <= minX || maxY <= minY {
			return nil, fmt.Errorf("gpui: clip-path inset is empty at this size")
		}
		builder.MoveTo(minX, minY)
		builder.LineTo(maxX, minY)
		builder.LineTo(maxX, maxY)
		builder.LineTo(minX, maxY)
		builder.Close()
	case clipShapePolygon:
		if len(c.points) < 3 {
			return nil, fmt.Errorf("gpui: clip-path polygon needs at least three points")
		}
		var points []Point
		for _, vertex := range c.points {
			points = append(points, Point{X: x + vertex.X.resolveDevice(w, scale), Y: y + vertex.Y.resolveDevice(h, scale)})
		}
		builder.AddPolygon(points, true)
	}
	return builder.Build()
}

// translatePath shifts a path's vertices and bounds (device pixels).
func translatePath(path *Path, dx, dy float32) *Path {
	out := &Path{
		Bounds:   offsetBounds(path.Bounds, dx, dy),
		Vertices: make([]PathVertex, len(path.Vertices)),
	}
	for i, vertex := range path.Vertices {
		out.Vertices[i] = PathVertex{X: vertex.X + dx, Y: vertex.Y + dy, S: vertex.S, T: vertex.T}
	}
	return out
}

// ---------------------------------------------------------------------------
// The paint scopes (the effect-group brackets of Style.paintBox)
// ---------------------------------------------------------------------------

// withMaskImageScope brackets f in the mask composite (rev 16, ticket
// 52's dst_in group; browser-core ticket 57's styling): the gradient
// resolves against the element's border box (the mask's painting area,
// mask-clip: border-box — the CSS default) and composites the group's
// alpha (or luminance) over it; the group clips to the border box (the
// masked content is clipped to the mask's bounding box, css-masking-1
// §the-mask-image and Chromium's MaskBoundingBox,
// css_mask_painter.cc:68-80: PhysicalBorderBoxRect).
func withMaskImageScope(w *Window, mask *MaskImage, bounds Bounds, f func()) {
	frame := currentFrame(w)
	if mask == nil || !frame.paintPhase {
		f()
		return
	}
	scale := frame.paint.ScaleFactor
	gradient, err := resolveCssGradient(mask.Gradient, bounds, scale)
	if err != nil {
		panic(fmt.Sprintf("gpui: mask-image: %v", err))
	}
	index, err := frame.scene.InsertGradient(gradient)
	if err != nil {
		panic(fmt.Sprintf("gpui: mask-image: gradient insert: %v", err))
	}
	group := EffectGroup{
		HasMask:       true,
		MaskKind:      MaskKindGradient,
		MaskReference: index,
		MaskMode:      mask.Mode,
		ClipToBounds:  true,
		Opacity:       1,
		Bounds:        snapBounds(bounds, scale),
		ContentMask:   snappedContentMask(frame.paint.Mask, scale),
	}
	if err := frame.scene.BeginEffectGroup(group); err != nil {
		panic(fmt.Sprintf("gpui: mask-image: effect group: %v", err))
	}
	defer func() {
		if err := frame.scene.EndEffectGroup(group); err != nil {
			panic(fmt.Sprintf("gpui: mask-image: effect group end: %v", err))
		}
	}()
	f()
}

// withClipPathScope brackets f in the clip-path coverage composite (rev
// 16, ticket 52's coverage mask; browser-core ticket 57's styling): the
// shape tessellates in window device coordinates, inserts as a scene
// path record (a transparent fill — it paints nothing, the coverage
// rasterization is its only consumer) and a coverage entry referencing
// it, and the group composites dst_in against the rasterized coverage.
// The group clips to the reference box (the border box): the clip-path
// region never leaves it, so overflowing ink is cut, as in CSS.
//
// The coverage entry's path index is the record's INSERTION index —
// the native renderer resolves it against the post-sort path array,
// which equals the insertion order for full-window-bounded path
// records (the bounds-tree order of a full-window leaf is above every
// prior path and below every later one); the record's bounds are
// therefore the full window, its mask unbounded.
func withClipPathScope(w *Window, clip *ClipPath, bounds Bounds, f func()) {
	frame := currentFrame(w)
	if clip == nil || !frame.paintPhase {
		f()
		return
	}
	scale := frame.paint.ScaleFactor
	path, err := buildClipPath(*clip, bounds, scale)
	if err != nil {
		panic(fmt.Sprintf("gpui: clip-path: %v", err))
	}
	// The window-sized device rect is the full-window bound the path
	// record carries (the index-stability rule above); the vertices are
	// the element's device-space shape.
	window := Bounds{Size: Size{Width: frame.viewport.Width * scale, Height: frame.viewport.Height * scale}}
	// The path record's insertion index, captured BEFORE the insert —
	// the coverage entry references it (the index the path will occupy
	// in the post-sort array; see the index-stability rule above).
	pathIndex := frame.scene.NextPathIndex()
	if err := frame.scene.kernel.InsertPath(&PathRecord{
		Bounds:      window,
		ContentMask: window,
		Color:       Background{Kind: BackgroundKindSolid, Solid: Hsla{}},
		Vertices:    path.Vertices,
	}); err != nil {
		panic(fmt.Sprintf("gpui: clip-path: path insert: %v", err))
	}
	coverage, err := frame.scene.InsertCoverage(pathIndex, path.Bounds)
	if err != nil {
		panic(fmt.Sprintf("gpui: clip-path: coverage insert: %v", err))
	}
	group := EffectGroup{
		HasMask:       true,
		MaskKind:      MaskKindCoverage,
		MaskReference: coverage,
		MaskMode:      MaskModeAlpha,
		ClipToBounds:  true,
		Opacity:       1,
		Bounds:        snapBounds(bounds, scale),
		ContentMask:   snappedContentMask(frame.paint.Mask, scale),
	}
	if err := frame.scene.BeginEffectGroup(group); err != nil {
		panic(fmt.Sprintf("gpui: clip-path: effect group: %v", err))
	}
	defer func() {
		if err := frame.scene.EndEffectGroup(group); err != nil {
			panic(fmt.Sprintf("gpui: clip-path: effect group end: %v", err))
		}
	}()
	f()
}

// withBlendScope brackets f in the blend composite (rev 16, ticket 52's
// W3C blend group; browser-core ticket 57's styling): the group's
// subtree composites with the mode instead of source-over, against a
// backdrop copy (the group forces the offscreen route — the blend
// boundary). The element's opacity rides the SAME group (Chromium's
// effect node carries state.opacity and state.blend_mode together,
// paint_property_tree_builder.cc:2285-2289) — paintInFlow skips the
// opacity scope when a blend group opens (styleElementOpacity), so a
// blended translucent element composites once, not twice.
//
// The group's bounds are the element's INK bounds — the border box
// dilated by the drop shadows, the outer ring and the outline (the
// filter group's reference box; the blend must cover the shadow ink).
func withBlendScope(w *Window, mode *BlendMode, opacity *float32, bounds Bounds, f func()) {
	frame := currentFrame(w)
	if mode == nil || !frame.paintPhase {
		f()
		return
	}
	scale := frame.paint.ScaleFactor
	group := EffectGroup{
		HasBlend:    true,
		BlendMode:   *mode,
		Opacity:     1,
		Bounds:      snapBounds(bounds, scale),
		ContentMask: snappedContentMask(frame.paint.Mask, scale),
	}
	if opacity != nil && *opacity < 1 {
		group.HasOpacity = true
		group.Opacity = clamp01(*opacity)
	}
	if err := frame.scene.BeginEffectGroup(group); err != nil {
		panic(fmt.Sprintf("gpui: mix-blend-mode: effect group: %v", err))
	}
	defer func() {
		if err := frame.scene.EndEffectGroup(group); err != nil {
			panic(fmt.Sprintf("gpui: mix-blend-mode: effect group end: %v", err))
		}
	}()
	f()
}

// withIsolationScope brackets f in an empty effect group (CSS
// isolation: isolate): no opacity, no blend, no mask — the group's
// isolation is the point (the group is the boundary that stops
// descendant blend modes from reaching past it; "isolation falls out
// of any group for free", the design's §3.7).
func withIsolationScope(w *Window, isolate bool, f func()) {
	frame := currentFrame(w)
	if !isolate || !frame.paintPhase {
		f()
		return
	}
	group := EffectGroup{Opacity: 1, ContentMask: snappedContentMask(frame.paint.Mask, frame.paint.ScaleFactor)}
	if err := frame.scene.BeginEffectGroup(group); err != nil {
		panic(fmt.Sprintf("gpui: isolation: effect group: %v", err))
	}
	defer func() {
		if err := frame.scene.EndEffectGroup(group); err != nil {
			panic(fmt.Sprintf("gpui: isolation: effect group end: %v", err))
		}
	}()
	f()
}

// styleElementOpacity is the element opacity the paintInFlow opacity
// scope should carry: nil when the blend group carries it instead (one
// effect node holds both — see withBlendScope), so a blended
// translucent element composites once.
func styleElementOpacity(style *Style) *float32 {
	if style.MixBlendMode != nil {
		return nil
	}
	return style.Opacity
}
