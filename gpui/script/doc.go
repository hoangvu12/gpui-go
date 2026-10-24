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
//	UI := ... // generated: see Release bundling below
//	host := script.New(script.WithoutBanner())
//	host.Load(UI.Bundle())
//
// # Release bundling (ticket 65)
//
// Go has no build.rs — go build never runs code — so the build-time
// compile is a go:generate step: cmd/tsxbundle (run through go run,
// carried by the module, nothing installed) compiles the entry with
// esbuild through gpui/script/compile and writes, beside the app's
// main package:
//
//	//go:generate go run github.com/hoangvu12/gpui-go/cmd/tsxbundle -var UI -entry ui/app.tsx
//
//	ui.tsxbundle.js       the compiled ES2017 bundle (inline source map)
//	ui.tsxbundle.go       the script.EmbeddedBundle var //go:embed picks up
//	ui.tsxbundle_test.go  the staleness guard (EmbeddedBundle.CheckFresh)
//
// The app embeds the result and loads it — host.Load(UI.Bundle()) —
// so the shipped binary reads no scripts from disk; and it imports
// neither the tool nor gpui/script/compile, so esbuild never enters
// the release import graph (go list -deps proves it). A compile error
// fails the generate step with the .tsx file and line, so a build
// that runs it fails there, not at run time. examples/tsx-hello is
// the sample app in this shape, with a headless -selftest mode (the
// release check: load the embedded bundle, render one frame in an
// offscreen test window, exercise one click, exit 0).
//
// Staleness is the one gap go:generate leaves: go build does not
// rerun the generator, so an edited .tsx does not fail the build by
// itself. The generated test closes it: it hashes the sources the
// bundle was compiled from and fails when one changed, before a
// release ships the old UI (EmbeddedBundle.CheckFresh). Regeneration
// is idempotent, so committing the generated files shows no churn.
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
// time it renders, so state changes and reloads redraw. It also wires
// the host's scheduler to the window's application on first render
// (AppScheduler), so the host APIs below work without explicit setup
// when the host is mounted; WithScheduler covers windowless hosts and
// tests. Host is not safe for concurrent use: call Load, Reload and
// handlers from the thread that renders (a watcher must marshal its
// notification to the app thread, e.g. with app.Foreground().Spawn and
// AsyncApp.Update).
//
// # The host's browser globals (ticket 64)
//
// Scripts get the browser-ish globals goja lacks, because browser- and
// LLM-trained code assumes them. Provided:
//
//	setTimeout / clearTimeout / setInterval / clearInterval   timers
//	queueMicrotask(fn)                    microtasks (before timers)
//	requestAnimationFrame / cancelAnimationFrame
//	                                      runs at the next frame request,
//	                                      before that frame paints
//	fetch(url, opts) → Promise<Response>  { method, headers, body };
//	                                      Response: status, ok, statusText,
//	                                      headers (lower-cased), url,
//	                                      text(), json()
//	console.log/info/debug/warn/error/trace/assert
//	performance.now()                     the scheduler's clock
//	atob / btoa                            base64 (whitespace ignored,
//	                                      padding tolerated)
//	TextEncoder (UTF-8) / TextDecoder (UTF-8, fatal option)
//	URL / URLSearchParams                 parsed by net/url
//
// Timers, fetch and requestAnimationFrame need the scheduler; without
// one the host raises a named error that says how to fix it (see
// WithScheduler). State values are snapshots, like zgui's: get returns
// a copy, so a change is a new value passed to set (arrays:
// set(a.concat([x])), not a.push).
//
// Divergences from a browser, on purpose or by scope:
//   - fetch rejects non-http(s) and relative URLs (there is no base
//     URL: no location); the default transport is the host's plain
//     net/http client (no cookies or credentials — the app installs
//     its own with WithFetcher, and ticket 67's plugins get a granted
//     one); fetch bodies are strings or JSON objects (typed-array
//     bodies are not provided yet).
//   - URL has no setters (parse, read, stringify); URL serialization
//     is net/url's, not WHATWG's (no IDNA, no dot-segment
//     normalization beyond net/url's, no IPv6 zones).
//   - URLSearchParams follows WHATWG: '+' is a space, insertion order
//     is kept in toString, init from a string, pairs or an object.
//   - TextDecoder supports the UTF-8 labels only; a missing label is
//     a named error (the WHATWG encoding list needs CLDR-scale data).
//     The stream option is not provided (the body is already in
//     memory).
//   - console formats like a devtools console (depth-2 truncation,
//     %s/%d/%f/%j/%o directives) and goes to the app's stderr, or the
//     app's own sink (WithConsoleSink).
//   - No Intl, crypto, location, navigator, document or window: the
//     host is not a page. Intl needs CLDR/ICU-scale data for real
//     locale formatting; nothing in the surface needs it yet (the ADR
//     named it as an example of a host API, not a requirement — a
//     ticket can add the pieces that matter).
//
// # The Go bridge: actions, shared state, fetch
//
// Scripts reach Go through three seams (zgui's model):
//
//	// a quick action, runs inline in the script call
//	host.Action("note.add", func(args []any) (any, error) {
//		return len(args), nil
//	})
//	// a slow one, runs off the host thread; its promise settles from
//	// the host thread; ctx is cancelled by a reload
//	host.AsyncAction("fs.read", func(ctx context.Context, args []any) (any, error) {
//		return os.ReadFile(args[0].(string))
//	})
//
// The script calls them:
//
//	const n = invoke("note.add", "one", 2)      // sync: a value, or an exception
//	const p = call("fs.read", "notes.txt")      // a promise (both kinds)
//	const text = await call("fs.read", "a.txt")  // goja runs async/await
//
// Arguments and return values are plain Go values as goja exported
// them — nil, bool, string, float64/int64, []any, map[string]any
// (JSON-shaped). An action's error surfaces as an exception (invoke)
// or a rejection (call) that names the action. A missing action is a
// named error at call time. WithActionResolver replaces how names
// resolve: ticket 67's plugin hosts answer only their granted names,
// with a named missing-grant error.
//
// Shared state is the app's store, keyed like the script's own state:
//
//	store := script.NewShared()
//	host := script.New(script.WithShared(store))
//	store.Write("note", "hello")     // Go writes: the script's bindings on
//	                                 // "note" recompute, no re-describe, and
//	                                 // the window redraws
//	v, _ := store.Read("note")        // Go reads what the script wrote
//
// The script reads and writes the same store through shared(key, init)
// — the same handle shape state has ({ key, get, set }), so
// bind(shared("note"), fn) updates only that value when either side
// writes. Shared values live in the store, so they survive reloads and
// outlive the host; several hosts can share one store. Shared keys and
// state keys are distinct namespaces (a state("x") and a shared("x")
// are two values). The SharedStore interface is the seam ticket 67's
// plugin gates wrap (a gated view of the app's store, answering a
// named missing-grant error for keys the plugin was not granted).
//
// # The threading model
//
// goja is single-threaded: all script runs on the thread that renders
// (the app's foreground executor). Nothing runs JS from another
// goroutine:
//
//   - Timers ride the app's task machinery (Scheduler.After →
//     SpawnDelivering + TaskRun.Sleep): the worker parks immediately,
//     the delivery runs the callback on the host thread. Equal
//     deadlines fire in registration order (the scheduler's FIFO).
//   - fetch and async actions run their blocking work on goroutines
//     the scheduler owns (Scheduler.Run) — never a scheduler task,
//     whose body the dispatcher drives inline to its first park point
//     (an HTTP round trip must not park the UI) — and settle their
//     promise by marshalling back through Scheduler.Post. A goroutine
//     never touches the VM: it computes and returns.
//   - Microtasks (queueMicrotask, promise reactions, async/await
//     continuations) drain when the current Go→JS call returns — goja
//     does it, exactly the browser's “after the current task”.
//   - Under TestApp the clock is virtual: AdvanceClock fires timers
//     and their continuations synchronously, so tests are
//     deterministic. requestAnimationFrame callbacks run at the
//     window's next frame request (Window.OnNextFrame) before that
//     frame paints.
//
// AppScheduler adapts a gpui application; the Scheduler interface is
// the seam for anything else. A host without a scheduler still loads
// and runs handlers.
//
// # Reload: what dies, what stays
//
// A successful reload builds a fresh VM, so everything the old VM had
// pending dies with it, while the values stay:
//
//   - timers and intervals are cancelled (no callback into a dead VM
//     — the browser's navigation analogue);
//   - in-flight fetches and async actions are aborted (their context
//     is cancelled, their promises never settle, like a navigation
//     aborting fetches); a completion that still lands is dropped;
//   - requestAnimationFrame callbacks pending when the reload landed
//     are dropped;
//   - keyed state (the script's own) and the shared store keep their
//     values — init matters only for keys that did not exist;
//   - unhandled promise rejections of the old VM are dropped.
//
// A failed reload keeps the old VM with its tree, timers and
// completions running (the last good UI still works).
//
// # Type checking: the generated .d.ts and tsc (ticket 63)
//
// The whole authoring surface above — the elements, the style
// object's CSS properties, the events, the host globals and the
// Tailwind class surface — is also a TypeScript declaration file,
// generated from the engine's own source (the host's switches and
// the class layer's generated tables) and committed at
// gpui/script/types/gpui-script.d.ts. The drift tests in
// internal/tsxgen regenerate it and compare it against the engine in
// both directions (everything the engine accepts is declared;
// everything declared is accepted — classes verified against
// gpui.CheckClasses), so the types cannot silently drift from the
// engine; regenerate with:
//
//	go generate ./gpui/script/types
//
// Write the declarations beside your sources so editors see them:
//
//	go run github.com/hoangvu12/gpui-go/cmd/tsxcheck -init ui
//
// and type-check a view (tsc, found locally in node_modules, or
// pinned with GPUI_TYPESCRIPT — nothing is installed for you):
//
//	go run github.com/hoangvu12/gpui-go/cmd/tsxcheck ui
//
// A dev host runs the same check after every reload when a checker is
// installed (ADR 0003: debug builds check after each reload when tsc
// is present) — reported through the WithTypeDiagnostics hook, never
// gating the running UI (types are advisory to esbuild; the engine's
// own load-time checks stay the source of truth):
//
//	source, _ := compile.Open("ui/app.tsx")
//	host := script.New(
//		script.WithSource(source),
//		script.WithTypeCheck(types.NewChecker("ui")),
//		script.WithTypeDiagnostics(func(diags []script.TypeDiagnostic) {
//			for _, d := range diags { fmt.Println(d) }
//		}))
//
// className stays a string in the types: a class list is
// space-separated and open-ended, and the engine validates every
// class at load with the .tsx file and line (the same check
// Styled.Class runs). The generated Class type is the single-class
// surface — every palette color, scale value, bracketed length and
// opacity modifier — for where a single class is built.
//
// # Later tickets (not built here)
//
// The dev command (Go rebuild
// and restart with state restore); plugin slots, contributions and
// capability grants (each plugin in its own goja runtime with a time
// budget) — the ActionResolver and SharedStore seams above are the
// gates those wrap. The SetGlobal mechanism and this package's public
// surface are the seam those build on.
package script
