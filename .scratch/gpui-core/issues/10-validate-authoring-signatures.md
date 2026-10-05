# Validate the selected Go authoring signatures

Parent: ../map.md
Type: task
Labels: wayfinder:task
Status: resolved
Assignee: Codex/root
Blocked by: 03, 04

## Question

Do the selected authoring declarations and call sites compile with Go 1.27, including external-package authors and the intended negative cases?

This is a concrete verification prerequisite for freezing the authoring surface in the final specification. It does not reopen already accepted ergonomic choices merely because the broader runtime is unimplemented.

## Execution boundary

The earlier attempted Go upgrade was rejected before execution. On 2026-10-05 the user installed Go themselves, and main verified `go1.27.1 windows/amd64` at `C:\Program Files\Go\bin\go.exe` using `GOTOOLCHAIN=local`. The user's subsequent "ok do them" explicitly authorizes the bounded signature-validation fixture, conformance-plan completion, specification and implementation tickets. Use the installed local compiler without installation or automatic toolchain downloads. Runtime, renderer and broader framework implementation remain outside this fixture.

## Required evidence

- Positive examples from the [counter](../../../docs/counter-view-api.md) and [selected authoring contract](../../../docs/authoring-decision-round.md), using at least two external packages.
- Verify the [distribution contract](../../../docs/distribution-contract.md)'s root package name `gpui` at local import `gpui-go`, the existing separate `authoring` package, shared public style/type identity and absence of root/authoring cycles. External extension authors must not need imports from `internal`.
- Concrete generic listener methods with owner and event inference; event subscription with a different observer state; shared/imported payload types; zero-value and independently declared equivalent event descriptors.
- Negative cases for wrong source, payload, observer and callback types, plus attempts to relabel event descriptors through ordinary explicit conversion.
- Pointer/state constraints for renderable entities and managed views, with explicit and inferred auxiliary type arguments; non-renderable state must fail at the typed adapter.
- Unit-action payload restriction, typed action handlers/dispatch, parameterized clone/equality descriptors and typed dispatch without named registration.
- Public conversion, stateless recipes, identity-bearing views, typed custom-element layout/prepaint states, accessibility companions and restricted parents authored outside the core package.
- Mixed and typed-slice child call sites. Compilation alone does not validate consumption, atomic attachment, lifetime, scheduling, or accessibility behavior.
- Lifetime-facing declarations from the [runtime contract](../../../docs/runtime-ownership-contract.md): typed creation, `RetainInto`, `TransferInto`, weak `UpgradeInto`, callback entity access, `ViewOfIn`, and typed `EmitOwned` factories. Compile the two-window example; behavioral borrowing/closing rules require runtime tests, not a claimed compile-time borrow checker.

Record toolchain identity, exact fixture inputs and outputs, and expected failures. Distinguish syntax/signature validation from runtime behavior; do not report this task passed because isolated styling tests passed under Go 1.26.5.

## Completion boundary

Resolve after the positive cases compile and each intended negative case fails for the relevant type constraint. A failure feeds a correction into the selected contract; broad ergonomic changes require explicit reconsideration. Behavioral proof remains in the conformance decision.

## Comments

- 2026-10-05 — Codex/root claimed signature validation. User installation clears the previous compiler-availability blocker; explicit authorization covers the complete stated planning sequence, superseding the skill's ordinary one-ticket-per-invocation stopping point. Main owns the isolated compiler fixture and integration; DashScope GLM-5.3 reviews source coverage and conformance planning independently.

## Answer

Resolved on 2026-10-05 using user-installed `go1.27.1 windows/amd64`. The [isolated fixture](../../signature-validation/README.md) models the selected root package/import, copies the existing authoring helper byte-for-byte, extracts the counter/label/subscriber/two-window snippets, and adds external consumer/shared-payload packages. Operation bodies are stubs and are not executed as a framework.

All positive packages compiled. Root and fixture `go test ./...` and `go vet ./...` passed. All **29 intentional negative cases failed for their expected constraint**, and all 29 minimally corrected controls passed. The [complete evidence record](../../../evidence/authoring-signature-validation.json) contains commands, output, diagnostic patterns, exact fixture hashes and helper-copy hashes. The initial runner wrapped stderr text across lines; capture was corrected and the full run passed without weakening the expected diagnostics.

Verified generic-method inference, typed event emission/subscription with independent observer state, declared interface payloads, zero/comparable event descriptors including non-comparable payload types, explicit-conversion rejection across pairs, pointer-state view/managed-view constraints, action payload/handler/unit restrictions, typed access and ownership-facing calls, complete custom-element phases/accessibility companions, mixed/typed children, restricted parents and sealed access carrier. Dynamic child values and borrowing/cycle rules remain runtime obligations.

Two concrete corrections: keep `Div()` as constructor and use `DivElement` for its concrete type (Go package identifiers cannot share that name); isolate copied upstream evidence with a nested evidence module so root Go commands do not compile reference files. The original snapshot bytes and hashes are unchanged. Root style identity can alias the independent `authoring` type without an import cycle. The authoring and counter documents now reflect the validated forms.

No entity/event/action/attachment/lifetime/native behavior or parity was demonstrated. Existing styling tests execute, but broader positive API functions are only typechecked. No compiler installation/download, native build, renderer or benchmark ran. This resolves the signature prerequisite and unblocks conformance-plan completion under the user's explicit sequence authorization.
