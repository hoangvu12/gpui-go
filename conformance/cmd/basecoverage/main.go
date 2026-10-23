// Command basecoverage builds the GPUI Base coverage ledger (ticket 32):
// every file of the hashed upstream source manifest
// (evidence/base/source-manifest.json) is accounted for, and every
// upstream `#[test]` / `#[gpui::test]` function is resolved to ported,
// deviated, not applicable, out of scope or deferred (owned by an open
// ticket) — or reported as a GAP.
//
// Test statuses are computed, not hand-typed: the Go tests cite the
// upstream test they port as `U:path:line[-line]` (path relative to
// crates/base/src), and a citation whose span overlaps an upstream test
// function credits it. A citation inside a Go func or declaration
// credits the test as ported, or as deviated when that func's comments
// record a deviation; a citation in a free comment is classified by its
// sentence (ported / deviated / not ported / deferred). Only tests no
// Go file cites fall back to the evidence table in overrides.go, each
// entry quoting its ticket's evidence.
//
//	go run ./conformance/cmd/basecoverage            # write the ledger + COVERAGE.md
//	go run ./conformance/cmd/basecoverage -check     # verify they are current; exit 1 on drift or GAPS
//
// The upstream checkout defaults to ../upstream-base/ce (override with
// -upstream or GPUI_BASE_UPSTREAM).
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	statusPorted     = "ported"
	statusDeviated   = "deviated"
	statusNA         = "not_applicable"
	statusOutOfScope = "out_of_scope"
	statusDeferred   = "deferred"
	statusGap        = "GAP"
)

type manifest struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Files      []struct {
		Path   string `json:"path"`
		Bytes  int    `json:"bytes"`
		SHA256 string `json:"sha256"`
	} `json:"files"`
}

type testEntry struct {
	Module   string   `json:"module"`
	Name     string   `json:"name"`
	Line     int      `json:"line"`
	Status   string   `json:"status"`
	Source   string   `json:"source"` // citation | evidence | module
	GoTests  []string `json:"go_tests,omitempty"`
	Tickets  []string `json:"tickets,omitempty"`
	Reason   string   `json:"reason,omitempty"`
	Evidence string   `json:"evidence,omitempty"`
}

type counts struct {
	Total         int `json:"total"`
	Ported        int `json:"ported"`
	Deviated      int `json:"deviated"`
	NotApplicable int `json:"not_applicable"`
	OutOfScope    int `json:"out_of_scope"`
	Deferred      int `json:"deferred"`
	Gap           int `json:"gap"`
}

func (c *counts) add(status string) {
	c.Total++
	switch status {
	case statusPorted:
		c.Ported++
	case statusDeviated:
		c.Deviated++
	case statusNA:
		c.NotApplicable++
	case statusOutOfScope:
		c.OutOfScope++
	case statusDeferred:
		c.Deferred++
	default:
		c.Gap++
	}
}

type moduleEntry struct {
	Path     string   `json:"path"`
	Status   string   `json:"status"`
	Tickets  []string `json:"tickets,omitempty"`
	Evidence []string `json:"evidence,omitempty"`
	GoFiles  []string `json:"go_files,omitempty"`
	Tests    counts   `json:"tests"`
	Reason   string   `json:"reason,omitempty"`
}

type otherFile struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Reason string `json:"reason"`
}

type ledger struct {
	Schema        string `json:"schema"`
	Generator     string `json:"generator"`
	Repository    string `json:"repository"`
	Commit        string `json:"commit"`
	ManifestFiles int    `json:"manifest_files"`
	ManifestCheck struct {
		Verified   int      `json:"sha256_verified"`
		Mismatches []string `json:"mismatches"`
	} `json:"manifest_check"`
	Totals struct {
		Modules         int            `json:"modules"`
		ModulesByStatus map[string]int `json:"modules_by_status"`
		Tests           counts         `json:"tests"`
		TestsInCrate    counts         `json:"tests_crate_only"`
		CitedByGo       int            `json:"tests_resolved_by_citation"`
		ByName          int            `json:"tests_resolved_by_go_test_name"`
		FromEvidence    int            `json:"tests_resolved_by_evidence_table"`
		Gaps            int            `json:"gaps"`
	} `json:"totals"`
	Gaps             []string      `json:"gaps"`
	UnknownCitations []string      `json:"unknown_citations"`
	BeyondEOF        []string      `json:"citations_beyond_upstream_eof"`
	UnusedOverrides  []string      `json:"unused_overrides"`
	Modules          []moduleEntry `json:"modules"`
	OtherFiles       []otherFile   `json:"other_files"`
	Tests            []testEntry   `json:"tests"`
}

func main() {
	root := flag.String("root", ".", "gpui-go repository root")
	upstream := flag.String("upstream", "", "upstream gpui-component checkout (default $GPUI_BASE_UPSTREAM or ../upstream-base/ce)")
	check := flag.Bool("check", false, "verify the committed ledger is current; exit 1 on drift or GAPS")
	flag.Parse()
	if *upstream == "" {
		*upstream = os.Getenv("GPUI_BASE_UPSTREAM")
	}
	if *upstream == "" {
		*upstream = filepath.Join(*root, "..", "upstream-base", "ce")
	}
	l, err := build(*root, *upstream)
	if err != nil {
		fmt.Fprintln(os.Stderr, "basecoverage:", err)
		os.Exit(2)
	}
	js, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "basecoverage:", err)
		os.Exit(2)
	}
	js = append(js, '\n')
	md := []byte(renderMarkdown(l))
	jsonPath := filepath.Join(*root, "evidence", "base", "coverage-ledger.json")
	mdPath := filepath.Join(*root, "evidence", "base", "COVERAGE.md")
	t := l.Totals
	fmt.Printf("modules %d %v\n", t.Modules, t.ModulesByStatus)
	fmt.Printf("tests %d: ported %d, deviated %d, not applicable %d, out of scope %d, deferred %d, GAPS %d (by citation %d, by evidence table %d)\n",
		t.Tests.Total, t.Tests.Ported, t.Tests.Deviated, t.Tests.NotApplicable, t.Tests.OutOfScope, t.Tests.Deferred, t.Gaps, t.CitedByGo, t.FromEvidence)
	for _, g := range l.Gaps {
		fmt.Println("GAP:", g)
	}
	if *check {
		ok := true
		for _, f := range []struct {
			path string
			want []byte
		}{{jsonPath, js}, {mdPath, md}} {
			got, err := os.ReadFile(f.path)
			if err != nil || !bytes.Equal(normalizeEOL(got), f.want) {
				fmt.Println("stale:", f.path)
				ok = false
			}
		}
		if !ok || len(l.Gaps) > 0 || len(l.ManifestCheck.Mismatches) > 0 {
			os.Exit(1)
		}
		fmt.Println("ledger current")
		return
	}
	if err := os.WriteFile(jsonPath, js, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "basecoverage:", err)
		os.Exit(2)
	}
	if err := os.WriteFile(mdPath, md, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "basecoverage:", err)
		os.Exit(2)
	}
	fmt.Println("wrote", jsonPath, "and", mdPath)
	if len(l.Gaps) > 0 {
		os.Exit(1)
	}
}

func normalizeEOL(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }

// moduleKey maps a manifest path to the key Go citations use.
func moduleKey(path string) string {
	if rel, ok := strings.CutPrefix(path, "crates/base/src/"); ok {
		return rel
	}
	return "../" + strings.TrimPrefix(path, "crates/base/")
}

func build(root, upstream string) (*ledger, error) {
	raw, err := os.ReadFile(filepath.Join(root, "evidence", "base", "source-manifest.json"))
	if err != nil {
		return nil, err
	}
	var man manifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return nil, err
	}
	l := &ledger{
		Schema:     "gpui-go/base-coverage-ledger@1",
		Generator:  "go run ./conformance/cmd/basecoverage",
		Repository: man.Repository,
		Commit:     man.Commit,
	}
	l.ManifestFiles = len(man.Files)
	l.ManifestCheck.Mismatches = []string{}
	l.Gaps = []string{}

	// Upstream sources: verify hashes and scan tests.
	type upMod struct {
		key    string
		path   string
		tests  []rustTest
		lines  int
		wiring bool
	}
	var mods []upMod
	for _, f := range man.Files {
		data, err := os.ReadFile(filepath.Join(upstream, filepath.FromSlash(f.Path)))
		if err != nil {
			return nil, fmt.Errorf("upstream source %s: %w (set -upstream)", f.Path, err)
		}
		// The manifest records the git blob size (LF) and the sha256 of the
		// Windows checkout (core.autocrlf); accept either line-ending form.
		lf := normalizeEOL(data)
		sum, sumLF := sha256.Sum256(data), sha256.Sum256(lf)
		shaOK := hex.EncodeToString(sum[:]) == f.SHA256 || hex.EncodeToString(sumLF[:]) == f.SHA256
		if !shaOK || (len(data) != f.Bytes && len(lf) != f.Bytes) {
			l.ManifestCheck.Mismatches = append(l.ManifestCheck.Mismatches, f.Path)
		} else {
			l.ManifestCheck.Verified++
		}
		if !strings.HasSuffix(f.Path, ".rs") {
			st, why := otherFileDisposition(f.Path)
			if strings.HasPrefix(f.Path, "crates/base/src/dock/fixtures/") {
				if _, err := os.Stat(filepath.Join(root, "base", "testdata", "dock", filepath.Base(f.Path))); err != nil {
					st, why = statusGap, "fixture missing from base/testdata/dock"
				}
			}
			if st == "" {
				st, why = statusGap, "no disposition"
			}
			if st == statusGap {
				l.Gaps = append(l.Gaps, "file "+f.Path+": "+why)
			}
			l.OtherFiles = append(l.OtherFiles, otherFile{Path: f.Path, Status: st, Reason: why})
			continue
		}
		mods = append(mods, upMod{key: moduleKey(f.Path), path: f.Path, tests: scanRustTests(data), lines: bytes.Count(data, []byte("\n")) + 1, wiring: wiringOnly(data)})
	}
	known := map[string]bool{}
	lineCount := map[string]int{}
	for _, m := range mods {
		known[m.key] = true
		lineCount[m.key] = m.lines
	}

	// Go side: implementation files citing each module; test citations.
	goFiles := map[string][]string{}
	var cites []citation
	funcsByName := map[string][]goTestFunc{}
	for _, dir := range []string{"base", "gpui", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".go") {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			rel = filepath.ToSlash(rel)
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if !bytes.Contains(src, []byte("U:")) {
				return nil
			}
			if !strings.HasSuffix(p, "_test.go") {
				if dir == "base" {
					for _, cp := range citedPaths(string(src)) {
						goFiles[cp] = append(goFiles[cp], rel)
					}
				}
				return nil
			}
			g, err := parseGo(rel, src)
			if err != nil {
				return err
			}
			cs, fs := scanCitations(g)
			cites = append(cites, cs...)
			for _, f := range fs {
				k := normName(f.Name)
				funcsByName[k] = append(funcsByName[k], f)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	unknown := map[string]bool{}
	byPath := map[string][]citation{}
	for _, c := range cites {
		key := c.Path
		if !known[key] && known["../"+key] {
			key = "../" + key
		}
		if !known[key] {
			unknown[c.File+": U:"+c.Path] = true
			continue
		}
		byPath[key] = append(byPath[key], c)
		for _, r := range c.Ranges {
			if r[1] > lineCount[key] || r[0] > r[1] {
				l.BeyondEOF = append(l.BeyondEOF, fmt.Sprintf("%s:%d U:%s:%d-%d (the upstream file has %d lines)", c.File, c.Line, c.Path, r[0], r[1], lineCount[key]))
			}
		}
	}
	for u := range unknown {
		l.UnknownCitations = append(l.UnknownCitations, u)
	}
	sort.Strings(l.UnknownCitations)

	usedOverride := map[string]bool{}
	l.Totals.ModulesByStatus = map[string]int{}
	for _, m := range mods {
		me := moduleEntry{Path: m.path}
		isExample := strings.HasPrefix(m.path, "crates/base/examples/")
		if !isExample {
			me.Tickets = ownersOf(m.key)
			for _, t := range me.Tickets {
				if ev := ticketEvidence[t]; ev != "" {
					me.Evidence = append(me.Evidence, "evidence/base/"+ev)
				}
			}
			me.GoFiles = goFiles[m.key]
			sort.Strings(me.GoFiles)
		}
		for _, rt := range m.tests {
			te := resolveTest(m.key, rt, byPath[m.key])
			if te.Status == "" {
				te = resolveByName(m.key, rt, funcsByName)
			}
			te.Module = m.path
			if isExample {
				te = testEntry{Module: m.path, Name: rt.Name, Line: rt.Attr, Status: statusOutOfScope, Source: "module", Reason: examplesReason}
			} else if ov, ok := overrides[m.key+"::"+rt.Name]; ok && (te.Status == "" || ov.Force) {
				usedOverride[m.key+"::"+rt.Name] = true
				te.Status, te.Source, te.Reason = ov.Status, "evidence", ov.Reason
				te.Tickets = []string{ov.Ticket}
				if ev := ticketEvidence[ov.Ticket]; ev != "" {
					te.Evidence = "evidence/base/" + ev
				}
			}
			if te.Status == "" {
				te.Status, te.Source = statusGap, "none"
				te.Reason = "no Go test cites it and no ticket evidence accounts for it"
				l.Gaps = append(l.Gaps, fmt.Sprintf("test %s:%d %s", m.key, rt.Attr, rt.Name))
			}
			if len(te.Tickets) == 0 && !isExample {
				te.Tickets = me.Tickets
			}
			switch te.Source {
			case "citation":
				l.Totals.CitedByGo++
			case "name":
				l.Totals.ByName++
			case "evidence":
				l.Totals.FromEvidence++
			}
			me.Tests.add(te.Status)
			l.Totals.Tests.add(te.Status)
			if !isExample {
				l.Totals.TestsInCrate.add(te.Status)
			}
			l.Tests = append(l.Tests, te)
		}
		switch {
		case isExample:
			me.Status, me.Reason = statusOutOfScope, examplesReason
		case moduleDisposition[m.key].Status != "":
			me.Status, me.Reason = moduleDisposition[m.key].Status, moduleDisposition[m.key].Reason
		case len(me.GoFiles) == 0 && m.wiring:
			me.Status, me.Reason = statusNA, "module wiring only (mod/use declarations, no code); the Go port is one flat package exporting the same names (map.md package layout)"
		case len(me.GoFiles) == 0 && len(m.tests) > 0 && me.Tests.Gap == 0:
			me.Status, me.Reason = statusPorted, "test-only module: no runtime code; its tests are resolved below"
			if me.Tests.Ported != me.Tests.Total {
				me.Status = statusDeviated
			}
		case len(me.GoFiles) == 0:
			me.Status, me.Reason = statusGap, "no Go file cites this module"
			l.Gaps = append(l.Gaps, "module "+m.path+": no Go file cites it")
		case me.Tests.Deviated+me.Tests.NotApplicable+me.Tests.Deferred+me.Tests.Gap > 0:
			me.Status = statusDeviated
			me.Reason = "some upstream tests deviate, are not applicable or are deferred (see tests)"
		default:
			me.Status = statusPorted
		}
		l.Totals.ModulesByStatus[me.Status]++
		l.Modules = append(l.Modules, me)
	}
	for k := range overrides {
		if !usedOverride[k] {
			l.UnusedOverrides = append(l.UnusedOverrides, k)
		}
	}
	sort.Strings(l.UnusedOverrides)
	l.Totals.Modules = len(l.Modules)
	l.Totals.Gaps = len(l.Gaps)
	if l.UnknownCitations == nil {
		l.UnknownCitations = []string{}
	}
	if l.BeyondEOF == nil {
		l.BeyondEOF = []string{}
	}
	if l.UnusedOverrides == nil {
		l.UnusedOverrides = []string{}
	}
	return l, nil
}

// resolveTest classifies one upstream test from the Go citations of
// its module. Citations inside a Go Test func beat those in other
// funcs/declarations (helpers), which beat free-comment citations;
// within a class the narrowest citing span wins (a precise
// `U:x.rs:120-140` beats a file-wide `U:x.rs:100-900`); at equal
// precision a deviation wins over a plain port.
func resolveTest(key string, rt rustTest, cs []citation) testEntry {
	te := testEntry{Name: rt.Name, Line: rt.Attr}
	type hit struct {
		c     citation
		width int
		decl  bool
		rank  int // 2 Test func, 1 other decl, 0 free comment
	}
	var best []hit
	better := func(a, b hit) int { // <0: a better
		if a.rank != b.rank {
			return b.rank - a.rank
		}
		return a.width - b.width
	}
	for _, c := range cs {
		for _, r := range c.Ranges {
			if r[0] > rt.EndLine || r[1] < rt.Attr {
				continue
			}
			h := hit{c: c, width: r[1] - r[0], decl: c.Context != ""}
			switch {
			case strings.HasPrefix(c.Context, "Test"):
				h.rank = 2
			case h.decl:
				h.rank = 1
			}
			switch {
			case len(best) == 0 || better(h, best[0]) < 0:
				best = []hit{h}
			case better(h, best[0]) == 0:
				best = append(best, h)
			}
		}
	}
	if len(best) == 0 {
		return te
	}
	te.Source = "citation"
	status := statusPorted
	var reasons []string
	seen := map[string]bool{}
	for _, h := range best {
		ref := h.c.File + ":" + fmt.Sprint(h.c.Line)
		if h.c.Context != "" {
			ref = h.c.File + ":" + h.c.Context
		}
		if !seen[ref] {
			seen[ref] = true
			te.GoTests = append(te.GoTests, ref)
		}
		s := statusPorted
		switch {
		case h.decl && h.c.Deviated:
			s = statusDeviated
		case h.decl:
		case h.c.Deferred && (h.c.NotApplicable || !h.c.Deviated):
			s = statusDeferred
		case h.c.NotApplicable:
			s = statusNA
		case h.c.Deviated:
			s = statusDeviated
		}
		if h.c.Sentence != "" && s != statusPorted {
			reasons = append(reasons, h.c.Sentence)
		}
		status = worse(status, s)
	}
	te.Status = status
	if status == statusDeviated && len(reasons) == 0 {
		te.Reason = "the citing Go test records a deviation (see its comment)"
	} else if len(reasons) > 0 {
		te.Reason = strings.Join(reasons, " | ")
	}
	return te
}

// normName folds a Go or Rust test name for comparison: lower case,
// no underscores, without the Go "Test" prefix or a Rust "test_" prefix.
func normName(n string) string {
	n = strings.ToLower(strings.ReplaceAll(n, "_", ""))
	return n
}

// resolveByName credits an upstream test that no line citation covers
// when a Go test in a file citing the module is named Test + the Rust
// name in CamelCase (the ticket-27 convention: "one Go test per Rust
// test, named Test + the Rust name in CamelCase").
func resolveByName(key string, rt rustTest, funcs map[string][]goTestFunc) testEntry {
	te := testEntry{Name: rt.Name, Line: rt.Attr}
	cands := []string{normName("Test" + rt.Name)}
	if s, ok := strings.CutPrefix(rt.Name, "test_"); ok {
		cands = append(cands, normName("Test"+s))
	}
	for _, c := range cands {
		for _, f := range funcs[c] {
			if !f.Cites[key] && !f.Cites[strings.TrimPrefix(key, "../")] {
				continue
			}
			te.Source = "name"
			te.GoTests = append(te.GoTests, f.File+":"+f.Name)
			if f.Deviated {
				te.Status = statusDeviated
				te.Reason = "the same-named Go test records a deviation (see its comment)"
			} else if te.Status == "" {
				te.Status = statusPorted
			}
		}
		if te.Source != "" {
			return te
		}
	}
	return te
}

func worse(a, b string) string {
	rank := map[string]int{statusPorted: 0, statusDeviated: 1, statusNA: 2, statusDeferred: 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}
