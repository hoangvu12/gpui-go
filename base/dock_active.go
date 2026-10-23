package base

// Port of U:dock/active.rs: the per-group record of what each panel was
// last told about its active state.

import "slices"

// activeTracker tracks what each panel was last told about its active
// state, so a group emits exactly one notification per real edge
// (U:dock/active.rs:5-23). TabGroup owns one and is the only thing that
// does.
type activeTracker struct {
	notified      map[PanelID]bool
	syncScheduled bool
}

// activeChange is one (panel, active) delivery.
type activeChange struct {
	panel  PanelID
	active bool
}

// reconcile compares the group's membership and displayed panel against
// what each panel was last told, and returns only the real edges,
// deactivations before the activation (U:dock/active.rs:25-82). A panel
// that left the group is forgotten, not deactivated; a panel never told
// true is never told false.
func (a *activeTracker) reconcile(panels []PanelID, displayed PanelID, hasDisplayed bool) []activeChange {
	if a.notified == nil {
		a.notified = make(map[PanelID]bool)
	}
	for panel := range a.notified {
		if !slices.Contains(panels, panel) {
			delete(a.notified, panel)
		}
	}

	var deactivations []activeChange
	var activation *activeChange
	for _, panel := range panels {
		shouldBeActive := hasDisplayed && displayed == panel
		wasActive, known := a.notified[panel]
		if known && wasActive == shouldBeActive {
			continue
		}
		if !shouldBeActive && !known {
			// Never announce false to a panel that was never told true.
			a.notified[panel] = false
			continue
		}
		a.notified[panel] = shouldBeActive
		if shouldBeActive {
			activation = &activeChange{panel, true}
		} else {
			deactivations = append(deactivations, activeChange{panel, false})
		}
	}
	if activation != nil {
		deactivations = append(deactivations, *activation)
	}
	return deactivations
}

// seed records what a panel was last told, without emitting anything
// (U:dock/active.rs:84-89).
func (a *activeTracker) seed(panel PanelID, active bool) {
	if a.notified == nil {
		a.notified = make(map[PanelID]bool)
	}
	a.notified[panel] = active
}

// lastNotified returns what this tracker last told panel, if anything
// (U:dock/active.rs:91-101).
func (a *activeTracker) lastNotified(panel PanelID) (bool, bool) {
	active, ok := a.notified[panel]
	return active, ok
}

// tracks is the test-only membership read (U:dock/active.rs:103-106).
func (a *activeTracker) tracks(panel PanelID) bool {
	_, ok := a.notified[panel]
	return ok
}

// scheduleSync reports whether a sync needs scheduling
// (U:dock/active.rs:108-115).
func (a *activeTracker) scheduleSync() bool {
	needed := !a.syncScheduled
	a.syncScheduled = true
	return needed
}

// syncFinished marks the deferred sync delivered (U:dock/active.rs:117-122).
func (a *activeTracker) syncFinished() { a.syncScheduled = false }
