//go:build windows

// The browser-core ticket 77 (rev 15.1) text-wrap gate: the run
// record's `text-wrap: nowrap` tag now drives shaping behaviour — the
// native service suppresses the soft-wrap constraint (the one rev-15
// browser text field the pinned TextLayoutRequest can express through
// wrap_width: Option; see TEXT_ABI.md). The other tags (balance,
// pretty, stable) are Go-side bisection algorithms by design, and
// word-break/overflow-wrap/locale/width/variations/opsz/justify stay
// blocked behind the pinned adapter's private Parley state.
package textspec

import (
	"testing"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// nowrapRun returns one plain run with the given text-wrap tag.
func nowrapRun(text string, textWrap uint32) native.TextRunSpec {
	return native.TextRunSpec{
		Len:      len(text),
		Family:   "Segoe UI",
		Weight:   400,
		TextWrap: textWrap,
	}
}

// lineCountOf reads one handle's visual row count.
func lineCountOf(t *testing.T, svc *native.TextService, handle native.ShapingID) int {
	t.Helper()
	count, err := svc.LineCount(handle)
	if err != nil {
		t.Fatalf("LineCount: %v", err)
	}
	return count
}

// TestTextWrapNoWrapSuppressesSoftWrap pins the wired behaviour: with
// the nowrap tag, a narrow wrap width produces no soft rows — the text
// stays on one line, the layout summary reports no active wrap
// constraint (the tag suppressed it), and run_info still echoes the
// tag. Without the tag the same request wraps (the control).
func TestTextWrapNoWrapSuppressesSoftWrap(t *testing.T) {
	svc := textService(t)
	const text = "The quick brown fox"
	wrap := float32(70)

	// Control: the recorded narrow wrap splits this text into 3 rows.
	control, err := svc.Shape(native.ShapeRequest{
		Text:      text,
		FontSize:  16,
		Runs:      []native.TextRunSpec{nowrapRun(text, native.TextTextWrapWrap)},
		WrapWidth: &wrap,
	})
	if err != nil {
		t.Fatalf("Shape (control): %v", err)
	}
	t.Cleanup(func() { _ = svc.Dispose(control) })
	if got := lineCountOf(t, svc, control); got != 3 {
		t.Fatalf("control line count = %d, want 3 (the recorded wrap-narrow rows)", got)
	}
	controlLayout, err := svc.LayoutInfo(control)
	if err != nil {
		t.Fatalf("LayoutInfo (control): %v", err)
	}
	if controlLayout.WrapWidth == nil || *controlLayout.WrapWidth != wrap {
		t.Errorf("control wrap = %+v, want %v", controlLayout.WrapWidth, wrap)
	}

	// nowrap: the same narrow wrap width, one row.
	handle, err := svc.Shape(native.ShapeRequest{
		Text:      text,
		FontSize:  16,
		Runs:      []native.TextRunSpec{nowrapRun(text, native.TextTextWrapNoWrap)},
		WrapWidth: &wrap,
	})
	if err != nil {
		t.Fatalf("Shape (nowrap): %v", err)
	}
	t.Cleanup(func() { _ = svc.Dispose(handle) })
	if got := lineCountOf(t, svc, handle); got != 1 {
		t.Fatalf("nowrap line count = %d, want 1 (the tag suppressed soft wrapping)", got)
	}
	layout, err := svc.LayoutInfo(handle)
	if err != nil {
		t.Fatalf("LayoutInfo (nowrap): %v", err)
	}
	if layout.WrapWidth != nil {
		t.Errorf("nowrap layout wrap = %+v, want nil (the active constraint was suppressed)", layout.WrapWidth)
	}
	record, err := svc.RunInfo(handle, 0)
	if err != nil {
		t.Fatalf("RunInfo (nowrap): %v", err)
	}
	if record.TextWrap != native.TextTextWrapNoWrap {
		t.Errorf("run_info text_wrap = %d, want nowrap", record.TextWrap)
	}

	// Relayout under a fresh narrow wrap constraint stays one row: the
	// tag governs re-shaping too, not just the initial request.
	if err := svc.Relayout(handle, &wrap, nil); err != nil {
		t.Fatalf("Relayout (nowrap): %v", err)
	}
	if got := lineCountOf(t, svc, handle); got != 1 {
		t.Fatalf("nowrap relayout line count = %d, want 1", got)
	}
	layout, err = svc.LayoutInfo(handle)
	if err != nil {
		t.Fatalf("LayoutInfo (nowrap relayout): %v", err)
	}
	if layout.WrapWidth != nil {
		t.Errorf("nowrap relayout wrap = %+v, want nil", layout.WrapWidth)
	}

	// Balance/pretty/stable do NOT suppress the wrap (they are Go-side
	// algorithms over the wrapped layout; the tag only crosses the ABI).
	for name, tag := range map[string]uint32{
		"balance": native.TextTextWrapBalance,
		"pretty":  native.TextTextWrapPretty,
		"stable":  native.TextTextWrapStable,
	} {
		tagged, err := svc.Shape(native.ShapeRequest{
			Text:      text,
			FontSize:  16,
			Runs:      []native.TextRunSpec{nowrapRun(text, tag)},
			WrapWidth: &wrap,
		})
		if err != nil {
			t.Fatalf("Shape (%s): %v", name, err)
		}
		t.Cleanup(func() { _ = svc.Dispose(tagged) })
		if got := lineCountOf(t, svc, tagged); got != 3 {
			t.Errorf("%s line count = %d, want 3 (the tag does not suppress the wrap)", name, got)
		}
	}
}

// TestTextWrapNoWrapKeepsHardBreaks: nowrap suppresses SOFT wrapping
// only — explicit newlines still produce their rows, and the line
// clamp still applies to them (CSS white-space/text-wrap never removes
// hard breaks).
func TestTextWrapNoWrapKeepsHardBreaks(t *testing.T) {
	svc := textService(t)
	const text = "one\ntwo"
	wrap := float32(20)

	handle, err := svc.Shape(native.ShapeRequest{
		Text:      text,
		FontSize:  16,
		Runs:      []native.TextRunSpec{nowrapRun(text, native.TextTextWrapNoWrap)},
		WrapWidth: &wrap,
	})
	if err != nil {
		t.Fatalf("Shape: %v", err)
	}
	t.Cleanup(func() { _ = svc.Dispose(handle) })
	if got := lineCountOf(t, svc, handle); got != 2 {
		t.Fatalf("nowrap hard-broken line count = %d, want 2 (hard breaks survive)", got)
	}

	// The clamp keeps its echo and follows the PIN's semantics: a
	// clamp collapses SOFT wrapping past the limit (break_remaining at
	// the maximum advance — the wrapped case's last row holds every
	// remaining cluster), but a mandatory hard break still ends its row,
	// so the clamped hard-broken text stays 2 rows (the pinned
	// layout_text does not remove hard-broken rows either — the clamp
	// is a row budget for soft wrapping, not a display:-webkit-box
	// line clamp).
	clamp := uint32(1)
	clamped, err := svc.Shape(native.ShapeRequest{
		Text:      text,
		FontSize:  16,
		Runs:      []native.TextRunSpec{nowrapRun(text, native.TextTextWrapNoWrap)},
		WrapWidth: &wrap,
		LineClamp: &clamp,
	})
	if err != nil {
		t.Fatalf("Shape (clamped): %v", err)
	}
	t.Cleanup(func() { _ = svc.Dispose(clamped) })
	if got := lineCountOf(t, svc, clamped); got != 2 {
		t.Fatalf("nowrap clamped line count = %d, want 2 (the pinned clamp collapses soft wrapping only; the hard break keeps its row)", got)
	}
	layout, err := svc.LayoutInfo(clamped)
	if err != nil {
		t.Fatalf("LayoutInfo (clamped): %v", err)
	}
	if layout.LineClamp == nil || *layout.LineClamp != 1 {
		t.Errorf("clamped layout clamp = %+v, want 1", layout.LineClamp)
	}
}
