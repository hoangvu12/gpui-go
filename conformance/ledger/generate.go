package ledger

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

// Toolchain reports the Go toolchain identity of the generator: the GOVERSION
// environment variable (set by the go tool for test/run/build invocations)
// with a runtime.Version fallback for directly executed binaries.
func Toolchain() string {
	if v := os.Getenv("GOVERSION"); v != "" {
		return v
	}
	return runtime.Version()
}

// coreExportKinds are the item kinds that count as public exports of the
// gpui crate for the core-export note.
var coreExportKinds = map[string]bool{
	"struct":       true,
	"enum":         true,
	"trait":        true,
	"fn":           true,
	"method":       true,
	"impl-method":  true,
	"trait-method": true,
	"const":        true,
	"static":       true,
	"type":         true,
	"macro":        true,
	"use":          true,
	"field":        true,
	"variant":      true,
}

// Generate loads the api-scan artifact, assigns owners, resolves profile
// membership and returns the versioned capability ledger. Every row is in
// the "planned" state; state transitions belong to later slices. The output
// is deterministic for a fixed scan and profile set (rows keep scan order,
// profile ids keep canonical order, maps marshal with sorted keys) except
// GeneratedAt and Toolchain, which record the generation environment.
func Generate(apiScanPath string, profiles map[string]ProfileRecord, generatedAt time.Time) (*Ledger, error) {
	scan, err := LoadApiScan(apiScanPath)
	if err != nil {
		return nil, err
	}
	n := 0
	for i := range scan.Crates {
		n += len(scan.Crates[i].Items)
	}
	rows := make([]Row, 0, n)
	for i := range scan.Crates {
		crate := &scan.Crates[i]
		for j := range crate.Items {
			rows = append(rows, buildRow(crate, &crate.Items[j], profiles))
		}
	}
	src := SourceIdentity{
		ApiScanSchema: scan.Schema,
		ApiScanCommit: scan.SourceCommit,
		GeneratedAt:   generatedAt.UTC().Format(time.RFC3339),
		Toolchain:     Toolchain(),
		Profiles:      profiles,
	}
	led := FromRows(rows, src)
	if err := led.Validate(); err != nil {
		return nil, err
	}
	return led, nil
}

// FromRows assembles a ledger from already-built rows, recomputing the
// summary and the unassigned row list.
func FromRows(rows []Row, src SourceIdentity) *Ledger {
	if rows == nil {
		rows = []Row{}
	}
	unassigned := []Row{}
	for _, r := range rows {
		if r.Owner == "" {
			unassigned = append(unassigned, r)
		}
	}
	return &Ledger{
		Schema:        LedgerSchema,
		GeneratedFrom: src,
		Rows:          rows,
		Summary:       summarize(rows, src.Profiles),
		Unassigned:    unassigned,
	}
}

func summarize(rows []Row, profiles map[string]ProfileRecord) Summary {
	s := Summary{
		TotalRows:          len(rows),
		ByOwner:            map[string]int{},
		ByState:            map[string]int{},
		ByDisposition:      map[string]int{},
		ByProfile:          map[string]int{},
		UnassignedCount:    0,
		UnimplementedCount: 0,
		FeatureGatedCount:  0,
	}
	for _, id := range ProfileOrder(profiles) {
		s.ByProfile[id] = 0
	}
	for _, r := range rows {
		if r.Owner == "" {
			s.UnassignedCount++
		} else {
			s.ByOwner[r.Owner]++
		}
		s.ByState[r.State]++
		s.ByDisposition[r.Disposition]++
		for _, id := range r.Profile {
			s.ByProfile[id]++
		}
		if r.BodyMarker != "" {
			s.UnimplementedCount++
		}
		if rowFeatureGated(r) {
			s.FeatureGatedCount++
		}
	}
	return s
}

// rowFeatureGated reports whether any cfg of the row compares a feature.
func rowFeatureGated(r Row) bool {
	for _, expr := range r.Cfg {
		if len(CfgFeatureNames(expr)) > 0 {
			return true
		}
	}
	return false
}

// RowMentionsFeature reports whether one of the row's cfg expressions
// compares the given feature.
func RowMentionsFeature(r Row, feature string) bool {
	for _, expr := range r.Cfg {
		for _, name := range CfgFeatureNames(expr) {
			if name == feature {
				return true
			}
		}
	}
	return false
}

func buildRow(crate *ScanCrate, item *ScanItem, profiles map[string]ProfileRecord) Row {
	cfg := item.Cfg
	if cfg == nil {
		cfg = []string{}
	} else {
		cfg = append([]string{}, cfg...)
	}
	row := Row{
		Symbol: item.Path,
		Kind:   item.Kind,
		Crate:  crate.Package,
		Source: fmt.Sprintf("%s:%d", item.File, item.Line),
		Cfg:    cfg,
		State:  StatePlanned,
	}
	if item.Doc != nil {
		row.Doc = firstLine(*item.Doc)
	}
	if item.BodyMarker != nil {
		row.BodyMarker = *item.BodyMarker
	}
	res := ResolveProfiles(row.Cfg, profiles)
	row.Profile = res.Profiles
	row.WindowsApplicable = len(res.Profiles) > 0
	row.Owner, row.OwnerSource = AssignOwner(row)
	row.Disposition = RowDisposition(row, row.Owner, row.OwnerSource)

	var notes []string
	if crate.Package == CrateGPUI && coreExportKinds[item.Kind] {
		notes = append(notes, "core-export")
	}
	for _, failed := range res.FailedCfg {
		notes = append(notes, "cfg-eval-failed: "+failed)
	}
	if res.DefaultFeature {
		notes = append(notes, "cfg-feature-default-always-true")
	}
	// The scanner records each item's own cfg attributes, not the cfg of the
	// enclosing module declaration. The gpui::profiler module is gated by
	// feature = "profiler" at its declaration (see feature_trace), so rows
	// under it with empty item cfg are conservatively listed in every
	// profile; the note keeps the parent gate visible.
	if crate.Package == CrateGPUI && strings.HasPrefix(row.Symbol, "gpui::profiler") && len(item.Cfg) == 0 {
		notes = append(notes, `parent-module-gate: feature="profiler"`)
	}
	// Ticket-34 dispositions: platform crates whose crate-level cfg removes
	// them from the Windows target keep their family owners but carry the gate
	// citation (inapplicable-by-platform is an applicability fact, never an
	// exception), and every triaged rule records its disposition note with
	// the pinned source usage that justifies the placement.
	if gate := PlatformGateNote(crate.Package); gate != "" {
		notes = append(notes, "platform-gate: "+gate+"; inapplicable on Windows, not an exception")
	}
	if note := DispositionNote(row); note != "" {
		notes = append(notes, "disposition: "+note)
	}
	row.Notes = strings.Join(notes, "; ")
	return row
}

func firstLine(doc string) string {
	if i := strings.IndexByte(doc, '\n'); i >= 0 {
		return doc[:i]
	}
	return doc
}
