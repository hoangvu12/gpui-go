# Resume after compaction or explicit chat transfer

Working directory: `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go`. Updated 2026-10-06 by the successor implementation chat (Roboco `386673cc-7e44-476f-94bd-50aaa0aa05b2`, harness `pi`, model `iroha/dashscope/glm-5.3`, branch `implement-core-remaining` + draft PR, main pushed after each resolved batch).

## Implementation authorization (supersedes all older planning-only restrictions)

**2026-10-05 — full implementation authorized** ("ok now use roboco mcp, create a new chat that use /implement skill to do all those tickets"). This chat and its predecessor implement ALL tickets in `.scratch/gpui-core-implementation/plan.md` (01–34) until complete or a concrete external blocker. Routine engineering decisions are delegated; do not re-ask whether to start/continue.

**2026-10-06 — user hard constraints (successor-chat handoff doc):**
- **Minimize Rust. NO cargo/DLL rebuilds without explicit user approval.** Pure-Go/Win32 tickets by default. Tickets that would want DLL/native additions: 17 (image codecs), 19 (SVG/resvg), 22 (AccessKit UIA), 28 (capture device exposure — the renderer ABI has no device/shared-texture export; `SurfaceSource::Unsupported` stand-in returns `ERR_UNSUPPORTED_SOURCE`). Defer these or propose pure-Go/Win32 alternatives per-ticket; surface to the user at checkpoints. Pure-Go path remaining without them: 13, 14, 15, 18(partially—needs 17), 20, 21, 23(needs 17), 24, 25, 26, 27, 30(partial), 31, 32(needs 28/29)… see frontier below.
- Resource care: keep artifacts small; never full-workspace `cargo test`; if a rebuild is ever approved: `-j 4`, idle hours only.
- Subagent discipline: parallel implementers with LEAN prompts (large prompts correlated with aborts); never auto-retry aborted subagents — surface and continue elsewhere or do the slice in the orchestrator.
- **Git:** remote https://github.com/hoangvu12/gpui-go (PUBLIC), branch `main`; commit and push after each resolved ticket batch. Work happens on `implement-core-remaining` (draft PR referencing spec+plan; created after the first batch commit since GitHub needs a diff); each batch merges to `main` and pushes. **No LICENSE file — the user must choose one (GPUI-CE is Apache-2.0); do not add one.**
- Verification: orchestrator re-runs tests itself; full suite `CGO_ENABLED=0 go test ./... -count=1` (18 packages green at handoff, 2026-10-06). Plain `go test ./...` for quick cached iterations.

`reference/ce-source` and `reference/target` are gitignored (re-fetch via reference/README.md). The embedded DLL and recorded traces are committed, so Go-only work needs no Rust. `.gitattributes` pins `* -text`. Toolchain: Go 1.27.1 (GOTOOLCHAIN=local), CGO_ENABLED=0 everywhere (race detector unavailable; forced-GC tests cover memory hazards).

## The delegation pattern (KEEP USING IT)

Parallel background implementer subagents (Agent tool, `background: true`, LEAN prompts: ticket path + contract pointers + pinned-source pointers + file ownership + per-package test command + "no git, no cargo, honest deviations"). Sessions resumable, but never auto-retry aborts.

1. Disjoint file ownership per subagent; same checkout (no worktrees — proven pattern). Subagents keep the `gpui` package compiling at all times; they run ONLY their packages (`CGO_ENABLED=0 go test ./gpui ./internal/<spec>` — never `./...` while others run).
2. Oracle-first applies to native-seam tickets (fixture kind + recorded trace + bit-exact `conformance.CompareTraces` gate). Go-only tickets verify with spec packages whose exact expectations derive from pinned CE semantics + existing fx gates stay green (no new recorded fixtures without Rust approval).
3. Orchestrator (this chat): claims tickets in the issue files before delegating, verifies subagent claims by re-running tests, resolves tickets (`## Answer`, evidence link), writes `evidence/ticketNN-*.json`, updates `evidence/README.md` + this note, commits and pushes per batch.
4. Ticket lifecycle per docs/agents/issue-tracker.md: `Status: claimed/Assignee` before work; resolve only with acceptance evidence.

## Completed (14 tickets, all with executed evidence in evidence/)

| Ticket | Result | Gate |
|---|---|---|
| 01 reference harness + ledger | pinned CE oracle, envelope/trace v1, capability ledger (6911 rows, schema @2) | fx-0001 mismatch demonstration |
| 02 native bootstrap | DLL loader (embed/bundle/cache, SYSTEM32-only, verified handle), bootstrap ABI | round trips + 16 loader tests |
| 03 entities/effects | real root gpui runtime (entities, leases, scopes, events, effects, executors) | fx-0001 effects 47/47 |
| 04 scoped tasks | exactly-once delivery/discard, generation gating, shutdown drain, seeded schedules | 24 tests |
| 05 Win32 window host | OLE-STA/PerMonitorV2 host, two-window lifecycle, pooled-thread WM_QUIT deadlock fixed | 7 real-window tests, 10/10 runs |
| 06 layout roundtrip | taffy 0.13.0 native service + Go adapter with pinned rounding | fx-0001 54/54 + fx-0002 63/63 bit-exact |
| 07 present+retire | D3D11 renderer service, both swap-chain modes, event-query retirement | real-GPU ledger proofs (RTX 5050) |
| 08 scene kernel | pinned scene.rs/plan.rs native kernel + Go paint API | fx-0003 142/142 bit-exact |
| 09 text geometry | Parley stack native service + Go text adapter | fx-0004 339/339 bit-exact |
| 10 draw text | DirectWrite glyph raster + D3D11 atlas service + Go draw path (ported pinned rasterizer with citations) | fx-0005 + real-window pixel verification |
| 11 authoring counter | typed counter + fluent custom components + two-window corpus | fx-0006 + authorspec |
| 12 action registry | typed actions, NoAction/Unbind reference semantics, boxed clones | 27 tests |
| 16 paths/filters | scene v3 path kernel + renderer v2 full draw pipeline (68 prebuilt DXBC, MSAA paths, filters, surfaces pipeline, staging readback) | fx-0007 428/428 bit-exact + pixel-verified composite draw |
| 34 ledger triage | 351 support rows dispositioned; regeneration deterministic | — |

**Current native artifact (do not rebuild without approval):** DLL 12,489,728 bytes, sha256 `d648523b7fe1173a8810a27de77ff8ecbfebaa53eab95d69a6335c4a042b87dd`, native revision 7, capability mask 0xFF (bootstrap/layout/renderer/scene/text/glyph-raster/glyph-atlas), static CRT, OS-only imports. Reference harness: `reference/target/debug/gpui-reference-harness.exe`. Fixture kinds v1: layout-effects, layout-metrics, scene-painting, paths-filters, text-geometry, glyph-raster, authoring-counter (fx-0001..0007 all byte-deterministic and reproduced bit-exactly).

## Current frontier and wave state (2026-10-06, successor chat)

Resolved: 01–12, 16, 34. **Wave 1 in flight (4 parallel background subagents): 13 (focus/keyboard), 15 (inline layout), 25 (dialogs/credentials), 31 (consumer delivery).**

- After 13 resolves: frontier adds 14 (retained scroll), 20 (IME editor), 21 (accessibility tree), 24 (drop/touchpad, needs 23), 26 (desktop operations).
- After 15 + 31: nothing new unblocks until 30/33 chains complete (33 needs 15✓,30,31✓,32).
- **Deferred pending user decision (want native/DLL work): 17, 19, 22, 28.** Their dependents stall: 18 (17), 23 (17), 27→29→32→30→33 partially (28/29/30/32/33). At the pure-Go exhaustion point, present the user: approve one bounded DLL rebuild for 17 (image codecs) or accept pure-Go alternatives per ticket (e.g. Go stdlib + x/image/webp for 17 with animated-WebP as explicit unavailable; CPU-staging capture for 28 if a device handle were exposed — it currently is not).
- Ticket 13 history: attempted by the predecessor chat, aborted before any files were written; nothing partial on disk.

## Verification state (2026-10-06, at wave-1 start)

`CGO_ENABLED=0 go test ./... -count=1` — 18 test packages green. `go vet ./...` clean, gofmt clean. fx-0001..fx-0007 all pass.

## Key conventions and gotchas

- Envelope JSON keys snake_case (Rust serde defaults); effect ops kebab-case tagged; trace f32 values `"f32:XXXXXXXX"` (8 uppercase hex); CompareTraces exact (first mismatch names event seq/field).
- Reference window scale factor 2.0; rem = 16 logical px; measurement snapping `ceil(max(0,x)*scale)`; rounding midpoint-toward-zero (util.rs).
- Recorded traces embed machine-dependent facts (Segoe UI etc. via pinned Parley store; FontId canonical bits) — deterministic on THIS machine; evidence records the environment.
- OLE STA required on the foreground host; winhost tests open REAL windows — must FAIL not skip if window creation fails.
- Ledger: `go run ./conformance/cmd/ledgergen -scan reference/out/api-scan.json -out conformance/ledger/generated -profiles-dir reference/out/profiles` (schema @2).
- Rust (only with approval): `cd reference && RUSTFLAGS="-C target-feature=+crt-static" cargo build --release -p gpui-go-native --target x86_64-pc-windows-msvc` then `go run ./reference/native/tools/measure` (refreshes manifest + embedded DLL; update cumulative mask assertions in internal/native/layout_test.go and internal/scenespec/scene_spec_test.go — established additive pattern). Harness builds fast in debug.
- CE pin `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a` (reference/ce-source, zero patches). Zed `a84689073d296dfd39987bc7dd478e43ef76d83a` comparison only. ~98% visual closeness is intent, not a threshold; calibration is ticket33.

## Historical planning notes

See [PROJECT.md](../PROJECT.md) and the sections below: Wayfinder map, specification, conformance contract/inventory, 34-ticket plan.
