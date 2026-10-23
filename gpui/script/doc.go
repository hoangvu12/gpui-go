// Package script is gpui-go's TSX UI script host (ADR 0003): a .tsx
// view — JSX elements with CSS-named style objects, Tailwind
// className and DOM-named events — compiled by esbuild and run by
// goja, lowered to the same browser-like elements a Go view builds.
// The probe that proved the model is research/tsx-probe (report 09);
// this package is its productionization.
//
// # The model (zgui's, per ADR 0003)
//
// The script describes the element tree once per (re)load. The host
// owns keyed state, so it survives a reload; updates change bound
// values without re-running the tree; the script runs again only for
// event handlers, function components and the bindings it declared.
// Lowering (tree → gpui elements) happens once per frame and never
// runs the script.
//
// Two update paths exist, and they are not equal:
//
//	const clicks = state("clicks", 0);            // keyed, host-owned
//	<text>{bind(clicks, (n) => `Clicked ${n}`)}</text>
//
// state(key, init) with bind is the fast path: a click that calls
// clicks.set recomputes only the bindings on that key — the probe
// measured 0.001 ms where a whole-tree re-describe costs 2.3 ms.
// useState(key, init) looks like React's, but setting it re-runs App
// and describes the whole tree again (~2.2 µs per node): it works, it
// survives reloads like any keyed state, and it is the slow path —
// reach for bound values on anything a user can click repeatedly.
//
// # The script's surface
//
// The entry module must export a function component named App; its JSX
// tree is the window's content. JSX compiles with the classic factory
// (JSXFactory "h"): function components run in JS, host elements are
// built in Go.
//
// Host elements are div and text (a text element is div sugar for a
// text run), plus Fragment, whose children lower as their parent's
// direct children. Their props:
//
//	key?: string         identity for stateful behaviour (scrolling,
//	                     hover): becomes the element's id when there is
//	                     none; assigned automatically (auto-N) to
//	                     elements with handlers or scroll.
//	id?: string          the DOM identity: the element's id and its
//	                     debug selector (bounds key).
//	className?: string   Tailwind classes, through the same class layer
//	                     as Styled.Class. Applied before style, so
//	                     inline style wins, as in CSS. An unknown class
//	                     is a load error, not stderr noise.
//	style?: CSSProperties  a CSS-named style object (camelCase, as in
//	                     React). Supported today: display ("flex" |
//	                     "block"), flexDirection, alignItems, justify-
//	                     Content, flex (1), gap, width, height (a px
//	                     number, "Npx", or "100%"), minHeight, padding
//	                     (one, two or four lengths), background, color,
//	                     borderColor, border, borderRadius, fontSize,
//	                     fontWeight, opacity and overflowY ("scroll" |
//	                     "auto") — colors as CSS hex. Anything else is a
//	                     load error that names the .tsx line.
//	onClick?: (e) => void
//	onMouseDown?: (e) => void  the left button. The event is DOM-shaped:
//	                     { button, x, y, shiftKey, ctrlKey, altKey,
//	                     metaKey }.
//
// Children are elements, strings, numbers, bindings, arrays (flattened)
// and false/undefined/null (skipped, so {cond && <x />} works).
//
// # Loading, reloading, errors
//
// Dev and release differ only in where the compiled bundle comes from
// (ADR 0003: esbuild ships in development only):
//
//	// development: read .tsx from disk, reload on save
//	source, _ := compile.Open("ui/app.tsx")       // gpui/script/compile
//	host := script.New(script.WithSource(source))
//	host.Reload()
//	// a file watcher (compile.Dir.Watch) calls host.Reload on change
//
//	// release: compile at build time, embed, never read from disk
//	//go:embed app.js
//	var js string
//	host := script.New()
//	host.Load(script.Bundle{Name: "app.js", JS: js})
//
// Reload builds a fresh goja runtime and re-describes the tree; the
// keyed state map is untouched, so values survive (init matters only
// for keys that did not exist). A failed load — a compile error, a
// runtime error, a bad prop, style key or class — keeps the last good
// tree, records the error (Host.Err, the WithErrorHandler hook) and,
// by default, mounts an error banner over the bottom of the window
// with the .tsx file and line (esbuild reports compile positions;
// goja maps runtime positions through the inline source map; describe-
// time errors carry the position of the JSX element that failed). The
// banner is mounted only while there is an error; a good reload clears
// it. Release apps turn it off with WithoutBanner and report through
// WithErrorHandler.
//
// # Mounting
//
// Mount the host's view as the window's root:
//
//	root := gpui.NewEntity(app, window.Scope(),
//		func(v *script.View, _ *gpui.Context[script.View]) { v.Host = host })
//	window.SetRootView(gpui.ViewOf(root))
//
// script.View renders Host.Element() (the described tree, lowered
// fresh each frame) and wires Host.Notify to Window.Refresh the first
// time it renders, so state changes and reloads redraw. Host is not
// safe for concurrent use: call Load, Reload and handlers from the
// thread that renders (a watcher must marshal its notification to the
// app thread, e.g. with app.Foreground().Spawn and AsyncApp.Update).
//
// # Later tickets (not built here)
//
// Host-provided browser globals (setTimeout, fetch, Intl) beyond the
// app's own SetGlobal values; a generated .d.ts and check command;
// the dev command (Go rebuild and restart with state restore);
// plugin slots, contributions and capability grants (each plugin in
// its own goja runtime with a time budget). The SetGlobal mechanism
// and this package's public surface are the seam those build on.
package script
