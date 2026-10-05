package ledger

import (
	"encoding/json"
	"fmt"
	"os"
)

// ApiScanSchema is the schema identifier of the api-scan input artifact.
const ApiScanSchema = "gpui-go/api-scan@1"

// ApiScan is the pinned CE api-scan artifact: every public item of every
// scanned crate plus the feature-gate trace. It is defined by the scanner in
// the reference checkout; the JSON field names match that output exactly.
// doc, body_marker and receiver may be absent and use pointers.
type ApiScan struct {
	Schema       string                   `json:"schema"`
	SourceCommit string                   `json:"source_commit"`
	Crates       []ScanCrate              `json:"crates"`
	FeatureTrace map[string][]FeatureSite `json:"feature_trace"`
}

// ScanCrate is one scanned crate: package is the cargo package name used as
// the ledger row's Crate field, lib_name the Rust library name.
type ScanCrate struct {
	Package           string     `json:"package"`
	LibName           string     `json:"lib_name"`
	Manifest          string     `json:"manifest"`
	RootFile          string     `json:"root_file"`
	Items             []ScanItem `json:"items"`
	UnresolvedModules []string   `json:"unresolved_modules"`
}

// ScanItem is one public item (module, use, struct, enum, trait, fn, method,
// impl-method, trait-method, const, static, type, macro, field or variant).
// Cfg holds the item's own cfg attribute strings; the scanner does not
// propagate cfg from parent module declarations (see the profiler parent-gate
// note in generate.go).
type ScanItem struct {
	Path       string   `json:"path"`
	Kind       string   `json:"kind"`
	File       string   `json:"file"`
	Line       int      `json:"line"`
	Cfg        []string `json:"cfg"`
	Doc        *string  `json:"doc"`
	BodyMarker *string  `json:"body_marker"`
	Receiver   *string  `json:"receiver"`
}

// FeatureSite is one cfg site mentioning a feature, recorded per feature name
// in the scan's feature_trace map.
type FeatureSite struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Context string `json:"context"`
	Cfg     string `json:"cfg"`
}

// LoadApiScan reads and validates an api-scan artifact.
func LoadApiScan(path string) (*ApiScan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("ledger: read api-scan %s: %w", path, err)
	}
	var scan ApiScan
	if err := json.Unmarshal(data, &scan); err != nil {
		return nil, fmt.Errorf("ledger: parse api-scan %s: %w", path, err)
	}
	if scan.Schema != ApiScanSchema {
		return nil, &SchemaError{Field: "api-scan schema", Want: ApiScanSchema, Got: scan.Schema}
	}
	if scan.SourceCommit == "" {
		return nil, &SchemaError{Field: "api-scan source_commit", Want: "non-empty", Got: "empty"}
	}
	if len(scan.Crates) == 0 {
		return nil, &SchemaError{Field: "api-scan crates", Want: "non-empty", Got: "empty"}
	}
	for i := range scan.Crates {
		c := &scan.Crates[i]
		if c.Package == "" {
			return nil, &SchemaError{Field: fmt.Sprintf("api-scan crates[%d].package", i), Want: "non-empty", Got: "empty"}
		}
		for j := range c.Items {
			it := &c.Items[j]
			if it.Path == "" {
				return nil, &SchemaError{Field: fmt.Sprintf("api-scan crates[%s].items[%d].path", c.Package, j), Want: "non-empty", Got: "empty"}
			}
			if it.Kind == "" {
				return nil, &SchemaError{Field: fmt.Sprintf("api-scan crates[%s].items[%d].kind", c.Package, j), Want: "non-empty", Got: "empty"}
			}
			if it.File == "" {
				return nil, &SchemaError{Field: fmt.Sprintf("api-scan crates[%s].items[%d].file", c.Package, j), Want: "non-empty", Got: "empty"}
			}
			if it.Line <= 0 {
				return nil, &SchemaError{Field: fmt.Sprintf("api-scan crates[%s].items[%d].line", c.Package, j), Want: "positive", Got: fmt.Sprint(it.Line)}
			}
		}
	}
	return &scan, nil
}
