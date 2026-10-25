package script

import (
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Element renders the described tree: the whole point of the host's
// model is that this walks a Go-side tree once per frame and never runs
// the script. While an error is held (a failed load or a handler that
// threw) the last good tree is kept and an error banner is mounted
// over the bottom of the window (without an error there is no wrapper
// and no banner: no extra nodes, no per-frame work).
func (h *Host) Element() gpui.AnyElement {
	root := h.root
	if root == nil {
		root = &Node{}
	}
	if h.err == nil || !h.banner {
		return h.lower(root).IntoElement()
	}
	return gpui.Div().Flex().
		SizeFull().
		Child(h.lower(root).IntoElement()).
		Child(h.errorBanner()).
		IntoElement()
}

// lower builds one element of the tree (fresh each frame, like any
// Render; the tree itself is only replaced by a load or re-describe).
// A node's handlers dispatch through the host that built it, so a
// plugin's nodes run in the plugin's runtime even though they lower
// with the app's tree.
func (h *Host) lower(n *Node) *gpui.DivElement {
	// A TSX <div> follows the browser default display — block since
	// browser-core 71 (spec D6), like an HTML div: children stack, an
	// auto width fills the containing block, and text children form a
	// paragraph. A node that relied on the old flex default now states
	// it through its className or style, exactly like HTML.
	//
	// The <text> tag is NOT a div — it is the host's sugar for one
	// styled text run — so it keeps the pin's shape: a flex row whose
	// single text child is a flex item (the cheap measured-leaf path).
	// A block paragraph per text run would lay every run out through
	// the inline paragraph machinery (its line queries re-run per
	// frame), which the sidebar-shaped trees — 200+ styled runs —
	// measured at ~100ms per frame; runs that must flow as inline
	// content go inside their block parent as bare text children
	// (the tsx fixtures' shape), which is the HTML spelling anyway.
	d := gpui.Div()
	if n.isTextElement {
		d = d.Flex()
	}
	if n.ID != "" {
		d = d.ID(n.ID)
	}
	if n.selector != "" {
		d = d.DebugSelector(n.selector)
	}
	if n.Class != "" {
		d = d.Class(n.Class)
	}
	d = n.Style.apply(d)
	owner := n.dispatchOwner(h)
	if n.OnClick != nil {
		fn := n.OnClick
		d = d.OnClick(func(e *gpui.ClickEvent, _ *gpui.Window, _ *gpui.App) {
			up := e.Mouse.Up
			owner.dispatch(fn, owner.eventObject(domButton(up.Button), up.Position.X, up.Position.Y, up.Modifiers))
		})
	}
	if n.OnMouseDown != nil {
		fn := n.OnMouseDown
		d = d.OnMouseDown(gpui.MouseButtonLeft, func(e *gpui.MouseDownEvent, _ *gpui.Window, _ *gpui.App) {
			owner.dispatch(fn, owner.eventObject(domButton(e.Button), e.Position.X, e.Position.Y, e.Modifiers))
		})
	}
	h.appendChildren(d, n.Children)
	return d
}

// appendChildren lowers children into d; a fragment's children lower
// as their parent's direct children (spliced, no wrapper), and a
// placeholder (a failed plugin's contribution) lowers as the slot's
// in-flow error box.
func (h *Host) appendChildren(d *gpui.DivElement, nodes []*Node) {
	for _, c := range nodes {
		switch c.kind {
		case nodeText:
			d = d.Child(c.Text)
		case nodeFragment:
			h.appendChildren(d, c.Children)
		case nodePlaceholder:
			d = d.Child(placeholderElement(c.Text))
		default:
			d = d.Child(h.lower(c))
		}
	}
}

// errorBanner mounts the load/handler error over the bottom of the
// window: the message with its .tsx file and line, monospace, one
// text line per line of the error (zgui's banner model).
func (h *Host) errorBanner() *gpui.DivElement {
	b := gpui.Div().Flex().
		DebugSelector(ErrorBannerSelector).
		Absolute().
		Left(gpui.Px(0)).
		Right(gpui.Px(0)).
		Bottom(gpui.Px(0)).
		Bg(gpui.Rgb(0x7f1d1d)).
		TextColor(gpui.Rgb(0xfecaca)).
		TextSize(gpui.Px(12)).
		FontFamily("Consolas").
		P(gpui.Px(8))
	for _, line := range strings.Split(h.err.Error(), "\n") {
		b = b.Child(line)
	}
	return b
}

// domButton maps gpui's MouseButton onto the DOM's button numbers
// (0 left, 1 middle, 2 right).
func domButton(b gpui.MouseButton) int {
	switch b {
	case gpui.MouseButtonLeft:
		return 0
	case gpui.MouseButtonMiddle:
		return 1
	case gpui.MouseButtonRight:
		return 2
	}
	return 0
}
