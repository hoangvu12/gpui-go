package gpui

// This file is the port's debug frame overlay (ticket27), a direct port
// of crates/gpui/src/debug_overlay.rs (429 lines) of the pinned
// GPUI-CE reference 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a: the
// overlay that paints frame-time statistics directly into the scene,
// bypassing layout, text and view invalidation entirely (to avoid
// infinitely triggering new frames).
//
// Reference integration points:
//
//   - window.rs:1334 — the Window owns one DebugFrameOverlay;
//   - window.rs:3187-3197 (draw, cfg(feature = "profiler")) — after
//     draw_roots, paint the overlay into the frame under construction
//     with the viewport size and scale factor;
//   - window.rs:3291-3298 (draw, cfg(feature = "profiler")) — after
//     the draw completes, record the draw duration
//     (window_profiler.end_draw) as one frame sample;
//   - window.rs:3366-3395 (cfg(feature = "profiler")) — the public
//     mode API: debug_frame_overlay_mode / set_debug_frame_overlay_mode
//     (+ refresh) / cycle_debug_frame_overlay_mode /
//     reset_debug_frame_overlay_stats;
//   - debug_overlay.rs itself carries the mode enum (Hidden → Minimal
//     → Full via next()), the 1000-sample percentile window, the exact
//     readout lines and the 5x7 bitmap glyph painter.
//
// Capability mapping: every profiler-gated call site checks
// CapFrameOverlay (the explicit switch documented in inspector.go); a
// release-profile run never paints or records, exactly like the
// cfg'd-out reference branches. The MODE TYPE and its cycle are
// compiled unconditionally like the pin's public enum.

import (
	"fmt"
	"slices"
	"time"
)

// DebugFrameOverlayMode is the overlay's mode (debug_overlay.rs
// DebugFrameOverlayMode): hidden, the current frame time only, or the
// full percentile readout.
type DebugFrameOverlayMode uint8

const (
	// DebugOverlayHidden is the default: no overlay.
	DebugOverlayHidden DebugFrameOverlayMode = iota
	// DebugOverlayMinimal shows only the current frame time.
	DebugOverlayMinimal
	// DebugOverlayFull shows the percentile readout.
	DebugOverlayFull
)

// String renders the mode.
func (m DebugFrameOverlayMode) String() string {
	switch m {
	case DebugOverlayMinimal:
		return "Minimal"
	case DebugOverlayFull:
		return "Full"
	default:
		return "Hidden"
	}
}

// Next returns the next mode in the Hidden → Minimal → Full cycle
// (debug_overlay.rs DebugFrameOverlayMode::next).
func (m DebugFrameOverlayMode) Next() DebugFrameOverlayMode {
	switch m {
	case DebugOverlayHidden:
		return DebugOverlayMinimal
	case DebugOverlayMinimal:
		return DebugOverlayFull
	default:
		return DebugOverlayHidden
	}
}

// The overlay's layout constants (debug_overlay.rs MAX_SAMPLES and the
// glyph metrics).
const (
	// debugOverlayMaxSamples is the number of most recent draw
	// durations retained for percentile statistics.
	debugOverlayMaxSamples = 1000

	debugOverlayGlyphWidth  = 5
	debugOverlayGlyphHeight = 7
	// debugOverlayCharAdvance is the glyph advance in font cells
	// (glyph width + 1).
	debugOverlayCharAdvance = float32(debugOverlayGlyphWidth + 1)
	// debugOverlayLineAdvance is the line advance in font cells
	// (glyph height + 2).
	debugOverlayLineAdvance = float32(debugOverlayGlyphHeight + 2)
	// debugOverlayPanelPadding is the padding between the panel edge
	// and the text, in font cells.
	debugOverlayPanelPadding = 2.0
	// debugOverlayPanelMargin is the margin between the panel and the
	// window corner, in font cells.
	debugOverlayPanelMargin = 4.0
	// debugOverlayCellSize is the side of one square font cell, in
	// logical pixels.
	debugOverlayCellSize = 2.0
)

// debugFrameOverlay is the per-window overlay state (debug_overlay.rs
// DebugFrameOverlay: the mode, the rolling sample window and the total
// frame count, which survives mode changes and stat resets).
type debugFrameOverlay struct {
	mode            DebugFrameOverlayMode
	drawDurations   []time.Duration
	totalFrameCount uint64
}

// windowOverlayStates is the per-window overlay registry (foreground
// thread only; dropped with the window's draw state via
// dropWindowDiagnostics).
var windowOverlayStates = map[*Window]*debugFrameOverlay{}

// windowOverlayOf returns (creating when absent) the window's overlay.
func windowOverlayOf(w *Window) *debugFrameOverlay {
	if w == nil {
		return nil
	}
	overlay, ok := windowOverlayStates[w]
	if !ok {
		overlay = &debugFrameOverlay{}
		windowOverlayStates[w] = overlay
	}
	return overlay
}

// dropWindowOverlay removes a destroyed window's overlay state.
func dropWindowOverlay(w *Window) {
	delete(windowOverlayStates, w)
}

// DebugFrameOverlayModeOf returns the window's current overlay mode
// (window.rs debug_frame_overlay_mode).
func DebugFrameOverlayModeOf(w *Window) DebugFrameOverlayMode {
	if overlay := windowOverlayOf(w); overlay != nil {
		return overlay.mode
	}
	return DebugOverlayHidden
}

// SetDebugFrameOverlayMode sets the mode and schedules a redraw
// (window.rs set_debug_frame_overlay_mode: set_mode + refresh).
func SetDebugFrameOverlayMode(w *Window, mode DebugFrameOverlayMode) {
	if w == nil {
		panic("gpui: SetDebugFrameOverlayMode requires a live window")
	}
	overlay := windowOverlayOf(w)
	overlay.mode = mode
	w.requestRefresh()
}

// CycleDebugFrameOverlayMode advances the overlay through its hidden,
// frame-time-only and detailed modes
// (window.rs cycle_debug_frame_overlay_mode).
func CycleDebugFrameOverlayMode(w *Window) {
	if w == nil {
		return
	}
	overlay := windowOverlayOf(w)
	overlay.mode = overlay.mode.Next()
	w.requestRefresh()
}

// ResetDebugFrameOverlayStats clears the frame-time statistics, except
// for the total frame count, and schedules a redraw
// (window.rs reset_debug_frame_overlay_stats; debug_overlay.rs
// reset_stats).
func ResetDebugFrameOverlayStats(w *Window) {
	if w == nil {
		return
	}
	overlay := windowOverlayOf(w)
	overlay.drawDurations = overlay.drawDurations[:0]
	w.requestRefresh()
}

// DebugFrameOverlayStats returns the overlay's observable statistics:
// the sample count, the total frame count and the current readout
// lines (the deterministic observable of record_frame/reset_stats).
func DebugFrameOverlayStats(w *Window) (samples int, frames uint64, lines []string) {
	overlay := windowOverlayOf(w)
	if overlay == nil {
		return 0, 0, nil
	}
	return len(overlay.drawDurations), overlay.totalFrameCount, overlay.lines()
}

// RecordDebugFrame records one frame's draw duration into the window's
// overlay (debug_overlay.rs DebugFrameOverlay::record_frame: bump the
// total count and push the sample, dropping the oldest beyond
// MAX_SAMPLES). The window.rs call site is cfg(feature = "profiler");
// the port gates on the CapFrameOverlay capability (the profiler gate
// is not tied to debug builds in the pin, so the default test profile
// keeps it OFF exactly like `cargo test` without the feature).
func RecordDebugFrame(w *Window, drawDuration time.Duration) {
	if w == nil || !DiagnosticCapabilityEnabled(CapFrameOverlay) {
		return
	}
	windowOverlayOf(w).record(drawDuration)
}

// record is DebugFrameOverlay::record_frame: bump the total count and
// push the sample, dropping the oldest beyond MAX_SAMPLES.
func (o *debugFrameOverlay) record(drawDuration time.Duration) {
	o.totalFrameCount++
	if len(o.drawDurations) >= debugOverlayMaxSamples {
		o.drawDurations = o.drawDurations[1:]
	}
	o.drawDurations = append(o.drawDurations, drawDuration)
}

// is_enabled (debug_overlay.rs DebugFrameOverlay::is_enabled).
func (o *debugFrameOverlay) is_enabled() bool {
	return o.mode != DebugOverlayHidden
}

// lines builds the readout lines (debug_overlay.rs lines): Minimal is
// the current duration; Full is the percentile readout with the
// right-aligned, saturating frame count.
func (o *debugFrameOverlay) lines() []string {
	var current *time.Duration
	if len(o.drawDurations) > 0 {
		last := o.drawDurations[len(o.drawDurations)-1]
		current = &last
	}
	switch o.mode {
	case DebugOverlayHidden:
		return nil
	case DebugOverlayMinimal:
		return []string{formatMsOverlay(current)}
	default:
		sorted := make([]time.Duration, len(o.drawDurations))
		copy(sorted, o.drawDurations)
		// sort_unstable over durations (time.Duration is ordered; equal
		// samples only feed percentile reads, so stability is
		// unobservable here).
		slices.Sort(sorted)
		percentile := func(numerator int) *time.Duration {
			if len(sorted) == 0 {
				return nil
			}
			value := sorted[(len(sorted)-1)*numerator/100]
			return &value
		}
		// Past five digits the count would break the column
		// alignment, so it saturates instead.
		frameCount := fmt.Sprintf("%d", o.totalFrameCount)
		if o.totalFrameCount > 99_999 {
			frameCount = "LOTS"
		}
		var max *time.Duration
		if len(sorted) > 0 {
			last := sorted[len(sorted)-1]
			max = &last
		}
		return []string{
			"CUR " + formatMsOverlay(current),
			"1%  " + formatMsOverlay(percentile(99)),
			"10% " + formatMsOverlay(percentile(90)),
			"MAX " + formatMsOverlay(max),
			fmt.Sprintf("FRAMES %5s", frameCount),
		}
	}
}

// formatMsOverlay formats as `abc.d MS`, right-aligned in room for
// three integer digits and one decimal (padded with spaces, not
// zeroes), so stacked readouts align (debug_overlay.rs format_ms; the
// None case renders "   -- MS").
func formatMsOverlay(duration *time.Duration) string {
	if duration == nil {
		return "   -- MS"
	}
	ms := float32(*duration) / float32(time.Millisecond)
	return fmt.Sprintf("%5.1f MS", ms)
}

// paintDebugFrameOverlay paints the overlay into the frame under
// construction (debug_overlay.rs DebugFrameOverlay::paint, called by
// window.rs draw after draw_roots): a translucent black panel with the
// readout's bitmap glyphs at the top-right of the viewport. Inert when
// disabled or the capability is off (the cfg'd-out branch).
func paintDebugFrameOverlay(w *Window, frame *frameDrawState) error {
	if !DiagnosticCapabilityEnabled(CapFrameOverlay) {
		return nil
	}
	overlay := windowOverlayOf(w)
	if overlay == nil || !overlay.is_enabled() {
		return nil
	}
	lines := overlay.lines()
	if len(lines) == 0 {
		return nil
	}
	maxLineChars := 0
	for _, line := range lines {
		if len(line) > maxLineChars {
			maxLineChars = len(line)
		}
	}
	// Ensure at least one physical pixel per cell so the text stays
	// legible at fractional downscale factors.
	cell := debugOverlayCellSize * frame.scale
	if cell < 1.0 {
		cell = 1.0
	}

	panelWidth := cell * (float32(maxLineChars)*debugOverlayCharAdvance + 2.0*debugOverlayPanelPadding)
	panelHeight := cell * (float32(len(lines))*debugOverlayLineAdvance + 2.0*debugOverlayPanelPadding)
	viewportWidth := frame.viewport.Width * frame.scale
	viewportHeight := frame.viewport.Height * frame.scale
	panelLeft := viewportWidth - panelWidth - cell*debugOverlayPanelMargin
	panelTop := cell * debugOverlayPanelMargin

	contentMask := Bounds{
		Origin: Point{},
		Size:   Size{Width: viewportWidth, Height: viewportHeight},
	}

	if err := frame.scene.InsertQuad(overlaySolidQuad(panelLeft, panelTop, panelWidth, panelHeight, &contentMask, overlayPanelColor())); err != nil {
		return err
	}

	textColor := overlayTextColor()
	for lineIndex, line := range lines {
		lineTop := panelTop + cell*(debugOverlayPanelPadding+float32(lineIndex)*debugOverlayLineAdvance)
		for charIndex, character := range []byte(line) {
			rows, ok := overlayGlyph(character)
			if !ok {
				continue
			}
			glyphLeft := panelLeft + cell*(debugOverlayPanelPadding+float32(charIndex)*debugOverlayCharAdvance)
			for rowIndex, row := range rows {
				rowTop := lineTop + cell*float32(rowIndex)
				// Merge horizontal runs of lit cells into single quads.
				column := 0
				for column < debugOverlayGlyphWidth {
					if row&(1<<(debugOverlayGlyphWidth-1-column)) == 0 {
						column++
						continue
					}
					runStart := column
					for column < debugOverlayGlyphWidth && row&(1<<(debugOverlayGlyphWidth-1-column)) != 0 {
						column++
					}
					if err := frame.scene.InsertQuad(overlaySolidQuad(
						glyphLeft+cell*float32(runStart),
						rowTop,
						cell*float32(column-runStart),
						cell,
						&contentMask,
						textColor,
					)); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// overlayTextColor is debug_overlay.rs text_color:
// rgb_to_hsla(rgba(0x33ff33ff)).
func overlayTextColor() Hsla { return RgbaToHsla(0x33ff33ff) }

// overlayPanelColor is debug_overlay.rs panel_color:
// rgb_to_hsla(rgba(0x000000aa)).
func overlayPanelColor() Hsla { return RgbaToHsla(0x000000aa) }

// overlaySolidQuad builds the pin's solid_quad (debug_overlay.rs: a
// Solid-border Quad with a transparent-black border and zero radii; all
// geometry in device pixels, order kernel-assigned).
func overlaySolidQuad(left, top, width, height float32, contentMask *Bounds, color Hsla) Quad {
	return Quad{
		Bounds: Bounds{
			Origin: Point{X: left, Y: top},
			Size:   Size{Width: width, Height: height},
		},
		ContentMask:  *contentMask,
		Background:   SolidBackground(color),
		BorderColor:  SolidBackground(TransparentBlack()),
		BorderStyle:  BorderStyleSolid,
		CornerRadii:  Corners{},
		BorderWidths: Edges{},
	}
}

// overlayGlyph returns the 5x7 bitmap for the given character, one
// byte of column bits per row with the most significant of the 5 bits
// leftmost (debug_overlay.rs glyph). Only the characters used by the
// overlay's readouts are defined; every other character renders as
// blank space.
func overlayGlyph(character byte) ([7]byte, bool) {
	switch character {
	case '0':
		return [7]byte{0b01110, 0b10001, 0b10011, 0b10101, 0b11001, 0b10001, 0b01110}, true
	case '1':
		return [7]byte{0b00100, 0b01100, 0b00100, 0b00100, 0b00100, 0b00100, 0b01110}, true
	case '2':
		return [7]byte{0b01110, 0b10001, 0b00001, 0b00010, 0b00100, 0b01000, 0b11111}, true
	case '3':
		return [7]byte{0b11111, 0b00010, 0b00100, 0b00010, 0b00001, 0b10001, 0b01110}, true
	case '4':
		return [7]byte{0b00010, 0b00110, 0b01010, 0b10010, 0b11111, 0b00010, 0b00010}, true
	case '5':
		return [7]byte{0b11111, 0b10000, 0b11110, 0b00001, 0b00001, 0b10001, 0b01110}, true
	case '6':
		return [7]byte{0b00110, 0b01000, 0b10000, 0b11110, 0b10001, 0b10001, 0b01110}, true
	case '7':
		return [7]byte{0b11111, 0b00001, 0b00010, 0b00100, 0b01000, 0b01000, 0b01000}, true
	case '8':
		return [7]byte{0b01110, 0b10001, 0b10001, 0b01110, 0b10001, 0b10001, 0b01110}, true
	case '9':
		return [7]byte{0b01110, 0b10001, 0b10001, 0b01111, 0b00001, 0b00010, 0b01100}, true
	case '.':
		return [7]byte{0b00000, 0b00000, 0b00000, 0b00000, 0b00000, 0b01100, 0b01100}, true
	case '-':
		return [7]byte{0b00000, 0b00000, 0b00000, 0b11111, 0b00000, 0b00000, 0b00000}, true
	case '%':
		return [7]byte{0b11001, 0b11001, 0b00010, 0b00100, 0b01000, 0b10011, 0b10011}, true
	case 'A':
		return [7]byte{0b01110, 0b10001, 0b10001, 0b11111, 0b10001, 0b10001, 0b10001}, true
	case 'C':
		return [7]byte{0b01110, 0b10001, 0b10000, 0b10000, 0b10000, 0b10001, 0b01110}, true
	case 'E':
		return [7]byte{0b11111, 0b10000, 0b10000, 0b11110, 0b10000, 0b10000, 0b11111}, true
	case 'F':
		return [7]byte{0b11111, 0b10000, 0b10000, 0b11110, 0b10000, 0b10000, 0b10000}, true
	case 'L':
		return [7]byte{0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b10000, 0b11111}, true
	case 'M':
		return [7]byte{0b10001, 0b11011, 0b10101, 0b10101, 0b10001, 0b10001, 0b10001}, true
	case 'N':
		return [7]byte{0b10001, 0b11001, 0b10101, 0b10011, 0b10001, 0b10001, 0b10001}, true
	case 'O':
		return [7]byte{0b01110, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b01110}, true
	case 'R':
		return [7]byte{0b11110, 0b10001, 0b10001, 0b11110, 0b10100, 0b10010, 0b10001}, true
	case 'S':
		return [7]byte{0b01111, 0b10000, 0b10000, 0b01110, 0b00001, 0b00001, 0b11110}, true
	case 'T':
		return [7]byte{0b11111, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100, 0b00100}, true
	case 'U':
		return [7]byte{0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b10001, 0b01110}, true
	case 'X':
		return [7]byte{0b10001, 0b10001, 0b01010, 0b00100, 0b01010, 0b10001, 0b10001}, true
	default:
		return [7]byte{}, false
	}
}
