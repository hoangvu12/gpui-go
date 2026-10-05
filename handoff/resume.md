# Resume after compaction or explicit chat transfer

Working directory: `C:\Users\ADMIN\Desktop\nnguyenvu\gpui-go`. Updated 2026-10-05 (implementation phase, 10 tickets resolved).

## Implementation authorization (supersedes all older planning-only restrictions)

**2026-10-05 — full implementation authorized.** The user: "ok now use roboco mcp, create a new chat that use /implement skill to do all those tickets, use iroha dashscope glm 5.3 with pi harness". This chat (Roboco `065e52f5-9c7f-4d65-8566-f4bb485196a1`, harness `pi`, model `iroha/dashscope/glm-5.3`) implements ALL tickets in `.scratch/gpui-core-implementation/plan.md` (01–34) until complete or a concrete external blocker. Later user guidance: **work in parallel background subagents; minimize Rust work (but do not skip it where contracts require it)**. Routine engineering decisions are delegated; do not re-ask whether to start/continue.

**Git state:** NO git repository exists (verified). The implement skill's commit step is environment-blocked — never fabricate commits, never init unrelated repos, never publish remotely. Record the limitation in each ticket's evidence.

**Toolchain:** Go 1.27.1 (`C:\Program Files\Go\bin\go.exe`, GOTOOLCHAIN=local), Rust stable 1.97.0 x86_64-pc-windows-msvc + VS2022 BuildTools, gh CLI, network OK. All commands run with `CGO_ENABLED=0`. The race detector cannot run (needs cgo) — forced-GC tests cover memory hazards instead.

## The delegation pattern (KEEP USING IT)

Work is executed by **parallel background subagents** (Agent tool, `background: true`, one call each with precise prompts; sessions are resumable — failed/wrong work gets one corrective call with `session` set). Verified wave structure:

1. Each ticket = one or more subagents with **explicit file ownership** (disjoint to avoid conflicts), pointers to the canonical contract + pinned source files + the exact oracle/gate, and quality gates (`CGO_ENABLED=0 go test <their packages>`, vet, gofmt; never `./...` while others run — the orchestrator runs the full suite at integration).
2. **Oracle-first**: extend `reference/harness` (Rust) with a fixture kind + envelope JSON under `conformance/fixtures/`, record 3×-deterministic traces into `conformance/recorded/` via `go run ./conformance/cmd/recordreference`, then the Go port must reproduce the trace **bit-exactly** via `conformance.CompareTraces` (gate test in `internal/portfixture`).
3. Native services grow the DLL (`reference/native`, slots: 0 bootstrap, 1 layout/taffy, 2 renderer/D3D11, 3 scene kernel, 4 text/Parley; next: 5+). After any rebuild: rerun `go run ./reference/native/tools/measure` (refreshes `reference/out/native-bootstrap.json` + `internal/native/artifacts/` DLL+manifest) and update the cumulative capability-mask assertions in `internal/native/layout_test.go` and `internal/scenespec/scene_spec_test.go` (the established additive pattern, e.g. 0x1F→next).
4. The orchestrator (main chat): verifies subagent claims by re-running tests, resolves tickets in `.scratch/gpui-core-implementation/issues/` (check boxes, `## Answer` with evidence link), writes `evidence/ticketNN-*.json`, updates `evidence/README.md` index + this resume note.
5. Ticket claims: claim via the issue file's Status/Assignee before delegating; resolve only with acceptance evidence.

## Completed (10 tickets, all with executed evidence in evidence/)

| Ticket | Result | Gate |
|---|---|---|
| 01 reference harness + ledger | pinned CE oracle, envelope/trace v1, capability ledger (6911 rows, 5 profiles, schema @2 after triage) | fx-0001 mismatch demonstration |
| 02 native bootstrap | DLL loader (embed/bundle/cache, SYSTEM32-only, verified handle), bootstrap ABI | round trips + 16 loader tests |
| 03 entities/effects | real root gpui runtime (entities, leases, scopes, events, effects, executors) | fx-0001 effects 47/47 |
| 04 scoped tasks | exactly-once delivery/discard, generation gating, shutdown drain, seeded schedules | 24 tests |
| 05 Win32 window host | OLE-STA/PerMonitorV2 host, two-window lifecycle, deadlock fixed (pooled-thread WM_QUIT poisoning) | 7 real-window tests, 10/10 runs |
| 06 layout roundtrip | taffy 0.13.0 native service + Go adapter with pinned rounding | **fx-0001 54/54 + fx-0002 63/63 bit-exact** |
| 07 present+retire | D3D11 renderer service, both swap-chain modes, event-query retirement | real-GPU ledger proofs (RTX 5050) |
| 08 scene kernel | pinned scene.rs/plan.rs native kernel + Go paint API | **fx-0003 142/142 bit-exact** |
| 09 text geometry | Parley stack native service + Go text adapter | **fx-0004 339/339 bit-exact** |
| 12 action registry | typed actions, NoAction/Unbind reference semantics, boxed clones | 27 tests |
| 34 ledger triage | 351 support rows dispositioned; unassigned 0 | regeneration deterministic |

Also landed: `conformance` package (envelope/trace/compare/runner + cmd/recordreference + cmd/ledgergen), `internal/native` (loader + 4 services), `internal/portfixture` (the port fixture runner: fx-0001/2/3/4 gates), spec test packages (taskspec/actionspec/layoutspec/scenespec/textspec/winhostspec/rendererspec), `gpui` runtime + style + layout + scene + paint + text + window + win32host + renderer + action files.

**Current native artifact:** DLL 7,579,648 bytes, sha256 `2d8e4a3dd5812143b6a1baa5ccedd9c3ae5ac55ca6240ccd7a65028a0e7517d7`, revision 5, capability mask 0x1F (bootstrap/layout/renderer/scene/text), static CRT, imports only OS components (incl. d3d11/dcomp/dxgi/dwrite). Reference harness: `reference/target/debug/gpui-reference-harness.exe`.

## Current frontier and next steps

Resolved so far: 01,02,03,04,05,06,07,08,09,12,34. **Open frontier (all blockers resolved): 10, 16, 17.**

- **Ticket10 (draw text in all required glyph formats)** — blocked by 08✓,09✓. Needs: native glyph rasterization (the pinned `WindowsGlyphRasterizer` is `pub(crate)` in gpui_windows — port its DirectWrite rasterization code INTO `reference/native` from the pinned `crates/gpui_windows/src/font_rasterizer.rs` with citations), atlas insertion service (pinned `crates/gpui_windows/src/directx_atlas.rs`), and the Go draw path (paint_glyphs → sprites in the scene). Oracle: raster records (bounds, formats mono/subpixel/polychrome, bitmap geometry) via the public `RasterizedGlyph` + draw integration. One subagent owning reference/native + reference/harness + conformance fixture + gpui (no conflicts).
- **Ticket16 (paths/filters/full scene plan)** — blocked by 08✓: extend the scene service with paths/surfaces/sprites + nested filters (already partially present: MAX_FILTER_GROUP_DEPTH=2 semantics ported; paths/sprites slots reserved in SCENE_ABI.md).
- **Ticket17 (image decode)** — blocked by 08✓: native image codec service (pinned image crate surface per the distribution contract) + Go entry modes.
- Then: 11 (authoring counter; 03✓+10), 13 (focus/keyboard; 05✓+11+12✓), 14, 15, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32, 33.

**Sequencing rule:** native-service tickets serialize (reference/native rebuilds + artifact refresh); Go-only work parallelizes. After each native rebuild, rerun measure + fix cumulative mask assertions. Wave pattern per ticket: oracle fixture first (or in the same subagent), then the gate must be bit-exact; no mocked happy paths.

## Verification state (2026-10-05, after ticket09)

`CGO_ENABLED=0 go test ./... -count=1` — all 13 test packages green. `go vet ./...` clean, `gofmt` clean. Reference traces: fx-0001 (54 events), fx-0002 (63), fx-0003 (142), fx-0004 (339) — all byte-deterministic and reproduced bit-exactly by the port.

## Key conventions and gotchas

- Envelope JSON keys are snake_case matching Rust serde defaults; effect ops kebab-case tagged. Trace f32 values are `"f32:XXXXXXXX"` (8 uppercase hex). CompareTraces is exact (first mismatch names event seq/field).
- The reference test window's scale factor is hardcoded **2.0**; rem default 16 logical px; the pinned measurement snapping is `ceil(max(0,x)*scale)`; rounding is midpoint-toward-zero (util.rs).
- Recorded traces embed machine-dependent facts (fonts: Segoe UI etc. resolve through the pinned Parley store; FontId canonical bits). They are deterministic on THIS machine; evidence records the environment.
- The Windows contract requires OLE STA on the foreground host; winhost tests open REAL windows (interactive session — they must FAIL not skip if window creation fails).
- Ledger: `go run ./conformance/cmd/ledgergen -scan reference/out/api-scan.json -out conformance/ledger/generated -profiles-dir reference/out/profiles` (schema @2, disposition field).
- Rust builds: `cd reference && RUSTFLAGS="-C target-feature=+crt-static" cargo build --release -p gpui-go-native --target x86_64-pc-windows-msvc` then measure. The harness builds fast in debug (`cargo build -p gpui-reference-harness`).
- Subagent prompts must include: file ownership lists, "no git; do not commit", "CGO_ENABLED=0", pointers to canonical contracts + pinned sources, the exact gate/oracle, and the instruction to report deviations honestly (never silently).

## Historical planning notes (superseded for authorization, retained as evidence)

See the sections below and [PROJECT.md](../PROJECT.md) for the planning history: Wayfinder map (10 resolved children), the specification, conformance contract/inventory, and the 34-ticket plan. The CE pin is `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a` (checkout at `reference/ce-source`, zero patches). Zed `a84689073d296dfd39987bc7dd478e43ef76d83a` is comparison only. Approximately 98% visual closeness is intent, not an SSIM threshold — calibration is ticket33.
