package base

// Port of U:questionnaire/keyboard.rs: the questionnaire key contract.

import "github.com/hoangvu12/gpui-go/gpui"

// QuestionnaireHandleKeyDown routes a key press to the questionnaire's
// behavior and prevents the default when it was consumed
// (U:questionnaire/keyboard.rs:5-90, handle_key_down). The skin installs
// it on the root: arrows move between answers and items, Enter confirms a
// filled answer, a bare letter or digit activates a shortcut.
func QuestionnaireHandleKeyDown(state gpui.Entity[QuestionnaireState], event *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
	if w.DefaultPrevented() || event.IsHeld || event.PreferCharacterInput || event.Keystroke.IsIMEInProgress() {
		return
	}

	modifiers := event.Keystroke.Modifiers
	key := event.Keystroke.Key
	read := func(f func(s *QuestionnaireState) bool) bool {
		return state.Read(app, func(s *QuestionnaireState, _ *gpui.App) bool { return f(s) })
	}
	update := func(f func(s *QuestionnaireState, cx *gpui.Context[QuestionnaireState]) bool) bool {
		return state.UpdateWith(app, f)
	}
	inputFocused := read(func(s *QuestionnaireState) bool { return s.IsCurrentInputFocused(w) })
	inputHasText := inputFocused && read(func(s *QuestionnaireState) bool { return s.CurrentInputHasText(app) })
	singleRadioFocused := read(func(s *QuestionnaireState) bool {
		item, ok := s.CurrentItem()
		if !ok {
			return false
		}
		itemState, ok := s.ItemState(item)
		if !ok || itemState.IsMultiple() {
			return false
		}
		_, focused := s.FocusedCurrentChoice(w)
		return focused
	})
	confirm := func() bool {
		return update(func(s *QuestionnaireState, cx *gpui.Context[QuestionnaireState]) bool { return s.ConfirmCurrent(w, cx) })
	}
	focusPrevious := func() bool {
		return update(func(s *QuestionnaireState, cx *gpui.Context[QuestionnaireState]) bool {
			return s.FocusPreviousAnswer(w, cx)
		})
	}
	focusNext := func() bool {
		return update(func(s *QuestionnaireState, cx *gpui.Context[QuestionnaireState]) bool {
			return s.FocusNextAnswer(w, cx)
		})
	}
	moveRadio := func(direction int) bool {
		return update(func(s *QuestionnaireState, cx *gpui.Context[QuestionnaireState]) bool {
			return s.MoveCurrentRadio(direction, w, cx)
		})
	}

	var handled bool
	switch {
	case key == "enter" && modifiers.Secondary() && modifiers.NumberOfModifiers() == 1:
		handled = confirm()
	case modifiers.NumberOfModifiers() != 0:
		handled = false
	case inputFocused:
		switch {
		case key == "enter" && questionnaireFocusedAnswerIsFilled(state, w, app):
			handled = confirm()
		case key == "up" && !inputHasText:
			handled = focusPrevious()
		case key == "down" && !inputHasText:
			handled = focusNext()
		}
	default:
		switch {
		case key == "up":
			handled = focusPrevious() || (singleRadioFocused && moveRadio(-1))
		case key == "down":
			handled = focusNext() || (singleRadioFocused && moveRadio(1))
		case key == "left" && singleRadioFocused:
			handled = moveRadio(-1)
		case key == "right" && singleRadioFocused:
			handled = moveRadio(1)
		case key == "left":
			handled = update(func(s *QuestionnaireState, cx *gpui.Context[QuestionnaireState]) bool { return s.GoPrevious(w, cx) })
		case key == "right" && read(func(s *QuestionnaireState) bool { return s.NavigationState().IsConfirmable() }):
			handled = update(func(s *QuestionnaireState, cx *gpui.Context[QuestionnaireState]) bool { return s.GoNext(w, cx) })
		case key == "right":
			handled = false
		case key == "enter" && questionnaireFocusedAnswerIsFilled(state, w, app):
			handled = confirm()
		case key == "enter":
			handled = false
		default:
			handled = update(func(s *QuestionnaireState, cx *gpui.Context[QuestionnaireState]) bool {
				return s.ActivateShortcut(key, w, cx)
			})
		}
	}

	if handled {
		w.PreventDefault()
	}
}

// questionnaireFocusedAnswerIsFilled reports whether the focused answer
// of the current item is filled (U:questionnaire/keyboard.rs:92-104).
func questionnaireFocusedAnswerIsFilled(state gpui.Entity[QuestionnaireState], w *gpui.Window, app *gpui.App) bool {
	return state.Read(app, func(s *QuestionnaireState, _ *gpui.App) bool {
		item, ok := s.CurrentItem()
		if !ok {
			return false
		}
		if s.IsCurrentInputFocused(w) {
			answer, ok := s.Answer(item)
			if !ok {
				return false
			}
			_, filled := answer.Freeform()
			return filled
		}
		value, ok := s.FocusedCurrentChoice(w)
		if !ok {
			return false
		}
		choice, ok := s.ChoiceState(item, value)
		return ok && choice.IsSelected()
	})
}
