package gpui

// This file is browser-core ticket 58: the window's damage computation
// and the paint-flashing overlay. Frames redraw and present only the
// damaged regions — the reused view ranges at the same bounds and mask
// are undamaged, everything else damages its old and new bounds — and
// the window's frame loop hands the rects to Surface.DrawSceneDamage
// (rev 16, ticket 53's native entry) instead of a full draw.
//
// The borrowed model is Chromium's raster invalidation
// (third_party/blink/renderer/platform/graphics/paint/raster_invalidator.cc
// @ 0d4dcd4f):
//
//   - the chunk rule (raster_invalidator.cc:242-249): a chunk moved
//     from a CACHED SUBSEQUENCE whose paint properties didn't change
//     relative to the layer is not invalidated. The port's cached view
//     is the cached subsequence (gpui-perf ticket 04, view_cache.go):
//     a cache hit requires the same bounds, content mask and text
//     style — the properties that decide where and how the range
//     rasterizes — and replays the previous scene's op range verbatim,
//     so the replayed range is undamaged.
//   - everything else damages old ∪ new (raster_invalidator.cc:268-
//     275): a chunk whose properties changed (or moved backward)
//     invalidates both its old and new bounds in the layer.
//   - disappeared content damages its old bounds
//     (raster_invalidator.cc:308-325: the trailing sweep invalidates
//     every unmatched old chunk).
//   - the in-order matching (raster_invalidator.cc:51-66,
//     MatchNewChunkToOldChunk searches only FORWARD from the previous
//     match): a range that moved backward fails to match and takes the
//     full invalidation. The port maps that to the conservative full
//     fallback — any out-of-order replay is a frame whose damage the
//     rule cannot prove, and correctness comes first.
//   - capture-backed surfaces are the port's contrast to Chromium's
//     foreign layers ("Foreign layers take care of raster
//     invalidation by themselves", raster_invalidator.cc:168-171):
//     the scene record cannot see a capture frame update, so a
//     capture-backed surface's bounds ALWAYS damage — even inside a
//     replayed range.
//
// Correctness gate (the design's §5 risk 2): any frame whose damage
// the rule cannot prove falls back to a full redraw — the first frame,
// a viewport/scale/appearance change, a Go-side damage log that fell
// out of step with the native op count (the drop rule is replicated at
// every tracked insert; drift means every range below is untrustworthy),
// out-of-order or out-of-range replays, and every abandoned build
// (the next frame re-renders everything anyway).
//
// The native half already clamps the rects to the viewport, expands
// them across blur/drop-shadow groups, masks, transforms and
// backdrops, merges nearby rects (the 8-px gap) and caps at 16 regions
// (over the cap a full redraw; ticket 53's answer). The Go side merges
// down to the SAME cap before passing, so a fragmented-but-small frame
// stays a partial present instead of tripping the native cap.

import (
	"fmt"
	"math"
	"sort"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// DamageRect is one device-pixel damage rectangle (the native
// GpuiGoRectRecord mirror, {x, y, w, h} i32): a region the frame
// changed. Constructed by the window's damage computation; passed to
// Surface.DrawSceneDamage.
type DamageRect = native.DamageRect

// damageReplay is one scene-op range the frame replayed from the
// previous scene (reusePaint): the reused view's undamaged region.
// prevStart/prevEnd index the PREVIOUS scene's paint-op space (the
// address space Len reports and Replay consumes); newStart/newEnd
// index THIS scene's — the copied range, exactly as long.
type damageReplay struct {
	prevStart, prevEnd uint32
	newStart, newEnd   uint32
}

// recordDamageReplay records one replayed range in the frame under
// construction (called by reusePaint after the scene copy succeeds).
func (frame *frameDrawState) recordDamageReplay(prevStart, prevEnd, newStart, newEnd uint32) {
	frame.damageReplays = append(frame.damageReplays, damageReplay{
		prevStart: prevStart,
		prevEnd:   prevEnd,
		newStart:  newStart,
		newEnd:    newEnd,
	})
}

// ---------------------------------------------------------------------------
// The scene kernel's damage log (the Go mirror of the paint-op index
// space)
// ---------------------------------------------------------------------------

// sceneOpDamage is one entry of a scene's damage log: the op's CLIPPED
// device bounds — exactly the region the op can paint, the intersection
// the native insertion core computes — or the zero rect for ops that
// carry no position (group markers, layer pushes and pops, the
// gradient/coverage/filter-op-chain table rows). capture marks a
// capture-frame surface record: its content updates outside the
// scene's knowledge, so its bounds always damage. tint marks a
// paint-flashing overlay quad (damage.go): the debug tint persists
// until its region repaints, so it never damages its own bounds.
type sceneOpDamage struct {
	bounds  Bounds
	capture bool
	tint    bool
	// table marks the ops that insert into or reference the scene's
	// index-addressed tables (browser-core 59, view_reuse.go): the
	// gradient, coverage and filter-op-chain rows the native replay
	// re-inserts at NEW indices while the copied records keep their
	// baked indices (scene.rs replay's insert_gradient/insert_coverage/
	// insert_filter_op_chain), the quads/shadows/boundaries carrying
	// those baked indices, and the path inserts (data-carried and safe
	// to replay, but they advance the path array the coverage entries
	// index into — Replay counts them).
	table sceneTableKind
}

// sceneTableKind classifies one op's relationship to the scene's
// index-addressed tables (sceneOpDamage.table).
type sceneTableKind uint8

const (
	// sceneTableNone is an index-free op (the default).
	sceneTableNone sceneTableKind = iota
	// sceneTableRef is an op whose record carries a baked table index
	// (a gradient-table background, a drop-shadow chain reference, a
	// coverage row's path index).
	sceneTableRef
	// sceneTableGradient is a gradient table insert.
	sceneTableGradient
	// sceneTableCoverage is a coverage table insert.
	sceneTableCoverage
	// sceneTableChain is a filter-op-chain table insert.
	sceneTableChain
	// sceneTablePath is a path insert (data-carried; advances the path
	// array the coverage entries index into).
	sceneTablePath
)

// sceneTableInserts is the table-insert classification counted over a
// range of log entries (Replay advances the kernel's counters by it).
type sceneTableInserts struct {
	gradients uint32
	coverage  uint32
	chains    uint32
	paths     uint32
}

// add accumulates one entry's kind.
func (c *sceneTableInserts) add(kind sceneTableKind) {
	switch kind {
	case sceneTableGradient:
		c.gradients++
	case sceneTableCoverage:
		c.coverage++
	case sceneTableChain:
		c.chains++
	case sceneTablePath:
		c.paths++
	}
}

// recordDamageRect records one positioned op: the clipped bounds, or
// nothing when the native insertion core drops the op (an empty
// clipped rect leaves no array entry and no log entry — the pinned
// drop rule, replicated field-for-field so the log's index space stays
// aligned with the native paint-op log).
func (k *SceneKernel) recordDamageRect(bounds, mask Bounds, capture bool) {
	k.recordDamageRectTable(bounds, mask, capture, sceneTableNone)
}

// recordDamageRectTable is recordDamageRect with the op's table
// classification (browser-core 59).
func (k *SceneKernel) recordDamageRectTable(bounds, mask Bounds, capture bool, table sceneTableKind) {
	if k == nil {
		return
	}
	clipped := damageClippedBounds(bounds, mask)
	if clipped.Size.Width <= 0 || clipped.Size.Height <= 0 {
		return
	}
	k.damageLog = append(k.damageLog, sceneOpDamage{bounds: clipped, capture: capture, table: table})
}

// recordDamageOpless records one op that counts in the paint-op space
// but paints nowhere: group markers (never dropped, but their pixels
// come from the children they bracket), layer pushes and pops, and the
// gradient/coverage/filter-op-chain table rows.
func (k *SceneKernel) recordDamageOpless() {
	k.recordDamageOplessTable(sceneTableNone)
}

// recordDamageOplessTable is recordDamageOpless with the op's table
// classification (browser-core 59).
func (k *SceneKernel) recordDamageOplessTable(table sceneTableKind) {
	if k == nil {
		return
	}
	k.damageLog = append(k.damageLog, sceneOpDamage{table: table})
}

// sceneRangeTableRefs reports whether prev's op-log range [start, end)
// contains ops that insert into or reference the scene's index-
// addressed tables (browser-core 59): the native replay re-inserts
// those rows at new table indices while the copied records keep their
// baked indices, so a range carrying them replays correctly only when
// the tables align exactly — unprovable at a reuse decision, so the
// view reuse refuses (renders fresh) instead. A range past the log's
// end (drift) reports true: unprovable is unsafe.
func sceneRangeTableRefs(prev *SceneKernel, start, end uint32) bool {
	if prev == nil {
		return true
	}
	if start >= end {
		return false
	}
	if end > uint32(len(prev.damageLog)) {
		return true
	}
	for i := start; i < end; i++ {
		if prev.damageLog[i].table != sceneTableNone && prev.damageLog[i].table != sceneTablePath {
			return true
		}
	}
	return false
}

// recordDamageReplay copies prev's log range into this scene's log:
// the replay re-inserts each op with its parameters unchanged, so the
// copied entries' bounds are the copied ops' bounds.
func (k *SceneKernel) recordDamageReplay(start, end uint32, prev *SceneKernel) {
	if k == nil || prev == nil || start >= end {
		return
	}
	if end > uint32(len(prev.damageLog)) {
		// The source log is shorter than the native range Replay read:
		// drift. Keep the count aligned with what Replay copied
		// (end - start ops) so a later drift check flags it.
		k.damageLog = append(k.damageLog, make([]sceneOpDamage, end-start)...)
		return
	}
	k.damageLog = append(k.damageLog, prev.damageLog[start:end]...)
}

// damageLogDrifted reports whether the kernel's Go-side damage log
// fell out of step with the native paint-op count (Len flushes the
// batch first, so the count is final). Drift means every range read
// from the log is untrustworthy, so the caller falls back to a full
// redraw — correctness first, savings second.
func (k *SceneKernel) damageLogDrifted() bool {
	if k == nil || k.scene == nil {
		return true
	}
	length, err := k.scene.Len()
	if err != nil {
		return true
	}
	return uint32(len(k.damageLog)) != length
}

// markDamageTints flags the log's last count entries as paint-flashing
// tints (the overlay's quads, inserted after the frame's damage was
// computed): the next frame's old-bounds scan skips them, so a tint
// persists until its region repaints instead of damaging its own
// bounds (Chrome's paint rects behave the same — they live until the
// area is invalidated again).
func (k *SceneKernel) markDamageTints(count int) {
	if k == nil || count <= 0 || count > len(k.damageLog) {
		return
	}
	for i := len(k.damageLog) - count; i < len(k.damageLog); i++ {
		k.damageLog[i].tint = true
	}
}

// damageClippedBounds replicates the native insertion core's
// clipped_bounds (scene.rs insert_common): the bounds intersected with
// the content mask, each far edge clamped to at least the near edge —
// the same float arithmetic, so the drop decision matches the native
// one bit for bit.
func damageClippedBounds(b, m Bounds) Bounds {
	x0 := max(b.Origin.X, m.Origin.X)
	y0 := max(b.Origin.Y, m.Origin.Y)
	x1 := max(min(b.Origin.X+b.Size.Width, m.Origin.X+m.Size.Width), x0)
	y1 := max(min(b.Origin.Y+b.Size.Height, m.Origin.Y+m.Size.Height), y0)
	return Bounds{
		Origin: Point{X: x0, Y: y0},
		Size:   Size{Width: x1 - x0, Height: y1 - y0},
	}
}

// ---------------------------------------------------------------------------
// The frame's damage computation
// ---------------------------------------------------------------------------

// computeFrameDamage computes the frame's damage (the merged device
// rects the present passes to Surface.DrawSceneDamage; full = a whole-
// window redraw). It runs after the frame's build and before the
// scene's finish — the paint-flashing overlay's tints must not enter
// this frame's damage, and the previous scene is still alive here
// (completeFrame disposes it).
//
// The rule, per the citations in the file comment:
//
//   - the replayed ranges (the reused view ranges, recorded by
//     reusePaint as [prevStart, prevEnd) in the previous scene's op
//     space) are undamaged — the ops were copied verbatim, so every
//     pixel they can paint is unchanged;
//   - the fresh ops (this scene's ranges outside the copies) damage
//     their clipped bounds — the new bounds;
//   - the previous scene's ops OUTSIDE the replayed ranges damage
//     their clipped bounds — the old bounds of everything that
//     changed, moved or disappeared;
//   - capture-backed surfaces anywhere in the new scene always damage
//     their bounds (their frames update outside the scene's
//     knowledge);
//   - any condition the rule cannot prove falls back to full.
func computeFrameDamage(w *Window, ds *windowDrawState, frame *frameDrawState, scene *Scene) (rects []DamageRect, full bool) {
	// No previously completed damage-tracked frame: the first frame
	// after the window opens (or after an abandoned build, which also
	// invalidates the state) is a full redraw.
	if !ds.lastDamageValid {
		return nil, true
	}
	// The device geometry or the clear's appearance changed: every
	// cached view's key re-resolves and the clear color repaints the
	// whole target, so the frame is a full redraw (the scissored clear
	// would otherwise leave the old color outside the damage).
	if frame.viewport != ds.lastDamageViewport || frame.scale != ds.lastDamageScale || frame.paint.Background != ds.lastDamageBackground {
		return nil, true
	}
	prev := ds.lastScene
	if prev == nil || prev.kernel == nil || scene.kernel == nil {
		return nil, true
	}
	// The op-space validation: both scenes' Go-side logs must match
	// their native op counts, or every range below is untrustworthy.
	if scene.kernel.damageLogDrifted() || prev.kernel.damageLogDrifted() {
		return nil, true
	}
	newLog, prevLog := scene.kernel.damageLog, prev.kernel.damageLog
	replays := frame.damageReplays

	// The in-order rule (MatchNewChunkToOldChunk searches only forward,
	// raster_invalidator.cc:51-66): the replayed ranges must be
	// disjoint, increasing and in range in the previous scene's op
	// space, and each copy exactly as long as its source. A range that
	// moved backward (a reordered tree) is a frame whose damage the
	// rule cannot prove — full.
	prevCursor, newCursor := uint32(0), uint32(0)
	for _, r := range replays {
		if r.prevStart < prevCursor || r.prevEnd < r.prevStart || r.prevEnd > uint32(len(prevLog)) ||
			r.newStart < newCursor || r.newEnd < r.newStart || r.newEnd > uint32(len(newLog)) ||
			r.newEnd-r.newStart != r.prevEnd-r.prevStart {
			return nil, true
		}
		prevCursor, newCursor = r.prevEnd, r.newEnd
	}

	// The device viewport the rects clip to (outward-rounded: the
	// surface's extent may differ by a rounding pixel, and a damage
	// rect that covers a pixel too many is only slower, never wrong).
	clipW := int32(math.Ceil(float64(frame.viewport.Width) * float64(frame.scale)))
	clipH := int32(math.Ceil(float64(frame.viewport.Height) * float64(frame.scale)))

	var raw []DamageRect
	collect := func(op sceneOpDamage) {
		if op.bounds.Size.Width <= 0 || op.bounds.Size.Height <= 0 {
			return
		}
		raw = append(raw, damageCoveringRect(op.bounds))
	}
	// The fresh segments: this scene's ops outside the copied ranges —
	// the new bounds of everything that painted afresh.
	newCursor = 0
	for _, r := range replays {
		for i := newCursor; i < r.newStart; i++ {
			collect(newLog[i])
		}
		newCursor = r.newEnd
	}
	for i := newCursor; i < uint32(len(newLog)); i++ {
		collect(newLog[i])
	}
	// Capture-backed surfaces: their bounds always damage, even inside
	// a replayed range (the capture frame updates outside the scene's
	// knowledge; Chromium's foreign layers self-invalidate instead —
	// raster_invalidator.cc:168-171).
	for _, op := range newLog {
		if op.capture {
			collect(op)
		}
	}
	// The old bounds: the previous scene's ops outside the replayed
	// ranges — everything that changed, moved or disappeared. The
	// paint-flashing tints are skipped (they persist until their region
	// repaints; erasing them would damage the whole flash region every
	// frame and defeat the overlay's purpose).
	prevCursor = 0
	for _, r := range replays {
		for i := prevCursor; i < r.prevStart; i++ {
			if op := prevLog[i]; !op.tint {
				collect(op)
			}
		}
		prevCursor = r.prevEnd
	}
	for i := prevCursor; i < uint32(len(prevLog)); i++ {
		if op := prevLog[i]; !op.tint {
			collect(op)
		}
	}

	return mergeDamageRects(raw, clipW, clipH), false
}

// damageCoveringRect converts one device-pixel bounds to a covering
// integer damage rect (each edge rounded outward — the rect covers at
// least the float bounds; Chrome snaps invalidation rects the same
// way).
func damageCoveringRect(b Bounds) DamageRect {
	x := int32(math.Floor(float64(b.Origin.X)))
	y := int32(math.Floor(float64(b.Origin.Y)))
	w := int32(math.Ceil(float64(b.Origin.X+b.Size.Width))) - x
	h := int32(math.Ceil(float64(b.Origin.Y+b.Size.Height))) - y
	return DamageRect{X: x, Y: y, W: w, H: h}
}

// ---------------------------------------------------------------------------
// Rect merging
// ---------------------------------------------------------------------------

// damageMergeLimit is the damage-rect cap the window presents: the
// native entry caps at max_damage_regions (16, zgui's MAX_REGIONS
// rule) and redraws fully above it, so the Go side merges down to the
// same cap to keep a fragmented-but-small frame a partial present.
const damageMergeLimit = 16

// damageCollapseCount is the raw-rect count above which the merge
// collapses to the bounding box outright: damage that fragmented is a
// near-full frame (a full rebuild, a refresh), and one union rect
// wastes far less than hundreds of pairwise merges.
const damageCollapseCount = 128

// mergeDamageRects clips the rects to the device viewport, merges them
// down to the cap, and returns the result (nil for no damage — a frame
// whose every op replayed). Merging only ever covers MORE area, so
// every merge stays sound; the escalating gap stops at the window,
// where one rect is the answer.
func mergeDamageRects(rects []DamageRect, width, height int32) []DamageRect {
	clipped := make([]DamageRect, 0, len(rects))
	for _, r := range rects {
		x0, y0, x1, y1 := r.X, r.Y, r.X+r.W, r.Y+r.H
		if x0 < 0 {
			x0 = 0
		}
		if y0 < 0 {
			y0 = 0
		}
		if x1 > width {
			x1 = width
		}
		if y1 > height {
			y1 = height
		}
		if x1 <= x0 || y1 <= y0 {
			continue
		}
		clipped = append(clipped, DamageRect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0})
	}
	if len(clipped) == 0 {
		return nil
	}
	if len(clipped) > damageCollapseCount {
		box := clipped[0]
		for _, r := range clipped[1:] {
			box = damageRectUnion(box, r)
		}
		return []DamageRect{box}
	}
	for _, gap := range [...]int32{0, 4, 16, 64, 256, 1024, 4096, 1 << 30} {
		clipped = damageMergePass(clipped, gap)
		if len(clipped) <= damageMergeLimit {
			return clipped
		}
	}
	return clipped
}

// damageMergePass greedily merges every pair of rects whose boxes,
// inflated by gap, overlap. O(m²) with m bounded by the collapse
// threshold; a pass leaves the count non-increasing.
func damageMergePass(rects []DamageRect, gap int32) []DamageRect {
	sorted := append([]DamageRect(nil), rects...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Y != sorted[j].Y {
			return sorted[i].Y < sorted[j].Y
		}
		return sorted[i].X < sorted[j].X
	})
	out := make([]DamageRect, 0, len(sorted))
	for _, r := range sorted {
		merged := false
		for i := range out {
			if damageRectsNear(out[i], r, gap) {
				out[i] = damageRectUnion(out[i], r)
				merged = true
				break
			}
		}
		if !merged {
			out = append(out, r)
		}
	}
	return out
}

// damageRectsNear reports whether two rects' boxes, inflated by gap,
// overlap (gap 0: overlap or touch).
func damageRectsNear(a, b DamageRect, gap int32) bool {
	return a.X <= b.X+b.W+gap && b.X <= a.X+a.W+gap &&
		a.Y <= b.Y+b.H+gap && b.Y <= a.Y+a.H+gap
}

// damageRectUnion returns the bounding box of two rects.
func damageRectUnion(a, b DamageRect) DamageRect {
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.W, b.X+b.W), max(a.Y+a.H, b.Y+b.H)
	return DamageRect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// ---------------------------------------------------------------------------
// The paint-flashing overlay
// ---------------------------------------------------------------------------

// paintDamageFlashingOverlay tints the frame's damage rects into the
// scene before its finish (WindowOptions.PaintFlashing /
// Window.SetPaintFlashing, the debug option): one translucent quad per
// merged damage rect, alternating red and green frame over frame so
// repeated changes flash, and one viewport-sized quad on a full
// redraw. The tints are inserted AFTER the damage was computed, so
// they do not enter this frame's damage; next frame they are previous-
// scene ops no range replayed, so their region damages and the tint
// erases (or re-tints where the damage persists) — the flashing
// effect. Chrome's paint rectangles (about:flags "Show paint rects",
// the DevTools "Highlight repaints" ancestor) tint the invalidated
// regions the same way.
func paintDamageFlashingOverlay(ds *windowDrawState, frame *frameDrawState, scene *Scene, rects []DamageRect, full bool) error {
	if !ds.paintFlashing {
		return nil
	}
	ds.damageFlashFrame++
	tint := Hsla{H: 0, S: 1, L: 0.5, A: 0.35}
	if ds.damageFlashFrame%2 == 0 {
		// The alternating frame: green on the other half of the cycle.
		tint = Hsla{H: 1.0 / 3.0, S: 1, L: 0.5, A: 0.35}
	}
	mask := Bounds{Origin: Point{}, Size: frame.deviceViewport()}
	if full {
		rects = []DamageRect{{X: 0, Y: 0, W: int32(mask.Size.Width), H: int32(mask.Size.Height)}}
	}
	for _, r := range rects {
		quad := Quad{
			Bounds: Bounds{
				Origin: Point{X: float32(r.X), Y: float32(r.Y)},
				Size:   Size{Width: float32(r.W), Height: float32(r.H)},
			},
			ContentMask: mask,
			Background:  SolidBackground(tint),
		}
		if err := scene.InsertQuad(quad); err != nil {
			return fmt.Errorf("gpui: paint flashing overlay: %w", err)
		}
	}
	// The tints persist until their region repaints (they never damage
	// their own bounds), so the next frame's old-bounds scan skips them.
	scene.kernel.markDamageTints(len(rects))
	return nil
}

// deviceViewport returns the frame's device-pixel viewport (the
// logical size scaled, outward-rounded so the overlay's mask covers
// every painted pixel).
func (frame *frameDrawState) deviceViewport() Size {
	return Size{
		Width:  float32(math.Ceil(float64(frame.viewport.Width) * float64(frame.scale))),
		Height: float32(math.Ceil(float64(frame.viewport.Height) * float64(frame.scale))),
	}
}

// ---------------------------------------------------------------------------
// The public surface (the present seam's damage inputs)
// ---------------------------------------------------------------------------

// LastFrameDamage returns the damage of the window's last completed
// frame: the merged device-pixel rects the frame changed, and whether
// the frame was a full redraw (rects nil). The window's frame loop
// computes it (DrawWindowFrame) from the reused view ranges — every
// replayed range is undamaged, everything else damages its old and new
// bounds — and the present step passes the rects to
// Surface.DrawSceneDamage instead of a full draw:
//
//	rects, full := w.LastFrameDamage()
//	sub, err := surface.DrawSceneDamage(scene, atlas, appearance, rects, full)
//
// or through the PresentFrame helper. The rects stay valid until the
// window's next completed frame; a full-redraw answer (full true) must
// be presented as one (the retained-target damage entry treats a
// forceFull flag exactly that way). A window with no completed frame
// answers full.
func (w *Window) LastFrameDamage() (rects []DamageRect, fullRedraw bool) {
	if w == nil {
		return nil, true
	}
	ds, ok := windowDrawStates[w]
	if !ok || !ds.lastDamageValid {
		return nil, true
	}
	return ds.lastDamageRects, ds.lastDamageFull
}

// SetPaintFlashing enables or disables the paint-flashing overlay
// (WindowOptions.PaintFlashing): each frame's damage rects are tinted
// into the scene before its finish, alternating red and green frame
// over frame. The change applies from the window's next drawn frame.
func (w *Window) SetPaintFlashing(enabled bool) {
	if w == nil {
		return
	}
	drawState(w).paintFlashing = enabled
}

// PaintFlashing reports whether the paint-flashing overlay is enabled
// for this window.
func (w *Window) PaintFlashing() bool {
	if w == nil {
		return false
	}
	ds, ok := windowDrawStates[w]
	return ok && ds.paintFlashing
}

// PresentFrame presents the window's last completed frame through the
// surface with the window's computed damage — the OnRedraw present
// step's damage-aware spelling (the frame loop's default present
// shape; see DrawWindowFrame and frameloop.go):
//
//	OnRedraw: func() {
//		if _, err := gpui.PresentFrame(window, surface, gpui.BackgroundOpaque); err != nil {
//			log.Print(err)
//		}
//	}
//
// The scene is the window's last drawn scene, the atlas the window's
// own glyph atlas (sprite-free scenes present without one), and the
// damage the window's LastFrameDamage — a full-redraw frame presents
// through the damage entry's forceFull path, never the legacy
// full-window draw (a legacy draw would invalidate the surface's
// retained frame target and cost the NEXT partial frame its savings).
// The returned submission follows DrawSceneDamage's protocol.
func PresentFrame(w *Window, surface *Surface, appearance BackgroundAppearance) (*Submission, error) {
	if surface == nil {
		return nil, fmt.Errorf("gpui: PresentFrame requires a surface")
	}
	if w == nil {
		return nil, fmt.Errorf("gpui: PresentFrame requires a window")
	}
	scene := LastDrawnScene(w)
	if scene == nil {
		return nil, fmt.Errorf("gpui: PresentFrame: the window has no drawn frame to present")
	}
	// The window's own atlas owns the scene's sprite textures; a
	// sprite-free window answers ErrNoGlyphAtlas and presents without
	// one (a sprite scene without its atlas fails with the typed
	// ErrSceneSpriteAtlas inside DrawSceneDamage).
	atlas, err := w.GlyphAtlas()
	if err != nil {
		atlas = nil
	}
	rects, full := w.LastFrameDamage()
	if !full && len(rects) == 0 {
		// A frame whose every op replayed unchanged has nothing to
		// present: the surface's last presented frame already shows
		// exactly this content (a root notification with every cached
		// view reusing its range). Skipping the submission also matters
		// for the native entry: an empty rect list is its full-redraw
		// fallback, which would waste exactly the frame the rule saved.
		return nil, nil
	}
	return surface.DrawSceneDamage(scene, atlas, appearance, rects, full)
}

// DebugFrameGuardState reports the request-frame re-entrancy guard
// (temporary ticket-58 instrumentation: the window whose frame is being
// built and the in-progress completion count).
func DebugFrameGuardState() (building *Window, finishing int) {
	return activeFrame, finishingFrames
}
