package base

// Port of U:dock/panel.rs: the behavior a dockable panel provides, and its
// object-safe handle.
//
// Go adaptations:
//   - The Panel trait's default methods are optional interfaces
//     (PanelVisible, PanelClosable, ...), the RootPlugin convention.
//   - The EventEmitter<PanelEvent> bound is not expressible on a Go
//     interface; a panel declares gpui.DefineEvent[S, PanelEvent]() itself.
//     Nothing in the dock subscribes to it, upstream included.
//   - Focusable is the FocusHandle(*gpui.App) method every Base state
//     exposes.
//   - PanelView handles do not own the panel entity: the dock holds the
//     handle it was given, whose lease stays with the caller's scope.

import (
	"github.com/hoangvu12/gpui-go/gpui"
)

// PanelEvent is what a panel may emit (U:dock/panel.rs:12-16).
type PanelEvent uint8

const (
	// PanelEventZoomIn is PanelEvent::ZoomIn.
	PanelEventZoomIn PanelEvent = iota
	// PanelEventZoomOut is PanelEvent::ZoomOut.
	PanelEventZoomOut
	// PanelEventLayoutChanged is PanelEvent::LayoutChanged.
	PanelEventLayoutChanged
)

// Panel is the behavior a dockable panel provides (U:dock/panel.rs:18-93).
// Presentation lives in the layer above.
type Panel[S any] interface {
	gpui.Render[S]
	// PanelName identifies the panel in persisted layouts. Once chosen,
	// never change it.
	PanelName() string
	// FocusHandle is Focusable::focus_handle.
	FocusHandle(app *gpui.App) gpui.FocusHandle
}

// PanelVisible is Panel::visible (U:dock/panel.rs:25-30; default true). A
// hidden panel keeps its place and tab; a container whose panels are all
// hidden gives up its slot.
type PanelVisible interface {
	Visible(app *gpui.App) bool
}

// PanelClosable is Panel::closable (U:dock/panel.rs:32-36; default true).
type PanelClosable interface {
	Closable(app *gpui.App) bool
}

// PanelZoomable is Panel::zoomable (U:dock/panel.rs:38-42; default true).
type PanelZoomable interface {
	Zoomable(app *gpui.App) bool
}

// PanelSetActive is Panel::set_active (U:dock/panel.rs:44-53): called
// with the frame-end net state when the panel becomes, or stops being, the
// displayed tab of its group, exactly once per edge. A removed panel is
// not told false; OnRemoved is its deactivation signal.
type PanelSetActive[S any] interface {
	SetActive(active bool, w *gpui.Window, cx *gpui.Context[S])
}

// PanelSetZoomed is Panel::set_zoomed (U:dock/panel.rs:55-61): only the
// displayed panel of the zooming group is told.
type PanelSetZoomed[S any] interface {
	SetZoomed(zoomed bool, w *gpui.Window, cx *gpui.Context[S])
}

// PanelOnAddedTo is Panel::on_added_to (U:dock/panel.rs:63-74): called
// when the panel joins a tab group, before any SetActive.
type PanelOnAddedTo[S any] interface {
	OnAddedTo(group gpui.WeakEntity[TabGroup], w *gpui.Window, cx *gpui.Context[S])
}

// PanelOnRemoved is Panel::on_removed (U:dock/panel.rs:76-82): the panel
// left the dock for good. A panel dragged between groups never hears it.
type PanelOnRemoved[S any] interface {
	OnRemoved(w *gpui.Window, cx *gpui.Context[S])
}

// PanelDump is Panel::dump (U:dock/panel.rs:84-92): the panel's own
// persisted state. The default records the name and nothing else.
type PanelDump interface {
	Dump(app *gpui.App) PanelState
}

// PanelView is the object-safe counterpart of Panel, used to hold
// heterogeneous panel entities behind a single handle
// (U:dock/panel.rs:95-132).
type PanelView interface {
	PanelName(app *gpui.App) string
	PanelID(app *gpui.App) PanelID
	Closable(app *gpui.App) bool
	Zoomable(app *gpui.App) bool
	Visible(app *gpui.App) bool
	SetActive(active bool, w *gpui.Window, app *gpui.App)
	SetZoomed(zoomed bool, w *gpui.Window, app *gpui.App)
	OnAddedTo(group gpui.WeakEntity[TabGroup], w *gpui.Window, app *gpui.App)
	OnRemoved(w *gpui.Window, app *gpui.App)
	View() gpui.View
	FocusHandle(app *gpui.App) gpui.FocusHandle
	Dump(app *gpui.App) PanelState
	// AsAny returns the concrete value behind this handle
	// (U:dock/panel.rs:112-131). The entity handle built by PanelViewOf
	// answers with its gpui.Entity[S].
	AsAny() any
}

// entityPanelView is the blanket impl PanelView for Entity<T: Panel>
// (U:dock/panel.rs:134-190).
type entityPanelView[S any, PS interface {
	*S
	Panel[S]
}] struct {
	entity gpui.Entity[S]
}

// PanelViewOf wraps a panel entity in its object-safe handle
// (U:dock/panel.rs:134 impl PanelView for Entity<T>).
func PanelViewOf[S any, PS interface {
	*S
	Panel[S]
}](entity gpui.Entity[S]) PanelView {
	return &entityPanelView[S, PS]{entity: entity}
}

// PanelEntityOf recovers the panel entity behind a handle built by
// PanelViewOf (U:dock/panel.rs:198-202 From<&dyn PanelView> for Entity<T>).
func PanelEntityOf[S any](view PanelView) (gpui.Entity[S], bool) {
	entity, ok := view.AsAny().(gpui.Entity[S])
	return entity, ok
}

func (v *entityPanelView[S, PS]) PanelName(app *gpui.App) string {
	return v.entity.Read(app, func(s *S, _ *gpui.App) string { return PS(s).PanelName() })
}

func (v *entityPanelView[S, PS]) PanelID(*gpui.App) PanelID { return PanelIDOf(v.entity.EntityID()) }

func (v *entityPanelView[S, PS]) Closable(app *gpui.App) bool {
	return v.entity.Read(app, func(s *S, app *gpui.App) bool {
		if p, ok := any(s).(PanelClosable); ok {
			return p.Closable(app)
		}
		return true
	})
}

func (v *entityPanelView[S, PS]) Zoomable(app *gpui.App) bool {
	return v.entity.Read(app, func(s *S, app *gpui.App) bool {
		if p, ok := any(s).(PanelZoomable); ok {
			return p.Zoomable(app)
		}
		return true
	})
}

func (v *entityPanelView[S, PS]) Visible(app *gpui.App) bool {
	return v.entity.Read(app, func(s *S, app *gpui.App) bool {
		if p, ok := any(s).(PanelVisible); ok {
			return p.Visible(app)
		}
		return true
	})
}

func (v *entityPanelView[S, PS]) SetActive(active bool, w *gpui.Window, app *gpui.App) {
	v.entity.Update(app, func(s *S, cx *gpui.Context[S]) {
		if p, ok := any(s).(PanelSetActive[S]); ok {
			p.SetActive(active, w, cx)
		}
	})
}

func (v *entityPanelView[S, PS]) SetZoomed(zoomed bool, w *gpui.Window, app *gpui.App) {
	v.entity.Update(app, func(s *S, cx *gpui.Context[S]) {
		if p, ok := any(s).(PanelSetZoomed[S]); ok {
			p.SetZoomed(zoomed, w, cx)
		}
	})
}

func (v *entityPanelView[S, PS]) OnAddedTo(group gpui.WeakEntity[TabGroup], w *gpui.Window, app *gpui.App) {
	v.entity.Update(app, func(s *S, cx *gpui.Context[S]) {
		if p, ok := any(s).(PanelOnAddedTo[S]); ok {
			p.OnAddedTo(group, w, cx)
		}
	})
}

func (v *entityPanelView[S, PS]) OnRemoved(w *gpui.Window, app *gpui.App) {
	v.entity.Update(app, func(s *S, cx *gpui.Context[S]) {
		if p, ok := any(s).(PanelOnRemoved[S]); ok {
			p.OnRemoved(w, cx)
		}
	})
}

func (v *entityPanelView[S, PS]) View() gpui.View { return gpui.ViewOf[S, PS](v.entity) }

func (v *entityPanelView[S, PS]) FocusHandle(app *gpui.App) gpui.FocusHandle {
	return v.entity.Read(app, func(s *S, app *gpui.App) gpui.FocusHandle { return PS(s).FocusHandle(app) })
}

func (v *entityPanelView[S, PS]) Dump(app *gpui.App) PanelState {
	return v.entity.Read(app, func(s *S, app *gpui.App) PanelState { return panelDump[S, PS](s, app) })
}

func (v *entityPanelView[S, PS]) AsAny() any { return v.entity }

// panelDump is Panel::dump with its default (U:dock/panel.rs:90-92).
func panelDump[S any, PS interface {
	*S
	Panel[S]
}](s *S, app *gpui.App) PanelState {
	if p, ok := any(s).(PanelDump); ok {
		return p.Dump(app)
	}
	return NewPanelState(PS(s).PanelName())
}

// PanelViewsEqual is PartialEq for dyn PanelView (U:dock/panel.rs:204-208):
// two handles are equal when they name the same panel.
func PanelViewsEqual(a, b PanelView, app *gpui.App) bool {
	return a.PanelID(app) == b.PanelID(app)
}

// livePanels reads panel properties out of the live panel map DockArea
// keeps (U:dock/panel.rs:210-245): the PanelSource DockArea.Dump writes
// against.
type livePanels struct {
	panels map[PanelID]PanelView
	app    *gpui.App
}

func (l livePanels) PanelName(id PanelID) string {
	if panel, ok := l.panels[id]; ok {
		return panel.PanelName(l.app)
	}
	return ""
}

func (l livePanels) IsVisible(id PanelID) bool {
	panel, ok := l.panels[id]
	return ok && panel.Visible(l.app)
}

func (l livePanels) Dump(id PanelID) PanelState {
	if panel, ok := l.panels[id]; ok {
		return panel.Dump(l.app)
	}
	return PanelState{}
}
