# Audit Base coverage, ship a showcase and release v0.2.0

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: claimed
Assignee: base-port-2 orchestrator (2026-10-23)
Blocked by: 16, 17, 18, 22, 24, 26, 29, 30, 31, 36, 37

## Question

A maintainer can see every Base module and test accounted for (ported/deviated/not-applicable), run a showcase example, and consume tag v0.2.0.

## Scope

- A Base coverage ledger over the source manifest (`evidence/base/source-manifest.json`); `examples/base-showcase`; README/THIRD_PARTY_NOTICES updates for any accepted modules; tag v0.2.0 (same flow as v0.1.0).

## Acceptance criteria

- [ ] The full corpus `CGO_ENABLED=0 go test ./... -count=1` green apart from the documented environmental classes.
- [ ] Every ported behavior cites the pinned Base source (`upstream-base/ce/crates/base/src/...`, c0820693) or the core pin (`reference/ce-source/crates/gpui/src/...`, 254b5dbd); no invented APIs.
- [ ] Upstream tests in scope are ported (count ported / deviated / not-applicable recorded with reasons); real-window tests FAIL rather than skip.
- [ ] No native artifact change (rev 11 stays frozen); any genuine native need is stopped and surfaced to the user with evidence.
- [ ] `CGO_ENABLED=0 go test` for the touched packages is green, `go vet` + gofmt clean, and the core regression packages stay green; executed evidence in `evidence/base/ticketNN-*.json`.

## Research

- [00-baseline.md](../research/00-baseline.md)

## Comments

- 2026-10-19 — Created by the GPUI Base port chat from the merged slice inventories (R: = `reference/ce-source/crates/gpui/src`).
- 2026-10-23 — Claimed. Part 1 (delegated, runs alongside 37): the coverage ledger over the source manifest, examples/base-showcase, README/notices. Part 2 (orchestrator, after 37): the full corpus run. Part 3 (only with the user's go-ahead): merge base-port-2 to main, push, tag v0.2.0.
- 2026-10-23 — Part 1 delivered. It includes the coverage tool (conformance/cmd/basecoverage, `-check` mode), evidence/base/{coverage-ledger.json,COVERAGE.md}, examples/base-showcase (opened a real window under a timeout), and the README Base section. The tool found a go1.27.1 compiler deadlock for importers naming base.InputState and the related types, caused by the generic alias inputCx in exported signatures; the orchestrator fixed it in 235e4f1. It also found 1 GAP, text_view.rs:2227 linked_image_handler…, now added to 37. The ledger is regenerated after 37. Input/Textarea can now go into the showcase.
