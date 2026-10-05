# Conformance and upstream update contract

Status: selected planning contract, 2026-10-05, for [Define conformance gates and upstream update policy](../.scratch/gpui-core/issues/09-define-conformance-and-update-policy.md). The [signature fixture](../.scratch/signature-validation/README.md) has executed; all framework/native/reference/visual gates below remain unexecuted. The [coverage inventory](conformance-inventory.md) assigns the capability families to future evidence and implementation work. [Reviewed research](../research/20-conformance-spec-coverage-review.md) supports the decision; this contract supersedes research proposals.

## Completion and reference profiles

The destination remains full Windows GPUI-CE core behavior at `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`, with the accepted Go adaptations and approximately 98% visual closeness. A working counter, a screenshot, a successful native build or completion of an early implementation slice does not establish core parity. Base, other operating systems and broad Windows/architecture support remain later work.

The baseline reference is the pin's native Windows host with its effective default dependency/features graph, plus the selected screen-capture capability. Record release and debug/inspector/test profiles separately. Pin all resolved dependencies and compiler/build inputs for both the independent oracle and extracted artifact; a Cargo lock is not a feature graph. Do not infer active codecs or platform operations from a filename-extension list or crate dependency alone.

Enumerate every public core export and Platform/PlatformWindow/text/element operation into a versioned ledger during the first implementation slice. Each row records source location, reference profile, Windows applicability, port owner, fixture IDs, status and any approved exception. Reference unsupported/no-op behavior is a real outcome to reproduce and identify. A missing port implementation is a failed/pending row, not automatically an upstream exception. The family inventory is the organizing map, not a claim that the per-symbol audit has already run.

Alternate wgpu/custom-GPU features require source applicability tracing separately from the selected native D3D11 host; selecting D3D11 does not promise a second backend or authorize dropping a core method. Preserve the native reference's availability/error behavior for exposed operations, with no blank successful draw. Rust-specific stack guards, embedding macros and compiler instrumentation are mechanisms; preserve the relevant behavior and effective reference configuration rather than inventing mandatory flags. Debug inspector behavior and public diagnostics have assigned implementation work. Any capability still applicable and unimplemented blocks a full-core claim unless a specific exception is approved.

## Evidence states and pass rules

Use `planned`, `recorded-reference`, `passed`, `failed`, `blocked-environment`, and `approved-exception`. Record the exact artifact/source/environment with each result. Missing hardware, an unavailable IME or blocked capture consent is not a passed test. A fixture can be applicable but blocked on the current PC; distinguish that from a source-supported capability query returning unavailable. No numeric pass rate can conceal required pending rows.

1. **Signature checks:** the chosen API declarations compile, positive external usages typecheck and each negative fails for its intended constraint with a passing control. [Current evidence](../evidence/authoring-signature-validation.json) covers the bounded surface, not every future API. Rerun against the real implementation as it replaces stubs.
2. **Exact deterministic checks:** compare identities/tags, ordered effects, layout trees and measurement requests, scene ordering/batches/filter plans, deterministic text geometry, CPU image outputs, and ABI record layouts. Use finite `f32` bit equality initially; preserve signed zero unless a fixture explicitly establishes its irrelevance. Investigate mismatches before proposing a narrowly justified tolerance. Approximate visuals cannot compensate for these failures.
3. **Behavioral checks:** compare reference traces plus explicit Go-adaptation assertions for ownership, cancellation, input routing, IME, accessibility and OS integration. Control scheduler decisions and use virtual time where feasible. Do not demand identical OS timestamps, addresses or Rust destructor mechanics when their documented Go replacement has a different implementation.
4. **Visual checks:** compare controlled reference/port captures with calibrated per-feature tolerances and material-difference review. The number 98% is user intent, not a chosen pixel-match fraction or SSIM score.
5. **Delivery checks:** build and run a real consumer with local Go, `CGO_ENABLED=0` and prebuilt assets alone, including callbacks, DPI resources and shutdown. Source inspection or a manifest without its binary is insufficient.

Responsiveness remains the accepted practical criterion: investigate visible stutter and input lag on representative workloads, preserving traces to reproduce them. Comparative Rust/Go build benchmarks, a hard disk/download budget and a new numerical latency budget are not prerequisites. The module-format size ceiling is a delivery constraint, not a performance target.

## Independent oracle and fixture format

Build the reference harness directly from the pinned Rust implementation, outside the new bridge's execution path. Sharing serialized fixture inputs is intended; sharing the Go adaptation or comparing the extracted bridge against itself is not an independent oracle. Maintainer tools may need Rust/MSVC/SDK/shader tooling. Consumers do not. Preserve reference patches and verify that instrumentation does not alter relevant behavior.

Every fixture has a schema version, stable ID, capability/contract links, profile, semantic inputs, declared environment dependencies, oracle source/build identity, expected outputs, comparison rules and adaptation/exception IDs. Store portable records; encode `f32` values as fixed-width hexadecimal bit patterns when exact comparison matters. Use stable fixture entity/resource labels instead of addresses, COM pointers or runtime allocation IDs. Retain raw traces alongside normalized results and document every normalization. No unlisted normalization may discard a semantic difference.

| Fixture family | Required input and output content |
|---|---|
| Ownership/effects | Seed, virtual ticks, ownership/scope operations, registration order, worker completion schedule; accesses, cancellations, releases, focus changes and delivered effects |
| Layout | Semantic style tree, refinement history, rem/DPI, per-axis available-space modes, optional known dimensions and deterministic measurement function; every callback query/result and every node's raw/snapped geometry, parent relation and cache history |
| Scene | Ordered semantic paint commands including layers, deferred floor changes and replay ranges; final operation order, primitive arrays, batches, filter-target plan, paired opacity and resource dependencies |
| Text | Font bytes/hash/face/axes, runs, byte ranges, features, direction, wrapping/clamp and inline boxes; clusters, line/caret/selection/hit-test geometry and raster format/bounds |
| Image/SVG | Input bytes/hash, declared entry mode, font/asset responses, SVG size mode; frame pixels, dimensions, rational delays, scale, orientation, alpha masks and request/resume trace |
| Input/keymap/IME | Physical messages and semantic inputs, context/focus tree, chord timing, UTF-16 document/marked ranges; synchronous replies, translated events, propagation and text/candidate geometry |
| Accessibility | IDs/roles/hidden/property/synthetic-child updates, activation and provider-query schedule; published snapshots, queued actions and teardown/lifetime trace |
| Native protocol | ABI/schema identity, buffers/tags/handles and completion schedule; checked records, callback failures, ownership transitions and terminal acknowledgments |
| Visual | Capture stage/format, fixture regions, reference repeats, injected defects and calibration revision; images, differences, per-region statistics and reviewer disposition |

Never feed layout responses solely by callback ordinal: calculate from the actual query and compare that query under identical cache history. Record nondeterministic worker completion as an explicit input schedule, then test additional schedules/seeds. A regression failure retains its seed and minimal reproducer.

## Required behavioral corpus

The capability inventory supplies the detailed ownership map. The minimum cross-cutting corpus includes:

- Independent leases in two windows; alias release; borrowed-handle expiry; weak upgrade failure at logical zero; scope cancellation before release; explicit strong-cycle diagnostics; retained callback/frame/cache resources; same-key removal/reinsertion and abandoned-frame cleanup.
- Pending-only notification coalescing and re-notify; every event occurrence in FIFO order; deferred registration activation; cancel-next delivery; frozen/owned payload retirement; arena-tail release progress; late worker results and detached tasks during shutdown.
- Canonical actions, duplicate names/aliases/type definitions, explicit Clone/Equal, NoAction/Unbind, NoJSON versus unregistered versus decode errors, bound boxed payload versus dispatched payload, capture/bubble defaults, key contexts, chord timeout/mismatch/replay, focus traversal and release.
- Child conversion precedence, all nil-capable values, token alias/duplicate attachment, converter reentry, failed batch reservation cleanup, no claimed rollback of external converter side effects, custom typed phases and accessibility companions.
- Full layout/flex/grid/inline/refinement/units/rounding/measurement/cache cases from the layout contract, plus actual-font paragraph/selection/hit-test fixtures distinct from fake-metrics layout tests.
- Every scene primitive and glyph format, layer/order-floor/replay behavior, opacity pairing, filter boundaries and depths beyond two, clipping, path targets/MSAA, blend/gamma/color rules and both DComp/HWND modes.
- Accepted versus retired GPU work; delayed queries; upload-only/hidden work; ensured query submission; partial-submit failure; cancellation; resize/device loss; safe abort versus quarantine; shared devices across windows and provider/capture references after window closure. Capture holds actual frame ownership through copy completion and immutable destination storage through later draws.
- Asset duplicate paths, on-demand source behavior, Null/Blocked/injected HTTP clients, redirect flag/error mapping, loading-delay transitions, clipboard versus resource WebP modes, GIF delays/error frames, all EXIF orientations, SVG unpremultiply arithmetic, lazy font snapshot/request/resume and sizing/masks.

Input/OS tests include real dead keys/AltGr/repeat/surrogates and keyboard-layout changes, Japanese/Chinese/Korean composition where installed, focus transfer between windows, marked-text commit/cancel, clipboard interoperability, OLE drag/drop, Direct Manipulation, file open/save prompts and modal pumping. Explicitly cover minimize/restore, suspend/resume, settings changes, cursor hide-until-move, character-palette modifier restoration and partial-send cleanup, restart reentry, notifications, jump lists/dock menus and credential-size/absence/error results. Use scripted assisted sessions, recording exact OS/IME/tool versions and unavailable cases; installing another reader or IME is not implicit authorization. Use available Narrator/UIA tooling and a third-party reader when already available.

Accessibility tests cover activation before a complete tree, hidden/synthetic nodes, semantic actions and concurrent provider queries during updates and teardown. Published provider queries cannot wait for a blocked Go UI update. Validate apartment-affine release and lock release before reentrant UIA event dispatch.

The caret-coordinate discrepancy is a mandatory reference fixture at a nonzero window origin, then move/scroll/DPI changes. Neither the core comment nor the Windows call site alone settles the convention. Instrument and classify the reference before freezing the adapter; if it is a reference bug, record evidence and the parity-policy decision. No guessed coordinate convention is silently selected here.

## Visual calibration procedure

Freeze this procedure now; numerical thresholds are an implementation acceptance gate requiring actual reference images. This permits planning completion without claiming a visual result.

1. Select fixed scenes spanning each primitive, text raster style, image/SVG type, clipping/opacity/filter combination and composition mode. Use representative pinned learn/legacy examples and targeted minimal fixtures. Record scene state and animation time.
2. Record OS/build, actual selected adapter/driver/feature level, target/capture format, scale/DPI, font bytes/face/axes/fallback set, rendering parameters, background and composition settings. Capture reference and port at the same defined stage. Normalize alpha/color representation identically and transparently; never compare premultiplied and straight-alpha images as if they were equal formats.
3. Repeat reference captures and measure variance per declared region. Retain sample counts, distributions and raw images. Missing content, shifted hitboxes or wrong text geometry cannot be excused by a small aggregate score or unrelated reference noise.
4. Evaluate candidate metrics using known defects: missing glyph/primitive, shifted edge, wrong color/gamma/alpha, dropped filter/shadow, clipping and crop errors. Require detection of every material seeded defect. SSIM may complement pixel/color/edge/region diagnostics; it is neither excluded nor automatically selected.
5. Propose local thresholds and a material-difference review rubric from repeatability and defect results. Calibrate on one set and confirm on held-out representative fixtures. Freeze the metric, threshold, mask/region definitions, environment and review result as a versioned calibration record before assessing the port's release corpus.
6. Require every required feature/region gate to pass and review reported local differences. Keep full images and largest-difference regions. No broad text/transparent-region masks, post-failure threshold loosening or aggregate pass percentage can hide a broken feature. Any narrowly justified mask is individually named, visible in reports and covered by other evidence.

If a material tradeoff remains after calibration, present concrete reference/port/diff images for user judgment; routine threshold selection follows the existing delegated technical workflow. Until calibration and required visual gates pass, report visual parity as pending, even if implementation tickets are complete.

## Distribution and failure evidence

Validate the exact artifact's PE machine, normal/delay/dynamic imports, static-CRT outcome, private ABI tables, feature graph, shader hashes and module zip contents/limits. Exercise byte corruption, incompatible schema/architecture, missing compiled capabilities, OS availability errors, offline/bundle mode, unwritable storage, two-process first publication and killed-writer recovery. Verify read/share/lock behavior on Windows; do not assume rename is atomic. Inspect the final application manifest and foreground DPI mode.

Run callbacks with forced GC and panic injection, cross-window nested layout, same-engine rejection and cancellation during native calls. Track exactly-once cleanup. Clean-machine success means no Rust/C/resource/shader compiler or VC++ redistributable assumption is needed; inspect actual imports and run the consumer. Module residency does not discharge COM/GPU ownership obligations.

## Baselines, exceptions and updates

Store fixtures, calibration and exceptions with the fixed CE pin and complete environment/build identities. A Go adaptation entry states the source behavior, replacement guarantee and evidence: explicit scopes versus Rust drop, weaker borrow enforcement, dynamic child diagnostics, payload ownership, added GPU retirement, same-device capture and lazy asset request/resume are examples. Do not classify every implementation mismatch as an intentional adaptation.

An exception record requires a narrow affected operation/profile, reproducer, source behavior, proposed port behavior, rationale, approval provenance and expiration/revisit condition. The initial ledger has design adaptations but no blanket exemption for missing core features. Native-reference unsupported behavior must cite its actual implementation. The final coverage report lists all pending/failed/blocked/excepted rows explicitly.

Updating the upstream pin is deliberate: propose the new commit and semantic/dependency diff, review changed contracts, rebuild independent oracles/artifacts, run the impacted and full integration/release corpus, update exceptions and record the accepted decision. Do not overwrite old failing goldens or auto-follow upstream. Dependency/security patches may preserve the CE pin but require a new artifact identity, patch rationale, feature/import/license inspection and affected plus integration gates; new expected outputs require reviewed evidence, not automatic rebasing.

## Planning closure versus implementation acceptance

Signature verification now passes and every known architecture seam has a selected boundary. The specification assigns oracle capture, coordinate resolution, native feasibility checks, capability enumeration and visual calibration to early or explicit implementation gates. If execution contradicts a selected contract, reopen the affected decision with the reproducer rather than concealing the failure or changing the parity target.

The specification and tickets authorize a plan, not a claim that the framework exists. A full-core completion report requires complete source coverage, all applicable exact/behavior/delivery gates, calibrated visual acceptance, and explicit treatment of every exception or unavailable environment. No native or parity gate has passed merely because Wayfinder is complete.
