package base

// Port of U:questionnaire/types.rs: the questionnaire's definition, state,
// event and error types. Option<SharedString> results are (string, bool)
// pairs; data-carrying enums are structs with a Kind.

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
)

// QuestionnaireValidator validates one item against the current answers
// (U:questionnaire/types.rs:7-9). A nil error passes; a non-nil error's
// text becomes the item's QuestionnaireValidationError message.
type QuestionnaireValidator func(ctx *QuestionnaireValidationContext) error

// QuestionnaireChoiceDefinition describes one selectable answer
// (U:questionnaire/types.rs:11-69).
type QuestionnaireChoiceDefinition struct {
	value              string
	accessibilityLabel string
	description        *string
	disabled           bool
	defaultSelected    bool
}

// NewQuestionnaireChoiceDefinition is QuestionnaireChoiceDefinition::new
// (U:questionnaire/types.rs:22-33).
func NewQuestionnaireChoiceDefinition(value, accessibilityLabel string) QuestionnaireChoiceDefinition {
	return QuestionnaireChoiceDefinition{value: value, accessibilityLabel: accessibilityLabel}
}

// WithDescription sets the description (U:questionnaire/types.rs:35-38).
func (c QuestionnaireChoiceDefinition) WithDescription(description string) QuestionnaireChoiceDefinition {
	c.description = &description
	return c
}

// WithDisabled sets the disabled state (U:questionnaire/types.rs:40-43).
func (c QuestionnaireChoiceDefinition) WithDisabled(disabled bool) QuestionnaireChoiceDefinition {
	c.disabled = disabled
	return c
}

// WithDefaultSelected selects the choice initially
// (U:questionnaire/types.rs:45-48).
func (c QuestionnaireChoiceDefinition) WithDefaultSelected(selected bool) QuestionnaireChoiceDefinition {
	c.defaultSelected = selected
	return c
}

// Value returns the choice value (U:questionnaire/types.rs:50-52).
func (c QuestionnaireChoiceDefinition) Value() string { return c.value }

// AccessibilityLabel returns the accessible name
// (U:questionnaire/types.rs:54-56).
func (c QuestionnaireChoiceDefinition) AccessibilityLabel() string { return c.accessibilityLabel }

// Description returns the description (U:questionnaire/types.rs:58-60).
func (c QuestionnaireChoiceDefinition) Description() (string, bool) { return optString(c.description) }

// IsDisabled reports the disabled state (U:questionnaire/types.rs:62-64).
func (c QuestionnaireChoiceDefinition) IsDisabled() bool { return c.disabled }

// IsDefaultSelected reports the default selection
// (U:questionnaire/types.rs:66-68).
func (c QuestionnaireChoiceDefinition) IsDefaultSelected() bool { return c.defaultSelected }

func optString(s *string) (string, bool) {
	if s == nil {
		return "", false
	}
	return *s, true
}

// QuestionnaireInputDefinition describes the optional freeform answer an
// item owns (U:questionnaire/types.rs:71-104).
type QuestionnaireInputDefinition struct {
	state              gpui.Entity[InputState]
	accessibilityLabel string
	disabled           bool
}

// NewQuestionnaireInputDefinition is QuestionnaireInputDefinition::new
// (U:questionnaire/types.rs:80-86).
func NewQuestionnaireInputDefinition(state gpui.Entity[InputState], accessibilityLabel string) QuestionnaireInputDefinition {
	return QuestionnaireInputDefinition{state: state, accessibilityLabel: accessibilityLabel}
}

// WithDisabled sets the disabled state (U:questionnaire/types.rs:88-91).
func (d QuestionnaireInputDefinition) WithDisabled(disabled bool) QuestionnaireInputDefinition {
	d.disabled = disabled
	return d
}

// State returns the input state (U:questionnaire/types.rs:93-95).
func (d QuestionnaireInputDefinition) State() gpui.Entity[InputState] { return d.state }

// AccessibilityLabel returns the accessible name
// (U:questionnaire/types.rs:97-99).
func (d QuestionnaireInputDefinition) AccessibilityLabel() string { return d.accessibilityLabel }

// IsDisabled reports the disabled state (U:questionnaire/types.rs:101-103).
func (d QuestionnaireInputDefinition) IsDisabled() bool { return d.disabled }

// QuestionnaireItemDefinition describes one ordered item
// (U:questionnaire/types.rs:106-236).
type QuestionnaireItemDefinition struct {
	name               string
	accessibilityLabel string
	description        *string
	required           bool
	multiple           bool
	disabled           bool
	choices            []QuestionnaireChoiceDefinition
	input              *QuestionnaireInputDefinition
	validator          QuestionnaireValidator
}

// NewQuestionnaireItemDefinition is QuestionnaireItemDefinition::new
// (U:questionnaire/types.rs:138-153).
func NewQuestionnaireItemDefinition(name, accessibilityLabel string) QuestionnaireItemDefinition {
	return QuestionnaireItemDefinition{name: name, accessibilityLabel: accessibilityLabel}
}

// String is the Debug impl (U:questionnaire/types.rs:120-135).
func (d QuestionnaireItemDefinition) String() string {
	validator := "None"
	if d.validator != nil {
		validator = "Some(<validator>)"
	}
	return fmt.Sprintf("QuestionnaireItemDefinition { name: %q, accessibility_label: %q, required: %v, multiple: %v, disabled: %v, choices: %d, input: %v, validator: %s }",
		d.name, d.accessibilityLabel, d.required, d.multiple, d.disabled, len(d.choices), d.input != nil, validator)
}

// WithDescription sets the description (U:questionnaire/types.rs:155-158).
func (d QuestionnaireItemDefinition) WithDescription(description string) QuestionnaireItemDefinition {
	d.description = &description
	return d
}

// WithRequired marks the item required (U:questionnaire/types.rs:160-163).
func (d QuestionnaireItemDefinition) WithRequired(required bool) QuestionnaireItemDefinition {
	d.required = required
	return d
}

// WithMultiple allows several choices (U:questionnaire/types.rs:165-168).
func (d QuestionnaireItemDefinition) WithMultiple(multiple bool) QuestionnaireItemDefinition {
	d.multiple = multiple
	return d
}

// WithDisabled sets the disabled state (U:questionnaire/types.rs:170-173).
func (d QuestionnaireItemDefinition) WithDisabled(disabled bool) QuestionnaireItemDefinition {
	d.disabled = disabled
	return d
}

// WithChoices replaces the choices (U:questionnaire/types.rs:175-181).
func (d QuestionnaireItemDefinition) WithChoices(choices ...QuestionnaireChoiceDefinition) QuestionnaireItemDefinition {
	d.choices = slices.Clone(choices)
	return d
}

// WithChoice appends a choice (U:questionnaire/types.rs:183-186).
func (d QuestionnaireItemDefinition) WithChoice(choice QuestionnaireChoiceDefinition) QuestionnaireItemDefinition {
	d.choices = append(slices.Clip(d.choices), choice)
	return d
}

// WithInput sets the freeform input (U:questionnaire/types.rs:188-191).
func (d QuestionnaireItemDefinition) WithInput(input QuestionnaireInputDefinition) QuestionnaireItemDefinition {
	d.input = &input
	return d
}

// WithValidator sets the validator (U:questionnaire/types.rs:193-199).
func (d QuestionnaireItemDefinition) WithValidator(validator QuestionnaireValidator) QuestionnaireItemDefinition {
	d.validator = validator
	return d
}

// Name returns the item name (U:questionnaire/types.rs:201-203).
func (d QuestionnaireItemDefinition) Name() string { return d.name }

// AccessibilityLabel returns the accessible name
// (U:questionnaire/types.rs:205-207).
func (d QuestionnaireItemDefinition) AccessibilityLabel() string { return d.accessibilityLabel }

// Description returns the description (U:questionnaire/types.rs:209-211).
func (d QuestionnaireItemDefinition) Description() (string, bool) { return optString(d.description) }

// IsRequired reports whether the item is required
// (U:questionnaire/types.rs:213-215).
func (d QuestionnaireItemDefinition) IsRequired() bool { return d.required }

// IsMultiple reports whether several choices are allowed
// (U:questionnaire/types.rs:217-219).
func (d QuestionnaireItemDefinition) IsMultiple() bool { return d.multiple }

// IsDisabled reports the disabled state (U:questionnaire/types.rs:221-223).
func (d QuestionnaireItemDefinition) IsDisabled() bool { return d.disabled }

// Choices returns the choices (U:questionnaire/types.rs:225-227).
func (d QuestionnaireItemDefinition) Choices() []QuestionnaireChoiceDefinition { return d.choices }

// Input returns the freeform input (U:questionnaire/types.rs:229-231).
func (d QuestionnaireItemDefinition) Input() (QuestionnaireInputDefinition, bool) {
	if d.input == nil {
		return QuestionnaireInputDefinition{}, false
	}
	return *d.input, true
}

// Validator returns the validator, or nil (U:questionnaire/types.rs:233-235).
func (d QuestionnaireItemDefinition) Validator() QuestionnaireValidator { return d.validator }

// QuestionnaireItemStatus is an item's answer status
// (U:questionnaire/types.rs:238-244). The zero value is Unanswered.
type QuestionnaireItemStatus uint8

const (
	QuestionnaireItemUnanswered QuestionnaireItemStatus = iota
	QuestionnaireItemAnswered
	QuestionnaireItemSkipped
)

// String names the status (the derived Debug).
func (s QuestionnaireItemStatus) String() string {
	switch s {
	case QuestionnaireItemAnswered:
		return "Answered"
	case QuestionnaireItemSkipped:
		return "Skipped"
	}
	return "Unanswered"
}

// QuestionnaireShortcutMode selects the choice shortcut alphabet
// (U:questionnaire/types.rs:246-250).
type QuestionnaireShortcutMode uint8

const (
	QuestionnaireShortcutLetters QuestionnaireShortcutMode = iota
	QuestionnaireShortcutNumbers
)

// QuestionnaireAnswer is an answer snapshot; input drafts are deliberately
// excluded (U:questionnaire/types.rs:252-294).
type QuestionnaireAnswer struct {
	choices  []string
	freeform *string
}

// NewQuestionnaireAnswer is QuestionnaireAnswer::new
// (U:questionnaire/types.rs:260-262).
func NewQuestionnaireAnswer() QuestionnaireAnswer { return QuestionnaireAnswer{} }

// WithChoices replaces the choices, dropping duplicates
// (U:questionnaire/types.rs:264-275).
func (a QuestionnaireAnswer) WithChoices(choices ...string) QuestionnaireAnswer {
	a.choices = nil
	for _, choice := range choices {
		if !slices.Contains(a.choices, choice) {
			a.choices = append(a.choices, choice)
		}
	}
	return a
}

// WithFreeform sets the freeform answer; blank text clears it
// (U:questionnaire/types.rs:277-281).
func (a QuestionnaireAnswer) WithFreeform(value string) QuestionnaireAnswer {
	a.freeform = nil
	if strings.TrimSpace(value) != "" {
		a.freeform = &value
	}
	return a
}

// Choices returns the selected choices (U:questionnaire/types.rs:283-285).
func (a QuestionnaireAnswer) Choices() []string { return a.choices }

// Freeform returns the freeform answer (U:questionnaire/types.rs:287-289).
func (a QuestionnaireAnswer) Freeform() (string, bool) { return optString(a.freeform) }

// IsEmpty reports an answer with no choice and no freeform text
// (U:questionnaire/types.rs:291-293).
func (a QuestionnaireAnswer) IsEmpty() bool { return len(a.choices) == 0 && a.freeform == nil }

// Equal is the derived PartialEq (U:questionnaire/types.rs:253).
func (a QuestionnaireAnswer) Equal(b QuestionnaireAnswer) bool {
	if !slices.Equal(a.choices, b.choices) {
		return false
	}
	af, aok := a.Freeform()
	bf, bok := b.Freeform()
	return aok == bok && af == bf
}

// clone copies the answer's slice.
func (a QuestionnaireAnswer) clone() QuestionnaireAnswer {
	a.choices = slices.Clone(a.choices)
	return a
}

// QuestionnaireAnswerEntry is one named answer of QuestionnaireAnswers.
type QuestionnaireAnswerEntry struct {
	Name   string
	Answer QuestionnaireAnswer
}

// QuestionnaireAnswers is the ordered answers of the enabled items
// (U:questionnaire/types.rs:296-323).
type QuestionnaireAnswers struct {
	entries []QuestionnaireAnswerEntry
}

// Get returns the named answer (U:questionnaire/types.rs:306-310).
func (a QuestionnaireAnswers) Get(name string) (QuestionnaireAnswer, bool) {
	for _, e := range a.entries {
		if e.Name == name {
			return e.Answer, true
		}
	}
	return QuestionnaireAnswer{}, false
}

// Entries returns the answers in schema order (U:questionnaire/types.rs:
// 312-314, iter).
func (a QuestionnaireAnswers) Entries() []QuestionnaireAnswerEntry { return a.entries }

// Len returns the number of answers (U:questionnaire/types.rs:316-318).
func (a QuestionnaireAnswers) Len() int { return len(a.entries) }

// IsEmpty reports no answers (U:questionnaire/types.rs:320-322).
func (a QuestionnaireAnswers) IsEmpty() bool { return len(a.entries) == 0 }

// QuestionnaireProgressState is the one-based position among the enabled
// items (U:questionnaire/types.rs:325-343).
type QuestionnaireProgressState struct {
	current, total int
}

// Current returns the one-based current position, 0 for none.
func (p QuestionnaireProgressState) Current() int { return p.current }

// Total returns the number of enabled items.
func (p QuestionnaireProgressState) Total() int { return p.total }

// QuestionnaireItemState is an item's derived state
// (U:questionnaire/types.rs:345-404).
type QuestionnaireItemState struct {
	name     string
	status   QuestionnaireItemStatus
	required bool
	multiple bool
	disabled bool
	invalid  bool
	hasInput bool
}

func (s QuestionnaireItemState) Name() string                    { return s.name }
func (s QuestionnaireItemState) Status() QuestionnaireItemStatus { return s.status }
func (s QuestionnaireItemState) IsRequired() bool                { return s.required }
func (s QuestionnaireItemState) IsMultiple() bool                { return s.multiple }
func (s QuestionnaireItemState) IsDisabled() bool                { return s.disabled }
func (s QuestionnaireItemState) IsInvalid() bool                 { return s.invalid }
func (s QuestionnaireItemState) HasInput() bool                  { return s.hasInput }

// QuestionnaireChoiceState is a choice's derived state
// (U:questionnaire/types.rs:406-451).
type QuestionnaireChoiceState struct {
	value    string
	selected bool
	disabled bool
	invalid  bool
	shortcut *string
}

func (s QuestionnaireChoiceState) Value() string            { return s.value }
func (s QuestionnaireChoiceState) IsSelected() bool         { return s.selected }
func (s QuestionnaireChoiceState) IsDisabled() bool         { return s.disabled }
func (s QuestionnaireChoiceState) IsInvalid() bool          { return s.invalid }
func (s QuestionnaireChoiceState) Shortcut() (string, bool) { return optString(s.shortcut) }

// QuestionnaireNavigationState is which navigation affordances apply
// (U:questionnaire/types.rs:453-498).
type QuestionnaireNavigationState struct {
	previousVisible bool
	nextVisible     bool
	skipVisible     bool
	submitVisible   bool
	confirmable     bool
}

func (s QuestionnaireNavigationState) IsPreviousVisible() bool { return s.previousVisible }
func (s QuestionnaireNavigationState) IsNextVisible() bool     { return s.nextVisible }
func (s QuestionnaireNavigationState) IsSkipVisible() bool     { return s.skipVisible }
func (s QuestionnaireNavigationState) IsSubmitVisible() bool   { return s.submitVisible }
func (s QuestionnaireNavigationState) IsConfirmable() bool     { return s.confirmable }

// QuestionnaireValidationContext is the immutable validation input
// (U:questionnaire/types.rs:500-532).
type QuestionnaireValidationContext struct {
	item    string
	answer  QuestionnaireAnswer
	answers QuestionnaireAnswers
}

func (c *QuestionnaireValidationContext) Item() string                  { return c.item }
func (c *QuestionnaireValidationContext) Answer() QuestionnaireAnswer   { return c.answer }
func (c *QuestionnaireValidationContext) Answers() QuestionnaireAnswers { return c.answers }

// QuestionnaireAnswerChange is the AnswerChanged payload
// (U:questionnaire/types.rs:534-565).
type QuestionnaireAnswerChange struct {
	item   string
	answer QuestionnaireAnswer
	status QuestionnaireItemStatus
}

func (c QuestionnaireAnswerChange) Item() string                    { return c.item }
func (c QuestionnaireAnswerChange) Answer() QuestionnaireAnswer     { return c.answer }
func (c QuestionnaireAnswerChange) Status() QuestionnaireItemStatus { return c.status }

// QuestionnaireSubmissionItem is one submitted item
// (U:questionnaire/types.rs:567-598).
type QuestionnaireSubmissionItem struct {
	name   string
	status QuestionnaireItemStatus
	answer QuestionnaireAnswer
}

func (i QuestionnaireSubmissionItem) Name() string                    { return i.name }
func (i QuestionnaireSubmissionItem) Status() QuestionnaireItemStatus { return i.status }
func (i QuestionnaireSubmissionItem) Answer() QuestionnaireAnswer     { return i.answer }

// QuestionnaireSubmission is the submitted items
// (U:questionnaire/types.rs:600-619).
type QuestionnaireSubmission struct {
	items []QuestionnaireSubmissionItem
}

// Items returns the submitted items (U:questionnaire/types.rs:610-612).
func (s QuestionnaireSubmission) Items() []QuestionnaireSubmissionItem { return s.items }

// Answer returns a submitted item's answer
// (U:questionnaire/types.rs:614-618).
func (s QuestionnaireSubmission) Answer(name string) (QuestionnaireAnswer, bool) {
	for _, item := range s.items {
		if item.name == name {
			return item.answer, true
		}
	}
	return QuestionnaireAnswer{}, false
}

// QuestionnaireEventKind discriminates QuestionnaireEvent
// (U:questionnaire/types.rs:621-631).
type QuestionnaireEventKind uint8

const (
	QuestionnaireCurrentItemChanged QuestionnaireEventKind = iota
	QuestionnaireAnswerChanged
	QuestionnaireCompleted
	QuestionnaireSubmit
)

// QuestionnaireEvent is emitted by QuestionnaireState
// (U:questionnaire/types.rs:621-631). Previous and Current carry
// CurrentItemChanged, Change carries AnswerChanged, Submission carries
// Completed and Submit.
type QuestionnaireEvent struct {
	Kind       QuestionnaireEventKind
	Previous   *string
	Current    *string
	Change     QuestionnaireAnswerChange
	Submission QuestionnaireSubmission
}

// QuestionnaireEvents is the QuestionnaireState event descriptor
// (U:questionnaire/state.rs:1160).
var QuestionnaireEvents = gpui.DefineEvent[QuestionnaireState, QuestionnaireEvent]()

// QuestionnaireValidationErrorKind discriminates
// QuestionnaireValidationError (U:questionnaire/types.rs:633-647).
type QuestionnaireValidationErrorKind uint8

const (
	// QuestionnaireErrorRequired: a required item has no answer.
	QuestionnaireErrorRequired QuestionnaireValidationErrorKind = iota
	// QuestionnaireErrorUnanswered: an optional item has no answer and has
	// not been skipped.
	QuestionnaireErrorUnanswered
	// QuestionnaireErrorMessage: a validator or the host supplied text.
	QuestionnaireErrorMessage
)

// QuestionnaireValidationError is why an item currently fails validation
// (U:questionnaire/types.rs:633-647). Required and Unanswered carry no
// text: the presentation layer supplies the localized sentence.
type QuestionnaireValidationError struct {
	Kind QuestionnaireValidationErrorKind
	text string
}

// QuestionnaireValidationMessage is QuestionnaireValidationError::Message.
func QuestionnaireValidationMessage(message string) QuestionnaireValidationError {
	return QuestionnaireValidationError{Kind: QuestionnaireErrorMessage, text: message}
}

// Message returns the text the host or a validator wrote, false for a
// built-in reason (U:questionnaire/types.rs:649-657).
func (e QuestionnaireValidationError) Message() (string, bool) {
	if e.Kind == QuestionnaireErrorMessage {
		return e.text, true
	}
	return "", false
}

// QuestionnaireSchemaErrorKind discriminates QuestionnaireSchemaError
// (U:questionnaire/types.rs:659-674).
type QuestionnaireSchemaErrorKind uint8

const (
	QuestionnaireDuplicateItem QuestionnaireSchemaErrorKind = iota
	QuestionnaireDuplicateChoice
	QuestionnaireMultipleDefaultsForSingleItem
	QuestionnaireUnknownItem
	QuestionnaireUnknownChoice
	QuestionnaireAnswerDoesNotMatchItem
)

// QuestionnaireSchemaError is a schema or answer mismatch
// (U:questionnaire/types.rs:659-698). Choice is set for DuplicateChoice
// and UnknownChoice. The value is comparable, so errors compare with ==.
type QuestionnaireSchemaError struct {
	Kind   QuestionnaireSchemaErrorKind
	Item   string
	Choice string
}

// Error is the Display impl (U:questionnaire/types.rs:676-696).
func (e QuestionnaireSchemaError) Error() string {
	switch e.Kind {
	case QuestionnaireDuplicateItem:
		return fmt.Sprintf("duplicate questionnaire item `%s`", e.Item)
	case QuestionnaireDuplicateChoice:
		return fmt.Sprintf("duplicate choice `%s` in item `%s`", e.Choice, e.Item)
	case QuestionnaireMultipleDefaultsForSingleItem:
		return fmt.Sprintf("single-choice item `%s` has more than one default answer", e.Item)
	case QuestionnaireUnknownItem:
		return fmt.Sprintf("unknown questionnaire item `%s`", e.Item)
	case QuestionnaireUnknownChoice:
		return fmt.Sprintf("unknown choice `%s` in item `%s`", e.Choice, e.Item)
	}
	return fmt.Sprintf("answer does not match item `%s`", e.Item)
}
