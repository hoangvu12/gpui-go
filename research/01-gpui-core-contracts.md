# GPUI-CE core contracts for a faithful Go port

Research date: 2026-10-04. Status: source audit and design proposals; no implementation, build, or benchmark.

## Scope and evidence

The target is GPUI's programming model, fluent composition, state management, interaction behavior, and extensibility. Merely reproducing the appearance of `div().flex().child(...)` would not support faithful ports of Base, styled component libraries, docking, or editors.

The primary reference is **gpui-ce/gpui-ce at `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`**, confirmed as repository HEAD through `gh api repos/gpui-ce/gpui-ce/commits/HEAD` during this audit. Twenty-eight source files were retrieved using `gh api` with the raw-content Accept header and read locally. The [evidence manifest](../evidence/core/manifest.json) records original paths, pinned URLs, and SHA-256 hashes of local text snapshots. These are reading snapshots with newline normalization, not a vendored build. Approximately 1.58 MB was retained. No upstream runtime behavior was experimentally validated.

**Observed** below means visible in this source revision. **Proposed** means a port design or future verification requirement. An upstream behavior is not automatically an approved product requirement; differences must be decided explicitly.

## 1. The public programming model

**Observed.** GPUI separates several concepts that superficially look like interchangeable widgets:

| Concept | Actual responsibility | Port implication |
|---|---|---|
| `Entity<T>` | Typed identity/reference to application-owned persistent state | Keep state ownership separate from temporary elements |
| `Render` | Stateful view renders through `Window` and `Context<Self>` | Preserve view-local services and state access |
| `RenderOnce` | Consumed component recipe expands into elements | Permit lightweight reusable components without independent entities |
| `IntoElement` / `AnyElement` | Typed construction with dynamic composition | Heterogeneous children and custom components must compose |
| `Element` | Layout request, prepaint, paint, accessibility hooks | Retain a low-level escape hatch for editors and custom layouts |
| `Styled` | Utility-style methods refining per-element style | Components should participate in common fluent styling |

`ParentElement::child` accepts any `IntoElement`; conversion into `AnyElement` happens at the composition boundary. `Styled` methods return the implementing `Self`; the trait's only core requirement is access to `StyleRefinement`. This is why third-party controls can inherit a large fluent vocabulary without becoming subclasses of one widget class. [Element traits][element], [Styled trait][styled].

**Proposed.** Preserve these distinct roles in the compatibility specification. A Go component author should be able to implement a reusable recipe, opt into styles and children, and create a custom low-level element without exposing backend handles. Define which conveniences are generated and which are interfaces only after the minimum Go version is chosen. Current Go documentation is versioned and currently describes generic methods introduced with Go 1.27; do not base the design on an unversioned assertion that Go can never have generic methods. [Go method declarations][go-methods].

## 2. A complete frame is more than rendering a tree

**Observed.** `Window::draw` and `draw_roots` implement this sequence:

1. Enter an application-owned element-arena scope; consume dirty entity IDs; clear the accessed-entity set; reset the dirty flag.
2. Restore the previous platform input handler into the old frame so cache indices remain meaningful.
3. Construct the root view element and request layout. An automatic root dimension is expanded to the viewport.
4. Compute layout and prepaint, registering hitboxes and dispatch nodes. Prepaint deferred elements, prompts, drag previews, or tooltips.
5. Hit-test against the newly prepared frame, then paint the root, deferred content, and overlays.
6. Finish accessibility updates and install the chosen text input handler. Complete text-cache bookkeeping and finish the new frame.
7. Swap `next_frame` and `rendered_frame`, clear the reusable old-frame storage, and deliver focus-path changes.
8. Record entity dependencies, reset cursor state, and schedule another frame if focus listeners moved focus. Mark presentation necessary.
9. `present` separately passes the completed `Scene` to `PlatformWindow::draw`.

Sources: [`Window::draw`, `draw_roots`, `present`, `record_entities_accessed`][window].

The element trait exposes `RequestLayoutState` passed into subsequent stages and `PrepaintState` passed into paint. Those phase-local values are distinct from state explicitly retained across frames. `Element::id` establishes an identity path scoped under the nearest identified ancestor. `with_element_state` keys persistent state by **global identity plus state type**, taking it from the current or previous frame and recording that it was accessed. [Element lifecycle and identity][element], [`Window::with_element_state`][window].

**Proposed.** Maintain a frame transaction with persistent identity-based state and explicit temporary storage. Do not equate a new element value with a new component identity. Define disappearance/reappearance, sibling reordering, duplicate IDs, and window isolation. Clear pooled Go references deliberately so old closures and entity references do not remain reachable accidentally.

## 3. Entity lifetimes and safe mutation

**Observed.** The application owns entity data. A handle contains an ID/type and participates in separate reference counting. Cloning increments the count; dropping the last handle queues the ID for release. Weak upgrade only succeeds while the count is nonzero. The effect loop drains released entities, removes their observers/event listeners/invalidation associations, calls release callbacks with the entity still available, then allows state to be dropped. [Ownership guide][ownership], [`AnyEntity::clone`, `Drop`, `AnyWeakEntity::upgrade`, `EntityMap::take_dropped`][entity-map], [`App::release_dropped_entities`][app].

Mutable access is a lease: `EntityMap::lease` temporarily removes the value, invokes the update closure, and returns it with `end_lease`. Reading or updating the same entity during that lease fails loudly. Accesses also record render dependencies. `Entity::update` does **not** implicitly mean `notify`; changed state and observer notification are separate operations. [`AppContext for App::update_entity`][app], [`EntityMap::lease`, `read`][entity-map].

**Proposed.** This needs an explicit Go lifecycle decision before API approval. Ordinary Go assignment cannot reproduce Rust's automatic clone/drop accounting. Plausible designs include explicitly owned scopes with disposable registrations, or explicit retained handles with release operations. Neither is silently equivalent to RAII. GC reachability may manage Go memory, but it must not define when focus disappears, callbacks unsubscribe, native resources close, or weak handles become logically invalid. Go cleanup functions are not guaranteed to execute before program termination and are scheduled asynchronously. [Go cleanup API][go-cleanup].

Use application identity and generational entity IDs to reject stale/cross-application handles, a mutation guard for reentrant updates, and explicit posting of background results to the UI executor. These are proposals, not verified Go implementations.

## 4. Effects, observation, typed events, and cancellation

**Observed.** Nested application updates increment `pending_updates`. Only the outer update, when not already flushing, runs `flush_effects`. Effects can create further effects; flushing continues until the queue drains. Pending notifications are deduplicated by entity; typed emitted events are individually queued. Deduplication is a pending-queue property: after a notification is delivered and removed from the set, a subsequent notification can enqueue another delivery in the same overall cycle. [`App::start_update`, `finish_update`, `push_effect`, `flush_effects`, `apply_notify_effect`][app].

`observe` listens for notification, while `subscribe` filters typed emitted events. Registration activation is deferred through the effect queue. Entity-context observer and listener adapters generally capture weak handles, so registration need not keep an observing entity alive. `Context::listener` captures weakly, whereas `processor` and `on_next_frame` deliberately capture strong handles. These distinctions affect lifetimes. [Context adapters][context], [`new_observer`, `new_subscription`][app].

Dropping `Subscription` unsubscribes; `detach` disables that drop action and allows the registration to continue until its participating entities disappear. Joining subscriptions joins cancellation. Separately, dropping scheduler `Task<T>` cancels the task; detaching permits independent continuation. [Subscription implementation][subscription], [scheduler Task contract][scheduler].

**Proposed.** Specify ordering and cancellation, not just method names. A Go goroutine must not automatically replace a cancellable GPUI task. Define explicit cancellation ownership, late-result handling, shutdown, detach semantics, and how subscription cancellation during dispatch behaves. Preserve a distinction between coalesced state notifications and ordered typed events.

## 5. Invalidation and cached views

**Observed.** Entity reads are tracked for each window. Although invalidator associations can accumulate, `App::notify` filters them against the entities currently tracked by each window. An off-screen former reader should not keep being invalidated merely because it once read the entity. `Window::mark_view_dirty` marks the containing view ancestry using the rendered dispatch tree. [`AppContext::notify`, `record_entities_accessed`][app], [`WindowInvalidator::invalidate_view`, `mark_view_dirty`][window].

View caching is explicit. `Entity::cached(style)` requires definite sizing because cached layout comes from the supplied style, not content measurement. A cache hit checks bounds, content mask, text style, dirty-view membership, and global refreshing. It reuses prepaint and paint ranges and extends the accessed-entity set. [ViewElement implementation][view].

Reused artifacts include hitboxes, tooltips, dispatch subtrees and focus associations, deferred draws, cursor requests, mouse callbacks, input handlers, tab stops, text-layout references, and scene operations. A bitmap cache alone cannot preserve these semantics. [`reuse_prepaint`, `reuse_paint`][window].

**Proposed.** First define correct invalidation with caching disabled; then require cache hits to remain behaviorally equivalent. Keep performance fidelity separate from observable semantic fidelity. Build tests where a cached focused text input survives unrelated updates and remains editable; upstream comments document actual bugs caused by moving handler slots and invalidating cached indices.

## 6. Input routing, actions, focus, and key contexts

**Observed.** Mouse dispatch traverses registered listeners in capture order and reverse bubble order, with hit-testing deciding local behavior. Keyboard dispatch follows the focused dispatch-tree path; a dirty window can redraw before key dispatch so focus and handlers are current. Dispatch node IDs are explicitly frame-local. [`dispatch_mouse_event`, `dispatch_key_event`][window], [DispatchTree][key-dispatch].

Ordinary event handlers propagate by default. **Action bubble handlers stop propagation by default**, and must call `propagate` to continue upward. Action dispatch includes global capture, root-to-target capture, target-to-root bubble, then global bubble. [`App::stop_propagation`, `propagate`][app], [`dispatch_action_on_node_inner`][window].

Keybinding matching considers nested context stacks, with deeper applicable bindings taking precedence. Multi-stroke chords have pending and replay states. Context predicates support identifiers, equality/inequality, negation, conjunction/disjunction, and ancestry expressions; these are more expressive than one active-context string. Actions also have registered names and builders for parameterized JSON payloads. [KeyDispatch][key-dispatch], [context parser/evaluator][key-context], [Action and registry][action].

Focus handles have their own lifetime. Released focused handles trigger blur during effect flushing. Frame swaps compare focus paths, and focus listeners can themselves request a subsequent frame. [Focus release and propagation][app], [draw/focus processing][window].

**Proposed.** Preserve these defaults exactly unless the compatibility document declares a divergence. They are prerequisites for keyboard-accessible menus, dialogs, docking focus restoration, editor shortcuts, and overlapping popovers.

## 7. Layout, text, and IME are public behavior

**Observed.** `TaffyLayoutEngine` creates ordinary or measured nodes, computes layouts, tracks absolute bounds, and clears its tree at frame completion. Taffy's own rounding is disabled; GPUI applies device-pixel rounding and manages extra inline-box state. Root auto-sizing, rem conversion, scale factors, intrinsic measurement, and detached inline placement are explicit behaviors. Substituting a simpler Flex implementation would therefore be a compatibility change. [Taffy integration][taffy], [window layout entry points][window].

The platform text interface covers font resolution and metrics, glyph rasterization, text-document layout, and inline layout containing atomic element boxes. `LineLayoutCache` keys text, font size, styled runs, wrapping width and line clamp; it invalidates against font-generation changes and migrates used layouts between frames. [PlatformTextSystem][platform], [TextSystem][text], [LineLayoutCache][line-layout].

Text input is not equivalent to receiving characters. GPUI exposes selection, marked composition ranges, replacement, selection direction, text queries, bounds for candidate-window placement, and point-to-character mapping. Its IME boundary uses UTF-16 code-unit offsets; helpers convert to/from UTF-8 byte offsets. [InputHandler and UTF16Selection][platform], [EntityInputHandler and conversions][input].

**Proposed.** Preserve documented text units at API boundaries and distinguish byte, rune, grapheme, glyph, and UTF-16 offsets internally. A future comparison corpus should include Vietnamese combining marks, CJK composition, emoji surrogate pairs and joined sequences, Arabic/Hebrew runs, fallback fonts, wrapped selection, and selection crossing hard line breaks. Native shaping and IME behavior need platform testing; the core interface audit does not prove equivalent rendering across operating systems.

## 8. Scene, renderer, platform, and accessibility boundaries

**Observed.** `PlatformWindow` handles window state, DPI, input callbacks, frame scheduling, text-input sessions, drawing, and sprite atlases. `PlatformTextSystem` and `PlatformDispatcher` are separate interfaces. This is an existing separation useful for a port; the API still includes platform-specific behavior that needs a support matrix. [Platform traits][platform].

`Scene` stores ordered quads, shadows, paths, underlines, monochrome/subpixel/color sprites, surfaces, backdrop filters, and filter boundaries. `Scene::finish` compiles a shared `ScenePlan` with ordered commands and resource requirements. Deferred draws raise the ordering floor so overlays remain above prior content. The pinned implementation limits dedicated isolated filter nesting to two levels and falls back to inline rendering deeper down. [Scene][scene], [ScenePlan][scene-plan].

GPU buffer layout is explicit: `ShaderBool` has a four-byte representation, and `SCENE_BUFFER_LAYOUTS` records CPU sizes/field offsets consumed by shader tooling. Copying field names into Go structs does not establish binary compatibility. [Scene types][scene], [ABI metadata][scene-abi].

Accessibility is part of `Element`: role, hidden subtree handling, property writing, and synthetic children. Stable element IDs are required for tree inclusion under the documented hooks, and frame processing sends updates through the platform. [Element accessibility hooks][element], [`draw_roots`][window].

**Proposed.** Keep the GPUI-facing scene/platform boundary even if an existing Go library supplies GPU submission. Require an adapter to preserve clipping, ordering, glyph formats, filters, and native text/focus behavior. A renderer may differ internally without forcing callers into another toolkit's widget or layout model. Establish which features are required at each milestone; do not advertise partial filters or accessibility hooks as complete parity.

## 9. Allocation and scheduling constraints

**Observed.** `Arena` retains chunks, invokes destructors when clearing allocations, and tracks nested scopes. A nested draw must not clear storage referenced by an outer draw, a case explicitly associated with reentrant window procedures. [Arena implementation][arena].

**Proposed.** Prefer typed reusable slices/records and short-lived handles for frame data before considering unsafe arena replication. A frame reset must preserve objects referenced by rendered callbacks or cached artifacts. Measure allocation rate, heap retention, GC assist costs, and frame-time distribution; the Go GC guide explains why pointer-heavy retained graphs and allocation rate affect costs, but it supplies no GPUI-port performance prediction. [Go GC guide][go-gc].

**Observed.** Foreground tasks are scheduled onto the main thread; background futures require transferable results. The test dispatcher supplies a seeded scheduler with randomized ordering, virtual time advancement, and run-until-parked behavior. The test application simulates key, mouse, resize, clipboard, prompt, notification, and typed-event behavior. [Executors][executor], [test dispatcher][test-dispatcher], [test application][test-context].

**Proposed.** Make a deterministic host/test backend part of the core design. UI mutation should be serialized even though application work can use goroutines. A virtual clock and controllable executor make timer, tooltip, cancellation, and stale-result cases testable without sleeping.

## 10. Proposed port boundaries and conformance scenarios

Preserve the application/entity/effect core, element lifecycle, style semantics, focus/action routing, text/input contracts, and scene submission boundary. Translate backend internals independently. Keep advanced application facilities as packages above those contracts. Do not choose a foundational toolkit until representative Base/editor requirements have been traced through its adapter surface.

The following are **proposed future conformance cases**, not tests run in this research:

| Case | Observable requirement |
|---|---|
| Repeated notification in one update | Coalesces while pending; observers see completed mutation |
| Typed events during mutation | Queue preserves emissions; no callback borrows the currently leased entity |
| Subscription created/cancelled during callbacks | Activation/cancellation follows specified effect order |
| Last entity owner released | Weak access fails; release callbacks run once; registrations stop |
| Background completion after view disposal | Does not resurrect or mutate the disposed view |
| Stable-ID child reorder | Retains state by identity, without transferring it to another row |
| Cached text field and unrelated update | Preserves input handler, focus, hitboxes, tab order, and editing |
| Entity no longer displayed in a window | Old dependency does not keep requesting that window's redraw |
| Nested action handlers | Default bubble stop and explicit propagation match GPUI |
| Overlapping popovers and outside clicks | Capture/bubble, clipping, ordering, and focus restoration agree |
| Chord prefix followed by mismatch | Pending/replayed keystrokes follow documented policy |
| Fractional DPI plus measured text | Layout, hitboxes, clipping, and caret agree in device space |
| IME marked-text replacement | UTF-16 ranges and candidate bounds remain correct |
| Nested draw during window callback | Outer frame storage remains valid |
| Disabled/hidden/synthetic accessibility nodes | Semantic tree and action routing remain coherent |

Before implementation, resolve: the exact upstream compatibility baseline; Go version; entity/resource lifetime model; first supported platforms; required layout subset; rendering backend; native text strategy; accessibility coverage; and measurable development-time/storage targets. This audit supports the feasibility of a port, but does not estimate parity completion, certify a backend, or establish performance gains.

## Pinned source references

[element]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/element.rs
[styled]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/styled.rs
[window]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/window.rs
[view]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/view.rs
[ownership]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/_ownership_and_data_flow.rs
[entity-map]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/entity_map.rs
[app]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app.rs
[context]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/context.rs
[subscription]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/subscription.rs
[scheduler]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_scheduler/src/executor.rs
[key-dispatch]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/key_dispatch.rs
[key-context]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/keymap/context.rs
[action]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/action.rs
[taffy]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/taffy.rs
[platform]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/platform.rs
[text]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/text_system.rs
[line-layout]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/text_system/line_layout.rs
[input]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/input.rs
[scene]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/scene.rs
[scene-plan]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/scene/plan.rs
[scene-abi]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/scene/abi.rs
[arena]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/arena.rs
[executor]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/executor.rs
[test-dispatcher]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/platform/test/dispatcher.rs
[test-context]: https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/app/test_context.rs
[go-methods]: https://go.dev/ref/spec#Method_declarations
[go-cleanup]: https://pkg.go.dev/runtime#AddCleanup
[go-gc]: https://go.dev/doc/gc-guide
