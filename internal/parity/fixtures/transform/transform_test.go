//go:build windows

// The transform fixtures' discrimination tests (the tracer's
// tracer_test.go pattern): each fixture has a deliberately-wrong view
// that must make its comparison FAIL with a readable diff, so a green
// fixture means the thing it says.
//
//   - the hit fixture's wrong view drops the transforms entirely: the
//     clicks that must hit through the transforms miss their targets
//     and the clicks that must miss hit them — Tier B fails, the
//     transcript diff naming both directions;
//   - each box fixture's wrong view drops its transform (or, for the
//     origin fixture, uses the default centre instead of the corner):
//     the mapped box the Tier A comparison reads is the layout box —
//     a readable box diff;
//   - the transition fixture's wrong view runs the transition at
//     800ms instead of 400ms: at the 200ms sample the scale is a
//     quarter of the way (1.1875) where the record holds the midpoint
//     (1.375) — a timing bug only the sampled state catches.
package transform_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/transform"
)

// run replays the fixture with the given view and returns the record
// and the replay.
func run(t *testing.T, fixture parity.Fixture, view func(*parity.EventLog) gpui.AnyElement) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture.View = view
	record, err := parity.LoadRecord(fixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, fixture)
	if err != nil {
		t.Fatal(err)
	}
	return record, replay
}

// TestWrongHitViewFailsTheTranscript is the spec-risk discrimination:
// the no-transform view's hit test sees the layout boxes, so the
// clicks that must hit through the transforms land on the wrong
// targets and the clicks that must miss hit — Tier B fails with a
// readable transcript diff. Tier A (the translated parent's box) and
// Tier C fail too.
func TestWrongHitViewFailsTheTranscript(t *testing.T) {
	record, replay := run(t, transform.HitFixture, transform.HitNoTransformView)
	err := parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the no-transform view (the hit test on the layout boxes) passed the Tier B transcript comparison")
	}
	msg := err.Error()
	// The click at (126, 56) must miss the rotated box (it records
	// mousedown:rot; the wrong view has nothing there), and the click
	// at (115, 35) must hit it (the wrong view records mousedown:rot
	// where the record has root).
	if !strings.Contains(msg, "mousedown") {
		t.Fatalf("the transcript diff is not readable:\n%v", err)
	}
	t.Logf("Tier B fails as it should:\n%v", err)
	if err := parity.CompareBoxes(record, replay); err == nil {
		t.Error("the no-transform view passed the Tier A box comparison (the parent's translated box)")
	}
}

// TestWrongTransformsFailTheBoxComparisons checks each box fixture's
// discrimination: the no-transform view reports the LAYOUT box where
// the record holds the transformed box, so the exact Tier A comparison
// fails with a readable diff. Each subtest names its fixture's wrong
// view inline (dropping the transform, or the wrong origin).
func TestWrongTransformsFailTheBoxComparisons(t *testing.T) {
	cases := []struct {
		name     string
		fixture  parity.Fixture
		wrong    func(*parity.EventLog) gpui.AnyElement
		mustName string
	}{
		{"translate", transform.TranslateFixture, noTransformBoxView, "box"},
		{"scale", transform.ScaleFixture, noTransformBoxView, "box"},
		{"rotate", transform.RotateFixture, noTransformBoxView, "box"},
		{"skew", transform.SkewFixture, noTransformBoxView, "box"},
		{"origin", transform.OriginFixture, wrongOriginView, "corner"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			record, replay := run(t, tc.fixture, tc.wrong)
			err := parity.CompareBoxes(record, replay)
			if err == nil {
				t.Fatalf("the wrong view passed the Tier A box comparison")
			}
			if !strings.Contains(err.Error(), tc.mustName) {
				t.Fatalf("the box diff is not readable:\n%v", err)
			}
			t.Logf("Tier A fails as it should:\n%v", err)
		})
	}
}

// noTransformBoxView is the single-box fixtures' wrong view: the same
// element without its transform.
func noTransformBoxView(_ *parity.EventLog) gpui.AnyElement {
	box := gpui.Div().
		ID("box").
		DebugSelector("box").
		Absolute().Left(gpui.Px(40)).Top(gpui.Px(30)).
		Size(gpui.Px(80), gpui.Px(40)).
		Bg(gpui.Rgb(0x00aa00))
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Bg(gpui.Rgb(0xffffff)).
		Child(box.IntoElement()).
		IntoElement()
}

// wrongOriginView is the origin fixture's wrong view: the corner box
// rotates about the DEFAULT centre instead of the top-left corner.
func wrongOriginView(_ *parity.EventLog) gpui.AnyElement {
	centre := gpui.Div().
		ID("centre").
		DebugSelector("centre").
		Absolute().Left(gpui.Px(40)).Top(gpui.Px(30)).
		Size(gpui.Px(80), gpui.Px(40)).
		Bg(gpui.Rgb(0x00aa00)).
		Rotate(90)
	corner := gpui.Div().
		ID("corner").
		DebugSelector("corner").
		Absolute().Left(gpui.Px(40)).Top(gpui.Px(30)).
		Size(gpui.Px(80), gpui.Px(40)).
		Bg(gpui.Rgb(0x0066ff)).
		Rotate(90)
	return gpui.Div().
		ID("root").
		DebugSelector("root").
		SizeFull().
		Bg(gpui.Rgb(0xffffff)).
		Child(centre.IntoElement()).
		Child(corner.IntoElement()).
		IntoElement()
}

// TestWrongTransitionDurationFailsTheBoxComparison checks the
// transition fixture's discrimination at the sampled instant: the
// 800ms-linear wrong view holds the scale at 1.1875 where the record
// (and the correct view) holds 1.375, so the exact Tier A box
// comparison fails. Tier B is untouched.
func TestWrongTransitionDurationFailsTheBoxComparison(t *testing.T) {
	record, replay := run(t, transform.TransitionFixture, transform.WrongTransitionDurationView)
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the 800ms-linear view (the scale a quarter of the way at the sample) passed the Tier A box comparison")
	}
	if !strings.Contains(err.Error(), "scale") || !strings.Contains(err.Error(), "55") {
		t.Fatalf("the box diff is not readable:\n%v", err)
	}
	t.Logf("Tier A fails as it should:\n%v", err)
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Errorf("the wrong duration must not change the transcript: %v", err)
	}
}
