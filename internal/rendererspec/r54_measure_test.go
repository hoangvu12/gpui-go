//go:build windows

// The browser-core ticket 54 (rev 16) §6 measurement harness: the
// compositor measurements of docs/design/r16-compositor.md §6 against
// the rev-16 embedded artifact, recorded in
// evidence/browser-core/ticket54-native-rebuild.json.
//
// Env-gated like the gpui-perf frame harness (GPUI_PERF_OUT): run with
//
//	R54_MEASURE_OUT=evidence/browser-core/ticket54-compositor.json \
//	  GOFLAGS=-p=4 go test ./internal/rendererspec -run TestR54MeasureCompositor -count=1
//
// and it writes the JSON; without the env it skips (the ordinary suite
// owns the correctness gates — this file only measures).
//
// What it measures (all off-screen content through one shown,
// never-activated window per surface mode, exactly the rendererspec GPU
// gate pattern):
//
//   - full frame (scene rebuild + full redraw) and full redraw of the
//     same finished scene — the frame-cost anchors;
//   - the property-only frame: a transform tick through
//     UpdateEffectMatrix + DrawSceneDamage(old ∪ new) on the SAME
//     finished handle, in both surface modes (the design's "frame cost
//     ≪ baseline frame" check);
//   - damage for a hover change and a scroll (rect count, area
//     fraction, present outcome);
//   - the group-target pool's high-water mark (targets + bytes);
//   - state binds per frame (full vs property-only);
//   - the tessellation cache hit rate over re-rendered frames;
//   - full vs partial present counts (the cumulative debug counters).
package rendererspec

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/native"
)

// The measurement scene's geometry (documented in the evidence JSON):
// a 1280x800 window, a 1,000-quad "page" of 18px divs on a 20px pitch
// (the gpui-perf FrameDivs analog at a window the GPU gates can
// comfortably open), a 36-quad transform property-node group (the
// animated element) and a 22-quad "scroll column" on the right side.
const (
	r54Width  = 1280.0
	r54Height = 800.0
	r54Divs   = 1000
	r54Cols   = 64
	r54NodeID = 7
	// The scroll column: x [650, 1250], rows every 20px from y 340.
	r54ColumnX   = 650.0
	r54ColumnW   = 600.0
	r54ColumnTop = 340.0
	r54ColumnN   = 22
)

var r54FullMask = gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 4096, Height: 4096}}

// r54HoverIndex is the page quad the hover case recolors.
const r54HoverIndex = 500

// r54PageScene builds the measurement page: the 1,000-quad grid, the
// transform property-node group (rotationDeg) and the scroll column
// (scrollY, positive = content moved down). It returns the hover
// quad's rect and the scroll column's region.
func r54PageScene(t *testing.T, rotationDeg, scrollY float64, hoverLight float32) (scene *gpui.Scene, hoverRect, columnRect gpui.Bounds) {
	t.Helper()
	scene, err := gpui.NewScene()
	if err != nil {
		t.Fatalf("NewScene: %v", err)
	}
	// The page background.
	r52Quad(t, scene, 0, 0, r54Width, r54Height, 0, 0.11, 1)
	// The 1,000 divs.
	for i := 0; i < r54Divs; i++ {
		x := float32((i % r54Cols) * 20)
		y := float32((i / r54Cols) * 20)
		light := 0.5 + 0.08*float32(i%7)
		if i == r54HoverIndex && hoverLight >= 0 {
			light = hoverLight
		}
		r52QuadHsla(t, scene, x+1, y+1, 18, 18, gpui.Hsla{H: float32(i%7) / 7, S: 0.6, L: light, A: 1})
	}
	// The scroll column (rows every 20px; scrollY shifts them).
	for i := 0; i < r54ColumnN; i++ {
		y := r54ColumnTop + float32(i)*20 + float32(scrollY)
		r52QuadHsla(t, scene, r54ColumnX, y, r54ColumnW, 18, gpui.Hsla{H: 0.58, S: 0.5, L: 0.3 + 0.02*float32(i%5), A: 1})
	}
	// The transform property-node group: 36 quads authored in the
	// group's LOCAL frame (0,0,120,120), rotated about the local
	// center and placed at device (700,460).
	angle := rotationDeg * math.Pi / 180
	group := gpui.EffectGroup{
		IsStart:      true,
		HasTransform: true,
		PropertyNode: true,
		NodeID:       r54NodeID,
		Opacity:      1,
		Transform: r16MatrixAbout(
			[2][2]float32{
				{float32(math.Cos(angle)), float32(-math.Sin(angle))},
				{float32(math.Sin(angle)), float32(math.Cos(angle))},
			},
			[2]float32{60, 60},
			[2]float32{700, 460},
		),
		Bounds:      gpui.Bounds{Origin: gpui.Point{}, Size: gpui.Size{Width: 120, Height: 120}},
		ContentMask: r54FullMask,
	}
	if err := scene.BeginEffectGroup(group); err != nil {
		t.Fatalf("BeginEffectGroup: %v", err)
	}
	for i := 0; i < 36; i++ {
		x := float32((i % 6) * 20)
		y := float32((i / 6) * 20)
		r52QuadHsla(t, scene, x, y, 18, 18, gpui.Hsla{H: float32(i) / 36, S: 1, L: 0.6, A: 1})
	}
	end := group
	end.IsStart = false
	if err := scene.EndEffectGroup(end); err != nil {
		t.Fatalf("EndEffectGroup: %v", err)
	}
	hoverQuad := gpui.Bounds{
		Origin: gpui.Point{X: float32(r54HoverIndex%r54Cols)*20 + 1, Y: float32(r54HoverIndex/r54Cols)*20 + 1},
		Size:   gpui.Size{Width: 18, Height: 18},
	}
	column := gpui.Bounds{
		Origin: gpui.Point{X: r54ColumnX, Y: r54ColumnTop},
		Size:   gpui.Size{Width: r54ColumnW, Height: r54Height - r54ColumnTop},
	}
	return scene, hoverQuad, column
}

// r54Matrix returns the group's matrix at rotationDeg degrees.
func r54Matrix(rotationDeg float64) gpui.TransformationMatrix {
	angle := rotationDeg * math.Pi / 180
	return r16MatrixAbout(
		[2][2]float32{
			{float32(math.Cos(angle)), float32(-math.Sin(angle))},
			{float32(math.Sin(angle)), float32(math.Cos(angle))},
		},
		[2]float32{60, 60},
		[2]float32{700, 460},
	)
}

// r54DamageRect converts one Bounds to a damage rect dilated by pad px.
func r54DamageRect(b gpui.Bounds, pad int) native.DamageRect {
	x := int(b.Origin.X) - pad
	y := int(b.Origin.Y) - pad
	w := int(b.Size.Width) + pad*2
	h := int(b.Size.Height) + pad*2
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return native.DamageRect{X: int32(x), Y: int32(y), W: int32(w), H: int32(h)}
}

// r54Draw draws one frame (draining the submission) and returns its
// wall time in milliseconds.
func r54Draw(t *testing.T, surface *gpui.Surface, scene *gpui.Scene, rects []native.DamageRect, forceFull bool) float64 {
	t.Helper()
	start := time.Now()
	sub, err := surface.DrawSceneDamage(scene, nil, gpui.BackgroundOpaque, rects, forceFull)
	if err != nil {
		t.Fatalf("DrawSceneDamage: %v", err)
	}
	if err := sub.WaitForRetirement(15 * time.Second); err != nil {
		t.Fatalf("the submission did not retire: %v", err)
	}
	return time.Since(start).Seconds() * 1000
}

// r54Timing is one measured sequence's wall-time statistics.
type r54Timing struct {
	N      int     `json:"n"`
	MeanMS float64 `json:"mean_ms"`
	MinMS  float64 `json:"min_ms"`
}

// r54Sequence is one measured scenario: the timing plus the debug
// counters the §6 measurement asks for.
type r54Sequence struct {
	Desc                  string    `json:"desc"`
	Timing                r54Timing `json:"timing"`
	InputRectCount        int       `json:"input_damage_rects"`
	FullPresents          int       `json:"full_presents"`
	PartialPresents       int       `json:"partial_presents"`
	DamageFractionPerMile int       `json:"damage_fraction_per_mille"`
	StateBindsPerFrame    float64   `json:"state_binds_per_frame"`
}

// r54Pool is the group-target pool high-water record.
type r54Pool struct {
	Desc              string `json:"desc"`
	Groups            int    `json:"isolated_groups_in_scene"`
	RequirementsCount uint32 `json:"requirements_isolated_target_count"`
	PoolTargets       uint32 `json:"pool_targets_retained"`
	PoolBytes         uint32 `json:"pool_bytes_retained"`
	PoolOverflow      uint32 `json:"pool_overflow_count"`
}

// r54Tess is the tessellation-cache record.
type r54Tess struct {
	Desc     string  `json:"desc"`
	Paths    int     `json:"paths_per_frame"`
	Frames   int     `json:"frames"`
	Hits     int     `json:"hits"`
	Misses   int     `json:"misses"`
	Eviction int     `json:"evictions"`
	HitRate  float64 `json:"hit_rate"`
}

// r54Doc is the whole measurement record.
type r54Doc struct {
	Schema  string         `json:"schema"`
	Date    string         `json:"date"`
	Window  [2]int         `json:"window_px"`
	Note    string         `json:"note"`
	Measure map[string]any `json:"measurements"`
}

// r54RunSequence times one scenario on surface: warmup+ n steps of
// step(i), bracketing the debug counters, and returns the sequence
// record. rectCount is the damage input's rect count for the record.
func r54RunSequence(t *testing.T, surface *gpui.Surface, desc string, warmup, n, rectCount int, step func(i int)) r54Sequence {
	t.Helper()
	for i := 0; i < warmup; i++ {
		step(i)
	}
	before, err := surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	best := math.MaxFloat64
	start := time.Now()
	for i := 0; i < n; i++ {
		t0 := time.Now()
		step(i)
		if d := time.Since(t0).Seconds() * 1000; d < best {
			best = d
		}
	}
	total := time.Since(start).Seconds() * 1000
	after, err := surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	return r54Sequence{
		Desc:                  desc,
		Timing:                r54Timing{N: n, MeanMS: total / float64(n), MinMS: best},
		InputRectCount:        rectCount,
		FullPresents:          int(after.FullPresentCount - before.FullPresentCount),
		PartialPresents:       int(after.PartialPresentCount - before.PartialPresentCount),
		DamageFractionPerMile: int(after.DamageFraction),
		StateBindsPerFrame:    float64(after.StateBindCount-before.StateBindCount) / float64(n),
	}
}

// TestR54MeasureCompositor runs the whole §6 measurement set against
// the embedded rev-16 artifact and writes the evidence JSON named by
// R54_MEASURE_OUT. Skipped when the env is unset.
func TestR54MeasureCompositor(t *testing.T) {
	out := os.Getenv("R54_MEASURE_OUT")
	if out == "" {
		t.Skip("set R54_MEASURE_OUT to write the ticket 54 compositor measurements")
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join("..", "..", out)
	}
	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Fatalf("native.Load: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	identity := lib.Identity()
	sceneSvc, err := lib.Scene()
	if err != nil {
		t.Fatalf("lib.Scene: %v", err)
	}

	measure := map[string]any{}
	measure["identity"] = map[string]any{
		"native_revision": identity.NativeRevision,
		"sha256":          identity.SHA256,
		"bytes":           identity.Bytes,
		"capabilities":    fmt.Sprintf("%#x", identity.Capabilities),
		"source":          identity.Source,
	}

	// ------------------------------------------------------------------
	// The default (DComp, mode 0) surface: the frame anchors, the
	// property-only tick, hover and scroll.
	// ------------------------------------------------------------------
	surface := r16Surface(t, "rendererspec r54 measure", r54Width, r54Height)

	// Full frame: rebuild the page scene + full redraw (the closest
	// ABI-level analog of a changed frame: re-layout/re-paint stand in
	// as the scene rebuild).
	rebuildOnly := r54RunSequence(t, surface, "scene rebuild only (no draw)", 3, 20, 0, func(i int) {
		scene, _, _ := r54PageScene(t, float64(i)*0.3, 0, -1)
		if err := scene.Dispose(); err != nil {
			t.Fatalf("Dispose: %v", err)
		}
	})

	fullFrameDraw := r54RunSequence(t, surface, "full frame: rebuild + full redraw (forceFull)", 3, 20, 1, func(i int) {
		scene, _, _ := r54PageScene(t, float64(i)*0.3, 0, -1)
		defer func() {
			if err := scene.Dispose(); err != nil {
				t.Fatalf("Dispose: %v", err)
			}
		}()
		r54Draw(t, surface, scene, nil, true)
	})
	measure["full_frame_rebuild_and_redraw"] = fullFrameDraw
	measure["scene_rebuild_only"] = rebuildOnly
	t.Logf("full frame: mean %.3f ms (min %.3f); rebuild only: mean %.3f ms",
		fullFrameDraw.Timing.MeanMS, fullFrameDraw.Timing.MinMS, rebuildOnly.Timing.MeanMS)

	// Full redraw of the same finished scene (no rebuild): the draw
	// cost of a full frame, with state binds per frame.
	frameScene, _, _ := r54PageScene(t, 0, 0, -1)
	defer func() { _ = frameScene.Dispose() }()
	fullRedraw := r54RunSequence(t, surface, "full redraw of the same finished scene (forceFull)", 3, 30, 1, func(i int) {
		r54Draw(t, surface, frameScene, nil, true)
	})
	measure["full_redraw_same_scene"] = fullRedraw
	t.Logf("full redraw: mean %.3f ms (min %.3f), %.1f state binds/frame",
		fullRedraw.Timing.MeanMS, fullRedraw.Timing.MinMS, fullRedraw.StateBindsPerFrame)

	// Property-only ticks: the transform transition on the SAME
	// finished handle; damage = the update's old ∪ new mapped bounds.
	angle := 0.0
	propertyTick := r54RunSequence(t, surface, "property-only frame: UpdateEffectMatrix + damaged redraw (old ∪ new)", 3, 60, 2, func(i int) {
		angle += 0.3
		oldBounds, newBounds, err := frameScene.UpdateEffectMatrix(r54NodeID, r54Matrix(angle))
		if err != nil {
			t.Fatalf("UpdateEffectMatrix: %v", err)
		}
		rects := []native.DamageRect{r54DamageRect(oldBounds, 2), r54DamageRect(newBounds, 2)}
		r54Draw(t, surface, frameScene, rects, false)
	})
	measure["property_only_frame_dcomp"] = propertyTick
	t.Logf("property-only tick (dcomp): mean %.3f ms (min %.3f), damage %d‰, partial %d full %d",
		propertyTick.Timing.MeanMS, propertyTick.Timing.MinMS, propertyTick.DamageFractionPerMile,
		propertyTick.PartialPresents, propertyTick.FullPresents)

	// Hover change: one page quad recolors; damage = its rect.
	hover := r54RunSequence(t, surface, "hover change: rebuild with one recolored quad + damaged redraw", 3, 30, 1, func(i int) {
		scene, hoverRect, _ := r54PageScene(t, angle, 0, []float32{0.3, 0.8}[i%2])
		defer func() {
			if err := scene.Dispose(); err != nil {
				t.Fatalf("Dispose: %v", err)
			}
		}()
		r54Draw(t, surface, scene, []native.DamageRect{r54DamageRect(hoverRect, 2)}, false)
	})
	measure["hover_change"] = hover
	t.Logf("hover: mean %.3f ms (min %.3f), damage %d‰, partial %d full %d",
		hover.Timing.MeanMS, hover.Timing.MinMS, hover.DamageFractionPerMile, hover.PartialPresents, hover.FullPresents)

	// Scroll (a): the whole column redraws (no content blit — the
	// native path has no scroll copy): 1 rect.
	scrollA := r54RunSequence(t, surface, "scroll: rebuild with the column shifted + full-column damage", 3, 30, 1, func(i int) {
		scene, _, columnRect := r54PageScene(t, angle, -16*float64(i+1), -1)
		defer func() {
			if err := scene.Dispose(); err != nil {
				t.Fatalf("Dispose: %v", err)
			}
		}()
		r54Draw(t, surface, scene, []native.DamageRect{r54DamageRect(columnRect, 0)}, false)
	})
	measure["scroll_full_column"] = scrollA
	t.Logf("scroll (full column): mean %.3f ms (min %.3f), damage %d‰, partial %d full %d",
		scrollA.Timing.MeanMS, scrollA.Timing.MinMS, scrollA.DamageFractionPerMile, scrollA.PartialPresents, scrollA.FullPresents)

	// Scroll (b): the exposed-strip damage a blitting compositor would
	// compute (the native side still redraws only what it is told).
	scrollB := r54RunSequence(t, surface, "scroll: rebuild with the column shifted + exposed-strip damage", 3, 30, 1, func(i int) {
		scene, _, _ := r54PageScene(t, angle, -16*float64(i+1), -1)
		defer func() {
			if err := scene.Dispose(); err != nil {
				t.Fatalf("Dispose: %v", err)
			}
		}()
		strip := native.DamageRect{X: int32(r54ColumnX), Y: int32(r54ColumnTop), W: int32(r54ColumnW), H: 16}
		r54Draw(t, surface, scene, []native.DamageRect{strip}, false)
	})
	measure["scroll_exposed_strip"] = scrollB
	t.Logf("scroll (exposed strip): mean %.3f ms (min %.3f), damage %d‰, partial %d full %d",
		scrollB.Timing.MeanMS, scrollB.Timing.MinMS, scrollB.DamageFractionPerMile, scrollB.PartialPresents, scrollB.FullPresents)

	// ------------------------------------------------------------------
	// The pool high-water mark: 3 nested opacity groups + 2 disjoint
	// siblings (5 concurrently-isolated groups) + the transform group.
	// ------------------------------------------------------------------
	poolScene := r16Scene(t)
	r52Quad(t, poolScene, 0, 0, r54Width, r54Height, 0, 1, 1)
	nest := func(depth int, x, y, w, h float32) {
		group := gpui.EffectGroup{
			IsStart:     true,
			HasOpacity:  true,
			Opacity:     0.9 - float32(depth)*0.1,
			Bounds:      gpui.Bounds{Origin: gpui.Point{X: x, Y: y}, Size: gpui.Size{Width: w, Height: h}},
			ContentMask: r54FullMask,
		}
		if err := poolScene.BeginEffectGroup(group); err != nil {
			t.Fatalf("BeginEffectGroup: %v", err)
		}
	}
	nestEnd := func(depth int, x, y, w, h float32) {
		group := gpui.EffectGroup{
			IsStart:     false,
			HasOpacity:  true,
			Opacity:     0.9 - float32(depth)*0.1,
			Bounds:      gpui.Bounds{Origin: gpui.Point{X: x, Y: y}, Size: gpui.Size{Width: w, Height: h}},
			ContentMask: r54FullMask,
		}
		if err := poolScene.EndEffectGroup(group); err != nil {
			t.Fatalf("EndEffectGroup: %v", err)
		}
	}
	nest(0, 100, 100, 300, 300)
	r52Quad(t, poolScene, 100, 100, 300, 20, 0, 0.5, 1)
	nest(1, 150, 150, 200, 200)
	r52Quad(t, poolScene, 150, 150, 200, 20, 0.14, 0.5, 1)
	nest(2, 200, 200, 100, 100)
	r52Quad(t, poolScene, 200, 200, 100, 20, 0.29, 0.5, 1)
	nestEnd(2, 200, 200, 100, 100)
	nestEnd(1, 150, 150, 200, 200)
	nestEnd(0, 100, 100, 300, 300)
	for _, s := range [][4]float32{{700, 100, 200, 200}, {950, 100, 200, 200}} {
		group := gpui.EffectGroup{
			IsStart:     true,
			HasOpacity:  true,
			Opacity:     0.8,
			Bounds:      gpui.Bounds{Origin: gpui.Point{X: s[0], Y: s[1]}, Size: gpui.Size{Width: s[2], Height: s[3]}},
			ContentMask: r54FullMask,
		}
		if err := poolScene.BeginEffectGroup(group); err != nil {
			t.Fatalf("BeginEffectGroup: %v", err)
		}
		r52Quad(t, poolScene, s[0], s[1], s[2], 20, 0.5, 0.5, 1)
		group.IsStart = false
		if err := poolScene.EndEffectGroup(group); err != nil {
			t.Fatalf("EndEffectGroup: %v", err)
		}
	}
	r54Draw(t, surface, poolScene, nil, true)
	poolStats, err := surface.DebugStats()
	if err != nil {
		t.Fatalf("DebugStats: %v", err)
	}
	poolReqs, err := poolScene.Requirements()
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	measure["pool_high_water"] = r54Pool{
		Desc:              "3 nested opacity groups + 2 disjoint siblings (5 isolated) over a full-page scene",
		Groups:            5,
		RequirementsCount: poolReqs.IsolatedTargetCount,
		PoolTargets:       poolStats.PoolTargetCount,
		PoolBytes:         poolStats.PoolBytes,
		PoolOverflow:      poolStats.PoolOverflowCount,
	}
	t.Logf("pool: requirements isolated targets %d, retained %d targets / %d bytes, %d overflows",
		poolReqs.IsolatedTargetCount, poolStats.PoolTargetCount, poolStats.PoolBytes, poolStats.PoolOverflowCount)

	// ------------------------------------------------------------------
	// The tessellation-cache hit rate: a path-heavy scene re-rendered
	// frame after frame (the paint layer rebuilds its PathBuilders, so
	// every frame re-issues the same scripts).
	// ------------------------------------------------------------------
	const r54Paths = 32
	const r54PathFrames = 30
	before, err := sceneSvc.TessCacheStats()
	if err != nil {
		t.Fatalf("TessCacheStats: %v", err)
	}
	for frame := 0; frame < r54PathFrames; frame++ {
		pathScene, err := gpui.NewScene()
		if err != nil {
			t.Fatalf("NewScene: %v", err)
		}
		r52Quad(t, pathScene, 0, 0, r54Width, r54Height, 0, 0.11, 1)
		for i := 0; i < r54Paths; i++ {
			x := float32(20 + (i%8)*155)
			y := float32(20 + (i/8)*195)
			builder := gpui.NewPathBuilder()
			builder.MoveTo(x, y).LineTo(x+120, y).LineTo(x+120, y+120).LineTo(x, y+120).Close()
			path, err := builder.Build()
			if err != nil {
				t.Fatalf("PathBuilder.Build: %v", err)
			}
			if err := pathScene.InsertPath(path, gpui.SolidBackground(gpui.Hsla{H: float32(i) / 32, S: 0.7, L: 0.5, A: 1}), r54FullMask); err != nil {
				t.Fatalf("InsertPath: %v", err)
			}
		}
		r54Draw(t, surface, pathScene, nil, true)
		if err := pathScene.Dispose(); err != nil {
			t.Fatalf("Dispose: %v", err)
		}
	}
	after, err := sceneSvc.TessCacheStats()
	if err != nil {
		t.Fatalf("TessCacheStats: %v", err)
	}
	hits := int(after.Hits - before.Hits)
	misses := int(after.Misses - before.Misses)
	evictions := int(after.Evictions - before.Evictions)
	rate := 0.0
	if hits+misses > 0 {
		rate = float64(hits) / float64(hits+misses)
	}
	measure["tess_cache"] = r54Tess{
		Desc:     "32 square paths re-rendered 30 frames (PathBuilder.Build each frame)",
		Paths:    r54Paths,
		Frames:   r54PathFrames,
		Hits:     hits,
		Misses:   misses,
		Eviction: evictions,
		HitRate:  rate,
	}
	t.Logf("tess cache: %d hits, %d misses, %d evictions, hit rate %.1f%%", hits, misses, evictions, rate*100)

	// ------------------------------------------------------------------
	// The property-only tick in hwnd mode (mode 1): Present1's accepted
	// mode on this machine (the r53 gate pinned the dcomp fallback).
	// ------------------------------------------------------------------
	hwndSurface := r53SurfaceMode(t, "rendererspec r54 measure hwnd", r54Width, r54Height, native.SurfaceModeHwndAlphaIgnore)
	hwndScene, _, _ := r54PageScene(t, 0, 0, -1)
	defer func() { _ = hwndScene.Dispose() }()
	// Finish and prime the retained target before the first update
	// (UpdateEffectMatrix requires a finished scene; the draw would
	// finish it, but the first tick must not depend on the draw order).
	if err := hwndScene.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	r54Draw(t, hwndSurface, hwndScene, nil, true)
	hwndAngle := 0.0
	hwndTick := r54RunSequence(t, hwndSurface, "property-only frame in hwnd mode (Present1's accepted mode)", 3, 60, 2, func(i int) {
		hwndAngle += 0.3
		oldBounds, newBounds, err := hwndScene.UpdateEffectMatrix(r54NodeID, r54Matrix(hwndAngle))
		if err != nil {
			t.Fatalf("UpdateEffectMatrix: %v", err)
		}
		rects := []native.DamageRect{r54DamageRect(oldBounds, 2), r54DamageRect(newBounds, 2)}
		r54Draw(t, hwndSurface, hwndScene, rects, false)
	})
	measure["property_only_frame_hwnd"] = hwndTick
	t.Logf("property-only tick (hwnd): mean %.3f ms (min %.3f), damage %d‰, partial %d full %d",
		hwndTick.Timing.MeanMS, hwndTick.Timing.MinMS, hwndTick.DamageFractionPerMile, hwndTick.PartialPresents, hwndTick.FullPresents)

	doc := r54Doc{
		Schema:  "gpui-go/r54-compositor-measure@1",
		Date:    time.Now().UTC().Format(time.RFC3339),
		Window:  [2]int{int(r54Width), int(r54Height)},
		Note:    "wall-time measurements on one shown-never-activated window per surface mode; the page is the gpui-perf FrameDivs analog (1,000 18px quads) with a 36-quad transform property-node group and a 22-quad scroll column; min/mean over the recorded frames after warmup; the damage rect counts are the CALLER's inputs (the native side clamps, expands, merges and caps them); state binds are the device-global tracker's delta over the frames",
		Measure: measure,
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %s", out)
}
