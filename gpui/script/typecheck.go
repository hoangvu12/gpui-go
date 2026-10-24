package script

import (
	"context"
	"fmt"
	"os"
)

// TypeDiagnostic is one type error a TypeChecker found in the script's
// sources: tsc's report, with the .tsx file, line and column (the same
// shape every other script error carries).
type TypeDiagnostic struct {
	// Kind is "error" or "warning".
	Kind string
	// Code is the checker's code (tsc's TS2322).
	Code string
	// File, Line and Column locate the problem in the sources.
	File   string
	Line   int
	Column int
	// Message is the diagnostic's text.
	Message string
}

// String renders the diagnostic like the host's other errors:
// file:line:col: message.
func (d TypeDiagnostic) String() string {
	return fmt.Sprintf("%s:%d:%d: %s %s: %s", d.File, d.Line, d.Column, d.Kind, d.Code, d.Message)
}

// TypeChecker type-checks the script's sources (ticket 63, ADR 0003:
// debug builds check after every reload when tsc is present). It runs
// off the host thread (a type check takes as long as a tsc run); the
// host delivers the result through the scheduler onto the thread that
// renders, or synchronously when there is no scheduler.
//
// types.NewChecker (gpui/script/types) is the implementation: it finds
// tsc — a local typescript package, the npm global one, or a pinned
// path — and runs it against the sources with the generated .d.ts. A
// checker that finds no tsc reports it from Check; a release build
// simply installs no checker.
type TypeChecker interface {
	// Check type-checks the current sources, returning the diagnostics
	// (nil when the sources are clean). It must be safe to call from
	// another goroutine.
	Check() ([]TypeDiagnostic, error)
}

// WithTypeCheck installs the checker a dev host runs after every
// successful reload (the "debug builds also check" half of ADR 0003):
// each reload that compiles and loads cleanly is followed by a type
// check of its sources; the diagnostics go to the WithTypeDiagnostics
// hook (or the host's stderr when there is none). A failed reload
// skips the check — its load error already says what broke.
//
// The check never gates the reload: esbuild's compiled bundle already
// runs, so a type error keeps the last good UI up while the check
// reports (tsc's stance: types are advisory). A reload that lands
// while a check is still running drops that check's result and starts
// a fresh one, so diagnostics always describe the current sources.
func WithTypeCheck(c TypeChecker) Option {
	return func(h *Host) { h.typeCheck = c }
}

// WithTypeDiagnostics receives a check's diagnostics (on the host
// thread, once the check returns); without it a dev host writes them
// to the app's stderr, one line per diagnostic. A clean check reports
// an empty slice.
func WithTypeDiagnostics(f func([]TypeDiagnostic)) Option {
	return func(h *Host) { h.typeDiags = f }
}

// checkTypes runs one type check of the current sources after a
// successful reload. With a scheduler the check runs on a goroutine
// and its result marshals back onto the host thread through Post (the
// same threading model as fetch and async actions: a goroutine never
// touches the host); without one it runs synchronously, before Reload
// returns. A result from a generation a newer reload replaced is
// dropped, so the diagnostics always match the sources the window now
// shows.
func (h *Host) checkTypes() {
	c := h.typeCheck
	if c == nil {
		return
	}
	gen := h.vmGen
	if h.scheduler != nil {
		var (
			diags []TypeDiagnostic
			err   error
		)
		h.scheduler.Run(context.Background(), func(context.Context) {
			diags, err = c.Check()
		}, func() {
			if gen != h.vmGen {
				return // a newer reload replaced these sources
			}
			h.reportTypes(diags, err)
		})
		return
	}
	diags, err := c.Check()
	if gen == h.vmGen {
		h.reportTypes(diags, err)
	}
}

// reportTypes delivers one check's result to the hook (or stderr).
func (h *Host) reportTypes(diags []TypeDiagnostic, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "[script] type check:", err)
		return
	}
	if h.typeDiags != nil {
		h.typeDiags(diags)
		return
	}
	for _, d := range diags {
		fmt.Fprintln(os.Stderr, "[script]", d.String())
	}
}
