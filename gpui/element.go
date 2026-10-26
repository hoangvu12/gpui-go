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
	"sort"

	"github.com/hoangvu12/gpui-go/internal/native"
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
	// ElementIDNamedInteger is a name plus an integer (the reference
	// ElementId::NamedInteger, window.rs:7667; e.g. ("item", ix)).
	ElementIDNamedInteger
	// ElementIDNamedChild is a labeled child of another element id (the
	// reference ElementId::NamedChild(Arc<ElementId>, SharedString),
	// window.rs:7673).
	ElementIDNamedChild
	// ElementIDCodeLocation is a source-location identity (the reference
	// ElementId::CodeLocation, window.rs:7671; Window.UseState keys its
	// state with the caller's location).
	ElementIDCodeLocation
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
	// Integer carries the numeric identity (Kind == ElementIDInteger, and
	// the integer half of ElementIDNamedInteger).
	Integer uint64
	// Parent carries the labeled element of an ElementIDNamedChild (the
	// child's label is Name).
	Parent *ElementID
}

// NamedIntegerElementID builds a name+integer identity (the reference
// ElementId::NamedInteger and its From<(&str, usize|u64|u32)> /
// named_usize conversions, window.rs:7667,7680-7682,7770-7798).
func NamedIntegerElementID(name string, value uint64) ElementID {
	return ElementID{Kind: ElementIDNamedInteger, Name: name, Integer: value}
}

// NamedChildElementID builds a labeled child of parent (the reference
// From<(ElementId, T: Into<SharedString>)> for ElementId, window.rs:7800-7803,
// which produces ElementId::NamedChild).
func NamedChildElementID(parent ElementID, name string) ElementID {
	p := parent
	return ElementID{Kind: ElementIDNamedChild, Name: name, Parent: &p}
}

// codeLocationElementID builds the caller-location identity used by
// Window.UseState (the reference ElementId::CodeLocation built from
// core::panic::Location::caller(), window.rs:4127-4131). Go has no
// column information, so the location renders as "file:line".
func codeLocationElementID(location string) ElementID {
	return ElementID{Kind: ElementIDCodeLocation, Name: location}
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
	case ElementIDNamedInteger:
		// window.rs:7692 `{}-{}` (name, integer).
		return fmt.Sprintf("%s-%d", id.Name, id.Integer)
	case ElementIDNamedChild:
		// window.rs:7696 `{}-{}` (parent id, label).
		parent := ""
		if id.Parent != nil {
			parent = id.Parent.String()
		}
		return parent + "-" + id.Name
	case ElementIDCodeLocation:
		// window.rs:7695 renders the Location (file:line:col; Go has no column).
		return id.Name
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
	// bounds are the layout bounds resolved at prepaint, under the
	// element offset prepaint ran with, and handed unchanged to paint
	// (the reference ElementDrawPhase::Prepaint::bounds, element.rs:
	// 537-545, consumed by Drawable::paint at 557-600).
	bounds Bounds
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

// paintsAfterInFlow implements elementObject's sticky paint-order
// query (browser-core ticket 34, sticky.go): the drawable forwards it
// to its element — a sticky div reports true.
func (d *drawable[L, P, E]) paintsAfterInFlow() bool {
	if p, ok := any(d.element).(interface{ PaintsAfterInFlow() bool }); ok {
		return p.PaintsAfterInFlow()
	}
	return false
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
	d.bounds = bounds
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
// bounds committed at prepaint and the phase states (the reference
// Drawable::paint, element.rs:557-600, which destructures the Prepaint
// phase's bounds rather than querying the layout again). An element
// prepainted under an absolute offset (PrepaintAt, PrepaintAsRoot)
// therefore paints at that offset whatever offset its paint runs under.
func (d *drawable[L, P, E]) paint(global *GlobalElementID, inspector *InspectorElementID, w *Window, app *App) {
	if d.phase != phasePrepainted {
		panic("gpui: must call prepaint before paint")
	}
	ownID, hasID := d.element.ID()
	if hasID {
		pushElementID(w, ownID)
		defer popElementID(w)
	}
	bounds := d.bounds
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
		// browser-core 76: a standalone root's request walk runs with the
		// enclosing containing-block scopes detached (List/UniformList/
		// virtual-list items, measured trees, cached-view refreshes, the
		// deferred surfaces): the subtree IS its own native root, so its
		// static elements keep the pin's direct-parent rule at the root
		// boundary, the CSS rule keeps holding inside it, and no deferred
		// child of a discarded measurement leaks into the live tree's
		// scopes (containingblock.go).
		withIsolatedContainingBlocks(w, func(w *Window) struct{} {
			d.requestLayout(nil, nil, w, app)
			return struct{}{}
		})
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

// PrepaintAt prepaints the element at the given absolute origin
// (AnyElement::prepaint_at: with_absolute_element_offset).
func (e AnyElement) PrepaintAt(origin Point, w *Window, app *App) {
	WithAbsoluteElementOffsetVoid(w, origin, func(w *Window) {
		e.Prepaint(w, app)
	})
}

// PrepainAt is the misspelled name v0.1.0 shipped for PrepaintAt.
//
// Deprecated: use PrepaintAt.
func (e AnyElement) PrepainAt(origin Point, w *Window, app *App) {
	e.PrepaintAt(origin, w, app)
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
// conversion). Browser-core 20 (spec D7): the text joins the window's
// selection by default — drag, double and triple click select it —
// and shows the automatic I-beam while user-select allows selection;
// UserSelect(None) on an ancestor opts the subtree out.
func Text(text string) AnyElement {
	return CustomElement[stringLayoutState, Hitbox](&textElement{text: text})
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
	// userSelectionState is the window's selection state, acquired once
	// into the window's scope (browser-core 20): installWindowTextSelection
	// resolves it through this handle, so the acquisition's lease count
	// stays one for the window's lifetime.
	userSelectionState WeakEntity[windowSelectionState]
	// userSelectParticipants are the automatic selection participants'
	// handles, indexed by the paint-order slot (browser-core 20): the
	// handle for slot i is created once and lives for the window — the
	// entities are owned by the window's scope — so the per-frame lookup
	// is an array index with no allocation.
	userSelectParticipants []TextSelectionHandle
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
	// lastAnchorBounds holds the last COMPLETED frame's element-anchored
	// anchor registry (ticket 25: the name -> anchorRecord map the
	// Anchored element's AnchorTo reads when the anchor has not yet
	// prepainted in the in-flight frame — the first frame or an anchor
	// later in the tree).
	lastAnchorBounds map[string]anchorRecord
	// trackAnchors records that this window has drawn an Anchored
	// element anchored to an element id (ticket 25): the div prepaint
	// then records named elements' bounds into the frame's anchor
	// registry. It is set during request layout (the whole tree
	// requests before any prepaint) and stays on once set, so a
	// window's plain frames pay no recording cost until it anchors.
	trackAnchors bool
	// lastInvokerPaths holds the last COMPLETED frame's invoker
	// registry (browser-core ticket 21): the name -> element-path-key
	// map the TopLayer elements' Invoker lookups read when the invoker
	// has not yet prepainted in the in-flight frame.
	lastInvokerPaths map[string]string
	// trackInvokers records that this window's overlays name their
	// invokers (ticket 21): the div prepaint then records named
	// elements' paths into the frame's invoker registry. Set during
	// request layout, like trackAnchors.
	trackInvokers bool
	// lastScene holds the last completed frame's finished scene (the
	// reference rendered_frame.scene).
	lastScene *Scene
	// lastInlineFacts holds the last completed frame's inline paragraph
	// facts (the port's test-support observable of the inline layer).
	lastInlineFacts []InlineParagraphFacts
	// lastAutoScrollbarFacts holds the last completed frame's shown
	// overflow: auto scrollbar pairs (browser-core 36, scrollbar.go).
	lastAutoScrollbarFacts []AutoScrollbarWindowFacts
	// frame is the in-flight frame draw state; nil outside a draw.
	frame *frameDrawState
	// autoScrollbarAppearanceChanged records that this frame's prepaint
	// changed an overflow: auto element's shown-scrollbar flags
	// (browser-core 36, scrollbar.go): the notify the prepaint raises is
	// ignored for the dirty marking while the window draws (the pin's
	// draw-phase guard, invalidateViewWindows), so completeFrame marks
	// the window dirty afterwards — the same pattern as a focus move's
	// refresh — and the reservation settles on the next frame (the
	// one-frame settle, Chromium's post-layout relayout).
	autoScrollbarAppearanceChanged bool
	// draws counts completed frame draws of this window.
	draws uint64
	// closed marks a removed window.
	closed bool
	// finishing reports a completed frame being published (render.go
	// completeFrame): the window still counts as drawing.
	finishing bool
	// refreshRequested records that focus or window state requested a
	// redraw since the last completed draw (the invalidation seam; it is
	// half of the frame loop's dirty flag, frameloop.go isFrameDirty).
	refreshRequested bool
	// inspectorHitboxes is the LAST COMPLETED frame's inspector
	// hitbox map (the pin's rendered_frame.inspector_hitboxes; ticket27
	// — picking dispatch reads it between draws).
	inspectorHitboxes map[uint64]InspectorElementID
	// lastDeferredPriorities holds the last COMPLETED frame's deferred
	// draw priorities in push order (the pinned
	// rendered_frame.deferred_draws observable the deferred.rs tests read;
	// Base ticket 08).
	lastDeferredPriorities []int
	// clientInset is the window's client inset (window.rs client_inset:
	// Option<Pixels>, set by Window.SetClientInset; paintapi.go).
	clientInset *float32
	// selfLeases caches the window-owned strong self-handle leases of
	// the views this window renders (Context.StrongSelf): one lease per
	// view entity, retained on the view's first self-handle read during
	// a render and registered in this window's scope, so it — and every
	// alias the view hands to its listeners — releases deterministically
	// when the window closes. The leases themselves are owned by the
	// window's scope; this map is the per-window install cache, dropped
	// with the element runtime (dropDrawState).
	selfLeases map[EntityID]*entityLease
	// refreshing is window.rs Window::refreshing (R:window.rs:1316): set
	// by Window.Refresh / the app refresh effect, it makes every cached
	// view of the next frame render afresh (view_cache.go); the cached
	// path also sets it while a cache miss renders. Cleared when a frame
	// completes.
	refreshing bool
	// invalidatedViews are the entities notified since the last draw (the
	// WindowInvalidator's dirty_views, R:window.rs:148, 189-214);
	// dirtyViews are the views marked dirty for the frame being drawn
	// (window.rs dirty_views, R:window.rs:1289) — the notified views and
	// their ancestor views (view_cache.go invalidateEntities).
	invalidatedViews map[EntityID]struct{}
	dirtyViews       map[EntityID]struct{}
	// lastAccessedElementStateLog and lastDeferredDraws are the last
	// COMPLETED frame's ordered element-state accesses and deferred draws
	// (rendered_frame.accessed_element_states / deferred_draws): the
	// source lists a cached view's reused ranges index into.
	lastAccessedElementStateLog []string
	lastDeferredDraws           []*deferredDraw
	// viewCacheHits and viewCacheMisses count cached-view prepaints that
	// reused their previous range or rendered afresh (test-support
	// diagnostics; the pin has none).
	viewCacheHits, viewCacheMisses uint64
}

// isViewDirty reports whether the view is dirty in the frame being
// drawn (window.dirty_views.contains, R:view.rs:434).
func (ds *windowDrawState) isViewDirty(id EntityID) bool {
	_, dirty := ds.dirtyViews[id]
	return dirty
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
	// paintPhase reports that the draw's paint walk is running (the
	// request and prepaint phases are over): only then do the R16 group
	// scopes insert their effect-group markers - a marker pair inserted
	// during prepaint would bracket nothing (the paint-phase children
	// sort above the pair's order floor) and silently drop the clip
	// (browser-core ticket 55).
	paintPhase bool
	// containingBlockScopes is the open containing-block scope stack of
	// the request-layout phase (browser-core ticket 33, spec D3;
	// containingblock.go): the innermost scope belongs to the nearest
	// positioned ancestor of the elements whose children are currently
	// being requested, and static parents defer their absolute children
	// into it. Balanced per element; empty outside a request walk.
	containingBlockScopes []*containingBlockScope
	// icbPending marks the request walk that builds the window's root
	// node: drawRoots sets it around the root element's request, and the
	// first scope open (openContainingBlock) consumes it to force the
	// root's ICB scope even when the root's own style is static, while
	// the plain requestLayoutOf seam discards it (the non-participating-
	// root approximation). See containingblock.go.
	icbPending bool
	// stickyScrollers is the open scroll-container scope stack of the
	// prepaint phase (browser-core ticket 34, spec L3; sticky.go): the
	// innermost context belongs to the nearest scroll container of the
	// elements whose prepaint is running, and sticky elements resolve
	// their constraints against it. Balanced per scroller around its
	// child walk; empty outside a prepaint walk or in frames with no
	// scrollers.
	stickyScrollers []stickyScrollContext
	// stickyRequested reports that this frame's request walk saw a
	// PositionSticky div (sticky.go): only then do divs' prepaints fill
	// stickyFlowBoxes, so views without sticky pay one boolean check.
	stickyRequested bool
	// fixedRequested reports that this frame's request walk saw a
	// PositionFixed node (browser-core 35, fixed.go): only then do the
	// node requests scan their children for fixed ones to defer into
	// the root scope, so views without fixed elements pay one boolean
	// check per node request.
	fixedRequested bool
	// stickyFlowBoxes is the frame's flow-rectangle registry for sticky
	// descendants (sticky.go): the native layout node of each div whose
	// prepaint ran, mapped to its flow containing-block rect (the
	// content box, or the scroller's scrollable extent). Filled only
	// while stickyRequested.
	stickyFlowBoxes map[native.NodeHandle]Bounds
	// contentsContinuations is the display:contents splice register of
	// the request-layout phase (browser-core 36; contents.go): a
	// contents element's first child id mapped to the children that
	// join it at the next node request. Entries are consumed exactly
	// once (expandContentsChildren); the map dies with the frame.
	contentsContinuations map[LayoutID][]LayoutID
	// elementOffset is the current absolute element offset
	// (window.element_offset_stack tail).
	elementOffset Point
	// transformScopes is the open element-transform stack of the
	// prepaint phase (browser-core ticket 56): each entry is the
	// COMPOSED window-to-window matrix at that depth (the parent's
	// matrix left-multiplied by the element's own), so the top of the
	// stack is the matrix a hitbox inserted right now carries — the
	// hit test then maps the pointer through the inverse, and nested
	// transforms compose exactly as CSS composes them down the tree.
	// Balanced around each transformed element's subtree prepaint;
	// empty for frames without transforms (one nil check at hitbox
	// insertion).
	transformScopes []TransformationMatrix
	// text is the window's text system seam.
	text windowTextSystem
	// debugBounds is this frame's debug-selector bounds
	// (window.next_frame.debug_bounds).
	debugBounds map[string]Bounds
	// anchorBounds is this frame's element-anchored anchor registry
	// (ticket 25): the name-id -> anchorRecord map filled by the div
	// prepaint of named elements, read by the Anchored element's
	// AnchorTo at its (usually deferred) prepaint.
	anchorBounds map[string]anchorRecord
	// invokerPaths is this frame's invoker registry (browser-core
	// ticket 21): the name -> element-path-key map filled by the div
	// prepaint of named elements, read at the frame-end overlay
	// reconciliation (toplayer.go finishOverlayFrame) to resolve a
	// TopLayer element's Invoker name onto the invoker's path.
	invokerPaths map[string]string
	// inlineFacts is this frame's inline paragraph facts (the
	// test-support observable; swapped into the window state when the
	// frame completes).
	inlineFacts []InlineParagraphFacts
	// autoScrollbarFacts is this frame's shown overflow: auto scrollbar
	// pairs (browser-core 36, scrollbar.go; the test-support observable
	// behind WindowAutoScrollbarFacts, swapped into the window state
	// when the frame completes).
	autoScrollbarFacts []AutoScrollbarWindowFacts
	// accessedElementStates records every element-state key used
	// during this frame (window.rs accessed_element_states): the
	// frame-end retention carries over exactly these keys from the
	// previous frame's state, dropping the states of elements that no
	// longer draw.
	accessedElementStates map[string]struct{}
	// accessedElementStateLog is the same accesses in order, duplicates
	// included (the pin's accessed_element_states Vec): the address space
	// of the prepaint and paint indices' accessed_element_states_index
	// (view_cache.go).
	accessedElementStateLog []string
	// currentInlineFragments is the fragment geometry of the element
	// whose prepaint/paint is running (window.current_inline_fragments:
	// Some — including an empty list — means the element's content
	// lives inside a parent paragraph; nil means standalone).
	currentInlineFragments []Bounds
	// userSelectSlot is this frame's paint-order counter of the
	// user-select text participants (browser-core 20): the slot part of
	// the keyed identity each auto-registered text's participant lives
	// under. Paint order within a frame is document order for a static
	// tree, so the slot is stable across frames.
	userSelectSlot uint64
	// userSelectPending collects the frame's automatic registrations
	// (browser-core 20): the flush at the frame's effect flush applies
	// them all in one window-state update, before any input dispatch —
	// an idle frame costs no per-text entity update.
	userSelectPending []userSelectRegistration
	// userSelectBand is the window rectangle the live selection's
	// endpoints span (browser-core 20), recorded at the frame's head:
	// only the texts it can touch read their participant snapshot and
	// paint the wash. None while nothing is selected.
	userSelectBand Optional[Bounds]
	// userSelectSuppressed is the open WithUserSelectSuppressed depth
	// (browser-core 20): the subtree prepainting/painting under it — a
	// drag preview snapshot, the inspector UI, an inline flow's object
	// fragments — is not part of the window's plain-text document, so
	// its texts never join the selection.
	userSelectSuppressed int
	// renderedViews is the rendered-view stack
	// (window.rendered_entity_stack).
	renderedViews []EntityID
	// trackingAccess reports that the frame's element phases are running:
	// entity accesses are then tracked by the window (Base ticket 33,
	// frameloop.go recordEntityAccess). It is off during the effect flush
	// that closes the draw's update.
	trackingAccess bool
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
	// deferredDraws are the frame's deferred draws (window.rs
	// Frame::deferred_draws, pushed by Window.DeferDraw during prepaint and
	// drawn by drawRoots after the root; Base ticket 08, deferred.go).
	deferredDraws []*deferredDraw
	// deferredPriorities is the pinned DeferredPriorityStackCache
	// (elements/deferred.rs:138-155): the priorities of the deferred draws
	// whose prepaint is running, innermost last. The pin keeps it in an app
	// global that is only non-empty inside prepaint_deferred_draws; the
	// port scopes it to the frame under construction.
	deferredPriorities []int
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
		// The window's last drawn scene dies with its runtime (gpui-perf
		// ticket 07).
		if ds.lastScene != nil {
			_ = ds.lastScene.Dispose()
			ds.lastScene = nil
		}
		// A real window's text system owns one native atlas slot (the
		// service bounds live atlases at 8) and a line-layout cache of
		// native shaping handles: both retire with the window's element
		// runtime, like the engine above (Base ticket 33 — with every real
		// window drawn by the frame loop, leaked slots exhausted the bound
		// across a test process; the cache's lines dispose the same way,
		// the reference window's Arc-held lines dying with its
		// WindowTextSystem).
		if rts, ok := ds.text.(*realWindowTextSystem); ok {
			rts.lines.dispose()
			if rts.atlas != nil {
				_ = rts.atlas.Dispose()
			}
		}
		delete(windowDrawStates, w)
		// Dropping the window's element states drops their resources (the
		// reference drops the frames with the window): keyed-state leases
		// release here (idempotent when the window scope already closed
		// them); the queued releases flush at the next update boundary.
		keys := make([]string, 0, len(ds.elementStates))
		for key := range ds.elementStates {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			for _, r := range releasersOf(ds.elementStates[key]) {
				r.releaseElementState()
			}
		}
		ds.elementStates = nil
		// The window's view self-handle leases released with the window's
		// scope (removeWindow / CloseTestWindow close it before the element
		// runtime drops); the install cache retires here with the runtime.
		ds.selfLeases = nil
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

// anchorRecord is one tracked anchor of the element-anchored
// positioning (ticket 25): the anchor element's window-space bounds at
// its prepaint, and the content mask there — its clipping scroller's
// visible rectangle, which position-visibility: anchors-visible hides
// the anchored element against (Chromium's intersection observer over
// the anchor's nearest clipping ancestor,
// anchor_position_visibility_observer.cc:33-71).
type anchorRecord struct {
	bounds Bounds
	clip   Bounds
}

// recordAnchorBounds records one named element's bounds and clip into
// this frame's anchor registry (ticket 25: the port of CSS
// position-anchor's anchor lookup — the anchor side names itself by
// its element id). The div prepaint calls it for elements carrying a
// plain name id, in prepaint order (a later same-named element wins);
// the registry is only filled once the window's Anchored elements
// asked for it (windowDrawState.trackAnchors, set at request layout).
func recordAnchorBounds(w *Window, name string, bounds, clip Bounds) {
	f := currentFrame(w)
	if f.anchorBounds == nil {
		f.anchorBounds = make(map[string]anchorRecord)
	}
	f.anchorBounds[name] = anchorRecord{bounds: bounds, clip: clip}
}

// lookupAnchorBounds resolves one anchor by name for the Anchored
// element's prepaint (ticket 25): the in-flight frame's registry first
// — the anchor prepaints before the (usually deferred) anchored
// element, so a static or scrolled anchor resolves at its current
// position — then the last completed frame's (the first frame, or an
// anchor later in the tree than the anchored element, which lags one
// frame like Base's popup first-frame sync).
func lookupAnchorBounds(w *Window, name string) (anchorRecord, bool) {
	if ds, ok := windowDrawStates[w]; ok {
		if ds.frame != nil {
			if record, ok := ds.frame.anchorBounds[name]; ok {
				return record, true
			}
		}
		if record, ok := ds.lastAnchorBounds[name]; ok {
			return record, true
		}
	}
	return anchorRecord{}, false
}

// requestAnchorTracking turns this window's anchor recording on (the
// Anchored element's request layout calls it; see
// windowDrawState.trackAnchors).
func requestAnchorTracking(w *Window) {
	if ds, ok := windowDrawStates[w]; ok {
		ds.trackAnchors = true
	}
}

// windowTracksAnchors reports whether the window records named
// elements' bounds into the anchor registry (the div prepaint's gate).
func windowTracksAnchors(w *Window) bool {
	ds, ok := windowDrawStates[w]
	return ok && ds.trackAnchors
}

// recordInvokerPath records one named element's global id path into
// this frame's invoker registry (browser-core ticket 21): the TopLayer
// element's Invoker names the overlay's invoker by element id, and the
// light-dismiss walk resolves that name onto the invoker's element
// path — the identity the hit-test chains carry. Like the anchor
// registry, a later same-named element wins.
func recordInvokerPath(w *Window, name, path string) {
	f := currentFrame(w)
	if f.invokerPaths == nil {
		f.invokerPaths = make(map[string]string)
	}
	f.invokerPaths[name] = path
}

// lookupInvokerPath resolves one invoker name onto its element path
// (ticket 21): the in-flight frame's registry, then the last completed
// frame's (the invoker prepaints in the main tree before the deferred
// overlay content, so the in-flight registry is the usual hit).
func lookupInvokerPath(w *Window, name string) (string, bool) {
	if ds, ok := windowDrawStates[w]; ok {
		if ds.frame != nil {
			if path, ok := ds.frame.invokerPaths[name]; ok {
				return path, true
			}
		}
		if path, ok := ds.lastInvokerPaths[name]; ok {
			return path, true
		}
	}
	return "", false
}

// requestInvokerTracking turns this window's invoker recording on (the
// TopLayer element's request layout calls it; see
// windowDrawState.trackInvokers).
func requestInvokerTracking(w *Window) {
	if ds, ok := windowDrawStates[w]; ok {
		ds.trackInvokers = true
	}
}

// windowTracksInvokers reports whether the window records named
// elements' paths into the invoker registry (the div prepaint's gate).
func windowTracksInvokers(w *Window) bool {
	ds, ok := windowDrawStates[w]
	return ok && ds.trackInvokers
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

// WithRoundedContentMask is WithContentMask's R16 form (browser-core
// ticket 55): the clip mask carries the rounded-corner clip, and during
// the PAINT phase a rounded mask brackets f's subtree in a
// rounded_clip_only effect group whose composite clips to the rounded
// rectangle - see withRoundedClipScope. A nil mask or a zero-radii mask
// behaves exactly as WithContentMask (the plain rectangular
// intersection), so the style paths can hand Style.OverflowMask's
// result over wholesale; the frame's rectangular mask narrows as
// before in both phases, and a mask opened during PREPAINT brackets
// nothing (paintPhase) - the intersection alone.
func WithRoundedContentMask[R any](w *Window, mask *ClipMask, f func(*Window) R) R {
	frame := currentFrame(w)
	if mask == nil {
		return f(w)
	}
	intersected := intersectBounds(mask.Bounds, frame.contentMask)
	previous := frame.contentMask
	frame.contentMask = intersected
	frame.paint.Mask = intersected
	defer func() {
		frame.contentMask = previous
		frame.paint.Mask = previous
	}()
	if !mask.IsRounded() || !frame.paintPhase {
		return f(w)
	}
	return withRoundedClipScope(w, *mask, intersected, f)
}

// WithElementOpacity scopes an element opacity around f (window.rs
// with_element_opacity; the reference takes Option<f32> - nil is a
// no-op). D9 / rev 16 (browser-core ticket 55): with isolateGroup the
// opacity composites the element's subtree as ONE R16 effect group -
// the bracket the native plan isolates (or folds into a per-primitive
// colour multiply when the subtree draws exactly one primitive,
// Chromium's "at least two layers in subtree draw content" surface
// rule, cc/trees/property_tree_builder.cc:395-403) - and the paint
// context is NOT multiplied: the group's composite applies the opacity
// once, so overlapping children blend inside the group before the
// group blends with the page, the browser's paint model (the classic
// overlap artifact of the per-primitive multiply, report 01 §2). The
// fold makes the single-primitive case pixel-identical to the
// per-primitive multiply, so only the overlapping-subtree pixels
// change - the D9 divergence, pinned by the parity fixtures.
// isolateGroup false keeps the per-primitive multiply (the pin's
// with_element_opacity): the backdrop-filter exception (see
// opacityCompositesAsGroup) and any caller without the element bounds.
func WithElementOpacity[R any](w *Window, opacity *float32, bounds Bounds, isolateGroup bool, f func(*Window) R) R {
	frame := currentFrame(w)
	if opacity == nil || *opacity >= 1 {
		return f(w)
	}
	if !isolateGroup || !frame.paintPhase {
		previous := frame.paint
		frame.paint = frame.paint.WithElementOpacity(*opacity)
		defer func() { frame.paint = previous }()
		return f(w)
	}
	scale := frame.paint.ScaleFactor
	group := EffectGroup{
		HasOpacity:  true,
		Opacity:     *opacity,
		Bounds:      snapBounds(bounds, scale),
		ContentMask: snappedContentMask(frame.paint.Mask, scale),
	}
	if err := frame.scene.BeginEffectGroup(group); err != nil {
		panic(fmt.Sprintf("gpui: WithElementOpacity: effect group: %v", err))
	}
	defer func() {
		if err := frame.scene.EndEffectGroup(group); err != nil {
			panic(fmt.Sprintf("gpui: WithElementOpacity: effect group end: %v", err))
		}
	}()
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

// WithRoundedContentMaskVoid is the void form of
// WithRoundedContentMask.
func WithRoundedContentMaskVoid(w *Window, mask *ClipMask, f func(*Window)) {
	WithRoundedContentMask(w, mask, func(w *Window) struct{} {
		f(w)
		return struct{}{}
	})
}

// WithElementOpacityVoid is the void form of WithElementOpacity.
func WithElementOpacityVoid(w *Window, opacity *float32, bounds Bounds, isolateGroup bool, f func(*Window)) {
	WithElementOpacity(w, opacity, bounds, isolateGroup, func(w *Window) struct{} {
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
// scale and the current absolute element offset. It returns a value:
// the engine only reads the context, so callers pass its address and it
// stays on their stack (gpui-perf ticket 06).
func layoutContextOf(w *Window) LayoutContext {
	f := currentFrame(w)
	return LayoutContext{remSize: f.rem, scaleFactor: f.scale, elementOffset: f.elementOffset}
}

// requestLayoutOf requests one layout node with the given style and
// children on the window's engine (window.rs request_layout). Under the
// containing-block policy (browser-core 33) a STATIC style defers its
// absolute children to the nearest open positioned-ancestor scope (or
// keeps them attached when there is none, the pin's behaviour); every
// other style attaches its children directly, exactly like the pin.
// Container elements that walk their own children should use
// RequestContainerLayout instead, which also absorbs deferred
// absolutes into the node. A request reaching this seam while the
// frame's icbPending flag is still set discards the flag: its element
// is a non-participating ROOT (it requests children without opening a
// scope), so no root ICB scope exists this frame and the pin's
// direct-parent approximation keeps holding below it
// (containingblock.go's documented limitation, browser-core 76).
func requestLayoutOf(w *Window, style Style, children ...LayoutID) (LayoutID, error) {
	currentFrame(w).icbPending = false
	return requestLayoutNode(w, nil, style, children)
}

// requestMeasuredLayoutOf requests one measured leaf node (window.rs
// request_measured_layout).
func requestMeasuredLayoutOf(w *Window, style Style, measure MeasureFunc) (LayoutID, error) {
	ds := drawState(w)
	ctx := layoutContextOf(w)
	id, err := ds.engine.RequestMeasuredLayout(&ctx, style, measure)
	return recordAnchorPath(w, id, err)
}

// computeLayoutOf computes one subtree under the given available space
// (window.rs compute_layout).
func computeLayoutOf(w *Window, root LayoutID, available AvailableSize) error {
	ds := drawState(w)
	ctx := layoutContextOf(w)
	return ds.engine.ComputeLayout(&ctx, root, available)
}

// layoutBoundsOf queries one node's window-local bounds (window.rs
// layout_bounds).
func layoutBoundsOf(w *Window, id LayoutID) (Bounds, error) {
	ds := drawState(w)
	ctx := layoutContextOf(w)
	return ds.engine.LayoutBounds(&ctx, id)
}

// parentRelativeLayoutBoundsOf queries one node's parent-relative
// bounds (window.rs parent_relative_layout_bounds; text placement).
func parentRelativeLayoutBoundsOf(w *Window, id LayoutID) (Bounds, error) {
	ds := drawState(w)
	ctx := layoutContextOf(w)
	return ds.engine.ParentRelativeLayoutBounds(&ctx, id)
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
	record := styleToRecord(&stretched, &ctx)
	if err := ds.engine.native.SetStyle(id.node, &record); err != nil {
		return fmt.Errorf("gpui: StretchAutoSizeToFill: %w", err)
	}
	// The grid extras replace the stored style's axes and placements, so
	// the full style write above must re-apply them (browser-core 42).
	if err := applyGridStyleExtras(ds.engine, id.node, &stretched, &ctx); err != nil {
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
		ds.frame.accessedElementStateLog = append(ds.frame.accessedElementStateLog, key)
	}
}

// retainElementStates filters the window's retained element state to
// the keys accessed during the completed frame (window.rs
// Frame::finish's carry-over loop): state of elements that did not
// draw this frame is dropped, so removal followed by reinsertion
// starts fresh.
//
// Evicted states that hold framework resources (Window.UseKeyedState's
// entity lease) are released: the reference drops the previous frame's
// unaccessed ElementStateBox values when the frames swap (window.rs
// Frame::finish 1225-1231 then next_frame.clear() at 3237), which drops
// their Entity handles. The Go port has no drop, so eviction calls the
// state's release hook explicitly (sorted by key for determinism).
//
// It returns the evicted releasable states; render.go hands them to the
// frame-finish hook (helpers_frame.go finishFrame), which releases them
// when the frames swap.
func retainElementStates(ds *windowDrawState, accessed map[string]struct{}) []elementStateReleaser {
	retained := make(map[string]map[reflect.Type]any, len(accessed))
	var evictedKeys []string
	for key, byType := range ds.elementStates {
		if _, keep := accessed[key]; keep {
			retained[key] = byType
			continue
		}
		evictedKeys = append(evictedKeys, key)
	}
	sort.Strings(evictedKeys)
	var evicted []elementStateReleaser
	for _, key := range evictedKeys {
		evicted = append(evicted, releasersOf(ds.elementStates[key])...)
	}
	ds.elementStates = retained
	return evicted
}

// elementStateReleaser is implemented by retained element states that own
// framework resources released on eviction (keyedstate.go).
type elementStateReleaser interface {
	releaseElementState()
}

// releasersOf returns the releasable states of one key, ordered by type
// name for determinism.
func releasersOf(byType map[reflect.Type]any) []elementStateReleaser {
	types := make([]reflect.Type, 0, len(byType))
	for t := range byType {
		types = append(types, t)
	}
	sort.Slice(types, func(i, j int) bool { return types[i].String() < types[j].String() })
	var out []elementStateReleaser
	for _, t := range types {
		if r, ok := byType[t].(elementStateReleaser); ok {
			out = append(out, r)
		}
	}
	return out
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
// element authors call from Element.RequestLayout). See requestLayoutOf
// for the containing-block behaviour of a static style.
func RequestElementLayout(w *Window, style Style, children ...LayoutID) (LayoutID, error) {
	return requestLayoutOf(w, style, children...)
}

// RequestContainerLayout requests one layout node for a CONTAINER
// element whose children are requested inside walkChildren, resolving
// absolutely positioned descendants against the nearest positioned
// ancestor (browser-core ticket 33, spec D3; css-position-3 §2.1).
// Custom container elements should request their children through
// this seam so the CSS containing-block rule holds inside them:
// walkChildren runs inside this element's containing-block scope —
// opened only when the style is positioned (relative or absolute) —
// and the node request then attaches, besides walkChildren's own
// children, every absolute child that a static descendant deferred to
// this element. A static style opens no scope and defers this element's
// own absolute children to the enclosing positioned ancestor instead
// (attaching them to itself only when no positioned scope is open —
// the pin's behaviour). DivElement uses this path internally
// (div.go requestLayout); plain RequestElementLayout attaches children
// directly and never absorbs another element's deferred absolutes, so
// leaves and non-walking elements keep the pinned semantics.
func RequestContainerLayout(w *Window, style Style, walkChildren func(w *Window) []LayoutID) (LayoutID, error) {
	if walkChildren == nil {
		return LayoutID{}, fmt.Errorf("gpui: RequestContainerLayout requires a non-nil child walk")
	}
	scope := openContainingBlock(w, &style)
	defer closeContainingBlock(w, scope)
	children := walkChildren(w)
	return requestLayoutNode(w, scope, style, children)
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
