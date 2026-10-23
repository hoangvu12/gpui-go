# Native rebuild: bulk scene insert, low-latency present, weight-500 face

Parent: ../map.md
Type: task
Labels: wayfinder:task
Triage: ready-for-agent
Status: open
Assignee: unassigned
Blocked by: none

## Question

This is one user-approved native rebuild (the next revision after the embedded one). It has four parts.

1. **Weight 450-599 without fake bold.**
   - Today 500 maps to Regular plus DirectWrite bold simulation. The chain is fontique 0.11.1 font.rs:97-103 and matching.rs:386, the gpui_ce_parley catalog.rs:188 and text_system.rs:713-717/1211-1216 carrying the flag, then native glyph.rs:1192-1199.
   - Stop the synthesis for requested weights below 600 in the native font-resolution layer, on both the resolve and the per-run shaping paths.
   - Do not edit the oracle `ce-source`; patch in the native crate or a vendored copy, and document it as a deviation.
   - Evidence and probes are in `%TEMP%\boldprobe\out\`.
2. **Bulk scene insert.**
   - Today there's one `SyscallN` per primitive (`internal/native/scene_windows.go:47-110`).
   - Add an entry that takes a packed buffer of primitives in one call. On the Go side, buffer the primitives per frame and flush once before `DrawScene`.
   - Ordering and the scene output must be identical (scenespec).
3. **Low-latency present.**
   - Today the swapchain is flip-sequential with 3 buffers, `Present(0,0)`, no waitable object and no `SetMaximumFrameLatency` (`reference/native/src/renderer.rs:1070-1085`, `draw.rs:2436`).
   - Evaluate and implement `DXGI_SWAP_CHAIN_FLAG_FRAME_LATENCY_WAITABLE_OBJECT` with maximum latency 1. The frame loop waits on the waitable object before building a frame, alongside the existing DwmFlush waker in `gpui/frameloop_windows.go`.
   - Don't add any idle cost: still no work when nothing is dirty.
4. **Present1 dirty rects.** Optional: implement only if correct under the flip model, otherwise record why not.

Rebuild per `reference/README.md` "Build and measure": crt-static, then `tools/measure` to rotate the embedded DLL and manifest, then the revision, mask and SHA assertions. The CE source is at `C:\Users\ADMIN\Desktop\nguyenvu\gpui-go\reference\ce-source`; copy it in, never modify the original.

## Comments

## Answer
