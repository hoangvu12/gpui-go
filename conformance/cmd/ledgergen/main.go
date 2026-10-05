// Command ledgergen generates the versioned gpui-go capability ledger from
// the pinned CE api-scan artifact and the recorded conformance profiles.
//
// It writes ledger.json (the machine-readable ledger) and ledger.md (the
// human review summary: summary counts, per-owner tables, the family→owner
// rules actually used, unassigned and unimplemented rows, feature-gated
// applicability with per-feature cfg site counts from the scan's
// feature_trace, and the five profile records) into the output directory.
// All rows are emitted in the "planned" state; later slices record
// reference results and move states.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gpui-go/conformance/ledger"
)

func main() {
	scanPath := flag.String("scan", "reference/out/api-scan.json", "path to the api-scan artifact")
	outDir := flag.String("out", "conformance/ledger/generated", "output directory for ledger.json and ledger.md")
	profilesDir := flag.String("profiles-dir", "reference/out/profiles", "directory holding the cargo tree profile graphs")
	flag.Parse()

	if err := run(*scanPath, *outDir, *profilesDir); err != nil {
		fmt.Fprintf(os.Stderr, "ledgergen: %v\n", err)
		os.Exit(1)
	}
}

func run(scanPath, outDir, profilesDir string) error {
	profiles := ledger.DefaultProfiles(profilesDir)
	for _, id := range ledger.DefaultProfileOrder {
		rec := profiles[id]
		if _, err := os.Stat(rec.GraphFile); err != nil {
			return fmt.Errorf("profile %s graph file %s: %w", id, rec.GraphFile, err)
		}
	}

	generatedAt := time.Now()
	led, err := ledger.Generate(scanPath, profiles, generatedAt)
	if err != nil {
		return err
	}

	// The feature-gated applicability section also reports the cfg sites the
	// scanner traced per feature, so load the scan once more for its trace.
	scan, err := ledger.LoadApiScan(scanPath)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	jsonPath := filepath.Join(outDir, "ledger.json")
	if err := writeJSON(jsonPath, led); err != nil {
		return err
	}
	mdPath := filepath.Join(outDir, "ledger.md")
	if err := os.WriteFile(mdPath, []byte(renderMarkdown(led, scan)), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", mdPath, err)
	}

	reportSummary(led)
	fmt.Printf("wrote %s and %s\n", jsonPath, mdPath)
	return nil
}

// applicabilityFeatures are the features whose applicability the report makes
// explicit, in report order (wgpu-surfaces is added because it is the actual
// gate of the custom-gpu-wgpu profile).
var applicabilityFeatures = []string{
	"wgpu", "wgpu-surfaces", "custom-gpu", "hot-patching", "profiler",
	"stacker", "leak-detection", "inspector", "test-support", "screen-capture",
}

func reportSummary(led *ledger.Ledger) {
	s := led.Summary
	fmt.Printf("schema            %s\n", led.Schema)
	fmt.Printf("source commit     %s\n", led.GeneratedFrom.ApiScanCommit)
	fmt.Printf("toolchain         %s\n", led.GeneratedFrom.Toolchain)
	fmt.Printf("total rows        %d\n", s.TotalRows)
	fmt.Printf("unassigned        %d\n", s.UnassignedCount)
	fmt.Printf("unimplemented     %d\n", s.UnimplementedCount)
	fmt.Printf("feature-gated     %d\n", s.FeatureGatedCount)
	fmt.Printf("by state          %s\n", formatCounts(s.ByState))
	fmt.Printf("by profile        %s\n", formatCounts(s.ByProfile))
	fmt.Println("top families:")
	usage := ledger.RuleUsageForRows(led.Rows)
	top := topRules(usage, 10)
	for _, u := range top {
		fmt.Printf("  %-32s owner %-2s %5d rows\n", u.Family, u.Owner, u.Matched)
	}
}

func topRules(usage []ledger.RuleUsage, n int) []ledger.RuleUsage {
	sorted := append([]ledger.RuleUsage{}, usage...)
	// Stable sort by matched count descending, then rule id.
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0; j-- {
			if sorted[j].Matched > sorted[j-1].Matched ||
				(sorted[j].Matched == sorted[j-1].Matched && sorted[j].RuleID < sorted[j-1].RuleID) {
				sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
			} else {
				break
			}
		}
	}
	if len(sorted) > n {
		sorted = sorted[:n]
	}
	return sorted
}

func formatCounts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if k == "" {
			k = "(unassigned)"
		}
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, " ")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func writeJSON(path string, led *ledger.Ledger) error {
	data, err := json.MarshalIndent(led, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ledger: %w", err)
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func renderMarkdown(led *ledger.Ledger, scan *ledger.ApiScan) string {
	var b strings.Builder
	s := led.Summary
	src := led.GeneratedFrom

	b.WriteString("# GPUI-Go capability ledger\n\n")
	b.WriteString("Generated by `conformance/cmd/ledgergen` from the pinned CE api-scan artifact.\n\n")
	fmt.Fprintf(&b, "- Schema: `%s`\n", led.Schema)
	fmt.Fprintf(&b, "- Generated from: `%s` at commit `%s`\n", src.ApiScanSchema, src.ApiScanCommit)
	fmt.Fprintf(&b, "- Generated at: `%s`\n", src.GeneratedAt)
	fmt.Fprintf(&b, "- Toolchain: `%s`\n", src.Toolchain)
	fmt.Fprintf(&b, "- Profiles: %d recorded\n\n", len(src.Profiles))

	b.WriteString("Every row is in the `planned` state; no reference result has been recorded\n")
	b.WriteString("against this ledger yet. Evidence states and pass rules are defined by\n")
	b.WriteString("docs/conformance-contract.md; a missing port implementation is a pending row,\n")
	b.WriteString("not an approved exception.\n\n")

	b.WriteString("## Summary\n\n")
	fmt.Fprintf(&b, "| Measure | Count |\n|---|---:|\n")
	fmt.Fprintf(&b, "| Total rows | %d |\n", s.TotalRows)
	fmt.Fprintf(&b, "| Unassigned rows | %d |\n", s.UnassignedCount)
	fmt.Fprintf(&b, "| Unimplemented / no-op rows | %d |\n", s.UnimplementedCount)
	fmt.Fprintf(&b, "| Feature-gated rows | %d |\n", s.FeatureGatedCount)
	b.WriteString("\nBy state:\n\n")
	fmt.Fprintf(&b, "| State | Rows |\n|---|---:|\n")
	for _, st := range ledger.ValidStates {
		fmt.Fprintf(&b, "| %s | %d |\n", st, s.ByState[st])
	}
	b.WriteString("\nBy profile (rows existing under each profile's feature set on a Windows target):\n\n")
	fmt.Fprintf(&b, "| Profile | Rows |\n|---|---:|\n")
	for _, id := range ledger.ProfileOrder(src.Profiles) {
		fmt.Fprintf(&b, "| %s | %d |\n", id, s.ByProfile[id])
	}

	b.WriteString("\n## Per-owner row counts\n\n")
	fmt.Fprintf(&b, "| Owner | Rows |\n|---|---:|\n")
	owners := sortedOwnerCounts(s.ByOwner)
	for _, oc := range owners {
		label := oc.owner
		if label == "" {
			label = "(unassigned)"
		}
		fmt.Fprintf(&b, "| %s | %d |\n", label, oc.count)
	}

	b.WriteString("\n## Family assignments actually used\n\n")
	b.WriteString("Ordered rules from docs/conformance-inventory.md; the first match wins.\n\n")
	fmt.Fprintf(&b, "| Rule | Family | Owner | Rows matched | Description |\n|---|---|---|---:|---|\n")
	for _, u := range ledger.RuleUsageForRows(led.Rows) {
		fmt.Fprintf(&b, "| %d | %s | %s | %d | %s |\n", u.RuleID, u.Family, u.Owner, u.Matched, u.Description)
	}

	b.WriteString("\n## Unassigned rows\n\n")
	b.WriteString("Rows no family rule matched; they need explicit assignment before the\n")
	b.WriteString("family they belong to can close.\n\n")
	fmt.Fprintf(&b, "Total: %d\n\n", len(led.Unassigned))
	fmt.Fprintf(&b, "| Symbol | Kind | Crate | Reason |\n|---|---|---|---|\n")
	for _, r := range led.Unassigned {
		fmt.Fprintf(&b, "| `%s` | %s | %s | no owner rule matched |\n", r.Symbol, r.Kind, r.Crate)
	}

	b.WriteString("\n## Unimplemented and no-op rows\n\n")
	b.WriteString("Rows whose reference body is marked unimplemented or a no-op; reproducing\n")
	b.WriteString("that outcome is a real result, not a blank pass.\n\n")
	var marked []ledger.Row
	for _, r := range led.Rows {
		if r.BodyMarker != "" {
			marked = append(marked, r)
		}
	}
	fmt.Fprintf(&b, "Total: %d\n\n", len(marked))
	fmt.Fprintf(&b, "| Symbol | Source | Marker |\n|---|---|---|\n")
	for _, r := range marked {
		fmt.Fprintf(&b, "| `%s` | %s | %s |\n", r.Symbol, r.Source, r.BodyMarker)
	}

	b.WriteString("\n## Feature-gated applicability\n\n")
	b.WriteString("Rows whose cfg compares the feature, plus the cfg sites the api-scan traced\n")
	b.WriteString("for the feature. Alternate GPU and dev-toolchain features are applicability\n")
	b.WriteString("rows owned by ticket 01; their absence from the selected native profile is\n")
	b.WriteString("not approval to drop an applicable core operation.\n\n")
	fmt.Fprintf(&b, "| Feature | Ledger rows gated | feature_trace cfg sites |\n|---|---:|---:|\n")
	for _, feat := range applicabilityFeatures {
		rows := 0
		for _, r := range led.Rows {
			if ledger.RowMentionsFeature(r, feat) {
				rows++
			}
		}
		sites := 0
		if scan != nil && scan.FeatureTrace != nil {
			sites = len(scan.FeatureTrace[feat])
		}
		fmt.Fprintf(&b, "| %s | %d | %d |\n", feat, rows, sites)
	}
	b.WriteString("\nPer-feature cfg forms traced by the scan:\n\n")
	fmt.Fprintf(&b, "| Feature | Cfg form | Sites |\n|---|---|---:|\n")
	for _, feat := range applicabilityFeatures {
		if scan == nil || scan.FeatureTrace == nil {
			continue
		}
		forms := map[string]int{}
		for _, site := range scan.FeatureTrace[feat] {
			forms[site.Cfg]++
		}
		keys := make([]string, 0, len(forms))
		for k := range forms {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "| %s | `%s` | %d |\n", feat, k, forms[k])
		}
	}

	b.WriteString("\n## Profiles\n\n")
	fmt.Fprintf(&b, "| Profile | Cargo features | Graph | Description |\n|---|---|---|---|\n")
	for _, id := range ledger.ProfileOrder(src.Profiles) {
		rec := src.Profiles[id]
		fmt.Fprintf(&b, "| %s | %s | %s | %s |\n",
			id, strings.Join(rec.CargoFeatures, ", "), rec.GraphFile, rec.Description)
	}
	b.WriteString("\nNote: the api-scan records each item's own cfg attributes, not the cfg of\n")
	b.WriteString("enclosing module declarations. Rows under `gpui::profiler` carry a\n")
	b.WriteString("`parent-module-gate` note for that reason: the module is gated by\n")
	b.WriteString("`feature = \"profiler\"` at its declaration, so their profile membership is\n")
	b.WriteString("conservative (every profile) until the ledger records explicit gates.\n")
	return b.String()
}

type ownerCount struct {
	owner string
	count int
}

func sortedOwnerCounts(byOwner map[string]int) []ownerCount {
	out := make([]ownerCount, 0, len(byOwner))
	for owner, count := range byOwner {
		out = append(out, ownerCount{owner: owner, count: count})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			if out[j].count > out[j-1].count ||
				(out[j].count == out[j-1].count && out[j].owner < out[j-1].owner) {
				out[j], out[j-1] = out[j-1], out[j]
			} else {
				break
			}
		}
	}
	return out
}
