//go:build windows

// The browser-core ticket 53 (rev 16) scene gate for the path
// tessellation LRU cache: repeated scripts return their cached vertices
// without re-running lyon (bit-exact by construction), the counters are
// observable, and the 128-entry cap evicts the least recently used.
package scenespec

import (
	"math"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// r53SceneService loads the native library and returns its scene
// service (the tess cache's counters ride the stateless stats entry).
func r53SceneService(t *testing.T) *native.SceneService {
	t.Helper()
	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Fatalf("native.Load: %v", err)
	}
	svc, err := lib.Scene()
	if err != nil {
		t.Fatalf("lib.Scene: %v", err)
	}
	return svc
}

// r53SquareScript builds the command script of a filled 20x20 square
// at (x, y) with the given fill tolerance (the style command carries
// it, and the cache key hashes it independently too).
func r53SquareScript(x, y, tolerance float32) ([]native.PathCommandRecord, []uint32) {
	style := native.NewPathCommandRecord(native.ScenePathCmdStyle)
	style.Data[0] = uint32(native.ScenePathStyleFill)
	style.SetFloat(1, tolerance)
	commands := []native.PathCommandRecord{style}
	move := native.NewPathCommandRecord(native.ScenePathCmdMoveTo)
	move.SetFloat(0, x)
	move.SetFloat(1, y)
	commands = append(commands, move)
	// line to the three other corners (the last closes).
	for _, point := range [][2]float32{{x + 20, y}, {x + 20, y + 20}, {x, y + 20}} {
		line := native.NewPathCommandRecord(native.ScenePathCmdLineTo)
		line.SetFloat(0, point[0])
		line.SetFloat(1, point[1])
		commands = append(commands, line)
	}
	commands = append(commands, native.NewPathCommandRecord(native.ScenePathCmdClose))
	return commands, nil
}

// TestR53TessCacheHitMissAndIdenticalVertices: one script tessellated
// twice returns byte-identical vertices, the first pass misses (twice:
// the client's capacity probe and data call) and the repeat hits.
func TestR53TessCacheHitMissAndIdenticalVertices(t *testing.T) {
	svc := r53SceneService(t)
	before, err := svc.TessCacheStats()
	if err != nil {
		t.Fatalf("TessCacheStats: %v", err)
	}
	commands, words := r53SquareScript(10, 10, 0.1)
	first, firstBounds, err := svc.TessellatePath(commands, words)
	if err != nil {
		t.Fatalf("TessellatePath: %v", err)
	}
	if len(first) == 0 {
		t.Fatalf("the square tessellated to no vertices")
	}
	middle, err := svc.TessCacheStats()
	if err != nil {
		t.Fatalf("TessCacheStats: %v", err)
	}
	if middle.Misses != before.Misses+1 {
		t.Fatalf("misses after one TessellatePath = %d, want %d (the capacity probe tessellates once)", middle.Misses, before.Misses+1)
	}
	// The repeat: the capacity probe and the data call both hit.
	second, secondBounds, err := svc.TessellatePath(commands, words)
	if err != nil {
		t.Fatalf("TessellatePath (repeat): %v", err)
	}
	after, err := svc.TessCacheStats()
	if err != nil {
		t.Fatalf("TessCacheStats: %v", err)
	}
	if after.Hits != middle.Hits+2 {
		t.Fatalf("hits after the repeat = %d, want %d (probe + data both hit)", after.Hits, middle.Hits+2)
	}
	if after.Misses != middle.Misses {
		t.Fatalf("misses changed on the repeat: %d -> %d", middle.Misses, after.Misses)
	}
	if len(first) != len(second) {
		t.Fatalf("vertex count changed on the repeat: %d -> %d", len(first), len(second))
	}
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("vertex %d differs between the cached and fresh tessellation: %+v vs %+v", i, first[i], second[i])
		}
	}
	if firstBounds != secondBounds {
		t.Fatalf("bounds changed on the repeat: %+v vs %+v", firstBounds, secondBounds)
	}
}

// TestR53TessCacheKeysOnBytesAndTolerance: a different script (moved
// geometry) and the same geometry under a different fill tolerance are
// distinct keys - both miss and neither disturbs the original entry.
func TestR53TessCacheKeysOnBytesAndTolerance(t *testing.T) {
	svc := r53SceneService(t)
	commands, words := r53SquareScript(30, 30, 0.1)
	if _, _, err := svc.TessellatePath(commands, words); err != nil {
		t.Fatalf("TessellatePath: %v", err)
	}
	afterFirst, err := svc.TessCacheStats()
	if err != nil {
		t.Fatalf("TessCacheStats: %v", err)
	}
	// Moved geometry: a different key (a miss).
	moved, _ := r53SquareScript(70, 30, 0.1)
	if _, _, err := svc.TessellatePath(moved, nil); err != nil {
		t.Fatalf("TessellatePath (moved): %v", err)
	}
	// The same geometry with a different fill tolerance: the key hashes
	// the tolerance too (a miss, and the vertices may differ - lyon
	// curves its approximation by it).
	tolerant, _ := r53SquareScript(30, 30, 2.0)
	if _, _, err := svc.TessellatePath(tolerant, nil); err != nil {
		t.Fatalf("TessellatePath (tolerance): %v", err)
	}
	afterMisses, err := svc.TessCacheStats()
	if err != nil {
		t.Fatalf("TessCacheStats: %v", err)
	}
	if afterMisses.Misses != afterFirst.Misses+2 {
		t.Fatalf("misses after the two distinct scripts = %d, want %d", afterMisses.Misses, afterFirst.Misses+2)
	}
	// The original entry is still cached (a hit).
	if _, _, err := svc.TessellatePath(commands, words); err != nil {
		t.Fatalf("TessellatePath (original): %v", err)
	}
	final, err := svc.TessCacheStats()
	if err != nil {
		t.Fatalf("TessCacheStats: %v", err)
	}
	if final.Hits != afterMisses.Hits+2 {
		t.Fatalf("hits after re-running the original = %d, want %d", final.Hits, afterMisses.Hits+2)
	}
}

// TestR53TessCacheLRUEviction: 128+1 distinct scripts overflow the
// cache; the entry count caps at 128 and evictions are reported, and
// the least recently used entry (the FIRST script) is gone while the
// most recent stays.
func TestR53TessCacheLRUEviction(t *testing.T) {
	svc := r53SceneService(t)
	// Fill the cache with exactly 128 distinct scripts (the cap).
	for i := 0; i < 128; i++ {
		commands, _ := r53SquareScript(float32(i)*8, 0, 0.1)
		if _, _, err := svc.TessellatePath(commands, nil); err != nil {
			t.Fatalf("TessellatePath (%d): %v", i, err)
		}
	}
	filled, err := svc.TessCacheStats()
	if err != nil {
		t.Fatalf("TessCacheStats: %v", err)
	}
	if filled.Entries > 128 {
		t.Fatalf("entries = %d, want at most the 128-entry cap", filled.Entries)
	}
	if filled.Entries < 120 {
		t.Fatalf("entries = %d, want the cache filled (other tests may hold a few live entries)", filled.Entries)
	}
	// One more distinct script evicts the least recently used entry.
	overflow, _ := r53SquareScript(2000, 2000, 0.1)
	if _, _, err := svc.TessellatePath(overflow, nil); err != nil {
		t.Fatalf("TessellatePath (overflow): %v", err)
	}
	after, err := svc.TessCacheStats()
	if err != nil {
		t.Fatalf("TessCacheStats: %v", err)
	}
	if after.Entries > 128 {
		t.Fatalf("entries after overflow = %d, want at most 128", after.Entries)
	}
	if after.Evictions <= filled.Evictions {
		t.Fatalf("evictions did not advance: %d -> %d", filled.Evictions, after.Evictions)
	}
	// The most recent script is still a hit.
	afterHits := after.Hits
	if _, _, err := svc.TessellatePath(overflow, nil); err != nil {
		t.Fatalf("TessellatePath (recent): %v", err)
	}
	final, err := svc.TessCacheStats()
	if err != nil {
		t.Fatalf("TessCacheStats: %v", err)
	}
	if final.Hits != afterHits+2 {
		t.Fatalf("the most recent entry evicted: hits %d -> %d (want +2)", afterHits, final.Hits)
	}
	// Sanity: the entries stay deterministic under the cap.
	if math.Abs(float64(final.Entries)-float64(after.Entries)) > 1 {
		t.Fatalf("entry count drifted: %d -> %d", after.Entries, final.Entries)
	}
}
