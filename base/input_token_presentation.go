package base

// Port of U:input/base/token_presentation.rs: presentation callbacks and
// measured geometry of inline tokens. None of this enters document
// history.
//
// Go adaptations: Rc<dyn Fn> renderer and listener are func values;
// HashMap<InlineToken, Pixels> is a map keyed by the comparable
// InlineToken; the cache key tuple (Font, Pixels, Pixels, Pixels, bool) is
// tokenLayoutKey.

import (
	"reflect"

	"github.com/hoangvu12/gpui-go/gpui"
)

// InlineTokenContext is the read-only context of one inline renderer; the
// width is the full available row
// (U:input/base/token_presentation.rs:6-15).
type InlineTokenContext struct {
	span           InlineTokenSpan
	selected       bool
	disabled       bool
	readonly       bool
	lineHeight     float32
	availableWidth float32
}

// Token is U:input/base/token_presentation.rs:17-19.
func (c InlineTokenContext) Token() InlineToken { return c.span.Token() }

// Range is U:input/base/token_presentation.rs:20-22.
func (c InlineTokenContext) Range() InputSelection { return c.span.Range() }

// IsSelected is U:input/base/token_presentation.rs:23-25.
func (c InlineTokenContext) IsSelected() bool { return c.selected }

// IsDisabled is U:input/base/token_presentation.rs:26-28.
func (c InlineTokenContext) IsDisabled() bool { return c.disabled }

// IsReadonly is U:input/base/token_presentation.rs:29-31.
func (c InlineTokenContext) IsReadonly() bool { return c.readonly }

// LineHeight is U:input/base/token_presentation.rs:32-34.
func (c InlineTokenContext) LineHeight() float32 { return c.lineHeight }

// AvailableWidth is U:input/base/token_presentation.rs:35-37.
func (c InlineTokenContext) AvailableWidth() float32 { return c.availableWidth }

// InlineTokenClickEvent is the current token snapshot delivered after the
// editor's update is released (U:input/base/token_presentation.rs:40-46).
type InlineTokenClickEvent struct {
	span   InlineTokenSpan
	bounds gpui.Bounds
	event  gpui.ClickEvent
}

// Token is U:input/base/token_presentation.rs:48-50.
func (e InlineTokenClickEvent) Token() InlineToken { return e.span.Token() }

// Range is U:input/base/token_presentation.rs:51-53.
func (e InlineTokenClickEvent) Range() InputSelection { return e.span.Range() }

// Bounds is U:input/base/token_presentation.rs:54-56.
func (e InlineTokenClickEvent) Bounds() gpui.Bounds { return e.bounds }

// Click is the click that opened the token; keyboard activation reports a
// keyboard click (U:input/base/token_presentation.rs:57-61).
func (e InlineTokenClickEvent) Click() gpui.ClickEvent { return e.event }

// InlineTokenRenderer is a renderer installed by a styled control; not
// part of the supported API (U:input/base/token_presentation.rs:64-66).
type InlineTokenRenderer func(token *InlineTokenContext, w *gpui.Window, app *gpui.App) gpui.AnyElement

// InlineTokenClickListener is a click listener installed by a styled
// control; not part of the supported API
// (U:input/base/token_presentation.rs:67-69).
type InlineTokenClickListener func(event *InlineTokenClickEvent, w *gpui.Window, app *gpui.App)

// inlineTokenPresentation is shared by Base and styled controls; it owns
// no content (U:input/base/token_presentation.rs:71-77).
type inlineTokenPresentation struct {
	renderer InlineTokenRenderer
	listener InlineTokenClickListener
	secret   bool
}

// token is U:input/base/token_presentation.rs:79-87.
func (p inlineTokenPresentation) token(render InlineTokenRenderer) inlineTokenPresentation {
	p.renderer = render
	return p
}

// onTokenClick is U:input/base/token_presentation.rs:88-94.
func (p inlineTokenPresentation) onTokenClick(listener InlineTokenClickListener) inlineTokenPresentation {
	p.listener = listener
	return p
}

// hasListener is U:input/base/token_presentation.rs:95-97.
func (p inlineTokenPresentation) hasListener() bool { return p.listener != nil }

// render falls back to a div with the token label
// (U:input/base/token_presentation.rs:98-112; gap 2b: the plain div is a
// block).
func (p inlineTokenPresentation) render(token *InlineTokenContext, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	if p.renderer != nil {
		return p.renderer(token, w, app)
	}
	return gpui.Div().Block().Child(token.Token().Label()).IntoElement()
}

// tokenLayoutKey is the cache key (font, font size, width, line height,
// unwrapped) (U:input/base/token_presentation.rs:117).
type tokenLayoutKey struct {
	font       gpui.FontDescriptor
	fontSize   float32
	width      float32
	lineHeight float32
	unwrapped  bool
}

func (k tokenLayoutKey) equal(o *tokenLayoutKey) bool {
	return o != nil && k.fontSize == o.fontSize && k.width == o.width && k.lineHeight == o.lineHeight &&
		k.unwrapped == o.unwrapped && reflect.DeepEqual(k.font, o.font)
}

// tokenLayoutCache is TokenLayoutCache
// (U:input/base/token_presentation.rs:115-122).
type tokenLayoutCache struct {
	key            *tokenLayoutKey
	revision       uint64
	unwrappedWidth float32
	metrics        []inlineMetric
	widths         map[InlineToken]float32
}

// setTokenPresentation injects presentation from a view without editing
// or notifying the document (U:input/base/token_presentation.rs:125-128).
func (s *InputBaseState[M]) setTokenPresentation(presentation inlineTokenPresentation) {
	s.tokenPresentation = presentation
}

// InstallTokenPresentation installs a styled control's renderer, click
// listener and secrecy without editing or notifying the document; not
// part of the supported API (U:input/base/token_presentation.rs:129-143).
func (s *InputBaseState[M]) InstallTokenPresentation(renderer InlineTokenRenderer, listener InlineTokenClickListener, secret bool) {
	s.tokenPresentation = inlineTokenPresentation{renderer: renderer, listener: listener, secret: secret}
}

// tokensVisible is U:input/base/token_presentation.rs:144-149.
func (s *InputBaseState[M]) tokensVisible() bool {
	return !s.masked && !s.tokenPresentation.secret && s.maskPattern.IsNone() && len(s.tokenSpans()) > 0
}

// tokenContext is U:input/base/token_presentation.rs:150-166.
func (s *InputBaseState[M]) tokenContext(span InlineTokenSpan, lineHeight, width float32) InlineTokenContext {
	rng := span.Range()
	selection := s.SelectedRange()
	return InlineTokenContext{
		span:           span,
		selected:       selection.Start < rng.End && rng.Start < selection.End,
		disabled:       s.disabled,
		readonly:       s.readonly,
		lineHeight:     lineHeight,
		availableWidth: width,
	}
}

// tokenActivation is the token starting at start paired with the listener
// that opens it (U:input/base/token_presentation.rs:167-190).
func (s *InputBaseState[M]) tokenActivation(start int, bounds gpui.Bounds, event gpui.ClickEvent) (InlineTokenClickListener, InlineTokenClickEvent, bool) {
	if s.disabled || !s.tokensVisible() || s.tokenPresentation.listener == nil {
		return nil, InlineTokenClickEvent{}, false
	}
	for _, span := range s.tokenSpans() {
		if span.Range().Start == start {
			return s.tokenPresentation.listener, InlineTokenClickEvent{span: span, bounds: bounds, event: event}, true
		}
	}
	return nil, InlineTokenClickEvent{}, false
}

// tokenIsSecret is U:input/base/token_presentation.rs:191-193.
func (s *InputBaseState[M]) tokenIsSecret() bool { return s.tokenPresentation.secret }
