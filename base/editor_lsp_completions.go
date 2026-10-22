package base

// Port of U:input/editor/lsp/completions.rs: the completion provider, the
// completion menu trigger, the context-menu routing and the debounced
// inline completion (ghost text).
//
// Not ported: CompletionProvider::resolve_completions
// (U:input/editor/lsp/completions.rs:90-97). Its items are
// lsp_types::request::Completion, an uninhabited request-marker enum, so
// no value can be passed to it, and Base never calls it.

import (
	"strings"
	"time"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// defaultInlineCompletionDebounce is DEFAULT_INLINE_COMPLETION_DEBOUNCE
// (U:input/editor/lsp/completions.rs:14-15).
const defaultInlineCompletionDebounce = 300 * time.Millisecond

// CompletionMenuOptions is the display options of the LSP completion
// popover, reached through Lsp.CompletionMenu
// (U:input/editor/lsp/completions.rs:17-29).
type CompletionMenuOptions struct {
	// MaxWidth is the popover's maximum width, 320 px by default; widen it
	// for long completion labels.
	MaxWidth float32
}

// DefaultCompletionMenuOptions is U:input/editor/lsp/completions.rs:31-37.
func DefaultCompletionMenuOptions() CompletionMenuOptions {
	return CompletionMenuOptions{MaxWidth: 320}
}

// CompletionProvider provides code completions for the text at a byte
// offset (U:input/editor/lsp/completions.rs:39-103). Its optional methods
// are InlineCompletionProvider and InlineCompletionDebouncer.
type CompletionProvider interface {
	// Completions fetches completions at offset, the byte offset of the
	// cursor: textDocument/completion.
	Completions(text rope.Rope, offset int, trigger LspCompletionContext, w *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[LspCompletionResponse]]
	// IsCompletionTrigger reports whether typing newText at offset should
	// request completions. It runs on the main thread.
	IsCompletionTrigger(offset int, newText string, app *gpui.App) bool
}

// InlineCompletionProvider is CompletionProvider::inline_completion, the
// optional override (U:input/editor/lsp/completions.rs:57-80): fetch an
// inline completion suggestion at offset after the debounce,
// textDocument/inlineCompletion. Without it there is no suggestion.
type InlineCompletionProvider interface {
	InlineCompletion(text rope.Rope, offset int, trigger LspInlineCompletionContext, w *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[LspInlineCompletionResponse]]
}

// InlineCompletionDebouncer is CompletionProvider::
// inline_completion_debounce, the optional override
// (U:input/editor/lsp/completions.rs:82-88).
type InlineCompletionDebouncer interface {
	InlineCompletionDebounce() time.Duration
}

// CompletionProviderInlineCompletionDebounce is the provider's debounce:
// its override, else 300ms (U:input/editor/lsp/completions.rs:82-88).
func CompletionProviderInlineCompletionDebounce(p CompletionProvider) time.Duration {
	if d, ok := p.(InlineCompletionDebouncer); ok {
		return d.InlineCompletionDebounce()
	}
	return defaultInlineCompletionDebounce
}

// inlineCompletion is InlineCompletion (U:input/editor/lsp/
// completions.rs:105-119): the suggestion on display and its debounced
// request.
type inlineCompletion struct {
	item *LspInlineCompletionItem
	task lspTask
}

// handleCompletionTrigger schedules the inline completion and, when the
// typed text triggers it, requests the completion menu
// (U:input/editor/lsp/completions.rs:122-224).
func (s *InputBaseState[M]) handleCompletionTrigger(rng InputSelection, newText string, w *gpui.Window, cx *inputCx[M]) {
	if s.completionInserting {
		return
	}
	e := s.editorExtras()
	provider := e.lsp.CompletionProvider
	if provider == nil {
		return
	}

	// Always schedule the inline completion (debounced); it checks the
	// menu before showing.
	s.scheduleInlineCompletion(w, cx)

	start := rng.End
	newOffset := s.Cursor()
	if !provider.IsCompletionTrigger(start, newText, cx.App()) {
		return
	}

	startOffset := start
	if e.contextMenuContent.completion.TriggerStartOffset != nil {
		startOffset = *e.contextMenuContent.completion.TriggerStartOffset
	}
	if newOffset < startOffset {
		return
	}

	query := ""
	if text, _, ok := s.TextForRange(s.rangeToUTF16(NewInputSelection(startOffset, newOffset)), w, cx); ok {
		query = strings.TrimSpace(text)
	}
	e.contextMenuContent.completion.TriggerStartOffset = &startOffset
	e.contextMenuContent.completion.Query = query

	trigger := query
	completionContext := LspCompletionContext{
		TriggerKind:      LspCompletionTriggerKindTriggerCharacter,
		TriggerCharacter: &trigger,
	}
	responses := provider.Completions(s.text, newOffset, completionContext, w, cx.App())
	e.contextMenuTask.set(cx.SpawnIn(w, func(editor gpui.WeakEntity[InputBaseState[M]], acx *gpui.AsyncWindowContext) struct{} {
		var completions []LspCompletionItem
		if response, ok := awaitProvider(acx.AsyncApp, responses); ok {
			completions = append(completions, response.items()...)
		}

		if len(completions) == 0 {
			acx.AsyncApp.Update(func(app *gpui.App) {
				_ = editor.TryUpdate(app, func(s *InputBaseState[M], cx *inputCx[M]) {
					menu := &s.editorExtras().contextMenuContent.completion
					menu.Open = false
					menu.Items = nil
					menu.bump()
					cx.Notify()
				})
			})
			return struct{}{}
		}

		_ = acx.Update(func(w *gpui.Window, app *gpui.App) {
			_ = editor.TryUpdateIn(app, w, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) {
				if !s.focusHandle.IsFocused(w) {
					return
				}
				menu := &s.editorExtras().contextMenuContent.completion
				menu.Items = completions
				menu.Open = len(menu.Items) > 0
				menu.bump()
				cx.Notify()
			})
		})
		return struct{}{}
	}))
}

// hideContextMenu closes the completion and code-action menus and drops
// their request (U:input/editor/lsp/completions.rs:226-231).
func (s *InputBaseState[M]) hideContextMenu(cx *inputCx[M]) {
	e := s.editorExtras()
	e.contextMenuContent.completion.Open = false
	e.contextMenuContent.codeAction.Open = false
	e.contextMenuTask.drop()
	cx.Notify()
}

// isContextMenuOpen is U:input/editor/lsp/completions.rs:233-236.
func (s *InputBaseState[M]) isContextMenuOpen() bool {
	e := s.editorExtras()
	return e.contextMenuContent.completion.Open || e.contextMenuContent.codeAction.Open
}

// handleActionForContextMenu offers the action to the open menu's
// overlay handler; a handled primary Enter or Escape closes the menu
// (U:input/editor/lsp/completions.rs:238-261).
func (s *InputBaseState[M]) handleActionForContextMenu(action gpui.BoxedAction, w *gpui.Window, cx *inputCx[M]) bool {
	closesOverlay := EnterIsPrimary(action) || action.Equal(EscapeAction.Box(Escape{}))
	e := s.editorExtras()
	var kind InputOverlayKind
	switch {
	case e.contextMenuContent.completion.Open:
		kind = InputOverlayKindCompletion
	case e.contextMenuContent.codeAction.Open:
		kind = InputOverlayKindCodeAction
	default:
		return false
	}
	handler := s.overlayActionHandler
	if handler == nil {
		return false
	}
	handled := handler(kind, action, w, cx)
	if handled && closesOverlay {
		s.hideContextMenu(cx)
	}
	return handled
}

// scheduleInlineCompletion requests an inline completion after the
// debounce (U:input/editor/lsp/completions.rs:263-333).
func (s *InputBaseState[M]) scheduleInlineCompletion(w *gpui.Window, cx *inputCx[M]) {
	// Clear any existing inline completion on text change.
	s.clearInlineCompletion(cx)

	e := s.editorExtras()
	provider := e.lsp.CompletionProvider
	if provider == nil {
		return
	}

	offset := s.Cursor()
	text := s.text
	debounce := CompletionProviderInlineCompletionDebounce(provider)

	e.inlineCompletion.task.set(cx.SpawnIn(w, func(editor gpui.WeakEntity[InputBaseState[M]], acx *gpui.AsyncWindowContext) struct{} {
		// Debounce: wait before fetching, to avoid requests while typing.
		if !lspSleep(acx.AsyncApp, debounce) {
			return struct{}{}
		}

		var task gpui.Task[ProviderResult[LspInlineCompletionResponse]]
		requested := false
		err := acx.Update(func(w *gpui.Window, app *gpui.App) {
			_ = editor.TryUpdateIn(app, w, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) {
				// The cursor moved during the debounce, or the menu is open.
				if s.Cursor() != offset || s.isContextMenuOpen() {
					return
				}
				// The trait default answers an empty array: nothing to show.
				inline, ok := provider.(InlineCompletionProvider)
				if !ok {
					return
				}
				trigger := LspInlineCompletionContext{TriggerKind: LspInlineCompletionTriggerKindAutomatic}
				task = inline.InlineCompletion(text, offset, trigger, w, cx.App())
				requested = true
			})
		})
		if err != nil || !requested {
			return struct{}{}
		}

		response, ok := awaitProvider(acx.AsyncApp, task)
		if !ok {
			return struct{}{}
		}

		_ = acx.Update(func(w *gpui.Window, app *gpui.App) {
			_ = editor.TryUpdateIn(app, w, func(s *InputBaseState[M], _ *gpui.Window, cx *inputCx[M]) {
				// Only apply if the cursor still has not moved and the menu
				// did not open while fetching.
				if s.Cursor() != offset || s.isContextMenuOpen() {
					return
				}
				if items := response.items(); len(items) > 0 {
					item := items[0]
					s.editorExtras().inlineCompletion.item = &item
					cx.Notify()
				}
			})
		})
		return struct{}{}
	}))
}

// hasInlineCompletion reports a suggestion on display
// (U:input/editor/lsp/completions.rs:335-339).
func (s *InputBaseState[M]) hasInlineCompletion() bool {
	return s.editorExtras().inlineCompletion.item != nil
}

// clearInlineCompletion drops the suggestion and its request
// (U:input/editor/lsp/completions.rs:341-345).
func (s *InputBaseState[M]) clearInlineCompletion(cx *inputCx[M]) {
	e := s.editorExtras()
	e.inlineCompletion.item = nil
	e.inlineCompletion.task.drop()
	cx.Notify()
}

// acceptInlineCompletion inserts the suggestion at the cursor; false when
// there is none (U:input/editor/lsp/completions.rs:347-363).
func (s *InputBaseState[M]) acceptInlineCompletion(w *gpui.Window, cx *inputCx[M]) bool {
	e := s.editorExtras()
	item := e.inlineCompletion.item
	if item == nil {
		return false
	}
	e.inlineCompletion.item = nil
	cursor := s.Cursor()
	rng := s.rangeToUTF16(NewInputSelection(cursor, cursor))
	s.replaceTextInRangeSilent(&rng, item.InsertText, w, cx)
	return true
}
