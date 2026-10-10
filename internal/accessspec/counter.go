// Package accessspec exercises the element accessibility slice
// (ticket21) against the real gpui runtime: a counter/editor model
// entity rendered by a stateless root view whose tree is composed of
// typed custom elements wrapped in gpui.Accessibility, driven through
// the real element phases (request_layout → compute → prepaint →
// paint) by DrawWindowFrame, with the semantic tree published by
// PublishWindowA11y and semantic actions dispatched through
// generation-checked foreground tokens.
//
// The corpus mirrors the CE a11y example shape (the
// crates/gpui/src/_accessibility.rs guide: a custom view publishes
// nodes with stable ids derived from the element id path, a button
// answers Click, an editor presents TextInput + synthetic TextRun
// children, and hidden subtrees keep their focus). No native UIA claim
// is made here: the published snapshot is the observable seam.
package accessspec

import (
	"fmt"
	"strings"

	"gpui-go/gpui"
)

// a11yHandlerRuns counts listener invocations independent of entity
// lifetime (the a11y tests assert stale dispatch never runs handlers).
// Package-level because the Click listener closure increments it.
var a11yHandlerRuns int

// ---------------------------------------------------------------------------
// The model (a counter/editor entity shared by the render tree)
// ---------------------------------------------------------------------------

// Counter is the persistent model: the count, the editor text and
// caret, the hidden/reinsertion toggles, the two focus handles, and the
// action observables the tests assert on.
type Counter struct {
	count       int
	text        string
	caret       int
	hiddenLabel bool
	showEditor  bool

	incFocus    gpui.FocusHandle
	editorFocus gpui.FocusHandle

	// clickRuns counts Click actions delivered through tokens.
	clickRuns int
	// valueRuns/lastValue record SetValue deliveries.
	valueRuns  int
	lastValue  string
	lastCaret  int
	replaceRun int
}

// newCounter builds the model entity with window-owned focus handles.
func newCounter(app *gpui.App, w *gpui.Window) gpui.Entity[Counter] {
	return gpui.NewEntity(app, w.Scope(), func(c *Counter, cx *gpui.Context[Counter]) {
		c.text = "hello a11y world"
		c.showEditor = true
		c.incFocus = gpui.NewFocusHandle(w)
		c.editorFocus = gpui.NewFocusHandle(w)
	})
}

// counterFacts is the read-only snapshot the render reads each frame.
type counterFacts struct {
	count       int
	text        string
	caret       int
	hiddenLabel bool
	showEditor  bool
	incFocus    gpui.FocusHandle
	editorFocus gpui.FocusHandle
}

// readCounterFacts reads the model through the entity (reads register
// dependencies; the render is re-driven by notification-driven draws).
func readCounterFacts(counter gpui.Entity[Counter], cx gpui.AppContext) counterFacts {
	return counter.Read(cx, func(c *Counter, _ *gpui.App) counterFacts {
		return counterFacts{
			count:       c.count,
			text:        c.text,
			caret:       c.caret,
			hiddenLabel: c.hiddenLabel,
			showEditor:  c.showEditor,
			incFocus:    c.incFocus,
			editorFocus: c.editorFocus,
		}
	})
}

// ---------------------------------------------------------------------------
// The root view (stateless: identical element id paths per window)
// ---------------------------------------------------------------------------

// counterRootView is the window root: a stateless View whose recipe
// reads the counter entity and renders the accessibility tree. Views
// without a backing entity are isolated by type name
// (view.go statelessView), so two windows rendering this same recipe
// type produce IDENTICAL element id paths — and therefore identical
// a11y node ids — which is exactly what the per-window generation
// separation test needs.
type counterRootView struct {
	counter gpui.Entity[Counter]
}

// EntityID implements gpui.View: stateless recipe.
func (v *counterRootView) EntityID() (gpui.EntityID, bool) { return 0, false }

// RenderOnce implements gpui.View.
func (v *counterRootView) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	facts := readCounterFacts(v.counter, app)
	children := []gpui.AnyElement{
		gpui.Accessibility(&countLabel{value: fmt.Sprintf("Count: %d", facts.count), hidden: facts.hiddenLabel}),
		gpui.Accessibility(&incrementButton{counter: v.counter, focus: facts.incFocus}),
	}
	if facts.showEditor {
		children = append(children, gpui.Accessibility(&editorElement{
			counter: v.counter,
			text:    facts.text,
			caret:   facts.caret,
			focus:   facts.editorFocus,
		}))
	}
	return gpui.Accessibility(&appContainer{children: children})
}

// ---------------------------------------------------------------------------
// The container element (custom phases, GenericContainer node)
// ---------------------------------------------------------------------------

// appContainer is the semantic root container: a custom element with
// typed phases driving its children through the same phases, wrapped
// by gpui.Accessibility so it publishes a GenericContainer node.
type appContainer struct {
	children []gpui.AnyElement
}

// ID implements gpui.Element (the node id derives from this path).
func (e *appContainer) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("counter-app"), true
}

// SourceLocation implements gpui.Element.
func (e *appContainer) SourceLocation() *gpui.SourceLocation { return nil }

// A11ySpec implements gpui.A11yCompanion: the container node.
func (e *appContainer) A11ySpec() gpui.A11ySpec {
	return gpui.A11ySpec{Role: gpui.RoleGenericContainer, AuthorID: "counter-app"}
}

// RequestLayout implements gpui.Element: request every child, then the
// container's own flex-column node over them.
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
		panic(fmt.Sprintf("accessspec: container request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements gpui.Element: drive the children's prepaint
// (their drawables resolve their own committed bounds).
func (e *appContainer) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	for _, child := range e.children {
		child.Prepaint(w, app)
	}
	return struct{}{}
}

// Paint implements gpui.Element: drive the children's paint (where the
// a11y focus/action registrations land).
func (e *appContainer) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
	for _, child := range e.children {
		child.Paint(w, app)
	}
}

// LayoutNodeStyle reports the container's node style for the window
// root stretch (window roots fill the window when their size is auto).
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
// The count label (StaticText node, hidden-subtree carrier)
// ---------------------------------------------------------------------------

// countLabel publishes a StaticText node carrying the count value; the
// hidden flag exercises aria_hidden semantics (hidden nodes keep their
// stable id and, per the guide, keep focus and input handlers).
type countLabel struct {
	value  string
	hidden bool
}

// ID implements gpui.Element.
func (e *countLabel) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("count-label"), true
}

// SourceLocation implements gpui.Element.
func (e *countLabel) SourceLocation() *gpui.SourceLocation { return nil }

// A11ySpec implements gpui.A11yCompanion.
func (e *countLabel) A11ySpec() gpui.A11ySpec {
	return gpui.A11ySpec{Role: gpui.RoleStaticText, Value: e.value, Hidden: e.hidden, AuthorID: "count-label"}
}

// RequestLayout implements gpui.Element: a content-sized leaf row.
func (e *countLabel) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size = gpui.LengthSize{Width: gpui.PxLength(160), Height: gpui.PxLength(24)}
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("accessspec: label request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements gpui.Element.
func (e *countLabel) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	return struct{}{}
}

// Paint implements gpui.Element (nothing to paint; the scene stays
// empty in this corpus — the a11y tree is the observable).
func (e *countLabel) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *struct{}, w *gpui.Window, app *gpui.App) {
}

// ---------------------------------------------------------------------------
// The increment button (Button node, focusable, Click/Increment actions)
// ---------------------------------------------------------------------------

// incrementButton publishes a focusable Button node and answers Click
// and Increment actions by updating the counter entity — the voice-
// control path of the pinned guide's "Handling actions" example.
type incrementButton struct {
	counter gpui.Entity[Counter]
	focus   gpui.FocusHandle
}

// ID implements gpui.Element.
func (e *incrementButton) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("increment"), true
}

// SourceLocation implements gpui.Element.
func (e *incrementButton) SourceLocation() *gpui.SourceLocation { return nil }

// A11ySpec implements gpui.A11yCompanion (label + focus binding).
func (e *incrementButton) A11ySpec() gpui.A11ySpec {
	spec := gpui.A11ySpec{Role: gpui.RoleButton, Label: "Increment", AuthorID: "counter.increment"}
	return spec.WithFocus(e.focus)
}

// A11yActionRegistrations implements gpui.A11yActions: this frame's
// listeners, built at paint time with the prepaint state available
// (div.rs a11y_action_listeners, registered in Interactivity::paint).
func (e *incrementButton) A11yActionRegistrations(prepaint *struct{}) []gpui.A11yActionRegistration {
	increment := func(data *gpui.A11yActionData, w *gpui.Window, app *gpui.App) {
		e.counter.Update(app, func(c *Counter, cx *gpui.Context[Counter]) {
			c.count++
			if data != nil {
				c.lastValue = data.Value
			}
			if e.focus.ID() != 0 {
				w.Focus(e.focus)
			}
			cx.Notify()
		})
	}
	return []gpui.A11yActionRegistration{
		{Action: gpui.A11yActionClick, Listener: func(data *gpui.A11yActionData, w *gpui.Window, app *gpui.App) {
			a11yHandlerRuns++
			e.counter.Update(app, func(c *Counter, cx *gpui.Context[Counter]) {
				c.clickRuns++
				cx.Notify()
			})
			increment(data, w, app)
		}},
		{Action: gpui.A11yActionIncrement, Listener: increment},
	}
}

// RequestLayout implements gpui.Element: a fixed-size button row.
func (e *incrementButton) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size = gpui.LengthSize{Width: gpui.PxLength(96), Height: gpui.PxLength(32)}
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("accessspec: button request_layout: %v", err))
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
// The editor (TextInput node with synthetic TextRun children)
// ---------------------------------------------------------------------------

// editorPrepaint is the editor's prepaint state: the visible runs the
// synthetic children derive from (the guide's a11y_synthetic_children
// example uses prepaint to determine what is on screen).
type editorPrepaint struct {
	runs  []string
	caret int
}

// editorElement publishes a focusable TextInput node whose value is the
// editor text, contributes one synthetic TextRun child per word AFTER
// prepaint, mutates its own node record (the caret description, the
// guide's builder.parent_node() usage), and answers SetValue /
// ReplaceSelectedText actions.
type editorElement struct {
	counter gpui.Entity[Counter]
	text    string
	caret   int
	focus   gpui.FocusHandle
}

// ID implements gpui.Element.
func (e *editorElement) ID() (gpui.ElementID, bool) {
	return gpui.NameElementID("editor"), true
}

// SourceLocation implements gpui.Element.
func (e *editorElement) SourceLocation() *gpui.SourceLocation { return nil }

// A11ySpec implements gpui.A11yCompanion.
func (e *editorElement) A11ySpec() gpui.A11ySpec {
	spec := gpui.A11ySpec{Role: gpui.RoleTextInput, Value: e.text, AuthorID: "editor", Label: "Editor"}
	return spec.WithFocus(e.focus)
}

// RequestLayout implements gpui.Element.
func (e *editorElement) RequestLayout(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	style := gpui.DefaultStyle()
	style.Size = gpui.LengthSize{Width: gpui.PxLength(240), Height: gpui.PxLength(28)}
	id, err := gpui.RequestElementLayout(w, style)
	if err != nil {
		panic(fmt.Sprintf("accessspec: editor request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements gpui.Element: derive the visible runs from the
// text (prepaint state feeds the synthetic children).
func (e *editorElement) Prepaint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, w *gpui.Window, app *gpui.App) editorPrepaint {
	runs := strings.Fields(e.text)
	return editorPrepaint{runs: runs, caret: e.caret}
}

// A11ySyntheticChildren implements gpui.A11ySynthetic: one TextRun node
// per run, ids derived from (parent id, index), plus a parent-node
// mutation recording the caret.
func (e *editorElement) A11ySyntheticChildren(prepaint *editorPrepaint, builder *gpui.A11ySubtree) {
	for i, run := range prepaint.runs {
		id := builder.SyntheticNodeID(i)
		builder.PushChild(id, gpui.A11ySpec{Role: gpui.RoleTextRun, Value: run})
	}
	builder.Parent().Description = fmt.Sprintf("caret at byte %d", prepaint.caret)
}

// A11yActionRegistrations implements gpui.A11yActions.
func (e *editorElement) A11yActionRegistrations(prepaint *editorPrepaint) []gpui.A11yActionRegistration {
	setValue := func(data *gpui.A11yActionData, w *gpui.Window, app *gpui.App) {
		if data == nil {
			return
		}
		e.counter.Update(app, func(c *Counter, cx *gpui.Context[Counter]) {
			c.text = data.Value
			c.valueRuns++
			c.lastValue = data.Value
			cx.Notify()
		})
	}
	replaceSelected := func(data *gpui.A11yActionData, w *gpui.Window, app *gpui.App) {
		if data == nil {
			return
		}
		e.counter.Update(app, func(c *Counter, cx *gpui.Context[Counter]) {
			caret := c.caret
			if caret > len(c.text) {
				caret = len(c.text)
			}
			c.text = c.text[:caret] + data.Value + c.text[caret:]
			c.caret = caret + len(data.Value)
			c.replaceRun++
			cx.Notify()
		})
	}
	focusEditor := func(data *gpui.A11yActionData, w *gpui.Window, app *gpui.App) {
		if e.focus.ID() != 0 {
			w.Focus(e.focus)
		}
	}
	return []gpui.A11yActionRegistration{
		{Action: gpui.A11yActionSetValue, Listener: setValue},
		{Action: gpui.A11yActionReplaceSelectedText, Listener: replaceSelected},
		{Action: gpui.A11yActionFocus, Listener: focusEditor},
	}
}

// Paint implements gpui.Element.
func (e *editorElement) Paint(global *gpui.GlobalElementID, inspector *gpui.InspectorElementID, bounds gpui.Bounds, layout *struct{}, prepaint *editorPrepaint, w *gpui.Window, app *gpui.App) {
}
