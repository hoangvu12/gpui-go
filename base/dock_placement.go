package base

// Port of U:dock/dock_placement.rs: pure size arithmetic for resizing one
// dock, and the dock's runtime state. It draws nothing.

import (
	"github.com/hoangvu12/gpui-go/gpui"
)

// DockSizing is pure arithmetic for resizing one dock
// (U:dock/dock_placement.rs:14-24). The caller supplies the area bounds
// and the opposite dock's size.
type DockSizing struct {
	placement        DockPlacement
	area             gpui.Bounds
	oppositeDockSize float32
}

// NewDockSizing is DockSizing::new (U:dock/dock_placement.rs:27-33).
func NewDockSizing(placement DockPlacement) DockSizing {
	return DockSizing{placement: placement}
}

// WithAreaBounds sets the full area bounds (U:dock/dock_placement.rs:35-41).
func (s DockSizing) WithAreaBounds(area gpui.Bounds) DockSizing {
	s.area = area
	return s
}

// WithAreaWidth sets only the area's width (U:dock/dock_placement.rs:43-49).
func (s DockSizing) WithAreaWidth(width float32) DockSizing {
	s.area.Size.Width = width
	return s
}

// WithAreaHeight sets only the area's height (U:dock/dock_placement.rs:51-57).
func (s DockSizing) WithAreaHeight(height float32) DockSizing {
	s.area.Size.Height = height
	return s
}

// WithOppositeDockSize sets the size of the dock on the opposite side
// (U:dock/dock_placement.rs:59-65).
func (s DockSizing) WithOppositeDockSize(size float32) DockSizing {
	s.oppositeDockSize = size
	return s
}

// SizeFromPointer returns the dock size the pointer implies, before
// clamping (U:dock/dock_placement.rs:66-81). The center answers zero.
func (s DockSizing) SizeFromPointer(pointer gpui.Point) float32 {
	switch s.placement {
	case DockPlacementLeft:
		return pointer.X - s.area.Left()
	case DockPlacementRight:
		return s.area.Right() - pointer.X
	case DockPlacementBottom:
		return s.area.Bottom() - pointer.Y
	}
	return 0
}

// Clamp holds a size within the range this dock may occupy: never below
// PanelMinSize, and never so large it squeezes the opposite dock below it
// either (U:dock/dock_placement.rs:83-102). The center clamps nothing.
func (s DockSizing) Clamp(size float32) float32 {
	var maxSize float32
	switch s.placement {
	case DockPlacementLeft, DockPlacementRight:
		maxSize = max(s.area.Size.Width-PanelMinSize-s.oppositeDockSize, PanelMinSize)
	case DockPlacementBottom:
		maxSize = max(s.area.Size.Height-PanelMinSize, PanelMinSize)
	default:
		return size
	}
	return pixelsClamp(size, PanelMinSize, maxSize)
}

// Dock is the runtime state for one dock: open, collapsible, its size,
// and whether it is mid-resize (U:dock/dock_placement.rs:105-119).
type Dock struct {
	open        bool
	collapsible bool
	size        float32
	liveSize    *float32
	resizing    bool
}

// NewDock is Dock::new (U:dock/dock_placement.rs:122-130): open and
// collapsible.
func NewDock(size float32) Dock {
	return Dock{open: true, collapsible: true, size: size}
}

// IsOpen is U:dock/dock_placement.rs:132-134.
func (d *Dock) IsOpen() bool { return d.open }

// SetOpen is U:dock/dock_placement.rs:136-138.
func (d *Dock) SetOpen(open bool) { d.open = open }

// IsCollapsible is U:dock/dock_placement.rs:140-142.
func (d *Dock) IsCollapsible() bool { return d.collapsible }

// SetCollapsible is U:dock/dock_placement.rs:144-146.
func (d *Dock) SetCollapsible(collapsible bool) { d.collapsible = collapsible }

// Size is U:dock/dock_placement.rs:148-150.
func (d *Dock) Size() float32 { return d.size }

// SetSize sets the dock's size, never below PanelMinSize
// (U:dock/dock_placement.rs:152-160).
func (d *Dock) SetSize(size float32) { d.size = max(size, PanelMinSize) }

// liveSizeValue is a size below PanelMinSize a drag in progress is
// showing; never persisted (U:dock/dock_placement.rs:162-167).
func (d *Dock) liveSizeValue() *float32 { return d.liveSize }

func (d *Dock) setLiveSize(size *float32) { d.liveSize = size }

// IsResizing is U:dock/dock_placement.rs:173-175.
func (d *Dock) IsResizing() bool { return d.resizing }

// SetResizing is U:dock/dock_placement.rs:177-179.
func (d *Dock) SetResizing(resizing bool) { d.resizing = resizing }
