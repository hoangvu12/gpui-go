# Expose the published tree to Windows UI Automation

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: wave-4 orchestrator chat (Roboco)
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

## Answer

RESOLVED (2026-10-11, wave 4, batches 8+10). The pinned AccessKit UIA provider is live end-to-end over the rev-11 native artifact:

- **Native (batch 8)**: the a11y adapter service in reserved slot 9 (capability bit 10) — accesskit 0.24.1 + accesskit_windows 0.33.1 (the CE lock resolution; the same windows 0.62.2 family) with the pin's adapter/publish/getobject/queued-action paths and the activation-before-complete-tree behavior (the stored initial tree returned by the activation handler — no Go trampoline).
- **Go (batch 9)**: the A11yProvider over App — the adapter lifecycle (window a11y activation on the message thread, close via the scope sentinel), the per-frame publication of the ticket-21 A11ySnapshot (the native publish gated by the active flag with the false→true edge forcing a redraw), WM_GETOBJECT routing in the WndProc (the lock dropped before the native call; DefWindowProc when unhandled), and the action drain routed to the generation-checked listeners with the built-ins (Click → the synthetic mouse at the node bounds center through the real dispatch, Focus, Blur).
- **Rev-11 fix**: the action mask decodes as a tag-presence set (all nine actions representable).
- **Gates**: internal/a11yspec — 9 tests over real windows (activation flow, publication semantics, action routing, concurrency 4×25 queries, close/reopen, stale generations) plus REAL Narrator evidence (a real UIA client queried the window 3 times; the launch + the protected-process limitation recorded). Regression set green.

Evidence: [evidence/ticket22-uia-provider.json](../../../evidence/ticket22-uia-provider.json) (executed results, the honest deviations, the environment record).
