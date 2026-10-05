# Set development and runtime success criteria

Parent: ../map.md
Type: grilling
Labels: wayfinder:grilling
Status: resolved
Assignee: Codex /root (current conversation)
Blocked by: none

## Question

What measurable build-time, storage, and runtime criteria make this port successful for the user?

Define representative workloads, clean and incremental build scenarios, developer setup burden, total disk accounting, and acceptable runtime differences against the pinned Rust reference. Distinguish required budgets from measurements still needed. Use the existing protocol; do not invent performance results or require the user to supply facts that can later be measured.

## Existing evidence

- [build footprint and validation](../../../research/05-build-footprint-and-validation.md)

## Resolution boundary

Record the user's accepted decision and rationale, alternatives considered, any explicit unresolved evidence needs, and newly exposed questions. Do not treat a proposal in the research as an accepted answer. This ticket is planning work.

## Comments

The user accepted Go plus downloaded native libraries for ordinary application development, with no local Rust or C compiler required. They declined a Rust-baseline measurement exercise and numerical targets for now, stating their expectation that Go would be faster. They accepted modest extra runtime RAM if responsiveness remains comparable.

## Answer

### Accepted development criteria

- Ordinary application development should require the Go toolchain and prebuilt native artifacts, without requiring a Rust or C compiler. Native-library compilation and related toolchain work belong to framework maintainers.
- Faster edit-and-run cycles and lower development disk usage remain motivations. No hard timing, storage, or percentage-improvement targets are set now, and a Rust performance baseline is not a prerequisite for progressing with the port.
- The user's expected speed improvement is an expectation, not a measured result or guarantee. Existing benchmarking proposals are optional future tools, not mandatory work the user has requested.
- A modest increase in runtime RAM is acceptable provided the application remains smooth and responsive. Noticeable stuttering or input lag remains a problem; no numerical RAM or latency allowance has been selected.

### Implications for later decisions

Backend and packaging candidates must satisfy the application-consumer toolchain constraint. A candidate requiring app developers to compile a C/Rust binding is not covered merely by permission to use native libraries. Maintainer builds may use those toolchains. Generated artifacts should not force consumers through native build steps during routine Go builds.

The conformance decision should retain functional/visual verification and checks for observable responsiveness problems, while keeping comparative build-speed and disk benchmarks optional. Any future performance claim needs actual evidence. No benchmark has been run and no implementation is authorized by this resolution.

### Alternatives considered

The user chose progress without an upfront measurement gate over measuring the Rust baseline and selecting budgets now. They accepted a modest memory tradeoff rather than requiring equal memory consumption. Go-only consumer tooling was accepted over requiring every application developer to install native compilers.
