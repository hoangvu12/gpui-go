package gpui

// Tests for the typed action registry (action.go). The canonical
// registry is process-global, so every payload type is defined exactly
// once at file scope and shared across tests; tests that exercise
// redefinition diagnostics do so with specs that panic (no state
// change) or specs that match the canonical one exactly (idempotent).
//
// The ~struct{} constraint of DefineUnitAction cannot be tested
// negatively from inside the package: a file that fails to compile
// cannot run. The constraint is compile-verified evidence recorded in
// the ticket report ("Payload does not satisfy ~struct{}"); the tests
// below pin the positive form and the zero-size invariant the runtime
// guard defends.

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Shared canonical fixtures
// ---------------------------------------------------------------------------

type actionTestUnit struct{}

var actionTestUnitAction = DefineUnitAction[actionTestUnit]("actiontest::Unit")

type actionTestSelect struct {
	Replace bool
	Tag     string
}

// actionTestSelectCloneCalls counts Clone service invocations for the
// shared select fixture. Tests reset it before measuring.
var actionTestSelectCloneCalls int

func actionTestSelectSpec() ActionSpec[actionTestSelect] {
	return ActionSpec[actionTestSelect]{
		Name:          "actiontest::Select",
		Aliases:       []string{"actiontest::SelectOld"},
		Deprecation:   "use actiontest::Select",
		Documentation: "Selects the next item.",
		Clone: func(v actionTestSelect) actionTestSelect {
			actionTestSelectCloneCalls++
			return v
		},
		Equal: func(a, b actionTestSelect) bool { return a == b },
		FromJSON: func(data json.RawMessage) (actionTestSelect, error) {
			var value actionTestSelect
			if err := json.Unmarshal(data, &value); err != nil {
				return actionTestSelect{}, err
			}
			return value, nil
		},
		Schema: func() any {
			return map[string]any{
				"type": "object",
				"properties": map[string]any{
					"Replace": map[string]any{"type": "boolean"},
					"Tag":     map[string]any{"type": "string"},
				},
			}
		},
	}
}

var actionTestSelectAction = DefineAction(actionTestSelectSpec())

// actionTestJSONlessAction is a rich action without FromJSON: registered
// names stay buildable-less, the JSON-disabled category.
type actionTestJSONless struct{ Count int }

var actionTestJSONlessAction = DefineAction(ActionSpec[actionTestJSONless]{
	Name:  "actiontest::JSONless",
	Clone: func(v actionTestJSONless) actionTestJSONless { return v },
	Equal: func(a, b actionTestJSONless) bool { return a == b },
	// No FromJSON: JSON-disabled by choice, independent of registration.
})

// actionTestConflictA/B collide: B's canonical name is A's alias.
type actionTestConflictA struct{}
type actionTestConflictB struct{}

var actionTestConflictAAction = DefineAction(ActionSpec[actionTestConflictA]{
	Name:     "actiontest::A",
	Aliases:  []string{"actiontest::Shared"},
	Clone:    func(v actionTestConflictA) actionTestConflictA { return v },
	Equal:    func(a, b actionTestConflictA) bool { return a == b },
	FromJSON: func(json.RawMessage) (actionTestConflictA, error) { return actionTestConflictA{}, nil },
})

var actionTestConflictBAction = DefineAction(ActionSpec[actionTestConflictB]{
	Name:     "actiontest::Shared",
	Clone:    func(v actionTestConflictB) actionTestConflictB { return v },
	Equal:    func(a, b actionTestConflictB) bool { return a == b },
	FromJSON: func(json.RawMessage) (actionTestConflictB, error) { return actionTestConflictB{}, nil },
})

// actionTestDupOne/DupTwo use the same canonical name for different
// payload types: allowed canonically, an error when both are registered
// in one application.
type actionTestDupOne struct{}
type actionTestDupTwo struct{}

var actionTestDupOneAction = DefineAction(ActionSpec[actionTestDupOne]{
	Name:  "actiontest::Dup",
	Clone: func(v actionTestDupOne) actionTestDupOne { return v },
	Equal: func(a, b actionTestDupOne) bool { return a == b },
})

var actionTestDupTwoAction = DefineAction(ActionSpec[actionTestDupTwo]{
	Name:  "actiontest::Dup",
	Clone: func(v actionTestDupTwo) actionTestDupTwo { return v },
	Equal: func(a, b actionTestDupTwo) bool { return a == b },
})

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// expectActionPanic runs f and asserts it panics with a message (string
// panic or error value) containing want.
func expectActionPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Fatalf("expected panic containing %q, got none", want)
		}
		var msg string
		switch value := r.(type) {
		case string:
			msg = value
		case error:
			msg = value.Error()
		default:
			t.Fatalf("panic value is neither string nor error: %v", r)
		}
		if !strings.Contains(msg, want) {
			t.Fatalf("panic %q does not contain %q", msg, want)
		}
	}()
	f()
}

// expectTypedActionPanic runs f and asserts it panics with a value of
// the concrete type T, returning it.
func expectTypedActionPanic[T any](t *testing.T, f func()) T {
	t.Helper()
	var caught T
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				value, ok := r.(T)
				if !ok {
					t.Fatalf("expected a %T panic, got %T: %v", caught, r, r)
				}
				caught = value
				panicked = true
			}
		}()
		f()
	}()
	if !panicked {
		t.Fatalf("expected a %T panic, got none", caught)
	}
	return caught
}

// ---------------------------------------------------------------------------
// Signature pins and canonical definitions
// ---------------------------------------------------------------------------

func TestActionSignaturePins(t *testing.T) {
	// Compile-level pins of the compile-verified fixture shape
	// (.scratch/signature-validation/api.go) against the runtime.
	var (
		_ func(ActionSpec[actionTestSelect]) Action[actionTestSelect]             = DefineAction[actionTestSelect]
		_ func(string) Action[actionTestUnit]                                     = DefineUnitAction[actionTestUnit]
		_ func(Action[actionTestSelect], actionTestSelect) BoxedAction            = Action[actionTestSelect].Box
		_ func(Action[actionTestSelect], actionTestSelect) actionTestSelect       = Action[actionTestSelect].Clone
		_ func(Action[actionTestSelect], actionTestSelect, actionTestSelect) bool = Action[actionTestSelect].Equal
		_ func(Action[actionTestSelect]) string                                   = Action[actionTestSelect].Name
		_ func(*App, ...AnyAction)                                                = (*App).RegisterActions
		_ func(*App, string, json.RawMessage) (BoxedAction, error)                = (*App).BuildAction
		_ func(*Window, Action[actionTestSelect], actionTestSelect, AppContext)   = (*Window).Dispatch[actionTestSelect]
		_ func(*Window, BoxedAction, AppContext)                                  = (*Window).DispatchBoxed
	)
	var _ AnyAction = actionTestUnitAction
	var _ AnyAction = actionTestSelectAction
}

func TestDefineActionCanonical(t *testing.T) {
	if name := actionTestSelectAction.Name(); name != "actiontest::Select" {
		t.Fatalf("Name() = %q, want actiontest::Select", name)
	}
	aliases := actionTestSelectAction.Aliases()
	if len(aliases) != 1 || aliases[0] != "actiontest::SelectOld" {
		t.Fatalf("Aliases() = %v, want [actiontest::SelectOld]", aliases)
	}
	// Aliases returns a copy: mutating it must not leak into the
	// immutable descriptor.
	aliases[0] = "mutated"
	if again := actionTestSelectAction.Aliases(); again[0] != "actiontest::SelectOld" {
		t.Fatalf("Aliases() leaked a mutation: %v", again)
	}
	if got := actionTestSelectAction.Deprecation(); got != "use actiontest::Select" {
		t.Fatalf("Deprecation() = %q", got)
	}
	if got := actionTestSelectAction.Documentation(); got != "Selects the next item." {
		t.Fatalf("Documentation() = %q", got)
	}
	schema := actionTestSelectAction.Schema()
	if schema == nil {
		t.Fatal("Schema() = nil, want the recorded schema value")
	}
	if _, err := json.Marshal(schema); err != nil {
		t.Fatalf("schema is not JSON-marshalable: %v", err)
	}

	// Clone and Equal delegate to the canonical spec functions.
	actionTestSelectCloneCalls = 0
	value := actionTestSelect{Replace: true, Tag: "x"}
	if got := actionTestSelectAction.Clone(value); got != value {
		t.Fatalf("Clone() = %+v, want %+v", got, value)
	}
	if actionTestSelectCloneCalls != 1 {
		t.Fatalf("Clone service calls = %d, want 1", actionTestSelectCloneCalls)
	}
	if !actionTestSelectAction.Equal(value, value) {
		t.Fatal("Equal(value, value) = false")
	}
	if actionTestSelectAction.Equal(value, actionTestSelect{}) {
		t.Fatal("Equal(value, zero) = true")
	}
	if actionTestJSONlessAction.Schema() != nil {
		t.Fatal("JSONless action exposes a schema, want none")
	}
}

func TestDefineActionIdempotentRedefinition(t *testing.T) {
	// A second definition that matches the canonical declaration exactly
	// returns the canonical descriptor.
	again := DefineAction(actionTestSelectSpec())
	if again != actionTestSelectAction {
		t.Fatal("idempotent redefinition returned a different descriptor")
	}
	unitAgain := DefineUnitAction[actionTestUnit]("actiontest::Unit")
	if unitAgain != actionTestUnitAction {
		t.Fatal("idempotent unit redefinition returned a different descriptor")
	}
}

func TestDefineActionConflictingRedefinition(t *testing.T) {
	spec := actionTestSelectSpec()
	spec.Name = "actiontest::Select2"
	err := expectTypedActionPanic[*ActionDefinitionError](t, func() {
		DefineAction(spec)
	})
	if err.Kind != ActionDefinitionRedefinition {
		t.Fatalf("Kind = %v, want ActionDefinitionRedefinition", err.Kind)
	}
	for _, want := range []string{"actiontest::Select", "actiontest::Select2", "actionTestSelect"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("redefinition diagnostic %q does not name %q", err.Error(), want)
		}
	}

	// A rich definition of a unit-defined payload type is a
	// redefinition, not an idempotent repeat: the services differ in
	// origin even when the name matches.
	rich := ActionSpec[actionTestUnit]{
		Name:  "actiontest::Unit",
		Clone: func(v actionTestUnit) actionTestUnit { return v },
		Equal: func(a, b actionTestUnit) bool { return a == b },
	}
	err = expectTypedActionPanic[*ActionDefinitionError](t, func() {
		DefineAction(rich)
	})
	if err.Kind != ActionDefinitionRedefinition {
		t.Fatalf("Kind = %v, want ActionDefinitionRedefinition", err.Kind)
	}

	// Conflicting unit redefinition with a different name.
	err = expectTypedActionPanic[*ActionDefinitionError](t, func() {
		DefineUnitAction[actionTestUnit]("actiontest::UnitOther")
	})
	if err.Kind != ActionDefinitionRedefinition {
		t.Fatalf("Kind = %v, want ActionDefinitionRedefinition", err.Kind)
	}
}

func TestDefineActionRichRequirements(t *testing.T) {
	spec := ActionSpec[actionTestJSONless]{
		Name:  "actiontest::MissingClone",
		Equal: func(a, b actionTestJSONless) bool { return a == b },
	}
	err := expectTypedActionPanic[*ActionDefinitionError](t, func() { DefineAction(spec) })
	if err.Kind != ActionDefinitionMissingClone {
		t.Fatalf("Kind = %v, want ActionDefinitionMissingClone", err.Kind)
	}
	if !strings.Contains(err.Error(), "Clone") {
		t.Fatalf("diagnostic %q does not mention Clone", err.Error())
	}

	spec = ActionSpec[actionTestJSONless]{
		Name:  "actiontest::MissingEqual",
		Clone: func(v actionTestJSONless) actionTestJSONless { return v },
	}
	err = expectTypedActionPanic[*ActionDefinitionError](t, func() { DefineAction(spec) })
	if err.Kind != ActionDefinitionMissingEqual {
		t.Fatalf("Kind = %v, want ActionDefinitionMissingEqual", err.Kind)
	}
	if !strings.Contains(err.Error(), "Equal") {
		t.Fatalf("diagnostic %q does not mention Equal", err.Error())
	}
}

func TestDefineActionNameAndAliasValidation(t *testing.T) {
	empty := ActionSpec[actionTestJSONless]{
		Name:  "",
		Clone: func(v actionTestJSONless) actionTestJSONless { return v },
		Equal: func(a, b actionTestJSONless) bool { return a == b },
	}
	err := expectTypedActionPanic[*ActionDefinitionError](t, func() { DefineAction(empty) })
	if err.Kind != ActionDefinitionEmptyName {
		t.Fatalf("Kind = %v, want ActionDefinitionEmptyName", err.Kind)
	}
	err = expectTypedActionPanic[*ActionDefinitionError](t, func() {
		DefineUnitAction[actionTestUnit]("")
	})
	if err.Kind != ActionDefinitionEmptyName {
		t.Fatalf("Kind = %v, want ActionDefinitionEmptyName", err.Kind)
	}

	self := ActionSpec[actionTestJSONless]{
		Name:    "actiontest::Self",
		Aliases: []string{"actiontest::Self"},
		Clone:   func(v actionTestJSONless) actionTestJSONless { return v },
		Equal:   func(a, b actionTestJSONless) bool { return a == b },
	}
	err = expectTypedActionPanic[*ActionDefinitionError](t, func() { DefineAction(self) })
	if err.Kind != ActionDefinitionBadAlias {
		t.Fatalf("Kind = %v, want ActionDefinitionBadAlias", err.Kind)
	}

	dup := ActionSpec[actionTestJSONless]{
		Name:    "actiontest::Dup",
		Aliases: []string{"actiontest::Alias", "actiontest::Alias"},
		Clone:   func(v actionTestJSONless) actionTestJSONless { return v },
		Equal:   func(a, b actionTestJSONless) bool { return a == b },
	}
	err = expectTypedActionPanic[*ActionDefinitionError](t, func() { DefineAction(dup) })
	if err.Kind != ActionDefinitionBadAlias {
		t.Fatalf("Kind = %v, want ActionDefinitionBadAlias", err.Kind)
	}

	blank := ActionSpec[actionTestJSONless]{
		Name:    "actiontest::Blank",
		Aliases: []string{""},
		Clone:   func(v actionTestJSONless) actionTestJSONless { return v },
		Equal:   func(a, b actionTestJSONless) bool { return a == b },
	}
	err = expectTypedActionPanic[*ActionDefinitionError](t, func() { DefineAction(blank) })
	if err.Kind != ActionDefinitionBadAlias {
		t.Fatalf("Kind = %v, want ActionDefinitionBadAlias", err.Kind)
	}
}

func TestDefineUnitActionServices(t *testing.T) {
	// The ~struct{} constraint pins the payload at compile time; the
	// zero-size invariant below is what the defensive runtime check
	// guards (it cannot trigger through the public API while the
	// constraint holds).
	if size := reflect.TypeFor[actionTestUnit]().Size(); size != 0 {
		t.Fatalf("unit payload size = %d, want 0", size)
	}

	value := actionTestUnit{}
	if got := actionTestUnitAction.Clone(value); got != value {
		t.Fatalf("unit Clone() = %+v, want %+v", got, value)
	}
	if !actionTestUnitAction.Equal(value, value) {
		t.Fatal("unit Equal(any, any) = false")
	}

	// Unit payloads ignore JSON input entirely (reference: unit-struct
	// build is Ok(Box::new(Self)) regardless of the value); that is
	// exercised through BuildAction in the registry tests.
	if IsNoAction(actionTestUnitAction.Box(value)) {
		t.Fatal("a plain unit action reports IsNoAction")
	}
}

// ---------------------------------------------------------------------------
// Named registration and BuildAction
// ---------------------------------------------------------------------------

func TestBuildActionUnregisteredAndPerApp(t *testing.T) {
	app := NewTestApp().App()
	other := NewTestApp().App()

	_, err := app.BuildAction("actiontest::Select", nil)
	var notFound *ActionBuildError
	if !errors.As(err, &notFound) {
		t.Fatalf("BuildAction error = %v, want *ActionBuildError", err)
	}
	if notFound.Kind != ActionBuildNotFound {
		t.Fatalf("Kind = %v, want ActionBuildNotFound", notFound.Kind)
	}
	if !strings.Contains(notFound.Error(), `Didn't find an action named "actiontest::Select"`) {
		t.Fatalf("diagnostic %q does not follow the reference wording", notFound.Error())
	}

	app.RegisterActions(actionTestSelectAction)
	// Registration is per application: the other app still cannot build
	// by that name.
	if _, err := other.BuildAction("actiontest::Select", nil); !errors.As(err, &notFound) {
		t.Fatalf("other app resolved a name registered elsewhere: %v", err)
	}
}

func TestBuildActionByNameAndAlias(t *testing.T) {
	app := NewTestApp().App()
	app.RegisterActions(actionTestSelectAction, actionTestJSONlessAction)

	// nil data reaches FromJSON as an empty object, the reference's
	// params.unwrap_or_else(|| json!({})).
	boxed, err := app.BuildAction("actiontest::Select", nil)
	if err != nil {
		t.Fatalf("BuildAction(canonical, nil) failed: %v", err)
	}
	value, err := Unbox[actionTestSelect](boxed)
	if err != nil {
		t.Fatalf("Unbox failed: %v", err)
	}
	if value != (actionTestSelect{}) {
		t.Fatalf("nil data built %+v, want the zero payload", value)
	}

	byAlias, err := app.BuildAction("actiontest::SelectOld", json.RawMessage(`{"Replace":true,"Tag":"alias"}`))
	if err != nil {
		t.Fatalf("BuildAction(alias) failed: %v", err)
	}
	aliasValue, err := Unbox[actionTestSelect](byAlias)
	if err != nil {
		t.Fatalf("Unbox(alias) failed: %v", err)
	}
	if aliasValue != (actionTestSelect{Replace: true, Tag: "alias"}) {
		t.Fatalf("alias built %+v", aliasValue)
	}

	// The parsed value is cloned before it is boxed.
	actionTestSelectCloneCalls = 0
	if _, err := app.BuildAction("actiontest::Select", json.RawMessage(`{"Tag":"counted"}`)); err != nil {
		t.Fatalf("BuildAction failed: %v", err)
	}
	if actionTestSelectCloneCalls != 1 {
		t.Fatalf("Clone service calls = %d, want 1 (cloned parsed value)", actionTestSelectCloneCalls)
	}

	// Re-registering the same canonical descriptor is idempotent.
	app.RegisterActions(actionTestSelectAction)
	if _, err := app.BuildAction("actiontest::Select", nil); err != nil {
		t.Fatalf("BuildAction after idempotent re-registration failed: %v", err)
	}
}

func TestBuildActionJSONRoundTrip(t *testing.T) {
	app := NewTestApp().App()
	app.RegisterActions(actionTestSelectAction)

	value := actionTestSelect{Replace: true, Tag: "round-trip"}
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	boxed, err := app.BuildAction("actiontest::Select", data)
	if err != nil {
		t.Fatalf("BuildAction failed: %v", err)
	}
	built, err := Unbox[actionTestSelect](boxed)
	if err != nil {
		t.Fatalf("Unbox failed: %v", err)
	}
	if !actionTestSelectAction.Equal(value, built) {
		t.Fatalf("round trip built %+v, want %+v", built, value)
	}
}

func TestBuildActionDistinctErrorCategories(t *testing.T) {
	app := NewTestApp().App()
	app.RegisterActions(actionTestJSONlessAction)

	// JSON-disabled is distinct from unregistered: the action is
	// registered, but building it by name fails with NoJSON.
	_, err := app.BuildAction("actiontest::JSONless", json.RawMessage(`{}`))
	var buildErr *ActionBuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("BuildAction error = %v, want *ActionBuildError", err)
	}
	if buildErr.Kind != ActionBuildNoJSON {
		t.Fatalf("Kind = %v, want ActionBuildNoJSON", buildErr.Kind)
	}
	if !strings.Contains(buildErr.Error(), "cannot be built from JSON") {
		t.Fatalf("diagnostic %q does not follow the reference wording", buildErr.Error())
	}

	// Invalid payload JSON is the decode category and wraps the
	// underlying error.
	app.RegisterActions(actionTestSelectAction)
	_, err = app.BuildAction("actiontest::Select", json.RawMessage(`{"Replace":`))
	if !errors.As(err, &buildErr) {
		t.Fatalf("BuildAction error = %v, want *ActionBuildError", err)
	}
	if buildErr.Kind != ActionBuildDecode {
		t.Fatalf("Kind = %v, want ActionBuildDecode", buildErr.Kind)
	}
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		t.Fatalf("decode error does not unwrap to the JSON error: %v", err)
	}

	// A wrong-shaped but valid payload is also a decode failure.
	_, err = app.BuildAction("actiontest::Select", json.RawMessage(`[1,2,3]`))
	if !errors.As(err, &buildErr) || buildErr.Kind != ActionBuildDecode {
		t.Fatalf("wrong-shaped payload error = %v, want decode category", err)
	}
}

func TestRegisterActionsConflicts(t *testing.T) {
	// B's canonical name collides with A's alias, in both registration
	// orders.
	for _, order := range []struct {
		first, second AnyAction
		kind          ActionRegistrationKind
		conflict      string
	}{
		{actionTestConflictAAction, actionTestConflictBAction, ActionRegistrationNameConflict, "actiontest::Shared"},
		{actionTestConflictBAction, actionTestConflictAAction, ActionRegistrationAliasConflict, "actiontest::Shared"},
		{actionTestDupOneAction, actionTestDupTwoAction, ActionRegistrationNameConflict, "actiontest::Dup"},
	} {
		app := NewTestApp().App()
		app.RegisterActions(order.first)
		err := expectTypedActionPanic[*ActionRegistrationError](t, func() {
			app.RegisterActions(order.second)
		})
		if err.Kind != order.kind {
			t.Fatalf("Kind = %v, want %v (%s vs %s)", err.Kind, order.kind, order.first, order.second)
		}
		if err.Conflict != order.conflict {
			t.Fatalf("Conflict = %q, want %q", err.Conflict, order.conflict)
		}
		if !strings.Contains(err.Error(), "already registered") {
			t.Fatalf("diagnostic %q does not follow the reference wording", err.Error())
		}
	}

	// Nil and zero-value actions are setup errors.
	app := NewTestApp().App()
	expectTypedActionPanic[*ActionRegistrationError](t, func() {
		app.RegisterActions(nil)
	})
	expectTypedActionPanic[*ActionRegistrationError](t, func() {
		app.RegisterActions(Action[actionTestSelect]{})
	})
}

// ---------------------------------------------------------------------------
// BoxedAction: clone-on-capture, clone-on-delivery, read-only
// ---------------------------------------------------------------------------

func TestBoxedActionCloneAndReadOnly(t *testing.T) {
	value := actionTestSelect{Replace: true, Tag: "bound"}

	actionTestSelectCloneCalls = 0
	boxed := actionTestSelectAction.Box(value)
	if actionTestSelectCloneCalls != 1 {
		t.Fatalf("clone-on-capture calls = %d, want 1", actionTestSelectCloneCalls)
	}
	if name := boxed.Name(); name != "actiontest::Select" {
		t.Fatalf("boxed Name() = %q", name)
	}

	// Every read returns a fresh clone of the captured payload.
	first, err := Unbox[actionTestSelect](boxed)
	if err != nil {
		t.Fatalf("Unbox failed: %v", err)
	}
	if actionTestSelectCloneCalls != 2 {
		t.Fatalf("clone-on-delivery calls = %d, want 2", actionTestSelectCloneCalls)
	}
	second, err := Unbox[actionTestSelect](boxed)
	if err != nil {
		t.Fatalf("Unbox failed: %v", err)
	}
	if actionTestSelectCloneCalls != 3 {
		t.Fatalf("second Unbox calls = %d, want 3", actionTestSelectCloneCalls)
	}
	if first != value || second != value {
		t.Fatalf("delivered payloads differ from the captured value")
	}

	// The payload is read-only by contract: mutating a delivered copy
	// does not reach the captured value.
	first.Tag = "mutated"
	third, err := Unbox[actionTestSelect](boxed)
	if err != nil {
		t.Fatalf("Unbox failed: %v", err)
	}
	if third != value {
		t.Fatalf("captured payload was mutated through a delivered copy: %+v", third)
	}

	// Payload() is the untyped accessor with the same cloning rule.
	if got := boxed.Payload(); got != (any)(value) {
		t.Fatalf("Payload() = %+v, want %+v", got, value)
	}

	// Clone re-clones; Equal compares payloads.
	actionTestSelectCloneCalls = 0
	cloned := boxed.Clone()
	if actionTestSelectCloneCalls != 1 {
		t.Fatalf("Clone calls = %d, want 1", actionTestSelectCloneCalls)
	}
	if !boxed.Equal(cloned) {
		t.Fatal("Equal(boxed, boxed.Clone()) = false")
	}
	if boxed.Equal(actionTestSelectAction.Box(actionTestSelect{Replace: false})) {
		t.Fatal("Equal compares unequal payloads as equal")
	}
	// Different payload types are never equal.
	if boxed.Equal(actionTestUnitAction.Box(actionTestUnit{})) {
		t.Fatal("Equal across payload types = true")
	}
}

func TestUnboxWrongType(t *testing.T) {
	boxed := actionTestSelectAction.Box(actionTestSelect{Tag: "typed"})
	_, err := Unbox[actionTestUnit](boxed)
	var typeErr *ActionTypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("Unbox error = %v, want *ActionTypeError", err)
	}
	if typeErr.Action != "actiontest::Select" {
		t.Fatalf("Action = %q", typeErr.Action)
	}
	for _, want := range []string{"actionTestUnit", "actionTestSelect"} {
		if !strings.Contains(typeErr.Error(), want) {
			t.Fatalf("type diagnostic %q does not name %q", typeErr.Error(), want)
		}
	}
}

// ---------------------------------------------------------------------------
// Builtin NoAction and Unbind
// ---------------------------------------------------------------------------

func TestBuiltinNoActionAndUnbind(t *testing.T) {
	if NoActionDescriptor.Name() != "zed::NoAction" {
		t.Fatalf("NoAction name = %q", NoActionDescriptor.Name())
	}
	if UnbindDescriptor.Name() != "zed::Unbind" {
		t.Fatalf("Unbind name = %q", UnbindDescriptor.Name())
	}

	noAction := NoActionDescriptor.Box(NoAction{})
	if !IsNoAction(noAction) {
		t.Fatal("IsNoAction(NoAction box) = false")
	}
	if IsUnbind(noAction) {
		t.Fatal("IsUnbind(NoAction box) = true")
	}

	unbind := UnbindDescriptor.Box(Unbind{Action: "editor::NewLine"})
	if !IsUnbind(unbind) {
		t.Fatal("IsUnbind(Unbind box) = false")
	}
	if IsNoAction(unbind) {
		t.Fatal("IsNoAction(Unbind box) = true")
	}
	if IsNoAction(actionTestSelectAction.Box(actionTestSelect{})) {
		t.Fatal("IsNoAction(user action) = true")
	}
	// Zero-value boxed actions are not actions of any kind.
	if IsNoAction(BoxedAction{}) || IsUnbind(BoxedAction{}) {
		t.Fatal("zero-value BoxedAction satisfies a builtin predicate")
	}

	// The builtins are seeded into every application: a fresh app with
	// no user registration resolves them by name, mirroring the
	// reference registry that loads the inventory of every action.
	app := NewTestApp().App()
	built, err := app.BuildAction("zed::NoAction", json.RawMessage(`"anything"`))
	if err != nil {
		t.Fatalf("BuildAction(NoAction) failed: %v", err)
	}
	if !IsNoAction(built) {
		t.Fatal("built NoAction is not NoAction")
	}
	// Unit payloads ignore the JSON input entirely.
	payload, err := Unbox[NoAction](built)
	if err != nil || payload != (NoAction{}) {
		t.Fatalf("built NoAction payload = %+v, %v", payload, err)
	}

	// zed::Unbind builds from a bare JSON string
	// (["zed::Unbind", "editor::NewLine"] in keymap JSON).
	built, err = app.BuildAction("zed::Unbind", json.RawMessage(`"editor::NewLine"`))
	if err != nil {
		t.Fatalf("BuildAction(Unbind) failed: %v", err)
	}
	unbindValue, err := Unbox[Unbind](built)
	if err != nil {
		t.Fatalf("Unbox(Unbind) failed: %v", err)
	}
	if unbindValue.Action != "editor::NewLine" {
		t.Fatalf("Unbind payload = %+v", unbindValue)
	}
	if !UnbindDescriptor.Equal(Unbind{Action: "a"}, Unbind{Action: "a"}) ||
		UnbindDescriptor.Equal(Unbind{Action: "a"}, Unbind{Action: "b"}) {
		t.Fatal("Unbind Equal is wrong")
	}
	if _, err := json.Marshal(UnbindDescriptor.Schema()); err != nil {
		t.Fatalf("Unbind schema is not JSON-marshalable: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Action handler bindings (the element-facing core)
// ---------------------------------------------------------------------------

func TestBoxedBindingDeliversCapturedClone(t *testing.T) {
	window := &Window{}
	app := NewTestApp().App()

	boundValue := actionTestSelect{Replace: true, Tag: "bound"}
	actionTestSelectCloneCalls = 0
	bound := actionTestSelectAction.Box(boundValue) // clone-on-capture
	if actionTestSelectCloneCalls != 1 {
		t.Fatalf("clone-on-capture calls = %d, want 1", actionTestSelectCloneCalls)
	}

	var received []actionTestSelect
	binding := bindBoxedAction(bound, func(action BoxedAction, _ *Window, _ *App) {
		value, err := Unbox[actionTestSelect](action)
		if err != nil {
			t.Errorf("handler Unbox failed: %v", err)
			return
		}
		received = append(received, value)
	})
	// The bound instance is cloned again at registration
	// (on_boxed_action: let action = action.boxed_clone()).
	if actionTestSelectCloneCalls != 2 {
		t.Fatalf("clone-at-bind calls = %d, want 2", actionTestSelectCloneCalls)
	}

	// Dispatch a DIFFERENT instance: the handler still receives the
	// stored clone captured at bind time, not the dispatched payload.
	dispatched := actionTestSelectAction.Box(actionTestSelect{Replace: false, Tag: "dispatched"})
	callsAtDispatch := actionTestSelectCloneCalls
	binding.dispatch(dispatched, window, app)
	if len(received) != 1 {
		t.Fatalf("handler calls = %d, want 1", len(received))
	}
	if received[0] != boundValue {
		t.Fatalf("handler received %+v, want the bound clone %+v", received[0], boundValue)
	}
	// Dispatching the stored clone does not clone again (the reference
	// passes &*action); the count only moved by the handler's Unbox.
	if actionTestSelectCloneCalls != callsAtDispatch+1 {
		t.Fatalf("clone calls during dispatch = %d, want +%d (handler read only)", actionTestSelectCloneCalls, 1)
	}

	// A second dispatch of yet another instance delivers the same stored
	// clone.
	binding.dispatch(actionTestSelectAction.Box(actionTestSelect{Tag: "other"}), window, app)
	if len(received) != 2 || received[1] != boundValue {
		t.Fatalf("second dispatch delivered %+v, want the bound clone", received)
	}
}

func TestTypedBindingReceivesDispatchedPayload(t *testing.T) {
	window := &Window{}
	app := NewTestApp().App()

	var received []actionTestSelect
	binding := bindTypedAction[actionTestSelect](func(value *actionTestSelect, _ *Window, _ *App) {
		received = append(received, *value)
		// Handler writes must not reach the dispatched action's stored
		// payload.
		value.Tag = "mutated-by-handler"
	})

	dispatched := actionTestSelectAction.Box(actionTestSelect{Replace: false, Tag: "dispatched"})
	binding.dispatch(dispatched, window, app)
	if len(received) != 1 || received[0] != (actionTestSelect{Replace: false, Tag: "dispatched"}) {
		t.Fatalf("typed handler received %+v, want the dispatched payload", received)
	}
	stored, err := Unbox[actionTestSelect](dispatched)
	if err != nil {
		t.Fatalf("Unbox failed: %v", err)
	}
	if stored.Tag != "dispatched" {
		t.Fatalf("dispatched stored payload was mutated by the handler: %+v", stored)
	}
}

func TestActionBindingSetRouting(t *testing.T) {
	window := &Window{}
	app := NewTestApp().App()

	var selectCalls, unitCalls int
	set := &actionBindingSet{}
	set.add(bindTypedAction[actionTestSelect](func(*actionTestSelect, *Window, *App) { selectCalls++ }))
	set.add(bindBoxedAction(actionTestUnitAction.Box(actionTestUnit{}), func(BoxedAction, *Window, *App) { unitCalls++ }))
	set.add(bindTypedAction[actionTestUnit](func(*actionTestUnit, *Window, *App) { unitCalls++ }))

	set.dispatch(actionTestSelectAction.Box(actionTestSelect{}), window, app)
	if selectCalls != 1 || unitCalls != 0 {
		t.Fatalf("select dispatch routed select=%d unit=%d, want 1/0", selectCalls, unitCalls)
	}
	set.dispatch(actionTestUnitAction.Box(actionTestUnit{}), window, app)
	if selectCalls != 1 || unitCalls != 2 {
		t.Fatalf("unit dispatch routed select=%d unit=%d, want 1/2", selectCalls, unitCalls)
	}

	// No matching bindings: dispatch is a no-op, like a dispatch with no
	// listeners in the reference.
	set.dispatch(actionTestJSONlessAction.Box(actionTestJSONless{}), window, app)
	if selectCalls != 1 || unitCalls != 2 {
		t.Fatal("dispatch with no matching bindings changed routing counts")
	}

	// Bindings registered during a dispatch join later dispatches only.
	late := false
	set.add(bindTypedAction[actionTestSelect](func(*actionTestSelect, *Window, *App) {
		// Registering while the walk is in flight: the snapshot rule keeps
		// the current dispatch unchanged.
		set.add(bindTypedAction[actionTestJSONless](func(*actionTestJSONless, *Window, *App) { late = true }))
	}))
	set.dispatch(actionTestSelectAction.Box(actionTestSelect{}), window, app)
	if late {
		t.Fatal("binding registered during dispatch fired in the same dispatch")
	}
	set.dispatch(actionTestJSONlessAction.Box(actionTestJSONless{}), window, app)
	if !late {
		t.Fatal("binding registered during dispatch was never delivered")
	}
}

func TestActionBindingMisusePanics(t *testing.T) {
	expectActionPanic(t, "nil handler", func() {
		bindBoxedAction(actionTestUnitAction.Box(actionTestUnit{}), nil)
	})
	expectActionPanic(t, "nil handler", func() {
		bindTypedAction[actionTestUnit](nil)
	})
	expectActionPanic(t, "zero-value BoxedAction", func() {
		bindBoxedAction(BoxedAction{}, func(BoxedAction, *Window, *App) {})
	})
}

// ---------------------------------------------------------------------------
// Window dispatch entry points
// ---------------------------------------------------------------------------

func TestWindowDispatchTypedErrors(t *testing.T) {
	app := NewTestApp().App()
	window := &Window{}
	value := actionTestSelect{Replace: true, Tag: "dispatched"}

	// A nil window is unavailable: ErrNoWindow (the entity path uses the
	// same sentinel).
	var nilWindow *Window
	err := expectTypedActionPanic[error](t, func() {
		nilWindow.DispatchBoxed(actionTestSelectAction.Box(value), app)
	})
	if !errors.Is(err, ErrNoWindow) {
		t.Fatalf("nil window dispatch panicked with %v, want ErrNoWindow", err)
	}

	// Since ticket13, dispatch routes through the window's focus tree
	// (Window::dispatch_action): the captured payload is cloned for
	// delivery and the dispatch is deferred to the end of the effect
	// cycle; a window with no listeners dispatches to nothing without
	// erroring.
	actionTestSelectCloneCalls = 0
	boxed := actionTestSelectAction.Box(value)
	window.DispatchBoxed(boxed, app)
	if actionTestSelectCloneCalls != 2 {
		t.Fatalf("clone calls through DispatchBoxed = %d, want 2 (capture + delivery)", actionTestSelectCloneCalls)
	}
	// The deferred dispatch runs at the end of the next update cycle
	// (an empty window has an empty root-only tree; nothing panics).
	app.Update(func(*App) {})

	// Typed dispatch clones the supplied payload for delivery.
	supplied := actionTestSelect{Tag: "supplied"}
	actionTestSelectCloneCalls = 0
	window.Dispatch(actionTestSelectAction, supplied, app)
	if actionTestSelectCloneCalls != 2 {
		t.Fatalf("clone calls through typed Dispatch = %d, want 2 (capture + delivery)", actionTestSelectCloneCalls)
	}
	app.Update(func(*App) {})

	// A nil context is rejected before anything is cloned.
	expectActionPanic(t, "nil AppContext", func() {
		window.Dispatch(actionTestSelectAction, supplied, nil)
	})
	// A zero-value boxed action is invalid.
	expectActionPanic(t, "zero-value BoxedAction", func() {
		window.DispatchBoxed(BoxedAction{}, app)
	})
}

// ---------------------------------------------------------------------------
// Zero-value misuse
// ---------------------------------------------------------------------------

func TestZeroValueActionMisuse(t *testing.T) {
	var zeroAction Action[actionTestSelect]
	expectActionPanic(t, "zero-value Action", func() { zeroAction.Name() })
	expectActionPanic(t, "zero-value Action", func() { zeroAction.Box(actionTestSelect{}) })
	expectActionPanic(t, "zero-value Action", func() { zeroAction.Clone(actionTestSelect{}) })
	expectActionPanic(t, "zero-value Action", func() { zeroAction.Equal(actionTestSelect{}, actionTestSelect{}) })
	expectActionPanic(t, "zero-value Action", func() { zeroAction.Aliases() })
	expectActionPanic(t, "zero-value Action", func() { zeroAction.Schema() })

	var zeroBoxed BoxedAction
	expectActionPanic(t, "zero-value BoxedAction", func() { zeroBoxed.Name() })
	expectActionPanic(t, "zero-value BoxedAction", func() { zeroBoxed.Payload() })
	expectActionPanic(t, "zero-value BoxedAction", func() { zeroBoxed.Clone() })
	expectActionPanic(t, "zero-value BoxedAction", func() { zeroBoxed.Equal(BoxedAction{}) })
	expectActionPanic(t, "zero-value BoxedAction", func() { Unbox[actionTestSelect](zeroBoxed) })
	valid := actionTestSelectAction.Box(actionTestSelect{})
	expectActionPanic(t, "zero-value BoxedAction", func() { valid.Equal(zeroBoxed) })

	expectActionPanic(t, "nil App", func() { (*App)(nil).RegisterActions(actionTestSelectAction) })
	expectActionPanic(t, "nil App", func() { _, _ = (*App)(nil).BuildAction("x", nil) })
}
