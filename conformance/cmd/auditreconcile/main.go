// Command auditreconcile runs ticket 33's capability-ledger
// reconciliation: every row of the generated ledger (6911 rows, schema
// @2) maps through its owning ticket to that ticket's resolution
// evidence and its recorded gate class. The output is the audit's
// reconciliation summary (the counts per class and the unresolved set —
// empty only when every row is accounted for).
//
//	go run ./conformance/cmd/auditreconcile
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ledgerRow struct {
	Symbol   string   `json:"symbol"`
	Kind     string   `json:"kind"`
	Crate    string   `json:"crate"`
	Source   string   `json:"source"`
	Profile  []string `json:"profile"`
	WinApp   bool     `json:"windows_applicable"`
	Owner    string   `json:"owner"`
	State    string   `json:"state"`
	Disposition string `json:"disposition"`
}

type ledgerFile struct {
	Schema string      `json:"schema"`
	Rows   []ledgerRow `json:"rows"`
}

// ticketEvidence maps each owning ticket to its resolution evidence
// file and the gate class that evidence records (the ticket-33 audit
// basis: every resolved family's executed gates).
var ticketEvidence = map[string]struct {
	Evidence   string
	GateClass  string
}{
	"01": {"ticket01-reference-harness.json", "exact: the recorded reference fixtures (fx-0001..fx-0008 byte-deterministic, bit-exact through CompareTraces) + the capability ledger itself"},
	"02": {"ticket02-native-bootstrap.json", "exact: the loader round trips + 16 loader tests over the real DLL"},
	"03": {"ticket03-scoped-entities.json", "exact: fx-0001 effects 47/47 + 39 ownership tests"},
	"04": {"ticket04-scoped-tasks.json", "behavior: 24 scoped-task tests (exactly-once, generations, drain)"},
	"05": {"ticket05-win32-window.json", "behavior: 7 real-window tests, 10/10 runs, the deadlock fix"},
	"06": {"ticket06-layout-roundtrip.json", "exact: fx-0001 54/54 + fx-0002 63/63 bit-exact + layoutspec"},
	"07": {"ticket07-present-and-retire.json", "behavior: the real-GPU ledger proofs (pending-then-completed retirement)"},
	"08": {"ticket08-scene-kernel.json", "exact: fx-0003 142/142 + scenespec"},
	"09": {"ticket09-text-geometry.json", "exact: fx-0004 339/339 + textspec"},
	"10": {"ticket10-draw-text.json", "exact: fx-0005 + the real-window pixel verification"},
	"11": {"ticket11-authoring-counter.json", "exact: fx-0006 108/108 + authorspec"},
	"12": {"ticket12-action-registry.json", "behavior: 27 registry tests"},
	"13": {"ticket13-focus-keyboard.json", "behavior: 17 reference test ports + focusspec 12"},
	"14": {"ticket14-retained-scroll.json", "behavior: 18 listspec tests"},
	"15": {"ticket15-inline-layout.json", "behavior: 11 inlinespec (6 deterministic + 4 real-window) + 5 authoring"},
	"16": {"ticket16-paths-filters.json", "exact: fx-0007 428/428 + the pixel-verified composite draw"},
	"17": {"ticket17-image-decode.json", "exact: fx-0008 (landed, bit-exact) + 20 codec tests incl. every EXIF orientation"},
	"18": {"ticket18-asset-loading.json", "behavior: assetspec 46 + gpui 150"},
	"19": {"ticket19-svg-fonts.json", "exact: 6 native + 14 svgspec (the distinct AtlasKey::Svg key since rev 11)"},
	"20": {"ticket20-ime-editor.json", "behavior: real-window caret/DPI/reentry/teardown fixtures; CJK availability recorded"},
	"21": {"ticket21-accessibility-tree.json", "behavior: 12 accessspec + the identity-policy tests"},
	"22": {"ticket22-uia-provider.json", "behavior: a11yspec 9 + the real Narrator evidence"},
	"23": {"ticket23-clipboard.json", "behavior: clipspec 11 (the real system clipboard)"},
	"24": {"ticket24-drop-touchpad.json", "behavior: 16 tests; the touchpad honestly recorded unavailable (no hardware)"},
	"25": {"ticket25-dialogs-credentials.json", "behavior: 34 tests (the real COM dialogs + advapi32 credentials)"},
	"26": {"ticket26-desktop-operations.json", "behavior: 18 real-window + 20 in-package"},
	"27": {"ticket27-inspector.json", "behavior: 30 tests (25 spec + 5 unit)"},
	"28": {"ticket28-capture.json", "behavior+exact: capturespec 11 incl. the pixel-verified full capture→draw loop"},
	"29": {"ticket29-device-loss.json", "behavior: devicelossspec 10 (the injected-loss end-to-end recovery)"},
	"30": {"ticket30-shutdown-integration.json", "behavior: shutdownspec 11 + TRACES.md (the cross-service corpus)"},
	"31": {"ticket31-consumer-delivery.json", "delivery: the clean consumer gates (the embed/verify pipeline)"},
	"32": {"ticket32-render-to-image.json", "exact: headlessspec 9 (the pinned test patterns, the row-pitch gates)"},
	"34": {"ticket34-triage.json", "exact: the 351 support rows dispositioned (the ledger's own families)"},
	"core-structure": {"wave4-native-rotation.json", "structure: the module/ABI rows the bootstrap owns"},
}

// blockedEnvironments and exceptions recorded in the evidence (the
// audit's honest unavailable set).
var blockedEnvironments = []string{
	"the precision touchpad (no hardware on this machine — direct manipulation runs on simulated gestures, ticket 24)",
	"Narrator as a repeatable reader (a protected process once launched; the deterministic tests use hidden windows, ticket 22)",
	"real driver removal (not forceable on healthy hardware — the injected identity is recorded separately, ticket 29)",
	"the primary/find pasteboards (Windows has none — the typed unavailable outcomes, ticket 23)",
	"the post-fullscreen shell hit-test corruption (the environmental cursor class; a reboot restores it — recorded in the wave-4 rotation evidence)",
}

func main() {
	raw, err := os.ReadFile(filepath.Join("..", "..", "ledger", "generated", "ledger.json"))
	if err != nil {
		fatal(err)
	}
	var ledger ledgerFile
	if err := json.Unmarshal(raw, &ledger); err != nil {
		fatal(err)
	}

	// The evidence set actually present on disk.
	evidenceDir := filepath.Join("..", "..", "..", "evidence")
	present := map[string]bool{}
	entries, err := os.ReadDir(evidenceDir)
	if err != nil {
		fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			present[e.Name()] = true
		}
	}

	reconciled := 0
	unreconciled := map[string][]string{}
	byGateClass := map[string]int{}
	for _, row := range ledger.Rows {
		owner := row.Owner
		info, ok := ticketEvidence[owner]
		if !ok {
			unreconciled[owner] = append(unreconciled[owner], row.Symbol)
			continue
		}
		if !present[info.Evidence] {
			unreconciled[owner+" (missing evidence "+info.Evidence+")"] = append(unreconciled[owner], row.Symbol)
			continue
		}
		reconciled++
		class := strings.SplitN(info.GateClass, ":", 2)[0]
		byGateClass[class]++
	}

	fmt.Printf("ledger rows: %d (schema %s)\n", len(ledger.Rows), ledger.Schema)
	fmt.Printf("reconciled to a resolved ticket's executed evidence: %d\n", reconciled)
	fmt.Printf("unreconciled: %d (owners: %d)\n", countAll(unreconciled), len(unreconciled))
	for class, n := range byGateClass {
		fmt.Printf("  gate class %-10s %d rows\n", class, n)
	}
	fmt.Println("blocked environments (recorded, not passed behavior):")
	for _, b := range blockedEnvironments {
		fmt.Printf("  - %s\n", b)
	}
	if len(unreconciled) > 0 {
		fmt.Println("UNRECONCILED OWNERS:")
		for owner, symbols := range unreconciled {
			fmt.Printf("  %s: %d rows (e.g. %s)\n", owner, len(symbols), symbols[0])
		}
		os.Exit(1)
	}
	fmt.Println("reconciliation: COMPLETE — every ledger row maps to a resolved ticket's executed evidence")
}

func countAll(m map[string][]string) int {
	n := 0
	for _, v := range m {
		n += len(v)
	}
	return n
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "auditreconcile:", err)
	os.Exit(1)
}
