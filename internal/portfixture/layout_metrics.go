package portfixture

// This file implements the layout-metrics-v1 fixture runner: one case at
// a time, each in its own window-equivalent scope (a fresh layout engine,
// mirroring the reference's one TestAppContext window per case), with the
// extended style key set, deterministic measurement functions, rem
// overrides applied at request time, per-axis available-space modes, and
// the case-begin/measure-query/measure-result/layout-bounds/case-end
// event sequence. It mirrors reference/harness/src/fixtures/
// layout_metrics.rs run_case op for op.

import (
	"fmt"

	"gpui-go/conformance"
	"gpui-go/gpui"
)

// RunLayoutMetrics executes the envelope's layout cases against the real
// gpui layout adapter and returns the port-side trace (with the port
// harness identity and no envelope hash: run metadata belongs to the
// caller, which pins the envelope bytes it executed).
func RunLayoutMetrics(envelope *conformance.Envelope) (*conformance.Trace, error) {
	if envelope == nil {
		return nil, fmt.Errorf("portfixture: nil envelope")
	}
	if envelope.FixtureKind != conformance.FixtureKindLayoutMetrics {
		return nil, fmt.Errorf("portfixture: unsupported fixture kind %q", envelope.FixtureKind)
	}
	inputs := envelope.Inputs.LayoutCases
	if inputs == nil {
		return nil, fmt.Errorf("portfixture: fixture kind layout-metrics-v1 requires inputs.layout_cases")
	}

	rec := &recorder{}
	for i := range inputs.Cases {
		// The reference panics with the case label on failure; the port
		// returns the error with it.
		if err := runLayoutMetricsCase(&inputs.Cases[i], rec); err != nil {
			return nil, fmt.Errorf("layout-metrics case %q failed: %w", inputs.Cases[i].Label, err)
		}
	}

	trace := &conformance.Trace{
		Schema:      conformance.TraceSchema,
		FixtureID:   envelope.FixtureID,
		FixtureKind: envelope.FixtureKind,
		Harness: conformance.HarnessInfo{
			HarnessVersion: "0.1.0",
			HarnessCrate:   "gpui-go-portfixture",
			GpuiCrate:      "gpui-ce 0.2.2",
			GpuiCommit:     pinnedGpuiCommit,
			Profile:        "test",
		},
		Events: rec.snapshot(),
	}
	return trace, nil
}

// runLayoutMetricsCase executes one case, mirroring the reference
// run_case: validate the measurement specs, open a fresh window scope (a
// fresh layout engine in the port), record the case-begin event with the
// label, rem size and scale, build and lay the style tree out under the
// case's available space (per-axis modes), record every measure query and
// result and every node's bounds, and close with the case-end event.
func runLayoutMetricsCase(c *conformance.LayoutMetricsCase, rec *recorder) error {
	if err := validateLayoutMetricsCase(c); err != nil {
		return err
	}
	if c.StyleTree == nil {
		return fmt.Errorf("case %q has no style tree", c.Label)
	}

	// A fresh engine per case: the reference opens a new window per case,
	// and each window owns its own layout engine.
	engine, err := gpui.NewLayoutEngine()
	if err != nil {
		return fmt.Errorf("layout engine: %w", err)
	}
	defer engine.Dispose()

	// The scale is part of the fixture semantics: the reference test
	// window's fixed scale factor, read from the actual window.
	scale := gpui.TestScaleFactor
	remSize := float64(gpui.DefaultRemSize)
	if c.RemSize != nil {
		remSize = *c.RemSize
	}
	rec.eventWith("case-begin", c.Label,
		field{"label", c.Label},
		field{"remSize", conformance.F32Bits(float32(remSize))},
		field{"scale", conformance.F32Bits(scale)},
	)

	// The rem override must be active while the tree's layouts are
	// requested: the layout engine resolves rem-based lengths from the
	// rem scope at request time (the reference scopes with_rem_size
	// around its tree construction).
	ctx := gpui.NewLayoutContext(float32(remSize), scale)
	runner := &layoutTreeRunner{engine: engine, ctx: ctx, rec: rec}

	var flat []flatStyleNode
	root, err := flattenStyleTree(c.StyleTree, &flat, true)
	if err != nil {
		return fmt.Errorf("style tree: %w", err)
	}
	nodes, err := runner.requestTree(flat, root, c.Measured)
	if err != nil {
		return fmt.Errorf("layout tree: %w", err)
	}

	availableWidth, err := axisAvailable(c.AvailableWidthMode, c.AvailableSpace.Width)
	if err != nil {
		return err
	}
	availableHeight, err := axisAvailable(c.AvailableHeightMode, c.AvailableSpace.Height)
	if err != nil {
		return err
	}
	available := gpui.AvailableSize{Width: availableWidth, Height: availableHeight}
	if len(nodes) == 0 {
		return fmt.Errorf("case %q style tree has no nodes", c.Label)
	}
	if err := engine.ComputeLayout(ctx, nodes[0].id, available); err != nil {
		return fmt.Errorf("compute layout: %w", err)
	}
	if err := runner.emitLayoutBounds(nodes); err != nil {
		return err
	}

	rec.event("case-end", c.Label)
	return nil
}

// validateLayoutMetricsCase validates the case's measurement specs before
// any layout runs, mirroring the reference validate_case: each spec's
// kind must be one of the supported kinds, and each measured label must
// name a node of the style tree.
func validateLayoutMetricsCase(c *conformance.LayoutMetricsCase) error {
	if c.StyleTree == nil {
		return nil // reported by the caller with the case label
	}
	var labels []string
	collectStyleTreeLabels(c.StyleTree, &labels)
	for label, spec := range c.Measured {
		switch spec.Kind {
		case conformance.MeasuredKindFixed, conformance.MeasuredKindEchoKnown, conformance.MeasuredKindMinMax:
		default:
			return fmt.Errorf("measured spec %q has unsupported kind %q", label, spec.Kind)
		}
		found := false
		for _, candidate := range labels {
			if candidate == label {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("measured spec %q has no node with that label in the style tree", label)
		}
	}
	return nil
}

// collectStyleTreeLabels walks the tree in preorder collecting node
// labels (reference collect_labels).
func collectStyleTreeLabels(node *conformance.StyleNode, out *[]string) {
	*out = append(*out, node.Label)
	for i := range node.Children {
		collectStyleTreeLabels(&node.Children[i], out)
	}
}
