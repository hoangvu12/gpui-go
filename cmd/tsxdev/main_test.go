//go:build windows

package main

// The command's flag handling: the usage errors a bad invocation
// produces, without starting a session.

import (
	"errors"
	"testing"
)

func TestRunUsageErrors(t *testing.T) {
	dir := t.TempDir()
	for _, test := range []struct {
		name string
		args []string
	}{
		{"no arguments", nil},
		{"two directories", []string{dir, dir}},
		{"a missing directory", []string{`C:\tsxdev-does-not-exist`}},
		{"a bad flag", []string{"-poll=notaduration", dir}},
	} {
		test := test
		t.Run(test.name, func(t *testing.T) {
			code, err := run(test.args)
			if code != 2 || err == nil {
				t.Fatalf("run(%v) = (%d, %v), want (2, an error)", test.args, code, err)
			}
		})
	}
}

func TestRunRejectsAFileAsRoot(t *testing.T) {
	// A real file (the package's own source) as the app root.
	code, err := run([]string{"watcher.go"})
	if code != 2 || err == nil {
		t.Fatalf("run on a file = (%d, %v), want (2, an error)", code, err)
	}
}

func TestExitCodeOf(t *testing.T) {
	if code := exitCodeOf(nil); code != 0 {
		t.Fatalf("exitCodeOf(nil) = %d", code)
	}
	if code := exitCodeOf(errors.New("not an exit")); code != 1 {
		t.Fatalf("exitCodeOf(a plain error) = %d, want 1", code)
	}
	var exit *exitError
	if errors.As(error(&exitError{code: 7}), &exit) && exit.code != 7 {
		t.Fatalf("exitError round trip: %v", exit)
	}
}
