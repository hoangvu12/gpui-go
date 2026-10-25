package base

// Window roots and presentation-layer plugins (U:root.rs).
//
// Go adaptations and deviations:
//
//   - browser-core 73: the !mac root copy binding (U:root.rs:17-21) is
//     gone — the core's copy command (browser-core 26) runs from the
//     keydown's default-action arm on the window selection's stand-in
//     handler (gpui/text_selection_copy.go), so ctrl-c copies the
//     window selection with nothing editable focused. The macOS cmd-c
//     row stays bound (the core's key table carries only the !Mac
//     rows, editing_behavior.cc:168-178), and on_action_copy (and the
//     Root's OnAction listener) stays for programmatic dispatch of the
//     aliased action: the write is the window selection's text — now
//     untrimmed, the core's Chrome rule (the selection is copied
//     exactly), where the pinned U:root.rs:264-273 trimmed it.
//   - install_window_hit_test_forwarder (U:root.rs:148-149) is macOS-only.
//   - browser-core ticket 72: the Root's Tab and Shift-Tab bindings and
//     the focus-trap cycling (U:root.rs:194-262) are gone with the focus
//     trap (focus_trap.rs, deleted): Tab is the core's default action
//     (browser-core 14 — ticket 22's Escape rule and 23's modal inertness
//     both reach it), and a modal dialog's inert outside keeps Tab inside
//     the dialog, the browser's answer to the trap. The ticket-73 survey
//     of what else binds Tab found only the input/editor's indent rows
//     (input_state.go, the tab-capture rule) and the time field's
//     segment rows (time_field.go) — dock, lists and the other widgets
//     bind none, so the core's default action drives them.

import (
	"reflect"
	"runtime"
	"strings"

	"github.com/hoangvu12/gpui-go/gpui"
)

// rootContext is the root key context (U:root.rs:12).
const rootContext = "Root"

// rootInit binds the root keys (U:root.rs:14-23): the platform's copy
// keystroke for input::Copy on macOS only (browser-core 73: off macOS
// the core's copy command owns the keystroke, so nothing is bound).
// Tab and Shift-Tab are not bound — the core's Tab default action owns
// focus movement (browser-core 14).
func rootInit(app *gpui.App) {
	if runtime.GOOS != "darwin" {
		return
	}
	ctx := rootContext
	app.BindKeys(
		gpui.NewKeyBinding("cmd-c", CopyAction.Box(Copy{}), &ctx),
	)
}

// RootPlugin is a presentation layer's retained, per-window facilities
// (U:root.rs:25-75). Register it with RootRegisterPlugin during explicit
// application initialization, before creating windows. Its Render output
// is mounted as an overlay above application content; plugins are
// processed in registration order, so later plugins appear above earlier
// ones. Notify the plugin entity after its state changes to render the
// root again.
//
// On every root render each plugin participates in three stages, each an
// optional interface (the Rust trait's default methods):
//
//  1. RootPluginPrepare synchronizes window state before elements are
//     built.
//  2. RootPluginStyle supplies defaults for the root surface.
//  3. RootPluginDecorate wraps the completed surface.
type RootPlugin[V any] interface {
	gpui.Render[V]
}

// RootPluginPrepare is RootPlugin::prepare (U:root.rs:47-52): synchronize
// settings derived from the plugin with the window (rem size, the active
// text selection scope). It must not notify: it runs during the root's
// render.
type RootPluginPrepare[V any] interface {
	Prepare(w *gpui.Window, cx *gpui.Context[V])
}

// RootPluginStyle is RootPlugin::style (U:root.rs:54-58): apply default
// styles directly to the root surface. Styles set on the Root instance are
// refined onto the surface afterwards and take precedence. It must not
// notify.
type RootPluginStyle interface {
	Style(surface *gpui.DivElement, w *gpui.Window, app *gpui.App)
}

// RootPluginDecorate is RootPlugin::decorate (U:root.rs:60-74): add
// presentation around the completed root surface (client-side window
// borders, a structural wrapper). Return surface unchanged when nothing is
// needed; root gives read-only access to the root and its view.
type RootPluginDecorate interface {
	Decorate(surface gpui.AnyElement, root *Root, w *gpui.Window, app *gpui.App) gpui.AnyElement
}

// pluginFactory builds one window's plugin (U:root.rs:77).
type pluginFactory func(w *gpui.Window, cx *gpui.Context[Root]) rootPlugin

// pluginRegistryEntry is one registered plugin type (U:root.rs:81-83).
type pluginRegistryEntry struct {
	typ     reflect.Type
	factory pluginFactory
}

// pluginRegistry is the plugin registry global, stored as
// *pluginRegistry (U:root.rs:81-83).
type pluginRegistry struct {
	entries []pluginRegistryEntry
}

// rootPlugin is one window's plugin (U:root.rs:84-89).
type rootPlugin struct {
	view     gpui.View
	prepare  func(w *gpui.Window, app *gpui.App)
	style    func(surface *gpui.DivElement, w *gpui.Window, app *gpui.App)
	decorate func(surface gpui.AnyElement, root *Root, w *gpui.Window, app *gpui.App) gpui.AnyElement
}

// Root is the window's content and overlay host, independent of any
// styled component library (U:root.rs:91-96). It is Styled: styles set on
// it are refined onto the root surface (U:root.rs:275-279). Build it in
// place with NewRoot.
type Root struct {
	gpui.Styled[*Root]
	view    gpui.View
	plugins []rootPlugin
}

// RootRegisterPlugin registers a presentation plugin once per application
// (U:root.rs:99-145 Root::register_plugin). Re-registering its type
// replaces the factory for future windows rather than mounting it twice.
// Factories are captured when a root is created, so registration affects
// only future windows. build initializes the plugin state in place (the
// Rust `fn(&mut Window, &mut Context<V>) -> V`).
func RootRegisterPlugin[V any, PV interface {
	*V
	RootPlugin[V]
}](app *gpui.App, build func(state *V, w *gpui.Window, cx *gpui.Context[V])) {
	if !gpui.HasGlobal[*pluginRegistry](app) {
		app.SetGlobal(&pluginRegistry{})
	}
	factory := func(w *gpui.Window, cx *gpui.Context[Root]) rootPlugin {
		// The plugin entity is owned by the window, like the Root that
		// owns it upstream (Root is the window's root view).
		entity := gpui.NewEntity(cx, w.Scope(), func(state *V, vcx *gpui.Context[V]) {
			build(state, w, vcx)
		})
		// Plugin construction can enqueue notifications. Observe only
		// after that effect cycle so mounting a Root does not immediately
		// render application content a second time (U:root.rs:111-120).
		cx.Defer(func(_ *Root, rcx *gpui.Context[Root]) {
			rcx.Observe(entity, func(_ *Root, _ gpui.Entity[V], rcx *gpui.Context[Root]) {
				rcx.Notify()
			})
		})
		return rootPlugin{
			view: gpui.ViewOfIn[V, PV](w.Scope(), entity),
			prepare: func(w *gpui.Window, app *gpui.App) {
				entity.Update(app, func(state *V, vcx *gpui.Context[V]) {
					if p, ok := any(PV(state)).(RootPluginPrepare[V]); ok {
						p.Prepare(w, vcx)
					}
				})
			},
			style: func(surface *gpui.DivElement, w *gpui.Window, app *gpui.App) {
				entity.Update(app, func(state *V, vcx *gpui.Context[V]) {
					if p, ok := any(PV(state)).(RootPluginStyle); ok {
						p.Style(surface, w, vcx.App())
					}
				})
			},
			decorate: func(surface gpui.AnyElement, root *Root, w *gpui.Window, app *gpui.App) gpui.AnyElement {
				return entity.UpdateWith(app, func(state *V, vcx *gpui.Context[V]) gpui.AnyElement {
					if p, ok := any(PV(state)).(RootPluginDecorate); ok {
						return p.Decorate(surface, root, w, vcx.App())
					}
					return surface
				})
			},
		}
	}
	registry := gpui.Global[*pluginRegistry](app)
	app.NotifyGlobal[*pluginRegistry]()
	typ := reflect.TypeFor[V]()
	for i := range registry.entries {
		if registry.entries[i].typ == typ {
			registry.entries[i].factory = factory
			return
		}
	}
	registry.entries = append(registry.entries, pluginRegistryEntry{typ: typ, factory: factory})
}

// NewRoot initializes root in place around the application view
// (U:root.rs:147-162 Root::new): every registered plugin is built for
// this window. Call it from the root entity's build:
//
//	ta.AddWindowView(func(r *base.Root, w *gpui.Window, cx *gpui.Context[base.Root]) {
//		base.NewRoot(r, view, w, cx)
//	})
func NewRoot(root *Root, view gpui.View, w *gpui.Window, cx *gpui.Context[Root]) {
	gpui.BindStyled(root)
	root.view = view
	var factories []pluginRegistryEntry
	if registry, ok := gpui.TryGlobal[*pluginRegistry](cx.App()); ok {
		factories = append(factories, registry.entries...)
	}
	root.plugins = make([]rootPlugin, 0, len(factories))
	for _, entry := range factories {
		root.plugins = append(root.plugins, entry.factory(w, cx))
	}
}

// View returns the original application content view (U:root.rs:164-167).
func (r *Root) View() gpui.View { return r.view }

// Plugin finds a presentation plugin owned by this window
// (U:root.rs:169-174).
func (r *Root) Plugin[V any]() (gpui.Entity[V], bool) {
	for _, plugin := range r.plugins {
		if entity, ok := gpui.DowncastView[V](plugin.view); ok {
			return entity, true
		}
	}
	return gpui.Entity[V]{}, false
}

// rootOf returns the window's Base Root (the shared body of Root::read and
// Root::update, U:root.rs:176-193).
func rootOf(w *gpui.Window) gpui.Entity[Root] {
	root, ok := w.Root[Root]()
	if !ok {
		panic("base: window must have a Base Root")
	}
	return root
}

// RootRead returns the window's Base Root; it panics when the window's
// root view is not a Root (U:root.rs:176-182 Root::read).
func RootRead(w *gpui.Window, app *gpui.App) *Root {
	return rootOf(w).Read(app, func(r *Root, _ *gpui.App) *Root { return r })
}

// RootUpdate updates the window's Base Root (U:root.rs:183-193
// Root::update); it panics when the window's root view is not a Root.
func RootUpdate[R any](w *gpui.Window, app *gpui.App, f func(root *Root, w *gpui.Window, cx *gpui.Context[Root]) R) R {
	return rootOf(w).UpdateWith(app, func(r *Root, cx *gpui.Context[Root]) R {
		return f(r, w, cx)
	})
}

// onActionCopy copies the window's text selection (U:root.rs:264-273):
// the selected text goes to the clipboard; with nothing selected the
// action propagates. The keystroke no longer dispatches it on !mac
// (the core's copy command does, untrimmed — see the file header); an
// app that binds or dispatches the aliased edit::Copy action still
// runs this handler. The write's error is dropped like the pinned
// write_to_clipboard, which logs it.
func (r *Root) onActionCopy(_ *Copy, w *gpui.Window, cx *gpui.Context[Root]) {
	text := strings.TrimSpace(TextSelectionSelectedText(w, cx.App()))
	if text == "" {
		cx.Propagate()
		return
	}
	_ = cx.App().WriteClipboard(gpui.NewClipboardStringItem(text))
}

// Render renders the root surface: the application view with every
// plugin's overlay above it, then plugin styles, the instance style and
// plugin decorations (U:root.rs:280-310).
func (r *Root) Render(w *gpui.Window, cx *gpui.Context[Root]) gpui.AnyElement {
	app := cx.App()
	for _, plugin := range r.plugins {
		plugin.prepare(w, app)
	}
	overlays := make([]any, 0, len(r.plugins))
	for _, plugin := range r.plugins {
		overlays = append(overlays, plugin.view)
	}
	content := gpui.Div().Flex().
		ID("root").
		KeyContext(rootContext).
		OnAction(CopyAction, cx.Listener(func(r *Root, a *Copy, w *gpui.Window, cx *gpui.Context[Root]) {
			r.onActionCopy(a, w, cx)
		})).
		Relative().
		SizeFull().
		Child(TextSelectionLayer{}).
		Child(r.view).
		Child(gpui.Div().Flex().Absolute().Inset0().Children(overlays...))
	for _, plugin := range r.plugins {
		plugin.style(content, w, app)
	}
	content.StyleMut().Refine(r.StyleMut())
	element := content.IntoElement()
	for _, plugin := range r.plugins {
		element = plugin.decorate(element, r, w, app)
	}
	return element
}
