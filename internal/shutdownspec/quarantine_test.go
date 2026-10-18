//go:build windows

// The exactly-once and honest-quarantine half of ticket30's real
// corpus: the renderer close guards that surround the native
// ERR_QUARANTINE path (retirement refused because the submission failed
// without confirmed device loss — renderer.rs submission_retire_body's
// Failed arm), and the abandonment record Close leaves when the caller
// does not drain.
//
// Honest reachability record: the native ERR_QUARANTINE itself needs a
// submission whose poll FAILED (a negative GetData HRESULT) while the
// device is NOT confirmed removed — real driver removal, which cannot
// be forced deterministically on this hardware, and the shipped
// service has no fake-failure injection (the rendererspec header
// records the same boundary; the devicelossspec suite drives the
// confirmed-loss side through the native test injection, whose failed
// submissions retire through the ABORTED_LOSS terminal instead). What
// this file pins are the guards the quarantine semantics hang on:
// acceptance is not completion, the close ordering refuses to detach
// undrained work, Close records abandonment as an explicit DROPPED
// ledger event (never as a retirement), and none of it moves with GC
// or elapsed time.
package shutdownspec

import (
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/native"
)

// TestRendererCloseGuardsAndAbandonmentRecord pins the close-side
// exactly-once guards and the honest abandonment record:
//
//   - Retire of a still-pending submission is refused (acceptance is
//     not completion — the Go-side guard ahead of the native
//     pending/quarantine refusals).
//   - Surface destroy with an outstanding submission is refused by the
//     native ERR_PENDING close guard (the close ordering: drain, then
//     detach).
//   - Renderer.Close with the undrained submission reports that refusal
//     and records the submission as DROPPED — the explicit abandonment
//     event (the ledger stays honest: the handle was abandoned without
//     retirement; it is NOT counted as retired or quarantined).
//   - The dropped record is state: GC and elapsed time do not change
//     it, and the closed renderer refuses new work through its own
//     gate (the second Close is the already-closed refusal).
func TestRendererCloseGuardsAndAbandonmentRecord(t *testing.T) {
	f := bootRealApp(t)
	window := f.openShown(t, "shutdownspec quarantine", 280, 200, nil)

	renderer, err := gpui.NewRendererManual()
	if err != nil {
		t.Fatalf("gpui.NewRendererManual: %v", err)
	}
	surface, err := renderer.GetOrCreateSurface(window)
	if err != nil {
		t.Fatalf("GetOrCreateSurface: %v", err)
	}
	sub, err := surface.PresentClear(gpui.Color{R: 0.4, G: 0.4, B: 0.4, A: 1})
	if err != nil {
		t.Fatalf("PresentClear: %v", err)
	}

	// Acceptance is not completion: the retire of a still-pending
	// submission is refused before any native retire (the Go-side
	// guard; the native side would answer ERR_PENDING for the same
	// state, and ERR_QUARANTINE for an unconfirmed failure).
	if _, err := sub.Retire(); !errors.Is(err, gpui.ErrRendererRetireNotCompleted) {
		t.Fatalf("Retire while pending = %v, want ErrRendererRetireNotCompleted", err)
	}

	// The close ordering: the surface refuses to detach while un-retired
	// submissions remain (the native ERR_PENDING guard — the
	// quarantine-family refusal that keeps the record tracked instead
	// of silently freeing).
	if err := surface.Destroy(); !errors.Is(err, native.ErrRendererPending) {
		t.Fatalf("Destroy with an outstanding submission = %v, want the native ERR_PENDING refusal", err)
	}

	// Neither GC nor elapsed time retires the submission: the state is
	// the record (still tracked, still in flight).
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	if entry, ok := renderer.LedgerEntry(sub.ID()); !ok || entry.State != "pending" {
		t.Fatalf("the submission's entry after GC and elapsed time = %+v ok=%v, want still pending", entry, ok)
	}
	if got := renderer.InFlight(); got != 1 {
		t.Fatalf("in-flight after GC and elapsed time = %d, want 1", got)
	}

	// Close with the undrained submission: the typed refusal surfaces
	// and the abandonment is the explicit DROPPED record.
	closeErr := renderer.Close()
	if !errors.Is(closeErr, native.ErrRendererPending) {
		t.Fatalf("renderer.Close() with an undrained submission = %v, want the ERR_PENDING refusal surfaced", closeErr)
	}
	entry, ok := renderer.LedgerEntry(sub.ID())
	if !ok || entry.State != "dropped" {
		t.Fatalf("the undrained submission's entry after Close = %+v ok=%v, want state dropped (the honest abandonment record)", entry, ok)
	}
	events, accepted, retired, quarantined, inFlight := renderer.Ledger()
	dropped := 0
	for _, event := range events {
		if event.Kind == gpui.LedgerDropped && event.Submission == sub.ID() {
			dropped++
		}
	}
	if dropped != 1 {
		t.Fatalf("LedgerDropped events for the abandoned submission = %d, want 1 (exactly-once abandonment record)", dropped)
	}
	// The abandonment is not a retirement and not a quarantine: the
	// counters stay honest (accepted 1; nothing retired; nothing
	// quarantined; nothing in flight).
	if accepted != 1 || retired != 0 || quarantined != 0 || inFlight != 0 {
		t.Fatalf("ledger counters after the abandonment: accepted=%d retired=%d quarantined=%d in-flight=%d, want 1/0/0/0", accepted, retired, quarantined, inFlight)
	}

	// The dropped record is terminal state: it does not move with GC or
	// time, and the closed renderer refuses new work through its own
	// gate (the exactly-once close guard).
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	if entry, ok := renderer.LedgerEntry(sub.ID()); !ok || entry.State != "dropped" {
		t.Fatalf("the dropped record moved after GC and elapsed time: %+v ok=%v", entry, ok)
	}
	if err := renderer.Close(); !isRendererClosedError(err) {
		t.Fatalf("the second renderer.Close() = %v, want the already-closed refusal", err)
	}
	if _, err := renderer.PresentClear(window, gpui.Color{}); err == nil {
		t.Fatal("PresentClear through the closed renderer succeeded, want the closed refusal")
	} else if !isRendererClosedRefusal(err) {
		t.Fatalf("PresentClear through the closed renderer = %v, want the closed refusal", err)
	}

	// The honest quarantine record: the native ERR_QUARANTINE (a failed
	// poll without confirmed loss) needs real driver removal, which
	// this machine cannot force deterministically; the confirmed-loss
	// release path it guards against is pinned by the devicelossspec
	// suite (the injected loss's failed submissions retire through the
	// ABORTED_LOSS terminal). Recorded here — a failed row, not a
	// passed quarantine behavior.
	t.Log("quarantine record: the native ERR_QUARANTINE path (failed-without-confirmed-loss retire refusal) is not forceable on healthy hardware with the shipped artifact; the surrounding guards above are the verified slice")
}

// isRendererClosedRefusal reports the renderer's closed-window/work
// refusals after Close.
func isRendererClosedRefusal(err error) bool {
	return err != nil && (err.Error() == "gpui: renderer is closed" ||
		err.Error() == "gpui: PresentClear window 1: gpui: renderer is closed")
}
