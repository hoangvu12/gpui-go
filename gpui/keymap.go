package gpui

// This file ports the keymap of the pinned CE reference
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/keymap/binding.rs — KeyBinding (boxed action +
//     keystrokes + optional context predicate + optional source meta +
//     optional JSON action input), match_keystrokes, load;
//   - crates/gpui/src/keymap.rs — Keymap (bindings in load order, an
//     index by action identity for bindings_for_action, disabled
//     binding indices for NoAction/Unbind, version), the
//     bindings_for_input precedence algorithm (deepest context match
//     first, then later load order; NoAction suppresses equal-or-weaker
//     sources; Unbind removes targeted bindings; pending bindings keep
//     the keymap chord state), binding_enabled, and
//     possible_next_bindings_for_input;
//   - the keymap JSON file format the reference's KeyBinding::load
//     action_input feeds from: a list of {"context": ..., "bindings":
//     [[keystrokes, action-or-null[, args]]]} entries, resolved
//     through the app's BuildAction name registry.
//
// Go adaptation: the action is the port's BoxedAction (ticket12), and
// action identity is the canonical actionDescriptor pointer.

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// KeyBinding (binding.rs KeyBinding)
// ---------------------------------------------------------------------------

// KeyBindingMetaIndex identifies the source of a key binding for
// precedence between disabling and disabled bindings
// (binding.rs KeyBindingMetaIndex). Smaller values are stronger
// sources: user keymaps override base keymaps, which override
// defaults. Bindings without a meta are treated as user bindings
// (meta 0).
type KeyBindingMetaIndex uint32

// KeyBinding is a keybinding and its associated metadata (binding.rs
// KeyBinding).
type KeyBinding struct {
	// action is the boxed action the binding dispatches. It may be
	// the builtin zed::NoAction (disable) or zed::Unbind(target).
	action BoxedAction
	// keystrokes are the binding's keystrokes in sequence order.
	keystrokes []KeybindingKeystroke
	// contextPredicate restricts the binding to matching contexts.
	contextPredicate *KeyBindingContextPredicate
	// meta is the binding's source precedence, or nil (user).
	meta *KeyBindingMetaIndex
	// actionInput is the JSON input used when building the action from
	// a keymap file, if any.
	actionInput *string
}

// NewKeyBinding constructs a keybinding from raw data, panicking on a
// parse error (the reference KeyBinding::new). The context string is
// optional (nil or empty means no predicate). Bindings map through the
// dummy keyboard mapper, like the reference constructor.
func NewKeyBinding(keystrokes string, action BoxedAction, context *string) KeyBinding {
	binding, err := LoadKeyBinding(keystrokes, action, context, false, nil, DummyKeyboardMapper{})
	if err != nil {
		panic(err)
	}
	return binding
}

// MustKeyBinding is the test spelling of NewKeyBinding with a value
// context.
func MustKeyBinding(keystrokes string, action BoxedAction, context string) KeyBinding {
	var ctx *string
	if context != "" {
		ctx = &context
	}
	return NewKeyBinding(keystrokes, action, ctx)
}

// LoadKeyBinding loads a keybinding from raw data (binding.rs load).
// keystrokes is a whitespace-separated sequence of keystroke sources;
// the action is boxed; the context predicate string is optional.
// useKeyEquivalents and the keyboard mapper feed
// KeybindingKeystroke::new_with_mapper (the port's
// MapKeyEquivalent).
func LoadKeyBinding(keystrokes string, action BoxedAction, context *string, useKeyEquivalents bool, actionInput *string, mapper KeyboardMapper) (KeyBinding, error) {
	var predicate *KeyBindingContextPredicate
	if context != nil && *context != "" {
		parsed, err := ParseKeyBindingContextPredicate(*context)
		if err != nil {
			return KeyBinding{}, err
		}
		predicate = parsed
	}

	sequence := make([]KeybindingKeystroke, 0, 2)
	for _, source := range strings.Fields(keystrokes) {
		keystroke, err := ParseKeystroke(source)
		if err != nil {
			return KeyBinding{}, err
		}
		sequence = append(sequence, mapper.MapKeyEquivalent(keystroke, useKeyEquivalents))
	}
	// The reference collects split_whitespace parts: an empty keystrokes
	// string produces a zero-keystroke binding that can never match
	// (KeyBinding::load keeps it; so does the port).

	return KeyBinding{
		action:           action,
		keystrokes:       sequence,
		contextPredicate: predicate,
		actionInput:      actionInput,
	}, nil
}

// WithMeta sets the metadata for this binding (the builder spelling of
// binding.rs with_meta).
func (b KeyBinding) WithMeta(meta KeyBindingMetaIndex) KeyBinding {
	b.meta = &meta
	return b
}

// SetMeta sets the metadata for this binding.
func (b *KeyBinding) SetMeta(meta KeyBindingMetaIndex) {
	m := meta
	b.meta = &m
}

// Action returns the action associated with this binding.
func (b KeyBinding) Action() BoxedAction { return b.action }

// Keystrokes returns the keystrokes of this binding.
func (b KeyBinding) Keystrokes() []KeybindingKeystroke { return b.keystrokes }

// Predicate returns a copy of the binding's context predicate, or nil.
func (b KeyBinding) Predicate() *KeyBindingContextPredicate {
	if b.contextPredicate == nil {
		return nil
	}
	return b.contextPredicate
}

// Meta returns the binding's source precedence, or nil (user).
func (b KeyBinding) Meta() (KeyBindingMetaIndex, bool) {
	if b.meta == nil {
		return 0, false
	}
	return *b.meta, true
}

// ActionInput returns the JSON action input used to build the binding
// from a keymap file, if any.
func (b KeyBinding) ActionInput() (string, bool) {
	if b.actionInput == nil {
		return "", false
	}
	return *b.actionInput, true
}

// MatchKeystrokes checks whether the typed keystrokes match this
// binding (binding.rs match_keystrokes): no match, a full match
// (matched, pending=false), or a chord prefix (matched, pending=true).
func (b KeyBinding) MatchKeystrokes(typed []Keystroke) (matched, pending bool) {
	if len(b.keystrokes) < len(typed) {
		return false, false
	}
	for i := range typed {
		if !typed[i].ShouldMatch(&b.keystrokes[i]) {
			return false, false
		}
	}
	return true, len(b.keystrokes) > len(typed)
}

// String renders the binding for diagnostics (the Debug shape:
// keystrokes + predicate + action name).
func (b KeyBinding) String() string {
	var bld strings.Builder
	for i, ks := range b.keystrokes {
		if i > 0 {
			bld.WriteByte(' ')
		}
		bld.WriteString(ks.Unparse())
	}
	if b.contextPredicate != nil {
		bld.WriteString(" in ")
		bld.WriteString(b.contextPredicate.String())
	}
	if b.action.descriptor != nil {
		fmt.Fprintf(&bld, " -> %s", b.action.Name())
	}
	return bld.String()
}

// ---------------------------------------------------------------------------
// Keymap (keymap.rs Keymap)
// ---------------------------------------------------------------------------

// KeymapVersion is an opaque identifier of which version of the keymap
// is currently active; it changes whenever bindings are added or
// removed.
type KeymapVersion uint64

// Keymap is a collection of key bindings for the application
// (keymap.rs Keymap). Bindings stay in load order; precedence rules
// order query results.
type Keymap struct {
	bindings               []KeyBinding
	bindingIndicesByAction map[*actionDescriptor][]int
	disabledBindingIndices []int
	version                KeymapVersion
}

// NewKeymap creates a keymap with the given bindings.
func NewKeymap(bindings []KeyBinding) *Keymap {
	k := &Keymap{}
	k.AddBindings(bindings)
	return k
}

// Version returns the keymap's current version.
func (k *Keymap) Version() KeymapVersion { return k.version }

// AddBindings adds more bindings to the keymap, in order.
func (k *Keymap) AddBindings(bindings []KeyBinding) {
	if k.bindingIndicesByAction == nil {
		k.bindingIndicesByAction = make(map[*actionDescriptor][]int)
	}
	for _, binding := range bindings {
		if binding.action.descriptor == nil {
			// A zero-value boxed action cannot be dispatched; treat it
			// as a disabled binding so it participates in precedence
			// without constructing payloads.
			k.disabledBindingIndices = append(k.disabledBindingIndices, len(k.bindings))
		} else if IsNoAction(binding.action) || IsUnbind(binding.action) {
			k.disabledBindingIndices = append(k.disabledBindingIndices, len(k.bindings))
		} else {
			id := actionIdentity(binding.action)
			k.bindingIndicesByAction[id] = append(k.bindingIndicesByAction[id], len(k.bindings))
		}
		k.bindings = append(k.bindings, binding)
	}
	k.version++
}

// Clear resets this keymap to its initial state.
func (k *Keymap) Clear() {
	k.bindings = nil
	k.bindingIndicesByAction = nil
	k.disabledBindingIndices = nil
	k.version++
}

// Bindings returns all bindings in the order they were added.
func (k *Keymap) Bindings() []KeyBinding { return k.bindings }

// actionIdentity is the action's identity for indexing: the canonical
// descriptor pointer.
func actionIdentity(action BoxedAction) *actionDescriptor {
	return action.descriptor
}

// BindingsForAction iterates the bindings for the given action in load
// order, honoring the NoAction/Unbind suppression rules of
// keymap.rs bindings_for_action: a disabled binding at a later index
// hides an earlier binding with the same keystrokes when the contexts
// are compatible and (for Unbind) the action name is targeted.
func (k *Keymap) BindingsForAction(action BoxedAction) []KeyBinding {
	if action.descriptor == nil {
		return nil
	}
	id := actionIdentity(action)
	indices := k.bindingIndicesByAction[id]
	var out []KeyBinding
	for _, ix := range indices {
		binding := k.bindings[ix]
		if !binding.action.Equal(action) {
			continue
		}
		suppressed := false
		for _, disabledIx := range k.disabledBindingIndices {
			if disabledIx <= ix {
				continue
			}
			disabled := k.bindings[disabledIx]
			if !sameBindingKeystrokes(disabled, binding) {
				continue
			}
			if IsNoAction(disabled.action) {
				if disabledBindingMatchesContext(disabled, binding) {
					suppressed = true
					break
				}
			} else if IsUnbind(disabled.action) &&
				disabledBindingMatchesContext(disabled, binding) &&
				bindingIsUnbound(disabled, binding) {
				suppressed = true
				break
			}
		}
		if !suppressed {
			out = append(out, binding)
		}
	}
	return out
}

func sameBindingKeystrokes(a, b KeyBinding) bool {
	if len(a.keystrokes) != len(b.keystrokes) {
		return false
	}
	for i := range a.keystrokes {
		if a.keystrokes[i].inner != b.keystrokes[i].inner {
			return false
		}
	}
	return true
}

func disabledBindingMatchesContext(disabled, binding KeyBinding) bool {
	switch {
	case disabled.contextPredicate == nil:
		return true
	case binding.contextPredicate == nil:
		return false
	default:
		return disabled.contextPredicate.IsSuperset(binding.contextPredicate)
	}
}

func bindingIsUnbound(disabled, binding KeyBinding) bool {
	if !sameBindingKeystrokes(disabled, binding) {
		return false
	}
	if !IsUnbind(disabled.action) {
		return false
	}
	payload, ok := disabled.action.Payload().(Unbind)
	return ok && payload.Action == binding.action.Name()
}

// AllBindingsForInput returns all bindings that fully match the input
// without checking context, in precedence order (reverse load order).
func (k *Keymap) AllBindingsForInput(input []Keystroke) []KeyBinding {
	var out []KeyBinding
	for i := len(k.bindings) - 1; i >= 0; i-- {
		binding := k.bindings[i]
		if matched, pending := binding.MatchKeystrokes(input); matched && !pending {
			out = append(out, binding)
		}
	}
	return out
}

// BindingsForInput returns the bindings that match the given input and
// context stack, and whether more bindings might match if the input
// were longer (keymap.rs bindings_for_input).
//
// Precedence: bindings matching at a deeper context position come
// first; at equal depth, bindings added later win. A NoAction binding
// suppresses out-ranked bindings from sources with equal or weaker
// precedence; Unbind removes targeted bindings; pending bindings only
// count when not shadowed by a matched binding.
func (k *Keymap) BindingsForInput(input []Keystroke, contextStack []KeyContext) (bindings []KeyBinding, pending bool) {
	var matchedBindings []matchedEntry
	type pendingEntry struct {
		ix int
	}
	var pendingBindings []pendingEntry

	for ix := len(k.bindings) - 1; ix >= 0; ix-- {
		binding := k.bindings[ix]
		depth := k.bindingEnabled(binding, contextStack)
		if depth < 0 {
			continue
		}
		matched, isPending := binding.MatchKeystrokes(input)
		if !matched {
			continue
		}
		if !isPending {
			matchedBindings = append(matchedBindings, matchedEntry{depth: depth, ix: ix})
		} else {
			pendingBindings = append(pendingBindings, pendingEntry{ix: ix})
		}
	}

	// sort by depth descending, then index descending (later wins at
	// equal depth).
	sortMatchedBindings(matchedBindings)

	var firstBindingIndex = -1
	var unboundBindings []KeyBinding
	var noActionMeta = -1
	for _, entry := range matchedBindings {
		binding := k.bindings[entry.ix]
		meta := 0
		if m, ok := binding.Meta(); ok {
			meta = int(m)
		}
		if IsNoAction(binding.action) {
			if noActionMeta < 0 || meta < noActionMeta {
				noActionMeta = meta
			}
			continue
		}
		if noActionMeta >= 0 && meta >= noActionMeta {
			continue
		}
		if IsUnbind(binding.action) {
			unboundBindings = append(unboundBindings, binding)
			continue
		}
		if anyBindingUnbound(unboundBindings, binding) {
			continue
		}
		bindings = append(bindings, binding)
		if firstBindingIndex < 0 {
			firstBindingIndex = entry.ix
		}
	}

	pendingSet := map[string]bool{}
	for i := len(pendingBindings) - 1; i >= 0; i-- {
		ix := pendingBindings[i].ix
		binding := k.bindings[ix]
		if firstBindingIndex >= 0 && firstBindingIndex > ix {
			continue
		}
		if IsNoAction(binding.action) || IsUnbind(binding.action) {
			delete(pendingSet, keystrokeListKey(binding.keystrokes))
			continue
		}
		pendingSet[keystrokeListKey(binding.keystrokes)] = true
	}
	pending = len(pendingSet) > 0
	return bindings, pending
}

// matchedEntry orders one matched binding for the precedence sort.
type matchedEntry struct {
	depth int
	ix    int
}

func sortMatchedBindings(entries []matchedEntry) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 &&
			(entries[j-1].depth < entries[j].depth ||
				(entries[j-1].depth == entries[j].depth && entries[j-1].ix < entries[j].ix)); j-- {
			entries[j-1], entries[j] = entries[j], entries[j-1]
		}
	}
}

func anyBindingUnbound(unbound []KeyBinding, binding KeyBinding) bool {
	for _, disabled := range unbound {
		if bindingIsUnbound(disabled, binding) {
			return true
		}
	}
	return false
}

func keystrokeListKey(keystrokes []KeybindingKeystroke) string {
	var b strings.Builder
	for i, ks := range keystrokes {
		if i > 0 {
			b.WriteByte('\x00')
		}
		fmt.Fprintf(&b, "%s\x01%t,%t,%t,%t,%t\x02%s",
			ks.inner.Key,
			ks.inner.Modifiers.Control, ks.inner.Modifiers.Alt,
			ks.inner.Modifiers.Shift, ks.inner.Modifiers.Platform,
			ks.inner.Modifiers.Function, ks.inner.KeyChar)
	}
	return b.String()
}

// bindingEnabled checks whether the binding is enabled for the context
// stack, returning the deepest depth at which it matches or -1
// (keymap.rs binding_enabled: a binding without a predicate is enabled
// at the stack depth).
func (k *Keymap) bindingEnabled(binding KeyBinding, contexts []KeyContext) int {
	if binding.contextPredicate != nil {
		return binding.contextPredicate.DepthOf(contexts)
	}
	return len(contexts)
}

// PossibleNextBindingsForInput finds the bindings that can follow the
// current input sequence (keymap.rs possible_next_bindings_for_input):
// pending (chord-prefix) matches in precedence order, without NoAction
// or Unbind entries.
func (k *Keymap) PossibleNextBindingsForInput(input []Keystroke, contextStack []KeyContext) []KeyBinding {
	var entries []matchedEntry
	for ix := len(k.bindings) - 1; ix >= 0; ix-- {
		binding := k.bindings[ix]
		depth := k.bindingEnabled(binding, contextStack)
		if depth < 0 {
			continue
		}
		matched, isPending := binding.MatchKeystrokes(input)
		if !matched {
			continue
		}
		if !isPending || IsNoAction(binding.action) || IsUnbind(binding.action) {
			continue
		}
		entries = append(entries, matchedEntry{depth: depth, ix: ix})
	}
	sortMatchedBindings(entries)
	out := make([]KeyBinding, 0, len(entries))
	for _, e := range entries {
		out = append(out, k.bindings[e.ix])
	}
	return out
}

// ---------------------------------------------------------------------------
// Keymap JSON (the keymap file format)
// ---------------------------------------------------------------------------

// keymapFileEntry is one entry of a keymap JSON file: a context
// predicate and the bindings under it.
type keymapFileEntry struct {
	Context  *string           `json:"context"`
	Bindings []json.RawMessage `json:"bindings"`
}

// ParseKeymapJSON parses keymap JSON into bindings. The format is the
// reference's keymap-file shape: a JSON array of entries, each with an
// optional "context" and a "bindings" array of [keystrokes, action]
// pairs (or [keystrokes, action, args] triples; a null action builds
// the builtin zed::NoAction so "x": null disables bindings, and a
// string action payload builds zed::Unbind targets).
//
// resolve builds the boxed action for a name + optional JSON input; it
// is the app's BuildAction surface (ticket12's named registry with its
// NotFound/NoJSON/Decode failure categories).
func ParseKeymapJSON(data []byte, resolve func(name string, input json.RawMessage) (BoxedAction, error), mapper KeyboardMapper) ([]KeyBinding, error) {
	var entries []keymapFileEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("gpui: keymap json: %w", err)
	}
	var bindings []KeyBinding
	for _, entry := range entries {
		for _, raw := range entry.Bindings {
			var parts []json.RawMessage
			if err := json.Unmarshal(raw, &parts); err != nil {
				return nil, fmt.Errorf("gpui: keymap json: a binding must be an array: %w", err)
			}
			if len(parts) < 2 || len(parts) > 3 {
				return nil, fmt.Errorf("gpui: keymap json: a binding must be [keystrokes, action[, args]]")
			}
			var keystrokes string
			if err := json.Unmarshal(parts[0], &keystrokes); err != nil {
				return nil, fmt.Errorf("gpui: keymap json: keystrokes must be a string: %w", err)
			}

			var actionInput *string
			if len(parts) == 3 {
				if string(parts[2]) != "null" {
					text := string(parts[2])
					actionInput = &text
				}
			}

			var action BoxedAction
			if string(parts[1]) == "null" {
				// "keystrokes": null disables the binding (NoAction).
				action = NoActionDescriptor.Box(NoAction{})
			} else {
				var name string
				if err := json.Unmarshal(parts[1], &name); err != nil {
					return nil, fmt.Errorf("gpui: keymap json: the action must be a name string or null: %w", err)
				}
				var input json.RawMessage
				if actionInput != nil {
					input = json.RawMessage(*actionInput)
				}
				built, err := resolve(name, input)
				if err != nil {
					return nil, fmt.Errorf("gpui: keymap json: action %q: %w", name, err)
				}
				action = built
			}

			binding, err := LoadKeyBinding(keystrokes, action, entry.Context, false, actionInput, mapper)
			if err != nil {
				return nil, fmt.Errorf("gpui: keymap json: %w", err)
			}
			bindings = append(bindings, binding)
		}
	}
	return bindings, nil
}
