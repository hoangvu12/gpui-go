package gpui

// This file ports the key dispatch of the pinned CE reference
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/key_dispatch.rs — DispatchTree (nodes with key,
//     action and modifiers-changed listeners, key contexts, focus and
//     view identities), the dispatch path, dispatch_key with its
//     pending/replay semantics, flush_dispatch, bindings_for_action
//     and the focus containment query;
//   - crates/gpui/src/window.rs — dispatch_key_event (modifiers-changed
//     synthesis of lone-modifier keystrokes, keystroke interceptors,
//     pending input with focus tracking and the chord timeout, the
//     prefer-character-input skip, binding dispatch, then the key
//     listeners in capture and bubble order), dispatch_action_on_node
//     (global capture, window capture, window bubble with
//     stop-by-default, global bubble), replay_pending_input, and the
//     keystroke observers.
//
// Go adaptations, recorded beside the behavior:
//
//   - The tree is built during element prepaint (the reference builds
//     it during the element draw's interactivity prepaint), into the
//     window's next-frame state, and swaps into the rendered state at
//     frame completion (render.go swaps; div.go prepaint pushes).
//   - Dispatch draws on demand are not performed: the port's draws are
//     explicit (DrawWindowFrame), so dispatch uses the last completed
//     frame's tree.
//   - The chord timeout task uses the scheduler's (virtual) clock; real
//     wall-clock timers arrive with the shutdown-integration ticket, so
//     real windows clear chords on the next key or focus change but do
//     not time out on idle yet (tests drive the virtual clock).

import (
	"fmt"
	"time"
)

// ---------------------------------------------------------------------------
// Dispatch tree (key_dispatch.rs DispatchTree)
// ---------------------------------------------------------------------------

// DispatchPhase tells the listener which phase of dispatch an event is
// in (the reference DispatchPhase).
type DispatchPhase uint8

const (
	// DispatchCapture runs listeners from the root down to the target
	// before the bubble phase.
	DispatchCapture DispatchPhase = iota
	// DispatchBubble runs listeners from the target up to the root.
	DispatchBubble
)

// DispatchNodeID identifies a node of a DispatchTree. Node identities
// are only meaningful within the tree that provided them (not stable
// between frames).
type DispatchNodeID struct {
	// Index is the node's index in the tree's node slice.
	Index int
}

// keyListener observes raw key events in a dispatch phase.
type keyListener struct {
	// down observes KeyDownEvent; a listener observes exactly one kind.
	down bool
	// capture routes the listener on the capture phase (bubble default).
	capture bool
	// callback is the plain listener shape (func(*KeyDownEvent, *Window,
	// *App) or func(*KeyUpEvent, *Window, *App)).
	callback any
}

// modifiersChangedListener observes modifier-state changes (bubble).
type modifiersChangedListener struct {
	// callback is func(*ModifiersChangedEvent, *Window, *App).
	callback any
}

// dispatchActionListener binds an action identity to a listener
// (DispatchActionListener).
type dispatchActionListener struct {
	// actionType is the action's canonical descriptor.
	actionType *actionDescriptor
	// capture routes the listener on the capture phase.
	capture bool
	// callback is func(payload any, phase DispatchPhase, w *Window,
	// app *App); the payload is the cloned action value.
	callback func(action any, phase DispatchPhase, w *Window, app *App)
}

// dispatchNode is one node of the dispatch tree (DispatchNode).
type dispatchNode struct {
	keyListeners              []keyListener
	actionListeners           []dispatchActionListener
	modifiersChangedListeners []modifiersChangedListener
	context                   *KeyContext
	focusID                   uint64
	hasFocusID                bool
	viewID                    EntityID
	hasViewID                 bool
	parent                    DispatchNodeID
	hasParent                 bool
}

// DispatchTree is the per-frame interaction tree (DispatchTree).
type DispatchTree struct {
	nodeStack []DispatchNodeID
	// contextStack mirrors the reference's tracked context stack of the
	// active node path.
	contextStack []KeyContext
	viewStack    []EntityID
	nodes        []dispatchNode

	focusableNodeIDs map[uint64]DispatchNodeID
	viewNodeIDs      map[EntityID]DispatchNodeID

	keymap         *Keymap
	actionRegistry *appActionRegistry
}

// NewDispatchTree creates a tree bound to a keymap (DispatchTree::new).
func NewDispatchTree(keymap *Keymap) *DispatchTree {
	return &DispatchTree{
		focusableNodeIDs: make(map[uint64]DispatchNodeID),
		viewNodeIDs:      make(map[EntityID]DispatchNodeID),
		keymap:           keymap,
	}
}

// Len returns the tree's node count.
func (t *DispatchTree) Len() int { return len(t.nodes) }

// Clear resets the tree for reuse (DispatchTree::clear).
func (t *DispatchTree) Clear() {
	t.nodeStack = nil
	t.contextStack = nil
	t.viewStack = nil
	t.nodes = nil
	t.focusableNodeIDs = make(map[uint64]DispatchNodeID)
	t.viewNodeIDs = make(map[EntityID]DispatchNodeID)
}

// PushNode pushes a child of the active node and makes it active
// (DispatchTree::push_node).
func (t *DispatchTree) PushNode() DispatchNodeID {
	parent, hasParent := t.activeNodeID()
	id := DispatchNodeID{Index: len(t.nodes)}
	t.nodes = append(t.nodes, dispatchNode{parent: parent, hasParent: hasParent})
	t.nodeStack = append(t.nodeStack, id)
	return id
}

// SetActiveNode rewinds the stack so the given node is active
// (DispatchTree::set_active_node): pops to the node's parent, or, when
// the stack cannot reach it (detached), rebuilds the stack from its
// ancestors.
func (t *DispatchTree) SetActiveNode(nodeID DispatchNodeID) {
	nextNodeParent := DispatchNodeID{}
	hasNext := false
	if nodeID.Index < len(t.nodes) && t.nodes[nodeID.Index].hasParent {
		nextNodeParent = t.nodes[nodeID.Index].parent
		hasNext = true
	}
	for len(t.nodeStack) > 0 {
		active, _ := t.activeNodeID()
		if hasNext && active == nextNodeParent {
			break
		}
		if !hasNext && active == nodeID {
			break
		}
		t.PopNode()
	}

	if len(t.nodeStack) > 0 {
		if active, _ := t.activeNodeID(); !hasNext || active == nextNodeParent {
			t.nodeStack = append(t.nodeStack, nodeID)
			node := &t.nodes[nodeID.Index]
			if node.hasViewID {
				t.viewStack = append(t.viewStack, node.viewID)
			}
			if node.context != nil {
				t.contextStack = append(t.contextStack, *node.context)
			}
			return
		}
	}

	// Detached: rebuild the stack from the node's ancestors.
	var chain []DispatchNodeID
	current := nodeID
	for {
		chain = append(chain, current)
		node := t.nodes[current.Index]
		if !node.hasParent {
			break
		}
		current = node.parent
	}
	t.nodeStack = nil
	t.contextStack = nil
	t.viewStack = nil
	for i := len(chain) - 1; i >= 0; i-- {
		id := chain[i]
		t.nodeStack = append(t.nodeStack, id)
		node := &t.nodes[id.Index]
		if node.context != nil {
			t.contextStack = append(t.contextStack, *node.context)
		}
		if node.hasViewID {
			t.viewStack = append(t.viewStack, node.viewID)
		}
	}
}

// SetKeyContext sets the active node's key context (and pushes it onto
// the tracked context stack).
func (t *DispatchTree) SetKeyContext(context KeyContext) {
	id, ok := t.activeNodeID()
	if !ok {
		panic("gpui: SetKeyContext requires an active node")
	}
	contextCopy := context
	t.nodes[id.Index].context = &contextCopy
	t.contextStack = append(t.contextStack, context)
}

// SetFocusID marks the active node with the focus identity
// (DispatchTree::set_focus_id).
func (t *DispatchTree) SetFocusID(focusID uint64) {
	id := t.activeNodeIDValue()
	t.nodes[id.Index].focusID = focusID
	t.nodes[id.Index].hasFocusID = true
	t.focusableNodeIDs[focusID] = id
}

// SetViewID marks the active node with the view identity
// (DispatchTree::set_view_id).
func (t *DispatchTree) SetViewID(viewID EntityID) {
	if len(t.viewStack) > 0 && t.viewStack[len(t.viewStack)-1] == viewID {
		return
	}
	id := t.activeNodeIDValue()
	t.nodes[id.Index].viewID = viewID
	t.nodes[id.Index].hasViewID = true
	t.viewNodeIDs[viewID] = id
	t.viewStack = append(t.viewStack, viewID)
}

// PopNode pops the active node (DispatchTree::pop_node).
func (t *DispatchTree) PopNode() {
	id := t.activeNodeIDValue()
	node := &t.nodes[id.Index]
	if node.context != nil {
		t.contextStack = t.contextStack[:len(t.contextStack)-1]
	}
	if node.hasViewID && len(t.viewStack) > 0 {
		t.viewStack = t.viewStack[:len(t.viewStack)-1]
	}
	t.nodeStack = t.nodeStack[:len(t.nodeStack)-1]
}

func (t *DispatchTree) activeNodeID() (DispatchNodeID, bool) {
	if len(t.nodeStack) == 0 {
		return DispatchNodeID{}, false
	}
	return t.nodeStack[len(t.nodeStack)-1], true
}

func (t *DispatchTree) activeNodeIDValue() DispatchNodeID {
	id, ok := t.activeNodeID()
	if !ok {
		panic("gpui: no active dispatch node (push one first)")
	}
	return id
}

// ActiveNodeID returns the active node (diagnostics).
func (t *DispatchTree) ActiveNodeID() (DispatchNodeID, bool) { return t.activeNodeID() }

// OnKeyEvent registers a raw key listener on the active node
// (DispatchTree::on_key_event). callback is
// func(*KeyDownEvent, *Window, *App) when down, else
// func(*KeyUpEvent, *Window, *App).
func (t *DispatchTree) OnKeyEvent(down, capture bool, callback any) {
	id := t.activeNodeIDValue()
	t.nodes[id.Index].keyListeners = append(t.nodes[id.Index].keyListeners,
		keyListener{down: down, capture: capture, callback: callback})
}

// OnModifiersChanged registers a modifiers-changed listener on the
// active node.
func (t *DispatchTree) OnModifiersChanged(callback any) {
	id := t.activeNodeIDValue()
	t.nodes[id.Index].modifiersChangedListeners = append(t.nodes[id.Index].modifiersChangedListeners,
		modifiersChangedListener{callback: callback})
}

// OnAction registers an action listener on the active node
// (DispatchTree::on_action).
func (t *DispatchTree) OnAction(actionType *actionDescriptor, capture bool, callback func(action any, phase DispatchPhase, w *Window, app *App)) {
	id := t.activeNodeIDValue()
	t.nodes[id.Index].actionListeners = append(t.nodes[id.Index].actionListeners,
		dispatchActionListener{actionType: actionType, capture: capture, callback: callback})
}

// FocusContains reports whether the parent handle's node contains the
// child handle's node (DispatchTree::focus_contains).
func (t *DispatchTree) focusContains(parent, child uint64) bool {
	if parent == child {
		return true
	}
	parentNode, ok := t.focusableNodeIDs[parent]
	if !ok {
		return false
	}
	current, ok := t.focusableNodeIDs[child]
	for ok {
		if current == parentNode {
			return true
		}
		node := t.nodes[current.Index]
		if !node.hasParent {
			return false
		}
		current = node.parent
	}
	return false
}

// DispatchPath returns the node path from the root to the target
// (DispatchTree::dispatch_path).
func (t *DispatchTree) DispatchPath(target DispatchNodeID) []DispatchNodeID {
	var path []DispatchNodeID
	current := target
	for {
		path = append(path, current)
		node := t.nodes[current.Index]
		if !node.hasParent {
			break
		}
		current = node.parent
	}
	// reverse to root-first
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// FocusPath returns the focus identities from the root to the given
// handle's node (DispatchTree::focus_path).
func (t *DispatchTree) focusPath(focusID uint64) []uint64 {
	var path []uint64
	current, ok := t.focusableNodeIDs[focusID]
	for ok {
		node := t.nodes[current.Index]
		if node.hasFocusID {
			path = append(path, node.focusID)
		}
		if !node.hasParent {
			break
		}
		current = node.parent
	}
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

// FocusableNodeID returns the node carrying the focus identity.
func (t *DispatchTree) focusableNodeID(focusID uint64) (DispatchNodeID, bool) {
	id, ok := t.focusableNodeIDs[focusID]
	return id, ok
}

// RootNodeID returns the root node.
func (t *DispatchTree) RootNodeID() DispatchNodeID { return DispatchNodeID{Index: 0} }

// ContextStack returns the tracked context stack of the active path.
func (t *DispatchTree) ContextStack() []KeyContext { return t.contextStack }

// Node returns the node's stored registrations (diagnostics).
func (t *DispatchTree) Node(id DispatchNodeID) *dispatchNode {
	return &t.nodes[id.Index]
}

// ---------------------------------------------------------------------------
// Key dispatch results (key_dispatch.rs Replay/DispatchResult)
// ---------------------------------------------------------------------------

// Replay is one keystroke to replay with the bindings it matched
// (key_dispatch.rs Replay).
type Replay struct {
	// Keystroke is the pending keystroke being replayed.
	Keystroke Keystroke
	// Bindings are the bindings the replayed prefix matched.
	Bindings []KeyBinding
}

// dispatchResult is the outcome of dispatch_key (DispatchResult).
type dispatchResult struct {
	pending           []Keystroke
	pendingHasBinding bool
	bindings          []KeyBinding
	toReplay          []Replay
	contextStack      []KeyContext
}

// bindingsForInputOnPath computes the context stack for a dispatch path
// and queries the keymap (DispatchTree::bindings_for_input).
func (t *DispatchTree) bindingsForInputOnPath(input []Keystroke, dispatchPath []DispatchNodeID) (bindings []KeyBinding, pending bool, contextStack []KeyContext) {
	for _, nodeID := range dispatchPath {
		if node := t.nodes[nodeID.Index]; node.context != nil {
			contextStack = append(contextStack, *node.context)
		}
	}
	bindings, pending = t.keymap.BindingsForInput(input, contextStack)
	return bindings, pending, contextStack
}

// dispatchKey processes a keystroke against the pending input
// (DispatchTree::dispatch_key): pending accumulates chord prefixes;
// unmatched suffixes replay through replayPrefix; matched bindings
// dispatch.
func (t *DispatchTree) dispatchKey(input []Keystroke, keystroke Keystroke, dispatchPath []DispatchNodeID) dispatchResult {
	input = append(append([]Keystroke{}, input...), keystroke)
	bindings, pending, contextStack := t.bindingsForInputOnPath(input, dispatchPath)

	if pending {
		return dispatchResult{
			pending:           input,
			pendingHasBinding: len(bindings) > 0,
			contextStack:      contextStack,
		}
	} else if len(bindings) > 0 {
		return dispatchResult{
			bindings:     bindings,
			contextStack: contextStack,
		}
	} else if len(input) == 1 {
		return dispatchResult{contextStack: contextStack}
	}
	input = input[:len(input)-1]

	suffix, toReplay := t.replayPrefix(input, dispatchPath)

	result := t.dispatchKey(suffix, keystroke, dispatchPath)
	toReplay = append(toReplay, result.toReplay...)
	result.toReplay = toReplay
	return result
}

// flushDispatch converts pending input to replay events on timeout
// (DispatchTree::flush_dispatch).
func (t *DispatchTree) flushDispatch(input []Keystroke, dispatchPath []DispatchNodeID) []Replay {
	suffix, toReplay := t.replayPrefix(input, dispatchPath)
	if len(suffix) > 0 {
		toReplay = append(toReplay, t.flushDispatch(suffix, dispatchPath)...)
	}
	return toReplay
}

// replayPrefix converts the longest matched prefix of the input into a
// replay event and returns the rest (DispatchTree::replay_prefix).
func (t *DispatchTree) replayPrefix(input []Keystroke, dispatchPath []DispatchNodeID) (suffix []Keystroke, toReplay []Replay) {
	suffix = append([]Keystroke{}, input...)
	for last := len(input) - 1; last >= 0; last-- {
		bindings, _, _ := t.bindingsForInputOnPath(input[:last+1], dispatchPath)
		if len(bindings) > 0 {
			// drain(0..=last).next_back() == the keystroke at last.
			replayed := suffix[last]
			suffix = append(append([]Keystroke{}, suffix[:last]...), suffix[last+1:]...)
			toReplay = append(toReplay, Replay{Keystroke: replayed, Bindings: bindings})
			return suffix, toReplay
		}
	}
	if len(suffix) > 0 {
		first := suffix[0]
		suffix = suffix[1:]
		toReplay = append(toReplay, Replay{Keystroke: first})
	}
	return suffix, toReplay
}

// BindingsForAction returns the key bindings that invoke the action on
// the context stack, unshadowed (DispatchTree::bindings_for_action).
func (t *DispatchTree) BindingsForAction(action BoxedAction, contextStack []KeyContext) []KeyBinding {
	var out []KeyBinding
	for _, binding := range t.keymap.BindingsForAction(action) {
		if t.bindingMatchesPredicateAndNotShadowed(binding, contextStack) {
			out = append(out, binding)
		}
	}
	return out
}

// HighestPrecedenceBindingForAction returns the last unshadowed binding
// for the action.
func (t *DispatchTree) HighestPrecedenceBindingForAction(action BoxedAction, contextStack []KeyContext) (KeyBinding, bool) {
	for _, binding := range t.keymap.BindingsForAction(action) {
		if t.bindingMatchesPredicateAndNotShadowed(binding, contextStack) {
			return binding, true
		}
	}
	return KeyBinding{}, false
}

func (t *DispatchTree) bindingMatchesPredicateAndNotShadowed(binding KeyBinding, contextStack []KeyContext) bool {
	bindings, _ := t.keymap.BindingsForInput(bindingKeystrokesOf(binding), contextStack)
	if len(bindings) > 0 {
		return bindings[0].action.Equal(binding.action)
	}
	return false
}

func bindingKeystrokesOf(binding KeyBinding) []Keystroke {
	out := make([]Keystroke, 0, len(binding.keystrokes))
	for _, ks := range binding.keystrokes {
		out = append(out, ks.inner)
	}
	return out
}

// PossibleNextBindingsForInput finds the bindings that can follow the
// input (DispatchTree::possible_next_bindings_for_input).
func (t *DispatchTree) PossibleNextBindingsForInput(input []Keystroke, contextStack []KeyContext) []KeyBinding {
	return t.keymap.PossibleNextBindingsForInput(input, contextStack)
}

// AvailableActions returns the actions listening on the dispatch path
// (DispatchTree::available_actions), building default payloads from the
// app registry when possible. The payload values are not constructed
// here: the port returns the action identities (see the file header;
// the reference builds Box<dyn Action> defaults, the port defers
// payload construction to dispatch).
func (t *DispatchTree) availableActions(target DispatchNodeID, registry *appActionRegistry) []BoxedAction {
	seen := map[*actionDescriptor]struct{}{}
	var out []BoxedAction
	for _, nodeID := range t.DispatchPath(target) {
		node := t.nodes[nodeID.Index]
		for _, listener := range node.actionListeners {
			if _, dup := seen[listener.actionType]; dup {
				continue
			}
			seen[listener.actionType] = struct{}{}
			// Intentionally skip actions that cannot be built by
			// default (the reference build_action_type().ok()).
			if action, ok := defaultBoxedAction(listener.actionType, registry); ok {
				out = append(out, action)
			}
		}
	}
	return out
}

// IsActionAvailable reports whether the action is listened to on the
// dispatch path (DispatchTree::is_action_available).
func (t *DispatchTree) isActionAvailable(action BoxedAction, target DispatchNodeID) bool {
	for _, nodeID := range t.DispatchPath(target) {
		node := t.nodes[nodeID.Index]
		for _, listener := range node.actionListeners {
			if listener.actionType == action.descriptor {
				return true
			}
		}
	}
	return false
}

// defaultBoxedAction builds the action's default payload when the
// descriptor carries a zero-value default (the reference's
// ActionRegistry::build_action_type with a default-constructed payload
// — the port constructs the payload through the descriptor's clone of
// the zero value).
func defaultBoxedAction(descriptor *actionDescriptor, registry *appActionRegistry) (BoxedAction, bool) {
	if descriptor == nil {
		return BoxedAction{}, false
	}
	if payload, ok := descriptor.defaultPayload(); ok {
		return BoxedAction{descriptor: descriptor, payload: payload}, true
	}
	return BoxedAction{}, false
}

// ---------------------------------------------------------------------------
// Frame construction helpers (the draw pipeline's tree build)
// ---------------------------------------------------------------------------

// beginDispatchFrame installs a fresh next frame (tree + tab stops) for
// a window draw. Called at draw start (render.go).
func beginDispatchFrame(w *Window) {
	fs := focusState(w)
	keymap := w.app.keymap
	if keymap == nil {
		keymap = NewKeymap(nil)
	}
	fs.next = &renderedFrame{
		dispatchTree: NewDispatchTree(keymap),
		tabStops:     newTabStopMap(),
	}
	fs.next.dispatchTree.PushNode() // the root node
}

// endDispatchFrame swaps the completed next frame into the rendered
// state at draw completion (render.go): the last focused input-handler
// registration of the frame becomes the window's active handler
// (window.rs: the next frame's input handlers feed the platform input
// handler).
func endDispatchFrame(w *Window) {
	fs := focusState(w)
	if fs.next == nil {
		return
	}
	if len(fs.frameInputHandlers) > 0 {
		fs.inputHandler = fs.frameInputHandlers[len(fs.frameInputHandlers)-1]
	} else {
		fs.inputHandler = nil
	}
	fs.frameInputHandlers = nil
	fs.rendered = fs.next
	fs.next = nil
}

// discardDispatchFrame drops an abandoned frame's dispatch tree without
// swapping the rendered state (render.go's failure path: the prior
// published frame's tree stays active).
func discardDispatchFrame(w *Window) {
	fs := focusState(w)
	fs.next = nil
	fs.frameInputHandlers = nil
}

// currentDispatchTree returns the tree under construction (element
// prepaint pushes nodes into it).
func currentDispatchTree(w *Window) *DispatchTree {
	fs := focusState(w)
	if fs.next == nil {
		panic("gpui: no dispatch frame is being built (prepaint runs inside a window draw)")
	}
	return fs.next.dispatchTree
}

// currentTabStops returns the tab-stop map under construction.
func currentTabStops(w *Window) *TabStopMap {
	fs := focusState(w)
	if fs.next == nil {
		panic("gpui: no dispatch frame is being built (prepaint runs inside a window draw)")
	}
	return fs.next.tabStops
}

// WithDispatchNode pushes a dispatch node for the duration of f (the
// interactivity prepaint's node scope): the node carries the optional
// key context and focus identity, and f runs with it active.
func WithDispatchNode[R any](w *Window, context *KeyContext, focus *FocusHandle, f func(w *Window) R) R {
	tree := currentDispatchTree(w)
	tree.PushNode()
	if context != nil {
		tree.SetKeyContext(*context)
	}
	if focus != nil {
		tree.SetFocusID(focus.id)
	}
	defer tree.PopNode()
	return f(w)
}

// WithTabGroup runs f inside a tab group with the given index
// (window.rs with_tab_group).
func WithTabGroup[R any](w *Window, index *int64, f func(w *Window) R) R {
	if index == nil {
		return f(w)
	}
	stops := currentTabStops(w)
	stops.BeginGroup(*index)
	defer stops.EndGroup()
	return f(w)
}

// registerFocusStop inserts a focus handle into the frame's tab stops
// (div.rs prepaint: the container's own handle inside its group).
func registerFocusStop(w *Window, handle FocusHandle) {
	stops := currentTabStops(w)
	tabIndex, tabStop := handle.tabIndex, handle.tabStop
	if ref := handle.windowRef(); ref != nil {
		tabIndex, tabStop = ref.tabIndex, ref.tabStop
	}
	stops.Insert(handle, tabIndex, tabStop)
}

// registerInputHandler records a focused input handler for the frame
// (window.rs handle_input: only when the handle is focused).
func registerInputHandler(w *Window, handle FocusHandle, handler InputHandler) {
	fs := focusState(w)
	if fs.next == nil {
		panic("gpui: HandleInput requires an active frame")
	}
	if !handle.IsFocused(w) {
		return
	}
	fs.frameInputHandlers = append(fs.frameInputHandlers, handler)
}

// ---------------------------------------------------------------------------
// Window keyboard dispatch (window.rs dispatch_key_event)
// ---------------------------------------------------------------------------

// DispatchKeyEvent dispatches a platform keyboard event through the
// window: pending chord state, bindings, action dispatch, key listeners
// in capture and bubble order, then keystroke observers
// (window.rs handle_input → dispatch_key_event; the platform input
// callback of the reference routes here).
//
// event is one of *KeyDownEvent, *KeyUpEvent or *ModifiersChangedEvent.
// It reports whether the event should continue to the platform's
// default processing (propagation was not stopped).
func (w *Window) DispatchKeyEvent(event any, cx AppContext) bool {
	if w == nil {
		panic("gpui: DispatchKeyEvent requires a live window")
	}
	app := appFrom(cx)
	var propagate bool
	app.Update(func(a *App) {
		propagate = w.dispatchKeyEvent(event, a)
	})
	return propagate
}

func (w *Window) dispatchKeyEvent(event any, app *App) (propagate bool) {
	w.dispatchKeyEventInner(event, app)
	return app.propagateEvent
}

func (w *Window) dispatchKeyEventInner(event any, app *App) {
	fs := focusState(w)
	dispatchPath, nodeID := w.focusDispatchPath()

	var keystroke *Keystroke

	// Propagation starts enabled for every path (cx.propagate_event =
	// true; the modifiers-only path keeps the default-continue rule).
	app.propagateEvent = true

	if modifiers, ok := event.(*ModifiersChangedEvent); ok {
		if modifiers.Modifiers.NumberOfModifiers() == 0 &&
			fs.pendingModifier.modifiers.NumberOfModifiers() == 1 &&
			!fs.pendingModifier.sawOtherInput {
			var key string
			switch {
			case fs.pendingModifier.modifiers.Shift:
				key = "shift"
			case fs.pendingModifier.modifiers.Control:
				key = "control"
			case fs.pendingModifier.modifiers.Alt:
				key = "alt"
			case fs.pendingModifier.modifiers.Platform:
				key = "platform"
			case fs.pendingModifier.modifiers.Function:
				key = "function"
			}
			if key != "" {
				keystroke = &Keystroke{Key: key, Modifiers: Modifiers{}}
			}
		}

		if fs.pendingModifier.modifiers.NumberOfModifiers() == 0 &&
			modifiers.Modifiers.NumberOfModifiers() == 1 {
			fs.pendingModifier.sawOtherInput = false
		} else if modifiers.Modifiers.NumberOfModifiers() > 1 {
			fs.pendingModifier.sawOtherInput = true
		}
		fs.pendingModifier.modifiers = modifiers.Modifiers
	} else if keyDown, ok := event.(*KeyDownEvent); ok {
		fs.pendingModifier.sawOtherInput = true
		k := keyDown.Keystroke
		keystroke = &k
	}

	if keystroke == nil {
		w.finishDispatchKeyEvent(event, dispatchPath, fs.contextStackOfRendered(), app)
		return
	}

	app.propagateEvent = true
	w.dispatchKeystrokeInterceptors(event, fs.contextStackOfRendered(), app)
	if !app.propagateEvent {
		w.finishDispatchKeyEvent(event, dispatchPath, fs.contextStackOfRendered(), app)
		return
	}

	currentlyPending := fs.pendingInput
	fs.pendingInput = nil
	if currentlyPending != nil && currentlyPending.focus != 0 && currentlyPending.focus != fs.focused {
		currentlyPending = nil
	}

	pendingKeystrokes := []Keystroke{}
	if currentlyPending != nil {
		pendingKeystrokes = currentlyPending.keystrokes
	}

	matchResult := fs.renderedDispatchTree().dispatchKey(pendingKeystrokes, *keystroke, dispatchPath)

	if len(matchResult.toReplay) > 0 {
		w.replayPendingInput(matchResult.toReplay, app)
		app.propagateEvent = true
	}

	if len(matchResult.pending) > 0 {
		pending := &pendingInput{
			keystrokes: matchResult.pending,
			focus:      fs.focused,
		}
		if currentlyPending != nil {
			pending.needsTimeout = currentlyPending.needsTimeout
		}

		textInputRequiresTimeout := false
		if keyDown, ok := event.(*KeyDownEvent); ok && keyDown.Keystroke.KeyChar != "" {
			if handler := fs.inputHandler; handler != nil {
				textInputRequiresTimeout = handler.AcceptsTextInput(w, app)
			}
		}

		pending.needsTimeout = pending.needsTimeout || matchResult.pendingHasBinding || textInputRequiresTimeout

		if pending.needsTimeout {
			pending.timer = w.spawnChordTimeout(app, 1*time.Second)
		}
		fs.pendingInput = pending
		w.pendingInputChanged(app)
		app.propagateEvent = false
		return
	}

	skipBindings := false
	if keyDown, ok := event.(*KeyDownEvent); ok && keyDown.PreferCharacterInput {
		if handler := fs.inputHandler; handler != nil {
			// If modifiers are not excessive (AltGr) and the input
			// handler accepts text input, text input wins over
			// bindings.
			skipBindings = handler.AcceptsTextInput(w, app)
		}
	}

	if !skipBindings {
		for _, binding := range matchResult.bindings {
			w.dispatchActionOnNode(nodeID, binding.action, app)
			if !app.propagateEvent {
				w.dispatchKeystrokeObservers(event, binding.action, matchResult.contextStack, app)
				w.pendingInputChanged(app)
				return
			}
		}
	}

	w.finishDispatchKeyEvent(event, dispatchPath, matchResult.contextStack, app)
	w.dispatchKeystrokeObservers(event, BoxedAction{}, matchResult.contextStack, app)
	w.pendingInputChanged(app)
}

// focusDispatchPath resolves the focused node and its dispatch path in
// the rendered frame (focus_node_id_in_rendered_frame +
// dispatch_path).
func (w *Window) focusDispatchPath() (path []DispatchNodeID, nodeID DispatchNodeID) {
	fs := focusState(w)
	tree := fs.renderedDispatchTree()
	nodeID = tree.RootNodeID()
	if fs.focused != 0 {
		if id, ok := tree.focusableNodeID(fs.focused); ok {
			nodeID = id
		}
	}
	return tree.DispatchPath(nodeID), nodeID
}

// renderedDispatchTree returns the rendered frame's tree, creating an
// empty root-only tree when no frame has been drawn yet.
func (fs *windowFocusState) renderedDispatchTree() *DispatchTree {
	if fs.rendered == nil {
		fs.rendered = &renderedFrame{
			dispatchTree: NewDispatchTree(NewKeymap(nil)),
			tabStops:     newTabStopMap(),
		}
		fs.rendered.dispatchTree.PushNode()
	}
	return fs.rendered.dispatchTree
}

// contextStackOfRendered returns the rendered tree's tracked context
// stack (window.rs context_stack — the active path's contexts; the
// port derives it from the focused node's path).
func (fs *windowFocusState) contextStackOfRendered() []KeyContext {
	tree := fs.renderedDispatchTree()
	return tree.contextStack
}

func (w *Window) finishDispatchKeyEvent(event any, dispatchPath []DispatchNodeID, contextStack []KeyContext, app *App) {
	w.dispatchKeyUpDownEvent(event, dispatchPath, app)
	if !app.propagateEvent {
		return
	}
	w.dispatchModifiersChangedEvent(event, dispatchPath, app)
	if !app.propagateEvent {
		return
	}
	w.dispatchKeystrokeObservers(event, BoxedAction{}, contextStack, app)
}

// dispatchKeyUpDownEvent delivers raw key events to the tree's key
// listeners in capture then bubble order (window.rs
// dispatch_key_down_up_event). Entering with propagation already
// stopped delivers nothing: the reference's loop invokes at most the
// first closure before its check, which is a phase no-op for
// bubble-only registrations, so the observable behavior is none.
func (w *Window) dispatchKeyUpDownEvent(event any, dispatchPath []DispatchNodeID, app *App) {
	fs := focusState(w)
	tree := fs.renderedDispatchTree()
	if !app.propagateEvent {
		return
	}

	// Capture phase.
	for _, nodeID := range dispatchPath {
		node := tree.nodes[nodeID.Index]
		for _, listener := range node.keyListeners {
			if !listener.capture {
				continue
			}
			invokeKeyListener(listener, event, DispatchCapture, w, app)
			if !app.propagateEvent {
				return
			}
		}
	}

	// Bubble phase.
	for i := len(dispatchPath) - 1; i >= 0; i-- {
		node := tree.nodes[dispatchPath[i].Index]
		for _, listener := range node.keyListeners {
			if listener.capture {
				continue
			}
			invokeKeyListener(listener, event, DispatchBubble, w, app)
			if !app.propagateEvent {
				return
			}
		}
	}
}

func invokeKeyListener(listener keyListener, event any, phase DispatchPhase, w *Window, app *App) {
	keyDown, isDown := event.(*KeyDownEvent)
	keyUp, isUp := event.(*KeyUpEvent)
	switch {
	case listener.down && isDown:
		if f, ok := listener.callback.(func(*KeyDownEvent, *Window, *App)); ok {
			f(keyDown, w, app)
		}
	case !listener.down && isUp:
		if f, ok := listener.callback.(func(*KeyUpEvent, *Window, *App)); ok {
			f(keyUp, w, app)
		}
	}
}

// dispatchModifiersChangedEvent delivers modifier changes to the
// tree's listeners (bubble only; window.rs
// dispatch_modifiers_changed_event).
func (w *Window) dispatchModifiersChangedEvent(event any, dispatchPath []DispatchNodeID, app *App) {
	modifiers, ok := event.(*ModifiersChangedEvent)
	if !ok {
		return
	}
	fs := focusState(w)
	tree := fs.renderedDispatchTree()
	for i := len(dispatchPath) - 1; i >= 0; i-- {
		node := tree.nodes[dispatchPath[i].Index]
		for _, listener := range node.modifiersChangedListeners {
			if f, ok := listener.callback.(func(*ModifiersChangedEvent, *Window, *App)); ok {
				f(modifiers, w, app)
			}
			if !app.propagateEvent {
				return
			}
		}
	}
}

// replayPendingInput replays unmatched pending keystrokes: their
// bindings dispatch, then the key listeners, then the text input
// (window.rs replay_pending_input).
func (w *Window) replayPendingInput(replays []Replay, app *App) {
	dispatchPath, nodeID := w.focusDispatchPath()
	fs := focusState(w)
	tree := fs.renderedDispatchTree()

replay:
	for _, replay := range replays {
		event := KeyDownEvent{
			Keystroke:            replay.Keystroke,
			PreferCharacterInput: true,
		}
		app.propagateEvent = true
		for _, binding := range replay.Bindings {
			w.dispatchActionOnNode(nodeID, binding.action, app)
			if !app.propagateEvent {
				w.dispatchKeystrokeObservers(&event, binding.action, nil, app)
				continue replay
			}
		}

		w.dispatchKeyUpDownEvent(&event, dispatchPath, app)
		if !app.propagateEvent {
			continue replay
		}
		if replay.Keystroke.KeyChar != "" {
			if handler := fs.inputHandler; handler != nil {
				handler.DispatchInput(replay.Keystroke.KeyChar, w, app)
			}
		}
	}
	_ = tree
}

// spawnChordTimeout starts the pending-input flush timer (window.rs
// dispatch_key_event's spawned 1s timer). The scheduler's (virtual)
// clock drives it; see the file header for the real-window limitation.
func (w *Window) spawnChordTimeout(app *App, d time.Duration) *Task[struct{}] {
	task := app.Spawn(func(cx *AsyncApp) struct{} {
		(&TaskRun{t: cx.run}).Sleep(d)
		cx.Update(func(app *App) {
			fs := focusState(w)
			pending := fs.pendingInput
			fs.pendingInput = nil
			if pending == nil || pending.focus != fs.focused {
				return
			}
			dispatchPath, _ := w.focusDispatchPath()
			tree := fs.renderedDispatchTree()
			toReplay := tree.flushDispatch(pending.keystrokes, dispatchPath)
			w.pendingInputChanged(app)
			w.replayPendingInput(toReplay, app)
		})
		return struct{}{}
	})
	taskHandle := task
	return &taskHandle
}

// HasPendingKeystrokes reports whether a multi-stroke binding is in
// progress (window.rs has_pending_keystrokes).
func (w *Window) HasPendingKeystrokes() bool {
	if w == nil {
		return false
	}
	fs := focusState(w)
	return fs.pendingInput != nil && fs.pendingInput.focus == fs.focused
}

// PendingInputKeystrokes returns the pending input keystrokes that
// might complete a multi-stroke binding (window.rs
// pending_input_keystrokes).
func (w *Window) PendingInputKeystrokes() []Keystroke {
	if w == nil {
		return nil
	}
	fs := focusState(w)
	if fs.pendingInput == nil || fs.pendingInput.focus != fs.focused {
		return nil
	}
	return append([]Keystroke{}, fs.pendingInput.keystrokes...)
}

// clearPendingKeystrokes discards pending input (window.rs
// clear_pending_keystrokes; deferred observer notification).
func (w *Window) clearPendingKeystrokes() {
	fs := focusState(w)
	if fs.pendingInput != nil {
		fs.pendingInput = nil
		if w.app != nil {
			w.app.Defer(func(a *App) {
				w.pendingInputChanged(a)
			})
		}
	}
}

// pendingInputChanged notifies pending-input observers (window.rs
// pending_input_changed).
func (w *Window) pendingInputChanged(app *App) {
	fs := focusState(w)
	observers := append([]*pendingInputSubscription{}, fs.pendingInputObservers...)
	for _, observer := range observers {
		if observer.callback != nil {
			observer.callback(w, app)
		}
	}
}

// OnPendingInputChange subscribes to pending-input state changes
// (window.rs pending_input_observers; the reference registers through
// cx.observe_pending_input). The returned subscription cancels the
// observer.
func (w *Window) OnPendingInputChange(callback func(w *Window, app *App)) *pendingInputSubscription {
	if w == nil {
		panic("gpui: OnPendingInputChange requires a live window")
	}
	fs := focusState(w)
	sub := &pendingInputSubscription{owner: fs, callback: callback}
	fs.pendingInputObservers = append(fs.pendingInputObservers, sub)
	return sub
}

// pendingInputSubscription is one pending-input observer registration.
type pendingInputSubscription struct {
	owner    *windowFocusState
	callback func(w *Window, app *App)
}

// Cancel removes this observer.
func (s *pendingInputSubscription) Cancel() {
	if s == nil || s.owner == nil {
		return
	}
	for i, existing := range s.owner.pendingInputObservers {
		if existing == s {
			s.owner.pendingInputObservers = append(s.owner.pendingInputObservers[:i], s.owner.pendingInputObservers[i+1:]...)
			break
		}
	}
	s.callback = nil
	s.owner = nil
}

// ---------------------------------------------------------------------------
// Keystroke observers and interceptors (window.rs
// dispatch_keystroke_observers / _interceptors)
// ---------------------------------------------------------------------------

// KeystrokeEvent is the observer payload of a keystroke
// (window.rs KeystrokeEvent).
type KeystrokeEvent struct {
	// Keystroke is the typed keystroke.
	Keystroke Keystroke
	// Action is the action a binding selected, when one did.
	Action BoxedAction
	// ContextStack is the dispatch context stack.
	ContextStack []KeyContext
}

// keystrokeObserver is one app-level keystroke observer.
type keystrokeObserver struct {
	// interceptor runs before dispatch and can stop propagation.
	interceptor bool
	// callback is func(*KeystrokeEvent, *Window, *App).
	callback func(event *KeystrokeEvent, w *Window, app *App)
}

// Propagate re-enables event propagation for the current dispatch
// (AppContext::propagate): bubble-phase action listeners stop
// propagation by default; calling this continues it.
func (a *App) Propagate() { a.propagateEvent = true }

// StopPropagation stops event propagation for the current dispatch
// (AppContext::stop_propagation): the remaining listeners are skipped.
func (a *App) StopPropagation() { a.propagateEvent = false }

// Propagate re-enables event propagation for the current dispatch
// from a view context (the Context forwarding of App.Propagate).
func (cx *Context[S]) Propagate() { cx.app.propagateEvent = true }

// StopPropagation stops event propagation for the current dispatch
// from a view context.
func (cx *Context[S]) StopPropagation() { cx.app.propagateEvent = false }

// OnKeystroke subscribes to keystrokes after dispatch (AppContext::
// on_keystroke). Returns the cancellation handle.
func (a *App) OnKeystroke(callback func(event *KeystrokeEvent, w *Window, app *App)) *keystrokeSubscription {
	a.keystrokeObservers = append(a.keystrokeObservers, keystrokeObserver{callback: callback})
	sub := &keystrokeSubscription{app: a, index: len(a.keystrokeObservers) - 1, interceptor: false}
	return sub
}

// OnKeystrokeInterceptor subscribes to keystrokes before dispatch
// (AppContext::observe_keystrokes_interceptor... the reference
// `cx.observe_keystrokes`/interceptor pair): interceptors can stop
// propagation to suppress bindings and listeners.
func (a *App) OnKeystrokeInterceptor(callback func(event *KeystrokeEvent, w *Window, app *App)) *keystrokeSubscription {
	a.keystrokeObservers = append(a.keystrokeObservers, keystrokeObserver{callback: callback, interceptor: true})
	return &keystrokeSubscription{app: a, index: len(a.keystrokeObservers) - 1, interceptor: true}
}

// keystrokeSubscription cancels one keystroke registration.
type keystrokeSubscription struct {
	app         *App
	index       int
	interceptor bool
}

// Cancel removes this observer.
func (s *keystrokeSubscription) Cancel() {
	if s == nil || s.app == nil {
		return
	}
	observers := &s.app.keystrokeObservers
	if s.index < len(*observers) {
		entry := (*observers)[s.index]
		if (entry.interceptor && s.interceptor) || (!entry.interceptor && !s.interceptor) {
			(*observers)[s.index].callback = nil
		}
	}
	s.app = nil
}

func (w *Window) dispatchKeystrokeObservers(event any, action BoxedAction, contextStack []KeyContext, app *App) {
	keyDown, ok := event.(*KeyDownEvent)
	if !ok {
		return
	}
	payload := &KeystrokeEvent{
		Keystroke:    keyDown.Keystroke,
		ContextStack: contextStack,
	}
	if action.descriptor != nil {
		payload.Action = action
	}
	for _, observer := range append([]keystrokeObserver{}, app.keystrokeObservers...) {
		if observer.interceptor || observer.callback == nil {
			continue
		}
		observer.callback(payload, w, app)
	}
}

func (w *Window) dispatchKeystrokeInterceptors(event any, contextStack []KeyContext, app *App) {
	keyDown, ok := event.(*KeyDownEvent)
	if !ok {
		return
	}
	payload := &KeystrokeEvent{
		Keystroke:    keyDown.Keystroke,
		ContextStack: contextStack,
	}
	for _, observer := range append([]keystrokeObserver{}, app.keystrokeObservers...) {
		if !observer.interceptor || observer.callback == nil {
			continue
		}
		observer.callback(payload, w, app)
	}
}

// ---------------------------------------------------------------------------
// Action dispatch (window.rs dispatch_action / dispatch_action_on_node)
// ---------------------------------------------------------------------------

// dispatchNodeIDOfRoot returns the root node of the rendered tree.
func dispatchNodeIDOfRoot(fs *windowFocusState) DispatchNodeID {
	return fs.renderedDispatchTree().RootNodeID()
}

// globalActionListener is one app-level action listener (App's
// on_action).
type globalActionListener struct {
	// actionType is the action's canonical descriptor.
	actionType *actionDescriptor
	// capture routes the listener on the capture phase (bubble default).
	capture bool
	// callback receives the cloned payload and phase.
	callback func(action any, phase DispatchPhase, app *App)
}

// OnAction registers an app-level action listener (AppContext::
// on_action): it observes the action around window dispatch in both
// phases. Returns the cancellation handle.
func (a *App) OnAction(action AnyAction, callback func(action any, phase DispatchPhase, app *App)) *actionSubscription {
	descriptor := anyActionDescriptor(action)
	if descriptor == nil {
		panic("gpui: OnAction requires a canonical action")
	}
	a.globalActionListeners = append(a.globalActionListeners, globalActionListener{
		actionType: descriptor,
		callback:   callback,
	})
	return &actionSubscription{app: a, index: len(a.globalActionListeners) - 1}
}

// actionSubscription cancels one global action listener.
type actionSubscription struct {
	app   *App
	index int
}

// Cancel removes this listener.
func (s *actionSubscription) Cancel() {
	if s == nil || s.app == nil {
		return
	}
	if s.index < len(s.app.globalActionListeners) {
		s.app.globalActionListeners[s.index].callback = nil
	}
	s.app = nil
}

// anyActionDescriptor resolves an action value's canonical descriptor.
// The value may be an Action[A] (its actionCarrier), a BoxedAction, or
// nil.
func anyActionDescriptor(action any) *actionDescriptor {
	if action == nil {
		return nil
	}
	if carrier, ok := action.(actionCarrier); ok {
		if ref := actionRefOf(carrier); ref != nil {
			return ref
		}
	}
	if boxed, ok := action.(BoxedAction); ok {
		return boxed.descriptor
	}
	return nil
}

// actionRefOf resolves an actionCarrier's descriptor.
func actionRefOf(carrier actionCarrier) *actionDescriptor {
	return carrier.actionRef()
}

// dispatchActionOnNode dispatches an action to the node's path: global
// capture listeners, window capture listeners, window bubble listeners
// (stop-by-default), then global bubble listeners (window.rs
// dispatch_action_on_node).
func (w *Window) dispatchActionOnNode(nodeID DispatchNodeID, action BoxedAction, app *App) {
	w.dispatchActionOnNodeInner(nodeID, action, app)
}

func (w *Window) dispatchActionOnNodeInner(nodeID DispatchNodeID, action BoxedAction, app *App) {
	if action.descriptor == nil {
		return
	}
	fs := focusState(w)
	tree := fs.renderedDispatchTree()
	dispatchPath := tree.DispatchPath(nodeID)
	payload := action.Payload()

	// Capture phase for global listeners.
	app.propagateEvent = true
	for _, listener := range append([]globalActionListener{}, app.globalActionListeners...) {
		if listener.actionType == action.descriptor && listener.callback != nil {
			listener.callback(payload, DispatchCapture, app)
			if !app.propagateEvent {
				return
			}
		}
	}

	// Capture phase for window listeners.
	for _, nodePathID := range dispatchPath {
		node := tree.nodes[nodePathID.Index]
		for _, listener := range node.actionListeners {
			if listener.actionType != action.descriptor || !listener.capture {
				continue
			}
			listener.callback(payload, DispatchCapture, w, app)
			if !app.propagateEvent {
				return
			}
		}
	}

	// Bubble phase for window listeners: actions stop propagation by
	// default.
	for i := len(dispatchPath) - 1; i >= 0; i-- {
		node := tree.nodes[dispatchPath[i].Index]
		for _, listener := range node.actionListeners {
			if listener.actionType != action.descriptor || listener.capture {
				continue
			}
			app.propagateEvent = false
			listener.callback(payload, DispatchBubble, w, app)
			if !app.propagateEvent {
				return
			}
		}
	}

	// Bubble phase for global listeners.
	for _, listener := range append([]globalActionListener{}, app.globalActionListeners...) {
		if listener.actionType == action.descriptor && !listener.capture && listener.callback != nil {
			app.propagateEvent = false
			listener.callback(payload, DispatchBubble, app)
			if !app.propagateEvent {
				return
			}
		}
	}
}

// BindingsForAction returns the key bindings that invoke the action on
// the window's current context stack (window.rs bindings_for_action,
// through the dispatch tree).
func (w *Window) BindingsForAction(action BoxedAction) []KeyBinding {
	if w == nil {
		return nil
	}
	fs := focusState(w)
	return fs.renderedDispatchTree().BindingsForAction(action, fs.contextStackOfRendered())
}

// AvailableActions returns the actions listening on the focused node's
// dispatch path (window.rs available_actions).
func (w *Window) AvailableActions() []BoxedAction {
	if w == nil {
		return nil
	}
	fs := focusState(w)
	_, nodeID := w.focusDispatchPath()
	return fs.renderedDispatchTree().availableActions(nodeID, actionRegistryFor(w.app))
}

// IsActionAvailable reports whether the action has a listener on the
// focused path (window.rs is_action_available).
func (w *Window) IsActionAvailable(action BoxedAction) bool {
	if w == nil {
		return false
	}
	fs := focusState(w)
	_, nodeID := w.focusDispatchPath()
	return fs.renderedDispatchTree().isActionAvailable(action, nodeID)
}

// String renders the tree for diagnostics.
func (t *DispatchTree) String() string {
	return fmt.Sprintf("DispatchTree(%d nodes)", len(t.nodes))
}
