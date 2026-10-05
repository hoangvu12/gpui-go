# Ticket 34: core-support triage disposition record

This is the durable disposition record for
[34-core-support-triage](../../../.scratch/gpui-core-implementation/issues/34-core-support-triage.md):
the triage of every capability-ledger row that the ticket-01 enumeration left
in the `core-support-triage` family (owner 34). It documents, group by group,
which concrete disposition each row received, which gpui public surface
exercises it (with the pinned CE source citation at `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`),
and why structural or platform-inapplicable rows are not observable operations.

The dispositions are encoded as the replacement of the former
`core-support-triage` catch-all rule (old rule 110) with rules 110–126 in
`conformance/ledger/owners.go`; regeneration is deterministic and reproduces
this table's counts. Each row's `notes` field carries the machine-greppable
provenance (`disposition: …`, `platform-gate: …`), and each row's
`disposition` field records one of `family`, `core-structure` or
`platform-inapplicable` (ledger schema `gpui-go/capability-ledger@2`).

## Before / after

| Measure | Before | After |
|---|---:|---:|
| Total rows | 6911 | 6911 |
| Unassigned rows | 0 | 0 |
| Rows owned by ticket 34 (`core-support-triage`) | 351 | 0 |
| Rows in the `core-support-triage` family | 351 | 0 |
| Owner changes | — | exactly the 351 triage rows |

Disposition values across the whole ledger after the triage:

| Disposition | Rows | of which former triage rows |
|---|---:|---:|
| `family` (implementation owner ticket) | 6757 | 245 |
| `core-structure` (non-observable structure, owner marker `core-structure`) | 115 | 115 |
| `platform-inapplicable` (crate-level/item platform gate) | 39 | 6 |

The 33 non-triage `platform-inapplicable` rows are the pre-existing
`gpui_ce_macos` / `gpui_ce_apple` / `gpui_ce_web` / `gpui_ce_linux` rows that
already had domain owners (the `os-*` families); the triage added their
`platform-gate` notes (see below) without changing any owner. Exactly 351 rows
changed owner; every other row is byte-identical apart from the added
platform-gate notes on those four crates' rows.

## Disposition breakdown of the 351 triage rows

| New rule family | Owner | Rows | Disposition |
|---|---|---:|---|
| shared-string-value | 03 | 31 | family |
| collections-maps | 03 | 8 | family |
| collections-unused | core-structure | 5 | core-structure |
| sum-tree-lists | 14 | 89 | family |
| util-arc-cow | core-structure | 21 | core-structure |
| util-windows-shell | 26 | 3 | family |
| util-async-errors | 04 | 36 | family |
| util-deferred | 03 | 4 | family |
| util-entity-plumbing | 03 | 6 | family |
| util-debug-diagnostics | 27 | 3 | family |
| util-unused | core-structure | 3 | core-structure |
| zed-util-unused | core-structure | 72 | core-structure |
| perf-tooling | 01 | 49 | family |
| platform-inapplicable | 01 | 5 | platform-inapplicable |
| alternate-gpu (rule 5 extended with `WebBackendPreference`) | 01 | 2 | family / platform-inapplicable¹ |
| gpui-private-structure | core-structure | 9 | core-structure |
| gpui-doc-structure | core-structure | 2 | core-structure |
| gpui-reexport-structure | core-structure | 3 | core-structure |

¹ `gpui_ce_platform::WebBackendPreference` is item-cfg-gated (`target_family
= "wasm"`, profile empty — the cfg itself records the inapplicability), so its
disposition is `family`; `gpui_ce_web::WebBackendPreference` carries the
crate-level gate note and is `platform-inapplicable`.

By owner ticket: 01 → 56 rows, 03 → 49, 04 → 36, 14 → 89, 26 → 3, 27 → 3,
core-structure marker → 115.

## Group-by-group dispositions

### gpui-ce structural rows (14 rows → `core-structure`)

| Rows | Disposition | Rationale and pinned-source citation |
|---|---|---|
| `gpui::util` (module) | core-structure | Private module (`mod util;` at `crates/gpui/src/gpui.rs:66`); its public members are re-exported individually (`pub use util::{FutureExt, Timeout};` gpui.rs:175, `FluentBuilder` via the prelude) and already have their own ledger rows. |
| `gpui::private` (module) + `gpui::private::{anyhow, inventory, schemars, serde, serde_json}` (5 use rows) | core-structure | `#[doc(hidden)] pub mod private` re-exporting macro-support dependencies "for use by gpui_macros and such" (gpui.rs:80-87). No observable operation; a Rust macro-hygiene mechanism. |
| `gpui::seal` (module) + `gpui::seal::Sealed` (trait) | core-structure | Private `mod seal` implementing the sealed-trait pattern restricting trait implementations to GPUI (gpui.rs:89-93). No observable operation. |
| `gpui::_accessibility`, `gpui::_ownership_and_data_flow` (modules) | core-structure | Declared `#[cfg(doc)]` (gpui.rs:73-76): documentation-only modules, never compiled into any profile (their ledger profile membership is already empty). |
| `gpui::Result`, `gpui::ctor`, `gpui::ArcCow` (use rows) | core-structure | Bare re-exports with no operation of their own: `pub use anyhow::Result` (gpui.rs:96 — error semantics are exercised per-API by the owning families), `pub use ctor::ctor` (gpui.rs:99 — module-initializer attribute macro, Rust-specific mechanism), `pub use gpui_util::arc_cow::ArcCow` (gpui.rs:144 — unused by any gpui API, see util-arc-cow below). |

### gpui_ce_shared_string (31 rows → owner 03, family `shared-string-value`)

SharedString is a gpui public value type re-exported wholesale
(`pub use gpui_shared_string::*;` at `crates/gpui/src/gpui.rs:143`). Its
observable semantics — Arc interning (`new_static`/`new`),
`PartialEq` across static/owned/`Arc` representations, `Display`/`Deref`/
`AsRef`/`From` conversions, and serde/`JsonSchema` support — are exercised
through every public API that consumes it:

- action names and the `Unbind(SharedString)` payload (`action.rs:447`,
  ticket 12's name registry and action serialization),
- keymap context keys/values and binding `action_input`
  (`keymap/context.rs:16-18`, `keymap/binding.rs:124`, ticket 13),
- div group names, author ids and aria label/description/keyshortcut/value/
  placeholder (`elements/div.rs:901-1635`, tickets 11/21/14's group hitboxes),
- `impl Element for SharedString`, `Text::new`, `Line::text`,
  `shape_text`, `Font::family` and `font_family` styling
  (`elements/text.rs`, `text_system.rs:327,350,864,886`,
  `text_system/line.rs:15,89`, `style.rs:598`, tickets 15/09/06),
- window/tab titles (`app.rs:406,558`, ticket 05), asset/cache paths
  (ticket 18) and menu labels (`platform/app_menu.rs`, ticket 26).

Disposition: reassigned to ticket 03 (entities/data plumbing) as the shared
value-type owner — the task's defensible default — because no single family
dominates the consumer set; the consuming families exercise SharedString
through their own rows. The one SharedString-named row family outside the
crate (`gpui::elements::text::Element for SharedString`, 6 rows) is already
owned by ticket 15.

### gpui_ce_collections (13 rows: 8 → owner 03 `collections-maps`, 5 → `core-structure`)

| Rows | Disposition | Rationale and citation |
|---|---|---|
| `HashMap`, `HashSet`, `TypeIdHashMap`, `TypeIdHashSet` (type aliases) and `FxBuildHasher`, `FxHashMap`, `FxHashSet`, `FxHasher` (use rows) | 03 / family | Deterministic Fx-hash map machinery (no random seed; `collections.rs:1-9`) backing gpui's app/entity data plumbing, and **exposed through public APIs**: `App::tab_groups() -> &FxHashMap` (app.rs:447), `Window::take_views() -> FxHashSet` (window.rs:282), `EntityMap::extend_accessed(&FxHashSet)` and `accessed_entities` (entity_map.rs:58,174 — ticket 03's access dependency tracking), plus the action-registry maps (`ActionRegistry::deprecated_aliases/deprecation_messages/documentation` action.rs:402-410, ticket 12). Map iteration order is deterministic and observable through those APIs. |
| `IndexMap`, `IndexSet`, `Equivalent`, `collections::*`, `collections::vecmap` | core-structure | Not used by gpui: IndexMap/IndexSet appear only in `gpui_macos/src/window.rs` (a crate gated off Windows); `Equivalent` is indexmap lookup machinery for those maps; `vecmap` has no gpui call site; the `*` row is the `pub use std::collections::*` re-export whose std behavior is exercised through consuming APIs. No gpui public API exercises these. |

### gpui_ce_sum_tree (89 rows → owner 14, family `sum-tree-lists`)

The whole crate is the sum-tree machinery behind two gpui data structures:

- the **list/uniform_list element's measured-item tree** —
  `elements/list.rs:19` (`use sum_tree::{Bias, Dimensions, SumTree}`),
  `list.rs:65` (`items: SumTree<ListItem>`), `list.rs:1024`
  (`SumTree::from_iter(measured_items)`), and the `Item`, `Summary`,
  `Dimension` (Count/Height) and `SeekTarget` impls at `list.rs:1667-1742`.
  This drives list scrolling, remeasurement and keyed state — ticket 14's
  "scroll state, lists/virtualized lists" evidence.
- the **tab-stop focus order** — `tab_stop.rs:3,15`
  (`SumTree<TabStopNode>`, `order: SumTree::new(())`), exercised by ticket
  13's tab-order evidence.

Disposition: reassigned to ticket 14 (retained-scroll/lists) as the primary
consumer; the disposition note records the tab-stop usage so ticket 13 knows
it exercises the same machinery.

### gpui_ce_util (76 rows, split across six dispositions)

| Rows | Disposition | Rationale and citation |
|---|---|---|
| `arc_cow` module, `ArcCow` enum, `Borrowed`/`Owned` variants and all impls (21 rows) | core-structure | ArcCow is re-exported at `gpui.rs:144` but **no gpui public API (nor any Windows-compiled crate) uses it** — generic copy-on-write machinery with no gpui-observable operation. |
| `new_std_command` (windows + not-windows variants), `get_powershell` (3 rows) | 26 / family | Exercised by `WindowsPlatform::restart` — `get_powershell` and `new_std_command` at `crates/gpui_windows/src/platform.rs:15,499-511` (gpui `Platform::restart`, family platform-desktop). The `not(target_os = "windows")` `new_std_command` variant's cfg records its non-Windows-only nature. |
| `ResultExt` (+ trait methods/impls), `log_err`, `DebugAsDisplay` Display impl, `TryFutureExt`, `TryFutureExtBacktrace`, `LogErrorFuture`, `LogErrorWithBacktraceFuture`, `UnwrapFuture` (+ polls) (36 rows) | 04 / family | Async/result error propagation machinery: `executor.rs:5` and `asset_cache.rs` use the future extensions for task error logging; `ResultExt`/`log_err` propagate errors across `app.rs:36`, `assets.rs:183`, `elements/*` and `window.rs:38`. Exercised through task outcomes and error traces (ticket 04's output handoff / late-result discard). |
| `Deferred`, `Deferred::abort`, `Drop for Deferred`, `defer` (4 rows) | 03 / family | Scopeguard exercised by public effect and dispatcher APIs: `Context::on_drop` returns `Deferred` (`app/context.rs:276-284`, ticket 03 logical release), async effect deferral (`app/async_context.rs:307-310`), dispatcher decrement on drop (`platform/threaded_dispatcher.rs:61`) and `PlatformDispatcher::increase_timer_resolution` returning `TimerResolutionGuard` (`platform.rs:1136,1171-1173`, ticket 04). |
| `post_inc`, `TypeIdHashBuilder`, `TypeIdHasher` (+ BuildHasher/Hasher impls) (6 rows) | 03 / family | Entity identity plumbing: `post_inc` drives entity handle ids (`app/entity_map.rs:967`), `subscription.rs` and `window.rs` counters; the TypeId hashers key the app's TypeIdHashMap globals/event listeners/window invalidators (`app.rs:755,781,802`) and the action registry `names_by_type_id` (`action.rs:235`, ticket 12). Exercised through ticket 03's typed access/generations evidence. |
| `debug_panic`, `some_or_debug_panic`, `measure` (3 rows) | 27 / family | Debug diagnostics: `debug_panic` backs entity lifecycle assertions (`app.rs:36`); `measure` records frame durations under `ZED_MEASUREMENTS` (`window.rs:38,1806`). The inventory records plain debug assertions and diagnostics with ticket 27 (public debug/test behavior, profile distinctions). |
| `maybe`, `truncate_to_bottom_n_sorted_by`, `get_windows_system_shell` (3 rows) | core-structure | No gpui call site: `maybe!` is trivial closure-invocation sugar (`lib.rs:203-213`), `truncate_to_bottom_n_sorted_by` is referenced only by the unused gpui_ce_zed_util tests, and `get_windows_system_shell` is called only from the unused gpui_ce_zed_util shell module. |

### gpui_ce_zed_util (72 rows → `core-structure`, family `zed-util-unused`)

`gpui_ce_zed_util` is a workspace member with **no dependents**: no crate's
Cargo.toml references the workspace `util` dependency
(`util = { path = "crates/gpui_zed_util", …, package = "gpui_ce_zed_util" }`
is defined in the workspace root but never used). None of its items (archive,
command, fs, process, shell, shell_builder, shell_env, disambiguate, markdown,
path_list, paths, redact, schemars, serde, size, time modules and their
contents) is linked into the gpui reference graph on any platform, so no gpui
public API exercises them. Disposition: non-observable structure — not
silently dropped, visible as the `zed-util-unused` family with the marker
owner.

### perf (49 rows → owner 01, family `perf-tooling`)

The `perf` crate (`tooling/perf`, "A tool for measuring GPUI test
performance") is dev-toolchain instrumentation: it is the test runner selected
by `.cargo/config.toml`'s `perf-test` alias
(`target.'cfg(true)'.runner='cargo run -p perf --release'`) and depends only
on collections/serde/serde_json. It is not a gpui dependency and not
observable through any gpui public API. Disposition: ticket 01 (the inventory's
applicability owner for dev-toolchain/instrumentation mechanisms — the same
family that owns hot-patching/profiler/stacker applicability) records the
toolchain applicability and any Go replacement.

### Non-Windows platform crates (web 3, linux 1, platform 3 triage rows)

These rows are **inapplicable-by-platform, not exceptions**. No
`approved-exception` state was used: an exception record requires narrow
provenance and approval, while a platform gate is an applicability fact.

| Rows | Disposition | Rationale and citation |
|---|---|---|
| `gpui_ce_web::WebBackendPreference` | 01 / alternate-gpu, platform-inapplicable | Re-export of the wgpu web backend preference (`pub use gpui_wgpu::WebBackendPreference`, `gpui_web.rs:17`): web GPU backend selection is alternate-GPU applicability (ticket 01), matching how rule 5 already owns `gpui_ce_wgpu` and `gpui_ce_macos::wgpu_renderer`. The whole web crate is `#![cfg(target_family = "wasm")]` (`gpui_web.rs:1`). |
| `gpui_ce_platform::WebBackendPreference` | 01 / alternate-gpu, family | Same symbol re-exported by the platform crate under `#[cfg(target_family = "wasm")]` (`gpui_platform.rs:35-37`); the item cfg already yields an empty profile list, so the inapplicability is visible without a note. |
| `gpui_ce_web::logging`, `gpui_ce_web::init_logging` | 01 / platform-inapplicable | Wasm platform support (browser logging/panic hooks), gated by the crate-level `#![cfg(target_family = "wasm")]`. No Windows-side domain family exists. |
| `gpui_ce_linux::linux` | 01 / platform-inapplicable | The linux platform implementation module, gated by the crate-level `#![cfg(any(target_os = "linux", target_os = "freebsd"))]` (`gpui_linux.rs:1`). |
| `gpui_ce_platform::single_threaded_web`, `gpui_ce_platform::web_init` | 01 / platform-inapplicable | Wasm-only platform init (`#[cfg(target_family = "wasm")]`, `gpui_platform.rs:52-68`): `single_threaded_web` builds a single-threaded web `Application`, `web_init` installs wasm panic hooks and logging. Windows platform selection runs through `current_platform` (ticket 05, rules 90-92 already own `background_executor`, `headless`, `application`/`current_platform`). |

In addition, every row of a crate-level gated platform crate
(`gpui_ce_web` 18, `gpui_ce_linux` 2, `gpui_ce_macos` 15, `gpui_ce_apple` 2
rows) now carries a `platform-gate:` note citing the crate's cfg, because the
api-scan records only item-level cfg and would otherwise make those rows look
Windows-applicable in all profiles. Their domain owners are unchanged
("owner stays"); the note and the `platform-inapplicable` disposition record
the target_os inapplicability.

## Notes for the final audit (ticket 33)

- No row remains in `core-support-triage` or owned by ticket 34;
  `summary.unassigned_count` is 0 and `summary.by_owner["34"]` is 0.
- `core-structure` rows (115) are documented non-observable structure: they
  are not implementation work and cannot block a parity claim. They remain in
  the `planned` state because the contract's evidence states describe
  operation outcomes; the disposition, not the state, records that they are
  not operations.
- `platform-inapplicable` rows (39) are not applicable on Windows: their
  crate-level or item cfg removes them from the Windows core. They are not
  approved exceptions and must not be counted as silently dropped coverage —
  they are listed here and in the ledger with the gate citation.
- Determinism: the disposition rules are pure functions of the scan rows;
  regenerating the ledger reproduces these counts exactly
  (family=6757, core-structure=115, platform-inapplicable=39 over 6911 rows).
