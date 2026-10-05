# Windows core implementation plan

Status: in progress (ticket01 resolved 2026-10-05; see [issue files](issues/) for per-ticket status)
Triage: ready-for-agent

The [specification](../gpui-core/spec.md) turns the completed [Wayfinder map](../gpui-core/map.md) into the following independently verifiable paths. All tickets start open and unassigned. Creating this backlog completes the requested planning sequence; broader framework implementation has not started. The next implementation instruction should claim the reference-harness ticket first.

Use the [local tracker conventions](../../docs/agents/issue-tracker.md): claim before work, resolve only with acceptance evidence, and discover the frontier from ticket metadata. Blocking edges name only prerequisites; unrelated branches can progress independently after their blockers resolve. Before claiming an unexpectedly large slice, split it into bounded child work and preserve coverage/edges instead of silently dropping requirements. All slices include their fixtures and failure handling, rather than leaving verification to the final audit.

The [coverage inventory](../../docs/conformance-inventory.md) assigns known families; the first ticket expands it into a per-symbol/profile ledger. Any newly found applicable operation requires an owner and evidence before full-core acceptance. Alternate backend/dev-toolchain mechanisms are not automatically exceptions. The final audit blocks completion on uncovered applicable rows.

| Ticket | Observable delivery | Blocked by |
|---|---|---|
| [01 — Record independent reference fixtures and the core capability ledger](issues/01-reference-harness.md) | A maintainer can run one pinned reference fixture and inspect a complete source coverage ledger before port behavior is accepted. | none |
| [02 — Load one exact native artifact from a Go-only consumer](issues/02-native-bootstrap.md) | An offline Go consumer loads the combined native DLL, checks identity and performs a checked buffer round trip without cgo. | 01 |
| [03 — Run scoped entities, typed access and ordered effects headlessly](issues/03-scoped-entities-effects.md) | A model shared by independent owners receives typed updates and events, then releases deterministically under a controlled foreground dispatcher. | 01 |
| [04 — Deliver scoped async results and cancel safely](issues/04-scoped-tasks.md) | An app starts background work, closes its owner, and safely delivers or discards the eventual result. | 03 |
| [05 — Open and close a DPI-aware window on the foreground thread](issues/05-win32-window.md) | A minimal Go app opens two native windows, processes events and closes each without blocking the other. | 02, 03 |
| [06 — Compute styled layout through the pinned Taffy service](issues/06-layout-roundtrip.md) | A Go-authored style tree produces full-node geometry matching the independent reference through the real DLL. | 02 |
| [07 — Present a clear frame and retire its resources on GPU completion](issues/07-present-and-retire.md) | A window presents a color frame while a resource ledger proves that accepted work retires only after actual GPU completion. | 05 |
| [08 — Render styled boxes through the scene kernel](issues/08-paint-boxes.md) | A nested Go scene of boxes, borders, shadows and underlines renders at oracle-checked bounds. | 06, 07 |
| [09 — Shape text and expose paragraph, caret and hit-test geometry](issues/09-text-geometry.md) | A Go consumer shapes pinned fonts and queries wrapping, clusters, selection and carets with reference-matching geometry. | 06 |
| [10 — Rasterize and draw text in all required glyph formats](issues/10-draw-text.md) | Text shaped by the selected service appears in a window using the reference raster and atlas paths. | 08, 09 |
| [11 — Run the typed counter and custom-component authoring path](issues/11-authoring-counter.md) | An external Go package builds a fluent custom component and a two-window counter with shared state and independently owned views. | 03, 10 |
| [12 — Register, serialize and bind typed actions](issues/12-action-registry.md) | An app defines unit and rich actions, loads a key binding payload and dispatches the correctly cloned bound value. | 03 |
| [13 — Route keyboard input through focus, actions and key contexts](issues/13-focus-keyboard.md) | A focused counter/editor reacts to physical keys, chords and actions using the same propagation rules as the reference. | 05, 11, 12 |
| [14 — Preserve keyed state and cached frames while scrolling lists](issues/14-retained-scroll.md) | A scrollable/virtualized view reuses retained content while callbacks, focus and dependencies stay valid across frames. | 11, 13 |
| [15 — Lay out inline text and embedded elements across fragments](issues/15-inline-layout.md) | A paragraph mixes shaped text and custom inline boxes while painting and hit testing follow the reference fragments. | 11 |
| [16 — Render paths, surfaces and nested filters with the full scene plan](issues/16-paths-filters.md) | An application paints every remaining scene primitive and composes nested filters and surfaces without ordering or opacity errors. | 08 |
| [17 — Decode CPU images and draw their selected frames](issues/17-image-decode.md) | A Go app displays decoded image bytes with correct orientation, alpha and animation metadata. | 08 |
| [18 — Load assets asynchronously and animate image elements](issues/18-asset-loading.md) | Image elements load from embedded, on-demand or explicitly injected HTTP sources and redraw safely as frames arrive. | 04, 11, 17 |
| [19 — Render SVGs with lazy fonts and correct alpha conversion](issues/19-svg-fonts.md) | An SVG element requests missing font assets, resumes decoding once they arrive and renders the reference pixels. | 09, 18 |
| [20 — Compose and edit text through IMM32](issues/20-ime-editor.md) | A focused text editor accepts native composition and positions its candidate UI correctly when moved, scrolled or rescaled. | 13 |
| [21 — Publish semantic accessibility trees from real elements](issues/21-accessibility-tree.md) | A custom view publishes stable semantic nodes and responds to accessibility actions through foreground dispatch. | 11, 13 |
| [22 — Expose the published tree to Windows UI Automation](issues/22-uia-provider.md) | Narrator or a UIA client reads and operates a live window even while app updates or teardown are occurring. | 21 |
| [23 — Exchange clipboard text, files, images and metadata](issues/23-clipboard.md) | An app copies and pastes multi-entry clipboard content with native applications without losing reference metadata or image semantics. | 05, 17 |
| [24 — Handle OLE drag/drop and precision touchpad gestures](issues/24-drop-touchpad.md) | A view accepts native drops and scroll/gesture input with correct targeting and cancellation. | 13, 23 |
| [25 — Complete native dialogs and credential storage operations](issues/25-dialogs-credentials.md) | An app prompts for files or confirmation and reads/writes/deletes credentials without blocking foreground progress or leaking resources. | 04, 05 |
| [26 — Implement remaining Windows app and desktop operations](issues/26-desktop-operations.md) | A Go app uses the reference window/app controls and receives desktop settings, power and lifecycle changes. | 05, 13 |
| [27 — Inspect live element state and expose debug diagnostics](issues/27-inspector.md) | A developer inspects the live element tree and diagnoses ownership or layout problems without altering release behavior. | 14, 21 |
| [28 — Capture and render frames using the renderer's D3D11 device](issues/28-capture.md) | A window displays captured content through the same-device adapter while producer frames remain valid through GPU copies. | 16 |
| [29 — Recover device loss and rebuild retained GPU resources](issues/29-device-loss.md) | An app survives device loss or reports an explicit terminal failure while retaining or safely retiring every in-flight dependency. | 14, 28 |
| [30 — Verify cross-service close, cancellation and shutdown](issues/30-shutdown-integration.md) | A multiwindow app closes while tasks, modal operations, UIA, capture and rendering are active, and every owner reaches its specified terminal state. | 19, 20, 22, 24, 25, 26, 27, 29 |
| [31 — Build an offline consumer with prebuilt assets and executable DPI resources](issues/31-consumer-delivery.md) | A clean Windows consumer builds and opens an app using local Go and the shipped native artifact alone. | 05 |
| [32 — Render scenes to owned images and account for headless APIs](issues/32-render-to-image.md) | A caller receives completed, owned scene pixels with explicit native/test/headless profile behavior. | 16, 29 |
| [33 — Calibrate visual gates and audit complete Windows core conformance](issues/33-full-core-acceptance.md) | A maintainer can produce an honest full-core coverage report tied to one exact artifact, independent reference and controlled environment. | 15, 30, 31, 32 |
| [34 — Triage core-support ledger rows into concrete families](issues/34-core-support-triage.md) | Every supporting-crate capability row from ticket01 is reassigned to a concrete owner or explicitly dispositioned, so the final audit has no untriaged applicable rows. | 01 |

The native artifact bootstrap measures packaging feasibility immediately. Actual GPU completion is part of the first presented frame, before other rendering acceptance. The delivery slice proves a clean consumer early and the final audit reruns it on the completed artifact. Numerical visual thresholds are calibrated from real reference noise and defect detection, never selected from the approximate 98% intent alone.

Scope and user preferences remain in the [project brief](../../PROJECT.md). Public publishing, licensing/branding decisions, Base, broad Windows/architecture coverage and other operating systems are later work. No package, DLL or application is published by this planning record.
