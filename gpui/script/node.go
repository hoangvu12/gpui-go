package script

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dop251/goja"
	"github.com/hoangvu12/gpui-go/gpui"
)

// nodeKind distinguishes described nodes.
type nodeKind uint8

const (
	nodeElement  nodeKind = iota // a div (or text, its sugar)
	nodeText                     // a text run
	nodeFragment                 // children lower as their parent's children
)

// Node is one element of the described tree: the shape App built
// during a load or re-describe, lowered to gpui elements once per
// frame.
type Node struct {
	kind nodeKind

	// Text is the nodeText's content.
	Text string

	// ID is the element's identity: the id prop (which is also the
	// debug selector), else the key prop, else an auto id for
	// interactive elements.
	ID string
	// selector is the bounds key (set only by the id prop).
	selector string
	// Class is the className prop.
	Class string
	// Style is the parsed style object.
	Style style

	// OnClick and OnMouseDown are the (callable) event handlers.
	OnClick     goja.Callable
	OnMouseDown goja.Callable

	Children []*Node
}

// binding is a bound (computed) value: fn(state[key]) becomes the text
// of the node it was placed at; setting the state recomputes only the
// bindings on that key.
type binding struct {
	key  string
	fn   goja.Callable
	node *Node
}

// style is the parsed CSS subset of a style object. Each set field
// maps to one fluent call in apply.
type style struct {
	set                                           uint32
	display, dir, align, justify                  int8
	flex                                          float32
	gap, w, h, minH                               float32
	wFull, hFull, overflowY                       bool
	pad                                           [4]float32
	bg, color, borderColor                        gpui.Hsla
	radius, fontSize, fontWeight, opacity, border float32
}

// style bits, one per supported CSS property.
const (
	sDisplay = 1 << iota
	sDir
	sAlign
	sJustify
	sFlex
	sGap
	sW
	sH
	sMinH
	sPad
	sBg
	sColor
	sBorderColor
	sBorder
	sRadius
	sFontSize
	sFontWeight
	sOpacity
	sOverflowY
)

// overflowYScroll reports whether overflowY asked for scrolling
// (the element needs an id).
func (s *style) overflowYScroll() bool {
	return s.set&sOverflowY != 0 && s.overflowY
}

// parseStyle reads a style object (camelCase CSS names) into the
// supported subset; anything else is an error naming the property.
func parseStyle(o *goja.Object) (style, error) {
	var s style
	for _, k := range o.Keys() {
		v := o.Get(k)
		str := v.String()
		switch k {
		case "display":
			s.set |= sDisplay
			switch str {
			case "flex":
				s.display = 1
			case "block":
				s.display = 2
			default:
				return style{}, fmt.Errorf("style display: %q is not supported (use \"flex\" or \"block\")", str)
			}
		case "flexDirection":
			s.set |= sDir
			switch str {
			case "row":
				s.dir = 1
			case "column":
				s.dir = 2
			default:
				return style{}, fmt.Errorf("style flexDirection: %q is not supported (use \"row\" or \"column\")", str)
			}
		case "alignItems":
			s.set |= sAlign
			switch str {
			case "center":
				s.align = 1
			case "flex-start", "start":
				s.align = 2
			case "flex-end", "end":
				s.align = 3
			default:
				return style{}, fmt.Errorf("style alignItems: %q is not supported (use start, center or end)", str)
			}
		case "justifyContent":
			s.set |= sJustify
			switch str {
			case "center":
				s.justify = 1
			case "flex-start", "start":
				s.justify = 2
			case "flex-end", "end":
				s.justify = 3
			case "space-between":
				s.justify = 4
			default:
				return style{}, fmt.Errorf("style justifyContent: %q is not supported (use start, center, end or space-between)", str)
			}
		case "flex":
			s.set |= sFlex
			s.flex = float32(v.ToFloat())
			if s.flex != 1 {
				return style{}, fmt.Errorf("style flex: %g is not supported (use flex: 1)", s.flex)
			}
		case "gap":
			s.set |= sGap
			s.gap = px(str)
		case "width":
			s.set |= sW
			if str == "100%" {
				s.wFull = true
			} else if s.w = px(str); s.w == 0 && str != "0" && str != "0px" {
				return style{}, fmt.Errorf("style width: %q is not supported (a px length or \"100%%\")", str)
			}
		case "height":
			s.set |= sH
			if str == "100%" {
				s.hFull = true
			} else if s.h = px(str); s.h == 0 && str != "0" && str != "0px" {
				return style{}, fmt.Errorf("style height: %q is not supported (a px length or \"100%%\")", str)
			}
		case "minHeight":
			s.set |= sMinH
			s.minH = px(str)
		case "padding":
			s.set |= sPad
			f := strings.Fields(str)
			switch len(f) {
			case 1:
				p := px(f[0])
				s.pad = [4]float32{p, p, p, p}
			case 2:
				y, x := px(f[0]), px(f[1])
				s.pad = [4]float32{y, x, y, x}
			case 4:
				s.pad = [4]float32{px(f[0]), px(f[1]), px(f[2]), px(f[3])}
			default:
				return style{}, fmt.Errorf("style padding: %q is not supported (one, two or four lengths)", str)
			}
		case "background":
			s.set |= sBg
			c, err := gpui.ParseHex(str)
			if err != nil {
				return style{}, fmt.Errorf("style background: %v", err)
			}
			s.bg = c
		case "color":
			s.set |= sColor
			c, err := gpui.ParseHex(str)
			if err != nil {
				return style{}, fmt.Errorf("style color: %v", err)
			}
			s.color = c
		case "borderColor":
			s.set |= sBorderColor
			c, err := gpui.ParseHex(str)
			if err != nil {
				return style{}, fmt.Errorf("style borderColor: %v", err)
			}
			s.borderColor = c
		case "border":
			s.set |= sBorder
			s.border = px(str)
		case "borderRadius":
			s.set |= sRadius
			s.radius = px(str)
		case "fontSize":
			s.set |= sFontSize
			s.fontSize = px(str)
		case "fontWeight":
			s.set |= sFontWeight
			s.fontWeight = float32(v.ToFloat())
		case "opacity":
			s.set |= sOpacity
			s.opacity = float32(v.ToFloat())
		case "overflowY":
			s.set |= sOverflowY
			switch str {
			case "scroll", "auto":
				s.overflowY = true
			case "hidden":
				s.overflowY = false
			default:
				return style{}, fmt.Errorf("style overflowY: %q is not supported (use \"scroll\", \"auto\" or \"hidden\")", str)
			}
		default:
			return style{}, fmt.Errorf("style: %q is not a supported CSS property here", k)
		}
	}
	return s, nil
}

// px reads a length value: a number ("8", from 8) or "8px".
func px(s string) float32 {
	f, _ := strconv.ParseFloat(strings.TrimSuffix(s, "px"), 32)
	return float32(f)
}

// apply lowers the parsed style onto a DivElement with the fluent API
// a Go view would use.
func (s *style) apply(d *gpui.DivElement) *gpui.DivElement {
	if s.set == 0 {
		return d
	}
	if s.set&sDisplay != 0 {
		if s.display == 1 {
			d = d.Flex()
		} else {
			d = d.Block()
		}
	}
	if s.set&sDir != 0 {
		if s.dir == 2 {
			d = d.FlexCol()
		} else {
			d = d.FlexRow()
		}
	}
	if s.set&sAlign != 0 {
		switch s.align {
		case 1:
			d = d.ItemsCenter()
		case 2:
			d = d.ItemsStart()
		default:
			d = d.ItemsEnd()
		}
	}
	if s.set&sJustify != 0 {
		switch s.justify {
		case 1:
			d = d.JustifyCenter()
		case 2:
			d = d.JustifyStart()
		case 3:
			d = d.JustifyEnd()
		default:
			d = d.JustifyBetween()
		}
	}
	if s.set&sFlex != 0 {
		d = d.Flex1()
	}
	if s.set&sGap != 0 {
		d = d.Gap(gpui.Px(s.gap))
	}
	if s.set&sW != 0 {
		if s.wFull {
			d = d.WFull()
		} else {
			d = d.W(gpui.Px(s.w))
		}
	}
	if s.set&sH != 0 {
		if s.hFull {
			d = d.HFull()
		} else {
			d = d.H(gpui.Px(s.h))
		}
	}
	if s.set&sMinH != 0 {
		d = d.MinH(gpui.Px(s.minH))
	}
	if s.set&sPad != 0 {
		d = d.Pt(gpui.Px(s.pad[0])).Pr(gpui.Px(s.pad[1])).Pb(gpui.Px(s.pad[2])).Pl(gpui.Px(s.pad[3]))
	}
	if s.set&sBg != 0 {
		d = d.Bg(s.bg)
	}
	if s.set&sColor != 0 {
		d = d.TextColor(s.color)
	}
	if s.set&sBorderColor != 0 {
		d = d.BorderColor(s.borderColor)
	}
	if s.set&sBorder != 0 {
		d = d.Border(gpui.Px(s.border))
	}
	if s.set&sRadius != 0 {
		d = d.Rounded(gpui.Px(s.radius))
	}
	if s.set&sFontSize != 0 {
		d = d.TextSize(gpui.Px(s.fontSize))
	}
	if s.set&sFontWeight != 0 {
		d = d.FontWeight(s.fontWeight)
	}
	if s.set&sOpacity != 0 {
		d = d.Opacity(s.opacity)
	}
	if s.set&sOverflowY != 0 && s.overflowY {
		d = d.OverflowYScroll()
	}
	return d
}
