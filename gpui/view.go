package gpui

import (
	"fmt"
	"log"
	"reflect"
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

// installViewSelfHandle installs the window-owned strong self-handle
// lease of a view being rendered by w (Context.StrongSelf): one lease
// per (window, view entity), retained on the first read and shared by
// every later read, registered in the window's scope so it releases
// when the window closes. The reference's listener closures own strong
// Entity clones that drop with the element tree (R:app/context.rs:50-56
// with Rust's Arc ref-counting); the window-owned lease is the port's
// deterministic equivalent — the element tree and its closures live as
// long as the window's placement, and the lease follows that lifetime
// instead of GC timing.
func installViewSelfHandle(w *Window, id EntityID) *entityLease {
	ds := drawState(w)
	if lease := ds.selfLeases[id]; lease != nil && !lease.dead {
		return lease
	}
	owner := w.Scope()
	owner.requireOpen()
	lease := &entityLease{id: id, app: w.app, kind: leaseOwned, owner: owner}
	owner.addLease(lease)
	w.app.entities.addLease(id)
	if ds.selfLeases == nil {
		ds.selfLeases = make(map[EntityID]*entityLease)
	}
	ds.selfLeases[id] = lease
	return lease
}

// entityBackedView is implemented by the entity view adapters: it
// reports the backing entity's identity, application and state type (the
// reference AnyView's entity + entity_type, view.rs:53-71).
type entityBackedView interface {
	backingEntity() (EntityID, *App, reflect.Type)
}

func (v *viewAdapter[S, PS]) backingEntity() (EntityID, *App, reflect.Type) {
	return v.entity.id, v.entity.app, reflect.TypeFor[S]()
}

func (v *managedViewAdapter[S, PS]) backingEntity() (EntityID, *App, reflect.Type) {
	return v.entity.id, v.entity.app, reflect.TypeFor[S]()
}

// DowncastView converts a view to the entity of state T backing it
// (view.rs:53-61 AnyView::downcast). It reports false for recipes
// (stateless views) and views backed by another state type.
//
// The returned handle is a non-releasable facade over the view's lease
// (Read/Update/Downgrade work while the entity is live; Release panics):
// retain it with RetainInto to own it independently — the reference
// returns a strong clone.
func DowncastView[T any](view View) (Entity[T], bool) {
	backed, ok := view.(entityBackedView)
	if !ok {
		return Entity[T]{}, false
	}
	id, app, stateType := backed.backingEntity()
	if stateType != reflect.TypeFor[T]() {
		return Entity[T]{}, false
	}
	return Entity[T]{
		id:    id,
		app:   app,
		lease: &entityLease{kind: leaseBorrow, id: id, app: app, valid: &borrowValidity{}},
	}, true
}

// Root returns the window's root view as the entity of state T
// (window.rs:2272-2279 Window::root). It reports false when the window
// has no root view or the root is not backed by T; RootView
// distinguishes the two (the reference's outer Option).
func (w *Window) Root[T any]() (Entity[T], bool) {
	root, ok := w.RootView()
	if !ok {
		return Entity[T]{}, false
	}
	return DowncastView[T](root)
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
	// cachedStyle is the cached view's layout style (the reference
	// cached_style: Some for AnyView::cached / Entity::cached; see
	// view_cache.go CachedView), nil for an uncached view.
	cachedStyle *StyleRefinement
}

// viewElementOf converts one View into its element (the reference
// IntoElement for View).
func viewElementOf(view View) AnyElement {
	return CustomElement[AnyElement, AnyElement](newViewElement(view))
}

// newViewElement builds the view element (the reference
// ViewElement::new).
func newViewElement(view View) *viewElement {
	if view == nil || isNilReflect(view) {
		panic("gpui: view element requires a non-nil view")
	}
	elem := &viewElement{view: view, typeName: fmt.Sprintf("%T", view)}
	if id, ok := view.EntityID(); ok {
		id := id
		elem.entityID = &id
	}
	return elem
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
// inside its rendered-view scope and requests the rendered element — or,
// for a cached view, requests one node laid out at the cached style
// without rendering (R:view.rs:368-404); the stateless path isolates the
// subtree by type name.
func (v *viewElement) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, AnyElement) {
	var result viewRequestResult
	if v.entityID != nil {
		result = withRenderedView(w, *v.entityID, func(w *Window) viewRequestResult {
			if v.cachedStyle != nil && !w.IsInspectorPicking() {
				style := DefaultStyle()
				style.Refine(v.cachedStyle)
				layoutID, err := requestLayoutOf(w, style)
				if err != nil {
					panic(fmt.Sprintf("gpui: cached view request_layout: %v", err))
				}
				return viewRequestResult{layoutID: layoutID}
			}
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
// R:view.rs:406-485): the stateful path marks the active dispatch node
// with the view (set_view_id) and prepaints the rendered element; a
// cached view with no rendered element reuses its previous frame's
// prepaint range when its cache key holds and it is neither dirty nor
// refreshing, and otherwise renders, lays itself out as a root at its
// bounds and prepaints there, recording the new range.
func (v *viewElement) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, element *AnyElement, w *Window, app *App) AnyElement {
	if v.entityID != nil {
		entityID := *v.entityID
		currentDispatchTree(w).SetViewID(entityID)
		return withRenderedView(w, entityID, func(w *Window) AnyElement {
			if element.obj != nil {
				element.Prepaint(w, app)
				return *element
			}
			return WithElementState(w, global, func(state *viewElementState, w *Window) (AnyElement, viewElementState) {
				ds := drawState(w)
				contentMask := ContentMask(w)
				textStyle := WindowTextStyle(w)
				if state != nil && state.cacheKey.matches(bounds, contentMask, textStyle) && !ds.isViewDirty(entityID) && !ds.refreshing {
					ds.viewCacheHits++
					reused := *state
					prepaintStart := w.prepaintIndex()
					w.reusePrepaint(state.prepaintStart, state.prepaintEnd)
					extendAccessedEntities(w, state.accessedEntities)
					reused.prepaintStart, reused.prepaintEnd = prepaintStart, w.prepaintIndex()
					return AnyElement{}, reused
				}

				ds.viewCacheMisses++
				refreshing := ds.refreshing
				ds.refreshing = true
				prepaintStart := w.prepaintIndex()
				var rendered AnyElement
				accessed := detectAccessedEntities(w, func() {
					rendered = v.view.RenderOnce(w, app)
					rendered.LayoutAsRoot(AvailableSize{
						Width:  DefiniteAvailableSpace(bounds.Size.Width),
						Height: DefiniteAvailableSpace(bounds.Size.Height),
					}, w, app)
					rendered.PrepaintAt(bounds.Origin, w, app)
				})
				prepaintEnd := w.prepaintIndex()
				ds.refreshing = refreshing
				return rendered, viewElementState{
					prepaintStart:    prepaintStart,
					prepaintEnd:      prepaintEnd,
					cacheKey:         viewElementCacheKey{bounds: bounds, contentMask: contentMask, textStyle: textStyle},
					accessedEntities: accessed,
				}
			})
		})
	}
	return WithID(w, NameElementID(v.typeName), func(w *Window) AnyElement {
		element.Prepaint(w, app)
		return *element
	})
}

// Paint implements Element (the reference ViewElement::paint,
// R:view.rs:487-537): a cached view paints its freshly rendered element
// (refreshing, so nested cached views paint fresh too) or replays its
// previous frame's paint range, recording the new range.
func (v *viewElement) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, requestLayout *AnyElement, prepaint *AnyElement, w *Window, app *App) {
	if v.entityID != nil {
		withRenderedViewVoid(w, *v.entityID, func(w *Window) {
			if v.cachedStyle == nil || w.IsInspectorPicking() {
				prepaint.Paint(w, app)
				return
			}
			WithElementState(w, global, func(state *viewElementState, w *Window) (struct{}, viewElementState) {
				if state == nil {
					panic("gpui: cached view paint without its prepaint state")
				}
				next := *state
				paintStart := w.paintIndex()
				if prepaint.obj != nil {
					ds := drawState(w)
					refreshing := ds.refreshing
					ds.refreshing = true
					prepaint.Paint(w, app)
					ds.refreshing = refreshing
				} else {
					w.reusePaint(state.paintStart, state.paintEnd)
				}
				next.paintStart, next.paintEnd = paintStart, w.paintIndex()
				return struct{}{}, next
			})
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
// stack around f (the reference window.with_rendered_view). The pushed
// view is tracked by the window's frame loop (frameloop.go
// recordRenderedView; R:window.rs:3302-3314).
func withRenderedView[R any](w *Window, id EntityID, f func(*Window) R) R {
	frame := currentFrame(w)
	frame.renderedViews = append(frame.renderedViews, id)
	recordRenderedView(w)
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
//
// The build runs with the window as the active frame-construction
// boundary (the reference pushes the window onto window_update_stack
// around build_root_view, app.rs:1346-1350): ViewOf placements made
// while opening the window — the root view above all — retain their
// independent leases in the window's scope, mirroring the reference
// window owning its root AnyView (app.rs:1360 window.root.replace), so
// closing the window releases the root placement instead of leaking it
// to the application root scope until shutdown.
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
			previousActive := activeFrame
			activeFrame = window
			defer func() { activeFrame = previousActive }()
			window.SetRootView(build(window, app))
			if _, err := DrawWindowFrame(window); err != nil {
				// The reference open_window's draw cannot fail; the port's
				// can (a layout or native failure). A panic here unwound
				// through the host's window trampoline, which recorded it
				// and failed Host.Stop much later (Base ticket 40: a window
				// opened while the shaping handles were exhausted). Treat
				// it like every later frame (frameloop.go requestFrame's
				// log_err): log it and leave the window dirty, so the next
				// frame request draws it again.
				log.Printf("gpui: window %d initial draw: %v", uint64(window.id), err)
				window.Refresh()
			}
		})
	}) {
		return nil, ErrHostStopped
	}
	return window, nil
}
