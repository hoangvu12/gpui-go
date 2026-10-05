package actionspec

// External-package coverage of the typed action registry. Everything
// here uses only the public gpui API: the payloads, Clone/Equal and
// FromJSON callbacks, and the handlers are all declared in this
// package, which is what "external typed callbacks" means for the
// action slice.
//
// The negative compile constraint of DefineUnitAction (a non-empty
// struct payload must not compile) cannot be expressed in a Go test
// package either; it is recorded as compiler evidence in the ticket
// report ("Payload does not satisfy ~struct{}").

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gpui-go/gpui"
)

// increment is a unit action declared from an external package with the
// full external name.
type increment struct{}

var incrementAction = gpui.DefineUnitAction[increment]("counter::Increment")

// selectNext is a rich action with every service supplied by this
// package.
type selectNext struct {
	ReplaceNewest bool `json:"ReplaceNewest"`
	Amount        int  `json:"Amount"`
}

var selectNextCloneCalls int

var selectNextAction = gpui.DefineAction(gpui.ActionSpec[selectNext]{
	Name:    "counter::SelectNext",
	Aliases: []string{"counter::SelectNextOld"},
	Clone:   func(v selectNext) selectNext { selectNextCloneCalls++; return v },
	Equal:   func(a, b selectNext) bool { return a == b },
	FromJSON: func(data json.RawMessage) (selectNext, error) {
		var value selectNext
		if err := json.Unmarshal(data, &value); err != nil {
			return selectNext{}, err
		}
		return value, nil
	},
	Schema: func() any {
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"ReplaceNewest": map[string]any{"type": "boolean"},
				"Amount":        map[string]any{"type": "integer"},
			},
		}
	},
	Deprecation:   "use counter::SelectNext",
	Documentation: "Selects the next entry.",
})

// unJSONed is a rich action without FromJSON: JSON-disabled by choice.
type unJSONed struct{ Value int }

var unJSONedAction = gpui.DefineAction(gpui.ActionSpec[unJSONed]{
	Name:  "counter::UnJSONed",
	Clone: func(v unJSONed) unJSONed { return v },
	Equal: func(a, b unJSONed) bool { return a == b },
})

// collideA/collideB collide through an alias/name pair.
type collideA struct{}
type collideB struct{}

var collideAAction = gpui.DefineAction(gpui.ActionSpec[collideA]{
	Name:    "counter::A",
	Aliases: []string{"counter::Shared"},
	Clone:   func(v collideA) collideA { return v },
	Equal:   func(a, b collideA) bool { return a == b },
})

var collideBAction = gpui.DefineAction(gpui.ActionSpec[collideB]{
	Name:  "counter::Shared",
	Clone: func(v collideB) collideB { return v },
	Equal: func(a, b collideB) bool { return a == b },
})

func TestExternalDefineAndBox(t *testing.T) {
	if got := incrementAction.Name(); got != "counter::Increment" {
		t.Fatalf("unit action name = %q", got)
	}
	if got := selectNextAction.Name(); got != "counter::SelectNext" {
		t.Fatalf("rich action name = %q", got)
	}
	if aliases := selectNextAction.Aliases(); len(aliases) != 1 || aliases[0] != "counter::SelectNextOld" {
		t.Fatalf("aliases = %v", aliases)
	}

	value := selectNext{ReplaceNewest: true, Amount: 3}
	selectNextCloneCalls = 0
	boxed := selectNextAction.Box(value)
	if selectNextCloneCalls != 1 {
		t.Fatalf("clone-on-capture calls = %d, want 1", selectNextCloneCalls)
	}
	// Clone and Equal run the callbacks this package supplied.
	if got := selectNextAction.Clone(value); got != value || selectNextCloneCalls != 2 {
		t.Fatalf("external Clone callback did not run (%+v, calls=%d)", got, selectNextCloneCalls)
	}
	if !selectNextAction.Equal(value, value) || selectNextAction.Equal(value, selectNext{}) {
		t.Fatal("external Equal callback is not used")
	}

	got, err := gpui.Unbox[selectNext](boxed)
	if err != nil {
		t.Fatalf("Unbox failed: %v", err)
	}
	if got != value {
		t.Fatalf("delivered %+v, want %+v", got, value)
	}
	// Every read is a fresh clone.
	if _, err := gpui.Unbox[selectNext](boxed); err != nil {
		t.Fatalf("Unbox failed: %v", err)
	}
	if selectNextCloneCalls != 4 {
		t.Fatalf("clone calls = %d, want 4 (capture + two deliveries + Clone probe)", selectNextCloneCalls)
	}

	unitBoxed := incrementAction.Box(increment{})
	if !boxed.Equal(boxed.Clone()) {
		t.Fatal("boxed Equal rejected its own clone")
	}
	if boxed.Equal(unitBoxed) {
		t.Fatal("boxed Equal crossed payload types")
	}
	if _, err := gpui.Unbox[increment](boxed); err == nil {
		t.Fatal("Unbox with the wrong type succeeded")
	}
	if name := unitBoxed.Name(); name != "counter::Increment" {
		t.Fatalf("boxed name = %q", name)
	}
}

func TestExternalRegisterAndBuild(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	app.RegisterActions(selectNextAction, incrementAction, unJSONedAction)

	// nil payload data becomes {} (the reference's
	// params.unwrap_or_else(|| json!({}))).
	built, err := app.BuildAction("counter::SelectNext", nil)
	if err != nil {
		t.Fatalf("BuildAction failed: %v", err)
	}
	value, err := gpui.Unbox[selectNext](built)
	if err != nil {
		t.Fatalf("Unbox failed: %v", err)
	}
	if value != (selectNext{}) {
		t.Fatalf("nil data built %+v", value)
	}

	// JSON round trip: marshal an external value, build it by name,
	// unbox and compare through the external Equal callback.
	original := selectNext{ReplaceNewest: true, Amount: 7}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	built, err = app.BuildAction("counter::SelectNext", data)
	if err != nil {
		t.Fatalf("BuildAction failed: %v", err)
	}
	value, err = gpui.Unbox[selectNext](built)
	if err != nil {
		t.Fatalf("Unbox failed: %v", err)
	}
	if !selectNextAction.Equal(original, value) {
		t.Fatalf("round trip built %+v, want %+v", value, original)
	}

	// Alias resolution and the schema accessor.
	if _, err := app.BuildAction("counter::SelectNextOld", data); err != nil {
		t.Fatalf("BuildAction(alias) failed: %v", err)
	}
	if selectNextAction.Schema() == nil {
		t.Fatal("external schema callback did not run")
	}
	if incrementAction.Schema() != nil {
		t.Fatal("unit action exposes a schema")
	}
}

func TestExternalBuildErrorCategories(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()

	_, err := app.BuildAction("counter::SelectNext", nil)
	var notFound *gpui.ActionBuildError
	if !errors.As(err, &notFound) || notFound.Kind != gpui.ActionBuildNotFound {
		t.Fatalf("unregistered error = %v, want NotFound", err)
	}

	app.RegisterActions(unJSONedAction, selectNextAction)
	_, err = app.BuildAction("counter::UnJSONed", json.RawMessage(`{}`))
	var noJSON *gpui.ActionBuildError
	if !errors.As(err, &noJSON) || noJSON.Kind != gpui.ActionBuildNoJSON {
		t.Fatalf("JSON-disabled error = %v, want NoJSON", err)
	}
	if !strings.Contains(noJSON.Error(), "cannot be built from JSON") {
		t.Fatalf("diagnostic %q does not follow the reference wording", noJSON.Error())
	}

	_, err = app.BuildAction("counter::SelectNext", json.RawMessage(`not json`))
	var decode *gpui.ActionBuildError
	if !errors.As(err, &decode) || decode.Kind != gpui.ActionBuildDecode {
		t.Fatalf("decode error = %v, want Decode", err)
	}
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		t.Fatalf("decode error does not unwrap to the JSON error: %v", err)
	}

	// Wrong-type unboxing is the typed wrong-type error.
	boxed, err := app.BuildAction("counter::SelectNext", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("BuildAction failed: %v", err)
	}
	_, err = gpui.Unbox[increment](boxed)
	var typeErr *gpui.ActionTypeError
	if !errors.As(err, &typeErr) {
		t.Fatalf("Unbox error = %v, want *gpui.ActionTypeError", err)
	}
}

func TestExternalBuiltins(t *testing.T) {
	app := gpui.NewTestApp().App()

	// The builtins are seeded into every application.
	built, err := app.BuildAction("zed::NoAction", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("BuildAction(NoAction) failed: %v", err)
	}
	if !gpui.IsNoAction(built) || gpui.IsUnbind(built) {
		t.Fatal("built zed::NoAction has wrong predicates")
	}
	built, err = app.BuildAction("zed::Unbind", json.RawMessage(`"editor::NewLine"`))
	if err != nil {
		t.Fatalf("BuildAction(Unbind) failed: %v", err)
	}
	if !gpui.IsUnbind(built) || gpui.IsNoAction(built) {
		t.Fatal("built zed::Unbind has wrong predicates")
	}
	unbind, err := gpui.Unbox[gpui.Unbind](built)
	if err != nil {
		t.Fatalf("Unbox failed: %v", err)
	}
	if unbind.Action != "editor::NewLine" {
		t.Fatalf("Unbind payload = %+v", unbind)
	}
	if _, err := gpui.Unbox[gpui.NoAction](built); err == nil {
		t.Fatal("wrong-type Unbox of a builtin succeeded")
	}
}

func TestExternalDispatchTypedError(t *testing.T) {
	ta := gpui.NewTestApp()
	app := ta.App()
	window := &gpui.Window{}
	value := selectNext{ReplaceNewest: true, Amount: 5}

	// Boxed dispatch: the typed error carries a clone of the captured
	// payload.
	boxed := selectNextAction.Box(value)
	dispatchErr := recoverDispatch(t, func() { window.DispatchBoxed(boxed, app) })
	delivered, err := gpui.Unbox[selectNext](dispatchErr.Action)
	if err != nil {
		t.Fatalf("Unbox failed: %v", err)
	}
	if delivered != value {
		t.Fatalf("dispatched action carried %+v, want %+v", delivered, value)
	}

	// Typed dispatch: the typed error carries a clone of the supplied
	// payload.
	supplied := selectNext{Amount: 9}
	dispatchErr = recoverDispatch(t, func() { window.Dispatch(selectNextAction, supplied, app) })
	delivered, err = gpui.Unbox[selectNext](dispatchErr.Action)
	if err != nil {
		t.Fatalf("Unbox failed: %v", err)
	}
	if delivered != supplied {
		t.Fatalf("typed dispatch carried %+v, want %+v", delivered, supplied)
	}
}

func TestExternalDefinitionAndRegistrationDiagnostics(t *testing.T) {
	// A different definition for an externally owned payload type is the
	// typed redefinition error.
	conflicting := gpui.ActionSpec[selectNext]{
		Name:  "counter::SelectNextOther",
		Clone: func(v selectNext) selectNext { return v },
		Equal: func(a, b selectNext) bool { return a == b },
	}
	err := recoverDefinition(t, func() { gpui.DefineAction(conflicting) })
	if err.Kind != gpui.ActionDefinitionRedefinition {
		t.Fatalf("Kind = %v, want Redefinition", err.Kind)
	}
	for _, want := range []string{"counter::SelectNext", "counter::SelectNextOther"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("diagnostic %q does not name %q", err.Error(), want)
		}
	}

	// Rich definitions from outside require Clone and Equal too.
	missing := gpui.ActionSpec[unJSONed]{
		Name:  "counter::Missing",
		Equal: func(a, b unJSONed) bool { return a == b },
	}
	err = recoverDefinition(t, func() { gpui.DefineAction(missing) })
	if err.Kind != gpui.ActionDefinitionMissingClone {
		t.Fatalf("Kind = %v, want MissingClone", err.Kind)
	}

	// Registration conflicts are typed setup errors, whichever side
	// registers first.
	app := gpui.NewTestApp().App()
	app.RegisterActions(collideAAction)
	registration := recoverRegistration(t, func() { app.RegisterActions(collideBAction) })
	if registration.Kind != gpui.ActionRegistrationNameConflict {
		t.Fatalf("Kind = %v, want NameConflict", registration.Kind)
	}
	other := gpui.NewTestApp().App()
	other.RegisterActions(collideBAction)
	registration = recoverRegistration(t, func() { other.RegisterActions(collideAAction) })
	if registration.Kind != gpui.ActionRegistrationAliasConflict {
		t.Fatalf("Kind = %v, want AliasConflict", registration.Kind)
	}
}

// recoverDispatch runs f and returns the typed dispatch error it panics
// with.
func recoverDispatch(t *testing.T, f func()) *gpui.ActionDispatchError {
	t.Helper()
	var caught *gpui.ActionDispatchError
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				value, ok := r.(*gpui.ActionDispatchError)
				if !ok {
					t.Fatalf("dispatch panicked with %T (%v), want *gpui.ActionDispatchError", r, r)
				}
				caught = value
				panicked = true
			}
		}()
		f()
	}()
	if !panicked {
		t.Fatal("dispatch did not panic with the typed no-route error")
	}
	return caught
}

func recoverDefinition(t *testing.T, f func()) *gpui.ActionDefinitionError {
	t.Helper()
	var caught *gpui.ActionDefinitionError
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				value, ok := r.(*gpui.ActionDefinitionError)
				if !ok {
					t.Fatalf("define panicked with %T (%v), want *gpui.ActionDefinitionError", r, r)
				}
				caught = value
				panicked = true
			}
		}()
		f()
	}()
	if !panicked {
		t.Fatal("define did not panic with the typed definition error")
	}
	return caught
}

func recoverRegistration(t *testing.T, f func()) *gpui.ActionRegistrationError {
	t.Helper()
	var caught *gpui.ActionRegistrationError
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				value, ok := r.(*gpui.ActionRegistrationError)
				if !ok {
					t.Fatalf("registration panicked with %T (%v), want *gpui.ActionRegistrationError", r, r)
				}
				caught = value
				panicked = true
			}
		}()
		f()
	}()
	if !panicked {
		t.Fatal("registration did not panic with the typed registration error")
	}
	return caught
}
