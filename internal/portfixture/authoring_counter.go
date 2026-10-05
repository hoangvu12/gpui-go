package portfixture

// This file implements the authoring-counter-v1 fixture runner (fx-0006):
// the envelope's counter cases executed against the real gpui
// authoring runtime — the element phases, the div element, the view
// root render, the shared model entity, the scene paint and the test
// text system — recording the same trace events the reference harness
// records (reference/harness/src/fixtures/authoring_counter.rs, run_case
// op for op).
//
// The port mirror of the reference test-support app's draw driving: the
// reference app redraws every dirty window at each update boundary
// (app.rs flush_effects under cfg(test, feature "test-support")), so an
// increment update's flush redraws each window whose root view was
// notified — exactly one draw per dirty window. The port fixture tracks
// the same dirtiness (a window is dirty when its root view's observer
// fired since the window's last draw) and draws those windows after the
// increment update, before capturing.
//
// The gate (authoring_counter_test.go) compares the port trace against
// the recorded reference trace with CompareTraces: every event must
// match exactly, including every f32 bit pattern of every bound, mask,
// color, radius, shaped metric and glyph position, every draw order,
// and every render/notify count.

import (
	"fmt"

	"gpui-go/conformance"
	"gpui-go/gpui"
)

// RunAuthoringCounter executes the envelope's counter cases against the
// real gpui authoring runtime and returns the port-side trace (with the
// port harness identity and no envelope hash: run metadata belongs to
// the caller, which pins the envelope bytes it executed).
func RunAuthoringCounter(envelope *conformance.Envelope) (*conformance.Trace, error) {
	if envelope == nil {
		return nil, fmt.Errorf("portfixture: nil envelope")
	}
	if envelope.FixtureKind != conformance.FixtureKindAuthoringCounter {
		return nil, fmt.Errorf("portfixture: unsupported fixture kind %q", envelope.FixtureKind)
	}
	inputs := envelope.Inputs.CounterCases
	if inputs == nil {
		return nil, fmt.Errorf("portfixture: fixture kind authoring-counter-v1 requires inputs.counter_cases")
	}

	rec := &recorder{}
	for i := range inputs.Cases {
		if err := runAuthoringCounterCase(&inputs.Cases[i], rec); err != nil {
			return nil, fmt.Errorf("authoring-counter case %q failed: %w", inputs.Cases[i].Label, err)
		}
	}

	trace := &conformance.Trace{
		Schema:      conformance.TraceSchema,
		FixtureID:   envelope.FixtureID,
		FixtureKind: envelope.FixtureKind,
		Harness: conformance.HarnessInfo{
			HarnessVersion: "0.1.0",
			HarnessCrate:   "gpui-go-portfixture",
			GpuiCrate:      "gpui-ce 0.2.2",
			GpuiCommit:     pinnedGpuiCommit,
			Profile:        "test",
		},
		Events: rec.snapshot(),
	}
	return trace, nil
}

// ---------------------------------------------------------------------------
// The counter app (the mirror of the reference fixture's view code)
// ---------------------------------------------------------------------------

// counterModel is the shared counter model entity.
type counterModel struct {
	value uint64
}

// resolvedCounterStyle is the case style applied to the counter div
// tree.
type resolvedCounterStyle struct {
	padding    gpui.DefiniteLength
	fontSize   gpui.AbsoluteLength
	justify    gpui.AlignContent
	background gpui.Hsla
}

// counterView is the counter root view: one per window, observing the
// shared model (the mirror of the reference CounterView).
type counterView struct {
	model    gpui.Entity[counterModel]
	style    resolvedCounterStyle
	renders  int
	notified int
}

// countText is the count text rendered by the view (the mirror of the
// reference count_text).
func countText(value uint64) string {
	return fmt.Sprintf("Count: %d", value)
}

// Render implements gpui.Render[counterView]: the div tree with the
// count label, styled by the case (the mirror of the reference
// CounterView::render).
func (v *counterView) Render(w *gpui.Window, cx *gpui.Context[counterView]) gpui.AnyElement {
	v.renders++
	value := v.model.Read(cx, func(m *counterModel, _ *gpui.App) uint64 { return m.value })
	return gpui.Div().
		Flex().
		Gap2().
		P(v.style.padding).
		Bg(v.style.background).
		SizeFull().
		TextSize(v.style.fontSize).
		Justify(v.style.justify).
		ID("counter-root").
		DebugSelector("root").
		Child(gpui.Div().
			Flex().
			ID("counter-label").
			DebugSelector("label").
			Child(countText(value))).
		IntoElement()
}

// fixtureWindow is one open window of a case.
type fixtureWindow struct {
	label  string
	window *gpui.Window
	view   gpui.Entity[counterView]
	// style is the case's resolved counter style (the capture derives
	// the text facts from it).
	style resolvedCounterStyle
	// lastNotified is the view's notified count at the window's last
	// draw (the dirty mirror of the reference flush-draw).
	lastNotified int
}

// viewFacts reads the view entity's render and notification counts.
func (fw *fixtureWindow) viewFacts(app *gpui.App) (renders, notified int) {
	type facts struct{ renders, notified int }
	f := fw.view.Read(app, func(v *counterView, _ *gpui.App) facts {
		return facts{renders: v.renders, notified: v.notified}
	})
	return f.renders, f.notified
}

// resolveCounterStyle parses the case's style inputs into real gpui
// values (the mirror of the reference resolve_style).
func resolveCounterStyle(style conformance.CounterStyleIn) (resolvedCounterStyle, error) {
	padding, err := gpui.ParseDefiniteLength(style.Padding)
	if err != nil {
		return resolvedCounterStyle{}, fmt.Errorf("parsing padding %q: %w", style.Padding, err)
	}
	fontSize, err := gpui.ParseAbsoluteLength(style.FontSize)
	if err != nil {
		return resolvedCounterStyle{}, fmt.Errorf("parsing font size %q: %w", style.FontSize, err)
	}
	var justify gpui.AlignContent
	switch style.Justify {
	case "Center":
		justify = gpui.AlignContentCenter
	case "Start":
		justify = gpui.AlignContentStart
	case "End":
		justify = gpui.AlignContentEnd
	case "SpaceBetween":
		justify = gpui.AlignContentSpaceBetween
	default:
		return resolvedCounterStyle{}, fmt.Errorf("unsupported justify %q: expected Center, Start, End or SpaceBetween", style.Justify)
	}
	value, err := parseSceneHexColor(style.Background)
	if err != nil {
		return resolvedCounterStyle{}, err
	}
	background := gpui.RgbaToHsla(value)
	return resolvedCounterStyle{padding: padding, fontSize: fontSize, justify: justify, background: background}, nil
}

// runAuthoringCounterCase executes one case (the mirror of the
// reference run_case).
func runAuthoringCounterCase(c *conformance.AuthoringCounterCase, rec *recorder) error {
	if len(c.Windows) != 2 {
		return fmt.Errorf("expected exactly 2 windows, got %d", len(c.Windows))
	}
	style, err := resolveCounterStyle(c.CounterStyle)
	if err != nil {
		return fmt.Errorf("style: %w", err)
	}

	ta := gpui.NewTestApp()
	app := ta.App()

	// The model entity is created first, owned by the app's root scope
	// (the reference model is an app-level entity both views share).
	model := gpui.NewEntity(app, app.RootScope(), func(m *counterModel, _ *gpui.Context[counterModel]) {
		m.value = c.InitialValue
	})

	// Open the two windows; each draws once at open (the mirror of the
	// reference App::open_window drawing before returning).
	var windows []*fixtureWindow
	for i := range c.Windows {
		windowIn := &c.Windows[i]
		window := gpui.NewTestWindow(app, gpui.Size{Width: float32(windowIn.Width), Height: float32(windowIn.Height)})
		view := gpui.NewEntity(app, window.Scope(), func(v *counterView, cx *gpui.Context[counterView]) {
			v.model = model
			v.style = style
			cx.Observe(model, func(_ *counterView, _ gpui.Entity[counterModel], cx *gpui.Context[counterView]) {
				v.notified++
				cx.Notify()
			}).Detach()
		})
		window.SetRootView(gpui.ViewOf(view))
		if _, err := gpui.DrawWindowFrame(window); err != nil {
			return fmt.Errorf("opening window %q: %w", windowIn.Label, err)
		}
		windows = append(windows, &fixtureWindow{label: windowIn.Label, window: window, view: view, style: style})
	}

	// case-begin with the test window profile facts (the reference test
	// window's fixed scale 2.0 and default rem 16).
	rec.eventWith("case-begin", c.Label,
		field{"label", c.Label},
		field{"value", c.InitialValue},
		field{"remSize", conformance.F32Bits(gpui.DefaultRemSize)},
		field{"scale", conformance.F32Bits(gpui.TestScaleFactor)},
		field{"windowCount", uint64(0)},
	)
	rec.eventWith("entity-created", "model", field{"value", c.InitialValue})
	for i, window := range windows {
		windowIn := &c.Windows[i]
		rec.eventWith("window-opened", window.label,
			field{"width", conformance.F32Bits(float32(windowIn.Width))},
			field{"height", conformance.F32Bits(float32(windowIn.Height))},
			field{"windowCount", uint64(i + 1)},
		)
		rec.eventWith("view-created", window.label,
			field{"value", c.InitialValue},
			field{"renders", uint64(0)},
		)
	}

	// The initial capture: the windows drew at open.
	for _, window := range windows {
		if err := captureCounterWindow(app, window, model, rec, countText(c.InitialValue)); err != nil {
			return fmt.Errorf("initial capture of %q: %w", window.label, err)
		}
	}

	// The script.
	for i := range c.Script {
		op := &c.Script[i]
		switch op.Op {
		case conformance.OpIncrement:
			rec.eventWith("op-begin", op.Window,
				field{"op", "increment"},
				field{"clicks", uint64(op.Clicks)},
			)
			window, err := findWindow(windows, op.Window)
			if err != nil {
				return err
			}
			// The increments apply inside one update from the given
			// window (entity updates + notify; the notify coalesces
			// within the update, like the reference update_window).
			app.Update(func(app *gpui.App) {
				for click := uint32(0); click < op.Clicks; click++ {
					model.UpdateIn(app, window.window, func(m *counterModel, _ *gpui.Window, cx *gpui.Context[counterModel]) {
						m.value++
						cx.Notify()
					})
				}
			})
			// The flush-draw mirror: every open window whose root view
			// was notified since its last draw redraws (the reference
			// test-support app's automatic dirty-window redraw at the
			// update boundary).
			for _, open := range windows {
				_, notified := open.viewFacts(app)
				if notified > open.lastNotified {
					open.lastNotified = notified
					if _, err := gpui.DrawWindowFrame(open.window); err != nil {
						return fmt.Errorf("redrawing window %q: %w", open.label, err)
					}
				}
			}
			value := model.Read(app, func(m *counterModel, _ *gpui.App) uint64 { return m.value })
			for _, open := range windows {
				if err := captureCounterWindow(app, open, model, rec, countText(value)); err != nil {
					return fmt.Errorf("capture of %q: %w", open.label, err)
				}
			}
		case conformance.OpCloseWindow:
			rec.eventWith("op-begin", op.Window, field{"op", "close-window"})
			index, err := findWindowIndex(windows, op.Window)
			if err != nil {
				return err
			}
			// Closing removes the window: its scope closes (retiring the
			// view entity and its observer registration) and its element
			// runtime state drops (the reference remove_window path).
			gpui.CloseTestWindow(windows[index].window)
			windows = append(windows[:index], windows[index+1:]...)
			var count uint64
			app.Update(func(app *gpui.App) { count = uint64(len(app.Windows())) })
			rec.eventWith("window-closed", op.Window, field{"windowCount", count})
		default:
			return fmt.Errorf("unknown counter script op %q", op.Op)
		}
	}

	rec.event("case-end", c.Label)
	return nil
}

// findWindow resolves one open window by label.
func findWindow(windows []*fixtureWindow, label string) (*fixtureWindow, error) {
	for _, window := range windows {
		if window.label == label {
			return window, nil
		}
	}
	return nil, fmt.Errorf("unknown window %q", label)
}

// findWindowIndex resolves one open window's index by label.
func findWindowIndex(windows []*fixtureWindow, label string) (int, error) {
	for i, window := range windows {
		if window.label == label {
			return i, nil
		}
	}
	return 0, fmt.Errorf("unknown window %q", label)
}

// captureCounterWindow records one window's current drawn frame and
// lifecycle facts (the mirror of the reference capture_window).
func captureCounterWindow(app *gpui.App, fw *fixtureWindow, model gpui.Entity[counterModel], rec *recorder, text string) error {
	value := model.Read(app, func(m *counterModel, _ *gpui.App) uint64 { return m.value })
	renders, notified := fw.viewFacts(app)

	// The text oracle: shape the current count text through this
	// window's text system with the ambient text style of the label
	// (the default text style with the case's font size).
	textStyle, fontSize, lineHeight := counterTextFacts(fw.style)
	shaped, err := fw.window.ShapeText(text, textStyle, nil)
	if err != nil {
		return fmt.Errorf("shaping %q: %w", text, err)
	}
	rec.eventWith("text-shape", fw.label,
		field{"textLen", uint64(shaped.TextLen)},
		field{"fontSize", conformance.F32Bits(fontSize)},
		field{"lineHeight", conformance.F32Bits(lineHeight)},
		field{"width", conformance.F32Bits(shaped.Width)},
		field{"ascent", conformance.F32Bits(shaped.Ascent)},
		field{"descent", conformance.F32Bits(shaped.Descent)},
		field{"lineCount", uint64(shaped.LineCount)},
		field{"glyphCount", uint64(len(shaped.Glyphs))},
	)
	for index, glyph := range shaped.Glyphs {
		rec.eventWith("text-glyph", fw.label,
			field{"index", uint64(index)},
			field{"id", uint64(glyph.ID)},
			field{"x", conformance.F32Bits(glyph.X)},
			field{"y", conformance.F32Bits(glyph.Y)},
			field{"isEmoji", uint64(boolToU(glyph.IsEmoji))},
		)
	}

	// The primitive counts and the painted quads of the drawn frame.
	quads, mono, sub, poly, err := gpui.WindowPrimitiveCounts(fw.window)
	if err != nil {
		return fmt.Errorf("primitive counts: %w", err)
	}
	rec.eventWith("window-drawn", fw.label,
		field{"value", value},
		field{"renders", uint64(renders)},
		field{"notified", uint64(notified)},
		field{"quads", uint64(quads)},
		field{"monoSprites", uint64(mono)},
		field{"subpixelSprites", uint64(sub)},
		field{"polychromeSprites", uint64(poly)},
	)
	scene := gpui.LastDrawnScene(fw.window)
	painted, err := scene.Quads()
	if err != nil {
		return fmt.Errorf("quads: %w", err)
	}
	for _, quad := range painted {
		rec.eventWith("painted-quad", fw.label, counterQuadFields(quad)...)
	}

	// The layout facts: the debug-selector bounds of the root and label
	// divs.
	for _, selector := range []string{"root", "label"} {
		bounds, ok := gpui.WindowDebugBound(fw.window, selector)
		if !ok {
			rec.eventWith("debug-bounds", selector, field{"present", uint64(0)})
			continue
		}
		rec.eventWith("debug-bounds", selector,
			field{"present", uint64(1)},
			field{"x", conformance.F32Bits(bounds.Origin.X)},
			field{"y", conformance.F32Bits(bounds.Origin.Y)},
			field{"width", conformance.F32Bits(bounds.Size.Width)},
			field{"height", conformance.F32Bits(bounds.Size.Height)},
		)
	}
	return nil
}

// counterTextFacts derives the ambient text style of the label text and
// its resolved font size and line height (the reference derivation:
// font_size = text_style.font_size.to_pixels(rem); line_height =
// pixel_snap(text_style.line_height.to_pixels(font_size, rem))).
func counterTextFacts(style resolvedCounterStyle) (textStyle gpui.TextStyle, fontSize, lineHeight float32) {
	textStyle = gpui.DefaultTextStyle()
	textStyle.FontSize = style.fontSize
	rem := gpui.DefaultRemSize
	fontSize = textStyle.FontSizePixels(rem)
	lineHeight = gpui.PixelSnapValue(textStyle.LineHeightPixels(fontSize, rem), gpui.TestScaleFactor)
	return textStyle, fontSize, lineHeight
}

// counterQuadFields builds the painted-quad trace fields (the same
// field set the reference records from Window::painted_quads).
func counterQuadFields(quad gpui.Quad) []field {
	fields := []field{
		{"order", uint64(quad.Order)},
		{"boundsX", conformance.F32Bits(quad.Bounds.Origin.X)},
		{"boundsY", conformance.F32Bits(quad.Bounds.Origin.Y)},
		{"boundsW", conformance.F32Bits(quad.Bounds.Size.Width)},
		{"boundsH", conformance.F32Bits(quad.Bounds.Size.Height)},
		{"maskX", conformance.F32Bits(quad.ContentMask.Origin.X)},
		{"maskY", conformance.F32Bits(quad.ContentMask.Origin.Y)},
		{"maskW", conformance.F32Bits(quad.ContentMask.Size.Width)},
		{"maskH", conformance.F32Bits(quad.ContentMask.Size.Height)},
	}
	fields = append(fields, colorFields("background", quad.Background.Solid)...)
	fields = append(fields, colorFields("borderColor", quad.BorderColor.Solid)...)
	fields = append(fields, radiiFields("radius", quad.CornerRadii)...)
	fields = append(fields, widthsFields("border", quad.BorderWidths)...)
	borderStyle := "solid"
	if quad.BorderStyle == gpui.BorderStyleDashed {
		borderStyle = "dashed"
	}
	fields = append(fields,
		field{"borderStyle", borderStyle},
		field{"borderDashedLength", conformance.F32Bits(quad.DashedLength)},
		field{"borderDashedGap", conformance.F32Bits(quad.DashedGap)},
		field{"cornerSmoothing", conformance.F32Bits(quad.CornerSmoothing)},
		field{"padding", uint64(quad.Padding)},
	)
	return fields
}
