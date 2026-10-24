package gpui

// The tooltip hook (browser-core ticket 24, spec story 71): any element
// can carry a tooltip (DivElement.Tooltip / TooltipView) that opens on
// hover and on keyboard focus with Chrome's delays and grace period,
// in the hint layer of the top layer, with a plain default look.
//
// The Chromium mechanism this ports is the INTEREST INVOKER
// (the interestfor API — the recordable renderer-side twin of the
// title tooltip; 0d4dcd4f, %TEMP%\research-src\chromium,
// third_party/blink/renderer below):
//
//   - The delays: interest is gained interest-delay-start after the
//     hover or focus (the default 0.25s, core/dom/element.h:306-308
//     kDefaultInterestDelayStartSeconds) and lost interest-delay-end
//     after the de-hover or blur (the default 0.15s,
//     kDefaultInterestDelayEndSeconds) — ScheduleInterestGainedTask /
//     ScheduleInterestLostTask read the style's interest-delay
//     (core/dom/element.cc:13280-13335). The port's Tooltip hook uses
//     the same two constants (element.cc:13286-13290 / :13301-13305's
//     normal arms) through the scheduler's timers, so a Wait step's
//     clock advance fires them exactly like Chrome's task runner.
//   - The triggers: pointerover/pointerout drive the hover source and
//     focusin/focusout the focus source (HandlePointerEventsFor-
//     InterestFor / HandleFocusEventsForInterestFor, element.cc:
//     2125-2158), each walking the ancestor chain and scheduling the
//     gained or lost task on the invokers it finds
//     (ScheduleInterestChangesIfNeeded, element.cc:13467-13511: a
//     hover/focus cancels the pending lost tasks and schedules a
//     gained task on a no-interest invoker; a de-hover/blur cancels
//     the pending gained task and schedules a lost task on a
//     full-interest one — and hovering the invoker's TARGET cancels
//     the invoker's pending lost tasks, the upstream arm). The port
//     rides the hover boundary sweeps (hoverboundary.go — the DOM
//     pointerenter/leave halves) and a paint-time focus diff, and the
//     tooltip's own surface cancels or re-schedules the pending lost
//     task the same way.
//   - The show: InterestGained dispatches a cancelable interest event
//     and invokes the target popover (element.cc:2053-2063) — an
//     OverlayHint entry here; showing a hint runs the hint stack's
//     show rules (toplayer.go, ticket 21). InterestLost dispatches
//     loseinterest and hides a popover invoked via interest
//     (element.cc:2108-2123).
//   - Escape: the default Escape handler drops ALL interest first
//     (Element::LoseInterestInAllElements, element.cc:9170-9183, from
//     DefaultEscapeEventHandler, core/input/keyboard_event_manager.cc:
//     618-628) — a non-cancelable immediate loss — and only then
//     signals the close-watcher stack; the tooltip's hint entry also
//     carries a close watcher (ticket 22's rule for hint kinds), so
//     an Escape that somehow misses the interest loss still closes
//     through the watcher.
//
// The title attribute's own tooltip is browser UI (the browser process
// paints it with OS-like delays, ui/views, not in this checkout and
// not reachable from a headless record); the interest invoker is the
// renderer-side mechanism a fixture CAN record, and its delay
// constants are what the port adopts.
//
// The DOM↔port identity mapping: the interest invoker is the tooltip-
// carrying element (its global element-id path key, the retained
// element-state identity), the target popover is the entry the window
// renders while the handle is open, and the implicit anchor (the
// invoker, ShowPopoverInternal's SetImplicitAnchor, html_element.cc:
// 2187) is the element's prepainted bounds cell, which the default
// look places itself against (ticket 25's Anchored). The window — the
// UA stand-in — renders the tooltip's TopLayer element itself (the
// inspector element's injection pattern), so an app never wires
// anything but the Tooltip call.

import (
	"sort"
	"time"
)

// interestDelayStart is Chrome's default interest-delay-start
// (kDefaultInterestDelayStartSeconds, element.h:307): the hover or
// focus must persist this long before the interest is gained.
const interestDelayStart = 250 * time.Millisecond

// interestDelayEnd is Chrome's default interest-delay-end
// (kDefaultInterestDelayEndSeconds, element.h:308): the de-hover or
// blur must persist this long before the interest is lost — the grace
// period that lets the pointer travel from the element onto the
// tooltip without it closing.
const interestDelayEnd = 150 * time.Millisecond

// tooltipSpec is a div's tooltip authoring state (the Tooltip /
// TooltipView / OnTooltipToggle builders): the plain default look's
// text or the custom content builder, plus the optional toggle
// listener.
type tooltipSpec struct {
	// text is the default look's text (Tooltip); empty with a builder.
	text string
	// builder builds the custom content each frame (TooltipView); nil
	// for the default look.
	builder func(w *Window, app *App) AnyElement
	// onToggle is the tooltip overlay's toggle listener (OnTooltipToggle).
	onToggle func(open bool, w *Window, app *App)
}

// Tooltip gives this element a tooltip (the title attribute's family;
// the interest invoker's target): the text opens in the hint layer on
// hover and on keyboard focus after Chrome's interest delays, stays
// open through the grace period while the pointer travels between the
// element and the tooltip, and closes on Escape. The plain default
// look renders the text below the element. The text also becomes the
// element's accessible description (the title attribute's a11y
// mapping). The element needs an ID (Div().ID) — the tooltip's
// identity, like the retained element state, keys on the element's
// id path.
func (d *DivElement) Tooltip(text string) *DivElement {
	d.tooltip = &tooltipSpec{text: text}
	// The title attribute's a11y mapping: the text becomes the
	// accessible description, unless the author set one themselves.
	if state := d.a11yStateIfAny(); state == nil {
		d.AriaDescription(text)
	} else if _, set := state.description.Get(); !set {
		d.AriaDescription(text)
	}
	return d
}

// TooltipView gives this element a tooltip with custom content (the
// research's TooltipView(builder)): the builder runs each frame to
// build the tooltip's element, which the window renders in the hint
// layer while the tooltip is open — everything else (the triggers,
// the delays, the grace period, Escape) is the Tooltip hook's. The
// content places itself (the default look's anchoring does not
// apply).
func (d *DivElement) TooltipView(builder func(w *Window, app *App) AnyElement) *DivElement {
	if builder == nil {
		panic("gpui: TooltipView requires a non-nil builder")
	}
	d.tooltip = &tooltipSpec{builder: builder}
	return d
}

// OnTooltipToggle sets the tooltip overlay's toggle listener (the
// DOM popover toggle event on the tooltip): notified on every open and
// close transition of THIS element's tooltip, delivered at the input
// task boundary like every overlay toggle. Setting it again replaces
// the listener.
func (d *DivElement) OnTooltipToggle(f func(open bool, w *Window, app *App)) *DivElement {
	if d.tooltip == nil {
		panic("gpui: OnTooltipToggle requires the element to carry a tooltip (Tooltip or TooltipView)")
	}
	d.tooltip.onToggle = f
	return d
}

// ---------------------------------------------------------------------------
// The per-window tooltip state
// ---------------------------------------------------------------------------

// tooltipRecord is one tooltip-carrying element's persistent record
// (the interest invoker): the element's identity (its id path key),
// the overlay handle of its tooltip, the retained hover and focus
// state, and the pending interest timers.
type tooltipRecord struct {
	key    string
	window *Window
	// spec is this frame's authoring state (refreshed every prepaint).
	spec *tooltipSpec
	// invoker is the element's id name when it has one: the tooltip's
	// light-dismiss invoker link (a click on the element relates to
	// the tooltip, so it stays open).
	invoker string
	// focusID is the element's focus handle id, 0 when it has none.
	focusID uint64
	// handle is the tooltip's overlay entry (the hint kind).
	handle OverlayHandle
	// boundsCell is the element's prepainted bounds cell — the default
	// look's anchor (the implicit anchor).
	boundsCell *Bounds
	// hover and focused are the retained trigger states.
	hover, focused bool
	// tipHovered is the retained state of the pointer over the
	// tooltip's own surface (the upstream arm of the grace period).
	tipHovered bool
	// interest is the invoker's full interest (the tooltip is open
	// through this element). Reconciled lazily: an entry hidden by
	// light dismiss or Escape clears it.
	interest bool
	// showTask and hideTask are the pending interest timers (the
	// cancelable delayed tasks of ScheduleInterestGainedTask /
	// ScheduleInterestLostTask); zero when none is pending.
	showTask, hideTask Task[struct{}]
}

// windowTooltipState is a window's tooltip records, keyed by the
// element's id path (the retained element-state identity).
type windowTooltipState struct {
	records map[string]*tooltipRecord
}

// tooltipWindowStates is the per-window registry (the overlay state
// pattern). Closed windows are pruned when a new entry is created.
var tooltipWindowStates = map[*Window]*windowTooltipState{}

// tooltipStateOf returns (creating when absent) the window's tooltip
// state.
func tooltipStateOf(w *Window) *windowTooltipState {
	if ts, ok := tooltipWindowStates[w]; ok {
		return ts
	}
	for other := range tooltipWindowStates {
		if !windowIsOpen(other) {
			delete(tooltipWindowStates, other)
		}
	}
	ts := &windowTooltipState{records: map[string]*tooltipRecord{}}
	tooltipWindowStates[w] = ts
	return ts
}

// tooltipRecordOf returns (creating when absent) the record of the
// element at path, with this frame's spec.
func tooltipRecordOf(w *Window, path string, spec *tooltipSpec) *tooltipRecord {
	ts := tooltipStateOf(w)
	if rec, ok := ts.records[path]; ok {
		return rec
	}
	cell := Bounds{}
	rec := &tooltipRecord{
		key:        path,
		window:     w,
		handle:     NewOverlayHandle(w),
		boundsCell: &cell,
	}
	ts.records[path] = rec
	return rec
}

// interested reports the invoker's full interest, reconciled against
// the entry's open state (an entry hidden by light dismiss, by Escape
// or by the app clears the interest lazily, like the DOM's interest
// following the popover's open state).
func (rec *tooltipRecord) interested() bool {
	if rec.interest && !rec.handle.IsOpen() {
		rec.interest = false
	}
	return rec.interest
}

// ---------------------------------------------------------------------------
// The interest timers (ScheduleInterestGainedTask / -LostTask)
// ---------------------------------------------------------------------------

// zeroTask is the pending-task sentinel.
var zeroTask = Task[struct{}]{}

// scheduleGained schedules the interest-gained timer
// (ScheduleInterestGainedTask, element.cc:13280-13308): the delay is
// interest-delay-start; the task is cancelable, and scheduling
// replaces any pending one. The source's caller already cancelled the
// pending lost task (ScheduleInterestChangesIfNeeded's hover/focus
// arm, element.cc:13477-13490).
func (rec *tooltipRecord) scheduleGained() {
	if rec.interested() || rec.showTask != zeroTask {
		return
	}
	w := rec.window
	rec.showTask = w.app.SpawnDelivering(w.Scope(), func(run *TaskRun) struct{} {
		run.Sleep(interestDelayStart)
		return struct{}{}
	}, func(_ struct{}, _ *App) {
		rec.gainInterest()
	})
}

// scheduleLost schedules the interest-lost timer
// (ScheduleInterestLostTask, element.cc:13293-13335): the delay is
// interest-delay-end, the grace period.
func (rec *tooltipRecord) scheduleLost() {
	if !rec.interested() || rec.hideTask != zeroTask {
		return
	}
	w := rec.window
	rec.hideTask = w.app.SpawnDelivering(w.Scope(), func(run *TaskRun) struct{} {
		run.Sleep(interestDelayEnd)
		return struct{}{}
	}, func(_ struct{}, _ *App) {
		rec.loseInterest(false)
	})
}

// cancelGained cancels the pending gained task
// (CancelInterestGainedTask).
func (rec *tooltipRecord) cancelGained() {
	if rec.showTask != zeroTask {
		rec.showTask.Cancel()
		rec.showTask = zeroTask
	}
}

// cancelLost cancels the pending lost task (CancelInterestLostTask).
func (rec *tooltipRecord) cancelLost() {
	if rec.hideTask != zeroTask {
		rec.hideTask.Cancel()
		rec.hideTask = zeroTask
	}
}

// gainInterest is the interest timer's delivery: the invoker gains
// full interest and the tooltip shows (InterestGained → InvokePopover,
// element.cc:2047-2063 — the interest event's preventDefault gate has
// no port-side twin; the port's tooltip has no interest listeners).
func (rec *tooltipRecord) gainInterest() {
	rec.showTask = zeroTask
	if rec.interest || rec.window == nil {
		return
	}
	rec.interest = true
	rec.handle.Show()
	rec.window.requestRefresh()
}

// loseInterest closes the tooltip (InterestLost → HidePopoverInternal,
// element.cc:2068-2123). notCancelable is the Escape path's form
// (LoseInterestInAllElements passes kNotCancelable); the port has no
// loseinterest listeners to gate, so the flag only documents the
// source.
func (rec *tooltipRecord) loseInterest(notCancelable bool) {
	_ = notCancelable
	rec.hideTask = zeroTask
	if !rec.interest || rec.window == nil {
		return
	}
	rec.interest = false
	rec.handle.Hide()
	rec.window.requestRefresh()
}

// ---------------------------------------------------------------------------
// The trigger hooks (hover, focus, the tooltip's own surface)
// ---------------------------------------------------------------------------

// prepaintTooltipHook is the div prepaint's tooltip half: refresh the
// record (the spec, the invoker name, the focus identity, the anchor
// bounds), mark it seen for the frame-end reconciliation, and diff the
// focus state (the focusin/focusout source — focus changes always
// trigger the frame that follows them, so a paint-time diff sees every
// transition). Called from div.go's prepaint while the div carries a
// tooltip and an element ID.
func prepaintTooltipHook(d *DivElement, global *GlobalElementID, bounds Bounds, w *Window, app *App) {
	if d.tooltip == nil || global == nil {
		return
	}
	path := global.key()
	rec := tooltipRecordOf(w, path, d.tooltip)
	rec.spec = d.tooltip
	rec.invoker = ""
	if d.elementID != nil && d.elementID.Kind == ElementIDName {
		rec.invoker = d.elementID.Name
		// browser-core ticket 72: the invoker also records its bounds
		// into the frame's anchor registry, so custom TooltipView content
		// can place itself with Anchored().AnchorTo(<the invoker's id>) —
		// the default look reads the record's own bounds cell, but the
		// builder's content has no cell. Recording here (turning the
		// window's tracking on and writing the in-flight frame's registry)
		// resolves the name on the FIRST frame the tooltip renders, not
		// one frame late: the window renders the tooltip's top-layer
		// element after the app tree's prepaints, so the registry entry is
		// already there when the content's Anchored looks it up.
		requestAnchorTracking(w)
		recordAnchorBounds(w, d.elementID.Name, bounds, ContentMask(w))
	}
	rec.focusID = 0
	if d.focusHandle != nil {
		rec.focusID = d.focusHandle.id
	}
	// The anchor's bounds cell (the implicit anchor): the default look
	// reads it at the deferred round's prepaint, after the in-flow
	// prepaints refreshed it.
	if rec.boundsCell != nil {
		*rec.boundsCell = bounds
	}
	// The toggle listener follows the authoring state (a fresh closure
	// every frame is fine: the listener is replaced, not accumulated).
	if d.tooltip.onToggle != nil {
		rec.handle.OnToggle(d.tooltip.onToggle)
	}
	// The focus diff (HandleFocusEventsForInterestFor, element.cc:
	// 2135-2158): focusin schedules a gained task, focusout a lost
	// one, with the pending opposite task cancelled
	// (ScheduleInterestChangesIfNeeded, element.cc:13467-13490).
	focused := rec.focusID != 0 && rec.focusID == focusState(w).focused
	if focused != rec.focused {
		rec.focused = focused
		if focused {
			rec.cancelLost()
			rec.scheduleGained()
		} else {
			rec.cancelGained()
			rec.scheduleLost()
		}
	}
	// The frame's seen mark (the frame-end reconciliation drops the
	// records whose element stopped drawing).
	if fs := focusState(w); fs.next != nil {
		fs.next.tooltipSeen = append(fs.next.tooltipSeen, path)
	}
}

// paintTooltipHoverListener registers the hover source's boundary
// listener (HandlePointerEventsForInterestFor's pointerover/out, via
// the hover boundary sweeps of browser-core ticket 10): a hover
// transition schedules a gained task, an unhover a lost one, with the
// pending opposite task cancelled. The layout-change path (a
// stationary pointer whose element moved under it) reports through the
// deferred sweep, like the boundary listeners.
func paintTooltipHoverListener(d *DivElement, hitbox Hitbox, path string, w *Window, app *App) {
	if d.tooltip == nil || path == "" {
		return
	}
	rec := tooltipRecordOf(w, path, d.tooltip)
	box := hitbox
	w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
		sweep, ok := event.(*hoverBoundarySweep)
		if !ok {
			return
		}
		hovered := !app.HasActiveDrag() && box.IsHovered(w)
		if hovered == rec.hover {
			return
		}
		// Each sweep owns only its own half of the transition (the
		// boundary listeners' rule).
		if hovered != sweep.enter {
			return
		}
		rec.hover = hovered
		if hovered {
			rec.cancelLost()
			rec.scheduleGained()
		} else {
			rec.cancelGained()
			rec.scheduleLost()
		}
	})
	if hovered := !app.HasActiveDrag() && hitbox.IsHovered(w); hovered != rec.hover {
		w.deferHoverBoundarySweep(app)
	}
}

// paintTooltipSurfaceListener registers the tooltip's own surface
// listener — the upstream arm of the grace period
// (ScheduleInterestChangesIfNeeded's AllSourceInterestInvokers loop,
// element.cc:13481-13508): hovering the invoker's TARGET cancels the
// invoker's pending lost task, and leaving the target schedules it.
// Called from toplayer.go's topLayerContent prepaint for the window's
// tooltip entries.
func paintTooltipSurfaceListener(rec *tooltipRecord, box Hitbox, w *Window, app *App) {
	w.OnMouseEvent(func(event any, phase DispatchPhase, w *Window, app *App) {
		sweep, ok := event.(*hoverBoundarySweep)
		if !ok {
			return
		}
		hovered := !app.HasActiveDrag() && box.IsHovered(w)
		if hovered == rec.tipHovered {
			return
		}
		if hovered != sweep.enter {
			return
		}
		rec.tipHovered = hovered
		if hovered {
			rec.cancelLost()
		} else if rec.interested() {
			rec.scheduleLost()
		}
	})
	// No registration-time check (the layout-change path of the
	// boundary listeners): this listener registers during the DEFERRED
	// round's prepaint, before the paint-time hit test covers the
	// deferred hitboxes (interact.go refreshPaintHitTestAfterDeferred
	// runs after the whole round), so the shared answer at registration
	// is stale and a check here would schedule a bogus deferred sweep —
	// a spurious pointerleave of the tooltip's own surface. The
	// input-driven sweeps (the moves, whose dispatch recomputes the hit
	// test against the rendered frame) carry every real transition.
}

// dropAllTooltipInterest is the Escape default action's interest loss
// (Element::LoseInterestInAllElements, element.cc:9170-9183, called
// from DefaultEscapeEventHandler BEFORE the close-watcher signal,
// keyboard_event_manager.cc:616-628): every invoker holding interest
// loses it immediately — a non-cancelable close of every
// interest-opened tooltip. Runs before signalCloseWatchers, so a
// tooltip always closes at the interest loss and the watcher stack
// serves whatever sits below it.
func (w *Window) dropAllTooltipInterest(app *App) {
	if w == nil {
		return
	}
	ts, ok := tooltipWindowStates[w]
	if !ok {
		return
	}
	keys := make([]string, 0, len(ts.records))
	for key := range ts.records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	// The document's interest sources in reverse order
	// (LoseInterestInAllElements's std::views::reverse).
	changed := false
	for i := len(keys) - 1; i >= 0; i-- {
		rec := ts.records[keys[i]]
		if rec.interested() {
			rec.cancelGained()
			rec.loseInterest(true)
			changed = true
		} else {
			rec.cancelGained()
			rec.cancelLost()
		}
	}
	if changed {
		w.requestRefresh()
	}
}

// ---------------------------------------------------------------------------
// The frame-end reconciliation
// ---------------------------------------------------------------------------

// finishTooltipFrame reconciles the tooltip records against the
// completed frame (finishFrame's tail, after finishOverlayFrame): the
// records whose element did not draw are dropped — their pending
// timers cancelled and their entries hidden (the popover's
// element-gone hide: an invoker that leaves the document loses its
// interest). It reports whether anything changed, so the frame end
// refreshes.
func (w *Window) finishTooltipFrame(app *App) bool {
	ts, ok := tooltipWindowStates[w]
	if !ok {
		return false
	}
	fs := focusState(w)
	if fs.rendered == nil {
		return false
	}
	seen := fs.rendered.tooltipSeen
	changed := false
	for key, rec := range ts.records {
		drew := false
		for _, seenKey := range seen {
			if seenKey == key {
				drew = true
				break
			}
		}
		if drew {
			continue
		}
		rec.cancelGained()
		rec.cancelLost()
		if rec.handle.IsOpen() {
			rec.interest = false
			rec.handle.Hide()
			changed = true
		}
		delete(ts.records, key)
	}
	return changed
}

// ---------------------------------------------------------------------------
// The window's tooltip rendering (the UA stand-in)
// ---------------------------------------------------------------------------

// tooltipOpenRecords returns the window's open tooltip records in
// deterministic key order (at most a couple in practice — the hint
// stack's show rules close the non-ancestors).
func tooltipOpenRecords(w *Window) []*tooltipRecord {
	ts, ok := tooltipWindowStates[w]
	if !ok {
		return nil
	}
	keys := make([]string, 0, len(ts.records))
	for key := range ts.records {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var open []*tooltipRecord
	for _, key := range keys {
		rec := ts.records[key]
		if rec.handle.IsOpen() {
			open = append(open, rec)
		}
	}
	return open
}

// buildTooltipLayer builds one open tooltip's top-layer element: the
// hint-kind entry with the invoker link and the content (the custom
// builder's or the default look's).
func buildTooltipLayer(rec *tooltipRecord, w *Window, app *App) *TopLayerElement {
	layer := TopLayer(rec.handle).Kind(OverlayHint)
	if rec.invoker != "" {
		layer = layer.Invoker(rec.invoker)
	}
	layer.tooltipRec = rec
	return layer.Child(tooltipContent(rec, w, app))
}

// tooltipContent builds the tooltip's element: the custom builder's
// content, or the plain default look — the text in a tooltip-yellow
// box placed below the element, start-aligned, a 4px gap away
// (ticket 25's Anchored against the element's bounds cell, the
// interest invoker's implicit anchor).
func tooltipContent(rec *tooltipRecord, w *Window, app *App) AnyElement {
	if rec.spec != nil && rec.spec.builder != nil {
		return rec.spec.builder(w, app)
	}
	text := ""
	if rec.spec != nil {
		text = rec.spec.text
	}
	return Anchored().
		AnchorBounds(rec.boundsCell).
		Side(AnchorSideBottom).
		Align(AnchorAlignStart).
		Gap(tooltipGap).
		Child(Div().
			ID("tooltip").
			DebugSelector("tooltip").
			Absolute().
			Bg(Rgb(tooltipBackground)).
			TextColor(Rgb(tooltipTextColor)).
			P(Px(tooltipPadding)).
			Child(text)).
		IntoElement()
}

// The default look's numbers (the plain default the spec story asks
// for: a tooltip-yellow box with black text, shared by the core's
// default look and the fixture pages that pin it).
const (
	tooltipBackground = 0xffffe1
	tooltipTextColor  = 0x000000
	tooltipPadding    = 4.0
	tooltipGap        = 4.0
)

// prepaintTooltipElements prepaints the window's open tooltips'
// top-layer elements as roots (the inspector element's injection
// pattern: called after the app tree's prepaint, so the elements'
// bounds cells are current, and before the deferred round, so the
// entries' composites join it — they defer like every TopLayer
// element). The returned elements paint after the app tree.
func prepaintTooltipElements(w *Window, app *App, viewport Size) []AnyElement {
	records := tooltipOpenRecords(w)
	if len(records) == 0 {
		return nil
	}
	available := AvailableSize{
		Width:  DefiniteAvailableSpace(viewport.Width),
		Height: DefiniteAvailableSpace(viewport.Height),
	}
	layers := make([]AnyElement, 0, len(records))
	for _, rec := range records {
		layer := buildTooltipLayer(rec, w, app)
		element := layer.IntoElement()
		element.PrepaintAsRoot(Point{}, available, w, app)
		layers = append(layers, element)
	}
	return layers
}

// paintTooltipElements paints the prepainted tooltip layers (their
// registrations — the entry's kind, invoker and content path — land
// here, before the frame-end reconciliation reads them).
func paintTooltipElements(w *Window, app *App, elements []AnyElement) {
	for _, element := range elements {
		element.Paint(w, app)
	}
}
