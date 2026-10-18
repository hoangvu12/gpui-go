package conformance

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// imageCodecsFixturePath is the fx-0008 fixture envelope, and
// imageCodecsRecordedTracePath is the reference trace recorded from it
// by conformance/cmd/recordreference (ticket17's recorded harness
// follow-up, landed with the wave-4 rebuild).
const (
	imageCodecsFixturePath       = "fixtures/fx-0008-image-codecs.json"
	imageCodecsRecordedTracePath = "recorded/fx-0008-image-codecs/trace.json"
)

// TestImageCodecsEnvelope checks the shipped fx-0008 fixture against
// the image-codecs wire format: the kind tag, the seven cases in
// execution order, and the byte-backed inputs the recorded trace
// depends on.
func TestImageCodecsEnvelope(t *testing.T) {
	env, err := LoadEnvelope(imageCodecsFixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope(%s): %v", imageCodecsFixturePath, err)
	}
	if env.FixtureID != "fx-0008-image-codecs" {
		t.Errorf("fixture_id = %q, want fx-0008-image-codecs", env.FixtureID)
	}
	if env.FixtureKind != FixtureKindImageCodecs {
		t.Fatalf("fixture_kind = %q, want %q", env.FixtureKind, FixtureKindImageCodecs)
	}
	if !env.IsImageCodecsKind() {
		t.Errorf("IsImageCodecsKind() = false, want true")
	}
	if env.Inputs.ImageCases == nil {
		t.Fatal("the envelope carries no image_cases inputs")
	}
	wantLabels := []string{
		"png-alpha-16x16",
		"bmp-16x16",
		"gif-anim-3",
		"jpeg-exif-1",
		"jpeg-exif-6",
		"webp-static",
		"webp-anim",
	}
	if len(env.Inputs.ImageCases.Cases) != len(wantLabels) {
		t.Fatalf("image cases = %d, want %d", len(env.Inputs.ImageCases.Cases), len(wantLabels))
	}
	for i, want := range wantLabels {
		got := env.Inputs.ImageCases.Cases[i].Label
		if got != want {
			t.Errorf("case %d label = %q, want %q", i, got, want)
		}
	}
	// Every case carries non-empty hex bytes (self-contained envelope).
	for _, c := range env.Inputs.ImageCases.Cases {
		if len(c.Bytes) < 8 || len(c.Bytes)%2 != 0 {
			t.Errorf("case %q bytes are not a hex string (%d chars)", c.Label, len(c.Bytes))
		}
	}
}

// TestImageCodecsRecordedTracePresent checks the recorded reference
// trace and its envelope identity.
func TestImageCodecsRecordedTracePresent(t *testing.T) {
	trace, err := LoadTrace(imageCodecsRecordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}
	if trace.FixtureID != "fx-0008-image-codecs" {
		t.Errorf("trace fixture_id = %q, want fx-0008-image-codecs", trace.FixtureID)
	}
	if trace.FixtureKind != FixtureKindImageCodecs {
		t.Errorf("trace fixture_kind = %q, want %q", trace.FixtureKind, FixtureKindImageCodecs)
	}
	if err := VerifyEnvelopeSHA256(trace, imageCodecsFixturePath); err != nil {
		t.Errorf("envelope hash mismatch: %v", err)
	}
	// The recorded shape: 7 case/format/decoded triples and 11 frame
	// events (the GIF carries 3 frames, the animated WebP 3, every
	// other case 1: 5 + 3 + 3 = 11).
	names := map[string]int{}
	for _, event := range trace.Events {
		names[event.Name]++
	}
	for _, want := range []struct {
		name  string
		count int
	}{
		{"image-case", 7},
		{"format", 7},
		{"decoded", 7},
		{"frame", 11},
	} {
		if names[want.name] != want.count {
			t.Errorf("trace has %d %q events, want %d", names[want.name], want.name, want.count)
		}
	}
}

// TestImageCodecsNativeDecodeMatchesReference is the fx-0008 gate: the
// native image service (the pinned image-crate graph behind slot 7)
// decodes every envelope case and the port's trace — format sniff,
// frame counts, per-frame dimensions, rational delays and BGRA pixel
// hashes — must match the recorded reference trace exactly
// (CompareTraces, first mismatch reported).
func TestImageCodecsNativeDecodeMatchesReference(t *testing.T) {
	recorded, err := LoadTrace(imageCodecsRecordedTracePath)
	if err != nil {
		t.Skipf("recorded reference trace not present (%v); run conformance/cmd/recordreference after building the reference harness", err)
	}
	env, err := LoadEnvelope(imageCodecsFixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope(%s): %v", imageCodecsFixturePath, err)
	}

	lib, err := native.Load(native.Options{})
	if err != nil {
		t.Skipf("native artifact unavailable (%v)", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	image, err := lib.Image()
	if err != nil {
		t.Fatalf("image service: %v", err)
	}

	envHash, err := SHA256Envelope(imageCodecsFixturePath)
	if err != nil {
		t.Fatalf("hashing the envelope: %v", err)
	}
	trace := Trace{
		Schema:         TraceSchema,
		FixtureID:      env.FixtureID,
		FixtureKind:    env.FixtureKind,
		EnvelopeSHA256: envHash,
		// The port-side trace carries the same pinned-source harness
		// identity the recorded reference does (only gpui_commit
		// participates in the comparison).
		Harness: referenceHarness(),
		Events:  make([]TraceEvent, 0, len(recorded.Events)),
	}
	for _, c := range env.Inputs.ImageCases.Cases {
		bytes, err := hex.DecodeString(c.Bytes)
		if err != nil {
			t.Fatalf("case %q hex decode: %v", c.Label, err)
		}
		emit := func(name string, fields map[string]any) {
			trace.Events = append(trace.Events, TraceEvent{
				Seq:    uint64(len(trace.Events) + 1),
				Name:   name,
				Label:  c.Label,
				Fields: fields,
			})
		}
		emit("image-case", map[string]any{"label": c.Label})

		format, ok := image.ProbeFormat(bytes)
		if !ok {
			emit("sniff-failed", map[string]any{"error": "UnknownFormat"})
			continue
		}
		emit("format", map[string]any{"format": imageFormatTag(format)})

		handle, err := image.DecodeResource(bytes)
		if err != nil {
			t.Fatalf("case %q native decode: %v", c.Label, err)
		}
		frameCount, err := image.FrameCount(handle)
		if err != nil {
			t.Fatalf("case %q frame count: %v", c.Label, err)
		}
		if frameCount > 1 {
			// The animated walk's decoded event (the reference records
			// the frame count only; the frames carry the dimensions).
			emit("decoded", map[string]any{"frames": uint64(frameCount)})
		} else {
			info, err := image.FrameInfo(handle, 0)
			if err != nil {
				t.Fatalf("case %q frame info: %v", c.Label, err)
			}
			emit("decoded", map[string]any{
				"width":  uint64(info.Width),
				"height": uint64(info.Height),
				"frames": uint64(1),
			})
		}
		for index := uint32(0); index < frameCount; index++ {
			info, err := image.FrameInfo(handle, index)
			if err != nil {
				t.Fatalf("case %q frame %d info: %v", c.Label, index, err)
			}
			pixels, err := image.FramePixels(handle, index)
			if err != nil {
				t.Fatalf("case %q frame %d pixels: %v", c.Label, index, err)
			}
			digest := sha256.Sum256(pixels)
			emit("frame", map[string]any{
				"index":        uint64(index),
				"width":        uint64(info.Width),
				"height":       uint64(info.Height),
				"delayNumerMS": uint64(info.DelayNumerMS),
				"delayDenomMS": uint64(info.DelayDenomMS),
				"bgraSHA256":   hex.EncodeToString(digest[:]),
			})
		}
		if err := image.Dispose(handle); err != nil {
			t.Fatalf("case %q dispose: %v", c.Label, err)
		}
	}

	result := CompareTraces(recorded, &trace)
	if !result.Equal {
		t.Fatalf("the native image decode trace does not match the recorded reference: %s", result.Diff)
	}
}

// imageFormatTag maps the ABI format tag onto the trace's tag string
// (the image crate's ImageFormat Debug name, lowercased — the same
// tags the reference harness records).
func imageFormatTag(format native.ImageFormat) string {
	switch format {
	case native.ImageFormatPNG:
		return "png"
	case native.ImageFormatJPEG:
		return "jpeg"
	case native.ImageFormatGIF:
		return "gif"
	case native.ImageFormatWebP:
		return "webp"
	case native.ImageFormatBMP:
		return "bmp"
	default:
		return fmt.Sprintf("format-%d", uint32(format))
	}
}
