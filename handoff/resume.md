# Resume after compaction or explicit chat transfer

Working directory: `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go`. Updated 2026-10-11 by the wave-3 implementation chat (Roboco `3a946d3c` "Wave 3: asset loading + clipboard (tickets 18, 23)", harness `pi`, model `iroha/dashscope/glm-5.3`, branch `implement-core-remaining`; batch 4 = tickets 18+23, batch 5 = ticket 24, batch 6 = ticket 27, batch 7 = ticket 19 (the user-approved second DLL rebuild), all merged to main and pushed; batches 6-7 under the user's light-load instruction). Predecessor: wave-2 chat `efb5f590` (retired after batch 2 = d7f2c5b, batch 3 = de99fb0 + 6d10ff3).

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

## Completed (28 tickets, all with executed evidence in evidence/)

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
| 13 focus/keyboard | focus/keys/keymap/dispatch runtimes + the gpui_windows keyboard path (accelerator pre-dispatch, AltGr/dead-key translation, surrogate WM_CHAR, layout reports) | 17 reference test ports + focusspec 12 tests (10 deterministic transcripts incl. two-window isolation + 2 real-host input evidences) |
| 18 asset loading | the pure-Go asset machinery over the frozen rev-8 codec: registry (duplicate diagnostics, embedded/pre-loaded/on-demand), HTTP policy (Null default zero-attempt, Blocked permission errors, explicit net/http adapter), three-state image cache with shared-task dedup + scope cancellation/result discard, rational-delay animation with reduce-motion + 200ms loading delay | assetspec 46 tests + gpui 150 green |
| 23 clipboard | the Win32 clipboard exchange over the real system clipboard: six registered formats, multi-entry text/metadata (seahash-gated)/files/images, CF_DIB→BMP, exactly-once guard/lock, HGLOBAL ownership transfer, busy/malformed handling, primary/find-pasteboard unavailable, real PowerShell native interop | clipspec 11 tests |
| 24 drop/touchpad | the OLE drag/drop port (IDropTarget/IDataObject/IDropSource over the real DoDragDrop modal loop, FileDropEvent routing + hitbox/mouse dispatch through the real element tree, RevokeDragDrop exactly once, generation-checked stale-callback refusal) + the whole direct_manipulation.rs module with simulated-gesture dispatch and the honest no-touchpad record; the orchestrator's input-routing watchdog hardening | 16 tests (11 gpui + 5 dropspec) |
| 27 inspector | the live element-state inspector + debug diagnostics: the DiagnosticCapability switch (cfg/feature mapping, release profile reproduced), long ids with per-frame instances, the tree build/publication with value-only snapshots, picking over the real hit test, the debug frame overlay port, the leak/cycle detector over the real entity map; the orchestrator-wired dispatch seam + integration test | 30 tests (25 inspectorspec + 5 gpui) |
| 19 SVG | the user-approved second DLL rebuild: the SVG service in slot 8 (the pinned resvg/usvg 0.48.1 through the public SvgRenderer paths, the three sizing modes with the smooth 2x, the 8192 clamp, BGRA, the alpha-mask entry, the NeedsFontAssets font surface) + the Go client + the SvgRenderer model (exactly-once font orchestration) + the tinted-mask Svg element | 6 native + 14 svgspec tests |

**Current native artifact (do not rebuild without approval):** DLL 18,216,448 bytes, sha256 `f10c79737a4f68f629efa70533b016ccdcfb03b919e0d8a2058b696d062fde8c`, native revision 9, capability mask 0x3FF (bootstrap/layout/renderer/scene/text/glyph-raster/glyph-atlas/image/svg), static CRT, OS-only imports; the reserved slot array holds 16 entries (slot 8 = SVG). Reference harness: `reference/target/debug/gpui-reference-harness.exe`. Fixture kinds v1: layout-effects, layout-metrics, scene-painting, paths-filters, text-geometry, glyph-raster, authoring-counter (fx-0001..0007 all byte-deterministic and reproduced bit-exactly; the fx-0008 image fixture and the distinct AtlasKey::Svg variant are the recorded follow-ups for the NEXT approved native touchpoint).

## Current frontier (2026-10-11, after batch 7)

**Resolved: 28/34 - 01-19, 20, 21, 23, 24, 25, 26, 27, 31, 34.** Batch 7 (wave 3): ticket 19 (SVG — the user-approved second DLL rebuild: the SVG service in slot 8 with the reserved array grown to 16, the Go client, the SvgRenderer model with exactly-once font orchestration, the tinted-mask Svg element; 6 native + 14 svgspec tests). **Remaining pure-Go frontier: only 30 (partial — its 19/22/29 blockers are deferred; the 24/25/26/27 coverage is reachable in a bounded slice) and 33 (the final audit — waits on everything).** Deferred-native set (approvals required): 22 (AccessKit UIA), 28/29/32 (the capture chain: 28 needs a device/shared-texture export that does not exist in the ABI; 29 and 32 hang off it). The next approved native touchpoint carries the recorded follow-ups: the fx-0008 image fixture (ticket 17), the distinct AtlasKey::Svg variant (ticket 19). Recorded test-robustness follow-up: the ticket26 cursor-gate flake (repeat-run hover-state accumulation + console-focus interplay, environmental; passes isolated, fails under interactive machine use).

## Historical wave state (2026-10-06, predecessor chat)

Wave 1: all four subagents aborted on connection errors (~18-20 min in); the orchestrator resolved 13 in-session and salvaged 15's tree. Per the no-auto-retry rule the remaining slices (15, 25, 31) were taken in-session; wave 2 (this chat) re-delegated 25/31/21/20 under the same pattern with the user's fresh authorization — all five workers have now run past the wave-1 abort window without connection failures.

## Verification state (2026-10-11, at the batch-7 commit)

`CGO_ENABLED=0 go test ./... -count=1` — the batch gate: 29 packages ok with only the documented environmental cursor class (ticket26 hover gates, fails under the machine's interactive use, passes isolated) and the consumerspec provenance check, which evidence/ticket19-svg-fonts.json resolves (the drift detector working as designed: the new SHA must appear in the evidence records). `go vet ./...` clean, gofmt clean, `go build ./...` green. fx-0001..fx-0007 all pass unchanged after the artifact rotation to revision 9 (the bit-exact property held through both rotations now); the rotated-artifact regression set (layoutspec/scenespec/glyphspec/textspec/imagespec/native) green. clipspec tests DO touch the real system clipboard by design; svgspec + internal/native touch the real rebuilt SVG service (rev 9).

## Key conventions and gotchas

- Envelope JSON keys snake_case (Rust serde defaults); effect ops kebab-case tagged; trace f32 values `"f32:XXXXXXXX"` (8 uppercase hex); CompareTraces exact (first mismatch names event seq/field).
- Reference window scale factor 2.0; rem = 16 logical px; measurement snapping `ceil(max(0,x)*scale)`; rounding midpoint-toward-zero (util.rs).
- Recorded traces embed machine-dependent facts (Segoe UI etc. via pinned Parley store; FontId canonical bits) — deterministic on THIS machine; evidence records the environment.
- OLE STA required on the foreground host; winhost tests open REAL windows — must FAIL not skip if window creation fails.
- Ledger: `go run ./conformance/cmd/ledgergen -scan reference/out/api-scan.json -out conformance/ledger/generated -profiles-dir reference/out/profiles` (schema @2).
- Rust (only with approval): `cd reference && RUSTFLAGS="-C target-feature=+crt-static" cargo build --release -p gpui-go-native --target x86_64-pc-windows-msvc -j 4` then `go run ./reference/native/tools/measure -dll reference/target/x86_64-pc-windows-msvc/release/gpui_go_native.dll` (refreshes manifest + embedded DLL). Update the cumulative assertions additively — the full set learned across the two rotations: internal/native/abi.go (nativeRevision + the reserved array if the slot table grows + the layout comment), layout_test.go + glyph_test.go + scenespec + glyphspec (the capability mask), native_test.go (the record sizes/offsets when the ABI table grows), consumerspec/evidence.go (the SHA/bytes/revision constants — and the new SHA must appear in an evidence JSON for the provenance check). Harness builds fast in debug.
- CE pin `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a` (reference/ce-source, zero patches). Zed `a84689073d296dfd39987bc7dd478e43ef76d83a` comparison only. ~98% visual closeness is intent, not a threshold; calibration is ticket33.

## Historical planning notes

See [PROJECT.md](../PROJECT.md) and the sections below: Wayfinder map, specification, conformance contract/inventory, 34-ticket plan.
