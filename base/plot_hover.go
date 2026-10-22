package base

// Hover tracking shared by every Plot (U:plot/hover.rs): which datum the
// cursor is on, how far its hover has faded in, and where a pointer
// following it has glided to. Behavior only: a styled layer draws the
// crosshair, dots and tooltip box, and projects its timing through
// PlotMotion.
//
// Core seams: the hover memory is window keyed state
// (Window.UseKeyedState, R:window.rs:4093) under the plot's element scope;
// the fade is MotionTransition and the glide MotionSpring (motion.go).
//
// Go adaptation: PlotHover.Glide's `id: impl Into<TransitionId>` is a
// generic method over IntoTransitionID; the deprecated `focus` alias is
// Focus.

import "github.com/hoangvu12/gpui-go/gpui"

// TooltipState is the datum the cursor resolved to, returned from
// Plot.TooltipState (U:plot/hover.rs:13-26). Positions are relative to the
// plot's origin.
type TooltipState struct {
	// Index is the hovered datum's index in the plot's data.
	Index int
	// CrossLine is where a crosshair marking the datum sits.
	CrossLine gpui.Point
	// Dots are the data points to mark, one per series at the datum.
	Dots []gpui.Point
}

// NewTooltipState returns a tooltip state (U:plot/hover.rs:28-36).
func NewTooltipState(index int, crossLine gpui.Point, dots []gpui.Point) TooltipState {
	return TooltipState{Index: index, CrossLine: crossLine, Dots: dots}
}

// clone is the derived Clone.
func (s TooltipState) clone() TooltipState {
	s.Dots = append([]gpui.Point(nil), s.Dots...)
	return s
}

// PlotHover is the datum a plot has under the pointer this frame, handed
// to Plot.Hover (U:plot/hover.rs:38-50). After the cursor leaves, the
// state lingers while the progress eases back to zero.
type PlotHover struct {
	state    TooltipState
	progress float32
	hovered  bool
}

// State is the hovered datum: the one under the cursor, or the last one
// while the hover fades out (U:plot/hover.rs:53-57).
func (h *PlotHover) State() *TooltipState { return &h.state }

// Progress is how far the hover has faded in, from 0 to 1
// (U:plot/hover.rs:59-66).
func (h *PlotHover) Progress() float32 { return h.progress }

// Focus is Progress (U:plot/hover.rs:68-71).
//
// Deprecated: since 0.7.0; use Progress.
func (h *PlotHover) Focus() float32 { return h.Progress() }

// IsHovered reports whether the cursor is on the datum, as opposed to the
// state lingering while its hover fades out (U:plot/hover.rs:73-77).
func (h *PlotHover) IsHovered() bool { return h.hovered }

// IsEntering reports whether this is the first frame the cursor is on a
// datum: the hover has not started fading in yet
// (U:plot/hover.rs:79-84).
func (h *PlotHover) IsEntering() bool { return h.hovered && h.progress == 0 }

// Glide follows target on the pointer spring, adopting the target on the
// entering frame instead of travelling from where the last hover ended
// (U:plot/hover.rs:86-101).
func (h *PlotHover) Glide[I IntoTransitionID](id I, target float32, w *gpui.Window, app *gpui.App) float32 {
	policy := PointerSpring(app).WithTravel(!h.IsEntering())
	return MotionSpring(id, target, policy, w, app)
}

// PointerSpring is the spring a hover pointer follows the hovered datum
// with: the active PlotMotion's pointer, which snaps unless a styled
// layer projects one (U:plot/hover.rs:104-109).
func PointerSpring(app *gpui.App) Spring {
	return ThemeGlobal(app).Plot.Motion().Pointer()
}

// plotHoverMemory is the last datum the cursor resolved to, where the
// cursor was and how far the hover has faded in, kept in element state
// (U:plot/hover.rs:111-134).
type plotHoverMemory struct {
	state    *TooltipState
	cursor   gpui.Point
	progress float32
	// entering is whether this frame is the first the cursor is on a
	// datum.
	entering bool
}

// plotHoverMemoryKey is the element-state key of a plot's hover memory,
// within the plot's scope (U:plot/hover.rs:136-137).
const plotHoverMemoryKey = "__plot-hover"

// plotUseHoverMemory is `window.use_keyed_state(HOVER_MEMORY, ..)` with
// the memory's default: an overlay rendered outside a plot's tracking is
// fully opaque (U:plot/hover.rs:124-134).
func plotUseHoverMemory(w *gpui.Window, app *gpui.App) gpui.Entity[plotHoverMemory] {
	return w.UseKeyedState[plotHoverMemory](gpui.NameElementID(plotHoverMemoryKey), app, func(*gpui.Window, *gpui.Context[plotHoverMemory]) plotHoverMemory {
		return plotHoverMemory{progress: 1}
	})
}

// plotTrackHover resolves the datum a plot shows this frame from the live
// state the cursor resolved to (U:plot/hover.rs:139-193). While live is
// set it is shown as is; after the cursor leaves the last state lingers
// with its progress easing to zero over the exit, then is dropped. The
// returned cursor is the live one, or the last one while the state
// lingers.
func plotTrackHover(live *TooltipState, cursor *gpui.Point, w *gpui.Window, app *gpui.App) (*PlotHover, gpui.Point) {
	hovered := live != nil
	memory := plotUseHoverMemory(w, app)

	motion := ThemeGlobal(app).Plot.Motion()
	policy := motion.Exit()
	var target float32
	if hovered {
		policy = motion.Enter()
		target = 1
	}
	progress := MotionTransition(TransitionIDWithChannel(plotHoverMemoryKey, "progress"), target, policy, w, app)

	memory.Update(app, func(m *plotHoverMemory, _ *gpui.Context[plotHoverMemory]) {
		if live != nil && cursor != nil {
			s := live.clone()
			m.state = &s
			m.cursor = *cursor
		}
		m.progress = progress
		m.entering = hovered && progress == 0
		if !hovered && progress <= 0 {
			m.state = nil
		}
	})

	m := memory.Read(app, func(m *plotHoverMemory, _ *gpui.App) plotHoverMemory { return *m })
	if m.state == nil {
		return nil, gpui.Point{}
	}
	return &PlotHover{state: m.state.clone(), progress: progress, hovered: hovered}, m.cursor
}

// HoverProgress is how far the enclosing plot's hover has faded in this
// frame, from 0 to 1 (U:plot/hover.rs:195-211). Only meaningful while
// that plot renders its overlay; anywhere else it returns 1.
func HoverProgress(w *gpui.Window, app *gpui.App) float32 {
	return plotUseHoverMemory(w, app).Read(app, func(m *plotHoverMemory, _ *gpui.App) plotHoverMemory { return *m }).progress
}

// IsHoverEntering reports whether this frame is the first the enclosing
// plot's cursor is on a datum (U:plot/hover.rs:213-224). Only meaningful
// while that plot renders its overlay; anywhere else it returns false.
func IsHoverEntering(w *gpui.Window, app *gpui.App) bool {
	return plotUseHoverMemory(w, app).Read(app, func(m *plotHoverMemory, _ *gpui.App) plotHoverMemory { return *m }).entering
}
