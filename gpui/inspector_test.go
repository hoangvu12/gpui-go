package gpui

import (
	"testing"
	"time"
)

// This file is the inspector slice's identity-policy and overlay unit
// tests (ticket27): the long-id value semantics, the capability
// surface's invariants and the debug frame overlay's pure readout
// logic. The behavioral corpus (tree, selection, picking, leak and
// cycle diagnostics) lives in internal/inspectorspec.

// TestInspectorElementIDValueIdentity pins the long id's value
// semantics (inspector.rs's derived PartialEq/Hash compare the path
// VALUE and the instance id, not the shared pointer).
func TestInspectorElementIDValueIdentity(t *testing.T) {
	a := &InspectorElementID{path: &InspectorElementPath{
		global:    &GlobalElementID{Path: []ElementID{IntegerElementID(1)}},
		globalKey: "1",
		location:  SourceLocation{File: "f.go", Line: 10},
	}, instanceID: 2}
	// The same path and instance in a distinct allocation are equal.
	b := &InspectorElementID{path: &InspectorElementPath{
		global:    &GlobalElementID{Path: []ElementID{IntegerElementID(1)}},
		globalKey: "1",
		location:  SourceLocation{File: "f.go", Line: 10},
	}, instanceID: 2}
	if !a.Equal(*b) || a.String() != b.String() {
		t.Errorf("value-equal ids differ: %s vs %s", a, b)
	}
	// A different instance id is a different element (the
	// disambiguation of same-path elements).
	c := &InspectorElementID{path: a.path, instanceID: 3}
	if a.Equal(*c) {
		t.Errorf("instance ids must disambiguate: %s == %s", a, c)
	}
	// A different path (source location or global path) is different.
	d := &InspectorElementID{path: &InspectorElementPath{
		global:    &GlobalElementID{Path: []ElementID{IntegerElementID(1)}},
		globalKey: "1",
		location:  SourceLocation{File: "f.go", Line: 11},
	}, instanceID: 2}
	if a.Equal(*d) {
		t.Errorf("source locations must disambiguate: %s == %s", a, d)
	}
	e := &InspectorElementID{path: &InspectorElementPath{
		global:    &GlobalElementID{Path: []ElementID{IntegerElementID(2)}},
		globalKey: "2",
		location:  SourceLocation{File: "f.go", Line: 10},
	}, instanceID: 2}
	if a.Equal(*e) {
		t.Errorf("global paths must disambiguate: %s == %s", a, e)
	}
	// The zero value is a valid "no inspector identity" (the pin's
	// empty release struct compares equal only to itself).
	var zero InspectorElementID
	if zero.String() != "<no inspector id>" || zero.Equal(*a) {
		t.Errorf("zero id misbehaves: %q Equal=%v", zero.String(), zero.Equal(*a))
	}
	if zero.SourceLocation() != (SourceLocation{}) {
		t.Error("the zero id must not report a source location")
	}
}

// TestInspectorPathAccessors pins the path's accessors.
func TestInspectorPathAccessors(t *testing.T) {
	path := &InspectorElementPath{
		global:    &GlobalElementID{Path: []ElementID{NameElementID("main"), IntegerElementID(3)}},
		globalKey: "main.3",
		location:  SourceLocation{File: "f.go", Line: 7},
	}
	if got := path.key(); got != "main.3\x00f.go:7" {
		t.Errorf("path key = %q", got)
	}
	if path.Global().String() != "main.3" {
		t.Errorf("path global = %q", path.Global().String())
	}
	if path.Location().Line != 7 {
		t.Errorf("path location = %+v", path.Location())
	}
	var nilPath *InspectorElementPath
	if nilPath.key() != "" || nilPath.Global() != nil || nilPath.Location() != (SourceLocation{}) {
		t.Error("the nil path must degrade to zero answers")
	}
}

// TestDiagnosticCapabilitySwitch pins the capability surface's
// invariants: the documented default (the cargo-test profile), bit
// flips and the full-set profile switch.
func TestDiagnosticCapabilitySwitch(t *testing.T) {
	defer SetDiagnosticCapabilities(CapabilitiesTestProfile)
	if got := DiagnosticCapabilities(); got != CapabilitiesTestProfile {
		t.Fatalf("default = %v, want the test profile", got)
	}
	SetDiagnosticCapability(CapFrameOverlay, true)
	if !DiagnosticCapabilityEnabled(CapFrameOverlay) || !inspectorIDsActive() {
		t.Error("enabling one bit must not clear the others")
	}
	SetDiagnosticCapability(CapInspectorIDs, false)
	if inspectorIDsActive() || !DiagnosticCapabilityEnabled(CapFrameOverlay) {
		t.Error("disabling one bit must not clear the others")
	}
	SetDiagnosticCapabilities(CapabilitiesReleaseProfile)
	if DiagnosticCapabilities() != 0 || inspectorIDsActive() {
		t.Error("the release profile must clear every diagnostic family")
	}
}

// TestDebugOverlayFormatAndGlyphs pins the pure readout helpers: the
// ms formatting (right-aligned, the None placeholder) and that the
// format's characters are all covered by the glyph table.
func TestDebugOverlayFormatAndGlyphs(t *testing.T) {
	one := time.Millisecond
	got := formatMsOverlay(&one)
	if got != "  1.0 MS" {
		t.Errorf("formatMs(1ms) = %q", got)
	}
	if got := formatMsOverlay(nil); got != "   -- MS" {
		t.Errorf("formatMs(nil) = %q", got)
	}
	durations := []time.Duration{time.Microsecond, 8333 * time.Microsecond, 123 * time.Millisecond, 2 * time.Second}
	for _, d := range durations {
		line := formatMsOverlay(&d)
		for i := 0; i < len(line); i++ {
			c := line[i]
			if c == ' ' || c == 'M' || c == 'S' {
				// 'M'/'S' are the unit suffix; the readout's glyph
				// table covers only the digits/decimal/percent forms
				// the pin's lines produce.
				continue
			}
			if _, ok := overlayGlyph(c); !ok {
				t.Errorf("no glyph for %q in %q", c, line)
			}
		}
	}
	// The pin's mode cycle.
	if DebugOverlayHidden.Next() != DebugOverlayMinimal || DebugOverlayMinimal.Next() != DebugOverlayFull || DebugOverlayFull.Next() != DebugOverlayHidden {
		t.Error("the mode cycle must be Hidden -> Minimal -> Full -> Hidden")
	}
}

// TestDebugOverlayLinesDeterminism pins the full-mode percentile
// readout over a synthetic sample set (the pin's
// percentile_lows_are_reported_as_times, exercised through the
// overlay's own lines without a window).
func TestDebugOverlayLinesDeterminism(t *testing.T) {
	overlay := &debugFrameOverlay{mode: DebugOverlayFull}
	for ms := 1; ms <= 100; ms++ {
		overlay.record(time.Duration(ms) * time.Millisecond)
	}
	lines := overlay.lines()
	want := []string{
		"CUR 100.0 MS",
		"1%   99.0 MS",
		"10%  90.0 MS",
		"MAX 100.0 MS",
		"FRAMES   100",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
	// The sample window saturates at MAX_SAMPLES: the sample count
	// stays capped while the frame count keeps growing.
	for i := 0; i < debugOverlayMaxSamples; i++ {
		overlay.record(time.Millisecond)
	}
	if len(overlay.drawDurations) != debugOverlayMaxSamples {
		t.Errorf("sample count = %d, want the %d cap", len(overlay.drawDurations), debugOverlayMaxSamples)
	}
	if overlay.totalFrameCount != uint64(100+debugOverlayMaxSamples) {
		t.Errorf("total frames = %d", overlay.totalFrameCount)
	}
}
