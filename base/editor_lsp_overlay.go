package base

// Port of U:input/editor/lsp/overlay.rs: the completion, code-action and
// hover overlay state an application renders, and the API that presents,
// routes and dismisses them. Every exported method is EditorState only
// and panics on another mode (ticket 20/21 convention, input_kind.go).

import "github.com/hoangvu12/gpui-go/gpui"

// CompletionMenuState is the completion menu's content
// (U:input/editor/lsp/overlay.rs:7-28).
type CompletionMenuState struct {
	Open               bool
	TriggerStartOffset *int
	Query              string
	Items              []LspCompletionItem
	revision           uint64
}

// Revision is bumped whenever the content changes, so a renderer
// mirroring the menu compares revisions instead of the items
// (U:input/editor/lsp/overlay.rs:17-23).
func (m *CompletionMenuState) Revision() uint64 { return m.revision }

// bump is U:input/editor/lsp/overlay.rs:25-27 (wrapping_add is Go's
// unsigned overflow).
func (m *CompletionMenuState) bump() { m.revision++ }

// CodeActionMenuState is the code-action menu's content
// (U:input/editor/lsp/overlay.rs:30-46).
type CodeActionMenuState struct {
	Open     bool
	Items    []CodeActionItem
	revision uint64
}

// Revision is U:input/editor/lsp/overlay.rs:38-41; see
// CompletionMenuState.Revision.
func (m *CodeActionMenuState) Revision() uint64 { return m.revision }

// bump is U:input/editor/lsp/overlay.rs:43-45.
func (m *CodeActionMenuState) bump() { m.revision++ }

// HoverPopoverState is the hover on display and the symbol it is anchored
// to (U:input/editor/lsp/overlay.rs:48-52).
type HoverPopoverState struct {
	SymbolRange InputSelection
	Hover       LspHover
}

// contextMenuContent is ContextMenuContent
// (U:input/editor/lsp/overlay.rs:54-58).
type contextMenuContent struct {
	completion CompletionMenuState
	codeAction CodeActionMenuState
}

// InputOverlayKind is which menu an overlay action is routed to
// (U:input/editor/lsp/overlay.rs:60-64).
type InputOverlayKind uint8

const (
	InputOverlayKindCompletion InputOverlayKind = iota
	InputOverlayKindCodeAction
)

// InputOverlayActionHandler is the application's overlay action handler:
// it receives actions while a menu is open and reports whether it handled
// them (U:input/base/state.rs:448-457).
type InputOverlayActionHandler[M InputModeKind] func(kind InputOverlayKind, action gpui.BoxedAction, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) bool

// PresentCompletionItems opens the completion menu with items
// (U:input/editor/lsp/overlay.rs:67-84).
func (s *InputBaseState[M]) PresentCompletionItems(triggerStartOffset int, query string, items []LspCompletionItem, cx *gpui.Context[InputBaseState[M]]) {
	menu := &s.lspExtras("PresentCompletionItems").contextMenuContent.completion
	menu.TriggerStartOffset = &triggerStartOffset
	menu.Query = query
	menu.Items = items
	menu.Open = len(menu.Items) > 0
	menu.bump()
	cx.Notify()
}

// PresentCodeActions opens the code-action menu with items
// (U:input/editor/lsp/overlay.rs:86-96).
func (s *InputBaseState[M]) PresentCodeActions(items []CodeActionItem, cx *gpui.Context[InputBaseState[M]]) {
	menu := &s.lspExtras("PresentCodeActions").contextMenuContent.codeAction
	menu.Items = items
	menu.Open = len(menu.Items) > 0
	menu.bump()
	cx.Notify()
}

// PresentHover shows hover anchored to symbolRange
// (U:input/editor/lsp/overlay.rs:98-109).
func (s *InputBaseState[M]) PresentHover(symbolRange InputSelection, hover LspHover, cx *gpui.Context[InputBaseState[M]]) {
	s.lspExtras("PresentHover").hoverPopover = &HoverPopoverState{SymbolRange: symbolRange, Hover: hover}
	cx.Notify()
}

// PresentDiagnostic shows diagnostic as the diagnostic popover
// (U:input/editor/lsp/overlay.rs:111-118).
func (s *InputBaseState[M]) PresentDiagnostic(diagnostic DiagnosticEntry, cx *gpui.Context[InputBaseState[M]]) {
	s.requireEditorMode("PresentDiagnostic")
	s.diagnosticPopover = &diagnostic
	cx.Notify()
}

// ClearDiagnosticPopover hides the diagnostic popover
// (U:input/editor/lsp/overlay.rs:120-124).
func (s *InputBaseState[M]) ClearDiagnosticPopover(cx *gpui.Context[InputBaseState[M]]) {
	s.requireEditorMode("ClearDiagnosticPopover")
	if s.diagnosticPopover != nil {
		s.diagnosticPopover = nil
		cx.Notify()
	}
}

// RouteOverlayAction offers action to the open menu's overlay handler
// (U:input/editor/lsp/overlay.rs:126-133).
func (s *InputBaseState[M]) RouteOverlayAction(action gpui.BoxedAction, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) bool {
	s.requireEditorMode("RouteOverlayAction")
	return s.handleActionForContextMenu(action, w, cx)
}

// SetOverlayActionHandler installs the overlay action handler
// (U:input/editor/lsp/overlay.rs:135-146).
func (s *InputBaseState[M]) SetOverlayActionHandler(handler InputOverlayActionHandler[M]) {
	s.requireEditorMode("SetOverlayActionHandler")
	s.overlayActionHandler = handler
}

// HasOverlayActionHandler is U:input/editor/lsp/overlay.rs:148-150.
func (s *InputBaseState[M]) HasOverlayActionHandler() bool {
	s.requireEditorMode("HasOverlayActionHandler")
	return s.overlayActionHandler != nil
}

// DismissCompletionOverlay closes the completion menu
// (U:input/editor/lsp/overlay.rs:152-157).
func (s *InputBaseState[M]) DismissCompletionOverlay(cx *gpui.Context[InputBaseState[M]]) {
	menu := &s.lspExtras("DismissCompletionOverlay").contextMenuContent.completion
	if menu.Open {
		menu.Open = false
		cx.Notify()
	}
}

// DismissCodeActionOverlay closes the code-action menu
// (U:input/editor/lsp/overlay.rs:159-164).
func (s *InputBaseState[M]) DismissCodeActionOverlay(cx *gpui.Context[InputBaseState[M]]) {
	menu := &s.lspExtras("DismissCodeActionOverlay").contextMenuContent.codeAction
	if menu.Open {
		menu.Open = false
		cx.Notify()
	}
}

// InsertCompletion inserts item: its text edit, else its insert text at
// the end of fallbackRange, else its label over fallbackRange; then the
// editor takes focus (U:input/editor/lsp/overlay.rs:166-197).
func (s *InputBaseState[M]) InsertCompletion(item *LspCompletionItem, fallbackRange InputSelection, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.requireEditorMode("InsertCompletion")
	rng := fallbackRange
	newText := item.Label
	ext := s.ext()
	switch {
	case item.TextEdit != nil && item.TextEdit.Edit != nil:
		edit := item.TextEdit.Edit
		newText = edit.NewText
		rng = NewInputSelection(ext.PositionToOffset(edit.Range.Start), ext.PositionToOffset(edit.Range.End))
	case item.TextEdit != nil && item.TextEdit.InsertAndReplace != nil:
		edit := item.TextEdit.InsertAndReplace
		newText = edit.NewText
		rng = NewInputSelection(ext.PositionToOffset(edit.Replace.Start), ext.PositionToOffset(edit.Replace.End))
	case item.TextEdit == nil && item.InsertText != nil:
		newText = *item.InsertText
		rng = NewInputSelection(rng.End, rng.End)
	}
	s.completionInserting = true
	utf16 := s.rangeToUTF16(rng)
	s.replaceTextInRangeSilent(&utf16, newText, w, cx)
	s.completionInserting = false
	s.Focus(w, cx)
}

// CompletionMenuState is the completion menu
// (U:input/editor/lsp/overlay.rs:199-202, doc(hidden)).
func (s *InputBaseState[M]) CompletionMenuState() *CompletionMenuState {
	return &s.lspExtras("CompletionMenuState").contextMenuContent.completion
}

// CodeActionMenuState is the code-action menu
// (U:input/editor/lsp/overlay.rs:204-207, doc(hidden)).
func (s *InputBaseState[M]) CodeActionMenuState() *CodeActionMenuState {
	return &s.lspExtras("CodeActionMenuState").contextMenuContent.codeAction
}

// HoverPopover is the hover on display; nil when none
// (U:input/editor/lsp/overlay.rs:209-211).
func (s *InputBaseState[M]) HoverPopover() *HoverPopoverState {
	return s.lspExtras("HoverPopover").hoverPopover
}

// DismissLspOverlays closes the menus and the hover
// (U:input/editor/lsp/overlay.rs:213-216).
func (s *InputBaseState[M]) DismissLspOverlays(cx *gpui.Context[InputBaseState[M]]) {
	s.requireEditorMode("DismissLspOverlays")
	s.hideContextMenu(cx)
	s.ClearHoverState(cx)
}
