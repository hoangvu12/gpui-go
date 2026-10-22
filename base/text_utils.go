package base

// Port of U:text/utils.rs (ticket 28): list item prefixes and inline
// data-URL images. ordered_list_ordinal is text_node.go's
// orderedListOrdinal (ticket 27 needed it first). Also the small geometry
// helpers the text renderer shares (the pinned Bounds methods gpui does
// not export: from_corners, intersect, union, dilate).

import (
	"reflect"
	"runtime"
	"strconv"
	"unsafe"

	"github.com/hoangvu12/gpui-go/base/dataurl"
	"github.com/hoangvu12/gpui-go/gpui"
)

const (
	textNumberedPrefixes1 = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	textNumberedPrefixes2 = "abcdefghijklmnopqrstuvwxyz"
)

var textBullets = [5]string{"•", "◦", "▪", "‣", "⁃"}

// listItemPrefix is the prefix of a list item (U:text/utils.rs:18-54):
// numbers at the top level (or when the list starts at 0), A-Z one level
// down, a-z below that; bullets by depth.
func listItemPrefix(ix int, start *uint32, ordered bool, depth int) string {
	if ordered {
		ordinal := orderedListOrdinal(start, ix)
		if depth == 0 || (start != nil && *start == 0) {
			return strconv.FormatUint(uint64(ordinal), 10) + ". "
		}
		alphaIx := 0
		if ordinal > 0 {
			alphaIx = int(ordinal - 1)
		}
		if depth == 1 {
			return string(textNumberedPrefixes1[alphaIx%len(textNumberedPrefixes1)]) + ". "
		}
		return string(textNumberedPrefixes2[alphaIx%len(textNumberedPrefixes2)]) + ". "
	}
	depth = min(depth, len(textBullets)-1)
	return textBullets[depth] + " "
}

// dataURLImage decodes a `data:` URL whose mime type names an image format
// gpui decodes; anything else is nil, and the caller keeps the URL
// URI-backed (U:text/utils.rs:56-66).
func dataURLImage(url string) *gpui.Image {
	d, err := dataurl.Process(url)
	if err != nil {
		return nil
	}
	mime := d.MimeType()
	format, ok := gpui.ImageFormatFromMimeType(mime.Type + "/" + mime.Subtype)
	if !ok {
		return nil
	}
	bytes, _, err := d.DecodeToVec()
	if err != nil {
		return nil
	}
	image := gpui.NewImage(format, bytes)
	return &image
}

// textBoundsFromCorners is Bounds::from_corners.
func textBoundsFromCorners(a, b gpui.Point) gpui.Bounds {
	return gpui.Bounds{Origin: a, Size: gpui.Size{Width: b.X - a.X, Height: b.Y - a.Y}}
}

// textBoundsIntersect is Bounds::intersect: the overlap, with a zero size
// when there is none.
func textBoundsIntersect(a, b gpui.Bounds) gpui.Bounds {
	left, top := max(a.Left(), b.Left()), max(a.Top(), b.Top())
	right, bottom := min(a.Right(), b.Right()), min(a.Bottom(), b.Bottom())
	return gpui.Bounds{
		Origin: gpui.Point{X: left, Y: top},
		Size:   gpui.Size{Width: max(right-left, 0), Height: max(bottom-top, 0)},
	}
}

// textBoundsDilate is Bounds::dilate.
func textBoundsDilate(b gpui.Bounds, amount float32) gpui.Bounds {
	return gpui.Bounds{
		Origin: gpui.Point{X: b.Origin.X - amount, Y: b.Origin.Y - amount},
		Size:   gpui.Size{Width: b.Size.Width + 2*amount, Height: b.Size.Height + 2*amount},
	}
}

// textPointAdd is Point + Point.
func textPointAdd(a, b gpui.Point) gpui.Point { return gpui.Point{X: a.X + b.X, Y: a.Y + b.Y} }

// textPointSub is Point - Point.
func textPointSub(a, b gpui.Point) gpui.Point { return gpui.Point{X: a.X - b.X, Y: a.Y - b.Y} }

// textStyleEqual is TextStyle's derived PartialEq (gpui.TextStyle holds
// slices and pointers, so it is compared deeply).
func textStyleEqual(a, b gpui.TextStyle) bool { return reflect.DeepEqual(a, b) }

// textStyleSubtractDefault is Refineable::subtract(&TextStyle::default()):
// a refinement holding every field that differs from the default style.
func textStyleSubtractDefault(style gpui.TextStyle) gpui.TextStyleRefinement {
	d := gpui.DefaultTextStyle()
	var r gpui.TextStyleRefinement
	if style.Color != d.Color {
		r.Color = gpui.Some(style.Color)
	}
	if style.FontFamily != d.FontFamily {
		r.FontFamily = gpui.Some(style.FontFamily)
	}
	if !reflect.DeepEqual(style.FontFeatures, d.FontFeatures) {
		r.FontFeatures = gpui.Some(style.FontFeatures)
	}
	if !reflect.DeepEqual(style.FontFallbacks, d.FontFallbacks) {
		r.FontFallbacks = gpui.Some(style.FontFallbacks)
	}
	if style.FontSize != d.FontSize {
		r.FontSize = gpui.Some(style.FontSize)
	}
	if style.LineHeight != d.LineHeight {
		r.LineHeight = gpui.Some(style.LineHeight)
	}
	if style.FontWeight != d.FontWeight {
		r.FontWeight = gpui.Some(style.FontWeight)
	}
	if style.FontStyle != d.FontStyle {
		r.FontStyle = gpui.Some(style.FontStyle)
	}
	if style.BackgroundColor != nil {
		r.BackgroundColor = gpui.Some(*style.BackgroundColor)
	}
	if style.Underline != nil {
		r.Underline = gpui.Some(*style.Underline)
	}
	if style.Strikethrough != nil {
		r.Strikethrough = gpui.Some(*style.Strikethrough)
	}
	if style.WhiteSpace != d.WhiteSpace {
		r.WhiteSpace = gpui.Some(style.WhiteSpace)
	}
	if style.TextOverflow != nil {
		r.TextOverflow = gpui.Some(*style.TextOverflow)
	}
	if style.TextAlign != d.TextAlign {
		r.TextAlign = gpui.Some(style.TextAlign)
	}
	if style.LineClamp != nil {
		r.LineClamp = gpui.Some(*style.LineClamp)
	}
	if style.LetterSpacing != nil {
		r.LetterSpacing = gpui.Some(*style.LetterSpacing)
	}
	if style.TextTransform != nil {
		r.TextTransform = gpui.Some(*style.TextTransform)
	}
	return r
}

// sameStringData reports whether two strings share their bytes (the
// SharedString pointer comparison of U:text/state.rs:375-377).
func sameStringData(a, b string) bool {
	return len(a) == len(b) && (len(a) == 0 || unsafe.StringData(a) == unsafe.StringData(b))
}

// textIsMacOS selects the macOS key bindings (cfg(target_os = "macos")).
func textIsMacOS() bool { return runtime.GOOS == "darwin" }
