package base

import (
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:tree.rs (6/6).

// newTreeState is `cx.new(|cx| TreeState::new(cx).items(items))`.
func newTreeState(t *testing.T, items []TreeItem) (*gpui.TestApp, gpui.Entity[TreeState]) {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	var state gpui.Entity[TreeState]
	ta.Update(func(app *gpui.App) {
		state = gpui.NewEntity(app, ta.RootScope(), func(s *TreeState, _ *gpui.Context[TreeState]) {
			*s = NewTreeState(app).Items(items)
		})
	})
	return ta, state
}

func updateTree(ta *gpui.TestApp, state gpui.Entity[TreeState], f func(*TreeState, *gpui.Context[TreeState])) {
	ta.Update(func(app *gpui.App) { state.Update(app, f) })
}

// U:tree.rs:591-609
func TestTreeClonesShareStateAndAncestorsKeepNearestFirstOrder(t *testing.T) {
	leaf := NewTreeItem("leaf", "Leaf")
	branch := NewTreeItem("branch", "Branch").Child(leaf)
	root := NewTreeItem("root", "Root").Child(branch)

	leaf.Disabled(true).Expanded(true)
	if !leaf.IsDisabled() || !leaf.IsExpanded() {
		t.Fatal("a clone's state change did not reach the original")
	}

	ancestors, ok := root.Ancestors("leaf")
	if !ok {
		t.Fatal("leaf not found")
	}
	var ids []string
	for _, a := range ancestors {
		ids = append(ids, a.ID)
	}
	if !slices.Equal(ids, []string{"branch", "root"}) {
		t.Fatalf("ancestors = %v", ids)
	}
}

// U:tree.rs:611-629
func TestTreeStateFlattensExpandedItemsAndResetsSelection(t *testing.T) {
	ta, state := newTreeState(t, []TreeItem{
		NewTreeItem("src", "src").Expanded(true).Child(NewTreeItem("src/lib.rs", "lib.rs")),
		NewTreeItem("README.md", "README.md"),
	})
	updateTree(ta, state, func(s *TreeState, cx *gpui.Context[TreeState]) {
		if len(s.entries) != 3 {
			t.Fatalf("entries = %d, want 3", len(s.entries))
		}
		if d := s.entries[1].Depth(); d != 1 {
			t.Fatalf("depth = %d, want 1", d)
		}
		s.SetSelectedIndex(1, true, cx)
		s.SetItems([]TreeItem{NewTreeItem("Cargo.toml", "Cargo.toml")}, cx)
		if _, ok := s.SelectedIndex(); ok {
			t.Fatal("set_items kept the selection")
		}
		if len(s.entries) != 1 {
			t.Fatalf("entries = %d, want 1", len(s.entries))
		}
	})
}

// U:tree.rs:631-646
func TestTreeSelectingHiddenItemExpandsItsAncestors(t *testing.T) {
	target := NewTreeItem("src/ui/tree.rs", "tree.rs")
	root := NewTreeItem("src", "src").Child(NewTreeItem("src/ui", "ui").Child(target))
	ta, state := newTreeState(t, []TreeItem{root})
	updateTree(ta, state, func(s *TreeState, cx *gpui.Context[TreeState]) {
		s.SetSelectedItem(&target, cx)
		if len(s.entries) != 3 {
			t.Fatalf("entries = %d, want 3", len(s.entries))
		}
		if item := s.SelectedItem(); item == nil || item.ID != "src/ui/tree.rs" {
			t.Fatalf("selected = %v", item)
		}
	})
}

type treeIDDepth struct {
	id    string
	depth int
}

// U:tree.rs:648-679
func TestTreeRevealingItemUnderLaterRootKeepsOtherSubtrees(t *testing.T) {
	guide := NewTreeItem("docs/guide", "guide").Child(NewTreeItem("docs/guide/a", "a"))
	docs := NewTreeItem("docs", "docs").Expanded(true).Child(guide)
	ui := NewTreeItem("src/ui", "ui").Child(NewTreeItem("src/ui/tree.rs", "tree.rs"))
	src := NewTreeItem("src", "src").Child(ui)
	ta, state := newTreeState(t, []TreeItem{docs, src})

	updateTree(ta, state, func(s *TreeState, cx *gpui.Context[TreeState]) {
		if len(s.entries) != 3 {
			t.Fatalf("entries = %d, want 3", len(s.entries))
		}
		s.RevealItem("src/ui/tree.rs", gpui.ScrollTop, cx)
		var got []treeIDDepth
		for i := range s.entries {
			got = append(got, treeIDDepth{s.entries[i].Item().ID, s.entries[i].Depth()})
		}
		want := []treeIDDepth{{"docs", 0}, {"docs/guide", 1}, {"src", 0}, {"src/ui", 1}, {"src/ui/tree.rs", 2}}
		if !slices.Equal(got, want) {
			t.Fatalf("entries = %v, want %v", got, want)
		}
		if !s.entries[0].IsFolder() || len(s.entries[0].Item().Children) != 1 {
			t.Fatal("the docs root lost its subtree")
		}

		s.toggleExpand(1, cx)
		if ix := s.IndexOf("docs/guide/a"); ix != 2 {
			t.Fatalf("index_of(docs/guide/a) = %d, want 2", ix)
		}
	})
}

// U:tree.rs:681-692
func TestTreeTogglingFolderRebuildsVisibleEntries(t *testing.T) {
	root := NewTreeItem("src", "src").Child(NewTreeItem("src/lib.rs", "lib.rs"))
	ta, state := newTreeState(t, []TreeItem{root})
	updateTree(ta, state, func(s *TreeState, cx *gpui.Context[TreeState]) {
		s.toggleExpand(0, cx)
		if len(s.entries) != 2 {
			t.Fatalf("entries = %d, want 2", len(s.entries))
		}
		s.toggleExpand(0, cx)
		if len(s.entries) != 1 {
			t.Fatalf("entries = %d, want 1", len(s.entries))
		}
	})
}

// treeEventCollector is U:tree.rs:566-589 EventCollector.
type treeEventCollector struct {
	events []TreeEvent
}

// U:tree.rs:694-714
func TestTreeExpansionEventsPreserveIDsAndSetItemsStaysSilent(t *testing.T) {
	root := NewTreeItem("src", "src").Child(NewTreeItem("src/lib.rs", "lib.rs"))
	ta, state := newTreeState(t, []TreeItem{root})
	var collector gpui.Entity[treeEventCollector]
	ta.Update(func(app *gpui.App) {
		collector = gpui.NewEntity(app, ta.RootScope(), func(_ *treeEventCollector, cx *gpui.Context[treeEventCollector]) {
			TreeEvents.Subscribe(cx, state, func(c *treeEventCollector, _ gpui.Entity[TreeState], e *TreeEvent, _ *gpui.Context[treeEventCollector]) {
				c.events = append(c.events, *e)
			})
		})
	})

	updateTree(ta, state, func(s *TreeState, cx *gpui.Context[TreeState]) {
		s.toggleExpand(0, cx)
		s.toggleExpand(0, cx)
		s.SetItems([]TreeItem{NewTreeItem("README.md", "README.md")}, cx)
	})
	ta.RunUntilParked()

	var events []TreeEvent
	ta.Update(func(app *gpui.App) {
		events = collector.Read(app, func(c *treeEventCollector, _ *gpui.App) []TreeEvent { return slices.Clone(c.events) })
	})
	want := []TreeEvent{{TreeEventExpanded, "src"}, {TreeEventCollapsed, "src"}}
	if !slices.Equal(events, want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
}

// Go-own: the rendered tree exposes Tree/TreeItem semantics, clicking a
// folder selects and expands it, and the arrow keys walk the entries.
func TestTreeRendersItemsAndHandlesClicksAndKeys(t *testing.T) {
	ta := gpui.NewTestApp()
	var state gpui.Entity[TreeState]
	_, cx := openTestView(t, ta, func(w *gpui.Window, vcx *gpui.Context[testView]) gpui.AnyElement {
		if state == (gpui.Entity[TreeState]{}) {
			state = gpui.NewEntity(vcx, w.Scope(), func(s *TreeState, _ *gpui.Context[TreeState]) {
				*s = NewTreeState(vcx.App()).Items([]TreeItem{
					NewTreeItem("src", "src").Child(NewTreeItem("src/lib.rs", "lib.rs")),
					NewTreeItem("README.md", "README.md"),
				})
			})
		}
		return gpui.OwnRecipe(NewTree(state).W(gpui.Px(200)).H(gpui.Px(200)).Item(
			func(ix int, entry *TreeEntry, _ TreeEntryState, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
				return gpui.Div().Flex().H(gpui.Px(20)).Child(entry.Item().Label).IntoElement()
			}))
	})
	ta.Update(func(app *gpui.App) { Init(app) })
	cx.RunUntilParked()
	if _, err := gpui.DrawWindowFrame(cx.Window()); err != nil {
		t.Fatal(err)
	}

	cx.SimulateClick(gpui.Point{X: 10, Y: 10}, gpui.NoModifiers())
	cx.RunUntilParked()
	readState := func() (int, int) {
		var sel, count int
		ta.Update(func(app *gpui.App) {
			state.Read(app, func(s *TreeState, _ *gpui.App) struct{} {
				sel, count = s.selectedIx, len(s.entries)
				return struct{}{}
			})
		})
		return sel, count
	}
	if sel, count := readState(); sel != 0 || count != 3 {
		t.Fatalf("after click: selected=%d entries=%d, want 0 and 3", sel, count)
	}

	ta.Update(func(app *gpui.App) {
		state.Read(app, func(s *TreeState, _ *gpui.App) struct{} { s.focusHandle.Focus(cx.Window()); return struct{}{} })
	})
	cx.RunUntilParked()
	ta.SimulateKeystrokes(cx.Window(), "down down")
	cx.RunUntilParked()
	if sel, _ := readState(); sel != 2 {
		t.Fatalf("after down down: selected=%d, want 2", sel)
	}
	ta.SimulateKeystrokes(cx.Window(), "up up left")
	cx.RunUntilParked()
	if sel, count := readState(); sel != 0 || count != 2 {
		t.Fatalf("after up up left: selected=%d entries=%d, want 0 and 2", sel, count)
	}
}
