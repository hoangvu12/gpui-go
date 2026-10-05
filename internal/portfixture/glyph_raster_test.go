package portfixture

import (
	"runtime"
	"testing"

	"gpui-go/conformance"
)

const (
	glyphRasterFixturePath       = "../../conformance/fixtures/fx-0005-glyph-raster.json"
	glyphRasterRecordedTracePath = "../../conformance/recorded/fx-0005-glyph-raster/trace.json"
)

func TestFx0005GlyphRasterMatchesRecordedReference(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skipf("the native glyph artifact is Windows AMD64; the raster path cannot run on %s", runtime.GOOS)
	}
	envelope, err := conformance.LoadEnvelope(glyphRasterFixturePath)
	if err != nil {
		t.Fatalf("loading envelope: %v", err)
	}
	trace, err := RunGlyphRaster(envelope)
	if err != nil {
		t.Fatalf("running port glyph raster: %v", err)
	}
	sha, err := conformance.SHA256Envelope(glyphRasterFixturePath)
	if err != nil {
		t.Fatalf("hashing envelope: %v", err)
	}
	trace.EnvelopeSHA256 = sha
	reference, err := conformance.LoadTrace(glyphRasterRecordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}
	result := conformance.CompareTraces(reference, trace)
	if !result.Equal {
		t.Fatalf("glyph raster trace mismatch:\n%s", result.Diff)
	}
}
