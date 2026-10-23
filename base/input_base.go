package base

// Port of U:input/base/mod.rs: InputContextMenuCapabilities and the
// InputBase frame. InputBase follows the Button pattern (button.go): it
// embeds gpui.Component[*InputBase] for its own instance style, its own
// child list and interactivity delegated to the inner div
// (U:input/base/mod.rs:207-230), and it is a RenderOnce recipe
// (U:input/base/mod.rs:232-242). test_support is the identity
// (observe.go).

import "github.com/hoangvu12/gpui-go/gpui"

// InputContextMenuCapabilities is what the input can offer to its context
// menu, at the moment it is opened (U:input/base/mod.rs:8-33). The zero
// value is Rust's Default; the builders return a modified copy.
type InputContextMenuCapabilities struct {
	disabled       bool
	readonly       bool
	codeEditor     bool
	selection      bool
	masked         bool
	goToDefinition bool
	codeActions    bool
}

// NewInputContextMenuCapabilities is U:input/base/mod.rs:36-38.
func NewInputContextMenuCapabilities() InputContextMenuCapabilities {
	return InputContextMenuCapabilities{}
}

// Disabled is U:input/base/mod.rs:40-43.
func (c InputContextMenuCapabilities) Disabled(disabled bool) InputContextMenuCapabilities {
	c.disabled = disabled
	return c
}

// Readonly is U:input/base/mod.rs:45-48.
func (c InputContextMenuCapabilities) Readonly(readonly bool) InputContextMenuCapabilities {
	c.readonly = readonly
	return c
}

// CodeEditor is U:input/base/mod.rs:50-53.
func (c InputContextMenuCapabilities) CodeEditor(codeEditor bool) InputContextMenuCapabilities {
	c.codeEditor = codeEditor
	return c
}

// Selection sets whether the input has a non-empty selection
// (U:input/base/mod.rs:55-59).
func (c InputContextMenuCapabilities) Selection(selection bool) InputContextMenuCapabilities {
	c.selection = selection
	return c
}

// Masked sets whether the input renders its value masked
// (U:input/base/mod.rs:61-65).
func (c InputContextMenuCapabilities) Masked(masked bool) InputContextMenuCapabilities {
	c.masked = masked
	return c
}

// GoToDefinition is U:input/base/mod.rs:67-70.
func (c InputContextMenuCapabilities) GoToDefinition(goToDefinition bool) InputContextMenuCapabilities {
	c.goToDefinition = goToDefinition
	return c
}

// CodeActions is U:input/base/mod.rs:72-75.
func (c InputContextMenuCapabilities) CodeActions(codeActions bool) InputContextMenuCapabilities {
	c.codeActions = codeActions
	return c
}

// IsDisabled is U:input/base/mod.rs:77-79.
func (c InputContextMenuCapabilities) IsDisabled() bool { return c.disabled }

// IsReadonly is U:input/base/mod.rs:81-83.
func (c InputContextMenuCapabilities) IsReadonly() bool { return c.readonly }

// IsEditable reports whether the user may change the text: the items that
// write (Cut, Paste, Code Actions) belong to it, the reading ones (Copy,
// Go to Definition) do not (U:input/base/mod.rs:85-91).
func (c InputContextMenuCapabilities) IsEditable() bool { return !c.disabled && !c.readonly }

// IsCodeEditor is U:input/base/mod.rs:93-95.
func (c InputContextMenuCapabilities) IsCodeEditor() bool { return c.codeEditor }

// HasSelection is U:input/base/mod.rs:97-99.
func (c InputContextMenuCapabilities) HasSelection() bool { return c.selection }

// IsMasked is U:input/base/mod.rs:101-103.
func (c InputContextMenuCapabilities) IsMasked() bool { return c.masked }

// IsCopyable reports whether the user may copy the text out; a masked
// input keeps its value out of the clipboard (U:input/base/mod.rs:105-111).
func (c InputContextMenuCapabilities) IsCopyable() bool { return c.selection && !c.masked }

// HasDefinition is U:input/base/mod.rs:113-115.
func (c InputContextMenuCapabilities) HasDefinition() bool { return c.goToDefinition }

// HasCodeActions is U:input/base/mod.rs:117-119.
func (c InputContextMenuCapabilities) HasCodeActions() bool { return c.codeActions }

// InputBase is the foundational input frame: it owns only input
// semantics, interaction forwarding and normal children; applications
// own all presentation (U:input/base/mod.rs:122-135).
type InputBase struct {
	gpui.Component[*InputBase]

	base           *gpui.DivElement
	semanticStyles InputStyles
	focused        bool
	disabled       bool
	role           RoleOverride
}

// NewInputBase creates a frame with the given element id
// (U:input/base/mod.rs:137-148 InputBase::new).
func NewInputBase[I gpui.IntoElementID](id I) *InputBase {
	base := TestSupport(gpui.Div().ID(gpui.ElementIDOf(id)).Block())
	return gpui.BindComponent(&InputBase{
		base: base,
		role: RoleOverrideImplicit(),
	}, base)
}

// Role overrides the accessibility role; the default is RoleTextInput
// (U:input/base/mod.rs:149-152).
func (b *InputBase) Role(role RoleOverride) *InputBase {
	b.role = role
	return b
}

// Focused is U:input/base/mod.rs:154-157.
func (b *InputBase) Focused(focused bool) *InputBase {
	b.focused = focused
	return b
}

// Disabled is U:input/base/mod.rs:159-162.
func (b *InputBase) Disabled(disabled bool) *InputBase {
	b.disabled = disabled
	return b
}

// Styles defines application-owned styles for the semantic states
// (U:input/base/mod.rs:164-167).
func (b *InputBase) Styles(build func(InputStyles) InputStyles) *InputBase {
	b.semanticStyles = build(b.semanticStyles)
	return b
}

// AccessibilityLabel sets the label exposed to accessibility clients
// (U:input/base/mod.rs:169-172).
func (b *InputBase) AccessibilityLabel(label string) *InputBase {
	b.base.AriaLabel(label)
	return b
}

// resolvedStyle layers the instance style, focused and disabled
// (U:input/base/mod.rs:174-185).
func (b *InputBase) resolvedStyle() gpui.StyleRefinement {
	instance := b.Style()
	var states []*gpui.StyleRefinement
	if b.focused {
		states = append(states, &b.semanticStyles.focused)
	}
	if b.disabled {
		states = append(states, &b.semanticStyles.disabled)
	}
	return resolveStyle(&instance, states...)
}

// InputStyles are the semantic styles supported by InputBase
// (U:input/base/mod.rs:187-191). The zero value is Rust's Default.
type InputStyles struct {
	focused  gpui.StyleRefinement
	disabled gpui.StyleRefinement
}

// Focused refines the frame style while focused (U:input/base/mod.rs:194-198).
func (s InputStyles) Focused(build func(*StateStyle) *StateStyle) InputStyles {
	s.focused = refineState(s.focused, build)
	return s
}

// Disabled refines the frame style while disabled
// (U:input/base/mod.rs:200-204).
func (s InputStyles) Disabled(build func(*StateStyle) *StateStyle) InputStyles {
	s.disabled = refineState(s.disabled, build)
	return s
}

// RenderOnce implements gpui.RenderOnce (U:input/base/mod.rs:232-242).
func (b *InputBase) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return b.render(w, app).IntoElement()
}

// render builds the frame's div (U:input/base/mod.rs:233-241).
func (b *InputBase) render(*gpui.Window, *gpui.App) *gpui.DivElement {
	style := b.resolvedStyle()
	d := b.base
	if role, ok := b.role.Resolve(func() gpui.Role { return gpui.RoleTextInput }); ok {
		d.Role(role)
	}
	d.Extend(b.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style)
}
