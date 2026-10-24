//go:build windows

// The user-select fixtures' comparison test (ticket 20's Tier B-like
// check): the recorded page's window.getSelection() against the
// replayed model's window selection, and the cursor resolution the
// screenshot cannot see — the replay's WindowCursorStyle against the
// page's computed cursor (CSS auto; the resolution is the rule the unit
// tests pin). The deliberately-wrong view is the none-opt-out on the
// DEFAULT fixture: the record selects a range, the wrong view selects
// nothing, so the selection comparison and Tier C both fail while the
// boxes still match.
package userselect_test

import (
	"strconv"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/userselect"
)

// recordedSelection is the Chrome record's read (the fixture's
// ComputedScript result).
type recordedSelection struct {
	anchor, focus int
	text          string
	anchorNode    string
	focusNode     string
	userSelect    string
	cursor        string
}

func loadRecordedSelection(t *testing.T, fixture parity.Fixture) recordedSelection {
	t.Helper()
	record, err := parity.LoadRecord(fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	if len(record.Computed) == 0 {
		t.Fatalf("the record carries no selection (re-record the fixture: go run ./cmd/parityrecord -fixture %s)", fixture.Name)
	}
	atoi := func(key string) int {
		value, ok := record.Computed[key]
		if !ok {
			t.Fatalf("the record's selection has no %q", key)
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			t.Fatalf("the record's selection %s %q: %v", key, value, err)
		}
		return n
	}
	text, _ := record.Computed["text"]
	anchorNode, _ := record.Computed["anchorNode"]
	focusNode, _ := record.Computed["focusNode"]
	userSelect, _ := record.Computed["userSelect"]
	cursor, _ := record.Computed["cursor"]
	return recordedSelection{
		anchor:     atoi("anchor"),
		focus:      atoi("focus"),
		text:       text,
		anchorNode: anchorNode,
		focusNode:  focusNode,
		userSelect: userSelect,
		cursor:     cursor,
	}
}

// TestFixturesMatchChromesSelection replays both fixtures and compares
// the recorded Chrome selection with the model's window selection: the
// selected text (the anchor/focus pair normalized to a range is the
// same single run on both sides), that both endpoints sit in the
// paragraph's text node, and the cursor resolution — the I-beam over
// the selectable text, the arrow over the none subtree (the record
// reads the CSS value, auto; the resolution is Chromium's
// SelectAutoCursor rule, event_handler.cc:839-847).
func TestFixturesMatchChromesSelection(t *testing.T) {
	for _, fixture := range []parity.Fixture{userselect.Fixture, userselect.NoneFixture} {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			want := loadRecordedSelection(t, fixture)
			// The none fixture's press on non-selectable content leaves
			// Chrome's selection at the body (the harness normalizes the
			// body to ""): the endpoints sit outside the paragraph's text
			// node exactly when nothing was selected there.
			if want.userSelect != "none" {
				if want.anchorNode != "text" || want.focusNode != "text" {
					t.Fatalf("the recorded selection left the paragraph's text node: %q, %q", want.anchorNode, want.focusNode)
				}
			}
			if want.cursor != "auto" {
				t.Fatalf("the recorded paragraph's computed cursor is %q, want auto (the fixtures pin the default)", want.cursor)
			}
			replay, err := parity.Replay(t, fixture)
			if err != nil {
				t.Fatal(err)
			}
			got := userselect.LastCapture()
			if got == nil {
				t.Fatal("the replay produced no selection capture")
			}
			if got.Text != want.text {
				t.Fatalf("selected text %q, want the record's %q", got.Text, want.text)
			}
			if want.userSelect == "none" {
				// The opt-out: nothing selected, the arrow over the text.
				if got.Text != "" {
					t.Fatalf("the none subtree selected %q", got.Text)
				}
				if got.Cursor != "arrow" {
					t.Fatalf("cursor over the select-none text = %q, want the arrow (HitOnUserSelectNoneDoesNotShowIBeam, event_handler_test.cc:489)", got.Cursor)
				}
			} else {
				if got.Cursor != "ibeam" {
					t.Fatalf("cursor over the selectable text = %q, want the I-beam (SelectAutoCursor, event_handler.cc:839-847)", got.Cursor)
				}
			}
			_ = replay
		})
	}
}

// TestWrongViewSelectNoneFailsTheSelectionComparison is the
// deliberately-wrong view (the ticket's one): the default fixture's view
// with user-select: none on the paragraph — the record selected a
// range, the wrong view selects nothing. The selection comparison fails
// and Tier C fails with it (the wash is missing); Tier A (the boxes are
// the same tree) still passes.
func TestWrongViewSelectNoneFailsTheSelectionComparison(t *testing.T) {
	fixture := userselect.Fixture
	fixture.View = func(_ *parity.EventLog) gpui.AnyElement { return userselect.NoneView() }
	record, err := parity.LoadRecord(fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	if got := userselect.LastCapture(); got == nil || got.Text != "" {
		t.Fatal("the wrong view selected text under user-select: none")
	}
	if want := loadRecordedSelection(t, userselect.Fixture); want.text == "" {
		t.Fatal("the record selected nothing: the wrong-view test needs a selected record")
	} else {
		t.Logf("the record selected %q; the wrong view selected nothing", want.text)
	}
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the wrong view must not change the boxes: %v", err)
	}
	if err := parity.ComparePixels(record, replay, fixture.Tolerance); err == nil {
		t.Fatal("the wrong view passed the Tier C pixel comparison: the missing selection wash is hidden by the tolerance")
	} else {
		t.Logf("Tier C fails as it should:\n%v", err)
	}
}
