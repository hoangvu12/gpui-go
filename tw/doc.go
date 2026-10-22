// Package tw is the Tailwind CSS v3.4 default color palette as gpui
// colors.
//
// Class names map 1:1 to values: bg-slate-800 is Bg(tw.Slate800) and
// text-white is TextColor(tw.White). The opacity modifier is an alpha,
// so bg-black/50 is Bg(tw.Black.Alpha(0.5)) (Hsla.Alpha sets the alpha;
// Hsla.Opacity multiplies it). tw.Transparent is bg-transparent's fully
// transparent black.
//
//	gpui.Div().
//		Bg(tw.Slate800).
//		TextColor(tw.White).
//		BorderColor(tw.Black.Alpha(0.5)).
//		Hover(gpui.Styles().Bg(tw.Slate700))
//
// For a color name known only at run time, use gpui.TailwindColor:
//
//	c, ok := gpui.TailwindColor("slate-800")
//
// The palette values are Tailwind CSS's (MIT, Copyright (c) Tailwind
// Labs, Inc.); see THIRD_PARTY_NOTICES.md.
package tw
