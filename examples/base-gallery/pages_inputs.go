package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

var inputPages = []page{
	{"input", "Input", "input/input/mod.rs", (*gallery).inputPage},
	{"textarea", "Textarea", "input/textarea/mod.rs", (*gallery).textareaPage},
	{"editor", "Editor", "input/editor/mod.rs", (*gallery).editorPage},
	{"number-input", "NumberInput", "number_input.rs", (*gallery).numberInputPage},
	{"otp-input", "OtpInput", "otp_input.rs", (*gallery).otpPage},
	{"time-field", "TimeField", "time_field.rs", (*gallery).timeFieldPage},
	{"select", "Select", "select.rs", (*gallery).selectPage},
	{"combobox", "Combobox", "combobox.rs", (*gallery).comboboxPage},
	{"calendar", "Calendar", "calendar.rs", (*gallery).calendarPage},
	{"date-picker", "DatePicker", "date_picker.rs", (*gallery).datePickerPage},
	{"color-picker", "ColorPicker", "color_picker.rs", (*gallery).colorPickerPage},
}

var frameworks = []string{"GPUI", "React", "SwiftUI", "Vue", "Svelte", "Flutter"}

type inputState struct {
	input         gpui.Entity[base.InputState]
	inputEmpty    gpui.Entity[base.InputState]
	inputDisabled gpui.Entity[base.InputState]
	password      gpui.Entity[base.InputState]
	search        gpui.Entity[base.InputState]
	textarea      gpui.Entity[base.TextareaState]
	textareaGrow  gpui.Entity[base.TextareaState]
	editor        gpui.Entity[base.EditorState]
	editorPlain   gpui.Entity[base.EditorState]
	quantity      gpui.Entity[base.InputState]
	price         gpui.Entity[base.InputState]
	otp           gpui.Entity[base.OtpState]
	otpMasked     gpui.Entity[base.OtpState]
	timeField     gpui.Entity[base.TimeFieldState]
	timeField12   gpui.Entity[base.TimeFieldState]
	selectOpen    bool
	selectIndex   int
	comboOpen     bool
	comboQuery    gpui.Entity[base.InputState]
	comboValue    string
	calendar      gpui.Entity[base.CalendarState]
	rangeCalendar gpui.Entity[base.CalendarState]
	dateCalendar  gpui.Entity[base.CalendarState]
	dateFocus     gpui.FocusHandle
	dateOpen      bool
	colorPicker   gpui.Entity[base.ColorPickerState]
}

// editorExample indents with spaces: a hard tab draws as a missing
// glyph in the editor (reported upstream of this example).
const editorExample = `package main

import "fmt"

// Workspace counts lines per file.
type Workspace struct {
    Name  string
    Files map[string]int
}

func (w *Workspace) Index(path string, lines int) {
    w.Files[path] = lines
}

func main() {
    ws := &Workspace{Name: "gpui-go", Files: map[string]int{}}
    ws.Index("base/button.go", 128)
    for path, lines := range ws.Files {
        fmt.Println(path, lines)
    }
}
`

func (g *gallery) initInputs(w *gpui.Window, cx *gpui.Context[gallery]) {
	app := cx.App()
	newInput := func(build func(base.InputState) base.InputState) gpui.Entity[base.InputState] {
		e := gpui.NewEntity(app, w.Scope(), func(st *base.InputState, icx *gpui.Context[base.InputState]) {
			*st = build(base.NewInputState(w, icx))
		})
		// Redraw on text changes only: observing the state would also
		// follow the notifications its own rendering raises.
		base.InputStateEvents.Subscribe(cx, e, func(_ *gallery, _ gpui.Entity[base.InputState], ev *base.InputEvent, cx *gpui.Context[gallery]) {
			if ev.Kind == base.InputEventChange {
				cx.Notify()
			}
		})
		return e
	}
	g.input = newInput(func(s base.InputState) base.InputState {
		return s.Placeholder("Project name").DefaultValue("Hello GPUI")
	})
	g.inputEmpty = newInput(func(s base.InputState) base.InputState { return s.Placeholder("you@example.com") })
	g.inputDisabled = newInput(func(s base.InputState) base.InputState {
		return s.DefaultValue("Read only for now").Disabled(true)
	})
	g.password = newInput(func(s base.InputState) base.InputState {
		return s.Placeholder("Password").DefaultValue("hunter2").Masked(true)
	})
	g.search = newInput(func(s base.InputState) base.InputState { return s.Placeholder("Search components…") })
	g.quantity = newInput(func(s base.InputState) base.InputState {
		return s.DefaultValue("12").Step(base.NumberStepFixed(1)).Min(0).Max(99)
	})
	g.price = newInput(func(s base.InputState) base.InputState {
		return s.DefaultValue("9.50").Step(base.NumberStepFixed(0.25)).Min(0)
	})
	g.comboQuery = newInput(func(s base.InputState) base.InputState { return s.Placeholder("Search frameworks…") })
	g.comboValue = "Select framework"

	g.textarea = gpui.NewEntity(app, w.Scope(), func(st *base.TextareaState, tcx *gpui.Context[base.TextareaState]) {
		*st = base.NewTextareaState(w, tcx).Rows(3).Placeholder("Write a longer note…").
			DefaultValue("Build focused interfaces.\nKeep behavior composable.\nLet the application own every visual.")
	})
	g.textareaGrow = gpui.NewEntity(app, w.Scope(), func(st *base.TextareaState, tcx *gpui.Context[base.TextareaState]) {
		*st = base.NewTextareaState(w, tcx).AutoGrow(2, 6).Placeholder("Grows from 2 to 6 rows as you type…")
	})
	g.editor = gpui.NewEntity(app, w.Scope(), func(st *base.EditorState, ecx *gpui.Context[base.EditorState]) {
		*st = base.NewEditorState(w, ecx).Language("go").LineNumber(true).Folding(true).IndentGuides(true).
			DefaultValue(editorExample)
	})
	g.editorPlain = gpui.NewEntity(app, w.Scope(), func(st *base.EditorState, ecx *gpui.Context[base.EditorState]) {
		*st = base.NewEditorState(w, ecx).Language("json").LineNumber(false).SoftWrap(true).
			DefaultValue("{\n  \"name\": \"gpui-go\",\n  \"components\": 43,\n  \"unstyled\": true\n}\n")
	})

	newOtp := func(masked bool) gpui.Entity[base.OtpState] {
		e := gpui.NewEntity(app, w.Scope(), func(st *base.OtpState, ocx *gpui.Context[base.OtpState]) {
			*st = base.NewOtpState(6, w, ocx).DefaultValue("12").Masked(masked)
		})
		cx.Observe(e, func(_ *gallery, _ gpui.Entity[base.OtpState], cx *gpui.Context[gallery]) { cx.Notify() })
		return e
	}
	g.otp, g.otpMasked = newOtp(false), newOtp(true)

	newTime := func(precision base.TimePrecision, cycle base.HourCycle, h, m, s uint32) gpui.Entity[base.TimeFieldState] {
		e := gpui.NewEntity(app, w.Scope(), func(st *base.TimeFieldState, tcx *gpui.Context[base.TimeFieldState]) {
			*st = base.NewTimeFieldState(w, tcx).Precision(precision).HourCycle(cycle)
			if t, ok := base.NaiveTimeFromHMS(h, m, s); ok {
				st.SetTime(t, w, tcx)
			}
		})
		cx.Observe(e, func(_ *gallery, _ gpui.Entity[base.TimeFieldState], cx *gpui.Context[gallery]) { cx.Notify() })
		return e
	}
	g.timeField = newTime(base.TimePrecisionSecond, base.HourCycleH23, 9, 30, 0)
	g.timeField12 = newTime(base.TimePrecisionMinute, base.HourCycleH12, 18, 45, 0)

	newCalendar := func(setup func(st *base.CalendarState, w *gpui.Window, cx *gpui.Context[base.CalendarState])) gpui.Entity[base.CalendarState] {
		e := gpui.NewEntity(app, w.Scope(), func(st *base.CalendarState, ccx *gpui.Context[base.CalendarState]) {
			*st = base.NewCalendarState(w, ccx)
			if setup != nil {
				setup(st, w, ccx)
			}
		})
		// Redraw when a selection completes (the calendar redraws itself
		// for navigation).
		base.CalendarEvents.Subscribe(cx, e, func(_ *gallery, _ gpui.Entity[base.CalendarState], _ *base.CalendarEvent, cx *gpui.Context[gallery]) {
			cx.Notify()
		})
		return e
	}
	g.calendar = newCalendar(func(st *base.CalendarState, w *gpui.Window, cx *gpui.Context[base.CalendarState]) {
		st.SetDate(base.DateOf(st.Today()), w, cx)
	})
	g.rangeCalendar = newCalendar(func(st *base.CalendarState, w *gpui.Window, cx *gpui.Context[base.CalendarState]) {
		start := st.Today()
		st.SetDate(base.DateOfRange(start, start.AddDays(4)), w, cx)
		st.SetDisabledMatcher(base.MatcherDayOfWeek(0, 6), w, cx)
	})
	g.dateCalendar = newCalendar(func(st *base.CalendarState, w *gpui.Window, cx *gpui.Context[base.CalendarState]) {
		if d, ok := base.NaiveDateFromYMD(2026, 8, 12); ok {
			st.SetDate(base.DateOf(d), w, cx)
		}
	})
	// Picking a day closes the date picker.
	base.CalendarEvents.Subscribe(cx, g.dateCalendar, func(g *gallery, _ gpui.Entity[base.CalendarState], _ *base.CalendarEvent, cx *gpui.Context[gallery]) {
		g.dateOpen = false
		cx.Notify()
	})
	g.dateFocus = app.FocusHandle()

	g.colorPicker = gpui.NewEntity(app, w.Scope(), func(st *base.ColorPickerState, pcx *gpui.Context[base.ColorPickerState]) {
		*st = base.NewColorPickerState(w, pcx).DefaultValue(gpui.Rgb(0x2563eb))
	})
	cx.Observe(g.colorPicker, func(_ *gallery, _ gpui.Entity[base.ColorPickerState], cx *gpui.Context[gallery]) { cx.Notify() })
}

func inputHeight(h float32) func(*base.InputBase) *base.InputBase {
	return func(b *base.InputBase) *base.InputBase { return b.H(gpui.Px(h)) }
}

func (g *gallery) inputPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	app := cx.App()
	value := g.input.Read(app, func(s *base.InputState, _ *gpui.App) string { return s.Value() })
	return col(
		demo("Default and empty (placeholder)", col(
			labelled("Project name", inputBox("input-default", 280, inputHeight(32), focusInput(g.input), base.NewInput(g.input))),
			note("Value: "+value),
			labelled("Email", inputBox("input-empty", 280, inputHeight(32), focusInput(g.inputEmpty), base.NewInput(g.inputEmpty))),
		)),
		demo("Masked (password)", inputBox("input-password", 280, inputHeight(32), focusInput(g.password), base.NewInput(g.password))),
		demo("With a leading icon", inputBox("input-search", 280, inputHeight(32), focusInput(g.search),
			gpui.Div().SizeFull().Flex().ItemsCenter().Gap2().
				Child(searchIcon(pal.muted)).
				Child(gpui.Div().Flex1().MinW(gpui.Px(0)).Child(base.NewInput(g.search))))),
		demo("Disabled", inputBox("input-disabled", 280, func(b *base.InputBase) *base.InputBase {
			return b.H(gpui.Px(32)).Disabled(true)
		}, focusInput(g.inputDisabled), base.NewInput(g.inputDisabled))),
	)
}

func (g *gallery) textareaPage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	return col(
		demo("Fixed height, three rows", inputBox("textarea-fixed", 360, func(b *base.InputBase) *base.InputBase {
			return b.H(gpui.Px(80)).Py2().OverflowHidden()
		}, focusTextarea(g.textarea), gpui.Div().SizeFull().Child(base.NewTextarea(g.textarea)))),
		demo("Auto-grow (2 to 6 rows)", inputBox("textarea-grow", 360, func(b *base.InputBase) *base.InputBase {
			return b.Py2()
		}, focusTextarea(g.textareaGrow), base.NewTextarea(g.textareaGrow))),
	)
}

func (g *gallery) editorPage(_ *gpui.Window, _ *gpui.Context[gallery]) any {
	return col(
		demo("Code mode: Go, line numbers, folding, indent guides", inputBox("editor-go", 620, func(b *base.InputBase) *base.InputBase {
			return b.H(gpui.Px(320)).Py2().OverflowHidden()
		}, focusEditor(g.editor), gpui.Div().SizeFull().Child(base.NewEditor(g.editor)))),
		demo("JSON, no gutter, soft wrap", inputBox("editor-json", 620, func(b *base.InputBase) *base.InputBase {
			return b.H(gpui.Px(120)).Py2().OverflowHidden()
		}, focusEditor(g.editorPlain), gpui.Div().SizeFull().Child(base.NewEditor(g.editorPlain)))),
		note("Syntax colours come from an application highlighter (InputEditorStyle.HighlightStyles); the gallery installs none, so code is drawn in the foreground colour."),
	)
}

func (g *gallery) numberInputPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	app := cx.App()
	stepButton := func(label any) func(*base.Button) *base.Button {
		return func(b *base.Button) *base.Button {
			return b.W7().Flex().ItemsCenter().JustifyCenter().TextXs().LineHeight(gpui.Fraction(1)).
				Bg(pal.elevated).Hover(gpui.Styles().Bg(pal.hover)).Child(label)
		}
	}
	number := func(state gpui.Entity[base.InputState], right, disabled bool) any {
		value := state.Read(app, func(s *base.InputState, _ *gpui.App) string { return s.Value() })
		_, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		border := pal.border
		if err != nil {
			border = pal.danger
		}
		n := base.NewNumberInput(state).
			Disabled(disabled).
			W(gpui.Px(200)).H8().
			Border1().BorderColor(border).RoundedMd().OverflowHidden().
			Input(gpui.Div().WFull().Px2().Child(base.NewInput(state))).
			DecrementButton(stepButton("−")).
			IncrementButton(stepButton("+"))
		if right {
			n = n.ControlsRight().
				DecrementButton(stepButton(chevron(down, pal.fg))).
				IncrementButton(stepButton(chevron(up, pal.fg)))
		}
		var view any = n
		if disabled {
			view = gpui.Div().Opacity(0.5).Child(n)
		}
		return col(view, note(map[bool]string{true: "Enter a number", false: "Value " + value}[err != nil]))
	}
	return col(
		demo("Buttons on both sides (step 1, 0–99; Up/Down keys step too)", number(g.quantity, false, false)),
		demo("Controls stacked on the right (step 0.25)", number(g.price, true, false)),
		demo("Disabled", number(g.price, false, true)),
	)
}

func otpCells(state gpui.Entity[base.OtpState], app *gpui.App) any {
	type read struct {
		value  string
		masked bool
	}
	r := state.Read(app, func(s *base.OtpState, _ *gpui.App) read { return read{s.Value(), s.IsMasked()} })
	chars := []rune(r.value)
	active := min(len(chars), 5)
	cells := base.NewOtpInput(state).Flex().Gap2()
	for ix := range 6 {
		text := ""
		if ix < len(chars) {
			text = string(chars[ix])
			if r.masked {
				text = "•"
			}
		}
		border := pal.border
		if ix == active {
			border = pal.fg
		}
		cells = cells.Child(gpui.Div().Size(gpui.Px(36)).Flex().ItemsCenter().JustifyCenter().
			TextBase().Border1().BorderColor(border).RoundedMd().Bg(pal.surface).Child(text))
	}
	return cells
}

func (g *gallery) otpPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	app := cx.App()
	value := g.otp.Read(app, func(s *base.OtpState, _ *gpui.App) string { return s.Value() })
	return col(
		demo("Six digits (click, then type)", col(otpCells(g.otp, app), note(fmt.Sprintf("Entered %q", value)))),
		demo("Masked", otpCells(g.otpMasked, app)),
	)
}

func (g *gallery) timeFieldPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	app := cx.App()
	field := func(id string, state gpui.Entity[base.TimeFieldState], disabled bool) any {
		f := base.NewTimeField(id, state).
			Disabled(disabled).
			Flex().ItemsCenter().H8().Px2().
			Border1().BorderColor(pal.border).RoundedMd().Bg(pal.surface).
			RenderSegment(func(segment *base.TimeFieldSegment, st base.TimeFieldSegmentState, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
				s := segment.Px(gpui.Px(2)).RoundedSm()
				if st.IsSelected() {
					s = s.Bg(pal.accent.Opacity(0.25))
				}
				return gpui.OwnRecipe(s)
			})
		if disabled {
			return gpui.Div().Opacity(0.5).Child(f)
		}
		return f
	}
	format := func(state gpui.Entity[base.TimeFieldState], layout string) string {
		t := state.Read(app, func(s *base.TimeFieldState, _ *gpui.App) base.NaiveTime { return s.Time() })
		out, err := t.Format(layout)
		if err != nil {
			return t.String()
		}
		return out
	}
	return col(
		demo("24-hour, seconds precision (arrow keys edit a segment)", row(
			field("time-24", g.timeField, false), note("Selected "+format(g.timeField, "%H:%M:%S")))),
		demo("12-hour, minutes precision", row(
			field("time-12", g.timeField12, false), note("Selected "+format(g.timeField12, "%I:%M %p")))),
		demo("Disabled", field("time-disabled", g.timeField12, true)),
	)
}

// optionList renders a popup list of options.
func optionList(prefix string, labels []string, selected string, pick func(label string) func(*gpui.ClickEvent, *gpui.Window, *gpui.App)) *gpui.DivElement {
	list := gpui.Div().Mt1().P1().W(gpui.Px(224)).Flex().FlexCol().
		Border1().BorderColor(pal.border).RoundedMd().Bg(pal.surface).TextSm()
	for ix, label := range labels {
		item := gpui.Div().ID(gpui.NamedIntegerElementID(prefix+"-option", uint64(ix))).CursorPointer().
			H7().Px2().Flex().ItemsCenter().JustifyBetween().RoundedSm().
			Hover(gpui.Styles().Bg(pal.hover)).
			OnClick(pick(label)).
			Child(label)
		if label == selected {
			item = item.Child("✓")
		}
		list = list.Child(item)
	}
	if len(labels) == 0 {
		list = list.Child(gpui.Div().H7().Px2().Flex().ItemsCenter().TextColor(pal.muted).Child("No results"))
	}
	return list
}

func selectTrigger(id, label string, open, disabled bool, onClick func(*gpui.ClickEvent, *gpui.Window, *gpui.App)) *gpui.DivElement {
	t := gpui.Div().ID(id).
		WFull().H8().Px3().Flex().ItemsCenter().JustifyBetween().
		Border1().BorderColor(pal.border).RoundedMd().Bg(pal.surface).
		Child(label).
		Child(openChevron(open))
	if disabled {
		return t.Opacity(0.45)
	}
	return t.CursorPointer().Hover(gpui.Styles().Bg(pal.hover)).OnClick(onClick)
}

func (g *gallery) selectPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	this := cx.Entity().Downgrade()
	open := g.selectOpen
	selected := frameworks[g.selectIndex]
	root := base.NewSelect("framework-select").
		Open(open).
		OnOpenChange(func(next bool, _ *gpui.Window, app *gpui.App) {
			set(this, app, func(g *gallery) { g.selectOpen = next })
		}).
		AccessibilityLabel("Framework").
		AccessibilityValue(selected).
		W(gpui.Px(224)).
		Child(selectTrigger("select-trigger", selected, open, false, g.update(cx, func(g *gallery) { g.selectOpen = !g.selectOpen })))
	popup := base.NewPopup("framework-select-options", root)
	if open {
		popup = popup.Content(optionList("select", frameworks, selected, func(label string) func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {
			return g.update(cx, func(g *gallery) {
				for ix, f := range frameworks {
					if f == label {
						g.selectIndex = ix
					}
				}
				g.selectOpen = false
			})
		}))
	}
	disabled := base.NewSelect("disabled-select").Disabled(true).W(gpui.Px(224)).
		Child(selectTrigger("disabled-select-trigger", "Unavailable", false, true, nil))
	return col(
		demo("Select (click the trigger; Escape closes)", gpui.Div().MinH(gpui.Px(40)).Child(popup)),
		demo("Disabled", disabled),
		note("Open the select to see the anchored option list; the popup is positioned by Base's Popup."),
	)
}

func (g *gallery) comboboxPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	app := cx.App()
	this := cx.Entity().Downgrade()
	open := g.comboOpen
	query := strings.ToLower(g.comboQuery.Read(app, func(s *base.InputState, _ *gpui.App) string { return s.Value() }))
	var matches []string
	for _, f := range frameworks {
		if query == "" || strings.Contains(strings.ToLower(f), query) {
			matches = append(matches, f)
		}
	}
	queryState := g.comboQuery
	root := base.NewCombobox("framework-combobox").
		Open(open).
		OnOpenChange(func(next bool, w *gpui.Window, app *gpui.App) {
			set(this, app, func(g *gallery) { g.comboOpen = next })
			if next {
				focusInput(queryState)(w, app)
			}
		}).
		W(gpui.Px(224)).
		Child(selectTrigger("combobox-trigger", g.comboValue, open, false, func(_ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
			set(this, app, func(g *gallery) { g.comboOpen = !open })
			if !open {
				focusInput(queryState)(w, app)
			}
		}))
	popup := base.NewPopup("framework-combobox-options", root)
	if open {
		popup = popup.Content(gpui.Div().Flex().FlexCol().Bg(pal.surface).RoundedMd().
			Child(gpui.Div().Mt1().
				Child(inputBox("combobox-search", 224, inputHeight(32), focusInput(queryState), base.NewInput(queryState)))).
			Child(optionList("combobox", matches, g.comboValue, func(label string) func(*gpui.ClickEvent, *gpui.Window, *gpui.App) {
				return g.update(cx, func(g *gallery) {
					g.comboValue = label
					g.comboOpen = false
				})
			})))
	}
	return col(
		demo("Combobox with a filter input", gpui.Div().MinH(gpui.Px(40)).Child(popup)),
		note("Type in the search field to filter; the list is application-owned."),
	)
}

var monthNames = []string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
var weekdayNames = []string{"Su", "Mo", "Tu", "We", "Th", "Fr", "Sa"}

// styledCalendar decorates every calendar item slot.
func styledCalendar(id string, state gpui.Entity[base.CalendarState], months int) *base.Calendar {
	return base.NewCalendar(id, state).
		NumberOfMonths(months).
		P3().Border1().BorderColor(pal.border).RoundedLg().Bg(pal.surface).TextXs().
		Item(func(item *base.CalendarItem, st base.CalendarItemState, _ *gpui.Window, _ *gpui.App) gpui.AnyElement {
			switch st.Kind() {
			case base.CalendarItemPrevious, base.CalendarItemNext:
				return gpui.OwnRecipe(item.Size7().CursorPointer().Flex().ItemsCenter().JustifyCenter().RoundedMd().
					Hover(gpui.Styles().Bg(pal.hover)))
			case base.CalendarItemMonthToggle, base.CalendarItemYearToggle:
				return gpui.OwnRecipe(item.Px1().H7().CursorPointer().Flex().ItemsCenter().JustifyCenter().RoundedMd().FontWeight(600).
					Hover(gpui.Styles().Bg(pal.hover)))
			case base.CalendarItemWeekday:
				return gpui.OwnRecipe(item.Size8().Flex().ItemsCenter().JustifyCenter().TextColor(pal.muted))
			case base.CalendarItemDay:
				d := item.Size8().Flex().ItemsCenter().JustifyCenter().RoundedMd()
				switch {
				case st.IsActive():
					d = d.Bg(pal.primary).TextColor(pal.primaryFg)
				case st.IsInRange():
					d = d.Bg(pal.selected)
				case st.IsToday():
					d = d.Border1().BorderColor(pal.border)
				}
				if st.IsMuted() || st.IsDisabled() {
					d = d.TextColor(pal.muted).Opacity(0.6)
				}
				if !st.IsDisabled() {
					d = d.CursorPointer()
				}
				if !st.IsDisabled() && !st.IsActive() {
					d = d.Hover(gpui.Styles().Bg(pal.hover))
				}
				return gpui.OwnRecipe(d)
			default: // Month and Year grids.
				d := item.W(gpui.Px(64)).H7().CursorPointer().Flex().ItemsCenter().JustifyCenter().RoundedMd()
				if st.IsActive() {
					d = d.Bg(pal.primary).TextColor(pal.primaryFg)
				} else {
					d = d.Hover(gpui.Styles().Bg(pal.hover))
				}
				return gpui.OwnRecipe(d)
			}
		}).
		Label(func(kind base.CalendarItemKind, value int32) string {
			switch kind {
			case base.CalendarItemPrevious:
				return "‹"
			case base.CalendarItemNext:
				return "›"
			case base.CalendarItemMonthToggle, base.CalendarItemMonth:
				if value >= 1 && int(value) < len(monthNames) {
					return monthNames[value]
				}
			case base.CalendarItemWeekday:
				if value >= 0 && int(value) < len(weekdayNames) {
					return weekdayNames[value]
				}
			}
			return strconv.Itoa(int(value))
		})
}

func formatDate(state gpui.Entity[base.CalendarState], app *gpui.App, empty string) string {
	return state.Read(app, func(s *base.CalendarState, _ *gpui.App) string {
		date := s.Date()
		start, ok := date.Start()
		if !ok {
			return empty
		}
		out, _ := start.Format("%b %d, %Y")
		if end, ok := date.End(); ok && !date.IsSingle() {
			to, _ := end.Format("%b %d, %Y")
			out += " – " + to
		}
		return out
	})
}

func (g *gallery) calendarPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	app := cx.App()
	return col(
		demo("Single date (today selected)", col(
			gpui.Div().W(gpui.Px(256)).Child(styledCalendar("calendar-single", g.calendar, 1)),
			note("Selected "+formatDate(g.calendar, app, "nothing")),
		)),
		demo("Range, weekends disabled", col(
			gpui.Div().W(gpui.Px(256)).Child(styledCalendar("calendar-range", g.rangeCalendar, 1)),
			note("Range "+formatDate(g.rangeCalendar, app, "not set")),
		)),
	)
}

func (g *gallery) datePickerPage(_ *gpui.Window, cx *gpui.Context[gallery]) any {
	app := cx.App()
	this := cx.Entity().Downgrade()
	open := g.dateOpen
	label := formatDate(g.dateCalendar, app, "Pick a date")
	trigger := btn("date-trigger", gpui.Div().WFull().Flex().JustifyBetween().
		Child("📅 "+label).Child(openChevron(open)), outline, md).
		WFull().JustifyStart().
		OnClick(g.update(cx, func(g *gallery) { g.dateOpen = !g.dateOpen }))
	popup := base.NewPopup("date-picker-popup", trigger)
	if open {
		popup = popup.Content(gpui.Div().Mt1().W(gpui.Px(256)).Child(styledCalendar("date-picker-calendar", g.dateCalendar, 1)))
	}
	picker := base.NewDatePicker("date-picker", g.dateFocus).
		Open(open).
		OnOpenChange(func(next bool, _ *gpui.Window, app *gpui.App) {
			set(this, app, func(g *gallery) { g.dateOpen = next })
		}).
		W(gpui.Px(256)).
		Child(popup)
	disabled := base.NewDatePicker("date-picker-disabled", g.dateFocus).Disabled(true).W(gpui.Px(256)).
		Child(gpui.Div().Opacity(0.45).Child(btn("date-trigger-disabled", "Unavailable", outline, md).WFull().Disabled(true)))
	return col(
		demo("DatePicker (trigger + calendar in a popup)", gpui.Div().MinH(gpui.Px(40)).Child(picker)),
		demo("Disabled", disabled),
	)
}

func (g *gallery) colorPickerPage(w *gpui.Window, cx *gpui.Context[gallery]) any {
	app := cx.App()
	// A builder default reaches the hex field and the sliders on the
	// first render with a window.
	g.colorPicker.Update(app, func(st *base.ColorPickerState, pcx *gpui.Context[base.ColorPickerState]) {
		st.SyncPendingValue(w, pcx)
	})
	type read struct {
		open      bool
		value     gpui.Hsla
		hasValue  bool
		displayed gpui.Hsla
		hexInput  gpui.Entity[base.InputState]
		focus     gpui.FocusHandle
		sliders   base.HslaSliders
	}
	r := g.colorPicker.Read(app, func(s *base.ColorPickerState, app *gpui.App) read {
		value, has := s.Value()
		displayed, ok := s.DisplayedColor()
		if !ok {
			displayed = pal.fg
		}
		return read{s.IsOpen(), value, has, displayed, s.HexInput(), s.FocusHandle(app), s.Sliders()}
	})
	hex := r.hexInput.Read(app, func(s *base.InputState, _ *gpui.App) string { return s.Value() })
	state := g.colorPicker

	trigger := gpui.Div().ID("color-trigger").CursorPointer().
		WFull().H8().Px2().Flex().ItemsCenter().Gap2().
		Border1().BorderColor(pal.border).RoundedMd().Bg(pal.surface).
		Hover(gpui.Styles().Bg(pal.hover)).
		OnClick(func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
			state.Update(app, func(s *base.ColorPickerState, pcx *gpui.Context[base.ColorPickerState]) { s.ToggleOpen(pcx) })
		}).
		Child(gpui.Div().Size4().RoundedSm().Bg(r.displayed).Border1().BorderColor(pal.border)).
		Child(hex).
		Child(gpui.Div().Flex1()).
		Child(openChevron(r.open))

	swatches := gpui.Div().Flex().Gap1()
	for ix, hexValue := range []uint32{0xdc2626, 0xd97706, 0x16a34a, 0x2563eb, 0x7c3aed, 0x171717} {
		color := gpui.Rgb(hexValue)
		chosen := r.hasValue && r.value == color
		border := pal.surface
		if chosen {
			border = pal.fg
		}
		swatches = swatches.Child(base.NewColorSwatch(gpui.NamedIntegerElementID("swatch", uint64(ix)), color).
			Selected(chosen).
			CursorPointer().
			Size(gpui.Px(24)).RoundedSm().Bg(color).Border2().BorderColor(border).
			OnHover(func(color gpui.Hsla, entered bool, w *gpui.Window, app *gpui.App) {
				state.Update(app, func(s *base.ColorPickerState, pcx *gpui.Context[base.ColorPickerState]) {
					if entered {
						s.PreviewColor(color, w, pcx)
					} else {
						s.ClearPreview(w, pcx)
					}
				})
			}).
			OnClick(func(color gpui.Hsla, _ *gpui.ClickEvent, w *gpui.Window, app *gpui.App) {
				state.Update(app, func(s *base.ColorPickerState, pcx *gpui.Context[base.ColorPickerState]) { s.SelectColor(color, w, pcx) })
			}))
	}
	channel := func(label string, slider gpui.Entity[base.SliderState]) any {
		return gpui.Div().Flex().ItemsCenter().Gap2().
			Child(gpui.Div().W3().TextXs().TextColor(pal.muted).Child(label)).
			Child(gpui.Div().W(gpui.Px(200)).Child(sliderView(slider, false, false, app)))
	}
	content := gpui.Div().Mt1().W(gpui.Px(260)).P3().Flex().FlexCol().Gap3().
		Border1().BorderColor(pal.border).RoundedLg().Bg(pal.surface).
		Child(swatches).
		Child(channel("H", r.sliders.Hue())).
		Child(channel("S", r.sliders.Saturation())).
		Child(channel("L", r.sliders.Lightness())).
		Child(inputBox("color-hex-input", 232, inputHeight(28), focusInput(r.hexInput), base.NewInput(r.hexInput)))

	root := base.NewColorPicker("brand-color").
		Open(r.open).
		TrackFocus(r.focus).
		AccessibilityLabel("Brand color").
		OnOpenChange(func(open bool, _ *gpui.Window, app *gpui.App) {
			state.Update(app, func(s *base.ColorPickerState, pcx *gpui.Context[base.ColorPickerState]) { s.SetOpen(open, pcx) })
		}).
		W(gpui.Px(260)).
		Child(trigger)
	popup := base.NewPopup("brand-color-popup", root)
	if r.open {
		popup = popup.Content(content)
	}
	return col(
		demo("ColorPicker (swatches preview on hover, HSL sliders, hex field)", gpui.Div().MinH(gpui.Px(40)).Child(popup)),
		demo("Committed colour", row(
			gpui.Div().Size(gpui.Px(48)).RoundedLg().Bg(r.displayed).Border1().BorderColor(pal.border),
			note(fmt.Sprintf("hex %s · h %.2f s %.2f l %.2f", hex, r.displayed.H, r.displayed.S, r.displayed.L)),
		)),
	)
}
