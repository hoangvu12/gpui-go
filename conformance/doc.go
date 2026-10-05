// Package conformance provides the versioned fixture envelope and trace
// formats shared between the pinned CE reference harness and the Go port,
// together with the exact trace comparator and the runner that executes the
// reference harness and retains raw run evidence.
//
// Wire compatibility is defined by the pinned Rust reference harness:
// reference/harness/src/envelope.rs (schema "gpui-go/conformance/envelope@1")
// and reference/harness/src/trace.rs (schema "gpui-go/conformance/trace@1").
// The JSON types in this package must match those definitions exactly:
// field names use the Rust structs' plain snake_case names, effect ops are
// internally tagged objects ("op" with kebab-case variant names), and trace
// event field maps serialize with sorted keys like Rust's BTreeMap. Any
// change to these shapes requires a schema version bump on both sides.
//
// Exact comparison rules follow docs/conformance-contract.md: every f32
// value that participates in a comparison is encoded as "f32:XXXXXXXX"
// (eight uppercase hex digits of the IEEE-754 bit pattern) and compared by
// bit equality, preserving signed zero; events are compared in order by
// seq, name, label, field key set and field values; the first mismatch is
// reported with its precise location.
//
// This package is deliberately low-level: it depends only on the standard
// library and must not import the gpui root package. The root package will
// import conformance later to emit port-side traces for comparison against
// recorded reference traces.
package conformance
