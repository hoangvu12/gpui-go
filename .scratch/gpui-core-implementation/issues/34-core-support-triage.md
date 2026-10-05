# Triage core-support ledger rows into concrete families

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: pi-implementer subagents (parallel), session 065e52f5
Blocked by: 01

## Question

Every capability-ledger row that supports a public core behavior is assigned to a concrete implementation owner, so the final conformance audit cannot be blocked by silently untriaged applicable work.

## What to build

Ticket01's capability ledger contains [rows](../../../conformance/ledger/generated/ledger.md) in the `core-support-triage` family (owner 34): the public surface of supporting crates (gpui_ce_shared_string, gpui_ce_collections, gpui_ce_sum_tree, gpui_ce_zed_util, gpui_ce_util, perf) plus structural gpui rows (private/seal modules and bare re-exports). These are applicable to the port through the gpui public APIs that consume them, but no family rule could place them confidently.

Deliver a bounded triage pass over exactly those rows:

- For each row, determine the public gpui surface that exercises it and either reassign the row to that surface's owner ticket (updating `conformance/ledger/owners.go` so regeneration is stable) or record a justified `approved-exception`/`blocked-environment` state per the [conformance contract](../../../docs/conformance-contract.md).
- Confirm structural rows (private modules, internal re-exports) are not observable operations and mark them accordingly rather than leaving them as planned work.
- Preserve the generated ledger and rerun `go run ./conformance/cmd/ledgergen` plus the ledger tests.
- Update the ticket with the row counts before/after and the disposition breakdown.

## Acceptance criteria

- [x] No `core-support-triage` rows remain unexamined; each is reassigned, excepted with provenance, or documented as non-observable structure.
- [x] The owner rules regenerate the same disposition deterministically.
- [x] The final audit's "no applicable pending rows" rule can point at this ticket's completion record.

## Blocking work

Ticket01 (ledger generation).

## Answer

Resolved 2026-10-05 by the implementation chat (Roboco `065e52f5`, pi / iroha/dashscope/glm-5.3), implemented by parallel background subagents and independently re-verified. Complete record: [evidence/ticket34-triage.json](../../../evidence/ticket34-triage.json).

- All 351 core-support-triage rows dispositioned: 49 -> family 03 (SharedString/collections data plumbing), 36 -> 04 (util async machinery), 89 -> 14 (sum_tree via list.rs citations), 3 -> 26 (restart shell helpers), 3 -> 27 (debug assertions/measurement), 56 -> 01 (perf instrumentation, web backend selection, platform-inapplicable), 115 -> core-structure marker (private modules, unused zed_util verified dependency-free across all workspace manifests). Ledger schema bumped to @2 with a required disposition field, ByDisposition summary, Validate invariant, and platform-gate notes for crate-level cfg (33 previously-invisible rows). Unassigned = 0, owner-34 = 0, total rows still 6911; regeneration deterministic; disposition table with pinned-source citations in conformance/ledger/generated/triage.md. Judgment calls (SharedString -> 03, sum_tree -> 14, perf -> 01, zed_util structural) recorded in the ticket comments and triage.md.

## Comments

- 2026-10-05 — Created during ticket01 resolution because its enumeration surfaced applicable supporting-crate rows without a confident family owner; the [conformance contract](../../../docs/conformance-contract.md) requires a concrete ticket before implementation proceeds rather than silent coverage loss.
