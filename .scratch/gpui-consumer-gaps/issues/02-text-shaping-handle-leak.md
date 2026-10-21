# Stop Text elements leaking native shaping handles on every frame

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi worker
Blocked by: none

## Question

`textElement.RequestLayout` shapes a fresh `WrappedLine` (a native
shaping handle) every frame and never disposes or reuses it
(gpui/div.go: the measure closure calls `frameTextSystem(w).shape(...)`
into a new `shapedCell` per frame; `ShapeText` allocates a new native
handle each call, and only the failure path disposes). The native
service's live-handle bound is 256 (`MaxShapingHandles`). Any window
whose frames redraw with text — hover, notify, animation — exhausts the
bound and the frame loop dies in layout with
"gpui: text element shaping failed: ErrTextShapingLimit".

Verified from a consumer program (mordbard's readiness spike,
2026-10-19): 17 forced redraws of a 7-`Text` view (~119 new handles)
were enough — after that every further frame fails and a fresh
`ShapeText` itself returns the limit error. The reference (GPUI-CE)
caches shaped lines in the TextSystem (`shape_text` with a
shaped-line cache keyed by layout parameters) so redraws do not
re-shape.

## What to build

A cross-frame shaped-line cache in `TextSystem` (the reference's
shaped-line LRU semantics: same text, font, size, wrap width and line
clamp reuse the shaped line), or failing that, disposal of the replaced
line in the element path. The element path must reuse shaped results
when the inputs are unchanged across frames.

## Acceptance criteria

- [x] A real window with a `Text` element inside a continuous
      `WithAnimation` loop runs for at least 10,000 frames without
      `ErrTextShapingLimit` and without unbounded handle growth.
- [x] Scrolling a 300-row `UniformList` of text rows (rows entering and
      leaving the visible range) for thousands of frames stays under
      the live-handle bound.
- [x] Resizing a window with text (wrap widths changing) does not leak
      the superseded lines.

## Answer

Resolved with the reference's own mechanism, ported where the reference
keeps it: a per-window **line-layout cache** (the `LineLayoutCache` of
crates/gpui/src/text_system/line_layout.rs:925-1127) in the real window
text system seam — the layer the reference's `shape_text` resolves
through — so both real shaped paths reuse shaped lines across frames:
the text element's measure seam (`realWindowTextSystem.shape`) and the
inline paragraph's per-segment shaping (`shapeRuns`).

The port and its wiring (gpui/text_cache.go, div.go, inline_real.go,
render.go, element.go, testwindow.go):

- **Two-frame rotation**, exactly the reference's `layout_wrapped_line` /
  `finish_frame`: a lookup consults the current frame, then the previous
  frame (a hit moves the entry forward), and only a full miss shapes
  through the native seam; the window draw's tail (`completeFrame`, the
  port of window.rs:3230 `text_system().finish_frame()`) swaps the
  frames and releases the old previous frame's leftovers — the entries
  no frame requested. In Rust the dropped `Arc` dies there; the port
  disposes the native handle at that point, because a Go `*WrappedLine`
  has no refcount. A cached line survives at most one idle frame, like
  the reference's entries.
- **The key is the reference `CacheKey`** — text, font size, runs, wrap
  width, line clamp — in a Go-comparable shape (the run list
  length-prefix-encoded; floats by IEEE-754 bits).
- **Window teardown disposes the cache's lines** with the element
  runtime (the reference window's `Arc`-held lines die with its
  `WindowTextSystem`); the font-generation invalidation is ported with
  the generation read off the fresh layout (the ABI exposes it per
  layout, not as a query — recorded deviation).
- **The element path reuses shaped results under unchanged inputs**:
  within a frame through the existing `shapedCell` (the reference
  `TextLayout`'s four-condition check, unchanged), across frames through
  this cache. The public `TextSystem.ShapeText` keeps its uncached,
  caller-disposes contract (the mordbard hand-built-scene workaround is
  unaffected).
- The dead `inlineShapedLines` frame map (registered, never disposed)
  is removed — the cache now owns every real line's lifetime.
- **The same leak family in the input field's model**: `Editor`
  `currentDocument` now disposes the superseded document at its
  version-gated refresh (the reference drops the old `Arc`), so typing
  no longer accumulates one live handle per change.
- New observable: `TextSystem.LiveShapingHandles()` — the Go-side mirror
  of the native live-handle count (every handle flows through the Go
  adapter; the ABI exposes only its bound). Additive, no behavior change.

**Verification** (evidence: [consumer-gaps-02-text-shaping-cache.json](../../evidence/consumer-gaps-02-text-shaping-cache.json)):
internal/textframespec (new, real windows) proves the three criteria —
10,102 drawn animation frames with `LiveShapingHandles` flat at exactly
14 (the 7 texts' unwrapped and wrapped keys; 100 further frames create
zero new handles), 3,000 scrolled frames through the 300-row list at a
max of 28, 200 resizes through distinct widths at a max/final of 6
(superseded widths released), each ending with a fresh `ShapeText`
probe. The pre-fix code path (cache bypassed) reproduces the ticket's
exact symptom in the same test: 18 drawn frames, then every further
frame fails with `gpui: text element shaping failed: … text shaping
handle limit exceeded` and the frame loop dies in layout — matching the
spike's 17-redraw measurement. The cache key's canonicality is pinned
by 4 in-package tests (gpui/text_cache_test.go).

No native artifact change: the fix is pure Go above the frozen native
shaping seam (no cargo/Rust touched).

## Comments

- 2026-10-19 (pi worker): claimed, implemented, verified, resolved.
  Full-suite regressions (with reference/ce-source copied in, a plain
  copy): 35 packages ok; every failure a machine-state class reproduced
  identically on the pristine base commit in a temporary git worktree
  (desktopspec's cursor gate — the resume note's documented flake — and
  dropspec's pointer-routing assertion, which passed in both earlier
  full-suite runs of the changed tree and turned flaky on both trees
  mid-session; shutdownspec's modal test passed 4/4 isolated and in the
  final full run).
- 2026-10-19 (pi worker): incident, recorded for the sibling workers —
  this repo's worktrees share the git stash, and a baseline check via
  `git stash`/`pop` raced a sibling worker's own stash cycle: one pop
  applied the ticket-05 worker's changes here and mine in their
  worktree. Both trees were restored to their pre-race state through the
  shared stash immediately (the ticket-01 worker's WIP entry was never
  touched); the ticket-05 worktree ended with its own changes back and
  the stash back to a single `ticket01-wip` entry. Baseline results were
  produced on the clean HEAD before the misroute and remain valid.
  Lesson (now orchestrator policy): never `git stash` in these worktrees
  while sibling workers run; baseline-check with a temporary
  `git worktree add` of the base commit instead — the final baseline
  verification here used that method.
