package gpui

// Ports of the pinned CE keymap and key-context unit tests
// (254b5dbd47cbb5acbcc5bbdcbb322a339276c88a):
// crates/gpui/src/keymap.rs `mod tests` and
// crates/gpui/src/keymap/context.rs `mod tests` (the parse cases).

import (
	"testing"
)

type testActionAlpha struct{}
type testActionBeta struct{}
type testActionGamma struct{}
type testActionDelta struct{}

var (
	testActionAlphaAction = DefineUnitAction[testActionAlpha]("test_only::ActionAlpha")
	testActionBetaAction  = DefineUnitAction[testActionBeta]("test_only::ActionBeta")
	testActionGammaAction = DefineUnitAction[testActionGamma]("test_only::ActionGamma")
	testActionDeltaAction = DefineUnitAction[testActionDelta]("test_only::ActionDelta")
)

func testContexts(sources ...string) []KeyContext {
	out := make([]KeyContext, 0, len(sources))
	for _, source := range sources {
		out = append(out, MustParseKeyContext(source))
	}
	return out
}

func bindingFor(keystrokes string, action BoxedAction, context string) KeyBinding {
	var ctx *string
	if context != "" {
		ctx = &context
	}
	return NewKeyBinding(keystrokes, action, ctx)
}

func equalActions(t *testing.T, have []KeyBinding, want ...BoxedAction) {
	t.Helper()
	if len(have) != len(want) {
		t.Fatalf("bindings = %d, want %d", len(have), len(want))
	}
	for i := range have {
		if !have[i].action.Equal(want[i]) {
			t.Fatalf("binding[%d] action = %s, want %s", i, have[i].action.Name(), want[i].Name())
		}
	}
}

// TestKeymapBindingEnabled ports keymap.rs test_keymap.
func TestKeymapBindingEnabled(t *testing.T) {
	bindings := []KeyBinding{
		bindingFor("ctrl-a", testActionAlphaAction.Box(testActionAlpha{}), ""),
		bindingFor("ctrl-a", testActionBetaAction.Box(testActionBeta{}), "pane"),
		bindingFor("ctrl-a", testActionGammaAction.Box(testActionGamma{}), "editor && mode==full"),
	}
	keymap := NewKeymap(bindings)

	// Global bindings are enabled in all contexts.
	if got := keymap.bindingEnabled(bindings[0], nil); got != 0 {
		t.Errorf("bindingEnabled(global, no contexts) = %d, want 0", got)
	}
	if got := keymap.bindingEnabled(bindings[0], testContexts("terminal")); got != 1 {
		t.Errorf("bindingEnabled(global, 1 context) = %d, want 1", got)
	}

	// Contextual bindings are enabled in contexts that match their
	// predicate.
	if got := keymap.bindingEnabled(bindings[1], testContexts("barf x=y")); got != -1 {
		t.Errorf("bindingEnabled(pane, barf) = %d, want -1", got)
	}
	if got := keymap.bindingEnabled(bindings[1], testContexts("pane x=y")); got != 1 {
		t.Errorf("bindingEnabled(pane, pane x=y) = %d, want 1", got)
	}

	if got := keymap.bindingEnabled(bindings[2], testContexts("editor")); got != -1 {
		t.Errorf("bindingEnabled(editor&&mode, editor) = %d, want -1", got)
	}
	if got := keymap.bindingEnabled(bindings[2], testContexts("editor mode=full")); got != 1 {
		t.Errorf("bindingEnabled(editor&&mode, editor mode=full) = %d, want 1", got)
	}
}

// TestKeymapDepthPrecedence ports test_depth_precedence.
func TestKeymapDepthPrecedence(t *testing.T) {
	keymap := NewKeymap([]KeyBinding{
		bindingFor("ctrl-a", testActionBetaAction.Box(testActionBeta{}), "pane"),
		bindingFor("ctrl-a", testActionGammaAction.Box(testActionGamma{}), "editor"),
	})

	bindings, pending := keymap.BindingsForInput(
		[]Keystroke{MustParseKeystroke("ctrl-a")},
		testContexts("pane", "editor"))

	if pending {
		t.Fatal("pending = true, want false")
	}
	equalActions(t, bindings,
		testActionGammaAction.Box(testActionGamma{}),
		testActionBetaAction.Box(testActionBeta{}))
}

// TestKeymapDisabled ports test_keymap_disabled.
func TestKeymapDisabled(t *testing.T) {
	keymap := NewKeymap([]KeyBinding{
		bindingFor("ctrl-a", testActionAlphaAction.Box(testActionAlpha{}), "editor"),
		bindingFor("ctrl-b", testActionAlphaAction.Box(testActionAlpha{}), "editor"),
		bindingFor("ctrl-a", NoActionDescriptor.Box(NoAction{}), "editor && mode==full"),
		bindingFor("ctrl-b", NoActionDescriptor.Box(NoAction{}), ""),
	})

	if got, _ := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("ctrl-a")}, testContexts("barf")); len(got) != 0 {
		t.Fatalf("ctrl-a in barf = %d bindings, want 0", len(got))
	}
	if got, _ := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("ctrl-a")}, testContexts("editor")); len(got) == 0 {
		t.Fatal("ctrl-a in editor produced no bindings")
	}
	if got, _ := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("ctrl-a")}, testContexts("editor mode=full")); len(got) != 0 {
		t.Fatalf("ctrl-a in editor mode=full = %d bindings, want 0 (NoAction)", len(got))
	}
	if got, _ := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("ctrl-b")}, testContexts("barf")); len(got) != 0 {
		t.Fatalf("ctrl-b globally disabled = %d bindings, want 0", len(got))
	}
}

// TestKeymapMultipleKeystrokeDisabled ports
// test_multiple_keystroke_binding_disabled (zed#30259).
func TestKeymapMultipleKeystrokeDisabled(t *testing.T) {
	space := MustParseKeystroke("space")
	w := MustParseKeystroke("w")

	keymap := NewKeymap([]KeyBinding{
		bindingFor("space w w", testActionAlphaAction.Box(testActionAlpha{}), "workspace"),
		bindingFor("space w w", NoActionDescriptor.Box(NoAction{}), "editor"),
	})

	// `space` and `space w` result in pending input on the workspace,
	// but not the editor (the NoAction removes the pending chord).
	got, pending := keymap.BindingsForInput([]Keystroke{space}, testContexts("workspace"))
	if len(got) != 0 || !pending {
		t.Fatalf("space on workspace: %d bindings, pending=%v, want 0+pending", len(got), pending)
	}
	got, pending = keymap.BindingsForInput([]Keystroke{space}, testContexts("workspace", "editor"))
	if len(got) != 0 || pending {
		t.Fatalf("space on workspace+editor: %d bindings, pending=%v, want 0+not-pending", len(got), pending)
	}
	for _, contexts := range [2][]KeyContext{testContexts("workspace"), testContexts("workspace", "editor")} {
		got, pending = keymap.BindingsForInput([]Keystroke{space, w}, contexts)
		if len(got) != 0 {
			t.Fatalf("space w in %v: %d bindings, want 0", contexts, len(got))
		}
		wantPending := len(contexts) == 1
		if pending != wantPending {
			t.Fatalf("space w in %v: pending=%v, want %v", contexts, pending, wantPending)
		}
	}

	// `space w w` resolves on the workspace, not the editor.
	got, pending = keymap.BindingsForInput([]Keystroke{space, w, w}, testContexts("workspace"))
	if len(got) != 1 || pending {
		t.Fatalf("space w w on workspace: %d bindings, pending=%v", len(got), pending)
	}
	got, _ = keymap.BindingsForInput([]Keystroke{space, w, w}, testContexts("workspace", "editor"))
	if len(got) != 0 {
		t.Fatalf("space w w on editor: %d bindings, want 0", len(got))
	}

	// Another binding AFTER the NoAction still results in pending.
	keymap = NewKeymap([]KeyBinding{
		bindingFor("space w w", testActionAlphaAction.Box(testActionAlpha{}), "workspace"),
		bindingFor("space w w", NoActionDescriptor.Box(NoAction{}), "editor"),
		bindingFor("space w x", testActionAlphaAction.Box(testActionAlpha{}), "editor"),
	})
	_, pending = keymap.BindingsForInput([]Keystroke{space}, testContexts("workspace", "editor"))
	if !pending {
		t.Fatal("space with a later editor binding: pending=false, want true")
	}

	// Another binding BEFORE the NoAction.
	keymap = NewKeymap([]KeyBinding{
		bindingFor("space w w", testActionAlphaAction.Box(testActionAlpha{}), "workspace"),
		bindingFor("space w x", testActionAlphaAction.Box(testActionAlpha{}), "editor"),
		bindingFor("space w w", NoActionDescriptor.Box(NoAction{}), "editor"),
	})
	_, pending = keymap.BindingsForInput([]Keystroke{space}, testContexts("workspace", "editor"))
	if !pending {
		t.Fatal("space with an earlier editor binding: pending=false, want true")
	}

	// Another binding at a higher context.
	keymap = NewKeymap([]KeyBinding{
		bindingFor("space w w", testActionAlphaAction.Box(testActionAlpha{}), "workspace"),
		bindingFor("space w x", testActionAlphaAction.Box(testActionAlpha{}), "workspace"),
		bindingFor("space w w", NoActionDescriptor.Box(NoAction{}), "editor"),
	})
	_, pending = keymap.BindingsForInput([]Keystroke{space}, testContexts("workspace", "editor"))
	if !pending {
		t.Fatal("space with a higher-context binding: pending=false, want true")
	}
}

// TestKeymapOverrideMultikey ports test_override_multikey.
func TestKeymapOverrideMultikey(t *testing.T) {
	keymap := NewKeymap([]KeyBinding{
		bindingFor("ctrl-w left", testActionAlphaAction.Box(testActionAlpha{}), "editor"),
		bindingFor("ctrl-w", NoActionDescriptor.Box(NoAction{}), "editor"),
	})
	got, pending := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("ctrl-w")}, testContexts("editor"))
	if len(got) != 0 || !pending {
		t.Fatalf("ctrl-w with NoAction: %d bindings, pending=%v, want 0+pending", len(got), pending)
	}

	keymap = NewKeymap([]KeyBinding{
		bindingFor("ctrl-w left", testActionAlphaAction.Box(testActionAlpha{}), "editor"),
		bindingFor("ctrl-w", testActionBetaAction.Box(testActionBeta{}), "editor"),
	})
	got, pending = keymap.BindingsForInput([]Keystroke{MustParseKeystroke("ctrl-w")}, testContexts("editor"))
	if len(got) != 1 || pending {
		t.Fatalf("ctrl-w with an override: %d bindings, pending=%v, want 1+not-pending", len(got), pending)
	}
}

// TestKeymapSimpleDisable ports test_simple_disable.
func TestKeymapSimpleDisable(t *testing.T) {
	keymap := NewKeymap([]KeyBinding{
		bindingFor("ctrl-x", testActionAlphaAction.Box(testActionAlpha{}), "editor"),
		bindingFor("ctrl-x", NoActionDescriptor.Box(NoAction{}), "editor"),
	})
	got, pending := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("ctrl-x")}, testContexts("editor"))
	if len(got) != 0 || pending {
		t.Fatalf("ctrl-x disabled: %d bindings, pending=%v, want 0+not-pending", len(got), pending)
	}
}

// TestKeymapDisableWeakerSourcesOnly ports test_disable_weaker_sources_only.
func TestKeymapDisableWeakerSourcesOnly(t *testing.T) {
	const (
		user  = KeyBindingMetaIndex(0)
		vim   = KeyBindingMetaIndex(1)
		base  = KeyBindingMetaIndex(2)
		deflt = KeyBindingMetaIndex(3)
	)
	ctrlX := []Keystroke{MustParseKeystroke("ctrl-x")}
	editor := testContexts("editor")
	workspaceEditor := testContexts("workspace", "editor")

	// A base keymap null disables a default binding in the same context.
	keymap := NewKeymap(nil)
	keymap.AddBindings([]KeyBinding{
		bindingFor("ctrl-x", testActionAlphaAction.Box(testActionAlpha{}), "editor").WithMeta(deflt),
		bindingFor("ctrl-x", NoActionDescriptor.Box(NoAction{}), "editor").WithMeta(base),
	})
	if got, _ := keymap.BindingsForInput(ctrlX, editor); len(got) != 0 {
		t.Fatalf("base null vs default: %d bindings, want 0", len(got))
	}

	// A user binding is not affected by base keymap or default nulls.
	keymap = NewKeymap(nil)
	keymap.AddBindings([]KeyBinding{
		bindingFor("ctrl-x", NoActionDescriptor.Box(NoAction{}), "editor").WithMeta(deflt),
		bindingFor("ctrl-x", NoActionDescriptor.Box(NoAction{}), "editor").WithMeta(base),
		bindingFor("ctrl-x", testActionBetaAction.Box(testActionBeta{}), "").WithMeta(user),
	})
	got, _ := keymap.BindingsForInput(ctrlX, editor)
	equalActions(t, got, testActionBetaAction.Box(testActionBeta{}))

	// A user binding at a shallower context is not disabled by a deeper
	// base keymap null.
	keymap = NewKeymap(nil)
	keymap.AddBindings([]KeyBinding{
		bindingFor("ctrl-x", NoActionDescriptor.Box(NoAction{}), "editor").WithMeta(base),
		bindingFor("ctrl-x", testActionBetaAction.Box(testActionBeta{}), "workspace").WithMeta(user),
	})
	got, _ = keymap.BindingsForInput(ctrlX, workspaceEditor)
	equalActions(t, got, testActionBetaAction.Box(testActionBeta{}))

	// A vim binding survives a base keymap null, and a user null
	// disables everything.
	keymap = NewKeymap(nil)
	keymap.AddBindings([]KeyBinding{
		bindingFor("ctrl-x", testActionAlphaAction.Box(testActionAlpha{}), "editor").WithMeta(deflt),
		bindingFor("ctrl-x", NoActionDescriptor.Box(NoAction{}), "editor").WithMeta(base),
		bindingFor("ctrl-x", testActionGammaAction.Box(testActionGamma{}), "editor").WithMeta(vim),
	})
	got, _ = keymap.BindingsForInput(ctrlX, editor)
	equalActions(t, got, testActionGammaAction.Box(testActionGamma{}))

	keymap = NewKeymap(nil)
	keymap.AddBindings([]KeyBinding{
		bindingFor("ctrl-x", testActionAlphaAction.Box(testActionAlpha{}), "editor").WithMeta(deflt),
		bindingFor("ctrl-x", testActionGammaAction.Box(testActionGamma{}), "editor").WithMeta(vim),
		bindingFor("ctrl-x", NoActionDescriptor.Box(NoAction{}), "editor").WithMeta(user),
	})
	if got, _ := keymap.BindingsForInput(ctrlX, editor); len(got) != 0 {
		t.Fatalf("user null: %d bindings, want 0", len(got))
	}
}

// TestKeymapFailToDisable ports test_fail_to_disable.
func TestKeymapFailToDisable(t *testing.T) {
	keymap := NewKeymap([]KeyBinding{
		bindingFor("ctrl-x", testActionAlphaAction.Box(testActionAlpha{}), "editor"),
		bindingFor("ctrl-x", NoActionDescriptor.Box(NoAction{}), "workspace"),
	})
	got, pending := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("ctrl-x")}, testContexts("workspace", "editor"))
	if len(got) != 1 || pending {
		t.Fatalf("wrong-level disable: %d bindings, pending=%v, want 1+not-pending", len(got), pending)
	}
}

// TestKeymapDisableDeeper ports test_disable_deeper.
func TestKeymapDisableDeeper(t *testing.T) {
	keymap := NewKeymap([]KeyBinding{
		bindingFor("ctrl-x", testActionAlphaAction.Box(testActionAlpha{}), "workspace"),
		bindingFor("ctrl-x", NoActionDescriptor.Box(NoAction{}), "editor"),
	})
	got, pending := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("ctrl-x")}, testContexts("workspace", "editor"))
	if len(got) != 0 || pending {
		t.Fatalf("deeper disable: %d bindings, pending=%v, want 0+not-pending", len(got), pending)
	}
}

// TestKeymapPendingMatchEnabled ports test_pending_match_enabled.
func TestKeymapPendingMatchEnabled(t *testing.T) {
	keymap := NewKeymap([]KeyBinding{
		bindingFor("ctrl-x", testActionBetaAction.Box(testActionBeta{}), "vim_mode == normal"),
		bindingFor("ctrl-x 0", testActionAlphaAction.Box(testActionAlpha{}), "Workspace"),
	})
	got, pending := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("ctrl-x")}, testContexts("Workspace", "Pane", "Editor vim_mode=normal"))
	equalActions(t, got, testActionBetaAction.Box(testActionBeta{}))
	if !pending {
		t.Fatal("pending = false, want true")
	}
}

// TestKeymapPendingMatchEnabledExtended ports
// test_pending_match_enabled_extended.
func TestKeymapPendingMatchEnabledExtended(t *testing.T) {
	keymap := NewKeymap([]KeyBinding{
		bindingFor("ctrl-x", testActionBetaAction.Box(testActionBeta{}), "vim_mode == normal"),
		bindingFor("ctrl-x 0", NoActionDescriptor.Box(NoAction{}), "Workspace"),
	})
	got, pending := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("ctrl-x")}, testContexts("Workspace", "Pane", "Editor vim_mode=normal"))
	equalActions(t, got, testActionBetaAction.Box(testActionBeta{}))
	if pending {
		t.Fatal("pending = true, want false")
	}

	keymap = NewKeymap([]KeyBinding{
		bindingFor("ctrl-x", testActionBetaAction.Box(testActionBeta{}), "Workspace"),
		bindingFor("ctrl-x 0", NoActionDescriptor.Box(NoAction{}), "vim_mode == normal"),
	})
	got, pending = keymap.BindingsForInput([]Keystroke{MustParseKeystroke("ctrl-x")}, testContexts("Workspace", "Pane", "Editor vim_mode=normal"))
	equalActions(t, got, testActionBetaAction.Box(testActionBeta{}))
	if pending {
		t.Fatal("pending = true, want false")
	}
}

// TestKeymapOverridingPrefix ports test_overriding_prefix.
func TestKeymapOverridingPrefix(t *testing.T) {
	keymap := NewKeymap([]KeyBinding{
		bindingFor("ctrl-x 0", testActionAlphaAction.Box(testActionAlpha{}), "Workspace"),
		bindingFor("ctrl-x", testActionBetaAction.Box(testActionBeta{}), "vim_mode == normal"),
	})
	got, pending := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("ctrl-x")}, testContexts("Workspace", "Pane", "Editor vim_mode=normal"))
	equalActions(t, got, testActionBetaAction.Box(testActionBeta{}))
	if pending {
		t.Fatal("pending = true, want false")
	}
}

// TestKeymapContextPrecedenceWithSameSource ports
// test_context_precedence_with_same_source.
func TestKeymapContextPrecedenceWithSameSource(t *testing.T) {
	keymap := NewKeymap([]KeyBinding{
		bindingFor("cmd-r", testActionAlphaAction.Box(testActionAlpha{}), "Workspace"),
		bindingFor("cmd-r", testActionBetaAction.Box(testActionBeta{}), "Editor"),
	})
	got, _ := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("cmd-r")}, testContexts("Workspace", "Editor"))
	equalActions(t, got,
		testActionBetaAction.Box(testActionBeta{}),
		testActionAlphaAction.Box(testActionAlpha{}))
}

// TestKeymapBindingsForAction ports test_bindings_for_action.
func TestKeymapBindingsForAction(t *testing.T) {
	keymap := NewKeymap([]KeyBinding{
		bindingFor("ctrl-a", testActionAlphaAction.Box(testActionAlpha{}), "pane"),
		bindingFor("ctrl-b", testActionBetaAction.Box(testActionBeta{}), "editor && mode == full"),
		bindingFor("ctrl-c", testActionGammaAction.Box(testActionGamma{}), "workspace"),
		bindingFor("ctrl-a", NoActionDescriptor.Box(NoAction{}), "pane && active"),
		bindingFor("ctrl-b", NoActionDescriptor.Box(NoAction{}), "editor"),
	})

	assertBindings := func(action BoxedAction, want ...string) {
		t.Helper()
		got := keymap.BindingsForAction(action)
		if len(got) != len(want) {
			t.Fatalf("BindingsForAction(%s) = %d, want %d", action.Name(), len(got), len(want))
		}
		for i := range got {
			if got[i].keystrokes[0].Unparse() != want[i] {
				t.Fatalf("binding[%d] = %s, want %s", i, got[i].keystrokes[0].Unparse(), want[i])
			}
		}
	}
	assertBindings(testActionAlphaAction.Box(testActionAlpha{}), "ctrl-a")
	assertBindings(testActionBetaAction.Box(testActionBeta{}))
	assertBindings(testActionGammaAction.Box(testActionGamma{}), "ctrl-c")
}

// TestKeymapTargetedUnbindIgnoresTargetContext ports
// test_targeted_unbind_ignores_target_context.
func TestKeymapTargetedUnbindIgnoresTargetContext(t *testing.T) {
	keymap := NewKeymap([]KeyBinding{
		bindingFor("tab", testActionAlphaAction.Box(testActionAlpha{}), "Editor"),
		bindingFor("tab", testActionBetaAction.Box(testActionBeta{}), "Editor && showing_completions"),
		bindingFor("tab", UnbindDescriptor.Box(Unbind{Action: "test_only::ActionAlpha"}), "Editor && edit_prediction"),
	})
	got, pending := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("tab")}, testContexts("Editor showing_completions edit_prediction"))
	if pending {
		t.Fatal("pending = true, want false")
	}
	equalActions(t, got, testActionBetaAction.Box(testActionBeta{}))
}

// TestKeymapBindingsForActionUnbindScopes ports
// test_bindings_for_action_keeps_binding_for_narrower_targeted_unbind
// and
// test_bindings_for_action_removes_binding_for_broader_targeted_unbind.
func TestKeymapBindingsForActionUnbindScopes(t *testing.T) {
	keymap := NewKeymap([]KeyBinding{
		bindingFor("tab", testActionAlphaAction.Box(testActionAlpha{}), "Editor"),
		bindingFor("tab", UnbindDescriptor.Box(Unbind{Action: "test_only::ActionAlpha"}), "Editor && edit_prediction"),
		bindingFor("tab", testActionBetaAction.Box(testActionBeta{}), "Editor && showing_completions"),
	})
	if got := keymap.BindingsForAction(testActionAlphaAction.Box(testActionAlpha{})); len(got) != 1 {
		t.Fatalf("narrower unbind: alpha bindings = %d, want 1", len(got))
	}
	if got := keymap.BindingsForAction(testActionBetaAction.Box(testActionBeta{})); len(got) != 1 {
		t.Fatalf("narrower unbind: beta bindings = %d, want 1", len(got))
	}

	keymap = NewKeymap([]KeyBinding{
		bindingFor("tab", testActionAlphaAction.Box(testActionAlpha{}), "Editor && edit_prediction"),
		bindingFor("tab", UnbindDescriptor.Box(Unbind{Action: "test_only::ActionAlpha"}), "Editor"),
	})
	if got := keymap.BindingsForAction(testActionAlphaAction.Box(testActionAlpha{})); len(got) != 0 {
		t.Fatalf("broader unbind: bindings = %d, want 0", len(got))
	}
}

// TestKeymapSourcePrecedenceSorting ports test_source_precedence_sorting.
func TestKeymapSourcePrecedenceSorting(t *testing.T) {
	keymap := NewKeymap(nil)
	keymap.AddBindings([]KeyBinding{
		bindingFor("cmd-r", testActionAlphaAction.Box(testActionAlpha{}), "Editor").WithMeta(3),
		bindingFor("cmd-r", testActionBetaAction.Box(testActionBeta{}), "Editor").WithMeta(0),
	})
	got, _ := keymap.BindingsForInput([]Keystroke{MustParseKeystroke("cmd-r")}, testContexts("Editor"))
	equalActions(t, got,
		testActionBetaAction.Box(testActionBeta{}),
		testActionAlphaAction.Box(testActionAlpha{}))
}

// ---------------------------------------------------------------------------
// KeyContext and predicate parse tests (context.rs mod tests)
// ---------------------------------------------------------------------------

// TestParseContext ports test_parse_context.
func TestParseContext(t *testing.T) {
	expected := KeyContext{}
	expected.Add("baz")
	expected.Set("foo", "bar")

	cases := []string{"baz foo=bar", "baz foo = bar", "  baz foo   =   bar baz", " baz foo = bar"}
	for _, source := range cases {
		if got := MustParseKeyContext(source); !got.Equal(expected) {
			t.Errorf("ParseKeyContext(%q) = %v, want %v", source, got, expected)
		}
	}
}

// TestParsePredicateIdentifiers ports test_parse_identifiers.
func TestParsePredicateIdentifiers(t *testing.T) {
	for source, want := range map[string]KeyBindingContextPredicate{
		"abc12": {Kind: PredicateIdentifier, Identifier: "abc12"},
		"_1a":   {Kind: PredicateIdentifier, Identifier: "_1a"},
	} {
		got, err := ParseKeyBindingContextPredicate(source)
		if err != nil {
			t.Fatalf("parse %q: %v", source, err)
		}
		if got.Kind != want.Kind || got.Identifier != want.Identifier {
			t.Errorf("parse %q = %+v, want %+v", source, got, want)
		}
	}
}

// TestParsePredicateNegations ports test_parse_negations.
func TestParsePredicateNegations(t *testing.T) {
	got, err := ParseKeyBindingContextPredicate("!abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != PredicateNot || got.Children[0].Identifier != "abc" {
		t.Errorf("parse !abc = %+v", got)
	}
	got, err = ParseKeyBindingContextPredicate(" ! ! abc")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != PredicateNot || got.Children[0].Kind != PredicateNot || got.Children[0].Children[0].Identifier != "abc" {
		t.Errorf("parse ! ! abc = %+v", got)
	}
}

// TestParsePredicateEquality ports test_parse_equality_operators.
func TestParsePredicateEquality(t *testing.T) {
	got, err := ParseKeyBindingContextPredicate("a == b")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != PredicateEqual || got.Identifier != "a" || got.Value != "b" {
		t.Errorf("parse a == b = %+v", got)
	}
	got, err = ParseKeyBindingContextPredicate("c!=d")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != PredicateNotEqual || got.Identifier != "c" || got.Value != "d" {
		t.Errorf("parse c!=d = %+v", got)
	}
	if _, err := ParseKeyBindingContextPredicate("c == !d"); err == nil {
		t.Error("operands of == must be identifiers")
	}
}

// TestParsePredicateBooleanOperators ports
// test_parse_boolean_operators.
func TestParsePredicateBooleanOperators(t *testing.T) {
	got, err := ParseKeyBindingContextPredicate("a || b")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != PredicateOr || got.Children[0].Identifier != "a" || got.Children[1].Identifier != "b" {
		t.Errorf("parse a || b = %+v", got)
	}

	got, err = ParseKeyBindingContextPredicate("a || !b && c")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != PredicateOr ||
		got.Children[0].Identifier != "a" ||
		got.Children[1].Kind != PredicateAnd ||
		got.Children[1].Children[0].Kind != PredicateNot ||
		got.Children[1].Children[1].Identifier != "c" {
		t.Errorf("parse a || !b && c = %+v", got)
	}

	got, err = ParseKeyBindingContextPredicate("a && b || c&&d")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != PredicateOr || got.Children[0].Kind != PredicateAnd || got.Children[1].Kind != PredicateAnd {
		t.Errorf("parse a && b || c&&d = %+v", got)
	}

	got, err = ParseKeyBindingContextPredicate("a == b && c || d == e && f")
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != PredicateOr ||
		got.Children[0].Kind != PredicateAnd ||
		got.Children[0].Children[0].Kind != PredicateEqual ||
		got.Children[0].Children[1].Identifier != "c" ||
		got.Children[1].Kind != PredicateAnd ||
		got.Children[1].Children[0].Kind != PredicateEqual ||
		got.Children[1].Children[1].Identifier != "f" {
		t.Errorf("parse a == b && c || d == e && f = %+v", got)
	}
}

// TestKeybindingKeystrokeMatchKeystrokes ports binding.rs match tests
// (via the keystroke should-match Windows branch).
func TestKeybindingKeystrokeMatchKeystrokes(t *testing.T) {
	binding := MustKeyBinding("ctrl-a", testActionAlphaAction.Box(testActionAlpha{}), "")

	if _, _, matched := func() (bool, bool, bool) {
		matched, pending := binding.MatchKeystrokes(nil)
		return matched, pending, true
	}(); !matched {
		t.Error("empty typed must prefix-match")
	}
	matched, pending := binding.MatchKeystrokes([]Keystroke{MustParseKeystroke("ctrl-a")})
	if !matched || pending {
		t.Errorf("exact match = %v/%v, want matched/not-pending", matched, pending)
	}
	matched, pending = binding.MatchKeystrokes([]Keystroke{MustParseKeystroke("ctrl-a"), MustParseKeystroke("b")})
	if matched || pending {
		t.Errorf("longer typed = %v/%v, want no-match", matched, pending)
	}
}
