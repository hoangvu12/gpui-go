package base

// Styled extensions (U:styled.rs): RoleOverride, the h_flex/v_flex
// constructors, box_shadow and the StyledExt trait.
//
// Go adaptation: StyledExt is a blanket trait over every gpui Styled
// element (U:styled.rs:205 impl<E: Styled> StyledExt for E). Go cannot add
// methods to gpui's types, so StyledExtOf(element) wraps any element that
// embeds gpui.Styled (DivElement, CanvasElement, StyleBuilder, components
// embedding gpui.Styled or gpui.Component) and its methods return the
// element: StyledExtOf(gpui.Div()).HFlex().SizeFull() is
// div().h_flex().size_full().
//
// The debug_* helpers are gated on cfg!(debug_assertions) upstream; Go
// has no build profile, so they always apply (the core made Styled.Debug
// unconditional the same way, gpui/styled.go Debug). The
// styled_ext_reflection_methods inspector reflection (U:styled.rs:74-77,
// 207-211) is not ported: the core inspector has no reflection registry.

import "github.com/hoangvu12/gpui-go/gpui"

// RoleOverride overrides a component's implicit accessibility role
// (U:styled.rs:6-32).
type RoleOverride struct {
	kind roleOverrideKind
	role gpui.Role
}

type roleOverrideKind uint8

const (
	roleOverrideImplicit roleOverrideKind = iota
	roleOverridePresentational
	roleOverrideRole
)

// RoleOverrideImplicit keeps the component's default role (the default
// variant).
func RoleOverrideImplicit() RoleOverride { return RoleOverride{} }

// RoleOverridePresentational removes the role.
func RoleOverridePresentational() RoleOverride {
	return RoleOverride{kind: roleOverridePresentational}
}

// RoleOverrideRole replaces the role (also From<Role>, U:styled.rs:23-27).
func RoleOverrideRole(role gpui.Role) RoleOverride {
	return RoleOverride{kind: roleOverrideRole, role: role}
}

// RoleOverrideFromOption is From<Option<Role>>: Some is the role, None is
// presentational (U:styled.rs:28-32).
func RoleOverrideFromOption(role gpui.Role, ok bool) RoleOverride {
	if !ok {
		return RoleOverridePresentational()
	}
	return RoleOverrideRole(role)
}

// Resolve returns the effective role: the default for Implicit, none for
// Presentational, the override for Role (U:styled.rs:14-21).
func (o RoleOverride) Resolve(defaultRole func() gpui.Role) (gpui.Role, bool) {
	switch o.kind {
	case roleOverridePresentational:
		return 0, false
	case roleOverrideRole:
		return o.role, true
	default:
		return defaultRole(), true
	}
}

// IsImplicit, IsPresentational and Role report the variant.
func (o RoleOverride) IsImplicit() bool { return o.kind == roleOverrideImplicit }

// IsPresentational reports the Presentational variant.
func (o RoleOverride) IsPresentational() bool { return o.kind == roleOverridePresentational }

// Role returns the Role variant's role.
func (o RoleOverride) Role() (gpui.Role, bool) { return o.role, o.kind == roleOverrideRole }

// HFlex returns a row that centers its children on the cross axis
// (U:styled.rs:34-40 h_flex). See StyledExt.HFlex for the cross-axis rule,
// which is not symmetric with VFlex.
func HFlex() *gpui.DivElement { return StyledExtOf(gpui.Div().Flex()).HFlex() }

// VFlex returns a column whose children stretch across the cross axis
// (U:styled.rs:42-48 v_flex).
func VFlex() *gpui.DivElement { return StyledExtOf(gpui.Div().Flex()).VFlex() }

// BoxShadow builds an outset box shadow (U:styled.rs:50-64 box_shadow).
func BoxShadow(x, y, blur, spread float32, color gpui.Hsla) gpui.BoxShadow {
	return gpui.BoxShadow{
		OffsetX:      x,
		OffsetY:      y,
		BlurRadius:   blur,
		SpreadRadius: spread,
		Inset:        false,
		Color:        gpui.SolidBackground(color),
	}
}

// StyledElement is any element carrying the core Styled vocabulary: it
// embeds gpui.Styled[S] (directly or through gpui.Component).
type StyledElement[S any] interface {
	StyleBase() *gpui.Styled[S]
}

// StyledExt is the StyledExt trait (U:styled.rs:78-203) over one element.
type StyledExt[S StyledElement[S]] struct {
	element S
}

// StyledExtOf wraps a styled element for the StyledExt methods.
func StyledExtOf[S StyledElement[S]](element S) StyledExt[S] {
	return StyledExt[S]{element: element}
}

// styled returns the element's bound Styled helper.
func (e StyledExt[S]) styled() *gpui.Styled[S] { return e.element.StyleBase() }

// RefineStyle refines the element's style with the present fields of
// style (U:styled.rs:79-82 refine_style).
func (e StyledExt[S]) RefineStyle(style *gpui.StyleRefinement) S {
	e.styled().StyleMut().Refine(style)
	return e.element
}

// HFlex lays children out in a row, centered on the cross axis
// (U:styled.rs:84-107 h_flex: flex().flex_row().items_center()).
//
// The centering is the desktop default for a row of controls, but it is
// not the mirror image of VFlex, which leaves the cross axis stretching:
// a column placed in a row takes its content's height and is centered,
// so content taller than the row overflows equally above and below. Give
// a full-height column HFull (or the row ItemsStart/ItemsStretch).
func (e StyledExt[S]) HFlex() S {
	s := e.styled()
	s.Flex()
	s.FlexRow()
	return s.ItemsCenter()
}

// VFlex lays children out in a column, stretching them across the cross
// axis (U:styled.rs:109-115 v_flex: flex().flex_col()).
func (e StyledExt[S]) VFlex() S {
	s := e.styled()
	s.Flex()
	return s.FlexCol()
}

// Paddings sets the four paddings (U:styled.rs:117-126).
func (e StyledExt[S]) Paddings[L gpui.IntoDefiniteLength](paddings Edges[L]) S {
	s := e.styled()
	s.Pt(paddings.Top)
	s.Pb(paddings.Bottom)
	s.Pl(paddings.Left)
	return s.Pr(paddings.Right)
}

// Margins sets the four margins (U:styled.rs:128-137).
func (e StyledExt[S]) Margins[L gpui.IntoDefiniteLength](margins Edges[L]) S {
	s := e.styled()
	s.Mt(gpui.DefiniteLengthFrom(margins.Top))
	s.Mb(gpui.DefiniteLengthFrom(margins.Bottom))
	s.Ml(gpui.DefiniteLengthFrom(margins.Left))
	return s.Mr(gpui.DefiniteLengthFrom(margins.Right))
}

// debugBorder draws a 1px border in color (the shared debug_* body).
func (e StyledExt[S]) debugBorder(color gpui.Hsla) S {
	s := e.styled()
	s.Border1()
	return s.BorderColor(color)
}

// DebugRed draws a red debug border (U:styled.rs:139-145).
func (e StyledExt[S]) DebugRed() S { return e.debugBorder(hsl(0, 72.2, 50.6)) }

// DebugBlue draws a blue debug border (U:styled.rs:147-153).
func (e StyledExt[S]) DebugBlue() S { return e.debugBorder(hsl(217.2, 91.2, 59.8)) }

// DebugYellow draws a yellow debug border (U:styled.rs:155-161).
func (e StyledExt[S]) DebugYellow() S { return e.debugBorder(hsl(47.9, 95.8, 53.1)) }

// DebugGreen draws a green debug border (U:styled.rs:163-169).
func (e StyledExt[S]) DebugGreen() S { return e.debugBorder(hsl(142.1, 70.6, 45.3)) }

// DebugPink draws a pink debug border (U:styled.rs:171-177).
func (e StyledExt[S]) DebugPink() S { return e.debugBorder(hsl(330.4, 81.2, 60.4)) }

// DebugFocused draws the blue debug border while focus contains the
// focused element (U:styled.rs:179-185).
func (e StyledExt[S]) DebugFocused(focus gpui.FocusHandle, w *gpui.Window, _ *gpui.App) S {
	if focus.ContainsFocused(w) {
		return e.DebugBlue()
	}
	return e.element
}

// fontWeight sets the font weight (the font_weight! macro body,
// U:styled.rs:66-72; weights from R:text_system.rs:420-436).
func (e StyledExt[S]) fontWeight(weight float32) S { return e.styled().FontWeight(weight) }

// FontThin sets FontWeight::THIN (U:styled.rs:187).
func (e StyledExt[S]) FontThin() S { return e.fontWeight(100) }

// FontExtralight sets FontWeight::EXTRA_LIGHT (U:styled.rs:188).
func (e StyledExt[S]) FontExtralight() S { return e.fontWeight(200) }

// FontLight sets FontWeight::LIGHT (U:styled.rs:189).
func (e StyledExt[S]) FontLight() S { return e.fontWeight(300) }

// FontNormal sets FontWeight::NORMAL (U:styled.rs:190).
func (e StyledExt[S]) FontNormal() S { return e.fontWeight(400) }

// FontMedium sets FontWeight::MEDIUM (U:styled.rs:191).
func (e StyledExt[S]) FontMedium() S { return e.fontWeight(500) }

// FontSemibold sets FontWeight::SEMIBOLD (U:styled.rs:192).
func (e StyledExt[S]) FontSemibold() S { return e.fontWeight(600) }

// FontBold sets FontWeight::BOLD (U:styled.rs:193).
func (e StyledExt[S]) FontBold() S { return e.fontWeight(700) }

// FontExtrabold sets FontWeight::EXTRA_BOLD (U:styled.rs:194).
func (e StyledExt[S]) FontExtrabold() S { return e.fontWeight(800) }

// FontBlack sets FontWeight::BLACK (U:styled.rs:195).
func (e StyledExt[S]) FontBlack() S { return e.fontWeight(900) }

// CornerRadii sets the four corner radii in logical pixels
// (U:styled.rs:197-202).
func (e StyledExt[S]) CornerRadii(radius gpui.Corners) S {
	s := e.styled()
	s.RoundedTl(gpui.Px(radius.TopLeft))
	s.RoundedTr(gpui.Px(radius.TopRight))
	s.RoundedBl(gpui.Px(radius.BottomLeft))
	return s.RoundedBr(gpui.Px(radius.BottomRight))
}

// hsl converts CSS-style degrees and percentages (U:styled.rs:213-215).
func hsl(hue, saturation, lightness float32) gpui.Hsla {
	return hsla(hue/360, saturation/100, lightness/100, 1)
}
