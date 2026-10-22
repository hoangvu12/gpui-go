package base

// Port of U:questionnaire/control.rs: the answer control for one choice.

import "github.com/hoangvu12/gpui-go/gpui"

// QuestionnaireChoiceControl is the answer control for one choice, wired
// to the questionnaire's behavior (U:questionnaire/control.rs:9-22). A
// multiple-answer item hands back a Checkbox and a single-answer item a
// Radio; exactly one of the two fields is set. Each already carries its
// checked and disabled state, accessible name, position in set, focus
// handle, confirm key and change handler.
type QuestionnaireChoiceControl struct {
	Checkbox *Checkbox
	Radio    *Radio
}

// NewQuestionnaireChoiceControl builds the control; false when the item or
// the choice is not part of the schema (U:questionnaire/control.rs:24-120).
func NewQuestionnaireChoiceControl[I gpui.IntoElementID](state gpui.Entity[QuestionnaireState], item, value string, id I, app *gpui.App) (QuestionnaireChoiceControl, bool) {
	type snapshot struct {
		choice      QuestionnaireChoiceState
		multiple    bool
		label       string
		description string
		hasDesc     bool
		position    int
		total       int
		hasPosition bool
		focusHandle gpui.FocusHandle
		hasFocus    bool
		ok          bool
	}
	snap := state.Read(app, func(s *QuestionnaireState, _ *gpui.App) snapshot {
		choice, ok := s.ChoiceState(item, value)
		if !ok {
			return snapshot{}
		}
		itemState, ok := s.ItemState(item)
		if !ok {
			return snapshot{}
		}
		definition, ok := s.ChoiceDefinition(item, value)
		if !ok {
			return snapshot{}
		}
		out := snapshot{choice: choice, multiple: itemState.IsMultiple(), label: definition.AccessibilityLabel(), ok: true}
		out.description, out.hasDesc = definition.Description()
		out.position, out.total, out.hasPosition = s.ChoicePosition(item, value)
		out.focusHandle, out.hasFocus = s.ChoiceFocusHandle(item, value)
		return out
	})
	if !snap.ok {
		return QuestionnaireChoiceControl{}, false
	}
	selected := snap.choice.IsSelected()
	disabled := snap.choice.IsDisabled()

	// Enter confirms an answer that is already selected; an unselected
	// control keeps Enter for activation.
	confirm := func(event *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
		if selected &&
			!w.DefaultPrevented() &&
			!event.IsHeld &&
			event.Keystroke.Key == "enter" &&
			event.Keystroke.Modifiers.NumberOfModifiers() == 0 &&
			state.UpdateWith(app, func(s *QuestionnaireState, cx *gpui.Context[QuestionnaireState]) bool {
				return s.ConfirmCurrent(w, cx)
			}) {
			w.PreventDefault()
		}
	}
	activate := func(w *gpui.Window, app *gpui.App) {
		state.Update(app, func(s *QuestionnaireState, cx *gpui.Context[QuestionnaireState]) {
			_ = s.ActivateChoice(item, value, cx)
			s.FocusChoice(item, value, w)
		})
	}

	if snap.multiple {
		checkState := CheckboxStateUnchecked
		if selected {
			checkState = CheckboxStateChecked
		}
		c := NewCheckbox(id).State(checkState).Disabled(disabled).AccessibilityLabel(snap.label)
		if snap.hasDesc {
			c.Interactivity().AriaDescription(snap.description)
		}
		if snap.hasPosition {
			c.Interactivity().AriaPositionInSet(snap.position).AriaSizeOfSet(snap.total)
		}
		if snap.hasFocus {
			c.TrackFocus(snap.focusHandle)
		}
		c.CaptureKeyDown(confirm)
		c.OnChange(func(_ CheckboxState, _ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) { activate(w, app) })
		return QuestionnaireChoiceControl{Checkbox: c}, true
	}

	r := NewRadio(id).Checked(selected).Disabled(disabled).AccessibilityLabel(snap.label)
	if snap.hasDesc {
		r.Interactivity().AriaDescription(snap.description)
	}
	if snap.hasPosition {
		r.SetPosition(snap.position, snap.total)
	}
	if snap.hasFocus {
		r.TrackFocus(snap.focusHandle)
	}
	r.CaptureKeyDown(confirm)
	r.OnChange(func(_ bool, _ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) { activate(w, app) })
	return QuestionnaireChoiceControl{Radio: r}, true
}
