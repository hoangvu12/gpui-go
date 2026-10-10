package gpui

// This file is the port's element runtime (ticket11): the typed Element
// phases with global/inspector identities, the AnyElement type erasure
// with the pinned Drawable phase machine, the child-conversion seam
// (TryIntoElement / OwnRecipe / Text / Empty / CustomElement), the
// accessibility companions, and the retained element-state model.
//
// The pinned reference is GPUI-CE 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a:
//
//   - crates/gpui/src/element.rs — the Element trait (request_layout /
//     prepaint / paint with Option<&GlobalElementId> and
//     Option<&InspectorElementId>, the id/source-location hooks, the a11y
//     hooks), IntoElement, ParentElement / ParentElementTyped, the
//     GlobalElementId path type and the Drawable ElementDrawPhase state
//     machine (Start → RequestLayout → LayoutComputed → Prepaint →
//     Painted) that drives AnyElement.request_layout / prepaint / paint /
//     layout_as_root / prepaint_as_root;
//   - crates/gpui/src/window.rs — with_element_state / with_optional_element_state
//     (state keyed by (GlobalElementId, TypeId), carried across frames
//     through the rendered frame), the element_id_stack, with_id, and the
//     draw_roots root stretch (stretch_auto_size_to_fill, taffy.rs);
//   - crates/gpui/src/style.rs — TextStyle::default() and
//     TextStyleRefinement (the text style stack composed by
//     Window::text_style / with_text_style).
//
// Go adaptations are documented at each site. The deliberate bound of
// this slice: interactivity (hitboxes, mouse/keyboard listeners, focus
// routing) is ticket13's work; the phase seams, identity paths and the
// retained state model are built here and used by the div (div.go), the
// text element and the view runtime (view.go).

import (
	"fmt"
	"reflect"
)

// ---------------------------------------------------------------------------
// Element identity (element.rs ElementId / GlobalElementId /
// InspectorElementId)
// ---------------------------------------------------------------------------

// ElementIDKind selects the form of an ElementID (the reference
// ElementId enum: Name(SharedString), View(EntityId), Integer(u64), ...).
type ElementIDKind uint8

const (
	// ElementIDName is a string identity (Div().ID("counter")).
	ElementIDName ElementIDKind = iota
	// ElementIDView is a view identity (the backing entity's id).
	ElementIDView
	// ElementIDInteger is a numeric identity.
	ElementIDInteger
)

// ElementID is the identity of one element within its parent's child
// list (the reference ElementId). Ids are scoped by the parent path:
// two views keyed on the same entity must not be siblings under the
// same parent, because their internal element state would collide (the
// accepted CE view identity contract, view.rs).
type ElementID struct {
	// Kind selects the identity form.
	Kind ElementIDKind
	// Name carries the string identity (Kind == ElementIDName).
	Name string
	// Entity carries the view identity (Kind == ElementIDView).
	Entity EntityID
	// Integer carries the numeric identity (Kind == ElementIDInteger).
	Integer uint64
}

// NameElementID builds a string identity.
func NameElementID(name string) ElementID {
	return ElementID{Kind: ElementIDName, Name: name}
}

// ViewElementID builds a view identity.
func ViewElementID(entity EntityID) ElementID {
	return ElementID{Kind: ElementIDView, Entity: entity}
}

// IntegerElementID builds a numeric identity.
func IntegerElementID(value uint64) ElementID {
	return ElementID{Kind: ElementIDInteger, Integer: value}
}

// String renders the identity like the reference Display impl.
func (id ElementID) String() string {
	switch id.Kind {
	case ElementIDName:
		return id.Name
	case ElementIDView:
		return fmt.Sprintf("view-%d", uint64(id.Entity))
	default:
		return fmt.Sprintf("%d", id.Integer)
	}
}

// GlobalElementID is the identity PATH of an element: the ids of every
// ancestor element with an id, plus this element's own (the reference
// GlobalElementId, Arc<[ElementId]>, built from the window's
// element_id_stack). Retained element state is keyed by this path.
type GlobalElementID struct {
	// Path is the element id stack snapshot (shared, never mutated
	// after construction).
	Path []ElementID
}

// key renders the path as the map-key string (the reference Display
// impl joins the ids with ".").
func (g *GlobalElementID) key() string {
	if g == nil {
		return ""
	}
	out := ""
	for i, id := range g.Path {
		if i > 0 {
			out += "."
		}
		out += id.String()
	}
	return out
}

// String renders the path like the reference Display impl.
func (g GlobalElementID) String() string {
	clone := g
	return clone.key()
}

// Len returns the path length.
func (g *GlobalElementID) Len() int {
	if g == nil {
		return 0
	}
	return len(g.Path)
}

// Last returns the path's final identity (this element's own id).
func (g *GlobalElementID) Last() (ElementID, bool) {
	if g.Len() == 0 {
		return ElementID{}, false
	}
	return g.Path[len(g.Path)-1], true
}

// InspectorElementPath is a GlobalElementID qualified by the source
// location of element construction (inspector.rs InspectorElementPath,
// lines 36-44: the path to the nearest ancestor element that has an
// ElementId, plus where this element was constructed). The path is
// shared between the ids built from it and compared BY VALUE (the
// pin's Rc<InspectorElementPath> PartialEq/Hash compare the inner
// path, not the pointer).
type InspectorElementPath struct {
	// global is the element id stack snapshot at construction (the
	// pin's global_id: Arc<[ElementId]>). Immutable after construction.
	global *GlobalElementID
	// globalKey is the path's map-key string (precomputed: the value
	// equality form of global).
	globalKey string
	// location is where the element was constructed (the pin's
	// &'static Location<'static>).
	location SourceLocation
}

// key renders the path's value key (global path + source location);
// same-path elements share it within a frame, so the instance ids
// disambiguate them.
func (p *InspectorElementPath) key() string {
	if p == nil {
		return ""
	}
	return p.globalKey + "\x00" + fmt.Sprintf("%s:%d", p.location.File, p.location.Line)
}

// Global returns the path's global element id (the ancestor-qualified
// identity; includes the element's own id when it has one).
func (p *InspectorElementPath) Global() *GlobalElementID {
	if p == nil {
		return nil
	}
	return p.global
}

// Location returns the construction source location.
func (p *InspectorElementPath) Location() SourceLocation {
	if p == nil {
		return SourceLocation{}
	}
	return p.location
}

// InspectorElementID is the inspector/debug-build identity of an
// element (inspector.rs InspectorElementId, lines 11-26: the gated
// long id — a shared path plus an instance id that disambiguates
// elements with the same path). The zero value is a valid "no
// inspector identity".
//
// CAPABILITY MAPPING: the pin compiles the path/instance fields only
// under #[cfg(any(feature = "inspector", debug_assertions))], leaving
// an empty struct in release builds (the Option<&InspectorElementId>
// parameters still thread through every phase). The Go port keeps the
// struct and gates CONSTRUCTION: when CapInspectorIDs is off, the
// drawable builds no inspector id (nil), which is the same release
// observable. See inspector.go's capability surface.
type InspectorElementID struct {
	// path is the stable part of the id (the pin's Rc<InspectorElementPath>).
	path *InspectorElementPath
	// instanceID disambiguates elements that have the same path (the
	// pin's instance_id: usize, assigned per frame by
	// Window::build_inspector_element_id).
	instanceID uint64
}

// String renders the identity for diagnostics (the pin's Debug impl
// plus the instance).
func (i InspectorElementID) String() string {
	if i.path == nil {
		return "<no inspector id>"
	}
	return fmt.Sprintf("%s:%d#%d", i.path.location.File, i.path.location.Line, i.instanceID)
}

// GlobalPath returns the id's global element id path key (the
// "."-joined form).
func (i InspectorElementID) GlobalPath() string {
	if i.path == nil {
		return ""
	}
	return i.path.globalKey
}

// SourceLocation returns the construction location.
func (i InspectorElementID) SourceLocation() SourceLocation {
	if i.path == nil {
		return SourceLocation{}
	}
	return i.path.location
}

// InstanceID returns the per-frame instance id.
func (i InspectorElementID) InstanceID() uint64 { return i.instanceID }

// Equal reports value identity (the pin's derived PartialEq: path
// value + instance id).
func (i InspectorElementID) Equal(other InspectorElementID) bool {
	if i.path == nil || other.path == nil {
		return i.path == nil && other.path == nil && i.instanceID == other.instanceID
	}
	return i.path.key() == other.path.key() && i.instanceID == other.instanceID
}

// SourceLocation is where an element was constructed (the reference
// std::panic::Location hook).
type SourceLocation struct {
	// File is the source file path.
	File string
	// Line is the 1-based line.
	Line int
}

// ---------------------------------------------------------------------------
// The typed element phases (element.rs Element)
// ---------------------------------------------------------------------------

// Element is the low-level element contract: the typed layout/prepaint
// phase states, the optional identity and source location, and the
// optional global/inspector identities threaded through every phase.
// It mirrors the pinned Element trait with the Go adaptation the
// authoring round selected: typed L and P stay on the author's element
// and are erased only inside CustomElement's adapter.
type Element[L, P any] interface {
	// ID returns this element's identity when it has one. The id makes
	// a GlobalElementId flow through the phases and keys retained
	// element state.
	ID() (ElementID, bool)
	// SourceLocation returns where this element was constructed, when
	// known.
	SourceLocation() *SourceLocation
	// RequestLayout requests a layout node from the window's layout
	// engine and initializes the element's request-layout state.
	RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, L)
	// Prepaint commits the element's bounds and builds its prepaint
	// state after layout completes.
	Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *L, w *Window, app *App) P
	// Paint draws the element into the window's scene.
	Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *L, prepaint *P, w *Window, app *App)
}

// Role is the accessibility role of an element (the accesskit Role
// discriminant).
type Role uint16

// Node is the accessibility node record an element writes its
// properties into (the accesskit Node; a structural placeholder until
// the accessibility tickets).
type Node struct {
	// Role is the node's role.
	Role Role
	// Value is the node's value (text elements set their text).
	Value string
	// Hidden marks the subtree hidden from assistive technology.
	Hidden bool
}

// A11ySubtreeBuilder builds synthetic accessibility children (the
// reference A11ySubtreeBuilder; structural placeholder).
type A11ySubtreeBuilder struct {
	// nodes collects the synthetic children built so far.
	nodes []Node
}

// PushNode appends one synthetic child node.
func (b *A11ySubtreeBuilder) PushNode(node Node) {
	if b == nil {
		return
	}
	b.nodes = append(b.nodes, node)
}

// NodeCount reports how many synthetic children were built.
func (b *A11ySubtreeBuilder) NodeCount() int {
	if b == nil {
		return 0
	}
	return len(b.nodes)
}

// A11yRole is the accessibility role capability of an element (the
// reference Element::a11y_role): inclusion in the accessibility tree
// requires a role AND an id.
type A11yRole interface {
	// A11yRole returns the element's accessibility role when it has one.
	A11yRole() (Role, bool)
}

// A11yHidden is the hidden-subtree capability (Element::is_a11y_hidden).
type A11yHidden interface {
	// IsA11yHidden reports whether assistive technology should ignore
	// this element and its descendants.
	IsA11yHidden() bool
}

// A11yProperties is the property-writing capability
// (Element::write_a11y_info).
type A11yProperties interface {
	// WriteA11yInfo writes this element's accessibility properties.
	WriteA11yInfo(node *Node)
}

// A11ySyntheticChildren is the synthetic-children capability
// (Element::a11y_synthetic_children) receiving the mutable prepaint
// state.
type A11ySyntheticChildren[P any] interface {
	// A11ySyntheticChildren adds accessibility nodes that do not
	// correspond to any element.
	A11ySyntheticChildren(prepaint *P, builder *A11ySubtreeBuilder)
}

// ParentElementTyped is the restricted-parent child capability: a parent
// that only accepts children of one type C (the reference
// ParentElementTyped).
type ParentElementTyped[C, Self any] interface {
	// Child adds a single typed child element.
	Child(child C) Self
	// Children adds multiple typed children.
	Children(children ...C) Self
}

// ---------------------------------------------------------------------------
// The AnyElement type erasure (element.rs AnyElement / Drawable)
// ---------------------------------------------------------------------------

// elementObject is the type-erased element surface AnyElement drives
// (the reference ElementObject): the phase machine plus the identity
// hooks. The drawable owns the phase ordering; phase misuse panics
// exactly like the reference Drawable's `must call ... only once`
// guards.
type elementObject interface {
	// elementID reports the element's identity.
	elementID() (ElementID, bool)
	// requestLayout runs the request_layout phase and returns the
	// layout id.
	requestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) LayoutID
	// prepaint runs the prepaint phase.
	prepaint(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App)
	// paint runs the paint phase.
	paint(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App)
	// layoutAsRoot requests, computes and measures the element as the
	// root of a layout under the available space.
	layoutAsRoot(available AvailableSize, w *Window, app *App) Size
	// rootStretchStyle reports the style the element's layout node was
	// requested with, for the window root stretch. The port engine
	// cannot query a node's style after the request (the reference
	// re-reads it from taffy), so elements report it; elements whose
	// node style is unknown report false and the stretch is skipped.
	rootStretchStyle() (Style, bool)
}

// drawPhase is the Drawable phase machine state (the reference
// ElementDrawPhase): Start → RequestLayout → (LayoutComputed) →
// Prepaint → Painted.
type drawPhase uint8

const (
	phaseStart drawPhase = iota
	phaseRequested
	phaseLayoutComputed
	phasePrepainted
	phasePainted
)

// drawable adapts one typed Element[L, P] into elementObject, holding
// the typed phase states and the identity bookkeeping (the reference
// Drawable<E>).
type drawable[L, P any, E Element[L, P]] struct {
	element E
	phase   drawPhase
	// layoutID is the requested layout node.
	layoutID LayoutID
	// global and inspector are this element's phase identities,
	// populated when the element has an id.
	global    *GlobalElementID
	inspector *InspectorElementID
	// inlineFragments is the fragment geometry of this element's layout
	// node, captured when prepaint runs and replayed around paint (the
	// reference Drawable's ElementDrawPhase::Prepaint::inline_fragments).
	inlineFragments []Bounds
	// available is the root available space of the last root compute
	// (the LayoutComputed recompute guard).
	available AvailableSize
	// requestState and prepaintState are the typed phase states.
	requestState  L
	prepaintState P
}

// elementID implements elementObject.
func (d *drawable[L, P, E]) elementID() (ElementID, bool) { return d.element.ID() }

// requestLayout implements elementObject: builds the global id from the
// window's element id stack when the element has an id (pushing it for
// the duration of the phase so children see it), calls the typed phase,
// and stores the phase state (the reference Drawable::request_layout).
func (d *drawable[L, P, E]) requestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) LayoutID {
	if d.phase != phaseStart {
		panic("gpui: must call request_layout only once")
	}
	ownID, hasID := d.element.ID()
	if hasID {
		pushElementID(w, ownID)
		defer popElementID(w)
		d.global = &GlobalElementID{Path: currentElementIDs(w)}
	}
	// The inspector id: built from the element's source location and
	// the CURRENT id stack (element.rs Drawable::request_layout,
	// 345-361: the path is the stack snapshot — the element's own id
	// when it has one, the nearest ancestor's path otherwise — and
	// window.build_inspector_element_id assigns the per-frame instance
	// id). Under the pin's #[cfg(any(feature = "inspector",
	// debug_assertions))] this is where the long id exists; the cfg'd-
	// out branch leaves inspector_id = None, which the port maps to
	// building no id when CapInspectorIDs is off (inspector.go).
	if inspectorIDsActive() {
		if loc := d.element.SourceLocation(); loc != nil {
			pathGlobal := &GlobalElementID{Path: currentElementIDs(w)}
			d.inspector = buildInspectorElementID(w, &InspectorElementPath{
				global:    pathGlobal,
				globalKey: pathGlobal.key(),
				location:  *loc,
			})
		}
	}
	d.phase = phaseRequested
	layoutID, state := d.element.RequestLayout(orGlobalID(global, d.global), orInspector(inspector, d.inspector), w, app)
	d.layoutID = layoutID
	d.requestState = state
	return layoutID
}

// orGlobalID returns the first non-nil global id.
func orGlobalID(a, b *GlobalElementID) *GlobalElementID {
	if a != nil {
		return a
	}
	return b
}

// orInspector returns the first non-nil inspector id.
func orInspector(a, b *InspectorElementID) *InspectorElementID {
	if a != nil {
		return a
	}
	return b
}

// rootStretchStyle implements elementObject.
func (d *drawable[L, P, E]) rootStretchStyle() (Style, bool) {
	if s, ok := any(d.element).(interface{ LayoutNodeStyle() (Style, bool) }); ok {
		return s.LayoutNodeStyle()
	}
	return Style{}, false
}

// prepaint implements elementObject: resolves the element's bounds from
// its layout node and runs the typed prepaint phase (the reference
// Drawable::prepaint, which reads window.layout_bounds(layout_id)).
func (d *drawable[L, P, E]) prepaint(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) {
	switch d.phase {
	case phaseRequested, phaseLayoutComputed:
	default:
		panic("gpui: must call request_layout before prepaint")
	}
	ownID, hasID := d.element.ID()
	if hasID {
		pushElementID(w, ownID)
		defer popElementID(w)
	}
	bounds, err := layoutBoundsOf(w, d.layoutID)
	if err != nil {
		panic(fmt.Sprintf("gpui: prepaint could not resolve layout bounds: %v", err))
	}
	d.phase = phasePrepainted
	// Scope this element's placed fragment geometry around its
	// prepaint (the reference Drawable::prepaint swaps
	// window.current_inline_fragments with window.inline_fragments)
	// so elements whose content lives inside a parent paragraph can
	// skip their standalone phases.
	d.inlineFragments = inlineFragmentsOf(w, d.layoutID)
	frame := currentFrame(w)
	previousFragments := frame.currentInlineFragments
	frame.currentInlineFragments = d.inlineFragments
	defer func() { frame.currentInlineFragments = previousFragments }()
	// The inspector tree build (ticket27; the pin has no tree — the
	// inspector UI is app-rendered over with_inspector_state. The port's
	// UI surface records every element with an inspector id while the
	// window's inspector is on, pushing the node around this element's
	// prepaint so children nest, with the committed bounds — the same
	// bounds div.rs writes into DivInspectorState at prepaint,
	// div.rs:2785-2794). Inert when the inspector is off.
	inspectorNode := beginInspectorNode(w, d.inspector, bounds)
	defer func() {
		if inspectorNode != nil {
			endInspectorNode(w)
		}
	}()
	d.prepaintState = d.element.Prepaint(orGlobalID(global, d.global), orInspector(inspector, d.inspector), bounds, &d.requestState, w, app)
}

// paint implements elementObject: runs the typed paint phase with the
// committed bounds and phase states (the reference Drawable::paint).
func (d *drawable[L, P, E]) paint(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) {
	if d.phase != phasePrepainted {
		panic("gpui: must call prepaint before paint")
	}
	ownID, hasID := d.element.ID()
	if hasID {
		pushElementID(w, ownID)
		defer popElementID(w)
	}
	bounds, err := layoutBoundsOf(w, d.layoutID)
	if err != nil {
		panic(fmt.Sprintf("gpui: paint could not resolve layout bounds: %v", err))
	}
	d.phase = phasePainted
	// Replay the fragments captured at prepaint around paint (the
	// reference Drawable::paint swaps current_inline_fragments with the
	// prepaint-captured list).
	frame := currentFrame(w)
	previousFragments := frame.currentInlineFragments
	frame.currentInlineFragments = d.inlineFragments
	defer func() { frame.currentInlineFragments = previousFragments }()
	d.element.Paint(orGlobalID(global, d.global), orInspector(inspector, d.inspector), bounds, &d.requestState, &d.prepaintState, w, app)
}

// layoutAsRoot implements elementObject (the reference
// Drawable::compute_layout_as_root / layout_as_root): request (if not
// yet), compute under the available space (recomputing when the
// available space changed), and return the root's measured size.
func (d *drawable[L, P, E]) layoutAsRoot(available AvailableSize, w *Window, app *App) Size {
	if d.phase == phaseStart {
		d.requestLayout(nil, nil, w, app)
	}
	if d.phase != phaseRequested && d.phase != phaseLayoutComputed {
		panic("gpui: cannot measure after painting")
	}
	recompute := d.phase == phaseRequested || available != d.available
	d.available = available
	d.phase = phaseLayoutComputed
	if recompute {
		if err := computeLayoutOf(w, d.layoutID, available); err != nil {
			panic(fmt.Sprintf("gpui: root layout compute failed: %v", err))
		}
	}
	bounds, err := layoutBoundsOf(w, d.layoutID)
	if err != nil {
		panic(fmt.Sprintf("gpui: root layout bounds failed: %v", err))
	}
	return bounds.Size
}

// AnyElement is a dynamically typed element: the uniform result of
// child conversion and of Render (the reference AnyElement). It hides
// the concrete phase states; phase misuse panics with the Drawable
// guards.
type AnyElement struct {
	obj elementObject
}

// newAnyElement wraps one elementObject into an AnyElement.
func newAnyElement(obj elementObject) AnyElement { return AnyElement{obj: obj} }

// CustomElement adapts a typed custom element into an AnyElement
// (the fixture's typed phase adapter: L and P stay on the author's
// element and are erased only here).
func CustomElement[L, P any](impl Element[L, P]) AnyElement {
	return newAnyElement(&drawable[L, P, Element[L, P]]{element: impl})
}

// ID reports the element's identity.
func (e AnyElement) ID() (ElementID, bool) {
	if e.obj == nil {
		return ElementID{}, false
	}
	return e.obj.elementID()
}

// RequestLayout runs the request_layout phase of the stored element and
// returns its layout id (AnyElement::request_layout).
func (e AnyElement) RequestLayout(w *Window, app *App) LayoutID {
	if e.obj == nil {
		panic("gpui: RequestLayout on a zero AnyElement")
	}
	return e.obj.requestLayout(nil, nil, w, app)
}

// Prepaint prepares the element for painting (AnyElement::prepaint).
func (e AnyElement) Prepaint(w *Window, app *App) {
	if e.obj == nil {
		panic("gpui: Prepaint on a zero AnyElement")
	}
	e.obj.prepaint(nil, nil, w, app)
}

// Paint paints the element (AnyElement::paint).
func (e AnyElement) Paint(w *Window, app *App) {
	if e.obj == nil {
		panic("gpui: Paint on a zero AnyElement")
	}
	e.obj.paint(nil, nil, w, app)
}

// LayoutAsRoot lays the element out under the available space and
// returns its size (AnyElement::layout_as_root).
func (e AnyElement) LayoutAsRoot(available AvailableSize, w *Window, app *App) Size {
	if e.obj == nil {
		panic("gpui: LayoutAsRoot on a zero AnyElement")
	}
	return e.obj.layoutAsRoot(available, w, app)
}

// PrepainAt prepaints the element at the given absolute origin
// (AnyElement::prepaint_at: with_absolute_element_offset).
func (e AnyElement) PrepainAt(origin Point, w *Window, app *App) {
	WithAbsoluteElementOffsetVoid(w, origin, func(w *Window) {
		e.Prepaint(w, app)
	})
}

// PrepaintAsRoot lays the element out under the available space and
// prepaints it at the given absolute origin
// (AnyElement::prepaint_as_root).
func (e AnyElement) PrepaintAsRoot(origin Point, available AvailableSize, w *Window, app *App) Size {
	size := e.LayoutAsRoot(available, w, app)
	WithAbsoluteElementOffsetVoid(w, origin, func(w *Window) {
		e.Prepaint(w, app)
	})
	return size
}

// IntoElement is implemented by values that convert themselves into an
// element (the reference IntoElement).
type IntoElement interface {
	// IntoElement converts self into an AnyElement.
	IntoElement() AnyElement
}

// ErrChildInvalid reports an invalid child value at a child position
// (the nil diagnostic of the checked conversion).
var ErrChildInvalid = fmt.Errorf("gpui: invalid child value")

// TryIntoElement converts one child-position value into an AnyElement
// with the selected precedence: reject nil (including typed nils);
// accept AnyElement; honor explicit IntoElement; convert strings; adapt
// identity-bearing View; then stateless RenderOnce. Raw entities and
// unregistered values are rejected (use ViewOf / CustomElement at those
// typed seams).
func TryIntoElement(value any) (AnyElement, error) {
	if value == nil {
		return AnyElement{}, fmt.Errorf("gpui: nil child value at a child position (accidental nil is a programmer error; the parent and child context should identify it)")
	}
	switch v := value.(type) {
	case AnyElement:
		if v.obj == nil {
			return AnyElement{}, fmt.Errorf("gpui: zero AnyElement child value")
		}
		if err := checkRecipeAttachment(v); err != nil {
			return AnyElement{}, err
		}
		return v, nil
	case *AnyElement:
		if v == nil || v.obj == nil {
			return AnyElement{}, fmt.Errorf("gpui: zero AnyElement child value")
		}
		if err := checkRecipeAttachment(*v); err != nil {
			return AnyElement{}, err
		}
		return *v, nil
	case IntoElement:
		if isNilReflect(value) {
			return AnyElement{}, fmt.Errorf("gpui: nil child value at a child position")
		}
		elem := v.IntoElement()
		if elem.obj == nil {
			return AnyElement{}, fmt.Errorf("gpui: IntoElement produced a zero element")
		}
		return elem, nil
	case string:
		return Text(v), nil
	case View:
		if isNilReflect(value) {
			return AnyElement{}, fmt.Errorf("gpui: nil View child value")
		}
		return viewElementOf(v), nil
	case RenderOnce:
		if isNilReflect(value) {
			return AnyElement{}, fmt.Errorf("gpui: nil RenderOnce child value")
		}
		return recipeElementOf(v), nil
	default:
		if isNilReflect(value) {
			return AnyElement{}, fmt.Errorf("gpui: nil child value of type %T at a child position", value)
		}
		return AnyElement{}, fmt.Errorf("gpui: unsupported child value of type %T (convert it with Text, IntoElement, ViewOf or CustomElement first)", value)
	}
}

// checkRecipeAttachment rejects an OwnRecipe handle that was already
// attached (the single-use token: "reusing an attached handle fails
// immediately, before expansion").
func checkRecipeAttachment(elem AnyElement) error {
	if owned, ok := elem.obj.(*attachedRecipe); ok && owned.adapter.attached {
		return fmt.Errorf("gpui: OwnRecipe handle already attached; recipes are single-use, create a fresh recipe instead")
	}
	return nil
}

// reserveAndCommitRecipes validates and attaches the owned-recipe
// handles of a converted batch: duplicates of the same handle in one
// batch are rejected before the commit, and the commit marks every
// candidate attached (the atomic reservation/commit of the selected
// contract).
func reserveAndCommitRecipes(converted []AnyElement) {
	seen := make(map[*ownRecipeAdapter]bool)
	for _, elem := range converted {
		owned, ok := elem.obj.(*attachedRecipe)
		if !ok {
			continue
		}
		if seen[owned.adapter] {
			panic("gpui: the same OwnRecipe handle was attached twice in one child batch; recipes are single-use")
		}
		seen[owned.adapter] = true
	}
	for _, elem := range converted {
		if owned, ok := elem.obj.(*attachedRecipe); ok {
			owned.adapter.attached = true
		}
	}
}

// isNilReflect reports whether value is a nil pointer, map, slice,
// function, channel or interface.
func isNilReflect(value any) bool {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return rv.IsNil()
	default:
		return false
	}
}

// intoChild converts one child-position value, panicking with the
// parent/child context on invalid input (the fluent Child path; the
// checked path is TryChildren).
func intoChild(parent string, index int, value any) AnyElement {
	elem, err := TryIntoElement(value)
	if err != nil {
		panic(fmt.Sprintf("gpui: %s child %d: %v", parent, index, err))
	}
	return elem
}

// Empty returns the empty element, which renders nothing (the reference
// Empty: a layout node with display none).
func Empty() AnyElement { return CustomElement[struct{}, struct{}](&emptyElement{}) }

// Text returns a text element displaying the given string (the
// reference Text/SharedString element; the string-child convenience
// conversion).
func Text(text string) AnyElement {
	return CustomElement[stringLayoutState, struct{}](&textElement{text: text})
}

// OwnRecipe wraps a third-party recipe with an explicit single-use
// attachment handle: the first conversion consumes the recipe and a
// second attempt fails immediately (the authoring round's misuse
// diagnostic; the framework has no global pointer-provenance registry).
// The wrapper attaches when the handle is first taken as a child.
func OwnRecipe(recipe RenderOnce) AnyElement {
	if recipe == nil {
		panic("gpui: OwnRecipe requires a non-nil recipe")
	}
	return newAnyElement(&attachedRecipe{adapter: &ownRecipeAdapter{recipe: recipe}})
}

// attachedRecipe is the owned-recipe handle: it carries the single-use
// attachment token (unattached at creation, attached at the first child
// commit) and the one expansion of the recipe (the recipe's element
// tree, built at request_layout and threaded through the phases).
type attachedRecipe struct {
	// adapter holds the recipe and the attachment token (shared with
	// the handle value: the AnyElement wraps this adapter).
	adapter *ownRecipeAdapter
	// expanded is the recipe's element, built at request_layout.
	expanded AnyElement
}

// elementID implements elementObject: recipes carry no identity.
func (a *attachedRecipe) elementID() (ElementID, bool) { return ElementID{}, false }

// requestLayout implements elementObject: expand the recipe once and
// request its layout.
func (a *attachedRecipe) requestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) LayoutID {
	a.adapter.attached = true
	a.expanded = recipeElementOf(a.adapter.recipe)
	return a.expanded.RequestLayout(w, app)
}

// prepaint implements elementObject.
func (a *attachedRecipe) prepaint(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) {
	a.requireExpanded().Prepaint(w, app)
}

// paint implements elementObject.
func (a *attachedRecipe) paint(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) {
	a.requireExpanded().Paint(w, app)
}

// layoutAsRoot implements elementObject.
func (a *attachedRecipe) layoutAsRoot(available AvailableSize, w *Window, app *App) Size {
	return a.requireExpanded().LayoutAsRoot(available, w, app)
}

// rootStretchStyle implements elementObject.
func (a *attachedRecipe) rootStretchStyle() (Style, bool) { return Style{}, false }

// requireExpanded returns the recipe's expansion, failing loudly when
// the phases run out of order.
func (a *attachedRecipe) requireExpanded() AnyElement {
	if a.expanded.obj == nil {
		panic("gpui: the owned recipe was not expanded (request_layout must run first)")
	}
	return a.expanded
}

// ownRecipeAdapter is the owned-recipe token: the recipe plus its
// attachment state (attached by the first child commit).
type ownRecipeAdapter struct {
	recipe   RenderOnce
	attached bool
}

// emptyElement is the Empty element (display none, no paint).
type emptyElement struct{}

// ID implements Element.
func (e *emptyElement) ID() (ElementID, bool) { return ElementID{}, false }

// SourceLocation implements Element.
func (e *emptyElement) SourceLocation() *SourceLocation { return nil }

// RequestLayout implements Element: a display-none layout node (the
// reference Empty::request_layout).
func (e *emptyElement) RequestLayout(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) (LayoutID, struct{}) {
	style := DefaultStyle()
	style.Display = DisplayNone
	id, err := requestLayoutOf(w, style)
	if err != nil {
		panic(fmt.Sprintf("gpui: Empty request_layout: %v", err))
	}
	return id, struct{}{}
}

// Prepaint implements Element.
func (e *emptyElement) Prepaint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, w *Window, app *App) struct{} {
	return struct{}{}
}

// Paint implements Element.
func (e *emptyElement) Paint(global *GlobalElementID, inspector *InspectorElementID, bounds Bounds, layout *struct{}, prepaint *struct{}, w *Window, app *App) {
}

// LayoutNodeStyle implements the root-stretch report.
func (e *emptyElement) LayoutNodeStyle() (Style, bool) {
	style := DefaultStyle()
	style.Display = DisplayNone
	return style, true
}

// ---------------------------------------------------------------------------
// The window draw state: per-window retained element state and the
// per-frame stacks (window.rs Window's element state arena, the
// element_id_stack, text_style_stack, content_mask_stack and
// element_offset_stack)
// ---------------------------------------------------------------------------

// windowDrawState is the element runtime state of one window: the
// retained element states keyed by (global id path, state type) —
// carried across frames — plus the per-frame draw state. The reference
// keeps this inside Window; the port's Window struct is frozen at its
// landed shape, so the state lives in this package-level registry keyed
// by the window pointer. Access is confined to the foreground thread
// (the window draw runs there, like the reference).
type windowDrawState struct {
	// engine is the window's layout engine (the reference keeps one
	// TaffyLayoutEngine per window and clears it every frame).
	engine *LayoutEngine
	// root is the window's root view (window.rs root: AnyView).
	root View
	// elementStates is the retained element state, keyed by the global
	// id path string then the state's Go type (the reference
	// (GlobalElementId, TypeId) key).
	elementStates map[string]map[reflect.Type]any
	// text is the window's text system seam (the reference
	// Arc<WindowTextSystem>).
	text windowTextSystem
	// viewport is the window's client size in logical pixels.
	viewport Size
	// scale is the window's scale factor.
	scale float32
	// rem is the window's rem size in logical pixels.
	rem float32
	// testWindow reports whether this is a deterministic test window
	// (no platform host behind it).
	testWindow bool
	// lastDebugBounds holds the last COMPLETED frame's debug-selector
	// bounds (the reference rendered_frame.debug_bounds).
	lastDebugBounds map[string]Bounds
	// lastScene holds the last completed frame's finished scene (the
	// reference rendered_frame.scene).
	lastScene *Scene
	// lastInlineFacts holds the last completed frame's inline paragraph
	// facts (the port's test-support observable of the inline layer).
	lastInlineFacts []InlineParagraphFacts
	// frame is the in-flight frame draw state; nil outside a draw.
	frame *frameDrawState
	// draws counts completed frame draws of this window.
	draws uint64
	// closed marks a removed window.
	closed bool
	// refreshRequested records that focus or window state requested a
	// redraw since the last completed draw (the invalidation seam; the
	// draw itself is the explicit DrawWindowFrame path).
	refreshRequested bool
	// inspectorHitboxes is the LAST COMPLETED frame's inspector
	// hitbox map (the pin's rendered_frame.inspector_hitboxes; ticket27
	// — picking dispatch reads it between draws).
	inspectorHitboxes map[uint64]InspectorElementID
}

// frameDrawState is one frame's draw state (the reference next_frame
// scene plus the window stacks active during the phases).
type frameDrawState struct {
	// scene receives the frame's paint operations.
	scene *Scene
	// paint carries the scale, current mask and element opacity.
	paint *PaintContext
	// viewport is the window's client size in logical pixels.
	viewport Size
	// scale is the window's scale factor.
	scale float32
	// rem is the window's rem size in logical pixels.
	rem float32
	// elementIDs is the element id stack (window.element_id_stack).
	elementIDs []ElementID
	// textStyles is the text style refinement stack
	// (window.text_style_stack).
	textStyles []TextStyleRefinement
	// contentMask is the current content mask (window.content_mask_stack
	// tail; the base mask is the window bounds).
	contentMask Bounds
	// elementOffset is the current absolute element offset
	// (window.element_offset_stack tail).
	elementOffset Point
	// text is the window's text system seam.
	text windowTextSystem
	// debugBounds is this frame's debug-selector bounds
	// (window.next_frame.debug_bounds).
	debugBounds map[string]Bounds
	// inlineFacts is this frame's inline paragraph facts (the
	// test-support observable; swapped into the window state when the
	// frame completes).
	inlineFacts []InlineParagraphFacts
	// accessedElementStates records every element-state key used
	// during this frame (window.rs accessed_element_states): the
	// frame-end retention carries over exactly these keys from the
	// previous frame's state, dropping the states of elements that no
	// longer draw.
	accessedElementStates map[string]struct{}
	// currentInlineFragments is the fragment geometry of the element
	// whose prepaint/paint is running (window.current_inline_fragments:
	// Some — including an empty list — means the element's content
	// lives inside a parent paragraph; nil means standalone).
	currentInlineFragments []Bounds
	// inlineShapedLines tracks the real WrappedLines referenced by this
	// frame's inline paint pieces (frame-scoped native shaping handles;
	// disposed when the frame completes).
	inlineShapedLines map[*WrappedLine]struct{}
	// renderedViews is the rendered-view stack
	// (window.rendered_entity_stack).
	renderedViews []EntityID
	// inspectorInstanceIDs is the per-frame inspector path instance
	// counter (window.rs Frame::next_inspector_instance_ids, cleared
	// by Frame::clear each frame; ticket27).
	inspectorInstanceIDs map[string]uint64
	// inspectorHitboxes maps hitbox ids to inspector ids for the frame
	// under construction (window.rs Frame::inspector_hitboxes,
	// populated only while picking).
	inspectorHitboxes map[uint64]InspectorElementID
	// inspectorTree is the frame's inspector tree build (ticket27's
	// UI surface; see inspector.go).
	inspectorTree *inspectorTreeBuild
}

// windowDrawStates is the per-window element runtime registry.
var windowDrawStates = map[*Window]*windowDrawState{}

// drawState returns (creating when absent) the window's draw state. It
// must be called on the foreground thread.
func drawState(w *Window) *windowDrawState {
	if w == nil {
		panic("gpui: element runtime requires a live window")
	}
	ds, ok := windowDrawStates[w]
	if !ok {
		engine, err := NewLayoutEngine()
		if err != nil {
			panic(fmt.Sprintf("gpui: creating the window layout engine: %v", err))
		}
		ds = &windowDrawState{
			engine:        engine,
			elementStates: make(map[string]map[reflect.Type]any),
			// The reference window's default rem size (window.rs
			// `rem_size: px(16.)`); test windows and rem overrides
			// overwrite it.
			rem: DefaultRemSize,
		}
		windowDrawStates[w] = ds
	}
	return ds
}

// dropDrawState removes a destroyed window's element runtime state.
func dropDrawState(w *Window) {
	if ds, ok := windowDrawStates[w]; ok {
		if ds.engine != nil {
			_ = ds.engine.Dispose()
		}
		delete(windowDrawStates, w)
	}
	// Retire the per-window diagnostic runtimes with the element
	// runtime (ticket27: the inspector state — generation bumped so
	// outstanding selections resolve dead — and the debug frame
	// overlay).
	dropWindowDiagnostics(w)
}

// currentFrame returns the window's in-flight frame draw state.
func currentFrame(w *Window) *frameDrawState {
	ds := drawState(w)
	if ds.frame == nil {
		panic("gpui: no frame is being drawn for this window (request_layout/prepaint/paint run inside a window draw)")
	}
	return ds.frame
}

// recordDebugBounds records one debug-selector bounds for this frame
// (window.rs next_frame.debug_bounds.insert — the test-support bounds
// probe).
func recordDebugBounds(w *Window, selector string, bounds Bounds) {
	f := currentFrame(w)
	if f.debugBounds == nil {
		f.debugBounds = make(map[string]Bounds)
	}
	f.debugBounds[selector] = bounds
}

// pushElementID pushes an element id onto the window's id stack
// (window.rs with_id / element_id_stack).
func pushElementID(w *Window, id ElementID) {
	f := currentFrame(w)
	f.elementIDs = append(f.elementIDs, id)
}

// popElementID pops the innermost element id.
func popElementID(w *Window) {
	f := currentFrame(w)
	f.elementIDs = f.elementIDs[:len(f.elementIDs)-1]
}

// currentElementIDs returns a copy of the window's element id stack.
func currentElementIDs(w *Window) []ElementID {
	f := currentFrame(w)
	return append([]ElementID{}, f.elementIDs...)
}

// WithID runs f with the given element id pushed onto the window's
// element id stack (window.rs with_id): id-less children of an
// identified parent are isolated by a chosen name (the reference
// isolates stateless views by type name).
func WithID[R any](w *Window, id ElementID, f func(*Window) R) R {
	pushElementID(w, id)
	defer popElementID(w)
	return f(w)
}

// WithTextStyle pushes a text style refinement onto the window's text
// style stack around f (window.rs with_text_style; nil refinement is a
// no-op).
func WithTextStyle[R any](w *Window, refinement *TextStyleRefinement, f func(*Window) R) R {
	frame := currentFrame(w)
	if refinement == nil {
		return f(w)
	}
	frame.textStyles = append(frame.textStyles, *refinement)
	defer func() { frame.textStyles = frame.textStyles[:len(frame.textStyles)-1] }()
	return f(w)
}

// WindowTextStyle returns the ambient text style: the default text
// style refined by every refinement on the window's text style stack
// (window.rs text_style).
func WindowTextStyle(w *Window) TextStyle {
	frame := currentFrame(w)
	style := DefaultTextStyle()
	for i := range frame.textStyles {
		frame.textStyles[i].Refine(&style)
	}
	return style
}

// WithContentMask intersects the given mask with the window's current
// content mask and runs f under it (window.rs with_content_mask).
func WithContentMask[R any](w *Window, mask *Bounds, f func(*Window) R) R {
	frame := currentFrame(w)
	if mask == nil {
		return f(w)
	}
	intersected := intersectBounds(*mask, frame.contentMask)
	previous := frame.contentMask
	frame.contentMask = intersected
	frame.paint.Mask = intersected
	defer func() {
		frame.contentMask = previous
		frame.paint.Mask = previous
	}()
	return f(w)
}

// WithElementOpacity multiplies an element opacity into the window's
// paint context around f (window.rs with_element_opacity; the reference
// takes Option<f32> — nil is a no-op).
func WithElementOpacity[R any](w *Window, opacity *float32, f func(*Window) R) R {
	frame := currentFrame(w)
	if opacity == nil {
		return f(w)
	}
	previous := frame.paint
	frame.paint = frame.paint.WithElementOpacity(*opacity)
	defer func() { frame.paint = previous }()
	return f(w)
}

// WithElementOffset adds a relative element offset (scrolling) around
// f (window.rs with_element_offset: a zero offset is a no-op).
func WithElementOffset[R any](w *Window, offset Point, f func(*Window) R) R {
	if offset.X == 0 && offset.Y == 0 {
		return f(w)
	}
	frame := currentFrame(w)
	abs := Point{X: frame.elementOffset.X + offset.X, Y: frame.elementOffset.Y + offset.Y}
	return WithAbsoluteElementOffset(w, abs, f)
}

// WithAbsoluteElementOffset sets the absolute element offset around f
// (window.rs with_absolute_element_offset).
func WithAbsoluteElementOffset[R any](w *Window, offset Point, f func(*Window) R) R {
	frame := currentFrame(w)
	previous := frame.elementOffset
	frame.elementOffset = offset
	defer func() { frame.elementOffset = previous }()
	return f(w)
}

// ElementOffset returns the current absolute element offset in logical
// pixels (window.rs element_offset).
func ElementOffset(w *Window) Point { return currentFrame(w).elementOffset }

// ContentMask returns the current content mask in logical pixels
// (window.rs content_mask).
func ContentMask(w *Window) Bounds { return currentFrame(w).contentMask }

// RemSize returns the window's rem size in logical pixels (window.rs
// rem_size).
func RemSize(w *Window) float32 { return currentFrame(w).rem }

// ScaleFactorOf returns the window's scale factor (window.rs
// scale_factor; the standalone spelling avoids the host-lease method).
func ScaleFactorOf(w *Window) float32 { return currentFrame(w).scale }

// WithRemSize scopes a rem override around f (window.rs with_rem_size:
// the override resolves rem lengths of trees REQUESTED inside it).
func WithRemSize[R any](w *Window, rem float32, f func(*Window) R) R {
	frame := currentFrame(w)
	previous := frame.rem
	frame.rem = rem
	defer func() { frame.rem = previous }()
	return f(w)
}

// WithTextStyleVoid is the void form of WithTextStyle.
func WithTextStyleVoid(w *Window, refinement *TextStyleRefinement, f func(*Window)) {
	WithTextStyle(w, refinement, func(w *Window) struct{} {
		f(w)
		return struct{}{}
	})
}

// WithContentMaskVoid is the void form of WithContentMask.
func WithContentMaskVoid(w *Window, mask *Bounds, f func(*Window)) {
	WithContentMask(w, mask, func(w *Window) struct{} {
		f(w)
		return struct{}{}
	})
}

// WithElementOpacityVoid is the void form of WithElementOpacity.
func WithElementOpacityVoid(w *Window, opacity *float32, f func(*Window)) {
	WithElementOpacity(w, opacity, func(w *Window) struct{} {
		f(w)
		return struct{}{}
	})
}

// WithAbsoluteElementOffsetVoid is the void form of
// WithAbsoluteElementOffset.
func WithAbsoluteElementOffsetVoid(w *Window, offset Point, f func(*Window)) {
	WithAbsoluteElementOffset(w, offset, func(w *Window) struct{} {
		f(w)
		return struct{}{}
	})
}

// PixelSnap snaps one logical length to the device-pixel grid and back
// (window.rs pixel_snap).
func PixelSnap(w *Window, value float32) float32 {
	scale := currentFrame(w).scale
	return pixelSnap(value, scale)
}

// PixelSnapValue is the standalone pixel snap for a scale factor (the
// window-less form fixtures use when deriving facts outside a draw).
func PixelSnapValue(value, scaleFactor float32) float32 {
	return pixelSnap(value, scaleFactor)
}

// ---------------------------------------------------------------------------
// Layout plumbing for elements (the window.rs request_layout /
// compute_layout / layout_bounds / parent_relative_layout_bounds
// adapters over the window's engine)
// ---------------------------------------------------------------------------

// layoutContextOf builds the window's current layout context: rem,
// scale and the current absolute element offset.
func layoutContextOf(w *Window) *LayoutContext {
	f := currentFrame(w)
	return NewLayoutContext(f.rem, f.scale).WithElementOffset(f.elementOffset)
}

// requestLayoutOf requests one layout node with the given style and
// children on the window's engine (window.rs request_layout).
func requestLayoutOf(w *Window, style Style, children ...LayoutID) (LayoutID, error) {
	ds := drawState(w)
	return ds.engine.RequestLayout(layoutContextOf(w), style, children...)
}

// requestMeasuredLayoutOf requests one measured leaf node (window.rs
// request_measured_layout).
func requestMeasuredLayoutOf(w *Window, style Style, measure MeasureFunc) (LayoutID, error) {
	ds := drawState(w)
	return ds.engine.RequestMeasuredLayout(layoutContextOf(w), style, measure)
}

// computeLayoutOf computes one subtree under the given available space
// (window.rs compute_layout).
func computeLayoutOf(w *Window, root LayoutID, available AvailableSize) error {
	ds := drawState(w)
	return ds.engine.ComputeLayout(layoutContextOf(w), root, available)
}

// layoutBoundsOf queries one node's window-local bounds (window.rs
// layout_bounds).
func layoutBoundsOf(w *Window, id LayoutID) (Bounds, error) {
	ds := drawState(w)
	return ds.engine.LayoutBounds(layoutContextOf(w), id)
}

// parentRelativeLayoutBoundsOf queries one node's parent-relative
// bounds (window.rs parent_relative_layout_bounds; text placement).
func parentRelativeLayoutBoundsOf(w *Window, id LayoutID) (Bounds, error) {
	ds := drawState(w)
	return ds.engine.ParentRelativeLayoutBounds(layoutContextOf(w), id)
}

// StretchAutoSizeToFill stretches the given node's auto-sized axes to
// the definite viewport size (the port of taffy.rs
// stretch_auto_size_to_fill + taffy.set_style as used by draw_roots:
// "window roots fill the window when their size is auto"). The port
// engine does not retain per-node styles (the reference re-reads the
// style from taffy), so the caller supplies the style the node was
// requested with.
func StretchAutoSizeToFill(w *Window, id LayoutID, style Style, viewport Size) error {
	ds := drawState(w)
	ctx := layoutContextOf(w)
	stretched := style
	changed := false
	if style.Size.Width.Auto {
		stretched.Size.Width = PxLength(roundToDevicePixel(viewport.Width, ctx.ScaleFactor()))
		changed = true
	}
	if style.Size.Height.Auto {
		stretched.Size.Height = PxLength(roundToDevicePixel(viewport.Height, ctx.ScaleFactor()))
		changed = true
	}
	if !changed {
		return nil
	}
	if err := ds.engine.checkID(id, "StretchAutoSizeToFill"); err != nil {
		return err
	}
	record := styleToRecord(&stretched, ctx)
	if err := ds.engine.native.SetStyle(id.node, &record); err != nil {
		return fmt.Errorf("gpui: StretchAutoSizeToFill: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Retained element state (window.rs with_element_state)
// ---------------------------------------------------------------------------

// WithElementState runs f with the retained state of type S keyed by
// the element's global id path in the window's element state arena
// (window.rs with_element_state). state is nil when no state was
// retained yet; f returns the new state to retain. Reentrant access for
// the same key and type panics (the reference's reentrancy guard). The
// state survives across frames until no element with that path draws.
func WithElementState[S any, R any](w *Window, global *GlobalElementID, f func(state *S, w *Window) (R, S)) R {
	ds := drawState(w)
	if global == nil {
		panic("gpui: WithElementState requires an element id (use WithOptionalElementState for id-less elements)")
	}
	return withElementStateOf[S, R](ds, global.key(), f, w)
}

// WithOptionalElementState is the id-optional variant (window.rs
// with_optional_element_state): an element without an id retains no
// state. The retained slot holds *S, so "not yet initialized" is
// representable (the reference's ElementStateBox inner Option<S>).
func WithOptionalElementState[S any, R any](w *Window, global *GlobalElementID, f func(state *S, w *Window) (R, *S)) R {
	if global == nil {
		result, _ := f(nil, w)
		return result
	}
	ds := drawState(w)
	key := global.key()
	recordElementStateAccess(w, key)
	slot, existed := takeElementStateSlot[S](ds, key)
	reentryGuard := elementStateReentry{key: key, stateType: reflect.TypeFor[S]()}
	reentryGuard.enter()
	defer reentryGuard.exit()
	result, retained := f(slot, w)
	if retained == nil && !existed {
		// No state to retain (the reference requires returning none only
		// when no id was supplied).
		return result
	}
	if retained == nil && existed {
		putElementState(ds, key, reflect.TypeFor[S](), slot)
		return result
	}
	putElementState(ds, key, reflect.TypeFor[S](), retained)
	return result
}

// withElementStateOf is the plain (non-optional) variant.
func withElementStateOf[S any, R any](ds *windowDrawState, key string, f func(state *S, w *Window) (R, S), w *Window) R {
	recordElementStateAccess(w, key)
	slot, existed := takeElementStateSlot[S](ds, key)
	reentryGuard := elementStateReentry{key: key, stateType: reflect.TypeFor[S]()}
	reentryGuard.enter()
	defer reentryGuard.exit()
	var previous *S
	if existed {
		previous = slot
	}
	result, retained := f(previous, w)
	putElementState(ds, key, reflect.TypeFor[S](), &retained)
	return result
}

// recordElementStateAccess marks an element-state key as used in the
// frame under construction (window.rs accessed_element_states push).
// Access outside a frame (allowed by this seam's diagnostics tests)
// records nothing: retention follows the draws that used the state.
func recordElementStateAccess(w *Window, key string) {
	if ds := drawState(w); ds.frame != nil {
		if ds.frame.accessedElementStates == nil {
			ds.frame.accessedElementStates = make(map[string]struct{})
		}
		ds.frame.accessedElementStates[key] = struct{}{}
	}
}

// retainElementStates filters the window's retained element state to
// the keys accessed during the completed frame (window.rs
// Frame::finish's carry-over loop): state of elements that did not
// draw this frame is dropped, so removal followed by reinsertion
// starts fresh.
func retainElementStates(ds *windowDrawState, accessed map[string]struct{}) {
	retained := make(map[string]map[reflect.Type]any, len(accessed))
	for key := range accessed {
		if byType, ok := ds.elementStates[key]; ok {
			retained[key] = byType
		}
	}
	ds.elementStates = retained
}

// takeElementStateSlot removes and returns the retained *S slot for the
// key (the reference removes the entry while the callback runs and
// reinserts after, which makes reentrant access observable).
func takeElementStateSlot[S any](ds *windowDrawState, key string) (*S, bool) {
	stateType := reflect.TypeFor[S]()
	byType, ok := ds.elementStates[key]
	if !ok {
		return nil, false
	}
	value, ok := byType[stateType]
	if !ok {
		return nil, false
	}
	delete(byType, stateType)
	if len(byType) == 0 {
		delete(ds.elementStates, key)
	}
	slot, ok := value.(*S)
	if !ok {
		panic(fmt.Sprintf("gpui: invalid element state type for id %q, requested %s", key, stateType))
	}
	return slot, true
}

// putElementState retains the state slot for the key.
func putElementState(ds *windowDrawState, key string, stateType reflect.Type, value any) {
	byType, ok := ds.elementStates[key]
	if !ok {
		byType = make(map[reflect.Type]any)
		ds.elementStates[key] = byType
	}
	byType[stateType] = value
}

// elementStateReentry is the reentrant-access guard (the reference
// "reentrant call to with_element_state for the same state type and
// element id" panic).
type elementStateReentry struct {
	key       string
	stateType reflect.Type
}

var elementStateActive = map[string]map[reflect.Type]bool{}

func (g elementStateReentry) enter() {
	set, ok := elementStateActive[g.key]
	if !ok {
		set = make(map[reflect.Type]bool)
		elementStateActive[g.key] = set
	}
	if set[g.stateType] {
		panic(fmt.Sprintf("gpui: reentrant call to with_element_state for the same state type %s and element id %q", g.stateType, g.key))
	}
	set[g.stateType] = true
}

func (g elementStateReentry) exit() {
	if set, ok := elementStateActive[g.key]; ok {
		delete(set, g.stateType)
		if len(set) == 0 {
			delete(elementStateActive, g.key)
		}
	}
}

// RequestElementLayout requests one layout node with the given style and
// children on the window's engine — the element-author-facing seam of
// the phases (the reference Window::request_layout, which custom
// element authors call from Element.RequestLayout).
func RequestElementLayout(w *Window, style Style, children ...LayoutID) (LayoutID, error) {
	return requestLayoutOf(w, style, children...)
}

// RequestElementMeasuredLayout requests one measured leaf layout node
// (the reference Window::request_measured_layout).
func RequestElementMeasuredLayout(w *Window, style Style, measure MeasureFunc) (LayoutID, error) {
	return requestMeasuredLayoutOf(w, style, measure)
}

// ElementStateOf reads one retained element state of type S keyed by the
// global id path (a read-only observable of the retained-state model).
func ElementStateOf[S any](w *Window, global *GlobalElementID) (S, bool) {
	ds, ok := windowDrawStates[w]
	if !ok || global == nil {
		var zero S
		return zero, false
	}
	byType, ok := ds.elementStates[global.key()]
	if !ok {
		var zero S
		return zero, false
	}
	value, ok := byType[reflect.TypeFor[S]()]
	if !ok {
		var zero S
		return zero, false
	}
	slot, ok := value.(*S)
	if !ok || slot == nil {
		var zero S
		return zero, false
	}
	return *slot, true
}

// ElementStateCount reports how many retained element states the window
// holds (a diagnostic observable for tests; the reference exposes none
// publicly).
func ElementStateCount(w *Window) int {
	ds, ok := windowDrawStates[w]
	if !ok {
		return 0
	}
	count := 0
	for _, byType := range ds.elementStates {
		count += len(byType)
	}
	return count
}
