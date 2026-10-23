package base

// Port of U:input/base/kind.rs: the compile-time input modes and the hook
// seam the shared engine calls back into.
//
// Go adaptations:
//
//   - The three markers are empty structs; InputBaseState[M] is generic
//     over them, and InputState / TextareaState / EditorState are aliases
//     of its instantiations (U:input/base/kind.rs:6-10).
//   - The sealed trait InputModeKind is an interface with unexported
//     methods, so only this package can add a mode
//     (U:input/base/kind.rs:49-55, 135-137).
//   - Rust's associated constants MULTI_LINE / CODE_EDITOR are the marker
//     methods multiLine / codeEditor; the associated type Extras is the
//     InputExtras value the hooks build (newExtras).
//   - A trait method with a default body is a method of inputModeDefaults,
//     which every mode's hook set embeds and may shadow (Go's spelling of
//     a default method). Hooks receive the state they act on, as the Rust
//     hooks do through `&mut InputBaseState<Self>`. Go has no RefCell
//     borrow split, so drive_highlighter / drive_highlighter_batch take
//     the state instead of the highlighter cell
//     (U:input/base/kind.rs:163-182); the editor reads the cell from the
//     state's layout mode.
//   - A method that only exists on one mode upstream (InputState::masked,
//     TextareaState::rows, ...) cannot be restricted to one instantiation
//     of a Go generic type. Those methods exist on every instantiation and
//     panic on the wrong mode (requireInputMode); upstream it is a compile
//     error (U:input/base/kind.rs:12-21). Recorded deviation.
//
// EditorMode's hook set and EditorExtras are editor.go's (the editor's
// impl lives with the editor code upstream, U:input/editor/mod.rs:13-192,
// 234-277); the LSP state they answer from is editor_lsp*.go's.

import (
	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// InputMode is a single-line text field: the mode of InputState
// (U:input/base/kind.rs:40-41).
type InputMode struct{}

// TextareaMode is ordinary multi-line text: the mode of TextareaState
// (U:input/base/kind.rs:43-44).
type TextareaMode struct{}

// EditorMode is source code, with language features: the mode of
// EditorState (U:input/base/kind.rs:46-47).
type EditorMode struct{}

// InputModeKind is the sealed set of input modes the engine branches on
// (U:input/base/kind.rs:129-137). Only InputMode, TextareaMode and
// EditorMode implement it.
type InputModeKind interface {
	sealedInputMode()
	// multiLine is MULTI_LINE: whether this kind of input spans more than
	// one line. The kind decides this, not the layout
	// (U:input/base/kind.rs:138-145).
	multiLine() bool
	// codeEditor is CODE_EDITOR (U:input/base/kind.rs:147-148).
	codeEditor() bool
	// hookSet returns the mode's inputModeHooks[Self] (the trait's
	// methods); typed any because an interface method cannot name the
	// implementing type.
	hookSet() any
}

// MultiLineMode is the modes whose layout spans more than one line:
// TextareaMode and EditorMode (U:input/base/kind.rs:57-67).
type MultiLineMode interface {
	InputModeKind
	sealedMultiLineMode()
}

func (InputMode) sealedInputMode()    {}
func (TextareaMode) sealedInputMode() {}
func (EditorMode) sealedInputMode()   {}

func (TextareaMode) sealedMultiLineMode() {}
func (EditorMode) sealedMultiLineMode()   {}

// U:input/base/kind.rs:334-348.
func (InputMode) multiLine() bool    { return false }
func (TextareaMode) multiLine() bool { return true }
func (EditorMode) multiLine() bool   { return true }

func (InputMode) codeEditor() bool    { return false }
func (TextareaMode) codeEditor() bool { return false }

// codeEditor is EditorMode's CODE_EDITOR (the editor's impl, which lives
// with the editor code upstream, sets it).
func (EditorMode) codeEditor() bool { return true }

func (InputMode) hookSet() any    { return inputModeDefaults[InputMode]{} }
func (TextareaMode) hookSet() any { return inputModeDefaults[TextareaMode]{} }

// hookSet for EditorMode is the editor's implementation (editor.go,
// U:input/editor/mod.rs:13-192).
func (EditorMode) hookSet() any { return editorModeHooks{} }

// InputExtras is what the renderer may read out of a mode's extra state
// (U:input/base/kind.rs:69-124). Every answer is empty for a plain input
// and a textarea. The methods are unexported: only this package's modes
// supply extras.
type InputExtras interface {
	// documentColorSwatches are the document colors to paint as swatches
	// for a visible range (U:input/base/kind.rs:101-108).
	documentColorSwatches(text rope.Rope, rng InputSelection) []inputColoredRange
	// hoverSymbolRange is the symbol range the hover popover is anchored
	// to (U:input/base/kind.rs:110-113).
	hoverSymbolRange() (InputSelection, bool)
	// contextMenuCapabilities is what this mode can offer its context
	// menu: go-to-definition, code actions (U:input/base/kind.rs:120-123).
	contextMenuCapabilities() (goToDefinition, codeActions bool)
	// decorationLayers are the decoration ranges to paint, innermost
	// collection first (U:input/base/kind.rs:81-84).
	decorationLayers() [][]TextDecoration
	// rangeDecorations are the geometric decorations intersecting the
	// visible, non-folded buffer spans (U:input/base/kind.rs:86-89).
	rangeDecorations(ranges []InputSelection) []RangeDecoration
	// inlineCompletionText is the insert_text of inline_completion_item,
	// the inline completion to paint as ghost text
	// (U:input/base/kind.rs:115-118): the ghost-line paint reads only that
	// field.
	inlineCompletionText() (string, bool)
	// semanticTokenStyles are the semantic-token styles of a visible
	// range, when an LSP supplies them (U:input/base/kind.rs:91-99).
	semanticTokenStyles(text rope.Rope, rng InputSelection, resolver HighlightStyleResolver) []gpui.HighlightRange
}

// inputColoredRange is one (Range<usize>, Hsla) pair of
// document_color_swatches (U:input/base/kind.rs:102-108).
type inputColoredRange struct {
	rng   InputSelection
	color gpui.Hsla
}

// noInputExtras is `impl InputExtras for ()`: a mode with nothing extra
// to render (U:input/base/kind.rs:126-127).
type noInputExtras struct{}

func (noInputExtras) documentColorSwatches(rope.Rope, InputSelection) []inputColoredRange {
	return nil
}
func (noInputExtras) hoverSymbolRange() (InputSelection, bool) { return InputSelection{}, false }
func (noInputExtras) contextMenuCapabilities() (bool, bool)    { return false, false }
func (noInputExtras) decorationLayers() [][]TextDecoration     { return nil }
func (noInputExtras) rangeDecorations([]InputSelection) []RangeDecoration {
	return nil
}
func (noInputExtras) inlineCompletionText() (string, bool) { return "", false }
func (noInputExtras) semanticTokenStyles(rope.Rope, InputSelection, HighlightStyleResolver) []gpui.HighlightRange {
	return nil
}

// SyntaxContext is the syntax context at an offset, for editing decisions
// (U:input/editor/highlighting.rs:84-89). It is declared here because the
// editing_syntax_context hook returns it; the highlighting port keeps it.
type SyntaxContext uint8

const (
	// SyntaxContextCode is ordinary code.
	SyntaxContextCode SyntaxContext = iota
	// SyntaxContextString is inside a string literal.
	SyntaxContextString
	// SyntaxContextComment is inside a comment.
	SyntaxContextComment
)

// inputModeHooks is InputModeKind's behavior: the points where the engine
// hands control back during an edit (U:input/base/kind.rs:129-332).
type inputModeHooks[M InputModeKind] interface {
	// newExtras builds M::Extras::default() (U:input/base/kind.rs:150-156).
	newExtras() InputExtras
	// driveHighlighter drives the syntax highlighter after the text
	// changed (U:input/base/kind.rs:158-171).
	driveHighlighter(s *InputBaseState[M], edit InputEdit, text rope.Rope, folding bool, w *gpui.Window, cx *gpui.Context[InputBaseState[M]])
	// driveHighlighterBatch drives it once for several edits applied as
	// one change (U:input/base/kind.rs:173-182).
	driveHighlighterBatch(s *InputBaseState[M], edits []InputEditWithText, folding bool, w *gpui.Window, cx *gpui.Context[InputBaseState[M]])
	// hoverDefinitionStyle is the range highlighted while Cmd-hovering a
	// symbol (U:input/base/kind.rs:184-190).
	hoverDefinitionStyle(s *InputBaseState[M], app *gpui.App) (gpui.HighlightRange, bool)
	// hoverDefinitionHitbox is the hitbox for Cmd-hover
	// (U:input/base/kind.rs:192-199).
	hoverDefinitionHitbox(s *InputBaseState[M], w *gpui.Window, app *gpui.App) (gpui.Hitbox, bool)
	// resetLanguageFeatures drops cached language-server results
	// (U:input/base/kind.rs:201-202).
	resetLanguageFeatures(s *InputBaseState[M])
	// resetAnnotations drops decorations and hover state when the text is
	// replaced wholesale (U:input/base/kind.rs:204-205).
	resetAnnotations(s *InputBaseState[M])
	// editingSyntaxContext is the syntax context at offset
	// (U:input/base/kind.rs:207-213).
	editingSyntaxContext(s *InputBaseState[M], offset int) SyntaxContext
	// adjustAnnotations slides decoration ranges along with an edit
	// (U:input/base/kind.rs:215-221).
	adjustAnnotations(s *InputBaseState[M], rng InputSelection, newLen int)
	// refreshLanguageFeatures refreshes language-server state after the
	// text changed (U:input/base/kind.rs:223-229).
	refreshLanguageFeatures(s *InputBaseState[M], w *gpui.Window, cx *gpui.Context[InputBaseState[M]])
	// acceptInlineCompletion takes the pending inline completion
	// (U:input/base/kind.rs:231-238).
	acceptInlineCompletion(s *InputBaseState[M], w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) bool
	// hasInlineCompletion reports a waiting inline completion
	// (U:input/base/kind.rs:240-243).
	hasInlineCompletion(s *InputBaseState[M]) bool
	// onClick reacts to a click, for Cmd-click go-to-definition
	// (U:input/base/kind.rs:245-254).
	onClick(s *InputBaseState[M], event *gpui.MouseDownEvent, offset int, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) bool
	// clearHoverState drops hover state (U:input/base/kind.rs:256-261).
	clearHoverState(s *InputBaseState[M], cx *gpui.Context[InputBaseState[M]])
	// onTextTyped offers freshly typed text to the completion engine
	// (U:input/base/kind.rs:263-271).
	onTextTyped(s *InputBaseState[M], rng InputSelection, text string, w *gpui.Window, cx *gpui.Context[InputBaseState[M]])
	// clearInlineCompletion drops any inline completion
	// (U:input/base/kind.rs:273-278).
	clearInlineCompletion(s *InputBaseState[M], cx *gpui.Context[InputBaseState[M]])
	// hideContextMenu closes any open completion or code-action menu
	// (U:input/base/kind.rs:280-285).
	hideContextMenu(s *InputBaseState[M], cx *gpui.Context[InputBaseState[M]])
	// isContextMenuOpen reports an open completion or code-action menu
	// (U:input/base/kind.rs:287-290).
	isContextMenuOpen(s *InputBaseState[M], app *gpui.App) bool
	// handleContextMenuAction lets an open menu consume the action first
	// (U:input/base/kind.rs:292-300).
	handleContextMenuAction(s *InputBaseState[M], action gpui.BoxedAction, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) bool
	// onHoverDefinition highlights the symbol under the pointer
	// (U:input/base/kind.rs:302-312).
	onHoverDefinition(s *InputBaseState[M], offset int, w *gpui.Window, cx *gpui.Context[InputBaseState[M]])
	// onMouseMove reacts to the pointer moving (U:input/base/kind.rs:314-322).
	onMouseMove(s *InputBaseState[M], offset int, event *gpui.MouseMoveEvent, w *gpui.Window, cx *gpui.Context[InputBaseState[M]])
	// registerActions registers the actions only this mode handles
	// (U:input/base/kind.rs:324-331).
	registerActions(element *gpui.DivElement, entity gpui.Entity[InputBaseState[M]], w *gpui.Window) *gpui.DivElement
}

// inputModeDefaults carries the trait's default method bodies; every
// mode's hook set embeds it (U:input/base/kind.rs:158-331).
type inputModeDefaults[M InputModeKind] struct{}

func (inputModeDefaults[M]) newExtras() InputExtras { return noInputExtras{} }
func (inputModeDefaults[M]) driveHighlighter(*InputBaseState[M], InputEdit, rope.Rope, bool, *gpui.Window, *gpui.Context[InputBaseState[M]]) {
}
func (inputModeDefaults[M]) driveHighlighterBatch(*InputBaseState[M], []InputEditWithText, bool, *gpui.Window, *gpui.Context[InputBaseState[M]]) {
}
func (inputModeDefaults[M]) hoverDefinitionStyle(*InputBaseState[M], *gpui.App) (gpui.HighlightRange, bool) {
	return gpui.HighlightRange{}, false
}
func (inputModeDefaults[M]) hoverDefinitionHitbox(*InputBaseState[M], *gpui.Window, *gpui.App) (gpui.Hitbox, bool) {
	return gpui.Hitbox{}, false
}
func (inputModeDefaults[M]) resetLanguageFeatures(*InputBaseState[M]) {}
func (inputModeDefaults[M]) resetAnnotations(*InputBaseState[M])      {}
func (inputModeDefaults[M]) editingSyntaxContext(*InputBaseState[M], int) SyntaxContext {
	return SyntaxContextCode
}
func (inputModeDefaults[M]) adjustAnnotations(*InputBaseState[M], InputSelection, int) {}
func (inputModeDefaults[M]) refreshLanguageFeatures(*InputBaseState[M], *gpui.Window, *gpui.Context[InputBaseState[M]]) {
}
func (inputModeDefaults[M]) acceptInlineCompletion(*InputBaseState[M], *gpui.Window, *gpui.Context[InputBaseState[M]]) bool {
	return false
}
func (inputModeDefaults[M]) hasInlineCompletion(*InputBaseState[M]) bool { return false }
func (inputModeDefaults[M]) onClick(*InputBaseState[M], *gpui.MouseDownEvent, int, *gpui.Window, *gpui.Context[InputBaseState[M]]) bool {
	return false
}
func (inputModeDefaults[M]) clearHoverState(*InputBaseState[M], *gpui.Context[InputBaseState[M]]) {}
func (inputModeDefaults[M]) onTextTyped(*InputBaseState[M], InputSelection, string, *gpui.Window, *gpui.Context[InputBaseState[M]]) {
}
func (inputModeDefaults[M]) clearInlineCompletion(*InputBaseState[M], *gpui.Context[InputBaseState[M]]) {
}
func (inputModeDefaults[M]) hideContextMenu(*InputBaseState[M], *gpui.Context[InputBaseState[M]]) {}
func (inputModeDefaults[M]) isContextMenuOpen(*InputBaseState[M], *gpui.App) bool                 { return false }
func (inputModeDefaults[M]) handleContextMenuAction(*InputBaseState[M], gpui.BoxedAction, *gpui.Window, *gpui.Context[InputBaseState[M]]) bool {
	return false
}
func (inputModeDefaults[M]) onHoverDefinition(*InputBaseState[M], int, *gpui.Window, *gpui.Context[InputBaseState[M]]) {
}
func (inputModeDefaults[M]) onMouseMove(*InputBaseState[M], int, *gpui.MouseMoveEvent, *gpui.Window, *gpui.Context[InputBaseState[M]]) {
}
func (inputModeDefaults[M]) registerActions(element *gpui.DivElement, _ gpui.Entity[InputBaseState[M]], _ *gpui.Window) *gpui.DivElement {
	return element
}

// hooksOf returns mode M's hook set.
func hooksOf[M InputModeKind]() inputModeHooks[M] {
	var m M
	return m.hookSet().(inputModeHooks[M])
}

// modeOf returns M's zero marker, for MULTI_LINE / CODE_EDITOR.
func modeOf[M InputModeKind]() M {
	var m M
	return m
}
