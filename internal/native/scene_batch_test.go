//go:build windows

package native

import (
	"errors"
	"reflect"
	"testing"
)

// buildBatchScene paints the same mixed op sequence into a scene: through
// the batched Scene methods when direct is false, or through the
// single-op native entries (one call per op, the pre-ticket-05 path) when
// direct is true.
func buildBatchScene(t *testing.T, svc *SceneService, direct bool) *Scene {
	t.Helper()
	sc, err := svc.CreateScene()
	if err != nil {
		t.Fatalf("CreateScene: %v", err)
	}
	h := uint64(sc.handle)
	tab := &svc.table
	check := func(code int32, err error) {
		t.Helper()
		if err != nil || code != sceneStatusOK {
			t.Fatalf("single-op entry: code %d err %v", code, err)
		}
	}
	quad := func(x float32) *SceneQuadRecord {
		rec := NewSceneQuadRecord()
		rec.Bounds = SceneBoundsOf(x, 0, 10, 10)
		rec.ContentMask = SceneContentMaskOf(0, 0, 1000, 1000)
		rec.Background = SceneBackgroundOf(0.5, 1, 0.5, 1)
		return &rec
	}
	sprite := func(x float32, kind uint32) *SceneSpriteRecord {
		rec := NewSceneSpriteRecord()
		rec.Bounds = SceneBoundsOf(x, 5, 8, 8)
		rec.ContentMask = SceneContentMaskOf(0, 0, 1000, 1000)
		rec.Tile = SceneTileRecord{TextureKind: kind, BoundsW: 8, BoundsH: 8}
		return &rec
	}
	surface := NewSceneSurfaceRecord()
	surface.Bounds = SceneBoundsOf(40, 40, 20, 20)
	surface.ContentMask = SceneContentMaskOf(0, 0, 1000, 1000)
	layer := SceneBoundsOf(0, 0, 50, 50)
	for i := 0; i < 600; i++ { // past one flush threshold of sprites
		x := float32(i % 90)
		if direct {
			check(callSceneInsertQuad(tab.insertQuad, h, quad(x)))
			check(callSceneInsertSprite(tab.insertMonochromeSprite, h, sprite(x, SceneSpriteTextureMonochrome)))
			if i%50 == 0 {
				check(callScenePushLayer(tab.pushLayer, h, &layer))
				check(callSceneInsertSprite(tab.insertSubpixelSprite, h, sprite(x+1, SceneSpriteTextureSubpixel)))
				check(callSceneOp(tab.popLayer, h))
				check(callSceneInsertSurface(tab.insertSurface, h, &surface, 0x3F000000))
			}
			if i == 300 {
				check(callSceneOp(tab.raiseOrderFloor, h))
			}
			continue
		}
		mustOK := func(err error) {
			t.Helper()
			if err != nil {
				t.Fatalf("batched op: %v", err)
			}
		}
		mustOK(sc.InsertQuad(quad(x)))
		mustOK(sc.InsertMonochromeSprite(sprite(x, SceneSpriteTextureMonochrome)))
		if i%50 == 0 {
			mustOK(sc.PushLayer(layer))
			mustOK(sc.InsertSubpixelSprite(sprite(x+1, SceneSpriteTextureSubpixel)))
			mustOK(sc.PopLayer())
			mustOK(sc.InsertSurface(&surface, 0.5))
		}
		if i == 300 {
			mustOK(sc.RaiseOrderFloor())
		}
	}
	if err := sc.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	return sc
}

// TestSceneBatchMatchesSingleOps pins gpui-perf ticket 05's bulk insert:
// the batched stream produces exactly the scene the one-call-per-op
// entries produce (op count, assigned orders, every dump and the plan),
// in a fraction of the native calls.
func TestSceneBatchMatchesSingleOps(t *testing.T) {
	svc, err := mustLoad(t, Options{}).Scene()
	if err != nil {
		t.Fatalf("Scene: %v", err)
	}
	direct := buildBatchScene(t, svc, true)
	before := CallCount()
	batched := buildBatchScene(t, svc, false)
	calls := CallCount() - before
	defer direct.Dispose()
	defer batched.Dispose()

	type snapshot struct {
		Len      uint32
		Meta     SceneMetaRecord
		Quads    []SceneQuadRecord
		Mono     []SceneSpriteRecord
		Sub      []SceneSpriteRecord
		Surfaces []SceneSurfaceRecord
		Opacity  []float32
		Plan     []SceneCommandRecord
	}
	take := func(sc *Scene) snapshot {
		var s snapshot
		var err error
		must := func(e error) {
			if e != nil && err == nil {
				err = e
			}
		}
		var e error
		s.Len, e = sc.Len()
		must(e)
		s.Meta, e = sc.Meta()
		must(e)
		s.Quads, e = sc.DumpQuads()
		must(e)
		s.Mono, e = sc.DumpMonochromeSprites()
		must(e)
		s.Sub, e = sc.DumpSubpixelSprites()
		must(e)
		s.Surfaces, s.Opacity, e = sc.DumpSurfaces()
		must(e)
		s.Plan, e = sc.PlanCommands()
		must(e)
		if err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		return s
	}
	want, got := take(direct), take(batched)
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("the batched scene differs from the single-op scene: len %d vs %d, meta %+v vs %+v", want.Len, got.Len, want.Meta, got.Meta)
	}
	if want.Meta.QuadCount != 600 || want.Meta.MonochromeSpriteCount != 600 {
		t.Fatalf("unexpected scene content: %+v", want.Meta)
	}
	// 1,000+ ops: create + a handful of threshold flushes + the flush
	// before finish + finish.
	if calls > 10 {
		t.Errorf("building the batched scene took %d native calls, want <= 10", calls)
	}
}

// TestSceneBatchErrors pins the error timing: a malformed record still
// fails at its insert call (the stateless checks run before buffering),
// and nothing of it reaches the stream.
func TestSceneBatchErrors(t *testing.T) {
	svc, err := mustLoad(t, Options{}).Scene()
	if err != nil {
		t.Fatalf("Scene: %v", err)
	}
	sc, err := svc.CreateScene()
	if err != nil {
		t.Fatalf("CreateScene: %v", err)
	}
	defer sc.Dispose()
	bad := NewSceneQuadRecord()
	bad.Order = 7
	if err := sc.InsertQuad(&bad); !errors.Is(err, ErrSceneBadValue) {
		t.Fatalf("InsertQuad(order 7) = %v, want ErrSceneBadValue at the call", err)
	}
	sprite := NewSceneSpriteRecord()
	sprite.Tile = SceneTileRecord{TextureKind: SceneSpriteTexturePolychrome, BoundsW: 4, BoundsH: 4}
	if err := sc.InsertMonochromeSprite(&sprite); !errors.Is(err, ErrSceneBadValue) {
		t.Fatalf("monochrome sprite with a polychrome tile = %v, want ErrSceneBadValue at the call", err)
	}
	if err := sc.InsertQuad(nil); !errors.Is(err, ErrSceneNullArg) {
		t.Fatalf("InsertQuad(nil) = %v, want ErrSceneNullArg", err)
	}
	length, err := sc.Len()
	if err != nil || length != 0 {
		t.Fatalf("Len after rejected inserts = %d, %v; want 0, nil", length, err)
	}
	// A state error surfaces at the flush: a disposed scene's stream.
	good := NewSceneQuadRecord()
	good.Bounds = SceneBoundsOf(0, 0, 4, 4)
	good.ContentMask = SceneContentMaskOf(0, 0, 4, 4)
	stale, err := svc.CreateScene()
	if err != nil {
		t.Fatalf("CreateScene: %v", err)
	}
	if err := stale.InsertQuad(&good); err != nil {
		t.Fatalf("buffered insert: %v", err)
	}
	if code, err := callSceneOp(svc.table.dispose, uint64(stale.handle)); err != nil || code != sceneStatusOK {
		t.Fatalf("native dispose: %d %v", code, err)
	}
	if err := stale.Finish(); !errors.Is(err, ErrSceneStaleHandle) {
		t.Fatalf("Finish of a stale scene with pending ops = %v, want ErrSceneStaleHandle from the flush", err)
	}
}
