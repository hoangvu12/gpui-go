// Command stylegen emits gpui/styled_generated.go: the Styled methods the
// pinned gpui_macros/src/styles.rs expands (ticket base-05; reference
// crates/gpui_macros/src/styles.rs at
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a). The tables below mirror the
// macro's prefix and suffix tables line for line; the emitted methods are
// methods of gpui.Styled[Self]. Each is a generic method (at least the
// `[_ lazy]` marker), so a consumer package compiles only the methods it
// calls (see lazy in gpui/styled.go).
//
// It is a maintainer tool: the generated file is committed, and
// consumers never run it. Regenerate from the gpui directory with
// `go generate` (styled.go carries the directive) or directly:
//
//	go run ./cmd/stylegen -out gpui/styled_generated.go
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"sort"
	"strings"
)

// field is one refinement field written by a method, with the kind of
// value it stores.
type field struct {
	name string // StyleRefinement field
	kind valueKind
}

type valueKind int

const (
	kindLength   valueKind = iota // Optional[Length]
	kindDefinite                  // Optional[DefiniteLength]
	kindAbsolute                  // Optional[AbsoluteLength]
)

// boxPrefix mirrors styles.rs BoxStylePrefix.
type boxPrefix struct {
	prefix      string
	autoAllowed bool
	fields      []string
	doc         string
	line        int
}

// boxSuffix mirrors styles.rs BoxStyleSuffix: the length is a gpui
// constructor applied to a constant.
type boxSuffix struct {
	suffix string
	unit   string // "px", "rems", "relative", "auto"
	value  string // Go constant expression
	doc    string
}

// marginPrefixes is styles.rs:1189-1239 margin_box_style_prefixes.
var marginPrefixes = []boxPrefix{
	{"m", true, []string{"MarginTop", "MarginBottom", "MarginLeft", "MarginRight"}, "Sets the margin of the element. [Docs](https://tailwindcss.com/docs/margin)", 1192},
	{"mt", true, []string{"MarginTop"}, "Sets the top margin of the element. [Docs](https://tailwindcss.com/docs/margin#add-margin-to-a-single-side)", 1203},
	{"mb", true, []string{"MarginBottom"}, "Sets the bottom margin of the element. [Docs](https://tailwindcss.com/docs/margin#add-margin-to-a-single-side)", 1209},
	{"my", true, []string{"MarginTop", "MarginBottom"}, "Sets the vertical margin of the element. [Docs](https://tailwindcss.com/docs/margin#add-vertical-margin)", 1215},
	{"mx", true, []string{"MarginLeft", "MarginRight"}, "Sets the horizontal margin of the element. [Docs](https://tailwindcss.com/docs/margin#add-horizontal-margin)", 1221},
	{"ml", true, []string{"MarginLeft"}, "Sets the left margin of the element. [Docs](https://tailwindcss.com/docs/margin#add-margin-to-a-single-side)", 1227},
	{"mr", true, []string{"MarginRight"}, "Sets the right margin of the element. [Docs](https://tailwindcss.com/docs/margin#add-margin-to-a-single-side)", 1233},
}

// paddingPrefixes is styles.rs:1241-1291 padding_box_style_prefixes.
var paddingPrefixes = []boxPrefix{
	{"p", false, []string{"PaddingTop", "PaddingBottom", "PaddingLeft", "PaddingRight"}, "Sets the padding of the element. [Docs](https://tailwindcss.com/docs/padding)", 1244},
	{"pt", false, []string{"PaddingTop"}, "Sets the top padding of the element. [Docs](https://tailwindcss.com/docs/padding#add-padding-to-a-single-side)", 1255},
	{"pb", false, []string{"PaddingBottom"}, "Sets the bottom padding of the element. [Docs](https://tailwindcss.com/docs/padding#add-padding-to-a-single-side)", 1261},
	{"px", false, []string{"PaddingLeft", "PaddingRight"}, "Sets the horizontal padding of the element. [Docs](https://tailwindcss.com/docs/padding#add-horizontal-padding)", 1267},
	{"py", false, []string{"PaddingTop", "PaddingBottom"}, "Sets the vertical padding of the element. [Docs](https://tailwindcss.com/docs/padding#add-vertical-padding)", 1273},
	{"pl", false, []string{"PaddingLeft"}, "Sets the left padding of the element. [Docs](https://tailwindcss.com/docs/padding#add-padding-to-a-single-side)", 1279},
	{"pr", false, []string{"PaddingRight"}, "Sets the right padding of the element. [Docs](https://tailwindcss.com/docs/padding#add-padding-to-a-single-side)", 1285},
}

// positionPrefixes is styles.rs:1293-1331 position_box_style_prefixes.
var positionPrefixes = []boxPrefix{
	{"inset", true, []string{"InsetTop", "InsetRight", "InsetBottom", "InsetLeft"}, "Sets the top, right, bottom, and left values of a positioned element. [Docs](https://tailwindcss.com/docs/top-right-bottom-left)", 1296},
	{"top", true, []string{"InsetTop"}, "Sets the top value of a positioned element. [Docs](https://tailwindcss.com/docs/top-right-bottom-left)", 1307},
	{"bottom", true, []string{"InsetBottom"}, "Sets the bottom value of a positioned element. [Docs](https://tailwindcss.com/docs/top-right-bottom-left)", 1313},
	{"left", true, []string{"InsetLeft"}, "Sets the left value of a positioned element. [Docs](https://tailwindcss.com/docs/top-right-bottom-left)", 1319},
	{"right", true, []string{"InsetRight"}, "Sets the right value of a positioned element. [Docs](https://tailwindcss.com/docs/top-right-bottom-left)", 1325},
}

// sizePrefixes is styles.rs:1333-1414 box_prefixes (style_helpers!).
var sizePrefixes = []boxPrefix{
	{"w", true, []string{"SizeWidth"}, "Sets the width of the element. [Docs](https://tailwindcss.com/docs/width)", 1336},
	{"h", true, []string{"SizeHeight"}, "Sets the height of the element. [Docs](https://tailwindcss.com/docs/height)", 1342},
	{"size", true, []string{"SizeWidth", "SizeHeight"}, "Sets the width and height of the element.", 1348},
	{"min_size", true, []string{"MinSizeWidth", "MinSizeHeight"}, "Sets the minimum width and height of the element.", 1354},
	{"min_w", true, []string{"MinSizeWidth"}, "Sets the minimum width of the element. [Docs](https://tailwindcss.com/docs/min-width)", 1360},
	{"min_h", true, []string{"MinSizeHeight"}, "Sets the minimum height of the element. [Docs](https://tailwindcss.com/docs/min-height)", 1368},
	{"max_size", true, []string{"MaxSizeWidth", "MaxSizeHeight"}, "Sets the maximum width and height of the element.", 1374},
	{"max_w", true, []string{"MaxSizeWidth"}, "Sets the maximum width of the element. [Docs](https://tailwindcss.com/docs/max-width)", 1382},
	{"max_h", true, []string{"MaxSizeHeight"}, "Sets the maximum height of the element. [Docs](https://tailwindcss.com/docs/max-height)", 1390},
	{"gap", false, []string{"GapWidth", "GapHeight"}, "Sets the gap between rows and columns in flex layouts. [Docs](https://tailwindcss.com/docs/gap)", 1396},
	{"gap_x", false, []string{"GapWidth"}, "Sets the gap between columns in flex layouts. [Docs](https://tailwindcss.com/docs/gap#changing-row-and-column-gaps-independently)", 1402},
	{"gap_y", false, []string{"GapHeight"}, "Sets the gap between rows in flex layouts. [Docs](https://tailwindcss.com/docs/gap#changing-row-and-column-gaps-independently)", 1408},
}

// boxSuffixes is styles.rs:1416-1649 box_style_suffixes.
var boxSuffixes = []boxSuffix{
	{"0", "px", "0", "0px"},
	{"0p5", "rems", "0.125", "2px (0.125rem)"},
	{"1", "rems", "0.25", "4px (0.25rem)"},
	{"1p5", "rems", "0.375", "6px (0.375rem)"},
	{"2", "rems", "0.5", "8px (0.5rem)"},
	{"2p5", "rems", "0.625", "10px (0.625rem)"},
	{"3", "rems", "0.75", "12px (0.75rem)"},
	{"3p5", "rems", "0.875", "14px (0.875rem)"},
	{"4", "rems", "1", "16px (1rem)"},
	{"5", "rems", "1.25", "20px (1.25rem)"},
	{"6", "rems", "1.5", "24px (1.5rem)"},
	{"7", "rems", "1.75", "28px (1.75rem)"},
	{"8", "rems", "2.0", "32px (2rem)"},
	{"9", "rems", "2.25", "36px (2.25rem)"},
	{"10", "rems", "2.5", "40px (2.5rem)"},
	{"11", "rems", "2.75", "44px (2.75rem)"},
	{"12", "rems", "3", "48px (3rem)"},
	{"16", "rems", "4", "64px (4rem)"},
	{"20", "rems", "5", "80px (5rem)"},
	{"24", "rems", "6", "96px (6rem)"},
	{"32", "rems", "8", "128px (8rem)"},
	{"40", "rems", "10", "160px (10rem)"},
	{"48", "rems", "12", "192px (12rem)"},
	{"56", "rems", "14", "224px (14rem)"},
	{"64", "rems", "16", "256px (16rem)"},
	{"72", "rems", "18", "288px (18rem)"},
	{"80", "rems", "20", "320px (20rem)"},
	{"96", "rems", "24", "384px (24rem)"},
	{"112", "rems", "28", "448px (28rem)"},
	{"128", "rems", "32", "512px (32rem)"},
	{"auto", "auto", "", "Auto"},
	{"px", "px", "1", "1px"},
	{"full", "relative", "1", "100%"},
	{"1_2", "relative", "0.5", "50% (1/2)"},
	{"1_3", "relative", "1.0 / 3.0", "33% (1/3)"},
	{"2_3", "relative", "2.0 / 3.0", "66% (2/3)"},
	{"1_4", "relative", "0.25", "25% (1/4)"},
	{"2_4", "relative", "0.5", "50% (2/4)"},
	{"3_4", "relative", "0.75", "75% (3/4)"},
	{"1_5", "relative", "0.2", "20% (1/5)"},
	{"2_5", "relative", "0.4", "40% (2/5)"},
	{"3_5", "relative", "0.6", "60% (3/5)"},
	{"4_5", "relative", "0.8", "80% (4/5)"},
	{"1_6", "relative", "1.0 / 6.0", "16% (1/6)"},
	{"5_6", "relative", "5.0 / 6.0", "80% (5/6)"},
	{"1_12", "relative", "1.0 / 12.0", "8% (1/12)"},
}

// cornerPrefix mirrors styles.rs CornerStylePrefix (styles.rs:1651-1716).
type cornerPrefix struct {
	prefix string
	fields []string
	doc    string
	line   int
}

var cornerPrefixes = []cornerPrefix{
	{"rounded", []string{"CornerRadiiTopLeft", "CornerRadiiTopRight", "CornerRadiiBottomRight", "CornerRadiiBottomLeft"}, "Sets the border radius of the element. [Docs](https://tailwindcss.com/docs/border-radius)", 1654},
	{"rounded_t", []string{"CornerRadiiTopLeft", "CornerRadiiTopRight"}, "Sets the border radius of the top side of the element. [Docs](https://tailwindcss.com/docs/border-radius#rounding-sides-separately)", 1664},
	{"rounded_b", []string{"CornerRadiiBottomLeft", "CornerRadiiBottomRight"}, "Sets the border radius of the bottom side of the element. [Docs](https://tailwindcss.com/docs/border-radius#rounding-sides-separately)", 1672},
	{"rounded_r", []string{"CornerRadiiTopRight", "CornerRadiiBottomRight"}, "Sets the border radius of the right side of the element. [Docs](https://tailwindcss.com/docs/border-radius#rounding-sides-separately)", 1680},
	{"rounded_l", []string{"CornerRadiiTopLeft", "CornerRadiiBottomLeft"}, "Sets the border radius of the left side of the element. [Docs](https://tailwindcss.com/docs/border-radius#rounding-sides-separately)", 1688},
	{"rounded_tl", []string{"CornerRadiiTopLeft"}, "Sets the border radius of the top left corner of the element. [Docs](https://tailwindcss.com/docs/border-radius#rounding-corners-separately)", 1696},
	{"rounded_tr", []string{"CornerRadiiTopRight"}, "Sets the border radius of the top right corner of the element. [Docs](https://tailwindcss.com/docs/border-radius#rounding-corners-separately)", 1701},
	{"rounded_bl", []string{"CornerRadiiBottomLeft"}, "Sets the border radius of the bottom left corner of the element. [Docs](https://tailwindcss.com/docs/border-radius#rounding-corners-separately)", 1706},
	{"rounded_br", []string{"CornerRadiiBottomRight"}, "Sets the border radius of the bottom right corner of the element. [Docs](https://tailwindcss.com/docs/border-radius#rounding-corners-separately)", 1711},
}

// cornerSuffixes is styles.rs:1718-1766 corner_suffixes.
var cornerSuffixes = []boxSuffix{
	{"none", "px", "0", "0px"},
	{"xs", "rems", "0.125", "2px (0.125rem)"},
	{"sm", "rems", "0.25", "4px (0.25rem)"},
	{"md", "rems", "0.375", "6px (0.375rem)"},
	{"lg", "rems", "0.5", "8px (0.5rem)"},
	{"xl", "rems", "0.75", "12px (0.75rem)"},
	{"2xl", "rems", "1", "16px (1rem)"},
	{"3xl", "rems", "1.5", "24px (1.5rem)"},
	{"full", "px", "9999", "9999px"},
}

// smoothingSuffixes is styles.rs:1768-1826 corner_smoothing_suffixes.
var smoothingSuffixes = []struct{ suffix, amount, doc string }{
	{"0", "0.0", "Keeps circular corners by setting rounded corner smoothing to `0.0`."},
	{"0p1", "0.1", "Sets rounded corner smoothing to `0.1`."},
	{"0p2", "0.2", "Sets rounded corner smoothing to `0.2`."},
	{"0p3", "0.3", "Sets rounded corner smoothing to `0.3`."},
	{"0p4", "0.4", "Sets rounded corner smoothing to `0.4`."},
	{"0p5", "0.5", "Sets rounded corner smoothing to `0.5`."},
	{"0p6", "0.6", "Sets rounded corner smoothing to `0.6`."},
	{"0p7", "0.7", "Sets rounded corner smoothing to `0.7`."},
	{"0p8", "0.8", "Sets rounded corner smoothing to `0.8`."},
	{"0p9", "0.9", "Sets rounded corner smoothing to `0.9`."},
	{"1", "1.0", "Requests maximum rounded corner smoothing by setting it to `1.0`."},
}

// borderPrefixes is styles.rs:1828-1877 border_prefixes.
var borderPrefixes = []cornerPrefix{
	{"border", []string{"BorderWidthsTop", "BorderWidthsRight", "BorderWidthsBottom", "BorderWidthsLeft"}, "Sets the border width of the element. [Docs](https://tailwindcss.com/docs/border-width)", 1831},
	{"border_t", []string{"BorderWidthsTop"}, "Sets the border width of the top side of the element. [Docs](https://tailwindcss.com/docs/border-width#individual-sides)", 1841},
	{"border_b", []string{"BorderWidthsBottom"}, "Sets the border width of the bottom side of the element. [Docs](https://tailwindcss.com/docs/border-width#individual-sides)", 1846},
	{"border_r", []string{"BorderWidthsRight"}, "Sets the border width of the right side of the element. [Docs](https://tailwindcss.com/docs/border-width#individual-sides)", 1851},
	{"border_l", []string{"BorderWidthsLeft"}, "Sets the border width of the left side of the element. [Docs](https://tailwindcss.com/docs/border-width#individual-sides)", 1856},
	{"border_x", []string{"BorderWidthsLeft", "BorderWidthsRight"}, "Sets the border width of the vertical sides of the element. [Docs](https://tailwindcss.com/docs/border-width#horizontal-and-vertical-sides)", 1861},
	{"border_y", []string{"BorderWidthsTop", "BorderWidthsBottom"}, "Sets the border width of the horizontal sides of the element. [Docs](https://tailwindcss.com/docs/border-width#horizontal-and-vertical-sides)", 1868},
}

// borderSuffixes is styles.rs:1879-1967 border_suffixes (all px).
var borderSuffixes = []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "16", "20", "24", "32"}

// cursorMethods is styles.rs:562-736 cursor_style_methods: method,
// CursorStyle constant, doc keyword.
var cursorMethods = []struct{ name, cursor, doc string }{
	{"cursor_default", "CursorArrow", "default"},
	{"cursor_pointer", "CursorPointingHand", "pointer"},
	{"cursor_text", "CursorIBeam", "text"},
	{"cursor_move", "CursorClosedHand", "move"},
	{"cursor_not_allowed", "CursorOperationNotAllowed", "not-allowed"},
	{"cursor_context_menu", "CursorContextualMenu", "context-menu"},
	{"cursor_crosshair", "CursorCrosshair", "crosshair"},
	{"cursor_vertical_text", "CursorIBeamVertical", "vertical-text"},
	{"cursor_alias", "CursorDragLink", "alias"},
	{"cursor_copy", "CursorDragCopy", "copy"},
	{"cursor_no_drop", "CursorOperationNotAllowed", "no-drop"},
	{"cursor_grab", "CursorOpenHand", "grab"},
	{"cursor_grabbing", "CursorClosedHand", "grabbing"},
	{"cursor_ew_resize", "CursorResizeLeftRight", "ew-resize"},
	{"cursor_ns_resize", "CursorResizeUpDown", "ns-resize"},
	{"cursor_nesw_resize", "CursorResizeUpRightDownLeft", "nesw-resize"},
	{"cursor_nwse_resize", "CursorResizeUpLeftDownRight", "nwse-resize"},
	{"cursor_col_resize", "CursorResizeColumn", "col-resize"},
	{"cursor_row_resize", "CursorResizeRow", "row-resize"},
	{"cursor_n_resize", "CursorResizeUp", "n-resize"},
	{"cursor_e_resize", "CursorResizeRight", "e-resize"},
	{"cursor_s_resize", "CursorResizeDown", "s-resize"},
	{"cursor_w_resize", "CursorResizeLeft", "w-resize"},
}

// shadowPresets is styles.rs:865-951: name, then (offsetY, alpha, blur,
// spread) per shadow (offset x is always 0).
var shadowPresets = []struct {
	name    string
	line    int
	shadows [][4]string
}{
	{"shadow_2xs", 867, [][4]string{{"1", "0.05", "", ""}}},
	{"shadow_xs", 879, [][4]string{{"1", "0.05", "2", ""}}},
	{"shadow_sm", 891, [][4]string{{"1", "0.1", "3", ""}, {"1", "0.1", "2", "-1"}}},
	{"shadow_md", 904, [][4]string{{"4", "0.1", "6", "-1"}, {"2", "0.1", "4", "-2"}}},
	{"shadow_lg", 917, [][4]string{{"10", "0.1", "15", "-3"}, {"4", "0.1", "6", "-4"}}},
	{"shadow_xl", 930, [][4]string{{"20", "0.1", "25", "-5"}, {"8", "0.1", "10", "-6"}}},
	{"shadow_2xl", 943, [][4]string{{"25", "0.25", "50", "-12"}}},
}

// goName maps a pinned snake_case method name to the port's Go name:
// segments are capitalized and concatenated; an underscore is kept
// between two numeric segments ("w_1_2" -> W1_2).
func goName(snake string) string {
	parts := strings.Split(snake, "_")
	var b strings.Builder
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i > 0 && isDigit(part[0]) && isDigit(parts[i-1][len(parts[i-1])-1]) {
			b.WriteByte('_')
		}
		b.WriteString(strings.ToUpper(part[:1]) + part[1:])
	}
	return b.String()
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

type emitter struct {
	buf   bytes.Buffer
	names map[string]string
}

func (e *emitter) method(rust, goSig, doc string, body ...string) {
	name := goName(rust)
	if prev, dup := e.names[name]; dup {
		panic(fmt.Sprintf("stylegen: Go name %s of %s collides with %s", name, rust, prev))
	}
	e.names[name] = rust
	for _, line := range strings.Split(doc, "\n") {
		fmt.Fprintf(&e.buf, "// %s\n", line)
	}
	// A method without type parameters of its own gets the lazy marker
	// (styled.go): generic methods compile only where they are called.
	if strings.HasPrefix(goSig, "(") {
		goSig = "[_ lazy]" + goSig
	}
	fmt.Fprintf(&e.buf, "func (s *Styled[Self]) %s%s Self {\n", name, goSig)
	for _, line := range body {
		fmt.Fprintf(&e.buf, "\t%s\n", line)
	}
	e.buf.WriteString("\treturn s.self\n}\n\n")
}

// lengthExpr renders a suffix as a Go expression of the field kind.
func lengthExpr(sfx boxSuffix, negate bool, kind valueKind) string {
	value := sfx.value
	if negate {
		value = "-(" + value + ")"
		if sfx.value == "0" {
			// Rust's -px(0.) is IEEE negative zero, which a Go constant
			// cannot express.
			value = "negativeZero"
		}
	}
	var definite string
	switch sfx.unit {
	case "auto":
		return "AutoLength()"
	case "px":
		if kind == kindAbsolute {
			return fmt.Sprintf("Px(%s)", value)
		}
		definite = fmt.Sprintf("DefinitePx(%s)", value)
	case "rems":
		if kind == kindAbsolute {
			return fmt.Sprintf("RemsOf(%s)", value)
		}
		definite = fmt.Sprintf("DefiniteRem(%s)", value)
	case "relative":
		definite = fmt.Sprintf("Fraction(%s)", value)
	}
	if kind == kindLength {
		return "DefiniteLengthOf(" + definite + ")"
	}
	return definite
}

func (e *emitter) boxMethods(prefixes []boxPrefix, family string) {
	for _, p := range prefixes {
		kind, constraint, conv := kindLength, "IntoLength", "LengthOf"
		if !p.autoAllowed {
			kind, constraint, conv = kindDefinite, "IntoDefiniteLength", "DefiniteLengthFrom"
		}
		var body []string
		body = append(body, "style := s.StyleMut()", fmt.Sprintf("v := %s(length)", conv))
		for _, f := range p.fields {
			body = append(body, fmt.Sprintf("style.%s = Some(v)", f))
		}
		e.method(p.prefix, fmt.Sprintf("[L %s](length L)", constraint),
			fmt.Sprintf("%s sets %s.\n%s\n\nstyles.rs:%d %s prefix %q (custom value setter).", goName(p.prefix), strings.Join(p.fields, ", "), p.doc, p.line, family, p.prefix),
			body...)
		for _, sfx := range boxSuffixes {
			for _, negate := range []bool{false, true} {
				if !negate && sfx.suffix == "auto" && !p.autoAllowed {
					continue
				}
				if negate && sfx.suffix == "auto" {
					continue
				}
				rust := p.prefix + "_" + sfx.suffix
				if negate {
					rust = p.prefix + "_neg_" + sfx.suffix
				}
				expr := lengthExpr(sfx, negate, kind)
				body := []string{"style := s.StyleMut()"}
				for _, f := range p.fields {
					body = append(body, fmt.Sprintf("style.%s = Some(%s)", f, expr))
				}
				sign := ""
				if negate {
					sign = "-"
				}
				e.method(rust, "()", fmt.Sprintf("%s: %s\n\n%s%s (styles.rs:%d %s prefix %q, box_style_suffixes).", goName(rust), p.doc, sign, sfx.doc, p.line, family, p.prefix), body...)
			}
		}
	}
}

func main() {
	out := flag.String("out", "styled_generated.go", "output file")
	flag.Parse()

	e := &emitter{names: map[string]string{}}
	e.buf.WriteString(`// Code generated by cmd/stylegen from the pinned
// gpui_macros/src/styles.rs (254b5dbd47cbb5acbcc5bbdcbb322a339276c88a);
// DO NOT EDIT.

package gpui

`)

	// style_helpers! (styles.rs:40, generate_methods styles.rs:1058):
	// the size/gap box methods, the corner radii and corner smoothing.
	e.boxMethods(sizePrefixes, "box_prefixes")
	for _, p := range cornerPrefixes {
		body := []string{"style := s.StyleMut()"}
		for _, f := range p.fields {
			body = append(body, fmt.Sprintf("style.%s = Some(length)", f))
		}
		e.method(p.prefix, "(length AbsoluteLength)", fmt.Sprintf("%s sets %s.\n%s\n\nstyles.rs:%d corner_prefixes %q (custom value setter).", goName(p.prefix), strings.Join(p.fields, ", "), p.doc, p.line, p.prefix), body...)
		for _, sfx := range cornerSuffixes {
			rust := p.prefix + "_" + sfx.suffix
			expr := lengthExpr(sfx, false, kindAbsolute)
			body := []string{"style := s.StyleMut()"}
			for _, f := range p.fields {
				body = append(body, fmt.Sprintf("style.%s = Some(%s)", f, expr))
			}
			e.method(rust, "()", fmt.Sprintf("%s: %s\n\n%s (styles.rs:%d corner_prefixes %q, corner_suffixes).", goName(rust), p.doc, sfx.doc, p.line, p.prefix), body...)
		}
	}
	for _, sfx := range smoothingSuffixes {
		rust := "rounded_smoothing_" + sfx.suffix
		e.method(rust, "()", fmt.Sprintf("%s: %s\n\nstyles.rs:1094 generate_corner_smoothing_methods.", goName(rust), sfx.doc),
			fmt.Sprintf("s.StyleMut().CornerSmoothing = Some(float32(%s))", sfx.amount))
	}

	// visibility_style_methods! (styles.rs:453-473).
	e.method("visible", "()", "Visible sets the visibility to visible. [Docs](https://tailwindcss.com/docs/visibility)\n\nstyles.rs:459.", "s.StyleMut().Visibility = Some(VisibilityVisible)")
	e.method("invisible", "()", "Invisible sets the visibility to hidden. [Docs](https://tailwindcss.com/docs/visibility)\n\nstyles.rs:466.", "s.StyleMut().Visibility = Some(VisibilityHidden)")

	// margin_style_methods!, padding_style_methods! (styles.rs:475-501).
	e.boxMethods(marginPrefixes, "margin_box_style_prefixes")
	e.boxMethods(paddingPrefixes, "padding_box_style_prefixes")

	// position_style_methods! (styles.rs:503-530).
	e.method("relative", "()", "Relative sets the position to relative. [Docs](https://tailwindcss.com/docs/position)\n\nstyles.rs:514.", "s.StyleMut().Position = Some(PositionRelative)")
	e.method("absolute", "()", "Absolute sets the position to absolute. [Docs](https://tailwindcss.com/docs/position)\n\nstyles.rs:521.", "s.StyleMut().Position = Some(PositionAbsolute)")
	e.boxMethods(positionPrefixes, "position_box_style_prefixes")

	// overflow_style_methods! (styles.rs:532-560).
	e.method("overflow_hidden", "()", "OverflowHidden hides overflowing content on both axes. [Docs](https://tailwindcss.com/docs/overflow#hiding-content-that-overflows)\n\nstyles.rs:538.",
		"style := s.StyleMut()", "style.OverflowX = Some(OverflowHidden)", "style.OverflowY = Some(OverflowHidden)")
	e.method("overflow_x_hidden", "()", "OverflowXHidden hides overflowing content on the X axis.\n\nstyles.rs:546.", "s.StyleMut().OverflowX = Some(OverflowHidden)")
	e.method("overflow_y_hidden", "()", "OverflowYHidden hides overflowing content on the Y axis.\n\nstyles.rs:553.", "s.StyleMut().OverflowY = Some(OverflowHidden)")

	// cursor_style_methods! (styles.rs:562-736).
	e.method("cursor", "(cursor CursorStyle)", "Cursor sets the cursor shown when hovering over this element.\n\nstyles.rs:567.", "s.StyleMut().MouseCursor = Some(cursor)")
	for _, c := range cursorMethods {
		e.method(c.name, "()", fmt.Sprintf("%s sets the hover cursor to `%s` (%s). [Docs](https://tailwindcss.com/docs/cursor)\n\nstyles.rs cursor_style_methods.", goName(c.name), c.doc, c.cursor),
			fmt.Sprintf("s.StyleMut().MouseCursor = Some(%s)", c.cursor))
	}

	// border_style_methods! (styles.rs:738-785).
	e.method("border_color", "[C IntoBackground](borderColor C)", "BorderColor sets the background painted into the border.\n\nstyles.rs:772.", "s.StyleMut().BorderColor = Some(BackgroundOf(borderColor))")
	for _, p := range borderPrefixes {
		body := []string{"style := s.StyleMut()"}
		for _, f := range p.fields {
			body = append(body, fmt.Sprintf("style.%s = Some(length)", f))
		}
		e.method(p.prefix, "(length AbsoluteLength)", fmt.Sprintf("%s sets %s.\n%s\n\nstyles.rs:%d border_prefixes %q (custom value setter).", goName(p.prefix), strings.Join(p.fields, ", "), p.doc, p.line, p.prefix), body...)
		for _, sfx := range borderSuffixes {
			rust := p.prefix + "_" + sfx
			body := []string{"style := s.StyleMut()"}
			for _, f := range p.fields {
				body = append(body, fmt.Sprintf("style.%s = Some(Px(%s))", f, sfx))
			}
			e.method(rust, "()", fmt.Sprintf("%s: %s\n\n%spx (styles.rs:%d border_prefixes %q, border_suffixes).", goName(rust), p.doc, sfx, p.line, p.prefix), body...)
		}
	}

	// box_shadow_style_methods! (styles.rs:787-955).
	e.method("shadow", "(shadows []BoxShadow)", "Shadow sets the box shadows. [Docs](https://tailwindcss.com/docs/box-shadow)\n\nstyles.rs:815.", "s.StyleMut().BoxShadow = Some(append([]BoxShadow(nil), shadows...))")
	e.method("shadow_none", "()", "ShadowNone clears the box shadows (an explicit empty list).\n\nstyles.rs:822.", "s.StyleMut().BoxShadow = Some([]BoxShadow{})")
	e.method("ring", "(width float32)", "Ring sets the outer ring width.\n\nstyles.rs:829.", "s.StyleMut().RingWidth = Some(width)")
	e.method("ring_color", "[C IntoBackground](color C)", "RingColor sets the outer ring color.\n\nstyles.rs:836.", "s.StyleMut().RingColor = Some(RingColorOf(BackgroundOf(color)))")
	e.method("inset_ring", "(width float32)", "InsetRing sets the inset ring width.\n\nstyles.rs:847.", "s.StyleMut().InsetRingWidth = Some(width)")
	e.method("inset_ring_color", "[C IntoBackground](color C)", "InsetRingColor sets the inset ring color.\n\nstyles.rs:854.", "s.StyleMut().InsetRingColor = Some(RingColorOf(BackgroundOf(color)))")
	// Ring widths: border_suffixes filtered to 0/1/2/4/8 (styles.rs:790-810).
	for _, w := range []string{"0", "1", "2", "4", "8"} {
		e.method("ring_"+w, "()", fmt.Sprintf("Ring%s sets the ring width to %spx. [Docs](https://tailwindcss.com/docs/box-shadow)\n\nstyles.rs:790-810.", w, w), fmt.Sprintf("s.StyleMut().RingWidth = Some(float32(%s))", w))
		e.method("inset_ring_"+w, "()", fmt.Sprintf("InsetRing%s sets the inset ring width to %spx. [Docs](https://tailwindcss.com/docs/box-shadow)\n\nstyles.rs:790-810.", w, w), fmt.Sprintf("s.StyleMut().InsetRingWidth = Some(float32(%s))", w))
	}
	for _, preset := range shadowPresets {
		var items []string
		for _, sh := range preset.shadows {
			expr := fmt.Sprintf("NewBoxShadow(0, %s, SolidBackground(Hsla{H: 0, S: 0, L: 0, A: %s}))", sh[0], sh[1])
			if sh[2] != "" {
				expr += fmt.Sprintf(".WithBlurRadius(%s)", sh[2])
			}
			if sh[3] != "" {
				expr += fmt.Sprintf(".WithSpreadRadius(%s)", sh[3])
			}
			items = append(items, expr)
		}
		e.method(preset.name, "()", fmt.Sprintf("%s sets the box shadow preset. [Docs](https://tailwindcss.com/docs/box-shadow)\n\nstyles.rs:%d.", goName(preset.name), preset.line),
			fmt.Sprintf("s.StyleMut().BoxShadow = Some([]BoxShadow{%s})", strings.Join(items, ", ")))
	}

	// The method index, for the coverage test.
	names := make([]string, 0, len(e.names))
	for name := range e.names {
		names = append(names, name)
	}
	sort.Strings(names)
	e.buf.WriteString("// generatedStyledMethods maps every generated Go method name to its\n// pinned snake_case name.\nvar generatedStyledMethods = map[string]string{\n")
	for _, name := range names {
		fmt.Fprintf(&e.buf, "\t%q: %q,\n", name, e.names[name])
	}
	e.buf.WriteString("}\n")

	src, err := format.Source(e.buf.Bytes())
	if err != nil {
		os.WriteFile(*out+".broken", e.buf.Bytes(), 0o644)
		fmt.Fprintln(os.Stderr, "stylegen: gofmt:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, src, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "stylegen:", err)
		os.Exit(1)
	}
}
