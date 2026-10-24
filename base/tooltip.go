package base

// An unstyled tooltip popup and the per-window tooltip provider
// (U:tooltip.rs).
//
// Go adaptations, recorded:
//
//   - Rust cancels a pending show or hide by dropping its Task
//     (`self.show_task = None`). The port cancels it explicitly
//     (overlayDelay.cancel): the delay is a background timer the
//     foreground continuation awaits, both cancelled together.
//   - TooltipTransition is a Rust enum with fields; the port is a struct
//     tagged by Kind.
//   - AnyView is gpui.View.
//   - Display (map.md gap 2b): the Tooltip base, the default renderer's
//     wrapper and the empty overlay (U:tooltip.rs:31, 122, 224) are plain
//     divs upstream, display Block under crates.io gpui 0.2.2 (the pin
//     defaults to Flex, R:style.rs:1024-1027), so they are built with
//     Block(); a caller's Flex() still wins.

import (
	"time"

	"github.com/hoangvu12/gpui-go/gpui"
)

// tooltipPriority is the deferred paint priority of tooltips, above
// popups (U:tooltip.rs:11 TOOLTIP_PRIORITY).
const tooltipPriority = 200

// tooltipWindowMargin is the distance kept from the window edge
// (U:tooltip.rs:12).
const tooltipWindowMargin float32 = 4

// tooltipGracePeriod keeps a hidden tooltip's successor immediate
// (U:tooltip.rs:13).
const tooltipGracePeriod = 300 * time.Millisecond

// tooltipShowDelay is the delay before a tooltip shows (U:tooltip.rs:14).
const tooltipShowDelay = 500 * time.Millisecond

// TooltipBuilder builds the tooltip content view (U:tooltip.rs:16).
type TooltipBuilder = func(w *gpui.Window, app *gpui.App) gpui.View

// TooltipRenderer presents the content view for a transition
// (U:tooltip.rs:17).
type TooltipRenderer = func(view gpui.View, transition TooltipTransition, w *gpui.Window, app *gpui.App) gpui.AnyElement

// Tooltip is an unstyled tooltip popup (U:tooltip.rs:19-26), Base UI's
// Tooltip.Popup: it owns the accessible tooltip role and accepts
// application-owned content and presentation. Its Styled and Parent
// methods reach the root div (U:tooltip.rs:36-46).
type Tooltip struct {
	gpui.Styled[*Tooltip]

	base *gpui.DivElement
}

// NewTooltip creates a tooltip popup (U:tooltip.rs:28-34 Tooltip::new).
func NewTooltip[I gpui.IntoElementID](id I) *Tooltip {
	base := gpui.Div().Block().ID(gpui.ElementIDOf(id)).Role(gpui.RoleTooltip)
	return gpui.BindStyledTo(&Tooltip{base: base}, base.StyleMut())
}

// Extend appends children (U:tooltip.rs:42-46).
func (t *Tooltip) Extend(elements []gpui.AnyElement) { t.base.Extend(elements) }

// Child adds one child (ParentElement::child).
func (t *Tooltip) Child(value any) *Tooltip {
	t.base.Child(value)
	return t
}

// Children adds the children in order (ParentElement::children).
func (t *Tooltip) Children(values ...any) *Tooltip {
	t.base.Children(values...)
	return t
}

// RenderOnce implements gpui.RenderOnce (U:tooltip.rs:48-52).
func (t *Tooltip) RenderOnce(*gpui.Window, *gpui.App) gpui.AnyElement {
	return t.base.IntoElement()
}

// TooltipRequest is the content requested by a tooltip trigger
// (U:tooltip.rs:54-60).
type TooltipRequest struct {
	build              TooltipBuilder
	triggerBounds      gpui.Bounds
	preferredPlacement gpui.Optional[Placement]
}

// NewTooltipRequest requests a tooltip built by build next to
// triggerBounds (U:tooltip.rs:62-72 TooltipRequest::new).
func NewTooltipRequest(triggerBounds gpui.Bounds, build TooltipBuilder) TooltipRequest {
	return TooltipRequest{build: build, triggerBounds: triggerBounds}
}

// Placement sets the preferred side (U:tooltip.rs:74-77).
func (r TooltipRequest) Placement(placement Placement) TooltipRequest {
	r.preferredPlacement = gpui.Some(placement)
	return r
}

// TooltipTransitionKind selects the TooltipTransition variant.
type TooltipTransitionKind uint8

const (
	// TooltipTransitionEnter is a tooltip appearing.
	TooltipTransitionEnter TooltipTransitionKind = iota
	// TooltipTransitionSwitch is a visible tooltip moving to another
	// trigger.
	TooltipTransitionSwitch
)

// TooltipTransition is the presentation transition requested by the
// tooltip lifecycle (U:tooltip.rs:80-91): Enter{epoch}, or
// Switch{epoch, previous, current} with the previous and current trigger
// bounds.
type TooltipTransition struct {
	Kind     TooltipTransitionKind
	Epoch    int
	Previous gpui.Bounds
	Current  gpui.Bounds
}

// TooltipOverlay is the per-window tooltip provider and overlay
// (U:tooltip.rs:93-108). Show requests are ignored on iOS and Android,
// where touch input must not open hover tooltips.
type TooltipOverlay struct {
	enabled          bool
	content          *TooltipRequest
	previousBounds   *gpui.Bounds
	epoch            int
	hadRecentTooltip bool
	animationEpoch   int
	isSwitching      bool
	showTask         *overlayDelay
	hideTask         *overlayDelay
	renderer         TooltipRenderer
}

// NewTooltipOverlay creates the provider (U:tooltip.rs:110-124
// TooltipOverlay::new); the default renderer wraps the view in a div.
func NewTooltipOverlay() TooltipOverlay {
	return TooltipOverlay{
		enabled: !IsMobile(),
		renderer: func(view gpui.View, _ TooltipTransition, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
			return gpui.Div().Block().Child(view).IntoElement()
		},
	}
}

// DefaultTooltipOverlay is NewTooltipOverlay (U:tooltip.rs:215-219).
func DefaultTooltipOverlay() TooltipOverlay { return NewTooltipOverlay() }

// RenderWith sets the presentation renderer (U:tooltip.rs:126-132).
func (o TooltipOverlay) RenderWith(renderer TooltipRenderer) TooltipOverlay {
	o.renderer = renderer
	return o
}

// nextEpoch advances the request epoch (U:tooltip.rs:134-137).
func (o *TooltipOverlay) nextEpoch() int {
	o.epoch++
	return o.epoch
}

// RequestShow asks for content to show (U:tooltip.rs:139-175): after the
// show delay, or immediately while a tooltip is visible or was hidden
// within the grace period (a switch).
func (o *TooltipOverlay) RequestShow(content TooltipRequest, w *gpui.Window, cx *gpui.Context[TooltipOverlay]) {
	// Gate both delayed display and the immediate grace-period switch;
	// keep this in Base so every managed component shares the policy.
	if !o.enabled {
		return
	}
	o.hideTask.cancel()
	o.hideTask = nil
	wasVisible := o.content != nil
	if wasVisible || o.hadRecentTooltip {
		o.previousBounds = nil
		if o.content != nil {
			previous := o.content.triggerBounds
			o.previousBounds = &previous
		}
		o.content = &content
		o.showTask.cancel()
		o.showTask = nil
		o.isSwitching = wasVisible
		o.animationEpoch++
		cx.Notify()
		return
	}

	epoch := o.nextEpoch()
	o.showTask = overlayAfter(cx, w, tooltipShowDelay, func(o *TooltipOverlay, _ *gpui.Window, cx *gpui.Context[TooltipOverlay]) {
		if o.epoch == epoch {
			o.content = &content
			o.previousBounds = nil
			o.isSwitching = false
			o.animationEpoch++
			cx.Notify()
		}
	})
}

// RequestHide asks the visible tooltip to hide after the grace period
// (U:tooltip.rs:177-195).
func (o *TooltipOverlay) RequestHide(w *gpui.Window, cx *gpui.Context[TooltipOverlay]) {
	o.showTask.cancel()
	o.showTask = nil
	if o.content == nil {
		return
	}
	epoch := o.nextEpoch()
	o.hadRecentTooltip = true
	o.hideTask = overlayAfter(cx, w, tooltipGracePeriod, func(o *TooltipOverlay, _ *gpui.Window, cx *gpui.Context[TooltipOverlay]) {
		if o.epoch == epoch {
			o.content = nil
			o.previousBounds = nil
			o.hadRecentTooltip = false
			cx.Notify()
		}
	})
}

// Hide hides the tooltip immediately and forgets the grace period
// (U:tooltip.rs:197-212).
func (o *TooltipOverlay) Hide(cx *gpui.Context[TooltipOverlay]) {
	changed := o.content != nil || o.previousBounds != nil || o.hadRecentTooltip || o.showTask != nil || o.hideTask != nil
	o.content = nil
	o.previousBounds = nil
	o.hadRecentTooltip = false
	o.isSwitching = false
	o.showTask.cancel()
	o.showTask = nil
	o.hideTask.cancel()
	o.hideTask = nil
	if changed {
		cx.Notify()
	}
}

// Render renders the content deferred above popups, positioned beside
// the trigger (U:tooltip.rs:221-248).
func (o *TooltipOverlay) Render(w *gpui.Window, cx *gpui.Context[TooltipOverlay]) gpui.AnyElement {
	content := o.content
	if content == nil {
		return gpui.Div().Block().IntoElement()
	}
	view := content.build(w, cx.App())
	transition := TooltipTransition{Kind: TooltipTransitionEnter, Epoch: o.animationEpoch}
	if o.isSwitching && o.previousBounds != nil {
		transition = TooltipTransition{
			Kind:     TooltipTransitionSwitch,
			Epoch:    o.animationEpoch,
			Previous: *o.previousBounds,
			Current:  content.triggerBounds,
		}
	}
	rendered := o.renderer(view, transition, w, cx.App())
	positioner := NewTooltipPositioner(&content.triggerBounds)
	if placement, ok := content.preferredPlacement.Get(); ok {
		positioner.Placement(placement)
	}
	return gpui.Deferred(positioner.Child(rendered)).Priority(tooltipPriority).IntoElement()
}

// TooltipPositioner is an unstyled tooltip positioner with
// viewport-aware flipping and clamping (U:tooltip.rs:250-279): a
// tooltip-named view of the core's element-anchored Anchored side
// placement (browser-core ticket 25 moved the positioner's side
// strategy into gpui.Anchored — position-anchor, the side/align
// placement, the flip fallback and the shift clamp — so every anchored
// surface in Base resolves its position through the same core rule as
// the browser's CSS anchor positioning). It adds no element of its
// own; the core Anchored is what gets rendered.
type TooltipPositioner struct {
	anchored *gpui.AnchoredElement
}

// NewTooltipPositioner positions beside the tracked trigger bounds
// with the tooltip window margin (U:tooltip.rs:257-259): top side by
// default, center-aligned, flipping to the opposite side when the
// preferred one does not fit and shifting into the viewport otherwise
// (the positioner's flip-then-clamp). The side-axis flip tactic is
// picked per side — flip-block flips a vertical side and flip-inline a
// horizontal one (CSS's tactics mirror across the anchor's midlines,
// so the tactic that flips the side depends on the side's axis).
func NewTooltipPositioner(triggerBounds *gpui.Bounds) *TooltipPositioner {
	return &TooltipPositioner{anchored: gpui.Anchored().
		AnchorBounds(triggerBounds).
		Side(gpui.AnchorSideTop).
		Align(gpui.AnchorAlignCenter).
		TryFallbacks(gpui.AnchorFlipBlock, gpui.AnchorShift).
		Margin(edgesAll(tooltipWindowMargin))}
}

// Placement sets the preferred side (U:tooltip.rs:261-264).
func (t *TooltipPositioner) Placement(placement Placement) *TooltipPositioner {
	t.anchored = t.anchored.Side(anchorSideOf(placement)).TryFallbacks(anchorSideFlip(placement), gpui.AnchorShift)
	return t
}

// Extend appends children (U:tooltip.rs:267-271).
func (t *TooltipPositioner) Extend(elements []gpui.AnyElement) {
	for _, element := range elements {
		t.anchored.Child(element)
	}
}

// Child adds one child (ParentElement::child).
func (t *TooltipPositioner) Child(value any) *TooltipPositioner {
	t.anchored.Child(value)
	return t
}

// IntoElement renders the core Anchored (U:tooltip.rs:273-279).
func (t *TooltipPositioner) IntoElement() gpui.AnyElement { return t.anchored.IntoElement() }

// anchorSideOf maps the placement onto the anchored side.
func anchorSideOf(placement Placement) gpui.AnchorSide {
	switch placement {
	case PlacementBottom:
		return gpui.AnchorSideBottom
	case PlacementLeft:
		return gpui.AnchorSideLeft
	case PlacementRight:
		return gpui.AnchorSideRight
	}
	return gpui.AnchorSideTop
}

// anchorSideFlip returns the flip tactic that flips the side the
// placement sits on: flip-block on a vertical side, flip-inline on a
// horizontal one (the CSS tactics mirror across the anchor's
// midlines).
func anchorSideFlip(placement Placement) gpui.AnchorFallback {
	switch placement {
	case PlacementLeft, PlacementRight:
		return gpui.AnchorFlipInline
	}
	return gpui.AnchorFlipBlock
}

// overlayDelay is a pending delayed update of an overlay state: the
// background timer and the foreground continuation awaiting it (the
// pinned `cx.spawn_in(window, async move |this, cx| {
// cx.background_executor().timer(delay).await; this.update_in(...) })`,
// U:tooltip.rs:163-174, U:hover_card.rs:138-160).
type overlayDelay struct {
	timer gpui.Task[struct{}]
	body  gpui.Task[struct{}]
}

// overlayAfter runs f with the entity and the window once delay has
// elapsed on the scheduler clock (the virtual clock under TestApp, wall
// time in a real application). The update is skipped when the entity
// released or the window closed (the pinned `let _ = this.update_in`).
func overlayAfter[S any](cx *gpui.Context[S], w *gpui.Window, delay time.Duration, f func(*S, *gpui.Window, *gpui.Context[S])) *overlayDelay {
	d := &overlayDelay{}
	d.timer = cx.App().Background().Spawn(func(run *gpui.TaskRun) struct{} {
		run.Sleep(delay)
		return struct{}{}
	})
	timer := d.timer
	d.body = cx.SpawnIn(w, func(this gpui.WeakEntity[S], acx *gpui.AsyncWindowContext) struct{} {
		acx.AsyncApp.Await(timer)
		if timer.Cancelled() || d.body.Cancelled() {
			return struct{}{}
		}
		_ = acx.Update(func(w *gpui.Window, app *gpui.App) {
			_ = this.TryUpdateIn(app, w, f)
		})
		return struct{}{}
	})
	return d
}

// cancel drops the pending update (the pinned Task drop). It is a no-op
// on nil.
func (d *overlayDelay) cancel() {
	if d == nil {
		return
	}
	d.timer.Cancel()
	d.body.Cancel()
}
