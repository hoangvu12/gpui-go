package base

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ports of U:text/inline_virtual_tests.rs: 2 tests; 1 deviated, 1
// deferred:
//   - inline_virtual_resource_growth_remeasures_all_blocks_and_preserves
//     _copy drags a selection through crate::TextSelectionLayer and copies
//     it: ported by ticket 29 in text_view_selection_test.go.
//   - inline_virtual_font_and_rem_zoom_remeasure_offscreen_blocks is
//     deviated: the text-size zoom is ported; the rem zoom step needs
//     Window::set_rem_size, which the gpui port does not expose.

const virtualBlocks = 24

func virtualDocument() string {
	parts := make([]string, virtualBlocks)
	for i := range parts {
		parts[i] = fmt.Sprintf("第%d段 $x$ $y$ English", i)
	}
	return strings.Join(parts, "\n\n")
}

// U:text/inline_virtual_tests.rs:135-170 (deviated, see above).
func TestInlineVirtualFontZoomRemeasuresOffscreenBlocks(t *testing.T) {
	ta := textTestApp(t)
	state := newTextViewStateEntity(ta, virtualDocument())
	scale := float32(1)
	plugin := textRenderPlugin{
		testInlinePlugin: newTestInlinePlugin("math").parseWith(func(node *MdastNode, _ *MarkdownParseContext) *MarkdownNode {
			if node.Kind != MdastInlineMath {
				return nil
			}
			n := NewMarkdownNode("math", nil).Text(node.Value + "²")
			return &n
		}),
		render: func(_ *MarkdownNode, context *InlineRenderContext, _ *gpui.Window, _ *gpui.App) (InlineElement, bool) {
			extent := context.FontSize() * 40 / 16
			img := gpui.Img(gpui.NewImage(gpui.ImageFormatSVG, []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="40"/>`))).Flex().W(gpui.Px(extent)).H(gpui.Px(extent))
			return NewInlineElement(img).WithBaseline(extent * 0.75), true
		},
	}
	root, cx := openTestView(t, ta, func(*gpui.Window, *gpui.Context[testView]) gpui.AnyElement {
		return gpui.Div().Block().W(gpui.Px(400)).H(gpui.Px(200)).OverflowHidden().TextSize(gpui.RemsOf(scale)).
			Child(NewTextView(state).Scrollable(true).Plugin(plugin)).IntoElement()
	})
	settle := func() {
		cx.RunUntilParked()
		drawText(t, cx)
		cx.RunUntilParked()
	}
	totalHeight := func() float32 {
		return readState(ta, state, func(s *TextViewState) float32 {
			if s.listState.ItemCount() != virtualBlocks {
				t.Fatalf("%d blocks", s.listState.ItemCount())
			}
			return s.listState.MaxOffsetForScrollbar().Y + s.listState.ViewportBounds().Size.Height
		})
	}
	settle()
	normal := totalHeight()
	for _, s := range []float32{1.5, 2} {
		scale = s
		root.Update(ta, func(_ *testView, cx *gpui.Context[testView]) { cx.Notify() })
		settle()
		if zoomed := totalHeight(); !(zoomed > normal*s*0.85) {
			t.Errorf("scale %v: normal %v, zoomed %v", s, normal, zoomed)
		}
	}
}
