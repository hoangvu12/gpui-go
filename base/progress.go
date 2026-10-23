package base

// An unstyled linear progress root with controlled value accessibility,
// plus its track and indicator parts (U:progress.rs).
//
// Display (map gap 2b): Progress, ProgressTrack and ProgressIndicator are
// plain divs upstream (U:progress.rs:24, 100), display Block under
// crates.io gpui 0.2.2 (the pin
// defaults to Flex, R:style.rs:1024-1027), so each such base is built
// with Block() before the caller's style refines over it: the upstream
// default layout holds and a caller's Flex() still wins.

import "github.com/hoangvu12/gpui-go/gpui"

// Progress is an unstyled linear progress root (U:progress.rs:10-19).
type Progress struct {
	gpui.Component[*Progress]

	base               *gpui.DivElement
	value              float32
	indeterminate      bool
	accessibilityLabel *string
}

// NewProgress creates a progress root with the given element id
// (U:progress.rs:22-31).
func NewProgress[I gpui.IntoElementID](id I) *Progress {
	base := gpui.Div().Block().ID(id)
	return gpui.BindComponent(&Progress{base: base}, base)
}

// Value sets the controlled percentage value, clamped to 0..=100
// (U:progress.rs:33-37).
func (p *Progress) Value(value float32) *Progress {
	p.value = clamp32(value, 0, 100)
	return p
}

// Indeterminate sets whether the progress has no known value
// (U:progress.rs:39-42).
func (p *Progress) Indeterminate(indeterminate bool) *Progress {
	p.indeterminate = indeterminate
	return p
}

// AccessibilityLabel sets the accessible name (U:progress.rs:44-47).
func (p *Progress) AccessibilityLabel(label string) *Progress {
	p.accessibilityLabel = &label
	return p
}

// RenderOnce implements gpui.RenderOnce (U:progress.rs:70-85).
func (p *Progress) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return p.render(w, app).IntoElement()
}

func (p *Progress) render(_ *gpui.Window, _ *gpui.App) *gpui.DivElement {
	d := p.base.Role(gpui.RoleProgressIndicator)
	if p.accessibilityLabel != nil {
		d.AriaLabel(*p.accessibilityLabel)
	}
	d.AriaMinNumericValue(0).AriaMaxNumericValue(100)
	if !p.indeterminate {
		d.AriaNumericValue(float64(p.value))
	}
	d.Extend(p.TakeChildren())
	style := p.Style()
	return StyledExtOf(d).RefineStyle(&style)
}

// ProgressTrack is an unstyled progress track (U:progress.rs:127).
type ProgressTrack struct {
	gpui.Styled[*ProgressTrack]
	gpui.Parent[*ProgressTrack]

	base *gpui.DivElement
}

// NewProgressTrack creates a track (U:progress.rs:97-105).
func NewProgressTrack() *ProgressTrack {
	return gpui.BindParent(gpui.BindStyled(&ProgressTrack{base: gpui.Div().Block()}))
}

// RenderOnce implements gpui.RenderOnce (U:progress.rs:119-123).
func (t *ProgressTrack) RenderOnce(_ *gpui.Window, _ *gpui.App) gpui.AnyElement {
	style := t.Style()
	return StyledExtOf(t.base.Extend(t.TakeChildren())).RefineStyle(&style).IntoElement()
}

// ProgressIndicator is an unstyled progress indicator
// (U:progress.rs:128).
type ProgressIndicator struct {
	gpui.Styled[*ProgressIndicator]
	gpui.Parent[*ProgressIndicator]

	base *gpui.DivElement
}

// NewProgressIndicator creates an indicator (U:progress.rs:97-105).
func NewProgressIndicator() *ProgressIndicator {
	return gpui.BindParent(gpui.BindStyled(&ProgressIndicator{base: gpui.Div().Block()}))
}

// RenderOnce implements gpui.RenderOnce (U:progress.rs:119-123).
func (i *ProgressIndicator) RenderOnce(_ *gpui.Window, _ *gpui.App) gpui.AnyElement {
	style := i.Style()
	return StyledExtOf(i.base.Extend(i.TakeChildren())).RefineStyle(&style).IntoElement()
}
