package main

import (
	"fmt"
	"os"
	"strings"
)

// diffContext is the number of unchanged lines shown around each hunk.
const diffContext = 3

// lcsGuard bounds the LCS dynamic program: beyond it the changed middle
// is emitted as one whole replacement hunk. The codemod's localized
// insertions on gofmt-clean files stay far below the guard.
const lcsGuard = 3000

// unifiedDiff renders the rewrite of one file as a unified diff: the
// original is read from path (in dry-run mode nothing was written), the
// new side is the rewritten source.
func unifiedDiff(path string, next []byte) string {
	orig, err := os.ReadFile(path)
	if err != nil {
		lines := splitLines(next)
		return fmt.Sprintf("--- %s\n+++ %s\n@@ -0,0 +1,%d @@\n%s", path, path, len(lines), markLines(lines, '+'))
	}
	a := splitLines(orig)
	b := splitLines(next)

	// Trim the identical prefix and suffix so the diff runs on the
	// changed middle only; the trimmed lines become context around it.
	prefix := commonPrefix(a, b)
	suffix := commonSuffix(a[prefix:], b[prefix:])
	ops := diffOps(a[prefix:len(a)-suffix], b[prefix:len(b)-suffix])
	hunks := hunksOf(ops, prefix)

	if len(hunks) == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n+++ %s\n", path, path)
	for _, hunk := range hunks {
		hunk.write(&sb)
	}
	return sb.String()
}

// opKind is one line-level edit operation.
type opKind uint8

const (
	opEqual opKind = iota
	opDelete
	opAdd
)

// op is one line of the edit script.
type op struct {
	kind opKind
	line string
}

// diffOps computes the line-level edit script turning a into b through
// a longest-common-subsequence table.
func diffOps(a, b []string) []op {
	if len(a) > lcsGuard || len(b) > lcsGuard {
		// Coarse fallback: replace everything.
		ops := make([]op, 0, len(a)+len(b))
		for _, line := range a {
			ops = append(ops, op{opDelete, line})
		}
		for _, line := range b {
			ops = append(ops, op{opAdd, line})
		}
		return ops
	}
	n, m := len(a), len(b)
	// table[i][j] is the LCS length of a[i:] and b[j:].
	table := make([][]int, n+1)
	for i := range table {
		table[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	ops := make([]op, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{opEqual, a[i]})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			ops = append(ops, op{opDelete, a[i]})
			i++
		default:
			ops = append(ops, op{opAdd, b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{opDelete, a[i]})
	}
	for ; j < m; j++ {
		ops = append(ops, op{opAdd, b[j]})
	}
	return ops
}

// hunk is one unified-diff hunk over a slice of ops.
type hunk struct {
	aStart, aCount int
	bStart, bCount int
	lines          []string
}

// write renders the hunk with its @@ header.
func (h *hunk) write(sb *strings.Builder) {
	fmt.Fprintf(sb, "@@ -%d,%d +%d,%d @@\n", h.aStart, h.aCount, h.bStart, h.bCount)
	for _, line := range h.lines {
		sb.WriteString(line)
		sb.WriteByte('\n')
	}
}

// hunksOf groups the edit script into hunks with diffContext lines of
// context, splitting where a run of unchanged lines is longer than
// twice the context. The ops carry the whole changed middle; before is
// the number of trimmed leading lines, used to number lines.
func hunksOf(ops []op, before int) []*hunk {
	// Mark which ops belong to a hunk: any change, plus context around
	// it. A long equal run splits two hunks.
	inHunk := make([]bool, len(ops))
	for i, o := range ops {
		if o.kind != opEqual {
			for j := i - diffContext; j <= i+diffContext; j++ {
				if j >= 0 && j < len(ops) {
					inHunk[j] = true
				}
			}
		}
	}
	// Split hunks where an equal run longer than 2*context separates
	// marked regions.
	split := make([]bool, len(ops)+1)
	run := 0
	for i, o := range ops {
		if o.kind == opEqual && !inHunk[i] {
			run++
			if run > 2*diffContext {
				// Split at the middle of this run.
				split[i-run/2] = true
				run = 0
			}
		} else {
			run = 0
		}
	}

	var hunks []*hunk
	var cur *hunk
	aLine, bLine := before, before
	flush := func() {
		if cur != nil {
			hunks = append(hunks, cur)
			cur = nil
		}
	}
	for i, o := range ops {
		if split[i] {
			flush()
		}
		if !inHunk[i] {
			switch o.kind {
			case opEqual:
				aLine++
				bLine++
			case opDelete:
				aLine++
			case opAdd:
				bLine++
			}
			continue
		}
		if cur == nil {
			cur = &hunk{aStart: aLine + 1, bStart: bLine + 1}
		}
		switch o.kind {
		case opEqual:
			cur.lines = append(cur.lines, " "+o.line)
			cur.aCount++
			cur.bCount++
			aLine++
			bLine++
		case opDelete:
			cur.lines = append(cur.lines, "-"+o.line)
			cur.aCount++
			aLine++
		case opAdd:
			cur.lines = append(cur.lines, "+"+o.line)
			cur.bCount++
			bLine++
		}
	}
	flush()
	return hunks
}

// splitLines splits s into lines without their terminators.
func splitLines(s []byte) []string {
	text := string(s)
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	// A trailing newline produces a final empty element that is not a
	// line.
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// commonPrefix returns the number of identical leading lines.
func commonPrefix(a, b []string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// commonSuffix returns the number of identical trailing lines.
func commonSuffix(a, b []string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[len(a)-1-i] != b[len(b)-1-i] {
			return i
		}
	}
	return n
}

// markLines renders every line with the given prefix.
func markLines(lines []string, prefix rune) string {
	var sb strings.Builder
	for _, line := range lines {
		fmt.Fprintf(&sb, "%c%s\n", prefix, line)
	}
	return sb.String()
}
