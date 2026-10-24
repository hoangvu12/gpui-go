# React + Tailwind → gpui-go

This guide is for people and coding agents who know HTML, React and Tailwind CSS and are writing gpui-go UI for the first time. gpui-go styles elements with Tailwind's vocabulary in two spellings: typed methods named after the classes, and `Class` strings that you can paste from web markup. Both are gpui-go additions on top of the GPUI-CE vocabulary ([ADR 0001](adr/0001-tailwind-authoring.md)).

```go
import (
	"github.com/hoangvu12/gpui-go/gpui"
	"github.com/hoangvu12/gpui-go/tw" // the Tailwind color palette
)

gpui.Div().Class("flex items-center gap-2 px-4 py-2 rounded-lg bg-slate-800 text-white hover:bg-slate-700")

// The same with typed methods:
gpui.Div().Flex().ItemsCenter().Gap2().Px4().Py2().RoundedLg().Bg(tw.Slate800).TextColor(tw.White).
	Hover(gpui.Styles().Bg(tw.Slate700))
```

Before you write UI code, read [the GPUI rules web code doesn't teach](#gpui-rules-web-code-doesnt-teach). They cover re-rendering, element IDs, event handlers and long lists, and they cause most first-time bugs.

## React concepts in gpui-go

| React / HTML | gpui-go |
|---|---|
| Component function with `useState` | A view: a struct with fields and `Render(w *gpui.Window, cx *gpui.Context[T]) gpui.AnyElement` |
| `setCount(count + 1)` | `v.count++` then `cx.Notify()` inside a `cx.Listener` |
| `<div>`, `<span>`, `<p>`, `<h2>`, `<button>` | `gpui.Div()`. Every box is a div. |
| Text node `{"Hello"}` | `.Child("Hello")`. Strings are children. |
| Children | `.Child(x)`, `.Children(a, b, c)` |
| `className="…"` | `.Class("…")` or the methods |
| `onClick={…}` | `.ID("name").OnClick(cx.Listener(…))` |
| `{cond && …}`, conditional classes | A Go `if`, or `.When(cond, func(d *gpui.DivElement) *gpui.DivElement { … })` |
| `items.map(…)` | A Go loop calling `.Child(…)` |
| A long list (react-window) | `gpui.UniformList` |
| `<img src=…>` | `gpui.Img(source)` |

## The naming rule: class → method

A class and its style method have the same name:

- Kebab case becomes CamelCase: `items-center` → `ItemsCenter()`, `flex-col` → `FlexCol()`, `rounded-tl-md` → `RoundedTlMd()`.
- A decimal point becomes `p`: `p-0.5` → `P0p5()`, `gap-1.5` → `Gap1p5()`.
- A fraction slash becomes `_`: `w-1/2` → `W1_2()`.
- A leading minus becomes `Neg` after the prefix: `-mt-2` → `MtNeg2()`, `-top-1/2` → `TopNeg1_2()`.
- A size that starts with a digit stays one lower-case word: `text-2xl` → `Text2xl()`, `shadow-2xl` → `Shadow2xl()`.
- A color is an argument: `bg-slate-800` → `Bg(tw.Slate800)`.
- A value outside the table is an argument to the base method: `w-[84px]` → `W(gpui.Px(84))`.

| Tailwind | Method |
|---|---|
| `flex`, `flex-col`, `flex-wrap` | `Flex()`, `FlexCol()`, `FlexWrap()` |
| `items-center`, `justify-between`, `self-start` | `ItemsCenter()`, `JustifyBetween()`, `SelfStart()` |
| `flex-1`, `flex-none`, `grow`, `shrink-0` | `Flex1()`, `FlexNone()`, `Grow()`, `Shrink0()` |
| `gap-2`, `gap-x-4` | `Gap2()`, `GapX4()` |
| `p-4`, `px-4`, `py-2`, `pt-1` | `P4()`, `Px4()`, `Py2()`, `Pt1()` |
| `m-auto`, `mx-auto`, `-mt-2` | `MAuto()`, `MxAuto()`, `MtNeg2()` |
| `w-64`, `w-full`, `w-1/2`, `w-px`, `w-auto` | `W64()`, `WFull()`, `W1_2()`, `WPx()`, `WAuto()` |
| `h-10`, `size-full`, `size-4` | `H10()`, `SizeFull()`, `Size4()` |
| `min-w-0`, `max-w-full` | `MinW0()`, `MaxWFull()` |
| `relative`, `absolute`, `inset-0`, `top-1/2` | `Relative()`, `Absolute()`, `Inset0()`, `Top1_2()` |
| `hidden`, `block`, `grid`, `invisible` | `Hidden()`, `Block()`, `Grid()`, `Invisible()` |
| `overflow-hidden`, `overflow-x-hidden` | `OverflowHidden()`, `OverflowXHidden()` |
| `text-sm`, `text-2xl`, `text-6xl` | `TextSm()`, `Text2xl()`, `Text6xl()` |
| `font-medium`, `font-bold` | `FontMedium()`, `FontBold()` |
| `leading-tight`, `uppercase`, `italic`, `underline` | `LeadingTight()`, `Uppercase()`, `Italic()`, `Underline()` |
| `font-sans`, `font-serif`, `font-mono` | The Windows families Chrome resolves Tailwind's default stacks to (the system UI font, Georgia, Consolas) |
| `tabular-nums` and the other numeric variants (`proportional-nums`, `lining-nums`, `oldstyle-nums`, `slashed-zero`, `ordinal`, `diagonal-fractions`, `stacked-fractions`, `normal-nums`) | The class sets the OpenType feature (`tnum`, `pnum`, `lnum`, `onum`, `zero`, `ordn`, `frac`, `afrc`; `normal-nums` clears them) |
| `text-center`, `truncate`, `whitespace-*` | `TextCenter()`, `Truncate()`, `WhitespaceNormal()` / `WhitespaceNowrap()` / `WhitespacePre()` / `WhitespacePreLine()` / `WhitespacePreWrap()` (browser-core 38) |
| `rounded-lg`, `rounded-full`, `rounded-t-md` | `RoundedLg()`, `RoundedFull()`, `RoundedTMd()` |
| `border`, `border-2`, `border-t`, `border-dashed` | `Border1()`, `Border2()`, `BorderT1()`, `BorderDashed()` |
| `shadow-md`, `ring-2` | `ShadowMd()`, `Ring2()` |
| `opacity-50` | `Opacity50()` |
| `cursor-pointer`, `cursor-not-allowed` | `CursorPointer()`, `CursorNotAllowed()` |
| `bg-slate-800`, `text-white`, `border-red-500`, `ring-blue-500` | `Bg(tw.Slate800)`, `TextColor(tw.White)`, `BorderColor(tw.Red500)`, `RingColor(tw.Blue500)` |
| `bg-black/50` | `Bg(tw.Black.Alpha(0.5))` |
| `w-[84px]`, `w-[50%]`, `p-[3px]`, `-mt-[4px]` | `W(gpui.Px(84))`, `W(gpui.Fraction(0.5))`, `P(gpui.Px(3))`, `Mt(gpui.Px(-4))` |
| `text-[13px]`, `rounded-[10px]`, `border-[3px]` | `TextSize(gpui.Px(13))`, `Rounded(gpui.Px(10))`, `Border(gpui.Px(3))` |
| `opacity-[.35]`, `font-[550]` | `Opacity(0.35)`, `FontWeight(550)` |
| `overflow-auto`, `overflow-y-auto` | `OverflowScroll()`, `OverflowYScroll()`, which need an ID |

A few classes exist only in `Class`: `inset-x-*` / `inset-y-*` (use `Left0().Right0()` and so on), the bare `rounded-t` / `rounded-tl` and friends (use `RoundedT(gpui.RemsOf(0.25))`), and spacing values with no method, such as `p-13`.

## Class strings

`Class` works on every styled element: `Div`, `Img`, any component that embeds `gpui.Styled` or `gpui.Component`, and `gpui.Styles()`.

- Within one string, classes apply left to right like a method chain, and a later class wins when two set the same property. Tailwind decides by stylesheet order instead, so avoid conflicting classes.
- `Class` mixes with methods. Calls run in chain order, so `.Class("p-4").P2()` ends with `p-2`. Each property the string sets replaces the element's, so a class cannot clear a value a method set before it (`content-normal`, `text-decoration-none`), and grid placement classes (`col-span-2`, `row-start-1`, …) replace the element's whole grid placement. Keep an element's placement classes in one string.
- The important modifier (`!p-4`, `p-4!`) is accepted and has no effect.
- Classes that only remove a value (`content-normal`, `text-decoration-none`) do nothing in `Class`. Call the method instead.
- Each distinct string is parsed once and cached (up to 16,384 strings; after that, strings are parsed on every call). Pass string literals.

Use each spelling where it fits:

| Prefer methods for | Prefer `Class` for |
|---|---|
| Computed values: `W(gpui.Px(width))`, `Bg(theme.Primary)` | Porting or pasting web markup |
| Values that change every render | Long, static utility lists |
| Compile-time checking and editor completion | Code that people who know Tailwind will read |

Conditional classes (React's `clsx` or template literals) become `When` with literal strings, which [twcheck](#errors-and-twcheck) can check:

```go
gpui.Div().
	Class("px-3 py-1 rounded-md text-sm text-slate-300").
	When(selected, func(d *gpui.DivElement) *gpui.DivElement {
		return d.Class("bg-blue-600 text-white")
	})
```

Do not format values into class strings (`Class(fmt.Sprintf("w-[%dpx]", n))`). Each value becomes a new cache entry, and twcheck cannot check the string. Write `W(gpui.Px(float32(n)))`.

## Colors

The palette is Tailwind CSS v3.4's default palette. It has 22 families (slate, gray, zinc, neutral, stone, red, orange, amber, yellow, lime, green, emerald, teal, cyan, sky, blue, indigo, violet, purple, fuchsia, pink, rose), each with shades 50, 100–900 and 950, plus `black`, `white` and `transparent`. Package `tw` has them as values: `tw.Slate800`, `tw.White`, `tw.Transparent`.

| Class | Method |
|---|---|
| `bg-slate-800` | `Bg(tw.Slate800)` |
| `text-white` | `TextColor(tw.White)` |
| `border-red-500` | `BorderColor(tw.Red500)` |
| `ring-blue-500` | `RingColor(tw.Blue500)` |
| `inset-ring-blue-500` | `InsetRingColor(tw.Blue500)` |
| `outline-blue-500` | `OutlineColor(tw.Blue500)` |
| `bg-transparent` | `Bg(tw.Transparent)` |
| `bg-black/50`, `bg-slate-900/[0.35]` | `Bg(tw.Black.Alpha(0.5))`, `Bg(tw.Slate900.Alpha(0.35))` |
| `bg-[#1e293b]` | `Bg(gpui.Hex("#1e293b"))` or `Bg(gpui.Rgb(0x1e293b))` |
| `bg-[#1e293b]/50` | `Bg(gpui.Hex("#1e293b").Alpha(0.5))` |

- The opacity modifier (`/50`, `/[0.35]`, `/[35%]`) multiplies the color's alpha (`Hsla.Opacity`). Palette colors are opaque, so for them it equals `Alpha`.
- `[#…]` accepts `#rgb`, `#rgba`, `#rrggbb` and `#rrggbbaa`.
- Color helpers: `gpui.Rgb(0xRRGGBB)`, `gpui.Hex("#…")` (panics on bad input), `gpui.ParseHex` (returns an error), `c.Alpha(a)` (sets the alpha) and `c.Opacity(f)` (multiplies it).
- For a color name known only at run time, use `gpui.TailwindColor("slate-800")`, which returns `(gpui.Hsla, bool)`.
- Theme colors are not classes. shadcn-style `bg-primary` or `text-muted-foreground` is an unknown class. Pass Go values to the method: `.Bg(theme.Primary)`.
- Text color, size, weight and font are inherited by children, as in CSS.

## State styles

| Tailwind | `Class` | Method | Needs |
|---|---|---|---|
| `hover:bg-slate-700` | `Class("hover:bg-slate-700")` | `.Hover(gpui.Styles().Bg(tw.Slate700))` | Nothing extra |
| `active:bg-slate-900` | `Class("active:bg-slate-900")` | `.Active(gpui.Styles().Bg(tw.Slate900))` | `.ID(…)` |
| `focus:border-blue-500` | `Class("focus:border-blue-500")` | `.Focus(gpui.Styles().BorderColor(tw.Blue500))` | `.TrackFocus(handle)` |
| `focus-visible:ring-2` | `Class("focus-visible:ring-2")` | `.FocusVisible(gpui.Styles().Ring2())` | `.TrackFocus(handle)`. Applies only after keyboard input. |
| `focus-within:border-blue-500` | `Class("focus-within:border-blue-500")` | `.InFocus(gpui.Styles().BorderColor(tw.Blue500))` | `.TrackFocus(handle)` on this element |
| `group` on the parent | `Class("group")` | `.Group("")` | Nothing extra |
| `group-hover:text-white` on a child | `Class("group-hover:text-white")` | `.GroupHover("", gpui.Styles().TextColor(tw.White))` | An ancestor in the group |
| `group/menu` with `group-hover/menu:…` | `Class("group/menu")`, `Class("group-hover/menu:text-white")` | `.Group("menu")`, `.GroupHover("menu", …)` | An ancestor in the group |
| `group-active:bg-slate-900` | `Class("group-active:bg-slate-900")` | `.GroupActive("", gpui.Styles().Bg(tw.Slate900))` | `.ID(…)` |

- `gpui.Styles()` returns a `*gpui.StyleBuilder`, which has the whole styled vocabulary on a style that belongs to no element. It also takes base classes: `.Hover(gpui.Styles().Class("bg-slate-700 text-white"))`. The pinned closure form, `func(gpui.StyleRefinement) gpui.StyleRefinement`, still works.
- State styles need a `Div` or a component that embeds `gpui.Interactive` (or `gpui.Component`). On an `Img` or inside `Styles()`, `Class` reports variants as errors.
- Each class takes one variant. Stacked variants such as `hover:focus:…` are not supported.
- `Class` merges into state styles set earlier in the chain. Calling `.Hover(…)` after a `Class` with `hover:` panics with "hover style already set", the same as calling `Hover` twice. The other state methods (`Active`, `Focus`, …) called after `Class` replace the state style that `Class` set. Use one spelling per state on an element.
- An element joins one group, and its `group-hover:` (or `group-active:`) styles follow one group.
- State styles switch instantly. There are no transitions.
- Create a focus handle once and keep it in the view. For example, set `v.focus = cx.App().FocusHandle()` in the entity's init function, track it with `.TrackFocus(v.focus)`, and focus it with `w.Focus(v.focus)`.

## What Class supports

- **Every zero-argument method that has a Tailwind class**, about 2,260 classes, named from the method names. This covers layout, flex, grid, spacing, sizing, position, insets, display, typography, borders, radii, shadows, rings, cursors and opacity steps. GPUI-only methods with no Tailwind class (negative padding, gap and sizes such as `PNeg4`; `MinSize4`, `MaxSize4`) have no class.
- **Spacing and sizing on the 0.25rem scale, any step.** `p-13`, `w-0.75` and `gap-7` work even without a method (`n` is `n/4` rem). The prefixes are `p px py pt pr pb pl`, `m mx my mt mr mb ml`, `gap gap-x gap-y`, `w h size min-w min-h max-w max-h`, `inset inset-x inset-y top right bottom left` and `basis`. Values can be `n`, `px`, `full`, a fraction `a/b`, `auto` (not for padding or gap), `[12px]`, `[1.5rem]`, `[50%]` or `[0]`. A leading `-` works for margins and insets only.
- **max-w names**: `max-w-xs` … `max-w-7xl` (20rem … 80rem, the v3 scale), plus `max-w-none` and `max-h-none`.
- **Borders**: `border`, `border-t|r|b|l|x|y` (1px), `border-<n>`, `border-[3px]`, `border-t-[3px]`, `border-solid`, `border-dashed`.
- **Radii**: `rounded` and `rounded-t|r|b|l|tl|tr|br|bl` (0.25rem), `rounded-none|xs|sm|md|lg|xl|2xl|3xl|full`, `rounded-[10px]`, `rounded-tl-[10px]`.
- **Shadows and rings**: `shadow` (v3's default), `shadow-2xs` … `shadow-2xl`, `shadow-none`, `shadow-inner`, `ring` (3px), `ring-<n>`, `ring-[3px]`, `inset-ring` (1px), `inset-ring-<n>`, `inset-ring-[3px]`.
- **Outlines** (ticket 48): `outline` (1px solid), `outline-<n>`, `outline-[3px]`, `outline-offset-<n>`, `outline-offset-[3px]`, `outline-<color>`, `outline-none` (also the default focus ring's opt-out — a focused, focus-visible element draws Chrome's ring otherwise), `outline-dashed`. The outline paints above the content and never affects layout; negative offsets have no class (call `.OutlineOffset(-3)`).
- **Typography**: `text-xs` … `text-9xl`, `text-[13px]`, `font-thin` … `font-black`, `font-[550]`, `leading-none|tight|snug|normal|relaxed|loose`, `leading-<n>` (n/4 rem), `leading-[1.4]` (a multiple of the font size), `leading-[20px]`, `tracking-[0.5px]`, `tracking-tighter|tight|normal|wide|wider|widest` (em-based, browser-core 38), `line-clamp-<n>`, `text-wrap`, `text-nowrap`, `whitespace-pre|pre-line|pre-wrap` (browser-core 38).
- **Opacity**: `opacity-<0..100>`, `opacity-[.35]`, `opacity-[35%]`.
- **Flex and grid**: `flex-grow`, `flex-shrink`, `grid-cols-<n>`, `grid-rows-<n>`, `col-span-<n>`, `row-span-<n>`, `col-start-<n>`, `col-end-<n>`, `row-start-<n>`, `row-end-<n>`.
- **Aspect ratio**: `aspect-square`, `aspect-video`, `aspect-[4/3]`, `aspect-[1.5]`.
- **Blur**: `blur-none`, `blur-xs|sm` (4px), `blur` (8px), `blur-md|lg|xl|2xl|3xl` (12/16/24/40/64px) and `blur-[6px]`. `backdrop-blur-*` takes the same values. Filter classes compose in class order (see the next bullet); `blur-none` resets the whole filter chain.
- **Gradients** (two stops, interpolated like CSS): `bg-linear-to-t|tr|r|br|b|bl|l|tl`, `bg-linear-<deg>` (also `-bg-linear-45`), `bg-linear-[45deg]` (deg, rad, grad, turn), then `from-<color>` and `to-<color>` set the two stops (`from-red-500`, `from-[#ff0000]`, `from-transparent`, `from-red-500/50`), and `from-<pos>`/`to-<pos>` their positions (`from-10%`, `from-[12.5%]`). The classes compose in order: `bg-linear-to-r from-red-500 to-blue-500` (Tailwind's default stops are transparent at 0% and 100%, so `bg-linear-to-r` alone paints nothing). The gradient replaces the background color — there is no CSS `background-color` under `background-image` layer. Interpolation is CSS's legacy sRGB, premultiplied: `bg-linear-[45deg]` matches a real Tailwind page bit for bit, and the named and numeric forms (`bg-linear-to-r`, `bg-linear-45`) render the equivalent CSS *without* the `in oklab` Tailwind itself emits — add the `/srgb` modifier to opt into exactly the one space drawn. Under a state variant the whole gradient must live in that variant: `hover:bg-linear-to-r hover:from-red-500 hover:to-blue-500`.
- **Colour filters**: `grayscale`, `grayscale-0`, `grayscale-[.5]`, `sepia`, `sepia-0`, `invert`, `invert-0`, `invert-[25%]`, `saturate-0…200` and `saturate-[1.25]`, `brightness-0…200` and `brightness-[1.75]`, `contrast-…`, `hue-rotate-0|15|30|60|90|180`, `-hue-rotate-90`, `hue-rotate-[90deg]`. Consecutive filter classes compose in class order — the CSS spelling of `filter: grayscale(1) blur(4)` — like the fluent methods (`.Grayscale(1).Blur(4)`); only the element's own filter chain takes the colour functions, `backdrop-*` stays blur-only.
- **Replaced-content options**: `object-fill`, `object-contain`, `object-cover`, `object-none`, `object-scale-down`, the nine position keywords `object-left-top` … `object-bottom`, `rendering-pixelated`, `rendering-auto`. They style the img element's paint (CSS applies them to replaced elements only); arbitrary positions call `.ObjectPosition(…)` on the img.
- **Overflow**: `overflow-hidden|visible|clip` and their `-x`/`-y` forms. `overflow-auto` and `overflow-scroll` (and their `-x`/`-y` forms) make the div scroll, so the element needs `.ID(…)` and must be a Div or a component that embeds `Interactive`.
- **Forced colours opt-out**: `forced-color-adjust-none` (CSS `forced-color-adjust: none`) keeps the element's authored colours, gradients and shadows while the forced-colours pass is active (Windows high contrast, or an `App.SetForcedColors` pin); the fluent method is `.ForcedColorAdjustNone()`. The `forced-colors:` media variant stays unsupported — the pass is not a styling variant but the application-level state.

## What Class rejects, and what to use instead

A rejected class is skipped, the rest of the string still applies, and the reason is reported ([Errors and twcheck](#errors-and-twcheck)).

| Tailwind | Reason, and what to do instead |
|---|---|
| `z-*` | There is no z-index: siblings paint in tree order. Render overlays last, or wrap them in `gpui.Deferred`. |
| `transition*`, `duration-*`, `ease-*`, `delay-*`, `animate-*` | There are no CSS transitions. State styles switch instantly. Animate with `gpui.WithAnimation`. |
| `translate-*`, `rotate-*`, `scale-*`, `skew-*`, `origin-*`, `transform` | There are no CSS transforms. Position with margins or insets. |
| `space-x-*`, `space-y-*` | Use `gap-*` on the flex parent. |
| `divide-*` | Give each child after the first a `border-t` (or `border-l`), or put separator elements between them. |
| `select-*` | Text in a div is not selectable. Drop it. |
| `pointer-events-*` | Not supported. |
| `fixed` | Use `absolute` inside a `relative` ancestor that fills the window, or `gpui.Deferred` / `gpui.Anchored` for overlays. |
| `sticky`, `static` | Not supported. Elements are relative unless absolute. |
| `inline-block`, `inline-grid`, `table*`, `contents`, `flow-root`, `list-item` | Use `flex`, `grid`, `block`, `inline` or `inline-flex`. |
| `outline-solid`, `outline-dotted`, `outline-double`, `outline-hidden` | Only `outline`, `outline-<n>`, `outline-offset-<n>`, `outline-<color>`, `outline-none` and `outline-dashed` are supported: the native scene's border records are solid or dashed only (a recorded limitation, ticket 48). Negative outline offsets have no class; call `.OutlineOffset(-3)`. |
| `ring-offset-*`, `ring-inset` | Not supported. Use `inset-ring-*` for an inset ring. |
| `bg-gradient-*` | Tailwind v4 renamed it: write `bg-linear-to-r from-… to-…`. |
| `via-*`, `via-none` | A middle stop makes three, and the scene record carries exactly two stops (ticket 52, the R16 gradient redesign). |
| `bg-linear-to-r/oklab` and other non-srgb modifiers | Gradients interpolate in sRGB (CSS's legacy default); the record's Oklab arm is the pinned 2.2-curve path, not CSS's (ticket 52). `/srgb` is accepted. |
| `bg-radial-*`, `bg-conic-*` | Radial and conic gradients need the R16 record (ticket 52); only two-stop linear gradients are supported. |
| `from-…` before `bg-linear-…` | from-/to- set one stop of the gradient the same class string (or variant) starts: put `bg-linear-…` first. |
| `from-[10px]` (a length position) | Stop positions are percentages of the gradient line (`from-10%`); a length needs the line's length at paint time (ticket 52). |
| `bg-cover`, `bg-center`, `bg-no-repeat`, … | Background images are not supported. Put an `Img` behind the content. |
| `drop-shadow-*`, `mix-blend-*`, the bare `filter` class | Only blur and the colour filters are supported (see above). Each filter utility applies itself, so the bare `filter` class is not needed. |
| `float-*`, `clear-*` | Use flex or grid. |
| `object-[10px_20px]` and other arbitrary positions | The keyword classes are supported (see above); call the img's `.ObjectPosition(…)` for arbitrary values. |
| `order-*` | Reorder the children. |
| `columns-*`, `break-after-*`, `break-inside-*` | Use grid. |
| `place-*`, `justify-items-*`, `justify-self-*` | Use `items-*`, `justify-*`, `content-*` and `self-*`. |
| `box-border`, `box-content` | Box sizing is always border-box. Drop it. |
| `whitespace-break-spaces`, `break-words`, `break-all`, `hyphens-*`, `text-balance`, `text-pretty` | `whitespace-normal`, `whitespace-nowrap`, `whitespace-pre`, `whitespace-pre-line` and `whitespace-pre-wrap` are supported (browser-core 38); break-spaces and the word-break family are not. |
| `text-justify` | Justified text is not supported. |
| `antialiased`, `subpixel-antialiased` | GPUI chooses text antialiasing itself. Drop it. |

| `no-underline` | Leave `underline` out, or call `.TextDecorationNone()`. |
| `decoration-*`, `underline-offset-*` | Use `underline` with `text-decoration-wavy` / `text-decoration-2` in the same string, or call `.TextDecorationColor(c)`. |
| `w-screen`, `h-screen`, `min-h-screen`, `*-dvh`, `*-svh`, `*-lvh` | There are no viewport units. Use `size-full` on a parent that fills the window. The root view is window-sized. |
| `w-fit`, `w-min`, `w-max` | Leave the size auto, and use `self-start` or `flex-none` so the element hugs its content. |
| Negative padding, gap or size (`-p-2`) | Negative values work for margins and insets only. |
| `border-t-red-500` (a per-side color) | An element has one border color. Use `border-<color>`. |
| `border-s-*`, `border-e-*` | Use `border-l` / `border-r`. |
| `text-current`, `bg-inherit` | Repeat the color. Text color already cascades to children. |
| `shadow-red-500` (shadow colors) | Call `.Shadow(…)` with colored `gpui.BoxShadow` values. |
| `fill-*`, `stroke-*` | An svg element is painted with its text color. Use `text-<color>`. |
| `placeholder-*` | Placeholder styling belongs to the text input component. |
| `list-*`, `appearance-*`, `resize-*`, `accent-*`, `caret-*` | Not supported. GPUI has no native form controls. |
| `scroll-*`, `snap-*`, `touch-*`, `overscroll-*`, `will-change-*`, `isolate` | Not supported. |
| `sr-only` | Describe the element through its accessibility properties. |
| `container`, `prose` | Set `max-w-*` and padding explicitly. |
| `bg-primary` and other theme names | Pass the Go value: `.Bg(theme.Primary)`. |

| Variant | Reason, and what to do instead |
|---|---|
| `sm:`, `md:`, `lg:`, `xl:`, `2xl:`, `max-*:`, `min-*:`, `portrait:`, `landscape:` | There are no media queries. Choose classes in Render from the window size (`w.ViewportSize().Width`). |
| `dark:`, `light:` | Pick colors from your theme in Render. |
| `disabled:`, `checked:`, `invalid:`, `required:`, `open:`, `visited:`, … | Style that state in Render: `.When(disabled, …)`. |
| `first:`, `last:`, `odd:`, `even:`, `only:`, `empty:`, `*:` | Style the item in the loop that builds it. |
| `peer-*:` | Put both elements in a group and use `group-hover:`. |
| `group-focus:` and other group variants | Only `group-hover:` and `group-active:` are supported. |
| `aria-*:`, `data-*:`, `has-*:`, `supports-*:`, `not-*:` | Style from state in Render. |
| `before:`, `after:`, `placeholder:`, `file:`, `marker:`, `selection:`, … | Pseudo-elements are not supported. |
| `motion-safe:`, `motion-reduce:`, `print:`, `rtl:`, `ltr:`, `contrast-*:`, `forced-colors:` | Not supported. |
| `[&_svg]:`, `@container`, `@md:` | Arbitrary and container variants are not supported. |
| `hover:focus:…` | Use one variant per class. |

## Tailwind versions

- **Palette**: Tailwind CSS v3.4.17.
- **Spacing**: the 0.25rem scale, which v3 and v4 share. `Class` accepts any multiple of 0.25 (v4 style). Methods exist for the standard steps.
- **Radii follow GPUI's v4-style names**: `rounded-xs` is 0.125rem, `rounded-sm` 0.25rem, `rounded-md` 0.375rem, `rounded-lg` 0.5rem, `rounded-xl` 0.75rem, `rounded-2xl` 1rem and `rounded-3xl` 1.5rem. Bare `rounded` is 0.25rem. v3's `rounded-sm` (0.125rem) is `rounded-xs` here.
- **Shadows follow GPUI's v4-style names**: `shadow-xs` is v3's `shadow-sm`, and `shadow-sm` is v3's `shadow`. `shadow-md`, `-lg`, `-xl` and `-2xl` match v3. Bare `shadow` is v3's default, the same as `shadow-sm` here.
- **Ring**: bare `ring` is 3px (v3). v4's bare `ring` is 1px, so write `ring-1` for that.
- **Blur**: the v3 scale (`blur` is 8px, `blur-sm` 4px). `blur-xs` (v4) is also 4px.
- **max-w-xs … 7xl**: the v3 rem scale.
- **Font size**: `text-xs` … `text-9xl` set only the font size. Tailwind also sets a line height, so add `leading-*` when line height matters.
- **Font weights and line heights**: the v3 values (`font-thin` 100 … `font-black` 900; `leading-tight` 1.25 … `leading-loose` 2).

## Errors and twcheck

`Class` never panics on a bad class by default. It skips the class and reports it to the class error handler. The default handler writes one line to stderr for each distinct string, the first time the string is used:

```text
gpui: Class("flex z-10 itms-center"): "z-10": GPUI has no z-index: siblings paint in tree order; render overlays last, or wrap them in gpui.Deferred; unknown class "itms-center"
```

For strict mode, in tests or development builds, install a handler that panics:

```go
gpui.SetClassErrorHandler(func(classes string, err error) {
	panic(fmt.Sprintf("Class(%q): %v", classes, err))
})
```

`SetClassErrorHandler` returns the previous handler. Passing nil restores the default. `gpui.CheckClasses(s)` returns the problems of one string (one joined error per bad class) or nil. It does not know the element, so it accepts state variants everywhere.

**twcheck** finds bad classes before the program runs. Run it after you edit UI code, the way you run `go vet`:

```sh
go run github.com/hoangvu12/gpui-go/cmd/twcheck ./...
```

- Arguments are directories, `dir/...`, or `.go` files. The default is `./...`. The walk skips `vendor`, `testdata`, and directories that start with `.` or `_`. Test files are checked too.
- It checks every `.Class(…)` call whose argument is a constant string: a literal, a package-level string constant from the same directory, or a `+` concatenation of those. Other arguments are skipped, and `-v` lists them with a summary.
- It prints one line per bad class, `path:line:col: error`, and exits 1 if it found any (2 on a usage error).
- It reads syntax only, so it cannot tell that a state variant is on an element without interactivity (an `Img`, for example). That case is reported at run time.

```text
ui/card.go:42:9: "z-10": GPUI has no z-index: siblings paint in tree order; render overlays last, or wrap them in gpui.Deferred
ui/card.go:57:9: unknown class "itms-center"
```

## GPUI rules web code doesn't teach

1. **Call `cx.Notify()` after changing state.** It plays the role of `setState`. Changing a view's fields does nothing on screen until the view notifies. Then Render runs again and rebuilds the element tree.
2. **Clickable, pressable and scrollable divs need `.ID(…)`.** `OnClick`, `active:` / `Active`, `group-active:`, `OnHover`, `OnDrag` and `overflow-auto` / `overflow-scroll` / `OverflowScroll` require an element ID. Without one, the frame panics with "… require an element ID". Hover alone does not need an ID. Give siblings distinct IDs; in a loop, use `gpui.NamedIntegerElementID("row", uint64(i))`.
3. **Update the view from handlers with `cx.Listener`.** `cx.Listener(func(v *T, e *gpui.ClickEvent, w *gpui.Window, cx *gpui.Context[T]) { … })` hands the callback the current state and a fresh context. Do not capture the handle from `cx.Entity()` in a closure, because it expires when Render returns. Do not mutate the `v` captured from Render either. When a callback needs a handle value (for example, to spawn work), use `cx.StrongSelf()`. See the README section [Updating the view from a listener](../README.md#updating-the-view-from-a-listener).
4. **Use `gpui.UniformList` for long lists.** It renders only the visible rows, which must all be the same height. Create the scroll handle once and keep it in the view:

   ```go
   // in the view: scroll *gpui.UniformListScrollHandle, set to gpui.NewUniformListScrollHandle() once
   gpui.UniformList(v.scroll, len(v.rows), func(start, end int, w *gpui.Window, app *gpui.App) []gpui.AnyElement {
   	items := make([]gpui.AnyElement, 0, end-start)
   	for i := start; i < end; i++ {
   		items = append(items, gpui.Div().Class("h-8 px-3 flex items-center").Child(v.rows[i]).IntoElement())
   	}
   	return items
   }).SizeFull()
   ```

   For rows of different heights, use `gpui.List` with `gpui.NewListState`. Do not build thousands of divs in Render.
5. **Layout is flexbox and grid (Taffy).** GPUI's default style is not CSS's: a div with no display class is a flex row (no wrap, `shrink` 1), not a block. Write `flex` (row) or `flex flex-col` (column) explicitly, as Tailwind code usually does. The root view fills the window, so give its root div `size-full`. There are no viewport units and no z-index: later siblings paint on top, and overlays go in `gpui.Deferred` or `gpui.Anchored`.
6. **Text is a child.** `.Child("Save")` or `.Child(fmt.Sprintf("%d items", n))`. Text styles on a div apply to its text and are inherited by its children.

## A full example: a card with a button

React with Tailwind:

```tsx
export function CounterCard() {
  const [count, setCount] = useState(0);
  return (
    <div className="flex h-screen items-center justify-center bg-slate-950">
      <div className="flex w-80 flex-col gap-4 rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg">
        <h2 className="text-lg font-semibold text-white">Counter</h2>
        <p className="text-sm text-slate-400">Clicked {count} times</p>
        <button
          onClick={() => setCount(count + 1)}
          className="rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white hover:bg-blue-500 active:bg-blue-700"
        >
          Increment
        </button>
      </div>
    </div>
  );
}
```

gpui-go:

```go
type counterCard struct {
	count int
}

func (v *counterCard) Render(w *gpui.Window, cx *gpui.Context[counterCard]) gpui.AnyElement {
	return gpui.Div().
		Class("flex size-full items-center justify-center bg-slate-950"). // h-screen → size-full
		Child(gpui.Div().
			Class("flex w-80 flex-col gap-4 rounded-xl border border-slate-800 bg-slate-900 p-6 shadow-lg").
			Child(gpui.Div().Class("text-lg font-semibold text-white").Child("Counter")).
			Child(gpui.Div().Class("text-sm text-slate-400").Child(fmt.Sprintf("Clicked %d times", v.count))).
			Child(gpui.Div().
				ID("increment"). // OnClick and active: need an ID
				Class("flex justify-center rounded-lg bg-blue-600 px-4 py-2 text-sm font-medium text-white cursor-pointer hover:bg-blue-500 active:bg-blue-700").
				OnClick(cx.Listener(func(v *counterCard, _ *gpui.ClickEvent, _ *gpui.Window, cx *gpui.Context[counterCard]) {
					v.count++
					cx.Notify() // re-render, like setState
				})).
				Child("Increment"))).
		IntoElement()
}
```

The translation:

- `h-screen` became `size-full`, because the root view already fills the window.
- `<h2>`, `<p>` and `<button>` became divs with the same classes.
- The button's `flex justify-center` and `cursor-pointer` replace what a browser `<button>` does by default.
- `useState` became a struct field. `setCount` became `v.count++` plus `cx.Notify()` inside `cx.Listener`.

The button with typed methods:

```go
gpui.Div().
	ID("increment").
	Flex().JustifyCenter().RoundedLg().Bg(tw.Blue600).Px4().Py2().
	TextSm().FontMedium().TextColor(tw.White).CursorPointer().
	Hover(gpui.Styles().Bg(tw.Blue500)).
	Active(gpui.Styles().Bg(tw.Blue700)).
	OnClick(cx.Listener(func(v *counterCard, _ *gpui.ClickEvent, _ *gpui.Window, cx *gpui.Context[counterCard]) {
		v.count++
		cx.Notify()
	})).
	Child("Increment")
```

To run the card in a window, copy [examples/hello/main.go](../examples/hello/main.go) and replace `hello` with `counterCard`.
