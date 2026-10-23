package base

// Editor diagnostics / LSP state tests: the ported diagnostics test of
// U:input/base/state.rs (4898-4955) and the full column-selection test
// (7568-7642, its cached hover-definition step included), plus Go-own
// tests that drive each provider hook through a real test window. The
// harness is textarea_state_test.go's.

import (
	"errors"
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// readyTask is a provider's Task::ready(Ok(value)).
func readyTask[T any](app *gpui.App, value T) gpui.Task[ProviderResult[T]] {
	return app.Spawn(func(*gpui.AsyncApp) ProviderResult[T] { return ProviderResult[T]{Value: value} })
}

// emptyEditorRoot is the test's `gpui::EmptyView` root: it creates the
// editor without rendering it.
type emptyEditorRoot struct{}

func (*emptyEditorRoot) Render(*gpui.Window, *gpui.Context[emptyEditorRoot]) gpui.AnyElement {
	return gpui.Div().IntoElement()
}

// U:input/base/state.rs:4898-4955
func TestEditorNoopScrollNotifiesDiagnosticDismissal(t *testing.T) {
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	ta.Update(func(app *gpui.App) {
		theme := DefaultTheme()
		app.SetGlobal(&theme)
		inputInit(app)
	})
	var input gpui.Entity[EditorState]
	opts := gpui.WindowOptions{Bounds: gpui.Bounds{Size: gpui.Size{Width: 400, Height: 100}}}
	tw := ta.OpenWindowWithOptions[emptyEditorRoot](opts, func(_ *emptyEditorRoot, w *gpui.Window, cx *gpui.Context[emptyEditorRoot]) {
		input = gpui.NewEntity(cx, w.Scope(), func(s *EditorState, scx *gpui.Context[InputBaseState[EditorMode]]) { *s = NewEditorState(w, scx) })
	})
	v := &inputView[EditorMode]{input: input, cx: gpui.NewVisualTestContext(tw.Window(), ta)}
	v.update(func(s *EditorState, _ *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.PresentDiagnostic(DiagnosticEntry{}, cx)
	})
	notifications := observeInput(v.cx, v.input)
	*notifications = 0
	scroll := &gpui.ScrollWheelEvent{Delta: gpui.ScrollDeltaPixels(0, -40)}

	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.inputBounds = gpui.Bounds{Size: gpui.Size{Width: 100, Height: 20}}
		s.scrollSize = gpui.Size{Width: 100, Height: 20}
		s.onScrollWheel(scroll, w, cx)
		if s.scrollHandle.Offset() != (gpui.Point{}) {
			t.Fatalf("offset = %v, want clamped to zero", s.scrollHandle.Offset())
		}
		if s.DiagnosticPopover() != nil {
			t.Fatal("scrolling must hide the diagnostic popover")
		}
	})
	if *notifications != 1 {
		t.Fatalf("notifications = %d: clearing a diagnostic must notify even when scrolling is clamped", *notifications)
	}
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.onScrollWheel(scroll, w, cx)
	})
	if *notifications != 1 {
		t.Fatalf("notifications = %d: an unchanged input must stay quiet", *notifications)
	}
}

// U:input/base/state.rs:7568-7642, in full: a cached Ctrl-hover
// definition must not steal a column gesture.
func TestEditorAltDragIgnoresACachedHoverDefinition(t *testing.T) {
	v := buildEditor(t)
	mods := []gpui.Modifiers{{Alt: true}, {Alt: true, Shift: true}}
	if isLinux() {
		mods = append(mods, gpui.Modifiers{Alt: true, Control: true})
	}
	for _, modifiers := range mods {
		setupCursors(v, "|abcd\nabcd\nabcd")
		v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) { s.Focus(w, cx) })
		var start, end gpui.Point
		v.read(func(s *EditorState) {
			start = columnPosition(s, 0, 1, 0.5)
			end = columnPosition(s, 2, 3, 2.5)
			end.X -= 0.5
		})
		v.update(func(s *EditorState, _ *gpui.Window, _ *gpui.Context[InputBaseState[EditorMode]]) {
			s.editorExtras().hoverDefinition.update(NewInputSelection(0, 4), []LspLocationLink{{TargetURI: "file:///tmp/column-selection.rs"}})
		})
		left := gpui.MouseButtonLeft
		v.cx.SimulateMouseDown(start, left, modifiers)
		v.cx.SimulateMouseMove(end, &left, modifiers)
		v.cx.SimulateMouseUp(end, left, modifiers)
		v.read(func(s *EditorState) {
			if got := selectionRanges(s); !slices.Equal(got, []InputSelection{sel(1, 3), sel(6, 8), sel(11, 13)}) {
				t.Fatalf("%+v: ranges = %v", modifiers, got)
			}
		})
		v.cx.SimulateMouseMove(start, nil, modifiers)
		v.cx.SimulateKeystrokes("x")
		assertCursors(t, v, "ax|d\nax|d\nax|d")
	}
}

// --- Go-own provider tests --------------------------------------------------

type testHoverProvider struct{ calls []int }

func (p *testHoverProvider) Hover(_ rope.Rope, offset int, _ *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[*LspHover]] {
	p.calls = append(p.calls, offset)
	return readyTask(app, &LspHover{Contents: LspHoverContentsScalar(LspMarkedStringString("docs"))})
}

// Go-own: the first hover waits 150ms, then shows the provider's hover
// anchored to the word; blur hides it (U:input/editor/lsp/hover.rs:25-86).
func TestEditorHoverProviderShowsThePopoverAfterTheDelay(t *testing.T) {
	v := buildEditor(t)
	provider := &testHoverProvider{}
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue("select value", w, cx)
		s.LspMut().HoverProvider = provider
	})
	v.cx.RunUntilParked()
	var at gpui.Point
	v.read(func(s *EditorState) { at = columnPosition(s, 0, 9, 0.5) })
	v.cx.SimulateMouseMove(at, nil, gpui.Modifiers{})
	v.cx.RunUntilParked()
	v.read(func(s *EditorState) {
		if s.HoverPopover() != nil || len(provider.calls) != 0 {
			t.Fatal("the first hover must wait for its delay")
		}
	})
	v.cx.AdvanceClock(150)
	v.cx.RunUntilParked()
	v.read(func(s *EditorState) {
		popover := s.HoverPopover()
		if popover == nil || popover.SymbolRange != NewInputSelection(7, 12) {
			t.Fatalf("popover = %+v, want anchored to 7..12", popover)
		}
		if r, ok := s.extras.hoverSymbolRange(); !ok || r != NewInputSelection(7, 12) {
			t.Fatalf("hover symbol range = %v, %v", r, ok)
		}
	})
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.onBlur(w, cx)
		if s.HoverPopover() != nil {
			t.Fatal("blur hides the hover")
		}
	})
}

type testCompletionProvider struct {
	trigger  bool
	items    []LspCompletionItem
	inline   string
	contexts []LspCompletionContext
}

func (p *testCompletionProvider) Completions(_ rope.Rope, _ int, trigger LspCompletionContext, _ *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[LspCompletionResponse]] {
	p.contexts = append(p.contexts, trigger)
	return readyTask(app, LspCompletionResponseList(LspCompletionList{Items: p.items}))
}

func (p *testCompletionProvider) IsCompletionTrigger(int, string, *gpui.App) bool { return p.trigger }

func (p *testCompletionProvider) InlineCompletion(_ rope.Rope, _ int, _ LspInlineCompletionContext, _ *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[LspInlineCompletionResponse]] {
	return readyTask(app, LspInlineCompletionResponse{Array: []LspInlineCompletionItem{{InsertText: p.inline}}})
}

// Go-own: typing schedules the inline completion after the debounce; it
// is the ghost text until accepted (U:input/editor/lsp/completions.rs:
// 263-363, U:input/editor/mod.rs:267-269).
func TestEditorInlineCompletionIsTheGhostTextUntilAccepted(t *testing.T) {
	v := buildEditor(t)
	v.update(func(s *EditorState, _ *gpui.Window, _ *gpui.Context[InputBaseState[EditorMode]]) {
		s.LspMut().CompletionProvider = &testCompletionProvider{inline: "ect"}
	})
	v.update(editorType("sel"))
	v.cx.AdvanceClock(299)
	v.cx.RunUntilParked()
	v.read(func(s *EditorState) {
		if s.hasInlineCompletion() {
			t.Fatal("the inline completion waits for the 300ms debounce")
		}
	})
	v.cx.AdvanceClock(1)
	v.cx.RunUntilParked()
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		if text, ok := s.extras.inlineCompletionText(); !ok || text != "ect" {
			t.Fatalf("ghost text = %q, %v", text, ok)
		}
		if !s.hooks.acceptInlineCompletion(s, w, cx) {
			t.Fatal("accept must insert the completion")
		}
		wantValue(t, s, "select")
		if s.hasInlineCompletion() {
			t.Fatal("accepting consumes the completion")
		}
	})
}

// Go-own: a trigger opens the completion menu with the provider's items;
// a handled Escape closes it (U:input/editor/lsp/completions.rs:122-261,
// overlay.rs).
func TestEditorCompletionTriggerOpensTheMenu(t *testing.T) {
	v := buildEditor(t)
	provider := &testCompletionProvider{trigger: true, items: []LspCompletionItem{{Label: "select"}}}
	var routed []InputOverlayKind
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.Focus(w, cx)
		s.LspMut().CompletionProvider = provider
		s.SetOverlayActionHandler(func(kind InputOverlayKind, _ gpui.BoxedAction, _ *gpui.Window, _ *gpui.Context[InputBaseState[EditorMode]]) bool {
			routed = append(routed, kind)
			return true
		})
	})
	v.update(editorType("s"))
	v.cx.RunUntilParked()
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		menu := s.CompletionMenuState()
		if !menu.Open || len(menu.Items) != 1 || menu.Query != "s" || menu.Revision() == 0 {
			t.Fatalf("menu = %+v", menu)
		}
		if len(provider.contexts) != 1 || provider.contexts[0].TriggerKind != LspCompletionTriggerKindTriggerCharacter || *provider.contexts[0].TriggerCharacter != "s" {
			t.Fatalf("contexts = %+v", provider.contexts)
		}
		if !s.RouteOverlayAction(EscapeAction.Box(Escape{}), w, cx) {
			t.Fatal("the handler handles Escape")
		}
		if s.CompletionMenuState().Open || !slices.Equal(routed, []InputOverlayKind{InputOverlayKindCompletion}) {
			t.Fatalf("a handled Escape closes the menu; routed %v", routed)
		}
		// insert_completion: the label replaces the fallback range.
		s.InsertCompletion(&menu.Items[0], NewInputSelection(0, 1), w, cx)
		wantValue(t, s, "select")
	})
}

type testDefinitionProvider struct{ locations []LspLocationLink }

func (p *testDefinitionProvider) Definitions(_ rope.Rope, _ int, _ *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[[]LspLocationLink]] {
	return readyTask(app, p.locations)
}

// Go-own: GoToDefinition selects the target in this document; a host
// show-document hook that answers true takes over
// (U:input/editor/lsp/definitions.rs:104-203).
func TestEditorGoToDefinitionSelectsTheTarget(t *testing.T) {
	v := buildEditor(t)
	target := NewLspRange(NewInputPosition(1, 0), NewInputPosition(1, 3))
	provider := &testDefinitionProvider{locations: []LspLocationLink{{TargetURI: "file:///a.sql", TargetSelectionRange: target}}}
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue("foo\nbar", w, cx)
		s.Focus(w, cx)
		s.LspMut().DefinitionProvider = provider
		if !s.ContextMenuCapabilities().HasDefinition() {
			t.Fatal("a definition provider offers Go to Definition")
		}
		s.onActionGoToDefinition(&GoToDefinition{}, w, cx)
	})
	v.cx.RunUntilParked()
	v.read(func(s *EditorState) { wantSelected(t, s, 4, 7) })

	var shown []LspShowDocumentParams
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.moveTo(0, nil, cx)
		provider.locations[0].TargetURI = "https://example.com/doc"
		s.LspMut().ShowDocument = func(params *LspShowDocumentParams, _ *gpui.Window, _ *gpui.App) bool {
			shown = append(shown, *params)
			return true
		}
		s.onActionGoToDefinition(&GoToDefinition{}, w, cx)
	})
	v.cx.RunUntilParked()
	v.read(func(s *EditorState) { wantSelected(t, s, 0, 0) })
	if len(shown) != 1 || shown[0].URI != "https://example.com/doc" || !*shown[0].External || !*shown[0].TakeFocus || *shown[0].Selection != target {
		t.Fatalf("show_document params = %+v", shown)
	}
}

// Go-own: a secondary-modifier hover underlines the symbol with
// definitions, and a secondary click follows it
// (U:input/editor/lsp/definitions.rs:59-164).
func TestEditorSecondaryClickFollowsTheHoveredDefinition(t *testing.T) {
	v := buildEditor(t)
	target := NewLspRange(NewInputPosition(1, 0), NewInputPosition(1, 3))
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue("foo\nbar", w, cx)
		s.Focus(w, cx)
		s.LspMut().DefinitionProvider = &testDefinitionProvider{locations: []LspLocationLink{{TargetURI: "file:///a.sql", TargetSelectionRange: target}}}
	})
	v.cx.RunUntilParked()
	var at gpui.Point
	v.read(func(s *EditorState) { at = columnPosition(s, 0, 1, 0.5) })
	secondary := gpui.Modifiers{Control: true}
	v.cx.SimulateMouseMove(at, nil, secondary)
	v.cx.RunUntilParked()
	v.read(func(s *EditorState) {
		h := s.editorExtras().hoverDefinition
		if h.isEmpty() || h.symbolRange != NewInputSelection(0, 3) {
			t.Fatalf("hover definition = %+v", h)
		}
	})
	v.cx.SimulateMouseDown(at, gpui.MouseButtonLeft, secondary)
	v.cx.SimulateMouseUp(at, gpui.MouseButtonLeft, secondary)
	v.read(func(s *EditorState) { wantSelected(t, s, 4, 7) })

	// A plain move drops the underline.
	v.cx.SimulateMouseMove(at, nil, gpui.Modifiers{})
	v.read(func(s *EditorState) {
		if !s.editorExtras().hoverDefinition.isEmpty() {
			t.Fatal("a plain move clears the definition underline")
		}
	})
}

type testCodeActionProvider struct {
	id        string
	fail      bool
	performed []LspCodeAction
}

func (p *testCodeActionProvider) ID() string { return p.id }

func (p *testCodeActionProvider) CodeActions(_ gpui.Entity[EditorState], _ InputSelection, _ *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[[]LspCodeAction]] {
	if p.fail {
		return app.Spawn(func(*gpui.AsyncApp) ProviderResult[[]LspCodeAction] {
			return ProviderResult[[]LspCodeAction]{Err: errors.New("no server")}
		})
	}
	return readyTask(app, []LspCodeAction{{Title: p.id + " fix"}})
}

func (p *testCodeActionProvider) PerformCodeAction(_ gpui.Entity[EditorState], action LspCodeAction, _ bool, _ *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[struct{}]] {
	p.performed = append(p.performed, action)
	return readyTask(app, struct{}{})
}

// Go-own: ToggleCodeActions gathers every provider's actions (a failing
// provider adds none), and performing one goes to its provider
// (U:input/editor/lsp/code_actions.rs:43-136).
func TestEditorCodeActionsGatherAndPerform(t *testing.T) {
	v := buildEditor(t)
	ok, failing := &testCodeActionProvider{id: "ok"}, &testCodeActionProvider{id: "bad", fail: true}
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.Focus(w, cx)
		s.LspMut().CodeActionProviders = []CodeActionProvider{failing, ok}
		s.onActionToggleCodeActions(&ToggleCodeActions{}, w, cx)
	})
	v.cx.RunUntilParked()
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		menu := s.CodeActionMenuState()
		if !menu.Open || len(menu.Items) != 1 || menu.Items[0].ProviderID != "ok" {
			t.Fatalf("code-action menu = %+v", menu)
		}
		if !s.isContextMenuOpen() || !s.ContextMenuCapabilities().HasCodeActions() {
			t.Fatal("the menu is open and code actions are offered")
		}
		s.PerformCodeAction(&menu.Items[0], w, cx)
		s.DismissLspOverlays(cx)
		if s.isContextMenuOpen() {
			t.Fatal("dismissing closes the menu")
		}
	})
	v.cx.RunUntilParked()
	if len(ok.performed) != 1 || ok.performed[0].Title != "ok fix" || len(failing.performed) != 0 {
		t.Fatalf("performed: ok %v, bad %v", ok.performed, failing.performed)
	}
}

type testDocumentColorProvider struct{}

func (testDocumentColorProvider) DocumentColors(_ rope.Rope, _ *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[[]LspColorInformation]] {
	return readyTask(app, []LspColorInformation{{Range: NewLspRange(NewInputPosition(0, 0), NewInputPosition(0, 3)), Color: LspColor{Red: 1, Alpha: 1}}})
}

type testSemanticTokensProvider struct{}

func (testSemanticTokensProvider) Legend() LspSemanticTokensLegend { return semanticLegend() }

func (testSemanticTokensProvider) SemanticTokens(_ rope.Rope, _ InputSelection, _ *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[LspSemanticTokens]] {
	return readyTask(app, LspSemanticTokens{Data: []LspSemanticToken{{Length: 3, TokenType: 0}}})
}

// Go-own: a refresh fetches document colors and semantic tokens after
// their 100ms debounce; a value reset drops them
// (U:input/editor/lsp/mod.rs:94-113, document_colors.rs, semantic_tokens.rs).
func TestEditorRefreshFetchesDocumentColorsAndSemanticTokens(t *testing.T) {
	v := buildEditor(t)
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue("red x", w, cx)
		lsp := s.LspMut()
		lsp.DocumentColorProvider = testDocumentColorProvider{}
		lsp.SemanticTokensProvider = testSemanticTokensProvider{}
		s.Refresh(cx)
	})
	v.cx.RunUntilParked()
	v.cx.AdvanceClock(100)
	v.cx.RunUntilParked()
	v.read(func(s *EditorState) {
		swatches := s.extras.documentColorSwatches(s.text, NewInputSelection(0, 1))
		if len(swatches) != 1 || swatches[0].rng != NewInputSelection(0, 3) || swatches[0].color != gpui.Red() {
			t.Fatalf("swatches = %+v", swatches)
		}
		styles := s.extras.semanticTokenStyles(s.text, NewInputSelection(0, 5), semanticTestTheme{})
		if len(styles) != 1 || styles[0].Range != (gpui.TextRange{Start: 0, End: 3}) {
			t.Fatalf("semantic styles = %+v", styles)
		}
	})
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue("other", w, cx)
		if len(s.Lsp().documentColors) != 0 || len(s.Lsp().semanticTokens) != 0 {
			t.Fatal("set_value resets the LSP state")
		}
	})
}

// Go-own: the pointer over a diagnostic presents it, an edit resets the
// set, and the diagnostic style is a wavy underline of its severity color
// (U:input/base/state.rs:2412-2437, element.rs:29-47).
func TestEditorDiagnosticPopoverFollowsThePointer(t *testing.T) {
	v := buildEditor(t)
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.SetValue("select 1", w, cx)
		s.DiagnosticsMut().Push(NewDiagnostic(NewInputPosition(0, 0), NewInputPosition(0, 6), "unknown").WithSeverity(DiagnosticSeverityError))
		cx.Notify()
	})
	v.cx.RunUntilParked()
	var on, off gpui.Point
	v.read(func(s *EditorState) {
		on = columnPosition(s, 0, 2, 0.5)
		off = columnPosition(s, 0, 7, 0.5)
	})
	v.cx.SimulateMouseMove(on, nil, gpui.Modifiers{})
	v.read(func(s *EditorState) {
		if p := s.DiagnosticPopover(); p == nil || p.Message != "unknown" || p.ByteRange != NewInputSelection(0, 6) {
			t.Fatalf("popover = %+v", p)
		}
	})
	v.cx.SimulateMouseMove(off, nil, gpui.Modifiers{})
	v.read(func(s *EditorState) {
		if s.DiagnosticPopover() != nil {
			t.Fatal("leaving the diagnostic hides the popover")
		}
	})
	v.update(func(s *EditorState, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
		s.ReplaceTextInRange(nil, "x", w, cx)
		if s.Diagnostics().Len() != 0 {
			t.Fatal("an edit resets the diagnostics")
		}
	})

	colors := DiagnosticColors{Error: gpui.Red()}
	style := diagnosticHighlightStyle(DiagnosticSeverityError, colors)
	if style.Underline == nil || !style.Underline.Wavy || style.Underline.Thickness != 1 || *style.Underline.Color != gpui.Red() {
		t.Fatalf("diagnostic style = %+v", style.Underline)
	}
	// Only the code editor has diagnostics.
	buildTextarea(t, plainTextarea).read(func(s *TextareaState) {
		if s.Diagnostics() != nil {
			t.Fatal("a textarea has no diagnostic set")
		}
	})
}
