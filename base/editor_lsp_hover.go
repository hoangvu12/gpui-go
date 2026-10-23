package base

// Port of U:input/editor/lsp/hover.rs: the hover provider, the delayed
// hover request on mouse move and clearing the hover state.

import (
	"time"

	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// hoverDelay is the first hover's delay (U:input/editor/lsp/hover.rs:53-57).
const hoverDelay = 150 * time.Millisecond

// HoverProvider answers textDocument/hover
// (U:input/editor/lsp/hover.rs:8-22). A nil hover is "no hover".
type HoverProvider interface {
	Hover(text rope.Rope, offset int, w *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[*LspHover]]
}

// handleHoverPopover requests the hover at offset; only the first hover
// waits (U:input/editor/lsp/hover.rs:25-86).
func (s *InputBaseState[M]) handleHoverPopover(offset int, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	if s.selecting {
		return
	}
	e := s.editorExtras()
	provider := e.lsp.HoverProvider
	if provider == nil {
		return
	}
	if e.hoverPopover != nil && e.hoverPopover.SymbolRange.Contains(offset) {
		return
	}

	text := s.text
	symbolRange := NewInputSelection(offset, offset)
	if start, end, ok := s.ext().WordRange(offset); ok {
		symbolRange = NewInputSelection(start, end)
	}
	shouldDelay := e.hoverPopover == nil
	// Ask the provider only after the delay: every mouse move replaces this
	// task, so positions the pointer merely passes over never reach it.
	e.lsp.hoverTask.set(cx.SpawnIn(w, func(this gpui.WeakEntity[InputBaseState[M]], acx *gpui.AsyncWindowContext) struct{} {
		if shouldDelay && !lspSleep(acx.AsyncApp, hoverDelay) {
			return struct{}{}
		}

		var task gpui.Task[ProviderResult[*LspHover]]
		requested := false
		_ = acx.Update(func(w *gpui.Window, app *gpui.App) {
			_ = this.TryUpdateIn(app, w, func(_ *InputBaseState[M], w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
				task = provider.Hover(text, offset, w, cx.App())
				requested = true
			})
		})
		if !requested {
			return struct{}{}
		}
		result, ok := awaitProvider(acx.AsyncApp, task)
		if !ok {
			return struct{}{}
		}

		acx.AsyncApp.Update(func(app *gpui.App) {
			_ = this.TryUpdate(app, func(s *InputBaseState[M], cx *gpui.Context[InputBaseState[M]]) {
				e := s.editorExtras()
				if result != nil {
					rng := symbolRange
					if result.Range != nil {
						rng = NewInputSelection(s.ext().PositionToOffset(result.Range.Start), s.ext().PositionToOffset(result.Range.End))
					}
					e.hoverPopover = &HoverPopoverState{SymbolRange: rng, Hover: *result}
				} else {
					e.hoverPopover = nil
				}
				cx.Notify()
			})
		})
		return struct{}{}
	}))
}

// handleMouseMove clears the hover on Alt, follows the definition under a
// secondary-modifier hover, and otherwise requests the hover popover
// (U:input/editor/lsp/hover.rs:88-110).
func (s *InputBaseState[M]) handleMouseMove(offset int, event *gpui.MouseMoveEvent, w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
	if event.Modifiers.Alt {
		s.ClearHoverState(cx)
		return
	}
	if event.Modifiers.Secondary() {
		s.handleHoverDefinition(offset, w, cx)
		return
	}
	// Repaint only when the definition underline goes away; the hover and
	// definition results notify when they arrive.
	e := s.editorExtras()
	if !e.hoverDefinition.isEmpty() {
		e.hoverDefinition.clear()
		cx.Notify()
	}
	s.handleHoverPopover(offset, w, cx)
}

// ClearHoverState drops the definition underline, the hover popover and
// the pending hover request (U:input/editor/lsp/hover.rs:112-121).
// EditorState only.
func (s *InputBaseState[M]) ClearHoverState(cx *gpui.Context[InputBaseState[M]]) {
	e := s.lspExtras("ClearHoverState")
	changed := !e.hoverDefinition.isEmpty() || e.hoverPopover != nil
	e.hoverDefinition.clear()
	e.hoverPopover = nil
	e.lsp.hoverTask.drop()
	if changed {
		cx.Notify()
	}
}
