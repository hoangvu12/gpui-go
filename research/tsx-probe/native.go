package tsxprobe

// The same sidebar as view.tsx, written in plain gpui-go: the frame-cost
// baseline. Classes are the same strings (gpui's Tailwind layer); style
// objects become the fluent calls a Go author would write.

import (
	"fmt"
	"strconv"

	"github.com/hoangvu12/gpui-go/gpui"
)

type nativeAgent struct{ pane, name, where, status string }

var nativeAgents = []nativeAgent{
	{"p1", "Refactor the layout pass", "gpui-go", "working"},
	{"p2", "Port the IME tests", "zterm", "blocked"},
	{"p3", "Fix shadow blur sigma", "gpui", "error"},
	{"p4", "Write the release notes", "docs", "done"},
	{"p5", "Probe goja reload", "research", "idle"},
	{"p6", "Tidy the class mapper", "gpui", "working"},
}

var nativeMark = map[string]gpui.Hsla{
	"working": gpui.Hex("#60a5fa"),
	"blocked": gpui.Hex("#f59e0b"),
	"error":   gpui.Hex("#ef4444"),
	"done":    gpui.Hex("#22c55e"),
	"idle":    gpui.Hex("#6b7280"),
}

var (
	c111827 = gpui.Hex("#111827")
	c6b7280 = gpui.Hex("#6b7280")
	c9ca3af = gpui.Hex("#9ca3af")
	cd1d5db = gpui.Hex("#d1d5db")
	ce5e7eb = gpui.Hex("#e5e7eb")
	cf9fafb = gpui.Hex("#f9fafb")
)

// Native is the plain-Go view's state.
type Native struct {
	Current  string
	Selected int
	Clicks   int
	Notify   func()
}

// Build builds the element tree: once per frame.
func (s *Native) Build() *gpui.DivElement {
	agents := gpui.Div().Flex().FlexCol().Gap(gpui.Px(1)).Py(gpui.Px(2)).Px(gpui.Px(8))
	for _, a := range nativeAgents {
		agents = agents.Child(s.agentRow(a))
	}
	list := gpui.Div().ID("sessions").Flex().FlexCol().Flex1().MinH(gpui.Px(0)).OverflowYScroll().Py(gpui.Px(0)).Px(gpui.Px(8))
	for i := range 200 {
		list = list.Child(s.sessionRow(i))
	}
	return gpui.Div().Flex().FlexCol().W(gpui.Px(240)).HFull().Bg(c111827).
		Child(gpui.Div().Class("flex items-center gap-1.5 px-4 pt-1 pb-1.5").TextSize(gpui.Px(14)).
			Child(gpui.Div().FontWeight(600).TextColor(cd1d5db).Child("Agents")).
			Child(gpui.Div().TextColor(c6b7280).Child(strconv.Itoa(len(nativeAgents))))).
		Child(agents).
		Child(gpui.Div().ID("counter").Class("mx-2 my-2 px-3 py-1.5 rounded-md bg-gray-700").
			TextColor(cf9fafb).TextSize(gpui.Px(13)).
			OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { s.Clicks++; s.notify() }).
			Child(gpui.Div().Child(fmt.Sprintf("Clicked %d times", s.Clicks)))).
		Child(list)
}

func (s *Native) notify() {
	if s.Notify != nil {
		s.Notify()
	}
}

func (s *Native) agentRow(a nativeAgent) *gpui.DivElement {
	class := "rounded-md"
	if a.pane == s.Current {
		class = "rounded-md bg-white/10"
	}
	pane := a.pane
	return gpui.Div().ID("agent-" + a.pane).Class(class).Flex().ItemsCenter().Gap(gpui.Px(8)).Py(gpui.Px(6)).Px(gpui.Px(8)).
		OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { s.Current = pane; s.notify() }).
		Child(gpui.Div().W(gpui.Px(8)).H(gpui.Px(8)).Rounded(gpui.Px(4)).Bg(nativeMark[a.status])).
		Child(gpui.Div().Flex().FlexCol().Flex1().
			Child(gpui.Div().TextColor(ce5e7eb).TextSize(gpui.Px(13)).Child(a.name)).
			Child(gpui.Div().TextColor(c9ca3af).TextSize(gpui.Px(11)).Child(a.status + " · " + a.where)))
}

func (s *Native) sessionRow(i int) *gpui.DivElement {
	class := "px-2 py-1 rounded"
	if i == s.Selected {
		class = "px-2 py-1 rounded bg-blue-500/30"
	}
	return gpui.Div().ID("row-" + strconv.Itoa(i)).Class(class).Flex().JustifyBetween().
		TextColor(cd1d5db).TextSize(gpui.Px(12)).
		OnClick(func(*gpui.ClickEvent, *gpui.Window, *gpui.App) { s.Selected = i; s.notify() }).
		Child(gpui.Div().Child("session " + strconv.Itoa(i))).
		Child(gpui.Div().TextColor(c6b7280).Child(strconv.Itoa((i*7)%60) + "m ago"))
}
