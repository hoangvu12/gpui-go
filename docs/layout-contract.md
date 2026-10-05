# Layout implementation and parity contract

Status: selected documentation contract for [Choose the layout implementation and parity method](../.scratch/gpui-core/issues/05-choose-layout-strategy.md), 2026-10-04. The decision records rationale and delegated selection; [layout research](../research/15-layout-strategy.md) supplies reviewed primary evidence. No bridge, DLL, layout implementation, compiler probe or executed parity result is supplied by this document.

## Engine and ownership

Use the pinned Taffy engine behind a private native interface, with GPUI's layout adaptation in Go. The native library owns the Taffy tree and algorithm state; Go owns GPUI style refinement, unit conversion, measurement registrations, absolute-position caches and inline/text orchestration. Application authors continue using the selected Go element/window operations and do not import Taffy or handle DLL pointers.

The target is GPUI-CE `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`, not current Taffy or generic browser CSS. Its [crate manifest](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/Cargo.toml) requests `=0.13.0`; its [lockfile](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/Cargo.lock) resolves the crates.io package with checksum `c034e05f6ee85a12daa63863c2245797715075c70649947aa0da54f3f2ab1d0f`. The native build manifest must pin the complete dependency resolution and effective features, compiler/target identity, bridge revision and ABI version. Updating any of these requires the recorded compatibility process.

This is an integration to build and verify, not a claim that a suitable ready-made Go binding exists. A pure-Go Taffy port remains a possible later replacement behind the same internal test surface, subject to the complete differential corpus. It is not a fallback that silently changes layout when a DLL is missing. Existing simplified flex/grid engines do not establish equivalence to the pin.

The GPUI dependency enables Taffy's defaults. The [0.13.0 manifest](https://github.com/DioxusLabs/taffy/blob/v0.13.0/Cargo.toml) lists `std`, `taffy_tree`, `flexbox`, `grid`, `block_layout`, `float_layout`, `calc`, `content_size` and `detailed_layout_info`; retain these rather than pruning features based on guessed usage. Record the resolved target feature graph when building the oracle/artifact; a lockfile alone is not a feature-activation report. Enabled native capabilities do not automatically become new public GPUI properties.

## Units, styles and rounding

Apply refinements before translation. Preserve absent versus explicit zero values, `Auto`, absolute logical pixels, rem-relative lengths and percentages as distinct values. Preserve `Definite`, `MinContent` and `MaxContent` available-space modes and optional known dimensions. Zero available width is not indefinite; `flex_shrink = 0` is not an omitted default. Rem values use the active rem scope at node construction.

The compatibility adapter follows the saved [Taffy integration](../evidence/core/crates__gpui__src__taffy.rs) and [style mappings](../evidence/core/crates__gpui__src__style.rs). It covers the fields GPUI forwards: display/overflow/scrollbar width, position/insets, size/min/max/aspect ratio, margin/padding/border, alignment, gap, flex direction/wrap/basis/grow/shrink, grid row/column templates and placement. Fields omitted by GPUI keep the pinned Taffy defaults. Do not invent a broader public CSS surface. `Inline` maps to block and `InlineFlex` to flex at the Taffy layer; GPUI separately preserves their inline semantics.

Taffy's own rounding is disabled. Computation uses device-pixel units; public measurements and bounds use logical pixels. Preserve the source's `f32` operations and their order rather than doing all intermediate arithmetic in Go `float64` and casting only at the end.

| Stage | Required behavior |
|---|---|
| Authored absolute lengths | Convert rem to logical pixels, multiply by scale, round nearest with midpoint ties toward zero |
| Percent lengths | Preserve fractions for layout resolution; do not preconvert against a guessed parent |
| Border widths | Preserve the pinned stroke rule: exact zero stays zero; otherwise clamp/snap with at least one device pixel |
| Padding on an auto-sized axis | For two nonnegative absolute edges with positive total, snap the total and redistribute in the original ratio; explicit axes, percentage edges and source fallback cases use their ordinary conversions |
| Measured leaf size | Clamp each returned logical dimension to at least zero, multiply by scale, then ceil |
| Ordinary outer bounds | Accumulate unrounded absolute device origins, round near and far edges independently, subtract edges for size, then divide by scale |
| Text parent-relative bounds | Follow the dedicated parent-relative path; inline fragments and parentless nodes use the source's fallback |
| Window element offset | Apply the separately snapped offset after obtaining layout bounds |

The midpoint rule rounds `1.5` to `1` and `-1.5` to `-1`, not away from zero or to even. Stroke behavior is a compatibility rule, not a new normalization policy for all lengths. Preserve the exact helper behavior, including negative inputs and signed zero, in fixtures. See the [pinned helpers](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/util.rs).

## Layout phases and measurement

Request ordinary or measured nodes during request-layout/prepaint. Stretch only automatic root dimensions to the snapped viewport; explicitly styled dimensions remain intact. Compute under prepaint, then query bounds. Recomputing an already computed subtree invalidates its absolute-position caches before querying again. Layout IDs include engine identity and generation and become unusable on engine reset; they are not persistent element-state keys.

For a measured node, Taffy supplies optional known dimensions and available-space modes in device units. Convert definite dimensions back to logical units, call the registered Go measurement synchronously on the owning foreground thread with fresh valid window/app access, then apply measured-size snapping to the result. Taffy's caching may avoid a callback or invoke it repeatedly with different constraints. Do not impose one callback per node/frame or replace min/max-content with an arbitrary large width.

Computing one engine marks it busy. A measurement callback cannot mutate, clear, query layout from, or recursively compute that same engine. Reject this before native entry. The source temporarily takes the engine out of its window during compute; this is not evidence that recursive access to that window's layout engine is supported. Separate legitimate window/build scopes retain their own engine state, and repeated sequential prepaint layout remains supported. Cross-window nesting must preserve the outer computation's callback context and scratch buffers. See [window entry points](../evidence/core/crates__gpui__src__window.rs).

Use per-engine busy guards, not a process-wide lock held across application callbacks. A trampoline is an authorized synchronous foreground entry only when its OS thread and active compute token match; do not rely on an inferred goroutine ID. Registry lookup must not leave a mutable global borrow/lock held across callback execution, including a legitimate nested call using another engine.

The Go callback registry and explicit resources captured by measurement callbacks belong to the construction scope. Callback pointers/contexts cannot outlive it. Reset/disposal occurs only after active native calls return; cancellation/window closure during a callback marks the construction for abandonment and defers native destruction until return. Failed construction publishes no new artifacts and does not roll back arbitrary application mutations, following the [runtime contract](runtime-ownership-contract.md).

## Inline content and cache behavior

Keep GPUI's paragraph and inline-layout orchestration outside the native Taffy kernel. It must coordinate text runs, atomic inline boxes, vertical alignment, fragment bounds and detached box placement. Moving a detached inline box invalidates descendant absolute bounds and origins. Taffy's block/flex mapping alone cannot preserve inline layout.

Window text measurement must use the same font/run/wrap/clamp inputs expected by the selected text implementation. Text font-generation and layout invalidation remain significant even if the Taffy tree is identical. [Choose Windows platform, text, and input integration](../.scratch/gpui-core/issues/06-choose-windows-platform-and-text.md) owns that implementation and actual-font evidence.

Frame/cache reuse retains copied geometry, text layouts, callbacks and dependencies under their proper scopes. It must not retain stale native node IDs after the tree clears. Frame-local Taffy caches are distinct from persistent element state and GPUI's reusable render artifacts. Additional caching must preserve the same observable layout/measurement behavior before it can be enabled.

## Native interface and consumer builds

Use a maintainer-built Windows DLL with a versioned C-compatible ABI and a Go Windows dynamic loader, so the consumer path can build with `CGO_ENABLED=0`. Maintainers need the Rust/native toolchain; app developers must not need it. Merely shipping a static library while requiring `import "C"` would not satisfy the existing consumer requirement: [cgo still invokes a C compiler](https://pkg.go.dev/cmd/cgo).

The interface needs engine creation/reset/disposal, ordinary/measured node creation, root size/style adjustment, compute, copied node geometry/parent relationships, and bulk result retrieval. Keep operations coarse where practical, with synchronous measurement callbacks only where computation needs them. Go-side GPUI semantics must have enough native outputs to preserve layout; do not assume root bounds alone suffice.

ABI records have explicit widths, tags, presence flags, offsets, counts and version/size fields. Exchange floating-point values as `f32` bit patterns or fields in pointer-addressed fixed-layout records; do not rely on floating-point positional arguments or returning Rust structs through Go's word-based call path. Go/Rust bools, enums, `Option`, slices, `Vec`, strings and closures never cross by their native representation. Native IDs are validated opaque handles, not Rust memory addresses exposed as public Go values.

A measurement request carries independent presence flags for known width and height, independent available-space tags for both axes, each definite value's bits, the engine/node identity, and its callback/compute tokens. One shared available-space tag cannot represent mixed modes. Node-creation operations return a new validated ID; engine and node disposal/reset operations are explicit. Native geometry retrieval includes parent relationships and all fields the GPUI adapter consumes, rather than reconstructing them from rounded bounds.

Use a fixed callback trampoline plus generation-checked registry tokens, not one Windows callback allocation per measured node or frame. Callback arguments/results use word-sized scalars and pointers to native-owned request/response records; copy them during the call. No Go object graph or callback closure pointer is retained by native code. Native buffers own any data retained between calls; result arrays copy into Go-owned storage before native release. A borrowed Go buffer, if used for a synchronous transfer, must contain no Go pointers and remain pinned/alive for that call; `uintptr` alone is not ownership.

The DLL and trampoline use a matching Windows calling convention. [Go's Windows callback and procedure facilities](https://pkg.go.dev/golang.org/x/sys/windows) establish the available mechanism, not proof that this proposed bridge works. Interpret the bridge's explicit return status; a procedure-call last-error value alone does not establish failure. Select and verify target architecture through the distribution decision rather than silently promising every Windows architecture.

Keep the loaded library alive while any engine, export invocation or callback can reference it. Remove registry entries only after the associated native call has finished; reset advances generations before token slots can be reused. A close request during a callback cannot free its request records or unregister its active dispatch token.

Each allocation has one matching owner and release operation. The bridge frees its native buffers through its own exports; it never frees Go-backed buffers, and Go never passes Rust-owned storage to an unrelated allocator. Validate counts, capacities and byte-size arithmetic before copying.

Contain Rust panics inside native exports and recover Go callback panics inside the callback trampoline. Latch callback failure, suppress subsequent user callback invocations during that compute, and discard the result; if the underlying measure API requires a size, a sentinel size is internal failure plumbing, never a successful layout. Mark failed engine state for reset/disposal after return. Re-raise the original Go panic only after returning to ordinary Go and restoring scope/engine bookkeeping. Unwinding must not cross the ABI; process-aborting faults cannot be promised recoverable.

The bridge's own build profile must enable unwinding for this Rust panic boundary; `catch_unwind` cannot recover an aborting build. Do not inherit an example/workspace release profile by assumption. This is a maintainer build requirement and does not change the engine algorithms or claim recovery from allocation failure or memory corruption.

Missing/incompatible artifacts, unsupported ABI versions, invalid handles, malformed buffer sizes and callback failure are explicit errors. Never fall back to another layout algorithm or publish partially computed geometry. [Distribution planning](../.scratch/gpui-core/issues/08-define-module-and-native-distribution.md) owns concrete artifact naming, provenance, installation/loading policy and native runtime dependencies. This contract selects no public module path.

## Differential verification

Use an independent oracle built from the pinned Rust GPUI adapter and locked Taffy, not the new bridge code compared with itself. Feed both sides the same semantic style tree, rem/scale, available space and deterministic measurement function. Record full callback requests/results in addition to every node's raw and snapped bounds; a changed callback query is itself a useful mismatch, not a reason to feed responses by ordinal regardless of arguments.

Require exact tags, tree relationships, callback inputs/order under identical cache history, and snapped device-edge values for deterministic fixtures. Compare finite `f32` values bit-for-bit initially; investigate any discrepancy before defining a narrowly justified tolerance. Signed zero may be normalized only where it is observationally irrelevant and the fixture explicitly says so. The approximate visual-closeness goal does not authorize a layout error allowance.

The fixture matrix includes auto/explicit roots, percentages with indefinite parents, zero/min/max-content space, min/max/aspect constraints, flex shrink-zero/grow/wrap/baseline, absolute positioning/overflow, exposed grid repeat/span/placement, rem overrides, fractional DPI, negative and half-pixel coordinates, borders and proportional padding, repeated measurement/recompute, text parent-relative snapping, inline atomic boxes/fragments, detached placement, reset/stale IDs, cached-frame reuse and aborted/nested construction.

Use deterministic fake metrics to isolate layout and a separate Windows-font suite for shaping, wrapping, fallback, baselines, clamp, bidi and inline integration. The latter depends on the platform/text decision. Neither suite is replaced by screenshot similarity alone.

Before claiming the native consumer path works, run a clean Windows consumer build with Go plus the prebuilt DLL and `CGO_ENABLED=0`, then execute measurement callbacks, forced GC during callbacks, stale-token/reset cases, same-engine reentry rejection, callback panic, failed compute and shutdown. Verify record size/alignment, f32 round trips, DLL version checks and thread affinity independently of generic-authoring signature tests. [Conformance planning](../.scratch/gpui-core/issues/09-define-conformance-and-update-policy.md) records these gates; none has run in this planning work.
