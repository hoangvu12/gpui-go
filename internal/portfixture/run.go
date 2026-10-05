// Package portfixture executes the conformance envelopes against the
// real gpui runtime, mirroring the reference fixtures op for op and
// recording the same trace events:
//
//   - layout-effects-v1 (reference/harness/src/fixtures/layout_effects.rs):
//     the effect script through the real entity/executor APIs, then the
//     style tree through the real gpui layout adapter (the Taffy engine
//     behind internal/native), with every node's bounds recorded as
//     exact f32 bit patterns;
//   - layout-metrics-v1 (reference/harness/src/fixtures/
//     layout_metrics.rs): per-case style trees with the extended style
//     key set, deterministic measurement functions, rem overrides and
//     per-axis available-space modes (RunLayoutMetrics).
package portfixture

import (
	"fmt"
	"sync"
	"time"

	"gpui-go/conformance"
	"gpui-go/gpui"
)

// pinnedGpuiCommit is the GPUI-CE source the port targets; it must match
// the recorded reference traces' harness.gpui_commit.
const pinnedGpuiCommit = "254b5dbd47cbb5acbcc5bbdcbb322a339276c88a"

// Counter is the entity used by the effects section.
type Counter struct {
	value uint64
}

// TickEvent is the typed event payload emitted by Counter.
type TickEvent struct {
	value uint32
}

// Observer subscribes to Counter events and observes Counter changes.
type Observer struct {
	receivedEvents uint32
	notified       uint32
}

// tickEvent is the declared Counter/TickEvent event descriptor.
var tickEvent = gpui.DefineEvent[Counter, TickEvent]()

// field is one trace event field.
type field struct {
	name  string
	value any
}

// recorder is the thread-safe ordered event recorder: background task
// bodies record concurrently with foreground code, so events are appended
// under a lock and the resulting order is the actual completion order
// (mirroring the reference TraceRecorder).
type recorder struct {
	mu     sync.Mutex
	events []conformance.TraceEvent
	seq    uint64
}

func (r *recorder) event(name string, label string) {
	r.eventWith(name, label)
}

func (r *recorder) eventWith(name string, label string, fields ...field) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	ev := conformance.TraceEvent{Seq: r.seq, Name: name, Label: label}
	if len(fields) > 0 {
		ev.Fields = make(map[string]any, len(fields))
		for _, f := range fields {
			ev.Fields[f.name] = f.value
		}
	}
	r.events = append(r.events, ev)
}

func (r *recorder) snapshot() []conformance.TraceEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]conformance.TraceEvent, len(r.events))
	copy(out, r.events)
	return out
}

// effectFixture holds the entities created by the effect script, like the
// reference fixture's EffectFixture.
type effectFixture struct {
	root      *gpui.Scope
	counters  map[string]gpui.Entity[Counter]
	observers map[string]gpui.Entity[Observer]
}

func newEffectFixture(root *gpui.Scope) *effectFixture {
	return &effectFixture{
		root:      root,
		counters:  make(map[string]gpui.Entity[Counter]),
		observers: make(map[string]gpui.Entity[Observer]),
	}
}

// Run executes the envelope's effect script against the real gpui APIs
// and then lays the envelope's style tree out through the real gpui
// layout adapter, mirroring the reference fixture end to end. It returns
// the port-side trace (with the port harness identity and no envelope
// hash: run metadata belongs to the caller, which pins the envelope bytes
// it executed) and the pending capabilities this slice does not implement
// (none: the layout capabilities are implemented by the layout adapter).
//
// Every op is recorded with op-begin/op-end markers so deliveries can be
// attributed to their triggering update, exactly like the reference
// fixture.
func Run(envelope *conformance.Envelope) (*conformance.Trace, []string, error) {
	if envelope == nil {
		return nil, nil, fmt.Errorf("portfixture: nil envelope")
	}
	if envelope.FixtureKind != "layout-effects-v1" {
		return nil, nil, fmt.Errorf("portfixture: unsupported fixture kind %q", envelope.FixtureKind)
	}

	ta := gpui.NewTestApp()
	rec := &recorder{}
	fixture := newEffectFixture(ta.RootScope())

	for i := range envelope.Inputs.EffectScript {
		op := &envelope.Inputs.EffectScript[i]
		rec.eventWith("op-begin", "", field{"op", op.Op})
		if err := fixture.runOp(ta, op, rec); err != nil {
			return nil, nil, fmt.Errorf("fixture op %s: %w", op.Op, err)
		}
		rec.event("op-end", "")
	}

	// Layout section (the reference fixture's window draw): lay the style
	// tree out as the root element under the envelope's definite available
	// space through the real gpui layout adapter, in the test-profile
	// layout context (the reference test window's hardcoded scale factor
	// 2.0 and the default 16px rem size), and record every node's computed
	// bounds.
	if err := runLayoutEffectsSection(envelope, rec); err != nil {
		return nil, nil, err
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
	return trace, nil, nil
}

// runLayoutEffectsSection executes the layout section of the
// layout-effects-v1 fixture: request the style tree (children first, then
// the parent with their ids), record one layout-requested event with the
// root's label, compute under the envelope's definite available space,
// and record one layout-bounds event per node in tree order. This mirrors
// the reference fixture's StyleTreeProbe: request_layout builds the tree,
// layout_as_root computes it with the drawn available space, and the
// prepaint loop queries window.layout_bounds for every node.
func runLayoutEffectsSection(envelope *conformance.Envelope, rec *recorder) error {
	engine, err := gpui.NewLayoutEngine()
	if err != nil {
		return fmt.Errorf("portfixture: layout engine: %w", err)
	}
	defer engine.Dispose()

	// The reference test window: scale 2.0, default rem 16, drawn at the
	// origin (zero element offset).
	ctx := gpui.NewTestLayoutContext()
	runner := &layoutTreeRunner{engine: engine, ctx: ctx, rec: rec}

	var flat []flatStyleNode
	root, err := flattenStyleTree(&envelope.Inputs.StyleTree, &flat, false)
	if err != nil {
		return fmt.Errorf("portfixture: style tree: %w", err)
	}
	nodes, err := runner.requestTree(flat, root, nil)
	if err != nil {
		return fmt.Errorf("portfixture: layout tree: %w", err)
	}
	if len(nodes) == 0 {
		return fmt.Errorf("portfixture: style tree has no nodes")
	}
	rec.event("layout-requested", flat[root].label)

	available := gpui.AvailableSize{
		Width:  gpui.DefiniteAvailableSpace(float32(envelope.Inputs.AvailableSpace.Width)),
		Height: gpui.DefiniteAvailableSpace(float32(envelope.Inputs.AvailableSpace.Height)),
	}
	if err := engine.ComputeLayout(ctx, nodes[0].id, available); err != nil {
		return fmt.Errorf("portfixture: compute layout: %w", err)
	}
	if err := runner.emitLayoutBounds(nodes); err != nil {
		return err
	}
	return nil
}

// runOp executes one effect op, mirroring the reference fixture's
// EffectOp handling op for op.
func (f *effectFixture) runOp(ta *gpui.TestApp, op *conformance.EffectOp, rec *recorder) error {
	app := ta.App()

	switch op.Op {
	case conformance.OpCreateEntity:
		entity := gpui.NewEntity(app, f.root, func(counter *Counter, _ *gpui.Context[Counter]) {
			*counter = Counter{value: op.Value}
		})
		f.counters[op.Label] = entity
		rec.eventWith("entity-created", op.Label, field{"value", op.Value})

	case conformance.OpObserveRelease:
		entity, ok := f.counters[op.Label]
		if !ok {
			return fmt.Errorf("unknown entity %s", op.Label)
		}
		label := op.Label
		entity.Update(app, func(_ *Counter, cx *gpui.Context[Counter]) {
			cx.OnRelease(func(counter *Counter, _ *gpui.App) {
				rec.eventWith("entity-released", label, field{"value", counter.value})
			}).Detach()
		})
		rec.event("release-observed", op.Label)

	case conformance.OpDropEntity:
		entity, ok := f.counters[op.Label]
		if !ok {
			return fmt.Errorf("unknown entity %s to drop", op.Label)
		}
		delete(f.counters, op.Label)
		rec.event("entity-dropped", op.Label)
		entity.Release()

	case conformance.OpSubscribe:
		if op.EventType != "tick" {
			return fmt.Errorf("fixture supports event_type \"tick\", got %q", op.EventType)
		}
		counter, ok := f.counters[op.Source]
		if !ok {
			return fmt.Errorf("unknown source entity %s", op.Source)
		}
		observerLabel := op.Observer
		observer := gpui.NewEntity(app, f.root, func(_ *Observer, cx *gpui.Context[Observer]) {
			tickEvent.Subscribe(cx, counter, func(obs *Observer, _ gpui.Entity[Counter], event *TickEvent, _ *gpui.Context[Observer]) {
				obs.receivedEvents++
				rec.eventWith("event-delivered", observerLabel,
					field{"value", uint64(event.value)},
					field{"receivedEvents", uint64(obs.receivedEvents)},
				)
			}).Detach()
			cx.Observe(counter, func(obs *Observer, _ gpui.Entity[Counter], _ *gpui.Context[Observer]) {
				obs.notified++
				rec.eventWith("observer-notified", observerLabel,
					field{"notified", uint64(obs.notified)},
					field{"receivedEvents", uint64(obs.receivedEvents)},
				)
			}).Detach()
		})
		f.observers[op.Observer] = observer
		rec.eventWith("subscription-registered", op.Observer, field{"source", op.Source})

	case conformance.OpNotify:
		entity, ok := f.counters[op.Entity]
		if !ok {
			return fmt.Errorf("unknown entity %s", op.Entity)
		}
		count := op.Count
		entityLabel := op.Entity
		// All notify calls happen within one outermost update so
		// coalescing behavior is observable.
		app.Update(func(app *gpui.App) {
			for i := uint32(0); i < count; i++ {
				entity.Update(app, func(counter *Counter, cx *gpui.Context[Counter]) {
					counter.value++
					cx.Notify()
				})
			}
			rec.eventWith("update-returned", entityLabel,
				field{"op", "notify"},
				field{"count", uint64(count)},
			)
		})

	case conformance.OpEmit:
		entity, ok := f.counters[op.Entity]
		if !ok {
			return fmt.Errorf("unknown entity %s", op.Entity)
		}
		values := op.Values
		entityLabel := op.Entity
		// All emits happen within one outermost update, in the given
		// order, so FIFO delivery is observable.
		app.Update(func(app *gpui.App) {
			for _, value := range values {
				entity.Update(app, func(_ *Counter, cx *gpui.Context[Counter]) {
					tickEvent.Emit(cx, TickEvent{value: value})
				})
			}
			rec.eventWith("update-returned", entityLabel,
				field{"op", "emit"},
				field{"count", uint64(len(values))},
			)
		})

	case conformance.OpDefer:
		deferLabel := op.Label
		schedule := op.Schedule
		app.Update(func(app *gpui.App) {
			app.Defer(func(app *gpui.App) {
				rec.event("defer-ran", deferLabel)
				if schedule != nil {
					next := *schedule
					app.Defer(func(_ *gpui.App) {
						rec.event("defer-ran", next)
					})
				}
			})
		})

	case conformance.OpSpawnTask:
		label := op.Label
		result := op.Result
		delayMs := op.DelayMs
		executor := ta.Background()
		task := executor.Spawn(func(run *gpui.TaskRun) int64 {
			rec.eventWith("task-started", label,
				field{"delayMs", delayMs},
				field{"result", result},
			)
			run.Sleep(time.Duration(delayMs) * time.Millisecond)
			rec.eventWith("task-completed", label, field{"result", result})
			return result
		})
		app.Update(func(app *gpui.App) {
			app.Spawn(func(cx *gpui.AsyncApp) struct{} {
				result := cx.Await(task)
				cx.Update(func(_ *gpui.App) {
					rec.eventWith("task-result-observed", label, field{"result", result})
				})
				return struct{}{}
			}).Detach()
		})

	case conformance.OpAdvanceClock:
		ta.AdvanceClock(int64(op.Ms))
		rec.eventWith("clock-advanced", "", field{"ms", op.Ms})

	case conformance.OpRunTasks:
		ta.RunUntilParked()
		rec.event("tasks-run", "")

	case conformance.OpRunEffects:
		ta.Update(func(_ *gpui.App) {})
		rec.event("effects-run", "")

	default:
		return fmt.Errorf("unknown op %q", op.Op)
	}
	return nil
}
