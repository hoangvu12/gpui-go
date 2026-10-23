package base

// Port of U:resizable/mod.rs: the resizable panel group state.
//
// Pixels are float32 in the Go core, so every size computation below runs
// in float32 in the same order as upstream (the drag test pins an exact
// 220/180 split).

import (
	"math"
	"slices"

	"github.com/hoangvu12/gpui-go/gpui"
)

// PanelMinSize is the default minimum panel size (U:resizable/mod.rs:13-14,
// #[doc(hidden)] PANEL_MIN_SIZE).
const PanelMinSize float32 = 100

// pixelsMax is Pixels::MAX (f32::MAX).
const pixelsMax float32 = math.MaxFloat32

// HResizable creates a ResizablePanelGroup with horizontal resizing
// (U:resizable/mod.rs:16-19).
func HResizable[I gpui.IntoElementID](id I) *ResizablePanelGroup {
	return NewResizablePanelGroup(id).Axis(gpui.AxisHorizontal)
}

// VResizable creates a ResizablePanelGroup with vertical resizing
// (U:resizable/mod.rs:21-24).
func VResizable[I gpui.IntoElementID](id I) *ResizablePanelGroup {
	return NewResizablePanelGroup(id).Axis(gpui.AxisVertical)
}

// PixelsRange is a half-open pixel range (upstream Range<Pixels>).
type PixelsRange struct {
	Start, End float32
}

// ResizableState is the state of a ResizablePanelGroup
// (U:resizable/mod.rs:31-40). Build it with NewResizableState: the Go
// zero Axis is vertical, upstream's Default is horizontal.
type ResizableState struct {
	// axis syncs to the actual axis of the group in use.
	axis   gpui.Axis
	panels []resizablePanelState
	sizes  []float32
	// resizingPanelIx is meaningful when hasResizingIx (Option<usize>).
	resizingPanelIx int
	hasResizingIx   bool
	bounds          gpui.Bounds

	// drawing is set while the group's render and prepaint callbacks
	// update the state, and tracked once a completed frame has drawn it;
	// see drawNotify.
	drawing bool
	tracked bool
}

// NewResizableState is ResizableState::default (U:resizable/mod.rs:42-52):
// horizontal, no panels.
func NewResizableState() ResizableState {
	return ResizableState{axis: gpui.AxisHorizontal}
}

// resizablePanelState is one panel's state (U:resizable/mod.rs:390-395).
// Its zero value is the derived Default: no size, a 0..0 range.
type resizablePanelState struct {
	size      *float32
	sizeRange PixelsRange
	bounds    gpui.Bounds
}

// ResizablePanelEvent is emitted by ResizableState
// (U:resizable/panel.rs:17-19).
type ResizablePanelEvent uint8

const (
	// ResizablePanelResized is ResizablePanelEvent::Resized.
	ResizablePanelResized ResizablePanelEvent = iota
)

// ResizableEvents is the ResizableState event descriptor
// (U:resizable/mod.rs:388).
var ResizableEvents = gpui.DefineEvent[ResizableState, ResizablePanelEvent]()

func someF32(v float32) *float32 { return &v }

// drawNotify is cx.Notify under the reference's draw-phase guard.
//
// In the pin, a notification of an entity the window drew in its last
// frame, raised while that window draws, only marks the view dirty: no
// Notify effect is queued, so no observer runs (R:window.rs:189-214
// invalidate_view, R:app.rs:2920-2953). The Go core still queues the
// effect while drawing (gpui/frameloop.go, recorded deviation), so the
// keyed-state observer would dirty the window again after every frame and
// the group, whose panels notify from prepaint on every frame, would never
// settle. The state therefore drops the draw-time notifications itself
// once a frame has drawn it, and keeps them on the first frame, where the
// pin delivers them too (the entity is not tracked yet).
func (s *ResizableState) drawNotify(cx *gpui.Context[ResizableState]) {
	if s.drawing && s.tracked {
		return
	}
	cx.Notify()
}

// duringDraw runs f with drawing set (see drawNotify).
func (s *ResizableState) duringDraw(f func()) {
	s.drawing = true
	defer func() { s.drawing = false }()
	f()
}

// Sizes returns the panel sizes (U:resizable/mod.rs:55-58).
func (s *ResizableState) Sizes() []float32 { return s.sizes }

// ResizingPanelIndex returns the panel whose handle is being dragged.
func (s *ResizableState) ResizingPanelIndex() (int, bool) {
	return s.resizingPanelIx, s.hasResizingIx
}

// ResizePanel programmatically resizes the panel at ix to size,
// redistributing space among siblings using the same logic as a drag
// (U:resizable/mod.rs:60-89). Sizes are clamped to the panel's size range
// and to the container; it emits Resized. Out-of-range indices are a
// no-op; for the last panel space is taken from the previous sibling.
func (s *ResizableState) ResizePanel(ix int, size float32, w *gpui.Window, cx *gpui.Context[ResizableState]) {
	if ix < 0 || ix >= len(s.sizes) {
		return
	}
	if ix+1 < len(s.sizes) {
		s.resizePanelAtHandle(ix, size, w, cx)
	} else if ix > 0 {
		// Last panel: drive its size by resizing the previous sibling so the
		// freed space lands here.
		delta := s.sizes[ix] - size
		prev := s.sizes[ix-1]
		s.resizePanelAtHandle(ix-1, prev+delta, w, cx)
	}
	s.doneResizing(cx)
}

// InsertPanel inserts a panel state at ix, or appends it when ix is
// negative (U:resizable/mod.rs:91-128). Existing panel sizes are
// redistributed so their total remains equal to the container size. A nil
// size uses PanelMinSize.
func (s *ResizableState) InsertPanel(size *float32, ix int, cx *gpui.Context[ResizableState]) {
	panelState := resizablePanelState{}
	if size != nil {
		panelState.size = someF32(*size)
	}
	sz := PanelMinSize
	if size != nil {
		sz = *size
	}

	// We make sure that the size always sums up to the container size by
	// reducing the size of all other panels first.
	containerSize := max(s.ContainerSize(), 1)
	totalLeftoverSize := max(containerSize-sz, 1)

	for i := range s.panels {
		ratio := s.sizes[i] / containerSize
		s.sizes[i] = totalLeftoverSize * ratio
		s.panels[i].size = someF32(s.sizes[i])
	}

	if ix >= 0 {
		s.panels = slices.Insert(s.panels, ix, panelState)
		s.sizes = slices.Insert(s.sizes, ix, sz)
	} else {
		s.panels = append(s.panels, panelState)
		s.sizes = append(s.sizes, sz)
	}
	cx.Notify()
}

// AdoptSizes adopts slot sizes decided by an owner that keeps its own
// record of the layout, as the dock's pane tree does
// (U:resizable/mod.rs:130-169, pub(crate)). Nothing is redistributed;
// unconstrained (nil) slots keep their measurement.
func (s *ResizableState) AdoptSizes(sizes []*float32, cx *gpui.Context[ResizableState]) {
	changed := false
	for ix, size := range sizes {
		// The preference is mirrored exactly, None included.
		if ix < len(s.panels) {
			panel := &s.panels[ix]
			if !optF32Equal(panel.size, size) {
				panel.size = nil
				if size != nil {
					panel.size = someF32(*size)
				}
				changed = true
			}
		}
		// The measurement only moves when the tree names a size.
		if size == nil {
			continue
		}
		if ix < len(s.sizes) && s.sizes[ix] != *size {
			s.sizes[ix] = *size
			changed = true
		}
	}
	if changed {
		cx.Notify()
	}
}

func optF32Equal(a, b *float32) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// syncPanelsCount syncs the axis and the panel count
// (U:resizable/mod.rs:171-198).
func (s *ResizableState) syncPanelsCount(axis gpui.Axis, panelsCount int, cx *gpui.Context[ResizableState]) {
	changed := s.axis != axis
	s.axis = axis

	if panelsCount > len(s.panels) {
		diff := panelsCount - len(s.panels)
		for range diff {
			s.panels = append(s.panels, resizablePanelState{})
			s.sizes = append(s.sizes, PanelMinSize)
		}
		changed = true
	}
	if panelsCount < len(s.panels) {
		s.panels = s.panels[:panelsCount]
		s.sizes = s.sizes[:panelsCount]
		changed = true
	}
	if changed {
		// We need to make sure the total size is in line with the
		// container size.
		s.adjustToContainerSize(cx)
	}
}

// updatePanelSize records a panel's measured bounds
// (U:resizable/mod.rs:200-218).
func (s *ResizableState) updatePanelSize(panelIx int, bounds gpui.Bounds, sizeRange PixelsRange, cx *gpui.Context[ResizableState]) {
	size := bounds.Size.Along(s.axis)
	// This check is only necessary to stop the very first panel from
	// resizing on its own: it needs to pass when the panel is freshly
	// created so we get the initial size, but it's also fine when it
	// sometimes passes later.
	if s.sizes[panelIx] == PanelMinSize {
		s.sizes[panelIx] = size
		s.panels[panelIx].size = someF32(size)
	}
	s.panels[panelIx].bounds = bounds
	s.panels[panelIx].sizeRange = sizeRange
	s.drawNotify(cx)
}

// RemovePanel removes the panel at panelIx and redistributes the
// remaining space (U:resizable/mod.rs:220-230).
func (s *ResizableState) RemovePanel(panelIx int, cx *gpui.Context[ResizableState]) {
	s.panels = slices.Delete(s.panels, panelIx, panelIx+1)
	s.sizes = slices.Delete(s.sizes, panelIx, panelIx+1)
	if s.hasResizingIx && s.resizingPanelIx > panelIx {
		s.resizingPanelIx--
	}
	s.adjustToContainerSize(cx)
}

// ResetPanel resets the panel at panelIx while preserving its current
// size (U:resizable/mod.rs:232-239).
func (s *ResizableState) ResetPanel(panelIx int, cx *gpui.Context[ResizableState]) {
	oldSize := s.sizes[panelIx]
	s.panels[panelIx] = resizablePanelState{}
	s.sizes[panelIx] = oldSize
	s.adjustToContainerSize(cx)
}

// Clear removes all panel state (U:resizable/mod.rs:241-245).
func (s *ResizableState) Clear() {
	s.panels = s.panels[:0]
	s.sizes = s.sizes[:0]
}

// ContainerSize returns the measured size of the group along its resize
// axis (U:resizable/mod.rs:247-251).
func (s *ResizableState) ContainerSize() float32 { return s.bounds.Size.Along(s.axis) }

// doneResizing ends a resize and emits Resized
// (U:resizable/mod.rs:253-256).
func (s *ResizableState) doneResizing(cx *gpui.Context[ResizableState]) {
	s.hasResizingIx = false
	s.resizingPanelIx = 0
	ResizableEvents.Emit(cx, ResizablePanelResized)
}

// panelSizeRange returns a panel's size range (U:resizable/mod.rs:258-264).
func (s *ResizableState) panelSizeRange(ix int) PixelsRange {
	if ix < 0 || ix >= len(s.panels) {
		return PixelsRange{PanelMinSize, pixelsMax}
	}
	return s.panels[ix].sizeRange
}

// syncRealPanelSizes copies the measured sizes (U:resizable/mod.rs:266-270).
func (s *ResizableState) syncRealPanelSizes() {
	for i := range s.panels {
		s.sizes[i] = s.panels[i].bounds.Size.Along(s.axis)
	}
}

// resizePanelAtHandle resizes the panel at ix by treating ix as the
// drag-handle position, the handle between panel ix and ix+1
// (U:resizable/mod.rs:272-348). It returns early on the last panel.
func (s *ResizableState) resizePanelAtHandle(ix int, size float32, _ *gpui.Window, cx *gpui.Context[ResizableState]) {
	oldSizes := slices.Clone(s.sizes)

	// Only resize the left panels.
	if ix >= len(oldSizes)-1 {
		return
	}
	containerSize := s.ContainerSize()
	s.syncRealPanelSizes()

	moveChanged := size - oldSizes[ix]
	if moveChanged == 0 {
		return
	}

	sizeRange := s.panelSizeRange(ix)
	newSize := pixelsClamp(size, sizeRange.Start, sizeRange.End)
	isExpand := moveChanged > 0

	mainIx := ix
	newSizes := slices.Clone(oldSizes)

	if isExpand {
		changed := newSize - oldSizes[ix]
		newSizes[ix] = newSize

		for changed > 0 && ix < len(oldSizes)-1 {
			ix++
			sizeRange := s.panelSizeRange(ix)
			availableSize := max(newSizes[ix]-sizeRange.Start, 0)
			toReduce := min(changed, availableSize)
			newSizes[ix] -= toReduce
			changed -= toReduce
		}
	} else {
		changed := newSize - size
		newSizes[ix] = newSize

		for changed > 0 && ix > 0 {
			ix--
			sizeRange := s.panelSizeRange(ix)
			availableSize := max(newSizes[ix]-sizeRange.Start, 0)
			toReduce := min(changed, availableSize)
			changed -= toReduce
			newSizes[ix] -= toReduce
		}

		newSizes[mainIx+1] += oldSizes[mainIx] - size - changed
	}

	// If the total size exceeds the container size, adjust the main panel.
	var totalSize float32
	for _, sz := range newSizes {
		totalSize += sz
	}
	if totalSize > containerSize {
		overflow := totalSize - containerSize
		newSizes[mainIx] = max(newSizes[mainIx]-overflow, sizeRange.Start)
	}

	for i := range oldSizes {
		s.panels[i].size = someF32(newSizes[i])
	}
	s.sizes = newSizes
	cx.Notify()
}

// pixelsClamp is Pixels::clamp (Ord::clamp over the f32 order: the value
// held between min and max).
func pixelsClamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// adjustToContainerSize rescales the panels to the container size, keeping
// their proportions (U:resizable/mod.rs:350-385).
func (s *ResizableState) adjustToContainerSize(cx *gpui.Context[ResizableState]) {
	if s.ContainerSize() == 0 {
		return
	}

	// A panel with no size preference is laid out by flex, and its entry in
	// sizes is a placeholder until something measures it. Rescaling by a
	// ratio computed from that placeholder drags the sized panels along
	// with it; flex already fits the container, so there is nothing to
	// adjust.
	for i := range s.panels {
		if s.panels[i].size == nil {
			return
		}
	}

	containerSize := s.ContainerSize()
	var total float32
	for _, sz := range s.sizes {
		total += sz
	}
	if math.IsInf(float64(total), 0) || math.IsNaN(float64(total)) || total <= 0 {
		return
	}
	totalSize := total

	for i := range s.panels {
		size := s.sizes[i]
		ratio := size / totalSize
		newSize := containerSize * ratio

		s.sizes[i] = newSize
		s.panels[i].size = someF32(newSize)
	}
	s.drawNotify(cx)
}
