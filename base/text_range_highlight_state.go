package base

// The TextViewState half of ticket 30 (U:text/state.rs:395-404, 586-700,
// 959-1001, 1019-1030): the motion policy, the rendered text, range
// highlights, reveal, and the streamed-fade repaint tick. The fields live
// in TextViewState (text_state.go); text_state.go calls
// reconcileRangeHighlights and the fade tracker where upstream does.

import (
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// streamFadeTick repaints a streamed fade at about 30 fps, not at the
// display refresh rate (U:text/state.rs:45-46).
const streamFadeTick = 33 * time.Millisecond

// Motion is the builder TextViewState::motion (U:text/state.rs:395-399).
func (s TextViewState) Motion(motion TextViewMotion) TextViewState {
	s.SetMotion(motion)
	return s
}

// SetMotion sets the motion policy (U:text/state.rs:401-404).
func (s *TextViewState) SetMotion(motion TextViewMotion) { s.streamFade.setMotion(motion) }

// RenderedText is the text this view renders, which RangeHighlight ranges
// index: the string plain copy produces, as of the last parse that landed
// (U:text/state.rs:586-597).
func (s *TextViewState) RenderedText() RenderedText {
	return RenderedText{owner: s.entityID, revision: s.committedRevision, document: s.parsedContent.document, index: s.renderedIndex}
}

// SetRangeHighlights replaces the range highlights, whose ranges index the
// current rendered text; any invalid range rejects the set
// (U:text/state.rs:599-624).
func (s *TextViewState) SetRangeHighlights(highlights []RangeHighlight, cx *gpui.Context[TextViewState]) error {
	if s.format != textViewFormatMarkdown {
		return RangeHighlightError{Kind: RangeHighlightUnsupported}
	}
	frame, err := newRangeHighlightFrame(s.RenderedText(), highlights)
	if err != nil {
		return err
	}
	s.rangeHighlights = frame
	cx.Notify()
	return nil
}

// RevealRange scrolls the line rng starts on into view, rng indexing the
// current rendered text. Revealing is best effort: nil means the range is
// valid and the request was taken (U:text/state.rs:626-668).
func (s *TextViewState) RevealRange(rng gpui.TextRange, cx *gpui.Context[TextViewState]) error {
	if s.format != textViewFormatMarkdown {
		return RangeHighlightError{Kind: RangeHighlightUnsupported}
	}
	text := s.RenderedText()
	if text.IsEmpty() && rng == (gpui.TextRange{}) {
		// Nothing to reveal in an empty view.
		s.pendingReveal = nil
		return nil
	}
	reveal, ok := newPendingReveal(text, rng, cx.App().Now())
	if !ok {
		return rangeHighlightInvalid(0)
	}
	s.pendingReveal = reveal
	cx.Notify()
	return nil
}

// ClearRangeHighlights removes all range highlights
// (U:text/state.rs:670-675).
func (s *TextViewState) ClearRangeHighlights(cx *gpui.Context[TextViewState]) {
	if s.rangeHighlights != nil {
		s.rangeHighlights = nil
		cx.Notify()
	}
}

// reconcileRangeHighlights carries the range highlights and the pending
// reveal over to next, the document the update of revision parsed, before
// it replaces the current one (U:text/state.rs:677-700).
func (s *TextViewState) reconcileRangeHighlights(next *parsedDocument, revision int, appendText bool) {
	tailOnly := appendText && s.fullUpdateRevision <= s.committedRevision
	s.committedRevision = revision
	s.renderedIndex = &renderedIndexCell{}
	if s.rangeHighlights != nil || s.pendingReveal != nil {
		remap := newLeafRemap(s.parsedContent.document, next, tailOnly)
		if s.rangeHighlights != nil {
			s.rangeHighlights = s.rangeHighlights.remap(remap)
		}
		if s.pendingReveal != nil {
			s.pendingReveal = s.pendingReveal.remap(remap)
		}
	}
}

// revealFrame starts a frame of the pending reveal: the line to hand to
// rendering, and the block a scrollable view has to scroll to first
// (U:text/state.rs:959-1001).
func (s *TextViewState) revealFrame(now time.Duration, w *gpui.Window) (*revealRequest, int, bool) {
	if s.maxLines != nil || (s.pendingReveal != nil && s.pendingReveal.isExpired(now)) {
		s.pendingReveal = nil
	}
	pending := s.pendingReveal
	if pending == nil {
		return nil, 0, false
	}
	blockIx, hasBlock := pending.blockIx(s.parsedContent.document)
	blockOffScreen := func(ix int) bool {
		viewport := s.listState.ViewportBounds()
		item, ok := s.listState.BoundsForItem(ix)
		return !ok || item.Bottom() <= viewport.Top() || item.Top() >= viewport.Bottom()
	}
	revealBlock := hasBlock && s.scrollable && (pending.isBlock() || !pending.wasLaidOut()) && blockOffScreen(blockIx)
	request := pending.request()
	if pending.isBlock() {
		// A block has no line to wait for.
		s.pendingReveal = nil
	} else {
		gpui.RequestAnimationFrame(w)
	}
	return request, blockIx, revealBlock
}

// renderStreamFade samples the streamed fade for this frame and schedules
// the repaint tick while anything fades (U:text/state.rs:1019-1029).
func (s *TextViewState) renderStreamFade(cx *gpui.Context[TextViewState]) *streamFadeFrame {
	frame := s.streamFade.frame(time.Now(), cx.App().ReduceMotion())
	if frame != nil && !s.fadeTick {
		s.fadeTick = true
		cx.App().SpawnDeliveringInto(cx.Scope(), cx.Entity(), func(run *gpui.TaskRun) struct{} {
			run.Sleep(streamFadeTick)
			return struct{}{}
		}, func(s *TextViewState, _ struct{}, cx *gpui.Context[TextViewState]) {
			s.fadeTick = false
			cx.Notify()
		})
	}
	return frame
}

// installTextLeafFrames hands the frame's fades, highlights and reveal to
// node rendering (U:text/state.rs:1031-1045).
func (cx *nodeRenderContext) installTextLeafFrames(fades *streamFadeFrame, highlights *rangeHighlightFrame, reveal *revealRequest) {
	if fades != nil {
		cx.streamFades = func(key textLeafKey) []textFadeRange {
			f, _ := fades.fades(key)
			return f
		}
	}
	if highlights != nil {
		cx.rangeBackgrounds = highlights.backgrounds
	}
	cx.reveal = reveal
}

// revealAt is the pending reveal when it starts in the text leaf key
// between start and end, rebased to start (U:text/node.rs:2035-2039).
func (cx *nodeRenderContext) revealAt(key *textLeafKey, start, end int) *revealAt {
	return cx.reveal.at(key, start, end)
}

// withReveal scrolls the line reveal starts on into view during prepaint
// (U:text/inline.rs:414-418).
func (e *inlineText) withReveal(reveal *revealAt) *inlineText {
	e.reveal = reveal
	return e
}

// requestReveal asks the enclosing list to scroll the line of the pending
// reveal into view, and reports where it is and whether it is inside the
// visible area (U:text/inline.rs:420-467; the request is gpui's
// Window.RequestAutoscroll, taken by gpui.List in prepaint, ticket 37).
func (e *inlineText) requestReveal(w *gpui.Window) {
	if e.reveal == nil {
		return
	}
	layout := e.styledText.Layout()
	bounds := layout.Bounds()
	lineHeight := layout.LineHeight()
	glyphs := glyphBoxes(e.text, e.retainedRuns, e.fontSize, layout, gpui.WindowTextStyle(w).TextAlign, bounds.Size.Width, w)
	// The glyph drawing the text at the offset, or, for text with no glyph
	// of its own such as a line break, the next glyph, or the last.
	offset := e.reveal.offset
	var hit rowBox
	if boxes := rangeBoxes(glyphs, gpui.TextRange{Start: offset, End: offset + 1}); len(boxes) > 0 {
		hit = boxes[0]
	} else if len(glyphs) > 0 {
		g := glyphs[len(glyphs)-1]
		for _, candidate := range glyphs {
			if candidate.text.Start >= offset {
				g = candidate
				break
			}
		}
		hit = rowBox{row: g.row, left: g.left, right: g.right}
	}
	line := textBoundsFromCorners(
		gpui.Point{X: bounds.Left() + hit.left, Y: bounds.Top() + lineHeight*float32(hit.row)},
		gpui.Point{X: bounds.Left() + max(hit.right, hit.left+1), Y: bounds.Top() + lineHeight*float32(hit.row+1)},
	)
	w.RequestAutoscroll(line)
	// A list scrolls the line to its edge, which layout may miss by a
	// fraction of a pixel.
	visible := gpui.ContentMask(w)
	top, bottom := visible.Top()-0.5, visible.Bottom()+0.5
	e.reveal.reportLine(line, line.Top() >= top && line.Bottom() <= bottom)
}

// revealFragment is the flow fragment a reveal goes to: the one it starts
// in, else the next fragment of its text, else the last
// (U:text/inline_flow.rs:438-463).
func (f *inlineFlow) revealFragment(fragments []positionedFragment) int {
	found, foundIn := -1, false
	for ix := range fragments {
		fragment := &fragments[ix]
		if fragment.kind != positionedText {
			continue
		}
		item := &f.items[fragment.itemIx]
		if item.kind != inlineFlowText || item.reveal == nil {
			continue
		}
		in := fragment.sourceRange.End > item.reveal.offset
		if found < 0 || !foundIn {
			found, foundIn = ix, in
		}
	}
	return found
}

// fragmentReveal is the reveal of fragment ix, clamped into its text.
func (f *inlineFlow) fragmentReveal(item *inlineFlowItem, fragment *positionedFragment, ix, revealIx int) *revealAt {
	if item.reveal == nil || ix != revealIx {
		return nil
	}
	return item.reveal.clamp(fragment.sourceRange.Start, fragment.sourceRange.End)
}

// textViewRevealProgress ends the frame of a pending reveal: every list has
// scrolled by now, so the line is where it ends up this frame
// (U:text/text_view.rs:813-830).
func textViewRevealProgress(state gpui.Entity[TextViewState], handler RevealHandlerFn, w *gpui.Window, app *gpui.App) {
	pending := state.Read(app, func(s *TextViewState, _ *gpui.App) bool { return s.pendingReveal != nil })
	if !pending {
		return
	}
	var kind revealProgressKind
	var line gpui.Bounds
	state.Update(app, func(s *TextViewState, _ *gpui.Context[TextViewState]) {
		if s.pendingReveal == nil {
			kind = revealNotLaidOut
			return
		}
		kind, line = s.pendingReveal.progress()
		if kind == revealShown {
			s.pendingReveal = nil
		}
	})
	if kind == revealHidden && handler != nil {
		handler(line, w, app)
	}
}

// RevealHandlerFn scrolls a container that does not follow scroll requests
// to the line of TextViewState.RevealRange (U:text/text_view.rs:85).
type RevealHandlerFn func(line gpui.Bounds, w *gpui.Window, app *gpui.App)

// OnReveal scrolls a container that does not follow scroll requests to the
// line of TextViewState.RevealRange: after a frame in which the line was
// laid out but not visible, handler receives its bounds in window
// coordinates (U:text/text_view.rs:386-400).
func (v *TextView) OnReveal(handler RevealHandlerFn) *TextView {
	v.revealHandler = handler
	return v
}

// Motion sets the motion policy; without one, the state's own policy
// applies, which plays no motion by default (U:text/text_view.rs:408-413).
func (v *TextView) Motion(motion TextViewMotion) *TextView {
	v.motion = &motion
	return v
}
