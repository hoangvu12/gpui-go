package base

// Port of U:input/editor/lsp/code_actions.rs: code action providers, the
// code-action menu request and performing an action.

import "github.com/hoangvu12/gpui-go/gpui"

// CodeActionProvider fetches and performs code actions
// (U:input/editor/lsp/code_actions.rs:8-34).
type CodeActionProvider interface {
	// ID identifies this provider.
	ID() string
	// CodeActions fetches code actions for the byte range:
	// textDocument/codeAction.
	CodeActions(state gpui.Entity[EditorState], rng InputSelection, w *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[[]LspCodeAction]]
	// PerformCodeAction performs the action.
	PerformCodeAction(state gpui.Entity[EditorState], action LspCodeAction, pushToHistory bool, w *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[struct{}]]
}

// CodeActionItem is one action and the provider it came from
// (U:input/editor/lsp/code_actions.rs:36-40).
type CodeActionItem struct {
	ProviderID string
	Action     LspCodeAction
}

// onActionToggleCodeActions is U:input/editor/lsp/code_actions.rs:43-50.
func (s *InputBaseState[M]) onActionToggleCodeActions(_ *ToggleCodeActions, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.handleCodeActionTrigger(w, cx)
}

// handleCodeActionTrigger asks every provider for the selection's code
// actions and opens the menu with them
// (U:input/editor/lsp/code_actions.rs:52-113).
func (s *InputBaseState[M]) handleCodeActionTrigger(w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	e := s.editorExtras()
	providers := append([]CodeActionProvider(nil), e.lsp.CodeActionProviders...)
	rng := s.SelectedRange()

	state, ok := any(cx.Entity()).(gpui.Entity[EditorState])
	if !ok {
		return
	}
	e.contextMenuTask.set(cx.SpawnIn(w, func(editor gpui.WeakEntity[InputBaseState[M]], acx *gpui.AsyncWindowContext) struct{} {
		type response struct {
			providerID string
			task       gpui.Task[ProviderResult[[]LspCodeAction]]
		}
		var responses []response
		_ = acx.Update(func(w *gpui.Window, app *gpui.App) {
			for _, provider := range providers {
				responses = append(responses, response{provider.ID(), provider.CodeActions(state, rng, w, app)})
			}
		})

		var codeActions []CodeActionItem
		for _, r := range responses {
			if actions, ok := awaitProvider(acx.AsyncApp, r.task); ok {
				for _, action := range actions {
					codeActions = append(codeActions, CodeActionItem{ProviderID: r.providerID, Action: action})
				}
			}
		}

		if len(codeActions) == 0 {
			acx.AsyncApp.Update(func(app *gpui.App) {
				_ = editor.TryUpdate(app, func(s *InputBaseState[M], cx *gpui.Context[InputBaseState[M]]) {
					menu := &s.editorExtras().contextMenuContent.codeAction
					menu.Open = false
					menu.Items = nil
					menu.bump()
					cx.Notify()
				})
			})
			return struct{}{}
		}
		_ = acx.Update(func(w *gpui.Window, app *gpui.App) {
			_ = editor.TryUpdateIn(app, w, func(s *InputBaseState[M], w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
				if !s.focusHandle.IsFocused(w) {
					return
				}
				menu := &s.editorExtras().contextMenuContent.codeAction
				menu.Items = codeActions
				menu.Open = len(menu.Items) > 0
				// The overlay rebuilds on a new revision, so a refresh while
				// the menu is already open must bump it too.
				menu.bump()
				cx.Notify()
			})
		})
		return struct{}{}
	}))
}

// PerformCodeAction performs item with the provider it came from
// (U:input/editor/lsp/code_actions.rs:115-136). EditorState only.
func (s *InputBaseState[M]) PerformCodeAction(item *CodeActionItem, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	e := s.lspExtras("PerformCodeAction")
	var provider CodeActionProvider
	for _, p := range e.lsp.CodeActionProviders {
		if p.ID() == item.ProviderID {
			provider = p
			break
		}
	}
	if provider == nil {
		return
	}
	state, ok := any(cx.Entity()).(gpui.Entity[EditorState])
	if !ok {
		return
	}
	task := provider.PerformCodeAction(state, item.Action, true, w, cx.App())
	cx.SpawnIn(w, func(_ gpui.WeakEntity[InputBaseState[M]], acx *gpui.AsyncWindowContext) struct{} {
		_ = acx.Await(task)
		return struct{}{}
	}).Detach()
}
