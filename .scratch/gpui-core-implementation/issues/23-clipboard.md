# Exchange clipboard text, files, images and metadata

Parent: ../plan.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: resolved
Assignee: implement-spec wave-3 worker (background subagent, model dashscope/glm-5.3; evidence: ../../evidence/ticket23-clipboard.json)
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

## Answer

Resolved 2026-10-10. Implemented by the wave-3 clipboard implementer subagent (session impl-23-clipboard; its first run hung on a runaway machine-wide `find /` and was stopped and resumed once with a corrective prompt — no silent auto-retry, per the wave rules; the resumed run completed cleanly) and verified by the orchestrator (clipspec 11/11 re-run green including the real PowerShell native interop, gpui green; vet/gofmt clean). The Go port of gpui_windows/src/clipboard.rs over the real system clipboard: the six registered formats ("GPUI internal text hash", "GPUI internal metadata", "image/svg+xml", "GIF", "PNG", "JFIF"); writes (EmptyClipboard + per-entry; the CF_UNICODETEXT + 8-byte hash + metadata pair; images through the native format plus the PNG compatibility copy with SVG skipped; ExternalPaths skipped on write); reads (the EnumClipboardFormats walk taking one text + one image + one files entry; CF_DIB→BMP with the exact 14-byte header math; metadata gated on the seahash text-hash match; CF_HDROP files through DragQueryFileW); the ClipboardGuard/LockedGlobal exactly-once open/lock discipline; HGLOBAL ownership transfer to the system. Clipboard images decode through ticket17's codec clipboard entry (gpui/image.go untouched). The App surface (gpui/clipboard.go) adds Read/WriteClipboard, the sync-wrapped ReadClipboardAsync, and the four primary/find-pasteboard methods as the pinned Windows unavailable/no-op outcomes; the Host surface (gpui/clipboard_windows.go) marshals to the host thread via the existing wake machinery — win32host_windows.go needed no wndproc cases (the pin makes none) and only win32host_other.go gained non-Windows stubs. A ticket-text/source discrepancy is resolved in the source's favor and recorded: `text()` concatenates entries without a separator and falls back to ExternalPaths display strings (platform.rs ~3361-3384), not the "\n" join the ticket sketched. The seahash 4.1.0 port is a documented best reading (not oracle-verified against the crate), self-consistent for write/read; Rust-interop hash parity is a recorded caveat. Real-clipboard test findings (viewer contention, CloseClipboard's opening-thread requirement, SetClipboardData's zero/odd-size rejections) are in the evidence file. Dependents unblocked: 24 (drop/touchpad). Executed evidence: [evidence/ticket23-clipboard.json](../../../evidence/ticket23-clipboard.json).
