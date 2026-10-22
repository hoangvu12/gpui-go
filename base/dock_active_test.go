package base

import (
	"slices"
	"testing"
)

// Ported tests of U:dock/active.rs (7/7).

func requireChanges(t *testing.T, got []activeChange, want []activeChange, msg string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("%s: changes = %v, want %v", msg, got, want)
	}
}

// U:dock/active.rs:133-139
func TestDockActiveTheFirstReconcileAnnouncesTheDisplayedPanelOnly(t *testing.T) {
	var tracker activeTracker
	changes := tracker.reconcile([]PanelID{dpid(1), dpid(2)}, dpid(1), true)
	requireChanges(t, changes, []activeChange{{dpid(1), true}}, "first reconcile")
}

// U:dock/active.rs:141-149
func TestDockActiveSwitchingReportsTheOldPanelFalseThenTheNewPanelTrue(t *testing.T) {
	var tracker activeTracker
	tracker.reconcile([]PanelID{dpid(1), dpid(2)}, dpid(1), true)
	changes := tracker.reconcile([]PanelID{dpid(1), dpid(2)}, dpid(2), true)
	requireChanges(t, changes, []activeChange{{dpid(1), false}, {dpid(2), true}}, "switch")
}

// U:dock/active.rs:151-157
func TestDockActiveReselectingTheDisplayedPanelIsSilent(t *testing.T) {
	var tracker activeTracker
	tracker.reconcile([]PanelID{dpid(1)}, dpid(1), true)
	requireChanges(t, tracker.reconcile([]PanelID{dpid(1)}, dpid(1), true), nil, "reselect")
}

// U:dock/active.rs:159-172
func TestDockActiveARemovedPanelIsForgottenRatherThanToldFalse(t *testing.T) {
	var tracker activeTracker
	tracker.reconcile([]PanelID{dpid(1), dpid(2)}, dpid(1), true)
	changes := tracker.reconcile([]PanelID{dpid(2)}, dpid(2), true)
	requireChanges(t, changes, []activeChange{{dpid(2), true}}, "panel 1 left the group; on_removed is its deactivation signal")
	if tracker.tracks(dpid(1)) {
		t.Fatal("the removed panel is still tracked")
	}
}

// U:dock/active.rs:174-183
func TestDockActiveSeedingAMovedPanelPreventsASpuriousReactivation(t *testing.T) {
	var tracker activeTracker
	tracker.seed(dpid(1), true)
	requireChanges(t, tracker.reconcile([]PanelID{dpid(1)}, dpid(1), true), nil,
		"a panel dragged in while displayed must not be told true twice")
}

// U:dock/active.rs:185-193
func TestDockActiveNoPanelDisplayedDeactivatesThePreviousOne(t *testing.T) {
	var tracker activeTracker
	tracker.reconcile([]PanelID{dpid(1)}, dpid(1), true)
	changes := tracker.reconcile([]PanelID{dpid(1)}, 0, false)
	requireChanges(t, changes, []activeChange{{dpid(1), false}}, "nothing displayed")
}

// U:dock/active.rs:195-221
func TestDockActiveLastNotifiedReadsAPanelsStateForHandoffToAnotherTracker(t *testing.T) {
	var source activeTracker
	source.reconcile([]PanelID{dpid(1), dpid(2)}, dpid(1), true)

	if active, ok := source.lastNotified(dpid(1)); !ok || !active {
		t.Fatal("panel 1 was told true")
	}
	if active, ok := source.lastNotified(dpid(2)); !ok || active {
		t.Fatal("panel 2 was recorded false")
	}
	if _, ok := source.lastNotified(dpid(3)); ok {
		t.Fatal("a panel never reconciled has no recorded state")
	}

	var destination activeTracker
	active, _ := source.lastNotified(dpid(1))
	destination.seed(dpid(1), active)
	requireChanges(t, destination.reconcile([]PanelID{dpid(1)}, dpid(1), true), nil,
		"seeded from the source's last_notified value, the move must not look like a fresh edge")
}
