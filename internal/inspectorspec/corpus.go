// Package inspectorspec exercises the inspector and debug diagnostics
// slice (ticket27) against the real gpui runtime: a virtualized
// custom-component corpus (ticket14's UniformList retained-state
// machinery) drawn through the real element phases with the inspector
// toggled on, its element tree recorded from the real prepaint bounds,
// selection/picking driven through the window's real hit-test
// machinery, and the leak/cycle + debug-frame-overlay diagnostics over
// the real entity map and scene.
//
// The corpus mirrors the pinned reference's inspector semantics
// (crates/gpui/src/inspector.rs, window.rs 7201-7393 and the
// div.rs:2672-2688 DivInspectorState pattern): custom elements carry
// source locations (the long-id participation), insert hitboxes,
// and write the active element's typed inspector state from their real
// phases. No reference fixture exists for the inspector family in this
// repository, so observable comparisons pin the port's own
// deterministic traces and the debug_overlay.rs unit-test expectations
// (which are text-exact).
package inspectorspec

import (
	"fmt"

	"gpui-go/gpui"
)

// listHeight is the virtualized list's definite height: 30px header +
// 270px list fills the 300px corpus column, so a 20px item pitch shows
// items 0..13 (the virtualization window).
const listHeight = 270.0

// The corpus source locations (every element of one kind reports the
// same location, exactly like the pin's macro-captured locations: the
// instance ids disambiguate them).
var (
	containerSource = gpui.SourceLocation{File: "inspectorspec/corpus.go", Line: 70}
	headerSource    = gpui.SourceLocation{File: "inspectorspec/corpus.go", Line: 200}
	itemSource      = gpui.SourceLocation{File: "inspectorspec/corpus.go", Line: 330}
)

// PanelModel is the corpus model entity: the item list, the uniform
// list's scroll handle and the mutation flags the tests flip.
type PanelModel struct {
	// items are the list item labels.
	items []string
	// itemHeight is the per-item pitch (mutable so bounds updates are
	// observable through the selection report).
	itemHeight float32
	// showList toggles the whole virtualized subtree.
	showList bool
	// containerName selects the container's element id ("main" or
	// "alt"): flipping it reparents every list item under a different
	// identity path.
	containerName string
	// handle is the uniform list's scroll state.
	handle *gpui.UniformListScrollHandle
	// panicNextRender flips the corpus to the panicking build (the
	// abandoned-frame test).
	panicNextRender bool
}

// panelFacts is the read-only snapshot the render reads each frame.
type panelFacts struct {
	items           []string
	itemHeight      float32
	showList        bool
	containerName   string
	handle          *gpui.UniformListScrollHandle
	panicNextRender bool
}

// newPanelModel builds the corpus model with count items in the
// application's root scope.
func newPanelModel(app *gpui.App, count int) gpui.Entity[PanelModel] {
	handle := gpui.NewUniformListScrollHandle()
	items := make([]string, count)
	for i := range items {
		items[i] = fmt.Sprintf("item-%d", i)
	}
	return gpui.NewEntity(app, app.RootScope(), func(m *PanelModel, cx *gpui.Context[PanelModel]) {
		m.items = items
		m.itemHeight = 20
		m.showList = true
		m.containerName = "main"
		m.handle = handle
	})
}

// readPanelFacts reads the model through the entity.
func readPanelFacts(model gpui.Entity[PanelModel], cx gpui.AppContext) panelFacts {
	return model.Read(cx, func(m *PanelModel, _ *gpui.App) panelFacts {
		return panelFacts{
			items:           m.items,
			itemHeight:      m.itemHeight,
			showList:        m.showList,
			containerName:   m.containerName,
			handle:          m.handle,
			panicNextRender: m.panicNextRender,
		}
	})
}

// ---------------------------------------------------------------------------
// The root view (stateless: identical element id paths per window)
// ---------------------------------------------------------------------------

// panelView is the window root: a stateless View whose recipe reads the
// model and renders the custom container with the header and the
// virtualized list (or the panicking build).
type panelView struct {
	model gpui.Entity[PanelModel]
}

// EntityID implements gpui.View: stateless recipe.
func (v *panelView) EntityID() (gpui.EntityID, bool) { return 0, false }

// RenderOnce implements gpui.View.
func (v *panelView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	facts := readPanelFacts(v.model, app)
	if facts.panicNextRender {
		return gpui.Div().Child(gpui.Empty()).Child(panickingElement()).IntoElement()
	}
	children := []gpui.AnyElement{
		gpui.CustomElement[struct{}, struct{}](&headerElement{model: v.model}),
	}
	if facts.showList {
		children = append(children, gpui.UniformList(
			facts.handle,
			len(facts.items),
			func(start, end int, w *gpui.Window, app *gpui.App) []gpui.AnyElement {
				items := make([]gpui.AnyElement, 0, end-start)
				for i := start; i < end; i++ {
					items = append(items, gpui.CustomElement[struct{}, itemPrepaint](
						&itemElement{label: facts.items[i], height: facts.itemHeight}))
				}
				return items
			},
		).W(gpui.Length{Definite: gpui.Fraction(1.0)}).H(gpui.PxLength(listHeight)).IntoElement())
	}
	return gpui.CustomElement[struct{}, struct{}](&containerElement{
		name:     facts.containerName,
		children: children,
	})
}

// panickingElement returns an element whose paint panics (the
// abandoned build, the listspec precedent).
func panickingElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, struct{}](&panicElement{})
}

type panicElement struct{}

// ID implements Element.
func (e *panicElement) ID() (gpui.ElementID, bool) { return gpui.ElementID{}, false }

// SourceLocation implements Element.
func (e *panicElement) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout implements Element.
func (e *panicElement) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	id, err := gpui.RequestElementLayout(w, gpui.DefaultStyle())
	if err != nil {
		panic(err)
	}
	return id, struct{}{}
}

// Prepaint implements Element.
func (e *panicElement) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	return struct{}{}
}

// Paint implements Element: the build fails here.
func (e *panicElement) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
	panic("gpui: abandoned build (inspectorspec)")
}

// LayoutNodeStyle implements the root-stretch report.
func (e *panicElement) LayoutNodeStyle() (gpui.Style, bool) { return gpui.DefaultStyle(), true }

// ---------------------------------------------------------------------------
// The container element (a flex column with a stable element id)
// ---------------------------------------------------------------------------

// containerElement is the corpus root container: a flex column with a
// NAME identity and a source location, so it and its subtree join the
// inspector tree and the container-name flip reparents the items.
type containerElement struct {
	name     string
	children []gpui.AnyElement
}

// ID implements Element: the container's stable name identity.
func (e *containerElement) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID(e.name), true
}

// SourceLocation implements Element: the container participates in the
// long-id paths.
func (e *containerElement) SourceLocation() *gpui.SourceLocation {
	location := containerSource
	return &location
}

// RequestLayout implements Element: request the children, then the
// column node sized to the corpus window.
func (e *containerElement) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.FlexDirection = gpui.FlexDirectionColumn
	style.Size.Width = gpui.Length{Definite: gpui.Fraction(1.0)}
	style.Size.Height = gpui.Length{Definite: gpui.Fraction(1.0)}
	childIDs := make([]gpui.LayoutID, 0, len(e.children))
	for _, child := range e.children {
		childIDs = append(childIDs, child.RequestLayout(w, app))
	}
	id, err := gpui.RequestElementLayout(w, style, childIDs...)
	if err != nil {
		panic(fmt.Sprintf("inspectorspec: container request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements Element.
func (e *containerElement) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	// The container's own hitbox, inserted BEFORE the children so it
	// sits behind them in the hit-test order (the div-like stacking the
	// pick-depth walk steps through).
	box := w.InsertHitbox(bounds, gpui.HitboxNormal)
	w.InsertInspectorHitbox(box, inspector)
	for _, child := range e.children {
		child.Prepaint(w, app)
	}
	return struct{}{}
}

// Paint implements Element.
func (e *containerElement) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
	for _, child := range e.children {
		child.Paint(w, app)
	}
}

// LayoutNodeStyle implements the root-stretch report.
func (e *containerElement) LayoutNodeStyle() (gpui.Style, bool) {
	style := gpui.DefaultStyle()
	style.FlexDirection = gpui.FlexDirectionColumn
	style.Size.Width = gpui.Length{Definite: gpui.Fraction(1.0)}
	style.Size.Height = gpui.Length{Definite: gpui.Fraction(1.0)}
	return style, true
}

// ---------------------------------------------------------------------------
// The header element (a stable non-virtualized node)
// ---------------------------------------------------------------------------

// headerElement is a fixed-size element with a NAME identity, a source
// location, a hitbox and a typed inspector state (the non-virtualized
// selection anchor). It also writes the strong-handle state the
// no-strong-cycle test exercises.
type headerElement struct {
	// model is the corpus model (the strong handle the typed state
	// retains while the header is the inspected element).
	model gpui.Entity[PanelModel]
}

// ID implements Element.
func (e *headerElement) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("header"), true
}

// SourceLocation implements Element.
func (e *headerElement) SourceLocation() *gpui.SourceLocation {
	location := headerSource
	return &location
}

// RequestLayout implements Element: a definite 200x30 node.
func (e *headerElement) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size.Width = gpui.PxLength(320)
	style.Size.Height = gpui.PxLength(30)
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("inspectorspec: header request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements Element: insert the hitbox, register it for
// inspector picking and write the DivInspectorState-pattern state.
func (e *headerElement) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	box := w.InsertHitbox(bounds, gpui.HitboxNormal)
	w.InsertInspectorHitbox(box, inspector)
	recordInspectorState(w, inspector, "header", bounds)
	// The strong-handle state (the diagnostics-retention observable: the
	// state holds an entity handle while the header is inspected; window
	// closure drops it with the inspector state, so no strong cycle
	// survives the teardown).
	gpui.WithInspectorState(w, inspector, func(state *HeldEntityState, exists bool, w *gpui.Window) (struct{}, *HeldEntityState) {
		return struct{}{}, &HeldEntityState{Model: e.model}
	})
	return struct{}{}
}

// Paint implements Element.
func (e *headerElement) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
}

// LayoutNodeStyle implements the root-stretch report.
func (e *headerElement) LayoutNodeStyle() (gpui.Style, bool) {
	style := gpui.DefaultStyle()
	style.Size.Width = gpui.PxLength(320)
	style.Size.Height = gpui.PxLength(30)
	return style, true
}

// ---------------------------------------------------------------------------
// The virtualized item element (the inspected cached/virtualized node)
// ---------------------------------------------------------------------------

// itemPrepaint is the item's prepaint phase state (the retained-state
// observable written per frame).
type itemPrepaint struct{}

// itemElement is one virtualized list item: anonymous in its own right
// (the uniform list's per-index wrapper supplies the identity), with a
// source location, a hitbox, keyed retained state and the typed
// inspector state of the active element (the div.rs
// DivInspectorState pattern: bounds and content at prepaint).
type itemElement struct {
	label  string
	height float32
}

// ID implements Element: the list wrapper stamps the per-index id.
func (e *itemElement) ID() (gpui.ElementID, bool) { return gpui.ElementID{}, false }

// SourceLocation implements Element: every item shares this code
// location, so the long-id INSTANCE ids disambiguate same-path items.
func (e *itemElement) SourceLocation() *gpui.SourceLocation {
	location := itemSource
	return &location
}

// RequestLayout implements Element: a definite-height leaf.
func (e *itemElement) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size.Width = gpui.Length{Definite: gpui.Fraction(1.0)}
	style.Size.Height = gpui.Length{Definite: gpui.DefinitePx(e.height)}
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("inspectorspec: item request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements Element: the hitbox, the inspector hitbox, the
// keyed retained state and the typed inspector state.
func (e *itemElement) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) itemPrepaint {
	box := w.InsertHitbox(bounds, gpui.HitboxNormal)
	w.InsertInspectorHitbox(box, inspector)
	recordInspectorState(w, inspector, e.label, bounds)
	// The keyed retained state (ticket14 machinery): a per-path hover
	// counter that survives frames while the item keeps rendering and
	// starts fresh after the frame-end retention drops it.
	gpui.WithOptionalElementState[int](w, global, func(state *int, w *gpui.Window) (struct{}, *int) {
		count := 1
		if state != nil {
			count = *state + 1
		}
		return struct{}{}, &count
	})
	return itemPrepaint{}
}

// Paint implements Element.
func (e *itemElement) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *itemPrepaint, w *gpui.Window, app *gpui.App) {
}

// LayoutNodeStyle implements the root-stretch report.
func (e *itemElement) LayoutNodeStyle() (gpui.Style, bool) {
	style := gpui.DefaultStyle()
	style.Size.Width = gpui.Length{Definite: gpui.Fraction(1.0)}
	style.Size.Height = gpui.Length{Definite: gpui.DefinitePx(e.height)}
	return style, true
}

// ---------------------------------------------------------------------------
// The typed inspector state (div.rs DivInspectorState)
// ---------------------------------------------------------------------------

// ItemInspectorState is the typed state the corpus elements write into
// the inspector's ACTIVE element while they are the inspected one (the
// div.rs DivInspectorState pattern: the content recorded at
// request_layout, the bounds and content size at prepaint, written only
// when the element's inspector id is the active one).
type ItemInspectorState struct {
	// Kind names the writing element kind ("header" or an item label).
	Kind string
	// Bounds is the element's committed bounds at the last live write.
	Bounds gpui.Bounds
	// Writes counts the live writes since the selection.
	Writes int
}

// HeldEntityState is a typed inspector state holding a STRONG entity
// handle (the strong-reference retention the window-closure test
// exercises: the inspector must drop it with the window so no strong
// cycle survives teardown).
type HeldEntityState struct {
	// Model is the retained handle.
	Model gpui.Entity[PanelModel]
}

// recordInspectorState writes the element's current facts into the
// active element's typed state (WithInspectorState is a no-op pass of
// a nil state unless this element IS the active one).
func recordInspectorState(w *gpui.Window, inspector *gpui.InspectorElementID, kind string, bounds gpui.Bounds) {
	gpui.WithInspectorState(w, inspector, func(state *ItemInspectorState, exists bool, w *gpui.Window) (struct{}, *ItemInspectorState) {
		next := &ItemInspectorState{Kind: kind, Bounds: bounds, Writes: 1}
		if state != nil {
			next.Writes = state.Writes + 1
		}
		return struct{}{}, next
	})
}
