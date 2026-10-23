package main

import (
	"strconv"

	"github.com/hoangvu12/gpui-go/base"
	"github.com/hoangvu12/gpui-go/gpui"
)

// palette is the gallery's styling, derived from the Base theme's colour
// tokens so the light/dark switch restyles every page.
type palette struct {
	dark      bool
	canvas    gpui.Hsla
	surface   gpui.Hsla
	elevated  gpui.Hsla
	fg        gpui.Hsla
	muted     gpui.Hsla
	border    gpui.Hsla
	hover     gpui.Hsla
	selected  gpui.Hsla
	primary   gpui.Hsla
	primaryFg gpui.Hsla
	accent    gpui.Hsla
	danger    gpui.Hsla
	dangerFg  gpui.Hsla
	selection gpui.Hsla
	series    []gpui.Hsla
}

// pal is the palette of the frame being drawn. The gallery's Render
// refreshes it from the theme before any page builds its elements; every
// read happens on the UI thread.
var pal = paletteFrom(base.DefaultTheme())

func paletteFrom(theme base.Theme) palette {
	c := theme.Tokens.Colors
	dark := theme.Appearance == base.ThemeAppearanceDark
	p := palette{
		dark:      dark,
		canvas:    c.Background,
		surface:   c.Surface,
		elevated:  c.Muted,
		fg:        c.Foreground,
		muted:     c.MutedForeground,
		border:    c.Border,
		hover:     c.Accent,
		selected:  c.Secondary,
		primary:   c.Primary,
		primaryFg: c.PrimaryForeground,
		danger:    c.Destructive,
		dangerFg:  c.DestructiveForeground,
		selection: c.Selection,
	}
	if dark {
		p.surface = gpui.Rgb(0x111111)
		p.accent = gpui.Rgb(0x60a5fa)
		p.dangerFg = gpui.Rgb(0xfafafa)
		p.series = []gpui.Hsla{gpui.Rgb(0x60a5fa), gpui.Rgb(0x34d399), gpui.Rgb(0xfbbf24), gpui.Rgb(0xf472b6), gpui.Rgb(0xa78bfa)}
	} else {
		p.accent = gpui.Rgb(0x2563eb)
		p.series = []gpui.Hsla{gpui.Rgb(0x2563eb), gpui.Rgb(0x059669), gpui.Rgb(0xd97706), gpui.Rgb(0xdb2777), gpui.Rgb(0x7c3aed)}
	}
	return p
}

func itoa(n int) string { return strconv.Itoa(n) }

// demo frames one state of a component: a caption over a bordered box.
func demo(label string, content any) *gpui.DivElement {
	return gpui.Div().Flex().FlexCol().Gap2().
		Child(gpui.Div().TextXs().FontWeight(600).TextColor(pal.muted).Child(label)).
		Child(gpui.Div().P4().
			Bg(pal.surface).
			Border1().BorderColor(pal.border).RoundedLg().
			Child(content))
}

// row lays children out horizontally, wrapping.
func row(children ...any) *gpui.DivElement {
	return gpui.Div().Flex().FlexWrap().ItemsCenter().Gap3().Children(children...)
}

// col lays children out vertically.
func col(children ...any) *gpui.DivElement {
	return gpui.Div().Flex().FlexCol().Gap3().Children(children...)
}

// note is a muted line of explanatory text.
func note(text string) *gpui.DivElement {
	return gpui.Div().TextXs().TextColor(pal.muted).Child(text)
}

// Button variants and sizes.
type variant uint8

const (
	outline variant = iota
	primary
	ghost
	destructive
)

type size uint8

const (
	sm size = iota
	md
	lg
)

// btn styles a base.Button.
func btn[I gpui.IntoElementID](id I, label any, v variant, s size) *base.Button {
	// Base sets no cursor on its controls; the skin adds the pointer, as
	// the styled component crate does upstream.
	b := base.NewButton(id).
		CursorPointer().
		Flex().ItemsCenter().JustifyCenter().Gap2().
		LineHeight(gpui.Fraction(1)).
		Border1().RoundedMd().
		Styles(func(st base.ButtonStyles) base.ButtonStyles {
			return st.Disabled(func(p *base.StateStyle) *base.StateStyle { return p.Opacity(0.45).CursorDefault() })
		})
	switch s {
	case sm:
		b = b.H6().Px2().TextXs()
	case md:
		b = b.H8().Px3().TextSm()
	case lg:
		b = b.H10().Px5().TextBase()
	}
	switch v {
	case primary:
		b = b.BorderColor(pal.primary).Bg(pal.primary).TextColor(pal.primaryFg).
			Hover(gpui.Styles().Opacity(0.85))
	case ghost:
		b = b.BorderColor(gpui.TransparentBlack()).Hover(gpui.Styles().Bg(pal.hover))
	case destructive:
		b = b.BorderColor(pal.danger).Bg(pal.danger).TextColor(pal.dangerFg).
			Hover(gpui.Styles().Opacity(0.85))
	default:
		b = b.BorderColor(pal.border).Bg(pal.surface).Hover(gpui.Styles().Bg(pal.hover))
	}
	return b.Child(label)
}

// themedSwitch is a base.Switch with a sliding thumb.
func themedSwitch[I gpui.IntoElementID](id I, checked, disabled bool, onChange func(next bool, app *gpui.App)) *base.Switch {
	sw := base.NewSwitch(id).
		Checked(checked).
		Disabled(disabled).
		CursorPointer().FlexNone().W(gpui.Px(36)).H5().P(gpui.Px(2)).RoundedFull().Flex().
		Bg(pal.border).
		Styles(func(st base.SwitchStyles) base.SwitchStyles {
			return st.
				Checked(func(p *base.StateStyle) *base.StateStyle { return p.Bg(pal.primary).JustifyEnd() }).
				Disabled(func(p *base.StateStyle) *base.StateStyle { return p.Opacity(0.45).CursorDefault() })
		}).
		Child(gpui.Div().Size4().RoundedFull().Bg(pal.primaryFg))
	if onChange != nil {
		sw = sw.OnChange(func(next bool, _ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) { onChange(next, app) })
	}
	return sw
}

// inputBox frames an input-family element: Base's InputBase carries the
// border and focus style, and a mouse down focuses the state.
func inputBox(id string, width float32, height func(*base.InputBase) *base.InputBase, focus func(*gpui.Window, *gpui.App), child any) *base.InputBase {
	b := base.NewInputBase(id).
		DebugSelector(id).
		W(gpui.Px(width)).Px2().
		Border1().BorderColor(pal.border).RoundedMd().Bg(pal.surface).
		Styles(func(st base.InputStyles) base.InputStyles {
			return st.
				Focused(func(p *base.StateStyle) *base.StateStyle { return p.BorderColor(pal.fg) }).
				Disabled(func(p *base.StateStyle) *base.StateStyle { return p.Opacity(0.5) })
		}).
		OnMouseDown(gpui.MouseButtonLeft, func(_ *gpui.MouseDownEvent, w *gpui.Window, app *gpui.App) { focus(w, app) })
	return height(b).Child(child)
}

// labelled stacks a small label over a control.
func labelled(label string, control any) *gpui.DivElement {
	return gpui.Div().Flex().FlexCol().ItemsStart().Gap1().
		Child(gpui.Div().TextXs().TextColor(pal.muted).Child(label)).
		Child(control)
}

// focusInput, focusTextarea and focusEditor focus an input-family state.
func focusInput(state gpui.Entity[base.InputState]) func(*gpui.Window, *gpui.App) {
	return func(w *gpui.Window, app *gpui.App) {
		state.Update(app, func(st *base.InputState, cx *gpui.Context[base.InputState]) { st.Focus(w, cx) })
	}
}

func focusTextarea(state gpui.Entity[base.TextareaState]) func(*gpui.Window, *gpui.App) {
	return func(w *gpui.Window, app *gpui.App) {
		state.Update(app, func(st *base.TextareaState, cx *gpui.Context[base.TextareaState]) { st.Focus(w, cx) })
	}
}

func focusEditor(state gpui.Entity[base.EditorState]) func(*gpui.Window, *gpui.App) {
	return func(w *gpui.Window, app *gpui.App) {
		state.Update(app, func(st *base.EditorState, cx *gpui.Context[base.EditorState]) { st.Focus(w, cx) })
	}
}

// direction is where a chevron points.
type direction uint8

const (
	down direction = iota
	up
	right
)

// chevron is a disclosure arrow drawn as a path in color: the text
// arrows (▾ ▸) are missing from the default UI font.
func chevron(dir direction, color gpui.Hsla) *gpui.CanvasElement[struct{}] {
	return gpui.Canvas(
		func(gpui.Bounds, *gpui.Window, *gpui.App) struct{} { return struct{}{} },
		func(bounds gpui.Bounds, _ struct{}, w *gpui.Window, _ *gpui.App) {
			cx := bounds.Origin.X + bounds.Size.Width/2
			cy := bounds.Origin.Y + bounds.Size.Height/2
			const half, depth = 3.5, 1.75
			b := gpui.StrokePathBuilder(1.4)
			switch dir {
			case up:
				b.MoveTo(cx-half, cy+depth).LineTo(cx, cy-depth).LineTo(cx+half, cy+depth)
			case right:
				b.MoveTo(cx-depth, cy-half).LineTo(cx+depth, cy).LineTo(cx-depth, cy+half)
			default:
				b.MoveTo(cx-half, cy-depth).LineTo(cx, cy+depth).LineTo(cx+half, cy-depth)
			}
			if path, err := b.Build(); err == nil {
				w.PaintPath(path, gpui.SolidBackground(color))
			}
		}).Size(gpui.Px(12)).FlexNone()
}

// searchIcon is a magnifier drawn as paths (the text glyph is missing
// from the default UI font).
func searchIcon(color gpui.Hsla) *gpui.CanvasElement[struct{}] {
	return gpui.Canvas(
		func(gpui.Bounds, *gpui.Window, *gpui.App) struct{} { return struct{}{} },
		func(bounds gpui.Bounds, _ struct{}, w *gpui.Window, _ *gpui.App) {
			cx := bounds.Origin.X + bounds.Size.Width/2 - 1
			cy := bounds.Origin.Y + bounds.Size.Height/2 - 1
			const r = 4
			b := gpui.StrokePathBuilder(1.4)
			b.MoveTo(cx-r, cy)
			b.ArcTo(r, r, 0, false, true, cx+r, cy)
			b.ArcTo(r, r, 0, false, true, cx-r, cy)
			b.MoveTo(cx+r*0.7, cy+r*0.7).LineTo(cx+r*1.6, cy+r*1.6)
			if path, err := b.Build(); err == nil {
				w.PaintPath(path, gpui.SolidBackground(color))
			}
		}).Size(gpui.Px(14)).FlexNone()
}

// openChevron points up while a popup is open.
func openChevron(open bool) any {
	if open {
		return chevron(up, pal.muted)
	}
	return chevron(down, pal.muted)
}

// folderChevron points down while a folder is expanded.
func folderChevron(expanded bool) any {
	if expanded {
		return chevron(down, pal.muted)
	}
	return chevron(right, pal.muted)
}

// missing is the note a page shows for a variant the public API cannot
// build.
func missing(text string) *gpui.DivElement {
	return gpui.Div().P3().TextXs().
		Border1().BorderColor(pal.danger).RoundedMd().TextColor(pal.danger).
		Child("Not shown: " + text)
}
