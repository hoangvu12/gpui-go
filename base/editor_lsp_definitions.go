package base

// Port of U:input/editor/lsp/definitions.rs: the definition provider, the
// secondary-modifier hover underline with its clickable hitbox, the
// GoToDefinition action and following a location.

import (
	"github.com/hoangvu12/gpui-go/base/rope"
	"github.com/hoangvu12/gpui-go/gpui"
)

// DefinitionProvider answers textDocument/definition
// (U:input/editor/lsp/definitions.rs:10-24).
type DefinitionProvider interface {
	Definitions(text rope.Rope, offset int, w *gpui.Window, app *gpui.App) gpui.Task[ProviderResult[[]LspLocationLink]]
}

// hoverDefinition is HoverDefinition: the hovered symbol and its
// definition locations (U:input/editor/lsp/definitions.rs:26-56).
type hoverDefinition struct {
	// symbolRange is the range of the symbol that triggered the hover.
	symbolRange InputSelection
	locations   []LspLocationLink
}

// update is U:input/editor/lsp/definitions.rs:34-42.
func (h *hoverDefinition) update(symbolRange InputSelection, locations []LspLocationLink) {
	h.clear()
	h.symbolRange = symbolRange
	h.locations = locations
}

// isEmpty is U:input/editor/lsp/definitions.rs:44-46.
func (h *hoverDefinition) isEmpty() bool { return len(h.locations) == 0 }

// clear is U:input/editor/lsp/definitions.rs:48-51.
func (h *hoverDefinition) clear() {
	h.symbolRange = InputSelection{}
	h.locations = nil
}

// isSame is U:input/editor/lsp/definitions.rs:53-55.
func (h *hoverDefinition) isSame(offset int) bool { return h.symbolRange.Contains(offset) }

// handleHoverDefinition asks for the definitions under the pointer and
// underlines the symbol when there are some
// (U:input/editor/lsp/definitions.rs:59-102).
func (s *InputBaseState[M]) handleHoverDefinition(offset int, w *gpui.Window, cx *inputCx[M]) {
	e := s.editorExtras()
	provider := e.lsp.DefinitionProvider
	if provider == nil {
		return
	}
	if e.hoverDefinition.isSame(offset) {
		return
	}

	task := provider.Definitions(s.text, offset, w, cx.App())
	symbolRange := NewInputSelection(offset, offset)
	if start, end, ok := s.ext().WordRange(offset); ok {
		symbolRange = NewInputSelection(start, end)
	}
	e.lsp.hoverTask.set(cx.SpawnIn(w, func(editor gpui.WeakEntity[InputBaseState[M]], acx *gpui.AsyncWindowContext) struct{} {
		locations, ok := awaitProvider(acx.AsyncApp, task)
		if !ok {
			return struct{}{}
		}
		acx.AsyncApp.Update(func(app *gpui.App) {
			_ = editor.TryUpdate(app, func(s *InputBaseState[M], cx *inputCx[M]) {
				e := s.editorExtras()
				if len(locations) == 0 {
					e.hoverDefinition.clear()
				} else {
					rng := symbolRange
					if origin := locations[0].OriginSelectionRange; origin != nil {
						rng = NewInputSelection(s.ext().PositionToOffset(origin.Start), s.ext().PositionToOffset(origin.End))
					}
					e.hoverDefinition.update(rng, locations)
				}
				cx.Notify()
			})
		})
		return struct{}{}
	}))
}

// onActionGoToDefinition goes to the first definition at the cursor; it
// works before the symbol was hovered. A blur, a cursor move or an edit
// before the answer cancels it (U:input/editor/lsp/definitions.rs:
// 104-136).
func (s *InputBaseState[M]) onActionGoToDefinition(_ *GoToDefinition, w *gpui.Window, cx *inputCx[M]) {
	offset := s.Cursor()
	e := s.editorExtras()
	provider := e.lsp.DefinitionProvider
	if provider == nil {
		return
	}
	text := s.text
	response := provider.Definitions(text, offset, w, cx.App())
	blurSubscription := cx.OnBlur(s.focusHandle, w, func(s *InputBaseState[M], _ *gpui.Window, _ *inputCx[M]) {
		s.editorExtras().lsp.dropDefinitionTask()
	})
	// The subscription lives as long as the task (`_blur_subscription`
	// moved into it): dropDefinitionTask cancels both.
	e.lsp.setDefinitionTask(cx.SpawnIn(w, func(editor gpui.WeakEntity[InputBaseState[M]], acx *gpui.AsyncWindowContext) struct{} {
		locations, ok := awaitProvider(acx.AsyncApp, response)
		if !ok {
			return struct{}{}
		}
		_ = acx.Update(func(w *gpui.Window, app *gpui.App) {
			_ = editor.TryUpdateIn(app, w, func(s *InputBaseState[M], w *gpui.Window, cx *inputCx[M]) {
				// The task ends here; its subscription goes with it.
				s.editorExtras().lsp.definitionBlur.Cancel()
				if s.Cursor() != offset || !s.text.Equal(text) || !s.focusHandle.IsFocused(w) {
					return
				}
				if len(locations) > 0 {
					s.goToDefinition(&locations[0], w, cx)
				}
			})
		})
		return struct{}{}
	}), blurSubscription)
}

// handleClickHoverDefinition follows the underlined definition on a
// secondary-modifier click; true when handled
// (U:input/editor/lsp/definitions.rs:138-164).
func (s *InputBaseState[M]) handleClickHoverDefinition(event *gpui.MouseDownEvent, offset int, w *gpui.Window, cx *inputCx[M]) bool {
	if !event.Modifiers.Secondary() || event.Modifiers.Alt {
		return false
	}
	e := s.editorExtras()
	if e.hoverDefinition.isEmpty() || !e.hoverDefinition.isSame(offset) {
		return false
	}
	location := e.hoverDefinition.locations[0]
	s.goToDefinition(&location, w, cx)
	return true
}

// goToDefinition offers the location to the host's show-document hook,
// then opens an http(s) target in the browser or selects the target range
// in this document (U:input/editor/lsp/definitions.rs:166-203).
func (s *InputBaseState[M]) goToDefinition(location *LspLocationLink, w *gpui.Window, cx *inputCx[M]) {
	scheme, hasScheme := uriScheme(location.TargetURI)
	external := hasScheme && (scheme == "https" || scheme == "http")

	// Give the host a chance to show the document first
	// (window/showDocument), e.g. virtual or external documents.
	if handler := s.editorExtras().lsp.ShowDocument; handler != nil {
		takeFocus := true
		selection := location.TargetSelectionRange
		params := LspShowDocumentParams{
			URI:       location.TargetURI,
			External:  &external,
			TakeFocus: &takeFocus,
			Selection: &selection,
		}
		if handler(&params, w, cx.App()) {
			return
		}
	}

	if external {
		_ = cx.App().OpenURL(location.TargetURI)
		return
	}
	// Move to the location.
	target := location.TargetSelectionRange
	start := s.ext().PositionToOffset(target.Start)
	end := s.ext().PositionToOffset(target.End)
	s.moveTo(start, nil, cx)
	s.selectTo(end, cx)
}

// uriScheme is fluent_uri's Uri::scheme: the leading
// `ALPHA *( ALPHA / DIGIT / "+" / "-" / "." ) ":"` of the URI (RFC 3986
// §3.1), as written.
func uriScheme(uri string) (string, bool) {
	for i := 0; i < len(uri); i++ {
		c := uri[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
		case i > 0 && c == ':':
			return uri[:i], true
		default:
			return "", false
		}
	}
	return "", false
}

// hoverDefinitionStyle is the range highlighted while Cmd-hovering a
// symbol: the theme's link_text style, underlined
// (U:input/editor/lsp/definitions.rs:209-227).
func (s *InputBaseState[M]) hoverDefinitionStyle() (gpui.HighlightRange, bool) {
	e := s.editorExtras()
	if e.hoverDefinition.isEmpty() {
		return gpui.HighlightRange{}, false
	}
	resolver := s.editorStyle.HighlightStyles
	if resolver == nil {
		return gpui.HighlightRange{}, false
	}
	style, ok := resolver.Style("link_text")
	if !ok {
		return gpui.HighlightRange{}, false
	}
	style.Underline = &gpui.UnderlineStyle{Thickness: 1}
	rng := e.hoverDefinition.symbolRange
	return gpui.HighlightRange{Range: gpui.TextRange{Start: rng.Start, End: rng.End}, Style: style}, true
}

// hoverDefinitionHitbox is the hitbox that makes a Cmd-hovered symbol
// clickable (U:input/editor/lsp/definitions.rs:229-242).
func (s *InputBaseState[M]) hoverDefinitionHitbox(w *gpui.Window) (gpui.Hitbox, bool) {
	e := s.editorExtras()
	if e.hoverDefinition.isEmpty() {
		return gpui.Hitbox{}, false
	}
	bounds, ok := s.RangeToBounds(e.hoverDefinition.symbolRange)
	if !ok {
		return gpui.Hitbox{}, false
	}
	return w.InsertHitbox(bounds, gpui.HitboxNormal), true
}
