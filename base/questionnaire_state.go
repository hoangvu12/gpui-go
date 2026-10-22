package base

// Port of U:questionnaire/state.rs: the headless questionnaire state
// machine (answers, validation, navigation, focus and shortcuts).
// Option<usize> indices are ints with -1 for None.

import (
	"slices"
	"strconv"
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
)

// questionnaireItemRuntime is one item's runtime state
// (U:questionnaire/state.rs:11-25).
type questionnaireItemRuntime struct {
	disabled           bool
	choiceDisabled     []bool
	inputDisabled      bool
	answer             QuestionnaireAnswer
	initialAnswer      QuestionnaireAnswer
	initialInputValue  *string
	skipped            bool
	validationAttempt  bool
	internalError      *QuestionnaireValidationError
	externalError      *QuestionnaireValidationError
	focusHandle        gpui.FocusHandle
	choiceFocusHandles []gpui.FocusHandle
	inputFocusHandle   *gpui.FocusHandle
}

// QuestionnaireState owns questionnaire answers, validation, navigation
// and focus state (U:questionnaire/state.rs:27-37).
type QuestionnaireState struct {
	items          []QuestionnaireItemDefinition
	runtime        []questionnaireItemRuntime
	current        int
	initialCurrent *string
	shortcutMode   *QuestionnaireShortcutMode
	complete       bool
	focusHandle    gpui.FocusHandle
}

// NewQuestionnaireState is QuestionnaireState::new
// (U:questionnaire/state.rs:40-118). It validates the schema, seeds each
// item's answer from its default choices and its input's text, and
// subscribes to every input's Change event.
func NewQuestionnaireState(items []QuestionnaireItemDefinition, cx *gpui.Context[QuestionnaireState]) (QuestionnaireState, error) {
	if err := questionnaireValidateSchema(items); err != nil {
		return QuestionnaireState{}, err
	}
	app := cx.App()
	runtime := make([]questionnaireItemRuntime, 0, len(items))
	for _, item := range items {
		var defaults []string
		for _, choice := range item.Choices() {
			if choice.IsDefaultSelected() && !choice.IsDisabled() {
				defaults = append(defaults, choice.Value())
			}
		}
		answer := NewQuestionnaireAnswer().WithChoices(defaults...)

		var initialInputValue *string
		var inputFocusHandle *gpui.FocusHandle
		input, hasInput := item.Input()
		if hasInput {
			state := input.State()
			disabled := item.IsDisabled() || input.IsDisabled()
			state.Update(app, func(s *InputState, icx *gpui.Context[InputState]) { s.SetDisabled(disabled, icx) })

			value := state.Read(app, func(s *InputState, _ *gpui.App) string { return s.Value() })
			initialInputValue = &value
			handle := state.Read(app, func(s *InputState, app *gpui.App) gpui.FocusHandle { return s.FocusHandle(app) })
			inputFocusHandle = &handle
			if strings.TrimSpace(value) != "" && !input.IsDisabled() {
				if !item.IsMultiple() {
					answer = NewQuestionnaireAnswer()
				}
				answer.freeform = &value
			}

			InputStateEvents.Subscribe(cx, state, func(this *QuestionnaireState, input gpui.Entity[InputState], event *InputEvent, cx *gpui.Context[QuestionnaireState]) {
				if event.Kind == InputEventChange {
					this.onInputChange(input.EntityID(), cx)
				}
			})
		}

		choiceDisabled := make([]bool, len(item.Choices()))
		choiceFocus := make([]gpui.FocusHandle, len(item.Choices()))
		for i, choice := range item.Choices() {
			choiceDisabled[i] = choice.IsDisabled()
			choiceFocus[i] = app.FocusHandle()
		}
		runtime = append(runtime, questionnaireItemRuntime{
			disabled:           item.IsDisabled(),
			choiceDisabled:     choiceDisabled,
			inputDisabled:      !hasInput || input.IsDisabled(),
			initialAnswer:      answer.clone(),
			initialInputValue:  initialInputValue,
			answer:             answer,
			focusHandle:        app.FocusHandle(),
			choiceFocusHandles: choiceFocus,
			inputFocusHandle:   inputFocusHandle,
		})
	}

	s := QuestionnaireState{
		items:       items,
		runtime:     runtime,
		current:     -1,
		focusHandle: app.FocusHandle(),
	}
	for ix := range runtime {
		if !runtime[ix].disabled {
			s.current = ix
			name := items[ix].Name()
			s.initialCurrent = &name
			break
		}
	}
	return s, nil
}

// questionnaireValidateSchema is validate_schema
// (U:questionnaire/state.rs:120-147).
func questionnaireValidateSchema(items []QuestionnaireItemDefinition) error {
	itemNames := map[string]bool{}
	for _, item := range items {
		if itemNames[item.Name()] {
			return QuestionnaireSchemaError{Kind: QuestionnaireDuplicateItem, Item: item.Name()}
		}
		itemNames[item.Name()] = true

		choices := map[string]bool{}
		defaults := 0
		for _, choice := range item.Choices() {
			if choices[choice.Value()] {
				return QuestionnaireSchemaError{Kind: QuestionnaireDuplicateChoice, Item: item.Name(), Choice: choice.Value()}
			}
			choices[choice.Value()] = true
			if choice.IsDefaultSelected() {
				defaults++
			}
		}
		if !item.IsMultiple() && defaults > 1 {
			return QuestionnaireSchemaError{Kind: QuestionnaireMultipleDefaultsForSingleItem, Item: item.Name()}
		}
	}
	return nil
}

// WithCurrentItem starts at the named item when it is enabled
// (U:questionnaire/state.rs:149-160).
func (s QuestionnaireState) WithCurrentItem(name string) (QuestionnaireState, error) {
	ix, err := s.itemIx(name)
	if err != nil {
		return s, err
	}
	if !s.runtime[ix].disabled {
		s.current = ix
		s.initialCurrent = &name
	}
	return s, nil
}

// WithShortcuts enables choice shortcuts (U:questionnaire/state.rs:162-165).
func (s QuestionnaireState) WithShortcuts(mode QuestionnaireShortcutMode) QuestionnaireState {
	s.shortcutMode = &mode
	return s
}

// CurrentItem returns the current item's name
// (U:questionnaire/state.rs:167-169).
func (s *QuestionnaireState) CurrentItem() (string, bool) {
	if s.current < 0 {
		return "", false
	}
	return s.items[s.current].Name(), true
}

// CurrentIx returns the current item's position among the enabled items
// (U:questionnaire/state.rs:171-174).
func (s *QuestionnaireState) CurrentIx() (int, bool) {
	if s.current < 0 {
		return 0, false
	}
	pos := slices.Index(s.enabledIndices(), s.current)
	return pos, pos >= 0
}

// Total returns the number of enabled items (U:questionnaire/state.rs:176-178).
func (s *QuestionnaireState) Total() int { return len(s.enabledIndices()) }

// Progress returns the one-based progress (U:questionnaire/state.rs:180-182).
func (s *QuestionnaireState) Progress() QuestionnaireProgressState {
	current := 0
	if ix, ok := s.CurrentIx(); ok {
		current = ix + 1
	}
	return QuestionnaireProgressState{current: current, total: s.Total()}
}

// ItemDefinition returns the named item's definition
// (U:questionnaire/state.rs:184-186).
func (s *QuestionnaireState) ItemDefinition(name string) (QuestionnaireItemDefinition, bool) {
	if ix := s.itemIxOpt(name); ix >= 0 {
		return s.items[ix], true
	}
	return QuestionnaireItemDefinition{}, false
}

// ChoiceDefinition returns a choice's definition
// (U:questionnaire/state.rs:188-197).
func (s *QuestionnaireState) ChoiceDefinition(item, value string) (QuestionnaireChoiceDefinition, bool) {
	definition, ok := s.ItemDefinition(item)
	if !ok {
		return QuestionnaireChoiceDefinition{}, false
	}
	for _, choice := range definition.Choices() {
		if choice.Value() == value {
			return choice, true
		}
	}
	return QuestionnaireChoiceDefinition{}, false
}

// ItemState returns an item's derived state (U:questionnaire/state.rs:199-211).
func (s *QuestionnaireState) ItemState(name string) (QuestionnaireItemState, bool) {
	ix := s.itemIxOpt(name)
	if ix < 0 {
		return QuestionnaireItemState{}, false
	}
	definition := s.items[ix]
	return QuestionnaireItemState{
		name:     definition.Name(),
		status:   s.status(ix),
		required: definition.IsRequired(),
		multiple: definition.IsMultiple(),
		disabled: s.runtime[ix].disabled,
		invalid:  s.errorAt(ix) != nil,
		hasInput: definition.input != nil,
	}, true
}

// ChoiceState returns a choice's derived state
// (U:questionnaire/state.rs:213-225).
func (s *QuestionnaireState) ChoiceState(item, value string) (QuestionnaireChoiceState, bool) {
	itemIx := s.itemIxOpt(item)
	if itemIx < 0 {
		return QuestionnaireChoiceState{}, false
	}
	choiceIx := s.choiceIxOpt(itemIx, value)
	if choiceIx < 0 {
		return QuestionnaireChoiceState{}, false
	}
	runtime := &s.runtime[itemIx]
	definition := s.items[itemIx].Choices()[choiceIx]
	var shortcut *string
	if sc, ok := s.ShortcutForChoice(item, value); ok {
		shortcut = &sc
	}
	return QuestionnaireChoiceState{
		value:    definition.Value(),
		selected: slices.Contains(runtime.answer.choices, definition.Value()),
		disabled: runtime.disabled || runtime.choiceDisabled[choiceIx],
		invalid:  s.errorAt(itemIx) != nil,
		shortcut: shortcut,
	}, true
}

// ChoicePosition returns the one-based position of a choice among its
// item's enabled choices, with the enabled total; assistive technology
// announces the pair (U:questionnaire/state.rs:227-243).
func (s *QuestionnaireState) ChoicePosition(item, value string) (position, total int, ok bool) {
	definition, found := s.ItemDefinition(item)
	if !found {
		return 0, 0, false
	}
	var enabled []string
	for _, choice := range definition.Choices() {
		if state, ok := s.ChoiceState(item, choice.Value()); ok && !state.IsDisabled() {
			enabled = append(enabled, choice.Value())
		}
	}
	pos := slices.Index(enabled, value)
	if pos < 0 {
		return 0, 0, false
	}
	return pos + 1, len(enabled), true
}

// NavigationState returns the navigation affordances
// (U:questionnaire/state.rs:245-260).
func (s *QuestionnaireState) NavigationState() QuestionnaireNavigationState {
	ix, ok := s.CurrentIx()
	if !ok {
		return QuestionnaireNavigationState{}
	}
	total := s.Total()
	itemIx := s.current
	return QuestionnaireNavigationState{
		previousVisible: ix > 0,
		nextVisible:     ix+1 < total,
		skipVisible:     !s.items[itemIx].IsRequired(),
		submitVisible:   ix+1 == total,
		confirmable:     s.status(itemIx) != QuestionnaireItemUnanswered,
	}
}

// Answer returns an item's effective answer (U:questionnaire/state.rs:262-265).
func (s *QuestionnaireState) Answer(name string) (QuestionnaireAnswer, bool) {
	ix := s.itemIxOpt(name)
	if ix < 0 {
		return QuestionnaireAnswer{}, false
	}
	return s.effectiveAnswer(ix), true
}

// Answers returns the enabled items' answers (U:questionnaire/state.rs:267-273).
func (s *QuestionnaireState) Answers() QuestionnaireAnswers {
	var entries []QuestionnaireAnswerEntry
	for _, ix := range s.enabledIndices() {
		entries = append(entries, QuestionnaireAnswerEntry{Name: s.items[ix].Name(), Answer: s.effectiveAnswer(ix)})
	}
	return QuestionnaireAnswers{entries: entries}
}

// Error returns the active validation failure for an item; Base reports
// the reason, the presentation layer turns it into text
// (U:questionnaire/state.rs:275-279).
func (s *QuestionnaireState) Error(name string) (QuestionnaireValidationError, bool) {
	ix := s.itemIxOpt(name)
	if ix < 0 {
		return QuestionnaireValidationError{}, false
	}
	if e := s.errorAt(ix); e != nil {
		return *e, true
	}
	return QuestionnaireValidationError{}, false
}

// IsComplete reports whether the questionnaire completed
// (U:questionnaire/state.rs:281-283).
func (s *QuestionnaireState) IsComplete() bool { return s.complete }

// InputState returns an item's input state (U:questionnaire/state.rs:285-289).
func (s *QuestionnaireState) InputState(name string) (gpui.Entity[InputState], bool) {
	definition, ok := s.ItemDefinition(name)
	if !ok || definition.input == nil {
		return gpui.Entity[InputState]{}, false
	}
	return definition.input.State(), true
}

// FocusHandle returns the questionnaire's focus handle
// (U:questionnaire/state.rs:291-293).
func (s *QuestionnaireState) FocusHandle() gpui.FocusHandle { return s.focusHandle }

// ItemFocusHandle returns an item's focus handle
// (U:questionnaire/state.rs:295-298).
func (s *QuestionnaireState) ItemFocusHandle(name string) (gpui.FocusHandle, bool) {
	ix := s.itemIxOpt(name)
	if ix < 0 {
		return gpui.FocusHandle{}, false
	}
	return s.runtime[ix].focusHandle, true
}

// ChoiceFocusHandle returns a choice's focus handle
// (U:questionnaire/state.rs:300-304).
func (s *QuestionnaireState) ChoiceFocusHandle(item, value string) (gpui.FocusHandle, bool) {
	itemIx := s.itemIxOpt(item)
	if itemIx < 0 {
		return gpui.FocusHandle{}, false
	}
	choiceIx := s.choiceIxOpt(itemIx, value)
	if choiceIx < 0 {
		return gpui.FocusHandle{}, false
	}
	return s.runtime[itemIx].choiceFocusHandles[choiceIx], true
}

// IsCurrentInputFocused reports whether the current item's input has
// focus (U:questionnaire/state.rs:306-312).
func (s *QuestionnaireState) IsCurrentInputFocused(w *gpui.Window) bool {
	if s.current < 0 {
		return false
	}
	handle := s.runtime[s.current].inputFocusHandle
	return handle != nil && handle.IsFocused(w)
}

// CurrentInputHasText reports whether the current item's input holds
// text; the skin uses it to decide whether an arrow key moves focus or
// edits the draft (U:questionnaire/state.rs:314-323).
func (s *QuestionnaireState) CurrentInputHasText(app *gpui.App) bool {
	if s.current < 0 {
		return false
	}
	input := s.items[s.current].input
	if input == nil {
		return false
	}
	value := input.State().Read(app, func(st *InputState, _ *gpui.App) string { return st.Value() })
	return strings.TrimSpace(value) != ""
}

// FocusedCurrentChoice returns the current item's focused choice
// (U:questionnaire/state.rs:325-332).
func (s *QuestionnaireState) FocusedCurrentChoice(w *gpui.Window) (string, bool) {
	if s.current < 0 {
		return "", false
	}
	for choiceIx, handle := range s.runtime[s.current].choiceFocusHandles {
		if handle.IsFocused(w) {
			return s.items[s.current].Choices()[choiceIx].Value(), true
		}
	}
	return "", false
}

// ShortcutMode returns the shortcut mode (U:questionnaire/state.rs:334-336).
func (s *QuestionnaireState) ShortcutMode() (QuestionnaireShortcutMode, bool) {
	if s.shortcutMode == nil {
		return 0, false
	}
	return *s.shortcutMode, true
}

// ShortcutForChoice returns an enabled choice's shortcut: a letter or a
// one-based digit by its position among the enabled choices
// (U:questionnaire/state.rs:338-358).
func (s *QuestionnaireState) ShortcutForChoice(item, value string) (string, bool) {
	if s.shortcutMode == nil {
		return "", false
	}
	itemIx := s.itemIxOpt(item)
	if itemIx < 0 {
		return "", false
	}
	choiceIx := s.choiceIxOpt(itemIx, value)
	if choiceIx < 0 {
		return "", false
	}
	runtime := &s.runtime[itemIx]
	if runtime.disabled || runtime.choiceDisabled[choiceIx] {
		return "", false
	}
	count := 0
	for ix := 0; ix <= choiceIx; ix++ {
		if !runtime.choiceDisabled[ix] {
			count++
		}
	}
	if count == 0 {
		return "", false
	}
	position := count - 1
	switch *s.shortcutMode {
	case QuestionnaireShortcutLetters:
		if position < 26 {
			return string(rune('A' + position)), true
		}
	case QuestionnaireShortcutNumbers:
		if position < 9 {
			return strconv.Itoa(position + 1), true
		}
	}
	return "", false
}

// asciiEqualFold is str::eq_ignore_ascii_case.
func asciiEqualFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}

// ChoiceForShortcut returns the choice a key activates
// (U:questionnaire/state.rs:360-367).
func (s *QuestionnaireState) ChoiceForShortcut(item, key string) (string, bool) {
	itemIx := s.itemIxOpt(item)
	if itemIx < 0 {
		return "", false
	}
	for _, choice := range s.items[itemIx].Choices() {
		if shortcut, ok := s.ShortcutForChoice(item, choice.Value()); ok && asciiEqualFold(shortcut, key) {
			return choice.Value(), true
		}
	}
	return "", false
}

// ActivateShortcut activates and focuses the current item's choice for
// key (U:questionnaire/state.rs:369-386).
func (s *QuestionnaireState) ActivateShortcut(key string, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
	if s.current < 0 {
		return false
	}
	item := s.items[s.current].Name()
	choice, ok := s.ChoiceForShortcut(item, key)
	if !ok {
		return false
	}
	if s.ActivateChoice(item, choice, cx) != nil {
		return false
	}
	return s.FocusChoice(item, choice, w)
}

// SetCurrentItem moves to an enabled item and focuses it
// (U:questionnaire/state.rs:388-401).
func (s *QuestionnaireState) SetCurrentItem(name string, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
	ix, err := s.itemIx(name)
	if err != nil {
		return err
	}
	if !s.runtime[ix].disabled {
		s.current = ix
		s.FocusCurrentItem(w)
		cx.Notify()
	}
	return nil
}

// SetAnswer sets an item's answer silently; a freeform answer is written
// to the input (U:questionnaire/state.rs:403-440).
func (s *QuestionnaireState) SetAnswer(item string, answer QuestionnaireAnswer, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
	itemIx, err := s.itemIx(item)
	if err != nil {
		return err
	}
	before := s.effectiveAnswer(itemIx)
	beforeStatus := s.status(itemIx)
	answer = answer.clone()
	if answer.freeform != nil && strings.TrimSpace(*answer.freeform) == "" {
		answer.freeform = nil
	}
	if err := s.checkAnswer(itemIx, answer); err != nil {
		return err
	}
	var ordered []string
	for _, choice := range s.items[itemIx].Choices() {
		if slices.Contains(answer.choices, choice.Value()) {
			ordered = append(ordered, choice.Value())
		}
	}
	answer.choices = ordered
	s.runtime[itemIx].answer = answer.clone()
	s.runtime[itemIx].skipped = false
	if input := s.items[itemIx].input; input != nil && answer.freeform != nil {
		value := *answer.freeform
		input.State().Update(cx, func(st *InputState, icx *gpui.Context[InputState]) { st.SetValue(value, w, icx) })
	}
	if !before.Equal(s.effectiveAnswer(itemIx)) || beforeStatus != s.status(itemIx) {
		s.answerDidChange(itemIx, false, cx)
	}
	return nil
}

// SetInputValue writes an item's input silently and syncs the answer
// (U:questionnaire/state.rs:442-462).
func (s *QuestionnaireState) SetInputValue(item, value string, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
	itemIx, err := s.itemIx(item)
	if err != nil {
		return err
	}
	input := s.items[itemIx].input
	if input == nil {
		return QuestionnaireSchemaError{Kind: QuestionnaireAnswerDoesNotMatchItem, Item: s.items[itemIx].Name()}
	}
	input.State().Update(cx, func(st *InputState, icx *gpui.Context[InputState]) { st.SetValue(value, w, icx) })
	s.syncInputAnswer(itemIx, false, cx)
	return nil
}

// SetItemDisabled enables or disables an item; disabling the current item
// moves to the next enabled one, else the previous
// (U:questionnaire/state.rs:464-500).
func (s *QuestionnaireState) SetItemDisabled(name string, disabled bool, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
	ix, err := s.itemIx(name)
	if err != nil {
		return err
	}
	if s.runtime[ix].disabled == disabled {
		return nil
	}
	s.runtime[ix].disabled = disabled
	if input := s.items[ix].input; input != nil {
		inputDisabled := disabled || s.runtime[ix].inputDisabled
		input.State().Update(cx, func(st *InputState, icx *gpui.Context[InputState]) { st.SetDisabled(inputDisabled, icx) })
	}
	s.complete = false
	if s.current == ix && disabled {
		next := -1
		enabled := s.enabledIndices()
		for _, candidate := range enabled {
			if candidate > ix {
				next = candidate
				break
			}
		}
		if next < 0 {
			for i := len(enabled) - 1; i >= 0; i-- {
				if enabled[i] < ix {
					next = enabled[i]
					break
				}
			}
		}
		s.current = next
		s.FocusCurrentItem(w)
	} else if s.current < 0 && !disabled {
		s.current = ix
		s.FocusCurrentItem(w)
	}
	cx.Notify()
	return nil
}

// SetChoiceDisabled enables or disables a choice
// (U:questionnaire/state.rs:502-517).
func (s *QuestionnaireState) SetChoiceDisabled(item, value string, disabled bool, cx *gpui.Context[QuestionnaireState]) error {
	itemIx, err := s.itemIx(item)
	if err != nil {
		return err
	}
	choiceIx, err := s.choiceIx(itemIx, value)
	if err != nil {
		return err
	}
	if s.runtime[itemIx].choiceDisabled[choiceIx] == disabled {
		return nil
	}
	s.runtime[itemIx].choiceDisabled[choiceIx] = disabled
	s.answerDidChange(itemIx, false, cx)
	return nil
}

// SetExternalError sets a host error on an item
// (U:questionnaire/state.rs:519-530).
func (s *QuestionnaireState) SetExternalError(item, message string, cx *gpui.Context[QuestionnaireState]) error {
	ix, err := s.itemIx(item)
	if err != nil {
		return err
	}
	e := QuestionnaireValidationMessage(message)
	s.runtime[ix].externalError = &e
	s.complete = false
	cx.Notify()
	return nil
}

// ClearExternalError clears an item's host error
// (U:questionnaire/state.rs:532-541).
func (s *QuestionnaireState) ClearExternalError(item string, cx *gpui.Context[QuestionnaireState]) error {
	ix, err := s.itemIx(item)
	if err != nil {
		return err
	}
	s.runtime[ix].externalError = nil
	cx.Notify()
	return nil
}

// Reset restores the initial answers, inputs and current item silently
// (U:questionnaire/state.rs:543-572).
func (s *QuestionnaireState) Reset(w *gpui.Window, cx *gpui.Context[QuestionnaireState]) {
	for ix := range s.items {
		s.runtime[ix].answer = s.runtime[ix].initialAnswer.clone()
		s.runtime[ix].skipped = false
		s.runtime[ix].validationAttempt = false
		s.runtime[ix].internalError = nil
		if input := s.items[ix].input; input != nil {
			value := ""
			if s.runtime[ix].initialInputValue != nil {
				value = *s.runtime[ix].initialInputValue
			}
			input.State().Update(cx, func(st *InputState, icx *gpui.Context[InputState]) { st.SetValue(value, w, icx) })
		}
	}
	s.complete = false
	s.current = -1
	if s.initialCurrent != nil {
		s.current = s.itemIxOpt(*s.initialCurrent)
	}
	if s.current >= 0 && s.runtime[s.current].disabled {
		s.current = -1
		if enabled := s.enabledIndices(); len(enabled) > 0 {
			s.current = enabled[0]
		}
	}
	s.FocusCurrentItem(w)
	cx.Notify()
}

// ActivateChoice toggles a multiple choice or selects a single one, and
// emits AnswerChanged when the answer changed
// (U:questionnaire/state.rs:574-609).
func (s *QuestionnaireState) ActivateChoice(item, value string, cx *gpui.Context[QuestionnaireState]) error {
	itemIx, err := s.itemIx(item)
	if err != nil {
		return err
	}
	choiceIx, err := s.choiceIx(itemIx, value)
	if err != nil {
		return err
	}
	runtime := &s.runtime[itemIx]
	if runtime.disabled || runtime.choiceDisabled[choiceIx] {
		return nil
	}
	before := s.effectiveAnswer(itemIx)
	beforeStatus := s.status(itemIx)

	if s.items[itemIx].IsMultiple() {
		if ix := slices.Index(runtime.answer.choices, value); ix >= 0 {
			runtime.answer.choices = slices.Delete(slices.Clone(runtime.answer.choices), ix, ix+1)
		} else {
			runtime.answer.choices = append(slices.Clip(runtime.answer.choices), value)
		}
	} else {
		runtime.answer.choices = []string{value}
		runtime.answer.freeform = nil
	}
	runtime.skipped = false
	if !before.Equal(s.effectiveAnswer(itemIx)) || beforeStatus != s.status(itemIx) {
		s.answerDidChange(itemIx, true, cx)
	}
	return nil
}

// ConfirmCurrent submits on the last item and moves next otherwise
// (U:questionnaire/state.rs:611-620).
func (s *QuestionnaireState) ConfirmCurrent(w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
	ix, ok := s.CurrentIx()
	if !ok {
		return false
	}
	if ix+1 == s.Total() {
		return s.Submit(w, cx)
	}
	return s.GoNext(w, cx)
}

// GoPrevious moves to the previous enabled item
// (U:questionnaire/state.rs:622-632).
func (s *QuestionnaireState) GoPrevious(w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
	ix, ok := s.CurrentIx()
	if !ok {
		return false
	}
	enabled := s.enabledIndices()
	if ix == 0 {
		return false
	}
	s.changeCurrent(enabled[ix-1], true, w, cx)
	return true
}

// GoNext validates the current item and moves to the next enabled one
// (U:questionnaire/state.rs:634-652).
func (s *QuestionnaireState) GoNext(w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
	current := s.current
	if current < 0 {
		return false
	}
	if !s.validateItem(current) {
		s.FocusInvalidItem(s.items[current].Name(), w)
		cx.Notify()
		return false
	}
	ix, ok := s.CurrentIx()
	if !ok {
		return false
	}
	enabled := s.enabledIndices()
	if ix+1 >= len(enabled) {
		return false
	}
	s.changeCurrent(enabled[ix+1], true, w, cx)
	return true
}

// SkipCurrent skips an optional current item, then submits on the last
// item or moves next (U:questionnaire/state.rs:654-671).
func (s *QuestionnaireState) SkipCurrent(w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
	ix := s.current
	if ix < 0 || s.items[ix].IsRequired() {
		return false
	}
	s.runtime[ix].answer = NewQuestionnaireAnswer()
	s.runtime[ix].skipped = true
	s.complete = false
	s.emitAnswerChanged(ix, cx)
	if pos, ok := s.CurrentIx(); ok && pos+1 == s.Total() {
		return s.Submit(w, cx)
	}
	return s.GoNext(w, cx)
}

// Submit validates every enabled item; on success it emits Completed (the
// first time) then Submit, else it returns to the first invalid item
// (U:questionnaire/state.rs:673-696).
func (s *QuestionnaireState) Submit(w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
	firstInvalid := -1
	for _, ix := range s.enabledIndices() {
		if !s.validateItem(ix) && firstInvalid < 0 {
			firstInvalid = ix
		}
	}
	if firstInvalid >= 0 {
		s.changeCurrent(firstInvalid, true, w, cx)
		s.FocusInvalidItem(s.items[firstInvalid].Name(), w)
		cx.Notify()
		return false
	}

	submission := s.submission()
	if !s.complete {
		s.complete = true
		QuestionnaireEvents.Emit(cx, QuestionnaireEvent{Kind: QuestionnaireCompleted, Submission: submission})
	}
	QuestionnaireEvents.Emit(cx, QuestionnaireEvent{Kind: QuestionnaireSubmit, Submission: submission})
	cx.Notify()
	return true
}

// FocusCurrentItem focuses the current item's group
// (U:questionnaire/state.rs:698-702).
func (s *QuestionnaireState) FocusCurrentItem(w *gpui.Window) bool {
	if s.current < 0 {
		return false
	}
	s.runtime[s.current].focusHandle.Focus(w)
	return true
}

// FocusInvalidItem focuses the most useful control of an invalid item: a
// filled input, a selected choice, the first enabled choice, the input,
// else the item group (U:questionnaire/state.rs:704-742).
func (s *QuestionnaireState) FocusInvalidItem(item string, w *gpui.Window) bool {
	itemIx := s.itemIxOpt(item)
	if itemIx < 0 {
		return false
	}
	runtime := &s.runtime[itemIx]
	if runtime.answer.freeform != nil && !runtime.inputDisabled && runtime.inputFocusHandle != nil {
		runtime.inputFocusHandle.Focus(w)
		return true
	}
	for choiceIx, choice := range s.items[itemIx].Choices() {
		if slices.Contains(runtime.answer.choices, choice.Value()) && !runtime.choiceDisabled[choiceIx] {
			runtime.choiceFocusHandles[choiceIx].Focus(w)
			return true
		}
	}
	if choiceIx := slices.Index(runtime.choiceDisabled, false); choiceIx >= 0 {
		runtime.choiceFocusHandles[choiceIx].Focus(w)
		return true
	}
	if !runtime.inputDisabled && runtime.inputFocusHandle != nil {
		runtime.inputFocusHandle.Focus(w)
		return true
	}
	runtime.focusHandle.Focus(w)
	return true
}

// FocusChoice focuses a choice (U:questionnaire/state.rs:744-753).
func (s *QuestionnaireState) FocusChoice(item, value string, w *gpui.Window) bool {
	handle, ok := s.ChoiceFocusHandle(item, value)
	if !ok {
		return false
	}
	handle.Focus(w)
	return true
}

// FocusInput focuses an item's input (U:questionnaire/state.rs:755-764).
func (s *QuestionnaireState) FocusInput(item string, w *gpui.Window) bool {
	itemIx := s.itemIxOpt(item)
	if itemIx < 0 || s.runtime[itemIx].inputFocusHandle == nil {
		return false
	}
	s.runtime[itemIx].inputFocusHandle.Focus(w)
	return true
}

// FocusPreviousAnswer moves focus to the previous answer
// (U:questionnaire/state.rs:766-768).
func (s *QuestionnaireState) FocusPreviousAnswer(w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
	return s.focusAdjacentAnswer(-1, w, cx)
}

// FocusNextAnswer moves focus to the next answer
// (U:questionnaire/state.rs:770-772).
func (s *QuestionnaireState) FocusNextAnswer(w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
	return s.focusAdjacentAnswer(1, w, cx)
}

// MoveCurrentRadio moves the current single-choice item's selection like
// a native radio group, wrapping (U:questionnaire/state.rs:774-818).
func (s *QuestionnaireState) MoveCurrentRadio(direction int, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
	itemIx := s.current
	if itemIx < 0 {
		return false
	}
	if s.items[itemIx].IsMultiple() || s.IsCurrentInputFocused(w) {
		return false
	}
	runtime := &s.runtime[itemIx]
	var enabled []int
	for ix, disabled := range runtime.choiceDisabled {
		if !disabled {
			enabled = append(enabled, ix)
		}
	}
	if len(enabled) == 0 {
		return false
	}
	current := slices.IndexFunc(enabled, func(ix int) bool { return runtime.choiceFocusHandles[ix].IsFocused(w) })
	if current < 0 {
		current = slices.IndexFunc(enabled, func(ix int) bool {
			return slices.Contains(runtime.answer.choices, s.items[itemIx].Choices()[ix].Value())
		})
	}
	target := wrapTarget(current, direction < 0, len(enabled))
	choiceIx := enabled[target]
	item := s.items[itemIx].Name()
	choice := s.items[itemIx].Choices()[choiceIx].Value()
	_ = s.ActivateChoice(item, choice, cx)
	s.runtime[itemIx].choiceFocusHandles[choiceIx].Focus(w)
	return true
}

// wrapTarget is the shared (current, direction) → target match
// (U:questionnaire/state.rs:806-811, 895-900); current is -1 for None.
func wrapTarget(current int, negative bool, n int) int {
	switch {
	case current >= 0 && negative:
		if current == 0 {
			return n - 1
		}
		return current - 1
	case current >= 0:
		return (current + 1) % n
	case negative:
		return n - 1
	}
	return 0
}

// questionnaireTarget is one answer target: a choice index, or the input
// (choice -1) (U:questionnaire/state.rs:833-837).
type questionnaireTarget struct {
	choice int
}

func (t questionnaireTarget) isInput() bool { return t.choice < 0 }

// focusAdjacentAnswer moves focus through the current item's answers in
// schema order (U:questionnaire/state.rs:820-931).
func (s *QuestionnaireState) focusAdjacentAnswer(direction int, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
	itemIx := s.current
	if itemIx < 0 {
		return false
	}
	if s.IsCurrentInputFocused(w) && s.CurrentInputHasText(cx.App()) {
		return false
	}

	runtime := &s.runtime[itemIx]
	var targets []questionnaireTarget
	for choiceIx := range s.items[itemIx].Choices() {
		if !runtime.choiceDisabled[choiceIx] {
			targets = append(targets, questionnaireTarget{choiceIx})
		}
	}
	if s.items[itemIx].input != nil && !runtime.inputDisabled {
		targets = append(targets, questionnaireTarget{-1})
	}
	if len(targets) == 0 {
		return false
	}

	focused := slices.IndexFunc(targets, func(t questionnaireTarget) bool {
		if t.isInput() {
			return s.IsCurrentInputFocused(w)
		}
		return runtime.choiceFocusHandles[t.choice].IsFocused(w)
	})

	if focused < 0 && runtime.focusHandle.IsFocused(w) {
		var filled []int
		for ix, t := range targets {
			if t.isInput() {
				if runtime.answer.freeform != nil {
					filled = append(filled, ix)
				}
			} else if slices.Contains(runtime.answer.choices, s.items[itemIx].Choices()[t.choice].Value()) {
				filled = append(filled, ix)
			}
		}
		if len(filled) > 0 {
			filledIx := filled[0]
			if direction < 0 {
				filledIx = filled[len(filled)-1]
			}
			if t := targets[filledIx]; t.isInput() {
				if runtime.inputFocusHandle != nil {
					runtime.inputFocusHandle.Focus(w)
				}
			} else {
				runtime.choiceFocusHandles[t.choice].Focus(w)
			}
			return true
		}
	}

	targetIx := wrapTarget(focused, direction < 0, len(targets))

	// The Radio primitive leaves group arrow behavior to its owner. The
	// root emulates native radio movement only when both adjacent answers
	// are radios; crossing to an Input stays in this schema-ordered
	// answer sequence.
	if !s.items[itemIx].IsMultiple() && focused >= 0 && !targets[focused].isInput() && !targets[targetIx].isInput() {
		return false
	}

	if t := targets[targetIx]; t.isInput() {
		if runtime.inputFocusHandle != nil {
			runtime.inputFocusHandle.Focus(w)
		}
	} else {
		if !s.items[itemIx].IsMultiple() {
			item := s.items[itemIx].Name()
			choice := s.items[itemIx].Choices()[t.choice].Value()
			_ = s.ActivateChoice(item, choice, cx)
		}
		s.runtime[itemIx].choiceFocusHandles[t.choice].Focus(w)
	}
	return true
}

// onInputChange syncs the answer of the item owning the input
// (U:questionnaire/state.rs:933-937).
func (s *QuestionnaireState) onInputChange(input gpui.EntityID, cx *gpui.Context[QuestionnaireState]) {
	if ix := s.inputItemIx(input); ix >= 0 {
		s.syncInputAnswer(ix, true, cx)
	}
}

// syncInputAnswer copies an input's text into the answer
// (U:questionnaire/state.rs:939-961).
func (s *QuestionnaireState) syncInputAnswer(itemIx int, emit bool, cx *gpui.Context[QuestionnaireState]) {
	if s.runtime[itemIx].disabled || s.runtime[itemIx].inputDisabled {
		return
	}
	before := s.effectiveAnswer(itemIx)
	beforeStatus := s.status(itemIx)
	input := s.items[itemIx].input
	if input == nil {
		return
	}
	value := input.State().Read(cx, func(st *InputState, _ *gpui.App) string { return st.Value() })
	runtime := &s.runtime[itemIx]
	if strings.TrimSpace(value) == "" {
		runtime.answer.freeform = nil
	} else {
		if !s.items[itemIx].IsMultiple() {
			runtime.answer.choices = nil
		}
		runtime.answer.freeform = &value
		runtime.skipped = false
	}
	if !before.Equal(s.effectiveAnswer(itemIx)) || beforeStatus != s.status(itemIx) {
		s.answerDidChange(itemIx, emit, cx)
	}
}

// answerDidChange revalidates or clears the error, resets completion and
// optionally emits AnswerChanged (U:questionnaire/state.rs:963-974).
func (s *QuestionnaireState) answerDidChange(itemIx int, emit bool, cx *gpui.Context[QuestionnaireState]) {
	if s.runtime[itemIx].validationAttempt {
		s.validateItem(itemIx)
	} else {
		s.runtime[itemIx].internalError = nil
	}
	s.complete = false
	if emit {
		s.emitAnswerChanged(itemIx, cx)
	}
	cx.Notify()
}

// emitAnswerChanged emits AnswerChanged (U:questionnaire/state.rs:976-984).
func (s *QuestionnaireState) emitAnswerChanged(itemIx int, cx *gpui.Context[QuestionnaireState]) {
	QuestionnaireEvents.Emit(cx, QuestionnaireEvent{
		Kind: QuestionnaireAnswerChanged,
		Change: QuestionnaireAnswerChange{
			item:   s.items[itemIx].Name(),
			answer: s.effectiveAnswer(itemIx),
			status: s.status(itemIx),
		},
	})
}

// validateItem validates an item and records the internal error
// (U:questionnaire/state.rs:986-1011).
func (s *QuestionnaireState) validateItem(itemIx int) bool {
	if s.runtime[itemIx].disabled || s.runtime[itemIx].skipped {
		return true
	}
	s.runtime[itemIx].validationAttempt = true
	answer := s.effectiveAnswer(itemIx)
	var e *QuestionnaireValidationError
	if answer.IsEmpty() {
		kind := QuestionnaireErrorUnanswered
		if s.items[itemIx].IsRequired() {
			kind = QuestionnaireErrorRequired
		}
		e = &QuestionnaireValidationError{Kind: kind}
	} else if validator := s.items[itemIx].Validator(); validator != nil {
		if err := validator(&QuestionnaireValidationContext{item: s.items[itemIx].Name(), answer: answer, answers: s.Answers()}); err != nil {
			m := QuestionnaireValidationMessage(err.Error())
			e = &m
		}
	}
	s.runtime[itemIx].internalError = e
	return s.errorAt(itemIx) == nil
}

// submission builds the submission (U:questionnaire/state.rs:1013-1025).
func (s *QuestionnaireState) submission() QuestionnaireSubmission {
	var items []QuestionnaireSubmissionItem
	for _, ix := range s.enabledIndices() {
		items = append(items, QuestionnaireSubmissionItem{name: s.items[ix].Name(), status: s.status(ix), answer: s.effectiveAnswer(ix)})
	}
	return QuestionnaireSubmission{items: items}
}

// status derives an item's status (U:questionnaire/state.rs:1027-1035).
func (s *QuestionnaireState) status(itemIx int) QuestionnaireItemStatus {
	switch {
	case s.runtime[itemIx].skipped:
		return QuestionnaireItemSkipped
	case s.effectiveAnswer(itemIx).IsEmpty():
		return QuestionnaireItemUnanswered
	}
	return QuestionnaireItemAnswered
}

// effectiveAnswer is the answer without disabled choices or a disabled
// input, in schema order (U:questionnaire/state.rs:1037-1058).
func (s *QuestionnaireState) effectiveAnswer(itemIx int) QuestionnaireAnswer {
	runtime := &s.runtime[itemIx]
	if runtime.disabled {
		return NewQuestionnaireAnswer()
	}
	var out QuestionnaireAnswer
	for _, choice := range s.items[itemIx].Choices() {
		if !slices.Contains(runtime.answer.choices, choice.Value()) {
			continue
		}
		if ix := s.choiceIxOpt(itemIx, choice.Value()); ix >= 0 && !runtime.choiceDisabled[ix] {
			out.choices = append(out.choices, choice.Value())
		}
	}
	if !runtime.inputDisabled && runtime.answer.freeform != nil {
		value := *runtime.answer.freeform
		out.freeform = &value
	}
	return out
}

// errorAt returns an item's active error: the host's first, then the
// attempted validation's (U:questionnaire/state.rs:1060-1070).
func (s *QuestionnaireState) errorAt(itemIx int) *QuestionnaireValidationError {
	runtime := &s.runtime[itemIx]
	if runtime.skipped || runtime.disabled {
		return nil
	}
	if runtime.externalError != nil {
		return runtime.externalError
	}
	if runtime.validationAttempt {
		return runtime.internalError
	}
	return nil
}

// checkAnswer rejects an answer the item cannot hold
// (U:questionnaire/state.rs:1072-1095).
func (s *QuestionnaireState) checkAnswer(itemIx int, answer QuestionnaireAnswer) error {
	item := s.items[itemIx]
	sources := len(answer.choices)
	if answer.freeform != nil {
		sources++
	}
	if (!item.IsMultiple() && sources > 1) || (answer.freeform != nil && item.input == nil) {
		return QuestionnaireSchemaError{Kind: QuestionnaireAnswerDoesNotMatchItem, Item: item.Name()}
	}
	for _, choice := range answer.choices {
		choiceIx, err := s.choiceIx(itemIx, choice)
		if err != nil {
			return err
		}
		if s.runtime[itemIx].choiceDisabled[choiceIx] {
			return QuestionnaireSchemaError{Kind: QuestionnaireAnswerDoesNotMatchItem, Item: item.Name()}
		}
	}
	return nil
}

// changeCurrent moves the current item, focusing it and optionally
// emitting CurrentItemChanged (U:questionnaire/state.rs:1097-1117).
func (s *QuestionnaireState) changeCurrent(next int, emit bool, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) {
	if s.current == next {
		return
	}
	var previous, current *string
	if s.current >= 0 {
		name := s.items[s.current].Name()
		previous = &name
	}
	s.current = next
	s.FocusCurrentItem(w)
	if emit {
		if next >= 0 {
			name := s.items[next].Name()
			current = &name
		}
		QuestionnaireEvents.Emit(cx, QuestionnaireEvent{Kind: QuestionnaireCurrentItemChanged, Previous: previous, Current: current})
	}
	cx.Notify()
}

// enabledIndices lists the enabled items (U:questionnaire/state.rs:1119-1124).
func (s *QuestionnaireState) enabledIndices() []int {
	var out []int
	for ix := range s.runtime {
		if !s.runtime[ix].disabled {
			out = append(out, ix)
		}
	}
	return out
}

// itemIx resolves an item name (U:questionnaire/state.rs:1126-1129).
func (s *QuestionnaireState) itemIx(name string) (int, error) {
	if ix := s.itemIxOpt(name); ix >= 0 {
		return ix, nil
	}
	return 0, QuestionnaireSchemaError{Kind: QuestionnaireUnknownItem, Item: name}
}

// itemIxOpt resolves an item name, -1 when unknown
// (U:questionnaire/state.rs:1131-1135).
func (s *QuestionnaireState) itemIxOpt(name string) int {
	return slices.IndexFunc(s.items, func(item QuestionnaireItemDefinition) bool { return item.Name() == name })
}

// choiceIx resolves a choice value (U:questionnaire/state.rs:1137-1143).
func (s *QuestionnaireState) choiceIx(itemIx int, value string) (int, error) {
	if ix := s.choiceIxOpt(itemIx, value); ix >= 0 {
		return ix, nil
	}
	return 0, QuestionnaireSchemaError{Kind: QuestionnaireUnknownChoice, Item: s.items[itemIx].Name(), Choice: value}
}

// choiceIxOpt resolves a choice value, -1 when unknown
// (U:questionnaire/state.rs:1145-1150).
func (s *QuestionnaireState) choiceIxOpt(itemIx int, value string) int {
	return slices.IndexFunc(s.items[itemIx].Choices(), func(c QuestionnaireChoiceDefinition) bool { return c.Value() == value })
}

// inputItemIx finds the item owning an input (U:questionnaire/state.rs:1152-1157).
func (s *QuestionnaireState) inputItemIx(input gpui.EntityID) int {
	return slices.IndexFunc(s.items, func(item QuestionnaireItemDefinition) bool {
		return item.input != nil && item.input.State().EntityID() == input
	})
}
