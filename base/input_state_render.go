package base

// Render of the input engine (U:input/base/state.rs:4354-4512): action
// and pointer listener registration around the TextElement and the
// editor scrollbar.

import "github.com/hoangvu12/gpui-go/gpui"

// Render implements gpui.Render (U:input/base/state.rs:4355-4511).
func (s *InputBaseState[M]) Render(w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) gpui.AnyElement {
	// Before anything reads it: the element resolves this style during
	// layout and paint, both after this call in the same frame. An unset
	// selection resolves from the system record (browser-core 28).
	tokens := ThemeGlobal(cx.App()).Tokens
	s.editorStyle = s.projectedEditorStyle.Resolved(cx.App(), &tokens)
	// The listeners and the element outlive this callback: the window-owned
	// self-handle (Context.StrongSelf) is the pinned cx.entity().
	entity := cx.StrongSelf()
	if s.pendingUpdate {
		s.updateHighlighter(highlighterUpdate{oldText: s.text, newText: s.text}, w, cx)
		s.updateFoldCandidates()
		s.hooks.refreshLanguageFeatures(s, w, cx)
		s.pendingUpdate = false
	}

	type self = InputBaseState[M]
	d := gpui.Div().Block().
		ID("input-state").
		KeyContext(inputContext).
		TrackFocus(s.focusHandle)
	if s.IsEditable() {
		d.OnAction(BackspaceAction, w.ListenerFor(entity, (*self).backspace)).
			OnAction(DeleteAction, w.ListenerFor(entity, (*self).delete)).
			OnAction(DeleteToBeginningOfLineAction, w.ListenerFor(entity, (*self).deleteToBeginningOfLine)).
			OnAction(DeleteToEndOfLineAction, w.ListenerFor(entity, (*self).deleteToEndOfLine)).
			OnAction(DeleteToPreviousWordStartAction, w.ListenerFor(entity, (*self).deletePreviousWord)).
			OnAction(DeleteToNextWordEndAction, w.ListenerFor(entity, (*self).deleteNextWord)).
			OnAction(EnterAction, w.ListenerFor(entity, (*self).enter)).
			OnAction(EscapeAction, w.ListenerFor(entity, (*self).escape)).
			OnAction(PasteAction, w.ListenerFor(entity, (*self).paste)).
			OnAction(CutAction, w.ListenerFor(entity, (*self).cut)).
			OnAction(UndoAction, w.ListenerFor(entity, (*self).undo)).
			OnAction(RedoAction, w.ListenerFor(entity, (*self).redo))
		if s.IsMultiLine() {
			d.OnAction(IndentInlineAction, w.ListenerFor(entity, (*self).indentInline)).
				OnAction(OutdentInlineAction, w.ListenerFor(entity, (*self).outdentInline)).
				OnAction(IndentAction, w.ListenerFor(entity, (*self).indentBlock)).
				OnAction(OutdentAction, w.ListenerFor(entity, (*self).outdentBlock))
		}
	}
	d.OnAction(MoveLeftAction, w.ListenerFor(entity, (*self).left)).
		OnAction(MoveRightAction, w.ListenerFor(entity, (*self).right)).
		OnAction(SelectLeftAction, w.ListenerFor(entity, (*self).selectLeft)).
		OnAction(SelectRightAction, w.ListenerFor(entity, (*self).selectRight))
	if s.IsMultiLine() {
		d.OnAction(MoveUpAction, w.ListenerFor(entity, (*self).up)).
			OnAction(MoveDownAction, w.ListenerFor(entity, (*self).down)).
			OnAction(SelectUpAction, w.ListenerFor(entity, (*self).selectUp)).
			OnAction(SelectDownAction, w.ListenerFor(entity, (*self).selectDown)).
			OnAction(MovePageUpAction, w.ListenerFor(entity, (*self).pageUp)).
			OnAction(MovePageDownAction, w.ListenerFor(entity, (*self).pageDown)).
			OnAction(AddCursorAboveAction, w.ListenerFor(entity, (*self).addCursorAbove)).
			OnAction(AddCursorBelowAction, w.ListenerFor(entity, (*self).addCursorBelow))
	}
	d.OnAction(SelectAllAction, w.ListenerFor(entity, (*self).onActionSelectAll)).
		OnAction(SelectToStartOfLineAction, w.ListenerFor(entity, (*self).selectToStartOfLine)).
		OnAction(SelectToEndOfLineAction, w.ListenerFor(entity, (*self).selectToEndOfLine)).
		OnAction(SelectToPreviousWordStartAction, w.ListenerFor(entity, (*self).selectToPreviousWord)).
		OnAction(SelectToNextWordEndAction, w.ListenerFor(entity, (*self).selectToNextWord)).
		OnAction(MoveHomeAction, w.ListenerFor(entity, (*self).home)).
		OnAction(MoveEndAction, w.ListenerFor(entity, (*self).end)).
		OnAction(MoveToStartAction, w.ListenerFor(entity, (*self).moveToStart)).
		OnAction(MoveToEndAction, w.ListenerFor(entity, (*self).moveToEnd)).
		OnAction(MoveToPreviousWordAction, w.ListenerFor(entity, (*self).moveToPreviousWord)).
		OnAction(MoveToNextWordAction, w.ListenerFor(entity, (*self).moveToNextWord)).
		OnAction(SelectToStartAction, w.ListenerFor(entity, (*self).selectToStart)).
		OnAction(SelectToEndAction, w.ListenerFor(entity, (*self).selectToEnd)).
		OnAction(ShowCharacterPaletteAction, w.ListenerFor(entity, (*self).showCharacterPalette)).
		// ActivateToken opens the token spanning exactly the selection
		// with a keyboard click (U:input/base/state.rs:4434-4458).
		OnAction(ActivateTokenAction, func(_ *ActivateToken, w *gpui.Window, app *gpui.App) {
			var listener InlineTokenClickListener
			var event InlineTokenClickEvent
			entity.Read(app, func(state *self, _ *gpui.App) struct{} {
				for _, span := range state.tokenSpans() {
					if span.Range() != state.SelectedRange() {
						continue
					}
					if bounds, ok := state.RangeToBounds(span.Range()); ok {
						click := gpui.ClickEvent{Kind: gpui.ClickEventKeyboard, Keyboard: gpui.KeyboardClickEvent{Bounds: bounds}}
						listener, event, _ = state.tokenActivation(span.Range().Start, bounds, click)
					}
					break
				}
				return struct{}{}
			})
			if listener != nil {
				listener(&event, w, app)
			}
		}).
		OnAction(CopyAction, w.ListenerFor(entity, (*self).copy)).
		OnAction(SearchAction, w.ListenerFor(entity, (*self).onActionSearch)).
		OnAction(ReplaceAction, w.ListenerFor(entity, (*self).onActionReplace)).
		OnMouseDown(gpui.MouseButtonLeft, w.ListenerFor(entity, (*self).onMouseDown)).
		OnMouseDown(gpui.MouseButtonRight, w.ListenerFor(entity, (*self).onMouseDown)).
		OnMouseUp(gpui.MouseButtonLeft, w.ListenerFor(entity, (*self).onMouseUp)).
		OnMouseUp(gpui.MouseButtonRight, w.ListenerFor(entity, (*self).onMouseUp)).
		OnMouseMove(w.ListenerFor(entity, (*self).onMouseMove))
	scrollWheel := w.ListenerFor(entity, (*self).onScrollWheel)
	d.OnScrollWheel(func(event *gpui.ScrollWheelEvent, _ gpui.DispatchPhase, _ *gpui.Hitbox, w *gpui.Window, app *gpui.App) {
		scrollWheel(event, w, app)
	})
	if s.IsMultiLine() && !s.disabled {
		d.OnModifiersChanged(cx.Listener(func(_ *self, _ *gpui.ModifiersChangedEvent, _ *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
			cx.Notify()
		}))
	}
	if !s.disabled {
		if s.IsMultiLine() && w.Modifiers().Alt {
			d.CursorCrosshair()
		} else {
			d.CursorText()
		}
	}
	d.Flex1().HFull()
	// A single line fills the frame and sits at its vertical center.
	if !s.IsMultiLine() {
		d.Flex().ItemsCenter()
	}
	d.FlexGrow1().OverflowXHidden()
	if s.IsMultiLine() {
		d.Pt(gpui.Px(s.editorPaddings.Top)).
			Pr(gpui.Px(s.editorPaddings.Right)).
			Pb(gpui.Px(s.editorPaddings.Bottom)).
			Pl(gpui.Px(s.editorPaddings.Left))
	}
	d.Child(newTextElement(entity).withPlaceholder(s.placeholder))
	if s.showsScrollbar() {
		d.Child(newEditorScrollbar(entity))
	}

	// Actions only one mode handles are registered by that mode.
	return s.hooks.registerActions(d, entity, w).IntoElement()
}
