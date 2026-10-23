package base

// Port of U:color_picker.rs: the color picker state, its HSLA sliders and
// hex field, the unstyled ColorPicker root and ColorSwatch.
//
// palette's Hsla / Rgba are the core's gpui.Hsla (hue as a 0..1 fraction)
// and gpui.Rgba. Rgba→Hsla (`Rgba::new(..).into_color()`) is the core's
// bit-exact RgbaToHsla over the packed bytes. Hsla→Rgba
// (`into_color()`, palette's clamping FromColor) and the clamping hsla()
// constructor (R:color.rs:49-60) are not exported by the core, so they
// are ported here (hslaToRgbaClamped, hslaClamped).

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
)

// colorPickerContext is the ColorPicker key context
// (U:color_picker.rs:21).
const colorPickerContext = "ColorPicker"

// colorHexPattern is the hex values the text field accepts while being
// typed (U:color_picker.rs:23-24).
const colorHexPattern = `^#[0-9a-fA-F]{0,8}$`

// colorPickerInit binds the picker keys (U:color_picker.rs:26-31).
func colorPickerInit(app *gpui.App) {
	ctx := colorPickerContext
	app.BindKeys(
		gpui.NewKeyBinding("enter", ConfirmAction.Box(Confirm{Secondary: false}), &ctx),
		gpui.NewKeyBinding("escape", CancelAction.Box(Cancel{}), &ctx),
	)
}

// parseHex parses #rgb, #rgba, #rrggbb and #rrggbbaa, with or without
// the # (U:color_picker.rs:33-67).
func parseHex(value string) (gpui.Hsla, bool) {
	value = strings.TrimPrefix(value, "#")
	// from_str_radix accepts a leading sign, so reject anything that is
	// not purely hexadecimal before slicing components out of it.
	for i := 0; i < len(value); i++ {
		if !isASCIIHexDigit(value[i]) {
			return gpui.Hsla{}, false
		}
	}
	var width int
	var hasAlpha bool
	switch len(value) {
	case 3:
		width, hasAlpha = 1, false
	case 4:
		width, hasAlpha = 1, true
	case 6:
		width, hasAlpha = 2, false
	case 8:
		width, hasAlpha = 2, true
	default:
		return gpui.Hsla{}, false
	}
	component := func(index int) uint32 {
		start := index * width
		raw, _ := strconv.ParseUint(value[start:start+width], 16, 8)
		// A single digit repeats itself rather than scaling, so #fff is
		// white.
		if width == 1 {
			raw *= 0x11
		}
		return uint32(raw)
	}
	alpha := uint32(0xFF)
	if hasAlpha {
		alpha = component(3)
	}
	// Rgba::new(byte / 255, ...).into_color(): RgbaToHsla divides the
	// packed bytes by 255 exactly the same way.
	return gpui.RgbaToHsla(component(0)<<24 | component(1)<<16 | component(2)<<8 | alpha), true
}

func isASCIIHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// hexString formats a color as #RRGGBB, or #RRGGBBAA when it is
// translucent (U:color_picker.rs:69-89).
func hexString(color gpui.Hsla) string {
	rgba := hslaToRgbaClamped(color)
	// `(value * 255.) as u32` saturates at zero.
	channel := func(v float32) uint32 {
		x := v * 255
		if !(x > 0) {
			return 0
		}
		return uint32(x)
	}
	if rgba.A < 1 {
		return fmt.Sprintf("#%02X%02X%02X%02X", channel(rgba.R), channel(rgba.G), channel(rgba.B), channel(rgba.A))
	}
	return fmt.Sprintf("#%02X%02X%02X", channel(rgba.R), channel(rgba.G), channel(rgba.B))
}

// hslaToRgbaClamped is palette 0.7's `Hsla::into_color::<Rgba>()`
// (hsla_to_rgba, R:color.rs:32-35): FromColorUnclamped<Hsl> for Rgb (the
// zone table over c = (1 - |2l - 1|)·s, h = hue / 60, m = l - c / 2,
// the same arithmetic as the core's unexported draw_text.go hslaToRgba)
// followed by the clamp of IntoColor.
func hslaToRgbaClamped(color gpui.Hsla) gpui.Rgba {
	degrees := color.H * 360
	c := (1 - float32(math.Abs(float64(2*color.L-1)))) * color.S
	h := degrees / 60
	hModTwo := h - float32(math.Floor(float64(h*0.5)))*2
	x := c * (1 - float32(math.Abs(float64(hModTwo-1))))
	m := color.L - c*0.5
	var r, g, b float32
	switch {
	case h >= 0 && h < 1:
		r, g, b = c, x, 0
	case h >= 1 && h < 2:
		r, g, b = x, c, 0
	case h >= 2 && h < 3:
		r, g, b = 0, c, x
	case h >= 3 && h < 4:
		r, g, b = 0, x, c
	case h >= 4 && h < 5:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return gpui.Rgba{R: clampUnit(r + m), G: clampUnit(g + m), B: clampUnit(b + m), A: clampUnit(color.A)}
}

// clampUnit is f32::clamp(0., 1.).
func clampUnit(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// hslaClamped is the core's hsla(h, s, l, a) constructor
// (R:color.rs:49-60): every component is clamped to 0..=1.
func hslaClamped(h, s, l, a float32) gpui.Hsla {
	return gpui.Hsla{H: clampUnit(h), S: clampUnit(s), L: clampUnit(l), A: clampUnit(a)}
}

// hueIntoDegrees is palette's RgbHue::into_degrees: the hue in
// (-180, 180] degrees.
func hueIntoDegrees(color gpui.Hsla) float32 {
	deg := color.H * 360
	for deg > 180 {
		deg -= 360
	}
	for deg <= -180 {
		deg += 360
	}
	return deg
}

// ColorPickerEvent reports that the committed color changed; Change is
// nil when the color was cleared (U:color_picker.rs:91-96).
type ColorPickerEvent struct {
	Change *gpui.Hsla
}

// ColorPickerEvents is the ColorPickerState event descriptor
// (U:color_picker.rs:445).
var ColorPickerEvents = gpui.DefineEvent[ColorPickerState, ColorPickerEvent]()

// HslaSliders are the four component sliders owned by a ColorPickerState
// (U:color_picker.rs:98-108). Applications render them with their own
// slider presentation; the picker keeps them in sync with the committed
// color.
type HslaSliders struct {
	hue, saturation, lightness, alpha gpui.Entity[SliderState]
}

// newHslaSliders is U:color_picker.rs:111-128.
func newHslaSliders(cx gpui.AppContext, scope *gpui.Scope) HslaSliders {
	component := func() gpui.Entity[SliderState] {
		return gpui.NewEntity(cx, scope, func(s *SliderState, _ *gpui.Context[SliderState]) {
			*s = NewSliderState().Min(0).Max(1).Step(0.01).DefaultValue(SliderValueSingle(0))
		})
	}
	return HslaSliders{hue: component(), saturation: component(), lightness: component(), alpha: component()}
}

// Hue is the hue slider, in 0..=1 (U:color_picker.rs:130-133).
func (s HslaSliders) Hue() gpui.Entity[SliderState] { return s.hue }

// Saturation is the saturation slider, in 0..=1 (U:color_picker.rs:135-138).
func (s HslaSliders) Saturation() gpui.Entity[SliderState] { return s.saturation }

// Lightness is the lightness slider, in 0..=1 (U:color_picker.rs:140-143).
func (s HslaSliders) Lightness() gpui.Entity[SliderState] { return s.lightness }

// Alpha is the alpha slider, in 0..=1 (U:color_picker.rs:145-148).
func (s HslaSliders) Alpha() gpui.Entity[SliderState] { return s.alpha }

// read is U:color_picker.rs:150-157.
func (s HslaSliders) read(app *gpui.App) gpui.Hsla {
	start := func(e gpui.Entity[SliderState]) float32 {
		return e.Read(app, func(s *SliderState, _ *gpui.App) float32 { return s.Value().Start() })
	}
	return hslaClamped(start(s.hue), start(s.saturation), start(s.lightness), start(s.alpha))
}

// write is U:color_picker.rs:159-169. The hue is written as
// into_degrees() / 360, which is negative past 180°, like upstream.
func (s HslaSliders) write(color gpui.Hsla, w *gpui.Window, app *gpui.App) {
	for _, c := range []struct {
		slider gpui.Entity[SliderState]
		value  float32
	}{
		{s.hue, hueIntoDegrees(color) / 360},
		{s.saturation, color.S},
		{s.lightness, color.L},
		{s.alpha, color.A},
	} {
		value := c.value
		c.slider.Update(app, func(sl *SliderState, cx *gpui.Context[SliderState]) {
			sl.SetValue(SliderValueSingle(value), w, cx)
		})
	}
}

// ColorPickerState is the state and interaction model for a color picker
// (U:color_picker.rs:172-191). It owns the committed color, the
// transient preview shown while the user hovers or edits, the controlled
// open state, the active panel, and the hex field and component sliders
// that stay in sync with all of them. The application owns the palette,
// popup, layout and every visual decision.
type ColorPickerState struct {
	focusHandle gpui.FocusHandle
	value       *gpui.Hsla
	preview     *gpui.Hsla
	open        bool
	activeTab   int
	hexInput    gpui.Entity[InputState]
	sliders     HslaSliders
	// needsSliderSync: a builder-supplied value cannot reach the sliders
	// without a Window, so the first render flushes it through
	// SyncPendingValue.
	needsSliderSync     bool
	suppressInputChange bool
	subscriptions       []gpui.Subscription
}

// NewColorPickerState creates an empty, closed picker
// (U:color_picker.rs:194-251).
func NewColorPickerState(w *gpui.Window, cx *gpui.Context[ColorPickerState]) ColorPickerState {
	pattern := regexp.MustCompile(colorHexPattern)
	hexInput := gpui.NewEntity(cx, cx.Scope(), func(s *InputState, icx *gpui.Context[InputState]) {
		*s = NewInputState(w, icx).Pattern(pattern)
	})
	sliders := newHslaSliders(cx, cx.Scope())

	subscriptions := []gpui.Subscription{
		InputStateEvents.SubscribeIn(cx, hexInput, w, func(this *ColorPickerState, input gpui.Entity[InputState], event *InputEvent, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
			switch event.Kind {
			case InputEventChange:
				if this.suppressInputChange {
					this.suppressInputChange = false
					return
				}
				value := input.Read(cx, func(s *InputState, _ *gpui.App) string { return s.Value() })
				if this.PreviewHex(value, w, cx) {
					this.sliders.write(*this.preview, w, cx.App())
				}
			case InputEventPressEnter:
				value := input.Read(cx, func(s *InputState, _ *gpui.App) string { return s.Value() })
				this.CommitHex(value, w, cx)
			}
		}),
	}
	for _, slider := range []gpui.Entity[SliderState]{sliders.hue, sliders.saturation, sliders.lightness, sliders.alpha} {
		subscriptions = append(subscriptions, SliderEvents.SubscribeIn(cx, slider, w, func(this *ColorPickerState, _ gpui.Entity[SliderState], _ *SliderEvent, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
			color := this.sliders.read(cx.App())
			this.updateValueFromSlider(color, w, cx)
		}))
	}

	return ColorPickerState{
		focusHandle:   cx.App().FocusHandle(),
		hexInput:      hexInput,
		sliders:       sliders,
		subscriptions: subscriptions,
	}
}

// DefaultValue sets the initial committed and previewed color
// (U:color_picker.rs:253-260).
func (s ColorPickerState) DefaultValue(value gpui.Hsla) ColorPickerState {
	s.value = &value
	preview := value
	s.preview = &preview
	s.needsSliderSync = true
	return s
}

func optionalColor(c *gpui.Hsla) (gpui.Hsla, bool) {
	if c == nil {
		return gpui.Hsla{}, false
	}
	return *c, true
}

func colorPtr(c gpui.Hsla) *gpui.Hsla { return &c }

func sameColor(a, b *gpui.Hsla) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Value returns the committed color (U:color_picker.rs:262-265).
func (s *ColorPickerState) Value() (gpui.Hsla, bool) { return optionalColor(s.value) }

// Preview returns the color currently being previewed
// (U:color_picker.rs:267-270).
func (s *ColorPickerState) Preview() (gpui.Hsla, bool) { return optionalColor(s.preview) }

// DisplayedColor returns the previewed color, falling back to the
// committed color (U:color_picker.rs:272-275).
func (s *ColorPickerState) DisplayedColor() (gpui.Hsla, bool) {
	if s.preview != nil {
		return *s.preview, true
	}
	return optionalColor(s.value)
}

// HexInput is the hex text field; render it with an application-owned
// input element (U:color_picker.rs:277-280).
func (s *ColorPickerState) HexInput() gpui.Entity[InputState] { return s.hexInput }

// Sliders are the HSLA component sliders (U:color_picker.rs:282-285).
func (s *ColorPickerState) Sliders() HslaSliders { return s.sliders }

// SetValue replaces the committed color without emitting a change
// (U:color_picker.rs:287-295).
func (s *ColorPickerState) SetValue(value gpui.Hsla, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
	s.updateValue(&value, false, w, cx)
}

// ClearValue clears the committed color without emitting a change
// (U:color_picker.rs:297-300).
func (s *ColorPickerState) ClearValue(w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
	s.updateValue(nil, false, w, cx)
}

// SyncPendingValue applies a value supplied to DefaultValue to the hex
// field and sliders. Call it from render; it is a no-op once nothing is
// pending (U:color_picker.rs:302-308).
func (s *ColorPickerState) SyncPendingValue(w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
	if s.needsSliderSync {
		s.updateValue(s.value, false, w, cx)
	}
}

// PreviewColor updates the transient preview color and the hex field
// that shows it (U:color_picker.rs:310-315).
func (s *ColorPickerState) PreviewColor(value gpui.Hsla, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
	s.preview = &value
	s.writeHexInput(&value, w, cx.App())
	cx.Notify()
}

// ClearPreview drops the transient preview, restoring the committed
// color (U:color_picker.rs:317-325).
func (s *ColorPickerState) ClearPreview(w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
	if sameColor(s.preview, s.value) {
		return
	}
	s.preview = s.value
	s.writeHexInput(s.value, w, cx.App())
	cx.Notify()
}

// PreviewHex parses a hex color into the transient preview
// (U:color_picker.rs:327-338). Invalid or incomplete input leaves the
// current preview unchanged. It does not write the hex field, so it is
// safe to call while typing in it.
func (s *ColorPickerState) PreviewHex(value string, _ *gpui.Window, cx *gpui.Context[ColorPickerState]) bool {
	color, ok := parseHex(value)
	if !ok {
		return false
	}
	s.preview = &color
	cx.Notify()
	return true
}

// CommitHex parses and commits a hex color, closing the picker on
// success (U:color_picker.rs:340-350).
func (s *ColorPickerState) CommitHex(value string, w *gpui.Window, cx *gpui.Context[ColorPickerState]) (gpui.Hsla, bool) {
	color, ok := parseHex(value)
	if !ok {
		return gpui.Hsla{}, false
	}
	s.SelectColor(color, w, cx)
	return color, true
}

// SelectColor commits a color and closes the picker, as a palette
// selection does (U:color_picker.rs:352-356).
func (s *ColorPickerState) SelectColor(value gpui.Hsla, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
	s.open = false
	s.updateValue(&value, true, w, cx)
}

// UpdateColor commits a color without changing the open state, as a
// slider drag does (U:color_picker.rs:358-361).
func (s *ColorPickerState) UpdateColor(value gpui.Hsla, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
	s.updateValue(&value, true, w, cx)
}

// SetOpen sets whether the picker popup is open
// (U:color_picker.rs:363-370).
func (s *ColorPickerState) SetOpen(open bool, cx *gpui.Context[ColorPickerState]) {
	if s.open == open {
		return
	}
	s.open = open
	cx.Notify()
}

// ToggleOpen toggles the picker popup (U:color_picker.rs:372-376).
func (s *ColorPickerState) ToggleOpen(cx *gpui.Context[ColorPickerState]) {
	s.open = !s.open
	cx.Notify()
}

// IsOpen is U:color_picker.rs:378-381.
func (s *ColorPickerState) IsOpen() bool { return s.open }

// SetActiveTab selects the application-defined picker panel
// (U:color_picker.rs:383-390).
func (s *ColorPickerState) SetActiveTab(tab int, cx *gpui.Context[ColorPickerState]) {
	if s.activeTab == tab {
		return
	}
	s.activeTab = tab
	cx.Notify()
}

// ActiveTab is U:color_picker.rs:392-395.
func (s *ColorPickerState) ActiveTab() int { return s.activeTab }

// updateValue is U:color_picker.rs:397-417.
func (s *ColorPickerState) updateValue(value *gpui.Hsla, emit bool, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
	if value != nil {
		value = colorPtr(*value)
	}
	s.needsSliderSync = false
	s.value = value
	s.preview = value
	s.writeHexInput(value, w, cx.App())
	// Drive the sliders from the full-precision color rather than letting
	// the hex round-trip do it.
	if value != nil {
		s.sliders.write(*value, w, cx.App())
	}
	if emit {
		ColorPickerEvents.Emit(cx, ColorPickerEvent{Change: value})
	}
	cx.Notify()
}

// updateValueFromSlider writes the hex field but leaves the sliders
// alone: they are the source of this change and rewriting them would
// fight the drag in progress (U:color_picker.rs:419-433).
func (s *ColorPickerState) updateValueFromSlider(value gpui.Hsla, w *gpui.Window, cx *gpui.Context[ColorPickerState]) {
	s.needsSliderSync = false
	s.value = &value
	s.preview = &value
	s.writeHexInput(&value, w, cx.App())
	ColorPickerEvents.Emit(cx, ColorPickerEvent{Change: colorPtr(value)})
	cx.Notify()
}

// writeHexInput writes the hex field, suppressing the change it would
// otherwise feed back through Hsla → hex → Hsla and lose precision to
// (U:color_picker.rs:435-442).
func (s *ColorPickerState) writeHexInput(value *gpui.Hsla, w *gpui.Window, app *gpui.App) {
	s.suppressInputChange = true
	text := ""
	if value != nil {
		text = hexString(*value)
	}
	s.hexInput.Update(app, func(input *InputState, cx *gpui.Context[InputState]) { input.SetValue(text, w, cx) })
}

// FocusHandle implements Focusable (U:color_picker.rs:447-451).
func (s *ColorPickerState) FocusHandle(*gpui.App) gpui.FocusHandle { return s.focusHandle }

// Render renders the hex field (U:color_picker.rs:453-457).
func (s *ColorPickerState) Render(*gpui.Window, *gpui.Context[ColorPickerState]) gpui.AnyElement {
	element, err := gpui.TryIntoElement(gpui.ViewOf[InputState](s.hexInput))
	if err != nil {
		panic("base: ColorPickerState render: " + err.Error())
	}
	return element
}

// ColorPicker is an unstyled controlled color-picker root
// (U:color_picker.rs:459-478). Applications own the trigger, palette,
// popup and every visual decision; this root owns focus, accessibility
// semantics, and the keyboard behavior that opens and dismisses the
// picker. Its interactivity is the inner div's.
type ColorPicker struct {
	gpui.Component[*ColorPicker]

	base               *gpui.DivElement
	open               bool
	disabled           bool
	focusHandle        *gpui.FocusHandle
	accessibilityLabel *string
	onOpenChange       OpenChangeHandler
	keyContext         string
	role               RoleOverride
}

// NewColorPicker is ColorPicker::new (U:color_picker.rs:480-494).
func NewColorPicker[I gpui.IntoElementID](id I) *ColorPicker {
	// Gap 2b: the plain upstream div() is a block.
	base := gpui.Div().ID(gpui.ElementIDOf(id)).Block()
	return gpui.BindComponent(&ColorPicker{
		base:       base,
		keyContext: colorPickerContext,
		role:       RoleOverrideImplicit(),
	}, base)
}

// Open sets the application-controlled open state
// (U:color_picker.rs:496-500).
func (p *ColorPicker) Open(open bool) *ColorPicker {
	p.open = open
	return p
}

// Disabled prevents keyboard interaction and removes the trigger from tab
// traversal (U:color_picker.rs:502-506).
func (p *ColorPicker) Disabled(disabled bool) *ColorPicker {
	p.disabled = disabled
	return p
}

// TrackFocus supplies the focus handle for the picker trigger
// (U:color_picker.rs:508-512).
func (p *ColorPicker) TrackFocus(handle gpui.FocusHandle) *ColorPicker {
	p.focusHandle = &handle
	return p
}

// AccessibilityLabel sets the accessible name exposed by the controlled
// root (U:color_picker.rs:514-518).
func (p *ColorPicker) AccessibilityLabel(label string) *ColorPicker {
	p.accessibilityLabel = &label
	return p
}

// Role overrides the accessibility role; the default is RoleButton
// (U:color_picker.rs:520-524).
func (p *ColorPicker) Role(role RoleOverride) *ColorPicker {
	p.role = role
	return p
}

// OnOpenChange handles requests to update the controlled open state:
// Confirm toggles the picker and Cancel dismisses an open one
// (U:color_picker.rs:526-535).
func (p *ColorPicker) OnOpenChange(handler OpenChangeHandler) *ColorPicker {
	p.onOpenChange = handler
	return p
}

// KeyContext overrides the key context (U:color_picker.rs:537-541,
// #[doc(hidden)]).
func (p *ColorPicker) KeyContext(keyContext string) *ColorPicker {
	p.keyContext = keyContext
	return p
}

// RenderOnce implements gpui.RenderOnce (U:color_picker.rs:564-608).
func (p *ColorPicker) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return p.render(w, app).IntoElement()
}

func (p *ColorPicker) render(*gpui.Window, *gpui.App) *gpui.DivElement {
	open := p.open
	disabled := p.disabled
	handler := p.onOpenChange

	d := p.base
	if role, ok := p.role.Resolve(func() gpui.Role { return gpui.RoleButton }); ok {
		d.Role(role)
	}
	d.AriaExpanded(open)
	if p.accessibilityLabel != nil {
		d.AriaLabel(*p.accessibilityLabel)
	}
	d.KeyContext(p.keyContext)
	if p.focusHandle != nil && !disabled {
		d.TrackFocus(p.focusHandle.TabStop(true))
	}
	d.OnAction(ConfirmAction, func(_ *Confirm, w *gpui.Window, app *gpui.App) {
		if disabled {
			app.Propagate()
			return
		}
		if handler != nil {
			handler(!open, w, app)
		}
	}).OnAction(CancelAction, func(_ *Cancel, w *gpui.Window, app *gpui.App) {
		if !open {
			app.Propagate()
			return
		}
		app.StopPropagation()
		if handler != nil {
			handler(false, w, app)
		}
	})
	d.Extend(p.TakeChildren())
	style := p.Style()
	return StyledExtOf(d).RefineStyle(&style)
}

// SwatchClickHandler handles a swatch activation with its color
// (U:color_picker.rs:610).
type SwatchClickHandler = func(color gpui.Hsla, event *gpui.ClickEvent, w *gpui.Window, app *gpui.App)

// SwatchHoverHandler handles a swatch hover enter or exit with its color
// (U:color_picker.rs:611).
type SwatchHoverHandler = func(color gpui.Hsla, entered bool, w *gpui.Window, app *gpui.App)

// ColorSwatch is an unstyled selectable color in a picker's palette
// (U:color_picker.rs:613-633). The application paints the color; this
// part carries the radio semantics, the accessible hex name, and the
// hover and activation callbacks a picker uses to preview and commit a
// color. Its interactivity is the inner div's.
type ColorSwatch struct {
	gpui.Component[*ColorSwatch]

	id                 gpui.ElementID
	base               *gpui.DivElement
	color              gpui.Hsla
	selected           bool
	disabled           bool
	accessibilityLabel *string
	onClick            SwatchClickHandler
	onHover            SwatchHoverHandler
	tabIndex           int64
	tabStop            bool
	role               RoleOverride
}

// NewColorSwatch is ColorSwatch::new (U:color_picker.rs:636-653).
func NewColorSwatch[I gpui.IntoElementID](id I, color gpui.Hsla) *ColorSwatch {
	eid := gpui.ElementIDOf(id)
	// Gap 2b: the plain upstream div() is a block.
	base := gpui.Div().ID(eid).Block()
	return gpui.BindComponent(&ColorSwatch{
		id:      eid,
		base:    base,
		color:   color,
		tabStop: true,
		role:    RoleOverrideImplicit(),
	}, base)
}

// Color is the color this swatch represents (U:color_picker.rs:655-658).
func (s *ColorSwatch) Color() gpui.Hsla { return s.color }

// Selected marks this swatch as the picker's current color
// (U:color_picker.rs:660-664).
func (s *ColorSwatch) Selected(selected bool) *ColorSwatch {
	s.selected = selected
	return s
}

// Disabled sets whether pointer and keyboard activation are ignored
// (U:color_picker.rs:666-670).
func (s *ColorSwatch) Disabled(disabled bool) *ColorSwatch {
	s.disabled = disabled
	return s
}

// AccessibilityLabel overrides the accessible name; the default is the
// color's hex value (U:color_picker.rs:672-676).
func (s *ColorSwatch) AccessibilityLabel(label string) *ColorSwatch {
	s.accessibilityLabel = &label
	return s
}

// Role overrides the accessibility role; the default is RoleRadioButton
// (U:color_picker.rs:678-682).
func (s *ColorSwatch) Role(role RoleOverride) *ColorSwatch {
	s.role = role
	return s
}

// OnClick handles activation with the swatch's color
// (U:color_picker.rs:684-691).
func (s *ColorSwatch) OnClick(handler SwatchClickHandler) *ColorSwatch {
	s.onClick = handler
	return s
}

// OnHover handles hover enter and exit with the swatch's color
// (U:color_picker.rs:693-700).
func (s *ColorSwatch) OnHover(handler SwatchHoverHandler) *ColorSwatch {
	s.onHover = handler
	return s
}

// TabIndex sets the focus traversal index; the default is 0
// (U:color_picker.rs:702-706).
func (s *ColorSwatch) TabIndex(tabIndex int64) *ColorSwatch {
	s.tabIndex = tabIndex
	return s
}

// TabStop sets whether this swatch participates in keyboard focus
// traversal (U:color_picker.rs:708-712).
func (s *ColorSwatch) TabStop(tabStop bool) *ColorSwatch {
	s.tabStop = tabStop
	return s
}

// RenderOnce implements gpui.RenderOnce (U:color_picker.rs:735-783).
func (s *ColorSwatch) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return s.render(w, app).IntoElement()
}

func (s *ColorSwatch) render(w *gpui.Window, app *gpui.App) *gpui.DivElement {
	color := s.color
	disabled := s.disabled
	selected := s.selected
	label := hexString(color)
	if s.accessibilityLabel != nil {
		label = *s.accessibilityLabel
	}
	focusHandle := keyedFocusHandle(nil, s.id, w, app)

	d := s.base
	if role, ok := s.role.Resolve(func() gpui.Role { return gpui.RoleRadioButton }); ok {
		d.Role(role)
	}
	toggled := gpui.ToggledFalse
	if selected {
		toggled = gpui.ToggledTrue
	}
	// A palette color is both "toggled" and "selected"; assistive
	// technology reads one or the other, so state both.
	d.AriaLabel(label).AriaToggled(toggled).AriaSelected(selected)
	if !disabled {
		d.TrackFocus(focusHandle.TabIndex(s.tabIndex).TabStop(s.tabStop))
		if onHover := s.onHover; onHover != nil {
			d.OnHover(func(entered *bool, w *gpui.Window, app *gpui.App) { onHover(color, *entered, w, app) })
		}
		if onClick := s.onClick; onClick != nil {
			d.OnClick(func(event *gpui.ClickEvent, w *gpui.Window, app *gpui.App) { onClick(color, event, w, app) })
		}
	}
	d.Extend(s.TakeChildren())
	style := s.Style()
	return StyledExtOf(d).RefineStyle(&style)
}
