package base

// The TextElement's inline-token paths (U:input/base/element.rs:1408-1864):
// the element each token renders as, measuring the tokens into the wrap
// map's inline metrics, shaping token rows as inline lines, and placing
// the token elements. They live beside token_presentation.rs's port
// because they only run when tokens show.

import (
	"slices"
	"sort"

	"github.com/hoangvu12/gpui-go/gpui"
)

// pressedToken is the pressed token's start offset, with the document
// revision and pointer position at the press
// (U:input/base/state.rs:353-355).
type pressedToken struct {
	start    int
	revision uint64
	position gpui.Point
}

// findTokenAt is the span starting at start.
func findTokenAt(spans []InlineTokenSpan, start int) (InlineTokenSpan, bool) {
	for _, span := range spans {
		if span.Range().Start == start {
			return span, true
		}
	}
	return InlineTokenSpan{}, false
}

// tokenElement is the element a token renders as: an object, not text,
// addressed by where it starts (U:input/base/element.rs:1408-1539).
func (e *textElement[M]) tokenElement(token *InlineTokenContext, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	presentation := e.read(app).tokenPresentation
	child := presentation.render(token, w, app)
	// The same reference may occur more than once, and the document
	// revision guards a press against edits that move it.
	start := token.Range().Start
	state := e.state
	disabled := token.IsDisabled()
	accessible := presentation.hasListener() && !disabled

	d := gpui.Div().ID(gpui.NamedIntegerElementID("inline-token", uint64(start))).
		Flex().
		ItemsCenter().
		H(gpui.Px(token.LineHeight())).
		MaxW(gpui.Px(token.AvailableWidth())).
		OverflowHidden().
		// A token is an object, not text: the arrow, never the I-beam.
		CursorDefault()
	if accessible {
		d.Role(gpui.RoleButton).
			AriaLabel(token.Token().Label()).
			OnA11yAction(gpui.A11yActionClick, func(_ *gpui.A11yActionData, w *gpui.Window, app *gpui.App) {
				listener, event, ok := state.Read(app, func(s *InputBaseState[M], _ *gpui.App) activation {
					span, found := findTokenAt(s.tokenSpans(), start)
					if !found {
						return activation{}
					}
					bounds, ok := s.RangeToBounds(span.Range())
					if !ok {
						return activation{}
					}
					click := gpui.ClickEvent{Kind: gpui.ClickEventKeyboard, Keyboard: gpui.KeyboardClickEvent{Bounds: bounds}}
					l, ev, ok := s.tokenActivation(start, bounds, click)
					return activation{l, ev, ok}
				}).unpack()
				if ok {
					listener(&event, w, app)
				}
			})
	}
	d.CaptureAnyMouseDown(func(_ *gpui.MouseDownEvent, w *gpui.Window, app *gpui.App) {
		if disabled {
			w.PreventDefault()
			app.StopPropagation()
		}
	}).
		CaptureAnyMouseUp(func(_ *gpui.MouseUpEvent, w *gpui.Window, app *gpui.App) {
			if disabled {
				w.PreventDefault()
				app.StopPropagation()
			}
		}).
		CaptureKeyDown(func(_ *gpui.KeyDownEvent, _ *gpui.Window, app *gpui.App) {
			if disabled {
				app.StopPropagation()
			}
		}).
		OnMouseDown(gpui.MouseButtonLeft, func(event *gpui.MouseDownEvent, w *gpui.Window, app *gpui.App) {
			if w.DefaultPrevented() {
				return
			}
			_ = state.UpdateIn(app, w, func(s *InputBaseState[M], w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
				s.pressedToken = nil
				if event.ClickCount == 2 {
					if span, ok := findTokenAt(s.tokenSpans(), start); ok {
						s.SetSelectedRange(span.Range(), cx)
						s.Focus(w, cx)
						w.PreventDefault()
					}
				} else if event.ClickCount == 1 && !event.Modifiers.Shift && !event.Modifiers.Alt {
					s.pressedToken = &pressedToken{start: start, revision: s.documentRevision, position: event.Position}
				}
			})
		}).
		OnMouseMove(func(event *gpui.MouseMoveEvent, _ *gpui.Window, app *gpui.App) {
			state.Update(app, func(s *InputBaseState[M], _ *gpui.Context[InputBaseState[M]]) {
				if p := s.pressedToken; p != nil {
					if abs32(event.Position.X-p.position.X)+abs32(event.Position.Y-p.position.Y) > 4 {
						s.pressedToken = nil
					}
				}
			})
		}).
		OnClick(func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			a := state.UpdateWith(app, func(s *InputBaseState[M], cx *gpui.Context[InputBaseState[M]]) activation {
				pressed := s.pressedToken
				s.pressedToken = nil
				if pressed == nil || pressed.start != start || pressed.revision != s.documentRevision {
					return activation{}
				}
				if m := event.Modifiers(); m.Shift || m.Alt {
					return activation{}
				}
				span, ok := findTokenAt(s.tokenSpans(), start)
				if !ok {
					return activation{}
				}
				// A click selects the whole token; opening it is the
				// listener's decision.
				s.SetSelectedRange(span.Range(), cx)
				bounds, ok := s.RangeToBounds(span.Range())
				if !ok {
					return activation{}
				}
				l, ev, ok := s.tokenActivation(start, bounds, *event)
				return activation{l, ev, ok}
			})
			if listener, ev, ok := a.unpack(); ok {
				listener(&ev, w, app)
			}
		}).
		Child(child)
	return d.IntoElement()
}

// activation is Option<(InlineTokenClickListener, InlineTokenClickEvent)>.
type activation struct {
	listener InlineTokenClickListener
	event    InlineTokenClickEvent
	ok       bool
}

func (a activation) unpack() (InlineTokenClickListener, InlineTokenClickEvent, bool) {
	return a.listener, a.event, a.ok
}

// tokenAvailableSize is size(MaxContent, Definite(line_height)).
func tokenAvailableSize(lineHeight float32) gpui.AvailableSize {
	return gpui.AvailableSize{Width: gpui.MaxContentAvailableSpace(), Height: gpui.DefiniteAvailableSpace(lineHeight)}
}

// measureTokens lays the visible (or, when the cache is stale, all)
// tokens out as root elements, records their widths and installs them as
// the wrap map's inline metrics; it returns the measured elements by
// start offset (U:input/base/element.rs:1541-1702).
func (e *textElement[M]) measureTokens(width, lineHeight, viewport float32, w *gpui.Window, app *gpui.App) map[int]gpui.AnyElement {
	style := gpui.WindowTextStyle(w)
	fontSize := style.FontSizePixels(gpui.RemSize(w))
	state := e.read(app)
	key := tokenLayoutKey{
		font:       style.FontDescriptorOf(),
		fontSize:   fontSize,
		width:      width,
		lineHeight: lineHeight,
		unwrapped:  state.IsSingleLine() || !state.softWrap,
	}
	if !state.tokensVisible() {
		if state.tokenLayoutCache == nil {
			return nil
		}
		e.state.Update(app, func(s *InputBaseState[M], _ *gpui.Context[InputBaseState[M]]) { s.displayMap.setInlineMetrics(nil, w) })
		return nil
	}
	revision := state.documentRevision
	cache := state.tokenLayoutCache
	spans := state.tokenSpans()
	all := cache == nil || !key.equal(cache.key) || cache.revision != revision ||
		slices.ContainsFunc(spans, func(span InlineTokenSpan) bool { _, ok := cache.widths[span.token]; return !ok })
	visible, _, _ := calculateVisibleRange(state, lineHeight, viewport)
	ext := state.ext()
	start := ext.LineStartOffset(visible.Start)
	end := ext.LineEndOffset(max(visible.End-1, 0))
	first := 0
	if !all {
		first = sort.Search(len(spans), func(i int) bool { return spans[i].rng.End > start })
	}
	var contexts []InlineTokenContext
	for _, span := range spans[first:] {
		if !all && span.rng.Start > end {
			break
		}
		inView := span.rng.Start <= end && span.rng.End >= start
		unmeasured := cache == nil || !key.equal(cache.key)
		if !unmeasured {
			_, measured := cache.widths[span.token]
			unmeasured = !measured
		}
		if inView || unmeasured {
			contexts = append(contexts, state.tokenContext(span, lineHeight, width))
		}
	}

	elements := make(map[int]gpui.AnyElement, len(contexts))
	type measuredWidth struct {
		token InlineToken
		width float32
	}
	measured := make([]measuredWidth, 0, len(contexts))
	for i := range contexts {
		token := &contexts[i]
		element := e.tokenElement(token, w, app)
		size := element.LayoutAsRoot(tokenAvailableSize(lineHeight), w, app)
		measured = append(measured, measuredWidth{token.Token(), max(min(size.Width, width), 1)})
		elements[token.Range().Start] = element
	}

	_ = e.state.UpdateIn(app, w, func(s *InputBaseState[M], w *gpui.Window, cx *gpui.Context[InputBaseState[M]]) {
		cache := s.tokenLayoutCache
		s.tokenLayoutCache = nil
		if cache == nil {
			cache = &tokenLayoutCache{}
		}
		if cache.widths == nil {
			cache.widths = map[InlineToken]float32{}
		}
		sameKey := key.equal(cache.key)
		changed := !sameKey || cache.revision != revision
		if !sameKey {
			clear(cache.widths)
		}
		for _, m := range measured {
			if old, ok := cache.widths[m.token]; !ok || old != m.width {
				changed = true
			}
			cache.widths[m.token] = m.width
		}
		k := key
		cache.key = &k
		if changed {
			spans := s.tokenSpans()
			present := make(map[InlineToken]bool, len(spans))
			for _, span := range spans {
				present[span.token] = true
			}
			for token := range cache.widths {
				if !present[token] {
					delete(cache.widths, token)
				}
			}
			cache.metrics = cache.metrics[:0:0]
			for _, span := range spans {
				if width, ok := cache.widths[span.token]; ok {
					cache.metrics = append(cache.metrics, inlineMetric{rng: span.rng, width: width})
				}
			}
			cache.revision = revision
			if s.IsSingleLine() || !s.softWrap {
				cache.unwrappedWidth = s.unwrappedTokenWidth(cache, fontSize, style, w)
			}
		}
		s.tokenLayoutCache = cache
		s.displayMap.setInlineMetrics(cache.metrics, w)
		if s.mode.isAutoGrow() {
			rows := s.mode.rowsCount()
			s.mode.updateAutoGrow(s.displayMap.wrapRowCount())
			if s.mode.rowsCount() != rows {
				cx.Notify()
			}
		}
	})
	return elements
}

// unwrappedTokenWidth is the widest of the token rows and the longest row
// laid out unwrapped with the token widths
// (U:input/base/element.rs:1639-1688).
func (s *InputBaseState[M]) unwrappedTokenWidth(cache *tokenLayoutCache, fontSize float32, style gpui.TextStyle, w *gpui.Window) float32 {
	spans := s.tokenSpans()
	ext := s.ext()
	rows := make([]int, 0, len(spans)+1)
	for _, span := range spans {
		rows = append(rows, ext.OffsetToPoint(span.rng.Start).Row)
	}
	rows = append(rows, s.displayMap.longestRow())
	slices.Sort(rows)
	rows = slices.Compact(rows)
	// Hard tabs draw as blank columns from the row's start
	// (input_tab_map.go).
	tabs := s.displayTabs(w)
	var widest float32
	for _, row := range rows {
		start := ext.LineStartOffset(row)
		text := ext.SliceLine(row).String()
		shapeWidth := func(from, to int) float32 {
			part := text[from:to]
			return tabs.shapeLine(w, part, tabs.columnAfter(text[:from], 0), fontSize, []gpui.TextRun{style.ToRun(len(part))}).Width()
		}
		offset := 0
		var width float32
		first := sort.Search(len(spans), func(i int) bool { return spans[i].rng.End > start })
		for _, span := range spans[first:] {
			if span.rng.Start >= start+len(text) {
				break
			}
			localStart, localEnd := span.rng.Start-start, span.rng.End-start
			width += shapeWidth(offset, localStart)
			width += cache.widths[span.token]
			offset = localEnd
		}
		widest = max(widest, width+shapeWidth(offset, len(text)))
	}
	return widest
}

// layoutTokenLines shapes the visible rows as inline lines: text pieces
// between fixed-width token boxes (U:input/base/element.rs:1704-1807).
func layoutTokenLines[M InputModeKind](state *InputBaseState[M], layout *lastLayout, fontSize float32, runs []gpui.TextRun, w *gpui.Window) []*inputLineLayout {
	spans := state.tokenSpans()
	cache := state.tokenLayoutCache
	if cache == nil {
		panic("base: tokens measured before shaping")
	}
	ext := state.ext()
	tabs := state.displayTabs(w)
	runOffset := 0
	out := make([]*inputLineLayout, 0, len(layout.visibleBufferLines))
	for _, row := range layout.visibleBufferLines {
		lineStart := ext.LineStartOffset(row)
		text := ext.SliceLine(row).String()
		ranges := []InputSelection{NewInputSelection(0, len(text))}
		if !state.IsSingleLine() {
			item, ok := state.displayMap.line(row)
			if !ok {
				panic("base: prepared line")
			}
			ranges = item.wrappedLines
		}
		// Hard tabs draw as blank columns from the row's start
		// (input_tab_map.go).
		shapePart := func(part InputSelection) *gpui.ShapedLine {
			return tabs.shapeLine(w, text[part.Start:part.End], tabs.columnAfter(text[:part.Start], 0), fontSize, runsForRange(runs, runOffset, part))
		}
		lines := make([]*inputLine, 0, len(ranges))
		for _, rng := range ranges {
			var fragments []inlineFragment
			offset := rng.Start
			var x float32
			first := sort.Search(len(spans), func(i int) bool { return spans[i].rng.End > lineStart+rng.Start })
			for _, span := range spans[first:] {
				if span.rng.Start >= lineStart+rng.End {
					break
				}
				local := NewInputSelection(span.rng.Start-lineStart, span.rng.End-lineStart)
				if offset < local.Start {
					part := NewInputSelection(offset, local.Start)
					shaped := shapePart(part)
					fragments = append(fragments, inlineFragment{
						rng: NewInputSelection(part.Start-rng.Start, part.End-rng.Start), x: x, width: shaped.Width(), text: shaped,
					})
					x += shaped.Width()
				}
				width := cache.widths[span.token]
				fragments = append(fragments, inlineFragment{
					rng: NewInputSelection(local.Start-rng.Start, local.End-rng.Start), x: x, width: width,
				})
				x += width
				offset = local.End
			}
			if offset < rng.End {
				part := NewInputSelection(offset, rng.End)
				shaped := shapePart(part)
				fragments = append(fragments, inlineFragment{
					rng: NewInputSelection(part.Start-rng.Start, part.End-rng.Start), x: x, width: shaped.Width(), text: shaped,
				})
			}
			lines = append(lines, newInlineInputLine(text[rng.Start:rng.End], fragments))
		}
		var indent uint32
		if item, ok := state.displayMap.line(row); ok {
			indent = item.indent
		}
		var wrapIndent float32
		if indent > 0 && len(lines) > 1 {
			wrapIndent = lines[0].xForIndex(nthCharByteIndex(text, int(indent)))
		}
		runOffset += len(text) + 1
		out = append(out, newInlineLineLayout(lines).withWrapIndent(wrapIndent))
	}
	return out
}

// nthCharByteIndex is char_indices().nth(n).map_or(len, |(ix, _)| ix).
func nthCharByteIndex(text string, n int) int {
	i := 0
	for ix := range text {
		if i == n {
			return ix
		}
		i++
	}
	return len(text)
}

// prepaintTokens places the token elements at their rows, laying out the
// ones measuring did not (U:input/base/element.rs:1809-1864).
func (e *textElement[M]) prepaintTokens(layout *lastLayout, bounds gpui.Bounds, measured map[int]gpui.AnyElement, w *gpui.Window, app *gpui.App) []gpui.AnyElement {
	state := e.read(app)
	if !state.tokensVisible() {
		return nil
	}
	width := bounds.Size.Width
	if c := state.tokenLayoutCache; c != nil && c.key != nil {
		width = c.key.width
	}
	type placement struct {
		token  InlineTokenContext
		origin gpui.Point
	}
	var placements []placement
	ext := state.ext()
	y := layout.visibleTop
	spans := state.tokenSpans()
	for ix, row := range layout.visibleBufferLines {
		start, end := ext.LineStartOffset(row), ext.LineEndOffset(row)
		first := sort.Search(len(spans), func(i int) bool { return spans[i].rng.End > start })
		for _, span := range spans[first:] {
			if span.rng.Start >= end {
				break
			}
			if pos, ok := layout.lines[ix].positionForIndex(span.rng.Start-start, layout, false); ok {
				placements = append(placements, placement{
					token: state.tokenContext(span, layout.lineHeight, width),
					origin: gpui.Point{
						X: bounds.Origin.X + pos.X + layout.lineNumberWidth,
						Y: bounds.Origin.Y + pos.Y + y,
					},
				})
			}
		}
		y += layout.lines[ix].size(layout.lineHeight).Height
	}
	out := make([]gpui.AnyElement, 0, len(placements))
	for i := range placements {
		p := &placements[i]
		element, ok := measured[p.token.Range().Start]
		delete(measured, p.token.Range().Start)
		if !ok {
			element = e.tokenElement(&p.token, w, app)
			element.LayoutAsRoot(tokenAvailableSize(layout.lineHeight), w, app)
		}
		element.PrepaintAt(p.origin, w, app)
		out = append(out, element)
	}
	return out
}
