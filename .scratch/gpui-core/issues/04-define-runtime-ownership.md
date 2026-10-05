# Define entity ownership, effects, and scheduling

Parent: ../map.md
Type: grilling
Labels: wayfinder:grilling
Status: resolved
Assignee: Codex /root (Wayfinder continuation, 2026-10-04)
Blocked by: 01, 03

## Later status — 2026-10-05

This ticket preserves its original decision and chronological comments. Subsequent [signature verification](10-validate-authoring-signatures.md) passed on user-installed Go 1.27.1, all ten Wayfinder children are resolved, and the [specification](../spec.md) plus [implementation backlog](../../gpui-core-implementation/plan.md) are complete. Earlier open-task/compiler-blocker statements below describe their recorded date; broader runtime/native behavior remains unimplemented.

## Question

How will Go preserve GPUI's logical entity lifetimes, notifications, event ordering, tasks, and foreground execution?

Decide ownership/disposal and weak-handle semantics; subscription and task cancellation; update versus notify; pending-effect flushing and reentrancy; persistent keyed state versus frame state; render caching; foreground thread affinity; and deterministic test scheduling. GC timing cannot be substituted for observable prompt cleanup. Record contracts for native resource owners without choosing GPU APIs here.

## Existing evidence

The prerequisite [Go authoring decision](03-choose-go-authoring-contract.md) is resolved at documentation level. Use its [selected contract](../../../docs/authoring-decision-round.md) as the interface baseline: callback-scoped access, weak listener capture, typed events and canonical action services, plus consumed attachment handles. This does not decide who retains entities, when disposal occurs, how subscriptions/tasks cancel, or when effects run. Feed any required lifetime-facing signature changes back into that contract. Compiler proof remains the separate validation task.

- [gpui core contracts](../../../research/01-gpui-core-contracts.md)
- [go api and lifetimes](../../../research/04-go-api-and-lifetimes.md)

## Resolution boundary

Record the user's accepted decision and rationale, alternatives considered, any explicit unresolved evidence needs, and newly exposed questions. Do not treat a proposal in the research as an accepted answer. This ticket is planning work.

## Comments

### Claim and bounded research, 2026-10-04

The user instructed continuing Wayfinder after the authoring resolution. Claimed this next decision and reused the required Roboco `pi` / `iroha/dashscope/glm-5.3` side chat for source-backed ownership/effect/scheduling research. The main chat retains synthesis and tracker ownership. Existing authoring selections are inputs, not evidence that lifetime policy has already been decided. No runtime implementation or compiler/toolchain operation is part of this work.

### Research review and integration, 2026-10-04

The side chat produced [runtime ownership research](../../../research/14-runtime-ownership-contracts.md), corrected it after main review, and performed a final read-only review of the synthesized contract. Main checked release/effect ordering, subscription iteration, strong versus weak context adapters, frame-state transfer and strong focus capture against the pinned snapshots. Corrections include rejecting release hooks as a solution to self-sustaining cycles, explicit ownership for detached tasks, independently scoped worker results, and distinguishing event-arena retirement from same-cycle entity release. No research recommendation was treated as automatic user acceptance.

## Answer

Resolved at documentation level on 2026-10-04 under the user's instruction to continue Wayfinder and delegated authority to select routine engineering choices from research. The user did not personally specify the lease/scope details below; these are reviewed engineering selections preserving the accepted parity and Go authoring direction. No remaining human preference blocks this decision.

Select **explicit logical entity leases grouped into deterministic scopes**, as detailed in the [selected runtime ownership contract](../../../docs/runtime-ownership-contract.md). Copies alias one lease; `RetainInto` creates an independent lease; `TransferInto` changes its owner; scope close/release are idempotent. Zero logical references irrevocably end weak upgrade eligibility, independently of GC. Callback entity facades are borrowed with temporary framework pins. Entity-dependent scopes survive independently of the window that first created the entity. Strong cycles require weak backedges or earlier explicit disposal; release hooks cannot collect them.

Select scope-owned subscriptions/tasks with weak callback endpoints by default, explicit early cancellation, and tracked app-owned detach registries. Strong processors and next-frame callbacks retain through named registration owners. Closing a window affects its own work, not another window's independent leases. Task cancellation is cooperative; worker execution dependencies and queued result dependencies have separate ownership. Foreground delivery gates check scope tokens, entity generations and any window binding. The design promises no forcible goroutine termination.

Preserve nested-update suppression of recursive flushes, release-before-next-effect processing, registration-ordered delivery with deferred activation and cancel-next behavior, pending-only notify coalescing, and per-occurrence FIFO emits/defers. Plain event payload values have a freeze-until-retirement convention for reachable mutable data; `EmitOwned` supplies explicit scoped dependencies. Arena retirement stays at cycle tail; newly resulting releases trigger a subsequent foreground flush, an explicit liveness adaptation.

Select one foreground goroutine bound to the owning OS thread, controlled test scheduling, generation-aware retained element state, cache reuse that retains dependencies/callbacks as well as scene artifacts, scoped frame construction, and completion-based native retirement. `ViewOf` binds a strong recipe lease to active construction; `ViewOfIn` supplies an explicit owner elsewhere. Aborted frame construction disposes unpublished resources without claiming rollback of arbitrary application mutations. Backend mechanisms remain their own decisions.

### Rationale and alternatives

- GC/finalizer/Go-weak-pointer ownership cannot reproduce prompt logical release and weak-upgrade observables. Go assignment cannot implement Rust clone/drop, so independent ownership must be explicit.
- Scope-only ownership would invalidate shared state when one window closes; universal app ownership would over-retain it. Independent leases plus entity-dependent scopes preserve sharing and deterministic release.
- Rust-style cancellation when a Go local variable becomes unused is unavailable. Scope retention with explicit cancellation is the documented adaptation; it also makes ignored registration handles meaningful.
- Arbitrary deep-copying or reflectively closing event/task payload fields is unsafe and underspecified. Frozen values and explicit owned dependency scopes define the responsibility without inventing hidden graph traversal.
- Reference counts do not collect cycles, a single UI thread does not control external I/O races, and arena reset does not prove GPU completion. The selected contract makes each limit explicit.

### Consequences, evidence limits and next step

Updated the [authoring contract](../../../docs/authoring-decision-round.md), [counter/two-window example](../../../docs/counter-view-api.md), glossary, research index and continuation notes. Added signature cases to [Validate the selected Go authoring signatures](10-validate-authoring-signatures.md) and behavioral obligations to [Define conformance gates and upstream update policy](09-define-conformance-and-update-policy.md). Platform/layout/renderer decisions must honor this ownership boundary. Native fencing, Windows loop integration, exact backend teardown and executed conformance remain future evidence needs, not claims of verified implementation.

All new signatures and runtime behavior remain uncompiled/unimplemented. Source inspection and documentation validation establish design consistency only. The policy-blocked Go upgrade was not retried or bypassed, and existing styling-helper tests do not validate this contract. No new runtime implementation, benchmark, installation, or compiler probe occurred.

Documentation validation passed: 19 documents checked, 172 local links resolved, code fences balanced, and all ticket blockers exist with an acyclic dependency graph. The map now records four resolved decisions, five open decisions and one open signature-verification task.

Next decision: [Choose the layout implementation and parity method](05-choose-layout-strategy.md).
