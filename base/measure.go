package base

// Elapsed-time measurement logging (U:measure.rs). The tracing::trace!
// event becomes a log/slog record at the trace level (below Debug).

import (
	"context"
	"log/slog"
	"os"
	"time"
)

// levelTrace is the slog level standing in for tracing's TRACE (one step
// below Debug).
const levelTrace = slog.LevelDebug - 4

// MeasurementEnabled reports whether measurement logging is enabled by
// the ZED_MEASUREMENTS or GPUI_MEASUREMENTS environment variable
// (U:measure.rs:3-6 measurement_enabled).
func MeasurementEnabled() bool {
	if _, ok := os.LookupEnv("ZED_MEASUREMENTS"); ok {
		return true
	}
	_, ok := os.LookupEnv("GPUI_MEASUREMENTS")
	return ok
}

// MeasureIf measures f when cond is true and measurement logging is
// enabled; otherwise it just runs f (U:measure.rs:8-19 measure_if).
func MeasureIf(name string, cond bool, f func()) {
	if cond && MeasurementEnabled() {
		m := NewMeasure(name)
		f()
		m.End()
		return
	}
	f()
}

// MeasureFunc measures f when measurement logging is enabled
// (U:measure.rs:21-26 measure). The Rust free function measure and the
// Measure struct share a name; in Go the struct keeps the name and the
// function is MeasureFunc.
func MeasureFunc(name string, f func()) { MeasureIf(name, true, f) }

// Measure is an elapsed-time measurement emitted when ended
// (U:measure.rs:28-32).
type Measure struct {
	name  string
	start time.Time
}

// NewMeasure starts a measurement (U:measure.rs:35-41 Measure::new).
func NewMeasure(name string) Measure {
	return Measure{name: name, start: time.Now()}
}

// End logs the elapsed time (U:measure.rs:43-47 Measure::end:
// tracing::trace!("{} in {:?}")).
func (m Measure) End() {
	duration := time.Since(m.start)
	slog.Log(context.Background(), levelTrace, m.name+" in "+duration.String())
}
