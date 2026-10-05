# Define the Windows core parity contract

Parent: ../map.md
Type: grilling
Labels: wayfinder:grilling
Status: resolved
Assignee: Codex /root (current conversation)
Blocked by: none

## Question

What constitutes 1:1 parity with the pinned GPUI-CE core on Windows, and how will every relevant upstream capability and observable behavior be accounted for?

Preserve the accepted full-core target. Define the API and behavior inventory, visual comparison policy, upstream quirks versus defects, native integrations, supported Windows versions/architectures, and treatment of Rust features with no literal Go equivalent. A narrower application or Base subset is not an alternative target. Record gaps explicitly; do not silently drop capabilities.

## Existing evidence

- [gpui core contracts](../../../research/01-gpui-core-contracts.md)
- [backend text layout options](../../../research/03-backend-text-layout-options.md)
- [go api and lifetimes](../../../research/04-go-api-and-lifetimes.md)

## Resolution boundary

Record the user's accepted decision and rationale, alternatives considered, any explicit unresolved evidence needs, and newly exposed questions. Do not treat a proposal in the research as an accepted answer. This ticket is planning work.

## Comments

### Opening interview round

The user asked to continue the existing map. Claimed this first frontier ticket before working it. Full core parity, the upstream revision, Windows-first sequencing, native-dependency permission, and Base deferral are already settled and are not being reopened.

The first round asks about visual-test tolerance, treatment of upstream defects, and the initial Windows version/architecture support boundary. Recommendations are pending user answers, not resolutions. Preserve the GPUI concepts and fluent authoring structure; detailed language adaptations belong in the Go authoring ticket.

Read-only host inspection reports Windows 11 Pro, version 10.0.26200, 64-bit. This is evidence about the available host, not an accepted support matrix.

### User answers

The user allowed small appearance differences and requested approximately 98% closeness, accepted the recommended upstream-bug policy, and asked to make the port work on their own PC first while deferring broader Windows support decisions.

## Answer

### Accepted compatibility policy

- **Functionality:** the existing 1:1 core capability and behavior target remains in force against the pinned GPUI-CE revision. GPUI's concepts, composition, and fluent authoring experience remain the reference; necessary Rust-to-Go adaptations are resolved in the Go authoring ticket.
- **Appearance:** target approximately 98% visual closeness, allowing small rendering differences. Pixel-for-pixel equality is not required. The user's percentage is a visual goal; no particular pixel percentage, SSIM score, image metric, or numerical tolerance has been selected. Functional coverage remains 1:1.
- **Upstream defects:** match upstream observable behavior by default. Bug fixes may be deliberate, documented exceptions, with the changed behavior and reason recorded. This does not require reproducing internal implementation details that have no observable effect.
- **Platform:** first make it work on the user's current PC. The inspected environment is Windows 11 Pro, build 10.0.26200, 64-bit. This is the initial verification environment, not a promise of support for every Windows 11 machine. Broader Windows versions and architectures are deferred, as are other operating systems.

### How later work accounts for the core

The specification and conformance plan must trace the pinned core's public surface and observable contracts to Go equivalents, necessary language adaptations, explicit behavioral exceptions, or outstanding gaps. A gap remains unfinished work. Account for entities and effects, element phases and caching, style/layout, input/actions/focus, text and IME, scene/rendering, Windows services and accessibility, and scheduling/testing. The existing source audit supplies the initial inventory, not proof that the inventory or implementation has passed validation.

### Rationale and alternatives

The user chose small visual differences over exact pixel equality and current-PC usability over immediately defining a broad Windows support matrix. They accepted behavior compatibility with documented fixes over either indiscriminately reproducing every defect or silently changing behavior.

### Follow-through

The conformance decision will choose comparison scenes, same-machine reference conditions, visual scoring and local error limits, and how the approximately 98% goal is judged. The Windows integration decision targets the current PC. API spelling and unavoidable language differences belong to the Go authoring decision. None of these decisions or runtime tests has been completed by resolving this policy ticket.
