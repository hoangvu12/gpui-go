// This file implements the typed action registry of the Go port:
// canonical once-per-type action descriptors, explicit named
// setup-time registration, JSON construction with distinct failure
// categories, boxed actions that preserve cloned bound payloads, and
// the builtin zed::NoAction / zed::Unbind actions.
//
// The pinned reference is reference/ce-source/crates/gpui/src/action.rs
// (the Action trait, ActionRegistry, ActionBuildError and the
// no_action module), the derive macro in
// reference/ce-source/crates/gpui_macros/src/derive_action.rs (unit
// versus rich build behavior, no_json, aliases) and the keymap
// semantics in reference/ce-source/crates/gpui/src/keymap.rs.
//
// Deliberate Go adaptations, recorded beside the behavior they alter:
//
//   - The reference registers actions through an inventory collected at
//     link time, so definition and named registration are one step. Go
//     has no link-time inventory; canonical definitions are
//     process-global and keyed by payload type, while named
//     registration is explicit, per application, and only enables
//     construction by name ("registration only allows for actions to be
//     built dynamically, and is unrelated to binding actions in the
//     element tree", app.rs).
//
//   - The reference's ActionBuildError has two categories (NotFound,
//     BuildError). The selected authoring contract requires NoJSON
//     ("JSON-disabled") to be distinguishable from decode failure, so
//     the port carries three: NotFound, NoJSON and Decode.
//
//   - The reference panics on duplicate name registration. RegisterActions
//     and the define functions return no error in the compile-verified
//     signature fixture, so the port panics with typed error values that
//     callers can recover and inspect.

package gpui

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sync"
)

// actionKind seals an Action to its payload type: values of different
// payload types cannot be converted into each other, mirroring the
// Rust type identity behind dyn Action as closely as Go allows (the
// same device the Event pair uses).
type actionKind[A any] struct{}

// Action is the canonical typed descriptor of an action whose payload is
// A. One descriptor exists per payload type per process; the canonical
// declaration is created with DefineAction (rich payloads) or
// DefineUnitAction (empty-struct payloads) and imported or copied for
// reuse elsewhere.
//
// The descriptor is immutable and its services do not depend on
// transient application state. Its zero value is invalid: methods that
// need the descriptor panic when called on it.
type Action[A any] struct {
	kind       actionKind[A]
	descriptor *actionDescriptor
}

// ActionSpec describes a rich action payload: its full external name,
// the required Clone and Equal services, the optional JSON and schema
// services, aliases and metadata. Rich descriptors require explicit
// Clone and Equal because Go cannot synthesize correct deep clones of
// user types; the derive macro in the reference enforces the same
// requirement through trait bounds.
type ActionSpec[A any] struct {
	Name     string
	Clone    func(A) A
	Equal    func(A, A) bool
	FromJSON func(json.RawMessage) (A, error)
	Schema   func() any
	Aliases  []string

	// Deprecation, when non-empty, is the deprecation message for the
	// action ("deprecated = \"...\"" in the reference derive).
	Deprecation string
	// Documentation is the action's documentation (doc comments in the
	// reference derive).
	Documentation string
}

// AnyAction erases the payload type of an Action. The interface is
// sealed by the unexported method name: only types declared in this
// package can implement it.
type AnyAction interface {
	actionDescriptor()
}

// actionCarrier is the internal companion of AnyAction used to recover
// the canonical descriptor behind an erased action. It is the same
// pattern the sealed AppContext uses with appCarrier.
type actionCarrier interface {
	actionDescriptor()
	actionRef() *actionDescriptor
}

func (a Action[A]) actionDescriptor() {}

func (a Action[A]) actionRef() *actionDescriptor { return a.descriptor }

// actionDescriptor is the shared, type-erased canonical record behind
// every Action[A] of one payload type. All functions are stored erased
// (any-typed) with the payload type baked in at definition time.
type actionDescriptor struct {
	payloadType reflect.Type

	name          string
	aliases       []string
	deprecation   string
	documentation string

	// unit records that the descriptor was created by DefineUnitAction
	// with default services, so a later rich definition of the same type
	// is a redefinition rather than an idempotent repeat.
	unit bool

	clone    func(value any) any
	equal    func(x, y any) bool
	fromJSON func(data json.RawMessage) (any, error)
	schema   func() any
}

// mustDescriptor panics with a misuse diagnostic when an Action method
// is used on a zero-value action. The contract makes the zero value
// invalid; misusing it is programmer error, like the nil-context panics
// of the entity and event paths.
func (a Action[A]) mustDescriptor(op string) *actionDescriptor {
	if a.descriptor == nil {
		panic(fmt.Sprintf("gpui: %s called on a zero-value Action[%s]; define it with DefineAction or DefineUnitAction first", op, reflect.TypeFor[A]()))
	}
	return a.descriptor
}

// mustDescriptor is the BoxedAction counterpart of the Action check.
func (b BoxedAction) mustDescriptor(op string) *actionDescriptor {
	if b.descriptor == nil {
		panic(fmt.Sprintf("gpui: %s called on a zero-value BoxedAction; box an action with Action[A].Box or App.BuildAction first", op))
	}
	return b.descriptor
}

// ---------------------------------------------------------------------------
// Typed error categories
// ---------------------------------------------------------------------------

// ActionDefinitionKind classifies a rejected canonical definition.
type ActionDefinitionKind int

const (
	// ActionDefinitionMissingClone: a rich definition without an
	// explicit Clone function.
	ActionDefinitionMissingClone ActionDefinitionKind = iota
	// ActionDefinitionMissingEqual: a rich definition without an
	// explicit Equal function.
	ActionDefinitionMissingEqual
	// ActionDefinitionEmptyName: an empty action name.
	ActionDefinitionEmptyName
	// ActionDefinitionBadAlias: an alias that is empty, equal to the
	// canonical name, or duplicated within the same spec.
	ActionDefinitionBadAlias
	// ActionDefinitionNonEmptyUnitPayload: DefineUnitAction called for a
	// payload with non-zero size. The ~struct{} constraint already
	// rejects non-empty structs at compile time; this check is the
	// defensive runtime guard required by the ticket.
	ActionDefinitionNonEmptyUnitPayload
	// ActionDefinitionRedefinition: a second, different definition for a
	// payload type that already has a canonical descriptor.
	ActionDefinitionRedefinition
)

// ActionDefinitionError reports a rejected canonical definition. It is
// panicked by DefineAction and DefineUnitAction, which return no error
// in the compile-verified signature fixture.
type ActionDefinitionError struct {
	Kind        ActionDefinitionKind
	PayloadType string
	// Name is the canonical name of the definition (the existing name
	// for a redefinition).
	Name string
	// Other is the second name of a redefinition, or the offending alias
	// of a BadAlias diagnostic.
	Other  string
	Detail string
}

func (e *ActionDefinitionError) Error() string {
	switch e.Kind {
	case ActionDefinitionMissingClone:
		return fmt.Sprintf("gpui: DefineAction for %s (%q) is missing Clone; rich actions require explicit Clone and Equal", e.PayloadType, e.Name)
	case ActionDefinitionMissingEqual:
		return fmt.Sprintf("gpui: DefineAction for %s (%q) is missing Equal; rich actions require explicit Clone and Equal", e.PayloadType, e.Name)
	case ActionDefinitionEmptyName:
		return fmt.Sprintf("gpui: DefineAction for %s used an empty action name", e.PayloadType)
	case ActionDefinitionBadAlias:
		return fmt.Sprintf("gpui: action %q has an invalid alias %q: %s", e.Name, e.Other, e.Detail)
	case ActionDefinitionNonEmptyUnitPayload:
		return fmt.Sprintf("gpui: DefineUnitAction requires an empty struct payload; %s has size %s bytes", e.PayloadType, e.Detail)
	default:
		return fmt.Sprintf("gpui: payload type %s is already canonically defined as action %q; attempted redefinition as %q (%s)", e.PayloadType, e.Name, e.Other, e.Detail)
	}
}

// ActionRegistrationKind classifies a rejected named registration.
type ActionRegistrationKind int

const (
	// ActionRegistrationInvalidAction: a nil or zero-value action passed
	// to RegisterActions.
	ActionRegistrationInvalidAction ActionRegistrationKind = iota
	// ActionRegistrationNameConflict: a canonical name already
	// registered by a different action.
	ActionRegistrationNameConflict
	// ActionRegistrationAliasConflict: an alias already registered by a
	// different action.
	ActionRegistrationAliasConflict
)

// ActionRegistrationError reports a setup-time registration conflict.
// It is panicked by RegisterActions, mirroring the reference panic on
// duplicate name registration ("Action with name `{name}` already
// registered", action.rs insert_action).
type ActionRegistrationError struct {
	Kind ActionRegistrationKind
	// Name is the canonical name of the action being registered.
	Name string
	// Conflict is the colliding name (the canonical name for a name
	// conflict, the alias for an alias conflict).
	Conflict string
	// Existing is the canonical name of the already-registered action.
	Existing string
}

func (e *ActionRegistrationError) Error() string {
	switch e.Kind {
	case ActionRegistrationInvalidAction:
		return "gpui: RegisterActions called with a nil or zero-value action"
	case ActionRegistrationNameConflict:
		return fmt.Sprintf("gpui: action with name %q is already registered by action %q; conflicting names are setup errors", e.Conflict, e.Existing)
	default:
		return fmt.Sprintf("gpui: action with name %q is already registered by action %q; %q is a deprecated alias of %q and must not collide with a registered action", e.Conflict, e.Existing, e.Conflict, e.Name)
	}
}

// ActionBuildKind classifies a BuildAction failure.
type ActionBuildKind int

const (
	// ActionBuildNotFound: no action with the requested name is
	// registered in this application (the reference's NotFound).
	ActionBuildNotFound ActionBuildKind = iota
	// ActionBuildNoJSON: the action is registered but JSON-disabled (the
	// reference's no_json derive flag, split into its own category by
	// the selected authoring contract).
	ActionBuildNoJSON
	// ActionBuildDecode: the action's FromJSON rejected the payload (the
	// reference's BuildError deserialization failure).
	ActionBuildDecode
)

// ActionBuildError reports a BuildAction failure with the three
// categories the selected authoring contract requires: unknown name,
// disabled JSON and decode failure. The Display wording follows the
// reference's ActionBuildError.
type ActionBuildError struct {
	Kind ActionBuildKind
	Name string
	// Err is the underlying decode error for ActionBuildDecode.
	Err error
}

func (e *ActionBuildError) Error() string {
	switch e.Kind {
	case ActionBuildNotFound:
		return fmt.Sprintf("Didn't find an action named %q", e.Name)
	case ActionBuildNoJSON:
		return fmt.Sprintf("Error while building action %q: %s cannot be built from JSON", e.Name, e.Name)
	default:
		return fmt.Sprintf("Error while building action %q: %v", e.Name, e.Err)
	}
}

func (e *ActionBuildError) Unwrap() error { return e.Err }

// ActionTypeError reports a BoxedAction payload asserted to the wrong
// concrete type. It mirrors the reference's downcast failure mode
// (partial_eq returning false on a type mismatch) with the typed error
// the contract requires for wrong-type unboxing.
type ActionTypeError struct {
	// Action is the boxed action's name.
	Action string
	// PayloadType is the concrete type of the boxed payload.
	PayloadType string
	// ExpectedType is the asserted type.
	ExpectedType string
}

func (e *ActionTypeError) Error() string {
	return fmt.Sprintf("gpui: action %q carries a payload of type %s, not %s", e.Action, e.PayloadType, e.ExpectedType)
}

// ActionDispatchError reports an action dispatch that could not be
// routed. Real dispatch routes through the window focus tree
// (window.rs dispatch_action walks focus nodes and their element action
// listeners); windows and focus arrive with later tickets, so the
// dispatch entry points on the Window placeholder validate and clone
// the action, then fail with this typed error instead of pretending to
// deliver.
//
// Action carries the validated, cloned action the dispatch prepared,
// so callers can inspect what would have been routed.
type ActionDispatchError struct {
	Action BoxedAction
	Reason string
}

func (e *ActionDispatchError) Error() string {
	name := "<invalid>"
	if e.Action.descriptor != nil {
		name = e.Action.descriptor.name
	}
	return fmt.Sprintf("gpui: cannot dispatch action %q: %s", name, e.Reason)
}

// ---------------------------------------------------------------------------
// Canonical once-per-type registry
// ---------------------------------------------------------------------------

// actionState is the process-global canonical registry plus the
// per-application named registries. Canonical definitions are keyed by
// payload type; named registration is keyed by *App and lives for the
// process, because application teardown (and with it any registry
// release) arrives with the shutdown ticket.
type actionState struct {
	mu     sync.Mutex
	byType map[reflect.Type]*actionDescriptor
	byApp  map[*App]*appActionRegistry
}

var actions = actionState{
	byType: map[reflect.Type]*actionDescriptor{},
	byApp:  map[*App]*appActionRegistry{},
}

// sameActionDefinition decides whether two descriptors are the same
// canonical declaration. Function bodies cannot be compared in Go, so
// equality is decided on the comparable fields (name, aliases,
// deprecation, documentation, unit origin) and capability presence
// (whether FromJSON and Schema exist). Two specs that differ only in
// Clone/Equal function bodies cannot be distinguished.
//
// The authoring contract states "every second DefineAction[A] is a
// definition error, even if the name matches ... This prevents
// inconsistent clone/equality functions without trying to compare
// functions". Ticket 12 instructs the softer rule: "same type
// redefinition with equal spec = idempotent; different spec = typed
// error/panic with both names". The port implements the ticket's rule
// and records the deviation: an exactly-matching declaration is
// accepted as the same canonical definition, while any difference in
// an observable field or capability is the typed redefinition error.
func sameActionDefinition(a, b *actionDescriptor) bool {
	return a.name == b.name &&
		a.unit == b.unit &&
		a.deprecation == b.deprecation &&
		a.documentation == b.documentation &&
		slices.Equal(a.aliases, b.aliases) &&
		(a.fromJSON == nil) == (b.fromJSON == nil) &&
		(a.schema == nil) == (b.schema == nil)
}

// defineCanonical validates spec, registers the descriptor for A and
// returns the canonical descriptor. A second definition that matches
// the canonical one returns the existing descriptor (idempotent); a
// different one panics with *ActionDefinitionError.
func defineCanonical[A any](spec ActionSpec[A], unit bool) *actionDescriptor {
	payloadType := reflect.TypeFor[A]()

	if !unit {
		if spec.Clone == nil {
			panic(&ActionDefinitionError{Kind: ActionDefinitionMissingClone, PayloadType: payloadType.String(), Name: spec.Name})
		}
		if spec.Equal == nil {
			panic(&ActionDefinitionError{Kind: ActionDefinitionMissingEqual, PayloadType: payloadType.String(), Name: spec.Name})
		}
	}
	if spec.Name == "" {
		panic(&ActionDefinitionError{Kind: ActionDefinitionEmptyName, PayloadType: payloadType.String()})
	}
	seen := make(map[string]bool, len(spec.Aliases))
	for _, alias := range spec.Aliases {
		switch {
		case alias == "":
			panic(&ActionDefinitionError{Kind: ActionDefinitionBadAlias, PayloadType: payloadType.String(), Name: spec.Name, Other: alias, Detail: "aliases must not be empty"})
		case alias == spec.Name:
			panic(&ActionDefinitionError{Kind: ActionDefinitionBadAlias, PayloadType: payloadType.String(), Name: spec.Name, Other: alias, Detail: "an alias must not equal the canonical name"})
		case seen[alias]:
			panic(&ActionDefinitionError{Kind: ActionDefinitionBadAlias, PayloadType: payloadType.String(), Name: spec.Name, Other: alias, Detail: "duplicate alias"})
		}
		seen[alias] = true
	}

	d := &actionDescriptor{
		payloadType:   payloadType,
		name:          spec.Name,
		aliases:       slices.Clone(spec.Aliases),
		deprecation:   spec.Deprecation,
		documentation: spec.Documentation,
		unit:          unit,
	}
	if spec.Clone != nil {
		clone := spec.Clone
		d.clone = func(value any) any { return clone(value.(A)) }
	}
	if spec.Equal != nil {
		equal := spec.Equal
		d.equal = func(x, y any) bool { return equal(x.(A), y.(A)) }
	}
	if spec.FromJSON != nil {
		fromJSON := spec.FromJSON
		d.fromJSON = func(data json.RawMessage) (any, error) { return fromJSON(data) }
	}
	if spec.Schema != nil {
		d.schema = spec.Schema
	}

	actions.mu.Lock()
	defer actions.mu.Unlock()
	existing := actions.byType[payloadType]
	switch {
	case existing == nil:
		actions.byType[payloadType] = d
		return d
	case sameActionDefinition(existing, d):
		// Idempotent repeat of the canonical declaration: the existing
		// descriptor stays canonical.
		return existing
	default:
		panic(&ActionDefinitionError{
			Kind:        ActionDefinitionRedefinition,
			PayloadType: payloadType.String(),
			Name:        existing.name,
			Other:       d.name,
			Detail:      "import or copy the canonical declaration instead of redefining the payload type",
		})
	}
}

// DefineAction defines the canonical action descriptor for the rich
// payload type A. Rich descriptors require an explicit Clone and Equal:
// Go cannot synthesize a correct clone of a user type, and inconsistent
// clone/equality functions are exactly what the once-per-type rule
// prevents. FromJSON and Schema are optional: a definition without
// FromJSON is JSON-disabled and BuildAction reports the distinct NoJSON
// error for it.
//
// A second definition for the same payload type is a typed
// *ActionDefinitionError panic when it differs from the canonical one
// (including its name), and idempotent when it matches the canonical
// declaration exactly; see sameActionDefinition for the recorded
// contract deviation.
func DefineAction[A any](spec ActionSpec[A]) Action[A] {
	return Action[A]{kind: actionKind[A]{}, descriptor: defineCanonical(spec, false)}
}

// DefineUnitAction defines the canonical action descriptor for an
// empty-struct payload A with default services: Clone and Equal are
// trivial for a zero-size struct, the payload ignores JSON input (the
// reference's unit-struct build is `Ok(Box::new(Self))` regardless of
// the value), and no schema is exposed (unit structs return None in the
// reference derive). The name is the full external name, including any
// namespace ("counter::Increment").
//
// The ~struct{} constraint rejects non-empty struct payloads at compile
// time ("Payload does not satisfy ~struct{}"). DefineUnitAction
// additionally checks payload size at runtime as a defensive guard;
// with the constraint in place the check cannot trigger, and it exists
// to document and preserve the invariant if the constraint is ever
// widened.
func DefineUnitAction[A ~struct{}](name string) Action[A] {
	payloadType := reflect.TypeFor[A]()
	if size := payloadType.Size(); size != 0 {
		panic(&ActionDefinitionError{
			Kind:        ActionDefinitionNonEmptyUnitPayload,
			PayloadType: payloadType.String(),
			Detail:      fmt.Sprintf("%d", size),
		})
	}
	return Action[A]{kind: actionKind[A]{}, descriptor: defineCanonical(unitSpec[A](name), true)}
}

// unitSpec is the default service set DefineUnitAction installs: a unit
// payload has no state to copy, so the value copy is the clone, any two
// values are equal, and any JSON input builds the zero value.
func unitSpec[A ~struct{}](name string) ActionSpec[A] {
	return ActionSpec[A]{
		Name:  name,
		Clone: func(value A) A { return value },
		Equal: func(a, b A) bool { return a == b },
		FromJSON: func(json.RawMessage) (A, error) {
			var zero A
			return zero, nil
		},
	}
}

// ---------------------------------------------------------------------------
// Action services
// ---------------------------------------------------------------------------

// Name returns the full external name of the action.
func (a Action[A]) Name() string { return a.mustDescriptor("Name").name }

// Aliases returns a copy of the action's deprecated aliases. These
// names resolve to the action in BuildAction but must not be
// registered by any other action.
func (a Action[A]) Aliases() []string { return slices.Clone(a.mustDescriptor("Aliases").aliases) }

// Deprecation returns the action's deprecation message, or "" when the
// action is not deprecated.
func (a Action[A]) Deprecation() string { return a.mustDescriptor("Deprecation").deprecation }

// Documentation returns the action's documentation, or "" when none
// was recorded.
func (a Action[A]) Documentation() string {
	return a.mustDescriptor("Documentation").documentation
}

// Schema returns the action's JSON schema as a plain Go value, or nil
// when the action exposes none. No schema dependency is selected: the
// port uses only the standard library encoding/json, and the value's
// shape is the action author's choice.
func (a Action[A]) Schema() any {
	d := a.mustDescriptor("Schema")
	if d.schema == nil {
		return nil
	}
	return d.schema()
}

// Clone clones a payload value through the canonical Clone service.
func (a Action[A]) Clone(value A) A {
	d := a.mustDescriptor("Clone")
	return d.clone(value).(A)
}

// Equal compares two payload values through the canonical Equal
// service.
func (a Action[A]) Equal(x, y A) bool {
	d := a.mustDescriptor("Equal")
	return d.equal(x, y)
}

// Box captures value in a BoxedAction for binding. The value is cloned
// at capture through the canonical Clone service, so the boxed action
// owns its payload ("a.Box(value) uses its clone service for owned
// storage", authoring contract). The stored payload is reachable only
// through cloning accessors, keeping it read-only by contract.
func (a Action[A]) Box(value A) BoxedAction {
	d := a.mustDescriptor("Box")
	return BoxedAction{descriptor: d, payload: d.clone(value)}
}

// ---------------------------------------------------------------------------
// BoxedAction
// ---------------------------------------------------------------------------

// BoxedAction erases an action descriptor plus its captured payload.
// The payload was cloned at capture (Action[A].Box or BuildAction) and
// every read returns a fresh clone, so boxed payloads are read-only by
// contract: handlers cannot mutate the captured value through any
// accessor.
//
// Typed dispatch recovers the concrete payload with Unbox.
type BoxedAction struct {
	descriptor *actionDescriptor
	payload    any
}

// Name returns the full external name of the boxed action.
func (b BoxedAction) Name() string { return b.mustDescriptor("Name").name }

// Clone produces a new boxed action with a re-cloned payload. Cloning
// at delivery keeps each recipient independent of the captured value.
func (b BoxedAction) Clone() BoxedAction {
	d := b.mustDescriptor("Clone")
	return BoxedAction{descriptor: d, payload: d.clone(b.payload)}
}

// Equal reports whether both boxed actions carry the same canonical
// action and equal payloads. Actions of different payload types are
// never equal, mirroring the reference partial_eq downcast-then-compare
// behavior; a zero-value argument is invalid and panics.
func (b BoxedAction) Equal(other BoxedAction) bool {
	d := b.mustDescriptor("Equal")
	if other.descriptor == nil {
		panic("gpui: Equal called with a zero-value BoxedAction")
	}
	if other.descriptor != d {
		return false
	}
	return d.equal(b.payload, other.payload)
}

// Payload returns a clone of the captured payload as any. It is the
// untyped accessor; Unbox is the typed one. Every call returns a fresh
// clone, so the captured payload stays read-only by contract.
func (b BoxedAction) Payload() any {
	d := b.mustDescriptor("Payload")
	return d.clone(b.payload)
}

// Unbox asserts the boxed payload back to its concrete type for typed
// dispatch, returning a fresh clone. A payload of a different type is
// a typed *ActionTypeError, the wrong-type failure category of the
// boxed path.
func Unbox[A any](b BoxedAction) (A, error) {
	d := b.mustDescriptor("Unbox")
	var zero A
	expected := reflect.TypeFor[A]()
	if d.payloadType != expected {
		return zero, &ActionTypeError{
			Action:       d.name,
			PayloadType:  d.payloadType.String(),
			ExpectedType: expected.String(),
		}
	}
	return d.clone(b.payload).(A), nil
}

// ---------------------------------------------------------------------------
// Builtin actions: NoAction and Unbind
// ---------------------------------------------------------------------------

// NoAction is the payload of the builtin zed::NoAction unit action.
//
// The reference documents it as: "Action with special handling which
// unbinds the keybinding this is associated with, if it is the highest
// precedence match." In the keymap a NoAction binding is a disabled
// binding: bindings_for_input records the strongest (smallest) meta of
// a matched NoAction and then skips every binding whose meta is equal
// or weaker ("A `NoAction` binding suppresses out-ranked bindings from
// sources with equal or weaker precedence, while bindings from
// stronger sources ... still apply", keymap.rs), and the reverse
// lookup bindings_for_action drops a binding when a later NoAction
// binding with matching keystrokes has a matching-or-superset context.
// The keymap-level suppression rules belong to the keymap ticket; this
// slice provides the action, its name, and its type predicate.
type NoAction struct{}

// Unbind is the payload of the builtin zed::Unbind action.
//
// The reference documents it as: "Action with special handling which
// unbinds later bindings for the same keystrokes when they dispatch the
// named action, regardless of that action's context. In keymap JSON
// this is written as: [`"zed::Unbind"`, `"editor::NewLine"`]". A
// binding whose action is Unbind(target) disables later bindings with
// the same keystrokes whose action name equals target, regardless of
// context: binding_is_unbound compares keystrokes for equality and the
// payload string with binding.action.name(), and the dispatch walk
// only honors bindings at an index earlier than the Unbind binding
// (keymap.rs bindings_for_action and bindings_for_input). The keymap
// wiring belongs to the keymap ticket.
type Unbind struct {
	// Action is the name of the action whose later bindings for the same
	// keystrokes are unbound.
	Action string
}

// NoActionDescriptor is the canonical descriptor of the builtin
// zed::NoAction unit action.
var NoActionDescriptor = DefineUnitAction[NoAction]("zed::NoAction")

// UnbindDescriptor is the canonical descriptor of the builtin
// zed::Unbind action.
var UnbindDescriptor = DefineAction[Unbind](ActionSpec[Unbind]{
	Name: "zed::Unbind",
	Clone: func(value Unbind) Unbind {
		return Unbind{Action: value.Action}
	},
	Equal: func(a, b Unbind) bool { return a.Action == b.Action },
	// The reference derives Deserialize on the one-field tuple struct,
	// which serde forwards to the inner string: the keymap payload is a
	// bare JSON string.
	FromJSON: func(data json.RawMessage) (Unbind, error) {
		var target string
		if err := json.Unmarshal(data, &target); err != nil {
			return Unbind{}, err
		}
		return Unbind{Action: target}, nil
	},
	// schemars derives a string schema for the one-field struct; the
	// port records the equivalent shape as a plain value (no schema
	// dependency is selected, see the Schema method).
	Schema: func() any { return map[string]any{"type": "string"} },
	Documentation: "Action with special handling which unbinds later bindings for the same " +
		"keystrokes when they dispatch the named action, regardless of that action's context.",
})

// IsNoAction reports whether the boxed action is the builtin
// zed::NoAction (is_no_action in the reference: a type check on the
// payload). A zero-value boxed action is not an action of any kind.
func IsNoAction(action BoxedAction) bool {
	return action.descriptor != nil && action.descriptor == NoActionDescriptor.descriptor
}

// IsUnbind reports whether the boxed action is the builtin
// zed::Unbind (is_unbind in the reference: a type check on the
// payload).
func IsUnbind(action BoxedAction) bool {
	return action.descriptor != nil && action.descriptor == UnbindDescriptor.descriptor
}

// ---------------------------------------------------------------------------
// Per-application named registration
// ---------------------------------------------------------------------------

// appActionRegistry is the named registration of one application: the
// names (canonical and alias) BuildAction resolves. Registration is
// separate from canonical definitions and only enables construction by
// name; JSON-disabled and unregistered remain independent choices.
type appActionRegistry struct {
	byName map[string]*actionDescriptor
	// names records names in registration order. The reference exposes
	// all_action_names for schema generation and validation; the
	// keymap/schema ticket consumes this order when that surface lands.
	names []string
}

// newAppActionRegistry seeds the builtin actions, mirroring the
// reference registry that loads every inventory-collected action into
// each App. User actions are registered explicitly.
func newAppActionRegistry() *appActionRegistry {
	r := &appActionRegistry{byName: map[string]*actionDescriptor{}}
	for _, d := range []*actionDescriptor{NoActionDescriptor.descriptor, UnbindDescriptor.descriptor} {
		if err := r.insert(d); err != nil {
			panic(err) // unreachable: the builtins cannot conflict
		}
	}
	return r
}

// insert adds a descriptor under its canonical name and aliases,
// accepting the same canonical descriptor idempotently. A name or
// alias held by a different action is the typed setup error.
func (r *appActionRegistry) insert(d *actionDescriptor) *ActionRegistrationError {
	if existing, ok := r.byName[d.name]; ok && existing != d {
		return &ActionRegistrationError{
			Kind:     ActionRegistrationNameConflict,
			Name:     d.name,
			Conflict: d.name,
			Existing: existing.name,
		}
	}
	for _, alias := range d.aliases {
		if existing, ok := r.byName[alias]; ok && existing != d {
			return &ActionRegistrationError{
				Kind:     ActionRegistrationAliasConflict,
				Name:     d.name,
				Conflict: alias,
				Existing: existing.name,
			}
		}
	}
	if _, ok := r.byName[d.name]; !ok {
		r.byName[d.name] = d
		r.names = append(r.names, d.name)
	}
	for _, alias := range d.aliases {
		if _, ok := r.byName[alias]; !ok {
			r.byName[alias] = d
			r.names = append(r.names, alias)
		}
	}
	return nil
}

// actionRegistryFor returns (creating on first use) the named
// registration of an application. The builtin actions are seeded into
// every application's registry.
func actionRegistryFor(a *App) *appActionRegistry {
	r := actions.byApp[a]
	if r == nil {
		r = newAppActionRegistry()
		actions.byApp[a] = r
	}
	return r
}

// RegisterActions sets up named registration for the given actions so
// BuildAction can construct them (and their aliases) by name in this
// application. It is an explicit setup-time step: canonical
// definitions are process-global and registerable independently of
// whether their payloads are JSON-capable.
//
// Registering the same canonical descriptor again is idempotent.
// Conflicting names, types and deprecated aliases are setup errors and
// panic with a typed *ActionRegistrationError, mirroring the reference
// panic ("Action with name `{name}` already registered"); conflicts are
// detected in registration order, whichever side registered first.
func (a *App) RegisterActions(actionsToRegister ...AnyAction) {
	if a == nil {
		panic("gpui: RegisterActions called on a nil App")
	}
	actions.mu.Lock()
	defer actions.mu.Unlock()
	registry := actionRegistryFor(a)
	for _, action := range actionsToRegister {
		carrier, ok := action.(actionCarrier)
		if !ok || carrier == nil || carrier.actionRef() == nil {
			panic(&ActionRegistrationError{Kind: ActionRegistrationInvalidAction})
		}
		if err := registry.insert(carrier.actionRef()); err != nil {
			panic(err)
		}
	}
}

// BuildAction constructs a boxed action from its registered name and
// a JSON payload, the path key binding payloads take. The name may be
// the canonical name or a deprecated alias. Failures are distinct
// typed categories:
//
//   - ActionBuildNotFound: the name is not registered in this
//     application ("Didn't find an action named ...").
//   - ActionBuildNoJSON: the action is registered but JSON-disabled
//     ("... cannot be built from JSON").
//   - ActionBuildDecode: the action's FromJSON rejected the payload,
//     wrapping the underlying error.
//
// A nil or empty payload is passed as an empty JSON object, matching
// the reference's `params.unwrap_or_else(|| json!({}))`. The parsed
// value is cloned before it is boxed, so the returned action owns its
// payload.
func (a *App) BuildAction(name string, data json.RawMessage) (BoxedAction, error) {
	if a == nil {
		panic("gpui: BuildAction called on a nil App")
	}
	actions.mu.Lock()
	descriptor := actionRegistryFor(a).byName[name]
	actions.mu.Unlock()

	if descriptor == nil {
		return BoxedAction{}, &ActionBuildError{Kind: ActionBuildNotFound, Name: name}
	}
	if descriptor.fromJSON == nil {
		return BoxedAction{}, &ActionBuildError{Kind: ActionBuildNoJSON, Name: name}
	}
	if len(data) == 0 {
		data = json.RawMessage("{}")
	}
	value, err := descriptor.fromJSON(data)
	if err != nil {
		return BoxedAction{}, &ActionBuildError{Kind: ActionBuildDecode, Name: name, Err: err}
	}
	return BoxedAction{descriptor: descriptor, payload: descriptor.clone(value)}, nil
}

// ---------------------------------------------------------------------------
// Action handler bindings (element-facing core)
// ---------------------------------------------------------------------------

// actionBinding is one entry of an element's action listener list: the
// port of the closures the reference's on_action and on_boxed_action
// push into an element's interactivity state
// (reference/ce-source/crates/gpui/src/elements/div.rs).
//
// A boxed binding stores a clone of the bound instance captured at
// registration and delivers that stored clone on every dispatch of the
// type, deliberately ignoring the dispatched instance: the reference
// closure is `move |_, phase, window, cx| (listener)(&*action, window,
// cx)`, where the incoming action parameter is `_`. A typed binding
// receives the dispatched payload instead, like the reference's
// downcast in on_action; the payload is copied before the handler
// receives its pointer, so handler writes cannot reach the dispatched
// action's stored payload.
type actionBinding struct {
	payloadType reflect.Type

	// stored is the bind-time clone a boxed binding delivers.
	stored  BoxedAction
	boxedFn func(BoxedAction, *Window, *App)

	// typedFn receives a copy of the dispatched payload (erased
	// func(*A, *Window, *App)).
	typedFn func(payload any, window *Window, app *App)
}

// bindBoxedAction registers handler for the type of the bound action.
// The bound instance is cloned at registration — the reference's
// on_boxed_action starts with `let action = action.boxed_clone();` —
// and that stored clone is what the handler receives on dispatch, even
// when a different instance is dispatched.
func bindBoxedAction(bound BoxedAction, handler func(BoxedAction, *Window, *App)) *actionBinding {
	d := bound.mustDescriptor("bindBoxedAction")
	if handler == nil {
		panic("gpui: bindBoxedAction called with a nil handler")
	}
	return &actionBinding{
		payloadType: d.payloadType,
		stored:      bound.Clone(),
		boxedFn:     handler,
	}
}

// bindTypedAction registers handler for payload type A. The handler
// receives a pointer to a copy of the dispatched payload, so callback
// action values stay read-only by contract (and by copy).
func bindTypedAction[A any](handler func(*A, *Window, *App)) *actionBinding {
	if handler == nil {
		panic("gpui: bindTypedAction called with a nil handler")
	}
	payloadType := reflect.TypeFor[A]()
	return &actionBinding{
		payloadType: payloadType,
		typedFn: func(payload any, window *Window, app *App) {
			value, ok := payload.(A)
			if !ok {
				// Unreachable through actionBindingSet, which matches by
				// payload type; the guard keeps direct misuse loud.
				panic(fmt.Sprintf("gpui: typed action binding for %s received a payload of type %T", payloadType, payload))
			}
			handler(&value, window, app)
		},
	}
}

// dispatch invokes the binding for a dispatched action.
func (b *actionBinding) dispatch(dispatched BoxedAction, window *Window, app *App) {
	if b.boxedFn != nil {
		// The dispatched instance is deliberately ignored; the stored
		// clone captured at bind time is delivered.
		b.boxedFn(b.stored, window, app)
		return
	}
	b.typedFn(dispatched.payload, window, app)
}

// actionBindingSet routes dispatched actions to the bindings
// registered for their payload type. The element tree's capture/bubble
// walk, propagation stop, and "registration does not guarantee all
// handlers run" rules arrive with the element and focus tickets; this
// flat, registration-ordered set is the headless routing core those
// tickets install into the focus walk. Bindings registered during a
// dispatch join later dispatches, the same rule event subscriptions
// follow.
type actionBindingSet struct {
	bindings []*actionBinding
}

func (s *actionBindingSet) add(b *actionBinding) {
	if b == nil {
		panic("gpui: actionBindingSet.add called with a nil binding")
	}
	s.bindings = append(s.bindings, b)
}

func (s *actionBindingSet) dispatch(action BoxedAction, window *Window, app *App) {
	d := action.mustDescriptor("actionBindingSet.dispatch")
	snapshot := slices.Clone(s.bindings)
	for _, binding := range snapshot {
		if binding.payloadType == d.payloadType {
			binding.dispatch(action, window, app)
		}
	}
}

// ---------------------------------------------------------------------------
// Window dispatch entry points
// ---------------------------------------------------------------------------

// Dispatch validates the action, clones the supplied payload into a
// BoxedAction and hands it to DispatchBoxed. Real routing walks the
// window's focus tree over element action listeners
// (window.rs dispatch_action / dispatch_action_on_node); windows and
// focus arrive with later tickets, so in this slice the dispatch entry
// points land on the Window placeholder and fail with the typed
// *ActionDispatchError carrying the cloned action they prepared.
func (w *Window) Dispatch[A any](action Action[A], payload A, cx AppContext) {
	if w == nil {
		panic(ErrNoWindow)
	}
	app := appFrom(cx)
	w.DispatchBoxed(action.Box(payload), app)
}

// DispatchBoxed dispatches a boxed action on the currently focused
// element (Window::dispatch_action): the captured payload is cloned
// again for delivery, and the dispatch is deferred to the end of the
// current effect cycle so entities on the stack return to the app
// before listeners run. Routing follows the focused element's dispatch
// path with the reference's capture/bubble phases and
// stop-by-default bubble semantics.
func (w *Window) DispatchBoxed(action BoxedAction, cx AppContext) {
	if w == nil {
		panic(ErrNoWindow)
	}
	app := appFrom(cx)
	delivered := action.Clone()
	app.Defer(func(app *App) {
		fs := focusState(w)
		nodeID := fs.renderedDispatchTree().RootNodeID()
		if fs.focused != 0 {
			if id, ok := fs.renderedDispatchTree().focusableNodeID(fs.focused); ok {
				nodeID = id
			}
		}
		w.dispatchActionOnNode(nodeID, delivered, app)
	})
}
