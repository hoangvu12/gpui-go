package base

// An unstyled link (U:link.rs), built on the Button pattern
// (base/button.go).
//
// Display: upstream's base is a plain `div().id(id)` (U:link.rs:57), which
// under Base's crates.io gpui-ce 0.2.2 defaults to display Block; the
// core pin defaults to Flex (R:style.rs:1024-1027, map.md gap 2b). The
// constructor sets Block on the base before the caller's style refines
// over it, so an unstyled link lays out its children like upstream.

import "github.com/hoangvu12/gpui-go/gpui"

// LinkOpenHandler opens href on activation (U:link.rs:14 OpenHandler).
type LinkOpenHandler = func(href string, event *gpui.ClickEvent, w *gpui.Window, app *gpui.App)

// Link is an unstyled link that owns focus, activation and accessibility
// semantics (U:link.rs:16-37). href is target data, not an instruction to
// launch a browser: applications inject navigation through OpenWith, and
// href is not rendered as text (children provide the visible content).
type Link struct {
	gpui.Component[*Link]

	id                 gpui.ElementID
	base               *gpui.DivElement
	semanticStyles     LinkStyles
	href               *string
	disabled           bool
	onActivate         ClickHandler
	openWith           LinkOpenHandler
	accessibilityLabel *string
	tabIndex           int64
	tabStop            bool
}

// LinkStyles are the semantic root styles supported by Link
// (U:link.rs:39-51). The zero value is Rust's Default.
type LinkStyles struct {
	disabled gpui.StyleRefinement
}

// Disabled refines the root style when the link is disabled
// (U:link.rs:46-50).
func (s LinkStyles) Disabled(build func(*StateStyle) *StateStyle) LinkStyles {
	s.disabled = refineState(s.disabled, build)
	return s
}

// NewLink creates a link with the given element id (U:link.rs:54-70
// Link::new).
func NewLink[I gpui.IntoElementID](id I) *Link {
	eid := gpui.ElementIDOf(id)
	// Block: the 0.2.2 plain-div default (see the file comment).
	base := gpui.Div().ID(eid).Block()
	return gpui.BindComponent(&Link{
		id:       eid,
		base:     base,
		tabIndex: 0,
		tabStop:  true,
	}, base)
}

// Href sets the application-defined navigation target (U:link.rs:72-76).
func (l *Link) Href(href string) *Link {
	l.href = &href
	return l
}

// OpenWith injects the strategy used to open href on activation
// (U:link.rs:78-87). Base never calls App.OpenURL on its own.
func (l *Link) OpenWith(open LinkOpenHandler) *Link {
	l.openWith = open
	return l
}

// OnActivate observes pointer or keyboard activation after the open
// strategy runs (U:link.rs:89-96).
func (l *Link) OnActivate(handler ClickHandler) *Link {
	l.onActivate = handler
	return l
}

// Disabled sets whether pointer and keyboard activation are ignored
// (U:link.rs:98-102).
func (l *Link) Disabled(disabled bool) *Link {
	l.disabled = disabled
	return l
}

// Styles configures application-owned styles for the link's semantic
// states (U:link.rs:104-108).
func (l *Link) Styles(build func(LinkStyles) LinkStyles) *Link {
	l.semanticStyles = build(l.semanticStyles)
	return l
}

// resolvedStyle layers the instance style and disabled
// (U:link.rs:110-115).
func (l *Link) resolvedStyle() gpui.StyleRefinement {
	instance := l.Style()
	if l.disabled {
		return resolveStyle(&instance, &l.semanticStyles.disabled)
	}
	return resolveStyle(&instance)
}

// AccessibilityLabel sets the name exposed to accessibility clients
// (U:link.rs:117-121).
func (l *Link) AccessibilityLabel(label string) *Link {
	l.accessibilityLabel = &label
	return l
}

// TabIndex sets the focus traversal index (U:link.rs:123-127).
func (l *Link) TabIndex(tabIndex int64) *Link {
	l.tabIndex = tabIndex
	return l
}

// TabStop sets whether the link participates in keyboard focus traversal
// (U:link.rs:129-133).
func (l *Link) TabStop(tabStop bool) *Link {
	l.tabStop = tabStop
	return l
}

// RenderOnce implements gpui.RenderOnce (U:link.rs:163-203).
func (l *Link) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return l.render(w, app).IntoElement()
}

// render builds the link's root div (U:link.rs:164-202).
func (l *Link) render(w *gpui.Window, app *gpui.App) *gpui.DivElement {
	focusHandle := keyedFocusHandle(nil, l.id, w, app) // U:link.rs:135-140
	disabled := l.disabled
	style := l.resolvedStyle()
	href := l.href
	openWith := l.openWith
	onActivate := l.onActivate
	activates := onActivate != nil || href != nil && openWith != nil

	d := l.base.Role(gpui.RoleLink)
	if l.accessibilityLabel != nil {
		d.AriaLabel(*l.accessibilityLabel)
	}
	if !disabled {
		d.TrackFocus(focusHandle.TabIndex(l.tabIndex).TabStop(l.tabStop))
	}
	if disabled {
		d.OnMouseDown(gpui.MouseButtonLeft, stopPropagation)
	}
	if !disabled && activates {
		d.OnClick(func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			if href != nil && openWith != nil {
				openWith(*href, event, w, app)
			}
			if onActivate != nil {
				onActivate(event, w, app)
			}
		})
	}
	d.Extend(l.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}
