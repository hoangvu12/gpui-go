package base

// Unstyled avatar slots (U:avatar.rs): the Avatar root renders its image
// slot, else its fallback slot.
//
// Deviation: upstream AvatarImage is an InteractiveElement over
// gpui::Img (U:avatar.rs:89-93, R:elements/img.rs Img implements
// InteractiveElement). The core's ImgElement (gpui/img.go) carries the
// Styled vocabulary only, no interactivity, so AvatarImage is Styled
// only; interactive avatars put listeners on the Avatar root.
//
// Display (map gap 2b): the Avatar root and AvatarFallback are plain divs
// upstream (U:avatar.rs:21, 112), display Block under crates.io gpui 0.2.2
// (the pin
// defaults to Flex, R:style.rs:1024-1027), so each such base is built
// with Block() before the caller's style refines over it: the upstream
// default layout holds and a caller's Flex() still wins.

import "github.com/hoangvu12/gpui-go/gpui"

// Avatar is an unstyled avatar root that renders its image slot or
// fallback slot (U:avatar.rs:9-16).
type Avatar struct {
	gpui.Styled[*Avatar]
	gpui.Interactive[*Avatar]

	base     *gpui.DivElement
	image    *AvatarImage
	fallback *AvatarFallback
}

// NewAvatar creates an empty avatar (U:avatar.rs:19-26).
func NewAvatar() *Avatar {
	base := gpui.Div().Block()
	a := gpui.BindStyled(&Avatar{base: base})
	return gpui.BindInteractive(a, base)
}

// DefaultAvatar is Default for Avatar (U:avatar.rs:39-43).
func DefaultAvatar() *Avatar { return NewAvatar() }

// Image sets the image slot (U:avatar.rs:28-31).
func (a *Avatar) Image(image *AvatarImage) *Avatar {
	a.image = image
	return a
}

// Fallback sets the fallback slot (U:avatar.rs:33-36).
func (a *Avatar) Fallback(fallback *AvatarFallback) *Avatar {
	a.fallback = fallback
	return a
}

// RenderOnce implements gpui.RenderOnce (U:avatar.rs:57-65): the image
// slot when present, otherwise the fallback slot.
func (a *Avatar) RenderOnce(w *gpui.Window, app *gpui.App) gpui.AnyElement {
	return a.render(w, app).IntoElement()
}

func (a *Avatar) render(_ *gpui.Window, _ *gpui.App) *gpui.DivElement {
	d := a.base
	switch {
	case a.image != nil:
		d.Child(a.image)
	case a.fallback != nil:
		d.Child(a.fallback)
	}
	style := a.Style()
	return StyledExtOf(d).RefineStyle(&style)
}

// AvatarImage is an unstyled avatar image slot (U:avatar.rs:67-72).
type AvatarImage struct {
	gpui.Styled[*AvatarImage]

	image *gpui.ImgElement
}

// NewAvatarImage creates an image slot for source, any value gpui.Img
// accepts (U:avatar.rs:74-81 `impl Into<ImageSource>`; R:elements/img.rs
// img).
func NewAvatarImage(source any) *AvatarImage {
	return gpui.BindStyled(&AvatarImage{image: gpui.Img(source).Flex()})
}

// RenderOnce implements gpui.RenderOnce (U:avatar.rs:95-99).
func (i *AvatarImage) RenderOnce(_ *gpui.Window, _ *gpui.App) gpui.AnyElement {
	style := i.Style()
	return StyledExtOf(i.image).RefineStyle(&style).IntoElement()
}

// AvatarFallback is an unstyled avatar fallback slot for initials or an
// application-owned icon (U:avatar.rs:101-107).
type AvatarFallback struct {
	gpui.Styled[*AvatarFallback]
	gpui.Parent[*AvatarFallback]

	base *gpui.DivElement
}

// NewAvatarFallback creates an empty fallback slot (U:avatar.rs:109-117).
func NewAvatarFallback() *AvatarFallback {
	f := gpui.BindStyled(&AvatarFallback{base: gpui.Div().Block()})
	return gpui.BindParent(f)
}

// DefaultAvatarFallback is Default for AvatarFallback
// (U:avatar.rs:119-123).
func DefaultAvatarFallback() *AvatarFallback { return NewAvatarFallback() }

// RenderOnce implements gpui.RenderOnce (U:avatar.rs:137-141).
func (f *AvatarFallback) RenderOnce(_ *gpui.Window, _ *gpui.App) gpui.AnyElement {
	style := f.Style()
	d := f.base.Extend(f.TakeChildren())
	return StyledExtOf(d).RefineStyle(&style).IntoElement()
}
