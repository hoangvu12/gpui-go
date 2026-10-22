# Port GPUI Base to Go

Labels: wayfinder:map
Status: in progress (implementation authorized 2026-10-19)

## Destination

Port GPUI Base — `gpui-ce/gpui-component` @ `c08206932417f863062d2ab70cc2854c6ca04238`, `crates/base` (108,213 Rust lines, 1,226 tests) — to a Go package `github.com/hoangvu12/gpui-go/base` over the published core, pure Go, with the native artifact frozen at rev 11. End state: every Base module and test accounted for (ported / deviated / not applicable with reasons), a showcase example, tag v0.2.0. The styled `crates/component` (WebView, Icon, TitleBar, DataTable, Sidebar, …) is out of scope (see [the baseline](research/00-baseline.md)).

## Notes

- Baseline and pin rationale: [research/00-baseline.md](research/00-baseline.md); hashed source manifest: [evidence/base/source-manifest.json](../../evidence/base/source-manifest.json). Upstream sources on disk: `C:/Users/ADMIN/Desktop/nguyenvu/upstream-base/{ce,kit}`.
- Slice inventories (2026-10-19, two independent passes that agree): [A](research/A-infra-overlays.md)/[01](research/slice-01-infra-overlays.md) infra+overlays, [B1](research/B1-controls.md)/[02](research/slice-02-simple-controls.md) simple controls, [B2](research/B2-composite-controls.md)/[03](research/slice-03-composite-controls.md) composite controls, [C](research/C-scroll-lists-dock.md)/[04](research/slice-04-scroll-lists-dock.md) scroll/lists/dock, [D (partial)](research/D-input-editor.md)/[05](research/slice-05-input-editor.md) input/editor, [E1](research/E1-text-selection.md)/[06](research/slice-06-text-selection.md) text/selection, [E2](research/E2-plot.md)/[07](research/slice-07-plot.md) plot; dependencies: [deps-recommendation](research/deps-recommendation.md).
- Tracker conventions: [docs/agents/issue-tracker.md](../../docs/agents/issue-tracker.md). Implementation follows the core's wave pattern (claim → parallel background implementers with disjoint file ownership → orchestrator re-runs tests → `## Answer` + `evidence/base/ticketNN-*.json` → commit per batch on a working branch → merge to main → push).

## Gap map (merged findings)

### 1. The core is missing much of GPUI's authoring surface (Go-side, no native work)

The core tickets delivered the runtime, renderer, text, input plumbing and platform services, but Base exposed that the *element authoring surface* a component library stands on is largely absent. Verified by the orchestrator by grep on 2026-10-19:

| Gap | Evidence | Ticket |
|---|---|---|
| No automatic redraw: notify/refresh/animation-frame only set a flag; `DirtyWindows()` result discarded; no production InvalidateRect | `gpui/focus.go:525`, `gpui/app.go:501-506` | [03](issues/03-frame-loop.md) |
| Timers never fire in production (virtual clock only advances in tests) | `gpui/executor.go:44,475-491`, `gpui/testapp.go:54` | [04](issues/04-wall-clock-timers.md) |
| Styled vocabulary: 6 builders / 5 refinement fields; `Style` layout-only; div paints no radii/borders/shadow | `authoring/style.go:47-53`, `gpui/div.go:1655-1675` | [05](issues/05-styled-vocabulary.md) |
| `OnClick` listeners stored, never dispatched; no mouse-down/hover/drag-source/cursor | `gpui/div.go:620-622,944` | [06](issues/06-div-interactivity.md) |
| No div/list wheel scrolling; ScrollHandle metrics never written | `gpui/scroll.go:33-47`, `gpui/listelement.go:20-36` | [07](issues/07-scrolling.md) |
| External elements cannot paint (scene private); no Canvas/Deferred/Anchored/img/with_animation | `gpui/svgelement.go:383` is the only exported painter | [08](issues/08-element-paint-api.md) |
| Small helpers: keyed state, notify-by-id, TryGlobal, SubscribeIn/SpawnIn, focus observers, ids, Axis… | slice-01 §C7/C10, slice-03 C4-C16 | [09](issues/09-state-context-helpers.md) |
| No styled text runs/HighlightStyle/StyledText; Base targets the legacy text API (below) | `gpui/text.go:453`, `gpui/draw_text.go:734` | [10](issues/10-rich-text.md) |
| Div has no a11y role/state builders | `gpui/div.go:1289`, `gpui/accessibility.go:305-331` | [11](issues/11-a11y-authoring.md) |
| No test input simulation | `gpui/testapp.go:32-87` | [12](issues/12-test-harness.md) |

Consequence for the core record: the ticket-33 audit's "6911/6911 reconciled" assigned rows such as `gpui::color::linear_gradient` to family owners without implementing them (`conformance/ledger/generated/ledger.json` state `planned`). Tickets 03–12 are effectively core completion; their evidence should update the core ledger rows they cover.

### 2. Base targets a different text API than our pin

Base resolves `gpui-ce 0.2.2` **from crates.io** (`upstream-base/ce/Cargo.lock:3057-3060`), which keeps Zed's legacy text system (`LineWrapper`/`LineFragment`/`Boundary`, `ShapedLine::x_for_index`, 4-arg `shape_line`, `layout_width`). Our pin 254b5dbd carries the same version string but replaced that API with the Parley backend. The baseline's "same API line" holds for everything except text. Resolution: a Go-side legacy facade over `ShapeText` ([10](issues/10-rich-text.md)); geometry in input/text is re-expressed, not transliterated.

**2b. Default display.** The same crates.io 0.2.2 line defaults `Style::default().display` to `Block` (`~/.cargo/git/checkouts/gpui-ce-6758b1db058c6497/beb5a6d/crates/gpui/src/style.rs:971`); our pin defaults to `Flex` (`reference/ce-source/crates/gpui/src/style.rs:1024-1027`). Upstream layouts and tests that rely on a plain div stacking as a block differ under the core; found by ticket 13 (styled cross-axis test adapted). Component tickets record each affected site. Convention (ticket 17): where upstream's base element is a plain `div()`, the Go constructor calls `.Block()` before the caller's style, so upstream layout holds and a caller's `.Flex()` still wins.

### 3. Native-artifact gaps (frozen rev 11) → decision [02](issues/02-native-gap-policy.md)

| Gap | Evidence | Proposed Go-side fallback |
|---|---|---|
| A11y ABI: 8 role tags, no toggled/selected/expanded/numeric/level/set/row/col state; an unknown role fails the whole publish | `reference/native/src/a11y.rs:598-612,816-870`; `internal/native/a11y.go:84-127`; `gpui/a11yprovider.go:545-566,673-676` | Full semantics in the Go snapshot (what upstream tests read); publish nearest supported role, drop states — screen readers get less |
| Per-run text color/decoration not in the text ABI; no glyph→byte index; no strikeout metric | `reference/native/src/text.rs:327,518-553,1099-1109` | Shape once, paint per run clipped to `SelectionRects`; ligature across a color boundary takes one color; derived strikethrough offset |
| Gradient/pattern backgrounds (scene color tag 0 only) | `reference/native/src/scene.rs:164-181,2506` | Solid only; Base only mentions gradients in a doc comment (`plot/shape/bar.rs:145`) |

Everything else (quads with radii/borders, shadows, layers, paths, cursor, pointer capture, timers, redraw, IME, clipboard, images, SVG) is available natively or is a Go/user32 change.

### 4. Per-area classification

| Area | Rust lines / tests | Pure Go today | Needs core tickets | Native |
|---|---|---|---|---|
| Infra + overlays + motion | 12.2k / 147 | positioner math, history/undo, geometry, index_path, theme tokens | 03 04 05 06 08 09 11 | a11y roles |
| Simple controls | 8.0k / 88 | collapsible | 05 06 08 09 11 | a11y roles/states |
| Composite controls | 9.6k / 70 | combobox, nav_stack (mostly), questionnaire state | 05 06 09 11 + resizable: cursor/drag/canvas | a11y roles/states |
| Scroll / lists / dock | 16.5k / 236 | auto_scroll, dock model (~4.4k / 89 tests) | 05 06 07 08 09 | none |
| Input / editor | 28.2k / 294 | rope_ext, cursor/selection/undo, mask_pattern, search, diagnostics, LSP data | 09 10 12 | none (text fallbacks) |
| Text / selection | 28.2k / 329 | HTML minifier walk, markdown→node adapter logic | 08 10 + parsers (01) | per-run paint fallback |
| Plot | 6.0k / 67 | scales, stack, pie, sankey (44 tests) | 08 09 10 | none (gradient fill unused) |

## Ticket waves

| Wave | Tickets | Parallelism |
|---|---|---|
| 0 — decisions (user) | [01 dependencies](issues/01-dependency-policy.md), [02 native gaps](issues/02-native-gap-policy.md) | — |
| 1 — core completion (`gpui/`) | [35 inline runs](issues/35-inline-styled-runs.md) (follow-up of 10); [03](issues/03-frame-loop.md) [04](issues/04-wall-clock-timers.md) [05](issues/05-styled-vocabulary.md) [09](issues/09-state-context-helpers.md) → [06](issues/06-div-interactivity.md) [07](issues/07-scrolling.md) [08](issues/08-element-paint-api.md) → [10](issues/10-rich-text.md) [11](issues/11-a11y-authoring.md) [12](issues/12-test-harness.md) | 4 → 3 → 3 workers |
| 2 — Base foundations (`base/`) | [13 infra](issues/13-base-infra.md), [14 motion](issues/14-motion.md), [15 data structures](issues/15-data-structures.md) | 13 ∥ 15, then 14 |
| 3 — overlays, controls, scroll | [16](issues/16-overlays.md) [17](issues/17-simple-controls.md) [18](issues/18-scroll-family.md), [19 input data](issues/19-input-data-layer.md), [31 plot](issues/31-plot.md) | 5 workers |
| 4 — input + text | [20](issues/20-single-line-input.md) → [21](issues/21-textarea.md) [22](issues/22-input-controls.md); [27 markdown](issues/27-markdown-parse.md) → [28 TextView](issues/28-text-view.md) | 2 chains |
| 5 — composites, dock, editor, text tail | [23](issues/23-editor-basics.md) → [24](issues/24-diagnostics-lsp.md); [25](issues/25-composite-controls.md) → [26 dock](issues/26-dock.md); [29](issues/29-text-selection.md) [30](issues/30-text-extras.md) | 3 chains |
| 6 — release | [32 audit + showcase + v0.2.0](issues/32-showcase-audit-release.md) | — |

Blocking edges live in each ticket's `Blocked by:`; the frontier is computed from them.

## Decisions so far

- Pin: `gpui-ce/gpui-component` c0820693 `crates/base` (CE rebase written for the gpui-ce 0.2.2 line); Kit 4c7f1350 comparison only — [baseline](research/00-baseline.md).
- [Third-party dependency policy](issues/01-dependency-policy.md) — goldmark, uax29, x/net/html as modules; rope, sum tree, lsp types, strftime, search, data-url recreated; stdlib for the rest.
- [Frozen-artifact gaps](issues/02-native-gap-policy.md) — Go-side fallbacks: a11y nearest-role publication, per-run paint clipped by selection rects, solid backgrounds; recorded deviations, no native revision.
- [Automatic redraw](issues/03-frame-loop.md) — notify/refresh/animation frames wake and redraw; default-on since [33](issues/33-frame-loop-default.md) (upstream draw-on-flush; corpora migrated).
- [Production timers](issues/04-wall-clock-timers.md) — wall clock on the real host via an AfterFunc-armed wake; TestApp stays virtual.
- [State and context helpers](issues/09-state-context-helpers.md) — keyed state, globals, notify-by-id, window-bound context, focus/activation observers, ids, geometry.
- [Styled vocabulary](issues/05-styled-vocabulary.md) — full pinned Styled surface (generated families via cmd/stylegen), refinements, FluentBuilder, Component mixins; div paints every box style.
- [Div interactivity](issues/06-div-interactivity.md) — pinned click/hover/drag/state styles/cursor/capture; real-window mouse routing found missing → [34](issues/34-real-window-input.md).
- [Scrolling](issues/07-scrolling.md) — div/uniform-list/list wheel scrolling with pinned clamping and scrollbar metrics; real-window wheel via 34.
- [A11y authoring](issues/11-a11y-authoring.md) — pinned div aria builders, 27 roles, snapshot states; nearest-role publication per decision 02.
- [Test harness — Base-shaped simulate helpers (keystrokes, mouse, drag, a11y node, ComputeStyle)](issues/12-test-harness.md)
- [Real-window input — button/move/wheel messages routed into element dispatch](issues/34-real-window-input.md)
- [Element paint API](issues/08-element-paint-api.md) — external painting, Canvas, Deferred, Anchored, Img, animation element, drag preview.
- [Rich text](issues/10-rich-text.md) — per-run paint fields, HighlightStyle, StyledText/TextLayout, the legacy ShapeLine/LineWrapper facade over ShapeText; ligatures split colors inside the glyph; inline paragraphs → [35](issues/35-inline-styled-runs.md).
- [Inline styled runs](issues/35-inline-styled-runs.md) — StyledText and Text keep per-run colors/decorations inside inline paragraphs.
- [Base infra](issues/13-base-infra.md) — `base` package: Init, Theme + shadcn tokens, StyledExt, StateStyle, Root + plugins, GlobalState, reduce motion, History/UndoHistory; 59 tests.
- [Data structures](issues/15-data-structures.md) — base/rope, base/sumtree, base/segment (uax29 v2.7.0 enters go.mod), base/dataurl, base/strftime; 57 tests.
- [Motion](issues/14-motion.md) — easings, timing, keyframes, transitions, springs, presence, sequence, reveal in the root base package; 48 upstream tests on the virtual clock; f64 duration scaling deviation.
- [Input data layer](issues/19-input-data-layer.md) — RopeExt, InputPoint/InputPosition/InputSelection, cursor, change, undo manager, mask patterns; 42 upstream tests.
- [Simple controls](issues/17-simple-controls.md) — Button pattern + 16 controls (toggle, checkbox, radio, switch, link, tabs, toolbar, accordion, collapsible, avatar, progress, slider, pagination, table); 78 upstream tests; `.Block()` convention for gap 2b.
- [Overlays](issues/16-overlays.md) — focus trap, positioner, popup, popover, tooltip, hover card, dialog, alert dialog, sheet, toast; 53 upstream tests; Init wires the overlay initializers.
- [Scroll family](issues/18-scroll-family.md) — scrollbar, scroll bounce, scrollable mask, auto scroll, virtual list; 83 upstream tests; VirtualList content-slot deviation.
- Package layout (orchestrator, routine): one `base` package mirroring the single crate; dependency-free data structures (rope, sum tree, segmentation) in subpackages under `base/`.

## Not yet specified

- none (implementation authorized 2026-10-19: "sounds good then")

## Out of scope

- The styled `crates/component` and `crates/webview` (WebView, Icon, TitleBar, DataTable, Sidebar, Menu, Notification, …) — a later "Component" effort.
- `macos_accessibility.rs`, objc2/ashpd paths (macOS/Linux only); syntect (used only by the upstream showcase, not by Base).
