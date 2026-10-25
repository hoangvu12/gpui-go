package base

import (
	"errors"
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:questionnaire/state.rs (6/6).

// questionnaireHarness is U:questionnaire/state.rs:1171-1239 Harness.
type questionnaireHarness struct {
	state       gpui.Entity[QuestionnaireState]
	firstInput  gpui.Entity[InputState]
	secondInput gpui.Entity[InputState]
	events      []string
}

func (*questionnaireHarness) Render(*gpui.Window, *gpui.Context[questionnaireHarness]) gpui.AnyElement {
	return gpui.Div().Flex().IntoElement()
}

func newQuestionnaireHarness(h *questionnaireHarness, w *gpui.Window, cx *gpui.Context[questionnaireHarness]) {
	h.firstInput = gpui.NewEntity(cx, w.Scope(), func(s *InputState, icx *gpui.Context[InputState]) {
		*s = NewInputState(w, icx)
	})
	h.secondInput = gpui.NewEntity(cx, w.Scope(), func(s *InputState, icx *gpui.Context[InputState]) {
		*s = NewInputState(w, icx).DefaultValue("initial draft")
	})
	items := []QuestionnaireItemDefinition{
		NewQuestionnaireItemDefinition("first", "First question").
			WithRequired(true).
			WithChoices(NewQuestionnaireChoiceDefinition("a", "A"), NewQuestionnaireChoiceDefinition("b", "B")).
			WithInput(NewQuestionnaireInputDefinition(h.firstInput, "Another answer")),
		NewQuestionnaireItemDefinition("second", "Second question").
			WithMultiple(true).
			WithChoices(NewQuestionnaireChoiceDefinition("x", "X"), NewQuestionnaireChoiceDefinition("y", "Y")).
			WithInput(NewQuestionnaireInputDefinition(h.secondInput, "Another answer")).
			WithValidator(func(ctx *QuestionnaireValidationContext) error {
				if v, ok := ctx.Answer().Freeform(); ok && v == "valid" {
					return nil
				}
				return errors.New("Use the valid answer")
			}),
		NewQuestionnaireItemDefinition("disabled", "Disabled").WithDisabled(true),
	}
	h.state = gpui.NewEntity(cx, w.Scope(), func(s *QuestionnaireState, scx *gpui.Context[QuestionnaireState]) {
		state, err := NewQuestionnaireState(items, scx)
		if err != nil {
			panic(err)
		}
		*s = state.WithShortcuts(QuestionnaireShortcutLetters)
	})
	QuestionnaireEvents.Subscribe(cx, h.state, func(h *questionnaireHarness, _ gpui.Entity[QuestionnaireState], e *QuestionnaireEvent, _ *gpui.Context[questionnaireHarness]) {
		name := map[QuestionnaireEventKind]string{
			QuestionnaireCurrentItemChanged: "current",
			QuestionnaireAnswerChanged:      "answer",
			QuestionnaireCompleted:          "completed",
			QuestionnaireSubmit:             "submit",
		}[e.Kind]
		h.events = append(h.events, name)
	})
}

// qHarness is U:questionnaire/state.rs:1241-1260 harness.
type qHarness struct {
	t       *testing.T
	cx      *gpui.VisualTestContext
	harness gpui.Entity[questionnaireHarness]
	state   gpui.Entity[QuestionnaireState]
	first   gpui.Entity[InputState]
	second  gpui.Entity[InputState]
}

func openQuestionnaireHarness(t *testing.T) *qHarness {
	t.Helper()
	ta := gpui.NewTestApp()
	shutdownAtCleanup(t, ta)
	ta.Update(Init)
	harness, cx := ta.AddWindowView(newQuestionnaireHarness)
	q := &qHarness{t: t, cx: cx, harness: harness}
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		harness.Read(app, func(h *questionnaireHarness, _ *gpui.App) struct{} {
			q.state, q.first, q.second = h.state, h.firstInput, h.secondInput
			return struct{}{}
		})
	})
	cx.RunUntilParked()
	return q
}

// update runs f in a state update with the window.
func qUpdate[R any](q *qHarness, f func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) R) R {
	var out R
	q.cx.Update(func(w *gpui.Window, app *gpui.App) {
		out = q.state.UpdateWith(app, func(s *QuestionnaireState, scx *gpui.Context[QuestionnaireState]) R { return f(s, w, scx) })
	})
	q.cx.RunUntilParked()
	return out
}

// read runs f over the state with the window.
func qRead[R any](q *qHarness, f func(s *QuestionnaireState, w *gpui.Window, app *gpui.App) R) R {
	var out R
	q.cx.Update(func(w *gpui.Window, app *gpui.App) {
		out = q.state.Read(app, func(s *QuestionnaireState, app *gpui.App) R { return f(s, w, app) })
	})
	return out
}

func (q *qHarness) events() []string {
	var out []string
	q.cx.Update(func(_ *gpui.Window, app *gpui.App) {
		out = q.harness.Read(app, func(h *questionnaireHarness, _ *gpui.App) []string { return slices.Clone(h.events) })
	})
	return out
}

func (q *qHarness) inputValue(input gpui.Entity[InputState]) string {
	var out string
	q.cx.Update(func(_ *gpui.Window, app *gpui.App) {
		out = input.Read(app, func(s *InputState, _ *gpui.App) string { return s.Value() })
	})
	return out
}

func (q *qHarness) answer(name string) QuestionnaireAnswer {
	return qRead(q, func(s *QuestionnaireState, _ *gpui.Window, _ *gpui.App) QuestionnaireAnswer {
		a, ok := s.Answer(name)
		if !ok {
			q.t.Fatalf("no answer for %q", name)
		}
		return a
	})
}

func (q *qHarness) errorOf(name string) (QuestionnaireValidationError, bool) {
	type res struct {
		e  QuestionnaireValidationError
		ok bool
	}
	r := qRead(q, func(s *QuestionnaireState, _ *gpui.Window, _ *gpui.App) res {
		e, ok := s.Error(name)
		return res{e, ok}
	})
	return r.e, r.ok
}

func (q *qHarness) currentItem() string {
	return qRead(q, func(s *QuestionnaireState, _ *gpui.Window, _ *gpui.App) string {
		name, _ := s.CurrentItem()
		return name
	})
}

func (q *qHarness) focusedChoice() (string, bool) {
	type res struct {
		v  string
		ok bool
	}
	r := qRead(q, func(s *QuestionnaireState, w *gpui.Window, _ *gpui.App) res {
		v, ok := s.FocusedCurrentChoice(w)
		return res{v, ok}
	})
	return r.v, r.ok
}

func freeformOf(a QuestionnaireAnswer) string {
	v, _ := a.Freeform()
	return v
}

// U:questionnaire/state.rs:1262-1299
func TestQuestionnaireSchemaRejectsDuplicateNamesAndInvalidSingleDefaults(t *testing.T) {
	duplicateItems := []QuestionnaireItemDefinition{
		NewQuestionnaireItemDefinition("same", "One"),
		NewQuestionnaireItemDefinition("same", "Two"),
	}
	if err := questionnaireValidateSchema(duplicateItems); err != (QuestionnaireSchemaError{Kind: QuestionnaireDuplicateItem, Item: "same"}) {
		t.Fatalf("duplicate items: %v", err)
	}

	invalidDefault := []QuestionnaireItemDefinition{
		NewQuestionnaireItemDefinition("single", "Single").WithChoices(
			NewQuestionnaireChoiceDefinition("a", "A").WithDefaultSelected(true),
			NewQuestionnaireChoiceDefinition("b", "B").WithDefaultSelected(true),
		),
	}
	if err := questionnaireValidateSchema(invalidDefault); err != (QuestionnaireSchemaError{Kind: QuestionnaireMultipleDefaultsForSingleItem, Item: "single"}) {
		t.Fatalf("invalid default: %v", err)
	}

	duplicateChoice := []QuestionnaireItemDefinition{
		NewQuestionnaireItemDefinition("item", "Item").WithChoices(
			NewQuestionnaireChoiceDefinition("same", "One"),
			NewQuestionnaireChoiceDefinition("same", "Two"),
		),
	}
	if err := questionnaireValidateSchema(duplicateChoice); err != (QuestionnaireSchemaError{Kind: QuestionnaireDuplicateChoice, Item: "item", Choice: "same"}) {
		t.Fatalf("duplicate choice: %v", err)
	}
}

// U:questionnaire/state.rs:1301-1359
func TestQuestionnaireValidatesNavigatesSkipsAndEmitsCompletionBeforeSubmit(t *testing.T) {
	q := openQuestionnaireHarness(t)

	progress := qRead(q, func(s *QuestionnaireState, _ *gpui.Window, _ *gpui.App) QuestionnaireProgressState {
		return s.Progress()
	})
	if progress.Current() != 1 || progress.Total() != 2 {
		t.Fatalf("progress = %d/%d, want 1/2", progress.Current(), progress.Total())
	}
	status := qRead(q, func(s *QuestionnaireState, _ *gpui.Window, _ *gpui.App) QuestionnaireItemStatus {
		st, _ := s.ItemState("first")
		return st.Status()
	})
	if status != QuestionnaireItemUnanswered {
		t.Fatalf("first status = %v", status)
	}
	if qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
		return s.Submit(w, cx)
	}) {
		t.Fatal("an empty questionnaire submitted")
	}
	if _, ok := q.errorOf("first"); !ok {
		t.Fatal("first has no error")
	}
	if e, _ := q.errorOf("second"); e != QuestionnaireValidationMessage("Use the valid answer") {
		t.Fatalf("second error = %+v", e)
	}

	qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.SetInputValue("first", "draft", w, cx)
	})
	if _, ok := q.errorOf("first"); ok {
		t.Fatal("a draft answer kept the error")
	}
	qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.SetInputValue("first", "", w, cx)
	})
	if _, ok := q.errorOf("first"); !ok {
		t.Fatal("once validation has been attempted, clearing an answer updates the error live")
	}

	qUpdate(q, func(s *QuestionnaireState, _ *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.ActivateChoice("first", "a", cx)
	})
	if !qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
		return s.GoNext(w, cx)
	}) {
		t.Fatal("go_next failed")
	}
	if cur := q.currentItem(); cur != "second" {
		t.Fatalf("current = %q", cur)
	}
	if !qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
		return s.SkipCurrent(w, cx)
	}) {
		t.Fatal("skip_current failed")
	}

	if !qRead(q, func(s *QuestionnaireState, _ *gpui.Window, _ *gpui.App) bool { return s.IsComplete() }) {
		t.Fatal("not complete")
	}
	status = qRead(q, func(s *QuestionnaireState, _ *gpui.Window, _ *gpui.App) QuestionnaireItemStatus {
		st, _ := s.ItemState("second")
		return st.Status()
	})
	if status != QuestionnaireItemSkipped {
		t.Fatalf("second status = %v", status)
	}
	if got, want := q.events(), []string{"answer", "current", "answer", "completed", "submit"}; !slices.Equal(got, want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
}

// U:questionnaire/state.rs:1361-1458
func TestQuestionnaireKeepsInputDraftSeparateAndSynchronizesSilentSettersAndReset(t *testing.T) {
	q := openQuestionnaireHarness(t)
	initialEvents := len(q.events())

	qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.SetAnswer("first", NewQuestionnaireAnswer().WithFreeform("  custom  "), w, cx)
	})
	if n := len(q.events()); n != initialEvents {
		t.Fatalf("programmatic setters are silent: %d events, want %d", n, initialEvents)
	}
	if v := q.inputValue(q.first); v != "  custom  " {
		t.Fatalf("first input = %q", v)
	}
	if v := freeformOf(q.answer("first")); v != "  custom  " {
		t.Fatalf("first freeform = %q", v)
	}

	qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.SetAnswer("first", NewQuestionnaireAnswer().WithChoices("a"), w, cx)
	})
	if v := q.inputValue(q.first); v != "  custom  " {
		t.Fatalf("first input = %q", v)
	}
	if _, ok := q.answer("first").Freeform(); ok {
		t.Fatal("a choice answer kept the freeform text")
	}
	if c := q.answer("first").Choices(); len(c) == 0 || c[0] != "a" {
		t.Fatalf("first choices = %v", c)
	}

	q.cx.Update(func(w *gpui.Window, app *gpui.App) {
		q.first.Update(app, func(s *InputState, icx *gpui.Context[InputState]) { s.ReplaceAll("", w, icx) })
	})
	q.cx.RunUntilParked()
	if n := len(q.events()); n != initialEvents {
		t.Fatalf("editing an unselected draft does not change the semantic answer: %d events", n)
	}
	if c := q.answer("first").Choices(); len(c) == 0 || c[0] != "a" {
		t.Fatalf("first choices = %v", c)
	}

	qUpdate(q, func(s *QuestionnaireState, _ *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.ActivateChoice("first", "b", cx)
	})

	qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) struct{} {
		s.Reset(w, cx)
		return struct{}{}
	})
	if v := q.inputValue(q.first); v != "" {
		t.Fatalf("first input = %q after reset", v)
	}
	if v := q.inputValue(q.second); v != "initial draft" {
		t.Fatalf("second input = %q after reset", v)
	}
	if !q.answer("first").IsEmpty() {
		t.Fatal("first answer survived the reset")
	}
	if v := freeformOf(q.answer("second")); v != "initial draft" {
		t.Fatalf("second freeform = %q", v)
	}
	if n := len(q.events()); n != initialEvents+1 {
		t.Fatalf("events = %d, want %d", n, initialEvents+1)
	}
}

// U:questionnaire/state.rs:1460-1501
func TestQuestionnaireValidatesAllItemsAndReturnsToTheFirstInvalidItem(t *testing.T) {
	q := openQuestionnaireHarness(t)
	qUpdate(q, func(s *QuestionnaireState, _ *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.ActivateChoice("first", "a", cx)
	})
	submit := func() bool {
		return qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
			return s.Submit(w, cx)
		})
	}

	if submit() {
		t.Fatal("submitted with an invalid second item")
	}
	if cur := q.currentItem(); cur != "second" {
		t.Fatalf("current = %q", cur)
	}
	if e, _ := q.errorOf("second"); e != QuestionnaireValidationMessage("Use the valid answer") {
		t.Fatalf("second error = %+v", e)
	}

	qUpdate(q, func(s *QuestionnaireState, _ *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.SetExternalError("first", "Server rejected it", cx)
	})
	if submit() {
		t.Fatal("submitted with an external error")
	}
	if cur := q.currentItem(); cur != "first" {
		t.Fatalf("current = %q", cur)
	}

	qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		if err := s.ClearExternalError("first", cx); err != nil {
			return err
		}
		return s.SetInputValue("second", "valid", w, cx)
	})
	if !submit() {
		t.Fatal("a valid questionnaire did not submit")
	}
}

// U:questionnaire/state.rs:1503-1575
func TestQuestionnairePreservesSchemaOrderAndTemporarilyExcludesDisabledAnswers(t *testing.T) {
	q := openQuestionnaireHarness(t)
	choices := func() []string { return q.answer("second").Choices() }

	qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.SetAnswer("second", NewQuestionnaireAnswer().WithChoices("y", "x", "y").WithFreeform("valid"), w, cx)
	})
	if got := choices(); !slices.Equal(got, []string{"x", "y"}) {
		t.Fatalf("choices = %v", got)
	}
	moved := qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
		if err := s.SetCurrentItem("second", w, cx); err != nil {
			t.Fatal(err)
		}
		s.FocusCurrentItem(w)
		return s.FocusNextAnswer(w, cx)
	})
	if !moved {
		t.Fatal("focus_next_answer failed")
	}
	if v, ok := q.focusedChoice(); !ok || v != "x" {
		t.Fatalf("filled multiple choices are focused in schema order: %q %v", v, ok)
	}
	if got := choices(); !slices.Equal(got, []string{"x", "y"}) {
		t.Fatalf("focusing a filled choice does not toggle it: %v", got)
	}

	qUpdate(q, func(s *QuestionnaireState, _ *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.SetChoiceDisabled("second", "x", true, cx)
	})
	if got := choices(); !slices.Equal(got, []string{"y"}) {
		t.Fatalf("choices with x disabled = %v", got)
	}
	qUpdate(q, func(s *QuestionnaireState, _ *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.SetChoiceDisabled("second", "x", false, cx)
	})
	if got := choices(); !slices.Equal(got, []string{"x", "y"}) {
		t.Fatalf("choices with x re-enabled = %v", got)
	}

	err := qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.SetAnswer("second", NewQuestionnaireAnswer().WithChoices("unknown"), w, cx)
	})
	if err != (QuestionnaireSchemaError{Kind: QuestionnaireUnknownChoice, Item: "second", Choice: "unknown"}) {
		t.Fatalf("err = %v", err)
	}
}

// U:questionnaire/state.rs:1577-1702
func TestQuestionnaireShortcutsDisabledCurrentFallbackAndRecompletionAreDeterministic(t *testing.T) {
	q := openQuestionnaireHarness(t)
	firstChoice := func() string {
		c := q.answer("first").Choices()
		if len(c) == 0 {
			return ""
		}
		return c[0]
	}
	reset := func() {
		qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) struct{} {
			s.Reset(w, cx)
			return struct{}{}
		})
	}

	moved := qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
		if err := s.SetAnswer("first", NewQuestionnaireAnswer().WithChoices("b"), w, cx); err != nil {
			t.Fatal(err)
		}
		s.FocusCurrentItem(w)
		return s.FocusNextAnswer(w, cx)
	})
	if !moved {
		t.Fatal("focus_next_answer failed")
	}
	if v, ok := q.focusedChoice(); !ok || v != "b" {
		t.Fatalf("the first move from the item group focuses the existing answer: %q %v", v, ok)
	}
	if c := firstChoice(); c != "b" {
		t.Fatalf("focusing the filled radio does not replace the answer: %q", c)
	}
	reset()

	moved = qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
		s.FocusInput("first", w)
		if s.CurrentInputHasText(cx.App()) {
			t.Fatal("the empty input has text")
		}
		return s.FocusNextAnswer(w, cx)
	})
	if !moved {
		t.Fatal("focus_next_answer from the empty input failed")
	}
	if c := firstChoice(); c != "a" {
		t.Fatalf("an empty focused input may move to and activate a radio: %q", c)
	}
	reset()

	moved = qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
		if err := s.SetInputValue("first", "draft", w, cx); err != nil {
			t.Fatal(err)
		}
		s.FocusInput("first", w)
		if !s.CurrentInputHasText(cx.App()) {
			t.Fatal("the drafted input has no text")
		}
		return s.FocusNextAnswer(w, cx)
	})
	if moved {
		t.Fatal("a drafted input gave up focus")
	}
	var inputFocused bool
	q.cx.Update(func(w *gpui.Window, app *gpui.App) {
		h := q.first.Read(app, func(s *InputState, app *gpui.App) gpui.FocusHandle { return s.FocusHandle(app) })
		inputFocused = h.IsFocused(w)
	})
	if !inputFocused {
		t.Fatal("the first input lost focus")
	}
	reset()

	moved = qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
		s.FocusCurrentItem(w)
		return s.FocusNextAnswer(w, cx)
	})
	if !moved {
		t.Fatal("focus_next_answer from the item group failed")
	}
	if c := firstChoice(); c != "a" {
		t.Fatalf("moving from the item group to a radio activates it: %q", c)
	}
	reset()

	shortcut := func(value string) string {
		return qRead(q, func(s *QuestionnaireState, _ *gpui.Window, _ *gpui.App) string {
			v, _ := s.ShortcutForChoice("first", value)
			return v
		})
	}
	if sc := shortcut("a"); sc != "A" {
		t.Fatalf("shortcut(a) = %q", sc)
	}
	qUpdate(q, func(s *QuestionnaireState, _ *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.SetChoiceDisabled("first", "a", true, cx)
	})
	if sc := shortcut("b"); sc != "A" {
		t.Fatalf("shortcut(b) with a disabled = %q", sc)
	}
	qUpdate(q, func(s *QuestionnaireState, _ *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.SetChoiceDisabled("first", "a", false, cx)
	})
	qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) struct{} {
		if !s.ActivateShortcut("a", w, cx) {
			t.Fatal("activate_shortcut(a) failed")
		}
		s.FocusChoice("first", "a", w)
		if !s.MoveCurrentRadio(1, w, cx) {
			t.Fatal("move_current_radio failed")
		}
		if !s.GoNext(w, cx) {
			t.Fatal("go_next failed")
		}
		if !s.SkipCurrent(w, cx) {
			t.Fatal("skip_current failed")
		}
		return struct{}{}
	})
	isComplete := func() bool {
		return qRead(q, func(s *QuestionnaireState, _ *gpui.Window, _ *gpui.App) bool { return s.IsComplete() })
	}
	if !isComplete() {
		t.Fatal("not complete after skipping the last item")
	}

	qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		if err := s.SetInputValue("second", "valid", w, cx); err != nil {
			return err
		}
		return s.ActivateChoice("second", "x", cx)
	})
	if isComplete() {
		t.Fatal("still complete after an answer change")
	}
	if !qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) bool {
		return s.Submit(w, cx)
	}) {
		t.Fatal("resubmission failed")
	}
	events := q.events()
	completed := 0
	for _, e := range events {
		if e == "completed" {
			completed++
		}
	}
	if completed != 2 {
		t.Fatalf("completed %d times, want 2 (%v)", completed, events)
	}

	beforeDisable := len(events)
	qUpdate(q, func(s *QuestionnaireState, w *gpui.Window, cx *gpui.Context[QuestionnaireState]) error {
		return s.SetItemDisabled("second", true, w, cx)
	})
	if cur := q.currentItem(); cur != "first" {
		t.Fatalf("current = %q", cur)
	}
	if n := len(q.events()); n != beforeDisable {
		t.Fatalf("programmatic disable and fallback are silent: %d events, want %d", n, beforeDisable)
	}
}
