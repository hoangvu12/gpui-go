package gpui

import (
	"strings"
	"testing"
)

// Unit tests for the accessibility identity policy (ticket21): the node
// id derivation from semantic paths, the synthetic child id derivation,
// and the canonical encoding's disambiguation. The full tree/snapshot/
// action corpus lives in internal/accessspec.

// TestA11yNodeIDDeterminism pins the node-id policy: ids derive from the
// canonical semantic path, are stable across calls (and processes: no
// addresses or map order), and distinct paths of every id kind never
// collide through the encoding.
func TestA11yNodeIDDeterminism(t *testing.T) {
	a := NameElementID("counter")
	b := NameElementID("counter")
	idA, okA := A11yNodeIDOf(&GlobalElementID{Path: []ElementID{a}})
	idB, okB := A11yNodeIDOf(&GlobalElementID{Path: []ElementID{b}})
	if !okA || !okB {
		t.Fatal("elements with ids must produce node ids")
	}
	if idA != idB {
		t.Errorf("same path produced different ids: %s vs %s", idA, idB)
	}
	if idA == RootA11yNodeID {
		t.Errorf("non-root path produced the root id")
	}

	// Different names, different ids.
	idOther, _ := A11yNodeIDOf(&GlobalElementID{Path: []ElementID{NameElementID("editor")}})
	if idOther == idA {
		t.Errorf("different names produced the same id")
	}

	// Path depth matters: appending a component changes the id.
	deep, _ := A11yNodeIDOf(&GlobalElementID{Path: []ElementID{a, NameElementID("label")}})
	if deep == idA {
		t.Errorf("nested path collided with its parent's id")
	}

	// The canonical encoding disambiguates kinds and separators: a name
	// containing the display separator never collides with the two-name
	// path that renders identically.
	dotted, _ := A11yNodeIDOf(&GlobalElementID{Path: []ElementID{NameElementID("a.b")}})
	twoNames, _ := A11yNodeIDOf(&GlobalElementID{Path: []ElementID{NameElementID("a"), NameElementID("b")}})
	if dotted == twoNames {
		t.Errorf("the dotted name collided with the two-name path (canonical encoding failure)")
	}
	if dotted == idA {
		t.Errorf("name %q collided with name %q", "a.b", "counter")
	}

	// Different kinds with the same display string stay distinct.
	view, _ := A11yNodeIDOf(&GlobalElementID{Path: []ElementID{ViewElementID(7)}})
	integer, _ := A11yNodeIDOf(&GlobalElementID{Path: []ElementID{IntegerElementID(7)}})
	if view == integer {
		t.Errorf("view id 7 collided with integer id 7 (kind tagging failure)")
	}

	// A nil or empty path produces no node (a11y.rs: nodes without a
	// GlobalElementId cannot join the tree).
	if _, ok := A11yNodeIDOf(nil); ok {
		t.Errorf("nil path produced a node id")
	}
	if _, ok := A11yNodeIDOf(&GlobalElementID{}); ok {
		t.Errorf("empty path produced a node id")
	}
}

// TestSyntheticA11yNodeID pins the synthetic child derivation (a11y.rs
// synthetic_node_id: hash of the parent id and the key): keys unique
// within one call, parent-scoped across calls, deterministic.
func TestSyntheticA11yNodeID(t *testing.T) {
	parent := A11yNodeID(0xfeed)
	run0 := syntheticA11yNodeID(parent, 0)
	run1 := syntheticA11yNodeID(parent, 1)
	if run0 == run1 {
		t.Errorf("distinct keys produced the same synthetic id")
	}
	if run0 == parent || run1 == parent {
		t.Errorf("synthetic id collided with its parent id")
	}
	// Deterministic.
	if syntheticA11yNodeID(parent, 0) != run0 {
		t.Errorf("synthetic id is not deterministic")
	}
	// Parent-scoped: the same key under a different parent is a
	// different node (keys "may be duplicated across different calls").
	other := syntheticA11yNodeID(A11yNodeID(0xbeef), 0)
	if other == run0 {
		t.Errorf("synthetic id leaked across parents")
	}
	// Key types stay distinct.
	if syntheticA11yNodeID(parent, "0") == run0 {
		t.Errorf("string key collided with int key")
	}
}

// TestA11ySpecFocusBinding covers the focus binding plumbing.
func TestA11ySpecFocusBinding(t *testing.T) {
	spec := A11ySpec{Role: RoleButton}
	if _, ok := spec.focusBinding(); ok {
		t.Errorf("unbound spec reported a focus binding")
	}
	bound := spec.WithFocus(FocusHandle{id: 3})
	if handle, ok := bound.focusBinding(); !ok || handle.id != 3 {
		t.Errorf("WithFocus did not bind the handle")
	}
	if !specPublishes(bound, true) {
		t.Errorf("a focus binding must publish the node")
	}
	if specPublishes(A11ySpec{}, true) {
		t.Errorf("an empty spec must not publish a node")
	}
	if specPublishes(A11ySpec{}, false) {
		t.Errorf("an element without a companion must not publish a node")
	}
	// Hidden-only nodes publish (aria_hidden without a role, the guide).
	if !specPublishes(A11ySpec{Hidden: true}, true) {
		t.Errorf("a hidden subtree must publish its node")
	}
}

// TestA11ySnapshotTraceIsDeterministic checks the trace shape used by
// fixture comparison.
func TestA11ySnapshotTraceIsDeterministic(t *testing.T) {
	snapshot := &A11ySnapshot{
		windowID: 1, generation: 1, sequence: 2, title: "Counter",
		nodes: []A11ySnapshotNode{
			{ID: 7, Role: RoleButton, Label: "Increment", AuthorID: "counter.increment", Children: nil, Actions: []A11yAction{A11yActionClick}},
			{ID: RootA11yNodeID, Role: RoleWindow, Label: "Counter", Children: []A11yNodeID{7}},
		},
		root: RootA11yNodeID,
	}
	trace := snapshot.Trace()
	first := snapshot.Trace()
	if trace != first {
		t.Fatalf("trace is not deterministic")
	}
	if !strings.Contains(trace, "#0000000000000000 Window label=\"Counter\"") {
		t.Errorf("trace missing the root line: %q", trace)
	}
	if !strings.Contains(trace, `Button id="counter.increment" label="Increment"`) {
		t.Errorf("trace missing the button properties: %q", trace)
	}
	if !strings.Contains(trace, "actions=[Click]") {
		t.Errorf("trace missing the actions: %q", trace)
	}
	if !strings.Contains(trace, "focus=#0000000000000000") {
		t.Errorf("trace missing the focus line: %q", trace)
	}
}
