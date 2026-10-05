# Exchange clipboard text, files, images and metadata

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: 05, 17

## Question

An app copies and pastes multi-entry clipboard content with native applications without losing reference metadata or image semantics.

## What to build

Deliver this complete observable path against the [implementation specification](../../gpui-core/spec.md). This ticket is planned work; its status is not evidence that implementation has begun. The selected contracts are [windows platform contract](../../../docs/windows-platform-contract.md), [distribution contract](../../../docs/distribution-contract.md). Preserve their complete requirements when implementing the bounded path below.

## Acceptance criteria

- [ ] Implement source-supported clipboard formats, multi-entry text/metadata, files and images, retaining distinctions between image entry modes.
- [ ] Test ownership transfer, unavailable/busy clipboard and malformed external data without leaking handles or leaving the clipboard open.
- [ ] Verify round trips with a native application and reference fixtures for Unicode, files, metadata and image pixels.
- [ ] Respect foreground/apartment rules and clean up exactly once on partial failures or app/window closure.
- [ ] Keep credentials separate from clipboard behavior and assign unsupported source formats explicit outcomes. Verify read_from_primary and read_from_find_pasteboard against the pinned Windows unavailable/no-op behavior.
- [ ] Attach exact source/artifact/environment identities and executed results for this slice; update capability rows and preserve failures/limitations. Passing an earlier slice does not establish full-core parity.

## Blocking work

- [Open and close a DPI-aware window on the foreground thread](05-win32-window.md)
- [Decode CPU images and draw their selected frames](17-image-decode.md)

## Comments

- 2026-10-05 — Created under the user's authorization to finish Wayfinder, the specification and local implementation tickets. Routine details are delegated; a discovered scope or architecture contradiction must reopen the affected decision with evidence.
