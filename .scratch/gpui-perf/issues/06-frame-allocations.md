# Cut per-frame allocations

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: gpui-perf W1
Blocked by: 01, 02

## Question

Use ticket 01's benchmark and `go test -memprofile` to find the top allocation sources per frame, then remove the cheap ones. Suspected sources, all unmeasured:

- tracked-entity maps rebuilt every frame (`gpui/frameloop.go:477`)
- the per-frame scene and frame state (`gpui/render.go:90-120`)
- per-glyph pixel buffers
- per-element slices and maps in the fluent builders

The approach is to reuse buffers across frames (clear instead of remake) and pool them where the lifetime is frame-scoped. Behaviour must not change, and the GPUI-parity tests stay green. Don't touch the files ticket 04 owns (`gpui/view.go`, the view-cache code paths in `render.go`) without coordinating through the orchestrator. Report the before/after table.

## Comments

## Answer
Measured with ticket 01's benchmark and `go test -memprofile -memprofilerate 1` on `BenchmarkFrameRefreshDivs` and `BenchmarkFrameRefreshText`. Changes:

1. **Style boxes** (`styled_refinement.go`). A div computes its style three times per frame: in request_layout, prepaint and paint. Each `refinePointer` boxed `Background` and `BorderColor` again, which came to about 6,000 allocs per frame at 1,000 divs. `Style.refineBoxed` now takes a per-element `styleBoxes` cache (`DivElement.styleBoxes`). Later computations reuse a box when its value is bit-identical. Nothing writes through Style's pointer fields (checked with grep), and boxes never alias the refinement. `Style.Refine` is unchanged: a nil cache means a fresh box each time.
2. **Layout context by value.** `layoutContextOf` (`element.go`) returned a pointer, so every layout query heap-allocated a context the engine only reads. It now returns a value and callers pass its address. That's about 2,000 fewer allocs per frame for divs and 1,700 for text.
3. **Allocation-free border strips.** `paintQuadStrips` returns `([4]Quad, n)`, which removes 3 slice-growth allocs per bordered quad.
4. **Div a11y state as a field.** It's now the `a11y` field, which is the STORAGE note's own intended end state. The weak-keyed side table used to call `weak.Make` per div per frame. The side-table GC test is replaced with a test that the state lives on its div.
5. **Shared empty inline record.** Leaf divs publish one shared, read-only empty inline-container record.
6. **Line paint dumps cached on the `WrappedLine`** (`text.go` `paintDumps`, dropped by `refreshSummaryOf` on every layout change). Paint reads them without the native handle, so lines parked under the 256-handle bound no longer re-shape at every paint. Before this, that re-shaping was 55% of the text frame's CPU. The public `Fragments`/`Glyphs` return clones.
7. **Text measure and key encoding.** The wrap width is boxed only when the text actually shapes. The cache-key buffer is sized correctly, so it no longer regrows, and the key aliases the buffer instead of copying it.

Before and after (ticket-01 baseline → after 02 → after 06; `evidence/perf/ticket06-after.json`, best of 5):

| scene | allocs/frame | B/frame | native calls/frame | ms/frame |
|---|---|---|---|---|
| FrameDivs | 16,476 → 16,477 → 5,431 | 5.02 → 5.02 → 4.04 MB | 8,037 → 8,037 → 8,037 | 11.3 → 10.4 → 7.5 |
| FrameRefreshDivs | 16,418 → 16,419 → 5,373 | 5.02 → 5.02 → 4.04 MB | 8,037 | 10.1 → 8.5 → 8.8 |
| FrameText | 81,583 → 17,412 → 8,394 | 18.7 → 3.5 → 1.9 MB | 129,292 → 23,530 → 21,130 | 200.1 → 82.6 → 31.1 |
| FrameRefreshText | 81,574 → 17,405 → 8,389 | 18.7 → 3.5 → 1.9 MB | 129,292 → 23,530 → 21,130 | 200.8 → 84.5 → 30.1 |

Remaining, measured and not changed:

- **Divs:** each div still makes about 5 allocations: `Div()` (a 2.6 KB DivElement, most of the bytes), `IntoElement`, `CustomElement`, and the first computation's two style boxes. These are the builder's own objects.
- **Text:** most of the rest is per-element structure: the measure closure, the cell, the inline-text record, the shaped-text wrapper, and the cache-lookup request runs.
- **Small, left alone:** the tracked-entity maps (`frameloop.go:477`, about 2 allocs per frame) and the per-frame scene and frame state (1–2 allocs, which belong to tickets 04 and 05).
- **Native calls:** the div scene's 8,037 native calls are layout nodes plus quad inserts, which is ticket 05's batching.

Validation: `CGO_ENABLED=0 go test -p 1 ./... -count=1` passed 49 packages. Two packages failed:

- **headlessspec:** fails because reference/ce-source is missing. This is environmental and known.
- **scenespec:** `TestInsetShadowBlurPaintsFromPaddingEdgeOnRealGPU` failed once in its cleanup ("foreground post was dropped: the host loop exited", after its pixel assertions passed). On re-runs it failed 2 times in 11 tries early on, then 0 times in about 70 tries, including 30 with the host trace on. The detached baseline 9bec2ec passed 24 out of 24. It's intermittent and real-window only. I haven't proven it's unrelated to these changes; none of them touch the host thread or window lifetime.
