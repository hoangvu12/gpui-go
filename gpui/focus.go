package gpui

// This file ports the focus runtime of the pinned CE reference
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/window.rs — FocusId/FocusHandle/FocusRef (the
//     per-app focus slotmap with reference counting, tab_index and
//     tab_stop), the focused handle of a window (focus, focus_enabled,
//     focus_generation), focus/blur/disable_focus/focus_next/
//     focus_prev, focus_contains through the rendered dispatch tree,
//     the focus-lost notification when the focused handle is released,
//     and the weak handle whose upgrade fails at logical zero;
//   - crates/gpui/src/tab_stop.rs — TabStopMap (path-ordered tab stops
//     with insertion history, group begin/end, next/prev with wrap and
//     non-tab-stop skipping).
//
// Go adaptations (recorded where they alter behavior):
//
//   - Reference counting is replaced by logical release: Go ownership
//     is garbage collected, so FocusHandle.Release performs the
//     idempotent logical release the runtime ownership contract
//     specifies ("release is idempotent", "weak upgrade fails
//     irrevocably at logical zero"). A released handle never refocuses,
//     never contains, and the window's Focused reports none.
//
//   - The reference keeps one app-global focus slotmap; the port keeps
//     the per-window handle registry the window slice landed (handles
//     are window-scoped identities), with the same observable release
//     semantics.

import (
	"fmt"
	"sort"
)

// ---------------------------------------------------------------------------
// Per-window focus runtime
// ---------------------------------------------------------------------------

// focusRef is the window's record for one allocated focus identity
// (window.rs FocusRef without the reference count: see the file header
// for the release adaptation).
type focusRef struct {
	// tabIndex is the tab order index of the handle's element.
	tabIndex int64
	// tabStop reports whether the handle's element is a tab stop.
	tabStop bool
	// released is the logical-release flag (idempotent).
	released bool
}

// windowFocusState is a window's focus runtime: handle allocation,
// focus identity, focus generation and the pending-input state the
// keyboard dispatch uses. All fields are foreground-thread only.
type windowFocusState struct {
	handles map[uint64]*focusRef
	seq     uint64

	// focused is the focused handle identity, or 0 (none).
	focused uint64
	// focusEnabled gates focus changes (window.rs focus_enabled; a
	// disabled window refuses focus and blur).
	focusEnabled bool
	// focusGeneration counts focus changes (window.rs
	// focus_generation, wrapping).
	focusGeneration uint64

	// pendingInput is the chord state (window.rs PendingInput), or nil.
	pendingInput *pendingInput
	// pendingModifier tracks the lone-modifier keystroke synthesis
	// (window.rs pending_modifier).
	pendingModifier modifierTracking

	// rendered is the last completed frame's dispatch tree and tab
	// stops (window.rs rendered_frame.dispatch_tree/tab_stops).
	rendered *renderedFrame
	// next is the frame under construction, or nil outside a draw.
	next *renderedFrame

	// inputHandler is the active input handler of the last completed
	// frame (the platform input handler seam), or nil.
	inputHandler InputHandler
	// frameInputHandlers are the focused registrations of the frame
	// under construction (window.rs next_frame.input_handlers).
	frameInputHandlers []InputHandler

	// focusLostListeners are notified when the focused handle is
	// released (window.rs focus_lost_listeners).
	focusLostListeners []*focusLostSubscription
	// focusLostPath is the previous focus path during focus-lost
	// dispatch (window.rs focus_lost_path).
	focusLostPath []uint64

	// pendingInputObservers observe pending-input state changes
	// (window.rs pending_input_observers).
	pendingInputObservers []*pendingInputSubscription
}

// renderedFrame is one completed frame's interaction state: the
// dispatch tree and the tab-stop map (window.rs RenderedFrame's
// dispatch members, bounded to this slice), plus ticket24's mouse
// interaction members — the frame-scoped mouse listeners (window.rs
// Frame::mouse_listeners) and hitboxes (Frame::hitboxes).
type renderedFrame struct {
	dispatchTree   *DispatchTree
	tabStops       *TabStopMap
	mouseListeners []mouseListener
	hitboxes       []*hitboxRecord
}

// focusState returns (creating when absent) the window's focus
// runtime. Foreground thread only.
func focusState(w *Window) *windowFocusState {
	if w == nil {
		panic("gpui: focus runtime requires a live window")
	}
	if w.focus == nil {
		w.focus = &windowFocusState{
			handles:      make(map[uint64]*focusRef),
			focusEnabled: true,
		}
	}
	return w.focus
}

// focusRefOf returns the window's focus record for a handle identity,
// or nil when absent or released.
func (w *Window) focusRefOf(id uint64) *focusRef {
	fs := focusState(w)
	ref, ok := fs.handles[id]
	if !ok || ref.released {
		return nil
	}
	return ref
}

// ---------------------------------------------------------------------------
// FocusHandle (window.rs FocusHandle)
// ---------------------------------------------------------------------------

// FocusHandle tracks and manipulates the focused element in a window.
// Handles are value identities into their window's focus registry (the
// reference holds the shared focus map on the handle; the port holds the
// owning window pointer, which stays valid as a logical identity after
// close); Release performs the logical release.
type FocusHandle struct {
	id       uint64
	window   *Window
	tabIndex int64
	tabStop  bool
}

// ID returns the focus identity within its window.
func (h FocusHandle) ID() uint64 { return h.id }

// WindowID returns the owning window identity.
func (h FocusHandle) WindowID() WindowID { return h.windowID() }

func (h FocusHandle) windowID() WindowID {
	if h.window == nil {
		return 0
	}
	return h.window.id
}

// TabIndex sets the tab order index of this handle's element (window.rs
// FocusHandle::tab_index): positive indices follow 0 in ascending
// order; negative indices precede it.
func (h FocusHandle) TabIndex(index int64) FocusHandle {
	if ref := h.windowRef(); ref != nil {
		ref.tabIndex = index
	}
	out := h
	out.tabIndex = index
	return out
}

// TabStop sets whether this handle's element is a tab stop (window.rs
// FocusHandle::tab_stop): non-tab-stop handles are skipped by
// focus_next/focus_prev.
func (h FocusHandle) TabStop(tabStop bool) FocusHandle {
	if ref := h.windowRef(); ref != nil {
		ref.tabStop = tabStop
	}
	out := h
	out.tabStop = tabStop
	return out
}

// windowRef returns the window's live focus record for this handle, or
// nil when absent or released.
func (h FocusHandle) windowRef() *focusRef {
	if h.window == nil {
		return nil
	}
	return h.window.focusRefOf(h.id)
}

// Release performs the logical release of this handle (the ownership
// contract's idempotent release). A released handle stops matching
// focus queries, cannot be focused again, and a focused handle's
// release clears the window focus and dispatches focus-lost listeners
// (window.rs notify_focus_lost).
func (h FocusHandle) Release() {
	w := h.window
	if w == nil {
		return
	}
	fs := focusState(w)
	ref, exists := fs.handles[h.id]
	if !exists || ref.released {
		return // idempotent
	}
	ref.released = true

	if fs.focused == h.id {
		fs.focused = 0
		fs.focusGeneration++
		// Record the lost path from the rendered frame (window.rs
		// notify_focus_lost: focus_lost_path = previous focus path).
		fs.focusLostPath = nil
		if fs.rendered != nil {
			fs.focusLostPath = fs.rendered.dispatchTree.focusPath(h.id)
		}
		fs.pendingInput = nil
		listeners := append([]*focusLostSubscription{}, fs.focusLostListeners...)
		for _, sub := range listeners {
			if sub.callback != nil {
				sub.callback(w)
			}
		}
		fs.focusLostPath = nil
	}
}

// Focus moves focus to this handle's element (FocusHandle::focus).
func (h FocusHandle) Focus(w *Window) {
	if w == nil {
		panic("gpui: FocusHandle.Focus requires a live window")
	}
	w.Focus(h)
}

// IsFocused reports whether this handle's element is focused.
func (h FocusHandle) IsFocused(w *Window) bool {
	if w == nil {
		return false
	}
	fs := focusState(w)
	return fs.focused == h.id && w.focusRefOf(h.id) != nil
}

// ContainsFocused reports whether this handle contains the focused
// element or is itself focused (window.rs contains_focused).
func (h FocusHandle) ContainsFocused(w *Window) bool {
	if w == nil {
		return false
	}
	focused := w.Focused()
	if !focused.Ok {
		return false
	}
	return h.Contains(focused.Handle, w)
}

// WithinFocused reports whether this handle is contained within the
// focused element or is itself focused (window.rs within_focused).
func (h FocusHandle) WithinFocused(w *Window) bool {
	if w == nil {
		return false
	}
	focused := w.Focused()
	if !focused.Ok {
		return false
	}
	return focused.Handle.Contains(h, w)
}

// Contains reports whether this handle contains the other handle in
// the most recently rendered frame (window.rs contains, through the
// dispatch tree's focus_contains).
func (h FocusHandle) Contains(other FocusHandle, w *Window) bool {
	if w == nil {
		return false
	}
	fs := focusState(w)
	if fs.rendered == nil {
		return h.id == other.id
	}
	return fs.rendered.dispatchTree.focusContains(h.id, other.id)
}

// DispatchAction dispatches an action on the element that rendered
// this focus handle (FocusHandle::dispatch_action).
func (h FocusHandle) DispatchAction(action BoxedAction, w *Window, cx AppContext) {
	if w == nil {
		panic("gpui: DispatchAction requires a live window")
	}
	app := appFrom(cx)
	fs := focusState(w)
	nodeID := dispatchNodeIDOfRoot(fs)
	if fs.rendered != nil {
		if id, ok := fs.rendered.dispatchTree.focusableNodeID(h.id); ok {
			nodeID = id
		}
	}
	w.dispatchActionOnNode(nodeID, action, app)
}

// Downgrade converts this handle into a weak variant which does not
// keep the focus registry entry live (FocusHandle::downgrade).
func (h FocusHandle) Downgrade() WeakFocusHandle {
	return WeakFocusHandle{handle: h}
}

// WeakFocusHandle is a focus handle reference whose upgrade fails
// once the strong handle has been released (WeakFocusHandle).
type WeakFocusHandle struct {
	handle FocusHandle
}

// Upgrade returns the strong handle, or false when it was released.
func (w WeakFocusHandle) Upgrade() (FocusHandle, bool) {
	if w.handle.windowRef() == nil {
		return FocusHandle{}, false
	}
	return w.handle, true
}

// ID returns the focus identity within its window.
func (w WeakFocusHandle) ID() uint64 { return w.handle.id }

// ---------------------------------------------------------------------------
// Window focus API (window.rs focus methods)
// ---------------------------------------------------------------------------

// FocusedResult is the focused-handle query result.
type FocusedResult struct {
	// Handle is the focused handle.
	Handle FocusHandle
	// Ok reports whether any handle is focused.
	Ok bool
}

// Focus moves focus to the given handle (Window::focus): no-op when
// focus is disabled or the handle already holds focus; clears pending
// keystrokes and bumps the focus generation.
func (w *Window) Focus(handle FocusHandle) {
	if w == nil {
		panic("gpui: Focus requires a live window")
	}
	fs := focusState(w)
	if !fs.focusEnabled || fs.focused == handle.id {
		return
	}
	if w.focusRefOf(handle.id) == nil {
		// Released (or foreign) handles are not focusable (the weak
		// upgrade failure of the reference).
		return
	}
	fs.focused = handle.id
	fs.focusGeneration++
	w.clearPendingKeystrokes()

	// The reference refreshes the invalidation and redraws; the port
	// records the dirty state through the draw state's frame count on
	// the next draw (DrawWindowFrame is the explicit draw path).
	w.requestRefresh()
}

// Focused returns the handle focused in this window, when any
// (Window::focused: a released handle reports none).
func (w *Window) Focused() FocusedResult {
	if w == nil {
		return FocusedResult{}
	}
	fs := focusState(w)
	if fs.focused == 0 || w.focusRefOf(fs.focused) == nil {
		return FocusedResult{}
	}
	return FocusedResult{Handle: FocusHandle{id: fs.focused, window: w}, Ok: true}
}

// Blur removes focus from all elements in this window (Window::blur).
func (w *Window) Blur() {
	if w == nil {
		return
	}
	fs := focusState(w)
	w.clearPendingKeystrokes()
	if !fs.focusEnabled {
		return
	}
	if fs.focused != 0 {
		fs.focusGeneration++
	}
	fs.focused = 0
	w.requestRefresh()
}

// DisableFocus blurs the window and refuses further focus
// (Window::disable_focus).
func (w *Window) DisableFocus() {
	if w == nil {
		return
	}
	w.Blur()
	focusState(w).focusEnabled = false
}

// FocusNext moves focus to the next tab stop (Window::focus_next).
func (w *Window) FocusNext() {
	if w == nil {
		return
	}
	fs := focusState(w)
	if !fs.focusEnabled {
		return
	}
	if fs.rendered == nil {
		return
	}
	var focused *uint64
	if fs.focused != 0 {
		id := fs.focused
		focused = &id
	}
	if handle, ok := fs.rendered.tabStops.Next(focused, w); ok {
		w.Focus(handle)
	}
}

// FocusPrev moves focus to the previous tab stop
// (Window::focus_prev).
func (w *Window) FocusPrev() {
	if w == nil {
		return
	}
	fs := focusState(w)
	if !fs.focusEnabled {
		return
	}
	if fs.rendered == nil {
		return
	}
	var focused *uint64
	if fs.focused != 0 {
		id := fs.focused
		focused = &id
	}
	if handle, ok := fs.rendered.tabStops.Prev(focused, w); ok {
		w.Focus(handle)
	}
}

// FocusGeneration returns the window's focus generation counter
// (diagnostics; window.rs focus_generation).
func (w *Window) FocusGeneration() uint64 {
	if w == nil {
		return 0
	}
	return focusState(w).focusGeneration
}

// FocusPath returns the focus path of a handle in the rendered frame,
// from the root to the handle (window.rs focus_path).
func (w *Window) FocusPath(handle FocusHandle) []FocusHandle {
	if w == nil {
		return nil
	}
	fs := focusState(w)
	if fs.rendered == nil {
		return []FocusHandle{handle}
	}
	ids := fs.rendered.dispatchTree.focusPath(handle.id)
	out := make([]FocusHandle, 0, len(ids))
	for _, id := range ids {
		out = append(out, FocusHandle{id: id, window: w})
	}
	return out
}

// OnFocusLost subscribes to focus-lost events: the focused handle was
// released (window.rs Window::on_focus_lost). The returned
// subscription cancels the listener.
func (w *Window) OnFocusLost(callback func(w *Window)) *focusLostSubscription {
	if w == nil {
		panic("gpui: OnFocusLost requires a live window")
	}
	fs := focusState(w)
	sub := &focusLostSubscription{owner: fs, callback: callback}
	fs.focusLostListeners = append(fs.focusLostListeners, sub)
	return sub
}

// focusLostSubscription is one focus-lost listener registration.
type focusLostSubscription struct {
	owner    *windowFocusState
	callback func(w *Window)
}

// Cancel removes this listener (the subscription semantics).
func (s *focusLostSubscription) Cancel() {
	if s == nil || s.owner == nil {
		return
	}
	for i, existing := range s.owner.focusLostListeners {
		if existing == s {
			s.owner.focusLostListeners = append(s.owner.focusLostListeners[:i], s.owner.focusLostListeners[i+1:]...)
			break
		}
	}
	s.callback = nil
	s.owner = nil
}

// requestRefresh records that this window's content changed and a new
// frame should be drawn (the invalidation seam; the draw itself is the
// explicit DrawWindowFrame path in this port).
func (w *Window) requestRefresh() {
	if w == nil {
		return
	}
	if ds, ok := windowDrawStates[w]; ok {
		ds.refreshRequested = true
	}
}

// RefreshRequested reports whether a refresh was requested since the
// last completed draw (test support).
func (w *Window) RefreshRequested() bool {
	if w == nil {
		return false
	}
	if ds, ok := windowDrawStates[w]; ok {
		return ds.refreshRequested
	}
	return false
}

// ---------------------------------------------------------------------------
// TabStopMap (tab_stop.rs TabStopMap)
// ---------------------------------------------------------------------------

// tabStopPath is a node's path: the group indices from the root,
// ending with the handle's tab index (tab_stop.rs TabStopPath).
type tabStopPath []int64

// compare orders paths lexicographically.
func (p tabStopPath) compare(other tabStopPath) int {
	for i := 0; i < len(p) && i < len(other); i++ {
		if p[i] != other[i] {
			if p[i] < other[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case len(p) < len(other):
		return -1
	case len(p) > len(other):
		return 1
	}
	return 0
}

// TabStopOperation records one tab-stop insertion or group boundary
// for replay (tab_stop.rs TabStopOperation).
type TabStopOperation struct {
	// Kind is the operation kind.
	Kind TabStopOperationKind
	// Handle is the inserted handle (Insert only).
	Handle FocusHandle
	// GroupIndex is the group's tab index (Group only).
	GroupIndex int64
}

// TabStopOperationKind discriminates tab-stop operations.
type TabStopOperationKind uint8

const (
	// TabStopInsert inserts a focus handle.
	TabStopInsert TabStopOperationKind = iota
	// TabStopGroup begins a tab group.
	TabStopGroup
	// TabStopGroupEnd ends a tab group.
	TabStopGroupEnd
)

// tabStopNode is one ordered tab-stop node (tab_stop.rs TabStopNode).
type tabStopNode struct {
	path    tabStopPath
	index   int // insertion order
	id      uint64
	tabStop bool
	window  *Window
}

// TabStopMap is the frame's collection of tab stops (tab_stop.rs
// TabStopMap, bounded to a sorted slice instead of a SumTree — the
// observable next/prev order is preserved).
type TabStopMap struct {
	currentPath      tabStopPath
	insertionHistory []TabStopOperation
	byID             map[uint64]tabStopNode
	order            []tabStopNode // kept sorted by (path, index)
}

// newTabStopMap creates an empty tab-stop map.
func newTabStopMap() *TabStopMap {
	return &TabStopMap{byID: make(map[uint64]tabStopNode)}
}

// Insert inserts a focus handle at the current group path (TabStopMap::insert).
func (t *TabStopMap) Insert(handle FocusHandle, tabIndex int64, tabStop bool) {
	t.insertionHistory = append(t.insertionHistory, TabStopOperation{Kind: TabStopInsert, Handle: handle})
	path := append(append(tabStopPath{}, t.currentPath...), tabIndex)
	node := tabStopNode{
		path:    path,
		index:   len(t.insertionHistory) - 1,
		id:      handle.id,
		tabStop: tabStop,
		window:  handle.window,
	}
	t.byID[handle.id] = node
	t.insertOrdered(node)
}

func (t *TabStopMap) insertOrdered(node tabStopNode) {
	pos := sort.Search(len(t.order), func(i int) bool {
		existing := t.order[i]
		cmp := existing.path.compare(node.path)
		if cmp != 0 {
			return cmp > 0
		}
		return existing.index > node.index
	})
	t.order = append(t.order, tabStopNode{})
	copy(t.order[pos+1:], t.order[pos:])
	t.order[pos] = node
}

// BeginGroup begins a tab group with the given index (TabStopMap::begin_group).
func (t *TabStopMap) BeginGroup(tabIndex int64) {
	t.insertionHistory = append(t.insertionHistory, TabStopOperation{Kind: TabStopGroup, GroupIndex: tabIndex})
	t.currentPath = append(t.currentPath, tabIndex)
}

// EndGroup ends the current tab group (TabStopMap::end_group).
func (t *TabStopMap) EndGroup() {
	t.insertionHistory = append(t.insertionHistory, TabStopOperation{Kind: TabStopGroupEnd})
	if len(t.currentPath) > 0 {
		t.currentPath = t.currentPath[:len(t.currentPath)-1]
	}
}

// Replay replays recorded operations (TabStopMap::replay).
func (t *TabStopMap) Replay(operations []TabStopOperation, tabIndexOf func(FocusHandle) (int64, bool)) {
	for _, op := range operations {
		switch op.Kind {
		case TabStopInsert:
			tabIndex, tabStop := int64(0), false
			if tabIndexOf != nil {
				if idx, ok := tabIndexOf(op.Handle); ok {
					tabIndex = idx
				}
			}
			if ref, ok := t.lookupRef(op.Handle); ok {
				tabIndex, tabStop = ref.tabIndex, ref.tabStop
			}
			t.Insert(op.Handle, tabIndex, tabStop)
		case TabStopGroup:
			t.BeginGroup(op.GroupIndex)
		case TabStopGroupEnd:
			t.EndGroup()
		}
	}
}

// lookupRef resolves a handle's tab properties from its window
// registry.
func (t *TabStopMap) lookupRef(handle FocusHandle) (*focusRef, bool) {
	if handle.window != nil {
		if ref := handle.window.focusRefOf(handle.id); ref != nil {
			return ref, true
		}
	}
	return nil, false
}

// Next returns the next tab stop after the focused identity, wrapping
// to the first (TabStopMap::next). Returns ok=false with no stops.
func (t *TabStopMap) Next(focusedID *uint64, w *Window) (FocusHandle, bool) {
	if len(t.order) == 0 {
		return FocusHandle{}, false
	}
	if focusedID == nil {
		first := t.order[0]
		if first.tabStop {
			return t.handleFor(first), true
		}
		if node, ok := t.nextInner(first); ok {
			return t.handleFor(node), true
		}
		return FocusHandle{}, false
	}
	node, ok := t.byID[*focusedID]
	if !ok {
		return t.Next(nil, w)
	}
	if next, ok := t.nextInner(node); ok {
		return t.handleFor(next), true
	}
	return t.Next(nil, w)
}

func (t *TabStopMap) nextInner(node tabStopNode) (tabStopNode, bool) {
	// The reference seeks a cursor over the ordered tree then skips
	// non-tab-stops forward; the ordered slice reproduces the same
	// successor sequence.
	start := t.slicePosition(node)
	for i := start + 1; i < len(t.order); i++ {
		if t.order[i].tabStop {
			return t.order[i], true
		}
	}
	return tabStopNode{}, false
}

// Prev returns the previous tab stop before the focused identity,
// wrapping to the last (TabStopMap::prev).
func (t *TabStopMap) Prev(focusedID *uint64, w *Window) (FocusHandle, bool) {
	if len(t.order) == 0 {
		return FocusHandle{}, false
	}
	if focusedID == nil {
		last := t.order[len(t.order)-1]
		if last.tabStop {
			return t.handleFor(last), true
		}
		if node, ok := t.prevInner(last); ok {
			return t.handleFor(node), true
		}
		return FocusHandle{}, false
	}
	node, ok := t.byID[*focusedID]
	if !ok {
		return t.Prev(nil, w)
	}
	if prev, ok := t.prevInner(node); ok {
		return t.handleFor(prev), true
	}
	return t.Prev(nil, w)
}

func (t *TabStopMap) prevInner(node tabStopNode) (tabStopNode, bool) {
	// Mirror of nextInner: scan slice order backwards, skipping
	// non-tab-stops.
	start := t.slicePosition(node)
	for i := start - 1; i >= 0; i-- {
		if t.order[i].tabStop {
			return t.order[i], true
		}
	}
	return tabStopNode{}, false
}

// slicePosition finds the node's position in the ordered slice.
func (t *TabStopMap) slicePosition(node tabStopNode) int {
	for i, existing := range t.order {
		if existing.id == node.id {
			return i
		}
	}
	return -1
}

func (t *TabStopMap) handleFor(node tabStopNode) FocusHandle {
	return FocusHandle{id: node.id, window: node.window}
}

// TabStopCount returns the number of tab stops (tab_stop.rs
// tab_stop_count).
func (t *TabStopMap) TabStopCount() int {
	count := 0
	for _, node := range t.order {
		if node.tabStop {
			count++
		}
	}
	return count
}

// PaintIndex returns the insertion history length (the reuse seam).
func (t *TabStopMap) PaintIndex() int { return len(t.insertionHistory) }

// History returns the recorded operations (replay source).
func (t *TabStopMap) History() []TabStopOperation {
	return append([]TabStopOperation{}, t.insertionHistory...)
}

// String renders the map for diagnostics.
func (t *TabStopMap) String() string {
	return fmt.Sprintf("TabStopMap(%d stops, %d operations)", t.TabStopCount(), len(t.insertionHistory))
}
