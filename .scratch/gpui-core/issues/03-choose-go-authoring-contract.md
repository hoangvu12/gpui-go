# Choose the Go language and authoring contract

Parent: ../map.md
Type: grilling
Labels: wayfinder:grilling
Status: resolved
Assignee: Codex /root (standalone continuation, transferred 2026-10-04)
Blocked by: 01

## Later status — 2026-10-05

This ticket preserves its original decision and chronological comments. Subsequent [signature verification](10-validate-authoring-signatures.md) passed on user-installed Go 1.27.1, all ten Wayfinder children are resolved, and the [specification](../spec.md) plus [implementation backlog](../../gpui-core-implementation/plan.md) are complete. Earlier open-task/compiler-blocker statements below describe their recorded date; broader runtime/native behavior remains unimplemented.

## Question

Which Go version and API adaptations preserve GPUI's authoring experience and third-party extensibility while remaining valid Go?

Resolve fluent concrete return types, generated forwarding versus reusable helpers, children/text conversions, generic context/listener operations, action/event typing, style refinement presence, and custom element/view contracts. Use illustrative documentation examples only while implementation remains unauthorized. Keep ownership semantics in the lifetime decision and coordinate any API consequences.

## Existing evidence

- [go api and lifetimes](../../../research/04-go-api-and-lifetimes.md)
- [gpui core contracts](../../../research/01-gpui-core-contracts.md)

## Resolution boundary

Record the user's accepted decision and rationale, alternatives considered, any explicit unresolved evidence needs, and newly exposed questions. Do not treat a proposal in the research as an accepted answer. This ticket is planning work.

## Comments

### Accepted language and text-child choices

- The user accepted Go 1.27 as the minimum language version and explicitly requested upgrading the installed toolchain.
- Support GPUI-style `Div().Child("Hello")`, as well as explicit `Text(...)`. The user accepted clear runtime errors for unsupported child types in exchange for convenient heterogeneous children. The exact supported input set and failure reporting remain part of the interface contract.

### Toolchain upgrade status

The current installation is the Windows MSI installation under `C:\Program Files\Go`, version 1.26.5. Official Go download metadata reported stable 1.27.1. An attempted command to download, checksum-verify and install the official MSI was rejected by automatic approval review with `blocked by policy` before execution. A subsequent `go version` still returned `go1.26.5 windows/amd64`. No successful download, checksum verification, or installation is claimed.

See [upgrade attempt record](../../../evidence/go-toolchain-upgrade.json). Installation is authorized by the user but remains incomplete because the execution was rejected. This does not block the remaining planning questions or authorize framework implementation.

### Remaining authoring decisions

Exact typed context/event/action and custom-element adapter shapes remain unresolved. The accepted language/text choices and the implemented styling mechanism below do not resolve the whole ticket.

### Clarification requested: does Rust GPUI require this generation step?

The user asked whether GPUI has the proposed custom-component generation step; this is a question, not acceptance of that workflow. Source inspection confirms that `Styled` supplies default fluent methods returning `Self`, with method groups expanded by GPUI macros. GPUI's component documentation recommends `RenderOnce` with `#[derive(IntoElement)]`. A component implements the style accessor and obtains the common methods through the trait; it does not need a separate developer-run generation command for that purpose.

Rust macro expansion occurs during normal compilation. A proposed `go generate` workflow differs: Go does not run generation automatically as part of `go build`. The assistant's earlier recommendation would therefore add an explicit workflow step for custom-component authors. Keep this tradeoff explicit; no user acceptance of it has been recorded. Evaluate the already-researched generic-helper alternative before treating generation as mandatory.

### Selected and implemented authoring mechanism

The user then instructed: "research the best method then do it." This authorized selecting and implementing the bounded mechanism without another preference question. [Focused source research](../../../research/09-fluent-authoring-methods.md) compared generic embedding, forwarding generation, wrappers, and standalone functions.

Selected: value-embed `Styled[*Component]` and bind the owner once with `BindStyled(&Component{})`. Shared methods return the concrete pointer. No custom-author generation, reflection, or unsafe recovery is needed. The tradeoff is constructor initialization and pointer-only use of bound components. Runtime guards reject copied/unbound helpers before shared style operations. Custom methods and pointer-embedded helpers cannot receive automatic copy protection; the latter embedding is unsupported.

[Implemented package and usage](../../../authoring/README.md) includes representative style operations and value snapshots with explicit field presence. Broader runtime, child conversion, and full style coverage remain outside this slice. The local module is `gpui-go`; this does not settle a public distribution path.

[Tests and vet](../../../evidence/authoring-validation.json) passed for byte-identical sources in an isolated Go 1.26.5 fixture. Root Go 1.27 compilation is still unverified because the selected toolchain upgrade was blocked. The root module retains its accepted Go 1.27.0 minimum. No speed, storage, or full-GPUI parity claim follows from these tests.

### Explicit handover and counter-view draft, 2026-10-04

The user transferred the existing claim from “Golang GPUI Feasibility Research” (`32c92329-afec-4ee5-a625-5c3d85a35002`) to this standalone continuation. This is the successor conversation, not a competing worker. Prior accepted decisions and the bounded implementation scope remain in force; the blocked toolchain installation is not to be retried or bypassed.

Added the [counter-view interface proposal](../../../docs/counter-view-api.md) as documentation only. It connects `Render`, `RenderOnce`, `Element`, `Entity[T]`, `Context[T]`, weak listener adaptation, notifications, typed events and routed actions. A focused read-only check of existing pinned snapshots confirmed relevant phase, callback, effect and routing contracts; no new broad research or runtime implementation was performed.

Recommendations awaiting human input: reject double attachment of a mutable recipe/builder; report unsupported child types immediately with a clear panic; use typed phase adapters for low-level custom elements. These are proposals, not accepted decisions. Additional technical work remains on emitter/event declarations, action registration, conversion rules, exact access/error signatures, and complete identity/accessibility hooks. Generic call inference has not been compiler-validated.

Decision 04 remains dependent and unclaimed. Its ownership/cancellation choices must feed back into entity and subscription usage before the overall design becomes a specification. This ticket remains claimed and unresolved.

### Original-source research requested, 2026-10-04

The user asked to “do research the original and stuff as well.” This directs further primary-source investigation, not acceptance of the three proposed choices. Compared original Zed GPUI at `a84689073d296dfd39987bc7dd478e43ef76d83a` with the accepted CE reference. See the [original-source report](../../../research/10-original-gpui-authoring.md); the comparison revision does not replace the parity pin.

The original includes a real counter example using typed actions, weak listener adapters, focus, keybindings, string children and explicit notification. Refined the draft: recipe consumption does not prohibit shared persistent entity handles; Rust rejects unsupported child types at compile time, so a Go panic is an adaptation, not upstream behavior; and typed custom-element phase state is already an upstream contract, while the extra Go adapter call is a proposed translation. Research these engineering choices before returning them to the user as unsupported preference questions.

Emitter/event compatibility declarations and action registration remain substantive gaps: typed event payloads alone do not reproduce `EventEmitter<E>`, and an empty Go action struct alone does not reproduce names, cloning/equality, and JSON construction/schema. No runtime code or toolchain changes were made.

### Wayfinder continuation: concrete authoring round, 2026-10-04

The user instructed continuing the Wayfinder process. Read the installed Wayfinder skill and retained this transferred claim. Created a [concrete decision round](../../../docs/authoring-decision-round.md) with two independent application-authoring questions:

1. Default to immediate panic for invalid fluent children, with a separate checked conversion for external/dynamic input, or require recoverable error handling in ordinary tree construction?
2. Pass a typed event declaration to emission/subscription calls to catch source/payload mismatches at compile time, or prefer shorter payload-only emission with registered pair checks at runtime?

Recommend the first option in each case. No answer is recorded yet. The typed declaration is a Go proposal, not compiled proof or a claim to reproduce Rust's trait declaration authority. A bounded read-only design review identified canonical type-pair identity, zero values, interface payloads, exported declarations, and type-conversion escapes as specification requirements. Official Go 1.27 docs were checked; no toolchain operation or compiler probe was attempted.

The round also records recommended directions for view/custom-element adapters, action descriptors, controlled entity access and conversion rules. Those still need a coherent signature review before the ticket can resolve. The dependent lifetime ticket remains open and unclaimed.

### Investigate GPUI-style short checked event calls, 2026-10-04

The user asked how original GPUI handles both questions. Explained that Rust rejects unsupported children at compile time and uses a one-time `EventEmitter<E>` declaration with short `cx.emit(payload)` calls. The user then said “ok do that” to investigating a Go translation preserving that experience. This authorizes the focused investigation, not either previously proposed choice, broader implementation, or another toolchain attempt.

Completed [Go event declaration research](../../../research/11-go-event-declarations.md). The earlier extra-argument-versus-runtime choice was incomplete: payload markers, auxiliary owner-pointer constraints, and owner-specific context facades can express short checked calls with different restrictions. The shared core `DismissEvent` contract rules out a single-source payload marker as the framework's only event mechanism. No general impossibility theorem is claimed.

Current recommendation: a typed declaration receiver, illustrated as `countChanged.Emit(cx, payload)` and `countChanged.Subscribe(summaryCx, counter, handler)`. This supports open source/payload pairs and avoids the earlier extra token argument. It still requires explicit capability integration for the Go equivalent of `ManagedView`, plus descriptor identity/zero-value/export/conversion rules. Exact context-receiver syntax remains an alternative with extra per-source machinery or runtime checks. The recommendation is unaccepted and all signatures remain uncompiled.

Updated the [current round](../../../docs/authoring-decision-round.md) and counter draft pointers. Child failure policy remains unanswered. Keep this ticket claimed; do not record a final authoring resolution yet. No framework source, compiler experiment, or installation was performed.

### Accepted event direction and research delegation, 2026-10-04

The user subsequently said "ok do rec then," accepting the typed declaration receiver direction: `countChanged.Emit(cx, payload)` and declaration-based subscription. This supersedes the earlier comments' unaccepted status without rewriting their history. Exact signatures remain uncompiled; the event runtime is not implemented.

The user instructed research first and delegated routine engineering decisions to source-backed investigation, with only genuine human tradeoffs returned for discussion. Research must use Roboco side chats with `pi` / `iroha/dashscope/glm-5.3`, reasoning `high`. Reuse side chat `a890d77e-8dc1-4fd1-848b-146cc2740eba`; the main chat retains this claim and integration responsibility. Its bounded assignment covers event descriptor/capability details, action registration, child conversion, and full view/element extension contracts, writing only `research/12-authoring-contract-completion.md`.

The whole authoring decision remains claimed and unresolved. Ownership/effects/scheduling remains a separate dependent decision. No runtime implementation, compiler probe, or retry of the policy-blocked toolchain upgrade is authorized by this continuation.

### Source-backed completion review, 2026-10-04

Resumed the existing Roboco research side chat on the required `pi` / `iroha/dashscope/glm-5.3` route. The fresh attempt produced [completion research](../../../research/12-authoring-contract-completion.md). Main review required corrections to action clone/equality preservation, pointer/state constraints, public view conversion, nil/atomicity guarantees, and unsupported claims of accepted divergences. The research output is evidence and candidate design, not compiler proof.

The [current authoring round](../../../docs/authoring-decision-round.md) records engineering selections made under the user's delegation: zero-value typed event descriptors sharing entity/payload routing; explicit action clone/equality services independent of registration; panic for invalid fluent child use with a checked path for dynamic data; staged bulk attachment; fresh consumed recipes; `ViewOf` plus a public properties/identity view contract; and typed custom-element phases with the complete identity/accessibility capability set. These are delegated technical selections, not invented quotations of user acceptance. Previous preference-question comments are historical.

Remaining work is precise context/listener/access/error signatures, action descriptor-to-handler association, conversion reservation/consumption behavior, and Go 1.27 positive/negative signature validation. Ownership, cancellation, payload mutation and scheduling remain the separate dependent decision. Keep this ticket claimed; no runtime implementation or new toolchain operation occurred.

### Wayfinder closure review, 2026-10-04

The user instructed continuing Wayfinder. Reused the required DashScope GLM-5.3 side chat for [focused closure research](../../../research/13-authoring-closure-contracts.md), then reviewed and corrected the access declarations, canonical action services, boxed-action payload behavior, and child staging/reentrancy contract. This completes the remaining authoring design choices under the previously explicit research-first delegation; it does not imply executable validation.

## Answer

Resolved as a documentation-level authoring decision on 2026-10-04. The [selected Go authoring contract](../../../docs/authoring-decision-round.md) is the detailed interface reference; the [counter example](../../../docs/counter-view-api.md) shows it in use. Preserve the distinction between the user's explicit choices and engineering selections made under their delegation.

**Explicit user choices:** Go 1.27 minimum; convenient string children with dynamic validation; no mandatory custom-author generation via the researched/implemented constructor-bound fluent helper; typed event declaration receivers, accepted with "ok do rec then"; and research-first resolution of routine technical details using Roboco `pi` / `iroha/dashscope/glm-5.3`.

**Selected engineering contract:**

- Value-embedded, constructor-bound styling preserves concrete fluent methods. Refinements retain presence and explicit zero/false/clear distinctions. The existing helper is a bounded implemented slice, not full styling coverage or a runtime.
- Concrete generic listener/event methods retain owner, source, observer and payload typing. A nongeneric access carrier supports typed entity read/update callbacks, void and result-returning mutation forms, weak failure paths, and visual access. Wrong-app, stale-context, foreground and conflicting-access misuse panic; weak release and window absence/closure have explicit errors. Borrowed-pointer misuse cannot be completely prevented by Go.
- Event descriptors have valid zero values and canonical entity/declared-payload routing, including shared/imported and nil interface payloads. Pair-dependent representation protects ordinary conversion; managed views expose rendering, focus and typed dismissal capability tied to the same state pointer. The public declaration is not a security capability or an implementation of Rust coherence.
- Action dispatch remains keyed by payload type. Exactly one immutable descriptor definition per payload type supplies explicit name, cloning/equality, JSON/schema and deprecation metadata. Reuse copies/imports; a second definition is an error. Registration is independent and permits typed unregistered use. Bound boxed handlers preserve the pinned behavior of receiving their captured cloned binding, with ordinary routing/propagation rules.
- One child conversion policy supports explicit converters, strings, identity-bearing views and stateless recipes. Fluent misuse panics; checked single/bulk paths return errors. A gated staging/reservation/commit operation preserves the destination child list on failure without claiming to undo user-converter effects elsewhere. Framework-owned handle aliases share a consumption token; independently wrapped raw third-party recipes cannot be universally diagnosed. `OwnRecipe` provides an explicit handle when those guarantees are needed.
- The public `View` properties/identity extension path and explicit `ViewOf` pointer/state adapter remain open to external authors. `CustomElement[L, P]` preserves typed phases, optional identities and source location, inspector state, all pinned accessibility hooks, and restricted-parent capabilities. Sharing model state remains distinct from sharing sibling view identity.

**Rationale and alternatives:** this keeps the reference's typed relationships and extension paths while adapting Rust moves, associated types and trait defaults to Go. It avoids mandatory generation, runtime-only event compatibility, dropped action clone services, implicit arbitrary formatting, and public untyped phase state. Staged child attachment is a deliberate Go adaptation; Rust's lazy iterator is not evidence of transactional conversion. The costs are constructor binding, explicit descriptors/adapters, runtime misuse checks, a once-per-type action definition, and documented limits on raw-pointer and third-party alias detection.

**Verification still required:** [Validate the selected Go authoring signatures](10-validate-authoring-signatures.md) now holds concrete cross-package positive and negative compiler cases. The final conformance decision depends on it. The Go 1.27 upgrade remains policy-blocked; no install retry, alternate compiler, new compiler probe or runtime code was used to close this design decision. Existing Go 1.26.5 helper tests do not prove these signatures.

**Separate decision:** [Define entity ownership, effects, and scheduling](04-define-runtime-ownership.md) remains open. Retention/disposal, weak upgrades, subscription/task cancellation, payload ownership, effect ordering, thread affinity, keyed/frame state and native cleanup must feed back into these lifetime-facing signatures. This closure neither decides those policies nor claims the full specification or core parity is complete.
