//go:build windows

// Package contextmenu is ticket 17's fixture set
// (.scratch/browser-core/issues/17-context-menu-event.md, spec story
// 60): the contextmenu event's triggers — the right-button release,
// the Menu key (on key-up, on Windows) and Shift+F10 (on key-down) —
// recorded in headless Chrome and replayed against the gpui-go core.
//
// The four Chrome fixtures:
//
//   - contextmenu-mouse: a right-button press and release over the
//     box. The contextmenu event follows the whole up tail — the
//     mouseup listeners, the click settling — and bubbles from the
//     element under the release.
//   - contextmenu-menu-key: a click focuses a, then the Menu key. The
//     keydown and keyup listeners run first; the contextmenu fires
//     after the KEYUP (Windows' is_context_menu_key_triggering_event)
//     at the focused element.
//   - contextmenu-shift-f10: a click focuses a, then Shift+F10. The
//     contextmenu fires between the halves — after the KEYDOWN
//     listeners (Shift alone held) — pinning the key-down arm.
//   - contextmenu-prevented: a's contextmenu listener calls
//     preventDefault; the record carries defaultPrevented on the
//     contextmenu entries (the only instrumented kind — the
//     triggering keyup's own prevented state is a separate DOM event
//     the harness's one-flag-per-dispatch marking cannot split, the
//     documented coarseness the activation click already has), and
//     the following Tab still moves focus to b: preventDefault on
//     contextmenu suppresses nothing else in the core.
//
// The pages pin outline: none on the focusable boxes (the port has no
// default focus ring yet, ticket 48) and line-height 1.618 (the pin's
// default until D1 lands), the harness's two standard pins.
package contextmenu

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The pages' shared numbers. Where a gpui-go default differs from a
// browser today, the page pins the gpui-go value explicitly (the
// tracer's rule): the line height 1.618 and the flex column.
const (
	windowWidth  = 480.0
	windowHeight = 360.0

	boxWidth  = 120.0
	boxHeight = 60.0

	pageBackground = 0xffffff
	boxBackground  = 0xe2e8f0
	focusColor     = 0x22c55e
	menuedColor    = 0xf97316
)

// flatTolerance is the pixel gate for the flat-colour pages (the
// keyorder rule): the measured correct-view statistics are 0.0 mean /
// 0.0 hot (solid rectangles; outline:none removes Chrome's ring), so
// the gates sit far below any real defect — a missed menu flip or a
// wrong focus colour measures whole-image.
var flatTolerance = parity.PixelTolerance{
	MaxMeanChannelDiff:    0.05,
	MaxROIMeanChannelDiff: 0.25,
	MaxHotPixelFraction:   0.003,
}

// MouseFixture is the right-button trigger: a press and release over
// the box. The transcript pins the order — mousedown, mouseup, then
// the contextmenu after the whole up tail — and the box's listener
// flips its background so Tier C sees the event too.
var MouseFixture = parity.Fixture{
	Name:   "contextmenu-mouse",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   menuPage("box", 1, false, false),
	Steps: []parity.Step{
		parity.Down(60, 30, parity.ButtonRight),
		parity.Up(60, 30, parity.ButtonRight),
	},
	BoxIDs:         []string{"root", "box"},
	EventKinds:     []string{"mousedown", "mouseup", "contextmenu"},
	ViewWithWindow: menuView("box", 1, false, false),
	Tolerance:      flatTolerance,
}

// MenuKeyFixture is the Menu-key trigger: a click focuses a (the
// mouse entries), then the Menu key. The keydown and keyup listeners
// run first and the contextmenu fires after the KEYUP at the focused
// element — Windows' menu-key arm.
var MenuKeyFixture = parity.Fixture{
	Name:   "contextmenu-menu-key",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   menuPage("a", 1, true, false),
	Steps: []parity.Step{
		parity.Click(60, 30),
		parity.Key("menu"),
	},
	BoxIDs:         []string{"root", "a"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup", "contextmenu"},
	ViewWithWindow: menuView("a", 1, true, false),
	Tolerance:      flatTolerance,
}

// ShiftF10Fixture is the Shift+F10 trigger: a click focuses a, then
// Shift+F10. The contextmenu fires between the halves — after the
// keydown listeners, before the keyup — pinning the key-down arm
// (Shift alone held).
var ShiftF10Fixture = parity.Fixture{
	Name:   "contextmenu-shift-f10",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   menuPage("a", 1, true, false),
	Steps: []parity.Step{
		parity.Click(60, 30),
		parity.Key("shift-f10"),
	},
	BoxIDs:         []string{"root", "a"},
	EventKinds:     []string{"mousedown", "mouseup", "click", "keydown", "keyup", "contextmenu"},
	ViewWithWindow: menuView("a", 1, true, false),
	Tolerance:      flatTolerance,
}

// PreventedFixture is the preventDefault check: a's contextmenu
// listener prevents the default (and flips a's colour), then a Tab
// moves focus to b — the default action still runs, so a prevented
// contextmenu suppresses nothing else in the core. The record binds
// only the contextmenu kind: the contextmenu's prevented state is the
// one flag the harness can mark on this dispatch (the triggering
// keyup's own state is a separate DOM event; see the package comment).
var PreventedFixture = parity.Fixture{
	Name:   "contextmenu-prevented",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   menuPage("a", 2, true, true),
	Steps: []parity.Step{
		parity.Click(60, 30),
		parity.Key("menu"),
		parity.Key("tab"),
	},
	BoxIDs:         []string{"root", "a", "b"},
	EventKinds:     []string{"contextmenu"},
	ViewWithWindow: PreventedMenuView("a", 2, true, true),
	Tolerance:      flatTolerance,
}

func init() {
	parity.Register(MouseFixture)
	parity.Register(MenuKeyFixture)
	parity.Register(ShiftF10Fixture)
	parity.Register(PreventedFixture)
}

// ---------------------------------------------------------------------------
// The pages
// ---------------------------------------------------------------------------

// menuPage is the fixtures' page: one box (a or box — tabindex 0 when
// focusable, so the click focuses it) whose :focus background is the
// focus probe and whose .menued background (the contextmenu listener's
// flip, source-ordered after :focus) is the menu probe. boxCount 2
// adds b (the Tab's landing) and prevent arms a's listener.
func menuPage(firstID string, boxCount int, focusable, prevent bool) string {
	tabindex := ""
	if focusable {
		tabindex = " tabindex=\"0\""
	}
	second := ""
	if boxCount > 1 {
		second = "\t<div id=\"b\" data-id=\"b\" tabindex=\"0\"></div>\n"
	}
	preventLine := ""
	if prevent {
		preventLine = "\t\te.preventDefault();\n"
	}
	return `<!doctype html>
<html>
<head>
<meta charset="utf-8">
<style>
	html, body {
		margin: 0;
		padding: 0;
		background: #ffffff;
	}
	body {
		width: ` + f2s(windowWidth) + `px;
		height: ` + f2s(windowHeight) + `px;
		display: flex;
		flex-direction: column;
		font: 16px/1.618 "Segoe UI", system-ui, sans-serif;
	}
	#box, #a, #b {
		box-sizing: border-box;
		width: ` + f2s(boxWidth) + `px;
		height: ` + f2s(boxHeight) + `px;
		background: #e2e8f0;
	}
	#box:focus, #a:focus, #b:focus {
		background: #22c55e;
		outline: none;
	}
	#box.menued, #a.menued, #b.menued {
		background: #f97316;
	}
</style>
</head>
<body data-id="root">
	<div id="` + firstID + `" data-id="` + firstID + `"` + tabindex + `></div>
` + second + `<script>
	document.getElementById("` + firstID + `").addEventListener("contextmenu", function (e) {
		this.classList.add("menued");
` + preventLine + "\t" + `});
</script>
</body>
</html>
`
}

// f2s renders a shared float constant for the pages' CSS.
func f2s(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

// ---------------------------------------------------------------------------
// The views
// ---------------------------------------------------------------------------

// menuView is the fixtures' view factory: the instrumented root, one
// (or two) focusable box whose contextmenu listener logs and flips the
// shared menued state (the closure state, keyed on the window — the
// keyorder fixtures' rule: the view function re-runs every frame).
// The boxes carry the :focus and menued styles like the page's rules.
// mouseKinds selects the mousedown/mouseup/click/keydown/keyup
// instrumentation (the page's addEventListener set — button-agnostic,
// so the view binds the OnAny* forms; the prevented fixture's page
// binds only the contextmenu kind, so its view binds only that).
func menuView(firstID string, boxCount int, focusable, prevent bool) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return menuViewOf(firstID, boxCount, focusable, prevent, true)
}

// PreventedMenuView is the prevented fixture's view: only the
// contextmenu kind is instrumented (the record's EventKinds), the
// click still focuses a through the core's mouse-focus default.
func PreventedMenuView(firstID string, boxCount int, focusable, prevent bool) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return menuViewOf(firstID, boxCount, focusable, prevent, false)
}

func menuViewOf(firstID string, boxCount int, focusable, prevent, mouseKinds bool) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	var handles map[string]gpui.FocusHandle
	var menued map[string]bool
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			handles = make(map[string]gpui.FocusHandle)
			menued = make(map[string]bool)
			ids := []string{firstID}
			if boxCount > 1 {
				ids = append(ids, "b")
			}
			for _, id := range ids {
				handle := gpui.NewFocusHandle(w)
				if focusable {
					handle = handle.TabStop(true)
				}
				handles[id] = handle
				log.TrackFocus(handle, id)
			}
		}
		root := gpui.Div().
			ID("root").
			DebugSelector("root").
			SizeFull().
			Flex().
			FlexCol().
			Bg(gpui.Rgb(pageBackground)).
			OnContextMenu(log.ContextMenu("root"))
		if mouseKinds {
			root = root.
				OnAnyMouseDown(log.MouseDown("root")).
				OnAnyMouseUp(log.MouseUp("root")).
				OnClick(log.Click("root")).
				OnKeyDown(log.KeyDown("root")).
				OnKeyUp(log.KeyUp("root"))
		}
		box := func(id string) *gpui.DivElement {
			background := uint32(boxBackground)
			if menued[id] {
				background = menuedColor
			}
			// The colours model the page's source order: .menued comes
			// after :focus, so a menued box keeps the menu colour even
			// while focused — the Focus refinement follows the state.
			box := gpui.Div().
				ID(id).
				DebugSelector(id).
				Size(gpui.Px(boxWidth), gpui.Px(boxHeight)).
				Bg(gpui.Rgb(background)).
				OnContextMenu(func(e *gpui.ContextMenuEvent, w *gpui.Window, app *gpui.App) {
					menued[id] = true
					if prevent && id == firstID {
						log.ContextMenuPreventDefault(id)(e, w, app)
						return
					}
					log.ContextMenu(id)(e, w, app)
				})
			if mouseKinds {
				box = box.
					OnAnyMouseDown(log.MouseDown(id)).
					OnAnyMouseUp(log.MouseUp(id)).
					OnClick(log.Click(id)).
					OnKeyDown(log.KeyDown(id)).
					OnKeyUp(log.KeyUp(id))
			}
			if handle, ok := handles[id]; ok && focusable {
				box = box.
					// The page pins outline: none on the focusable
					// boxes; the port has a default focus ring now
					// (ticket 48), so the view carries the pin's
					// twin.
					OutlineNone().
					TrackFocus(handle).
					Focus(func(s gpui.StyleRefinement) gpui.StyleRefinement {
						focusBg := uint32(focusColor)
						if menued[id] {
							focusBg = menuedColor
						}
						s.Background = gpui.Some(gpui.BackgroundOf(gpui.Rgb(focusBg)))
						return s
					})
			}
			return box
		}
		out := root.Child(box(firstID))
		if boxCount > 1 {
			out = out.Child(box("b"))
		}
		return out.IntoElement()
	}
}
