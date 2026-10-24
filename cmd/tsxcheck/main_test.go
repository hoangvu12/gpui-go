package main

// The check command's plumbing: usage errors, -init writing the
// declarations beside the sources, and a missing path. The type-check
// itself (tsc discovery, the clean fixture, the caught errors) is
// tested in gpui/script/types, which is the engine the command runs.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui/script/types"
)

func TestUsageErrorExitsTwo(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != 2 {
		t.Fatalf("no arguments: exit %d, want 2", code)
	}
	if code := run([]string{filepath.Join(t.TempDir(), "missing")}, &out, &errOut); code != 2 {
		t.Fatalf("missing path: exit %d, want 2", code)
	}
}

func TestInitWritesTheDeclarations(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	if code := run([]string{"-init", dir}, &out, &errOut); code != 0 {
		t.Fatalf("-init: exit %d, want 0: %s", code, errOut.String())
	}
	b, err := os.ReadFile(filepath.Join(dir, types.DTSFilename))
	if err != nil {
		t.Fatalf("the declarations were not written: %v", err)
	}
	if string(b) != types.DTS() {
		t.Fatalf("-init wrote a different .d.ts than the module carries")
	}
}

func TestMissingTypeScriptExitsThree(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "view.tsx"), []byte("export function App() { return <div>hi</div>; }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	code := run([]string{"-typescript", filepath.Join(dir, "no-tsc.js"), dir}, &out, &errOut)
	if code != 3 {
		t.Fatalf("missing tsc: exit %d, want 3: %s", code, errOut.String())
	}
	if errOut.String() == "" {
		t.Fatalf("the missing-tsc error printed nothing")
	}
}
