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
			Size(gpui.PxLength(160), gpui.PxLength(96)).
			Bg(gpui.RgbaToHsla(0x2563ebff))).
		IntoElement()
}
```

The rest of the example creates the app and its Win32 host, opens the window with `App.OpenWindowView`, and presents frames. The present loop is still explicit. `gpui.DrawWindowFrame` builds the scene, and the window's `Surface.DrawScene` puts it on screen. The example repeats both steps from the window's `OnRedraw` hook. The public API does not yet expose the atlas that holds a window's glyphs, so a hand-written loop like this one can only present scenes without text. For larger programs, the real-window tests under `internal/*spec` exercise each subsystem against live windows. The [two-window counter](internal/authorspec/counter_window_test.go) is a good place to start.

**Embedded fonts.** Apps that ship their own typefaces (embedded with `go:embed`, for instance) register the bytes before the first text layout:

```go
ts, err := gpui.DefaultTextSystem()
if err != nil { /* ... */ }
if _, err := ts.AddFont(geistBytes); err != nil { /* the bytes hold no font face */ }
```

`TextSystem.AddFont` mirrors GPUI's `add_fonts`: family names come from each font's embedded name table, they appear in `FontNames`, and `FontDescriptor{Family: "Geist"}` resolves to the registered face for shaping and rasterization. Fonts are process-stable; there is no unregister. If a family resolves nowhere — an embedded font that failed to register, or a name that differs from the font's internal family name — `ResolveFont` still returns the fallback font (GPUI's behavior), but `FontIdentity.FamilyFound` is `false`, so the missing font is diagnosable.

## Status

Version 0.1.0 is the first tagged release. All 34 tickets in the core backlog are closed. The test suite drives real windows on a real GPU device, and the accessibility tree was checked with Narrator. Because this is a 0.x release, the API can still change. The planning record and the upstream research are linked from the [research index](docs/research-index.md).

## License

gpui-go is licensed under the [Apache License 2.0](LICENSE), the same license as GPUI-CE. The embedded native library includes third-party crates, whose licenses are collected in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
