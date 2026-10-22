package base

// Port of U:input/editor/lsp/document_colors.rs: the document color
// provider, its debounced refresh and the swatches of the visible lines.

import (
	"math"
	"slices"
	"time"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// maxDocumentColors is MAX_DOCUMENT_COLORS, the most colors accepted from
// one response (U:input/editor/lsp/document_colors.rs:11-12).
const maxDocumentColors = 10_000

// documentColorDebounce is the refresh debounce
// (U:input/editor/lsp/document_colors.rs:98-102).
const documentColorDebounce = 100 * time.Millisecond

// DocumentColorProvider answers textDocument/documentColor
// (U:input/editor/lsp/document_colors.rs:14-26).
type DocumentColorProvider interface {
	DocumentColors(text rope.Rope, w *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[[]LspColorInformation]]
}

// lspDocumentColor is one cached (lsp_types::Range, Hsla) pair
// (U:input/editor/lsp/mod.rs:61).
type lspDocumentColor struct {
	rng   LspRange
	color gpui.Hsla
}

// documentColorsFromResponse converts the colors, sorted by start; false
// when the response is over the limit
// (U:input/editor/lsp/document_colors.rs:28-52).
func documentColorsFromResponse(colors []LspColorInformation) ([]lspDocumentColor, bool) {
	if len(colors) > maxDocumentColors {
		return nil, false
	}
	out := make([]lspDocumentColor, 0, len(colors))
	for _, info := range colors {
		out = append(out, lspDocumentColor{rng: info.Range, color: lspColorToHsla(info.Color)})
	}
	// sort_by_key is stable.
	slices.SortStableFunc(out, func(a, b lspDocumentColor) int { return comparePosition(a.rng.Start, b.rng.Start) })
	return out, true
}

// comparePosition is lsp_types::Position's derived Ord: line, then
// character.
func comparePosition(a, b InputPosition) int {
	switch {
	case a.Line != b.Line:
		if a.Line < b.Line {
			return -1
		}
		return 1
	case a.Character < b.Character:
		return -1
	case a.Character > b.Character:
		return 1
	}
	return 0
}

// lspColorToHsla is `gpui::Rgba::new(r, g, b, a).into_color()`: palette
// 0.7's f32 Srgb -> Hsl, the arithmetic of the core's RgbaToHsla
// (gpui/paint.go) on unquantized channels (that core function takes
// packed bytes; its f32 form is unexported).
func lspColorToHsla(c LspColor) gpui.Hsla {
	red, green, blue := max(c.Red, 0), max(c.Green, 0), max(c.Blue, 0)
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
	// palette's normalize_unsigned_angle.
	hue := h - float32(math.Floor(float64(h/360)))*360
	if b := math.Float32bits(hue); (b&0x7f800000 == 0 && b&0x007fffff != 0) || hue >= 360 {
		hue = 0
	}
	return gpui.Hsla{H: hue / 360, S: s, L: l, A: c.Alpha}
}

// documentColorsForRange is the colors intersecting the visible rows
// (0-based), as non-empty byte ranges; ranges that convert to empty or
// inverted byte ranges are skipped (U:input/editor/lsp/
// document_colors.rs:55-82).
func (l *Lsp) documentColorsForRange(text rope.Rope, visibleRange InputSelection) []inputColoredRange {
	ext := RopeExtOf(&text)
	var out []inputColoredRange
	for _, dc := range l.documentColors {
		if int(dc.rng.Start.Line) > visibleRange.End || int(dc.rng.End.Line) < visibleRange.Start {
			continue
		}
		start := ext.PositionToOffset(dc.rng.Start)
		end := ext.PositionToOffset(dc.rng.End)
		if start >= end {
			continue
		}
		out = append(out, inputColoredRange{rng: NewInputSelection(start, end), color: dc.color})
	}
	return out
}

// updateDocumentColors refetches the colors after a 100ms debounce
// (U:input/editor/lsp/document_colors.rs:84-123).
func (l *Lsp) updateDocumentColors(text rope.Rope, w *gpui.Window, cx *inputCx[EditorMode]) {
	provider := l.DocumentColorProvider
	if provider == nil {
		return
	}
	inputState := cx.Entity().Downgrade()
	l.documentColorTask.set(cx.SpawnIn(w, func(_ gpui.WeakEntity[EditorState], acx *gpui.AsyncWindowContext) struct{} {
		if !lspSleep(acx.AsyncApp, documentColorDebounce) {
			return struct{}{}
		}
		var task gpui.Task[ProviderResult[[]LspColorInformation]]
		if acx.Update(func(w *gpui.Window, app *gpui.App) { task = provider.DocumentColors(text, w, app) }) != nil {
			return struct{}{}
		}
		colors, ok := awaitProvider(acx.AsyncApp, task)
		if !ok {
			return struct{}{}
		}
		acx.AsyncApp.Update(func(app *gpui.App) {
			_ = inputState.TryUpdate(app, func(s *EditorState, cx *inputCx[EditorMode]) {
				documentColors, ok := documentColorsFromResponse(colors)
				if !ok {
					return
				}
				lsp := &s.editorExtras().lsp
				if !slices.Equal(documentColors, lsp.documentColors) {
					lsp.documentColors = documentColors
					cx.Notify()
				}
			})
		})
		return struct{}{}
	}))
}
