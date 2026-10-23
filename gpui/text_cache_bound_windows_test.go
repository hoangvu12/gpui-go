//go:build windows

package gpui

import (
	"errors"
	"fmt"
	"testing"
)

// Base ticket 40: the line-layout cache under the native live-handle
// bound (256 process-wide, frozen at rev 11). The reference's
// LineLayoutCache (line_layout.rs:925-1127) has no such bound — its
// entries are Arc'd layouts — so the port parks the least recently used
// cache-owned handle when a shape reaches the bound and re-shapes it on
// its next query. These tests drive the cache directly on the real text
// stack (no window).

// TestShapedLineCacheParksPastTheHandleBound shapes more distinct lines
// into one cache frame than the native bound holds: every shape
// succeeds, the live count never exceeds the bound, every line still
// answers its geometry (parked lines re-shape), a caller-held line is
// never parked, and the frame rotation still disposes everything.
func TestShapedLineCacheParksPastTheHandleBound(t *testing.T) {
	system, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	bound := system.MaxShapingHandles()
	baseline := system.LiveShapingHandles()

	held, err := system.ShapeText(keyRequest("a caller-held line", 16, DefaultFontDescriptor(), nil, nil))
	if err != nil {
		t.Fatalf("ShapeText (held): %v", err)
	}
	defer func() { _ = held.Dispose() }()

	var cache shapedLineCache
	defer cache.dispose()
	count := bound + 144
	lines := make([]*WrappedLine, count)
	widths := make([]float32, count)
	for i := range lines {
		line, err := cache.layoutWrappedLine(system, keyRequest(fmt.Sprintf("bound line %d", i), 16, DefaultFontDescriptor(), nil, nil))
		if err != nil {
			t.Fatalf("layoutWrappedLine %d: %v (the cache must park instead of failing)", i, err)
		}
		lines[i] = line
		summary, _ := line.Summary()
		widths[i] = summary.Width
		if live := system.LiveShapingHandles(); live > bound {
			t.Fatalf("after line %d: LiveShapingHandles = %d exceeds the bound %d", i, live, bound)
		}
	}
	parked := 0
	for _, line := range lines {
		if line.parked {
			parked++
		}
	}
	if parked == 0 {
		t.Fatalf("no line was parked although %d lines exceed the bound %d", count, bound)
	}

	// Every line answers its geometry; a parked line re-shapes to the
	// same layout.
	for i, line := range lines {
		glyphs, err := line.Glyphs()
		if err != nil {
			t.Fatalf("Glyphs of line %d: %v", i, err)
		}
		if len(glyphs) == 0 {
			t.Fatalf("line %d has no glyphs", i)
		}
		summary, _ := line.Summary()
		if summary.Width != widths[i] {
			t.Fatalf("line %d width after re-shape = %v, want %v", i, summary.Width, widths[i])
		}
		if live := system.LiveShapingHandles(); live > bound {
			t.Fatalf("re-shaping line %d: LiveShapingHandles = %d exceeds the bound %d", i, live, bound)
		}
	}
	if held.parked {
		t.Fatalf("a caller-held ShapeText line was parked")
	}
	if _, err := held.Glyphs(); err != nil {
		t.Fatalf("the caller-held line: %v", err)
	}

	// The reference rotation: two finishes with no requests release
	// every cached line; parked or not, nothing leaks.
	cache.finishFrame()
	cache.finishFrame()
	if got, want := system.LiveShapingHandles(), baseline+1; got != want {
		t.Fatalf("LiveShapingHandles after the rotation = %d, want %d (only the held line)", got, want)
	}
	if _, err := lines[0].Glyphs(); !errors.Is(err, ErrTextStaleHandle) {
		t.Fatalf("a disposed cached line answered %v, want ErrTextStaleHandle (never resurrected)", err)
	}
}

// TestParkedLineReshapesUnderItsRelayoutConstraints pins the parked
// line's request: a Relayout's wrap width travels with the line, so a
// parked line re-shapes under the constraints it carried.
func TestParkedLineReshapesUnderItsRelayoutConstraints(t *testing.T) {
	system, err := DefaultTextSystem()
	if err != nil {
		t.Fatalf("DefaultTextSystem: %v", err)
	}
	var cache shapedLineCache
	defer cache.dispose()
	text := "a long line of text that wraps into several rows at a narrow width"
	line, err := cache.layoutWrappedLine(system, keyRequest(text, 16, DefaultFontDescriptor(), nil, nil))
	if err != nil {
		t.Fatalf("layoutWrappedLine: %v", err)
	}
	narrow := float32(80)
	if err := line.Relayout(&narrow, nil); err != nil {
		t.Fatalf("Relayout: %v", err)
	}
	wrapped, _ := line.Summary()
	if wrapped.LineCount < 2 {
		t.Fatalf("LineCount at width %v = %d, want a wrapped layout", narrow, wrapped.LineCount)
	}
	before := system.LiveShapingHandles()
	for !line.parked {
		if !system.parkOne(nil) {
			t.Fatalf("could not park the cached line")
		}
	}
	if got := system.LiveShapingHandles(); got >= before {
		t.Fatalf("parking released nothing: %d live, was %d", got, before)
	}
	row, err := line.Line(wrapped.LineCount-1, 20)
	if err != nil {
		t.Fatalf("Line on a parked line: %v", err)
	}
	if row.Index != wrapped.LineCount-1 {
		t.Fatalf("row index = %d, want %d", row.Index, wrapped.LineCount-1)
	}
	again, _ := line.Summary()
	if again.LineCount != wrapped.LineCount || again.Width != wrapped.Width {
		t.Fatalf("re-shaped summary = %+v, want the wrapped %+v", again, wrapped)
	}
	if again.WrapWidth == nil || *again.WrapWidth != narrow {
		t.Fatalf("re-shaped wrap width = %v, want %v", again.WrapWidth, narrow)
	}
}
