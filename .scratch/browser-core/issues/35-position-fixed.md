# 35 - position: fixed

Parent: ../map.md
Spec: ../../browser-parity/spec.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: orchestrator 9 (Claude Code)
Blocked by: 33, 21
Triage: ready-for-agent

**What to build:** Fixed elements are placed relative to the window and painted above normal flow, regardless of their ancestors.

**Blocked by:** 33 (position: static (opt-in), containing block and lint), 21 (Top layer and light dismiss)

**Status:** ready-for-agent

- [x] Fixtures with fixed inside scrolled and transformed-free ancestors match Chrome.

Every behaviour check is a browser-parity harness fixture (an HTML page recorded in headless Chrome plus the equivalent gpui-go view), unless stated otherwise.

## Comments

- 2026-10-25, worker (core/22-35-escape-fixed): implemented test-first, headless only, one commit (ticket 22 is the other half of the bundle, cf2b1ac). The Chrome record matched the predicted geometry exactly (fixed-a (140,60) through the 120px scroll, fixed-b (300,300) out of the relative+static chain, fixed-c (470,150) over the later sibling). Status left as claimed for the orchestrator's verification.

- 2026-10-24, orchestrator 9 (verification): merged with 22 as e845d10. Spot check: `ContainerForFixedPosition` → `CanContainFixedPositionObjects` (layout_object.cc:1885-1892) matches. fixed-window passes Tier A and C on the merged tree, and the touched packages and ./internal/parity/... pass. Resolved.

## Answer

- **API:** `PositionFixed` (gpui/style.go, enum + `String`/`ParsePosition`) + `Fixed()` (gpui/styled.go, hand-written like Static/Sticky — the pinned styles.rs has no fixed variant) + the Tailwind `fixed` class (regenerated `gpui/class_generated.go`, removed from class.go's unsupported list; the TSX types regenerated — `fixed` in CoreClass/Class).
- **Layout:** the record maps Fixed onto Taffy's Absolute with the insets kept (gpui/layout.go positionToRecord); the containing block is decided Go-side — `gpui/containingblock.go`'s fixed split: every container (static, relative, absolute, sticky, custom, a TopLayer entry) defers a fixed child into the ROOT scope (`deferFixedChild`, scope[0] = the root's, the port's initial-containing-block stand-in; a fixed element opens its own scope and IS a containing block for absolute descendants). Fixed children are out of flow for the scroller range (`scrollTrailingMargins` skips them).
- **Paint:** `gpui/fixed.go` — the fixed div's whole draw defers at `fixedDrawPriority` (0) under a ZEROED element offset, so it paints above the whole in-flow scene (the deferred draws paint after the main scene), is not scrolled or clipped by any ancestor scroller, and keeps its hitbox on top; explicitly deferred surfaces (Base popups 100, tooltips 200, the top layer 1<<20) still paint above it; equal priorities keep tree order.
- **Files:** gpui/style.go (PositionFixed), gpui/styled.go (Fixed), gpui/layout.go (positionToRecord, scrollTrailingMargins), gpui/containingblock.go (the fixed split + deferFixedChild + the root-scope attach), gpui/element.go (frameDrawState.fixedRequested — the scan runs only in frames that requested a fixed node), gpui/div.go (IntoElement wraps the fixed div), gpui/fixed.go (the deferring drawable), gpui/class.go + gpui/class_generated.go + gpui/script/types/gpui-script.d.ts (the class), gpui/fixed_test.go (6 unit tests) + gpui/containingblock_test.go (the enum surface), internal/parity/fixtures/fixed/ + the record in internal/parity/testdata/fixed-window/, registered in internal/parity/replay_test.go and cmd/parityrecord/main.go, docs/browser-parity-harness.md.
- **Fixtures:** `fixed-window` — a fixed box inside a scroller scrolled 120 by three keyboard line steps stays at (140,60) straddling the scroller's right edge (the clip escape) while the content moves under it; a fixed box inside a relative ancestor's static parent sits at (300,300); a fixed box among a flow column's children paints above a later in-flow sibling at (470,150). Tier A exact (14 boxes), Tier C 0.0 (flat colors), Tier B empty (no listeners). The wrong view (`WrongAbsolute`, the three boxes as plain absolute — the pre-ticket pin) fails Tier A on exactly the three moved boxes and Tier C on their pixels.
- **Key citations:** `core/layout/layout_object.cc:1885-1892` (`ContainerForFixedPosition` walks PAST every positioned ancestor — the fixed containing-block predicate is `CanContainFixedPositionObjects`, not `!static`), `:2009-2067` (`ComputeIsFixedContainer` — the LayoutView always, plus transforms, filters, backdrop-filter, containment, will-change; the port has no transforms so the window root is always the one), `:2069-2081` (`ComputeIsAbsoluteContainer` — a fixed container is an absolute container too, so a fixed element IS a containing block for absolute descendants); css-position-3 §3.3 (the viewport as the fixed containing block) and css2.1 Appendix E step 8 (a fixed box paints as a positioned element of the root stacking context — above all in-flow content); css-overflow-1 §2 (scroll offsets and clips accumulate along the containing-block chain, which skips the fixed element's scroller ancestors).
- **Limitations:** a transformed/filtered/containment ancestor does not capture fixed descendants (the port has no transforms, filters or containment yet — the viewport is always the containing block; noted per the task); a fixed element whose ROOT does not participate (a non-div root that walks children outside RequestContainerLayout) falls back to the direct-parent attach (the ticket-33 documented approximation) or defers to the outermost participating scope; the paint deferral keys on the AUTHORED position (a state style setting position would not defer — position is not a state style in practice); fixed boxes paint below every explicitly deferred surface and the top layer (the DOM's shape) and keep tree order among themselves (no z-index — a later positioned sibling in CSS would paint above an earlier fixed box); the sticky scroller scope is closed when a fixed element's deferred prepaint runs, so a sticky descendant of a fixed element resolves against no scroller unless the fixed element itself scrolls; the root's own padding shifts fixed children (the root stands in for the ICB, ticket 33's static-chain approximation).
