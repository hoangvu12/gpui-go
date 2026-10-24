//go:build windows

// The selection fixtures' comparison test (ticket 19's Tier B-like check)
// and the deliberately-wrong view (the one wrong-view test the ticket
// allows): the recorded page's window.getSelection() offsets against the
// replayed core model's projected source ranges, per fixture; the wrong
// view mounts no TextSelectionLayer, so nothing is ever selected — the
// range comparison and Tier C (the highlight pixels) fail while Tier B
// (no instrumented events) still passes.
package selection_test

import (
	"strconv"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/selection"
)

// recordedSelection is the Chrome record's selection read (the fixture's
// ComputedScript result).
type recordedSelection struct {
	anchor, focus int
	text          string
	anchorNode    string
	focusNode     string
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
	return recordedSelection{
		anchor:     atoi("anchor"),
		focus:      atoi("focus"),
		text:       text,
		anchorNode: anchorNode,
		focusNode:  focusNode,
	}
}

// TestFixturesMatchChromesSelectionRanges replays every selection
// fixture and compares the recorded Chrome selection with the core
// model's projection: the anchor/focus pair normalized to a range (the
// DOM reports the endpoints in gesture order — a backwards drag reports
// anchor > focus), the selected text, and that both endpoints sit in the
// paragraph's text node.
func TestFixturesMatchChromesSelectionRanges(t *testing.T) {
	for _, fixture := range []parity.Fixture{selection.Fixture, selection.WordFixture, selection.LineFixture, selection.WordDragFixture} {
		fixture := fixture
		t.Run(fixture.Name, func(t *testing.T) {
			want := loadRecordedSelection(t, fixture)
			if want.anchorNode != "text" || want.focusNode != "text" {
				t.Fatalf("the recorded selection left the paragraph's text node: %q, %q", want.anchorNode, want.focusNode)
			}
			replay, err := parity.Replay(t, fixture)
			if err != nil {
				t.Fatal(err)
			}
			got, ok := selection.LastCapture().Range()
			if !ok {
				t.Fatalf("nothing was selected: want %q", want.text)
			}
			lo, hi := min(want.anchor, want.focus), max(want.anchor, want.focus)
			if got.Start != lo || got.End != hi {
				t.Fatalf("selected range [%d, %d), want [%d, %d) (%q)", got.Start, got.End, lo, hi, want.text)
			}
			if captureText := selection.LastCapture().Text; captureText != want.text {
				t.Fatalf("selected text %q, want %q", captureText, want.text)
			}
			_ = replay
		})
	}
}

// TestWrongViewWithoutTheLayerFailsTheSelectionComparison is the
// deliberately-wrong view (the ticket's one): the view renders the same
// selectable text but opts the text out with user-select: none
// (.SelectNone(), the new opt-out since browser-core 20 made selection
// default-on — the old form, dropping the TextSelectionLayer, no longer
// defeats the model because every window installs the gestures itself),
// so nothing is ever selected — no range, no highlight. The range
// comparison fails (nothing selected where the record selected a range)
// and Tier C fails with it (the highlight is missing); Tier A (the boxes
// are the same tree) and Tier B (no instrumented events) still pass,
// which is what makes the failure mean the selection.
func TestWrongViewWithoutTheLayerFailsTheSelectionComparison(t *testing.T) {
	fixture := selection.WordFixture
	fixture.ViewWithWindow = wrongViewWithoutLayer
	record, err := parity.LoadRecord(fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}

	// The selection comparison fails: nothing was selected.
	if _, ok := selection.LastCapture().Range(); ok {
		t.Fatal("the wrong view selected a range despite user-select: none")
	}
	if want := loadRecordedSelection(t, fixture); want.text != "" {
		t.Logf("the record selected %q; the wrong view selected nothing", want.text)
	}

	// The boxes are the same tree: Tier A passes.
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("the wrong view must not change the boxes: %v", err)
	}
	// No instrumented events: Tier B passes.
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("the wrong view must not change the event transcript: %v", err)
	}
	// The missing highlight is a pixel failure.
	if err := parity.ComparePixels(record, replay, fixture.Tolerance); err == nil {
		t.Fatal("the wrong view passed the Tier C pixel comparison: the missing selection wash is hidden by the tolerance")
	} else {
		t.Logf("Tier C fails as it should:\n%v", err)
	}
}

// wrongViewWithoutLayer renders the word fixture's tree with the text
// opted out of selection (user-select: none).
func wrongViewWithoutLayer(_ *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	handle, ok := wrongHandles[w]
	if !ok {
		handle = gpui.NewTextSelectionHandle("one latte, three", app, w.Scope())
		wrongHandles[w] = handle
	}
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Flex().
		AlignItems(gpui.AlignItemsCenter).
		Justify(gpui.AlignContentCenter).
		Bg(gpui.Rgb(0xffffff)).
		TextSize(gpui.Px(16)).
		LineHeight(gpui.Px(25)).
		TextColor(gpui.Rgb(0xdc2626)).
		Child(gpui.Div().
			ID("text").
			DebugSelector("text").
			W(gpui.Px(380)).
			SelectNone().
			Child(gpui.SelectableTextWithHandle("text", handle, "one latte, three"))).
		IntoElement()
}

var wrongHandles = map[*gpui.Window]gpui.TextSelectionHandle{}
