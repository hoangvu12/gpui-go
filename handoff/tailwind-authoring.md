# Handoff: Tailwind-style authoring for gpui-go (2026-10-22)

From the Roboco chat "Optimizing Rust GUI Development" (claude-code, opus). Read this whole note before touching code.

## Hard constraints right now

- **The user is playing a game.** Do not run `go build`, `go test`, `go vet`, `go generate`, `go run` or anything else CPU-heavy until the user says it's OK. `gofmt -l`/`gofmt -w` on single files, `git` and file reads are fine. Write the code, then ask before verifying.
- Work only in this worktree: `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go-wt-tailwind`, branch `tailwind-authoring` (from `main` @ `8eed385`).
- **Don't touch the main checkout** `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go`. It's on branch `fast-test-loop` with 38 uncommitted files from another chat ("GPUI-Go Performance Improvement Research").
- Native DLL frozen at rev 11: no cargo/Rust. `CGO_ENABLED=0`.
- Don't push and don't merge to `main` without asking. The repo is public.
- Repo conventions: `AGENTS.md`, `PROJECT.md`, `handoff/resume.md` (on main), and `docs/agents/*`. Leave `handoff/resume.md` to the main checkout's owner; this note is the handoff for this branch.

## What the user asked for and why

The user wants AI agents to write gpui-go apps easily. LLMs know HTML/CSS/Tailwind/React/shadcn best, and no model has seen gpui-go. The user approved four items ("ok 1 to 4 then"):

1. **Complete the Tailwind mapping.** Add the Tailwind color palette and the missing utilities, so a Tailwind class always has a same-named fluent method (`items-center` → `ItemsCenter()`, `bg-slate-800` → `Bg(tw.Slate800)`).
2. **Make state styles one-liners.** Today hover means mutating struct fields by hand (see `mordbard/app/ui/icons.go:153`). Target: `.Hover(gpui.Styles().Bg(tw.Slate700))`.
3. **`.Class("flex items-center gap-2 px-4 py-2 rounded-lg bg-slate-800 hover:bg-slate-700")`** on every Styled element. Parse each string once, cache it, and give agents a checker that rejects unknown classes.
4. **A component library: the GPUI Base port.** This already exists as a planned effort (see "Item 4" below).

The user also wants a short agent guide (a "React + Tailwind → gpui-go" cheat sheet). Write it as part of this work (`docs/tailwind.md`).

## Status update (2026-10-22, second chat)

Written since the table below, all uncommitted and **never compiled** (the user was still gaming):
- `gpui/class.go`: the parser as designed in section a, with these deviations. `group` alone is the group named `""`. An element has one group and one group-hover/group-active group, so a second one is an error. `shadow-inner` is supported (inset shadows exist). `shadow` = `ShadowSm()`, which is v3's default. `text-wrap`/`text-nowrap` map to the whitespace methods. `overflow-visible`/`-clip` are supported. `outline-*` is rejected with a "drop it" hint. Blur classes replace the filter chain. Tokens apply to a scratch builder and are then refined in, so `content-normal`/`text-decoration-none` are no-ops in Class (documented on Class). The parse cache is capped at 16k strings. The internals the tests use are `applyUtility`, `lengthClass`, `lengthProps`, `parseClasses` and `classSet`.
- Delegated to subagents (check their output exists): `cmd/twcheck/main.go`, `tw/doc.go`, `gpui/class_test.go`, `tw/palette_test.go`, `docs/tailwind.md`, the README section, `docs/adr/0001-tailwind-authoring.md`, THIRD_PARTY_NOTICES plus `gen.py`, CONTEXT.md entries, and Hover doc lines.
- Verified 2026-10-22 and committed on `tailwind-authoring`; not merged or pushed. Checks run:
  - `go vet ./...` is clean.
  - `go test -short ./...` is green except `headlessspec`, which needs the gitignored `reference/ce-source` that this worktree lacks. One `a11yspec` concurrency test and one gpui direct-manipulation timing test failed under load but pass alone and on `main`.
  - `buildcostspec` probe sizes are unchanged.
  - `go generate ./gpui` produces no diff.
  - `twcheck` passed a smoke test.
- The build cost was measured and then reduced. A switch over all 3,210 classes doubled gpui's own compile (5.6 s to 10.7 s). The switch now holds 399 classes; the 1,864 spacing/sizing classes use the length fallback, which `class_generated_test.go` checks against each method's field writes. The 947 GPUI-only classes are rejected. gpui now compiles in about 6.6 s. ADR 0001 has the numbers.
- Not done: the mordbard smoke test (f.4) and real-window checks.

## Done so far (uncommitted in this worktree; not yet compiled together)

`go build ./gpui` passed after the first two files below were added. Nothing after that has been compiled: not style_builder.go, the interact edits, or the generated files. No tests have run.

| File | What |
|---|---|
| `gpui/styled_tailwind.go` | New Styled methods, all with the `[_ lazy]` marker: `FontThin`…`FontBlack` (100–900), `Uppercase`/`Lowercase`/`Capitalize`/`NormalCase` (TextTransform), `LeadingNone/Tight/Snug/Normal/Relaxed/Loose` (LineHeight Fraction 1/1.25/1.375/1.5/1.625/2), `Text4xl`…`Text9xl` (rems 2.25/3/3.75/4.5/6/8), `Grow`/`Grow0`/`Shrink`/`Shrink0`, `Opacity0`…`Opacity100` in steps of 5. |
| `gpui/color_helpers.go` | `Rgb(0xRRGGBB) Hsla` (the pinned rust `rgb()`), `Hex("#…")` (panics on bad input), `ParseHex`, `Hsla.Opacity(factor)` (multiplies alpha, pinned `Hsla::opacity`), `Hsla.Alpha(a)` (sets alpha). It reuses `clamp01` from paint.go. |
| `gpui/style_builder.go` | `type StyleBuilder struct{ Styled[*StyleBuilder] }`, `Styles()`, `(*StyleBuilder).Refinement()`, the `StyleArg` constraint (`func(StyleRefinement) StyleRefinement \| *StyleBuilder`), and `stateRefinement`. A prebuilt refinement is equivalent to the closure because the pin runs the closure once on an empty refinement (div.rs:970-977). |
| `gpui/interact.go`, `gpui/interact_mixin.go` | The 7 state builders (`Hover`, `GroupHover`, `Active`, `GroupActive`, `Focus`, `InFocus`, `FocusVisible`) are now `[F StyleArg]` on both `*DivElement` and `Interactive[Self]`, using `stateRefinement`. Existing closure call sites should still compile; confirm with `go vet ./...` later. Their doc comments don't yet mention `Styles()`; add one line to `Hover`. |
| `cmd/tailwindgen/main.go`, `cmd/tailwindgen/palette.go` | Generator. `palette.go` is the committed palette data (Tailwind v3.4.17 `src/public/colors.js`, MIT): 22 families × 11 shades. `main.go` parses `styled.go`, `styled_generated.go` and `styled_tailwind.go` with go/parser, takes every exported zero-arg `*Styled[Self]` method whose only type param is `[_ lazy]`, and converts the name to a class. Words go kebab; `Neg` becomes a leading `-`; a `p` between digits becomes `.`; `_` between digits becomes `/`. Examples: `MtNeg2`→`-mt-2`, `W1_2`→`w-1/2`, `P0p5`→`p-0.5`, `Text2xl`→`text-2xl`. It fails on duplicate classes. |
| `gpui/class_generated.go` | Generated: `applyMethodClass(class string, b *StyleBuilder) bool` (a switch calling the method) and `methodClassCount` = **3210**. Spot-checked correct: items-center, justify-between, p-0.5, w-1/2, -mt-2, -m-0.5, text-2xl, rounded-tl-md, w-px, size-full, font-bold, opacity-50, uppercase, leading-tight, cursor-ns-resize, overflow-x-hidden, flex-row-reverse, shadow-2xl, border-t-1, border-x-1, top-1/2, -top-1/2, truncate, whitespace-nowrap, aspect-square, shrink-0, grow, ring-1, border-dashed. It also contains GPUI-only oddities like `-py-8` and `min-size-1/3`; they're harmless. **Not** in it: `inset-x-0` (GPUI has no inset_x), `border`, `rounded`, `shadow`, `border-t` (their GPUI methods take an argument), so the parser handles those. |
| `gpui/tailwind_palette_generated.go` | Generated: `tailwindPalette map[string]uint32`, keyed `"slate-800"` etc. plus `"black"` and `"white"`. |
| `tw/palette_generated.go` | Generated: `package tw` vars `Black`, `White`, `Transparent` (= `Rgb(0).Alpha(0)`), and `Slate50`…`Rose950` as `gpui.Rgb(0x…)`. |

Run the generator from `gpui/`: `go run ../cmd/tailwindgen` (defaults `-gpui . -tw ../tw`). Put the directive in class.go: `//go:generate go run ../cmd/tailwindgen`.

## Remaining work for items 1–3

### a. `gpui/class.go`: the parser (the main piece; design already worked out)

Public API:
- `func (s *Styled[Self]) Class[_ lazy](classes string) Self` parses (cached) and applies the classes, then returns `s.self`. Keep the body tiny and call non-generic helpers, because the lazy method compiles per call site.
- `func CheckClasses(classes string) error` returns every unknown or unsupported class, joined. Used by tests and twcheck.
- `func TailwindColor(name string) (Hsla, bool)` looks up `"slate-800"`, `"white"`, `"black"` and `"transparent"`.
- `func SetClassErrorHandler(h func(classes string, err error)) (previous func(classes string, err error))`. The default writes `gpui: Class("…"): …` to stderr **once per distinct string**.

Internals:
- `parseClasses(classes) *classSet` is cached in a `sync.Map` and immutable after the parse.
- `classSet` holds:
  - `base StyleRefinement`
  - states as kind + group + `StyleRefinement`, with all tokens of one kind/group merged into one refinement
  - `groups []string` (from `group` / `group/name`)
  - `scrollX`, `scrollY bool`
  - `err error`
- Apply: `b := set.base.Clone(); style.Refine(&b)`. Clone because Refine copies slices (BoxShadow) by reference.
- States, groups and scroll need the interactive div:
  - `interactiveDiv(owner any) *DivElement` is `*DivElement` itself, or `interface{ Interactivity() *DivElement }`. That's a plain method on `Interactive[Self]` (fluent.go:232), so it's in the method set.
  - If there's no such div and the set has states, groups or scroll, report an error naming the classes.
  - Merge into the existing state style rather than replacing it: if `d.hoverStyle != nil`, Refine into it, else set it. Same for groupHover (`*GroupStyle{Group, Style}`, merged when the group matches), active, groupActive, focus, inFocus and focusVisible.
  - Calling `.Hover()` *after* a Class containing `hover:` still panics "hover style already set", matching the pin. Document that.

Token handling:
- Split on whitespace. Strip a leading `!` (important).
- Split variants on `:` outside `[...]`. Supported variants:
  - `hover:`, `active:`, `focus:`
  - `focus-visible:`, `focus-within:` (→ InFocus)
  - `group-hover:`, `group-active:`, `group-hover/name:`, `group-active/name:`
  - Only one variant per token.
  - Unsupported, with an error: `sm: md: lg: xl: 2xl: dark: disabled: first: last: odd: even: peer-*: aria-*: data-*:` and arbitrary variants.

Each utility is applied to a scratch `*StyleBuilder`, in this order:
1. `applyMethodClass` (generated, exact parity with the methods).
2. **Specials.**
   - `border` → `Border1()`; `border-t|r|b|l|x|y` → `BorderT1()`… (zero-arg 1px).
   - `rounded` → `Rounded(RemsOf(0.25))`; `rounded-t|r|b|l|tl|tr|br|bl` with no size → that side at 0.25rem.
   - `shadow` → the Tailwind v3 default: `0 1px 3px 0 rgb(0 0 0/.1), 0 1px 2px -1px rgb(0 0 0/.1)`. Build it with `NewBoxShadow(0,1,SolidBackground(Hsla{A:0.1})).WithBlurRadius(3)` etc., like the generated ShadowMd.
   - `ring` → `Ring(3)` (v3 width).
   - `flex-grow` → Grow; `flex-shrink` → Shrink.
   - `aspect-video` → `AspectRatio(16/9)`; `aspect-[a/b]`.
   - `group`, `group/name` → record a group.
   - `overflow-scroll`, `overflow-auto` → both axes; `overflow-x-scroll|auto`, `overflow-y-scroll|auto` → that axis. Applied with `d.OverflowXScroll()` / `d.OverflowYScroll()` / `d.OverflowScroll()` (scroll_element.go:266-288). They need an ID at render, like the pin.
3. **Length prefixes, with an optional leading `-` for negatives** (margins and insets only). Use longest-prefix match.
   - Definite lengths (no auto, no negative): `p px py pt pr pb pl`, `gap gap-x gap-y`. Call `b.P(DefiniteLength)` etc.
   - Lengths (auto allowed): `m mx my mt mr mb ml`, `w h size min-w min-h max-w max-h`, `inset top right bottom left basis` (basis → `FlexBasis`). `inset-x`/`inset-y` have no GPUI method, so write `StyleMut().InsetLeft/InsetRight` (or Top/Bottom) directly.
   - Values:
     - A number that's a multiple of 0.25 → `DefiniteRem(n/4)`. This matches the generated scale: `W4` = `DefiniteRem(1)`, `W0p5` = `DefiniteRem(0.125)`.
     - `px` → `DefinitePx(1)`.
     - `full` → `Fraction(1)`; `a/b` → `Fraction(a/b)`.
     - `auto` → `AutoLength()`.
     - `max-w-none` / `max-h-none` → `AutoLength()`.
     - `max-w-xs|sm|md|lg|xl|2xl|3xl|4xl|5xl|6xl|7xl` → rems 20/24/28/32/36/42/48/56/64/72/80.
     - Arbitrary `[12px]`, `[1.5rem]`, `[50%]`.
     - Wrap in `DefiniteLengthOf` for Length kinds.
   - Unsupported, with a hint: `w-screen`, `h-screen`, `min-h-screen`, `*-dvh`, `*-svh` (no viewport units; use `size-full` on a full-size parent).
4. **Typography.**
   - `text-[13px]` / `text-[0.8rem]` → `TextSize`.
   - `font-[550]` → `FontWeight`.
   - `leading-<n>` → `LineHeight(DefiniteRem(n/4))`; `leading-[1.4]` → `Fraction`; `leading-[20px]` → absolute.
   - `tracking-[0.5px]` → `LetterSpacing(px)`. Named `tracking-*` is unsupported (em-based, while LetterSpacing is px).
   - `line-clamp-<n>` → `LineClamp`.
   - `font-sans|serif|mono` is unsupported; suggest `FontFamily("…")`.
5. **Colors.** Prefixes `bg-`, `text-`, `border-`, `ring-` → `Bg` / `TextColor` / `BorderColor` / `RingColor`.
   - Values: a palette name, `white`, `black`, `transparent`, `[#hex]` (`ParseHex`).
   - Optional `/<0-100>` or `/[0.35]` → `Alpha(c.A*n/100)`.
   - Try the method switch first, so `text-sm`, `border-2` and `ring-2` win over colors.
   - Unsupported: per-side border colors (`border-t-red-500`), gradients (`from-`/`via-`/`to-`/`bg-gradient-*`; the native artifact is solid-only, decision 02), `current`, `inherit`, `placeholder-`, `decoration-`, `outline-`, `accent-`, `caret-`, `fill-`, `stroke-`.
6. **Borders and radii with values.** `border-<n>` beyond the table → `Border(Px(n))`; `border-[3px]`; `border-t-[3px]` → `BorderT(Px(3))`; `rounded-[10px]` → `Rounded(Px(10))`; `rounded-t-[..]` etc. → `RoundedT(...)`.
7. **Opacity.** `opacity-<0..100>` beyond the 5-steps → `Opacity(n/100)`; `opacity-[.35]`.
8. **Grid.** `grid-cols-<n>` → `GridCols(uint16)`, `grid-rows-<n>` → `GridRows`, `col-span-<n>` → `ColSpan`, `row-span-<n>` → `RowSpan`.
9. **Blur.**
   - `blur-sm|blur|blur-md|lg|xl|2xl|3xl` → `Blur(4|8|12|16|24|40|64)`.
   - `backdrop-blur-*` uses the same scale → `BackdropBlur`.
   - Arbitrary `blur-[6px]`.
   - `blur-none` → unsupported unless 0 works.
10. **Explicitly unsupported** (clear reason in the error):
   - no engine support: `z-*`, `transition*`, `duration-*`, `ease-*`, `delay-*`, `animate-*`, `translate-*`, `rotate-*`, `scale-*`, `skew-*`, `origin-*`
   - `space-x|y-*` (say: use `gap-*` on a flex parent)
   - `divide-*`, `select-*`, `pointer-events-*`
   - `fixed`, `sticky`, `inline`, `inline-block`, `inline-flex`, `table*`, `contents`
   - `text-justify` (if there's no TextAlignJustify)
   - `shadow-inner`, `no-underline`
11. Anything else → `unknown class "…"`.

### b. `tw/doc.go`

Package doc: "Tailwind CSS v3.4 palette as gpui colors. Class names map 1:1: bg-slate-800 → Bg(tw.Slate800); opacity modifier bg-black/50 → Bg(tw.Black.Alpha(0.5))." Mention `gpui.TailwindColor` for names known only at run time.

### c. `cmd/twcheck`

`go run github.com/hoangvu12/gpui-go/cmd/twcheck ./...` walks the given dirs (with `dir/...` recursion; skip vendor, testdata and dot dirs). It parses `.go` files with go/parser and finds calls `X.Class(<string literal or constant concatenation>)`. Each one goes through `gpui.CheckClasses`. It prints `file:line: err` and exits 1 on any problem. gpui's `init` only arms lazy loaders (glyph.go), so importing gpui from a cmd doesn't touch the native DLL.

### d. Tests (headless, safe under `-short`; no real windows)

All in `package gpui` unless noted.

- **Method-class parity.** `Div().Class("p-4")` equals `Div().P4()` (compare `StyleMut()` with reflect.DeepEqual), for a handful of tokens.
- **Fallback parity, the strong one.** For every generated class with a spacing/sizing/inset prefix and a numeric, fraction, `px`, `full` or `auto` value, the length-prefix fallback must produce the same refinement as `applyMethodClass`. Call the fallback directly to prove the scales agree.
- **Colors.** `bg-slate-800` = `Bg(Rgb(0x1e293b))`, `text-white`, `border-red-500`, `bg-black/50` = `Alpha(0.5)`, `bg-[#1e293b]`, `bg-[#1e293b]/[0.35]`.
- **Arbitrary values.** `w-[84px]`, `w-[50%]`, `p-[3px]`, `text-[13px]`, `rounded-[10px]`, `-mt-[4px]`, `p-13` (v4-style any multiple).
- **Variants on a Div.**
  - `Div().ID("x").Class("hover:bg-slate-700 hover:text-white active:bg-slate-900 focus:border-blue-500 group group-hover:text-white")` sets `hoverStyle` (both props merged), `activeStyle`, `focusStyle`, `group` and `groupHoverStyle`.
  - The same through a component embedding `Interactive`.
  - An error when used on a `StyleBuilder` (no div).
- **Errors.** `CheckClasses("flex foo z-10 sm:p-4 space-x-2")` names all four problems. The handler runs once per string. Unknown classes don't panic.
- **StyleBuilder.** `Div().Hover(Styles().Bg(c))` equals `Div().Hover(func(s StyleRefinement) StyleRefinement { s.Background = Some(BackgroundOf(c)); return s })`. A nil `*StyleBuilder` panics.
- **Colors API.** `Rgb`, `Hex` (3/4/6/8 digits, bad input), `Opacity`, `Alpha`, `TailwindColor`.
- **Package tw** (external test): `tw.Slate800 == gpui.Rgb(0x1e293b)`; `tw.Transparent.A == 0`.
- **Generator round trip.** Running tailwindgen produces no diff. Do that in verification, not as a test.

### e. Docs

- **`docs/tailwind.md`, the agent guide (high value).**
  - The rule `class → Method` with examples, plus Class strings and when to prefer methods (typed, compile-checked) vs Class (paste-from-web).
  - The color palette and the `/50` alpha.
  - A state-styles table.
  - The supported/unsupported lists.
  - twcheck.
  - The GPUI-only rules LLMs don't know:
    - after changing state call `cx.Notify()` (like React's setState)
    - clickable/active/scrollable divs need `.ID(...)`
    - use `cx.Listener` for handlers that update the view; don't capture the entity handle from `cx.Entity()` (README "Updating the view from a listener")
    - `uniform_list` for long lists
  - One full example: a card with a button, written as React+Tailwind and then gpui-go.
- **README.md:** a short "Tailwind-style authoring" section linking the guide.
- **ADR `docs/adr/0001-tailwind-authoring.md`** (the first ADR; see `docs/agents/domain.md`).
  - Decision: add Tailwind-named extensions beyond the 1:1 GPUI-CE pin, for agent/LLM familiarity.
  - The pinned vocabulary stays byte-identical in `styled_generated.go`; extensions live in separate, labelled files (`styled_tailwind.go`, `class*.go`, `color_helpers.go`, `style_builder.go`, `tw/`).
  - The state builders' params widened to `StyleArg`; the closure form is unchanged.
- **THIRD_PARTY_NOTICES.md:** add Tailwind CSS (MIT, Copyright (c) Tailwind Labs, Inc.), covering the palette values and the scale/names.
- **Optional:** add `StyleBuilder` and `Class` to the root `CONTEXT.md` glossary.

### f. Verification (only once the user allows CPU use)

1. `gofmt -l .`, `go vet ./...` (compiles all tests, so it catches broken Hover call sites in `internal/*spec` and examples), `go test -short ./...`.
2. `internal/buildcostspec` is the lazy-marker guard. All new Styled methods have the marker. `class_generated.go` instantiates 3,210 methods for `*StyleBuilder` **inside gpui**, which affects gpui's own compile time but not consumers' object size. Measure `go build ./gpui` time before and after (stash-free: compare against `main` in a scratch worktree) and report it honestly. If it's a real regression, move the switch body to call field writes or another cheaper form.
3. `go generate ./gpui` → no diff.
4. Build mordbard against this branch: in a scratch copy, set the replace to the worktree. Then convert one screen's hover closures to `Styles()` / `Class` as a smoke test. Real-window checks only with the user's OK, since they steal focus.

### g. Commit

Commit on `tailwind-authoring`, and ask the user before merging to `main`/pushing. Suggested message: "Tailwind-style authoring: palette, missing utilities, Styles() state builders, Class() + twcheck (ADR 0001)".

## Item 4: the GPUI Base port (exists; the user paused it)

- **Plan:** `.scratch/gpui-base/map.md` (pin `gpui-ce/gpui-component@c0820693`, `crates/base`, 108k Rust lines, 1,226 tests, to a Go package `base`). Tickets are in `.scratch/gpui-base/issues/`. The process is in `handoff/resume.md` § "ACTIVE EFFORT": orchestrator plus parallel background implementers (Roboco, harness `pi`, model `iroha/dashscope/glm-5.3`), disjoint file ownership, and the orchestrator re-runs tests. The decisions are recorded (deps policy, native gap fallbacks).
- **State:**
  - Wave 1 (core completion) is done except **10 rich text**.
  - **10, 13 (base infra) and 15 (data structures) are marked `claimed` but their workers died.** Nothing is on disk: there's no `base/` dir. Reopen them.
  - 14 and 16–32 are open.
  - `base-port` was merged into main at `8eed385`.
- **On 2026-10-19 the user said "can u stopp all the work"** in the orchestrator chat "GPUI Base port" (`37dc6248`). They've now approved item 4 again as part of "1 to 4", but **confirm with the user before restarting the multi-agent waves**, and mind machine load (they were gaming).
- Sequencing: land items 1–3 first, so Base components can use `Class`/`Styles()`/`tw`. Base itself follows GPUI Base's own theme tokens (shadcn-like: primary, muted, border, ring…) and naming. That's what makes it agent-friendly, so keep upstream names.

## Background (why gpui-go matters to the user)

Measured in the source chat on this PC:
- **Rust GPUI app (roboco):**
  - one-literal UI edit → app: 19–23 s (12–14 s with rust-lld)
  - engine test rebuild: ~3 min
  - `target` dirs: 10–18 GB per worktree
- **mordbard on gpui-go:**
  - UI edit → exe: 1–2 s
  - all test binaries: 1.25 s
  - one shared 6.6 GB Go cache

The user's complaints about mordbard (hover only after a click, double-click needed to navigate, Backspace not reaching the text field, missing cursor) were classes of core bugs. Tickets 03 (auto redraw), 34 (real-window input) and consumer gap 11 (initial cursor) addressed several. Worth re-checking mordbard on current main before assuming they persist.
