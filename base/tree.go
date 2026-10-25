package base

// Port of U:tree.rs: the unstyled, virtualized tree.

import (
	"fmt"
	"slices"

	"github.com/hoangvu12/gpui-go/gpui"
)

// treeContext is the Tree key context (U:tree.rs:17).
const treeContext = "Tree"

// treeInit binds the tree keys (U:tree.rs:19-27).
func treeInit(app *gpui.App) {
	ctx := treeContext
	app.BindKeys(
		gpui.NewKeyBinding("up", SelectUpAction.Box(SelectUp{}), &ctx),
		gpui.NewKeyBinding("down", SelectDownAction.Box(SelectDown{}), &ctx),
		gpui.NewKeyBinding("left", SelectLeftAction.Box(SelectLeft{}), &ctx),
		gpui.NewKeyBinding("right", SelectRightAction.Box(SelectRight{}), &ctx),
	)
}

// TreeKeyContext returns the tree's key context (U:tree.rs:29-32,
// #[doc(hidden)] key_context).
func TreeKeyContext() string { return treeContext }

// treeItemState is the state every clone of a TreeItem shares
// (U:tree.rs:34-37).
type treeItemState struct {
	expanded bool
	disabled bool
}

// TreeItem is a tree item with a stable id, display label, children, and
// shared state (U:tree.rs:39-46). Copies share the expanded and disabled
// state, like upstream's Rc<RefCell<TreeItemState>> clones.
type TreeItem struct {
	ID       string
	Label    string
	Children []TreeItem
	state    *treeItemState
}

// NewTreeItem is TreeItem::new (U:tree.rs:99-109).
func NewTreeItem(id, label string) TreeItem {
	return TreeItem{ID: id, Label: label, state: &treeItemState{}}
}

// Child appends a child (U:tree.rs:111-114). The children slice is
// clipped first, so a copy never shares its spare capacity with the
// receiver (upstream moves an owned Vec).
func (t TreeItem) Child(child TreeItem) TreeItem {
	t.Children = append(slices.Clip(t.Children), child)
	return t
}

// WithChildren appends children (U:tree.rs:116-119, children).
func (t TreeItem) WithChildren(children ...TreeItem) TreeItem {
	t.Children = append(slices.Clip(t.Children), children...)
	return t
}

// Expanded sets the shared expanded state (U:tree.rs:121-124).
func (t TreeItem) Expanded(expanded bool) TreeItem {
	t.state.expanded = expanded
	return t
}

// Disabled sets the shared disabled state (U:tree.rs:126-129).
func (t TreeItem) Disabled(disabled bool) TreeItem {
	t.state.disabled = disabled
	return t
}

// IsFolder reports whether the item has children (U:tree.rs:131-134).
func (t TreeItem) IsFolder() bool { return len(t.Children) > 0 }

// IsDisabled reports the shared disabled state (U:tree.rs:136-138).
func (t TreeItem) IsDisabled() bool { return t.state.disabled }

// IsExpanded reports the shared expanded state (U:tree.rs:140-143).
func (t TreeItem) IsExpanded() bool { return t.state.expanded }

// Ancestors returns the target's ancestors from nearest parent to root,
// and false when the target is not in this subtree (U:tree.rs:145-149).
func (t TreeItem) Ancestors(targetID string) ([]TreeItem, bool) {
	refs, ok := t.ancestorRefs(targetID)
	if !ok {
		return nil, false
	}
	out := make([]TreeItem, len(refs))
	for i, r := range refs {
		out[i] = *r
	}
	return out, true
}

// ancestorRefs is Ancestors by reference (U:tree.rs:151-166).
func (t *TreeItem) ancestorRefs(targetID string) ([]*TreeItem, bool) {
	if t.ID == targetID {
		return []*TreeItem{}, true
	}
	for i := range t.Children {
		if path, ok := t.Children[i].ancestorRefs(targetID); ok {
			return append(path, t), true
		}
	}
	return nil, false
}

// TreeEntry is a flat representation of a tree item with its depth
// (U:tree.rs:48-89).
type TreeEntry struct {
	item  TreeItem
	depth int
}

// NewTreeEntry is TreeEntry::new (U:tree.rs:56-58).
func NewTreeEntry(item TreeItem, depth int) TreeEntry {
	return TreeEntry{item: item, depth: depth}
}

// Item returns the entry's item (U:tree.rs:60-63).
func (e *TreeEntry) Item() *TreeItem { return &e.item }

// Depth returns the entry's depth (U:tree.rs:65-68).
func (e *TreeEntry) Depth() int { return e.depth }

// IsRoot reports whether the entry is a root (U:tree.rs:70-73).
func (e *TreeEntry) IsRoot() bool { return e.depth == 0 }

// IsFolder reports whether the entry's item has children
// (U:tree.rs:75-78).
func (e *TreeEntry) IsFolder() bool { return e.item.IsFolder() }

// IsExpanded reports the item's expanded state (U:tree.rs:80-83).
func (e *TreeEntry) IsExpanded() bool { return e.item.IsExpanded() }

// IsDisabled reports the item's disabled state (U:tree.rs:85-88).
func (e *TreeEntry) IsDisabled() bool { return e.item.IsDisabled() }

// TreeEventKind discriminates TreeEvent (U:tree.rs:91-96).
type TreeEventKind uint8

const (
	// TreeEventExpanded is TreeEvent::Expanded.
	TreeEventExpanded TreeEventKind = iota
	// TreeEventCollapsed is TreeEvent::Collapsed.
	TreeEventCollapsed
)

// TreeEvent is emitted by a tree when user-visible expansion state
// changes (U:tree.rs:91-96). The data-carrying enum is a kind plus the
// item id.
type TreeEvent struct {
	Kind TreeEventKind
	ID   string
}

// TreeEvents is the TreeState event descriptor (U:tree.rs:201).
var TreeEvents = gpui.DefineEvent[TreeState, TreeEvent]()

// TreeEntryState is the interaction state supplied while rendering a
// visible tree entry (U:tree.rs:169-186).
type TreeEntryState struct {
	selected     bool
	rightClicked bool
}

// IsSelected reports whether the entry is selected (U:tree.rs:177-180).
func (s TreeEntryState) IsSelected() bool { return s.selected }

// IsRightClicked reports whether the entry was right-clicked last
// (U:tree.rs:182-185).
func (s TreeEntryState) IsRightClicked() bool { return s.rightClicked }

// TreeRenderItem renders the application-owned content of one visible
// entry (U:tree.rs:188).
type TreeRenderItem func(ix int, entry *TreeEntry, state TreeEntryState, w *gpui.Window, app *gpui.App) gpui.AnyElement

// defaultTreeRenderItem is the empty div the state and element start
// with (U:tree.rs:211, 511).
func defaultTreeRenderItem(int, *TreeEntry, TreeEntryState, *gpui.Window, *gpui.App) gpui.AnyElement {
	return gpui.Div().Flex().IntoElement()
}

// TreeState is the behavior and interaction state for a virtualized
// tree (U:tree.rs:190-199). Selection indices are -1 for None.
type TreeState struct {
	focusHandle    gpui.FocusHandle
	entries        []TreeEntry
	scrollHandle   *gpui.UniformListScrollHandle
	selectedIx     int
	rightClickedIx int
	renderItem     TreeRenderItem
	listStyle      gpui.StyleRefinement
}

// NewTreeState is TreeState::new (U:tree.rs:204-214).
func NewTreeState(app *gpui.App) TreeState {
	return TreeState{
		focusHandle:    app.FocusHandle(),
		scrollHandle:   gpui.NewUniformListScrollHandle(),
		selectedIx:     -1,
		rightClickedIx: -1,
		renderItem:     defaultTreeRenderItem,
	}
}

// Items replaces the items, builder-style (U:tree.rs:216-219).
func (s TreeState) Items(items []TreeItem) TreeState {
	s.replaceItems(items)
	return s
}

// SetItems replaces the items and clears the selection; it emits no
// event (U:tree.rs:221-226).
func (s *TreeState) SetItems(items []TreeItem, cx *gpui.Context[TreeState]) {
	s.replaceItems(items)
	s.selectedIx = -1
	s.rightClickedIx = -1
	cx.Notify()
}

// SelectedIndex returns the selected entry index (U:tree.rs:228-230).
func (s *TreeState) SelectedIndex() (int, bool) { return s.selectedIx, s.selectedIx >= 0 }

// SetSelectedIndex sets the selected entry index; a negative index
// clears it (U:tree.rs:232-235).
func (s *TreeState) SetSelectedIndex(ix int, ok bool, cx *gpui.Context[TreeState]) {
	if !ok || ix < 0 {
		ix = -1
	}
	s.selectedIx = ix
	cx.Notify()
}

// SetSelectedItem selects an item, expanding its ancestors when it is
// hidden; nil clears the selection (U:tree.rs:237-248).
func (s *TreeState) SetSelectedItem(item *TreeItem, cx *gpui.Context[TreeState]) {
	if item != nil {
		s.selectedIx = s.IndexOf(item.ID)
		if s.selectedIx < 0 {
			s.expandAncestors(item.ID, cx)
			s.selectedIx = s.IndexOf(item.ID)
		}
	} else {
		s.selectedIx = -1
	}
	cx.Notify()
}

// SelectedItem returns the selected item (U:tree.rs:250-253).
func (s *TreeState) SelectedItem() *TreeItem {
	if e := s.SelectedEntry(); e != nil {
		return e.Item()
	}
	return nil
}

// SelectedEntry returns the selected entry (U:tree.rs:255-257).
func (s *TreeState) SelectedEntry() *TreeEntry { return s.Entry(s.selectedIx) }

// Entry returns the entry at ix, or nil (U:tree.rs:259-261).
func (s *TreeState) Entry(ix int) *TreeEntry {
	if ix < 0 || ix >= len(s.entries) {
		return nil
	}
	return &s.entries[ix]
}

// EntryCount returns the number of visible entries (Go accessor for the
// upstream module-private entries.len()).
func (s *TreeState) EntryCount() int { return len(s.entries) }

// ScrollHandle returns the list's scroll handle (U:tree.rs:263-265).
func (s *TreeState) ScrollHandle() *gpui.UniformListScrollHandle { return s.scrollHandle }

// ScrollToItem scrolls the list to an entry (U:tree.rs:267-269).
func (s *TreeState) ScrollToItem(ix int, strategy gpui.ScrollStrategy) {
	s.scrollHandle.ScrollToItem(ix, strategy)
}

// IndexOf returns the visible index of an item id, or -1
// (U:tree.rs:271-273).
func (s *TreeState) IndexOf(id string) int {
	for i := range s.entries {
		if s.entries[i].item.ID == id {
			return i
		}
	}
	return -1
}

// RevealItem expands an item's ancestors and scrolls it into view
// (U:tree.rs:275-285).
func (s *TreeState) RevealItem(id string, strategy gpui.ScrollStrategy, cx *gpui.Context[TreeState]) {
	s.expandAncestors(id, cx)
	if ix := s.IndexOf(id); ix >= 0 {
		s.ScrollToItem(ix, strategy)
	}
}

// Focus focuses the tree (U:tree.rs:287-289).
func (s *TreeState) Focus(w *gpui.Window) { s.focusHandle.Focus(w) }

// FocusHandle returns the tree's focus handle (Go accessor for the
// handle upstream's Tree reads directly, U:tree.rs:539).
func (s *TreeState) FocusHandle() gpui.FocusHandle { return s.focusHandle }

// replaceItems flattens the roots (U:tree.rs:291-296).
func (s *TreeState) replaceItems(items []TreeItem) {
	s.entries = nil
	for _, item := range items {
		s.pushRoot(item)
	}
}

// expandAncestors expands every collapsed ancestor of the target, root
// first, emitting Expanded for each (U:tree.rs:298-319).
func (s *TreeState) expandAncestors(targetID string, cx *gpui.Context[TreeState]) {
	// Entries are in depth-first order, so the root containing the target
	// precedes every descendant entry whose subtree could also contain it.
	var ancestors []*TreeItem
	for i := range s.entries {
		if !s.entries[i].IsRoot() {
			continue
		}
		if path, ok := s.entries[i].item.ancestorRefs(targetID); ok {
			ancestors = path
			break
		}
	}
	if len(ancestors) == 0 {
		return
	}
	for i := len(ancestors) - 1; i >= 0; i-- {
		ancestor := ancestors[i]
		if !ancestor.IsExpanded() {
			ancestor.state.expanded = true
			TreeEvents.Emit(cx, TreeEvent{Kind: TreeEventExpanded, ID: ancestor.ID})
		}
	}
	s.rebuildEntries()
}

// pushRoot appends a root and its visible descendants (U:tree.rs:321-338).
func (s *TreeState) pushRoot(item TreeItem) {
	s.entries = append(s.entries, NewTreeEntry(item, 0))
	if item.IsExpanded() {
		for _, child := range item.Children {
			s.addEntry(child, 1)
		}
	}
}

// addEntry appends an entry and, when expanded, its descendants
// (U:tree.rs:340-347).
func (s *TreeState) addEntry(item TreeItem, depth int) {
	s.entries = append(s.entries, NewTreeEntry(item, depth))
	if item.IsExpanded() {
		for _, child := range item.Children {
			s.addEntry(child, depth+1)
		}
	}
}

// toggleExpand flips a folder's expansion and emits the matching event
// (U:tree.rs:349-367).
func (s *TreeState) toggleExpand(ix int, cx *gpui.Context[TreeState]) {
	entry := s.Entry(ix)
	if entry == nil || !entry.IsFolder() {
		return
	}
	expanded := !entry.IsExpanded()
	id := entry.item.ID
	entry.item.state.expanded = expanded
	kind := TreeEventCollapsed
	if expanded {
		kind = TreeEventExpanded
	}
	TreeEvents.Emit(cx, TreeEvent{Kind: kind, ID: id})
	s.rightClickedIx = -1
	s.rebuildEntries()
}

// rebuildEntries reflattens the roots (U:tree.rs:369-376).
func (s *TreeState) rebuildEntries() {
	var roots []TreeItem
	for i := range s.entries {
		if s.entries[i].IsRoot() {
			roots = append(roots, s.entries[i].item)
		}
	}
	s.replaceItems(roots)
}

// onActionConfirm toggles a selected folder (U:tree.rs:378-391).
func (s *TreeState) onActionConfirm(_ *Confirm, _ *gpui.Window, cx *gpui.Context[TreeState]) {
	if e := s.Entry(s.selectedIx); e != nil && e.IsFolder() {
		s.toggleExpand(s.selectedIx, cx)
		cx.Notify()
	}
}

// onActionLeft collapses a selected expanded folder (U:tree.rs:393-403).
func (s *TreeState) onActionLeft(_ *SelectLeft, _ *gpui.Window, cx *gpui.Context[TreeState]) {
	if e := s.Entry(s.selectedIx); e != nil && e.IsFolder() && e.IsExpanded() {
		s.toggleExpand(s.selectedIx, cx)
		cx.Notify()
	}
}

// onActionRight expands a selected collapsed folder (U:tree.rs:405-415).
func (s *TreeState) onActionRight(_ *SelectRight, _ *gpui.Window, cx *gpui.Context[TreeState]) {
	if e := s.Entry(s.selectedIx); e != nil && e.IsFolder() && !e.IsExpanded() {
		s.toggleExpand(s.selectedIx, cx)
		cx.Notify()
	}
}

// onActionUp selects the previous entry, wrapping to the last
// (U:tree.rs:417-426).
func (s *TreeState) onActionUp(_ *SelectUp, _ *gpui.Window, cx *gpui.Context[TreeState]) {
	ix := max(s.selectedIx, 0)
	if ix > 0 {
		ix--
	} else {
		ix = max(len(s.entries)-1, 0)
	}
	s.selectedIx = ix
	s.scrollHandle.ScrollToItem(ix, gpui.ScrollTop)
	cx.Notify()
}

// onActionDown selects the next entry, wrapping to the first
// (U:tree.rs:428-439).
func (s *TreeState) onActionDown(_ *SelectDown, _ *gpui.Window, cx *gpui.Context[TreeState]) {
	ix := max(s.selectedIx, 0)
	if ix+1 < len(s.entries) {
		ix++
	} else {
		ix = 0
	}
	s.selectedIx = ix
	s.scrollHandle.ScrollToItem(ix, gpui.ScrollBottom)
	cx.Notify()
}

// onEntryClick selects an entry and toggles it (U:tree.rs:441-445).
func (s *TreeState) onEntryClick(ix int, cx *gpui.Context[TreeState]) {
	s.selectedIx = ix
	s.toggleExpand(ix, cx)
	cx.Notify()
}

// treeVisibleEntry is one visible entry snapshotted for the list
// callback.
type treeVisibleEntry struct {
	entry TreeEntry
	state TreeEntryState
}

// Render renders the virtual list of visible entries (U:tree.rs:448-492).
//
// Upstream's cx.processor runs the item callback inside an update of the
// tree entity. The Go list callback runs at layout, after Render
// returns, so it reads a snapshot of the visible range through the
// view's self-handle and calls the application's render_item outside
// that read; an item renderer may therefore read the tree state.
//
// Deviation: the core UniformListElement exposes no style refinement
// (only W/H/SizeFull), so list_style applies its size fields only.
func (s *TreeState) Render(_ *gpui.Window, cx *gpui.Context[TreeState]) gpui.AnyElement {
	self := cx.StrongSelf()
	renderItem := s.renderItem
	list := gpui.UniformList(s.scrollHandle, len(s.entries), func(start, end int, w *gpui.Window, app *gpui.App) []gpui.AnyElement {
		visible := self.Read(app, func(state *TreeState, _ *gpui.App) []treeVisibleEntry {
			end := min(end, len(state.entries))
			out := make([]treeVisibleEntry, 0, max(end-start, 0))
			for ix := start; ix < end; ix++ {
				out = append(out, treeVisibleEntry{
					entry: state.entries[ix],
					state: TreeEntryState{
						selected:     ix == state.selectedIx,
						rightClicked: ix == state.rightClickedIx,
					},
				})
			}
			return out
		})
		items := make([]gpui.AnyElement, 0, len(visible))
		for i := range visible {
			ix := start + i
			entry := &visible[i].entry
			entryState := visible[i].state
			d := TestSupport(gpui.Div().Flex().ID(gpui.IntegerElementID(uint64(ix)))).
				Role(gpui.RoleTreeItem).
				AriaLabel(entry.item.Label).
				AriaSelected(entryState.selected)
			if entry.IsFolder() {
				d.AriaExpanded(entry.IsExpanded())
			}
			d.Child(renderItem(ix, entry, entryState, w, app))
			if !entry.IsDisabled() {
				d.OnMouseDown(gpui.MouseButtonLeft, w.ListenerFor(self, func(state *TreeState, _ *gpui.MouseDownEvent, _ *gpui.Window, cx *gpui.Context[TreeState]) {
					state.onEntryClick(ix, cx)
				})).OnMouseDown(gpui.MouseButtonRight, w.ListenerFor(self, func(state *TreeState, _ *gpui.MouseDownEvent, _ *gpui.Window, cx *gpui.Context[TreeState]) {
					state.rightClickedIx = ix
					cx.Notify()
				}))
			}
			items = append(items, d.IntoElement())
		}
		return items
	})
	if v, ok := s.listStyle.SizeWidth.Get(); ok {
		list.W(v)
	}
	if v, ok := s.listStyle.SizeHeight.Get(); ok {
		list.H(v)
	}
	return list.IntoElement()
}

// Tree is an unstyled, virtualized tree element (U:tree.rs:494-502).
type Tree struct {
	gpui.Styled[*Tree]

	id         gpui.ElementID
	state      gpui.Entity[TreeState]
	listStyle  gpui.StyleRefinement
	renderItem TreeRenderItem
}

// NewTree is Tree::new (U:tree.rs:505-513).
func NewTree(state gpui.Entity[TreeState]) *Tree {
	return gpui.BindStyled(&Tree{
		id:         gpui.NameElementID(fmt.Sprintf("tree-%d", uint64(state.EntityID()))),
		state:      state,
		renderItem: defaultTreeRenderItem,
	})
}

// Item supplies the application-owned content for each visible entry
// (U:tree.rs:515-522).
func (t *Tree) Item(renderItem TreeRenderItem) *Tree {
	t.renderItem = renderItem
	return t
}

// ListStyle applies caller-owned presentation to the internal virtual
// list (U:tree.rs:524-528).
func (t *Tree) ListStyle(style gpui.StyleRefinement) *Tree {
	t.listStyle = style
	return t
}

// RenderOnce implements gpui.RenderOnce (U:tree.rs:537-559).
func (t *Tree) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	state := t.state
	focusHandle := state.Read(app, func(s *TreeState, _ *gpui.App) gpui.FocusHandle { return s.focusHandle })
	renderItem, listStyle := t.renderItem, t.listStyle
	state.Update(app, func(s *TreeState, _ *gpui.Context[TreeState]) {
		s.renderItem = renderItem
		s.listStyle = listStyle
	})

	// Gap 2b: the plain upstream div() is a block.
	d := TestSupport(gpui.Div().ID(t.id).Block()).
		Role(gpui.RoleTree).
		KeyContext(treeContext).
		TrackFocus(focusHandle)
	d.OnAction(ConfirmAction, w.ListenerFor(state, (*TreeState).onActionConfirm))
	d.OnAction(SelectLeftAction, w.ListenerFor(state, (*TreeState).onActionLeft))
	d.OnAction(SelectRightAction, w.ListenerFor(state, (*TreeState).onActionRight))
	d.OnAction(SelectUpAction, w.ListenerFor(state, (*TreeState).onActionUp))
	d.OnAction(SelectDownAction, w.ListenerFor(state, (*TreeState).onActionDown))
	d.Child(gpui.ViewOf[TreeState](state))
	style := t.Style()
	return StyledExtOf(d).RefineStyle(&style).IntoElement()
}
