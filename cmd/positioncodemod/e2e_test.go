//go:build windows

// The behaviour witness for ticket 76: the sample program under
// testdata/sample renders two frames in the hostless test window — a
// pinned variant (explicit .Relative(), the pre-76 semantics
// stand-in) and a reliant variant (the same tree with the pins
// removed) — and prints each variant's element bounds. The test copies
// the sample into a temporary package inside the module (dot-prefixed,
// so no other go command sees it), runs it, applies the codemod, runs
// it again, and requires:
//
//   - BEFORE the codemod the reliant variant differs from the pinned
//     one (the static default moves the absolute child and ignores the
//     insets — the flip has teeth on exactly the default-reliant code);
//   - AFTER the codemod the two variants match line for line (the
//     pins restore the pre-76 layout);
//   - a second codemod pass changes nothing (idempotence).

package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestSampleReliantVariantFlipsAndTheCodemodRestoresIt is the
// acceptance check: under the static default the default-reliant
// construction sites change layout, and the codemod's .Relative()
// insertions restore the pinned variant's bounds exactly.
func TestSampleReliantVariantFlipsAndTheCodemodRestoresIt(t *testing.T) {
	repoRoot := findRepoRoot(t)

	tmp, err := os.MkdirTemp(repoRoot, ".positioncodemod-e2e-")
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

	// BEFORE: the reliant variant differs from the pinned one — the
	// static default moved the absolute child (it resolves against the
	// window root instead of the parent) and ignored the inset user's
	// offsets. This is what makes the sample a real regression witness
	// for the flip; if it stops failing, the sample no longer relies
	// on the default.
	if before == after {
		t.Fatalf("the reliant variant already matched the pinned one before the codemod; the sample does not rely on the default:\n%s", before)
	}
	pinnedBefore := variantLines(before, "P")
	reliantBefore := variantLines(before, "U")
	if pinnedBefore == reliantBefore {
		t.Fatalf("before the codemod the variants already agreed:\n%s", before)
	}

	// AFTER: the pins restore the pre-76 layout — the reliant variant
	// matches the pinned one line for line.
	pinnedAfter := variantLines(after, "P")
	reliantAfter := variantLines(after, "U")
	if pinnedAfter != reliantAfter {
		t.Fatalf("the codemod did not restore the pinned layout.\npinned:\n%s\nreliant:\n%s", pinnedAfter, reliantAfter)
	}
	if pinnedBefore != pinnedAfter {
		t.Fatalf("the pinned variant changed across the codemod.\nbefore:\n%s\nafter:\n%s", pinnedBefore, pinnedAfter)
	}
	if !strings.Contains(after, "P:root:") {
		t.Fatalf("the sample printed no pinned root bounds; output:\n%s", after)
	}
	t.Logf("codemod pinned %d default-reliant element(s); bounds identical across %d selectors",
		insertions, strings.Count(pinnedAfter, ": x="))
}

// variantLines extracts one variant's tagged output lines with the
// tag stripped, so the two variants compare line for line.
func variantLines(out, tag string) string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if rest, ok := strings.CutPrefix(line, tag+":"); ok {
			lines = append(lines, rest)
		}
	}
	return strings.Join(lines, "\n")
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
