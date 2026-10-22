package base

// Port of U:input/base/state.rs, the shared text-editing engine behind
// InputState, TextareaState and EditorState. The engine is split over
// input_state*.go:
//
//   - input_state.go: actions, key bindings, InputEvent, the state and its
//     configuration, value and selection API, boundaries, focus.
//   - input_state_edit.go: editing, history, clipboard, the IME handler.
//   - input_state_mouse.go: pointer input, scrolling, selection extension.
//   - input_state_render.go: Render (action and listener registration).
//   - input_state_style.go: InputEditorStyle.
//
// This ticket ports the single-line engine. Parts owned by other modules
// join with their tickets and are stand-ins here:
//
//   - inline tokens (U:input/base/inline_tokens.rs, token_presentation.rs):
//     no token spans exist, so token_boundary / normalize_token_range are
//     the identity, edit_tokens records no delta and ActivateToken has no
//     span to activate (input_state_edit.go);
//   - search (U:input/editor/search.rs): update_search has no session; the
//     Search / Replace actions propagate, which is what a non-searchable
//     input does (only the multi-line modes can be searchable);
//   - touch selection (U:input/base/touch.rs): no touch selection exists,
//     so dismissing it is a no-op;
//   - the native context menu (U:input/base/native.rs): on_context_menu and
//     handle_right_click_menu need NativeMenu; a right press still moves
//     the caret like upstream;
//   - number stepping (number_input.rs NumberStep / StepAction): the
//     number_step field and step / step_by / set_step join with the
//     number-input ticket; min / max and the blur clamp are here;
//   - the display map (U:input/editor/display_map): inputDisplayMap
//     (input_layout.go);
//   - code-editor language state, highlighter, diagnostics, folding,
//     indentation, auto close (U:input/editor/*): the hooks and layoutMode
//     flags are wired; their data joins with the editor tickets.
//
// IME: InputBaseState implements the pinned EntityInputHandler
// (R:input.rs:36-147) and paints register an adapter implementing
// gpui.IMEInputHandler (gpui/ime.go:135) through Window.HandleInput, the
// pinned ElementInputHandler (R:input.rs:153-305).

import (
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/base/segment"
	"github.com/hoangvu12/gpui-go/base/sumtree"
	"github.com/hoangvu12/gpui-go/gpui"
)

// scrollPadding is the vertical clearance kept when revealing a text
// position (U:input/base/state.rs:42-46).
type scrollPadding uint8

const (
	scrollPaddingMinimal scrollPadding = iota
	scrollPaddingSurroundingLines
)

// Enter confirms the input (U:input/base/state.rs:48-55;
// #[action(namespace = input, no_json)]).
type Enter struct {
	// Secondary reports a confirmation with the secondary modifier.
	Secondary bool
	// Shift reports whether Shift was held when Enter was pressed.
	Shift bool
}

// EnterAction is the input::Enter descriptor.
var EnterAction = gpui.DefineAction(gpui.ActionSpec[Enter]{
	Name:  "input::Enter",
	Clone: func(e Enter) Enter { return e },
	Equal: func(a, b Enter) bool { return a == b },
})

// EnterIsPrimary reports whether action is a primary Enter
// (secondary: false), regardless of Shift (U:input/base/state.rs:57-69).
func EnterIsPrimary(action gpui.BoxedAction) bool {
	return action.Equal(EnterAction.Box(Enter{})) || action.Equal(EnterAction.Box(Enter{Shift: true}))
}

// The unit actions of actions!(input, [...]) (U:input/base/state.rs:71-120).
type (
	// ActivateToken activates the selected inline token.
	ActivateToken struct{}
	// Backspace deletes backwards.
	Backspace struct{}
	// Delete deletes forwards.
	Delete struct{}
	// DeleteToBeginningOfLine deletes to the line start.
	DeleteToBeginningOfLine struct{}
	// DeleteToEndOfLine deletes to the line end.
	DeleteToEndOfLine struct{}
	// DeleteToPreviousWordStart deletes the previous word.
	DeleteToPreviousWordStart struct{}
	// DeleteToNextWordEnd deletes the next word.
	DeleteToNextWordEnd struct{}
	// Indent indents the selected lines.
	Indent struct{}
	// Outdent outdents the selected lines.
	Outdent struct{}
	// IndentInline indents at the cursor (Tab).
	IndentInline struct{}
	// OutdentInline outdents at the cursor (Shift-Tab).
	OutdentInline struct{}
	// MoveUp moves the cursor up one row.
	MoveUp struct{}
	// MoveDown moves the cursor down one row.
	MoveDown struct{}
	// MoveLeft moves the cursor left.
	MoveLeft struct{}
	// MoveRight moves the cursor right.
	MoveRight struct{}
	// MoveHome moves the cursor to the line start.
	MoveHome struct{}
	// MoveEnd moves the cursor to the line end.
	MoveEnd struct{}
	// MovePageUp moves the cursor up one page.
	MovePageUp struct{}
	// MovePageDown moves the cursor down one page.
	MovePageDown struct{}
	// AddCursorAbove adds a cursor one row up.
	AddCursorAbove struct{}
	// AddCursorBelow adds a cursor one row down.
	AddCursorBelow struct{}
	// SelectAll selects the whole text.
	SelectAll struct{}
	// SelectToStartOfLine extends the selection to the line start.
	SelectToStartOfLine struct{}
	// SelectToEndOfLine extends the selection to the line end.
	SelectToEndOfLine struct{}
	// SelectToStart extends the selection to the text start.
	SelectToStart struct{}
	// SelectToEnd extends the selection to the text end.
	SelectToEnd struct{}
	// SelectToPreviousWordStart extends the selection by a word backwards.
	SelectToPreviousWordStart struct{}
	// SelectToNextWordEnd extends the selection by a word forwards.
	SelectToNextWordEnd struct{}
	// ShowCharacterPalette shows the platform character palette.
	ShowCharacterPalette struct{}
	// Copy copies the selection.
	Copy struct{}
	// Cut cuts the selection.
	Cut struct{}
	// Paste pastes the clipboard.
	Paste struct{}
	// Undo undoes the last change.
	Undo struct{}
	// Redo redoes the last undone change.
	Redo struct{}
	// MoveToStartOfLine moves the cursor to the line start.
	MoveToStartOfLine struct{}
	// MoveToEndOfLine moves the cursor to the line end.
	MoveToEndOfLine struct{}
	// MoveToStart moves the cursor to the text start.
	MoveToStart struct{}
	// MoveToEnd moves the cursor to the text end.
	MoveToEnd struct{}
	// MoveToPreviousWord moves the cursor to the previous word start.
	MoveToPreviousWord struct{}
	// MoveToNextWord moves the cursor to the next word end.
	MoveToNextWord struct{}
	// Escape cancels.
	Escape struct{}
	// ToggleCodeActions toggles the code-action menu.
	ToggleCodeActions struct{}
	// Search opens search.
	Search struct{}
	// Replace opens search and replace.
	Replace struct{}
	// GoToDefinition goes to the symbol's definition.
	GoToDefinition struct{}
)

// Descriptors of the input unit actions (U:input/base/state.rs:71-120).
var (
	ActivateTokenAction             = gpui.DefineUnitAction[ActivateToken]("input::ActivateToken")
	BackspaceAction                 = gpui.DefineUnitAction[Backspace]("input::Backspace")
	DeleteAction                    = gpui.DefineUnitAction[Delete]("input::Delete")
	DeleteToBeginningOfLineAction   = gpui.DefineUnitAction[DeleteToBeginningOfLine]("input::DeleteToBeginningOfLine")
	DeleteToEndOfLineAction         = gpui.DefineUnitAction[DeleteToEndOfLine]("input::DeleteToEndOfLine")
	DeleteToPreviousWordStartAction = gpui.DefineUnitAction[DeleteToPreviousWordStart]("input::DeleteToPreviousWordStart")
	DeleteToNextWordEndAction       = gpui.DefineUnitAction[DeleteToNextWordEnd]("input::DeleteToNextWordEnd")
	IndentAction                    = gpui.DefineUnitAction[Indent]("input::Indent")
	OutdentAction                   = gpui.DefineUnitAction[Outdent]("input::Outdent")
	IndentInlineAction              = gpui.DefineUnitAction[IndentInline]("input::IndentInline")
	OutdentInlineAction             = gpui.DefineUnitAction[OutdentInline]("input::OutdentInline")
	MoveUpAction                    = gpui.DefineUnitAction[MoveUp]("input::MoveUp")
	MoveDownAction                  = gpui.DefineUnitAction[MoveDown]("input::MoveDown")
	MoveLeftAction                  = gpui.DefineUnitAction[MoveLeft]("input::MoveLeft")
	MoveRightAction                 = gpui.DefineUnitAction[MoveRight]("input::MoveRight")
	MoveHomeAction                  = gpui.DefineUnitAction[MoveHome]("input::MoveHome")
	MoveEndAction                   = gpui.DefineUnitAction[MoveEnd]("input::MoveEnd")
	MovePageUpAction                = gpui.DefineUnitAction[MovePageUp]("input::MovePageUp")
	MovePageDownAction              = gpui.DefineUnitAction[MovePageDown]("input::MovePageDown")
	AddCursorAboveAction            = gpui.DefineUnitAction[AddCursorAbove]("input::AddCursorAbove")
	AddCursorBelowAction            = gpui.DefineUnitAction[AddCursorBelow]("input::AddCursorBelow")
	SelectAllAction                 = gpui.DefineUnitAction[SelectAll]("input::SelectAll")
	SelectToStartOfLineAction       = gpui.DefineUnitAction[SelectToStartOfLine]("input::SelectToStartOfLine")
	SelectToEndOfLineAction         = gpui.DefineUnitAction[SelectToEndOfLine]("input::SelectToEndOfLine")
	SelectToStartAction             = gpui.DefineUnitAction[SelectToStart]("input::SelectToStart")
	SelectToEndAction               = gpui.DefineUnitAction[SelectToEnd]("input::SelectToEnd")
	SelectToPreviousWordStartAction = gpui.DefineUnitAction[SelectToPreviousWordStart]("input::SelectToPreviousWordStart")
	SelectToNextWordEndAction       = gpui.DefineUnitAction[SelectToNextWordEnd]("input::SelectToNextWordEnd")
	ShowCharacterPaletteAction      = gpui.DefineUnitAction[ShowCharacterPalette]("input::ShowCharacterPalette")
	CopyAction                      = gpui.DefineUnitAction[Copy]("input::Copy")
	CutAction                       = gpui.DefineUnitAction[Cut]("input::Cut")
	PasteAction                     = gpui.DefineUnitAction[Paste]("input::Paste")
	UndoAction                      = gpui.DefineUnitAction[Undo]("input::Undo")
	RedoAction                      = gpui.DefineUnitAction[Redo]("input::Redo")
	MoveToStartOfLineAction         = gpui.DefineUnitAction[MoveToStartOfLine]("input::MoveToStartOfLine")
	MoveToEndOfLineAction           = gpui.DefineUnitAction[MoveToEndOfLine]("input::MoveToEndOfLine")
	MoveToStartAction               = gpui.DefineUnitAction[MoveToStart]("input::MoveToStart")
	MoveToEndAction                 = gpui.DefineUnitAction[MoveToEnd]("input::MoveToEnd")
	MoveToPreviousWordAction        = gpui.DefineUnitAction[MoveToPreviousWord]("input::MoveToPreviousWord")
	MoveToNextWordAction            = gpui.DefineUnitAction[MoveToNextWord]("input::MoveToNextWord")
	EscapeAction                    = gpui.DefineUnitAction[Escape]("input::Escape")
	ToggleCodeActionsAction         = gpui.DefineUnitAction[ToggleCodeActions]("input::ToggleCodeActions")
	SearchAction                    = gpui.DefineUnitAction[Search]("input::Search")
	ReplaceAction                   = gpui.DefineUnitAction[Replace]("input::Replace")
	GoToDefinitionAction            = gpui.DefineUnitAction[GoToDefinition]("input::GoToDefinition")
)

// InputEventKind is the InputEvent variant (U:input/base/state.rs:122-128).
type InputEventKind uint8

const (
	// InputEventChange reports a text change.
	InputEventChange InputEventKind = iota
	// InputEventPressEnter reports Enter (Secondary, Shift).
	InputEventPressEnter
	// InputEventFocus reports the input gaining focus.
	InputEventFocus
	// InputEventBlur reports the input losing focus.
	InputEventBlur
)

// InputEvent is an input state event (U:input/base/state.rs:122-128).
// Secondary and Shift are the PressEnter payload.
type InputEvent struct {
	Kind      InputEventKind
	Secondary bool
	Shift     bool
}

// InputBaseStateEvents is the InputEvent emitter of a mode's state
// (`impl EventEmitter<InputEvent> for InputBaseState<M>`,
// U:input/base/state.rs:541).
func InputBaseStateEvents[M InputModeKind]() gpui.Event[InputBaseState[M], InputEvent] {
	return gpui.DefineEvent[InputBaseState[M], InputEvent]()
}

// InputStateEvents is InputState's InputEvent emitter.
var InputStateEvents = InputBaseStateEvents[InputMode]()

// inputContext is the input key context (U:input/base/state.rs:130).
const inputContext = "Input"

// inputInit binds the input keys (U:input/base/state.rs:132-305). The
// platform cfgs are runtime.GOOS checks.
func inputInit(app *gpui.App) {
	ctx := inputContext
	macos := runtime.GOOS == "darwin"
	windows := runtime.GOOS == "windows"
	linux := runtime.GOOS == "linux"
	var bindings []gpui.KeyBinding
	bind := func(when bool, keys string, action gpui.BoxedAction) {
		if when {
			bindings = append(bindings, gpui.NewKeyBinding(keys, action, &ctx))
		}
	}
	bind(true, "backspace", BackspaceAction.Box(Backspace{}))
	bind(true, "shift-backspace", BackspaceAction.Box(Backspace{}))
	bind(macos, "ctrl-backspace", BackspaceAction.Box(Backspace{}))
	bind(true, "delete", DeleteAction.Box(Delete{}))
	bind(true, "shift-delete", DeleteAction.Box(Delete{}))
	bind(macos, "cmd-backspace", DeleteToBeginningOfLineAction.Box(DeleteToBeginningOfLine{}))
	bind(macos, "cmd-delete", DeleteToEndOfLineAction.Box(DeleteToEndOfLine{}))
	bind(macos, "alt-backspace", DeleteToPreviousWordStartAction.Box(DeleteToPreviousWordStart{}))
	bind(!macos, "ctrl-backspace", DeleteToPreviousWordStartAction.Box(DeleteToPreviousWordStart{}))
	bind(macos, "alt-delete", DeleteToNextWordEndAction.Box(DeleteToNextWordEnd{}))
	bind(!macos, "ctrl-delete", DeleteToNextWordEndAction.Box(DeleteToNextWordEnd{}))
	bind(true, "enter", EnterAction.Box(Enter{}))
	bind(true, "shift-enter", EnterAction.Box(Enter{Shift: true}))
	bind(true, "secondary-enter", EnterAction.Box(Enter{Secondary: true}))
	bind(true, "escape", EscapeAction.Box(Escape{}))
	bind(true, "up", MoveUpAction.Box(MoveUp{}))
	bind(true, "down", MoveDownAction.Box(MoveDown{}))
	bind(true, "left", MoveLeftAction.Box(MoveLeft{}))
	bind(true, "right", MoveRightAction.Box(MoveRight{}))
	bind(true, "pageup", MovePageUpAction.Box(MovePageUp{}))
	bind(true, "pagedown", MovePageDownAction.Box(MovePageDown{}))
	bind(true, "tab", IndentInlineAction.Box(IndentInline{}))
	bind(true, "shift-tab", OutdentInlineAction.Box(OutdentInline{}))
	bind(macos, "cmd-]", IndentAction.Box(Indent{}))
	bind(!macos, "ctrl-]", IndentAction.Box(Indent{}))
	bind(macos, "cmd-[", OutdentAction.Box(Outdent{}))
	bind(!macos, "ctrl-[", OutdentAction.Box(Outdent{}))
	bind(true, "shift-left", SelectLeftAction.Box(SelectLeft{}))
	bind(true, "shift-right", SelectRightAction.Box(SelectRight{}))
	bind(true, "shift-up", SelectUpAction.Box(SelectUp{}))
	bind(true, "shift-down", SelectDownAction.Box(SelectDown{}))
	bind(!macos && !linux, "shift-alt-left", SelectLeftAction.Box(SelectLeft{}))
	bind(!macos && !linux, "shift-alt-right", SelectRightAction.Box(SelectRight{}))
	bind(macos, "cmd-alt-up", AddCursorAboveAction.Box(AddCursorAbove{}))
	bind(macos, "cmd-alt-down", AddCursorBelowAction.Box(AddCursorBelow{}))
	bind(windows, "ctrl-alt-up", AddCursorAboveAction.Box(AddCursorAbove{}))
	bind(!macos && !windows, "shift-alt-up", AddCursorAboveAction.Box(AddCursorAbove{}))
	bind(windows, "ctrl-alt-down", AddCursorBelowAction.Box(AddCursorBelow{}))
	bind(!macos && !windows, "shift-alt-down", AddCursorBelowAction.Box(AddCursorBelow{}))
	bind(true, "home", MoveHomeAction.Box(MoveHome{}))
	bind(true, "end", MoveEndAction.Box(MoveEnd{}))
	bind(!macos, "ctrl-home", MoveToStartAction.Box(MoveToStart{}))
	bind(!macos, "ctrl-end", MoveToEndAction.Box(MoveToEnd{}))
	bind(!macos, "ctrl-shift-home", SelectToStartAction.Box(SelectToStart{}))
	bind(!macos, "ctrl-shift-end", SelectToEndAction.Box(SelectToEnd{}))
	bind(true, "shift-home", SelectToStartOfLineAction.Box(SelectToStartOfLine{}))
	bind(true, "shift-end", SelectToEndOfLineAction.Box(SelectToEndOfLine{}))
	bind(macos, "ctrl-shift-a", SelectToStartOfLineAction.Box(SelectToStartOfLine{}))
	bind(macos, "ctrl-shift-e", SelectToEndOfLineAction.Box(SelectToEndOfLine{}))
	bind(macos, "shift-cmd-left", SelectToStartOfLineAction.Box(SelectToStartOfLine{}))
	bind(macos, "shift-cmd-right", SelectToEndOfLineAction.Box(SelectToEndOfLine{}))
	bind(macos || linux, "alt-shift-left", SelectToPreviousWordStartAction.Box(SelectToPreviousWordStart{}))
	bind(!macos, "ctrl-shift-left", SelectToPreviousWordStartAction.Box(SelectToPreviousWordStart{}))
	bind(macos || linux, "alt-shift-right", SelectToNextWordEndAction.Box(SelectToNextWordEnd{}))
	bind(!macos, "ctrl-shift-right", SelectToNextWordEndAction.Box(SelectToNextWordEnd{}))
	bind(macos, "ctrl-cmd-space", ShowCharacterPaletteAction.Box(ShowCharacterPalette{}))
	bind(macos, "cmd-a", SelectAllAction.Box(SelectAll{}))
	bind(!macos, "ctrl-a", SelectAllAction.Box(SelectAll{}))
	bind(macos, "cmd-c", CopyAction.Box(Copy{}))
	bind(!macos, "ctrl-c", CopyAction.Box(Copy{}))
	bind(macos, "cmd-x", CutAction.Box(Cut{}))
	bind(!macos, "ctrl-x", CutAction.Box(Cut{}))
	bind(macos, "cmd-v", PasteAction.Box(Paste{}))
	bind(!macos, "ctrl-v", PasteAction.Box(Paste{}))
	bind(macos, "ctrl-a", MoveHomeAction.Box(MoveHome{}))
	bind(macos, "cmd-left", MoveHomeAction.Box(MoveHome{}))
	bind(macos, "ctrl-e", MoveEndAction.Box(MoveEnd{}))
	bind(macos, "cmd-right", MoveEndAction.Box(MoveEnd{}))
	bind(macos, "cmd-z", UndoAction.Box(Undo{}))
	bind(macos, "cmd-shift-z", RedoAction.Box(Redo{}))
	bind(macos, "cmd-up", MoveToStartAction.Box(MoveToStart{}))
	bind(macos, "cmd-down", MoveToEndAction.Box(MoveToEnd{}))
	bind(macos, "alt-left", MoveToPreviousWordAction.Box(MoveToPreviousWord{}))
	bind(macos, "alt-right", MoveToNextWordAction.Box(MoveToNextWord{}))
	bind(!macos, "ctrl-left", MoveToPreviousWordAction.Box(MoveToPreviousWord{}))
	bind(!macos, "ctrl-right", MoveToNextWordAction.Box(MoveToNextWord{}))
	bind(macos, "cmd-shift-up", SelectToStartAction.Box(SelectToStart{}))
	bind(macos, "cmd-shift-down", SelectToEndAction.Box(SelectToEnd{}))
	bind(!macos, "ctrl-z", UndoAction.Box(Undo{}))
	bind(!macos, "ctrl-y", RedoAction.Box(Redo{}))
	bind(macos, "cmd-.", ToggleCodeActionsAction.Box(ToggleCodeActions{}))
	bind(!macos, "ctrl-.", ToggleCodeActionsAction.Box(ToggleCodeActions{}))
	bind(macos, "cmd-f", SearchAction.Box(Search{}))
	bind(!macos, "ctrl-f", SearchAction.Box(Search{}))
	bind(macos, "cmd-shift-f", ReplaceAction.Box(Replace{}))
	bind(!macos, "ctrl-h", ReplaceAction.Box(Replace{}))
	app.BindKeys(bindings...)
}

// isLinux is cfg!(target_os = "linux").
func isLinux() bool { return runtime.GOOS == "linux" }

// columnarPoint is a mouse position resolved for a columnar selection:
// the clipped offset plus the columns past the row's end
// (U:input/base/state.rs:307-326).
type columnarPoint struct {
	offset             int
	columnsPastLineEnd int
}

// inputCx is the context of a mode's state entity.
type inputCx[M InputModeKind] = gpui.Context[InputBaseState[M]]

// InputBaseState is the shared text-editing engine behind InputState,
// TextareaState and EditorState (U:input/base/state.rs:328-471). M is the
// mode marker. Prefer the aliases: write InputState, not
// InputBaseState[InputMode].
type InputBaseState[M InputModeKind] struct {
	hooks inputModeHooks[M]
	// extras is the state only this mode needs (M::Extras).
	extras InputExtras

	focusHandle gpui.FocusHandle
	mode        layoutMode
	text        rope.Rope
	displayMap  inputDisplayMap
	undoManager *undoManager

	replayingHistory   bool
	validatedTokenEdit bool
	documentRevision   uint64

	searchable             bool
	replaceable            bool
	softWrap               bool
	scrollBeyondLastLine   *int
	cursorSurroundingLines *int
	blinkCursor            gpui.Entity[blinkCursor]
	loading                bool

	// selections always holds at least one selection; index 0 is the
	// active cursor.
	selections *selections
	// selectedWordRange keeps the double-clicked word while dragging.
	selectedWordRange *cursorSelection
	// imeMarkedRange is the IME composition's temporary text.
	imeMarkedRange *cursorSelection
	lastLayout     *lastLayout
	lastCursor     *int
	// inputBounds is the input container bounds.
	inputBounds gpui.Bounds
	// lastBounds is the text bounds.
	lastBounds        *gpui.Bounds
	lastSelectedRange *cursorSelection
	selecting         bool
	columnSelectStart *columnarPoint

	disabled        bool
	readonly        bool
	textAlign       gpui.TextAlign
	masked          bool
	cleanOnEscape   bool
	submitOnEnter   bool
	showWhitespaces bool
	// cursorLineEndAffinity tells the renderer to prefer the end of the
	// current visual line.
	cursorLineEndAffinity bool
	pattern               *regexp.Regexp
	validate              func(text string, app *gpui.App) bool
	numberMin             *float64
	numberMax             *float64

	scrollHandle gpui.ScrollHandle
	// deferredScrollOffset is applied on the next layout.
	deferredScrollOffset *gpui.Point
	// scrollSize is the size of the scrollable content.
	scrollSize gpui.Size
	// longestLineWidth caches the unwrapped longest-line width.
	longestLineWidth *longestLineCache
	editorPaddings   gpui.Edges
	// editorStyle is the style this state paints with, resolved at the
	// top of every render; projectedEditorStyle is what a consumer
	// projected, kept verbatim (U:input/base/state.rs:415-424).
	editorStyle          InputEditorStyle
	projectedEditorStyle InputEditorStyle

	maskPattern    MaskPattern
	maskPatternSet bool
	placeholder    string

	enableContextMenu   bool
	completionInserting bool

	// pendingUpdate requests the highlighter and LSP refresh on render.
	pendingUpdate bool
	// silentReplaceText suppresses UI reactions such as completion.
	silentReplaceText bool
	emitEvents        bool

	subscriptions []gpui.Subscription
	keystrokeSub  interface{ Cancel() }
	autoScroll    *AutoScroll
}

// longestLineCache is the (LongestLineKey, Pixels) cell
// (U:input/base/element.rs:123-134).
type longestLineCache struct {
	documentRevision uint64
	row              int
	length           int
	font             gpui.FontDescriptor
	textSize         float32
	wrapWidth        *float32
	width            float32
}

// InputState is the state of a single-line text input
// (U:input/input/mod.rs:5-10).
type InputState = InputBaseState[InputMode]

// TextareaState is the state of a multi-line text input
// (U:input/mod.rs:123; the textarea module is a later ticket).
type TextareaState = InputBaseState[TextareaMode]

// EditorState is the state of a code editor (U:input/mod.rs:93).
type EditorState = InputBaseState[EditorMode]

// InputPresentation is read-only styling data exposed to presentation
// facades (U:input/base/state.rs:473-539).
type InputPresentation struct {
	focusHandle     gpui.FocusHandle
	disabled        bool
	readonly        bool
	loading         bool
	masked          bool
	multiLine       bool
	codeEditor      bool
	textAlign       gpui.TextAlign
	placeholder     string
	maskPlaceholder *string
}

// FocusHandle is U:input/base/state.rs:492-494.
func (p InputPresentation) FocusHandle() gpui.FocusHandle { return p.focusHandle }

// IsDisabled is U:input/base/state.rs:496-498.
func (p InputPresentation) IsDisabled() bool { return p.disabled }

// IsReadonly is U:input/base/state.rs:500-502.
func (p InputPresentation) IsReadonly() bool { return p.readonly }

// IsEditable reports whether the user may change the text
// (U:input/base/state.rs:504-509).
func (p InputPresentation) IsEditable() bool { return !p.disabled && !p.readonly }

// IsLoading is U:input/base/state.rs:511-513.
func (p InputPresentation) IsLoading() bool { return p.loading }

// IsMasked is U:input/base/state.rs:515-517.
func (p InputPresentation) IsMasked() bool { return p.masked }

// IsMultiLine is U:input/base/state.rs:519-521.
func (p InputPresentation) IsMultiLine() bool { return p.multiLine }

// IsCodeEditor is U:input/base/state.rs:523-525.
func (p InputPresentation) IsCodeEditor() bool { return p.codeEditor }

// TextAlign is U:input/base/state.rs:527-529.
func (p InputPresentation) TextAlign() gpui.TextAlign { return p.textAlign }

// Placeholder is U:input/base/state.rs:531-533.
func (p InputPresentation) Placeholder() string { return p.placeholder }

// MaskPlaceholder is the placeholder derived from the mask pattern, e.g.
// `(___) ___-____` (U:input/base/state.rs:535-538).
func (p InputPresentation) MaskPlaceholder() (string, bool) {
	if p.maskPlaceholder == nil {
		return "", false
	}
	return *p.maskPlaceholder, true
}

func (s *InputBaseState[M]) ext() RopeExt { return RopeExtOf(&s.text) }

// CursorLayout is the active caret's bounds and the line height, once
// painted (U:input/base/state.rs:544-548, doc(hidden)).
func (s *InputBaseState[M]) CursorLayout() (gpui.Bounds, float32, bool) {
	if s.lastLayout == nil || s.lastLayout.cursorBounds == nil {
		return gpui.Bounds{}, 0, false
	}
	return *s.lastLayout.cursorBounds, s.lastLayout.lineHeight, true
}

// InputBounds is the input container bounds (U:input/base/state.rs:550-552).
func (s *InputBaseState[M]) InputBounds() gpui.Bounds { return s.inputBounds }

// TextBounds is the text bounds, once painted (U:input/base/state.rs:554-556).
func (s *InputBaseState[M]) TextBounds() (gpui.Bounds, bool) {
	if s.lastBounds == nil {
		return gpui.Bounds{}, false
	}
	return *s.lastBounds, true
}

// Presentation is U:input/base/state.rs:562-575.
func (s *InputBaseState[M]) Presentation() InputPresentation {
	p := InputPresentation{
		focusHandle: s.focusHandle,
		disabled:    s.disabled,
		readonly:    s.readonly,
		loading:     s.loading,
		masked:      s.masked,
		multiLine:   s.IsMultiLine(),
		codeEditor:  s.IsCodeEditor(),
		textAlign:   s.textAlign,
		placeholder: s.placeholder,
	}
	if placeholder, ok := s.maskPattern.Placeholder(); ok {
		p.maskPlaceholder = &placeholder
	}
	return p
}

// showsScrollbar reports whether the input paints scrollbars: only a
// multi-line input scrolls (U:input/base/state.rs:577-591).
func (s *InputBaseState[M]) showsScrollbar() bool { return s.IsMultiLine() }

// IsMultiLine is answered by the mode marker (U:input/base/state.rs:593-595).
func (s *InputBaseState[M]) IsMultiLine() bool { return modeOf[M]().multiLine() }

// IsSingleLine is U:input/base/state.rs:597-601.
func (s *InputBaseState[M]) IsSingleLine() bool { return !modeOf[M]().multiLine() }

// IsCodeEditor is U:input/base/state.rs:603-607.
func (s *InputBaseState[M]) IsCodeEditor() bool { return modeOf[M]().codeEditor() }

// IsCopyable reports whether the user may copy the selection out; a
// masked input keeps its value out of the clipboard
// (U:input/base/state.rs:609-614).
func (s *InputBaseState[M]) IsCopyable() bool {
	for sel := range s.selections.iter() {
		if !sel.isEmpty() {
			return !s.masked
		}
	}
	return false
}

// ContextMenuCapabilities is U:input/base/state.rs:616-626.
func (s *InputBaseState[M]) ContextMenuCapabilities() InputContextMenuCapabilities {
	goToDefinition, codeActions := s.extras.contextMenuCapabilities()
	return NewInputContextMenuCapabilities().
		Disabled(s.disabled).
		Readonly(s.readonly).
		CodeEditor(s.IsCodeEditor()).
		Selection(!s.activeSelection().isEmpty()).
		Masked(s.masked).
		GoToDefinition(goToDefinition).
		CodeActions(codeActions)
}

// SetTextAlign aligns a single line (U:input/base/state.rs:628-635).
func (s *InputBaseState[M]) SetTextAlign(textAlign gpui.TextAlign, cx *inputCx[M]) {
	if !s.IsSingleLine() || s.textAlign == textAlign {
		return
	}
	s.textAlign = textAlign
	cx.Notify()
}

// ToggleMasked flips the password mask (U:input/base/state.rs:637-645).
func (s *InputBaseState[M]) ToggleMasked(_ *gpui.Window, cx *inputCx[M]) {
	s.masked = !s.masked
	cx.Notify()
}

// newInMode builds the engine; each mode's constructor sets its layout on
// top (U:input/base/state.rs:656-765).
func newInMode[M InputModeKind](w *gpui.Window, cx *inputCx[M]) InputBaseState[M] {
	hooks := hooksOf[M]()
	focusHandle := cx.App().FocusHandle().TabStop(true)
	cursor := gpui.NewEntity(cx, cx.Scope(), func(b *blinkCursor, _ *gpui.Context[blinkCursor]) {
		*b = newBlinkCursor()
	})

	// Key bindings can consume events before on_key_down. Observe input
	// before action dispatch so every keystroke resets the blink delay
	// (U:input/base/state.rs:663-673). The pinned cx.intercept_keystrokes
	// subscription drops with the entity; App.OnKeystrokeInterceptor is
	// cancelled on release.
	weakCursor := cursor.Downgrade()
	keystrokeSub := cx.App().OnKeystrokeInterceptor(func(_ *gpui.KeystrokeEvent, w *gpui.Window, app *gpui.App) {
		if focusHandle.IsFocused(w) {
			_ = weakCursor.TryUpdate(app, func(b *blinkCursor, bcx *gpui.Context[blinkCursor]) { b.pause(bcx) })
		}
	})
	cx.OnRelease(func(s *InputBaseState[M], _ *gpui.App) {
		if s.keystrokeSub != nil {
			s.keystrokeSub.Cancel()
		}
	})

	subscriptions := []gpui.Subscription{
		// Observe the blink cursor to repaint the view when it changes
		// (U:input/base/state.rs:674-675).
		cx.Observe(cursor, func(_ *InputBaseState[M], _ gpui.Entity[blinkCursor], cx *inputCx[M]) { cx.Notify() }),
		// Blink the cursor when the window is active, pause when it is not
		// (U:input/base/state.rs:676-686).
		cx.ObserveWindowActivation(w, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) {
			if windowIsActive(w) && s.focusHandle.IsFocused(w) {
				s.blinkCursor.Update(cx, func(b *blinkCursor, bcx *gpui.Context[blinkCursor]) { b.start(bcx) })
			}
		}),
		cx.OnFocus(focusHandle, w, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) { s.onFocus(w, cx) }),
		cx.OnBlur(focusHandle, w, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) { s.onBlur(w, cx) }),
	}

	auto := DefaultAutoScroll()
	return InputBaseState[M]{
		hooks:             hooks,
		extras:            hooks.newExtras(),
		focusHandle:       focusHandle,
		mode:              layoutModePlainText(),
		undoManager:       newUndoManager(),
		replaceable:       true,
		softWrap:          true,
		blinkCursor:       cursor,
		selections:        defaultSelections(),
		textAlign:         gpui.TextAlignLeft,
		scrollHandle:      gpui.NewScrollHandle(),
		enableContextMenu: true,
		emitEvents:        true,
		subscriptions:     subscriptions,
		keystrokeSub:      keystrokeSub,
		autoScroll:        &auto,
	}
}

// windowIsActive is Window::is_window_active (R:window.rs, the cached
// activation flag). The core keeps that flag unexported
// (gpui/helpers_focus.go isWindowActiveCached), so this asks the platform
// (Window.IsActive); a window without a platform answer (the test window)
// counts as active, which is the cached flag's initial state.
func windowIsActive(w *gpui.Window) bool {
	active, err := w.IsActive()
	if err != nil {
		return true
	}
	return active
}

// requireInputMode panics when a single-line-only method is reached on
// another mode; upstream it does not compile (see input_kind.go).
func (s *InputBaseState[M]) requireInputMode(method string) {
	if _, ok := any(modeOf[M]()).(InputMode); !ok {
		panic("base: " + method + " is only available on InputState")
	}
}

// NewInputState creates a single-line text input state
// (U:input/base/state.rs:9722-9727 InputBaseState::<InputMode>::new). Call
// it from the entity's build:
//
//	gpui.NewEntity(app, scope, func(s *base.InputState, cx *gpui.Context[base.InputState]) {
//		*s = base.NewInputState(w, cx).Placeholder("Name")
//	})
func NewInputState(w *gpui.Window, cx *gpui.Context[InputState]) InputState {
	return newInMode[InputMode](w, cx)
}

// ContextMenu sets whether the right-click context menu is enabled
// (U:input/base/state.rs:767-776).
func (s InputBaseState[M]) ContextMenu(enable bool) InputBaseState[M] {
	s.enableContextMenu = enable
	return s
}

// SetContextMenuEnabled is U:input/base/state.rs:778-780.
func (s *InputBaseState[M]) SetContextMenuEnabled(enabled bool) { s.enableContextMenu = enabled }

// Replaceable sets whether search UI allows replacement
// (U:input/base/state.rs:782-787, doc(hidden)).
func (s InputBaseState[M]) Replaceable(allow bool) InputBaseState[M] {
	s.replaceable = allow
	return s
}

// Placeholder sets the placeholder (U:input/base/state.rs:789-793).
func (s InputBaseState[M]) Placeholder(placeholder string) InputBaseState[M] {
	s.placeholder = placeholder
	return s
}

// resetHighlighter drops the code editor's highlighter
// (U:input/base/state.rs:815-823); the highlighter joins with the editor
// ticket, so only the notify remains.
func (s *InputBaseState[M]) resetHighlighter(cx *inputCx[M]) { cx.Notify() }

// SetEditorStyle projects the editor style (U:input/base/state.rs:842-845).
func (s *InputBaseState[M]) SetEditorStyle(style InputEditorStyle) {
	s.editorStyle = style
	s.projectedEditorStyle = style
}

// SetEditorPaddings sets the multi-line presentation padding
// (U:input/base/state.rs:847-851, doc(hidden)).
func (s *InputBaseState[M]) SetEditorPaddings(paddings gpui.Edges) { s.editorPaddings = paddings }

// SetPlaceholder is U:input/base/state.rs:874-883.
func (s *InputBaseState[M]) SetPlaceholder(placeholder string, _ *gpui.Window, cx *inputCx[M]) {
	s.placeholder = placeholder
	cx.Notify()
}

// lineAndPositionForOffset finds the visible line and sub-line of offset
// with its position (U:input/base/state.rs:885-914).
func (s *InputBaseState[M]) lineAndPositionForOffset(offset int) (int, int, gpui.Point, bool) {
	layout := s.lastLayout
	if layout == nil {
		return 0, 0, gpui.Point{}, false
	}
	yOffset := layout.visibleTop
	for vi, line := range layout.lines {
		local := max(offset-layout.visibleLineByteOffsets[vi], 0)
		if pos, ok := line.positionForIndex(local, layout, false); ok {
			subLine := int(pos.Y / layout.lineHeight)
			return vi, subLine, gpui.Point{X: pos.X + layout.lineNumberWidth, Y: pos.Y + yOffset}, true
		}
		yOffset += line.size(layout.lineHeight).Height
	}
	return 0, 0, gpui.Point{}, false
}

// SetValue sets the value: history is cleared and no change event is
// emitted. A single-line caret goes to the end while the view scrolls
// back to the start (U:input/base/state.rs:916-945). The value is plain
// text; restoring inline tokens (InputContent) joins with the
// inline-tokens port.
func (s *InputBaseState[M]) SetValue(value string, w *gpui.Window, cx *inputCx[M]) {
	s.undoManager.setIgnoring(true)
	s.emitEvents = false
	s.replaceText(value, w, cx)
	s.undoManager.setIgnoring(false)
	s.emitEvents = true

	s.resetSelection()
	s.resetLSPState()
	s.resetScrollToStart()

	s.undoManager.clear()
	cx.Notify()
}

// ReplaceAll replaces the whole text, recording it for undo
// (U:input/base/state.rs:947-968).
func (s *InputBaseState[M]) ReplaceAll(text string, w *gpui.Window, cx *inputCx[M]) {
	s.replaceText(text, w, cx)
	s.resetSelection()
	s.resetLSPState()
	s.resetScrollToStart()
	cx.Notify()
}

// withEditsAllowed runs f with the user-facing edit restrictions lifted
// (U:input/base/state.rs:970-979).
func (s *InputBaseState[M]) withEditsAllowed(f func()) {
	wasDisabled, wasReadonly := s.disabled, s.readonly
	s.disabled, s.readonly = false, false
	f()
	s.disabled, s.readonly = wasDisabled, wasReadonly
}

// Insert inserts text at the cursor and moves the cursor to its end
// (U:input/base/state.rs:981-998).
func (s *InputBaseState[M]) Insert(text string, w *gpui.Window, cx *inputCx[M]) {
	s.withEditsAllowed(func() {
		s.undoManager.setPendingIntent(editIntentAtomic)
		cursor := s.Cursor()
		rng := s.rangeToUTF16(NewInputSelection(cursor, cursor))
		s.replaceTextInRangeSilent(&rng, text, w, cx)
		s.setCursorTo(s.activeSelection().end)
	})
}

// Replace replaces the selection and moves the cursor to the end of the
// replacement (U:input/base/state.rs:1000-1016).
func (s *InputBaseState[M]) Replace(text string, w *gpui.Window, cx *inputCx[M]) {
	s.withEditsAllowed(func() {
		s.undoManager.setPendingIntent(editIntentAtomic)
		s.replaceTextInRangeSilent(nil, text, w, cx)
		s.setCursorTo(s.activeSelection().end)
	})
}

// replaceText replaces the whole text (U:input/base/state.rs:1018-1031).
func (s *InputBaseState[M]) replaceText(text string, w *gpui.Window, cx *inputCx[M]) {
	s.withEditsAllowed(func() {
		s.undoManager.setPendingIntent(editIntentAtomic)
		rng := gpui.UTF16Range{Start: 0, End: s.text.LenUTF16()}
		s.replaceTextInRangeSilent(&rng, text, w, cx)
		s.resetHighlighter(cx)
	})
}

// resetSelection puts a single-line caret at the end (like HTML <input>)
// and clears a multi-line selection (U:input/base/state.rs:1033-1045).
func (s *InputBaseState[M]) resetSelection() {
	s.selections.removeAllButActive()
	if s.IsSingleLine() {
		s.setCursorTo(s.text.Len())
	} else {
		s.selections.activeMut().clear()
	}
}

// resetLSPState is U:input/base/state.rs:1047-1052.
func (s *InputBaseState[M]) resetLSPState() {
	if s.IsCodeEditor() {
		s.pendingUpdate = true
		s.hooks.resetLanguageFeatures(s)
	}
}

// resetScrollToStart scrolls to the start; a single line also defers the
// start for the next paint, overriding the cursor follow
// (U:input/base/state.rs:1054-1062).
func (s *InputBaseState[M]) resetScrollToStart() {
	s.scrollHandle.SetOffset(gpui.Point{})
	if s.IsSingleLine() {
		s.deferredScrollOffset = &gpui.Point{}
	}
}

// Disabled sets the disabled mode (U:input/base/state.rs:1064-1071).
func (s InputBaseState[M]) Disabled(disabled bool) InputBaseState[M] {
	s.disabled = disabled
	return s
}

// SetDisabled is U:input/base/state.rs:1073-1084.
func (s *InputBaseState[M]) SetDisabled(disabled bool, cx *inputCx[M]) {
	if s.disabled == disabled {
		return
	}
	s.disabled = disabled
	if disabled {
		s.hooks.hideContextMenu(s, cx)
		s.hooks.clearInlineCompletion(s, cx)
	}
	cx.Notify()
}

// Readonly sets the read-only mode: the input keeps its appearance, focus,
// caret, selection and copy, and rejects user edits
// (U:input/base/state.rs:1086-1097).
func (s InputBaseState[M]) Readonly(readonly bool) InputBaseState[M] {
	s.readonly = readonly
	return s
}

// SetReadonly is U:input/base/state.rs:1099-1111 (the search session's
// replace mode joins with the search port).
func (s *InputBaseState[M]) SetReadonly(readonly bool, cx *inputCx[M]) {
	if s.readonly == readonly {
		return
	}
	s.readonly = readonly
	if readonly {
		s.hooks.hideContextMenu(s, cx)
		s.hooks.clearInlineCompletion(s, cx)
	}
	cx.Notify()
}

// IsEditable reports whether the user may change the text; programmatic
// APIs are not limited by it (U:input/base/state.rs:1113-1119).
func (s *InputBaseState[M]) IsEditable() bool { return !s.disabled && !s.readonly }

// CleanOnEscape clears the input on Escape (U:input/base/state.rs:1121-1125).
func (s InputBaseState[M]) CleanOnEscape() InputBaseState[M] {
	s.cleanOnEscape = true
	return s
}

// SetCleanOnEscape is U:input/base/state.rs:1127-1129.
func (s *InputBaseState[M]) SetCleanOnEscape(clean bool) { s.cleanOnEscape = clean }

// SubmitOnEnter treats a plain Enter as submit in multi-line mode
// (U:input/base/state.rs:1131-1139, doc(hidden)).
func (s InputBaseState[M]) SubmitOnEnter(submit bool) InputBaseState[M] {
	s.submitOnEnter = submit
	return s
}

// SetSubmitOnEnter is U:input/base/state.rs:1141-1144.
func (s *InputBaseState[M]) SetSubmitOnEnter(submit bool, cx *inputCx[M]) {
	s.submitOnEnter = submit
	cx.Notify()
}

// ShowWhitespaces shows whitespace markers (U:input/base/state.rs:1146-1151).
func (s InputBaseState[M]) ShowWhitespaces(show bool) InputBaseState[M] {
	s.showWhitespaces = show
	return s
}

// SetShowWhitespaces is U:input/base/state.rs:1153-1157.
func (s *InputBaseState[M]) SetShowWhitespaces(show bool, _ *gpui.Window, cx *inputCx[M]) {
	s.showWhitespaces = show
	cx.Notify()
}

// ScrollBeyondLastLine sets the empty rows below the last line, code
// editor only; nil is half the viewport (U:input/base/state.rs:1159-1171).
func (s InputBaseState[M]) ScrollBeyondLastLine(rows *int) InputBaseState[M] {
	s.scrollBeyondLastLine = rows
	return s
}

// SetScrollBeyondLastLine is U:input/base/state.rs:1173-1185.
func (s *InputBaseState[M]) SetScrollBeyondLastLine(rows *int, _ *gpui.Window, cx *inputCx[M]) {
	if equalIntPtr(s.scrollBeyondLastLine, rows) {
		return
	}
	s.scrollBeyondLastLine = rows
	cx.Notify()
}

// CursorSurroundingLines sets the lines kept between the cursor and the
// viewport edge (U:input/base/state.rs:1187-1200).
func (s InputBaseState[M]) CursorSurroundingLines(lines *int) InputBaseState[M] {
	s.cursorSurroundingLines = lines
	return s
}

// SetCursorSurroundingLines is U:input/base/state.rs:1202-1214.
func (s *InputBaseState[M]) SetCursorSurroundingLines(lines *int, _ *gpui.Window, cx *inputCx[M]) {
	if equalIntPtr(s.cursorSurroundingLines, lines) {
		return
	}
	s.cursorSurroundingLines = lines
	cx.Notify()
}

func equalIntPtr(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// DefaultValue sets the default value (U:input/base/state.rs:1216-1227).
func (s InputBaseState[M]) DefaultValue(value string) InputBaseState[M] {
	s.text = rope.FromStr(s.normalizeInput(value))
	s.displayMap.setText(s.text)
	s.pendingUpdate = true
	return s
}

// Value returns the value as a string (U:input/base/state.rs:1229-1235).
func (s *InputBaseState[M]) Value() string { return s.text.String() }

// SelectedValue returns the active selection's text
// (U:input/base/state.rs:1237-1244).
func (s *InputBaseState[M]) SelectedValue() string { return s.SelectedText().String() }

// UnmaskValue returns the value without the mask
// (U:input/base/state.rs:1246-1249).
func (s *InputBaseState[M]) UnmaskValue() string { return s.maskPattern.Unmask(s.text.String()) }

// Prepare does nothing; it is kept so render paths keep compiling
// (U:input/base/state.rs:1251-1257, doc(hidden)).
func (s *InputBaseState[M]) Prepare(*gpui.Window, *inputCx[M]) {}

// Text returns the text rope (U:input/base/state.rs:1259-1265). A Rope is
// a value: the copy costs nothing and later edits do not affect it.
func (s *InputBaseState[M]) Text() rope.Rope { return s.text }

// CursorPosition returns the 0-based position of the cursor
// (U:input/base/state.rs:1267-1271).
func (s *InputBaseState[M]) CursorPosition() InputPosition {
	return s.ext().OffsetToPosition(s.Cursor())
}

// SetCursorPosition moves the cursor to a 0-based position and focuses
// the input (U:input/base/state.rs:1273-1288).
func (s *InputBaseState[M]) SetCursorPosition(position InputPosition, w *gpui.Window, cx *inputCx[M]) {
	offset := s.ext().PositionToOffset(position)
	s.moveTo(offset, nil, cx)
	s.updatePreferredColumn()
	s.Focus(w, cx)
}

// Focus focuses the input and starts the caret blink
// (U:input/base/state.rs:1290-1296).
func (s *InputBaseState[M]) Focus(w *gpui.Window, cx *inputCx[M]) {
	s.focusHandle.Focus(w)
	s.blinkCursor.Update(cx, func(b *blinkCursor, bcx *gpui.Context[blinkCursor]) { b.start(bcx) })
}

// Refresh requests a highlighter and LSP refresh on the next render
// (U:input/base/state.rs:1298-1314).
func (s *InputBaseState[M]) Refresh(cx *inputCx[M]) {
	s.pendingUpdate = true
	cx.Notify()
}

// previousStartOfWordAt is the start of the word before offset; a masked
// value has no word boundaries (U:input/base/state.rs:1446-1465).
func (s *InputBaseState[M]) previousStartOfWordAt(offset int) int {
	if s.masked {
		return 0
	}
	offset = s.offsetFromUTF16(s.offsetToUTF16(offset))
	leftPart := s.text.Slice(0, offset).String()
	target := 0
	for i, seg := range segment.SplitWordBoundIndices(leftPart) {
		if strings.TrimLeftFunc(seg, unicode.IsSpace) != "" {
			target = i
		}
	}
	return s.tokenBoundary(target, sumtree.Left)
}

// nextEndOfWordAt is the end of the word after offset
// (U:input/base/state.rs:1467-1482).
func (s *InputBaseState[M]) nextEndOfWordAt(offset int) int {
	if s.masked {
		return s.text.Len()
	}
	offset = s.offsetFromUTF16(s.offsetToUTF16(offset))
	rightPart := s.text.Slice(offset, s.text.Len()).String()
	target := s.text.Len()
	for i, seg := range segment.SplitWordBoundIndices(rightPart) {
		if strings.TrimLeftFunc(seg, unicode.IsSpace) != "" {
			target = offset + i + len(seg)
			break
		}
	}
	return s.tokenBoundary(target, sumtree.Right)
}

// startOfLineAt is the line start of offset; a single line starts at 0.
// The soft-wrap visual start of a code editor joins with the display map
// (U:input/base/state.rs:1484-1511).
func (s *InputBaseState[M]) startOfLineAt(offset int, _ bool) int {
	if s.IsSingleLine() {
		return 0
	}
	row := s.ext().OffsetToPoint(offset).Row
	return s.ext().LineStartOffset(row)
}

// endOfLineAt is the line end of offset (U:input/base/state.rs:1513-1544).
func (s *InputBaseState[M]) endOfLineAt(offset int, _ bool) int {
	if s.IsSingleLine() {
		return s.text.Len()
	}
	row := s.ext().OffsetToPoint(offset).Row
	return s.ext().LineEndOffset(row)
}

// Cursor returns the cursor's UTF-8 byte offset
// (U:input/base/state.rs:2873-2882).
func (s *InputBaseState[M]) Cursor() int {
	if s.imeMarkedRange != nil {
		return s.imeMarkedRange.end
	}
	return s.selections.active().cursorOffset()
}

// activeSelection is U:input/base/state.rs:2884-2887.
func (s *InputBaseState[M]) activeSelection() cursorSelection { return s.selections.active() }

// activeSelectionMut is U:input/base/state.rs:2889-2892.
func (s *InputBaseState[M]) activeSelectionMut() *cursorSelection { return s.selections.activeMut() }

// setSelection sets the active selection's range, keeping reversed and
// the column anchor (U:input/base/state.rs:2894-2902).
func (s *InputBaseState[M]) setSelection(start, end int) {
	rng := s.normalizeTokenRange(NewInputSelection(start, end))
	active := s.activeSelectionMut()
	active.start, active.end = rng.Start, rng.End
}

// setCursorTo collapses the active selection to offset
// (U:input/base/state.rs:2904-2912).
func (s *InputBaseState[M]) setCursorTo(offset int) {
	offset = s.tokenBoundary(offset, sumtree.Right)
	active := s.activeSelectionMut()
	active.start, active.end, active.reversed = offset, offset, false
}

// VisibleRowRange is the visible row range of the last layout
// (U:input/base/state.rs:2914-2917).
func (s *InputBaseState[M]) VisibleRowRange() (InputSelection, bool) {
	if s.lastLayout == nil {
		return InputSelection{}, false
	}
	return s.lastLayout.visibleRange, true
}

// ScrollOffset is the viewport's scroll offset (U:input/base/state.rs:2919-2922).
func (s *InputBaseState[M]) ScrollOffset() gpui.Point { return s.scrollHandle.Offset() }

// SetScrollOffset defers a clamped scroll offset to the next layout
// (U:input/base/state.rs:2924-2930).
func (s *InputBaseState[M]) SetScrollOffset(offset gpui.Point, cx *inputCx[M]) {
	s.deferredScrollOffset = &offset
	cx.Notify()
}

// LineHeight is the laid-out line height (U:input/base/state.rs:2932-2935).
func (s *InputBaseState[M]) LineHeight() (float32, bool) {
	if s.lastLayout == nil {
		return 0, false
	}
	return s.lastLayout.lineHeight, true
}

// SelectedRange is the active selection's byte range
// (U:input/base/state.rs:2937-2946).
func (s *InputBaseState[M]) SelectedRange() InputSelection {
	a := s.selections.active()
	return NewInputSelection(a.start, a.end)
}

// SelectAll selects the whole text (U:input/base/state.rs:2948-2953).
func (s *InputBaseState[M]) SelectAll(_ *gpui.Window, cx *inputCx[M]) {
	s.undoManager.breakTransactionCoalescing()
	s.selections.removeAllButActive()
	s.setSelection(0, s.text.Len())
	cx.Notify()
}

// SetSelectedRange sets the selection by UTF-8 offsets, removing extra
// cursors; a non-empty range expands to character boundaries
// (U:input/base/state.rs:2955-2973).
func (s *InputBaseState[M]) SetSelectedRange(rng InputSelection, cx *inputCx[M]) {
	rng = s.normalizeTokenRange(rng)
	endBias := sumtree.Right
	if rng.Start == rng.End {
		endBias = sumtree.Left
	}
	start := s.ext().ClipOffset(rng.Start, sumtree.Left)
	end := s.ext().ClipOffset(rng.End, endBias)

	s.moveTo(start, nil, cx)
	s.activeSelectionMut().reversed = false
	s.selectedWordRange = nil
	s.selectTo(end, cx)
}

// Unselect collapses the selection to the cursor
// (U:input/base/state.rs:3221-3227).
func (s *InputBaseState[M]) Unselect(_ *gpui.Window, cx *inputCx[M]) {
	s.undoManager.breakTransactionCoalescing()
	s.setCursorTo(s.Cursor())
	cx.Notify()
}

// offsetFromUTF16 is U:input/base/state.rs:3229-3232.
func (s *InputBaseState[M]) offsetFromUTF16(offset int) int {
	return s.ext().OffsetUTF16ToOffset(offset)
}

// offsetToUTF16 is U:input/base/state.rs:3234-3237.
func (s *InputBaseState[M]) offsetToUTF16(offset int) int { return s.ext().OffsetToOffsetUTF16(offset) }

// rangeToUTF16 is U:input/base/state.rs:3239-3242.
func (s *InputBaseState[M]) rangeToUTF16(rng InputSelection) gpui.UTF16Range {
	return gpui.UTF16Range{Start: s.offsetToUTF16(rng.Start), End: s.offsetToUTF16(rng.End)}
}

// rangeFromUTF16 is U:input/base/state.rs:3244-3247.
func (s *InputBaseState[M]) rangeFromUTF16(rng gpui.UTF16Range) InputSelection {
	return NewInputSelection(s.offsetFromUTF16(rng.Start), s.offsetFromUTF16(rng.End))
}

// cursorBoundary clips a cursor offset without splitting a CRLF
// (U:input/base/state.rs:3277-3293).
func (s *InputBaseState[M]) cursorBoundary(offset int, bias sumtree.Bias) int {
	offset = s.tokenBoundary(s.ext().ClipOffset(offset, bias), bias)
	if offset > 0 {
		prev, okPrev := s.ext().CharAt(offset - 1)
		next, okNext := s.ext().CharAt(offset)
		if okPrev && okNext && prev == '\r' && next == '\n' {
			if bias == sumtree.Left {
				return offset - 1
			}
			return offset + 1
		}
	}
	return offset
}

// previousBoundary is U:input/base/state.rs:3295-3298 (no folds: the
// visible clamp is the identity).
func (s *InputBaseState[M]) previousBoundary(offset int) int {
	return s.cursorBoundary(max(offset-1, 0), sumtree.Left)
}

// nextBoundary is U:input/base/state.rs:3300-3303.
func (s *InputBaseState[M]) nextBoundary(offset int) int {
	return s.cursorBoundary(offset+1, sumtree.Right)
}

// showCursor reports whether the caret paints: focused (or a context menu
// open), enabled, blinking visible and the window active
// (U:input/base/state.rs:3305-3311).
func (s *InputBaseState[M]) showCursor(w *gpui.Window, app *gpui.App) bool {
	visible := s.blinkCursor.Read(app, func(b *blinkCursor, _ *gpui.App) bool { return b.isVisible() })
	return (s.focusHandle.IsFocused(w) || s.hooks.isContextMenuOpen(s, app)) &&
		!s.disabled && visible && windowIsActive(w)
}

// onFocus is U:input/base/state.rs:3313-3318.
func (s *InputBaseState[M]) onFocus(_ *gpui.Window, cx *inputCx[M]) {
	s.blinkCursor.Update(cx, func(b *blinkCursor, bcx *gpui.Context[blinkCursor]) { b.start(bcx) })
	InputBaseStateEvents[M]().Emit(cx, InputEvent{Kind: InputEventFocus})
}

// onBlur ends the typing session, stops the blink and clamps a number
// (U:input/base/state.rs:3320-3341). The selection is kept: an app menu
// may want to copy it.
func (s *InputBaseState[M]) onBlur(w *gpui.Window, cx *inputCx[M]) {
	if s.hooks.isContextMenuOpen(s, cx.App()) {
		return
	}
	s.hooks.hideContextMenu(s, cx)
	s.undoManager.breakTransactionCoalescing()
	s.hooks.clearHoverState(s, cx)
	s.hooks.clearInlineCompletion(s, cx)
	s.dismissTouchSelection(cx)
	s.blinkCursor.Update(cx, func(b *blinkCursor, bcx *gpui.Context[blinkCursor]) { b.stop(bcx) })
	s.clampNumberValue(w, cx)
	InputBaseStateEvents[M]().Emit(cx, InputEvent{Kind: InputEventBlur})
	cx.Notify()
}

// clampNumberValue clamps a number to min/max on blur; out-of-range
// values are allowed while typing (U:input/base/state.rs:3343-3377). Rust
// f64 parse/Display are strconv.ParseFloat / FormatFloat('f', -1).
func (s *InputBaseState[M]) clampNumberValue(w *gpui.Window, cx *inputCx[M]) {
	if !s.IsSingleLine() || s.maskPattern.Kind != MaskPatternKindNumber {
		return
	}
	if s.numberMin == nil && s.numberMax == nil {
		return
	}
	value, err := strconv.ParseFloat(s.UnmaskValue(), 64)
	if err != nil {
		return
	}
	var clamped float64
	switch {
	case s.numberMin != nil && value < *s.numberMin:
		clamped = *s.numberMin
	case s.numberMax != nil && value > *s.numberMax:
		clamped = *s.numberMax
	default:
		return
	}
	// The clamped value must pass the pattern/validate check.
	newText := strconv.FormatFloat(clamped, 'f', -1, 64)
	if !s.isValidInput(newText, cx) {
		return
	}
	rng := s.rangeToUTF16(NewInputSelection(0, s.text.Len()))
	s.replaceTextInRangeSilent(&rng, newText, w, cx)
}

// pauseBlinkCursor is U:input/base/state.rs:3379-3383.
func (s *InputBaseState[M]) pauseBlinkCursor(cx *inputCx[M]) {
	s.blinkCursor.Update(cx, func(b *blinkCursor, bcx *gpui.Context[blinkCursor]) { b.pause(bcx) })
}

// normalizeInput normalizes inserted text: full-width number characters
// for a number mask, and no line breaks in a single line
// (U:input/base/state.rs:3438-3455).
func (s *InputBaseState[M]) normalizeInput(newText string) string {
	normalized := newText
	if s.maskPattern.Kind == MaskPatternKindNumber {
		normalized = normalizeNumberInput(newText)
	}
	if s.IsSingleLine() && strings.ContainsAny(normalized, "\n\r") {
		normalized = strings.NewReplacer("\n", "", "\r", "").Replace(normalized)
	}
	return normalized
}

// isValidInput checks validate, the mask and the pattern
// (U:input/base/state.rs:3457-3477). The regex crate's is_match is Go
// regexp's MatchString (both RE2-class).
func (s *InputBaseState[M]) isValidInput(newText string, cx *inputCx[M]) bool {
	if newText == "" {
		return true
	}
	if s.validate != nil && !s.validate(newText, cx.App()) {
		return false
	}
	if !s.maskPattern.IsValid(newText) {
		return false
	}
	if s.pattern == nil {
		return true
	}
	return s.pattern.MatchString(newText)
}

// MaskPattern sets the mask pattern: `9` digit, `A` letter, `*` any, other
// characters literal (U:input/base/state.rs:3479-3495).
func (s InputBaseState[M]) MaskPattern(pattern MaskPattern) InputBaseState[M] {
	s.maskPattern = pattern
	s.maskPatternSet = true
	if placeholder, ok := s.maskPattern.Placeholder(); ok {
		s.placeholder = placeholder
	}
	return s
}

// SetMaskPattern is U:input/base/state.rs:3497-3509.
func (s *InputBaseState[M]) SetMaskPattern(pattern MaskPattern, _ *gpui.Window, cx *inputCx[M]) {
	s.maskPattern = pattern
	s.maskPatternSet = true
	if placeholder, ok := s.maskPattern.Placeholder(); ok {
		s.placeholder = placeholder
	}
	cx.Notify()
}

// EnsureNumberMask applies the default numeric mask unless one was chosen
// (U:input/base/state.rs:3511-3520).
func (s *InputBaseState[M]) EnsureNumberMask() {
	if s.maskPatternSet {
		return
	}
	s.maskPattern = MaskPattern{Kind: MaskPatternKindNumber}
}

// setInputBounds records the container bounds
// (U:input/base/state.rs:3522-3543). The stand-in display map does not
// wrap, so a width change has no wrap width to forward.
func (s *InputBaseState[M]) setInputBounds(newBounds gpui.Bounds, cx *inputCx[M]) {
	widthChanged := s.inputBounds.Size.Width != newBounds.Size.Width
	s.inputBounds = newBounds
	if s.lastLayout != nil && widthChanged {
		if s.IsMultiLine() {
			s.mode.updateAutoGrow(s.displayMap.wrapRowCount())
		}
		cx.Notify()
	}
}

// SelectedText is the active selection's text
// (U:input/base/state.rs:3545-3553).
func (s *InputBaseState[M]) SelectedText() rope.RopeSlice {
	rng := s.rangeFromUTF16(s.rangeToUTF16(s.SelectedRange()))
	return s.text.Slice(rng.Start, rng.End)
}

// RangeToBounds is the rendered bounds of a UTF-8 range, when laid out
// (U:input/base/state.rs:3555-3581).
func (s *InputBaseState[M]) RangeToBounds(rng InputSelection) (gpui.Bounds, bool) {
	if s.lastLayout == nil || s.lastBounds == nil {
		return gpui.Bounds{}, false
	}
	_, _, startPos, ok1 := s.lineAndPositionForOffset(rng.Start)
	_, _, endPos, ok2 := s.lineAndPositionForOffset(rng.End)
	if !ok1 || !ok2 {
		return gpui.Bounds{}, false
	}
	origin := s.lastBounds.Origin
	return boundsFromCorners(
		gpui.Point{X: origin.X + startPos.X, Y: origin.Y + startPos.Y},
		gpui.Point{X: origin.X + endPos.X, Y: origin.Y + endPos.Y + s.lastLayout.lineHeight},
	), true
}

// boundsFromCorners is Bounds::from_corners.
func boundsFromCorners(a, b gpui.Point) gpui.Bounds {
	return gpui.Bounds{Origin: a, Size: gpui.Size{Width: b.X - a.X, Height: b.Y - a.Y}}
}

// FocusHandle is `impl Focusable` (U:input/base/state.rs:4348-4352).
func (s *InputBaseState[M]) FocusHandle(*gpui.App) gpui.FocusHandle { return s.focusHandle }

// ---------------------------------------------------------------------------
// InputMode-only methods (U:input/base/state.rs:9722-9852)
// ---------------------------------------------------------------------------

// Masked sets the password mask; InputState only
// (U:input/base/state.rs:9748-9752).
func (s InputBaseState[M]) Masked(masked bool) InputBaseState[M] {
	s.requireInputMode("Masked")
	s.masked = masked
	return s
}

// SetMasked is U:input/base/state.rs:9754-9758; InputState only.
func (s *InputBaseState[M]) SetMasked(masked bool, _ *gpui.Window, cx *inputCx[M]) {
	s.requireInputMode("SetMasked")
	s.masked = masked
	cx.Notify()
}

// Pattern sets the regular-expression pattern; InputState only
// (U:input/base/state.rs:9760-9764).
func (s InputBaseState[M]) Pattern(pattern *regexp.Regexp) InputBaseState[M] {
	s.requireInputMode("Pattern")
	s.pattern = pattern
	return s
}

// SetPattern is U:input/base/state.rs:9766-9774; InputState only.
func (s *InputBaseState[M]) SetPattern(pattern *regexp.Regexp, _ *gpui.Window, _ *inputCx[M]) {
	s.requireInputMode("SetPattern")
	s.pattern = pattern
}

// Validate sets the validation function; InputState only
// (U:input/base/state.rs:9776-9780).
func (s InputBaseState[M]) Validate(f func(text string, app *gpui.App) bool) InputBaseState[M] {
	s.requireInputMode("Validate")
	s.validate = f
	return s
}

// SetValidator is U:input/base/state.rs:9782-9788; InputState only.
func (s *InputBaseState[M]) SetValidator(f func(text string, app *gpui.App) bool, _ *inputCx[M]) {
	s.requireInputMode("SetValidator")
	s.validate = f
}

// Min sets the number minimum, clamped on blur; InputState only
// (U:input/base/state.rs:9804-9812).
func (s InputBaseState[M]) Min(value float64) InputBaseState[M] {
	s.requireInputMode("Min")
	s.numberMin = &value
	return s
}

// Max sets the number maximum, clamped on blur; InputState only
// (U:input/base/state.rs:9814-9822).
func (s InputBaseState[M]) Max(value float64) InputBaseState[M] {
	s.requireInputMode("Max")
	s.numberMax = &value
	return s
}

// SetMin is U:input/base/state.rs:9837-9840; InputState only.
func (s *InputBaseState[M]) SetMin(value *float64, _ *gpui.Window, _ *inputCx[M]) {
	s.requireInputMode("SetMin")
	s.numberMin = value
}

// SetMax is U:input/base/state.rs:9842-9845; InputState only.
func (s *InputBaseState[M]) SetMax(value *float64, _ *gpui.Window, _ *inputCx[M]) {
	s.requireInputMode("SetMax")
	s.numberMax = value
}

// SetLoading shows a spinner; InputState only
// (U:input/base/state.rs:9847-9851).
func (s *InputBaseState[M]) SetLoading(loading bool, _ *gpui.Window, cx *inputCx[M]) {
	s.requireInputMode("SetLoading")
	s.loading = loading
	cx.Notify()
}

// runeLenOr1 is the UTF-8 length of the rune at the start of s (1 for an
// invalid byte).
func runeLenOr1(s string) int {
	_, n := utf8.DecodeRuneInString(s)
	return max(n, 1)
}
