package base

import (
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Go-own tests: U:questionnaire/control.rs and keyboard.rs have no
// upstream tests.

// Go-own: a single-answer item yields a Radio, a multiple-answer item a
// Checkbox, and an unknown choice nothing (U:questionnaire/control.rs:24-120).
func TestQuestionnaireChoiceControlPicksTheControlBySchema(t *testing.T) {
	q := openQuestionnaireHarness(t)
	q.cx.Update(func(_ *gpui.Window, app *gpui.App) {
		if c, ok := NewQuestionnaireChoiceControl(q.state, "first", "a", "first-a", app); !ok || c.Radio == nil || c.Checkbox != nil {
			t.Fatalf("first/a = %+v %v, want a Radio", c, ok)
		}
		if c, ok := NewQuestionnaireChoiceControl(q.state, "second", "x", "second-x", app); !ok || c.Checkbox == nil || c.Radio != nil {
			t.Fatalf("second/x = %+v %v, want a Checkbox", c, ok)
		}
		if _, ok := NewQuestionnaireChoiceControl(q.state, "first", "zzz", "unknown", app); ok {
			t.Fatal("an unknown choice built a control")
		}
	})
}

// Go-own: a bare letter activates its shortcut and prevents the default;
// a modified key is ignored (U:questionnaire/keyboard.rs:5-90).
func TestQuestionnaireHandleKeyDownActivatesShortcuts(t *testing.T) {
	q := openQuestionnaireHarness(t)
	q.cx.Update(func(w *gpui.Window, app *gpui.App) {
		QuestionnaireHandleKeyDown(q.state, &gpui.KeyDownEvent{Keystroke: gpui.Keystroke{Key: "b", Modifiers: gpui.Modifiers{Control: true, Shift: true}}}, w, app)
	})
	if c := q.answer("first").Choices(); len(c) != 0 {
		t.Fatalf("a modified key activated %v", c)
	}
	var prevented bool
	q.cx.Update(func(w *gpui.Window, app *gpui.App) {
		QuestionnaireHandleKeyDown(q.state, &gpui.KeyDownEvent{Keystroke: gpui.Keystroke{Key: "b", KeyChar: "b"}}, w, app)
		prevented = w.DefaultPrevented()
	})
	if c := q.answer("first").Choices(); len(c) != 1 || c[0] != "b" {
		t.Fatalf("shortcut b selected %v", c)
	}
	if !prevented {
		t.Fatal("a handled key did not prevent the default")
	}
}
