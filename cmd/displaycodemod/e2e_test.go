//go:build windows

// The behaviour witness for ticket 68: the sample program under
// testdata/sample renders one frame in the hostless test window and
// prints its element bounds. The test copies it into a temporary
// package inside the module (dot-prefixed, so no other go command sees
// it), runs it, applies the codemod, runs it again, and requires
// identical output — plus a second codemod pass that changes nothing.

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSamplePackageNoBehaviorChange is the acceptance check: running
// the codemod on a real package produces no behaviour change (the
// offscreen layout bounds are identical line for line).
func TestSamplePackageNoBehaviorChange(t *testing.T) {
	repoRoot := findRepoRoot(t)

	tmp, err := os.MkdirTemp(repoRoot, ".displaycodemod-e2e-")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })

	sampleDir := filepath.Join("testdata", "sample")
	entries, err := os.ReadDir(sampleDir)
	if err != nil {
		t.Fatalf("sample dir: %v", err)
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		src := filepath.Join(sampleDir, entry.Name())
		dst := filepath.Join(tmp, entry.Name())
		if err := copyFile(src, dst); err != nil {
			t.Fatalf("copy %s: %v", src, err)
		}
		files = append(files, dst)
	}
	if len(files) == 0 {
		t.Fatal("no sample sources found")
	}

	before := goRun(t, repoRoot, tmp)

	// Apply the codemod in place (the -w path, through the same
	// RewriteSource the CLI uses).
	insertions := 0
	for _, file := range files {
		res, err := RewriteSource(file, nil)
		if err != nil {
			t.Fatalf("rewrite %s: %v", file, err)
		}
		if !res.Changed {
			continue
		}
		insertions += len(res.Edits)
		if err := os.WriteFile(file, res.Final, 0o644); err != nil {
			t.Fatalf("write %s: %v", file, err)
		}
	}
	if insertions == 0 {
		t.Fatal("the codemod pinned nothing in the sample; the witness is vacuous")
	}

	// Idempotence on the rewritten files: a second pass must be a
	// no-op.
	for _, file := range files {
		res, err := RewriteSource(file, nil)
		if err != nil {
			t.Fatalf("second rewrite %s: %v", file, err)
		}
		if res.Changed {
			t.Fatalf("second pass would still change %s (%d insertions)", file, len(res.Edits))
		}
	}

	after := goRun(t, repoRoot, tmp)

	if before != after {
		t.Fatalf("behaviour changed across the codemod.\nbefore:\n%s\nafter:\n%s", before, after)
	}
	if !strings.Contains(after, "row-a:") {
		t.Fatalf("the sample printed no row-a bounds; output:\n%s", after)
	}
	t.Logf("codemod pinned %d default-reliant element(s); bounds identical across %d selectors",
		insertions, strings.Count(after, ": x="))
}

// goRun runs `go run` on the package directory from the module root
// and returns its stdout.
func goRun(t *testing.T, repoRoot, dir string) string {
	t.Helper()
	cmd := exec.Command("go", "run", dir)
	cmd.Dir = repoRoot
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("go run %s: %v\nstderr:\n%s", dir, err, errOut.String())
	}
	return out.String()
}

// copyFile copies one file.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// findRepoRoot walks up from the test's working directory to the
// module root (the directory holding go.mod).
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
