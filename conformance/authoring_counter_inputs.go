package conformance

import (
	"encoding/json"
	"fmt"
)

// FixtureKindAuthoringCounter identifies the authoring-counter fixture
// kind ("authoring-counter-v1"). It must match the fixture kind the
// reference harness dispatches in reference/harness/src/main.rs.
const FixtureKindAuthoringCounter = "authoring-counter-v1"

// AuthoringCounterInputs carries the per-case inputs of the
// authoring-counter-v1 fixture kind. The JSON field names match the Rust
// reference struct fields exactly.
type AuthoringCounterInputs struct {
	// Cases are executed in order, each with its own app context.
	Cases []AuthoringCounterCase `json:"cases"`
}

// AuthoringCounterCase is one authoring-counter case: a declarative
// counter description — the initial value, two windows with their sizes,
// the counter view's style, an increment script (simulated through
// entity updates, never real input) and per-capture-point observation
// of the rendered windows.
type AuthoringCounterCase struct {
	// Label identifies the case; it is recorded in the case-begin and
	// case-end trace events.
	Label string `json:"label"`
	// InitialValue is the counter model's initial value.
	InitialValue uint64 `json:"initial_value"`
	// Windows are the two windows sharing the model, in opening order.
	Windows []CounterWindowIn `json:"windows"`
	// CounterStyle is the counter view's style (padding, font size,
	// justify content, background).
	CounterStyle CounterStyleIn `json:"counter_style"`
	// Script is the ordered script of increments and window closures.
	Script []CounterScriptOp `json:"script"`
}

// CounterWindowIn is one window of an authoring-counter case.
type CounterWindowIn struct {
	// Label is the window's label ("A"/"B"): it identifies the window in
	// the trace and the script.
	Label string `json:"label"`
	// Width is the window's client width in logical pixels.
	Width float64 `json:"width"`
	// Height is the window's client height in logical pixels.
	Height float64 `json:"height"`
}

// CounterStyleIn is the counter view's style inputs.
type CounterStyleIn struct {
	// Padding is the root div's padding (all edges), e.g. "0.75rem".
	Padding string `json:"padding"`
	// FontSize is the cascaded text font size, e.g. "1.25rem".
	FontSize string `json:"font_size"`
	// Justify is the root's justify content, a reference variant name
	// ("Center").
	Justify string `json:"justify"`
	// Background is the root's background color, 8 hex digits RRGGBBAA.
	Background string `json:"background"`
}

// Counter script op wire tags. They match the kebab-case variant names
// of the Rust reference's internally tagged CounterScriptOp enum.
const (
	OpIncrement   = "increment"
	OpCloseWindow = "close-window"
)

// counterScriptRequiredFields lists the fields each Rust
// CounterScriptOp variant requires on the wire.
var counterScriptRequiredFields = map[string][]string{
	OpIncrement:   {"window", "clicks"},
	OpCloseWindow: {"window"},
}

// CounterScriptOp is one authoring-counter script operation. On the wire
// it is an internally tagged object, e.g.
//
//	{"op": "increment", "window": "A", "clicks": 2}
//
// matching the Rust CounterScriptOp enum (#[serde(tag = "op",
// rename_all = "kebab-case")]).
type CounterScriptOp struct {
	// Op is the operation tag.
	Op string `json:"op"`
	// Window is the window the operation concerns.
	Window string `json:"window"`
	// Clicks is how many simulated clicks an increment applies.
	Clicks uint32 `json:"clicks,omitempty"`
}

// UnmarshalJSON decodes one counter script op, rejecting unknown op tags
// and missing required fields the way the Rust enum deserializer would.
func (op *CounterScriptOp) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("counter script op: %w", err)
	}
	tagRaw, ok := raw["op"]
	if !ok {
		return fmt.Errorf("counter script op: missing \"op\" tag")
	}
	var tag string
	if err := json.Unmarshal(tagRaw, &tag); err != nil {
		return fmt.Errorf("counter script op: \"op\" tag is not a string: %w", err)
	}
	required, known := counterScriptRequiredFields[tag]
	if !known {
		return fmt.Errorf("counter script op: unknown op tag %q", tag)
	}
	for _, field := range required {
		if _, ok := raw[field]; !ok {
			return fmt.Errorf("counter script op %s: missing required field %q", tag, field)
		}
	}
	type plain CounterScriptOp // avoid recursion on the custom unmarshaler
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return fmt.Errorf("counter script op %s: %w", tag, err)
	}
	*op = CounterScriptOp(p)
	return nil
}

// MarshalJSON encodes one counter script op in the exact Rust wire
// shape: the op tag plus the variant's fields.
func (op CounterScriptOp) MarshalJSON() ([]byte, error) {
	m := map[string]any{"op": op.Op}
	switch op.Op {
	case OpIncrement:
		m["window"] = op.Window
		m["clicks"] = op.Clicks
	case OpCloseWindow:
		m["window"] = op.Window
	default:
		return nil, fmt.Errorf("counter script op: unknown op tag %q", op.Op)
	}
	return json.Marshal(m)
}

// IsAuthoringCounterKind reports whether the envelope declares the
// authoring-counter-v1 fixture kind.
func (env *Envelope) IsAuthoringCounterKind() bool {
	return env != nil && env.FixtureKind == FixtureKindAuthoringCounter
}
