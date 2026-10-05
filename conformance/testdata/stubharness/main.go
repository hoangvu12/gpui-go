// Command stubharness is a test double for the pinned Rust reference
// harness (gpui-reference-harness). It exists only so the Go conformance
// runner can be exercised without a Rust toolchain: it accepts the same CLI
// contract (<envelope.json> <trace-out.json>), validates and hashes the
// envelope the same way, and writes a trace in the reference wire format
// with the reference harness's recorded identity constants.
//
// The identity constants mirror reference/harness/src/trace.rs
// (GPUI_CE_COMMIT, GPUI_CE_CRATE, HARNESS_PROFILE) and the crate identity
// in reference/harness/Cargo.toml. Keep them in sync if the pin moves.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"gpui-go/conformance"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: stubharness <envelope.json> <trace-out.json>")
		os.Exit(2)
	}
	envelopePath, traceOutPath := os.Args[1], os.Args[2]

	env, err := conformance.LoadEnvelope(envelopePath)
	if err != nil {
		fail(err)
	}
	if env.FixtureKind != "layout-effects-v1" {
		fail(fmt.Errorf("unknown fixture kind %q", env.FixtureKind))
	}
	envelopeSHA, err := conformance.SHA256Envelope(envelopePath)
	if err != nil {
		fail(err)
	}

	trace := &conformance.Trace{
		Schema:         conformance.TraceSchema,
		FixtureID:      env.FixtureID,
		FixtureKind:    env.FixtureKind,
		EnvelopeSHA256: envelopeSHA,
		Harness: conformance.HarnessInfo{
			HarnessVersion: "0.1.0",
			HarnessCrate:   "gpui-reference-harness",
			GpuiCrate:      "gpui-ce 0.2.2",
			GpuiCommit:     "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a",
			Profile:        "test",
			Features:       []string{"default", "test-support"},
		},
		Events: []conformance.TraceEvent{
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
					"x":      conformance.F32Bits(0),
					"y":      conformance.F32Bits(0),
					"width":  conformance.F32Bits(300),
					"height": conformance.F32Bits(200),
				},
			},
		},
	}

	out, err := json.MarshalIndent(trace, "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(traceOutPath, out, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("stub harness wrote %s (%d bytes)\n", traceOutPath, len(out))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "stubharness:", err)
	os.Exit(1)
}
