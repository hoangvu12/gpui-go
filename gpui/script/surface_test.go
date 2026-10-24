//go:build windows

package script_test

// The surface fixture runs (ticket 63): the .tsx that exercises the
// whole typed surface — every element, every style property, both
// events, the state globals and a spread of classes — loads through
// the real compile path (esbuild) and runs through the host (goja),
// its handlers fire, and its bound values update without re-describing.
//
// tsc checks the same file against the generated .d.ts in
// gpui/script/types's tests (when a tsc is available); this test is
// the engine half: everything the types declare actually runs.

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui/script"
	"github.com/hoangvu12/gpui-go/gpui/script/compile"
)

// loadSurfaceFixture compiles testdata/surface.tsx and loads it into a
// fresh host with the event log bridge installed.
func loadSurfaceFixture(t testing.TB) (*script.Host, *eventLog) {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("testdata", "surface.tsx"))
	if err != nil {
		t.Fatal(err)
	}
	log := &eventLog{}
	h := script.New()
	h.SetGlobal("logEvent", log.record)
	bundle, err := compile.String("surface.tsx", string(src))
	if err != nil {
		t.Fatalf("compile surface.tsx: %v", err)
	}
	if err := h.Load(bundle); err != nil {
		t.Fatalf("load surface.tsx: %v", err)
	}
	return h, log
}

// eventLog records the handlers' bridge calls.
type eventLog struct {
	mu    sync.Mutex
	calls []string
}

func (l *eventLog) record(kind, target string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, kind+" "+target)
}

func (l *eventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.calls...)
}

func TestTheSurfaceFixtureRunsThroughTheHost(t *testing.T) {
	h, log := loadSurfaceFixture(t)
	if h.Err() != nil {
		t.Fatalf("the surface fixture failed to load: %v", h.Err())
	}
	if got, ok := h.Text("root"); !ok || got != "surfaceclicked 0no row" {
		t.Fatalf("root text = %q, ok=%v", got, ok)
	}
	// The function component and the Fragment both lowered into the
	// tree (their text is part of the root's).
	if h.NodeCount() < 6 {
		t.Fatalf("node count = %d, want the full described tree", h.NodeCount())
	}
	// A click drives the bound value without re-describing, and selects
	// the row (useState, the documented slow path, which re-describes).
	before := h.DescribeCount()
	for range 3 {
		if err := h.Click("counter"); err != nil {
			t.Fatalf("click counter: %v", err)
		}
	}
	if got, _ := h.Text("root"); !strings.Contains(got, "clicked 3") {
		t.Fatalf("root text after clicks = %q", got)
	} else if !strings.Contains(got, "row selected") {
		t.Fatalf("root text = %q, want the row selected by the click", got)
	}
	if h.DescribeCount() != before+3 {
		t.Fatalf("DescribeCount = %d, want one re-describe per useState set", h.DescribeCount())
	}
	// The handlers fired through the bridge.
	if len(log.snapshot()) < 3 {
		t.Fatalf("handler calls = %v, want the three clicks", log.snapshot())
	}
}

// The root's onMouseDown handler runs (the other declared event), with
// the event object's fields reaching the handler.
func TestTheSurfaceFixtureHandlesMouseDown(t *testing.T) {
	h, log := loadSurfaceFixture(t)
	if err := h.MouseDown("root"); err != nil {
		t.Fatalf("mousedown root: %v", err)
	}
	calls := log.snapshot()
	if len(calls) != 1 || calls[0] != "mousedown root:0" {
		t.Fatalf("mousedown calls = %v", calls)
	}
}
