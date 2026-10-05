# Conformance gates and upstream update policy

Research date: 2026-10-04; revised after main review. Proposal for [ticket 09](../.scratch/gpui-core/issues/09-define-conformance-and-update-policy.md), which **remains open** — this report proposes methodology, not frozen thresholds, and **no metric is selected yet**. No test, probe, native build, or benchmark has run. Inputs: the four selected contracts, tickets 01/02/08/10, [research 08](08-upstream-version-check.md), and the pinned sources.

## 1. Gate tiers: exact versus approximate

**Tier A — exact deterministic comparisons.** Layout (full-node bounds, unrounded f32 origins, measure-call transcripts under identical cache history), scene/plan (orders, batches, boundaries, replay output, surface-opacity pairing), text geometry (carets, selections, hit tests), effect ordering (notify coalescing/re-notify, per-occurrence FIFO emits, deferred activation), image/SVG decode outputs (BGRA frames, sizes, delays, EXIF-applied orientation, alpha masks). Pass = bit-for-bit equality (finite f32; signed zero normalized only where a fixture declares it) plus exact sequence equality. No tolerance; approximate evidence never substitutes for an existing exact gate.

**Tier B — behavioral traces with recorded transcripts.** IME composition, keyboard, clipboard, per-message return policies, ownership lifecycle events, scripted assisted IME/UIA sessions (§4). Pass = transcript equality against a recorded reference run, with every documented Go adaptation asserted explicitly and labeled — never silently mixed with source-preserved behavior.

**Tier C — approximate visual comparison.** The user's accepted target — "approximately 98% visual closeness with small differences permitted" ([parity policy](../.scratch/gpui-core/issues/01-define-windows-parity.md)) — **is not defined as a metric**, and this report does not invent one. What is proposed is the *methodology for deriving and freezing* a defensible numeric gate, in order:

1. **Controlled captures.** The Go port and the pinned Rust reference render the same fixture set on the recorded PC with controlled fonts, DPI, scale, and ClearType settings; both sides capture device-pixel frames. Captures are per-feature (per primitive class, per text setting, per composition mode) rather than one blended screenshot.
2. **ROI-based per-feature reporting.** Frames are compared per region of interest with a per-feature report — a shadow, a ClearType band, a gradient, a filtered group each produce their own diff statistics — so an aggregate number can never average away one badly wrong feature. No blanket text or transparency masks: any masked region must be individually justified and reported, because a mask that hides text bands can hide missing glyphs — missing content is a failure by definition, not noise.
3. **Reference-repeat noise measurement.** Repeat the reference captures under the same protocol and retain the sample count and per-region variation. Estimate repeatability before selecting thresholds; a single pair cannot characterize all noise. A small score is never sufficient to excuse a structural defect such as missing content, even if unrelated reference variation is larger.
4. **Deliberate defect calibration.** Known defects are injected into the Go-side output — a dropped shadow, a one-pixel edge shift, a wrong gamma, a missing filter pass, one dropped glyph — and the candidate metric must detect every injected defect at the level the policy cares about *before* any threshold is frozen. A metric that passes injected defects is rejected, whatever its aggregate behavior.

Only after those four steps is a numeric gate (whatever form — pixel-difference statistics, SSIM, or a composite) proposed, reviewed, and frozen; **SSIM is available as a diagnostic alongside other metrics** rather than excluded or adopted wholesale. This section deliberately names no numbers.

## 2. Evidence matrix

| Area | Gate | Evidence | Criterion |
|---|---|---|---|
| Layout | A | pinned Rust `TaffyLayoutEngine` oracle + fixed measurement transcripts (ticket 05 matrix) | exact per-node and transcript equality |
| Scene/plan | A | independent pinned Rust scene oracle (ticket 07 corpus) | exact command/batch/boundary equality |
| Text geometry | A | pinned Parley/rasterizer oracle with recorded font bytes | exact caret/selection/bounds |
| Image/SVG/asset | A | pinned decode/rasterization oracle: BGRA frames, delays, EXIF orientation, alpha masks, 8192 cap behavior | exact frame/geometry equality |
| Effects/lifetime | A/B | deterministic dispatcher, controlled completions, virtual time; ownership-contract mandatory cases | transcript equality + adaptation labels |
| Native ABI | A | record round-trips; GC pressure during callbacks; panic containment both directions | invariants hold |
| Input/IME/UIA | B | scripted assisted sessions (§4), recorded traces; nonzero-origin candidate fixture; caret-coordinate discrepancy fixture | single delivery, correct ranges/geometry |
| Renderer | A + C | Tier A plan outputs; Tier C per-feature captures for every primitive class, path AA, glyph formats, filters, composition modes, capture import | A exact; C per frozen gate |
| Distribution | B | clean-machine `CGO_ENABLED=0` build+run; typed-error injection; concurrent publication; bundle override | typed errors, no silent fallback |
| Responsiveness | B | frame-pacing/input-latency/stutter observation on reference workloads, traces retained | no perceptible stutter/input lag (criteria 02); no hard budget |

## 3. Full source capability inventory and compiled-feature coverage

Conformance covers the **full pinned capability surface, not a reduced core**. The inventory must trace windows/multi-window lifecycle, dialogs and prompts, menus/jump lists, notifications, clipboard formats/metadata/images, credentials, drag/drop, Direct Manipulation, capture, character palette, keyboard layouts/dead keys/AltGr, IMM32, accessibility, DPI, suspend/resume, restart, cursors/settings, text and **assets/images/SVG** to actual source declarations and Windows implementations. This is a checklist to audit, not a claim that every named service is implemented on Windows by the pin. Record each row's source, applicability, implementation owner, exact feature conditions and evidence or approved exception. The [asset research](18-module-native-distribution.md) adds registry/path/URI loading, distinct clipboard/resource decode modes, animation, EXIF, font resolution and alpha-mask icons.

**Compiled-feature coverage is exact and recorded**: capture is required in the selected artifact, while runtime availability may be absent. The native D3D11 selection does not supply the optional wgpu/custom-GPU surface variants. Audit inspector/test support, hot-patching, profiling and stacker separately; an absent Rust implementation detail may have a Go counterpart, and neither an omitted build flag nor an unsupported error automatically grants a parity exception. Do not declare all these features excluded without that review. Capability claims list exactly which applicable features were exercised.

## 4. Scripted assisted IME and UIA sessions

Not "inherently manual": **scripted assisted sessions** — deterministic scripts drive the app (composition steps, focus transfers, teardown sequences) while a human assists only where the OS UI is genuinely outside automation (choosing a candidate, confirming a system dialog). Recorded transcripts make repeat runs comparable. Third-party screen readers run **when already available on the recorded PC — no install requirement**; the environment manifest (§5) records which tools were present. Gates: JCK IME composition (commit/cancel/focus-loss, surrogate input, candidate position at nonzero origin and after move/scroll/DPI change); screen-reader activation before the first full tree, navigation, text/selection actions, provider queries during teardown; clipboard interop; modal dialogs and live resize with tasks in flight.

## 5. Environment and reference fixtures

Per-run **environment manifest** stored with results: OS build, DPI mode and scale, installed fonts (family + file hash), IME list, keyboard layout, GPU adapter/feature level as reported at runtime, DirectWrite parameters, reader/inspection tool versions present. Reference fixtures pin exact font bytes/faces/axes and wrap/clamp inputs; oracle outputs are recorded JSON committed to the repo for offline validation; the independent-oracle rule stands (pinned Rust compiled separately, never the bridge comparing itself).

## 6. Fixed pin and update policy

The CE pin stays `254b5dbd47cbb5acbcc5bbdcbb322a339276c88a`; drift is tracked by the [research 08](08-upstream-version-check.md) method without moving the target. **Every pin change requires explicitly recorded approval** — a re-pin procedure: new commit + diff summary; rebuild services/oracles/fixtures; full gate matrix re-run; exception ledger update; decision recorded. **No automatic rebaseline exists.** Dependency or security patches (e.g., a CVE fix in a native dependency) form a separate, smaller procedure: recorded approval + targeted re-gates for the affected service + ledger entry — they never silently move the parity baseline. The **exception ledger** (versioned with the pin) enumerates every documented divergence: Go lifetime adaptations, the same-device capture adapter, completion/abort additions, panic/ABI rules, and accepted parity-policy allowances; anything unlisted is a defect.

## 7. Clean consumer artifact tests

Clean machine (no Rust/C toolchain, no VC++ redist — static CRT makes it meaningful) builds and runs a `gpui-go` app with `CGO_ENABLED=0`; typed-error injection for every distribution failure class; concurrent first-run publication; unwritable-cache → bundle override; bundle loading verified. Precondition for ever claiming the consumer path; unexecuted.

## 8. Authoring signature validation (ticket 10) — blocked prerequisite

The full case list lives in [ticket 10](../.scratch/gpui-core/issues/10-validate-authoring-signatures.md) (positive counter/two-window examples with two external packages; inference cases; shared/imported payloads; zero-value and equivalent descriptors; negative relabeling and type mismatches; pointer-state constraints; managed views; unit-action restriction; typed dispatch without registration; external custom elements and a11y companions; mixed and typed-slice children; lifetime-facing declarations). Execution remains blocked on the policy-blocked Go 1.27 toolchain; this report only records the dependency. Behavioral authoring checks join Tier B; compile validation is not claimed.

## 9. Status

All content is a proposal pending main review; every executable item — oracles, fixtures, traces, clean-machine builds, assisted sessions, metric derivation — is unrun. Tier C explicitly has no numbers yet: the methodology of §1 exists so that future thresholds are calibrated, not invented.
