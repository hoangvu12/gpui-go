//go:build windows

// The replay half of the harness: run a fixture's gpui-go view in the
// hostless test window (gpui/testwindow.go) on the real text system,
// drive the scripted input through the simulated-input context
// (gpui/visualtest.go — the same dispatch the drag tests use), and
// collect the three observables the comparisons consume: element
// boxes from the last completed frame's debug bounds, the app-visible
// event transcript from the fixture's instrumented handlers, and a
// device-pixel capture of the final frame (capture.go).
package parity

import (
	"fmt"
	"image"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// DeviceScaleFactor is the device pixel ratio every fixture runs at:
// the gpui test window's fixed scale factor (gpui.TestScaleFactor)
// and the Chrome recording's forced device scale factor must both be
// this, or the two sides' device pixels are not comparable.
const DeviceScaleFactor float32 = 2

// Box is one element's box in logical (CSS-pixel) coordinates, the
// replay side of Chrome's getBoundingClientRect record.
type Box struct {
	X      float32 `json:"x"`
	Y      float32 `json:"y"`
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

func (b Box) String() string {
	return fmt.Sprintf("(%.4g, %.4g) %.4gx%.4g", b.X, b.Y, b.Width, b.Height)
}

// ReplayResult is everything the replay observed for one fixture.
type ReplayResult struct {
	// Viewport is the fixture's logical window size.
	Viewport Size
	// Scale is the test window's device pixel ratio (2.0).
	Scale float32
	// Boxes are the replayed element boxes, in logical pixels, keyed
	// by element id.
	Boxes map[string]Box
	// Events is the replayed app-visible event transcript.
	Events []EventRecord
	// Image is the final frame's device-pixel capture (opaque RGBA).
	Image *image.RGBA
}

// fixtureView is the test window's root view: the fixture's view
// function, re-run every frame like any Render (the openPerfWindow
// pattern; fixture View functions must rebuild their tree, not return
// a cached element).
type fixtureView struct {
	build func(*EventLog) gpui.AnyElement
	log   *EventLog
}

// Render implements Render.
func (v *fixtureView) Render(w *gpui.Window, cx *gpui.Context[fixtureView]) gpui.AnyElement {
	return v.build(v.log)
}

// Replay runs the fixture's view in the hostless test window on the
// real text system, dispatches the scripted input sequence through the
// simulated-input context, and captures the final frame's pixels
// through the offscreen render surface. Nothing is ever shown: the
// test window has no host, and the capture window is hidden and
// placed off screen.
//
// The real text system and the offscreen renderer need the native
// artifact; Replay fails with the underlying error when it is
// unavailable (the perf-benchmark and zterm-capture patterns).
func Replay(t testing.TB, f Fixture) (*ReplayResult, error) {
	if f.View == nil && f.ViewWithWindow == nil {
		return nil, fmt.Errorf("parity: fixture %q has no view", f.Name)
	}
	if err := validate(f); err != nil {
		return nil, err
	}
	ta := gpui.NewTestApp()
	t.Cleanup(func() { _ = ta.App().Shutdown() })
	app := ta.App()
	// The fixture's app-level configuration runs before the window and
	// the view, so the first frame already resolves styles against it
	// (the forced-colours pass's palette).
	if f.Configure != nil {
		if err := f.Configure(app); err != nil {
			return nil, fmt.Errorf("parity: %s: configuring the app: %w", f.Name, err)
		}
	}
	window := gpui.NewTestWindowAtScale(app, gpui.Size{Width: f.Window.Width, Height: f.Window.Height}, FixtureScale(f))
	t.Cleanup(func() { gpui.CloseTestWindow(window) })
	if err := gpui.UseRealTextSystem(window); err != nil {
		return nil, fmt.Errorf("parity: %s: real text system: %w", f.Name, err)
	}

	log := &EventLog{}
	build := f.View
	if build == nil {
		build = func(l *EventLog) gpui.AnyElement { return f.ViewWithWindow(l, window, app) }
	}
	ta.Update(func(app *gpui.App) {
		root := gpui.NewEntity(app, window.Scope(), func(v *fixtureView, cx *gpui.Context[fixtureView]) {
			v.build, v.log = build, log
		})
		window.SetRootView(gpui.ViewOf(root))
	})
	cx := gpui.NewVisualTestContext(window, ta)
	cx.RunUntilParked()
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		return nil, fmt.Errorf("parity: %s: initial draw: %w", f.Name, err)
	}

	for _, step := range ExpandSteps(f.Steps) {
		if err := applyStep(cx, log, step); err != nil {
			return nil, fmt.Errorf("parity: %s: step %v: %w", f.Name, step, err)
		}
	}
	cx.RunUntilParked()
	if _, err := gpui.DrawWindowFrame(window); err != nil {
		return nil, fmt.Errorf("parity: %s: final draw: %w", f.Name, err)
	}

	// Tier A inputs: the last completed frame's debug bounds.
	boxes := make(map[string]Box, len(f.BoxIDs))
	for _, id := range f.BoxIDs {
		b, ok := cx.DebugBounds(id)
		if !ok {
			return nil, fmt.Errorf("parity: %s: element %q has no bounds in the final frame", f.Name, id)
		}
		boxes[id] = Box{X: b.Origin.X, Y: b.Origin.Y, Width: b.Size.Width, Height: b.Size.Height}
	}

	// Tier C inputs: the final frame's scene, rendered offscreen.
	scale, err := windowScale(window)
	if err != nil {
		return nil, fmt.Errorf("parity: %s: %w", f.Name, err)
	}
	if scale != FixtureScale(f) {
		return nil, fmt.Errorf("parity: %s: the test window's scale factor is %v, want the fixture's %v (keep the recorder and the test window in step)",
			f.Name, scale, FixtureScale(f))
	}
	deviceWidth := uint32(f.Window.Width*scale + 0.5)
	deviceHeight := uint32(f.Window.Height*scale + 0.5)
	scene := gpui.LastDrawnScene(window)
	if scene == nil {
		return nil, fmt.Errorf("parity: %s: no drawn scene to capture", f.Name)
	}
	atlas, err := window.GlyphAtlas()
	if err != nil {
		return nil, fmt.Errorf("parity: %s: glyph atlas: %w", f.Name, err)
	}
	host, err := newCaptureHost(t)
	if err != nil {
		return nil, fmt.Errorf("parity: %s: %w", f.Name, err)
	}
	captured, err := host.renderScene(scene, atlas, deviceWidth, deviceHeight)
	if err != nil {
		return nil, fmt.Errorf("parity: %s: %w", f.Name, err)
	}

	return &ReplayResult{
		Viewport: f.Window,
		Scale:    scale,
		Boxes:    boxes,
		Events:   log.Events(),
		Image:    captured,
	}, nil
}

// windowScale reads the test window's device pixel ratio.
func windowScale(window *gpui.Window) (float32, error) {
	viewport, scale, _, err := gpui.TestWindowDrawGeometry(window)
	if err != nil {
		return 0, fmt.Errorf("test window geometry: %w", err)
	}
	if viewport.Width <= 0 || viewport.Height <= 0 {
		return 0, fmt.Errorf("test window viewport %v", viewport)
	}
	return scale, nil
}

// applyStep dispatches one scripted input step through the
// simulated-input context (one update per event, whose flush draws the
// dirty test window, so hit testing always sees the last frame). The
// dispatch's default-prevented result is marked onto the entries the
// event's handlers logged.
func applyStep(cx *gpui.VisualTestContext, log *EventLog, step Step) error {
	position := gpui.Point{X: step.X, Y: step.Y}
	button, err := mouseButton(step.Button)
	if err != nil {
		return err
	}
	switch step.Kind {
	case StepMove:
		cx.SimulateMouseMove(position, nil, gpui.NoModifiers())
	case StepDown:
		before := len(log.entries)
		result := cx.SimulateEvent(&gpui.MouseDownEvent{
			Position: position, Button: button, ClickCount: 1, FirstMouse: false,
		})
		log.markDefaultPrevented(before, result.DefaultPrevented)
	case StepUp:
		before := len(log.entries)
		result := cx.SimulateEvent(&gpui.MouseUpEvent{
			Position: position, Button: button, ClickCount: 1,
		})
		log.markDefaultPrevented(before, result.DefaultPrevented)
	case StepWait:
		// Advance the virtual clock by the step's milliseconds, then
		// deliver one animation frame: the transitions the click steps
		// started re-sample at the new clock (their frame requests are
		// the pending callbacks delivered here), the flush draws the
		// notified view, and the last drawn frame carries the state at
		// exactly now+ms — the replay side of the recorder's
		// virtual-time budget (browser-core ticket 44).
		cx.AdvanceClock(step.Ms)
		cx.Window().SimulateNextFrame(cx.App())
		cx.RunUntilParked()
	case StepKey:
		// One full key press — key down then key up, the recorder's
		// keyDown+keyUp pair — through the platform dispatch (bindings,
		// listeners and the core default actions of browser-core 09).
		// The default-prevented state is marked per half, matching the
		// recorder's per-event final state. The keystroke takes the
		// platform's spelling first (browser-core 13): the explicit code
		// of a Step.Code step (the numpad) or the US-derived one, the
		// location it implies, and the shifted-key fold with Shift kept —
		// the exact keystroke the Windows translation reports, and the
		// one the recorder's CDP events spell on the Chrome side.
		key, err := gpui.ParseKeystroke(step.Key)
		if err != nil {
			return err
		}
		if step.Code != "" {
			key.Code = step.Code
		}
		key = key.WithSimulatedCode()
		before := len(log.entries)
		result := cx.SimulateEvent(&gpui.KeyDownEvent{Keystroke: key})
		log.markDefaultPrevented(before, result.DefaultPrevented)
		before = len(log.entries)
		result = cx.SimulateEvent(&gpui.KeyUpEvent{Keystroke: key})
		log.markDefaultPrevented(before, result.DefaultPrevented)
	case StepKeyDown, StepKeyUp:
		// One half of a key press (browser-core ticket 12): the
		// StepKeyDown arm dispatches only the KeyDownEvent and StepKeyUp
		// only the KeyUpEvent, matching the recorder's single CDP event —
		// a key can be held at the record's settle (the Space-held
		// :active fixture) or released after something between the
		// halves. The keystroke spelling is StepKey's.
		key, err := gpui.ParseKeystroke(step.Key)
		if err != nil {
			return err
		}
		if step.Code != "" {
			key.Code = step.Code
		}
		key = key.WithSimulatedCode()
		event := any(&gpui.KeyUpEvent{Keystroke: key})
		if step.Kind == StepKeyDown {
			event = &gpui.KeyDownEvent{Keystroke: key}
		}
		before := len(log.entries)
		result := cx.SimulateEvent(event)
		log.markDefaultPrevented(before, result.DefaultPrevented)
	case StepImeSet, StepImeCommit:
		// A composition message (browser-core ticket 18): the replay's
		// simulated IME seam drives the ported composition driver
		// (gpui.Window.SimulateIMEComposition) with the same message
		// the recorder's Input.imeSetComposition / Input.insertText
		// produced the page's composition events from: the composition
		// string and cursor of an update (GCS_COMPSTR|GCS_CURSORPOS, the
		// cursor adjacent to ATTR_INPUT text so the IME's position is
		// used), or the result string of a commit (GCS_RESULTSTR).
		var msg gpui.IMECompositionMessage
		if step.Kind == StepImeSet {
			msg = gpui.IMECompositionMessage{
				Flags:       gpui.GCSCompStr | gpui.GCSCursorPos,
				Composition: step.Text,
				Cursor:      step.SelEnd,
				Attrs:       imeInputAttrs(step.Text),
			}
		} else {
			msg = gpui.IMECompositionMessage{
				Flags:  gpui.GCSResultStr,
				Result: step.Text,
			}
		}
		cx.Window().SimulateIMEComposition(msg, cx)
	case StepScript:
		// The Go half of a scripted state change (browser-core 46): run
		// the step's function on the application update path (the same
		// context a listener runs in) and park, so the state change's
		// refresh draws before the next step samples anything — the same
		// one-frame settle the recorder's script gets from its own
		// dispatch loop.
		cx.Update(func(w *gpui.Window, app *gpui.App) {
			step.Run(log, w, app)
		})
		cx.RunUntilParked()
	case StepWheel:
		// The wheel event (browser-core 15): one ScrollWheelEvent with a
		// pixel delta at the step's position, through the platform
		// dispatch (the same path a real Windows wheel takes after the
		// input translation), followed by the gesture's end — a
		// zero-delta phase-Ended event that clears the window's wheel
		// latch. The pairing mirrors the recorder's CDP wheel, which the
		// DevTools handler stamps kPhaseBegan (input_handler.cc:490),
		// making every CDP wheel a complete gesture of its own; without
		// the Ended half a run of Wheel steps would ride one 500ms latch
		// in the port while Chrome treats each as its own gesture. The
		// step's deltas are already in the platform's convention
		// (negative deltaY scrolls down).
		before := len(log.entries)
		result := cx.SimulateEvent(&gpui.ScrollWheelEvent{
			Position:   position,
			Delta:      gpui.ScrollDeltaPixels(step.DeltaX, step.DeltaY),
			TouchPhase: gpui.TouchPhaseMoved,
		})
		log.markDefaultPrevented(before, result.DefaultPrevented)
		// The gesture's end: the same shape the recorder's page sees when
		// the browser's latching transaction timer fires its synthetic
		// wheel-end (mouse_wheel_phase_handler.cc).
		cx.SimulateEvent(&gpui.ScrollWheelEvent{
			Position:   position,
			TouchPhase: gpui.TouchPhaseEnded,
		})
	default:
		return fmt.Errorf("unknown step kind %q", step.Kind)
	}
	return nil
}

// imeInputAttrs spells a composition string's GCS_COMPATTR bytes for
// the simulated update: every character unconverted (ATTR_INPUT), so
// the composition cursor position is used (the adjacency test).
func imeInputAttrs(text string) []byte {
	return make([]byte, len([]rune(text)))
}

// mouseButton maps the shared button name onto gpui's MouseButton.
func mouseButton(name string) (gpui.MouseButton, error) {
	switch name {
	case ButtonLeft, "":
		return gpui.MouseButtonLeft, nil
	case ButtonMiddle:
		return gpui.MouseButtonMiddle, nil
	case ButtonRight:
		return gpui.MouseButtonRight, nil
	default:
		return 0, fmt.Errorf("unknown mouse button %q", name)
	}
}

// ExpandSteps expands the click sugar (and validates): both the
// recorder and the replay dispatch the expanded sequence, so a click
// means exactly the same three events on both sides.
func ExpandSteps(steps []Step) []Step {
	out := make([]Step, 0, len(steps))
	for _, step := range steps {
		switch step.Kind {
		case StepClick:
			button := step.Button
			if button == "" {
				button = ButtonLeft
			}
			out = append(out,
				Step{Kind: StepMove, X: step.X, Y: step.Y},
				Step{Kind: StepDown, X: step.X, Y: step.Y, Button: button},
				Step{Kind: StepUp, X: step.X, Y: step.Y, Button: button},
			)
		default:
			out = append(out, step)
		}
	}
	return out
}
