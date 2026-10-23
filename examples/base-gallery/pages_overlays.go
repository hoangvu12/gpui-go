package main

import (
	"fmt"
	"time"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

var overlayPages = []page{
	{"popup", "Popup", "popup.rs", (*gallery).popupPage},
	{"popover", "Popover", "popover.rs", (*gallery).popoverPage},
	{"tooltip", "Tooltip", "tooltip.rs", (*gallery).tooltipPage},
	{"hover-card", "HoverCard", "hover_card.rs", (*gallery).hoverCardPage},
	{"dialog", "Dialog", "dialog.rs", (*gallery).dialogPage},
	{"alert-dialog", "AlertDialog", "alert_dialog.rs", (*gallery).alertDialogPage},
	{"sheet", "Sheet", "sheet.rs", (*gallery).sheetPage},
	{"toast", "Toast", "toast.rs", (*gallery).toastPage},
}

type toastItem struct {
	id      int
	title   string
	body    string
	failure bool
}

type overlayState struct {
	popupOpen      bool
	popupAboveOpen bool
	popoverOpen    bool
	tooltipHover   bool
	tooltipPinned  bool
	dialogOpen     bool
	dialogHandle   *base.DialogHandle
	dialogResult   string
	alertOpen      bool
	alertResult    string
	sheetOpen      bool
	sheetSide      int
	toasts         []toastItem
	nextToast      int
}

func (g *gallery) initOverlays(_ *gpui.Window, _ *gpui.Context[gallery]) {
	g.dialogHandle = base.NewDialogHandle(false)
	g.dialogResult = "No decision yet"
	g.alertResult = "Nothing deleted"
}

// surface is the frame of every floating panel.
func surface(width float32) *gpui.DivElement {
	return gpui.Div().W(gpui.Px(width)).P3().Flex().FlexCol().Gap2().TextSm().
		Bg(pal.surface).Border1().BorderColor(pal.border).RoundedLg().
		Shadow(base.ShadowTokensElevations(gpui.Black().Opacity(0.18)).Md)
}

func (g *gallery) popupPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	popup := func(id string, open bool, anchor gpui.Anchor, label string, toggle func(g *gallery)) any {
		p := base.NewPopup(id, btn(id+"-trigger", map[bool]string{true: "Close " + label, false: "Open " + label}[open], primary, md).
			OnClick(g.update(cx, toggle))).
			Anchor(anchor).
			Offset(6)
		if open {
			p = p.Content(surface(240).
				Child(gpui.Div().FontWeight(600).Child("Anchored surface")).
				Child(note("Popup positions content relative to its trigger and snaps to the window edge.")))
		}
		return p
	}
	return col(
		demo("Below the trigger (top-left corner anchored)", popup("popup-below", g.popupOpen, gpui.AnchorTopLeft, "popup",
			func(g *gallery) { g.popupOpen = !g.popupOpen })),
		demo("Above the trigger (bottom-left corner anchored)", gpui.Div().Pt(gpui.Px(120)).
			Child(popup("popup-above", g.popupAboveOpen, gpui.AnchorBottomLeft, "upward popup",
				func(g *gallery) { g.popupAboveOpen = !g.popupAboveOpen }))),
	)
}

func popoverContent(title, body string) base.PopoverContentBuilder {
	return func(_ *base.PopoverState, _ *gpui.Window, cx *gpui.Context[base.PopoverState]) any {
		state := cx.Entity().Downgrade()
		return surface(260).ID("popover-content").
			Child(gpui.Div().FontWeight(600).Child(title)).
			Child(note(body)).
			Child(gpui.Div().Mt1().Flex().JustifyEnd().
				Child(btn("popover-done", "Done", primary, sm).
					OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
						_ = state.TryUpdate(app, func(st *base.PopoverState, pcx *gpui.Context[base.PopoverState]) {
							st.Dismiss(w, pcx)
						})
					})))
	}
}

func (g *gallery) popoverPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	this := cx.Entity().Downgrade()
	return col(
		demo("Uncontrolled (click to open, click outside or Escape to close)", base.NewPopover("share-popover").
			Trigger(btn("share-trigger", "Share", outline, md)).
			Content(popoverContent("Workspace access", "Anyone with the link can view."))),
		demo("Controlled, top-right corner anchored (opens to the left)", row(
			base.NewPopover("controlled-popover").
				Anchor(gpui.AnchorTopRight).
				Open(g.popoverOpen).
				OnOpenChange(func(open bool, _ *gpui.Window, app *gpui.App) {
					set(this, app, func(g *gallery) { g.popoverOpen = open })
				}).
				Trigger(btn("controlled-trigger", "Filters", outline, md)).
				Content(popoverContent("Filters", "This popover's open state lives in the gallery.")),
			note(map[bool]string{true: "open", false: "closed"}[g.popoverOpen]),
		)),
		demo("Right-click trigger", base.NewPopover("context-popover").
			MouseButton(gpui.MouseButtonRight).
			Trigger(btn("context-trigger", "Right-click me", ghost, md)).
			Content(popoverContent("Context actions", "Opened with the secondary mouse button."))),
	)
}

func (g *gallery) tooltipPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	this := cx.Entity().Downgrade()
	tip := func(id, text string) *base.Tooltip {
		return base.NewTooltip(id).
			Px2().Py1().TextXs().RoundedMd().
			Bg(pal.primary).TextColor(pal.primaryFg).
			Child(text)
	}
	visible := g.tooltipHover || g.tooltipPinned
	hoverTrigger := gpui.Div().ID("tooltip-trigger").
		OnHover(func(hovered *bool, _ *gpui.Window, app *gpui.App) {
			h := *hovered
			set(this, app, func(g *gallery) { g.tooltipHover = h })
		}).
		Child(btn("tooltip-anchor", "Command menu", outline, md))
	hoverPopup := base.NewPopup("tooltip-popup", hoverTrigger).Anchor(gpui.AnchorBottomCenter).Offset(6)
	if visible {
		hoverPopup = hoverPopup.Content(tip("command-tooltip", "Open command menu · Ctrl K"))
	}
	return col(
		demo("Hover to show (application-owned visibility)", gpui.Div().Pt(gpui.Px(36)).Child(hoverPopup)),
		demo("Pinned open", row(
			themedSwitch("pin-tooltip", g.tooltipPinned, false, func(next bool, app *gpui.App) {
				set(this, app, func(g *gallery) { g.tooltipPinned = next })
			}),
			note("Keep the tooltip above visible"),
		)),
		demo("Tooltip surfaces", row(
			tip("tip-plain", "Plain tooltip"),
			base.NewTooltip("tip-rich").P2().W(gpui.Px(200)).TextXs().RoundedMd().
				Bg(pal.surface).Border1().BorderColor(pal.border).
				Child(gpui.Div().FontWeight(600).Child("Rich tooltip")).
				Child(note("A tooltip can hold any element.")),
		)),
	)
}

func (g *gallery) hoverCardPage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	card := func(_ *base.HoverCardState, _ *gpui.Window, _ *gpui.Context[base.HoverCardState]) *gpui.DivElement {
		return surface(240).ID("hover-content").
			Child(gpui.Div().Flex().ItemsCenter().Gap2().
				Child(gpui.Div().Size8().RoundedFull().Flex().ItemsCenter().JustifyCenter().
					Bg(pal.elevated).FontWeight(600).Child("G")).
				Child(gpui.Div().Flex().FlexCol().
					Child(gpui.Div().FontWeight(600).Child("gpui-go")).
					Child(note("@hoangvu12")))).
			Child(note("A Go port of GPUI and its unstyled Base components."))
	}
	return col(
		demo("Hover the link (opens after 0.6 s, closes after 0.3 s)", gpui.Div().Pt(gpui.Px(130)).Child(
			base.NewHoverCard("profile-card").
				Trigger(gpui.Div().ID("hover-trigger").TextColor(pal.accent).Underline().Child("@gpui-go")).
				Content(card))),
		demo("Fast card, anchored below", base.NewHoverCard("fast-card").
			Anchor(gpui.AnchorTopLeft).
			OpenDelay(100*time.Millisecond).
			CloseDelay(100*time.Millisecond).
			Trigger(btn("fast-hover-trigger", "Hover me", outline, md)).
			Content(card)),
	)
}

func (g *gallery) dialogPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	this := cx.Entity().Downgrade()
	popup := func(title, description string, footer any) *base.DialogPopup {
		return base.NewDialogPopup().
			W(gpui.Px(360)).P5().Flex().FlexCol().Gap2().TextSm().
			Bg(pal.surface).Border1().BorderColor(pal.border).RoundedLg().
			Shadow(base.ShadowTokensElevations(gpui.Black().Opacity(0.25)).Lg).
			Child(base.NewDialogTitle().TextBase().FontWeight(600).Child(title)).
			Child(base.NewDialogDescription().TextColor(pal.muted).Child(description)).
			Child(gpui.Div().Mt3().Flex().JustifyEnd().Gap2().Child(footer))
	}
	backdrop := func() *base.DialogBackdrop {
		return base.NewDialogBackdrop().Absolute().Inset0().Bg(gpui.Black()).Opacity(0.35)
	}
	controlled := base.NewDialog().
		Open(g.dialogOpen).
		OnOpenChange(func(open bool, reason base.DialogChangeReason, _ *gpui.Window, app *gpui.App) {
			set(this, app, func(g *gallery) {
				g.dialogOpen = open
				if !open {
					g.dialogResult = fmt.Sprintf("Closed (reason %d)", reason)
				}
			})
		}).
		Backdrop(backdrop()).
		Popup(popup("Edit profile", "Update the public details shown on your profile.",
			gpui.Div().Flex().Gap2().
				Child(base.NewDialogClose().Child(btn("dialog-cancel", "Cancel", outline, md))).
				Child(btn("dialog-save", "Save changes", primary, md).
					OnClick(g.update(cx, func(g *gallery) {
						g.dialogOpen = false
						g.dialogResult = "Saved"
					})))))
	handle := g.dialogHandle
	uncontrolled := base.NewDialog().
		Handle(handle).
		Backdrop(backdrop()).
		Popup(popup("Keyboard shortcuts", "Escape closes this dialog; it is driven by a DialogHandle, not gallery state.",
			base.NewDialogClose().Trigger(func(b *base.Button) any {
				return b.H8().Px3().Flex().ItemsCenter().Border1().BorderColor(pal.border).RoundedMd().
					Hover(gpui.Styles().Bg(pal.hover)).Child("Close")
			})))
	return col(
		demo("Controlled dialog (open flag in the gallery)", row(
			btn("open-dialog", "Edit profile", primary, md).OnClick(g.update(cx, func(g *gallery) { g.dialogOpen = true })),
			note(g.dialogResult),
			controlled,
		)),
		demo("DialogTrigger + DialogHandle", row(
			base.NewDialogTrigger(btn("handle-dialog-trigger", "Show shortcuts", outline, md)).Handle(handle),
			uncontrolled,
		)),
	)
}

func (g *gallery) alertDialogPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	this := cx.Entity().Downgrade()
	alert := base.NewAlertDialog().
		Open(g.alertOpen).
		OnOpenChange(func(open bool, _ base.DialogChangeReason, _ *gpui.Window, app *gpui.App) {
			set(this, app, func(g *gallery) { g.alertOpen = open })
		}).
		OnOK(func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) bool {
			set(this, app, func(g *gallery) {
				g.alertOpen = false
				g.alertResult = "Project deleted"
			})
			return true
		}).
		Backdrop(base.NewAlertDialogBackdrop().Absolute().Inset0().Bg(gpui.Black()).Opacity(0.35)).
		Popup(base.NewAlertDialogPopup().
			W(gpui.Px(380)).P5().Flex().FlexCol().Gap2().TextSm().
			Bg(pal.surface).Border1().BorderColor(pal.border).RoundedLg().
			Child(base.NewAlertDialogTitle().TextBase().FontWeight(600).Child("Delete project?")).
			Child(base.NewAlertDialogDescription().TextColor(pal.muted).
				Child("This permanently deletes Acme Studio and all of its data. The backdrop does not dismiss an alert.")).
			Child(gpui.Div().Mt3().Flex().JustifyEnd().Gap2().
				Child(base.NewAlertDialogCancel().Child(btn("cancel-delete", "Cancel", outline, md).
					OnClick(g.update(cx, func(g *gallery) {
						g.alertOpen = false
						g.alertResult = "Cancelled"
					})))).
				Child(base.NewAlertDialogAction().Child(btn("confirm-delete", "Delete", destructive, md).
					OnClick(g.update(cx, func(g *gallery) {
						g.alertOpen = false
						g.alertResult = "Project deleted"
					}))))))
	return col(
		demo("Destructive confirmation", row(
			btn("open-alert", "Delete project", destructive, md).OnClick(g.update(cx, func(g *gallery) { g.alertOpen = true })),
			note(g.alertResult),
			alert,
		)),
	)
}

func (g *gallery) sheetPage(w *gpui.Window, cx *gpui.Context[gallery]) any {
	sides := []string{"Right", "Left", "Bottom"}
	viewport := w.ViewportSize()
	panel := gpui.Div().Absolute().P5().Flex().FlexCol().Gap3().TextSm().
		Bg(pal.surface).Border1().BorderColor(pal.border).
		Child(gpui.Div().TextBase().FontWeight(600).Child("Settings")).
		Child(labelled("Workspace name", gpui.Div().H8().Px2().W(gpui.Px(220)).Flex().ItemsCenter().
			Border1().BorderColor(pal.border).RoundedMd().Child("Acme Studio"))).
		Child(note("Update the workspace preferences for your team.")).
		Child(gpui.Div().Py2().BorderT1().BorderColor(pal.border).Child("Notifications · Enabled")).
		Child(gpui.Div().Flex().JustifyEnd().
			Child(btn("close-sheet", "Done", primary, md).OnClick(g.update(cx, func(g *gallery) { g.sheetOpen = false }))))
	switch g.sheetSide {
	case 1:
		panel = panel.Left0().Top0().H(gpui.Px(viewport.Height)).W(gpui.Px(300))
	case 2:
		panel = panel.Left0().Bottom0().W(gpui.Px(viewport.Width)).H(gpui.Px(300))
	default:
		panel = panel.Right0().Top0().H(gpui.Px(viewport.Height)).W(gpui.Px(300))
	}
	this := cx.Entity().Downgrade()
	triggers := row()
	for ix, side := range sides {
		triggers = triggers.Child(btn("open-sheet-"+side, "Open "+side, outline, md).
			OnClick(g.update(cx, func(g *gallery) {
				g.sheetSide = ix
				g.sheetOpen = true
			})))
	}
	view := col(
		demo("Modal sheet from a window edge (click the overlay or press Escape to close)", triggers),
	)
	if g.sheetOpen {
		// The sheet anchors at the window origin but draws in place, so
		// the page's scroll area would clip it; deferring it paints it
		// above the whole window, as Dialog does for itself.
		view = view.Child(gpui.Deferred(base.NewSheet().
			RequestClose(func(_ *gpui.Window, app *gpui.App) {
				set(this, app, func(g *gallery) { g.sheetOpen = false })
			}).
			Overlay(gpui.Div().Absolute().Inset0().Bg(gpui.Black()).Opacity(0.3)).
			Surface(panel)).Priority(10))
	}
	return view
}

func (g *gallery) toastPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	push := func(failure bool) func(g *gallery) {
		return func(g *gallery) {
			g.nextToast++
			item := toastItem{id: g.nextToast, title: "Changes saved", body: "Your preferences are now up to date."}
			if failure {
				item.title, item.body, item.failure = "Upload failed", "The server rejected the file. Try again.", true
			}
			g.toasts = append(g.toasts, item)
			if len(g.toasts) > 3 {
				g.toasts = g.toasts[len(g.toasts)-3:]
			}
		}
	}
	stack := gpui.Div().Absolute().Right(gpui.Px(12)).Bottom(gpui.Px(12)).Flex().FlexCol().Gap2()
	for _, item := range g.toasts {
		id := item.id
		accent := pal.series[1]
		if item.failure {
			accent = pal.danger
		}
		stack = stack.Child(base.NewToast(gpui.NamedIntegerElementID("toast", uint64(id))).
			TransitionStatus(base.ToastPresent).
			W(gpui.Px(280)).P3().TextSm().
			Bg(pal.surface).Border1().BorderColor(pal.border).BorderL4().RoundedLg().
			Shadow(base.ShadowTokensElevations(gpui.Black().Opacity(0.15)).Md).
			Child(gpui.Div().Flex().ItemsCenter().JustifyBetween().
				Child(gpui.Div().FontWeight(600).TextColor(accent).Child(item.title)).
				Child(btn(gpui.NamedIntegerElementID("dismiss-toast", uint64(id)), "×", ghost, sm).W6().Px0().
					AccessibilityLabel("Dismiss").
					OnClick(g.update(cx, func(g *gallery) {
						for ix, t := range g.toasts {
							if t.id == id {
								g.toasts = append(g.toasts[:ix:ix], g.toasts[ix+1:]...)
								break
							}
						}
					})))).
			Child(gpui.Div().Mt1().TextXs().TextColor(pal.muted).Child(item.body)))
	}
	area := gpui.Div().Relative().W(gpui.Px(560)).H(gpui.Px(300)).
		Border1().BorderColor(pal.border).RoundedLg().Bg(pal.canvas).
		Child(gpui.Div().P4().Flex().Gap2().
			Child(btn("show-toast", "Show success toast", primary, md).OnClick(g.update(cx, push(false)))).
			Child(btn("show-error-toast", "Show error toast", outline, md).OnClick(g.update(cx, push(true))))).
		Child(stack)
	return col(
		demo("Toasts stacked in a region (up to three; × dismisses)", area),
		note("Toast owns the Alert role and the lifecycle phase; presentation and motion are application-owned."),
	)
}
