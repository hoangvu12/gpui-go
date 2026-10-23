package base

// Port of U:otp_input.rs: the fixed-length numeric one-time-code state and
// its unstyled interaction root.

import (
	"unicode/utf8"

	"github.com/hoangvu12/gpui-go/gpui"
)

// OtpEvent is a semantic notification from a one-time-code state
// (U:otp_input.rs:8-17).
type OtpEvent uint8

const (
	// OtpEventChange reports a value change through keyboard editing.
	OtpEventChange OtpEvent = iota
	// OtpEventComplete reports that keyboard editing filled the final cell.
	OtpEventComplete
	// OtpEventFocus reports the state gaining focus.
	OtpEventFocus
	// OtpEventBlur reports the state losing focus.
	OtpEventBlur
)

// OtpEvents is the OtpState event descriptor
// (`impl EventEmitter<OtpEvent> for OtpState`, U:otp_input.rs:156).
var OtpEvents = gpui.DefineEvent[OtpState, OtpEvent]()

// OtpState is the stateful input and focus behavior for a fixed-length
// numeric one-time code (U:otp_input.rs:19-27).
type OtpState struct {
	focusHandle   gpui.FocusHandle
	value         string
	blinkCursor   gpui.Entity[blinkCursor]
	masked        bool
	length        int
	subscriptions []gpui.Subscription
}

// NewOtpState is OtpState::new (U:otp_input.rs:30-51).
func NewOtpState(length int, w *gpui.Window, cx *gpui.Context[OtpState]) OtpState {
	focusHandle := cx.App().FocusHandle()
	cursor := gpui.NewEntity(cx, cx.Scope(), func(b *blinkCursor, _ *gpui.Context[blinkCursor]) {
		*b = newBlinkCursor()
	})
	subscriptions := []gpui.Subscription{
		cx.Observe(cursor, func(_ *OtpState, _ gpui.Entity[blinkCursor], cx *gpui.Context[OtpState]) { cx.Notify() }),
		cx.ObserveWindowActivation(w, func(s *OtpState, w *gpui.Window, cx *gpui.Context[OtpState]) {
			if windowIsActive(w) && s.focusHandle.IsFocused(w) {
				s.blinkCursor.Update(cx, func(b *blinkCursor, bcx *gpui.Context[blinkCursor]) { b.start(bcx) })
			}
		}),
		cx.OnFocus(focusHandle, w, func(s *OtpState, w *gpui.Window, cx *gpui.Context[OtpState]) { s.onFocus(w, cx) }),
		cx.OnBlur(focusHandle, w, func(s *OtpState, w *gpui.Window, cx *gpui.Context[OtpState]) { s.onBlur(w, cx) }),
	}
	return OtpState{
		focusHandle:   focusHandle,
		blinkCursor:   cursor,
		length:        length,
		subscriptions: subscriptions,
	}
}

// DefaultValue is U:otp_input.rs:53-56.
func (s OtpState) DefaultValue(value string) OtpState {
	s.value = value
	return s
}

// SetValue is U:otp_input.rs:58-66. Programmatic values are not filtered.
func (s *OtpState) SetValue(value string, _ *gpui.Window, cx *gpui.Context[OtpState]) {
	s.value = value
	cx.Notify()
}

// Value is U:otp_input.rs:68-70.
func (s *OtpState) Value() string { return s.value }

// Len is the code length (U:otp_input.rs:71-73).
func (s *OtpState) Len() int { return s.length }

// IsEmpty reports an empty value (U:otp_input.rs:74-76).
func (s *OtpState) IsEmpty() bool { return s.value == "" }

// IsMasked is U:otp_input.rs:77-79.
func (s *OtpState) IsMasked() bool { return s.masked }

// CursorVisible is U:otp_input.rs:80-82.
func (s *OtpState) CursorVisible(app *gpui.App) bool {
	return s.blinkCursor.Read(app, func(b *blinkCursor, _ *gpui.App) bool { return b.isVisible() })
}

// Masked is U:otp_input.rs:83-86.
func (s OtpState) Masked(masked bool) OtpState {
	s.masked = masked
	return s
}

// SetMasked is U:otp_input.rs:87-90.
func (s *OtpState) SetMasked(masked bool, _ *gpui.Window, cx *gpui.Context[OtpState]) {
	s.masked = masked
	cx.Notify()
}

// Focus is U:otp_input.rs:91-93.
func (s *OtpState) Focus(w *gpui.Window, _ *gpui.Context[OtpState]) {
	s.focusHandle.Focus(w)
}

// FocusHandle implements Focusable (U:otp_input.rs:151-155).
func (s *OtpState) FocusHandle(*gpui.App) gpui.FocusHandle { return s.focusHandle }

// otpToDigitChar maps an ASCII or full-width digit to its ASCII digit
// (U:otp_input.rs:95-100). char::to_digit(10) accepts ASCII digits only;
// a full-width digit maps through its offset from '０'.
func otpToDigitChar(value rune) (rune, bool) {
	if value >= '0' && value <= '9' {
		return value, true
	}
	if digit := value - '０'; value >= '０' && digit <= 9 {
		return '0' + digit, true
	}
	return 0, false
}

// otpEditValue applies one key to value (U:otp_input.rs:102-119): it
// returns false when the key does not edit the code.
func otpEditValue(value, key, keyChar string, length int) (string, bool) {
	chars := []rune(value)
	if key == "backspace" {
		if len(chars) > 0 {
			chars = chars[:len(chars)-1]
		}
		return string(chars), true
	}
	first := func(s string) (rune, bool) {
		if s == "" {
			return 0, false
		}
		r, _ := utf8.DecodeRuneInString(s)
		return otpToDigitChar(r)
	}
	digit, ok := first(key)
	if !ok {
		digit, ok = first(keyChar)
	}
	if !ok || len(chars) >= length {
		return "", false
	}
	return string(append(chars, digit)), true
}

// onKeyDown is U:otp_input.rs:121-139.
func (s *OtpState) onKeyDown(event *gpui.KeyDownEvent, w *gpui.Window, cx *gpui.Context[OtpState]) {
	value, ok := otpEditValue(s.value, event.Keystroke.Key, event.Keystroke.KeyChar, s.length)
	if !ok {
		return
	}
	w.PreventDefault()
	cx.StopPropagation()
	s.blinkCursor.Update(cx, func(b *blinkCursor, bcx *gpui.Context[blinkCursor]) { b.pause(bcx) })
	s.value = value
	OtpEvents.Emit(cx, OtpEventChange)
	if utf8.RuneCountInString(s.value) == s.length {
		OtpEvents.Emit(cx, OtpEventComplete)
	}
	cx.Notify()
}

// onFocus is U:otp_input.rs:141-144.
func (s *OtpState) onFocus(_ *gpui.Window, cx *gpui.Context[OtpState]) {
	s.blinkCursor.Update(cx, func(b *blinkCursor, bcx *gpui.Context[blinkCursor]) { b.start(bcx) })
	OtpEvents.Emit(cx, OtpEventFocus)
}

// onBlur is U:otp_input.rs:145-148.
func (s *OtpState) onBlur(_ *gpui.Window, cx *gpui.Context[OtpState]) {
	s.blinkCursor.Update(cx, func(b *blinkCursor, bcx *gpui.Context[blinkCursor]) { b.stop(bcx) })
	OtpEvents.Emit(cx, OtpEventBlur)
}

// Render renders nothing (U:otp_input.rs:157-161).
func (s *OtpState) Render(*gpui.Window, *gpui.Context[OtpState]) gpui.AnyElement {
	return gpui.Empty()
}

// OtpInput is the unstyled OTP interaction root; applications provide
// the visual cells as children (U:otp_input.rs:163-170).
type OtpInput struct {
	gpui.Styled[*OtpInput]
	gpui.Parent[*OtpInput]

	state    gpui.Entity[OtpState]
	disabled bool
}

// NewOtpInput is OtpInput::new (U:otp_input.rs:173-180).
func NewOtpInput(state gpui.Entity[OtpState]) *OtpInput {
	return gpui.BindParent(gpui.BindStyled(&OtpInput{state: state}))
}

// Disabled is U:otp_input.rs:181-184.
func (o *OtpInput) Disabled(disabled bool) *OtpInput {
	o.disabled = disabled
	return o
}

// RenderOnce implements gpui.RenderOnce (U:otp_input.rs:196-208).
func (o *OtpInput) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	state := o.state
	focusHandle := state.Read(app, func(s *OtpState, _ *gpui.App) gpui.FocusHandle { return s.focusHandle })
	// Gap 2b: the plain upstream div() is a block.
	d := gpui.Div().
		ID(gpui.NamedIntegerElementID("base-otp-input", uint64(state.EntityID()))).
		Block().
		TrackFocus(focusHandle)
	if !o.disabled {
		d.OnKeyDown(w.ListenerFor(state, func(s *OtpState, e *gpui.KeyDownEvent, w *gpui.Window, cx *gpui.Context[OtpState]) {
			s.onKeyDown(e, w, cx)
		}))
	}
	d.Extend(o.TakeChildren())
	style := o.Style()
	return StyledExtOf(d).RefineStyle(&style).IntoElement()
}
