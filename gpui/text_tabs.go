package gpui

// Hard tabs (Base ticket 44): a '\t' paints nothing and advances to the
// next tab stop, as on Base's GPUI 0.2.2 line.
//
// The facts this follows:
//
//   - The pin shapes with Parley/harfrust and emits every positioned
//     glyph without a control-character filter (gpui_ce_parley
//     text_system.rs:1273-1285), so '\t' maps to glyph 0, the font's
//     missing-glyph box, at the box's advance (measured: Consolas 14px
//     7.70px, the advance of every Consolas glyph; Segoe UI 14px 9.04px,
//     where a space is 3.83px).
//   - Base's 0.2.2 line laid each line out with a DirectWrite
//     IDWriteTextLayout (gpui_windows direct_write.rs:536-555) and never
//     called SetIncrementalTabStop, so DirectWrite's default applied.
//     Measured through dwrite.dll on this machine: a tab is one blank
//     whitespace cluster reaching the next multiple of 4 × the font size
//     from the line's start (14px: "\t" = 56, "a\tb" tab = 48.30 in
//     Consolas and 48.88 in Segoe UI, "\t\tb" = 56 + 56; 16px: 64).
//     0.2.2's LineWrapper measured a lone tab through the same layout
//     (layout_width), so it wrapped a tab as one full stop.
//   - Nothing in the GPUI family between the two (gpui_elements
//     editable_text, which inserts '\t' and shapes it as is, state.rs
//     1101; Base's input, which relies on the platform) expands tabs.
//
// The port keeps the pin's shaping and reproduces DirectWrite's result
// without a native change (the DLL is frozen at rev 11): ShapeText hands
// the native service a space for every tab, in a one-byte run of its
// own, so byte offsets, clusters and carets are untouched and the tab
// paints a blank glyph; it then measures each tab's x from its visual
// row's start and widens the tab with that run's letter spacing (the
// native service adds a run's letter spacing after each glyph, measured)
// until it ends on the next stop. A tab stays one cluster, so the caret
// steps over it in one move and a hit inside it snaps to its nearer
// edge, as DirectWrite's tab cluster did. Wrapping can move a tab to
// another row; the fit re-runs (at most tabFitPasses shapes) until every
// tab sits on a stop. Relayout re-fits the same way.
//
// Recorded deviations: stops are measured from each shaped document's
// visual row start, as DirectWrite measured them from each
// IDWriteTextLayout's line start, so a text split into separately shaped
// pieces (the inline flow's segments) measures from each piece. An
// editor that wants column tab stops (Base's input: Zed's tab map) shapes
// spaces itself and never reaches this path.

import (
	"math"
	"strings"

	"github.com/hoangvu12/gpui-go/internal/native"
)

const (
	// DefaultTabStopEms is the incremental tab stop in ems: DirectWrite's
	// default (4 × the font size), the stop Base's 0.2.2 line drew tabs at.
	DefaultTabStopEms = 4
	// tabFitPasses bounds the re-shapes one tab fit may take.
	tabFitPasses = 4
	// tabFitTolerance is the error (px) a fitted tab end may keep.
	tabFitTolerance = 0.01
	// tabStopEpsilon keeps a pen that float error put a hair short of a
	// stop from producing a sliver of a tab.
	tabStopEpsilon = 0.001
)

// DefaultTabStop returns the incremental tab stop for a font size
// (DirectWrite's default, 4 × the font size).
func DefaultTabStop(fontSize float32) float32 { return fontSize * DefaultTabStopEms }

// nextTabStop returns the first stop strictly past x.
func nextTabStop(x, stop float32) float32 {
	return float32(math.Floor(float64((x+tabStopEpsilon)/stop))+1) * stop
}

// tabFit is a shaped document's hard-tab bookkeeping: where its tabs
// are, the native run each one shapes in, and the letter spacing the fit
// gave each.
type tabFit struct {
	// stop is the incremental tab stop (px).
	stop float32
	// tabs are the tabs' byte indices, in order.
	tabs []int
	// runs are the indices of the tabs' one-byte runs in the native
	// request.
	runs []int
	// base is each tab run's own letter spacing (the run it was cut
	// from); extra is the spacing the fit adds.
	base, extra []float32
}

// prepareTabs rewrites a request holding hard tabs for the fit: every tab
// becomes a space in a one-byte run cut from the run that held it. It
// returns the request unchanged and a nil fit when the text has no tab,
// the font size gives no stop, or the runs do not cover the text exactly
// (the native service reports that).
func prepareTabs(request TextLayoutRequest) (TextLayoutRequest, *tabFit) {
	if strings.IndexByte(request.Text, '\t') < 0 || !(request.FontSize > 0) {
		return request, nil
	}
	total := 0
	for _, run := range request.Runs {
		if run.Len < 0 {
			return request, nil
		}
		total += run.Len
	}
	if total != len(request.Text) {
		return request, nil
	}
	fit := &tabFit{stop: DefaultTabStop(request.FontSize)}
	runs := make([]TextRun, 0, len(request.Runs)+2*strings.Count(request.Text, "\t"))
	offset := 0
	for _, run := range request.Runs {
		start, end := offset, offset+run.Len
		offset = end
		for start < end {
			ix := strings.IndexByte(request.Text[start:end], '\t')
			if ix < 0 {
				piece := run
				piece.Len = end - start
				runs = append(runs, piece)
				break
			}
			if ix > 0 {
				piece := run
				piece.Len = ix
				runs = append(runs, piece)
			}
			var base float32
			if run.LetterSpacing != nil {
				base = *run.LetterSpacing
			}
			tab := run
			tab.Len = 1
			spacing := base
			tab.LetterSpacing = &spacing
			fit.tabs = append(fit.tabs, start+ix)
			fit.runs = append(fit.runs, len(runs))
			fit.base = append(fit.base, base)
			fit.extra = append(fit.extra, 0)
			runs = append(runs, tab)
			start += ix + 1
		}
	}
	request.Text = strings.ReplaceAll(request.Text, "\t", " ")
	request.Runs = runs
	return request, fit
}

// fitTabsLocked widens every tab of the line to the next stop past its
// x on its visual row, re-shaping until the tabs sit on their stops (or
// tabFitPasses shapes ran). The caller holds l.mu and the line holds a
// live handle.
func (l *WrappedLine) fitTabsLocked() error {
	fit := l.tabs
	svc := l.system.svc
	for range tabFitPasses {
		changed := false
		rowY := float32(math.NaN())
		var shift float32
		for i, ix := range fit.tabs {
			start, err := svc.Caret(l.handle, ix, native.CaretDownstream, 1)
			if err != nil {
				return err
			}
			end, err := svc.Caret(l.handle, ix+1, native.CaretUpstream, 1)
			if err != nil {
				return err
			}
			if !start.Present || !end.Present || start.Bounds.Y != end.Bounds.Y {
				continue
			}
			if start.Bounds.Y != rowY {
				rowY, shift = start.Bounds.Y, 0
			}
			have := end.Bounds.X - start.Bounds.X
			if have < 0 {
				// A right-to-left row: its pen runs the other way.
				continue
			}
			// Earlier tabs on the row move this one by what they change.
			x := start.Bounds.X + shift
			delta := nextTabStop(x, fit.stop) - x - have
			shift += delta
			if delta > tabFitTolerance || delta < -tabFitTolerance {
				fit.extra[i] += delta
				changed = true
			}
		}
		if !changed {
			return nil
		}
		request := l.request
		request.Runs = append([]native.TextRunSpec(nil), l.request.Runs...)
		for i, run := range fit.runs {
			spacing := fit.base[i] + fit.extra[i]
			request.Runs[run].LetterSpacing = &spacing
		}
		handle, err := l.system.shapeHandle(request, l)
		if err != nil {
			return err
		}
		l.system.liveHandles.Add(1)
		if err := l.refreshSummaryOf(handle); err != nil {
			if svc.Dispose(handle) == nil {
				l.system.liveHandles.Add(-1)
			}
			return err
		}
		old := l.handle
		l.handle, l.request = handle, request
		if svc.Dispose(old) == nil {
			l.system.liveHandles.Add(-1)
		}
	}
	return nil
}
