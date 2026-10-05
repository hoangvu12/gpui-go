# Research evidence

This directory mixes 2026-10-04 planning research with implementation evidence from the 2026-10-05-authorized implementation phase. The planning items are selected reference files, not a complete checkout, vendored dependencies, or a working implementation; implementation items record executed results.

| Location | Contents and provenance |
|---|---|
| [ticket01-reference-harness.json](ticket01-reference-harness.json) | **Executed implementation evidence (ticket 01)**: pinned CE oracle build identity, deterministic recorded reference trace for fx-0001, capability ledger (6911 planned rows, 5 profiles), mismatch-demonstration results, executed Go test/vet output and preserved limitations |
| [ticket02-native-bootstrap.json](ticket02-native-bootstrap.json) | **Executed implementation evidence (ticket 02)**: bootstrap native DLL identity/size/imports/CRT outcome, Go loader contract alignment (cache path, SYSTEM32-only loading, retained verified handle, corrupt-file policy), executed tests and demo output, module-format measurements |
| [ticket04-scoped-tasks.json](ticket04-scoped-tasks.json) | **Executed implementation evidence (ticket 04)**: task ownership layer (execution-resource retention until ack, exactly-once result delivery/discard, generation gating, detached registry, shutdown drain), 24 tests, documented adaptations |
| [ticket05-win32-window.json](ticket05-win32-window.json) | **Executed implementation evidence (ticket 05)**: real Win32 OLE-STA/PerMonitorV2 host, two-window lifecycle tests, deadlock diagnosis and fix, observed DPI |
| [ticket06-layout-roundtrip.json](ticket06-layout-roundtrip.json) | **Executed implementation evidence (ticket 06)**: taffy 0.13.0 native service identity, Go adapter semantics with source citations, both exact-trace gates (fx-0001 54/54, fx-0002 63/63) |
| [ticket07-present-and-retire.json](ticket07-present-and-retire.json) | **Executed implementation evidence (ticket 07)**: D3D11 renderer service identity, real-GPU ledger proofs (pending-then-completed retirement), device/driver environment identity, measurements |
| [ticket08-scene-kernel.json](ticket08-scene-kernel.json) | **Executed implementation evidence (ticket 08)**: native scene kernel identity and pinned semantics, fx-0003 oracle boundary, 142-event exact gate result |
| [ticket34-triage.json](ticket34-triage.json) | **Executed implementation evidence (ticket 34)**: 351 core-support rows dispositioned with citations; ledger schema @2 |
| [ticket09-text-geometry.json](ticket09-text-geometry.json) | **Executed implementation evidence (ticket 09)**: Parley text service identity, fx-0004 oracle boundary and cases, 339-event exact gate result |
| [ticket10-draw-text.json](ticket10-draw-text.json) | **Executed implementation evidence (ticket 10)**: DirectWrite rasterizer/atlas ports, fx-0005 bit-exact pixel-hash gate, real-window text draw, renderer race fix |
| [ticket11-authoring-counter.json](ticket11-authoring-counter.json) | **Executed implementation evidence (ticket 11)**: element/view runtime architecture, fx-0006 108/108 gate, real two-window counter results |
| [ticket16-paths-filters.json](ticket16-paths-filters.json) | **Executed implementation evidence (ticket 16)**: scene v3/renderer v2 draw pipeline with prebuilt DXBC, fx-0007 428/428 gate, pixel-verified composite draw |
| [ticket12-action-registry.json](ticket12-action-registry.json) | **Executed implementation evidence (ticket 12)**: typed action registry with reference-derived NoAction/Unbind semantics, boxed-binding clone proofs, schema decision, 27 tests |
| [ticket13-focus-keyboard.json](ticket13-focus-keyboard.json) | **Executed implementation evidence (ticket 13)**: focus/keyboard/action dispatch — keystroke+keymap+dispatch-tree+focus runtimes, div interactivity wiring, the gpui_windows keyboard path (accelerator pre-dispatch, AltGr/dead-key translation, surrogate WM_CHAR, layout reports), deterministic transcripts + real-host input evidence |
| [ticket03-scoped-entities.json](ticket03-scoped-entities.json) | **Executed implementation evidence (ticket 03)**: root gpui runtime (entities, leases, effects, scopes, events, executors) with the exact-trace gate result against the recorded reference, 39 ownership-rule tests, documented Go adaptations |
| [go-toolchain-installed.json](go-toolchain-installed.json) | User-installed Go 1.27.1 identity and explicit fixture/planning-sequence authorization; the earlier blocked MSI attempt was not retried |
| [authoring-signature-validation.json](authoring-signature-validation.json) | Executed root/fixture tests and vet, external positive compilation, 29 negative/control pairs, command outputs and source hashes; compile-only broader API, no native/runtime parity proof |
| [wayfinder-completion-validation.json](wayfinder-completion-validation.json) | Final specification/inventory/backlog local links, tracker status/DAG, fixture-source and pinned-snapshot hash checks; no native or visual execution |
| [go.mod](go.mod) | Module boundary excluding upstream reading snapshots from root Go package discovery; it does not turn this evidence into a project dependency |
| [distribution/manifest.json](distribution/manifest.json) | Pinned asset/image/SVG sources and root/core manifests, with local UTF-8 text hashes; supplements the Windows lockfile and core platform snapshots |
| [distribution-planning-validation.json](distribution-planning-validation.json) | Distribution/conformance-research documentation, local links, tracker dependencies and snapshot hashes; no build, loading, ABI or parity execution |
| [renderer/manifest.json](renderer/manifest.json) | Pinned CE D3D11 devices/renderer/atlas, shader tooling, vsync, wgpu capture interop and capture wrapper/producer snapshots; 12 raw-source files with local SHA-256 hashes, not a native build |
| [renderer-planning-validation.json](renderer-planning-validation.json) | Renderer decision documentation/link/dependency and snapshot-hash checks; no GPU, shader, ABI or consumer-build execution |
| [windows-platform/manifest.json](windows-platform/manifest.json) | Selected Windows host/input/raster/clipboard/touchpad and Parley files, Cargo lock and manifest at the accepted CE commit; raw-URL retrieval with local UTF-8 text hashes, not a build |
| [windows-target-inspection.json](windows-target-inspection.json) | Read-only current-PC Windows/architecture/CPU/GPU/driver inventory; no graphics, font, IME or accessibility tests |
| [windows-planning-validation.json](windows-planning-validation.json) | Windows decision document/link/dependency and local snapshot-hash checks; no executable validation |
| [core/manifest.json](core/manifest.json) | GPUI-CE core snapshots, pinned URLs, and hashes of local files; text output has normalized newlines |
| [api/manifest.json](api/manifest.json) | Additional pinned GPUI-CE files decoded directly from GitHub Contents API Base64 into bytes |
| [original-gpui-events/manifest.json](original-gpui-events/manifest.json) | Zed comparison source at `a84689073d296dfd39987bc7dd478e43ef76d83a`: counter example, context/event/action contracts and key-dispatch documentation; Base64-decoded bytes, Git blob IDs and SHA-256 hashes |
| [original-gpui-authoring/manifest.json](original-gpui-authoring/manifest.json) | Same Zed comparison pin: element/view/style contracts, text conversions, derive helper and example; exact retrieval format and hashes recorded in manifest |
| [ecosystem/README.md](ecosystem/README.md) | Kit, CE components, and Yororen snapshots; manifest, tree, and commit metadata |
| [backends/manifest.json](backends/manifest.json) | Selected backend source, recorded repository revisions, local file sizes and hashes; original retrieval used HEAD as noted in the manifest |
| [go-language/](go-language/) | Installed Go 1.26.5 grammar and runtime documentation; these do not describe Go 1.27's new generic methods |
| [exa-port-discovery.json](exa-port-discovery.json) | Search queries and returned discovery material; use primary sources for technical conclusions |
| [research-environment.json](research-environment.json) | Host and installed tool versions, without build measurements |
| [earlier-research.md](earlier-research.md) | Historical investigation before the clarified faithful-port scope; its broad Gio-first advice is superseded by the current synthesis |

Local hashes verify the stored evidence, not runtime behavior or byte equality with every remote blob. In particular, normalized text and byte-decoded copies of the same upstream file can have different hashes. Preserve snapshots when editing research conclusions.

The [source ledger](../research/07-sources-and-method.md) describes the broader inspection: not every remotely read source was copied here. Upstream code remains subject to its own notices. This directory does not assign a license to the future gpui-go implementation.
