// Package ledger generates the versioned gpui-go capability ledger from the
// pinned CE api-scan artifact.
//
// The ledger enumerates every symbol of the scanned crates (the public gpui
// crate plus its supporting crates), resolves which conformance profiles each
// symbol exists in, assigns an implementation owner from the family mapping in
// docs/conformance-inventory.md, and records the evidence state of each row.
// Rows start in the "planned" state; later slices move them to
// "recorded-reference", "passed", "failed", "blocked-environment" or
// "approved-exception" as fixtures run.
//
// Dispositions (schema @2): every row carries a Disposition recording how it
// was placed. "family" rows belong to an implementation owner ticket (the
// Owner/OwnerSource fields name it). "core-structure" rows are non-observable
// structure (private modules, bare re-exports, support-crate surface no gpui
// public API exercises); their Owner is the OwnerStructural marker, not a
// ticket. "platform-inapplicable" rows are gated off Windows by a crate-level
// or item cfg; that is an applicability fact recorded with a provenance note,
// never an approved exception. Dispositions keep the final audit's "no
// applicable pending rows" rule from counting non-observable or
// platform-inapplicable rows as work.
//
// This package depends only on the standard library and must not import the
// gpui root package.
package ledger

// LedgerSchema identifies the capability ledger format version. Version @2
// adds the per-row Disposition field and the Summary.ByDisposition counts of
// the ticket-34 core-support triage; @1 rows carried no disposition.
const LedgerSchema = "gpui-go/capability-ledger@2"

// Evidence states defined by docs/conformance-contract.md. Every row starts
// as planned; no state transition happens inside this package.
const (
	StatePlanned            = "planned"
	StateRecordedReference  = "recorded-reference"
	StatePassed             = "passed"
	StateFailed             = "failed"
	StateBlockedEnvironment = "blocked-environment"
	StateApprovedException  = "approved-exception"
)

// ValidStates lists the evidence states in contract order.
var ValidStates = []string{
	StatePlanned,
	StateRecordedReference,
	StatePassed,
	StateFailed,
	StateBlockedEnvironment,
	StateApprovedException,
}

// Dispositions record how a ledger row was placed, per the ticket-34
// core-support triage. The values are mutually exclusive per row.
const (
	// DispositionFamily marks rows assigned to an implementation owner
	// ticket through a family rule (Owner holds the ticket id, OwnerSource
	// names the family).
	DispositionFamily = "family"
	// DispositionCoreStructure marks rows documented as non-observable
	// structure: private modules, bare re-exports and support-crate surface
	// that no gpui public API exercises. Owner is the OwnerStructural
	// marker; the Notes field carries the rationale and source citation.
	DispositionCoreStructure = "core-structure"
	// DispositionPlatformInapplicable marks rows whose platform gate
	// (a crate-level cfg the api-scan does not record, or the explicit
	// platform-inapplicable family) removes them from the Windows core.
	// Inapplicable-by-platform is an applicability fact, not an exception.
	DispositionPlatformInapplicable = "platform-inapplicable"
)

// ValidDispositions lists the disposition values in review order.
var ValidDispositions = []string{
	DispositionFamily,
	DispositionCoreStructure,
	DispositionPlatformInapplicable,
}

// ValidDisposition reports whether s is one of the disposition values.
func ValidDisposition(s string) bool {
	for _, v := range ValidDispositions {
		if v == s {
			return true
		}
	}
	return false
}

// Row is one enumerated capability of the scanned surface.
//
// Symbol is the api-scan path (e.g. "gpui::Platform::open_window"), Crate is
// the package name from api-scan (e.g. "gpui-ce", "gpui_ce_windows"), Source
// is "file:line" within the pinned CE checkout, and Cfg holds the item's
// effective cfg conditions as written in the scan. Profile lists the profile
// ids under whose feature sets the cfg evaluates true on a Windows target; a
// row with an empty Profile slice does not exist on Windows in any recorded
// profile (WindowsApplicable is false). Empty Cfg means the row exists in
// every profile.
type Row struct {
	Symbol            string   `json:"symbol"`
	Kind              string   `json:"kind"`
	Crate             string   `json:"crate"`
	Source            string   `json:"source"`
	Cfg               []string `json:"cfg"`
	Profile           []string `json:"profile"`
	WindowsApplicable bool     `json:"windows_applicable"`
	BodyMarker        string   `json:"body_marker,omitempty"`
	Doc               string   `json:"doc,omitempty"`
	Owner             string   `json:"owner,omitempty"`
	OwnerSource       string   `json:"owner_source,omitempty"`
	Fixtures          []string `json:"fixtures,omitempty"`
	State             string   `json:"state"`
	Disposition       string   `json:"disposition"`
	Notes             string   `json:"notes,omitempty"`
}

// Summary aggregates the ledger for quick review. ByOwner counts rows per
// owner ticket id with the empty key holding the unassigned rows and the
// OwnerStructural marker key holding non-observable structure; ByState and
// ByProfile count rows per evidence state and profile id; ByDisposition
// counts rows per disposition value.
type Summary struct {
	TotalRows          int            `json:"total_rows"`
	ByOwner            map[string]int `json:"by_owner"`
	ByState            map[string]int `json:"by_state"`
	ByDisposition      map[string]int `json:"by_disposition"`
	ByProfile          map[string]int `json:"by_profile"`
	UnassignedCount    int            `json:"unassigned_count"`
	UnimplementedCount int            `json:"unimplemented_count"`
	FeatureGatedCount  int            `json:"feature_gated_count"`
}

// SourceIdentity records where the ledger came from: the api-scan artifact
// identity, the generation timestamp (RFC3339), the Go toolchain (GOVERSION
// environment variable with a runtime.Version fallback) and the conformance
// profile records used for cfg evaluation.
type SourceIdentity struct {
	ApiScanSchema string                   `json:"api_scan_schema"`
	ApiScanCommit string                   `json:"api_scan_commit"`
	GeneratedAt   string                   `json:"generated_at"`
	Toolchain     string                   `json:"toolchain"`
	Profiles      map[string]ProfileRecord `json:"profiles"`
}

// ProfileRecord describes one conformance profile: the cargo feature set the
// reference graph resolves to, a human description, and the cargo tree
// artifact that proves the graph. CargoFeatures doubles as the active feature
// set used by the cfg evaluator ("default" is recorded but is not a
// cfg-testable feature name; cfg feature = "default" is treated as always
// true).
type ProfileRecord struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	CargoFeatures []string `json:"cargo_features"`
	GraphFile     string   `json:"graph_file,omitempty"`
}

// Ledger is the generated capability ledger document.
type Ledger struct {
	Schema        string         `json:"schema"`
	GeneratedFrom SourceIdentity `json:"generated_from"`
	Rows          []Row          `json:"rows"`
	Summary       Summary        `json:"summary"`
	Unassigned    []Row          `json:"unassigned"`
}

// ValidState reports whether s is one of the contract's evidence states.
func ValidState(s string) bool {
	for _, v := range ValidStates {
		if v == s {
			return true
		}
	}
	return false
}

// Validate checks the ledger's structural invariants: schema identity, valid
// states and dispositions, the structural-marker/owner agreement, non-empty
// symbols and sources, and a summary consistent with the rows.
func (l *Ledger) Validate() error {
	if l.Schema != LedgerSchema {
		return &SchemaError{Want: LedgerSchema, Got: l.Schema, Field: "schema"}
	}
	if len(l.Rows) != l.Summary.TotalRows {
		return &SchemaError{Want: "summary.total_rows == len(rows)", Got: "mismatch", Field: "summary.total_rows"}
	}
	counts := make(map[string]int, len(l.Summary.ByOwner))
	dispositions := make(map[string]int, len(l.Summary.ByDisposition))
	unassigned := 0
	for _, r := range l.Rows {
		if r.Symbol == "" {
			return &SchemaError{Want: "non-empty symbol", Got: "empty", Field: "row.symbol"}
		}
		if r.Source == "" {
			return &SchemaError{Want: "non-empty source", Got: "empty", Field: "row.source"}
		}
		if !ValidState(r.State) {
			return &SchemaError{Want: "valid state", Got: r.State, Field: "row.state"}
		}
		if !ValidDisposition(r.Disposition) {
			return &SchemaError{Want: "valid disposition", Got: r.Disposition, Field: "row.disposition"}
		}
		// The core-structure disposition and the OwnerStructural marker owner
		// must agree so the audit can rely on either field alone.
		if (r.Disposition == DispositionCoreStructure) != (r.Owner == OwnerStructural) {
			return &SchemaError{Want: "core-structure disposition iff OwnerStructural owner", Got: r.Owner, Field: "row.disposition"}
		}
		if r.Owner == "" {
			unassigned++
		} else {
			counts[r.Owner]++
		}
		dispositions[r.Disposition]++
	}
	if unassigned != l.Summary.UnassignedCount {
		return &SchemaError{Want: "summary.unassigned_count matches rows", Got: "mismatch", Field: "summary.unassigned_count"}
	}
	if len(counts) != len(l.Summary.ByOwner) {
		return &SchemaError{Want: "summary.by_owner matches rows", Got: "mismatch", Field: "summary.by_owner"}
	}
	if len(dispositions) != len(l.Summary.ByDisposition) {
		return &SchemaError{Want: "summary.by_disposition matches rows", Got: "mismatch", Field: "summary.by_disposition"}
	}
	for _, d := range ValidDispositions {
		if dispositions[d] != l.Summary.ByDisposition[d] {
			return &SchemaError{Want: "summary.by_disposition matches rows", Got: "mismatch", Field: "summary.by_disposition"}
		}
	}
	return nil
}

// SchemaError reports a structural mismatch in a ledger or input artifact.
type SchemaError struct {
	Field string
	Want  string
	Got   string
}

func (e *SchemaError) Error() string {
	return "ledger: " + e.Field + ": want " + e.Want + ", got " + e.Got
}
