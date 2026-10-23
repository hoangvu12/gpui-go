//go:build windows

// The keyorder fixtures' discrimination tests (the tracer_test.go
// pattern): every fixture must FAIL its comparison when the behaviour
// it claims is absent, with a readable diff — so a passing fixture
// means the thing it says. The binding-order pair also pins the two
// sides of spec D5: the GPUI shape (bindings before listeners, the
// binding consumes the key) and the browser order (listeners first,
// then the binding).
package keyorder_test

import (
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
	"github.com/hoangvu12/gpui-go/internal/parity/fixtures/keyorder"
)

// viewFn is the fixture view signature (parity.Fixture.ViewWithWindow).
type viewFn = func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement

// runWithView replays one fixture's record against a variant view.
func runWithView(t *testing.T, fixture parity.Fixture, view viewFn) (*parity.Record, *parity.ReplayResult) {
	t.Helper()
	fixture.ViewWithWindow = view
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

// TestTabPreventedFailsWhenTheListenerDoesNotPrevent: replaying the
// prevented record against the plain (non-preventing) view must fail
// Tier B (the keydown entries lose their default-prevented state) and
// Tier C (focus moved: the pixels show b focused), while the boxes are
// unchanged.
func TestTabPreventedFailsWhenTheListenerDoesNotPrevent(t *testing.T) {
	record, replay := runWithView(t, keyorder.PreventedFixture, keyorder.TabView(false))
	err := parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the non-preventing view passed the Tier B transcript against the prevented record")
	}
	if !strings.Contains(err.Error(), "defaultPrevented") {
		t.Fatalf("the transcript diff does not name the prevented state:\n%v", err)
	}
	t.Logf("Tier B fails as it should:\n%v", err)
	if err := parity.ComparePixels(record, replay, keyorder.PreventedFixture.Tolerance); err == nil {
		t.Fatal("the non-preventing view passed Tier C (the tab must have moved focus to b)")
	} else {
		t.Logf("Tier C fails as it should:\n%v", err)
	}
	if err := parity.CompareBoxes(record, replay); err != nil {
		t.Errorf("a focus move must not change any box: %v", err)
	}
}

// TestScrollPreventedFailsWhenTheListenerDoesNotPrevent: the
// non-preventing view scrolls, so Tier A (the content's box) and Tier C
// fail against the prevented record.
func TestScrollPreventedFailsWhenTheListenerDoesNotPrevent(t *testing.T) {
	record, replay := runWithView(t, keyorder.ScrollPreventedFixture, keyorder.ScrollView(false))
	err := parity.CompareBoxes(record, replay)
	if err == nil {
		t.Fatal("the non-preventing view passed Tier A (the arrowdown must have scrolled the content)")
	}
	if !strings.Contains(err.Error(), "content: recorded") {
		t.Fatalf("the box diff does not name the content element:\n%v", err)
	}
	t.Logf("Tier A fails as it should:\n%v", err)
	if err := parity.ComparePixels(record, replay, keyorder.ScrollPreventedFixture.Tolerance); err == nil {
		t.Fatal("the non-preventing view passed Tier C (the scrolled content is visible)")
	}
}

// TestActivatePreventedFailsWhenTheListenerDoesNotPrevent: the
// non-preventing view clicks, so Tier B fails with the extra click
// entry; the view renders identically (the same focus), so Tier C
// still passes.
func TestActivatePreventedFailsWhenTheListenerDoesNotPrevent(t *testing.T) {
	record, replay := runWithView(t, keyorder.ActivatePreventedFixture, keyorder.ActivateView(false))
	err := parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the non-preventing view passed Tier B (the space keyup must have clicked)")
	}
	if !strings.Contains(err.Error(), "click:btn") {
		t.Fatalf("the transcript diff does not name the extra click:\n%v", err)
	}
	t.Logf("Tier B fails as it should:\n%v", err)
	if err := parity.ComparePixels(record, replay, keyorder.ActivatePreventedFixture.Tolerance); err != nil {
		t.Errorf("a suppressed activation click must not change any pixel: %v", err)
	}
}

// TestBindingOrderGPUIShapeConsumesTheKey: with the window's dispatch
// order left at the GPUI shape, the bound action runs before the key
// listeners and stops propagation, so the keydown listeners never see
// the key — the record's key entries are missing from the transcript.
// This is the pin's bindings-before-listeners shape, preserved by D5.
func TestBindingOrderGPUIShapeConsumesTheKey(t *testing.T) {
	keyorder.BindingOrderActionsFired() // reset
	record, replay := runWithView(t, keyorder.BindingOrderFixture, keyorder.BindingOrderView(false))
	err := parity.CompareEvents(record, replay)
	if err == nil {
		t.Fatal("the GPUI-shape view passed Tier B against the browser-order record (the binding must consume the keydown)")
	}
	if !strings.Contains(err.Error(), "keydown:a") {
		t.Fatalf("the transcript diff does not name the lost keydown entries:\n%v", err)
	}
	t.Logf("Tier B fails as it should under the GPUI shape:\n%v", err)
	// The binding really fired (the view is not vacuous), and the
	// rendered pixels are unchanged (no focus move either way).
	if fired := keyorder.BindingOrderActionsFired(); fired == 0 {
		t.Fatal("the bound action never fired under the GPUI shape")
	} else {
		t.Logf("the bound action fired %d time(s) before the listeners", fired)
	}
	if err := parity.ComparePixels(record, replay, keyorder.BindingOrderFixture.Tolerance); err != nil {
		t.Errorf("the dispatch order must not change any pixel: %v", err)
	}
}

// TestBindingOrderBrowserOrderRunsListenersFirst: the fixture's own
// view (browser order) matches its record — also asserted by
// TestFixtures — and its bound action fires exactly once, after the
// keydown listeners (the record's keydown entries are present, which
// is only possible if the listeners ran before the binding consumed
// the key).
func TestBindingOrderBrowserOrderRunsListenersFirst(t *testing.T) {
	keyorder.BindingOrderActionsFired() // reset
	record, err := parity.LoadRecord(keyorder.BindingOrderFixture.Name)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := parity.Replay(t, keyorder.BindingOrderFixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := parity.CompareEvents(record, replay); err != nil {
		t.Fatalf("the browser-order view must match its record: %v", err)
	}
	if fired := keyorder.BindingOrderActionsFired(); fired != 1 {
		t.Fatalf("the bound action fired %d time(s), want 1 (after the keydown listeners, before the keyup)", fired)
	}
}
