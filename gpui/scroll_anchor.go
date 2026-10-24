package gpui

// The scroll-anchoring slice (browser-core ticket 46, spec story 83):
// content inserted above the visible area keeps the visible content in
// place, as Chrome's scroll anchoring does.
//
// Chromium's model, verified in %TEMP%\research-src\chromium
// (0d4dcd4ffc1d, third_party/blink/renderer/core):
//
//   - Each scroll container's ScrollAnchor picks an ANCHOR NODE: the
//     first box in DOM-order DFS whose rect (the border box, extended
//     by the box's scrollable overflow when it does not clip) occupies
//     space and intersects the scrollport — a fully-contained box stops
//     the walk (kReturn), a partially-visible one becomes the tentative
//     anchor and the walk DESCENDS into it (kConstrain), because a
//     deeper fully-visible descendant is a better anchor
//     (ScrollAnchor::FindAnchor / Examine / FindAnchorRecursive,
//     core/layout/scroll_anchor.cc:360-395, 498-545). The walk starts
//     at the scroller's children and prunes sticky subtrees, boxes that
//     cannot move with the scroller and non-occupying boxes (kSkip).
//   - NotifyBeforeLayout (scroll_anchor.cc:613-651) runs before each
//     layout: at a zero block-direction scroll offset the anchor is
//     cleared (no anchoring at rest); otherwise the anchor's position
//     relative to the scroller is saved and an adjustment is queued.
//   - Adjust (scroll_anchor.cc:713-760) runs at the end of the frame:
//     the adjustment is the anchor's position delta, ROUNDED to whole
//     pixels (ComputeAdjustment, :653-695 — "the anchor node is
//     DIP-snapped when painting, so we must round the offsets to
//     determine the visual delta"), on the BLOCK axis only, and the
//     scroll offset moves by it (kAnchoring). A dead anchor object
//     re-runs FindAnchor without adjusting.
//
// Port mapping. The layout runs in the request phase and the scroll
// offset cell is clamped at the scroller's prepaint, so the port keeps
// the same observable behaviour across frames instead of within one:
// the scroller saves the anchor's CONTENT-space top (scroll-independent
// — a user scroll changes neither the saved nor the resolved position,
// so the anchoring never fights the user), and at each prepaint, before
// the clamp, it resolves the anchor again and moves the offset by the
// content-space delta — content inserted above the anchor pushes it
// down by exactly its height, and the offset follows. The anchor's
// cross-frame identity is its element id path plus the occurrence index
// among the same-path nodes of the scroller's subtree (the port's
// equivalent of Chromium's LayoutObject pointer; the serialized form
// uses the same nth-of-siblings idea, ComputeUniqueSelector). The
// identity needs the request walk to record node paths, which the
// scroller's own request turns on (div.go's requestLayout) — one small
// string per node per frame for windows that have scroll containers.
//
// The anchor walk is the native tree (preorder), whose order matches
// the element tree's DOM order. Measured against Chrome 155 (the
// anchoring fixture's record): a wrapper div's rows inserted above the
// scrolled viewport adjust the offset by the inserted height, keeping
// the anchor row at the viewport top — the walk must descend past the
// partially-visible wrapper to the rows, exactly the DFS rule above.
//
// Divergences from Chromium (documented, ticket-scoped):
//   - no overflow-anchor: none (no opt-out style exists);
//   - no scroll-anchor-disabling style suppression (a style-driven
//     move of the anchor adjusts like a layout move);
//   - the anchor rect's bottom extension uses the node's content size
//     unconditionally, where Chromium extends only non-clipping boxes;
//   - auto end margins resolve to zero (they are not in the record);
//   - the anchoring axis is Y only, horizontal-tb assumed (the port's
//     only writing mode).

import (
	"fmt"
	"math"

	"github.com/hoangvu12/gpui-go/internal/native"
)

// scrollAnchorState is one scroller's anchoring bookkeeping, persistent
// across frames in the scroll state (the handle's or the element
// state's).
type scrollAnchorState struct {
	// has reports whether an anchor is saved.
	has bool
	// path is the anchor node's element id path key at save time.
	path string
	// occurrence is the anchor's occurrence index among the same-path
	// nodes of the scroller's subtree walk (the nth-sibling identity).
	occurrence int
	// savedTop is the anchor's content-space top (logical pixels, the
	// scroller's border-box space) under the previous frame's layout.
	savedTop float32
}

// save adopts one candidate as the anchor.
func (s *scrollAnchorState) save(c *ScrollAnchorCandidate) {
	s.has = true
	s.path = c.Path
	s.occurrence = c.Occurrence
	s.savedTop = c.Top
}

// clear drops the anchor (the scroller returned to rest).
func (s *scrollAnchorState) clear() {
	*s = scrollAnchorState{}
}

// ScrollAnchorCandidate is one anchoring candidate resolved by the
// engine's walk, in logical pixels of the scroller's border-box space.
type ScrollAnchorCandidate struct {
	// Path is the node's element id path key ("" when the request walk
	// did not record one).
	Path string
	// Occurrence is the candidate's index among the same-path nodes of
	// the subtree walk.
	Occurrence int
	// Top and Bottom are the candidate rect's edges: the border box,
	// the bottom extended by the node's content overflow (Chromium's
	// RelativeBounds extension).
	Top, Bottom float32
}

// ScrollAnchorFacts is the anchoring walk's result.
type ScrollAnchorFacts struct {
	// Selected is the DFS selection's candidate (Chromium's FindAnchor):
	// nil when nothing viable intersects the scrollport.
	Selected *ScrollAnchorCandidate
	// Saved is the candidate matching the previous frame's anchor
	// identity: nil when none was requested or the anchor node is gone.
	Saved *ScrollAnchorCandidate
}

// anchorWalkState carries the enumeration walk's frame-local state.
type anchorWalkState struct {
	occurrences map[string]int
	// candidates maps each walked node to its candidate (the selection
	// walk resolves its handle through this).
	candidates map[native.NodeHandle]*ScrollAnchorCandidate
	// wantSaved reports whether a saved identity should resolve.
	wantSaved bool
	savedPath string
	savedOcc  int
	saved     *ScrollAnchorCandidate
}

// ScrollAnchorFacts walks the scroller's subtree and resolves the
// anchoring facts (browser-core 46). offset is the scroller's current
// scroll offset (logical pixels, negative y scrolls down); the
// scrollport is the border box inset by the borders and the scrollbar
// reservation, shifted by the offset into content space. wantSelected
// requests the DFS selection (the selection frame, or a dead anchor's
// re-selection); wantSaved resolves the previous frame's anchor
// identity (savedPath/savedOccurrence). The walk is O(subtree) — twice
// when both the selection and the resolution run — a documented cost
// (Chromium's is O(1) while the anchor object lives).
func (e *LayoutEngine) ScrollAnchorFacts(ctx *LayoutContext, id LayoutID, offset Point, wantSelected, wantSaved bool, savedPath string, savedOccurrence int) (ScrollAnchorFacts, error) {
	if err := e.checkBusy("ScrollAnchorFacts"); err != nil {
		return ScrollAnchorFacts{}, err
	}
	if ctx == nil {
		return ScrollAnchorFacts{}, fmt.Errorf("gpui: ScrollAnchorFacts requires a layout context")
	}
	if err := e.checkID(id, "ScrollAnchorFacts"); err != nil {
		return ScrollAnchorFacts{}, err
	}
	record, err := e.native.NodeLayout(id.node)
	if err != nil {
		return ScrollAnchorFacts{}, fmt.Errorf("gpui: ScrollAnchorFacts: %w", err)
	}
	scale := ctx.ScaleFactor()
	// The scrollport in the scroller's border-box space (device px),
	// shifted by the scroll offset: the offset is negative-down, so the
	// visible content window moves DOWN by -offset.Y.
	scrolled := -offset.Y * scale
	viewportTop := record.BorderTop + scrolled
	viewportBottom := record.SizeH - record.BorderBottom - record.ScrollbarH + scrolled

	// Walk A: the unpruned preorder enumeration — every occupying node
	// becomes a candidate with its (path, occurrence) identity, and the
	// saved identity resolves wherever it lives (the pruned selection
	// walk would skip rows above the viewport).
	state := &anchorWalkState{
		occurrences: make(map[string]int),
		candidates:  make(map[native.NodeHandle]*ScrollAnchorCandidate),
		wantSaved:   wantSaved,
		savedPath:   savedPath,
		savedOcc:    savedOccurrence,
	}
	e.anchorEnumerate(id.node, devicePoint{}, state, scale)

	out := ScrollAnchorFacts{}
	if state.saved != nil {
		out.Saved = state.saved
	}
	if wantSelected || out.Saved == nil {
		// Walk B: the DFS selection. It only matters when the caller
		// selects (no anchor yet) or the saved anchor died and a fresh
		// one is needed.
		if handle := e.anchorSelect(id.node, devicePoint{}, viewportTop, viewportBottom); handle != 0 {
			out.Selected = state.candidates[handle]
		}
	}
	return out, nil
}

// anchorRect computes one node's candidate rect (device px): the border
// box at the accumulated offset, the bottom extended by the node's
// content size (the scrollable overflow — Chromium's RelativeBounds
// extends the bottom edge for boxes that do not clip; the port extends
// unconditionally, a documented divergence for clipped boxes in the
// intersection test only, since the anchored corner is the top).
func anchorRect(record native.LayoutRecord, offset devicePoint) (top, bottom, left, right float32) {
	top = offset.Y
	bottom = max32(offset.Y+record.SizeH, offset.Y+record.BorderTop+record.ContentH)
	left = offset.X
	right = offset.X + record.SizeW
	return top, bottom, left, right
}

// anchorEnumerate walks the subtree in unpruned preorder (walk A):
// every occupying node becomes a candidate. display:none subtrees are
// skipped (they have no boxes, like Chromium's layout tree, which never
// contains them).
func (e *LayoutEngine) anchorEnumerate(node native.NodeHandle, offset devicePoint, state *anchorWalkState, scale float32) {
	count, err := e.native.NodeChildCount(node)
	if err != nil {
		return
	}
	for i := uint32(0); i < count; i++ {
		child, err := e.native.NodeChild(node, i)
		if err != nil {
			continue
		}
		childID := LayoutID{engine: e, node: child, generation: e.generation}
		if record, ok := e.displayPositions[childID]; ok && record.display == DisplayNone {
			continue
		}
		record, err := e.native.NodeLayout(child)
		if err != nil {
			continue
		}
		childOffset := devicePoint{X: offset.X + record.LocationX, Y: offset.Y + record.LocationY}
		top, bottom, left, right := anchorRect(record, childOffset)
		if right > left && bottom > top {
			path := e.nodePaths[childID]
			occurrence := state.occurrences[path]
			state.occurrences[path] = occurrence + 1
			candidate := &ScrollAnchorCandidate{
				Path:       path,
				Occurrence: occurrence,
				Top:        top / scale,
				Bottom:     bottom / scale,
			}
			state.candidates[child] = candidate
			if state.wantSaved && path == state.savedPath && occurrence == state.savedOcc {
				state.saved = candidate
			}
		}
		e.anchorEnumerate(child, childOffset, state, scale)
	}
}

// anchorSelect walks the subtree with Chromium's DFS rules (walk B) and
// returns the selected node's handle (0 when nothing is viable):
//
//   - a sticky subtree is skipped (Examine's HasStickyConstrained
//     Position rule — a sticky box does not move with the content);
//   - a non-occupying or non-intersecting box is skipped with its
//     subtree (kSkip);
//   - an intersecting box becomes the selection; a fully-contained one
//     stops the walk (kReturn), a partial one is descended into hoping
//     for a better (fully visible) descendant (kConstrain) — and the
//     first viable child ends the sibling examination either way.
func (e *LayoutEngine) anchorSelect(node native.NodeHandle, offset devicePoint, viewportTop, viewportBottom float32) native.NodeHandle {
	count, err := e.native.NodeChildCount(node)
	if err != nil {
		return 0
	}
	for i := uint32(0); i < count; i++ {
		child, err := e.native.NodeChild(node, i)
		if err != nil {
			continue
		}
		childID := LayoutID{engine: e, node: child, generation: e.generation}
		if record, ok := e.displayPositions[childID]; ok {
			if record.display == DisplayNone || record.position == PositionSticky {
				continue
			}
		}
		childRecord, err := e.native.NodeLayout(child)
		if err != nil {
			continue
		}
		childOffset := devicePoint{X: offset.X + childRecord.LocationX, Y: offset.Y + childRecord.LocationY}
		top, bottom, left, right := anchorRect(childRecord, childOffset)
		if right <= left || bottom <= top {
			continue // kSkip: no occupied space
		}
		if bottom <= viewportTop || top >= viewportBottom {
			continue // kSkip: the box does not intersect the scrollport
		}
		if top >= viewportTop && bottom <= viewportBottom {
			// kReturn: fully contained — this is the anchor.
			return child
		}
		// kConstrain: partially visible — descend for a fully-visible
		// descendant, then stop (the first viable child ends the walk).
		if selected := e.anchorSelect(child, childOffset, viewportTop, viewportBottom); selected != 0 {
			return selected
		}
		return child
	}
	return 0
}

// applyScrollAnchoring is the scroller's prepaint half (browser-core
// 46): resolve the anchor under THIS frame's layout and move the offset
// by the content-space delta before the clamp bounds it. The block axis
// is Y (the port's only writing mode); a scroller at rest clears its
// anchor (Chromium's zero-offset rule — nothing can be kept in place
// from the top).
func (s *elementScroll) applyScrollAnchoring(w *Window, layoutID LayoutID) {
	if s == nil || s.anchor == nil {
		return
	}
	if s.offset.Y == 0 {
		// Chromium's zero-offset rule (NotifyBeforeLayout clears at
		// rest): nothing can be kept in place from the top.
		s.anchor.clear()
		return
	}
	engine := layoutID.engine
	ctx := layoutContextOf(w)
	facts, err := engine.ScrollAnchorFacts(&ctx, layoutID, *s.offset, !s.anchor.has, s.anchor.has, s.anchor.path, s.anchor.occurrence)
	if err != nil {
		panic("gpui: DivElement scroll anchoring: " + err.Error())
	}
	if !s.anchor.has {
		// The selection frame (Chromium's FindAnchor at
		// NotifyBeforeLayout): adopt the first viable candidate without
		// adjusting — the position is saved under THIS layout, and the
		// NEXT frame's content moves are what the adjustment measures.
		if facts.Selected != nil {
			s.anchor.save(facts.Selected)
		}
		return
	}
	saved := facts.Saved
	if saved == nil {
		// The anchor node is gone (the row was removed or rebuilt under
		// a different path): re-select like Chromium's dead-object path,
		// with no adjustment this frame.
		if facts.Selected != nil {
			s.anchor.save(facts.Selected)
		} else {
			s.anchor.clear()
		}
		return
	}
	// ComputeAdjustment's rounding (scroll_anchor.cc:665-668): the delta
	// is rounded to whole pixels, block axis only.
	delta := float32(math.Round(float64(saved.Top - s.anchor.savedTop)))
	if delta != 0 {
		s.offset.Y -= delta
		// browser-core 45: an anchoring adjustment during a running
		// animation shifts the curve with the offset (ApplyAdjustment,
		// scroll_offset_animation_curve.cc:300-305 — blink's kAnchoring
		// arm calls ScrollAnimator::AdjustAnimation, scrollable_area.cc:
		// 379-383), so the animation keeps running toward the shifted
		// target instead of snapping back at its next tick.
		s.smoothScrollAdjust(Point{Y: -delta})
	}
	s.anchor.savedTop = saved.Top
}
