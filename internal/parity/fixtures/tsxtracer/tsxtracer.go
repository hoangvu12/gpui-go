//go:build windows

// Package tsxtracer is the TSX twin of the tracer fixture (ticket 62,
// spec seam 3): the same padded, bordered box with text and click
// handlers in the same 640x400 page, written as a .tsx view that runs
// through the script host (esbuild + goja, ADR 0003) instead of Go.
//
// It reuses the tracer's record (internal/parity/testdata/
// tracer-box-click): the .tsx view is compared against the same Chrome
// page as the Go twin, so it is not registered in the fixture registry
// (there is no record of its own to record); tsxtracer_test.go runs
// the three comparisons against tracer.Fixture's record.
package tsxtracer

import (
	_ "embed"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/gpui/script"
	"github.com/hoangvu12/gpui-go/gpui/script/compile"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/tracer"
)

// viewSrc is the .tsx twin (view.tsx).
//
//go:embed view.tsx
var viewSrc string

// Fixture is the tracer fixture with the .tsx twin as its view: the
// same page, window, steps, box ids, event kinds and pixel tolerance.
var Fixture = func() parity.Fixture {
	f := tracer.Fixture
	f.View = View()
	return f
}()

// View returns the fixture's view function: it creates the script host
// once per event log (the harness calls the view every frame; the
// host, its VM and its keyed state live in the closure, as the
// harness guide allows) and lowers the described tree per call.
func View() func(log *parity.EventLog) gpui.AnyElement {
	var host *script.Host
	var bound *parity.EventLog
	return func(log *parity.EventLog) gpui.AnyElement {
		if host == nil || bound != log {
			bound = log
			host = newHost(log)
		}
		return host.Element()
	}
}

// newHost loads the .tsx twin with the harness's event log installed
// as a script global (the Go twin logs from its Go handlers; the .tsx
// twin logs through the global its handlers call).
func newHost(log *parity.EventLog) *script.Host {
	host := script.New()
	host.SetGlobal("logEvent", func(kind, target string) { log.Append(kind, target) })
	bundle, err := compile.String("view.tsx", viewSrc)
	if err != nil {
		panic("tsxtracer: compiling view.tsx: " + err.Error())
	}
	if err := host.Load(bundle); err != nil {
		panic("tsxtracer: loading view.tsx: " + err.Error())
	}
	return host
}
