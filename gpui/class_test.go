package gpui

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// These tests cover Styled.Class (class.go), the StyleBuilder state
// argument (style_builder.go) and the color helpers (color_helpers.go)
// without a window. The parse cache and the set of reported strings are
// process-wide, so the error tests use class strings of their own;
// installing a handler clears the reported set.

// classTestCase is a class string and the method chain it must equal.
type classTestCase struct {
	classes string
	want    func(d *DivElement) *DivElement
}

// classTestCheck applies each case's classes to a fresh div and compares
// its base style with the one the method chain builds.
func classTestCheck(t *testing.T, cases []classTestCase) {
	t.Helper()
	for _, tc := range cases {
		got := *Div().Class(tc.classes).StyleMut()
		want := *tc.want(Div()).StyleMut()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Class(%q):\n got %+v\nwant %+v", tc.classes, got, want)
		}
	}
}

// classTestReport is one call of the class error handler.
type classTestReport struct {
	classes string
	err     error
}

// classTestCapture installs a class error handler that records every
// report, and restores the previous handler when the test ends.
func classTestCapture(t *testing.T) *[]classTestReport {
	t.Helper()
	reports := &[]classTestReport{}
	previous := SetClassErrorHandler(func(classes string, err error) {
		*reports = append(*reports, classTestReport{classes, err})
	})
	t.Cleanup(func() { SetClassErrorHandler(previous) })
	return reports
}

// classTestNoErrors installs a class error handler that fails the test,
// and restores the previous handler when the test ends.
func classTestNoErrors(t *testing.T) {
	t.Helper()
	previous := SetClassErrorHandler(func(classes string, err error) {
		t.Errorf("Class(%q) reported: %v", classes, err)
	})
	t.Cleanup(func() { SetClassErrorHandler(previous) })
}

// classTestWantPanic runs f and requires a panic mentioning want.
func classTestWantPanic(t *testing.T, want string, f func()) {
	t.Helper()
	defer func() {
		t.Helper()
		if r := recover(); r == nil || !strings.Contains(fmt.Sprint(r), want) {
			t.Errorf("panic = %v; want one mentioning %q", r, want)
		}
	}()
	f()
}

// classTestGeneratedClasses returns the case labels of applyMethodClass
// in class_generated.go.
func classTestGeneratedClasses(t *testing.T) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "class_generated.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var classes []string
	for _, decl := range f.Decls {
		fd, ok := decl.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "applyMethodClass" {
			continue
		}
		ast.Inspect(fd.Body, func(n ast.Node) bool {
			cc, ok := n.(*ast.CaseClause)
			if !ok {
				return true
			}
			for _, e := range cc.List {
				lit, ok := e.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					t.Fatalf("applyMethodClass has a non-string case %T", e)
				}
				class, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				classes = append(classes, class)
			}
			return true
		})
	}
	if len(classes) == 0 {
		t.Fatal("no applyMethodClass cases in class_generated.go")
	}
	return classes
}

// TestClassMatchesMethods checks that a class sets exactly what the
// fluent method of the same name sets, and that classes apply left to
// right.
func TestClassMatchesMethods(t *testing.T) {
	classTestNoErrors(t)
	classTestCheck(t, []classTestCase{
		{"p-4", func(d *DivElement) *DivElement { return d.P4() }},
		{"items-center", func(d *DivElement) *DivElement { return d.ItemsCenter() }},
		{"w-1/2", func(d *DivElement) *DivElement { return d.W1_2() }},
		{"-mt-2", func(d *DivElement) *DivElement { return d.MtNeg2() }},
		{"text-2xl", func(d *DivElement) *DivElement { return d.Text2xl() }},
		{"rounded-lg", func(d *DivElement) *DivElement { return d.RoundedLg() }},
		{"font-bold", func(d *DivElement) *DivElement { return d.FontBold() }},
		{"opacity-50", func(d *DivElement) *DivElement { return d.Opacity50() }},
		{"shadow-md", func(d *DivElement) *DivElement { return d.ShadowMd() }},
		{"truncate", func(d *DivElement) *DivElement { return d.Truncate() }},
		{"border-t-2", func(d *DivElement) *DivElement { return d.BorderT2() }},
		{"flex flex-col justify-between gap-2", func(d *DivElement) *DivElement {
			return d.Flex().FlexCol().JustifyBetween().Gap2()
		}},
		{"cursor-pointer uppercase leading-tight shrink-0", func(d *DivElement) *DivElement {
			return d.CursorPointer().Uppercase().LeadingTight().Shrink0()
		}},
		// A later class wins; the important modifier changes nothing.
		{"p-2 p-4", func(d *DivElement) *DivElement { return d.P4() }},
		{"!p-4 items-center!", func(d *DivElement) *DivElement { return d.P4().ItemsCenter() }},
		{"  ", func(d *DivElement) *DivElement { return d }},
	})
}

// TestClassGeneratedClassesAreValid checks that every class of the
// generated method table passes CheckClasses.
func TestClassGeneratedClassesAreValid(t *testing.T) {
	classes := classTestGeneratedClasses(t)
	if len(classes) != methodClassCount {
		t.Fatalf("class_generated.go has %d cases, methodClassCount = %d", len(classes), methodClassCount)
	}
	for _, class := range classes {
		if err := CheckClasses(class); err != nil {
			t.Errorf("CheckClasses(%q) = %v", class, err)
		}
	}
}

// TestClassLengthFallbackMatchesMethods proves that every spacing,
// sizing and inset method class the switch leaves to the length fallback
// (class_generated_test.go, emitted by cmd/tailwindgen from the method
// bodies) applies exactly the method's field writes through Class's
// production path. The %+v comparison also catches the generated
// negative zero (-m-0 is DefinitePx(-0)).
func TestClassLengthFallbackMatchesMethods(t *testing.T) {
	if len(lengthMethodClasses) < 1500 {
		t.Fatalf("lengthMethodClasses has %d entries; want the ~1,860 spacing and sizing classes", len(lengthMethodClasses))
	}
	for _, c := range lengthMethodClasses {
		if applyMethodClass(c.class, Styles()) {
			t.Errorf("%q is both in the switch and left to the fallback", c.class)
		}
		got := Styles()
		if ok, reason := applyUtility(got, c.class); !ok {
			t.Errorf("applyUtility(%q) failed: %q", c.class, reason)
			continue
		}
		var want StyleRefinement
		c.want(&want)
		g := *got.StyleMut()
		if !reflect.DeepEqual(g, want) || fmt.Sprintf("%+v", g) != fmt.Sprintf("%+v", want) {
			t.Errorf("Class %q (%s):\n got %+v\nwant %+v", c.class, c.method, g, want)
		}
	}
}

// TestClassRejectsGPUIOnlyMethodClasses checks that the method classes
// with no Tailwind counterpart (-p-4, -w-4, min-size-4) are rejected
// rather than silently applied.
func TestClassRejectsGPUIOnlyMethodClasses(t *testing.T) {
	if len(gpuiOnlyMethodClasses) == 0 {
		t.Fatal("gpuiOnlyMethodClasses is empty")
	}
	for _, class := range gpuiOnlyMethodClasses {
		if CheckClasses(class) == nil {
			t.Errorf("CheckClasses(%q) = nil; want an error (no Tailwind class)", class)
		}
	}
}

// TestClassColors checks the color classes: palette names, white, black,
// transparent, hex values and the opacity modifier.
func TestClassColors(t *testing.T) {
	classTestNoErrors(t)
	classTestCheck(t, []classTestCase{
		{"bg-slate-800", func(d *DivElement) *DivElement { return d.Bg(Rgb(0x1e293b)) }},
		{"text-white", func(d *DivElement) *DivElement { return d.TextColor(Rgb(0xffffff)) }},
		{"border-red-500", func(d *DivElement) *DivElement { return d.BorderColor(Rgb(0xef4444)) }},
		{"ring-blue-500", func(d *DivElement) *DivElement { return d.RingColor(Rgb(0x3b82f6)) }},
		{"inset-ring-red-500", func(d *DivElement) *DivElement { return d.InsetRingColor(Rgb(0xef4444)) }},
		{"bg-black/50", func(d *DivElement) *DivElement { return d.Bg(Rgb(0).Alpha(0.5)) }},
		{"text-slate-500/75", func(d *DivElement) *DivElement { return d.TextColor(Rgb(0x64748b).Alpha(0.75)) }},
		{"border-black/[35%]", func(d *DivElement) *DivElement { return d.BorderColor(Black().Alpha(0.35)) }},
		{"bg-[#1e293b]", func(d *DivElement) *DivElement { return d.Bg(Rgb(0x1e293b)) }},
		{"bg-[#1e293b]/[0.35]", func(d *DivElement) *DivElement { return d.Bg(Rgb(0x1e293b).Alpha(0.35)) }},
		{"bg-[#fff8]", func(d *DivElement) *DivElement { return d.Bg(Hex("#fff8")) }},
		{"bg-transparent", func(d *DivElement) *DivElement { return d.Bg(Rgb(0).Alpha(0)) }},
		// The method classes win over colors: text-sm is a size.
		{"text-sm text-white", func(d *DivElement) *DivElement { return d.TextSm().TextColor(White()) }},
	})
}

// TestClassArbitraryValues checks the classes whose method takes the
// value as an argument: arbitrary [..] values, numbers off the generated
// scale, the bare border, rounded, shadow and ring classes, and the
// typography, grid and blur families.
func TestClassArbitraryValues(t *testing.T) {
	classTestNoErrors(t)
	classTestCheck(t, []classTestCase{
		{"w-[84px]", func(d *DivElement) *DivElement { return d.W(Px(84)) }},
		{"w-[50%]", func(d *DivElement) *DivElement { return d.W(Fraction(0.5)) }},
		{"w-[1.5rem]", func(d *DivElement) *DivElement { return d.W(RemsOf(1.5)) }},
		{"p-[3px]", func(d *DivElement) *DivElement { return d.P(Px(3)) }},
		{"-mt-[4px]", func(d *DivElement) *DivElement { return d.Mt(DefinitePx(-4)) }},
		{"p-13", func(d *DivElement) *DivElement { return d.P(DefiniteRem(3.25)) }},
		{"w-0.75", func(d *DivElement) *DivElement { return d.W(DefiniteRem(0.1875)) }},
		{"max-w-md", func(d *DivElement) *DivElement { return d.MaxW(RemsLength(28)) }},
		{"max-w-none", func(d *DivElement) *DivElement { return d.MaxW(AutoLength()) }},
		{"inset-x-0", func(d *DivElement) *DivElement { return d.Left0().Right0() }},
		{"-inset-x-2", func(d *DivElement) *DivElement { return d.LeftNeg2().RightNeg2() }},
		{"top-[13px]", func(d *DivElement) *DivElement { return d.Top(Px(13)) }},
		{"size-[20px]", func(d *DivElement) *DivElement { return d.W(Px(20)).H(Px(20)) }},
		{"basis-1/2", func(d *DivElement) *DivElement { return d.FlexBasis(Fraction(0.5)) }},
		{"gap-[6px]", func(d *DivElement) *DivElement { return d.GapX(Px(6)).GapY(Px(6)) }},
		{"text-[13px]", func(d *DivElement) *DivElement { return d.TextSize(Px(13)) }},
		{"text-[0.8rem]", func(d *DivElement) *DivElement { return d.TextSize(RemsOf(0.8)) }},
		{"rounded-[10px]", func(d *DivElement) *DivElement { return d.Rounded(Px(10)) }},
		{"rounded-tl-[6px]", func(d *DivElement) *DivElement { return d.RoundedTl(Px(6)) }},
		{"border-[3px]", func(d *DivElement) *DivElement { return d.Border(Px(3)) }},
		{"border-t-[3px]", func(d *DivElement) *DivElement { return d.BorderT(Px(3)) }},
		{"border-13", func(d *DivElement) *DivElement { return d.Border(Px(13)) }},
		{"border", func(d *DivElement) *DivElement { return d.Border1() }},
		{"border-x", func(d *DivElement) *DivElement { return d.BorderX1() }},
		{"rounded", func(d *DivElement) *DivElement { return d.Rounded(RemsOf(0.25)) }},
		{"rounded-t", func(d *DivElement) *DivElement { return d.RoundedT(RemsOf(0.25)) }},
		{"shadow", func(d *DivElement) *DivElement { return d.ShadowSm() }},
		{"ring", func(d *DivElement) *DivElement { return d.Ring(3) }},
		{"flex-grow", func(d *DivElement) *DivElement { return d.FlexGrow(1) }},
		{"aspect-video", func(d *DivElement) *DivElement { return d.AspectRatio(16.0 / 9.0) }},
		{"aspect-[4/3]", func(d *DivElement) *DivElement { return d.AspectRatio(float32(4) / float32(3)) }},
		{"ring-3", func(d *DivElement) *DivElement { return d.Ring(3) }},
		{"ring-[3px]", func(d *DivElement) *DivElement { return d.Ring(3) }},
		{"inset-ring-[2px]", func(d *DivElement) *DivElement { return d.InsetRing(2) }},
		{"leading-[1.4]", func(d *DivElement) *DivElement { return d.LineHeight(Fraction(1.4)) }},
		{"leading-[20px]", func(d *DivElement) *DivElement { return d.LineHeight(Px(20)) }},
		{"leading-6", func(d *DivElement) *DivElement { return d.LineHeight(DefiniteRem(1.5)) }},
		{"tracking-[0.5px]", func(d *DivElement) *DivElement { return d.LetterSpacing(0.5) }},
		{"font-[550]", func(d *DivElement) *DivElement { return d.FontWeight(550) }},
		{"line-clamp-2", func(d *DivElement) *DivElement { return d.LineClamp(2) }},
		{"opacity-37", func(d *DivElement) *DivElement { return d.Opacity(0.37) }},
		{"opacity-[0.35]", func(d *DivElement) *DivElement { return d.Opacity(0.35) }},
		{"grid-cols-3", func(d *DivElement) *DivElement { return d.GridCols(3) }},
		{"col-span-2", func(d *DivElement) *DivElement { return d.ColSpan(2) }},
		{"col-start-2", func(d *DivElement) *DivElement { return d.ColStart(2) }},
		{"blur-md", func(d *DivElement) *DivElement { return d.Filter([]Filter{Blur(12)}) }},
		{"blur", func(d *DivElement) *DivElement { return d.Filter([]Filter{Blur(8)}) }},
		{"blur-[6px]", func(d *DivElement) *DivElement { return d.Filter([]Filter{Blur(6)}) }},
		{"backdrop-blur-sm", func(d *DivElement) *DivElement { return d.BackdropBlur(4) }},
		{"overflow-clip", func(d *DivElement) *DivElement {
			st := d.StyleMut()
			st.OverflowX, st.OverflowY = Some(OverflowClip), Some(OverflowClip)
			return d
		}},
	})
}

// TestClassComposesEditingMethods checks that classes whose methods edit
// a shared value (the grid location, the underline) compose within one
// string the way the method chain does.
func TestClassComposesEditingMethods(t *testing.T) {
	classTestNoErrors(t)
	classTestCheck(t, []classTestCase{
		{"col-start-2 col-end-4", func(d *DivElement) *DivElement { return d.ColStart(2).ColEnd(4) }},
		{"col-span-2 row-span-3", func(d *DivElement) *DivElement { return d.ColSpan(2).RowSpan(3) }},
		{"underline text-decoration-wavy text-decoration-2", func(d *DivElement) *DivElement {
			return d.Underline().TextDecorationWavy().TextDecoration2()
		}},
	})
}

// TestClassVariantsOnDiv checks that state variants, group classes and
// scrolling classes configure the div's interactivity, merging into the
// state styles it already has.
func TestClassVariantsOnDiv(t *testing.T) {
	classTestNoErrors(t)
	slate700 := Some(SolidBackground(Rgb(0x334155)))
	slate900 := Some(SolidBackground(Rgb(0x0f172a)))
	white := Some(White())

	d := Div().ID("x").Class("hover:bg-slate-700 hover:text-white active:bg-slate-900 focus:border-blue-500 group group-hover:text-white")
	if base := d.StyleMut(); base.Background.IsSome() || base.Text.Color.IsSome() || base.BorderColor.IsSome() {
		t.Errorf("state classes reached the base style: %+v", *base)
	}
	if d.hoverStyle == nil {
		t.Fatal("hover style not set")
	}
	if d.hoverStyle.Background != slate700 || d.hoverStyle.Text.Color != white {
		t.Errorf("hover style = %+v; want the slate-700 background and white text merged", *d.hoverStyle)
	}
	if d.activeStyle == nil || d.activeStyle.Background != slate900 {
		t.Errorf("active style = %+v", d.activeStyle)
	}
	if d.focusStyle == nil || d.focusStyle.BorderColor != Some(SolidBackground(Rgb(0x3b82f6))) {
		t.Errorf("focus style = %+v", d.focusStyle)
	}
	if d.group == nil || *d.group != "" {
		t.Errorf("group = %v; want the unnamed group", d.group)
	}
	if gs := d.groupHoverStyle; gs == nil || gs.Group != "" || gs.Style.Text.Color != white {
		t.Errorf("group hover style = %+v", gs)
	}

	d = Div().ID("y").Class("focus-visible:bg-slate-700 focus-within:bg-slate-900 group-active:text-white")
	if d.focusVisibleStyle == nil || d.focusVisibleStyle.Background != slate700 {
		t.Errorf("focus-visible style = %+v", d.focusVisibleStyle)
	}
	if d.inFocusStyle == nil || d.inFocusStyle.Background != slate900 {
		t.Errorf("focus-within (in-focus) style = %+v", d.inFocusStyle)
	}
	if gs := d.groupActiveStyle; gs == nil || gs.Group != "" || gs.Style.Text.Color != white {
		t.Errorf("group active style = %+v", gs)
	}

	// Named groups.
	if g := Div().ID("g").Class("group/menu"); g.group == nil || *g.group != "menu" {
		t.Errorf("group/menu group = %v", g.group)
	}
	d = Div().ID("m").Class("group-hover/menu:text-white group-active/menu:bg-slate-900")
	if gs := d.groupHoverStyle; gs == nil || gs.Group != "menu" || gs.Style.Text.Color != white {
		t.Errorf("group-hover/menu style = %+v", gs)
	}
	if gs := d.groupActiveStyle; gs == nil || gs.Group != "menu" || gs.Style.Background != slate900 {
		t.Errorf("group-active/menu style = %+v", gs)
	}

	// Class merges into state styles set before, by a builder or a Class.
	d = Div().ID("merge").Hover(Styles().Bg(Rgb(0x334155))).Class("hover:text-white")
	if d.hoverStyle.Background != slate700 || d.hoverStyle.Text.Color != white {
		t.Errorf("Hover then Class: hover style = %+v; want both properties", *d.hoverStyle)
	}
	d = Div().ID("merge2").Class("hover:bg-slate-700").Class("hover:text-white")
	if d.hoverStyle.Background != slate700 || d.hoverStyle.Text.Color != white {
		t.Errorf("Class then Class: hover style = %+v; want both properties", *d.hoverStyle)
	}
	d = Div().GroupHover("menu", Styles().Bg(Rgb(0x334155))).Class("group-hover/menu:text-white")
	if gs := d.groupHoverStyle; gs.Group != "menu" || gs.Style.Background != slate700 || gs.Style.Text.Color != white {
		t.Errorf("GroupHover then Class: group hover style = %+v; want both properties", *gs)
	}

	// Setting the hover state after a Class with hover: panics like
	// setting it twice.
	classTestWantPanic(t, "hover style already set", func() {
		Div().ID("p").Class("hover:bg-slate-700").Hover(Styles().TextColor(White()))
	})

	// Scrolling classes match the scroll builders.
	s := Div().ID("s").Class("overflow-y-auto")
	if v, ok := s.StyleMut().OverflowY.Get(); !ok || v != OverflowScroll {
		t.Errorf("overflow-y-auto OverflowY = %v, %v; want OverflowScroll", v, ok)
	}
	if s.StyleMut().OverflowX.IsSome() {
		t.Error("overflow-y-auto set OverflowX")
	}
	if !s.scrollStateful {
		t.Error("overflow-y-auto did not make the div scroll-stateful")
	}
	if want := Div().ID("s").OverflowYScroll(); !reflect.DeepEqual(*s.StyleMut(), *want.StyleMut()) {
		t.Errorf("overflow-y-auto = %+v; want OverflowYScroll's %+v", *s.StyleMut(), *want.StyleMut())
	}
	s = Div().ID("s2").Class("overflow-scroll")
	if want := Div().ID("s2").OverflowScroll(); !s.scrollStateful || !reflect.DeepEqual(*s.StyleMut(), *want.StyleMut()) {
		t.Errorf("overflow-scroll = %+v (stateful %v); want OverflowScroll's %+v", *s.StyleMut(), s.scrollStateful, *want.StyleMut())
	}
}

// classTestCard is a component embedding Component: its own style, and
// interactivity delegated to an inner div.
type classTestCard struct {
	Component[*classTestCard]
}

func newClassTestCard(id string) *classTestCard {
	return BindComponent(&classTestCard{}, Div().ID(id))
}

// TestClassVariantsOnComponent checks Class on a component: base classes
// write the component's own style, and state, group and scroll classes
// reach the inner div's interactivity.
func TestClassVariantsOnComponent(t *testing.T) {
	classTestNoErrors(t)
	c := newClassTestCard("card").Class("p-2 hover:bg-slate-700 group overflow-y-auto")
	own := c.StyleMut()
	if v, ok := own.PaddingTop.Get(); !ok || v != DefiniteRem(0.5) {
		t.Errorf("component padding = %v, %v; want 0.5rem", v, ok)
	}
	if v, ok := own.OverflowY.Get(); !ok || v != OverflowScroll {
		t.Errorf("component OverflowY = %v, %v; want OverflowScroll", v, ok)
	}
	if own.Background.IsSome() {
		t.Error("hover background reached the component's base style")
	}
	inner := c.Interactivity()
	if inner.StyleMut().PaddingTop.IsSome() {
		t.Error("padding reached the inner div; want the component's own style")
	}
	if inner.hoverStyle == nil || inner.hoverStyle.Background != Some(SolidBackground(Rgb(0x334155))) {
		t.Errorf("inner div hover style = %+v", inner.hoverStyle)
	}
	if inner.group == nil || *inner.group != "" {
		t.Errorf("inner div group = %v", inner.group)
	}
	if !inner.scrollStateful {
		t.Error("overflow-y-auto did not make the inner div scroll-stateful")
	}
	classTestWantPanic(t, "hover style already set", func() {
		c.Hover(Styles().TextColor(White()))
	})
}

// TestClassStatesOnStyleBuilderReport checks that state classes on a
// StyleBuilder, which has no interactivity, are reported once while the
// base classes still apply.
func TestClassStatesOnStyleBuilderReport(t *testing.T) {
	reports := classTestCapture(t)
	const classes = "bg-red-500 hover:bg-red-600"
	b := Styles().Class(classes)
	if len(*reports) != 1 {
		t.Fatalf("handler ran %d times; want 1: %+v", len(*reports), *reports)
	}
	r := (*reports)[0]
	if r.classes != classes || r.err == nil || !strings.Contains(r.err.Error(), "hover:bg-red-600") {
		t.Errorf("report = %q, %v; want the string and an error naming hover:bg-red-600", r.classes, r.err)
	}
	if v, ok := b.StyleMut().Background.Get(); !ok || v != SolidBackground(Rgb(0xef4444)) {
		t.Errorf("bg-red-500 on the builder = %v, %v", v, ok)
	}
	Styles().Class(classes)
	if len(*reports) != 1 {
		t.Errorf("handler ran %d times for one string; want 1", len(*reports))
	}
}

// TestCheckClassesNamesEveryProblem checks that CheckClasses reports
// each unusable class of a string, and nothing for a valid one.
func TestCheckClassesNamesEveryProblem(t *testing.T) {
	err := CheckClasses("flex foo z-10 sm:p-4 space-x-2")
	if err == nil {
		t.Fatal("CheckClasses = nil; want four problems")
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("CheckClasses error %T does not unwrap to a list", err)
	}
	var got []string
	for _, e := range joined.Unwrap() {
		var ce *classError
		if !errors.As(e, &ce) {
			t.Errorf("error %v (%T) is not a classError", e, e)
			continue
		}
		got = append(got, ce.class)
	}
	if want := []string{"foo", "z-10", "sm:p-4", "space-x-2"}; !reflect.DeepEqual(got, want) {
		t.Errorf("reported classes = %q; want %q", got, want)
	}
	msg := err.Error()
	for _, class := range []string{`"foo"`, `"z-10"`, `"sm:p-4"`, `"space-x-2"`} {
		if !strings.Contains(msg, class) {
			t.Errorf("error %q does not name %s", msg, class)
		}
	}
	if strings.Contains(msg, `"flex"`) {
		t.Errorf("error %q names the valid class flex", msg)
	}

	for _, valid := range []string{
		"",
		"flex items-center gap-2 px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700",
		"w-[84px] -mt-[4px] text-[13px] bg-black/50 bg-[#1e293b]/[0.35] group group-hover:text-white overflow-y-auto",
		"focus-visible:ring-2 focus-within:ring-blue-500 active:bg-slate-900 !p-4",
	} {
		if err := CheckClasses(valid); err != nil {
			t.Errorf("CheckClasses(%q) = %v; want nil", valid, err)
		}
	}
}

// TestCheckClassesReasons checks that unsupported classes say why.
func TestCheckClassesReasons(t *testing.T) {
	for _, tc := range []struct{ classes, want string }{
		{"foo", `unknown class "foo"`},
		{"z-10", "z-index"},
		{"space-x-2", "gap-*"},
		{"sm:p-4", "responsive variants"},
		{"dark:bg-black", "dark:"},
		{"hover:focus:bg-red-500", "stacked variants"},
		{"group-focus:bg-red-500", "group-hover: and group-active:"},
		{"peer-hover:bg-red-500", "peer variants"},
		{"hover:group", "group cannot take a variant"},
		{"group/", "needs a name"},
		{"group/a group/b", "belongs to one group"},
		{"group-hover/a:text-white group-hover/b:bg-red-500", "only one group"},
		{"hover:overflow-auto", "scrolling cannot depend on a state"},
		{"w-screen", "viewport units"},
		{"h-dvh", "viewport units"},
		{"w-fit", "fit/min/max-content"},
		{"w-[84vw]", "is not a length here"},
		{"-w-[4px]", "margins and insets only"},
		{"p-auto", "auto is not a valid value here"},
		{"tracking-tight", "em-based"},
		{"font-sans", "FontFamily"},
		{"text-justify", "justified text"},
		{"bg-slate-850", "unknown shade"},
		{"bg-brand", "palette color"},
		{"text-current", "currentColor"},
		{"border-t-red-500", "per-side border colors"},
		{"bg-gradient-to-r", "gradients"},
		{"rounded-[50%]", "px or rem"},
		{"opacity-150", "0–100"},
		{"grid-cols-0", "takes a count"},
		{"blur-huge", "blur-…"},
		{"backdrop-grayscale", "backdrop-blur"},
		{"shadow-red-500", "shadow colors"},
		{"translate-x-1", "transforms"},
		{"transition", "transitions"},
		{"fixed", "fixed positioning"},
	} {
		err := CheckClasses(tc.classes)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("CheckClasses(%q) = %v; want an error mentioning %q", tc.classes, err, tc.want)
		}
	}
}

// TestClassUnknownClassesAreSkipped checks that applying an unknown class
// neither panics nor stops the other classes.
func TestClassUnknownClassesAreSkipped(t *testing.T) {
	reports := classTestCapture(t)
	d := Div().Class("p-1 classtest-skip-me bg-slate-800 z-10")
	if v, ok := d.StyleMut().PaddingLeft.Get(); !ok || v != DefiniteRem(0.25) {
		t.Errorf("p-1 = %v, %v", v, ok)
	}
	if v, ok := d.StyleMut().Background.Get(); !ok || v != SolidBackground(Rgb(0x1e293b)) {
		t.Errorf("bg-slate-800 = %v, %v", v, ok)
	}
	if len(*reports) != 1 || !strings.Contains((*reports)[0].err.Error(), "classtest-skip-me") {
		t.Errorf("reports = %+v; want one naming classtest-skip-me", *reports)
	}
	Styles().Class("classtest-skip-me-too")
	newClassTestCard("skip").Class("classtest-skip-me-also hover:classtest-nope")
	if len(*reports) != 3 {
		t.Errorf("handler ran %d times; want 3", len(*reports))
	}
}

// TestClassErrorsReportOncePerString checks that the handler runs once
// per distinct bad string, and again after a handler is installed.
func TestClassErrorsReportOncePerString(t *testing.T) {
	reports := classTestCapture(t)
	const bad = "p-1 classtest-once-a"
	Div().Class(bad)
	Div().Class(bad)
	if len(*reports) != 1 {
		t.Fatalf("handler ran %d times for one string; want 1", len(*reports))
	}
	if (*reports)[0].classes != bad {
		t.Errorf("reported string = %q; want %q", (*reports)[0].classes, bad)
	}
	Div().Class("p-1 classtest-once-b")
	if len(*reports) != 2 {
		t.Errorf("handler ran %d times for two strings; want 2", len(*reports))
	}
	// Installing a handler forgets the strings already reported.
	again := classTestCapture(t)
	Div().Class(bad)
	if len(*again) != 1 || len(*reports) != 2 {
		t.Errorf("after a new handler: new %d, old %d reports; want 1 and 2", len(*again), len(*reports))
	}
}

// TestClassCachedSetIsNotAliased checks that elements built from one
// cached class string do not share the parsed refinements.
func TestClassCachedSetIsNotAliased(t *testing.T) {
	classTestNoErrors(t)
	const classes = "shadow-md hover:shadow-lg"
	a := Div().Class(classes)
	if base, ok := a.StyleMut().BoxShadow.Get(); ok && len(base) > 0 {
		base[0].OffsetY = 99
	}
	if hover, ok := a.hoverStyle.BoxShadow.Get(); ok && len(hover) > 0 {
		hover[0].OffsetY = 99
	}
	b := Div().Class(classes)
	if got, want := *b.StyleMut(), *Div().ShadowMd().StyleMut(); !reflect.DeepEqual(got, want) {
		t.Errorf("second element base = %+v; want %+v", got, want)
	}
	if got, want := *b.hoverStyle, *Div().ShadowLg().StyleMut(); !reflect.DeepEqual(got, want) {
		t.Errorf("second element hover = %+v; want %+v", got, want)
	}
}

// TestStyleBuilderStateArgument checks that the state builders take a
// Styles() builder as well as the pinned closure, with the same result.
func TestStyleBuilderStateArgument(t *testing.T) {
	c := Rgb(0x334155)
	built := Div().Hover(Styles().Bg(c))
	closure := Div().Hover(func(s StyleRefinement) StyleRefinement {
		s.Background = Some(BackgroundOf(c))
		return s
	})
	if built.hoverStyle == nil || closure.hoverStyle == nil || !reflect.DeepEqual(*built.hoverStyle, *closure.hoverStyle) {
		t.Errorf("Hover(Styles().Bg(c)) = %+v; want the closure's %+v", built.hoverStyle, closure.hoverStyle)
	}

	d := Div().ID("b").Active(Styles().Bg(c)).Focus(Styles().TextColor(White())).GroupHover("g", Styles().Opacity50())
	if d.activeStyle == nil || d.activeStyle.Background != Some(SolidBackground(c)) {
		t.Errorf("Active(Styles()) = %+v", d.activeStyle)
	}
	if d.focusStyle == nil || d.focusStyle.Text.Color != Some(White()) {
		t.Errorf("Focus(Styles()) = %+v", d.focusStyle)
	}
	if gs := d.groupHoverStyle; gs == nil || gs.Group != "g" || gs.Style.Opacity != Some(float32(0.5)) {
		t.Errorf("GroupHover(Styles()) = %+v", gs)
	}

	// The state style is a copy: changing the builder afterwards does
	// not reach the element.
	b := Styles().ShadowMd()
	d = Div().Hover(b)
	b.Bg(c)
	if d.hoverStyle.Background.IsSome() {
		t.Error("the hover style aliases its builder")
	}
	r := b.Refinement()
	if shadows, ok := r.BoxShadow.Get(); ok && len(shadows) > 0 {
		shadows[0].OffsetY = 99
	}
	if shadows, _ := b.StyleMut().BoxShadow.Get(); len(shadows) == 0 || shadows[0].OffsetY == 99 {
		t.Error("Refinement aliases the builder's shadows")
	}

	classTestWantPanic(t, "non-nil style", func() { Div().Hover((*StyleBuilder)(nil)) })
}

// TestColorHelpers checks Rgb, Hex, ParseHex, Opacity and Alpha.
func TestColorHelpers(t *testing.T) {
	for _, tc := range []struct {
		name      string
		got, want Hsla
	}{
		{"Rgb(0x1e293b)", Rgb(0x1e293b), RgbaToHsla(0x1e293bff)},
		{"Rgb(0xffffff)", Rgb(0xffffff), White()},
		{"Rgb(0)", Rgb(0), Black()},
		{"Rgb(0xff0000)", Rgb(0xff0000), Red()},
	} {
		if tc.got != tc.want {
			t.Errorf("%s = %+v, want %+v", tc.name, tc.got, tc.want)
		}
	}

	for _, tc := range []struct {
		css  string
		want Hsla
	}{
		{"#abc", Rgb(0xaabbcc)},
		{"abc", Rgb(0xaabbcc)},
		{"#abcd", RgbaToHsla(0xaabbccdd)},
		{"#1e293b", Rgb(0x1e293b)},
		{"1E293B", Rgb(0x1e293b)},
		{"#1e293b80", RgbaToHsla(0x1e293b80)},
		{"#fff", White()},
	} {
		got, err := ParseHex(tc.css)
		if err != nil || got != tc.want {
			t.Errorf("ParseHex(%q) = %+v, %v; want %+v", tc.css, got, err, tc.want)
		}
		if got := Hex(tc.css); got != tc.want {
			t.Errorf("Hex(%q) = %+v, want %+v", tc.css, got, tc.want)
		}
	}
	for _, css := range []string{"#12345", "#ggg", "", "0x1234", "#", "#1234567", "#123456789"} {
		if got, err := ParseHex(css); err == nil {
			t.Errorf("ParseHex(%q) = %+v, nil; want an error", css, got)
		}
		classTestWantPanic(t, "not a CSS hex color", func() { Hex(css) })
	}

	c := Rgb(0x1e293b)
	same := func(name string, got Hsla, wantA float32) {
		t.Helper()
		if got.H != c.H || got.S != c.S || got.L != c.L || got.A != wantA {
			t.Errorf("%s = %+v; want %+v with alpha %v", name, got, c, wantA)
		}
	}
	same("Opacity(0.5)", c.Opacity(0.5), 0.5)
	same("Alpha(0.5).Opacity(0.5)", c.Alpha(0.5).Opacity(0.5), 0.25)
	same("Alpha(0.5).Opacity(2)", c.Alpha(0.5).Opacity(2), 0.5)
	same("Opacity(-1)", c.Opacity(-1), 0)
	same("Alpha(0.3)", c.Alpha(0.3), 0.3)
	same("Alpha(0.5).Alpha(0.8)", c.Alpha(0.5).Alpha(0.8), 0.8)
	same("Alpha(1.5)", c.Alpha(1.5), 1)
	same("Alpha(-0.2)", c.Alpha(-0.2), 0)
}

// TestTailwindColor checks the run-time palette lookup.
func TestTailwindColor(t *testing.T) {
	for _, tc := range []struct {
		name string
		want Hsla
		ok   bool
	}{
		{"slate-800", Rgb(0x1e293b), true},
		{"rose-950", Rgb(0x4c0519), true},
		{"white", White(), true},
		{"black", Black(), true},
		{"transparent", TransparentBlack(), true},
		{"slate-850", Hsla{}, false},
		{"brand", Hsla{}, false},
		{"", Hsla{}, false},
	} {
		got, ok := TailwindColor(tc.name)
		if ok != tc.ok || got != tc.want {
			t.Errorf("TailwindColor(%q) = %+v, %v; want %+v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
	if c, _ := TailwindColor("transparent"); c.A != 0 {
		t.Errorf("transparent alpha = %v", c.A)
	}
}
