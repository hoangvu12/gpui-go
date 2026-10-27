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
	nodeElement     nodeKind = iota // a div (or text, its sugar)
	nodeText                        // a text run
	nodeFragment                    // children lower as their parent's children
	nodePlaceholder                 // a failed plugin's slot contribution
)

// Node is one element of the described tree: the shape App built
// during a load or re-describe, lowered to gpui elements once per
// frame.
type Node struct {
	kind nodeKind

	// Text is the nodeText's content — and the nodePlaceholder's
	// message (the plugin error the placeholder shows).
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

	// owner is the host whose JSX factory built this node: its runtime
	// owns the node's handlers, so events dispatch through it (a
	// plugin's nodes run in the plugin's VM even though they lower
	// with the app's tree). nil on Go-built nodes (no handlers).
	owner *Host
	// isTextElement records that the node was built from the host's
	// <text> tag (div sugar for a text run) rather than a <div>: the
	// lowering pins the text run's own display (lower.go).
	isTextElement bool
	// slotName is the slot element's name (which plugins fill it), and
	// slotFallback the JSX children it falls back to when none do.
	slotName     string
	slotFallback []*Node

	// OnClick and OnMouseDown are the (callable) event handlers.
	OnClick     goja.Callable
	OnMouseDown goja.Callable

	Children []*Node
}

// dispatchOwner is the host a node's handlers dispatch through: the
// host that built it, else the asking host.
func (n *Node) dispatchOwner(fallback *Host) *Host {
	if n.owner != nil {
		return n.owner
	}
	return fallback
}

// binding is a bound (computed) value: fn(state[key]) becomes the text
// of the node it was placed at; setting the state recomputes only the
// bindings on that key.
type binding struct {
	// bucket names the value's source: stateBucket (the host's keyed
	// state map) or sharedBucket (the app's shared store).
	bucket string
	key    string
	fn     goja.Callable
	node   *Node
}

// The binding buckets. A key is prefixed with its bucket in the host's
// dirty-key and binding indexes, so a state key and a shared key of
// the same name are two values.
const (
	stateBucket  = "state:"
	sharedBucket = "shared:"
)

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
	transform                                     []gpui.TransformFunction
	transformOrigin                               gpui.TransformOrigin
	fade                                          [4]float32
	blend                                         gpui.BlendMode
	isolate                                       bool
	clipPath, maskImage, dropShadow               string
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
	sTransform
	sTransformOrigin
	sOverflowFade
	sMixBlendMode
	sIsolation
	sClipPath
	sMaskImage
	sDropShadow
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
		case "transform":
			s.set |= sTransform
			fns, err := gpui.ParseTransformList(str)
			if err != nil {
				return style{}, fmt.Errorf("style transform: %v", err)
			}
			s.transform = fns
		case "transformOrigin":
			s.set |= sTransformOrigin
			origin, err := gpui.ParseTransformOrigin(str)
			if err != nil {
				return style{}, fmt.Errorf("style transformOrigin: %v", err)
			}
			s.transformOrigin = origin
		case "overflowFade":
			s.set |= sOverflowFade
			f := strings.Fields(str)
			switch len(f) {
			case 1:
				p := px(f[0])
				s.fade = [4]float32{p, p, p, p}
			case 2:
				y, x := px(f[0]), px(f[1])
				s.fade = [4]float32{y, x, y, x}
			case 4:
				s.fade = [4]float32{px(f[0]), px(f[1]), px(f[2]), px(f[3])}
			default:
				return style{}, fmt.Errorf("style overflowFade: %q is not supported (one, two or four lengths)", str)
			}
		case "mixBlendMode":
			s.set |= sMixBlendMode
			switch str {
			case "normal":
				s.blend = gpui.BlendNormal
			case "multiply":
				s.blend = gpui.BlendMultiply
			case "screen":
				s.blend = gpui.BlendScreen
			case "overlay":
				s.blend = gpui.BlendOverlay
			case "darken":
				s.blend = gpui.BlendDarken
			case "lighten":
				s.blend = gpui.BlendLighten
			case "color-dodge":
				s.blend = gpui.BlendColorDodge
			case "color-burn":
				s.blend = gpui.BlendColorBurn
			case "hard-light":
				s.blend = gpui.BlendHardLight
			case "soft-light":
				s.blend = gpui.BlendSoftLight
			case "difference":
				s.blend = gpui.BlendDifference
			case "exclusion":
				s.blend = gpui.BlendExclusion
			case "hue":
				s.blend = gpui.BlendHue
			case "saturation":
				s.blend = gpui.BlendSaturation
			case "color":
				s.blend = gpui.BlendColor
			case "luminosity":
				s.blend = gpui.BlendLuminosity
			case "plus-lighter":
				s.blend = gpui.BlendPlusLighter
			default:
				return style{}, fmt.Errorf("style mixBlendMode: %q is not supported (the W3C blend modes and plus-lighter)", str)
			}
		case "isolation":
			s.set |= sIsolation
			switch str {
			case "isolate":
				s.isolate = true
			case "auto":
				s.isolate = false
			default:
				return style{}, fmt.Errorf("style isolation: %q is not supported (use \"isolate\" or \"auto\")", str)
			}
		case "clipPath":
			s.set |= sClipPath
			if _, err := gpui.ParseClipPath(str); err != nil {
				return style{}, fmt.Errorf("style clipPath: %v", err)
			}
			s.clipPath = str
		case "maskImage":
			s.set |= sMaskImage
			if _, err := gpui.ParseMaskImage(str); err != nil {
				return style{}, fmt.Errorf("style maskImage: %v", err)
			}
			s.maskImage = str
		case "dropShadow":
			s.set |= sDropShadow
			if _, err := gpui.ParseDropShadowValues(str); err != nil {
				return style{}, fmt.Errorf("style dropShadow: %v", err)
			}
			s.dropShadow = str
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
	if s.set&sTransform != 0 {
		d = d.Transform(s.transform...)
	}
	if s.set&sTransformOrigin != 0 {
		d = d.TransformOrigin(s.transformOrigin.X, s.transformOrigin.Y)
	}
	if s.set&sOverflowFade != 0 {
		d = d.OverflowFade(gpui.AbsoluteLengthEdges{
			Top:    gpui.Px(s.fade[0]),
			Right:  gpui.Px(s.fade[1]),
			Bottom: gpui.Px(s.fade[2]),
			Left:   gpui.Px(s.fade[3]),
		})
	}
	if s.set&sMixBlendMode != 0 {
		d = d.MixBlendMode(s.blend)
	}
	if s.set&sIsolation != 0 && s.isolate {
		d = d.Isolate()
	}
	if s.set&sClipPath != 0 {
		clip, err := gpui.ParseClipPath(s.clipPath)
		if err != nil {
			panic(fmt.Sprintf("script: style clipPath: %v", err))
		}
		d = d.ClipPath(clip)
	}
	if s.set&sMaskImage != 0 {
		mask, err := gpui.ParseMaskImage(s.maskImage)
		if err != nil {
			panic(fmt.Sprintf("script: style maskImage: %v", err))
		}
		d = d.MaskImage(mask)
	}
	if s.set&sDropShadow != 0 {
		filters, err := gpui.ParseDropShadowValues(s.dropShadow)
		if err != nil {
			panic(fmt.Sprintf("script: style dropShadow: %v", err))
		}
		for _, f := range filters {
			d = d.DropShadow(f.OffsetX, f.OffsetY, f.BlurRadius, f.Color)
		}
	}
	return d
}
