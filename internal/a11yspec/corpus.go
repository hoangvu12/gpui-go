// Package a11yspec exercises the native UIA provider slice (ticket22)
// against real Win32 windows on the real host: the adapter creation at
// window setup, the activation-before-complete-tree flow driven by
// synthetic WM_GETOBJECT queries (SendMessage), the per-frame
// publication semantics of the ticket-21 snapshot through the native
// service, the queued-action drain routing into the registered
// listeners and the built-ins, the generation/staleness behavior, and
// close/reopen adapter lifetimes.
//
// Environment expectation (the winhostspec/desktopspec pattern): this
// session is interactive with the rev-10 native artifact embedded in
// the module. Window-creation and service-resolution failures FAIL
// the tests, never skip. Controlled events are delivered as synthetic
// messages (SendMessage with the UIA root object id -25 or
// OBJID_CLIENT -4); no real-cursor assertion is made anywhere (this
// machine's mouse hit tests are corrupted post-fullscreen — the
// synthetic-message discipline). Narrator is an opt-in evidence test
// behind GPUI_A11Y_NARRATOR=1 (run it in its own `go test -run`
// invocation: every real window's first draw holds one of the native
// atlas service's 8 process-wide slots, and the default suite already
// uses the budget).
package a11yspec

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
)

// ---------------------------------------------------------------------------
// The model (a shared entity the render tree reads and mutates)
// ---------------------------------------------------------------------------

// a11yModel is the persistent corpus model: the observables the action
// routing mutates and the tests assert on.
type a11yModel struct {
	// clickRuns counts Click listener deliveries.
	clickRuns int
	// incrementRuns counts Increment listener deliveries.
	incrementRuns int
	// valueRuns/lastValue record SetValue deliveries.
	valueRuns int
	lastValue string
	// selRuns/lastSelection record SetTextSelection deliveries.
	selectionRuns int
	lastSelection gpui.A11yTextSelection
	// showEditor toggles the editor node out of the tree (the
	// removal/reinsertion identity test).
	showEditor bool

	incFocus    gpui.FocusHandle
	editorFocus gpui.FocusHandle
}

// newA11yModel builds the model entity with window-owned focus handles.
func newA11yModel(app *gpui.App, w *gpui.Window) gpui.Entity[a11yModel] {
	return gpui.NewEntity(app, w.Scope(), func(m *a11yModel, cx *gpui.Context[a11yModel]) {
		m.showEditor = true
		m.incFocus = gpui.NewFocusHandle(w)
		m.editorFocus = gpui.NewFocusHandle(w)
	})
}

// modelFacts is the read-only view the render reads each frame.
type modelFacts struct {
	clickRuns     int
	incrementRuns int
	valueRuns     int
	lastValue     string
	selectionRuns int
	lastSelection gpui.A11yTextSelection
	showEditor    bool
	incFocus      gpui.FocusHandle
	editorFocus   gpui.FocusHandle
}

// readModelFacts reads the model through the entity.
func readModelFacts(model gpui.Entity[a11yModel], app *gpui.App) modelFacts {
	return model.Read(app, func(m *a11yModel, _ *gpui.App) modelFacts {
		return modelFacts{
			clickRuns:     m.clickRuns,
			incrementRuns: m.incrementRuns,
			valueRuns:     m.valueRuns,
			lastValue:     m.lastValue,
			selectionRuns: m.selectionRuns,
			lastSelection: m.lastSelection,
			showEditor:    m.showEditor,
			incFocus:      m.incFocus,
			editorFocus:   m.editorFocus,
		}
	})
}

// ---------------------------------------------------------------------------
// The root view (stateless: identical element id paths per window)
// ---------------------------------------------------------------------------

// a11yRootView is the window root: a stateless View (isolated by type
// name in view.go) so multiple windows of this recipe produce identical
// element id paths — the per-window provider/generation separation.
type a11yRootView struct {
	model gpui.Entity[a11yModel]
}

// EntityID implements gpui.View: stateless recipe.
func (v *a11yRootView) EntityID() (gpui.EntityID, bool) { return 0, false }

// RenderOnce implements gpui.View.
func (v *a11yRootView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	facts := readModelFacts(v.model, app)
	children := []gpui.AnyElement{
		gpui.Accessibility(&statusLabel{}),
		gpui.Accessibility(&incrementButton{model: v.model, focus: facts.incFocus}),
	}
	if facts.showEditor {
		children = append(children, gpui.Accessibility(&selectionEditor{model: v.model, focus: facts.editorFocus}))
	}
	return gpui.Accessibility(&appContainer{children: children})
}

// ---------------------------------------------------------------------------
// The container (GenericContainer node)
// ---------------------------------------------------------------------------

// appContainer is the semantic root container.
type appContainer struct {
	children []gpui.AnyElement
}

// ID implements gpui.Element.
func (e *appContainer) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("a11y-app"), true
}

// SourceLocation implements gpui.Element.
func (e *appContainer) SourceLocation() *gpui.SourceLocation { return nil }

// A11ySpec implements gpui.A11yCompanion.
func (e *appContainer) A11ySpec() gpui.A11ySpec {
	return gpui.A11ySpec{Role: gpui.RoleGenericContainer, AuthorID: "a11y-app"}
}

// RequestLayout implements gpui.Element: a flex column with padding.
func (e *appContainer) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	childIDs := make([]gpui.LayoutID, 0, len(e.children))
	for _, child := range e.children {
		childIDs = append(childIDs, child.RequestLayout(w, app))
	}
	style := gpui.DefaultStyle()
	style.FlexDirection = gpui.FlexDirectionColumn
	style.Gap = gpui.DefiniteLengthSize{Width: gpui.DefinitePx(8), Height: gpui.DefinitePx(8)}
	style.Padding = gpui.DefiniteLengthEdges{
		Top:    gpui.DefinitePx(12),
		Right:  gpui.DefinitePx(12),
		Bottom: gpui.DefinitePx(12),
		Left:   gpui.DefinitePx(12),
	}
	id, err := gpui.RequestElementLayout(w, style, childIDs...)
	if err != nil {
		panic(fmt.Sprintf("a11yspec: container request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements gpui.Element.
func (e *appContainer) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	for _, child := range e.children {
		child.Prepaint(w, app)
	}
	return struct{}{}
}

// Paint implements gpui.Element.
func (e *appContainer) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
	for _, child := range e.children {
		child.Paint(w, app)
	}
}

// LayoutNodeStyle reports the root stretch style (window roots fill
// the window when their size is auto).
func (e *appContainer) LayoutNodeStyle() (gpui.Style, bool) {
	style := gpui.DefaultStyle()
	style.FlexDirection = gpui.FlexDirectionColumn
	style.Gap = gpui.DefiniteLengthSize{Width: gpui.DefinitePx(8), Height: gpui.DefinitePx(8)}
	style.Padding = gpui.DefiniteLengthEdges{
		Top:    gpui.DefinitePx(12),
		Right:  gpui.DefinitePx(12),
		Bottom: gpui.DefinitePx(12),
		Left:   gpui.DefinitePx(12),
	}
	return style, true
}

// ---------------------------------------------------------------------------
// The status label (StaticText node, no listeners: the Click built-in
// fallback's target)
// ---------------------------------------------------------------------------

// statusLabel publishes a StaticText node with no action listeners and
// real layout bounds: dispatching Click on it reaches the built-in
// fallback (synthetic mouse events at the bounds center).
type statusLabel struct{}

// ID implements gpui.Element.
func (e *statusLabel) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("status"), true
}

// SourceLocation implements gpui.Element.
func (e *statusLabel) SourceLocation() *gpui.SourceLocation { return nil }

// A11ySpec implements gpui.A11yCompanion.
func (e *statusLabel) A11ySpec() gpui.A11ySpec {
	return gpui.A11ySpec{Role: gpui.RoleStaticText, Value: "Status: idle", AuthorID: "status"}
}

// RequestLayout implements gpui.Element: a fixed-size row.
func (e *statusLabel) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size = gpui.LengthSize{Width: gpui.PxLength(160), Height: gpui.PxLength(24)}
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("a11yspec: label request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements gpui.Element.
func (e *statusLabel) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	return struct{}{}
}

// Paint implements gpui.Element.
func (e *statusLabel) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
}

// ---------------------------------------------------------------------------
// The increment button (Button node, Click/Increment listeners)
// ---------------------------------------------------------------------------

// incrementButton publishes a focusable Button node and answers Click
// and Increment through listeners (the voice-control path of the
// pinned guide's "Handling actions").
type incrementButton struct {
	model gpui.Entity[a11yModel]
	focus gpui.FocusHandle
}

// ID implements gpui.Element.
func (e *incrementButton) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("increment"), true
}

// SourceLocation implements gpui.Element.
func (e *incrementButton) SourceLocation() *gpui.SourceLocation { return nil }

// A11ySpec implements gpui.A11yCompanion (label + focus binding).
func (e *incrementButton) A11ySpec() gpui.A11ySpec {
	spec := gpui.A11ySpec{Role: gpui.RoleButton, Label: "Increment", AuthorID: "increment"}
	return spec.WithFocus(e.focus)
}

// A11yActionRegistrations implements gpui.A11yActions.
func (e *incrementButton) A11yActionRegistrations(prepaint *struct{}) []gpui.A11yActionRegistration {
	return []gpui.A11yActionRegistration{
		{Action: gpui.A11yActionClick, Listener: func(data *gpui.A11yActionData, w *gpui.Window, app *gpui.App) {
			e.model.Update(app, func(m *a11yModel, cx *gpui.Context[a11yModel]) {
				m.clickRuns++
				if data != nil {
					m.lastValue = data.Value
				}
				cx.Notify()
			})
		}},
		{Action: gpui.A11yActionIncrement, Listener: func(data *gpui.A11yActionData, w *gpui.Window, app *gpui.App) {
			e.model.Update(app, func(m *a11yModel, cx *gpui.Context[a11yModel]) {
				m.incrementRuns++
				cx.Notify()
			})
		}},
	}
}

// RequestLayout implements gpui.Element: a fixed-size button row.
func (e *incrementButton) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size = gpui.LengthSize{Width: gpui.PxLength(96), Height: gpui.PxLength(32)}
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("a11yspec: button request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements gpui.Element.
func (e *incrementButton) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	return struct{}{}
}

// Paint implements gpui.Element.
func (e *incrementButton) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
}

// ---------------------------------------------------------------------------
// The selection editor (TextInput node, SetValue/SetTextSelection
// listeners)
// ---------------------------------------------------------------------------

// selectionEditor publishes a focusable TextInput node and answers
// SetValue and SetTextSelection through listeners (the payload decode
// of the native request).
type selectionEditor struct {
	model gpui.Entity[a11yModel]
	focus gpui.FocusHandle
}

// ID implements gpui.Element.
func (e *selectionEditor) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("editor"), true
}

// SourceLocation implements gpui.Element.
func (e *selectionEditor) SourceLocation() *gpui.SourceLocation { return nil }

// A11ySpec implements gpui.A11yCompanion.
func (e *selectionEditor) A11ySpec() gpui.A11ySpec {
	spec := gpui.A11ySpec{Role: gpui.RoleTextInput, Value: "select me", AuthorID: "editor", Label: "Editor"}
	return spec.WithFocus(e.focus)
}

// A11yActionRegistrations implements gpui.A11yActions.
func (e *selectionEditor) A11yActionRegistrations(prepaint *struct{}) []gpui.A11yActionRegistration {
	return []gpui.A11yActionRegistration{
		{Action: gpui.A11yActionSetValue, Listener: func(data *gpui.A11yActionData, w *gpui.Window, app *gpui.App) {
			if data == nil {
				return
			}
			e.model.Update(app, func(m *a11yModel, cx *gpui.Context[a11yModel]) {
				m.valueRuns++
				m.lastValue = data.Value
				cx.Notify()
			})
		}},
		{Action: gpui.A11yActionSetTextSelection, Listener: func(data *gpui.A11yActionData, w *gpui.Window, app *gpui.App) {
			if data == nil || data.TextSelection == nil {
				return
			}
			e.model.Update(app, func(m *a11yModel, cx *gpui.Context[a11yModel]) {
				m.selectionRuns++
				m.lastSelection = *data.TextSelection
				cx.Notify()
			})
		}},
	}
}

// RequestLayout implements gpui.Element: a fixed-size editor row.
func (e *selectionEditor) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size = gpui.LengthSize{Width: gpui.PxLength(240), Height: gpui.PxLength(28)}
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("a11yspec: editor request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements gpui.Element.
func (e *selectionEditor) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	return struct{}{}
}

// Paint implements gpui.Element.
func (e *selectionEditor) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
}
