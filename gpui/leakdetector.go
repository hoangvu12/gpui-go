package gpui

// This file is the port's leak/cycle diagnostics (ticket27): the
// leak-detector snapshot and assertion over the REAL entity map, with
// type-name listings and the semantic labels this port records.
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/app.rs:1005-1040 — App::leak_detector_snapshot /
//     App::assert_no_new_leaks ("Captures a snapshot of all entities
//     that currently have alive handles ... verify that no entities
//     created after the snapshot are still alive", "The panic message
//     lists every leaked entity with its type name");
//   - crates/gpui/src/app/entity_map.rs:930-1095 — the leak detector:
//     EntityLeakData { handles: HashMap<HandleId, Option<Backtrace>>,
//     type_name }, LeakDetector::handle_created/handle_released,
//     snapshot() (the set of alive entity ids) and
//     assert_no_new_leaks (the panic listing "Leaked handle for entity
//     {type_name} ({entity_id:?})" with allocation-site backtraces when
//     the LEAK_BACKTRACE env var is set, otherwise the hint "(export
//     LEAK_BACKTRACE to find allocation site)");
//   - the same cfg gate everywhere: #[cfg(any(test, feature =
//     "leak-detection"))] — the test/leak-detection feature gating the
//     ticket maps onto the port's explicit test-support surface.
//
// Recorded applicability of the Rust-specific mechanisms (ledger
// note, not a silent exemption):
//
//   - LEAK_BACKTRACE allocation-site capture is a Rust-only mechanism
//     (backtrace::Backtrace at handle creation). The Go port records
//     the allocation-context labels it does have: each leaked entity's
//     TYPE NAME and the lease-owner scopes currently holding it (the
//     ownership labels of App.OwnershipReport). No stack capture is
//     invented.
//   - The pin's LeakDetector::drop reports remaining leaks at app
//     teardown (a Rust destructor). Go has no destructor: the port
//     surfaces the same content through AssertNoNewLeaks at the
//     owner's chosen boundary.
//   - The pin PANICS from assert_no_new_leaks (a test-support panic).
//     The Go port returns the report lines (empty slice = clean) and
//     the test package fails on any line — the same observable, at
//     the a11y-diagnostics precedent's fidelity, without turning
//     release mis-calls into crashes.
//   - The ArcTracker mention in the ticket's pointer does not exist at
//     this pin: the tracked structure is EntityRefCounts/LeakDetector
//     (entity_map.rs:58-77, 930+). This port follows the pin's
//     actual shape and records the discrepancy.
//
// Capability mapping: #[cfg(any(test, feature = "leak-detection"))]
// becomes the CapLeakDetection capability bit (inspector.go). The
// surface itself is pure observation over the entity map with zero
// call sites inside the runtime, so the RELEASE PROFILE's behavior is
// untouched whether or not the capability is set; the bit documents
// the intended profile (test-support) and gates nothing else.

import (
	"fmt"
	"sort"
	"strings"
)

// LeakDetectorSnapshot is a snapshot of the set of alive entities at a
// point in time (entity_map.rs LeakDetectorSnapshot). Pass it to
// App.AssertNoNewLeaks to verify that no entities created after the
// snapshot are still alive.
type LeakDetectorSnapshot struct {
	// entities records each live entity at snapshot time: its
	// type-name and lease-owner labels (the port's allocation-context
	// record).
	entities map[EntityID]leakEntityRecord
}

// leakEntityRecord is one snapshot entity's semantic labels.
type leakEntityRecord struct {
	// typeName is the entity state's Go type (the pin's &'static str
	// type name).
	typeName string
	// leases is the live lease count at snapshot time.
	leases int
	// owners are the lease-owner scope labels at snapshot time.
	owners []string
}

// String renders the snapshot for diagnostics.
func (s *LeakDetectorSnapshot) String() string {
	if s == nil {
		return "leak detector snapshot: <nil>"
	}
	ids := make([]EntityID, 0, len(s.entities))
	for id := range s.entities {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	var b strings.Builder
	fmt.Fprintf(&b, "leak detector snapshot: %d entities\n", len(ids))
	for _, id := range ids {
		record := s.entities[id]
		fmt.Fprintf(&b, "  entity %d (%s) %d lease(s): %s\n",
			uint64(id), record.typeName, record.leases, strings.Join(record.owners, ", "))
	}
	return b.String()
}

// EntityCount returns how many entities were live at snapshot time.
func (s *LeakDetectorSnapshot) EntityCount() int {
	if s == nil {
		return 0
	}
	return len(s.entities)
}

// leakLabels collects one live entity's semantic labels: the state's
// type name and the lease-owner scopes holding it (the
// OwnershipReport scope-label vocabulary — the port's
// allocation-context record where the pin captures allocation-site
// backtraces).
func (a *App) leakLabels(id EntityID, slot *entitySlot) (typeName string, leases int, owners []string) {
	typeName = "<unknown state>"
	if slot.state != nil {
		typeName = fmt.Sprintf("%T", slot.state)
	}
	// Map dependent scopes back to owning entities (OwnershipReport's
	// dependent-scope labeling).
	dependent := make(map[*Scope]EntityID)
	a.entities.forEachLive(func(other EntityID, otherSlot *entitySlot) {
		if otherSlot.dependent != nil {
			dependent[otherSlot.dependent] = other
		}
	})
	scopeLabel := func(s *Scope) string {
		if owner, ok := dependent[s]; ok {
			return fmt.Sprintf("dependent scope of entity %d", uint64(owner))
		}
		if s == a.rootScope {
			return "root scope"
		}
		return "scope"
	}
	var collect func(s *Scope)
	collect = func(s *Scope) {
		for _, lease := range s.leases {
			if lease.id == id {
				leases++
				owners = append(owners, scopeLabel(s))
			}
		}
		for _, child := range s.children {
			collect(child)
		}
	}
	collect(a.rootScope)
	for _, scope := range a.arena {
		for _, lease := range scope.leases {
			if lease.id == id {
				leases++
				owners = append(owners, scopeLabel(scope))
			}
		}
	}
	if owners == nil {
		owners = []string{"<unregistered lease>"}
	}
	sort.Strings(owners)
	return typeName, leases, owners
}

// LeakDetectorSnapshot captures a snapshot of all entities that
// currently have alive handles (App::leak_detector_snapshot, app.rs
// 1005-1016). Foreground thread (the owning dispatcher goroutine).
func (a *App) LeakDetectorSnapshot() *LeakDetectorSnapshot {
	snapshot := &LeakDetectorSnapshot{entities: make(map[EntityID]leakEntityRecord)}
	a.entities.forEachLive(func(id EntityID, slot *entitySlot) {
		typeName, leases, owners := a.leakLabels(id, slot)
		snapshot.entities[id] = leakEntityRecord{typeName: typeName, leases: leases, owners: owners}
	})
	return snapshot
}

// AssertNoNewLeaks asserts that no entities created after the snapshot
// still have alive handles (App::assert_no_new_leaks, app.rs
// 1018-1031). Entities already tracked at snapshot time are ignored
// even if they still have handles; only NEW entities (ids absent from
// the snapshot) are leaks. It returns the leak report lines — an
// empty slice is clean — listing every leaked entity with its type
// name and this port's allocation-context labels (the lease-owner
// scopes; see the file header for the LEAK_BACKTRACE applicability
// record). The report is observation only: ownership is never
// mutated.
func (a *App) AssertNoNewLeaks(snapshot *LeakDetectorSnapshot) []string {
	if snapshot == nil {
		return []string{"no leak detector snapshot: the assertion needs a snapshot from LeakDetectorSnapshot"}
	}
	var lines []string
	var live []EntityID
	a.entities.forEachLive(func(id EntityID, slot *entitySlot) {
		if _, tracked := snapshot.entities[id]; tracked {
			return
		}
		live = append(live, id)
		if slot.count > 0 {
			typeName, _, owners := a.leakLabels(id, slot)
			lines = append(lines, fmt.Sprintf(
				"leaked entity %d (%s): %d lease(s) held by: %s (allocation-context: this port records lease-owner scopes; the reference's LEAK_BACKTRACE allocation-site capture is a Rust-only mechanism)",
				uint64(id), typeName, slot.count, strings.Join(owners, ", ")))
		}
	})
	if len(lines) > 0 {
		out := make([]string, 0, len(lines)+1)
		out = append(out, "new entity leaks detected since snapshot:")
		out = append(out, lines...)
		return out
	}
	return nil
}

// AssertEntityReleased asserts that every handle to the given entity
// has been released (entity_map.rs LeakDetector::assert_released):
// the returned lines are empty when the entity is fully released (or
// never existed); otherwise they report the entity's type name, live
// lease count and lease-owner labels.
func (a *App) AssertEntityReleased(id EntityID) []string {
	slot := a.entities.slot(id)
	if slot == nil || slot.count <= 0 {
		return nil
	}
	typeName, leases, owners := a.leakLabels(id, slot)
	return []string{fmt.Sprintf(
		"entity %d (%s) still has %d live lease(s): %s",
		uint64(id), typeName, leases, strings.Join(owners, ", "))}
}

// LeakCycleLines returns the strong-cycle diagnostics of the current
// ownership graph: each cycle among entities whose leases are held by
// other entities' dependent scopes, reported (never broken) exactly
// like App.OwnershipReport's cycle lines ("cycles are not collected;
// break an edge explicitly"). This is the leak detector's cycle
// counterpart the ticket requires: diagnosing ownership problems
// without altering release behavior — the report only observes.
func (a *App) LeakCycleLines() []string {
	var lines []string
	for _, line := range a.OwnershipReport() {
		if strings.HasPrefix(line, "strong cycle:") {
			lines = append(lines, line)
		}
	}
	return lines
}
