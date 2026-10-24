//go:build windows

// Package parity is the browser-parity harness of the browser-core
// spec's seam 1 (.scratch/browser-parity/spec.md, "Testing Decisions"):
// one fixture is a small HTML/CSS page, the equivalent gpui-go view and
// a scripted input sequence; a recorder captures the page once in
// headless Chrome (element boxes, the event log for the input sequence,
// a screenshot, the Chromium version) as checked-in files, and a replay
// test runs the gpui-go view in the hostless test window on the real
// text system with simulated input and compares
//
//   - boxes exactly (Tier A, layout geometry),
//   - the event log exactly (Tier B, an app-visible transcript),
//   - pixels approximately (Tier C, a per-fixture tolerance that is
//     documented as provisional until research 19's calibration runs).
//
// The recorder is run by hand (cmd/parityrecord); the replay is an
// ordinary go test. Everything here is headless and offscreen: the
// gpui side drives the deterministic test window (virtual clock,
// simulated input), and the pixel capture renders that window's scene
// through a hidden, never-shown host window's surface. GPUI_REAL_INPUT_
// TESTS is never set by this harness.
//
// See docs/browser-parity-harness.md for the fixture-writing guide.
package parity

import (
	"fmt"
	"strings"
	"sync"

	"github.com/hoangvu12/gpui-go/gpui"
)

// Size is a logical (CSS-pixel) viewport size: both the Chrome page and
// the gpui test window are sized to it, and both sides' device pixel
// ratio is the test window's fixed 2.0, so one coordinate space serves
// the whole fixture.
type Size struct {
	Width  float32 `json:"width"`
	Height float32 `json:"height"`
}

// StepKind is one scripted input step's kind.
type StepKind string

// The scripted input steps. Positions are logical (CSS) pixels from the
// viewport's top-left; the same step drives Chrome (CDP input dispatch,
// full hit testing) and the gpui test window (simulated events).
const (
	// StepMove moves the pointer to (X, Y).
	StepMove StepKind = "move"
	// StepDown presses the button at (X, Y).
	StepDown StepKind = "down"
	// StepUp releases the button at (X, Y).
	StepUp StepKind = "up"
	// StepClick is sugar for move, down, up of the left button at
	// (X, Y) — expanded identically on both sides.
	StepClick StepKind = "click"
	// StepKey presses and releases one keystroke ("a", "enter",
	// "ctrl-a", the pinned keystroke syntax).
	StepKey StepKind = "key"
	// StepKeyDown presses one keystroke without releasing it
	// (browser-core ticket 12): the recorder sends only the CDP keyDown
	// and the replay dispatches only the KeyDownEvent, so a fixture can
	// record a key held down at the settle — the Space-held :active
	// fixture is the use.
	StepKeyDown StepKind = "keydown"
	// StepKeyUp releases a keystroke a StepKeyDown pressed (browser-core
	// ticket 12). The pair spells one full press across steps when a
	// fixture needs something between the halves.
	StepKeyUp StepKind = "keyup"
	// StepWait advances time by Ms milliseconds without input: the
	// recorder pauses and seeks the page's animations (the Web
	// Animations seek; see recorder.go) and the
	// replay advances the test application's virtual clock and delivers
	// one animation frame (browser-core ticket 44).
	StepWait StepKind = "wait"
	// StepImeSet updates the IME composition (browser-core ticket 18):
	// the recorder drives CDP Input.imeSetComposition and the replay a
	// simulated WM_IME_COMPOSITION (GCS_COMPSTR), so both sides run a
	// real composition session on the focused editable element. An
	// empty Text cancels the composition.
	StepImeSet StepKind = "imeset"
	// StepImeCommit commits text (browser-core ticket 18): the recorder
	// drives CDP Input.insertText and the replay a simulated
	// WM_IME_COMPOSITION (GCS_RESULTSTR) — the commit that ends a
	// composition session.
	StepImeCommit StepKind = "imecommit"
	// StepScript runs a fixture-side page script (browser-core ticket
	// 46): the recorder evaluates Script (a JavaScript expression) on
	// the page and the replay calls Run (a Go function) with the event
	// log, the test window and its application — the two halves of a
	// scripted state change neither input nor time can express (rows
	// inserted above the scrolled viewport). Run must leave both sides
	// in the same state the page's script leaves the page in, and
	// refresh the window when the view must redraw.
	StepScript StepKind = "script"
)

// MouseButton names, shared by the recorder and the replay.
const (
	ButtonLeft   = "left"
	ButtonMiddle = "middle"
	ButtonRight  = "right"
)

// Step is one scripted input event of a fixture's sequence.
type Step struct {
	Kind StepKind
	X, Y float32
	// Button is the step's mouse button (ButtonLeft and so on).
	Button string
	// Clicks is the press's click count of a StepDown or StepUp step
	// (browser-core ticket 19): the multi-click count the event carries
	// (CDP Input.dispatchMouseEvent's clickCount, which becomes the
	// WebMouseEvent's click_count the renderer's double/triple-click
	// arms read; the DOM event's detail). 0 means 1. A double click is a
	// Click followed by a Down/Up pair with Clicks 2; a triple click
	// adds a pair with Clicks 3 — the counts a real rapid sequence of
	// presses reports, so the selection's word and paragraph arms fire
	// (MouseEventManager hands SelectionController the click count of
	// the event; it does not count the presses itself).
	Clicks int
	// Key is the keystroke of a StepKey step, in the pinned keystroke
	// syntax ("a", "enter", "ctrl-shift-]").
	Key string
	// Ms is the milliseconds a StepWait step advances time by.
	Ms int64
	// Code is the physical DOM code of a StepKey step when the
	// keystroke syntax cannot spell the physical key: "Numpad1" with
	// Key "1" drives the numpad's 1 (a real key press reports code
	// Numpad1, key "1" and the numpad location). Empty means the code
	// is derived from the key (browser-core 13).
	Code string
	// Text is the composition or committed text of a StepImeSet /
	// StepImeCommit step (browser-core 18).
	Text string
	// SelStart and SelEnd are the composition selection of a
	// StepImeSet step, within Text (Input.imeSetComposition's
	// selectionStart/selectionEnd).
	SelStart, SelEnd int
	// Script is the JavaScript expression a StepScript step evaluates
	// on the recorded page (the state change's page half). It must be
	// side-effecting and synchronous; its return value is ignored.
	Script string
	// Run is the Go half of a StepScript step: the state change on the
	// replay side, called with the fixture's event log, the test window
	// and its application. It should mutate the fixture's view state —
	// keyed like the view's other per-run state — and refresh the window.
	Run func(log *EventLog, w *gpui.Window, app *gpui.App)
}

// Move returns a pointer-move step to (x, y).
func Move(x, y float32) Step { return Step{Kind: StepMove, X: x, Y: y} }

// Down returns a button-press step at (x, y).
func Down(x, y float32, button string) Step {
	return Step{Kind: StepDown, X: x, Y: y, Button: button}
}

// Up returns a button-release step at (x, y).
func Up(x, y float32, button string) Step {
	return Step{Kind: StepUp, X: x, Y: y, Button: button}
}

// Click returns a left-button click step at (x, y).
func Click(x, y float32) Step { return Step{Kind: StepClick, X: x, Y: y, Button: ButtonLeft} }

// MultiClick returns one multi-click press/release pair at (x, y)
// carrying count as its click count (browser-core ticket 19): count 2 is
// the double-click press of a real two-press sequence, count 3 the
// triple-click press of a three-press one. Pair it with the preceding
// presses the sequence needs: Click then MultiClick(x, y, 2) is a double
// click, Click, MultiClick(x, y, 2), MultiClick(x, y, 3) a triple click.
func MultiClick(x, y float32, count int) Step {
	return Step{Kind: StepClick, X: x, Y: y, Button: ButtonLeft, Clicks: count}
}

// DownWithClicks returns a button-press step at (x, y) carrying count as
// its click count (the raw half of MultiClick: a fixture that moves or
// waits between the halves of a multi-click press spells them out).
func DownWithClicks(x, y float32, button string, count int) Step {
	return Step{Kind: StepDown, X: x, Y: y, Button: button, Clicks: count}
}

// UpWithClicks returns a button-release step at (x, y) carrying count as
// its click count.
func UpWithClicks(x, y float32, button string, count int) Step {
	return Step{Kind: StepUp, X: x, Y: y, Button: button, Clicks: count}
}

// Key returns a keystroke step.
func Key(keystroke string) Step { return Step{Kind: StepKey, Key: keystroke} }

// KeyDown returns a keystroke half-press step: the key goes down and
// stays down (browser-core ticket 12 — the recorder sends one CDP
// keyDown, the replay one KeyDownEvent; pair with KeyUp).
func KeyDown(keystroke string) Step { return Step{Kind: StepKeyDown, Key: keystroke} }

// KeyUp returns the release half of a KeyDown step (browser-core
// ticket 12).
func KeyUp(keystroke string) Step { return Step{Kind: StepKeyUp, Key: keystroke} }

// Wait returns a time-advance step: the recorder seeks the page's
// animations by ms and the replay advances its virtual clock and delivers
// one animation frame, so mid-transition states are sampled at a fixed
// time on both sides (browser-core ticket 44).
func Wait(ms int64) Step { return Step{Kind: StepWait, Ms: ms} }

// ImeSet returns a composition-update step (browser-core ticket 18):
// the recorder drives Input.imeSetComposition with the text and its
// selection, and the replay a simulated WM_IME_COMPOSITION carrying
// the composition string (GCS_COMPSTR) with the cursor at selEnd. A
// non-empty text over no session starts one; a non-empty text over an
// active session updates it; an empty text cancels it.
func ImeSet(text string, selStart, selEnd int) Step {
	return Step{Kind: StepImeSet, Text: text, SelStart: selStart, SelEnd: selEnd}
}

// ImeCommit returns a commit step (browser-core ticket 18): the
// recorder drives Input.insertText and the replay a simulated
// WM_IME_COMPOSITION carrying the result string (GCS_RESULTSTR). A
// commit over an active session ends it; over no session it inserts
// plain text.
func ImeCommit(text string) Step { return Step{Kind: StepImeCommit, Text: text} }

// Script returns a page-script step (browser-core ticket 46): the
// recorder evaluates script (a JavaScript expression) on the settled
// page — before the record's two-frame settle — and the replay calls
// run with the event log, the test window and its application. The
// two halves must make the same state change (rows inserted above the
// viewport, a class toggled by a timer's callback): the step exists
// for state changes neither the input vocabulary nor the clock can
// express. run must be idempotent across the replays of one fixture
// (TestFixtures and the tolerance check run the steps twice — key the
// state like the view's other per-run data, on the event log or the
// window).
func Script(script string, run func(log *EventLog, w *gpui.Window, app *gpui.App)) Step {
	return Step{Kind: StepScript, Script: script, Run: run}
}

// KeyCode returns a keystroke step with an explicit physical DOM code
// (browser-core 13): the keystroke's key and modifiers parse as usual
// and both sides carry the code — the Chrome side sends it to
// Input.dispatchKeyEvent and the gpui side sets it on the keystroke —
// so a step can drive keys the keystroke syntax cannot spell (the
// numpad: KeyCode("1", "Numpad1") types the numpad's 1).
func KeyCode(keystroke, code string) Step {
	return Step{Kind: StepKey, Key: keystroke, Code: code}
}

// FixtureScale returns the fixture's device pixel ratio: the fixture's
// own Scale when it sets one, else the harness default (2.0). Both the
// recorder and the replay resolve through this, so a fixture's record
// and its replay always run at the same ratio.
func FixtureScale(f Fixture) float32 {
	if f.Scale > 0 {
		return f.Scale
	}
	return DeviceScaleFactor
}

// PixelTolerance is a fixture's provisional Tier C gate. Every number
// here is a placeholder until the research-19 calibration (reference
// repeat noise, injected-defect detection) freezes real thresholds;
// each fixture carries its own values and the harness reports the
// measured statistics next to the tolerances when it fails.
type PixelTolerance struct {
	// MaxMeanChannelDiff bounds the mean per-channel difference over
	// the whole image (see pixelStats).
	MaxMeanChannelDiff float64 `json:"maxMeanChannelDiff"`
	// MaxROIMeanChannelDiff bounds the mean per-channel difference
	// within any compared element's box (the whole-image number can
	// average away one badly wrong element; research 19 §1.2).
	MaxROIMeanChannelDiff float64 `json:"maxROIMeanChannelDiff"`
	// MaxHotPixelFraction bounds the fraction of pixels whose mean
	// channel difference exceeds 32, image-wide and per box.
	MaxHotPixelFraction float64 `json:"maxHotPixelFraction"`
}

// EmulatedMediaFeature is one CDP emulated media feature the recorder
// applies to the page before loading it (Emulation.setEmulatedMedia,
// features: [{name, value}]): forced-colors active, prefers-color-scheme,
// print, and the rest of the media-feature emulation vocabulary. The
// features are record-time only (like ChromeFlags): the replay never
// reads them, but the record they produce — a page rendered under
// those features — is what the replay compares against, so the feature
// set is part of the fixture's identity; copy it when cloning a
// fixture (the forcedcolors fixture is the pattern).
type EmulatedMediaFeature struct {
	// Name is the media feature name ("forced-colors").
	Name string `json:"name"`
	// Value is the feature's value ("active", "none", ...).
	Value string `json:"value"`
}

// Fixture is one browser-parity fixture: an HTML/CSS page, the
// equivalent gpui-go view, the scripted input sequence, and the
// comparison policy.
type Fixture struct {
	// Name is the fixture's identity: the recorded files live in
	// internal/parity/testdata/<Name>/.
	Name string
	// HTML is the whole page (doctype on). The recorder writes it to a
	// temp file, loads it in headless Chrome and keeps a copy beside
	// the recorded files for reference.
	HTML string
	// Window is the page's and the gpui test window's logical size.
	Window Size
	// Scale is the fixture's device pixel ratio, overriding the harness
	// default of 2.0 when non-zero: the recorder forces Chrome to it and
	// the replay opens the test window at it (browser-core ticket 07: the
	// border fixtures record at fractional ratios, where the device-pixel
	// rounding rules differ). The window's device extent (logical × Scale)
	// must still fit the capture budget; boxes compare in logical pixels
	// exactly as at the default scale.
	Scale float32
	// View builds the gpui-go view of the page. It receives the event
	// log the harness's instrumented handlers append to; the returned
	// element tree becomes the test window's root. The element ids
	// (Div().ID(...)) map the page's data-id attributes. Set either
	// View or ViewWithWindow, not both (View is enough for fixtures
	// that only log; ViewWithWindow also hands the view the test
	// window, which focus handles and window setup need).
	View func(log *EventLog) gpui.AnyElement
	// ViewWithWindow is the window-aware form of View: the harness
	// passes the test window and its application with the event log.
	// Fixtures that create focus handles (gpui.NewFocusHandle), flip
	// window flags (the key dispatch order) or bind keys (App.BindKeys)
	// use it; the handles and bindings are created once in the
	// fixture's closure and rebuilt views reuse them.
	ViewWithWindow func(log *EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement
	// Steps is the scripted input sequence, dispatched to Chrome at
	// record time and simulated in the test window at replay time.
	Steps []Step
	// EventKinds are the event kinds the page's [data-id] elements
	// listen for (and the view's handlers log): the Tier B transcript's
	// vocabulary for this fixture. A kind here that the view does not
	// log (or the reverse) is a transcript difference the comparison
	// reports. The vocabulary is "mousedown", "mouseup", "click",
	// "pointerenter" and "pointerleave" (the pointer boundary events
	// of browser-core ticket 10), "pointerdown", "pointermove",
	// "pointerup", "gotpointercapture" and "lostpointercapture" (ticket
	// 11), and "keydown" and "keyup" (ticket 09: key entries carry the
	// key and the focused element at listener time; ticket 13 adds the
	// physical code and location; ticket 18 adds the isComposing flag),
	// plus the composition kinds "compositionstart", "compositionupdate"
	// and "compositionend" (ticket 18: entries carry the event's data,
	// logged from the input handler's IMECompositionHandler callbacks —
	// EventLog.AppendComposition — since composition events live on the
	// handler seam, not the div listeners); a fixture avoids the kinds
	// whose delivery order the pin currently gets wrong (see
	// docs/browser-parity-harness.md, known divergences).
	EventKinds []string
	// BoxIDs are the page's data-id values (the gpui elements' ids)
	// whose boxes are compared exactly, in this order.
	BoxIDs []string
	// Tolerance is the provisional Tier C gate.
	Tolerance PixelTolerance
	// ChromeFlags are extra command-line flags the recorder passes to
	// headless Chrome, after the harness's own flags (per-fixture browser
	// knobs: AA mode, color profile, hinting). Recording a page under
	// more than one flag set registers one fixture per flag set (the
	// textpaint rasterization probes are the pattern). The flags are
	// record-time only: the replay never reads them, but a fixture whose
	// record was captured under them compares its replay against that
	// record, so the flag set is part of the fixture's identity — copy
	// the flags into the new fixture when cloning one.
	ChromeFlags []string
	// EmulatedMedia are the CDP emulated media features the recorder
	// applies before loading the page (record-time only, like
	// ChromeFlags; see EmulatedMediaFeature). The forced-colours fixture
	// records with forced-colors active so its Chrome record carries the
	// mapped system colours.
	EmulatedMedia []EmulatedMediaFeature
	// ComputedScript, when non-empty, is a JavaScript expression the
	// recorder evaluates on the settled page (after the input steps,
	// before the screenshot); its value must be a JSON object of strings
	// and lands verbatim in Record.Computed. A fixture that needs
	// page-computed values on the replay side records them here — the
	// forced-colours fixture reads Chrome's emulated system palette out
	// of the record so its replay injects the exact colours the record
	// was rendered with, independent of the machine's theme.
	ComputedScript string
	// Configure, when non-nil, runs on the replay's application right
	// after it is created, before the test window and the fixture's
	// view: the place to pin app-level state the view's styles resolve
	// against (the forced-colours fixture installs the palette Chrome
	// used and turns the pass on through gpui.App.SetForcedColors). It
	// runs in every replay of the fixture — the harness's own run, the
	// tolerance headroom check, and the discrimination tests — because
	// the fixture value is copied wholesale when a test swaps the View.
	Configure func(app *gpui.App) error

	// SkipPixels, when non-empty, skips the Tier C pixel comparison
	// (and its tolerance-headroom check) with this reason, while Tier A
	// and Tier B keep comparing: a fixture documents here a known
	// renderer/native blocker it cannot pass until a specific fix lands,
	// so the record and the view stay checked in and the geometry keeps
	// verifying. Empty for fixtures whose pixels compare today. The
	// reason must name the blocker (ticket and file:line) and the fix
	// that unskips it.
	SkipPixels string
}

// EventRecord is one app-visible event of the Tier B transcript.
type EventRecord struct {
	// Kind is the canonical event kind ("mousedown", "keydown", ...).
	Kind string `json:"kind"`
	// Target is the element the fixture's listener is bound to: the
	// page's data-id, the gpui element's id.
	Target string `json:"target"`
	// DefaultPrevented is the event's default-prevented state.
	DefaultPrevented bool `json:"defaultPrevented"`
	// Key is the event's key for keydown and keyup entries (the DOM
	// key name: "Tab", " ", "ArrowDown"; "" for the mouse kinds).
	Key string `json:"key,omitempty"`
	// Focus is the focused element's id at listener time for keydown
	// and keyup entries ("" when nothing is focused — the page's
	// body-normalized document.activeElement; "" for the mouse
	// kinds). Key entries carrying it prove listener-then-default
	// order: a listener that still sees the old focus ran before the
	// default action changed it.
	Focus string `json:"focus,omitempty"`
	// Code is the event's physical DOM code for keydown and keyup
	// entries (KeyboardEvent.code: "Tab", "BracketRight", "Numpad1";
	// "" for the mouse kinds and when the platform reports none).
	// Ticket 13: key entries carry the physical key's identity
	// regardless of the layout.
	Code string `json:"code,omitempty"`
	// Location is the event's key location for keydown and keyup
	// entries (KeyboardEvent.location: 0 standard, 1 left, 2 right,
	// 3 numpad; 0 for the mouse kinds).
	Location int `json:"location,omitempty"`
	// Data is the event's data for compositionstart, compositionupdate
	// and compositionend entries (CompositionEvent.data: the selected
	// text a start replaces, the new composition string of an update,
	// the committed text of an end, "" for a cancel; "" for the other
	// kinds) — browser-core ticket 18.
	Data string `json:"data,omitempty"`
	// IsComposing is the event's isComposing flag for keydown and
	// keyup entries (KeyboardEvent.isComposing: true while an IME
	// composition is active on the focused editable element; false for
	// the mouse and composition kinds) — browser-core ticket 18. Key
	// entries carrying true prove keydowns are delivered during a
	// composition, flagged, not dropped.
	IsComposing bool `json:"isComposing,omitempty"`
	// State is the toggle event's new state for toggle entries (the
	// DOM ToggleEvent's newState: "open" or "closed"; browser-core
	// ticket 21 — popover opens and light-dismiss closes land in the
	// transcript as toggle entries; "" for the other kinds).
	State string `json:"state,omitempty"`
	// InputType is the event's inputType for beforeinput entries
	// (InputEvent.inputType: "deleteByCut" for the cut command's
	// mutation, "insertFromPaste" for a paste, "insertText" for typed
	// text; "" for the other kinds) — browser-core ticket 26.
	InputType string `json:"inputType,omitempty"`
}

// EventLog is the fixture-side event recorder the View's handlers
// append to. The helpers match the page's data-id instrumentation: the
// recorder's script binds the fixture's EventKinds to every [data-id]
// element, so one fixture writes both sides' transcript with the same
// words. The replay marks each dispatched event's recorded entries
// with the dispatch's default-prevented result, matching the
// recorder's final-state semantics.
type EventLog struct {
	entries []EventRecord
	// focusIDs maps the tracked focus handles to element ids for the
	// Focus field of key entries (TrackFocus).
	focusIDs map[uint64]string
}

// Events returns the recorded transcript.
func (l *EventLog) Events() []EventRecord { return l.entries }

// Append records one event.
func (l *EventLog) Append(kind, target string) {
	l.entries = append(l.entries, EventRecord{Kind: kind, Target: target})
}

// markDefaultPrevented sets the default-prevented state of every entry
// from index on (the entries one dispatched platform event produced),
// the replay side of the DOM event's final defaultPrevented.
func (l *EventLog) markDefaultPrevented(from int, prevented bool) {
	for i := from; i < len(l.entries); i++ {
		l.entries[i].DefaultPrevented = prevented
	}
}

// MouseDown returns an OnMouseDown handler that records a "mousedown"
// on target.
func (l *EventLog) MouseDown(target string) func(*gpui.MouseDownEvent, *gpui.Window, *gpui.App) {
	return func(*gpui.MouseDownEvent, *gpui.Window, *gpui.App) { l.Append("mousedown", target) }
}

// MouseUp returns an OnMouseUp handler that records a "mouseup" on
// target.
func (l *EventLog) MouseUp(target string) func(*gpui.MouseUpEvent, *gpui.Window, *gpui.App) {
	return func(*gpui.MouseUpEvent, *gpui.Window, *gpui.App) { l.Append("mouseup", target) }
}

// Click returns an OnClick handler that records a "click" on target.
func (l *EventLog) Click(target string) func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {
	return func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { l.Append("click", target) }
}

// ContextMenu returns an OnContextMenu handler that records a
// "contextmenu" on target (browser-core ticket 17: the DOM's menu
// event, from the right-button release, the Menu key and Shift+F10).
func (l *EventLog) ContextMenu(target string) func(*gpui.ContextMenuEvent, *gpui.Window, *gpui.App) {
	return func(*gpui.ContextMenuEvent, *gpui.Window, *gpui.App) { l.Append("contextmenu", target) }
}

// ContextMenuPreventDefault returns an OnContextMenu handler that
// records the "contextmenu" on target and prevents the default (the
// DOM listener that calls preventDefault — the core itself suppresses
// nothing with it, the acceptance's check).
func (l *EventLog) ContextMenuPreventDefault(target string) func(*gpui.ContextMenuEvent, *gpui.Window, *gpui.App) {
	return func(e *gpui.ContextMenuEvent, w *gpui.Window, app *gpui.App) {
		l.Append("contextmenu", target)
		w.PreventDefault()
	}
}

// Toggle returns the overlay toggle listener that records a "toggle"
// on target with the transition's new state (browser-core ticket 21:
// the DOM's popover toggle event — oldState/newState — which the
// layer's Show, Hide and light dismiss each fire; pass it to
// OverlayHandle.OnToggle or TopLayer.OnToggle).
func (l *EventLog) Toggle(target string) func(open bool, w *gpui.Window, app *gpui.App) {
	return func(open bool, _ *gpui.Window, _ *gpui.App) {
		state := "closed"
		if open {
			state = "open"
		}
		l.entries = append(l.entries, EventRecord{Kind: "toggle", Target: target, State: state})
	}
}

// PointerEnter returns an OnPointerEnter handler that records a
// "pointerenter" on target (browser-core ticket 10: the DOM boundary
// events, delivered outermost-first on the entered chain).
func (l *EventLog) PointerEnter(target string) func(*gpui.PointerEnterEvent, *gpui.Window, *gpui.App) {
	return func(*gpui.PointerEnterEvent, *gpui.Window, *gpui.App) { l.Append("pointerenter", target) }
}

// PointerLeave returns an OnPointerLeave handler that records a
// "pointerleave" on target (innermost-first on the exited chain).
func (l *EventLog) PointerLeave(target string) func(*gpui.PointerLeaveEvent, *gpui.Window, *gpui.App) {
	return func(*gpui.PointerLeaveEvent, *gpui.Window, *gpui.App) { l.Append("pointerleave", target) }
}

// KeyDown returns an OnKeyDown handler that records a "keydown" on
// target with the event's key, physical code, location, the
// isComposing flag and the focused element's id at listener time (the
// page side's e.key / e.code / e.location / e.isComposing /
// document.activeElement set). The focus field is the order probe of
// the key fixtures: it proves a listener ran before the default
// action moved focus. The isComposing field proves keydowns during an
// IME composition reach the listeners flagged (ticket 18).
func (l *EventLog) KeyDown(target string) func(*gpui.KeyDownEvent, *gpui.Window, *gpui.App) {
	return func(e *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
		l.appendKey("keydown", target, domKeyOf(e.Keystroke.Key), e.Keystroke.Code, int(e.Keystroke.Location), l.focusTarget(w), e.IsComposing)
	}
}

// KeyUp returns an OnKeyUp handler that records a "keyup" on target
// with the event's key, physical code, location, isComposing flag and
// the focused element's id at listener time.
func (l *EventLog) KeyUp(target string) func(*gpui.KeyUpEvent, *gpui.Window, *gpui.App) {
	return func(e *gpui.KeyUpEvent, w *gpui.Window, app *gpui.App) {
		l.appendKey("keyup", target, domKeyOf(e.Keystroke.Key), e.Keystroke.Code, int(e.Keystroke.Location), l.focusTarget(w), e.IsComposing)
	}
}

// KeyDownPreventDefault returns an OnKeyDown handler that records the
// "keydown" on target and prevents the default action (the DOM
// listener that calls preventDefault).
func (l *EventLog) KeyDownPreventDefault(target string) func(*gpui.KeyDownEvent, *gpui.Window, *gpui.App) {
	return func(e *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
		l.appendKey("keydown", target, domKeyOf(e.Keystroke.Key), e.Keystroke.Code, int(e.Keystroke.Location), l.focusTarget(w), e.IsComposing)
		w.PreventDefault()
	}
}

// TrackFocus maps a focus handle to the element id reported in the
// Focus field of key entries. Call it from the view build for every
// instrumented focusable element; the replay's key listeners resolve
// Window.Focused through this map (the page side reads
// document.activeElement's data-id, with body normalized to "").
func (l *EventLog) TrackFocus(handle gpui.FocusHandle, id string) {
	if l.focusIDs == nil {
		l.focusIDs = make(map[uint64]string)
	}
	l.focusIDs[handle.ID()] = id
}

// focusTarget resolves the focused element's id, "" when nothing is
// focused or the handle is not tracked.
func (l *EventLog) focusTarget(w *gpui.Window) string {
	if focused := w.Focused(); focused.Ok {
		if l.focusIDs != nil {
			return l.focusIDs[focused.Handle.ID()]
		}
	}
	return ""
}

// appendKey records one key event entry with its key, physical code,
// location, listener-time focus and isComposing flag.
func (l *EventLog) appendKey(kind, target, key, code string, location int, focus string, isComposing bool) {
	l.entries = append(l.entries, EventRecord{
		Kind:        kind,
		Target:      target,
		Key:         key,
		Code:        code,
		Location:    location,
		Focus:       focus,
		IsComposing: isComposing,
	})
}

// AppendComposition records one composition event entry (kind is
// "compositionstart", "compositionupdate" or "compositionend", data
// the event's data) on target — browser-core ticket 18. The
// composition callbacks live on the input handler
// (gpui.IMECompositionHandler), not the div listener surface, so a
// fixture's handler implementation calls this from its callbacks.
func (l *EventLog) AppendComposition(kind, target, data string) {
	l.entries = append(l.entries, EventRecord{Kind: kind, Target: target, Data: data})
}

// AppendEditEvent records one editing event entry (kind is "copy",
// "cut", "paste" or "select") on target — browser-core ticket 26. The
// clipboard and select events live on the input handler
// (gpui.EditActionHandler), not the div listener surface, so a
// fixture's handler implementation calls this from its callbacks.
func (l *EventLog) AppendEditEvent(kind, target string) {
	l.entries = append(l.entries, EventRecord{Kind: kind, Target: target})
}

// AppendEditEventWithData records one editing event entry carrying
// data (the paste event's clipboard text) on target — browser-core
// ticket 26.
func (l *EventLog) AppendEditEventWithData(kind, target, data string) {
	l.entries = append(l.entries, EventRecord{Kind: kind, Target: target, Data: data})
}

// AppendBeforeInput records one beforeinput entry on target with the
// event's inputType ("deleteByCut", "insertFromPaste", ...) and data —
// browser-core ticket 26.
func (l *EventLog) AppendBeforeInput(target, inputType, data string) {
	l.entries = append(l.entries, EventRecord{
		Kind:      "beforeinput",
		Target:    target,
		InputType: inputType,
		Data:      data,
	})
}

// domKeyOf maps the pinned keystroke key name onto the DOM key name
// the recorder reads from e.key ("tab" → "Tab", "space" → " ",
// "arrowdown" → "ArrowDown"); single characters pass through.
func domKeyOf(key string) string {
	switch key {
	case "tab":
		return "Tab"
	case "enter":
		return "Enter"
	case "escape":
		return "Escape"
	case "space":
		return " "
	case "arrowup":
		return "ArrowUp"
	case "arrowdown":
		return "ArrowDown"
	case "arrowleft":
		return "ArrowLeft"
	case "arrowright":
		return "ArrowRight"
	case "home":
		return "Home"
	case "end":
		return "End"
	case "pageup":
		return "PageUp"
	case "pagedown":
		return "PageDown"
	case "backspace":
		return "Backspace"
	case "delete":
		return "Delete"
	case "insert":
		return "Insert"
	case "shift":
		return "Shift"
	case "control":
		return "Control"
	case "alt":
		return "Alt"
	case "fn":
		return "Fn"
	case "menu":
		// The DOM key name of the application menu key (VKEY_APPS,
		// KeyboardEvent.key "ContextMenu" — the contextmenu trigger of
		// browser-core ticket 17).
		return "ContextMenu"
	case "f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10",
		"f11", "f12":
		// The function keys' DOM names are capitalized ("F10" —
		// Shift+F10 is the contextmenu trigger of browser-core ticket
		// 17).
		return strings.ToUpper(key[:1]) + key[1:]
	}
	return key
}

// PointerDown returns an OnMouseDown handler that records a
// "pointerdown" on target (browser-core ticket 11: the DOM's pointer
// event names for the press).
func (l *EventLog) PointerDown(target string) func(*gpui.MouseDownEvent, *gpui.Window, *gpui.App) {
	return func(*gpui.MouseDownEvent, *gpui.Window, *gpui.App) { l.Append("pointerdown", target) }
}

// PointerMove returns an OnMouseMove handler that records a
// "pointermove" on target (the move events the pointer-capture fixture
// compares: while an element holds the capture, the moves target the
// captured chain — the transcript's retargeting check).
func (l *EventLog) PointerMove(target string) func(*gpui.MouseMoveEvent, *gpui.Window, *gpui.App) {
	return func(*gpui.MouseMoveEvent, *gpui.Window, *gpui.App) { l.Append("pointermove", target) }
}

// PointerUp returns an OnMouseUp handler that records a "pointerup" on
// target.
func (l *EventLog) PointerUp(target string) func(*gpui.MouseUpEvent, *gpui.Window, *gpui.App) {
	return func(*gpui.MouseUpEvent, *gpui.Window, *gpui.App) { l.Append("pointerup", target) }
}

// GotPointerCapture returns an OnGotPointerCapture handler that
// records a "gotpointercapture" on target (browser-core ticket 11:
// the DOM capture events; both bubble, so ancestors record the
// bubbled deliveries too).
func (l *EventLog) GotPointerCapture(target string) func(*gpui.GotPointerCaptureEvent, *gpui.Window, *gpui.App) {
	return func(*gpui.GotPointerCaptureEvent, *gpui.Window, *gpui.App) { l.Append("gotpointercapture", target) }
}

// LostPointerCapture returns an OnLostPointerCapture handler that
// records a "lostpointercapture" on target.
func (l *EventLog) LostPointerCapture(target string) func(*gpui.LostPointerCaptureEvent, *gpui.Window, *gpui.App) {
	return func(*gpui.LostPointerCaptureEvent, *gpui.Window, *gpui.App) { l.Append("lostpointercapture", target) }
}

// ---------------------------------------------------------------------------
// The fixture registry
// ---------------------------------------------------------------------------

var registry struct {
	mu       sync.Mutex
	fixtures []Fixture
	byName   map[string]Fixture
}

// eventKindVocabulary is the event kinds a fixture's EventKinds may
// name: exactly the kinds the EventLog helpers bind (and the
// recorder's page instrumentation understands). Extend the helpers and
// this set together; a kind outside it fails validation with this
// message.
var eventKindVocabulary = map[string]bool{
	"mousedown":          true,
	"mouseup":            true,
	"click":              true,
	"contextmenu":        true,
	"pointerenter":       true,
	"pointerleave":       true,
	"keydown":            true,
	"keyup":              true,
	"pointerdown":        true,
	"pointermove":        true,
	"pointerup":          true,
	"gotpointercapture":  true,
	"lostpointercapture": true,
	"compositionstart":   true,
	"compositionupdate":  true,
	"compositionend":     true,
	"toggle":             true,
	// The editing-event kinds of browser-core ticket 26: the clipboard
	// events and beforeinput/select fire on the focused editable
	// element (the input-handler seam, gpui.EditActionHandler), bound
	// on the page side like any other kind.
	"copy":        true,
	"cut":         true,
	"paste":       true,
	"beforeinput": true,
	"select":      true,
}

// validate checks a fixture's invariants at record and replay time,
// so a vocabulary mistake fails before it produces a confusing diff.
func validate(f Fixture) error {
	if f.Name == "" {
		return fmt.Errorf("parity: a fixture must have a name")
	}
	if (f.View == nil) == (f.ViewWithWindow == nil) {
		return fmt.Errorf("parity: fixture %q must set exactly one of View and ViewWithWindow", f.Name)
	}
	for _, kind := range f.EventKinds {
		if !eventKindVocabulary[kind] {
			return fmt.Errorf("parity: fixture %q: event kind %q is outside the harness's vocabulary (the EventLog helpers bind mousedown, mouseup, click, pointerenter, pointerleave, pointerdown, pointermove, pointerup, gotpointercapture, lostpointercapture, keydown and keyup today; extend them together with the recorder)",
				f.Name, kind)
		}
	}
	for _, step := range f.Steps {
		switch step.Kind {
		case StepMove, StepDown, StepUp, StepClick, StepKey, StepKeyDown, StepKeyUp, StepWait, StepImeSet, StepImeCommit:
			if step.Clicks < 0 {
				return fmt.Errorf("parity: fixture %q: a step's click count %d is negative", f.Name, step.Clicks)
			}
			if step.Clicks > 0 && step.Kind != StepDown && step.Kind != StepUp && step.Kind != StepClick {
				return fmt.Errorf("parity: fixture %q: only press and release steps carry a click count (browser-core 19)", f.Name)
			}
		case StepScript:
			if step.Script == "" || step.Run == nil {
				return fmt.Errorf("parity: fixture %q: a script step needs both its page script and its Go half", f.Name)
			}
		default:
			return fmt.Errorf("parity: fixture %q: unknown step kind %q", f.Name, step.Kind)
		}
	}
	return nil
}

// Register adds a fixture to the harness's registry (a fixture
// package's init). A duplicate name is a panic: fixture names are file
// names.
func Register(f Fixture) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.byName == nil {
		registry.byName = map[string]Fixture{}
	}
	if f.Name == "" {
		panic("parity: a fixture must have a name")
	}
	if _, dup := registry.byName[f.Name]; dup {
		panic(fmt.Sprintf("parity: fixture %q is registered twice", f.Name))
	}
	registry.byName[f.Name] = f
	registry.fixtures = append(registry.fixtures, f)
}

// All returns every registered fixture in registration order.
func All() []Fixture {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	return append([]Fixture(nil), registry.fixtures...)
}

// Lookup returns the registered fixture by name.
func Lookup(name string) (Fixture, bool) {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	f, ok := registry.byName[name]
	return f, ok
}
