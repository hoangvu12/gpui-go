package gpui

// Base ticket 10 (rich text): the paint side of text runs — the pinned
// HighlightStyle, its blend/refine semantics, combine_highlights, the
// TextStyle highlight/to_run helpers and the palette color blend they
// rest on. The pinned reference is GPUI-CE
// 254b5dbd47cbb5acbcc5bbdcbb322a339276c88a (R: = crates/gpui/src):
//
//   - HighlightStyle R:style.rs:751-772, HighlightStyle::color /
//     highlight R:style.rs:1159-1197, the From conversions
//     R:style.rs:1199-1234, combine_highlights R:style.rs:1236-1274;
//   - TextStyle::highlight R:style.rs:675-705, TextStyle::to_run
//     R:style.rs:724-740;
//   - ColorExt::blend / fade_out for Hsla R:color.rs:227-243 (palette
//     0.7.7 BlendWith over premultiplied Rgba with
//     Equations::from_parameters(OneMinusSourceAlpha, SourceAlpha):
//     palette-0.7.7/src/blend/blend_with.rs:57-71, equations.rs:52-140,
//     237-238).
//
// Go adaptation: the pinned (Range<usize>, HighlightStyle) tuples are
// HighlightRange values; Option fields are nil-able pointers like the
// port's TextStyle paint fields (div.go TextStyle).

import "sort"

// HighlightStyle is a highlight to apply to a text range: the pinned
// HighlightStyle (R:style.rs:751-772), a TextStyle overlay for one
// uniformly sized font. Nil fields leave the base style unchanged.
type HighlightStyle struct {
	// Color is the text color.
	Color *Hsla
	// FontWeight is the font weight, e.g. bold.
	FontWeight *float32
	// FontStyle is the font style, e.g. italic.
	FontStyle *FontStyle
	// BackgroundColor is the background color of the text.
	BackgroundColor *Hsla
	// Underline is the underline style of the text.
	Underline *UnderlineStyle
	// Strikethrough is the strikethrough style of the text.
	Strikethrough *StrikethroughStyle
	// FadeOut makes the text less vibrant, like the CSS opacity property
	// (0 leaves the color unchanged, 1 fades it out completely).
	FadeOut *float32
}

// HighlightStyleFromColor is the pinned HighlightStyle::color and
// From<Hsla> for HighlightStyle (R:style.rs:1161, 1199): a highlight
// with just a color.
func HighlightStyleFromColor(color Hsla) HighlightStyle {
	return HighlightStyle{Color: &color}
}

// HighlightStyleFromFontWeight is the pinned From<FontWeight> for
// HighlightStyle (R:style.rs:1208).
func HighlightStyleFromFontWeight(weight float32) HighlightStyle {
	return HighlightStyle{FontWeight: &weight}
}

// HighlightStyleFromFontStyle is the pinned From<FontStyle> for
// HighlightStyle (R:style.rs:1217).
func HighlightStyleFromFontStyle(style FontStyle) HighlightStyle {
	return HighlightStyle{FontStyle: &style}
}

// Highlight blends this highlight style with another
// (R:style.rs:1170-1196): the colors blend (palette source-alpha
// blend), fade-outs compound (dest * (1 + source), clamped), and every
// other property is overwritten by the other style when it sets it.
func (h HighlightStyle) Highlight(other HighlightStyle) HighlightStyle {
	out := HighlightStyle{
		Color:           h.Color,
		FontWeight:      firstSome(other.FontWeight, h.FontWeight),
		FontStyle:       firstSome(other.FontStyle, h.FontStyle),
		BackgroundColor: firstSome(other.BackgroundColor, h.BackgroundColor),
		Underline:       firstSome(other.Underline, h.Underline),
		Strikethrough:   firstSome(other.Strikethrough, h.Strikethrough),
		FadeOut:         h.FadeOut,
	}
	if other.Color != nil {
		color := *other.Color
		if h.Color != nil {
			color = h.Color.Blend(*other.Color)
		}
		out.Color = &color
	}
	if other.FadeOut != nil {
		fade := *other.FadeOut
		if h.FadeOut != nil {
			fade = clamp01(*h.FadeOut * (1 + *other.FadeOut))
		}
		out.FadeOut = &fade
	}
	return out
}

// Equal reports whether two highlight styles set the same properties to
// the same values (the pinned derived PartialEq, comparing the Option
// payloads rather than pointer identity).
func (h HighlightStyle) Equal(other HighlightStyle) bool {
	return equalPtr(h.Color, other.Color) &&
		equalPtr(h.FontWeight, other.FontWeight) &&
		equalPtr(h.FontStyle, other.FontStyle) &&
		equalPtr(h.BackgroundColor, other.BackgroundColor) &&
		equalUnderline(h.Underline, other.Underline) &&
		equalStrikethrough(h.Strikethrough, other.Strikethrough) &&
		equalPtr(h.FadeOut, other.FadeOut)
}

// IsEmpty reports whether the style sets nothing (the pinned
// HighlightStyle::default() comparison).
func (h HighlightStyle) IsEmpty() bool { return h.Equal(HighlightStyle{}) }

// HighlightRange is one highlighted byte range: the pinned
// (Range<usize>, HighlightStyle) pair the highlight APIs take.
type HighlightRange struct {
	// Range is the highlighted UTF-8 byte range.
	Range TextRange
	// Style is the highlight applied to the range.
	Style HighlightStyle
}

// CombineHighlights combines and merges the highlights and ranges of two
// lists (R:style.rs:1236-1274 combine_highlights): every non-empty range
// contributes a start and an end endpoint; between consecutive endpoint
// positions with at least one active highlight, the result carries the
// fold of the active styles from the default style.
//
// The pin folds the active set out of a HashSet, whose iteration order
// is unspecified; the port folds the active highlights in their input
// order (a's entries, then b's), the order the pinned test's expected
// values reflect (style.rs:1718-1804).
func CombineHighlights(a, b []HighlightRange) []HighlightRange {
	type endpoint struct {
		position    int
		highlightID int
		isStart     bool
	}
	var endpoints []endpoint
	var highlights []HighlightStyle
	for _, list := range [][]HighlightRange{a, b} {
		for _, entry := range list {
			if entry.Range.End <= entry.Range.Start {
				continue
			}
			id := len(highlights)
			endpoints = append(endpoints,
				endpoint{position: entry.Range.Start, highlightID: id, isStart: true},
				endpoint{position: entry.Range.End, highlightID: id, isStart: false})
			highlights = append(highlights, entry.Style)
		}
	}
	sort.SliceStable(endpoints, func(i, j int) bool { return endpoints[i].position < endpoints[j].position })

	var out []HighlightRange
	active := make(map[int]bool)
	ix := 0
	for _, point := range endpoints {
		previous := ix
		ix = point.position
		if ix > previous && len(active) > 0 {
			ids := make([]int, 0, len(active))
			for id := range active {
				ids = append(ids, id)
			}
			sort.Ints(ids)
			current := HighlightStyle{}
			for _, id := range ids {
				current = current.Highlight(highlights[id])
			}
			out = append(out, HighlightRange{Range: TextRange{Start: previous, End: ix}, Style: current})
		}
		if point.isStart {
			active[point.highlightID] = true
		} else {
			delete(active, point.highlightID)
		}
	}
	return out
}

// Highlight returns the text style with the highlight applied
// (R:style.rs:675-705 TextStyle::highlight): weight and style overwrite,
// the color blends with the highlight color then fades out, and the
// background, underline and strikethrough overwrite when set.
func (t TextStyle) Highlight(style HighlightStyle) TextStyle {
	if style.FontWeight != nil {
		t.FontWeight = *style.FontWeight
	}
	if style.FontStyle != nil {
		t.FontStyle = *style.FontStyle
	}
	if style.Color != nil {
		t.Color = t.Color.Blend(*style.Color)
	}
	if style.FadeOut != nil {
		t.Color.FadeOut(*style.FadeOut)
	}
	if style.BackgroundColor != nil {
		color := *style.BackgroundColor
		t.BackgroundColor = &color
	}
	if style.Underline != nil {
		underline := *style.Underline
		t.Underline = &underline
	}
	if style.Strikethrough != nil {
		strikethrough := *style.Strikethrough
		t.Strikethrough = &strikethrough
	}
	return t
}

// ToRun converts the text style into a run of the given byte length
// (R:style.rs:724-740 TextStyle::to_run): the style's font, paint
// fields and letter spacing.
func (t TextStyle) ToRun(length int) TextRun {
	run := TextRun{
		Len:             length,
		Font:            t.FontDescriptorOf(),
		Color:           t.Color,
		BackgroundColor: t.BackgroundColor,
		Underline:       t.Underline,
		Strikethrough:   t.Strikethrough,
		LetterSpacing:   t.LetterSpacing,
	}
	return run
}

// Blend performs the pinned SrcAlpha x (1 - SrcAlpha) blend of this
// color (the source) with another (the destination) (R:color.rs:228-232
// ColorExt::blend for Hsla): both convert to sRGB Rgba, blend through
// palette's BlendWith with Equations::from_parameters(
// OneMinusSourceAlpha, SourceAlpha) over premultiplied components, and
// convert back.
func (c Hsla) Blend(other Hsla) Hsla {
	src := hslaToRgba(c)
	dst := hslaToRgba(other)
	// premultiply (blend_with.rs:63-70): color * alpha.
	sr, sg, sb, sa := src.R*src.A, src.G*src.A, src.B*src.A, src.A
	dr, dg, db, da := dst.R*dst.A, dst.G*dst.A, dst.B*dst.A, dst.A
	// Equations::apply_to with Add (equations.rs:78-140): the source
	// parameter is 1 - source alpha, the destination parameter the
	// source alpha (equations.rs:235-236), for color and alpha alike.
	srcParam := 1 - sa
	dstParam := sa
	r := sr*srcParam + dr*dstParam
	g := sg*srcParam + dg*dstParam
	b := sb*srcParam + db*dstParam
	a := sa*srcParam + da*dstParam
	// unpremultiply: a zero alpha yields black (palette blend.rs:94-99).
	if a == 0 {
		return rgbaToHsla(Rgba{})
	}
	return rgbaToHsla(Rgba{R: r / a, G: g / a, B: b / a, A: a})
}

// FadeOut fades the color out by a factor in [0, 1]: 0 leaves the color
// unchanged, 1 makes it fully transparent (R:color.rs:234-236
// ColorExt::fade_out: alpha *= 1 - clamp(factor)).
func (c *Hsla) FadeOut(factor float32) {
	c.A *= 1 - clamp01(factor)
}

// rgbaToHsla converts an sRGB Rgba to the scene's Hsla: the f32-channel
// form of RgbaToHsla's palette Srgb->Hsl port (paint.go), used by the
// blend's into_color round trip.
func rgbaToHsla(color Rgba) Hsla {
	red, green, blue := max32(color.R, 0), max32(color.G, 0), max32(color.B, 0)
	var mx, mn, sep, coeff float32
	if red > green {
		mx, mn, sep, coeff = red, green, green-blue, 0
	} else {
		mx, mn, sep, coeff = green, red, blue-red, 2
	}
	if blue > mx {
		mx, sep, coeff = blue, red-green, 4
	} else if blue < mn {
		mn = blue
	}
	sum := mx + mn
	l := sum / 2
	h, s := float32(0), float32(0)
	if mx != mn {
		d := mx - mn
		if sum > 1 {
			s = d / (2 - sum)
		} else {
			s = d / sum
		}
		h = (sep/d + coeff) * 60
	}
	return Hsla{H: normHueDeg(h) / 360, S: s, L: l, A: color.A}
}

// firstSome is the pinned Option::or: a when set, else b.
func firstSome[T any](a, b *T) *T {
	if a != nil {
		return a
	}
	return b
}

// equalPtr compares two optional values by payload.
func equalPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// equalUnderline compares two optional underline styles by payload.
func equalUnderline(a, b *UnderlineStyle) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Thickness == b.Thickness && a.Wavy == b.Wavy && equalPtr(a.Color, b.Color)
}

// equalStrikethrough compares two optional strikethrough styles by
// payload.
func equalStrikethrough(a, b *StrikethroughStyle) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Thickness == b.Thickness && equalPtr(a.Color, b.Color)
}
