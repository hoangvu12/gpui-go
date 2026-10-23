package base

// Tessellated-path reuse across frames (U:plot/path_cache.rs).
//
// Go adaptations (recorded deviations):
//
//   - ShapeKey hashes with hash/maphash under one process-wide seed instead
//     of std's DefaultHasher (U:plot/path_cache.rs:1, 135-160): keys only
//     need to agree within one process. `ShapeKey::new(extra: impl Hash)`
//     is NewShapeKey[T comparable] (maphash.WriteComparable).
//   - The build closure returns (*gpui.Path, bool) for Option<Path>.
//   - PathCaches::for_paint returns the keyed-state entity; callers
//     mutate it through Entity.Update like upstream's `caches.update`.

import (
	"hash/maphash"
	"math"

	"github.com/hoangvu12/gpui-go/gpui"
)

// PathCache is a tessellated path reused across frames while its shape is
// unchanged (U:plot/path_cache.rs:5-20). The path is built once per shape
// key, at a zero origin, and moved to the frame's origin on every paint.
type PathCache struct {
	key    uint64
	hasKey bool
	// path is built relative to a zero origin.
	path *gpui.Path
}

// Get returns the path for key, moved to origin. build runs only when the
// key differs from the last call's; it must build relative to a zero
// origin (U:plot/path_cache.rs:23-36).
func (c *PathCache) Get(key uint64, origin gpui.Point, build func() (*gpui.Path, bool)) (*gpui.Path, bool) {
	if !c.hasKey || c.key != key {
		path, ok := build()
		if !ok {
			path = nil
		}
		c.path = path
		c.key, c.hasKey = key, true
	}
	if c.path == nil {
		return nil, false
	}
	return plotTranslated(c.path, origin), true
}

// IsWarm reports whether the last Get reused the path built by an earlier
// one (U:plot/path_cache.rs:38-41).
func (c *PathCache) IsWarm() bool { return c.hasKey }

// plotTranslated is path moved by offset; the copy owns its vertices
// (U:plot/path_cache.rs:44-53).
func plotTranslated(path *gpui.Path, offset gpui.Point) *gpui.Path {
	out := &gpui.Path{Bounds: path.Bounds, Vertices: make([]gpui.PathVertex, len(path.Vertices))}
	out.Bounds.Origin.X += offset.X
	out.Bounds.Origin.Y += offset.Y
	for i, v := range path.Vertices {
		v.X += offset.X
		v.Y += offset.Y
		out.Vertices[i] = v
	}
	return out
}

// PathCaches are the PathCaches of a plot that is rebuilt on every render,
// kept in the window's element state so they outlive the plot value
// (U:plot/path_cache.rs:55-76).
type PathCaches struct {
	slots []PathCache
}

// PathCachesForPaint returns the caches for the plot painting under the
// window's current element id; key tells apart several groups of shapes
// in one plot (U:plot/path_cache.rs:79-83).
func PathCachesForPaint[I gpui.IntoElementID](key I, w *gpui.Window, app *gpui.App) gpui.Entity[PathCaches] {
	return w.UseKeyedState[PathCaches](gpui.ElementIDOf(key), app, func(*gpui.Window, *gpui.Context[PathCaches]) PathCaches {
		return PathCaches{}
	})
}

// Slot returns the index-th cache, created on first use. Paint each shape
// through the same index every frame (U:plot/path_cache.rs:85-92).
func (c *PathCaches) Slot(index int) *PathCache {
	if len(c.slots) <= index {
		c.slots = append(c.slots, make([]PathCache, index+1-len(c.slots))...)
	}
	return &c.slots[index]
}

// SlotPair returns two caches for a shape that keeps a fill and a stroke,
// such as Area.PaintCached, at 2*index and 2*index+1
// (U:plot/path_cache.rs:94-104).
func (c *PathCaches) SlotPair(index int) (*PathCache, *PathCache) {
	first := 2 * index
	c.Slot(first + 1)
	return &c.slots[first], &c.slots[first+1]
}

// plotShapeKeySeed is the process-wide hash seed of every ShapeKey.
var plotShapeKeySeed = maphash.MakeSeed()

// ShapeKey is a shape key from its projected points (origin-relative) and
// whatever else shapes the tessellation (stroke width, curve style, dash
// pattern) (U:plot/path_cache.rs:107-109).
type ShapeKey struct {
	h maphash.Hash
}

// NewShapeKey starts a key from extra (U:plot/path_cache.rs:112-116).
func NewShapeKey[T comparable](extra T) *ShapeKey {
	k := &ShapeKey{}
	k.h.SetSeed(plotShapeKeySeed)
	maphash.WriteComparable(&k.h, extra)
	return k
}

// Point adds a point's bits (U:plot/path_cache.rs:118-122).
func (k *ShapeKey) Point(p gpui.Point) *ShapeKey {
	return k.F32(p.X).F32(p.Y)
}

// F32 adds a value's bits (U:plot/path_cache.rs:124-127).
func (k *ShapeKey) F32(value float32) *ShapeKey {
	bits := math.Float32bits(value)
	var b [4]byte
	b[0], b[1], b[2], b[3] = byte(bits), byte(bits>>8), byte(bits>>16), byte(bits>>24)
	_, _ = k.h.Write(b[:])
	return k
}

// Finish returns the key (U:plot/path_cache.rs:129-131).
func (k *ShapeKey) Finish() uint64 { return k.h.Sum64() }
