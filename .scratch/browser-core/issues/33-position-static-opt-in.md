# 33 - position: static (opt-in), containing block and lint

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: claimed
Assignee: orchestrator 7 (Claude Code)
Blocked by: 01
Triage: ready-for-agent

**What to build:** PositionStatic exists, and absolute elements resolve against the nearest positioned ancestor when their parents opt in; a lint reports absolute under a non-positioned parent. The default flips later (ticket 76) (D3).

**Blocked by:** 01 (Browser-parity harness (tracer))

**Status:** ready-for-agent

- [x] Fixtures with absolute inside static and relative ancestors match Chrome.
- [x] The lint flags a sample and passes on a corrected one.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-24, worker (core/33-position-static): implemented, test-first. The Chrome record was captured before the mechanism ran (the wrong view — every static parent left relative, which IS the pre-ticket pin — fails Tier A on exactly the five moved absolute boxes, so the record is a real gate), and the unit tests were written against the computed CSS geometry. The default does not flip: with no `Static()` anywhere the native tree is built exactly as before (empty deferred lists, identical child handles to CreateNode); `TestAbsoluteUnderDefaultParentKeepsPinBehaviour` and the fixture's control case pin it. One transient environmental failure during validation: `TestWin32TranslateKeyDownCarriesCodeAndLocation` read the physical Shift state while the user was typing (passes 3/3 isolated; passes at the base commit — keyboard state, not the diff).

## Answer

**Implemented on `core/33-position-static` (headless only; recorded with Chrome 155.0.8059.85; test-first: the record and the unit expectations were written before the containing-block mechanism ran).**

### The opt-in shape (and why)

**A `Position` value: `PositionStatic` (the enum's third value), not a Window/App flag.** D3's own text is "`position: static` as the default" — the CSS property value is the semantic unit, and containing-block relationships are **structural** (per-element-tree ancestry), not window-global: each parent opts its own absolute children out, so the opt-in must be per-element. A window flag would flip every element at once, which is ticket 76's default flip, not this opt-in. The value also falls out of the existing machinery for free: it refines like any style field (`Optional[Position]`), the class layer gets the Tailwind `static` class from the generated method switch, and the lint keys on the same value. Taffy 0.13 has no static value, so the ABI record maps it onto the in-flow `Relative` (`position_toRecord`, `gpui/layout.go:279-294`); the containing-block semantics are decided Go-side (below), never by the native layout service — **no native or DLL change was needed** (the README's "reparent in Go" is exactly implementable at the node-request seam, because the native ABI attaches children when the parent node is created).

### What changed (with file:line)

1. **`PositionStatic`** — `gpui/style.go:364-395` (enum value 2, doc comment; `String()`/`ParsePosition` accept "Static").
2. **Record translation** — `gpui/layout.go:279-294` `positionToRecord` (Static → Taffy `Relative`; the native `position_from` rejects unknown values, and the engine-level semantics stay "in flow") and `gpui/layout.go:313-320` (CSS static **ignores the insets**, so the record zeroes them — a deliberate CSS rule; no pin behaviour existed for the value).
3. **The containing-block scope walk** — new file `gpui/containingblock.go`:
   - `containingBlockScope` (`:66-72`), `openContainingBlock` (`:76-87`, positioned = relative/absolute today; sticky/fixed join with tickets 34/35), `closeContainingBlock` (`:90-105`), `deferAbsoluteChild` (`:107-119`), `requestLayoutNode` (`:140-177`).
   - Mechanism: a positioned element opens a scope around its child-request walk; a static parent's node request defers its absolute children into the innermost open scope instead of attaching them to itself; the scope owner's node request attaches them to ITSELF (after its element children), so they become Taffy children of the containing block and Taffy's absolute pass (`compute/flexbox.rs:2231-2339`: `inset_relative_size = container size − border`) resolves their insets against its **padding box**, exactly as CSS says. With no open scope, the deferred child falls back to the direct parent — the pin's behaviour — so non-participating containers keep today's semantics (documented limitation; see below).
   - The scope stack lives on the frame under construction: `gpui/element.go:1120-1127` (`frameDrawState.containingBlockScopes`), balanced per element (open before the child walk, close after the node request), dying with an abandoned frame.
4. **Integration** — `gpui/div.go:1574-1603` (the div's request opens its scope around the child walk and requests its node through `requestLayoutNode`; both the flex/grid and Block/Inline paths), `gpui/inline.go:751-777` (`requestInlineDivFrame` takes the scope, so static Block divs defer their absolute flow children too).
5. **Window seams** — `gpui/element.go:1486-1502` (`requestLayoutOf` is now the plain create: static styles still defer into the enclosing scope; every other style attaches children directly, never absorbing another element's deferred absolutes), `gpui/element.go:1765-1775` (`RequestElementLayout` unchanged for leaves) and the new public **`gpui.RequestContainerLayout`** (`gpui/element.go:1778-1802`): the author-facing seam for custom container elements to walk children inside their containing-block scope (the same path divs use). Anchored/Deferred keep the plain seam today (ticket 25 / 76 can adopt `RequestContainerLayout`).
6. **`Static()` fluent method** — `gpui/styled.go:264-281` (hand-written; the pinned styles.rs has no static variant, so cmd/stylegen does not emit it). **Tailwind `static` class** — `gpui/class_generated.go:773-774` (regenerated with `go run ./cmd/tailwindgen`; the class layer had the slot — the method-class switch) and the stale "static positioning is not supported" hint removed from `gpui/class.go` (`unsupportedClasses`).
7. **The lint** — new file `gpui/position_lint.go`: `LintAbsoluteUnderStatic(roots ...AnyElement) []PositionLint` (`:53-61`) walks the authored div tree (unwrapping a11y companions, `:160-180`) BEFORE any render and reports every absolute div whose direct parent is static, with the element path, the parent path, the containing block the runtime will actually use, and the fix ("call `.Relative()` on the parent if it should be the containing block"). No cmd integration: twcheck checks class *strings*, and element trees are built at render time, so the library function + tests are the natural shape.

### Fixtures (new package `internal/parity/fixtures/position`, registered in `internal/parity/replay_test.go` and `cmd/parityrecord/main.go`; record in `internal/parity/testdata/position-static/`)

- **`position-static`** (one page, three cases, text-free geometry → pixel-exact):
  - the escape case: a relative ancestor (200×160, border 6, padding 14 → padding box (6,6) 188×148) with a static 120×90 parent and three absolute children — left/top (recorded **(18,14)** = padding box + 12/8), right/bottom (**(128,108)** = padding box far edges − 16/10 − size), and percentage (**(53,80)** = padding box + 25%/50% — Chrome's record confirms the percentage basis is the containing block's **padding box**);
  - the control case: a relative parent (border 5, padding 10) with an absolute child at **(255,11)** — the pin's untouched direct-parent rule;
  - the static chain: 60×50 under 40×30 static divs whose absolute children escape to the initial containing block — the viewport in the page, the window root div in the view — at **(576,356)** and **(30,354)**.
  - Tier A: 12 boxes exact. Tier C: 0.0000 mean everywhere (correct view; gates 0.02/0.1/0.002 with the wrong view at 1.319 whole-image and 30-52 per moved box). Tier B: no instrumented events.
- **Wrong-view discrimination** (`internal/parity/fixtures/position/position_test.go`): the wrong view is the pre-ticket pin (every static parent left at the default relative), and it fails Tier A on exactly the five moved absolute boxes with readable diffs (`abs-tl: recorded (18,14) 40x30, replayed (32,28)` etc.) while the control case and every in-flow box still match; it also fails Tier C and keeps the transcript empty. That is the permanent regression gate.
- Unit coverage: `gpui/containingblock_test.go` (9 tests — the defer, far-edge and percentage insets, the static chain to the root, the untouched default, the Block path, in-flow + ignored insets, the root-static fallback, the enum/class surface) and `internal/layoutspec/position_static_test.go` (the engine-level record translation: in flow, insets ignored, direct-parent attach at the engine seam).

### Chromium citations (verified in %TEMP%\research-src\chromium, third_party/blink/renderer/core/layout)

- `layout_object.cc:1877-1883` `ContainerForAbsolutePosition` — the first ancestor with `CanContainAbsolutePositionObjects`, via `FindAncestorByPredicate`'s parent-chain walk (`layout_object.cc:192-207`).
- `layout_object.cc:2069-2081` `ComputeIsAbsoluteContainer` — **position != kStatic** (plus will-change/fixed-containment/fieldset cases) decides containership; the bit is set from style at `layout_box_model_object.cc:503-504` and read at `layout_object.h:2356-2360`.
- `layout_object.cc:2035-2040` `ComputeIsFixedContainer` — the LayoutView is always a container, the fallback when no positioned ancestor exists (the initial containing block).
- `out_of_flow_layout_part.cc:2574-2576` — "|inset| is relative to the container's **padding-box**", matching Taffy's `container size − border` basis.
- Spec: css-position-3 §2.1/§3.1 (def-cb/abs-cb); Taffy 0.13 `compute/flexbox.rs:2231-2339` (absolute children against the direct parent — the pin's rule).

### Intended divergence (toward CSS, noted in code)

- `gpui/layout.go:313-320`: static ignores insets (no pin behaviour existed for the value — recorded as the CSS rule, not a pin divergence).
- `gpui/containingblock.go` header: the whole containing-block rule is a deliberate divergence from the pin's direct-parent resolution, gated on the opt-in per D3.

### Known limitations

- **Non-participating containers:** a static div nested under `Anchored`, `Deferred` or a custom element that walks children outside `RequestContainerLayout` defers its absolute children to the nearest *participating* positioned ancestor (or attaches them directly when there is none) — the anchored node itself is skipped, where CSS would use it as the containing block. Anchored/Deferred keep the plain seam (ticket 25 owns `gpui/anchored.go` in parallel; ticket 76's default-flip sweeps every container to `RequestContainerLayout`).
- **A static ROOT** (the window's root element itself opts into static) falls back to the direct parent for its absolute children; the root plays the initial containing block only while positioned (the default). Ticket 76 revisits the root's ICB role when the default flips.
- The scroll-range union (`scrollContentSize`) still measures the element-tree children, so a scroller's absolute descendants resolving against an outer ancestor inflate its range (pre-existing, report 05 §2 / ticket 07 owns the range).
- `internal/portfixture` `TestFx0004` still fails on main (system font count 94 vs 95) — pre-existing, unrelated, untouched.
