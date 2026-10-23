package main

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

// Palette (the upstream showcase's light example palette).
var (
	colForeground = gpui.Rgb(0x171717)
	colMuted      = gpui.Rgb(0x737373)
	colBorder     = gpui.Rgb(0xd4d4d4)
	colSurface    = gpui.Rgb(0xffffff)
	colCanvas     = gpui.Rgb(0xfafafa)
	colHover      = gpui.Rgb(0xf5f5f5)
	colSelected   = gpui.Rgb(0xf0f0f0)
	colAccent     = gpui.Rgb(0x2563eb)
)

var frameworks = []string{"GPUI", "React", "SwiftUI", "Vue"}

const textViewMarkdown = "# TextView\n\n" +
	"Markdown parsed by **goldmark** into Base's node tree, rendered with " +
	"*inline styles*, `inline code` and [links](https://github.com/hoangvu12/gpui-go).\n\n" +
	"- Lists with **bold** items\n- [x] Task list items\n- Nested `code`\n\n" +
	"> Block quotes render with a rule.\n\n" +
	"```go\nfunc main() { fmt.Println(\"hello, base\") }\n```\n\n" +
	"| Component | Status |\n|---|---|\n| TextView | ported |\n| Plot | ported |\n"

// showcase is the root content view: every section's state lives here,
// as in the upstream BaseShowcase (examples/showcase/mod.rs).
type showcase struct {
	clicks      int
	toggled     bool
	checked     bool
	switchOn    bool
	selectedTab int
	selectOpen  bool
	selectIndex int
	comboOpen   bool
	comboIndex  int
	dialogOpen  bool

	tree     gpui.Entity[base.TreeState]
	textView gpui.Entity[base.TextViewState]
	dock     gpui.Entity[base.DockArea]
}

func newShowcase(w *gpui.Window, cx *gpui.Context[showcase]) showcase {
	app := cx.App()
	s := showcase{}
	s.tree = gpui.NewEntity(app, w.Scope(), func(st *base.TreeState, _ *gpui.Context[base.TreeState]) {
		*st = base.NewTreeState(app).Items([]base.TreeItem{
			base.NewTreeItem("src", "src").Expanded(true).WithChildren(
				base.NewTreeItem("components", "components").Expanded(true).WithChildren(
					base.NewTreeItem("button", "button.go"),
					base.NewTreeItem("tree-file", "tree.go"),
				),
				base.NewTreeItem("lib", "lib.go"),
			),
			base.NewTreeItem("examples", "examples").WithChildren(base.NewTreeItem("showcase", "main.go")),
			base.NewTreeItem("gomod", "go.mod"),
		})
	})
	s.textView = gpui.NewEntity(app, w.Scope(), func(st *base.TextViewState, tcx *gpui.Context[base.TextViewState]) {
		*st = base.TextViewStateMarkdown(textViewMarkdown, tcx)
	})
	s.dock = buildDock(w, app)
	return s
}

// update returns a click handler that mutates the showcase and redraws.
func (s *showcase) update(cx *gpui.Context[showcase], f func(s *showcase)) func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {
	this := cx.Entity().Downgrade()
	return func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
		_ = this.TryUpdate(app, func(s *showcase, cx *gpui.Context[showcase]) {
			f(s)
			cx.Notify()
		})
	}
}

func (s *showcase) Render(w *gpui.Window, cx *gpui.Context[showcase]) gpui.AnyElement {
	return gpui.Div().
		ID("showcase").
		SizeFull().
		OverflowYScroll().
		Bg(colCanvas).
		TextColor(colForeground).
		TextSm().
		Child(gpui.Div().
			Flex().FlexCol().Gap4().P6().
			Child(gpui.Div().TextXl().FontWeight(600).Child("GPUI Base for Go")).
			Child(gpui.Div().TextColor(colMuted).
				Child("Unstyled components from github.com/hoangvu12/gpui-go/base; every visual below is application styling.")).
			Child(gpui.Div().Flex().FlexWrap().Gap4().
				Child(card("Button, Toggle, Checkbox, Switch", s.buttons(cx))).
				Child(card("Select and Combobox", gpui.Div().Flex().Gap4().
					Child(s.selectExample(false, cx)).
					Child(s.selectExample(true, cx)))).
				Child(card("Tabs", s.tabs(cx))).
				Child(card("Dialog and Popover", gpui.Div().Flex().Gap2().
					Child(s.dialog(cx)).
					Child(s.popover()))).
				Child(card("Plot", plotExample())).
				Child(card("Resizable", resizableExample())).
				Child(card("Tree", s.treeExample())).
				Child(card("TextView (markdown)", s.textViewExample())).
				Child(card("Dock", s.dockExample())))).
		IntoElement()
}

// card frames one section.
func card(title string, content any) *gpui.DivElement {
	return gpui.Div().
		Flex().FlexCol().Gap3().P4().
		MinW(gpui.Px(300)).
		Bg(colSurface).
		Border1().BorderColor(colBorder).RoundedMd().
		Child(gpui.Div().TextXs().FontWeight(600).TextColor(colMuted).Child(title)).
		Child(content)
}

// button styles a base.Button like the upstream showcase's buttons.
func button[I gpui.IntoElementID](id I, label string, primary bool) *base.Button {
	b := base.NewButton(id).
		H7().Px3().LineHeight(gpui.Fraction(1)).
		Flex().ItemsCenter().JustifyCenter().
		TextXs().Border1().RoundedMd()
	if primary {
		b = b.BorderColor(colForeground).Bg(colForeground).TextColor(colSurface).
			Hover(gpui.Styles().Bg(gpui.Rgb(0x404040)))
	} else {
		b = b.BorderColor(colBorder).Bg(colSurface).
			Hover(gpui.Styles().Bg(colHover))
	}
	return b.Child(label)
}

func (s *showcase) buttons(cx *gpui.Context[showcase]) any {
	this := cx.Entity().Downgrade()
	return gpui.Div().Flex().FlexCol().Gap3().
		Child(gpui.Div().Flex().ItemsCenter().Gap2().
			Child(button("primary-button", "Save changes", true).
				OnClick(s.update(cx, func(s *showcase) { s.clicks++ }))).
			Child(button("secondary-button", "Cancel", false)).
			Child(gpui.Div().TextXs().TextColor(colMuted).Child(fmt.Sprintf("clicked %d×", s.clicks)))).
		Child(gpui.Div().Flex().ItemsCenter().Gap3().
			Child(base.NewToggle("bold-toggle").
				Pressed(s.toggled).
				H7().Px3().Flex().ItemsCenter().TextXs().Border1().BorderColor(colBorder).RoundedMd().
				Styles(func(st base.ToggleStyles) base.ToggleStyles {
					return st.Pressed(func(p *base.StateStyle) *base.StateStyle { return p.Bg(colSelected).BorderColor(colForeground) })
				}).
				OnChange(func(pressed bool, _ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
					set(this, app, func(s *showcase) { s.toggled = pressed })
				}).
				Child("Bold")).
			Child(base.NewCheckbox("terms").
				Checked(s.checked).
				Flex().ItemsCenter().Gap2().TextXs().
				OnChange(func(next base.CheckboxState, _ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
					set(this, app, func(s *showcase) { s.checked = next == base.CheckboxStateChecked })
				}).
				Child(gpui.Div().Size4().Border1().BorderColor(colForeground).RoundedSm().
					Flex().ItemsCenter().JustifyCenter().TextXs().
					When(s.checked, func(d *gpui.DivElement) *gpui.DivElement {
						return d.Bg(colForeground).TextColor(colSurface).Child("✓")
					})).
				Child("Accept terms")).
			Child(base.NewSwitch("notifications").
				Checked(s.switchOn).
				W(gpui.Px(36)).H5().P(gpui.Px(2)).RoundedFull().Flex().
				Bg(colBorder).
				Styles(func(st base.SwitchStyles) base.SwitchStyles {
					return st.Checked(func(p *base.StateStyle) *base.StateStyle { return p.Bg(colAccent).JustifyEnd() })
				}).
				OnChange(func(checked bool, _ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
					set(this, app, func(s *showcase) { s.switchOn = checked })
				}).
				Child(gpui.Div().Size4().RoundedFull().Bg(colSurface))))
}

// set mutates the showcase from a value-change handler and redraws.
func set(this gpui.WeakEntity[showcase], app *gpui.App, f func(s *showcase)) {
	_ = this.TryUpdate(app, func(s *showcase, cx *gpui.Context[showcase]) {
		f(s)
		cx.Notify()
	})
}

// selectExample follows the upstream select.rs: the application owns the
// trigger, the option list and the selected value; Select / Combobox own
// the semantics, keyboard and focus, and Popup anchors the options.
func (s *showcase) selectExample(combobox bool, cx *gpui.Context[showcase]) any {
	open, selected := s.selectOpen, s.selectIndex
	prefix := "select"
	if combobox {
		open, selected, prefix = s.comboOpen, s.comboIndex, "combobox"
	}
	setOpen := func(s *showcase, v bool) {
		if combobox {
			s.comboOpen = v
		} else {
			s.selectOpen = v
		}
	}
	chevron := "▾"
	if open {
		chevron = "▴"
	}
	trigger := gpui.Div().ID(prefix + "-trigger").
		H7().Px2().TextXs().Flex().ItemsCenter().JustifyBetween().
		Border1().BorderColor(colForeground).RoundedMd().Bg(colSurface).
		OnClick(s.update(cx, func(s *showcase) { setOpen(s, !open) })).
		Child(frameworks[selected]).
		Child(chevron)
	options := gpui.Div().Mt1().P1().Border1().BorderColor(colForeground).RoundedMd().Bg(colSurface).TextXs()
	for ix, label := range frameworks {
		row := gpui.Div().ID(gpui.NamedIntegerElementID(prefix+"-option", uint64(ix))).
			Px2().Py1().Flex().JustifyBetween().
			Hover(gpui.Styles().Bg(colHover)).
			OnClick(s.update(cx, func(s *showcase) {
				if combobox {
					s.comboIndex = ix
				} else {
					s.selectIndex = ix
				}
				setOpen(s, false)
			})).
			Child(label)
		if ix == selected {
			row = row.Child("✓")
		}
		options = options.Child(row)
	}
	var root any
	if combobox {
		root = base.NewCombobox("example-combobox").Open(open).W40().Child(trigger)
	} else {
		this := cx.Entity().Downgrade()
		root = base.NewSelect("example-select").
			Open(open).
			OnOpenChange(func(next bool, _ *gpui.Window, app *gpui.App) {
				_ = this.TryUpdate(app, func(s *showcase, cx *gpui.Context[showcase]) {
					s.selectOpen = next
					cx.Notify()
				})
			}).
			AccessibilityLabel("Framework").
			W40().
			Child(trigger)
	}
	popup := base.NewPopup(prefix+"-options", root)
	if open {
		popup = popup.Content(options)
	}
	return gpui.Div().Flex().FlexCol().Gap1().
		Child(gpui.Div().TextXs().TextColor(colMuted).Child(map[bool]string{false: "Select", true: "Combobox"}[combobox])).
		Child(popup)
}

// tabs follows the upstream tabs.rs.
func (s *showcase) tabs(cx *gpui.Context[showcase]) any {
	labels := []string{"Overview", "Activity", "Settings"}
	bodies := [][2]string{
		{"Workspace overview", "12 components · 4 contributors · updated today"},
		{"Recent activity", "Button example was updated 8 minutes ago."},
		{"Project settings", "Manage notifications and member access."},
	}
	strip := base.NewTabs("example-tabs").Flex().Px2().Pt1().BorderB1().BorderColor(colBorder)
	for ix, label := range labels {
		selected := s.selectedTab == ix
		tab := base.NewTab(gpui.IntegerElementID(uint64(ix))).
			Selected(selected).
			SetPosition(ix+1, len(labels)).
			Px2().H7().Flex().ItemsCenter().BorderB2().
			OnClick(s.update(cx, func(s *showcase) { s.selectedTab = ix })).
			Child(label)
		if selected {
			tab = tab.BorderColor(colForeground).FontWeight(600)
		} else {
			tab = tab.BorderColor(colSurface).TextColor(colMuted)
		}
		strip = strip.Child(tab)
	}
	body := bodies[s.selectedTab]
	return gpui.Div().W72().TextXs().Border1().BorderColor(colBorder).
		Child(strip).
		Child(gpui.Div().MinH20().P3().
			Child(body[0]).
			Child(gpui.Div().Mt1().TextColor(colMuted).Child(body[1])))
}

// dialog follows the upstream dialog.rs.
func (s *showcase) dialog(cx *gpui.Context[showcase]) any {
	this := cx.Entity().Downgrade()
	return gpui.Div().
		Child(button("open-dialog", "Edit profile", true).
			OnClick(s.update(cx, func(s *showcase) { s.dialogOpen = true }))).
		Child(base.NewDialog().
			Open(s.dialogOpen).
			OnOpenChange(func(open bool, _ base.DialogChangeReason, _ *gpui.Window, app *gpui.App) {
				_ = this.TryUpdate(app, func(s *showcase, cx *gpui.Context[showcase]) {
					s.dialogOpen = open
					cx.Notify()
				})
			}).
			Backdrop(base.NewDialogBackdrop().Absolute().Inset0().Bg(gpui.Black()).Opacity(0.2)).
			Popup(base.NewDialogPopup().
				W72().P3().Flex().FlexCol().TextXs().
				Bg(colSurface).Border1().BorderColor(colBorder).RoundedMd().
				Child(base.NewDialogTitle().FontWeight(600).Child("Edit profile")).
				Child(base.NewDialogDescription().Mt2().TextColor(colMuted).
					Child("Update the public details shown on your profile.")).
				Child(gpui.Div().Mt3().Flex().JustifyEnd().Gap2().
					Child(base.NewDialogClose().Child(button("dialog-cancel", "Cancel", false))).
					Child(button("dialog-save", "Save changes", true).
						OnClick(s.update(cx, func(s *showcase) { s.dialogOpen = false }))))))
}

// popover follows the upstream popover.rs.
func (s *showcase) popover() any {
	return base.NewPopover("example-popover").
		Trigger(button("popover-trigger", "Open popover", false)).
		Content(func(_ *base.PopoverState, _ *gpui.Window, cx *gpui.Context[base.PopoverState]) any {
			state := cx.Entity().Downgrade()
			return gpui.Div().ID("popover-content").
				W64().P2().Flex().FlexCol().Gap2().TextXs().
				Bg(colSurface).Border1().BorderColor(colBorder).RoundedMd().
				Child("Workspace access").
				Child(gpui.Div().TextColor(colMuted).Child("Anyone with the link can view.")).
				Child(gpui.Div().Mt1().Flex().JustifyEnd().
					Child(button("popover-done", "Done", true).
						OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
							_ = state.TryUpdate(app, func(st *base.PopoverState, pcx *gpui.Context[base.PopoverState]) {
								st.Dismiss(w, pcx)
							})
						})))
		})
}

// resizableExample follows the upstream resizable.rs.
func resizableExample() any {
	nav := gpui.Div().SizeFull().P2().Flex().FlexCol().Gap1().
		BorderR1().BorderColor(colForeground).
		Child(gpui.Div().TextXs().TextColor(colMuted).Child("PROJECT"))
	for _, label := range []string{"Overview", "Components", "Settings"} {
		nav = nav.Child(gpui.Div().WFull().H(gpui.Px(26)).Px2().Flex().ItemsCenter().WhitespaceNowrap().Child(label))
	}
	return gpui.Div().W72().H40().TextXs().Border1().BorderColor(colForeground).
		Child(base.HResizable("example-resizable").
			Child(base.NewResizablePanel().Size(124).SizeRange(116, 210).Child(nav)).
			Child(base.NewResizablePanel().Child(gpui.Div().SizeFull().P2().Flex().FlexCol().Gap2().Bg(colSurface).
				Child("Workspace").
				Child(gpui.Div().TextColor(colMuted).Child("Drag the divider to resize navigation.")))))
}

// treeExample follows the upstream tree.rs (text chevrons instead of SVG).
func (s *showcase) treeExample() any {
	return base.NewTree(s.tree).
		W64().H48().Relative().TextSm().
		Border1().BorderColor(colBorder).Py1().
		ListStyle(gpui.Styles().FlexGrow1().SizeFull().Refinement()).
		Item(func(_ int, entry *base.TreeEntry, state base.TreeEntryState, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
			icon := ""
			if entry.IsFolder() {
				icon = "▸"
				if entry.IsExpanded() {
					icon = "▾"
				}
			}
			row := gpui.Div().H8().Mx1().Px2().Flex().ItemsCenter().Gap1()
			if state.IsSelected() {
				row = row.Bg(colSelected)
			}
			if depth := entry.Depth(); depth > 0 {
				row = row.Child(gpui.Div().FlexNone().W(gpui.Px(float32(depth) * 12)))
			}
			return row.
				Child(gpui.Div().W3().FlexNone().TextXs().Child(icon)).
				Child(entry.Item().Label).
				IntoElement()
		})
}

func (s *showcase) textViewExample() any {
	return gpui.Div().W(gpui.Px(420)).H(gpui.Px(320)).Border1().BorderColor(colBorder).OverflowHidden().
		Child(base.NewTextView(s.textView).SizeFull().Px4().Scrollable(true))
}

func (s *showcase) dockExample() any {
	return gpui.Div().W(gpui.Px(520)).H(gpui.Px(320)).Border1().BorderColor(colBorder).OverflowHidden().
		Child(gpui.ViewOf(s.dock))
}
