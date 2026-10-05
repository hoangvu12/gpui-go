# Expose the published tree to Windows UI Automation

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 21

## Question

Narrator or a UIA client reads and operates a live window even while app updates or teardown are occurring.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [windows platform contract](../../../docs/windows-platform-contract.md), [runtime ownership contract](../../../docs/runtime-ownership-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Integrate pinned AccessKit/native provider services with actual HWND ownership, provider threads and activation-before-complete-tree behavior.
- [ ] Serve queries from published snapshots without waiting on a blocked Go update; release locks before raising reentrant UIA events.
- [ ] Exercise concurrent queries, updates, actions and close/reopen with real UIA tooling and available Narrator; record tooling/environment identities.
- [ ] Verify apartment-affine release and native provider references after window closure, preserving safe snapshot lifetimes.
- [ ] Compare property/focus/hidden/synthetic-node/action results with the reference; unavailable reader sessions remain blocked-environment evidence.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Publish semantic accessibility trees from real elements](21-accessibility-tree.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.
- 2026-10-06 — DEFERRED (successor chat): the pinned AccessKit provider is a Rust crate (native work, blocked by the no-cargo/DLL-rebuild constraint). A pure-Go/Win32 alternative is plausible (hand-written UIA provider COM interfaces in Go over the ticket21 semantic trees) but is a large COM surface; propose it to the user before starting. Blocked by ticket21 either way. No work started.

