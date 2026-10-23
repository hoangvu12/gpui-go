package base

// Port of U:input/editor/lsp/mod.rs: the Lsp provider set and its cached
// results, apply_lsp_edits, and the task plumbing the provider files
// share. The providers are editor_lsp_{completions,code_actions,hover,
// definitions,document_colors,semantic_tokens}.go; the overlay state is
// editor_lsp_overlay.go.
//
// Go adaptations:
//
//   - A provider method returning Task<anyhow::Result<T>> returns
//     gpui.Task[ProviderResult[T]]; a nil Err is Ok. Providers build the
//     task with App.Spawn or the background executor.
//   - A provider trait method with a default body is an optional
//     interface plus a package function supplying the default
//     (doc.go naming rules), e.g. InlineCompletionProvider.
//   - Rc<dyn Provider> is the interface value; Option is nil.
//   - Replacing a pinned `_task` field drops (cancels) the previous task;
//     the port cancels it explicitly (lspTask), as input_blink_cursor.go
//     does. BackgroundExecutor::timer is a background task sleeping on
//     the scheduler clock; a cancelled sleep ends the task body, as a
//     dropped Rust task stops at its await, so nothing spawns after
//     App.Shutdown cancels the task.

import (
	"time"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// ProviderResult is the anyhow::Result<T> a provider task resolves to: Err
// nil is Ok(Value).
type ProviderResult[T any] struct {
	Value T
	Err   error
}

// ShowDocumentHandler is the host hook that shows a document when Go to
// Definition follows a location, modeled after window/showDocument
// (U:input/editor/lsp/mod.rs:24-34). It runs before the built-in
// behavior; returning true means the host showed the document, false
// falls through (external URIs open in the browser, anything else jumps
// within the current document).
type ShowDocumentHandler func(params *LspShowDocumentParams, w *gpui.Window, app *gpui.App) bool

// Lsp is the editor's LSP providers and their cached results, the LSP
// ServerCapabilities (U:input/editor/lsp/mod.rs:36-70). Configure the
// providers through EditorState.LspMut; call Refresh afterwards for them
// to take effect before the text next changes.
type Lsp struct {
	// CompletionProvider is the completion provider.
	CompletionProvider CompletionProvider
	// CodeActionProviders are the code action providers.
	CodeActionProviders []CodeActionProvider
	// HoverProvider is the hover provider.
	HoverProvider HoverProvider
	// DefinitionProvider is the definition provider.
	DefinitionProvider DefinitionProvider
	// DocumentColorProvider is the document color provider.
	DocumentColorProvider DocumentColorProvider
	// SemanticTokensProvider is the range semantic tokens provider.
	SemanticTokensProvider DocumentRangeSemanticTokensProvider
	// ShowDocument is the optional host hook for Go to Definition
	// locations, following window/showDocument.
	ShowDocument ShowDocumentHandler

	// CompletionMenu is the display options of the completion popover.
	CompletionMenu CompletionMenuOptions

	documentColors []lspDocumentColor
	// semanticTokens are the cached semantic tokens as absolute position
	// ranges and theme token-type names; the color is resolved from the
	// name at paint time so theme switches take effect without a refetch.
	semanticTokens []lspSemanticTokenRange

	hoverTask      lspTask
	definitionTask lspTask
	// definitionBlur is the blur subscription that lives as long as
	// definitionTask (U:input/editor/lsp/definitions.rs:117-121).
	definitionBlur     gpui.Subscription
	documentColorTask  lspTask
	semanticTokensTask lspTask
}

// setDefinitionTask replaces the go-to-definition task and the blur
// subscription that lives with it.
func (l *Lsp) setDefinitionTask(task gpui.Task[struct{}], blur gpui.Subscription) {
	l.dropDefinitionTask()
	l.definitionTask.set(task)
	l.definitionBlur = blur
}

// dropDefinitionTask is `_definition_task = Task::ready(Ok(()))`, which
// also drops the subscription the task owned.
func (l *Lsp) dropDefinitionTask() {
	l.definitionTask.drop()
	l.definitionBlur.Cancel()
	l.definitionBlur = gpui.Subscription{}
}

// DefaultLsp is `impl Default for Lsp` (U:input/editor/lsp/mod.rs:72-91).
func DefaultLsp() Lsp { return Lsp{CompletionMenu: DefaultCompletionMenuOptions()} }

// update refreshes the LSP state when the text changes
// (U:input/editor/lsp/mod.rs:94-103).
func (l *Lsp) update(text rope.Rope, w *gpui.Window, cx *gpui.Context[InputBaseState[EditorMode]]) {
	l.updateDocumentColors(text, w, cx)
	l.updateSemanticTokens(text, w, cx)
}

// reset drops every cached result and pending request
// (U:input/editor/lsp/mod.rs:105-113).
func (l *Lsp) reset() {
	l.documentColors = nil
	l.semanticTokens = nil
	l.hoverTask.drop()
	l.dropDefinitionTask()
	l.documentColorTask.drop()
	l.semanticTokensTask.drop()
}

// ApplyLspEdits applies text edits to the text, each without UI reactions
// such as completion (U:input/editor/lsp/mod.rs:116-131). EditorState
// only.
func (s *InputBaseState[M]) ApplyLspEdits(textEdits []LspTextEdit, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.requireEditorMode("ApplyLspEdits")
	for _, edit := range textEdits {
		start := s.ext().PositionToOffset(edit.Range.Start)
		end := s.ext().PositionToOffset(edit.Range.End)
		rng := s.rangeToUTF16(NewInputSelection(start, end))
		s.replaceTextInRangeSilent(&rng, edit.NewText, w, cx)
	}
}

// Lsp is the LSP providers and their cached results
// (U:input/editor/mod.rs:195-201). EditorState only: an input or textarea
// has no language server.
func (s *InputBaseState[M]) Lsp() *Lsp { return &s.lspExtras("Lsp").lsp }

// LspMut is the LSP providers, to configure them
// (U:input/editor/mod.rs:203-206). EditorState only. Go has no shared
// reference, so it is the same pointer as Lsp.
func (s *InputBaseState[M]) LspMut() *Lsp { return &s.lspExtras("LspMut").lsp }

// lspExtras is `self.extras` of an EditorState; it panics on another mode.
func (s *InputBaseState[M]) lspExtras(method string) *EditorExtras {
	s.requireEditorMode(method)
	return s.editorExtras()
}

// lspTask is a `_task: Task<..>` field: replacing it cancels the
// previous task, and Task::ready is the empty slot.
type lspTask struct {
	task    gpui.Task[struct{}]
	hasTask bool
}

// set replaces the task, cancelling the previous one.
func (t *lspTask) set(task gpui.Task[struct{}]) {
	t.drop()
	t.task, t.hasTask = task, true
}

// drop is `= Task::ready(..)`: cancel any pending task.
func (t *lspTask) drop() {
	if t.hasTask {
		t.task.Cancel()
		t.task = gpui.Task[struct{}]{}
		t.hasTask = false
	}
}

// lspSleep is `cx.background_executor().timer(d).await` inside a
// foreground task: false when the sleep was cancelled (the task dropped
// or the application shutting down), and the body must then stop.
func lspSleep(acx *gpui.AsyncApp, d time.Duration) bool {
	return acx.Await(acx.App().Background().Spawn(func(run *gpui.TaskRun) bool {
		run.Sleep(d)
		return !run.Cancelled()
	}))
}

// awaitProvider awaits a provider task: ok is false when it failed
// (Err) or was cancelled, the `?` / `.ok()` of the upstream bodies.
func awaitProvider[T any](acx *gpui.AsyncApp, task gpui.Task[ProviderResult[T]]) (T, bool) {
	result := acx.Await(task)
	if task.Cancelled() || !task.Done() || result.Err != nil {
		var zero T
		return zero, false
	}
	return result.Value, true
}
