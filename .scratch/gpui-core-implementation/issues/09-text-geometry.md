# Shape text and expose paragraph, caret and hit-test geometry

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer subagents (parallel), session 065e52f5
Blocked by: 06

## Question

A Go consumer shapes pinned fonts and queries wrapping, clusters, selection and carets with reference-matching geometry.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [windows platform contract](../../../docs/windows-platform-contract.md), [layout contract](../../../docs/layout-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Integrate pinned Parley/Fontique/HarfRust/Skrifa/Swash and font discovery behind checked native text services; record font bytes/face/axes and fallback selection.
- [x] Cover multilingual shaping, bidi, features, ligatures, fallback, wrapping, line clamp, byte ranges and caret/selection/hit-test behavior against independent text fixtures.
- [x] Preserve font/load/cache identities and text layout ownership across foreground measurement; expose explicit errors and lifetimes for result buffers.
- [x] Keep UTF-16 document coordinates distinct from text byte offsets and test surrogate/cluster boundaries; do not settle native IME screen/window coordinates by assumption.
- [x] Exercise text measurement within actual layout callbacks, including nested independent windows and failed callbacks. Produce CPU geometry evidence without claiming glyph rendering.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Compute styled layout through the pinned Taffy service](06-layout-roundtrip.md)

## Answer

Resolved 2026-10-05 by the implementation chat (Roboco `065e52f5`, pi / iroha/dashscope/glm-5.3), implemented by three coordinated subagents (native service, reference oracle, Go adapter) and independently re-verified. Complete record: [evidence/ticket09-text-geometry.json](../../../evidence/ticket09-text-geometry.json).

- Native text service (slot 4, mask 0x1F, revision 5): the pinned Parley/Fontique/HarfRust/Skrifa/Swash stack through gpui::TextSystem with the exact WindowsPlatform::new construction (Segoe UI default + Lilex/IBM Plex Sans/Arial fallbacks), Fontique system-font enumeration (DirectWrite), 15 ABI entries covering resolve/metrics/shape/layout/lines/fragments/clusters/carets/hit-tests/selection with generation handles and capacity bounds. DLL 7,579,648 bytes static CRT (the full pinned gpui graph, as the combined-artifact contract anticipates).
- Reference oracle fx-0004 (339 events, deterministic): 10 cases covering basic Latin, grapheme clusters (decomposed marks + ZWJ), CJK fallback, wrapping with both affinities at every boundary, line-height override, features/letter-spacing, multi-run weights, wrap+clamp, empty and newline-only — all through the pinned public text API only.
- THE GATE: the port reproduces ALL 339 events bit-exactly through its real adapter over the native service (CompareTraces equal, empty diff), including canonical FontId interning order, the golden-ratio default line height bits, cluster=grapheme semantics, caret/hit/selection geometry and the recorded edge behaviors. Full suite green.

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

