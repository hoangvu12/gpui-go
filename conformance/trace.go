package conformance

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strconv"
)

// TraceSchema identifies the trace format version. It must match
// TRACE_SCHEMA in reference/harness/src/trace.rs.
const TraceSchema = "gpui-go/conformance/trace@1"

// Trace is a recorded, deterministic observation of one fixture run,
// produced by the reference harness or by the Go port under comparison.
//
// Determinism rules (mirroring the Rust trace module): events appear in the
// exact order they occurred and are numbered by seq; field maps serialize
// with sorted keys; every f32 that participates in a comparison is encoded
// as the exact bit-pattern string "f32:XXXXXXXX"; no timestamps, addresses,
// allocation ids or thread ids appear in traces (run metadata lives in the
// separate run-meta.json written by the runner).
type Trace struct {
	Schema         string       `json:"schema"`
	FixtureID      string       `json:"fixture_id"`
	FixtureKind    string       `json:"fixture_kind"`
	EnvelopeSHA256 string       `json:"envelope_sha256"`
	Harness        HarnessInfo  `json:"harness"`
	Events         []TraceEvent `json:"events"`
}

// HarnessInfo identifies the harness binary that recorded the trace and
// the pinned GPUI-CE source it was built against.
type HarnessInfo struct {
	HarnessVersion string   `json:"harness_version"`
	HarnessCrate   string   `json:"harness_crate"`
	GpuiCrate      string   `json:"gpui_crate"`
	GpuiCommit     string   `json:"gpui_commit"`
	Profile        string   `json:"profile"`
	Features       []string `json:"features"`
}

// TraceEvent is one recorded observation. Label is the fixture entity or
// tree-node label the event concerns; it is omitted when empty, matching
// the Rust Option<String> skip rule. Field values are JSON scalars:
// integers, strings, booleans, and f32 values pre-rendered with F32Bits.
type TraceEvent struct {
	Seq    uint64         `json:"seq"`
	Name   string         `json:"name"`
	Label  string         `json:"label,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
}

// LoadTrace reads and parses a trace from path, validating its schema
// version.
func LoadTrace(path string) (*Trace, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading trace %s: %w", path, err)
	}
	var trace Trace
	if err := json.Unmarshal(raw, &trace); err != nil {
		return nil, fmt.Errorf("parsing trace %s: %w", path, err)
	}
	if trace.Schema != TraceSchema {
		return nil, fmt.Errorf("unsupported trace schema %q (expected %q) in %s", trace.Schema, TraceSchema, path)
	}
	return &trace, nil
}

// F32Bits formats v as the exact bit-pattern string used by the trace
// schema: "f32:" followed by eight uppercase hex digits of the IEEE-754
// bit pattern (value.to_bits() in Rust). Signed zero keeps its sign bit.
func F32Bits(v float32) string {
	return fmt.Sprintf("f32:%08X", math.Float32bits(v))
}

// ParseF32Bits parses the exact bit-pattern string produced by F32Bits. It
// accepts exactly "f32:" followed by eight hex digits (case-insensitive
// digits; the prefix itself is lowercase as written by the Rust harness).
func ParseF32Bits(s string) (float32, error) {
	if !isF32Bits(s) {
		return 0, fmt.Errorf("invalid f32 bit pattern %q: want \"f32:\" followed by 8 hex digits", s)
	}
	bits, err := strconv.ParseUint(s[len("f32:"):], 16, 32)
	if err != nil {
		// Unreachable for strings accepted by isF32Bits; kept defensive.
		return 0, fmt.Errorf("invalid f32 bit pattern %q: %v", s, err)
	}
	return math.Float32frombits(uint32(bits)), nil
}

// isF32Bits reports whether s is exactly "f32:" followed by eight hex
// digits, case-insensitive.
func isF32Bits(s string) bool {
	if len(s) != len("f32:")+8 || s[:4] != "f32:" {
		return false
	}
	for i := 4; i < len(s); i++ {
		if !isHexDigit(s[i]) {
			return false
		}
	}
	return true
}

func isHexDigit(c byte) bool {
	return '0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F'
}

// VerifyEnvelopeSHA256 checks that the envelope hash recorded in trace
// matches the SHA-256 of the raw bytes of the envelope at envelopePath. A
// mismatch means the trace was recorded against different fixture bytes.
func VerifyEnvelopeSHA256(trace *Trace, envelopePath string) error {
	if trace == nil {
		return fmt.Errorf("verifying envelope sha256: nil trace")
	}
	got, err := SHA256Envelope(envelopePath)
	if err != nil {
		return err
	}
	if trace.EnvelopeSHA256 != got {
		return fmt.Errorf("envelope sha256 mismatch: trace records %q but envelope %s hashes to %q", trace.EnvelopeSHA256, envelopePath, got)
	}
	return nil
}
