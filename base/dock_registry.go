package base

// Port of U:dock/registry.rs: the global registry of panel builders.

import (
	"github.com/hoangvu12/gpui-go/gpui"
)

// PanelBuildContext is everything a panel builder needs to reconstruct a
// panel from persisted data (U:dock/registry.rs:7-38).
type PanelBuildContext struct {
	dockArea gpui.WeakEntity[DockArea]
	state    *PanelState
	info     *PanelInfo
}

// NewPanelBuildContext is PanelBuildContext::new (U:dock/registry.rs:15-25).
func NewPanelBuildContext(dockArea gpui.WeakEntity[DockArea], state *PanelState, info *PanelInfo) PanelBuildContext {
	return PanelBuildContext{dockArea: dockArea, state: state, info: info}
}

// DockArea is U:dock/registry.rs:27-29.
func (c PanelBuildContext) DockArea() gpui.WeakEntity[DockArea] { return c.dockArea }

// State is U:dock/registry.rs:31-33.
func (c PanelBuildContext) State() *PanelState { return c.state }

// Info is U:dock/registry.rs:35-37.
func (c PanelBuildContext) Info() *PanelInfo { return c.info }

// PanelBuildFunc builds a panel from persisted data
// (U:dock/registry.rs:59-64 the registered closure).
type PanelBuildFunc func(context PanelBuildContext, w *gpui.Window, app *gpui.App) PanelView

// PanelRegistry is the global registry of panel builders, keyed by panel
// name (U:dock/registry.rs:40-64). It is stored as the *PanelRegistry
// global. Building a panel that was never registered returns false
// rather than a placeholder.
type PanelRegistry struct {
	items map[string]PanelBuildFunc
}

// NewPanelRegistry is PanelRegistry::new (U:dock/registry.rs:74-78).
func NewPanelRegistry() *PanelRegistry {
	return &PanelRegistry{items: make(map[string]PanelBuildFunc)}
}

// panelRegistryInit installs the global registry once
// (U:dock/registry.rs:67-72).
func panelRegistryInit(app *gpui.App) {
	if !gpui.HasGlobal[*PanelRegistry](app) {
		app.SetGlobal(NewPanelRegistry())
	}
}

// PanelRegistryGlobal is PanelRegistry::global and global_mut
// (U:dock/registry.rs:80-86): the registry is a pointer global.
func PanelRegistryGlobal(app *gpui.App) *PanelRegistry {
	return gpui.Global[*PanelRegistry](app)
}

// PanelRegistryBuildPanel builds a panel by name, reporting false when no
// builder is registered for panelName (U:dock/registry.rs:88-99).
func PanelRegistryBuildPanel(panelName string, context PanelBuildContext, w *gpui.Window, app *gpui.App) (PanelView, bool) {
	build, ok := PanelRegistryGlobal(app).items[panelName]
	if !ok {
		return nil, false
	}
	return build(context, w, app), true
}

// RegisterPanel registers the builder for panelName in the global
// registry (U:dock/registry.rs:110-119).
func RegisterPanel(app *gpui.App, panelName string, build PanelBuildFunc) {
	panelRegistryInit(app)
	PanelRegistryGlobal(app).items[panelName] = build
}
