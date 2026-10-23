package base

// Focus traps (U:focus_trap.rs): a container element that registers
// itself with the application-wide FocusTrapManager while it lays out,
// so the Root's Tab handlers (root.go cycleFocus, U:root.rs:194-262) keep
// focus cycling inside it instead of escaping to the rest of the window.
//
// Go adaptations, recorded:
//
//   - FocusTrapElement is an extension trait over every InteractiveElement
//     (U:focus_trap.rs:14-50). Following the doc.go rule it is the wrapper
//     FocusTrapElementOf(element), which accepts a *gpui.DivElement or a
//     component embedding gpui.Interactive (or gpui.Component); any other
//     value panics.
//   - The pinned container calls its base element's phases directly with
//     its own global id (U:focus_trap.rs:176-221), so the base's own id is
//     replaced by the trap id. The port's phases are reached through
//     AnyElement, which pushes the base's id when it has one: the base
//     keeps its id one level below the trap id (dialog-0/dialog-host-0
//     rather than dialog-0). Debug selectors and keyed state below the
//     host are unaffected.
//   - The pinned manager is a HashMap iterated in arbitrary order
//     (U:focus_trap.rs:53-56, 84-95); the port keeps registration order so
//     the active trap of nested traps is deterministic (the first
//     registered trap containing focus, which is the outermost one drawn).
//   - FocusTrapManager::global panics when init has not run
//     (U:focus_trap.rs:68-70). The lookup here reports no trap instead and
//     registration creates the manager on demand, because Init wiring
//     (lib.go) is not owned by this ticket; Init still calls
//     focusTrapInit once it is wired.

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
)

// focusTrapInit installs the focus trap manager global
// (U:focus_trap.rs:8-11 init).
func focusTrapInit(app *gpui.App) {
	app.SetGlobal(newFocusTrapManager())
}

// focusTrapManager is the global state of every focus trap container
// (U:focus_trap.rs:52-56 FocusTrapManager): the container element's
// global id mapped to its weak focus handle, in registration order. The
// global is stored as *focusTrapManager.
type focusTrapManager struct {
	order []string
	traps map[string]gpui.WeakFocusHandle
}

// newFocusTrapManager creates an empty manager (U:focus_trap.rs:61-66).
func newFocusTrapManager() *focusTrapManager {
	return &focusTrapManager{traps: make(map[string]gpui.WeakFocusHandle)}
}

// focusTrapManagerGlobalMut returns the manager, creating it when Init
// has not installed it (see the file comment; U:focus_trap.rs:72-74).
func focusTrapManagerGlobalMut(app *gpui.App) *focusTrapManager {
	if !gpui.HasGlobal[*focusTrapManager](app) {
		focusTrapInit(app)
	}
	return gpui.Global[*focusTrapManager](app)
}

// focusTrapRegister registers a focus trap container and drops the traps
// whose handles were released (U:focus_trap.rs:76-81 register_trap).
func focusTrapRegister(id *gpui.GlobalElementID, container gpui.WeakFocusHandle, app *gpui.App) {
	m := focusTrapManagerGlobalMut(app)
	key := id.String()
	if _, ok := m.traps[key]; !ok {
		m.order = append(m.order, key)
	}
	m.traps[key] = container
	m.cleanup()
}

// cleanup drops traps with released handles (U:focus_trap.rs:97-100).
func (m *focusTrapManager) cleanup() {
	live := m.order[:0]
	for _, key := range m.order {
		if _, ok := m.traps[key].Upgrade(); ok {
			live = append(live, key)
		} else {
			delete(m.traps, key)
		}
	}
	m.order = live
}

// findActiveTrap finds the trap containing the focused element
// (U:focus_trap.rs:83-95 find_active_trap).
func (m *focusTrapManager) findActiveTrap(w *gpui.Window) (gpui.FocusHandle, bool) {
	for _, key := range m.order {
		container, ok := m.traps[key].Upgrade()
		if !ok {
			continue
		}
		if container.ContainsFocused(w) {
			return container, true
		}
	}
	return gpui.FocusHandle{}, false
}

// ActiveFocusTrap returns the focus handle of the active focus trap, if
// any (U:focus_trap.rs:224-228 active_focus_trap, doc(hidden) upstream).
func ActiveFocusTrap(w *gpui.Window, app *gpui.App) (gpui.FocusHandle, bool) {
	m, ok := gpui.TryGlobal[*focusTrapManager](app)
	if !ok {
		return gpui.FocusHandle{}, false
	}
	return m.findActiveTrap(w)
}

// FocusTrapElement is the FocusTrapElement extension trait
// (U:focus_trap.rs:13-50) over one interactive element.
type FocusTrapElement[S any] struct {
	element S
	div     *gpui.DivElement
}

// FocusTrapElementOf wraps an interactive element (a *gpui.DivElement or
// a component embedding gpui.Interactive) for the FocusTrapElement
// methods.
func FocusTrapElementOf[S any](element S) FocusTrapElement[S] {
	switch e := any(element).(type) {
	case *gpui.DivElement:
		return FocusTrapElement[S]{element: element, div: e}
	case interface{ Interactivity() *gpui.DivElement }:
		return FocusTrapElement[S]{element: element, div: e.Interactivity()}
	}
	panic(fmt.Sprintf("base: FocusTrapElementOf requires a *gpui.DivElement or a gpui.Interactive component, got %T", element))
}

// FocusTrap enables a focus trap for this element (U:focus_trap.rs:15-48
// focus_trap): Tab and Shift-Tab, intercepted by the Root, cycle focus
// inside the container instead of escaping to elements outside it. The
// element tracks focusHandle.
//
//	FocusTrapElementOf(VFlex().Child(a).Child(b).Child(c)).
//		FocusTrap("trap1", containerFocus)
//	// Tab cycles a -> b -> c -> a.
func (e FocusTrapElement[S]) FocusTrap[I gpui.IntoElementID](id I, focusHandle gpui.FocusHandle) *FocusTrapContainer[S] {
	return newFocusTrapContainer(gpui.ElementIDOf(id), focusHandle, e.element, e.div)
}

// FocusTrapContainer wraps an element and registers it as a focus trap
// container (U:focus_trap.rs:109-117). Its Styled, Interactive and Parent
// methods delegate to the wrapped element (U:focus_trap.rs:138-160).
type FocusTrapContainer[S any] struct {
	gpui.Styled[*FocusTrapContainer[S]]
	gpui.Interactive[*FocusTrapContainer[S]]

	id          gpui.ElementID
	focusHandle gpui.FocusHandle
	base        S
	extend      func([]gpui.AnyElement)
}

// newFocusTrapContainer builds the container, tracking focusHandle on the
// child (U:focus_trap.rs:119-127 FocusTrapContainer::new).
func newFocusTrapContainer[S any](id gpui.ElementID, focusHandle gpui.FocusHandle, child S, div *gpui.DivElement) *FocusTrapContainer[S] {
	// track_focus is the child's own: components shadowing it (Button,
	// Toast) keep their override, like the pinned trait dispatch.
	if tracker, ok := any(child).(interface{ TrackFocus(gpui.FocusHandle) S }); ok {
		child = tracker.TrackFocus(focusHandle)
	} else {
		div.TrackFocus(focusHandle)
	}
	c := &FocusTrapContainer[S]{id: id, focusHandle: focusHandle, base: child}
	switch e := any(child).(type) {
	case *gpui.DivElement:
		c.extend = func(elements []gpui.AnyElement) { e.Extend(elements) }
	case interface{ Extend([]gpui.AnyElement) }:
		c.extend = e.Extend
	default:
		c.extend = func([]gpui.AnyElement) {
			panic(fmt.Sprintf("base: FocusTrapContainer: %T is not a parent element", child))
		}
	}
	storage := div.StyleMut()
	if styled, ok := any(child).(interface{ StyleMut() *gpui.StyleRefinement }); ok {
		storage = styled.StyleMut()
	}
	gpui.BindStyledTo(c, storage)
	gpui.BindInteractive(c, div)
	return c
}

// Base returns the wrapped element.
func (c *FocusTrapContainer[S]) Base() S { return c.base }

// Extend appends children to the wrapped element
// (U:focus_trap.rs:138-144).
func (c *FocusTrapContainer[S]) Extend(elements []gpui.AnyElement) { c.extend(elements) }

// Child adds one child to the wrapped element (ParentElement::child).
func (c *FocusTrapContainer[S]) Child(value any) *FocusTrapContainer[S] {
	c.extend([]gpui.AnyElement{overlayIntoElement("FocusTrapContainer", value)})
	return c
}

// Children adds the children in order (ParentElement::children).
func (c *FocusTrapContainer[S]) Children(values ...any) *FocusTrapContainer[S] {
	for _, value := range values {
		c.Child(value)
	}
	return c
}

// IntoElement implements gpui.IntoElement (U:focus_trap.rs:129-137,
// 162-222): the container element with the trap id.
func (c *FocusTrapContainer[S]) IntoElement() gpui.AnyElement {
	return gpui.CustomElement[struct{}, struct{}](&focusTrapDrawable{
		id:          c.id,
		focusHandle: c.focusHandle,
		inner:       overlayIntoElement("FocusTrapContainer", c.base),
	})
}

// focusTrapDrawable adapts the container into the Element phases.
type focusTrapDrawable struct {
	id          gpui.ElementID
	focusHandle gpui.FocusHandle
	inner       gpui.AnyElement
}

// ID implements gpui.Element (U:focus_trap.rs:168-170).
func (d *focusTrapDrawable) ID() (gpui.ElementID, bool) { return d.id, true }

// SourceLocation implements gpui.Element (U:focus_trap.rs:172-174).
func (d *focusTrapDrawable) SourceLocation() *gpui.SourceLocation { return nil }

// RequestLayout registers the trap with the manager, then lays out the
// wrapped element (U:focus_trap.rs:176-187).
func (d *focusTrapDrawable) RequestLayout(global *gpui.GlobalElementID, _ *gpui.InspectorElementID, w *gpui.Window, app *gpui.App) (gpui.LayoutID, struct{}) {
	focusTrapRegister(global, d.focusHandle.Downgrade(), app)
	return d.inner.RequestLayout(w, app), struct{}{}
}

// Prepaint prepaints the wrapped element (U:focus_trap.rs:189-200).
func (d *focusTrapDrawable) Prepaint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *struct{}, w *gpui.Window, app *gpui.App) struct{} {
	d.inner.Prepaint(w, app)
	return struct{}{}
}

// Paint paints the wrapped element (U:focus_trap.rs:202-221).
func (d *focusTrapDrawable) Paint(_ *gpui.GlobalElementID, _ *gpui.InspectorElementID, _ gpui.Bounds, _ *struct{}, _ *struct{}, w *gpui.Window, app *gpui.App) {
	d.inner.Paint(w, app)
}

// overlayIntoElement converts one child-position value (an element,
// IntoElement, string, view or recipe) into an AnyElement, panicking
// with the owner's name on invalid input (the `impl IntoElement`
// parameters of the overlay builders).
func overlayIntoElement(owner string, value any) gpui.AnyElement {
	element, err := gpui.TryIntoElement(value)
	if err != nil {
		panic(fmt.Sprintf("base: %s: %v", owner, err))
	}
	return element
}
