# gpui-go

gpui-go is a Go port of [GPUI-CE](https://github.com/gpui-ce/gpui-ce), a community-maintained fork of GPUI, the UI framework Zed is built with. It follows GPUI-CE at commit `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`. The goal is to behave like that core and look about 98% the same on screen. The 98% is a design target; no test measures it. You write views, entities and fluent element trees in Go much as you would in Rust, without a Rust toolchain or Rust compile times.

## What works

The whole GPUI core is ported for Windows on AMD64: layout (Taffy), text shaping and rasterization, the D3D11 and DirectComposition renderer, windows and input (including IME composition), accessibility through UI Automation, screen capture, the clipboard, drag and drop, and SVG and image decoding.

GPUI Base, the component library that sits on top of the core, is being ported and is not in a release yet. Other operating systems and architectures will come later.

## Install

You need Go 1.27 or newer on Windows AMD64.

```sh
go get github.com/hoangvu12/gpui-go@v0.1.0
```

v0.1.0 is the latest tag. Newer work, including the Tailwind-style authoring described below, is on `main` until the next release: `go get github.com/hoangvu12/gpui-go@main`.

Build with `CGO_ENABLED=0`. The project uses no cgo, so you need neither a C compiler nor Rust. The native services (text, layout, rendering and accessibility) are in one DLL embedded in the module. On first use, the loader checks the DLL's SHA-256 against the embedded manifest and writes it to a per-user cache under `%LocalAppData%\gpui-go\native`. Nothing is downloaded at run time.

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

The rest of the example creates the app and its Win32 host, opens the window with `App.OpenWindowView`, and presents frames. You still write the present loop yourself. The window's `OnRedraw` hook takes the scene the frame loop last built (`gpui.LastDrawnScene`) and draws it with the window's `Surface.DrawScene`. Text needs one more argument. Its glyph sprites live in the atlas the window's text system rasterized them into, so the loop passes `Window.GlyphAtlas()` along with the scene. A scene without sprites ignores the atlas, and a scene with sprites fails with the typed `ErrSceneSpriteAtlas` if the atlas is nil or a different one.

For larger programs, look at the real-window tests under `internal/*spec`, which exercise each subsystem against live windows. The [two-window counter](internal/authorspec/counter_window_test.go) is a good place to start, and [the consumer present loop](internal/presentspec/present_spec_test.go) presents element-tree text and reads it back pixel by pixel.

## Updating the view from a listener

Listeners run after `Render` returns, so a listener must not capture the entity handle from `cx.Entity()`. That handle is borrowed for the render callback and expires when the callback returns, because in Go a borrow cannot keep a lease alive the way a cloned Rust `Entity<T>` does. Where Rust GPUI code captures `cx.entity()`, use the view's strong self-handle:

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

`cx.StrongSelf()` works only inside the view's `Render`; anywhere else it panics with a misuse message. The window drawing the view owns one lease for the handle. Every render shares that lease, so repeated renders do not pile up leases, and it is released when the window closes. Never call `Release` or `TransferInto` on the self-handle. Call `RetainInto` for an independently owned copy with its own lifetime, or `Downgrade` for a weak identity.

When a listener only updates the view's own state, prefer `cx.Listener`. It is lighter: it captures a weak identity, passes your callback the current state and context, and does nothing once the view is released.

```go
OnClick(cx.Listener(func(v *counter, _ *gpui.ClickEvent, _ *gpui.Window, cx *gpui.Context[counter]) {
	v.count++
	cx.Notify()
}))
```

Use `cx.StrongSelf()` when porting GPUI code that captures `cx.entity()` in a closure and calls `Update` on it, or when a callback needs the handle as a value, for example to spawn work that updates the view with its result. Either way, change the entity's state inside an update, and never mutate the state pointer a closure captured. The [self-handle tests](internal/authorspec/self_handle_window_test.go) run both forms in real windows, and [internal/portfixture](internal/portfixture/self_handle_test.go) holds the deterministic lease and release rules.

## Embedded fonts

Apps that ship their own typefaces, embedded with `go:embed` for instance, register the font bytes before the first text layout:

```go
ts, err := gpui.DefaultTextSystem()
if err != nil { /* ... */ }
if _, err := ts.AddFont(geistBytes); err != nil { /* the bytes hold no font face */ }
```

`TextSystem.AddFont` mirrors GPUI's `add_fonts`. Family names come from each font's name table and appear in `FontNames`, and `FontDescriptor{Family: "Geist"}` then resolves to the registered face for shaping and rasterization. A registered font stays for the life of the process; there is no unregister. If a family resolves nowhere, because the font failed to register or the name differs from the font's internal family name, `ResolveFont` still returns the fallback font as GPUI does. `FontIdentity.FamilyFound` is `false` in that case, so you can tell the font is missing.

## Tailwind-style authoring

GPUI's style methods already use Tailwind's names, and gpui-go fills in the rest, so most Tailwind utility classes have a method of the same name: `items-center` is `ItemsCenter()`, `px-4` is `Px4()`, `w-1/2` is `W1_2()` and `bg-slate-800` is `Bg(tw.Slate800)`. Package [`tw`](tw) holds the Tailwind v3.4 color palette. The state methods (`Hover`, `Active` and the rest) take a style built with `gpui.Styles()`, and `Class` applies a class string the way an HTML `class` attribute does:

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

`Class` parses each distinct string once and caches the result. A class it cannot apply, such as a typo or a utility GPUI has no equivalent for (`z-10`, `sm:p-4`), is skipped and reported on stderr. `go run github.com/hoangvu12/gpui-go/cmd/twcheck ./...` finds those classes before the program runs.

[docs/tailwind.md](docs/tailwind.md) is the full guide for people and coding agents coming from React and Tailwind. It covers the naming rule, what is supported, and the GPUI rules that web code never needs, such as calling `cx.Notify()` after a state change. These names go beyond the GPUI-CE pin; [ADR 0001](docs/adr/0001-tailwind-authoring.md) records why.

## Build speed

A one-line edit in a UI package rebuilds in about 1 to 2 seconds. The fluent methods of `Styled`, `Parent` and `Interactive` (and so of `Div`, `Img`, `Canvas` and every `Component[Self]` component) are generic methods with an internal type parameter: `func (s *Styled[Self]) Flex[_ lazy]() Self`. You call them as usual (`d.Flex()`), and Go compiles only the methods a package calls. With ordinary methods, every package that mentions a styled element would recompile the whole vocabulary of about 3,100 methods. The catch is that you cannot take a fluent method as a method value (`f := d.Flex`); wrap it in a closure instead. [internal/buildcostspec](internal/buildcostspec/buildcost_test.go) fails if a consumer package's compiled size goes over its cap.

## Status

Version 0.1.0 is the first tagged release, and all 34 tickets in the core backlog are closed. The test suite drives real windows on a real GPU, and the accessibility tree was checked with Narrator. This is a 0.x release, so the API can still change. The [research index](docs/research-index.md) links the planning record and the upstream research.

## License

gpui-go is licensed under the [Apache License 2.0](LICENSE), the same license as GPUI-CE. The embedded native library includes third-party crates, and the color palette comes from Tailwind CSS (MIT). [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) collects their licenses.
