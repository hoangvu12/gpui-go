package conformance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// ComparisonResult reports the outcome of an exact trace comparison.
type ComparisonResult struct {
	// Equal is true when the traces match under the conformance rules.
	Equal bool
	// Diff describes the first mismatch with its precise location (event
	// index, seq, name, field name, expected and actual values). It is
	// empty when Equal is true.
	Diff string
	// HarnessNote records non-fatal differences in harness identity
	// (harness version, crate names, profile, features). These do not
	// affect Equal; only a differing gpui_commit does.
	HarnessNote string
}

// CompareTraces compares a recorded reference trace (expected) against a
// port-side trace (actual) under the exact rules of the conformance
// contract:
//
//   - schema, fixture_id, fixture_kind and envelope_sha256 must match
//     exactly; a mismatch fails the comparison.
//   - harness.gpui_commit must match exactly (the pinned GPUI-CE source);
//     differences in other harness identity fields are recorded in
//     HarnessNote but do not fail the comparison.
//   - events must have the same count, and each event must match in seq,
//     name, label, field key set and field values, in order.
//
// Field values compare as f32 bit patterns when both sides are strings of
// the form "f32:XXXXXXXX": both are normalized through ParseF32Bits and
// F32Bits and compared canonically, which is bit equality preserving
// negative zero. All other values compare as raw JSON scalars, where a
// type difference (integer vs string vs bool) is a diff.
//
// The first mismatch is reported; Diff names the event index, seq, event
// name, field name, and both values.
func CompareTraces(expected, actual *Trace) *ComparisonResult {
	res := &ComparisonResult{Equal: true}

	if expected == nil || actual == nil {
		res.Equal = false
		switch {
		case expected == nil && actual == nil:
			res.Diff = "both traces are nil"
		case expected == nil:
			res.Diff = "expected trace is nil"
		default:
			res.Diff = "actual trace is nil"
		}
		return res
	}

	// Identity fields must match exactly.
	for _, c := range []struct{ name, want, got string }{
		{"schema", expected.Schema, actual.Schema},
		{"fixture_id", expected.FixtureID, actual.FixtureID},
		{"fixture_kind", expected.FixtureKind, actual.FixtureKind},
		{"envelope_sha256", expected.EnvelopeSHA256, actual.EnvelopeSHA256},
	} {
		if c.want != c.got {
			return res.failf("%s: expected %q, actual %q", c.name, c.want, c.got)
		}
	}

	// Harness identity: only the pinned gpui source must match; other
	// differences are noted without failing.
	res.HarnessNote = harnessNote(expected.Harness, actual.Harness)
	if expected.Harness.GpuiCommit != actual.Harness.GpuiCommit {
		return res.failf("harness.gpui_commit: expected %q, actual %q", expected.Harness.GpuiCommit, actual.Harness.GpuiCommit)
	}

	// Events: same count, then pairwise in order.
	if len(expected.Events) != len(actual.Events) {
		return res.failf("event count: expected %d events, actual %d", len(expected.Events), len(actual.Events))
	}
	for i := range expected.Events {
		if diff := compareEvent(i, &expected.Events[i], &actual.Events[i]); diff != "" {
			return res.failf("%s", diff)
		}
	}
	return res
}

// failf marks the comparison failed and records the diff message.
func (r *ComparisonResult) failf(format string, args ...any) *ComparisonResult {
	r.Equal = false
	r.Diff = fmt.Sprintf(format, args...)
	return r
}

// compareEvent compares one event pair and returns the first mismatch with
// its location, or "" when the events match.
func compareEvent(i int, e, a *TraceEvent) string {
	if e.Seq != a.Seq {
		return fmt.Sprintf("event[%d]: seq: expected %d, actual %d", i, e.Seq, a.Seq)
	}
	loc := fmt.Sprintf("event[%d] seq=%d", i, e.Seq)
	if e.Name != a.Name {
		return fmt.Sprintf("%s: name: expected %q, actual %q", loc, e.Name, a.Name)
	}
	loc = fmt.Sprintf("%s name=%q", loc, e.Name)
	if e.Label != a.Label {
		return fmt.Sprintf("%s: label: expected %q, actual %q", loc, e.Label, a.Label)
	}

	// Field key sets must match; report a missing or unexpected key by
	// name, in sorted key order for a deterministic first mismatch.
	for _, k := range slices.Sorted(maps.Keys(e.Fields)) {
		if _, ok := a.Fields[k]; !ok {
			return fmt.Sprintf("%s: expected field %q (value %s) missing in actual", loc, k, formatValue(e.Fields[k]))
		}
	}
	for _, k := range slices.Sorted(maps.Keys(a.Fields)) {
		if _, ok := e.Fields[k]; !ok {
			return fmt.Sprintf("%s: actual has unexpected field %q (value %s) not in expected", loc, k, formatValue(a.Fields[k]))
		}
	}
	for _, k := range slices.Sorted(maps.Keys(e.Fields)) {
		if equal, reason := valuesEqual(e.Fields[k], a.Fields[k]); !equal {
			return fmt.Sprintf("%s: field %q: expected %s, actual %s (%s)", loc, k, formatValue(e.Fields[k]), formatValue(a.Fields[k]), reason)
		}
	}
	return ""
}

// valuesEqual compares two field values. When both are f32 bit-pattern
// strings they compare by normalized bits; otherwise they compare as raw
// JSON scalars (canonical JSON text), so an integer on one side and a
// string or boolean on the other is a type mismatch.
func valuesEqual(ev, av any) (equal bool, reason string) {
	if es, ok := ev.(string); ok {
		if as, ok := av.(string); ok && isF32Bits(es) && isF32Bits(as) {
			if canonicalF32(es) == canonicalF32(as) {
				return true, ""
			}
			return false, "f32 bit patterns differ"
		}
	}

	eb, eerr := json.Marshal(ev)
	ab, aerr := json.Marshal(av)
	if eerr != nil || aerr != nil {
		return fmt.Sprintf("%v", ev) == fmt.Sprintf("%v", av), "value is not JSON-comparable"
	}
	if bytes.Equal(eb, ab) {
		return true, ""
	}
	eKind, aKind := scalarKind(ev), scalarKind(av)
	if eKind != aKind {
		return false, fmt.Sprintf("type mismatch: expected %s, actual %s", eKind, aKind)
	}
	return false, fmt.Sprintf("%s values differ", eKind)
}

// canonicalF32 normalizes an f32 bit-pattern string to the canonical
// uppercase form used for bit-equality comparison.
func canonicalF32(s string) string {
	v, err := ParseF32Bits(s)
	if err != nil {
		// Unreachable for strings accepted by isF32Bits.
		return s
	}
	return F32Bits(v)
}

// scalarKind classifies a field value by its JSON scalar type.
func scalarKind(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float32, float64, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return "number"
	default:
		return fmt.Sprintf("%T", v)
	}
}

// formatValue renders a field value for diff messages: strings quoted,
// other scalars as their canonical JSON text.
func formatValue(v any) string {
	if s, ok := v.(string); ok {
		return strconv.Quote(s)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(b)
}

// harnessNote describes differences in harness identity fields that do not
// fail the comparison: harness version, crate names, profile and features.
func harnessNote(expected, actual HarnessInfo) string {
	var parts []string
	add := func(name, want, got string) {
		if want != got {
			parts = append(parts, fmt.Sprintf("harness.%s: expected %q, actual %q", name, want, got))
		}
	}
	add("harness_version", expected.HarnessVersion, actual.HarnessVersion)
	add("harness_crate", expected.HarnessCrate, actual.HarnessCrate)
	add("gpui_crate", expected.GpuiCrate, actual.GpuiCrate)
	add("profile", expected.Profile, actual.Profile)
	if !slices.Equal(expected.Features, actual.Features) {
		parts = append(parts, fmt.Sprintf("harness.features: expected %q, actual %q", expected.Features, actual.Features))
	}
	return strings.Join(parts, "; ")
}
