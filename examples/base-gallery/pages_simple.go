package main

import (
	"fmt"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

var simplePages = []page{
	{"button", "Button", "button.rs", (*gallery).buttonPage},
	{"toggle", "Toggle & ToggleGroup", "toggle.rs, toggle_group.rs", (*gallery).togglePage},
	{"checkbox", "Checkbox", "checkbox.rs", (*gallery).checkboxPage},
	{"radio", "Radio & RadioGroup", "radio.rs, radio_group.rs", (*gallery).radioPage},
	{"switch", "Switch", "switch.rs", (*gallery).switchPage},
	{"link", "Link", "link.rs", (*gallery).linkPage},
	{"tabs", "Tabs", "tabs.rs", (*gallery).tabsPage},
	{"toolbar", "Toolbar", "toolbar.rs", (*gallery).toolbarPage},
	{"accordion", "Accordion", "accordion.rs", (*gallery).accordionPage},
	{"collapsible", "Collapsible", "collapsible.rs", (*gallery).collapsiblePage},
	{"avatar", "Avatar", "avatar.rs", (*gallery).avatarPage},
	{"progress", "Progress", "progress.rs", (*gallery).progressPage},
	{"slider", "Slider", "slider.rs", (*gallery).sliderPage},
	{"pagination", "Pagination", "pagination.rs", (*gallery).paginationPage},
	{"table", "Table", "table.rs", (*gallery).tablePage},
}

type simpleState struct {
	clicks            int
	bold, italic      bool
	underline         bool
	align             int
	checks            [3]bool
	terms             bool
	radio             int
	plan              int
	switches          [2]bool
	tab               int
	pillTab           int
	toolbarAction     string
	toolbarSearch     gpui.Entity[base.InputState]
	accordion         [3]bool
	collapsible       bool
	collapsibleClosed bool
	page              int
	pageLong          int
	slider            gpui.Entity[base.SliderState]
	sliderStep        gpui.Entity[base.SliderState]
	sliderRange       gpui.Entity[base.SliderState]
	sliderVertical    gpui.Entity[base.SliderState]
	progressTick      int
}

func (g *gallery) initSimple(w *gpui.Window, cx *gpui.Context[gallery]) {
	app := cx.App()
	g.bold = true
	g.checks = [3]bool{true, false, true}
	g.switches = [2]bool{true, false}
	g.toolbarAction = "No command yet"
	g.accordion = [3]bool{true, false, false}
	g.collapsible = true
	g.page, g.pageLong = 3, 12
	g.toolbarSearch = gpui.NewEntity(app, w.Scope(), func(st *base.InputState, icx *gpui.Context[base.InputState]) {
		*st = base.NewInputState(w, icx).Placeholder("Search")
	})
	newSlider := func(build func(base.SliderState) base.SliderState) gpui.Entity[base.SliderState] {
		s := gpui.NewEntity(app, w.Scope(), func(st *base.SliderState, _ *gpui.Context[base.SliderState]) {
			*st = build(base.NewSliderState())
		})
		cx.Observe(s, func(_ *gallery, _ gpui.Entity[base.SliderState], cx *gpui.Context[gallery]) { cx.Notify() })
		return s
	}
	g.slider = newSlider(func(s base.SliderState) base.SliderState {
		return s.Min(0).Max(100).DefaultValue(base.SliderValueSingle(64))
	})
	g.sliderStep = newSlider(func(s base.SliderState) base.SliderState {
		return s.Min(0).Max(100).Step(10).DefaultValue(base.SliderValueSingle(30))
	})
	g.sliderRange = newSlider(func(s base.SliderState) base.SliderState {
		return s.Min(0).Max(1000).Step(10).DefaultValue(base.SliderValueRange(200, 700))
	})
	g.sliderVertical = newSlider(func(s base.SliderState) base.SliderState {
		return s.Min(0).Max(100).DefaultValue(base.SliderValueSingle(40))
	})
}

func (g *gallery) buttonPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	count := func(s *gallery) { s.clicks++ }
	return col(
		demo("Variants", row(
			btn("btn-primary", "Primary", primary, md).DebugSelector("primary-button").OnClick(g.update(cx, count)),
			btn("btn-outline", "Outline", outline, md).OnClick(g.update(cx, count)),
			btn("btn-ghost", "Ghost", ghost, md).OnClick(g.update(cx, count)),
			btn("btn-destructive", "Delete", destructive, md).OnClick(g.update(cx, count)),
			note(fmt.Sprintf("clicked %d×", g.clicks)),
		)),
		demo("Sizes", row(
			btn("btn-sm", "Small", primary, sm),
			btn("btn-md", "Medium", primary, md),
			btn("btn-lg", "Large", primary, lg),
		)),
		demo("With icon and text", row(
			btn("btn-icon-left", gpui.Div().Flex().ItemsCenter().Gap2().Child("＋").Child("New file"), outline, md),
			btn("btn-icon-right", gpui.Div().Flex().ItemsCenter().Gap2().Child("Continue").Child("→"), primary, md),
			btn("btn-icon-only", "⚙", outline, md).W8().Px0().AccessibilityLabel("Settings"),
		)),
		demo("Disabled and selected", row(
			btn("btn-disabled", "Disabled", primary, md).Disabled(true).OnClick(g.update(cx, count)),
			btn("btn-disabled-outline", "Disabled outline", outline, md).Disabled(true),
			btn("btn-selected", "Selected", outline, md).Selected(true).
				Styles(func(st base.ButtonStyles) base.ButtonStyles {
					return st.Selected(func(p *base.StateStyle) *base.StateStyle { return p.Bg(pal.selected).BorderColor(pal.fg) })
				}),
		)),
	)
}

// toggleButton styles a base.Toggle.
func toggleButton(id, label string, pressed bool, onChange func(next bool, app *gpui.App)) *base.Toggle {
	return base.NewToggle(id).
		CursorPointer().
		Pressed(pressed).
		H8().MinW(gpui.Px(32)).Px2().Flex().ItemsCenter().JustifyCenter().
		TextSm().Border1().BorderColor(pal.border).RoundedMd().
		Hover(gpui.Styles().Bg(pal.hover)).
		Styles(func(st base.ToggleStyles) base.ToggleStyles {
			return st.
				Pressed(func(p *base.StateStyle) *base.StateStyle {
					return p.Bg(pal.primary).TextColor(pal.primaryFg).BorderColor(pal.primary)
				}).
				Disabled(func(p *base.StateStyle) *base.StateStyle { return p.Opacity(0.45).CursorDefault() })
		}).
		AccessibilityLabel(label).
		OnChange(func(next bool, _ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
			if onChange != nil {
				onChange(next, app)
			}
		}).
		Child(label)
}

func (g *gallery) togglePage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	this := cx.Entity().Downgrade()
	alignments := []string{"Left", "Center", "Right"}
	single := base.NewToggleGroup("align-group").Flex().Gap1()
	for ix, label := range alignments {
		single = single.Child(toggleButton("align-"+label, label, g.align == ix, func(_ bool, app *gpui.App) {
			set(this, app, func(g *gallery) { g.align = ix })
		}))
	}
	return col(
		demo("Toggle (pressed / unpressed / disabled)", row(
			toggleButton("toggle-bold", "Bold", g.bold, func(next bool, app *gpui.App) {
				set(this, app, func(g *gallery) { g.bold = next })
			}).FontWeight(700),
			toggleButton("toggle-italic-solo", "Italic", g.italic, func(next bool, app *gpui.App) {
				set(this, app, func(g *gallery) { g.italic = next })
			}).Italic(),
			toggleButton("toggle-disabled", "Disabled", false, nil).Disabled(true),
			toggleButton("toggle-disabled-on", "Disabled on", true, nil).Disabled(true),
		)),
		demo("ToggleGroup: multiple selection", base.NewToggleGroup("format-group").Flex().Gap1().
			Child(toggleButton("group-bold", "B", g.bold, func(next bool, app *gpui.App) {
				set(this, app, func(g *gallery) { g.bold = next })
			}).FontWeight(700)).
			Child(toggleButton("group-italic", "I", g.italic, func(next bool, app *gpui.App) {
				set(this, app, func(g *gallery) { g.italic = next })
			}).Italic()).
			Child(toggleButton("group-underline", "U", g.underline, func(next bool, app *gpui.App) {
				set(this, app, func(g *gallery) { g.underline = next })
			}).Underline())),
		demo("ToggleGroup: single selection (application-owned)", single),
		demo("ToggleGroup: vertical axis", base.NewToggleGroup("vertical-group").Axis(gpui.AxisVertical).
			Flex().FlexCol().Gap1().W(gpui.Px(120)).
			Child(toggleButton("v-one", "Grid view", g.align == 0, func(_ bool, app *gpui.App) {
				set(this, app, func(g *gallery) { g.align = 0 })
			})).
			Child(toggleButton("v-two", "List view", g.align != 0, func(_ bool, app *gpui.App) {
				set(this, app, func(g *gallery) { g.align = 1 })
			}))),
	)
}

// checkbox styles a base.Checkbox with a CheckboxIndicator.
func checkbox(id, label string, state base.CheckboxState, disabled bool, onChange func(next base.CheckboxState, app *gpui.App)) *base.Checkbox {
	mark := ""
	switch state {
	case base.CheckboxStateChecked:
		mark = "✓"
	case base.CheckboxStateIndeterminate:
		mark = "–"
	}
	cb := base.NewCheckbox(id).
		CursorPointer().
		State(state).
		Disabled(disabled).
		Flex().ItemsCenter().Gap2().TextSm().
		Styles(func(st base.CheckboxStyles) base.CheckboxStyles {
			return st.Disabled(func(p *base.StateStyle) *base.StateStyle { return p.Opacity(0.45).CursorDefault() })
		}).
		Child(base.NewCheckboxIndicator().
			State(state).
			Size4().Flex().ItemsCenter().JustifyCenter().
			Border1().BorderColor(pal.fg).RoundedSm().TextXs().
			Styles(func(st base.CheckboxIndicatorStyles) base.CheckboxIndicatorStyles {
				fill := func(p *base.StateStyle) *base.StateStyle { return p.Bg(pal.primary).TextColor(pal.primaryFg) }
				return st.Checked(fill).Indeterminate(fill)
			}).
			Child(mark)).
		Child(label)
	if onChange != nil {
		cb = cb.OnChange(func(next base.CheckboxState, _ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) { onChange(next, app) })
	}
	return cb
}

func checkedState(checked bool) base.CheckboxState {
	if checked {
		return base.CheckboxStateChecked
	}
	return base.CheckboxStateUnchecked
}

func (g *gallery) checkboxPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	this := cx.Entity().Downgrade()
	labels := []string{"Email digests", "Product updates", "Security alerts"}
	all, any := true, false
	for _, c := range g.checks {
		all = all && c
		any = any || c
	}
	parent := base.CheckboxStateUnchecked
	switch {
	case all:
		parent = base.CheckboxStateChecked
	case any:
		parent = base.CheckboxStateIndeterminate
	}
	children := gpui.Div().Pl(gpui.Px(24)).Flex().FlexCol().Gap2()
	for ix, label := range labels {
		children = children.Child(checkbox(fmt.Sprintf("notify-%d", ix), label, checkedState(g.checks[ix]), false,
			func(next base.CheckboxState, app *gpui.App) {
				set(this, app, func(g *gallery) { g.checks[ix] = next == base.CheckboxStateChecked })
			}))
	}
	return col(
		demo("Checked and unchecked", col(
			checkbox("terms", "Accept terms and conditions", checkedState(g.terms), false, func(next base.CheckboxState, app *gpui.App) {
				set(this, app, func(g *gallery) { g.terms = next == base.CheckboxStateChecked })
			}),
			note(map[bool]string{true: "State: checked", false: "State: unchecked"}[g.terms]),
		)),
		demo("Indeterminate parent (select all)", col(
			checkbox("notify-all", "All notifications", parent, false, func(next base.CheckboxState, app *gpui.App) {
				set(this, app, func(g *gallery) {
					on := next == base.CheckboxStateChecked
					g.checks = [3]bool{on, on, on}
				})
			}),
			children,
		)),
		demo("Disabled", row(
			checkbox("disabled-off", "Disabled", base.CheckboxStateUnchecked, true, nil),
			checkbox("disabled-on", "Disabled checked", base.CheckboxStateChecked, true, nil),
			checkbox("disabled-mixed", "Disabled mixed", base.CheckboxStateIndeterminate, true, nil),
		)),
	)
}

// radio styles a base.Radio with a dot indicator and a description.
func radio(id, label, description string, checked, disabled bool, position, total int, onCheck func(app *gpui.App)) *base.Radio {
	dot := gpui.Div().Mt(gpui.Px(2)).Size(gpui.Px(16)).FlexNone().
		Flex().ItemsCenter().JustifyCenter().
		Border1().BorderColor(pal.fg).RoundedFull()
	if checked {
		dot = dot.Child(gpui.Div().Size(gpui.Px(8)).RoundedFull().Bg(pal.fg))
	}
	text := gpui.Div().Flex().FlexCol().Child(label)
	if description != "" {
		text = text.Child(gpui.Div().TextXs().TextColor(pal.muted).Child(description))
	}
	r := base.NewRadio(id).
		CursorPointer().
		Checked(checked).
		Disabled(disabled).
		SetPosition(position, total).
		Flex().ItemsStart().Gap2().TextSm().
		Styles(func(st base.RadioStyles) base.RadioStyles {
			return st.Disabled(func(p *base.StateStyle) *base.StateStyle { return p.Opacity(0.45).CursorDefault() })
		}).
		Child(dot).
		Child(text)
	if onCheck != nil {
		r = r.OnChange(func(next bool, _ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
			if next {
				onCheck(app)
			}
		})
	}
	return r
}

func (g *gallery) radioPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	this := cx.Entity().Downgrade()
	shipping := [][2]string{
		{"Standard", "3–5 business days"},
		{"Express", "Next business day"},
		{"Local pickup", "Currently unavailable"},
	}
	vertical := base.NewRadioGroup("shipping").Flex().FlexCol().Gap3().W(gpui.Px(260))
	for ix, option := range shipping {
		vertical = vertical.Child(radio(fmt.Sprintf("shipping-%d", ix), option[0], option[1],
			g.radio == ix, ix == 2, ix+1, len(shipping), func(app *gpui.App) {
				set(this, app, func(g *gallery) { g.radio = ix })
			}))
	}
	plans := []string{"Free", "Pro", "Team"}
	horizontal := base.NewRadioGroup("plans").Axis(gpui.AxisHorizontal).Flex().Gap5()
	for ix, plan := range plans {
		horizontal = horizontal.Child(radio(fmt.Sprintf("plan-%d", ix), plan, "", g.plan == ix, false, ix+1, len(plans),
			func(app *gpui.App) { set(this, app, func(g *gallery) { g.plan = ix }) }))
	}
	return col(
		demo("Single Radio", radio("solo-radio", "Remember this device", "", g.radio == 0, false, 1, 1, func(app *gpui.App) {
			set(this, app, func(g *gallery) { g.radio = 0 })
		})),
		demo("RadioGroup (vertical, one disabled option)", vertical),
		demo("RadioGroup (horizontal axis)", horizontal),
	)
}

func (g *gallery) switchPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	this := cx.Entity().Downgrade()
	setting := func(ix int, title, description string) any {
		return gpui.Div().W(gpui.Px(320)).Flex().ItemsCenter().JustifyBetween().
			Child(gpui.Div().Flex().FlexCol().Gap1().Child(title).Child(note(description))).
			Child(themedSwitch(fmt.Sprintf("setting-%d", ix), g.switches[ix], false, func(next bool, app *gpui.App) {
				set(this, app, func(g *gallery) { g.switches[ix] = next })
			}))
	}
	// The composed form: a SwitchTrack and a SwitchThumb inside the root,
	// as in the upstream example.
	on := g.switches[1]
	thumbOffset := gpui.Px(0)
	if on {
		thumbOffset = gpui.Px(16)
	}
	composed := base.NewSwitch("composed-switch").
		CursorPointer().
		Checked(on).
		OnChange(func(next bool, _ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
			set(this, app, func(g *gallery) { g.switches[1] = next })
		}).
		Child(base.NewSwitchTrack(gpui.NamedChildElementID(gpui.NameElementID("composed-switch"), "track")).
			Checked(on).
			W(gpui.Px(36)).H(gpui.Px(20)).P(gpui.Px(2)).RoundedFull().
			Bg(pal.border).
			Styles(func(st base.SwitchTrackStyles) base.SwitchTrackStyles {
				return st.Checked(func(p *base.StateStyle) *base.StateStyle { return p.Bg(pal.accent) })
			}).
			Child(base.NewSwitchThumb(on).Size4().RoundedFull().Bg(gpui.White()).Ml(thumbOffset)))
	return col(
		demo("Settings", col(
			setting(0, "Automatic updates", "Install stable releases automatically."),
			setting(1, "Beta channel", "Receive preview builds."),
		)),
		demo("Composed from SwitchTrack and SwitchThumb", row(composed, note(map[bool]string{true: "On", false: "Off"}[on]))),
		demo("Disabled", row(
			themedSwitch("switch-disabled-off", false, true, nil),
			themedSwitch("switch-disabled-on", true, true, nil),
		)),
	)
}

func (g *gallery) linkPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	this := cx.Entity().Downgrade()
	link := func(id, href, label string) *base.Link {
		return base.NewLink(id).
			CursorPointer().
			Href(href).
			TextColor(pal.accent).Underline().
			Hover(gpui.Styles().Opacity(0.75)).
			Styles(func(st base.LinkStyles) base.LinkStyles {
				return st.Disabled(func(p *base.StateStyle) *base.StateStyle { return p.TextColor(pal.muted).Opacity(0.6).CursorDefault() })
			}).
			OpenWith(func(href string, _ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
				set(this, app, func(g *gallery) { g.toolbarAction = "Link opened: " + href })
			}).
			Child(label)
	}
	return col(
		demo("Inline links (navigation is application-owned)", col(
			gpui.Div().Flex().FlexWrap().Gap1().
				Child("Read the").
				Child(link("docs-link", "/base/primitives/link", "Link documentation")).
				Child("or browse the").
				Child(link("repo-link", "https://github.com/hoangvu12/gpui-go", "repository")).
				Child("."),
			note(g.toolbarAction),
		)),
		demo("Button-styled link", base.NewLink("cta-link").
			CursorPointer().
			Href("/get-started").
			H8().Px3().Flex().ItemsCenter().Border1().BorderColor(pal.fg).RoundedMd().
			Hover(gpui.Styles().Bg(pal.hover)).
			OpenWith(func(href string, _ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
				set(this, app, func(g *gallery) { g.toolbarAction = "Link opened: " + href })
			}).
			Child("Get started  →")),
		demo("Disabled", link("disabled-link", "/disabled", "Disabled destination").Disabled(true)),
	)
}

func (g *gallery) tabsPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	labels := []string{"Overview", "Activity", "Settings", "Billing"}
	bodies := [][2]string{
		{"Workspace overview", "12 components · 4 contributors · updated today"},
		{"Recent activity", "Button example was updated 8 minutes ago."},
		{"Project settings", "Manage notifications and member access."},
		{"Billing", "Disabled tab."},
	}
	underline := base.NewTabs("underline-tabs").Flex().Gap1().Px2().BorderB1().BorderColor(pal.border)
	for ix, label := range labels {
		selected := g.tab == ix
		tab := base.NewTab(gpui.NamedIntegerElementID("underline-tab", uint64(ix))).
			CursorPointer().
			Selected(selected).
			Disabled(ix == 3).
			SetPosition(ix+1, len(labels)).
			Px2().H8().Flex().ItemsCenter().BorderB2().BorderColor(gpui.TransparentBlack()).TextColor(pal.muted).
			Styles(func(st base.TabStyles) base.TabStyles {
				return st.
					Selected(func(p *base.StateStyle) *base.StateStyle {
						return p.BorderColor(pal.fg).TextColor(pal.fg).FontWeight(600)
					}).
					Disabled(func(p *base.StateStyle) *base.StateStyle { return p.Opacity(0.4).CursorDefault() })
			}).
			OnClick(g.update(cx, func(g *gallery) { g.tab = ix })).
			Child(label)
		underline = underline.Child(tab)
	}
	body := bodies[g.tab]
	pills := base.NewTabs("pill-tabs").Flex().Gap1().P1().Bg(pal.elevated).RoundedLg()
	for ix, label := range []string{"Day", "Week", "Month"} {
		pills = pills.Child(base.NewTab(gpui.NamedIntegerElementID("pill-tab", uint64(ix))).
			CursorPointer().
			Selected(g.pillTab == ix).
			SetPosition(ix+1, 3).
			H7().Px3().Flex().ItemsCenter().RoundedMd().TextXs().TextColor(pal.muted).
			Styles(func(st base.TabStyles) base.TabStyles {
				return st.Selected(func(p *base.StateStyle) *base.StateStyle { return p.Bg(pal.surface).TextColor(pal.fg) })
			}).
			OnClick(g.update(cx, func(g *gallery) { g.pillTab = ix })).
			Child(label))
	}
	return col(
		demo("Underline tabs (last tab disabled)", gpui.Div().W(gpui.Px(420)).Flex().FlexCol().
			Child(underline).
			Child(gpui.Div().MinH20().P3().Flex().FlexCol().
				Child(body[0]).
				Child(gpui.Div().Mt1().TextXs().TextColor(pal.muted).Child(body[1])))),
		demo("Segmented tabs", row(pills)),
	)
}

func (g *gallery) toolbarPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	command := func(id, label string) *base.Button {
		return btn(id, label, ghost, sm).AccessibilityLabel(label).
			OnClick(g.update(cx, func(g *gallery) { g.toolbarAction = label + " command" }))
	}
	separator := func() any { return gpui.Div().W(gpui.Px(1)).H5().Bg(pal.border) }
	search := g.toolbarSearch
	return col(
		demo("Toolbar with groups (Left/Right move focus)", col(
			base.NewToolbar("editor-toolbar").
				P1().Flex().ItemsCenter().Gap1().
				Border1().BorderColor(pal.border).RoundedLg().
				Child(base.NewToolbarGroup("document-commands").Label("Document").Flex().Gap1().
					Child(command("toolbar-new", "New")).
					Child(command("toolbar-save", "Save"))).
				Child(separator()).
				Child(base.NewToolbarGroup("history-commands").Label("History").Flex().Gap1().
					Child(command("toolbar-undo", "Undo")).
					Child(command("toolbar-redo", "Redo"))).
				Child(separator()).
				Child(inputBox("toolbar-search", 160, func(b *base.InputBase) *base.InputBase { return b.H6() },
					focusInput(search), base.NewInput(search))),
			note(g.toolbarAction),
		)),
		demo("Disabled keyboard navigation", base.NewToolbar("disabled-toolbar").Disabled(true).
			P1().Flex().Gap1().Border1().BorderColor(pal.border).RoundedLg().
			Child(btn("tb-cut", "Cut", ghost, sm)).
			Child(btn("tb-copy", "Copy", ghost, sm)).
			Child(btn("tb-paste", "Paste", ghost, sm).Disabled(true))),
	)
}

func (g *gallery) accordionPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	this := cx.Entity().Downgrade()
	items := [][2]string{
		{"What is GPUI Base?", "Unstyled, accessible primitives for building native GPUI interfaces."},
		{"Can I bring my own theme?", "Yes. Every visual detail remains application-owned."},
		{"Does it support keyboard input?", "Focus, activation, and semantic state are built into the primitives."},
	}
	accordion := base.NewAccordion("faq").W(gpui.Px(380)).BorderT1().BorderColor(pal.border)
	for ix, item := range items {
		open := g.accordion[ix]
		accordion = accordion.Child(base.NewAccordionItem().
			Open(open).
			Disabled(false).
			Header(base.NewAccordionHeader(base.NewAccordionTrigger(fmt.Sprintf("faq-trigger-%d", ix)).
				CursorPointer().
				OnChange(func(next bool, _ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
					set(this, app, func(g *gallery) { g.accordion[ix] = next })
				}).
				WFull().H9().Flex().ItemsCenter().JustifyBetween().
				BorderB1().BorderColor(pal.border).
				Child(item[0]).
				Child(gpui.Div().TextColor(pal.muted).Child(map[bool]string{true: "−", false: "+"}[open])))).
			Panel(base.NewAccordionPanel().
				Py2().BorderB1().BorderColor(pal.border).TextXs().TextColor(pal.muted).
				Child(item[1])))
	}
	disabled := base.NewAccordion("faq-disabled").W(gpui.Px(380)).
		Child(base.NewAccordionItem().
			Disabled(true).
			Header(base.NewAccordionHeader(base.NewAccordionTrigger("faq-disabled-trigger").
				Disabled(true).
				WFull().H9().Flex().ItemsCenter().JustifyBetween().Opacity(0.45).
				BorderB1().BorderColor(pal.border).
				Child("Locked section").Child("+"))).
			Panel(base.NewAccordionPanel().Child("Hidden")))
	return col(
		demo("Accordion (first item open)", accordion),
		demo("Disabled item", disabled),
	)
}

func (g *gallery) collapsiblePage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	repo := func(name string) any {
		return gpui.Div().Px2().H8().Flex().ItemsCenter().Border1().BorderColor(pal.border).RoundedMd().TextXs().Child(name)
	}
	collapsible := func(id string, open bool, toggle func(g *gallery)) any {
		return base.NewCollapsible().
			Open(open).
			W(gpui.Px(320)).
			Child(gpui.Div().Flex().ItemsCenter().JustifyBetween().
				Child(gpui.Div().TextSm().Child("@gpui/base · 3 repositories")).
				Child(btn(id, map[bool]string{true: "−", false: "+"}[open], outline, sm).W6().Px0().
					AccessibilityLabel("Toggle repositories").
					OnClick(g.update(cx, toggle)))).
			Child(gpui.Div().Mt2().Flex().FlexCol().Child(repo("gpui-component"))).
			Content(gpui.Div().Mt2().Flex().FlexCol().Gap2().
				Child(repo("gpui-base")).
				Child(repo("gpui-storybook")))
	}
	return col(
		demo("Open", collapsible("collapsible-open", g.collapsible, func(g *gallery) { g.collapsible = !g.collapsible })),
		demo("Closed", collapsible("collapsible-closed", g.collapsibleClosed, func(g *gallery) { g.collapsibleClosed = !g.collapsibleClosed })),
	)
}

func (g *gallery) avatarPage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	avatar := func(initials string, size float32, bg gpui.Hsla, round bool) any {
		// The content mask is rectangular, so the fallback carries the
		// avatar's rounding itself.
		radius := gpui.Px(6)
		if round {
			radius = gpui.Px(size / 2)
		}
		return base.NewAvatar().
			Size(gpui.Px(size)).OverflowHidden().Rounded(radius).
			Fallback(base.NewAvatarFallback().
				Size(gpui.Px(size)).Flex().ItemsCenter().JustifyCenter().Rounded(radius).
				Border2().BorderColor(pal.surface).
				Bg(bg).TextColor(pal.fg).TextXs().FontWeight(600).
				Child(initials))
	}
	stack := gpui.Div().Flex()
	for ix, initials := range []string{"AM", "JL", "SK", "+3"} {
		item := gpui.Div().Child(avatar(initials, 36, pal.elevated, true))
		if ix > 0 {
			item = item.Ml(gpui.Px(-10))
		}
		stack = stack.Child(item)
	}
	return col(
		demo("Sizes (fallback initials)", row(
			avatar("XS", 24, pal.elevated, true),
			avatar("SM", 32, pal.elevated, true),
			avatar("MD", 40, pal.elevated, true),
			avatar("LG", 56, pal.elevated, true),
		)),
		demo("Shapes and colours", row(
			avatar("GO", 40, pal.series[0].Opacity(0.25), true),
			avatar("UI", 40, pal.series[1].Opacity(0.25), false),
			avatar("BA", 40, pal.series[2].Opacity(0.25), false),
		)),
		demo("Overlapping group", stack),
		note("AvatarImage takes any gpui.Img source; the gallery ships no image assets, so only fallbacks are shown."),
	)
}

// progressBar composes Progress, ProgressTrack and ProgressIndicator.
func progressBar(id string, value float32, width, height float32, color gpui.Hsla) *base.Progress {
	return base.NewProgress(id).Value(value).
		Child(base.NewProgressTrack().
			W(gpui.Px(width)).H(gpui.Px(height)).RoundedFull().OverflowHidden().Bg(pal.elevated).
			Child(base.NewProgressIndicator().
				W(gpui.Px(width * value / 100)).HFull().RoundedFull().Bg(color)))
}

func (g *gallery) progressPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	value := float32((68 + g.progressTick*12) % 112)
	if value > 100 {
		value = 100
	}
	labelled := func(label string, value float32, bar any) any {
		return gpui.Div().W(gpui.Px(300)).Flex().FlexCol().Gap1().
			Child(gpui.Div().Flex().JustifyBetween().TextXs().
				Child(label).
				Child(gpui.Div().TextColor(pal.muted).Child(fmt.Sprintf("%.0f%%", value)))).
			Child(bar)
	}
	return col(
		demo("Determinate", col(
			labelled("Uploading assets", value, progressBar("upload-progress", value, 300, 8, pal.primary)),
			labelled("Optimizing bundle", 32, progressBar("bundle-progress", 32, 300, 4, pal.muted)),
			labelled("Complete", 100, progressBar("done-progress", 100, 300, 8, pal.series[1])),
			row(btn("advance-progress", "Advance", outline, sm).OnClick(g.update(cx, func(g *gallery) { g.progressTick++ }))),
		)),
		demo("Indeterminate (no value)", base.NewProgress("busy-progress").Indeterminate(true).AccessibilityLabel("Loading").
			Child(base.NewProgressTrack().W(gpui.Px(300)).H(gpui.Px(8)).RoundedFull().Bg(pal.elevated).
				Child(base.NewProgressIndicator().W(gpui.Px(90)).HFull().RoundedFull().Bg(pal.accent)))),
	)
}

// sliderView composes Slider, SliderTrack, SliderIndicator and one or two
// SliderThumbs (a range shows a start thumb too).
func sliderView(state gpui.Entity[base.SliderState], vertical, disabled bool, app *gpui.App) any {
	type read struct {
		start, end float32
		isRange    bool
	}
	r := state.Read(app, func(s *base.SliderState, _ *gpui.App) read {
		start, end := s.Percentage()
		return read{start, end, s.Value().IsRange()}
	})
	if !r.isRange {
		r.start = 0
	}
	const thumb = 14
	thumbAt := func(p float32, start bool) any {
		t := base.NewSliderThumb(state).Start(start).Disabled(disabled).
			Absolute().Size(gpui.Px(thumb)).RoundedFull().
			Bg(pal.surface).Border2().BorderColor(pal.fg)
		if vertical {
			return t.Axis(gpui.AxisVertical).Left(gpui.Px(7)).Bottom(gpui.Fraction(p)).Mb(gpui.Px(-thumb / 2))
		}
		return t.Top(gpui.Px(7)).Left(gpui.Fraction(p)).Ml(gpui.Px(-thumb / 2))
	}
	fill := gpui.Div().Absolute().Bg(pal.fg).RoundedFull()
	rail := gpui.Div().Absolute().Bg(pal.border).RoundedFull()
	indicator := base.NewSliderIndicator(state).Absolute()
	track := base.NewSliderTrack(state).Disabled(disabled).Relative().SizeFull()
	slider := base.NewSlider(state).Disabled(disabled)
	if vertical {
		slider = slider.Vertical().W7().H(gpui.Px(160))
		track = track.Axis(gpui.AxisVertical)
		rail = rail.Left(gpui.Px(13)).Top0().HFull().W(gpui.Px(2))
		indicator = indicator.Left(gpui.Px(13)).Top0().HFull().W(gpui.Px(2))
		fill = fill.Left0().Right0().Bottom(gpui.Fraction(r.start)).Top(gpui.Fraction(1 - r.end))
	} else {
		slider = slider.W(gpui.Px(260)).H7()
		rail = rail.Top(gpui.Px(13)).Left0().WFull().H(gpui.Px(2))
		indicator = indicator.Top(gpui.Px(13)).Left0().WFull().H(gpui.Px(2))
		fill = fill.Top0().Bottom0().Left(gpui.Fraction(r.start)).Right(gpui.Fraction(1 - r.end))
	}
	track = track.Child(rail).Child(indicator.Child(fill))
	if r.isRange {
		track = track.Child(thumbAt(r.start, true))
	}
	track = track.Child(thumbAt(r.end, false))
	view := slider.Child(track)
	if disabled {
		return gpui.Div().Opacity(0.45).Child(view)
	}
	return view
}

func sliderLabel(state gpui.Entity[base.SliderState], app *gpui.App) string {
	return state.Read(app, func(s *base.SliderState, _ *gpui.App) string {
		v := s.Value()
		if v.IsRange() {
			return fmt.Sprintf("%.0f – %.0f", v.Start(), v.End())
		}
		return fmt.Sprintf("%.0f", v.End())
	})
}

func (g *gallery) sliderPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	app := cx.App()
	return col(
		demo("Continuous", row(sliderView(g.slider, false, false, app), note("Volume "+sliderLabel(g.slider, app)))),
		demo("Stepped (step 10)", row(sliderView(g.sliderStep, false, false, app), note(sliderLabel(g.sliderStep, app)))),
		demo("Range (two thumbs)", row(sliderView(g.sliderRange, false, false, app), note("Price "+sliderLabel(g.sliderRange, app)))),
		demo("Vertical and disabled", row(
			sliderView(g.sliderVertical, true, false, app),
			note(sliderLabel(g.sliderVertical, app)),
			sliderView(g.slider, false, true, app),
		)),
	)
}

// pagination renders a pagination control from its items.
func pagination(id string, current, total int, onChange func(page int, app *gpui.App)) any {
	state := base.NewPaginationState(current, total).OnChange(func(page int, _ *gpui.Window, app *gpui.App) {
		onChange(page, app)
	})
	nav := base.NewPagination(id, state).Flex().ItemsCenter().Gap1().TextXs()
	step := func(label string, target int, ok bool) any {
		b := btn(gpui.NamedChildElementID(gpui.NameElementID(id), label), label, ghost, sm).Disabled(!ok)
		if ok {
			b = b.OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) { state.RequestPage(target, w, app) })
		}
		return b
	}
	prev, hasPrev := state.PreviousPage()
	next, hasNext := state.NextPage()
	nav = nav.Child(step("‹ Prev", prev, hasPrev))
	for _, item := range state.Items() {
		if item.Ellipsis {
			nav = nav.Child(gpui.Div().W5().H7().Flex().ItemsCenter().JustifyCenter().TextColor(pal.muted).Child("…"))
			continue
		}
		pageNo := item.Page
		v := outline
		if pageNo == state.CurrentPage() {
			v = primary
		}
		nav = nav.Child(btn(gpui.NamedIntegerElementID(id+"-page", uint64(pageNo)), itoa(pageNo), v, sm).W7().Px0().
			OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) { state.RequestPage(pageNo, w, app) }))
	}
	return nav.Child(step("Next ›", next, hasNext))
}

func (g *gallery) paginationPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	this := cx.Entity().Downgrade()
	return col(
		demo("Few pages", col(
			pagination("short-pagination", g.page, 8, func(page int, app *gpui.App) {
				set(this, app, func(g *gallery) { g.page = page })
			}),
			note(fmt.Sprintf("Page %d of 8", g.page)),
		)),
		demo("Many pages (ellipsis)", col(
			pagination("long-pagination", g.pageLong, 40, func(page int, app *gpui.App) {
				set(this, app, func(g *gallery) { g.pageLong = page })
			}),
			note(fmt.Sprintf("Page %d of 40", g.pageLong)),
		)),
	)
}

func (g *gallery) tablePage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	rows := [][3]string{
		{"gpui-base", "Stable", "0.4.1"},
		{"gpui-component", "Active", "0.4.1"},
		{"story-web", "Preview", "0.2.8"},
		{"gpui-web", "Beta", "0.1.0"},
	}
	widths := []float32{160, 100, 90}
	head := base.NewTableRow("header-row", 0).Flex().Bg(pal.elevated).FontWeight(600)
	for ix, label := range []string{"Component", "Status", "Version"} {
		head = head.Child(base.NewTableHead(gpui.NamedIntegerElementID("head", uint64(ix)), ix+1).
			W(gpui.Px(widths[ix])).Px3().Py2().Child(label))
	}
	body := base.NewTableBody("body")
	for r, cells := range rows {
		tr := base.NewTableRow(gpui.NamedIntegerElementID("body-row", uint64(r)), r+1).
			Flex().BorderT1().BorderColor(pal.border).Hover(gpui.Styles().Bg(pal.hover))
		tr = tr.Child(base.NewTableCell(gpui.NamedIntegerElementID("name", uint64(r)), 1).
			W(gpui.Px(widths[0])).Px3().Py2().Child(cells[0]))
		tr = tr.Child(base.NewTableCell(gpui.NamedIntegerElementID("status", uint64(r)), 2).
			W(gpui.Px(widths[1])).Px3().Py2().
			Child(gpui.Div().Px2().TextXs().Border1().BorderColor(pal.border).RoundedFull().Child(cells[1])))
		tr = tr.Child(base.NewTableCell(gpui.NamedIntegerElementID("version", uint64(r)), 3).
			W(gpui.Px(widths[2])).Px3().Py2().TextColor(pal.muted).Child(cells[2]))
		body = body.Child(tr)
	}
	return col(
		demo("Table with header, body and caption", base.NewTable("packages").
			RowCount(len(rows)+1).ColumnCount(3).
			AccessibilityLabel("Packages").
			TextSm().Border1().BorderColor(pal.border).RoundedMd().OverflowHidden().
			Child(base.NewTableHeader("header").Child(head)).
			Child(body).
			Child(base.NewTableCaption("caption").Px3().Py2().BorderT1().BorderColor(pal.border).
				TextXs().TextColor(pal.muted).Child("4 packages · versions as of this release"))),
	)
}
