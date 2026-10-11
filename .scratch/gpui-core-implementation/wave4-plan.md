# Wave 4 implementation plan (2026-10-11)

Orchestrator chat for the final implementation wave. Authorization: the
user's 2026-10-11 standing pre-authorization ("spawn a new chat as
orchestrator, and try to implement everything, dont need to ask me pls, just
implement everything"), which includes the native rebuilds the remaining
tickets need. The resource rules stay: `-j 4`, release, crt-static, bounded
scopes, honest evidence, no full-workspace `cargo test`.

## Remaining tickets

22 (AccessKit UIA), 28 (capture), 29 (device loss), 32 (render-to-image),
30 (shutdown integration), 33 (final audit, LAST). Fold-in follow-ups:
fx-0008 image fixtures (ticket 17), distinct `AtlasKey::Svg` (ticket 19),
ticket-26 cursor-gate bounded-retry robustness fix (environmental flake).

## Native surface (ONE bounded rebuild, revision 10, "the third")

All additive; planned together per the wave instruction:

1. **a11y.rs (new, slot 9, capability bit 10)** — the pinned AccessKit
   provider service (`accesskit 0.24.1` + `accesskit_windows 0.33.1`, the
   pin's lock resolution; both same-version windows 0.62.2 family).
   Adapter create (hwnd + is_focused + initial root label, the pin's
   `Adapter::new`), tree publish (`update_if_active` semantics: decode only
   when active, events raised after the state lock drops — the pin's
   reentrancy rule), `handle_wm_getobject` (activation from the stored
   pending initial tree — the pin's activation-before-complete-tree
   behavior; LRESULT conversion outside the lock), bounded action queue
   drained by polling (the pin's async_channel action handoff), is_active
   (the pin's active flag), destroy. No Go trampolines: activation reads
   the pending initial tree; actions are queued.
2. **capture.rs (new, slot 10, capability bit 11)** — the Windows Graphics
   Capture producer on the renderer's actual D3D11 device (ticket 28's
   first criterion: IDXGIDevice from the shared device →
   `CreateDirect3D11DeviceFromDXGIDevice` → free-threaded frame pool →
   capture session), driven through the existing `windows 0.62.2` crate's
   WinRT bindings (features added: `Graphics_Capture`,
   `Graphics_DirectX_Direct3D11`, `System_WinRT`, `System_WinRT_Direct3D11`,
   `System_WinRT_Graphics_Capture`, `Win32_Graphics_Gdi`, `Win32_System_Com`)
   — no new crate family. Monitor source enumeration
   (EnumDisplayMonitors + the item interop), native MTA housekeeping thread
   for WinRT object lifetime, FrameArrived notifications to a bounded
   pending queue (drop-oldest + counter), foreground poll = checkout/copy
   under the renderer's global mutex (the specified immediate-context
   protection; multithread-protected device already set), immutable
   destination textures (written once by the copy; pool reuse never
   mutates published content), frame release, stream stop/destroy, content
   size watch with pool Recreate.
3. **renderer draw extension** — the real `draw_surfaces`
   (`renderer_status::ERR_UNSUPPORTED_SOURCE` stays for tag 0): scene
   surface records with source tag 1 = capture frame handle; the pinned
   surfaces pipeline draw (per-surface uniforms, SRV on the same device,
   the view cache keyed by texture, retention by the drawn scene), driven
   from the ABI surface records.
4. **renderer device-loss entries (ticket 29)** — device state check
   (GetDeviceRemovedReason + the injected-loss flag, recorded separately),
   device rebuild (the pin's 5-attempt bounded retry + 350ms settle,
   device recreated, every surface's swap chain/DComp rebuilt in place —
   surface identity persists like the pin's renderer objects, draw globals
   dropped for lazy rebuild, in-flight submissions failed with the
   device-removed reason, surface-view cache cleared), and the
   skip-draws/mark-drawable gate (render/draw refuse while recovering —
   the pin's render_to_image ensure).
5. **render-to-image entry (ticket 32)** — the pin's test-support path
   verbatim: render-without-present + staging copy + row-pitch handling +
   BGRA→RGBA swap + dims, gated on the recovery flag.
6. **AtlasKey::Svg (ticket 19 follow-up)** — atlas key kind 2 with the
   SVG identity hash; the glyph-key AlphaMask ride retires; observable
   pool/cache/removal semantics unchanged.
7. **lib.rs** — revision 10, capability bits 10 (a11y) + 11 (capture),
   slots 9/10 filled (the reserved array stays 16 wide — no record growth
   this time; only the revision/mask constants move).
8. **fx-0008 (ticket 17 follow-up)** — harness fixture
   `image-codecs-v1` + committed synthetic test images (PNG/BMP/GIF/JPEG
   WebP set) + the Go conformance fixture test comparing the native image
   service's decode against the pinned image crate's reference trace.

Then: `cargo build -j 4` (release, crt-static, incremental except the new
accesskit_windows crate and the new windows features), `tools/measure`
rotation, additive Go assertions (abi.go revision/mask, layout/glyph/
scenespec/glyphspec masks, consumerspec constants + SHA provenance).

## Go-side waves (against the frozen rev-10 artifact)

Batch 8 = native surface + clients + fx-0008 + AtlasKey::Svg switch +
cursor-gate fix (all orchestrator in-session). Batch 9 = parallel
implementer subagents with strict file ownership:

- **22**: gpui/accessibility*.go + a11yprovider.go (new) +
  win32host_windows.go (WM_GETOBJECT case) + internal/a11yspec (new).
- **28**: gpui/capture.go (new) + surfaces.go + internal/capturespec (new).
- **29**: gpui/renderer.go + deviceloss.go (new) + internal/devicelossspec
  (new).
- **32**: gpui/headless.go (new) + conformance/ render-to-image comparison
  + internal/imagingspec (new).

Batch 10 = ticket 30 (shutdown integration spec; serial — additive hooks
across many files). Batch 11 = ticket 33 (final audit, orchestrator-led).

## Standing rules honored

CGO_ENABLED=0 everywhere; no machine-wide searches; real-window tests FAIL
not skip; never auto-retry aborted workers (stop, resume the same session
once, or take the slice in-session); known environmental flake classes
documented, not chased (the cursor-gate fix retires the worst one).
