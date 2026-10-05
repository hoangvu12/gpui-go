# Load one exact native artifact from a Go-only consumer

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer subagents (parallel), session 065e52f5
Blocked by: 01

## Question

An offline Go consumer loads the combined native DLL, checks identity and performs a checked buffer round trip without cgo.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [distribution contract](../../../docs/distribution-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [x] Build the smallest maintainer artifact with pinned provenance and bootstrap/service tables; expose fixed-width records, explicit ownership, generation-stamped handles and typed failures. Run a real call and release it exactly once.
- [x] Embed the exact artifact and manifest; support a verified absolute bundle override. Use per-user full-hash cache paths, cross-process locking and protected verification/read handles with restricted absolute LoadLibraryExW loading.
- [x] Reject wrong hash, architecture, ABI or missing compiled service; distinguish OS runtime unavailability. Test concurrent first publication, killed writer, unwritable cache and corrupt mapped files without unsafe replacement.
- [x] Verify no retained Go object pointers, buffer bounds, generated wire round trips, panic containment and callback failure reporting under forced GC.
- [x] Record PE imports and CRT outcome, artifact and compressed/expanded module size at this first build. Exceeding the module format ceiling reopens distribution before more packaging work; no network fallback or DLL hot unload.
- [x] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Record independent reference fixtures and the core capability ledger](01-reference-harness.md)

## Answer

Resolved 2026-10-05 by the implementation chat (Roboco `065e52f5`, pi / iroha/dashscope/glm-5.3), implemented by a parallel background subagent and independently re-verified by the orchestrator. Complete record: [evidence/ticket02-native-bootstrap.json](../../../evidence/ticket02-native-bootstrap.json).

- Bootstrap native artifact `gpui-go-native` (202,752 bytes, SHA-256 `b8a836dc...`, static CRT verified from actual PE imports: KERNEL32/api-ms-win-core-synch/ntdll only, no VC++ redistributable) with a versioned C ABI table (magic GPGO, abi_version 1, CE pin 254b5dbd recorded, capability bit 0 = buffer round trip, 8 reserved service slots, panic containment via catch_unwind). - Go loader `internal/native` (stdlib syscall only, CGO_ENABLED=0): embedded artifact + verified absolute bundle override; per-user cache publication at the contract path `UserCacheDir()/gpui-go/native/windows-amd64/<sha>/gpui_go_native-<sha>.dll` with cross-process LockFileEx lock, staging+fsync+rename, corrupt-final-file typed error (never overwritten), killed-writer staging recovery; verified read handle retained with FILE_SHARE_READ-only for process lifetime; LoadLibraryExW with absolute path + LOAD_LIBRARY_SEARCH_SYSTEM32 only; identity checks (hash, PE machine, ABI, magic, commit, capability) with distinct typed errors. - Offline consumer demo (`internal/native/cmd/demo`) loads the embedded artifact, prints identity and performs checked buffer round trips (0/1/4096 bytes) with FNV-1a verification and panic containment, all PASS. 16 top-level + 21 subtests pass; module embedding contributes 203,264 bytes (far under the module-format ceiling, no distribution reopening).

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.

- 2026-10-05 — Claimed by the implementation chat and delegated to a parallel background subagent (Roboco session 065e52f5, pi / iroha/dashscope/glm-5.3), ticket01 resolved as its blocker.
