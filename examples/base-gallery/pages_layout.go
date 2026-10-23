package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

var layoutPages = []page{
	{"tree", "Tree", "tree.rs", (*gallery).treePage},
	{"nav-stack", "NavStack", "nav_stack.rs", (*gallery).navStackPage},
	{"resizable", "Resizable", "resizable/panel.rs", (*gallery).resizablePage},
	{"questionnaire", "Questionnaire", "questionnaire/state.rs", (*gallery).questionnairePage},
	{"dock", "Dock", "dock/mod.rs", (*gallery).dockPage},
}

type layoutState struct {
	fileTree      gpui.Entity[base.TreeState]
	stack         gpui.Entity[base.NavStackState]
	questionnaire gpui.Entity[base.QuestionnaireState]
	qNotes        gpui.Entity[base.InputState]
	qResult       string
	qError        string
	dock          gpui.Entity[base.DockArea]
}

func (g *gallery) initLayout(w *gpui.Window, cx *gpui.Context[gallery]) {
	app := cx.App()
	g.fileTree = gpui.NewEntity(app, w.Scope(), func(st *base.TreeState, _ *gpui.Context[base.TreeState]) {
		*st = base.NewTreeState(app).Items([]base.TreeItem{
			base.NewTreeItem("base", "base").Expanded(true).WithChildren(
				base.NewTreeItem("components", "components").Expanded(true).WithChildren(
					base.NewTreeItem("button", "button.go"),
					base.NewTreeItem("tree-file", "tree.go"),
					base.NewTreeItem("legacy", "legacy.go (disabled)").Disabled(true),
				),
				base.NewTreeItem("lib", "lib.go"),
				base.NewTreeItem("theme", "theme.go"),
			),
			base.NewTreeItem("examples", "examples").WithChildren(
				base.NewTreeItem("gallery", "base-gallery"),
				base.NewTreeItem("showcase", "base-showcase"),
			),
			base.NewTreeItem("gomod", "go.mod"),
		})
	})
	cx.Observe(g.fileTree, func(_ *gallery, _ gpui.Entity[base.TreeState], cx *gpui.Context[gallery]) { cx.Notify() })

	g.stack = gpui.NewEntity(app, w.Scope(), func(st *base.NavStackState, _ *gpui.Context[base.NavStackState]) {
		*st = base.NewNavStackState()
	})
	stack := g.stack
	stack.Update(app, func(st *base.NavStackState, scx *gpui.Context[base.NavStackState]) {
		root := gpui.NewEntity(app, w.Scope(), func(p *stackPage, _ *gpui.Context[stackPage]) {
			*p = stackPage{depth: 1, stack: stack.Downgrade()}
		})
		st.Push(gpui.ViewOf(root), base.NavMotionImmediate, scx)
	})
	cx.Observe(g.stack, func(_ *gallery, _ gpui.Entity[base.NavStackState], cx *gpui.Context[gallery]) { cx.Notify() })

	g.qNotes = gpui.NewEntity(app, w.Scope(), func(st *base.InputState, icx *gpui.Context[base.InputState]) {
		*st = base.NewInputState(w, icx).Placeholder("Anything else? (optional)")
	})
	g.buildQuestionnaire(w, cx)

	g.dock = buildDock(w, app)
}

func (g *gallery) buildQuestionnaire(w *gpui.Window, cx *gpui.Context[gallery]) {
	app := cx.App()
	items := []base.QuestionnaireItemDefinition{
		base.NewQuestionnaireItemDefinition("framework", "Which UI framework do you use most?").
			WithDescription("Pick one. This question is required.").
			WithRequired(true).
			WithChoices(
				base.NewQuestionnaireChoiceDefinition("gpui", "GPUI").WithDescription("Rust or Go, GPU accelerated"),
				base.NewQuestionnaireChoiceDefinition("react", "React"),
				base.NewQuestionnaireChoiceDefinition("swiftui", "SwiftUI"),
				base.NewQuestionnaireChoiceDefinition("qt", "Qt").WithDisabled(true).WithDescription("Not offered in this survey"),
			),
		base.NewQuestionnaireItemDefinition("features", "Which components matter to you?").
			WithDescription("Choose any number, or skip.").
			WithMultiple(true).
			WithChoices(
				base.NewQuestionnaireChoiceDefinition("inputs", "Inputs and editors").WithDefaultSelected(true),
				base.NewQuestionnaireChoiceDefinition("overlays", "Overlays"),
				base.NewQuestionnaireChoiceDefinition("charts", "Charts"),
				base.NewQuestionnaireChoiceDefinition("dock", "Docking layouts"),
			),
		base.NewQuestionnaireItemDefinition("notes", "Anything else?").
			WithDescription("Free-form answer; optional.").
			WithInput(base.NewQuestionnaireInputDefinition(g.qNotes, "Notes")),
	}
	var schemaErr error
	g.questionnaire = gpui.NewEntity(app, w.Scope(), func(st *base.QuestionnaireState, qcx *gpui.Context[base.QuestionnaireState]) {
		s, err := base.NewQuestionnaireState(items, qcx)
		if err != nil {
			schemaErr = err
			return
		}
		*st = s.WithShortcuts(base.QuestionnaireShortcutNumbers)
	})
	if schemaErr != nil {
		g.qError = schemaErr.Error()
	}
	g.qResult = "Not submitted"
	cx.Observe(g.questionnaire, func(_ *gallery, _ gpui.Entity[base.QuestionnaireState], cx *gpui.Context[gallery]) { cx.Notify() })
	base.QuestionnaireEvents.Subscribe(cx, g.questionnaire, func(g *gallery, _ gpui.Entity[base.QuestionnaireState], ev *base.QuestionnaireEvent, cx *gpui.Context[gallery]) {
		if ev.Kind != base.QuestionnaireSubmit {
			return
		}
		var parts []string
		for _, item := range ev.Submission.Items() {
			answer := item.Answer()
			value := strings.Join(answer.Choices(), ", ")
			if text, ok := answer.Freeform(); ok {
				value = strings.TrimSpace(value + " " + fmt.Sprintf("%q", text))
			}
			if value == "" {
				value = item.Status().String()
			}
			parts = append(parts, item.Name()+": "+value)
		}
		g.qResult = "Submitted — " + strings.Join(parts, " · ")
		cx.Notify()
	})
}

func (g *gallery) treePage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	app := cx.App()
	selected := g.fileTree.Read(app, func(st *base.TreeState, _ *gpui.App) string {
		if item := st.SelectedItem(); item != nil {
			return item.Label
		}
		return "nothing"
	})
	tree := base.NewTree(g.fileTree).
		W(gpui.Px(300)).H(gpui.Px(260)).Relative().TextSm().Py1().
		Border1().BorderColor(pal.border).RoundedLg().Bg(pal.surface).
		ListStyle(gpui.Styles().FlexGrow1().SizeFull().Refinement()).
		Item(func(_ int, entry *base.TreeEntry, state base.TreeEntryState, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
			var icon any = gpui.Div().Size3()
			if entry.IsFolder() {
				icon = folderChevron(entry.IsExpanded())
			}
			row := gpui.Div().H7().Mx1().Px2().Flex().ItemsCenter().Gap1().RoundedMd()
			if !entry.IsDisabled() {
				row = row.CursorPointer()
			}
			if state.IsSelected() {
				row = row.Bg(pal.selected)
			} else if !entry.IsDisabled() {
				row = row.Hover(gpui.Styles().Bg(pal.hover))
			}
			if entry.IsDisabled() {
				row = row.Opacity(0.45)
			}
			if depth := entry.Depth(); depth > 0 {
				row = row.Child(gpui.Div().FlexNone().W(gpui.Px(float32(depth) * 14)))
			}
			return row.
				Child(icon).
				Child(entry.Item().Label).
				IntoElement()
		})
	return col(
		demo("Virtualized tree (click or use arrow keys; Left/Right fold)", col(tree, note("Selected: "+selected))),
		note("This gallery's sidebar is also a base.Tree."),
	)
}

// stackPage is one page of the NavStack demo. It knows its depth and the
// stack it lives in, so its buttons can push, replace or pop.
type stackPage struct {
	depth int
	stack gpui.WeakEntity[base.NavStackState]
}

func (p *stackPage) navigate(depth int, apply func(st *base.NavStackState, view gpui.View, cx *gpui.Context[base.NavStackState])) func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {
	stack := p.stack
	return func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
		_ = stack.TryUpdate(app, func(st *base.NavStackState, scx *gpui.Context[base.NavStackState]) {
			next := gpui.NewEntity(app, w.Scope(), func(np *stackPage, _ *gpui.Context[stackPage]) {
				*np = stackPage{depth: depth, stack: stack}
			})
			apply(st, gpui.ViewOf(next), scx)
		})
	}
}

func (p *stackPage) Render(_ *gpui.Window, cx *gpui.Context[stackPage]) gpui.AnyElement {
	type read struct{ behind, ahead int }
	r := read{p.depth, 0}
	if stack, err := p.stack.TryRead(cx.App(), func(st *base.NavStackState, _ *gpui.App) read {
		return read{st.Depth(), len(st.ForwardViews())}
	}); err == nil {
		r = stack
	}
	bg := pal.surface
	if p.depth%2 == 0 {
		bg = pal.elevated
	}
	trail := gpui.Div().Flex().Gap1().TextXs()
	for n := 1; n <= r.behind+r.ahead; n++ {
		crumb := gpui.Div().Px1().TextColor(pal.muted).Child(itoa(n))
		if n == p.depth {
			crumb = crumb.TextColor(pal.fg).FontWeight(600)
		} else if n > r.behind {
			crumb = crumb.Opacity(0.4)
		}
		trail = trail.Child(crumb)
	}
	buttons := gpui.Div().Flex().Gap2().
		Child(btn("push", "Push", primary, sm).OnClick(p.navigate(p.depth+1, func(st *base.NavStackState, v gpui.View, cx *gpui.Context[base.NavStackState]) {
			st.Push(v, base.NavMotionAnimated, cx)
		}))).
		Child(btn("replace", "Replace", outline, sm).OnClick(p.navigate(p.depth, func(st *base.NavStackState, v gpui.View, cx *gpui.Context[base.NavStackState]) {
			st.Replace(v, base.NavMotionAnimated, cx)
		})))
	stack := p.stack
	if p.depth > 1 {
		buttons = buttons.Child(btn("pop", "Pop", outline, sm).OnClick(func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
			_ = stack.TryUpdate(app, func(st *base.NavStackState, scx *gpui.Context[base.NavStackState]) {
				st.Pop(base.NavMotionAnimated, scx)
			})
		}))
	}
	if r.ahead > 0 {
		buttons = buttons.Child(btn("forward", "Forward", outline, sm).OnClick(func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
			_ = stack.TryUpdate(app, func(st *base.NavStackState, scx *gpui.Context[base.NavStackState]) {
				st.Forward(base.NavMotionAnimated, scx)
			})
		}))
	}
	return gpui.Div().SizeFull().Flex().FlexCol().Gap3().P4().TextSm().Bg(bg).TextColor(pal.fg).
		Child(gpui.Div().TextBase().FontWeight(600).Child(fmt.Sprintf("Page %d", p.depth))).
		Child(trail).
		Child(buttons).
		IntoElement()
}

// slide moves a page by its transition: a pushed page slides in from the
// right and back out when popped; the page underneath drifts a little.
func slide(page *base.NavPage, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
	var offset float32
	op, ok := page.Operation()
	switch {
	case !ok:
	case page.Phase() == base.PresencePhaseEntering && (op == base.NavOperationPush || op == base.NavOperationReplace):
		offset = 1 - page.Progress()
	case page.Phase() == base.PresencePhaseExiting && op == base.NavOperationPop:
		offset = page.Progress()
	case page.Phase() == base.PresencePhaseExiting && op == base.NavOperationPush:
		offset = -0.3 * page.Progress()
	case page.Phase() == base.PresencePhaseEntering && op == base.NavOperationPop:
		offset = -0.3 * (1 - page.Progress())
	}
	return gpui.OwnRecipe(page.Left(gpui.Fraction(offset)))
}

func (g *gallery) navStackPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	depth := g.stack.Read(cx.App(), func(st *base.NavStackState, _ *gpui.App) int { return st.Depth() })
	return col(
		demo("Push, replace, pop and forward with a slide transition", col(
			base.NewNavStack(g.stack).
				W(gpui.Px(420)).H(gpui.Px(200)).OverflowHidden().
				Border1().BorderColor(pal.border).RoundedLg().
				Transition(base.NewTransition(220*time.Millisecond)).
				Item(slide),
			note(fmt.Sprintf("Stack depth %d", depth)),
		)),
	)
}

func (g *gallery) resizablePage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	pane := func(title, body string, bg gpui.Hsla) any {
		return gpui.Div().SizeFull().P3().Flex().FlexCol().Gap1().Bg(bg).TextXs().
			Child(gpui.Div().FontWeight(600).Child(title)).
			Child(gpui.Div().TextColor(pal.muted).Child(body))
	}
	handle := func(ctx *base.ResizeHandleContext, _ *gpui.Window, _ *gpui.App) (gpui.AnyElement, bool) {
		color := pal.border
		if ctx.IsActive() {
			color = pal.accent
		}
		line := gpui.Div().Bg(color)
		if ctx.Axis() == gpui.AxisHorizontal {
			line = line.W(gpui.Px(2)).HFull()
		} else {
			line = line.H(gpui.Px(2)).WFull()
		}
		return gpui.Div().SizeFull().Flex().ItemsCenter().JustifyCenter().Child(line).IntoElement(), true
	}
	frame := func(w, h float32, child any) any {
		return gpui.Div().W(gpui.Px(w)).H(gpui.Px(h)).Border1().BorderColor(pal.border).RoundedLg().OverflowHidden().Child(child)
	}
	return col(
		demo("Horizontal (navigation 116–260 px)", frame(520, 180,
			base.HResizable("resizable-h").
				WithHandleAppearance(handle).
				Child(base.NewResizablePanel().Size(150).SizeRange(116, 260).
					Child(gpui.Div().SizeFull().DebugSelector("resizable-nav").
						Child(pane("Navigation", "Drag the divider to resize.", pal.elevated)))).
				Child(base.NewResizablePanel().
					Child(pane("Workspace", "This panel takes the remaining space.", pal.surface))))),
		demo("Vertical", frame(520, 240,
			base.VResizable("resizable-v").
				WithHandleAppearance(handle).
				Child(base.NewResizablePanel().Size(140).SizeRange(60, 200).
					Child(pane("Editor", "Top panel.", pal.surface))).
				Child(base.NewResizablePanel().
					Child(pane("Terminal", "Bottom panel.", pal.elevated))))),
		demo("Nested groups (default handle appearance)", frame(520, 220,
			base.HResizable("resizable-outer").
				Child(base.NewResizablePanel().Size(140).SizeRange(100, 240).
					Child(pane("Files", "Left.", pal.elevated))).
				Child(base.NewResizablePanel().
					Child(base.VResizable("resizable-inner").
						Child(base.NewResizablePanel().Child(pane("Code", "Right top.", pal.surface))).
						Child(base.NewResizablePanel().Size(80).SizeRange(48, 160).
							Child(pane("Output", "Right bottom.", pal.elevated))))))),
	)
}

func (g *gallery) questionnairePage(w *gpui.Window, cx *gpui.Context[gallery]) any {
	if g.qError != "" {
		return missing("the questionnaire schema was rejected: " + g.qError)
	}
	app := cx.App()
	q := g.questionnaire
	type read struct {
		item       base.QuestionnaireItemDefinition
		hasItem    bool
		progress   base.QuestionnaireProgressState
		nav        base.QuestionnaireNavigationState
		errText    string
		complete   bool
		focus      gpui.FocusHandle
		shortcuts  map[string]string
		itemStatus string
	}
	r := q.Read(app, func(s *base.QuestionnaireState, _ *gpui.App) read {
		out := read{progress: s.Progress(), nav: s.NavigationState(), complete: s.IsComplete(), focus: s.FocusHandle(), shortcuts: map[string]string{}}
		name, ok := s.CurrentItem()
		if !ok {
			return out
		}
		out.item, out.hasItem = s.ItemDefinition(name)
		if st, ok := s.ItemState(name); ok {
			out.itemStatus = st.Status().String()
		}
		if e, ok := s.Error(name); ok {
			switch e.Kind {
			case base.QuestionnaireErrorRequired:
				out.errText = "This question is required."
			case base.QuestionnaireErrorUnanswered:
				out.errText = "Answer or skip this question."
			default:
				out.errText, _ = e.Message()
			}
		}
		for _, choice := range out.item.Choices() {
			if key, ok := s.ShortcutForChoice(name, choice.Value()); ok {
				out.shortcuts[choice.Value()] = key
			}
		}
		return out
	})
	if !r.hasItem {
		return missing("the questionnaire has no enabled item")
	}
	name := r.item.Name()
	choices := gpui.Div().Flex().FlexCol().Gap2()
	for _, choice := range r.item.Choices() {
		control, ok := base.NewQuestionnaireChoiceControl(q, name, choice.Value(),
			gpui.NamedChildElementID(gpui.NameElementID("q-"+name), choice.Value()), app)
		if !ok {
			continue
		}
		label := gpui.Div().Flex().FlexCol().Child(choice.AccessibilityLabel())
		if desc, ok := choice.Description(); ok {
			label = label.Child(note(desc))
		}
		key := gpui.Div().W5().FlexNone().TextXs().TextColor(pal.muted).Child(r.shortcuts[choice.Value()])
		selected := q.Read(app, func(s *base.QuestionnaireState, _ *gpui.App) bool {
			c, ok := s.ChoiceState(name, choice.Value())
			return ok && c.IsSelected()
		})
		var item any
		if control.Checkbox != nil {
			item = control.Checkbox.CursorPointer().Flex().ItemsStart().Gap2().P2().RoundedMd().Hover(gpui.Styles().Bg(pal.hover)).
				Styles(func(st base.CheckboxStyles) base.CheckboxStyles {
					return st.Disabled(func(p *base.StateStyle) *base.StateStyle { return p.Opacity(0.45).CursorDefault() })
				}).
				Child(key).
				Child(gpui.Div().Mt(gpui.Px(2)).Size4().FlexNone().Flex().ItemsCenter().JustifyCenter().TextXs().
					Border1().BorderColor(pal.fg).RoundedSm().
					When(selected, func(d *gpui.DivElement) *gpui.DivElement {
						return d.Bg(pal.primary).TextColor(pal.primaryFg).Child("✓")
					})).
				Child(label)
		} else {
			dot := gpui.Div().Mt(gpui.Px(2)).Size4().FlexNone().Flex().ItemsCenter().JustifyCenter().
				Border1().BorderColor(pal.fg).RoundedFull()
			if selected {
				dot = dot.Child(gpui.Div().Size2().RoundedFull().Bg(pal.fg))
			}
			item = control.Radio.CursorPointer().Flex().ItemsStart().Gap2().P2().RoundedMd().Hover(gpui.Styles().Bg(pal.hover)).
				Styles(func(st base.RadioStyles) base.RadioStyles {
					return st.Disabled(func(p *base.StateStyle) *base.StateStyle { return p.Opacity(0.45).CursorDefault() })
				}).
				Child(key).Child(dot).Child(label)
		}
		choices = choices.Child(item)
	}
	if _, ok := r.item.Input(); ok {
		choices = choices.Child(inputBox("q-notes", 360, inputHeight(32), focusInput(g.qNotes), base.NewInput(g.qNotes)))
	}

	act := func(f func(s *base.QuestionnaireState, w *gpui.Window, cx *gpui.Context[base.QuestionnaireState]) bool) func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {
		return func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			q.Update(app, func(s *base.QuestionnaireState, qcx *gpui.Context[base.QuestionnaireState]) { f(s, w, qcx) })
		}
	}
	nav := gpui.Div().Flex().Gap2()
	if r.nav.IsPreviousVisible() {
		nav = nav.Child(btn("q-prev", "Previous", outline, sm).OnClick(act((*base.QuestionnaireState).GoPrevious)))
	}
	if r.nav.IsSkipVisible() {
		nav = nav.Child(btn("q-skip", "Skip", ghost, sm).OnClick(act((*base.QuestionnaireState).SkipCurrent)))
	}
	if r.nav.IsNextVisible() {
		nav = nav.Child(btn("q-next", "Next", primary, sm).Disabled(!r.nav.IsConfirmable()).OnClick(act((*base.QuestionnaireState).GoNext)))
	}
	if r.nav.IsSubmitVisible() {
		nav = nav.Child(btn("q-submit", "Submit", primary, sm).OnClick(act((*base.QuestionnaireState).Submit)))
	}
	nav = nav.Child(btn("q-reset", "Reset", ghost, sm).OnClick(func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
		q.Update(app, func(s *base.QuestionnaireState, qcx *gpui.Context[base.QuestionnaireState]) { s.Reset(w, qcx) })
	}))

	header := gpui.Div().Flex().FlexCol().Gap1().
		Child(gpui.Div().TextXs().TextColor(pal.muted).
			Child(fmt.Sprintf("Question %d of %d · %s", r.progress.Current(), r.progress.Total(), r.itemStatus))).
		Child(gpui.Div().TextBase().FontWeight(600).Child(r.item.AccessibilityLabel()))
	if desc, ok := r.item.Description(); ok {
		header = header.Child(note(desc))
	}
	card := gpui.Div().ID("questionnaire").
		TrackFocus(r.focus).
		OnKeyDown(func(event *gpui.KeyDownEvent, w *gpui.Window, app *gpui.App) {
			base.QuestionnaireHandleKeyDown(q, event, w, app)
		}).
		W(gpui.Px(440)).Flex().FlexCol().Gap3().
		Child(header).
		Child(choices)
	if r.errText != "" {
		card = card.Child(gpui.Div().TextXs().TextColor(pal.danger).Child(r.errText))
	}
	card = card.Child(nav)
	status := g.qResult
	if r.complete && status == "Not submitted" {
		status = "Complete"
	}
	return col(
		demo("Three questions: single choice (required), multiple choice, free text", card),
		note(status),
		note("Number keys pick a choice; Enter confirms a selected answer."),
	)
}

func (g *gallery) dockPage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	return col(
		demo("DockArea: split center and a bottom dock (drag tabs between groups)",
			gpui.Div().W(gpui.Px(680)).H(gpui.Px(420)).OverflowHidden().Child(gpui.ViewOf(g.dock))),
	)
}
