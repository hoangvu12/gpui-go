//go:build windows

// Package dnd is ticket 16's fixture set
// (.scratch/browser-core/issues/16-drag-and-drop-sequence.md, spec
// stories 55/56): the full HTML drag-and-drop sequence recorded in
// headless Chrome and replayed against the gpui-go core.
//
// The drag is an internal one — a draggable source dragged with CDP
// mouse events (Input.dispatchMouseEvent; the browser's drag
// controller simulates the drag loop the way it does for Chrome's own
// drag tests, content/browser/devtools/protocol/input_handler.cc:
// 928-991 HandleMouseEvent: the press sets the initial state, the
// move past the threshold starts the drag and every later move drives
// a DragTargetDragEnter/DragOver, the release drops). Escape mid-drag
// is Input.cancelDragging (the OS drag loop's DRAGDROP_S_CANCEL twin).
//
//   - dnd-across: the drag across three targets (t1 accepting with
//     the default copy effect, t2 not accepting, t3 accepting with
//     the move effect), dropped on t3 — Chromium's order
//     (event_handler.cc:1375-1478): dragstart, drag, dragenter,
//     dragover at the start; drag + dragover on the same target;
//     drag, dragenter, dragleave on a change; dragover alone on the
//     move after it; drop and dragend (carrying the final effect).
//   - dnd-drop-refused: the release over the non-accepting t2 — no
//     drop event, dragend with the none effect.
//   - dnd-escape: Escape mid-drag (Input.cancelDragging on the record
//     side, the consumed Escape keydown on the replay side) — drag,
//     dragleave on the current target, dragend with no drop.
//
// The pages bind the drag kinds on every [data-id] element (the
// instrumentation binds EventKinds); the acceptance lives in the
// pages' own dragover listeners (preventDefault; t3 also sets
// dropEffect "move") and the drop listeners flip the target's
// background, so Tier C sees the landed drop.
package dnd

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/internal/parity"
)

// The pages' shared numbers (the pin's defaults the pages pin back:
// flex display, the 1.618 line height — no text on the page, so it
// never matters).
const (
	windowWidth  = 480.0
	windowHeight = 360.0

	sourceHeight = 40.0

	targetWidth  = 160.0
	targetHeight = 320.0

	pageBackground = 0xffffff
	sourceColor    = 0xe2e8f0
	targetColor    = 0xf1f5f9
	droppedColor   = 0x86efac
)

// The input positions (logical pixels): the source strip at the top,
// the three targets side by side below it.
const (
	sourceX, sourceY = 240.0, 20.0

	t1X, t1Y   = 80.0, 200.0
	t1bX, t1bY = 90.0, 210.0
	t2X, t2Y   = 240.0, 200.0
	t2bX, t2bY = 250.0, 210.0
	t3X, t3Y   = 400.0, 200.0
	t3bX, t3bY = 410.0, 210.0
)

// flatTolerance is the pixel gate for the flat-colour page (the
// keyorder rule): the measured correct-view statistics are 0.0 mean /
// 0.0 hot (solid rectangles), so the gates sit far below any real
// defect — a missed drop flip measures ~0.4 whole-image.
var flatTolerance = parity.PixelTolerance{
	MaxMeanChannelDiff:    0.05,
	MaxROIMeanChannelDiff: 0.25,
	MaxHotPixelFraction:   0.003,
}

// dragKinds are the fixture's event kinds: the DOM drag sequence.
var dragKinds = []string{
	"mousedown", "dragstart", "drag", "dragenter", "dragover", "dragleave", "drop", "dragend",
}

// AcrossFixture is the drag across the three targets, dropped on the
// move-effect one: the whole Chromium order in one transcript.
var AcrossFixture = parity.Fixture{
	Name:   "dnd-across",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(),
	Steps: []parity.Step{
		parity.Down(sourceX, sourceY, parity.ButtonLeft),
		parity.Move(t1X, t1Y),
		parity.Move(t1bX, t1bY),
		parity.Move(t2X, t2Y),
		parity.Move(t2bX, t2bY),
		parity.Move(t3X, t3Y),
		parity.Move(t3bX, t3bY),
		parity.Up(t3bX, t3bY, parity.ButtonLeft),
	},
	BoxIDs:         []string{"root", "source", "t1", "t2", "t3"},
	EventKinds:     dragKinds,
	ViewWithWindow: DndView(dndCorrect),
	Tolerance:      flatTolerance,
}

// DropRefusedFixture is the release over the non-accepting target: no
// drop event, and the dragend carries the none effect.
var DropRefusedFixture = parity.Fixture{
	Name:   "dnd-drop-refused",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(),
	Steps: []parity.Step{
		parity.Down(sourceX, sourceY, parity.ButtonLeft),
		parity.Move(t1X, t1Y),
		parity.Move(t1bX, t1bY),
		parity.Move(t2X, t2Y),
		parity.Move(t2bX, t2bY),
		parity.Up(t2bX, t2bY, parity.ButtonLeft),
	},
	BoxIDs:         []string{"root", "source", "t1", "t2", "t3"},
	EventKinds:     dragKinds,
	ViewWithWindow: DndView(dndCorrect),
	Tolerance:      flatTolerance,
}

// EscapeFixture is Escape mid-drag: the drag machinery consumes the
// key, and the cancel fires drag, dragleave on the current target and
// dragend — no drop.
var EscapeFixture = parity.Fixture{
	Name:   "dnd-escape",
	Window: parity.Size{Width: windowWidth, Height: windowHeight},
	HTML:   page(),
	Steps: []parity.Step{
		parity.Down(sourceX, sourceY, parity.ButtonLeft),
		parity.Move(t1X, t1Y),
		parity.Move(t1bX, t1bY),
		parity.Move(t2X, t2Y),
		parity.CancelDrag(),
	},
	BoxIDs:         []string{"root", "source", "t1", "t2", "t3"},
	EventKinds:     dragKinds,
	ViewWithWindow: DndView(dndCorrect),
	Tolerance:      flatTolerance,
}

func init() {
	parity.Register(AcrossFixture)
	parity.Register(DropRefusedFixture)
	parity.Register(EscapeFixture)
}

// ---------------------------------------------------------------------------
// The page
// ---------------------------------------------------------------------------

// page is the fixtures' page: a draggable source strip and three
// targets. t1 accepts the dragover (the default copy effect), t2
// never accepts, t3 accepts with dropEffect "move"; t1's and t3's
// drop listeners flip the target's background (the landed drop's
// pixel signal).
func page() string {
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
	#source {
		box-sizing: border-box;
		width: ` + f2s(windowWidth) + `px;
		height: ` + f2s(sourceHeight) + `px;
		background: #e2e8f0;
	}
	#targets {
		display: flex;
		width: ` + f2s(windowWidth) + `px;
		height: ` + f2s(targetHeight) + `px;
	}
	#t1, #t2, #t3 {
		box-sizing: border-box;
		width: ` + f2s(targetWidth) + `px;
		height: ` + f2s(targetHeight) + `px;
		background: #f1f5f9;
	}
	#t1.dropped, #t3.dropped {
		background: #86efac;
	}
</style>
</head>
<body data-id="root">
	<div id="source" data-id="source" draggable="true"></div>
	<div id="targets">
		<div id="t1" data-id="t1"></div>
		<div id="t2" data-id="t2"></div>
		<div id="t3" data-id="t3"></div>
	</div>
	<script>
		document.getElementById("t1").addEventListener("dragover", function (e) {
			e.preventDefault();
		});
		document.getElementById("t3").addEventListener("dragover", function (e) {
			e.preventDefault();
			e.dataTransfer.dropEffect = "move";
		});
		document.getElementById("t1").addEventListener("drop", function (e) {
			this.classList.add("dropped");
		});
		document.getElementById("t3").addEventListener("drop", function (e) {
			this.classList.add("dropped");
		});
	</script>
</body>
</html>
`
}

// f2s renders a shared float constant for the page's CSS.
func f2s(v float64) string {
	return fmt.Sprintf("%.0f", v)
}

// ---------------------------------------------------------------------------
// The view
// ---------------------------------------------------------------------------

// dndShape parameterizes the view: whether the accepting targets
// preventDefault their dragover (the DOM acceptance). The correct
// view does; the deliberately-wrong view (NoDragoverAcceptance) does
// not, so no target ever accepts.
type dndShape struct {
	accept bool
}

var dndCorrect = dndShape{accept: true}

// DndView is the fixtures' view: a drag source (OnDrag) and the three
// targets, every [data-id] element binding the drag kinds (the page's
// instrumentation twin). t1 and t3 accept the dragover (t3 with the
// move effect) and flip their background on the drop. The drop state
// and the window live in the closure (the keyorder fixtures' rule:
// the view function re-runs every frame, and a fresh replay window
// resets the state).
func DndView(shape dndShape) func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	var dropped map[string]bool
	var builtFor *gpui.Window
	return func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
		if builtFor != w {
			builtFor = w
			dropped = map[string]bool{}
		}
		logDragEvents := func(d *gpui.DivElement, id string) *gpui.DivElement {
			return d.
				OnMouseDown(gpui.MouseButtonLeft, log.MouseDown(id)).
				OnDragStart(log.DragStart(id)).
				OnDragEvent(log.DragSource(id)).
				OnDragEnter(log.DragEnter(id)).
				OnDragOverEvent(log.DragOver(id)).
				OnDragLeave(log.DragLeave(id)).
				OnDropEvent(log.Drop(id)).
				OnDragEnd(log.DragEnd(id))
		}
		root := logDragEvents(gpui.Div().Flex().ID("root").DebugSelector("root"), "root").
			SizeFull().
			Flex().
			FlexCol().
			Bg(gpui.Rgb(pageBackground)).
			// The page pins the reference's 1.618 line height (body
			// font 16px/1.618); the port's default is CSS normal since
			// browser-parity D1, so the golden-ratio opt-in keeps the
			// recorded text placement.
			LineHeight(gpui.PhiLineHeight())
		source := logDragEvents(gpui.Div().ID("source").DebugSelector("source"), "source").
			Size(gpui.Px(windowWidth), gpui.Px(sourceHeight)).
			Bg(gpui.Rgb(sourceColor)).
			OnDrag(dndPayload{}, func(value *dndPayload, offset gpui.Point, w *gpui.Window, app *gpui.App) gpui.View {
				return gpui.ViewOf(gpui.NewEntity(app, w.Scope(), func(v *dndPreview, cx *gpui.Context[dndPreview]) {}))
			})
		target := func(id string) *gpui.DivElement {
			background := uint32(targetColor)
			if dropped[id] {
				background = droppedColor
			}
			return logDragEvents(gpui.Div().ID(id).DebugSelector(id), id).
				Size(gpui.Px(targetWidth), gpui.Px(targetHeight)).
				Bg(gpui.Rgb(background))
		}
		t1 := target("t1")
		if shape.accept {
			t1 = t1.OnDragOverEvent(func(e *gpui.DragOverEvent, w *gpui.Window, app *gpui.App) {
				w.PreventDefault()
			})
		}
		t1 = t1.OnDropEvent(func(e *gpui.DropEvent, w *gpui.Window, app *gpui.App) {
			dropped["t1"] = true
			w.Refresh()
		})
		t2 := target("t2")
		t3 := target("t3")
		if shape.accept {
			t3 = t3.OnDragOverEvent(func(e *gpui.DragOverEvent, w *gpui.Window, app *gpui.App) {
				e.DropEffect = gpui.DragEffectMove
				w.PreventDefault()
			})
		}
		t3 = t3.OnDropEvent(func(e *gpui.DropEvent, w *gpui.Window, app *gpui.App) {
			dropped["t3"] = true
			w.Refresh()
		})
		// The targets row: the page's #targets wrapper (an id-less flex
		// row holding the three targets side by side).
		targets := gpui.Div().Size(gpui.Px(windowWidth), gpui.Px(targetHeight)).Flex().Child(t1).Child(t2).Child(t3)
		return root.
			Child(source).
			Child(targets).
			IntoElement()
	}
}

// NoDragoverAcceptance is the deliberately-wrong view for the Tier B
// comparison: the targets bind the drag listeners but never accept
// (no preventDefault on the dragover), so no target ever accepts the
// drag — the transcript loses the drop entry, the dragover entries'
// defaultPrevented flips to false, and the settled frame never flips
// the dropped colour (Tier C differs with it; Tier A is unchanged).
func NoDragoverAcceptance() func(log *parity.EventLog, w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return DndView(dndShape{accept: false})
}

// dndPayload is the drag's value.
type dndPayload struct{}

// dndPreview is the drag preview view (a small chip; the settled
// frames never show it — the drag has ended by then).
type dndPreview struct{}

// Render implements the preview's element tree.
func (v *dndPreview) Render(w *gpui.Window, cx *gpui.Context[dndPreview]) gpui.AnyElement {
	return gpui.Div().Flex().Size(gpui.Px(20), gpui.Px(20)).Bg(gpui.Rgb(sourceColor)).IntoElement()
}
