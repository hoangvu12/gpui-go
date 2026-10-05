# Conformance specification coverage review

Main integration status, 2026-10-05: root/fixture tests and vet and all 29 negative/control pairs now pass; the stderr recording issue below was resolved. The final [conformance inventory](../docs/conformance-inventory.md), [specification](../.scratch/gpui-core/spec.md) and [33-ticket plan](../.scratch/gpui-core-implementation/plan.md) supersede intermediate status and proposed slice grouping in this research. Follow-up read-only reviews added explicit Windows operations and a headless/render-to-image owner. No native or parity execution is claimed.

Research date: 2026-10-05 (revised after main review and compiler-fixture progress). Decision-ready coverage map for closing the Wayfinder planning phase into an implementation-ready specification, per the user's "ok do them". Status inputs: **main's compiler fixture positives passed under the user-installed Go 1.27.1** — generic methods and inference compile, including external-package authors; **all 29 negative builds failed as intended**, with runner stderr wrapping being fixed before the final ticket-10 record. This document is source/planning review only: nothing here compiles, executes, or claims parity. Inputs: the five selected contracts, [authoring decisions](../docs/authoring-decision-round.md), [conformance research](19-conformance-update-policy.md), the [map](../.scratch/gpui-core/map.md), ticket 09, and pinned CE sources. CE pin `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`.

## 1. Capability coverage map

| Family | Pin/evidence | Owner | Gate |
|---|---|---|---|
| App/entities/scopes/effects/executors | app.rs, app/context.rs, entity_map.rs, subscription.rs (local) | Go | A ordering/transcripts + B |
| Authoring surface (events/actions/children/views/custom elements) | authoring decisions + counter/two-window corpus | Go | compile (ticket 10, near-closed) + B |
| Windows host: messages/OLE/DPI/modal/restart/power | [gpui_windows platform.rs](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_windows/src/platform.rs), events.rs, window.rs | Go host | B transcripts |
| Layout engine + snapping + rem/DPI/root stretch | [taffy.rs](../evidence/core/crates__gpui__src__taffy.rs), window entrypoints | native DLL, Go adapter | A full-node oracle |
| Text shaping/fallback/raster/paragraph/caret | [gpui_ce_parley](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_ce_parley/src/text_system.rs), [font_rasterizer.rs](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_windows/src/font_rasterizer.rs) | native DLL, Go adapter | A (fonts pinned) |
| Inline text/atomic boxes/fragments | [text_system/line_layout.rs](../evidence/core/crates__gpui__src__text_system__line_layout.rs), window.rs (`InlineContent`, inline fragments), [taffy.rs](../evidence/core/crates__gpui__src__taffy.rs) `place_inline`, elements/div.rs | Go + layout/text services | A + visual |
| Scene kernel/paint ops/replay/order floor | [scene.rs](../evidence/core/crates__gpui__src__scene.rs), [scene/plan.rs](../evidence/core/crates__gpui__src__scene__plan.rs) | native kernel in DLL | A command oracle |
| Renderer primitives/filters/composition/paths/MSAA/atlas | [directx_renderer.rs](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_windows/src/directx_renderer.rs), directx_atlas.rs | native DLL | A (plan) + C (frames) |
| GPU completion/device loss/retirement/quarantine | renderer contract (Microsoft D3D11 docs) | native + Go leases | B protocol tests |
| Capture producer/import (same-device) | [screen_capture/windows.rs](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/platform/screen_capture/windows.rs), wgpu surfaces/windows.rs (local) | native in renderer service | B + C |
| Keyboard/mappers/layouts/dead keys/AltGr | [keyboard.rs](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_windows/src/keyboard.rs), events.rs | Go host | B transcripts |
| Actions/keymap/chords/contexts | [action.rs](../evidence/core/crates__gpui__src__action.rs), [key_dispatch.rs](../evidence/core/crates__gpui__src__key_dispatch.rs), keymap/context.rs | Go | A registry + B dispatch |
| Focus handles/release/tab order/paths | [platform.rs](../evidence/core/crates__gpui__src__platform.rs), window.rs | Go + host | B |
| IME (IMM32)/UTF-16 input handler | platform.rs traits, [events.rs](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_windows/src/events.rs) | Go host | B + nonzero-origin fixture |
| Accessibility (AccessKit/UIA) | gpui_windows window.rs a11y (pin), accesskit_windows 0.33.1 | native provider + Go tree | B |
| Assets/registry/embedded/on-demand | [assets.rs](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/assets.rs) | Go | A + B |
| Images (formats/EXIF/WebP modes/animation) | [platform.rs](../evidence/core/crates__gpui__src__platform.rs), [img.rs](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/elements/img.rs) | native decode tables | A |
| SVG (parse/raster/fonts/emoji/alpha-mask/8192) | [svg_renderer.rs](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/svg_renderer.rs) | native tables | A |
| HTTP client (Null default/injection) | [http_client.rs](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/src/http_client.rs) | Go interface | B (Null default asserted) |
| Clipboard (formats/metadata/images) | [clipboard.rs](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_windows/src/clipboard.rs), platform.rs | Go + narrow adapters | B |
| Platform credentials (Windows Credential Manager) | gpui_windows platform.rs `CredWriteW`/`CredReadW` (pin) — separate from clipboard | Go host | B |
| Drag-drop/Direct Manipulation/dialogs/menus/jump list/notifications | platform.rs, destination_list.rs, direct_manipulation.rs (pin) | Go host + native COM adapters | B |
| Scheduler/tasks/detached registry/foreground FIFO | [executor.rs](../evidence/core/crates__gpui__src__executor.rs), scheduler (pin) | Go | A/B deterministic |
| Test infrastructure (deterministic dispatcher) | test_context.rs, platform/test/dispatcher.rs (local) | Go | prerequisite for B |

## 2. Optional feature applicability and reference profiles

Three **CE reference profiles** make the target precise (no blanket exceptions claimed or implied):

- **P1 — native Windows default + capture**: the pinned `gpui` default feature set on Windows (`wayland`, `x11`, `windows-manifest` per the [manifest](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui/Cargo.toml)) plus `screen-capture` — the selected artifact's compiled set. Effective feature graph recorded at build (a lock is not an activation report).
- **P2 — debug/test capabilities preserved**: inspector element ids (compiled under plain `debug_assertions` in the pin, not only the `inspector` feature), the deterministic test dispatcher (`test-support`), and leak-detection diagnostics. The Go port preserves the debug behaviors; the interactive inspector UI is an owned capability (slice S16), not deferred.
- **P3 — stock optional wgpu path**: `gpui_wgpu` (wgpu 29.0.4, Windows D3D12, capture shared-handle import) behind `custom-gpu`/`wgpu-surfaces` — a **distinct backend and opt-in API surface**, not part of P1 and not claimed by the Go artifact. No accepted exception exists for either hot-patching or the wgpu path: the initial artifact simply does not compile them, and applicability review stays open until full-core-parity claims are made.

**Mechanism classification** (corrected): `stacker`/`stacksafe` and `profiler` are **build hardening/diagnostic mechanisms, not layout-algorithm or public-core behavior** — the pin's `taffy.rs` compiles `StackSafe<T>` as an identity alias without the feature (`#[cfg(not(feature = "stacker"))] type StackSafe<T> = T`), and `stacker` is absent from P1 defaults. The extraction preserves the **reference effective feature graph** (stacker off, profiler off in P1) with bounded failure tests for deep-recursion and diagnostic behavior whatever they are — no unverified mandatory dependency is introduced. `hot-patching` is a dev-toolchain mechanism (subsecond/dioxus-devtools), applicability recorded, not compiled.

**Windows-unsupported operations the Go port must reproduce** (parity includes matching unsupported behavior — verified in pinned sources): native anchored popups are **rejected on Windows** ("Native popups are not implemented on Windows yet. Rejecting lets callers fall back to gpui's in-window popovers", [window.rs](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_windows/src/window.rs)); `hide_other_apps`/`unhide_other_apps` are `unimplemented!()` with `todo(windows)` and `path_for_auxiliary_executable` bails "not yet implemented" ([platform.rs](https://github.com/gpui-ce/gpui-ce/blob/254b5dbd47cbb5acbcc5bbdcbb322a339276c88a/crates/gpui_windows/src/platform.rs)); the DirectX renderer **bails "unsupported surface source"** for non-`WindowsCapture` sources (directx_renderer.rs). The Go host reports exactly these behaviors — including the popup fallback — rather than claiming a new wgpu backend or silently "fixing" them. The custom-GPU API's Windows-platform support in the pin was **not verified** in this review; its applicability row stays open (P3 review) instead of being asserted either way.

## 3. Architecture blockers vs execution needs

**No true architecture blocker remains.** Ticket 10's positives passing removes the last compile-level design risk (negatives failing as intended confirms the type-constraint design; the stderr-wrapping fix is recording work, not design). Residual items are execution facts, ranked: (1) module-zip 500 MiB vs combined DLL size — measure at first artifact build; the distribution contract's revision path exists if exceeded; (2) oracle recordings (all Tier A evidence depends on them); (3) MSAA 4× path and capture producer on the actual GPU; (4) ABI record double-definition drift (generator + round-trip tests); (5) the caret-coordinate discrepancy — **an open oracle question** (fixture decides; no default reading is asserted).

## 4. Oracle, input, and transcript schemas

Unchanged from the prior revision (JSON schemas for LayoutFixture, SceneFixture, TextFixture, KeymapFixture, InputTranscript, IMETranscript, OwnershipTranscript, A11yTranscript, EnvironmentManifest, VisualCalibration — [research 19](19-conformance-update-policy.md) §2 carries the matrix). Minimum fixture sets per focus area remain as recorded there: actions/keymap/chords/focus (registry semantics, multi-matcher chords with stop-propagation defaults, context-stack dispatch, focus release/tab order); assets/HTTP (Null default asserted — the pinned "No HttpClient available" error with zero network attempts; one injected fake client; duplicate-path errors; loading-delay transitions); image modes/lazy fonts (WebP static vs animated entry points exercised separately, EXIF orientation matrix, GIF delays with reduce-motion, SVG lazy `NeedsFontAssets` first-use with bytes arriving exactly once, emoji fallback, alpha-mask icons, 8192 cap); UIA/IME (activation-before-tree, concurrent provider queries, teardown queries, JCK commit/cancel/focus-loss with UTF-16 ranges and surrogates, candidate placement at nonzero origin, after move/scroll, and across DPI change — the discrepancy fixture); close/reentry/scheduler (two-window shared-entity close, modal pumping, cancel-next, deferred activation, coalescing/re-notify, FIFO emits, virtual-time ownership suite); GPU/query/capture (event-query retirement vs Present-only, DONOTFLUSH with flush-when-quiescing, device-loss cycle, capture copy-before-pool-return, repeat-frame caching); distribution (clean-machine build, typed-error injection, concurrent publication, killed-writer recovery, bundle override, wrong-arch/ABI/hash, `.syso` inspection).

## 5. Visual gate: calibration process frozen, not numbers

Unchanged: controlled per-feature captures, ROI reporting (no blanket masks), reference repeat-run noise floor, injected-defect detection before thresholds, metric chosen during implementation and only then frozen. SSIM is an available diagnostic; no 98%-pixel or SSIM score is asserted; no parity claim before calibrated thresholds exist.

## 6. Vertical implementation slices with blocker edges

Reordered (corrected): the oracle harness comes **first** (every Tier A gate consumes its outputs), the **completion baseline precedes rendering acceptance** (visual gates are meaningless without retirement semantics), capture/loss are advanced slices, and inspector + remaining host operations are owned.

| # | Slice | Owns | Gates | Blocked by |
|---|---|---|---|---|
| S1 | **Oracle/fixture harness** | fixture runners, oracle recordings, environment manifests, calibration tool | runs everything below | — |
| S2 | ABI bootstrap + loader + artifact identity | `internal/abi`, nativeloader, manifest, typed errors | A round-trips, error injection | S1 (schemas) |
| S3 | Win32 host skeleton | locked thread, OLE, pump, dispatcher window, wakes, DPI guard | B transcripts with stub services | S2 |
| S4 | Entity/scope/effects core | ownership runtime | A/B virtual-time suite | S1 |
| S5 | **Completion baseline** | event-query protocol, ensured submission, loss quarantine primitives, lease ledger | B protocol tests | S2 |
| S6 | Layout service vertical | style→records→Taffy→bounds+snapping | A layout oracle | S1, S2 |
| S7 | Renderer vertical I: window/present | HWND lease, swap chains (DComp/HWND), clear/present, resize/DPI | B modes; **visual acceptance only after S5** | S3, S5 |
| S8 | Renderer vertical II + scene kernel | paint commands→plan→quads/shadows/underlines, atlas | A scene oracle + C frames | S6, S7 |
| S9 | Text vertical | shape/paragraph/carets, mono/subpixel/poly atlas | A text fixtures; on-screen text | S6, S8 |
| S10 | Authoring surface + counter app | public `gpui` API, children, events/actions | compile (ticket 10) + counter via S6–S9 | S4, S9 |
| S11 | Keyboard/actions/keymap | focus tree, chords, mappers, keymap JSON | B dispatch transcripts | S3 |
| S12 | IME vertical | IMM32, UTF-16 handler, marks, candidates | B IME transcripts incl. nonzero origin | S11 |
| S13 | Accessibility vertical | AccessKit tree/update/action, provider threads | B a11y transcripts | S3 |
| S14 | Clipboard + credentials + COM adapters | formats, metadata, Credential Manager, drag-drop, dialogs | B interop transcripts | S3 |
| S15 | Assets/images/SVG/HTTP | registry, decode modes, lazy fonts, Null HTTP | A fixtures + B sources | S6 |
| S16 | **Inspector UI + remaining host ops** | inspector element tree/panels (owned, not deferred), jump lists, notifications, restart, power, unsupported-op parity (popup fallback, `todo(windows)` behaviors) | B + debug-capability checks | S3, S8 |
| S17 | **Capture + advanced loss** | same-device producer, frame retention, full loss cycle, quarantined retirement | B protocol + C captures | S5, S8 |
| S18 | Scheduler/shutdown integration | tasks, detached registry, close ordering, multiwindow | B close/reentry suite | S4, S5 |
| S19 | Distribution finalization | clean-machine consumer, `.syso` tooling, publication prep | B consumer gates | S2, all |

## 7. Residual design gaps → concrete answers

Updated table: HTTP default = Null matching the pin (`net/http` adapters explicit-only); WebP duality = explicit decode-mode record; bundled SVG fonts = lazy `NeedsFontAssets` request-resume; **caret coordinates = open oracle question (fixture decides — no asserted reading)**; capture producer = same-device adapter (QI `IDXGIDevice` from the D3D11 device, HRESULT checked, frame retention until copy); GPU completion = event-query protocol with ensured submission, quarantine on loss; visual metric = frozen calibration process; module-zip size = measure at first build; **stacker/profiler = preserve reference effective feature graph, bounded failure tests, no mandated dependency**; **wgpu/custom-GPU and hot-patching = not compiled in P1, no accepted exception, applicability review open**; unsupported Windows operations = reproduce pinned behavior (popup fallback, `todo(windows)` errors, unsupported surface bail).

## 8. Specification readiness

Implementation-ready now: every family has an owner, gate, and fixture schema; feature applicability is classified by mechanism with three precise reference profiles; the compiler-positive milestone closes the last design-level uncertainty (negatives confirm the constraints; the record finalizes with main's stderr fix). Remaining items are execution facts owned by named slices: oracle recordings (S1), the size measurement (S19 precondition), on-device MSAA/capture (S17), calibration (S1 tool + S7/S8 captures), and the caret-coordinate fixture (S12). Nothing here is parity evidence until the gates run.
