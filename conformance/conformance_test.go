package conformance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

// fixturePath is the fixture used throughout the tests; the test binary
// runs with the package directory as its working directory.
const fixturePath = "fixtures/fx-0001-layout-effects.json"

// referenceHarness returns the harness identity the pinned Rust reference
// harness records in its traces (reference/harness/src/trace.rs and
// reference/harness/Cargo.toml).
func referenceHarness() HarnessInfo {
	return HarnessInfo{
		HarnessVersion: "0.1.0",
		HarnessCrate:   "gpui-reference-harness",
		GpuiCrate:      "gpui-ce 0.2.2",
		GpuiCommit:     "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a",
		Profile:        "test",
		Features:       []string{"default", "test-support"},
	}
}

// testTrace builds a small layout/effects trace with f32 fields rendered
// through F32Bits plus integer, string and fieldless events.
func testTrace(t *testing.T) *Trace {
	t.Helper()
	sha, err := SHA256Envelope(fixturePath)
	if err != nil {
		t.Fatalf("SHA256Envelope: %v", err)
	}
	return &Trace{
		Schema:         TraceSchema,
		FixtureID:      "fx-0001-layout-effects",
		FixtureKind:    "layout-effects-v1",
		EnvelopeSHA256: sha,
		Harness:        referenceHarness(),
		Events: []TraceEvent{
			{
				Seq:   1,
				Name:  "layout-bounds",
				Label: "root",
				Fields: map[string]any{
					"x":      F32Bits(0),
					"y":      F32Bits(0),
					"width":  F32Bits(200),
					"height": F32Bits(100),
				},
			},
			{
				Seq:    2,
				Name:   "entity-created",
				Label:  "counter",
				Fields: map[string]any{"value": uint64(0)},
			},
			{
				Seq:    3,
				Name:   "update-returned",
				Label:  "counter",
				Fields: map[string]any{"op": "notify", "count": uint64(2)},
			},
			{Seq: 4, Name: "effects-run"},
		},
	}
}

// cloneTrace deep-copies a trace through its JSON wire form.
func cloneTrace(t *testing.T, tr *Trace) *Trace {
	t.Helper()
	data, err := json.Marshal(tr)
	if err != nil {
		t.Fatalf("marshaling trace: %v", err)
	}
	var out Trace
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshaling trace: %v", err)
	}
	return &out
}

func countStyleNodes(n StyleNode) int {
	total := 1
	for _, c := range n.Children {
		total += countStyleNodes(c)
	}
	return total
}

// TestEnvelopeFixture checks the shipped fx-0001 fixture against the
// envelope wire format.
func TestEnvelopeFixture(t *testing.T) {
	env, err := LoadEnvelope(fixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope(%s): %v", fixturePath, err)
	}
	if env.Schema != EnvelopeSchema {
		t.Errorf("schema = %q, want %q", env.Schema, EnvelopeSchema)
	}
	if env.FixtureID != "fx-0001-layout-effects" {
		t.Errorf("fixture_id = %q, want %q", env.FixtureID, "fx-0001-layout-effects")
	}
	if env.FixtureKind != "layout-effects-v1" {
		t.Errorf("fixture_kind = %q, want %q", env.FixtureKind, "layout-effects-v1")
	}
	if env.Profile != "test" {
		t.Errorf("profile = %q, want %q", env.Profile, "test")
	}
	if len(env.Capabilities) != 9 {
		t.Errorf("len(capabilities) = %d, want 9", len(env.Capabilities))
	}
	if want := []string{"headless", "test-platform"}; !slices.Equal(env.Environment, want) {
		t.Errorf("environment = %q, want %q", env.Environment, want)
	}
	if want := (SizeInput{Width: 640, Height: 480}); env.Inputs.AvailableSpace != want {
		t.Errorf("available_space = %+v, want %+v", env.Inputs.AvailableSpace, want)
	}

	root := env.Inputs.StyleTree
	if root.Label != "root" {
		t.Errorf("style tree root label = %q, want %q", root.Label, "root")
	}
	if len(root.Children) != 4 {
		t.Fatalf("style tree root has %d children, want 4", len(root.Children))
	}
	wantLabels := []string{"a", "b", "c", "d"}
	for i, want := range wantLabels {
		if got := root.Children[i].Label; got != want {
			t.Errorf("child %d label = %q, want %q", i, got, want)
		}
	}
	if got := root.Children[2].Children; len(got) != 1 || got[0].Label != "c1" {
		t.Errorf("child c has children %+v, want single c1", got)
	}
	if got, want := countStyleNodes(root), 6; got != want {
		t.Errorf("style tree has %d nodes, want %d", got, want)
	}

	script := env.Inputs.EffectScript
	if len(script) != 13 {
		t.Fatalf("effect script has %d ops, want 13", len(script))
	}
	if first := script[0]; first.Op != OpCreateEntity || first.Label != "counter" || first.Value != 0 {
		t.Errorf("first op = %+v, want create-entity label counter value 0", first)
	}
	if sub := script[1]; sub.Op != OpSubscribe || sub.Observer != "obs1" || sub.Source != "counter" || sub.EventType != "tick" {
		t.Errorf("subscribe op = %+v, want observer obs1 source counter event_type tick", sub)
	}
	var notify, emit, deferOp, spawn *EffectOp
	for i := range script {
		switch script[i].Op {
		case OpNotify:
			notify = &script[i]
		case OpEmit:
			emit = &script[i]
		case OpDefer:
			deferOp = &script[i]
		case OpSpawnTask:
			spawn = &script[i]
		}
	}
	if notify == nil || notify.Entity != "counter" || notify.Count != 2 {
		t.Errorf("notify op = %+v, want entity counter count 2", notify)
	}
	if emit == nil || !slices.Equal(emit.Values, []uint32{1, 2, 3}) {
		t.Errorf("emit op = %+v, want values [1 2 3]", emit)
	}
	if deferOp == nil || deferOp.Schedule == nil || *deferOp.Schedule != "d1b" {
		t.Errorf("defer op = %+v, want label d1 schedule d1b", deferOp)
	}
	if spawn == nil || spawn.Label != "t1" || spawn.Result != 42 || spawn.DelayMs != 50 {
		t.Errorf("spawn-task op = %+v, want label t1 result 42 delay_ms 50", spawn)
	}

	sha, err := SHA256Envelope(fixturePath)
	if err != nil {
		t.Fatalf("SHA256Envelope: %v", err)
	}
	if len(sha) != 64 || strings.ToLower(sha) != sha {
		t.Errorf("envelope sha256 = %q, want 64 lowercase hex digits", sha)
	}
	if again, _ := SHA256Envelope(fixturePath); again != sha {
		t.Errorf("envelope sha256 not stable: %q vs %q", sha, again)
	}
}

// TestEnvelopeRoundTrip checks that a loaded envelope marshals back into
// the same wire shape (op tags, required fields, snake_case keys).
func TestEnvelopeRoundTrip(t *testing.T) {
	env, err := LoadEnvelope(fixturePath)
	if err != nil {
		t.Fatalf("LoadEnvelope: %v", err)
	}
	data, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshaling envelope: %v", err)
	}
	var again Envelope
	if err := json.Unmarshal(data, &again); err != nil {
		t.Fatalf("unmarshaling envelope: %v", err)
	}
	if !reflect.DeepEqual(*env, again) {
		t.Errorf("envelope round-trip mismatch:\ngot  %+v\nwant %+v", again, *env)
	}
}

// TestF32Bits checks the f32 bit-pattern encoding and decoding.
func TestF32Bits(t *testing.T) {
	if got, want := F32Bits(float32(math.Copysign(0, -1))), "f32:80000000"; got != want {
		t.Errorf("F32Bits(-0) = %q, want %q", got, want)
	}
	if got, want := F32Bits(200.0), "f32:43480000"; got != want {
		t.Errorf("F32Bits(200) = %q, want %q", got, want)
	}
	if got, want := F32Bits(100.0), "f32:42C80000"; got != want {
		t.Errorf("F32Bits(100) = %q, want %q", got, want)
	}
	if got, want := F32Bits(0), "f32:00000000"; got != want {
		t.Errorf("F32Bits(0) = %q, want %q", got, want)
	}
	if got, err := ParseF32Bits("f32:43480000"); err != nil || got != 200 {
		t.Errorf("ParseF32Bits(\"f32:43480000\") = %v, %v, want 200, nil", got, err)
	}

	values := []float32{
		0,
		float32(math.Copysign(0, -1)),
		1,
		-1,
		0.5,
		0.1,
		100,
		200,
		float32(math.Pi),
		math.MaxFloat32,
		math.SmallestNonzeroFloat32,
		float32(math.Inf(1)),
		float32(math.Inf(-1)),
		float32(math.NaN()),
	}
	for _, v := range values {
		got, err := ParseF32Bits(F32Bits(v))
		if err != nil {
			t.Errorf("ParseF32Bits(F32Bits(%v)): %v", v, err)
			continue
		}
		// Compare bit patterns so NaN and signed zero round-trip exactly.
		if math.Float32bits(got) != math.Float32bits(v) {
			t.Errorf("round-trip of %v gave %v", v, got)
		}
	}

	// Hex digits are case-insensitive on parse; the prefix is exact.
	got, err := ParseF32Bits(strings.ToLower(F32Bits(0.1)))
	if err != nil || math.Float32bits(got) != math.Float32bits(0.1) {
		t.Errorf("lowercase parse = %v, %v, want 0.1, nil", got, err)
	}

	// Bit equality preserves negative zero.
	if equal, _ := valuesEqual("f32:80000000", "f32:00000000"); equal {
		t.Error("negative zero must not equal positive zero")
	}
	if equal, _ := valuesEqual("f32:4348000a", "f32:4348000A"); !equal {
		t.Error("case-insensitive f32 hex digits must compare equal")
	}
}

// TestParseF32BitsRejects checks malformed bit patterns fail exactly.
func TestParseF32BitsRejects(t *testing.T) {
	bad := []string{
		"",              // empty
		"f32:",          // no digits
		"f32:4348",      // too few digits
		"43480000",      // missing prefix
		"f32:GGGGGGGG",  // not hex digits
		"f32:434800000", // too many digits
		"F32:43480000",  // wrong prefix case
		"f32:43480000 ", // trailing space
	}
	for _, s := range bad {
		if got, err := ParseF32Bits(s); err == nil {
			t.Errorf("ParseF32Bits(%q) = %v, want error", s, got)
		}
	}
}

// TestTraceRoundTrip marshals a trace with F32Bits field values, unmarshals
// it and compares it exactly, and pins the omitempty wire behavior for
// label and fields.
func TestTraceRoundTrip(t *testing.T) {
	trace := testTrace(t)

	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatalf("marshaling trace: %v", err)
	}
	var round Trace
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshaling trace: %v", err)
	}
	res := CompareTraces(trace, &round)
	if !res.Equal || res.Diff != "" || res.HarnessNote != "" {
		t.Errorf("round-trip comparison failed: Equal=%v Diff=%q HarnessNote=%q", res.Equal, res.Diff, res.HarnessNote)
	}

	// Empty label and empty fields are omitted, matching the Rust
	// Option::is_none / BTreeMap::is_empty skip rules.
	bare, err := json.Marshal(TraceEvent{Seq: 4, Name: "effects-run"})
	if err != nil {
		t.Fatalf("marshaling event: %v", err)
	}
	if want := `{"seq":4,"name":"effects-run"}`; string(bare) != want {
		t.Errorf("bare event marshaled as %s, want %s", bare, want)
	}
	loaded, err := json.Marshal(TraceEvent{Seq: 2, Name: "entity-created", Label: "counter", Fields: map[string]any{"value": uint64(0)}})
	if err != nil {
		t.Fatalf("marshaling event: %v", err)
	}
	if want := `{"seq":2,"name":"entity-created","label":"counter","fields":{"value":0}}`; string(loaded) != want {
		t.Errorf("event marshaled as %s, want %s", loaded, want)
	}
}

// TestCompareTracesMismatchDemonstration is the intentional mismatch
// demonstration: each single mutation of a base layout/effects trace must
// fail with a diff naming the exact event and field.
func TestCompareTracesMismatchDemonstration(t *testing.T) {
	base := testTrace(t)

	// Control: a deep copy compares equal.
	if res := CompareTraces(base, cloneTrace(t, base)); !res.Equal {
		t.Fatalf("identical traces compare unequal: %s (note %q)", res.Diff, res.HarnessNote)
	}

	tests := []struct {
		name         string
		mutate       func(actual *Trace)
		wantContains []string
	}{
		{
			name:   "flip one bit in one f32 field",
			mutate: func(a *Trace) { a.Events[0].Fields["width"] = "f32:43480001" },
			wantContains: []string{
				"event[0]", "seq=1", `name="layout-bounds"`, `field "width"`,
				`"f32:43480000"`, `"f32:43480001"`, "f32 bit patterns differ",
			},
		},
		{
			name:         "swap two consecutive events",
			mutate:       func(a *Trace) { a.Events[0], a.Events[1] = a.Events[1], a.Events[0] },
			wantContains: []string{"event[0]", "seq: expected 1, actual 2"},
		},
		{
			name:         "delete one event",
			mutate:       func(a *Trace) { a.Events = a.Events[:3] },
			wantContains: []string{"event count: expected 4 events, actual 3"},
		},
		{
			name:         "change one integer field value",
			mutate:       func(a *Trace) { a.Events[2].Fields["count"] = 3 },
			wantContains: []string{"event[2]", "seq=3", `field "count"`, "expected 2", "actual 3"},
		},
		{
			name:         "add an extra field",
			mutate:       func(a *Trace) { a.Events[1].Fields["extra"] = 1 },
			wantContains: []string{"event[1]", "seq=2", `"extra"`, "not in expected"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := cloneTrace(t, base)
			tt.mutate(actual)
			res := CompareTraces(base, actual)
			if res.Equal {
				t.Fatalf("mutation %q must not compare equal", tt.name)
			}
			if res.Diff == "" {
				t.Fatal("Diff is empty on unequal traces")
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(res.Diff, want) {
					t.Errorf("Diff = %q, want substring %q", res.Diff, want)
				}
			}
		})
	}
}

// TestCompareTracesIdentity checks that differing trace identity fields
// fail the comparison with a precise message.
func TestCompareTracesIdentity(t *testing.T) {
	base := testTrace(t)
	tests := []struct {
		name         string
		mutate       func(a *Trace)
		wantContains []string
	}{
		{
			name:         "schema",
			mutate:       func(a *Trace) { a.Schema = "gpui-go/conformance/trace@2" },
			wantContains: []string{`schema: expected "gpui-go/conformance/trace@1", actual "gpui-go/conformance/trace@2"`},
		},
		{
			name:         "fixture_id",
			mutate:       func(a *Trace) { a.FixtureID = "fx-9999-other" },
			wantContains: []string{`fixture_id: expected "fx-0001-layout-effects", actual "fx-9999-other"`},
		},
		{
			name:         "fixture_kind",
			mutate:       func(a *Trace) { a.FixtureKind = "scene-v1" },
			wantContains: []string{`fixture_kind: expected "layout-effects-v1", actual "scene-v1"`},
		},
		{
			name:         "envelope_sha256",
			mutate:       func(a *Trace) { a.EnvelopeSHA256 = strings.Repeat("0", 64) },
			wantContains: []string{"envelope_sha256"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := cloneTrace(t, base)
			tt.mutate(actual)
			res := CompareTraces(base, actual)
			if res.Equal {
				t.Fatalf("%s mismatch must fail the comparison", tt.name)
			}
			for _, want := range tt.wantContains {
				if !strings.Contains(res.Diff, want) {
					t.Errorf("Diff = %q, want substring %q", res.Diff, want)
				}
			}
		})
	}
}

// TestCompareTracesHarnessFields checks harness identity rules: a
// differing gpui_commit fails, other harness differences only produce a
// note.
func TestCompareTracesHarnessFields(t *testing.T) {
	base := testTrace(t)

	commitChanged := cloneTrace(t, base)
	commitChanged.Harness.GpuiCommit = "0123456789abcdef0123456789abcdef01234567"
	res := CompareTraces(base, commitChanged)
	if res.Equal {
		t.Fatal("differing gpui_commit must fail the comparison")
	}
	if want := `harness.gpui_commit: expected "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a", actual "0123456789abcdef0123456789abcdef01234567"`; !strings.Contains(res.Diff, want) {
		t.Errorf("Diff = %q, want substring %q", res.Diff, want)
	}

	versionChanged := cloneTrace(t, base)
	versionChanged.Harness.HarnessVersion = "0.2.0"
	res = CompareTraces(base, versionChanged)
	if !res.Equal {
		t.Fatalf("differing harness_version must not fail: %s", res.Diff)
	}
	if want := `harness.harness_version: expected "0.1.0", actual "0.2.0"`; !strings.Contains(res.HarnessNote, want) {
		t.Errorf("HarnessNote = %q, want substring %q", res.HarnessNote, want)
	}

	profileAndFeatures := cloneTrace(t, base)
	profileAndFeatures.Harness.Profile = "release"
	profileAndFeatures.Harness.Features = []string{"default"}
	res = CompareTraces(base, profileAndFeatures)
	if !res.Equal {
		t.Fatalf("differing profile/features must not fail: %s", res.Diff)
	}
	for _, want := range []string{`harness.profile: expected "test", actual "release"`, "harness.features"} {
		if !strings.Contains(res.HarnessNote, want) {
			t.Errorf("HarnessNote = %q, want substring %q", res.HarnessNote, want)
		}
	}
}

// TestRustWireAgreement pins the exact event JSON the Rust harness emits
// for a layout-bounds event: the same fields decode, re-encode and compare
// equal on the Go side.
func TestRustWireAgreement(t *testing.T) {
	const rustEventJSON = `{"seq":1,"name":"layout-bounds","label":"root","fields":{"x":"f32:00000000","y":"f32:00000000","width":"f32:43480000","height":"f32:42C80000"}}`

	var fromRust TraceEvent
	if err := json.Unmarshal([]byte(rustEventJSON), &fromRust); err != nil {
		t.Fatalf("unmarshaling rust event: %v", err)
	}
	if fromRust.Seq != 1 || fromRust.Name != "layout-bounds" || fromRust.Label != "root" {
		t.Errorf("decoded event = %+v", fromRust)
	}
	wantFields := map[string]any{
		"x":      "f32:00000000",
		"y":      "f32:00000000",
		"width":  "f32:43480000",
		"height": "f32:42C80000",
	}
	if !reflect.DeepEqual(fromRust.Fields, wantFields) {
		t.Errorf("decoded fields = %v, want %v", fromRust.Fields, wantFields)
	}

	// Re-marshaling must keep the same fields and key set. Go's map
	// marshaling sorts keys, matching serde's BTreeMap ordering.
	remarshaled, err := json.Marshal(fromRust)
	if err != nil {
		t.Fatalf("marshaling event: %v", err)
	}
	if want := `{"seq":1,"name":"layout-bounds","label":"root","fields":{"height":"f32:42C80000","width":"f32:43480000","x":"f32:00000000","y":"f32:00000000"}}`; string(remarshaled) != want {
		t.Errorf("remarshaled event = %s, want %s", remarshaled, want)
	}

	// Traces built from the Rust JSON, from the re-marshaled JSON and from
	// in-memory F32Bits values all compare equal.
	fromJSON := testTrace(t)
	fromJSON.Events = []TraceEvent{fromRust}

	var again TraceEvent
	if err := json.Unmarshal(remarshaled, &again); err != nil {
		t.Fatalf("unmarshaling remarshaled event: %v", err)
	}
	fromRemarshal := testTrace(t)
	fromRemarshal.Events = []TraceEvent{again}

	if res := CompareTraces(fromJSON, fromRemarshal); !res.Equal {
		t.Errorf("rust vs remarshaled: %s", res.Diff)
	}
	if res := CompareTraces(fromJSON, fromJSON); !res.Equal {
		t.Errorf("self comparison: %s", res.Diff)
	}

	// The same event built in memory through F32Bits agrees with the Rust
	// bit-pattern encoding.
	built := testTrace(t)
	built.Events = []TraceEvent{{
		Seq:   1,
		Name:  "layout-bounds",
		Label: "root",
		Fields: map[string]any{
			"x":      F32Bits(0),
			"y":      F32Bits(0),
			"width":  F32Bits(200),
			"height": F32Bits(100),
		},
	}}
	if res := CompareTraces(fromJSON, built); !res.Equal {
		t.Errorf("rust vs F32Bits-built: %s", res.Diff)
	}
}

// buildStubHarness compiles the test-double reference harness.
func buildStubHarness(t *testing.T) string {
	t.Helper()
	name := "harness"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	exe := filepath.Join(t.TempDir(), name)
	build := exec.Command("go", "build", "-o", exe, "./testdata/stubharness")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building stub harness: %v\n%s", err, out)
	}
	return exe
}

// TestRunReferenceFixture runs the full runner path against the stub
// harness: trace parsing, envelope hash verification and raw evidence
// retention.
func TestRunReferenceFixture(t *testing.T) {
	exe := buildStubHarness(t)
	outDir := t.TempDir()

	res, err := RunReferenceFixture(context.Background(), exe, fixturePath, outDir)
	if err != nil {
		t.Fatalf("RunReferenceFixture: %v", err)
	}

	if res.Trace == nil {
		t.Fatal("RunResult.Trace is nil")
	}
	if res.Trace.FixtureID != "fx-0001-layout-effects" {
		t.Errorf("trace fixture_id = %q", res.Trace.FixtureID)
	}
	if want, err := SHA256Envelope(fixturePath); err != nil || res.Trace.EnvelopeSHA256 != want {
		t.Errorf("trace envelope_sha256 = %q, want %q (%v)", res.Trace.EnvelopeSHA256, want, err)
	}
	if len(res.Trace.Events) != 2 {
		t.Errorf("trace has %d events, want 2", len(res.Trace.Events))
	}
	if !bytes.Contains(res.RawStdout, []byte("stub harness wrote")) {
		t.Errorf("RawStdout = %q, want it to mention the written trace", res.RawStdout)
	}
	if len(res.RawStderr) != 0 {
		t.Errorf("RawStderr = %q, want empty", res.RawStderr)
	}

	// The parsed trace must be exactly what the stub harness recorded.
	expected := testTrace(t)
	expected.Events = []TraceEvent{
		{
			Seq:    1,
			Name:   "entity-created",
			Label:  "counter",
			Fields: map[string]any{"value": uint64(0)},
		},
		{
			Seq:   2,
			Name:  "layout-bounds",
			Label: "root",
			Fields: map[string]any{
				"x":      F32Bits(0),
				"y":      F32Bits(0),
				"width":  F32Bits(300),
				"height": F32Bits(200),
			},
		},
	}
	if cmp := CompareTraces(expected, res.Trace); !cmp.Equal {
		t.Errorf("parsed trace differs from what the stub recorded: %s", cmp.Diff)
	}

	// Raw evidence files exist next to the trace and match the captured
	// streams.
	runDir := filepath.Join(outDir, "fx-0001-layout-effects")
	tracePath := filepath.Join(runDir, "trace.json")
	if _, err := os.Stat(tracePath); err != nil {
		t.Errorf("trace.json missing: %v", err)
	}
	stdoutPath := filepath.Join(runDir, "raw-stdout.txt")
	stderrPath := filepath.Join(runDir, "raw-stderr.txt")
	for _, p := range []string{stdoutPath, stderrPath, res.MetaPath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("evidence file %s missing: %v", p, err)
		}
	}
	if got, err := os.ReadFile(stdoutPath); err != nil || !bytes.Equal(got, res.RawStdout) {
		t.Errorf("raw-stdout.txt = %q (%v), want %q", got, err, res.RawStdout)
	}
	if got, err := os.ReadFile(stderrPath); err != nil || len(got) != 0 {
		t.Errorf("raw-stderr.txt = %q (%v), want empty", got, err)
	}

	// The sidecar records the full run identity.
	metaBytes, err := os.ReadFile(res.MetaPath)
	if err != nil {
		t.Fatalf("reading run-meta.json: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("parsing run-meta.json: %v", err)
	}
	if v, ok := meta["run_started_at"].(string); !ok {
		t.Errorf("run_started_at = %#v, want RFC3339 string", meta["run_started_at"])
	} else if _, err := time.Parse(time.RFC3339, v); err != nil {
		t.Errorf("run_started_at %q is not RFC3339: %v", v, err)
	}
	absExe, err := filepath.Abs(exe)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if meta["harness_exe"] != absExe {
		t.Errorf("harness_exe = %v, want %v", meta["harness_exe"], absExe)
	}
	exeRaw, err := os.ReadFile(exe)
	if err != nil {
		t.Fatalf("reading harness exe: %v", err)
	}
	exeSHA := sha256Hex(exeRaw)
	if meta["harness_sha256"] != exeSHA {
		t.Errorf("harness_sha256 = %v, want %v", meta["harness_sha256"], exeSHA)
	}
	if meta["envelope_path"] != fixturePath {
		t.Errorf("envelope_path = %v, want %v", meta["envelope_path"], fixturePath)
	}
	if want, _ := SHA256Envelope(fixturePath); meta["envelope_sha256"] != want {
		t.Errorf("envelope_sha256 = %v, want %v", meta["envelope_sha256"], want)
	}
	wantArgv := []any{absExe, fixturePath, tracePath}
	if !reflect.DeepEqual(meta["argv"], wantArgv) {
		t.Errorf("argv = %v, want %v", meta["argv"], wantArgv)
	}
	if meta["go_version"] != runtime.Version() {
		t.Errorf("go_version = %v, want %v", meta["go_version"], runtime.Version())
	}
	if meta["os"] != runtime.GOOS {
		t.Errorf("os = %v, want %v", meta["os"], runtime.GOOS)
	}
	if meta["arch"] != runtime.GOARCH {
		t.Errorf("arch = %v, want %v", meta["arch"], runtime.GOARCH)
	}
	if v, ok := meta["exit_error"]; ok && v != nil {
		t.Errorf("exit_error = %v, want absent on success", v)
	}
}

// TestRunReferenceFixtureHarnessFailure checks the failure path: a harness
// that exits non-zero still leaves run-meta.json (with exit_error) and raw
// stderr behind, and surfaces the exit status in the returned error.
func TestRunReferenceFixtureHarnessFailure(t *testing.T) {
	exe := buildStubHarness(t)

	// An envelope whose fixture kind the stub (like the real harness)
	// rejects.
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	bad := bytes.Replace(raw, []byte(`"layout-effects-v1"`), []byte(`"unknown-kind-v9"`), 1)
	if bytes.Equal(bad, raw) {
		t.Fatal("fixture kind substitution failed")
	}
	badPath := filepath.Join(t.TempDir(), "bad-envelope.json")
	if err := os.WriteFile(badPath, bad, 0o644); err != nil {
		t.Fatalf("writing bad envelope: %v", err)
	}

	outDir := t.TempDir()
	_, err = RunReferenceFixture(context.Background(), exe, badPath, outDir)
	if err == nil {
		t.Fatal("RunReferenceFixture must fail when the harness exits non-zero")
	}
	if !strings.Contains(err.Error(), "exit status") {
		t.Errorf("error %q does not mention exit status", err)
	}
	if !strings.Contains(err.Error(), "stubharness") {
		t.Errorf("error %q does not include the harness stderr tail", err)
	}

	runDir := filepath.Join(outDir, "fx-0001-layout-effects")
	metaBytes, err := os.ReadFile(filepath.Join(runDir, "run-meta.json"))
	if err != nil {
		t.Fatalf("run-meta.json missing after failure: %v", err)
	}
	var meta map[string]any
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("parsing run-meta.json: %v", err)
	}
	if v, _ := meta["exit_error"].(string); v == "" {
		t.Errorf("exit_error = %v, want non-empty", meta["exit_error"])
	}
	if want, _ := SHA256Envelope(badPath); meta["envelope_sha256"] != want {
		t.Errorf("envelope_sha256 = %v, want hash of the bad envelope %v", meta["envelope_sha256"], want)
	}
	stderrBytes, err := os.ReadFile(filepath.Join(runDir, "raw-stderr.txt"))
	if err != nil || !bytes.Contains(stderrBytes, []byte("stubharness:")) {
		t.Errorf("raw-stderr.txt = %q (%v), want stubharness error output", stderrBytes, err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "trace.json")); err == nil {
		t.Error("trace.json should not exist after a failed harness run")
	}
}

// sha256Hex returns the lowercase hex SHA-256 of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
