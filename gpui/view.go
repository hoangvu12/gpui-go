package gpui

import (
	"fmt"
)

// This file is the port's view runtime (ticket11): the Render/RenderOnce
// roles, the public View interface with the ViewOf/ViewOfIn/
// ManagedViewOf adapters, and the view element that hooks a view into
// the element phases.
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/view.rs — the View trait (entity_id + consuming
//     render), the blanket View impls (RenderOnce recipes are stateless
//     views; Entity<T: Render> is a view keyed on its own id), the
//     ViewElement (RequestLayoutState/PrepaintState = Option<AnyElement>;
//     the stateful path renders inside with_rendered_view and re-requests
//     the rendered element; the stateless path isolates the subtree by
//     type name through window.with_id), and AnyView/any_view::render
//     (view.update(cx, |view, cx| view.render(window, cx)));
//   - crates/gpui/src/element.rs — Render (entity state render) and
//     RenderOnce (recipe render);
//   - crates/gpui/src/app.rs — open_window: the root view is built
//     inside the window's context and the window draws once before the
//     open returns.
//
// Go adaptations (from the resolved authoring contract): View is a
// concrete interface with EntityID() and a consuming RenderOnce; the
// entity adapter connects *S to Render[S]; recipes get a stateless
// wrapper without identity.

// Render is implemented by entity state that renders itself into an
// element tree (the reference Render).
type Render[S any] interface {
	// Render renders this view's element tree.
	Render(w *Window, cx *Context[S]) AnyElement
}

// RenderOnce is implemented by stateless component recipes (the
// reference RenderOnce): plain struct props consumed at conversion.
type RenderOnce interface {
	// RenderOnce renders this component's element tree.
	RenderOnce(w *Window, app *App) AnyElement
}

// View is the unifying renderable: properties plus optional backing
// identity (the reference View). When EntityID reports an identity it
// becomes the view's ElementID (a unique element-id space so internal
// ids never collide across siblings) and notifications on the backing
// entity re-render this view's subtree; recipes report no identity and
// behave like stateless components.
type View interface {
	// EntityID returns this view's backing entity identity when it has
	// one. Views keyed on the same entity must not be siblings under the
	// same parent (their internal element state would collide).
	EntityID() (EntityID, bool)
	// RenderOnce renders this view into an element tree, consuming the
	// recipe semantics of the underlying component.
	RenderOnce(w *Window, app *App) AnyElement
}

// DismissEvent is the managed-view dismissal event payload (the authoring
// round's managed-view contract).
type DismissEvent struct{}

// ManagedView is a view that owns focus and can be dismissed (the
// managed-view contract: rendering, a focus handle and a dismissal
// event).
type ManagedView[S any] interface {
	Render[S]
	// FocusHandle returns the view's focus handle.
	FocusHandle() FocusHandle
	// DismissEvent returns the view's dismissal event descriptor.
	DismissEvent() Event[S, DismissEvent]
}

// viewAdapter adapts an entity whose state renders itself into a View
// (the reference impl View for Entity<T: Render>).
type viewAdapter[S any, PS interface {
	*S
	Render[S]
}] struct {
	entity Entity[S]
}

// EntityID implements View: the entity's identity is the view's.
func (v *viewAdapter[S, PS]) EntityID() (EntityID, bool) {
	return v.entity.EntityID(), true
}

// RenderOnce implements View (the reference any_view::render):
// view.update(cx, |view, cx| view.render(window, cx)).
func (v *viewAdapter[S, PS]) RenderOnce(w *Window, app *App) AnyElement {
	var element AnyElement
	v.entity.Update(app, func(state *S, cx *Context[S]) {
		element = PS(state).Render(w, cx)
	})
	if element.obj == nil {
		panic("gpui: view render produced no element")
	}
	return element
}

// managedViewAdapter adapts a managed view entity.
type managedViewAdapter[S any, PS interface {
	*S
	ManagedView[S]
}] struct {
	entity Entity[S]
}

// EntityID implements View.
func (v *managedViewAdapter[S, PS]) EntityID() (EntityID, bool) {
	return v.entity.EntityID(), true
}

// RenderOnce implements View through the managed view's Render.
func (v *managedViewAdapter[S, PS]) RenderOnce(w *Window, app *App) AnyElement {
	var element AnyElement
	v.entity.Update(app, func(state *S, cx *Context[S]) {
		element = PS(state).Render(w, cx)
	})
	if element.obj == nil {
		panic("gpui: managed view render produced no element")
	}
	return element
}

// ViewOf adapts an entity whose state implements Render[S] into a View.
// The adapter retains an independent lease for the entity in the active
// foreground frame-construction scope — the window currently being drawn
// when one is — falling back to the application root scope (the resolved
// ownership rule; ViewOfIn names an owner outside the frame boundary).
func ViewOf[S any, PS interface {
	*S
	Render[S]
}](entity Entity[S]) View {
	return &viewAdapter[S, PS]{entity: retainForFrame(entity)}
}

// ViewOfIn adapts a renderable entity into a View, retaining an
// independent lease in the explicitly named owner scope.
func ViewOfIn[S any, PS interface {
	*S
	Render[S]
}](owner *Scope, entity Entity[S]) View {
	if owner == nil {
		panic("gpui: ViewOfIn requires a non-nil owner scope")
	}
	return &viewAdapter[S, PS]{entity: entity.RetainInto(owner)}
}

// ManagedViewOf adapts a managed-view entity into a View (rendering,
// focus, dismissal).
func ManagedViewOf[S any, PS interface {
	*S
	ManagedView[S]
}](entity Entity[S]) View {
	return &managedViewAdapter[S, PS]{entity: retainForFrame(entity)}
}

// retainForFrame retains an independent lease in the active
// frame-construction scope (the window being drawn, else the app root
// scope).
func retainForFrame[S any](entity Entity[S]) Entity[S] {
	owner := entity.lease.app.RootScope()
	if w := activeFrameWindow(); w != nil {
		owner = w.Scope()
	}
	return entity.RetainInto(owner)
}

// statelessView wraps a RenderOnce recipe as a View without identity
// (the reference blanket impl View for RenderOnce).
type statelessView struct {
	recipe RenderOnce
}

// EntityID implements View: recipes carry no identity.
func (v statelessView) EntityID() (EntityID, bool) { return 0, false }

// RenderOnce implements View: the recipe's own render.
func (v statelessView) RenderOnce(w *Window, app *App) AnyElement {
	return v.recipe.RenderOnce(w, app)
}

// ---------------------------------------------------------------------------
// The view element (view.rs ViewElement)
// ---------------------------------------------------------------------------

// viewElement hooks one View into the element phases (the reference
// ViewElement). RequestLayoutState and PrepaintState are the rendered
// element (the reference Option<AnyElement>: the stateful path renders
// during request_layout and threads the element through the phases).
type viewElement struct {
	// view is the adapted view.
	view View
	// entityID is the view's identity when it has one.
	entityID *EntityID
	// typeName isolates stateless subtrees (the reference
	// ElementId::Name(std::any::type_name::<V>())).
	typeName string
}

// viewElementOf converts one View into its element (the reference
// IntoElement for View).
func viewElementOf(view View) AnyElement {
	if view == nil || isNilReflect(view) {
		panic("gpui: view element requires a non-nil view")
	}
	elem := &viewElement{view: view, typeName: fmt.Sprintf("%T", view)}
	if id, ok := view.EntityID(); ok {
		id := id
		elem.entityID = &id
	}
	return CustomElement[AnyElement, AnyElement](elem)
}

// recipeElementOf converts one RenderOnce recipe into its stateless view
// element (the recipe adapter of the child-conversion precedence).
func recipeElementOf(recipe RenderOnce) AnyElement {
	return viewElementOf(statelessView{recipe: recipe})
}

// ID implements Element: the view's identity (the reference
// ElementId::View(entity_id)).
func (v *viewElement) ID() (ElementID, bool) {
	if v.entityID == nil {
		return ElementID{}, false
	}
	return ViewElementID(*v.entityID), true
}

// SourceLocation implements Element.
func (v *viewElement) SourceLocation() *SourceLocation { return nil }

// viewRequestResult is the view element's request-layout phase result
// (the reference (LayoutId, Option<AnyElement>) pair).
type viewRequestResult struct {
	layoutID LayoutID
	element  AnyElement
}

// RequestLayout implements Element: the stateful path renders the view
// inside its rendered-view scope and requests the rendered element; the
// stateless path isolates the subtree by type name (the reference
// ViewElement::request_layout, uncached path).
func (v *viewElement) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, AnyElement) {
	var result viewRequestResult
	if v.entityID != nil {
		result = withRenderedView(w, *v.entityID, func(w *Window) viewRequestResult {
			element := v.view.RenderOnce(w, app)
			return viewRequestResult{layoutID: element.RequestLayout(w, app), element: element}
		})
	} else {
		result = WithID(w, NameElementID(v.typeName), func(w *Window) viewRequestResult {
			element := v.view.RenderOnce(w, app)
			return viewRequestResult{layoutID: element.RequestLayout(w, app), element: element}
		})
	}
	return result.layoutID, result.element
}

// Prepaint implements Element (the reference ViewElement::prepaint,
// uncached path: set the view id and prepaint the rendered element).
func (v *viewElement) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, element *AnyElement, w *Window, app *App) AnyElement {
	if v.entityID != nil {
		return withRenderedView(w, *v.entityID, func(w *Window) AnyElement {
			element.Prepaint(w, app)
			return *element
		})
	}
	return WithID(w, NameElementID(v.typeName), func(w *Window) AnyElement {
		element.Prepaint(w, app)
		return *element
	})
}

// Paint implements Element (the reference ViewElement::paint, uncached
// path).
func (v *viewElement) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, requestLayout *AnyElement, prepaint *AnyElement, w *Window, app *App) {
	if v.entityID != nil {
		withRenderedViewVoid(w, *v.entityID, func(w *Window) {
			prepaint.Paint(w, app)
		})
		return
	}
	WithIDVoid(w, NameElementID(v.typeName), func(w *Window) {
		prepaint.Paint(w, app)
	})
}

// LayoutNodeStyle implements the root-stretch report: the view's node is
// the rendered element's node; the rendered element reports it (the
// drawable of the returned child).
func (v *viewElement) LayoutNodeStyle() (Style, bool) {
	// The view element delegates its layout node to the rendered element;
	// the stretch report is not available before rendering, so the window
	// draw asks the rendered element's drawable after request.
	return Style{}, false
}

// withRenderedView pushes the entity onto the window's rendered-view
// stack around f (the reference window.with_rendered_view).
func withRenderedView[R any](w *Window, id EntityID, f func(*Window) R) R {
	frame := currentFrame(w)
	frame.renderedViews = append(frame.renderedViews, id)
	defer func() { frame.renderedViews = frame.renderedViews[:len(frame.renderedViews)-1] }()
	return f(w)
}

// withRenderedViewVoid is the void form.
func withRenderedViewVoid(w *Window, id EntityID, f func(*Window)) {
	withRenderedView(w, id, func(w *Window) struct{} { f(w); return struct{}{} })
}

// WithIDVoid is the void form of WithID.
func WithIDVoid(w *Window, id ElementID, f func(*Window)) {
	WithID(w, id, func(w *Window) struct{} { f(w); return struct{}{} })
}

// CurrentView returns the entity id of the currently rendering view
// (window.rs current_view).
func CurrentView(w *Window) (EntityID, bool) {
	frame := currentFrame(w)
	if len(frame.renderedViews) == 0 {
		return 0, false
	}
	return frame.renderedViews[len(frame.renderedViews)-1], true
}

// ---------------------------------------------------------------------------
// Window root views (app.rs open_window)
// ---------------------------------------------------------------------------

// SetRootView installs the window's root view: the view rendered by
// every frame draw of this window (the reference window.root).
func (w *Window) SetRootView(view View) {
	ds := drawState(w)
	if view == nil || isNilReflect(view) {
		panic("gpui: SetRootView requires a non-nil view")
	}
	if ds.root != nil {
		panic("gpui: the window already has a root view")
	}
	ds.root = view
}

// RootView returns the window's root view.
func (w *Window) RootView() (View, bool) {
	ds, ok := windowDrawStates[w]
	if !ok || ds.root == nil || ds.closed {
		return nil, false
	}
	return ds.root, true
}

// OpenWindowView opens a real window through the attached host whose
// root view is built by build inside the window's context, then draws
// the window's first frame before returning (the reference
// App::open_window: the root view is built before the first draw, and
// the window draws at least once so the frame is complete).
func (a *App) OpenWindowView(opts WindowOptions, build func(w *Window, app *App) View) (*Window, error) {
	if build == nil {
		return nil, fmt.Errorf("gpui: OpenWindowView requires a root view builder")
	}
	window, err := a.OpenWindow(opts)
	if err != nil {
		return nil, err
	}
	// Registration and the first draw run on the foreground thread in one
	// unit, exactly like the reference open_window's update.
	h := window.handle.host
	if h == nil {
		return nil, ErrNoHost
	}
	if !h.runForegroundSync(func() {
		a.Update(func(app *App) {
			window.SetRootView(build(window, app))
			if _, err := DrawWindowFrame(window); err != nil {
				panic(fmt.Sprintf("gpui: OpenWindowView initial draw: %v", err))
			}
		})
	}) {
		return nil, ErrHostStopped
	}
	return window, nil
}
