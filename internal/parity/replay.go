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
	if f.View == nil {
		return nil, fmt.Errorf("parity: fixture %q has no view", f.Name)
	}
	if err := validate(f); err != nil {
		return nil, err
	}
	ta := gpui.NewTestApp()
	t.Cleanup(func() { _ = ta.App().Shutdown() })
	window := gpui.NewTestWindow(ta.App(), gpui.Size{Width: f.Window.Width, Height: f.Window.Height})
	t.Cleanup(func() { gpui.CloseTestWindow(window) })
	if err := gpui.UseRealTextSystem(window); err != nil {
		return nil, fmt.Errorf("parity: %s: real text system: %w", f.Name, err)
	}

	log := &EventLog{}
	ta.Update(func(app *gpui.App) {
		root := gpui.NewEntity(app, window.Scope(), func(v *fixtureView, cx *gpui.Context[fixtureView]) {
			v.build, v.log = f.View, log
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
	if scale != DeviceScaleFactor {
		return nil, fmt.Errorf("parity: %s: the test window's scale factor is %v, want the harness's %v (keep the recorder and the test window in step)",
			f.Name, scale, DeviceScaleFactor)
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
	case StepKey:
		cx.SimulateKeystrokes(step.Key)
	default:
		return fmt.Errorf("unknown step kind %q", step.Kind)
	}
	return nil
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
