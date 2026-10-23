package base

// Port of U:dock/mod.rs: the dockable layout — splits and tab groups that a
// host can rearrange, persist and restore, with no appearance of its own.
//
// The tree is the source of truth. Each region (the center plus an
// optional left, right and bottom dock) is one PaneTree: pure data, with
// containers addressed by NodeID and panels by PanelID, both stable across
// every edit and normalization rule. Every mutation goes through the tree
// (InsertPanel, RemovePanel, MovePanel, Split, SetActive, SetSizes), and
// each normalizes before returning and reports an EditResult.
//
// DockArea owns the trees and a cache of container entities keyed by
// NodeID (a TabGroup per Tabs node, a ResizableState per split), plus the
// panel handles keyed by PanelID. After any edit that reports a change it
// reconciles: it creates entities for new ids, drops the ones that are
// gone (telling departed panels OnRemoved), pushes sizes and active
// indices into the survivors, and emits DockEventLayoutChanged. A layout
// is described entity-free with DockLayout and installed with SetCenter /
// SetDock; Dump and Load round-trip a whole area through DockAreaState,
// rebuilding panels through the PanelRegistry.
//
// Base supplies behavior; the host supplies appearance through
// DockAreaRenderer and TabGroupRenderer, whose optional hooks each fall
// back to base's own minimum. An area built without a renderer still
// docks, drags, resizes and persists; it draws nothing but the panels.
//
// Files: dock_layout_*.go (layout/*), dock_active.go, dock_state.go,
// dock_state_convert.go, dock_placement.go, dock_drag.go, dock_panel.go,
// dock_registry.go, dock_tab_group.go and dock_area.go; the shared test
// doubles of U:dock/test_support.rs are in dock_test_support_test.go.
