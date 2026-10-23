package base

// Port of U:dock/drag.rs: drag hit-testing and drop geometry for the dock.
// It draws nothing: the styled drag preview and the drop indicator are
// appearance and belong to the skin.

import (
	"sync/atomic"

	"github.com/hoangvu12/gpui-go/gpui"
)

// DragPanel is a panel being dragged out of a tab group
// (U:dock/drag.rs:25-40). It is carried as a PanelID; the drag offset and
// preview size are shared by every copy of the value (the upstream
// Rc<Cell<..>>).
type DragPanel struct {
	panel         PanelID
	source        NodeID
	dragOffset    *gpui.Point
	previewSize   *gpui.Size
	dragSessionID uint64
}

// nextDragSessionID starts at 1 so itemDragSessionID never collides
// (U:dock/drag.rs:42-47).
var nextDragSessionID atomic.Uint64

// itemDragSessionID stands in for DragPanel.DragSessionID on host-owned
// drag items (U:dock/drag.rs:44-47).
const itemDragSessionID uint64 = 0

// NewDragPanel is DragPanel::new (U:dock/drag.rs:50-58).
func NewDragPanel(panel PanelID, source NodeID) DragPanel {
	return DragPanel{
		panel:         panel,
		source:        source,
		dragOffset:    new(gpui.Point),
		previewSize:   new(gpui.Size),
		dragSessionID: nextDragSessionID.Add(1),
	}
}

// Panel returns the dragged panel (U:dock/drag.rs:60-62).
func (d DragPanel) Panel() PanelID { return d.panel }

// Source returns the tab group the panel was dragged out of
// (U:dock/drag.rs:64-67).
func (d DragPanel) Source() NodeID { return d.source }

// DragOffset is U:dock/drag.rs:69-71.
func (d DragPanel) DragOffset() gpui.Point {
	if d.dragOffset == nil {
		return gpui.Point{}
	}
	return *d.dragOffset
}

// SetDragOffset records where inside the panel the drag started
// (U:dock/drag.rs:73-77).
func (d DragPanel) SetDragOffset(offset gpui.Point) {
	if d.dragOffset != nil {
		*d.dragOffset = offset
	}
}

// PreviewSize is how large the drag preview is on screen
// (U:dock/drag.rs:79-88); zero by default.
func (d DragPanel) PreviewSize() gpui.Size {
	if d.previewSize == nil {
		return gpui.Size{}
	}
	return *d.previewSize
}

// SetPreviewSize is U:dock/drag.rs:90-92.
func (d DragPanel) SetPreviewSize(size gpui.Size) {
	if d.previewSize != nil {
		*d.previewSize = size
	}
}

// DragSessionID is U:dock/drag.rs:94-96.
func (d DragPanel) DragSessionID() uint64 { return d.dragSessionID }

// Render draws nothing (U:dock/drag.rs:99-105): the styled preview belongs
// to the skin.
func (*DragPanel) Render(*gpui.Window, *gpui.Context[DragPanel]) gpui.AnyElement { return gpui.Empty() }

// AnyDrag is a host-owned value being dragged over the dock, opaque to the
// dock itself (U:dock/drag.rs:107-123).
type AnyDrag struct {
	value any
}

// NewAnyDrag is AnyDrag::new (U:dock/drag.rs:114-118).
func NewAnyDrag(value any) AnyDrag { return AnyDrag{value: value} }

// Value is U:dock/drag.rs:120-122.
func (d AnyDrag) Value() any { return d.value }

// DropTarget is where a host-owned drag landed: a tab group, and the edge
// it resolved (U:dock/drag.rs:125-148). No placement means the centre zone:
// merge into the group rather than split.
type DropTarget struct {
	node         NodeID
	placement    Placement
	hasPlacement bool
}

func newDropTarget(node NodeID, placement Placement, hasPlacement bool) DropTarget {
	return DropTarget{node: node, placement: placement, hasPlacement: hasPlacement}
}

// Node is U:dock/drag.rs:141-143.
func (t DropTarget) Node() NodeID { return t.node }

// Placement is U:dock/drag.rs:145-147.
func (t DropTarget) Placement() (Placement, bool) { return t.placement, t.hasPlacement }

// DropIndicator is what the skin should draw while a drag hovers a group
// (U:dock/drag.rs:150-163): geometry over time, not the tween.
type DropIndicator struct {
	bounds        gpui.Bounds
	placement     Placement
	hasPlacement  bool
	from          DropPlaceholderBounds
	to            DropPlaceholderBounds
	dragSessionID uint64
	epoch         uint64
}

// Bounds is the hovered group's content bounds, in window coordinates
// (U:dock/drag.rs:184-187).
func (i DropIndicator) Bounds() gpui.Bounds { return i.bounds }

// Placement reports no placement for a merge into the tab group
// (U:dock/drag.rs:189-192).
func (i DropIndicator) Placement() (Placement, bool) { return i.placement, i.hasPlacement }

// From is where the placeholder starts, relative to Bounds
// (U:dock/drag.rs:194-197).
func (i DropIndicator) From() DropPlaceholderBounds { return i.from }

// To is where the placeholder settles, relative to Bounds
// (U:dock/drag.rs:199-202).
func (i DropIndicator) To() DropPlaceholderBounds { return i.to }

// DragSessionID is the drag session this indicator belongs to
// (U:dock/drag.rs:204-208).
func (i DropIndicator) DragSessionID() uint64 { return i.dragSessionID }

// Epoch is bumped on every restart (U:dock/drag.rs:210-214).
func (i DropIndicator) Epoch() uint64 { return i.epoch }

// DropPlaceholderBounds is the bounds a drop placeholder occupies within a
// tab group (U:dock/drag.rs:217-223).
type DropPlaceholderBounds struct {
	origin gpui.Point
	size   gpui.Size
}

// NewDropPlaceholderBounds is DropPlaceholderBounds::new
// (U:dock/drag.rs:225-228; pub(crate) upstream, used by the tests).
func NewDropPlaceholderBounds(origin gpui.Point, size gpui.Size) DropPlaceholderBounds {
	return DropPlaceholderBounds{origin: origin, size: size}
}

// DropPlaceholderBoundsForPlacement is U:dock/drag.rs:230-256: the half the
// placement names, or the whole group for a merge.
func DropPlaceholderBoundsForPlacement(bounds gpui.Bounds, placement Placement, hasPlacement bool) DropPlaceholderBounds {
	halfWidth := bounds.Size.Width * 0.5
	halfHeight := bounds.Size.Height * 0.5
	if !hasPlacement {
		return DropPlaceholderBounds{size: bounds.Size}
	}
	switch placement {
	case PlacementLeft:
		return DropPlaceholderBounds{size: gpui.Size{Width: halfWidth, Height: bounds.Size.Height}}
	case PlacementRight:
		return DropPlaceholderBounds{origin: gpui.Point{X: halfWidth}, size: gpui.Size{Width: halfWidth, Height: bounds.Size.Height}}
	case PlacementTop:
		return DropPlaceholderBounds{size: gpui.Size{Width: bounds.Size.Width, Height: halfHeight}}
	default:
		return DropPlaceholderBounds{origin: gpui.Point{Y: halfHeight}, size: gpui.Size{Width: bounds.Size.Width, Height: halfHeight}}
	}
}

// Origin is U:dock/drag.rs:258-260.
func (b DropPlaceholderBounds) Origin() gpui.Point { return b.origin }

// Size is U:dock/drag.rs:262-264.
func (b DropPlaceholderBounds) Size() gpui.Size { return b.size }

// splitPlacementAt returns which split zone position falls into within
// bounds, or false for the centre zone (U:dock/drag.rs:267-281). The
// comparisons are strict and x is tested first.
func splitPlacementAt(bounds gpui.Bounds, position gpui.Point) (Placement, bool) {
	switch {
	case position.X < bounds.Left()+bounds.Size.Width*0.35:
		return PlacementLeft, true
	case position.X > bounds.Left()+bounds.Size.Width*0.65:
		return PlacementRight, true
	case position.Y < bounds.Top()+bounds.Size.Height*0.35:
		return PlacementTop, true
	case position.Y > bounds.Top()+bounds.Size.Height*0.65:
		return PlacementBottom, true
	}
	return 0, false
}
