package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// listGoFiles resolves the package patterns the way the go tool does —
// through `go list` — so testdata, ignored directories (reference/) and
// the module layout are handled exactly like any go command. The
// package's build files, in-package tests, external tests and
// build-tag-ignored files are all returned: the codemod must pin every
// spelling of a default-reliant element, whatever platform the file is
// tagged for.
func listGoFiles(patterns []string) ([]string, error) {
	args := append([]string{"list", "-e", "-json"}, patterns...)
	cmd := exec.Command("go", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list %s: %w", strings.Join(patterns, " "), err)
	}

	seen := map[string]bool{}
	var files []string
	dec := json.NewDecoder(&out)
	for {
		var pkg struct {
			Dir            string
			GoFiles        []string
			CgoFiles       []string
			TestGoFiles    []string
			XTestGoFiles   []string
			IgnoredGoFiles []string
		}
		if err := dec.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			return nil, fmt.Errorf("go list %s: decode: %w", strings.Join(patterns, " "), err)
		}
		if pkg.Dir == "" {
			continue
		}
		for _, name := range pkg.GoFiles {
			add := filepath.Join(pkg.Dir, name)
			if !seen[add] {
				seen[add] = true
				files = append(files, add)
			}
		}
		for _, name := range append(append(append(pkg.CgoFiles, pkg.TestGoFiles...), pkg.XTestGoFiles...), pkg.IgnoredGoFiles...) {
			add := filepath.Join(pkg.Dir, name)
			if !seen[add] {
				seen[add] = true
				files = append(files, add)
			}
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("go list %s: no Go files", strings.Join(patterns, " "))
	}
	return files, nil
}

// dirStats aggregates one directory bucket.
type dirStats struct {
	files      int
	insertions int
	testFiles  int
}

// summarize renders the run's per-directory totals: a top-level rollup
// (the sizing numbers ticket 76 needs) and, below it, the two-level
// breakdown wherever one top-level directory spans several subpackages.
func summarize(results []*FileRewrite, applied bool) string {
	cwd, _ := os.Getwd()
	var (
		totalFiles     int
		changed        int
		insertions     int
		testInsertions int
		testChanged    int
		top            = map[string]*dirStats{}
		sub            = map[string]*dirStats{}
		topOrder       []string
		subOrder       []string
	)
	bucket := func(m map[string]*dirStats, order *[]string, key string) *dirStats {
		st, ok := m[key]
		if !ok {
			st = &dirStats{}
			m[key] = st
			*order = append(*order, key)
		}
		return st
	}
	for _, res := range results {
		totalFiles++
		if !res.Changed {
			continue
		}
		changed++
		insertions += len(res.Edits)
		isTest := strings.HasSuffix(res.Path, "_test.go")
		if isTest {
			testInsertions += len(res.Edits)
			testChanged++
		}
		rel := relPath(cwd, res.Path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		topKey := parts[0]
		if len(parts) == 1 {
			topKey = "."
		}
		st := bucket(top, &topOrder, topKey)
		st.files++
		st.insertions += len(res.Edits)
		if isTest {
			st.testFiles++
		}
		if len(parts) >= 3 {
			subKey := strings.Join(parts[:2], "/")
			sst := bucket(sub, &subOrder, subKey)
			sst.files++
			sst.insertions += len(res.Edits)
			if isTest {
				sst.testFiles++
			}
		}
	}
	sort.Strings(topOrder)
	sort.Strings(subOrder)

	var b strings.Builder
	verb := "would change"
	if applied {
		verb = "changed"
	}
	fmt.Fprintf(&b, "positioncodemod: %d files scanned; %d %s; %d .Relative() insertions (%d in %d _test.go files)\n",
		totalFiles, changed, verb, insertions, testInsertions, testChanged)
	if changed == 0 {
		b.WriteString("positioncodemod: nothing to do (already pinned)\n")
		return b.String()
	}
	b.WriteString("per top-level directory (changed files):\n")
	for _, key := range topOrder {
		st := top[key]
		fmt.Fprintf(&b, "  %-22s %4d files  %5d insertions", key, st.files, st.insertions)
		if st.testFiles > 0 {
			fmt.Fprintf(&b, "  (%d test files)", st.testFiles)
		}
		b.WriteString("\n")
	}
	if len(subOrder) > 0 {
		b.WriteString("two-level detail (changed files):\n")
		for _, key := range subOrder {
			st := sub[key]
			fmt.Fprintf(&b, "  %-22s %4d files  %5d insertions", key, st.files, st.insertions)
			if st.testFiles > 0 {
				fmt.Fprintf(&b, "  (%d test files)", st.testFiles)
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// relPath makes path relative to base when possible, else returns it
// unchanged.
func relPath(base, path string) string {
	if rel, err := filepath.Rel(base, path); err == nil {
		return rel
	}
	return path
}
