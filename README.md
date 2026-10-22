# gpui-go

gpui-go is a Go port of [GPUI-CE](https://github.com/gpui-ce/gpui-ce), a community-maintained fork of GPUI, the UI framework Zed is built with. It follows GPUI-CE at commit `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`, aims to behave like that core, and should look about 98% the same on screen. The 98% is a design target that no test reports as a score. You write views, entities and fluent element trees in Go much as you would in Rust, without a Rust toolchain or Rust compile times.

## What works

The whole GPUI core is ported for Windows on AMD64. That covers layout (Taffy), text shaping and rasterization, the D3D11 and DirectComposition renderer, windows and input, including IME composition. It also covers accessibility through UI Automation, screen capture, the clipboard, drag and drop, SVG and image decoding.

GPUI Base, the component library that sits on top of the core, has not been ported. Other operating systems and architectures will come later.

## Install

You need Go 1.27 or newer on Windows AMD64.

```sh
go get github.com/hoangvu12/gpui-go@v0.1.0
```

Build with `CGO_ENABLED=0`. The project uses no cgo, so you need neither a C compiler nor Rust. The native services (text, layout, rendering, accessibility) live in one DLL that is embedded in the module. On first use the loader checks its SHA-256 against the embedded manifest and writes it to a per-user cache under `%LocalAppData%\gpui-go\native`. Nothing is downloaded at run time.

## A first window

[examples/hello](examples/hello/main.go) opens a window with a centered box:

```sh
go run github.com/hoangvu12/gpui-go/examples/hello@v0.1.0
```

A view is a struct with a `Render` method that returns an element tree:

```go
type hello struct{}

func (h *hello) Render(w *gpui.Window, cx *gpui.Context[hello]) gpui.AnyElement {
	return gpui.Div().
		Flex().
		SizeFull().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		Bg(gpui.RgbaToHsla(0x1e293bff)).
		Child(gpui.Div().
			Flex().
			Justify(gpui.AlignContentCenter).
			AlignItems(gpui.AlignItemsCenter).
			Size(gpui.PxLength(160), gpui.PxLength(96)).
			Bg(gpui.RgbaToHsla(0x2563ebff)).
			TextColor(gpui.RgbaToHsla(0xffffffff)).
			TextLg().
			Child("Hello!")).
		IntoElement()
}
```

The rest of the example creates the app and its Win32 host, opens the window with `App.OpenWindowView`, and presents frames. The present loop is still explicit. The window's `OnRedraw` hook hands the last drawn scene to the window's surface: `gpui.LastDrawnScene` returns the scene the frame loop built, and the window's `Surface.DrawScene` puts it on screen. Text drawn by the element tree presents too: the scene's glyph sprites live in the atlas the window's text system rasterized them into, so the loop passes `Window.GlyphAtlas()` alongside the scene (a scene without sprites ignores it, and drawing a sprite scene with a nil or foreign atlas fails with the typed `ErrSceneSpriteAtlas`). For larger programs, the real-window tests under `internal/*spec` exercise each subsystem against live windows. The [two-window counter](internal/authorspec/counter_window_test.go) is a good place to start, and [the consumer present loop](internal/presentspec/present_spec_test.go) shows element-tree text presented and read back pixel by pixel.

## Updating the view from a listener

Listeners run after `Render` returns, so a listener must not capture the entity handle that `cx.Entity()` hands out: in Go a borrow cannot keep a lease alive the way Rust's `Entity<T>` clone does, so that handle is callback-borrowed and expires when the render callback returns. The view's strong self-handle replaces the reference's `cx.entity()` capture pattern:

```go
type counter struct {
	count int
}

func (v *counter) Render(w *gpui.Window, cx *gpui.Context[counter]) gpui.AnyElement {
	self := cx.StrongSelf()
	return gpui.Div().
		ID("increment").
		Flex().
		SizeFull().
		Justify(gpui.AlignContentCenter).
		AlignItems(gpui.AlignItemsCenter).
		OnClick(func(_ *gpui.ClickEvent, _ *gpui.Window, app *gpui.App) {
			self.Update(app, func(v *counter, cx *gpui.Context[counter]) {
				v.count++
				cx.Notify() // queues the redraw that re-renders this view
			})
		}).
		Child(fmt.Sprintf("Count: %d", v.count)).
		IntoElement()
}
```

`cx.StrongSelf()` is only valid inside the view's `Render` — it panics elsewhere with a misuse diagnostic. The window drawing the view owns one lease for the handle (shared by every render, so repeated renders do not accumulate leases), and that lease releases when the window closes. Never `Release` or `TransferInto` the self-handle; call `RetainInto` if you need an independently owned copy with its own lifetime, or `Downgrade` for a weak identity.

When a listener only needs to update the view's own state, `cx.Listener` is the lighter and preferred shape — it captures a weak identity, hands your callback the fresh state and context, and becomes a no-op after the view releases:

```go
OnClick(cx.Listener(func(v *counter, _ *gpui.ClickEvent, _ *gpui.Window, cx *gpui.Context[counter]) {
	v.count++
	cx.Notify()
}))
```

Use `cx.StrongSelf()` when porting GPUI code that captured `cx.entity()` into a closure and calls `Update` on it, or when a callback needs the handle as a value (for example to spawn work that updates the view from its result). Both shapes update the entity through an update — never mutate the state pointer captured in a closure directly. The real-window proof of both shapes is the [self-handle corpus](internal/authorspec/self_handle_window_test.go); the deterministic lease and release rules live in [internal/portfixture](internal/portfixture/self_handle_test.go).

**Embedded fonts.** Apps that ship their own typefaces (embedded with `go:embed`, for instance) register the bytes before the first text layout:

```go
ts, err := gpui.DefaultTextSystem()
if err != nil { /* ... */ }
if _, err := ts.AddFont(geistBytes); err != nil { /* the bytes hold no font face */ }
```

`TextSystem.AddFont` mirrors GPUI's `add_fonts`: family names come from each font's embedded name table, they appear in `FontNames`, and `FontDescriptor{Family: "Geist"}` resolves to the registered face for shaping and rasterization. Fonts are process-stable; there is no unregister. If a family resolves nowhere — an embedded font that failed to register, or a name that differs from the font's internal family name — `ResolveFont` still returns the fallback font (GPUI's behavior), but `FontIdentity.FamilyFound` is `false`, so the missing font is diagnosable.

## Tailwind-style authoring

GPUI's style methods already follow Tailwind's names. gpui-go completes the mapping so that a Tailwind class always has a method of the same name (`items-center` is `ItemsCenter()`, `px-4` is `Px4()`, `w-1/2` is `W1_2()`, `bg-slate-800` is `Bg(tw.Slate800)`). Package [`tw`](tw) holds the Tailwind v3.4 color palette, state styles take a prebuilt style from `gpui.Styles()`, and `Class` applies a class string the way an HTML `class` attribute does:

```go
gpui.Div().
	ID("save").
	Class("flex items-center gap-2 px-4 py-2 rounded-lg bg-slate-800 text-white hover:bg-slate-700 active:bg-slate-900").
	OnClick(cx.Listener(func(v *editor, _ *gpui.ClickEvent, _ *gpui.Window, cx *gpui.Context[editor]) {
		v.save()
		cx.Notify()
	})).
	Child("Save")

// The same style with typed methods:
gpui.Div().ID("save").Flex().ItemsCenter().Gap2().Px4().Py2().RoundedLg().Bg(tw.Slate800).TextColor(tw.White).
	Hover(gpui.Styles().Bg(tw.Slate700)).
	Active(gpui.Styles().Bg(tw.Slate900))
```

`Class` parses each distinct string once and caches it. A class it cannot apply, such as a typo or a utility GPUI has no equivalent for (`z-10`, `sm:p-4`), is skipped and reported on stderr. `go run github.com/hoangvu12/gpui-go/cmd/twcheck ./...` finds those classes before the program runs. [docs/tailwind.md](docs/tailwind.md) is the full guide for people and coding agents who come from React and Tailwind. It covers the naming rule, what is and is not supported, and the GPUI rules that web code does not teach. These names are gpui-go additions beyond the GPUI-CE pin; [ADR 0001](docs/adr/0001-tailwind-authoring.md) records why.

## Build speed

A one-line edit in a UI package rebuilds in about 1–2 seconds. The fluent methods of `Styled`, `Parent` and `Interactive` (and so of `Div`, `Img`, `Canvas` and every `Component[Self]` component) are generic methods marked with an internal type parameter, `func (s *Styled[Self]) Flex[_ lazy]() Self`. You call them as usual (`d.Flex()`), and Go compiles only the methods a package actually calls. With ordinary methods, every package that mentioned a styled element recompiled the whole 3,100-method vocabulary. The one difference from ordinary methods is that a fluent method cannot be taken as a method value (`f := d.Flex`); wrap it in a closure instead. [internal/buildcostspec](internal/buildcostspec/buildcost_test.go) fails if a consumer package's compiled size grows back.

## Status

Version 0.1.0 is the first tagged release. All 34 tickets in the core backlog are closed. The test suite drives real windows on a real GPU device, and the accessibility tree was checked with Narrator. Because this is a 0.x release, the API can still change. The planning record and the upstream research are linked from the [research index](docs/research-index.md).

## License

gpui-go is licensed under the [Apache License 2.0](LICENSE), the same license as GPUI-CE. The embedded native library includes third-party crates, and the Tailwind color palette comes from Tailwind CSS (MIT); their licenses are collected in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
