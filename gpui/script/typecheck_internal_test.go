//go:build windows

package script

// Internal (package script) tests for the type check's error
// reporting: a checker error is a session-level fact, reported once
// per host — a dev session without tsc reloads every save, and the
// no-tsc error must not repeat on every reload. The reporting path
// (reportTypes) writes to the app's stderr, so the test swaps
// os.Stderr for a pipe and reads it back.

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

// staticErrChecker is a TypeChecker whose checks fail with its
// current error (the shape of a missing-tsc discovery; the error can
// change, like a checker whose situation changes mid-session).
type staticErrChecker struct{ err error }

func (c *staticErrChecker) Check() ([]TypeDiagnostic, error) { return nil, c.err }

// captureStderr runs f with os.Stderr replaced by a pipe, returning
// everything written.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = writer
	defer func() {
		os.Stderr = old
		writer.Close()
	}()
	f()
	writer.Close()
	out, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestTheSameCheckerErrorIsReportedOncePerSession(t *testing.T) {
	noTsc := errors.New("script/types: tsc (TypeScript) was not found")
	host := New(WithTypeCheck(&staticErrChecker{err: noTsc}))

	// checkTypes runs the checker synchronously (no scheduler) and
	// reports — the path every reload takes.
	first := captureStderr(t, func() { host.checkTypes() })
	second := captureStderr(t, func() { host.checkTypes() })
	third := captureStderr(t, func() { host.checkTypes() })

	for i, out := range []string{first, second, third} {
		lines := strings.Count(out, "[script] type check:")
		if i == 0 && lines != 1 {
			t.Fatalf("the first check reported %d lines, want 1: %q", lines, out)
		}
		if i > 0 && lines != 0 {
			t.Fatalf("check %d repeated the same error: %q", i+1, out)
		}
	}
	if !strings.Contains(first, "tsc (TypeScript) was not found") {
		t.Fatalf("the first report does not carry the error: %q", first)
	}
}

func TestADifferentCheckerErrorIsReportedAgain(t *testing.T) {
	checker := &staticErrChecker{err: errors.New("no tsc")}
	host := New(WithTypeCheck(checker))

	first := captureStderr(t, func() { host.checkTypes() })
	// The checker finds a genuinely different problem later in the
	// session: that is news, not noise.
	quiet := captureStderr(t, func() { host.checkTypes() })
	checker.err = errors.New("no sources under ui")
	news := captureStderr(t, func() { host.checkTypes() })

	if !strings.Contains(first, "no tsc") || strings.Contains(quiet, "type check") {
		t.Fatalf("reporting = %q then %q, want the error once", first, quiet)
	}
	if !strings.Contains(news, "no sources under ui") {
		t.Fatalf("a different error was not reported: %q", news)
	}
}
