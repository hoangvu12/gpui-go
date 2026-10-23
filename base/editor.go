package base

// Port of U:input/editor/mod.rs: EditorState (the engine's code-editor
// kind), EditorMode's hook set, EditorExtras and the Editor component,
// plus the EditorState constructor and builders of
// U:input/base/state.rs:9984-10137 and the highlighter / fold-candidate
// plumbing of U:input/base/state.rs:795-840, 3810-3853.
//
// The hooks dispatching to the LSP providers call into
// editor_lsp*.go; the lsp() / lsp_mut() accessors
// (U:input/editor/mod.rs:194-206) are editor_lsp.go's Lsp / LspMut.

import (
	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// EditorExtras is what a code editor adds on top of multi-line text:
// language features (U:input/base/kind.rs:352-377).
type EditorExtras struct {
	lsp                Lsp
	decorations        decorationCollections[TextDecoration]
	rangeDecos         decorationCollections[RangeDecoration]
	inlineCompletion   inlineCompletion
	contextMenuContent contextMenuContent
	hoverPopover       *HoverPopoverState
	hoverDefinition    hoverDefinition
	contextMenuTask    lspTask
}

// decorationLayers is U:input/editor/mod.rs:236-238.
func (e *EditorExtras) decorationLayers() [][]TextDecoration { return e.decorations.layers() }

// rangeDecorations is U:input/editor/mod.rs:240-242.
func (e *EditorExtras) rangeDecorations(ranges []InputSelection) []RangeDecoration {
	return e.rangeDecos.intersecting(ranges)
}

// semanticTokenStyles is U:input/editor/mod.rs:244-251.
func (e *EditorExtras) semanticTokenStyles(text rope.Rope, rng InputSelection, resolver HighlightStyleResolver) []gpui.HighlightRange {
	return e.lsp.semanticTokensForRange(text, rng, resolver)
}

// documentColorSwatches is U:input/editor/mod.rs:253-259.
func (e *EditorExtras) documentColorSwatches(text rope.Rope, rng InputSelection) []inputColoredRange {
	return e.lsp.documentColorsForRange(text, rng)
}

// hoverSymbolRange is U:input/editor/mod.rs:261-265.
func (e *EditorExtras) hoverSymbolRange() (InputSelection, bool) {
	if e.hoverPopover == nil {
		return InputSelection{}, false
	}
	return e.hoverPopover.SymbolRange, true
}

// inlineCompletionText is the insert text of inline_completion_item
// (U:input/editor/mod.rs:267-269).
func (e *EditorExtras) inlineCompletionText() (string, bool) {
	if e.inlineCompletion.item == nil {
		return "", false
	}
	return e.inlineCompletion.item.InsertText, true
}

// contextMenuCapabilities is U:input/editor/mod.rs:271-276.
func (e *EditorExtras) contextMenuCapabilities() (bool, bool) {
	return e.lsp.DefinitionProvider != nil, len(e.lsp.CodeActionProviders) > 0
}

// editorModeHooks is `impl InputModeKind for EditorMode`
// (U:input/editor/mod.rs:13-192).
type editorModeHooks struct {
	inputModeDefaults[EditorMode]
}

// newExtras is EditorExtras::default (U:input/base/kind.rs:364-377).
func (editorModeHooks) newExtras() InputExtras { return &EditorExtras{lsp: DefaultLsp()} }

// hoverDefinitionStyle is U:input/editor/mod.rs:19-24.
func (editorModeHooks) hoverDefinitionStyle(s *EditorState, _ *gpui.App) (gpui.HighlightRange, bool) {
	return s.hoverDefinitionStyle()
}

// hoverDefinitionHitbox is U:input/editor/mod.rs:26-32.
func (editorModeHooks) hoverDefinitionHitbox(s *EditorState, w *gpui.Window, _ *gpui.App) (gpui.Hitbox, bool) {
	return s.hoverDefinitionHitbox(w)
}

// resetLanguageFeatures is U:input/editor/mod.rs:34-36.
func (editorModeHooks) resetLanguageFeatures(s *EditorState) { s.editorExtras().lsp.reset() }

// resetAnnotations drops the hover popover and the decorations
// (U:input/editor/mod.rs:38-42).
func (editorModeHooks) resetAnnotations(s *EditorState) {
	e := s.editorExtras()
	e.hoverPopover = nil
	e.decorations.clear()
	e.rangeDecos.clear()
}

// refreshLanguageFeatures is U:input/editor/mod.rs:60-67.
func (editorModeHooks) refreshLanguageFeatures(s *EditorState, w *gpui.Window, cx *gpui.Context[EditorState]) {
	s.editorExtras().lsp.update(s.text, w, cx)
}

// acceptInlineCompletion is U:input/editor/mod.rs:69-75.
func (editorModeHooks) acceptInlineCompletion(s *EditorState, w *gpui.Window, cx *gpui.Context[EditorState]) bool {
	return s.acceptInlineCompletion(w, cx)
}

// hasInlineCompletion is U:input/editor/mod.rs:77-79.
func (editorModeHooks) hasInlineCompletion(s *EditorState) bool { return s.hasInlineCompletion() }

// onClick is U:input/editor/mod.rs:81-89.
func (editorModeHooks) onClick(s *EditorState, event *gpui.MouseDownEvent, offset int, w *gpui.Window, cx *gpui.Context[EditorState]) bool {
	return s.handleClickHoverDefinition(event, offset, w, cx)
}

// clearHoverState is U:input/editor/mod.rs:91-96.
func (editorModeHooks) clearHoverState(s *EditorState, cx *gpui.Context[EditorState]) {
	s.ClearHoverState(cx)
}

// onTextTyped is U:input/editor/mod.rs:98-106.
func (editorModeHooks) onTextTyped(s *EditorState, rng InputSelection, text string, w *gpui.Window, cx *gpui.Context[EditorState]) {
	s.handleCompletionTrigger(rng, text, w, cx)
}

// clearInlineCompletion is U:input/editor/mod.rs:108-113.
func (editorModeHooks) clearInlineCompletion(s *EditorState, cx *gpui.Context[EditorState]) {
	s.clearInlineCompletion(cx)
}

// hideContextMenu is U:input/editor/mod.rs:115-120.
func (editorModeHooks) hideContextMenu(s *EditorState, cx *gpui.Context[EditorState]) {
	s.hideContextMenu(cx)
}

// isContextMenuOpen is U:input/editor/mod.rs:122-124.
func (editorModeHooks) isContextMenuOpen(s *EditorState, _ *gpui.App) bool {
	return s.isContextMenuOpen()
}

// handleContextMenuAction is U:input/editor/mod.rs:126-133.
func (editorModeHooks) handleContextMenuAction(s *EditorState, action gpui.BoxedAction, w *gpui.Window, cx *gpui.Context[EditorState]) bool {
	return s.handleActionForContextMenu(action, w, cx)
}

// onHoverDefinition is U:input/editor/mod.rs:135-142.
func (editorModeHooks) onHoverDefinition(s *EditorState, offset int, w *gpui.Window, cx *gpui.Context[EditorState]) {
	s.handleHoverDefinition(offset, w, cx)
}

// onMouseMove is U:input/editor/mod.rs:144-152.
func (editorModeHooks) onMouseMove(s *EditorState, offset int, event *gpui.MouseMoveEvent, w *gpui.Window, cx *gpui.Context[EditorState]) {
	s.handleMouseMove(offset, event, w, cx)
}

// registerActions registers ToggleCodeActions and GoToDefinition
// (U:input/editor/mod.rs:183-191).
func (editorModeHooks) registerActions(element *gpui.DivElement, entity gpui.Entity[EditorState], w *gpui.Window) *gpui.DivElement {
	return element.
		OnAction(ToggleCodeActionsAction, w.ListenerFor(entity, (*EditorState).onActionToggleCodeActions)).
		OnAction(GoToDefinitionAction, w.ListenerFor(entity, (*EditorState).onActionGoToDefinition))
}

// editingSyntaxContext is U:input/editor/mod.rs:44-46, 208-211.
func (editorModeHooks) editingSyntaxContext(s *EditorState, offset int) SyntaxContext {
	return s.mode.syntaxContextAt(s.text, offset)
}

// adjustAnnotations is U:input/editor/mod.rs:48-58.
func (editorModeHooks) adjustAnnotations(s *EditorState, rng InputSelection, newLen int) {
	e := s.editorExtras()
	e.decorations.adjustForEdit(rng, newLen)
	e.rangeDecos.adjustForEdit(rng, newLen)
}

// driveHighlighter is U:input/editor/mod.rs:154-167.
func (editorModeHooks) driveHighlighter(s *EditorState, edit InputEdit, text rope.Rope, folding bool, w *gpui.Window, cx *gpui.Context[EditorState]) {
	if h := s.mode.currentHighlighter(); h != nil {
		h.Update(&edit, text, folding, w, cx)
	}
}

// driveHighlighterBatch is U:input/editor/mod.rs:169-181.
func (editorModeHooks) driveHighlighterBatch(s *EditorState, edits []InputEditWithText, folding bool, w *gpui.Window, cx *gpui.Context[EditorState]) {
	if h := s.mode.currentHighlighter(); h != nil {
		highlighterUpdateBatch(h, edits, folding, w, cx)
	}
}

// editorExtras is `state.extras` of an editor; it panics on another mode.
func (s *InputBaseState[M]) editorExtras() *EditorExtras {
	e, ok := s.extras.(*EditorExtras)
	if !ok {
		panic("base: editor decorations are only available on EditorState")
	}
	return e
}

// requireEditorMode panics when an EditorState-only method is reached on
// another mode; upstream it does not compile (see input_kind.go).
func (s *InputBaseState[M]) requireEditorMode(method string) {
	if !s.IsCodeEditor() {
		panic("base: " + method + " is only available on EditorState")
	}
}

// NewEditorState creates a source-code editor state: line numbers, tab
// size 2 with soft tabs, indent guides, folding, automatic closing, smart
// indent and search (U:input/base/state.rs:9987-10002). Set the language
// with Language; without one the text is shown unhighlighted.
func NewEditorState(w *gpui.Window, cx *gpui.Context[EditorState]) EditorState {
	s := newInMode[EditorMode](w, cx)
	s.mode = layoutModeCodeEditor(newEditorLanguage(cx.App()))
	s.searchable = true
	return s
}

// Language sets the language to highlight, e.g. "rust"
// (U:input/base/state.rs:10004-10018). EditorState only.
func (s InputBaseState[M]) Language(language string) InputBaseState[M] {
	s.requireEditorMode("Language")
	s.mode.language.setName(language)
	s.mode.highlighter.h = nil
	return s
}

// LanguageName is the current language name
// (U:input/base/state.rs:10020-10026). EditorState only.
func (s *InputBaseState[M]) LanguageName() string {
	s.requireEditorMode("LanguageName")
	return s.mode.language.name
}

// Folding sets whether code folding is enabled, default true
// (U:input/base/state.rs:10028-10037, doc(hidden)). EditorState only.
func (s InputBaseState[M]) Folding(folding bool) InputBaseState[M] {
	s.requireEditorMode("Folding")
	s.mode.folding = folding
	return s
}

// SetFolding sets folding at runtime; disabling clears every fold
// (U:input/base/state.rs:10039-10050). EditorState only.
func (s *InputBaseState[M]) SetFolding(folding bool, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.requireEditorMode("SetFolding")
	s.mode.folding = folding
	if !folding {
		s.displayMap.clearFolds()
	}
	cx.Notify()
}

// LineNumber sets whether line numbers show
// (U:input/base/state.rs:10087-10094, doc(hidden)). EditorState only.
func (s InputBaseState[M]) LineNumber(lineNumber bool) InputBaseState[M] {
	s.requireEditorMode("LineNumber")
	s.mode.lineNumber = lineNumber
	return s
}

// SetLineNumber is U:input/base/state.rs:10096-10102. EditorState only.
func (s *InputBaseState[M]) SetLineNumber(lineNumber bool, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.requireEditorMode("SetLineNumber")
	s.mode.lineNumber = lineNumber
	cx.Notify()
}

// AutoClose sets automatic closing of brackets and quotes, default true
// (U:input/base/state.rs:10104-10113, doc(hidden)). EditorState only.
func (s InputBaseState[M]) AutoClose(autoClose bool) InputBaseState[M] {
	s.requireEditorMode("AutoClose")
	s.mode.setAutoClose(autoClose)
	return s
}

// SetAutoClose is U:input/base/state.rs:10115-10119. EditorState only.
func (s *InputBaseState[M]) SetAutoClose(autoClose bool, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.requireEditorMode("SetAutoClose")
	s.mode.setAutoClose(autoClose)
	cx.Notify()
}

// SmartIndent sets smart indentation on Enter, default true
// (U:input/base/state.rs:10121-10130, doc(hidden)). EditorState only.
func (s InputBaseState[M]) SmartIndent(smartIndent bool) InputBaseState[M] {
	s.requireEditorMode("SmartIndent")
	s.mode.setSmartIndent(smartIndent)
	return s
}

// SetSmartIndent is U:input/base/state.rs:10132-10136. EditorState only.
func (s *InputBaseState[M]) SetSmartIndent(smartIndent bool, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	s.requireEditorMode("SetSmartIndent")
	s.mode.setSmartIndent(smartIndent)
	cx.Notify()
}

// SetHighlighter sets the highlighter language of a code editor and
// requests a refresh (U:input/base/state.rs:795-813).
func (s *InputBaseState[M]) SetHighlighter(language string, cx *gpui.Context[InputBaseState[M]]) {
	if s.mode.kind == layoutCodeEditor {
		s.mode.language.setName(language)
		s.mode.highlighter.h = nil
	}
	s.Refresh(cx)
}

// SetHighlighterFactory installs the parser/highlighter adapter of the
// code-editor mode (U:input/base/state.rs:825-835).
func (s *InputBaseState[M]) SetHighlighterFactory(factory InputHighlighterFactory, cx *gpui.Context[InputBaseState[M]]) {
	s.mode.setHighlighterFactory(factory)
	s.pendingUpdate = true
	s.tokenLayoutCache = nil
	cx.Notify()
}

// EnsureHighlighterFactory installs a default adapter without replacing an
// application-provided one (U:input/base/state.rs:837-840).
func (s *InputBaseState[M]) EnsureHighlighterFactory(factory InputHighlighterFactory) {
	s.mode.ensureHighlighterFactory(factory)
}

// highlighterUpdate is HighlighterUpdate (U:input/base/mode.rs:14-21).
type highlighterUpdate struct {
	selectedRange InputSelection
	oldText       rope.Rope
	newText       rope.Rope
	changeText    string
	force         bool
}

// updateHighlighter is LayoutMode::update_highlighter: create the
// highlighter if needed and hand it the edit (U:input/base/mode.rs:268-301).
func (s *InputBaseState[M]) updateHighlighter(u highlighterUpdate, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	if s.mode.kind != layoutCodeEditor {
		return
	}
	if !u.force && s.mode.currentHighlighter() != nil {
		return
	}
	if !s.mode.ensureHighlighter() {
		return
	}
	edit := replacementInputEdit(u.oldText, u.newText, u.selectedRange, u.changeText)
	s.hooks.driveHighlighter(s, edit, u.newText, s.mode.folding, w, cx)
}

// updateHighlighterBatch is LayoutMode::update_highlighter_batch
// (U:input/base/mode.rs:303-325).
func (s *InputBaseState[M]) updateHighlighterBatch(edits []InputEditWithText, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	if s.mode.kind != layoutCodeEditor || !s.mode.ensureHighlighter() {
		return
	}
	s.hooks.driveHighlighterBatch(s, edits, s.mode.folding, w, cx)
}

// updateFoldCandidates extracts every fold candidate from the
// highlighter, on load or a language change
// (U:input/base/state.rs:3810-3828).
func (s *InputBaseState[M]) updateFoldCandidates() {
	if !s.mode.isFolding() {
		return
	}
	h := s.mode.currentHighlighter()
	if h == nil {
		return
	}
	s.displayMap.setFoldCandidates(h.FoldRanges(s.text))
}

// updateFoldCandidatesIncremental re-extracts the candidates of the edited
// region only (U:input/base/state.rs:3830-3853).
func (s *InputBaseState[M]) updateFoldCandidatesIncremental(editRange InputSelection, newText string) {
	if !s.mode.isFolding() {
		return
	}
	h := s.mode.currentHighlighter()
	if h == nil {
		return
	}
	newEnd := editRange.Start + len(newText)
	s.displayMap.updateFoldCandidatesForEdit(func(rng InputSelection, text *rope.Rope) []FoldRange {
		return highlighterFoldRangesForEdit(h, rng, *text)
	}, NewInputSelection(editRange.Start, newEnd), s.text)
}

// clampOffsetToVisibleBackward moves an offset on a folded line to the end
// of the fold's header line (U:input/base/state.rs:3249-3261).
func (s *InputBaseState[M]) clampOffsetToVisibleBackward(offset int) int {
	line := s.ext().OffsetToPoint(offset).Row
	if s.displayMap.isBufferLineHidden(line) {
		for _, fold := range s.displayMap.foldedRanges() {
			if line > fold.StartLine && line <= fold.EndLine {
				return s.ext().LineEndOffset(fold.StartLine)
			}
		}
	}
	return offset
}

// clampOffsetToVisibleForward moves an offset on a folded line to the
// start of the fold's end line (U:input/base/state.rs:3263-3275).
func (s *InputBaseState[M]) clampOffsetToVisibleForward(offset int) int {
	line := s.ext().OffsetToPoint(offset).Row
	if s.displayMap.isBufferLineHidden(line) {
		for _, fold := range s.displayMap.foldedRanges() {
			if line > fold.StartLine && line <= fold.EndLine {
				return s.ext().LineStartOffset(fold.EndLine)
			}
		}
	}
	return offset
}

// Editor is an unstyled source-code editor (U:input/editor/mod.rs:214-232).
type Editor struct {
	state gpui.Entity[EditorState]
}

// NewEditor is Editor::new (U:input/editor/mod.rs:220-226).
func NewEditor(state gpui.Entity[EditorState]) *Editor { return &Editor{state: state} }

// RenderOnce implements gpui.RenderOnce: the editor renders as its state
// view (U:input/editor/mod.rs:228-232).
func (e *Editor) RenderOnce(_ *gpui.Window, _ *gpui.App) gpui.AnyElement {
	element, err := gpui.TryIntoElement(gpui.ViewOf[EditorState](e.state))
	if err != nil {
		panic("base: Editor render: " + err.Error())
	}
	return element
}
