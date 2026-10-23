package base

import (
	"math"
	"slices"
	"testing"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Ported tests of U:color_picker.rs (10/10).

// U:color_picker.rs:791-803
func TestColorPickerParsesEverySupportedHexWidth(t *testing.T) {
	white, ok := parseHex("#fff")
	if !ok {
		t.Fatal("#fff did not parse")
	}
	if got := hexString(white); got != "#FFFFFF" {
		t.Fatalf("hex = %q, want #FFFFFF", got)
	}
	if got, ok := parseHex("ffffff"); !ok || got != white {
		t.Fatalf("ffffff = %v, %v; want %v", got, ok, white)
	}
	half, ok := parseHex("#ff000080")
	if !ok || math.Abs(float64(half.A-0.5)) >= 0.01 {
		t.Fatalf("#ff000080 = %v, %v", half, ok)
	}
	if got, ok := parseHex("#f008"); !ok || got.H != half.H {
		t.Fatalf("#f008 hue = %v, want %v", got.H, half.H)
	}
}

// U:color_picker.rs:805-810
func TestColorPickerRejectsMalformedHex(t *testing.T) {
	for _, value := range []string{"#nope", "#12", "#1234567", "", "#+f0000", "#-fffff"} {
		if _, ok := parseHex(value); ok {
			t.Fatalf("%q should not parse", value)
		}
	}
}

// U:color_picker.rs:812-816
func TestColorPickerFormatsAlphaOnlyWhenTranslucent(t *testing.T) {
	if got := hexString(hslaClamped(0, 1, 0.5, 1)); got != "#FF0000" {
		t.Fatalf("opaque = %q", got)
	}
	if got := hexString(hslaClamped(0, 1, 0.5, 0.5)); got != "#FF00007F" {
		t.Fatalf("translucent = %q", got)
	}
}

// colorPickerView opens a ColorPickerState as a window view and draws it
// (U:color_picker.rs:818-822 state).
func colorPickerView(t *testing.T, build func(ColorPickerState) ColorPickerState) (*gpui.VisualTestContext, gpui.Entity[ColorPickerState]) {
	t.Helper()
	ta := gpui.NewTestApp()
	ta.Update(initWithTheme)
	state, cx := ta.AddWindowView(func(s *ColorPickerState, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
		*s = build(NewColorPickerState(w, cx))
	})
	cx.Update(func(w *gpui.Window, _ *gpui.App) {
		if _, err := gpui.DrawWindowFrame(w); err != nil {
			t.Fatal(err)
		}
	})
	return cx, state
}

func identityPicker(s ColorPickerState) ColorPickerState { return s }

func updatePicker(cx *gpui.VisualTestContext, state gpui.Entity[ColorPickerState], f func(*ColorPickerState, *gpui.Window, *gpui.Context[ColorPickerState])) {
	cx.Update(func(w *gpui.Window, app *gpui.App) { _ = state.UpdateIn(app, w, f) })
}

func readPicker[R any](cx *gpui.VisualTestContext, state gpui.Entity[ColorPickerState], f func(*ColorPickerState, *gpui.App) R) R {
	var r R
	cx.Update(func(_ *gpui.Window, app *gpui.App) { r = state.Read(app, f) })
	return r
}

func someColor(c gpui.Hsla, ok bool) *gpui.Hsla {
	if !ok {
		return nil
	}
	return &c
}

// U:color_picker.rs:824-837
func TestColorPickerDefaultValueReachesTheHexFieldAndSliders(t *testing.T) {
	cx, state := colorPickerView(t, func(s ColorPickerState) ColorPickerState {
		return s.DefaultValue(hslaClamped(0, 1, 0.5, 1))
	})
	updatePicker(cx, state, func(s *ColorPickerState, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
		s.SyncPendingValue(w, cx)
	})
	hex := readPicker(cx, state, func(s *ColorPickerState, app *gpui.App) string {
		return s.HexInput().Read(app, func(i *InputState, _ *gpui.App) string { return i.Value() })
	})
	if hex != "#FF0000" {
		t.Fatalf("hex field = %q, want #FF0000", hex)
	}
	lightness := readPicker(cx, state, func(s *ColorPickerState, app *gpui.App) float32 {
		return s.Sliders().Lightness().Read(app, func(sl *SliderState, _ *gpui.App) float32 { return sl.Value().Start() })
	})
	if lightness != 0.5 {
		t.Fatalf("lightness slider = %v, want 0.5", lightness)
	}
}

// U:color_picker.rs:839-863
func TestColorPickerPreviewDoesNotChangeTheCommittedValue(t *testing.T) {
	cx, state := colorPickerView(t, identityPicker)
	committed := hslaClamped(0.1, 0.2, 0.3, 1)
	previewed := hslaClamped(0.6, 0.7, 0.8, 1)
	updatePicker(cx, state, func(s *ColorPickerState, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
		s.SetValue(committed, w, cx)
		s.PreviewColor(previewed, w, cx)
	})
	if v := readPicker(cx, state, func(s *ColorPickerState, _ *gpui.App) *gpui.Hsla { return someColor(s.Value()) }); v == nil || *v != committed {
		t.Fatalf("value = %v, want %v", v, committed)
	}
	if v := readPicker(cx, state, func(s *ColorPickerState, _ *gpui.App) *gpui.Hsla { return someColor(s.DisplayedColor()) }); v == nil || *v != previewed {
		t.Fatalf("displayed = %v, want %v", v, previewed)
	}
	updatePicker(cx, state, func(s *ColorPickerState, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
		s.ClearPreview(w, cx)
	})
	if v := readPicker(cx, state, func(s *ColorPickerState, _ *gpui.App) *gpui.Hsla { return someColor(s.DisplayedColor()) }); v == nil || *v != committed {
		t.Fatalf("displayed after clear = %v, want %v", v, committed)
	}
}

// U:color_picker.rs:865-882
func TestColorPickerInvalidHexLeavesPreviewAndValueAlone(t *testing.T) {
	cx, state := colorPickerView(t, identityPicker)
	color := hslaClamped(0.1, 0.2, 0.3, 1)
	updatePicker(cx, state, func(s *ColorPickerState, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
		s.SetValue(color, w, cx)
		if s.PreviewHex("#nope", w, cx) {
			t.Fatal("#nope previewed")
		}
		if _, ok := s.CommitHex("#12", w, cx); ok {
			t.Fatal("#12 committed")
		}
	})
	if v := readPicker(cx, state, func(s *ColorPickerState, _ *gpui.App) *gpui.Hsla { return someColor(s.Preview()) }); v == nil || *v != color {
		t.Fatalf("preview = %v", v)
	}
	if v := readPicker(cx, state, func(s *ColorPickerState, _ *gpui.App) *gpui.Hsla { return someColor(s.Value()) }); v == nil || *v != color {
		t.Fatalf("value = %v", v)
	}
}

// colorObserver subscribes to a picker from outside it (the upstream
// App-level cx.subscribe).
type colorObserver struct{}

// U:color_picker.rs:884-911
func TestColorPickerPaletteSelectionClosesButASliderUpdateStaysOpen(t *testing.T) {
	cx, state := colorPickerView(t, identityPicker)
	var changes []*gpui.Hsla
	cx.Update(func(_ *gpui.Window, app *gpui.App) {
		observer := gpui.NewEntity(app, cx.RootScope(), func(*colorObserver, *gpui.Context[colorObserver]) {})
		observer.Update(app, func(_ *colorObserver, ocx *gpui.Context[colorObserver]) {
			ColorPickerEvents.Subscribe(ocx, state, func(_ *colorObserver, _ gpui.Entity[ColorPickerState], e *ColorPickerEvent, _ *gpui.Context[colorObserver]) {
				changes = append(changes, e.Change)
			})
		})
	})
	updatePicker(cx, state, func(s *ColorPickerState, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
		s.SetOpen(true, cx)
		s.UpdateColor(hslaClamped(0.2, 0.3, 0.4, 1), w, cx)
	})
	if !readPicker(cx, state, func(s *ColorPickerState, _ *gpui.App) bool { return s.IsOpen() }) {
		t.Fatal("a slider update closed the picker")
	}
	updatePicker(cx, state, func(s *ColorPickerState, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
		s.SelectColor(hslaClamped(0.5, 0.6, 0.7, 1), w, cx)
	})
	if readPicker(cx, state, func(s *ColorPickerState, _ *gpui.App) bool { return s.IsOpen() }) {
		t.Fatal("a palette selection left the picker open")
	}
	cx.RunUntilParked()
	if len(changes) != 2 {
		t.Fatalf("changes = %d, want 2", len(changes))
	}
}

// U:color_picker.rs:913-929
func TestColorPickerCommittingHexUpdatesTheValueAndCloses(t *testing.T) {
	cx, state := colorPickerView(t, identityPicker)
	var committed *gpui.Hsla
	updatePicker(cx, state, func(s *ColorPickerState, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
		s.SetOpen(true, cx)
		committed = someColor(s.CommitHex("#ff0000", w, cx))
	})
	if committed == nil {
		t.Fatal("#ff0000 did not commit")
	}
	if v := readPicker(cx, state, func(s *ColorPickerState, _ *gpui.App) *gpui.Hsla { return someColor(s.Value()) }); v == nil || *v != *committed {
		t.Fatalf("value = %v, want %v", v, *committed)
	}
	if v := readPicker(cx, state, func(s *ColorPickerState, _ *gpui.App) *gpui.Hsla { return someColor(s.Preview()) }); v == nil || *v != *committed {
		t.Fatalf("preview = %v, want %v", v, *committed)
	}
	if readPicker(cx, state, func(s *ColorPickerState, _ *gpui.App) bool { return s.IsOpen() }) {
		t.Fatal("committing left the picker open")
	}
}

// U:color_picker.rs:931-946
func TestColorPickerOpenAndActivePanelAreControlled(t *testing.T) {
	cx, state := colorPickerView(t, identityPicker)
	updatePicker(cx, state, func(s *ColorPickerState, _ *gpui.Window, cx *gpui.Context[ColorPickerState]) {
		s.ToggleOpen(cx)
		s.SetActiveTab(1, cx)
	})
	open, tab := readPicker(cx, state, func(s *ColorPickerState, _ *gpui.App) bool { return s.IsOpen() }),
		readPicker(cx, state, func(s *ColorPickerState, _ *gpui.App) int { return s.ActiveTab() })
	if !open || tab != 1 {
		t.Fatalf("open = %v, tab = %d", open, tab)
	}
}

// pickerHarness is U:color_picker.rs:948-970 PickerHarness.
type pickerHarness struct {
	open        bool
	focusHandle gpui.FocusHandle
	changes     *[]bool
}

func (h *pickerHarness) Render(_ *gpui.Window, cx *gpui.Context[pickerHarness]) gpui.AnyElement {
	entity := cx.Entity().Downgrade()
	changes := h.changes
	return gpui.OwnRecipe(NewColorPicker("picker").
		Open(h.open).
		TrackFocus(h.focusHandle).
		Size(gpui.Px(20)).
		OnOpenChange(func(open bool, _ *gpui.Window, app *gpui.App) {
			*changes = append(*changes, open)
			_ = entity.TryUpdate(app, func(h *pickerHarness, cx *gpui.Context[pickerHarness]) {
				h.open = open
				cx.Notify()
			})
		}))
}

// U:color_picker.rs:972-998
func TestColorPickerConfirmTogglesAndCancelDismisses(t *testing.T) {
	ta := gpui.NewTestApp()
	ta.Update(Init)
	changes := new([]bool)
	state, cx := ta.AddWindowView(func(h *pickerHarness, _ *gpui.Window, cx *gpui.Context[pickerHarness]) {
		h.focusHandle = cx.App().FocusHandle()
		h.changes = changes
	})
	cx.Update(func(w *gpui.Window, app *gpui.App) {
		state.Read(app, func(h *pickerHarness, _ *gpui.App) struct{} { h.focusHandle.Focus(w); return struct{}{} })
		if _, err := gpui.DrawWindowFrame(w); err != nil {
			t.Fatal(err)
		}
	})
	isOpen := func() bool { return state.Read(ta, func(h *pickerHarness, _ *gpui.App) bool { return h.open }) }

	cx.SimulateKeystrokes("enter")
	if !isOpen() {
		t.Fatal("enter did not open the picker")
	}
	cx.SimulateKeystrokes("enter")
	if isOpen() {
		t.Fatal("enter did not close the picker")
	}
	cx.SimulateKeystrokes("enter escape")
	if isOpen() {
		t.Fatal("escape did not dismiss the picker")
	}
	if !slices.Equal(*changes, []bool{true, false, true, false}) {
		t.Fatalf("changes = %v, want [true false true false]", *changes)
	}
}

// Go-own: a swatch carries the radio semantics, its hex name and both
// selection states (U:color_picker.rs:735-783).
func TestColorSwatchProjectsRadioSemantics(t *testing.T) {
	red := hslaClamped(0, 1, 0.5, 1)
	got := a11yOf(t,
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewColorSwatch("red", red).Selected(true).render(w, app)
		},
		func(w *gpui.Window, app *gpui.App) *gpui.DivElement {
			return NewColorPicker("picker").Open(true).AccessibilityLabel("Color").render(w, app)
		},
	)
	swatch, picker := got[0], got[1]
	if swatch.role != gpui.RoleRadioButton || swatch.spec.Label != "#FF0000" {
		t.Fatalf("swatch role = %v, label = %q", swatch.role, swatch.spec.Label)
	}
	if v, ok := swatch.spec.Selected.Get(); !ok || !v {
		t.Fatal("selected swatch is not selected")
	}
	if v, ok := swatch.spec.Toggled.Get(); !ok || v != gpui.ToggledTrue {
		t.Fatal("selected swatch is not toggled")
	}
	if picker.role != gpui.RoleButton || picker.spec.Label != "Color" {
		t.Fatalf("picker role = %v, label = %q", picker.role, picker.spec.Label)
	}
	if v, ok := picker.spec.Expanded.Get(); !ok || !v {
		t.Fatal("open picker is not expanded")
	}
}
